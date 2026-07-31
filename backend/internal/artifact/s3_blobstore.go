package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const maximumSignedURLTTL = 7 * 24 * time.Hour

// S3BlobStoreConfig contains only process-level object storage settings.
type S3BlobStoreConfig struct {
	AccessKey      string
	Backend        string
	Bucket         string
	Endpoint       string
	PublicEndpoint string
	Region         string
	SecretKey      string
}

// S3BlobStore implements the common contract for MinIO and S3-compatible
// providers. Internal API traffic and browser-facing signatures use separate
// endpoints so private service DNS never leaks to the browser.
type S3BlobStore struct {
	backend      string
	bucket       string
	client       *minio.Core
	httpClient   *http.Client
	public       *minio.Core
	readinessURL *url.URL
}

// NewS3BlobStore initializes authenticated internal and public signing clients.
func NewS3BlobStore(storageConfig S3BlobStoreConfig) (*S3BlobStore, error) {
	if storageConfig.Backend != "minio" && storageConfig.Backend != "s3" {
		return nil, fmt.Errorf("S3-compatible backend must be minio or s3")
	}
	if strings.TrimSpace(storageConfig.Bucket) == "" {
		return nil, fmt.Errorf("object storage bucket is required")
	}
	internal, err := newMinIOCore(storageConfig.Endpoint, storageConfig)
	if err != nil {
		return nil, fmt.Errorf("initialize internal object storage client: %w", err)
	}
	publicEndpoint := storageConfig.PublicEndpoint
	if publicEndpoint == "" {
		publicEndpoint = storageConfig.Endpoint
	}
	public, err := newMinIOCore(publicEndpoint, storageConfig)
	if err != nil {
		return nil, fmt.Errorf("initialize public object storage signer: %w", err)
	}
	readinessURL, err := url.Parse(storageConfig.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse object storage readiness endpoint: %w", err)
	}
	return &S3BlobStore{
		backend:      storageConfig.Backend,
		bucket:       storageConfig.Bucket,
		client:       internal,
		httpClient:   &http.Client{Timeout: 5 * time.Second},
		public:       public,
		readinessURL: readinessURL,
	}, nil
}

// Backend identifies persisted blob ownership.
func (store *S3BlobStore) Backend() string { return store.backend }

// Name identifies the readiness dependency.
func (*S3BlobStore) Name() string { return "object_storage" }

// Check verifies authenticated access to the configured bucket.
func (store *S3BlobStore) Check(ctx context.Context) error {
	if store.backend == "minio" {
		healthURL := *store.readinessURL
		healthURL.Path = strings.TrimRight(healthURL.Path, "/") + "/minio/health/ready"
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			healthURL.String(),
			nil,
		)
		if err != nil {
			return fmt.Errorf("create MinIO readiness request: %w", err)
		}
		response, err := store.httpClient.Do(request)
		if err != nil {
			return fmt.Errorf("check MinIO readiness: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("MinIO readiness returned HTTP %d", response.StatusCode)
		}
	}
	exists, err := store.client.BucketExists(ctx, store.bucket)
	if err != nil {
		return fmt.Errorf("check object storage bucket: %w", err)
	}
	if !exists {
		return fmt.Errorf("object storage bucket is unavailable")
	}
	return nil
}

// CreateMultipart starts a provider upload for a random staging key.
func (store *S3BlobStore) CreateMultipart(
	ctx context.Context,
	objectKey string,
	contentType string,
) (MultipartUpload, error) {
	if err := ValidateObjectKey(objectKey); err != nil {
		return MultipartUpload{}, err
	}
	uploadID, err := store.client.NewMultipartUpload(
		ctx,
		store.bucket,
		objectKey,
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		return MultipartUpload{}, fmt.Errorf("create S3 multipart upload: %w", err)
	}
	return MultipartUpload{
		ObjectKey:        objectKey,
		ProviderUploadID: uploadID,
	}, nil
}

// PresignPart creates a short-lived PUT bound to one upload, key, and part.
func (store *S3BlobStore) PresignPart(
	ctx context.Context,
	upload MultipartUpload,
	partNumber int,
	ttl time.Duration,
) (SignedRequest, error) {
	if err := validateProviderUpload(upload); err != nil {
		return SignedRequest{}, err
	}
	if partNumber < 1 || partNumber > MultipartMaxParts {
		return SignedRequest{}, ErrInvalidPart
	}
	if err := validateSignedURLTTL(ttl); err != nil {
		return SignedRequest{}, err
	}
	query := url.Values{}
	query.Set("partNumber", strconv.Itoa(partNumber))
	query.Set("uploadId", upload.ProviderUploadID)
	signed, err := store.public.Presign(
		ctx,
		http.MethodPut,
		store.bucket,
		upload.ObjectKey,
		ttl,
		query,
	)
	if err != nil {
		return SignedRequest{}, fmt.Errorf("sign S3 multipart part: %w", err)
	}
	return SignedRequest{
		ExpiresAt: time.Now().UTC().Add(ttl),
		Headers:   map[string]string{},
		Method:    http.MethodPut,
		URL:       signed.String(),
	}, nil
}

// PutPart provides a bounded streaming path used by adapter integration tests
// and trusted Core callers. Browsers use PresignPart instead.
func (store *S3BlobStore) PutPart(
	ctx context.Context,
	upload MultipartUpload,
	partNumber int,
	body io.Reader,
	sizeBytes int64,
) (CompletedPart, error) {
	if err := validateProviderUpload(upload); err != nil {
		return CompletedPart{}, err
	}
	if partNumber < 1 || partNumber > MultipartMaxParts || sizeBytes < 0 {
		return CompletedPart{}, ErrInvalidPart
	}
	part, err := store.client.PutObjectPart(
		ctx,
		store.bucket,
		upload.ObjectKey,
		upload.ProviderUploadID,
		partNumber,
		body,
		sizeBytes,
		minio.PutObjectPartOptions{},
	)
	if err != nil {
		return CompletedPart{}, mapS3UploadError("put S3 multipart part", err)
	}
	return CompletedPart{
		ETag:       part.ETag,
		PartNumber: part.PartNumber,
		SizeBytes:  part.Size,
	}, nil
}

// ListParts returns all provider parts in ascending order.
func (store *S3BlobStore) ListParts(
	ctx context.Context,
	upload MultipartUpload,
) ([]CompletedPart, error) {
	if err := validateProviderUpload(upload); err != nil {
		return nil, err
	}
	parts := make([]CompletedPart, 0)
	marker := 0
	for {
		result, err := store.client.ListObjectParts(
			ctx,
			store.bucket,
			upload.ObjectKey,
			upload.ProviderUploadID,
			marker,
			1000,
		)
		if err != nil {
			return nil, mapS3UploadError("list S3 multipart parts", err)
		}
		for _, part := range result.ObjectParts {
			parts = append(parts, CompletedPart{
				ETag:       part.ETag,
				PartNumber: part.PartNumber,
				SizeBytes:  part.Size,
			})
		}
		if !result.IsTruncated {
			break
		}
		if result.NextPartNumberMarker <= marker {
			return nil, fmt.Errorf("list S3 multipart parts returned an invalid marker")
		}
		marker = result.NextPartNumberMarker
	}
	sort.Slice(parts, func(left, right int) bool {
		return parts[left].PartNumber < parts[right].PartNumber
	})
	return parts, nil
}

// CompleteMultipart validates provider state before committing the object.
func (store *S3BlobStore) CompleteMultipart(
	ctx context.Context,
	upload MultipartUpload,
	parts []CompletedPart,
) (ObjectInfo, error) {
	if err := validateProviderUpload(upload); err != nil {
		return ObjectInfo{}, err
	}
	actual, err := store.ListParts(ctx, upload)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := compareCompletedParts(actual, parts); err != nil {
		return ObjectInfo{}, err
	}
	providerParts := make([]minio.CompletePart, len(parts))
	for index, part := range parts {
		providerParts[index] = minio.CompletePart{
			ETag:       normalizeETag(part.ETag),
			PartNumber: part.PartNumber,
		}
	}
	if _, err := store.client.CompleteMultipartUpload(
		ctx,
		store.bucket,
		upload.ObjectKey,
		upload.ProviderUploadID,
		providerParts,
		minio.PutObjectOptions{},
	); err != nil {
		return ObjectInfo{}, mapS3UploadError("complete S3 multipart upload", err)
	}
	return store.Stat(ctx, upload.ObjectKey)
}

// AbortMultipart is idempotent when a provider upload has already gone away.
func (store *S3BlobStore) AbortMultipart(
	ctx context.Context,
	upload MultipartUpload,
) error {
	if err := validateProviderUpload(upload); err != nil {
		return err
	}
	err := store.client.AbortMultipartUpload(
		ctx,
		store.bucket,
		upload.ObjectKey,
		upload.ProviderUploadID,
	)
	if isS3Code(err, "NoSuchUpload") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("abort S3 multipart upload: %w", err)
	}
	return nil
}

// Stat returns provider metadata.
func (store *S3BlobStore) Stat(
	ctx context.Context,
	objectKey string,
) (ObjectInfo, error) {
	if err := ValidateObjectKey(objectKey); err != nil {
		return ObjectInfo{}, err
	}
	info, err := store.client.StatObject(
		ctx,
		store.bucket,
		objectKey,
		minio.StatObjectOptions{},
	)
	if isS3NotFound(err) {
		return ObjectInfo{}, ErrObjectNotFound
	}
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat S3 object: %w", err)
	}
	return ObjectInfo{
		ContentType:  info.ContentType,
		ETag:         info.ETag,
		LastModified: info.LastModified,
		SizeBytes:    info.Size,
	}, nil
}

// Open returns a streaming provider object reader.
func (store *S3BlobStore) Open(
	ctx context.Context,
	objectKey string,
) (io.ReadCloser, error) {
	if _, err := store.Stat(ctx, objectKey); err != nil {
		return nil, err
	}
	object, err := store.client.Client.GetObject(
		ctx,
		store.bucket,
		objectKey,
		minio.GetObjectOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("open S3 object: %w", err)
	}
	return object, nil
}

// Promote copies a staging object to a content-addressed key, then removes the
// staging source. The destination is checked first to preserve immutability.
func (store *S3BlobStore) Promote(
	ctx context.Context,
	sourceKey string,
	destinationKey string,
) error {
	if err := ValidateObjectKey(sourceKey); err != nil {
		return err
	}
	if err := ValidateObjectKey(destinationKey); err != nil {
		return err
	}
	if _, err := store.Stat(ctx, destinationKey); err == nil {
		return ErrObjectExists
	} else if !errors.Is(err, ErrObjectNotFound) {
		return err
	}
	if _, err := store.client.Client.CopyObject(
		ctx,
		minio.CopyDestOptions{Bucket: store.bucket, Object: destinationKey},
		minio.CopySrcOptions{Bucket: store.bucket, Object: sourceKey},
	); err != nil {
		return fmt.Errorf("promote S3 object: %w", err)
	}
	if err := store.client.RemoveObject(
		ctx,
		store.bucket,
		sourceKey,
		minio.RemoveObjectOptions{},
	); err != nil {
		return fmt.Errorf("remove promoted S3 staging object: %w", err)
	}
	return nil
}

// Delete removes one exact object key and is provider-idempotent.
func (store *S3BlobStore) Delete(ctx context.Context, objectKey string) error {
	if err := ValidateObjectKey(objectKey); err != nil {
		return err
	}
	if err := store.client.RemoveObject(
		ctx,
		store.bucket,
		objectKey,
		minio.RemoveObjectOptions{},
	); err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}

// PresignGet creates a short-lived direct download.
func (store *S3BlobStore) PresignGet(
	ctx context.Context,
	objectKey string,
	ttl time.Duration,
	options GetObjectOptions,
) (SignedRequest, error) {
	if err := ValidateObjectKey(objectKey); err != nil {
		return SignedRequest{}, err
	}
	if err := validateSignedURLTTL(ttl); err != nil {
		return SignedRequest{}, err
	}
	responseHeaders := make(url.Values)
	if options.ContentDisposition != "" {
		responseHeaders.Set(
			"response-content-disposition",
			options.ContentDisposition,
		)
	}
	if options.ContentType != "" {
		responseHeaders.Set("response-content-type", options.ContentType)
	}
	signed, err := store.public.PresignedGetObject(
		ctx,
		store.bucket,
		objectKey,
		ttl,
		responseHeaders,
	)
	if err != nil {
		return SignedRequest{}, fmt.Errorf("sign S3 download: %w", err)
	}
	return SignedRequest{
		ExpiresAt: time.Now().UTC().Add(ttl),
		Headers:   map[string]string{},
		Method:    http.MethodGet,
		URL:       signed.String(),
	}, nil
}

func newMinIOCore(
	endpoint string,
	storageConfig S3BlobStoreConfig,
) (*minio.Core, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil ||
		parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		(parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("endpoint must be an HTTP(S) origin")
	}
	return minio.NewCore(parsed.Host, &minio.Options{
		Creds: credentials.NewStaticV4(
			storageConfig.AccessKey,
			storageConfig.SecretKey,
			"",
		),
		Region: storageConfig.Region,
		Secure: parsed.Scheme == "https",
	})
}

func validateProviderUpload(upload MultipartUpload) error {
	if err := ValidateObjectKey(upload.ObjectKey); err != nil {
		return err
	}
	if strings.TrimSpace(upload.ProviderUploadID) == "" ||
		len(upload.ProviderUploadID) > 2048 {
		return ErrUploadNotFound
	}
	return nil
}

func validateSignedURLTTL(ttl time.Duration) error {
	if ttl < time.Second || ttl > maximumSignedURLTTL {
		return fmt.Errorf("signed URL TTL must be between one second and seven days")
	}
	return nil
}

func mapS3UploadError(operation string, err error) error {
	if isS3Code(err, "NoSuchUpload") {
		return ErrUploadNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func isS3NotFound(err error) bool {
	return isS3Code(err, "NoSuchKey") ||
		isS3Code(err, "NoSuchObject") ||
		isS3Code(err, "NotFound")
}

func isS3Code(err error, expected string) bool {
	if err == nil {
		return false
	}
	response := minio.ToErrorResponse(err)
	return response.Code == expected
}
