# Electron 升级评估：33.4.11 → 45

2026-11-07 只读评估，未改依赖、未运行新版 Electron。代码位置以 2026-11-07 首批死代码清理后的工作区为准，写文件与符号而不写行号；落地前按当时代码重新 `rg` 核对。标「推断」的结论未经实测。

## 结论

升级到 **Electron 45，锁定当时最新补丁（评估时为 45.2.3）**。只在 45 上出现的阻断问题，可退到 44.10.x；下文的代码改动对 44 同样成立，换版本不必回退。

必须升级的原因：33 已于 2025-04-28 停止支持（Chromium M130），而归知会在 Electron 窗口里加载不受信任的网页（渲染采集、平台登录、网站验证），AI 阅读还在隔离视图里执行模型生成的脚本。这些边界都依赖 Chromium 沙箱和后续安全补丁。

附带收益：主进程 Node 由 20.18.3 升到 24.22.2，与 `apps/desktop/package.json` 的 `engines`（≥24）、CI 和 vitest 运行环境一致；主进程依赖 sanitize-html 2.17.7 声明的 `engines`（≥22.12.0）当前不满足，升级后满足。

| 版本 | Chromium | 内置 Node | 稳定 / 停止支持 | win32-x64 zip |
| --- | --- | --- | --- | --- |
| 33.4.11（现状） | 130.0.6723.191 | v20.18.3 | 2024-10-14 / 2025-04-28 | 115,028,145 B |
| 44.10.2 | 152.0.7977.169 | v24.22.2 | 2026-08-24 / 2027-03-02† | 158,203,656 B |
| 45.2.3 | 156.0.8078.80 | v24.22.2 | 2026-10-20 / 2027-04-27† | 158,182,975 B |

† 为官方暂定日期。44 与 45 的最新补丁内置同一 Node，差别在 Chromium 与支持窗口：46 稳定（2027-01-05†）后 44 只收安全修复，45 仍收大部分修复。34→45 共 68 条破坏性变更，本仓库命中的见下两节。

## 必须修改

| 变更 | 起始版本 | 位置 | 处理 |
| --- | --- | --- | --- |
| 移除登录项属性 `openAsHidden`、`wasOpenedAsHidden` | 44 | `main/index.ts`：读取 `getLoginItemSettings().wasOpenedAsHidden`；`app:setAutoLaunch` 传入 `openAsHidden` | 删除这两个字段。Windows 上它们本就无效，隐藏启动依靠 `--hidden` 参数，行为不变；不删会导致类型检查失败（推断） |
| `console-message` 位置参数 | 35 弃用，47 移除 | `platform-capture/electron-capture-runtime.ts`（`level >= 2`）；`themed-reading/visual-compiler.ts`（`level >= 3` 判定图形编译失败） | 改为读取事件对象的 `level`、`message`、`lineNumber`、`sourceId`，`level` 改按字符串判断（`"warning"`/`"error"`）。visual-compiler 的失败判定依赖它，改后必须跑阅读图形回归 |
| 安装依赖时不再下载 Electron 二进制 | 42 | `pnpm-workspace.yaml` 的 `allowBuilds.electron` 不再起作用；首次 `require("electron")` 或 Playwright 启动时才下载 | 在升级用的工作副本执行 `pnpm install` 后，立即 `pnpm --filter @guizhi/desktop exec install-electron --no` 预下载并确认路径存在；隔离验证链接回源副本的依赖，未预下载会在验证时临时下载 |
| 安装包体积预算 | — | `config/package-size-budget.json`：安装包上限 143,000,000 B，基线 132,016,967 B | Electron 发行包增大约 43.2 MB，推断会超出上限。打包实测后按该文件 `$comment` 的约定，在同一提交更新预算并写明原因；也可评估 electron-builder 的 `electronLanguages` 只保留所需语言包 |

45 移除沙箱 preload 中的 `Buffer`、`setImmediate`、`clearImmediate` 与 Node 模块垫片。原先唯一的使用点 `saveImageBuffer` 已随首批清理删除，当前 preload 不受影响；以后新增的 preload 代码不能再用这些全局对象。

## 行为变化：不报错，但需要决策或验证

- **对话框默认目录（43）**：未传 `defaultPath` 的打开对话框改为固定从「下载」目录开始，系统不再记住上次目录。涉及 7 处：选择数据目录（`main/index.ts`）、Markdown 导出目录（`ipc/backup.ipc.ts`）、导入配置文件（`ipc/config-transfer.ipc.ts`）、导入文件（`ipc/import.ipc.ts`）、选择可执行文件（`ipc/media.ipc.ts`）、网页快照导出目录（`web-capture/snapshot-service.ts`）、选择背景图（`ipc/image.ipc.ts`）。需要保留旧体验时，按用途记住上次目录并作为 `defaultPath` 传入。主题阅读导出只传了文件名，推断维持旧行为，需手工确认。
- **`executeJavaScript` 改走 mojo（45，据上游 PR #53969）**：帧在回复前消失时，Promise 由一直挂起改为拒绝。沉降轮询有容错，最后的快照与链接提取（`web-capture/web-electron-renderer.ts`）没有；脚本跳转的页面可能比以前更容易失败。
- **webRequest 与协议请求链路改造（44/45 补丁）**：只观察的监听器不再阻塞主线程、没有监听器时请求不经主线程、协议请求新增 `initiatorOrigin`、worker 请求改经 `protocol.handle`。出口防护所依赖的 33 实测前提都要重测（见下一节第 1–4 条）。
- **隐藏窗口渲染修复（44/45 补丁）**：影响采集窗口先隐藏后挪到屏幕外的做法，以及离屏截图。
- **Chromium 156 导航请求 `Accept` 的组成改为按 JXL/SXG 开关拼接**：`web-capture/web-request-identity.ts` 的 `DOCUMENT_ACCEPT` 写死的是 Chromium 130 实测值，需要用请求身份探测重新取值；按 `image/` 前缀分类图片请求的规则仍然成立。
- **`registerFileProtocol` 对跨源 no-cors 请求返回 opaque 响应（41–45 补丁）**：`local-image://`、`local-video://` 只用于 `<img>`/`<video>`，推断无影响，需手工确认图片显示与视频拖动进度。
- **回滚风险**：Chromium 不保证用户数据可降级。升到 45 后若回滚到 33，localStorage（含 `guizhi-settings`）、平台登录 Cookie 能否被旧版读取需要实测。

## 有时限的后续，不放进本次升级

- **safeStorage 同步接口**：45 弃用（只打告警），46 移除。涉及 `services/safe-storage-key-protector.ts`（备份仓库无人值守解锁）、`mobile-capture/credentials.ts`、`reading-sync/credentials.ts`、`themed-reading/search-service.ts`，以及对应测试中的 mock。升级 46 之前必须迁移到 `isAsyncEncryptionAvailable`、`encryptStringAsync`、`decryptStringAsync`（42 起提供），`RepositoryKeyProtector` 也要改为异步。官方只保证同步接口加密的数据能被异步接口解开，没说明反向是否可行，因此迁移单独发版，并评估回滚。
- **`protocol.registerFileProtocol`**：自 25 起弃用，到 47 仍未移除，择机迁移到 `protocol.handle`。
- **46 的其他相关变更**：`capturePage()` 返回的图像带页面缩放系数，`apps/desktop/scripts/screenshot.mjs` 拼接阅读子视图的尺寸计算要改；http(s) 页面的 `unload` 处理器不再执行，当前代码没有使用。

## 工具链

| 组件 | 当前 | 判断 |
| --- | --- | --- |
| electron-builder | 26.15.7 | 打包时按版本自行下载 Electron，不绑定版本；本次不升级。`pnpm-workspace.yaml` 对 `@electron/get@3` 的覆盖仍需保留 |
| electron-updater | 6.8.9 | 用 `net.request` 下载更新，44 的 Sec-Fetch-Dest 限制不触发；发版前在 Draft Release 实测一次 33 版自动更新到新版（全量下载） |
| vite-plugin-electron / -renderer | 0.28.8 / 0.14.7 | 只取 Electron 可执行文件路径后启动，不绑定版本；不升级 |
| Playwright | 1.61.1 | 支持 Electron ≥21。升级期间不要同时升级 Playwright：1.64 起用 `executablePath` 启动打包应用可能丢失首个窗口（microsoft/playwright#43247），`pnpm shot --executable` 依赖这条路径 |
| node-sqlite3-wasm | 0.8.x | 纯 WASM，无 ABI 绑定；MCP server（以 Node 模式运行）将跑在 Node 24 上，需回归 |
| @resvg/resvg-js | 2.6.2 | Node-API 预编译，ABI 稳定，无需重编译 |

## 需要重验的「Electron 33 实测」结论

| # | 结论 | 主要位置 | 回归入口 |
| --- | --- | --- | --- |
| 1 | `protocol.handle` 接管 http(s) 后 webRequest 不触发；`request.destination` 为空、没有 `Sec-Fetch-*`，资源类别只能看 `Accept` | `desktop-development.md`；`web-electron-renderer.ts`、`web-network-activity.ts`、`web-trace.ts` | 出口探测、沉降夹具 |
| 2 | 导航与 prefetch 的 `Accept` 以 `text/html,application/xhtml+xml` 开头；jQuery 片段请求带空格 | `web-trace.ts` | 沉降夹具 `ajaxHtml`、`prefetchHang`；请求身份探测 |
| 3 | WebSocket 仍会触发 webRequest；黑洞代理与 WebRTC 兜底 | `web-electron-renderer.ts` | 出口探测 |
| 4 | `will-frame-navigate`、`will-redirect` 先于取页面触发；跨源子框架在此取消 | `web-electron-renderer.ts` | 出口探测；web-capture-runtime 集成测试 |
| 5 | 转发请求没有 `Accept-Language`；`DOCUMENT_ACCEPT` 为 Chromium 130 值 | `web-request-identity.ts` | 请求身份探测 |
| 6 | 图片占位应答必须带 CORS 与 CORP 头 | `web-inert-resources.ts` | 惰性图片探测 |
| 7 | 加载期间 `webContents.executeJavaScript` 推迟到 load 之后，`mainFrame.executeJavaScript` 立即执行 | `web-network-activity.ts`、`web-settle.ts` | 沉降夹具 `jsRedirect`、`onloadTimer`；dom-ready 时序需临时步骤脚本 |
| 8 | 屏幕外 + 透明度 0 + `showInactive` 截图与正常显示逐字节相同；真隐藏窗口合成停摆 | `testing/window-mode.ts`；`desktop-development.md` | `pnpm shot` 连截比对哈希；rAF 计数需临时脚本 |
| 9 | 截图需关闭动画、隐藏光标 | `scripts/screenshot.mjs` | 连截三张比对 |
| 10 | 部分 Windows 图形环境直接加载 `file://` 失败，截图改用回环 HTTP 并固定 SwiftShader | `screenshot.mjs`、`testing/e2e.ts` | `test:e2e`（`file://`）与 `shot`（HTTP）都跑 |
| 11 | srcdoc 同步死循环被终止后 `loadURL` 可能不返回，另设故障通道 | `themed-reading/v3-views.ts` | reading-v3 验收中的 `while(true)` 用例（12 秒内报故障） |
| 12 | 部分大型单页应用迟迟不触发 dom-ready，改以 did-navigate 结束等待 | `electron-capture-runtime.ts` | 平台登录手工验证 |

重测后同步更新上述位置的注释与 `desktop-development.md` 技术栈中的 Electron 版本。

## 回归清单

全部在独立工作副本中执行；`pnpm shot` 会复制当前源码到系统临时目录，使用临时 userData 与屏幕外窗口。

**升级后必须跑（按顺序）：**

1. `pnpm typecheck`、`pnpm lint`、`pnpm test:unit`、`pnpm build:isolated`。单测中 Electron 全部被 mock，只能验证改动本身。
2. `pnpm test:e2e`：smoke、security（CSP、外部导航）、persistence（关闭前保存、重启后数据仍在）、accessibility，页面经 `file://` 生产路径加载。
3. 出口探测：`pnpm shot --steps scripts/web-egress-probe-steps.mjs --out <新目录>`，再加 `GUIZHI_WEB_SUBFRAMES=allow` 跑一次。离线，但需要被 Git 忽略的 `apps/desktop/resources/crawl4ai`。
4. 沉降夹具：`scripts/web-settle-fixtures-steps.mjs`。
5. 请求身份探测：`scripts/web-identity-probe-steps.mjs`，再以 `GUIZHI_WEB_IDENTITY=none` 跑基线；`Accept` 变化时更新 `DOCUMENT_ACCEPT` 后重跑 4、5。
6. 惰性图片探测：`scripts/web-inert-probe-steps.mjs`。
7. web-capture-runtime 集成测试：`GUIZHI_TEST_BUNDLED_CRAWLER=1 pnpm --filter @guizhi/desktop exec vitest run tests/integration/web-capture-runtime.test.ts`（真实 Electron + 随包 Python；只能在独立副本运行）。
8. AI 阅读：`GUIZHI_GRAPHICS_VALIDATION=1 pnpm shot --steps apps/desktop/tests/e2e/reading-v3-acceptance.mjs`（隔离进程、网络与开窗拦截、死循环故障）和 `reading-graphics-shots.mjs`（console-message 失败判定）。
9. `apps/desktop/tests/e2e/heavy-user-workflows-shots.mjs`（Wiki 编译流程）与 `heavy-user-index-shots.mjs`（5 万分块语义检索期间输入不卡顿）。
10. `pnpm check:linux`：锁文件必然变化，按 `docs/agents/linux-preflight.md` 走德国共享构建器。
11. 打包：在 `apps/desktop` 执行 `pnpm build && pnpm electron-builder --config electron-builder.config.cjs --win --x64 --publish never`，再执行 `node scripts/check-package-size.mjs --installer <安装包>`。不要用 `electron:build:win`，它会同时打 arm64。
12. `pnpm shot --executable <win-unpacked 或安装后的 GuiZhi.exe> --steps <任一步骤脚本>`：确认打包产物启动、asar 内外资源与 MCP 资源可读。

**建议跑：**

- 升级前先在 33 上跑一轮 `scripts/web-trace-steps.mjs` 公网基线（需本机代理，设 `GUIZHI_TRACE_SAVE_CONTENT=1`），升级后用 `GUIZHI_TRACE_BASELINE` 逐页对照。
- `scripts/nodeseek-ui-steps.mjs`、公众号快照（`wechat-snapshot-shots.mjs` 加 `wechat-snapshot-restore` 集成测试）：需要公网与本机代理。
- Windows Sandbox 安装验收包 `scripts/crawl4ai-acceptance/`：旧版覆盖升级、卸载重装；`rollback.mjs` 里写死的旧版本号需改为当前发布版。
- 手工：平台登录（抖音、小红书、LINUX DO、NodeSeek）、网站验证窗口、开机隐藏启动与托盘、系统通知、7 处对话框默认目录、MCP、safeStorage 已存凭据（手机收集、阅读同步、搜索服务 Key、备份仓库解锁）、数据目录迁移、四种代理模式下的 AI 请求与采集。

**覆盖缺口：** `local-image://`/`local-video://` 没有离线且纳入版本库的回归；隐藏窗口 rAF 停摆没有脚本；跨版本 Chromium 用户数据与 safeStorage 数据的升级/回滚没有自动化。

## 实施顺序

1. 确认用户开发实例状态，不关闭用户应用；确认要带进升级分支的未提交改动。
2. 在 33 上取基线：跑必须清单第 3–6、8–9 项与公网 web-trace，保留结果目录。
3. `git worktree add <路径> -b chore/electron-45`，准备 `resources/crawl4ai` 与所需 `/artifacts` 夹具。
4. 把 `apps/desktop/package.json` 的 `electron` 改为精确的 `45.2.3`，其余工具链不动；`pnpm install` 后预下载并核对 Electron 二进制。
5. 改代码：登录项字段、`console-message`；可选为对话框记住上次目录。
6. 跑必须清单。只在 45 上失败的项目，用 44.10.2（同为 Node 24.22.2）复跑，区分 Chromium 152→156 与 45 专属变化。
7. 打包、更新体积预算、`shot --executable` 冒烟；有条件时做 Sandbox 覆盖升级与回滚记录。
8. 真实流程验收，报告时分开写源码/隔离测试、真实第三方流程和已发布状态。
9. 交付差异；用户切换版本时先关闭应用，再在开发目录 install 并预下载 Electron。
10. 发版按 `.cursor/skills/guizhi-release/SKILL.md`，在 Draft Release 实测 33 版经 electron-updater 升级到新版。

## 未确认事项

1. 44/45 上 webRequest 与 `protocol.handle` 的交互、`request.destination`、`Sec-Fetch-*`、`Accept-Language` 是否仍如 33（只有源码层面的线索）。
2. Chromium 156 实际发出的导航 `Accept`，取决于 Electron 中 JXL/SXG 的默认状态。
3. `executeJavaScript` 改走 mojo 后，脚本跳转页面的快照失败率是否变化。
4. 屏幕外截图合成、隐藏窗口 rAF、`file://` 加载问题在新 Chromium 与 ANGLE 静态链接下的表现。
5. 安装包实际体积（只知道 zip 增量）。
6. 回滚到 33 后 Chromium 用户数据能否读取；异步 safeStorage 加密的数据能否被同步接口解开。
7. 45 默认禁用 `document.requestStorageAccessFor` 对平台登录页的影响。
8. 本机代理网络下按需下载 Electron 的表现，以及 `@electron/get` 5 支持的镜像与代理环境变量。
9. electron-builder、electron-updater、vite-plugin-electron 都没有发布 Electron 支持矩阵，兼容结论来自代码阅读与发布说明。

## 依据

- 破坏性变更：<https://www.electronjs.org/docs/latest/breaking-changes>（各版本小节，如 `#breaking-api-changes-450`）
- 发布时间表与支持策略：<https://releases.electronjs.org/schedule>、<https://www.electronjs.org/docs/latest/tutorial/electron-timelines>
- 版本内置的 Chromium 与 Node：`electron/electron` 各 tag 的 `DEPS`；发行资产大小：GitHub release `v33.4.11`、`v44.10.2`、`v45.2.3`
- safeStorage：<https://www.electronjs.org/docs/latest/api/safe-storage>；`console-message`：<https://www.electronjs.org/docs/latest/api/web-contents#event-console-message>
- 上游变更：executeJavaScript <https://github.com/electron/electron/pull/53969>；对话框默认目录 <https://github.com/electron/electron/pull/49868>；registerFileProtocol opaque 响应 <https://github.com/electron/electron/pull/52822>；webRequest/协议链路 #53141、#53240、#53685、#55119；隐藏窗口渲染 #54118、#54341
- Chromium 导航 Accept：<https://github.com/chromium/chromium/blob/156.0.8078.80/content/public/browser/frame_accept_header.cc>
- Playwright Electron 支持：<https://playwright.dev/docs/api/class-electron>；回归 <https://github.com/microsoft/playwright/issues/43247>

## 实施结果（2026-11-07）

在独立工作副本的 `chore/electron-45` 分支（基于快照 `50288cfe`）完成：`apps/desktop` 的 electron 锁定为 `45.2.3`，实测 Chromium 156.0.8078.80、Node 24.22.2。Electron 33 对照在同一快照的临时工作副本中独立安装依赖，用同一命令运行。下文全部是源码与隔离测试，没有跑真实第三方流程，也没有发布。

### 代码改动

- 登录项：删除 `wasOpenedAsHidden` 与 `openAsHidden`（改前 `tsc` 恰好报这两处），隐藏启动只认 `--hidden`。macOS 上 `args` 不生效，按代码阅读由 `minimizeOnLaunch` 兜底，未实测。
- `console-message`：两处改读事件对象，`level` 按字符串判断。临时探测确认：45 上 `console.error`、未捕获异常、未处理的 Promise 拒绝都报 `"error"`，`console.warn` 报 `"warning"`；33 的事件对象没有这些字段。
- `DOCUMENT_ACCEPT` 改为 Chromium 156 的实测值（多了 `image/jxl`），见下文第 2、5 条。
- `tests/e2e/reading-v3-shots.mjs` 的 Worker 边界检查改为以脚本能否执行为准：Chromium 156 拦截 `data:` Worker 时不再同步抛 `SecurityError`，改为异步触发 error 事件，脚本仍不执行。
- 安装包体积预算上调，见下文。

### 门禁结果

| 门禁 | 结果 | 说明 |
| --- | --- | --- |
| typecheck、lint、test:unit、build:isolated | 通过 | vitest 304 个文件、2612 个测试；node --test 20/20 |
| test:e2e | 6 项中 5 项通过 | accessibility 失败，33 上同样失败，属既有问题 |
| 出口探测（默认与 `GUIZHI_WEB_SUBFRAMES=allow`） | 通过 | 没有连接绕过出口；越界导航在取页面前被拦截 |
| 沉降夹具 | 通过 | 更新 `DOCUMENT_ACCEPT` 前后各跑一次 |
| 请求身份探测与 `GUIZHI_WEB_IDENTITY=none` 基线 | 通过 | 首跑因导航 Accept 多了 `image/jxl` 而失败，更新常量后通过 |
| 惰性图片探测 | 通过 | |
| web-capture-runtime 集成测试 | 默认失败；`allow` 时通过 | 默认失败在“跨源子框架被请求”的断言，33 上同样失败，属既有问题 |
| reading-v3-acceptance | 第 3 次通过 | 第 1 次失败于上述 Worker 检查；第 2 次在高负载下等待 900 ms 后找不到阅读子视图（第 1、3 次这一步都通过）。死循环 5.6 s 内报故障，导出无网络请求 |
| reading-graphics-shots | 通过 | 只覆盖成功路径，失败判定由上述 console-message 探测补充 |
| heavy-user-workflows-shots | 1 次失败、1 次通过 | 失败时 CPU 约 83%，等待“已选 60 项”超时 |
| heavy-user-index-shots | 4 次中 2 次失败 | 见下文“SwiftShader 下的输入延迟” |
| 打包与体积门禁 | 通过（预算已上调） | 运行包 193.7 MiB，未变 |
| 打包产物冒烟（`shot --executable`） | 通过 | asar 内外资源、网页组件可用；MCP 以 `ELECTRON_RUN_AS_NODE`（Node 24.22.2）完成握手，并检索到打包产物写入的条目 |
| Linux 预检（Bero） | 通过 | snapshotSHA256 `39ca02447f3c3a4d86123f58021ce6b3ac3de6b4eb0870566af9e808fa83290a`，runId `2026-11-06T19-10-35-213Z-c5a8fe3d` |

### SwiftShader 下的输入延迟

`heavy-user-index-shots` 测量 5 万分块检索期间“输入事件到下一帧”的 P95（阈值 100 ms）：45 上四次为 103.1、71.9、111.4、68.8 ms，33 上三次为 18.9、16.8、15.9 ms。不跑检索时 45 三次的 p50 为 42–74 ms、p95 为 125–164 ms，33 为 11–13 ms、36–55 ms；页面上临时插入的原生 `<input>` 两边接近；输入后的同步更新 45 为 3.3/5.4 ms、33 为 5.6/8.4 ms（p50/p95），时间耗在出帧阶段。检索让出事件循环的方式从 `setImmediate` 换成 `setTimeout(0)` 不改善（P95 约 68 ms），已撤回。改走 e2e 路径（`file://`、默认 GPU 合成）时，45 为 p50 3.6 ms、p95 6.0 ms，33 为 3.9 ms、23.4 ms。

结论：变慢只出现在 `pnpm shot` 固定使用的 SwiftShader 软件渲染下，GPU 路径没有变慢。没有 GPU 加速、靠软件渲染的用户机器是否受影响，未验证。门禁如何处理（调整阈值、换测量路径或继续追查 SwiftShader）待决定。

### 安装包体积

本地打包 `GuiZhi-Setup-0.26.0-x64.exe` 为 159,948,002 字节（152.5 MiB），超过原上限 143,000,000。同一份应用代码用 Electron 33.4.11 打包为 129,979,908 字节，相差 29,968,094 字节；win-unpacked 中 resources 以外的 Electron 文件由 280,967,425 增至 374,036,010 字节，resources 两边相同。按 `config/package-size-budget.json` 的约定，上限改为 173,000,000（实测约 +8%）。`electronLanguages` 精简语言包未评估。

### 重验结论（编号对应上文“需要重验的结论”）

1. **已变化**：45 上 `webRequest` 对被 `protocol.handle` 接管的 http(s) 请求都会触发，包括 `loadURL` 的主框架导航、脚本跳转、子框架、样式、脚本、图片、XHR 与预取；33 上一个都不触发。`request.destination` 仍为空，仍无 `Sec-Fetch-*`。现有监听放行 http(s)，行为不变；已更新相关注释与 `desktop-development.md`。
2. **成立**：导航、子框架与预取的 Accept 仍以 `text/html,application/xhtml+xml` 开头；值多了 `image/jxl`，预取另带 `Sec-Purpose: prefetch`；jQuery 式自定义 Accept 原样转发。图片 Accept 改为 `image/jxl,image/avif,…`，仍以 `image/` 开头。
3. **成立**：WebSocket 仍触发 `webRequest`；去掉监听后被黑洞代理兜住；本机端口没有收到连接，也没有 UDP 数据报。
4. **成立**：越界导航在取页面前被拦截；跨源子框架默认不取，`allow` 时取。
5. **成立**：转发请求仍无 `Accept-Language`；`DOCUMENT_ACCEPT` 已更新。
6. **成立**：CORS 与 COEP 用例中占位图正常加载。
7. **成立**：dom-ready 时调用，`webContents.executeJavaScript` 在 did-finish-load 之后 1–3 ms 返回，`mainFrame.executeJavaScript` 在 3–6 ms 内返回（两个版本各测 2 次）。
8. **部分重验**：屏幕外窗口连截三张逐字节相同。“与正常显示逐字节相同”需要把窗口显示出来，未重测。“真隐藏窗口 rAF 停摆”没有复现：在同一个 Playwright 实例里新建、从未显示的窗口，33 与 45 都约 330 帧/2 s；原测量对象是不调用 `show()` 的主窗口加 `page.screenshot`，条件不同，这条结论未重验。
9. **成立**：默认截图选项下连截三张逐字节相同。
10. 本机上两条路径都正常（e2e 走 `file://`，shot 走回环 HTTP + SwiftShader）；“部分图形环境加载 `file://` 失败”无法在本机重测。
11. 死循环在 5.6 s 内经故障通道报出；`loadURL` 本身是否仍不返回，未单独验证。
12. 未测，需要平台登录手工验证。

“未确认事项”中，第 1、2、5 条已有实测结果；第 4、8、9 条部分有结果（本机网络下 `install-electron` 用 26 s 下载成功；electron-builder 26.15.7 能打包 45；vite-plugin-electron 的开发模式与 electron-updater 未跑）；第 3、6、7 条仍未确认。

### 既有问题（33 上同样出现，本分支未修复）

- 新用户首次启动的界面语言可能是英文。`settings-defaults.ts` 的默认 `language` 读取 `i18n.language`，而 `main.tsx` 并行加载 App 与语言资源；探测中 `navigator.language` 为 `zh-CN`，`guizhi-settings` 里保存的却是 `en`，离屏实例与打包产物首次启动都显示英文。机制来自代码阅读与探测结果，未单独证实。这也是 accessibility 用例失败的原因之一：“处理中心”的匹配正则不含英文的 `Inbox`。
- web-capture-runtime 集成测试仍断言跨源子框架会被请求，与默认不加载跨源子框架的策略矛盾。

### 本次未验证

- 公网 `web-trace` 基线对照、NodeSeek 与公众号流程（需要本机代理和公网）。
- 平台登录（抖音、小红书、LINUX DO、NodeSeek）与网站验证窗口。
- Windows Sandbox 安装验收（覆盖升级、卸载重装）。
- 回滚到 33 后 localStorage、平台登录 Cookie 与 safeStorage 已存凭据能否读取。
- 33 经 electron-updater 升级到新版（Draft Release）。
- 7 处对话框的默认目录、`local-image://` 与 `local-video://` 的显示和拖动进度。
- 开机隐藏启动与托盘、系统通知、在真实 Cursor 中使用 MCP、四种代理模式、数据目录迁移。
- `executeJavaScript` 改走 mojo 对真实脚本跳转页面的影响（只跑了夹具 `jsRedirect`），以及 `document.requestStorageAccessFor` 默认禁用对平台登录的影响。
