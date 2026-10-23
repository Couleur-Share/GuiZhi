import {createLifecycle} from './wslc-lifecycle.mjs';
import {connectSession} from './wslc-session.mjs';
let connection, lifecycle, ownership, successMessage;
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { randomUUID } from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';
import { snapshotSource } from './snapshot.mjs';
import { overallSeconds } from './deadline.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const config = JSON.parse(fs.readFileSync(path.join(root, 'scripts/wslc/config.json'), 'utf8'));
let output, wslc, container;
let interrupted = false;
let timedOut = false;
try {
  if (process.platform !== 'win32') throw Error('This entry uses Windows WSLC; Linux CI must run the native project checks.');
  const limitSeconds = overallSeconds();
  const installed = path.join(process.env.ProgramFiles || 'C:\\Program Files', 'WSL', 'wslc.exe');
  wslc = fs.existsSync(installed) ? installed : 'wslc.exe';
  connection = await connectSession(wslc);
  lifecycle = createLifecycle({root, project: 'guizhi', evidenceDirectory: 'artifacts/wslc-linux', session: connection.session, run: connection.run});
  const recovered = lifecycle.recover();
  console.log('WSLC session: ' + connection.session + '; checked prior runs: ' + recovered.length);
  const env = { ...process.env, WSL_UTF8: '1' };
  const version = spawnSync(wslc, ['version'], { encoding: 'utf8', env , timeout: 15000, windowsHide: true});
  if (version.error || version.status !== 0) throw Error('WSLC is unavailable. Update WSL and reopen the terminal.');
  const id = new Date().toISOString().replace(/[:.]/g, '-') + '-' + randomUUID().slice(0, 8);
  const runRoot = path.join(root, ...config.evidenceDirectory.split('/'), id);
  const input = path.join(runRoot, 'input');
  output = path.join(runRoot, 'output');
  const cache = path.join(root, ...config.evidenceDirectory.split('/'), 'cache');
  fs.mkdirSync(output, { recursive: true });
  fs.mkdirSync(cache, { recursive: true });
  const source = snapshotSource(root, input, config.scopes);
  fs.writeFileSync(path.join(output, 'source.json'), JSON.stringify(source, null, 2) + '\n');
  console.log('Base SHA: ' + source.baseSHA);
  console.log('Working source snapshot: ' + source.snapshotSHA256 + (source.dirty ? ' (contains local changes)' : ''));
  console.log('Logs/results: ' + output);
  ownership = lifecycle.register();
  container = ownership.container;
  const args = ['run', '--rm', ...ownership.labels, '--name', container,
    '-v', input + ':/input:ro', '-v', output + ':/output', '-v', cache + ':/cache',
    config.image, 'bash', '/input/source/scripts/wslc/run.sh'];
  console.log('Overall limit: ' + limitSeconds + 's (CHECK_LINUX_TIMEOUT)');
  const exitCode = await new Promise((resolve, reject) => {
    const child = spawn(wslc, ['--session', connection.session, ...args], { stdio: 'inherit', env });
    const stop = () => {
      interrupted = true;
      connection.run(['stop', '-t', '5', container]);
      child.kill();
    };
    const deadline = setTimeout(() => {
      timedOut = true;
      stop();
    }, limitSeconds * 1000);
    process.once('SIGINT', stop);
    process.once('SIGTERM', stop);
    child.once('error', error => {
      clearTimeout(deadline);
      reject(error);
    });
    child.once('close', code => {
      clearTimeout(deadline);
      process.removeListener('SIGINT', stop);
      process.removeListener('SIGTERM', stop);
      resolve(code ?? 1);
    });
  });
  if (timedOut) throw Error('Linux check exceeded the overall limit of ' + limitSeconds + 's and was stopped.');
  if (interrupted || exitCode !== 0) throw Error(interrupted ? 'Linux check interrupted.' : 'Linux check failed; exit code ' + exitCode);
  const result = JSON.parse(fs.readFileSync(path.join(output, 'result.json'), 'utf8'));
  if (!result.passed || result.platform !== 'linux' ||
      Number(result.node.split('.')[0].slice(1)) !== config.nodeMajor ||
      result.snapshotSHA256 !== source.snapshotSHA256 || result.phase !== 'complete') {
    throw Error('Check result does not match the source/environment/completed stages.');
  }
  const inspect = connection.run(['image', 'inspect', config.image]);
  if (!inspect.error && inspect.status === 0) {
    const details = JSON.parse(inspect.stdout)[0];
    result.image = { reference: config.image, id: details.Id, digests: details.RepoDigests };
    fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify(result, null, 2) + '\n');
  }
  successMessage = ('Linux check passed: ' + result.node + ', ' + result.seconds + 's. Evidence: ' + output);
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  if (output) console.error('Evidence retained: ' + output);
  process.exitCode = 1;
} finally {
  try {
    if (ownership) {
      const cleanup = lifecycle.finish(ownership, output);
      if (!cleanup.confirmed) {
        console.error('Container cleanup unconfirmed (' + cleanup.state + '): ' + container);
        process.exitCode = 1;
      }
    }
  } catch (error) {
    console.error('Container cleanup failed: ' + error.message);
    process.exitCode = 1;
  } finally { connection?.release(); }
  if (successMessage && !process.exitCode) console.log(successMessage);
}
