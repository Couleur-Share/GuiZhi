# 归知 GuiZhi — 开发约定

归知是本地优先的 AI 个人知识库：Go + WebView2 桌面外壳（基于 MyGo）加 React + TypeScript 界面，pnpm monorepo。
界面骨架源自 PromptHub v0.5.9；AGPL-3.0 归属说明见 `NOTICE`。具体依赖版本以当前 `package.json` 与 `apps/desktop-go/go.mod` 为准。
Electron 版已于 2026-11-13 退役，终版保存在分支 `electron-final`、标签 `electron-final-0.28.x`；删除主进程前的完整状态是标签 `pre-electron-removal`。

## 源码与数据边界

- `apps/desktop-go`：桌面外壳——窗口、托盘、IPC 处理器、SQLite、采集、AI 请求代理、备份、更新器，以及构建、安装包与截图工具链。
- `apps/desktop/src/renderer`：React 界面与状态；`apps/desktop/src/preload`：`window.api` 白名单，经 `apps/desktop-go/shim` 接到 Go 处理器。
- `packages/core`：AI 配置与客户端；`packages/db`：SQLite schema 与迁移；`packages/shared`：跨进程类型和 IPC 常量。
- 渲染进程不直接访问系统能力，一律走 preload 白名单。IPC 频道集中在 `packages/shared/constants/ipc-channels.ts`；`apps/desktop-go/ipc_channels_gen.go` 由 preload 扫描生成，新增频道必须同时补 Go 处理器。
- 数据库在用户数据目录的 `data/knowledge.db`；旧 .NET 数据库仅作只读迁移源。AI 配置在 `config/ai-models.json`，AI HTTP/流式请求经 Go 壳代理。
- 远程抓取经过 `apps/desktop-go/net_safety.go`（含 `safe_fetch.go`）的 SSRF 防护。不要把 API Key、代理口令等机密写入日志或普通导出；配置迁移中离开数据目录的机密字段必须加密（Windows DPAPI）。数据库备份与 Markdown 导出不携带 AI 配置。
- 手机收集已从桌面端移除（见 `docs/product-scope.md`）：桌面端不保存手机收集凭证；`apps/capture-*` 仍在仓库中，其约束见下文各节。
- `apps/desktop-go/jsrc` 的 TS 经 `jsgen` 生成嵌入 goja 的产物，**产物入库**，CI 要求与重新生成的结果一致：改了 `jsrc`/`jsgen` 后运行 `pnpm --filter @guizhi/desktop-go gen` 并一起提交。

## UI 与错误处理

- 复用现有语义令牌与 `components/ui` 控件；不引入原生 select、color picker 或 alert/confirm/prompt。颜色、主题与字体遵循现有设置体系。
- Tooltip 补充可见文字之外的信息；不要重复控件文字，图标控件保留可访问名。
- 用户操作失败必须可理解、可重试；用 `showToast` 的 `detail` 展示可复制的详细原因。批量操作分别记账，汇总成功/失败及逐条原因，不能失败后仍报成功。
- store 变更使用 `runGuardedMutation`，调用方检查其 boolean 结果；后台失败通过 `logAppError` 留痕，日志脱敏。加载失败用 `loadError` / `LoadErrorState`，不要渲染成正常空态。
- IPC 返回携带错误原因的对象。当前 `strict: false` 下沿用 `{ success/ok: boolean; error?: string }`，不要依赖判别式联合收窄。
- 注释和日志使用中文或中英双语，标识符用英文。文件行数门禁见 `config/file-line-limit-baseline.json`。

## 平台支持政策

- 官方仅为 Windows 11+ x64 提供安装包、平台测试与维护支持；Windows ARM64 不提供当前版本安装包。外壳依赖 WebView2 运行时。
- Linux / macOS 没有桌面外壳：Go 壳只面向 Windows，不再保留 Electron 的跨平台构建入口，也不提供官方安装包与兼容性保证。
- 平台专属问题不属于官方维护范围；欢迎可验证的社区 PR，不承诺修复或合并时限。未经用户明确要求，不将 Linux / macOS 加回官方发布矩阵。
- Linux 上的通用质量检查（含手机收集服务预检）不代表 Linux 桌面版获得官方支持。

## 开发与验证

```bash
pnpm typecheck
pnpm test:unit           # 校验脚本 + 渲染层 vitest + jsrc 单测
pnpm lint
pnpm shot                # 隔离副本构建 Go 壳 + 离屏启动 + 截图（含渲染层包体预算）
pnpm build               # 原位构建到 apps/desktop-go/out/win-x64（需要 pwsh 7）
pnpm build:installer     # 在 build 产物上打 NSIS 安装包
pnpm verify:release      # 发版前验证组合
```

Go 侧在 `apps/desktop-go` 下运行 `go vet ./...` 与 `go test -count=1 ./...`（约 2 分钟，会拉起真实 WebView2 隐藏窗口，需先 `pnpm build`）。
没有 HMR 开发服务：改完界面源码用 `pnpm shot` 看效果。按改动范围选择验证；发版按发版流程完成门禁。报告区分源码/隔离测试、真实第三方流程和已发布状态。

- UI 验证使用 `pnpm shot`，自动复制当前源码（含未提交改动）到系统临时目录、构建并离屏启动。需要交互时用 `--steps <file>`（示例在 `apps/desktop-go/scripts/steps/`）；无需先在开发目录 build。
- 保留屏幕外窗口 + `showInactive()` 的截图实现及临时 userData 隔离，不能改成隐藏窗口（隐藏窗口的合成器停摆，截图拿不到新帧）；细节见桌面开发参考。
- 默认采用串行开发：用户在代理执行任务期间不使用归知，任务完成后再启动；无需管理工作副本或候选版本。代理开始前仍须检查实例，不得自行关闭用户应用。
- 用户可能正在运行归知（安装版或自行构建的 `GuiZhi.exe`）。并行使用时，源码修改与验证必须在独立 worktree/工作副本中进行（包含本任务所需的未提交改动，并保留原目录改动）；完成后先交付差异，待用户切换版本时再回写原目录。
- 验证不得停掉、重启或复用用户实例，不得按进程名称批量结束 `GuiZhi.exe`/Node，不得使用用户数据目录：直接运行构建出的 `GuiZhi.exe` 不带 `-userdata` 会指向真实 `%APPDATA%\GuiZhi`。不要原位覆盖用户正在运行的 `apps/desktop-go/out/win-x64/GuiZhi.exe`；构建检查用 `pnpm shot`，打包等放在独立工作副本。

## 按需参考

- 改架构、IPC、错误处理、截图工具或安装包时，查阅 [桌面开发细节](docs/agents/desktop-development.md) 中对应章节；其中源码路径相对于仓库根目录。Electron 时代的旧实现笔记在 [electron-era-development.md](docs/agents/electron-era-development.md)（历史资料）。
- 追溯旧实现与设计原因时，在 [实现历史](docs/agents/implementation-history.md) 中按模块搜索。它是历史资料，当前行为以源码和实测为准，无需每次全文加载。
- 新增功能、设置项或改动外围功能前，先读 [产品边界](docs/product-scope.md)；冻结中的功能只修安全、崩溃与数据问题。
- 发版时读取 `.cursor/skills/guizhi-release/SKILL.md`。
- 手机收集中转服务的自托管说明见 [mobile-capture-ops.md](docs/mobile-capture-ops.md)；生产服务器的部署与升级约束属于维护者本机配置，不随仓库公开，由本机 skill `guizhi-linux-preflight` 指引。

## Linux 收集服务预检

手机收集服务的完整 Linux 检查用 `pnpm check:linux` 执行，在维护者的共享远程构建器上运行；连接方式、操作步骤与证据回收属于维护者本机配置，不随仓库公开，细节见本机 skill `guizhi-linux-preflight`（`.cursor/skills/guizhi-linux-preflight/SKILL.md`，不入库）。没有该 skill 的检出（含社区贡献者）无法运行远程预检，以 CI 的 `Mobile capture quality` 为准。本机容器预检入口（`--local`）已退役并返回非零，不要自动安装或恢复本机容器环境。

`pnpm check:linux:prepare` 仅打包源码快照，不是通过凭证；之后 Agent 必须按本机 skill 完成远程执行并取回完整证据，再运行 `pnpm check:linux:verify --run <本机运行目录>`。`check:linux` 强制新检查；`check:linux:reuse` 优先核验已有完整证据，缺少结果时返回非零；绝不把 prepare 当作通过。不得读取宿主凭据或挂载生产数据库。


Windows Agent 修改 apps/capture-relay、apps/capture-web、apps/capture-harmony/entry/src/main/ets/core、packages/shared、根依赖/锁文件或 scripts/wslc 后，交付前必须在仓库根目录运行 pnpm check:linux。命令检查当前源码快照，在独立 Node 24/Linux 容器内执行共享包/收集网页/中转服务类型检查、中转服务全量单测、两端构建，以及真实中转进程的健康、静态网页和 SQLite 重启持久性检查。源码变化后须重跑，报告结果指纹和证据目录。

输入只读挂载，依赖及构建放在容器副本内；不得挂载用户数据、手机配对凭据、宿主 .env 或 Windows node_modules，不得停掉或重启用户实例。该门禁验证 Linux 收集服务，桌面端仍执行原有 Windows 隔离验证，HarmonyOS 仍需原有构建/设备验收；不改变官方桌面平台政策，不替代浏览器/E2E、镜像构建或生产发布。远程构建器不可用或失败必须如实报告，不能把入口存在当成预检通过；Linux CI 使用原生检查。

修改预检工具 scripts/wslc 时，还必须先运行 pnpm test:wslc 验证源码快照、凭据排除及原 Git 暂存区保留。
