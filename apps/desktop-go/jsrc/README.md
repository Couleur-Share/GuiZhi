# jsrc：Go 壳里 goja 运行的 TS 源码

这里是 `jsgen` 打包成 goja 产物的**TS 源码**（共 42 个文件，约 3.9k 行）：

| 产物 | 入口 | 说明 |
|---|---|---|
| `../themed_engine.js` | `../jsgen/themed-entry.ts` | 主题阅读渲染层 |
| `../themed_pipeline.js` | `../jsgen/pipeline-entry.ts` | 主题阅读生成管线 |
| `../websnap_sanitize.js` | `../jsgen/sanitizer-entry.ts` | 网页快照净化 |
| `../render_scripts_gen.go` | `../jsgen/build-render-scripts.mts` | 渲染窗口里执行的页面侧脚本 |

## 来源与约定

- 2026-11-13 从 `apps/desktop/src/main/services/{themed-reading,web-capture}` **原样复制**（逐字节一致），目的是让 Go 壳的 TS 产物不再依赖 Electron 主进程目录；
  复制期间两处并存，Electron 终版已冻结（`electron-final-0.28.x`），两边不应再各自修改。主进程目录删除后这里是唯一来源。
- 保留 `services/` 层级，源文件里的相对导入（`./x`、`../web-capture/x`）无需改动。`../ai-usage`、`../search-service`、
  `web-capture/web-capture`、`@guizhi/core` 由 `jsgen` 的插件换成 shim，不需要对应文件。
- `@guizhi/shared`、`@guizhi/db` 的别名由本目录 `tsconfig.json` 解析；第三方依赖（linkedom、css-tree、sanitize-html、unified 等）
  目前仍由 `apps/desktop/package.json` 声明、经 `shamefullyHoist` 解析。

## 验证

重新生成上述产物应与入库版本**逐字节一致**：

```powershell
pnpm dlx tsx apps/desktop-go/jsgen/build-themed.mts
pnpm dlx tsx apps/desktop-go/jsgen/build-pipeline.mts
pnpm dlx tsx apps/desktop-go/jsgen/build.mts
pnpm dlx tsx apps/desktop-go/jsgen/build-render-scripts.mts
git status --short apps/desktop-go   # 应无改动
```

## 已知限制

- 这里的源码没有独立的类型检查：原先由 `apps/desktop` 的 `tsc --noEmit` 覆盖，而 `ai-usage`、`search-service` 等被 shim 的模块不在本目录。
- 原有 27 个单测仍在 `apps/desktop/tests/unit`，指向 `apps/desktop/src/main` 的原文件；主进程目录删除前需要把它们迁到这里。
