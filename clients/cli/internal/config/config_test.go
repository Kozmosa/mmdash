package config

import (
	"path/filepath"
	"testing"
)

func TestResolvePathsAcrossPlatforms(t *testing.T) {
	environment := func(key string) string {
		return map[string]string{
			"APPDATA":         "C:/Users/test/AppData/Roaming",
			"LOCALAPPDATA":    "C:/Users/test/AppData/Local",
			"XDG_CONFIG_HOME": "/tmp/config",
			"XDG_STATE_HOME":  "/tmp/state",
		}[key]
	}
	windows, err := ResolvePaths(environment, "C:/Users/test", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(windows.ConfigDir) != "mmdash" || filepath.Base(windows.ConfigFile) != "config.json" {
		t.Fatalf("unexpected Windows paths: %#v", windows)
	}
	windowsFallback, err := ResolvePaths(func(string) string { return "" }, "C:/Users/test", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if windowsFallback.StateDir != filepath.Join("C:/Users/test", "mmdash") {
		t.Fatalf("unexpected Windows fallback paths: %#v", windowsFallback)
	}
	linux, err := ResolvePaths(environment, "/home/test", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if linux.ConfigDir != filepath.Join("/tmp/config", "mmdash") || linux.StateDir != filepath.Join("/tmp/state", "mmdash") {
		t.Fatalf("unexpected Linux paths: %#v", linux)
	}
	darwin, err := ResolvePaths(func(string) string { return "" }, "/Users/test", "darwin")
	if err != nil {
		t.Fatal(err)
	}
	if darwin.ConfigDir != filepath.Join("/Users/test", "Library", "Application Support", "mmdash") {
		t.Fatalf("unexpected macOS paths: %#v", darwin)
	}
}

func TestSaveUsesVersionedNonSecretConfig(t *testing.T) {
	clearURLOverrides(t)
	directory := t.TempDir()
	paths := pathsFromDirs(directory, directory)
	value := Config{CoreURL: "https://example.test", MCPURL: "https://example.test/mcp", ServerURL: "https://example.test", CurrentProjectID: "project-1"}
	if err := Save(paths, value); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != CurrentVersion || loaded.CurrentProjectID != "project-1" {
		t.Fatalf("unexpected config: %#v", loaded)
	}
}

func TestLoadRejectsPlainHTTPOutsideLoopback(t *testing.T) {
	clearURLOverrides(t)
	directory := t.TempDir()
	paths := pathsFromDirs(directory, directory)
	value := Config{CoreURL: "http://example.test", MCPURL: "https://example.test/mcp", ServerURL: "https://example.test"}
	if err := Save(paths, value); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(paths); err == nil {
		t.Fatal("expected non-loopback HTTP Core URL to be rejected")
	}
}

func TestLoadAllowsLoopbackHTTPDevelopment(t *testing.T) {
	clearURLOverrides(t)
	directory := t.TempDir()
	paths := pathsFromDirs(directory, directory)
	value := Config{CoreURL: "http://127.0.0.1:8080", MCPURL: "http://localhost:3002/mcp", ServerURL: "http://dev.localhost:3000"}
	if err := Save(paths, value); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(paths); err != nil {
		t.Fatalf("expected loopback development URLs to be accepted: %v", err)
	}
}

func TestWithDomainUsesTheHostedDefault(t *testing.T) {
	updated, err := WithDomain(Config{CurrentProjectID: "project-1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ServerURL != "https://"+DefaultDomain || updated.CoreURL != updated.ServerURL || updated.MCPURL != updated.ServerURL+"/mcp" {
		t.Fatalf("unexpected hosted endpoints: %#v", updated)
	}
	if updated.CurrentProjectID != "project-1" {
		t.Fatal("changing the domain must preserve the current Project")
	}
}

func TestWithDomainAllowsLoopbackDevelopment(t *testing.T) {
	updated, err := WithDomain(Config{}, "localhost:3000")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ServerURL != "http://localhost:3000" || updated.CoreURL != updated.ServerURL || updated.MCPURL != "http://localhost:3000/mcp" {
		t.Fatalf("unexpected loopback endpoints: %#v", updated)
	}
}

func TestWithDomainRejectsPathsAndInsecureRemoteOrigins(t *testing.T) {
	for _, value := range []string{"example.test/path", "http://example.test"} {
		if _, err := WithDomain(Config{}, value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func clearURLOverrides(t *testing.T) {
	t.Helper()
	for _, name := range []string{"MMDASH_URL", "MMDASH_CORE_URL", "MMDASH_MCP_URL"} {
		t.Setenv(name, "")
	}
}
