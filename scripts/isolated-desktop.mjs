import fs from "node:fs";
import path from "node:path";
import { spawn, execFileSync } from "node:child_process";
import { pathToFileURL } from "node:url";
import { createDesktopSnapshot } from "./desktop-validation-snapshot.mjs";

const REPO_ROOT = path.resolve(import.meta.dirname, "..");

export function validationEnv(env = process.env) {
  const result = {
    ...env,
    NODE_ENV: "production",
    GUIZHI_WINDOW_MODE: "offscreen",
  };
  // 绝不继承开发服务地址或 Electron 的 Node 模式。
  for (const key of [
    "VITE_DEV_SERVER_URL",
    "ELECTRON_RUN_AS_NODE",
    "GUIZHI_E2E_RENDERER_URL",
    "GUIZHI_E2E_USER_DATA_DIR",
  ])
    delete result[key];
  return result;
}

function runNode(cli, args, cwd, env, logFile) {
  return new Promise((resolve, reject) => {
    const fd = logFile ? fs.openSync(logFile, "a") : null;
    const child = spawn(process.execPath, [cli, ...args], {
      cwd,
      env,
      windowsHide: true,
      detached: process.platform !== "win32",
      stdio: fd === null ? "inherit" : ["ignore", fd, fd],
    });
    let interrupted = false;
    const stop = () => {
      interrupted = true;
      if (!child.pid) return;
      // 只终止本次创建的子进程树，禁止按 electron.exe/node.exe 名称批量结束。
      if (process.platform === "win32") {
        try {
          execFileSync("taskkill", ["/pid", String(child.pid), "/T", "/F"], {
            windowsHide: true,
            stdio: "ignore",
          });
        } catch {
          /* 子进程可能已退出。 */
        }
      } else {
        try {
          process.kill(-child.pid, "SIGTERM");
        } catch {
          /* 子进程可能已退出。 */
        }
      }
    };
    process.once("SIGINT", stop);
    process.once("SIGTERM", stop);
    const finish = () => {
      process.removeListener("SIGINT", stop);
      process.removeListener("SIGTERM", stop);
      if (fd !== null) fs.closeSync(fd);
    };
    child.once("error", (error) => {
      finish();
      reject(error);
    });
    child.once("exit", (code) => {
      finish();
      resolve(interrupted ? 130 : (code ?? 1));
    });
  });
}

function shotArgs(argv, cwd) {
  const pathOptions = new Set([
    "--steps",
    "--out",
    "--data-db",
    "--executable",
    "--renderer-root",
  ]);
  const result = [];
  for (let i = 0; i < argv.length; i += 1) {
    result.push(argv[i]);
    if (pathOptions.has(argv[i])) {
      const value = argv[++i];
      if (!value) throw new Error("截图路径参数缺少值");
      result.push(path.resolve(cwd, value));
    }
  }
  if (!argv.includes("--out"))
    result.push("--out", path.join(REPO_ROOT, "apps/desktop/.tmp-shots"));
  return result;
}

/** shot / e2e / build 共用同一份当前源码副本，构建缓存和 out 都在临时目录。 */
export async function runIsolatedDesktop(
  command,
  argv = [],
  cwd = process.cwd(),
) {
  if (!["shot", "e2e", "build"].includes(command))
    throw new Error(
      "用法：node scripts/isolated-desktop.mjs <shot|e2e|build> [...参数]",
    );
  if (command === "build" && argv.length)
    throw new Error("隔离 build 不接受额外参数");
  const args = command === "shot" ? shotArgs(argv, cwd) : argv;
  const snapshot = createDesktopSnapshot(REPO_ROOT);
  const env = validationEnv();
  const logFile = path.join(snapshot.root, "build.log");
  console.log(
    `隔离验证：已复制 ${snapshot.copied} 个当前文件 → ${snapshot.root}`,
  );
  let status = 1;
  try {
    const vite = path.join(
      snapshot.desktopRoot,
      "node_modules/vite/bin/vite.js",
    );
    for (const buildArgs of [
      ["build"],
      ["build", "--config", "vite.mcp.config.ts"],
    ]) {
      status = await runNode(
        vite,
        buildArgs,
        snapshot.desktopRoot,
        env,
        logFile,
      );
      if (status !== 0) {
        console.error(
          fs.readFileSync(logFile, "utf8").split(/\r?\n/).slice(-65).join("\n"),
        );
        return status;
      }
    }
    console.log("隔离构建完成；开发目录的 out/ 未被用于本次验证。");
    if (command === "shot") {
      // 步骤脚本保持原来的相对 fixture 路径；Electron 本身另设 cwd 到源码副本。
      status = await runNode(
        path.join(snapshot.desktopRoot, "scripts/screenshot.mjs"),
        args,
        cwd,
        env,
      );
    } else if (command === "e2e") {
      const reportDir = fs.mkdtempSync(
        path.join(REPO_ROOT, "apps/desktop/.tmp-e2e-"),
      );
      status = await runNode(
        path.join(snapshot.desktopRoot, "node_modules/@playwright/test/cli.js"),
        ["test", ...args, "--output", reportDir],
        snapshot.desktopRoot,
        env,
      );
      console.log(`E2E 测试产物：${reportDir}`);
    }
    return status;
  } finally {
    snapshot.cleanup();
  }
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href
) {
  try {
    process.exitCode = await runIsolatedDesktop(
      process.argv[2],
      process.argv.slice(3),
    );
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
