"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, RotateCw } from "lucide-react";
import { toast } from "sonner";
import { useEffect, useState } from "react";

import { useCurrentProject } from "@/components/providers/project-provider";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { apiClient } from "@/lib/api-client";

const notificationTypes = [
  {
    key: "project.invitation.received",
    title: "项目邀请",
    description: "邀请始终保留在收件箱中，不会广播到外部群组。",
    requiredInbox: true,
  },
  {
    key: "progress.reminder.due",
    title: "Progress 提醒",
    description:
      "由 Progress 发布稳定事件，再由 Notification 负责站内和外部投递。",
    requiredInbox: false,
  },
] as const;

const channelTypes = [
  {
    key: "notification.feishu_webhook",
    title: "飞书群机器人 Webhook",
    endpointKey: "webhook_url",
    secretKey: null,
  },
  {
    key: "notification.generic_webhook",
    title: "通用签名 Webhook",
    endpointKey: "endpoint",
    secretKey: "signing_secret",
  },
] as const;

type NotificationRule = {
  project_id: string;
  type_key: string;
  inbox_enabled: boolean;
  external_enabled: boolean;
  channel_keys: string[];
  minimum_priority: "low" | "normal" | "high" | "urgent";
  version: number;
};

type Delivery = {
  delivery_id: string;
  notification_id: string;
  channel_key: string;
  status: string;
  attempts: number;
  last_error_code?: string;
  last_error?: string;
  created_at: string;
};

export function NotificationSettingsPanel() {
  const project = useCurrentProject();
  const deliveries = useQuery({
    queryKey: ["notification-deliveries", project.id],
    queryFn: () =>
      apiClient.request<{ items: Delivery[] }>(
        `/projects/${encodeURIComponent(project.id)}/notification-deliveries?limit=8`,
      ),
  });

  return (
    <section
      className="space-y-4"
      aria-labelledby="notification-settings-title"
    >
      <div>
        <h2
          className="flex items-center gap-2 text-lg font-semibold"
          id="notification-settings-title"
        >
          <Bell aria-hidden="true" className="size-5" />
          Notification 规则与投递
        </h2>
        <p className="text-sm text-muted-foreground">
          渠道密钥由 Settings 加密保存；这里仅管理 Type 规则和安全投递诊断。
        </p>
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        {channelTypes.map((definition) => (
          <NotificationChannelCard
            definition={definition}
            key={definition.key}
            projectId={project.id}
          />
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        {notificationTypes.map((definition) => (
          <NotificationRuleCard
            definition={definition}
            key={definition.key}
            projectId={project.id}
          />
        ))}
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="text-base">最近外部投递</CardTitle>
          <CardDescription>
            只展示状态和脱敏错误，不展示 URL、Secret 或 Provider 原文。
          </CardDescription>
        </CardHeader>
        <CardContent>
          {deliveries.isLoading ? (
            <p className="text-sm text-muted-foreground">正在读取投递记录…</p>
          ) : null}
          {deliveries.isError ? (
            <p className="text-sm text-destructive">无法读取投递诊断。</p>
          ) : null}
          {!deliveries.isLoading &&
          !deliveries.isError &&
          !deliveries.data?.items.length ? (
            <p className="text-sm text-muted-foreground">
              还没有外部投递记录。
            </p>
          ) : null}
          <ul className="space-y-2 text-sm">
            {deliveries.data?.items.map((delivery) => (
              <DeliveryRow
                delivery={delivery}
                key={delivery.delivery_id}
                projectId={project.id}
                onRetried={() => void deliveries.refetch()}
              />
            ))}
          </ul>
        </CardContent>
      </Card>
    </section>
  );
}

function NotificationChannelCard({
  definition,
  projectId,
}: {
  definition: (typeof channelTypes)[number];
  projectId: string;
}) {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["notification-channel", projectId, definition.key],
    queryFn: () =>
      apiClient.request<{
        channel_key: string;
        enabled: boolean;
        configured: boolean;
        settings_version: number;
      }>(
        `/projects/${encodeURIComponent(projectId)}/notification-channels/${encodeURIComponent(definition.key)}`,
      ),
  });
  const [enabled, setEnabled] = useState(false);
  const [endpoint, setEndpoint] = useState("");
  const [secret, setSecret] = useState("");

  useEffect(() => {
    if (query.data) {
      setEnabled(query.data.enabled);
    }
  }, [query.data]);

  const update = useMutation({
    mutationFn: () => {
      const values: Record<string, unknown> = { enabled };
      if (endpoint.trim()) {
        values[definition.endpointKey] = endpoint.trim();
      }
      if (definition.secretKey && secret.trim()) {
        values[definition.secretKey] = secret;
      }
      return apiClient.request(
        `/projects/${encodeURIComponent(projectId)}/notification-channels/${encodeURIComponent(definition.key)}`,
        { method: "PATCH", body: { values } },
      );
    },
    onSuccess: () => {
      setSecret("");
      setEndpoint("");
      void queryClient.invalidateQueries({
        queryKey: ["notification-channel", projectId, definition.key],
      });
      toast.success("通知渠道已保存");
    },
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "通知渠道保存失败"),
  });
  const test = useMutation({
    mutationFn: () =>
      apiClient.request(
        `/projects/${encodeURIComponent(projectId)}/notification-channels/${encodeURIComponent(definition.key)}/test`,
        { method: "POST" },
      ),
    onSuccess: () => toast.success("连接测试已完成"),
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "连接测试失败"),
  });
  const remove = useMutation({
    mutationFn: () =>
      apiClient.request(
        `/projects/${encodeURIComponent(projectId)}/notification-channels/${encodeURIComponent(definition.key)}`,
        { method: "DELETE" },
      ),
    onSuccess: () => {
      setEnabled(false);
      setEndpoint("");
      setSecret("");
      void queryClient.invalidateQueries({
        queryKey: ["notification-channel", projectId, definition.key],
      });
      toast.success("通知渠道已删除");
    },
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "通知渠道删除失败"),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{definition.title}</CardTitle>
        <CardDescription>
          {query.data?.configured
            ? `已配置 · Settings v${query.data.settings_version}`
            : "尚未配置；密钥不会回显。"}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="space-y-3"
          onSubmit={(event) => {
            event.preventDefault();
            update.mutate();
          }}
        >
          <label className="flex items-center gap-2 text-sm">
            <input
              checked={enabled}
              disabled={update.isPending}
              onChange={(event) => setEnabled(event.target.checked)}
              type="checkbox"
            />
            启用渠道
          </label>
          <input
            aria-label={`${definition.title} endpoint`}
            className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            onChange={(event) => setEndpoint(event.target.value)}
            placeholder={
              query.data?.configured ? "Endpoint 已保存，留空以保留" : "https://…"
            }
            type="url"
            value={endpoint}
          />
          {definition.secretKey ? (
            <input
              aria-label={`${definition.title} secret`}
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              onChange={(event) => setSecret(event.target.value)}
              placeholder={
                query.data?.configured
                  ? "Secret 已保存，留空以保留"
                  : "签名 Secret"
              }
              type="password"
              value={secret}
            />
          ) : null}
          <div className="flex flex-wrap gap-2">
            <Button disabled={update.isPending} type="submit">
              保存
            </Button>
            <Button
              disabled={!query.data?.configured || test.isPending}
              onClick={() => test.mutate()}
              type="button"
              variant="outline"
            >
              测试连接
            </Button>
            <Button
              disabled={!query.data?.configured || remove.isPending}
              onClick={() => remove.mutate()}
              type="button"
              variant="ghost"
            >
              删除
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function NotificationRuleCard({
  definition,
  projectId,
}: {
  definition: (typeof notificationTypes)[number];
  projectId: string;
}) {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["notification-rule", projectId, definition.key],
    queryFn: () =>
      apiClient.request<NotificationRule>(
        `/projects/${encodeURIComponent(projectId)}/notification-rules/${encodeURIComponent(definition.key)}`,
      ),
  });
  const update = useMutation({
    mutationFn: (input: Partial<NotificationRule>) =>
      apiClient.request<NotificationRule>(
        `/projects/${encodeURIComponent(projectId)}/notification-rules/${encodeURIComponent(definition.key)}`,
        {
          method: "PUT",
          body: {
            inbox_enabled:
              input.inbox_enabled ?? query.data?.inbox_enabled ?? true,
            external_enabled:
              input.external_enabled ?? query.data?.external_enabled ?? false,
            channel_keys: input.channel_keys ?? query.data?.channel_keys ?? [],
            minimum_priority:
              input.minimum_priority ??
              query.data?.minimum_priority ??
              "normal",
            version: input.version ?? query.data?.version ?? 0,
          },
        },
      ),
    onSuccess: (rule) => {
      queryClient.setQueryData(
        ["notification-rule", projectId, definition.key],
        rule,
      );
      toast.success("Notification 规则已保存");
    },
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "规则保存失败"),
  });

  const rule = query.data;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{definition.title}</CardTitle>
        <CardDescription>{definition.description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {query.isLoading ? (
          <p className="text-sm text-muted-foreground">正在读取规则…</p>
        ) : null}
        {query.isError ? (
          <p className="text-sm text-destructive">无法读取规则。</p>
        ) : null}
        {rule ? (
          <>
            <label className="flex items-center gap-2 text-sm">
              <input
                checked={definition.requiredInbox || rule.inbox_enabled}
                disabled={definition.requiredInbox || update.isPending}
                onChange={(event) =>
                  update.mutate({ inbox_enabled: event.target.checked })
                }
                type="checkbox"
              />
              保留在 Inbox
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                checked={rule.external_enabled}
                disabled={!definition.requiredInbox && update.isPending}
                onChange={(event) =>
                  update.mutate({ external_enabled: event.target.checked })
                }
                type="checkbox"
              />
              启用外部投递
            </label>
            <fieldset className="space-y-2 text-sm">
              <legend className="text-muted-foreground">投递渠道</legend>
              {channelTypes.map((channel) => (
                <label className="flex items-center gap-2" key={channel.key}>
                  <input
                    checked={rule.channel_keys.includes(channel.key)}
                    disabled={!rule.external_enabled || update.isPending}
                    onChange={(event) => {
                      const keys = new Set(rule.channel_keys);
                      if (event.target.checked) {
                        keys.add(channel.key);
                      } else {
                        keys.delete(channel.key);
                      }
                      update.mutate({ channel_keys: [...keys] });
                    }}
                    type="checkbox"
                  />
                  {channel.title}
                </label>
              ))}
            </fieldset>
            <label className="flex items-center justify-between gap-3 text-sm">
              最低优先级
              <select
                className="rounded-md border border-border bg-background px-2 py-1"
                disabled={update.isPending}
                onChange={(event) =>
                  update.mutate({
                    minimum_priority: event.target
                      .value as NotificationRule["minimum_priority"],
                  })
                }
                value={rule.minimum_priority}
              >
                <option value="low">低</option>
                <option value="normal">普通</option>
                <option value="high">高</option>
                <option value="urgent">紧急</option>
              </select>
            </label>
          </>
        ) : null}
      </CardContent>
    </Card>
  );
}

function DeliveryRow({
  delivery,
  projectId,
  onRetried,
}: {
  delivery: Delivery;
  projectId: string;
  onRetried: () => void;
}) {
  const retry = useMutation({
    mutationFn: () =>
      apiClient.request<Delivery>(
        `/projects/${encodeURIComponent(projectId)}/notification-deliveries/${encodeURIComponent(delivery.delivery_id)}/retry`,
        {
          method: "POST",
          body: { reason: "Manual retry from Notification settings" },
        },
      ),
    onSuccess: () => {
      toast.success("已创建新的投递尝试");
      onRetried();
    },
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "重试失败"),
  });

  return (
    <li className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-2">
      <span>
        <code>{delivery.channel_key}</code> · {delivery.status} ·{" "}
        {delivery.attempts} 次尝试
        {delivery.last_error_code ? ` · ${delivery.last_error_code}` : ""}
      </span>
      {delivery.status === "failed" ? (
        <button
          className="inline-flex items-center gap-1 rounded-md border border-border px-2 py-1 text-xs hover:bg-accent disabled:opacity-50"
          disabled={retry.isPending}
          onClick={() => retry.mutate()}
          type="button"
        >
          <RotateCw aria-hidden="true" className="size-3" />
          重试
        </button>
      ) : null}
    </li>
  );
}
