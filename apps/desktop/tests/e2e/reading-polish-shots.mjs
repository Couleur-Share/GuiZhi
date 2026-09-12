import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

export default async ({ win, app, shot, outDir }) => {
  const compiled = await app.evaluate(async (_e, out) => globalThis.readingGraphicsFixture(out), outDir);
  assert(compiled.success);
  const html = await fs.readFile(path.join(outDir, 'user-page.html'), 'utf8');
  await win.goto('about:blank');
  const cases = [], requests = [];
  await win.context().route('**/*', route => { requests.push(route.request().url()); return route.abort(); });
  for (const width of [1400, 360]) for (const theme of ['light','dark']) {
    await win.setViewportSize({ width, height: 1000 });
    await win.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    await win.setContent('<iframe sandbox="allow-scripts" style="width:100%;height:970px;border:0"></iframe>');
    await win.locator('iframe').evaluate((el, html) => { el.srcdoc = html; }, html);
    const frame = await (await win.locator('iframe').elementHandle()).contentFrame();
    await frame.locator('[data-reading-visual="freshbridge"] svg').waitFor();
    for (const id of ['freshbridge','labelguide']) {
      const visual = frame.locator(`[data-reading-visual="${id}"]`);
      await visual.scrollIntoViewIfNeeded();
      const metrics = await visual.locator('.gz-system-visual-scroll').evaluate(el => ({
        scrollbar: getComputedStyle(el, '::-webkit-scrollbar').height,
        button: getComputedStyle(el, '::-webkit-scrollbar-button').display,
        thumb: getComputedStyle(el, '::-webkit-scrollbar-thumb').backgroundColor,
        overflow: el.scrollWidth > el.clientWidth + 1,
        documentOverflow: document.documentElement.scrollWidth > innerWidth + 2,
      }));
      assert.equal(metrics.scrollbar, '7px'); assert.equal(metrics.button, 'none'); assert.equal(metrics.documentOverflow, false);
      if (width === 1400) assert.equal(metrics.overflow, false);
      cases.push({width,theme,id,...metrics}); await shot(`reading-${width}-${theme}-${id}`);
    }
  }
  assert.deepEqual(requests, []);
  await fs.writeFile(path.join(outDir, 'polish-evidence.json'), JSON.stringify({cases,requests}, null, 2));
};
