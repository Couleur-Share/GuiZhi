import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { buildReadingLibraries } from '../../apps/desktop/scripts/reading-libraries-build.mjs';

function fixture(t, source) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'guizhi-reading-build-'));
  t.after(() => {
    assert(path.resolve(root).startsWith(path.resolve(os.tmpdir()) + path.sep));
    fs.rmSync(root, { recursive: true, force: true });
  });
  const worker = path.join(root, 'worker.mjs');
  fs.writeFileSync(worker, source);
  return worker;
}

test('产物返回后等待编译子进程退出，不保留后台大堆', async t => {
  const worker = fixture(t, `process.send({ok:true,value:{compiler:'真实 IPC 文本',pid:process.pid}},()=>process.exit(0));`);
  const result = await buildReadingLibraries(worker);
  assert.equal(result.compiler, '真实 IPC 文本');
  assert.throws(() => process.kill(result.pid, 0), { code: 'ESRCH' });
});

test('编译错误保留原因，不把退出当作成功', async t => {
  const worker = fixture(t, `process.send({ok:false,error:'资源不可读'},()=>process.exit(1));`);
  await assert.rejects(buildReadingLibraries(worker), /资源不可读/);
});

test('未返回产物就退出时拒绝继续构建', async t => {
  const worker = fixture(t, `process.stderr.write('中断诊断');process.exit(0);`);
  await assert.rejects(buildReadingLibraries(worker), /中断诊断/);
});

test('超时结束本次编译进程，不能挂住 Vite', async t => {
  const worker = fixture(t, `import fs from 'node:fs';fs.writeFileSync(new URL('./pid',import.meta.url),String(process.pid));setInterval(()=>{},100);`);
  await assert.rejects(buildReadingLibraries(worker, 1500), /已停止子进程/);
  const pid = Number(fs.readFileSync(path.join(path.dirname(worker), 'pid'), 'utf8'));
  assert.throws(() => process.kill(pid, 0), { code: 'ESRCH' });
});
