package repo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/repo/provider"
)

func TestProviderFailureTaxonomyAndRetryability(t *testing.T) {
	for _, test := range []struct {
		code      string
		err       error
		outcome   string
		retryable bool
	}{
		{code: "REPO_NETWORK_UNAVAILABLE", err: provider.ErrNetworkUnavailable, outcome: "network", retryable: true},
		{code: "REPO_GIT_TIMEOUT", err: provider.ErrTimeout, outcome: "timeout", retryable: true},
		{code: "REPO_PROVIDER_TEMPORARILY_UNAVAILABLE", err: provider.ErrTemporarilyUnavailable, outcome: "provider_5xx", retryable: true},
		{code: "REPO_AUTH_FAILED", err: provider.ErrAuthentication, outcome: "auth"},
		{code: "REPO_REMOTE_NOT_FOUND", err: provider.ErrRemoteNotFound, outcome: "not_found"},
		{code: "REPO_BRANCH_NOT_FOUND", err: provider.ErrBranchMissing, outcome: "not_found"},
		{code: "REPO_WRITE_PERMISSION_REQUIRED", err: provider.ErrWritePermission, outcome: "auth"},
	} {
		failure := classifyProviderFailure(test.err)
		if failure.Code != test.code || failure.Outcome != test.outcome ||
			failure.Retryable != test.retryable {
			t.Fatalf("unexpected failure for %v: %#v", test.err, failure)
		}
	}
}

func TestWriteRepoErrorExposesOnlySafeRetryableDetails(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/projects/project/repository/test", nil)
	writeRepoError(response, request, provider.ErrNetworkUnavailable)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	var body struct {
		Code    string `json:"code"`
		Details struct {
			Retryable bool `json:"retryable"`
		} `json:"details"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Code != "REPO_NETWORK_UNAVAILABLE" || !body.Details.Retryable ||
		body.Message != "External repository network is temporarily unavailable" {
		t.Fatalf("unexpected safe error: %#v", body)
	}
}
