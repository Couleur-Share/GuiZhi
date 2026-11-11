// 生成 spikes/mygo-shell/themed_engine.js：主题阅读渲染层（TS 原文件 + linkedom / sanitize-html / unified 等依赖）的 goja 打包产物。
// 用法（仓库根）：pnpm dlx tsx spikes/mygo-js/build-themed.mts
import { buildGoja } from "./build-common.mts";
await buildGoja("themed-entry.ts", "themed_engine.js");
