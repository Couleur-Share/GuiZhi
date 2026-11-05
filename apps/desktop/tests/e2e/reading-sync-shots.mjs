import assert from 'node:assert/strict';

// 只替换隔离截图实例中的服务边界，验证真实 preload 与 React 操作，不上传用户资料。
export default async ({ win, app, shot }) => {
  await app.evaluate(({ ipcMain, BrowserWindow }) => {
    BrowserWindow.getAllWindows()[0].setSize(1400, 1050);
    globalThis.readingPreview = { calls: [], status: { available: true, configured: false, origin: null,
      collectionIds: [], sequence: 0, pending: false, busy: false, error: null } };
    ipcMain.removeHandler('reading-sync:command');
    ipcMain.handle('reading-sync:command', (_event, action, args = {}) => {
      const fixture = globalThis.readingPreview;
      fixture.calls.push(action);
      if (action === 'activate') Object.assign(fixture.status, { configured: true, origin: args.origin });
      if (action === 'configure') fixture.status.collectionIds = args.collectionIds;
      if (action === 'sync') fixture.status.sequence++;
      if (action === 'pair') return { success: true, data: { url: 'https://example.invalid/#synthetic-pairing', expiresAt: Date.now() + 300000 } };
      if (action === 'pair-status') return { success: true, data: { pairId: 'synthetic_pair', deviceId: 'synthetic_phone', approved: false } };
      return { success: true, data: fixture.status };
    });
  });
  await win.evaluate(() => {
    localStorage.setItem('guizhi-setup-dismissed', '1');
    localStorage.setItem('guizhi-migration-dismissed', '1');
    const settings = JSON.parse(localStorage.getItem('guizhi-settings') || '{"state":{}}');
    Object.assign(settings.state, { language: 'zh', themeMode: 'light', isDarkMode: false });
    localStorage.setItem('guizhi-settings', JSON.stringify(settings));
  });
  await win.evaluate(() => window.api.collection.create({ name: '阅读同步合成资料' }));
  await win.reload();
  await win.getByTestId('rail-settings').click();
  await win.getByTestId('settings-nav-mobile-capture').click();
  const panel = win.getByTestId('reading-sync-settings');
  await panel.waitFor();
  await shot('reading-sync-activation');
  await panel.getByLabel('服务地址', { exact: true }).fill('https://reading.example.com');
  await panel.getByLabel('开通凭据', { exact: true }).fill('synthetic-invite');
  await panel.getByRole('button', { name: '开启阅读同步', exact: true }).click();
  await panel.getByText('阅读同步合成资料', { exact: true }).click();
  assert.equal(await panel.getByRole('button', { name: '立即同步', exact: true }).isDisabled(), true);
  await panel.getByRole('button', { name: '保存选择', exact: true }).click();
  await panel.getByRole('button', { name: '立即同步', exact: true }).click();
  await panel.getByText('https://reading.example.com · 已同步版本 1', { exact: true }).waitFor();
  await panel.getByRole('button', { name: '生成阅读配对码', exact: true }).click();
  await panel.getByAltText('手机阅读配对二维码，五分钟内有效').waitFor();
  await panel.getByRole('button', { name: '刷新待确认设备', exact: true }).click();
  await panel.getByRole('button', { name: '确认这台手机', exact: true }).waitFor();
  await shot('reading-sync-pairing');
  // 清空清单必须二次确认；取消不产生同步请求。
  await panel.getByText('阅读同步合成资料', { exact: true }).click();
  await panel.getByRole('button', { name: '保存选择', exact: true }).click();
  await panel.getByRole('button', { name: '立即同步', exact: true }).click();
  await win.getByRole('button', { name: '取消', exact: true }).click();
  const count = await app.evaluate(() => globalThis.readingPreview.calls.filter(action => action === 'sync').length);
  assert.equal(count, 1);
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(1000, 850));
  assert.equal(await panel.evaluate(element => element.scrollWidth > element.clientWidth + 1), false);
  await shot('reading-sync-narrow');
  await win.evaluate(() => {
    const settings = JSON.parse(localStorage.getItem('guizhi-settings'));
    Object.assign(settings.state, { themeMode: 'dark', isDarkMode: true });
    localStorage.setItem('guizhi-settings', JSON.stringify(settings));
  });
  await win.reload();
  await win.getByTestId('rail-settings').click();
  await win.getByTestId('settings-nav-mobile-capture').click();
  await panel.waitFor();
  await shot('reading-sync-dark');
  console.log('PASS: 阅读同步开通、选库保存、配对二维码、确认设备、撤回取消保护、深浅主题及窄屏布局；使用隔离 IPC 合成样例。');
};
