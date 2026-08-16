package boxcontrol

import (
	"fmt"
	"testing"
	"time"
)

type nullableTaskScanner struct{}

func (nullableTaskScanner) Scan(destinations ...interface{}) error {
	if len(destinations) != 30 {
		return fmt.Errorf("unexpected destination count: %d", len(destinations))
	}
	*destinations[0].(*string) = "task-1"
	*destinations[1].(*string) = "experiment-1"
	*destinations[2].(*string) = "project-1"
	*destinations[3].(*string) = "box-1"
	*destinations[4].(*string) = "00000000-0000-4000-8000-000000000001"
	*destinations[5].(*string) = TaskPreparing
	*destinations[6].(*int) = 1
	*destinations[7].(*int) = 1
	*destinations[8].(*[]byte) = []byte(`{"schema_version":"2"}`)
	*destinations[9].(*bool) = false
	*destinations[10].(*string) = "local-docker"
	*destinations[11].(*string) = "1"
	*destinations[12].(*int64) = 0
	*destinations[13].(*bool) = false
	*destinations[16].(*string) = ""
	*destinations[17].(*string) = ""
	*destinations[18].(*string) = ""
	*destinations[19].(*bool) = false
	*destinations[20].(*[]byte) = []byte(`{}`)
	*destinations[21].(*[]byte) = []byte(`{}`)
	*destinations[22].(*string) = ""
	*destinations[23].(*string) = ""
	*destinations[24].(*string) = ""
	*destinations[25].(*string) = ""
	now := time.Now().UTC()
	*destinations[26].(*time.Time) = now
	*destinations[29].(*time.Time) = now
	return nil
}

func TestScanTaskNormalizesInitialOptionalFields(t *testing.T) {
	task, err := scanTask(nullableTaskScanner{})
	if err != nil {
		t.Fatal(err)
	}
	if task.Failure != nil || task.Summary != "" || task.ResourceUsage == nil || len(task.ResourceUsage) != 0 {
		t.Fatalf("optional task fields were not normalized: %#v", task)
	}
}

func TestTaskTransitionsAreForwardOnlyAfterRuntimeStart(t *testing.T) {
	for _, transition := range [][2]string{
		{TaskPreparing, TaskRunning},
		{TaskRunning, TaskUploading},
		{TaskRunning, TaskFailed},
		{TaskUploading, TaskTimedOut},
	} {
		if !validTaskTransition(transition[0], transition[1]) {
			t.Fatalf("valid transition rejected: %s -> %s", transition[0], transition[1])
		}
	}
	for _, transition := range [][2]string{
		{TaskRunning, TaskQueued},
		{TaskUploading, TaskRunning},
		{TaskPreparing, TaskSucceeded},
	} {
		if validTaskTransition(transition[0], transition[1]) {
			t.Fatalf("invalid transition accepted: %s -> %s", transition[0], transition[1])
		}
	}
}
