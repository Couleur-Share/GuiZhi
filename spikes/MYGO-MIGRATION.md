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

1. **备份：MyGo 的 SQLite 插件用授权回调拒绝 `VACUUM INTO` 与 `ATTACH`（authorization denied）。已验证的替代方案：备份单独用 `modernc.org/sqlite` 只读连接做 `VACUUM INTO`（见下节「备份验证」）。
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
## 备份验证（`spikes/mygo-backup`）

`createBackup` 用 `VACUUM INTO` 对在线库生成单文件备份；MyGo 内置 SQLite 拒绝它（`authorization denied (code 23)`，已实测）。替代做法：业务读写仍走 MyGo 自带 SQLite，备份只用纯 Go 的 `modernc.org/sqlite` 以 `mode=ro` 打开同一个库并执行 `VACUUM INTO`。运行：`go run . <含 knowledge.db 的目录> [padMB]`，样本是 212 MB 的库（真实 `knowledge.db` 加两张各 100 MB 的 pad 表）。

| 检查 | 结果 |
|---|---|
| 备份文件的 `integrity_check` 与 FTS5 `integrity-check` | 通过 |
| MyGo 自带 SQLite 打开备份，所有表行数、建表语句与源库一致 | 通过 |
| 单次备份耗时（212 MB） | 约 0.83 秒 |
| MyGo 持续写入时连续备份 5 次 | 全部通过，快照原子（成对写入的两张表行数相等），写入 0 次失败 |
| 备份期间写入的最长停顿 | 882 ms |
| 目标文件已存在 | 报错 `output file already exists`，与 `VACUUM INTO` 语义一致 |

要点：

- 当前库是回滚日志模式（没有 WAL，见 `packages/db/src/init.ts`），备份持有读锁期间，写入会等到备份结束，所以停顿大约等于备份耗时。这和现状一致，但 Go 里备份可以放在后台协程，界面线程不会像现在这样被同步 `VACUUM INTO` 冻住。
- 二进制体积增加约 4 MB（只含这两个包的最小程序对比，粗略值）。
### 新旧版互通（`verify-wasm.mts`）

用 Go 加 modernc 生成的备份，交给旧版真实代码处理，再把旧版写过的库交回新版：

| 步骤 | 结果 |
|---|---|
| 旧版 `validateBackupFile`（恢复前校验：`quick_check`、核心表、结构版本） | 通过 |
| 旧版 `initDatabase`（恢复后的真实打开路径，含迁移执行器）读条目数 | 通过 |
| 旧版 FTS5 中文检索 | 通过，有命中 |
| 旧版在该备份上新建条目（含 FTS 同步） | 通过，条目数加 1 |
| 新版（modernc `integrity_check`、FTS `integrity-check`、MyGo 打开读条目数） 回读旧版写过的库 | 通过，条目数含新增那条 |

也就是说，备份格式在新旧版之间是双向互通的。运行：`GUIZHI_BACKUP_KEEP=<路径> go run . <含 knowledge.db 的目录> 1` 生成备份，再 `pnpm dlx tsx spikes/mygo-backup/verify-wasm.mts <备份> <回写副本>`，最后 `GUIZHI_BACKUP_CHECK=<回写副本> go run .` 回读。

还没验证：恢复流程里的 `pre-restore` 快照、换库、重启；加密备份格式（`encrypted` 字段）；旧版 wasm 与新版 SQLite 同时打开同一个库的锁冲突（第 3 条风险，仍成立）。
## 正文提取等价性（`spikes/mygo-readability`）

产品的网页正文提取是 `@mozilla/readability` 0.6.0（经 linkedom）加一层自己的规则（`web-extract-article.ts` 约 400 行、`web-extract-markdown.ts` 约 190 行）。这一步只验证最底层：Go 的 `codeberg.org/readeck/go-readability/v2`（`github.com/go-shiori/go-readability` 已弃用，指向它；该库声明对标 Readability.js 0.6.0，版本与产品一致）与 JS 在同一份真实网页上选出的正文是否一致。

语料：`%TEMP%\guizhi-crawl-eval` 里 2026-11-06 评估 Crawl4AI 时留下的 59 个真实网页缓存（静态与渲染两份 HTML）。它不在仓库里，也不随提交走；样本偏技术文档，且是评估时调阈值用过的那一批，不是留出样本。按产品的静态/渲染分流（`assessStaticPage`）只比较每页实际会使用的那份 HTML。比较方式：正文去空白后逐字比较，不相同时用 5 字窗口的多重集合重合度。

| 模式 | 逐字相同 | ≥0.99 | 0.95–0.99 | 0.80–0.95 | <0.80 | 标题一致 |
|---|---|---|---|---|---|---|
| 库默认行为（仅 `charThreshold=100`） | 51 / 59 | 5 | 2 | 1 | 0 | 59 / 59 |
| 加上产品对 Readability 的两处改动 | 52 / 59 | 3 | 3 | 1 | 0 | 59 / 59 |

- **产品的两处改动必须移植。** 它们是 `linkDensityModifier = 0.1`（条件清理里两处链接占比阈值 `0.2`、`0.5` 各加该值）与「保留与标题重复的首个 h1/h2」（把 `_headerDuplicatesTitle` 改成恒 `false`）。Go 库没有对应选项；不打补丁时，对产品模式的基准只有 52 / 118 输入逐字相同，打补丁后为 100 / 118（118 是静态与渲染两份都算）。补丁很小（3 处，`prepare-patched.ps1`），需要维护一份 fork 或补丁序列。
- **最差的一页相似度 0.931**（martinfowler 的长文，JS 多保留了一些图示文字）；其余差异在 0.988 以上，都是个别元素的取舍（例如 dev.to 代码块的「全屏」按钮文字、Playwright 文档里的示意图文字）。没有出现整块正文丢失。
- **静态 HTML 的解析器差异：** 产品用 linkedom（非规范解析器），Go 用 `x/net/html`（HTML5 规范解析）。5 个 MDN 页面的静态版本差异很大（其中 3 个 JS 取到约 1.8 万字，Go 只有 800 到 2000 字；另 2 个相差约 25%），但产品把它们分流到渲染版本，渲染版本两边一致，所以不影响实际结果。分流规则本身需要一并迁移。
- **库层面之外的部分，也做了移植验证（下一小节）。**

### 整条提取链路（`spikes/mygo-readability/extract`）

把 `web-extract-article.ts` 与 `web-extract-markdown.ts` 移植到 Go（`extract` 包约 1100 行，含辅助的 DOM 函数；TS 原文约 590 行），Markdown 转换用 `JohannesKaufmann/html-to-markdown` v2 加两条自定义规则（代码块、数据表格转管道表格），对同一批 59 页与 TS 的 `extractWebArticle` 逐页比较：

| 指标 | 结果 |
|---|---|
| 选出的正文来源（Readability / 区域回退 / 整页 / 公众号）一致 | 59 / 59 |
| 标题一致 | 59 / 59 |
| Readability 与语义区域的段落字数都一致 | 49 / 59 |
| 去掉 Markdown 符号后的纯文字相似度 ≥0.99 | 54 / 59（其余 4 页 0.95–0.99，1 页 0.80–0.95，最低 0.940） |
| Markdown 去空白后相似度 ≥0.99 | 50 / 59 |
| Markdown 逐字相同 | 7 / 59 |

结论分两层：

- **内容层：基本等价。** 每一页都选中了同一块正文，没有选错区域或整块丢失，纯文字相似度最低 0.94（差异来自个别元素取舍）。
- **字节层：不等价。** Markdown 逐字相同只有 7 页，差异来自两个库的写法：Turndown 转义 `]`、html-to-markdown 不转义；html-to-markdown 会重新编码链接 URL（把查询串里的 `/` 改成 `%2F`，并对括号、方括号、空格做百分号编码）；列表标记后的空格数也不同。URL 被改写会改变链接的字面内容。要字节级一致，就得自己写一个 Turndown 的 Go 移植（需处理空白折叠与转义规则，工作量未估算），而不是用现成库；或者接受换引擎后 `contentHash` 全变，由 `WEB_EXTRACT_ENGINE` 版本号（现为 `readability/1`）标明引擎变化，已入库的页面按新引擎版本重新比较。
- 10 页的段落字数（`readabilityChars`）不一致，但正文来源的决定没有因此翻转。原因没有深查：最可能是 Readability 输出里 `div` 与 `p` 的划分，跟 linkedom 与 `x/net/html` 的解析差异有关。回退阈值是 0.6 的比例，目前样本里没有页面因此越线，但越线的风险存在。

**决定（2026-11-11）：** 接受 Markdown 字节差异，不移植 Turndown。换引擎时提高 `WEB_EXTRACT_ENGINE` 版本号，已入库页面按新引擎版本重新比较。前提：内容层等价（上表前两行）在更大样本上仍成立；若留出样本出现正文选错，再重新评估是否移植 Turndown。

仍没有验证：`sanitize-html`、GBK 等非 UTF-8 站点、非技术类网站的留出样本、微信公众号页面、正文超长截断的页面。
运行方式（语料目录与输出目录自行指定）：

```powershell
node spikes/mygo-readability/make-golden.mjs <样本目录> <golden 目录>
pnpm dlx tsx spikes/mygo-readability/route-check.mts <样本目录> <golden 目录>
pnpm dlx tsx spikes/mygo-readability/make-article-golden.mts <样本目录> <golden 目录>
cd spikes/mygo-readability
go run . <样本目录> <golden 目录> stock
powershell -File prepare-patched.ps1
go run -tags patched "-modfile=go.patched.mod" . <样本目录> <golden 目录> product
go run -tags patched "-modfile=go.patched.mod" ./cmd/compare-extract <样本目录> <golden 目录>
```
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
## 工作量与继续条件（2026-11-11）

规模（`Measure-Object -Line`，非空行，不含测试）：主进程 TS 约 4.0 万行（其中 `ipc` 0.47 万、`services` 3.2 万），`packages/db` 0.83 万，`packages/core` 0.16 万；已写的 Go 约 3000 行加约 700 行测试。冻结功能涉及的目录（`research`、`illustration`、`reading-sync`、`discovery`、`mobile-capture`）合计约 0.27 万行。

- 需要迁的核心代码约 4.4 万行 TS，已迁约 7%（按频道数 9%，按代码行数略低，因为剩下的多是采集、导入、媒体、AI 这类体量大的服务）。
- 本次 spike 的 Go 与被替换的 TS 行数大致 1:1；按此，剩余约 4 万行 Go 加对应的对拍基准。这是量级估计，不是工期承诺。
- 最大的几块：`import` 0.71 万、`web-capture` 0.50 万、`media` 0.49 万、`themed-reading` 0.44 万、`platform-capture` 0.26 万。

没有现成等价物、需要单独验证输出一致性的 JS 库（`apps/desktop/package.json`）：`@mozilla/readability` + `linkedom`（网页正文提取）、`turndown`（HTML 转 Markdown）、`sanitize-html`、`css-tree`、`@modelcontextprotocol/server`（MCP）、`@resvg/resvg-js`（SVG 渲染）、`adm-zip`/`fflate`。Go 里有同类库；正文提取的底层（Readability）已验证基本等价，见「正文提取等价性」，但其余规则、Markdown 转换与净化库尚未验证。网页正文提取的差异会直接改变用户入库的内容，仍是最大的行为风险。

继续迁移的建议顺序与停止条件：

1. **先做一个端到端垂直切片，再铺开频道。** 切片：采集进程抓一个真实网页 → 正文提取与转 Markdown → 入库与 FTS → 带流式输出的 AI 问答。它同时覆盖三个最大的未知项（独立采集进程的真实站点表现、正文提取的等价性、AI 流式请求），其余频道主要是已有模式的重复。
2. **停止条件（任一成立就暂停，回头评估是否保留 Node 辅助进程或放弃）**：正文提取在对拍语料上与 TS 的差异无法收敛；真实站点上独立采集进程的稳定性明显低于 Electron；新旧版互斥使用造成的数据风险无法接受。
3. **不做的事**：不迁冻结功能；不在没有对拍基准的情况下迁带副作用的频道；不把 spike 目录直接当作产品代码合并进主线。
## 建议的下一步

1. 服务边界已定，网络拦截已有可行路径。可以继续迁移。
2. 用真实站点和多 target 并发压测独立采集进程；验证备份恢复流程（`pre-restore`、换库、重启）与加密备份格式。
3. 如果继续迁：按「纯读库 → 写库 → 带副作用」的顺序，沿用差分对拍，每迁一组频道就补一组基准。
