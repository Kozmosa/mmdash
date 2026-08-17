// Package config owns the user-editable Box configuration. Keeping this
// schema in the Box module lets the mbox commands and the Gateway share one
// on-disk contract without introducing a second service or a platform-specific
// registry store.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const schemaVersion = 1

type Config struct {
	SchemaVersion int         `json:"schema_version"`
	ControlURL    string      `json:"control_url"`
	Name          string      `json:"name"`
	LocalDocker   LocalDocker `json:"local_docker"`
	E2B           E2B         `json:"e2b"`
}

type LocalDocker struct {
	Enabled bool   `json:"enabled"`
	Image   string `json:"image"`
}

type E2B struct {
	Enabled    bool   `json:"enabled"`
	APIKey     string `json:"api_key,omitempty"`
	Domain     string `json:"domain"`
	APIURL     string `json:"api_url,omitempty"`
	SandboxURL string `json:"sandbox_url,omitempty"`
	Template   string `json:"template"`
	User       string `json:"user"`
	AdminUser  string `json:"admin_user"`
}

func Default(root string) Config {
	return Config{
		SchemaVersion: schemaVersion,
		ControlURL:    "https://mmdash.moe",
		Name:          "local-box",
		LocalDocker: LocalDocker{
			Enabled: true,
			Image:   "mmdash/sandbox:latest",
		},
		E2B: E2B{
			Enabled:   false,
			Domain:    "e2b.app",
			Template:  "base",
			User:      "user",
			AdminUser: "root",
		},
	}
}

func DefaultRoot() string {
	if value := strings.TrimSpace(os.Getenv("MMDASH_BOX_DATA_ROOT")); value != "" {
		return value
	}
	if runtime.GOOS == "windows" {
		if value := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); value != "" {
			return filepath.Join(value, "MMDash Box")
		}
	}
	if value := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); value != "" {
		return filepath.Join(value, "mmdash-box")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "mmdash-box")
	}
	return filepath.Join(os.TempDir(), "mmdash-box")
}

func Path(root string) string {
	return filepath.Join(root, "config.json")
}

func Load(root string) (Config, error) {
	config := Default(root)
	data, err := os.ReadFile(Path(root))
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("decode Box configuration: %w", err)
	}
	if config.SchemaVersion == 0 {
		config.SchemaVersion = schemaVersion
	}
	if config.SchemaVersion != schemaVersion {
		return config, fmt.Errorf("unsupported Box configuration schema %d", config.SchemaVersion)
	}
	if err := Validate(config); err != nil {
		return config, err
	}
	return config, nil
}

func Save(root string, config Config) error {
	if err := Validate(config); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	config.SchemaVersion = schemaVersion
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temporary := Path(root) + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, Path(root)); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func Validate(config Config) error {
	if strings.TrimSpace(config.ControlURL) == "" {
		return errors.New("Box control URL is required")
	}
	parsed, err := url.Parse(strings.TrimSpace(config.ControlURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("Box control URL must be an absolute http or https URL")
	}
	if strings.TrimSpace(config.Name) == "" {
		return errors.New("Box name is required")
	}
	if !config.LocalDocker.Enabled && !config.E2B.Enabled {
		return errors.New("at least one Runtime must be enabled")
	}
	if config.LocalDocker.Enabled && strings.TrimSpace(config.LocalDocker.Image) == "" {
		return errors.New("Local Docker image is required when Local Docker is enabled")
	}
	if config.E2B.Enabled && strings.TrimSpace(config.E2B.APIKey) == "" {
		return errors.New("E2B API key is required when E2B is enabled")
	}
	return nil
}
