/** 隔离数据验收，不连接用户实例或真实模型。 */
import assert from 'node:assert/strict';

export default async ({ win, app, shot }) => {
  const errors = [];
  win.on('pageerror', e => errors.push(e.message));
  const items = await win.evaluate(async () => {
    const settings = JSON.parse(localStorage.getItem('guizhi-settings') || '{"state":{}}');
    Object.assign(settings.state, { autoSave: false, language: 'zh', themeMode: 'light', isDarkMode: false });
    localStorage.setItem('guizhi-settings', JSON.stringify(settings));
    localStorage.setItem('guizhi-setup-dismissed', '1'); localStorage.setItem('guizhi-migration-dismissed', '1');
    return Promise.all(['连续整理 A', '连续整理 B'].map(title => window.api.knowledge.create({ title, content: '用于检验保存与切换的独立资料。' })));
  });
  await win.reload(); await win.setViewportSize({ width: 1440, height: 1000 });
  const list = win.getByTestId('item-list');
  await list.getByText(items[0].title, { exact: true }).click();
  const title = win.getByTestId('item-title-input');
  await title.fill('本地未保存标题');
  await win.evaluate(id => window.api.knowledge.update(id, { title: '另一次操作写入的标题' }), items[0].id);
  await title.press('Control+s');
  await win.getByText('内容已在其他操作中修改，草稿尚未覆盖已保存版本。').waitFor();
  await win.getByText('比较两份内容', { exact: true }).click();
  assert.equal(await title.inputValue(), '本地未保存标题');
  await shot('01-draft-conflict-light');
  await list.getByText(items[1].title, { exact: true }).click();
  assert.equal(await title.inputValue(), '本地未保存标题');
  await win.getByRole('button', { name: '保留我的修改并保存' }).click();
  await win.waitForFunction(async id => (await window.api.knowledge.get(id)).title === '本地未保存标题', items[0].id);
  await list.getByText(items[1].title, { exact: true }).click();
  await win.waitForFunction(() => document.querySelector('[data-testid="item-title-input"]')?.value === '连续整理 B');
  await app.evaluate(({ ipcMain }) => {
    globalThis.__heavyOriginalGet = ipcMain._invokeHandlers.get('knowledge:get');
    ipcMain.removeHandler('knowledge:get');
    ipcMain.handle('knowledge:get', () => { throw new Error('验收注入：详情读取失败'); });
  });
  await list.getByText('本地未保存标题', { exact: true }).click();
  await win.getByText(/验收注入：详情读取失败/).waitFor();
  assert.equal(await title.count(), 0);
  await shot('02-detail-load-error');
  await app.evaluate(({ ipcMain }) => { ipcMain.removeHandler('knowledge:get'); ipcMain.handle('knowledge:get', globalThis.__heavyOriginalGet); });
  await win.getByRole('button', { name: /重试/ }).last().click();
  await title.waitFor(); assert.equal(await title.inputValue(), '本地未保存标题');
  await win.evaluate(() => { document.documentElement.classList.add('dark'); });
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(1.25));
  await shot('03-recovered-dark-125');
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(1.5));
  await shot('04-recovered-dark-150');
  assert.deepEqual(errors, []);
};
