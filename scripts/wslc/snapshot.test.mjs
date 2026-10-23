import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { execFileSync } from 'node:child_process';
import { snapshotSource } from './snapshot.mjs';

function fixture(t) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'wslc-snapshot-'));
  const root = path.join(directory, 'repo');
  fs.mkdirSync(root);
  const git = args => execFileSync('git', args, { cwd: root, encoding: 'utf8', stdio: 'pipe' }).trim();
  git(['init', '-q']);
  git(['config', 'core.autocrlf', 'false']);
  git(['config', 'user.name', 'Fixture']);
  git(['config', 'user.email', 'fixture@example.invalid']);
  fs.mkdirSync(path.join(root, 'apps'));
  fs.writeFileSync(path.join(root, '.gitignore'), '*.log\nnode_modules/\n');
  fs.writeFileSync(path.join(root, 'apps', 'old.txt'), 'old\n');
  fs.writeFileSync(path.join(root, 'apps', 'deleted.txt'), 'delete\n');
  git(['add', '.']);
  git(['commit', '-qm', 'fixture']);
  t.after(() => {
    const relative = path.relative(os.tmpdir(), directory);
    if (relative.startsWith('..') || path.isAbsolute(relative) || !path.basename(directory).startsWith('wslc-snapshot-')) {
      throw Error('Refusing unsafe fixture cleanup.');
    }
    fs.rmSync(directory, { recursive: true, force: true });
  });
  return { root, directory, git };
}

test('snapshot captures working bytes/new/deleted files, excludes credentials/output, preserves Git index', t => {
  const { root, directory, git } = fixture(t);
  fs.writeFileSync(path.join(root, 'apps', 'old.txt'), 'staged\n');
  git(['add', 'apps/old.txt']);
  fs.writeFileSync(path.join(root, 'apps', 'old.txt'), 'working\n');
  fs.rmSync(path.join(root, 'apps', 'deleted.txt'));
  fs.writeFileSync(path.join(root, 'apps', '新增 空格.txt'), 'new\n');
  fs.writeFileSync(path.join(root, 'apps', '.env.production'), 'fixture-credential\n');
  fs.writeFileSync(path.join(root, 'apps', '.npmrc'), '//registry.invalid/:_authToken=fixture\n');
  fs.mkdirSync(path.join(root, 'apps', 'dist'));
  fs.writeFileSync(path.join(root, 'apps', 'dist', 'result.js'), 'output\n');
  const before = git(['status', '--porcelain']);
  const staged = git(['diff', '--cached']);
  const result = snapshotSource(root, path.join(directory, 'input'), ['apps']);
  assert.deepEqual(result.files.map(file => file.path), ['apps/old.txt', 'apps/新增 空格.txt']);
  assert.equal(fs.readFileSync(path.join(directory, 'input/source/apps/old.txt'), 'utf8'), 'working\n');
  assert.equal(result.dirty, true);
  assert.equal(git(['status', '--porcelain']), before);
  assert.equal(git(['diff', '--cached']), staged);
  assert.equal(git(['rev-parse', 'HEAD']), result.baseSHA);
  const second = snapshotSource(root, path.join(directory, 'second'), ['apps']);
  assert.equal(second.snapshotSHA256, result.snapshotSHA256);
  fs.writeFileSync(path.join(root, 'apps', 'old.txt'), 'changed\n');
  assert.notEqual(snapshotSource(root, path.join(directory, 'third'), ['apps']).snapshotSHA256, result.snapshotSHA256);
});

test('snapshot rejects links to files outside the source scope', t => {
  const { root, directory } = fixture(t);
  fs.writeFileSync(path.join(directory, 'outside.txt'), 'private fixture\n');
  try { fs.symlinkSync(path.join(directory, 'outside.txt'), path.join(root, 'apps', 'linked.txt')); }
  catch (error) { if (error.code === 'EPERM') { t.skip('Windows symlink privilege unavailable'); return; } throw error; }
  assert.throws(() => snapshotSource(root, path.join(directory, 'input'), ['apps']), /symlink/);
});
