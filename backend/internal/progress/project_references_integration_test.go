package progress

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgconn"
	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/datahub"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresTaskProjectReferenceBoundaries(t *testing.T) {
	fixture := newProgressReferenceFixture(t)
	milestoneA := fixture.createMilestone(t, fixture.projectA, fixture.ownerA, "Milestone A")
	milestoneB := fixture.createMilestone(t, fixture.projectB, fixture.ownerB, "Milestone B")
	activeObject := fixture.createObject(t, fixture.projectA, "active")
	deletedObject := fixture.createObject(t, fixture.projectA, "deleted")
	hiddenObject := fixture.createObject(t, fixture.projectA, "hidden")
	crossObject := fixture.createObject(t, fixture.projectB, "active")

	task, err := fixture.store.CreateTask(fixture.ctx, fixture.projectA, fixture.ownerA, CreateTaskInput{
		MilestoneID: milestoneA.ID,
		Title:       "Valid task",
		AssigneeID:  fixture.memberA,
		RelatedObjectIDs: []string{
			activeObject, activeObject, deletedObject,
		},
	}, "human")
	if err != nil {
		t.Fatalf("create same-Project task: %v", err)
	}
	if len(task.RelatedObjectIDs) != 3 || task.RelatedObjectIDs[0] != task.RelatedObjectIDs[1] {
		t.Fatalf("related object duplicates should follow the contract: %#v", task.RelatedObjectIDs)
	}

	createCases := []struct {
		name  string
		input CreateTaskInput
	}{
		{name: "cross milestone", input: CreateTaskInput{Title: "bad", MilestoneID: milestoneB.ID}},
		{name: "cross assignee", input: CreateTaskInput{Title: "bad", AssigneeID: fixture.ownerB}},
		{name: "nonmember assignee", input: CreateTaskInput{Title: "bad", AssigneeID: fixture.nonmember}},
		{name: "cross object", input: CreateTaskInput{Title: "bad", RelatedObjectIDs: []string{crossObject}}},
		{name: "hidden object", input: CreateTaskInput{Title: "bad", RelatedObjectIDs: []string{hiddenObject}}},
		{name: "missing object", input: CreateTaskInput{Title: "bad", RelatedObjectIDs: []string{fixture.generator.MustNew()}}},
		{name: "malformed object", input: CreateTaskInput{Title: "bad", RelatedObjectIDs: []string{"not-a-uuid"}}},
		{name: "empty object", input: CreateTaskInput{Title: "bad", RelatedObjectIDs: []string{""}}},
	}
	for _, testCase := range createCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := fixture.store.CreateTask(fixture.ctx, fixture.projectA, fixture.ownerA, testCase.input, "human"); !errors.Is(err, ErrReferenceInvalid) {
				t.Fatalf("create error=%v, want safe reference error", err)
			}
		})
	}

	crossMilestone := milestoneB.ID
	crossAssignee := fixture.ownerB
	crossObjects := []string{crossObject}
	updateCases := []struct {
		name  string
		input UpdateTaskInput
	}{
		{name: "cross milestone", input: UpdateTaskInput{MilestoneID: &crossMilestone}},
		{name: "cross assignee", input: UpdateTaskInput{AssigneeID: &crossAssignee}},
		{name: "cross objects", input: UpdateTaskInput{RelatedObjectIDs: &crossObjects}},
	}
	for _, testCase := range updateCases {
		t.Run("update "+testCase.name, func(t *testing.T) {
			if _, err := fixture.store.UpdateTask(fixture.ctx, fixture.projectA, task.ID, fixture.ownerA, testCase.input, "human"); !errors.Is(err, ErrReferenceInvalid) {
				t.Fatalf("update error=%v, want safe reference error", err)
			}
		})
	}

	if _, err := fixture.db.ExecContext(fixture.ctx, `UPDATE data_objects SET status='hidden' WHERE object_id=$1`, activeObject); err != nil {
		t.Fatalf("hide referenced object: %v", err)
	}
	updatedTitle := "Historical references remain stable"
	if _, err := fixture.store.UpdateTask(fixture.ctx, fixture.projectA, task.ID, fixture.ownerA, UpdateTaskInput{Title: &updatedTitle}, "human"); err != nil {
		t.Fatalf("unrelated update after object is hidden: %v", err)
	}
	if _, err := fixture.store.UpdateTask(fixture.ctx, fixture.projectA, task.ID, fixture.ownerA, UpdateTaskInput{RelatedObjectIDs: &task.RelatedObjectIDs}, "human"); !errors.Is(err, ErrReferenceInvalid) {
		t.Fatalf("explicitly rewriting hidden references: %v", err)
	}

	assigned := fixture.createTask(t, fixture.projectA, fixture.ownerA, CreateTaskInput{Title: "Assigned", AssigneeID: fixture.memberA})
	if _, err := fixture.db.ExecContext(fixture.ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, fixture.projectA, fixture.memberA); err != nil {
		t.Fatalf("remove assignee membership: %v", err)
	}
	var projectID string
	var assigneeID *string
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT project_id::text,assignee_id::text FROM progress_tasks WHERE task_id=$1`, assigned.ID).Scan(&projectID, &assigneeID); err != nil {
		t.Fatalf("read task after member removal: %v", err)
	}
	if projectID != fixture.projectA || assigneeID != nil {
		t.Fatalf("member removal changed wrong columns: project=%s assignee=%v", projectID, assigneeID)
	}
}

func TestPostgresProgressAggregateExcludesTerminalTasksFromTodayAndOverdue(t *testing.T) {
	fixture := newProgressReferenceFixture(t)
	create := func(title, status string, dueAt time.Time) {
		t.Helper()
		fixture.createTask(t, fixture.projectA, fixture.ownerA, CreateTaskInput{Title: title, Status: status, DueAt: &dueAt})
	}
	create("done", TaskDone, fixture.now.Add(-7*time.Hour))
	create("blocked-overdue", TaskBlocked, fixture.now.Add(-4*time.Hour))
	create("day-start", TaskTodo, time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC))
	create("todo-overdue", TaskTodo, fixture.now.Add(-2*time.Hour))
	create("cancelled", TaskCancelled, fixture.now.Add(-30*time.Minute))
	create("blocked-now", TaskBlocked, fixture.now)
	create("in-progress", TaskInProgress, fixture.now.Add(time.Hour))
	create("tomorrow", TaskTodo, fixture.now.Add(21*time.Hour))

	service := Service{Access: &progressAccessTestStub{}, Clock: clock.Fixed{Time: fixture.now}, Store: &fixture.store}
	result, err := service.List(fixture.ctx, auth.Identity{}, fixture.projectA)
	if err != nil {
		t.Fatalf("list progress aggregate: %v", err)
	}
	titles := func(items []Task) []string {
		values := make([]string, 0, len(items))
		for _, item := range items {
			values = append(values, item.Title)
		}
		return values
	}
	if got, want := titles(result.Tasks), []string{"done", "blocked-overdue", "day-start", "todo-overdue", "cancelled", "blocked-now", "in-progress", "tomorrow"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("task ordering changed: got=%v want=%v", got, want)
	}
	if got, want := titles(result.Today), []string{"day-start", "todo-overdue", "blocked-now", "in-progress"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("today tasks=%v, want %v", got, want)
	}
	if got, want := titles(result.Overdue), []string{"blocked-overdue", "day-start", "todo-overdue"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("overdue tasks=%v, want %v", got, want)
	}
	if got, want := titles(result.Blocked), []string{"blocked-overdue", "blocked-now"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("blocked tasks=%v, want %v", got, want)
	}
	if got, want := titles(result.Board.Done), []string{"done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("done board tasks=%v, want %v", got, want)
	}
}

func TestPostgresDependencyReminderProjectBoundariesAndDeletion(t *testing.T) {
	fixture := newProgressReferenceFixture(t)
	milestoneA := fixture.createMilestone(t, fixture.projectA, fixture.ownerA, "Milestone A")
	milestoneB := fixture.createMilestone(t, fixture.projectB, fixture.ownerB, "Milestone B")
	taskA := fixture.createTask(t, fixture.projectA, fixture.ownerA, CreateTaskInput{Title: "Task A"})
	prerequisiteA := fixture.createTask(t, fixture.projectA, fixture.ownerA, CreateTaskInput{Title: "Prerequisite A"})
	taskB := fixture.createTask(t, fixture.projectB, fixture.ownerB, CreateTaskInput{Title: "Task B"})

	dependency, err := fixture.store.CreateDependency(fixture.ctx, fixture.projectA, fixture.ownerA, CreateDependencyInput{TaskID: taskA.ID, DependsOnTaskID: prerequisiteA.ID, Kind: "blocks"})
	if err != nil {
		t.Fatalf("create same-Project dependency: %v", err)
	}
	for name, input := range map[string]CreateDependencyInput{
		"cross task":         {TaskID: taskB.ID, DependsOnTaskID: prerequisiteA.ID, Kind: "blocks"},
		"cross prerequisite": {TaskID: taskA.ID, DependsOnTaskID: taskB.ID, Kind: "blocks"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fixture.store.CreateDependency(fixture.ctx, fixture.projectA, fixture.ownerA, input); !errors.Is(err, ErrReferenceInvalid) {
				t.Fatalf("dependency error=%v, want safe reference error", err)
			}
		})
	}

	remindAt := fixture.now.Add(time.Hour)
	taskReminder, err := fixture.store.CreateReminder(fixture.ctx, fixture.projectA, fixture.ownerA, CreateReminderInput{TaskID: taskA.ID, RemindAt: remindAt})
	if err != nil {
		t.Fatalf("create same-Project task reminder: %v", err)
	}
	milestoneReminder, err := fixture.store.CreateReminder(fixture.ctx, fixture.projectA, fixture.ownerA, CreateReminderInput{MilestoneID: milestoneA.ID, RemindAt: remindAt})
	if err != nil {
		t.Fatalf("create same-Project milestone reminder: %v", err)
	}
	for name, input := range map[string]CreateReminderInput{
		"cross task":      {TaskID: taskB.ID, RemindAt: remindAt},
		"cross milestone": {MilestoneID: milestoneB.ID, RemindAt: remindAt},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fixture.store.CreateReminder(fixture.ctx, fixture.projectA, fixture.ownerA, input); !errors.Is(err, ErrReferenceInvalid) {
				t.Fatalf("reminder error=%v, want safe reference error", err)
			}
		})
	}

	if err := fixture.store.DeleteTask(fixture.ctx, fixture.projectA, taskA.ID, fixture.ownerA); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	fixture.assertMissing(t, "progress_dependencies", "dependency_id", dependency.ID)
	fixture.assertMissing(t, "progress_reminders", "reminder_id", taskReminder.ID)
	if _, err := fixture.db.ExecContext(fixture.ctx, `DELETE FROM progress_milestones WHERE project_id=$1 AND milestone_id=$2`, fixture.projectA, milestoneA.ID); err != nil {
		t.Fatalf("delete milestone: %v", err)
	}
	fixture.assertMissing(t, "progress_reminders", "reminder_id", milestoneReminder.ID)
}

func TestPostgresCompositeForeignKeysRejectDirectCrossProjectWrites(t *testing.T) {
	fixture := newProgressReferenceFixture(t)
	milestoneA := fixture.createMilestone(t, fixture.projectA, fixture.ownerA, "Milestone A")
	milestoneB := fixture.createMilestone(t, fixture.projectB, fixture.ownerB, "Milestone B")
	taskA := fixture.createTask(t, fixture.projectA, fixture.ownerA, CreateTaskInput{Title: "Task A", MilestoneID: milestoneA.ID})
	prerequisiteA := fixture.createTask(t, fixture.projectA, fixture.ownerA, CreateTaskInput{Title: "Prerequisite A"})
	taskB := fixture.createTask(t, fixture.projectB, fixture.ownerB, CreateTaskInput{Title: "Task B"})

	directTaskID := fixture.generator.MustNew()
	_, err := fixture.db.ExecContext(fixture.ctx, `
		INSERT INTO progress_tasks(task_id,project_id,milestone_id,title,status,source,created_by,updated_by,created_at,updated_at)
		VALUES($1,$2,$3,'Cross milestone SQL','todo','human',$4,$4,$5,$5)
	`, directTaskID, fixture.projectA, milestoneB.ID, fixture.ownerA, fixture.now)
	assertPostgresCode(t, err, "23503")

	directAssigneeTaskID := fixture.generator.MustNew()
	_, err = fixture.db.ExecContext(fixture.ctx, `
		INSERT INTO progress_tasks(task_id,project_id,title,status,assignee_id,source,created_by,updated_by,created_at,updated_at)
		VALUES($1,$2,'Cross assignee SQL','todo',$3,'human',$4,$4,$5,$5)
	`, directAssigneeTaskID, fixture.projectA, fixture.ownerB, fixture.ownerA, fixture.now)
	assertPostgresCode(t, err, "23503")
	for name, relatedJSON := range map[string]string{
		"non-string": `[42]`,
		"malformed":  `["not-a-uuid"]`,
	} {
		t.Run("related object "+name, func(t *testing.T) {
			_, err := fixture.db.ExecContext(fixture.ctx, `
				INSERT INTO progress_tasks(task_id,project_id,title,status,source,related_object_ids,created_by,updated_by,created_at,updated_at)
				VALUES($1,$2,'Invalid related SQL','todo','human',$3::jsonb,$4,$4,$5,$5)
			`, fixture.generator.MustNew(), fixture.projectA, relatedJSON, fixture.ownerA, fixture.now)
			assertPostgresCode(t, err, "23514")
		})
	}

	_, err = fixture.db.ExecContext(fixture.ctx, `
		INSERT INTO progress_dependencies(dependency_id,project_id,task_id,depends_on_task_id,kind,created_by,created_at)
		VALUES($1,$2,$3,$4,'blocks',$5,$6)
	`, fixture.generator.MustNew(), fixture.projectA, taskB.ID, prerequisiteA.ID, fixture.ownerA, fixture.now)
	assertPostgresCode(t, err, "23503")
	_, err = fixture.db.ExecContext(fixture.ctx, `
		INSERT INTO progress_dependencies(dependency_id,project_id,task_id,depends_on_task_id,kind,created_by,created_at)
		VALUES($1,$2,$3,$4,'blocks',$5,$6)
	`, fixture.generator.MustNew(), fixture.projectA, taskA.ID, taskB.ID, fixture.ownerA, fixture.now)
	assertPostgresCode(t, err, "23503")

	for name, values := range map[string][2]string{
		"task":      {taskB.ID, ""},
		"milestone": {"", milestoneB.ID},
	} {
		t.Run("reminder "+name, func(t *testing.T) {
			_, err := fixture.db.ExecContext(fixture.ctx, `
				INSERT INTO progress_reminders(reminder_id,project_id,task_id,milestone_id,remind_at,available_at,status,source,created_by,created_at,updated_at)
				VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$5,'pending','human',$6,$7,$7)
			`, fixture.generator.MustNew(), fixture.projectA, values[0], values[1], fixture.now.Add(time.Hour), fixture.ownerA, fixture.now)
			assertPostgresCode(t, err, "23503")
		})
	}
}

func TestPostgresProposalReferencesValidateAtCreationAndAcceptance(t *testing.T) {
	fixture := newProgressReferenceFixture(t)
	milestoneA := fixture.createMilestone(t, fixture.projectA, fixture.ownerA, "Milestone A")
	milestoneB := fixture.createMilestone(t, fixture.projectB, fixture.ownerB, "Milestone B")
	taskA := fixture.createTask(t, fixture.projectA, fixture.ownerA, CreateTaskInput{Title: "Task A"})
	taskB := fixture.createTask(t, fixture.projectB, fixture.ownerB, CreateTaskInput{Title: "Task B"})
	activeObject := fixture.createObject(t, fixture.projectA, "active")
	hiddenObject := fixture.createObject(t, fixture.projectA, "hidden")
	crossObject := fixture.createObject(t, fixture.projectB, "active")

	invalidProposals := []struct {
		name  string
		input CreateProposalInput
	}{
		{name: "create with target", input: proposalInput("task.create", taskA.ID, map[string]interface{}{"title": "bad"})},
		{name: "update without target", input: proposalInput("task.update", "", map[string]interface{}{"title": "bad"})},
		{name: "cross milestone target", input: proposalInput("milestone.update", milestoneB.ID, map[string]interface{}{"title": "bad"})},
		{name: "cross task target", input: proposalInput("task.update", taskB.ID, map[string]interface{}{"title": "bad"})},
		{name: "cross milestone change", input: proposalInput("task.create", "", map[string]interface{}{"title": "bad", "milestone_id": milestoneB.ID})},
		{name: "cross assignee change", input: proposalInput("task.create", "", map[string]interface{}{"title": "bad", "assignee_id": fixture.ownerB})},
		{name: "hidden related change", input: proposalInput("task.create", "", map[string]interface{}{"title": "bad", "related_object_ids": []interface{}{hiddenObject}})},
		{name: "cross related change", input: proposalInput("task.create", "", map[string]interface{}{"title": "bad", "related_object_ids": []interface{}{crossObject}})},
		{name: "non-string related change", input: proposalInput("task.create", "", map[string]interface{}{"title": "bad", "related_object_ids": []interface{}{42.0}})},
		{name: "non-string milestone change", input: proposalInput("task.create", "", map[string]interface{}{"title": "bad", "milestone_id": 42.0})},
	}
	for _, testCase := range invalidProposals {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := fixture.store.CreateProposal(fixture.ctx, fixture.projectA, fixture.ownerA, testCase.input); !errors.Is(err, ErrReferenceInvalid) {
				t.Fatalf("proposal error=%v, want safe reference error", err)
			}
		})
	}

	pending, err := fixture.store.CreateProposal(fixture.ctx, fixture.projectA, fixture.ownerA, proposalInput("task.create", "", map[string]interface{}{
		"title":              "Proposal task",
		"milestone_id":       milestoneA.ID,
		"assignee_id":        fixture.memberA,
		"related_object_ids": []interface{}{activeObject, activeObject},
	}))
	if err != nil {
		t.Fatalf("create valid pending proposal: %v", err)
	}
	if _, err := fixture.db.ExecContext(fixture.ctx, `UPDATE data_objects SET status='hidden' WHERE object_id=$1`, activeObject); err != nil {
		t.Fatalf("hide proposal object: %v", err)
	}
	if _, err := fixture.store.ReviewProposal(fixture.ctx, fixture.projectA, pending.ID, fixture.ownerA, ReviewProposalInput{Decision: "accepted"}); !errors.Is(err, ErrReferenceInvalid) {
		t.Fatalf("accept after reference changed: %v", err)
	}
	fixture.assertProposalPending(t, pending.ID)
	fixture.assertTaskTitleCount(t, "Proposal task", 0)

	if _, err := fixture.db.ExecContext(fixture.ctx, `UPDATE data_objects SET status='active' WHERE object_id=$1`, activeObject); err != nil {
		t.Fatalf("restore proposal object: %v", err)
	}
	accepted, err := fixture.store.ReviewProposal(fixture.ctx, fixture.projectA, pending.ID, fixture.ownerA, ReviewProposalInput{Decision: "accepted"})
	if err != nil || accepted.Status != "accepted" {
		t.Fatalf("accept revalidated proposal: item=%#v err=%v", accepted, err)
	}
	created := fixture.taskByTitle(t, "Proposal task")
	if created.MilestoneID != milestoneA.ID || created.AssigneeID != fixture.memberA || len(created.RelatedObjectIDs) != 2 {
		t.Fatalf("proposal did not apply references: %#v", created)
	}

	updateProposal, err := fixture.store.CreateProposal(fixture.ctx, fixture.projectA, fixture.ownerA, proposalInput("task.update", taskA.ID, map[string]interface{}{
		"milestone_id":       milestoneA.ID,
		"assignee_id":        fixture.memberA,
		"related_object_ids": []interface{}{activeObject},
	}))
	if err != nil {
		t.Fatalf("create task update proposal: %v", err)
	}
	if _, err := fixture.store.ReviewProposal(fixture.ctx, fixture.projectA, updateProposal.ID, fixture.ownerA, ReviewProposalInput{Decision: "accepted"}); err != nil {
		t.Fatalf("accept task update proposal: %v", err)
	}
	updated, err := fixture.store.GetTask(fixture.ctx, fixture.projectA, taskA.ID)
	if err != nil || updated.MilestoneID != milestoneA.ID || updated.AssigneeID != fixture.memberA || len(updated.RelatedObjectIDs) != 1 || updated.RelatedObjectIDs[0] != activeObject {
		t.Fatalf("applied task update references: task=%#v err=%v", updated, err)
	}

	membershipProposal, err := fixture.store.CreateProposal(fixture.ctx, fixture.projectA, fixture.ownerA, proposalInput("task.create", "", map[string]interface{}{"title": "Removed member task", "assignee_id": fixture.memberA}))
	if err != nil {
		t.Fatalf("create membership proposal: %v", err)
	}
	if _, err := fixture.db.ExecContext(fixture.ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, fixture.projectA, fixture.memberA); err != nil {
		t.Fatalf("remove proposed assignee: %v", err)
	}
	if _, err := fixture.store.ReviewProposal(fixture.ctx, fixture.projectA, membershipProposal.ID, fixture.ownerA, ReviewProposalInput{Decision: "accepted"}); !errors.Is(err, ErrReferenceInvalid) {
		t.Fatalf("accept after member removal: %v", err)
	}
	fixture.assertProposalPending(t, membershipProposal.ID)

	targetProposal, err := fixture.store.CreateProposal(fixture.ctx, fixture.projectA, fixture.ownerA, proposalInput("task.update", taskA.ID, map[string]interface{}{"title": "Deleted target"}))
	if err != nil {
		t.Fatalf("create target proposal: %v", err)
	}
	if err := fixture.store.DeleteTask(fixture.ctx, fixture.projectA, taskA.ID, fixture.ownerA); err != nil {
		t.Fatalf("delete proposal target: %v", err)
	}
	if _, err := fixture.store.ReviewProposal(fixture.ctx, fixture.projectA, targetProposal.ID, fixture.ownerA, ReviewProposalInput{Decision: "accepted"}); !errors.Is(err, ErrReferenceInvalid) {
		t.Fatalf("accept after target deletion: %v", err)
	}
	fixture.assertProposalPending(t, targetProposal.ID)
}

func TestPostgresConcurrentMilestoneDeletionPreservesTaskProject(t *testing.T) {
	fixture := newProgressReferenceFixture(t)
	milestone := fixture.createMilestone(t, fixture.projectA, fixture.ownerA, "Concurrent milestone")
	insertReached := make(chan struct{})
	releaseInsert := make(chan struct{})
	blockingManager := transaction.Manager{DB: blockingBeginner{DB: fixture.db, InsertReached: insertReached, ReleaseInsert: releaseInsert}}
	store := fixture.store
	store.Transaction = blockingManager
	createResult := make(chan struct {
		task Task
		err  error
	}, 1)
	go func() {
		task, err := store.CreateTask(fixture.ctx, fixture.projectA, fixture.ownerA, CreateTaskInput{Title: "Concurrent task", MilestoneID: milestone.ID}, "human")
		createResult <- struct {
			task Task
			err  error
		}{task: task, err: err}
	}()
	<-insertReached
	deleteResult := make(chan error, 1)
	go func() {
		_, err := fixture.db.ExecContext(fixture.ctx, `DELETE FROM progress_milestones WHERE project_id=$1 AND milestone_id=$2`, fixture.projectA, milestone.ID)
		deleteResult <- err
	}()
	select {
	case err := <-deleteResult:
		t.Fatalf("delete should wait for the reference mutation lock, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseInsert)
	created := <-createResult
	if created.err != nil {
		t.Fatalf("create task during delete race: %v", created.err)
	}
	if err := <-deleteResult; err != nil {
		t.Fatalf("delete milestone after task commit: %v", err)
	}
	var projectID string
	var milestoneID *string
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT project_id::text,milestone_id::text FROM progress_tasks WHERE task_id=$1`, created.task.ID).Scan(&projectID, &milestoneID); err != nil {
		t.Fatalf("read raced task: %v", err)
	}
	if projectID != fixture.projectA || milestoneID != nil {
		t.Fatalf("partial SET NULL changed Project scope: project=%s milestone=%v", projectID, milestoneID)
	}
}

type blockingBeginner struct {
	DB            *sql.DB
	InsertReached chan<- struct{}
	ReleaseInsert <-chan struct{}
}

func (beginner blockingBeginner) Begin(ctx context.Context, options *sql.TxOptions) (transaction.Tx, error) {
	tx, err := beginner.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &blockingProgressTx{Tx: tx, insertReached: beginner.InsertReached, releaseInsert: beginner.ReleaseInsert}, nil
}

type blockingProgressTx struct {
	*sql.Tx
	once          sync.Once
	insertReached chan<- struct{}
	releaseInsert <-chan struct{}
}

func (tx *blockingProgressTx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO progress_tasks") {
		tx.once.Do(func() { close(tx.insertReached) })
		select {
		case <-tx.releaseInsert:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return tx.Tx.ExecContext(ctx, query, args...)
}

type progressReferenceFixture struct {
	ctx       context.Context
	db        *sql.DB
	generator identity.Generator
	now       time.Time
	ownerA    string
	ownerB    string
	memberA   string
	nonmember string
	projectA  string
	projectB  string
	store     PostgresStore
}

func newProgressReferenceFixture(t *testing.T) progressReferenceFixture {
	t.Helper()
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}
	generator := identity.Generator{}
	ownerA, ownerB := generator.MustNew(), generator.MustNew()
	memberA, nonmember := generator.MustNew(), generator.MustNew()
	projectA, projectB := generator.MustNew(), generator.MustNew()
	now := time.Date(2026, time.August, 6, 3, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES
		($1,$5,'Progress Owner A','test','active',$9,$9),
		($2,$6,'Progress Owner B','test','active',$9,$9),
		($3,$7,'Progress Member A','test','active',$9,$9),
		($4,$8,'Progress Nonmember','test','active',$9,$9)
	`, ownerA, ownerB, memberA, nonmember, ownerA+"@progress-reference.test", ownerB+"@progress-reference.test", memberA+"@progress-reference.test", nonmember+"@progress-reference.test", now); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES
		($1,'Progress Project A',$3,$5,$5),
		($2,'Progress Project B',$4,$5,$5)
	`, projectA, projectB, ownerA, ownerB, now); err != nil {
		t.Fatalf("insert projects: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES
		($1,$3,'owner',$5,$5),
		($1,$4,'editor',$5,$5),
		($2,$6,'owner',$5,$5)
	`, projectA, projectB, ownerA, memberA, now, ownerB); err != nil {
		t.Fatalf("insert memberships: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM system_outbox WHERE project_id IN ($1,$2)`, projectA, projectB)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM projects WHERE project_id IN ($1,$2)`, projectA, projectB)
		_, _ = db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE user_id IN ($1,$2,$3,$4)`, ownerA, ownerB, memberA, nonmember)
	})
	manager := transaction.Manager{DB: transaction.SQLBeginner{DB: db}}
	fixedClock := clock.Fixed{Time: now}
	store := PostgresStore{
		Clock:       fixedClock,
		DB:          db,
		Generator:   generator,
		Outbox:      outbox.Writer{Clock: fixedClock, Generator: generator},
		References:  datahub.PostgresStore{},
		Transaction: manager,
	}
	return progressReferenceFixture{ctx: ctx, db: db, generator: generator, now: now, ownerA: ownerA, ownerB: ownerB, memberA: memberA, nonmember: nonmember, projectA: projectA, projectB: projectB, store: store}
}

func (fixture progressReferenceFixture) createMilestone(t *testing.T, projectID, actorID, title string) Milestone {
	t.Helper()
	item, err := fixture.store.CreateMilestone(fixture.ctx, projectID, actorID, CreateMilestoneInput{Title: title})
	if err != nil {
		t.Fatalf("create milestone %s: %v", title, err)
	}
	return item
}

func (fixture progressReferenceFixture) createTask(t *testing.T, projectID, actorID string, input CreateTaskInput) Task {
	t.Helper()
	item, err := fixture.store.CreateTask(fixture.ctx, projectID, actorID, input, "human")
	if err != nil {
		t.Fatalf("create task %s: %v", input.Title, err)
	}
	return item
}

func (fixture progressReferenceFixture) createObject(t *testing.T, projectID, status string) string {
	t.Helper()
	objectID, sourceID := fixture.generator.MustNew(), fixture.generator.MustNew()
	if _, err := fixture.db.ExecContext(fixture.ctx, `
		INSERT INTO data_objects(object_id,project_id,object_type,source_module,source_id,title,status,occurred_at,created_at,updated_at)
		VALUES($1,$2,'test-object','progress-test',$3,'Progress reference object',$4,$5,$5,$5)
	`, objectID, projectID, sourceID, status, fixture.now); err != nil {
		t.Fatalf("create Data Hub object: %v", err)
	}
	return objectID
}

func (fixture progressReferenceFixture) assertMissing(t *testing.T, table, column, id string) {
	t.Helper()
	var count int
	query := "SELECT COUNT(*) FROM " + table + " WHERE " + column + "=$1"
	if err := fixture.db.QueryRowContext(fixture.ctx, query, id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expected %s %s to be absent: count=%d err=%v", table, id, count, err)
	}
}

func (fixture progressReferenceFixture) assertProposalPending(t *testing.T, proposalID string) {
	t.Helper()
	var status string
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT status FROM progress_proposals WHERE proposal_id=$1`, proposalID).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("proposal should remain pending: status=%q err=%v", status, err)
	}
}

func (fixture progressReferenceFixture) assertTaskTitleCount(t *testing.T, title string, want int) {
	t.Helper()
	var count int
	if err := fixture.db.QueryRowContext(fixture.ctx, `SELECT COUNT(*) FROM progress_tasks WHERE project_id=$1 AND title=$2`, fixture.projectA, title).Scan(&count); err != nil || count != want {
		t.Fatalf("task title count %q: got=%d want=%d err=%v", title, count, want, err)
	}
}

func (fixture progressReferenceFixture) taskByTitle(t *testing.T, title string) Task {
	t.Helper()
	item, err := scanTask(fixture.db.QueryRowContext(fixture.ctx, taskSelect+` WHERE project_id=$1 AND title=$2`, fixture.projectA, title).Scan)
	if err != nil {
		t.Fatalf("read task by title %q: %v", title, err)
	}
	return item
}

func proposalInput(proposalType, targetID string, changes map[string]interface{}) CreateProposalInput {
	return CreateProposalInput{ProposalType: proposalType, TargetID: targetID, Title: "Reference proposal", Changes: changes, SourceRunID: "run-reference-test"}
}

func assertPostgresCode(t *testing.T, err error, code string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != code {
		t.Fatalf("PostgreSQL error=%v, want SQLSTATE %s", err, code)
	}
}
