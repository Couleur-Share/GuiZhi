# 质量门禁与性能基线

归知的发布验证分为四层，按从快到慢的顺序执行：

1. `pnpm --filter @guizhi/desktop typecheck`：跨进程类型、preload 白名单与渲染层类型。
2. `pnpm --filter @guizhi/desktop test:unit`：数据库迁移、导入队列、筛选、语义检索与关键组件。
3. `pnpm --filter @guizhi/desktop build && pnpm --filter @guizhi/desktop bundle:budget`：生产构建及 renderer gzip 预算。
4. `pnpm --filter @guizhi/desktop test:e2e:smoke`：Electron 冒烟验证。

CI 的 `quality` 工作流执行构建、预算和 Electron 冒烟；本地发布前可直接运行
`pnpm --filter @guizhi/desktop test:release`，再补一轮 `test:e2e:smoke`。

## 性能观察口径

- 知识库列表使用服务端分页；批量普通字段操作以 400 条为一个 SQL 批次。
- 语义检索使用进程内向量缓存、精确 Top-K 堆选择和分块让出事件循环。问答侧栏会显示最近一次耗时、扫描分块数和缓存冷热状态。
- 导入队列默认并发为 2，可暂停尚未启动的任务，不中断正在下载、转写或写库的任务。

若语义检索在缓存命中后仍稳定超过 500ms，或分块数达到 50,000，应评估引入 ANN/HNSW 索引；在此之前保留精确检索，避免近似召回悄悄漏掉用户资料。
