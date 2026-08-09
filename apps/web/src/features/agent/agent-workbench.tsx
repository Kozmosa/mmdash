"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  Check,
  CircleStop,
  ExternalLink,
  GitFork,
  MessageSquarePlus,
  Pencil,
  Play,
  RefreshCw,
  RotateCcw,
  Settings,
  Sparkles,
} from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/cn";

import { agentApi, streamAgentRun } from "./agent-api";
import {
  ContextProposalReviewPanel,
  contextProposalQueryKey,
} from "./context-proposal-review";
import type {
  AgentApproval,
  AgentApprovalChoice,
  AgentInstance,
  AgentMessage,
  AgentRun,
  AgentRunLaunch,
  AgentSession,
  AgentSessionType,
  AgentStreamEvent,
  AgentToolCall,
} from "./types";

const terminalStatuses = new Set<AgentRun["status"]>([
  "completed",
  "failed",
  "stopped",
]);

export function AgentWorkbench() {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const canOperate =
    project.role !== "viewer" &&
    project.role !== "agent" &&
    project.role !== "box";
  const instances = useQuery({
    queryFn: () => agentApi.listInstances(project.id),
    queryKey: ["agent-instances", project.id],
  });
  const [instanceId, setInstanceId] = useState<string | null>(null);
  const instance = useMemo(
    () =>
      instances.data?.items.find(
        (candidate) => candidate.agent_instance_id === instanceId,
      ) ?? null,
    [instanceId, instances.data?.items],
  );

  useEffect(() => {
    if (instanceId || !instances.data?.items.length) {
      return;
    }
    const preferred =
      instances.data.items.find((candidate) => candidate.status === "active") ??
      instances.data.items.find(
        (candidate) => candidate.status !== "disabled",
      ) ??
      instances.data.items[0];
    setInstanceId(preferred?.agent_instance_id ?? null);
  }, [instanceId, instances.data?.items]);

  if (instances.isLoading) {
    return (
      <p className="text-sm text-muted-foreground">正在连接 Agent 工作台…</p>
    );
  }
  if (instances.isError) {
    return (
      <Card>
        <CardContent className="py-8 text-center text-sm text-destructive">
          Agent 实例读取失败，请稍后重试。
        </CardContent>
      </Card>
    );
  }
  if (!instances.data?.items.length) {
    return <UnconfiguredAgent projectId={project.id} />;
  }

  return (
    <AgentWorkspace
      canOperate={canOperate}
      instance={instance}
      instanceId={instanceId}
      instances={instances.data.items}
      onInstanceChange={setInstanceId}
      projectId={project.id}
      queryClient={queryClient}
    />
  );
}

function AgentWorkspace({
  canOperate,
  instance,
  instanceId,
  instances,
  onInstanceChange,
  projectId,
  queryClient,
}: {
  canOperate: boolean;
  instance: AgentInstance | null;
  instanceId: string | null;
  instances: AgentInstance[];
  onInstanceChange: (instanceId: string) => void;
  projectId: string;
  queryClient: ReturnType<typeof useQueryClient>;
}) {
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [newSessionTitle, setNewSessionTitle] = useState("");
  const [newSessionType, setNewSessionType] =
    useState<AgentSessionType>("main");
  const [composer, setComposer] = useState("");
  const [optimisticUser, setOptimisticUser] = useState<AgentMessage | null>(
    null,
  );
  const [streamDraft, setStreamDraft] = useState<AgentMessage | null>(null);
  const [streamTools, setStreamTools] = useState<AgentToolCall[]>([]);
  const [activeRun, setActiveRun] = useState<AgentRun | null>(null);
  const [approvals, setApprovals] = useState<AgentApproval[]>([]);
  const [streamError, setStreamError] = useState<string | null>(null);
  const streamAbort = useRef<AbortController | null>(null);
  const streamGeneration = useRef(0);
  const lastEventId = useRef<string | undefined>(undefined);

  const sessions = useQuery({
    enabled: Boolean(instanceId),
    queryFn: () => agentApi.listSessions(projectId, instanceId!),
    queryKey: ["agent-sessions", projectId, instanceId],
  });
  const selectedSession = useMemo(
    () =>
      sessions.data?.items.find(
        (session) => session.session_id === sessionId,
      ) ?? null,
    [sessionId, sessions.data?.items],
  );
  const messages = useQuery({
    enabled: Boolean(instanceId && sessionId),
    queryFn: () => agentApi.listMessages(projectId, instanceId!, sessionId!),
    queryKey: ["agent-messages", projectId, instanceId, sessionId],
  });
  const persistedRun = useQuery({
    enabled: Boolean(
      instanceId && selectedSession?.last_run_id && selectedSession.session_id,
    ),
    queryFn: () =>
      agentApi.getRun(
        projectId,
        instanceId!,
        selectedSession!.session_id,
        selectedSession!.last_run_id!,
      ),
    queryKey: [
      "agent-run",
      projectId,
      instanceId,
      selectedSession?.session_id,
      selectedSession?.last_run_id,
    ],
  });
  const run = activeRun ?? persistedRun.data ?? null;

  useEffect(() => {
    if (!sessions.data?.items.length) {
      streamGeneration.current += 1;
      streamAbort.current?.abort();
      streamAbort.current = null;
      setApprovals([]);
      setSessionId(null);
      return;
    }
    if (
      sessions.data.items.some((session) => session.session_id === sessionId)
    ) {
      return;
    }
    const preferred =
      sessions.data.items.find(
        (session) => session.default && session.status === "active",
      ) ??
      sessions.data.items.find(
        (session) =>
          session.session_type === "main" && session.status === "active",
      ) ??
      sessions.data.items.find((session) => session.status === "active") ??
      sessions.data.items[0];
    streamGeneration.current += 1;
    streamAbort.current?.abort();
    streamAbort.current = null;
    setApprovals([]);
    setSessionId(preferred?.session_id ?? null);
  }, [sessionId, sessions.data?.items]);

  useEffect(
    () => () => {
      streamGeneration.current += 1;
      streamAbort.current?.abort();
    },
    [],
  );

  const cacheSession = useCallback(
    (session: AgentSession) => {
      queryClient.setQueryData<{ items: AgentSession[] }>(
        ["agent-sessions", projectId, instanceId],
        (current) => ({
          items: [
            session,
            ...(current?.items.filter(
              (candidate) => candidate.session_id !== session.session_id,
            ) ?? []),
          ],
        }),
      );
    },
    [instanceId, projectId, queryClient],
  );

  const finishRun = useCallback(async () => {
    await Promise.all([
      messages.refetch(),
      sessions.refetch(),
      queryClient.invalidateQueries({
        queryKey: ["agent-run", projectId, instanceId],
      }),
      queryClient.invalidateQueries({
        queryKey: contextProposalQueryKey(projectId),
      }),
    ]);
    setOptimisticUser(null);
    setStreamDraft(null);
  }, [instanceId, messages, projectId, queryClient, sessions]);

  const handleStreamEvent = useCallback(
    async (event: AgentStreamEvent) => {
      lastEventId.current = event.event_id;
      if (event.run) {
        setActiveRun(event.run);
      }
      if (event.event === "message.started") {
        setStreamDraft({
          content: "",
          message_id: event.message_id ?? `stream-${event.event_id}`,
          role: "assistant",
        });
      } else if (event.event === "message.delta" && event.delta) {
        setStreamDraft((current) => ({
          content: `${current?.content ?? ""}${event.delta}`,
          message_id:
            event.message_id ??
            current?.message_id ??
            `stream-${event.event_id}`,
          role: "assistant",
        }));
      }
      if (event.tool_call) {
        setStreamTools((current) => upsertToolCall(current, event.tool_call!));
      }
      if (event.event === "approval.required" && event.approval) {
        setApprovals((current) => enqueueApproval(current, event.approval!));
      } else if (event.event === "approval.responded" && event.approval) {
        setApprovals((current) =>
          removeApproval(current, event.approval!.approval_id),
        );
      }
      const terminalStatus = terminalStatusForEvent(event.event);
      if (terminalStatus) {
        streamGeneration.current += 1;
        setActiveRun(
          (current) =>
            event.run ??
            (current
              ? {
                  ...current,
                  status: terminalStatus,
                  updated_at: event.occurred_at,
                }
              : current),
        );
        setApprovals([]);
        await finishRun();
      }
      if (event.event === "error") {
        setStreamError(event.safe_error_message ?? "Agent 回复流发生错误");
      }
    },
    [finishRun],
  );

  const beginStream = useCallback(
    (launch: AgentRunLaunch) => {
      streamAbort.current?.abort();
      const generation = streamGeneration.current + 1;
      streamGeneration.current = generation;
      const controller = new AbortController();
      streamAbort.current = controller;
      lastEventId.current = undefined;
      setActiveRun(launch.run);
      setStreamDraft(null);
      setStreamTools(launch.run.tool_calls);
      setApprovals([]);
      setStreamError(null);
      cacheSession(launch.session);
      setSessionId(launch.session.session_id);

      void (async () => {
        for (let attempt = 0; attempt < 2; attempt += 1) {
          try {
            await streamAgentRun(
              {
                instanceId: instanceId!,
                lastEventId: lastEventId.current,
                projectId,
                runId: launch.run.run_id,
                sessionId: launch.session.session_id,
              },
              async (event) => {
                if (
                  streamGeneration.current !== generation ||
                  event.run_id !== launch.run.run_id ||
                  event.session_id !== launch.session.session_id
                ) {
                  return;
                }
                await handleStreamEvent(event);
              },
              { signal: controller.signal },
            );
            return;
          } catch (error) {
            if (
              controller.signal.aborted ||
              streamGeneration.current !== generation
            ) {
              return;
            }
            if (attempt === 0) {
              continue;
            }
            setStreamError(
              error instanceof Error ? error.message : "Agent 回复流已中断",
            );
            try {
              const latest = await agentApi.getRun(
                projectId,
                instanceId!,
                launch.session.session_id,
                launch.run.run_id,
              );
              setActiveRun(latest);
              if (terminalStatuses.has(latest.status)) {
                await finishRun();
              }
            } catch {
              // The safe stream error is already visible to the user.
            }
          }
        }
      })();
    },
    [cacheSession, finishRun, handleStreamEvent, instanceId, projectId],
  );

  const createSession = useMutation({
    mutationFn: () =>
      agentApi.createSession(projectId, instanceId!, {
        default: !sessions.data?.items.length,
        session_type: newSessionType,
        title: newSessionTitle.trim(),
      }),
    onError: showError("Session 创建失败"),
    onSuccess: (session) => {
      cacheSession(session);
      setSessionId(session.session_id);
      setNewSessionTitle("");
      toast.success("Session 已创建");
    },
  });

  const mutateSession = useMutation({
    mutationFn: async (action: {
      kind: "rename" | "fork" | "end" | "continue" | "default";
      title?: string;
    }) => {
      const session = selectedSession!;
      switch (action.kind) {
        case "rename":
          return agentApi.renameSession(
            projectId,
            instanceId!,
            session.session_id,
            action.title!,
          );
        case "fork":
          return agentApi.forkSession(
            projectId,
            instanceId!,
            session.session_id,
            action.title,
          );
        case "end":
          return agentApi.endSession(
            projectId,
            instanceId!,
            session.session_id,
          );
        case "continue":
          return agentApi.continueSession(
            projectId,
            instanceId!,
            session.session_id,
          );
        case "default":
          return agentApi.setDefaultSession(
            projectId,
            instanceId!,
            session.session_id,
          );
      }
    },
    onError: showError("Session 操作失败"),
    onSuccess: (session, action) => {
      cacheSession(session);
      if (action.kind === "fork") {
        setSessionId(session.session_id);
      }
      void sessions.refetch();
      toast.success("Session 已更新");
    },
  });

  const startRun = useMutation({
    mutationFn: (message: string) =>
      agentApi.startRun(
        projectId,
        instanceId!,
        selectedSession!.session_id,
        message,
      ),
    onError: (error) => {
      setOptimisticUser(null);
      showError("消息发送失败")(error);
    },
    onSuccess: beginStream,
  });

  const replay = useMutation({
    mutationFn: (action: "regenerate" | "rerun") => {
      if (!run || !selectedSession) {
        throw new Error("没有可重放的 Run");
      }
      return action === "regenerate"
        ? agentApi.regenerateRun(
            projectId,
            instanceId!,
            selectedSession.session_id,
            run.run_id,
          )
        : agentApi.rerunRun(
            projectId,
            instanceId!,
            selectedSession.session_id,
            run.run_id,
          );
    },
    onError: showError("Run 重放失败"),
    onSuccess: beginStream,
  });

  const stop = useMutation({
    mutationFn: () =>
      agentApi.stopRun(
        projectId,
        instanceId!,
        selectedSession!.session_id,
        run!.run_id,
      ),
    onError: showError("停止 Run 失败"),
    onSuccess: (latest) => setActiveRun(latest),
  });

  const approve = useMutation({
    mutationFn: ({
      approvalId,
      choice,
    }: {
      approvalId: string;
      choice: AgentApprovalChoice;
    }) =>
      agentApi.approveRun(
        projectId,
        instanceId!,
        selectedSession!.session_id,
        run!.run_id,
        approvalId,
        choice,
      ),
    onError: showError("审批响应失败"),
    onSuccess: (latest, request) => {
      setActiveRun(latest);
      setApprovals((current) => removeApproval(current, request.approvalId));
      toast.success("审批选择已提交");
    },
  });

  const chooseSession = (nextSessionId: string) => {
    streamGeneration.current += 1;
    streamAbort.current?.abort();
    streamAbort.current = null;
    setActiveRun(null);
    setStreamDraft(null);
    setStreamTools([]);
    setOptimisticUser(null);
    setApprovals([]);
    setStreamError(null);
    setSessionId(nextSessionId);
  };

  const chooseInstance = (nextInstanceId: string) => {
    streamGeneration.current += 1;
    streamAbort.current?.abort();
    setSessionId(null);
    setActiveRun(null);
    setStreamDraft(null);
    setStreamTools([]);
    setApprovals([]);
    setStreamError(null);
    onInstanceChange(nextInstanceId);
  };

  const currentMessages = messages.data?.items ?? [];
  const hasDraftMessage = Boolean(
    streamDraft &&
    !currentMessages.some(
      (message) => message.message_id === streamDraft.message_id,
    ),
  );
  const toolCalls = mergeToolCalls(run?.tool_calls ?? [], streamTools);
  const approval = approvals[0] ?? null;
  const runBusy = Boolean(run && !terminalStatuses.has(run.status));
  const canChat =
    canOperate &&
    instance?.status === "active" &&
    selectedSession?.status === "active";

  return (
    <div className="space-y-4">
      <AgentHeader
        instance={instance}
        instanceId={instanceId}
        instances={instances}
        onInstanceChange={chooseInstance}
        projectId={projectId}
      />

      <div className="grid min-h-[680px] gap-4 xl:grid-cols-[280px_minmax(0,1fr)_320px]">
        <SessionSidebar
          canOperate={canOperate}
          createPending={createSession.isPending}
          instanceReady={instance?.status === "active"}
          newSessionTitle={newSessionTitle}
          newSessionType={newSessionType}
          onCreate={() => createSession.mutate()}
          onSelect={chooseSession}
          selectedSession={selectedSession}
          selectedSessionId={sessionId}
          sessionPending={mutateSession.isPending}
          sessions={sessions.data?.items ?? []}
          setNewSessionTitle={setNewSessionTitle}
          setNewSessionType={setNewSessionType}
          updateSession={(action) => mutateSession.mutate(action)}
        />

        <main className="flex min-h-0 flex-col rounded-xl border border-border bg-card shadow-sm">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
            <div>
              <h2 className="font-semibold">
                {selectedSession?.title ?? "选择或创建 Session"}
              </h2>
              {selectedSession ? (
                <p className="text-xs text-muted-foreground">
                  {selectedSession.session_type} · {selectedSession.status}
                  {selectedSession.default ? " · 默认" : ""}
                </p>
              ) : null}
            </div>
            <RunControls
              canOperate={canOperate}
              onRegenerate={() => replay.mutate("regenerate")}
              onRerun={() => replay.mutate("rerun")}
              onStop={() => stop.mutate()}
              pending={replay.isPending || stop.isPending}
              run={run}
            />
          </div>

          <div
            aria-label="Agent 消息历史"
            className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4"
          >
            {messages.isLoading ? (
              <p className="text-sm text-muted-foreground">
                正在读取 Hermes 消息历史…
              </p>
            ) : null}
            {!messages.isLoading &&
            selectedSession &&
            currentMessages.length === 0 &&
            !optimisticUser ? (
              <div className="py-16 text-center">
                <Sparkles
                  aria-hidden="true"
                  className="mx-auto size-8 text-muted-foreground"
                />
                <p className="mt-3 font-medium">开始这个项目会话</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  Hermes 保存完整历史；重要结论可通过 context.promote
                  提交为待审提议。
                </p>
              </div>
            ) : null}
            {currentMessages.map((message) => (
              <MessageBubble key={message.message_id} message={message} />
            ))}
            {optimisticUser ? (
              <MessageBubble message={optimisticUser} optimistic />
            ) : null}
            {hasDraftMessage && streamDraft ? (
              <MessageBubble message={streamDraft} streaming />
            ) : null}
            {approval ? (
              <ApprovalCard
                approval={approval}
                disabled={approve.isPending}
                onChoose={(choice) =>
                  approve.mutate({
                    approvalId: approval.approval_id,
                    choice,
                  })
                }
              />
            ) : null}
            {streamError ? (
              <p className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
                {streamError}
              </p>
            ) : null}
          </div>

          <form
            className="border-t border-border p-4"
            onSubmit={(event) => {
              event.preventDefault();
              const content = composer.trim();
              if (!content || !canChat || runBusy) {
                return;
              }
              setOptimisticUser({
                content,
                message_id: `optimistic-${Date.now()}`,
                role: "user",
              });
              setComposer("");
              startRun.mutate(content);
            }}
          >
            <label className="sr-only" htmlFor="agent-message">
              发给 Hermes 的消息
            </label>
            <textarea
              className="min-h-24 w-full resize-y rounded-md border border-input bg-background p-3 text-sm outline-none placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
              disabled={!canChat || runBusy}
              id="agent-message"
              onChange={(event) => setComposer(event.target.value)}
              placeholder={
                selectedSession?.status === "ended"
                  ? "继续 Session 后才能发送消息"
                  : instance?.status !== "active"
                    ? "完成 Hermes 与 MCP 验证后才能对话"
                    : "向 Hermes 发送项目问题…"
              }
              value={composer}
            />
            <div className="mt-2 flex items-center justify-between gap-3">
              <p className="text-xs text-muted-foreground">
                浏览器不会持有 Hermes API Key 或 Agent Token。
              </p>
              <Button
                disabled={
                  !composer.trim() || !canChat || runBusy || startRun.isPending
                }
                type="submit"
              >
                <Play aria-hidden="true" className="size-4" />
                发送
              </Button>
            </div>
          </form>
        </main>

        <aside className="space-y-4">
          <RunStatusCard run={run} toolCalls={toolCalls} />
          <ContextProposalReviewPanel
            canReview={canOperate}
            instances={instances}
            projectId={projectId}
          />
          {instance ? (
            <PromptCard
              canOperate={canOperate}
              instanceId={instance.agent_instance_id}
              projectId={projectId}
            />
          ) : null}
        </aside>
      </div>
    </div>
  );
}

function AgentHeader({
  instance,
  instanceId,
  instances,
  onInstanceChange,
  projectId,
}: {
  instance: AgentInstance | null;
  instanceId: string | null;
  instances: AgentInstance[];
  onInstanceChange: (instanceId: string) => void;
  projectId: string;
}) {
  return (
    <header className="flex flex-wrap items-start justify-between gap-4">
      <div className="flex items-start gap-3">
        <span className="flex size-11 items-center justify-center rounded-xl border border-border bg-card shadow-sm">
          <Bot aria-hidden="true" className="size-5" />
        </span>
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">
            mmdash Agent
          </h1>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <Badge>{instance?.status ?? "unavailable"}</Badge>
            <span>{instance?.management_mode ?? "—"}</span>
            <span>
              MCP {instance?.grant.project_access_status ?? "pending"}
            </span>
            {instance?.management_mode === "auto" ? (
              <span>管理链路 {instance.management_path}</span>
            ) : null}
          </div>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {instances.length > 1 ? (
          <label className="text-sm">
            <span className="sr-only">Agent 实例</span>
            <select
              className="h-9 rounded-md border border-input bg-background px-3 text-sm"
              onChange={(event) => onInstanceChange(event.target.value)}
              value={instanceId ?? ""}
            >
              {instances.map((candidate) => (
                <option
                  key={candidate.agent_instance_id}
                  value={candidate.agent_instance_id}
                >
                  {candidate.display_name}
                </option>
              ))}
            </select>
          </label>
        ) : null}
        {instance?.management_mode === "manual" && instance.management_url ? (
          <a
            className="inline-flex h-9 items-center gap-2 rounded-md border border-border px-3 text-sm"
            href={instance.management_url}
            rel="noreferrer"
            target="_blank"
          >
            Hermes MCP 配置
            <ExternalLink aria-hidden="true" className="size-3" />
          </a>
        ) : null}
        <Link
          className="inline-flex h-9 items-center gap-2 rounded-md border border-border px-3 text-sm"
          href={`/projects/${encodeURIComponent(projectId)}/settings#agent-settings`}
        >
          <Settings aria-hidden="true" className="size-4" />
          Agent 设置
        </Link>
      </div>
    </header>
  );
}

function SessionSidebar({
  canOperate,
  createPending,
  instanceReady,
  newSessionTitle,
  newSessionType,
  onCreate,
  onSelect,
  selectedSession,
  selectedSessionId,
  sessionPending,
  sessions,
  setNewSessionTitle,
  setNewSessionType,
  updateSession,
}: {
  canOperate: boolean;
  createPending: boolean;
  instanceReady: boolean;
  newSessionTitle: string;
  newSessionType: AgentSessionType;
  onCreate: () => void;
  onSelect: (sessionId: string) => void;
  selectedSession: AgentSession | null;
  selectedSessionId: string | null;
  sessionPending: boolean;
  sessions: AgentSession[];
  setNewSessionTitle: (title: string) => void;
  setNewSessionType: (type: AgentSessionType) => void;
  updateSession: (action: {
    kind: "rename" | "fork" | "end" | "continue" | "default";
    title?: string;
  }) => void;
}) {
  return (
    <aside className="rounded-xl border border-border bg-card p-3 shadow-sm">
      <div className="flex items-center justify-between gap-2 px-1">
        <h2 className="font-semibold">Sessions</h2>
        <Badge>{sessions.length}</Badge>
      </div>
      {canOperate ? (
        <form
          className="mt-3 space-y-2 border-b border-border pb-3"
          onSubmit={(event) => {
            event.preventDefault();
            if (newSessionTitle.trim()) {
              onCreate();
            }
          }}
        >
          <Input
            aria-label="新 Session 名称"
            disabled={!instanceReady}
            onChange={(event) => setNewSessionTitle(event.target.value)}
            placeholder="新 Session 名称"
            value={newSessionTitle}
          />
          <div className="flex gap-2">
            <select
              aria-label="新 Session 类型"
              className="h-8 min-w-0 flex-1 rounded-md border border-input bg-background px-2 text-xs"
              disabled={!instanceReady}
              onChange={(event) =>
                setNewSessionType(event.target.value as AgentSessionType)
              }
              value={newSessionType}
            >
              <option value="main">main</option>
              <option value="progress">progress</option>
              <option value="experiment">experiment</option>
            </select>
            <Button
              aria-label="创建 Session"
              disabled={
                !newSessionTitle.trim() || !instanceReady || createPending
              }
              size="icon"
              type="submit"
            >
              <MessageSquarePlus aria-hidden="true" className="size-4" />
            </Button>
          </div>
        </form>
      ) : null}
      <ul className="mt-3 max-h-[430px] space-y-1 overflow-y-auto">
        {sessions.map((session) => (
          <li key={session.session_id}>
            <button
              aria-current={
                selectedSessionId === session.session_id ? "true" : undefined
              }
              className={cn(
                "w-full rounded-md px-3 py-2 text-left text-sm transition-colors",
                selectedSessionId === session.session_id
                  ? "bg-accent text-accent-foreground"
                  : "hover:bg-accent/60",
              )}
              onClick={() => onSelect(session.session_id)}
              type="button"
            >
              <span className="flex items-center justify-between gap-2">
                <span className="truncate font-medium">{session.title}</span>
                {session.default ? (
                  <Check aria-label="默认 Session" className="size-3" />
                ) : null}
              </span>
              <span className="mt-1 flex gap-1 text-xs text-muted-foreground">
                <span>{session.session_type}</span>
                <span>·</span>
                <span>{session.status}</span>
              </span>
            </button>
          </li>
        ))}
      </ul>
      {selectedSession && canOperate ? (
        <div className="mt-3 grid grid-cols-2 gap-1 border-t border-border pt-3">
          <SessionAction
            disabled={sessionPending}
            icon={Pencil}
            label="重命名"
            onClick={() => {
              const title = window
                .prompt("Session 新名称", selectedSession.title)
                ?.trim();
              if (title && title !== selectedSession.title) {
                updateSession({ kind: "rename", title });
              }
            }}
          />
          <SessionAction
            disabled={sessionPending || (!selectedSession.default && false)}
            icon={Check}
            label="设为默认"
            onClick={() => updateSession({ kind: "default" })}
          />
          <SessionAction
            disabled={sessionPending}
            icon={GitFork}
            label="分叉"
            onClick={() => {
              const title = window
                .prompt("分叉 Session 名称", `${selectedSession.title}（分叉）`)
                ?.trim();
              if (title) {
                updateSession({ kind: "fork", title });
              }
            }}
          />
          {selectedSession.status === "active" ? (
            <SessionAction
              disabled={sessionPending}
              icon={CircleStop}
              label="结束"
              onClick={() => {
                if (window.confirm("结束这个 Session？消息历史不会删除。")) {
                  updateSession({ kind: "end" });
                }
              }}
            />
          ) : (
            <SessionAction
              disabled={sessionPending}
              icon={Play}
              label="继续"
              onClick={() => updateSession({ kind: "continue" })}
            />
          )}
        </div>
      ) : null}
    </aside>
  );
}

function SessionAction({
  disabled,
  icon: Icon,
  label,
  onClick,
}: {
  disabled: boolean;
  icon: typeof Pencil;
  label: string;
  onClick: () => void;
}) {
  return (
    <Button disabled={disabled} onClick={onClick} size="sm" variant="ghost">
      <Icon aria-hidden="true" className="size-3" />
      {label}
    </Button>
  );
}

function MessageBubble({
  message,
  optimistic,
  streaming,
}: {
  message: AgentMessage;
  optimistic?: boolean;
  streaming?: boolean;
}) {
  const user = message.role === "user";
  return (
    <div className={cn("flex", user ? "justify-end" : "justify-start")}>
      <div
        className={cn(
          "max-w-[88%] rounded-xl px-4 py-3 text-sm",
          user ? "bg-primary text-primary-foreground" : "bg-muted",
          optimistic ? "opacity-70" : null,
        )}
      >
        <p className="mb-1 text-[11px] font-medium opacity-70">
          {message.role}
        </p>
        <div className="whitespace-pre-wrap break-words leading-6">
          {message.content || (streaming ? "…" : "")}
          {streaming ? <span aria-label="正在流式回复"> ▍</span> : null}
        </div>
        {message.tool_calls?.length ? (
          <div className="mt-2 space-y-1 border-t border-current/10 pt-2">
            {message.tool_calls.map((tool) => (
              <p className="text-xs opacity-80" key={tool.tool_call_id}>
                {tool.name} · {tool.status}
              </p>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function ApprovalCard({
  approval,
  disabled,
  onChoose,
}: {
  approval: AgentApproval;
  disabled: boolean;
  onChoose: (choice: AgentApprovalChoice) => void;
}) {
  const labels: Record<AgentApprovalChoice, string> = {
    always: "始终允许",
    deny: "拒绝",
    once: "仅本次",
    session: "本 Session",
  };
  return (
    <Card className="border-amber-500/40 bg-amber-500/5">
      <CardHeader>
        <CardTitle className="text-sm">Agent 请求工具审批</CardTitle>
        <CardDescription>
          仅展示运行时中立的审批选择；原始工具参数和 Provider 状态不会暴露。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-wrap gap-2">
        {approval.choices.map((choice) => (
          <Button
            disabled={disabled}
            key={choice}
            onClick={() => onChoose(choice)}
            size="sm"
            variant={choice === "deny" ? "outline" : "default"}
          >
            {labels[choice]}
          </Button>
        ))}
      </CardContent>
    </Card>
  );
}

function RunControls({
  canOperate,
  onRegenerate,
  onRerun,
  onStop,
  pending,
  run,
}: {
  canOperate: boolean;
  onRegenerate: () => void;
  onRerun: () => void;
  onStop: () => void;
  pending: boolean;
  run: AgentRun | null;
}) {
  if (!run) {
    return <Badge>idle</Badge>;
  }
  const busy = !terminalStatuses.has(run.status);
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Badge>{run.status}</Badge>
      {canOperate && busy ? (
        <Button
          disabled={pending || run.status === "stopping"}
          onClick={onStop}
          size="sm"
          variant="outline"
        >
          <CircleStop aria-hidden="true" className="size-3" />
          停止
        </Button>
      ) : null}
      {canOperate && terminalStatuses.has(run.status) ? (
        <>
          <Button
            disabled={pending}
            onClick={onRegenerate}
            size="sm"
            variant="outline"
          >
            <RefreshCw aria-hidden="true" className="size-3" />
            重新生成
          </Button>
          <Button
            disabled={pending}
            onClick={onRerun}
            size="sm"
            variant="outline"
          >
            <RotateCcw aria-hidden="true" className="size-3" />
            重新执行
          </Button>
        </>
      ) : null}
    </div>
  );
}

function RunStatusCard({
  run,
  toolCalls,
}: {
  run: AgentRun | null;
  toolCalls: AgentToolCall[];
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center justify-between gap-2 text-base">
          当前 Run <Badge>{run?.status ?? "idle"}</Badge>
        </CardTitle>
        <CardDescription>
          {run ? `${run.source} · ${run.run_id.slice(0, 8)}` : "尚未启动 Run"}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {run?.safe_error_message ? (
          <p className="text-sm text-destructive">{run.safe_error_message}</p>
        ) : null}
        {run?.usage ? (
          <p className="text-xs text-muted-foreground">
            Tokens：{run.usage.total_tokens ?? "—"}
          </p>
        ) : null}
        <div className="space-y-2">
          <h4 className="text-sm font-medium">Tool Calls</h4>
          {toolCalls.length === 0 ? (
            <p className="text-xs text-muted-foreground">暂无工具调用。</p>
          ) : null}
          {toolCalls.map((tool) => (
            <div
              className="rounded-md border border-border p-3 text-xs"
              key={tool.tool_call_id}
            >
              <div className="flex items-center justify-between gap-2">
                <code className="truncate">{tool.name}</code>
                <Badge>{tool.status}</Badge>
              </div>
              {tool.input_summary ? (
                <p className="mt-2 text-muted-foreground">
                  {tool.input_summary}
                </p>
              ) : null}
              {tool.output_summary ? (
                <p className="mt-1 text-muted-foreground">
                  {tool.output_summary}
                </p>
              ) : null}
              {tool.safe_error_code ? (
                <p className="mt-1 text-destructive">{tool.safe_error_code}</p>
              ) : null}
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

function PromptCard({
  canOperate,
  instanceId,
  projectId,
}: {
  canOperate: boolean;
  instanceId: string;
  projectId: string;
}) {
  const prompt = useQuery({
    queryFn: () => agentApi.getPrompt(projectId, instanceId),
    queryKey: ["agent-prompt", projectId, instanceId],
  });
  const [content, setContent] = useState("");
  useEffect(() => {
    if (prompt.data) {
      setContent(prompt.data.effective_prompt);
    }
  }, [prompt.data]);
  const update = useMutation({
    mutationFn: () =>
      agentApi.updatePrompt(projectId, instanceId, content.trim()),
    onError: showError("Prompt 保存失败"),
    onSuccess: (result) => {
      setContent(result.effective_prompt);
      void prompt.refetch();
      toast.success("项目 Prompt 已更新");
    },
  });
  const reset = useMutation({
    mutationFn: () => agentApi.resetPrompt(projectId, instanceId),
    onError: showError("Prompt 恢复失败"),
    onSuccess: (result) => {
      setContent(result.effective_prompt);
      void prompt.refetch();
      toast.success("已恢复自动生成 Prompt");
    },
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">项目 Prompt</CardTitle>
        <CardDescription>
          默认内容由项目结构生成，可覆盖并随时恢复。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {prompt.isLoading ? (
          <p className="text-xs text-muted-foreground">正在生成 Prompt…</p>
        ) : (
          <textarea
            aria-label="项目 Agent Prompt"
            className="min-h-52 w-full resize-y rounded-md border border-input bg-background p-3 text-xs leading-5 outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            disabled={!canOperate}
            onChange={(event) => setContent(event.target.value)}
            value={content}
          />
        )}
        {prompt.data ? (
          <p className="text-xs text-muted-foreground">
            {prompt.data.custom
              ? "当前使用自定义 Prompt"
              : "当前使用自动生成 Prompt"}
          </p>
        ) : null}
        {canOperate ? (
          <div className="flex flex-wrap gap-2">
            <Button
              disabled={!content.trim() || update.isPending}
              onClick={() => update.mutate()}
              size="sm"
            >
              保存修改
            </Button>
            <Button
              disabled={!prompt.data?.custom || reset.isPending}
              onClick={() => reset.mutate()}
              size="sm"
              variant="outline"
            >
              恢复默认
            </Button>
          </div>
        ) : null}
        {prompt.data ? (
          <details className="text-xs text-muted-foreground">
            <summary className="cursor-pointer">查看自动生成版本</summary>
            <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3">
              {prompt.data.default_prompt}
            </pre>
          </details>
        ) : null}
      </CardContent>
    </Card>
  );
}

function UnconfiguredAgent({ projectId }: { projectId: string }) {
  return (
    <Card className="border-dashed">
      <CardContent className="flex flex-col items-center py-16 text-center">
        <Bot aria-hidden="true" className="size-10 text-muted-foreground" />
        <h1 className="mt-4 text-xl font-semibold">尚未配置 Hermes 连接</h1>
        <p className="mt-2 max-w-xl text-sm text-muted-foreground">
          选择 manual 获取一次性 Agent Token，或选择 auto 由服务端通过受控
          Dashboard 管理连接完成 MCP 安装与安全轮换。
        </p>
        <Link
          className="mt-5 inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground"
          href={`/projects/${encodeURIComponent(projectId)}/settings#agent-settings`}
        >
          <Settings aria-hidden="true" className="size-4" />
          配置 Hermes 连接
        </Link>
      </CardContent>
    </Card>
  );
}

function terminalStatusForEvent(
  event: AgentStreamEvent["event"],
): AgentRun["status"] | null {
  if (event === "run.completed" || event === "done") return "completed";
  if (event === "run.failed" || event === "error") return "failed";
  if (event === "run.stopped") return "stopped";
  return null;
}

function upsertToolCall(
  current: AgentToolCall[],
  toolCall: AgentToolCall,
): AgentToolCall[] {
  return [
    ...current.filter((item) => item.tool_call_id !== toolCall.tool_call_id),
    toolCall,
  ];
}

function mergeToolCalls(
  persisted: AgentToolCall[],
  streamed: AgentToolCall[],
): AgentToolCall[] {
  return streamed.reduce(upsertToolCall, persisted);
}

function enqueueApproval(
  current: AgentApproval[],
  approval: AgentApproval,
): AgentApproval[] {
  const existingIndex = current.findIndex(
    (item) => item.approval_id === approval.approval_id,
  );
  if (existingIndex < 0) {
    return [...current, approval];
  }
  return current.map((item, index) =>
    index === existingIndex ? approval : item,
  );
}

function removeApproval(
  current: AgentApproval[],
  approvalId: string,
): AgentApproval[] {
  return current.filter((item) => item.approval_id !== approvalId);
}

function showError(fallback: string) {
  return (error: unknown) =>
    toast.error(error instanceof Error ? error.message : fallback);
}
