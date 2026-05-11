import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  LocalAgentError,
  connectLocalAgent,
  disconnectLocalAgent,
  getLocalAgentState,
  sendAction,
} from "./local_agent";

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  static OPEN = 1;
  static CLOSED = 3;

  readyState = 0;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  sentMessages: string[] = [];
  url: string;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(payload: string) {
    this.sentMessages.push(payload);
  }

  close() {
    this.readyState = 3;
    this.onclose?.();
  }

  open() {
    this.readyState = 1;
    this.onopen?.();
  }

  emitMessage(payload: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(payload) });
  }
}

describe("local_agent client", () => {
  const flushPromises = async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  };

  beforeEach(() => {
    MockWebSocket.instances = [];
    vi.stubGlobal("WebSocket", MockWebSocket as unknown as typeof WebSocket);
    Object.assign(globalThis.WebSocket, {
      OPEN: MockWebSocket.OPEN,
      CLOSED: MockWebSocket.CLOSED,
    });
  });

  afterEach(() => {
    disconnectLocalAgent();
    vi.unstubAllGlobals();
  });

  it("sends requests and resolves structured responses", async () => {
    const connectPromise = connectLocalAgent();
    const socket = MockWebSocket.instances[0];
    socket.open();
    await connectPromise;

    const requestPromise = sendAction("shell.run", { command: "echo hello" });
    await flushPromises();
    const infoRequest = JSON.parse(socket.sentMessages[0]);
    socket.emitMessage({
      request_id: infoRequest.request_id,
      action: "agent.info",
      ok: true,
      protocol_version: 2,
      data: { protocol_version: 2 },
    });
    await flushPromises();

    const shellRequest = JSON.parse(socket.sentMessages[1]);
    socket.emitMessage({
      request_id: shellRequest.request_id,
      action: "shell.run",
      ok: true,
      protocol_version: 2,
      data: { returncode: 0, stdout: "hello\n", stderr: "" },
    });

    await expect(requestPromise).resolves.toEqual({
      returncode: 0,
      stdout: "hello\n",
      stderr: "",
    });
    expect(getLocalAgentState()).toBe("ready");
  });

  it("decodes structured errors", async () => {
    const connectPromise = connectLocalAgent();
    const socket = MockWebSocket.instances[0];
    socket.open();
    await connectPromise;

    const requestPromise = sendAction("shell.run", { command: "echo hello" });
    await flushPromises();
    const infoRequest = JSON.parse(socket.sentMessages[0]);
    socket.emitMessage({
      request_id: infoRequest.request_id,
      action: "agent.info",
      ok: true,
      protocol_version: 2,
      data: { protocol_version: 2 },
    });
    await flushPromises();

    const shellRequest = JSON.parse(socket.sentMessages[1]);
    socket.emitMessage({
      request_id: shellRequest.request_id,
      action: "shell.run",
      ok: false,
      protocol_version: 2,
      error: {
        code: "timeout",
        message: "command timed out after 5s",
        retryable: true,
        details: { timeout_seconds: 5 },
      },
    });

    await expect(requestPromise).rejects.toMatchObject({
      code: "timeout",
      message: "command timed out after 5s",
      retryable: true,
    });
  });
});
