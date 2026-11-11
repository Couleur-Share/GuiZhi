# 归知迁移到 MyGo：spike 结论与进度

分支 `spike/mygo`，代码都在 `spikes/` 下，不改动主线任何文件。本文是阶段性成果的收束，不是迁移方案定稿。

## 结论

1. **技术上可行，没有发现阻塞性的硬伤。** 复用现有 React 界面（不改一行渲染进程代码），用 MyGo 加 Go 服务替换 Electron 主进程，已经在壳层里跑通：启动、列表、搜索、写入、设置、收件箱、导入列表。
2. **工作量在主进程服务，不在框架。** 渲染进程约有 215 个 `invoke` 频道，已迁约 20 个（约 9%）。剩下的每一类都牵出一整套服务：采集、AI 请求、后台任务调度、Wiki 编译、导入队列、研究、备份、更新、快捷键、手机收集等。
3. **差分对拍是这条路的安全网。** 迁一个频道，就让 TS 原版和 Go 在同一份数据上各跑一遍，逐字段比。这套方法抓出了多处手写时看不出的偏差（见下文「对拍抓到的问题」）。

## 已验证的事

| 项目 | 结果 |
|---|---|
| 采集链路基础（`mygo-capture`） | 持久会话与 cookie、隐藏窗口加关闭节流、页面内 JS 取值（含 Promise）、整页截图均可用 |
| 数据库互通（`mygo-db`） | Go 的 SQLite 能读 wasm 写的库，FTS5 与 bm25 排序一致；Go 写入的行 wasm 能读回 |
| 壳层（`mygo-shell`） | 用 `electron` 垫片让现有 preload 原样跑在 MyGo 上，所有频道经一个 Go 的 `Bridge` 服务分发 |
| 读频道 | `collection:list`、`tag:list`、`knowledge:counts`、`knowledge:list`（50 个用例，含游标分页、置顶、并列时间、搜索、非法游标） |
| 写频道 | `knowledge:get/create/update/bulkUpdate/setStatus/moveToTrash/restore`（26 个场景）；`deleteForever/emptyTrash` 只实现了数据库部分，频道未登记 |
| 收件箱与导入 | `inbox:list`、`import:list`（45 个用例，含搜索转义、状态与来源筛选、游标、`pageSize` 取整、坏 JSON 容错） |
| 设置 | `settings:get/set`（白名单、FlareSolverr 校验、整批不写）与 AI 配置文件读写归一化（共 33 个场景，含逐字节比较输出文件） |
| 小频道 | `updater:version`、`updater:logAutoSkip`、`backupRepository:syncRendererSettings`、窗口控制 |

### 怎么复跑

```powershell
# 1) 生成 TS 基准（会在输出目录建隔离的 APPDATA，不读用户真实目录）
pnpm dlx tsx spikes\mygo-db\make-golden.mts "$env:TEMP\guizhi-mygo-golden"
# 2) Go 对拍
cd spikes\mygo-shell
$env:GUIZHI_GOLDEN_DIR = "$env:TEMP\guizhi-mygo-golden"
go test ./...
```

Go 工具链要求 1.27.3，本机 1.26.3 由 `GOTOOLCHAIN=auto` 自动拉取。壳层预加载与前端用 `vite.*.config.ts` 构建，运行参数见 `main.go`（`-dist`、`-db`、`-config`、`-shot`、`-wait`）。

## 对拍抓到的问题

这些都是「看起来对、但和 TS 不一致」的地方，已修复或已记录：

- **JS 与 Go 的语义差**：`\s` 与 `trim` 的空白集合、多行 `^`（含 `\r`、U+2028/2029）、UTF-16 长度与切片、`JSON.stringify` 的键序与转义、停用词排序的稳定性。
- **标签排序依赖系统语言**：TS 用 `localeCompare`，结果跟随操作系统语言。同一批标签在 zh-CN 下汉字排在拉丁字母前，在 en 下相反，`Éclair`/`eclair` 先后也不同。该顺序只影响全文索引 `tags` 列的词序，不影响命中，所以对拍按词集合比较。
- **批量更新里新标签要在同一批语句内复用**：MyGo 事务是静态语句批，不能读，多个条目共用同一个新标签时，必须在拼语句阶段记住已决定创建的标签，否则会重复创建。
- **基准生成脚本读到了用户真实配置**：`settings:get` 会读 `%APPDATA%\GuiZhi\config\ai-models.json`。已在脚本开头把 `APPDATA` 指到隔离目录并加了断言。凡是调用 TS 原版处理函数做对拍，都先确认它会读哪些真实目录。

## 已知阻塞与风险

1. **备份：`VACUUM INTO` 与 `ATTACH` 被 MyGo 的 SQLite 插件拒绝。** 备选方案：换纯 Go 驱动（如 `modernc.org/sqlite`，未验证）、或关库后拷贝。
2. **网络层拦截：已找到可行路径，附带安全约束（见下节「网络拦截验证」）。** MyGo 没有直接的 webRequest 接口，但可以在进程内设置 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=0`，再由 Go 连 DevTools 协议拦截。
3. **新旧版本不能同时打开同一个数据库。** 旧版 wasm 驱动用 `mkdir <db>.lock` 目录锁，与原生 SQLite 的字节范围锁不兼容；MCP 服务也用 wasm 驱动。灰度期要么互斥，要么统一驱动。
4. **机密与加密。** `ai-models.json` 里 `apiKey` 以明文保存在数据目录（与 TS 一致，迁移时没改）；`safeStorage`（DPAPI）保存的凭证、`electron-updater` 都需要单独的迁移方案。
5. **`settings:set` 的半写入特性。** 与 TS 一致：先写 AI 配置文件，再校验并写库，同一批里 AI 合法而 `flareSolverr` 非法时会出现「AI 已落盘、其余未写入」。正式迁移时建议改成先校验、后写入。
6. **窗口选项的顺序依赖。** MyGo 的 `ThrottlingOff` 必须给第一个窗口或 web view，后建的窗口设置无效。

## 网络拦截验证（`spikes/mygo-net`）

GuiZhi 主进程对网络层的实际用法（`apps/desktop/src/main`）：`onBeforeRequest` 拦截（SSRF 防护、采集时屏蔽资源、阻止外联）、`onCompleted` 观测状态码、`onHeadersReceived` 改写 CSP、`setProxy`、`setUserAgent`、`will-download` 与权限回调，另有 `webContents.debugger` 的 Network 域。

在 MyGo 上用 DevTools 协议替代，已用真实 WebView2 验证以下 8 项全部通过（`go run .`）：

| 能力 | 做法 | 结果 |
|---|---|---|
| 取得调试端口 | `--remote-debugging-port=0`，读用户数据目录下的 `DevToolsActivePort` | 通过 |
| 端口只在回环地址 | 对本机非回环 IP 拨号 | 拒绝连接 |
| 子请求拦截 | `Fetch.enable` + `Fetch.failRequest` | 页面 `fetch` 失败，服务端 0 命中 |
| 改写请求头 | `Fetch.continueRequest` | 服务端收到自定义头；`headers` 是整体替换，必须带上原有头 |
| 读响应状态码 | `Network.responseReceived` | 读到 404 |
| 改写响应头 | `Fetch.continueResponse` | 页面读到被改写的响应头，状态码保留（可用于 CSP） |
| 覆盖 User-Agent | `Emulation.setUserAgentOverride` | 服务端收到新 UA |
| 主框架导航拦截 | `Fetch.failRequest` | 服务端 0 命中，`OnDidFailLoad` 触发 |

### 独立采集进程（`spikes/mygo-netproc`）

主进程（带界面，不开调试端口）拉起子进程（采集进程，环境变量里带 `--remote-debugging-port=0`），再只用 DevTools 协议驱动它。`go build` 后运行，15 行输出全部为 OK（其中 5 行是启用步骤）：

| 能力 | 结果 |
|---|---|
| 主进程的数据目录里没有 `DevToolsActivePort` | 通过 |
| 采集进程的 DevTools 只列出自己的页面，看不到主界面 | 通过（1 个 target） |
| 先在空白页附加并启用 `Fetch`，再 `Page.navigate` | 首个文档请求被拦到并改写请求头 |
| 文档里的子资源拦截 | 服务端 0 命中 |
| `Runtime.evaluate` 取值、`Page.captureScreenshot`（隐藏窗口） | 通过，可替代 `Eval` 和 `CapturePage` |
| 权限拦截：`Browser.setPermission` 拒绝 geolocation | 立即返回 `denied:1`，不挂起 |
| 下载拦截：`Browser.setDownloadBehavior` 为 `deny` | 事件为 begin、inProgress、canceled，没有文件落盘；服务端仍收到了一次请求 |
| 杀掉采集进程后主进程窗口仍可用 | 通过 |

结论：网络拦截这条路径在设计上可行，采集放进独立进程既解决安全暴露，也解决崩溃隔离。

仍然成立的约束：

- 调试端口对整个 WebView2 环境生效。同用户的其他本机进程只要拿到端口就能驱动采集进程里的页面，所以采集进程不应加载任何带 `window.mygo` 桥的页面，也不应使用主进程的数据目录。
- 下载被拒绝前，请求已经发出。涉及 SSRF 防护的地址仍必须在 `Fetch` 的请求阶段拦截，不能只靠下载拒绝。
- 代理是环境级参数（`--proxy-server`），按进程设置；Electron 的按分区 `setProxy` 在 MyGo 的 `Session` 接口里没找到对应物。
- 按分区隔离：`Fetch` 拦截按 target 生效，天然隔离；持久会话与 cookie 已在 `mygo-capture` 验证。
- 还没验证：多个并发 target 的拦截隔离、采集进程池与复用、反爬场景下的真实站点。
## 还没做的

启动时壳层仍有 7 个频道未实现：

| 频道 | 牵出的东西 |
|---|---|
| `backgroundJob:syncRenderer` | 后台任务运行时：租约、重试、定时 tick |
| `backupRepository:consumeRendererSettings` | 备份子系统 |
| `import:queueState` | 内存里的导入队列，和采集链路绑在一起 |
| `migration:detectLegacy` | 旧 .NET 库的检测与计数 |
| `research:list` | 研究存储与工作流上下文（产品边界里属冻结功能） |
| `shortcuts:get` | 快捷键文件的新旧路径、旧键名迁移；注册全局热键需要先查 MyGo 的接口 |
| `wiki:compiler` | Wiki 编译器状态机 |

另有约 190 个频道未迁。设置里的「网络代理生效」和「可执行文件路径白名单」也未迁（只写库，没有实际效果）。

## 服务边界（2026-11-11 已定）

**新版只迁核心闭环（采集、整理、检索、问答、Wiki、备份），冻结功能不迁，旧版 Electron 留作只修安全问题的存档版。**

频道统计（按 `ipc-channels.ts` 的名字前缀归类，共 230 个，未逐个核对 handler）：

| 类别 | 频道数 | 内容 |
|---|---|---|
| 已迁 | 约 20 | 读写、设置、收件箱、导入列表等 |
| 冻结，不迁 | 约 43 | `research` 19、`illustration` 12、`discoveryView`/`discovery`/`discoveryCandidate` 8、`readingGraphics` 2、`mobile-capture` 1、`reading-sync` 1 |
| 核心，待迁 | 约 167 | 采集一族约 45（`crawl`、`web`、`platformCapture`、`media`、`ffmpeg`、`ytdlp`、`funasr`），AI 一族约 55（`ai`、`ask`、`articleAsk`、`semantic`、`wiki`、`themedReading`），其余为备份、导入、后台任务等 |

依据：`docs/product-scope.md` 规定冻结功能不新增频道、设置项和数据表，只修安全、崩溃和数据丢失；把它们移植到 Go 等于对冻结代码做大改动。

代价与约束：

1. 新版不含手机同步、近期研究、平台发现、正文配图。需要的用户回旧版。
2. 研究、发现等数据保留在库里，新版不读不写，也不删。
3. 新旧版本不能同时打开同一个库（见上文第 3 条风险），只能轮流使用。
4. 采集一族依赖的网络拦截已验证可由 DevTools 协议替代，须放进独立的采集进程（见「网络拦截验证」）；真实站点和并发仍待验证。
## 建议的下一步

1. 服务边界已定，网络拦截已有可行路径。可以继续迁移。
2. 验证备份替代方案（第 1 条）；用真实站点和多 target 并发压测独立采集进程。
3. 如果继续迁：按「纯读库 → 写库 → 带副作用」的顺序，沿用差分对拍，每迁一组频道就补一组基准。
