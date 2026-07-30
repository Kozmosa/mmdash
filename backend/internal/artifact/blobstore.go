// Package artifact owns Artifact business state and its object-storage boundary.
package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

const (
	// MultipartMinPartBytes is the S3 minimum for every non-final part.
	MultipartMinPartBytes int64 = 5 * 1024 * 1024
	// MultipartMaxPartBytes is the S3 maximum size of a single part.
	MultipartMaxPartBytes int64 = 5 * 1024 * 1024 * 1024
	// MultipartMaxParts is the S3 maximum number of parts.
	MultipartMaxParts = 10000
	// MultipartMaxObjectBytes is the S3 maximum object size.
	MultipartMaxObjectBytes int64 = 5 * 1024 * 1024 * 1024 * 1024
	// mebibyte is the required part-size rounding boundary.
	mebibyte int64 = 1024 * 1024
)

var (
	// ErrDirectTransferUnsupported means bytes must use an Artifact transfer route.
	ErrDirectTransferUnsupported = errors.New("direct object-storage transfer is unsupported")
	// ErrInvalidObjectKey protects every adapter from traversal and ambiguous keys.
	ErrInvalidObjectKey = errors.New("invalid object key")
	// ErrInvalidPart identifies an invalid or conflicting multipart part.
	ErrInvalidPart = errors.New("invalid multipart part")
	// ErrObjectExists preserves immutable content-addressed objects.
	ErrObjectExists = errors.New("object already exists")
	// ErrObjectNotFound is returned when an object key has no stored bytes.
	ErrObjectNotFound = errors.New("object not found")
	// ErrUploadNotFound is returned when a multipart upload no longer exists.
	ErrUploadNotFound = errors.New("multipart upload not found")
)

// MultipartPlan is a bounded, S3-compatible upload partition.
type MultipartPlan struct {
	PartCount int
	PartBytes int64
	SizeBytes int64
}

// MultipartUpload is an internal provider handle. It must never leave Core.
type MultipartUpload struct {
	ObjectKey        string
	ProviderUploadID string
}

// CompletedPart is the provider-confirmed identity of one uploaded part.
type CompletedPart struct {
	ETag       string
	PartNumber int
	SizeBytes  int64
}

// ObjectInfo is the bounded metadata required for verification and downloads.
type ObjectInfo struct {
	ContentType  string
	ETag         string
	LastModified time.Time
	SizeBytes    int64
}

// SignedRequest is a short-lived direct object-storage request.
type SignedRequest struct {
	ExpiresAt time.Time
	Headers   map[string]string
	Method    string
	URL       string
}

// BlobStore is the multipart contract shared by Local, MinIO, and S3.
//
// Provider upload IDs and object keys stay inside Core. Local PutPart/Open
// calls are exposed only through signed streaming routes in the Artifact
// module; MinIO/S3 use short-lived direct requests.
type BlobStore interface {
	AbortMultipart(context.Context, MultipartUpload) error
	Backend() string
	Check(context.Context) error
	CompleteMultipart(context.Context, MultipartUpload, []CompletedPart) (ObjectInfo, error)
	CreateMultipart(context.Context, string, string) (MultipartUpload, error)
	Delete(context.Context, string) error
	ListParts(context.Context, MultipartUpload) ([]CompletedPart, error)
	Name() string
	Open(context.Context, string) (io.ReadCloser, error)
	PresignGet(context.Context, string, time.Duration) (SignedRequest, error)
	PresignPart(context.Context, MultipartUpload, int, time.Duration) (SignedRequest, error)
	Promote(context.Context, string, string) error
	PutPart(context.Context, MultipartUpload, int, io.Reader, int64) (CompletedPart, error)
	Stat(context.Context, string) (ObjectInfo, error)
}

// CalculateMultipartPlan selects a MiB-aligned part size without exceeding
// provider limits. Empty files still use one final zero-byte part.
func CalculateMultipartPlan(sizeBytes, configuredPartBytes, maxUploadBytes int64) (MultipartPlan, error) {
	if sizeBytes < 0 {
		return MultipartPlan{}, fmt.Errorf("size must not be negative")
	}
	if maxUploadBytes < 1 || maxUploadBytes > MultipartMaxObjectBytes {
		return MultipartPlan{}, fmt.Errorf("maximum upload size is invalid")
	}
	if sizeBytes > maxUploadBytes {
		return MultipartPlan{}, fmt.Errorf("upload exceeds configured maximum")
	}
	if configuredPartBytes < MultipartMinPartBytes ||
		configuredPartBytes > MultipartMaxPartBytes {
		return MultipartPlan{}, fmt.Errorf("configured part size is outside provider limits")
	}

	requiredPartBytes := divideRoundUp(sizeBytes, MultipartMaxParts)
	partBytes := roundUp(configuredPartBytes, mebibyte)
	if requiredPartBytes > partBytes {
		partBytes = roundUp(requiredPartBytes, mebibyte)
	}
	if partBytes > MultipartMaxPartBytes {
		return MultipartPlan{}, fmt.Errorf("upload cannot fit within multipart limits")
	}
	partCount := int(divideRoundUp(sizeBytes, partBytes))
	if partCount == 0 {
		partCount = 1
	}
	if partCount > MultipartMaxParts {
		return MultipartPlan{}, fmt.Errorf("upload requires too many parts")
	}
	return MultipartPlan{
		PartBytes: partBytes,
		PartCount: partCount,
		SizeBytes: sizeBytes,
	}, nil
}

// PartSize returns the exact byte count expected for one part.
func (plan MultipartPlan) PartSize(partNumber int) (int64, error) {
	if partNumber < 1 || partNumber > plan.PartCount {
		return 0, ErrInvalidPart
	}
	if partNumber < plan.PartCount {
		return plan.PartBytes, nil
	}
	remaining := plan.SizeBytes - int64(plan.PartCount-1)*plan.PartBytes
	if remaining < 0 {
		return 0, ErrInvalidPart
	}
	return remaining, nil
}

// ValidateObjectKey accepts only normalized, relative slash-separated keys.
func ValidateObjectKey(objectKey string) error {
	if objectKey == "" ||
		len(objectKey) > 1024 ||
		strings.ContainsRune(objectKey, '\x00') ||
		strings.Contains(objectKey, `\`) ||
		strings.HasPrefix(objectKey, "/") ||
		objectKey == ".." ||
		strings.HasPrefix(objectKey, "../") ||
		strings.Contains(objectKey, "/../") ||
		strings.HasSuffix(objectKey, "/..") ||
		path.Clean(objectKey) != objectKey ||
		objectKey == "." {
		return ErrInvalidObjectKey
	}
	return nil
}

func divideRoundUp(value, divisor int64) int64 {
	if value == 0 {
		return 0
	}
	return 1 + (value-1)/divisor
}

func roundUp(value, boundary int64) int64 {
	if value == 0 {
		return 0
	}
	return divideRoundUp(value, boundary) * boundary
}

func normalizeETag(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}
