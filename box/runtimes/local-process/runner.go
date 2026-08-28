package localprocess

// The runner subcommand is the small per-task supervisor required for
// bare-metal execution. It is re-executed from the same Box binary as
// `mmdash-box task-runner`, starts the fixed entrypoint without a shell and
// enforces timeout, cancellation and the frozen resource limits over the
// complete process tree. Its durable state file lets a restarted Gateway
// reconnect while a host reboot terminates the task with HOST_RESTARTED.

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// jobSpec is the complete frozen description of one supervised execution. The
// Gateway writes it before the runner starts; the runner never receives or
// inherits the Gateway environment.
type jobSpec struct {
	SchemaVersion  int    `json:"schema_version"`
	TaskID         string `json:"task_id"`
	ExecutionEpoch string `json:"execution_epoch"`
	ExperimentID   string `json:"experiment_id"`
	Workspace      string `json:"workspace"`
	OutputDir      string `json:"output_dir"`
	ParametersFile string `json:"parameters_file"`
	Command        []string
	Environment    []string `json:"environment"`
	TimeoutSecond  int      `json:"timeout_seconds"`
	CPUMillis      int64    `json:"cpu_millis"`
	MemoryBytes    int64    `json:"memory_bytes"`
	PIDs           int      `json:"pids"`
	CgroupPath     string   `json:"cgroup_path,omitempty"`
}

func (spec jobSpec) limits() contractLimits {
	return contractLimits{CPUMillis: spec.CPUMillis, MemoryBytes: spec.MemoryBytes, PIDs: spec.PIDs}
}

// RunTaskRunner executes the `task-runner` subcommand. It returns after the
// task reached a terminal state and the durable record is complete.
func RunTaskRunner(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("task-runner", flag.ContinueOnError)
	stateDir := flags.String("state-dir", "", "durable runner state directory")
	taskID := flags.String("task-id", "", "supervised task ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" || *taskID == "" {
		return errors.New("task-runner requires --state-dir and --task-id")
	}
	taskDir := filepath.Join(*stateDir, *taskID)
	recordPath := filepath.Join(taskDir, "state.json")
	record, exists, err := loadTaskRecord(recordPath)
	if err != nil {
		return err
	}
	if exists && taskTerminal(record.State) {
		return nil
	}
	job, err := loadJobSpec(filepath.Join(taskDir, "job.json"))
	if err != nil {
		return err
	}
	if record.TaskID != "" && record.TaskID != *taskID {
		return errors.New("task record does not match the requested task")
	}

	// A cancel request that arrived before the runner started is still a
	// cancellation; the execution is never launched in that case.
	if _, err := os.Stat(cancelSentinelPath(taskDir)); err == nil {
		return persistTerminal(recordPath, record, *taskID, taskStateCanceled, nil)
	}

	startedAt := time.Now().UTC()
	if err := saveTaskRecord(recordPath, taskRecord{
		SchemaVersion: taskStateSchemaVersion, TaskID: *taskID,
		ExecutionEpoch: job.ExecutionEpoch, BootID: bootID(),
		RunnerPID: os.Getpid(), State: taskStateStarting, StartedAt: startedAt,
	}); err != nil {
		return fmt.Errorf("persist task record: %w", err)
	}

	// The runner is detached from the Gateway process group, so a terminal
	// signal aimed at the Gateway never reaches the task through this process.
	signal.Ignore(syscall.SIGHUP)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	process, taskJob, launchErr := launchTask(&job)
	if launchErr != nil {
		return persistTerminal(recordPath, record, *taskID, taskStateFailed, nil)
	}
	defer taskJob.close()

	record.TaskPID = process.Process.Pid
	record.State = taskStateRunning
	if err := saveTaskRecord(recordPath, record); err != nil {
		_ = taskJob.terminate()
		killTree(process.Process.Pid)
		_, _ = process.Process.Wait()
		return fmt.Errorf("persist running task record: %w", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- process.Wait() }()

	timeout := time.NewTimer(time.Duration(job.TimeoutSecond) * time.Second)
	defer timeout.Stop()
	cancelPoll := time.NewTicker(200 * time.Millisecond)
	defer cancelPoll.Stop()
	for {
		select {
		case waitErr := <-waitDone:
			exitCode := 0
			if waitErr != nil {
				var exitErr *exec.ExitError
				if errors.As(waitErr, &exitErr) {
					exitCode = exitErr.ExitCode()
				} else {
					exitCode = -1
				}
			} else if state := process.ProcessState; state != nil {
				exitCode = state.ExitCode()
			}
			return persistTerminal(recordPath, record, *taskID, taskStateExited, &exitCode)
		case <-timeout.C:
			_ = taskJob.terminate()
			_ = killTree(process.Process.Pid)
			<-waitDone
			return persistTerminal(recordPath, record, *taskID, taskStateTimedOut, nil)
		case <-cancelPoll.C:
			if _, err := os.Stat(cancelSentinelPath(taskDir)); err == nil {
				_ = taskJob.terminate()
				_ = killTree(process.Process.Pid)
				<-waitDone
				return persistTerminal(recordPath, record, *taskID, taskStateCanceled, nil)
			}
		case received := <-signals:
			_ = received
			_ = taskJob.terminate()
			_ = killTree(process.Process.Pid)
			<-waitDone
			return persistTerminal(recordPath, record, *taskID, taskStateCanceled, nil)
		}
	}
}

// launchTask starts the task suspended where the platform requires it and
// applies the frozen hard limits before the first instruction of the task can
// run. A limit that cannot be applied aborts the launch: an unenforced limit
// must never silently degrade into an advisory value.
func launchTask(job *jobSpec) (*exec.Cmd, *jobObject, error) {
	taskJob, err := newTaskJob(job.limits())
	if err != nil {
		return nil, nil, err
	}
	process, err := startTaskProcess(job.Command, job.Environment, job.Workspace, taskJob)
	if err != nil {
		taskJob.close()
		return nil, nil, err
	}
	if job.CgroupPath != "" {
		if err := applyCgroupLimits(job.CgroupPath, process.Process.Pid, job.limits()); err != nil {
			_ = taskJob.terminate()
			_ = process.Process.Kill()
			_, _ = process.Process.Wait()
			taskJob.close()
			return nil, nil, err
		}
	}
	return process, taskJob, nil
}

func loadJobSpec(path string) (jobSpec, error) {
	var job jobSpec
	data, err := os.ReadFile(path)
	if err != nil {
		return job, err
	}
	if err := json.Unmarshal(data, &job); err != nil {
		return job, fmt.Errorf("decode local-process job spec: %w", err)
	}
	if job.SchemaVersion != 1 || job.TaskID == "" || len(job.Command) == 0 {
		return job, errors.New("invalid local-process job spec")
	}
	return job, nil
}

func persistTerminal(recordPath string, record taskRecord, taskID, state string, exitCode *int) error {
	now := time.Now().UTC()
	record.SchemaVersion = taskStateSchemaVersion
	record.TaskID = taskID
	record.State = state
	record.ExitCode = exitCode
	record.FinishedAt = &now
	return saveTaskRecord(recordPath, record)
}

func cancelSentinelPath(taskDir string) string { return filepath.Join(taskDir, "cancel") }
