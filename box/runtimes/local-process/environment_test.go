package localprocess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner records the executed commands and materialises a minimal fake
// virtual environment so the cache logic is testable without a Python
// installation.
type fakeRunner struct {
	commands [][]string
	failOn   func(argv []string) error
	freeze   []string
}

func (runner *fakeRunner) Run(_ context.Context, _ string, _ []string, _, _ io.Writer, argv ...string) error {
	runner.commands = append(runner.commands, argv)
	if runner.failOn != nil {
		if err := runner.failOn(argv); err != nil {
			return err
		}
	}
	if len(argv) >= 3 && argv[1] == "-m" && argv[2] == "venv" ||
		len(argv) >= 2 && argv[0] == "uv" && argv[1] == "venv" {
		return os.MkdirAll(argv[len(argv)-1], 0o700)
	}
	if len(argv) > 2 && argv[2] == "pip" {
		// pip freeze/fake install outputs are provided by freezeOutput.
	}
	return nil
}

func (runner *fakeRunner) Output(_ context.Context, _ string, _ []string, argv ...string) ([]byte, error) {
	runner.commands = append(runner.commands, argv)
	joined := strings.Join(argv, " ")
	switch {
	case strings.Contains(joined, "sys.version_info"):
		return []byte("3.12.7\n"), nil
	case strings.Contains(joined, "pip --version"):
		return []byte("pip 24.0 from /usr/lib/python3/pip (python 3.12)\n"), nil
	case strings.Contains(joined, "uv --version"):
		return []byte("uv 0.5.1\n"), nil
	case strings.Contains(joined, "poetry --version"):
		return []byte("Poetry (version 1.8.3)\n"), nil
	case strings.Contains(joined, "pipenv --version"):
		return []byte("pipenv, version 2023.12.20\n"), nil
	case strings.Contains(joined, "pip freeze") || strings.Contains(joined, "uv pip freeze"):
		return []byte(strings.Join(runner.freeze, "\n") + "\n"), nil
	}
	if runner.failOn != nil {
		if err := runner.failOn(argv); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func newTestManager(t *testing.T, python string) (*EnvironmentManager, *fakeRunner) {
	t.Helper()
	runner := &fakeRunner{freeze: []string{"numpy==1.26.4", "scipy==1.11.4"}}
	manager := NewEnvironmentManager(t.TempDir(), python)
	manager.Runner = runner
	fixed := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	manager.Now = func() time.Time { return fixed }
	return manager, runner
}

func TestEnvironmentPrepareCacheMissAndHit(t *testing.T) {
	manager, runner := newTestManager(t, "/usr/bin/python3")
	workspace := t.TempDir()
	writeWorkspaceFile(t, workspace, "requirements.lock", "numpy==1.26.4 --hash=sha256:0000000000000000000000000000000000000000000000000000000000000000\n")

	result, err := manager.Prepare(context.Background(), "task-1", workspace, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if result.CacheHit {
		t.Fatal("first prepare must be a cache miss")
	}
	if result.VenvDir == "" || result.Provider != "local-process" {
		t.Fatalf("incomplete environment result: %+v", result)
	}
	if len(result.ResolvedDependencies) != 2 {
		t.Fatalf("resolved dependency evidence missing: %v", result.ResolvedDependencies)
	}
	if _, err := os.Stat(filepath.Join(manager.Root, result.EnvironmentKey, "entry.json")); err != nil {
		t.Fatalf("cache entry was not published atomically: %v", err)
	}

	second, err := manager.Prepare(context.Background(), "task-2", workspace, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !second.CacheHit || second.EnvironmentKey != result.EnvironmentKey {
		t.Fatalf("second prepare must reuse the cache: %+v", second)
	}
	entry, ok := manager.loadEntry(result.EnvironmentKey)
	if !ok {
		t.Fatal("cache entry vanished")
	}
	if len(entry.ActiveRefs) != 2 {
		t.Fatalf("expected both task references, got %v", entry.ActiveRefs)
	}
	if err := manager.Release(context.Background(), "task-1"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Release(context.Background(), "task-2"); err != nil {
		t.Fatal(err)
	}
	entry, _ = manager.loadEntry(result.EnvironmentKey)
	if len(entry.ActiveRefs) != 0 {
		t.Fatalf("release did not clear the references: %v", entry.ActiveRefs)
	}
	if len(runner.commands) == 0 {
		t.Fatal("no build commands were recorded")
	}
}

func TestEnvironmentPrepareKeyReflectsManifestContent(t *testing.T) {
	manager, _ := newTestManager(t, "python3")
	workspace := t.TempDir()
	writeWorkspaceFile(t, workspace, "requirements.lock", "numpy==1.26.4 --hash=sha256:0000000000000000000000000000000000000000000000000000000000000000\n")
	first, err := manager.Prepare(context.Background(), "task-1", workspace, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	writeWorkspaceFile(t, workspace, "requirements.lock", "numpy==2.0.0 --hash=sha256:1111111111111111111111111111111111111111111111111111111111111111\n")
	second, err := manager.Prepare(context.Background(), "task-2", workspace, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if first.EnvironmentKey == second.EnvironmentKey {
		t.Fatal("changed manifest content must produce a different environment key")
	}
}

func TestEnvironmentFailedBuildIsNotCached(t *testing.T) {
	manager, runner := newTestManager(t, "python3")
	runner.failOn = func(argv []string) error {
		if len(argv) > 3 && argv[2] == "pip" && argv[3] == "install" {
			return errors.New("resolution failed")
		}
		return nil
	}
	workspace := t.TempDir()
	writeWorkspaceFile(t, workspace, "requirements.lock", "numpy==1.26.4 --hash=sha256:0000000000000000000000000000000000000000000000000000000000000000\n")
	if _, err := manager.Prepare(context.Background(), "task-1", workspace, nil, io.Discard); err == nil {
		t.Fatal("expected the build to fail")
	}
	items, err := os.ReadDir(manager.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if strings.HasPrefix(item.Name(), ".build-") {
			t.Fatalf("failed build directory was not cleaned up: %s", item.Name())
		}
		if item.IsDir() {
			t.Fatalf("failed build must not enter the cache: %s", item.Name())
		}
	}
}

func TestEnvironmentGCTTLAndReferences(t *testing.T) {
	manager, _ := newTestManager(t, "python3")
	workspace := t.TempDir()
	writeWorkspaceFile(t, workspace, "requirements.lock", "numpy==1.26.4 --hash=sha256:0000000000000000000000000000000000000000000000000000000000000000\n")
	result, err := manager.Prepare(context.Background(), "task-1", workspace, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// An actively referenced environment is never collected, no matter how old.
	base := manager.Now
	manager.Now = func() time.Time { return base().Add(97 * time.Hour) }
	if err := manager.GC(context.Background(), manager.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.loadEntry(result.EnvironmentKey); !ok {
		t.Fatal("referenced environment was collected")
	}
	// After the reference is released and 96 hours pass, the entry is removed.
	if err := manager.Release(context.Background(), "task-1"); err != nil {
		t.Fatal(err)
	}
	ahead := manager.Now
	manager.Now = func() time.Time { return ahead().Add(time.Hour) }
	if err := manager.GC(context.Background(), manager.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.loadEntry(result.EnvironmentKey); ok {
		t.Fatal("unreferenced environment older than 96 hours survived GC")
	}
	if _, err := os.Stat(result.VenvDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("GC must remove the cached environment directory")
	}
}

func TestEnvironmentGCCapacityLRU(t *testing.T) {
	manager, _ := newTestManager(t, "python3")
	freezeSets := [][]string{{"numpy==1.26.4"}, {"scipy==1.11.4"}}
	var clock = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	manager.Now = func() time.Time { return clock }
	keys := make([]string, 0, 2)
	for index, freeze := range freezeSets {
		workspace := t.TempDir()
		writeWorkspaceFile(t, workspace, "requirements.lock",
			fmt.Sprintf("dep%d==1.0 --hash=sha256:%064d\n", index, index))
		runner := &fakeRunner{freeze: freeze}
		manager.Runner = runner
		result, err := manager.Prepare(context.Background(), fmt.Sprintf("task-%d", index), workspace, nil, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, result.EnvironmentKey)
		if err := manager.Release(context.Background(), fmt.Sprintf("task-%d", index)); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Minute)
	}
	// A budget one byte below the recorded total forces exactly one LRU
	// eviction while the most recently used entry survives.
	first, firstOK := manager.loadEntry(keys[0])
	second, secondOK := manager.loadEntry(keys[1])
	if !firstOK || !secondOK {
		t.Fatal("both entries must exist before the capacity check")
	}
	manager.MaxCacheBytes = first.SizeBytes + second.SizeBytes - 1
	if err := manager.GC(context.Background(), clock); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.loadEntry(keys[0]); ok {
		t.Fatal("least recently used entry should have been evicted")
	}
	if _, ok := manager.loadEntry(keys[1]); !ok {
		t.Fatal("most recently used entry should have survived the LRU eviction")
	}
}

func TestEnvironmentNoManifestUsesBareInterpreter(t *testing.T) {
	manager, _ := newTestManager(t, "python3")
	workspace := t.TempDir()
	result, err := manager.Prepare(context.Background(), "task-1", workspace, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if result.VenvDir != "" || result.InterpreterIdentity == "" {
		t.Fatalf("bare interpreter result is wrong: %+v", result)
	}
}
