package migration

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v4/stdlib"
)

func TestRunnerAppliesFreshCanonicalCatalog(t *testing.T) {
	db := openMigrationRunnerDatabase(t)
	directory := migrationTestDirectory(t)
	names, err := migrationNames(directory)
	if err != nil {
		t.Fatalf("read canonical migration catalog: %v", err)
	}

	runMigrations(t, db, directory)
	assertMigrationCount(t, db, len(names))
	for _, name := range names {
		assertMigrationRecorded(t, db, name)
	}

	runMigrations(t, db, directory)
	assertMigrationCount(t, db, len(names))
}

func TestRunnerReconcilesLegacyMigrationNamesWithoutReexecution(t *testing.T) {
	db := openMigrationRunnerDatabase(t)
	directory := migrationTestDirectory(t)
	names, err := migrationNames(directory)
	if err != nil {
		t.Fatalf("read canonical migration catalog: %v", err)
	}
	overrides := map[string]string{
		"000026_agent_sessions.up.sql":             "000023_agent_sessions.up.sql",
		"000029_model_stage7.up.sql":               "000022_model_stage7.up.sql",
		"000030_model_notion_oauth.up.sql":         "000023_model_notion_oauth.up.sql",
		"000031_notification_routing_model.up.sql": "000024_notification_routing_model.up.sql",
		"000041_stage8_box_experiment.up.sql":      "000033_stage8_box_experiment.up.sql",
	}
	seedMigrations(t, db, directory, names, overrides)
	insertMigrationSentinel(t, db, "legacy-upgrade")

	runMigrations(t, db, directory)
	assertMigrationCount(t, db, len(names)+len(overrides))
	for canonical, legacy := range overrides {
		assertMigrationRecorded(t, db, canonical)
		assertMigrationRecorded(t, db, legacy)
	}
	assertMigrationSentinel(t, db, "legacy-upgrade")

	runMigrations(t, db, directory)
	assertMigrationCount(t, db, len(names)+len(overrides))
}

func TestRunnerHandlesPartialLegacyState(t *testing.T) {
	db := openMigrationRunnerDatabase(t)
	directory := migrationTestDirectory(t)
	names, err := migrationNames(directory)
	if err != nil {
		t.Fatalf("read canonical migration catalog: %v", err)
	}
	seedMigrations(t, db, directory, names[:28], nil)
	applyAndRecordMigration(
		t,
		db,
		directory,
		"000029_model_stage7.up.sql",
		"000022_model_stage7.up.sql",
	)

	runMigrations(t, db, directory)
	assertMigrationRecorded(t, db, "000029_model_stage7.up.sql")
	assertMigrationRecorded(t, db, "000030_model_notion_oauth.up.sql")
	assertMigrationRecorded(t, db, "000031_notification_routing_model.up.sql")
	assertMigrationCount(t, db, len(names)+1)
}

func TestRunnerReconcilesPreMergeNotificationAlias(t *testing.T) {
	db := openMigrationRunnerDatabase(t)
	directory := migrationTestDirectory(t)
	names, err := migrationNames(directory)
	if err != nil {
		t.Fatalf("read canonical migration catalog: %v", err)
	}
	overrides := map[string]string{
		"000031_notification_routing_model.up.sql": "000023_notification_routing_model.up.sql",
	}
	seedMigrations(t, db, directory, names, overrides)

	runMigrations(t, db, directory)
	assertMigrationRecorded(t, db, "000031_notification_routing_model.up.sql")
	assertMigrationRecorded(t, db, "000023_notification_routing_model.up.sql")
	assertMigrationCount(t, db, len(names)+1)
}

func TestRunnerAcceptsCanonicalAndLegacyRecordsTogether(t *testing.T) {
	db := openMigrationRunnerDatabase(t)
	directory := migrationTestDirectory(t)
	names, err := migrationNames(directory)
	if err != nil {
		t.Fatalf("read canonical migration catalog: %v", err)
	}
	seedMigrations(t, db, directory, names, nil)
	legacyCount := 0
	for _, aliases := range legacyMigrationAliases {
		for _, legacy := range aliases {
			if _, err := db.Exec(
				"INSERT INTO system_schema_migrations (version) VALUES ($1)",
				legacy,
			); err != nil {
				t.Fatalf("record coexisting legacy migration %s: %v", legacy, err)
			}
			legacyCount++
		}
	}

	runMigrations(t, db, directory)
	assertMigrationCount(t, db, len(names)+legacyCount)
}

func TestRecentMigrationsSupportDownAndUp(t *testing.T) {
	db := openMigrationRunnerDatabase(t)
	directory := migrationTestDirectory(t)
	names, err := migrationNames(directory)
	if err != nil {
		t.Fatalf("read canonical migration catalog: %v", err)
	}
	seedMigrations(t, db, directory, names[:28], nil)
	for _, name := range []string{
		"000029_model_stage7.up.sql",
		"000030_model_notion_oauth.up.sql",
		"000031_notification_routing_model.up.sql",
		"000032_agent_progress_evaluation_source.up.sql",
	} {
		executeMigrationFile(t, db, directory, name)
	}

	for _, name := range []string{
		"000032_agent_progress_evaluation_source.down.sql",
		"000031_notification_routing_model.down.sql",
		"000030_model_notion_oauth.down.sql",
		"000029_model_stage7.down.sql",
	} {
		executeMigrationFile(t, db, directory, name)
	}
	assertTableExists(t, db, "model_sources", false)
	assertTableExists(t, db, "model_notion_oauth_authorizations", false)
	assertColumnExists(t, db, "notification_rules", "inbox_enabled", true)
	assertColumnExists(t, db, "agent_runs", "source_evaluation_id", false)

	for _, name := range []string{
		"000029_model_stage7.up.sql",
		"000030_model_notion_oauth.up.sql",
		"000031_notification_routing_model.up.sql",
		"000032_agent_progress_evaluation_source.up.sql",
	} {
		executeMigrationFile(t, db, directory, name)
	}
	assertTableExists(t, db, "model_sources", true)
	assertTableExists(t, db, "model_notion_oauth_authorizations", true)
	assertColumnExists(t, db, "notification_rules", "inbox_enabled", false)
	assertColumnExists(t, db, "agent_runs", "source_evaluation_id", true)
}

func openMigrationRunnerDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	adminDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open migration PostgreSQL: %v", err)
	}
	if err := adminDB.Ping(); err != nil {
		_ = adminDB.Close()
		t.Fatalf("ping migration PostgreSQL: %v", err)
	}

	schema := "migration_runner_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create migration schema: %v", err)
	}

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		_, _ = adminDB.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = adminDB.Close()
		t.Fatalf("parse MMDASH_TEST_DATABASE_URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		_, _ = adminDB.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = adminDB.Close()
		t.Fatalf("open schema-scoped migration PostgreSQL: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		_, _ = adminDB.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = adminDB.Close()
		t.Fatalf("ping schema-scoped migration PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = adminDB.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = adminDB.Close()
	})
	return db
}

func migrationTestDirectory(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migration test source path")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "migrations")
}

func runMigrations(t *testing.T, db *sql.DB, directory string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := (Runner{DB: db, Directory: directory}).Run(ctx); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
}

func seedMigrations(
	t *testing.T,
	db *sql.DB,
	directory string,
	names []string,
	recordOverrides map[string]string,
) {
	t.Helper()
	if _, err := db.Exec(`
		CREATE TABLE system_schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create seeded migration table: %v", err)
	}
	for _, name := range names {
		recordedName := name
		if legacyName := recordOverrides[name]; legacyName != "" {
			recordedName = legacyName
		}
		applyAndRecordMigration(t, db, directory, name, recordedName)
	}
}

func applyAndRecordMigration(t *testing.T, db *sql.DB, directory, filename, recordedName string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(directory, filename))
	if err != nil {
		t.Fatalf("read seeded migration %s: %v", filename, err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin seeded migration %s: %v", filename, err)
	}
	if _, err := tx.Exec(string(contents)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("execute seeded migration %s: %v", filename, err)
	}
	if _, err := tx.Exec(
		"INSERT INTO system_schema_migrations (version) VALUES ($1)",
		recordedName,
	); err != nil {
		_ = tx.Rollback()
		t.Fatalf("record seeded migration %s as %s: %v", filename, recordedName, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seeded migration %s: %v", filename, err)
	}
}

func executeMigrationFile(t *testing.T, db *sql.DB, directory, filename string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(directory, filename))
	if err != nil {
		t.Fatalf("read migration %s: %v", filename, err)
	}
	if _, err := db.Exec(string(contents)); err != nil {
		t.Fatalf("execute migration %s: %v", filename, err)
	}
}

func insertMigrationSentinel(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	if _, err := db.Exec(
		"INSERT INTO system_runtime_checks(check_name) VALUES($1)",
		name,
	); err != nil {
		t.Fatalf("insert migration sentinel: %v", err)
	}
}

func assertMigrationSentinel(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(
		"SELECT EXISTS (SELECT 1 FROM system_runtime_checks WHERE check_name=$1)",
		name,
	).Scan(&exists); err != nil || !exists {
		t.Fatalf("migration sentinel exists=%v err=%v", exists, err)
	}
}

func assertMigrationRecorded(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(
		"SELECT EXISTS (SELECT 1 FROM system_schema_migrations WHERE version=$1)",
		name,
	).Scan(&exists); err != nil || !exists {
		t.Fatalf("migration %s recorded=%v err=%v", name, exists, err)
	}
}

func assertMigrationCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM system_schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != want {
		t.Fatalf("migration count = %d, want %d", count, want)
	}
}

func assertTableExists(t *testing.T, db *sql.DB, name string, want bool) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(
		"SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL",
		name,
	).Scan(&exists); err != nil || exists != want {
		t.Fatalf("table %s exists=%v want=%v err=%v", name, exists, want, err)
	}
}

func assertColumnExists(t *testing.T, db *sql.DB, table, column string, want bool) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema=current_schema()
			  AND table_name=$1
			  AND column_name=$2
		)
	`, table, column).Scan(&exists); err != nil || exists != want {
		t.Fatalf("column %s.%s exists=%v want=%v err=%v", table, column, exists, want, err)
	}
}
