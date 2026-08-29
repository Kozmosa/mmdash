//go:build windows

package localprocess

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetTickCount64 = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount64")

// systemUptimeMillis mirrors the kernel32 GetTickCount64 API, which the pinned
// golang.org/x/sys version does not export.
func systemUptimeMillis() uint64 {
	value, _, _ := procGetTickCount64.Call()
	return uint64(value)
}

// jobObject owns the per-task Windows Job Object. Every process of a task is
// assigned to one Job Object, so termination covers the complete descendant
// tree and CPU rate, committed memory and active process limits are enforced
// by the kernel instead of being advisory values.
type jobObject struct {
	handle windows.Handle
}

func newTaskJob(limits contractLimits) (*jobObject, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil || handle == 0 {
		return nil, fmt.Errorf("create task job object: %w", err)
	}
	job := &jobObject{handle: handle}
	if limits.CPUMillis > 0 {
		// CPUMillis is CPU millis per second (1000 == one core). CpuRate is a
		// hard share of the total processor capacity in hundredths of a
		// percent, so the mapping needs the machine's core count.
		cpuRate := uint32(uint64(limits.CPUMillis) * 10 / uint64(runtime.NumCPU()))
		if cpuRate < 1 {
			cpuRate = 1
		}
		if cpuRate > 10000 {
			cpuRate = 10000
		}
		info := jobObjectCpuRateControlInformation{
			CpuRate:      cpuRate,
			ControlFlags: jobObjectCpuRateControlEnable | jobObjectCpuRateControlHardLimit,
		}
		if _, err := windows.SetInformationJobObject(handle,
			windows.JobObjectCpuRateControlInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			job.close()
			return nil, fmt.Errorf("apply task CPU limit: %w", err)
		}
	}
	extended := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			ActiveProcessLimit: uint32(limits.PIDs),
			LimitFlags: windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS |
				windows.JOB_OBJECT_LIMIT_JOB_MEMORY |
				// If the supervisor dies, the kernel terminates the complete
				// task tree instead of leaving it running without timeout
				// enforcement.
				windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
		JobMemoryLimit: uintptr(limits.MemoryBytes),
	}
	if _, err := windows.SetInformationJobObject(handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&extended)), uint32(unsafe.Sizeof(extended))); err != nil {
		job.close()
		return nil, fmt.Errorf("apply task job limits: %w", err)
	}
	return job, nil
}

// jobObjectCpuRateControlInformation mirrors JOBOBJECT_CPU_RATE_CONTROL_INFORMATION,
// which the pinned golang.org/x/sys version does not define. The OS reads
// ControlFlags from the first word, so the field order must not change.
type jobObjectCpuRateControlInformation struct {
	ControlFlags uint32
	CpuRate      uint32
}

const (
	jobObjectCpuRateControlEnable    = 0x1
	jobObjectCpuRateControlHardLimit = 0x4
)

func startTaskProcess(argv []string, env []string, dir string, job *jobObject,
	stdoutLog, stderrLog *os.File) (*exec.Cmd, error) {
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = dir
	command.Env = env
	command.Stdout = stdoutLog
	command.Stderr = stderrLog
	// CREATE_SUSPENDED lets the runner assign the process to its Job Object
	// before any code of the task (and therefore any descendant) can run.
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if err := command.Start(); err != nil {
		return nil, err
	}
	if err := job.assign(command); err != nil {
		_ = job.terminate()
		_ = command.Process.Kill()
		command.Wait()
		job.close()
		return nil, fmt.Errorf("assign task to job object: %w", err)
	}
	if err := resumeMainThread(command.Process.Pid); err != nil {
		_ = job.terminate()
		command.Wait()
		job.close()
		return nil, err
	}
	return command, nil
}

func (job *jobObject) assign(command *exec.Cmd) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.AssignProcessToJobObject(job.handle, handle)
}

func (job *jobObject) terminate() error {
	if job == nil || job.handle == 0 {
		return nil
	}
	return windows.TerminateJobObject(job.handle, 1)
}

// killTree is a cross-process fallback. The runner always owns the task Job
// Object, so in-runner termination goes through terminate(); outside the
// runner only the recorded root process can be terminated.
func killTree(pid int) error {
	terminateProcess(pid)
	return nil
}

func (job *jobObject) close() {
	if job != nil && job.handle != 0 {
		_ = windows.CloseHandle(job.handle)
		job.handle = 0
	}
}

// resumeMainThread resumes the CREATE_SUSPENDED initial thread of the task.
func resumeMainThread(pid int) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshot task threads: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return fmt.Errorf("enumerate task threads: %w", err)
	}
	resumed := false
	for {
		if entry.OwnerProcessID == uint32(pid) {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				continue
			}
			_, _ = windows.ResumeThread(thread)
			_ = windows.CloseHandle(thread)
			resumed = true
			break
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			break
		}
	}
	if !resumed {
		return errors.New("task initial thread was not found")
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}

// reapProcess is a no-op on Windows: there are no zombies and the kernel
// reclaims the process once its handles are closed.
func reapProcess(pid int) {}

func venvPython(python, venv string) string {
	return filepath.Join(venv, "Scripts", "python.exe")
}

func terminateProcess(pid int) {
	if pid <= 0 {
		return
	}
	if handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid)); err == nil {
		_ = windows.TerminateProcess(handle, 1)
		_ = windows.CloseHandle(handle)
	}
}

// bootID identifies the current OS session. A stored boot ID from a previous
// host session marks every recorded task as unrecoverable (HOST_RESTARTED).
func bootID() string {
	boot := time.Now().Unix() - int64(systemUptimeMillis()/1000)
	return "windows-boot-" + strconv.FormatInt(boot, 10)
}

func sameBootID(recorded, current string) bool {
	recordedBoot, recordedOK := strings.CutPrefix(recorded, "windows-boot-")
	currentBoot, currentOK := strings.CutPrefix(current, "windows-boot-")
	if !recordedOK || !currentOK {
		return recorded == current
	}
	recordedValue, recordErr := strconv.ParseInt(recordedBoot, 10, 64)
	currentValue, currentErr := strconv.ParseInt(currentBoot, 10, 64)
	if recordErr != nil || currentErr != nil {
		return recorded == current
	}
	// Allow small clock adjustments within one session; a reboot moves the
	// boot instant by at least the downtime.
	difference := recordedValue - currentValue
	if difference < 0 {
		difference = -difference
	}
	return difference <= 120
}

// enforceAvailability verifies this host can actually enforce the requested
// frozen limits. Windows enforces CPU/memory/PIDs through Job Objects, so only
// the Job Object API itself needs to work.
func enforceAvailability(limits contractLimits) error {
	job, err := newTaskJob(limits)
	if err != nil {
		return fmt.Errorf("Job Object enforcement is unavailable: %w", err)
	}
	job.close()
	return nil
}

// platformRunnerFeatures reports the enforcement capabilities probed on this
// host for the Runtime descriptor advertised to Core.
func platformRunnerFeatures() []string {
	return []string{
		"enforce:timeout:hard",
		"enforce:process-tree:job-object",
		"enforce:cpu:job-object",
		"enforce:memory:job-object",
		"enforce:pids:job-object",
		"enforce:disk:output-collection",
		"network:unsupported",
	}
}

// cgroupV2Root returns an empty string: cgroups do not exist on Windows and
// enforcement comes from the per-task Job Object.
func cgroupV2Root() string { return "" }

func removeCgroup(_ string) {}

// applyCgroupLimits is unreachable on Windows: the Runtime only sets a
// cgroup path when the platform reports cgroup v2 availability.
func applyCgroupLimits(_ string, _ int, _ contractLimits) error { return nil }

// platformRunnerProcessAttributes puts the runner into its own process group
// so console events aimed at the Gateway do not reach the supervised task.
func platformRunnerProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}
