//go:build !windows

package localprocess

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// The Unix implementation supervises the task through its own process group:
// the runner starts in a fresh group and the task in another one, so killing
// the negative process group ID terminates the complete descendant tree even
// when descendants detached from their parent. CPU, memory and PID limits are
// enforced with cgroup v2, which must be delegated and writable.

type jobObject struct{}

// newTaskJob is a no-op on Unix: hard limits come from the per-task cgroup
// v2 subtree applied after the task process exists.
func newTaskJob(_ contractLimits) (*jobObject, error) {
	return &jobObject{}, nil
}

func (job *jobObject) terminate() error { return nil }
func (job *jobObject) close()           {}

func startTaskProcess(argv []string, env []string, dir string, _ *jobObject,
	stdoutLog, stderrLog *os.File) (*exec.Cmd, error) {
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = dir
	command.Env = env
	command.Stdout = stdoutLog
	command.Stderr = stderrLog
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return nil, err
	}
	return command, nil
}

// killTree terminates the task process group. It is only valid while the task
// still runs in the group the runner created for it.
func killTree(pid int) error {
	if pid <= 0 {
		return errors.New("task process ID is not recorded")
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		// The root may already be gone while descendants survive, so fall
		// back to the individual process before reporting failure. ESRCH for
		// the group does not prove the root process is gone: a recovered task
		// may predate process-group supervision or a test may start it directly.
		if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			return fmt.Errorf("terminate task process group %d: %w", pid, err)
		}
	}
	// SIGKILL delivery is asynchronous. Wait briefly until the root is gone or
	// a zombie so callers never report RUNNER_LOST while the task can still run.
	for attempt := 0; attempt < 50; attempt++ {
		if processStopped(pid) {
			break
		}
		sleepMilli(10)
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if processStopped(pid) {
		return false
	}
	// The detached supervisor is never waited for directly, so an exited
	// runner lingers as a zombie that still answers kill(pid, 0). Reap it
	// opportunistically and report it as gone. A runner started by a previous
	// Gateway process is not our child: Wait4 then fails with ECHILD and the
	// process counts as alive.
	var status syscall.WaitStatus
	if waited, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil); err == nil && waited == pid {
		return false
	}
	return true
}

func processStopped(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return true
	}
	// An orphaned task can remain as a zombie until the host's PID 1 reaps it.
	// It no longer executes or owns a process tree, so treat it as terminated
	// even though kill(pid, 0) still succeeds. /proc is Linux-specific; other
	// Unix kernels simply fall through to the existing wait/liveness check.
	if status, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		closingParen := strings.LastIndexByte(string(status), ')')
		if closingParen >= 0 && len(status) > closingParen+2 && status[closingParen+2] == 'Z' {
			return true
		}
	}
	return false
}

// reapProcess best-effort reaps an exited child so detached runners never
// accumulate as zombies.
func reapProcess(pid int) {
	if pid <= 0 {
		return
	}
	var status syscall.WaitStatus
	_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
}

func venvPython(python, venv string) string {
	return filepath.Join(venv, "bin", "python")
}

func terminateProcess(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// bootID identifies the current OS session. A stored boot ID from a previous
// host session marks every recorded task as unrecoverable (HOST_RESTARTED).
func bootID() string {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(data)) == "" {
		// Non-Linux Unix kernels fall back to the boot-relative stat of a
		// stable system directory, which changes across reboots.
		info, statErr := os.Stat("/")
		if statErr != nil {
			return "unknown-boot"
		}
		return "unix-boot-" + strconv.FormatInt(info.ModTime().Unix(), 10)
	}
	return strings.TrimSpace(string(data))
}

func sameBootID(recorded, current string) bool {
	return recorded == current
}

// cgroupV2Root returns the writable cgroup v2 mount used for per-task limits.
func cgroupV2Root() string {
	if cgroupRootOverride != "" {
		return cgroupRootOverride
	}
	if value := strings.TrimSpace(os.Getenv("MMDASH_BOX_CGROUP_ROOT")); value != "" {
		return value
	}
	return "/sys/fs/cgroup"
}

// enforceAvailability verifies this host can actually enforce the requested
// frozen limits with cgroup v2 before any task is scheduled or started.
func enforceAvailability(limits contractLimits) error {
	root := cgroupV2Root()
	controllers, err := os.ReadFile(root + "/cgroup.controllers")
	if err != nil {
		return fmt.Errorf("cgroup v2 enforcement is unavailable: %w", err)
	}
	available := map[string]bool{}
	for _, name := range strings.Fields(string(controllers)) {
		available[name] = true
	}
	for _, required := range []string{"memory", "cpu", "pids"} {
		if !available[required] {
			return fmt.Errorf("cgroup v2 enforcement is unavailable: controller %q is not delegated", required)
		}
	}
	probe := fmt.Sprintf("mmdash-probe-%d", os.Getpid())
	path := root + "/" + probe
	if err := os.Mkdir(path, 0o755); err != nil {
		return fmt.Errorf("cgroup v2 enforcement is unavailable: %w", err)
	}
	defer func() { _ = os.Remove(path) }()
	if limits.MemoryBytes > 0 {
		if err := os.WriteFile(path+"/memory.max", []byte(strconv.FormatInt(limits.MemoryBytes, 10)), 0o644); err != nil {
			return fmt.Errorf("cgroup v2 enforcement is unavailable: %w", err)
		}
	}
	if limits.CPUMillis > 0 {
		if err := os.WriteFile(path+"/cpu.max", []byte(cgroupCpuMax(limits.CPUMillis)), 0o644); err != nil {
			return fmt.Errorf("cgroup v2 enforcement is unavailable: %w", err)
		}
	}
	if limits.PIDs > 0 {
		if err := os.WriteFile(path+"/pids.max", []byte(strconv.Itoa(limits.PIDs)), 0o644); err != nil {
			return fmt.Errorf("cgroup v2 enforcement is unavailable: %w", err)
		}
	}
	return nil
}

func cgroupCpuMax(cpuMillis int64) string {
	// cpu.max is "<quota> <period>" in microseconds; 1000 millis of one core
	// per second is quota 100000 with the standard 100000 microsecond period.
	period := int64(100000)
	quota := cpuMillis * period / 1000
	if quota < 1000 {
		quota = 1000
	}
	return strconv.FormatInt(quota, 10) + " " + strconv.FormatInt(period, 10)
}

// applyCgroupLimits moves the task process into a dedicated cgroup with the
// frozen hard limits. Any failure is fatal for the runner: an unenforced
// limit must never silently degrade into an advisory value.
func applyCgroupLimits(path string, pid int, limits contractLimits) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create task cgroup: %w", err)
	}
	if limits.MemoryBytes > 0 {
		if err := os.WriteFile(path+"/memory.max", []byte(strconv.FormatInt(limits.MemoryBytes, 10)), 0o644); err != nil {
			return fmt.Errorf("apply task memory limit: %w", err)
		}
		if err := os.WriteFile(path+"/memory.swap.max", []byte("0"), 0o644); err != nil {
			return fmt.Errorf("apply task swap limit: %w", err)
		}
	}
	if limits.CPUMillis > 0 {
		if err := os.WriteFile(path+"/cpu.max", []byte(cgroupCpuMax(limits.CPUMillis)), 0o644); err != nil {
			return fmt.Errorf("apply task CPU limit: %w", err)
		}
	}
	if limits.PIDs > 0 {
		if err := os.WriteFile(path+"/pids.max", []byte(strconv.Itoa(limits.PIDs)), 0o644); err != nil {
			return fmt.Errorf("apply task PID limit: %w", err)
		}
	}
	if err := os.WriteFile(path+"/cgroup.procs", []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return fmt.Errorf("move task into cgroup: %w", err)
	}
	return nil
}

func removeCgroup(path string) {
	if path == "" {
		return
	}
	// A cgroup is removed once its last process exits; retry briefly because
	// the kernel may need a moment after the task terminated.
	for attempt := 0; attempt < 10; attempt++ {
		if err := os.Remove(path); err == nil {
			return
		}
		sleepMilli(100)
	}
}

// platformRunnerFeatures reports the enforcement capabilities probed on this
// host for the Runtime descriptor advertised to Core.
func platformRunnerFeatures() []string {
	return []string{
		"enforce:timeout:hard",
		"enforce:process-tree:process-group",
		"enforce:cpu:cgroup-v2",
		"enforce:memory:cgroup-v2",
		"enforce:pids:cgroup-v2",
		"enforce:disk:output-collection",
		"network:unsupported",
	}
}

// platformRunnerProcessAttributes detaches the runner from the Gateway
// process group so terminal signals aimed at the Gateway never reach the
// supervised task through the runner.
func platformRunnerProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
