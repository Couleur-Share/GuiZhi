import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

export default async ({win,app,shot,outDir}) => {
  const data=JSON.parse(await fs.readFile(path.resolve(process.env.GUIZHI_RECONSTRUCTION_FIXTURES || '../../artifacts/themed-reading/reconstruction-fixture/fixtures.json'),'utf8'));
  const fixtures=data.fixtures;
  if(data.fixtureOnly)for(const f of fixtures)f.page.source.content += '\n\n'+Array.from({length:12},(_,i)=>`### 原文章节 ${i+1}\n\n${'用于验证原文滚动和独立目录的测试段落。'.repeat(20)}`).join('\n\n');
  for(const fixture of fixtures)fixture.document=await fs.readFile(fixture.files.embedded,'utf8');
  const items=await win.evaluate(async fixtures=>{
    const settings=JSON.parse(localStorage.getItem('guizhi-settings')||'{"state":{}}');Object.assign(settings.state,{language:'zh',themeMode:'light',isDarkMode:false,editorMarkdownPreview:true});localStorage.setItem('guizhi-settings',JSON.stringify(settings));localStorage.setItem('guizhi-setup-dismissed','1');localStorage.setItem('guizhi-migration-dismissed','1');
    const out=[];for(const f of fixtures){out.push(await window.api.knowledge.create({title:f.title,itemType:f.page.sourceKind==='summary'?'forum':'note',content:f.page.sourceKind==='summary'?`> 平台：测试论坛\n\n## 讨论总结\n\n${f.page.source.content}\n\n## 正文\n\n主楼必须保留\n\n## 讨论（1 条）\n\n### 1 楼 · 用户\n\n回复必须保留`:f.page.source.content}));}return out;
  },fixtures);
  await app.evaluate(({ipcMain},{fixtures,items})=>{
    for(const name of ['themedReading:get','themedReading:state'])ipcMain.removeHandler(name);
    const find=input=>fixtures[items.findIndex(item=>item.id===input.itemId)];
    ipcMain.handle('themedReading:state',(_e,input)=>{const f=find(input);return {success:true,state:{hasPage:!!f,versionId:f?.page.id,formatVersion:2,stale:false}}});
    ipcMain.handle('themedReading:get',(_e,input)=>{const f=find(input);if(!f)return {success:true,page:null,models:{text:'fixture-model',image:null}};return {success:true,page:{...f.page,itemId:input.itemId,sourceKind:input.sourceKind},document:f.document.replace('data-instance="reconstruction-fixture"',`data-instance="${input.instanceId}"`),models:{text:'fixture-model',image:null},search:{configured:false,persistent:true,provider:'tavily'}}});
  },{fixtures,items});
  await win.reload();const report=[];
  for(const [index,f] of fixtures.entries()){
    await win.setViewportSize({width:1680,height:1100});
    await win.getByTestId('item-list').getByText(items[index].title,{exact:true}).click();
    if(f.page.sourceKind==='summary')await win.getByRole('button',{name:'讨论总结',exact:true}).click();
    const frame=win.frameLocator('iframe[aria-label="AI 重构阅读"]');await frame.locator('h1').waitFor();await win.waitForTimeout(200);
    assert.equal(await win.getByRole('button',{name:'AI 阅读',exact:true}).getAttribute('aria-pressed'),'true');await shot(`${f.id}-app-ai`);
    await win.evaluate(()=>document.documentElement.classList.add('dark'));await win.waitForTimeout(150);assert.equal(await frame.locator('html').getAttribute('data-theme'),'dark');await shot(`${f.id}-app-dark`);await win.evaluate(()=>document.documentElement.classList.remove('dark'));
    const choice=frame.locator('[data-reading-tool] button').last();if(await choice.count()){await choice.click();assert.equal(await choice.getAttribute('aria-pressed'),'true');}
    const calc=frame.locator('[data-reading-tool]:has(input)').first();if(await calc.count()){const inputs=calc.locator('input');for(let j=0;j<await inputs.count();j++)await inputs.nth(j).fill(j===0?'200':j===1?'90':'1');assert(!/填写|__name|未知|未定义/.test(await calc.locator('output').innerText()));}
    await win.getByRole('button',{name:'参考资料',exact:true}).click();await shot(`${f.id}-app-references`);await win.getByRole('button',{name:'参考资料',exact:true}).click();
    await win.getByRole('button',{name:'原文',exact:true}).click();assert.equal(await win.locator('iframe[aria-label="AI 重构阅读"]').count(),0);await shot(`${f.id}-app-original`);
    const original=win.locator('[data-testid="reconstruction-reader"] .h-full.overflow-y-auto');
    await original.evaluate(el=>el.scrollTop=300);const originalTop=await original.evaluate(el=>el.scrollTop);
    await win.getByRole('button',{name:'目录',exact:true}).click();await win.getByRole('navigation',{name:'原文目录'}).waitFor();
    if(data.fixtureOnly)assert(await win.getByRole('navigation',{name:'原文目录'}).getByRole('button').count()>5);
    await win.getByRole('button',{name:'AI 阅读',exact:true}).click();await frame.locator('h1').waitFor();
    await win.getByRole('button',{name:'原文',exact:true}).click();await win.waitForTimeout(150);
    assert(Math.abs(await original.evaluate(el=>el.scrollTop)-originalTop)<2);
    assert.equal(await win.getByRole('button',{name:'目录',exact:true}).getAttribute('aria-expanded'),'true');
    await win.getByRole('button',{name:'目录',exact:true}).click();
    await win.getByRole('button',{name:f.page.sourceKind==='summary'?'编辑讨论总结':'编辑原文',exact:true}).click();await win.locator('.cm-content').waitFor();
    await win.locator('.cm-content').click();await win.keyboard.press('Control+End');await win.keyboard.insertText('\n\n本次编辑验收唯一文本');
    await win.getByRole('button',{name:'完成编辑',exact:true}).click();await win.getByRole('button',{name:'原文',exact:true}).waitFor();
    const saved=await win.evaluate(async id=>window.api.knowledge.get(id),items[index].id);
    assert(saved.content.includes('本次编辑验收唯一文本'));
    if(f.page.sourceKind==='summary'){assert(saved.content.includes('主楼必须保留'));assert(saved.content.includes('回复必须保留'));assert(saved.content.includes('> 平台：测试论坛'));}
    await shot(`${f.id}-app-edited`);
    await win.keyboard.press('Control+f');const search=win.locator('input[type="search"]').last();await search.fill('本次编辑验收唯一文本');await original.locator('mark').first().waitFor();await search.press('Escape');
    await win.getByRole('button',{name:'AI 阅读',exact:true}).click();await frame.locator('h1').waitFor();
    await win.setViewportSize({width:1120,height:1000});await win.waitForTimeout(250);
    const state=await frame.locator('body').evaluate(()=>({width:innerWidth,scrollWidth:document.documentElement.scrollWidth,scrollY}));assert(state.scrollWidth<=state.width+2);assert.equal(state.scrollY,0);await shot(`${f.id}-app-narrow`);
    report.push({id:f.id,sourceKind:f.page.sourceKind,...state,edited:true,tools:true});
  }
  if(data.fixtureOnly){
    const snapshotItem=await win.evaluate(async()=>window.api.knowledge.create({title:'网页快照入口验收',itemType:'note',content:'原始 Markdown 正文'}));
    // 普通 create 不写采集来源表；此隔离用例通过读取响应注入来源元数据。
    await app.evaluate(({ipcMain},item)=>{ipcMain.removeHandler('knowledge:get');ipcMain.handle('knowledge:get',()=>({...item,sourceUri:'https://mp.weixin.qq.com/s/isolated-test'}));},snapshotItem);
    await win.reload();await win.getByTestId('item-list').getByText('网页快照入口验收',{exact:true}).click();
    await win.getByRole('button',{name:'网页快照',exact:true}).click();await win.getByTestId('web-snapshot-pane').waitFor();await shot('web-snapshot-entry');
    await win.getByRole('button',{name:'返回阅读',exact:true}).click();await win.getByTestId('reconstruction-reader').waitFor();await shot('web-snapshot-return');
  }
  await fs.writeFile(path.join(outDir,'evidence.json'),JSON.stringify({success:true,fixtureOnly:!!data.fixtureOnly,isolatedApp:true,webVerified:false,cases:report},null,2));
};
