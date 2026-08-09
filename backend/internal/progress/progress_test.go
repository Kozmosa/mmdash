package progress

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgconn"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type progressAccessTestStub struct {
	authorized []project.Permission
	err        error
}

func (stub *progressAccessTestStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return auth.Identity{}, nil
}

func (stub *progressAccessTestStub) Authorize(_ context.Context, _ auth.Identity, _ string, permission project.Permission) error {
	stub.authorized = append(stub.authorized, permission)
	return stub.err
}

type progressAuditTestStub struct {
	events []audit.Event
}

func (stub *progressAuditTestStub) Record(_ context.Context, event audit.Event) error {
	stub.events = append(stub.events, event)
	return nil
}

type progressStoreTestStub struct {
	settings        Settings
	milestones      []Milestone
	tasks           []Task
	createdTask     Task
	createdReminder Reminder
	reviewed        Proposal
}

func (stub *progressStoreTestStub) ListMilestones(context.Context, string) ([]Milestone, error) {
	return stub.milestones, nil
}
func (stub *progressStoreTestStub) GetMilestone(context.Context, string, string) (Milestone, error) {
	return Milestone{}, ErrNotFound
}
func (stub *progressStoreTestStub) CreateMilestone(_ context.Context, projectID, actorID string, input CreateMilestoneInput) (Milestone, error) {
	return Milestone{ID: "milestone-1", ProjectID: projectID, Title: input.Title, Source: "human", CreatedBy: actorID, UpdatedBy: actorID}, nil
}
func (stub *progressStoreTestStub) UpdateMilestone(context.Context, string, string, string, UpdateMilestoneInput) (Milestone, error) {
	return Milestone{ID: "milestone-1"}, nil
}
func (stub *progressStoreTestStub) ListTasks(context.Context, string) ([]Task, error) {
	return stub.tasks, nil
}
func (stub *progressStoreTestStub) GetTask(context.Context, string, string) (Task, error) {
	return Task{}, ErrNotFound
}
func (stub *progressStoreTestStub) CreateTask(_ context.Context, projectID, actorID string, input CreateTaskInput, source string) (Task, error) {
	stub.createdTask = Task{ID: "task-1", ProjectID: projectID, Title: input.Title, Status: input.Status, Source: source, SourceRunID: input.SourceRunID, CreatedBy: actorID, UpdatedBy: actorID}
	return stub.createdTask, nil
}
func (stub *progressStoreTestStub) UpdateTask(_ context.Context, projectID, taskID, actorID string, input UpdateTaskInput, source string) (Task, error) {
	return Task{ID: taskID, ProjectID: projectID, Source: source, UpdatedBy: actorID}, nil
}
func (stub *progressStoreTestStub) DeleteTask(context.Context, string, string, string) error {
	return nil
}
func (stub *progressStoreTestStub) ListDependencies(context.Context, string) ([]Dependency, error) {
	return []Dependency{}, nil
}
func (stub *progressStoreTestStub) CreateDependency(context.Context, string, string, CreateDependencyInput) (Dependency, error) {
	return Dependency{}, nil
}
func (stub *progressStoreTestStub) DeleteDependency(context.Context, string, string, string) error {
	return nil
}
func (stub *progressStoreTestStub) ListReminders(context.Context, string) ([]Reminder, error) {
	return []Reminder{}, nil
}
func (stub *progressStoreTestStub) CreateReminder(_ context.Context, projectID, actorID string, input CreateReminderInput) (Reminder, error) {
	stub.createdReminder = Reminder{ID: "reminder-1", ProjectID: projectID, TaskID: input.TaskID, MilestoneID: input.MilestoneID, CreatedBy: actorID}
	return stub.createdReminder, nil
}
func (stub *progressStoreTestStub) TriggerReminder(context.Context, string, string, string) (Reminder, error) {
	return Reminder{}, nil
}
func (stub *progressStoreTestStub) ListProposals(context.Context, string) ([]Proposal, error) {
	return []Proposal{}, nil
}
func (stub *progressStoreTestStub) GetProposal(context.Context, string, string) (Proposal, error) {
	return Proposal{}, ErrNotFound
}
func (stub *progressStoreTestStub) CreateProposal(context.Context, string, string, CreateProposalInput) (Proposal, error) {
	return Proposal{}, nil
}
func (stub *progressStoreTestStub) ReviewProposal(_ context.Context, projectID, proposalID, reviewerID string, input ReviewProposalInput) (Proposal, error) {
	stub.reviewed = Proposal{ID: proposalID, ProjectID: projectID, Status: input.Decision, ReviewedBy: reviewerID}
	return stub.reviewed, nil
}
func (stub *progressStoreTestStub) GetSettings(context.Context, string) (Settings, error) {
	return stub.settings, nil
}
func (stub *progressStoreTestStub) UpdateSettings(_ context.Context, projectID, actorID string, value bool) (Settings, error) {
	stub.settings = Settings{ProjectID: projectID, UpdatedBy: actorID, AutoTaskChanges: value}
	return stub.settings, nil
}

func TestCreateMilestoneRequiresHumanSessionAndRecordsAudit(t *testing.T) {
	access := &progressAccessTestStub{}
	auditRecorder := &progressAuditTestStub{}
	service := Service{Access: access, Audit: auditRecorder, Store: &progressStoreTestStub{}}
	agent := auth.Identity{Kind: "agent", User: auth.User{ID: "agent-user"}}
	if _, err := service.CreateMilestone(context.Background(), agent, "project-1", CreateMilestoneInput{Title: "critical"}); !errors.Is(err, ErrHumanRequired) {
		t.Fatalf("agent milestone mutation returned %v", err)
	}
	if len(access.authorized) != 0 {
		t.Fatal("rejected agent milestone should not reach authorization")
	}

	human := auth.Identity{Kind: "session", User: auth.User{ID: "human-user"}}
	item, err := service.CreateMilestone(context.Background(), human, "project-1", CreateMilestoneInput{Title: "critical"})
	if err != nil || item.Source != "human" {
		t.Fatalf("human milestone creation failed: item=%#v err=%v", item, err)
	}
	if !reflect.DeepEqual(access.authorized, []project.Permission{project.PermissionProgressManage}) {
		t.Fatalf("unexpected authorization calls: %#v", access.authorized)
	}
	if len(auditRecorder.events) != 1 || auditRecorder.events[0].Action != "progress.milestone.created" || auditRecorder.events[0].Outcome != "success" {
		t.Fatalf("milestone audit missing: %#v", auditRecorder.events)
	}
}

func TestAgentTaskChangesObeySettingAndRequireRunSource(t *testing.T) {
	access := &progressAccessTestStub{}
	store := &progressStoreTestStub{settings: Settings{AutoTaskChanges: false}}
	service := Service{Access: access, Store: store}
	agent := auth.Identity{Kind: "agent", User: auth.User{ID: "agent-user"}}
	input := CreateTaskInput{Title: "agent task", SourceRunID: "run-1"}
	if _, err := service.CreateTask(context.Background(), agent, "project-1", input); !errors.Is(err, ErrProposalRequired) {
		t.Fatalf("proposal-only setting returned %v", err)
	}

	store.settings.AutoTaskChanges = true
	input.SourceRunID = ""
	if _, err := service.CreateTask(context.Background(), agent, "project-1", input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing run source returned %v", err)
	}
	input.SourceRunID = "run-1"
	item, err := service.CreateTask(context.Background(), agent, "project-1", input)
	if err != nil || item.Source != "agent" || item.SourceRunID != "run-1" {
		t.Fatalf("automatic task change failed: item=%#v err=%v", item, err)
	}
}

func TestProgressHomeItemsContainOpenTasksOnly(t *testing.T) {
	store := &progressStoreTestStub{tasks: []Task{
		{ID: "open", Status: TaskTodo},
		{ID: "done", Status: TaskDone},
		{ID: "cancelled", Status: TaskCancelled},
	}, milestones: []Milestone{{ID: "milestone-1"}}}
	service := Service{Access: &progressAccessTestStub{}, Store: store}
	milestones, todos, err := service.ProgressHomeItems(context.Background(), auth.Identity{}, "project-1")
	if err != nil || len(milestones) != 1 || len(todos) != 1 {
		t.Fatalf("unexpected home items: milestones=%#v todos=%#v err=%v", milestones, todos, err)
	}
	if todos[0].(Task).ID != "open" {
		t.Fatalf("completed tasks leaked into home: %#v", todos)
	}
}

func TestProgressAggregateTodayAndOverdueContainOpenTasksAtUTCBoundaries(t *testing.T) {
	now := time.Date(2026, time.August, 6, 3, 0, 0, 0, time.UTC)
	china := time.FixedZone("CST", 8*60*60)
	due := func(value time.Time) *time.Time { return &value }
	store := &progressStoreTestStub{tasks: []Task{
		{ID: "done", Status: TaskDone, DueAt: due(time.Date(2026, time.August, 5, 23, 0, 0, 0, china))},
		{ID: "cancelled", Status: TaskCancelled, DueAt: due(time.Date(2026, time.August, 6, 12, 0, 0, 0, china))},
		{ID: "todo-overdue", Status: TaskTodo, DueAt: due(now.Add(-time.Hour))},
		{ID: "in-progress-today", Status: TaskInProgress, DueAt: due(time.Date(2026, time.August, 6, 12, 0, 0, 0, china))},
		{ID: "blocked-overdue", Status: TaskBlocked, DueAt: due(now.Add(-time.Hour))},
		{ID: "blocked-now", Status: TaskBlocked, DueAt: due(time.Date(2026, time.August, 6, 11, 0, 0, 0, china))},
		{ID: "todo-day-start", Status: TaskTodo, DueAt: due(time.Date(2026, time.August, 6, 8, 0, 0, 0, china))},
		{ID: "todo-tomorrow", Status: TaskTodo, DueAt: due(time.Date(2026, time.August, 7, 8, 0, 0, 0, china))},
	}}
	service := Service{Access: &progressAccessTestStub{}, Clock: clock.Fixed{Time: now}, Store: store}
	result, err := service.List(context.Background(), auth.Identity{}, "project-1")
	if err != nil {
		t.Fatalf("list progress aggregate: %v", err)
	}

	taskIDs := func(items []Task) []string {
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		return ids
	}
	if got, want := taskIDs(result.Today), []string{"todo-overdue", "in-progress-today", "blocked-overdue", "blocked-now", "todo-day-start"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("today tasks=%v, want %v", got, want)
	}
	if got, want := taskIDs(result.Overdue), []string{"todo-overdue", "blocked-overdue", "todo-day-start"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("overdue tasks=%v, want %v", got, want)
	}
	if got, want := taskIDs(result.Blocked), []string{"blocked-overdue", "blocked-now"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("blocked tasks=%v, want %v", got, want)
	}
	if got, want := taskIDs(result.Board.Done), []string{"done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("done board tasks=%v, want %v", got, want)
	}
	if len(result.Tasks) != 8 || len(result.Gantt) != 8 {
		t.Fatalf("aggregate task views changed unexpectedly: tasks=%d gantt=%d", len(result.Tasks), len(result.Gantt))
	}
}

func TestHumanCanCreateReminderOnlyWithProgressManage(t *testing.T) {
	access := &progressAccessTestStub{err: project.ErrForbidden}
	service := Service{Access: access, Store: &progressStoreTestStub{}}
	human := auth.Identity{Kind: "session", User: auth.User{ID: "human-user"}}
	_, err := service.CreateReminder(context.Background(), human, "project-1", CreateReminderInput{TaskID: "task-1", RemindAt: time.Now()})
	if !errors.Is(err, project.ErrForbidden) {
		t.Fatalf("reminder creation bypassed RBAC: %v", err)
	}
	if !reflect.DeepEqual(access.authorized, []project.Permission{project.PermissionProgressManage}) {
		t.Fatalf("unexpected reminder authorization: %#v", access.authorized)
	}
}

func TestHumanReviewDelegatesToProgressStoreAndAudits(t *testing.T) {
	auditRecorder := &progressAuditTestStub{}
	store := &progressStoreTestStub{}
	service := Service{Access: &progressAccessTestStub{}, Audit: auditRecorder, Store: store}
	human := auth.Identity{Kind: "session", User: auth.User{ID: "human-user"}}
	item, err := service.ReviewProposal(context.Background(), human, "project-1", "proposal-1", ReviewProposalInput{Decision: "accepted"})
	if err != nil || item.Status != "accepted" || store.reviewed.ID != "proposal-1" {
		t.Fatalf("proposal review failed: item=%#v err=%v", item, err)
	}
	if len(auditRecorder.events) != 1 || auditRecorder.events[0].Action != "progress.proposal.reviewed" {
		t.Fatalf("proposal review audit missing: %#v", auditRecorder.events)
	}
}

func TestReferenceErrorsUseStableSafeAPIResponse(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/projects/project-a/progress/tasks", nil)
	response := httptest.NewRecorder()
	writeError(response, request, ErrReferenceInvalid)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("reference error status=%d, want 400", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"code":"PROGRESS_REFERENCE_INVALID"`) || !strings.Contains(body, `"message":"Progress reference is invalid"`) {
		t.Fatalf("unsafe or unstable reference response: %s", body)
	}
	if ErrorCode(ErrReferenceInvalid) != "PROGRESS_REFERENCE_INVALID" {
		t.Fatalf("audit error code=%s", ErrorCode(ErrReferenceInvalid))
	}
}

func TestPostgresConstraintErrorsMapWithoutReferenceDisclosure(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
		want error
	}{
		{name: "foreign key", err: &pgconn.PgError{Code: "23503", ConstraintName: "progress_tasks_project_milestone_fk"}, want: ErrReferenceInvalid},
		{name: "uuid cast", err: &pgconn.PgError{Code: "22P02"}, want: ErrReferenceInvalid},
		{name: "related shape", err: &pgconn.PgError{Code: "23514", ConstraintName: "progress_tasks_related_object_ids_uuid_array_check"}, want: ErrReferenceInvalid},
		{name: "duplicate dependency", err: &pgconn.PgError{Code: "23505"}, want: ErrConflict},
		{name: "ordinary check", err: &pgconn.PgError{Code: "23514", ConstraintName: "progress_tasks_status_check"}, want: ErrInvalid},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapPostgresMutationError(testCase.err); !errors.Is(got, testCase.want) {
				t.Fatalf("mapped error=%v, want %v", got, testCase.want)
			}
		})
	}
}
