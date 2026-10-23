import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';
export function sharedClient() {
  const file = process.env.GS_LINUX_PREFLIGHT_CONFIG || path.join(os.homedir(), '.config/gs-linux-preflight/client.json');
  const config = JSON.parse(fs.readFileSync(file, 'utf8'));
  return createRequire(import.meta.url)(path.join(config.directory, 'client.cjs'));
}
