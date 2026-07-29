package repo

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

const minimumGitVersion = "2.20.0"

// GitChecker verifies that the configured Git binary can start and supports
// the worktree and revision-safety behavior required by Stage 1.
type GitChecker struct {
	Client    *gitcli.Client
	Directory string
}

func (checker GitChecker) Check(ctx context.Context) error {
	if checker.Client == nil || strings.TrimSpace(checker.Directory) == "" {
		return fmt.Errorf("Git readiness is not configured")
	}
	result, err := checker.Client.Run(ctx, gitcli.Command{
		Args: []string{"--version"}, Directory: checker.Directory,
		Operation: "repo.readiness.git",
	})
	if err != nil {
		return err
	}
	return validateGitVersion(string(result.Stdout))
}

func (GitChecker) Name() string { return "git" }

// StorageChecker verifies writable, atomic managed storage.
type StorageChecker struct {
	Storage *gitcli.Storage
}

func (checker StorageChecker) Check(context.Context) error {
	return checker.Storage.Check()
}

func (StorageChecker) Name() string { return "repo_storage" }

func validateGitVersion(output string) error {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return fmt.Errorf("unrecognized Git version output")
	}
	current, err := versionParts(fields[2])
	if err != nil {
		return fmt.Errorf("parse Git version: %w", err)
	}
	minimum, _ := versionParts(minimumGitVersion)
	for index := range minimum {
		if current[index] > minimum[index] {
			return nil
		}
		if current[index] < minimum[index] {
			return fmt.Errorf(
				"Git %s or newer is required; found %s",
				minimumGitVersion,
				fields[2],
			)
		}
	}
	return nil
}

func versionParts(value string) ([3]int, error) {
	var result [3]int
	parts := strings.SplitN(value, ".", 4)
	if len(parts) < 2 {
		return result, fmt.Errorf("version has fewer than two components")
	}
	for index := range result {
		if index >= len(parts) {
			break
		}
		digits := strings.TrimRightFunc(parts[index], func(character rune) bool {
			return character < '0' || character > '9'
		})
		if digits == "" {
			return result, fmt.Errorf("version component is not numeric")
		}
		number, err := strconv.Atoi(digits)
		if err != nil {
			return result, err
		}
		result[index] = number
	}
	return result, nil
}
