import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
/** GPU 进程实际使用的渲染路径；特性状态在 SwiftShader 下同样报 enabled，只能看 GL 实现与渲染器。 */
async function renderingPath(app, gpu) {
  const { featureStatus, aux } = await app.evaluate(async ({ app }) => {
    const { auxAttributes = {} } = await app.getGPUInfo('complete');
    return { featureStatus: app.getGPUFeatureStatus(), aux: { displayType: auxAttributes.displayType, glImplementationParts: auxAttributes.glImplementationParts, glRenderer: auxAttributes.glRenderer } };
  });
  const software = !aux.glRenderer || /swiftshader|basic render driver|llvmpipe|softpipe/i.test(`${aux.displayType} ${aux.glRenderer}`);
  return { requested: gpu ? 'gpu' : 'swiftshader', hardware: !software && featureStatus.gpu_compositing === 'enabled', gpuCompositing: featureStatus.gpu_compositing, rasterization: featureStatus.rasterization, ...aux };
}
export default async ({ win, app, shot, outDir, userDataDir, gpu }) => {
  assert.ok(path.basename(userDataDir).startsWith('guizhi-shot-'));
  // 输入延迟门禁在 GPU 渲染下测量：pnpm shot 默认的 SwiftShader 在 Electron 45 上出帧变慢，P95 在阈值上下波动，
  // 不代表有显卡的用户。须加 --gpu，实际渲染路径写进证据；软件渲染下不测。
  const rendering = await renderingPath(app, gpu);
  assert.ok(rendering.hardware, `输入延迟门禁须在 GPU 渲染下测量（pnpm shot --gpu），本次渲染路径：${JSON.stringify(rendering)}`);
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
  await fs.writeFile(path.join(outDir,'index-runtime.json'),JSON.stringify({vectors:50000,dims:64,rendering,cancellation,inputP95,inputSamples,driverSamples,...result},null,2));
  await title.press('Control+s');
  await win.waitForFunction(async id=>(await window.api.knowledge.get(id)).title==='后台索引中继续输入 19',item.id);
  assert.equal(inputSamples.length,20);
  assert.ok(cancellation.cancelled,JSON.stringify(cancellation)); assert.ok(cancellation.elapsed<=100,JSON.stringify(cancellation));
  assert.equal(result.hits[0].itemId,item.id); assert.ok(inputP95<=100,`input P95 ${inputP95}`);
  assert.ok(result.status.lastScannedChunks>=50000,JSON.stringify(result.status));
  await shot('13-input-during-50k-index');
};
