"use client";

import { useQuery } from "@tanstack/react-query";
import { Bot, Brain, Clock3, Wrench, X } from "lucide-react";
import { memo, useEffect, useMemo, useRef, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { MarkdownPreview } from "@/components/ui/markdown-preview";
import type { ProgressEvaluation } from "@/features/progress/types";
import { apiClient } from "@/lib/api-client";
import { cn } from "@/lib/cn";

import { agentApi, streamAgentRun } from "./agent-api";
import type {
  AgentMessage,
  AgentRun,
  AgentRunStatus,
  AgentToolCall,
} from "./types";

const terminalStatuses = new Set<AgentRunStatus>([
  "completed",
  "failed",
  "stopped",
]);

export function AgentSessionDialog({
  agentInstanceId,
  evaluationId,
  evaluationStatus,
  onClose,
  open,
  projectId,
  runId,
  sessionId,
}: Readonly<{
  agentInstanceId?: string;
  evaluationId: string;
  evaluationStatus: string;
  onClose: () => void;
  open: boolean;
  projectId: string;
  runId?: string;
  sessionId?: string;
}>) {
  const [activeRun, setActiveRun] = useState<AgentRun | null>(null);
  const [reasoningState, setReasoningState] = useState<
    "idle" | "active" | "complete"
  >("idle");
  const [streamDraft, setStreamDraft] = useState<AgentMessage | null>(null);
  const [streamError, setStreamError] = useState<string | null>(null);
  const [streamTools, setStreamTools] = useState<AgentToolCall[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);

  const initialIdentifiersReady = Boolean(
    agentInstanceId && sessionId && runId,
  );
  const evaluation = useQuery({
    enabled: open && !initialIdentifiersReady,
    queryFn: () =>
      apiClient.request<ProgressEvaluation>(
        `/projects/${encodeURIComponent(projectId)}/progress/evaluations/${encodeURIComponent(evaluationId)}`,
        { cache: "no-store" },
      ),
    queryKey: ["progress-evaluation", projectId, evaluationId],
    refetchInterval: (query) => {
      const item = query.state.data;
      if (item?.status === "failed" || item?.status === "succeeded") {
        return false;
      }
      return item?.agent_run_id ? false : 1_000;
    },
    refetchIntervalInBackground: true,
  });
  const resolvedAgentInstanceId =
    evaluation.data?.agent_instance_id ?? agentInstanceId;
  const resolvedSessionId = evaluation.data?.agent_session_id ?? sessionId;
  const resolvedRunId = evaluation.data?.agent_run_id ?? runId;
  const resolvedEvaluationStatus = evaluation.data?.status ?? evaluationStatus;
  const identifiersReady = Boolean(
    resolvedAgentInstanceId && resolvedSessionId && resolvedRunId,
  );
  const messages = useQuery({
    enabled: open && identifiersReady,
    queryFn: () =>
      agentApi.listMessages(
        projectId,
        resolvedAgentInstanceId!,
        resolvedSessionId!,
      ),
    queryKey: [
      "agent-messages",
      projectId,
      resolvedAgentInstanceId,
      resolvedSessionId,
      "progress-evaluation",
    ],
    staleTime: Number.POSITIVE_INFINITY,
  });
  const persistedRun = useQuery({
    enabled: open && identifiersReady,
    queryFn: () =>
      agentApi.getRun(
        projectId,
        resolvedAgentInstanceId!,
        resolvedSessionId!,
        resolvedRunId!,
      ),
    queryKey: [
      "agent-run",
      projectId,
      resolvedAgentInstanceId,
      resolvedSessionId,
      resolvedRunId,
      "progress-evaluation",
    ],
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return open &&
        identifiersReady &&
        (!status || !terminalStatuses.has(status))
        ? 3_000
        : false;
    },
    refetchIntervalInBackground: true,
  });

  useEffect(() => {
    setActiveRun(null);
    setReasoningState("idle");
    setStreamDraft(null);
    setStreamError(null);
    setStreamTools([]);
  }, [evaluationId, resolvedRunId, resolvedSessionId]);

  useEffect(() => {
    if (persistedRun.data) {
      setActiveRun((current) =>
        sameRunSnapshot(current, persistedRun.data!)
          ? current
          : persistedRun.data!,
      );
    }
  }, [persistedRun.data]);

  const run = activeRun ?? persistedRun.data ?? null;
  const runBusy = Boolean(run && !terminalStatuses.has(run.status));
  const refetchMessages = messages.refetch;
  const refetchRun = persistedRun.refetch;

  useEffect(() => {
    if (!open || !identifiersReady || !run || !runBusy) return;
    const controller = new AbortController();
    let pendingDraft: AgentMessage | null = null;
    let draftTimer: number | undefined;
    const publishDraft = () => {
      draftTimer = undefined;
      if (pendingDraft) setStreamDraft(pendingDraft);
    };
    const publishDraftNow = (draft: AgentMessage) => {
      if (draftTimer !== undefined) window.clearTimeout(draftTimer);
      draftTimer = undefined;
      pendingDraft = draft;
      setStreamDraft(draft);
    };
    const queueDraft = (draft: AgentMessage) => {
      pendingDraft = draft;
      if (draftTimer === undefined) {
        draftTimer = window.setTimeout(publishDraft, 80);
      }
    };
    void (async () => {
      try {
        await streamAgentRun(
          {
            instanceId: resolvedAgentInstanceId!,
            projectId,
            runId: resolvedRunId!,
            sessionId: resolvedSessionId!,
          },
          async (event) => {
            if (event.run) {
              setActiveRun((current) =>
                sameRunSnapshot(current, event.run!) ? current : event.run!,
              );
            }
            if (event.event === "reasoning.available") {
              setReasoningState("active");
            } else if (event.event === "message.started") {
              publishDraftNow({
                content: "",
                message_id: event.message_id ?? `stream-${event.event_id}`,
                role: "assistant",
              });
            } else if (event.event === "message.delta" && event.delta) {
              setReasoningState("complete");
              queueDraft({
                content: mergeStreamText(
                  pendingDraft?.content ?? "",
                  event.delta,
                ),
                message_id:
                  event.message_id ??
                  pendingDraft?.message_id ??
                  `stream-${event.event_id}`,
                role: "assistant",
              });
            } else if (event.event === "message.completed" && event.delta) {
              setReasoningState("complete");
              publishDraftNow({
                content: event.delta,
                message_id: event.message_id ?? `stream-${event.event_id}`,
                role: "assistant",
              });
            }
            if (event.tool_call) {
              setStreamTools((current) =>
                upsertToolCall(current, event.tool_call!),
              );
            }
            const terminalStatus = terminalStatusForEvent(event.event);
            if (terminalStatus) {
              if (pendingDraft) publishDraftNow(pendingDraft);
              setReasoningState((current) =>
                current === "idle" ? current : "complete",
              );
              setActiveRun(
                (current) =>
                  event.run ??
                  (current
                    ? {
                        ...current,
                        status: terminalStatus,
                        updated_at: event.occurred_at,
                      }
                    : null),
              );
              await Promise.all([refetchMessages(), refetchRun()]);
            }
            if (event.event === "error") {
              setStreamError(
                event.safe_error_message ?? "Agent 评估输出流暂时不可用。",
              );
            }
          },
          { signal: controller.signal },
        );
        if (!controller.signal.aborted) {
          await refetchMessages();
        }
      } catch (error: unknown) {
        if (!controller.signal.aborted) {
          setStreamError(
            error instanceof Error ? error.message : "Agent 评估输出流已中断。",
          );
        }
      }
    })();
    return () => {
      controller.abort();
      if (draftTimer !== undefined) window.clearTimeout(draftTimer);
    };
  }, [
    identifiersReady,
    open,
    projectId,
    refetchMessages,
    refetchRun,
    resolvedAgentInstanceId,
    resolvedRunId,
    resolvedSessionId,
    runBusy,
  ]);

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose, open]);

  const toolCalls = useMemo(
    () => mergeToolCalls(run?.tool_calls ?? [], streamTools),
    [run?.tool_calls, streamTools],
  );
  const currentMessages = useMemo(
    () =>
      prepareSessionMessages(
        messages.data?.items ?? [],
        new Set(runBusy ? toolCalls.map((tool) => tool.tool_call_id) : []),
      ),
    [messages.data?.items, runBusy, toolCalls],
  );
  const streamDraftPersisted = Boolean(
    streamDraft?.content.trim() &&
    currentMessages.some(
      (message) =>
        message.role === "assistant" &&
        messageComparisonKey(message.content) ===
          messageComparisonKey(streamDraft.content),
    ),
  );

  useEffect(() => {
    const viewport = scrollRef.current;
    if (viewport) viewport.scrollTop = viewport.scrollHeight;
  }, [currentMessages.length, streamDraft?.content, toolCalls.length]);

  if (!open) return null;

  return (
    <>
      <div
        aria-label="自动进度评估 Session"
        aria-modal="true"
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-3 sm:p-6"
        onMouseDown={(event) => {
          if (event.currentTarget === event.target) onClose();
        }}
        role="dialog"
      >
        <section className="flex h-[min(88vh,56rem)] w-full max-w-4xl min-w-0 flex-col overflow-hidden rounded-2xl border border-border bg-background shadow-2xl">
          <header className="flex shrink-0 items-start justify-between gap-4 border-b border-border px-4 py-3 sm:px-5">
            <div className="min-w-0">
              <h2 className="flex items-center gap-2 font-semibold">
                <Bot aria-hidden="true" className="size-4" />
                自动进度评估 Session
              </h2>
              <p className="mt-1 truncate text-xs text-muted-foreground">
                只读 · 评估 {evaluationId}
              </p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <Badge>
                {run
                  ? runStatusLabel(run.status)
                  : evaluationStatusLabel(resolvedEvaluationStatus)}
              </Badge>
              <Button
                aria-label="关闭评估 Session"
                onClick={onClose}
                size="icon"
                variant="ghost"
              >
                <X aria-hidden="true" className="size-4" />
              </Button>
            </div>
          </header>

          <div
            className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-5 sm:px-6"
            ref={scrollRef}
          >
            <div className="mx-auto flex w-full max-w-3xl flex-col gap-5">
              {!identifiersReady &&
              (resolvedEvaluationStatus === "queued" ||
                resolvedEvaluationStatus === "running") ? (
                <div className="rounded-xl border border-dashed border-border p-8 text-center">
                  <Clock3
                    aria-hidden="true"
                    className="mx-auto size-6 text-muted-foreground"
                  />
                  <p className="mt-3 text-sm font-medium">
                    正在等待 Agent Session
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    评估进入 Agent
                    执行阶段后，这里会自动显示思考状态、工具调用和输出。
                  </p>
                </div>
              ) : null}
              {!identifiersReady && resolvedEvaluationStatus === "failed" ? (
                <div className="rounded-xl border border-destructive/30 bg-destructive/5 p-6 text-center">
                  <p className="text-sm font-medium text-destructive">
                    评估在 Agent Session 或 Run 可用前失败
                  </p>
                  <p className="mt-2 text-xs text-muted-foreground">
                    {evaluation.data?.error_message ??
                      "该次评估没有可读取的 Session 输出。"}
                  </p>
                  {evaluation.data?.error_code ? (
                    <Badge className="mt-3">{evaluation.data.error_code}</Badge>
                  ) : null}
                </div>
              ) : null}
              {!identifiersReady && resolvedEvaluationStatus === "succeeded" ? (
                <p className="rounded-lg border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
                  该评估没有关联 Agent Session，可能由 mock 评估器生成。
                </p>
              ) : null}
              {evaluation.isError ? (
                <p className="rounded-lg bg-destructive/10 p-3 text-sm text-destructive">
                  无法刷新评估状态，正在显示页面已有的信息。
                </p>
              ) : null}
              {identifiersReady && messages.isLoading ? (
                <p className="text-sm text-muted-foreground">
                  正在读取评估会话…
                </p>
              ) : null}
              {messages.isError || persistedRun.isError ? (
                <p className="rounded-lg bg-destructive/10 p-3 text-sm text-destructive">
                  无法读取该评估 Session，请稍后重试。
                </p>
              ) : null}
              {currentMessages.map((message) => (
                <SessionMessageBubble
                  key={message.message_id}
                  message={message}
                />
              ))}
              {identifiersReady ? (
                <SessionRunActivity
                  reasoningState={reasoningState}
                  runBusy={runBusy}
                  toolCalls={toolCalls}
                />
              ) : null}
              {streamDraft && !streamDraftPersisted ? (
                <SessionMessageBubble
                  message={streamDraft}
                  streaming={runBusy}
                />
              ) : null}
              {streamError ? (
                <p className="rounded-lg bg-destructive/10 p-3 text-sm text-destructive">
                  {streamError}
                </p>
              ) : null}
            </div>
          </div>

          <footer className="shrink-0 border-t border-border px-4 py-3 text-center text-xs text-muted-foreground">
            此窗口为只读视图，不能向自动评估 Session 发送消息。
          </footer>
        </section>
      </div>
    </>
  );
}

const SessionMessageBubble = memo(function SessionMessageBubble({
  message,
  streaming = false,
}: Readonly<{ message: AgentMessage; streaming?: boolean }>) {
  const user = message.role === "user";
  return (
    <div
      className={cn(
        "flex [content-visibility:auto] [contain-intrinsic-size:auto_120px]",
        user ? "justify-end" : "justify-start",
      )}
    >
      <div
        className={cn(
          "min-w-0 max-w-[88%] text-sm",
          user ? "rounded-2xl bg-muted px-4 py-3" : "w-full",
        )}
      >
        {!user ? (
          <p className="mb-2 flex items-center gap-2 text-xs font-medium text-muted-foreground">
            <Bot aria-hidden="true" className="size-3.5" /> Hermes
          </p>
        ) : null}
        {user ? (
          <details className="group">
            <summary className="cursor-pointer list-none text-sm text-muted-foreground marker:hidden">
              <span className="mr-2 inline-block transition-transform group-open:rotate-90">
                ›
              </span>
              发送到 Agent 的信息
              {message.content ? (
                <span className="ml-2 text-xs opacity-70">
                  {message.content.replace(/\s+/g, " ").slice(0, 80)}
                  {message.content.length > 80 ? "…" : ""}
                </span>
              ) : null}
            </summary>
            <div className="mt-3">
              {message.attachments?.length ? (
                <div className="mb-2 flex flex-wrap gap-2">
                  {message.attachments.map((attachment) => (
                    <Badge
                      key={`${attachment.artifact_id}-${attachment.version_id}`}
                    >
                      {attachment.filename}
                    </Badge>
                  ))}
                </div>
              ) : null}
              <p className="whitespace-pre-wrap text-sm leading-7">
                {message.content}
              </p>
              {message.tool_calls?.length ? (
                <SessionToolCallList toolCalls={message.tool_calls} />
              ) : null}
            </div>
          </details>
        ) : (
          <>
            {message.attachments?.length ? (
              <div className="mb-2 flex flex-wrap gap-2">
                {message.attachments.map((attachment) => (
                  <Badge
                    key={`${attachment.artifact_id}-${attachment.version_id}`}
                  >
                    {attachment.filename}
                  </Badge>
                ))}
              </div>
            ) : null}
            {streaming ? (
              <p className="whitespace-pre-wrap text-sm leading-7">
                {message.content || "…"}
                <span aria-label="正在流式回复"> ▍</span>
              </p>
            ) : (
              <MarkdownPreview
                className="border-0 bg-transparent p-0 text-sm leading-7 shadow-none"
                source={message.content}
              />
            )}
            {message.tool_calls?.length ? (
              <SessionToolCallList toolCalls={message.tool_calls} />
            ) : null}
          </>
        )}
      </div>
    </div>
  );
});

const SessionRunActivity = memo(function SessionRunActivity({
  reasoningState,
  runBusy,
  toolCalls,
}: Readonly<{
  reasoningState: "idle" | "active" | "complete";
  runBusy: boolean;
  toolCalls: AgentToolCall[];
}>) {
  if (!runBusy) {
    const settled = toolCalls.map((toolCall) =>
      toolCall.status === "queued" || toolCall.status === "running"
        ? { ...toolCall, status: "completed" as const }
        : toolCall,
    );
    return (
      <>
        {settled.length ? <SessionToolCallList toolCalls={settled} /> : null}
        {reasoningState === "idle" ? (
          <p className="mt-2 text-xs text-muted-foreground">
            此 Run 没有收到可展示的 reasoning.available
            事件；不能据此判断模型未思考，隐藏推理文本按安全策略不输出。
          </p>
        ) : null}
      </>
    );
  }
  return (
    <div className="rounded-xl border border-border/70 bg-muted/30 p-3 text-sm">
      {reasoningState !== "idle" ? (
        <div className="flex items-center gap-2 text-muted-foreground">
          <Brain
            aria-hidden="true"
            className={cn(
              "size-4",
              reasoningState === "active" ? "animate-pulse" : null,
            )}
          />
          <span>
            {reasoningState === "active" ? "正在思考…" : "已完成思考"}
          </span>
        </div>
      ) : null}
      {toolCalls.length ? <SessionToolCallList toolCalls={toolCalls} /> : null}
      {reasoningState === "idle" && toolCalls.length === 0 ? (
        <p className="text-muted-foreground">
          未收到可展示的思考事件；模型的隐藏推理不会在 Session 中输出。
        </p>
      ) : null}
    </div>
  );
});

const SessionToolCallList = memo(function SessionToolCallList({
  toolCalls,
}: Readonly<{ toolCalls: AgentToolCall[] }>) {
  return (
    <div className="mt-2 space-y-2">
      {toolCalls.map((tool) => (
        <details
          className="rounded-lg border border-border bg-background/70 px-3 py-2"
          key={tool.tool_call_id || `${tool.name}-${tool.status}`}
        >
          <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-xs">
            <span className="flex min-w-0 items-center gap-2">
              <Wrench aria-hidden="true" className="size-3.5 shrink-0" />
              <code className="truncate">{tool.name || "工具调用"}</code>
            </span>
            <Badge>{toolStatusLabel(tool.status)}</Badge>
          </summary>
          {tool.input_summary ? (
            <p className="mt-2 text-xs text-muted-foreground">
              {tool.input_summary}
            </p>
          ) : null}
          {tool.output_summary ? (
            <p className="mt-1 text-xs text-muted-foreground">
              {tool.output_summary}
            </p>
          ) : null}
          {tool.safe_error_code ? (
            <p className="mt-1 text-xs text-destructive">
              {tool.safe_error_code}
            </p>
          ) : null}
        </details>
      ))}
    </div>
  );
});

function prepareSessionMessages(
  messages: AgentMessage[],
  activeToolCallIds: Set<string>,
): AgentMessage[] {
  const result: AgentMessage[] = [];
  const assistantMessages = new Map<string, number>();
  for (const message of messages) {
    const toolCalls = (message.tool_calls ?? []).filter(
      (toolCall) => !activeToolCallIds.has(toolCall.tool_call_id),
    );
    const candidate = { ...message, tool_calls: toolCalls };
    const key = messageComparisonKey(candidate.content);
    if (!key && !candidate.attachments?.length && toolCalls.length === 0)
      continue;
    if (candidate.role === "assistant" && key) {
      const existingIndex = assistantMessages.get(key);
      if (existingIndex !== undefined) {
        const existing = result[existingIndex];
        result[existingIndex] = {
          ...existing,
          attachments: candidate.attachments ?? existing.attachments,
          tool_calls: mergeToolCalls(existing.tool_calls ?? [], toolCalls),
        };
        continue;
      }
      assistantMessages.set(key, result.length);
    }
    result.push(candidate);
  }
  return result;
}

function upsertToolCall(
  current: AgentToolCall[],
  toolCall: AgentToolCall,
): AgentToolCall[] {
  const index = current.findIndex(
    (item) => item.tool_call_id === toolCall.tool_call_id,
  );
  if (index < 0) return [...current, toolCall];
  const previous = current[index];
  if (
    previous.status === toolCall.status &&
    previous.input_summary === toolCall.input_summary &&
    previous.output_summary === toolCall.output_summary &&
    previous.safe_error_code === toolCall.safe_error_code
  ) {
    return current;
  }
  return current.map((item, itemIndex) =>
    itemIndex === index ? toolCall : item,
  );
}

function mergeToolCalls(
  persisted: AgentToolCall[],
  streamed: AgentToolCall[],
): AgentToolCall[] {
  return streamed.reduce(upsertToolCall, persisted);
}

function mergeStreamText(current: string, incoming: string): string {
  if (!current) return incoming;
  if (!incoming || current === incoming || current.endsWith(incoming)) {
    return current;
  }
  if (incoming.startsWith(current)) return incoming;
  const maximumOverlap = Math.min(current.length, incoming.length);
  for (let length = maximumOverlap; length > 0; length -= 1) {
    if (current.slice(-length) === incoming.slice(0, length)) {
      return current + incoming.slice(length);
    }
  }
  return current + incoming;
}

function messageComparisonKey(content: string): string {
  return content.trim().replace(/\s+/g, " ");
}

function sameRunSnapshot(current: AgentRun | null, next: AgentRun): boolean {
  return Boolean(
    current &&
    current.run_id === next.run_id &&
    current.status === next.status &&
    current.version === next.version &&
    current.updated_at === next.updated_at,
  );
}

function toolStatusLabel(status: AgentToolCall["status"]): string {
  return {
    completed: "已完成",
    failed: "失败",
    queued: "排队中",
    running: "执行中",
  }[status];
}

function terminalStatusForEvent(event: string): AgentRunStatus | null {
  if (event === "run.completed" || event === "done") return "completed";
  if (event === "run.failed" || event === "error") return "failed";
  if (event === "run.stopped") return "stopped";
  return null;
}

function runStatusLabel(status: AgentRunStatus): string {
  return {
    completed: "已完成",
    failed: "失败",
    queued: "排队中",
    running: "评估中",
    stopped: "已停止",
    stopping: "正在停止",
    waiting_for_approval: "等待审批",
  }[status];
}

function evaluationStatusLabel(status: string): string {
  return (
    {
      failed: "失败",
      queued: "排队中",
      running: "评估中",
      succeeded: "已完成",
    }[status] ?? status
  );
}
