import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';
import {DEFAULT_OVERALL_SECONDS, MAX_OVERALL_SECONDS, overallSeconds} from './deadline.mjs';
const here = path.dirname(fileURLToPath(import.meta.url));
test('overall limit defaults to 900 seconds when the variable is unset or empty', () => {
  assert.equal(DEFAULT_OVERALL_SECONDS, 900);
  assert.equal(overallSeconds({}), 900);
  assert.equal(overallSeconds({CHECK_LINUX_TIMEOUT: ''}), 900);
});
test('overall limit accepts integers from 1 to 86400 only', () => {
  assert.equal(overallSeconds({CHECK_LINUX_TIMEOUT: '1'}), 1);
  assert.equal(overallSeconds({CHECK_LINUX_TIMEOUT: '1200'}), 1200);
  assert.equal(overallSeconds({CHECK_LINUX_TIMEOUT: String(MAX_OVERALL_SECONDS)}), MAX_OVERALL_SECONDS);
  for (const bad of ['0', '-1', '1.5', 'abc', ' 5', '5 ', '1e3', '0x10', String(MAX_OVERALL_SECONDS + 1)]) {
    assert.throws(() => overallSeconds({CHECK_LINUX_TIMEOUT: bad}), /CHECK_LINUX_TIMEOUT/, bad);
  }
});
test('the entry arms and clears the deadline and reports timeouts before generic failures', () => {
  const source = fs.readFileSync(path.join(here, 'check-linux-local.mjs'), 'utf8');
  assert.match(source, /import \{ overallSeconds \} from '\.\/deadline\.mjs'/);
  assert.match(source, /const limitSeconds = overallSeconds\(\);/);
  assert.match(source, /setTimeout\(\(\) => \{\s*timedOut = true;\s*stop\(\);\s*\}, limitSeconds \* 1000\)/);
  assert.equal(source.match(/clearTimeout\(deadline\)/g)?.length, 2, 'both error and close must clear the timer');
  assert(source.indexOf('if (timedOut) throw') < source.indexOf('if (interrupted || exitCode !== 0)'));
});

test('Germany entry keeps the configured deadline and Docker executes it', () => {
  const source = fs.readFileSync(path.join(here, 'check-linux.mjs'), 'utf8');
  const dockerfile = fs.readFileSync(path.join(here, 'Dockerfile.remote'), 'utf8');
  assert.match(source, /config.remote.timeoutSeconds = overallSeconds\(\)/);
  assert.match(dockerfile, /ARG CHECK_LINUX_TIMEOUT=900/);
  assert.match(dockerfile, /timeout --foreground "\$CHECK_LINUX_TIMEOUT"/);
});
