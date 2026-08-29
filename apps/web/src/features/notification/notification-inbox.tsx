"use client";

import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Archive, ArchiveRestore, CheckCheck, FilterX } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";

import { EmptyState } from "@/components/states/empty-state";
import { ErrorState } from "@/components/states/error-state";
import { LoadingState } from "@/components/states/loading-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { apiClient } from "@/lib/api-client";

import {
  acceptInboxInvitation,
  inboxBody,
  inboxTitle,
  listInbox,
  markAllInboxRead,
  updateInboxItem,
} from "./notification-api";
import type {
  InboxItem,
  InboxListQuery,
  InboxPage,
  ProjectOption,
} from "./types";

const notificationTypes = [
  { key: "project.invitation.received", label: "项目邀请" },
  { key: "progress.reminder.due", label: "Progress 提醒" },
] as const;

const outcomeLabels: Record<InboxItem["outcome"], string> = {
  active: "待处理",
  expired: "已过期",
  resolved: "已处理",
  revoked: "已撤销",
};

type InboxView = "unread" | "all" | "processed";

export function NotificationInbox() {
  const queryClient = useQueryClient();
  const [view, setView] = useState<InboxView>("unread");
  const [archiveMode, setArchiveMode] = useState<"active" | "archived">(
    "active",
  );
  const [projectId, setProjectId] = useState("");
  const [typeKey, setTypeKey] = useState("");
  const [occurredFrom, setOccurredFrom] = useState("");
  const [occurredTo, setOccurredTo] = useState("");

  const projects = useQuery({
    queryFn: () => apiClient.request<{ items: ProjectOption[] }>("/projects"),
    queryKey: ["projects", "inbox-filter"],
  });
  const filter: InboxListQuery = {
    archived: archiveMode === "archived" ? "true" : "false",
    limit: 20,
    occurred_from: toISOString(occurredFrom),
    occurred_to: toISOString(occurredTo),
    outcome_group: view === "processed" ? "processed" : undefined,
    project_id: projectId || undefined,
    read_state: view === "unread" ? "unread" : undefined,
    type_key: typeKey || undefined,
  };
  const inbox = useInfiniteQuery({
    getNextPageParam: (lastPage: InboxPage) => lastPage.next_cursor,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => listInbox({ ...filter, cursor: pageParam }),
    queryKey: ["inbox", "list", filter],
  });
  const items = inbox.data?.pages.flatMap((page) => page.items) ?? [];

  const invalidateInbox = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ["inbox", "list"] }),
      queryClient.invalidateQueries({ queryKey: ["inbox", "unread-count"] }),
    ]);
  const update = useMutation({
    mutationFn: ({
      body,
      id,
    }: {
      body: { archived?: boolean; read_state?: "read" | "unread" };
      id: string;
    }) => updateInboxItem(id, body),
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "消息状态更新失败"),
    onSuccess: () => void invalidateInbox(),
  });
  const markAll = useMutation({
    mutationFn: () =>
      markAllInboxRead({
        project_id: projectId || undefined,
        type_key: typeKey || undefined,
      }),
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "批量已读失败"),
    onSuccess: () => {
      void invalidateInbox();
      toast.success("筛选范围内的消息已全部标为已读");
    },
  });
  const accept = useMutation({
    mutationFn: (invitationId: string) => acceptInboxInvitation(invitationId),
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "接受邀请失败"),
    onSuccess: () => {
      void Promise.all([
        invalidateInbox(),
        queryClient.invalidateQueries({ queryKey: ["projects"] }),
      ]);
      toast.success("已接受项目邀请");
    },
  });

  function clearFilters() {
    setProjectId("");
    setTypeKey("");
    setOccurredFrom("");
    setOccurredTo("");
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3">
        <nav aria-label="收件箱视图" className="flex flex-wrap gap-2">
          {(
            [
              ["unread", "未读"],
              ["all", "全部"],
              ["processed", "已处理"],
            ] as const
          ).map(([key, label]) => (
            <Button
              aria-pressed={view === key}
              key={key}
              onClick={() => setView(key)}
              variant={view === key ? "secondary" : "outline"}
            >
              {label}
            </Button>
          ))}
        </nav>
        <div className="flex flex-wrap gap-2">
          <Button
            aria-pressed={archiveMode === "archived"}
            onClick={() =>
              setArchiveMode((current) =>
                current === "active" ? "archived" : "active",
              )
            }
            variant="outline"
          >
            {archiveMode === "archived" ? (
              <ArchiveRestore aria-hidden="true" className="size-4" />
            ) : (
              <Archive aria-hidden="true" className="size-4" />
            )}
            {archiveMode === "archived" ? "返回未归档" : "查看已归档"}
          </Button>
          <Button disabled={markAll.isPending} onClick={() => markAll.mutate()}>
            <CheckCheck aria-hidden="true" className="size-4" />
            全部标为已读
          </Button>
        </div>
      </div>

      <section
        aria-label="收件箱筛选"
        className="flex flex-wrap items-center gap-2"
      >
        <select
          aria-label="项目"
          className="h-9 min-w-[130px] rounded-md border border-input bg-background px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
          onChange={(event) => setProjectId(event.target.value)}
          value={projectId}
        >
          <option value="">全部项目</option>
          {projects.data?.items.map((project) => (
            <option key={project.id} value={project.id}>
              {project.name}
            </option>
          ))}
        </select>

        <select
          aria-label="消息类型"
          className="h-9 min-w-[130px] rounded-md border border-input bg-background px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
          onChange={(event) => setTypeKey(event.target.value)}
          value={typeKey}
        >
          <option value="">全部类型</option>
          {notificationTypes.map((type) => (
            <option key={type.key} value={type.key}>
              {type.label}
            </option>
          ))}
        </select>

        <div className="flex flex-wrap items-center gap-1.5 rounded-md border border-input bg-background px-2.5 py-1 text-xs text-muted-foreground h-9">
          <span>从</span>
          <input
            aria-label="开始时间"
            className="bg-transparent border-0 outline-none focus:ring-0 text-xs w-[125px]"
            onChange={(event) => setOccurredFrom(event.target.value)}
            type="datetime-local"
            value={occurredFrom}
          />
          <span>至</span>
          <input
            aria-label="结束时间"
            className="bg-transparent border-0 outline-none focus:ring-0 text-xs w-[125px]"
            onChange={(event) => setOccurredTo(event.target.value)}
            type="datetime-local"
            value={occurredTo}
          />
        </div>

        {(projectId || typeKey || occurredFrom || occurredTo) && (
          <Button
            onClick={clearFilters}
            variant="ghost"
            size="sm"
            className="h-9 px-3 text-xs text-muted-foreground hover:text-foreground"
          >
            <FilterX aria-hidden="true" className="mr-1.5 size-3.5" />
            清除筛选
          </Button>
        )}
      </section>

      {inbox.isPending ? <LoadingState label="正在读取收件箱…" /> : null}
      {inbox.isError ? (
        <ErrorState
          description={inbox.error.message}
          onRetry={() => void inbox.refetch()}
          title="无法读取收件箱"
        />
      ) : null}
      {!inbox.isPending && !inbox.isError && items.length === 0 ? (
        <EmptyState
          description={
            archiveMode === "archived"
              ? "当前筛选范围内没有已归档消息。"
              : "新的邀请、Progress 提醒和后续模块消息会出现在这里。"
          }
          title={view === "unread" ? "没有未读消息" : "收件箱暂无消息"}
        />
      ) : null}
      {items.length > 0 ? (
        <div className="space-y-3">
          {items.map((item) => (
            <InboxCard
              accepting={accept.isPending}
              item={item}
              key={item.inbox_item_id}
              onAccept={(invitationId) => accept.mutate(invitationId)}
              onUpdate={(body) =>
                update.mutate({ body, id: item.inbox_item_id })
              }
              projectName={projectName(projects.data?.items, item)}
              updating={update.isPending}
            />
          ))}
        </div>
      ) : null}
      {inbox.hasNextPage ? (
        <div className="flex justify-center">
          <Button
            disabled={inbox.isFetchingNextPage}
            onClick={() => void inbox.fetchNextPage()}
            variant="outline"
          >
            {inbox.isFetchingNextPage ? "正在加载…" : "加载更多"}
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function InboxCard({
  accepting,
  item,
  onAccept,
  onUpdate,
  projectName: currentProjectName,
  updating,
}: Readonly<{
  accepting: boolean;
  item: InboxItem;
  onAccept: (invitationId: string) => void;
  onUpdate: (body: {
    archived?: boolean;
    read_state?: "read" | "unread";
  }) => void;
  projectName?: string;
  updating: boolean;
}>) {
  const action = item.notification.action;
  return (
    <Card className={item.read_state === "unread" ? "border-primary/50" : ""}>
      <CardHeader className="flex-row items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle className="text-base">{inboxTitle(item)}</CardTitle>
            {item.read_state === "unread" ? (
              <Badge className="border-primary/40 bg-primary/10 text-primary">
                未读
              </Badge>
            ) : null}
            <Badge>{outcomeLabels[item.outcome]}</Badge>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            {currentProjectName ? `${currentProjectName} · ` : ""}
            {new Date(item.notification.occurred_at).toLocaleString()}
          </p>
        </div>
        <Badge>{priorityLabel(item.notification.priority)}</Badge>
      </CardHeader>
      <CardContent className="flex flex-wrap items-end justify-between gap-4">
        <p className="max-w-3xl text-sm leading-6 text-muted-foreground">
          {inboxBody(item)}
        </p>
        <div className="flex flex-wrap gap-2">
          {action?.action_type === "project.invitation.accept" &&
          item.outcome === "active" ? (
            <Button
              disabled={accepting}
              onClick={() => onAccept(action.action_resource_id)}
              size="sm"
            >
              接受邀请
            </Button>
          ) : null}
          <Link
            className="inline-flex h-8 items-center justify-center rounded-md border border-border bg-background px-3 text-xs font-medium shadow-xs outline-none transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring"
            href={`/inbox/${encodeURIComponent(item.inbox_item_id)}`}
          >
            查看详情
          </Link>
          <Button
            disabled={updating}
            onClick={() =>
              onUpdate({
                read_state: item.read_state === "unread" ? "read" : "unread",
              })
            }
            size="sm"
            variant="outline"
          >
            {item.read_state === "unread" ? "标为已读" : "标为未读"}
          </Button>
          <Button
            disabled={updating}
            onClick={() => onUpdate({ archived: !item.archived_at })}
            size="sm"
            variant="ghost"
          >
            {item.archived_at ? "取消归档" : "归档"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function toISOString(value: string): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

function projectName(
  projects: ProjectOption[] | undefined,
  item: InboxItem,
): string | undefined {
  return projects?.find(
    (project) => project.id === item.notification.project_id,
  )?.name;
}

function priorityLabel(
  priority: InboxItem["notification"]["priority"],
): string {
  return { high: "高", low: "低", normal: "普通", urgent: "紧急" }[priority];
}
