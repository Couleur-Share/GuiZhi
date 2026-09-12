import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

export default async ({ win, app, shot, outDir }) => {
  const result = await app.evaluate(async (_e, out) => globalThis.readingGraphicsFixture(out), outDir);
  assert(result.success);
  const html = await fs.readFile(path.join(outDir, 'required-image.html'), 'utf8');
  await win.goto('about:blank');
  const requests = [], cases = [];
  await win.context().route('**/*', route => { requests.push(route.request().url()); return route.abort(); });
  for (const width of [1200,360]) for (const theme of ['light','dark']) {
    await win.setViewportSize({width,height:900});
    await win.emulateMedia({colorScheme:theme,reducedMotion:'reduce'});
    await win.setContent('<style>body{margin:0;overflow:hidden}</style><iframe sandbox="allow-scripts" style="width:100%;height:900px;border:0"></iframe>');
    await win.locator('iframe').evaluate((el, html) => {el.srcdoc=html;},html);
    const frame = await (await win.locator('iframe').elementHandle()).contentFrame();
    const pic = frame.locator('img[data-theme-asset="requested"]');
    await pic.waitFor(); await pic.scrollIntoViewIfNeeded();
    const metrics = await pic.evaluate(el => ({decoded:el.complete&&el.naturalWidth>0,width:el.getBoundingClientRect().width,overflow:document.documentElement.scrollWidth>innerWidth+2,body:document.body.textContent}));
    assert(metrics.decoded); assert(metrics.width>0); assert.equal(metrics.overflow,false);
    assert(metrics.body.includes('这是一段扩写后的文章说明')); assert.equal(await pic.count(),1);
    cases.push({width,theme,decoded:metrics.decoded,imageWidth:metrics.width,overflow:metrics.overflow});
    await shot(`required-image-${width}-${theme}`);
  }
  assert.deepEqual(requests,[]);
  await fs.writeFile(path.join(outDir,'required-image-evidence.json'),JSON.stringify({fixtureOnly:true,paidImageRequests:0,cases,requests},null,2));
};
