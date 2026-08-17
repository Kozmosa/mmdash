package boxcontrol

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const sourceTransferTTL = 15 * time.Minute

var ErrSourceTransferExpired = errors.New("Box source transfer expired")
var commitSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

type SourceTransfer struct {
	URL          string    `json:"url"`
	ExpiresAt    time.Time `json:"expires_at"`
	SourceCommit string    `json:"source_commit"`
}

type sourceTransferClaims struct {
	BoxID          string `json:"box_id"`
	TaskID         string `json:"task_id"`
	ProjectID      string `json:"project_id"`
	ExecutionEpoch string `json:"execution_epoch"`
	SourceCommit   string `json:"source_commit"`
	ExpiresAt      int64  `json:"expires_at"`
}

type SourceTransferSigner struct {
	publicURL string
	secret    []byte
}

func NewSourceTransferSigner(secret, publicURL string) (*SourceTransferSigner, error) {
	if len(secret) < 32 {
		return nil, errors.New("Box source transfer signing secret is too short")
	}
	parsed, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("Box source transfer public URL is invalid")
	}
	digest := sha256.Sum256([]byte("mmdash-box-source-transfer-v1:" + secret))
	return &SourceTransferSigner{publicURL: strings.TrimRight(parsed.String(), "/"), secret: digest[:]}, nil
}

func (signer *SourceTransferSigner) Sign(task Task, sourceCommit string, now time.Time) (SourceTransfer, error) {
	if signer == nil || task.ID == "" || task.BoxID == "" || task.ProjectID == "" ||
		task.ExecutionEpoch == "" || !commitSHA256Pattern.MatchString(sourceCommit) {
		return SourceTransfer{}, ErrInvalid
	}
	expiresAt := now.UTC().Add(sourceTransferTTL)
	claims := sourceTransferClaims{
		BoxID: task.BoxID, TaskID: task.ID, ProjectID: task.ProjectID,
		ExecutionEpoch: task.ExecutionEpoch, SourceCommit: sourceCommit,
		ExpiresAt: expiresAt.Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return SourceTransfer{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, signer.secret)
	_, _ = mac.Write([]byte(encoded))
	token := encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return SourceTransfer{
		URL:       signer.publicURL + "/v1/box-source-transfers/" + url.PathEscape(token),
		ExpiresAt: expiresAt, SourceCommit: sourceCommit,
	}, nil
}

func (signer *SourceTransferSigner) Verify(token string, now time.Time) (sourceTransferClaims, error) {
	if signer == nil || token == "" || len(token) > 4096 {
		return sourceTransferClaims{}, ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return sourceTransferClaims{}, ErrInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return sourceTransferClaims{}, ErrInvalid
	}
	mac := hmac.New(sha256.New, signer.secret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return sourceTransferClaims{}, ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return sourceTransferClaims{}, ErrInvalid
	}
	var claims sourceTransferClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.BoxID == "" || claims.TaskID == "" ||
		claims.ProjectID == "" || claims.ExecutionEpoch == "" || !commitSHA256Pattern.MatchString(claims.SourceCommit) {
		return sourceTransferClaims{}, ErrInvalid
	}
	if claims.ExpiresAt <= now.UTC().Unix() {
		return sourceTransferClaims{}, ErrSourceTransferExpired
	}
	return claims, nil
}

type SourceArchiveWriter interface {
	WriteSourceArchive(context.Context, string, string, io.Writer) error
}

func (service Service) OpenSourceTransfer(ctx context.Context, token string, output io.Writer) error {
	if service.SourceSigner == nil || service.Sources == nil || service.Store == nil || output == nil {
		return ErrInvalid
	}
	claims, err := service.SourceSigner.Verify(token, service.now())
	if err != nil {
		return err
	}
	task, err := service.Store.GetTask(ctx, claims.TaskID)
	if err != nil {
		return err
	}
	commit, _ := stringRunSpecValue(task.RunSpec, "source_commit")
	if task.BoxID != claims.BoxID || task.ProjectID != claims.ProjectID ||
		task.ExecutionEpoch != claims.ExecutionEpoch || commit != claims.SourceCommit ||
		(task.Status != TaskPreparing && task.Status != TaskRunning) {
		return ErrNotFound
	}
	if err := service.Sources.WriteSourceArchive(ctx, claims.ProjectID, claims.SourceCommit, output); err != nil {
		return fmt.Errorf("write Box source archive: %w", err)
	}
	return nil
}

func sourceTransferToken(raw string) (string, error) {
	value, err := url.PathUnescape(strings.TrimSpace(raw))
	if err != nil || value == "" || strings.Contains(value, "/") {
		return "", ErrInvalid
	}
	return value, nil
}

func stringRunSpecValue(runSpec map[string]interface{}, key string) (string, bool) {
	value, ok := runSpec[key].(string)
	return strings.TrimSpace(value), ok
}
