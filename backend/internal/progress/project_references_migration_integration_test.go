package progress

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

func TestProgressProjectReferenceMigrationFreshUpDownUp(t *testing.T) {
	db := openProgressMigrationDatabase(t)
	connection, _ := newProgressMigrationSchema(t, db)
	ctx := context.Background()
	applyProgressMigrationBase(t, ctx, connection)

	generator := identity.Generator{}
	owner, member, project := generator.MustNew(), generator.MustNew(), generator.MustNew()
	hiddenObject, taskID := generator.MustNew(), generator.MustNew()
	proposalID, pendingProposalID := generator.MustNew(), generator.MustNew()
	now := time.Date(2026, time.August, 6, 4, 0, 0, 0, time.UTC)
	if _, err := connection.ExecContext(ctx, `
		INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES
		($1,$3,'Migration Owner','test','active',$5,$5),
		($2,$4,'Migration Member','test','active',$5,$5)
	`, owner, member, owner+"@progress-migration.test", member+"@progress-migration.test", now); err != nil {
		t.Fatalf("seed migration identities: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Migration Project',$2,$3,$3)`, project, owner, now); err != nil {
		t.Fatalf("seed migration project: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES($1,$2,'owner',$4,$4),($1,$3,'editor',$4,$4)`, project, owner, member, now); err != nil {
		t.Fatalf("seed migration memberships: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `
		INSERT INTO data_objects(object_id,project_id,object_type,source_module,source_id,title,status,occurred_at,created_at,updated_at)
		VALUES($1,$2,'test-object','migration-test',$3,'Hidden historical object','hidden',$4,$4,$4)
	`, hiddenObject, project, generator.MustNew(), now); err != nil {
		t.Fatalf("seed historical references: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO progress_tasks(task_id,project_id,title,status,assignee_id,source,related_object_ids,created_by,updated_by,created_at,updated_at) VALUES($1,$2,'Historical task','todo',$3,'human',jsonb_build_array($4::text,$4::text),$3,$3,$5,$5)`, taskID, project, owner, hiddenObject, now); err != nil {
		t.Fatalf("seed historical task: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO progress_proposals(proposal_id,project_id,proposal_type,title,changes,source,source_run_id,proposed_by,status,created_at,updated_at) VALUES($1,$2,'task.create','Terminal legacy proposal','{"related_object_ids":[42]}'::jsonb,'agent','legacy',$3,'accepted',$4,$4)`, proposalID, project, owner, now); err != nil {
		t.Fatalf("seed terminal proposal: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO progress_proposals(proposal_id,project_id,proposal_type,title,changes,source,source_run_id,proposed_by,status,created_at,updated_at) VALUES($1,$2,'task.create','Pending historical proposal',jsonb_build_object('title','Pending hidden object','related_object_ids',jsonb_build_array($3::text)),'agent','legacy',$4,'pending',$5,$5)`, pendingProposalID, project, hiddenObject, owner, now); err != nil {
		t.Fatalf("seed pending hidden-object proposal: %v", err)
	}

	execProgressMigration(t, ctx, connection, "000024_progress_project_references.up.sql")
	assertConstraintDefinitionContains(t, ctx, connection, "progress_tasks_project_milestone_fk", "ON DELETE SET NULL (milestone_id)")
	assertConstraintDefinitionContains(t, ctx, connection, "progress_tasks_project_assignee_fk", "ON DELETE SET NULL (assignee_id)")
	assertConstraintExists(t, ctx, connection, "progress_dependencies_project_task_fk", true)
	assertConstraintExists(t, ctx, connection, "progress_reminders_project_milestone_fk", true)

	if _, err := connection.ExecContext(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, project, member); err != nil {
		t.Fatalf("delete unused member after migration: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, project, owner); err != nil {
		t.Fatalf("delete assigned member after migration: %v", err)
	}
	var storedProject string
	var storedAssignee *string
	if err := connection.QueryRowContext(ctx, `SELECT project_id::text,assignee_id::text FROM progress_tasks WHERE task_id=$1`, taskID).Scan(&storedProject, &storedAssignee); err != nil {
		t.Fatalf("read task after member removal: %v", err)
	}
	if storedProject != project || storedAssignee != nil {
		t.Fatalf("migration SET NULL touched Project scope: project=%s assignee=%v", storedProject, storedAssignee)
	}

	execProgressMigration(t, ctx, connection, "000024_progress_project_references.down.sql")
	assertConstraintExists(t, ctx, connection, "progress_tasks_project_milestone_fk", false)
	assertConstraintExists(t, ctx, connection, "progress_tasks_milestone_id_fkey", true)
	execProgressMigration(t, ctx, connection, "000024_progress_project_references.up.sql")
	assertConstraintExists(t, ctx, connection, "progress_tasks_project_milestone_fk", true)
}

func TestProgressHumanWorkbenchMigrationFreshUpDownUp(t *testing.T) {
	db := openProgressMigrationDatabase(t)
	connection, _ := newProgressMigrationSchema(t, db)
	ctx := context.Background()
	applyProgressMigrationBase(t, ctx, connection)
	execProgressMigration(t, ctx, connection, "000024_progress_project_references.up.sql")
	if _, err := connection.ExecContext(ctx, `ALTER TABLE progress_proposals ADD COLUMN source_evaluation_id UUID`); err != nil {
		t.Fatalf("add tracking provenance fixture column: %v", err)
	}

	generator := identity.Generator{}
	owner, project := generator.MustNew(), generator.MustNew()
	taskID, milestoneID := generator.MustNew(), generator.MustNew()
	taskProposalID, milestoneProposalID := generator.MustNew(), generator.MustNew()
	now := time.Date(2026, time.August, 11, 5, 0, 0, 0, time.UTC)
	if _, err := connection.ExecContext(ctx, `INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES($1,$2,'Workbench Migration Owner','test','active',$3,$3)`, owner, owner+"@workbench-migration.test", now); err != nil {
		t.Fatalf("seed workbench migration owner: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Workbench Migration Project',$2,$3,$3)`, project, owner, now); err != nil {
		t.Fatalf("seed workbench migration project: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES($1,$2,'owner',$3,$3)`, project, owner, now); err != nil {
		t.Fatalf("seed workbench migration member: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO progress_milestones(milestone_id,project_id,title,status,source,created_by,updated_by,created_at,updated_at) VALUES($1,$2,'Legacy cancelled milestone','cancelled','human',$3,$3,$4,$4)`, milestoneID, project, owner, now); err != nil {
		t.Fatalf("seed workbench migration milestone: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO progress_tasks(task_id,project_id,title,status,source,created_by,updated_by,created_at,updated_at) VALUES($1,$2,'Legacy cancelled task','cancelled','human',$3,$3,$4,$4)`, taskID, project, owner, now); err != nil {
		t.Fatalf("seed workbench migration task: %v", err)
	}

	execProgressMigration(t, ctx, connection, "000037_progress_human_workbench.up.sql")
	var taskStatus, workState, milestoneStatus string
	var targetHasTime bool
	if err := connection.QueryRowContext(ctx, `SELECT status,work_state FROM progress_tasks WHERE task_id=$1`, taskID).Scan(&taskStatus, &workState); err != nil {
		t.Fatalf("read migrated task: %v", err)
	}
	if err := connection.QueryRowContext(ctx, `SELECT status,target_has_time FROM progress_milestones WHERE milestone_id=$1`, milestoneID).Scan(&milestoneStatus, &targetHasTime); err != nil {
		t.Fatalf("read migrated milestone: %v", err)
	}
	if taskStatus != TaskTodo || workState != TaskTodo || milestoneStatus != StatusPlanned || targetHasTime {
		t.Fatalf("unexpected migrated state: task=%s work=%s milestone=%s has_time=%v", taskStatus, workState, milestoneStatus, targetHasTime)
	}
	if _, err := connection.ExecContext(ctx, `
		INSERT INTO progress_proposals(proposal_id,project_id,proposal_type,target_id,title,changes,source,proposed_by,status,created_at,updated_at)
		VALUES
		($1,$3,'task.complete',$4,'Complete task','{}'::jsonb,'system',$5,'pending',$6,$6),
		($2,$3,'milestone.complete',$7,'Complete milestone','{}'::jsonb,'system',$5,'pending',$6,$6)
	`, taskProposalID, milestoneProposalID, project, taskID, owner, now, milestoneID); err != nil {
		t.Fatalf("seed completion proposals: %v", err)
	}

	execProgressMigration(t, ctx, connection, "000037_progress_human_workbench.down.sql")
	var taskProposalType, taskCompletionStatus, milestoneProposalType, milestoneCompletionStatus string
	if err := connection.QueryRowContext(ctx, `SELECT proposal_type,changes->>'status' FROM progress_proposals WHERE proposal_id=$1`, taskProposalID).Scan(&taskProposalType, &taskCompletionStatus); err != nil {
		t.Fatalf("read rolled-back task proposal: %v", err)
	}
	if err := connection.QueryRowContext(ctx, `SELECT proposal_type,changes->>'status' FROM progress_proposals WHERE proposal_id=$1`, milestoneProposalID).Scan(&milestoneProposalType, &milestoneCompletionStatus); err != nil {
		t.Fatalf("read rolled-back milestone proposal: %v", err)
	}
	if taskProposalType != "task.update" || taskCompletionStatus != TaskDone || milestoneProposalType != "milestone.update" || milestoneCompletionStatus != StatusCompleted {
		t.Fatalf("completion proposal rollback lost meaning: task=%s/%s milestone=%s/%s", taskProposalType, taskCompletionStatus, milestoneProposalType, milestoneCompletionStatus)
	}
	execProgressMigration(t, ctx, connection, "000037_progress_human_workbench.up.sql")
}

func TestProgressProjectReferenceMigrationRejectsDirtyDataTransactionally(t *testing.T) {
	db := openProgressMigrationDatabase(t)
	connection, _ := newProgressMigrationSchema(t, db)
	ctx := context.Background()
	applyProgressMigrationBase(t, ctx, connection)

	generator := identity.Generator{}
	ownerA, ownerB := generator.MustNew(), generator.MustNew()
	projectA, projectB := generator.MustNew(), generator.MustNew()
	milestoneB, taskA := generator.MustNew(), generator.MustNew()
	now := time.Date(2026, time.August, 6, 4, 30, 0, 0, time.UTC)
	if _, err := connection.ExecContext(ctx, `
		INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES
		($1,$3,'Dirty Owner A','test','active',$5,$5),
		($2,$4,'Dirty Owner B','test','active',$5,$5)
	`, ownerA, ownerB, ownerA+"@dirty-progress-migration.test", ownerB+"@dirty-progress-migration.test", now); err != nil {
		t.Fatalf("seed dirty identities: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Dirty Project A',$3,$5,$5),($2,'Dirty Project B',$4,$5,$5)`, projectA, projectB, ownerA, ownerB, now); err != nil {
		t.Fatalf("seed dirty projects: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,role,created_at,updated_at) VALUES($1,$3,'owner',$5,$5),($2,$4,'owner',$5,$5)`, projectA, projectB, ownerA, ownerB, now); err != nil {
		t.Fatalf("seed dirty memberships: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `
		INSERT INTO progress_milestones(milestone_id,project_id,title,status,source,created_by,updated_by,created_at,updated_at)
		VALUES($1,$2,'Cross milestone','planned','human',$3,$3,$4,$4)
	`, milestoneB, projectB, ownerB, now); err != nil {
		t.Fatalf("seed dirty cross-Project milestone: %v", err)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO progress_tasks(task_id,project_id,milestone_id,title,status,source,created_by,updated_by,created_at,updated_at) VALUES($1,$2,$3,'Dirty task','todo','human',$4,$4,$5,$5)`, taskA, projectA, milestoneB, ownerA, now); err != nil {
		t.Fatalf("seed dirty cross-Project task: %v", err)
	}

	contents := readProgressMigration(t, "000024_progress_project_references.up.sql")
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin dirty migration: %v", err)
	}
	_, migrationErr := tx.ExecContext(ctx, contents)
	if migrationErr == nil {
		_ = tx.Rollback()
		t.Fatal("dirty migration unexpectedly succeeded")
	}
	_ = tx.Rollback()
	message := migrationErr.Error()
	if !strings.Contains(message, "task milestone references (1 rows)") || strings.Contains(message, taskA) || strings.Contains(message, milestoneB) {
		t.Fatalf("dirty migration error is not category/count-only: %s", message)
	}
	var storedProject, storedMilestone string
	if err := connection.QueryRowContext(ctx, `SELECT project_id::text,milestone_id::text FROM progress_tasks WHERE task_id=$1`, taskA).Scan(&storedProject, &storedMilestone); err != nil {
		t.Fatalf("read dirty row after rollback: %v", err)
	}
	if storedProject != projectA || storedMilestone != milestoneB {
		t.Fatalf("dirty migration modified data: project=%s milestone=%s", storedProject, storedMilestone)
	}
	assertConstraintExists(t, ctx, connection, "progress_tasks_project_milestone_fk", false)
	var functionExists bool
	if err := connection.QueryRowContext(ctx, `SELECT to_regprocedure('progress_jsonb_uuid_array_is_valid(jsonb)') IS NOT NULL`).Scan(&functionExists); err != nil || functionExists {
		t.Fatalf("failed migration left helper function: exists=%v err=%v", functionExists, err)
	}
}

func openProgressMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open migration PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping migration PostgreSQL: %v", err)
	}
	return db
}

func newProgressMigrationSchema(t *testing.T, db *sql.DB) (*sql.Conn, string) {
	t.Helper()
	ctx := context.Background()
	connection, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve migration connection: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	schema := "progress_migration_" + strings.ReplaceAll(identity.Generator{}.MustNew(), "-", "")
	quoted := `"` + schema + `"`
	if _, err := connection.ExecContext(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatalf("create migration schema: %v", err)
	}
	if _, err := connection.ExecContext(ctx, "SET search_path TO "+quoted); err != nil {
		t.Fatalf("set migration search path: %v", err)
	}
	t.Cleanup(func() {
		_, _ = connection.ExecContext(context.Background(), "SET search_path TO public")
		_, _ = connection.ExecContext(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
	})
	return connection, schema
}

func applyProgressMigrationBase(t *testing.T, ctx context.Context, connection *sql.Conn) {
	t.Helper()
	for _, name := range []string{
		"000003_auth_project.up.sql",
		"000007_datahub.up.sql",
		"000016_progress.up.sql",
		"000023_progress_reminder_processing.up.sql",
	} {
		execProgressMigration(t, ctx, connection, name)
	}
}

func execProgressMigration(t *testing.T, ctx context.Context, connection *sql.Conn, name string) {
	t.Helper()
	if _, err := connection.ExecContext(ctx, readProgressMigration(t, name)); err != nil {
		t.Fatalf("execute migration %s: %v", name, err)
	}
}

func readProgressMigration(t *testing.T, name string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migration test source path")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations", name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(contents)
}

func assertConstraintExists(t *testing.T, ctx context.Context, connection *sql.Conn, name string, want bool) {
	t.Helper()
	var exists bool
	if err := connection.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname=$1 AND connamespace=current_schema()::regnamespace)`, name).Scan(&exists); err != nil || exists != want {
		t.Fatalf("constraint %s exists=%v want=%v err=%v", name, exists, want, err)
	}
}

func assertConstraintDefinitionContains(t *testing.T, ctx context.Context, connection *sql.Conn, name, fragment string) {
	t.Helper()
	var definition string
	if err := connection.QueryRowContext(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1 AND connamespace=current_schema()::regnamespace`, name).Scan(&definition); err != nil {
		t.Fatalf("read constraint %s: %v", name, err)
	}
	if !strings.Contains(definition, fragment) {
		t.Fatalf("constraint %s definition=%q, want %q", name, definition, fragment)
	}
}
