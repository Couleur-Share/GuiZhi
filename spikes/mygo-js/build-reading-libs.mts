// 生成 spikes/mygo-shell/reading_libraries.json：与 apps/desktop/scripts/reading-libraries-worker.mjs 同一套编译
//（daisyUI 组件样式 + echarts / mermaid / animation 运行时脚本；compiler 属于冻结的阅读图形，不随 Go 版带出）。
// 用法（仓库根）：pnpm dlx tsx spikes/mygo-js/build-reading-libs.mts
import fs from "node:fs/promises";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";

const require = createRequire(path.join(process.cwd(), "apps/desktop/package.json"));
const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(process.cwd(), "apps/desktop");
const names = ["timeline", "steps", "alert", "badge", "stat", "table", "card"];
const { build } = await import(pathToFileURL(require.resolve("vite")).href);
const bundle = async (name: string) => {
  const result = await build({ configFile: false, root, logLevel: "error", define: { "process.env.NODE_ENV": '"production"' }, build: { write: false, minify: true, target: "chrome130", lib: { entry: path.join(root, `src/reading-graphics/${name}.ts`), name: "GuiZhiReading", formats: ["iife"] }, rollupOptions: { output: { inlineDynamicImports: true } } } });
  return (Array.isArray(result) ? result[0] : result).output.find((item: any) => item.type === "chunk").code as string;
};
const components: Record<string, string> = {};
const daisy = path.dirname(require.resolve("daisyui/package.json"));
for (const name of names) {
  const value = await fs.readFile(path.join(daisy, `components/${name}.css`), "utf8");
  components[name] = `:where(.gz-reading-components){${value.replace(/\.([a-zA-Z][\w-]*)/g, ".gz-ui-$1")}}`;
}
const runtime = { echarts: await bundle("runtime-echarts"), mermaid: await bundle("runtime-mermaid"), animation: await bundle("runtime-animation") };
const out = path.join(here, "..", "mygo-shell", "reading_libraries.json");
await fs.writeFile(out, JSON.stringify({ components, runtime }));
console.log("ok", out, Object.fromEntries(Object.entries(runtime).map(([k, v]) => [k, v.length])));