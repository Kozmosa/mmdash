package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
)

type createSandboxRequest struct {
	TemplateID          string            `json:"templateID"`
	Timeout             int               `json:"timeout"`
	AutoPause           bool              `json:"autoPause"`
	Secure              bool              `json:"secure"`
	AllowInternetAccess bool              `json:"allow_internet_access"`
	Network             sandboxNetwork    `json:"network"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	EnvVars             map[string]string `json:"envVars,omitempty"`
}

type sandboxNetwork struct {
	DenyOut            []string `json:"denyOut,omitempty"`
	AllowPublicTraffic bool     `json:"allowPublicTraffic"`
}

type createSandboxResponse struct {
	SandboxID       string  `json:"sandboxID"`
	EnvdVersion     string  `json:"envdVersion"`
	EnvdAccessToken *string `json:"envdAccessToken"`
	Domain          *string `json:"domain"`
}

type listedSandbox struct {
	SandboxID string            `json:"sandboxID"`
	Metadata  map[string]string `json:"metadata"`
}

type sandboxInfo struct {
	SandboxID           string `json:"sandboxID"`
	TemplateID          string `json:"templateID"`
	EnvdVersion         string `json:"envdVersion"`
	EnvdAccessToken     string `json:"envdAccessToken"`
	AllowInternetAccess *bool  `json:"allowInternetAccess"`
	CPUCount            int    `json:"cpuCount"`
	MemoryMB            int    `json:"memoryMB"`
	DiskSizeMB          int    `json:"diskSizeMB"`
}

type sandboxMetric struct {
	CPUUsedPct float64 `json:"cpuUsedPct"`
	MemUsed    int64   `json:"memUsed"`
	DiskUsed   int64   `json:"diskUsed"`
}

type providerErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (client *ProviderClient) createSandbox(ctx context.Context, template string, request sandbox.RunRequest) (sandboxConnection, error) {
	timeout := request.Spec.Limits.TimeoutSecond + int((client.sandboxGrace+time.Second-1)/time.Second)
	if timeout > 86_400 {
		timeout = 86_400
	}
	allowInternet := request.Spec.Limits.Network == "enabled"
	body := createSandboxRequest{
		TemplateID: template, Timeout: timeout, AutoPause: false, Secure: true,
		AllowInternetAccess: allowInternet,
		Network:             sandboxNetwork{AllowPublicTraffic: false},
		Metadata: map[string]string{
			"mmdash_task_id": request.ID, "mmdash_experiment_id": request.Spec.ExperimentID,
			"mmdash_project_id": request.Spec.ProjectID,
		},
		EnvVars: cloneStrings(request.Spec.Environment),
	}
	if !allowInternet {
		body.Network.DenyOut = []string{allIPv4Traffic}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return sandboxConnection{}, err
	}
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, client.apiURL+"/sandboxes", bytes.NewReader(payload))
	if err != nil {
		return sandboxConnection{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	client.platformHeaders(httpRequest)
	response, err := client.httpClient.Do(httpRequest)
	if err != nil {
		client.cleanupTaskSandboxes(request.ID)
		return sandboxConnection{}, fmt.Errorf("create E2B sandbox: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return sandboxConnection{}, client.responseError("create E2B sandbox", response)
	}
	var created createSandboxResponse
	if err := decodeJSON(response.Body, &created); err != nil {
		client.cleanupInvalidCreate(created.SandboxID, request.ID)
		return sandboxConnection{}, fmt.Errorf("decode E2B sandbox response: %w", err)
	}
	if created.SandboxID == "" || created.EnvdVersion == "" || created.EnvdAccessToken == nil || *created.EnvdAccessToken == "" {
		client.cleanupInvalidCreate(created.SandboxID, request.ID)
		return sandboxConnection{}, errors.New("E2B secure sandbox response is missing required connection fields")
	}
	providerSandboxDomain := ""
	if created.Domain != nil {
		providerSandboxDomain = *created.Domain
	}
	sandboxURL, err := client.sandboxEndpoint(created.SandboxID, providerSandboxDomain)
	if err != nil {
		client.cleanupInvalidCreate(created.SandboxID, request.ID)
		return sandboxConnection{}, err
	}
	return sandboxConnection{SandboxID: created.SandboxID, SandboxURL: sandboxURL, EnvdVersion: created.EnvdVersion, EnvdAccessToken: *created.EnvdAccessToken}, nil
}

func (client *ProviderClient) cleanupInvalidCreate(sandboxID, taskID string) {
	if sandboxID == "" {
		client.cleanupTaskSandboxes(taskID)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), client.cleanupTimeout)
	defer cancel()
	_ = client.killSandbox(ctx, sandboxID)
}

func (client *ProviderClient) cleanupTaskSandboxes(taskID string) {
	if strings.TrimSpace(taskID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), client.cleanupTimeout)
	defer cancel()
	values := url.Values{}
	values.Set("metadata", "mmdash_task_id="+taskID)
	values.Set("limit", "100")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.apiURL+"/v2/sandboxes?"+values.Encode(), nil)
	if err != nil {
		return
	}
	client.platformHeaders(request)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return
	}
	var sandboxes []listedSandbox
	if decodeJSON(response.Body, &sandboxes) != nil {
		return
	}
	for _, item := range sandboxes {
		if item.Metadata["mmdash_task_id"] == taskID {
			_ = client.killSandbox(ctx, item.SandboxID)
		}
	}
}

func (client *ProviderClient) sandboxEndpoint(sandboxID, responseDomain string) (string, error) {
	if client.sandboxURL != "" {
		return client.sandboxURL, nil
	}
	if !sandboxIDPattern.MatchString(sandboxID) {
		return "", errors.New("E2B sandbox response contains an invalid sandbox ID")
	}
	domain := strings.TrimSpace(responseDomain)
	if domain == "" {
		domain = client.domain
	}
	domain, err := providerDomain(domain)
	if err != nil {
		return "", fmt.Errorf("invalid E2B sandbox domain: %w", err)
	}
	if _, stable := stableSandboxDomains[domain]; stable {
		return "https://sandbox." + domain, nil
	}
	return "https://" + strconv.Itoa(defaultEnvdPort) + "-" + sandboxID + "." + domain, nil
}

func providerDomain(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse("https://" + value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host != value {
		return "", errors.New("provider domain must be a DNS host without a scheme or path")
	}
	return value, nil
}

func (client *ProviderClient) getSandbox(ctx context.Context, sandboxID string) (sandboxInfo, error) {
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodGet, client.apiURL+"/sandboxes/"+url.PathEscape(sandboxID), nil)
	if err != nil {
		return sandboxInfo{}, err
	}
	client.platformHeaders(httpRequest)
	response, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return sandboxInfo{}, fmt.Errorf("read E2B sandbox: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return sandboxInfo{}, client.responseError("read E2B sandbox", response)
	}
	var info sandboxInfo
	if err := decodeJSON(response.Body, &info); err != nil {
		return sandboxInfo{}, fmt.Errorf("decode E2B sandbox detail: %w", err)
	}
	if info.SandboxID != sandboxID || info.CPUCount < 1 || info.MemoryMB < 1 || info.DiskSizeMB < 1 {
		return sandboxInfo{}, errors.New("E2B sandbox detail is incomplete")
	}
	return info, nil
}

func (client *ProviderClient) getSandboxMetrics(ctx context.Context, sandboxID string, startedAt time.Time) ([]sandboxMetric, error) {
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	values := url.Values{}
	values.Set("start", strconv.FormatInt(startedAt.Unix(), 10))
	values.Set("end", strconv.FormatInt(time.Now().Unix(), 10))
	endpoint := client.apiURL + "/sandboxes/" + url.PathEscape(sandboxID) + "/metrics?" + values.Encode()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client.platformHeaders(httpRequest)
	response, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, client.responseError("read E2B sandbox metrics", response)
	}
	var metrics []sandboxMetric
	if err := decodeJSON(response.Body, &metrics); err != nil {
		return nil, err
	}
	return metrics, nil
}

func (client *ProviderClient) killSandbox(ctx context.Context, sandboxID string) error {
	if sandboxID == "" {
		return nil
	}
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodDelete, client.apiURL+"/sandboxes/"+url.PathEscape(sandboxID), nil)
	if err != nil {
		return err
	}
	client.platformHeaders(httpRequest)
	response, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("kill E2B sandbox: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil
	}
	return client.responseError("kill E2B sandbox", response)
}

func (client *ProviderClient) platformHeaders(request *http.Request) {
	request.Header.Set("X-API-Key", client.apiKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", client.userAgent)
}

func (client *ProviderClient) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if client.requestTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, client.requestTimeout)
}

func (client *ProviderClient) responseError(operation string, response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	message := strings.TrimSpace(string(data))
	var provider providerErrorBody
	if json.Unmarshal(data, &provider) == nil && provider.Message != "" {
		message = provider.Message
	}
	message = client.redact(message)
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	return fmt.Errorf("%s: E2B returned HTTP %d: %s", operation, response.StatusCode, message)
}

func (client *ProviderClient) redact(message string) string {
	message = strings.ReplaceAll(message, client.apiKey, "[REDACTED]")
	client.mu.Lock()
	defer client.mu.Unlock()
	for _, session := range client.sessions {
		if session.envdAccessToken != "" {
			message = strings.ReplaceAll(message, session.envdAccessToken, "[REDACTED]")
		}
	}
	return message
}

func decodeJSON(reader io.Reader, destination interface{}) error {
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	return decoder.Decode(destination)
}

func cloneStrings(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
