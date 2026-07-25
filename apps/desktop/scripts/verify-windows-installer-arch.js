#!/usr/bin/env node
const fs = require('fs');
const path = require('path');

const MACHINE_TYPES = {
  x64: 0x8664,
  arm64: 0xaa64,
};

function getArg(flag) {
  const index = process.argv.indexOf(flag);
  if (index === -1 || index + 1 >= process.argv.length) {
    return null;
  }
  return process.argv[index + 1];
}

function readMachineType(filePath) {
  const buffer = fs.readFileSync(filePath);
  if (buffer.length < 0x40) {
    throw new Error(`File too small to be a PE executable: ${filePath}`);
  }

  const peOffset = buffer.readUInt32LE(0x3c);
  if (peOffset + 6 > buffer.length) {
    throw new Error(`Invalid PE header offset in ${filePath}`);
  }

  const signature = buffer.toString('ascii', peOffset, peOffset + 4);
  if (signature !== 'PE\u0000\u0000') {
    throw new Error(`Missing PE signature in ${filePath}`);
  }

  return buffer.readUInt16LE(peOffset + 4);
}

const filePath = getArg('--file');
const expectedArch = getArg('--arch');

if (!filePath || !expectedArch) {
  console.error('Usage: node scripts/verify-windows-installer-arch.js --file <exe> --arch <x64|arm64>');
  process.exit(1);
}

if (!fs.existsSync(filePath)) {
  console.error(`Installer not found: ${filePath}`);
  process.exit(1);
}

const expectedMachine = MACHINE_TYPES[expectedArch];
if (!expectedMachine) {
  console.error(`Unsupported architecture: ${expectedArch}`);
  process.exit(1);
}

const actualMachine = readMachineType(path.resolve(filePath));
if (actualMachine !== expectedMachine) {
  console.error(
    `Windows installer architecture mismatch for ${filePath}: expected ${expectedArch} (0x${expectedMachine.toString(16)}), got 0x${actualMachine.toString(16)}`
  );
  process.exit(1);
}

console.log(`Verified Windows installer architecture: ${filePath} -> ${expectedArch}`);
