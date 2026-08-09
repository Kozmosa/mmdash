package hermes

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

var (
	cgnatNetwork = mustCIDR("100.64.0.0/10")
	metadataIPv4 = net.ParseIP("169.254.169.254")
	metadataIPv6 = net.ParseIP("fd00:ec2::254")
)

type connector struct {
	base   *url.URL
	policy NetworkPolicy
	client *http.Client
}

func newConnector(rawBase string, policy NetworkPolicy) (*connector, error) {
	normalized, err := normalizePolicy(policy)
	if err != nil {
		return nil, err
	}
	base, err := parseConnectorURL(rawBase, normalized)
	if err != nil {
		return nil, err
	}
	result := &connector{base: base, policy: normalized}
	result.client = &http.Client{
		Transport: &safeRoundTripper{policy: normalized},
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= normalized.MaxRedirects {
				return &agent.AdapterError{Code: agent.ErrorProtocol, Operation: "redirect", Message: "too many redirects"}
			}
			if len(previous) == 0 || !sameOrigin(previous[0].URL, request.URL) {
				return &agent.AdapterError{Code: agent.ErrorPermission, Operation: "redirect", Message: "cross-origin redirect rejected"}
			}
			return validateTargetURL(request.URL, normalized)
		},
	}
	return result, nil
}

func parseConnectorURL(raw string, policy NetworkPolicy) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return nil, fmt.Errorf("%w: invalid connector URL", agent.ErrInvalidArgument)
	}
	if err := validateTargetURL(parsed, policy); err != nil {
		return nil, err
	}
	if parsed.RawQuery != "" {
		return nil, fmt.Errorf("%w: connector base URL cannot contain a query", agent.ErrInvalidArgument)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed, nil
}

func validateTargetURL(target *url.URL, policy NetworkPolicy) error {
	if target == nil || target.Host == "" || target.Scheme != "http" && target.Scheme != "https" {
		return fmt.Errorf("%w: connector requires http or https", agent.ErrInvalidArgument)
	}
	if target.User != nil || target.Fragment != "" {
		return fmt.Errorf("%w: connector URL cannot contain credentials or a fragment", agent.ErrInvalidArgument)
	}
	if strings.ContainsAny(target.Host, "\r\n\x00") {
		return fmt.Errorf("%w: invalid connector host", agent.ErrInvalidArgument)
	}
	if strings.Contains(target.Hostname(), "%") {
		return fmt.Errorf("%w: scoped IPv6 connector addresses are not supported", agent.ErrInvalidArgument)
	}
	port, err := targetPort(target)
	if err != nil {
		return err
	}
	allowed := false
	for _, candidate := range policy.AllowedPorts {
		if candidate == port {
			allowed = true
			break
		}
	}
	if !allowed {
		return &agent.AdapterError{Code: agent.ErrorPermission, Operation: "connector", Message: "target port is not allowed"}
	}
	return nil
}

func targetPort(target *url.URL) (int, error) {
	if explicit := target.Port(); explicit != "" {
		port, err := strconv.Atoi(explicit)
		if err != nil || port < 1 || port > 65535 {
			return 0, fmt.Errorf("%w: invalid connector port", agent.ErrInvalidArgument)
		}
		return port, nil
	}
	if target.Scheme == "https" {
		return 443, nil
	}
	return 80, nil
}

func (safe *connector) endpoint(path string, query url.Values) *url.URL {
	result := *safe.base
	basePath := strings.TrimRight(safe.base.Path, "/")
	result.Path = basePath + "/" + strings.TrimLeft(path, "/")
	result.RawPath = ""
	result.RawQuery = query.Encode()
	return &result
}

type safeRoundTripper struct {
	policy NetworkPolicy
}

func (roundTripper *safeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, fmt.Errorf("%w: nil request", agent.ErrInvalidArgument)
	}
	if err := validateTargetURL(request.URL, roundTripper.policy); err != nil {
		return nil, err
	}
	addresses, err := roundTripper.resolve(request.Context(), request.URL.Hostname())
	if err != nil {
		return nil, err
	}
	if request.URL.Scheme == "http" {
		for _, address := range addresses {
			if !address.IsLoopback() && !address.IsPrivate() {
				return nil, &agent.AdapterError{Code: agent.ErrorPermission, Operation: "connector", Message: "unencrypted public target rejected"}
			}
		}
	}
	port, err := targetPort(request.URL)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		Proxy:                 nil,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     false,
		ResponseHeaderTimeout: roundTripper.policy.ResponseHeaderTimeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var dialErrors []error
		for _, address := range addresses {
			connection, dialErr := roundTripper.policy.Dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), strconv.Itoa(port)))
			if dialErr == nil {
				return connection, nil
			}
			dialErrors = append(dialErrors, dialErr)
		}
		return nil, errors.Join(dialErrors...)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, normalizeNetworkError("request", err)
	}
	response.Body = &transportBody{ReadCloser: response.Body, transport: transport}
	return response, nil
}

type transportBody struct {
	io.ReadCloser
	transport *http.Transport
}

func (body *transportBody) Close() error {
	err := body.ReadCloser.Close()
	body.transport.CloseIdleConnections()
	return err
}

func (roundTripper *safeRoundTripper) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if literal := net.ParseIP(host); literal != nil {
		if err := validateIP(literal, roundTripper.policy); err != nil {
			return nil, err
		}
		return []net.IP{literal}, nil
	}
	resolved, err := roundTripper.policy.Resolver.LookupIPAddr(ctx, host)
	if err != nil || len(resolved) == 0 {
		return nil, &agent.AdapterError{Code: agent.ErrorUnavailable, Operation: "resolve", Message: "target resolution failed", Retryable: true}
	}
	addresses := make([]net.IP, 0, len(resolved))
	for _, item := range resolved {
		if err := validateIP(item.IP, roundTripper.policy); err != nil {
			// Reject mixed public/private DNS answers rather than silently choosing
			// the public one; this closes rebinding and failover ambiguity.
			return nil, err
		}
		addresses = append(addresses, item.IP)
	}
	return addresses, nil
}

func validateIP(ip net.IP, policy NetworkPolicy) error {
	if ip == nil {
		return &agent.AdapterError{Code: agent.ErrorPermission, Operation: "resolve", Message: "invalid target address"}
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.Equal(metadataIPv4) || ip.Equal(metadataIPv6) {
		return &agent.AdapterError{Code: agent.ErrorPermission, Operation: "resolve", Message: "sensitive target address rejected"}
	}
	if cgnatNetwork.Contains(ip) {
		return &agent.AdapterError{Code: agent.ErrorPermission, Operation: "resolve", Message: "carrier-grade NAT target rejected"}
	}
	if ip.IsLoopback() && !policy.AllowLoopback {
		return &agent.AdapterError{Code: agent.ErrorPermission, Operation: "resolve", Message: "loopback target is not allowed"}
	}
	if ip.IsPrivate() && !ip.IsLoopback() && !policy.AllowPrivate {
		return &agent.AdapterError{Code: agent.ErrorPermission, Operation: "resolve", Message: "private target is not allowed"}
	}
	return nil
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil || !strings.EqualFold(left.Scheme, right.Scheme) || !strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	leftPort, leftErr := targetPort(left)
	rightPort, rightErr := targetPort(right)
	return leftErr == nil && rightErr == nil && leftPort == rightPort
}

func normalizeNetworkError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if adapterErr := new(agent.AdapterError); errors.As(err, &adapterErr) {
		return adapterErr
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, syscall.ETIMEDOUT) {
		return &agent.AdapterError{Code: agent.ErrorTimeout, Operation: operation, Message: "remote request timed out", Retryable: true}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return &agent.AdapterError{Code: agent.ErrorTimeout, Operation: operation, Message: "remote request timed out", Retryable: true}
	}
	return &agent.AdapterError{Code: agent.ErrorUnavailable, Operation: operation, Message: "remote request failed", Retryable: true}
}

func mustCIDR(value string) *net.IPNet {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return network
}
