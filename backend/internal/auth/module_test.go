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

type agentRelayProbeModule struct{}

func (agentRelayProbeModule) Name() string { return "agent-relay-probe" }

func (agentRelayProbeModule) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/projects/project-1/members", func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		values := requestctx.TrustedSnapshot(request.Context())
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]string{
			"actor_id": values.ActorID, "actor_kind": values.ActorKind,
			"project_id": values.ProjectID,
		})
	})
}

func TestProductAgentBusinessRequestsRequireTrustedGatewayAttestation(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	store.user = User{
		ID: "admin-user", Status: "active", SystemRole: "admin",
	}
	agentSecret := "product-agent-secret-must-not-appear-in-errors"
	trustedSecret := "trusted-gateway-secret-must-not-appear-in-errors"
	ordinarySecret := "ordinary-service-secret-must-not-appear-in-errors"
	crossProjectSecret := "cross-project-gateway-secret-must-not-appear-in-errors"
	store.agentTokens["agent-token-id"] = AgentToken{
		AgentInstanceID: "agent-instance-1",
		AllowedTools:    []string{"project.get"},
		GrantID:         "grant-1",
		ID:              "agent-token-id",
		ProjectID:       "project-1",
		Status:          "active",
		TokenHash:       hashToken(agentSecret),
	}
	store.tokens[hashToken(trustedSecret)] = Token{
		ID: "trusted-gateway-token-id", Kind: "api", ProjectID: "project-1",
		TokenHash: hashToken(trustedSecret), UserID: store.user.ID,
	}
	store.tokens[hashToken(ordinarySecret)] = Token{
		ID: "ordinary-service-token-id", Kind: "api", ProjectID: "project-1",
		TokenHash: hashToken(ordinarySecret), UserID: store.user.ID,
	}
	store.tokens[hashToken(crossProjectSecret)] = Token{
		ID: "trusted-gateway-token-id", Kind: "api", ProjectID: "project-2",
		TokenHash: hashToken(crossProjectSecret), UserID: store.user.ID,
	}
	service := &Service{
		AgentVerificationTokenID: "trusted-gateway-token-id",
		Clock:                    clock.Fixed{Time: now},
		Store:                    store,
	}
	modules := module.NewRegistry()
	if err := modules.Register(Module{Service: service}); err != nil {
		t.Fatal(err)
	}
	if err := modules.Register(agentRelayProbeModule{}); err != nil {
		t.Fatal(err)
	}
	var logOutput bytes.Buffer
	var observations []struct {
		observation coreapp.HTTPObservation
		values      requestctx.Values
	}
	handler := coreapp.NewHandler(coreapp.Options{
		AgentRequestGuard: service.AuthorizeAgentRequest,
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

	for name, gatewayAuthorization := range map[string]string{
		"cross_project": "Bearer " + crossProjectSecret,
		"malformed":     "Bearer not-the-configured-gateway-secret",
		"missing":       "",
		"ordinary":      "Bearer " + ordinarySecret,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet, "/v1/projects/project-1/members", nil,
			)
			request.Header.Set("Authorization", "Bearer "+agentSecret)
			if gatewayAuthorization != "" {
				request.Header.Set(AgentGatewayAuthorizationHeader, gatewayAuthorization)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden ||
				!strings.Contains(response.Body.String(), "AGENT_GATEWAY_ATTESTATION_REQUIRED") {
				t.Fatalf("untrusted relay: got %d body=%s",
					response.Code, response.Body.String())
			}
			for _, secret := range []string{
				agentSecret, trustedSecret, ordinarySecret, crossProjectSecret,
			} {
				if strings.Contains(response.Body.String(), secret) ||
					strings.Contains(logOutput.String(), secret) {
					t.Fatalf("credential leaked for %s", name)
				}
			}
		})
	}

	trusted := httptest.NewRequest(
		http.MethodGet, "/v1/projects/project-1/members", nil,
	)
	trusted.Header.Set("Authorization", "Bearer "+agentSecret)
	trusted.Header.Set(
		AgentGatewayAuthorizationHeader, "Bearer "+trustedSecret,
	)
	trustedResponse := httptest.NewRecorder()
	handler.ServeHTTP(trustedResponse, trusted)
	if trustedResponse.Code != http.StatusOK {
		t.Fatalf("trusted relay: got %d body=%s",
			trustedResponse.Code, trustedResponse.Body.String())
	}
	var relayed map[string]string
	if err := json.Unmarshal(trustedResponse.Body.Bytes(), &relayed); err != nil {
		t.Fatal(err)
	}
	if relayed["actor_id"] != "agent-instance-1" ||
		relayed["actor_kind"] != "agent" || relayed["project_id"] != "project-1" {
		t.Fatalf("trusted relay replaced Agent identity: %#v", relayed)
	}
	last := observations[len(observations)-1]
	if last.observation.Path != "/v1/projects/project-1/members" ||
		last.observation.Status != http.StatusOK ||
		last.values.ActorID != "agent-instance-1" ||
		last.values.ActorKind != "agent" || last.values.ProjectID != "project-1" {
		t.Fatalf("trusted Agent Audit context was lost: %#v", last)
	}

	directPatch := httptest.NewRequest(http.MethodPatch, "/v1/auth/me", nil)
	directPatch.Header.Set("Authorization", "Bearer "+agentSecret)
	directPatchResponse := httptest.NewRecorder()
	handler.ServeHTTP(directPatchResponse, directPatch)
	if directPatchResponse.Code != http.StatusForbidden {
		t.Fatalf("non-GET Agent introspection bypass: got %d",
			directPatchResponse.Code)
	}
}

func TestAgentTokenVerificationRouteRequiresDedicatedGatewayCredential(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	store.user = User{
		ID: "admin-user", Status: "active", SystemRole: "admin",
	}
	trustedSecret := "trusted-gateway-core-api-token"
	ordinarySecret := "ordinary-admin-api-token"
	store.tokens[hashToken(trustedSecret)] = Token{
		ID: "trusted-gateway-token-id", Kind: "api", ProjectID: "project-1",
		TokenHash: hashToken(trustedSecret), UserID: store.user.ID,
	}
	store.tokens[hashToken(ordinarySecret)] = Token{
		ID: "ordinary-admin-token-id", Kind: "api", ProjectID: "project-1",
		TokenHash: hashToken(ordinarySecret), UserID: store.user.ID,
	}
	pendingSecret := "pending-product-agent-token"
	store.agentTokens["pending-token-id"] = AgentToken{
		AgentInstanceID: "agent-1", CreatedAt: now.Add(-time.Minute),
		GrantID: "grant-1", ID: "pending-token-id", ProjectID: "project-1",
		Status: "pending", TokenHash: hashToken(pendingSecret),
	}
	service := &Service{
		AgentVerificationTokenID: "trusted-gateway-token-id",
		Clock:                    clock.Fixed{Time: now},
		Generator:                identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		Store:                    store,
	}
	mux := http.NewServeMux()
	(Module{Service: service}).RegisterRoutes(mux)
	body := []byte(`{
		"project_id":"project-1",
		"agent_instance_id":"agent-1",
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

	trusted := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/agent-tokens/pending-token-id/verification",
		bytes.NewReader(body),
	)
	trusted.Header.Set("Authorization", "Bearer "+trustedSecret)
	trusted.Header.Set("Content-Type", "application/json")
	trustedResponse := httptest.NewRecorder()
	mux.ServeHTTP(trustedResponse, trusted)
	if trustedResponse.Code != http.StatusOK {
		t.Fatalf("trusted Gateway status: got %d body=%s", trustedResponse.Code, trustedResponse.Body.String())
	}
	var evidence AgentTokenVerificationEvidence
	if err := json.Unmarshal(trustedResponse.Body.Bytes(), &evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if evidence.TokenID != "pending-token-id" || evidence.MCPMethod != "tools/list" ||
		evidence.MCPSessionID != "mcp-session-1" || evidence.EvidenceID == "" {
		t.Fatalf("unexpected evidence: %#v", evidence)
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
