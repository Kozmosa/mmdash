package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	result, err := adapter.Send(context.Background(), map[string]interface{}{"endpoint": "https://example.test/hook", "signing_secret": "secret"}, "delivery-1", "progress.reminder.due", RenderedMessage{Body: body, ContentType: "application/json"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseSummary != "http_status=204" {
		t.Fatalf("safe response summary: %#v", result)
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

func TestWebhookReturnsOnlySafeStructuredProviderResults(t *testing.T) {
	tests := []struct {
		name        string
		adapter     ProviderAdapter
		config      map[string]interface{}
		body        string
		wantID      string
		wantSummary string
	}{
		{
			name: "generic allowlisted id only",
			adapter: GenericWebhook{Client: httpDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"provider_message_id":"generic-message-1","status":"accepted","token":"body-secret","email":"person@example.test","url":"https://user:password@example.test/callback?access_token=query-secret"}`)), Header: make(http.Header)}, nil
			})},
			config:      map[string]interface{}{"endpoint": "https://example.test/hook", "signing_secret": "signing-secret"},
			wantID:      "generic-message-1",
			wantSummary: "http_status=200",
		},
		{
			name: "feishu allowlisted response",
			adapter: FeishuWebhook{Client: httpDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","data":{"message_id":"feishu-message-1","authorization":"Bearer body-secret"},"cookie":"session=secret"}`)), Header: make(http.Header)}, nil
			})},
			config:      map[string]interface{}{"webhook_url": "https://example.test/feishu"},
			wantID:      "feishu-message-1",
			wantSummary: "http_status=200; code=0; msg=success",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.adapter.Send(context.Background(), test.config, "delivery-result", "test", RenderedMessage{Body: []byte("{}"), ContentType: "application/json"})
			if err != nil {
				t.Fatal(err)
			}
			if result.ProviderMessageID != test.wantID || result.ResponseSummary != test.wantSummary {
				t.Fatalf("provider result: got %#v, want id=%q summary=%q", result, test.wantID, test.wantSummary)
			}
			for _, forbidden := range []string{"body-secret", "query-secret", "person@example.test", "password"} {
				if strings.Contains(result.ProviderMessageID+result.ResponseSummary, forbidden) {
					t.Fatalf("provider result leaked %q: %#v", forbidden, result)
				}
			}
		})
	}
}

func TestWebhookResponseBoundariesRemainSafe(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "empty"},
		{name: "non json", body: "not-json Authorization: Bearer response-secret"},
		{name: "overlong", body: strings.Repeat("x", providerResponseBodyLimit+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := GenericWebhook{Client: httpDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
			})}
			result, err := adapter.Send(context.Background(), map[string]interface{}{"endpoint": "https://example.test/hook", "signing_secret": "secret"}, "delivery-boundary", "test", RenderedMessage{Body: []byte("{}"), ContentType: "application/json"})
			if err != nil {
				t.Fatal(err)
			}
			if result.ProviderMessageID != "" || result.ResponseSummary != "http_status=202" || strings.Contains(result.ResponseSummary, "response-secret") {
				t.Fatalf("unsafe boundary result: %#v", result)
			}
		})
	}

	longID := strings.Repeat("a", providerResultMaxRunes+25)
	result := summarizeProviderResponse(http.StatusOK, []byte(`{"request_id":"`+longID+`"}`), true, false)
	if len([]rune(result.ProviderMessageID)) != providerResultMaxRunes {
		t.Fatalf("provider id length: got %d, want %d", len([]rune(result.ProviderMessageID)), providerResultMaxRunes)
	}
	result = summarizeProviderResponse(http.StatusOK, []byte("{\"request_id\":\"bad\\u0000id\"}"), true, false)
	if result.ProviderMessageID != "" {
		t.Fatalf("control-bearing provider id was retained: %#v", result)
	}
}

func TestProviderResultSanitizerRedactsSecretsAndTruncatesRunes(t *testing.T) {
	result := sanitizeProviderResult(ProviderSendResult{
		ProviderMessageID: "message-1",
		ResponseSummary: "Authorization: Bearer authorization-secret token=token-secret " +
			"https://user:password@example.test/callback?access_token=query-secret#fragment " +
			"person@example.test\x00\n" + strings.Repeat("界", providerResultMaxRunes),
	})
	if result.ProviderMessageID != "message-1" || len([]rune(result.ResponseSummary)) != providerResultMaxRunes {
		t.Fatalf("sanitized result boundary: %#v", result)
	}
	for _, forbidden := range []string{"authorization-secret", "token-secret", "query-secret", "password", "fragment", "person@example.test", "\x00", "\n"} {
		if strings.Contains(result.ResponseSummary, forbidden) {
			t.Fatalf("sanitized summary leaked %q: %q", forbidden, result.ResponseSummary)
		}
	}
	if !strings.Contains(result.ResponseSummary, "authorization=[REDACTED]") || !strings.Contains(result.ResponseSummary, "https://example.test") {
		t.Fatalf("sanitized summary lost safe diagnostics: %q", result.ResponseSummary)
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
			_, err := adapter.Send(context.Background(), map[string]interface{}{"endpoint": "https://example.test/hook", "signing_secret": "secret"}, "delivery-1", "test", RenderedMessage{Body: []byte("{}"), ContentType: "application/json"})
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
	_, err := adapter.Send(context.Background(), map[string]interface{}{"endpoint": "https://example.test/hook", "signing_secret": "secret"}, "delivery-1", "test", RenderedMessage{Body: []byte("{}"), ContentType: "application/json"})
	providerErr, ok := err.(ProviderError)
	if !ok || !providerErr.Retryable || providerErr.Code != "network_error" {
		t.Fatalf("expected retryable timeout, got %T %v", err, err)
	}
	if err := adapter.ValidateConfig(map[string]interface{}{"endpoint": "https://user:password@example.test/hook", "signing_secret": "secret"}); err == nil {
		t.Fatal("webhook URL with embedded credentials was accepted")
	}
}

func TestWebhookURLPolicyRequiresHTTPSExceptExplicitLoopbackDevelopment(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		local bool
		want  bool
	}{
		{name: "https", url: "https://example.test/hook?provider=allowed", want: true},
		{name: "production http loopback", url: "http://127.0.0.1:8080/hook"},
		{name: "local localhost", url: "http://localhost:8080/hook", local: true, want: true},
		{name: "local localhost subdomain", url: "http://receiver.localhost/hook", local: true, want: true},
		{name: "local ipv4 loopback", url: "http://127.0.0.2:8080/hook", local: true, want: true},
		{name: "local ipv6 loopback", url: "http://[::1]:8080/hook", local: true, want: true},
		{name: "local public host", url: "http://example.test/hook", local: true},
		{name: "deceptive localhost", url: "http://localhost.example.test/hook", local: true},
		{name: "embedded credentials", url: "https://user:credential@example.test/hook", local: true},
		{name: "fragment", url: "https://example.test/hook#credential", local: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secret := "signing-credential-that-must-not-leak"
			adapter := GenericWebhook{AllowHTTPLoopback: test.local}
			err := adapter.ValidateConfig(map[string]interface{}{"endpoint": test.url, "signing_secret": secret})
			if (err == nil) != test.want {
				t.Fatalf("ValidateConfig(%q) error=%v, want valid=%t", test.url, err, test.want)
			}
			if err != nil && (strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "credential")) {
				t.Fatalf("validation error leaked credentials: %v", err)
			}
		})
	}

	if err := (FeishuWebhook{AllowHTTPLoopback: true}).ValidateConfig(map[string]interface{}{"webhook_url": "http://localhost:8080/feishu"}); err != nil {
		t.Fatalf("explicit local Feishu loopback URL was rejected: %v", err)
	}
}

func TestWebhookSendRejectsInsecureConfigurationBeforeRequest(t *testing.T) {
	requested := false
	credential := "delivery-secret-that-must-not-leak"
	adapter := GenericWebhook{Client: httpDoerFunc(func(*http.Request) (*http.Response, error) {
		requested = true
		return nil, nil
	})}
	_, err := adapter.Send(context.Background(), map[string]interface{}{
		"endpoint": "http://127.0.0.1:8080/hook", "signing_secret": credential,
	}, "delivery-1", "test", RenderedMessage{Body: []byte("{}"), ContentType: "application/json"})
	providerErr, ok := err.(ProviderError)
	if !ok || providerErr.Code != "configuration_error" || requested {
		t.Fatalf("insecure delivery result: requested=%t err=%T %#v", requested, err, err)
	}
	if strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("delivery validation leaked configuration: %v", err)
	}
}

func TestWebhookClientDoesNotFollowRedirects(t *testing.T) {
	for _, test := range []struct {
		name  string
		local bool
		tls   bool
	}{
		{name: "https", tls: true},
		{name: "explicit local http", local: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var targetRequests atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				targetRequests.Add(1)
			}))
			defer target.Close()

			redirect := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				http.Redirect(response, request, target.URL, http.StatusTemporaryRedirect)
			})
			var source *httptest.Server
			if test.tls {
				source = httptest.NewTLSServer(redirect)
			} else {
				source = httptest.NewServer(redirect)
			}
			defer source.Close()

			adapter := GenericWebhook{
				AllowHTTPLoopback: test.local,
				Client:            NewWebhookHTTPClient(source.Client()),
			}
			_, err := adapter.Send(context.Background(), map[string]interface{}{
				"endpoint": source.URL, "signing_secret": "redirect-signing-secret",
			}, "delivery-redirect", "test", RenderedMessage{Body: []byte(`{"secret":"payload"}`), ContentType: "application/json"})
			providerErr, ok := err.(ProviderError)
			if !ok || providerErr.Code != "provider_rejected" || providerErr.StatusCode != http.StatusTemporaryRedirect {
				t.Fatalf("redirect result: got %T %#v", err, err)
			}
			if targetRequests.Load() != 0 {
				t.Fatal("Webhook payload or signature was forwarded to the redirect target")
			}
		})
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
	if _, err := adapter.Send(context.Background(), map[string]interface{}{"endpoint": "https://example.test/hook", "signing_secret": "secret"}, "delivery-1", "test", RenderedMessage{Body: body, ContentType: "application/json"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, copyBody) {
		t.Fatal("adapter mutated rendered body")
	}
}
