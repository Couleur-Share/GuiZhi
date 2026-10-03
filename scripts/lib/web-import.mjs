/**
 * pnpm shot 步骤脚本共用：经导入队列采集一个网址，并读取本次的应用内追踪行。
 * Shared by pnpm shot step scripts: capture one URL through the import queue and read its trace row.
 *
 * 每次采集完成后删除生成的条目与任务，避免后续同网址被判重复。
 */
import fs from "node:fs";
import path from "node:path";

const TERMINAL = new Set(["completed", "failed", "canceled", "duplicate"]);
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

export function lineCount(file) {
  return fs.existsSync(file) ? fs.readFileSync(file, "utf8").split("\n").filter(Boolean).length : 0;
}

/** 读取追踪文件中第 from 行之后新增的行。 */
export function readTraceRows(file, from) {
  return fs
    .readFileSync(file, "utf8")
    .split("\n")
    .filter(Boolean)
    .slice(from)
    .map((line) => JSON.parse(line));
}

/**
 * @param {{ win: import("playwright").Page, outDir: string, timeoutMs: number, saveContent?: boolean }} options
 * 返回 importOnce(url, traceFile, label)：traceFile 为空则不读追踪；
 * saveContent 且 label 非空时，把正文存入 <outDir>/content/<label>.md。
 */
export function createImporter({ win, outDir, timeoutMs, saveContent = false }) {
  return async function importOnce(url, traceFile, label = "") {
    const before = traceFile ? lineCount(traceFile) : 0;
    const started = Date.now();
    const [created] = await win.evaluate(
      (input) => window.api.import.enqueue([{ kind: "url", input }]),
      url,
    );
    let task;
    for (const end = started + timeoutMs; Date.now() < end; ) {
      task = await win.evaluate(
        async (id) => (await window.api.import.list()).find((entry) => entry.id === id),
        created.id,
      );
      if (TERMINAL.has(task?.status)) break;
      await sleep(250);
    }
    const wallMs = Date.now() - started;
    let content = "";
    if (task?.resultItemId) {
      const item = await win.evaluate((id) => window.api.knowledge.get(id), task.resultItemId);
      content = typeof item?.content === "string" ? item.content : "";
    }
    if (saveContent && label && content) {
      fs.mkdirSync(path.join(outDir, "content"), { recursive: true });
      fs.writeFileSync(path.join(outDir, "content", `${label}.md`), content);
    }
    try {
      await win.evaluate(
        async ({ itemId, taskId }) => {
          if (itemId) await window.api.knowledge.deleteForever([itemId], { clearEvidence: true });
          await window.api.import.remove(taskId);
        },
        { itemId: task?.resultItemId ?? null, taskId: created.id },
      );
    } catch (error) {
      console.log(`  清理失败（后续同网址可能被判重复）：${String(error?.message ?? error).slice(0, 200)}`);
    }
    if (!TERMINAL.has(task?.status)) throw new Error(`${url} 未在 ${timeoutMs / 1000}s 内结束`);
    if (task.status === "duplicate")
      throw new Error(`${url} 被判定为重复，对照数据无效；请检查上一轮清理是否成功`);
    const rows = traceFile && fs.existsSync(traceFile) ? readTraceRows(traceFile, before) : [];
    return {
      status: task.status,
      error: task.error ?? null,
      wallMs,
      stageStats: task.stageStats ?? null,
      trace: rows.at(-1) ?? null,
      traceRows: rows.length,
      content,
    };
  };
}
