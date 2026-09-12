# 归知桌面开发参考

从本工作副本的原 AGENTS.md 提取，按任务查阅对应章节。源码路径相对于仓库根目录；过去的测量和平台结论不代替当前验证。

## 技术栈

- Electron 33（main / preload / renderer 三进程）
- React 18 + TypeScript 5 + Vite 6
- Zustand 5（渲染进程状态）
- Tailwind CSS 3 + Liquid Glass 玻璃拟态令牌（`renderer/styles/globals.css`）
- node-sqlite3-wasm（主进程 SQLite，FTS5；选 wasm 是因为原生模块在 Windows ARM64 上构建不过）
- i18next（zh / en）
- pnpm monorepo：`apps/desktop` + `packages/{core,db,shared}`

## 架构约定

```text
apps/desktop/src/
├── main/        # Electron 主进程：窗口/托盘/快捷键、SQLite、IPC handlers、更新器
│   ├── database/  # @guizhi/db 的桌面包装（路径解析）
│   ├── ipc/       # IPC handlers（settings/security/image/ai + M1 起的知识域）
│   ├── services/  # ai-client、net-safety(SSRF)、network-proxy
│   └── settings/  # 主进程侧设置读取
├── preload/     # contextBridge 白名单：window.api（settings/ai/security）+ window.electron（壳能力）
└── renderer/    # React UI
    ├── components/{app,layout,settings,ui}/
    ├── stores/    # ui.store（AppModule 导航）、settings.store（外观/AI/同步配置）
    ├── services/  # ai.ts（AI HTTP 经主进程代理）
    └── i18n/      # zh.json / en.json
packages/
├── core/        # AI 配置文件服务（config/ai-models.json）、AI 客户端、运行时路径
├── db/          # SQLite 适配器、schema、初始化与进程锁
└── shared/      # 跨进程类型、IPC 频道常量、ai-protocol/network-proxy 工具
```

- 导航：无 react-router。`App.tsx` 的 `PageType`（home/settings）+
  `ui.store` 的 `AppModule`（library/ask/wiki/imports）双层切换。
- IPC：频道常量集中在 `packages/shared/constants/ipc-channels.ts`；
  main 注册于 `main/ipc/index.ts`；preload 白名单暴露。
- 数据：`%APPDATA%/GuiZhi/data/knowledge.db`。知识域表（knowledge_items、
  collections、tags、import_tasks、wiki_*、FTS5 虚表）在 M1 加入
  `packages/db/src/schema.ts`。旧 .NET 版的 `data/guizhi.db` 是只读迁移源。
- 主题：FONT_SIZES（3 档）/ MORANDI_THEMES（6 套）/ 背景图令牌在
  `stores/settings/settings-appearance.ts` 与 `styles/globals.css`。
  不要绕过语义令牌硬编码颜色。
- AI：Provider/模型/路由配置持久化在 `config/ai-models.json`
  （`packages/core/src/ai-config.ts`）；渲染进程经 `ai:httpRequest`/
  `ai:httpStream` IPC 走主进程发起请求（绕过 CORS、支持代理）。
 模型路由：mainText（问答/Wiki/视频总结）、fastText（摘要/标签）、
 visionText（OCR）、embedding（语义检索）、audioText（语音转写）。

## 常用命令

```bash
pnpm electron:dev        # 开发（Vite HMR + Electron）
pnpm typecheck           # TS 全量检查
pnpm test:unit           # vitest 单测
pnpm lint                # eslint + 文件行数门禁
pnpm shot                # 界面截图（窗口在屏幕外，不打扰用户）
pnpm build:isolated      # 在当前源码副本中构建，不覆盖开发目录产物
pnpm test:e2e:smoke      # 根目录入口自动隔离构建与 E2E
pnpm electron:build:win  # Windows 打包
```

### 界面截图不要打扰用户

改了 UI 想看效果时走 `pnpm shot`（`apps/desktop/scripts/screenshot.mjs`），
不要手写一次性的启动脚本，也不要让用户自己去开应用截图。它拉起真实 Electron、
截图、退出，窗口不出现在屏幕上也不抢焦点——这些脚本经常在用户
正干活时被跑起来，弹出来的窗口会把用户手上的东西整个顶掉。数据目录是一次性
临时目录，碰不到用户的库，也不与用户正开着的归知抢单实例门（`GUIZHI_E2E=1`
本来就绕过它）。默认截首屏；`--steps <file>` 传一个默认导出
`async ({ win, app, shot }) => {}` 的模块，就能先点到目标界面再截。

**窗口是「挪到屏幕外」而不是「隐藏」，这条改不得**（查过社区做法了，别再查
一遍）。`main/testing/window-mode.ts` 的做法是保持 `visible`、把窗口挪到全体
显示器边界之外、`setSkipTaskbar` + `setOpacity(0)`、并用 `showInactive()` 显示。
实测这样截出来的首屏与正常显示时**逐字节相同**（同一 sha256），渲染路径没有
差异；系统前台窗口自始至终不变，焦点不易主。透明度归零是第二道保险，防的是
Windows 在 DPI 变化或显示器热插拔时把屏幕外的窗口拽回可见区域。

社区主流方案是另一套，且**在这里不适用**——不了解这一点的人迟早会拿它来
「修正」这段代码。Electron 没有 headless 模式（Playwright 团队在
microsoft/playwright#16851 里的原话是 "it should be always visible"），
issue #13288 下被反复引用的解法是在 `ready-to-show` 里直接 `return`、
根本不调用 `show()`。它对**纯 DOM 断言**的 e2e 完全够用：Playwright 的
`click` / `waitFor` / `fill` 走 CDP 与 DOM，不需要合成帧——实测那种状态下
点击 239ms 正常完成。但截图需要一帧新的合成结果，而隐藏窗口的合成器是停摆的：
实测 `requestAnimationFrame` 两秒只推进 **2** 帧（正常是 323 帧），
`page.screenshot()` 三次里两次 15 秒超时、一次侥幸 1173ms 返回。
`setBackgroundThrottling(false)` 救不了（Electron 文档说它能让 visibility
state 保持 visible，但实测 rAF 仍是 2 帧、截图仍是两超时一成功）。
所以「不 show」与「截图」不可兼得，我们要的是后者。
另一种失败形态别混为一谈：`show()` 之后再 `hide()` 比从未 show 更糟，
连 `locator.click()` 都会超时（实测 20s / 10s 双双失败）。
真正的 headless 只有 Linux CI 上的 Xvfb，Windows 上没有对应物；VS Code 的
Electron 冒烟测试干脆就让窗口正常显示，只在 README 里叮嘱测试别依赖焦点态。
「屏幕外 + showInactive」也不是我们独创：`EchoTechFE/dimina-kit` 的
electron-deck 为了截图走的是同一套（`setPosition(-3000, -3000)` +
`showInactive()`），注释写的就是 *forces a real paint ... WITHOUT focus*。
我们只是把固定坐标换成按显示器边界算，多屏布局下才不会露出来。

截图默认带 `animations: "disabled"` + `caret: "hide"`：不加的话，界面就绪后
立刻截的**第一张**会抓到淡入过渡的中间态——实测同一界面连截三张，默认选项下
首张与后两张不一致，加上之后三张逐字节相同，耗时不变。

将来做 macOS 时这套要重新验证：`showInactive()` 在那边虽不给焦点，却硬编码
把窗口带到前台（electron#49393，2026-01 仍未合入可选项），全屏状态下还会
触发切换桌面（electron#24703）。窗口在屏幕外时未必看得出来，但别假设它同样安静。

`GUIZHI_WINDOW_MODE` 两个方向都能显式指定：`visible` 让 e2e 恢复可见（人工
盯着跑时用，`pnpm shot --visible` 就是它），`offscreen` 反过来可用于
`electron:dev`。E2E 同时跳过全局快捷键注册——自动化实例与用户正在运行的归知
并存，抢注 `Alt+Shift+P` 会把用户那个夺走，而注册失败只打一条 warn，两边都
发现不了。

### 验证实例与开发实例完全分开

`pnpm shot` 默认经 `scripts/isolated-desktop.mjs` 创建系统临时目录中的源码副本，
包含 Git 跟踪文件的当前内容，以及未忽略的新源码；已删除文件不会复活，不会 stash、
reset 或覆盖原工作区。副本中构建 main、preload、renderer、MCP，随后启动真实
Electron。每次验证使用不同目录，结束后清理；多次验证不会共用 `out/`。

依赖包通过目录链接复用现有安装，不运行 install/rebuild。`node_modules` 自身和
Vite 缓存目录各自独立，`@guizhi/*` 指向副本内源码。环境文件不复制，也不继承开发
服务 URL、用户数据目录和 Electron 的 Node 模式。截图仍使用临时 userData、随机
回环端口与屏幕外窗口。原目录的产物不参与验证，`--stale-ok` 不会省略隔离构建。
代价是每次截图都需等待一次完整构建；构建失败则退出，不回退到旧产物。

根目录 `pnpm test:e2e` / `pnpm test:e2e:smoke` 使用同一入口；测试报告保留在
`apps/desktop/.tmp-e2e-*`。仅检查能否构建时用 `pnpm build:isolated`，其临时产物
验证后即删除，不用于打包。子包的 build/e2e 和打包命令仍会原地写入产物，只能在
没有用户开发实例的独立工作副本中运行。

截图 `--steps`、`--data-db`、`--out` 的相对路径仍按调用目录解析；步骤脚本读取
原有 fixture 的路径保持兼容，Electron 的 cwd 则明确指向源码副本。步骤回调新增
`mainEntry`：需要重启验证应用时必须使用这个绝对入口，不得再拼原目录的 `out/`。
`--executable` 验收指定安装包时直接使用候选程序，仍隔离数据和窗口，不做源码构建。
fixture 脚本自身的外部读写仍由脚本负责，不能借步骤脚本操作用户实例或用户库。

### 并行写代码必须隔离源码

离屏只控制新拉起的验证窗口，无法阻止用户的 Vite 响应源码变化。main 和其导入的
`packages/{shared,db,core}` 变动会使开发 Electron 重启；preload 会刷新页面，
renderer 的 HMR 也可能改变用户正在操作的界面。修改 Vite 配置或依赖同样可能重启服务。

开始编辑前先检查当前目录是否有开发服务。用户一边使用开发版、代理一边工作时，
代理必须在独立 worktree 或工作副本里编辑和验证，保留原目录所有未提交改动；
任务依赖未提交功能时，工作副本也要包含这些改动，不能只从 HEAD 验证。
验证结束先交付可审阅差异，等用户切换版本时再回写原目录。不要把“每个文件一次写完”
当作隔离，也不要为了采用新流程而主动重启用户已经运行的服务。

清理只针对本次创建的进程和临时目录。禁止按 `electron.exe` / `node.exe` 名称批量
结束进程，也不要停止、复用用户的 Vite 服务。截图实例的日志在临时 userData 中；
用户 `startup.log` 新增的启动记录不能解释成离屏验证启动。

## 编码约定

- TreatWarnings 严格：eslint `--max-warnings 0`；文件行数上限见
  `config/file-line-limit-baseline.json`（`pnpm lint:file-size`）。
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
- 后台自动执行的任务可以不弹提示，但必须留痕：`main/diagnostic-log.ts` 的
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
- 远程抓取必须走 `main/services/net-safety.ts` 的 SSRF 防护。
- 机密（API Key、`networkProxy.password`）只允许出现在两个位置：渲染进程的
 localStorage `guizhi-settings`，与主进程的 `config/ai-models.json`——它们都在
 用户自己的数据目录里。**离开数据目录的机密必须加密**。
 唯一带得走机密的功能是配置迁移（`config:export`），它用一次性的传输密码
 派生密钥、只加密机密字段；除它之外，导出一律不得携带机密：Markdown 导出只碰
 条目正文，`.db` 备份不含 AI 配置（Key 不在 SQLite 里），两者都不要为了「方便」
 破这条。日志同理，`scrubMessage` 只抹主目录，别把 Key 写进报错原文。

手机收集桌面凭证例外：仅主进程 safeStorage 加密保存于 `.machine/mobile-capture.json`；不通过 IPC 返回，不加入备份、导出或目录迁移白名单。Linux basic_text 或安全存储不可用时仅保存在当前进程内存，禁止明文回落。
