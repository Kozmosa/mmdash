package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Credentials are injected only into one Git subprocess through AskPass.
type Credentials struct {
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

// CommandError intentionally omits argv, paths, credentials, and provider output.
type CommandError struct {
	Code      error
	ExitCode  int
	Operation string
}

func (err *CommandError) Error() string {
	return fmt.Sprintf("%s: %v", err.Operation, err.Code)
}

func (err *CommandError) Unwrap() error {
	return err.Code
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
	sensitive := append([]string(nil), request.Sensitive...)
	if request.Credentials != nil {
		sensitive = append(sensitive, request.Credentials.Token)
	}
	result := Result{
		Duration: duration,
		Stderr:   redact(stderr.Bytes(), sensitive),
		Stdout:   redact(stdout.Bytes(), sensitive),
	}
	if runErr == nil && !stdout.exceeded && !stderr.exceeded {
		return result, nil
	}
	code := ErrCommandFailed
	switch {
	case commandContext.Err() != nil:
		code = ErrTimeout
	case stdout.exceeded || stderr.exceeded:
		code = ErrOutputLimit
	case looksLikeAuthenticationFailure(result.Stderr):
		code = ErrAuthentication
	}
	exitCode := -1
	var exitError *exec.ExitError
	if errors.As(runErr, &exitError) {
		exitCode = exitError.ExitCode()
	}
	return result, &CommandError{
		Code: code, ExitCode: exitCode, Operation: request.Operation,
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
	for key, value := range request.Environment {
		if allowedGitEnvironment[key] {
			environment = append(environment, key+"="+value)
		}
	}
	if request.Credentials != nil {
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
		strings.Contains(message, "permission denied")
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
