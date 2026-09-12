import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

export default async ({win,shot,outDir}) => {
  const data = JSON.parse(await fs.readFile(path.resolve(process.env.GUIZHI_RECONSTRUCTION_FIXTURES || '../../artifacts/themed-reading/reconstruction-fixture/fixtures.json'),'utf8'));
  const cases = [], errors = [], requests = []; let html = '';
  const origin = new URL('/reading-reconstruction/',win.url()).href;
  await win.goto('about:blank'); await win.setContent('<iframe sandbox="allow-scripts" style="position:fixed;inset:0;width:100%;height:100%;border:0"></iframe>');
  win.on('pageerror', e => errors.push(e.message));
  await win.context().route('**/*', async route => { if (route.request().url().startsWith(origin)) await route.fulfill({contentType:'text/html',body:html}); else { requests.push(route.request().url()); await route.abort(); } });
  for (const fixture of data.fixtures) {
    assert(fixture.success);
    for (const mode of ['light','dark']) for (const width of [1440,420]) {
      await win.setViewportSize({width,height:1100}); await win.emulateMedia({colorScheme:mode}); html = await fs.readFile(fixture.files.offline,'utf8');
      const url = `${origin}${fixture.id}-${mode}-${width}.html`;
      await win.locator('iframe').evaluate((el,url)=>el.src=url,url);
      const frame = await (await win.locator('iframe').elementHandle()).contentFrame(); await frame.waitForURL(url);
      await frame.waitForFunction(()=>document.querySelector('h1')&&[...document.images].every(i=>i.complete)); await win.waitForTimeout(200);
      assert.equal(await frame.locator('html').getAttribute('data-theme'),mode);
      assert(await frame.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2));
      const rendered = await frame.locator('body').innerText(); assert(!rendered.includes('核对原文')); assert(!rendered.includes('原文归纳')); assert.equal(await frame.locator('[data-source-block]').count(),0);
      await shot(`${fixture.id}-${mode}-${width}-top`);
      const sections = frame.locator('h2'); await sections.nth(Math.floor(await sections.count()/2)).evaluate(el=>el.scrollIntoView()); await win.waitForTimeout(150); await shot(`${fixture.id}-${mode}-${width}-middle`);
      const calculators = frame.locator('[data-reading-tool]:has(input)');
      for (let i=0;i<await calculators.count();i++) {
        const calc=calculators.nth(i),inputs=calc.locator('input');
        for (let j=0;j<await inputs.count();j++) await inputs.nth(j).fill(j===0?'200':j===1?'90':'1');
        assert(!/未知变量|不支持|__name|未定义/.test(await calc.locator('output').innerText()));
        await inputs.first().fill(''); assert.match(await calc.locator('output').innerText(),/填写/);
        for (let j=0;j<await inputs.count();j++) await inputs.nth(j).fill(j===0?'200':j===1?'90':'1');
      }
      const choices = frame.locator('[data-reading-tool] button');
      for(let choiceIndex=0;choiceIndex<await choices.count();choiceIndex++){
        await choices.nth(choiceIndex).focus();await win.keyboard.press('Space');assert.equal(await choices.nth(choiceIndex).getAttribute('aria-pressed'),'true');
        if(width===1440&&mode==='light'){await choices.nth(choiceIndex).evaluate(el=>el.scrollIntoView());await shot(`${fixture.id}-choice-${choiceIndex}`);}
      }
      if(await frame.locator('[data-reading-tool]').count()) await frame.locator('[data-reading-tool]').last().evaluate(el=>el.scrollIntoView());
      await win.waitForTimeout(150); await shot(`${fixture.id}-${mode}-${width}-tools`);
      const firstLink=frame.locator('a[href^="#"]').first(); if(await firstLink.count()){await firstLink.click(); await win.waitForTimeout(100);}
      cases.push({id:fixture.id,mode,width,calculators:await calculators.count(),choices:await choices.count()});
    }
  }
  assert.deepEqual(errors,[]); assert.deepEqual(requests,[]);
  await fs.writeFile(path.join(outDir,'evidence.json'),JSON.stringify({success:true,fixtureOnly:!!data.fixtureOnly,offline:true,webVerified:false,cases,errors,requests},null,2));
};
