#!/usr/bin/env node
/**
 * 构建「React 参照」：对等工程的 React 侧采集要反复拉起同一份构建，不能每次都走 `pnpm shot` 的
 * 「复制 → 构建 → 截图 → 删除」。这里复用同一套隔离构建（scripts/isolated-go.mjs 的 buildInSnapshot），
 * 但把产物（GuiZhi.exe、Sqlitecore.dll、渲染层 dist）留在基准目录，并记下源码指纹；指纹没变就不重建。
 *
 * 用法：
 *   node apps/desktop-go/scripts/parity/build-react-ref.mjs [--force] [--out <目录>]
 *
 * 约束（AGENTS.md）：不读写开发目录的 dist/out，不碰用户实例与用户数据目录。
 */
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { createDesktopSnapshot } from "../../../../scripts/desktop-validation-snapshot.mjs";
import { buildInSnapshot, GO_SOURCE_PATHS, goValidationEnv } from "../../../../scripts/isolated-go.mjs";
import { REPO_ROOT, reactRefDir } from "./lib/paths.mjs";

/** 影响 React 参照行为的源码范围（相对仓库根）；不含对等工具自身、Viva 与原生界面的代码，改它们不该触发重建。 */
const FINGERPRINT_INCLUDE = [
  "package.json", "pnpm-lock.yaml",
  "apps/desktop/", "packages/",
  "apps/desktop-go/go.mod", "apps/desktop-go/go.sum",
  "apps/desktop-go/shim/", "apps/desktop-go/jsrc/",
  "apps/desktop-go/vite.preload.config.ts", "apps/desktop-go/vite.renderer.config.ts",
];
const FINGERPRINT_EXCLUDE = [/[\\/]node_modules[\\/]/, /[\\/]tests?[\\/]/, /_test\.go$/, /\.test\.(ts|tsx|mjs)$/, /[\\/]dist[\\/]/, /[\\/]out[\\/]/];

function listFiles(root, rel, out) {
  const abs = path.join(root, rel);
  if (!fs.existsSync(abs)) return;
  const stat = fs.statSync(abs);
  if (stat.isFile()) {
    out.push(rel.replaceAll("\\", "/"));
  } else if (stat.isDirectory()) {
    for (const name of fs.readdirSync(abs)) listFiles(root, path.join(rel, name), out);
  }
}

/** 源码指纹：对纳入范围内的文件按路径排序后逐个哈希（路径 + 内容）。 */
export function sourceFingerprint(root) {
  const files = [];
  for (const rel of FINGERPRINT_INCLUDE) listFiles(root, rel, files);
  const kept = files.filter((f) => !FINGERPRINT_EXCLUDE.some((re) => re.test(f))).sort();
  const hash = crypto.createHash("sha256");
  for (const f of kept) {
    hash.update(f);
    hash.update("\0");
    hash.update(fs.readFileSync(path.join(root, f)));
    hash.update("\0");
  }
  return { sha256: hash.digest("hex"), files: kept.length };
}

export function readBuildRecord(dir = reactRefDir()) {
  try {
    return JSON.parse(fs.readFileSync(path.join(dir, "build.json"), "utf8"));
  } catch {
    return null;
  }
}

export async function buildReactRef({ force = false, outDir = reactRefDir() } = {}) {
  const current = sourceFingerprint(REPO_ROOT);
  const record = readBuildRecord(outDir);
  const exe = path.join(outDir, "bin", "GuiZhi.exe");
  if (!force && record?.fingerprint === current.sha256 && fs.existsSync(exe)) {
    console.log(`React 参照已是最新（指纹 ${current.sha256.slice(0, 12)}，${record.builtAt}）：${outDir}`);
    return { ...record, reused: true };
  }
  const env = goValidationEnv();
  const snapshot = createDesktopSnapshot(REPO_ROOT, { extraSourcePaths: GO_SOURCE_PATHS });
  console.log(`隔离验证：已复制 ${snapshot.copied} 个当前文件 → ${snapshot.root}`);
  try {
    // 指纹以副本为准：与实际参与构建的文件一致（包含未提交的改动）。
    const fingerprint = sourceFingerprint(snapshot.root);
    const built = await buildInSnapshot(snapshot, env);
    const binDir = path.join(outDir, "bin");
    const distDir = path.join(outDir, "dist");
    fs.rmSync(outDir, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
    fs.mkdirSync(binDir, { recursive: true });
    for (const name of fs.readdirSync(path.dirname(built.exe))) {
      fs.copyFileSync(path.join(path.dirname(built.exe), name), path.join(binDir, name));
    }
    fs.cpSync(built.dist, distDir, { recursive: true });
    const result = {
      fingerprint: fingerprint.sha256,
      fingerprintFiles: fingerprint.files,
      builtAt: new Date().toISOString(),
      exe: path.join(binDir, "GuiZhi.exe"),
      dist: distDir,
    };
    fs.writeFileSync(path.join(outDir, "build.json"), JSON.stringify(result, null, 2) + "\n");
    console.log(`React 参照构建完成：${outDir}（指纹 ${fingerprint.sha256.slice(0, 12)}，${fingerprint.files} 个文件）`);
    return { ...result, reused: false };
  } finally {
    snapshot.cleanup();
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  const argv = process.argv.slice(2);
  const outIdx = argv.indexOf("--out");
  try {
    await buildReactRef({ force: argv.includes("--force"), outDir: outIdx >= 0 ? path.resolve(argv[outIdx + 1]) : reactRefDir() });
  } catch (error) {
    console.error(error?.message ?? error);
    process.exitCode = 1;
  }
}
