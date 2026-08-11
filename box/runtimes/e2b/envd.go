package e2b

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"connectrpc.com/connect"
)

const (
	processStartProcedure   = "/process.Process/Start"
	processSignalProcedure  = "/process.Process/SendSignal"
	filesystemListProcedure = "/filesystem.Filesystem/ListDir"
	filesystemMakeProcedure = "/filesystem.Filesystem/MakeDir"
	keepalivePingHeader     = "Keepalive-Ping-Interval"
	keepalivePingSeconds    = "50"
	signalTerminate         = "SIGNAL_SIGTERM"
	signalKill              = "SIGNAL_SIGKILL"
)

var exitStatusPattern = regexp.MustCompile(`(?:^|\s)exit status (-?[0-9]+)(?:$|\s)`)

type providerJSONCodec struct{}

func (providerJSONCodec) Name() string                           { return "json" }
func (providerJSONCodec) Marshal(value any) ([]byte, error)      { return json.Marshal(value) }
func (providerJSONCodec) Unmarshal(data []byte, value any) error { return json.Unmarshal(data, value) }

type processConfig struct {
	Cmd  string            `json:"cmd"`
	Args []string          `json:"args,omitempty"`
	Envs map[string]string `json:"envs,omitempty"`
	Cwd  *string           `json:"cwd,omitempty"`
}

type processStartRequest struct {
	Process processConfig `json:"process"`
	Stdin   bool          `json:"stdin"`
}

type processStartResponse struct {
	Event *processEvent `json:"event,omitempty"`
}

type processEvent struct {
	Start     *processStartEvent `json:"start,omitempty"`
	Data      *processDataEvent  `json:"data,omitempty"`
	End       *processEndEvent   `json:"end,omitempty"`
	Keepalive *struct{}          `json:"keepalive,omitempty"`
}

type processStartEvent struct {
	PID int `json:"pid"`
}
type processDataEvent struct {
	Stdout []byte `json:"stdout,omitempty"`
	Stderr []byte `json:"stderr,omitempty"`
	PTY    []byte `json:"pty,omitempty"`
}
type processEndEvent struct {
	ExitCode int     `json:"exitCode"`
	Exited   bool    `json:"exited"`
	Status   string  `json:"status"`
	Error    *string `json:"error,omitempty"`
}

type processSelector struct {
	PID int `json:"pid"`
}
type processSignalRequest struct {
	Process processSelector `json:"process"`
	Signal  string          `json:"signal"`
}
type processSignalResponse struct{}

type filesystemMakeDirRequest struct {
	Path string `json:"path"`
}
type filesystemMakeDirResponse struct{}
type filesystemListDirRequest struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
}
type filesystemListDirResponse struct {
	Entries []filesystemEntry `json:"entries"`
}
type filesystemEntry struct {
	Name          string        `json:"name"`
	Type          string        `json:"type"`
	Path          string        `json:"path"`
	Size          flexibleInt64 `json:"size"`
	SymlinkTarget *string       `json:"symlinkTarget,omitempty"`
}

type flexibleInt64 int64

func (value *flexibleInt64) UnmarshalJSON(data []byte) error {
	var number int64
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return err
		}
		number = parsed
	} else if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*value = flexibleInt64(number)
	return nil
}

func (client *ProviderClient) runProcess(ctx context.Context, session *executionSession, user string, command []string, environment map[string]string, stdout, stderr io.Writer, onStart func(int)) (int, error) {
	if len(command) == 0 || command[0] == "" {
		return 0, errors.New("E2B process command is empty")
	}
	rpc := connect.NewClient[processStartRequest, processStartResponse](client.httpClient, session.sandboxURL+processStartProcedure, connect.WithCodec(providerJSONCodec{}))
	cwd := remoteWorkspace
	request := connect.NewRequest(&processStartRequest{Process: processConfig{Cmd: command[0], Args: append([]string(nil), command[1:]...), Envs: cloneStrings(environment), Cwd: &cwd}, Stdin: false})
	client.envdHeaders(request.Header(), session, user)
	request.Header().Set(keepalivePingHeader, keepalivePingSeconds)
	stream, err := rpc.CallServerStream(ctx, request)
	if err != nil {
		return 0, fmt.Errorf("start E2B process: %w", err)
	}
	started, ended := false, false
	exitCode := 0
	for stream.Receive() {
		message := stream.Msg()
		if message == nil || message.Event == nil {
			continue
		}
		event := message.Event
		switch {
		case event.Start != nil:
			if event.Start.PID <= 0 || started {
				return 0, errors.New("E2B process returned an invalid start event")
			}
			started = true
			if onStart != nil {
				onStart(event.Start.PID)
			}
		case event.Data != nil:
			if err := writeOutput(stdout, event.Data.Stdout); err != nil {
				return 0, fmt.Errorf("write E2B stdout: %w", err)
			}
			if err := writeOutput(stderr, event.Data.Stderr); err != nil {
				return 0, fmt.Errorf("write E2B stderr: %w", err)
			}
		case event.End != nil:
			var parseErr error
			exitCode, parseErr = processExitCode(*event.End)
			if parseErr != nil {
				return 0, parseErr
			}
			ended = true
		}
	}
	if err := stream.Err(); err != nil {
		return 0, fmt.Errorf("stream E2B process: %w", err)
	}
	if !started || !ended {
		return 0, errors.New("E2B process stream ended without complete lifecycle events")
	}
	return exitCode, nil
}

func processExitCode(event processEndEvent) (int, error) {
	if matches := exitStatusPattern.FindStringSubmatch(event.Status); len(matches) == 2 {
		value, err := strconv.Atoi(matches[1])
		if err == nil {
			return value, nil
		}
	}
	if event.Exited || event.ExitCode != 0 {
		return event.ExitCode, nil
	}
	if event.Error != nil && *event.Error != "" {
		return 0, fmt.Errorf("E2B process ended without an exit status: %s", *event.Error)
	}
	return 0, errors.New("E2B process ended without an exit status")
}

func (client *ProviderClient) sendSignal(ctx context.Context, session *executionSession, signal string) error {
	if session == nil || session.pid <= 0 {
		return nil
	}
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	rpc := connect.NewClient[processSignalRequest, processSignalResponse](client.httpClient, session.sandboxURL+processSignalProcedure, connect.WithCodec(providerJSONCodec{}))
	request := connect.NewRequest(&processSignalRequest{Process: processSelector{PID: session.pid}, Signal: signal})
	client.envdHeaders(request.Header(), session, client.adminUser)
	if _, err := rpc.CallUnary(requestCtx, request); err != nil {
		return fmt.Errorf("signal E2B process: %w", err)
	}
	return nil
}

func (client *ProviderClient) makeDir(ctx context.Context, session *executionSession, remotePath, user string) error {
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	rpc := connect.NewClient[filesystemMakeDirRequest, filesystemMakeDirResponse](client.httpClient, session.sandboxURL+filesystemMakeProcedure, connect.WithCodec(providerJSONCodec{}))
	request := connect.NewRequest(&filesystemMakeDirRequest{Path: remotePath})
	client.envdHeaders(request.Header(), session, user)
	if _, err := rpc.CallUnary(requestCtx, request); err != nil && connect.CodeOf(err) != connect.CodeAlreadyExists {
		return fmt.Errorf("create E2B directory %s: %w", remotePath, err)
	}
	return nil
}

func (client *ProviderClient) listDir(ctx context.Context, session *executionSession, remotePath string) ([]filesystemEntry, error) {
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	rpc := connect.NewClient[filesystemListDirRequest, filesystemListDirResponse](client.httpClient, session.sandboxURL+filesystemListProcedure, connect.WithCodec(providerJSONCodec{}))
	request := connect.NewRequest(&filesystemListDirRequest{Path: remotePath, Depth: 1})
	client.envdHeaders(request.Header(), session, client.adminUser)
	response, err := rpc.CallUnary(requestCtx, request)
	if err != nil {
		return nil, fmt.Errorf("list E2B directory %s: %w", remotePath, err)
	}
	return response.Msg.Entries, nil
}

func (client *ProviderClient) uploadFile(ctx context.Context, session *executionSession, localPath, remotePath string) error {
	file, err := openRegularFile(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	endpoint := client.fileURL(session, remotePath, client.adminUser)
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, file)
	if err != nil {
		return err
	}
	request.ContentLength = info.Size()
	request.Header.Set("Content-Type", "application/octet-stream")
	client.envdHeaders(request.Header, session, "")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("upload E2B file %s: %w", remotePath, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return client.responseError("upload E2B file", response)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	return nil
}

func (client *ProviderClient) downloadFile(ctx context.Context, session *executionSession, remotePath string, destination io.Writer, limit int64) (int64, error) {
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, client.fileURL(session, remotePath, client.adminUser), nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	client.envdHeaders(request.Header, session, "")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("download E2B file %s: %w", remotePath, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, client.responseError("download E2B file", response)
	}
	if response.ContentLength > limit && response.ContentLength >= 0 {
		return 0, errors.New("E2B output exceeds the frozen disk limit")
	}
	written, err := io.Copy(destination, io.LimitReader(response.Body, limit+1))
	if err != nil {
		return written, err
	}
	if written > limit {
		return written, errors.New("E2B output exceeds the frozen disk limit")
	}
	return written, nil
}

func (client *ProviderClient) fileURL(session *executionSession, remotePath, user string) string {
	values := url.Values{}
	values.Set("path", remotePath)
	if user != "" {
		values.Set("username", user)
	}
	return session.sandboxURL + "/files?" + values.Encode()
}

func (client *ProviderClient) envdHeaders(headers http.Header, session *executionSession, user string) {
	headers.Set("E2b-Sandbox-Id", session.sandboxID)
	headers.Set("E2b-Sandbox-Port", strconv.Itoa(defaultEnvdPort))
	headers.Set("X-Access-Token", session.envdAccessToken)
	headers.Set("User-Agent", client.userAgent)
	if user != "" {
		headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":")))
	}
}

func versionAtLeast(actual, minimum string) bool {
	parse := func(value string) [3]int {
		var result [3]int
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
		for index := 0; index < len(result) && index < len(parts); index++ {
			digits := parts[index]
			for offset, character := range digits {
				if character < '0' || character > '9' {
					digits = digits[:offset]
					break
				}
			}
			result[index], _ = strconv.Atoi(digits)
		}
		return result
	}
	left, right := parse(actual), parse(minimum)
	for index := range left {
		if left[index] != right[index] {
			return left[index] > right[index]
		}
	}
	return true
}
