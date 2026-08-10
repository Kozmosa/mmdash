package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

var (
	safeRemoteCode   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,80}$`)
	safePathID       = regexp.MustCompile(`^[A-Za-z0-9_.:@-]{1,512}$`)
	knownRemoteCodes = map[string]struct{}{
		"approval_not_active": {}, "approval_not_pending": {},
		"gateway_auth_failed": {}, "gateway_draining": {},
		"invalid_approval_choice": {}, "invalid_session_id": {},
		"invalid_system_message": {}, "invalid_system_prompt": {},
		"invalid_title": {}, "model_lock_persistence_failed": {},
		"run_not_found": {}, "session_db_unavailable": {},
		"session_exists": {}, "session_not_found": {},
		"unsupported_session_field": {},
	}
)

type apiClient struct {
	connector    *connector
	bearerToken  string
	profile      string
	extraHeaders http.Header
}

func (client *apiClient) doJSON(ctx context.Context, operation, method, path string, query url.Values, body, destination any, expected ...int) error {
	response, cancel, err := client.do(ctx, method, path, query, body, nil, true)
	if cancel != nil {
		defer cancel()
	}
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if !containsStatus(expected, response.StatusCode) {
		return decodeHTTPError(operation, response, client.connector.policy.MaxResponseBytes)
	}
	if destination == nil {
		_, err = readBounded(response.Body, client.connector.policy.MaxResponseBytes)
		return err
	}
	payload, err := readBounded(response.Body, client.connector.policy.MaxResponseBytes)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return &agent.AdapterError{Code: agent.ErrorProtocol, Operation: operation, Message: "invalid remote JSON response"}
	}
	return nil
}

// openStream omits a whole-request timeout. Connection and response-header
// timeouts remain enforced by the connector, while stream lifetime follows the
// caller's context.
func (client *apiClient) openStream(ctx context.Context, operation, method, path string, query url.Values, body any, headers http.Header) (*http.Response, error) {
	response, _, err := client.do(ctx, method, path, query, body, headers, false)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		return nil, decodeHTTPError(operation, response, client.connector.policy.MaxResponseBytes)
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if !strings.HasPrefix(contentType, "text/event-stream") {
		response.Body.Close()
		return nil, &agent.AdapterError{Code: agent.ErrorProtocol, Operation: operation, Message: "remote response is not an event stream"}
	}
	return response, nil
}

func (client *apiClient) do(ctx context.Context, method, path string, query url.Values, body any, headers http.Header, bounded bool) (*http.Response, context.CancelFunc, error) {
	requestContext := ctx
	cancel := context.CancelFunc(nil)
	if bounded {
		requestContext, cancel = context.WithTimeout(ctx, client.connector.policy.RequestTimeout)
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			if cancel != nil {
				cancel()
			}
			return nil, nil, &agent.AdapterError{Code: agent.ErrorInvalid, Operation: "request", Message: "request encoding failed"}
		}
		reader = bytes.NewReader(payload)
	}
	target := client.connector.endpoint(client.profilePath(path), query)
	request, err := http.NewRequestWithContext(requestContext, method, target.String(), reader)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, nil, &agent.AdapterError{Code: agent.ErrorInvalid, Operation: "request", Message: "request construction failed"}
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if client.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+client.bearerToken)
	}
	for key, values := range client.extraHeaders {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	for key, values := range headers {
		request.Header.Del(key)
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	response, err := client.connector.client.Do(request)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, nil, normalizeNetworkError("request", err)
	}
	return response, cancel, nil
}

func (client *apiClient) profilePath(path string) string {
	if client.profile == "" {
		return path
	}
	return "/p/" + url.PathEscape(client.profile) + "/" + strings.TrimLeft(path, "/")
}

func decodeHTTPError(operation string, response *http.Response, limit int64) error {
	code := remoteErrorCode(response.Body, limit)
	adapterError := &agent.AdapterError{
		Operation:  operation,
		StatusCode: response.StatusCode,
		Message:    "remote request rejected",
	}
	switch response.StatusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		adapterError.Code = agent.ErrorInvalid
	case http.StatusUnauthorized:
		adapterError.Code = agent.ErrorAuthentication
	case http.StatusForbidden:
		adapterError.Code = agent.ErrorPermission
	case http.StatusNotFound:
		adapterError.Code = agent.ErrorNotFound
	case http.StatusConflict:
		adapterError.Code = agent.ErrorConflict
	case http.StatusTooManyRequests:
		adapterError.Code = agent.ErrorRateLimited
		adapterError.Retryable = true
		adapterError.RetryAfter = retryAfter(response.Header.Get("Retry-After"), time.Now())
	default:
		if response.StatusCode >= 500 {
			adapterError.Code = agent.ErrorUnavailable
			adapterError.Retryable = true
		} else {
			adapterError.Code = agent.ErrorProtocol
		}
	}
	if code != "" {
		adapterError.Message = "remote error code: " + code
	}
	return adapterError
}

func remoteErrorCode(body io.Reader, limit int64) string {
	payload, err := readBounded(body, limit)
	if err != nil {
		return ""
	}
	var envelope struct {
		Error json.RawMessage `json:"error"`
		Code  string          `json:"code"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return ""
	}
	code := envelope.Code
	if len(envelope.Error) > 0 {
		var object struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(envelope.Error, &object) == nil && object.Code != "" {
			code = object.Code
		}
	}
	if !safeRemoteCode.MatchString(code) {
		return ""
	}
	if _, known := knownRemoteCodes[code]; !known {
		return ""
	}
	return code
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = 4 << 20
	}
	payload, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, normalizeNetworkError("read", err)
	}
	if int64(len(payload)) > limit {
		return nil, &agent.AdapterError{Code: agent.ErrorProtocol, Operation: "read", Message: "remote response exceeded size limit"}
	}
	return payload, nil
}

func containsStatus(statuses []int, status int) bool {
	for _, expected := range statuses {
		if expected == status {
			return true
		}
	}
	return false
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if parsed, err := http.ParseTime(value); err == nil && parsed.After(now) {
		return parsed.Sub(now)
	}
	return 0
}

func pathID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if !safePathID.MatchString(id) {
		return "", fmt.Errorf("%w: invalid remote ID", agent.ErrInvalidArgument)
	}
	return id, nil
}

func validateProfile(profile string) error {
	return agent.ValidateHermesProfile(profile)
}
