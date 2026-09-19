# 归知 GuiZhi — 开发约定

归知是本地优先的 AI 个人知识库，采用 Electron + React + TypeScript、pnpm monorepo。
应用骨架源自 PromptHub v0.5.9；AGPL-3.0 归属说明见 `NOTICE`。具体依赖版本以当前 `package.json` 为准。

## 源码与数据边界

- `apps/desktop/src/main`：窗口、IPC、采集、AI 请求及桌面服务；`preload`：contextBridge 白名单；`renderer`：React 界面和状态。
- `packages/core`：AI 配置与客户端；`packages/db`：SQLite schema 与适配器；`packages/shared`：跨进程类型和 IPC 常量。
- 渲染进程不直接访问 Node API，系统能力走 preload 白名单。IPC 频道集中在 `packages/shared/constants/ipc-channels.ts`，main 注册于 `main/ipc/index.ts`。
- 数据库在用户数据目录的 `data/knowledge.db`；旧 .NET 数据库仅作只读迁移源。AI 配置在 `config/ai-models.json`，AI HTTP/流式请求经主进程代理。
- 远程抓取经过 `main/services/net-safety.ts` 的 SSRF 防护。不要把 API Key、代理口令等机密写入日志或普通导出；配置迁移中离开数据目录的机密字段必须加密。数据库备份与 Markdown 导出不携带 AI 配置。
- 手机收集桌面凭证例外：仅主进程 safeStorage 加密保存于 `.machine/mobile-capture.json`；不通过 IPC 返回，不加入备份、导出或目录迁移白名单。Linux basic_text 或安全存储不可用时仅保存在当前进程内存，禁止明文回落。

## UI 与错误处理

- 复用现有语义令牌与 `components/ui` 控件；不引入原生 select、color picker 或 alert/confirm/prompt。颜色、主题与字体遵循现有设置体系。
- Tooltip 补充可见文字之外的信息；不要重复控件文字，图标控件保留可访问名。
- 用户操作失败必须可理解、可重试；用 `showToast` 的 `detail` 展示可复制的详细原因。批量操作分别记账，汇总成功/失败及逐条原因，不能失败后仍报成功。
- store 变更使用 `runGuardedMutation`，调用方检查其 boolean 结果；后台失败通过 `logAppError` 留痕，日志脱敏。加载失败用 `loadError` / `LoadErrorState`，不要渲染成正常空态。
- IPC 返回携带错误原因的对象。当前 `strict: false` 下沿用 `{ success/ok: boolean; error?: string }`，不要依赖判别式联合收窄。
- 注释和日志使用中文或中英双语，标识符用英文。文件行数门禁见 `config/file-line-limit-baseline.json`。

## 开发与验证

```bash
pnpm electron:dev        # 开发，连接用户数据目录，启动前注意当前实例
pnpm typecheck
pnpm test:unit
pnpm lint
pnpm shot                # 使用独立临时数据目录的离屏截图
pnpm build:isolated      # 当前源码副本中构建，不覆盖开发实例的 out/
pnpm electron:build:win
```

按改动范围选择验证；发版按发版流程完成门禁。报告区分源码/隔离测试、真实第三方流程和已发布状态。

- UI 验证使用 `pnpm shot`，自动复制当前源码（含未提交改动）到系统临时目录、构建并离屏启动。需要交互时用 `--steps <file>`；无需先在开发目录 build，不得用 `--stale-ok` 验证新改动。根目录 `pnpm test:e2e` / `pnpm test:e2e:smoke` 同样走隔离副本。
- 保留现有屏幕外窗口 + `showInactive()` 的截图实现及临时 userData 隔离，不能简单改成隐藏窗口；平台差异见下方桌面开发参考。
- 默认采用串行开发：用户在代理执行任务期间不使用归知，任务完成后再启动；无需管理工作副本或候选版本。代理开始前仍须检查实例，不得自行关闭用户应用。
- 用户的 `electron:dev` 可能正在运行。开始修改前先检查当前目录的开发服务；并行使用时，源码修改与验证必须在独立 worktree/工作副本中进行（包含本任务所需的未提交改动，并保留原目录改动）。不要在被监听的目录改 main/preload/renderer、共享包、Vite 配置或依赖文件；仅减少写入次数不能避免干扰。完成后先交付差异，待用户切换版本时再回写原目录。
- 验证不得停掉、重启或复用用户实例，不得按进程名称批量结束 Electron/Node，也不得连接用户的 Vite 端口或使用其数据目录。开发目录仍有服务时，不运行原地 `pnpm build`、子包 `test:e2e`、依赖 install/rebuild；构建检查用 `pnpm build:isolated`，打包等必须在独立工作副本运行。

## 按需参考

- 改架构、IPC、错误处理或截图工具时，查阅 [桌面开发细节](docs/agents/desktop-development.md) 中对应章节；其中源码路径相对于仓库根目录。
- 追溯旧实现与设计原因时，在 [实现历史](docs/agents/implementation-history.md) 中按模块搜索。它是历史资料，当前行为以源码和实测为准，无需每次全文加载。
- 发版时读取 `.cursor/skills/guizhi-release/SKILL.md`。
