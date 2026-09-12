# 本文问答验证记录

验证日期：2026-09-08。验证对象为当前工作区源码，包含现有未提交的阅读改版；未提交、打包或发布。

| 检查 | 结果 |
| --- | --- |
| `pnpm typecheck` | 通过 |
| `pnpm test:unit` | 240 个测试文件、2135 项测试通过；验证副本工具的 4 项 Node 测试也通过 |
| `pnpm --filter @guizhi/desktop lint` | 通过，无 ESLint 错误或警告 |
| `pnpm lint` | 被任务开始前已有的未跟踪脚本 `scripts/generate_fish_redesign.mjs` 行数门禁阻断：1720 行，限制 1500 行；该脚本未修改 |
| `pnpm build:isolated` | 通过，开发目录 `out/` 未用于验证 |
| `git diff --check` | 通过；保留既有文件的换行形式 |
| 离屏交互 | 正文、文字稿、OCR、讨论、AI 阅读页、网页快照、全局历史、新会话、无搜索配置、搜索失败与重新打开历史通过，无页面异常 |

新增回归覆盖：旧会话幂等迁移、同名文章按 ID 隔离、已删除文章、长文末尾选段及邻段、快照版本、旧 AI 阅读格式、联网去重和补抓预算、取消、流式拼接、保存失败、加载失败、中断恢复及迟到回调隔离。

离屏验收过程中发现并修复：iframe 选区不属于父窗口的 `Selection`，外层鼠标事件原本会在按钮点击前清除划词菜单；菜单事件现已单独处理，两个 iframe 视图均已复验。

交互证据由以下脚本生成：

- `apps/desktop/tests/e2e/article-ask-shots.mjs`
- `apps/desktop/tests/e2e/article-ask-frames-shots.mjs`
- 输出位于 `artifacts/article-ask-frames/`，包括 12 张截图及两份 JSON 验收结果。

全部截图使用独立临时数据目录和屏幕外 Electron；模型与搜索响应由隔离测试 IPC 提供。没有复用用户实例、访问用户数据库或测试真实 AI／搜索服务，因此不将这些结果视为真实第三方流程或已安装发布版验收。
