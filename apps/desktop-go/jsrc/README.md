# jsrc：Go 壳里 goja 运行的 TS 源码

这里是 `jsgen` 打包成 goja 产物的**TS 源码**（共 42 个文件，约 3.9k 行），以及这批源码的单测：

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
- `@guizhi/shared`、`@guizhi/db` 的别名由本目录 `tsconfig.json`（构建）与 `vitest.config.ts`（单测）解析；第三方依赖
  （linkedom、css-tree、sanitize-html、unified 等）由 `apps/desktop-go/package.json` 声明。`apps/desktop/package.json`
  里的同名声明在主进程目录删除前保留，届时一并去掉。

## 验证

重新生成上述产物应与入库版本**逐字节一致**：

```powershell
pnpm --filter @guizhi/desktop-go gen    # 依次运行 jsgen 下四个构建脚本
git status --short apps/desktop-go      # 应无改动
```

单测（24 个文件、229 个用例，覆盖主题阅读与网页快照的这批源码）：

```powershell
pnpm --filter @guizhi/desktop-go test
```

配置见 `vitest.config.ts`：`virtual:reading-libraries` 复用 `apps/desktop/scripts/reading-libraries-plugin.mjs`；
被 shim 的模块与 `jsgen` 解析到同一批 shim，因此测试里的 `vi.mock` 与源码指向同一个模块。
CI 的 `desktop-go` job 在删除 `apps/desktop/src/main` 之后运行它们，证明不依赖 Electron 主进程。

## 已知限制

- 这里的源码没有独立的类型检查：原先由 `apps/desktop` 的 `tsc --noEmit` 覆盖，而 `ai-usage`、`search-service`、
  `virtual:reading-libraries` 等被构建期替换的模块，以及 `web-settle.ts` 仅类型引用的 `web-network-activity`（Electron 侧文件），
  在本目录里 `tsc` 解析不到（试跑共 8 处 TS2307，均属此类）。补齐需要为这些模块提供类型声明，留待主进程目录删除时处理。
- 迁来的单测只有 24 个文件；其余测试同时依赖 Electron 专有模块（如 `native-search-response`），留在 `apps/desktop/tests/unit`，
  随主进程目录一起处理。
  - `reading-research-recovery` 里原先借 `nativeSearchResults` 造搜索结果的那条用例，迁移时改为直接给出标题与 URL；
    原生搜索响应的解析本身仍由 `apps/desktop/tests/unit/native-reading-search.test.ts` 覆盖。
  - `jsrc/tests` 下的 4 个 fixture 与 `fixtures/reading-graphics` 是从 `apps/desktop/tests` **复制**的，Electron 侧脚本仍在使用原件。
