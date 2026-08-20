package localdocker

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

type fakeDocker struct {
	mu           sync.Mutex
	calls        [][]string
	builds       int
	removes      int
	buildErr     error
	buildStarted chan struct{}
	allowBuild   chan struct{}
	contextFiles []string
	dockerfile   string
	managedKey   string
}

func (docker *fakeDocker) Run(_ context.Context, _ io.Writer, _ io.Writer, args ...string) error {
	return docker.recordRun(args...)
}

var _ DockerCommandRunner = (*fakeDocker)(nil)

func (docker *fakeDocker) Output(_ context.Context, args ...string) ([]byte, error) {
	docker.mu.Lock()
	defer docker.mu.Unlock()
	docker.calls = append(docker.calls, append([]string{"output"}, args...))
	if len(args) >= 2 && args[0] == "image" && args[1] == "inspect" {
		if len(args) >= 5 && strings.Contains(args[3], "io.mmdash.managed") {
			return []byte("sha256:managed\ttrue\t" + docker.managedKey + "\n"), nil
		}
		if strings.HasSuffix(args[len(args)-1], "python:3.12-slim") {
			return []byte("sha256:base\n"), nil
		}
		return []byte("sha256:managed\n"), nil
	}
	return nil, errors.New("unexpected docker output")
}

func (docker *fakeDocker) recordRun(args ...string) error {
	docker.mu.Lock()
	docker.calls = append(docker.calls, args)
	if len(args) > 0 && args[0] == "build" {
		docker.builds++
		for index, arg := range args {
			if arg == "--label" && index+1 < len(args) && strings.HasPrefix(args[index+1], "io.mmdash.environment_key=") {
				docker.managedKey = strings.TrimPrefix(args[index+1], "io.mmdash.environment_key=")
			}
		}
		if docker.buildStarted != nil {
			select {
			case <-docker.buildStarted:
			default:
				close(docker.buildStarted)
			}
		}
		if path := findArg(args, "--file"); path != "" {
			content, _ := os.ReadFile(path)
			docker.dockerfile = string(content)
			contextRoot := args[len(args)-1]
			entries, _ := os.ReadDir(contextRoot)
			for _, entry := range entries {
				docker.contextFiles = append(docker.contextFiles, entry.Name())
			}
		}
	}
	buildErr := docker.buildErr
	docker.mu.Unlock()
	if docker.allowBuild != nil {
		<-docker.allowBuild
	}
	return buildErr
}

func findArg(args []string, needle string) string {
	for index, arg := range args {
		if arg == needle && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

func TestEnvironmentBuildContextIsManifestOnlyAndCacheIsReusable(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "requirements.lock"), []byte("requests==2.32.3 --hash=sha256:abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "run.py"), []byte("print('secret source')"), 0o600); err != nil {
		t.Fatal(err)
	}
	docker := &fakeDocker{}
	manager := NewEnvironmentManager(filepath.Join(root, "environments"), "python:3.12-slim")
	manager.Runner = docker
	request := sandbox.EnvironmentRequest{ID: "task-1", Workspace: workspace, Spec: contracts.RunSpec{Runtime: "local-docker", RuntimeVersion: "2"}}
	result, err := manager.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Managed || result.CacheHit || result.EnvironmentKey == "" || result.ImageID != "sha256:managed" {
		t.Fatalf("unexpected build result: %#v", result)
	}
	if len(docker.contextFiles) != 2 || contains(docker.contextFiles, "run.py") || !contains(docker.contextFiles, "requirements.lock") {
		t.Fatalf("source leaked into build context: %#v", docker.contextFiles)
	}
	if !strings.Contains(docker.dockerfile, "--require-hashes") || strings.Contains(docker.dockerfile, "run.py") {
		t.Fatalf("unsafe or non-reproducible Dockerfile: %s", docker.dockerfile)
	}
	if err := manager.Release(context.Background(), "task-1"); err != nil {
		t.Fatal(err)
	}
	second, err := manager.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !second.CacheHit || second.ImageID != result.ImageID {
		t.Fatalf("cache was not reused: %#v", second)
	}
	docker.mu.Lock()
	builds := docker.builds
	docker.mu.Unlock()
	if builds != 1 {
		t.Fatalf("expected one Docker build, got %d", builds)
	}
}

func TestEnvironmentBuildFailureDoesNotCreateCacheEntry(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "requirements.lock"), []byte("requests==2.32.3 --hash=sha256:abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	docker := &fakeDocker{buildErr: errors.New("pip download failed")}
	manager := NewEnvironmentManager(filepath.Join(root, "environments"), "python:3.12-slim")
	manager.Runner = docker
	_, err := manager.Prepare(context.Background(), sandbox.EnvironmentRequest{ID: "task-1", Workspace: workspace, Spec: contracts.RunSpec{Runtime: "local-docker", RuntimeVersion: "2"}})
	if err == nil {
		t.Fatal("failed Docker build was accepted")
	}
	state, loadErr := manager.load()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(state.Entries) != 0 {
		t.Fatalf("failed build polluted cache: %#v", state.Entries)
	}
}

func TestEnvironmentRejectsKnownUnsupportedManifest(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "requirements.txt"), []byte("requests==2.32.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewEnvironmentManager(filepath.Join(root, "environments"), "python:3.12-slim")
	_, err := manager.Prepare(context.Background(), sandbox.EnvironmentRequest{ID: "task-1", Workspace: workspace})
	if err == nil || !strings.Contains(err.Error(), "requirements.lock") {
		t.Fatalf("unsupported manifest was accepted without actionable error: %v", err)
	}
	var coded interface{ EnvironmentCode() string }
	if !errors.As(err, &coded) || coded.EnvironmentCode() != "ENVIRONMENT_INVALID" {
		t.Fatalf("unsupported manifest did not receive stable error code: %v", err)
	}
}

func TestEnvironmentGCUsesExactImageIDAfterFourDays(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "requirements.lock"), []byte("requests==2.32.3 --hash=sha256:abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	docker := &fakeDocker{}
	manager := NewEnvironmentManager(filepath.Join(root, "environments"), "python:3.12-slim")
	manager.Runner = docker
	manager.Now = func() time.Time { return time.Unix(1000, 0).UTC() }
	request := sandbox.EnvironmentRequest{ID: "task-1", Workspace: workspace, Spec: contracts.RunSpec{Runtime: "local-docker", RuntimeVersion: "2"}}
	result, err := manager.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Release(context.Background(), request.ID); err != nil {
		t.Fatal(err)
	}
	state, err := manager.load()
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Entries[result.EnvironmentKey]
	entry.LastUsedAt = time.Unix(1000, 0).Add(-EnvironmentCacheTTL - time.Second)
	state.Entries[result.EnvironmentKey] = entry
	if err := manager.save(state); err != nil {
		t.Fatal(err)
	}
	if err := manager.GC(context.Background(), time.Unix(1000, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	docker.mu.Lock()
	defer docker.mu.Unlock()
	found := false
	for _, call := range docker.calls {
		if len(call) == 3 && call[0] == "image" && call[1] == "rm" {
			found = true
			if call[2] != "sha256:managed" {
				t.Fatalf("GC did not delete exact image ID: %#v", call)
			}
		}
	}
	if !found {
		t.Fatal("GC did not remove expired managed image")
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
