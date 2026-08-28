package datahub

import (
	"context"
	"encoding/json"
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

func TestProgressReferenceValidationSkipsEmptyInput(t *testing.T) {
	valid, err := (PostgresStore{}).ValidateProgressReferences(context.Background(), nil, "project-id", nil)
	if err != nil || !valid {
		t.Fatalf("empty Progress references should not query: valid=%v err=%v", valid, err)
	}
}

func TestEvaluationFactsExcludeSelfGeneratedProgressHistoryAndTimestamps(t *testing.T) {
	now := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	objects := stableEvaluationObjects([]Object{
		{ID: "task-object", ObjectType: "task", SourceModule: "progress", SourceID: "task-1", Title: "Task", Summary: "progress.task.updated", Status: "done", Version: 2, Metadata: map[string]interface{}{"source": "agent"}, CreatedAt: now, OccurredAt: now, UpdatedAt: now},
		{ID: "evaluation-object", ObjectType: "progress_evaluation", SourceModule: "progress", SourceID: "evaluation-1"},
		{ID: "risk-object", ObjectType: "progress_risk", SourceModule: "progress", SourceID: "risk-1"},
	})
	if len(objects) != 1 || objects[0]["object_id"] != "task-object" {
		t.Fatalf("unexpected stable evaluation objects: %#v", objects)
	}
	for _, volatile := range []string{"created_at", "occurred_at", "updated_at"} {
		if _, exists := objects[0][volatile]; exists {
			t.Fatalf("stable evaluation object retained %s: %#v", volatile, objects[0])
		}
	}
	activity := stableEvaluationActivity([]Activity{
		{ID: "task-activity", ActivityType: "progress.task.updated", ObjectID: "task-object", Title: "Task", Summary: "done", CreatedAt: now, OccurredAt: now},
		{ID: "evaluation-activity", ActivityType: "progress.evaluation.completed"},
		{ID: "risk-activity", ActivityType: "progress.risk.detected"},
	})
	if len(activity) != 1 || activity[0]["activity_id"] != "task-activity" {
		t.Fatalf("unexpected stable evaluation activity: %#v", activity)
	}
	for _, volatile := range []string{"created_at", "occurred_at"} {
		if _, exists := activity[0][volatile]; exists {
			t.Fatalf("stable evaluation activity retained %s: %#v", volatile, activity[0])
		}
	}
}

func TestEvaluationEvidenceSeedIsMinimalNavigableAndSemantic(t *testing.T) {
	project := map[string]interface{}{
		"project_id": "project-1", "problem_title": "Optimize a model",
	}
	objects := []map[string]interface{}{
		{"object_id": "object-1", "object_type": "model_snapshot", "version": 1},
		{"object_id": "object-2", "object_type": "repo_commit", "version": 2},
		{"object_id": "object-3", "object_type": "repo_commit", "version": 3},
	}
	seed, err := evaluationEvidenceSeed(
		"project-1", project, objects,
		[]map[string]interface{}{{"activity_id": "activity-1"}},
		[]map[string]interface{}{{"context_id": "context-1", "content": "confirmed"}},
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if seed["facts_schema_version"] != 2 {
		t.Fatalf("unexpected facts schema version: %#v", seed)
	}
	for _, forbidden := range []string{"data_objects", "recent_activity", "confirmed_context"} {
		if _, exists := seed[forbidden]; exists {
			t.Fatalf("minimal evaluation seed leaked %s: %#v", forbidden, seed)
		}
	}
	if !reflect.DeepEqual(seed["project"], map[string]interface{}{"project_id": "project-1"}) {
		t.Fatalf("evaluation seed leaked Project content: %#v", seed["project"])
	}
	catalog, ok := seed["evidence_catalog"].(map[string]interface{})
	if !ok {
		t.Fatalf("evaluation seed lost its evidence catalog: %#v", seed)
	}
	if !reflect.DeepEqual(catalog["available_object_types"], []string{"model_snapshot", "repo_commit"}) ||
		!reflect.DeepEqual(catalog["object_type_counts"], map[string]int{"model_snapshot": 1, "repo_commit": 2}) ||
		catalog["catalog_truncated"] != true {
		t.Fatalf("unexpected evidence catalog: %#v", catalog)
	}
	firstRevision, _ := catalog["revision"].(string)
	if len(firstRevision) != 64 {
		t.Fatalf("evaluation evidence revision is not SHA-256: %q", firstRevision)
	}
	changed, err := evaluationEvidenceSeed(
		"project-1", project,
		[]map[string]interface{}{
			{"object_id": "object-1", "object_type": "model_snapshot", "version": 2},
			objects[1], objects[2],
		},
		[]map[string]interface{}{{"activity_id": "activity-1"}},
		[]map[string]interface{}{{"context_id": "context-1", "content": "confirmed"}},
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	changedCatalog := changed["evidence_catalog"].(map[string]interface{})
	if changedCatalog["revision"] == firstRevision {
		t.Fatalf("semantic evidence change did not change the revision: %#v", changedCatalog)
	}
}

type accessStub struct {
	authorized []project.Permission
	err        error
	identity   auth.Identity
}

func (stub *accessStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return stub.identity, stub.err
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
	contexts       []ContextEntry
	createdActor   ProposalActor
	createdInput   CreateProposalInput
	createdResult  ContextProposal
	proposals      []ContextProposal
	reviewed       bool
	reviewedResult ContextProposal
}

type problemProviderStub struct {
	items []interface{}
}

type progressProviderStub struct {
	milestones []interface{}
	tracking   interface{}
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

func (stub progressProviderStub) ProgressHomeTracking(
	context.Context,
	auth.Identity,
	string,
) (interface{}, error) {
	if stub.tracking == nil {
		return map[string]interface{}{}, nil
	}
	return stub.tracking, nil
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
	if stub.createdResult.ID != "" {
		return stub.createdResult, nil
	}
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
	if stub.contexts != nil {
		return stub.contexts, nil
	}
	return []ContextEntry{}, nil
}
func (stub *storeStub) ListObjects(
	context.Context, string, string, pagination.Request,
) (ObjectPage, error) {
	return ObjectPage{Items: []Object{}}, nil
}
func (stub *storeStub) ListProposals(context.Context, string) ([]ContextProposal, error) {
	if stub.proposals != nil {
		return stub.proposals, nil
	}
	return []ContextProposal{}, nil
}
func (stub *storeStub) ReviewProposal(
	context.Context, string, string, string, ReviewProposalInput,
) (ContextProposal, error) {
	stub.reviewed = true
	if stub.reviewedResult.ID != "" {
		return stub.reviewedResult, nil
	}
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
	if home.ProgressTracking == nil {
		t.Fatal("home Progress tracking shell must be non-nil")
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
			tracking:   map[string]interface{}{"effective_stage": "execution"},
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
	if home.ProgressTracking.(map[string]interface{})["effective_stage"] != "execution" {
		t.Fatalf("Progress tracking was not aggregated into Home: %#v", home.ProgressTracking)
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

func TestModuleContextResponsesMatchOpenAPIActorProvenance(t *testing.T) {
	const (
		agentID    = "00000000-0000-4000-8000-000000000001"
		contextID  = "00000000-0000-4000-8000-000000000002"
		projectID  = "00000000-0000-4000-8000-000000000003"
		proposalID = "00000000-0000-4000-8000-000000000004"
		reviewerID = "00000000-0000-4000-8000-000000000005"
		runID      = "00000000-0000-4000-8000-000000000006"
		sessionID  = "00000000-0000-4000-8000-000000000007"
	)
	now := time.Date(2026, time.August, 9, 8, 30, 0, 0, time.UTC)
	pending := ContextProposal{
		AgentRunID: runID, AgentSessionID: sessionID,
		Content: "Agent finding", ContextType: "finding", CreatedAt: now,
		ID: proposalID, ProjectID: projectID, ProposedBy: agentID,
		ProposedByActorID: agentID, ProposedByActorKind: "agent",
		Rationale: "Run evidence", ReviewNote: "", SourceObjectIDs: []string{},
		Status: "pending", Title: "Candidate context", UpdatedAt: now,
	}
	reviewedAt := now.Add(time.Minute)
	reviewed := pending
	reviewed.PromotedContext = contextID
	reviewed.ReviewedAt = &reviewedAt
	reviewed.ReviewedBy = reviewerID
	reviewed.ReviewNote = "Approved"
	reviewed.Status = "accepted"
	reviewed.UpdatedAt = reviewedAt
	store := &storeStub{
		contexts: []ContextEntry{{
			ConfirmedAt: reviewedAt, ConfirmedBy: reviewerID,
			Content: pending.Content, ContextType: pending.ContextType,
			CreatedAt: reviewedAt, ID: contextID, ProjectID: projectID,
			ProposedBy: agentID, ProposedByActorKind: "agent",
			SourceObjectIDs: []string{}, Title: pending.Title, UpdatedAt: reviewedAt,
		}},
		createdResult:  pending,
		proposals:      []ContextProposal{pending},
		reviewedResult: reviewed,
	}
	agentModule := Module{Service: Service{
		Access: &accessStub{identity: auth.Identity{
			AgentInstanceID: agentID, AllowedTools: []string{"context.promote"},
			CredentialStatus: "active", Kind: "agent", ProjectID: projectID,
		}},
		AgentProvenance: &provenanceValidatorStub{},
		Store:           store,
	}}
	humanModule := Module{Service: Service{
		Access: &accessStub{identity: auth.Identity{
			Kind: "session", User: auth.User{ID: reviewerID},
		}},
		Store: store,
	}}
	agentMux, humanMux := http.NewServeMux(), http.NewServeMux()
	agentModule.RegisterRoutes(agentMux)
	humanModule.RegisterRoutes(humanMux)

	proposalKeys := []string{
		"agent_run_id", "agent_session_id", "content", "context_type",
		"created_at", "proposal_id", "project_id", "proposed_by",
		"proposed_by_actor_id", "proposed_by_actor_kind", "rationale",
		"review_note", "source_object_ids", "status", "title", "updated_at",
	}

	t.Run("create returns the explicit Agent actor", func(t *testing.T) {
		body := `{"title":"Candidate context","content":"Agent finding",` +
			`"context_type":"finding","agent_session_id":"` + sessionID +
			`","agent_run_id":"` + runID + `"}`
		response := serveDataHubJSON(t, agentMux, http.MethodPost,
			"/v1/data/projects/"+projectID+"/context/proposals", body,
			http.StatusCreated)
		assertExactJSONKeys(t, response, proposalKeys)
		assertAgentProposalJSON(t, response, agentID)
	})

	t.Run("list returns the explicit Agent actor", func(t *testing.T) {
		response := serveDataHubJSON(t, agentMux, http.MethodGet,
			"/v1/data/projects/"+projectID+"/context/proposals", "",
			http.StatusOK)
		items, ok := response["items"].([]interface{})
		if !ok || len(items) != 1 {
			t.Fatalf("unexpected proposal list: %#v", response)
		}
		proposal, ok := items[0].(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected proposal response: %#v", items[0])
		}
		assertExactJSONKeys(t, proposal, proposalKeys)
		assertAgentProposalJSON(t, proposal, agentID)
	})

	t.Run("review preserves the Agent actor", func(t *testing.T) {
		response := serveDataHubJSON(t, humanMux, http.MethodPost,
			"/v1/data/projects/"+projectID+"/context/proposals/"+proposalID+"/review",
			`{"decision":"accepted","review_note":"Approved"}`, http.StatusOK)
		assertExactJSONKeys(t, response, append(proposalKeys,
			"promoted_context_id", "reviewed_at", "reviewed_by"))
		assertAgentProposalJSON(t, response, agentID)
		if !store.reviewed || response["promoted_context_id"] != contextID {
			t.Fatalf("proposal was not reviewed and promoted: %#v", response)
		}
	})

	t.Run("promoted context omits internal actor kind", func(t *testing.T) {
		response := serveDataHubJSON(t, humanMux, http.MethodGet,
			"/v1/data/projects/"+projectID+"/context", "", http.StatusOK)
		items, ok := response["items"].([]interface{})
		if !ok || len(items) != 1 {
			t.Fatalf("unexpected context list: %#v", response)
		}
		entry, ok := items[0].(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected context response: %#v", items[0])
		}
		assertExactJSONKeys(t, entry, []string{
			"confirmed_at", "confirmed_by", "content", "context_id", "context_type",
			"created_at", "project_id", "proposed_by", "source_object_ids", "title",
			"updated_at",
		})
		if entry["proposed_by"] != agentID {
			t.Fatalf("promoted context lost Agent provenance: %#v", entry)
		}
	})
}

func serveDataHubJSON(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body string,
	wantStatus int,
) map[string]interface{} {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != wantStatus {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v: %s", err, response.Body.String())
	}
	return decoded
}

func assertExactJSONKeys(t *testing.T, value map[string]interface{}, want []string) {
	t.Helper()
	if len(value) != len(want) {
		t.Fatalf("unexpected JSON fields: got %#v, want %v", value, want)
	}
	for _, key := range want {
		if _, ok := value[key]; !ok {
			t.Fatalf("JSON response is missing %q: %#v", key, value)
		}
	}
}

func assertAgentProposalJSON(
	t *testing.T,
	proposal map[string]interface{},
	agentID string,
) {
	t.Helper()
	if proposal["proposed_by"] != agentID ||
		proposal["proposed_by_actor_id"] != agentID ||
		proposal["proposed_by_actor_kind"] != "agent" {
		t.Fatalf("Agent provenance is incomplete: %#v", proposal)
	}
	if _, leaked := proposal["proposed_by_kind"]; leaked {
		t.Fatalf("undocumented proposed_by_kind leaked: %#v", proposal)
	}
}
