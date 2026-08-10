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
	ProjectID    string
	Name         string
	Version      string
	Capabilities []contracts.Capability
	Runtimes     []contracts.Runtime
	Limits       contracts.ResourceLimits
	Idempotency  string
}

type Registration struct {
	BoxID string
	Token string
}

type CoreClient interface {
	Register(context.Context, string, RegistrationInput) (Registration, error)
	Heartbeat(context.Context, string, string, RegistrationInput, contracts.Load) error
	Claim(context.Context, string, string, time.Duration) (*contracts.Task, error)
	Renew(context.Context, string, string, string, time.Duration) (bool, error)
	Log(context.Context, string, string, string, contracts.Log) error
	Status(context.Context, string, string, string, string, *int, string, string, map[string]interface{}, string) error
	Result(context.Context, string, string, string, contracts.Manifest, contracts.ArtifactPointer) error
	UploadArtifact(context.Context, string, string, string, io.Reader, int64, string) (contracts.ArtifactPointer, error)
}

// HTTPClient speaks only the frozen Core Box Control endpoints. It never
// forwards provider credentials or accepts an arbitrary command payload.
type HTTPClient struct {
	BaseURL string
	Client  *http.Client
}

func (client HTTPClient) Register(ctx context.Context, authorization string, input RegistrationInput) (Registration, error) {
	var response struct {
		Box struct {
			ID string `json:"box_id"`
		} `json:"box"`
		Token string `json:"token"`
	}
	err := client.do(ctx, http.MethodPost, "/v1/boxes", authorization, map[string]interface{}{
		"project_id": input.ProjectID, "name": input.Name, "version": input.Version,
		"capabilities": input.Capabilities, "runtimes": input.Runtimes, "limits": input.Limits,
		"idempotency_key": input.Idempotency,
	}, &response)
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
		"version": input.Version, "capabilities": input.Capabilities, "runtimes": input.Runtimes,
		"limits": input.Limits, "load": load,
	}, nil)
}

func (client HTTPClient) Claim(ctx context.Context, boxID, token string, lease time.Duration) (*contracts.Task, error) {
	var task contracts.Task
	err := client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/tasks/claim", token, map[string]interface{}{"lease_seconds": int64(lease / time.Second)}, &task)
	if errors.Is(err, errNoContent) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (client HTTPClient) Renew(ctx context.Context, boxID, token, taskID string, lease time.Duration) (bool, error) {
	var response struct {
		CancelRequested bool `json:"cancel_requested"`
	}
	err := client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/tasks/"+taskID+"/heartbeat", token, map[string]interface{}{"lease_seconds": int64(lease / time.Second)}, &response)
	return response.CancelRequested, err
}

func (client HTTPClient) Log(ctx context.Context, boxID, token, taskID string, log contracts.Log) error {
	return client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/tasks/"+taskID+"/logs", token, log, nil)
}

func (client HTTPClient) Status(ctx context.Context, boxID, token, taskID, status string, exitCode *int, code, message string, usage map[string]interface{}, summary string) error {
	var exit *int64
	if exitCode != nil {
		value := int64(*exitCode)
		exit = &value
	}
	return client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/tasks/"+taskID+"/status", token, map[string]interface{}{
		"status": status, "exit_code": exit, "error_code": optionalString(code),
		"error_message": optionalString(message), "resource_usage": usage, "summary": optionalString(summary),
	}, nil)
}

func (client HTTPClient) Result(ctx context.Context, boxID, token, taskID string, manifest contracts.Manifest, artifact contracts.ArtifactPointer) error {
	return client.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/tasks/"+taskID+"/result", token, map[string]interface{}{"manifest": manifest, "artifact": artifact}, nil)
}

func (client HTTPClient) UploadArtifact(ctx context.Context, boxID, token, taskID string, input io.Reader, size int64, sha string) (contracts.ArtifactPointer, error) {
	var result contracts.ArtifactPointer
	base := strings.TrimRight(client.BaseURL, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/boxes/"+boxID+"/tasks/"+taskID+"/artifact", input)
	if err != nil {
		return result, err
	}
	request.ContentLength = size
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/zip")
	request.Header.Set("X-Mmdash-Artifact-SHA256", sha)
	request.Header.Set("Authorization", "Bearer "+token)
	httpClient := client.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Minute}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return result, fmt.Errorf("Core artifact upload returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return result, err
	}
	return result, nil
}

var errNoContent = errors.New("Core returned no task")

func (client HTTPClient) do(ctx context.Context, method, path, token string, body, result interface{}) error {
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
	httpClient := client.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return errNoContent
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("Core Box API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(result)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
