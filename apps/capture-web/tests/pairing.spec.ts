import { test, expect, type Page } from "@playwright/test";

async function pairingPage(page: Page, pending = false) {
  if (pending) await page.addInitScript(() => sessionStorage.setItem("pairing-pending", "synthetic"));
  const state = { claimed: pending, confirmed: false, unavailable: false, sessions: 0, claims: 0 };
  await page.route("**/v1/meta", route => route.fulfill({ json: { protocol: 1 } }));
  await page.route("**/v1/history", route => route.fulfill({ json: [] }));
  await page.route("**/v1/session", route => {
    state.sessions++;
    if (state.unavailable) return route.abort();
    return route.fulfill(state.claimed
      ? { json: { paired: state.confirmed, deviceId: "synthetic-phone" } }
      : { status: 401, json: { error: "unauthorized" } });
  });
  await page.route("**/v1/pairings/claim", route => {
    state.claims++; state.claimed = true;
    return route.fulfill({ json: { id: "synthetic-phone" } });
  });
  await page.goto("/#pair=synthetic&nonce=synthetic");
  await page.waitForFunction(() => !!navigator.serviceWorker.controller);
  await expect(page.getByLabel("链接、分享口令或文字")).toBeEnabled();
  return state;
}

test("电脑确认后自动结束等待并保留草稿，无需刷新手机页面", async ({ page }) => {
  const state = await pairingPage(page);
  await page.getByLabel("链接、分享口令或文字").fill("绑定前写下的草稿");
  await page.getByLabel("设备名称").fill("Mate80 Pro Max");
  await page.getByRole("button", { name: "绑定这台手机", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("请在电脑上确认这台手机的绑定请求");
  await expect(page.getByRole("button", { name: "等待电脑确认", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "发送到归知", exact: true })).toBeDisabled();
  await page.screenshot({ path: "test-results/pairing-waiting.png", fullPage: true });
  state.confirmed = true;
  await expect(page.getByRole("button", { name: "发送到归知", exact: true })).toBeEnabled({ timeout: 5000 });
  await expect(page.getByText("请在电脑上确认这台手机的绑定请求")).toHaveCount(0);
  await expect(page.getByLabel("设备名称")).toHaveCount(0);
  await expect(page.getByLabel("链接、分享口令或文字")).toHaveValue("绑定前写下的草稿");
  await expect(page).toHaveURL("http://127.0.0.1:4178/");
  expect(state.claims).toBe(1);
  const sessions = state.sessions;
  await page.clock.install(); await page.clock.runFor(6000);
  expect(state.sessions).toBe(sessions);
  await page.screenshot({ path: "test-results/pairing-confirmed.png", fullPage: true });
});

test("重新打开待确认页面后自动恢复配对检查", async ({ page }) => {
  const state = await pairingPage(page, true);
  await page.reload();
  await expect(page.getByRole("status")).toContainText("请在电脑上确认这台手机的绑定请求");
  state.confirmed = true;
  await expect(page.getByLabel("设备名称")).toHaveCount(0, { timeout: 5000 });
  await expect(page.getByRole("status")).toHaveText("绑定成功，现在可以发送到归知了。");
});

test("旧版待确认页面没有本地标记时仍自动发现绑定完成", async ({ page }) => {
  const state = await pairingPage(page);
  state.claimed = true; state.confirmed = true;
  await expect(page.getByLabel("设备名称")).toHaveCount(0, { timeout: 5000 });
});

test("旧设备已撤销时，新二维码仍可申请绑定", async ({ page }) => {
  const state = await pairingPage(page);
  state.claimed = true;
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.getByLabel("设备名称").fill("重新绑定的手机");
  await page.getByRole("button", { name: "绑定这台手机", exact: true }).click();
  expect(state.claims).toBe(1);
  state.confirmed = true;
  await expect(page.getByLabel("设备名称")).toHaveCount(0, { timeout: 5000 });
});

for (const event of ["focus", "visibilitychange", "online"]) {
  test(`手机 ${event} 时立即检查电脑确认结果`, async ({ page }) => {
    const state = await pairingPage(page, true);
    await expect(page.getByRole("status")).toBeVisible();
    await page.clock.install(); await page.clock.pauseAt(new Date());
    await page.getByLabel("链接、分享口令或文字").fill("保留草稿");
    state.confirmed = true;
    await page.evaluate(event => (event === "visibilitychange" ? document : window).dispatchEvent(new Event(event)), event);
    await expect(page.getByRole("button", { name: "发送到归知", exact: true })).toBeEnabled({ timeout: 1000 });
    await expect(page.getByRole("status")).toHaveText("绑定成功，现在可以发送到归知了。");
  });
}

test("临时断网后继续检查，成功恢复后清除刷新错误", async ({ page }) => {
  const state = await pairingPage(page, true);
  await expect(page.getByRole("status")).toBeVisible();
  state.unavailable = true;
  await expect(page.getByRole("alert")).toBeVisible({ timeout: 5000 });
  state.unavailable = false; state.confirmed = true;
  await expect(page.getByLabel("设备名称")).toHaveCount(0, { timeout: 5000 });
  await expect(page.getByRole("alert")).toHaveCount(0);
  await expect(page.getByRole("status")).toHaveText("绑定成功，现在可以发送到归知了。");
});


test("绑定请求慢响应期间即时反馈，失败可重试且设备名称保留", async ({ page }) => {
  const state = await pairingPage(page);
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route("**/v1/pairings/claim", async route => {
    await held;
    await route.fulfill({ status: 503, json: { error: "network" } });
  });
  await page.getByLabel("设备名称").fill("测试手机");
  await page.getByRole("button", { name: "绑定这台手机", exact: true }).click();
  await expect(page.getByRole("button", { name: "正在提交绑定请求…", exact: true })).toBeDisabled();
  await expect(page.getByRole("status")).toContainText("正在提交绑定请求…");
  await expect(page.getByLabel("设备名称")).toBeDisabled();
  await page.screenshot({ path: "test-results/pairing-submitting.png", fullPage: true });
  release();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByRole("button", { name: "绑定这台手机", exact: true })).toBeEnabled();
  await expect(page.getByLabel("设备名称")).toHaveValue("测试手机");
  await page.route("**/v1/pairings/claim", route => {
    state.claimed = true;
    return route.fulfill({ json: { id: "synthetic-phone" } });
  });
  await page.getByRole("button", { name: "绑定这台手机", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("本页会自动更新，无需刷新。");
  state.confirmed = true;
  await expect(page.getByRole("status")).toHaveText("绑定成功，现在可以发送到归知了。");
});
