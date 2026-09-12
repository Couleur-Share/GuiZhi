import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

export default async ({ win, app, shot, outDir, mainEntry }) => {
  const result = await app.evaluate(async (_electron, { entry, out }) => {
    return await globalThis.readingGraphicsFixture(out);
  }, { entry: path.join(path.dirname(mainEntry), 'reading-graphics-fixture.js'), out: outDir });
  await fs.writeFile(path.join(outDir, 'compile-evidence.json'), JSON.stringify(result, null, 2));
  assert.equal(result.success, true, JSON.stringify(result.errors));
  const requests = [], errors = [], cases = [];
  const offline = await fs.readFile(path.join(outDir, 'offline.html'), 'utf8');
  const embedded = await fs.readFile(path.join(outDir, 'embedded.html'), 'utf8');
  const page = JSON.parse(await fs.readFile(path.join(outDir, 'page.json'), 'utf8'));
  const item = await win.evaluate(async content => window.api.knowledge.create({ title: '阅读图形隔离验收', itemType: 'note', content }), page.source.content);
  await app.evaluate(({ ipcMain }, { page, html, itemId }) => {
    ipcMain.removeHandler('themedReading:get'); ipcMain.removeHandler('themedReading:state');
    ipcMain.handle('themedReading:state', () => ({ success: true, state: { hasPage: true, versionId: page.id, formatVersion: 2, stale: false } }));
    ipcMain.handle('themedReading:get', (_e, input) => ({ success: true, page: { ...page, itemId }, document: html.replace('data-instance="graphics-fixture"', `data-instance="${input.instanceId}"`), models: { text: 'fixture', image: null } }));
  }, { page, html: embedded, itemId: item.id });
  await win.evaluate(() => {
    localStorage.setItem('guizhi-setup-dismissed', '1');
    const settings = JSON.parse(localStorage.getItem('guizhi-settings') || '{"state":{}}');
    Object.assign(settings.state, { language: 'zh', themeMode: 'light', isDarkMode: false, editorMarkdownPreview: true });
    localStorage.setItem('guizhi-settings', JSON.stringify(settings));
  });
  await win.reload(); await win.getByTestId('item-list').getByText(item.title, { exact: true }).click();
  const appFrame = win.frameLocator('iframe[data-article-instance]');
  await appFrame.locator('[data-reading-visual="flow"] svg').waitFor();
  await shot('graphics-real-reader');
  const host = win.getByTestId('themed-reading-frame').locator('div.overflow-auto');
  const top = await appFrame.locator('[data-reading-visual="chart"]').evaluate(el => el.getBoundingClientRect().top);
  await host.evaluate((el, top) => { el.scrollTop = top; }, top);
  await appFrame.locator('[data-reading-visual="chart"][data-motion-state]').waitFor();
  await shot('graphics-real-reader-chart');
  const chart = appFrame.locator('[data-reading-visual="chart"]');
  await chart.getByRole('button', { name: '重播动画', exact: true }).click();
  await new Promise(resolve => setTimeout(resolve, 120));
  const chartRunning = await chart.evaluate(el => el.getAnimations({ subtree: true }).map(a => ({ state: a.playState, time: a.currentTime })));
  assert(chartRunning.some(a => a.state === 'running' && a.time > 0));
  await chart.getByRole('button', { name: '暂停动画', exact: true }).click();
  const chartPaused = await chart.evaluate(el => el.getAnimations({ subtree: true }).map(a => a.currentTime));
  await new Promise(resolve => setTimeout(resolve, 160));
  assert.deepEqual(await chart.evaluate(el => el.getAnimations({ subtree: true }).map(a => a.currentTime)), chartPaused);
  await chart.getByRole('button', { name: '继续动画', exact: true }).click();
  await appFrame.locator('[data-reading-tool="cost"] input').first().fill('100');
  await appFrame.locator('[data-reading-tool="cost"] input').last().fill('5');
  assert.match(await appFrame.locator('[data-reading-tool="cost"] output').innerText(), /20/);
  await appFrame.getByRole('button', { name: '关闭动画', exact: true }).click();
  await win.waitForFunction(() => true);
  assert.equal(await appFrame.locator('html').getAttribute('data-reading-motion'), 'off');
  await win.goto('about:blank');
  win.on('pageerror', e => errors.push(e.message));
  await win.context().route('**/*', route => { requests.push(route.request().url()); return route.abort(); });
  for (const width of [1440, 360]) for (const theme of ['light', 'dark']) {
    await win.setViewportSize({ width, height: 1000 });
    await win.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    await win.setContent('<iframe sandbox="allow-scripts" style="width:100%;height:960px;border:0"></iframe>');
    await win.locator('iframe').evaluate((el, html) => { el.srcdoc = html; }, offline);
    const frame = await (await win.locator('iframe').elementHandle()).contentFrame();
    await frame.locator('[data-reading-visual="flow"] svg').waitFor();
    const metrics = await frame.evaluate(() => ({ overflow: document.documentElement.scrollWidth > innerWidth + 2, figures: document.querySelectorAll('[data-reading-visual] svg').length, motion: document.documentElement.dataset.readingMotion, labels: [...document.querySelectorAll('svg text')].map(e => e.textContent) }));
    assert.equal(metrics.overflow, false); assert.equal(metrics.figures, 6); assert.equal(metrics.motion, 'off'); assert(metrics.labels.some(s => s.includes('采集')));
    for (const id of ['flow', 'sequence', 'state', 'mind', 'chart', 'custom']) { await frame.locator(`[data-reading-visual="${id}"]`).scrollIntoViewIfNeeded(); await shot(`graphics-${width}-${theme}-${id}`); }
    cases.push({ width, theme, ...metrics });
  }
  for (const type of ['line', 'area', 'pie', 'donut']) {
    await win.locator('iframe').evaluate((el, html) => { el.srcdoc = html; }, await fs.readFile(path.join(outDir, `chart-${type}.html`), 'utf8'));
    const chartFrame = await (await win.locator('iframe').elementHandle()).contentFrame();
    await chartFrame.locator('[data-reading-visual="chart"] svg').waitFor();
    await chartFrame.locator('[data-reading-visual="chart"]').scrollIntoViewIfNeeded(); await shot(`chart-${type}-360-dark`);
  }
  const openingSamples = [];
  for (const name of ['plain', 'components', 'diagrams', 'animated']) {
    await win.setContent('<iframe sandbox="allow-scripts" style="width:100%;height:960px;border:0"></iframe>');
    const html = await fs.readFile(path.join(outDir, `variant-${name}.html`), 'utf8');
    const start = performance.now();
    await win.locator('iframe').evaluate((el, html) => { el.srcdoc = html; }, html);
    const sample = await (await win.locator('iframe').elementHandle()).contentFrame();
    await sample.locator('h1').waitFor();
    await sample.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    openingSamples.push({ name, openToTwoFramesMs: performance.now() - start, heapSample: await sample.evaluate(() => performance.memory?.usedJSHeapSize ?? null) });
  }
  const performanceFile = path.join(outDir, 'performance.json');
  const performanceData = JSON.parse(await fs.readFile(performanceFile, 'utf8'));
  await fs.writeFile(performanceFile, JSON.stringify({ ...performanceData, openingSamples, openingNote: '同一隔离浏览器在360px下各打开一次，包含自动化往返开销；内存为浏览器采样值，非峰值或库净增量。' }, null, 2));
  // 模拟真实伸高 iframe，外层滚动产生坐标消息，避免 iframe 内视口误触发。
  await win.emulateMedia({ reducedMotion: 'no-preference' });
  await win.setViewportSize({ width: 1000, height: 900 });
  await win.setContent('<div id="host" style="height:850px;overflow:auto"><iframe sandbox="allow-scripts" scrolling="no" style="width:100%;height:6000px;border:0"></iframe></div>');
  await win.locator('iframe').evaluate((el, html) => { el.srcdoc = html; }, embedded);
  const frame = await (await win.locator('iframe').elementHandle()).contentFrame();
  await frame.locator('[data-reading-visual="custom"] svg').waitFor();
  assert.equal(await frame.locator('[data-reading-visual="custom"]').getAttribute('data-motion-state'), null);
  const snapshotMotion = () => frame.locator('[data-reading-visual="custom"] svg').evaluate(el => el.outerHTML);
  const animationEvidence = { initial: await snapshotMotion() };
  const position = await frame.locator('[data-reading-visual="custom"]').evaluate(el => el.getBoundingClientRect().top);
  await win.evaluate(top => { document.querySelector('iframe').contentWindow.postMessage({ id: 'graphics-fixture', type: 'reading-viewport', value: { top, bottom: top + 800, visible: true } }, '*'); }, position);
  const custom = frame.locator('[data-reading-visual="custom"]');
  await custom.getByRole('button', { name: '播放动画', exact: true }).click({ force: true });
  await frame.waitForFunction(() => document.querySelector('[data-reading-visual="custom"]').dataset.motionState === 'playing');
  await win.evaluate(top => document.querySelector('iframe').contentWindow.postMessage({ id: 'graphics-fixture', type: 'reading-viewport', value: { top, bottom: top + 800, visible: false } }, '*'), position);
  await frame.waitForFunction(() => document.querySelector('[data-reading-visual="custom"]').dataset.motionState === 'paused');
  await win.evaluate(top => document.querySelector('iframe').contentWindow.postMessage({ id: 'graphics-fixture', type: 'reading-viewport', value: { top, bottom: top + 800, visible: true } }, '*'), position);
  await frame.waitForFunction(() => document.querySelector('[data-reading-visual="custom"]').dataset.motionState === 'playing');
  await new Promise(resolve => setTimeout(resolve, 180));
  await custom.getByRole('button', { name: '暂停动画', exact: true }).click({ force: true });
  await win.evaluate(top => document.querySelector('iframe').contentWindow.postMessage({ id: 'graphics-fixture', type: 'reading-viewport', value: { top, bottom: top + 800, visible: true } }, '*'), position);
  const before = await snapshotMotion();
  assert.notEqual(before, animationEvidence.initial);
  animationEvidence.paused = before;
  await new Promise(resolve => setTimeout(resolve, 180));
  animationEvidence.afterPausedWait = await snapshotMotion();
  assert.equal(animationEvidence.afterPausedWait, before);
  await custom.getByRole('button', { name: '继续动画', exact: true }).click({ force: true });
  await frame.waitForFunction(() => document.querySelector('[data-reading-visual="custom"]').dataset.motionState === 'done');
  await win.evaluate(top => document.querySelector('iframe').contentWindow.postMessage({ id: 'graphics-fixture', type: 'reading-viewport', value: { top, bottom: top + 800, visible: true } }, '*'), position);
  await new Promise(resolve => setTimeout(resolve, 100));
  assert.equal(await custom.getAttribute('data-motion-state'), 'done');
  animationEvidence.finished = await snapshotMotion();
  assert.equal(animationEvidence.finished, animationEvidence.initial);
  await custom.getByRole('button', { name: '重播动画', exact: true }).click({ force: true });
  assert.equal(await custom.getAttribute('data-motion-state'), 'playing');
  assert.equal(await frame.locator('[data-motion-state="playing"]').count(), 1);
  animationEvidence.replay = await snapshotMotion();
  assert.notEqual(animationEvidence.replay, animationEvidence.initial);
  await frame.getByRole('button', { name: '关闭动画', exact: true }).evaluate(el => el.click());
  assert.equal(await frame.locator('html').getAttribute('data-reading-motion'), 'off');
  assert.equal(await frame.locator('#custom-path').getAttribute('style'), null);
  const widthBefore = await frame.locator('[data-reading-visual="custom"] svg').evaluate(el => el.getBoundingClientRect().width);
  await win.evaluate(() => document.querySelector('iframe').contentWindow.postMessage({ id: 'graphics-fixture', type: 'appearance', value: { theme: 'dark', fontSize: 20 } }, '*'));
  await frame.waitForFunction(() => document.documentElement.style.getPropertyValue('--reader-font-size') === '20px');
  const widthAfter = await frame.locator('[data-reading-visual="custom"] svg').evaluate(el => el.getBoundingClientRect().width);
  assert(Math.abs(widthAfter / widthBefore - 1.25) < .01);
  await win.emulateMedia({ media: 'print' });
  assert.equal(await frame.locator('.gz-system-motion-controls').first().evaluate(el => getComputedStyle(el).display), 'none');
  await win.emulateMedia({ media: 'screen' });
  await win.setContent('<iframe sandbox="" style="width:100%;height:850px;border:0"></iframe>');
  await win.locator('iframe').evaluate((el, html) => { el.srcdoc = html; }, offline);
  const staticFrame = await (await win.locator('iframe').elementHandle()).contentFrame();
  await staticFrame.locator('[data-reading-visual="chart"] svg').waitFor();
  assert.equal(await staticFrame.locator('.gz-system-motion-controls').count(), 0);
  assert.equal(await staticFrame.locator('[data-reading-visual="chart"] path').last().evaluate(el => getComputedStyle(el).animationName), 'none');
  assert.deepEqual(requests, []); assert.deepEqual(errors, []);
  await fs.writeFile(path.join(outDir, 'evidence.json'), JSON.stringify({ success: true, fixtureOnly: true, modelVerified: false, compilation: result, cases, animationPauseResumeReplay: true, backgroundPause: true, manualPausePreserved: true, autoplayOnce: true, printStatic: true, scriptDisabledStatic: true, fontScaling: true, animationEvidence, chartAnimationEvidence: { chartRunning, chartPaused }, requests, errors }, null, 2));
  if (process.env.GUIZHI_GRAPHICS_LIVE_CONFIG) {
    console.log('开始当前配置模型的端到端生成验收（合成教学材料，不联网搜索）');
    const live = await app.evaluate(async (_electron, { directory, config }) => globalThis.readingGraphicsLiveFixture(directory, config), { directory: outDir, config: process.env.GUIZHI_GRAPHICS_LIVE_CONFIG });
    console.log(JSON.stringify(live)); assert.deepEqual(live.errors, []);
  }
};
