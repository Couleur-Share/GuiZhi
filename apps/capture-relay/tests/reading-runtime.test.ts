import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

test('独立阅读进程健康检查、HTTP 鉴权、SQLite 重启持久性', { timeout: 30000 }, async t => {
  const root = mkdtempSync(join(tmpdir(), 'reading-runtime-')), file = join(root, 'reading.db');
  const bootstrap = randomBytes(32).toString('base64url'), writer = randomBytes(32).toString('base64url');
  const libraryId = 'runtime_library_01', generation = 'runtime_generation_01';
  const start = async () => {
    const child = spawn(process.execPath, ['--import', 'tsx', 'src/reading-main.ts'], {
      cwd: fileURLToPath(new URL('../', import.meta.url)), stdio: ['ignore', 'pipe', 'pipe'],
      env: { PATH: process.env.PATH, SystemRoot: process.env.SystemRoot, READING_PORT: '0', READING_DATABASE: file, READING_BOOTSTRAP_CREDENTIAL: bootstrap },
    });
    child.stderr.on('data', () => {});
    const closed = new Promise<void>(resolve => child.once('exit', () => resolve()));
    const stop = async () => { if (child.exitCode === null) child.kill(); await closed; };
    t.after(stop);
    const port = await new Promise<number>((resolve, reject) => {
      const timer = setTimeout(() => { void stop(); reject(new Error('阅读进程启动超时')); }, 10000);
      let output = '';
      child.once('error', error => { clearTimeout(timer); reject(error); });
      child.once('exit', () => { clearTimeout(timer); reject(new Error('阅读进程在就绪前退出')); });
      child.stdout.on('data', data => {
        output += data.toString();
        if (!output.includes('\n')) return;
        try { const value = JSON.parse(output.trim()); if (value.service === 'guizhi-reading' && value.port) { clearTimeout(timer); resolve(value.port); } }
        catch { clearTimeout(timer); reject(new Error('阅读进程就绪信息无效')); }
      });
    });
    return { origin: `http://127.0.0.1:${port}`, stop };
  };
  let server = await start();
  try {
    assert.equal((await (await fetch(server.origin + '/healthz')).json() as { ok: boolean }).ok, true);
    const created = await fetch(server.origin + '/v1/reading/libraries', { method: 'POST',
      headers: { 'content-type': 'application/json', 'x-guizhi-reading': '1', authorization: `Bearer ${bootstrap}` },
      body: JSON.stringify({ libraryId, generation, writerCredential: writer }) });
    assert.equal(created.status, 200);
    await server.stop(); server = await start();
    const response = await fetch(`${server.origin}/v1/reading/${libraryId}/head`, { headers: { 'x-guizhi-reading': '1', authorization: `Bearer ${writer}` } });
    assert.equal(response.status, 200); assert.equal((await response.json() as { generation: string }).generation, generation);
  } finally { await server.stop(); rmSync(root, { recursive: true, force: true }); }
});
