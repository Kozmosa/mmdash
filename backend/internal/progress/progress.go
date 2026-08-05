// Package progress owns the authoritative Milestone, Task, Dependency,
// Reminder, and Progress Proposal workflow.
package progress

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/project"
)

const (
	StatusPlanned    = "planned"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusCancelled  = "cancelled"

	TaskTodo       = "todo"
	TaskInProgress = "in_progress"
	TaskBlocked    = "blocked"
	TaskDone       = "done"
	TaskCancelled  = "cancelled"

	ReminderPending    = "pending"
	ReminderProcessing = "processing"
	ReminderTriggered  = "triggered"
	ReminderFailed     = "failed"
	ReminderCancelled  = "cancelled"
)

type Milestone struct {
	ID          string     `json:"milestone_id"`
	ProjectID   string     `json:"project_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Critical    bool       `json:"critical"`
	StartAt     *time.Time `json:"start_at,omitempty"`
	TargetAt    *time.Time `json:"target_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Source      string     `json:"source"`
	SourceRunID string     `json:"source_run_id,omitempty"`
	CreatedBy   string     `json:"created_by"`
	UpdatedBy   string     `json:"updated_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Task struct {
	ID               string     `json:"task_id"`
	ProjectID        string     `json:"project_id"`
	MilestoneID      string     `json:"milestone_id,omitempty"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Status           string     `json:"status"`
	AssigneeID       string     `json:"assignee_id,omitempty"`
	StartAt          *time.Time `json:"start_at,omitempty"`
	DueAt            *time.Time `json:"due_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	Source           string     `json:"source"`
	SourceRunID      string     `json:"source_run_id,omitempty"`
	RelatedObjectIDs []string   `json:"related_object_ids"`
	CreatedBy        string     `json:"created_by"`
	UpdatedBy        string     `json:"updated_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Dependency struct {
	ID              string    `json:"dependency_id"`
	ProjectID       string    `json:"project_id"`
	TaskID          string    `json:"task_id"`
	DependsOnTaskID string    `json:"depends_on_task_id"`
	Kind            string    `json:"kind"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

type Reminder struct {
	ID               string     `json:"reminder_id"`
	ProjectID        string     `json:"project_id"`
	TaskID           string     `json:"task_id,omitempty"`
	MilestoneID      string     `json:"milestone_id,omitempty"`
	RemindAt         time.Time  `json:"remind_at"`
	Status           string     `json:"status"`
	Note             string     `json:"note"`
	Source           string     `json:"source"`
	TriggeredAt      *time.Time `json:"triggered_at,omitempty"`
	CreatedBy        string     `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	AvailableAt      time.Time  `json:"-"`
	Attempts         int        `json:"-"`
	MaxAttempts      int        `json:"-"`
	LockedBy         string     `json:"-"`
	LeaseExpiresAt   *time.Time `json:"-"`
	LastErrorCode    string     `json:"-"`
	LastErrorMessage string     `json:"-"`
}

type Proposal struct {
	ID           string                 `json:"proposal_id"`
	ProjectID    string                 `json:"project_id"`
	ProposalType string                 `json:"proposal_type"`
	TargetID     string                 `json:"target_id,omitempty"`
	Title        string                 `json:"title"`
	Rationale    string                 `json:"rationale"`
	Changes      map[string]interface{} `json:"changes"`
	Source       string                 `json:"source"`
	SourceRunID  string                 `json:"source_run_id,omitempty"`
	ProposedBy   string                 `json:"proposed_by"`
	Status       string                 `json:"status"`
	ReviewedBy   string                 `json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time             `json:"reviewed_at,omitempty"`
	ReviewNote   string                 `json:"review_note"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type Settings struct {
	ProjectID       string    `json:"project_id"`
	AutoTaskChanges bool      `json:"auto_task_changes"`
	UpdatedBy       string    `json:"updated_by"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Progress struct {
	ProjectID    string       `json:"project_id"`
	GeneratedAt  time.Time    `json:"generated_at"`
	Milestones   []Milestone  `json:"milestones"`
	Tasks        []Task       `json:"tasks"`
	Dependencies []Dependency `json:"dependencies"`
	Reminders    []Reminder   `json:"reminders"`
	Proposals    []Proposal   `json:"proposals"`
	Today        []Task       `json:"today"`
	Overdue      []Task       `json:"overdue"`
	Blocked      []Task       `json:"blocked"`
	Board        Board        `json:"board"`
	Gantt        []GanttItem  `json:"gantt"`
	Settings     Settings     `json:"settings"`
}

type Board struct {
	Todo       []Task `json:"todo"`
	InProgress []Task `json:"in_progress"`
	Blocked    []Task `json:"blocked"`
	Done       []Task `json:"done"`
}

type GanttItem struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Title    string     `json:"title"`
	StartAt  *time.Time `json:"start_at,omitempty"`
	TargetAt *time.Time `json:"target_at,omitempty"`
	Status   string     `json:"status"`
}

type CreateMilestoneInput struct {
	Title       string
	Description string
	Critical    bool
	StartAt     *time.Time
	TargetAt    *time.Time
}

type UpdateMilestoneInput struct {
	Title       *string
	Description *string
	Status      *string
	Critical    *bool
	StartAt     **time.Time
	TargetAt    **time.Time
}

type CreateTaskInput struct {
	MilestoneID      string
	Title            string
	Description      string
	Status           string
	AssigneeID       string
	StartAt          *time.Time
	DueAt            *time.Time
	RelatedObjectIDs []string
	SourceRunID      string
}

type UpdateTaskInput struct {
	MilestoneID      *string
	Title            *string
	Description      *string
	Status           *string
	AssigneeID       *string
	StartAt          **time.Time
	DueAt            **time.Time
	RelatedObjectIDs *[]string
	SourceRunID      *string
}

type CreateDependencyInput struct {
	TaskID          string
	DependsOnTaskID string
	Kind            string
}

type CreateReminderInput struct {
	TaskID      string
	MilestoneID string
	RemindAt    time.Time
	Note        string
}

type CreateProposalInput struct {
	ProposalType string
	TargetID     string
	Title        string
	Rationale    string
	Changes      map[string]interface{}
	SourceRunID  string
}

type ReviewProposalInput struct {
	Decision string
	Note     string
}

type Access interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

type AuditRecorder interface {
	Record(context.Context, audit.Event) error
}

type Store interface {
	ListMilestones(context.Context, string) ([]Milestone, error)
	GetMilestone(context.Context, string, string) (Milestone, error)
	CreateMilestone(context.Context, string, string, CreateMilestoneInput) (Milestone, error)
	UpdateMilestone(context.Context, string, string, string, UpdateMilestoneInput) (Milestone, error)
	ListTasks(context.Context, string) ([]Task, error)
	GetTask(context.Context, string, string) (Task, error)
	CreateTask(context.Context, string, string, CreateTaskInput, string) (Task, error)
	UpdateTask(context.Context, string, string, string, UpdateTaskInput, string) (Task, error)
	DeleteTask(context.Context, string, string, string) error
	ListDependencies(context.Context, string) ([]Dependency, error)
	CreateDependency(context.Context, string, string, CreateDependencyInput) (Dependency, error)
	DeleteDependency(context.Context, string, string, string) error
	ListReminders(context.Context, string) ([]Reminder, error)
	CreateReminder(context.Context, string, string, CreateReminderInput) (Reminder, error)
	TriggerReminder(context.Context, string, string, string) (Reminder, error)
	ListProposals(context.Context, string) ([]Proposal, error)
	GetProposal(context.Context, string, string) (Proposal, error)
	CreateProposal(context.Context, string, string, CreateProposalInput) (Proposal, error)
	ReviewProposal(context.Context, string, string, string, ReviewProposalInput) (Proposal, error)
	GetSettings(context.Context, string) (Settings, error)
	UpdateSettings(context.Context, string, string, bool) (Settings, error)
}

type Service struct {
	Access    Access
	Audit     AuditRecorder
	Clock     interface{ Now() time.Time }
	Generator identity.Generator
	Store     Store
}

func (service Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service Service) List(ctx context.Context, identity auth.Identity, projectID string) (Progress, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return Progress{}, err
	}
	milestones, err := service.Store.ListMilestones(ctx, projectID)
	if err != nil {
		return Progress{}, err
	}
	tasks, err := service.Store.ListTasks(ctx, projectID)
	if err != nil {
		return Progress{}, err
	}
	dependencies, err := service.Store.ListDependencies(ctx, projectID)
	if err != nil {
		return Progress{}, err
	}
	reminders, err := service.Store.ListReminders(ctx, projectID)
	if err != nil {
		return Progress{}, err
	}
	proposals, err := service.Store.ListProposals(ctx, projectID)
	if err != nil {
		return Progress{}, err
	}
	settings, err := service.Store.GetSettings(ctx, projectID)
	if err != nil {
		return Progress{}, err
	}
	now := service.now()
	result := Progress{ProjectID: projectID, GeneratedAt: now, Milestones: nonNilMilestones(milestones), Tasks: nonNilTasks(tasks), Dependencies: dependencies, Reminders: reminders, Proposals: proposals, Settings: settings}
	today := now.Truncate(24 * time.Hour)
	tomorrow := today.Add(24 * time.Hour)
	for _, task := range result.Tasks {
		if task.Status == TaskBlocked {
			result.Blocked = append(result.Blocked, task)
		}
		if task.Status != TaskDone && task.DueAt != nil && task.DueAt.Before(now) {
			result.Overdue = append(result.Overdue, task)
		}
		if task.DueAt != nil && !task.DueAt.Before(today) && task.DueAt.Before(tomorrow) {
			result.Today = append(result.Today, task)
		}
		switch task.Status {
		case TaskTodo:
			result.Board.Todo = append(result.Board.Todo, task)
		case TaskInProgress:
			result.Board.InProgress = append(result.Board.InProgress, task)
		case TaskBlocked:
			result.Board.Blocked = append(result.Board.Blocked, task)
		case TaskDone:
			result.Board.Done = append(result.Board.Done, task)
		}
	}
	for _, milestone := range result.Milestones {
		result.Gantt = append(result.Gantt, GanttItem{ID: milestone.ID, Kind: "milestone", Title: milestone.Title, StartAt: milestone.StartAt, TargetAt: milestone.TargetAt, Status: milestone.Status})
	}
	for _, task := range result.Tasks {
		result.Gantt = append(result.Gantt, GanttItem{ID: task.ID, Kind: "task", Title: task.Title, StartAt: task.StartAt, TargetAt: task.DueAt, Status: task.Status})
	}
	return result, nil
}

func (service Service) ProgressHomeItems(ctx context.Context, identity auth.Identity, projectID string) ([]interface{}, []interface{}, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return nil, nil, err
	}
	milestones, err := service.Store.ListMilestones(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	tasks, err := service.Store.ListTasks(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	milestoneItems := make([]interface{}, 0, len(milestones))
	for _, item := range milestones {
		milestoneItems = append(milestoneItems, item)
	}
	taskItems := make([]interface{}, 0, len(tasks))
	for _, item := range tasks {
		if item.Status == TaskDone || item.Status == TaskCancelled {
			continue
		}
		taskItems = append(taskItems, item)
	}
	return milestoneItems, taskItems, nil
}

func (service Service) ListMilestones(ctx context.Context, identity auth.Identity, projectID string) ([]Milestone, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return nil, err
	}
	return service.Store.ListMilestones(ctx, projectID)
}

func (service Service) ListTasks(ctx context.Context, identity auth.Identity, projectID string) ([]Task, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return nil, err
	}
	return service.Store.ListTasks(ctx, projectID)
}

func (service Service) ListDependencies(ctx context.Context, identity auth.Identity, projectID string) ([]Dependency, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return nil, err
	}
	return service.Store.ListDependencies(ctx, projectID)
}

func (service Service) ListReminders(ctx context.Context, identity auth.Identity, projectID string) ([]Reminder, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return nil, err
	}
	return service.Store.ListReminders(ctx, projectID)
}

func (service Service) ListProposals(ctx context.Context, identity auth.Identity, projectID string) ([]Proposal, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return nil, err
	}
	return service.Store.ListProposals(ctx, projectID)
}

func (service Service) GetSettings(ctx context.Context, identity auth.Identity, projectID string) (Settings, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return Settings{}, err
	}
	return service.Store.GetSettings(ctx, projectID)
}

func (service Service) CreateMilestone(ctx context.Context, identity auth.Identity, projectID string, input CreateMilestoneInput) (Milestone, error) {
	if identity.Kind != "session" {
		return Milestone{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return Milestone{}, err
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return Milestone{}, ErrInvalid
	}
	item, err := service.Store.CreateMilestone(ctx, projectID, identity.User.ID, input)
	service.record(ctx, identity, "progress.milestone.created", "milestone", item.ID, projectID, map[string]interface{}{"source": "human"}, err)
	return item, err
}

func (service Service) UpdateMilestone(ctx context.Context, identity auth.Identity, projectID, milestoneID string, input UpdateMilestoneInput) (Milestone, error) {
	if identity.Kind != "session" {
		return Milestone{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return Milestone{}, err
	}
	item, err := service.Store.UpdateMilestone(ctx, projectID, milestoneID, identity.User.ID, input)
	service.record(ctx, identity, "progress.milestone.updated", "milestone", milestoneID, projectID, map[string]interface{}{"source": "human"}, err)
	return item, err
}

func (service Service) CreateTask(ctx context.Context, identity auth.Identity, projectID string, input CreateTaskInput) (Task, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return Task{}, err
	}
	source := "human"
	if identity.Kind != "session" {
		settings, err := service.Store.GetSettings(ctx, projectID)
		if err != nil {
			return Task{}, err
		}
		if !settings.AutoTaskChanges {
			return Task{}, ErrProposalRequired
		}
		if strings.TrimSpace(input.SourceRunID) == "" {
			return Task{}, ErrInvalid
		}
		source = "agent"
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return Task{}, ErrInvalid
	}
	if input.Status == "" {
		input.Status = TaskTodo
	}
	item, err := service.Store.CreateTask(ctx, projectID, identity.User.ID, input, source)
	service.record(ctx, identity, "progress.task.created", "task", item.ID, projectID, map[string]interface{}{"source": source, "source_run_id": input.SourceRunID}, err)
	return item, err
}

func (service Service) UpdateTask(ctx context.Context, identity auth.Identity, projectID, taskID string, input UpdateTaskInput) (Task, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return Task{}, err
	}
	source := "human"
	if identity.Kind != "session" {
		settings, err := service.Store.GetSettings(ctx, projectID)
		if err != nil {
			return Task{}, err
		}
		if !settings.AutoTaskChanges {
			return Task{}, ErrProposalRequired
		}
		if input.SourceRunID == nil || strings.TrimSpace(*input.SourceRunID) == "" {
			return Task{}, ErrInvalid
		}
		source = "agent"
	}
	item, err := service.Store.UpdateTask(ctx, projectID, taskID, identity.User.ID, input, source)
	metadata := map[string]interface{}{"source": source}
	if input.SourceRunID != nil {
		metadata["source_run_id"] = *input.SourceRunID
	}
	service.record(ctx, identity, "progress.task.updated", "task", taskID, projectID, metadata, err)
	return item, err
}

func (service Service) DeleteTask(ctx context.Context, identity auth.Identity, projectID, taskID string) error {
	if identity.Kind != "session" {
		return ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return err
	}
	err := service.Store.DeleteTask(ctx, projectID, taskID, identity.User.ID)
	service.record(ctx, identity, "progress.task.deleted", "task", taskID, projectID, map[string]interface{}{"source": "human"}, err)
	return err
}

func (service Service) CreateDependency(ctx context.Context, identity auth.Identity, projectID string, input CreateDependencyInput) (Dependency, error) {
	if identity.Kind != "session" {
		return Dependency{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return Dependency{}, err
	}
	if input.Kind == "" {
		input.Kind = "blocks"
	}
	item, err := service.Store.CreateDependency(ctx, projectID, identity.User.ID, input)
	service.record(ctx, identity, "progress.dependency.created", "dependency", item.ID, projectID, map[string]interface{}{"source": "human"}, err)
	return item, err
}

func (service Service) DeleteDependency(ctx context.Context, identity auth.Identity, projectID, dependencyID string) error {
	if identity.Kind != "session" {
		return ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return err
	}
	err := service.Store.DeleteDependency(ctx, projectID, dependencyID, identity.User.ID)
	service.record(ctx, identity, "progress.dependency.deleted", "dependency", dependencyID, projectID, map[string]interface{}{"source": "human"}, err)
	return err
}

func (service Service) CreateReminder(ctx context.Context, identity auth.Identity, projectID string, input CreateReminderInput) (Reminder, error) {
	if identity.Kind != "session" {
		return Reminder{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return Reminder{}, err
	}
	if (input.TaskID == "" && input.MilestoneID == "") || (input.TaskID != "" && input.MilestoneID != "") || input.RemindAt.IsZero() {
		return Reminder{}, ErrInvalid
	}
	item, err := service.Store.CreateReminder(ctx, projectID, identity.User.ID, input)
	service.record(ctx, identity, "progress.reminder.created", "reminder", item.ID, projectID, map[string]interface{}{"source": "human"}, err)
	return item, err
}

func (service Service) TriggerReminder(ctx context.Context, identity auth.Identity, projectID, reminderID string) (Reminder, error) {
	if identity.Kind != "session" {
		return Reminder{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return Reminder{}, err
	}
	item, err := service.Store.TriggerReminder(ctx, projectID, reminderID, identity.User.ID)
	service.record(ctx, identity, "progress.reminder.triggered", "reminder", reminderID, projectID, map[string]interface{}{"source": "human", "notification_boundary": "NotificationAdapter"}, err)
	return item, err
}

func (service Service) CreateProposal(ctx context.Context, identity auth.Identity, projectID string, input CreateProposalInput) (Proposal, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressPropose); err != nil {
		return Proposal{}, err
	}
	if input.ProposalType == "" || input.Title == "" || input.Changes == nil {
		return Proposal{}, ErrInvalid
	}
	if identity.Kind == "session" {
		return Proposal{}, ErrInvalid
	}
	if strings.TrimSpace(input.SourceRunID) == "" {
		return Proposal{}, ErrInvalid
	}
	item, err := service.Store.CreateProposal(ctx, projectID, identity.User.ID, input)
	service.record(ctx, identity, "progress.proposal.created", "progress_proposal", item.ID, projectID, map[string]interface{}{"source": item.Source, "source_run_id": item.SourceRunID}, err)
	return item, err
}

func (service Service) ReviewProposal(ctx context.Context, identity auth.Identity, projectID, proposalID string, input ReviewProposalInput) (Proposal, error) {
	if identity.Kind != "session" {
		return Proposal{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return Proposal{}, err
	}
	input.Decision = strings.TrimSpace(input.Decision)
	if input.Decision != "accepted" && input.Decision != "rejected" {
		return Proposal{}, ErrInvalid
	}
	item, err := service.Store.ReviewProposal(ctx, projectID, proposalID, identity.User.ID, input)
	service.record(ctx, identity, "progress.proposal.reviewed", "progress_proposal", proposalID, projectID, map[string]interface{}{"decision": input.Decision}, err)
	return item, err
}

func (service Service) UpdateSettings(ctx context.Context, identity auth.Identity, projectID string, autoTaskChanges bool) (Settings, error) {
	if identity.Kind != "session" {
		return Settings{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressManage); err != nil {
		return Settings{}, err
	}
	item, err := service.Store.UpdateSettings(ctx, projectID, identity.User.ID, autoTaskChanges)
	service.record(ctx, identity, "progress.settings.updated", "progress_settings", projectID, projectID, map[string]interface{}{"auto_task_changes": autoTaskChanges}, err)
	return item, err
}

func (service Service) ReadMilestone(ctx context.Context, identity auth.Identity, projectID, id string) (Milestone, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return Milestone{}, err
	}
	return service.Store.GetMilestone(ctx, projectID, id)
}

func (service Service) ReadTask(ctx context.Context, identity auth.Identity, projectID, id string) (Task, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return Task{}, err
	}
	return service.Store.GetTask(ctx, projectID, id)
}

func (service Service) ReadProposal(ctx context.Context, identity auth.Identity, projectID, id string) (Proposal, error) {
	if err := service.Access.Authorize(ctx, identity, projectID, project.PermissionProgressRead); err != nil {
		return Proposal{}, err
	}
	return service.Store.GetProposal(ctx, projectID, id)
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock.Now().UTC()
}

func (service Service) record(ctx context.Context, identity auth.Identity, action, resourceType, resourceID, projectID string, metadata map[string]interface{}, err error) {
	if service.Audit == nil {
		return
	}
	outcome, errorCode := "success", ""
	if err != nil {
		outcome, errorCode = "error", ErrorCode(err)
	}
	_ = service.Audit.Record(ctx, audit.Event{Action: action, ActorID: identity.User.ID, ActorKind: identity.Kind, Category: "progress", ErrorCode: errorCode, Metadata: metadata, Outcome: outcome, ProjectID: projectID, RequestID: requestctx.RequestID(ctx), ResourceID: resourceID, ResourceType: resourceType, Source: "core"})
}

func nonNilMilestones(items []Milestone) []Milestone {
	if items == nil {
		return []Milestone{}
	}
	return items
}
func nonNilTasks(items []Task) []Task {
	if items == nil {
		return []Task{}
	}
	return items
}

var (
	ErrInvalid           = errors.New("invalid progress input")
	ErrNotFound          = errors.New("progress record not found")
	ErrConflict          = errors.New("progress conflict")
	ErrHumanRequired     = errors.New("human confirmation required")
	ErrProposalRequired  = errors.New("progress proposal required")
	ErrForbidden         = errors.New("progress permission denied")
	ErrReminderLeaseLost = errors.New("progress reminder processing lease lost")
)

func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrHumanRequired):
		return "HUMAN_REVIEW_REQUIRED"
	case errors.Is(err, ErrProposalRequired):
		return "PROGRESS_PROPOSAL_REQUIRED"
	case errors.Is(err, ErrConflict):
		return "PROGRESS_CONFLICT"
	case errors.Is(err, ErrNotFound):
		return "PROGRESS_NOT_FOUND"
	case errors.Is(err, ErrForbidden):
		return "FORBIDDEN"
	default:
		return "INVALID_PROGRESS_REQUEST"
	}
}
