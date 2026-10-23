import {acquireSessionLock as lock, closeSession} from './wslc-coordination.mjs';
// One explicitly configured disk, shared by all four preflight launchers.
import fs from 'node:fs';
import path from 'node:path';
import {randomUUID} from 'node:crypto';
import {spawn} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {boundedRunner, inventory, processAlive} from './wslc-lifecycle.mjs';

const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const self = fileURLToPath(import.meta.url);
export function readProfile(env = process.env) {
  if (!env.LOCALAPPDATA) throw Error('LOCALAPPDATA is required for the WSLC profile');
  const base = path.resolve(env.LOCALAPPDATA, 'wslc');
  const profile = JSON.parse(fs.readFileSync(path.join(base, 'preflight.json'), 'utf8'));
  if (profile.version !== 1 || !/^[a-z][a-z0-9-]{2,63}$/.test(profile.session) || profile.session.startsWith('wslc-')) throw Error('Invalid fixed WSLC session profile');
  if (!path.isAbsolute(profile.storagePath)) throw Error('WSLC storage must be an absolute existing path');
  const storage = path.resolve(profile.storagePath);
  for (let item = storage; path.dirname(item) !== item; item = path.dirname(item)) {
    if (fs.lstatSync(item).isSymbolicLink()) throw Error('WSLC storage links are forbidden');
  }
  const disk = fs.lstatSync(path.join(storage, 'storage.vhdx'));
  if (!disk.isFile() || disk.isSymbolicLink()) throw Error('Existing WSLC storage.vhdx required; automatic new disks are forbidden');
  if (env.SOFTMEMO_WSLC_SESSION && env.SOFTMEMO_WSLC_SESSION.trim() !== profile.session) throw Error('SOFTMEMO_WSLC_SESSION conflicts with the fixed preflight profile');
  const runtime = path.join(base, 'preflight-runtime');
  if (fs.existsSync(runtime) && fs.lstatSync(runtime).isSymbolicLink()) throw Error('WSLC runtime links are forbidden');
  fs.mkdirSync(runtime, {recursive: true});
  return {...profile, storagePath: storage, runtime};
}

function readRegular(file) {
  if (!fs.existsSync(file)) return null;
  if (!fs.lstatSync(file).isFile() || fs.lstatSync(file).isSymbolicLink()) throw Error('Invalid WSLC runtime record');
  return JSON.parse(fs.readFileSync(file, 'utf8'));
}

export async function connectSession(wslc) {
  const profile = readProfile();
  const release = await lock(profile);
  const lease = path.join(profile.runtime, `lease-${process.pid}-${randomUUID()}.json`);
  let holder;
  try {
    fs.writeFileSync(lease, JSON.stringify({pid: process.pid, session: profile.session}), {flag: 'wx'});
    const run = boundedRunner(wslc, profile.session);
    let initial;
    let listed = inventory(args => (initial = run(args, 15000)));
    if (!listed.ok) {
      // Match only the stable symbolic error code, never translated prose.
      if (initial.error || initial.signal || !/\bWSLC_E_SESSION_NOT_FOUND\b/.test(initial.stderr ?? ''))
        throw Error('Fixed WSLC inventory is unavailable; no bootstrap or alternate session attempted');
      const host = readRegular(path.join(profile.runtime, 'host.json'));
      if (host && processAlive(host.pid)) throw Error('Fixed WSLC session is unhealthy; existing host retained, no alternate disk/session created');
      holder = spawn(process.execPath, [self, '--hold-wslc-session', wslc], {detached: true, stdio: 'ignore', windowsHide: true});
      await new Promise((resolve, reject) => { holder.once('spawn', resolve); holder.once('error', reject); });
      fs.writeFileSync(path.join(profile.runtime, 'host.json'), JSON.stringify({pid: holder.pid, session: profile.session}));
      holder.unref();
      for (let i = 0; i < 30; i++) {
        await delay(500);
        listed = inventory(args => run(args, 3000));
        if (listed.ok) break;
        if (!processAlive(holder.pid)) break;
      }
      if (!listed.ok) throw Error('Fixed WSLC session did not become ready; no alternate disk/session created');
    }
    return {session: profile.session, run, release: () => { if (fs.existsSync(lease)) fs.unlinkSync(lease); }};
  } catch (error) {
    if (fs.existsSync(lease)) fs.unlinkSync(lease);
    throw error;
  } finally { release(); }
}

// This hidden helper owns only the named session it creates. It exits after all
// launchers release their leases AND inventory confirms there are no containers.
// It never terminates someone else's session or removes an unowned container.
async function hold(wslc) {
  const profile = readProfile();
  const child = spawn(wslc, ['system', 'session', 'enter', profile.storagePath, '--name', profile.session], {
    stdio: ['pipe', 'ignore', 'ignore'], windowsHide: true, env: {...process.env, WSL_UTF8: '1'},
  });
  child.stdin.on('error', () => {});
  child.on('error', () => { process.exitCode = 1; });
  let closed = false;
  child.once('close', code => { closed = true; process.exitCode = code ?? 1; });
  const run = boundedRunner(wslc, profile.session);
  while (!closed) {
    await delay(5000);
    if (closed) break;
    let release;
    try {
      try { release = await lock(profile); }
      catch (error) { if (error.message.includes('bootstrap is busy')) continue; throw error; }
      let active = false;
      for (const name of fs.readdirSync(profile.runtime).filter(name => /^lease-\d+-[a-f0-9-]+\.json$/.test(name))) {
        const file = path.join(profile.runtime, name);
        const lease = readRegular(file);
        if (!Number.isSafeInteger(lease?.pid) || lease.pid <= 0 || processAlive(lease.pid)) active = true;
        else fs.unlinkSync(file);
      }
      if (!active) {
        const listed = inventory(run);
        if (listed.ok && listed.names.length === 0) {
          await closeSession(child, profile);
        }
      }
    } finally { release?.(); }
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === self && process.argv[2] === '--hold-wslc-session') {
  await hold(process.argv[3]);
}
