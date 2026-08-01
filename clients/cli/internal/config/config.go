package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	CurrentVersion = 1
	DefaultDomain  = "mmdash.moe"
)

type Config struct {
	CoreURL          string `json:"core_url"`
	CurrentProjectID string `json:"current_project_id,omitempty"`
	MCPURL           string `json:"mcp_url"`
	ServerURL        string `json:"server_url"`
	Version          int    `json:"version"`
}

type Paths struct {
	ConfigDir  string
	ConfigFile string
	StateDir   string
}

func ResolvePaths(environment func(string) string, home string, goos string) (Paths, error) {
	if override := strings.TrimSpace(environment("MMDASH_CONFIG_DIR")); override != "" {
		absolute, err := filepath.Abs(override)
		if err != nil {
			return Paths{}, err
		}
		return pathsFromDirs(absolute, absolute), nil
	}
	var configDir, stateDir string
	switch goos {
	case "windows":
		configBase := first(environment("APPDATA"), home)
		stateBase := first(environment("LOCALAPPDATA"), configBase)
		configDir = filepath.Join(configBase, "mmdash")
		stateDir = filepath.Join(stateBase, "mmdash")
	case "darwin":
		configDir = filepath.Join(home, "Library", "Application Support", "mmdash")
		stateDir = configDir
	default:
		configDir = filepath.Join(first(environment("XDG_CONFIG_HOME"), filepath.Join(home, ".config")), "mmdash")
		stateDir = filepath.Join(first(environment("XDG_STATE_HOME"), filepath.Join(home, ".local", "state")), "mmdash")
	}
	return pathsFromDirs(configDir, stateDir), nil
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return ResolvePaths(os.Getenv, home, runtime.GOOS)
}

func Load(paths Paths) (Config, error) {
	config := Default(os.Getenv)
	bytes, err := os.ReadFile(paths.ConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return config, validate(config)
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(bytes, &config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if config.Version != CurrentVersion {
		return Config{}, fmt.Errorf("unsupported config version %d", config.Version)
	}
	applyEnvironment(&config, os.Getenv)
	return config, validate(config)
}

func Save(paths Paths, config Config) error {
	config.Version = CurrentVersion
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	bytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	temporary, err := os.CreateTemp(paths.ConfigDir, ".config-*.json")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(bytes); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, paths.ConfigFile)
}

func Default(environment func(string) string) Config {
	serverURL := first(environment("MMDASH_URL"), "https://"+DefaultDomain)
	config := Config{ServerURL: strings.TrimRight(serverURL, "/"), Version: CurrentVersion}
	config.CoreURL = strings.TrimRight(first(environment("MMDASH_CORE_URL"), config.ServerURL), "/")
	config.MCPURL = first(environment("MMDASH_MCP_URL"), config.ServerURL+"/mcp")
	return config
}

func WithDomain(current Config, value string) (Config, error) {
	origin, err := originForDomain(value)
	if err != nil {
		return Config{}, err
	}
	current.ServerURL = origin
	current.CoreURL = origin
	current.MCPURL = origin + "/mcp"
	current.Version = CurrentVersion
	if err := validate(current); err != nil {
		return Config{}, err
	}
	return current, nil
}

func applyEnvironment(config *Config, environment func(string) string) {
	if value := environment("MMDASH_URL"); value != "" {
		config.ServerURL = strings.TrimRight(value, "/")
	}
	if value := environment("MMDASH_CORE_URL"); value != "" {
		config.CoreURL = strings.TrimRight(value, "/")
	}
	if value := environment("MMDASH_MCP_URL"); value != "" {
		config.MCPURL = value
	}
}

func validate(config Config) error {
	for name, value := range map[string]string{
		"core_url":   config.CoreURL,
		"mcp_url":    config.MCPURL,
		"server_url": config.ServerURL,
	} {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("%s must be an absolute URL without credentials, query, or fragment", name)
		}
		if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
			return fmt.Errorf("%s must use HTTPS, except for loopback development", name)
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func originForDomain(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = DefaultDomain
	}
	candidate := value
	if !strings.Contains(candidate, "://") {
		candidate = "//" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("domain must be a host with an optional port, without credentials, path, query, or fragment")
	}
	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "https"
		if isLoopbackHost(parsed.Hostname()) {
			scheme = "http"
		}
	}
	if scheme != "https" && !(scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return "", fmt.Errorf("domain must use HTTPS, except for loopback development")
	}
	return scheme + "://" + parsed.Host, nil
}

func pathsFromDirs(configDir string, stateDir string) Paths {
	return Paths{ConfigDir: configDir, ConfigFile: filepath.Join(configDir, "config.json"), StateDir: stateDir}
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
