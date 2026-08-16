package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExperimentClientUsesFrozenStage8Requests(t *testing.T) {
	type capturedRequest struct {
		method string
		path   string
		body   map[string]interface{}
	}
	requests := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body := map[string]interface{}{}
		if request.Body != nil {
			_ = json.NewDecoder(request.Body).Decode(&body)
		}
		requests = append(requests, capturedRequest{method: request.Method, path: request.URL.EscapedPath(), body: body})
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"experiment_id":"experiment-1","project_id":"project one","name":"test","experiment_type":"box","execution_status":"created","source_commit":"0123456789012345678901234567890123456789","entrypoint":"python:main.py","requested_runtime_policy":"auto","result_directory":"experiments/experiment-1_20260816_1200/","retry":{},"logs_truncated":false,"progress":0}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if _, err := client.CreateExperiment(context.Background(), "token", "project one", "box", "test", "0123456789012345678901234567890123456789", "python:main.py", "auto"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RunExperiment(context.Background(), "token", "project one", "experiment/one"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %#v", requests)
	}
	create := requests[0]
	if create.method != http.MethodPost || create.path != "/v1/projects/project%20one/experiments" || create.body["experiment_type"] != "box" || create.body["runtime_policy"] != "auto" {
		t.Fatalf("create request = %#v", create)
	}
	if _, legacyRuntime := create.body["runtime"]; legacyRuntime {
		t.Fatalf("legacy runtime field leaked: %#v", create.body)
	}
	run := requests[1]
	if run.path != "/v1/projects/project%20one/experiments/experiment%2Fone/run" || run.body["idempotency_key"] == "" {
		t.Fatalf("run request = %#v", run)
	}
}

func TestClientRetriesOnlySafeRequests(t *testing.T) {
	t.Run("identity read retries a transient failure", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			requests++
			if requests == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"kind":"session","user":{"id":"user-1"}}`))
		}))
		defer server.Close()

		if _, err := NewClient(server.URL).WhoAmI(context.Background(), "token"); err != nil {
			t.Fatal(err)
		}
		if requests != 2 {
			t.Fatalf("expected one bounded retry, got %d requests", requests)
		}
	})

	t.Run("device authorization is never blindly replayed", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			requests++
			response.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		if _, err := NewClient(server.URL).StartDeviceAuthorization(context.Background()); err == nil {
			t.Fatal("expected device authorization failure")
		}
		if requests != 1 {
			t.Fatalf("device authorization was replayed %d times", requests)
		}
	})
}

func TestModelClientUsesAuthorizedCoreRoutes(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer cli-token" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		requests = append(requests, request.Method+" "+request.URL.EscapedPath())
		response.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.EscapedPath() {
		case "GET /v1/projects/project%20one/models":
			_, _ = response.Write([]byte(`{"project_id":"project one","configured":true,"questions":[]}`))
		case "GET /v1/projects/project%20one/models/questions/question%2Fone":
			_, _ = response.Write([]byte(`{"question":{"question_id":"question/one"},"snapshots":[]}`))
		case "POST /v1/projects/project%20one/models/source/sync":
			response.WriteHeader(http.StatusAccepted)
			_, _ = response.Write([]byte(`{"sync_id":"sync-source","scope":"source","status":"queued","job_id":"job-source"}`))
		case "POST /v1/projects/project%20one/models/questions/question%2Fone/sync":
			response.WriteHeader(http.StatusAccepted)
			_, _ = response.Write([]byte(`{"sync_id":"sync-question","question_id":"question/one","scope":"question","status":"queued","job_id":"job-question"}`))
		default:
			http.Error(response, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	if _, err := client.GetModels(ctx, "cli-token", "project one"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetModelQuestion(ctx, "cli-token", "project one", "question/one"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SyncModels(ctx, "cli-token", "project one", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SyncModels(ctx, "cli-token", "project one", "question/one"); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"GET /v1/projects/project%20one/models",
		"GET /v1/projects/project%20one/models/questions/question%2Fone",
		"POST /v1/projects/project%20one/models/source/sync",
		"POST /v1/projects/project%20one/models/questions/question%2Fone/sync",
	}, "\n")
	if got := strings.Join(requests, "\n"); got != want {
		t.Fatalf("routes:\n%s\nwant:\n%s", got, want)
	}
}
