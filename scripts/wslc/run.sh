#!/usr/bin/env bash
set -euo pipefail
exec > >(tee /output/validation.log) 2>&1
started=$(date +%s)
phase=environment
finish() {
  code=$?
  trap - EXIT
  CHECK_EXIT=$code CHECK_PHASE=$phase CHECK_STARTED=$started node --input-type=module <<'NODE'
import fs from 'node:fs';
const source = JSON.parse(fs.readFileSync('/input/snapshot.json', 'utf8'));
const exitCode = Number(process.env.CHECK_EXIT);
const result = {
  passed: exitCode === 0, exitCode, phase: process.env.CHECK_PHASE,
  baseSHA: source.baseSHA, snapshotSHA256: source.snapshotSHA256, dirty: source.dirty,
  node: process.version, platform: process.platform,
  seconds: Math.floor(Date.now() / 1000) - Number(process.env.CHECK_STARTED),
};
fs.writeFileSync('/output/result.json', JSON.stringify(result, null, 2) + '\n');
NODE
  exit "$code"
}
trap finish EXIT
node --input-type=module <<'NODE'
import fs from 'node:fs';
const config = JSON.parse(fs.readFileSync('/input/source/scripts/wslc/config.json', 'utf8'));
if (process.platform !== 'linux' || Number(process.versions.node.split('.')[0]) !== config.nodeMajor) {
  throw Error('Unexpected Linux/Node environment.');
}
NODE
mkdir -p /work/repo
cp -a /input/source/. /work/repo/
cd /work/repo
export CI=true NEXT_TELEMETRY_DISABLED=1 PNPM_HOME=/usr/local/bin
phase=package-manager
if [[ "${PREFLIGHT_PREPARED:-}" != "1" ]]; then npm install -g pnpm@11.1.0 --no-audit --no-fund; fi
if [[ "${PREFLIGHT_PREPARED:-}" != "1" ]]; then pnpm config set store-dir /cache/pnpm-store; fi
source scripts/wslc/checks.sh
phase=complete
