/** 保存的真实主题布局留白回归；只读本地夹具，截图运行在隔离用户目录。 */
import fs from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

export default async ({ win, app, shot, outDir, userDataDir }) => {
  const fixture = JSON.parse(await fs.readFile(path.resolve("../../artifacts/themed-reading/spacing-fixture.json"), "utf8"));
  const targetAssets = path.resolve(userDataDir, "data/assets/images");
  await fs.mkdir(targetAssets, { recursive: true });
  for (const asset of fixture.page.assets.filter((entry) => entry.status === "ready")) {
    assert.match(asset.fileName, /^[\w.-]+\.(png|jpe?g|gif|webp)$/);
    assert.ok(!asset.fileName.includes(".."));
    await fs.copyFile(path.join(fixture.assetsDirectory, asset.fileName), path.join(targetAssets, asset.fileName));
  }
  const item = await win.evaluate(async ({ title, content }) => {
    const saved = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
    Object.assign(saved.state, { language: "zh", themeMode: "dark", isDarkMode: true, editorMarkdownPreview: true });
    localStorage.setItem("guizhi-settings", JSON.stringify(saved));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
    return window.api.knowledge.create({ title, content, itemType: "note" });
  }, fixture.page.source);
  await app.evaluate(({ ipcMain }, { fixture, item }) => {
    ipcMain.removeHandler("themedReading:get");
    ipcMain.handle("themedReading:get", (_event, input) => ({ success: true, page: { ...fixture.page, itemId: item.id }, stale: false, previous: false, models: { text: fixture.page.textModel, image: fixture.page.imageModel }, document: fixture.document.replace('data-instance="spacing-fixture"', `data-instance="${input.instanceId}"`) }));
    for (const name of ["generate", "cancel", "resume", "regenerateAsset"]) {
      ipcMain.removeHandler(`themedReading:${name}`);
      ipcMain.handle(`themedReading:${name}`, () => ({ success: false, error: "留白验收禁止调用模型" }));
    }
  }, { fixture, item });
  await win.reload();
  await win.getByTestId("item-list").getByText(item.title, { exact: true }).click();
  await win.getByRole("button", { name: "AI 主题页", exact: true }).click();
  const iframe = win.locator('iframe[aria-label="AI 主题阅读页"]');
  await iframe.waitFor();
  assert.equal(await iframe.getAttribute("title"), null, "阅读容器不能触发覆盖正文的原生悬浮提示");
  assert.equal(await iframe.getAttribute("aria-label"), "AI 主题阅读页", "保留辅助阅读名称");
  const frame = await (await iframe.elementHandle()).contentFrame();
  await frame.waitForFunction((count) => document.querySelectorAll("[data-source-block]").length === count && [...document.images].every((image) => image.complete && image.naturalWidth > 0), fixture.page.source.blocks.length);
  const scroll = win.getByTestId("themed-reading-frame").locator("div.overflow-auto").first();
  const trigger = win.locator("[data-snapshot-toc-trigger]");
  assert.equal(await win.getByTestId("themed-reading-frame").locator("[data-snapshot-toc-trigger]").count(), 0, "目录入口不能悬浮遮挡正文");
  assert.equal(await trigger.innerText(), "目录", "常驻目录不混入阅读百分比");
  assert.equal(await iframe.getAttribute("scrolling"), "no");
  const report = [];
  for (const mode of ["dark", "light"]) for (const width of [2000, 1120]) {
    await win.setViewportSize({ width, height: 1000 });
    await win.emulateMedia({ colorScheme: mode });
    await win.evaluate((mode) => document.documentElement.classList.toggle("dark", mode === "dark"), mode);
    await frame.waitForFunction((mode) => document.documentElement.dataset.theme === mode, mode);
    await frame.locator("[data-source-block]").first().hover();
    await win.waitForTimeout(1000);
    await frame.waitForFunction(() => document.documentElement.scrollHeight <= innerHeight + 1);
    await win.waitForTimeout(200);
    const geometry = await frame.evaluate((blocks) => {
      const slots = [...document.querySelectorAll("[data-source-block]")];
      const root = [...document.body.children].find((element) => element.contains(slots[0]));
      const bounds = root.getBoundingClientRect();
      const normalize = (text) => text.replace(/\s+/g, " ").trim();
      return { width: innerWidth, scrollWidth: document.documentElement.scrollWidth,
        leftInset: slots[0].getBoundingClientRect().left - bounds.left,
        rightInset: bounds.right - slots[0].getBoundingClientRect().right,
        rootPadding: getComputedStyle(root).paddingInlineStart,
        rows: slots.map((element, index) => ({ id: element.dataset.sourceBlock,
          matches: normalize(element.textContent) === normalize(blocks[index].text),
          left: element.getBoundingClientRect().left, right: element.getBoundingClientRect().right,
          visible: element.getBoundingClientRect().width > 0 && element.getBoundingClientRect().height > 0 })),
        images: [...document.images].map((image) => image.complete && image.naturalWidth > 0) };
    }, fixture.page.source.blocks);
    report.push({ mode, windowWidth: width, ...geometry });
    await fs.writeFile(path.join(outDir, "spacing-report.json"), JSON.stringify(report, null, 2));
    assert.ok(geometry.leftInset >= 20 && geometry.rightInset >= 20, "主题底色内部两侧至少保留 20px，宽屏自适应增加");
    assert.ok(width < 1200 || geometry.leftInset >= 40, "宽阅读页应有充足内边距");
    assert.ok(geometry.scrollWidth <= geometry.width + 2, "窄屏不能整体横向溢出");
    assert.ok(geometry.rows.every((row) => row.matches && row.visible && row.left >= 0 && row.right <= geometry.width + 2), "所有原文完整可见且未溢出");
    assert.ok(geometry.images.every(Boolean), "原有图片必须正常加载");
    await shot(`spacing-${mode}-${width > 1200 ? "wide" : "narrow"}`);
  }
  // 鼠标停在 iframe 内滚动，必须移动宿主唯一阅读滚动区。
  await scroll.evaluate((element) => { element.scrollTop = 0; });
  await frame.locator("[data-source-block]").first().hover();
  await win.mouse.wheel(0, 500);
  await win.waitForTimeout(350);
  assert.ok(await scroll.evaluate((element) => element.scrollTop > 100));
  assert.equal(await frame.evaluate(() => scrollY), 0, "子页面不能积累独立滚动位置");
  await trigger.click();
  const toc = win.getByRole("navigation", { name: "文章章节目录" });
  await toc.getByRole("button", { name: "返回顶部", exact: true }).click();
  await win.waitForTimeout(180);
  await trigger.click();
  assert.ok((await toc.innerText()).includes("0%"), "开头进度必须为 0%");
  await toc.getByRole("button").nth(4).click();
  assert.ok(await scroll.evaluate((element) => element.scrollTop > 500), "章节跳转必须移动宿主滚动区");
  await scroll.evaluate((element) => { element.scrollTop = element.scrollHeight; });
  await win.waitForTimeout(180);
  await trigger.click();
  assert.ok((await toc.innerText()).includes("100%"), "到底后才显示 100%");
  await shot("reading-bottom-catalog");
  await toc.getByRole("button", { name: "返回顶部", exact: true }).click();
};
