#!/usr/bin/env node
/**
 * Recalculate SHA512 and size in update manifests from the actual binary files.
 *
 * electron-builder on macOS occasionally generates manifests whose hashes do
 * not match the final DMG/ZIP artifacts (a known timing issue during
 * DMG-creation / code-signing). This script patches the manifests so the
 * published updater metadata is always consistent with the real files that
 * users will download.
 *
 * Usage:
 *   node scripts/fix-manifest-hashes.js <manifest1.yml> [manifest2.yml ...]
 *
 * The script looks for referenced assets in the same directory as each manifest.
 */

const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

const manifestPaths = process.argv.slice(2);
if (manifestPaths.length === 0) {
  console.error("Usage: node scripts/fix-manifest-hashes.js <manifest...>");
  process.exit(1);
}

function sha512Base64(filePath) {
  const hash = crypto.createHash("sha512");
  hash.update(fs.readFileSync(filePath));
  return hash.digest("base64");
}

let hasError = false;

for (const manifestPath of manifestPaths) {
  if (!fs.existsSync(manifestPath)) {
    console.error(`Manifest not found: ${manifestPath}`);
    hasError = true;
    continue;
  }

  const manifestDir = path.dirname(manifestPath);
  let content = fs.readFileSync(manifestPath, "utf8");
  let patched = false;

  // Match each file entry block:
  //   - url: <filename>
  //     sha512: <hash>
  //     size: <bytes>
  //
  // Groups: 1=url, 2=old sha512 hash, 3=old size
  const entryRegex =
    /(- url:\s*)(.+)((?:\r?\n)\s*sha512:\s*)(.+)((?:\r?\n)\s*size:\s*)(\d+)/g;

  content = content.replace(
    entryRegex,
    (_match, urlPrefix, url, shaBlock, oldSha, sizeBlock, oldSize) => {
      const fileName = url.trim();
      const assetPath = path.join(manifestDir, fileName);

      if (!fs.existsSync(assetPath)) {
        console.error(`Missing asset for ${manifestPath}: ${fileName}`);
        hasError = true;
        return _match; // leave unchanged
      }

      const stats = fs.statSync(assetPath);
      const actualSha = sha512Base64(assetPath);
      const actualSize = stats.size;

      if (actualSha !== oldSha.trim() || actualSize !== Number(oldSize)) {
        console.log(
          `Patched ${fileName}: sha512 ${oldSha.trim() !== actualSha ? "CHANGED" : "ok"}, size ${Number(oldSize) !== actualSize ? `${oldSize}->${actualSize}` : "ok"}`,
        );
        patched = true;
      }

      // shaBlock already contains "\n    sha512: ", sizeBlock contains "\n    size: "
      return `${urlPrefix}${url}${shaBlock}${actualSha}${sizeBlock}${actualSize}`;
    },
  );

  // Also fix the top-level sha512 field (path/sha512 pair for the primary file).
  const topPathMatch = content.match(/^path:\s*(.+)$/m);
  if (topPathMatch) {
    const primaryFile = topPathMatch[1].trim();
    const primaryPath = path.join(manifestDir, primaryFile);

    if (fs.existsSync(primaryPath)) {
      const primarySha = sha512Base64(primaryPath);
      content = content.replace(/^(sha512:\s*)(.+)$/m, (_m, prefix, oldSha) => {
        if (oldSha.trim() !== primarySha) {
          console.log(`Patched top-level sha512 for ${primaryFile}`);
          patched = true;
        }
        return `${prefix}${primarySha}`;
      });
    }
  }

  if (patched) {
    fs.writeFileSync(manifestPath, content, "utf8");
    console.log(`Updated: ${manifestPath}`);
  } else {
    console.log(`No changes needed: ${manifestPath}`);
  }
}

if (hasError) {
  process.exit(1);
}
