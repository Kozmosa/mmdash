package hermes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

func TestConfigureProjectAccessUsesDashboardMCPContract(t *testing.T) {
	state := newDashboardState()
	server := httptest.NewServer(state.handler(t, false, false))
	defer server.Close()
	adapter := managedAdapterForServer(t, server.URL)
	request := agent.ProjectAccessRequest{
		BindingID: "project-123", Endpoint: "https://mcp.example.test/mcp",
		Credential: "agent-token-plaintext-once", ExpectedTools: []string{"data.read", "project.get"},
	}
	result, err := adapter.ConfigureProjectAccess(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	expectedID := versionedServerName(request.BindingID, request.Credential)
	if !result.Verified || result.State != agent.ProjectAccessReady || result.Route != agent.AccessRouteDirect || result.RemoteID != expectedID || !reflect.DeepEqual(result.Tools, []string{"data.read", "project.get"}) {
		t.Fatalf("unexpected access result: %#v", result)
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	wantSequence := []string{"health", "list", "list", "create:" + expectedID, "test:" + expectedID, "restart", "test:" + expectedID}
	if !reflect.DeepEqual(state.calls, wantSequence) {
		t.Fatalf("unexpected management sequence:\n got %#v\nwant %#v", state.calls, wantSequence)
	}
	if state.lastBearer != request.Credential || state.lastEndpoint != request.Endpoint || state.lastProfile != "research" {
		t.Fatalf("incorrect MCP create mapping: %#v", state)
	}
	if state.sawCloudflareHeaders {
		t.Fatal("Cloudflare headers were sent on a directly reachable management path")
	}
	for _, call := range state.rawPaths {
		if strings.Contains(call, "/api/env") {
			t.Fatalf("unsupported env API used: %s", call)
		}
	}
}

func TestRotateProjectAccessStagesOldCleanupUntilCoreFinalizes(t *testing.T) {
	state := newDashboardState()
	state.servers["mmdash-project-old"] = mcpServer{Name: "mmdash-project-old", Transport: "http", URL: "https://mcp.example.test/mcp", Auth: "header", Enabled: true}
	server := httptest.NewServer(state.handler(t, false, false))
	defer server.Close()
	adapter := managedAdapterForServer(t, server.URL)
	request := agent.ProjectAccessRequest{
		BindingID: "project", Endpoint: "https://mcp.example.test/mcp", Credential: "new-agent-token",
		ExpectedTools: []string{"project.get"}, CurrentRemoteID: "mmdash-project-old",
	}
	result, err := adapter.RotateProjectAccess(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	newID := versionedServerName(request.BindingID, request.Credential)
	if !result.Verified || result.RemoteID != newID || result.CleanupPending {
		t.Fatalf("unexpected result: %#v", result)
	}
	state.mutex.Lock()
	wantTail := []string{"create:" + newID, "test:" + newID, "restart", "test:" + newID}
	if len(state.calls) < len(wantTail) || !reflect.DeepEqual(state.calls[len(state.calls)-len(wantTail):], wantTail) {
		state.mutex.Unlock()
		t.Fatalf("unexpected staged rotation sequence: %#v", state.calls)
	}
	if _, exists := state.servers["mmdash-project-old"]; !exists {
		state.mutex.Unlock()
		t.Fatal("old server was deleted before Core activation")
	}
	state.mutex.Unlock()

	finalized, err := adapter.FinalizeProjectAccess(context.Background(), agent.ProjectAccessFinalizeRequest{
		ActiveRemoteID: newID, PreviousRemoteID: "mmdash-project-old",
	})
	if err != nil || finalized.CleanupPending {
		t.Fatalf("finalize activated access: %#v %v", finalized, err)
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if _, exists := state.servers["mmdash-project-old"]; exists {
		t.Fatal("old server still present after post-activation finalize")
	}
	wantFinalizeTail := []string{"health", "list", "delete:mmdash-project-old"}
	if len(state.calls) < len(wantFinalizeTail) ||
		!reflect.DeepEqual(state.calls[len(state.calls)-len(wantFinalizeTail):], wantFinalizeTail) {
		t.Fatalf("unexpected finalize sequence: %#v", state.calls)
	}
}

func TestFinalizeProjectAccessReportsCleanupPendingWithoutDeletingActiveServer(t *testing.T) {
	state := newDashboardState()
	state.failDelete = true
	state.servers["mmdash-project-old"] = mcpServer{Name: "mmdash-project-old", Transport: "http", URL: "https://mcp.example.test/mcp", Auth: "header", Enabled: true}
	state.servers["mmdash-project-new"] = mcpServer{Name: "mmdash-project-new", Transport: "http", URL: "https://mcp.example.test/mcp", Auth: "header", Enabled: true}
	server := httptest.NewServer(state.handler(t, false, false))
	defer server.Close()
	adapter := managedAdapterForServer(t, server.URL)

	result, err := adapter.FinalizeProjectAccess(context.Background(), agent.ProjectAccessFinalizeRequest{
		ActiveRemoteID: "mmdash-project-new", PreviousRemoteID: "mmdash-project-old",
	})
	var adapterError *agent.AdapterError
	if !errors.As(err, &adapterError) || !result.CleanupPending ||
		adapterError.Operation != "hermes.dashboard.mcp.delete" {
		t.Fatalf("unexpected cleanup failure: %#v %#v", result, err)
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if _, exists := state.servers["mmdash-project-old"]; !exists {
		t.Fatal("failed cleanup removed the previous server")
	}
	if _, exists := state.servers["mmdash-project-new"]; !exists {
		t.Fatal("failed cleanup touched the active server")
	}
}

func TestRotateFailurePreservesOldProjectAccess(t *testing.T) {
	state := newDashboardState()
	state.failRestart = true
	state.servers["mmdash-project-old"] = mcpServer{Name: "mmdash-project-old", Transport: "http", URL: "https://mcp.example.test/mcp", Auth: "header", Enabled: true}
	server := httptest.NewServer(state.handler(t, false, false))
	defer server.Close()
	adapter := managedAdapterForServer(t, server.URL)
	request := agent.ProjectAccessRequest{
		BindingID: "project", Endpoint: "https://mcp.example.test/mcp", Credential: "new-agent-token-super-secret",
		ExpectedTools: []string{"project.get"}, CurrentRemoteID: "mmdash-project-old",
	}
	result, err := adapter.RotateProjectAccess(context.Background(), request)
	if err == nil || result.Verified {
		t.Fatalf("expected rotation failure: %#v %v", result, err)
	}
	if strings.Contains(err.Error(), request.Credential) {
		t.Fatalf("credential leaked in error: %v", err)
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if _, exists := state.servers["mmdash-project-old"]; !exists {
		t.Fatal("old project access was deleted after failed rotation")
	}
	for _, call := range state.calls {
		if call == "delete:mmdash-project-old" {
			t.Fatalf("old server deletion attempted after failure: %#v", state.calls)
		}
	}
}

func TestDashboardAuthModesAndManualBehavior(t *testing.T) {
	t.Run("interactive dashboard auth is unsupported", func(t *testing.T) {
		state := newDashboardState()
		server := httptest.NewServer(state.handler(t, true, false))
		defer server.Close()
		adapter := managedAdapterForServer(t, server.URL)
		_, err := adapter.ConfigureProjectAccess(context.Background(), agent.ProjectAccessRequest{BindingID: "p", Endpoint: "https://mcp.example.test/mcp", Credential: "token"})
		var adapterError *agent.AdapterError
		if !errors.As(err, &adapterError) || adapterError.Code != agent.ErrorUnsupported {
			t.Fatalf("expected unsupported auth, got %#v", err)
		}
		state.mutex.Lock()
		defer state.mutex.Unlock()
		if !reflect.DeepEqual(state.calls, []string{"health"}) {
			t.Fatalf("protected management API should not be attempted: %#v", state.calls)
		}
	})

	t.Run("cloudflare service auth is normalized as an authenticated proxy", func(t *testing.T) {
		state := newDashboardState()
		server := httptest.NewServer(state.handler(t, false, true))
		defer server.Close()
		adapter := managedAdapterForServer(t, server.URL)
		result, err := adapter.ConfigureProjectAccess(context.Background(), agent.ProjectAccessRequest{
			BindingID: "proxy-project", Endpoint: "https://mcp.example.test/mcp",
			Credential: "proxy-token", ExpectedTools: []string{"project.get"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Route != agent.AccessRouteAuthenticatedProxy || !result.Verified {
			t.Fatalf("unexpected proxy result: %#v", result)
		}
		state.mutex.Lock()
		defer state.mutex.Unlock()
		if len(state.calls) < 2 || state.calls[0] != "challenge" || state.calls[1] != "health" || !state.sawCloudflareHeaders {
			t.Fatalf("proxy detection did not retry with service auth: %#v", state.calls)
		}
	})

	t.Run("cloudflare challenge requires service credentials", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Server", "cloudflare")
			response.Header().Set("CF-Ray", "abc-SHA")
			response.Header().Set("Location", "https://team.cloudflareaccess.com/cdn-cgi/access/login")
			response.WriteHeader(http.StatusFound)
		}))
		defer server.Close()
		policy := loopbackPolicy(t, server.URL)
		adapter, err := New(Config{
			InstanceID: "instance", RuntimeURL: server.URL, APIKey: "runtime-secret", RuntimePolicy: policy,
			ManagementPolicy: policy, Management: &ManagementConfig{URL: server.URL, DashboardSessionToken: "dashboard-secret"},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = adapter.ConfigureProjectAccess(context.Background(), agent.ProjectAccessRequest{BindingID: "p", Endpoint: "https://mcp.example.test/mcp", Credential: "token"})
		var adapterError *agent.AdapterError
		if !errors.As(err, &adapterError) || adapterError.Code != agent.ErrorAuthentication || !strings.Contains(adapterError.Message, "Cloudflare Access") {
			t.Fatalf("unexpected challenge result: %#v", err)
		}
	})

	t.Run("manual mode does not mutate Hermes", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		defer server.Close()
		adapter := runtimeAdapterForServer(t, server.URL, "")
		verification, err := adapter.VerifyProjectAccess(context.Background(), agent.ProjectAccessRequest{})
		if err != nil || verification.State != agent.ProjectAccessPending || verification.Verified {
			t.Fatalf("unexpected manual verification: %#v %v", verification, err)
		}
		configured, err := adapter.ConfigureProjectAccess(context.Background(), agent.ProjectAccessRequest{BindingID: "p", Endpoint: "https://mcp.example.test/mcp", Credential: "token"})
		var adapterError *agent.AdapterError
		if !errors.As(err, &adapterError) || configured.State != agent.ProjectAccessUnsupported {
			t.Fatalf("unexpected manual configure: %#v %#v", configured, err)
		}
	})
}

func TestManagementRateLimitHonorsContextCancellationAndReturnsPermit(t *testing.T) {
	management := newManagementClient(nil, "default", ManagementConfig{}, time.Hour)
	if err := management.acquire(context.Background()); err != nil {
		t.Fatalf("acquire first permit: %v", err)
	}
	management.release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := management.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled rate-limit wait, got %v", err)
	}

	// A cancelled waiter must return the serialization permit. Reset the
	// interval so this assertion remains deterministic and fast.
	management.minimumInterval = 0
	management.nextOperationAt = time.Time{}
	if err := management.acquire(context.Background()); err != nil {
		t.Fatalf("acquire permit after cancellation: %v", err)
	}
	management.release()
}

type dashboardState struct {
	mutex                sync.Mutex
	calls                []string
	rawPaths             []string
	servers              map[string]mcpServer
	lastBearer           string
	lastEndpoint         string
	lastProfile          string
	failRestart          bool
	failDelete           bool
	sawCloudflareHeaders bool
}

func newDashboardState() *dashboardState {
	return &dashboardState{servers: map[string]mcpServer{}}
}

func (state *dashboardState) handler(t *testing.T, authRequired, requireProxy bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		state.mutex.Lock()
		defer state.mutex.Unlock()
		state.rawPaths = append(state.rawPaths, request.Method+" "+request.URL.RequestURI())
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/health" {
			if requireProxy && request.Header.Get("CF-Access-Client-Id") == "" {
				state.calls = append(state.calls, "challenge")
				response.Header().Set("Server", "cloudflare")
				response.Header().Set("CF-Ray", "proxy-SHA")
				response.Header().Set("Location", "https://team.cloudflareaccess.com/cdn-cgi/access/login")
				response.WriteHeader(http.StatusFound)
				return
			}
			state.calls = append(state.calls, "health")
			if request.Header.Get("CF-Access-Client-Id") != "" {
				state.sawCloudflareHeaders = true
			}
			writeJSON(t, response, map[string]any{"ok": true, "version": "2026.8.3", "auth_required": authRequired})
			return
		}
		if request.Header.Get("X-Hermes-Session-Token") != "dashboard-secret" {
			t.Errorf("missing dashboard session token on %s", request.URL.Path)
			response.WriteHeader(http.StatusUnauthorized)
			writeJSON(t, response, map[string]any{"detail": "Unauthorized"})
			return
		}
		if requireProxy {
			if request.Header.Get("CF-Access-Client-Id") != "cf-client" || request.Header.Get("CF-Access-Client-Secret") != "cf-secret" {
				t.Errorf("missing Cloudflare service headers on %s", request.URL.Path)
			}
			state.sawCloudflareHeaders = true
		} else if request.Header.Get("CF-Access-Client-Id") != "" || request.Header.Get("CF-Access-Client-Secret") != "" {
			state.sawCloudflareHeaders = true
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/mcp/servers":
			state.calls = append(state.calls, "list")
			state.lastProfile = request.URL.Query().Get("profile")
			servers := make([]mcpServer, 0, len(state.servers))
			for _, server := range state.servers {
				servers = append(servers, server)
			}
			writeJSON(t, response, map[string]any{"servers": servers})
		case request.Method == http.MethodPost && request.URL.Path == "/api/mcp/servers":
			body := decodeRequestMap(t, request)
			name, _ := body["name"].(string)
			endpoint, _ := body["url"].(string)
			bearer, _ := body["bearer_token"].(string)
			profile, _ := body["profile"].(string)
			if body["auth"] != "header" {
				t.Errorf("unexpected MCP auth: %#v", body)
			}
			state.calls = append(state.calls, "create:"+name)
			state.lastBearer, state.lastEndpoint, state.lastProfile = bearer, endpoint, profile
			server := mcpServer{Name: name, Transport: "http", URL: endpoint, Auth: "header", Enabled: true}
			state.servers[name] = server
			writeJSON(t, response, server)
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/test"):
			name := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/api/mcp/servers/"), "/test")
			state.calls = append(state.calls, "test:"+name)
			if _, exists := state.servers[name]; !exists {
				response.WriteHeader(http.StatusNotFound)
				writeJSON(t, response, map[string]any{"detail": "not found"})
				return
			}
			writeJSON(t, response, map[string]any{"ok": true, "tools": []any{map[string]any{"name": "project.get", "description": ""}, map[string]any{"name": "data.read", "description": ""}}, "prompts": 0, "resources": 0})
		case request.Method == http.MethodPost && request.URL.Path == "/api/gateway/restart":
			state.calls = append(state.calls, "restart")
			if state.failRestart {
				response.WriteHeader(http.StatusInternalServerError)
				writeJSON(t, response, map[string]any{"detail": "restart failed with token new-agent-token-super-secret"})
				return
			}
			writeJSON(t, response, map[string]any{"ok": true, "pid": 123, "name": "gateway-restart"})
		case request.Method == http.MethodDelete && strings.HasPrefix(request.URL.Path, "/api/mcp/servers/"):
			name := strings.TrimPrefix(request.URL.Path, "/api/mcp/servers/")
			state.calls = append(state.calls, "delete:"+name)
			if state.failDelete {
				response.WriteHeader(http.StatusInternalServerError)
				writeJSON(t, response, map[string]any{"detail": "delete failed"})
				return
			}
			delete(state.servers, name)
			writeJSON(t, response, map[string]any{"ok": true})
		default:
			http.NotFound(response, request)
		}
	})
}

func managedAdapterForServer(t *testing.T, rawURL string) *Adapter {
	t.Helper()
	policy := loopbackPolicy(t, rawURL)
	adapter, err := New(Config{
		InstanceID: "instance", RuntimeURL: rawURL, APIKey: "runtime-secret", Profile: "research", RuntimePolicy: policy,
		ManagementPolicy: policy,
		Management: &ManagementConfig{
			URL: rawURL, DashboardSessionToken: "dashboard-secret",
			CloudflareClientID: "cf-client", CloudflareClientSecret: "cf-secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}
