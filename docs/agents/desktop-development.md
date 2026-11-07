# 归知桌面开发参考

从本工作副本的原 AGENTS.md 提取，按任务查阅对应章节。源码路径相对于仓库根目录；过去的测量和平台结论不代替当前验证。

## 技术栈

- Electron 45（main / preload / renderer 三进程；Chromium 156、内置 Node 24）
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
- 网页采集耗时诊断：设置环境变量 `GUIZHI_WEB_TRACE_FILE=<绝对路径>` 后，每次
  `captureWebPage` 向该文件追加一行 JSON（`services/web-capture/web-trace.ts`）：
  各阶段耗时、出口请求数与新建/复用连接数、静态路由判定；只含 origin 与网址哈希，
  默认关闭。单次采集内的代理连接由 `web-connection-pool.ts` 持有并在采集结束时销毁，
  `GUIZHI_WEB_CONNECTION_REUSE=0` 可关闭复用用于排查。前后对照用
  `scripts/web-trace-steps.mjs`（经 `pnpm shot` 运行，本机代理地址由
  `GUIZHI_TEST_PROXY` 提供，默认见 `scripts/lib/test-proxy.mjs`）。
  渲染后的快照会把开放 shadow root 的内容展开进快照副本（`web-rendered-document.ts`）；
  闭合 root 不可见，shadow 内的 CodeMirror 编辑器不做还原。
- 正文提取（`web-extract-article.ts` + `web-extract-markdown.ts`）在独立的 worker 线程里运行
  （`web-extract-worker.ts`；主进程侧 `web-node-extractor.ts`）：linkedom 解析、Readability 取正文、
  turndown 转 Markdown。线程内禁用全部网络 API，串行处理，单页 45 s 超时，输入与输出各 16 MiB、
  堆 1 GiB 上限，空闲 60 s 退出。生产构建把它打成只引用 Node 内置模块的单文件
  `out/web-extract-worker/web-extract-worker.js`，主进程读出源码后以 `eval` 方式启动（asar 内的文件
  不能直接作为 worker 入口）。`engineVersion` 为 `readability/1-<static|electron>`：规则或依赖升级
  使同一页面的正文输出变化时提升版本号，重复导入的“检查更新”据此只保存待采用版本、不改写正文
  （比较时去掉路由后缀，只看 `readability/1` 这一段）。
  规则、回归口径与已知差异见 [网页采集](../web-capture.md)。
- 渲染页加载后的沉降等待（`web-settle.ts`）：事件驱动，替代固定的 1.2 s + 3×(滚动 + 0.2 s)。
  网络、DOM（MutationObserver）与可见文字都静止约 300 ms 即结束；可见文字不足 200 字的空壳页
  等满。各段上限之和等于旧的固定等待，命中上限时与旧行为等价；首段上限从沉降开始（安装探针之前）
  计时，因为探针脚本要排在页面刚加载完的水合任务后面（Next.js 站点实测排队 0.3–0.4 s），这段排队
  不计入就会比旧版更久。网络信号只统计“可能改动文档”的在途请求（`web-network-activity.ts`）：
  样式表、图片，以及文档类请求（`<link rel=prefetch>`、子框架）不计入。结果写入追踪的 `settle`：
  `reason`（`quiet|cap|shell|fixed|blind`）、首段用时、各信号的阻塞轮数 `blocked`、命中上限时仍在途
  的请求 `last.blockers`（类别、origin、已等待毫秒），用来回答“为什么没有提前结束”；
  `maxPollMs`（一次读取的最长往返）与 `maxCostMs`（页面侧 `innerText` 的最长计算）用来区分
  “页面主线程忙”和“轮询本身太重”。`GUIZHI_WEB_SETTLE=fixed` 回退到固定等待。
- 渲染窗口的请求类别取自 `Accept`（`web-trace.ts` 的 `classifyAccept`）：`protocol.handle`
  接管 http(s) 后，处理器里的 `request.destination` 恒为空，也没有 `Sec-Fetch-*`（Electron 45 实测，
  33 相同）。Electron 45 上 `webRequest` 对这些被接管的请求也会触发并带 `resourceType`（33 不触发），
  目前没有用它分类。Chromium 的导航、子框架与 `<link rel=prefetch>` 以 `text/html,application/xhtml+xml`
  开头（逗号后无空格；Chromium 156 的预取另带 `Sec-Purpose: prefetch`）；jQuery 一类取 HTML 片段的 XHR 是
  `text/html, */*; q=0.01`（有空格），它会改动 DOM，必须计入。改动分类规则后用
  `scripts/web-settle-fixtures-steps.mjs` 的本机夹具回归（经 `pnpm shot` 运行，
  `GUIZHI_FIXTURE_ONLY=ajaxHtml,prefetchHang` 只跑指定夹具）。
- 沉降等待的已知限制：既不发请求也不改 DOM 的纯定时器渲染（实测约 0.6–0.7 s 之后才写入正文的会
  漏）、open shadow root 内部的变化（由网络静止兜底）、间隔超过 300 ms 的请求链，都可能比固定
  等待更早截取；约一半的渲染页仍等到上限，`last.blockers` 里最常见的是第三方 consent/统计脚本
  （OneTrust、微软遥测等）、部分站点的同源接口请求，以及 MDN 兼容表的 XHR：请求头无法区分它们
  会不会改动正文，保守起见一律等待，不按站点写名单。对照与验收：`GUIZHI_TRACE_VARIANT=settle` 配合
  `scripts/web-trace-steps.mjs` 做公网语料 A/B（`GUIZHI_TRACE_CORPUS` 指定语料，
  `GUIZHI_TRACE_SAVE_CONTENT=1` 保存正文用于逐页相似度对比）。
- 渲染窗口的出口与导航防护（`web-electron-renderer.ts`）：http(s) 经 `protocol.handle` 统一走
  `webNetworkRequest`（SSRF 校验、200 个请求 / 50 MiB 预算；预算用尽的错误会写明请求数最多的主机）。
  `webRequest.onBeforeRequest` 在 Electron 45 上对被接管的 http(s) 请求（含主框架导航）也会触发
  （33 只对未被接管的请求触发），所以该监听必须放行 http(s)，只取消 ws/wss 与非 http(s)（实测 WebSocket
  仍会触发）；监听之外还有两层兜底：黑洞代理 `127.0.0.1:9`（配合 `<-loopback>`，连本机也
  不直连）与 WebRTC `disable_non_proxied_udp`。http(s) 的主框架范围检查（目录模式）放在
  `will-frame-navigate` 与 `will-redirect`：它们先于取页面触发，越界的导航不会被请求，也不会执行
  （Electron 45 出口探测复测）；程序化的 `loadURL` 不触发（入口由调用方校验）。同一对事件
  也承担子框架策略：跨源子框架默认不加载（`web-subframe-policy.ts`；采集只用主框架 DOM，跨源
  子框架拖慢 load，且渲染窗口没有跨框架缓存，每个子框架重复下载同一批脚本，会耗尽请求预算），
  `GUIZHI_WEB_SUBFRAMES=allow` 恢复加载；追踪的 `blockedFrames` 记被取消的个数，`domReadyMs`
  记主框架 DOMContentLoaded 的时刻（对照 `ms.load`）。
  改动这些逻辑后用 `scripts/web-egress-probe-steps.mjs`（`pnpm shot --steps`，不依赖公网）回归：
  本机见证端口加夹具代理，断言没有连接绕过出口（ws/wss、WebRTC、`file:`、到本机的 HTTP 类请求），
  越界导航在取页面之前被拦截且报“导航超出允许范围”，跨源子框架不被取走也不拖慢 load；
  加 `GUIZHI_WEB_SUBFRAMES=allow` 运行同一脚本得到“放行”基线。公网语料对照用
  `GUIZHI_TRACE_VARIANT=frames`。
- 静态请求的身份（`web-request-identity.ts`）：静态获取、重定向的每一跳以及 robots.txt / sitemap
  的文本请求，与渲染窗口使用同一个身份——Chromium 自己的 `User-Agent`（含 `GuiZhi/<版本>` 标记，
  自报家门，不冒充搜索引擎或其他爬虫）和导航 `Accept`。此前这些请求不带 UA，Wikipedia 等站点据此
  直接 403（en/zh 条目实测全部失败），同一页面由渲染窗口取却正常；robots.txt 取不到还会让文档站
  批量暂停。不带 `Accept-Language`：渲染窗口经 `protocol.handle` 转发时本来就没有（Electron 45
  实测），补上只会让两条路径因语言协商拿到不同版本；也不带 `Upgrade-Insecure-Requests`（渲染窗口的
  主文档请求会带，这是有意保留的差异：它是能力声明而非身份，静态请求发出去只会让 http 站点多一次
  重定向）。**这不是 403 之后的重试**：
  静态遇到 403/429 仍保持失败状态、不回退到渲染窗口——那等于遇到访问限制后升级手段
  （`docs/web-capture.md`：已经遇到的访问限制不能通过回退绕过；公开样例 `httpbin.org/status/403`
  期望 `restricted`）。`GUIZHI_WEB_IDENTITY=none` 恢复旧行为。回归：
  `scripts/web-identity-probe-steps.mjs`（`pnpm shot --steps`，本机夹具，不依赖公网）断言静态与
  渲染的 UA/Accept 一致、仅凭 UA 放行的站点可采集；公网对照用 `GUIZHI_TRACE_VARIANT=identity`
  （45 个网址 × 2 轮 × 2 条件，只有 Wikipedia 的 4 对由 403 变为成功，其余路由、状态、正文无变化）。
- 渲染窗口里的图片不出网（`web-inert-resources.ts`）：`Accept` 以 `image/` 开头的 GET 子请求（img、
  picture、CSS 背景图、图标）以一张有效的 1×1 透明 GIF 本地应答，不占请求预算与网络槽位。采集结果
  来自渲染后的 DOM，图片在其中只是网址；图片由导入流程按网址另行下载，渲染窗口从不截图（本路径没有
  `capturePage`），窗口里加载的那一份没有下游使用，却会拖住 load。不取消、不报错：`onerror` 处理器
  常把 `src` 改写成占位图，会污染采集到的图片地址；有效图片使 `onload` 照常触发，`src` 保持原样。应答
  带 CORS（回显 `Origin`）与 CORP 头，否则 `<img crossorigin>` 与 COEP 页面会收到 `onerror`。字体、
  媒体、脚本在协议处理器里区分不了（只有 `Accept`），不处理。已知差异：图片塌缩为 1×1，没有标注尺寸
  的图片不再撑高页面，依赖滚动位置的懒加载 / 无限滚动会比真实加载多取几批（`scroll` / `scrollbare`
  夹具，接口延迟 80 ms：每批带文字的信息流 2→4 批、多约 0.2 s；每批只有未标注尺寸图片的最坏情形
  2→21 批、多约 1.1 s，均采集成功——批数受沉降上限约束，随接口往返时间而变）；脚本若按
  `naturalWidth` 判断图片真假会读到 1。`GUIZHI_WEB_INERT_RESOURCES=load` 恢复真实加载；追踪的
  `inertStubs` 记本地应答数。占位图只在渲染采集窗口生效，不影响验证浏览器和平台连接器（它们自成
  一套窗口）。公网对照（`GUIZHI_TRACE_VARIANT=inert`，53 个网址 × 2 轮 × 2 条件）：图片占渲染页请求的
  22%–49%、字节的 9%–34%；超出 200 个请求预算而失败的图片密集页（theverge.com、smzdm.com，两轮
  皆然）全部变为成功；主框架 DOMContentLoaded 到 load 的间隔 P90 由 2.4–2.8 s 降到 1.4–1.8 s。
  采集总时长按配对计：一般语料（66 对）中位数 −73 ms（−1.7%），快 300 ms 以上的配对占 41%、慢
  300 ms 以上的占 35%，在噪声内（load 由最慢的第三方子资源决定，同一页面两轮能差 2–3 s）；图片
  密集语料（22 对）中位数 −434 ms（−13%），59% 的配对快 300 ms 以上、14% 慢。正文无超出同条件
  重复噪声的差异（个别页面的推荐流每次加载本就不同）。回归：
  `scripts/web-inert-probe-steps.mjs`；沉降夹具与出口探测需要真实的在途图片，脚本里已固定
  `GUIZHI_WEB_INERT_RESOURCES=load`。
- 评估过但不做：主框架 DOMContentLoaded 时就开始沉降观察（与 load 重叠）。网络静止窗口的计时本来
  就跨越 load（`web-network-activity.ts` 的时钟是连续的），重叠只能提前 DOM 与可见文字这两个从沉降
  开始才计时的窗口；100 次渲染采集里只有 12 次不被网络窗口卡住，据 `settle.blocked` 推算最多省
  约 28 ms/次（占采集总时长 0.5%）。把网络窗口也算上的朴素上限 276 ms/次不成立。
  随后做了原型实测（DOM / 文字窗口计入 load 之前已安静的时间，load 后至少再观察 100 ms；公网 51 个
  网址 × 2 轮，两侧都走渲染的 91 对）：首段用时配对差中位数 −1 ms、均值 +11 ms，81% 在 ±50 ms 内，
  快 ≥100 ms 与慢 ≥100 ms 各 8 对；整个沉降阶段中位数 +3 ms、均值 +16 ms；正文差异未超出同条件
  重复噪声。代价是 load 之后才触发的定时渲染会丢：夹具 `onloadTimer`（图片把 load 拖到约 1 s 后，
  load 处理器再用 550 ms 定时器写入）现行模式 2/2 采到，重叠模式 0/2，其余夹具的结束时间不变。
  结论：没有可测的收益，却更容易丢内容，不做；`onloadTimer` 留作哨兵，防止以后把静止窗口前移到
  DOMContentLoaded。实现上有个陷阱：页面加载期间 `webContents.executeJavaScript` 会被推迟到 load
  之后才执行（Electron 33 与 45 实测：dom-ready 时调用，load 后才返回；`webContents.mainFrame.executeJavaScript`
  则立即执行），在 DOMContentLoaded 提前安装探针必须用后者，否则“提前”实际没有发生。

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
首张与后两张不一致，加上之后三张逐字节相同，耗时不变（Electron 45 复测：加上之后三张仍逐字节相同）。

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

### 开发态构建失败不会杀掉应用

`electron:dev` 里 main / preload 是监听构建。vite-plugin-electron 在构建失败时同样会执行
`onstart`（Rollup 仍会触发 `closeBundle`），而 Vite 监听重建在开始前就清空输出目录：
一次构建错误会杀掉运行中的应用，再用空的 `out/main` 重启 Electron，弹出
“Unable to find Electron app … out/main/index.js”，整个 dev 会话随之退出。

`scripts/electron-dev-guard.mjs` 记录每个入口最近一次构建的成败：失败时跳过重启/重载，
只在终端留下 `[guizhi] main 构建失败…` 提示，修复后下一次成功的重建自动恢复。它只作用于
dev 的 `onstart`，不改变构建产物，也不影响 `vite build`。看到提示时，以终端里
`build started...` 之后的第一条构建错误为准；批量改写源码时重建也可能读到中间状态而失败，
保存一次相关文件即可重新构建。

已知余量：失败期间 `out/main` 仍是空的，此时应用自身的 `app.relaunch()`（如恢复备份）
会找不到入口；修复构建错误后即恢复。

### 打包依赖分类

被 Vite 编译进主进程、渲染进程、MCP 或阅读组件脚本的纯 JavaScript/CSS 库放在
`apps/desktop/package.json` 的 `devDependencies`，避免 electron-builder 再把整包
`node_modules` 带入安装包。例如 Mermaid、ECharts、CodeMirror 和阅读组件依赖。
保持 `dependencies` 中的运行时 external、原生模块、WASM 和需要按路径读取的资源；
不能只按“开发/运行时使用”字面分类。调整时同步锁文件，并验证真实打包产物的启动、
相关功能及资源读取；开发环境可解析依赖不代表安装包完整。

打包副本应使用 `createDesktopSnapshot(root, { reuseDependencies: false })`，在副本中
按冻结锁文件独立安装依赖。不要把共享包链接的验证副本交给包管理器：pnpm 的依赖
枚举也可能触发自动整理。验证副本不共享 `.pnpm` 或包管理器状态，避免改写原目录。

### 阅读组件编译内存

阅读图形库由 `scripts/reading-libraries-worker.mjs` 在独立 Node 子进程内编译，
父 Vite 进程只接收最终脚本，等待子进程退出后再输出资源。不要把多次图形库构建
合回长期运行的 Vite 进程；这会把编译堆保留在开发服务中。编译失败必须中断构建，
重试时重新启动子进程；正文库仍按需读取，不改变应用的运行时加载策略。

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
