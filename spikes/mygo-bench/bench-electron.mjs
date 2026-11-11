// 基准：打包后的 Electron 版（win-unpacked）在隔离数据目录里冷启动到首屏就绪的耗时与空闲内存。
// 就绪判据与 apps/desktop/scripts/screenshot.mjs 一致：顶栏搜索框 topbar-search 出现。
// 用法：node bench-electron.mjs <GuiZhi.exe> <knowledge.db 副本来源> [次数] [空闲秒数]
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";

const [exe, srcDb, nStr = "5", idleStr = "10"] = process.argv.slice(2);
const require = createRequire(path.resolve(import.meta.dirname, "../../apps/desktop/package.json"));
const { _electron: electron } = require("playwright");
const n = Number(nStr), idle = Number(idleStr) * 1000;
const sample = (pid) => JSON.parse(execFileSync("powershell", ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path.join(import.meta.dirname, "tree-mem.ps1"), "-RootPid", String(pid)], { encoding: "utf8" }));

const userData = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-bench-electron-"));
fs.mkdirSync(path.join(userData, "data"), { recursive: true });
fs.copyFileSync(srcDb, path.join(userData, "data", "knowledge.db"));
const rows = [];
try {
  for (let i = 0; i < n; i++) {
    const t0 = performance.now();
    const app = await electron.launch({
      executablePath: exe,
      env: { ...process.env, GUIZHI_E2E: "1", GUIZHI_E2E_USER_DATA_DIR: userData, GUIZHI_WINDOW_MODE: "offscreen" },
    });
    const pid = app.process().pid;
    try {
      const win = await app.firstWindow();
      await win.getByTestId("topbar-search").waitFor({ state: "attached", timeout: 60000 });
      const readyMs = performance.now() - t0;
      await new Promise((r) => setTimeout(r, idle));
      const m = sample(pid);
      rows.push({ trial: i + 1, profile: i === 0 ? "cold" : "warm", readyMs: Math.round(readyMs), procs: m.procs, wsMB: m.wsMB, privMB: m.privMB });
      console.log(JSON.stringify(rows.at(-1)));
    } finally {
      await app.close().catch(() => {});
      try { execFileSync("taskkill", ["/PID", String(pid), "/T", "/F"], { stdio: "ignore" }); } catch {}
    }
  }
} finally {
  fs.rmSync(userData, { recursive: true, force: true, maxRetries: 10, retryDelay: 300 });
}
fs.writeFileSync(process.env.BENCH_OUT ?? path.join(os.tmpdir(), "bench-electron.json"), JSON.stringify(rows));
