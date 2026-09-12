import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

export default async ({ win, app, shot, outDir }) => {
  if (!process.env.GUIZHI_GRAPHICS_LIVE_CONFIG) throw new Error('缺少只读模型配置路径');
  const live = await app.evaluate(async (_electron, input) => globalThis.readingGraphicsLiveFixture(input.out, input.config), { out: outDir, config: process.env.GUIZHI_GRAPHICS_LIVE_CONFIG });
  console.log(JSON.stringify(live));
  assert.notEqual(live.success, false); assert.deepEqual(live.errors, []);
  await win.goto('about:blank');
  await win.setContent('<iframe sandbox="allow-scripts" style="width:100%;height:1000px;border:0"></iframe>');
  await win.locator('iframe').evaluate((el, html) => { el.srcdoc = html; }, await fs.readFile(path.join(outDir, 'live-offline.html'), 'utf8'));
  const frame = await (await win.locator('iframe').elementHandle()).contentFrame();
  await frame.locator('h1').waitFor();
  await shot('live-reading-top');
  for (const v of live.visuals) { await frame.locator(`[data-reading-visual="${v.id}"]`).scrollIntoViewIfNeeded(); await shot(`live-reading-${v.id}`); }
};
