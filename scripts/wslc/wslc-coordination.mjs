// Coordination is shared across projects; a slow boot must not look like contention.
import fs from 'node:fs';
import path from 'node:path';
import {randomUUID} from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {processAlive} from './wslc-lifecycle.mjs';

export const LOCK_WAIT_MS = 180000;
export async function acquireSessionLock(profile, {
  now = () => performance.now(),
  sleep = ms => new Promise(resolve => setTimeout(resolve, ms)),
  alive = processAlive,
  report = message => console.error(message),
} = {}) {
  const file = path.join(profile.runtime, 'bootstrap.lock');
  const started = now();
  let nextReport = 0;
  for (;;) {
    let fd;
    try { fd = fs.openSync(file, 'wx'); }
    catch (error) { if (error.code !== 'EEXIST') throw error; }
    if (fd !== undefined) {
      const token = randomUUID();
      try { fs.writeFileSync(fd, JSON.stringify({pid: process.pid, token})); }
      finally { fs.closeSync(fd); }
      return () => {
        const owner = JSON.parse(fs.readFileSync(file, 'utf8'));
        if (owner.token !== token || owner.pid !== process.pid)
          throw Error('WSLC bootstrap lock ownership changed; lock retained');
        fs.unlinkSync(file);
      };
    }
    const stat = fs.lstatSync(file, {throwIfNoEntry: false});
    if (stat && (!stat.isFile() || stat.isSymbolicLink())) throw Error('Invalid WSLC bootstrap lock');
    let prior;
    try { prior = JSON.parse(fs.readFileSync(file, 'utf8')); }
    catch (error) { if (error.code !== 'ENOENT' && !(error instanceof SyntaxError)) throw error; }
    if (Number.isSafeInteger(prior?.pid) && prior.pid > 0 && !alive(prior.pid))
      throw Error('WSLC bootstrap owner exited while holding its lock; inspect ' + file + ' before retrying');
    const elapsed = now() - started;
    if (elapsed >= LOCK_WAIT_MS)
      throw Error('WSLC session bootstrap is busy after 180s; inspect ' + file + '; no fallback session was created');
    if (elapsed >= nextReport) {
      report('WSLC: waiting for shared session startup/shutdown (' + Math.floor(elapsed / 1000) + 's/180s, owner PID ' + (prior?.pid ?? 'pending') + ')');
      nextReport = elapsed + 15000;
    }
    await sleep(Math.min(250, LOCK_WAIT_MS - elapsed));
  }
}

// Called under bootstrap.lock, only after the session process has closed.
export function clearClosedHost(profile, pid = process.pid) {
  const file = path.join(profile.runtime, 'host.json');
  if (!fs.existsSync(file)) return;
  if (fs.lstatSync(file).isSymbolicLink()) throw Error('Invalid WSLC host record');
  const host = JSON.parse(fs.readFileSync(file, 'utf8'));
  if (host.pid === pid && host.session === profile.session) fs.unlinkSync(file);
}

export async function closeSession(child, profile, timeoutMs = 30000) {
  await new Promise((resolve, reject) => {
    const onClose = () => { clearTimeout(timer); resolve(); };
    const timer = setTimeout(() => {
      child.removeListener('close', onClose);
      reject(Error('WSLC session shutdown unconfirmed'));
    }, timeoutMs);
    child.once('close', onClose);
    // Register before asking to exit: even a synchronous close cannot be lost.
    child.stdin.end('exit\n');
  });
  // The old holder is still alive here. Remove its record before releasing the
  // lock so the next project cannot mistake orderly shutdown for an unhealthy VM.
  clearClosedHost(profile);
}

