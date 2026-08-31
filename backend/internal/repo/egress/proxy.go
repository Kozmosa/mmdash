// Package egress owns the deployment-scoped outbound proxy policy used only
// by the Repo GitHub provider and its Git HTTPS subprocesses.
package egress

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidConfig = errors.New("repository GitHub proxy configuration is invalid")

const (
	connectTimeout        = 10 * time.Second
	requestTimeout        = 30 * time.Second
	responseHeaderTimeout = 15 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
)

// Config is a validated, secret-bearing Repo egress configuration. Its fields
// stay private so accidental structured logging cannot serialize proxy
// credentials.
type Config struct {
	bypass    []bypassRule
	noProxy   string
	proxyURL  *url.URL
	rawURL    string
	sensitive []string
}

type bypassRule struct {
	host   string
	prefix netip.Prefix
}

// Parse validates the optional Repo GitHub proxy and its narrowly-scoped
// bypass list. Only HTTP(S) proxies are supported; SOCKS DNS behavior is not
// accepted implicitly.
func Parse(rawProxyURL, rawNoProxy string) (Config, error) {
	rawProxyURL = strings.TrimSpace(rawProxyURL)
	config := Config{}
	if rawProxyURL != "" {
		parsed, err := url.Parse(rawProxyURL)
		if err != nil || parsed.Opaque != "" || parsed.Host == "" ||
			parsed.Hostname() == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") ||
			(parsed.Path != "" && parsed.Path != "/") ||
			parsed.RawPath != "" || parsed.ForceQuery || parsed.RawQuery != "" ||
			parsed.Fragment != "" {
			return Config{}, ErrInvalidConfig
		}
		if parsed.User != nil && parsed.User.Username() == "" {
			return Config{}, ErrInvalidConfig
		}
		if port := parsed.Port(); port != "" {
			value, err := strconv.Atoi(port)
			if err != nil || value < 1 || value > 65535 {
				return Config{}, ErrInvalidConfig
			}
		}
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		config.proxyURL = parsed
		config.rawURL = rawProxyURL
		config.sensitive = proxySensitiveValues(rawProxyURL, parsed)
	}

	rules, normalized, err := parseNoProxy(rawNoProxy)
	if err != nil {
		return Config{}, err
	}
	config.bypass = rules
	config.noProxy = normalized
	return config, nil
}

// Enabled reports whether Repo GitHub traffic must use the configured proxy.
func (config Config) Enabled() bool {
	return config.proxyURL != nil
}

// HTTPClient creates the dedicated GitHub metadata client. It never reads
// process-wide proxy variables and keeps normal TLS and hostname verification.
func (config Config) HTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	transport.TLSHandshakeTimeout = tlsHandshakeTimeout
	if config.proxyURL != nil {
		transport.Proxy = config.proxyForRequest
	}
	return &http.Client{Transport: transport, Timeout: requestTimeout}
}

// GitEnvironment returns only the reviewed variables injected into one Git
// subprocess. Callers cannot add arbitrary environment keys through this API.
func (config Config) GitEnvironment() map[string]string {
	if config.proxyURL == nil {
		return nil
	}
	values := map[string]string{
		"HTTP_PROXY":  config.rawURL,
		"HTTPS_PROXY": config.rawURL,
		"http_proxy":  config.rawURL,
		"https_proxy": config.rawURL,
	}
	if config.noProxy != "" {
		values["NO_PROXY"] = config.noProxy
		values["no_proxy"] = config.noProxy
	}
	return values
}

// SensitiveValues returns copies of credential-bearing values that command
// output redaction must remove.
func (config Config) SensitiveValues() []string {
	return append([]string(nil), config.sensitive...)
}

func (config Config) proxyForRequest(request *http.Request) (*url.URL, error) {
	if config.proxyURL == nil || config.bypasses(request.URL.Hostname()) {
		return nil, nil
	}
	clone := *config.proxyURL
	return &clone, nil
}

func (config Config) bypasses(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	address, addressErr := netip.ParseAddr(host)
	for _, rule := range config.bypass {
		if rule.prefix.IsValid() && addressErr == nil && rule.prefix.Contains(address) {
			return true
		}
		if rule.host != "" && (host == rule.host || strings.HasSuffix(host, "."+rule.host)) {
			return true
		}
	}
	return false
}

func proxySensitiveValues(raw string, parsed *url.URL) []string {
	values := []string{raw, parsed.String()}
	if parsed.User != nil {
		values = append(values, parsed.User.String(), parsed.User.Username())
		if password, ok := parsed.User.Password(); ok {
			values = append(values, password, url.QueryEscape(password), url.PathEscape(password))
		}
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func parseNoProxy(raw string) ([]bypassRule, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", nil
	}
	parts := strings.Split(raw, ",")
	rules := make([]bypassRule, 0, len(parts))
	normalized := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" || strings.Contains(token, "*") {
			return nil, "", ErrInvalidConfig
		}
		host, err := noProxyHost(token)
		if err != nil {
			return nil, "", err
		}
		host = strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(host), "."), ".")
		if host == "" || strings.Contains(host, "%") {
			return nil, "", ErrInvalidConfig
		}
		rule := bypassRule{}
		if prefix, err := netip.ParsePrefix(host); err == nil {
			prefix = prefix.Masked()
			if !internalPrefix(prefix) {
				return nil, "", ErrInvalidConfig
			}
			rule.prefix = prefix
		} else if address, err := netip.ParseAddr(host); err == nil {
			if !internalAddress(address) {
				return nil, "", ErrInvalidConfig
			}
			rule.prefix = netip.PrefixFrom(address, address.BitLen())
		} else {
			if !internalHostname(host) {
				return nil, "", ErrInvalidConfig
			}
			rule.host = host
		}
		if !seen[token] {
			seen[token] = true
			rules = append(rules, rule)
			normalized = append(normalized, token)
		}
	}
	return rules, strings.Join(normalized, ","), nil
}

func noProxyHost(token string) (string, error) {
	if strings.Contains(token, "/") {
		return token, nil
	}
	if strings.HasPrefix(token, "[") {
		if strings.HasSuffix(token, "]") {
			return strings.TrimSuffix(strings.TrimPrefix(token, "["), "]"), nil
		}
		host, port, err := net.SplitHostPort(token)
		if err != nil || !validPort(port) {
			return "", ErrInvalidConfig
		}
		return host, nil
	}
	if strings.Count(token, ":") == 1 {
		host, port, err := net.SplitHostPort(token)
		if err == nil {
			if !validPort(port) {
				return "", ErrInvalidConfig
			}
			return host, nil
		}
	}
	return token, nil
}

func validPort(raw string) bool {
	port, err := strconv.Atoi(raw)
	return err == nil && port >= 1 && port <= 65535
}

func internalHostname(host string) bool {
	if host == "localhost" || !strings.Contains(host, ".") {
		return validHostname(host)
	}
	for _, suffix := range []string{".internal", ".local", ".localhost"} {
		if strings.HasSuffix(host, suffix) {
			return validHostname(host)
		}
	}
	return false
}

func validHostname(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') &&
				(character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func internalAddress(address netip.Addr) bool {
	return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast()
}

func internalPrefix(prefix netip.Prefix) bool {
	if !prefix.IsValid() {
		return false
	}
	for _, raw := range []string{
		"10.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.168.0.0/16", "::1/128",
		"fc00::/7", "fe80::/10",
	} {
		container := netip.MustParsePrefix(raw)
		if container.Addr().BitLen() == prefix.Addr().BitLen() &&
			container.Bits() <= prefix.Bits() && container.Contains(prefix.Addr()) {
			return true
		}
	}
	return false
}
