package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/auth"
)

func TestModuleRejectsExtraAgentRouteSegments(t *testing.T) {
	module := Module{}
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}
	tests := []struct {
		name string
		rest []string
	}{
		{name: "checks", rest: []string{"agent-1", "checks", "extra"}},
		{name: "project access", rest: []string{"agent-1", "project-access", "verify", "extra"}},
		{name: "prompt reset", rest: []string{"agent-1", "prompt", "reset", "extra"}},
		{name: "session end", rest: []string{"agent-1", "sessions", "session-1", "end", "extra"}},
		{name: "run stop", rest: []string{"agent-1", "sessions", "session-1", "runs", "run-1", "stop", "extra"}},
		{name: "approval", rest: []string{"agent-1", "sessions", "session-1", "runs", "run-1", "approvals", "approval-1", "extra"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/projects/project-1/agent-instances/invalid", nil)
			response := httptest.NewRecorder()
			module.handleInstance(response, request, identity, "project-1", test.rest)
			if response.Code != http.StatusNotFound {
				t.Fatalf("extra route segment status: got %d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestWriteAgentErrorMapsAuthDomainErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid", err: auth.ErrInvalid, status: http.StatusBadRequest},
		{name: "forbidden", err: auth.ErrForbidden, status: http.StatusForbidden},
		{name: "not found", err: auth.ErrNotFound, status: http.StatusNotFound},
		{name: "conflict", err: auth.ErrConflict, status: http.StatusConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/projects/project-1/agent-instances/agent-1/tokens/token-1/verify", nil)
			response := httptest.NewRecorder()
			writeAgentError(response, request.WithContext(context.Background()), test.err)
			if response.Code != test.status {
				t.Fatalf("auth error status: got %d want %d body=%s", response.Code, test.status, response.Body.String())
			}
		})
	}
}
