"use client";

import { useQuery } from "@tanstack/react-query";
import { Bot, Brain, Clock3, RefreshCw, Wrench, X } from "lucide-react";
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
  const [streamReconnectAttempt, setStreamReconnectAttempt] = useState(0);
  const [streamTools, setStreamTools] = useState<AgentToolCall[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const terminalRefreshRunId = useRef<string | null>(null);

  const evaluation = useQuery({
    enabled: open,
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
      return 1_000;
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
    setStreamReconnectAttempt(0);
    setStreamTools([]);
    terminalRefreshRunId.current = null;
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
    let reconnectTimer: number | undefined;
    const scheduleReconnect = () => {
      if (reconnectTimer !== undefined) return;
      const delay = Math.min(1_000 * 2 ** streamReconnectAttempt, 8_000);
      reconnectTimer = window.setTimeout(() => {
        reconnectTimer = undefined;
        if (!controller.signal.aborted) {
          setStreamReconnectAttempt((current) => current + 1);
        }
      }, delay);
    };
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
            const recoverableStreamError =
              event.event === "error" &&
              event.safe_error_code === "runtime_stream_failed";
            if (event.event !== "error") setStreamError(null);
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
            const terminalStatus = recoverableStreamError
              ? null
              : terminalStatusForEvent(event.event);
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
                recoverableStreamError
                  ? "实时过程连接暂时中断，评估仍在后台执行；完成后会自动补全记录。"
                  : (event.safe_error_message ??
                      "Agent 评估输出流暂时不可用。"),
              );
              if (recoverableStreamError) {
                await Promise.all([refetchMessages(), refetchRun()]);
                scheduleReconnect();
              }
            }
          },
          { signal: controller.signal },
        );
        if (!controller.signal.aborted) {
          await refetchMessages();
        }
      } catch {
        if (!controller.signal.aborted) {
          setStreamError("实时过程连接暂时中断，正在从已保存记录恢复。");
          await Promise.all([refetchMessages(), refetchRun()]);
          scheduleReconnect();
        }
      }
    })();
    return () => {
      controller.abort();
      if (draftTimer !== undefined) window.clearTimeout(draftTimer);
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer);
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
    streamReconnectAttempt,
  ]);

  useEffect(() => {
    if (
      !open ||
      !identifiersReady ||
      !run ||
      !terminalStatuses.has(run.status) ||
      terminalRefreshRunId.current === run.run_id
    ) {
      return;
    }
    terminalRefreshRunId.current = run.run_id;
    setStreamError(null);
    void Promise.all([refetchMessages(), refetchRun()]);
  }, [identifiersReady, open, refetchMessages, refetchRun, run]);

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
  const hasAssistantOutput = currentMessages.some(
    (message) => message.role === "assistant" && message.content.trim(),
  );
  const showEvaluationFallback = Boolean(
    evaluation.data?.status === "succeeded" &&
    (!hasAssistantOutput || messages.isError),
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
              {messages.isError ? (
                <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-300/60 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-700/50 dark:bg-amber-950/30 dark:text-amber-100">
                  <p>
                    原始会话消息暂时无法读取；下面仍会显示已保存的工具步骤和评估结论。
                  </p>
                  <Button
                    onClick={() => void refetchMessages()}
                    size="sm"
                    variant="outline"
                  >
                    <RefreshCw aria-hidden="true" className="size-3.5" />
                    重新读取会话
                  </Button>
                </div>
              ) : null}
              {persistedRun.isError ? (
                <p className="rounded-lg bg-destructive/10 p-3 text-sm text-destructive">
                  无法刷新 Run 状态，正在使用已保存的评估记录。
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
              {showEvaluationFallback && evaluation.data ? (
                <PersistedEvaluationResult evaluation={evaluation.data} />
              ) : null}
              {streamError && runBusy ? (
                <p className="rounded-lg border border-amber-300/60 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-700/50 dark:bg-amber-950/30 dark:text-amber-100">
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
            为保护模型内部推理，这里不展示隐藏思考文本；可核验的工具步骤和最终结论会完整保留。
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
          正在准备项目证据；隐藏思考文本不会展示，可核验的工具步骤会保留在这里。
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
              <span className="truncate font-medium">
                {toolStepLabel(tool.name)}
              </span>
            </span>
            <Badge>{toolStatusLabel(tool.status)}</Badge>
          </summary>
          {tool.name ? (
            <code className="mt-2 block truncate text-[11px] text-muted-foreground">
              {tool.name}
            </code>
          ) : null}
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

const PersistedEvaluationResult = memo(function PersistedEvaluationResult({
  evaluation,
}: Readonly<{ evaluation: ProgressEvaluation }>) {
  const sections = [
    { items: evaluation.changes_since_last, title: "本轮确认的变化" },
    { items: evaluation.completed_items, title: "已经完成" },
    { items: evaluation.in_progress_items, title: "正在进行" },
    { items: evaluation.blockers, title: "当前阻塞" },
    { items: evaluation.pending_questions, title: "需要你确认" },
  ].filter((section) => section.items?.length);
  return (
    <section className="rounded-xl border border-border bg-muted/20 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-semibold">评估结论（已保存）</h3>
        <Badge>{evaluation.detected_stage || "阶段待确认"}</Badge>
      </div>
      <p className="mt-3 whitespace-pre-wrap text-sm leading-7">
        {evaluation.summary || "本轮评估已完成，但没有生成摘要。"}
      </p>
      {sections.map((section) => (
        <div className="mt-4" key={section.title}>
          <h4 className="text-xs font-medium text-muted-foreground">
            {section.title}
          </h4>
          <ul className="mt-2 space-y-1.5 text-sm leading-6">
            {section.items.map((item) => (
              <li className="flex gap-2" key={item}>
                <span aria-hidden="true" className="text-muted-foreground">
                  ·
                </span>
                <span>{item}</span>
              </li>
            ))}
          </ul>
        </div>
      ))}
      {evaluation.risks?.length ? (
        <div className="mt-4">
          <h4 className="text-xs font-medium text-muted-foreground">
            需要关注的风险
          </h4>
          <ul className="mt-2 space-y-2">
            {evaluation.risks.map((risk) => (
              <li
                className="rounded-lg border border-border bg-background/70 p-3"
                key={risk.risk_id}
              >
                <div className="flex flex-wrap items-center justify-between gap-2 text-sm font-medium">
                  <span>{risk.title}</span>
                  <Badge>{riskSeverityLabel(risk.severity)}</Badge>
                </div>
                <p className="mt-1 text-xs leading-5 text-muted-foreground">
                  {risk.detail}
                </p>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </section>
  );
});

function toolStepLabel(name?: string): string {
  const normalized = (name ?? "").toLowerCase();
  if (normalized.includes("tool_describe")) return "确认可用工具";
  if (normalized.endsWith("project_get")) return "读取项目目标与约束";
  if (normalized.endsWith("progress_get")) return "读取当前任务与里程碑";
  if (normalized.endsWith("data_list")) return "查找项目证据";
  if (normalized.endsWith("data_read")) return "读取证据详情";
  if (normalized.endsWith("project.get")) return "读取项目目标与约束";
  if (normalized.endsWith("progress.get")) return "读取当前任务与里程碑";
  if (normalized.endsWith("data.list")) return "查找项目证据";
  if (normalized.endsWith("data.read")) return "读取证据详情";
  return name || "工具调用";
}

function riskSeverityLabel(
  severity: ProgressEvaluation["risks"][number]["severity"],
): string {
  return { critical: "严重", high: "高", low: "低", medium: "中" }[severity];
}

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
