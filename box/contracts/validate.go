package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

var (
	shaPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	entryPattern  = regexp.MustCompile(`^[a-z][a-z0-9._-]*:[a-zA-Z0-9_./-]+$`)
)

func (spec RunSpec) Validate() error {
	if spec.SchemaVersion != "2" || !commitPattern.MatchString(spec.SourceCommit) ||
		spec.ExperimentID == "" || spec.ProjectID == "" || spec.ExecutionEpoch == "" ||
		!entryPattern.MatchString(spec.Entrypoint) {
		return errors.New("invalid frozen run specification")
	}
	if spec.Runtime != "local-docker" && spec.Runtime != "e2b" {
		return errors.New("unsupported runtime")
	}
	if spec.RuntimeVersion == "" || spec.SourceTransfer.SourceCommit != spec.SourceCommit ||
		spec.SourceTransfer.ExpiresAt.IsZero() || !spec.SourceTransfer.ExpiresAt.After(time.Now().UTC()) {
		return errors.New("invalid source transfer or runtime version")
	}
	parsed, err := url.ParseRequestURI(spec.SourceTransfer.URL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return errors.New("invalid source transfer URL")
	}
	if spec.ResultContract.BundleFilename != "execution-bundle.zip" ||
		spec.ResultContract.Directory == "" || spec.ResultContract.ManifestSchema == "" ||
		spec.ResultContract.MaxBundleBytes < 1 || spec.ResultContract.MaxBundleBytes > 5<<30 {
		return errors.New("invalid result contract")
	}
	if err := spec.Limits.Validate(); err != nil {
		return err
	}
	return nil
}

func (limits ResourceLimits) Validate() error {
	if limits.CPUMillis < 1 || limits.MemoryBytes < 1<<20 || limits.TimeoutSecond < 1 ||
		limits.DiskBytes < 1<<20 || limits.PIDs < 1 ||
		(limits.Network != "disabled" && limits.Network != "restricted" && limits.Network != "enabled") {
		return errors.New("invalid sandbox resource limits")
	}
	return nil
}

func (manifest Manifest) Validate() error {
	if manifest.SchemaVersion != "2" || manifest.ExperimentID == "" ||
		!commitPattern.MatchString(manifest.SourceCommit) || manifest.ResultDirectory == "" ||
		manifest.StartedAt.IsZero() || manifest.FinishedAt.Before(manifest.StartedAt) ||
		(manifest.Runtime != "local-docker" && manifest.Runtime != "e2b") ||
		manifest.RuntimeVersion == "" ||
		(manifest.Status != "succeeded" && manifest.Status != "failed" && manifest.Status != "canceled" && manifest.Status != "timed_out") {
		return errors.New("invalid manifest header")
	}
	if len(manifest.Files) > 10000 {
		return errors.New("manifest contains too many files")
	}
	seen := map[string]struct{}{}
	for _, file := range manifest.Files {
		if err := ValidateRelativePath(file.Path); err != nil || !shaPattern.MatchString(file.SHA256) || file.Size < 0 || file.Size > 10<<30 {
			return fmt.Errorf("invalid manifest file %q", file.Path)
		}
		if _, exists := seen[file.Path]; exists {
			return fmt.Errorf("duplicate manifest path %q", file.Path)
		}
		seen[file.Path] = struct{}{}
	}
	return nil
}

func ValidateRelativePath(value string) error {
	if value == "" || strings.ContainsRune(value, 0) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\\`) {
		return errors.New("path must be relative")
	}
	clean := path.Clean(strings.ReplaceAll(value, `\\`, "/"))
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("path escapes output directory")
	}
	return nil
}

func SHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
