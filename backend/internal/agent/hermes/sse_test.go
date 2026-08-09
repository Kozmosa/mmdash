package hermes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

func TestStreamChatNormalizesNamedHermesEventsAndForwardsLastEventID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assertRuntimeAuth(t, request)
		if request.Method != http.MethodPost || request.URL.Path != "/api/sessions/session-main/chat/stream" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Last-Event-ID") != "prior:7" {
			t.Fatalf("Last-Event-ID not forwarded: %#v", request.Header)
		}
		body := decodeRequestMap(t, request)
		if body["message"] != "hello" || body["instructions"] != "project prompt" {
			t.Fatalf("unexpected stream body: %#v", body)
		}
		response.Header().Set("Content-Type", "text/event-stream")
		flusher := response.(http.Flusher)
		frames := []string{
			": keepalive\n\n",
			"event: run.started\ndata: {\"run_id\":\"run-1\",\"session_id\":\"session-main\",\"seq\":1,\"ts\":1754000000}\n\n",
			"event: message.started\ndata: {\"run_id\":\"run-1\",\"session_id\":\"session-main\",\"message\":{\"id\":\"message-1\",\"role\":\"assistant\"}}\n\n",
			"event: assistant.delta\ndata: {\"run_id\":\"run-1\",\"message_id\":\"message-1\",\"delta\":\"hel\"}\n\n",
			"event: tool.started\ndata: {\"run_id\":\"run-1\",\"message_id\":\"message-1\",\"tool_name\":\"data.read\",\"args\":{\"token\":\"do-not-leak\"},\"preview\":\"private\"}\n\n",
			"event: tool.completed\ndata: {\"run_id\":\"run-1\",\"tool_name\":\"data.read\",\"preview\":\"sensitive result\"}\n\n",
			"event: tool.progress\ndata: {\"run_id\":\"run-1\",\"tool_name\":\"_thinking\",\"delta\":\"private reasoning\"}\n\n",
			"event: assistant.completed\ndata: {\"run_id\":\"run-1\",\"message_id\":\"message-1\",\"content\":\"hello\"}\n\n",
			"event: run.completed\ndata: {\"run_id\":\"run-1\",\"messages\":[{\"tool_result\":\"do-not-leak\"}],\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}\n\n",
			"event: done\ndata: {}\n\n",
		}
		for _, frame := range frames {
			_, _ = response.Write([]byte(frame))
			flusher.Flush()
		}
	}))
	defer server.Close()
	adapter := runtimeAdapterForServer(t, server.URL, "")

	var events []agent.Event
	err := adapter.StreamChat(context.Background(), "session-main", agent.ChatRequest{Message: "hello", Instructions: "project prompt"}, agent.StreamOptions{LastEventID: "prior:7"}, func(_ context.Context, event agent.Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTypes := []agent.EventType{
		agent.EventHeartbeat, agent.EventRunStarted, agent.EventMessageStarted, agent.EventMessageDelta,
		agent.EventToolStarted, agent.EventToolCompleted, agent.EventToolProgress,
		agent.EventMessageCompleted, agent.EventRunCompleted, agent.EventDone,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("unexpected events: %#v", events)
	}
	for index, want := range wantTypes {
		if events[index].Type != want || events[index].ID != fmt.Sprintf("session-main:%d", index+1) || events[index].Sequence != int64(index+1) {
			t.Fatalf("event %d: %#v", index, events[index])
		}
		serialized := fmt.Sprintf("%#v", events[index])
		for _, secret := range []string{"do-not-leak", "private reasoning", "sensitive result"} {
			if strings.Contains(serialized, secret) {
				t.Fatalf("unsafe event leaked %q: %s", secret, serialized)
			}
		}
	}
	if events[3].Text != "hel" || events[7].Text != "hello" || events[8].Usage.TotalTokens != 5 {
		t.Fatalf("assistant output not normalized: %#v", events)
	}
	if events[4].Tool.RemoteID == "" || events[4].Tool.RemoteID != events[5].Tool.RemoteID {
		t.Fatalf("tool lifecycle was not correlated: %#v %#v", events[4], events[5])
	}
}

func TestStreamRunNormalizesUnnamedEventsAndStopsOnHandlerError(t *testing.T) {
	stop := errors.New("stop callback")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/runs/run-1/events" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Last-Event-ID") != "run-1:3" {
			t.Fatalf("missing Last-Event-ID: %#v", request.Header)
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = response.Write([]byte("data: {\"event\":\"reasoning.available\",\"run_id\":\"run-1\",\"text\":\"hidden reasoning\"}\n\n"))
		_, _ = response.Write([]byte("data: {\"event\":\"approval.request\",\"run_id\":\"run-1\",\"command\":\"rm secret\",\"choices\":[\"once\",\"session\",\"always\",\"deny\"]}\n\n"))
		_, _ = response.Write([]byte("data: {\"event\":\"message.delta\",\"run_id\":\"run-1\",\"delta\":\"ignored after callback\"}\n\n"))
	}))
	defer server.Close()
	adapter := runtimeAdapterForServer(t, server.URL, "")
	var events []agent.Event
	err := adapter.StreamRun(context.Background(), "run-1", agent.StreamOptions{LastEventID: "run-1:3"}, func(_ context.Context, event agent.Event) error {
		events = append(events, event)
		if len(events) == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("expected callback error, got %v", err)
	}
	if len(events) != 2 || events[0].Type != agent.EventToolProgress || events[0].Status != "reasoning_available" || events[0].Text != "" || events[1].Type != agent.EventApprovalRequested || len(events[1].Approval.Choices) != 4 {
		t.Fatalf("unexpected normalized events: %#v", events)
	}
	if events[1].Approval.RemoteID == "" {
		t.Fatal("approval event lacks a stable remote ID")
	}
	if strings.Contains(fmt.Sprintf("%#v", events), "hidden reasoning") || strings.Contains(fmt.Sprintf("%#v", events), "rm secret") {
		t.Fatal("reasoning or approval command leaked")
	}
}

func TestStreamHonorsContextCancellationWithoutOrdinaryRequestTimeout(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		flusher := response.(http.Flusher)
		_, _ = response.Write([]byte("data: {\"event\":\"message.delta\",\"run_id\":\"run-1\",\"delta\":\"first\"}\n\n"))
		flusher.Flush()
		once.Do(func() { close(started) })
		<-request.Context().Done()
	}))
	defer server.Close()
	policy := loopbackPolicy(t, server.URL)
	policy.RequestTimeout = 10 * time.Millisecond
	adapter, err := New(Config{InstanceID: "instance", RuntimeURL: server.URL, APIKey: "runtime-secret", RuntimePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- adapter.StreamRun(ctx, "run-1", agent.StreamOptions{}, func(_ context.Context, _ agent.Event) error {
			cancel()
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not stop after cancellation")
	}
}

func TestSSERejectsOversizedEvent(t *testing.T) {
	data := "data: {\"event\":\"message.delta\",\"delta\":\"" + strings.Repeat("x", 1024) + "\"}\n\n"
	err := consumeSSE(context.Background(), strings.NewReader(data), "run-1", 128, func(context.Context, agent.Event) error { return nil })
	var adapterError *agent.AdapterError
	if !errors.As(err, &adapterError) || adapterError.Code != agent.ErrorProtocol {
		t.Fatalf("expected event size error, got %#v", err)
	}
}

func TestApprovalResponseCorrelatesToGeneratedRequestID(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"event":"approval.request","run_id":"run-1","choices":["once","deny"]}`,
		"",
		`data: {"event":"approval.responded","run_id":"run-1","choice":"once","resolved":1}`,
		"",
	}, "\n")
	var events []agent.Event
	if err := consumeSSE(context.Background(), strings.NewReader(stream), "run-1", 4096, func(_ context.Context, event agent.Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Approval == nil || events[1].Approval == nil ||
		events[0].Approval.RemoteID == "" || events[0].Approval.RemoteID != events[1].Approval.RemoteID {
		t.Fatalf("approval lifecycle was not correlated: %#v", events)
	}
}

func TestApprovalResponseRemovesExplicitIDFromCorrelationQueue(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"event":"approval.request","approval_id":"approval-1","run_id":"run-1"}`,
		"",
		`data: {"event":"approval.request","approval_id":"approval-2","run_id":"run-1"}`,
		"",
		`data: {"event":"approval.responded","approval_id":"approval-2","run_id":"run-1","choice":"once","resolved":1}`,
		"",
		`data: {"event":"approval.responded","run_id":"run-1","choice":"deny","resolved":1}`,
		"",
	}, "\n")
	var events []agent.Event
	if err := consumeSSE(context.Background(), strings.NewReader(stream), "run-1", 4096, func(_ context.Context, event agent.Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[2].Approval == nil || events[3].Approval == nil ||
		events[2].Approval.RemoteID != "approval-2" ||
		events[3].Approval.RemoteID != "approval-1" {
		t.Fatalf("explicit response left a stale correlation entry: %#v", events)
	}
}

func TestUncorrelatedApprovalResponseKeepsRemoteIDEmpty(t *testing.T) {
	stream := "data: {\"event\":\"approval.responded\",\"run_id\":\"run-1\",\"choice\":\"once\",\"resolved\":1}\n\n"
	var actual agent.Event
	if err := consumeSSE(context.Background(), strings.NewReader(stream), "run-1", 4096, func(_ context.Context, event agent.Event) error {
		actual = event
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if actual.Approval == nil || actual.Approval.RemoteID != "" {
		t.Fatalf("unidentified upstream response invented an approval ID: %#v", actual)
	}
}
