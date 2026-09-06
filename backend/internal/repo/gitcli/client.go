package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mmdash/mmdash/backend/internal/repo/egress"
)

const (
	maintenanceGitEmail = "repo@mmdash.local"
	maintenanceGitName  = "mmdash"
)

// Credentials are injected only into one Git subprocess through AskPass.
type Credentials struct {
	Proxy    egress.Config
	Token    string
	Username string
}

// Command is one reviewed Git command template invocation.
type Command struct {
	Args        []string
	Credentials *Credentials
	Directory   string
	Environment map[string]string
	Operation   string
	Sensitive   []string
	Timeout     time.Duration
}

// Result contains bounded, redacted process output.
type Result struct {
	Duration time.Duration
	Stderr   []byte
	Stdout   []byte
}

// StreamResult reports a bounded stdout stream without retaining file bytes.
type StreamResult struct {
	Bytes    int64
	Duration time.Duration
}

// CommandError intentionally omits argv, paths, credentials, and provider output.
type CommandError struct {
	Cause     error
	Code      error
	ExitCode  int
	Operation string
}

func (err *CommandError) Error() string {
	return fmt.Sprintf("%s: %v", err.Operation, err.Code)
}

func (err *CommandError) Unwrap() []error {
	result := []error{err.Code}
	if err.Cause != nil {
		result = append(result, err.Cause)
	}
	return result
}

// Client runs Git with bounded concurrency, time, environment, and output.
type Client struct {
	AskPassPath    string
	CommandTimeout time.Duration
	GitPath        string
	MaxOutputBytes int
	environment    func(string) (string, bool)
	semaphore      chan struct{}
}

// NewClient creates a Git command client.
func NewClient(
	gitPath string,
	askPassPath string,
	commandTimeout time.Duration,
	maxConcurrent int,
	maxOutputBytes int,
) (*Client, error) {
	if strings.TrimSpace(gitPath) == "" ||
		commandTimeout <= 0 ||
		maxConcurrent < 1 ||
		maxOutputBytes < 1 {
		return nil, ErrCommandFailed
	}
	return &Client{
		AskPassPath: askPassPath, CommandTimeout: commandTimeout,
		GitPath: gitPath, MaxOutputBytes: maxOutputBytes,
		environment: os.LookupEnv,
		semaphore:   make(chan struct{}, maxConcurrent),
	}, nil
}

// Run executes one Git command without a shell.
func (client *Client) Run(ctx context.Context, request Command) (Result, error) {
	if strings.TrimSpace(request.Operation) == "" ||
		request.Directory == "" ||
		len(request.Args) == 0 {
		return Result{}, &CommandError{
			Code: ErrCommandFailed, ExitCode: -1, Operation: "invalid",
		}
	}
	select {
	case client.semaphore <- struct{}{}:
		defer func() { <-client.semaphore }()
	case <-ctx.Done():
		return Result{}, &CommandError{
			Code: ErrTimeout, ExitCode: -1, Operation: request.Operation,
		}
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = client.CommandTimeout
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout := &limitedBuffer{limit: client.MaxOutputBytes}
	stderr := &limitedBuffer{limit: client.MaxOutputBytes}
	command := exec.CommandContext(commandContext, client.GitPath, request.Args...)
	command.Dir = request.Directory
	command.Env = client.commandEnvironment(request)
	command.Stdout = stdout
	command.Stderr = stderr
	startedAt := time.Now()
	runErr := command.Run()
	duration := time.Since(startedAt)
	sensitive := commandSensitive(request)
	result := Result{
		Duration: duration,
		Stderr:   redact(stderr.Bytes(), sensitive),
		Stdout:   redact(stdout.Bytes(), sensitive),
	}
	if runErr == nil && !stdout.exceeded && !stderr.exceeded {
		return result, nil
	}
	code := classifyCommandFailure(
		commandContext.Err(), stdout.exceeded, stderr.exceeded, result.Stderr,
	)
	exitCode := -1
	var exitError *exec.ExitError
	if errors.As(runErr, &exitError) {
		exitCode = exitError.ExitCode()
	}
	return result, &CommandError{
		Cause: runErr, Code: code, ExitCode: exitCode, Operation: request.Operation,
	}
}

// RunStream executes one reviewed Git command and streams stdout into a
// caller-owned sink. It is reserved for immutable Git object bytes that may be
// larger than the normal diagnostic-output limit.
func (client *Client) RunStream(
	ctx context.Context,
	request Command,
	output io.Writer,
	maxBytes int64,
) (StreamResult, error) {
	if strings.TrimSpace(request.Operation) == "" || request.Directory == "" ||
		len(request.Args) == 0 || output == nil || maxBytes < 0 || maxBytes > 10<<30 {
		return StreamResult{}, &CommandError{
			Code: ErrCommandFailed, ExitCode: -1, Operation: "invalid",
		}
	}
	select {
	case client.semaphore <- struct{}{}:
		defer func() { <-client.semaphore }()
	case <-ctx.Done():
		return StreamResult{}, &CommandError{
			Code: ErrTimeout, ExitCode: -1, Operation: request.Operation,
		}
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = client.CommandTimeout
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout := &limitedStreamWriter{limit: maxBytes, writer: output}
	stderr := &limitedBuffer{limit: client.MaxOutputBytes}
	command := exec.CommandContext(commandContext, client.GitPath, request.Args...)
	command.Dir = request.Directory
	command.Env = client.commandEnvironment(request)
	command.Stdout = stdout
	command.Stderr = stderr
	startedAt := time.Now()
	runErr := command.Run()
	result := StreamResult{Bytes: stdout.written, Duration: time.Since(startedAt)}
	if runErr == nil && !stdout.exceeded && !stderr.exceeded {
		return result, nil
	}
	redactedStderr := redact(stderr.Bytes(), commandSensitive(request))
	code := classifyCommandFailure(
		commandContext.Err(), stdout.exceeded, stderr.exceeded, redactedStderr,
	)
	exitCode := -1
	var exitError *exec.ExitError
	if errors.As(runErr, &exitError) {
		exitCode = exitError.ExitCode()
	}
	return result, &CommandError{
		Cause: runErr, Code: code, ExitCode: exitCode, Operation: request.Operation,
	}
}

func (client *Client) commandEnvironment(request Command) []string {
	environment := []string{}
	for _, key := range []string{"PATH", "Path", "SYSTEMROOT", "SystemRoot", "TEMP", "TMP"} {
		if value, ok := client.environment(key); ok && value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	environment = append(environment,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_COUNT=0",
		"LC_ALL=C",
		"LANG=C",
	)
	for _, value := range []struct {
		key      string
		fallback string
	}{
		{key: "GIT_AUTHOR_EMAIL", fallback: maintenanceGitEmail},
		{key: "GIT_AUTHOR_NAME", fallback: maintenanceGitName},
		{key: "GIT_COMMITTER_EMAIL", fallback: maintenanceGitEmail},
		{key: "GIT_COMMITTER_NAME", fallback: maintenanceGitName},
	} {
		resolved := value.fallback
		if override, ok := request.Environment[value.key]; ok {
			resolved = override
		}
		environment = append(environment, value.key+"="+resolved)
	}
	for key, value := range request.Environment {
		if allowedGitEnvironment[key] && !maintenanceIdentityKey(key) {
			environment = append(environment, key+"="+value)
		}
	}
	if request.Credentials != nil {
		for key, value := range request.Credentials.Proxy.GitEnvironment() {
			environment = append(environment, key+"="+value)
		}
		username := request.Credentials.Username
		if username == "" {
			username = "x-access-token"
		}
		environment = append(environment,
			"GIT_ASKPASS="+client.AskPassPath,
			"MMDASH_GIT_USERNAME="+username,
			"MMDASH_GIT_TOKEN="+request.Credentials.Token,
		)
	}
	return environment
}

func maintenanceIdentityKey(key string) bool {
	return key == "GIT_AUTHOR_EMAIL" || key == "GIT_AUTHOR_NAME" ||
		key == "GIT_COMMITTER_EMAIL" || key == "GIT_COMMITTER_NAME"
}

var allowedGitEnvironment = map[string]bool{
	"GIT_AUTHOR_DATE":     true,
	"GIT_AUTHOR_EMAIL":    true,
	"GIT_AUTHOR_NAME":     true,
	"GIT_COMMITTER_DATE":  true,
	"GIT_COMMITTER_EMAIL": true,
	"GIT_COMMITTER_NAME":  true,
}

func looksLikeAuthenticationFailure(stderr []byte) bool {
	message := strings.ToLower(string(stderr))
	return strings.Contains(message, "authentication failed") ||
		strings.Contains(message, "could not read username") ||
		strings.Contains(message, "invalid username or password") ||
		strings.Contains(message, "permission denied") ||
		strings.Contains(message, "requested url returned error: 401") ||
		strings.Contains(message, "requested url returned error: 403")
}

func classifyCommandFailure(
	contextErr error,
	stdoutExceeded bool,
	stderrExceeded bool,
	stderr []byte,
) error {
	switch {
	case contextErr != nil:
		return ErrTimeout
	case stdoutExceeded || stderrExceeded:
		return ErrOutputLimit
	case looksLikeAuthenticationFailure(stderr):
		return ErrAuthentication
	case containsAny(stderr,
		"repository not found",
		"requested url returned error: 404",
	):
		return ErrRemoteNotFound
	case containsAny(stderr,
		"couldn't find remote ref",
		"remote ref does not exist",
	):
		return ErrBranchNotFound
	case containsAny(stderr,
		"requested url returned error: 429",
		"requested url returned error: 500",
		"requested url returned error: 502",
		"requested url returned error: 503",
		"requested url returned error: 504",
		"remote: internal server error",
		"service unavailable",
	):
		return ErrProviderUnavailable
	case containsAny(stderr,
		"could not resolve host",
		"could not resolve hostname",
		"could not resolve proxy",
		"failed to connect",
		"connection refused",
		"connection reset",
		"connection timed out",
		"network is unreachable",
		"no route to host",
		"proxy connect aborted",
		"proxy connect failed",
		"ssl certificate problem",
		"server certificate verification failed",
		"tls connect error",
		"gnutls_handshake",
	):
		return ErrNetworkUnavailable
	default:
		return ErrCommandFailed
	}
}

func containsAny(contents []byte, fragments ...string) bool {
	message := strings.ToLower(string(contents))
	for _, fragment := range fragments {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func commandSensitive(request Command) []string {
	sensitive := append([]string(nil), request.Sensitive...)
	if request.Credentials != nil {
		sensitive = append(sensitive, request.Credentials.Token)
		sensitive = append(
			sensitive,
			request.Credentials.Proxy.SensitiveValues()...,
		)
	}
	return sensitive
}

func redact(contents []byte, sensitive []string) []byte {
	redacted := string(contents)
	for _, secret := range sensitive {
		if secret != "" {
			redacted = strings.ReplaceAll(redacted, secret, "[REDACTED]")
		}
	}
	return []byte(redacted)
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	exceeded bool
	limit    int
	mutex    sync.Mutex
}

type limitedStreamWriter struct {
	exceeded bool
	limit    int64
	writer   io.Writer
	written  int64
}

func (writer *limitedStreamWriter) Write(contents []byte) (int, error) {
	remaining := writer.limit - writer.written
	if remaining <= 0 && len(contents) > 0 {
		writer.exceeded = true
		return 0, ErrOutputLimit
	}
	if int64(len(contents)) > remaining {
		contents = contents[:remaining]
		writer.exceeded = true
	}
	written, err := writer.writer.Write(contents)
	writer.written += int64(written)
	if err != nil {
		return written, err
	}
	if writer.exceeded {
		return written, ErrOutputLimit
	}
	if written != len(contents) {
		return written, io.ErrShortWrite
	}
	return written, nil
}

func (writer *limitedBuffer) Write(contents []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	remaining := writer.limit - writer.buffer.Len()
	if remaining <= 0 {
		writer.exceeded = true
		return 0, ErrOutputLimit
	}
	if len(contents) > remaining {
		_, _ = writer.buffer.Write(contents[:remaining])
		writer.exceeded = true
		return remaining, ErrOutputLimit
	}
	return writer.buffer.Write(contents)
}

func (writer *limitedBuffer) Bytes() []byte {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return append([]byte(nil), writer.buffer.Bytes()...)
}
