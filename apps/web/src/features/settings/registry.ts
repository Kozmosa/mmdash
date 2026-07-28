export type SettingsSlot = {
  description: string;
  id: string;
  order: number;
  owner: string;
  title: string;
};

export class SettingsSlotRegistry {
  private readonly slots = new Map<string, SettingsSlot>();

  register(slot: SettingsSlot): void {
    if (this.slots.has(slot.id)) {
      throw new Error(`Settings slot "${slot.id}" is already registered`);
    }
    this.slots.set(slot.id, { ...slot });
  }

  list(): readonly SettingsSlot[] {
    return [...this.slots.values()].sort(
      (left, right) =>
        left.order - right.order || left.id.localeCompare(right.id),
    );
  }
}

export const settingsSlots = new SettingsSlotRegistry();

[
  {
    id: "project",
    title: "Project",
    description: "题目、约束、成员和角色",
    owner: "project",
    order: 10,
  },
  {
    id: "repo",
    title: "Repo",
    description: "Provider、仓库、三个分支和 Webhook",
    owner: "repo",
    order: 20,
  },
  {
    id: "artifact",
    title: "Artifact",
    description: "对象存储和上传限制",
    owner: "artifact",
    order: 30,
  },
  {
    id: "agent",
    title: "Agent",
    description: "Hermes 和自动进度跟踪",
    owner: "agent",
    order: 40,
  },
  {
    id: "progress",
    title: "Progress",
    description: "提醒和外部通知渠道",
    owner: "progress",
    order: 50,
  },
  {
    id: "model",
    title: "Model",
    description: "Notion 来源与同步策略",
    owner: "model",
    order: 60,
  },
  {
    id: "article",
    title: "Article",
    description: "Zotero、LaTeX 模板和论文规范",
    owner: "article",
    order: 70,
  },
  {
    id: "experiment",
    title: "Experiment",
    description: "Box、Runtime、资源和超时",
    owner: "experiment",
    order: 80,
  },
  {
    id: "mcp-cli",
    title: "MCP / CLI",
    description: "Token、默认项目和仓库绑定",
    owner: "mcp",
    order: 90,
  },
].forEach((slot) => settingsSlots.register(slot));
