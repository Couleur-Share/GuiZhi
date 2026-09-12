# 质量门禁与性能基线

归知的发布验证分为四层，按从快到慢的顺序执行：

1. `pnpm --filter @guizhi/desktop typecheck`：跨进程类型、preload 白名单与渲染层类型。
2. `pnpm --filter @guizhi/desktop test:unit`：数据库迁移、导入队列、筛选、语义检索与关键组件。
3. `pnpm build:isolated`：隔离副本生产构建；renderer gzip 预算由发布流程另行校验。
4. `pnpm test:e2e:smoke`：隔离副本 Electron 冒烟验证。

CI 的 `quality` 工作流执行构建、预算和 Electron 冒烟；本地发布前可直接运行
`pnpm --filter @guizhi/desktop test:release`，再从根目录补一轮 `pnpm test:e2e:smoke`；存在开发服务时，打包与发布门禁在独立副本运行。

## 性能观察口径

- 知识库列表使用服务端分页；批量普通字段操作以 400 条为一个 SQL 批次。
- 语义检索使用进程内向量缓存、精确 Top-K 堆选择和分块让出事件循环。问答侧栏会显示最近一次耗时、扫描分块数和缓存冷热状态。
- 导入队列默认并发为 2，可暂停尚未启动的任务，不中断正在下载、转写或写库的任务。

语义检索在热态中位超过 500ms、达到 50,000 分块或 256 MiB 向量时尝试 worker HNSW；运行时不兼容、侧车损坏和构建失败分别处理，必要时回退精确检索。

重度使用合成基线覆盖 1k／10k／50k 资料和 50k 向量，记录冷态、热态与 P95。验收命令、实际结果及真实服务边界见 [本批验收记录](../artifacts/heavy-user-implementation-20260912/REPORT.md)。
