import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { sharedClient } from './shared-client.mjs';
import { snapshotSource, fingerprintSource } from './snapshot.mjs';
import assert from 'node:assert/strict';
import { overallSeconds } from './deadline.mjs';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
try {
  if (process.argv.includes('--local')) {
    throw Error("WSLC retired on 2026-10-27; --local is disabled. Use check:linux:prepare and verify the Germany gs-preflight receipt; preparing alone is not a passed gate.");
  } else {
    const config = JSON.parse(fs.readFileSync(path.join(root, 'scripts/wslc/config.json'), 'utf8'));
    config.remote.timeoutSeconds = overallSeconds();
    sharedClient().main({
      root, config,
      capture: (input, sha) => {
        if (sha) throw Error('GuiZhi gate checks current working source, not --sha');
        return input ? snapshotSource(root, input, config.scopes) : fingerprintSource(root, config.scopes);
      },
      validate: (_result, _source, _profile, output) => {
        const smoke = JSON.parse(fs.readFileSync(path.join(output, 'capture-smoke.json'), 'utf8'));
        for (const key of ['health', 'web', 'sqlitePersistenceAfterRestart']) assert.equal(smoke[key], true);
        assert.equal(smoke.databaseScope, 'temporary-container-only');
        for (const file of ['validation.log', 'build-artifacts.tar.gz']) assert.ok(fs.statSync(path.join(output, file)).isFile());
      },
    });
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
