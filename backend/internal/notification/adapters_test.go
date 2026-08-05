package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (fn httpDoerFunc) Do(request *http.Request) (*http.Response, error) { return fn(request) }

func TestGenericWebhookSignsRequestAndPassesNotificationType(t *testing.T) {
	body := []byte(`{"event":"test"}`)
	var request *http.Request
	adapter := GenericWebhook{
		Client: httpDoerFunc(func(got *http.Request) (*http.Response, error) {
			request = got
			return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		}),
		Clock: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	}
	if err := adapter.Send(context.Background(), map[string]interface{}{"endpoint": "https://example.test/hook", "signing_secret": "secret"}, "delivery-1", "progress.reminder.due", RenderedMessage{Body: body, ContentType: "application/json"}); err != nil {
		t.Fatal(err)
	}
	if request == nil || request.Header.Get("X-Mmdash-Notification-Type") != "progress.reminder.due" {
		t.Fatalf("notification type header missing: %#v", request)
	}
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte("1700000000." + string(body)))
	if got, want := request.Header.Get("X-Mmdash-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Fatalf("signature mismatch: got %s want %s", got, want)
	}
}

func TestWebhookClassifiesRetryAfterAndPermanentFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		retryable  bool
		wantDelay  time.Duration
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "7", retryable: true, wantDelay: 7 * time.Second},
		{name: "server", status: http.StatusBadGateway, retryable: true},
		{name: "bad request", status: http.StatusBadRequest, retryable: false},
		{name: "auth", status: http.StatusUnauthorized, retryable: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := GenericWebhook{Client: httpDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: http.Header{"Retry-After": []string{test.retryAfter}}, Body: io.NopCloser(strings.NewReader("provider secret response"))}, nil
			})}
			err := adapter.Send(context.Background(), map[string]interface{}{"endpoint": "https://example.test/hook", "signing_secret": "secret"}, "delivery-1", "test", RenderedMessage{Body: []byte("{}"), ContentType: "application/json"})
			providerErr, ok := err.(ProviderError)
			if !ok {
				t.Fatalf("expected ProviderError, got %T %v", err, err)
			}
			if providerErr.StatusCode != test.status || providerErr.Retryable != test.retryable || providerErr.Message == "provider secret response" {
				t.Fatalf("unsafe or incorrect provider error: %#v", providerErr)
			}
			if test.wantDelay != 0 && providerErr.RetryAfter != test.wantDelay {
				t.Fatalf("retry-after: got %s want %s", providerErr.RetryAfter, test.wantDelay)
			}
		})
	}
}

func TestWebhookTimeoutIsRetryableAndCredentialsInURLAreRejected(t *testing.T) {
	adapter := GenericWebhook{Timeout: 5 * time.Millisecond, Client: httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	err := adapter.Send(context.Background(), map[string]interface{}{"endpoint": "https://example.test/hook", "signing_secret": "secret"}, "delivery-1", "test", RenderedMessage{Body: []byte("{}"), ContentType: "application/json"})
	providerErr, ok := err.(ProviderError)
	if !ok || !providerErr.Retryable || providerErr.Code != "network_error" {
		t.Fatalf("expected retryable timeout, got %T %v", err, err)
	}
	if err := adapter.ValidateConfig(map[string]interface{}{"endpoint": "https://user:password@example.test/hook", "signing_secret": "secret"}); err == nil {
		t.Fatal("webhook URL with embedded credentials was accepted")
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	value := now.Add(3 * time.Second).Format(http.TimeFormat)
	if got := retryAfter(value, now); got != 3*time.Second {
		t.Fatalf("got %s", got)
	}
}

func TestRenderedMessageBodyIsNotMutated(t *testing.T) {
	body := []byte(`{"event":"test"}`)
	copyBody := append([]byte(nil), body...)
	adapter := GenericWebhook{Client: httpDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})}
	if err := adapter.Send(context.Background(), map[string]interface{}{"endpoint": "https://example.test/hook", "signing_secret": "secret"}, "delivery-1", "test", RenderedMessage{Body: body, ContentType: "application/json"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, copyBody) {
		t.Fatal("adapter mutated rendered body")
	}
}
