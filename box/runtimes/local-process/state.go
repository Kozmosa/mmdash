package localprocess

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The runner persists one durable record per task under the Box runner state
// directory. It is the only cross-restart evidence the Gateway needs: a
// restart reattaches to a live runner by task ID, a reboot terminates the
// recorded task with the stable HOST_RESTARTED code.

const (
	taskStateSchemaVersion = 1

	taskStateStarting = "starting"
	taskStateRunning  = "running"
	taskStateExited   = "exited"
	taskStateCanceled = "canceled"
	taskStateTimedOut = "timed_out"
	taskStateFailed   = "failed"
)

type taskRecord struct {
	SchemaVersion  int        `json:"schema_version"`
	TaskID         string     `json:"task_id"`
	ExecutionEpoch string     `json:"execution_epoch,omitempty"`
	BootID         string     `json:"boot_id"`
	RunnerPID      int        `json:"runner_pid"`
	TaskPID        int        `json:"task_pid"`
	State          string     `json:"state"`
	ExitCode       *int       `json:"exit_code,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	StartedAt      time.Time  `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

func loadTaskRecord(path string) (taskRecord, bool, error) {
	var record taskRecord
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return record, false, nil
	}
	if err != nil {
		return record, false, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, false, fmt.Errorf("decode local-process task record: %w", err)
	}
	if record.SchemaVersion != taskStateSchemaVersion {
		return record, false, fmt.Errorf("unsupported local-process task record schema %d", record.SchemaVersion)
	}
	return record, true, nil
}

// loadTaskRecordRetry tolerates the transient sharing violations of the
// atomic rename publish on Windows: a reader can hit "being used by another
// process" for the instant the supervisor replaces the record. Only genuine
// read errors are retried; a missing record returns immediately.
func loadTaskRecordRetry(path string, attempts int, interval time.Duration) (taskRecord, bool, error) {
	var record taskRecord
	var exists bool
	var err error
	for index := 0; index < attempts; index++ {
		record, exists, err = loadTaskRecord(path)
		if err == nil {
			return record, exists, nil
		}
		if index+1 < attempts {
			time.Sleep(interval)
		}
	}
	return record, exists, err
}

func saveTaskRecord(path string, record taskRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	// The Gateway polls this record while the runner replaces it, so the
	// rename can hit a transient Windows sharing violation; losing it would
	// drop the terminal record and lose the task. Retry briefly.
	var renameErr error
	for attempt := 0; attempt < 10; attempt++ {
		if renameErr = os.Rename(temporary, path); renameErr == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = os.Remove(temporary)
	return renameErr
}

// spoolOffsets tracks how much of the runner-owned task output the Gateway has
// already forwarded into its durable log spool. After a Gateway restart the
// recorded offsets replay exactly the output the new Gateway has not seen.
type spoolOffsets struct {
	SchemaVersion int   `json:"schema_version"`
	StdoutBytes   int64 `json:"stdout_bytes"`
	StderrBytes   int64 `json:"stderr_bytes"`
}

func loadSpoolOffsets(path string) spoolOffsets {
	offsets := spoolOffsets{SchemaVersion: 1}
	data, err := os.ReadFile(path)
	if err != nil {
		return offsets
	}
	if err := json.Unmarshal(data, &offsets); err != nil {
		return spoolOffsets{SchemaVersion: 1}
	}
	return offsets
}

func saveSpoolOffsets(path string, offsets spoolOffsets) error {
	data, err := json.Marshal(offsets)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func taskTerminal(state string) bool {
	switch state {
	case taskStateExited, taskStateCanceled, taskStateTimedOut, taskStateFailed:
		return true
	}
	return false
}
