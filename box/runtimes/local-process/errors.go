package localprocess

import (
	"errors"
	"time"
)

// Stable failure codes surfaced through the Box failure contract. The Gateway
// maps them onto the frozen Experiment failure vocabulary.
const (
	// ErrCodeHostRestarted terminates a recorded task after the Box host
	// rebooted; the same execution is never replayed automatically.
	ErrCodeHostRestarted = "HOST_RESTARTED"
	// ErrCodeLimitsNotEnforceable rejects a frozen RunSpec whose network or
	// resource policy this host cannot enforce. Degradation is never silent.
	ErrCodeLimitsNotEnforceable = "LIMITS_NOT_ENFORCEABLE"
	// ErrCodeRunnerLost means the supervisor died while the task state
	// claimed it was still running on the same host session.
	ErrCodeRunnerLost = "RUNNER_LOST"
	// ErrCodeRunnerFailed covers runner-side infrastructure failures.
	ErrCodeRunnerFailed = "RUNNER_FAILED"
)

// runtimeError carries a stable code through the Gateway failure mapping.
type runtimeError struct {
	code string
	err  error
}

func (err runtimeError) Error() string     { return err.err.Error() }
func (err runtimeError) Unwrap() error     { return err.err }
func (err runtimeError) ErrorCode() string { return err.code }

func codedError(code, message string) error {
	return runtimeError{code: code, err: errors.New(message)}
}

// contractLimits is the subset of the frozen resource limits the platform
// supervision enforces directly.
type contractLimits struct {
	CPUMillis   int64
	MemoryBytes int64
	PIDs        int
}

func sleepMilli(milli int) {
	<-time.After(time.Duration(milli) * time.Millisecond)
}
