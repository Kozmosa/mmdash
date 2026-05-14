import { expect, test } from "@playwright/test";
import {
  createCompetitionProblemFile,
  createTestRepo,
  mockModelAnalysisApis,
  registerAndCreateProject,
} from "./helpers";

test("follows a competition workflow from problem upload to model drafting and experiment execution", async ({
  page,
}) => {
  const repo = createTestRepo({ mismatchSolver: true });
  const problemFile = createCompetitionProblemFile();

  await mockModelAnalysisApis(page);
  await registerAndCreateProject(page, {
    teamName: "Competition Team",
    projectName: "Urban Relief Challenge",
  });

  await page.locator('input[type="file"]').setInputFiles(problemFile);
  await expect(page.getByText("urban-relief-brief.txt")).toBeVisible();

  await page.getByPlaceholder("添加 TODO...").fill("完成问题重述");
  await page.getByRole("button").filter({ has: page.locator("svg.lucide-plus") }).last().click();
  await expect(page.getByText("完成问题重述")).toBeVisible();

  await page.getByPlaceholder("添加 TODO...").fill("设计目标函数");
  await page.getByLabel("团队").click();
  await page.getByRole("button").filter({ has: page.locator("svg.lucide-plus") }).last().click();
  await expect(page.getByText("设计目标函数")).toBeVisible();
  await expect(page.locator("span").filter({ hasText: /^团队$/ })).toBeVisible();

  await page.goto("/model");
  await expect(page.getByRole("heading", { name: "还没有模型文档" })).toBeVisible();
  await page.getByRole("button", { name: "创建模型文档" }).click();
  await expect(page.getByText("文档已创建")).toBeVisible({ timeout: 10_000 });

  const editor = page.locator(".cm-content");
  await editor.click();
  await editor.pressSequentially(
    [
      "# 城市应急配送模型",
      "",
      "## 问题重述",
      "需要在 6 小时内完成 4 个社区的应急物资配送。",
      "",
      "## 基本假设",
      "- 每辆车只能服务一个社区后再返回",
      "- 道路平均通行时间在短时内稳定",
      "",
      "## 符号说明",
      "- x_i: 第 i 辆车是否启用",
      "- y_j: 第 j 个社区是否迟到",
      "",
      "## 目标函数",
      "$$",
      "min Z = \\sum_i c_i x_i + \\lambda \\sum_j d_j y_j",
      "$$",
      "",
      "## 实验计划",
      "通过 alpha 和 beta 参数测试迟到惩罚与负载平衡。",
      "",
    ].join("\n")
  );

  await expect(page.getByText("保存中...")).toBeVisible();
  await expect(page.getByText("已保存")).toBeVisible({ timeout: 10_000 });

  const toolButtons = page.locator("aside").filter({ hasText: "AI Tools" }).getByRole("button");
  await toolButtons.nth(0).click();
  await expect(page.getByText("车辆调度变量")).toBeVisible();

  await toolButtons.nth(1).click();
  await expect(page.getByText("问题重述 -> 假设 -> 目标函数 -> 实验计划")).toBeVisible();

  await toolButtons.nth(2).click();
  await expect(page.getByText("缺少灵敏度分析说明")).toBeVisible();

  await toolButtons.nth(3).click();
  await expect(page.getByText("该公式同时平衡车辆启用成本与社区迟到惩罚")).toBeVisible();

  await page.getByRole("button", { name: "版本控制" }).click();
  await page.getByPlaceholder("提交信息...").fill("v1 模型草稿");
  await page.getByRole("button", { name: "提交版本" }).click();
  await expect(page.getByText("版本已提交")).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText("v1 模型草稿")).toBeVisible();

  await page.goto("/experiment");
  await expect(page.getByText("状态: ready")).toBeVisible({ timeout: 20_000 });
  await expect(page.getByPlaceholder("Git 仓库路径").first()).toHaveValue("");

  await page.getByPlaceholder("Git 仓库路径").first().fill(repo.repoPath);
  await page.getByRole("button", { name: "扫描 Solver" }).click();
  await page.getByRole("button", { name: "solver_ok.py" }).click();
  await expect(page.getByText("alpha: 1 (number)")).toBeVisible();

  await page.getByPlaceholder("参数名").nth(1).fill("alpha");
  await page.getByPlaceholder("值，逗号分隔").fill("3,4");
  await page.getByRole("button", { name: "添加", exact: true }).click();
  await expect(page.getByText("alpha: [3, 4]")).toBeVisible();

  await page.getByRole("button", { name: "开始实验" }).click();
  await expect(page.getByText("实验结果与同步")).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText("运行次数: 4")).toBeVisible();

  await page.getByRole("tab", { name: "实验记录" }).click();
  await page.getByRole("button", { name: "刷新" }).click();
  await expect(page.getByText("solver_ok")).toBeVisible({ timeout: 10_000 });
});

test("uses timeline as an extended collaboration lane for the same competition story", async ({ page }) => {
  await registerAndCreateProject(page, {
    teamName: "Timeline Team",
    projectName: "Urban Relief Timeline",
  });

  await page.goto("/timeline");
  await expect(page.getByRole("heading", { name: "时间线" })).toBeVisible();

  await page.getByRole("button", { name: "添加日程" }).click();
  await page.getByLabel("标题").fill("题意澄清会");
  await page.getByLabel("描述").fill("明确社区优先级和迟到惩罚口径");
  await page.getByLabel("开始时间").fill("2026-05-13T09:00");
  await page.getByLabel("结束时间").fill("2026-05-13T09:30");
  await page.getByLabel("团队日程").click();
  await page.getByRole("button", { name: "添加" }).click();
  await expect(page.getByText("题意澄清会")).toBeVisible();

  await page.getByRole("button", { name: "添加日程" }).click();
  await page.getByLabel("标题").fill("模型定稿");
  await page.getByLabel("描述").fill("确认目标函数与约束条件");
  await page.getByLabel("开始时间").fill("2026-05-13T13:00");
  await page.getByLabel("结束时间").fill("2026-05-13T14:00");
  await page.getByRole("button", { name: "添加" }).click();
  await expect(page.getByText("模型定稿")).toBeVisible();

  await expect(page.getByText("2 个日程安排")).toBeVisible();
  await expect(page.getByText("团队", { exact: true })).toBeVisible();
});
