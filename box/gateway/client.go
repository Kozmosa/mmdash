package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mmdash/mmdash/box/contracts"
)

type RegistrationInput struct {
	InstallationID string
	Name           string
	Version        string
	Capabilities   []contracts.Capability
	Runtimes       []contracts.Runtime
	Limits         contracts.ResourceLimits
}

// RegistrationInputFor builds the frozen registration payload for
// administrative commands such as `mbox account login` without exposing
// Gateway internals.
func RegistrationInputFor(installationID, name, version string, runtimes []contracts.Runtime, limits contracts.ResourceLimits) RegistrationInput {
	return RegistrationInput{
		InstallationID: installationID,
		Name:           name,
		Version:        version,
		Capabilities: []contracts.Capability{{
			Name: "sandbox", Version: "2", Features: []string{"execution-bundle", "durable-log-spool", "offline-resume"},
		}},
		Runtimes: runtimes,
		Limits:   limits,
	}
}

type Registration struct {
	BoxID string
	Token string
}

type CoreClient interface {
	StartDeviceAuthorization(context.Context) (contracts.DeviceAuthorization, error)
	ExchangeDeviceAuthorization(context.Context, string) (contracts.BoxRegistrationGrant, error)
	Register(context.Context, string, RegistrationInput) (Registration, error)
	Heartbeat(context.Context, string, string, RegistrationInput, contracts.Load) error
	Claim(context.Context, string, string, time.Duration) (*contracts.Task, error)
	Resume(context.Context, string, string, string, contracts.ResumeRequest) (contracts.Resume, error)
	Logs(context.Context, string, string, string, contracts.LogBatch) (contracts.LogAcknowledgement, error)
	Status(context.Context, string, string, string, string, string, string, time.Time, *int, *contracts.Failure, map[string]interface{}, string) error
	Result(context.Context, string, string, string, string, string, contracts.ArtifactPointer) error
	UploadArtifact(context.Context, string, string, string, string, io.Reader, int64, string) (contracts.ArtifactPointer, error)
}

// HTTPClient speaks only the frozen Core Box Control endpoints. It never
// forwards provider credentials or accepts an arbitrary command payload.
type HTTPClient struct {
	BaseURL string
	Client  *http.Client
}

type coreAPIError struct {
	status  int
	code    string
	message string
}

func (err *coreAPIError) Error() string {
	return fmt.Sprintf("Core Box API returned HTTP %d (%s): %s", err.status, err.code, err.message)
}

func (err *coreAPIError) Temporary() bool {
	return err.status == http.StatusRequestTimeout || err.status == http.StatusTooEarly ||
		err.status == http.StatusTooManyRequests || err.status >= http.StatusInternalServerError
}

func (err *coreAPIError) AuthorizationPending() bool {
	return err.code == "AUTHORIZATION_PENDING"
}

type coreTransportError struct{ err error }

func (err *coreTransportError) Error() string { return err.err.Error() }
func (err *coreTransportError) Unwrap() error { return err.err }
func (*coreTransportError) Temporary() bool   { return true }

func (client HTTPClient) StartDeviceAuthorization(ctx context.Context) (contracts.DeviceAuthorization, error) {
	var result contracts.DeviceAuthorization
	err := client.do(ctx, http.MethodPost, "/v1/auth/device/authorize", "", map[string]interface{}{
		"client_kind": "box",
	}, &result, 30*time.Second)
	return result, err
}

func (client HTTPClient) ExchangeDeviceAuthorization(ctx context.Context, deviceCode string) (contracts.BoxRegistrationGrant, error) {
	var result contracts.BoxRegistrationGrant
	err := client.do(ctx, http.MethodPost, "/v1/auth/device/token", "", map[string]interface{}{
		"device_code": deviceCode, "client_kind": "box",
	}, &result, 30*time.Second)
	return result, err
}

func (client HTTPClient) Register(ctx context.Context, grant string, input RegistrationInput) (Registration, error) {
	var response struct {
		Box struct {
			ID string `json:"box_id"`
		} `json:"box"`
		Token string `json:"box_token"`
	}
	err := client.do(ctx, http.MethodPost, "/v1/boxes", "", map[string]interface{}{
		"registration_grant": grant, "installation_id": input.InstallationID,
		"name": input.Name, "version": input.Version,
		"capabilities": input.Capabilities, "runtimes": input.Runtimes,
		"limits": input.Limits,
	}, &response, 30*time.Second)
	if err != nil {
		return Registration{}, err
	}
	if response.Box.ID == "" || response.Token == "" {
		return Registration{}, errors.New("Core returned an incomplete Box registration")
	}
	return Registration{BoxID: response.Box.ID, Token: response.Token}, nil
}

func (client HTTPClient) Heartbeat(ctx context.Context, boxID, token string, input RegistrationInput, load contracts.Load) error {
	return client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/heartbeat", token, map[string]interface{}{
		"version": input.Version, "capabilities": input.Capabilities,
		"runtimes": input.Runtimes, "limits": input.Limits, "load": load,
	}, nil, 30*time.Second)
}

func (client HTTPClient) Claim(ctx context.Context, boxID, token string, wait time.Duration) (*contracts.Task, error) {
	var task contracts.Task
	err := client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/tasks/claim", token, map[string]interface{}{
		"wait_seconds": int64(wait / time.Second),
	}, &task, wait+15*time.Second)
	if errors.Is(err, errNoContent) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (client HTTPClient) Resume(ctx context.Context, boxID, token, taskID string, request contracts.ResumeRequest) (contracts.Resume, error) {
	var result contracts.Resume
	err := client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/tasks/"+taskID+"/resume", token, request, &result, 30*time.Second)
	return result, err
}

func (client HTTPClient) Logs(ctx context.Context, boxID, token, taskID string, batch contracts.LogBatch) (contracts.LogAcknowledgement, error) {
	var result contracts.LogAcknowledgement
	err := client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/tasks/"+taskID+"/logs", token, batch, &result, 30*time.Second)
	return result, err
}

func (client HTTPClient) Status(
	ctx context.Context,
	boxID, token, taskID, executionEpoch, runtimeVersion, status string,
	occurredAt time.Time,
	exitCode *int,
	failure *contracts.Failure,
	usage map[string]interface{},
	summary string,
) error {
	return client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/tasks/"+taskID+"/status", token, map[string]interface{}{
		"execution_epoch": executionEpoch, "runtime_version": runtimeVersion,
		"status": status, "occurred_at": occurredAt, "exit_code": exitCode,
		"failure": failure, "resource_usage": usage, "summary": optionalString(summary),
	}, nil, 30*time.Second)
}

func (client HTTPClient) Result(ctx context.Context, boxID, token, taskID, executionEpoch, manifestSHA string, artifact contracts.ArtifactPointer) error {
	return client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/tasks/"+taskID+"/result", token, map[string]interface{}{
		"execution_epoch": executionEpoch, "manifest_sha256": manifestSHA,
		"execution_bundle": artifact,
	}, nil, 30*time.Second)
}

func (client HTTPClient) UploadArtifact(
	ctx context.Context,
	boxID, token, taskID, executionEpoch string,
	input io.Reader,
	size int64,
	sha string,
) (contracts.ArtifactPointer, error) {
	var result contracts.ArtifactPointer
	base := strings.TrimRight(client.BaseURL, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/boxes/"+boxID+"/tasks/"+taskID+"/artifact", input)
	if err != nil {
		return result, err
	}
	request.ContentLength = size
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/zip")
	request.Header.Set("X-Mmdash-Execution-Epoch", executionEpoch)
	request.Header.Set("X-Mmdash-Artifact-SHA256", sha)
	request.Header.Set("X-Mmdash-Artifact-Size", fmt.Sprintf("%d", size))
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.httpClient(30 * time.Minute).Do(request)
	if err != nil {
		return result, &coreTransportError{err: err}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, decodeAPIError(response)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return result, err
	}
	return result, nil
}

var errNoContent = errors.New("Core returned no task")

func (client HTTPClient) do(
	ctx context.Context,
	method, path, token string,
	body, result interface{},
	timeout time.Duration,
) error {
	base := strings.TrimRight(client.BaseURL, "/")
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.httpClient(timeout).Do(request)
	if err != nil {
		return &coreTransportError{err: err}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return errNoContent
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeAPIError(response)
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(result)
}

func (client HTTPClient) httpClient(timeout time.Duration) *http.Client {
	if client.Client != nil {
		return client.Client
	}
	return &http.Client{Timeout: timeout}
}

func decodeAPIError(response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	payload := struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{}
	_ = json.Unmarshal(data, &payload)
	if payload.Message == "" {
		payload.Message = strings.TrimSpace(string(data))
	}
	return &coreAPIError{status: response.StatusCode, code: payload.Code, message: payload.Message}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
