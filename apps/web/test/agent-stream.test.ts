import { describe, expect, it, vi } from "vitest";

import {
  parseAgentEventStream,
  streamAgentRun,
} from "@/features/agent/agent-api";

describe("Agent SSE parser", () => {
  it("parses fragmented CRLF frames, comments, and multiline data", async () => {
    const chunks = [
      ": heartbeat\r",
      "\n\r\nid: event-1\r\ndata: {\"event\":\"message.delta\",\r\n",
      "data: \"event_id\":\"event-1\",\"sequence\":1,\"occurred_at\":\"2026-08-06T00:00:00Z\",\"run_id\":\"run-1\",\"session_id\":\"session-1\",\"delta\":\"Hi\"}\r\n\r\n",
      "id: event-2\ndata: {\"event\":\"done\",\"event_id\":\"event-2\",\"sequence\":2,\"occurred_at\":\"2026-08-06T00:00:01Z\",\"run_id\":\"run-1\",\"session_id\":\"session-1\"}\n\n",
    ];
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) {
          controller.enqueue(new TextEncoder().encode(chunk));
        }
        controller.close();
      },
    });

    const frames = [];
    for await (const frame of parseAgentEventStream(stream)) {
      frames.push(frame);
    }

    expect(frames).toHaveLength(2);
    expect(frames[0]).toMatchObject({
      event: { delta: "Hi", event: "message.delta" },
      id: "event-1",
    });
    expect(frames[1]).toMatchObject({
      event: { event: "done" },
      id: "event-2",
    });
  });

  it("sends resume headers and emits normalized events incrementally", async () => {
    const onEvent = vi.fn();
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        'id: event-3\ndata: {"event":"tool.progress","event_id":"event-3","sequence":3,"occurred_at":"2026-08-06T00:00:02Z","run_id":"run-1","session_id":"session-1","tool_call":{"tool_call_id":"tool-1","name":"data.read","status":"running"}}\n\n',
        { headers: { "content-type": "text/event-stream" } },
      ),
    );

    const lastId = await streamAgentRun(
      {
        instanceId: "instance-1",
        lastEventId: "event-2",
        projectId: "project-1",
        runId: "run-1",
        sessionId: "session-1",
      },
      onEvent,
      { fetchImplementation },
    );

    expect(lastId).toBe("event-3");
    expect(onEvent).toHaveBeenCalledWith(
      expect.objectContaining({ event: "tool.progress" }),
    );
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(
      "/api/projects/project-1/agent-instances/instance-1/sessions/session-1/runs/run-1/events",
    );
    expect(options?.credentials).toBe("include");
    expect(new Headers(options?.headers).get("last-event-id")).toBe("event-2");
  });

  it("rejects an event that is not runtime-neutral AgentStreamEvent", async () => {
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          new TextEncoder().encode('data: {"provider":"hermes-raw"}\n\n'),
        );
        controller.close();
      },
    });

    const read = async () => {
      const frames = [];
      for await (const frame of parseAgentEventStream(stream)) {
        frames.push(frame);
      }
      return frames;
    };
    await expect(read()).rejects.toThrow("invalid event");
  });
});
