import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

export default async ({ win, shot, outDir }) => {
  const { fixtures } = JSON.parse(await fs.readFile(path.resolve('../../artifacts/themed-reading/composition/fixtures.json'), 'utf8'));
  const origin = new URL('/composition-fixture/', win.url()).href;
  await win.goto('about:blank');
  const requests = [], report = { cases: [], interactions: [], requests, success: false, modelCalls: 0 };
  let html = '', frame;
  await win.context().route('**/*', route => route.abort());
  await win.context().route(`${origin}**`, route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: html }));
  await win.context().setOffline(true);
  win.context().on('request', request => { if (!request.url().startsWith(origin)) requests.push(request.url()); });
  await win.setContent('<style>html,body{margin:0;height:100%;overflow:hidden}#reader{height:100%;overflow:auto}iframe{display:block;width:100%;height:1100px;border:0}</style><div id="reader"><iframe sandbox="allow-scripts"></iframe></div>');
  await win.evaluate(() => window.addEventListener('message', event => {
    const iframe = document.querySelector('iframe');
    if (event.source !== iframe.contentWindow || event.data?.id !== 'composition-fixture') return;
    if (event.data.type === 'height' && Number.isFinite(event.data.value)) iframe.style.height = `${event.data.value}px`;
    if (event.data.type === 'anchor' && Number.isFinite(event.data.value)) document.querySelector('#reader').scrollTop = event.data.value;
  }));
  const iframe = win.locator('iframe');
  const load = async (fixture, mode, width, embedded = false) => {
    await win.setViewportSize({ width, height: 1100 });
    await win.emulateMedia({ colorScheme: embedded ? (mode === 'light' ? 'dark' : 'light') : mode });
    html = await fs.readFile(fixture.files[embedded ? 'embedded' : 'offline'], 'utf8');
    const url = `${origin}${fixture.id}-${mode}-${width}.html`;
    await iframe.evaluate((element, { html, url, embedded }) => {
      element.style.height = '1100px';
      if (embedded) element.srcdoc = html;
      else { element.removeAttribute('srcdoc'); element.src = url; }
      document.querySelector('#reader').scrollTop = 0;
    }, { html, url, embedded });
    frame = await (await iframe.elementHandle()).contentFrame();
    await frame.waitForURL(embedded ? 'about:srcdoc' : url);
    await frame.waitForFunction(() => document.querySelector('.gz-title') && [...document.images].every(image => image.complete && image.naturalWidth > 0));
    if (embedded) {
      await iframe.evaluate((element, mode) => element.contentWindow.postMessage({ id: 'composition-fixture', type: 'appearance', value: { theme: mode, fontSize: 16 } }, '*'), mode);
      await frame.waitForFunction(mode => document.documentElement.dataset.theme === mode, mode);
    }
    await win.waitForTimeout(180);
  };
  const captureSection = async (selector, name) => {
    await frame.locator(selector).first().evaluate(node => node.scrollIntoView());
    await win.waitForTimeout(220); await shot(name);
  };
  try {
    for (const fixture of fixtures) {
      for (const mode of ['light', 'dark']) for (const width of [1440, 900, 420]) {
        await load(fixture, mode, width);
        const state = await frame.evaluate(() => {
          const rect = node => { const r = node.getBoundingClientRect(); return { x: r.x, right: r.right, width: r.width, height: r.height }; };
          const contrast = node => {
            let parent = node;
            while (parent.parentElement && ['transparent', 'rgba(0, 0, 0, 0)'].includes(getComputedStyle(parent).backgroundColor)) parent = parent.parentElement;
            const luminance = color => color.match(/[\d.]+/g).slice(0, 3).map(Number).map(v => { v /= 255; return v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4; }).reduce((sum, v, i) => sum + v * [.2126, .7152, .0722][i], 0);
            const a = luminance(getComputedStyle(node).color), b = luminance(getComputedStyle(parent).backgroundColor);
            return (Math.max(a, b) + .05) / (Math.min(a, b) + .05);
          };
          return { width: innerWidth, scrollWidth: document.documentElement.scrollWidth, source: [...document.querySelectorAll('[data-source-block]')].map(node => ({ id: node.dataset.sourceBlock, text: node.textContent.replace(/\s+/g, ' ').trim() })),
            cards: [...document.querySelectorAll('.gz-card')].filter(node => !node.closest('[hidden]')).map(rect),
            contrast: [...document.querySelectorAll('.gz-title,.gz-card h3,.gz-card p,.gz-evidence,.gz-section-label')].filter(node => node.getBoundingClientRect().width).map(contrast),
            title: document.querySelector('h1').textContent, originalOpen: document.querySelector('.gz-original').open,
            scripts: document.scripts.length, api: typeof window.api };
        });
        assert.equal(state.title, fixture.page.design.composition.chapters[0].displayTitle ?? fixture.title); assert.equal(state.originalOpen, false); assert.equal(state.scripts, 1); assert.equal(state.api, 'undefined');
        assert(state.scrollWidth <= state.width + 2, '整页不横向溢出');
        assert(state.cards.every(card => card.x >= 0 && card.right <= state.width && card.height > 0));
        assert(state.contrast.every(value => value >= 4.5), `对比度不足 ${Math.min(...state.contrast)}`);
        assert.deepEqual(state.source, fixture.page.source.blocks.map(block => ({ id: block.id, text: block.text.replace(/\s+/g, ' ').trim() })));
        report.cases.push({ id: fixture.id, mode, width, ...state });
        await shot(`${fixture.id}-${mode}-${width}-top`);
        if (width !== 900) {
          await captureSection('#gz-0-2', `${fixture.id}-${mode}-${width}-detail`);
          await captureSection('.gz-tool', `${fixture.id}-${mode}-${width}-tool`);
        }
      }
      await load(fixture, 'light', 1440);
      if (fixture.id === 'fish-oil') {
        const daily = frame.locator('[data-gz-calculator="daily-total"]');
        for (const [i, value] of ['360', '243', '2'].entries()) await daily.locator('input').nth(i).fill(value);
        assert.match(await daily.locator('output').textContent(), /1,206 mg/);
        const cost = frame.locator('[data-gz-calculator="unit-cost"]');
        for (const [i, value] of ['200', '90', '1'].entries()) await cost.locator('input').nth(i).fill(value);
        assert.match(await cost.locator('output').textContent(), /2.22 元/);
        await captureSection('[data-gz-calculator="unit-cost"]', 'fish-cost-result');
        await cost.locator('input').nth(1).fill('0'); assert.match(await cost.locator('output').textContent(), /不能为零/);
        await cost.locator('input').nth(1).fill('-1'); assert.match(await cost.locator('output').textContent(), /有效数值/);
        await cost.locator('input').nth(1).fill(''); assert.match(await cost.locator('output').textContent(), /填写/);
      } else {
        const buttons = frame.locator('[data-gz-choice]');
        await buttons.nth(1).focus(); await win.keyboard.press('Space');
        assert.equal(await buttons.nth(1).getAttribute('aria-pressed'), 'true');
        assert.equal(await frame.locator('.gz-panel:not([hidden]) h3').textContent(), '鲜');
        await captureSection('.gz-explorer', 'beer-explorer-selection');
      }
      await frame.locator('.gz-theme').click();
      assert.equal(await frame.locator('html').getAttribute('data-theme'), 'dark');
      await frame.locator('.gz-evidence a').first().click();
      assert.equal(await frame.locator('.gz-original').evaluate(node => node.open), true);
      assert(await frame.evaluate(() => scrollY) > 500);
      report.interactions.push({ id: fixture.id, offline: true, calculatorOrExplorer: true, sourceJump: true, theme: true });
      for (const mode of ['light', 'dark']) {
        await load(fixture, mode, 1280, true);
        assert.equal(await frame.locator('script').count(), 2);
        assert.equal(await frame.locator('.gz-theme').isVisible(), false);
        await frame.locator('.gz-evidence a').first().click();
        await win.waitForTimeout(200);
        assert.equal(await frame.locator('.gz-original').evaluate(node => node.open), true);
        assert(await win.locator('#reader').evaluate(node => node.scrollTop) > 500);
        report.interactions.push({ id: fixture.id, embedded: true, mode, sourceJump: true });
      }
    }
    assert.deepEqual(requests, []); report.success = true;
  } finally { await fs.writeFile(path.join(outDir, 'composition-evidence.json'), JSON.stringify(report, null, 2)); }
};
