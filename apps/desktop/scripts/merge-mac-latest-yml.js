#!/usr/bin/env node
const fs = require('fs');

function getArg(flag) {
  const index = process.argv.indexOf(flag);
  if (index === -1 || index + 1 >= process.argv.length) {
    return null;
  }
  return process.argv[index + 1];
}

function readRequired(filePath, label) {
  if (!filePath) {
    console.error(`Missing required argument: ${label}`);
    process.exit(1);
  }
  if (!fs.existsSync(filePath)) {
    console.error(`File not found: ${filePath}`);
    process.exit(1);
  }
  return fs.readFileSync(filePath, 'utf8');
}

function extractEntries(content) {
  const entryRegex = /- url:\s*(.+)\r?\n\s*sha512:\s*(.+)(?:\r?\n\s*size:\s*(\d+))?/g;
  const entries = [];
  let match;

  while ((match = entryRegex.exec(content)) !== null) {
    entries.push({
      url: match[1].trim(),
      sha512: match[2].trim(),
      size: match[3] ? Number(match[3]) : undefined,
    });
  }

  return entries;
}

function replaceFilesBlock(content, entries) {
  const filesBlockRegex = /files:\r?\n(?:[ \t].*\r?\n)*/;
  if (!filesBlockRegex.test(content)) {
    console.error('Unable to locate files block in base manifest');
    process.exit(1);
  }

  const lines = ['files:'];
  for (const entry of entries) {
    lines.push(`  - url: ${entry.url}`);
    lines.push(`    sha512: ${entry.sha512}`);
    if (typeof entry.size === 'number') {
      lines.push(`    size: ${entry.size}`);
    }
  }

  return content.replace(filesBlockRegex, `${lines.join('\n')}\n`);
}

function sortEntries(entries) {
  const extensionRank = (url) => {
    if (url.endsWith('.zip')) return 0;
    if (url.endsWith('.dmg')) return 1;
    return 2;
  };
  const archRank = (url) => {
    if (url.includes('-x64.')) return 0;
    if (url.includes('-arm64.')) return 1;
    return 2;
  };

  return [...entries].sort((a, b) => {
    return extensionRank(a.url) - extensionRank(b.url) || archRank(a.url) - archRank(b.url) || a.url.localeCompare(b.url);
  });
}

function pickPrimaryEntry(entries) {
  const preferred = [
    entries.find((entry) => entry.url.endsWith('-x64.zip')),
    entries.find((entry) => entry.url.endsWith('-x64.dmg')),
    entries.find((entry) => entry.url.endsWith('-arm64.zip')),
    entries.find((entry) => entry.url.endsWith('-arm64.dmg')),
  ].find(Boolean);

  return preferred || entries[0];
}

const x64File = getArg('--x64');
const arm64File = getArg('--arm64');
const outputFile = getArg('--output');

const x64Content = readRequired(x64File, '--x64');
const arm64Content = readRequired(arm64File, '--arm64');

if (!outputFile) {
  console.error('Missing required argument: --output');
  process.exit(1);
}

const x64Entries = extractEntries(x64Content);
const arm64Entries = extractEntries(arm64Content);
if (x64Entries.length === 0 || arm64Entries.length === 0) {
  console.error('Both manifests must contain at least one file entry');
  process.exit(1);
}

const mergedEntries = [];
const seenUrls = new Set();
for (const entry of sortEntries([...x64Entries, ...arm64Entries])) {
  if (seenUrls.has(entry.url)) {
    continue;
  }
  seenUrls.add(entry.url);
  mergedEntries.push(entry);
}

const primaryEntry = pickPrimaryEntry(mergedEntries);
let mergedContent = replaceFilesBlock(x64Content, mergedEntries);
mergedContent = mergedContent.replace(/^path:.*$/m, `path: ${primaryEntry.url}`);
mergedContent = mergedContent.replace(/^sha512:.*$/m, `sha512: ${primaryEntry.sha512}`);

if (!mergedContent.endsWith('\n')) {
  mergedContent += '\n';
}

fs.writeFileSync(outputFile, mergedContent, 'utf8');
console.log(`Merged macOS manifests into ${outputFile}`);
