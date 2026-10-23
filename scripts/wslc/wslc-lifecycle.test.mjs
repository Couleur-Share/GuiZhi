import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {cleanupContainer, createLifecycle, inventory} from './wslc-lifecycle.mjs';
import {readProfile} from './wslc-session.mjs';

const ok = stdout => ({status: 0, stdout});
const failed = {status: null, error: {code: 'ETIMEDOUT'}};
const rows = (...names) => ok(names.map(Names => JSON.stringify({Names})).join('\n'));
function fixture(t, run, alive = () => false) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'wslc-lifecycle-'));
  t.after(() => {
    assert.equal(path.dirname(root), fs.realpathSync(os.tmpdir()));
    assert.match(path.basename(root), /^wslc-lifecycle-/);
    fs.rmSync(root, {recursive: true, force: true});
  });
  const lifecycle = createLifecycle({root, project: 'test', evidenceDirectory: 'evidence', session: 'couleur-preflight', run, alive});
  return {root, lifecycle, recordFile: record => path.join(root, 'evidence/owners', record.id + '.json')};
}

test('actual WSLC NDJSON, empty inventory, array format and exact names are supported', () => {
  assert.deepEqual(inventory(() => rows()).names, []);
  assert.deepEqual(inventory(() => rows('one', 'two')).names, ['one', 'two']);
  assert.deepEqual(inventory(() => ok('[{"Name":"/one"}]')).names, ['one']);
  assert.equal(inventory(() => ok('unparseable RPC error')).ok, false);
  assert.equal(inventory(() => ok('[{"Id":"missing name"}]')).ok, false);
  assert.equal(inventory(() => failed).ok, false);
});

test('timeout and remove success alone never confirm cleanup; retries are bounded', () => {
  const calls = [];
  const result = cleanupContainer(args => {calls.push(args); return args[0] === 'container' && args[1] === 'list' ? failed : ok('');}, 'owned');
  assert.equal(result.confirmed, false);
  assert.equal(result.state, 'unknown');
  assert.equal(calls.length, 4);
  assert.deepEqual(calls.filter(c => c[1] === 'remove'), [['container', 'remove', '--force', 'owned']]);
});

test('cleanup confirms absence, preserves similarly named and unrelated containers', () => {
  const result = cleanupContainer(args => args[1] === 'list' ? rows('owned-other', 'user-container') : ok(''), 'owned');
  assert.equal(result.confirmed, true);
  assert.equal(result.steps.length, 2);
});

test('a surviving container cannot be reported as cleaned', () => {
  assert.equal(cleanupContainer(args => args[1] === 'list' ? rows('owned') : ok(''), 'owned').confirmed, false);
});

test('register-before-create supports recovery after a crash before container creation', t => {
  const {lifecycle} = fixture(t, args => {assert.equal(args[1], 'list'); return rows();});
  const record = lifecycle.register();
  assert.match(record.container, /^wslc-test-/);
  assert.deepEqual(lifecycle.recover(), [{container: record.container, state: 'absent'}]);
  assert.deepEqual(lifecycle.recover(), []);
});

test('live launcher or uncertain process ownership is never touched', t => {
  const {lifecycle} = fixture(t, () => {throw Error('must not call WSLC');}, () => true);
  const record = lifecycle.register();
  assert.deepEqual(lifecycle.recover(), [{container: record.container, state: 'active-or-unknown'}]);
});

test('dead launcher recovery requires both exact owner labels; foreign container survives', t => {
  let record;
  const calls = [];
  const {lifecycle} = fixture(t, args => {
    calls.push(args);
    if (args[1] === 'list') return rows(record.container);
    if (args[1] === 'inspect') return ok('[{"Config":{"Labels":{}}}]');
    throw Error('foreign container must not be stopped');
  });
  record = lifecycle.register();
  assert.throws(() => lifecycle.recover(), /ownership unconfirmed/);
  assert.equal(calls.length, 2);
});

test('dead owned launcher is stopped, removed, verified and persisted for idempotent recovery', t => {
  let record, removed = false;
  const calls = [];
  const {lifecycle, recordFile} = fixture(t, args => {
    calls.push(args);
    if (args[1] === 'list') return rows(...(removed ? ['foreign'] : [record.container, 'foreign']));
    if (args[1] === 'inspect') return ok(JSON.stringify([{Config: {Labels: {'io.couleur.wslc.owner': record.owner, 'io.couleur.wslc.run': record.id}}}]));
    if (args[1] === 'remove') removed = true;
    return ok('');
  });
  record = lifecycle.register();
  assert.equal(lifecycle.recover()[0].state, 'removed');
  assert.equal(JSON.parse(fs.readFileSync(recordFile(record))).cleanup.confirmed, true);
  assert.deepEqual(lifecycle.recover(), []);
  assert.ok(calls.filter(c => c[0] === 'stop' || c[1] === 'remove').every(c => c.at(-1) === record.container));
});

test('inventory failures block orphan recovery without interpreting error text', t => {
  const {lifecycle} = fixture(t, () => failed);
  lifecycle.register();
  assert.throws(() => lifecycle.recover(), /inventory unavailable/);
});

test('tampered session, PID or container records fail closed before WSLC calls', t => {
  const {lifecycle, recordFile} = fixture(t, () => {throw Error('must not call WSLC');});
  const record = lifecycle.register();
  for (const patch of [{session: 'foreign'}, {pid: -1}, {container: 'user-container'}, {owner: 'foreign'}]) {
    fs.writeFileSync(recordFile(record), JSON.stringify({...record, ...patch}));
    assert.throws(() => lifecycle.recover(), /Invalid WSLC ownership/);
  }
});

test('ledger path outside the project is refused', t => {
  const {root} = fixture(t, () => rows());
  assert.throws(() => createLifecycle({root, project: 'test', session: 'fixed', evidenceDirectory: '../outside', run: () => rows()}), /escaped/);
});

test('finish writes independent cleanup proof; an unconfirmed result remains recoverable', t => {
  const {lifecycle, root, recordFile} = fixture(t, () => failed);
  const record = lifecycle.register();
  const output = path.join(root, 'output'); fs.mkdirSync(output);
  assert.equal(lifecycle.finish(record, output).confirmed, false);
  assert.equal(JSON.parse(fs.readFileSync(recordFile(record))).cleanup.confirmed, false);
  assert.equal(JSON.parse(fs.readFileSync(path.join(output, 'cleanup.json'))).state, 'unknown');
});

test('fixed profile requires existing disk and rejects reserved names and old session overrides', t => {
  const {root} = fixture(t, () => rows());
  fs.mkdirSync(path.join(root, 'wslc/disk'), {recursive: true});
  const storagePath = path.join(root, 'wslc/disk');
  const file = path.join(root, 'wslc/preflight.json');
  const profile = {version: 1, session: 'couleur-preflight', storagePath};
  fs.writeFileSync(file, JSON.stringify(profile));
  assert.throws(() => readProfile({LOCALAPPDATA: root}), /ENOENT/);
  fs.writeFileSync(path.join(storagePath, 'storage.vhdx'), 'fixture');
  assert.equal(readProfile({LOCALAPPDATA: root}).session, profile.session);
  assert.throws(() => readProfile({LOCALAPPDATA: root, SOFTMEMO_WSLC_SESSION: 'wslc-cli-admin-user'}), /conflicts/);
  fs.writeFileSync(file, JSON.stringify({...profile, session: 'wslc-cli-admin-user'}));
  assert.throws(() => readProfile({LOCALAPPDATA: root}), /Invalid fixed/);
});
