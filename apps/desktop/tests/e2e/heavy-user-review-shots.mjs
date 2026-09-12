import assert from 'node:assert/strict';
import editing from './heavy-user-editing-shots.mjs';

/** 发布前复核：合成资料，经真实 IPC 与用户删除确认流程验证，不调用模型。 */
export default async context => {
  await editing(context);
  const { win, app, shot } = context;
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(1));
  const itemId = await win.evaluate(async () => {
    const item = await window.api.knowledge.create({ title: '复核用待删除来源', content: '原始资料正文' });
    const source = { ordinal: 1, kind: 'item', refId: item.id, title: '最终引用来源', evidence: { version: 1, kind: 'item', sourceId: item.id, title: '回答时的来源', text: '仅应出现在删除前的证据片段', capturedAt: Date.now(), fingerprint: 'historical', reviewStatus: 'clear', reviewReasons: [] } };
    await window.api.askSession.save({ id: 'review-evidence', title: '发布前历史证据验收', messagesJson: JSON.stringify([{ id: 'm', question: '历史证据能否保持一致？', answer: '这段回答文本应始终保留。[1]', status: 'done', sources: [source], evidenceSources: [{ ...source, title: '补充已读来源' }], steps: [] }]) });
    await window.api.knowledge.moveToTrash([item.id]);
    localStorage.setItem('guizhi-ask-active-session', 'review-evidence');
    localStorage.setItem('ui-storage', JSON.stringify({ state: { appModule: 'ask', libraryViewMode: 'card' }, version: 0 }));
    return item.id;
  });
  await win.reload();
  await win.getByRole('button', { name: 'AI 问答', exact: true }).click();
  await win.getByText('历史证据能否保持一致？', { exact: true }).waitFor();
  await win.getByText('本次读取的全部证据（1）', { exact: true }).click();
  await win.getByRole('button', { name: '补充已读来源', exact: true }).click();
  await win.getByText('仅应出现在删除前的证据片段', { exact: true }).waitFor();
  await shot('05-history-evidence-restored');
  await win.getByRole('button', { name: '知识库', exact: true }).click();
  await win.getByText('回收站', { exact: true }).click();
  await win.getByTestId('item-list').getByText('复核用待删除来源', { exact: true }).click({ button: 'right' });
  await win.getByRole('menuitem', { name: '彻底删除', exact: true }).click();
  await win.getByText('同时清除历史问答中的相关证据', { exact: true }).click();
  await win.getByRole('button', { name: '确认', exact: true }).click();
  await win.waitForFunction(async id => (await window.api.knowledge.get(id)) === null, itemId);
  await win.getByRole('button', { name: 'AI 问答', exact: true }).click();
  await win.getByRole('button', { name: /来源已清除/ }).first().click();
  await win.getByText('相关证据正文与链接已清除。回答文本保留原样。', { exact: true }).waitFor();
  assert.equal(await win.getByText('仅应出现在删除前的证据片段', { exact: true }).count(), 0);
  assert.equal(await win.getByRole('button', { name: '查看当前版本', exact: true }).count(), 0);
  await shot('06-evidence-cleared-light');
  const saved = await win.evaluate(() => window.api.askSession.get('review-evidence'));
  assert.ok(!saved.messagesJson.includes('仅应出现在删除前的证据片段'));
  assert.ok(saved.messagesJson.includes('这段回答文本应始终保留。'));
  await win.evaluate(() => document.documentElement.classList.add('dark'));
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(1.25));
  await shot('07-evidence-cleared-dark-125');
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(1.5));
  await shot('08-evidence-cleared-dark-150');
  // 先通过正式会话切换保存，避免 Playwright 强制退出与 beforeunload 的保存关闭竞速。
  await win.getByRole('button', { name: '新对话', exact: true }).click();
  await win.getByText('这段回答文本应始终保留。', { exact: false }).waitFor({ state: 'detached' });
};
