import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
export default async ({ win, app, shot, outDir }) => {
  const errors = []; win.on('pageerror', error => errors.push(error.message));
  const workerResult = await app.evaluate(async ({ app }) => {
    try {
    const { Worker } = process.getBuiltinModule('node:worker_threads'), path = process.getBuiltinModule('node:path');
    const file = path.join(app.getAppPath(), '../semantic-worker/semantic-worker.js');
    const worker = new Worker(file);
    try {
      const result = await new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('工作线程启动超时')), 30000);
        worker.once('message', message => { clearTimeout(timer); resolve(message); }); worker.once('error', error => { clearTimeout(timer); reject(error); });
        worker.postMessage({ id: 1, action: 'init', input: { model: 'fixture', generation: 'fixture', rootDir: path.join(app.getPath('userData'), 'isolated-index'), cache: { model: 'fixture', dims: 2, vectors: new Float32Array([1,0]), itemIds: ['fixture'], chunkIndexes: [0] } } });
      }); return result;
    } finally { await worker.terminate(); }
    } catch (error) { return { fatal: String(error), stack: error.stack, appPath: app.getAppPath() }; }
  });
  await fs.writeFile(path.join(outDir, 'worker-runtime.json'), JSON.stringify(workerResult, null, 2));
  assert.ok(workerResult.ready || workerResult.incompatible, JSON.stringify(workerResult));
  await app.evaluate(({ ipcMain }) => {
    ipcMain.removeHandler('ai:httpRequest');
    ipcMain.handle('ai:httpRequest', () => ({ ok: true, status: 200, statusText: 'OK', headers: {}, body: JSON.stringify({ choices: [{ message: { content: JSON.stringify({ pages: [{ title: '全文验收知识', kind: 'topic', summary: '合成来源', body: '知识来自本次合成分块。', aliases: [] }] }) }, finish_reason: 'stop' }] }) }));
  });
  const data = await win.evaluate(async () => {
    const settings = { language: 'zh', themeMode: 'light', isDarkMode: false, autoSave: false, wikiCompileEnabled: false, aiProvider: 'openai', aiApiProtocol: 'openai', aiApiKey: 'isolated-fixture-key', aiApiUrl: 'https://example.com/v1', aiModel: 'fixture-model' };
    localStorage.setItem('guizhi-settings', JSON.stringify({ state: settings, version: 0 }));
    await window.api.settings.set(settings);
    localStorage.setItem('ui-storage', JSON.stringify({ state: { libraryViewMode: 'list' }, version: 0 }));
    localStorage.setItem('guizhi-setup-dismissed', '1'); localStorage.setItem('guizhi-migration-dismissed', '1');
    const items = [];
    for (let n = 0; n < 60; n++) items.push(await window.api.knowledge.create({ title: `连续整理 ${String(n).padStart(2,'0')}：足够长的标题用于核对两行显示与阅读空间`, content: `唯一开头 ${n}\n\n唯一中段 ${n}\n\n唯一尾部 ${n}`, reviewStatus: n === 0 ? 'needs_review' : 'clear', reviewReasons: n === 0 ? ['文字稿缺失'] : [] }));
    for (let n = 0; n < 1000; n++) await window.api.askSession.save({ id: `history-${n}`, title: `历史会话 ${n}`, messagesJson: JSON.stringify([{ id: `message-${n}`, question: `问题 ${n}`, answer: n === 0 ? '最早记录的唯一答案' : `回答 ${n}`, status: 'done', sources: [] }]) });
    return items;
  });
  await win.reload(); await win.setViewportSize({ width: 1440, height: 1000 });
  await win.getByTestId('item-table').waitFor();
  await win.getByRole('button', { name: '全部符合筛选', exact: true }).click();
  await win.getByText('已选 60 项', { exact: true }).waitFor();
  await win.getByTestId('item-pagination').getByRole('button', { name: '下一页', exact: true }).click();
  await win.getByText('已选 60 项', { exact: true }).waitFor();
  await shot('05-cross-page-selection-light');
  await win.getByRole('button', { name: '取消选择', exact: true }).click();
  await win.getByTestId('item-table').locator('tbody tr').first().getByRole('button').first().click();
  await win.getByRole('button', { name: '保存并下一条', exact: true }).waitFor();
  const title = win.getByTestId('item-title-input'); await title.fill('连续整理中保留的标题');
  await win.getByRole('button', { name: '保存并下一条', exact: true }).click();
  await win.waitForFunction(() => document.querySelector('[data-testid="item-title-input"]')?.value !== '连续整理中保留的标题');
  await shot('06-frozen-review-navigation');
  await win.keyboard.press('Escape');
  await win.evaluate(() => window.dispatchEvent(new Event('wiki-compile-preview')));
  await win.getByText('全文编译与历史升级', { exact: true }).waitFor();
  await win.getByText(/可执行 59/).waitFor();
  await shot('07-wiki-preview-quality-and-budget');
  await win.getByRole('button', { name: '开始所选资料', exact: true }).click();
  await win.getByText(/全文完成 · 已完成/).waitFor({ timeout: 60000 });
  await shot('08-wiki-completed-checkpoints');
  const query = await win.evaluate(() => window.api.askSession.query({ search: '最早记录的唯一答案', limit: 50 }));
  assert.equal(query.entries.length, 1); assert.equal(query.entries[0].id, 'history-0');
  await win.evaluate(() => document.documentElement.classList.add('dark'));
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(1.25));
  await shot('09-wiki-dark-125');
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(1.5));
  await shot('10-wiki-dark-150');
  assert.equal(data.length, 60); assert.deepEqual(errors, []);
};
