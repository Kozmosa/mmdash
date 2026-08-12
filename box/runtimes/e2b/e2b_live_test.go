package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

func TestLiveE2BAcceptance(t *testing.T) {
	if os.Getenv("E2B_LIVE_ACCEPTANCE") != "1" {
		t.Skip("set E2B_LIVE_ACCEPTANCE=1 with E2B_API_KEY to run paid provider acceptance")
	}
	apiKey := strings.TrimSpace(os.Getenv("E2B_API_KEY"))
	if apiKey == "" {
		t.Fatal("E2B_API_KEY is required for live acceptance")
	}
	domain := envOrDefault("E2B_DOMAIN", "e2b.app")
	config := Config{
		APIKey:         apiKey,
		Domain:         domain,
		APIURL:         envOrDefault("E2B_API_URL", "https://api."+domain),
		SandboxURL:     strings.TrimSpace(os.Getenv("E2B_SANDBOX_URL")),
		RequestTimeout: 60 * time.Second, CleanupTimeout: 30 * time.Second,
		SandboxGrace: 30 * time.Second,
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	template := envOrDefault("MMDASH_E2B_TEMPLATE", "base")
	before, err := liveSandboxCount(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("success_logs_files_and_artifact", func(t *testing.T) {
		workspace := liveWorkspace(t)
		output := t.TempDir()
		var stdout, stderr bytes.Buffer
		request := liveRunRequest("mmdash-live-success", workspace, output, 30, "success")
		request.Stdout, request.Stderr = &stdout, &stderr
		result, runErr := client.Run(context.Background(), template, request)
		if runErr != nil {
			t.Fatal(runErr)
		}
		if result.ExitCode != 0 || result.TimedOut || result.Canceled {
			t.Fatalf("live result: %#v", result)
		}
		if !strings.Contains(stdout.String(), "MMDASH_E2B_STDOUT") || !strings.Contains(stderr.String(), "MMDASH_E2B_STDERR") {
			t.Fatalf("live logs were incomplete: stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
		manifestData, readErr := os.ReadFile(filepath.Join(output, "manifest.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		var manifest contracts.Manifest
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			t.Fatal(err)
		}
		artifactPath := filepath.Join(t.TempDir(), "artifact.zip")
		if _, err := sandbox.BuildArtifactZip(output, artifactPath, manifest); err != nil {
			t.Fatalf("live artifact validation: %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		request := liveRunRequest("mmdash-live-timeout", liveWorkspace(t), t.TempDir(), 2, "sleep")
		result, runErr := client.Run(context.Background(), template, request)
		if runErr != nil || !result.TimedOut || result.Canceled {
			t.Fatalf("live timeout: %#v %v", result, runErr)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		started := make(chan struct{})
		writer := &notifyingWriter{match: "MMDASH_E2B_STDOUT", notify: started}
		request := liveRunRequest("mmdash-live-cancel", liveWorkspace(t), t.TempDir(), 30, "sleep")
		request.Stdout = writer
		type outcome struct {
			result sandbox.RunResult
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			result, runErr := client.Run(context.Background(), template, request)
			done <- outcome{result: result, err: runErr}
		}()
		select {
		case <-started:
		case value := <-done:
			t.Fatalf("live E2B command failed before start: %#v %v", value.result, value.err)
		case <-time.After(30 * time.Second):
			t.Fatal("live E2B command did not start")
		}
		if err := client.Cancel(context.Background(), request.ID); err != nil {
			t.Fatal(err)
		}
		select {
		case value := <-done:
			if value.err != nil || !value.result.Canceled || value.result.TimedOut {
				t.Fatalf("live cancel: %#v %v", value.result, value.err)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("live canceled command did not stop")
		}
	})

	deadline := time.Now().Add(15 * time.Second)
	for {
		after, countErr := liveSandboxCount(context.Background(), client)
		if countErr == nil && after == before {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live sandbox cleanup did not converge: before=%d after=%d error=%v", before, after, countErr)
		}
		time.Sleep(time.Second)
	}
}

func liveWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	script := `import hashlib
import json
import os
import sys
import time

print("MMDASH_E2B_STDOUT", flush=True)
print("MMDASH_E2B_STDERR", file=sys.stderr, flush=True)
if os.environ.get("MMDASH_ACCEPTANCE_MODE") == "sleep":
    time.sleep(30)

os.makedirs("/output/tables", exist_ok=True)
files = {
    "summary.md": b"E2B acceptance passed\n",
    "tables/result.csv": b"x,y\n1,2\n",
}
manifest_files = []
for relative, content in files.items():
    destination = os.path.join("/output", relative)
    with open(destination, "wb") as handle:
        handle.write(content)
    manifest_files.append({
        "path": relative,
        "sha256": hashlib.sha256(content).hexdigest(),
        "size_bytes": len(content),
        "kind": "summary" if relative == "summary.md" else "table",
    })
manifest = {
    "schema_version": "1",
    "experiment_id": "experiment-live",
    "status": "succeeded",
    "summary": "E2B acceptance passed",
    "exit_code": 0,
    "files": manifest_files,
}
with open("/output/manifest.json", "w", encoding="utf-8") as handle:
    json.dump(manifest, handle)
`
	if err := os.WriteFile(filepath.Join(workspace, "run.py"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func liveRunRequest(id, workspace, output string, timeout int, mode string) sandbox.RunRequest {
	return sandbox.RunRequest{
		ID: id,
		Spec: contracts.RunSpec{
			SchemaVersion: "1", ExperimentID: "experiment-live", ProjectID: "project-live",
			SourceCommit: strings.Repeat("b", 40), Entrypoint: "python:run.py", Runtime: "e2b",
			Environment: map[string]string{"MMDASH_ACCEPTANCE_MODE": mode},
			Limits:      contracts.ResourceLimits{CPUMillis: 1000, MemoryBytes: 512 << 20, TimeoutSecond: timeout, DiskBytes: 1 << 30, PIDs: 64, Network: "disabled"},
		},
		Workspace: workspace, OutputDir: output,
	}
}

func liveSandboxCount(ctx context.Context, client *ProviderClient) (int, error) {
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, client.apiURL+"/v2/sandboxes?limit=100", nil)
	if err != nil {
		return 0, err
	}
	client.platformHeaders(request)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, client.responseError("list E2B sandboxes", response)
	}
	var values []json.RawMessage
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&values); err != nil {
		return 0, err
	}
	return len(values), nil
}

type notifyingWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	match  string
	notify chan struct{}
	once   sync.Once
}

func (writer *notifyingWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	written, err := writer.buffer.Write(data)
	if strings.Contains(writer.buffer.String(), writer.match) {
		writer.once.Do(func() { close(writer.notify) })
	}
	return written, err
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func (writer *notifyingWriter) String() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.buffer.String()
}

var _ io.Writer = (*notifyingWriter)(nil)
