package hermes

import (
	"context"
	"encoding/json"
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

func TestProbeMatchesHermesV2026_8_3Contract(t *testing.T) {
	if TestedVersion != "v2026.8.3" || TestedCommit != "3c27eb6234bf91b8ceee9e9071591b31e9b148cb" {
		t.Fatalf("unexpected upstream contract pin: %s %s", TestedVersion, TestedCommit)
	}
	var calls []string
	var mutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer runtime-secret" {
			t.Errorf("missing runtime bearer on %s", request.URL.Path)
		}
		mutex.Lock()
		calls = append(calls, request.Method+" "+request.URL.RequestURI())
		mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/p/research/health":
			writeJSON(t, response, map[string]any{"status": "ok", "platform": "hermes-agent", "version": "2026.8.3"})
		case "/p/research/health/detailed":
			writeJSON(t, response, map[string]any{"status": "ready", "readiness": map[string]any{"status": "ready"}})
		case "/p/research/v1/capabilities":
			writeJSON(t, response, authoritativeCapabilities())
		case "/p/research/api/sessions":
			if request.URL.Query().Get("limit") != "1" || request.URL.Query().Get("offset") != "0" {
				t.Errorf("unexpected session probe query: %s", request.URL.RawQuery)
			}
			writeJSON(t, response, map[string]any{"object": "list", "data": []any{}, "limit": 1, "offset": 0, "has_more": false})
		case "/p/research/api/jobs":
			if request.URL.Query().Get("include_disabled") != "true" {
				t.Error("jobs probe did not include disabled jobs")
			}
			writeJSON(t, response, map[string]any{"jobs": []any{}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	adapter := runtimeAdapterForServer(t, server.URL, "research")
	probe, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !probe.Healthy || !probe.Authenticated || probe.Platform != "hermes-agent" || probe.Version != "2026.8.3" || probe.Model != "hermes-4" {
		t.Fatalf("unexpected probe: %#v", probe)
	}
	if !probe.Capabilities.Sessions || !probe.Capabilities.Runs || !probe.Capabilities.RunStreaming || !probe.Capabilities.Jobs || probe.Capabilities.EventReplay {
		t.Fatalf("unexpected capabilities: %#v", probe.Capabilities)
	}
	if len(calls) != 5 {
		t.Fatalf("unexpected probe calls: %#v", calls)
	}
}

func TestRuntimeProbeDoesNotDependOnDashboardManagement(t *testing.T) {
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assertRuntimeAuth(t, request)
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/health":
			writeJSON(t, response, map[string]any{"status": "ok", "platform": "hermes-agent", "version": "2026.8.3"})
		case "/health/detailed":
			writeJSON(t, response, map[string]any{"status": "ready"})
		case "/v1/capabilities":
			writeJSON(t, response, authoritativeCapabilities())
		case "/api/sessions":
			writeJSON(t, response, map[string]any{"data": []any{}, "limit": 1, "offset": 0, "has_more": false})
		case "/api/jobs":
			writeJSON(t, response, map[string]any{"jobs": []any{}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer runtimeServer.Close()

	managementCalls := 0
	managementServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		managementCalls++
		http.Error(response, "dashboard unavailable", http.StatusServiceUnavailable)
	}))
	defer managementServer.Close()

	runtimePolicy := loopbackPolicy(t, runtimeServer.URL)
	managementPolicy := loopbackPolicy(t, managementServer.URL)
	adapter, err := New(Config{
		InstanceID: "instance", RuntimeURL: runtimeServer.URL, APIKey: "runtime-secret",
		RuntimePolicy: runtimePolicy, ManagementPolicy: managementPolicy,
		Management: &ManagementConfig{URL: managementServer.URL, DashboardSessionToken: "dashboard-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	probe, err := adapter.Probe(context.Background())
	if err != nil || !probe.Healthy || !probe.Authenticated {
		t.Fatalf("runtime probe depended on management: %#v %v", probe, err)
	}
	if !probe.Capabilities.ProjectAccess.Configure || !probe.Capabilities.ProjectAccess.Rotate {
		t.Fatalf("configured management capabilities missing: %#v", probe.Capabilities.ProjectAccess)
	}
	if managementCalls != 0 {
		t.Fatalf("runtime probe called management %d times", managementCalls)
	}
}

func TestCheckRuntimeExercisesLiveSessionRunSSEStatusStopAndCleanup(t *testing.T) {
	var calls []string
	var mu sync.Mutex
	var sessionID string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assertRuntimeAuth(t, request)
		mu.Lock()
		calls = append(calls, request.Method+" "+request.URL.Path)
		mu.Unlock()
		switch request.Method + " " + request.URL.Path {
		case "POST /api/sessions":
			body := decodeRequestMap(t, request)
			sessionID = stringValue(body["id"])
			if !strings.HasPrefix(sessionID, "mmdash_runtime_check_") ||
				body["source"] != runtimeCheckSource || body["title"] != runtimeCheckTitle {
				t.Fatalf("unexpected runtime-check session body: %#v", body)
			}
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, map[string]any{"session": map[string]any{"id": sessionID}})
		case "GET /api/sessions/" + sessionID:
			writeJSON(t, response, map[string]any{"session": map[string]any{"id": sessionID}})
		case "GET /api/sessions/" + sessionID + "/messages":
			writeJSON(t, response, map[string]any{"session_id": sessionID, "data": []any{}})
		case "POST /v1/runs":
			body := decodeRequestMap(t, request)
			if body["input"] != runtimeCheckInput || body["instructions"] != runtimeCheckPrompt ||
				body["session_id"] != sessionID {
				t.Fatalf("unexpected runtime-check run body: %#v", body)
			}
			response.WriteHeader(http.StatusAccepted)
			writeJSON(t, response, map[string]any{"run_id": "runtime-check-run", "status": "started"})
		case "POST /v1/runs/runtime-check-run/stop":
			body := decodeRequestMap(t, request)
			if len(body) != 0 {
				t.Fatalf("stop body was not empty: %#v", body)
			}
			writeJSON(t, response, map[string]any{"run_id": "runtime-check-run", "status": "stopping"})
		case "GET /v1/runs/runtime-check-run/events":
			response.Header().Set("Content-Type", "text/event-stream")
			_, _ = response.Write([]byte("event: run.cancelled\ndata: {\"event\":\"run.cancelled\",\"run_id\":\"runtime-check-run\"}\n\n: stream closed\n\n"))
		case "GET /v1/runs/runtime-check-run":
			writeJSON(t, response, map[string]any{"run_id": "runtime-check-run", "status": "cancelled"})
		case "DELETE /api/sessions/" + sessionID:
			writeJSON(t, response, map[string]any{"deleted": true})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	adapter := runtimeAdapterForServer(t, server.URL, "")
	if err := adapter.CheckRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{
		"POST /api/sessions",
		"GET /api/sessions/" + sessionID,
		"GET /api/sessions/" + sessionID + "/messages",
		"POST /v1/runs",
		"POST /v1/runs/runtime-check-run/stop",
		"GET /v1/runs/runtime-check-run/events",
		"GET /v1/runs/runtime-check-run",
		"DELETE /api/sessions/" + sessionID,
	}
	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	mu.Unlock()
	if !reflect.DeepEqual(gotCalls, wantCalls) {
		t.Fatalf("unexpected runtime check call sequence: got=%#v want=%#v", gotCalls, wantCalls)
	}
}

func TestCheckRuntimeCleansUpAfterFailureWithoutLeakingUpstreamBody(t *testing.T) {
	const secret = "runtime-check-upstream-secret"
	var deleted bool
	var sessionID string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assertRuntimeAuth(t, request)
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/sessions":
			body := decodeRequestMap(t, request)
			sessionID = stringValue(body["id"])
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, map[string]any{"session": map[string]any{"id": sessionID}})
		case request.Method == http.MethodGet && request.URL.Path == "/api/sessions/"+sessionID:
			writeJSON(t, response, map[string]any{"session": map[string]any{"id": sessionID}})
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/messages"):
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusBadRequest)
			writeJSON(t, response, map[string]any{"error": map[string]any{"message": secret}})
		case request.Method == http.MethodDelete:
			deleted = true
			writeJSON(t, response, map[string]any{"deleted": true})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	adapter := runtimeAdapterForServer(t, server.URL, "")
	err := adapter.CheckRuntime(context.Background())
	if err == nil || !deleted || strings.Contains(err.Error(), secret) {
		t.Fatalf("runtime failure/cleanup handling: err=%v deleted=%v", err, deleted)
	}
	var adapterErr *agent.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.Operation != "hermes.sessions.messages" {
		t.Fatalf("primary runtime error was not preserved: %#v", err)
	}
}

func TestCheckRuntimeReportsCleanupFailureWithoutMaskingPrimaryError(t *testing.T) {
	const secret = "cleanup-upstream-secret"
	var sessionID string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assertRuntimeAuth(t, request)
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/sessions":
			body := decodeRequestMap(t, request)
			sessionID = stringValue(body["id"])
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, map[string]any{"session": map[string]any{"id": sessionID}})
		case request.Method == http.MethodGet && request.URL.Path == "/api/sessions/"+sessionID:
			response.WriteHeader(http.StatusBadRequest)
			writeJSON(t, response, map[string]any{"error": map[string]any{"message": secret}})
		case request.Method == http.MethodDelete:
			response.WriteHeader(http.StatusBadGateway)
			writeJSON(t, response, map[string]any{"error": map[string]any{"message": secret}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	adapter := runtimeAdapterForServer(t, server.URL, "")
	err := adapter.CheckRuntime(context.Background())
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "temporary session cleanup failed") {
		t.Fatalf("cleanup failure was not safely observable: %v", err)
	}
	var adapterErr *agent.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.Operation != "hermes.sessions.get" {
		t.Fatalf("primary error was masked by cleanup failure: %#v", err)
	}
}

func TestCheckRuntimeCleanupRunsAfterParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sessionID string
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assertRuntimeAuth(t, request)
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/sessions":
			body := decodeRequestMap(t, request)
			sessionID = stringValue(body["id"])
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, map[string]any{"session": map[string]any{"id": sessionID}})
		case request.Method == http.MethodGet && request.URL.Path == "/api/sessions/"+sessionID:
			writeJSON(t, response, map[string]any{"session": map[string]any{"id": sessionID}})
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/messages"):
			writeJSON(t, response, map[string]any{"session_id": sessionID, "data": []any{}})
			if flusher, ok := response.(http.Flusher); ok {
				flusher.Flush()
			}
			cancel()
		case request.Method == http.MethodDelete:
			deleted = true
			writeJSON(t, response, map[string]any{"deleted": true})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	adapter := runtimeAdapterForServer(t, server.URL, "")
	if err := adapter.CheckRuntime(parent); err == nil {
		t.Fatal("runtime check unexpectedly passed after parent cancellation")
	}
	if !deleted {
		t.Fatal("temporary session cleanup did not run after parent/deep context cancellation")
	}
}

type runtimeCheckHarnessConfig struct {
	emptyCreate       bool
	wrongCreateID     string
	emptyGet          bool
	wrongGetID        string
	emptyMessages     bool
	messagesSessionID string
	stopStatus        string
	sseBody           string
	sseBodySet        bool
	status            string
	statusSet         bool
	statusID          string
	statusIDSet       bool
}

type runtimeCheckHarnessState struct {
	mu        sync.Mutex
	requested string
	deleted   bool
	calls     []string
}

func runtimeCheckHarness(t *testing.T, config runtimeCheckHarnessConfig) (*httptest.Server, *runtimeCheckHarnessState) {
	t.Helper()
	state := &runtimeCheckHarnessState{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assertRuntimeAuth(t, request)
		state.mu.Lock()
		state.calls = append(state.calls, request.Method+" "+request.URL.Path)
		state.mu.Unlock()

		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/sessions":
			body := decodeRequestMap(t, request)
			state.mu.Lock()
			state.requested = stringValue(body["id"])
			requested := state.requested
			state.mu.Unlock()
			id := requested
			if config.emptyCreate {
				id = ""
			} else if config.wrongCreateID != "" {
				id = config.wrongCreateID
			}
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, map[string]any{"session": map[string]any{"id": id}})
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/api/sessions/") && !strings.HasSuffix(request.URL.Path, "/messages"):
			state.mu.Lock()
			requested := state.requested
			state.mu.Unlock()
			id := requested
			if config.emptyGet {
				id = ""
			} else if config.wrongGetID != "" {
				id = config.wrongGetID
			}
			writeJSON(t, response, map[string]any{"session": map[string]any{"id": id}})
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/messages"):
			state.mu.Lock()
			requested := state.requested
			state.mu.Unlock()
			sessionID := requested
			if config.emptyMessages {
				sessionID = ""
			} else if config.messagesSessionID != "" {
				sessionID = config.messagesSessionID
			}
			writeJSON(t, response, map[string]any{"session_id": sessionID, "data": []any{}})
		case request.Method == http.MethodPost && request.URL.Path == "/v1/runs":
			response.WriteHeader(http.StatusAccepted)
			writeJSON(t, response, map[string]any{"run_id": "runtime-check-run", "status": "started"})
		case request.Method == http.MethodPost && request.URL.Path == "/v1/runs/runtime-check-run/stop":
			status := config.stopStatus
			if status == "" {
				status = "stopping"
			}
			writeJSON(t, response, map[string]any{"run_id": "runtime-check-run", "status": status})
		case request.Method == http.MethodGet && request.URL.Path == "/v1/runs/runtime-check-run/events":
			response.Header().Set("Content-Type", "text/event-stream")
			body := config.sseBody
			if !config.sseBodySet {
				body = "event: run.cancelled\ndata: {\"event\":\"run.cancelled\",\"run_id\":\"runtime-check-run\"}\n\n"
			}
			_, _ = response.Write([]byte(body))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/runs/runtime-check-run":
			status := config.status
			if !config.statusSet {
				status = "cancelled"
			}
			runID := "runtime-check-run"
			if config.statusIDSet {
				runID = config.statusID
			}
			writeJSON(t, response, map[string]any{"run_id": runID, "status": status})
		case request.Method == http.MethodDelete:
			state.mu.Lock()
			state.deleted = true
			state.mu.Unlock()
			writeJSON(t, response, map[string]any{"deleted": true})
		default:
			http.NotFound(response, request)
		}
	}))
	return server, state
}

func runtimeCheckHarnessResult(t *testing.T, config runtimeCheckHarnessConfig, operation string) {
	t.Helper()
	server, state := runtimeCheckHarness(t, config)
	defer server.Close()
	adapter := runtimeAdapterForServer(t, server.URL, "")
	err := adapter.CheckRuntime(context.Background())
	if err == nil {
		t.Fatalf("runtime check unexpectedly passed for %s", operation)
	}
	var adapterErr *agent.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.Operation != operation {
		t.Fatalf("unexpected %s failure: %#v", operation, err)
	}
	state.mu.Lock()
	deleted := state.deleted
	state.mu.Unlock()
	if !deleted {
		t.Fatalf("runtime-check temporary session was not cleaned after %s failure", operation)
	}
}

func TestCheckRuntimeRejectsSessionIdentityMismatchesAndCleansUp(t *testing.T) {
	tests := []struct {
		name      string
		config    runtimeCheckHarnessConfig
		operation string
	}{
		{name: "empty create id", config: runtimeCheckHarnessConfig{emptyCreate: true}, operation: "hermes.runtime_check.create"},
		{name: "wrong create id", config: runtimeCheckHarnessConfig{wrongCreateID: "other-session"}, operation: "hermes.runtime_check.create"},
		{name: "empty get id", config: runtimeCheckHarnessConfig{emptyGet: true}, operation: "hermes.runtime_check.get"},
		{name: "wrong get id", config: runtimeCheckHarnessConfig{wrongGetID: "other-session"}, operation: "hermes.runtime_check.get"},
		{name: "empty messages session id", config: runtimeCheckHarnessConfig{emptyMessages: true}, operation: "hermes.runtime_check.messages"},
		{name: "wrong messages session id", config: runtimeCheckHarnessConfig{messagesSessionID: "other-session"}, operation: "hermes.runtime_check.messages"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeCheckHarnessResult(t, test.config, test.operation)
		})
	}
}

func TestCheckRuntimeRejectsStopStatusAndCleansUp(t *testing.T) {
	runtimeCheckHarnessResult(t, runtimeCheckHarnessConfig{stopStatus: "completed"}, "hermes.runtime_check.stop")
}

func TestCheckRuntimeRequiresCancelledSSEAndCleansUp(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "started only", body: "event: run.started\ndata: {\"event\":\"run.started\",\"run_id\":\"runtime-check-run\"}\n\n"},
		{name: "wrong cancelled run", body: "event: run.cancelled\ndata: {\"event\":\"run.cancelled\",\"run_id\":\"other-run\"}\n\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeCheckHarnessResult(t, runtimeCheckHarnessConfig{sseBody: test.body, sseBodySet: true}, "hermes.runtime_check.events")
		})
	}
}

func TestCheckRuntimeRequiresCancelledFinalStatusAndCleansUp(t *testing.T) {
	tests := []struct {
		name   string
		config runtimeCheckHarnessConfig
	}{
		{name: "non-cancelled", config: runtimeCheckHarnessConfig{status: "completed", statusSet: true}},
		{name: "wrong run id", config: runtimeCheckHarnessConfig{statusID: "other-run", statusIDSet: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeCheckHarnessResult(t, test.config, "hermes.runtime_check.status")
		})
	}
}

func TestSessionMessageAndChatMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assertRuntimeAuth(t, request)
		response.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "GET /api/sessions":
			writeJSON(t, response, map[string]any{
				"data": []any{sessionFixture("session-main")}, "limit": 2, "offset": 1, "has_more": true,
			})
		case "POST /api/sessions":
			body := decodeRequestMap(t, request)
			if body["id"] != "session-new" || body["model"] != "hermes-4" || body["system_prompt"] != "project prompt" || body["source"] != "mmdash" || body["title"] != "Main" {
				t.Fatalf("unexpected create body: %#v", body)
			}
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, map[string]any{"object": "hermes.session", "session": sessionFixture("session-new")})
		case "GET /api/sessions/session-main":
			writeJSON(t, response, map[string]any{"session": sessionFixture("session-main")})
		case "PATCH /api/sessions/session-main":
			body := decodeRequestMap(t, request)
			if len(body) != 2 || body["title"] != "Renamed" || body["end_reason"] != "completed" {
				t.Fatalf("unexpected patch body: %#v", body)
			}
			writeJSON(t, response, map[string]any{"session": sessionFixture("session-main")})
		case "DELETE /api/sessions/session-main":
			writeJSON(t, response, map[string]any{"object": "hermes.session.deleted", "id": "session-main", "deleted": true})
		case "POST /api/sessions/session-main/fork":
			body := decodeRequestMap(t, request)
			if body["id"] != "session-fork" || body["title"] != "Fork" {
				t.Fatalf("unexpected fork body: %#v", body)
			}
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, map[string]any{"session": sessionFixture("session-fork")})
		case "GET /api/sessions/session-main/messages":
			writeJSON(t, response, map[string]any{"session_id": "session-resolved", "data": []any{
				map[string]any{"id": 3, "session_id": "session-main", "role": "assistant", "content": "safe answer", "reasoning": "private chain", "reasoning_content": "private chain 2", "tool_calls": []any{map[string]any{"id": "call-1", "function": map[string]any{"name": "data.read", "arguments": `{"secret":"do-not-leak"}`}}}, "timestamp": 1_754_000_000.5},
				map[string]any{"id": "m2", "session_id": "session-main", "role": "tool", "content": "sensitive tool result", "tool_name": "data.read", "tool_call_id": "call-1"},
			}})
		case "POST /api/sessions/session-main/chat":
			body := decodeRequestMap(t, request)
			if body["message"] != "continue" || body["instructions"] != "stay scoped" {
				t.Fatalf("unexpected chat body: %#v", body)
			}
			writeJSON(t, response, map[string]any{
				"object": "hermes.session.chat.completion", "session_id": "session-main",
				"message": map[string]any{"role": "assistant", "content": "reply"},
				"usage":   map[string]any{"input_tokens": 3, "output_tokens": 4, "total_tokens": 7},
				"runtime": map[string]any{"provider": "nous", "model": "hermes-4", "route_source": "profile", "model_lock": "confirmed", "api_key": "never-copy"},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	adapter := runtimeAdapterForServer(t, server.URL, "")
	ctx := context.Background()

	page, err := adapter.ListSessions(ctx, agent.SessionFilter{Page: agent.PageRequest{Limit: 2, Offset: 1}, Source: "api_server", IncludeChildren: true})
	if err != nil || len(page.Sessions) != 1 || page.Sessions[0].RemoteID != "session-main" || !page.HasMore {
		t.Fatalf("unexpected session page: %#v, %v", page, err)
	}
	if page.Sessions[0].StartedAt.IsZero() || page.Sessions[0].EndedAt == nil || page.Sessions[0].LastActiveAt == nil {
		t.Fatalf("timestamps not normalized: %#v", page.Sessions[0])
	}
	created, err := adapter.CreateSession(ctx, agent.CreateSessionRequest{RemoteID: "session-new", Model: "hermes-4", SystemPrompt: "project prompt", Source: "mmdash", Title: "Main"})
	if err != nil || created.RemoteID != "session-new" {
		t.Fatalf("create: %#v %v", created, err)
	}
	if _, err := adapter.GetSession(ctx, "session-main"); err != nil {
		t.Fatal(err)
	}
	title, reason := "Renamed", "completed"
	if _, err := adapter.UpdateSession(ctx, "session-main", agent.UpdateSessionRequest{Title: &title, EndReason: &reason}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.DeleteSession(ctx, "session-main"); err != nil {
		t.Fatal(err)
	}
	if fork, err := adapter.ForkSession(ctx, "session-main", agent.ForkSessionRequest{RemoteID: "session-fork", Title: "Fork"}); err != nil || fork.RemoteID != "session-fork" {
		t.Fatalf("fork: %#v %v", fork, err)
	}
	// Hermes v2026.8.3 ends the upstream parent with end_reason=branched as
	// part of this call. The adapter returns only the child; the Agent domain
	// synchronizes the local parent index in the same product operation.

	// Hermes may resolve a parent request to the active descendant while
	// returning the descendant session_id; the public adapter preserves the
	// normalized messages without rejecting that valid resolution.
	messages, err := adapter.ListMessages(ctx, "session-main")
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages: %#v %v", messages, err)
	}
	if messages[0].RemoteID != "3" || messages[0].Content != "safe answer" || len(messages[0].ToolCalls) != 1 || messages[0].ToolCalls[0].Name != "data.read" {
		t.Fatalf("unexpected safe assistant message: %#v", messages[0])
	}
	if messages[1].Content != "" {
		t.Fatalf("tool result leaked: %#v", messages[1])
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(messages[0].Content)), "private chain") || strings.Contains(strings.TrimSpace(messages[1].Content), "sensitive") {
		t.Fatal("private content leaked")
	}
	chat, err := adapter.Chat(ctx, "session-main", agent.ChatRequest{Message: "continue", Instructions: "stay scoped"})
	if err != nil || chat.Message.Content != "reply" || chat.Usage.TotalTokens != 7 || chat.Runtime.Model != "hermes-4" {
		t.Fatalf("chat: %#v %v", chat, err)
	}
}

func TestRunAndJobMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assertRuntimeAuth(t, request)
		response.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "POST /v1/runs":
			body := decodeRequestMap(t, request)
			if body["input"] != "analyze" || body["instructions"] != "project prompt" || body["session_id"] != "session-main" || body["model"] != "hermes-4" || body["provider"] != "nous" {
				t.Fatalf("unexpected run body: %#v", body)
			}
			response.WriteHeader(http.StatusAccepted)
			writeJSON(t, response, map[string]any{"run_id": "run-1", "status": "started"})
		case "GET /v1/runs/run-1":
			writeJSON(t, response, map[string]any{"object": "hermes.run", "run_id": "run-1", "session_id": "session-main", "status": "completed", "model": "hermes-4", "output": "answer", "usage": map[string]any{"input_tokens": 2, "output_tokens": 5, "total_tokens": 7}, "created_at": 1_754_000_000, "updated_at": 1_754_000_001})
		case "POST /v1/runs/run-1/approval":
			body := decodeRequestMap(t, request)
			if _, guessed := body["approval_id"]; guessed ||
				body["choice"] != string(agent.ApprovalSession) || body["resolve_all"] != true {
				t.Fatalf("approval: %#v", body)
			}
			writeJSON(t, response, map[string]any{"object": "hermes.run.approval_response", "run_id": "run-1", "choice": "session", "resolved": 2})
		case "POST /v1/runs/run-1/stop":
			writeJSON(t, response, map[string]any{"run_id": "run-1", "status": "stopping"})
		case "GET /api/jobs":
			writeJSON(t, response, map[string]any{"jobs": []any{jobFixture("job-1")}})
		case "POST /api/jobs":
			body := decodeRequestMap(t, request)
			if body["name"] != "progress" || body["schedule"] != "0 * * * *" || body["repeat"] != json.Number("3") {
				t.Fatalf("create job: %#v", body)
			}
			writeJSON(t, response, map[string]any{"job": jobFixture("job-1")})
		case "GET /api/jobs/job-1":
			writeJSON(t, response, map[string]any{"job": jobFixture("job-1")})
		case "PATCH /api/jobs/job-1":
			body := decodeRequestMap(t, request)
			if body["enabled"] != false || body["name"] != "renamed" {
				t.Fatalf("update job: %#v", body)
			}
			writeJSON(t, response, map[string]any{"job": jobFixture("job-1")})
		case "DELETE /api/jobs/job-1":
			writeJSON(t, response, map[string]any{"ok": true})
		case "POST /api/jobs/job-1/pause", "POST /api/jobs/job-1/resume", "POST /api/jobs/job-1/run":
			writeJSON(t, response, map[string]any{"job": jobFixture("job-1")})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	adapter := runtimeAdapterForServer(t, server.URL, "")
	ctx := context.Background()

	started, err := adapter.StartRun(ctx, agent.StartRunRequest{Input: "analyze", Instructions: "project prompt", SessionRemoteID: "session-main", ConversationHistory: []agent.ConversationMessage{{Role: "user", Content: "earlier"}}, Model: "hermes-4", Provider: "nous"})
	if err != nil || started.RemoteID != "run-1" || started.Status != agent.RunQueued {
		t.Fatalf("start: %#v %v", started, err)
	}
	run, err := adapter.GetRun(ctx, "run-1")
	if err != nil || run.Status != agent.RunCompleted || run.Output != "answer" || run.Usage.TotalTokens != 7 {
		t.Fatalf("get run: %#v %v", run, err)
	}
	approved, err := adapter.ApproveRun(ctx, "run-1", agent.ApprovalRequest{RemoteID: "approval-1", Choice: agent.ApprovalSession, ResolveAll: true})
	if err != nil || approved.RemoteID != "approval-1" || approved.Resolved != 2 {
		t.Fatalf("approve: %#v %v", approved, err)
	}
	stopped, err := adapter.StopRun(ctx, "run-1")
	if err != nil || stopped.Status != agent.RunStopping {
		t.Fatalf("stop: %#v %v", stopped, err)
	}

	jobs, err := adapter.ListJobs(ctx, true)
	if err != nil || len(jobs) != 1 || jobs[0].Schedule != "0 * * * *" || jobs[0].ScheduleDisplay != "0 * * * *" || !jobs[0].HasLastError || !jobs[0].HasDeliveryError || jobs[0].RepeatTimes != 3 || jobs[0].RepeatCompleted != 1 {
		t.Fatalf("jobs: %#v %v", jobs, err)
	}
	created, err := adapter.CreateJob(ctx, agent.CreateJobRequest{Name: "progress", Schedule: "0 * * * *", Prompt: "check", Deliver: "local", Skills: []string{"research"}, Repeat: 3})
	if err != nil || created.RemoteID != "job-1" {
		t.Fatalf("create job: %#v %v", created, err)
	}
	if _, err := adapter.GetJob(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	name, enabled := "renamed", false
	if _, err := adapter.UpdateJob(ctx, "job-1", agent.UpdateJobRequest{Name: &name, Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.PauseJob(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.ResumeJob(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.RunJob(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	if err := adapter.DeleteJob(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeErrorsNeverExposeUpstreamSecrets(t *testing.T) {
	const secret = "runtime-secret"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusBadRequest)
		writeJSON(t, response, map[string]any{"error": map[string]any{"code": secret, "message": "leaked " + secret}})
	}))
	defer server.Close()
	adapter := runtimeAdapterForServer(t, server.URL, "")
	_, err := adapter.GetSession(context.Background(), "session-main")
	var adapterError *agent.AdapterError
	if !errors.As(err, &adapterError) || strings.Contains(err.Error(), secret) || strings.Contains(adapterError.Message, secret) {
		t.Fatalf("unsafe error: %#v", err)
	}
}

func authoritativeCapabilities() map[string]any {
	return map[string]any{
		"object": "hermes.api_server.capabilities", "platform": "hermes-agent", "model": "hermes-4",
		"features": map[string]any{
			"session_resources": true, "session_fork": true, "session_chat": true,
			"session_chat_streaming": true, "run_submission": true, "run_status": true,
			"run_events_sse": true, "run_stop": true, "run_approval_response": true,
			"tool_progress_events": true,
			// Hermes 0.20.0 publishes transport metadata beside boolean flags.
			"session_continuity_header": "X-Hermes-Session-Id",
			"session_key_header":        "X-Hermes-Session-Key",
		},
		"endpoints": map[string]any{
			"sessions":            map[string]any{"method": "GET", "path": "/api/sessions"},
			"session_create":      map[string]any{"method": "POST", "path": "/api/sessions"},
			"session":             map[string]any{"method": "GET", "path": "/api/sessions/{session_id}"},
			"session_update":      map[string]any{"method": "PATCH", "path": "/api/sessions/{session_id}"},
			"session_delete":      map[string]any{"method": "DELETE", "path": "/api/sessions/{session_id}"},
			"session_messages":    map[string]any{"method": "GET", "path": "/api/sessions/{session_id}/messages"},
			"session_fork":        map[string]any{"method": "POST", "path": "/api/sessions/{session_id}/fork"},
			"session_chat":        map[string]any{"method": "POST", "path": "/api/sessions/{session_id}/chat"},
			"session_chat_stream": map[string]any{"method": "POST", "path": "/api/sessions/{session_id}/chat/stream"},
			"runs":                map[string]any{"method": "POST", "path": "/v1/runs"},
			"run_status":          map[string]any{"method": "GET", "path": "/v1/runs/{run_id}"},
			"run_events":          map[string]any{"method": "GET", "path": "/v1/runs/{run_id}/events"},
			"run_approval":        map[string]any{"method": "POST", "path": "/v1/runs/{run_id}/approval"},
			"run_stop":            map[string]any{"method": "POST", "path": "/v1/runs/{run_id}/stop"},
		},
	}
}

func TestValidateCapabilityEndpointsRequiresExactMethodAndPath(t *testing.T) {
	valid := map[string]capabilityEndpoint{
		"sessions":            {Method: http.MethodGet, Path: "/api/sessions"},
		"session_create":      {Method: http.MethodPost, Path: "/api/sessions"},
		"session":             {Method: http.MethodGet, Path: "/api/sessions/{session_id}"},
		"session_update":      {Method: http.MethodPatch, Path: "/api/sessions/{session_id}"},
		"session_delete":      {Method: http.MethodDelete, Path: "/api/sessions/{session_id}"},
		"session_messages":    {Method: http.MethodGet, Path: "/api/sessions/{session_id}/messages"},
		"session_fork":        {Method: http.MethodPost, Path: "/api/sessions/{session_id}/fork"},
		"session_chat":        {Method: http.MethodPost, Path: "/api/sessions/{session_id}/chat"},
		"session_chat_stream": {Method: http.MethodPost, Path: "/api/sessions/{session_id}/chat/stream"},
		"runs":                {Method: http.MethodPost, Path: "/v1/runs"},
		"run_status":          {Method: http.MethodGet, Path: "/v1/runs/{run_id}"},
		"run_events":          {Method: http.MethodGet, Path: "/v1/runs/{run_id}/events"},
		"run_approval":        {Method: http.MethodPost, Path: "/v1/runs/{run_id}/approval"},
		"run_stop":            {Method: http.MethodPost, Path: "/v1/runs/{run_id}/stop"},
	}
	if err := validateCapabilityEndpoints(valid); err != nil {
		t.Fatalf("valid capability endpoints rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(map[string]capabilityEndpoint)
	}{
		{name: "missing endpoint", mutate: func(endpoints map[string]capabilityEndpoint) {
			delete(endpoints, "session_create")
		}},
		{name: "wrong method", mutate: func(endpoints map[string]capabilityEndpoint) {
			endpoints["session_update"] = capabilityEndpoint{Method: http.MethodPut, Path: "/api/sessions/{session_id}"}
		}},
		{name: "wrong path", mutate: func(endpoints map[string]capabilityEndpoint) {
			endpoints["run_approval"] = capabilityEndpoint{Method: http.MethodPost, Path: "/v1/runs/{run_id}/approvals"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpoints := make(map[string]capabilityEndpoint, len(valid))
			for name, endpoint := range valid {
				endpoints[name] = endpoint
			}
			test.mutate(endpoints)
			err := validateCapabilityEndpoints(endpoints)
			var adapterErr *agent.AdapterError
			if !errors.As(err, &adapterErr) || adapterErr.Code != agent.ErrorUnsupported || adapterErr.Operation != "hermes.capabilities" || adapterErr.Message != "required Hermes endpoint is unavailable" {
				t.Fatalf("unexpected capability validation error: %#v", err)
			}
		})
	}
}

func sessionFixture(id string) map[string]any {
	return map[string]any{
		"id": id, "source": "api_server", "model": "hermes-4", "title": "Main",
		"started_at": 1_754_000_000.25, "ended_at": "2026-08-03T10:00:00Z", "end_reason": "completed",
		"message_count": 2, "tool_call_count": 1, "input_tokens": 3, "output_tokens": 4,
		"cache_read_tokens": 1, "cache_write_tokens": 1, "reasoning_tokens": 2,
		"estimated_cost_usd": 0.1, "actual_cost_usd": 0.2, "api_call_count": 1,
		"parent_session_id": "", "last_active": "1754000001.5", "preview": "hello",
		"has_system_prompt": true, "has_model_config": true,
	}
}

func jobFixture(id string) map[string]any {
	return map[string]any{
		"id": id, "name": "progress", "prompt": "check", "skills": []any{"research"},
		"schedule": map[string]any{"kind": "cron", "expr": "0 * * * *", "display": "0 * * * *"}, "schedule_display": "0 * * * *",
		"repeat": map[string]any{"times": 3, "completed": 1}, "enabled": true, "state": "scheduled",
		"created_at": "2026-08-03T10:00:00Z", "next_run_at": 1_754_000_000, "last_run_at": 1_753_000_000,
		"last_status": "failed", "last_error": "secret provider failure", "last_delivery_error": "secret delivery failure",
		"deliver": "local", "origin": "api_server",
	}
}

func TestNormalizedScheduleSupportsHermesAndLegacyShapes(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "hermes cron", value: map[string]any{"kind": "cron", "expr": "0 * * * *", "display": "0 * * * *"}, want: "0 * * * *"},
		{name: "hermes display fallback", value: map[string]any{"kind": "interval", "display": "every 30m"}, want: "every 30m"},
		{name: "legacy expression", value: map[string]any{"type": "cron", "expression": "*/5 * * * *"}, want: "*/5 * * * *"},
		{name: "legacy string", value: "every 2h", want: "every 2h"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizedSchedule(test.value); got != test.want {
				t.Fatalf("normalizedSchedule(%#v) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func runtimeAdapterForServer(t *testing.T, rawURL, profile string) *Adapter {
	t.Helper()
	policy := loopbackPolicy(t, rawURL)
	adapter, err := New(Config{InstanceID: "instance-1", RuntimeURL: rawURL, APIKey: "runtime-secret", Profile: profile, RuntimePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func assertRuntimeAuth(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Header.Get("Authorization") != "Bearer runtime-secret" {
		t.Fatalf("missing runtime auth: %#v", request.Header)
	}
}

func decodeRequestMap(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	var result map[string]any
	decoder := json.NewDecoder(request.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func writeJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(response).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizedJobDoesNotExposeErrorText(t *testing.T) {
	job := normalizedJob(jobFixture("job-1"))
	if !job.HasLastError || !job.HasDeliveryError {
		t.Fatalf("missing safe error flags: %#v", job)
	}
	if reflect.ValueOf(job).NumField() == 0 {
		t.Fatal("unexpected empty job")
	}
}

func TestAdapterFactoryCreatesDistinctInstances(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	policy := loopbackPolicy(t, server.URL)
	factory := NewFactory(FactoryOptions{RuntimePolicy: policy})
	first, err := factory(context.Background(), agent.AdapterConfig{InstanceID: "first", Values: map[string]string{ConfigRuntimeURL: server.URL, ConfigAPIKey: "one"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := factory(context.Background(), agent.AdapterConfig{InstanceID: "second", Values: map[string]string{ConfigRuntimeURL: server.URL, ConfigAPIKey: "two"}})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("factory reused adapter instance")
	}
	if first.(*Adapter).runtime == second.(*Adapter).runtime || first.(*Adapter).runtime.connector == second.(*Adapter).runtime.connector {
		t.Fatal("factory reused credential-bearing HTTP context")
	}
}

func TestParseTimeSupportsEpochAndRFC3339(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`1754000000.5`), json.RawMessage(`"2026-08-03T10:00:00Z"`)} {
		parsed, ok := parseTime(raw)
		if !ok || parsed.IsZero() {
			t.Fatalf("failed to parse %s", raw)
		}
	}
	if _, ok := parseTime(json.RawMessage(`null`)); ok {
		t.Fatal("null timestamp parsed")
	}
}

func TestRequestTimeoutHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	policy := loopbackPolicy(t, server.URL)
	policy.RequestTimeout = 20 * time.Millisecond
	adapter, err := New(Config{InstanceID: "i", RuntimeURL: server.URL, APIKey: "k", RuntimePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.GetSession(context.Background(), "session-main")
	var adapterError *agent.AdapterError
	if !errors.As(err, &adapterError) || adapterError.Code != agent.ErrorTimeout {
		t.Fatalf("expected timeout, got %#v", err)
	}
}

func TestApproveRunRejectsMultipleResolutionsWithoutResolveAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body := decodeRequestMap(t, request)
		if _, guessed := body["approval_id"]; guessed || body["choice"] != "once" {
			t.Fatalf("request diverged from pinned Hermes contract: %#v", body)
		}
		writeJSON(t, response, map[string]any{
			"object": "hermes.run.approval_response", "run_id": "run-1",
			"choice": "once", "resolved": 2,
		})
	}))
	defer server.Close()
	adapter := runtimeAdapterForServer(t, server.URL, "")
	_, err := adapter.ApproveRun(context.Background(), "run-1", agent.ApprovalRequest{
		RemoteID: "approval-1", Choice: agent.ApprovalOnce,
	})
	var adapterError *agent.AdapterError
	if !errors.As(err, &adapterError) || adapterError.Code != agent.ErrorProtocol {
		t.Fatalf("non-FIFO approval count was accepted: %#v", err)
	}
}
