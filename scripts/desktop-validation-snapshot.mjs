import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";

const SNAPSHOT_PREFIX = "guizhi-validation-";
const SOURCE_PATHS = [
  "package.json",
  "pnpm-lock.yaml",
  "pnpm-workspace.yaml",
  "tsconfig*.json",
  "apps/desktop",
  "apps/*/package.json",
  "packages",
  "scripts",
  "config",
];

export function isInside(root, target) {
  const relative = path.relative(root, target);
  return (
    relative === "" ||
    (!path.isAbsolute(relative) &&
      relative !== ".." &&
      !relative.startsWith(`..${path.sep}`))
  );
}

export function isValidationSnapshot(desktopRoot) {
  const root = path.resolve(desktopRoot, "../..");
  try {
    const marker = JSON.parse(
      fs.readFileSync(path.join(root, ".validation-snapshot.json"), "utf8"),
    );
    return marker.root === root && marker.sourceRoot !== root;
  } catch {
    return false;
  }
}

// node_modules 自身必须是新目录；不能整目录链接，否则 Vite 缓存会写回开发目录。
// 只链接已安装的包，不执行 install/rebuild。工作区包始终指向源码副本。
function linkDependencies(sourceRoot, root, relativeDir) {
  const source = path.join(sourceRoot, relativeDir, "node_modules");
  if (!fs.existsSync(source)) return;
  const target = path.join(root, relativeDir, "node_modules");
  fs.mkdirSync(target, { recursive: true });
  const link = (from, to) => {
    const resolved = fs.realpathSync(from);
    const relative = path.relative(sourceRoot, resolved);
    const workspaceCopy = /^packages[/\\][^/\\]+$/.test(relative)
      ? path.join(root, relative)
      : resolved;
    fs.symlinkSync(
      workspaceCopy,
      to,
      process.platform === "win32" ? "junction" : "dir",
    );
  };
  for (const entry of fs.readdirSync(source, { withFileTypes: true })) {
    // 包管理器可改写 .pnpm，不能链接共享；仅复用单个包的只读目录。
    // .bin、.vite、.vite-temp 同样独立；通过包内 CLI 的绝对路径启动工具。
    if (entry.name.startsWith(".")) continue;
    const from = path.join(source, entry.name);
    const to = path.join(target, entry.name);
    if (entry.name.startsWith("@")) {
      fs.mkdirSync(to, { recursive: true });
      for (const name of fs.readdirSync(from))
        link(path.join(from, name), path.join(to, name));
    } else if (fs.statSync(from).isDirectory()) {
      link(from, to);
    }
  }
}

export function createDesktopSnapshot(sourceRoot, { reuseDependencies = true } = {}) {
  sourceRoot = fs.realpathSync(sourceRoot);
  // macOS 的临时目录可能含 /var -> /private/var 链接，统一实路径以匹配模块路径。
  const tempRoot = fs.realpathSync(os.tmpdir());
  const root = fs.mkdtempSync(path.join(tempRoot, SNAPSHOT_PREFIX));
  const cleanup = () => {
    // 只移除本次 mkdtemp 的直接子目录；rm 不跟随包目录的 junction/symlink。
    if (
      path.dirname(root) !== tempRoot ||
      !path.basename(root).startsWith(SNAPSHOT_PREFIX) ||
      isInside(sourceRoot, root)
    ) {
      throw new Error(`拒绝清理非隔离目录：${root}`);
    }
    fs.rmSync(root, {
      recursive: true,
      force: true,
      maxRetries: 5,
      retryDelay: 200,
    });
  };
  try {
    if (isInside(sourceRoot, root))
      throw new Error("临时目录位于开发工作区内，无法隔离文件监听");
    // 读取当前文件内容：包含暂存、未暂存及未忽略的新源码，绝不 checkout/reset/stash。
    const listed = execFileSync(
      "git",
      [
        "ls-files",
        "-z",
        "--cached",
        "--others",
        "--exclude-standard",
        "--",
        ...SOURCE_PATHS,
      ],
      { cwd: sourceRoot, maxBuffer: 16 * 1024 * 1024 },
    );
    const files = [
      ...new Set(listed.toString("utf8").split("\0").filter(Boolean)),
    ];
    let copied = 0;
    for (const relative of files) {
      if (
        relative
          .split(/[\\/]/)
          .some((part) => part === "node_modules" || part.startsWith(".env"))
      )
        continue;
      const from = path.resolve(sourceRoot, relative);
      const to = path.resolve(root, relative);
      if (!isInside(sourceRoot, from) || !isInside(root, to))
        throw new Error(`非法源码路径：${relative}`);
      if (!fs.existsSync(from)) continue; // 已在工作区删除的 tracked 文件不应复活。
      if (
        fs.lstatSync(from).isSymbolicLink() ||
        !isInside(sourceRoot, fs.realpathSync(from))
      ) {
        throw new Error(`源码链接不能用于隔离快照：${relative}`);
      }
      if (!fs.statSync(from).isFile()) continue;
      fs.mkdirSync(path.dirname(to), { recursive: true });
      fs.copyFileSync(from, to); // 独立文件，不能用硬链接。
      copied += 1;
    }
    for (const relativeDir of [
      "",
      "apps/desktop",
      "packages/core",
      "packages/db",
      "packages/shared",
    ]) {
      if (reuseDependencies) linkDependencies(sourceRoot, root, relativeDir);
    }
    fs.writeFileSync(
      path.join(root, ".validation-snapshot.json"),
      JSON.stringify({ root, sourceRoot, copied }, null, 2),
    );
    return {
      root,
      desktopRoot: path.join(root, "apps/desktop"),
      copied,
      cleanup,
    };
  } catch (error) {
    cleanup();
    throw error;
  }
}
