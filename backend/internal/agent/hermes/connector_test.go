package hermes

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

func TestConnectorRejectsUnsafeTargets(t *testing.T) {
	policy := NetworkPolicy{AllowedPorts: []int{80, 443, 8642, 9119}}
	tests := []string{
		"file:///etc/passwd",
		"https://user:secret@example.com",
		"https://example.com/path#fragment",
		"https://example.com:22",
		"http://[fe80::1%25eth0]:80",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			if _, err := newConnector(target, policy); err == nil {
				t.Fatal("expected target rejection")
			}
		})
	}
}

func TestConnectorRejectsUnencryptedPublicTargetsBeforeDial(t *testing.T) {
	policy := withResolver(t, NetworkPolicy{Dialer: rejectDialer{}}, staticResolver{{IP: net.ParseIP("93.184.216.34")}})
	roundTripper := &safeRoundTripper{policy: policy}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://runtime.example/health", nil)
	_, err := roundTripper.RoundTrip(request)
	var adapterError *agent.AdapterError
	if !errors.As(err, &adapterError) || adapterError.Code != agent.ErrorPermission {
		t.Fatalf("expected cleartext public target rejection, got %#v", err)
	}
}

func TestConnectorAddressPolicyAndDNSRevalidation(t *testing.T) {
	resolver := &sequenceResolver{answers: [][]net.IPAddr{
		{{IP: net.ParseIP("93.184.216.34")}},
		{{IP: net.ParseIP("10.0.0.2")}},
	}}
	policy, err := normalizePolicy(NetworkPolicy{Resolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	roundTripper := &safeRoundTripper{policy: policy}
	if _, err := roundTripper.resolve(context.Background(), "runtime.example"); err != nil {
		t.Fatalf("first public resolution failed: %v", err)
	}
	if _, err := roundTripper.resolve(context.Background(), "runtime.example"); err == nil {
		t.Fatal("expected rebound private address to be rejected")
	}

	mixed := &safeRoundTripper{policy: withResolver(t, NetworkPolicy{}, staticResolver{
		{IP: net.ParseIP("93.184.216.34")},
		{IP: net.ParseIP("192.168.1.2")},
	})}
	if _, err := mixed.resolve(context.Background(), "mixed.example"); err == nil {
		t.Fatal("expected mixed safe/unsafe answer rejection")
	}

	permissive := withResolver(t, NetworkPolicy{AllowLoopback: true, AllowPrivate: true}, staticResolver{})
	for _, address := range []string{"169.254.169.254", "fe80::1", "100.64.0.1", "0.0.0.0", "224.0.0.1"} {
		if err := validateIP(net.ParseIP(address), permissive); err == nil {
			t.Fatalf("expected sensitive address %s to be rejected", address)
		}
	}
	if err := validateIP(net.ParseIP("127.0.0.1"), permissive); err != nil {
		t.Fatalf("explicitly allowed loopback rejected: %v", err)
	}
	if err := validateIP(net.ParseIP("10.1.2.3"), permissive); err != nil {
		t.Fatalf("explicitly allowed private address rejected: %v", err)
	}
}

func TestConnectorRejectsCrossOriginRedirectWithoutLeakingCredentials(t *testing.T) {
	var targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		targetCalls.Add(1)
		if request.Header.Get("Authorization") != "" || request.Header.Get("X-Hermes-Session-Token") != "" {
			t.Error("credential reached redirect target")
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Redirect(response, &http.Request{}, target.URL, http.StatusFound)
	}))
	defer source.Close()

	connector, err := newConnector(source.URL, loopbackPolicy(t, source.URL, target.URL))
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, connector.endpoint("/start", nil).String(), nil)
	request.Header.Set("Authorization", "Bearer runtime-secret")
	request.Header.Set("X-Hermes-Session-Token", "dashboard-secret")
	if _, err := connector.client.Do(request); err == nil {
		t.Fatal("expected cross-origin redirect rejection")
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target received %d requests", targetCalls.Load())
	}
}

func TestConnectorPreservesEscapedBasePathAndEnforcesHeaderTimeout(t *testing.T) {
	receivedPath := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedPath <- request.URL.EscapedPath()
		if request.URL.Path == "/hermes api/slow" {
			time.Sleep(80 * time.Millisecond)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	policy := loopbackPolicy(t, server.URL)
	policy.ResponseHeaderTimeout = 20 * time.Millisecond
	connector, err := newConnector(server.URL+"/hermes%20api", policy)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, connector.endpoint("/slow", nil).String(), nil)
	_, err = connector.client.Do(request)
	var adapterError *agent.AdapterError
	if !errors.As(err, &adapterError) || adapterError.Code != agent.ErrorTimeout {
		t.Fatalf("expected timeout, got %#v", err)
	}
	path := <-receivedPath
	if path != "/hermes%20api/slow" {
		t.Fatalf("base path was double encoded: %q", path)
	}
}

func TestReadBoundedRejectsOversizedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"value":"0123456789"}`))
	}))
	defer server.Close()
	policy := loopbackPolicy(t, server.URL)
	policy.MaxResponseBytes = 8
	connector, err := newConnector(server.URL, policy)
	if err != nil {
		t.Fatal(err)
	}
	client := &apiClient{connector: connector}
	var result map[string]any
	err = client.doJSON(context.Background(), "test", http.MethodGet, "/", nil, nil, &result, http.StatusOK)
	var adapterError *agent.AdapterError
	if !errors.As(err, &adapterError) || adapterError.Code != agent.ErrorProtocol {
		t.Fatalf("expected response limit error, got %#v", err)
	}
}

type staticResolver []net.IPAddr

func (resolver staticResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return append([]net.IPAddr(nil), resolver...), nil
}

type sequenceResolver struct {
	answers [][]net.IPAddr
	index   atomic.Int64
}

type rejectDialer struct{}

func (rejectDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("dial should not be reached")
}

func (resolver *sequenceResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	index := int(resolver.index.Add(1) - 1)
	if index >= len(resolver.answers) {
		index = len(resolver.answers) - 1
	}
	return append([]net.IPAddr(nil), resolver.answers[index]...), nil
}

func withResolver(t *testing.T, policy NetworkPolicy, resolver Resolver) NetworkPolicy {
	t.Helper()
	policy.Resolver = resolver
	normalized, err := normalizePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	return normalized
}

func loopbackPolicy(t *testing.T, rawURLs ...string) NetworkPolicy {
	t.Helper()
	ports := make([]int, 0, len(rawURLs))
	for _, raw := range rawURLs {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(parsed.Port())
		if err != nil {
			t.Fatal(err)
		}
		ports = append(ports, port)
	}
	return NetworkPolicy{
		AllowLoopback:         true,
		AllowedPorts:          ports,
		ConnectTimeout:        time.Second,
		ResponseHeaderTimeout: time.Second,
		RequestTimeout:        time.Second,
	}
}
