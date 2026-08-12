import { ApiError, apiClient, type ApiClient } from "@/lib/api-client";

import type {
  AgentApprovalChoice,
  AgentChecksResult,
  AgentInstance,
  AgentInstanceInput,
  AgentInstanceProvisioningResult,
  AgentInstanceUpdate,
  AgentMessage,
  AgentProjectAccessVerificationResult,
  AgentPrompt,
  AgentReasoningEffort,
  AgentRun,
  AgentRunLaunch,
  AgentSession,
  AgentSessionType,
  AgentStreamEvent,
  AgentTokenRotationResult,
} from "./types";

export const reviewedAgentTools = [
  "project.get",
  "data.list",
  "data.read",
  "context.promote",
  "progress.get",
  "progress.recalculate",
  "artifact.upload",
  "artifact.read",
  "experiment.create",
  "experiment.run",
  "experiment.status",
  "result.get",
] as const;

export const agentApi = {
  abortToken(projectId: string, instanceId: string, tokenId: string) {
    return apiClient.request(
      `${instancePath(projectId, instanceId)}/tokens/${encodeURIComponent(tokenId)}/abort`,
      { method: "POST" },
    );
  },
  approveRun(
    projectId: string,
    instanceId: string,
    sessionId: string,
    runId: string,
    approvalId: string,
    choice: AgentApprovalChoice,
  ) {
    return apiClient.request<AgentRun>(
      `${runPath(projectId, instanceId, sessionId, runId)}/approvals/${encodeURIComponent(approvalId)}`,
      { body: { choice }, method: "POST" },
    );
  },
  checkInstance(projectId: string, instanceId: string) {
    return apiClient.request<AgentChecksResult>(
      `${instancePath(projectId, instanceId)}/checks`,
      { body: { scope: "all" }, method: "POST" },
    );
  },
  continueSession(projectId: string, instanceId: string, sessionId: string) {
    return apiClient.request<AgentSession>(
      `${sessionPath(projectId, instanceId, sessionId)}/continue`,
      { method: "POST" },
    );
  },
  createInstance(projectId: string, input: AgentInstanceInput) {
    return apiClient.request<AgentInstanceProvisioningResult>(
      `/projects/${encodeURIComponent(projectId)}/agent-instances`,
      { body: input, method: "POST" },
    );
  },
  createSession(
    projectId: string,
    instanceId: string,
    input: { default?: boolean; session_type: AgentSessionType; title: string },
  ) {
    return apiClient.request<AgentSession>(
      `${instancePath(projectId, instanceId)}/sessions`,
      { body: input, method: "POST" },
    );
  },
  disableInstance(projectId: string, instanceId: string) {
    return apiClient.request<void>(instancePath(projectId, instanceId), {
      method: "DELETE",
    });
  },
  endSession(projectId: string, instanceId: string, sessionId: string) {
    return apiClient.request<AgentSession>(
      `${sessionPath(projectId, instanceId, sessionId)}/end`,
      { body: { reason: "Ended by the user" }, method: "POST" },
    );
  },
  forkSession(
    projectId: string,
    instanceId: string,
    sessionId: string,
    title?: string,
  ) {
    return apiClient.request<AgentSession>(
      `${sessionPath(projectId, instanceId, sessionId)}/fork`,
      { body: title ? { title } : {}, method: "POST" },
    );
  },
  getPrompt(projectId: string, instanceId: string) {
    return apiClient.request<AgentPrompt>(
      `${instancePath(projectId, instanceId)}/prompt`,
    );
  },
  getRun(
    projectId: string,
    instanceId: string,
    sessionId: string,
    runId: string,
  ) {
    return apiClient.request<AgentRun>(
      runPath(projectId, instanceId, sessionId, runId),
      { cache: "no-store" },
    );
  },
  listInstances(projectId: string) {
    return apiClient.request<{ items: AgentInstance[] }>(
      `/projects/${encodeURIComponent(projectId)}/agent-instances`,
    );
  },
  listMessages(projectId: string, instanceId: string, sessionId: string) {
    return apiClient.request<{ items: AgentMessage[] }>(
      `${sessionPath(projectId, instanceId, sessionId)}/messages`,
      { cache: "no-store" },
    );
  },
  listSessions(projectId: string, instanceId: string) {
    return apiClient.request<{ items: AgentSession[] }>(
      `${instancePath(projectId, instanceId)}/sessions`,
    );
  },
  regenerateRun(
    projectId: string,
    instanceId: string,
    sessionId: string,
    runId: string,
    messageId?: string,
  ) {
    return replayRun(
      projectId,
      instanceId,
      sessionId,
      runId,
      "regenerate",
      messageId,
    );
  },
  renameSession(
    projectId: string,
    instanceId: string,
    sessionId: string,
    title: string,
  ) {
    return apiClient.request<AgentSession>(
      sessionPath(projectId, instanceId, sessionId),
      { body: { title }, method: "PATCH" },
    );
  },
  rerunRun(
    projectId: string,
    instanceId: string,
    sessionId: string,
    runId: string,
    messageId?: string,
  ) {
    return replayRun(
      projectId,
      instanceId,
      sessionId,
      runId,
      "rerun",
      messageId,
    );
  },
  resetPrompt(projectId: string, instanceId: string) {
    return apiClient.request<AgentPrompt>(
      `${instancePath(projectId, instanceId)}/prompt/reset`,
      { method: "POST" },
    );
  },
  revokeToken(projectId: string, instanceId: string, tokenId: string) {
    return apiClient.request<void>(
      `${instancePath(projectId, instanceId)}/tokens/${encodeURIComponent(tokenId)}`,
      { method: "DELETE" },
    );
  },
  rotateToken(projectId: string, instanceId: string) {
    return apiClient.request<AgentTokenRotationResult>(
      `${instancePath(projectId, instanceId)}/tokens/rotate`,
      { body: {}, method: "POST" },
    );
  },
  setDefaultSession(projectId: string, instanceId: string, sessionId: string) {
    return apiClient.request<AgentSession>(
      `${sessionPath(projectId, instanceId, sessionId)}/default`,
      { method: "POST" },
    );
  },
  startRun(
    projectId: string,
    instanceId: string,
    sessionId: string,
    message: string,
    artifactIds: string[] = [],
    reasoningEffort?: AgentReasoningEffort,
  ) {
    return apiClient.request<AgentRunLaunch>(
      `${sessionPath(projectId, instanceId, sessionId)}/runs`,
      {
        body: {
          ...(artifactIds.length ? { artifact_ids: artifactIds } : {}),
          message,
          ...(reasoningEffort ? { reasoning_effort: reasoningEffort } : {}),
        },
        method: "POST",
      },
    );
  },
  stopRun(
    projectId: string,
    instanceId: string,
    sessionId: string,
    runId: string,
  ) {
    return apiClient.request<AgentRun>(
      `${runPath(projectId, instanceId, sessionId, runId)}/stop`,
      { method: "POST" },
    );
  },
  updateInstance(
    projectId: string,
    instanceId: string,
    input: AgentInstanceUpdate,
  ) {
    return apiClient.request<AgentInstanceProvisioningResult>(
      instancePath(projectId, instanceId),
      { body: input, method: "PATCH" },
    );
  },
  updatePrompt(projectId: string, instanceId: string, content: string) {
    return apiClient.request<AgentPrompt>(
      `${instancePath(projectId, instanceId)}/prompt`,
      { body: { content }, method: "PATCH" },
    );
  },
  verifyProjectAccess(projectId: string, instanceId: string) {
    return apiClient.request<AgentProjectAccessVerificationResult>(
      `${instancePath(projectId, instanceId)}/project-access/verify`,
      { method: "POST" },
    );
  },
  verifyToken(projectId: string, instanceId: string, tokenId: string) {
    return apiClient.request(
      `${instancePath(projectId, instanceId)}/tokens/${encodeURIComponent(tokenId)}/verify`,
      { method: "POST" },
    );
  },
};

export async function streamAgentRun(
  input: {
    instanceId: string;
    lastEventId?: string;
    projectId: string;
    runId: string;
    sessionId: string;
  },
  onEvent: (event: AgentStreamEvent) => void | Promise<void>,
  options: {
    fetchImplementation?: typeof fetch;
    signal?: AbortSignal;
  } = {},
): Promise<string | undefined> {
  const headers = new Headers({ accept: "text/event-stream" });
  if (input.lastEventId) {
    headers.set("last-event-id", input.lastEventId);
  }
  const fetchImplementation = options.fetchImplementation ?? globalThis.fetch;
  const response = await fetchImplementation(
    `/api${runPath(input.projectId, input.instanceId, input.sessionId, input.runId)}/events`,
    {
      credentials: "include",
      headers,
      method: "GET",
      signal: options.signal,
    },
  );
  if (!response.ok) {
    throw new ApiError({
      code: "AGENT_STREAM_UNAVAILABLE",
      message: "Agent 回复流暂时不可用",
      status: response.status,
    });
  }
  if (!response.body) {
    throw new ApiError({
      code: "AGENT_STREAM_EMPTY",
      message: "Agent 回复流为空",
      status: 502,
    });
  }

  let lastEventId = input.lastEventId;
  for await (const frame of parseAgentEventStream(response.body)) {
    lastEventId = frame.id ?? lastEventId;
    await onEvent(frame.event);
  }
  return lastEventId;
}

export async function* parseAgentEventStream(
  stream: ReadableStream<Uint8Array>,
): AsyncGenerator<{ event: AgentStreamEvent; id?: string }> {
  const decoder = new TextDecoder();
  const reader = stream.getReader();
  let buffer = "";

  try {
    while (true) {
      const { done, value } = await reader.read();
      buffer += decoder.decode(value, { stream: !done });
      const frames = takeCompleteFrames(buffer);
      buffer = frames.rest;
      for (const rawFrame of frames.complete) {
        const frame = parseFrame(rawFrame);
        if (frame) {
          yield frame;
        }
      }
      if (done) {
        if (buffer.trim()) {
          const frame = parseFrame(buffer);
          if (frame) {
            yield frame;
          }
        }
        return;
      }
    }
  } finally {
    reader.releaseLock();
  }
}

function instancePath(projectId: string, instanceId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/agent-instances/${encodeURIComponent(instanceId)}`;
}

function sessionPath(
  projectId: string,
  instanceId: string,
  sessionId: string,
): string {
  return `${instancePath(projectId, instanceId)}/sessions/${encodeURIComponent(sessionId)}`;
}

function runPath(
  projectId: string,
  instanceId: string,
  sessionId: string,
  runId: string,
): string {
  return `${sessionPath(projectId, instanceId, sessionId)}/runs/${encodeURIComponent(runId)}`;
}

function replayRun(
  projectId: string,
  instanceId: string,
  sessionId: string,
  runId: string,
  action: "regenerate" | "rerun",
  messageId?: string,
) {
  return apiClient.request<AgentRunLaunch>(
    `${runPath(projectId, instanceId, sessionId, runId)}/${action}`,
    { body: messageId ? { message_id: messageId } : {}, method: "POST" },
  );
}

function takeCompleteFrames(buffer: string): {
  complete: string[];
  rest: string;
} {
  const complete: string[] = [];
  let offset = 0;
  for (const match of buffer.matchAll(/\r\n\r\n|\n\n|\r\r/g)) {
    complete.push(buffer.slice(offset, match.index));
    offset = (match.index ?? 0) + match[0].length;
  }
  return {
    complete,
    rest: buffer.slice(offset),
  };
}

function parseFrame(
  rawFrame: string,
): { event: AgentStreamEvent; id?: string } | null {
  let id: string | undefined;
  const data: string[] = [];
  for (const line of rawFrame.split(/\r\n|\r|\n/)) {
    if (!line || line.startsWith(":")) {
      continue;
    }
    const separator = line.indexOf(":");
    const field = separator === -1 ? line : line.slice(0, separator);
    let value = separator === -1 ? "" : line.slice(separator + 1);
    if (value.startsWith(" ")) {
      value = value.slice(1);
    }
    if (field === "id") {
      id = value;
    } else if (field === "data") {
      data.push(value);
    }
  }
  if (data.length === 0) {
    return null;
  }
  const parsed = JSON.parse(data.join("\n")) as unknown;
  if (!isAgentStreamEvent(parsed)) {
    throw new Error("Agent stream returned an invalid event");
  }
  return { event: parsed, id };
}

function isAgentStreamEvent(value: unknown): value is AgentStreamEvent {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as Record<string, unknown>).event === "string" &&
    typeof (value as Record<string, unknown>).event_id === "string" &&
    typeof (value as Record<string, unknown>).run_id === "string" &&
    typeof (value as Record<string, unknown>).session_id === "string" &&
    typeof (value as Record<string, unknown>).sequence === "number"
  );
}

export async function optionalAgentInstance(
  client: ApiClient,
  projectId: string,
): Promise<AgentInstance | null> {
  const result = await client.request<{ items: AgentInstance[] }>(
    `/projects/${encodeURIComponent(projectId)}/agent-instances`,
  );
  return (
    result.items.find((instance) => instance.status !== "disabled") ?? null
  );
}
