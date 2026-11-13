/// <reference types="vitest" />
// jsrc 的单测配置：测试对象就是被 jsgen 打进 goja 的那批 TS 源码。
// 模块替换与 jsgen（../jsgen/build-common.mts、build-pipeline.mts）保持一致：被 shim 的模块在测试里也解析到同一批 shim，
// 测试里的 vi.mock("../services/…") 因此与源码里的 import 指向同一个模块。
import { defineConfig } from "vitest/config";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { readingLibrariesPlugin } from "../../desktop/scripts/reading-libraries-plugin.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const packages = path.resolve(here, "../../../packages");
const shim = (name: string) => path.resolve(here, "../jsgen", name);

export default defineConfig({
  root: here,
  plugins: [readingLibrariesPlugin()],
  test: {
    globals: true,
    environment: "node",
    include: ["tests/**/*.test.ts"],
    testTimeout: 30_000,
    hookTimeout: 30_000,
  },
  resolve: {
    alias: [
      { find: /^@guizhi\/core$/, replacement: path.join(packages, "core/src") },
      { find: /^@shared\//, replacement: path.join(packages, "shared") + "/" },
      { find: /^@guizhi\/shared\//, replacement: path.join(packages, "shared") + "/" },
      { find: /^@guizhi\/db$/, replacement: path.join(packages, "db/src") },
      { find: /^@guizhi\/db\//, replacement: path.join(packages, "db/src") + "/" },
      // 与 jsgen/build-pipeline.mts 的 swap 一致
      { find: /^(?:.*\/)?ai-usage$/, replacement: shim("ai-usage-shim.ts") },
      { find: /^(?:.*\/)?search-service$/, replacement: shim("search-shim.ts") },
      { find: /^(?:.*\/)?web-capture\/web-capture$/, replacement: shim("capture-shim.ts") },
    ],
  },
});
