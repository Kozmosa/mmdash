// Package datahub owns cross-module object projections, activity, and
// human-confirmed project context. Authoritative content remains in its domain.
package datahub

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/project"
)

const (
	PermissionDataRead       project.Permission = "project.data.read"
	PermissionContextPropose project.Permission = "project.context.propose"
	PermissionContextReview  project.Permission = "project.context.review"
)

// Object is a stable, searchable cross-module projection.
type Object struct {
	CreatedAt    time.Time              `json:"created_at"`
	ID           string                 `json:"object_id"`
	Metadata     map[string]interface{} `json:"metadata"`
	ObjectType   string                 `json:"object_type"`
	OccurredAt   time.Time              `json:"occurred_at"`
	ProjectID    string                 `json:"project_id"`
	SourceID     string                 `json:"source_id"`
	SourceModule string                 `json:"source_module"`
	Status       string                 `json:"status"`
	Summary      string                 `json:"summary"`
	Title        string                 `json:"title"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Version      int64                  `json:"version"`
}

// Activity is one immutable project timeline projection.
type Activity struct {
	ActivityType string                 `json:"activity_type"`
	Actor        map[string]string      `json:"actor"`
	CreatedAt    time.Time              `json:"created_at"`
	ID           string                 `json:"activity_id"`
	Metadata     map[string]interface{} `json:"metadata"`
	ObjectID     string                 `json:"object_id,omitempty"`
	OccurredAt   time.Time              `json:"occurred_at"`
	ProjectID    string                 `json:"project_id"`
	Summary      string                 `json:"summary"`
	Title        string                 `json:"title"`
}

// ContextProposal is an untrusted suggestion awaiting human review.
type ContextProposal struct {
	AgentRunID          string     `json:"agent_run_id,omitempty"`
	AgentSessionID      string     `json:"agent_session_id,omitempty"`
	Content             string     `json:"content"`
	ContextType         string     `json:"context_type"`
	CreatedAt           time.Time  `json:"created_at"`
	ID                  string     `json:"proposal_id"`
	ProjectID           string     `json:"project_id"`
	PromotedContext     string     `json:"promoted_context_id,omitempty"`
	ProposedBy          string     `json:"proposed_by"`
	ProposedByActorID   string     `json:"proposed_by_actor_id,omitempty"`
	ProposedByActorKind string     `json:"proposed_by_actor_kind,omitempty"`
	Rationale           string     `json:"rationale"`
	ReviewedAt          *time.Time `json:"reviewed_at,omitempty"`
	ReviewedBy          string     `json:"reviewed_by,omitempty"`
	ReviewNote          string     `json:"review_note"`
	SourceObjectIDs     []string   `json:"source_object_ids"`
	Status              string     `json:"status"`
	Title               string     `json:"title"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// ContextEntry is project context made formal by a human reviewer.
type ContextEntry struct {
	ConfirmedAt         time.Time `json:"confirmed_at"`
	ConfirmedBy         string    `json:"confirmed_by"`
	Content             string    `json:"content"`
	ContextType         string    `json:"context_type"`
	CreatedAt           time.Time `json:"created_at"`
	ID                  string    `json:"context_id"`
	ProjectID           string    `json:"project_id"`
	ProposedBy          string    `json:"proposed_by"`
	ProposedByActorKind string    `json:"-"`
	SourceObjectIDs     []string  `json:"source_object_ids"`
	Title               string    `json:"title"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// ObjectPage and ActivityPage expose opaque continuation cursors.
type ObjectPage struct {
	HasMore    bool     `json:"has_more"`
	Items      []Object `json:"items"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

type ActivityPage struct {
	HasMore    bool       `json:"has_more"`
	Items      []Activity `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// HomeSection is a typed extension point; business cards arrive later.
type HomeSection struct {
	Available bool          `json:"available"`
	Items     []interface{} `json:"items"`
	Total     int           `json:"total"`
}

// HomeAggregate is the stable, intentionally empty project-home DTO shell.
type HomeAggregate struct {
	Agent            HomeSection `json:"agent"`
	Article          HomeSection `json:"article"`
	Experiments      HomeSection `json:"experiments"`
	GeneratedAt      time.Time   `json:"generated_at"`
	Milestones       HomeSection `json:"milestones"`
	Models           HomeSection `json:"models"`
	Problem          HomeSection `json:"problem"`
	ProgressTracking interface{} `json:"progress_tracking"`
	ProjectID        string      `json:"project_id"`
	Todos            HomeSection `json:"todos"`
}

type CreateProposalInput struct {
	AgentRunID      string
	AgentSessionID  string
	Content         string
	ContextType     string
	Rationale       string
	SourceObjectIDs []string
	Title           string
}

type ReviewProposalInput struct {
	Decision string
	Note     string
}

// ProposalActor records the independent caller identity without requiring an
// Agent instance to exist in auth_users. UserID is populated only for human or
// generic API credentials whose legacy foreign key remains valid.
type ProposalActor struct {
	AgentRunID     string
	AgentSessionID string
	ID             string
	Kind           string
	UserID         string
}

// Store is the Data Hub persistence boundary.
type Store interface {
	CreateProposal(context.Context, string, ProposalActor, CreateProposalInput) (ContextProposal, error)
	GetContext(context.Context, string, string) (ContextEntry, error)
	GetObject(context.Context, string, string) (Object, error)
	GetProposal(context.Context, string, string) (ContextProposal, error)
	ListActivity(context.Context, string, pagination.Request) (ActivityPage, error)
	ListContext(context.Context, string) ([]ContextEntry, error)
	ListObjects(context.Context, string, string, pagination.Request) (ObjectPage, error)
	ListProposals(context.Context, string) ([]ContextProposal, error)
	ReviewProposal(context.Context, string, string, string, ReviewProposalInput) (ContextProposal, error)
}

// Access is implemented by the authoritative Project service.
type Access interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

type AgentProvenanceValidator interface {
	ValidateProvenance(context.Context, string, string, string, string) error
}

// Reader resolves full authoritative content for one registered object type.
type Reader interface {
	Read(context.Context, auth.Identity, Object) (interface{}, error)
}

// ReaderFunc adapts a function into a Reader.
type ReaderFunc func(context.Context, auth.Identity, Object) (interface{}, error)

func (reader ReaderFunc) Read(
	ctx context.Context,
	identity auth.Identity,
	object Object,
) (interface{}, error) {
	return reader(ctx, identity, object)
}

// ProblemProvider resolves the Project's validated source Artifact cards.
type ProblemProvider interface {
	ProblemItems(context.Context, auth.Identity, string) ([]interface{}, error)
}

type ProgressProvider interface {
	ProgressHomeItems(context.Context, auth.Identity, string) ([]interface{}, []interface{}, error)
	ProgressHomeTracking(context.Context, auth.Identity, string) (interface{}, error)
}

type ModelProvider interface {
	ModelHomeItems(context.Context, auth.Identity, string) ([]interface{}, error)
}

type AgentProvider interface {
	AgentHomeItems(context.Context, auth.Identity, string) ([]interface{}, error)
}

// AdapterRegistry maps stable object types to domain-owned readers.
type AdapterRegistry struct {
	mu      sync.RWMutex
	readers map[string]Reader
}

func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{readers: map[string]Reader{}}
}

func (registry *AdapterRegistry) Register(objectType string, reader Reader) error {
	objectType = strings.TrimSpace(objectType)
	if objectType == "" || reader == nil {
		return fmt.Errorf("object type and reader are required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.readers[objectType]; exists {
		return fmt.Errorf("object reader already registered: %s", objectType)
	}
	registry.readers[objectType] = reader
	return nil
}

func (registry *AdapterRegistry) Read(
	ctx context.Context,
	identity auth.Identity,
	object Object,
) (interface{}, error) {
	registry.mu.RLock()
	reader := registry.readers[object.ObjectType]
	registry.mu.RUnlock()
	if reader == nil {
		return nil, ErrAdapterNotFound
	}
	return reader.Read(ctx, identity, object)
}

func (registry *AdapterRegistry) Types() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	types := make([]string, 0, len(registry.readers))
	for objectType := range registry.readers {
		types = append(types, objectType)
	}
	sort.Strings(types)
	return types
}

// Service coordinates authorization, routing, and context policy.
type Service struct {
	Access          Access
	Adapters        *AdapterRegistry
	Agent           AgentProvider
	AgentProvenance AgentProvenanceValidator
	Clock           interface{ Now() time.Time }
	Models          ModelProvider
	Problem         ProblemProvider
	Progress        ProgressProvider
	Store           Store
}

func (service Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service Service) ListObjects(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	objectType string,
	page pagination.Request,
) (ObjectPage, error) {
	if err := auth.RequireAgentTool(identity, "data.list"); err != nil {
		return ObjectPage{}, ErrForbidden
	}
	if err := service.Access.Authorize(ctx, identity, projectID, PermissionDataRead); err != nil {
		return ObjectPage{}, err
	}
	normalized, err := page.Normalize()
	if err != nil {
		return ObjectPage{}, ErrInvalid
	}
	return service.Store.ListObjects(ctx, projectID, strings.TrimSpace(objectType), normalized)
}

func (service Service) ReadObject(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	objectID string,
) (map[string]interface{}, error) {
	if err := auth.RequireAgentTool(identity, "data.read"); err != nil {
		return nil, ErrForbidden
	}
	if err := service.Access.Authorize(ctx, identity, projectID, PermissionDataRead); err != nil {
		return nil, err
	}
	object, err := service.Store.GetObject(ctx, projectID, objectID)
	if err != nil {
		return nil, err
	}
	content, err := service.Adapters.Read(ctx, identity, object)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"content": content, "object": object}, nil
}

func (service Service) ListActivity(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	page pagination.Request,
) (ActivityPage, error) {
	if err := auth.RequireAgentTool(identity, "data.list"); err != nil {
		return ActivityPage{}, ErrForbidden
	}
	if err := service.Access.Authorize(ctx, identity, projectID, PermissionDataRead); err != nil {
		return ActivityPage{}, err
	}
	normalized, err := page.Normalize()
	if err != nil {
		return ActivityPage{}, ErrInvalid
	}
	return service.Store.ListActivity(ctx, projectID, normalized)
}

func (service Service) ListContext(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) ([]ContextEntry, error) {
	if err := auth.RequireAgentTool(identity, "data.list"); err != nil {
		return nil, ErrForbidden
	}
	if err := service.Access.Authorize(ctx, identity, projectID, PermissionDataRead); err != nil {
		return nil, err
	}
	return service.Store.ListContext(ctx, projectID)
}

func (service Service) ListProposals(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) ([]ContextProposal, error) {
	if err := auth.RequireAgentTool(identity, "context.promote"); err != nil {
		return nil, ErrForbidden
	}
	if err := service.Access.Authorize(ctx, identity, projectID, PermissionContextPropose); err != nil {
		return nil, err
	}
	return service.Store.ListProposals(ctx, projectID)
}

func (service Service) CreateProposal(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	input CreateProposalInput,
) (ContextProposal, error) {
	if err := auth.RequireAgentTool(identity, "context.promote"); err != nil {
		return ContextProposal{}, ErrForbidden
	}
	if err := service.Access.Authorize(ctx, identity, projectID, PermissionContextPropose); err != nil {
		return ContextProposal{}, err
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	input.ContextType = strings.TrimSpace(input.ContextType)
	input.Rationale = strings.TrimSpace(input.Rationale)
	input.AgentSessionID = strings.TrimSpace(input.AgentSessionID)
	input.AgentRunID = strings.TrimSpace(input.AgentRunID)
	if input.Title == "" || input.Content == "" || input.ContextType == "" {
		return ContextProposal{}, ErrInvalid
	}
	if len(input.SourceObjectIDs) > 100 {
		return ContextProposal{}, ErrInvalid
	}
	seen := map[string]bool{}
	validatedSourceIDs := make([]string, 0, len(input.SourceObjectIDs))
	for _, sourceID := range input.SourceObjectIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			return ContextProposal{}, ErrInvalid
		}
		if !seen[sourceID] {
			seen[sourceID] = true
			validatedSourceIDs = append(validatedSourceIDs, sourceID)
		}
	}
	input.SourceObjectIDs = validatedSourceIDs
	actor := ProposalActor{ID: identity.ActorID(), Kind: identity.Kind}
	if identity.Kind == "agent" {
		if (input.AgentSessionID == "") != (input.AgentRunID == "") {
			return ContextProposal{}, ErrInvalid
		}
		if input.AgentSessionID != "" {
			if service.AgentProvenance == nil ||
				service.AgentProvenance.ValidateProvenance(
					ctx, identity.AgentInstanceID, projectID,
					input.AgentSessionID, input.AgentRunID,
				) != nil {
				return ContextProposal{}, ErrForbidden
			}
			actor.AgentSessionID = input.AgentSessionID
			actor.AgentRunID = input.AgentRunID
		}
	} else {
		if input.AgentSessionID != "" || input.AgentRunID != "" {
			return ContextProposal{}, ErrForbidden
		}
		actor.UserID = identity.User.ID
	}
	return service.Store.CreateProposal(ctx, projectID, actor, input)
}

func (service Service) ReviewProposal(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	proposalID string,
	input ReviewProposalInput,
) (ContextProposal, error) {
	// Tokens and Agents may propose context but can never make it formal.
	if identity.Kind != "session" {
		return ContextProposal{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, identity, projectID, PermissionContextReview); err != nil {
		return ContextProposal{}, err
	}
	input.Decision = strings.TrimSpace(input.Decision)
	input.Note = strings.TrimSpace(input.Note)
	if input.Decision != "accepted" && input.Decision != "rejected" {
		return ContextProposal{}, ErrInvalid
	}
	return service.Store.ReviewProposal(ctx, projectID, proposalID, identity.User.ID, input)
}

func (service Service) Home(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (HomeAggregate, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, PermissionDataRead); err != nil {
		return HomeAggregate{}, err
	}
	empty := func() HomeSection {
		return HomeSection{Available: false, Items: []interface{}{}, Total: 0}
	}
	problem := empty()
	if service.Problem != nil {
		items, err := service.Problem.ProblemItems(
			ctx, identity, projectID,
		)
		if err != nil {
			return HomeAggregate{}, err
		}
		if items == nil {
			items = []interface{}{}
		}
		problem = HomeSection{
			Available: true, Items: items, Total: len(items),
		}
	}
	milestones, todos := empty(), empty()
	var progressTracking interface{} = map[string]interface{}{}
	if service.Progress != nil {
		milestoneItems, todoItems, err := service.Progress.ProgressHomeItems(ctx, identity, projectID)
		if err != nil {
			return HomeAggregate{}, err
		}
		milestones = HomeSection{Available: true, Items: nonNilItems(milestoneItems), Total: len(milestoneItems)}
		todos = HomeSection{Available: true, Items: nonNilItems(todoItems), Total: len(todoItems)}
		progressTracking, err = service.Progress.ProgressHomeTracking(ctx, identity, projectID)
		if err != nil {
			return HomeAggregate{}, err
		}
	}
	models := empty()
	if service.Models != nil {
		items, err := service.Models.ModelHomeItems(ctx, identity, projectID)
		if err != nil {
			return HomeAggregate{}, err
		}
		models = HomeSection{Available: true, Items: nonNilItems(items), Total: len(items)}
	}
	agentSection := empty()
	if service.Agent != nil {
		agentItems, err := service.Agent.AgentHomeItems(ctx, identity, projectID)
		if err != nil {
			return HomeAggregate{}, err
		}
		agentSection = HomeSection{
			Available: true, Items: nonNilItems(agentItems), Total: len(agentItems),
		}
	}
	return HomeAggregate{
		Agent: agentSection, Article: empty(), Experiments: empty(),
		GeneratedAt: service.Clock.Now().UTC(), Milestones: milestones,
		Models: models, Problem: problem, ProgressTracking: progressTracking,
		ProjectID: projectID, Todos: todos,
	}, nil
}

func nonNilItems(items []interface{}) []interface{} {
	if items == nil {
		return []interface{}{}
	}
	return items
}

var (
	ErrAdapterNotFound = errors.New("data object adapter not found")
	ErrConflict        = errors.New("data hub conflict")
	ErrForbidden       = project.ErrForbidden
	ErrHumanRequired   = errors.New("human session required")
	ErrInvalid         = errors.New("invalid data hub input")
	ErrNotFound        = errors.New("data hub record not found")
)
