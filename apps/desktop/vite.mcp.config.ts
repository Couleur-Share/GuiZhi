import { defineConfig } from "vite";
import path from "path";

/**
 * MCP server 的独立构建。
 *
 * 产物是单文件 ESM（out/mcp/guizhi-mcp.mjs），打包后由应用本体以
 * ELECTRON_RUN_AS_NODE 模式执行，所以用户机器上不需要装 Node。
 *
 * 依赖全部内联（ssr.noExternal），唯独 node-sqlite3-wasm 保持外部引用：
 * 它要按相对路径读同目录下 1.2MB 的 .wasm，bundle 进来那个文件就找不到了。
 * electron-builder 会把它整包复制到产物旁的 node_modules/ 下，
 * 靠 Node 常规的向上查找解析，不用手拼 app.asar.unpacked 路径。
 */
export default defineConfig({
  resolve: {
    alias: {
      "@guizhi/core": path.resolve(__dirname, "../../packages/core/src"),
      "@guizhi/shared": path.resolve(__dirname, "../../packages/shared"),
      "@guizhi/db": path.resolve(__dirname, "../../packages/db/src"),
    },
  },
  ssr: {
    noExternal: true,
    external: ["node-sqlite3-wasm"],
  },
  build: {
    outDir: "out/mcp",
    emptyOutDir: true,
    ssr: "src/mcp/server.ts",
    target: "node20",
    // 不压缩：这个产物要被用户在 mcp.json 里按绝对路径引用，
    // 出问题时读得懂堆栈比省几百 KB 重要
    minify: false,
    rollupOptions: {
      output: {
        format: "es",
        entryFileNames: "guizhi-mcp.mjs",
      },
    },
  },
});
