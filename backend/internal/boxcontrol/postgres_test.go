package boxcontrol

import (
	"database/sql"
	"testing"
	"time"
)

type nullableTaskScanner struct{}

func (nullableTaskScanner) Scan(destinations ...interface{}) error {
	*destinations[0].(*string) = "task-1"
	*destinations[1].(*string) = "experiment-1"
	*destinations[2].(*string) = "project-1"
	*destinations[3].(*string) = "box-1"
	*destinations[4].(*string) = TaskPreparing
	*destinations[5].(*int) = 1
	*destinations[6].(*int) = 3
	*destinations[7].(*sql.NullTime) = sql.NullTime{}
	*destinations[8].(*sql.NullTime) = sql.NullTime{}
	*destinations[9].(*[]byte) = []byte(`{"schema_version":"1"}`)
	*destinations[11].(*sql.NullString) = sql.NullString{}
	*destinations[12].(*sql.NullString) = sql.NullString{}
	*destinations[13].(*[]byte) = nil
	*destinations[14].(*sql.NullString) = sql.NullString{}
	now := time.Now().UTC()
	*destinations[15].(*time.Time) = now
	*destinations[16].(*sql.NullTime) = sql.NullTime{}
	*destinations[17].(*sql.NullTime) = sql.NullTime{}
	*destinations[18].(*time.Time) = now
	return nil
}

func TestScanTaskAcceptsNullableInitialResultFields(t *testing.T) {
	task, err := scanTask(nullableTaskScanner{})
	if err != nil {
		t.Fatal(err)
	}
	if task.ErrorCode != "" || task.ErrorMessage != "" || task.Summary != "" || task.ResourceUsage == nil || len(task.ResourceUsage) != 0 {
		t.Fatalf("nullable task fields were not normalized: %#v", task)
	}
}
