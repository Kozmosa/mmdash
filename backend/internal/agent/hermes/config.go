package hermes

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

const (
	AdapterKey = "hermes"

	ConfigRuntimeURL             = "runtime_url"
	ConfigAPIKey                 = "api_key"
	ConfigProfile                = "profile"
	ConfigManagementURL          = "management_url"
	ConfigDashboardSessionToken  = "dashboard_session_token"
	ConfigCloudflareClientID     = "cloudflare_access_client_id"
	ConfigCloudflareClientSecret = "cloudflare_access_client_secret"
	ConfigRequestTimeoutSeconds  = "request_timeout_seconds"

	// TestedVersion and TestedCommit pin the upstream contract implemented by
	// this adapter. Compatibility is still capability-probed at runtime.
	TestedVersion = "v2026.8.3"
	TestedCommit  = "3c27eb6234bf91b8ceee9e9071591b31e9b148cb"
)

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// NetworkPolicy is deployment policy, not user-controlled runtime settings.
// Metadata and link-local addresses remain denied even when private access is
// enabled.
type NetworkPolicy struct {
	AllowLoopback         bool
	AllowPrivate          bool
	AllowedPorts          []int
	ConnectTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	RequestTimeout        time.Duration
	MaxResponseBytes      int64
	MaxRedirects          int
	Resolver              Resolver
	Dialer                Dialer
}

type ManagementConfig struct {
	URL                    string
	DashboardSessionToken  string
	CloudflareClientID     string
	CloudflareClientSecret string
}

type Config struct {
	InstanceID                string
	RuntimeURL                string
	APIKey                    string
	Profile                   string
	Management                *ManagementConfig
	RuntimePolicy             NetworkPolicy
	ManagementPolicy          NetworkPolicy
	ManagementMinimumInterval time.Duration
}

type FactoryOptions struct {
	RuntimePolicy             NetworkPolicy
	ManagementPolicy          NetworkPolicy
	ManagementMinimumInterval time.Duration
}

func Descriptor() agent.Descriptor {
	return agent.Descriptor{
		Key:         AdapterKey,
		DisplayName: "Hermes",
		Capabilities: agent.DeclaredCapabilities{
			ProjectAccess: agent.ProjectAccessCapabilities{
				Verify: true, Configure: true, Rotate: true,
			},
		},
	}
}

func Register(registry *agent.Registry, options FactoryOptions) error {
	if registry == nil {
		return agent.ErrInvalidArgument
	}
	return registry.Register(Descriptor(), NewFactory(options))
}

// NewFactory turns opaque per-instance values into an isolated Hermes
// adapter. Network policy is captured from deployment configuration and cannot
// be weakened by values stored on an Agent instance.
func NewFactory(options FactoryOptions) agent.Factory {
	return func(_ context.Context, opaque agent.AdapterConfig) (agent.Adapter, error) {
		values := opaque.Values
		runtimePolicy := options.RuntimePolicy
		managementPolicy := options.ManagementPolicy
		if rawTimeout := strings.TrimSpace(values[ConfigRequestTimeoutSeconds]); rawTimeout != "" {
			seconds, err := strconv.Atoi(rawTimeout)
			if err != nil || seconds < 1 || seconds > 300 {
				return nil, fmt.Errorf("%w: invalid request timeout", agent.ErrInvalidArgument)
			}
			requested := time.Duration(seconds) * time.Second
			runtimePolicy = narrowRequestTimeout(runtimePolicy, requested)
			managementPolicy = narrowRequestTimeout(managementPolicy, requested)
		}
		config := Config{
			InstanceID: strings.TrimSpace(opaque.InstanceID),
			RuntimeURL: strings.TrimSpace(values[ConfigRuntimeURL]),
			APIKey:     values[ConfigAPIKey],
			// Profile identifiers are canonical and must not be silently
			// normalized at an adapter ingress. An omitted setting remains the
			// unscoped/default Hermes profile.
			Profile:                   values[ConfigProfile],
			RuntimePolicy:             runtimePolicy,
			ManagementPolicy:          managementPolicy,
			ManagementMinimumInterval: options.ManagementMinimumInterval,
		}
		if managementURL := strings.TrimSpace(values[ConfigManagementURL]); managementURL != "" {
			config.Management = &ManagementConfig{
				URL:                    managementURL,
				DashboardSessionToken:  values[ConfigDashboardSessionToken],
				CloudflareClientID:     values[ConfigCloudflareClientID],
				CloudflareClientSecret: values[ConfigCloudflareClientSecret],
			}
		}
		return New(config)
	}
}

func narrowRequestTimeout(policy NetworkPolicy, requested time.Duration) NetworkPolicy {
	maximum := policy.RequestTimeout
	if maximum <= 0 {
		maximum = 30 * time.Second
	}
	policy.RequestTimeout = min(requested, maximum)
	return policy
}

func normalizePolicy(policy NetworkPolicy) (NetworkPolicy, error) {
	if len(policy.AllowedPorts) == 0 {
		policy.AllowedPorts = []int{80, 443, 8642, 9119}
	}
	seen := make(map[int]struct{}, len(policy.AllowedPorts))
	for _, port := range policy.AllowedPorts {
		if port < 1 || port > 65535 {
			return NetworkPolicy{}, fmt.Errorf("%w: invalid connector port %s", agent.ErrInvalidArgument, strconv.Itoa(port))
		}
		seen[port] = struct{}{}
	}
	policy.AllowedPorts = policy.AllowedPorts[:0]
	for port := range seen {
		policy.AllowedPorts = append(policy.AllowedPorts, port)
	}
	if policy.ConnectTimeout <= 0 {
		policy.ConnectTimeout = 5 * time.Second
	}
	if policy.ResponseHeaderTimeout <= 0 {
		policy.ResponseHeaderTimeout = 10 * time.Second
	}
	if policy.RequestTimeout <= 0 {
		policy.RequestTimeout = 30 * time.Second
	}
	if policy.MaxResponseBytes <= 0 {
		policy.MaxResponseBytes = 4 << 20
	}
	if policy.MaxRedirects <= 0 {
		policy.MaxRedirects = 3
	}
	if policy.Resolver == nil {
		policy.Resolver = net.DefaultResolver
	}
	if policy.Dialer == nil {
		policy.Dialer = &net.Dialer{Timeout: policy.ConnectTimeout, KeepAlive: -1}
	}
	return policy, nil
}
