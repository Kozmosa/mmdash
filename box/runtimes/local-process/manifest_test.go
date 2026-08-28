package localprocess

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeWorkspaceFile(t *testing.T, workspace, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(workspace, name)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDetectManifestFamilies(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		family   string
		primary  string
		missing  bool // no manifest at all
		bestEff  bool
	}{
		{name: "pip lock", files: map[string]string{"requirements.lock": "numpy==1.0.0 --hash=sha256:aaa\n"}, family: familyPipLock, primary: "requirements.lock"},
		{name: "pip requirements", files: map[string]string{"requirements.txt": "numpy\n"}, family: familyPipRequirement, primary: "requirements.txt", bestEff: true},
		{name: "pinned pip requirements", files: map[string]string{"requirements.txt": "numpy==1.0.0 --hash=sha256:aaa\n"}, family: familyPipRequirement, primary: "requirements.txt"},
		{name: "uv lock", files: map[string]string{
			"pyproject.toml": "[project]\nname='x'\n",
			"uv.lock":        "version = 1\n",
		}, family: familyUv, primary: "uv.lock"},
		{name: "poetry lock", files: map[string]string{
			"pyproject.toml": "[tool.poetry]\nname='x'\n",
			"poetry.lock":    "[[package]]\n",
		}, family: familyPoetry, primary: "poetry.lock"},
		{name: "pipenv lock", files: map[string]string{
			"Pipfile":      "[packages]\n",
			"Pipfile.lock": "{\"default\": {}}\n",
		}, family: familyPipenv, primary: "Pipfile"},
		{name: "pyproject without lock is not a family", files: map[string]string{"pyproject.toml": "[project]\n"}, missing: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			workspace := t.TempDir()
			for name, content := range testCase.files {
				writeWorkspaceFile(t, workspace, name, content)
			}
			info, err := detectManifest(workspace, nil)
			if testCase.missing {
				if err != nil || info != nil {
					t.Fatalf("expected no manifest, got %v, %v", info, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if info.Family != testCase.family || info.PrimaryPath != testCase.primary {
				t.Fatalf("detected %s/%s, want %s/%s", info.Family, info.PrimaryPath, testCase.family, testCase.primary)
			}
			if info.BestEffort != testCase.bestEff {
				t.Fatalf("best effort %v, want %v", info.BestEffort, testCase.bestEff)
			}
		})
	}
}

func TestDetectManifestAmbiguous(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceFile(t, workspace, "requirements.txt", "numpy\n")
	writeWorkspaceFile(t, workspace, "requirements.lock", "numpy==1.0.0 --hash=sha256:aaa\n")
	_, err := detectManifest(workspace, nil)
	var coded manifestError
	if !errors.As(err, &coded) || coded.EnvironmentCode() != EnvCodeAmbiguous {
		t.Fatalf("expected ENVIRONMENT_MANIFEST_AMBIGUOUS, got %v", err)
	}
	// An explicit frozen RunSpec selection resolves the ambiguity.
	info, err := detectManifest(workspace, map[string]string{"python_manifest": "requirements.lock"})
	if err != nil || info.Family != familyPipLock {
		t.Fatalf("explicit selection failed: %v, %v", info, err)
	}
	// A selection that matches no detected family stays invalid.
	_, err = detectManifest(workspace, map[string]string{"python_manifest": "requirements.in"})
	var invalid manifestError
	if !errors.As(err, &invalid) || invalid.EnvironmentCode() != EnvCodeInvalid {
		t.Fatalf("expected ENVIRONMENT_INVALID, got %v", err)
	}
}

func TestDetectManifestCondaUnsupported(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceFile(t, workspace, "environment.yml", "name: x\ndependencies: []\n")
	_, err := detectManifest(workspace, nil)
	var coded manifestError
	if !errors.As(err, &coded) || coded.EnvironmentCode() != EnvCodeUnsupported {
		t.Fatalf("expected ENVIRONMENT_MANIFEST_UNSUPPORTED, got %v", err)
	}
}

func TestFullyHashPinned(t *testing.T) {
	cases := []struct {
		content string
		pinned  bool
	}{
		{"", false},
		{"# only a comment\n", false},
		{"numpy==1.0.0 --hash=sha256:aaa\nscipy==1.2 --hash=sha256:bbb\n", true},
		{"numpy==1.0.0 --hash=sha256:aaa\nscipy\n", false},
		{"--index-url https://example.invalid/simple\nnumpy==1.0.0 --hash=sha256:aaa\n", true},
	}
	for _, testCase := range cases {
		if got := fullyHashPinned([]byte(testCase.content)); got != testCase.pinned {
			t.Fatalf("fullyHashPinned(%q) = %v, want %v", testCase.content, got, testCase.pinned)
		}
	}
}
