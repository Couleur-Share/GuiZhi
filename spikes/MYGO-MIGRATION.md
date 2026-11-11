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
| AI 请求与流式 | i:httpRequest、i:httpStream、i:httpCancel：48 个场景与 TS 原版对拍，47 个一致，1 个是 Go 版更严（见「AI 流式请求切片」） |

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
- 已验证（见「采集进程的真实页面与并发压测」）：多个并发 target 的拦截隔离、崩溃恢复。还没验证：采集进程池的复用策略、真实互联网站点与反爬。仅在主页面启用拦截会漏掉 `window.open` 弹窗的首个请求。
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

### 盲区验证：字符集、超长截断、sanitize-html（2026-11-11）

只用本机样本与合成输入，不访问外网。

**1. 字符集解码**（`cmd/charset`）。产品静态抓取用 `new TextDecoder(label, {fatal: true})` 解码，只看响应头里的 charset，不嗅探 `<meta>`；解码失败就回退到渲染路径（由 Chromium 自己解码）。Go 侧用 `x/text` 的 `htmlindex` 对拍 7729 例：

| 类型 | 用例数 | 两边相同 | Go 判失败而 Node 成功 | Go 成功而 Node 失败 | 两边成功但文本不同 |
|---|---|---|---|---|---|
| 标签识别（69 个标签与大小写变体） | 69 | 69 | 0 | 0 | 0 |
| 合法编码文本（11 种编码） | 898 | 879 | 0 | 1 | 18 |
| BOM | 138 | 127 | 4 | 4 | 3 |
| 特殊序列（GBK 四字节等） | 24 | 15 | 8 | 0 | 1 |
| 随机字节 | 6600 | 5666 | 817 | 59 | 58 |

- 标签识别完全一致；合法文本 879/898 相同。18 例是 Big5 扩展区（假名、西里尔字母）：Go 解成原字符，WHATWG 解成私用区字符；1 例是 EUC-KR 的 `a2e6`（欧元号）：Go 接受，WHATWG 判非法。
- 差异几乎都出在畸形字节上：未定义字节（windows-1252 的 0x81、iso-8859-2 的 0x80–0x9F 等）在 WHATWG 里映射为 C1 控制字符，`x/text` 当作非法。
- 「Go 判失败」这一类在产品里等于回退到渲染路径，是安全降级；「Go 成功而 Node 失败」与「文本不同」会得到乱码或静默差异，随机字节里共约 2%。
- 试了两处修正：`gbk`/`gb2312` 标签改用 GB18030 解码器，UTF-16 丢弃开头 BOM。BOM 的文本差异 3→0，GBK 四字节序列从失败变成可解码，但有 3 个特殊序列的映射仍与 WHATWG 不同（例如 `84 31 a4 39`）。修正由环境变量 `CHARSET_FIX=1` 打开，尚未并入主路径。
- 最省事的完全一致办法：Go 版对任何非 UTF-8 的 charset 一律走渲染路径，代价是 GBK、Big5 等静态页变慢。

**2. 超长截断**（`cmd/compare-synthetic`）。9 个合成用例，含 199,999 / 200,000 / 200,001 码点的单段文本、全是代理对的 10 万与 25 万个表情、码点数恰为 20 万但 UTF-16 长度更长的混合文本、30 万字多段中文。Markdown 内容、截断标志与正文来源 9/9 逐字节相同。

**3. sanitize-html**（`extract/sanitize.go`，`cmd/compare-sanitize`）。这段逻辑用于微信快照，并在数据库或备份读出时再清理一遍，属于安全边界。Go 用 `x/net/html` 加手写白名单移植 HTML 部分，与产品 `cleanHtml` 对拍 35 个攻击用例与 59 个真实网页，两边输出都重新解析、去掉 `style` 后比较：

- DOM 完全一致 92 / 94；其余 2 条分别是 `noscript` 用例（见下）与一个 `<form>` 内的空白差异（martinfowler 页，纯空白，文字一致）。
- 攻击用例 34 / 35 一致：覆盖脚本、事件属性、`javascript:`（含实体、制表符、大小写变体）、`data:`、协议相对 URL、SVG 内容、`<math>`、`iframe`/`form`/`object`、注释与 CDATA、未闭合标签、标签粗糙拼接等。
- 移植时发现并照搬了三处产品行为：`sanitize-html` 丢弃空值属性；`img` 的 `src` 只放行本快照登记的 `local-image`，其余替换为 `alt="图片未保存"`；`nonTextTags` 里的 `foreignobject` 实际不生效（htmlparser2 在 SVG 内保留标签大小写，`foreignObject` 匹配不上），所以 `foreignObject` 里的 `div` 会保留，再经白名单过滤。
- 剩下的不一致是解析器差异：`<noscript><p title="</noscript><img src=x onerror=...">` 里，`x/net/html` 把 `noscript` 当原始文本，在第一个 `</noscript>` 处结束，htmlparser2 则不是。Go 的输出里没有事件属性和可执行内容，`img` 被去掉 `src` 并替换为占位，残留的是转义后的文字 `">`，是安全的，但文字与 TS 不同。
- `style` 属性与样式表由 `cleanCss` 处理，另见下一小节；`CleanHTML` 本身一律丢弃 `style`，HTML 对拍时两边都去掉 `style` 再比较。
- 没有微信公众号的真实 HTML 样本，快照路径的完整行为未验证。

**4. CSS 白名单 `cleanCss`**（`extract/css.go`，`cmd/compare-css`）。产品用 `css-tree` 解析，按属性白名单、`url()` 必须能映射到本快照资源、拒绝 `expression`/`var`/`env`/`paint`/`image-set`、`position` 只允许 `relative`/`absolute`/`static`、样式表中的 at 规则整体移除。Go 用 `tdewolff/parse` 的 CSS 解析器移植（约 260 行）。对拍输入是 59 个真实网页里的全部 style 属性（19,980 条）与 `<style>` 块（372 个），加 40 条攻击用例，每条用「拒绝所有 url」和「映射所有 url」两种映射各跑一次，两边输出去掉空白、注释、url 引号后比较：

| 类别 | 总数 | 一致 |
|---|---|---|
| 真实网页的 style 属性 | 39,960 | 39,960 |
| 真实网页的 `<style>` 块 | 744 | 744 |
| 攻击用例（行内） | 60 | 54 |
| 攻击用例（样式表） | 20 | 18 |

真实数据全部一致。攻击用例里 6 条不一致，都是 Go 更严格或写法不同，没有 Go 放行而产品拒绝的情况：

- 函数名里的 CSS 转义：产品直接拿原文比较，`i\6d age-set(...)`、`v\61r(--x)`、`ex\70ression(...)` 都能通过黑名单；Go 先解码转义再匹配，所以拒绝。这是产品现有实现的缺口：我在原目录之外的测试里确认 `background-image:i\6d age-set("https://…")` 会被原样保留。快照文档的 CSP 是 `default-src 'none'; img-src local-image:`，外链图片与 `image-set` 会被挡住，所以不构成可利用漏洞，属于纵深防御缺口；本次没有改产品代码。
- 解析失败的片段：产品会把 css-tree 无法解析为声明的片段原样保留（如 `color:;`、没有冒号的 `padding`），Go 一律丢弃。
- 嵌套规则（CSS nesting）：产品原样保留，Go 整体丢弃。第一版 Go 把嵌套规则的选择器错误合并成 `.a,.b{…}`，改成丢弃后修正。
- 属性名大小写：产品保留，Go 小写化，语义相同。
- 值里出现冒号（如缺分号的 `justify-content:space-between overflow:hidden`）：产品和浏览器都当作无效声明丢弃，Go 起初放行，已加规则丢弃。

产品输出里还有三类对渲染无影响的内容，对拍时忽略：`/*! … */` 注释、空值声明（`vertical-align:;`）、声明被清空的规则（`.a{}`，Go 也保留以便一致）。

仍没有验证：非技术类网站的留出样本（不在本机的 47 页）、微信公众号真实快照、带 `charset` 的真实 GBK 站点（本次是合成字节，不是真实页面）。
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
## 收益量化（2026-11-11）

在同一台机器上、各自隔离的数据目录里，对比打包后的 Electron 版（本分支源码构建出的 `win-unpacked`，Electron 45.2.3）与 MyGo 壳层原型（同一份渲染进程构建、同一个 0.8 MB 的数据库副本）。就绪判据相同：顶栏搜索框 `topbar-search` 出现在 DOM 里。各跑 7 次，第 1 次是新建的空配置（冷），后 6 次复用配置（热），每次就绪后空闲 10 秒再采样进程树内存。脚本在 `spikes/mygo-bench`，壳层加了 `-bench` 模式。

| 指标 | Electron 版 | MyGo 原型 |
|---|---|---|
| 程序体积 | 404 MB（`win-unpacked`，其中 `GuiZhi.exe` 238 MB、语言包 49 MB、`resources` 48 MB） | 约 23 MB（可执行文件 19.7 MB 加渲染产物 2.9 MB） |
| 首屏就绪，热启动中位数（范围） | 1097 ms（1062–1240） | 1060 ms（804–1543） |
| 首屏就绪，冷启动（各 1 次） | 1365 ms | 937 ms |
| 空闲私有工作集（任务管理器「内存」口径），热启动中位数 | 362 MB | 218 MB |
| 空闲工作集（含共享页），热启动中位数 | 647 MB | 519 MB |
| 进程数 | 5 | 8 |

- **体积差异确凿。** MyGo 版不带浏览器内核，依赖系统里的 WebView2 运行时（本机已装 157.0.4341.42，没有计入上面的 23 MB）。本机装了运行时不能代表目标用户的机器；官方只支持 Windows 11，WebView2 运行时是否随系统预装，本次没有核实，发版前要确认并决定是否随包带引导安装程序。
- **启动速度没有可测的改善。** 热启动中位数 1060 对 1097 ms，MyGo 的离散度更大（804–1543 ms），冷启动各只有 1 次，不能下结论。
- **内存的差距是上界，不是预期值。** 原型只实现了约 20 个频道，Electron 版的主进程带着全部服务（是否都在启动时载入，没有核实）；等 Go 端补齐采集、AI、后台任务后，空闲内存会上升，上升多少未知。另外 WebView2 的多进程（8 个进程、工作集 519 MB）本身就占了大头，差距有多少来自 Node 主进程、多少来自别处，这次没有拆开。
- **口径限制。** Electron 版经 Playwright 启动，会多一点连接开销；两边只测了一台机器、一个很小的数据库，没有测大库（数万条）下的内存与首屏；没有测安装包（NSIS）压缩后的大小；没有测更新、托盘等后台常驻场景。

这些数字回答的是「换内核能得到什么」：体积减少约 94%；空闲私有工作集少约 40%、含共享页的工作集少约 20%（都是上界）；启动速度没有可测的变化。它们能否抵得上 4.4–8 万行 Go 的迁移成本，是决策问题，不是技术问题。
## AI 流式请求切片（`spikes/mygo-ai`、`mygo-shell/ai_*.go`，2026-11-11）

目标：把最后一个大未知项查清——流式请求在 MyGo 里到底行不行，尤其是「Go 往页面推事件」「请求挂起时还能取消」「守卫不被绕过」。

**做法**：本地模拟服务（`spikes/mygo-ai-mock`，只监听 127.0.0.1，不访问外网）提供 SSE、畸形字节、挂起、断连、重定向、gzip、各类状态码。同一批 48 个场景（`scenarios.json`）用同一个运行器（`runner.mjs`）跑两遍，都经**真实 preload 的 `aiApi`**：

- TS 侧：真实的 `ai.ipc.ts`、`ai-endpoint-guard.ts`、`net-safety.ts`，只把 `electron` 换成内存总线，`fetch` 是 Node 自带的 undici（`ts-run.mts`）。
- Go 侧：壳层 `-aitest` 模式把运行器注入真实渲染页，请求走 WebView2 → `Bridge.Invoke` → `ai_proxy.go`，块事件由 Go 经 `ipc` 事件推回页面，取消走 `Bridge.Send`。

场景覆盖：流式基本与 UTF-8 多字节被切开、畸形字节、3 MB 大流、5000 个小事件、非 2xx（不走流）、服务端中途断连、中途取消、响应头前取消、响应体中途超时、响应头前超时、连接被拒、多请求并发、取消其中一个、普通请求的 JSON/401/429/500/POST 回显/重定向/gzip/204/超时/取消、守卫（元数据地址的多种写法、协议、非法 URL、数字 IP 的十进制/十六进制/带点写法、userinfo 混淆）、未知主机限速。比较的是 ok、状态、状态文本、错误文案、响应体或其长度与哈希、流式拼接文本的长度与哈希、流错误列表、`content-type` 与自定义响应头；块数与耗时只作参考。

### 结果

| 项 | 结果 |
|---|---|
| 端到端对拍 | 48 个场景，47 个逐字段一致；连续跑 3 次结果相同 |
| 唯一的不一致 | `redirect-to-metadata`：服务端 302 到 `http://169.254.169.254/`。TS 照跟，最后因为地址不通撞上超时（`AI 请求超时（1500ms）`）；Go 在拨号时拦下（`不允许访问该地址: 169.254.169.254`）。**这是 Go 版更严，不是缺陷**：TS 的守卫只校验第一跳 URL，重定向的下一跳和 DNS 重绑定都不过守卫 |
| 守卫的地址解析 | 87 条 URL 与 Node 的 WHATWG `URL` 逐条对拍（`host`、`hostname`、协议、合法性）全部一致；46 个地址的 `isForbiddenAIEndpointAddress` 全部一致；`evaluateEndpointAccess` 204 个状态组合全部一致 |
| 流式 UTF-8 解码 | 600 组随机字节加随机切块，对拍 Node 的 `TextDecoder`（stream 模式），0 组不一致；另有 4 组只差 U+FEFF（见下） |
| 3 MB 流、5000 小事件 | 拼接结果与 TS 逐字节一致，事件没有丢失、没有乱序、没有在返回之后才到 |
| 并发与取消 | 并发 3 路流互不影响；一路挂起时取消另一路和取消自己都立即生效。**MyGo 没有把绑定方法的调用串行化**（取消的 `Bridge.Send` 不会排在挂起的 `Bridge.Invoke` 后面），这是方案成立的前提 |
| 计时 | 取消、超时的触发时刻与 TS 在 10 ms 量级内一致（Go 400/710/505 ms，TS 403/712/514 ms） |

### 必须单独处理的差异

1. **地址解析不能用 `net/url` 和 `netip`。** `http://2852039166/`、`http://0xa9fea9fe/`、`http://169.254.169.254./`、`http://127.1/` 在 WHATWG 里都是 IPv4 字面量，Go 的 `net/url` 当域名处理。`ai_net.go` 按 WHATWG 重写了主机解析（IPv4 的十进制/八进制/十六进制/省略段、IPv6 的十六进制序列化与零段压缩、权限部分切分、端口范围、百分号解码），用上面的 golden 守住。守卫是安全边界，这部分以后只要改动就必须重跑对拍。
2. **错误文案要按阶段还原。** Node 的 `fetch failed`（连接阶段）与 `terminated`（响应体读取阶段）在 Go 里没有对应物，`ai_proxy.go` 按错误发生的阶段补回；取消与超时的文案用 `context.Cause` 带出。DNS 失败的文案不同（Node 是 `getaddrinfo ENOTFOUND x`，Go 是自己的措辞），场景未覆盖，渲染进程不依赖它。
3. **UTF-8 解码要自己实现。** Go 的 `string([]byte)` 把每个坏字节替换成一个 U+FFFD，WHATWG 按最大合法前缀折成一个；流式解码还要跨块保留未完成的序列。`ai_utf8.go` 按 Encoding 标准的状态机实现。残留差异：Node（ICU）会在「不完整序列之后紧跟的 BOM」「连续两个 BOM」处额外吞掉 U+FEFF，偏离了规范，Go 按规范只剥流开头的一个；对 SSE 文本没有实际影响。
4. **undici 的隐式请求头。** 字符串 body 且调用方没给 `Content-Type` 时 undici 补 `text/plain;charset=UTF-8`、默认 `User-Agent: node`；Go 版照补（已用 Node 实测确认这两个值）。undici 还会发 Accept: */*、Accept-Language: *、Sec-Fetch-Mode: cors，Go 版没有照搬，没有发现服务端依赖它们，但真实服务未测。

### 与产品 TS 版刻意不同的地方

- 拨号时再校验一次实际要连的地址（`net.Dialer.Control`）：关闭 DNS 重绑定和重定向到元数据地址两个缺口。
- 网络代理设置未接入，一律直连。这是 AI 请求切片里剩下的唯一功能缺口，要等「设置里的网络代理生效」一起做。

### 顺带发现的产品问题（TS 原版，未改）

- `isLoopbackHost` 用 `/^127\./` 判回环，任何以 `127.` 开头的**域名**（如 `127.evil.com`）都被当成回环，绕过未知端点的限速。Go 版为了保持对拍一致照搬了这一行为（`ai_net.go` 的 `isLoopbackHostname`，golden 里有对应用例）。修法很小：先判是不是 IPv4 字面量，再判前缀。

### 复跑

```powershell
# 单元级对拍（守卫、地址、UTF-8）：golden 已提交在 mygo-shell/testdata/
cd spikes/mygo-shell; go test -run 'TestEndpoint|TestForbidden|TestUTF8' -v .
# 重新生成 golden
pnpm dlx tsx spikes/mygo-ai/make-net-golden.mts spikes/mygo-shell/testdata/ai-net.json
node spikes/mygo-ai/make-utf8-golden.mjs spikes/mygo-shell/testdata/ai-utf8.json
# 端到端（需要先 go build 出壳层与模拟服务；脚本自动起停模拟服务，用临时数据库副本与临时 WebView2 目录）
cd spikes/mygo-ai-mock; go build -o ../mygo-ai-mock.exe .
cd ../mygo-shell; go build -o ../mygo-shell-ai.exe .
powershell -File spikes\mygo-ai\run-diff.ps1 -ShellExe <壳层.exe> -Dist <dist> -DbFile <fixture 数据库>
```

### 没覆盖的

- 真实的第三方 AI 服务（OpenAI、Anthropic、Gemini 的真实 SSE 与限流行为）、HTTP/2 与 TLS 细节：只测了本地明文 HTTP/1.1。
- 用户配置了网络代理（系统代理、HTTP/SOCKS）的环境。
- 超长空闲的流（undici 默认 5 分钟的头/体超时没有照搬）。
- 渲染进程真实的问答界面（流式渲染、停止按钮）：这里测的是它依赖的 `aiApi` 契约，不是界面。
- 事件吞吐的上限：最大测到 3 MB（Go 版 97 个事件、TS 版 232 个）；没有测每秒数千个事件的洪峰。
## 备份恢复与加密仓库切片（`spikes/mygo-restore`，2026-11-11）

目标：确认加密备份仓库的格式和恢复流程能在 Go 里重写，且和 TS 版双向互通。方法：用产品的真实 TS 代码（`backup-repository*.ts`、`backup-portable.ts`）和 Go 版各自创建、读取、恢复，再对拍结果。全部在隔离的临时目录进行，不碰用户数据目录和运行中的实例。

### 结果（`run-diff.ps1`，9 组，失败 0 项）

| 组 | 内容 | 结果 |
|---|---|---|
| 1 | TS 创建的仓库，Go 读取清单与全部对象 | 一致（含第二个快照复用旧对象） |
| 2 | 口令：全角/emoji、NFKC 等价写法、错误口令与过短口令的提示文本 | 一致 |
| 3 | 同一“现场”分别由 TS 与 Go 恢复：文件树、设置、条目、机器相关设置保留、暂存与回滚目录清理 | 一致 |
| 4 | Go 创建的仓库：TS 解开、预检通过、恢复结果与 TS 自己的快照一致 | 一致 |
| 5 | 混合仓库：Go 往 TS 仓库加快照，对象跨实现去重（复用 ≥6 个） | 通过 |
| 6 | 预检矩阵：完好、快照不存在、id 非法、对象改一字节、对象缺失、清单被篡改、仓库版本不支持 | 7 项判定与文本一致 |
| 7 | 改口令（双向）、旧口令失效、删除快照后对象回收 | 一致 |
| 8 | 便携包导出：解压后条目与内容哈希相同；目标已存在时拒绝覆盖 | 一致 |
| 9 | 换库第 1/2/3/5/7 步后注入失败：回滚后与换库前完全相同 | 全部回滚 |

### safeStorage（无人值守解锁）

用 Electron 45.2.3 在隔离 userData 里实测（`safestorage-test.ps1`，4 项全过）：

- Windows 上 safeStorage 密文是 `v10` + 12 字节 nonce + AES-256-GCM 密文 + 16 字节 tag；密钥是 `<userData>/Local State` 里 `os_crypt.encrypted_key`（`DPAPI` 前缀 + `CryptProtectData`）解开的 32 字节主密钥。Go 用 `x/sys/windows` 的 `CryptUnprotectData` 即可，不需要 cgo。
- Electron 加密的仓库密钥，Go 能据此无人值守解锁并读完快照；Go 加密的 `v10` 密文，Electron 能解开。
- 删掉 `Local State` 后 Go 解锁失败，不会回落成明文，与产品约定一致。

**迁移约束**：已有仓库头里的 `safeStorageWrappedKey` 绑定在旧 Electron 的 `Local State` 上，新版必须能读到旧 userData 里的这个文件，否则无人值守备份只能退回口令。新版自己的新建仓库用什么包装（继续写 Electron 同格式以便新旧互读，还是改成直接 DPAPI）是待定的设计点；Go 版目前只实现了读取和测试用的写入。

### 与 TS 版刻意或被迫不同的地方

- 快照数据库来自 `VACUUM INTO`：MyGo 内置 SQLite 拒绝该语句，Go 版用 modernc 的只读连接执行。
- GCM 校验失败时，Go 版返回与 Node 完全相同的文案“Unsupported state or unable to authenticate data”，供界面沿用。

### 没覆盖的

- 主题阅读资产的检查与校验（依赖主题阅读服务，冻结范围之外但仍属核心，待迁时补）。
- `sanitizeSnapshot` 只校验了结构；TS 里它的清理结果本来就被丢弃。
- 恢复前后的编排：`pre-restore` 备份、停后台任务、关库与重启、活动任务检查。这里只验证了“换库”这一步及其回滚。
- 渲染进程设置的整数式键排序（JS 对象的排序规则）未模拟。
- 没有按真实体量（数百 MB 媒体）测过耗时与内存；新旧版 SQLite 锁冲突仍未验证。

### 复跑

```powershell
# 构建（在 spikes\mygo-restore 下）
go build -o ..\mygo-restore.exe .
# 全部对拍（在仓库根目录；FixtureDb 是一份产品数据库副本）
powershell -File spikes\mygo-restore\run-diff.ps1 -GoExe spikes\mygo-restore.exe -FixtureDb $env:TEMP\guizhi-shell.db
# safeStorage 互通：-SrcRepo 用上面某次运行目录里的 r-ts，-Snapshot 用 make.json 里的快照 id
powershell -File spikes\mygo-restore\safestorage-test.ps1 -GoExe spikes\mygo-restore.exe -Electron <electron.exe> -SrcRepo <r-ts> -Snapshot <id>
```
## 采集进程的真实页面与并发压测（`spikes/mygo-capstress`，2026-11-11）

目的：补上「独立采集进程」此前没验证的部分——多 target 并发的拦截隔离、故障下的行为、崩溃恢复、内存。输入是本机 59 页真实网页的静态 HTML，回放到本地模拟站点（另在 127.0.0.2 上放一个“内网”探针站点）。**不访问外网**：采集进程带 `--host-resolver-rules` 兜底，并且所有非本地请求同时在 `Fetch` 阶段被拦（59 页共拦掉约 790 个外部子资源请求）。

**离屏运行**：压测本体在独立的 Win32 桌面里启动（`launcher.go`，`CreateDesktop`），采集进程的窗口和页面弹窗都不会出现在用户桌面、任务栏，也不抢焦点。这同时说明：采集进程在非交互桌面里也能正常工作。

### 结果

| 项 | 结果 |
|---|---|
| `Target.createTarget` 每个任务一个页面 | 可用；59 页串行全部成功，标题与源文件一致 59/59，3.5 页/秒 |
| 并发 3 / 8（各 177 个任务） | 全部成功；4.5 / 4.3 页/秒（并发超过 3 后吞吐不再增加，单页耗时从 0.66 秒涨到 1.8 秒） |
| 请求头串扰（每个任务给自己的文档请求注入唯一令牌，服务端校验） | 354 个主文档请求，串扰 0、缺失 0 |
| 并发任务各自的拦截规则（一半全放行、一半全拦截） | 36/36 行为正确 |
| target 泄漏 | 每个场景之后页面数都回到基线 |
| 故障：慢响应超时、HTTP 500、重定向死循环、连接重置、下载、三种 JS 弹窗、脚本死循环、30 MB 页面、外部资源、渲染进程崩溃 | 均未拖垮并发的 6 个正常任务；卡死页面取值超时后 0.5 秒内可关闭 |
| 采集进程被杀 | 3 个在途任务 0.13–0.25 秒内收到“连接已断开”；无残留子进程；重启 0.45 秒，随后 20/20 成功 |
| 内存（300 个任务，并发 3） | 进程树工作集从约 520 MB 涨到约 800–850 MB（三次测量相对起点 +284 到 +326 MB）；进程数 7→8 |

### 安全探针（SSRF 相关）

先做正向对照：放行 127.0.0.2 时，重定向、iframe、弹窗三个探针都能命中内网，所以下面“没命中”的结论可信。

- **302 与 meta refresh 跳到内网地址**：在跳转那一跳被 `Fetch` 拦下，内网零命中。
- **跨站 iframe**：只在主页面启用 `Fetch` 也没有漏（对照组零命中）；开了递归自动接管后同样零命中。
- **`window.open` 弹窗：有缺口。** WebView2 没有 Chromium 的弹窗拦截，无论有没有用户手势，页面脚本都能弹出窗口；弹窗的首个导航请求在我们接管并启用 `Fetch` 之前就已发出，内网被命中。浏览器级 `Target.setAutoAttach`（暂停等待调试器）接管得到弹窗，但拦不住这个首请求。
- **缓解（实测，且已验证宿主页确实加载成功，避免“页面没起来所以没命中”的假阳性）**：
  - 在每个新文档里用 `Page.addScriptToEvaluateOnNewDocument` 把 `window.open` 置空，只挡住 `window.open` 与 `iframe.contentWindow.open`；脚本 `click()` 一个 `<a target=_blank>`、`form[target=_blank].submit()`、`<base target=_blank>` 加链接点击这三条路径照样弹窗并命中内网（5 条路径里 3 条漏）。
  - 通过 `Fetch.continueResponse` 给文档响应加 `Content-Security-Policy: sandbox`：**无效**。页面能正常加载（59 页结果与不加时一致，耗时 +15%），但 `window.origin` 仍是真实来源，弹窗照开。WebView2 下改写响应头没有让 CSP 生效。（这条路径曾因 `continueResponse` 少传 `responseCode` 而整页挂死、使探针“没命中”，是靠加宿主页加载校验才发现的。）
  - 浏览器参数 `--block-new-web-contents`：不可靠，两次运行一次仍弹出并命中内网，一次没命中。
  - 另观察到：弹窗打开后，打开者页面的下一次 `Runtime.evaluate` 在 8 秒内无响应（压测在离屏桌面里运行，真实桌面是否相同未验证），弹窗不只是安全问题，也会拖住采集任务。
- **结论**：目前没有已验证有效的弹窗封堵办法。可选方向：（a）在 MyGo 层查新窗口的拦截点（`NewWindowRequested`），拒绝一切新窗口，这是根本办法；（b）采集进程走按进程设置的过滤代理，对所有目标一视同仁；（c）接受残余风险，仅对“放行的站点”采集。选择前，涉及不可信页面的采集都不应在此架构上上线。

### 兜底参数的一个坑

`--host-resolver-rules="MAP * ~NOTFOUND, EXCLUDE ..."` 会把 IP 字面量也当作“待解析主机”：不排除 127.0.0.2 时，访问它得到 `ERR_NAME_NOT_RESOLVED`，探针看起来“没漏”，其实是被这条规则挡的。这次是靠正向对照发现的。产品里若用这条规则兜底外网，要知道它对 IP 字面量同样生效，且不能代替 `Fetch` 的 SSRF 检查。

### 没覆盖的

- 真实互联网站点（本次刻意只用本机语料）：反爬、慢站、证书错误、真实的 JS 渲染站点。
- 语料是静态 HTML 回放，页面里的外部脚本全被拦掉，所以没有测大量脚本执行与长时间渲染的稳定性。
- 内存增长是否收敛：三次测量的增量并不单调，不能据此断言有或没有泄漏。保守做法是每 100–200 个任务回收一次采集进程（重启 0.45 秒）。
- 带登录态的会话、`browserContext` 隔离、长时间（小时级）运行。
- 弹窗之外的其他“新目标”路径（见上）。
- 弹窗是否也会拖住真实桌面上的采集（本次在离屏桌面运行）。

### 复跑

```powershell
# 构建（在 spikes\mygo-capstress 下）；语料目录默认 %TEMP%\guizhi-crawl-eval\cache
go build -o ..\mygo-capstress.exe .
# 全部场景，默认离屏运行（日志在 %TEMP%\capstress-*.log）；-run 可选 base,conc,rules,faults,security,popup2,sandbox,kill,mem
.\spikes\mygo-capstress.exe
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

规模（`Measure-Object -Line`，非空行，不含测试）：主进程 TS 约 4.0 万行（其中 `ipc` 0.47 万、`services` 3.2 万），`packages/db` 0.83 万，`packages/core` 0.16 万；已写的 Go 约 7500 行（含测试，其中壳层与频道约 3700、正文提取与清理约 2400，其余是网络拦截、采集进程、备份、数据库互通等 spike）。冻结功能涉及的目录（`research`、`illustration`、`reading-sync`、`discovery`、`mobile-capture`）合计约 0.27 万行。

- 需要迁的核心代码约 4.4 万行 TS，已迁约 7%（按频道数 9%，按代码行数略低，因为剩下的多是采集、导入、媒体、AI 这类体量大的服务）。
- Go 与被替换的 TS 的行数比，读库、写库、IPC 这类代码大致 1:1；DOM 与文本处理这类代码更高：正文提取、Markdown 转换、HTML 与 CSS 清理，非空行 Go 1565（含一份约 240 行的 DOM 辅助）对 TS 846，约 1.6–1.85 倍。剩余约 4.4 万行 TS，按 1:1 到 1.85:1 的区间，估计 Go 约 4.4–8 万行，另加对应的对拍基准。这是量级估计，不是工期承诺。
- 最大的几块：`import` 0.71 万、`web-capture` 0.50 万、`media` 0.49 万、`themed-reading` 0.44 万、`platform-capture` 0.26 万。

没有现成等价物、需要单独验证输出一致性的 JS 库（`apps/desktop/package.json`）：`@mozilla/readability` + `linkedom`（网页正文提取）、`turndown`（HTML 转 Markdown）、`sanitize-html`、`css-tree`、`@modelcontextprotocol/server`（MCP）、`@resvg/resvg-js`（SVG 渲染）、`adm-zip`/`fflate`。Go 里有同类库。已验证：Readability、产品规则、Markdown 转换、`sanitize-html` 与 `css-tree` 在本机 59 页上内容层等价（Markdown 字节不等价，已决定接受），见「正文提取等价性」。AI 请求代理已对拍（见「AI 流式请求切片」）；备份仓库、恢复与便携包已与 TS 双向对拍（见「备份恢复与加密仓库切片」）。未验证：`@modelcontextprotocol/server`、`@resvg/resvg-js`、`adm-zip`/`fflate` 在产品里的其他用法，以及本机之外的留出样本。网页正文提取的差异会直接改变用户入库的内容，仍是最大的行为风险。

继续迁移的建议顺序与停止条件：

1. **先做一个端到端垂直切片，再铺开频道。** 切片：采集进程抓一个真实网页 → 正文提取与转 Markdown → 入库与 FTS → 带流式输出的 AI 问答。它同时覆盖三个最大的未知项（独立采集进程的真实站点表现、正文提取的等价性、AI 流式请求），其余频道主要是已有模式的重复。
2. **停止条件（任一成立就暂停，回头评估是否保留 Node 辅助进程或放弃）**：正文提取在对拍语料上与 TS 的差异无法收敛；真实站点上独立采集进程的稳定性明显低于 Electron；新旧版互斥使用造成的数据风险无法接受。
3. **不做的事**：不迁冻结功能；不在没有对拍基准的情况下迁带副作用的频道；不把 spike 目录直接当作产品代码合并进主线。
## 建议的下一步

1. 服务边界已定，网络拦截已有可行路径。可以继续迁移。
2. 独立采集进程的并发压测已在本机语料上做完（见「采集进程的真实页面与并发压测」），真实互联网站点仍待验证，弹窗缺口需要先决定处理方式；AI 流式请求（见「AI 流式请求切片」）与加密备份格式、换库与回滚（见「备份恢复与加密仓库切片」）已验证；备份恢复外围的编排（`pre-restore`、停后台任务、重启）还没验证。非 UTF-8 站点的处理策略要定：一律走渲染路径，或接受 `x/text` 对畸形字节的差异。
3. 如果继续迁：按「纯读库 → 写库 → 带副作用」的顺序，沿用差分对拍，每迁一组频道就补一组基准。
