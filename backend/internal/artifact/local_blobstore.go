package artifact

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	localPartPattern = regexp.MustCompile(`^([1-9][0-9]{0,3}|10000)\.part$`)
	localUploadID    = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type localUploadMetadata struct {
	ContentType string `json:"content_type"`
	ObjectKey   string `json:"object_key"`
}

// LocalBlobStore stores multipart parts in private temporary directories and
// atomically joins them without buffering a complete part or object.
type LocalBlobStore struct {
	root string
}

// NewLocalBlobStore initializes an isolated absolute storage root.
func NewLocalBlobStore(root string) (*LocalBlobStore, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("local artifact storage root must be absolute")
	}
	cleanRoot := filepath.Clean(root)
	if err := os.MkdirAll(filepath.Join(cleanRoot, "objects"), 0o700); err != nil {
		return nil, fmt.Errorf("create local object root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cleanRoot, ".multipart"), 0o700); err != nil {
		return nil, fmt.Errorf("create local multipart root: %w", err)
	}
	return &LocalBlobStore{root: cleanRoot}, nil
}

// Backend identifies persisted blob ownership.
func (*LocalBlobStore) Backend() string { return "local" }

// Name identifies the readiness dependency.
func (*LocalBlobStore) Name() string { return "object_storage" }

// Check verifies the local root remains writable without changing business data.
func (store *LocalBlobStore) Check(context.Context) error {
	file, err := os.CreateTemp(store.root, ".readiness-*")
	if err != nil {
		return fmt.Errorf("check local artifact storage: %w", err)
	}
	name := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(name)
		return fmt.Errorf("close local artifact readiness file: %w", closeErr)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("remove local artifact readiness file: %w", err)
	}
	return nil
}

// CreateMultipart creates a refresh-safe temporary upload directory.
func (store *LocalBlobStore) CreateMultipart(
	_ context.Context,
	objectKey string,
	contentType string,
) (MultipartUpload, error) {
	if err := ValidateObjectKey(objectKey); err != nil {
		return MultipartUpload{}, err
	}
	uploadID, err := randomLocalUploadID()
	if err != nil {
		return MultipartUpload{}, err
	}
	directory := store.uploadPath(uploadID)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return MultipartUpload{}, fmt.Errorf("create local multipart upload: %w", err)
	}
	metadata, err := json.Marshal(localUploadMetadata{
		ContentType: contentType,
		ObjectKey:   objectKey,
	})
	if err != nil {
		_ = os.RemoveAll(directory)
		return MultipartUpload{}, fmt.Errorf("encode local multipart metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "upload.json"), metadata, 0o600); err != nil {
		_ = os.RemoveAll(directory)
		return MultipartUpload{}, fmt.Errorf("write local multipart metadata: %w", err)
	}
	return MultipartUpload{
		ObjectKey:        objectKey,
		ProviderUploadID: uploadID,
	}, nil
}

// PresignPart is intentionally unsupported: Core signs its own streaming
// transfer route for Local storage.
func (*LocalBlobStore) PresignPart(
	context.Context,
	MultipartUpload,
	int,
	time.Duration,
) (SignedRequest, error) {
	return SignedRequest{}, ErrDirectTransferUnsupported
}

// PutPart streams one exact-size part to a temporary file and atomically makes
// it visible to session recovery.
func (store *LocalBlobStore) PutPart(
	ctx context.Context,
	upload MultipartUpload,
	partNumber int,
	body io.Reader,
	sizeBytes int64,
) (CompletedPart, error) {
	if partNumber < 1 || partNumber > MultipartMaxParts || sizeBytes < 0 {
		return CompletedPart{}, ErrInvalidPart
	}
	directory, _, err := store.loadUpload(upload)
	if err != nil {
		return CompletedPart{}, err
	}
	if err := ctx.Err(); err != nil {
		return CompletedPart{}, err
	}

	temporary, err := os.CreateTemp(directory, ".part-*")
	if err != nil {
		return CompletedPart{}, fmt.Errorf("create local part: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	digest := md5.New() // S3-compatible ETag only; never used as the content hash.
	written, copyErr := io.Copy(
		io.MultiWriter(temporary, digest),
		io.LimitReader(&contextReader{ctx: ctx, reader: body}, sizeBytes+1),
	)
	if copyErr != nil {
		return CompletedPart{}, fmt.Errorf("stream local part: %w", copyErr)
	}
	if written != sizeBytes {
		return CompletedPart{}, fmt.Errorf("%w: expected %d bytes, received %d", ErrInvalidPart, sizeBytes, written)
	}
	if err := temporary.Sync(); err != nil {
		return CompletedPart{}, fmt.Errorf("sync local part: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return CompletedPart{}, fmt.Errorf("close local part: %w", err)
	}
	partPath := filepath.Join(directory, fmt.Sprintf("%d.part", partNumber))
	if err := os.Rename(temporaryPath, partPath); err != nil {
		if removeErr := os.Remove(partPath); removeErr != nil &&
			!errors.Is(removeErr, os.ErrNotExist) {
			return CompletedPart{}, fmt.Errorf("replace local part: %w", removeErr)
		}
		if retryErr := os.Rename(temporaryPath, partPath); retryErr != nil {
			return CompletedPart{}, fmt.Errorf("publish local part: %w", retryErr)
		}
	}
	return CompletedPart{
		ETag:       hex.EncodeToString(digest.Sum(nil)),
		PartNumber: partNumber,
		SizeBytes:  sizeBytes,
	}, nil
}

// ListParts reconstructs provider state from durable local part files.
func (store *LocalBlobStore) ListParts(
	ctx context.Context,
	upload MultipartUpload,
) ([]CompletedPart, error) {
	directory, _, err := store.loadUpload(upload)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrUploadNotFound
		}
		return nil, fmt.Errorf("list local multipart upload: %w", err)
	}
	parts := make([]CompletedPart, 0, len(entries))
	for _, entry := range entries {
		matches := localPartPattern.FindStringSubmatch(entry.Name())
		if entry.IsDir() || matches == nil {
			continue
		}
		partNumber, parseErr := strconv.Atoi(matches[1])
		if parseErr != nil {
			return nil, fmt.Errorf("parse local part number: %w", parseErr)
		}
		part, partErr := hashLocalPart(ctx, filepath.Join(directory, entry.Name()), partNumber)
		if partErr != nil {
			return nil, partErr
		}
		parts = append(parts, part)
	}
	sort.Slice(parts, func(left, right int) bool {
		return parts[left].PartNumber < parts[right].PartNumber
	})
	return parts, nil
}

// CompleteMultipart validates the exact provider part list, streams the join,
// and atomically publishes the staging object.
func (store *LocalBlobStore) CompleteMultipart(
	ctx context.Context,
	upload MultipartUpload,
	parts []CompletedPart,
) (ObjectInfo, error) {
	if len(parts) == 0 {
		return ObjectInfo{}, fmt.Errorf("%w: no parts", ErrInvalidPart)
	}
	directory, metadata, err := store.loadUpload(upload)
	if err != nil {
		return ObjectInfo{}, err
	}
	actual, err := store.ListParts(ctx, upload)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := compareCompletedParts(actual, parts); err != nil {
		return ObjectInfo{}, err
	}

	objectPath, err := store.objectPath(metadata.ObjectKey)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o700); err != nil {
		return ObjectInfo{}, fmt.Errorf("create local object directory: %w", err)
	}
	if _, err := os.Stat(objectPath); err == nil {
		return ObjectInfo{}, ErrObjectExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return ObjectInfo{}, fmt.Errorf("check local object: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(objectPath), ".complete-*")
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("create local completed object: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	var total int64
	for _, part := range parts {
		source, openErr := os.Open(filepath.Join(directory, fmt.Sprintf("%d.part", part.PartNumber)))
		if openErr != nil {
			return ObjectInfo{}, fmt.Errorf("open local part %d: %w", part.PartNumber, openErr)
		}
		written, copyErr := io.Copy(
			temporary,
			&contextReader{ctx: ctx, reader: source},
		)
		closeErr := source.Close()
		if copyErr != nil {
			return ObjectInfo{}, fmt.Errorf("join local part %d: %w", part.PartNumber, copyErr)
		}
		if closeErr != nil {
			return ObjectInfo{}, fmt.Errorf("close local part %d: %w", part.PartNumber, closeErr)
		}
		if written != part.SizeBytes {
			return ObjectInfo{}, fmt.Errorf(
				"%w: part %d changed size",
				ErrInvalidPart,
				part.PartNumber,
			)
		}
		total += written
	}
	if err := temporary.Sync(); err != nil {
		return ObjectInfo{}, fmt.Errorf("sync local completed object: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return ObjectInfo{}, fmt.Errorf("close local completed object: %w", err)
	}
	if err := os.Rename(temporaryPath, objectPath); err != nil {
		if _, statErr := os.Stat(objectPath); statErr == nil {
			return ObjectInfo{}, ErrObjectExists
		}
		return ObjectInfo{}, fmt.Errorf("publish local completed object: %w", err)
	}
	if err := os.RemoveAll(directory); err != nil {
		return ObjectInfo{}, fmt.Errorf("remove completed local multipart upload: %w", err)
	}
	info, err := os.Stat(objectPath)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat local completed object: %w", err)
	}
	return ObjectInfo{
		ContentType:  metadata.ContentType,
		LastModified: info.ModTime(),
		SizeBytes:    total,
	}, nil
}

// AbortMultipart removes only the validated temporary session directory.
func (store *LocalBlobStore) AbortMultipart(
	_ context.Context,
	upload MultipartUpload,
) error {
	directory, _, err := store.loadUpload(upload)
	if err != nil {
		if errors.Is(err, ErrUploadNotFound) {
			return nil
		}
		return err
	}
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("abort local multipart upload: %w", err)
	}
	return nil
}

// Stat returns object metadata without opening the content.
func (store *LocalBlobStore) Stat(
	_ context.Context,
	objectKey string,
) (ObjectInfo, error) {
	objectPath, err := store.objectPath(objectKey)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := os.Stat(objectPath)
	if errors.Is(err, os.ErrNotExist) {
		return ObjectInfo{}, ErrObjectNotFound
	}
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat local object: %w", err)
	}
	return ObjectInfo{
		LastModified: info.ModTime(),
		SizeBytes:    info.Size(),
	}, nil
}

// Open returns a streaming object reader.
func (store *LocalBlobStore) Open(
	_ context.Context,
	objectKey string,
) (io.ReadCloser, error) {
	objectPath, err := store.objectPath(objectKey)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(objectPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open local object: %w", err)
	}
	return file, nil
}

// Promote atomically moves a staging object to an immutable final key.
func (store *LocalBlobStore) Promote(
	_ context.Context,
	sourceKey string,
	destinationKey string,
) error {
	sourcePath, err := store.objectPath(sourceKey)
	if err != nil {
		return err
	}
	destinationPath, err := store.objectPath(destinationKey)
	if err != nil {
		return err
	}
	if _, err := os.Stat(destinationPath); err == nil {
		return ErrObjectExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check local destination object: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
		return fmt.Errorf("create local destination directory: %w", err)
	}
	if err := os.Rename(sourcePath, destinationPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrObjectNotFound
		}
		return fmt.Errorf("promote local object: %w", err)
	}
	return nil
}

// Delete removes one exact validated object key and is idempotent.
func (store *LocalBlobStore) Delete(_ context.Context, objectKey string) error {
	objectPath, err := store.objectPath(objectKey)
	if err != nil {
		return err
	}
	if err := os.Remove(objectPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete local object: %w", err)
	}
	return nil
}

// PresignGet is intentionally unsupported: Core signs its own streaming route.
func (*LocalBlobStore) PresignGet(
	context.Context,
	string,
	time.Duration,
) (SignedRequest, error) {
	return SignedRequest{}, ErrDirectTransferUnsupported
}

func (store *LocalBlobStore) loadUpload(
	upload MultipartUpload,
) (string, localUploadMetadata, error) {
	if !localUploadID.MatchString(upload.ProviderUploadID) {
		return "", localUploadMetadata{}, ErrUploadNotFound
	}
	if err := ValidateObjectKey(upload.ObjectKey); err != nil {
		return "", localUploadMetadata{}, err
	}
	directory := store.uploadPath(upload.ProviderUploadID)
	contents, err := os.ReadFile(filepath.Join(directory, "upload.json"))
	if errors.Is(err, os.ErrNotExist) {
		return "", localUploadMetadata{}, ErrUploadNotFound
	}
	if err != nil {
		return "", localUploadMetadata{}, fmt.Errorf("read local multipart metadata: %w", err)
	}
	var metadata localUploadMetadata
	if err := json.Unmarshal(contents, &metadata); err != nil {
		return "", localUploadMetadata{}, fmt.Errorf("decode local multipart metadata: %w", err)
	}
	if metadata.ObjectKey != upload.ObjectKey {
		return "", localUploadMetadata{}, ErrUploadNotFound
	}
	return directory, metadata, nil
}

func (store *LocalBlobStore) uploadPath(uploadID string) string {
	return filepath.Join(store.root, ".multipart", uploadID)
}

func (store *LocalBlobStore) objectPath(objectKey string) (string, error) {
	if err := ValidateObjectKey(objectKey); err != nil {
		return "", err
	}
	root := filepath.Join(store.root, "objects")
	target := filepath.Join(root, filepath.FromSlash(objectKey))
	relative, err := filepath.Rel(root, target)
	if err != nil ||
		relative == "." ||
		relative == ".." ||
		strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", ErrInvalidObjectKey
	}
	return target, nil
}

func randomLocalUploadID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", fmt.Errorf("create local upload identity: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func hashLocalPart(
	ctx context.Context,
	path string,
	partNumber int,
) (CompletedPart, error) {
	file, err := os.Open(path)
	if err != nil {
		return CompletedPart{}, fmt.Errorf("open local part: %w", err)
	}
	defer file.Close()
	digest := md5.New()
	size, err := io.Copy(digest, &contextReader{ctx: ctx, reader: file})
	if err != nil {
		return CompletedPart{}, fmt.Errorf("hash local part: %w", err)
	}
	return CompletedPart{
		ETag:       hex.EncodeToString(digest.Sum(nil)),
		PartNumber: partNumber,
		SizeBytes:  size,
	}, nil
}

func compareCompletedParts(actual, requested []CompletedPart) error {
	if len(actual) != len(requested) {
		return fmt.Errorf("%w: provider part count differs", ErrInvalidPart)
	}
	for index := range requested {
		if requested[index].PartNumber != index+1 ||
			actual[index].PartNumber != requested[index].PartNumber ||
			actual[index].SizeBytes != requested[index].SizeBytes ||
			normalizeETag(actual[index].ETag) != normalizeETag(requested[index].ETag) {
			return fmt.Errorf(
				"%w: part %d differs",
				ErrInvalidPart,
				requested[index].PartNumber,
			)
		}
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(contents []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(contents)
}
