# 归知桌面开发参考

桌面端是 Go + WebView2 外壳（`apps/desktop-go`，基于 [MyGo](https://github.com/egoist/mygo)）。Electron 主进程已于 2026-11-13 退役；
需要追溯旧实现时见 [Electron 时代开发参考](./electron-era-development.md)（历史资料）。源码路径相对于仓库根目录；
过去的测量和平台结论不代替当前验证。

## 技术栈

- Go（模块 `guizhi-mygo-shell`）+ MyGo v0.8.3 + WebView2；官方仅支持 Windows 11+ x64
- 界面：React 18 + TypeScript 5 + Vite 6 + Zustand 5 + Tailwind CSS 3 + i18next（zh / en），源码在 `apps/desktop/src/renderer`
- SQLite：MyGo 提供的原生库（`mygo-sqlite3.dll`，随安装包发布），schema 与迁移来自 `packages/db`
- goja：在 Go 里运行由 TS 生成的 JS 产物（主题阅读管线、快照净化、渲染脚本），源码在 `apps/desktop-go/jsrc`
- pnpm monorepo：`apps/desktop`（界面与预加载源码）、`apps/desktop-go`（Go 壳与构建链）、`packages/{core,db,shared}`

## 架构约定

```text
apps/desktop/src/
├── preload/     # contextBridge 白名单：window.api.* 与 window.electron（壳能力）
└── renderer/    # React UI：components、stores、services、i18n
apps/desktop-go/
├── *.go             # 窗口/托盘/快捷键、SQLite、IPC 处理器、采集、AI 代理、备份、更新器
├── shim/            # `electron` 模块的替身：把 preload 的 invoke/send/on 接到 Go（见下）
├── jsrc/ + jsgen/   # goja 产物的 TS 源码与生成脚本（产物入库，CI 校验一致）
├── vite.*.config.ts # 渲染进程与 preload 的构建配置
├── installer/       # NSIS 安装器脚本与图标
├── scripts/         # build-shell、build-installer、shot（界面截图）及其步骤脚本
└── testdata/golden/ # 冻结的差分测试基准（来自 Electron/TS 终版）
packages/
├── core/        # AI 配置文件服务（config/ai-models.json）、AI 客户端、运行时路径
├── db/          # SQLite schema、迁移、初始化与进程锁
└── shared/      # 跨进程类型、IPC 频道常量、协议工具
```

- 导航：无 react-router。`App.tsx` 的 `PageType`（home/settings）+ `ui.store` 的 `AppModule`（library/ask/wiki/imports）双层切换。
- IPC 链路：渲染进程 → preload（`vite.preload.config.ts` 打成一个 IIFE，由 MyGo 作为 PreloadScript 注入）→
  `shim/electron.ts`（把 `electron` 模块别名到自身，`invoke`/`send` 统一调 Go 绑定 `Bridge.Invoke`/`Bridge.Send`，
  Go 推送的事件走 `ipc` 事件再按频道分发）→ Go 处理器。频道常量集中在 `packages/shared/constants/ipc-channels.ts`。
- 频道清单 `apps/desktop-go/ipc_channels_gen.go` 由 `jsgen/gen-channels.mts` 扫描 preload 生成；
  发版冒烟会检查「全部频道均有处理器」，新增频道必须同时补 Go 处理器，否则该检查失败。
- 类型检查时 `tsconfig` 把 `electron` 映射到 `shim/electron.ts`：preload 用到 shim 没有的 API（例如 `ipcRenderer.once`）会在
  `pnpm typecheck` 里直接报错，而不是等到运行时。
- 数据：用户数据目录下 `data/knowledge.db`（默认即 `%APPDATA%\GuiZhi`）；旧 .NET 版的 `data/guizhi.db` 是只读迁移源。
- 主题：FONT_SIZES（3 档）/ MORANDI_THEMES（6 套）/ 背景图令牌在 `stores/settings/settings-appearance.ts` 与 `styles/globals.css`，
  不要绕过语义令牌硬编码颜色。
- AI：Provider/模型/路由配置持久化在 `config/ai-models.json`（`packages/core/src/ai-config.ts`，Go 侧同格式读写）；
  渲染进程经 `ai:httpRequest`/`ai:httpStream` IPC 走 Go 发起请求（绕过 CORS、支持代理）。
  模型路由：mainText（问答/Wiki/视频总结）、fastText（摘要/标签）、visionText（OCR）、embedding（语义检索）、audioText（语音转写）。
- 远程抓取走 `apps/desktop-go/net_safety.go` / `safe_fetch.go` 的 SSRF 防护；网页采集的实现笔记见历史资料
  （[Electron 时代开发参考](./electron-era-development.md)）与 [网页采集](../web-capture.md)。
- goja 产物：`jsrc` 里的 TS 经 `pnpm --filter @guizhi/desktop-go gen` 生成 `themed_engine.js`、`themed_pipeline.js`、
  `websnap_sanitize.js`、`render_scripts_gen.go`。**产物入库**，CI 会重新生成并要求与入库版本逐字节一致：
  改了 `jsrc` 或 `jsgen` 后必须重新生成并一起提交。`jsrc` 的单测用 `pnpm --filter @guizhi/desktop-go test` 运行；
  它没有独立的类型检查（部分模块在构建时被替换，详见 `apps/desktop-go/jsrc/README.md`）。
- 差分测试：`testdata/golden/` 是来自 Electron/TS 终版的冻结基准，Go 测试默认读取它，不需要 TS 主进程。基准不可重新生成于主线，
  细节见 `testdata/golden/README.md`。

## 常用命令

```bash
pnpm typecheck           # 全部工作区类型检查
pnpm test:unit           # 校验脚本 + 渲染层 vitest + jsrc（goja 产物输入）单测
pnpm lint                # 文件行数门禁 + i18n + eslint
pnpm shot                # 界面截图（隔离构建 Go 壳 + 离屏启动 + CDP），含渲染层包体预算检查
pnpm build               # 原位构建 Go 壳到 apps/desktop-go/out/win-x64（需要 pwsh 7）
pnpm build:installer     # 在上一步产物上打 NSIS 安装包与 latest.yml（apps/desktop-go/out/installer）
pnpm verify:release      # 发版前验证组合：lint、类型检查、单测、Go 壳隔离构建 + 界面冒烟
```

Go 侧在 `apps/desktop-go` 下运行 `go vet ./...` 与 `go test -count=1 ./...`（全量约 2 分钟，会拉起真实 WebView2 隐藏窗口）；
测试依赖 `pnpm build` 产出的 `dist/` 与 `reading_libraries.json`，须先构建。

没有 Vite 热更新：改完界面源码后，用 `pnpm shot` 看效果，或 `pnpm build` 后运行 `apps/desktop-go/out/win-x64/GuiZhi.exe`。
**不带 `-db` / `-userdata` 直接运行会解析到 `%APPDATA%\GuiZhi`，即用户的真实数据**（见 `startup_paths.go`）；试跑请加 `-userdata <临时目录>`。

### 界面截图（`pnpm shot`）

改了 UI 想看效果时走 `pnpm shot`，不要手写一次性的启动脚本，也不要让用户自己去开应用截图：它在屏幕外拉起一个独立实例，
不打扰用户、不抢焦点、不碰用户的库。

`scripts/isolated-go.mjs` 把当前源码（含未提交改动）复制到系统临时目录，在副本里构建渲染进程、preload、阅读库和 Go 壳
（约 30 秒；构建步骤里包含渲染层包体预算检查，超预算即中止），再调用 `apps/desktop-go/scripts/shot.mjs` 离屏启动并截图。
副本里的 `dist/`、`out-go/` 与开发目录无关；开发目录的产物、数据目录和正在运行的实例都不参与。不得用旧产物验证新改动。

```bash
pnpm shot                                                          # 首页截一张
pnpm shot --steps apps/desktop-go/scripts/steps/smoke-steps.mjs    # 走到指定界面再截
pnpm shot --data-db path/knowledge.db --out ./shots --keep-profile
```

原理：以 `-bench` 方式启动（自动化实例：窗口在屏幕外，不占单实例锁、不建托盘、不抢全局快捷键），环境变量
`WEBVIEW2_USER_DATA_FOLDER` 指向一次性目录，`WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=0`
打开随机 CDP 端口，端口从 `<userData>/webview2/EBWebView/DevToolsActivePort` 读取（两行写全才有效），
Playwright `connectOverCDP` 接管页面；就绪判据是 `topbar-search` 出现。

**窗口是「挪到屏幕外」而不是「隐藏」，这条改不得**：隐藏窗口的合成器会停摆，截图拿不到新帧；
屏幕外 + `showInactive()` 既能正常出帧又不抢焦点。截图默认带 `animations: "disabled"` + `caret: "hide"`，
否则界面就绪后立刻截的第一张会抓到淡入过渡的中间态。

步骤脚本的参数是 `{ win, shot, outDir, userDataDir }`，`win` 是 Playwright 的 Page。已有步骤脚本在
`apps/desktop-go/scripts/steps/`：`smoke-steps.mjs`（CI 冒烟）、`content-panel-switch-shots.mjs`、`library-count-shots.mjs`。
限制：

- **没有 `app`**（Electron 主进程对象）。需要替换后端行为时，在页面里拦截 `window.electron.ipcRenderer.invoke`；
  访问了 `app` 会得到说明性错误。
- `shot()` 只截宿主页面：阅读子视图是独立的 WebView2，不会被合成进截图。
- 退出时直接结束本次启动的进程树，不等待优雅关闭；临时目录随后删除（`--keep-profile` 保留）。
- 单独手动运行 `apps/desktop-go/scripts/*.ps1` 必须用 `pwsh`：脚本含中文且无 BOM，Windows PowerShell 5.1 会解析失败。

### 验证实例与用户实例完全分开

`pnpm shot` 的源码副本包含 Git 跟踪文件的当前内容，以及未忽略的新源码；已删除文件不会复活，不会 stash、reset 或覆盖原工作区。
依赖包通过目录链接复用现有安装，不运行 install/rebuild；每次验证使用不同目录，结束后清理。
`createDesktopSnapshot` 用 `fs.realpathSync.native` 取临时目录：Windows 的 8.3 短路径会让 Vite 解析 HTML 模块失败。
`--steps`、`--data-db`、`--out` 的相对路径按调用目录解析。

### 协作与进程约束

- 用户可能正在运行归知（安装版或自行构建的 `GuiZhi.exe`）。验证不得停止、重启或复用它，不得按 `GuiZhi.exe` / `node.exe` 名称批量结束进程，
  只清理本次启动的进程（按 PID）和临时目录。
- 不要原位覆盖用户正在运行的 `apps/desktop-go/out/win-x64/GuiZhi.exe`（被占用会构建失败，更糟的是悄悄换掉他在用的版本）；
  构建检查用 `pnpm shot`，打包等放在独立 worktree。
- 并行开发时，源码修改与验证放在独立 worktree，保留原目录的未提交改动；完成后先交付差异，待用户切换版本时再回写原目录。

### 安装包与发版

- `pnpm build` → `apps/desktop-go/scripts/build-shell.ps1`（渲染进程与 preload、阅读库、Go 壳、`mygo-sqlite3.dll`）；
  `pnpm build:installer` → `build-installer.ps1`：用 `electron-builder --prepackaged` 把上一步产物打成 NSIS 安装包并生成 `latest.yml`。
  这里 electron-builder 只是安装包工具，不含 Electron；配置在 `apps/desktop-go/electron-builder.mygo.cjs`，自包含。
- **不要改 `appId`、`productName`、`nsis.guid`**：同一个 guid 让新版本作为同一产品的「升级」覆盖安装旧版本，改了老用户无法原地升级。
- 版本号唯一来源是 `apps/desktop/package.json`（`build-shell.ps1`、安装包配置、`release.yml` 的 tag 校验都读它）；
  `apps/desktop-go/package.json` 的 `0.0.0` 不是发布版本。
- `electron-builder.mygo.cjs` 里的 `electronVersion` 只是满足 electron-builder 版本检查的占位值，不会下载 Electron。
- 发版由 `v*` tag 触发 `.github/workflows/release.yml`：构建 → 渲染层包体预算（`apps/desktop/bundle-budget.json`）→ 安装包 →
  安装包体积预算（`config/package-size-budget.json`）→ 离屏启动冒烟 → 校验更新清单 → Draft Release。
  安装包未做代码签名；安装需要系统带有 WebView2 运行时。
- 人工验收脚本与清单在 `apps/desktop-go/verify/`（沙箱安装/升级、泄漏、大库、损坏库等），属手动项，不进 CI。

### 阅读组件编译内存

阅读图形库由 `scripts/reading-libraries-worker.mjs` 在独立 Node 子进程内编译，
父进程只接收最终脚本，等待子进程退出后再输出资源。不要把多次图形库构建合回长期运行的 Vite 进程；
这会把编译堆保留在开发服务中。编译失败必须中断构建，重试时重新启动子进程；正文库仍按需读取，不改变应用的运行时加载策略。

## 编码约定

- TreatWarnings 严格：eslint `--max-warnings 0`；文件行数上限见
  `config/file-line-limit-baseline.json`（`pnpm lint:file-size`）。
- 界面文案写成 `t("键", "中文兜底")` 并同时补齐 `zh.json` / `en.json`；`pnpm lint:i18n`
  检查缺失键、无人引用的键、英文漏译，以及写死中文不超过
  `config/i18n-hardcoded-baseline.json`（冻结功能暂留在基线里，只减不增）。
  非组件模块用 `i18n.t`；`--report` 列出每一处。
- 生成的代码注释与日志用中文（可中英双语），标识符用英文。
- 渲染进程不得直接访问 Node API；一切系统能力经 preload 白名单。
- 界面里不出现原生控件。`<select>` 的下拉列表、`<input type="color">`、
  `alert/confirm/prompt` 都由操作系统绘制，CSS 完全够不着，一律换成
  `components/ui` 下的 `Select` / `ColorPicker` / `ConfirmDialog`；eslint 的
  `no-alert` 与 `no-restricted-syntax` 会挡住回潮。滑块、数字步进箭头、搜索框
  清除叉这些浏览器自绘的默认外观在 `globals.css` 统一收敛。`title` 提示由
  `ui/TooltipLayer` 全局接管（悬停时摘走 `title` 自绘、移开再装回），
  所以写 `title` 无需改动即可获得主题化气泡。
- `title` 必须给出元素自己没写出来的信息。纯图标控件该写（气泡是它唯一的名字），
  但图标本身已是通用词汇的除外——侧栏底部那颗齿轮挂「设置」等于把图标念一遍，
  可访问名交给 `aria-label` 就够了；气泡内容与可见文字不同的该写（绝对时间对
  相对时间、URL 对显示名、置灰原因、快捷键）。而 `title={x}` 配 `{x}` 这种把同一句话再念一遍的一律不写——即便
  文字被 `truncate` 截断也不写，完整内容靠点开条目/加宽列去看，不靠悬停。
  同一控件在不同形态下结论不同时写成条件式（`collapsed ? label : undefined`），
  不要图省事一律挂上。点击会打开菜单或弹窗的按钮尤其别挂无谓的 `title`。
  eslint 的 `guizhi/no-redundant-title`（`apps/desktop/eslint-rules/`）会挡住
  原生标签上的回潮；组件（`ui/Input` 这类透传 title 的）它查不到，仍需人工把关。
- 用户主动点击触发的操作，失败时必须给得出原因。`showToast` 的第三个参数是
 `{ detail }`：提示语保持一句话，完整报错折叠在「查看详情」里，可展开、可复制。
 批量操作尤其要用——概要说「N 成功 M 失败」，逐条原因进 detail，
 光给个计数用户无从判断该不该重试。后台自动执行的任务可以静默。
 反面教材：快捷键保存曾经忽略主进程返回的 `false`，失败了却弹绿色的「已保存」。
- store 里的变更方法几乎都被 `void mutate(...)` 调用，向外抛就是一条无人处理的
 rejection：界面毫无反应，用户以为改成功了。一律用
 `stores/operation-error.store.ts` 的 `runGuardedMutation` 包起来，失败投进错误
 通道，由挂在 `App` 里的 `useOperationErrorToast` 统一提示。它返回 boolean，
 「成功后还要弹提示」的调用方必须判一下（`moveToTrash` 就是为此改的签名，
 否则删失败还会补一句「已移到回收站」）。批量循环不要因为一条失败就 break，
 逐条记账、结束后一次报出来。
- IPC 的失败别用裸 boolean 表达。`{ success, error }` 才带得回原因；
 Wiki 页面保存/删除/回滚与备份删除都是从 boolean 改过来的。
 注意本仓库 `strict: false`，判别式联合的 narrowing **不生效**，
 所以结果类型一律写成 `{ ok/success: boolean; error?: string }` 而不是
 `{ ok: true } | { ok: false; error: string }`。
- 后台自动执行的任务可以不弹提示，但必须留痕：`apps/desktop-go/app_runtime.go` 的
 `logAppError` 写 `<userData>/logs/error.log`，渲染进程经 `log:appError`
 汇入同一个文件，设置页「数据」里的「打开日志」是它唯一的出口。
 定时备份、后台 Wiki 编译、store 的变更失败与 ErrorBoundary 都已接入。
 写进日志的 message 会过 `scrubMessage` 抹掉主目录——日志是会被用户
 发出来求助的东西，别把用户名带出去。
- 加载失败不要渲染成空态。「读不出来」和「真的没有」在界面上长得一样，
 用户看到「暂无条目」不会去重试。相关 store 记 `loadError`
 （`stores/load-error.ts` 的 `describeLoadError`），列表区改用
 `ui/LoadErrorState` 渲染原因 + 重试。Wiki 搜索失败尤其要分开——
 它此前显示成「没有匹配的页面」，是最误导的一处。
- 远程抓取必须走 `apps/desktop-go/net_safety.go`（含 `safe_fetch.go`）的 SSRF 防护。
- 机密（API Key、`networkProxy.password`）只允许出现在两个位置：渲染进程的
 localStorage `guizhi-settings`，与 Go 壳读写的 `config/ai-models.json`——它们都在
 用户自己的数据目录里。**离开数据目录的机密必须加密**。
 唯一带得走机密的功能是配置迁移（`config:export`），它用一次性的传输密码
 派生密钥、只加密机密字段；除它之外，导出一律不得携带机密：Markdown 导出只碰
 条目正文，`.db` 备份不含 AI 配置（Key 不在 SQLite 里），两者都不要为了「方便」
 破这条。日志同理，`scrubMessage` 只抹主目录，别把 Key 写进报错原文。

手机收集已从桌面端移除（见 [产品边界](../product-scope.md)），桌面端不再保存手机收集凭证，原先针对它的 safeStorage 规则不再适用；`apps/capture-*` 服务端与客户端源码仍保留，它们的约束见根 `AGENTS.md`。
