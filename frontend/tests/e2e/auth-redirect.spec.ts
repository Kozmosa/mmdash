import { expect, test } from "@playwright/test";

test("redirects unauthenticated users away from main routes", async ({ page }) => {
  await page.goto("/home");
  await expect(page).toHaveURL(/\/auth\/login/);
});
