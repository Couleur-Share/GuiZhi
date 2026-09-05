import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.route("**/v1/meta", route => route.fulfill({ json: { protocol: 1 } }));
  await page.route("**/v1/session", route => route.fulfill({ json: { paired: true, deviceId: "synthetic" } }));
  await page.route("**/v1/history", route => route.fulfill({ json: [] }));
});
test("丢失响应后保留草稿和请求编号，确认接收后清空", async ({ page }) => {
  const requests: string[] = [];
  await page.route("**/v1/captures", async route => { requests.push(route.request().postDataJSON().requestId); if (requests.length === 1) await route.abort(); else await route.fulfill({ status: 201, json: { id: "receipt", state: "accepted" } }); });
  await page.goto("/"); await page.waitForFunction(() => !!navigator.serviceWorker.controller);
  await page.getByLabel("链接、分享口令或文字").fill("https://example.com/test?token=keep%2Fthis");
  await page.getByRole("button", { name: "发送到归知", exact: true }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByLabel("链接、分享口令或文字")).toHaveValue(/token=keep/);
  await page.reload(); await expect(page.getByLabel("链接、分享口令或文字")).toHaveValue(/token=keep/);
  await page.getByRole("button", { name: "发送到归知", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText(/服务端已接收/);
  await expect(page.getByLabel("链接、分享口令或文字")).toHaveValue(""); expect(requests[0]).toBe(requests[1]);
});
test("离线草稿、解析改判、中英文与深色布局", async ({ page, context }) => {
  await page.goto("/"); await page.waitForFunction(() => !!navigator.serviceWorker.controller);
  await page.getByLabel("链接、分享口令或文字").fill("明天看 https://example.com/a https://example.com/b");
  await expect(page.getByText("将创建 1 个采集项")).toBeVisible();
  await page.getByRole("button", { name: "采集链接", exact: true }).click();
  await expect(page.getByText("将创建 2 个采集项")).toBeVisible();
  await context.setOffline(true); await expect(page.getByText("当前离线，联网后请点击发送。")).toBeVisible();
  await expect(page.getByRole("button", { name: "发送到归知", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "切换主题" }).click(); await page.screenshot({ path: "test-results/capture-dark.png", fullPage: true });
  await page.getByRole("button", { name: "Language" }).click(); await expect(page.getByRole("heading", { name: /Save it now/ })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
test("系统分享 POST 保留多条草稿", async ({ page }) => {
  await page.goto("/"); await page.waitForFunction(() => !!navigator.serviceWorker.controller);
  await page.evaluate(async () => {
    for (const text of ["第一个分享 https://example.com/a", "第二个分享 https://example.com/b"]) {
      const form = new FormData(); form.set("text", text); await fetch("/share", { method: "POST", body: form });
    }
  });
  await page.reload(); await expect(page.getByText("第一个分享 https://example.com/a")).toBeVisible(); await expect(page.getByText("第二个分享 https://example.com/b")).toBeVisible();
});
