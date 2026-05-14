import { expect, test } from "@playwright/test";
import path from "path";
import { createFixtureFile, createTestRepo, registerAndCreateProject } from "./helpers";

test("supports pdf upload download delete and exposes repo-path discontinuity before experiment run", async ({
  page,
}) => {
  const pdfPath = createFixtureFile("problem.pdf", Buffer.from("%PDF-1.4\n% e2e pdf\n", "utf-8"));

  await registerAndCreateProject(page, {
    teamName: "Upload Flow Team",
    projectName: "Upload Flow Project",
  });

  const fileInput = page.locator('input[type="file"]');
  await fileInput.setInputFiles(pdfPath);
  await expect(page.getByText("problem.pdf")).toBeVisible();
  await expect(page.getByText("PDF", { exact: true })).toBeVisible();

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button").filter({ has: page.locator("svg.lucide-download") }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("problem.pdf");

  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button").filter({ has: page.locator("svg.lucide-trash2") }).click();
  await expect(page.getByText("problem.pdf")).toHaveCount(0);
  await expect(page.getByText("暂无已上传文件")).toBeVisible();

  await page.goto("/experiment");
  await expect(page.getByRole("heading", { name: "实验和求解" })).toBeVisible();
  await expect(page.getByText("Local Agent")).toBeVisible();
  await expect(page.getByPlaceholder("Git 仓库路径").first()).toHaveValue("");
});

test("runs a small experiment end to end and can inspect experiment history", async ({ page }) => {
  const repo = createTestRepo({ mismatchSolver: true });

  await registerAndCreateProject(page, {
    teamName: "Experiment Team",
    projectName: "Experiment Project",
  });

  await page.goto("/experiment");
  await expect(page.getByText("状态: ready")).toBeVisible({ timeout: 20_000 });

  await page.getByPlaceholder("Git 仓库路径").first().fill(repo.repoPath);
  await page.getByRole("button", { name: "扫描 Solver" }).click();
  await expect(page.getByText("solver_ok.py")).toBeVisible();

  await page.getByRole("button", { name: "solver_ok.py" }).click();
  await expect(page.getByText("alpha: 1 (number)")).toBeVisible();
  await expect(page.getByText("beta: 2 (number)")).toBeVisible();

  await page.getByPlaceholder("参数名").nth(1).fill("alpha");
  await page.getByPlaceholder("值，逗号分隔").fill("3,4");
  await page.getByRole("button", { name: "添加", exact: true }).click();
  await expect(page.getByText("alpha: [3, 4]")).toBeVisible();

  await page.getByRole("button", { name: "开始实验" }).click();
  await expect(page.getByText("实验结果与同步")).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText("success")).toBeVisible();
  await expect(page.getByText("运行次数: 4")).toBeVisible();
  await expect(page.locator("pre").filter({ hasText: '"beta_env": "2"' })).toHaveCount(2);
  await expect(page.locator("pre").filter({ hasText: '"beta_env": "4"' })).toHaveCount(2);

  await page.getByRole("tab", { name: "实验记录" }).click();
  await page.getByRole("button", { name: "刷新" }).click();
  await expect(page.getByText("solver_ok")).toBeVisible({ timeout: 10_000 });
});

test("shows clear failures for invalid repo path and failing solver", async ({ page }) => {
  const repo = createTestRepo({ failingSolver: true });

  await registerAndCreateProject(page, {
    teamName: "Failure Team",
    projectName: "Failure Project",
  });

  await page.goto("/experiment");
  await expect(page.getByText("状态: ready")).toBeVisible({ timeout: 20_000 });

  await page.getByPlaceholder("Git 仓库路径").first().fill("/nonexistent/repo");
  await page.getByRole("button", { name: "扫描 Solver" }).click();
  await expect(page.getByText("Invalid repository path")).toBeVisible();

  await page.getByPlaceholder("/path/to/solver.py").fill(repo.failingSolverPath);
  await page.getByPlaceholder("/path/to/repo").fill(repo.repoPath);
  await page.getByRole("button", { name: "开始实验" }).click();

  await expect(page.getByText("error")).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText("intentional failure", { exact: true })).toBeVisible();
});
