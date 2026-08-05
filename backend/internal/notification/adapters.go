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
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	Client  HTTPDoer
	Timeout time.Duration
	Clock   func() time.Time
}

func (adapter GenericWebhook) Key() string { return "notification.generic_webhook" }
func (adapter GenericWebhook) ValidateConfig(config map[string]interface{}) error {
	return validateWebhookConfig(config, true)
}
func (adapter GenericWebhook) Test(ctx context.Context, config map[string]interface{}) error {
	return adapter.send(ctx, config, "test", "mmdash.notification.test", RenderedMessage{Body: []byte(`{"event":"mmdash.notification.test","version":1}`), ContentType: "application/json"})
}
func (adapter GenericWebhook) Render(_ context.Context, notification Notification, _ int) (RenderedMessage, error) {
	body, err := json.Marshal(map[string]interface{}{"event": "mmdash.notification", "version": 1, "notification": notification})
	return RenderedMessage{Body: body, ContentType: "application/json"}, err
}
func (adapter GenericWebhook) Send(ctx context.Context, config map[string]interface{}, deliveryID, notificationType string, message RenderedMessage) error {
	return adapter.send(ctx, config, deliveryID, notificationType, message)
}
func (adapter GenericWebhook) send(ctx context.Context, config map[string]interface{}, deliveryID, notificationType string, message RenderedMessage) error {
	return sendWebhook(ctx, adapter.Client, adapter.Timeout, adapter.Clock, config, deliveryID, notificationType, message, true)
}

type FeishuWebhook struct {
	Client  HTTPDoer
	Timeout time.Duration
	Clock   func() time.Time
}

func (adapter FeishuWebhook) Key() string { return "notification.feishu_webhook" }
func (adapter FeishuWebhook) ValidateConfig(config map[string]interface{}) error {
	return validateWebhookConfig(config, false)
}
func (adapter FeishuWebhook) Test(ctx context.Context, config map[string]interface{}) error {
	return adapter.send(ctx, config, "test", "mmdash.notification.test", RenderedMessage{Body: []byte(`{"msg_type":"text","content":{"text":"mmdash notification test"}}`), ContentType: "application/json"})
}
func (adapter FeishuWebhook) Render(_ context.Context, notification Notification, _ int) (RenderedMessage, error) {
	text := notification.TypeKey
	if title, ok := notification.Data["title"].(string); ok && title != "" {
		text += ": " + title
	}
	body, err := json.Marshal(map[string]interface{}{"msg_type": "text", "content": map[string]string{"text": text}})
	return RenderedMessage{Body: body, ContentType: "application/json"}, err
}
func (adapter FeishuWebhook) Send(ctx context.Context, config map[string]interface{}, deliveryID, notificationType string, message RenderedMessage) error {
	return adapter.send(ctx, config, deliveryID, notificationType, message)
}
func (adapter FeishuWebhook) send(ctx context.Context, config map[string]interface{}, deliveryID, notificationType string, message RenderedMessage) error {
	return sendWebhook(ctx, adapter.Client, adapter.Timeout, adapter.Clock, config, deliveryID, notificationType, message, false)
}

func validateWebhookConfig(config map[string]interface{}, signed bool) error {
	endpoint, _ := config["endpoint"].(string)
	if endpoint == "" {
		endpoint, _ = config["webhook_url"].(string)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" && parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil {
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

func sendWebhook(ctx context.Context, client HTTPDoer, timeout time.Duration, clockFn func() time.Time, config map[string]interface{}, deliveryID, notificationType string, message RenderedMessage, signed bool) error {
	if client == nil {
		return ErrNotReady
	}
	if err := validateWebhookConfig(config, signed); err != nil {
		return ProviderError{Code: "configuration_error", Message: "invalid webhook configuration"}
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
		return ProviderError{Code: "configuration_error", Message: "invalid webhook endpoint"}
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
		return ProviderError{Code: "network_error", Message: "provider request failed", Retryable: true}
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if response.StatusCode == 429 || response.StatusCode >= 500 {
		return ProviderError{Code: "provider_retryable", Message: "provider temporarily unavailable", StatusCode: response.StatusCode, RetryAfter: retryAfter(response.Header.Get("Retry-After"), clockFn()), Retryable: true}
	}
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return ProviderError{Code: "provider_authentication", Message: "provider authentication failed", StatusCode: response.StatusCode}
	}
	return ProviderError{Code: "provider_rejected", Message: "provider rejected notification", StatusCode: response.StatusCode}
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
