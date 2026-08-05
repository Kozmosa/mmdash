"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Inbox, MailOpen, RotateCcw } from "lucide-react";
import { useState } from "react";

import { EmptyState } from "@/components/states/empty-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { apiClient } from "@/lib/api-client";

type InboxItem = {
  inbox_item_id: string;
  read_state: "read" | "unread";
  archived_at?: string;
  outcome: string;
  created_at: string;
  notification: {
    type_key: string;
    project_id?: string;
    data: Record<string, unknown>;
    occurred_at: string;
    action?: {
      action_type: string;
      action_resource_id: string;
      route?: string;
    };
  };
};
type InboxPage = {
  items: InboxItem[];
  has_more: boolean;
  next_cursor?: string;
};

export default function InboxPageView() {
  const client = useQueryClient();
  const [view, setView] = useState<"all" | "unread" | "processed">("all");
  const inbox = useQuery({
    queryKey: ["inbox", view],
    queryFn: () =>
      apiClient.request<InboxPage>("/inbox", {
        query:
          view === "unread"
            ? { read_state: "unread", archived: "false" }
            : view === "processed"
              ? { outcome: "resolved" }
              : {},
      }),
  });
  const mark = useMutation({
    mutationFn: (input: {
      id: string;
      body: { read_state?: "read" | "unread"; archived?: boolean };
    }) =>
      apiClient.request(`/inbox/${encodeURIComponent(input.id)}`, {
        method: "PATCH",
        body: input.body,
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["inbox"] });
      void client.invalidateQueries({ queryKey: ["inbox-unread-count"] });
    },
  });
  const markAll = useMutation({
    mutationFn: () =>
      apiClient.request("/inbox/mark-all-read", { method: "POST", body: {} }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["inbox"] });
      void client.invalidateQueries({ queryKey: ["inbox-unread-count"] });
    },
  });
  const acceptInvitation = useMutation({
    mutationFn: (invitationId: string) =>
      apiClient.request(`/projects/invitations/${encodeURIComponent(invitationId)}/accept`, {
        method: "POST",
        body: {},
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["inbox"] });
      void client.invalidateQueries({ queryKey: ["inbox-unread-count"] });
    },
  });

  return (
    <section className="space-y-6" aria-labelledby="inbox-title">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs">
            <Inbox className="size-5" />
          </div>
          <h1
            id="inbox-title"
            className="text-2xl font-semibold tracking-tight"
          >
            Inbox
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            只显示当前用户的通知；阅读、归档和业务结果相互独立。
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => void inbox.refetch()}>
            <RotateCcw className="size-4" />
            刷新
          </Button>
          <Button disabled={markAll.isPending} onClick={() => markAll.mutate()}>
            <MailOpen className="size-4" />
            全部标为已读
          </Button>
        </div>
      </header>
      <nav
        aria-label="Inbox 筛选"
        className="flex gap-2 border-b border-border pb-3"
      >
        {(
          [
            ["all", "全部"],
            ["unread", "未读"],
            ["processed", "已处理"],
          ] as const
        ).map(([key, label]) => (
          <Button
            key={key}
            variant={view === key ? "secondary" : "outline"}
            aria-pressed={view === key}
            onClick={() => setView(key)}
          >
            {label}
          </Button>
        ))}
      </nav>
      {inbox.isLoading ? (
        <p className="text-sm text-muted-foreground">正在读取 Inbox…</p>
      ) : null}
      {inbox.error ? (
        <p className="text-sm text-destructive">{inbox.error.message}</p>
      ) : null}
      {inbox.data?.items.length ? (
        <div className="space-y-3">
          {inbox.data.items.map((item) => (
            <Card
              key={item.inbox_item_id}
              className={
                item.read_state === "unread" ? "border-primary/50" : undefined
              }
            >
              <CardHeader className="flex-row items-start justify-between gap-3">
                <div>
                  <CardTitle className="text-base">{title(item)}</CardTitle>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {item.notification.type_key} ·{" "}
                    {new Date(item.created_at).toLocaleString()}
                  </p>
                </div>
                <Badge>{item.outcome}</Badge>
              </CardHeader>
              <CardContent className="flex flex-wrap items-center justify-between gap-3">
                <p className="text-sm text-muted-foreground">
                  {description(item)}
                </p>
                <div className="flex gap-2">
                  {item.notification.action?.action_type ===
                    "project.invitation.accept" &&
                  item.outcome === "active" ? (
                    <Button
                      disabled={acceptInvitation.isPending}
                      onClick={() =>
                        acceptInvitation.mutate(
                          item.notification.action!.action_resource_id,
                        )
                      }
                      size="sm"
                    >
                      接受邀请
                    </Button>
                  ) : null}
                  {item.read_state === "unread" ? (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        mark.mutate({
                          id: item.inbox_item_id,
                          body: { read_state: "read" },
                        })
                      }
                    >
                      标为已读
                    </Button>
                  ) : (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        mark.mutate({
                          id: item.inbox_item_id,
                          body: { read_state: "unread" },
                        })
                      }
                    >
                      标为未读
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() =>
                      mark.mutate({
                        id: item.inbox_item_id,
                        body: { archived: !item.archived_at },
                      })
                    }
                  >
                    {item.archived_at ? "取消归档" : "归档"}
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      ) : (
        <EmptyState
          description="新的邀请、Progress 提醒和后续模块通知会出现在这里。"
          title="Inbox 暂无消息"
        />
      )}
    </section>
  );
}

function title(item: InboxItem): string {
  const value = item.notification.data.title;
  return typeof value === "string" && value
    ? value
    : item.notification.type_key === "project.invitation.received"
      ? "项目邀请"
      : "需要关注的通知";
}
function description(item: InboxItem): string {
  const role = item.notification.data.role;
  return typeof role === "string"
    ? `邀请角色：${role}`
    : "通知内容由经过审查的模板字段生成。";
}
