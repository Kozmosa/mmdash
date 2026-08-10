package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mmdash/mmdash/box/contracts"
)

func TestHTTPClientRegisterUsesOnlyTheBoxRegistrationContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/boxes" || request.Header.Get("Authorization") != "Bearer registration-token" {
			t.Fatalf("unexpected registration request: %s %s %s", request.Method, request.URL.Path, request.Header.Get("Authorization"))
		}
		var body map[string]interface{}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"project_id", "name", "version", "capabilities", "runtimes", "limits", "idempotency_key"} {
			if _, ok := body[key]; !ok {
				t.Fatalf("registration omitted %s: %#v", key, body)
			}
		}
		if _, leaked := body["provider_api_key"]; leaked {
			t.Fatal("provider credential crossed the Core contract")
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"box":{"box_id":"00000000-0000-4000-8000-000000000001"},"token":"box-token"}`)
	}))
	defer server.Close()

	registration, err := (HTTPClient{BaseURL: server.URL}).Register(nilContext(), "registration-token", RegistrationInput{
		ProjectID: "00000000-0000-4000-8000-000000000002", Name: "box", Version: "1", Idempotency: "register-1",
		Capabilities: []contracts.Capability{{Name: "sandbox", Version: "1"}}, Runtimes: []contracts.Runtime{{Name: "local-docker", Version: "1"}}, Limits: contracts.ResourceLimits{CPUMillis: 1, MemoryBytes: 1 << 20, TimeoutSecond: 1, DiskBytes: 1 << 20, PIDs: 1, Network: "disabled"},
	})
	if err != nil || registration.BoxID == "" || registration.Token != "box-token" {
		t.Fatalf("registration: %#v %v", registration, err)
	}
}

func TestHTTPClientUploadStreamsExactArtifactMetadata(t *testing.T) {
	contents := []byte("artifact")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/boxes/box-1/tasks/task-1/artifact" || request.ContentLength != int64(len(contents)) {
			t.Fatalf("unexpected artifact request: %s %s length=%d", request.Method, request.URL.Path, request.ContentLength)
		}
		if request.Header.Get("Authorization") != "Bearer box-token" || request.Header.Get("X-Mmdash-Artifact-SHA256") != strings.Repeat("a", 64) {
			t.Fatal("artifact authentication or hash header missing")
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != string(contents) {
			t.Fatalf("artifact body changed: %q", body)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"artifact_id":"00000000-0000-4000-8000-000000000003","version_id":"00000000-0000-4000-8000-000000000004","filename":"artifact.zip","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size_bytes":8}`)
	}))
	defer server.Close()
	pointer, err := (HTTPClient{BaseURL: server.URL}).UploadArtifact(nilContext(), "box-1", "box-token", "task-1", strings.NewReader(string(contents)), int64(len(contents)), strings.Repeat("a", 64))
	if err != nil || pointer.Filename != "artifact.zip" || pointer.Size != int64(len(contents)) {
		t.Fatalf("upload: %#v %v", pointer, err)
	}
}

func nilContext() context.Context { return context.Background() }
