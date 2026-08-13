package migration

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMigrationNamesReturnsSortedUpMigrations(t *testing.T) {
	directory := t.TempDir()
	writeMigrationPair(t, directory, "000002_second")
	writeMigrationPair(t, directory, "000001_first")
	writeFixture(t, directory, "README.md")

	names, err := migrationNames(directory)
	if err != nil {
		t.Fatalf("read migration names: %v", err)
	}
	expected := []string{"000001_first.up.sql", "000002_second.up.sql"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("unexpected migrations: %#v", names)
	}
}

func TestMigrationNamesRejectsDuplicateNumbers(t *testing.T) {
	directory := t.TempDir()
	writeMigrationPair(t, directory, "000001_first")
	writeMigrationPair(t, directory, "000001_duplicate")

	_, err := migrationNames(directory)
	if err == nil || !strings.Contains(err.Error(), "unique and continuous") {
		t.Fatalf("duplicate migration error = %v", err)
	}
}

func TestMigrationNamesRejectsSequenceGaps(t *testing.T) {
	directory := t.TempDir()
	writeMigrationPair(t, directory, "000001_first")
	writeMigrationPair(t, directory, "000003_third")

	_, err := migrationNames(directory)
	if err == nil || !strings.Contains(err.Error(), "want 000002") {
		t.Fatalf("gap migration error = %v", err)
	}
}

func TestMigrationNamesRequiresDownMigration(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, directory, "000001_first.up.sql")

	_, err := migrationNames(directory)
	if err == nil || !strings.Contains(err.Error(), "missing 000001_first.down.sql") {
		t.Fatalf("missing down migration error = %v", err)
	}
}

func TestMigrationNamesRejectsInvalidFilename(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, directory, "1_first.up.sql")
	writeFixture(t, directory, "1_first.down.sql")

	_, err := migrationNames(directory)
	if err == nil || !strings.Contains(err.Error(), "invalid migration filename") {
		t.Fatalf("invalid migration error = %v", err)
	}
}

func TestLegacyMigrationAliasesRemainExplicit(t *testing.T) {
	expected := map[string][]string{
		"000026_agent_sessions.up.sql": {"000023_agent_sessions.up.sql"},
		"000029_model_stage7.up.sql":   {"000022_model_stage7.up.sql"},
		"000030_model_notion_oauth.up.sql": {
			"000023_model_notion_oauth.up.sql",
		},
		"000031_notification_routing_model.up.sql": {
			"000023_notification_routing_model.up.sql",
			"000024_notification_routing_model.up.sql",
		},
		"000041_stage8_box_experiment.up.sql": {
			"000033_stage8_box_experiment.up.sql",
		},
	}
	if !reflect.DeepEqual(legacyMigrationAliases, expected) {
		t.Fatalf("legacy migration aliases changed: %#v", legacyMigrationAliases)
	}
}

func writeMigrationPair(t *testing.T, directory, stem string) {
	t.Helper()
	writeFixture(t, directory, stem+".up.sql")
	writeFixture(t, directory, stem+".down.sql")
}

func writeFixture(t *testing.T, directory, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte("-- test"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
