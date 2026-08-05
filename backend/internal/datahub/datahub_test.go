package datahub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type accessStub struct {
	authorized []project.Permission
	err        error
}

func (stub *accessStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return auth.Identity{}, stub.err
}

func (stub *accessStub) Authorize(
	_ context.Context,
	_ auth.Identity,
	_ string,
	permission project.Permission,
) error {
	stub.authorized = append(stub.authorized, permission)
	return stub.err
}

type storeStub struct {
	createdActor ProposalActor
	createdInput CreateProposalInput
	reviewed     bool
}

type problemProviderStub struct {
	items []interface{}
}

type progressProviderStub struct {
	milestones []interface{}
	todos      []interface{}
}

type agentProviderStub struct {
	items []interface{}
}

type provenanceValidatorStub struct {
	agentInstanceID string
	err             error
	projectID       string
	runID           string
	sessionID       string
}

func (stub problemProviderStub) ProblemItems(
	context.Context,
	auth.Identity,
	string,
) ([]interface{}, error) {
	return stub.items, nil
}

func (stub progressProviderStub) ProgressHomeItems(
	context.Context,
	auth.Identity,
	string,
) ([]interface{}, []interface{}, error) {
	return stub.milestones, stub.todos, nil
}

func (stub agentProviderStub) AgentHomeItems(
	context.Context,
	auth.Identity,
	string,
) ([]interface{}, error) {
	return stub.items, nil
}

func (stub *provenanceValidatorStub) ValidateProvenance(
	_ context.Context,
	agentInstanceID string,
	projectID string,
	sessionID string,
	runID string,
) error {
	stub.agentInstanceID = agentInstanceID
	stub.projectID = projectID
	stub.sessionID = sessionID
	stub.runID = runID
	return stub.err
}

func (stub *storeStub) CreateProposal(
	_ context.Context, _ string, actor ProposalActor, input CreateProposalInput,
) (ContextProposal, error) {
	stub.createdActor = actor
	stub.createdInput = input
	return ContextProposal{ID: "proposal"}, nil
}
func (stub *storeStub) GetContext(context.Context, string, string) (ContextEntry, error) {
	return ContextEntry{}, nil
}
func (stub *storeStub) GetObject(context.Context, string, string) (Object, error) {
	return Object{ObjectType: "project"}, nil
}
func (stub *storeStub) GetProposal(context.Context, string, string) (ContextProposal, error) {
	return ContextProposal{}, nil
}
func (stub *storeStub) ListActivity(
	context.Context, string, pagination.Request,
) (ActivityPage, error) {
	return ActivityPage{Items: []Activity{}}, nil
}
func (stub *storeStub) ListContext(context.Context, string) ([]ContextEntry, error) {
	return []ContextEntry{}, nil
}
func (stub *storeStub) ListObjects(
	context.Context, string, string, pagination.Request,
) (ObjectPage, error) {
	return ObjectPage{Items: []Object{}}, nil
}
func (stub *storeStub) ListProposals(context.Context, string) ([]ContextProposal, error) {
	return []ContextProposal{}, nil
}
func (stub *storeStub) ReviewProposal(
	context.Context, string, string, string, ReviewProposalInput,
) (ContextProposal, error) {
	stub.reviewed = true
	return ContextProposal{Status: "accepted"}, nil
}

func TestAdapterRegistryRoutesByObjectType(t *testing.T) {
	registry := NewAdapterRegistry()
	if err := registry.Register("project", ReaderFunc(
		func(context.Context, auth.Identity, Object) (interface{}, error) {
			return map[string]string{"source": "project"}, nil
		},
	)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("project", ReaderFunc(
		func(context.Context, auth.Identity, Object) (interface{}, error) {
			return nil, nil
		},
	)); err == nil {
		t.Fatal("expected duplicate adapter registration to fail")
	}
	result, err := registry.Read(
		context.Background(), auth.Identity{}, Object{ObjectType: "project"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, map[string]string{"source": "project"}) {
		t.Fatalf("unexpected adapter result: %#v", result)
	}
	if _, err := registry.Read(
		context.Background(), auth.Identity{}, Object{ObjectType: "unknown"},
	); !errors.Is(err, ErrAdapterNotFound) {
		t.Fatalf("expected adapter-not-found, got %v", err)
	}
}

func TestProjectionRegistryDeclaresPatternsAndRoutes(t *testing.T) {
	registry := NewProjectionRegistry()
	var projected string
	if err := registry.Register("project.created", ProjectorFunc(
		func(_ context.Context, event contract.EventEnvelope) error {
			projected = event.EventType
			return nil
		},
	)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("project.updated", ProjectorFunc(
		func(context.Context, contract.EventEnvelope) error { return nil },
	)); err != nil {
		t.Fatal(err)
	}
	if got := registry.Patterns(); !reflect.DeepEqual(
		got, []string{"project.created", "project.updated"},
	) {
		t.Fatalf("patterns are not deterministic: %#v", got)
	}
	if err := registry.Handle(context.Background(), contract.EventEnvelope{
		EventType: "project.created",
	}); err != nil {
		t.Fatal(err)
	}
	if projected != "project.created" {
		t.Fatalf("event was not projected: %q", projected)
	}
}

func TestOnlyHumanSessionCanReviewContext(t *testing.T) {
	access := &accessStub{}
	store := &storeStub{}
	service := Service{Access: access, Store: store}
	agent := auth.Identity{
		Kind: "api",
		User: auth.User{ID: "00000000-0000-4000-8000-000000000001"},
	}
	_, err := service.ReviewProposal(
		context.Background(), agent, "project", "proposal",
		ReviewProposalInput{Decision: "accepted"},
	)
	if !errors.Is(err, ErrHumanRequired) || store.reviewed {
		t.Fatalf("agent review must be rejected before persistence: %v", err)
	}
	human := agent
	human.Kind = "session"
	if _, err := service.ReviewProposal(
		context.Background(), human, "project", "proposal",
		ReviewProposalInput{Decision: "accepted"},
	); err != nil {
		t.Fatal(err)
	}
	if !store.reviewed ||
		!reflect.DeepEqual(access.authorized, []project.Permission{PermissionContextReview}) {
		t.Fatalf("human review did not use review permission: %#v", access.authorized)
	}
}

func TestAgentContextProposalRequiresExactToolAndPairedOwnedProvenance(t *testing.T) {
	identity := auth.Identity{
		AgentInstanceID:  "agent-1",
		AllowedTools:     []string{"context.promote"},
		CredentialStatus: "active",
		Kind:             "agent",
		ProjectID:        "project-1",
	}
	input := CreateProposalInput{
		AgentRunID:      " run-1 ",
		AgentSessionID:  " session-1 ",
		Content:         " conclusion ",
		ContextType:     " finding ",
		Rationale:       " evidence ",
		SourceObjectIDs: []string{" object-1 ", "object-1", "object-2"},
		Title:           " result ",
	}

	t.Run("accepts an owned pair and records the Agent actor", func(t *testing.T) {
		store := &storeStub{}
		provenance := &provenanceValidatorStub{}
		service := Service{
			Access:          &accessStub{},
			AgentProvenance: provenance,
			Store:           store,
		}
		if _, err := service.CreateProposal(
			context.Background(), identity, "project-1", input,
		); err != nil {
			t.Fatal(err)
		}
		if provenance.agentInstanceID != "agent-1" ||
			provenance.projectID != "project-1" ||
			provenance.sessionID != "session-1" || provenance.runID != "run-1" {
			t.Fatalf("provenance was not checked against the caller: %#v", provenance)
		}
		if store.createdActor.ID != "agent-1" ||
			store.createdActor.Kind != "agent" || store.createdActor.UserID != "" ||
			store.createdActor.AgentSessionID != "session-1" ||
			store.createdActor.AgentRunID != "run-1" {
			t.Fatalf("unexpected persisted actor: %#v", store.createdActor)
		}
		if !reflect.DeepEqual(
			store.createdInput.SourceObjectIDs,
			[]string{"object-1", "object-2"},
		) {
			t.Fatalf("source IDs were not normalized: %#v", store.createdInput.SourceObjectIDs)
		}
	})

	t.Run("rejects a half-filled provenance pair", func(t *testing.T) {
		service := Service{Access: &accessStub{}, Store: &storeStub{}}
		half := input
		half.AgentRunID = ""
		if _, err := service.CreateProposal(
			context.Background(), identity, "project-1", half,
		); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected invalid provenance pair, got %v", err)
		}
	})

	t.Run("rejects missing or mismatched ownership validation", func(t *testing.T) {
		for name, validator := range map[string]AgentProvenanceValidator{
			"missing":  nil,
			"mismatch": &provenanceValidatorStub{err: errors.New("not owned")},
		} {
			t.Run(name, func(t *testing.T) {
				service := Service{
					Access: &accessStub{}, AgentProvenance: validator, Store: &storeStub{},
				}
				if _, err := service.CreateProposal(
					context.Background(), identity, "project-1", input,
				); !errors.Is(err, ErrForbidden) {
					t.Fatalf("expected forbidden provenance, got %v", err)
				}
			})
		}
	})

	t.Run("rejects a credential without context.promote", func(t *testing.T) {
		denied := identity
		denied.AllowedTools = []string{"data.read"}
		access := &accessStub{}
		if _, err := (Service{Access: access, Store: &storeStub{}}).CreateProposal(
			context.Background(), denied, "project-1", CreateProposalInput{
				Content: "content", ContextType: "finding", Title: "title",
			},
		); !errors.Is(err, ErrForbidden) || len(access.authorized) != 0 {
			t.Fatalf("tool denial must precede project access: %v %#v", err, access.authorized)
		}
	})
}

func TestNonAgentCannotForgeAgentContextProvenance(t *testing.T) {
	store := &storeStub{}
	service := Service{Access: &accessStub{}, Store: store}
	identity := auth.Identity{
		Kind: "session",
		User: auth.User{ID: "00000000-0000-4000-8000-000000000001"},
	}
	_, err := service.CreateProposal(
		context.Background(), identity, "project-1", CreateProposalInput{
			AgentRunID: "run-1", AgentSessionID: "session-1",
			Content: "content", ContextType: "finding", Title: "title",
		},
	)
	if !errors.Is(err, ErrForbidden) || store.createdActor.ID != "" {
		t.Fatalf("non-Agent provenance forgery reached persistence: %v %#v", err, store.createdActor)
	}
}

func TestContextProposalRejectsMoreThanContractSourceLimit(t *testing.T) {
	sourceIDs := make([]string, 101)
	for index := range sourceIDs {
		sourceIDs[index] = "source-" + strconv.Itoa(index)
	}
	service := Service{Access: &accessStub{}, Store: &storeStub{}}
	_, err := service.CreateProposal(
		context.Background(), auth.Identity{
			Kind: "session", User: auth.User{ID: "00000000-0000-4000-8000-000000000001"},
		}, "project-1", CreateProposalInput{
			Content: "content", ContextType: "finding",
			SourceObjectIDs: sourceIDs, Title: "title",
		},
	)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected maxItems=100 enforcement, got %v", err)
	}
}

func TestHomeAggregateIsTypedEmptyShell(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	service := Service{
		Access: &accessStub{},
		Clock:  clock.Fixed{Time: now},
		Store:  &storeStub{},
	}
	home, err := service.Home(
		context.Background(), auth.Identity{}, "project-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	sections := []HomeSection{
		home.Problem, home.Milestones, home.Todos, home.Models,
		home.Experiments, home.Article, home.Agent,
	}
	for _, section := range sections {
		if section.Available || section.Total != 0 || section.Items == nil {
			t.Fatalf("home section must be a non-nil empty shell: %#v", section)
		}
	}
	if home.ProjectID != "project-1" || !home.GeneratedAt.Equal(now) {
		t.Fatalf("unexpected home shell: %#v", home)
	}
}

func TestHomeAggregateIncludesValidatedProblemArtifacts(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := Service{
		Access: &accessStub{}, Clock: clock.Fixed{Time: now},
		Problem: problemProviderStub{items: []interface{}{
			map[string]interface{}{"artifact_id": "artifact-1"},
		}},
		Store: &storeStub{},
	}
	home, err := service.Home(
		context.Background(), auth.Identity{}, "project-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !home.Problem.Available ||
		home.Problem.Total != 1 ||
		len(home.Problem.Items) != 1 {
		t.Fatalf("unexpected Problem section: %#v", home.Problem)
	}
	if home.Models.Available || home.Models.Items == nil {
		t.Fatalf("future sections must remain typed empty shells: %#v", home.Models)
	}
}

func TestHomeAggregateIncludesProgressItemsAndKeepsFutureSectionsEmpty(t *testing.T) {
	service := Service{
		Access: &accessStub{}, Clock: clock.Fixed{Time: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)},
		Progress: progressProviderStub{
			milestones: []interface{}{map[string]interface{}{"milestone_id": "milestone-1"}},
			todos:      []interface{}{map[string]interface{}{"task_id": "task-1"}},
		},
		Store: &storeStub{},
	}
	home, err := service.Home(context.Background(), auth.Identity{}, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if !home.Milestones.Available || home.Milestones.Total != 1 || !home.Todos.Available || home.Todos.Total != 1 {
		t.Fatalf("progress was not aggregated into Home: %#v", home)
	}
	if home.Models.Available || home.Experiments.Available || home.Article.Available || home.Agent.Available {
		t.Fatalf("future home sections must remain empty: %#v", home)
	}
}

func TestHomeAggregateIncludesAgentItems(t *testing.T) {
	service := Service{
		Access: &accessStub{}, Agent: agentProviderStub{items: []interface{}{
			map[string]interface{}{"agent_instance_id": "agent-1", "status": "active"},
		}},
		Clock: clock.Fixed{Time: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)},
		Store: &storeStub{},
	}
	home, err := service.Home(context.Background(), auth.Identity{}, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if !home.Agent.Available || home.Agent.Total != 1 || len(home.Agent.Items) != 1 {
		t.Fatalf("Agent provider was not aggregated: %#v", home.Agent)
	}
	if home.Models.Available || home.Experiments.Available || home.Article.Available {
		t.Fatalf("future home sections must remain typed empty shells: %#v", home)
	}
}

func TestModuleExposesHomeAggregateRoute(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	module := Module{Service: Service{
		Access: &accessStub{},
		Clock:  clock.Fixed{Time: now},
		Store:  &storeStub{},
	}}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux)
	request := httptest.NewRequest(
		http.MethodGet, "/v1/data/projects/project-1/home", nil,
	)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	for _, fragment := range []string{
		`"project_id":"project-1"`,
		`"problem":{"available":false,"items":[],"total":0}`,
		`"agent":{"available":false,"items":[],"total":0}`,
	} {
		if !strings.Contains(response.Body.String(), fragment) {
			t.Fatalf("home response is missing %s: %s", fragment, response.Body.String())
		}
	}
}
