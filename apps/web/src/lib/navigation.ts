import type { LucideIcon } from "lucide-react";
import {
  Bot,
  FilePenLine,
  Files,
  FlaskConical,
  Gauge,
  ListChecks,
  Settings,
  Waypoints,
} from "lucide-react";

export type WorkspaceRoute = {
  description: string;
  icon: LucideIcon;
  id:
    | "home"
    | "artifacts"
    | "agent"
    | "progress"
    | "models"
    | "article"
    | "experiments"
    | "settings";
  label: string;
  segment: string;
};

export const workspaceRoutes: readonly WorkspaceRoute[] = [
  {
    id: "home",
    label: "首页",
    description: "项目总览",
    segment: "",
    icon: Gauge,
  },
  {
    id: "agent",
    label: "mmdash Agent",
    description: "研究协作会话",
    segment: "agent",
    icon: Bot,
  },
  {
    id: "artifacts",
    label: "项目文件",
    description: "上传、版本、预览与回收站",
    segment: "artifacts",
    icon: Files,
  },
  {
    id: "progress",
    label: "进度跟踪",
    description: "节点与任务",
    segment: "progress",
    icon: ListChecks,
  },
  {
    id: "models",
    label: "模型版本",
    description: "快照与差异",
    segment: "models",
    icon: Waypoints,
  },
  {
    id: "article",
    label: "论文写作",
    description: "块级协作",
    segment: "article",
    icon: FilePenLine,
  },
  {
    id: "experiments",
    label: "求解记录",
    description: "实验与结果",
    segment: "experiments",
    icon: FlaskConical,
  },
  {
    id: "settings",
    label: "设置",
    description: "项目连接与偏好",
    segment: "settings",
    icon: Settings,
  },
] as const;

export function workspaceHref(projectId: string, segment: string): string {
  const base = `/projects/${encodeURIComponent(projectId)}`;
  return segment ? `${base}/${segment}` : base;
}
