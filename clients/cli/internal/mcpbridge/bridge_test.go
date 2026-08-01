package mcpbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type staticTokens struct {
	token  string
	forced int
}

func (tokens *staticTokens) AccessToken(_ context.Context, force bool) (string, error) {
	if force {
		tokens.forced++
	}
	return tokens.token, nil
}

func TestBridgeInjectsSelectedProjectAndPreservesProtocolStdout(t *testing.T) {
	var forwarded map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer delegated-token" {
			t.Fatalf("missing delegated token")
		}
		if err := json.NewDecoder(request.Body).Decode(&forwarded); err != nil {
			t.Fatal(err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	defer server.Close()
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"data.list","arguments":{}}}` + "\n"
	var stdout bytes.Buffer
	bridge := Bridge{CurrentProjectID: "project-1", Endpoint: server.URL, Stdin: strings.NewReader(input), Stdout: &stdout, Stderr: &bytes.Buffer{}, Tokens: &staticTokens{token: "delegated-token"}}
	if err := bridge.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	params := forwarded["params"].(map[string]interface{})
	arguments := params["arguments"].(map[string]interface{})
	if arguments["project_id"] != "project-1" {
		t.Fatalf("project was not injected: %#v", forwarded)
	}
	if strings.Count(stdout.String(), "\n") != 1 || !strings.Contains(stdout.String(), `"jsonrpc":"2.0"`) {
		t.Fatalf("stdout contains non-protocol output: %q", stdout.String())
	}
}

func TestBridgeRejectsAmbiguousProjectWithoutCallingGateway(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	input := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"data.read","arguments":{}}}` + "\n"
	var stdout bytes.Buffer
	bridge := Bridge{Endpoint: server.URL, Stdin: strings.NewReader(input), Stdout: &stdout, Stderr: &bytes.Buffer{}, Tokens: &staticTokens{token: "token"}}
	if err := bridge.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("gateway was called without explicit project context")
	}
	if !strings.Contains(stdout.String(), "PROJECT_CONTEXT_REQUIRED") {
		t.Fatalf("missing stable error: %s", stdout.String())
	}
}

func TestBridgeRefreshesOnceAfterUnauthorized(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":{"tools":[]}}`))
	}))
	defer server.Close()
	tokens := &staticTokens{token: "token"}
	var stdout bytes.Buffer
	bridge := Bridge{Endpoint: server.URL, Stdin: strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}` + "\n"), Stdout: &stdout, Stderr: &bytes.Buffer{}, Tokens: tokens}
	if err := bridge.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || tokens.forced != 1 {
		t.Fatalf("unexpected refresh behavior: requests=%d forced=%d", requests, tokens.forced)
	}
}

func TestBridgeForwardsDiscoverySSEAndLogicalSession(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			response.Header().Set(sessionHeader, "gateway-session-1")
			response.Header().Set("Content-Type", "text/event-stream")
			_, _ = response.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":4,\"result\":{\"tools\":[{\"name\":\"future.tool\"}]}}\n\n"))
			return
		}
		if request.Header.Get(sessionHeader) != "gateway-session-1" {
			t.Fatalf("logical session header was not forwarded")
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":5,"result":{}}`))
	}))
	defer server.Close()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":5,"method":"ping"}`,
	}, "\n") + "\n"
	var stdout bytes.Buffer
	bridge := Bridge{Endpoint: server.URL, Stdin: strings.NewReader(input), Stdout: &stdout, Stderr: &bytes.Buffer{}, Tokens: &staticTokens{token: "token"}}
	if err := bridge.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || !strings.Contains(stdout.String(), `"name":"future.tool"`) || strings.Count(stdout.String(), "\n") != 2 {
		t.Fatalf("unexpected transparent discovery output: requests=%d stdout=%q", requests, stdout.String())
	}
}

func TestBridgeRetriesDiscoveryButNeverBlindlyReplaysToolCalls(t *testing.T) {
	t.Run("discovery", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			requests++
			if requests == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":6,"result":{"tools":[]}}`))
		}))
		defer server.Close()
		var stdout bytes.Buffer
		bridge := Bridge{Endpoint: server.URL, Stdin: strings.NewReader(`{"jsonrpc":"2.0","id":6,"method":"tools/list"}` + "\n"), Stdout: &stdout, Stderr: &bytes.Buffer{}, Tokens: &staticTokens{token: "token"}}
		if err := bridge.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if requests != 2 || !strings.Contains(stdout.String(), `"tools":[]`) {
			t.Fatalf("unexpected discovery retry: requests=%d stdout=%q", requests, stdout.String())
		}
	})

	t.Run("tool call", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			requests++
			response.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()
		var stdout bytes.Buffer
		input := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"data.read","arguments":{}}}` + "\n"
		bridge := Bridge{CurrentProjectID: "project-1", Endpoint: server.URL, Stdin: strings.NewReader(input), Stdout: &stdout, Stderr: &bytes.Buffer{}, Tokens: &staticTokens{token: "token"}}
		if err := bridge.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if requests != 1 || !strings.Contains(stdout.String(), "REMOTE_MCP_UNAVAILABLE") {
			t.Fatalf("tool call replay boundary failed: requests=%d stdout=%q", requests, stdout.String())
		}
	})
}
