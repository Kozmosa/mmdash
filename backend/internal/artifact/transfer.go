package artifact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	transferUploadPart = "upload_part"
	transferDownload   = "download"
)

// TransferClaims identify application state only. Provider IDs, object keys,
// credentials, and user content never enter the token.
type TransferClaims struct {
	Kind        string `json:"kind"`
	ProjectID   string `json:"project_id"`
	UploadID    string `json:"upload_id,omitempty"`
	ArtifactID  string `json:"artifact_id,omitempty"`
	VersionID   string `json:"version_id,omitempty"`
	PreviewID   string `json:"preview_id,omitempty"`
	PreviewType string `json:"preview_type,omitempty"`
	JobID       string `json:"job_id,omitempty"`
	PartNumber  int    `json:"part_number,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	ExpiresAt   int64  `json:"expires_at"`
}

// TransferSigner creates and verifies short-lived Core streaming grants.
type TransferSigner struct {
	publicURL string
	secret    []byte
}

func NewTransferSigner(secret, publicURL string) (*TransferSigner, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("artifact transfer signing secret is too short")
	}
	parsed, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("artifact public URL is invalid")
	}
	sum := sha256.Sum256([]byte("mmdash-artifact-transfer-v1:" + secret))
	return &TransferSigner{
		publicURL: strings.TrimRight(parsed.String(), "/"),
		secret:    sum[:],
	}, nil
}

func (signer *TransferSigner) Sign(
	claims TransferClaims,
	now time.Time,
	ttl time.Duration,
) (TransferGrant, error) {
	if signer == nil || ttl <= 0 || !validTransferClaims(claims) {
		return TransferGrant{}, ErrInvalid
	}
	claims.ExpiresAt = now.Add(ttl).UTC().Unix()
	payload, err := json.Marshal(claims)
	if err != nil {
		return TransferGrant{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, signer.secret)
	_, _ = mac.Write([]byte(encoded))
	token := encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	method := "GET"
	if claims.Kind == transferUploadPart || claims.Kind == transferPreviewOutput {
		method = "PUT"
	}
	return TransferGrant{
		Method: method,
		URL: signer.publicURL + "/v1/artifact-transfers/" +
			url.PathEscape(token),
		Headers:   map[string]string{},
		ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(),
	}, nil
}

func (signer *TransferSigner) Verify(token string, now time.Time) (TransferClaims, error) {
	if signer == nil || len(token) == 0 || len(token) > 4096 {
		return TransferClaims{}, ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return TransferClaims{}, ErrInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return TransferClaims{}, ErrInvalid
	}
	mac := hmac.New(sha256.New, signer.secret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return TransferClaims{}, ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return TransferClaims{}, ErrInvalid
	}
	var claims TransferClaims
	if err := json.Unmarshal(payload, &claims); err != nil ||
		!validTransferClaims(claims) {
		return TransferClaims{}, ErrInvalid
	}
	if claims.ExpiresAt <= now.UTC().Unix() {
		return TransferClaims{}, ErrTransferExpired
	}
	return claims, nil
}

func validTransferClaims(claims TransferClaims) bool {
	if claims.ProjectID == "" || claims.SizeBytes < 0 {
		return false
	}
	switch claims.Kind {
	case transferUploadPart:
		return claims.UploadID != "" &&
			claims.PartNumber >= 1 &&
			claims.PartNumber <= MultipartMaxParts
	case transferDownload:
		return claims.ArtifactID != "" && claims.VersionID != ""
	case transferPreviewInput:
		return claims.ArtifactID != "" && claims.VersionID != "" &&
			claims.PreviewID != "" && claims.JobID != ""
	case transferPreviewOutput:
		return claims.ArtifactID != "" && claims.VersionID != "" &&
			claims.PreviewID != "" && claims.JobID != "" &&
			claims.PreviewType == PreviewThumbnail
	case transferPreviewDownload:
		return claims.ArtifactID != "" && claims.VersionID != "" &&
			claims.PreviewID != "" && claims.PreviewType == PreviewThumbnail
	default:
		return false
	}
}

func transferToken(rawPath string) (string, error) {
	value, err := url.PathUnescape(strings.TrimSpace(rawPath))
	if err != nil || value == "" || strings.Contains(value, "/") {
		return "", ErrInvalid
	}
	return value, nil
}

func contentDisposition(filename string) string {
	filename = strings.TrimSpace(filename)
	filename = strings.Map(func(value rune) rune {
		if value < 0x20 || value == 0x7f || value == '"' ||
			value == '\\' || value == '/' {
			return '_'
		}
		return value
	}, filename)
	if filename == "" {
		filename = "download"
	}
	encoded := url.QueryEscape(filename)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	return `attachment; filename="download"; filename*=UTF-8''` + encoded
}

func parseContentLength(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return -1, nil
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size < 0 {
		return 0, errors.New("invalid content length")
	}
	return size, nil
}
