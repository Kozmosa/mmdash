package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
)

const modelImportTimeout = 2 * time.Minute

// ModelFileImport is a Core-internal request to preserve one temporary Notion
// media URL as an immutable Artifact related to a Model question.
type ModelFileImport struct {
	ProjectID      string
	CreatedBy      string
	SourceObjectID string
	SourceBlockID  string
	URL            string
	Filename       string
	MIMEType       string
}

// ImportModelFile downloads a public HTTPS resource with SSRF protections,
// streams it through the normal verified multipart path, and deduplicates it
// by project, content hash, and source block.
func (service Service) ImportModelFile(ctx context.Context, input ModelFileImport) (Detail, error) {
	if service.Storage == nil || service.Store == nil || service.Generator == nil {
		return Detail{}, ErrNotAvailable
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	input.SourceObjectID = strings.TrimSpace(input.SourceObjectID)
	input.SourceBlockID = strings.TrimSpace(input.SourceBlockID)
	input.Filename = safeModelFilename(input.Filename)
	if input.ProjectID == "" || input.CreatedBy == "" || !uuidPattern.MatchString(input.SourceObjectID) || input.SourceBlockID == "" || input.Filename == "" {
		return Detail{}, ErrInvalid
	}
	temporary, sizeBytes, digest, contentType, err := service.fetchModelFile(ctx, input)
	if err != nil {
		return Detail{}, err
	}
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}()
	input.MIMEType = contentType
	plan, err := CalculateMultipartPlan(sizeBytes, service.MultipartPartBytes, service.MaxUploadBytes)
	if err != nil {
		return Detail{}, ErrTooLarge
	}
	idempotency := modelImportIdempotency(input.SourceObjectID, input.SourceBlockID, digest)
	if existing, err := service.Store.GetUploadByIdempotency(ctx, input.ProjectID, idempotency); err == nil {
		if existing.ExpectedSHA256 != digest || existing.ExpectedSize != sizeBytes || existing.CreatedBy != input.CreatedBy {
			return Detail{}, ErrUploadConflict
		}
		if existing.Status == UploadCompleted {
			return service.Store.GetDetail(ctx, input.ProjectID, existing.ArtifactID, false)
		}
		return service.finishModelUpload(ctx, input, temporary, existing, plan)
	} else if !errors.Is(err, ErrNotFound) {
		return Detail{}, err
	}

	artifactID, versionID, uploadID, err := service.newUploadIDs()
	if err != nil {
		return Detail{}, err
	}
	now := service.now()
	sourceObjectID := input.SourceObjectID
	description := "从 Notion 模型页面转存（block " + input.SourceBlockID + "）"
	artifact := Artifact{ID: artifactID, ProjectID: input.ProjectID, Kind: KindModelFile, Source: SourceModel, SourceObjectID: &sourceObjectID, Tags: []string{"模型文件"}, Name: input.Filename, Description: &description, RecommendedUsage: []string{}, CurrentVersionID: &versionID, Status: StatusPendingUpload, CreatedBy: input.CreatedBy, CreatedAt: now, UpdatedAt: now}
	version := Version{ID: versionID, ArtifactID: artifactID, ProjectID: input.ProjectID, VersionNo: 1, StorageClass: "object", Filename: input.Filename, SHA256: digest, MIMEType: input.MIMEType, SizeBytes: sizeBytes, Status: StatusPendingUpload, CreatedBy: input.CreatedBy, CreatedAt: now}
	upload, provider, err := service.prepareUpload(ctx, input.ProjectID, artifactID, versionID, uploadID, input.CreatedBy, input.Filename, input.MIMEType, digest, sizeBytes, idempotency, plan)
	if err != nil {
		return Detail{}, err
	}
	if upload.Status == UploadCompleted {
		artifact.Status, version.Status, version.BlobID, version.AvailableAt = StatusAvailable, StatusAvailable, strings.TrimPrefix(upload.ProviderUploadID, "deduplicated:"), upload.CompletedAt
	}
	if err := service.Store.CreateFirst(ctx, artifact, version, upload); err != nil {
		service.abortPrepared(ctx, provider)
		if errors.Is(err, ErrUploadConflict) {
			existing, findErr := service.Store.GetUploadByIdempotency(ctx, input.ProjectID, idempotency)
			if findErr == nil && existing.ExpectedSHA256 == digest && existing.ExpectedSize == sizeBytes {
				if existing.Status == UploadCompleted {
					return service.Store.GetDetail(ctx, input.ProjectID, existing.ArtifactID, false)
				}
				return service.finishModelUpload(ctx, input, temporary, existing, plan)
			}
		}
		return Detail{}, err
	}
	if upload.Status == UploadCompleted {
		return service.Store.GetDetail(ctx, input.ProjectID, artifactID, false)
	}
	return service.finishModelUpload(ctx, input, temporary, upload, plan)
}

func (service Service) finishModelUpload(ctx context.Context, input ModelFileImport, file *os.File, upload UploadSession, plan MultipartPlan) (Detail, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Detail{}, err
	}
	parts := make([]CompletedPart, 0, plan.PartCount)
	for partNumber := 1; partNumber <= plan.PartCount; partNumber++ {
		sizeBytes, err := plan.PartSize(partNumber)
		if err != nil {
			return Detail{}, err
		}
		part, err := service.Storage.PutPart(ctx, providerHandle(upload), partNumber, io.LimitReader(file, sizeBytes), sizeBytes)
		if err != nil {
			return Detail{}, service.storageError(err)
		}
		parts = append(parts, part)
	}
	now := service.now()
	if err := service.Store.UpsertParts(ctx, upload.ID, completedToUploadParts(parts, now)); err != nil {
		return Detail{}, err
	}
	if err := service.Store.MarkUploading(ctx, upload.ID, now); err != nil {
		return Detail{}, err
	}
	submitted := make([]ConfirmPart, 0, len(parts))
	for _, part := range parts {
		submitted = append(submitted, ConfirmPart{PartNumber: part.PartNumber, ETag: normalizeETag(part.ETag)})
	}
	detail, _, err := service.Confirm(ctx, auth.Identity{Kind: "session", User: auth.User{ID: input.CreatedBy}}, input.ProjectID, upload.ID, submitted)
	return detail, err
}

func (service Service) fetchModelFile(ctx context.Context, input ModelFileImport) (*os.File, int64, string, string, error) {
	parsed, err := validatePublicHTTPSURL(input.URL)
	if err != nil {
		return nil, 0, "", "", ErrInvalid
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, 0, "", "", ErrInvalid
	}
	if err := validatePublicModelHost(ctx, parsed.Hostname()); err != nil {
		return nil, 0, "", "", ErrInvalid
	}
	proxyURL, err := http.ProxyFromEnvironment(request)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("resolve model file proxy: %w", err)
	}
	client := safeModelHTTPClient(proxyURL)
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("download model file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, 0, "", "", fmt.Errorf("download model file: HTTP %d", response.StatusCode)
	}
	limit := service.MaxUploadBytes
	if limit < 1 {
		return nil, 0, "", "", ErrTooLarge
	}
	if response.ContentLength > limit {
		return nil, 0, "", "", ErrTooLarge
	}
	temporary, err := os.CreateTemp("", "mmdash-model-import-*")
	if err != nil {
		return nil, 0, "", "", err
	}
	failed := true
	defer func() {
		if failed {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
	}()
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, 0, "", "", err
	}
	if written > limit {
		return nil, 0, "", "", ErrTooLarge
	}
	contentType := strings.TrimSpace(input.MIMEType)
	if contentType == "" || strings.HasSuffix(contentType, "/*") || contentType == "application/octet-stream" {
		contentType = response.Header.Get("Content-Type")
	}
	if mediaType, _, parseErr := mime.ParseMediaType(contentType); parseErr == nil {
		contentType = mediaType
	}
	contentType, err = normalizeMIME(contentType)
	if err != nil {
		contentType = "application/octet-stream"
	}
	failed = false
	return temporary, written, hex.EncodeToString(digest.Sum(nil)), contentType, nil
}

func safeModelHTTPClient(proxyURL *url.URL) *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if modelProxyAddress(address, proxyURL) {
			return dialer.DialContext(ctx, network, address)
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			if unsafeModelIP(address.IP) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		}
		return nil, errors.New("model file host resolved only to private addresses")
	}}
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Timeout: modelImportTimeout, Transport: transport}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many model file redirects")
		}
		parsed, err := validatePublicHTTPSURL(request.URL.String())
		if err != nil {
			return err
		}
		return validatePublicModelHost(request.Context(), parsed.Hostname())
	}
	return client
}

func validatePublicModelHost(ctx context.Context, hostname string) error {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if !unsafeModelIP(address.IP) {
			return nil
		}
	}
	return errors.New("model file host resolved only to private addresses")
}

func modelProxyAddress(address string, proxyURL *url.URL) bool {
	if proxyURL == nil {
		return false
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || !strings.EqualFold(host, proxyURL.Hostname()) {
		return false
	}
	proxyPort := proxyURL.Port()
	if proxyPort == "" {
		if proxyURL.Scheme == "https" {
			proxyPort = "443"
		} else {
			proxyPort = "80"
		}
	}
	return port == proxyPort
}

func validatePublicHTTPSURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return nil, ErrInvalid
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return nil, ErrInvalid
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && unsafeModelIP(ip) {
		return nil, ErrInvalid
	}
	return parsed, nil
}

func unsafeModelIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

func safeModelFilename(raw string) string {
	value := strings.TrimSpace(filepath.Base(strings.ReplaceAll(raw, "\\", "/")))
	value = strings.Map(func(character rune) rune {
		if character < 32 || strings.ContainsRune(`<>:"/\\|?*`, character) {
			return '_'
		}
		return character
	}, value)
	if len([]rune(value)) > 255 {
		runes := []rune(value)
		value = string(runes[:255])
	}
	if !validFilename(value) {
		return "notion-file"
	}
	return value
}

func modelImportIdempotency(sourceObjectID, sourceBlockID, digest string) string {
	value := sha256.Sum256([]byte(sourceObjectID + "\x00" + sourceBlockID + "\x00" + digest))
	return "model-file-" + hex.EncodeToString(value[:])
}
