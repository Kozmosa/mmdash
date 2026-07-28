package datahub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	reviewed bool
}

func (stub *storeStub) CreateProposal(
	context.Context, string, string, CreateProposalInput,
) (ContextProposal, error) {
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
