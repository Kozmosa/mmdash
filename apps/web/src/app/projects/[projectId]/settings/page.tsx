"use client";

import { useEffect, useState } from "react";
import {
  Settings,
  GitBranch,
  Waypoints,
  Bot,
  ListChecks,
  Bell,
  FlaskConical,
  FilePenLine,
  Users,
  Sliders,
} from "lucide-react";

import { SettingsSlotGrid } from "@/features/settings/settings-slot-grid";
import { RegisteredSettingsPanel } from "@/features/settings/registered-settings-panel";
import { settingsSlots } from "@/features/settings/registry";
import { MemberManagement } from "@/features/members/member-management";
import { NotificationSettingsPanel } from "@/features/notification/notification-settings-panel";
import { RepoSettingsPanel } from "@/features/repo/repo-settings-panel";
import { ModelSettingsPanel } from "@/features/model/model-settings-panel";
import { AgentSettingsPanel } from "@/features/agent/agent-settings-panel";
import { ProgressSettingsPanel } from "@/features/progress/progress-settings-panel";
import { ExperimentSettingsPanel } from "@/features/experiment/experiment-settings-panel";
import { ProjectBoxSettingsPanel } from "@/features/experiment/project-box-settings-panel";
import { ArticleSettingsPanel } from "@/features/article/article-settings-panel";
import { cn } from "@/lib/cn";

type TabId =
  | "repo"
  | "model"
  | "agent"
  | "progress"
  | "notification"
  | "sandbox"
  | "article"
  | "members"
  | "advanced";

type TabConfig = {
  id: TabId;
  label: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
};

const tabsConfig: TabConfig[] = [
  {
    id: "repo",
    label: "代码仓库",
    description: "管理 Git 仓库绑定与逻辑工作区分支映射",
    icon: GitBranch,
  },
  {
    id: "model",
    label: "模型来源",
    description: "授权并配置 Notion 根页面作为模型题目来源",
    icon: Waypoints,
  },
  {
    id: "agent",
    label: "智能助手",
    description: "配置 Hermes Agent 连接、API 密钥与 MCP 权限",
    icon: Bot,
  },
  {
    id: "progress",
    label: "自动评估",
    description: "管理自动评估规则、触发时机与任务状态更新策略",
    icon: ListChecks,
  },
  {
    id: "notification",
    label: "通知渠道",
    description: "配置 Webhook 等外部通知投递规则与连接测试",
    icon: Bell,
  },
  {
    id: "sandbox",
    label: "沙箱与实验",
    description: "管理分配的 Sandbox Runtimes 与实验资源超时策略",
    icon: FlaskConical,
  },
  {
    id: "article",
    label: "论文写作",
    description: "管理论文写作协作规范与文档发布分支设置",
    icon: FilePenLine,
  },
  {
    id: "members",
    label: "成员管理",
    description: "邀请项目成员、管理角色权限与协作历史",
    icon: Users,
  },
  {
    id: "advanced",
    label: "扩展配置",
    description: "查看已注册的配置插槽与插件类型的契约状态",
    icon: Sliders,
  },
];

export default function SettingsPage() {
  const [activeTab, setActiveTab] = useState<TabId>("repo");

  useEffect(() => {
    const handleHashChange = () => {
      const hash = window.location.hash;
      if (hash === "#agent-settings") {
        setActiveTab("agent");
      } else if (hash === "#model-settings") {
        setActiveTab("model");
      } else if (hash === "#repo-settings") {
        setActiveTab("repo");
      } else if (hash === "#progress-settings") {
        setActiveTab("progress");
      } else if (hash === "#notification-settings") {
        setActiveTab("notification");
      } else if (hash === "#sandbox-settings") {
        setActiveTab("sandbox");
      } else if (hash === "#article-settings") {
        setActiveTab("article");
      } else if (hash === "#members-settings") {
        setActiveTab("members");
      } else if (hash === "#advanced-settings") {
        setActiveTab("advanced");
      }
    };

    handleHashChange();
    window.addEventListener("hashchange", handleHashChange);
    return () => window.removeEventListener("hashchange", handleHashChange);
  }, []);

  const changeTab = (tabId: TabId) => {
    setActiveTab(tabId);
    window.history.replaceState(null, "", `#${tabId}-settings`);
  };

  return (
    <section className="space-y-6" aria-labelledby="settings-title">
      <header>
        <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs">
          <Settings aria-hidden="true" className="size-5" />
        </div>
        <h1
          className="text-2xl font-semibold tracking-tight"
          id="settings-title"
        >
          项目设置
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          在这里管理项目配置、连接扩展、自动化工作流和成员权限。
        </p>
      </header>

      {/* Tabs Layout */}
      <div className="flex flex-col md:grid md:grid-cols-[220px_1fr] md:gap-8 items-start">
        {/* Mobile Horizontal Tabs */}
        <div className="flex w-full overflow-x-auto pb-2 -mx-4 px-4 md:hidden gap-1 border-b border-border mb-4 scrollbar-none">
          {tabsConfig.map((tab) => {
            const Icon = tab.icon;
            const active = activeTab === tab.id;
            return (
              <button
                key={tab.id}
                onClick={() => changeTab(tab.id)}
                className={cn(
                  "flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium whitespace-nowrap transition-colors",
                  active
                    ? "bg-secondary text-secondary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                )}
              >
                <Icon className="size-4 shrink-0" />
                <span>{tab.label}</span>
              </button>
            );
          })}
        </div>

        {/* Desktop Sidebar Navigation */}
        <aside className="hidden md:flex flex-col space-y-1 w-full border-r border-border/40 pr-4 h-fit">
          {tabsConfig.map((tab) => {
            const Icon = tab.icon;
            const active = activeTab === tab.id;
            return (
              <button
                key={tab.id}
                onClick={() => changeTab(tab.id)}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors text-left w-full",
                  active
                    ? "bg-secondary text-secondary-foreground"
                    : "text-muted-foreground hover:bg-muted/70 hover:text-foreground"
                )}
              >
                <Icon className="size-4 shrink-0" />
                <span>{tab.label}</span>
              </button>
            );
          })}
        </aside>

        {/* Tab Content Panel */}
        <div className="min-w-0 flex-1 w-full space-y-6">
          {/* Repo Tab */}
          <div
            id="repo-settings"
            className={cn("scroll-mt-6 space-y-4", activeTab !== "repo" && "hidden")}
          >
            <div className="border-b border-border pb-4 mb-4">
              <h2 className="text-xl font-semibold tracking-tight">代码仓库</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                管理 Git 仓库绑定与逻辑工作区分支映射
              </p>
            </div>
            <RepoSettingsPanel />
          </div>

          {/* Model Tab */}
          <div
            id="model-settings"
            className={cn("scroll-mt-6 space-y-4", activeTab !== "model" && "hidden")}
          >
            <div className="border-b border-border pb-4 mb-4">
              <h2 className="text-xl font-semibold tracking-tight">模型来源</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                授权并配置 Notion 根页面作为模型题目来源
              </p>
            </div>
            <ModelSettingsPanel />
          </div>

          {/* Agent Tab */}
          <div
            id="agent-settings"
            className={cn("scroll-mt-6 space-y-4", activeTab !== "agent" && "hidden")}
          >
            <div className="border-b border-border pb-4 mb-4">
              <h2 className="text-xl font-semibold tracking-tight">智能助手</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                配置 Hermes Agent 连接、API 密钥与 MCP 权限
              </p>
            </div>
            <AgentSettingsPanel />
          </div>

          {/* Progress Tab */}
          <div
            id="progress-settings"
            className={cn("scroll-mt-6 space-y-4", activeTab !== "progress" && "hidden")}
          >
            <div className="border-b border-border pb-4 mb-4">
              <h2 className="text-xl font-semibold tracking-tight">自动评估</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                管理自动评估规则、触发时机与任务状态更新策略
              </p>
            </div>
            <ProgressSettingsPanel />
          </div>

          {/* Notification Tab */}
          <div
            id="notification-settings"
            className={cn("scroll-mt-6 space-y-4", activeTab !== "notification" && "hidden")}
          >
            <div className="border-b border-border pb-4 mb-4">
              <h2 className="text-xl font-semibold tracking-tight">通知渠道</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                配置 Webhook 等外部通知投递规则与连接测试
              </p>
            </div>
            <NotificationSettingsPanel />
          </div>

          {/* Sandbox Tab */}
          <div
            id="sandbox-settings"
            className={cn("scroll-mt-6 space-y-4", activeTab !== "sandbox" && "hidden")}
          >
            <div className="border-b border-border pb-4 mb-4">
              <h2 className="text-xl font-semibold tracking-tight">沙箱与实验</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                管理分配的 Sandbox Runtimes 与实验资源超时策略
              </p>
            </div>
            <ProjectBoxSettingsPanel />
            <ExperimentSettingsPanel />
          </div>

          {/* Article Tab */}
          <div
            id="article-settings"
            className={cn("scroll-mt-6 space-y-4", activeTab !== "article" && "hidden")}
          >
            <div className="border-b border-border pb-4 mb-4">
              <h2 className="text-xl font-semibold tracking-tight">论文写作</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                管理论文写作协作规范与文档发布分支设置
              </p>
            </div>
            <ArticleSettingsPanel />
          </div>

          {/* Members Tab */}
          <div
            id="members-settings"
            className={cn("scroll-mt-6 space-y-4", activeTab !== "members" && "hidden")}
          >
            <div className="border-b border-border pb-4 mb-4">
              <h2 className="text-xl font-semibold tracking-tight">成员管理</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                邀请项目成员、管理角色权限与协作历史
              </p>
            </div>
            <MemberManagement />
          </div>

          {/* Advanced Tab */}
          <div
            id="advanced-settings"
            className={cn("scroll-mt-6 space-y-4", activeTab !== "advanced" && "hidden")}
          >
            <div className="border-b border-border pb-4 mb-4">
              <h2 className="text-xl font-semibold tracking-tight">扩展配置</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                查看已注册的配置插槽与插件类型的契约状态
              </p>
            </div>
            <section className="space-y-3" aria-labelledby="settings-slots-title">
              <div>
                <h3 className="text-lg font-semibold" id="settings-slots-title">
                  插槽绑定列表
                </h3>
                <p className="text-sm text-muted-foreground">
                  未明确内置渲染的部分插槽状态列表
                </p>
              </div>
              <SettingsSlotGrid
                slots={settingsSlots
                  .list()
                  .filter(
                    (slot) =>
                      slot.id !== "repo" &&
                      slot.id !== "model" &&
                      slot.id !== "agent" &&
                      slot.id !== "progress" &&
                      slot.id !== "experiment" &&
                      slot.id !== "article"
                  )}
              />
            </section>
            <section className="space-y-3" aria-labelledby="registered-settings-title-section">
              <div>
                <h3 className="text-lg font-semibold" id="registered-settings-title-section">
                  已注册配置类型
                </h3>
                <p className="text-sm text-muted-foreground">
                  字段契约、密钥标记和连接测试能力由所属模块注册
                </p>
              </div>
              <RegisteredSettingsPanel />
            </section>
          </div>
        </div>
      </div>
    </section>
  );
}
