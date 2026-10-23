import fs from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';

const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const git = (root, args) => execFileSync('git', args, { cwd: root, encoding: 'utf8' }).trim();
const excluded = /(^|\/)(node_modules|dist|build|out|coverage|\.next|\.git|generated|test-results|playwright-report|__pycache__)(\/|$)|(^|\/)(\.env[^/]*|\.npmrc|\.dev\.vars|[^/]*\.(p12|pfx|p8|pem|key|db|sqlite|sqlite3|log|tsbuildinfo))$/;

function sourceFiles(root, scopes) {
  return [...new Set(execFileSync('git', ['ls-files', '-z', '--cached', '--others', '--exclude-standard', '--', ...scopes],
    { cwd: root, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 }).split('\0').filter(Boolean))]
    .filter(file => !excluded.test(file))
    .sort((a, b) => Buffer.compare(Buffer.from(a), Buffer.from(b)));
}

function regularFile(root, file, scopes) {
  if (!scopes.some(scope => file === scope || file.startsWith(scope + '/')) ||
      file.split('/').some(part => part === '..' || !part) || path.isAbsolute(file)) {
    throw Error('Source path is outside the configured scopes: ' + file);
  }
  let current = root;
  for (const part of file.split('/')) {
    current = path.join(current, part);
    let stat;
    try { stat = fs.lstatSync(current); }
    catch (error) { if (error.code === 'ENOENT') return false; throw error; }
    if (stat.isSymbolicLink()) throw Error('Source symlink/junction is not allowed: ' + file);
  }
  return fs.statSync(current).isFile();
}

// Capture working bytes, including unstaged/new files and tracked deletions.
// No Git operation writes to the user's index, HEAD or working files.
function captureSource(root, input, scopes) {
  const baseSHA = git(root, ['rev-parse', 'HEAD']);
  const files = [];
  const digest = createHash('sha256');
  if (input) fs.mkdirSync(path.join(input, 'source'), { recursive: true });
  for (const file of sourceFiles(root, scopes)) {
    if (!regularFile(root, file, scopes)) continue;
    const bytes = fs.readFileSync(path.join(root, file));
    const sha256 = hash(bytes);
    if (input) {
      const destination = path.join(input, 'source', file);
      fs.mkdirSync(path.dirname(destination), { recursive: true });
      fs.writeFileSync(destination, bytes);
    }
    files.push({ path: file, sha256 });
    digest.update(file + '\0' + sha256 + '\n');
  }
  const metadata = {
    baseSHA, snapshotSHA256: digest.digest('hex'),
    dirty: Boolean(git(root, ['status', '--porcelain=v1', '--untracked-files=all', '--', ...scopes])),
    scopes, files,
  };
  const after = sourceFiles(root, scopes).filter(file => regularFile(root, file, scopes));
  if (git(root, ['rev-parse', 'HEAD']) !== baseSHA ||
      after.join('\0') !== files.map(file => file.path).join('\0') ||
      files.some(file => hash(fs.readFileSync(path.join(root, file.path))) !== file.sha256)) {
    throw Error('Source changed while copying; run the check again.');
  }
  if (input) fs.writeFileSync(path.join(input, 'snapshot.json'), JSON.stringify(metadata, null, 2) + '\n');
  return metadata;
}

export const snapshotSource = (root, input, scopes) => captureSource(root, input, scopes);
export const fingerprintSource = (root, scopes) => captureSource(root, undefined, scopes);
