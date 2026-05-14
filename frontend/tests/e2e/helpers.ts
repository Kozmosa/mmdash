import { expect, Page } from "@playwright/test";
import fs from "fs";
import os from "os";
import path from "path";
import { execFileSync } from "child_process";

export function createTestRepo(options?: {
  slowSolver?: boolean;
  failingSolver?: boolean;
  mismatchSolver?: boolean;
}) {
  const repoPath = fs.mkdtempSync(path.join(os.tmpdir(), "mmdash-e2e-repo-"));
  execFileSync("git", ["init"], { cwd: repoPath });
  execFileSync("git", ["config", "user.email", "e2e@example.com"], { cwd: repoPath });
  execFileSync("git", ["config", "user.name", "E2E"], { cwd: repoPath });

  const okSolverPath = path.join(repoPath, "solver_ok.py");
  fs.writeFileSync(
    okSolverPath,
    [
      "import json",
      "import os",
      "",
      "alpha = 1",
      "beta = 2",
      "",
      "if __name__ == '__main__':",
      "    payload = {",
      "        'alpha_env': os.getenv('alpha'),",
      "        'beta_env': os.getenv('beta'),",
      "        'alpha_static': alpha,",
      "        'beta_static': beta,",
      "    }",
      "    print(json.dumps(payload, ensure_ascii=False))",
      "",
    ].join("\n"),
    "utf-8"
  );

  if (options?.slowSolver) {
    fs.writeFileSync(
      path.join(repoPath, "solver_slow.py"),
      ["import time", "", "alpha = 1", "", "if __name__ == '__main__':", "    time.sleep(130)", ""].join(
        "\n"
      ),
      "utf-8"
    );
  }

  if (options?.failingSolver) {
    fs.writeFileSync(
      path.join(repoPath, "solver_fail.py"),
      [
        "import sys",
        "",
        "alpha = 1",
        "",
        "if __name__ == '__main__':",
        "    print('intentional failure', file=sys.stderr)",
        "    raise SystemExit(3)",
        "",
      ].join("\n"),
      "utf-8"
    );
  }

  if (options?.mismatchSolver) {
    fs.writeFileSync(
      path.join(repoPath, "solver_mismatch.py"),
      [
        "threshold = 10",
        "",
        "if __name__ == '__main__':",
        "    print(f'threshold_static={threshold}')",
        "",
      ].join("\n"),
      "utf-8"
    );
  }

  fs.writeFileSync(path.join(repoPath, "README.md"), "# e2e fixture\n", "utf-8");
  execFileSync("git", ["add", "."], { cwd: repoPath });
  execFileSync("git", ["commit", "-m", "init"], { cwd: repoPath });

  return {
    repoPath,
    okSolverPath,
    slowSolverPath: path.join(repoPath, "solver_slow.py"),
    failingSolverPath: path.join(repoPath, "solver_fail.py"),
    mismatchSolverPath: path.join(repoPath, "solver_mismatch.py"),
  };
}

export async function registerAndCreateProject(page: Page, options?: { teamName?: string; projectName?: string }) {
  const email = `e2e_${Date.now()}_${Math.random().toString(36).slice(2, 8)}@example.com`;
  const teamName = options?.teamName ?? "E2E Team";
  const projectName = options?.projectName ?? "E2E Project";
  const teamCard = page.getByText("我的团队").locator("..").locator("..");

  await page.goto("/auth/register");
  await page.locator("#email").fill(email);
  await page.locator("#password").fill("testpass123");
  await page.getByRole("button", { name: "注册" }).click();
  await expect(page).toHaveURL(/\/home/);

  await teamCard.getByRole("button", { name: "创建", exact: true }).click();
  await page.locator("#teamName").fill(teamName);
  await page.getByRole("button", { name: "创建团队" }).click();

  await page.getByRole("button", { name: "创建项目" }).click();
  await expect(page.getByText("团队文档后端")).toBeVisible();
  await page.locator("#projectName").fill(projectName);
  await page.getByRole("button", { name: "创建项目" }).last().click();
  await expect(page.getByText(`当前项目: ${projectName}`)).toBeVisible();
}

export function createFixtureFile(name: string, content: string | Buffer) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "mmdash-e2e-file-"));
  const filePath = path.join(root, name);
  fs.writeFileSync(filePath, content);
  return filePath;
}

export function createCompetitionProblemFile() {
  return createFixtureFile(
    "urban-relief-brief.txt",
    [
      "题目：城市应急物资配送优化",
      "",
      "背景：某市在暴雨后需要在 6 小时内向 4 个社区配送应急物资。",
      "目标：在满足基本需求的前提下，平衡总配送成本与迟到惩罚。",
      "要求：建立调度模型，说明关键参数，并完成灵敏度分析。",
      "",
      "评估指标：",
      "1. 总配送时间",
      "2. 车辆负载平衡",
      "3. 社区迟到惩罚",
      "",
    ].join("\n")
  );
}

export async function mockModelAnalysisApis(page: Page) {
  await page.route("**/api/model/**/analyze/symbols", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        symbols: [
          { symbol: "x_i", type: "variable", meaning: "车辆调度变量" },
          { symbol: "y_j", type: "penalty", meaning: "社区迟到指示变量" },
        ],
        disclaimer: "仅供参考",
      }),
    });
  });

  await page.route("**/api/model/**/analyze/structure", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        structure: {
          summary: "文档已形成问题重述、假设、目标函数与实验计划的基本链路。",
          sections: ["问题重述", "基本假设", "符号说明", "目标函数", "实验计划"],
          problem_relationship: "问题重述 -> 假设 -> 目标函数 -> 实验计划",
        },
        disclaimer: "仅供参考",
      }),
    });
  });

  await page.route("**/api/model/**/analyze/errors", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        errors: [
          {
            excerpt: "实验计划",
            description: "缺少灵敏度分析说明",
            severity: "warning",
          },
        ],
        disclaimer: "仅供参考",
      }),
    });
  });

  await page.route("**/api/model/**/analyze/formula*", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        explanation: "该公式同时平衡车辆启用成本与社区迟到惩罚。",
        disclaimer: "仅供参考",
      }),
    });
  });
}
