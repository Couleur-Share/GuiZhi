// 生成 spikes/mygo-shell/themed_pipeline.js：主题阅读生成管线（v3-pipeline / design / v3-research / v3-runtime-repair 原文件）。
// 模型、搜索、网页抓取、用量记录换成 Go 宿主桥（见 pipeline-host.ts）。
// 用法（仓库根）：pnpm dlx tsx spikes/mygo-js/build-pipeline.mts
import path from "node:path";
import { buildGoja, here } from "./build-common.mts";

await buildGoja("pipeline-entry.ts", "themed_pipeline.js", [
  {
    name: "pipeline-shims",
    setup(b) {
      const swap = (filter: RegExp, file: string) => b.onResolve({ filter }, () => ({ path: path.join(here, file) }));
      swap(/^@guizhi\/core$/, "core-shim.ts");
      swap(/(^|\/)ai-usage$/, "ai-usage-shim.ts");
      swap(/(^|\/)search-service$/, "search-shim.ts");
      swap(/(^|\/)web-capture\/web-capture$/, "capture-shim.ts");
    },
  },
]);
