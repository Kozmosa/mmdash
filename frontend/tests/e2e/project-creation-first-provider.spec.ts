import { expect, test } from "@playwright/test";

test("first project creation requires explicit provider selection", async ({ page }) => {
  const email = `e2e_${Date.now()}@example.com`;
  const teamCard = page.getByText("我的团队").locator("..").locator("..");

  await page.goto("/auth/register");
  await page.locator("#email").fill(email);
  await page.locator("#password").fill("testpass123");
  await page.getByRole("button", { name: "注册" }).click();
  await expect(page).toHaveURL(/\/home/);

  await teamCard.getByRole("button", { name: "创建", exact: true }).click();
  await page.locator("#teamName").fill("Playwright Team");
  await page.getByRole("button", { name: "创建团队" }).click();

  await page.getByRole("button", { name: "创建项目" }).click();
  await expect(page.getByText("团队文档后端")).toBeVisible();
  await page.locator("#projectName").fill("Playwright Project");
});
