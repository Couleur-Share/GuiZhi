import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

export default async ({ win, shot, outDir }) => {
  const data = JSON.parse(await fs.readFile(path.resolve('../../artifacts/themed-reading/composition-live/fixtures.json'), 'utf8'));
  const origin = new URL('/composition-live/', win.url()).href;
  await win.goto('about:blank');
  let html = '';
  const requests = [], cases = [];
  await win.context().route('**/*', route => route.abort());
  await win.context().route(`${origin}**`, route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: html }));
  win.context().on('request', request => { if (!request.url().startsWith(origin)) requests.push(request.url()); });
  await win.setContent('<style>html,body{margin:0;height:100%}iframe{border:0;width:100%;height:100%;display:block}</style><iframe sandbox="allow-scripts"></iframe>');
  for (const fixture of data.fixtures) {
    assert.equal(fixture.success, true);
    for (const mode of ['light', 'dark']) for (const width of [1440, 420]) {
      await win.setViewportSize({ width, height: 1100 }); await win.emulateMedia({ colorScheme: mode });
      html = await fs.readFile(fixture.files.offline, 'utf8');
      const url = `${origin}${fixture.id}-${mode}-${width}.html`, iframe = win.locator('iframe');
      await iframe.evaluate((element, url) => element.src = url, url);
      const frame = await (await iframe.elementHandle()).contentFrame(); await frame.waitForURL(url);
      await frame.waitForFunction(() => document.querySelector('.gz-title') && [...document.images].every(image => image.complete && image.naturalWidth > 0));
      await win.waitForTimeout(200); await shot(`${fixture.id}-${mode}-${width}-top`);
      const overflow = await frame.evaluate(() => ({ width: innerWidth, content: document.documentElement.scrollWidth }));
      assert(overflow.content <= overflow.width + 2);
      await frame.locator('.gz-matrix').first().evaluate(node => node.scrollIntoView()); await win.waitForTimeout(220); await shot(`${fixture.id}-${mode}-${width}-matrix`);
      await frame.locator('.gz-tool').first().evaluate(node => node.scrollIntoView()); await win.waitForTimeout(220);
      if (fixture.id === 'fish-oil') {
        const inputs = frame.locator('[data-gz-calculator="unit-cost"] input');
        for (const [i, value] of ['200', '90', '1'].entries()) await inputs.nth(i).fill(value);
        assert.match(await frame.locator('output').textContent(), /2.22 元/);
      } else {
        const choice = frame.locator('[data-gz-choice]').last(); await choice.focus(); await win.keyboard.press('Space');
        assert.equal(await choice.getAttribute('aria-pressed'), 'true');
        assert.equal(await frame.locator('.gz-panel:not([hidden])').count(), 1);
      }
      await win.waitForTimeout(200); await shot(`${fixture.id}-${mode}-${width}-tool`);
      await frame.locator('.gz-evidence a').first().click(); await win.waitForTimeout(150);
      assert.equal(await frame.locator('.gz-original').evaluate(node => node.open), true);
      const texts = await frame.locator('[data-source-block]').evaluateAll(nodes => nodes.map(node => node.textContent.replace(/\s+/g, ' ').trim()));
      assert.deepEqual(texts, fixture.page.source.blocks.map(block => block.text.replace(/\s+/g, ' ').trim()));
      cases.push({ id: fixture.id, mode, width, blocks: texts.length, overflow, interactive: true, sourceJump: true });
    }
  }
  assert.deepEqual(requests, []);
  await fs.writeFile(path.join(outDir, 'live-evidence.json'), JSON.stringify({ success: true, model: data.model, modelCalls: data.modelCalls, imageCalls: data.imageCalls, requests, cases }, null, 2));
};
