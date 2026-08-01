package mcpbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const sessionHeader = "X-Mmdash-Session-Id"

type TokenSource interface {
	AccessToken(context.Context, bool) (string, error)
}

type HTTPTransport interface {
	Do(*http.Request) (*http.Response, error)
}

type Transport interface {
	Run(context.Context) error
}

type Bridge struct {
	CurrentProjectID string
	Endpoint         string
	HTTP             HTTPTransport
	Stderr           io.Writer
	Stdin            io.Reader
	Stdout           io.Writer
	Tokens           TokenSource
	mu               sync.Mutex
	sessionID        string
}

var _ Transport = (*Bridge)(nil)

func (bridge *Bridge) Run(ctx context.Context) error {
	if bridge.HTTP == nil {
		bridge.HTTP = &http.Client{Timeout: 2 * time.Minute}
	}
	scanner := bufio.NewScanner(bridge.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	writer := bufio.NewWriter(bridge.Stdout)
	defer writer.Flush()
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		prepared, requestID, method, err := bridge.prepare(line)
		if err != nil {
			if requestID != nil {
				if writeErr := writeRPCError(writer, requestID, -32602, err.Error(), "PROJECT_CONTEXT_REQUIRED"); writeErr != nil {
					return writeErr
				}
			}
			continue
		}
		responses, err := bridge.forward(ctx, prepared, method)
		if err != nil {
			if requestID != nil {
				if writeErr := writeRPCError(writer, requestID, -32001, err.Error(), "REMOTE_MCP_UNAVAILABLE"); writeErr != nil {
					return writeErr
				}
			} else {
				_, _ = fmt.Fprintf(bridge.Stderr, "mcp bridge: %v\n", err)
			}
			continue
		}
		for _, response := range responses {
			if _, err := writer.Write(append(response, '\n')); err != nil {
				return err
			}
			if err := writer.Flush(); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (bridge *Bridge) prepare(line []byte) ([]byte, interface{}, string, error) {
	var message map[string]interface{}
	if err := json.Unmarshal(line, &message); err != nil {
		return nil, nil, "", err
	}
	requestID, hasID := message["id"]
	if !hasID {
		requestID = nil
	}
	method, _ := message["method"].(string)
	if method != "tools/call" {
		return line, requestID, method, nil
	}
	params, _ := message["params"].(map[string]interface{})
	name, _ := params["name"].(string)
	if name == "project.list" {
		return line, requestID, method, nil
	}
	arguments, _ := params["arguments"].(map[string]interface{})
	if arguments == nil {
		arguments = map[string]interface{}{}
		params["arguments"] = arguments
	}
	projectID, _ := arguments["project_id"].(string)
	if projectID == "" {
		if bridge.CurrentProjectID == "" {
			return nil, requestID, method, errors.New("select a project with 'mmdash project use <project_id>'")
		}
		arguments["project_id"] = bridge.CurrentProjectID
	} else if bridge.CurrentProjectID != "" && projectID != bridge.CurrentProjectID {
		return nil, requestID, method, errors.New("the requested project does not match the explicitly selected CLI project")
	}
	prepared, err := json.Marshal(message)
	return prepared, requestID, method, err
}

func (bridge *Bridge) forward(ctx context.Context, payload []byte, method string) ([][]byte, error) {
	attempts := 1
	if retryableMethod(method) {
		attempts = 3
	}
	forceRefresh := false
	for attempt := 0; attempt < attempts; attempt++ {
		token, err := bridge.Tokens.AccessToken(ctx, forceRefresh)
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, bridge.Endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		bridge.mu.Lock()
		if bridge.sessionID != "" {
			request.Header.Set(sessionHeader, bridge.sessionID)
		}
		bridge.mu.Unlock()
		response, err := bridge.HTTP.Do(request)
		if err != nil {
			if attempt+1 < attempts {
				time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
				continue
			}
			return nil, errors.New("cannot reach the remote MCP Gateway")
		}
		if sessionID := response.Header.Get(sessionHeader); sessionID != "" {
			bridge.mu.Lock()
			bridge.sessionID = sessionID
			bridge.mu.Unlock()
		}
		if response.StatusCode == http.StatusUnauthorized && !forceRefresh {
			_ = response.Body.Close()
			forceRefresh = true
			attempts++
			continue
		}
		if response.StatusCode >= 500 && attempt+1 < attempts {
			_ = response.Body.Close()
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
			continue
		}
		return decodeMCPResponse(response)
	}
	return nil, errors.New("remote MCP request failed")
}

func decodeMCPResponse(response *http.Response) ([][]byte, error) {
	defer response.Body.Close()
	if response.StatusCode == http.StatusAccepted || response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var value struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value)
		if value.Message == "" {
			value.Message = fmt.Sprintf("remote MCP returned HTTP %d", response.StatusCode)
		}
		return nil, errors.New(value.Message)
	}
	contentType := response.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return decodeSSE(response.Body)
	}
	value, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	value = bytes.TrimSpace(value)
	if len(value) == 0 {
		return nil, nil
	}
	return [][]byte{value}, nil
}

func decodeSSE(reader io.Reader) ([][]byte, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, 16<<20))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	responses := [][]byte{}
	var data strings.Builder
	flush := func() {
		if data.Len() == 0 {
			return
		}
		responses = append(responses, []byte(strings.TrimSuffix(data.String(), "\n")))
		data.Reset()
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			data.WriteByte('\n')
		}
	}
	flush()
	return responses, scanner.Err()
}

func retryableMethod(method string) bool {
	switch method {
	case "initialize", "ping", "server/discover", "tools/list", "resources/list", "resources/templates/list", "prompts/list":
		return true
	default:
		return false
	}
}

func writeRPCError(writer *bufio.Writer, id interface{}, code int, message string, stableCode string) error {
	return json.NewEncoder(writer).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": map[string]interface{}{"code": code, "message": message, "data": map[string]string{"code": stableCode}}})
}
