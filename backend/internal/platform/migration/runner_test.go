package migration

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMigrationNamesReturnsSortedUpMigrations(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{
		"000002_second.up.sql",
		"000001_first.up.sql",
		"000001_first.down.sql",
		"README.md",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("-- test"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	names, err := migrationNames(directory)
	if err != nil {
		t.Fatalf("read migration names: %v", err)
	}
	expected := []string{"000001_first.up.sql", "000002_second.up.sql"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("unexpected migrations: %#v", names)
	}
}
