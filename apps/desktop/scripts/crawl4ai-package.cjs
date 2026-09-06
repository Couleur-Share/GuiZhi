/* 打包签名可能改变 Chromium / Python 二进制，最后再生成分发文件校验表。 */
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { execFileSync } = require("node:child_process");
function digest(file) {
  return crypto
    .createHash("sha256")
    .update(fs.readFileSync(file))
    .digest("hex");
}
function workerHashes(root) {
  return Object.fromEntries(
    fs
      .readdirSync(root)
      .filter((name) => name.endsWith(".py"))
      .map((name) => [name, digest(path.join(root, name))]),
  );
}
function finalizeManifest(resources) {
  const root = path.join(resources, "crawl4ai"),
    file = path.join(root, "manifest.json");
  const manifest = JSON.parse(fs.readFileSync(file, "utf8"));
  const files = {};
  function walk(directory) {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const full = path.join(directory, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (fs.statSync(full).isFile() && full !== file)
        files[path.relative(root, full).split(path.sep).join("/")] =
          digest(full);
    }
  }
  walk(root);
  manifest.files = files;
  manifest.workerHashes = workerHashes(path.join(resources, "crawl4ai-worker"));
  fs.writeFileSync(file, JSON.stringify(manifest, null, 2));
}
async function afterSign(context) {
  const mac = context.electronPlatformName === "darwin";
  if (mac) return; // macOS 在 custom sign 内完成校验表，之后才交给 builder 公证。
  const appPath = path.join(
    context.appOutDir,
    `${context.packager.appInfo.productFilename}.app`,
  );
  const resources = mac
    ? path.join(appPath, "Contents/Resources")
    : path.join(context.appOutDir, "resources");
  finalizeManifest(resources);
}
async function signMac(options) {
  const builderRoot = path.dirname(
    require.resolve("app-builder-lib/package.json", {
      paths: [path.dirname(require.resolve("electron-builder/package.json"))],
    }),
  );
  const { sign } = require(
    path.join(builderRoot, "out/codeSign/macCodeSign.js"),
  );
  await sign(options);
  const appPath = options.app;
  finalizeManifest(path.join(appPath, "Contents/Resources"));
  {
    // 只重新封装外层应用签名，保留已签好的嵌套二进制，避免再次改变其哈希。
    const inspection = require("node:child_process").spawnSync(
      "codesign",
      ["-d", "--verbose=4", appPath],
      { encoding: "utf8" },
    );
    if (inspection.status !== 0) throw new Error("无法读取 macOS 签名信息");
    const details = inspection.stderr || "";
    const identity =
      /^Authority=(.+)$/m.exec(details)?.[1] ??
      (/Signature=adhoc/.test(details) ? "-" : null);
    if (!identity) throw new Error("无法确认 macOS 签名身份");
    execFileSync(
      "codesign",
      [
        "--force",
        "--sign",
        identity,
        "--preserve-metadata=identifier,entitlements,requirements,flags,runtime",
        ...(identity === "-" ? [] : ["--timestamp"]),
        appPath,
      ],
      { stdio: "inherit" },
    );
    execFileSync("codesign", ["--verify", "--deep", "--strict", appPath], {
      stdio: "inherit",
    });
  }
}
module.exports = { workerHashes, finalizeManifest, afterSign, signMac };
