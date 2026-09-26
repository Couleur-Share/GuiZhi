// 先完成原始正常内存测量，再生成堆快照；只用于临时合成知识库。
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

export default async function (context) {
  const kit = process.env.GUIZHI_MAIN_MEMORY_KIT;
  assert.ok(kit, "需要指定离线测试工具目录");
  const capture = await import(pathToFileURL(path.join(kit, "memory-installed.mjs")).href);
  const { app, outDir } = context;
  const observe = () => app.evaluate(async () => ({
    time: Date.now(), usage: process.memoryUsage(),
    memory: await process.getProcessMemoryInfo(), heap: process.getHeapStatistics(),
  }));
  const startup = await observe();
  await capture.default(context);
  const settled = await observe();
  const heap = await app.evaluate((_, file) => {
    const load = process.mainModule.require.bind(process.mainModule);
    const heapFile = load("node:v8").writeHeapSnapshot(file);
    return { heapFile, usage: process.memoryUsage() };
  }, path.join(outDir, "post-capture.heapsnapshot"));
  fs.writeFileSync(path.join(outDir, "heap-diagnostics.json"), JSON.stringify({ startup, settled, heap }, null, 2));
}
