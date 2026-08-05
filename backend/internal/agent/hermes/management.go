package hermes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

var serverNamePart = regexp.MustCompile(`[^a-z0-9_-]+`)

var errCloudflareChallenge = errors.New("cloudflare access challenge")

type managementClient struct {
	api                    *apiClient
	profile                string
	dashboardSessionToken  string
	cloudflareClientID     string
	cloudflareClientSecret string
	route                  agent.AccessRoute
	operationPermit        chan struct{}
}

type dashboardHealth struct {
	OK           bool   `json:"ok"`
	Version      string `json:"version"`
	AuthRequired bool   `json:"auth_required"`
}

type mcpServer struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	URL       string   `json:"url"`
	Auth      string   `json:"auth"`
	Enabled   bool     `json:"enabled"`
	Tools     []string `json:"tools"`
}

type mcpTestResult struct {
	OK    bool `json:"ok"`
	Tools []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
	Prompts   int `json:"prompts"`
	Resources int `json:"resources"`
}

func newManagementClient(connector *connector, profile string, config ManagementConfig) *managementClient {
	headers := http.Header{}
	if config.DashboardSessionToken != "" {
		headers.Set("X-Hermes-Session-Token", config.DashboardSessionToken)
	}
	management := &managementClient{
		api:                    &apiClient{connector: connector, extraHeaders: headers},
		profile:                profile,
		dashboardSessionToken:  config.DashboardSessionToken,
		cloudflareClientID:     config.CloudflareClientID,
		cloudflareClientSecret: config.CloudflareClientSecret,
		operationPermit:        make(chan struct{}, 1),
	}
	management.operationPermit <- struct{}{}
	return management
}

func (management *managementClient) acquire(ctx context.Context) error {
	select {
	case <-management.operationPermit:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (management *managementClient) release() {
	management.operationPermit <- struct{}{}
}

func (management *managementClient) profileQuery() url.Values {
	query := url.Values{}
	if management.profile != "" {
		query.Set("profile", management.profile)
	}
	return query
}

func (management *managementClient) ensureReady(ctx context.Context) (dashboardHealth, error) {
	health, err := management.probeHealth(ctx)
	if err != nil {
		return dashboardHealth{}, err
	}
	if !health.OK {
		return health, &agent.AdapterError{Code: agent.ErrorUnavailable, Operation: "hermes.dashboard.health", Message: "Hermes Dashboard is not healthy", Retryable: true}
	}
	// Hermes v2026.8.3 exposes only cookie/OAuth authentication on a
	// non-loopback bind. Cloudflare Access authenticates the outer hop but does
	// not create the required Dashboard cookie, so non-interactive management is
	// unsupported in that mode.
	if health.AuthRequired {
		return health, &agent.AdapterError{Code: agent.ErrorUnsupported, Operation: "hermes.dashboard.authentication", Message: "Hermes Dashboard requires unsupported interactive authentication"}
	}
	if management.dashboardSessionToken == "" {
		return health, &agent.AdapterError{Code: agent.ErrorAuthentication, Operation: "hermes.dashboard.authentication", Message: "Hermes Dashboard session authentication is required"}
	}
	if _, err := management.listServers(ctx); err != nil {
		return health, err
	}
	return health, nil
}

func (management *managementClient) probeHealth(ctx context.Context) (dashboardHealth, error) {
	directHeaders := management.dashboardHeaders()
	health, err := management.probeHealthAttempt(ctx, directHeaders)
	if err == nil {
		management.api.extraHeaders = directHeaders
		management.route = agent.AccessRouteDirect
		return health, nil
	}
	if !errors.Is(err, errCloudflareChallenge) {
		return dashboardHealth{}, err
	}
	if management.cloudflareClientID == "" || management.cloudflareClientSecret == "" {
		return dashboardHealth{}, &agent.AdapterError{Code: agent.ErrorAuthentication, Operation: "hermes.dashboard.health", Message: "Cloudflare Access service credentials are required"}
	}
	proxyHeaders := management.dashboardHeaders()
	proxyHeaders.Set("CF-Access-Client-Id", management.cloudflareClientID)
	proxyHeaders.Set("CF-Access-Client-Secret", management.cloudflareClientSecret)
	health, err = management.probeHealthAttempt(ctx, proxyHeaders)
	if errors.Is(err, errCloudflareChallenge) {
		return dashboardHealth{}, &agent.AdapterError{Code: agent.ErrorAuthentication, Operation: "hermes.dashboard.health", Message: "Cloudflare Access service authentication failed"}
	}
	if err != nil {
		return dashboardHealth{}, err
	}
	management.api.extraHeaders = proxyHeaders
	management.route = agent.AccessRouteAuthenticatedProxy
	return health, nil
}

func (management *managementClient) dashboardHeaders() http.Header {
	headers := http.Header{}
	if management.dashboardSessionToken != "" {
		headers.Set("X-Hermes-Session-Token", management.dashboardSessionToken)
	}
	return headers
}

func (management *managementClient) probeHealthAttempt(ctx context.Context, headers http.Header) (dashboardHealth, error) {
	requestContext, cancel := context.WithTimeout(ctx, management.api.connector.policy.RequestTimeout)
	defer cancel()
	target := management.api.connector.endpoint("/api/health", nil)
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, target.String(), nil)
	if err != nil {
		return dashboardHealth{}, &agent.AdapterError{Code: agent.ErrorInvalid, Operation: "hermes.dashboard.health", Message: "management request construction failed"}
	}
	request.Header.Set("Accept", "application/json")
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	client := &http.Client{
		Transport: &safeRoundTripper{policy: management.api.connector.policy},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return dashboardHealth{}, normalizeNetworkError("hermes.dashboard.health", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && isCloudflareChallenge(response) {
		return dashboardHealth{}, errCloudflareChallenge
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, management.api.connector.policy.MaxResponseBytes))
		return dashboardHealth{}, &agent.AdapterError{Code: agent.ErrorPermission, Operation: "hermes.dashboard.health", Message: "management redirect rejected"}
	}
	if response.StatusCode != http.StatusOK {
		return dashboardHealth{}, decodeHTTPError("hermes.dashboard.health", response, management.api.connector.policy.MaxResponseBytes)
	}
	payload, err := readBounded(response.Body, management.api.connector.policy.MaxResponseBytes)
	if err != nil {
		return dashboardHealth{}, err
	}
	var health dashboardHealth
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&health); err != nil {
		return dashboardHealth{}, &agent.AdapterError{Code: agent.ErrorProtocol, Operation: "hermes.dashboard.health", Message: "invalid Dashboard health response"}
	}
	return health, nil
}

func isCloudflareChallenge(response *http.Response) bool {
	if response == nil {
		return false
	}
	server := strings.ToLower(response.Header.Get("Server"))
	location := strings.ToLower(response.Header.Get("Location"))
	return response.Header.Get("CF-Ray") != "" || strings.Contains(server, "cloudflare") ||
		strings.Contains(location, "cloudflareaccess.com") || strings.Contains(location, "/cdn-cgi/access/")
}

func (management *managementClient) listServers(ctx context.Context) ([]mcpServer, error) {
	var response struct {
		Servers []mcpServer `json:"servers"`
	}
	if err := management.api.doJSON(ctx, "hermes.dashboard.mcp.list", http.MethodGet, "/api/mcp/servers", management.profileQuery(), nil, &response, http.StatusOK); err != nil {
		return nil, err
	}
	return response.Servers, nil
}

func (management *managementClient) createServer(ctx context.Context, name, endpoint, credential string) (mcpServer, error) {
	body := map[string]any{
		"name": name, "url": endpoint, "auth": "header", "bearer_token": credential,
	}
	if management.profile != "" {
		body["profile"] = management.profile
	}
	var response mcpServer
	if err := management.api.doJSON(ctx, "hermes.dashboard.mcp.create", http.MethodPost, "/api/mcp/servers", nil, body, &response, http.StatusOK, http.StatusCreated); err != nil {
		return mcpServer{}, err
	}
	if response.Name != name || response.Transport != "http" || response.URL != endpoint || response.Auth != "header" {
		return mcpServer{}, unexpectedObject("mcp create")
	}
	return response, nil
}

func (management *managementClient) deleteServer(ctx context.Context, name string) error {
	id, err := pathID(name)
	if err != nil {
		return err
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := management.api.doJSON(ctx, "hermes.dashboard.mcp.delete", http.MethodDelete, "/api/mcp/servers/"+id, management.profileQuery(), nil, &response, http.StatusOK); err != nil {
		return err
	}
	if !response.OK {
		return unexpectedObject("mcp delete")
	}
	return nil
}

func (management *managementClient) testServer(ctx context.Context, name string) (mcpTestResult, error) {
	id, err := pathID(name)
	if err != nil {
		return mcpTestResult{}, err
	}
	var result mcpTestResult
	if err := management.api.doJSON(ctx, "hermes.dashboard.mcp.test", http.MethodPost, "/api/mcp/servers/"+id+"/test", management.profileQuery(), map[string]any{}, &result, http.StatusOK); err != nil {
		return mcpTestResult{}, err
	}
	if !result.OK {
		return result, &agent.AdapterError{Code: agent.ErrorUnavailable, Operation: "hermes.dashboard.mcp.test", Message: "Hermes could not access the project MCP endpoint", Retryable: true}
	}
	return result, nil
}

func (management *managementClient) restartGateway(ctx context.Context) error {
	var response struct {
		OK bool `json:"ok"`
	}
	if err := management.api.doJSON(ctx, "hermes.dashboard.gateway.restart", http.MethodPost, "/api/gateway/restart", management.profileQuery(), map[string]any{}, &response, http.StatusOK); err != nil {
		return err
	}
	if !response.OK {
		return unexpectedObject("gateway restart")
	}
	return nil
}

func (adapter *Adapter) VerifyProjectAccess(ctx context.Context, request agent.ProjectAccessRequest) (agent.ProjectAccessResult, error) {
	if adapter.management == nil {
		// Manual management has no Dashboard credential by design. The Agent
		// domain completes verification using the actual Agent Token observation
		// at MCP Gateway; the adapter cannot safely infer that reverse connection.
		return agent.ProjectAccessResult{State: agent.ProjectAccessPending}, nil
	}
	if err := adapter.management.acquire(ctx); err != nil {
		return agent.ProjectAccessResult{State: agent.ProjectAccessUnavailable}, err
	}
	defer adapter.management.release()
	if _, err := adapter.management.ensureReady(ctx); err != nil {
		return agent.ProjectAccessResult{State: stateForError(err)}, err
	}
	remoteID := strings.TrimSpace(request.CurrentRemoteID)
	if remoteID == "" && request.BindingID != "" && request.Credential != "" {
		remoteID = versionedServerName(request.BindingID, request.Credential)
	}
	if remoteID == "" {
		return agent.ProjectAccessResult{}, agent.ErrInvalidArgument
	}
	return adapter.verifyManagedServer(ctx, remoteID, request.ExpectedTools)
}

func (adapter *Adapter) ConfigureProjectAccess(ctx context.Context, request agent.ProjectAccessRequest) (agent.ProjectAccessResult, error) {
	if err := validateProjectAccessRequest(request, false); err != nil {
		return agent.ProjectAccessResult{}, err
	}
	if adapter.management == nil {
		return agent.ProjectAccessResult{State: agent.ProjectAccessUnsupported}, &agent.AdapterError{Code: agent.ErrorUnsupported, Operation: "project_access.configure", Message: "automatic project access management is unavailable"}
	}
	if err := adapter.management.acquire(ctx); err != nil {
		return agent.ProjectAccessResult{State: agent.ProjectAccessUnavailable}, err
	}
	defer adapter.management.release()
	return adapter.installProjectAccess(ctx, request)
}

func (adapter *Adapter) RotateProjectAccess(ctx context.Context, request agent.ProjectAccessRequest) (agent.ProjectAccessResult, error) {
	if err := validateProjectAccessRequest(request, true); err != nil {
		return agent.ProjectAccessResult{}, err
	}
	if adapter.management == nil {
		return agent.ProjectAccessResult{State: agent.ProjectAccessUnsupported}, &agent.AdapterError{Code: agent.ErrorUnsupported, Operation: "project_access.rotate", Message: "automatic project access rotation is unavailable"}
	}
	if err := adapter.management.acquire(ctx); err != nil {
		return agent.ProjectAccessResult{State: agent.ProjectAccessUnavailable}, err
	}
	defer adapter.management.release()
	newRemoteID := versionedServerName(request.BindingID, request.Credential)
	if newRemoteID == request.CurrentRemoteID {
		if _, err := adapter.management.ensureReady(ctx); err != nil {
			return agent.ProjectAccessResult{State: stateForError(err), Route: adapter.management.route, RemoteID: newRemoteID}, err
		}
		return adapter.verifyManagedServer(ctx, newRemoteID, request.ExpectedTools)
	}
	result, err := adapter.installProjectAccess(ctx, request)
	if err != nil {
		// installProjectAccess never deletes CurrentRemoteID. The caller must
		// keep the old product token active whenever this path returns an error.
		return result, err
	}
	if err := adapter.management.deleteServer(ctx, request.CurrentRemoteID); err != nil {
		// The new configuration has passed both probes. A stale old server is
		// safe to clean up later; it does not invalidate the verified new path.
		result.CleanupPending = true
	}
	return result, nil
}

func (adapter *Adapter) installProjectAccess(ctx context.Context, request agent.ProjectAccessRequest) (agent.ProjectAccessResult, error) {
	if _, err := adapter.management.ensureReady(ctx); err != nil {
		return agent.ProjectAccessResult{State: stateForError(err), Route: adapter.management.route}, err
	}
	remoteID := versionedServerName(request.BindingID, request.Credential)
	created := false
	servers, err := adapter.management.listServers(ctx)
	if err != nil {
		return agent.ProjectAccessResult{State: stateForError(err), Route: adapter.management.route}, err
	}
	found := false
	for _, server := range servers {
		if server.Name != remoteID {
			continue
		}
		found = true
		if server.Transport != "http" || server.URL != request.Endpoint || server.Auth != "header" {
			return agent.ProjectAccessResult{State: agent.ProjectAccessUnavailable, Route: adapter.management.route, RemoteID: remoteID}, &agent.AdapterError{Code: agent.ErrorConflict, Operation: "project_access.configure", Message: "remote project access reference conflicts with existing configuration"}
		}
		break
	}
	if !found {
		if _, err := adapter.management.createServer(ctx, remoteID, request.Endpoint, request.Credential); err != nil {
			return agent.ProjectAccessResult{State: stateForError(err), Route: adapter.management.route, RemoteID: remoteID}, err
		}
		created = true
	}
	cleanupNew := func() {
		if created {
			_ = adapter.management.deleteServer(context.WithoutCancel(ctx), remoteID)
		}
	}
	first, err := adapter.verifyManagedServer(ctx, remoteID, request.ExpectedTools)
	if err != nil {
		cleanupNew()
		return first, err
	}
	if err := adapter.management.restartGateway(ctx); err != nil {
		cleanupNew()
		return agent.ProjectAccessResult{State: stateForError(err), Route: adapter.management.route, RemoteID: remoteID}, err
	}
	verified, err := adapter.verifyManagedServer(ctx, remoteID, request.ExpectedTools)
	if err != nil {
		// Do not touch CurrentRemoteID. Keeping the new entry is safer than
		// deleting a configuration that may already be live after restart.
		return verified, err
	}
	return verified, nil
}

func (adapter *Adapter) verifyManagedServer(ctx context.Context, remoteID string, expectedTools []string) (agent.ProjectAccessResult, error) {
	test, err := adapter.management.testServer(ctx, remoteID)
	if err != nil {
		return agent.ProjectAccessResult{State: stateForError(err), Route: adapter.management.route, RemoteID: remoteID}, err
	}
	tools := make([]string, 0, len(test.Tools))
	for _, tool := range test.Tools {
		if strings.TrimSpace(tool.Name) != "" {
			tools = append(tools, tool.Name)
		}
	}
	tools = sortedUnique(tools)
	if missing := missingTools(tools, expectedTools); len(missing) > 0 {
		return agent.ProjectAccessResult{State: agent.ProjectAccessUnavailable, Route: adapter.management.route, RemoteID: remoteID, Tools: tools}, &agent.AdapterError{Code: agent.ErrorUnavailable, Operation: "project_access.verify", Message: "required MCP tools are unavailable"}
	}
	return agent.ProjectAccessResult{State: agent.ProjectAccessReady, Route: adapter.management.route, RemoteID: remoteID, Verified: true, Tools: tools}, nil
}

func validateProjectAccessRequest(request agent.ProjectAccessRequest, rotation bool) error {
	if strings.TrimSpace(request.BindingID) == "" || strings.TrimSpace(request.Credential) == "" {
		return agent.ErrInvalidArgument
	}
	if rotation && strings.TrimSpace(request.CurrentRemoteID) == "" {
		return agent.ErrInvalidArgument
	}
	parsed, err := url.Parse(strings.TrimSpace(request.Endpoint))
	if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%w: invalid MCP endpoint", agent.ErrInvalidArgument)
	}
	if strings.ContainsAny(parsed.Host, "\r\n\x00") {
		return agent.ErrInvalidArgument
	}
	return nil
}

func versionedServerName(bindingID, credential string) string {
	part := strings.ToLower(strings.TrimSpace(bindingID))
	part = serverNamePart.ReplaceAllString(part, "-")
	part = strings.Trim(part, "-_")
	if part == "" {
		part = "project"
	}
	if len(part) > 40 {
		part = part[:40]
	}
	digest := sha256.Sum256([]byte(credential))
	return "mmdash-" + part + "-" + hex.EncodeToString(digest[:8])
}

func stateForError(err error) agent.ProjectAccessState {
	var adapterError *agent.AdapterError
	if errors.As(err, &adapterError) && adapterError.Code == agent.ErrorUnsupported {
		return agent.ProjectAccessUnsupported
	}
	return agent.ProjectAccessUnavailable
}
