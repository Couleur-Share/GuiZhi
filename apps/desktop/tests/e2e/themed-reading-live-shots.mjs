/** 真实 AI 设计 + 原图，使用正常主题 iframe/桥接/查找/目录/放大；模型接口被夹具明确禁止。 */
import fs from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

export default async ({ win, app, shot, outDir, userDataDir }) => {
  const fixture = JSON.parse(await fs.readFile(path.resolve("../../artifacts/themed-reading/live-layout-fixture.json"), "utf8"));
  const targetAssets = path.resolve(userDataDir, "data/assets/images");
  assert.ok(path.relative(path.resolve(userDataDir), targetAssets).startsWith(`data${path.sep}`));
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
    for (const name of ["get", "generate", "cancel", "resume", "listTasks", "regenerateAsset"]) ipcMain.removeHandler(`themedReading:${name}`);
    const task = { id: "live-layout-task", itemId: item.id, sourceKind: "body", versionId: fixture.page.id, title: item.title, state: "completed", stage: "done", completed: 3, total: 3, plannedImages: 1, createdAt: Date.now(), updatedAt: Date.now() };
    ipcMain.handle("themedReading:get", (_event, input) => ({ success: true, page: { ...fixture.page, itemId: item.id }, task, stale: false, previous: false, models: { text: fixture.page.textModel, image: fixture.page.imageModel }, document: fixture.document.replace('data-instance="live-layout-fixture"', `data-instance="${input.instanceId}"`) }));
    ipcMain.handle("themedReading:listTasks", () => ({ success: true, tasks: [task] }));
    for (const name of ["generate", "cancel", "resume", "regenerateAsset"]) ipcMain.handle(`themedReading:${name}`, () => ({ success: false, error: "离屏验收禁止调用模型或修改真实任务" }));
  }, { fixture, item });
  await win.reload();
  await win.getByTestId("item-list").getByText(item.title, { exact: true }).click();
  await win.getByRole("button", { name: "AI 主题页", exact: true }).click();
  const iframe = win.locator('iframe[aria-label="AI 主题阅读页"]');
  await iframe.waitFor();
  const frame = await (await iframe.elementHandle()).contentFrame();
  await frame.waitForFunction(() => document.querySelectorAll("[data-source-block]").length === 12 && [...document.images].every((image) => image.complete && image.naturalWidth > 0));
  assert.equal(await iframe.getAttribute("sandbox"), "allow-scripts");
  const report = { sourcePage: fixture.page.id, assets: fixture.page.assets.map((asset) => ({ id: asset.id, role: asset.role, status: asset.status })), scenarios: [], interactions: {} };
  const scroll = win.getByTestId("themed-reading-frame").locator("div.overflow-auto").first();

  for (const scenario of [{ mode: "dark", width: 1600, height: 1000 }, { mode: "light", width: 1600, height: 1000 }, { mode: "dark", width: 1120, height: 850 }, { mode: "light", width: 1120, height: 850 }]) {
    await win.setViewportSize({ width: scenario.width, height: scenario.height });
    await win.emulateMedia({ colorScheme: scenario.mode });
    await win.evaluate((mode) => document.documentElement.classList.toggle("dark", mode === "dark"), scenario.mode);
    await frame.waitForFunction((mode) => document.documentElement.dataset.theme === mode, scenario.mode);
    await scroll.evaluate((element) => { element.scrollTop = 0; });
    await win.waitForTimeout(180);
    const geometry = await frame.evaluate((blocks) => {
      const normalize = (value) => value.replace(/\s+/g, " ").trim();
      const width = document.documentElement.clientWidth;
      const rows = [...document.querySelectorAll("[data-source-block]")].map((element, index) => {
        const rect = element.getBoundingClientRect(), style = getComputedStyle(element);
        return { id: element.dataset.sourceBlock, left: rect.left, right: rect.right, width: rect.width, height: rect.height, textMatches: normalize(element.textContent) === normalize(blocks[index].text), visible: rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && Number(style.opacity) > 0 };
      });
      return { width, scrollWidth: document.documentElement.scrollWidth, rows, imageCount: document.images.length, background: getComputedStyle(document.querySelector("article")).backgroundColor, textColor: getComputedStyle(document.querySelector("[data-source-block]")).color };
    }, fixture.page.source.blocks);
    report.scenarios.push({ ...scenario, viewportWidth: scenario.width, ...geometry });
    await fs.writeFile(path.join(outDir, "live-layout-report.json"), JSON.stringify(report, null, 2), "utf8");
    assert.equal(geometry.rows.length, fixture.page.source.blocks.length);
    assert.ok(geometry.rows.every((row) => row.visible && row.textMatches), "正文块必须全部可见且与原文完全一致");
    assert.ok(geometry.rows.every((row) => row.left >= -2 && row.right <= geometry.width + 2), "正文不能横向移出iframe");
    assert.ok(geometry.scrollWidth <= geometry.width + 2, "主题文档不能发生整体横向溢出");
    await shot(`live-layout-${scenario.mode}-${scenario.width > 1200 ? "wide" : "narrow"}`);
  }

  await win.setViewportSize({ width: 1600, height: 1000 });
  await frame.locator('[data-source-block="b0"]').click();
  await win.keyboard.press("Control+f");
  const find = win.getByPlaceholder(/查找/).last();
  await find.waitFor();
  await find.fill("都不符合精酿标准");
  await win.waitForTimeout(200);
  const matches = await frame.evaluate(() => CSS.highlights.get("guizhi-find")?.size ?? 0);
  assert.ok(matches >= 1, "Ctrl+F必须找到正文尾部");
  assert.ok(await scroll.evaluate((element) => element.scrollTop > 200), "查找应滚动到尾部");
  report.interactions.findMatches = matches;
  await shot("live-layout-find-tail");
  await find.press("Escape");

  const tocTrigger = win.locator("[data-snapshot-toc-trigger]").first();
  await tocTrigger.click();
  const toc = win.getByRole("navigation", { name: "文章章节目录" });
  await toc.waitFor();
  report.interactions.tocHeadings = (await toc.getByRole("button").count()) - 1;
  assert.equal(report.interactions.tocHeadings, 5, "目录应识别视频总结及四个明确编号章节");
  for (const heading of ["一、生啤与熟啤：杀不杀菌", "二、鲜啤：状态而非工艺", "三、精酿：生产规模与理念", "四、三个选购判断问题"]) {
    assert.ok(await toc.getByRole("button", { name: heading, exact: true }).isVisible(), `目录遗漏章节：${heading}`);
  }
  await shot("live-layout-toc");
  await toc.getByRole("button", { name: "返回顶部", exact: true }).click();
  await win.waitForTimeout(160);
  await frame.locator('nav a[href="#chapter-0-guide"]').click();
  assert.ok(await scroll.evaluate((element) => element.scrollTop > 200), "AI页面目录应能定位到选购章节");
  report.interactions.inlineChapterNavigation = true;
  await scroll.evaluate((element) => { element.scrollTop = 0; });
  const image = frame.locator("img").first();
  await image.scrollIntoViewIfNeeded();
  await image.click();
  await win.locator(".yarl__root").waitFor();
  await win.waitForFunction(() => {
    const current = document.querySelector(".yarl__slide_current img");
    return current?.complete && current.naturalWidth > 0;
  });
  await win.waitForTimeout(120);
  report.interactions.lightbox = true;
  await shot("live-layout-image-zoom");
  await win.keyboard.press("Escape");
  await fs.writeFile(path.join(outDir, "live-layout-report.json"), JSON.stringify(report, null, 2), "utf8");
};
