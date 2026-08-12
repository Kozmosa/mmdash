"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  Brain,
  Bot,
  Check,
  ChevronLeft,
  CircleStop,
  ExternalLink,
  FileText,
  GitFork,
  Menu,
  MessageSquarePlus,
  MoreHorizontal,
  PanelRightClose,
  PanelRightOpen,
  Paperclip,
  Pencil,
  Play,
  RefreshCw,
  RotateCcw,
  SendHorizontal,
  Settings,
  Sparkles,
  Wrench,
  X,
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
import { MarkdownPreview } from "@/components/ui/markdown-preview";
import { cn } from "@/lib/cn";
import { artifactApi } from "@/features/artifact/artifact-api";
import { ArtifactDetailDrawer } from "@/features/artifact/artifact-detail-drawer";
import { MultipartUploadTask } from "@/features/artifact/multipart-upload";

import { agentApi, streamAgentRun } from "./agent-api";
import {
  ContextProposalReviewPanel,
  contextProposalQueryKey,
} from "./context-proposal-review";
import type {
  AgentApproval,
  AgentApprovalChoice,
  AgentChatAttachment,
  AgentInstance,
  AgentMessage,
  AgentReasoningEffort,
  AgentRun,
  AgentRunLaunch,
  AgentSession,
  AgentStreamEvent,
  AgentToolCall,
} from "./types";

const terminalStatuses = new Set<AgentRun["status"]>([
  "completed",
  "failed",
  "stopped",
]);

function sessionSidebarStorageKey(projectId: string, instanceId: string) {
  return `mmdash.agent.session-sidebar.${projectId}.${instanceId}`;
}

type ReasoningEffortPreference = "auto" | AgentReasoningEffort;

const reasoningEffortOptions: Array<{
  label: string;
  value: ReasoningEffortPreference;
}> = [
  { label: "自动", value: "auto" },
  { label: "关闭", value: "none" },
  { label: "最低", value: "minimal" },
  { label: "低", value: "low" },
  { label: "中", value: "medium" },
  { label: "高", value: "high" },
  { label: "超高", value: "xhigh" },
  { label: "最大", value: "max" },
  { label: "极致", value: "ultra" },
];

function reasoningEffortStorageKey(projectId: string, instanceId: string) {
  return `mmdash.agent.reasoning-effort.${projectId}.${instanceId}`;
}

type ComposerAttachment = AgentChatAttachment & {
  key: string;
  progress: number;
  status: "uploading" | "completed" | "failed";
  error?: string;
};

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
    const requestedInstanceId = new URL(window.location.href).searchParams.get(
      "agent",
    );
    const preferred =
      instances.data.items.find(
        (candidate) => candidate.agent_instance_id === requestedInstanceId,
      ) ??
      instances.data.items.find((candidate) => candidate.status === "active") ??
      instances.data.items.find(
        (candidate) => candidate.status !== "disabled",
      ) ??
      instances.data.items[0];
    setInstanceId(preferred?.agent_instance_id ?? null);
  }, [instanceId, instances.data?.items]);

  useEffect(() => {
    if (!instanceId) return;
    const url = new URL(window.location.href);
    url.searchParams.set("agent", instanceId);
    window.history.replaceState(window.history.state, "", url);
  }, [instanceId]);

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
  const [sessionSidebarOpen, setSessionSidebarOpen] = useState(true);
  const [contextPanelOpen, setContextPanelOpen] = useState(true);
  const [creatingSession, setCreatingSession] = useState(false);
  const [newSessionTitle, setNewSessionTitle] = useState("");
  const [composer, setComposer] = useState("");
  const [reasoningEffort, setReasoningEffort] =
    useState<ReasoningEffortPreference>("auto");
  const [composerAttachments, setComposerAttachments] = useState<
    ComposerAttachment[]
  >([]);
  const [detailArtifactId, setDetailArtifactId] = useState<string>();
  const [optimisticUser, setOptimisticUser] = useState<AgentMessage | null>(
    null,
  );
  const [streamDraft, setStreamDraft] = useState<AgentMessage | null>(null);
  const [streamTools, setStreamTools] = useState<AgentToolCall[]>([]);
  const [activeRun, setActiveRun] = useState<AgentRun | null>(null);
  const [approvals, setApprovals] = useState<AgentApproval[]>([]);
  const [streamError, setStreamError] = useState<string | null>(null);
  const [settledStreamRunId, setSettledStreamRunId] = useState<string | null>(
    null,
  );
  const [reasoningState, setReasoningState] = useState<
    "idle" | "active" | "complete"
  >("idle");
  const streamAbort = useRef<AbortController | null>(null);
  const composerRef = useRef<HTMLTextAreaElement | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const uploadTasks = useRef(new Map<string, MultipartUploadTask>());
  const messageScrollRef = useRef<HTMLDivElement | null>(null);
  const streamGeneration = useRef(0);
  const streamRunId = useRef<string | null>(null);

  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const wideViewport = window.matchMedia("(min-width: 1280px)");
    const syncContextPanel = () => setContextPanelOpen(wideViewport.matches);
    syncContextPanel();
    wideViewport.addEventListener("change", syncContextPanel);
    return () => wideViewport.removeEventListener("change", syncContextPanel);
  }, []);
  const lastEventId = useRef<string | undefined>(undefined);
  const completionTimers = useRef<number[]>([]);

  useEffect(() => {
    if (!instanceId) return;
    const saved = window.localStorage.getItem(
      sessionSidebarStorageKey(projectId, instanceId),
    );
    setSessionSidebarOpen(saved === null ? true : saved === "true");
    const savedReasoningEffort = window.localStorage.getItem(
      reasoningEffortStorageKey(projectId, instanceId),
    );
    setReasoningEffort(
      isReasoningEffortPreference(savedReasoningEffort)
        ? savedReasoningEffort
        : "auto",
    );
  }, [instanceId, projectId]);

  const sessions = useQuery({
    enabled: Boolean(instanceId),
    queryFn: () => agentApi.listSessions(projectId, instanceId!),
    queryKey: ["agent-sessions", projectId, instanceId],
    refetchInterval: 2_000,
    refetchIntervalInBackground: true,
    refetchOnWindowFocus: "always",
  });
  const userSessions = useMemo(
    () =>
      (sessions.data?.items ?? []).filter(
        (session) => session.session_type === "main",
      ),
    [sessions.data?.items],
  );
  const selectedSession = useMemo(
    () =>
      userSessions.find((session) => session.session_id === sessionId) ?? null,
    [sessionId, userSessions],
  );
  const messages = useQuery({
    enabled: Boolean(instanceId && sessionId),
    queryFn: () => agentApi.listMessages(projectId, instanceId!, sessionId!),
    queryKey: ["agent-messages", projectId, instanceId, sessionId],
    refetchInterval: 2_000,
    refetchIntervalInBackground: true,
    refetchOnWindowFocus: "always",
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
    refetchInterval: (query) => {
      const latest = query.state.data;
      return latest && !terminalStatuses.has(latest.status) ? 2_000 : false;
    },
    refetchIntervalInBackground: true,
    refetchOnWindowFocus: "always",
  });
  const run = activeRun ?? persistedRun.data ?? null;

  useEffect(() => {
    if (!userSessions.length) {
      streamGeneration.current += 1;
      streamAbort.current?.abort();
      streamAbort.current = null;
      streamRunId.current = null;
      setApprovals([]);
      setSessionId(null);
      return;
    }
    if (userSessions.some((session) => session.session_id === sessionId)) {
      return;
    }
    const preferred =
      userSessions.find(
        (session) =>
          session.session_id ===
          new URL(window.location.href).searchParams.get("session"),
      ) ??
      userSessions.find(
        (session) => session.default && session.status === "active",
      ) ??
      userSessions.find(
        (session) =>
          session.session_type === "main" && session.status === "active",
      ) ??
      userSessions.find((session) => session.status === "active") ??
      userSessions[0];
    streamGeneration.current += 1;
    streamAbort.current?.abort();
    streamAbort.current = null;
    streamRunId.current = null;
    setApprovals([]);
    setSessionId(preferred?.session_id ?? null);
  }, [sessionId, userSessions]);

  useEffect(() => {
    if (!sessionId) return;
    const url = new URL(window.location.href);
    url.searchParams.set("session", sessionId);
    window.history.replaceState(window.history.state, "", url);
  }, [sessionId]);

  useEffect(
    () => () => {
      streamGeneration.current += 1;
      streamAbort.current?.abort();
      completionTimers.current.forEach((timer) => window.clearTimeout(timer));
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
    // Hermes can make its message-history projection visible shortly after
    // the terminal SSE event. Keep the safe streamed draft on screen and
    // converge the persisted query again instead of flashing it away.
    completionTimers.current.forEach((timer) => window.clearTimeout(timer));
    completionTimers.current = [
      window.setTimeout(() => void messages.refetch(), 750),
      window.setTimeout(() => void messages.refetch(), 2_000),
    ];
  }, [instanceId, messages, projectId, queryClient, sessions]);

  const handleStreamEvent = useCallback(
    async (event: AgentStreamEvent) => {
      lastEventId.current = event.event_id;
      if (event.run) {
        setActiveRun(event.run);
      }
      if (event.event !== "error") {
        setStreamError(null);
      }
      if (event.event === "reasoning.available") {
        setReasoningState("active");
      }
      if (event.event === "message.started") {
        setStreamDraft({
          content: "",
          message_id: event.message_id ?? `stream-${event.event_id}`,
          role: "assistant",
        });
      } else if (event.event === "message.delta" && event.delta) {
        setReasoningState("complete");
        setStreamDraft((current) => ({
          content: mergeStreamText(current?.content ?? "", event.delta!),
          message_id:
            event.message_id ??
            current?.message_id ??
            `stream-${event.event_id}`,
          role: "assistant",
        }));
      } else if (event.event === "message.completed" && event.delta) {
        setReasoningState("complete");
        setStreamDraft({
          content: event.delta,
          message_id: event.message_id ?? `stream-${event.event_id}`,
          role: "assistant",
        });
      } else if (event.event === "run.completed" && event.delta) {
        setReasoningState("complete");
        setStreamDraft((current) =>
          current?.content
            ? current
            : {
                content: event.delta!,
                message_id:
                  event.message_id ??
                  current?.message_id ??
                  `stream-${event.event_id}`,
                role: "assistant",
              },
        );
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
        setSettledStreamRunId(event.run_id);
        setReasoningState((current) =>
          current === "idle" ? current : "complete",
        );
        streamGeneration.current += 1;
        streamRunId.current = null;
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
        setStreamError("回复流暂时中断，正在自动重连…");
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
      streamRunId.current = launch.run.run_id;
      lastEventId.current = undefined;
      setActiveRun(launch.run);
      setStreamDraft(null);
      setStreamTools(launch.run.tool_calls);
      setApprovals([]);
      setStreamError(null);
      setReasoningState("idle");
      setSettledStreamRunId(null);
      cacheSession(launch.session);

      void (async () => {
        let reconnectAttempt = 0;
        try {
          while (
            !controller.signal.aborted &&
            streamGeneration.current === generation
          ) {
            try {
              lastEventId.current = await streamAgentRun(
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
            } catch {
              if (
                controller.signal.aborted ||
                streamGeneration.current !== generation
              ) {
                return;
              }
            }

            if (
              controller.signal.aborted ||
              streamGeneration.current !== generation
            ) {
              return;
            }

            try {
              const latest = await agentApi.getRun(
                projectId,
                instanceId!,
                launch.session.session_id,
                launch.run.run_id,
              );
              setActiveRun(latest);
              if (terminalStatuses.has(latest.status)) {
                setSettledStreamRunId(latest.run_id);
                setStreamError(
                  latest.status === "failed"
                    ? "Agent 回复失败，请重试。"
                    : null,
                );
                await finishRun();
                return;
              }
            } catch {
              // A failed status probe is recoverable while the stream reconnects.
            }

            setStreamError("回复流暂时中断，正在自动重连…");
            reconnectAttempt += 1;
            await waitForAgentReconnect(
              Math.min(250 * 2 ** Math.min(reconnectAttempt - 1, 4), 4_000),
              controller.signal,
            );
          }
        } finally {
          if (streamGeneration.current === generation) {
            streamAbort.current = null;
            streamRunId.current = null;
          }
        }
      })();
    },
    [cacheSession, finishRun, handleStreamEvent, instanceId, projectId],
  );

  useEffect(() => {
    const resumableRun = persistedRun.data;
    if (
      !resumableRun ||
      !selectedSession ||
      terminalStatuses.has(resumableRun.status) ||
      (activeRun?.run_id === resumableRun.run_id &&
        terminalStatuses.has(activeRun.status)) ||
      streamRunId.current === resumableRun.run_id
    ) {
      return;
    }
    beginStream({ run: resumableRun, session: selectedSession });
  }, [activeRun, beginStream, persistedRun.data, selectedSession]);

  const createSession = useMutation({
    mutationFn: () =>
      agentApi.createSession(projectId, instanceId!, {
        default: !userSessions.length,
        session_type: "main",
        title: newSessionTitle.trim(),
      }),
    onError: showError("Session 创建失败"),
    onSuccess: (session) => {
      cacheSession(session);
      chooseSession(session.session_id);
      setNewSessionTitle("");
      setCreatingSession(false);
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
        chooseSession(session.session_id);
      }
      void sessions.refetch();
      toast.success("Session 已更新");
    },
  });

  const startRun = useMutation({
    mutationFn: ({
      artifactIds,
      message,
      reasoningEffort,
    }: {
      artifactIds: string[];
      message: string;
      reasoningEffort?: AgentReasoningEffort;
    }) => {
      const base = [
        projectId,
        instanceId!,
        selectedSession!.session_id,
        message,
      ] as const;
      if (reasoningEffort) {
        return agentApi.startRun(...base, artifactIds, reasoningEffort);
      }
      return artifactIds.length
        ? agentApi.startRun(...base, artifactIds)
        : agentApi.startRun(...base);
    },
    onError: (error) => {
      setOptimisticUser(null);
      showError("消息发送失败")(error);
    },
    onSuccess: (launch) => {
      for (const attachment of composerAttachments) {
        if (attachment.local_url) URL.revokeObjectURL(attachment.local_url);
      }
      setComposerAttachments([]);
      beginStream(launch);
    },
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
    onSuccess: (launch) => {
      if (launch.session.session_id !== selectedSession?.session_id) {
        chooseSession(launch.session.session_id);
      }
      beginStream(launch);
    },
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

  function chooseSession(nextSessionId: string) {
    streamGeneration.current += 1;
    streamAbort.current?.abort();
    streamAbort.current = null;
    streamRunId.current = null;
    setActiveRun(null);
    setStreamDraft(null);
    setStreamTools([]);
    setOptimisticUser(null);
    setApprovals([]);
    setStreamError(null);
    setReasoningState("idle");
    setSettledStreamRunId(null);
    setDetailArtifactId(undefined);
    setSessionId(nextSessionId);
  }

  function toggleSessionSidebar() {
    setSessionSidebarOpen((current) => {
      const next = !current;
      if (instanceId) {
        window.localStorage.setItem(
          sessionSidebarStorageKey(projectId, instanceId),
          String(next),
        );
      }
      return next;
    });
  }

  const chooseInstance = (nextInstanceId: string) => {
    streamGeneration.current += 1;
    streamAbort.current?.abort();
    streamAbort.current = null;
    streamRunId.current = null;
    setSessionId(null);
    setActiveRun(null);
    setStreamDraft(null);
    setStreamTools([]);
    setApprovals([]);
    setStreamError(null);
    setReasoningState("idle");
    setSettledStreamRunId(null);
    setDetailArtifactId(undefined);
    onInstanceChange(nextInstanceId);
  };

  const toolCalls = mergeToolCalls(run?.tool_calls ?? [], streamTools);
  const runBusy = Boolean(run && !terminalStatuses.has(run.status));
  const currentMessages = prepareMessagesForDisplay(
    messages.data?.items ?? [],
    new Set(runBusy ? toolCalls.map((toolCall) => toolCall.tool_call_id) : []),
    runBusy && settledStreamRunId !== run?.run_id,
    run?.run_id,
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
  const hasDraftMessage = Boolean(
    streamDraft &&
    !streamDraftPersisted &&
    !currentMessages.some(
      (message) => message.message_id === streamDraft.message_id,
    ),
  );
  const hasOptimisticUser = Boolean(
    optimisticUser &&
    !currentMessages.some(
      (message) =>
        message.role === "user" &&
        message.content.trim() === optimisticUser.content.trim(),
    ),
  );
  const approval = approvals[0] ?? null;
  const canChat =
    canOperate &&
    instance?.status === "active" &&
    selectedSession?.status === "active";

  useEffect(() => {
    if (streamDraftPersisted) setStreamDraft(null);
  }, [streamDraftPersisted]);

  useEffect(() => {
    const textarea = composerRef.current;
    if (!textarea) return;
    textarea.style.height = "auto";
    textarea.style.height = `${Math.min(Math.max(textarea.scrollHeight, 56), 160)}px`;
  }, [composer]);

  useEffect(() => {
    const viewport = messageScrollRef.current;
    if (!viewport) return;
    viewport.scrollTop = viewport.scrollHeight;
  }, [
    approval,
    currentMessages.length,
    streamDraft?.content,
    toolCalls.length,
  ]);

  const uploadFiles = (files: FileList | null) => {
    if (!files?.length || !canChat || runBusy) return;
    for (const file of Array.from(files).slice(
      0,
      10 - composerAttachments.length,
    )) {
      const key =
        typeof crypto !== "undefined" && "randomUUID" in crypto
          ? crypto.randomUUID()
          : `${Date.now()}-${Math.random()}`;
      const localUrl = file.type.startsWith("image/")
        ? URL.createObjectURL(file)
        : undefined;
      const initial: ComposerAttachment = {
        artifact_id: "",
        created_at: new Date().toISOString(),
        direction: "input",
        filename: file.name,
        key,
        local_url: localUrl,
        mime_type: file.type || "application/octet-stream",
        name: file.name,
        progress: 0,
        run_id: "",
        size_bytes: file.size,
        status: "uploading",
        version_id: "",
      };
      setComposerAttachments((current) => [...current, initial]);
      const task = new MultipartUploadTask({
        file,
        kind: "attachment",
        name: file.name,
        projectId,
        tags: ["agent-chat"],
      });
      uploadTasks.current.set(key, task);
      task.subscribe((snapshot) => {
        setComposerAttachments((current) =>
          current.map((attachment) =>
            attachment.key === key
              ? { ...attachment, progress: snapshot.progress }
              : attachment,
          ),
        );
      });
      void task
        .start()
        .then((detail) => {
          setComposerAttachments((current) =>
            current.map((attachment) =>
              attachment.key === key
                ? {
                    ...attachment,
                    artifact_id: detail.artifact.artifact_id,
                    status: "completed",
                    version_id: detail.current_version?.version_id ?? "",
                  }
                : attachment,
            ),
          );
        })
        .catch((error) => {
          setComposerAttachments((current) =>
            current.map((attachment) =>
              attachment.key === key
                ? {
                    ...attachment,
                    error: error instanceof Error ? error.message : "上传失败",
                    status: "failed",
                  }
                : attachment,
            ),
          );
        })
        .finally(() => uploadTasks.current.delete(key));
    }
    if (fileInputRef.current) fileInputRef.current.value = "";
  };

  const removeComposerAttachment = (key: string) => {
    const task = uploadTasks.current.get(key);
    if (task) void task.cancel().catch(() => undefined);
    setComposerAttachments((current) => {
      const removed = current.find((attachment) => attachment.key === key);
      if (removed?.local_url) URL.revokeObjectURL(removed.local_url);
      return current.filter((attachment) => attachment.key !== key);
    });
  };

  const submitMessage = () => {
    const readyAttachments = composerAttachments.filter(
      (attachment) => attachment.status === "completed",
    );
    const hasPendingAttachment = composerAttachments.some(
      (attachment) => attachment.status === "uploading",
    );
    const content =
      composer.trim() ||
      (readyAttachments.length ? "请查看随消息上传的附件。" : "");
    if (
      !content ||
      hasPendingAttachment ||
      !canChat ||
      runBusy ||
      startRun.isPending
    )
      return;
    setOptimisticUser({
      attachments: readyAttachments,
      content,
      message_id: `optimistic-${Date.now()}`,
      role: "user",
    });
    setComposer("");
    startRun.mutate({
      artifactIds: readyAttachments.map((attachment) => attachment.artifact_id),
      message: content,
      reasoningEffort: reasoningEffort === "auto" ? undefined : reasoningEffort,
    });
  };

  const scrollMessages = (edge: "top" | "bottom") => {
    const viewport = messageScrollRef.current;
    if (!viewport) return;
    const top = edge === "top" ? 0 : viewport.scrollHeight;
    if (typeof viewport.scrollTo === "function") {
      viewport.scrollTo({ behavior: "smooth", top });
    } else {
      viewport.scrollTop = top;
    }
  };

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-background">
      <AgentHeader
        contextPanelOpen={contextPanelOpen}
        instance={instance}
        instanceId={instanceId}
        instances={instances}
        onInstanceChange={chooseInstance}
        onToggleContext={() => setContextPanelOpen((current) => !current)}
        onToggleSessions={toggleSessionSidebar}
        projectId={projectId}
        sessionSidebarOpen={sessionSidebarOpen}
      />

      <div className="relative flex min-h-0 flex-1 overflow-hidden border-t border-border">
        <SessionSidebar
          canOperate={canOperate}
          createPending={createSession.isPending}
          creating={creatingSession}
          instanceReady={instance?.status === "active"}
          newSessionTitle={newSessionTitle}
          onCancelCreate={() => {
            setCreatingSession(false);
            setNewSessionTitle("");
          }}
          onCreate={() => createSession.mutate()}
          onStartCreate={() => setCreatingSession(true)}
          onSelect={chooseSession}
          open={sessionSidebarOpen}
          selectedSessionId={sessionId}
          sessionPending={mutateSession.isPending}
          sessions={userSessions}
          setNewSessionTitle={setNewSessionTitle}
          updateSession={(action) => mutateSession.mutate(action)}
        />

        <main
          className={cn(
            "flex min-h-0 min-w-0 flex-1 flex-col bg-background transition-[margin] duration-200",
            contextPanelOpen ? "xl:mr-[380px]" : null,
          )}
        >
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
              pending={replay.isPending}
              run={run}
            />
          </div>

          <div className="relative min-h-0 flex-1 overflow-hidden">
            <div
              aria-label="Agent 消息历史"
              className="absolute inset-0 overflow-y-auto overscroll-contain px-4 py-6"
              ref={messageScrollRef}
            >
              <div className="mx-auto flex w-full max-w-3xl flex-col gap-5">
                {messages.isLoading ? (
                  <p className="text-sm text-muted-foreground">
                    正在读取 Hermes 消息历史…
                  </p>
                ) : null}
                {!messages.isLoading &&
                selectedSession &&
                currentMessages.length === 0 &&
                !hasOptimisticUser ? (
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
                  <MessageBubble
                    key={message.message_id}
                    message={message}
                    onOpenArtifact={setDetailArtifactId}
                    projectId={projectId}
                  />
                ))}
                {hasOptimisticUser && optimisticUser ? (
                  <MessageBubble
                    message={optimisticUser}
                    onOpenArtifact={setDetailArtifactId}
                    optimistic
                    projectId={projectId}
                  />
                ) : null}
                <RunActivity
                  reasoningState={reasoningState}
                  runBusy={runBusy}
                  toolCalls={toolCalls}
                />
                {hasDraftMessage && streamDraft ? (
                  <MessageBubble
                    message={streamDraft}
                    onOpenArtifact={setDetailArtifactId}
                    projectId={projectId}
                    streaming={runBusy}
                  />
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
            </div>
            <div className="absolute bottom-3 right-3 flex flex-col gap-1">
              <Button
                aria-label="滚动到聊天顶部"
                className="rounded-full bg-background/90 shadow"
                onClick={() => scrollMessages("top")}
                size="icon"
                variant="outline"
              >
                <ArrowUp aria-hidden="true" className="size-4" />
              </Button>
              <Button
                aria-label="滚动到最新消息"
                className="rounded-full bg-background/90 shadow"
                onClick={() => scrollMessages("bottom")}
                size="icon"
                variant="outline"
              >
                <ArrowDown aria-hidden="true" className="size-4" />
              </Button>
            </div>
          </div>

          <form
            className="shrink-0 border-t border-border bg-background px-4 py-3"
            onSubmit={(event) => {
              event.preventDefault();
              submitMessage();
            }}
          >
            <div className="mx-auto w-full max-w-3xl rounded-[26px] border border-input bg-card px-3 py-2 shadow-sm focus-within:ring-2 focus-within:ring-ring">
              <input
                className="hidden"
                disabled={!canChat || runBusy}
                multiple
                onChange={(event) => uploadFiles(event.target.files)}
                ref={fileInputRef}
                type="file"
              />
              {composerAttachments.length ? (
                <div className="flex flex-wrap gap-2 px-1 pb-2">
                  {composerAttachments.map((attachment) => (
                    <ComposerAttachmentChip
                      attachment={attachment}
                      key={attachment.key}
                      onRemove={() => removeComposerAttachment(attachment.key)}
                    />
                  ))}
                </div>
              ) : null}
              <label className="sr-only" htmlFor="agent-message">
                发给 Hermes 的消息
              </label>
              <textarea
                className="block min-h-14 max-h-40 w-full resize-none overflow-y-auto bg-transparent px-2 py-2 text-sm leading-6 outline-none placeholder:text-muted-foreground disabled:opacity-50"
                disabled={!canChat}
                id="agent-message"
                onChange={(event) => setComposer(event.target.value)}
                onKeyDown={(event) => {
                  if (
                    event.key === "Enter" &&
                    !event.shiftKey &&
                    !event.nativeEvent.isComposing
                  ) {
                    event.preventDefault();
                    submitMessage();
                  }
                }}
                placeholder={
                  selectedSession?.status === "ended"
                    ? "继续 Session 后才能发送消息"
                    : instance?.status !== "active"
                      ? "完成 Hermes 与 MCP 验证后才能对话"
                      : "向 Hermes 发送项目问题…"
                }
                ref={composerRef}
                value={composer}
              />
              <div className="flex items-center justify-between gap-3 px-1 pb-1">
                <div className="flex items-center gap-1.5">
                  <Button
                    aria-label="上传文件"
                    className="rounded-full"
                    disabled={
                      !canChat || runBusy || composerAttachments.length >= 10
                    }
                    onClick={() => fileInputRef.current?.click()}
                    size="icon"
                    type="button"
                    variant="ghost"
                  >
                    <Paperclip aria-hidden="true" className="size-4" />
                  </Button>
                  <label className="flex items-center gap-1 text-[11px] text-muted-foreground">
                    <Brain aria-hidden="true" className="size-3.5" />
                    <span className="sr-only">思考强度</span>
                    <select
                      aria-label="思考强度"
                      className="max-w-24 bg-transparent py-1 outline-none"
                      disabled={runBusy}
                      onChange={(event) => {
                        const value = event.target
                          .value as ReasoningEffortPreference;
                        setReasoningEffort(value);
                        if (instanceId) {
                          window.localStorage.setItem(
                            reasoningEffortStorageKey(projectId, instanceId),
                            value,
                          );
                        }
                      }}
                      value={reasoningEffort}
                    >
                      {reasoningEffortOptions.map((option) => (
                        <option key={option.value} value={option.value}>
                          思考：{option.label}
                        </option>
                      ))}
                    </select>
                  </label>
                  <p className="text-[11px] text-muted-foreground">
                    Enter 发送 · Shift+Enter 换行
                  </p>
                </div>
                {runBusy ? (
                  <Button
                    aria-label="停止输出"
                    className="rounded-full"
                    disabled={stop.isPending || run?.status === "stopping"}
                    onClick={() => stop.mutate()}
                    size="icon"
                    type="button"
                  >
                    <CircleStop aria-hidden="true" className="size-4" />
                  </Button>
                ) : (
                  <Button
                    aria-label="发送"
                    className="rounded-full"
                    disabled={
                      (!composer.trim() &&
                        !composerAttachments.some(
                          (attachment) => attachment.status === "completed",
                        )) ||
                      composerAttachments.some(
                        (attachment) => attachment.status === "uploading",
                      ) ||
                      !canChat ||
                      startRun.isPending
                    }
                    size="icon"
                    type="submit"
                  >
                    <SendHorizontal aria-hidden="true" className="size-4" />
                  </Button>
                )}
              </div>
            </div>
          </form>
        </main>

        {contextPanelOpen ? (
          <aside className="absolute inset-y-3 right-3 z-20 w-[min(360px,calc(100%-1.5rem))] overflow-y-auto overscroll-contain rounded-2xl border border-border bg-background/95 p-3 shadow-xl backdrop-blur xl:w-[360px]">
            <div className="mb-2 flex items-center justify-between px-1">
              <h2 className="text-sm font-semibold">项目上下文</h2>
              <Button
                aria-label="关闭项目上下文"
                onClick={() => setContextPanelOpen(false)}
                size="icon"
                variant="ghost"
              >
                <X aria-hidden="true" className="size-4" />
              </Button>
            </div>
            <div className="space-y-3">
              <RunStatusCard run={run} />
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
            </div>
          </aside>
        ) : null}
        <ArtifactDetailDrawer
          artifactId={detailArtifactId}
          onClose={() => setDetailArtifactId(undefined)}
          projectId={projectId}
          trashView={false}
        />
      </div>
    </div>
  );
}

function AgentHeader({
  contextPanelOpen,
  instance,
  instanceId,
  instances,
  onInstanceChange,
  onToggleContext,
  onToggleSessions,
  projectId,
  sessionSidebarOpen,
}: {
  contextPanelOpen: boolean;
  instance: AgentInstance | null;
  instanceId: string | null;
  instances: AgentInstance[];
  onInstanceChange: (instanceId: string) => void;
  onToggleContext: () => void;
  onToggleSessions: () => void;
  projectId: string;
  sessionSidebarOpen: boolean;
}) {
  return (
    <header className="flex h-16 shrink-0 items-center justify-between gap-3 px-3 md:px-4">
      <div className="flex min-w-0 items-center gap-2">
        <Button
          aria-label={sessionSidebarOpen ? "收起会话列表" : "展开会话列表"}
          onClick={onToggleSessions}
          size="icon"
          variant="ghost"
        >
          {sessionSidebarOpen ? (
            <ChevronLeft aria-hidden="true" className="size-4" />
          ) : (
            <Menu aria-hidden="true" className="size-4" />
          )}
        </Button>
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-border bg-card">
          <Bot aria-hidden="true" className="size-4" />
        </span>
        <div>
          <h1 className="truncate text-base font-semibold">mmdash Agent</h1>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Badge>{instance?.status ?? "unavailable"}</Badge>
            <span className="hidden sm:inline">
              {instance?.management_mode ?? "—"}
            </span>
            <span className="hidden sm:inline">
              MCP {instance?.grant.project_access_status ?? "pending"}
            </span>
          </div>
        </div>
      </div>
      <div className="flex min-w-0 items-center gap-1.5">
        {instances.length > 1 ? (
          <label className="text-sm">
            <span className="sr-only">Agent 实例</span>
            <select
              className="h-9 max-w-36 rounded-md border border-input bg-background px-3 text-sm"
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
          aria-label="Agent 设置"
          className="inline-flex size-9 items-center justify-center rounded-md border border-border"
          href={`/projects/${encodeURIComponent(projectId)}/settings#agent-settings`}
        >
          <Settings aria-hidden="true" className="size-4" />
        </Link>
        <Button
          aria-label={contextPanelOpen ? "关闭项目上下文" : "打开项目上下文"}
          onClick={onToggleContext}
          size="icon"
          variant="outline"
        >
          {contextPanelOpen ? (
            <PanelRightClose aria-hidden="true" className="size-4" />
          ) : (
            <PanelRightOpen aria-hidden="true" className="size-4" />
          )}
        </Button>
      </div>
    </header>
  );
}

function SessionSidebar({
  canOperate,
  createPending,
  creating,
  instanceReady,
  newSessionTitle,
  onCancelCreate,
  onCreate,
  onStartCreate,
  onSelect,
  open,
  selectedSessionId,
  sessionPending,
  sessions,
  setNewSessionTitle,
  updateSession,
}: {
  canOperate: boolean;
  createPending: boolean;
  creating: boolean;
  instanceReady: boolean;
  newSessionTitle: string;
  onCancelCreate: () => void;
  onCreate: () => void;
  onStartCreate: () => void;
  onSelect: (sessionId: string) => void;
  open: boolean;
  selectedSessionId: string | null;
  sessionPending: boolean;
  sessions: AgentSession[];
  setNewSessionTitle: (title: string) => void;
  updateSession: (action: {
    kind: "rename" | "fork" | "end" | "continue" | "default";
    title?: string;
  }) => void;
}) {
  const [menu, setMenu] = useState<{
    session: AgentSession;
    x: number;
    y: number;
  } | null>(null);

  useEffect(() => {
    if (!menu) return;
    const close = () => setMenu(null);
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    window.addEventListener("pointerdown", close);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("pointerdown", close);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [menu]);

  if (!open) return null;

  const runAction = (
    session: AgentSession,
    action: "rename" | "fork" | "end" | "continue" | "default",
  ) => {
    if (selectedSessionId !== session.session_id) onSelect(session.session_id);
    if (action === "rename") {
      const title = window.prompt("Session 新名称", session.title)?.trim();
      if (title && title !== session.title)
        updateSession({ kind: action, title });
    } else if (action === "fork") {
      const title = window
        .prompt("分叉 Session 名称", `${session.title}（分叉）`)
        ?.trim();
      if (title) updateSession({ kind: action, title });
    } else if (
      action !== "end" ||
      window.confirm("结束这个 Session？消息历史不会删除。")
    ) {
      updateSession({ kind: action });
    }
    setMenu(null);
  };

  return (
    <aside className="flex w-72 shrink-0 flex-col border-r border-border bg-card/50 p-3">
      <div className="flex items-center justify-between gap-2 px-1">
        <div className="flex items-center gap-2">
          <h2 className="font-semibold">会话</h2>
          <Badge>{sessions.length}</Badge>
        </div>
        {canOperate && !creating ? (
          <Button
            aria-label="新建会话"
            disabled={!instanceReady}
            onClick={onStartCreate}
            size="icon"
            variant="ghost"
          >
            <MessageSquarePlus aria-hidden="true" className="size-4" />
          </Button>
        ) : null}
      </div>
      {canOperate && creating ? (
        <form
          className="mt-3 space-y-2 rounded-lg border border-border bg-background p-2"
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
            autoFocus
            placeholder="会话名称"
            value={newSessionTitle}
          />
          <div className="flex justify-end gap-1">
            <Button
              onClick={onCancelCreate}
              size="sm"
              type="button"
              variant="ghost"
            >
              取消
            </Button>
            <Button
              disabled={
                !newSessionTitle.trim() || !instanceReady || createPending
              }
              size="sm"
              type="submit"
            >
              创建
            </Button>
          </div>
        </form>
      ) : null}
      <ul className="mt-3 min-h-0 flex-1 space-y-1 overflow-y-auto overscroll-contain">
        {sessions.map((session) => (
          <li className="group relative" key={session.session_id}>
            <button
              aria-current={
                selectedSessionId === session.session_id ? "true" : undefined
              }
              className={cn(
                "w-full rounded-lg px-3 py-2 pr-9 text-left text-sm transition-colors",
                selectedSessionId === session.session_id
                  ? "bg-accent text-accent-foreground"
                  : "hover:bg-accent/60",
              )}
              onClick={() => onSelect(session.session_id)}
              onContextMenu={(event) => {
                event.preventDefault();
                onSelect(session.session_id);
                setMenu({ session, x: event.clientX, y: event.clientY });
              }}
              type="button"
            >
              <span className="flex items-center justify-between gap-2">
                <span className="truncate font-medium">{session.title}</span>
                {session.default ? (
                  <Check aria-label="默认 Session" className="size-3" />
                ) : null}
              </span>
              <span className="mt-1 block text-xs text-muted-foreground">
                {session.status}
                {session.default ? " · 默认" : ""}
              </span>
            </button>
            {canOperate ? (
              <Button
                aria-label={`打开 ${session.title} 会话菜单`}
                className="absolute right-1 top-1.5 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100"
                onClick={(event) => {
                  event.stopPropagation();
                  const bounds = event.currentTarget.getBoundingClientRect();
                  setMenu({ session, x: bounds.right, y: bounds.bottom });
                }}
                size="icon"
                variant="ghost"
              >
                <MoreHorizontal aria-hidden="true" className="size-4" />
              </Button>
            ) : null}
          </li>
        ))}
      </ul>
      {menu && canOperate ? (
        <div
          aria-label="会话操作"
          className="fixed z-50 min-w-40 rounded-lg border border-border bg-popover p-1 text-popover-foreground shadow-lg"
          onClick={(event) => event.stopPropagation()}
          onPointerDown={(event) => event.stopPropagation()}
          role="menu"
          style={{
            left: Math.min(menu.x, window.innerWidth - 180),
            top: Math.min(menu.y, window.innerHeight - 220),
          }}
        >
          <SessionMenuItem
            disabled={sessionPending}
            icon={Pencil}
            label="重命名"
            onClick={() => runAction(menu.session, "rename")}
          />
          <SessionMenuItem
            disabled={sessionPending || menu.session.default}
            icon={Check}
            label="设为默认"
            onClick={() => runAction(menu.session, "default")}
          />
          <SessionMenuItem
            disabled={sessionPending}
            icon={GitFork}
            label="分叉"
            onClick={() => runAction(menu.session, "fork")}
          />
          <SessionMenuItem
            disabled={sessionPending}
            icon={menu.session.status === "active" ? CircleStop : Play}
            label={menu.session.status === "active" ? "结束" : "继续"}
            onClick={() =>
              runAction(
                menu.session,
                menu.session.status === "active" ? "end" : "continue",
              )
            }
          />
        </div>
      ) : null}
    </aside>
  );
}

function SessionMenuItem({
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
    <Button
      className="w-full justify-start"
      disabled={disabled}
      onClick={onClick}
      role="menuitem"
      size="sm"
      variant="ghost"
    >
      <Icon aria-hidden="true" className="size-3" />
      {label}
    </Button>
  );
}

export function MessageBubble({
  message,
  onOpenArtifact,
  optimistic,
  projectId,
  streaming,
}: {
  message: AgentMessage;
  onOpenArtifact: (artifactId: string) => void;
  optimistic?: boolean;
  projectId: string;
  streaming?: boolean;
}) {
  const user = message.role === "user";
  return (
    <div className={cn("flex", user ? "justify-end" : "justify-start")}>
      <div
        className={cn(
          "min-w-0 max-w-[88%] text-sm",
          user ? "rounded-2xl bg-muted px-4 py-3" : "w-full",
          optimistic ? "opacity-70" : null,
        )}
      >
        {!user ? (
          <p className="mb-2 flex items-center gap-2 text-xs font-medium text-muted-foreground">
            <Bot aria-hidden="true" className="size-3.5" /> Hermes
          </p>
        ) : null}
        {message.attachments?.length ? (
          <AttachmentGallery
            attachments={message.attachments}
            onOpenArtifact={onOpenArtifact}
            projectId={projectId}
          />
        ) : null}
        <MarkdownPreview
          className={cn(
            "border-0 bg-transparent p-0 text-sm leading-7 shadow-none",
            user ? "prose-p:my-0" : null,
          )}
          source={message.content || (streaming ? "…" : "")}
        />
        {streaming ? <span aria-label="正在流式回复">▍</span> : null}
        {message.tool_calls?.length ? (
          <ToolCallList toolCalls={message.tool_calls} />
        ) : null}
      </div>
    </div>
  );
}

function AttachmentGallery({
  attachments,
  onOpenArtifact,
  projectId,
}: {
  attachments: AgentChatAttachment[];
  onOpenArtifact: (artifactId: string) => void;
  projectId: string;
}) {
  return (
    <div className="mb-2 grid gap-2 sm:grid-cols-2">
      {attachments.map((attachment) => (
        <ArtifactAttachmentCard
          attachment={attachment}
          key={`${attachment.artifact_id}-${attachment.version_id}`}
          onOpen={() => onOpenArtifact(attachment.artifact_id)}
          projectId={projectId}
        />
      ))}
    </div>
  );
}

function ArtifactAttachmentCard({
  attachment,
  onOpen,
  projectId,
}: {
  attachment: AgentChatAttachment;
  onOpen: () => void;
  projectId: string;
}) {
  const [downloadUrl, setDownloadUrl] = useState(attachment.local_url ?? "");
  const image = attachment.mime_type.startsWith("image/");

  useEffect(() => {
    if (!image || attachment.local_url || !attachment.artifact_id) return;
    let active = true;
    void artifactApi
      .download(projectId, attachment.artifact_id, attachment.version_id)
      .then((grant) => {
        if (active) setDownloadUrl(grant.transfer.url);
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [
    attachment.artifact_id,
    attachment.local_url,
    attachment.version_id,
    image,
    projectId,
  ]);

  return (
    <button
      aria-label={`打开 Artifact 详情：${attachment.name || attachment.filename}`}
      className="group overflow-hidden rounded-xl border border-border bg-background text-left transition-colors hover:bg-muted/40"
      onClick={onOpen}
      type="button"
    >
      {image && downloadUrl ? (
        <img
          alt={attachment.name || attachment.filename}
          className="max-h-72 w-full object-contain bg-muted/30"
          src={downloadUrl}
        />
      ) : null}
      <span className="flex items-center gap-3 p-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
          {image ? (
            <Paperclip aria-hidden="true" className="size-4" />
          ) : (
            <FileText aria-hidden="true" className="size-4" />
          )}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate font-medium">
            {attachment.name || attachment.filename}
          </span>
          <span className="block truncate text-xs text-muted-foreground">
            {attachment.filename} · {formatBytes(attachment.size_bytes)}
          </span>
        </span>
        <PanelRightOpen
          aria-hidden="true"
          className="size-4 shrink-0 text-muted-foreground group-hover:text-foreground"
        />
      </span>
    </button>
  );
}

function ComposerAttachmentChip({
  attachment,
  onRemove,
}: {
  attachment: ComposerAttachment;
  onRemove: () => void;
}) {
  return (
    <div className="relative flex max-w-56 items-center gap-2 rounded-xl border border-border bg-background px-3 py-2 pr-8 text-xs">
      <FileText aria-hidden="true" className="size-4 shrink-0" />
      <div className="min-w-0">
        <p className="truncate font-medium">{attachment.filename}</p>
        <p
          className={cn(
            "truncate text-muted-foreground",
            attachment.status === "failed" ? "text-destructive" : null,
          )}
        >
          {attachment.status === "uploading"
            ? `上传中 ${Math.round(attachment.progress * 100)}%`
            : attachment.status === "failed"
              ? attachment.error || "上传失败"
              : formatBytes(attachment.size_bytes)}
        </p>
      </div>
      <button
        aria-label={`移除 ${attachment.filename}`}
        className="absolute right-1.5 top-1.5 rounded-full p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
        onClick={onRemove}
        type="button"
      >
        <X aria-hidden="true" className="size-3" />
      </button>
    </div>
  );
}

function formatBytes(size: number): string {
  if (size < 1_024) return `${size} B`;
  if (size < 1_048_576) return `${(size / 1_024).toFixed(1)} KB`;
  return `${(size / 1_048_576).toFixed(1)} MB`;
}

export function RunActivity({
  reasoningState,
  runBusy,
  toolCalls,
}: {
  reasoningState: "idle" | "active" | "complete";
  runBusy: boolean;
  toolCalls: AgentToolCall[];
}) {
  if (!runBusy) {
    const settledToolCalls = toolCalls.map((toolCall) =>
      toolCall.status === "queued" || toolCall.status === "running"
        ? { ...toolCall, status: "completed" as const }
        : toolCall,
    );
    return settledToolCalls.length ? (
      <ToolCallList toolCalls={settledToolCalls} />
    ) : null;
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
      {toolCalls.length ? <ToolCallList toolCalls={toolCalls} /> : null}
      {runBusy && reasoningState === "idle" && toolCalls.length === 0 ? (
        <p className="text-muted-foreground">正在准备回答…</p>
      ) : null}
    </div>
  );
}

function ToolCallList({ toolCalls }: { toolCalls: AgentToolCall[] }) {
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
            <Badge>{tool.status}</Badge>
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
  pending,
  run,
}: {
  canOperate: boolean;
  onRegenerate: () => void;
  onRerun: () => void;
  pending: boolean;
  run: AgentRun | null;
}) {
  if (!run) {
    return <Badge>idle</Badge>;
  }
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Badge>{run.status}</Badge>
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

function RunStatusCard({ run }: { run: AgentRun | null }) {
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
  if (event === "run.failed") return "failed";
  if (event === "run.stopped") return "stopped";
  return null;
}

function waitForAgentReconnect(
  delayMs: number,
  signal: AbortSignal,
): Promise<void> {
  return new Promise((resolve) => {
    if (signal.aborted) {
      resolve();
      return;
    }
    const onAbort = () => {
      window.clearTimeout(timer);
      resolve();
    };
    const timer = window.setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, delayMs);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

function isReasoningEffortPreference(
  value: string | null,
): value is ReasoningEffortPreference {
  return reasoningEffortOptions.some((option) => option.value === value);
}

export function upsertToolCall(
  current: AgentToolCall[],
  toolCall: AgentToolCall,
): AgentToolCall[] {
  return [
    ...current.filter((item) => item.tool_call_id !== toolCall.tool_call_id),
    toolCall,
  ];
}

export function mergeToolCalls(
  persisted: AgentToolCall[],
  streamed: AgentToolCall[],
): AgentToolCall[] {
  return streamed.reduce(upsertToolCall, persisted);
}

export function mergeStreamText(current: string, incoming: string): string {
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

export function messageComparisonKey(content: string): string {
  return content.trim().replace(/\s+/g, " ");
}

export function prepareMessagesForDisplay(
  messages: AgentMessage[],
  activeToolCallIds: Set<string>,
  deferAssistantAttachments = false,
  activeRunId?: string,
): AgentMessage[] {
  const result: AgentMessage[] = [];
  let assistantMessages = new Map<string, number>();
  for (const message of messages) {
    if (message.role === "user") assistantMessages = new Map<string, number>();
    const toolCalls = (message.tool_calls ?? []).filter(
      (toolCall) => !activeToolCallIds.has(toolCall.tool_call_id),
    );
    const attachments = (message.attachments ?? []).filter(
      (attachment) =>
        !(
          deferAssistantAttachments &&
          message.role === "assistant" &&
          attachment.run_id === activeRunId
        ),
    );
    const candidate: AgentMessage = {
      ...message,
      attachments,
      tool_calls: toolCalls,
    };
    const hasContent = Boolean(messageComparisonKey(candidate.content));
    const hasAttachments = Boolean(candidate.attachments?.length);
    if (!hasContent && !hasAttachments && toolCalls.length === 0) continue;
    // Core projects output artifacts as an empty assistant message. Keep that
    // projection out of the live reasoning/tool sequence until the Run settles.
    if (
      deferAssistantAttachments &&
      candidate.role === "assistant" &&
      !hasContent &&
      hasAttachments &&
      toolCalls.length === 0
    ) {
      continue;
    }

    const key = messageComparisonKey(candidate.content);
    if (candidate.role === "assistant" && key) {
      const existingIndex = assistantMessages.get(key);
      if (existingIndex !== undefined) {
        const existing = result[existingIndex];
        result[existingIndex] = {
          ...existing,
          attachments: mergeMessageAttachments(
            existing.attachments ?? [],
            candidate.attachments ?? [],
          ),
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

function mergeMessageAttachments(
  current: AgentChatAttachment[],
  incoming: AgentChatAttachment[],
): AgentChatAttachment[] {
  const attachments = new Map(
    current.map((attachment) => [
      `${attachment.artifact_id}:${attachment.version_id}`,
      attachment,
    ]),
  );
  for (const attachment of incoming) {
    attachments.set(
      `${attachment.artifact_id}:${attachment.version_id}`,
      attachment,
    );
  }
  return [...attachments.values()];
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
