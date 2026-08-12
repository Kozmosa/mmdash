package e2b

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

type providerMock struct {
	t       *testing.T
	server  *httptest.Server
	apiKey  string
	token   string
	mu      sync.Mutex
	killed  bool
	created createSandboxRequest
	files   map[string][]byte
	dirs    map[string]struct{}
	extra   map[string][]filesystemEntry
	starts  []recordedStart
	signals []processSignalRequest
	deletes int
	nextPID int
	block   bool
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type recordedStart struct {
	Request processStartRequest
	User    string
}

func newProviderMock(t *testing.T, block bool) *providerMock {
	t.Helper()
	mock := &providerMock{
		t: t, apiKey: "test-api-key", token: "envd-access-token", files: map[string][]byte{},
		dirs: map[string]struct{}{remoteOutput: {}, remoteOutput + "/tables": {}}, extra: map[string][]filesystemEntry{},
		nextPID: 100, block: block, started: make(chan struct{}), release: make(chan struct{}),
	}
	mock.files[remoteOutput+"/manifest.json"] = []byte(`{"schema_version":"1","experiment_id":"experiment-1","status":"succeeded","files":[]}`)
	mock.files[remoteOutput+"/summary.md"] = []byte("accepted\n")
	mock.files[remoteOutput+"/tables/result.csv"] = []byte("x,y\n1,2\n")
	mux := http.NewServeMux()
	mux.HandleFunc("/sandboxes", mock.handleSandboxes)
	mux.HandleFunc("/sandboxes/", mock.handleSandbox)
	mux.HandleFunc("/files", mock.handleFiles)
	mux.Handle(processStartProcedure, connect.NewServerStreamHandler(processStartProcedure, mock.handleProcessStart, connect.WithCodec(providerJSONCodec{})))
	mux.Handle(processSignalProcedure, connect.NewUnaryHandler(processSignalProcedure, mock.handleSignal, connect.WithCodec(providerJSONCodec{})))
	mux.Handle(filesystemMakeProcedure, connect.NewUnaryHandler(filesystemMakeProcedure, mock.handleMakeDir, connect.WithCodec(providerJSONCodec{})))
	mux.Handle(filesystemListProcedure, connect.NewUnaryHandler(filesystemListProcedure, mock.handleListDir, connect.WithCodec(providerJSONCodec{})))
	mock.server = httptest.NewServer(mux)
	t.Cleanup(mock.server.Close)
	return mock
}

func (mock *providerMock) client(t *testing.T) *ProviderClient {
	t.Helper()
	client, err := NewClient(Config{
		APIKey: mock.apiKey, APIURL: mock.server.URL, SandboxURL: mock.server.URL,
		HTTPClient: mock.server.Client(), RequestTimeout: 5 * time.Second,
		CleanupTimeout: 5 * time.Second, SandboxGrace: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func (mock *providerMock) handleSandboxes(response http.ResponseWriter, request *http.Request) {
	mock.requirePlatform(request)
	if request.Method != http.MethodPost {
		http.Error(response, "method", http.StatusMethodNotAllowed)
		return
	}
	var body createSandboxRequest
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	mock.mu.Lock()
	mock.created = body
	mock.killed = false
	mock.mu.Unlock()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusCreated)
	_, _ = io.WriteString(response, fmt.Sprintf(`{"templateID":%q,"sandboxID":"sandbox-1","clientID":"deprecated-client","envdVersion":"0.6.5","envdAccessToken":%q,"trafficAccessToken":"traffic","domain":null}`, body.TemplateID, mock.token))
}

func (mock *providerMock) handleSandbox(response http.ResponseWriter, request *http.Request) {
	mock.requirePlatform(request)
	if request.URL.Path == "/sandboxes/sandbox-1/metrics" {
		if request.Method != http.MethodGet || request.URL.Query().Get("start") == "" || request.URL.Query().Get("end") == "" {
			http.Error(response, "invalid metrics request", http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `[{"timestamp":"2026-08-11T00:00:00Z","timestampUnix":1786406400,"cpuCount":1,"cpuUsedPct":12.5,"memUsed":1234,"memTotal":1073741824,"memCache":10,"diskUsed":5678,"diskTotal":10737418240}]`)
		return
	}
	if request.URL.Path != "/sandboxes/sandbox-1" {
		http.NotFound(response, request)
		return
	}
	switch request.Method {
	case http.MethodGet:
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"templateID":"base","sandboxID":"sandbox-1","clientID":"deprecated-client","startedAt":"2026-08-11T00:00:00Z","endAt":"2026-08-11T00:05:00Z","envdVersion":"0.6.5","envdAccessToken":"envd-access-token","allowInternetAccess":false,"domain":null,"cpuCount":1,"memoryMB":512,"diskSizeMB":10240,"metadata":{},"state":"running","network":{"denyOut":["0.0.0.0/0"],"allowPublicTraffic":false},"lifecycle":{"onTimeout":"kill","autoResume":false},"volumeMounts":[]}`)
	case http.MethodDelete:
		mock.mu.Lock()
		mock.deletes++
		alreadyKilled := mock.killed
		mock.killed = true
		mock.mu.Unlock()
		if alreadyKilled {
			http.Error(response, `{"code":404,"message":"not found"}`, http.StatusNotFound)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		http.Error(response, "method", http.StatusMethodNotAllowed)
	}
}

func (mock *providerMock) handleFiles(response http.ResponseWriter, request *http.Request) {
	mock.requireEnvd(request, "")
	remotePath := request.URL.Query().Get("path")
	if request.URL.Query().Get("username") != "root" || remotePath == "" {
		http.Error(response, "missing path or user", http.StatusBadRequest)
		return
	}
	switch request.Method {
	case http.MethodPost:
		if request.Header.Get("Content-Type") != "application/octet-stream" {
			http.Error(response, "content type", http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		mock.mu.Lock()
		mock.files[remotePath] = data
		mock.dirs[path.Dir(remotePath)] = struct{}{}
		mock.mu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, fmt.Sprintf(`[{"path":%q,"name":%q,"type":"file","metadata":{}}]`, remotePath, path.Base(remotePath)))
	case http.MethodGet:
		mock.mu.Lock()
		data, found := mock.files[remotePath]
		mock.mu.Unlock()
		if !found {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/octet-stream")
		response.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = response.Write(data)
	default:
		http.Error(response, "method", http.StatusMethodNotAllowed)
	}
}

func (mock *providerMock) handleProcessStart(ctx context.Context, request *connect.Request[processStartRequest], stream *connect.ServerStream[processStartResponse]) error {
	user := mock.requireEnvdHeader(request.Header())
	if request.Header().Get(keepalivePingHeader) != keepalivePingSeconds {
		mock.t.Errorf("missing official E2B keepalive header: %#v", request.Header())
	}
	mock.mu.Lock()
	mock.nextPID++
	pid := mock.nextPID
	mock.starts = append(mock.starts, recordedStart{Request: *request.Msg, User: user})
	mock.mu.Unlock()
	if err := stream.Send(&processStartResponse{Event: &processEvent{Start: &processStartEvent{PID: pid}}}); err != nil {
		return err
	}
	main := request.Msg.Process.Cmd == "/usr/bin/prlimit"
	if main {
		if err := stream.Send(&processStartResponse{Event: &processEvent{Data: &processDataEvent{Stdout: []byte("hello from E2B\n")}}}); err != nil {
			return err
		}
		if err := stream.Send(&processStartResponse{Event: &processEvent{Data: &processDataEvent{Stderr: []byte("provider warning\n")}}}); err != nil {
			return err
		}
		mock.once.Do(func() { close(mock.started) })
		if mock.block {
			select {
			case <-mock.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	status := "exit status 0"
	if main && mock.block {
		status = "exit status 143"
	}
	return stream.Send(&processStartResponse{Event: &processEvent{End: &processEndEvent{Exited: true, Status: status}}})
}

func (mock *providerMock) handleSignal(_ context.Context, request *connect.Request[processSignalRequest]) (*connect.Response[processSignalResponse], error) {
	mock.requireEnvdHeader(request.Header())
	mock.mu.Lock()
	mock.signals = append(mock.signals, *request.Msg)
	mock.mu.Unlock()
	mock.once.Do(func() { close(mock.started) })
	select {
	case <-mock.release:
	default:
		close(mock.release)
	}
	return connect.NewResponse(&processSignalResponse{}), nil
}

func (mock *providerMock) handleMakeDir(_ context.Context, request *connect.Request[filesystemMakeDirRequest]) (*connect.Response[filesystemMakeDirResponse], error) {
	mock.requireEnvdHeader(request.Header())
	mock.mu.Lock()
	mock.dirs[request.Msg.Path] = struct{}{}
	mock.mu.Unlock()
	return connect.NewResponse(&filesystemMakeDirResponse{}), nil
}

func (mock *providerMock) handleListDir(_ context.Context, request *connect.Request[filesystemListDirRequest]) (*connect.Response[filesystemListDirResponse], error) {
	mock.requireEnvdHeader(request.Header())
	mock.mu.Lock()
	defer mock.mu.Unlock()
	prefix := strings.TrimRight(request.Msg.Path, "/") + "/"
	entries := map[string]filesystemEntry{}
	for directory := range mock.dirs {
		if directory == request.Msg.Path || !strings.HasPrefix(directory, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(directory, prefix)
		if remainder == "" || strings.Contains(remainder, "/") {
			continue
		}
		entries[directory] = filesystemEntry{Name: path.Base(directory), Type: "FILE_TYPE_DIRECTORY", Path: directory}
	}
	for filePath, data := range mock.files {
		if !strings.HasPrefix(filePath, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(filePath, prefix)
		if remainder == "" || strings.Contains(remainder, "/") {
			continue
		}
		entries[filePath] = filesystemEntry{Name: path.Base(filePath), Type: "FILE_TYPE_FILE", Path: filePath, Size: flexibleInt64(len(data))}
	}
	for _, entry := range mock.extra[request.Msg.Path] {
		entries[entry.Path] = entry
	}
	result := make([]filesystemEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	return connect.NewResponse(&filesystemListDirResponse{Entries: result}), nil
}

func (mock *providerMock) requirePlatform(request *http.Request) {
	mock.t.Helper()
	if request.Header.Get("X-API-Key") != mock.apiKey || request.Header.Get("User-Agent") != providerUserAgent {
		mock.t.Errorf("invalid platform headers: %#v", request.Header)
	}
}

func (mock *providerMock) requireEnvd(request *http.Request, user string) {
	mock.t.Helper()
	actualUser := mock.requireEnvdHeader(request.Header)
	if user != "" && actualUser != user {
		mock.t.Errorf("envd user = %q, want %q", actualUser, user)
	}
}

func (mock *providerMock) requireEnvdHeader(header http.Header) string {
	mock.t.Helper()
	if header.Get("E2b-Sandbox-Id") != "sandbox-1" || header.Get("E2b-Sandbox-Port") != "49983" || header.Get("X-Access-Token") != mock.token {
		mock.t.Errorf("invalid envd headers: %#v", header)
	}
	auth := header.Get("Authorization")
	if auth == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
	if err != nil {
		mock.t.Errorf("decode basic auth: %v", err)
		return ""
	}
	return strings.TrimSuffix(string(decoded), ":")
}

func TestProviderClientRunsOfficialLifecycleAndCollectsOutput(t *testing.T) {
	mock := newProviderMock(t, false)
	client := mock.client(t)
	workspace := t.TempDir()
	output := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "run.py"), []byte("print('ok')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "helper"), []byte("helper\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("run.py", filepath.Join(workspace, "run-link.py")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".git", "credential"), []byte("must-not-upload"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	request := providerRunRequest("task-1", workspace, output, 5)
	request.Stdout, request.Stderr = &stdout, &stderr
	result, err := client.Run(context.Background(), "base", request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.TimedOut || result.Canceled {
		t.Fatalf("result: %#v", result)
	}
	if stdout.String() != "hello from E2B\n" || stderr.String() != "provider warning\n" {
		t.Fatalf("logs: %q %q", stdout.String(), stderr.String())
	}
	if result.ResourceUsage["cpu_used_percent_peak"] != 12.5 || result.ResourceUsage["memory_used_bytes_peak"] != int64(1234) {
		t.Fatalf("resource usage: %#v", result.ResourceUsage)
	}
	for relative, expected := range map[string]string{"manifest.json": string(mock.files[remoteOutput+"/manifest.json"]), "summary.md": "accepted\n", "tables/result.csv": "x,y\n1,2\n"} {
		data, readErr := os.ReadFile(filepath.Join(output, filepath.FromSlash(relative)))
		if readErr != nil || string(data) != expected {
			t.Fatalf("output %s: %q %v", relative, string(data), readErr)
		}
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if !mock.created.Secure || mock.created.AllowInternetAccess || !slices.Equal(mock.created.Network.DenyOut, []string{allIPv4Traffic}) || mock.created.Network.AllowPublicTraffic {
		t.Fatalf("create network/security: %#v", mock.created)
	}
	if mock.created.TemplateID != "base" || mock.created.Timeout != 10 || mock.created.EnvVars["EXAMPLE"] != "value" || mock.created.Metadata["mmdash_task_id"] != "task-1" {
		t.Fatalf("create body: %#v", mock.created)
	}
	if _, leaked := mock.files[remoteWorkspace+"/.git/credential"]; leaked {
		t.Fatal(".git credential was uploaded")
	}
	if string(mock.files[remoteWorkspace+"/run.py"]) != "print('ok')\n" {
		t.Fatalf("workspace upload: %#v", mock.files)
	}
	if mock.deletes != 1 {
		t.Fatalf("sandbox deletes = %d", mock.deletes)
	}
	var main *recordedStart
	for index := range mock.starts {
		item := &mock.starts[index]
		if item.Request.Process.Cmd == "/usr/bin/prlimit" {
			main = item
		}
	}
	if main == nil || main.User != "user" || main.Request.Process.Cwd == nil || *main.Request.Process.Cwd != remoteWorkspace || main.Request.Process.Envs["EXAMPLE"] != "value" {
		t.Fatalf("main process: %#v", main)
	}
	if !slices.Equal(main.Request.Process.Args, []string{"--nproc=128:128", "--", "python3", "/workspace/run.py"}) {
		t.Fatalf("main argv: %#v", main.Request.Process.Args)
	}
	if !hasSetupCommand(mock.starts, "/bin/ln") || !hasSetupCommand(mock.starts, "/bin/chmod") || !hasSetupCommand(mock.starts, "/bin/chown") {
		t.Fatalf("setup commands: %#v", mock.starts)
	}
}

func TestProviderClientCancelSignalsAndKillsSandbox(t *testing.T) {
	mock := newProviderMock(t, true)
	client := mock.client(t)
	request := providerRunRequest("task-cancel", minimalWorkspace(t), t.TempDir(), 30)
	type outcome struct {
		result sandbox.RunResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := client.Run(context.Background(), "base", request)
		done <- outcome{result: result, err: err}
	}()
	select {
	case <-mock.started:
	case <-time.After(5 * time.Second):
		t.Fatal("main process did not start")
	}
	if err := client.Cancel(context.Background(), request.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-done:
		if value.err != nil || !value.result.Canceled || value.result.TimedOut {
			t.Fatalf("cancel outcome: %#v %v", value.result, value.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled run did not stop")
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.signals) == 0 || mock.signals[0].Signal != signalTerminate || mock.signals[0].Process.PID <= 0 {
		t.Fatalf("signals: %#v", mock.signals)
	}
	if mock.deletes < 1 {
		t.Fatal("sandbox was not deleted")
	}
}

func TestProviderClientTimeoutKillsProcessAndSandbox(t *testing.T) {
	mock := newProviderMock(t, true)
	client := mock.client(t)
	request := providerRunRequest("task-timeout", minimalWorkspace(t), t.TempDir(), 1)
	result, err := client.Run(context.Background(), "base", request)
	if err != nil || !result.TimedOut || result.Canceled {
		t.Fatalf("timeout outcome: %#v %v", result, err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.signals) == 0 || mock.signals[len(mock.signals)-1].Signal != signalKill {
		t.Fatalf("signals: %#v", mock.signals)
	}
	if mock.deletes < 1 {
		t.Fatal("sandbox was not deleted")
	}
}

func TestProviderClientRedactsCredentialFromProviderErrors(t *testing.T) {
	apiKey := "secret-provider-key"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(response, fmt.Sprintf(`{"code":401,"message":"bad key %s"}`, apiKey))
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: apiKey, APIURL: server.URL, SandboxURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Run(context.Background(), "base", providerRunRequest("task-error", minimalWorkspace(t), t.TempDir(), 5))
	if err == nil || strings.Contains(err.Error(), apiKey) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error was not sanitized: %v", err)
	}
}

func TestProviderClientDestroysSandboxWhenSecureCreateResponseIsIncomplete(t *testing.T) {
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/sandboxes":
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(response, `{"sandboxID":"incomplete-sandbox","envdVersion":"0.6.5","envdAccessToken":null}`)
		case request.Method == http.MethodDelete && request.URL.Path == "/sandboxes/incomplete-sandbox":
			deleted = true
			response.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: "test-key", APIURL: server.URL, SandboxURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Run(context.Background(), "base", providerRunRequest("task-incomplete", minimalWorkspace(t), t.TempDir(), 5))
	if err == nil || !deleted {
		t.Fatalf("incomplete create cleanup: deleted=%v error=%v", deleted, err)
	}
}

func TestProviderClientReconcilesSandboxWhenCreateResponseLosesIdentity(t *testing.T) {
	deleted := false
	taskID := "task-ambiguous-create"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/sandboxes":
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(response, `{`)
		case request.Method == http.MethodGet && request.URL.Path == "/v2/sandboxes":
			if request.URL.Query().Get("metadata") != "mmdash_task_id="+taskID || request.URL.Query().Get("limit") != "100" {
				http.Error(response, "invalid metadata reconciliation", http.StatusBadRequest)
				return
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(response, `[{"sandboxID":"ambiguous-sandbox","metadata":{"mmdash_task_id":"task-ambiguous-create"}}]`)
		case request.Method == http.MethodDelete && request.URL.Path == "/sandboxes/ambiguous-sandbox":
			deleted = true
			response.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: "test-key", APIURL: server.URL, SandboxURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Run(context.Background(), "base", providerRunRequest(taskID, minimalWorkspace(t), t.TempDir(), 5))
	if err == nil || !deleted {
		t.Fatalf("ambiguous create cleanup: deleted=%v error=%v", deleted, err)
	}
}

func TestProviderClientUsesOfficialDynamicSandboxDomainRouting(t *testing.T) {
	client, err := NewClient(Config{APIKey: "test-key", Domain: "self-hosted.example"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := client.sandboxEndpoint("sandbox-123", "cluster.example")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://49983-sandbox-123.cluster.example" {
		t.Fatalf("dynamic sandbox endpoint = %q", endpoint)
	}
	hosted, err := NewClient(Config{APIKey: "test-key", APIURL: "https://control.example"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err = hosted.sandboxEndpoint("sandbox-123", "e2b.app")
	if err != nil || endpoint != "https://sandbox.e2b.app" {
		t.Fatalf("stable hosted sandbox endpoint = %q, %v", endpoint, err)
	}
	explicit, err := NewClient(Config{APIKey: "test-key", Domain: "self-hosted.example", SandboxURL: "http://envd.internal:49983"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err = explicit.sandboxEndpoint("sandbox-123", "ignored.example")
	if err != nil || endpoint != "http://envd.internal:49983" {
		t.Fatalf("explicit sandbox endpoint = %q, %v", endpoint, err)
	}
}

func TestProviderClientRejectsSymlinkOutputAndStillDestroys(t *testing.T) {
	mock := newProviderMock(t, false)
	mock.extra[remoteOutput] = []filesystemEntry{{Name: "escape", Type: "FILE_TYPE_SYMLINK", Path: remoteOutput + "/escape"}}
	client := mock.client(t)
	_, err := client.Run(context.Background(), "base", providerRunRequest("task-output-symlink", minimalWorkspace(t), t.TempDir(), 5))
	if err == nil || !strings.Contains(err.Error(), "refusing symlink") {
		t.Fatalf("symlink output error: %v", err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.deletes != 1 {
		t.Fatalf("sandbox deletes = %d", mock.deletes)
	}
}

func TestProviderCapacityUsesTemplateAsTheResourceEnvelope(t *testing.T) {
	request := providerRunRequest("task-capacity", t.TempDir(), t.TempDir(), 5)
	request.Spec.Limits.MemoryBytes = 513 << 20
	if err := validateProviderCapacity(sandboxInfo{CPUCount: 1, MemoryMB: 512, DiskSizeMB: 10240}, request); err == nil || !strings.Contains(err.Error(), "available=536870912") {
		t.Fatalf("capacity error: %v", err)
	}
}

func TestOutputRelativeRejectsProviderTraversal(t *testing.T) {
	for _, value := range []string{"/output", "/output/../secret", "/other/file", `\\output\\..\\secret`} {
		if _, err := outputRelative(value); err == nil {
			t.Fatalf("unsafe provider path accepted: %q", value)
		}
	}
	if value, err := outputRelative("/output/tables/result.csv"); err != nil || value != "tables/result.csv" {
		t.Fatalf("safe provider path: %q %v", value, err)
	}
}

func providerRunRequest(id, workspace, output string, timeout int) sandbox.RunRequest {
	return sandbox.RunRequest{
		ID: id,
		Spec: contracts.RunSpec{
			SchemaVersion: "1", ExperimentID: "experiment-1", ProjectID: "project-1",
			SourceCommit: strings.Repeat("a", 40), Entrypoint: "python:run.py", Runtime: "e2b",
			Environment: map[string]string{"EXAMPLE": "value"},
			Limits:      contracts.ResourceLimits{CPUMillis: 1000, MemoryBytes: 512 << 20, TimeoutSecond: timeout, DiskBytes: 1 << 30, PIDs: 128, Network: "disabled"},
		},
		Workspace: workspace, OutputDir: output,
	}
}

func minimalWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "run.py"), []byte("print('ok')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func hasSetupCommand(starts []recordedStart, command string) bool {
	for _, start := range starts {
		if start.Request.Process.Cmd == command && start.User == "root" {
			return true
		}
	}
	return false
}
