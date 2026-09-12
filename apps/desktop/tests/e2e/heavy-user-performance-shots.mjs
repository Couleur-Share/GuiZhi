import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
export default async ({ win, app, shot, outDir, userDataDir, mainEntry }) => {
  win.on('console', message => { if (message.text().startsWith('PERF:')) console.log(message.text()); });
  // 性能夹具固定系统字体；远程字体下载不属于数据处理预算。
  process.env.PW_TEST_SCREENSHOT_NO_FONTS_READY = '1';
  assert.ok(path.basename(userDataDir).startsWith('guizhi-shot-'));
  await app.evaluate((_electron, input) => {
    const require = process.getBuiltinModule('module').createRequire(input.mainEntry);
    const { Database } = require('node-sqlite3-wasm');
    const db = new Database(input.file);
    try {
      db.exec('BEGIN');
      const insert = db.prepare('INSERT INTO knowledge_items(id,title,content,review_status,created_at,updated_at) VALUES(?,?,?,?,?,?)');
      const fts = db.prepare('INSERT INTO knowledge_fts(item_id,title,content,tags) VALUES(?,?,?,?)');
      for (let n=0;n<9999;n++) { insert.run([`perf-${n}`,`性能资料 ${n}`,`开头事实 ${n}\n\n中间事实\n\n尾部事实`,n%10?'clear':'needs_review',n,n]); fts.run([`perf-${n}`,`性 能 资 料 ${n}`,`开 头 事 实 ${n}`,'']); }
      insert.finalize(); fts.finalize(); db.exec('INSERT INTO knowledge_fts_map(item_id,fts_rowid) SELECT item_id,rowid FROM knowledge_fts'); db.exec('COMMIT');
    } finally { db.close(); }
  }, {mainEntry,file:path.join(userDataDir,'data/knowledge.db')});
  await win.evaluate(async () => {
    localStorage.setItem('guizhi-setup-dismissed','1'); localStorage.setItem('guizhi-migration-dismissed','1');
    const settings = { autoSave:false, wikiCompileEnabled:false, language:'zh', themeMode:'light', isDarkMode:false };
    localStorage.setItem('guizhi-settings',JSON.stringify({state:settings,version:0})); await window.api.settings.set(settings);
    localStorage.setItem('ui-storage',JSON.stringify({state:{libraryViewMode:'list',appModule:'library'},version:0}));
    await window.api.knowledge.create({title:'性能资料 9999',content:'最后一条合成资料'});
  });
  await win.reload(); await win.setViewportSize({width:1440,height:1000}); await win.getByTestId('item-table').waitFor();
  await win.addStyleTag({content:'* { font-family: Segoe UI, Microsoft YaHei, sans-serif !important; }'});
  const metrics = await win.evaluate(async () => {
    const samples = {list:[],inbox:[],wiki:[]}, cold = {};
    for (let n=0;n<21;n++) for (const [key,run] of Object.entries({list:()=>window.api.knowledge.list({scope:'all',limit:20}),inbox:()=>window.api.inbox.list(),wiki:()=>window.api.wiki.compiler({action:'preview',model:'fixture'})})) {
      const start=performance.now(), value=await run(), elapsed=performance.now()-start;
      if(n===0){cold[key]=elapsed;console.log(`PERF: cold ${key} ${elapsed}ms`);}else samples[key].push(elapsed);
      if(key==='list'&&value.total!==10000)throw new Error('列表数量不符');
      if(key==='wiki'&&!value.ok)throw new Error(value.error);
    }
    return {cold,samples};
  });
  await fs.writeFile(path.join(outDir,'performance-ipc.json'),JSON.stringify(metrics,null,2));
  await shot('11-10k-library-light');
  await win.getByRole('button',{name:'处理中心',exact:true}).click();
  await win.getByRole('button',{name:'刷新处理中心',exact:true}).waitFor();
  const renderSamples = await win.evaluate(async () => {
    const samples=[];
    for(let n=0;n<21;n++) {
      const button=document.querySelector('button[aria-label="刷新处理中心"]');
      while(button.disabled)await new Promise(resolve=>setTimeout(resolve,5));
      const start=performance.now(); button.click();
      await new Promise(resolve=>requestAnimationFrame(resolve));
      while(button.disabled)await new Promise(resolve=>setTimeout(resolve,5));
      await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
      if(n)samples.push(performance.now()-start);
    }return samples;
  });
  const p95=values=>[...values].sort((a,b)=>a-b)[Math.ceil(values.length*.95)-1];
  const report={date:new Date().toISOString(),cpu:os.cpus()[0].model,count:10000,...metrics,p95:Object.fromEntries(Object.entries(metrics.samples).map(([key,values])=>[key,p95(values)])),inboxRefreshRenderP95:p95(renderSamples),renderSamples};
  await fs.writeFile(path.join(outDir,'performance-ui.json'),JSON.stringify(report,null,2));
  // 大列表中持续动画会令截图器的禁用动画步骤反复失效；直接捕获当前静态帧。
  await shot('12-10k-processing-center-light', {animations:'allow'});
  assert.ok(report.p95.list<=500,JSON.stringify(report.p95)); assert.ok(report.p95.inbox<=500); assert.ok(report.inboxRefreshRenderP95<=500,JSON.stringify(report));
};
