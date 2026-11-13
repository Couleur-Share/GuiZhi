# jsrc：Go 壳里 goja 运行的 TS 源码

这里是 `jsgen` 打包成 goja 产物的**TS 源码**（共 42 个文件，约 3.9k 行），以及这批源码的单测：

| 产物 | 入口 | 说明 |
|---|---|---|
| `../themed_engine.js` | `../jsgen/themed-entry.ts` | 主题阅读渲染层 |
| `../themed_pipeline.js` | `../jsgen/pipeline-entry.ts` | 主题阅读生成管线 |
| `../websnap_sanitize.js` | `../jsgen/sanitizer-entry.ts` | 网页快照净化 |
| `../render_scripts_gen.go` | `../jsgen/build-render-scripts.mts` | 渲染窗口里执行的页面侧脚本 |

## 来源与约定

- 2026-11-13 从 `apps/desktop/src/main/services/{themed-reading,web-capture}` **原样复制**而来；Electron 主进程目录已于同日删除（标签 `pre-electron-removal` 可查旧位置），这里是唯一来源。
- 保留 `services/` 层级，源文件里的相对导入（`./x`、`../web-capture/x`）无需改动。`../ai-usage`、`../search-service`、
  `web-capture/web-capture`、`@guizhi/core` 由 `jsgen` 的插件换成 shim，不需要对应文件。
- `@guizhi/shared`、`@guizhi/db` 的别名由本目录 `tsconfig.json`（构建）与 `vitest.config.ts`（单测）解析；第三方依赖
  （linkedom、css-tree、sanitize-html、unified 等）由 `apps/desktop-go/package.json` 声明。`apps/desktop/package.json`
  里残留的 linkedom、css-tree、sanitize-html 声明已无使用方，可择机清理（需同步更新 lockfile）。

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
CI 的 `desktop-go` job 会运行它们。

## 已知限制

- 这里的源码没有独立的类型检查：原先由 `apps/desktop` 的 `tsc --noEmit` 覆盖，而 `ai-usage`、`search-service`、
  `virtual:reading-libraries` 等被构建期替换的模块，以及 `web-settle.ts` 仅类型引用的 `web-network-activity`（该文件随 Electron 主进程已删除，引用现在是悬空的），
  在本目录里 `tsc` 解析不到（试跑共 8 处 TS2307，均属此类）。补齐需要为这些模块提供类型声明或清理悬空引用。
- 单测只有 24 个文件：其余依赖 Electron 专有模块的旧测试已随主进程删除（见标签 `pre-electron-removal`）。
  - `reading-research-recovery` 里原先借 `nativeSearchResults` 造搜索结果的那条用例，迁移时改为直接给出标题与 URL；
    原生搜索响应的解析在 Go 壳中由 Go 测试覆盖。
