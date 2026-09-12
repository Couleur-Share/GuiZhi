/** 只验证离线文章夹具，不调用任何 AI 服务。 */
import fs from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

export default async ({ win, app, shot, outDir }) => {
  const fixtures = JSON.parse(await fs.readFile(path.resolve("../../artifacts/themed-reading/content-fixtures.json"), "utf8"));
  const items = await win.evaluate(async (fixtures) => {
    const saved = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
    Object.assign(saved.state, { language: "zh", themeMode: "light", isDarkMode: false, editorMarkdownPreview: true });
    localStorage.setItem("guizhi-settings", JSON.stringify(saved));
    localStorage.setItem("guizhi-setup-dismissed", "1"); localStorage.setItem("guizhi-migration-dismissed", "1");
    const created = [];
    for (const entry of fixtures) created.push(await window.api.knowledge.create({ title: entry.title, content: entry.content, itemType: entry.itemType }));
    return created;
  }, fixtures);
  await app.evaluate(({ ipcMain }, { fixtures, items }) => {
    ipcMain.removeHandler("themedReading:get");
    ipcMain.handle("themedReading:get", (_event, input) => {
      const index = items.findIndex((item) => item.id === input.itemId), fixture = fixtures[index];
      return { success: true, models: { text: "offline-fixture", image: null }, page: { ...fixture.page, itemId: input.itemId }, document: fixture.document.replace('data-instance="fixture-instance"', `data-instance="${input.instanceId}"`) };
    });
  }, { fixtures, items });
  await win.reload();
  await win.setViewportSize({ width: 1120, height: 850 });
  const evidence = [];
  for (const [index, fixture] of fixtures.entries()) {
    await win.getByTestId("item-list").getByText(items[index].title, { exact: true }).click();
    await win.getByRole("button", { name: "AI 主题页", exact: true }).click();
    const frame = win.frameLocator('iframe[aria-label="AI 主题阅读页"]');
    await frame.locator("[data-source-block]").first().waitFor();
    await win.waitForTimeout(200);
    const frameDimensions = await frame.locator("body").evaluate((body) => ({ clientHeight: document.documentElement.clientHeight, scrollHeight: document.documentElement.scrollHeight, rootHeight: document.documentElement.getBoundingClientRect().height, bodyHeight: body.getBoundingClientRect().height }));
    const hostFrameHeight = await win.locator('iframe[aria-label="AI 主题阅读页"]').evaluate((element) => element.getBoundingClientRect().height);
    const stats = await frame.locator("body").evaluate((body) => ({ text: Array.from(body.querySelectorAll("[data-source-block]"), (element) => element.textContent).join("\n"), blocks: body.querySelectorAll("[data-source-block]").length, width: document.documentElement.scrollWidth, viewport: document.documentElement.clientWidth, tableCount: body.querySelectorAll("table").length, codeCount: body.querySelectorAll("pre").length, tableWidths: Array.from(body.querySelectorAll("table"), (table) => ({ width: table.clientWidth, scroll: table.scrollWidth, minCellWidth: Math.min(...Array.from(table.querySelectorAll("th,td"), (cell) => cell.getBoundingClientRect().width)), cellsReadable: Array.from(table.querySelectorAll("th,td"), (cell) => cell.getBoundingClientRect().width >= 8 * parseFloat(getComputedStyle(cell).fontSize) - 2).every(Boolean) })) }));
    assert.equal(stats.blocks, fixture.page.source.blocks.length);
    assert(stats.text.includes(fixture.tail));
    assert(stats.width <= stats.viewport + 2, `正文横向溢出: ${fixture.id} ${stats.width}/${stats.viewport}`);
    if (fixture.id === "forum") {
      assert(!stats.text.includes("FORUM_BODY_EXCLUDED")); assert(!stats.text.includes("FORUM_REPLIES_EXCLUDED"));
      const summaryTab = await win.getByRole("button", { name: "讨论总结", exact: true }).evaluate((element) => ({ whiteSpace: getComputedStyle(element).whiteSpace, shrink: getComputedStyle(element).flexShrink }));
      assert.equal(summaryTab.whiteSpace, "nowrap"); assert.equal(summaryTab.shrink, "0");
    }
    await shot(`theme-content-${fixture.id}-narrow`);
    if (fixture.id === "technical") {
      assert(stats.tableWidths[0].cellsReadable, "长表格单元格必须保留至少 8em 可读宽度");
      assert(stats.tableWidths[0].scroll > stats.tableWidths[0].width, "长表格应在自身容器横向滚动");
      for (const selector of ["pre", "table"]) {
        const top = await frame.locator(selector).first().evaluate((element) => element.getBoundingClientRect().top + scrollY);
        await win.getByTestId("themed-reading-frame").locator(":scope > div").first().evaluate((element, top) => { element.scrollTop = Math.max(0, top - 30); }, top);
        await win.waitForTimeout(180);
        await shot(`theme-content-technical-${selector}`);
        if (selector === "table") {
          const lastColumn = await frame.locator("table").first().evaluate((table) => {
            table.scrollLeft = table.scrollWidth;
            const last = table.querySelector("tr").lastElementChild;
            return { text: last.textContent, left: last.getBoundingClientRect().left, right: last.getBoundingClientRect().right, tableLeft: table.getBoundingClientRect().left, tableRight: table.getBoundingClientRect().right };
          });
          assert.equal(lastColumn.text, "重试方式");
          assert(lastColumn.left >= lastColumn.tableLeft - 2 && lastColumn.right <= lastColumn.tableRight + 2, "横向滚动后末列表头应可见");
          await shot("theme-content-technical-table-last-column");
        }
      }
    }
    if (fixture.id === "long") {
      await win.getByRole("button", { name: "在当前页查找 (Ctrl+F)", exact: true }).click();
      await win.getByPlaceholder("在当前页查找…").fill(fixture.tail);
      await win.waitForTimeout(250);
      const highlights = await frame.locator("body").evaluate(() => Array.from(CSS.highlights.get("guizhi-find-active") ?? [], (range) => range.toString()));
      assert(highlights.includes(fixture.tail), "尾部查找须产生精确的 CSS Highlight 范围");
      await shot("theme-content-long-tail-find");
    }
    evidence.push({ id: fixture.id, sourceCharacters: fixture.page.source.content.length, renderedCharacters: stats.text.length, blocks: stats.blocks, width: stats.width, viewport: stats.viewport, frameDimensions, hostFrameHeight, tableCount: stats.tableCount, codeCount: stats.codeCount, tableWidths: stats.tableWidths, tailPresent: true });
  }
  await fs.writeFile(path.join(outDir, "content-evidence.json"), JSON.stringify({ fixtureOnly: true, evidence }, null, 2));
};
