"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Archive, ArchiveRestore } from "lucide-react";
import Link from "next/link";
import { toast } from "sonner";

import { ErrorState } from "@/components/states/error-state";
import { LoadingState } from "@/components/states/loading-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

import {
  acceptInboxInvitation,
  getInboxItem,
  inboxBody,
  inboxTitle,
  updateInboxItem,
} from "./notification-api";

export function NotificationInboxDetail({
  inboxItemId,
}: Readonly<{ inboxItemId: string }>) {
  const queryClient = useQueryClient();
  const item = useQuery({
    queryFn: () => getInboxItem(inboxItemId),
    queryKey: ["inbox", "detail", inboxItemId],
  });
  const invalidate = () =>
    Promise.all([
      queryClient.invalidateQueries({
        queryKey: ["inbox", "detail", inboxItemId],
      }),
      queryClient.invalidateQueries({ queryKey: ["inbox", "list"] }),
      queryClient.invalidateQueries({ queryKey: ["inbox", "unread-count"] }),
    ]);
  const update = useMutation({
    mutationFn: (body: {
      archived?: boolean;
      read_state?: "read" | "unread";
    }) => updateInboxItem(inboxItemId, body),
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "消息状态更新失败"),
    onSuccess: () => void invalidate(),
  });
  const accept = useMutation({
    mutationFn: (invitationId: string) => acceptInboxInvitation(invitationId),
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "接受邀请失败"),
    onSuccess: () => {
      void Promise.all([
        invalidate(),
        queryClient.invalidateQueries({ queryKey: ["projects"] }),
      ]);
      toast.success("已接受项目邀请");
    },
  });

  if (item.isPending) return <LoadingState label="正在读取消息详情…" />;
  if (item.isError) {
    return (
      <ErrorState
        description={item.error.message}
        onRetry={() => void item.refetch()}
        title="无法读取消息详情"
      />
    );
  }

  const action = item.data.notification.action;
  return (
    <div className="space-y-5">
      <Link
        className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
        href="/inbox"
      >
        <ArrowLeft aria-hidden="true" className="size-4" />
        返回收件箱
      </Link>
      <Card>
        <CardHeader className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <Badge>{item.data.read_state === "unread" ? "未读" : "已读"}</Badge>
            <Badge>{outcomeLabel(item.data.outcome)}</Badge>
            <Badge>{priorityLabel(item.data.notification.priority)}</Badge>
          </div>
          <CardTitle>{inboxTitle(item.data)}</CardTitle>
          <p className="text-sm text-muted-foreground">
            {new Date(item.data.notification.occurred_at).toLocaleString()}
          </p>
        </CardHeader>
        <CardContent className="space-y-6">
          <p className="max-w-3xl text-sm leading-7">{inboxBody(item.data)}</p>
          <dl className="grid gap-4 rounded-lg bg-muted/40 p-4 text-sm sm:grid-cols-2">
            <Metadata
              label="消息类型"
              value={item.data.notification.type_key}
            />
            <Metadata
              label="关联对象"
              value={`${item.data.notification.resource_type}:${item.data.notification.resource_id}`}
            />
            <Metadata
              label="模板版本"
              value={`v${item.data.notification.template_version}`}
            />
            <Metadata
              label="归档状态"
              value={item.data.archived_at ? "已归档" : "未归档"}
            />
          </dl>
          <div className="flex flex-wrap gap-2 border-t border-border pt-4">
            {action?.action_type === "project.invitation.accept" &&
            item.data.outcome === "active" ? (
              <Button
                disabled={accept.isPending}
                onClick={() => accept.mutate(action.action_resource_id)}
              >
                接受邀请
              </Button>
            ) : null}
            <Button
              disabled={update.isPending}
              onClick={() =>
                update.mutate({
                  read_state:
                    item.data.read_state === "unread" ? "read" : "unread",
                })
              }
              variant="outline"
            >
              {item.data.read_state === "unread" ? "标为已读" : "标为未读"}
            </Button>
            <Button
              disabled={update.isPending}
              onClick={() =>
                update.mutate({ archived: !item.data.archived_at })
              }
              variant="ghost"
            >
              {item.data.archived_at ? (
                <ArchiveRestore aria-hidden="true" className="size-4" />
              ) : (
                <Archive aria-hidden="true" className="size-4" />
              )}
              {item.data.archived_at ? "取消归档" : "归档"}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function Metadata({
  label,
  value,
}: Readonly<{ label: string; value: string }>) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 break-all font-medium">{value}</dd>
    </div>
  );
}

function outcomeLabel(outcome: string): string {
  return (
    {
      active: "待处理",
      expired: "已过期",
      resolved: "已处理",
      revoked: "已撤销",
    }[outcome] ?? outcome
  );
}

function priorityLabel(priority: string): string {
  return (
    { high: "高优先级", low: "低优先级", normal: "普通", urgent: "紧急" }[
      priority
    ] ?? priority
  );
}
