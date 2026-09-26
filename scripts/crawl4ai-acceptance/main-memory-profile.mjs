// 仅由隔离截图驱动调用；正常采样完成后才进行模块清单和堆诊断。
import fs from "node:fs";
import path from "node:path";
import { spawn } from "node:child_process";

const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
export default async function ({ app, outDir }) {
  if (["sampler.stop", "main-memory.json", "memory-samples.jsonl"].some((name) => fs.existsSync(path.join(outDir, name)))) {
    throw new Error("测量输出目录已有数据，请通过 --out 指定新的目录");
  }
  const samples = [];
  let samplerExit;
  if (process.env.GUIZHI_MAIN_MEMORY_KIT) {
    const kit = process.env.GUIZHI_MAIN_MEMORY_KIT;
    const pid = await app.evaluate(() => process.pid);
    fs.writeFileSync(
      path.join(outDir, "memory-stage-00000.json"),
      JSON.stringify({ stage: "startup-idle" }),
    );
    const sampler = spawn(
      path.join(kit, "tools/python/python.exe"),
      [
        "-B",
        path.join(kit, "memory-sampler.py"),
        "--pid",
        String(pid),
        "--out",
        outDir,
      ],
      { windowsHide: true, stdio: "inherit" },
    );
    samplerExit = new Promise((resolve, reject) => {
      sampler.once("error", reject);
      sampler.once("exit", (code) =>
        code === 0 ? resolve() : reject(new Error(`内存采样失败：${code}`)),
      );
    });
    // 错误仍在 finally 中传播；提前挂接处理器，避免采样期间的未处理拒绝。
    samplerExit.catch(() => {});
  }
  try {
    for (let i = 0; i < 20; i++) {
      await delay(500);
      samples.push(
        await app.evaluate(async () => ({
          time: Date.now(),
          pid: process.pid,
          usage: process.memoryUsage(),
          memory: await process.getProcessMemoryInfo(),
          heap: process.getHeapStatistics(),
        })),
      );
    }
  } finally {
    if (samplerExit) {
      fs.writeFileSync(path.join(outDir, "sampler.stop"), "");
      await samplerExit;
    }
  }
  const details = await app.evaluate(({ app }) => ({
    version: app.getVersion(),
    versions: process.versions,
    appPath: app.getAppPath(),
    moduleAccess: typeof require,
    modules: typeof require === "function" ? Object.keys(require.cache) : [],
    mainModule: process.mainModule?.filename,
    mainModuleRequire: typeof process.mainModule?.require,
  }));
  fs.writeFileSync(
    path.join(outDir, "main-memory.json"),
    JSON.stringify({ samples, details }, null, 2),
  );
  if (process.env.GUIZHI_MAIN_HEAP_SNAPSHOT === "1") {
    const snapshot = await app.evaluate(
      (_, file) => {
        const load = process.mainModule.require.bind(process.mainModule);
        const Module = load("node:module");
        const modules = Object.keys(Module._cache);
        const heapFile = load("node:v8").writeHeapSnapshot(file);
        return { modules, heapFile, usage: process.memoryUsage() };
      },
      path.join(outDir, "main.heapsnapshot"),
    );
    fs.writeFileSync(
      path.join(outDir, "main-heap.json"),
      JSON.stringify(snapshot, null, 2),
    );
  }
}
