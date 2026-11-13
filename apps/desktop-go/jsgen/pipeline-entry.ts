// 打进 goja 的主题阅读生成管线入口：直接引用 TS 原文件，保证提示词、分块、重试、校验与 Electron 版逐字一致。
// 异步：每次运行由 Go 侧事件循环驱动；模型 / 搜索 / 网页抓取走宿主桥，钩子把最新版本 JSON 同步回 Go 保存。
import "./text-polyfill";
import "./url-polyfill";
import "./async-polyfill";
import { runReadingV3 } from "../../desktop/src/main/services/themed-reading/v3-pipeline";
import { repairReadingRuntime } from "../../desktop/src/main/services/themed-reading/v3-runtime-repair";
import { hostAsync, hostCallback, hostSync } from "./pipeline-host";

const controllers = new Map<number, AbortController>();

async function execute(runId: number, op: string, args: { version: any; config: any }, signal: AbortSignal): Promise<string> {
  const page = args.version;
  const snapshot = () => hostSync("checkpoint", { version: JSON.stringify(page) });
  if (op === "design") {
    await runReadingV3(page, args.config, signal, {
      checkpoint: snapshot,
      stage: (stage, done, total) => { hostSync("stage", { version: JSON.stringify(page), stage, done: done ?? 0, total: total ?? 0 }); },
      request: kind => { hostSync("request", { kind }); },
      designProgress: (attempt, chars) => { hostSync("designProgress", { attempt, chars }); },
    });
  } else if (op === "repair") {
    await repairReadingRuntime(page, args.config, signal,
      async p => { const r = JSON.parse(await hostAsync("verify", { version: JSON.stringify(p) }, signal)); return r ?? undefined; },
      snapshot, () => { hostSync("request", { kind: "textCalls" }); });
  } else throw new Error(`未知管线操作：${op}`);
  return JSON.stringify(page);
}

(globalThis as Record<string, unknown>).GZP = {
  start(runId: number, op: string, argsJSON: string, done: (result: string | null, error: string | null) => void) {
    const ac = new AbortController();
    controllers.set(runId, ac);
    execute(runId, op, JSON.parse(argsJSON), ac.signal).then(
      r => { controllers.delete(runId); done(r, null); },
      (e: any) => { controllers.delete(runId); done(null, JSON.stringify({ message: e?.message ?? String(e), aborted: ac.signal.aborted })); },
    );
  },
  abort(runId: number, reason: string) { controllers.get(runId)?.abort(new Error(reason)); },
  callback: hostCallback,
};
