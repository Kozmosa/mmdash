package localprocess

// This file owns the content-addressed Python environment cache for the
// bare-metal Runtime. A cache entry is a virtual environment that contains
// dependencies only: the transferred Project source is never copied into it
// and the task workspace is never used as the build directory. Builds run in
// a temporary directory and are published atomically, so a failed or partial
// build can never enter the cache.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// EnvironmentBuilderVersion bumps whenever the build strategy output
	// changes in a way that must invalidate cached environments.
	EnvironmentBuilderVersion = "1"
	// PackageIndexConfigVersion participates in the environment key and must
	// change when the configured package index trust changes.
	PackageIndexConfigVersion = "pip-default-v1"
	// CacheTTL is the idle time after which an environment without active
	// task references becomes eligible for ordinary removal.
	CacheTTL = 96 * time.Hour
)

// CommandRunner executes build tooling. It is deliberately narrower than a
// process API and keeps tests independent from a real Python installation.
type CommandRunner interface {
	Run(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, argv ...string) error
	Output(ctx context.Context, dir string, env []string, argv ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, argv ...string) error {
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir = dir
	command.Env = env
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func (execRunner) Output(ctx context.Context, dir string, env []string, argv ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir = dir
	command.Env = env
	return command.CombinedOutput()
}

// EnvironmentResult is retained by the Runtime and surfaced through
// RunResult.ResourceUsage as provider-neutral environment evidence.
type EnvironmentResult struct {
	EnvironmentKey       string
	Provider             string
	InterpreterIdentity  string
	EnvironmentIdentity  string
	VenvDir              string
	PythonVersion        string
	BuilderVersion       string
	CacheHit             bool
	BestEffort           bool
	ManifestPaths        []string
	ManifestHashes       map[string]string
	ResolvedDependencies []string
}

type cacheEntry struct {
	SchemaVersion        int               `json:"schema_version"`
	EnvironmentKey       string            `json:"environment_key"`
	Family               string            `json:"family"`
	PythonVersion        string            `json:"python_version"`
	BuilderVersion       string            `json:"builder_version"`
	BuilderTools         string            `json:"builder_tools"`
	CreatedAt            time.Time         `json:"created_at"`
	LastUsedAt           time.Time         `json:"last_used_at"`
	ActiveRefs           []string          `json:"active_refs,omitempty"`
	SizeBytes            int64             `json:"size_bytes"`
	BestEffort           bool              `json:"best_effort"`
	ManifestPaths        []string          `json:"manifest_paths,omitempty"`
	ManifestHashes       map[string]string `json:"manifest_hashes,omitempty"`
	ResolvedDependencies []string          `json:"resolved_dependencies,omitempty"`
}

type buildFlight struct {
	done chan struct{}
}

// EnvironmentManager builds and caches Box-managed virtual environments. It
// is safe for concurrent Gateway tasks; only one build runs per key.
type EnvironmentManager struct {
	Root           string
	Python         string
	RuntimeName    string
	RuntimeVersion string
	MaxCacheBytes  int64
	Runner         CommandRunner
	Now            func() time.Time

	mu sync.Mutex
	// flights guards the per-key singleflight map.
	flights map[string]*buildFlight
	// entriesMu serialises entry.json read-modify-write cycles.
	entriesMu sync.Mutex
}

func NewEnvironmentManager(root, python string) *EnvironmentManager {
	return &EnvironmentManager{
		Root:    root,
		Python:  python,
		Runner:  execRunner{},
		Now:     func() time.Time { return time.Now().UTC() },
		flights: map[string]*buildFlight{},
	}
}

func (manager *EnvironmentManager) now() func() time.Time {
	if manager.Now != nil {
		return manager.Now
	}
	return func() time.Time { return time.Now().UTC() }
}

func (manager *EnvironmentManager) builderVersion() string { return EnvironmentBuilderVersion }

// Prepare implements the environment pipeline. It returns a cached or newly
// built environment and records an active reference for the task.
func (manager *EnvironmentManager) Prepare(ctx context.Context, taskID, workspace string, selection map[string]string, system io.Writer) (EnvironmentResult, error) {
	if manager == nil || manager.Root == "" || strings.TrimSpace(manager.Python) == "" {
		return EnvironmentResult{}, codedEnvironmentError(EnvCodeUnavailable, errors.New("Local Process environment manager is not configured"))
	}
	now := manager.now()
	info, err := detectManifest(workspace, selection)
	if err != nil {
		return EnvironmentResult{}, err
	}
	pythonVersion, err := manager.pythonVersion(ctx)
	if err != nil {
		return EnvironmentResult{}, codedEnvironmentError(EnvCodeUnavailable, err)
	}
	interpreterIdentity := manager.Python + "@" + pythonVersion
	if info == nil {
		// No dependency manifest: the task runs with the bare interpreter and
		// no cache entry is needed.
		result := EnvironmentResult{
			EnvironmentKey:      environmentKey(manager.Python, manager.builderVersion(), pythonVersion, "none", nil),
			Provider:            "local-process",
			InterpreterIdentity: interpreterIdentity, EnvironmentIdentity: interpreterIdentity,
			PythonVersion: pythonVersion, BuilderVersion: manager.builderVersion(),
			CacheHit: true,
		}
		writeSystem(system, "using the bare Local Process interpreter", resultFields(result))
		return result, nil
	}
	toolIdentity, err := manager.builderToolIdentity(ctx, info.Family)
	if err != nil {
		return EnvironmentResult{}, codedEnvironmentError(EnvCodeUnavailable, err)
	}
	key := environmentKey(manager.Python, manager.builderVersion(), pythonVersion, toolIdentity, info.Files)
	for {
		manager.mu.Lock()
		if flight := manager.flights[key]; flight != nil {
			manager.mu.Unlock()
			select {
			case <-flight.done:
				continue
			case <-ctx.Done():
				return EnvironmentResult{}, ctx.Err()
			}
		}
		flight := &buildFlight{done: make(chan struct{})}
		manager.flights[key] = flight
		manager.mu.Unlock()
		result, buildErr := manager.resolve(ctx, taskID, info, key, pythonVersion, toolIdentity, interpreterIdentity, system)
		if buildErr != nil && errors.Is(buildErr, errEnvironmentEntryVanished) {
			// The published entry disappeared before its task reference was
			// recorded (for example capacity GC between publish and addRef):
			// rebuild once instead of failing the task.
			result, buildErr = manager.resolve(ctx, taskID, info, key, pythonVersion, toolIdentity, interpreterIdentity, system)
		}
		manager.mu.Lock()
		delete(manager.flights, key)
		close(flight.done)
		manager.mu.Unlock()
		if buildErr == nil {
			// Garbage collection is best effort and never blocks a task.
			_ = manager.GC(context.WithoutCancel(ctx), now())
		}
		return result, buildErr
	}
}

func (manager *EnvironmentManager) resolve(ctx context.Context, taskID string, info *manifestInfo, key, pythonVersion, toolIdentity, interpreterIdentity string, system io.Writer) (EnvironmentResult, error) {
	if entry, ok := manager.loadEntry(key); ok {
		if err := manager.addRef(key, taskID, &entry); err == nil {
			result := EnvironmentResult{
				EnvironmentKey: key, Provider: "local-process",
				InterpreterIdentity: interpreterIdentity,
				EnvironmentIdentity: "venv:" + entry.EnvironmentKey,
				VenvDir:             manager.venvDir(key),
				PythonVersion:       entry.PythonVersion,
				BuilderVersion:      entry.BuilderVersion,
				CacheHit:            true, BestEffort: entry.BestEffort,
				ManifestPaths:        entry.ManifestPaths,
				ManifestHashes:       entry.ManifestHashes,
				ResolvedDependencies: entry.ResolvedDependencies,
			}
			writeSystem(system, "reusing cached Local Process environment", resultFields(result))
			return result, nil
		}
		// A damaged entry directory falls through to a rebuild.
	}
	writeSystem(system, "building Local Process environment", map[string]interface{}{
		"environment_key": key, "family": info.Family, "cache_hit": false,
		"builder_version": manager.builderVersion(),
	})
	if err := os.MkdirAll(manager.Root, 0o700); err != nil {
		return EnvironmentResult{}, codedEnvironmentError(EnvCodeUnavailable, err)
	}
	buildRoot, err := os.MkdirTemp(manager.Root, ".build-")
	if err != nil {
		return EnvironmentResult{}, codedEnvironmentError(EnvCodeUnavailable, err)
	}
	defer os.RemoveAll(buildRoot)
	result, err := manager.build(ctx, buildRoot, info, key, pythonVersion, toolIdentity, interpreterIdentity)
	if err != nil {
		if _, ok := err.(manifestError); ok {
			return EnvironmentResult{}, err
		}
		return EnvironmentResult{}, codedEnvironmentError(EnvCodeBuildFailed, err)
	}
	// Publish atomically: the entry exists only once the complete environment
	// has been built and recorded inside the temporary directory.
	if err := os.Rename(filepath.Join(buildRoot, "entry"), manager.entryDir(key)); err != nil {
		// The rename can only fail because the target already exists (on
		// Windows directory renames surface as generic errors rather than
		// IsExist): a concurrent builder won the race with an identical
		// entry, because the key covers every build input. Anything else is a
		// real publication failure.
		_ = os.RemoveAll(filepath.Join(buildRoot, "entry"))
		if _, ok := manager.loadEntry(key); !ok {
			return EnvironmentResult{}, codedEnvironmentError(EnvCodeBuildFailed, err)
		}
	}
	entry, _ := manager.loadEntry(key)
	if err := manager.addRef(key, taskID, &entry); err != nil {
		if errors.Is(err, errEnvironmentEntryVanished) {
			return EnvironmentResult{}, err
		}
		return EnvironmentResult{}, codedEnvironmentError(EnvCodeUnavailable, err)
	}
	result.CacheHit = false
	result.VenvDir = manager.venvDir(key)
	writeSystem(system, "Local Process environment ready", resultFields(result))
	return result, nil
}

func (manager *EnvironmentManager) build(ctx context.Context, buildRoot string, info *manifestInfo, key, pythonVersion, toolIdentity, interpreterIdentity string) (EnvironmentResult, error) {
	entryRoot := filepath.Join(buildRoot, "entry")
	venv := filepath.Join(entryRoot, "venv")
	staging := filepath.Join(buildRoot, "staging")
	if err := os.MkdirAll(entryRoot, 0o700); err != nil {
		return EnvironmentResult{}, err
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return EnvironmentResult{}, err
	}
	// The manifests are staged from the verified in-memory copies, so the
	// build never depends on workspace layout or lifetime.
	for _, file := range info.Files {
		if err := os.WriteFile(filepath.Join(staging, filepath.Base(file.Path)), file.Content, 0o600); err != nil {
			return EnvironmentResult{}, err
		}
	}
	buildEnv := buildEnvironment(buildRoot)
	resolved, err := manager.install(ctx, info, venv, staging, buildEnv, buildRoot)
	if err != nil {
		return EnvironmentResult{}, err
	}
	paths, hashes := manifestEvidence(info.Files)
	now := manager.now()().UTC()
	entry := cacheEntry{
		SchemaVersion: 1, EnvironmentKey: key, Family: info.Family,
		PythonVersion: pythonVersion, BuilderVersion: manager.builderVersion(),
		BuilderTools: toolIdentity, CreatedAt: now, LastUsedAt: now,
		BestEffort:    info.BestEffort,
		ManifestPaths: paths, ManifestHashes: hashes,
		ResolvedDependencies: resolved,
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return EnvironmentResult{}, err
	}
	// The recorded size covers the entry record itself so capacity based
	// garbage collection accounts for every cached byte.
	entry.SizeBytes, err = directorySize(entryRoot)
	if err != nil {
		return EnvironmentResult{}, err
	}
	entry.SizeBytes += int64(len(data)) + 1
	data, err = json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return EnvironmentResult{}, err
	}
	if err := os.WriteFile(filepath.Join(entryRoot, "entry.json"), append(data, '\n'), 0o600); err != nil {
		return EnvironmentResult{}, err
	}
	return EnvironmentResult{
		EnvironmentKey: key, Provider: "local-process",
		InterpreterIdentity: interpreterIdentity,
		EnvironmentIdentity: "venv:" + key,
		VenvDir:             manager.venvDir(key),
		PythonVersion:       pythonVersion, BuilderVersion: manager.builderVersion(),
		BestEffort: entry.BestEffort, ManifestPaths: paths, ManifestHashes: hashes,
		ResolvedDependencies: resolved,
	}, nil
}

// install creates the virtual environment and applies the frozen install
// strategy of the detected family. The venv contains dependencies only; the
// staged manifest directory acts as the tool working directory, never the
// Project source tree.
func (manager *EnvironmentManager) install(ctx context.Context, info *manifestInfo, venv, staging string, env []string, buildRoot string) ([]string, error) {
	switch info.Family {
	case familyPipLock, familyPipRequirement:
		if err := manager.Runner.Run(ctx, "", env, io.Discard, io.Discard,
			manager.Python, "-m", "venv", venv); err != nil {
			return nil, fmt.Errorf("create virtual environment: %w", err)
		}
		args := []string{venvPython(manager.Python, venv), "-m", "pip", "install",
			"--no-input", "--disable-pip-version-check"}
		if !info.BestEffort {
			args = append(args, "--require-hashes")
		}
		args = append(args, "-r", filepath.Join(staging, filepath.Base(info.PrimaryPath)))
		if err := manager.Runner.Run(ctx, "", env, io.Discard, io.Discard, args...); err != nil {
			return nil, fmt.Errorf("pip install failed: %w", err)
		}
		return manager.listInstalled(ctx, familyUv == info.Family, venv, buildEnvironment(buildRoot), buildRoot)
	case familyUv:
		if err := manager.Runner.Run(ctx, staging, env, io.Discard, io.Discard,
			"uv", "venv", venv, "--python", manager.Python); err != nil {
			return nil, fmt.Errorf("uv venv failed: %w", err)
		}
		syncEnv := append(buildEnvironment(buildRoot),
			"UV_PROJECT_ENVIRONMENT="+venv,
			"UV_PYTHON="+manager.Python,
		)
		// Frozen, no-project install: only the locked dependencies enter the
		// cached environment, never the Project source.
		if err := manager.Runner.Run(ctx, staging, syncEnv, io.Discard, io.Discard,
			"uv", "sync", "--frozen", "--no-install-project", "--no-dev"); err != nil {
			return nil, fmt.Errorf("uv sync failed: %w", err)
		}
		return manager.listInstalled(ctx, true, venv, buildEnvironment(buildRoot), buildRoot)
	case familyPoetry:
		if err := manager.Runner.Run(ctx, "", env, io.Discard, io.Discard,
			manager.Python, "-m", "venv", venv); err != nil {
			return nil, fmt.Errorf("create virtual environment: %w", err)
		}
		poetryEnv := append(buildEnvironment(buildRoot),
			"VIRTUAL_ENV="+venv,
			"POETRY_VIRTUALENVS_CREATE=false",
			"POETRY_NO_INTERACTION=1",
		)
		// poetry verifies the lock file hash, so the installation is frozen;
		// --no-root keeps the Project source out of the environment and
		// --without dev mirrors the uv path by installing dependencies only.
		if err := manager.Runner.Run(ctx, staging, poetryEnv, io.Discard, io.Discard,
			"poetry", "install", "--no-root", "--without", "dev"); err != nil {
			return nil, fmt.Errorf("poetry install failed: %w", err)
		}
		return manager.listInstalled(ctx, false, venv, buildEnvironment(buildRoot), buildRoot)
	case familyPipenv:
		if err := manager.Runner.Run(ctx, "", env, io.Discard, io.Discard,
			manager.Python, "-m", "venv", venv); err != nil {
			return nil, fmt.Errorf("create virtual environment: %w", err)
		}
		exportPath := filepath.Join(buildRoot, "requirements-export.txt")
		exportFile, err := os.OpenFile(exportPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		pipenvEnv := append(buildEnvironment(buildRoot),
			"PIPENV_PIPFILE="+filepath.Join(staging, "Pipfile"),
		)
		exportErr := manager.Runner.Run(ctx, staging, pipenvEnv, exportFile, exportFile,
			"pipenv", "requirements")
		_ = exportFile.Close()
		if exportErr != nil {
			return nil, fmt.Errorf("pipenv requirements export failed: %w", exportErr)
		}
		if err := manager.Runner.Run(ctx, "", buildEnvironment(buildRoot), io.Discard, io.Discard,
			venvPython(manager.Python, venv), "-m", "pip", "install", "--no-input",
			"--disable-pip-version-check", "--require-hashes", "-r", exportPath); err != nil {
			return nil, fmt.Errorf("pip install failed: %w", err)
		}
		return manager.listInstalled(ctx, false, venv, buildEnvironment(buildRoot), buildRoot)
	default:
		return nil, codedEnvironmentError(EnvCodeUnsupported, fmt.Errorf("unsupported environment family %q", info.Family))
	}
}

func (manager *EnvironmentManager) listInstalled(ctx context.Context, useUv bool, venv string, env []string, buildRoot string) ([]string, error) {
	var (
		output []byte
		err    error
	)
	if useUv {
		output, err = manager.Runner.Output(ctx, "", env, "uv", "pip", "freeze", "--python", venvPython(manager.Python, venv))
	} else {
		output, err = manager.Runner.Output(ctx, "", env, venvPython(manager.Python, venv), "-m", "pip", "freeze", "--local")
	}
	if err != nil {
		// The resolved dependency set is required environment evidence; a
		// freeze failure must not produce an environment without evidence.
		return nil, fmt.Errorf("resolve installed dependency set: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	resolved := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			resolved = append(resolved, line)
		}
	}
	return resolved, nil
}

// Release removes a task's durable environment reference. It is idempotent.
func (manager *EnvironmentManager) Release(_ context.Context, taskID string) error {
	if manager == nil || taskID == "" {
		return nil
	}
	manager.entriesMu.Lock()
	defer manager.entriesMu.Unlock()
	changed := false
	entries, err := manager.scanEntries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		refs := removeRef(entry.ActiveRefs, taskID)
		if len(refs) != len(entry.ActiveRefs) {
			entry.ActiveRefs = refs
			if err := manager.writeEntry(entry); err != nil {
				return err
			}
			changed = true
		}
	}
	_ = changed
	return nil
}

// GC removes only entries that have been idle for 96 hours and carry no
// active task references. When a total capacity budget is configured, the
// least recently used unreferenced entries are evicted first.
func (manager *EnvironmentManager) GC(ctx context.Context, now time.Time) error {
	if manager == nil {
		return nil
	}
	_ = ctx
	manager.entriesMu.Lock()
	defer manager.entriesMu.Unlock()
	entries, err := manager.scanEntries()
	if err != nil {
		return err
	}
	var referencedBytes int64
	evictable := make([]cacheEntry, 0, len(entries))
	for _, entry := range entries {
		if len(entry.ActiveRefs) > 0 {
			referencedBytes += entry.SizeBytes
			continue
		}
		if now.Sub(entry.LastUsedAt) > CacheTTL {
			if err := os.RemoveAll(manager.entryDir(entry.EnvironmentKey)); err == nil {
				continue
			}
			// A removal failure keeps the entry for the next cycle.
		}
		evictable = append(evictable, entry)
	}
	if manager.MaxCacheBytes <= 0 {
		return nil
	}
	var total int64
	for _, entry := range evictable {
		total += entry.SizeBytes
	}
	total += referencedBytes
	if total <= manager.MaxCacheBytes {
		return nil
	}
	// LRU: evict the least recently used unreferenced entries until the
	// recorded sizes fit the configured budget again.
	for index := range evictable {
		if total <= manager.MaxCacheBytes {
			break
		}
		oldest := index
		for candidate := index + 1; candidate < len(evictable); candidate++ {
			if evictable[candidate].LastUsedAt.Before(evictable[oldest].LastUsedAt) {
				oldest = candidate
			}
		}
		victim := evictable[oldest]
		evictable[oldest] = evictable[len(evictable)-1]
		evictable = evictable[:len(evictable)-1]
		if err := os.RemoveAll(manager.entryDir(victim.EnvironmentKey)); err == nil {
			total -= victim.SizeBytes
		}
	}
	return nil
}

func (manager *EnvironmentManager) entryDir(key string) string {
	return filepath.Join(manager.Root, key)
}

func (manager *EnvironmentManager) venvDir(key string) string {
	return filepath.Join(manager.Root, key, "venv")
}

func (manager *EnvironmentManager) entryPath(key string) string {
	return filepath.Join(manager.Root, key, "entry.json")
}

func (manager *EnvironmentManager) loadEntry(key string) (cacheEntry, bool) {
	data, err := os.ReadFile(manager.entryPath(key))
	if err != nil {
		return cacheEntry{}, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return cacheEntry{}, false
	}
	if entry.EnvironmentKey != key {
		return cacheEntry{}, false
	}
	if _, err := os.Stat(manager.venvDir(key)); err != nil {
		return cacheEntry{}, false
	}
	return entry, true
}

func (manager *EnvironmentManager) scanEntries() ([]cacheEntry, error) {
	items, err := os.ReadDir(manager.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries := make([]cacheEntry, 0, len(items))
	for _, item := range items {
		if !item.IsDir() || strings.HasPrefix(item.Name(), ".") {
			continue
		}
		if entry, ok := manager.loadEntry(item.Name()); ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (manager *EnvironmentManager) writeEntry(entry cacheEntry) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(manager.entryDir(entry.EnvironmentKey), 0o700); err != nil {
		return err
	}
	temporary := manager.entryPath(entry.EnvironmentKey) + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, manager.entryPath(entry.EnvironmentKey))
}

// errEnvironmentEntryVanished marks the race in which a published or loaded
// cache entry disappears before the task reference is recorded; the caller
// rebuilds once instead of failing the task.
var errEnvironmentEntryVanished = errors.New("environment entry disappeared before the reference was recorded")

// addRef records the task reference and refreshes last_used_at under the
// entry lock so garbage collection never races an active task.
func (manager *EnvironmentManager) addRef(key, taskID string, entry *cacheEntry) error {
	if entry.EnvironmentKey == "" {
		loaded, ok := manager.loadEntry(key)
		if !ok {
			return errEnvironmentEntryVanished
		}
		*entry = loaded
	}
	manager.entriesMu.Lock()
	defer manager.entriesMu.Unlock()
	current, ok := manager.loadEntry(key)
	if !ok {
		return errEnvironmentEntryVanished
	}
	*entry = current
	entry.ActiveRefs = addRef(entry.ActiveRefs, taskID)
	entry.LastUsedAt = manager.now()().UTC()
	return manager.writeEntry(*entry)
}

func (manager *EnvironmentManager) pythonVersion(ctx context.Context) (string, error) {
	output, err := manager.Runner.Output(ctx, "", nil, manager.Python, "-c",
		"import sys; print('%d.%d.%d' % sys.version_info[:3])")
	if err != nil {
		return "", fmt.Errorf("probe Local Process Python interpreter %q: %w", manager.Python, err)
	}
	version := strings.TrimSpace(string(output))
	if version == "" || len(version) > 40 || strings.ContainsAny(version, " \t\n\r\x00") {
		return "", fmt.Errorf("Local Process Python interpreter %q reported an unusable version", manager.Python)
	}
	return version, nil
}

// builderToolIdentity detects the installer version that participates in the
// environment key, so a tool upgrade invalidates cached environments.
func (manager *EnvironmentManager) builderToolIdentity(ctx context.Context, family string) (string, error) {
	switch family {
	case familyPipLock, familyPipRequirement:
		output, err := manager.Runner.Output(ctx, "", nil, manager.Python, "-m", "pip", "--version")
		return "pip:" + firstField(output, err), nil
	case familyUv:
		output, err := manager.Runner.Output(ctx, "", nil, "uv", "--version")
		return "uv:" + firstField(output, err), nil
	case familyPoetry:
		output, err := manager.Runner.Output(ctx, "", nil, "poetry", "--version")
		return "poetry:" + firstField(output, err), nil
	case familyPipenv:
		output, err := manager.Runner.Output(ctx, "", nil, "pipenv", "--version")
		return "pipenv:" + firstField(output, err), nil
	default:
		return "", codedEnvironmentError(EnvCodeUnsupported, fmt.Errorf("unsupported environment family %q", family))
	}
}

func firstField(output []byte, err error) string {
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(output))
	for _, field := range fields {
		if strings.Contains(field, ".") && field[0] >= '0' && field[0] <= '9' {
			return strings.TrimPrefix(field, "v")
		}
	}
	if len(fields) > 0 {
		return fields[len(fields)-1]
	}
	return "unknown"
}

// environmentKey covers every input that can change the built environment:
// interpreter identity (path and detected version), builder strategy, package
// index trust, platform, installer tooling and the exact manifest bytes. The
// interpreter path participates because a virtual environment embeds absolute
// paths to its base interpreter: the same version at a different path must
// never reuse an existing entry.
func environmentKey(interpreterPath, builderVersion, pythonVersion, toolIdentity string, files []manifestFile) string {
	hash := sha256.New()
	for _, part := range []string{
		interpreterPath, builderVersion, PackageIndexConfigVersion, "local-process",
		runtime.GOOS, runtime.GOARCH, pythonVersion, toolIdentity,
	} {
		_, _ = io.WriteString(hash, part)
		_, _ = hash.Write([]byte{0})
	}
	for _, file := range files {
		_, _ = io.WriteString(hash, file.Path)
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(file.Content)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// buildEnvironment gives the installer a minimal, credential-free
// environment: the dependency build also executes package build code, so it
// runs with the same restricted variable set as the task itself.
func buildEnvironment(buildRoot string) []string {
	return append(taskBaseEnvironment("", "", buildRoot), "MMDASH_ENVIRONMENT_BUILD=1")
}

func addRef(refs []string, taskID string) []string {
	for _, ref := range refs {
		if ref == taskID {
			return refs
		}
	}
	return append(refs, taskID)
}

func removeRef(refs []string, taskID string) []string {
	filtered := refs[:0]
	for _, ref := range refs {
		if ref != taskID {
			filtered = append(filtered, ref)
		}
	}
	return filtered
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, item os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() {
			return nil
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func writeSystem(system io.Writer, message string, fields map[string]interface{}) {
	if writer, ok := system.(interface {
		WriteFields([]byte, map[string]interface{}) (int, error)
	}); ok {
		_, _ = writer.WriteFields([]byte(message), fields)
		return
	}
	if system != nil {
		_, _ = fmt.Fprintln(system, message)
	}
}

func resultFields(result EnvironmentResult) map[string]interface{} {
	return map[string]interface{}{
		"environment_key":             result.EnvironmentKey,
		"provider":                    result.Provider,
		"interpreter_identity":        result.InterpreterIdentity,
		"environment_identity":        result.EnvironmentIdentity,
		"cache_hit":                   result.CacheHit,
		"builder_version":             result.BuilderVersion,
		"environment_manifest_paths":  result.ManifestPaths,
		"environment_manifest_hashes": result.ManifestHashes,
		"resolved_dependencies":       result.ResolvedDependencies,
		"best_effort":                 result.BestEffort,
	}
}
