import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {EventEmitter} from 'node:events';
import test from 'node:test';
import {acquireSessionLock, clearClosedHost, closeSession, LOCK_WAIT_MS} from './wslc-coordination.mjs';
function fixture(t) {
  const runtime = fs.mkdtempSync(path.join(os.tmpdir(), 'wslc-coordination-'));
  t.after(() => {
    assert.equal(path.dirname(runtime), fs.realpathSync(os.tmpdir()));
    assert.match(path.basename(runtime), /^wslc-coordination-/);
    fs.rmSync(runtime, {recursive: true, force: true});
  });
  return {runtime, session:'test-session'};
}
test('a second project survives a 110-second cold start with visible bounded waiting', async t => {
  const p = fixture(t), file = path.join(p.runtime, 'bootstrap.lock');
  fs.writeFileSync(file, JSON.stringify({pid:process.pid}));
  let time = 0;
  const reports = [];
  const unlock = await acquireSessionLock(p, {now:()=>time, report:m=>reports.push(m), sleep:async ms=>{
    time += ms;
    if (time === 110000) fs.unlinkSync(file);
  }});
  assert.equal(time,110000);
  assert(reports.length >= 7);
  unlock();
  assert(!fs.existsSync(file));
});
test('live lock timeout never steals ownership or creates an alternate session', async t => {
  const p = fixture(t), file = path.join(p.runtime,'bootstrap.lock'), content = JSON.stringify({pid:process.pid});
  fs.writeFileSync(file,content);
  let time=0;
  await assert.rejects(acquireSessionLock(p,{now:()=>time,sleep:async ms=>{time+=ms;},report:()=>{}}),/busy after 180s/);
  assert.equal(time,LOCK_WAIT_MS);
  assert.equal(fs.readFileSync(file,'utf8'),content);
});
test('dead owners and malformed locks are retained for inspection', async t => {
  const p=fixture(t), file=path.join(p.runtime,'bootstrap.lock');
  fs.writeFileSync(file,JSON.stringify({pid:12345}));
  await assert.rejects(acquireSessionLock(p,{alive:()=>false}),/owner exited/);
  fs.writeFileSync(file,'partial');
  let time=0;
  await assert.rejects(acquireSessionLock(p,{now:()=>time,sleep:async ms=>{time+=ms;},report:()=>{}}),/busy after/);
  assert.equal(fs.readFileSync(file,'utf8'),'partial');
});
test('normal host shutdown clears only its record before a new project attaches', async t => {
  const p=fixture(t), file=path.join(p.runtime,'host.json');
  fs.writeFileSync(file,JSON.stringify({pid:process.pid,session:p.session}));
  const child=new EventEmitter();
  child.stdin={end:()=>child.emit('close',0)};
  await closeSession(child,p);
  assert(!fs.existsSync(file));
  fs.writeFileSync(file,JSON.stringify({pid:process.pid+1,session:p.session}));
  clearClosedHost(p);
  assert(fs.existsSync(file));
});
test('unconfirmed shutdown preserves host ownership', async t => {
  const p=fixture(t), file=path.join(p.runtime,'host.json');
  fs.writeFileSync(file,JSON.stringify({pid:process.pid,session:p.session}));
  const child=new EventEmitter();
  child.stdin={end:()=>{}};
  await assert.rejects(closeSession(child,p,5),/shutdown unconfirmed/);
  assert(fs.existsSync(file));
});
test('unlock cannot delete a replacement lock', async t => {
  const p=fixture(t), file=path.join(p.runtime,'bootstrap.lock');
  const unlock=await acquireSessionLock(p);
  fs.writeFileSync(file,JSON.stringify({pid:process.pid,token:'replacement'}));
  assert.throws(unlock,/ownership changed/);
  assert(fs.existsSync(file));
});

