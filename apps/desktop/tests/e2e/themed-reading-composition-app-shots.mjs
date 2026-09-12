/** 在真实阅读组件内验收，只往 pnpm shot 的隔离用户目录写入测试条目。 */
import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

export default async ({ win, app, shot, outDir }) => {
  const { fixtures } = JSON.parse(await fs.readFile(path.resolve(process.env.GUIZHI_COMPOSITION_FIXTURES || '../../artifacts/themed-reading/composition/fixtures.json'), 'utf8'));
  for (const fixture of fixtures) fixture.document = await fs.readFile(fixture.files.embedded, 'utf8');
  const items = await win.evaluate(async fixtures => {
    const settings = JSON.parse(localStorage.getItem('guizhi-settings') || '{"state":{}}');
    Object.assign(settings.state, { language: 'zh', themeMode: 'light', isDarkMode: false, editorMarkdownPreview: true });
    localStorage.setItem('guizhi-settings', JSON.stringify(settings));
    localStorage.setItem('guizhi-setup-dismissed', '1'); localStorage.setItem('guizhi-migration-dismissed', '1');
    const items = [];
    for (const fixture of fixtures) items.push(await window.api.knowledge.create({ title: fixture.title, content: fixture.page.source.content, itemType: 'note' }));
    return items;
  }, fixtures);
  await app.evaluate(({ ipcMain }, { fixtures, items }) => {
    ipcMain.removeHandler('themedReading:get');
    ipcMain.handle('themedReading:get', (_event, input) => {
      const fixture = fixtures[items.findIndex(item => item.id === input.itemId)];
      return { success: true, models: { text: 'composition-fixture', image: null }, page: { ...fixture.page, itemId: input.itemId, sourceKind: input.sourceKind }, document: fixture.document.replace('data-instance="composition-fixture"', `data-instance="${input.instanceId}"`) };
    });
  }, { fixtures, items });
  await win.reload();
  const report = [];
  for (const [index, fixture] of fixtures.entries()) {
    await win.setViewportSize({ width: 1680, height: 1100 });
    await win.getByTestId('item-list').getByText(items[index].title, { exact: true }).click();
    await win.getByRole('button', { name: 'AI 主题页', exact: true }).click();
    const frame = win.frameLocator('iframe[aria-label="AI 主题阅读页"]');
    await frame.locator('.gz-title').waitFor(); await win.waitForTimeout(250);
    await shot(`${fixture.id}-app-top`);
    await frame.locator('.gz-nav a').first().click();
    await win.waitForTimeout(200);
    const reader = win.getByTestId('themed-reading-frame').locator(':scope > div').first();
    assert(await reader.evaluate(node => node.scrollTop) > 200);
    await frame.locator('.gz-evidence a').first().click(); await win.waitForTimeout(200);
    assert.equal(await frame.locator('.gz-original').evaluate(node => node.open), true);
    assert(await reader.evaluate(node => node.scrollTop) > 500);
    await shot(`${fixture.id}-app-source`);
    await win.getByRole('button', { name: '在当前页查找 (Ctrl+F)', exact: true }).click();
    const query = fixture.id === 'fish-oil' ? '看法不一' : '不符合精酿标准';
    await win.getByPlaceholder('在当前页查找…').fill(query); await win.waitForTimeout(250);
    const hits = await frame.locator('body').evaluate(() => [...(CSS.highlights.get('guizhi-find-active') ?? [])].map(range => range.toString()));
    assert(hits.includes(query), '完整原文中的词仍可通过应用查找');
    await shot(`${fixture.id}-app-find`);
    await win.getByPlaceholder('在当前页查找…').fill('');
    await win.keyboard.press('Escape');
    await frame.locator('.gz-original>summary').click();
    await reader.evaluate(node => node.scrollTop = 0);
    await win.setViewportSize({ width: 1120, height: 1000 }); await win.waitForTimeout(220);
    const state = await frame.locator('body').evaluate(() => ({ width: innerWidth, scrollWidth: document.documentElement.scrollWidth, scrollY, sourceBlocks: document.querySelectorAll('[data-source-block]').length, theme: document.documentElement.dataset.theme }));
    assert(state.scrollWidth <= state.width + 2); assert.equal(state.scrollY, 0);
    await shot(`${fixture.id}-app-narrow`);
    report.push({ id: fixture.id, query, ...state });
  }
  await fs.writeFile(path.join(outDir, 'app-evidence.json'), JSON.stringify({ fixtureOnly: true, success: true, cases: report }, null, 2));
};
