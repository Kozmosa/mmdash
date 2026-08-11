"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Save } from "lucide-react";
import { type FormEvent, useState } from "react";
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
import { agentApi } from "@/features/agent/agent-api";
import { apiClient } from "@/lib/api-client";

import type { ProgressSettings } from "./types";

const manageRoles = new Set(["owner", "maintainer", "editor"]);

type SettingsForm = Pick<
  ProgressSettings,
  | "agent_instance_id"
  | "auto_task_changes"
  | "auto_tracking_enabled"
  | "cron_enabled"
  | "cron_schedule"
  | "debounce_seconds"
  | "event_triggers_enabled"
  | "min_interval_seconds"
  | "reasoning_effort"
>;

export function ProgressSettingsPanel() {
  const project = useCurrentProject();
  const canManage = manageRoles.has(project.role ?? "");
  const path = `/projects/${encodeURIComponent(project.id)}/progress/settings`;
  const settings = useQuery({
    queryFn: () => apiClient.request<ProgressSettings>(path),
    queryKey: ["progress-settings", project.id],
  });
  const agents = useQuery({
    enabled: canManage,
    queryFn: () => agentApi.listInstances(project.id),
    queryKey: ["agent-instances", project.id],
  });

  return (
    <section aria-labelledby="progress-settings-title" className="space-y-4">
      <div>
        <h2
          className="flex items-center gap-2 text-lg font-semibold"
          id="progress-settings-title"
        >
          <Bot aria-hidden="true" className="size-5" />
          Progress · 自动评估
        </h2>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
          配置领域事件和定时计划如何触发项目进度评估，以及评估可以自动应用哪些普通
          TODO 变化。
        </p>
      </div>

      {settings.isPending ? (
        <Card className="min-h-48 animate-pulse bg-muted/20" />
      ) : settings.isError ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">无法读取自动评估设置</CardTitle>
            <CardDescription>请刷新页面后重试。</CardDescription>
          </CardHeader>
        </Card>
      ) : canManage ? (
        <ProgressSettingsEditor
          activeAgents={(agents.data?.items ?? []).filter(
            (agent) =>
              agent.status === "active" && agent.grant.status === "active",
          )}
          initialSettings={settings.data}
          key={settings.data.updated_at}
          projectId={project.id}
        />
      ) : (
        <ReadOnlySettings settings={settings.data} />
      )}
    </section>
  );
}

function ProgressSettingsEditor({
  activeAgents,
  initialSettings,
  projectId,
}: Readonly<{
  activeAgents: Array<{ agent_instance_id: string; display_name: string }>;
  initialSettings: ProgressSettings;
  projectId: string;
}>) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState<SettingsForm>(() =>
    settingsForm(initialSettings),
  );
  const update = useMutation({
    mutationFn: () =>
      apiClient.request<ProgressSettings>(
        `/projects/${encodeURIComponent(projectId)}/progress/settings`,
        { body: settingsRequest(form), method: "PATCH" },
      ),
    onError: () =>
      toast.error("无法保存自动评估设置，请检查 Agent、Cron 和时间参数。"),
    onSuccess: async (saved) => {
      queryClient.setQueryData(["progress-settings", projectId], saved);
      await queryClient.invalidateQueries({
        queryKey: ["progress", projectId],
      });
      toast.success("自动评估设置已保存。");
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (form.auto_tracking_enabled && !form.agent_instance_id) {
      toast.error(
        "启用自动评估前请选择一个处于 active 状态的 Progress Agent。",
      );
      return;
    }
    if (form.cron_enabled && !form.agent_instance_id) {
      toast.error(
        "启用定时评估前请选择一个处于 active 状态的 Progress Agent。",
      );
      return;
    }
    if (form.cron_schedule.trim().split(/\s+/).length !== 5) {
      toast.error("Cron 必须使用标准 5 字段表达式。");
      return;
    }
    update.mutate();
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="text-base">自动评估与调度</CardTitle>
            <CardDescription className="mt-1">
              保存后，后台会异步同步定时计划；Progress
              页面继续提供手动立即评估。
            </CardDescription>
          </div>
          <Badge>{form.auto_tracking_enabled ? "已启用" : "未启用"}</Badge>
        </div>
      </CardHeader>
      <CardContent>
        <form className="space-y-5" onSubmit={submit}>
          <div className="divide-y divide-border rounded-xl border border-border">
            <SettingSwitch
              checked={form.auto_tracking_enabled}
              description="允许领域事件或计划任务安排自动进度评估。"
              label="启用自动进度评估"
              onChange={(checked) =>
                setForm((current) => ({
                  ...current,
                  auto_tracking_enabled: checked,
                }))
              }
            />
            <SettingSwitch
              checked={form.event_triggers_enabled}
              description="Repo、Artifact、Model 等领域发生变化时触发防抖评估。"
              label="消费领域事件"
              onChange={(checked) =>
                setForm((current) => ({
                  ...current,
                  event_triggers_enabled: checked,
                }))
              }
            />
            <SettingSwitch
              checked={form.cron_enabled}
              description="由 mmdash 按 Cron 表达式定期创建评估；所选 Agent 只负责执行评估 Run。"
              label="启用定时评估"
              onChange={(checked) =>
                setForm((current) => ({ ...current, cron_enabled: checked }))
              }
            />
            <SettingSwitch
              checked={form.auto_task_changes}
              description="允许评估直接应用普通 TODO 状态变化；关键节点仍需人工确认。"
              label="允许 Agent 自动调整普通 TODO"
              onChange={(checked) =>
                setForm((current) => ({
                  ...current,
                  auto_task_changes: checked,
                }))
              }
            />
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <label className="space-y-2 text-sm">
              <span className="font-medium">Progress Agent</span>
              <select
                aria-label="设置 Progress Agent"
                className={selectClass}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    agent_instance_id: event.target.value || undefined,
                  }))
                }
                value={form.agent_instance_id ?? ""}
              >
                <option value="">未选择</option>
                {activeAgents.map((agent) => (
                  <option
                    key={agent.agent_instance_id}
                    value={agent.agent_instance_id}
                  >
                    {agent.display_name}
                  </option>
                ))}
              </select>
            </label>
            <label className="space-y-2 text-sm">
              <span className="font-medium">Cron（5 字段）</span>
              <Input
                aria-label="Progress Cron"
                maxLength={100}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    cron_schedule: event.target.value,
                  }))
                }
                value={form.cron_schedule}
              />
            </label>
            <label className="space-y-2 text-sm">
              <span className="font-medium">评估思考强度</span>
              <select
                aria-label="Progress 评估思考强度"
                className={selectClass}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    reasoning_effort: event.target
                      .value as SettingsForm["reasoning_effort"],
                  }))
                }
                value={form.reasoning_effort}
              >
                <option value="minimal">最低</option>
                <option value="low">低</option>
                <option value="medium">中</option>
                <option value="high">高</option>
                <option value="xhigh">极高</option>
              </select>
              <span className="block text-xs text-muted-foreground">
                仅控制评估 Run 的模型推理预算；隐藏推理文本不会展示。
              </span>
            </label>
            <NumberField
              label="防抖秒数"
              max={3_600}
              onChange={(value) =>
                setForm((current) => ({
                  ...current,
                  debounce_seconds: value,
                }))
              }
              value={form.debounce_seconds}
            />
            <NumberField
              label="最短评估间隔（秒）"
              max={86_400}
              onChange={(value) =>
                setForm((current) => ({
                  ...current,
                  min_interval_seconds: value,
                }))
              }
              value={form.min_interval_seconds}
            />
          </div>

          <div className="rounded-lg border border-border bg-muted/30 p-3 text-xs text-muted-foreground">
            评估器：{initialSettings.evaluator_mode} · 调度器：mmdash
            {initialSettings.cron_next_run_at
              ? ` · 下次 ${new Date(initialSettings.cron_next_run_at).toLocaleString()}`
              : " · 未安排定时评估"}
          </div>
          <Button disabled={update.isPending} type="submit">
            <Save aria-hidden="true" className="size-4" />
            {update.isPending ? "正在保存…" : "保存自动评估设置"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function ReadOnlySettings({
  settings,
}: Readonly<{ settings: ProgressSettings }>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center justify-between gap-3 text-base">
          自动评估与调度
          <Badge>{settings.auto_tracking_enabled ? "已启用" : "未启用"}</Badge>
        </CardTitle>
        <CardDescription>
          你可以查看配置，但当前角色不能修改项目设置。
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3 text-sm sm:grid-cols-2">
        <SettingValue
          label="领域事件"
          value={settings.event_triggers_enabled ? "已启用" : "未启用"}
        />
        <SettingValue
          label="定时评估"
          value={settings.cron_enabled ? settings.cron_schedule : "未启用"}
        />
        <SettingValue
          label="自动 TODO 变化"
          value={settings.auto_task_changes ? "允许" : "需人工确认"}
        />
        <SettingValue
          label="下次定时评估"
          value={formatScheduleTime(settings.cron_next_run_at)}
        />
      </CardContent>
    </Card>
  );
}

function SettingSwitch({
  checked,
  description,
  label,
  onChange,
}: Readonly<{
  checked: boolean;
  description: string;
  label: string;
  onChange: (checked: boolean) => void;
}>) {
  return (
    <label className="flex cursor-pointer items-center justify-between gap-4 p-4">
      <span>
        <span className="block text-sm font-medium">{label}</span>
        <span className="mt-1 block text-xs text-muted-foreground">
          {description}
        </span>
      </span>
      <input
        aria-label={label}
        checked={checked}
        className="peer sr-only"
        onChange={(event) => onChange(event.target.checked)}
        role="switch"
        type="checkbox"
      />
      <span className="relative h-6 w-11 shrink-0 rounded-full bg-muted-foreground/30 transition-colors after:absolute after:left-1 after:top-1 after:size-4 after:rounded-full after:bg-background after:shadow-sm after:transition-transform peer-checked:bg-primary peer-checked:after:translate-x-5 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-ring" />
    </label>
  );
}

function NumberField({
  label,
  max,
  onChange,
  value,
}: Readonly<{
  label: string;
  max: number;
  onChange: (value: number) => void;
  value: number;
}>) {
  return (
    <label className="space-y-2 text-sm">
      <span className="font-medium">{label}</span>
      <Input
        aria-label={label}
        max={max}
        min={0}
        onChange={(event) => onChange(Number(event.target.value))}
        type="number"
        value={value}
      />
    </label>
  );
}

function SettingValue({
  label,
  value,
}: Readonly<{ label: string; value: string }>) {
  return (
    <div className="rounded-lg border border-border p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 font-medium">{value}</p>
    </div>
  );
}

function settingsForm(value: ProgressSettings): SettingsForm {
  return {
    agent_instance_id: value.agent_instance_id,
    auto_task_changes: value.auto_task_changes,
    auto_tracking_enabled: value.auto_tracking_enabled,
    cron_enabled: value.cron_enabled,
    cron_schedule: value.cron_schedule,
    debounce_seconds: value.debounce_seconds,
    event_triggers_enabled: value.event_triggers_enabled,
    min_interval_seconds: value.min_interval_seconds,
    reasoning_effort: value.reasoning_effort ?? "medium",
  };
}

function settingsRequest(value: SettingsForm) {
  return {
    agent_instance_id: value.agent_instance_id || undefined,
    auto_task_changes: value.auto_task_changes,
    auto_tracking_enabled: value.auto_tracking_enabled,
    cron_enabled: value.cron_enabled,
    cron_schedule: value.cron_schedule,
    debounce_seconds: value.debounce_seconds,
    event_triggers_enabled: value.event_triggers_enabled,
    min_interval_seconds: value.min_interval_seconds,
    reasoning_effort: value.reasoning_effort,
  };
}

function formatScheduleTime(value?: string): string {
  return value ? new Date(value).toLocaleString() : "未安排";
}

const selectClass =
  "flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50";
