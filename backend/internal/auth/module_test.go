package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/coreapp"
	"github.com/mmdash/mmdash/backend/internal/platform/health"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/logging"
	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
	"github.com/mmdash/mmdash/backend/internal/platform/module"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
)

type agentRelayProbeModule struct{ service *Service }

func (agentRelayProbeModule) Name() string { return "agent-relay-probe" }

func (probe agentRelayProbeModule) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/projects/project-1/members", func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		identity, err := probe.service.Authenticate(
			request.Context(), request.Header.Get("Authorization"),
		)
		if err != nil {
			writeDomainError(response, request, err)
			return
		}
		requestctx.SetActor(request.Context(), identity.AgentInstanceID, identity.Kind)
		requestctx.SetProject(request.Context(), identity.ProjectID)
		values := requestctx.TrustedSnapshot(request.Context())
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]string{
			"actor_id": values.ActorID, "actor_kind": values.ActorKind,
			"project_id": values.ProjectID,
		})
	})
}

func TestProductAgentBusinessRequestsUseFirstClassAgentIdentity(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	store.user = User{
		ID: "admin-user", Status: "active", SystemRole: "admin",
	}
	agentSecret := "product-agent-secret-must-not-appear-in-errors"
	store.agentTokens["agent-token-id"] = AgentToken{
		AgentInstanceID: "agent-instance-1",
		AllowedTools:    []string{"project.get"},
		GrantID:         "grant-1",
		ID:              "agent-token-id",
		ProjectID:       "project-1",
		Status:          "active",
		TokenHash:       hashToken(agentSecret),
	}
	service := &Service{
		Clock: clock.Fixed{Time: now},
		Store: store,
	}
	modules := module.NewRegistry()
	if err := modules.Register(Module{Service: service}); err != nil {
		t.Fatal(err)
	}
	if err := modules.Register(agentRelayProbeModule{service: service}); err != nil {
		t.Fatal(err)
	}
	var logOutput bytes.Buffer
	var observations []struct {
		observation coreapp.HTTPObservation
		values      requestctx.Values
	}
	handler := coreapp.NewHandler(coreapp.Options{
		Audit: func(ctx context.Context, observation coreapp.HTTPObservation) error {
			observations = append(observations, struct {
				observation coreapp.HTTPObservation
				values      requestctx.Values
			}{observation: observation, values: requestctx.TrustedSnapshot(ctx)})
			return nil
		},
		Health: health.Handler{},
		IDGenerator: identity.Generator{
			Reader: bytes.NewReader(make([]byte, 1024)),
		},
		Logger:  logging.New(&logOutput, clock.Fixed{Time: now}),
		Metrics: metrics.New("core", "test"),
		Modules: modules,
	})

	directMe := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	directMe.Header.Set("Authorization", "Bearer "+agentSecret)
	directMeResponse := httptest.NewRecorder()
	handler.ServeHTTP(directMeResponse, directMe)
	if directMeResponse.Code != http.StatusOK {
		t.Fatalf("direct Agent introspection: got %d body=%s",
			directMeResponse.Code, directMeResponse.Body.String())
	}

	businessRequest := httptest.NewRequest(
		http.MethodGet, "/v1/projects/project-1/members", nil,
	)
	businessRequest.Header.Set("Authorization", "Bearer "+agentSecret)
	businessResponse := httptest.NewRecorder()
	handler.ServeHTTP(businessResponse, businessRequest)
	if businessResponse.Code != http.StatusOK {
		t.Fatalf("first-class Agent request: got %d body=%s",
			businessResponse.Code, businessResponse.Body.String())
	}
	if strings.Contains(businessResponse.Body.String(), agentSecret) ||
		strings.Contains(logOutput.String(), agentSecret) {
		t.Fatal("Agent credential leaked")
	}
	var actor map[string]string
	if err := json.Unmarshal(businessResponse.Body.Bytes(), &actor); err != nil {
		t.Fatal(err)
	}
	if actor["actor_id"] != "agent-instance-1" ||
		actor["actor_kind"] != "agent" || actor["project_id"] != "project-1" {
		t.Fatalf("Agent identity was not preserved: %#v", actor)
	}
	last := observations[len(observations)-1]
	if last.observation.Path != "/v1/projects/project-1/members" ||
		last.observation.Status != http.StatusOK ||
		last.values.ActorID != "agent-instance-1" ||
		last.values.ActorKind != "agent" || last.values.ProjectID != "project-1" {
		t.Fatalf("Agent Audit context was lost: %#v", last)
	}

	directPatch := httptest.NewRequest(
		http.MethodPatch, "/v1/auth/me", bytes.NewBufferString(`{}`),
	)
	directPatch.Header.Set("Authorization", "Bearer "+agentSecret)
	directPatch.Header.Set("Content-Type", "application/json")
	directPatchResponse := httptest.NewRecorder()
	handler.ServeHTTP(directPatchResponse, directPatch)
	if directPatchResponse.Code != http.StatusForbidden {
		t.Fatalf("non-GET Agent introspection bypass: got %d",
			directPatchResponse.Code)
	}
}

func TestAgentTokenVerificationRouteRequiresSamePendingAgentAndChallenge(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	store.user = User{
		ID: "admin-user", Status: "active", SystemRole: "admin",
	}
	ordinarySecret := "ordinary-admin-api-token"
	store.tokens[hashToken(ordinarySecret)] = Token{
		ID: "ordinary-admin-token-id", Kind: "api", ProjectID: "project-1",
		TokenHash: hashToken(ordinarySecret), UserID: store.user.ID,
	}
	pendingSecret := "pending-product-agent-token"
	challenge := "mmdash_challenge_pending-product-agent"
	store.agentTokens["pending-token-id"] = AgentToken{
		AgentInstanceID: "agent-1", CreatedAt: now.Add(-time.Minute),
		GrantID: "grant-1", ID: "pending-token-id", ProjectID: "project-1",
		Status: "pending", TokenHash: hashToken(pendingSecret),
		VerificationChallengeHash: hashToken(challenge),
	}
	service := &Service{
		Clock:     clock.Fixed{Time: now},
		Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		Store:     store,
	}
	mux := http.NewServeMux()
	(Module{Service: service}).RegisterRoutes(mux)
	body := []byte(`{
		"project_id":"project-1",
		"agent_instance_id":"agent-1",
		"challenge":"mmdash_challenge_pending-product-agent",
		"mcp_method":"tools/list",
		"mcp_session_id":"mcp-session-1",
		"request_id":"request-1"
	}`)

	ordinary := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/agent-tokens/pending-token-id/verification",
		bytes.NewReader(body),
	)
	ordinary.Header.Set("Authorization", "Bearer "+ordinarySecret)
	ordinary.Header.Set("Content-Type", "application/json")
	ordinaryResponse := httptest.NewRecorder()
	mux.ServeHTTP(ordinaryResponse, ordinary)
	if ordinaryResponse.Code != http.StatusForbidden {
		t.Fatalf("ordinary admin token status: got %d body=%s", ordinaryResponse.Code, ordinaryResponse.Body.String())
	}

	me := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	me.Header.Set("Authorization", "Bearer "+pendingSecret)
	meResponse := httptest.NewRecorder()
	mux.ServeHTTP(meResponse, me)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("pending /auth/me: got %d body=%s", meResponse.Code, meResponse.Body.String())
	}
	if token := store.agentTokens["pending-token-id"]; token.LastUsedAt != nil || token.Verification != nil {
		t.Fatalf("ordinary authentication created evidence: %#v", token)
	}

	pendingVerification := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/agent-tokens/pending-token-id/verification",
		bytes.NewReader(body),
	)
	pendingVerification.Header.Set("Authorization", "Bearer "+pendingSecret)
	pendingVerification.Header.Set("Content-Type", "application/json")
	pendingResponse := httptest.NewRecorder()
	mux.ServeHTTP(pendingResponse, pendingVerification)
	if pendingResponse.Code != http.StatusOK {
		t.Fatalf("pending Agent verification status: got %d body=%s", pendingResponse.Code, pendingResponse.Body.String())
	}
	var evidence AgentTokenVerificationEvidence
	if err := json.Unmarshal(pendingResponse.Body.Bytes(), &evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if evidence.TokenID != "pending-token-id" || evidence.MCPMethod != "tools/list" ||
		evidence.MCPSessionID != "mcp-session-1" || evidence.EvidenceID == "" {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
	if token := store.agentTokens["pending-token-id"]; token.VerificationChallengeHash != "" {
		t.Fatal("verification challenge was not consumed")
	}
}

func TestGenericTokenRouteRejectsCompatibilityAgentKind(t *testing.T) {
	store := newMemoryStore()
	store.user = User{
		ID: "admin-user", Status: "active", SystemRole: "admin",
	}
	secret := "generic-token-route-admin-api-secret"
	store.tokens[hashToken(secret)] = Token{
		ID: "admin-api-token-id", Kind: "api", TokenHash: hashToken(secret),
		UserID: store.user.ID,
	}
	service := &Service{
		Clock:         clock.Fixed{Time: time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)},
		ProjectTokens: projectAuthorizerStub{},
		Store:         store,
	}
	mux := http.NewServeMux()
	(Module{Service: service}).RegisterRoutes(mux)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/tokens",
		bytes.NewBufferString(`{"kind":"agent","name":"legacy generic agent"}`),
	)
	request.Header.Set("Authorization", "Bearer "+secret)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("generic Agent token status: got %d body=%s",
			response.Code, response.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode generic Agent token error: %v", err)
	}
	if body["code"] != "INVALID_REQUEST" {
		t.Fatalf("generic Agent token error: %#v", body)
	}
}
