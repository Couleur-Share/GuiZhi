# 质量门禁与性能基线

归知的发布验证分为四层，按从快到慢的顺序执行：

1. `pnpm typecheck`：跨进程类型、preload 白名单（对着 Go 壳的 `electron` 替身检查）与渲染层类型。
2. `pnpm test:unit`：校验脚本、渲染层单测与 jsrc（goja 产物输入）单测。
3. `go vet ./...` 与 `go test -count=1 ./...`（在 `apps/desktop-go` 下）：Go 壳的处理器、数据库、采集与差分测试。
4. `pnpm shot --steps apps/desktop-go/scripts/steps/smoke-steps.mjs`：隔离副本构建 Go 壳 + 离屏启动冒烟，构建中含渲染层 gzip 预算检查。

CI 的 `quality` 工作流执行以上检查、安装包体积预算与 jsrc 产物一致性校验；本地发布前可直接运行 `pnpm verify:release`
（lint、类型检查、单测、Go 壳隔离构建与界面冒烟）。存在用户实例时，打包与发布门禁在独立副本运行。

## 交付体积门禁

`config/package-size-budget.json` 给 Windows 安装包设一条上限，由 `scripts/check-package-size.mjs` 判定。发版工作流在安装包构建后执行，超限直接失败，不会产出 Draft Release。renderer 的 gzip 预算（`apps/desktop/bundle-budget.json`）只覆盖前端资源，两者互不替代。

- 上限取实测值上浮约 8%。有意增长（升级 MyGo、新增随包组件）时，在同一个提交里修改预算并写明原因，先确认新增内容确实会在运行时被加载。
- 随包 Python 运行包（Crawl4AI）已于 2026-11-07 移除，运行包的字节数、文件数预算及其 Python 回归随之取消。网页正文提取规则的单测与回归口径见 [网页采集](web-capture.md)；正文提取与阅读管线的回归由 jsrc 单测与 Go 差分测试（`apps/desktop-go/testdata/golden`）覆盖。
- 本地检查：`node scripts/check-package-size.mjs --installer apps/desktop-go/out/installer/GuiZhi-Setup-<版本>-x64.exe`。

## 性能观察口径

- 知识库列表使用服务端分页；批量普通字段操作以 400 条为一个 SQL 批次。
- 语义检索使用进程内向量缓存、精确 Top-K 堆选择和分块让出事件循环。问答侧栏会显示最近一次耗时、扫描分块数和缓存冷热状态。
- 导入队列默认并发为 2，可暂停尚未启动的任务，不中断正在下载、转写或写库的任务。

语义检索只做精确扫描；HNSW 加速索引（`hnswlib-wasm`）在 Electron 主进程中无法加载，已于 2026-11-07 移除，Go 壳沿用精确扫描。

重度使用合成基线覆盖 1k／10k／50k 资料和 50k 向量，记录冷态、热态与 P95。验收命令、实际结果及真实服务边界见 [本批验收记录](../artifacts/heavy-user-implementation-20260912/REPORT.md)。
