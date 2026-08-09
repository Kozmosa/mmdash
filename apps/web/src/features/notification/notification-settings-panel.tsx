"use client";

import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  useQueries,
} from "@tanstack/react-query";
import { Bell, CheckCircle2, RotateCw, Send, ShieldCheck } from "lucide-react";
import { useState } from "react";
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
import { apiClient } from "@/lib/api-client";

import type { NotificationRule } from "./types";

const channelTypes = [
  {
    endpointKey: "webhook_url",
    key: "notification.feishu_webhook",
    secretKey: null,
    title: "飞书群机器人",
  },
  {
    endpointKey: "endpoint",
    key: "notification.generic_webhook",
    secretKey: "signing_secret",
    title: "通用签名 Webhook",
  },
] as const;

type ChannelDefinition = (typeof channelTypes)[number];
type ChannelState = {
  channel_key: string;
  configured: boolean;
  enabled: boolean;
  settings_version: number;
};
type Delivery = {
  attempts: number;
  channel_key: string;
  created_at: string;
  delivery_id: string;
  has_more?: boolean;
  last_error_code?: string;
  last_error?: string;
  notification_id: string;
  status: string;
};
type DeliveryPage = {
  has_more: boolean;
  items: Delivery[];
  next_cursor?: string;
};

export function NotificationSettingsPanel() {
  const project = useCurrentProject();
  const canManage = project.role === "owner" || project.role === "maintainer";

  return (
    <section
      aria-labelledby="notification-settings-title"
      className="space-y-6"
    >
      <div>
        <h2
          className="flex items-center gap-2 text-lg font-semibold"
          id="notification-settings-title"
        >
          <Bell aria-hidden="true" className="size-5" />
          通知与投递
        </h2>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
          Notification 统一接收领域消息并按类型策略投递。Inbox
          是站内渠道；飞书和 Webhook
          是项目级外部渠道，三者共享消息事实但各自保存投递状态。
        </p>
      </div>

      <InboxPolicyCard />

      <section aria-labelledby="external-channels-title" className="space-y-3">
        <div>
          <h3 className="font-semibold" id="external-channels-title">
            外部渠道
          </h3>
          <p className="text-sm text-muted-foreground">
            项目成员可查看脱敏状态；只有 owner 和 maintainer
            可以修改或测试凭据。
          </p>
        </div>
        <div className="grid gap-4 lg:grid-cols-2">
          {channelTypes.map((definition) => (
            <NotificationChannelCard
              canManage={canManage}
              definition={definition}
              key={definition.key}
              projectId={project.id}
            />
          ))}
        </div>
      </section>

      {canManage ? (
        <>
          <ExternalRuleCard projectId={project.id} />
          <DeliveryDiagnostics projectId={project.id} />
        </>
      ) : (
        <Card>
          <CardContent className="flex items-start gap-3 p-5 text-sm text-muted-foreground">
            <ShieldCheck
              aria-hidden="true"
              className="mt-0.5 size-5 shrink-0"
            />
            <p>
              外部投递规则和 Delivery 诊断仅对 owner、maintainer
              开放；你的个人消息仍可在全局 Inbox 中查看和管理。
            </p>
          </CardContent>
        </Card>
      )}
    </section>
  );
}

function InboxPolicyCard() {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <CheckCircle2 aria-hidden="true" className="size-4" />
          站内 Inbox
        </CardTitle>
        <CardDescription>
          Inbox 是否接收消息由 Notification Type 的安全策略决定，不属于 Project
          外部投递规则。
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3 text-sm md:grid-cols-2">
        <div className="rounded-lg border border-border p-3">
          <div className="flex items-center justify-between gap-3">
            <span className="font-medium">项目邀请</span>
            <Badge>必须进入 Inbox</Badge>
          </div>
          <p className="mt-2 text-muted-foreground">
            只发送给被邀请人，不允许广播到项目外部群渠道。
          </p>
        </div>
        <div className="rounded-lg border border-border p-3">
          <div className="flex items-center justify-between gap-3">
            <span className="font-medium">Progress 提醒</span>
            <Badge>默认进入 Inbox</Badge>
          </div>
          <p className="mt-2 text-muted-foreground">
            可额外开启项目外部投递；未来的个人订阅偏好不会由项目管理员代替设置。
          </p>
        </div>
      </CardContent>
    </Card>
  );
}

function NotificationChannelCard({
  canManage,
  definition,
  projectId,
}: Readonly<{
  canManage: boolean;
  definition: ChannelDefinition;
  projectId: string;
}>) {
  const queryClient = useQueryClient();
  const channel = useQuery({
    queryFn: () =>
      apiClient.request<ChannelState>(
        "/projects/" +
          encodeURIComponent(projectId) +
          "/notification-channels/" +
          encodeURIComponent(definition.key),
      ),
    queryKey: ["notification", "channel", projectId, definition.key],
  });

  if (channel.isPending) {
    return <Card className="min-h-48 animate-pulse bg-muted/20" />;
  }
  if (channel.isError) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{definition.title}</CardTitle>
          <CardDescription>无法读取渠道状态。</CardDescription>
        </CardHeader>
      </Card>
    );
  }
  if (!canManage) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between gap-3 text-base">
            {definition.title}
            <Badge>{channel.data.enabled ? "已启用" : "未启用"}</Badge>
          </CardTitle>
          <CardDescription>
            {channel.data.configured ? "凭据已安全配置" : "尚未配置凭据"}
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  return (
    <ChannelEditor
      channel={channel.data}
      definition={definition}
      key={channel.data.settings_version}
      onChanged={() =>
        void queryClient.invalidateQueries({
          queryKey: ["notification", "channel", projectId, definition.key],
        })
      }
      projectId={projectId}
    />
  );
}

function ChannelEditor({
  channel,
  definition,
  onChanged,
  projectId,
}: Readonly<{
  channel: ChannelState;
  definition: ChannelDefinition;
  onChanged: () => void;
  projectId: string;
}>) {
  const [enabled, setEnabled] = useState(channel.enabled);
  const [endpoint, setEndpoint] = useState("");
  const [secret, setSecret] = useState("");
  const basePath =
    "/projects/" +
    encodeURIComponent(projectId) +
    "/notification-channels/" +
    encodeURIComponent(definition.key);
  const update = useMutation({
    mutationFn: () => {
      const values: Record<string, unknown> = { enabled };
      if (endpoint.trim()) values[definition.endpointKey] = endpoint.trim();
      if (definition.secretKey && secret.trim()) {
        values[definition.secretKey] = secret;
      }
      return apiClient.request(basePath, {
        body: { values },
        method: "PATCH",
      });
    },
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "渠道保存失败"),
    onSuccess: () => {
      setEndpoint("");
      setSecret("");
      onChanged();
      toast.success("外部渠道已保存");
    },
  });
  const test = useMutation({
    mutationFn: () => apiClient.request(basePath + "/test", { method: "POST" }),
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "连接测试失败"),
    onSuccess: () => toast.success("连接测试通过"),
  });
  const remove = useMutation({
    mutationFn: () => apiClient.request(basePath, { method: "DELETE" }),
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "渠道删除失败"),
    onSuccess: () => {
      onChanged();
      toast.success("外部渠道已删除");
    },
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center justify-between gap-3 text-base">
          {definition.title}
          <Badge>{channel.enabled ? "已启用" : "未启用"}</Badge>
        </CardTitle>
        <CardDescription>
          {channel.configured
            ? "凭据已保存；留空不会覆盖。Settings v" + channel.settings_version
            : "尚未配置；密钥保存后不会回显。"}
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
            启用此渠道
          </label>
          <Input
            aria-label={definition.title + " 地址"}
            onChange={(event) => setEndpoint(event.target.value)}
            placeholder={
              channel.configured ? "地址已保存，留空以保留" : "https://…"
            }
            type="url"
            value={endpoint}
          />
          {definition.secretKey ? (
            <Input
              aria-label={definition.title + " 签名密钥"}
              onChange={(event) => setSecret(event.target.value)}
              placeholder={
                channel.configured ? "密钥已保存，留空以保留" : "签名密钥"
              }
              type="password"
              value={secret}
            />
          ) : null}
          <div className="flex flex-wrap gap-2">
            <Button disabled={update.isPending} type="submit">
              保存渠道
            </Button>
            <Button
              disabled={!channel.configured || test.isPending}
              onClick={() => test.mutate()}
              type="button"
              variant="outline"
            >
              测试连接
            </Button>
            <Button
              disabled={!channel.configured || remove.isPending}
              onClick={() => remove.mutate()}
              type="button"
              variant="ghost"
            >
              删除渠道
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function ExternalRuleCard({ projectId }: Readonly<{ projectId: string }>) {
  const queryClient = useQueryClient();
  const rule = useQuery({
    queryFn: () =>
      apiClient.request<NotificationRule>(
        "/projects/" +
          encodeURIComponent(projectId) +
          "/notification-rules/progress.reminder.due",
      ),
    queryKey: ["notification", "rule", projectId, "progress.reminder.due"],
  });
  const channelQueries = useQueries({
    queries: channelTypes.map((definition) => ({
      queryFn: () =>
        apiClient.request<ChannelState>(
          "/projects/" +
            encodeURIComponent(projectId) +
            "/notification-channels/" +
            encodeURIComponent(definition.key),
        ),
      queryKey: ["notification", "channel", projectId, definition.key],
    })),
  });
  const channels = channelQueries.flatMap((query, index) =>
    query.data ? [{ definition: channelTypes[index], state: query.data }] : [],
  );

  return (
    <section aria-labelledby="external-rules-title" className="space-y-3">
      <div>
        <h3 className="font-semibold" id="external-rules-title">
          外部投递规则
        </h3>
        <p className="text-sm text-muted-foreground">
          规则只决定是否把允许外发的消息额外投递到项目渠道，不改变任何人的
          Inbox。
        </p>
      </div>
      {rule.isPending ? (
        <Card className="min-h-48 animate-pulse bg-muted/20" />
      ) : null}
      {rule.isError ? (
        <Card>
          <CardContent className="p-5 text-sm text-destructive">
            无法读取 Progress 提醒投递规则。
          </CardContent>
        </Card>
      ) : null}
      {rule.data ? (
        <ExternalRuleEditor
          channels={channels}
          key={rule.data.version}
          onSaved={(saved) => {
            queryClient.setQueryData(
              ["notification", "rule", projectId, "progress.reminder.due"],
              saved,
            );
          }}
          projectId={projectId}
          rule={rule.data}
        />
      ) : null}
    </section>
  );
}

function ExternalRuleEditor({
  channels,
  onSaved,
  projectId,
  rule,
}: Readonly<{
  channels: Array<{ definition: ChannelDefinition; state: ChannelState }>;
  onSaved: (rule: NotificationRule) => void;
  projectId: string;
  rule: NotificationRule;
}>) {
  const [externalEnabled, setExternalEnabled] = useState(rule.external_enabled);
  const [channelKeys, setChannelKeys] = useState(rule.channel_keys);
  const [minimumPriority, setMinimumPriority] = useState<
    NotificationRule["minimum_priority"]
  >(rule.minimum_priority);
  const save = useMutation({
    mutationFn: () =>
      apiClient.request<NotificationRule>(
        "/projects/" +
          encodeURIComponent(projectId) +
          "/notification-rules/progress.reminder.due",
        {
          body: {
            channel_keys: channelKeys,
            external_enabled: externalEnabled,
            minimum_priority: minimumPriority,
            version: rule.version,
          },
          method: "PUT",
        },
      ),
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "投递规则保存失败"),
    onSuccess: (saved) => {
      onSaved(saved);
      toast.success("外部投递规则已保存");
    },
  });

  function toggleChannel(key: string, checked: boolean) {
    setChannelKeys((current) =>
      checked
        ? Array.from(new Set([...current, key]))
        : current.filter((value) => value !== key),
    );
  }

  const canEnable = channels.some((channel) => channel.state.enabled);
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Progress 提醒</CardTitle>
        <CardDescription>
          Inbox 默认保留；这里可以额外选择已启用的项目外部渠道。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (externalEnabled && channelKeys.length === 0) {
              toast.error("启用外部投递前，请至少选择一个已启用渠道");
              return;
            }
            save.mutate();
          }}
        >
          <label className="flex items-center gap-2 text-sm font-medium">
            <input
              checked={externalEnabled}
              disabled={(!canEnable && !externalEnabled) || save.isPending}
              onChange={(event) => setExternalEnabled(event.target.checked)}
              type="checkbox"
            />
            额外发送到外部渠道
          </label>
          {!canEnable ? (
            <p className="text-sm text-muted-foreground">
              请先在上方配置并启用至少一个外部渠道。
            </p>
          ) : null}
          <fieldset className="space-y-2 text-sm">
            <legend className="font-medium">选择渠道</legend>
            {channels.map(({ definition, state }) => (
              <label className="flex items-center gap-2" key={definition.key}>
                <input
                  checked={channelKeys.includes(definition.key)}
                  disabled={
                    save.isPending ||
                    (!state.enabled && !channelKeys.includes(definition.key))
                  }
                  onChange={(event) =>
                    toggleChannel(definition.key, event.target.checked)
                  }
                  type="checkbox"
                />
                {definition.title}
                <span className="text-xs text-muted-foreground">
                  {state.enabled ? "已启用" : "不可用"}
                </span>
              </label>
            ))}
          </fieldset>
          <label className="grid max-w-xs gap-1.5 text-sm font-medium">
            最低优先级
            <select
              className="h-9 rounded-md border border-input bg-background px-3 text-sm"
              disabled={save.isPending}
              onChange={(event) =>
                setMinimumPriority(
                  event.target.value as NotificationRule["minimum_priority"],
                )
              }
              value={minimumPriority}
            >
              <option value="low">低</option>
              <option value="normal">普通</option>
              <option value="high">高</option>
              <option value="urgent">紧急</option>
            </select>
          </label>
          <Button disabled={save.isPending} type="submit">
            <Send aria-hidden="true" className="size-4" />
            保存投递规则
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function DeliveryDiagnostics({ projectId }: Readonly<{ projectId: string }>) {
  const deliveries = useInfiniteQuery({
    getNextPageParam: (lastPage: DeliveryPage) => lastPage.next_cursor,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      apiClient.request<DeliveryPage>(
        "/projects/" +
          encodeURIComponent(projectId) +
          "/notification-deliveries",
        { query: { cursor: pageParam, limit: 8 } },
      ),
    queryKey: ["notification", "deliveries", projectId],
  });
  const items = deliveries.data?.pages.flatMap((page) => page.items) ?? [];

  return (
    <section aria-labelledby="delivery-diagnostics-title" className="space-y-3">
      <div>
        <h3 className="font-semibold" id="delivery-diagnostics-title">
          外部投递诊断
        </h3>
        <p className="text-sm text-muted-foreground">
          只展示脱敏状态和安全错误。显式重投必须填写原因并写入 Audit。
        </p>
      </div>
      <Card>
        <CardContent className="p-5">
          {deliveries.isPending ? (
            <p className="text-sm text-muted-foreground">正在读取投递记录…</p>
          ) : null}
          {deliveries.isError ? (
            <p className="text-sm text-destructive">无法读取投递诊断。</p>
          ) : null}
          {!deliveries.isPending &&
          !deliveries.isError &&
          items.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              还没有外部投递记录。
            </p>
          ) : null}
          <ul className="space-y-3">
            {items.map((delivery) => (
              <DeliveryRow
                delivery={delivery}
                key={delivery.delivery_id}
                onRetried={() => void deliveries.refetch()}
                projectId={projectId}
              />
            ))}
          </ul>
          {deliveries.hasNextPage ? (
            <Button
              className="mt-4"
              disabled={deliveries.isFetchingNextPage}
              onClick={() => void deliveries.fetchNextPage()}
              variant="outline"
            >
              {deliveries.isFetchingNextPage ? "正在加载…" : "加载更多记录"}
            </Button>
          ) : null}
        </CardContent>
      </Card>
    </section>
  );
}

function DeliveryRow({
  delivery,
  onRetried,
  projectId,
}: Readonly<{
  delivery: Delivery;
  onRetried: () => void;
  projectId: string;
}>) {
  const [reason, setReason] = useState("");
  const retry = useMutation({
    mutationFn: () =>
      apiClient.request(
        "/projects/" +
          encodeURIComponent(projectId) +
          "/notification-deliveries/" +
          encodeURIComponent(delivery.delivery_id) +
          "/retry",
        { body: { reason: reason.trim() }, method: "POST" },
      ),
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "重投失败"),
    onSuccess: () => {
      setReason("");
      onRetried();
      toast.success("已创建新的投递尝试");
    },
  });
  const retryable =
    delivery.status === "failed" || delivery.status === "retrying";

  return (
    <li className="rounded-lg border border-border p-3 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="font-medium">
            {channelLabel(delivery.channel_key)} ·{" "}
            {statusLabel(delivery.status)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {new Date(delivery.created_at).toLocaleString()} · 已尝试{" "}
            {delivery.attempts} 次
            {delivery.last_error_code ? " · " + delivery.last_error_code : ""}
          </p>
        </div>
        {retryable ? (
          <div className="flex flex-wrap gap-2">
            <Input
              aria-label="重投原因"
              className="h-8 w-56"
              maxLength={1000}
              onChange={(event) => setReason(event.target.value)}
              placeholder="填写重投原因"
              value={reason}
            />
            <Button
              disabled={!reason.trim() || retry.isPending}
              onClick={() => retry.mutate()}
              size="sm"
              variant="outline"
            >
              <RotateCw aria-hidden="true" className="size-3" />
              重投
            </Button>
          </div>
        ) : null}
      </div>
      {delivery.last_error ? (
        <p className="mt-2 rounded bg-muted/50 p-2 text-xs text-muted-foreground">
          {delivery.last_error}
        </p>
      ) : null}
    </li>
  );
}

function channelLabel(key: string): string {
  return channelTypes.find((channel) => channel.key === key)?.title ?? key;
}

function statusLabel(status: string): string {
  return (
    {
      cancelled: "已取消",
      delivered: "已送达",
      failed: "失败",
      pending: "等待发送",
      retrying: "等待重试",
      sending: "发送中",
    }[status] ?? status
  );
}
