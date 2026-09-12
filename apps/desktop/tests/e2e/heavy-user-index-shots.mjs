import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
export default async ({ win, shot, outDir, userDataDir }) => {
  assert.ok(path.basename(userDataDir).startsWith('guizhi-shot-'));
  const item = await win.evaluate(async () => {
    const settings = { autoSave:false, wikiCompileEnabled:false, language:'zh' };
    localStorage.setItem('guizhi-settings',JSON.stringify({state:settings,version:0})); await window.api.settings.set(settings);
    localStorage.setItem('guizhi-setup-dismissed','1'); localStorage.setItem('guizhi-migration-dismissed','1');
    return window.api.knowledge.create({title:'后台索引响应验收',content:'用于检查输入和取消的独立合成资料'});
  });
  await win.evaluate(async id => {
    const pending = await window.api.semantic.listPending({model:'fixture',limit:50});
    const source = pending.find(value => value.id === id); if(!source)throw new Error('合成来源未进入索引集合');
    const ok = await window.api.semantic.applyEmbeddings({itemId:id,contentHash:source.contentHash,model:'fixture',dims:64,
      chunks:Array.from({length:50000},(_,n)=>({text:`向量分块 ${n}`,vector:new Array(64).fill(1/8)}))});
    if(!ok)throw new Error('合成向量落库失败');
  },item.id);
  await win.reload(); await win.setViewportSize({width:1440,height:1000});
  await win.getByTestId('item-list').getByText(item.title,{exact:true}).click();
  const title=win.getByTestId('item-title-input'); await title.waitFor();
  const cancellation = await win.evaluate(async () => {
    const requestId='index-cancel-fixture'; const result=window.api.semantic.search({model:'fixture',vector:new Array(64).fill(1/8),requestId}).then(()=>({cancelled:false}),error=>({cancelled:/取消|abort/i.test(String(error))}));
    await new Promise(resolve=>setTimeout(resolve,10)); const start=performance.now(); window.api.ai.cancel(requestId);
    return {...await result,elapsed:performance.now()-start};
  });
  await win.evaluate(() => { globalThis.__indexResult=window.api.semantic.search({model:'fixture',vector:new Array(64).fill(1/8),requestId:'index-normal-fixture'}); });
  await win.evaluate(() => {
    globalThis.__inputFrameSamples=[];
    document.querySelector('[data-testid="item-title-input"]').addEventListener('input', event => {
      const start=event.timeStamp;
      requestAnimationFrame(()=>globalThis.__inputFrameSamples.push(performance.now()-start));
    });
  });
  const driverSamples=[];
  for(let n=0;n<20;n++){const start=performance.now();await title.fill(`后台索引中继续输入 ${n}`); driverSamples.push(performance.now()-start);}
  const result=await win.evaluate(async()=>({hits:await globalThis.__indexResult,status:await window.api.semantic.status('fixture')}));
  const inputSamples=await win.evaluate(()=>globalThis.__inputFrameSamples);
  const inputP95=[...inputSamples].sort((a,b)=>a-b)[18];
  await fs.writeFile(path.join(outDir,'index-runtime.json'),JSON.stringify({vectors:50000,dims:64,cancellation,inputP95,inputSamples,driverSamples,...result},null,2));
  await title.press('Control+s');
  await win.waitForFunction(async id=>(await window.api.knowledge.get(id)).title==='后台索引中继续输入 19',item.id);
  assert.equal(inputSamples.length,20);
  assert.ok(cancellation.cancelled,JSON.stringify(cancellation)); assert.ok(cancellation.elapsed<=100,JSON.stringify(cancellation));
  assert.equal(result.hits[0].itemId,item.id); assert.ok(inputP95<=100,`input P95 ${inputP95}`);
  assert.ok(['hnsw','exact'].includes(result.status.lastBackend));
  if(result.status.lastBackend==='exact')assert.ok(result.status.fallbackReason);
  await shot('13-input-during-50k-index');
};
