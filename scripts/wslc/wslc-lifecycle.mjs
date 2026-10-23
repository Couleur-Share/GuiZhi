// Shared contract copied into each independent repository; no global pruning.
import fs from 'node:fs';
import path from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { spawnSync } from 'node:child_process';

const LIST = ['container', 'list', '--all', '--no-trunc', '--format', 'json'];
const LABEL = 'io.couleur.wslc.owner';
const RUN_LABEL = 'io.couleur.wslc.run';
const UUID = /^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/;
const completed = value => value && !value.error && !value.signal && value.status === 0;
const summary = value => ({status: value?.status ?? null, error: value?.error?.code ?? null, signal: value?.signal ?? null});

export function inventory(run) {
  const result = run(LIST);
  if (!completed(result)) return {ok: false, ...summary(result)};
  try {
    // WSLC 3.0.1 emits one JSON object per line, and no bytes for an empty list.
    const text = String(result.stdout ?? '').replace(/^\uFEFF/, '').trim();
    const rows = !text ? [] : text.startsWith('[') ? JSON.parse(text) : text.split(/\r?\n/).map(line => JSON.parse(line));
    if (!Array.isArray(rows)) throw Error('array required');
    const names = rows.flatMap(row => {
      const value = row.Names ?? row.Name;
      const names = Array.isArray(value) ? value : [value];
      if (!names.length || names.some(name => typeof name !== 'string' || !name)) throw Error('name required');
      return names.map(name => name.replace(/^\//, ''));
    });
    return {ok: true, names};
  } catch { return {ok: false, error: 'INVALID_INVENTORY'}; }
}

export function cleanupContainer(run, container) {
  const steps = [{action: 'stop', ...summary(run(['stop', '-t', '5', container]))}];
  let listed = inventory(run);
  steps.push({action: 'inventory', ok: listed.ok});
  if (listed.ok && !listed.names.includes(container)) return {confirmed: true, state: 'absent', steps};
  steps.push({action: 'remove', ...summary(run(['container', 'remove', '--force', container]))});
  listed = inventory(run);
  steps.push({action: 'inventory', ok: listed.ok});
  const confirmed = listed.ok && !listed.names.includes(container);
  return {confirmed, state: confirmed ? 'removed' : listed.ok ? 'present' : 'unknown', steps};
}

// EPERM and PID reuse are deliberately treated as live: uncertainty cannot authorize deletion.
export function processAlive(pid) {
  try { process.kill(pid, 0); return true; }
  catch (error) { return error.code !== 'ESRCH'; }
}

function regularPath(file, root) {
  const relative = path.relative(root, file);
  if (!relative || relative.startsWith('..') || path.isAbsolute(relative)) throw Error('WSLC ledger path escaped its root');
  for (let item = file; item !== root; item = path.dirname(item)) {
    if (fs.existsSync(item) && fs.lstatSync(item).isSymbolicLink()) throw Error('WSLC ledger links are forbidden');
  }
}

export function createLifecycle({root, project, evidenceDirectory, session, run, alive = processAlive}) {
  root = fs.realpathSync(root);
  if (!/^[a-z0-9-]+$/.test(project)) throw Error('Invalid WSLC project');
  const directory = path.resolve(root, evidenceDirectory, 'owners');
  regularPath(directory, root);
  fs.mkdirSync(directory, {recursive: true});
  const owner = createHash('sha256').update(root.toLowerCase() + '\n' + project).digest('hex');
  function write(record) {
    const file = path.join(directory, record.id + '.json');
    regularPath(file, root);
    const temporary = file + '.' + randomUUID() + '.tmp';
    fs.writeFileSync(temporary, JSON.stringify(record, null, 2) + '\n', {flag: 'wx'});
    fs.renameSync(temporary, file);
  }
  function recover() {
    const outcomes = [];
    for (const entry of fs.readdirSync(directory, {withFileTypes: true})) {
      if (!entry.name.endsWith('.json')) continue;
      const file = path.join(directory, entry.name);
      regularPath(file, root);
      if (!entry.isFile()) throw Error('Unexpected WSLC ownership record');
      const record = JSON.parse(fs.readFileSync(file, 'utf8'));
      if (record.version !== 1 || !UUID.test(record.id) || entry.name !== record.id + '.json' || record.owner !== owner ||
          record.project !== project || record.session !== session || record.container !== `wslc-${project}-${record.id}` ||
          !Number.isSafeInteger(record.pid) || record.pid <= 0) throw Error('Invalid WSLC ownership record; manual review required');
      if (record.cleanup?.confirmed) continue;
      if (alive(record.pid)) { outcomes.push({container: record.container, state: 'active-or-unknown'}); continue; }
      const listed = inventory(run);
      if (!listed.ok) throw Error('WSLC orphan inventory unavailable; refusing a new run');
      let cleanup;
      if (!listed.names.includes(record.container)) cleanup = {confirmed: true, state: 'absent', steps: []};
      else {
        const inspected = run(['container', 'inspect', record.container]);
        let labels;
        try { if (completed(inspected)) labels = JSON.parse(inspected.stdout)[0]?.Config?.Labels; } catch {}
        if (labels?.[LABEL] !== owner || labels?.[RUN_LABEL] !== record.id) throw Error('WSLC orphan ownership unconfirmed; no container removed');
        cleanup = cleanupContainer(run, record.container);
      }
      write({...record, cleanup, recoveredAt: new Date().toISOString()});
      outcomes.push({container: record.container, state: cleanup.state});
      if (!cleanup.confirmed) throw Error('WSLC orphan cleanup unconfirmed; refusing a new run');
    }
    return outcomes;
  }
  function register() {
    const id = randomUUID();
    const record = {version: 1, id, owner, project, session, container: `wslc-${project}-${id}`, pid: process.pid, createdAt: new Date().toISOString()};
    // Persist before creating a container, so abrupt host termination is recoverable next time.
    write(record);
    return {...record, labels: ['--label', `${LABEL}=${owner}`, '--label', `${RUN_LABEL}=${id}`]};
  }
  function finish(record, output) {
    const cleanup = cleanupContainer(run, record.container);
    const receipt = {container: record.container, session, ...cleanup, completedAt: new Date().toISOString()};
    fs.writeFileSync(path.join(output, 'cleanup.json'), JSON.stringify(receipt, null, 2) + '\n');
    const {labels, ...stored} = record;
    write({...stored, cleanup});
    return receipt;
  }
  return {recover, register, finish};
}

export function boundedRunner(wslc, session, env = process.env) {
  return (args, timeout = 15000) => {
    try { return spawnSync(wslc, ['--session', session, ...args], {encoding: 'utf8', env: {...env, WSL_UTF8: '1'}, timeout, windowsHide: true}); }
    catch (error) { return {status: null, error}; }
  };
}
