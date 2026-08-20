package localdocker

// This file owns the small, Box-local environment cache used by Local Docker.
// A cache entry contains only dependency manifests and an immutable image
// reference. The transferred repository is deliberately never a Docker build
// context; it remains a read-only bind mount for the execution container.

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

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

const (
	EnvironmentBuilderVersion = "1"
	EnvironmentCacheTTL       = 96 * time.Hour
	PackageIndexConfigVersion = "pip-default-v1"
	maximumDependencyBytes    = int64(4 << 20)
)

// DockerCommandRunner is deliberately narrower than the Docker API. It keeps
// tests independent from a daemon and makes it impossible for this feature to
// smuggle a Docker socket into an execution container.
type DockerCommandRunner interface {
	Run(context.Context, io.Writer, io.Writer, ...string) error
	Output(context.Context, ...string) ([]byte, error)
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, stdout, stderr io.Writer, args ...string) error {
	command := execCommandContext(ctx, "docker", args...)
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func (commandRunner) Output(ctx context.Context, args ...string) ([]byte, error) {
	return execCommandContext(ctx, "docker", args...).CombinedOutput()
}

// execCommandContext is a variable for tests in this package and keeps the
// production runner's dependency on os/exec in one place.
var execCommandContext = osExecCommandContext

func osExecCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// EnvironmentResult is retained by Runtime and surfaced through
// RunResult.ResourceUsage for audit and troubleshooting.
type EnvironmentResult struct {
	EnvironmentKey string
	BaseImageID    string
	ImageID        string
	Image          string
	CacheHit       bool
	Managed        bool
	BuilderVersion string
	ManifestPaths  []string
	ManifestHashes map[string]string
}

type environmentError struct {
	code string
	err  error
}

func (err environmentError) Error() string           { return err.err.Error() }
func (err environmentError) Unwrap() error           { return err.err }
func (err environmentError) EnvironmentCode() string { return err.code }

type dependencyInput struct {
	Path    string
	Content []byte
	Hash    string
}

type environmentEntry struct {
	EnvironmentKey string            `json:"environment_key"`
	BaseImageID    string            `json:"base_image_id"`
	ImageID        string            `json:"image_id"`
	Image          string            `json:"image"`
	BuilderVersion string            `json:"builder_version"`
	CreatedAt      time.Time         `json:"created_at"`
	LastUsedAt     time.Time         `json:"last_used_at"`
	ActiveRefs     []string          `json:"active_refs,omitempty"`
	ManifestPaths  []string          `json:"manifest_paths,omitempty"`
	ManifestHashes map[string]string `json:"manifest_hashes,omitempty"`
}

type environmentState struct {
	SchemaVersion int                         `json:"schema_version"`
	Entries       map[string]environmentEntry `json:"entries"`
}

type environmentFlight struct {
	done chan struct{}
}

// EnvironmentManager builds and caches Box-managed images. It is safe for
// concurrent Gateway tasks; only one Docker build runs for a given key.
type EnvironmentManager struct {
	Root           string
	BaseImage      string
	Runner         DockerCommandRunner
	Now            func() time.Time
	BuilderVersion string

	mu      sync.Mutex
	stateMu sync.Mutex
	flights map[string]*environmentFlight
}

func NewEnvironmentManager(root, baseImage string) *EnvironmentManager {
	return &EnvironmentManager{
		Root:           root,
		BaseImage:      strings.TrimSpace(baseImage),
		Runner:         commandRunner{},
		Now:            func() time.Time { return time.Now().UTC() },
		BuilderVersion: EnvironmentBuilderVersion,
		flights:        map[string]*environmentFlight{},
	}
}

// Prepare implements sandbox.EnvironmentPreparer.
func (manager *EnvironmentManager) Prepare(ctx context.Context, request sandbox.EnvironmentRequest) (EnvironmentResult, error) {
	if manager == nil {
		return EnvironmentResult{}, errors.New("environment manager is nil")
	}
	if request.Workspace == "" || !filepath.IsAbs(request.Workspace) {
		return EnvironmentResult{}, environmentError{code: "ENVIRONMENT_INVALID", err: errors.New("environment workspace must be an absolute path")}
	}
	if strings.TrimSpace(manager.BaseImage) == "" {
		return EnvironmentResult{}, environmentError{code: "ENVIRONMENT_INVALID", err: errors.New("Local Docker base image is required")}
	}
	if manager.Now == nil {
		manager.Now = func() time.Time { return time.Now().UTC() }
	}
	if manager.Runner == nil {
		manager.Runner = commandRunner{}
	}
	// Garbage collection is best effort. A transient inspect/rm failure must
	// not prevent a new experiment from using a valid environment.
	_ = manager.GC(ctx, manager.Now())

	inputs, err := detectDependencyInputs(request.Workspace)
	if err != nil {
		return EnvironmentResult{}, environmentError{code: "ENVIRONMENT_INVALID", err: err}
	}
	baseID, baseErr := manager.imageID(ctx, manager.BaseImage)
	if baseErr != nil {
		return EnvironmentResult{}, environmentError{code: "ENVIRONMENT_UNAVAILABLE", err: baseErr}
	}
	if len(inputs) == 0 {
		key := environmentKey(manager.builderVersion(), manager.BaseImage, baseID, request.Spec, inputs)
		result := EnvironmentResult{
			EnvironmentKey: key, BaseImageID: baseID, ImageID: baseID, Image: manager.BaseImage,
			CacheHit: true, BuilderVersion: manager.builderVersion(),
		}
		manager.writeSystem(request, "using preconfigured Local Docker image", map[string]interface{}{
			"environment_key": key, "base_image_id": baseID, "image_id": baseID,
			"cache_hit": true, "builder_version": manager.builderVersion(),
		})
		return result, nil
	}
	key := environmentKey(manager.builderVersion(), manager.BaseImage, baseID, request.Spec, inputs)
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
		flight := &environmentFlight{done: make(chan struct{})}
		manager.flights[key] = flight
		manager.mu.Unlock()
		result, buildErr := manager.resolveSingleflight(ctx, request, inputs, key, baseID)
		manager.mu.Lock()
		delete(manager.flights, key)
		close(flight.done)
		manager.mu.Unlock()
		return result, buildErr
	}
}

// PrepareEnvironment is the optional Sandbox hook used by Gateway.
func (manager *EnvironmentManager) PrepareEnvironment(ctx context.Context, request sandbox.EnvironmentRequest) error {
	_, err := manager.Prepare(ctx, request)
	return err
}

func (manager *EnvironmentManager) resolveSingleflight(ctx context.Context, request sandbox.EnvironmentRequest, inputs []dependencyInput, key, baseID string) (EnvironmentResult, error) {
	manager.stateMu.Lock()
	state, err := manager.load()
	if err != nil {
		manager.stateMu.Unlock()
		return EnvironmentResult{}, environmentError{code: "ENVIRONMENT_UNAVAILABLE", err: err}
	}
	if entry, ok := state.Entries[key]; ok && entry.ImageID != "" && manager.imageExists(ctx, entry.ImageID) {
		entry.LastUsedAt = manager.Now().UTC()
		entry.ActiveRefs = addRef(entry.ActiveRefs, request.ID)
		state.Entries[key] = entry
		if err := manager.save(state); err != nil {
			manager.stateMu.Unlock()
			return EnvironmentResult{}, environmentError{code: "ENVIRONMENT_UNAVAILABLE", err: err}
		}
		manager.stateMu.Unlock()
		result := EnvironmentResult{EnvironmentKey: key, BaseImageID: entry.BaseImageID, ImageID: entry.ImageID, Image: entry.Image, CacheHit: true, Managed: true, BuilderVersion: entry.BuilderVersion, ManifestPaths: entry.ManifestPaths, ManifestHashes: entry.ManifestHashes}
		manager.writeSystem(request, "reusing cached Local Docker environment", resultFields(result))
		return result, nil
	}
	manager.stateMu.Unlock()

	manager.writeSystem(request, "building Local Docker environment", map[string]interface{}{
		"environment_key": key, "base_image_id": baseID, "cache_hit": false,
		"builder_version": manager.builderVersion(),
	})
	image, imageID, err := manager.build(ctx, request, inputs, key)
	if err != nil {
		// No state is written on a failed build, so a previous valid cache entry
		// remains untouched and the failed image is not considered managed.
		return EnvironmentResult{}, environmentError{code: "ENVIRONMENT_BUILD_FAILED", err: err}
	}
	now := manager.Now().UTC()
	entry := environmentEntry{EnvironmentKey: key, BaseImageID: baseID, ImageID: imageID, Image: image, BuilderVersion: manager.builderVersion(), CreatedAt: now, LastUsedAt: now, ActiveRefs: []string{request.ID}}
	manager.stateMu.Lock()
	state, err = manager.load()
	if err != nil {
		manager.stateMu.Unlock()
		return EnvironmentResult{}, environmentError{code: "ENVIRONMENT_UNAVAILABLE", err: err}
	}
	entry.ManifestPaths, entry.ManifestHashes = manifestEvidence(inputs)
	state.Entries[key] = entry
	if err := manager.save(state); err != nil {
		manager.stateMu.Unlock()
		return EnvironmentResult{}, environmentError{code: "ENVIRONMENT_UNAVAILABLE", err: err}
	}
	manager.stateMu.Unlock()
	result := EnvironmentResult{EnvironmentKey: key, BaseImageID: baseID, ImageID: imageID, Image: image, Managed: true, BuilderVersion: manager.builderVersion()}
	result.ManifestPaths, result.ManifestHashes = manifestEvidence(inputs)
	manager.writeSystem(request, "Local Docker environment ready", resultFields(result))
	return result, nil
}

func (manager *EnvironmentManager) build(ctx context.Context, request sandbox.EnvironmentRequest, inputs []dependencyInput, key string) (string, string, error) {
	root := manager.Root
	if root == "" {
		root = filepath.Join(os.TempDir(), "mmdash-box-environments")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", "", err
	}
	contextRoot, err := os.MkdirTemp(root, ".build-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(contextRoot)
	for _, input := range inputs {
		if err := os.WriteFile(filepath.Join(contextRoot, filepath.Base(input.Path)), input.Content, 0o600); err != nil {
			return "", "", err
		}
	}
	dockerfile, err := generatedDockerfile(manager.BaseImage, inputs)
	if err != nil {
		return "", "", err
	}
	dockerfilePath := filepath.Join(contextRoot, "Dockerfile.mmdash")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o600); err != nil {
		return "", "", err
	}
	image := "mmdash-managed:" + key[:24]
	args := []string{"build", "--pull=false", "--file", dockerfilePath, "--tag", image,
		"--label", "io.mmdash.managed=true", "--label", "io.mmdash.environment_key=" + key,
		"--label", "io.mmdash.builder_version=" + manager.builderVersion(), contextRoot}
	if err := manager.Runner.Run(ctx, request.System, request.System, args...); err != nil {
		return "", "", fmt.Errorf("build Local Docker environment: %w", err)
	}
	imageIDOutput, err := manager.Runner.Output(ctx, "image", "inspect", "--format", "{{.Id}}", image)
	if err != nil {
		return "", "", fmt.Errorf("inspect built Local Docker environment: %w", err)
	}
	imageID := strings.TrimSpace(string(imageIDOutput))
	if imageID == "" {
		return "", "", errors.New("built Local Docker environment has no image ID")
	}
	return image, imageID, nil
}

func generatedDockerfile(baseImage string, inputs []dependencyInput) (string, error) {
	if !validImageReference(baseImage) {
		return "", errors.New("unsafe Local Docker base image reference")
	}
	if len(inputs) != 1 || inputs[0].Path != "requirements.lock" {
		return "", errors.New("unsupported Local Docker dependency manifest")
	}
	manifest := filepath.Base(inputs[0].Path)
	return fmt.Sprintf("FROM %s\n\nENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 PIP_DISABLE_PIP_VERSION_CHECK=1\nWORKDIR /opt/mmdash/environment\nCOPY %s /tmp/mmdash/%s\nRUN python -m pip install --no-cache-dir --require-hashes --target /opt/mmdash/environment -r /tmp/mmdash/%s\nENV PYTHONPATH=/opt/mmdash/environment\n", baseImage, manifest, manifest, manifest), nil
}

func detectDependencyInputs(workspace string) ([]dependencyInput, error) {
	// Keep this detector intentionally explicit. New language ecosystems can
	// add another detector without turning arbitrary repository files into a
	// Docker build context.
	knownUnsupported := []string{
		"requirements.txt", "pyproject.toml", "uv.lock", "package.json", "package-lock.json",
		"pnpm-lock.yaml", "yarn.lock", "go.mod", "go.sum", filepath.Join(".mmdash", "environment.yaml"),
	}
	for _, name := range knownUnsupported {
		if _, err := os.Lstat(filepath.Join(workspace, name)); err == nil {
			return nil, fmt.Errorf("dependency manifest %q is not supported; use requirements.lock", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	for _, name := range []string{"requirements.lock"} {
		path := filepath.Join(workspace, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("dependency manifest %q is not a regular file", name)
		}
		if info.Size() > maximumDependencyBytes {
			return nil, fmt.Errorf("dependency manifest %q exceeds the Box limit", name)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(content)
		return []dependencyInput{{Path: name, Content: content, Hash: hex.EncodeToString(digest[:])}}, nil
	}
	return nil, nil
}

func environmentKey(builderVersion, baseImage, baseID string, spec contracts.RunSpec, inputs []dependencyInput) string {
	hash := sha256.New()
	for _, part := range []string{builderVersion, PackageIndexConfigVersion, baseImage, baseID, spec.Runtime, spec.RuntimeVersion, runtime.GOOS, runtime.GOARCH} {
		_, _ = io.WriteString(hash, part)
		_, _ = hash.Write([]byte{0})
	}
	for _, input := range inputs {
		_, _ = io.WriteString(hash, input.Path)
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(input.Content)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (manager *EnvironmentManager) imageID(ctx context.Context, image string) (string, error) {
	output, err := manager.Runner.Output(ctx, "image", "inspect", "--format", "{{.Id}}", image)
	if err == nil {
		imageID := strings.TrimSpace(string(output))
		if imageID != "" {
			return imageID, nil
		}
		return "", errors.New("Local Docker base image has no image ID")
	}
	return "", fmt.Errorf("inspect Local Docker base image %q failed: %w", image, err)
}

func (manager *EnvironmentManager) imageExists(ctx context.Context, imageID string) bool {
	output, err := manager.Runner.Output(ctx, "image", "inspect", "--format", "{{.Id}}", imageID)
	return err == nil && strings.TrimSpace(string(output)) != ""
}

// Release removes a task's durable reference. It is idempotent and safe to
// call after a task that used the preconfigured base image.
func (manager *EnvironmentManager) Release(_ context.Context, taskID string) error {
	if manager == nil || taskID == "" {
		return nil
	}
	manager.stateMu.Lock()
	defer manager.stateMu.Unlock()
	state, err := manager.load()
	if err != nil {
		return err
	}
	changed := false
	for key, entry := range state.Entries {
		refs := removeRef(entry.ActiveRefs, taskID)
		if len(refs) != len(entry.ActiveRefs) {
			entry.ActiveRefs = refs
			state.Entries[key] = entry
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return manager.save(state)
}

// GC removes only Box-managed image IDs that have been idle for 96 hours and
// have no durable active task references. Tags are never passed to rm.
func (manager *EnvironmentManager) GC(ctx context.Context, now time.Time) error {
	if manager == nil || manager.Runner == nil {
		return nil
	}
	manager.stateMu.Lock()
	defer manager.stateMu.Unlock()
	state, err := manager.load()
	if err != nil {
		return err
	}
	changed := false
	for key, entry := range state.Entries {
		if entry.ImageID == "" || len(entry.ActiveRefs) > 0 || now.Sub(entry.LastUsedAt) <= EnvironmentCacheTTL {
			continue
		}
		if !manager.isManagedImage(ctx, entry.ImageID, key) {
			continue
		}
		if err := manager.Runner.Run(ctx, io.Discard, io.Discard, "image", "rm", entry.ImageID); err != nil {
			continue
		}
		delete(state.Entries, key)
		changed = true
	}
	if changed {
		return manager.save(state)
	}
	return nil
}

func (manager *EnvironmentManager) isManagedImage(ctx context.Context, imageID, environmentKey string) bool {
	output, err := manager.Runner.Output(ctx, "image", "inspect", "--format", `{{.Id}}\t{{index .Config.Labels "io.mmdash.managed"}}\t{{index .Config.Labels "io.mmdash.environment_key"}}`, imageID)
	if err != nil {
		return false
	}
	parts := strings.Split(strings.TrimSpace(string(output)), "\t")
	return len(parts) == 3 && parts[0] == imageID && parts[1] == "true" && parts[2] == environmentKey
}

func (manager *EnvironmentManager) builderVersion() string {
	if strings.TrimSpace(manager.BuilderVersion) == "" {
		return EnvironmentBuilderVersion
	}
	return manager.BuilderVersion
}

func (manager *EnvironmentManager) statePath() string {
	root := manager.Root
	if root == "" {
		root = filepath.Join(os.TempDir(), "mmdash-box-environments")
	}
	return filepath.Join(root, "cache.json")
}

func (manager *EnvironmentManager) load() (environmentState, error) {
	state := environmentState{SchemaVersion: 1, Entries: map[string]environmentEntry{}}
	data, err := os.ReadFile(manager.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("decode Local Docker environment cache: %w", err)
	}
	if state.SchemaVersion != 1 {
		return state, fmt.Errorf("unsupported Local Docker environment cache schema %d", state.SchemaVersion)
	}
	if state.Entries == nil {
		state.Entries = map[string]environmentEntry{}
	}
	return state, nil
}

func (manager *EnvironmentManager) save(state environmentState) error {
	path := manager.statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func (manager *EnvironmentManager) writeSystem(request sandbox.EnvironmentRequest, message string, fields map[string]interface{}) {
	if request.SystemFields != nil {
		_ = request.SystemFields(message, fields)
		return
	}
	if request.System != nil {
		_, _ = fmt.Fprintln(request.System, message)
	}
}

func resultFields(result EnvironmentResult) map[string]interface{} {
	return map[string]interface{}{
		"environment_key": result.EnvironmentKey, "base_image_id": result.BaseImageID,
		"image_id": result.ImageID, "cache_hit": result.CacheHit,
		"builder_version": result.BuilderVersion, "environment_manifest_paths": result.ManifestPaths,
		"environment_manifest_hashes": result.ManifestHashes,
	}
}

func manifestEvidence(inputs []dependencyInput) ([]string, map[string]string) {
	paths := make([]string, 0, len(inputs))
	hashes := make(map[string]string, len(inputs))
	for _, input := range inputs {
		paths = append(paths, input.Path)
		hashes[input.Path] = input.Hash
	}
	return paths, hashes
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

func validImageReference(image string) bool {
	if image == "" || strings.ContainsAny(image, " \t\r\n\\'\"`") {
		return false
	}
	for _, character := range image {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
