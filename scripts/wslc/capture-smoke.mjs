import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { DatabaseSync } from 'node:sqlite';

const database = '/work/capture-smoke/capture.db';
fs.mkdirSync(path.dirname(database), { recursive: true });
const env = { ...process.env, CAPTURE_ORIGIN: 'https://capture.invalid', CAPTURE_DATABASE: database,
  CAPTURE_WEB_ROOT: path.resolve('apps/capture-web/dist'), PORT: '18787' };
async function start() {
  const child = spawn(process.execPath, ['apps/capture-relay/dist/main.js'], { env, stdio: 'inherit' });
  let exited = false;
  child.once('exit', () => { exited = true; });
  for (let attempt = 0; attempt < 40; attempt++) {
    if (exited) throw Error('Capture relay exited before becoming healthy.');
    try {
      const health = await fetch('http://127.0.0.1:18787/healthz', { signal: AbortSignal.timeout(1000) });
      if (health.ok && (await health.json()).ok === true) return child;
    } catch {}
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  child.kill('SIGTERM');
  throw Error('Capture health check timed out.');
}
async function stop(child) {
  if (child.exitCode !== null) return;
  const closed = once(child, 'exit');
  child.kill('SIGTERM');
  await Promise.race([closed, new Promise((_, reject) => setTimeout(() => {
    child.kill('SIGKILL'); reject(Error('Capture shutdown timed out.'));
  }, 5000).unref())]);
}
let child;
try {
  child = await start();
  const web = await fetch('http://127.0.0.1:18787/', { signal: AbortSignal.timeout(3000) });
  assert.equal(web.status, 200);
  assert.match(await web.text(), /\/assets\//);
  const db = new DatabaseSync(database);
  db.exec('CREATE TABLE wslc_persistence_check(value TEXT); INSERT INTO wslc_persistence_check VALUES (\'survives-restart\')');
  db.close();
  await stop(child);
  child = await start();
  const reopened = new DatabaseSync(database);
  assert.equal(reopened.prepare('SELECT value FROM wslc_persistence_check').get().value, 'survives-restart');
  reopened.close();
  fs.writeFileSync('/output/capture-smoke.json', JSON.stringify({
    health: true, web: true, sqlitePersistenceAfterRestart: true, databaseScope: 'temporary-container-only',
  }, null, 2) + '\n');
  console.log('Capture health, built web and SQLite persistence after restart passed.');
} finally {
  if (child) await stop(child);
}
