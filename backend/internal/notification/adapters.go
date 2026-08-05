package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	providerResponseBodyLimit = 16 * 1024
	providerResultMaxRunes    = 500
)

var (
	providerURLPattern           = regexp.MustCompile(`(?i)https?://[^\s]+`)
	providerAuthorizationPattern = regexp.MustCompile(
		`(?i)\b(?:authorization|proxy-authorization)\b\s*[:=]\s*(?:bearer|basic)?\s*[^\s,;]+`,
	)
	providerSensitiveAssignmentPattern = regexp.MustCompile(
		`(?i)\b(?:cookie|set-cookie|token|access_token|refresh_token|id_token|api_key|apikey|secret|password|passwd|signature|sign)\b\s*[:=]\s*[^\s,;]+`,
	)
	providerBearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	providerJWTPattern    = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	providerEmailPattern  = regexp.MustCompile(`\b[A-Za-z0-9.!#$%&'*+/=?^_{|}~-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
)

type AdapterRegistry struct{ adapters map[string]ProviderAdapter }

func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{adapters: map[string]ProviderAdapter{}}
}
func (registry *AdapterRegistry) Register(adapter ProviderAdapter) error {
	if adapter == nil || adapter.Key() == "" {
		return ErrInvalid
	}
	if _, ok := registry.adapters[adapter.Key()]; ok {
		return fmt.Errorf("notification adapter already registered: %s", adapter.Key())
	}
	registry.adapters[adapter.Key()] = adapter
	return nil
}
func (registry *AdapterRegistry) Get(key string) (ProviderAdapter, bool) {
	adapter, ok := registry.adapters[key]
	return adapter, ok
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type WebhookConfig struct {
	URL     string
	Secret  string
	Enabled bool
}
type ProviderError struct {
	Code       string
	Message    string
	StatusCode int
	Retryable  bool
	RetryAfter time.Duration
}

func (err ProviderError) Error() string { return err.Code + ": " + err.Message }

type GenericWebhook struct {
	AllowHTTPLoopback bool
	Client            HTTPDoer
	Timeout           time.Duration
	Clock             func() time.Time
}

func (adapter GenericWebhook) Key() string { return "notification.generic_webhook" }
func (adapter GenericWebhook) ValidateConfig(config map[string]interface{}) error {
	return validateWebhookConfig(config, true, adapter.AllowHTTPLoopback)
}
func (adapter GenericWebhook) Test(ctx context.Context, config map[string]interface{}) error {
	_, err := adapter.send(ctx, config, "test", "mmdash.notification.test", RenderedMessage{Body: []byte(`{"event":"mmdash.notification.test","version":1}`), ContentType: "application/json"})
	return err
}
func (adapter GenericWebhook) Render(_ context.Context, notification Notification, _ int) (RenderedMessage, error) {
	body, err := json.Marshal(map[string]interface{}{"event": "mmdash.notification", "version": 1, "notification": notification})
	return RenderedMessage{Body: body, ContentType: "application/json"}, err
}
func (adapter GenericWebhook) Send(ctx context.Context, config map[string]interface{}, deliveryID, notificationType string, message RenderedMessage) (ProviderSendResult, error) {
	return adapter.send(ctx, config, deliveryID, notificationType, message)
}
func (adapter GenericWebhook) send(ctx context.Context, config map[string]interface{}, deliveryID, notificationType string, message RenderedMessage) (ProviderSendResult, error) {
	return sendWebhook(ctx, adapter.Client, adapter.Timeout, adapter.Clock, config, deliveryID, notificationType, message, true, adapter.AllowHTTPLoopback, false)
}

type FeishuWebhook struct {
	AllowHTTPLoopback bool
	Client            HTTPDoer
	Timeout           time.Duration
	Clock             func() time.Time
}

func (adapter FeishuWebhook) Key() string { return "notification.feishu_webhook" }
func (adapter FeishuWebhook) ValidateConfig(config map[string]interface{}) error {
	return validateWebhookConfig(config, false, adapter.AllowHTTPLoopback)
}
func (adapter FeishuWebhook) Test(ctx context.Context, config map[string]interface{}) error {
	_, err := adapter.send(ctx, config, "test", "mmdash.notification.test", RenderedMessage{Body: []byte(`{"msg_type":"text","content":{"text":"mmdash notification test"}}`), ContentType: "application/json"})
	return err
}
func (adapter FeishuWebhook) Render(_ context.Context, notification Notification, _ int) (RenderedMessage, error) {
	text := notification.TypeKey
	if title, ok := notification.Data["title"].(string); ok && title != "" {
		text += ": " + title
	}
	body, err := json.Marshal(map[string]interface{}{"msg_type": "text", "content": map[string]string{"text": text}})
	return RenderedMessage{Body: body, ContentType: "application/json"}, err
}
func (adapter FeishuWebhook) Send(ctx context.Context, config map[string]interface{}, deliveryID, notificationType string, message RenderedMessage) (ProviderSendResult, error) {
	return adapter.send(ctx, config, deliveryID, notificationType, message)
}
func (adapter FeishuWebhook) send(ctx context.Context, config map[string]interface{}, deliveryID, notificationType string, message RenderedMessage) (ProviderSendResult, error) {
	return sendWebhook(ctx, adapter.Client, adapter.Timeout, adapter.Clock, config, deliveryID, notificationType, message, false, adapter.AllowHTTPLoopback, true)
}

// NewWebhookHTTPClient returns an isolated client that never forwards a
// Webhook payload or signature across an HTTP redirect.
func NewWebhookHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	cloned := *client
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &cloned
}

func validateWebhookConfig(config map[string]interface{}, signed, allowHTTPLoopback bool) error {
	endpoint, _ := config["endpoint"].(string)
	if endpoint == "" {
		endpoint, _ = config["webhook_url"].(string)
	}
	if err := validateWebhookURL(endpoint, allowHTTPLoopback); err != nil {
		return ErrInvalid
	}
	if signed {
		secret, _ := config["signing_secret"].(string)
		if strings.TrimSpace(secret) == "" {
			return ErrInvalid
		}
	}
	return nil
}

func validateWebhookURL(endpoint string, allowHTTPLoopback bool) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return ErrInvalid
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "https" {
		return nil
	}
	if scheme != "http" || !allowHTTPLoopback || !explicitLoopbackHost(parsed.Hostname()) {
		return ErrInvalid
	}
	return nil
}

func explicitLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sendWebhook(ctx context.Context, client HTTPDoer, timeout time.Duration, clockFn func() time.Time, config map[string]interface{}, deliveryID, notificationType string, message RenderedMessage, signed, allowHTTPLoopback, feishu bool) (ProviderSendResult, error) {
	if client == nil {
		return ProviderSendResult{}, ErrNotReady
	}
	if err := validateWebhookConfig(config, signed, allowHTTPLoopback); err != nil {
		return ProviderSendResult{}, ProviderError{Code: "configuration_error", Message: "invalid webhook configuration"}
	}
	endpoint, _ := config["endpoint"].(string)
	if endpoint == "" {
		endpoint, _ = config["webhook_url"].(string)
	}
	secret, _ := config["signing_secret"].(string)
	if secret == "" {
		secret, _ = config["secret"].(string)
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if clockFn == nil {
		clockFn = time.Now
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(message.Body))
	if err != nil {
		return ProviderSendResult{}, ProviderError{Code: "configuration_error", Message: "invalid webhook endpoint"}
	}
	req.Header.Set("Content-Type", message.ContentType)
	req.Header.Set("X-Mmdash-Delivery-Id", deliveryID)
	req.Header.Set("X-Mmdash-Notification-Type", notificationType)
	for key, value := range message.Headers {
		req.Header.Set(key, value)
	}
	timestamp := strconv.FormatInt(clockFn().Unix(), 10)
	req.Header.Set("X-Mmdash-Timestamp", timestamp)
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(timestamp + "." + string(message.Body)))
		req.Header.Set("X-Mmdash-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	response, err := client.Do(req)
	if err != nil {
		return ProviderSendResult{}, ProviderError{Code: "network_error", Message: "provider request failed", Retryable: true}
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		body, complete := readProviderResponse(response.Body)
		return summarizeProviderResponse(response.StatusCode, body, complete, feishu), nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode == 429 || response.StatusCode >= 500 {
		return ProviderSendResult{}, ProviderError{Code: "provider_retryable", Message: "provider temporarily unavailable", StatusCode: response.StatusCode, RetryAfter: retryAfter(response.Header.Get("Retry-After"), clockFn()), Retryable: true}
	}
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return ProviderSendResult{}, ProviderError{Code: "provider_authentication", Message: "provider authentication failed", StatusCode: response.StatusCode}
	}
	return ProviderSendResult{}, ProviderError{Code: "provider_rejected", Message: "provider rejected notification", StatusCode: response.StatusCode}
}

func readProviderResponse(body io.Reader) ([]byte, bool) {
	if body == nil {
		return nil, true
	}
	value, err := io.ReadAll(io.LimitReader(body, providerResponseBodyLimit+1))
	if err != nil || len(value) > providerResponseBodyLimit {
		return nil, false
	}
	return value, true
}

func summarizeProviderResponse(status int, body []byte, complete, feishu bool) ProviderSendResult {
	result := ProviderSendResult{ResponseSummary: "http_status=" + strconv.Itoa(status)}
	if !complete || len(bytes.TrimSpace(body)) == 0 {
		return sanitizeProviderResult(result)
	}
	values := decodeProviderResponse(body)
	if values == nil {
		return sanitizeProviderResult(result)
	}
	result.ProviderMessageID = providerMessageID(values)
	if feishu {
		if code, ok := providerScalar(values["code"]); ok {
			result.ResponseSummary += "; code=" + code
		} else if code, ok = providerScalar(values["StatusCode"]); ok {
			result.ResponseSummary += "; code=" + code
		}
		if message := stableFeishuMessage(values); message != "" {
			result.ResponseSummary += "; msg=" + message
		}
	}
	return sanitizeProviderResult(result)
}

func decodeProviderResponse(body []byte) map[string]interface{} {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var values map[string]interface{}
	if err := decoder.Decode(&values); err != nil {
		return nil
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil
	}
	return values
}

func providerMessageID(values map[string]interface{}) string {
	if value := providerMessageIDFromMap(values); value != "" {
		return value
	}
	if data, ok := values["data"].(map[string]interface{}); ok {
		return providerMessageIDFromMap(data)
	}
	return ""
}

func providerMessageIDFromMap(values map[string]interface{}) string {
	for _, key := range []string{"provider_message_id", "message_id", "request_id"} {
		if value, ok := values[key].(string); ok {
			if safe := sanitizeProviderMessageID(value); safe != "" {
				return safe
			}
		}
	}
	return ""
}

func providerScalar(value interface{}) (string, bool) {
	switch typed := value.(type) {
	case json.Number:
		return typed.String(), true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(typed), true
	default:
		return "", false
	}
}

func stableFeishuMessage(values map[string]interface{}) string {
	for _, key := range []string{"msg", "StatusMessage"} {
		value, ok := values[key].(string)
		if !ok {
			continue
		}
		safe := strings.ToLower(sanitizeProviderText(value))
		switch safe {
		case "success", "ok", "failed", "failure", "error":
			return safe
		}
	}
	return ""
}

func sanitizeProviderResult(result ProviderSendResult) ProviderSendResult {
	result.ProviderMessageID = sanitizeProviderMessageID(result.ProviderMessageID)
	result.ResponseSummary = sanitizeProviderText(result.ResponseSummary)
	return result
}

func sanitizeProviderMessageID(value string) string {
	value = sanitizeProviderText(value)
	if value == "" || strings.Contains(value, "[REDACTED") || strings.Contains(value, "://") {
		return ""
	}
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || strings.ContainsRune("-_.:/", char) {
			continue
		}
		return ""
	}
	return value
}

func sanitizeProviderText(value string) string {
	value = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return ' '
		}
		return char
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	value = providerURLPattern.ReplaceAllStringFunc(value, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "[REDACTED_URL]"
		}
		return parsed.Scheme + "://" + parsed.Host
	})
	value = providerAuthorizationPattern.ReplaceAllString(value, "authorization=[REDACTED]")
	value = providerSensitiveAssignmentPattern.ReplaceAllString(value, "credential=[REDACTED]")
	value = providerBearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = providerJWTPattern.ReplaceAllString(value, "[REDACTED_TOKEN]")
	value = providerEmailPattern.ReplaceAllString(value, "[REDACTED_EMAIL]")
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > providerResultMaxRunes {
		runes = runes[:providerResultMaxRunes]
	}
	return string(runes)
}

func retryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil && when.After(now) {
		return when.Sub(now)
	}
	return 0
}
