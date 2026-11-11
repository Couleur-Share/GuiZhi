// TS 原版基准：把 electron 的 ipcMain / ipcRenderer 接成内存总线，让真实的 ai.ipc.ts（主进程）与真实的 preload aiApi
// 在 Node 里端到端跑一遍同一套场景。electron 之外的依赖都是真实的（undici 的 fetch、net-safety、ai-endpoint-guard）。
// 用法：pnpm dlx tsx spikes/mygo-ai/ts-run.mts <模拟服务地址> <输出.json>
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import Module from "node:module";

// 隔离 APPDATA：ai-endpoint-guard 会读用户的 ai-models.json（取已配置端点），基准不能碰真实目录
const fakeAppData = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-ai-ts-"));
process.env.APPDATA = fakeAppData;
process.env.HOME = fakeAppData;

type Handler = (event: unknown, ...args: unknown[]) => unknown;
const handlers = new Map<string, Handler>();
const onHandlers = new Map<string, Handler>();
const rendererListeners = new Map<string, Set<(event: unknown, payload: unknown) => void>>();
const sender = {
  send(channel: string, payload: unknown) {
    // 与 Electron 一致：send 是异步投递，不阻塞主进程循环
    queueMicrotask(() => { for (const fn of rendererListeners.get(channel) ?? []) fn({}, payload); });
  },
};
const electronStub = {
  ipcMain: {
    handle: (ch: string, fn: Handler) => void handlers.set(ch, fn),
    on: (ch: string, fn: Handler) => void onHandlers.set(ch, fn),
  },
  ipcRenderer: {
    invoke: async (ch: string, ...args: unknown[]) => {
      const fn = handlers.get(ch);
      if (!fn) throw new Error(`no handler ${ch}`);
      return fn({ sender }, ...args);
    },
    send: (ch: string, ...args: unknown[]) => void onHandlers.get(ch)?.({ sender }, ...args),
    on: (ch: string, fn: (e: unknown, p: unknown) => void) => {
      if (!rendererListeners.has(ch)) rendererListeners.set(ch, new Set());
      rendererListeners.get(ch)!.add(fn);
    },
    removeListener: (ch: string, fn: (e: unknown, p: unknown) => void) => void rendererListeners.get(ch)?.delete(fn),
  },
  session: { defaultSession: {} },
};
const orig = (Module as any)._load;
(Module as any)._load = function (request: string, ...rest: unknown[]) {
  if (request === "electron") return electronStub;
  return orig.call(this, request, ...rest);
};

const [base, outFile] = process.argv.slice(2);
if (!base || !outFile) throw new Error("用法: ts-run.mts <模拟服务地址> <输出.json>");

const { registerAIIPC } = await import("../../apps/desktop/src/main/ipc/ai.ipc.ts");
const { aiApi } = await import("../../apps/desktop/src/preload/api/ai.ts");
const { runAll } = await import("./runner.mjs");
const fakeDb = new Proxy({}, { get: () => () => ({ run() {}, get() {}, all: () => [] }) });
registerAIIPC(fakeDb as never);

const scenarios = JSON.parse(fs.readFileSync(path.join(import.meta.dirname, "scenarios.json"), "utf8"));
const result = await runAll(scenarios, aiApi, base);
fs.writeFileSync(outFile, JSON.stringify(result, null, 1));
fs.rmSync(fakeAppData, { recursive: true, force: true });
console.log("TS 基准完成：", Object.keys(result).length, "个场景");
process.exit(0);
