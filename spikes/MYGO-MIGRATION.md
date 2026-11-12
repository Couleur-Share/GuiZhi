# 归知迁移到 MyGo：spike 结论与进度

分支 `spike/mygo`，代码都在 `spikes/` 下，不改动主线任何文件。本文是阶段性成果的收束，不是迁移方案定稿。

## 结论

1. **技术上可行，没有发现阻塞性的硬伤。** 复用现有 React 界面（不改一行渲染进程代码），用 MyGo 加 Go 服务替换 Electron 主进程，已经在壳层里跑通：启动、列表、搜索、写入、设置、收件箱、导入列表。
2. **工作量在主进程服务，不在框架。** 渲染进程约有 215 个 `invoke` 频道，已迁约 20 个（约 9%）。剩下的每一类都牵出一整套服务：采集、AI 请求、后台任务调度、Wiki 编译、导入队列、研究、备份、更新、快捷键、手机收集等。
3. **差分对拍是这条路的安全网。** 迁一个频道，就让 TS 原版和 Go 在同一份数据上各跑一遍，逐字段比。这套方法抓出了多处手写时看不出的偏差（见下文「对拍抓到的问题」）。

## 决策摘要（2026-11-11 收束）

**判断：技术上可行，没有发现阻塞性硬伤；是否值得做是成本与收益的取舍，不是技术问题。** 下面只列事实、证据位置和我的建议，数字来自本机实测，不外推。

### 已验证（每项都有可复跑的对拍或压测）

| 能力 | 证据 | 结果 |
|---|---|---|
| 现有 React 界面原样运行、读写频道 | `mygo-shell` 差分对拍 | 约 20 个频道一致 |
| 数据库互通、备份快照 | `mygo-db`、`mygo-backup` | 新旧驱动互读，`VACUUM INTO` 有替代方案 |
| 正文提取、Markdown、HTML/CSS 清理 | `mygo-readability` | 本机 59 页内容层等价；Markdown 字节不等价，已决定接受 |
| AI 请求、流式、取消、SSRF 守卫 | `mygo-ai` | 48 个场景 47 个一致，1 个是 Go 版更严 |
| 加密备份仓库、恢复、便携包、回滚 | `mygo-restore` | 9 组与 TS 双向互通，0 失败；safeStorage 能互解 |
| 独立采集进程：并发隔离、故障、崩溃恢复、SSRF 探针 | `mygo-capstress` | 窗口池方案全部场景 0 失败；弹窗由宿主层拒绝，5 条路径零命中 |

### 收益（上界，见「收益量化」）

体积 404 MB → 约 23 MB（不含系统的 WebView2 运行时）；空闲私有工作集约 −40%（原型只有 20 个频道，补齐服务后会上升）；启动速度没有可测的变化。采集页的吞吐在窗口池下比 `createTarget` 高 2–3 倍，但这是采集进程的实现选择，Electron 版也能做类似的池化，不能算作 MyGo 的收益。

### 成本

需要迁移的核心代码约 4.4 万行 TS，按已测的比例估计 Go 约 4.4–8 万行，另加每个频道一套对拍基准；已迁约 7%。这是量级估计，不是工期。冻结功能（研究、发现、手机同步、正文配图）不迁，旧版 Electron 留作只修安全问题的存档版。

### 还没有验证、可能改变结论的事

1. **真实互联网站点**的采集稳定性与反爬行为（本次刻意只用本机语料，且语料偏技术文档）；留出样本 47 页、微信公众号真实快照。
2. **目标用户机器上的 WebView2 运行时**是否存在，以及随包带引导安装程序的体积。
3. **大库**（数万条）下的内存、首屏与搜索耗时。
4. 新旧版共用同一个库时的 SQLite 锁冲突；灰度期只能互斥使用。
5. 恢复流程外围的编排（`pre-restore` 备份、停后台任务、关库重启）。
6. 窗口池的内存曲线在长序列（上千任务）下是否收敛，换新间隔取多少。
7. `@modelcontextprotocol/server`、`@resvg/resvg-js` 的 Go 替代方案没有验证。

### 建议

- **不建议现在承诺整体重写。** 已经排除的是“做不到”的风险；剩下的主要是“值不值”：用户可见的收益集中在体积和空闲内存，启动速度和功能都没有改善，而成本是数万行 Go 加长期维护两套对拍基准。
- **如果要继续，分两步，并设止损点：** 先花一个有限的阶段做第 1、2、3 项（真实站点试采、WebView2 前置条件、大库基准），这三项都可能直接否决方案，且不需要再写大量服务代码；通过后再按“纯读库 → 写库 → 带副作用”的顺序迁移，每组频道补一组基准。止损条件：正文提取在更大样本上无法收敛；真实站点上采集进程稳定性明显低于 Electron；新旧互斥带来的数据风险无法接受。
- **如果不继续**，这批 spike 仍有可复用的价值：`mygo-ai` 和 `mygo-restore` 里发现的 TS 版行为（例如 `isLoopbackHost` 把 `127.evil.com` 当回环导致绕过限流）、窗口池里踩到的三个并发坑，对 Electron 版也有参考意义。
## 全量迁移进度（2026-11-11 起，目标：核心频道全部迁完）

范围沿用「服务边界」：冻结功能不迁，核心频道全部迁。进度用脚本统计：在仓库根目录运行 `.\spikes\migration-status.ps1`（加 `-List` 列出待迁频道）。统计口径是 `ipc-channels.ts` 的频道字符串是否出现在 `mygo-shell` 的 Go 源码里，只表示「登记了」，是否对拍通过看对应测试。

| 日期 | 切片 | 对拍 | 登记频道（累计 / 待迁） |
|---|---|---|---|
| 11-11 | `collection:create/update/delete`、`tag:create/update/delete` | 11 个场景，逐表比较（含 sortOrder 浮点、null、非字符串名称、大小写不敏感的标签唯一） | 27 / 160 |
| 11-11 | `inbox:organize/markReviewed/acknowledgeImportWarning/aiClassificationSources/applyAiClassification` | 用真实 `inbox.ipc.ts`（electron 换桩）生成基准，约 40 个场景，含 NFKC、Final_Sigma、UTF-16 长度、保留名、新建上限 | 32 / 155 |
| 11-11 | `ask:listSessions/getSession/saveSession/deleteSession`、`askSession:query/meta`（含 JS 值模型 `jsvalue.go`、证据清洗 `evidence.go`） | 232 个证据清洗用例逐字节一致；17 个会话场景（分页游标、搜索、保存/更新证据、置顶与自定义标题、字符串入参的强转） | 38 / 149 |
| 11-11 | `ai:usageRecord/usageSummary/usageClear` | 3 组 36 个调用：按本地自然日聚合、保留期清理、`Number(x)||fallback` 强转、小数天数 | 41 / 146 |
| 11-11 | `semantic:status/listPending/applyEmbeddings/search/clear`（含内容哈希、进程内向量缓存、float32 小端 BLOB、堆容量按 `<limit` 多留一个） | 7 组场景（独立播种库 `semantic-base.db`）；含缓存失效只覆盖本次写入的模型这一 TS 特点、孤立代理项截断、NaN 输出为 null | 46 / 141 |
| 11-11 | `wiki:catalog/backlinkCounts/search/getPage/updatePage/deletePage/status/graph/listRevisions/restoreRevision`（含 `flushWikiInvalidations` 失效重建，事务内读写用内存状态模拟） | 11 组场景（独立播种库 + 带贡献与失效记录的库）：节点/边/历史保留 10 份/手动编辑标记/旧版页面改走建议；适配器的单参数绑定语义（数组按位置参数、布尔、对象报错）也已复刻。对拍时 uuid 键与并列时间戳、图谱边顺序做了稳定化 | 56 / 131 |
| 11-11 | `knowledge:saveDraft` | 4 组 59 个调用：基线冲突（含 patch 等于当前值不算冲突）、标签按基线增删合并（trim+小写键、Map 语义）、参数校验（`Object.keys` 对数字/数组/字符串的行为）。读写之间有极小窗口（TS 是单事务），见源码注释 | 57 / 130 |
| 11-11 | `app:getRuntimePaths`、`app:relaunch`、`log:appError`、`dialog:selectImage`、`image:save/readBase64` | 路径、诊断日志（脱敏、512KB 滚动、字段顺序）、图片选择集授权、保存与读取全部与 TS 对拍；发现 Node 对 `a.png␠`、`nul` 返回不存在而 Go 的 Win32 路径会规范化，已按 Node 行为拒绝；`image:save` 传字符串时不抛错且清空选择集。`app:relaunch` 只验证返回值，未实机重启 | 63 / 124 |
| 11-11 | `backgroundJob:*`（8 个，含 `available` 推送）+ 调度器 | 6 组场景对拍：渲染进程任务领取/续租/完成/失败退避（15 分/1 时/6 时/24 时封顶）/暂停恢复、租约过期重领、upsert 在运行中的保留规则、主进程 handler（成功/抛错/暂停/自定义下次时间/并发多类）。时钟与租约持有者注入，时间戳逐位比较。已知与 TS 不同：Go 的 tick 在互斥锁内串行而非靠 JS 单线程；handler 返回过去的 `nextRunAt` 时两边都会立即重跑（TS 版会把微任务队列占死，属已有行为，未修）。未验证：`backup` handler 尚未接入（等备份迁移）、30 秒定时器与真实窗口推送未实机跑 | 71 / 116 |
| 11-11 | 导入队列：`import:enqueue/queueState/pause/resume/cancel/retry/remove/clearFinished/clearTerminalPreview/clearTerminal`、`dialog:selectImportFiles`、`import:changed` 推送 | 11 组场景对拍（真实 TS `import.ipc.ts` + `ImportQueue` + 持久化，只把抽取换成固定替身）：入队校验（含 50 个登录态上限）、同源合并、并发 2 的排队与暂停/恢复、运行中/排队中取消、重试选项校验与 `verifyWeb` 规则、URL/内容去重、强制重复、来源刷新（含修订快照与「来源不一致」拒绝）、失败/降级/警告、清理预览与清理、启动恢复（含登录态任务停在 `login_required`）。比较范围：返回值、按任务分组的事件序列、`import_tasks` 全列（阶段名取自 `stage_stats`）、`source_records`、`source_capture_revisions`、条目/标签/FTS。发现并修复：判重与入库必须原子——TS 靠单线程同步段，Go 并发 2 个任务时内容相同的两条会同时通过判重，已加 `saveMu`。已知差异：不同任务间事件的交错顺序不比较（只比同一任务的状态序列）；同一毫秒并发的两条相同内容任务，谁是「原件」谁是「重复」不确定（TS 为先进先出）。未验证：URL 类抽取、短链解析（`resolveSourceIdentity`）、热门评论采集、微信快照附着都等采集族迁移；`MobileCaptureDB.observe` 属冻结功能未移植；真实文件选择对话框未实机点过 | 82 / 105 |
| 11-11 | `export:markdown`、`export:aiHandoff` | Markdown 批量导出：同一份库（含 `con`/`Work`/`work`/空白/70 字长名/带点集合、非法字符与控制符标题、在 UTF-16 第 60 码元处截断代理对的标题、空标题、转写/摘要/收藏、CRLF 正文、标签与来源）和同一批资产（图片、视频、缺失、`..`、隐藏文件），Go 导出的 23 个文件 + 2 个资产与 TS 目录树逐字节一致（含 `../assets/` 与 `./assets/` 相对链接改写、孤立代理项落成 U+FFFD 的文件名）。`export:aiHandoff` 没有 TS 对拍（需要真实对话框），改用 Go 单测固定行为：空文本/`null` 请求报错、非字符串标题抛错、取消、文件名清洗、孤立代理项写成 U+FFFD、写入失败带原因。已知差异：默认保存目录用 `%USERPROFILE%\Downloads`，Electron 的 `app.getPath("downloads")` 读系统已知文件夹，用户改过下载位置时两者不同；写文件错误文案来自 Go 的 `*PathError`，与 Node 的 `EACCES` 措辞不同。未验证：真实目录/保存对话框未实机点过 | 84 / 103 |
| 11-11 | `knowledge:selection`（`freeze`/`results`/`source-versions`/`execute`）、`knowledge:deleteForever`、`knowledge:emptyTrash`；同时补上 `moveToTrash` 的主题阅读取消挂钩 | 14 组场景对拍（真实 TS `knowledge.ipc.ts` + `knowledge-batch.ts` + `asset-cleanup.ts`，基准库带多处资产引用）：冻结筛选（范围/搜索/排除/集合范围/标签/平台/排序，含不可检索词与 null 查询的报错文案）、25 份修订的 LIMIT 20 与排序、逐条独立事务的批量 `update/status/trash/restore/delete`、回执幂等（同 runId 同命令复用结果、换命令拒绝并保留原回执、失败回执落库）、无效输入文案（`ids` 为数字/对象/字符串/null，缺 `command`/`patch`）、彻底删除时问答证据清除（`clearEvidence` 仅严格 `true` 生效，会话 `target` 被改写）。磁盘资产逐场景比对剩余文件：正文/采集修订/网页版本/快照资产/主题阅读引用的文件在最后一条引用消失时才回收，仍被另一条目、网页版本或抓取页引用的保留，未被引用者不碰。比较范围：返回值、条目/标签/FTS/问答会话/清除记录/批量回执/冻结集合/网页版本表、磁盘资产清单。把 `bulkUpdate/setStatus/trash/restore/deleteForever` 拆成「生成语句」与「执行」两层，批量路径把改动、证据清除与回执放进同一事务（Go 已有的写频道行为不变，全部回归通过）。已知差异：SQLite 约束报错文案（来自驱动）不比；`ids` 里非字符串元素只覆盖 null/数字/空串。未验证：主题阅读取消（该族未迁，挂钩留空）、`leasedSnapshotAssets`（网页快照租约，采集族接入后连上）、真实大批量（>1000 条）耗时 | 87 / 100 |
| 11-11 | `mcp:config/getScope/setScope/install`，以及 `--mcp` stdio MCP server（`search_knowledge` / `read_item`） | 4 组对拍，golden 全部来自真实 TS 实现：①两个工具的检索与读取输出（15 个查询词、7 档 limit、平台/合集过滤、4 种范围、12 个条目的 `includeFullText` 两档）；②`buildAiHandoff` 交接稿（阅读须知、元信息、截断统计）；③范围文件解析/序列化；④一键安装进 Cursor 的 `mcp.json`：移植 `jsonc-parser` 的扫描/容错解析/`setProperty`/`format`（UTF-16 偏移、仅 space/tab 为空白、插入后整行格式化），26 个场景逐字节比对产物与 `.guizhi-backup`（含无文件、空文件、注释、尾逗号、BOM、非对象根、数组根、已存在条目覆盖等）。stdio 协议层用真实 TS server 进程（`@modelcontextprotocol/server` 2.0.0）对拍 70 余条请求：版本协商（5 个旧版本 + 未知版本回落 2025-11-25）、`initialize`/`tools/call` 的 zod 校验报错原文、`limit` 的 int/min/max、`arguments` 非对象时的协议层拒绝、未知方法/工具、通知无响应、非法行被忽略。设计变更：TS 版靠 `ELECTRON_RUN_AS_NODE` 跑 JS 包，Go 外壳没有 Node，改为本体可执行文件加 `--mcp`；`mcp:config` 因此返回「command = 本体 exe，args = [--mcp]」。已知差异：请求串行处理（TS 并发）；响应 JSON 键序不同（语义相同）；未做 schema 版本过新的拦截。未验证：与旧版 wasm 锁（`.lock` 目录 + 客户端租约）的互斥——Go 版用原生 SQLite 只读打开，旧/新版不应同时驻留；便携/自定义数据目录的四级解析未移植（`--mcp` 仅认 `GUIZHI_DATA_DIR` 或 `-userdata`，默认 `%APPDATA%\GuiZhi`）；未在真实 Cursor 里拉起验证 | 91 / 96 |
| 11-11 | `config:export/read/apply`、`shortcuts:get/set/setMode/getMode`（含启动时加载与系统级注册），以及配图风格服务（`illustration-styles`）的读写端口 | 2 组对拍，golden 来自真实 TS（`shortcuts.ts`、`config-transfer.ipc.ts`、`services/config-transfer`、`illustration-styles`，隔离 userData）：①快捷键 18 个场景（加载默认/自定义/旧根目录回退/`newPrompt`→`newItem` 迁移、`set`/`setMode` 的规范化与非对象入参、`persistImportedShortcuts`），比对返回值与 `config/shortcuts.json`、`shortcut-mode.json` 及旧根目录文件的逐字节内容（`JSON.stringify(x,null,2)` 无末尾换行）；②配置迁移 85 个场景、126 次调用：导出（含/不含机密、各 `include*` 开关组合、取消保存、`settings` 缺失或为 null、本地 FunASR 引擎保留、AI 配置损坏时仍可导出）逐字节比对导出文件，加密导出用 TS 的解密函数验证密文与 `encryption` 块（scrypt N=16384/r=8/p=1，AES-256-GCM，canary）；读取（不是 JSON/非对象/kind 错误/版本 0、1.5、2/`encryption` 各类非法与越界参数/取消/文件不存在）；应用（密码缺失与错误、选择性导入、`selection` 为 null 的 TypeError、`lastReadFilePath` 不匹配、导入前快照目录 `pre-import-*` 只保留最新 3 份、AI 配置对账、本地引擎保护）后比对 `config/` 目录全部文件与旧根目录文件。内置配图风格表由 TS 导出为 `illustration_builtin.json` 嵌入，用一条测试守住与 TS 一致。反向验证：Go 加密的配置文件用 TS 的 `decryptConfigSecrets` 能解开（`spikes/mygo-db/config-decrypt-check.mts`，4 个机密字段全部还原）。系统级快捷键接到 `mygo.GlobalShortcut`：`showApp` 切换显隐并广播 `window:visibility-changed`，所有动作推 `shortcut:triggered`；`-shot`/`-aitest`/`-bench` 不注册，以免夺走用户正在用的 Alt+Shift+P。已接受差异：`JSON.parse` 的错误文案（V8 与 Go 不同，读取与应用两处的错误只比前缀）；写文件失败的系统错误文案；U+2028/2029；`isManagedFunasrURL` 对 WHATWG 与 Go URL 解析的边缘差异。未验证：`mygo.GlobalShortcut` 的真实按键触发与 Windows 上被占用时的失败路径（离屏工作，没有抢系统快捷键）；真实对话框点击；应用后渲染进程对 `shortcuts:updated` 的反应 | 94 / 93 |
| 11-11 | `migration:*` 旧 .NET 库迁移（检测/执行，只读迁移源） | 1 组 13 场景对拍，golden 来自真实 TS（`migration.ipc.ts` + `legacy-migration.ts`，隔离 userData）：全量、缺 Wiki 表、WAL 头、空旧库、仅已删除、目标库非空、无旧库、显式路径（存在/缺失/空串回退/非旧库）、遗留 `-wal`、唯一键冲突整体回滚。比较返回值与迁移后全部表转储（时间戳按「现在」归一）、临时副本被清理。已知差异：保持 TS 行为——拒绝带非空 `-wal` 的旧库；SQLite 驱动的报错文案不比；非常规日期字符串回退到当前时间。未验证：真实 .NET 生成的大库（>10 万条）耗时 | 95 / 94 |
| 11-12 | `dialog:selectFolder`、`data:getPath/getStatus/previewDataPathChange/applyDataPathChange`、`shell:openPath`，以及数据目录四级解析（自定义 > 历史默认且有数据 > 安装目录旁 `data` > 默认） | 1 组差分 + 1 组手写：①golden 来自真实 `data-path.ts`/`shell-open-path.ts`（临时目录树）：11 种 `inspectDataPath`（含 link-safe、`data/knowledge.db` 标记顺序）、11 种 `data-path.json` 读取、写出字节、11 种初始目录解析（含 Program Files/默认每用户安装目录/正斜杠 exe/darwin）、16 种 `shell:openPath`（`~`、`%APPDATA%`、文件→在文件夹中显示、openPath 报错文案，假依赖记录调用）；②`applyDataPathChange` 逻辑在 `index.ts` 内无法直接差分，用 Go 单测固定：参数校验、同目录、migrate 不覆盖/overwrite 覆盖、switch 要求目标存在且不拷贝、子目录拒绝、采集任务/主题排版进行中拒绝、系统目录拒绝；另测 `data:getStatus`/预览。已知差异：复制时符号链接跳过；`isPathWritable` 用探针文件（比 Node 的 `access` 更严）；Go 的复制错误文案。未验证：启动时尚未用四级解析决定 userData（壳层仍靠 `-userdata`/`-db`，新增 `-appdata`，默认取 userData 上一级以免测试写到真实 `%APPDATA%`）；真实文件夹选择对话框未点；`stopMobileCapture/stopReadingSync` 对应的冻结服务在 Go 里是空操作 | 96 / 91 |
| 11-12 | 应用外壳：窗口状态持久化、启动可见性、关闭行为（`window:showCloseDialog/closeDialogResult/closeDialogCancel`、`app:setCloseAction/setMinimizeToTray/setDebugMode/setAutoLaunch`）、托盘与 `app:command`、`window:toggleVisibility`、可见性/全屏推送、单实例锁与第二实例聚焦、Windows 无边框窗口 | 2 层：①差分——golden 来自真实 `window-state.ts`/`tray-menu.ts`：25 种持久化载荷 × 6 种屏幕布局（含负坐标屏、无屏、小屏）的启动边界、10 种启动可见性（`--hidden`、登录项、最大化推迟）、5 种状态文件读取（缺失/坏 JSON/BOM/空）、5 种捕获与写出字节、13 种区域→托盘语言与文案、托盘菜单结构与动作；②手写——这些逻辑在 `index.ts` 内部无法差分，按其分支固定：ask/minimize/exit 三种关闭策略（含「对话框未决时再次关闭放行」这一原样行为）、对话框结果的真值与非法载荷、取消后可再次弹出、托盘创建/销毁时机、命令派发（最小化先还原、加载中延后发送、无窗口返回 false）、设置读取（`minimizeOnLaunch`、`language`）。真实窗口里验证过：离屏 `-shot` 跑通，页面渲染正常，`.titlebar-drag` 的 `-webkit-app-region` 经 shim 镜像成 `--app-region: drag`。已知差异：mygo 不能给登录项启动命令加自定义参数，TS 的 `--hidden` 改成「系统登录项拉起 + 启动时最小化设置」在启动时判定（`--hidden` 命令行仍认）；Electron 版在 Windows 写的旧 Run 项名字与 mygo 的不同，迁移后可能留下一条旧自启项（单实例锁会兜住重复启动）；mygo 没有「还原后的普通边界」接口，改为在窗口非最大化/最小化/全屏时记录；窗口销毁后不重建（Windows 版关最后一个窗口即退出，TS 同）。未验证：真实托盘图标的左键切换/右键菜单（离屏不建托盘）、真实关闭按钮点击与对话框、真实登录项写注册表、第二实例聚焦、无边框窗口的拖拽与缩放手感（只验证了 CSS 镜像）、DPI 不同的多屏恢复；DevTools 快捷键门控（`isDebugMode`）生产环境未移植 | 97 / 90 |
| 11-12 | `updater:check/download/install/installSource/openDownloadedUpdate/openReleases/platform`（应用内更新，替代 electron-updater 6.8.9 的 NSIS 路径） | 2 层：①差分——golden 来自真实 `updater.ts`/`updater-channel.ts`/`version.ts`/`changelog.ts`：版本规整/解析/比较 1000+ 组（含预发布标识、非法串回退）、预览通道选择（正式版与预发布同版本偏好正式版）、更新说明区间提取与最新段落提取（CRLF/孤立 CR/Unicode 行分隔符）；②手写+本地 httptest 模拟 GitHub/镜像（不访问真实网络）：官方源 atom + `/releases/latest` + `latest.yml`、预览通道 atom 选 tag + API 查询、镜像逐个回退（失败镜像各推一条 error 状态）、下载进度单调、sha512(base64) 校验失败清理残留并给出归一文案、缓存命中不重复下载、先备份（`pre-update` 钩子）再以 `--updated --force-run` 脱离启动安装包并退出、重复安装拒绝、开发模式三个频道的禁用文案、`normalizeUpdateOptions`。有意差异：无安装包路径时 `updater:install` 如实返回失败（electron-updater 只推 error 事件，TS handler 仍报 success）；启动安装包失败时回退为 `Shell.OpenPath`（系统按需弹 UAC），不再依赖 elevate.exe。已知差异：非法版本串的自然序回退是近似实现（非 ICU）；多镜像下载时只推一次 error。未验证：真实 GitHub/镜像更新源与真实安装包；真实 NSIS 安装与「安装后自动拉起」；网络代理设置尚未作用于更新器的 HTTP 客户端（待「网络代理」切片）；`backup` 钩子已接到本地备份（见备份行）；打包版需用 `-ldflags -X main.packagedBuild=true` 注入 `isPackaged`，CHANGELOG 随包位置（`resources/CHANGELOG.md`）待打包切片确定；无 Authenticode 校验（与现有 electron-builder 配置一致，未配置发布者签名） | 97 / 90（字面量频道 7 个 `updater:*` 全部登记） |
| 11-12 | 备份与恢复：`backup:create/list/delete/restore`（本地单文件 .db 备份）与 `backupRepository:status/init/changePassword/createSnapshot/listSnapshots/preview/deleteSnapshot/exportPortable/restore/consumeRendererSettings`（加密仓库），以及恢复编排、安装前 `pre-update` 备份钩子（已接到更新器）、重启交接 | 3 层：①TS 差分——`golden-backup.mts` 用真实 `backup.ts`：列表（合法/非法/同秒序号/越界日期进位/四类前缀）、删除（路径穿越与缺失）、按类清理（keep 0/1/2/3/10 × 4 类）、`validateBackupFile` 22 个 DB 夹具（缺核心表、`user_version` 40/999、`web_source_versions` 快照各类非法、空/乱码/截断文件）、`performRestoreSwap` 4 个场景（旁车文件、`.clean`、失败回滚、pre-restore 清理）；②仓库互操作——`mygo-restore/run-diff.ps1` 对着真实 TS 仓库跑 9 组：TS 建仓库 Go 读、口令错误、Go 恢复、Go 建仓库 TS 读、混合仓库去重、预检矩阵、改口令/删快照 GC、便携导出、注入失败后的回滚，0 项失败；Go 建的 `Local State` 与加密后的 `safeStorageWrappedKey` 已由真实 Electron `safeStorage.decryptString` 解出同一密钥；③手写——恢复编排（守卫、换库、pre-restore、重启回调）、仓库初始化/状态、后台备份任务按开关与间隔调度且保留既有 `nextRunAt`、`consumeRendererSettings`、渲染端设置键序。设计取舍：MyGo 内置 SQLite 拒绝 `VACUUM INTO`，备份用 `modernc.org/sqlite` 只读连接；Go 新建仓库的 `keyStorageBackend` 写 `dpapi`（Electron 在 Windows 写 `unavailable`），`status()` 额外尝试解开包装密钥；重启用环境变量 `GUIZHI_RELAUNCH_PARENT=<pid>` 让子进程等父进程退出后再取单实例锁（修复新旧进程抢锁）；`lib.jobs.Start()` 此前从未被调用，现在非自动化模式下启动。已知差异：主题排版备份的体检只做 `themed_reading_assets` 索引一致性与资源文件校验，TS 的 HTML 清洗/槽位/任务归属检查随主题排版切片补；`sanitizeSnapshot` 仅移植结构校验；legacy 自动备份调度器在 TS 里从未启动，不移植（`backup:autoStatus` 仅保留频道常量）；驱动/V8 错误原文（乱码库、截断库、JSON 解析）不逐字比较。该缺口已由下一行「数据库初始化与迁移」补上（`supportedSchemaVersion` 现取自 Go 迁移清单长度）。未验证：真实 UI 流程；真实重启后的整链路；与开发实例并存时的 Local State；大库耗时；新旧版本互相读取 Local State 的长期兼容 | 常量频道待迁 75 / 字面量 33 全部登记 |
| 11-12 | 数据库初始化与结构迁移（对应 `init.ts` + `migrations.ts` + `database-client-lock.ts` + `database/index.ts` 的启动补齐），新数据目录从零自建库、旧库逐条升级 | 2 层：①整库转储差分——`golden-schema.mts` 让真实 TS `initDatabase` 在 4 个起点上跑完并转储（结构 SQL 逐字 + 全部表数据 + `user_version`）：空目录、TS 已建好带数据的当前库、**历史形态库**（把 TS 建好的库改回三态 `knowledge_items`/无 `forum`、无 `linuxdo`/`web` 约束、缺 `time_scope`/`access_uri`/会话文章列等，清空迁移记录，让 39 条迁移全部重跑；含状态折叠、平台重算、访问链接回填、问答历史索引重建、触发器补建）、仅剩 0022 起待执行且含重复发现候选的库。Go 同起点跑完后 181 条结构定义与每张表的数据逐项相等（`sqlite_stat1/stat4` 行序随构建不同，按内容排序后比较，数值一致）；②手写——迁移清单与 TS 同名同序（`gen-schema.mts` 把 16 个建表/索引 SQL 常量原样导出为 `schema_gen.go`，迁移里的内联 SQL 逐字移植并由整库差分兜底）、中途崩溃后只补未登记的迁移、存在待执行迁移才留 `pre-update` 备份（失败则中止升级）、干净关闭标记（新鲜跳过 quick_check/过期一周复检/乱码库报错）与 freelist/索引自动修复的诊断判定、客户端租约（死进程残留清理、拒绝与存活进程并存、重复释放无副作用，Windows 用 `OpenProcess`+`GetExitCodeProcess` 判活）、启动补齐缺失的条目/Wiki 全文索引行（含回收站条目与映射表）。真实二进制：在全新数据目录启动 `-shot`，自建库到 v39、界面正常渲染、正常退出后留下 `.clean` 标记且租约目录已清除。设计取舍：迁移用 modernc 单连接在 MyGo 插件打开之前完成（插件事务是静态语句批，读不了；连接池下 `PRAGMA foreign_keys` 无法保证落在同一条连接上）。**有意差异**：租约更严——TS 只用租约决定是否回收 wasm 遗留的 `<db>.lock`，Go 发现存活的其他客户端就拒绝启动（新旧驱动的锁互不兼容，同时写同一个库有损坏风险）；不回收 `<db>.lock` 目录（原生 SQLite 不用它，回到旧版时由旧版自己回收）。已知差异（第二轮已更新）：`ensureDatabaseIntegrity` 的 freelist 修复分支已用真实损坏文件验证（自动修复、22 条完整、留完整性备份），索引损坏的 REINDEX 仍只有诊断判定单测；拒绝启动现在写 `logs/error.log` 并在交互启动时弹系统对话框。未验证：大库（数万条）迁移耗时；与用户真实 Electron 实例并存时的租约交互；`PRAGMA user_version` 在迁移中途失败时的状态 | 常量频道待迁 75 / 字面量 33 全部登记 |
| 11-12 | 出站网络层（对应 `net-safety.ts`、`shared/utils/network-proxy.ts`、`network-proxy.ts` 的主进程部分、`import/safe-fetch.ts`）：SSRF 地址判定、网络代理设置的归一化与生效、采集链路的安全出站 HTTP（`fetchHtml`/`fetchRawText`/`fetchJson`/`downloadToTempFile`/`resolveRedirectUrl`） | 2 层：①差分——`golden-net.mts` 用真实 TS 模块对 ~100 个地址（IPv4 边界、映射/翻译/兼容写法、ULA/link-local/6to4/Teredo/NAT64、zone）逐个判 `isPrivateAddress`/`isForbiddenAIEndpointAddress`、对 12 个主机 × 4 组选项判 `resolvePublicAddress` 字面量路径（含错误文本）、对 47 组设置输入判 `normalizeNetworkProxySettings`+`buildProxyUrl`（含 host 字段内嵌 URL、端口字符串 `parseInt` 语义、控制字符、长度截断、IPv6 方括号、百分号编码口令）、对 11 组绕过规则 × 18 主机判 `shouldBypassProxyForHostname`，Go 全部一致；②手写（`httptest`，不访问真实网络）——解析结果钉扎到校验过的 IP 且 Host/SNI 仍用原域名、任一解析结果为私网即拒（防混合应答）、每跳重定向重新校验（重定向到内网被拦、5 跳上限、跨主机跟随）、fake-ip（198.18/15）仅在存在代理配置（非 direct）时放行并给出 TUN 提示、手动代理（绝对 URI + `Proxy-Authorization`、不做本地 DNS、字面内网 IP 仍拦、绕过规则命中直连）、非 2xx / 非网页 / 超大小上限的错误文本、`acceptStatuses`（NGA 403 握手 + Set-Cookie 拆分）、GBK（响应头 / meta / NGA `encode` 字段）与未知标签回退 UTF-8、取消（`已取消`）与 30 秒空闲超时（`请求超时`）、下载上限与网页误当媒体的拒绝及失败清理、代理环境变量写入与还原（子进程 yt-dlp/ffmpeg 继承）。已接入：设置写入 `networkProxy` 与启动时按库值生效；AI 请求代理与更新器的 HTTP 客户端改走 `activeNetProxy`。**有意差异**：点分写法的映射 fake-ip 字面量（`::ffff:198.18.0.1`）TS 因按十六进制误解析而拒绝，Go 按真实字节放行（仅已配置代理时）；system 模式下主进程请求直连（与 Node 一致，Electron 会话代理对渲染进程才有意义，渲染进程只加载本地页面，不适用）；不发 Accept-Encoding；非法字节的替换字符位置可能与 TextDecoder 不同。未接入：采集进程的 `--proxy-server`（随采集进程切片）、`web-verification-proxy`；未验证：真实 SOCKS5 代理（Go 的 socks5 由代理侧解析域名，对应 TS 的 socks5h，只有 URL 构造被对拍）、真实 TUN/fake-ip 环境 | 常量频道待迁 75 / 字面量 33 全部登记 |
| 11-12 | 网页采集（静态路由）与导入接入（对应 `web-capture.ts`、`web-electron-capture.ts` 的 `staticPage`/`capture` 编排、`web-network.ts`、`web-request-identity.ts`、`web-static-route.ts`、`web-extract-article.ts` 的结果组装、`web-error.ts`、`shared/utils/web-scope.ts`、`import-service.ts` 的 `captureWebpage` 回调与 `WebSourceDB.initialize`）：规范化入口、逐跳抓取、严格解码、静态/渲染判定、提取、错误归类、并发与超时、条目入库时的原文版本与基线 | 2 层：①差分——`golden-web.mts` 用真实 TS 模块对 37 个合成页面（发布时间各种写法：Z/偏移/毫秒/无时区/仅日期/溢出日/24:00/空白、作者多值、HTTP 401/403/404/500、验证页标题、登录表单、200,000 码点截断、`<base href>`、片段无外壳、SPA 外壳、隐藏主体/祖先、meta refresh、`aria-busy`、加载占位、自定义元素、`pre` 空锚、微信结构、H1 回退标题、超长标题）比较整个提取结果（来源、标题、作者、`publishedAt`、`dateConfidence`、Markdown、段落、`contentHash`、`complete`、`truncated`、`warnings`、`error`），比较 `assessStaticPage` 全字段（路由、原因文本、`textChars`、语义区域数、脚本）、`isWebVerificationPage`、`staticPageLinks`（相对/协议相对/非 http 过滤/上限）、27 条错误文本的 `webCaptureError` 归类（含显式 `webCaptureCode`、网址脱敏与查询参数剔除、1000 码元截断）、14 个入口的 `canonicalWebUrl`（含 IDN、百分号、默认端口、账号口令）与 20 个目录范围用例（`webScope` 含编码分隔符/`..`/`%2e`/畸形转义的错误文本、`inWebScope` 20 个网址、`isWebPageLink` 14 个后缀），Go 全部一致；②手写（`httptest`，不访问真实网络）——静态文章端到端（gzip、302 + 片段、元数据、链接、阶段回调）、请求身份（`GuiZhi/` UA、无 `Accept-Language`、`Accept-Encoding: gzip, deflate`、文档 `Accept`）、GBK 页、非法 UTF-8 转渲染、SPA 转渲染（渲染器未接入时如实失败；注入替身渲染器时走 `readability/1-electron`、渲染失败如实带出）、403/PDF/重定向环/目录范围越界/验证页/内网目标/非法网址/交互式验证的错误归类、超时与取消带归类、同源串行与名额不泄漏、16 MiB 输入上限；**采集 → 提取 → 入库 → FTS 检索 → `web_source_versions`/`web_source_baselines`** 的端到端，以及抓取失败时导入侧得到降级原因（不入库空壳）。已接入：`url` 来源按 视频 → 论坛 → 公众号 → 通用网页 分发，网页走本采集；条目创建事务内写入首个远程版本与基线（`JSON.stringify` 逐字节一致的载荷）。**有意差异**：请求发 `gzip, deflate` 不发 `br`（Go 无 brotli，服务端按 Accept-Encoding 协商）；提取在进程内协程完成（无 worker 线程），45 秒超时/16 MiB 上限/panic 捕获保留，超时后协程跑完即弃（CPU 密集无取消点）；严格解码对非 UTF-8 编码以输出出现 U+FFFD 近似 `TextDecoder` 的 fatal；`gbk`/`gb2312` 标签统一按 GB18030 解码（WHATWG 规范，此前误用 GBK，NGA 等场景同步受益）；UA 为 `GuiZhi/<版本> Chrome/156.0.0.0` 的自报家门字符串（渲染窗口接入后改取其真实 UA）。**未接入**：渲染路由（隐藏窗口/采集进程池，`--proxy-server`）、FlareSolverr 与交互式验证浏览器、微信公众号/快照、视频与论坛连接器（`url` 分发在这三类上如实报"尚未迁移"）、`WebSourceDB` 的 check/adopt/attach（随 `web:*` 频道切片）；未验证：真实网站（需另行确认）、真实渲染场景下的静态→渲染回退 | 常量频道待迁 75 / 字面量 33 全部登记 |
| 11-12 | 文档批次与网页原文版本（对应 `CrawlJobDB`、`validateCrawlInput`、`robots.ts`、`crawl-service.ts`、`WebSourceDB.check/adopt`、`web-capture.ipc.ts` 的 `crawl:*` 与 `web:adopt`）：批次校验与持久化、robots.txt 规则、sitemap 候选、逐页采集入库、发现新链接、重复策略、暂停/取消/恢复/重试、重启恢复、重采后更新或保留待采用版本、采用某个原文版本 | 2 层：①差分——`golden-crawl.mts` 用真实 TS 模块比较 52 个 `validateCrawlInput` 用例（含 V8 TypeError 文本）、44 步 `CrawlJobDB` 操作序列（每步返回值/错误 + 最终两表转储，uuid 按首次出现编号、当前时间掩码）、18 份 robots 文本 × 32 个网址的规则与允许结果、12 份 sitemap 的 `url > loc` 解析；`golden-websource.mts` 从同一份起始库（6 个条目：可自动更新/带摘要/用户改过正文/已删除/无基线/首版不完整，含向量与 Wiki 摄入记录）重放 26 步 `check`/`adopt`，每步比较返回值/错误与全部条目的正文、基线、版本载荷、向量与摄入记录条数，Go 全部一致；②手写（`httptest` 本地站点，不访问真实网络）——目录批次端到端（入口 + sitemap 候选 + 链接、深度/页数上限、范围外与非网页后缀与 robots 禁止项的过滤、`skipped` 原因、条目/来源记录/基线/导入任务/FTS 落库）、`skip`/`update` 两种重复策略（`duplicate`/`updated`/`unchanged`、local 快照版本）、robots 503 时暂停来源并在恢复后完成、暂停（页面回 `pending` 并带中断原因）/取消/已取消不可恢复/重试、`crawl:*` 与 `web:adopt` 的 `{ok,data}`/`{ok:false,error}` 形状与参数校验、启动恢复、`-race` 通过。**有意差异**：TS 在同步事务里完成 persist，Go 改为「读→生成语句→单次事务」，新建条目路径里条目、来源记录、原文版本、导入任务、页面状态与新发现的链接仍同一事务；已有条目 + `update` 策略时 `check` 是自己的事务，之后的任务/页面状态是第二个事务（两者之间崩溃只会让页面回 `pending`，重采得到 `unchanged`，幂等）；导入任务直接写入终态行而不是先 create 再 update（列值与 TS 最终行一致）；页面里保存的采集结果由 Go 结构体序列化（无 `snapshot`，`undefined` 字段输出为 `null`）；robots/sitemap 请求带 `Accept: */*`（TS 不发）。**未接入**：`attach`（补采只加版本，随微信快照切片）、`web:versions`（需移植 `sanitizeSnapshot`）、快照资源；未验证：真实网站、渲染回退下的批次、与 Electron 实例并行 | 常量频道待迁 67 / 字面量 33 全部登记 |
| 11-12 | yt-dlp / ffmpeg 工具管理（对应 `ytdlp-manager.ts`、`ytdlp-update.ts`、`ffmpeg-manager.ts`、`ffmpeg-update.ts`、`media.ipc.ts` 的 `ytdlp:*` / `ffmpeg:*`）：`status/checkUpdate/install/remove/pickBinary` 与 `downloadProgress` 推送，共 10 个频道；解析顺序 自定义路径 → 托管副本 → PATH，托管版下载镜像回退、SHA-256 校验（yt-dlp）、zip 解包与可运行性探测（ffmpeg）、单任务互斥、状态 TTL 缓存与并发合并、版本探测缓存 | 无 TS 差分（行为全是进程/文件/HTTP，不落库）。Go 行为测试 7 组：解析函数（`parseSha256Sums`/`parseReleaseTag`/构建日期/`numericLocaleCompare` 对 14 对 Node 实测基准）、状态缓存并发合并与 TTL、版本缓存、解析顺序（含坏路径不回退、托管损坏回落 PATH）、yt-dlp 多镜像回退+篡改拒绝+无校验和降级+并发互斥、ffmpeg zip（无 bin/不可运行/反斜杠条目/非 Windows 拒绝）、IPC（缓存、更新检查走注入传输层、选择器登记后 `settings:set` 才放行路径）。测试替身是现编译的 Go 小程序，不使用任何真实网络 | 未验证：真实 GitHub / 镜像下载与 SOCKS5/TUN 下的下载；真实 ffmpeg 构建的探测；真实文件选择对话框。接受的差异：ffmpeg 无哈希校验（与 TS 一致）；`numericLocaleCompare` 为 Intl 数字排序的近似实现 |
| 11-12 | 本地转写引擎 FunASR（对应 `funasr-manager.ts`、`funasr-service.ts`、`funasr-update*.ts`、`funasr-server-script.ts`、`media.ipc.ts` 的 `funasr:*`）：`status/checkUpdate/update/install/uninstall` 与 `installProgress` 推送，共 6 个频道；内置 Python 安装包下载（官方 + 3 个镜像、校验和）、venv + pip 依赖、服务脚本落盘、健康探测与端口占用接管、启动/停止（taskkill /T）、单飞启动与 FIFO 独占转写队列、`ai-models.json` 内置转写模型增删、更新事务（空间预检、备份 manifest schema 2、目录身份校验、pip 修复、回滚、需人工恢复）| 2 层：①差分——`golden-funasr.mts` 用真实 TS 模块导出内嵌服务脚本（9176 字节逐字节一致）并比较路径布局、版本比较表、进程日志摘要、代理失败解释、目录身份（device/inode/birthtime 与 libuv 一致）、环境度量与字节格式、备份 manifest、备份清理告警与残留、`ai-models.json` upsert/remove 用例，Go 全部一致（含一次性 disposable 根目录用例：每次重新生成 golden 只能通过一次）；②手写——`fakefunasr` 一个二进制扮演 python.exe/venv python/服务端（环境变量驱动失败模式），覆盖工具进程运行器、日志汇聚、FIFO 独占、tar.gz 解压安全（路径穿越/符号链接）、服务生命周期（启动、健康、接管、停止）、启动失败（崩溃/超时）、状态、安装/卸载及各类安装失败、检查更新、更新成功/回滚/回滚受阻/预检/pip 修复、备份生命周期、纯净目录与空间、IPC 形状与进度推送、状态缓存失效。全部使用本地 httptest + 假可执行文件，不访问真实网络。**有意差异**：tar.gz 为 Go 原生解压（TS 调系统 tar）；不提供 GGUF/macOS 的安装与启动（仅识别）；本机回环探测绕过代理；`installSupported` 仅 win32；PyPI 重定向报 HTTP 状态错误而非 TypeError；进程启动错误文本不同；PIP_PACKAGES 与 TS 一致为 funasr、fastapi、uvicorn、python-multipart、torch、torchaudio | 未验证：真实 Python 安装包/pip/torch/模型下载、真实 GitHub 与 PyPI、SOCKS5 下的安装、真实 GPU/CPU 转写 | 常量频道待迁 49 / 字面量 33 全部登记 |
| 11-12 | 转写 / 排版 / 总结（对应 `media.ipc.ts` 的 `media:transcribe/transcribeProgress/formatTranscript/formatProgress/summarize/refreshForumDiscussion/testTranscription/capabilities`、`transcript-formatter.ts`、`media-summary.ts`、`media-note.ts`、`ai-protocol.ts`、主进程 `chatCompletion` 与 `recordMainAiUsage`）：新增 Go 对话客户端（有序请求体、qwen `enable_thinking` 失败重试、流式读取、usage 汇总、按网络代理选路）、协议/端点/请求头解析、主进程 AI 用量上报（`withAICallSink` 上下文，导入队列已接入）、转写云端接口 multipart 流式上传（ffmpeg 16k 单声道 mp3 预转码，失败回退直传）、文字稿 AI 分块排版（块 1600 字、并发 3、总时间预算、部分排版/跳过原因）、内容总结与 AI 标题（元数据引用块记原标题、转写来源注记）、1s 进度推送（阶段 transcribing/formatting/summarizing + 本地引擎心跳停滞时长）、说话人分离能力检测与拒绝 | 2 层：①差分——`golden-media.mts`（快速运行器 `run-golden-media.mts` 约 5s）用真实 TS 模块在脚本化 HTTP 服务上录制请求/响应，Go 对 7 组全部一致：协议与端点解析、对话（含 qwen 重试、流式、错误文案）、排版纯函数与整条排版流程（成功/部分/跳过/预算耗尽）、总结与 AI 标题、正文注记 upsert、转写请求与错误；②手写——`media_ipc_test.go`：各频道守卫与 `notConfigured`、说话人分离拒绝、媒体文件缺失、转写→排版→总结→落库（标题、来源注记、FTS 映射、阶段推送）、手动排版与总结落库、`testTranscription`、论坛频道如实报未迁移。全部使用本地 httptest，不访问真实网络、不使用真实密钥。**有意差异**：不支持 brotli；对话客户端超时给出原因文案而非 Node 的 “This operation was aborted”；JSON 语法错误文本与 Node 不同；`jsNumber` 近似 `Number()`（数组/对象按 NaN）；说话人前缀正则 `(?m)^` 只识别 `\n`；块硬切落在代理对中间时 Go 得到 U+FFFD（JS 保留孤立代理项）；转写请求不发 `Accept` | 未验证：真实云端 ASR/LLM、真实 ffmpeg 转码长音频、真实 yt-dlp 音轨下载。**暂以桩实现，待连接器切片替换（`media_pending.go`）**：在线视频重新转写（需视频连接器 `video-url.ts`）、论坛讨论总结与刷新（需 appinn/linuxdo/twolibra 连接器）——三者目前如实返回“连接器尚未迁移” | 常量频道待迁 41 / 字面量 33 全部登记 |
| 11-12 | Wiki 编译器（对应 `wiki-compiler.ipc.ts`、`wiki-compiler-blocks.ts` 与 `wiki-compiler` 服务：`wiki:compiler` 的 preview/start/next/finish/jobs/control/suggestion 动作、按块切分与指纹、块级缓存复用、贡献校验、页面发布与人工页/旧版页的更新建议、任务暂停/失败重试/启动时中断恢复） | 2 层：①差分——`golden-wikicompiler.mts`（快速运行器 `run-golden-wikicompiler.mts`，需从 `D:\Project\GuiZhi` 目录运行）用真实 TS 模块在带种子的库（A..N 共 14 种资料状态、普通/人工/旧版页、建议、失效页）上回放 23 组场景共 ~190 步，每步比对响应与整库转储（uuid 与时间已规范化）：预览与范围、启动校验、完整编译流、缓存复用、失败与重试、非法贡献、非法回执、暂停/继续/取消、源在途变化、中断恢复、建议查看与接受、未知动作；另有 32 个切块用例（标题/围栏/CRLF/emoji/超长行）逐块对拍。Go 全部一致；②`wiki_compiler_golden_test.go`。**有意差异**：无 5 秒预览缓存；`publishCompleted` 拆成两个事务（崩溃后靠 published=0 重放保持一致）；接受前的 `flushWikiInvalidations` 单独提交；块硬切落在代理对中间得到 U+FFFD。未验证：渲染进程驱动的真实模型循环、真实大库耗时 | 常量频道待迁 40 / 字面量 33 全部登记 |
| 11-12 | 原文快照（对应 `web-capture.ipc.ts` 的 `web:snapshot` / `web:snapshot-export` / `web:snapshot-enqueue` / `web:solverCheck` / `web:versions`、`snapshot-service.ts`、`snapshot-sanitize.ts`、`snapshot-document.ts`、`flaresolverr-connection.ts` 与 `flaresolverr.ts` 的连接与控制接口）：读取公众号原文快照（再净化、图片哈希校验、实例标识与桥接脚本文档）、按版本导出 HTML 目录（暂存目录原子改名、Windows 占用重试）、批量补采入队、版本列表与待采用状态、FlareSolverr 连通性检查（本地直连或经已有 SSH 配置的回环隧道，控制命令不走全局代理） | **净化引擎决策**：不手写移植 `sanitize-html` + `css-tree`（净化器是安全边界，上游的 mXSS 修补与 css-tree 的 Raw 回退等行为很细，手写移植会滞后且无法保证逐字节一致），而是用 esbuild 把 TS 原文件连同依赖打成单文件（`spikes/mygo-js`，产物 `websnap_sanitize.js` 273 KB，`go:embed`），在纯 Go 的 JS 解释器 goja 里运行；升级依赖后重新打包即可。2 层：①差分——`golden-websnap.mts`（快速运行器 `run-golden-websnap.mts`）用真实 TS 模块录制 116 组 HTML、197 组行内样式、101 组样式表、25 组快照校验、12 份文档（有/无实例标识、含特殊字符的实例标识）、转义与快照哈希，Go 全部逐字节一致，桥接脚本全文一致；②手写——`web_snapshot_test.go`：ID 校验、无版本/有基线/改过正文（edited）/更新快照（pending）/指定版本、图片被篡改/缺失/快照格式无效、版本列表（无效快照被移除、有效快照被净化）、导出（文档头、资源改写、不含脚本、取消、无快照）、补采入队（去重、回退 source_uri、非公众号、已删除、1–50 上限）、FlareSolverr 本地检查（连通/验证超时/上游错误不泄露/无效响应/无服务/设置无效）。**有意差异**：图片缺失时错误文案为“原文图片缺失（文件名），请重新补采”（Node 会带出绝对路径的 ENOENT 原文）；桥接层 JSON 参数里 `undefined` 与 `null` 无法区分，`web:snapshot` 的版本参数为 null 视同未指定（TS 对显式 null 会报“记录 ID 无效”）；净化耗时随体量线性增长（约 1 MB HTML 5 秒，另有 60 秒上限，超时返回错误而不是卡住）；FlareSolverr 后备采集（`captureWithFlareSolverr`）随渲染/验证切片接入，目前只有连接、命令与连通性检查。未验证：真实 SSH 隧道、真实 FlareSolverr 服务、真实公众号快照（需公众号连接器切片产生） | 常量频道待迁 35 / 字面量 33 全部登记 |
| 11-12 | 公众号连接器（对应 `web-capture/wechat.ts` 与 `snapshot-assets.ts`，并补齐 `WebSourceDB` 的快照部分）：`captureWechat`（抓取 → 解析 → 隐藏容器还原 → 懒加载图片 → 媒体占位 → 资源收集 → 净化 → 快照与 Markdown）、图片落盘（`wechat-<sha256>.<ext>`、同卷硬链接发布、落盘即租约、4 并发/200 个/20 MiB/100 MiB 上限）、版本 payload 带 `snapshot` 与 `web_snapshot_assets` 登记、`attach`（补采只追加版本）、`check` 的快照哈希比较与「带快照一律待采用」、导入队列的公众号补采分支与来源一致性校验、`disposeExtracted`（释放租约并清孤儿资产）；导入分发里的公众号路由与 `captureWebPage` 的公众号路由已接入（视频/论坛仍报「尚未迁移」） | 2 层：①TS 差分——`golden-wechat.mts` 把真实 `wechat.ts` 经 esbuild 打包（出站抓取与图片目录换成桩，其余依赖照常解析）跑在 19 个夹具页上：完整文章（隐藏容器、懒加载、封面、锚点、`<style>` 与 `@import`、`url()`、iframe/video/mpvoice 占位、表格/代码/列表）、标题三级回退、发布时间（无时区/Z/非法）、无正文/空白正文/仅图片、动态 SVG、不支持的协议资源、非图片内容、跳转到其他平台、同内容多来源、容器样式三种、脏 HTML（未闭合、大小写、表格有无 tbody、实体、`@font-face`/`@media` 内 url、协议相对地址、script 内伪造 `#js_content`）；Go 逐项对拍净化 HTML/CSS、哈希、资产与失败清单（按键排序，不依赖并发完成序）、元数据、警告、错误文案与落盘文件名，全部一致（Markdown 只校验来源尾部）；②手写——导入保存（版本/资产登记/基线）、补采同内容不新增版本、补采变化内容追加版本且不覆盖正文、来源不一致拒绝、未保存即丢弃时孤儿资产被清理、带快照的 `check` 只留待采用版本。设计取舍：净化仍走 goja 里的 TS 原文件，新增 `cleanHtmlFn/cleanCssFn` 入口让 Go 回调参与资源映射（按 JS 完全相同的调用顺序收集 URL，保证资产与失败清单顺序一致）；`x/net/html` 会按规范补出 `<tbody>`，先给源码里显式的 `<tbody>` 打标记，解析后只去掉补出来的，与 linkedom 一致；`display:none !important` 与 linkedom 一样不被当作 `none`。已知差异：HTML 解析器为 `x/net/html`（TS 为 linkedom），格式化标签错位嵌套（`<b>…</span>…</b>`）时规范会重建 `<b>`、linkedom 不重建，净化结果因此可能不同；Markdown 为 html-to-markdown（TS 为 Turndown，列表缩进等字节差异属已接受）；SVG 图片不栅格化（TS 经 resvg 转 PNG），记入失败清单、文章标记为不完整、可补采；`published_time` 只识别带秒的 ISO 形态（`legacyDateParse`）。未验证：真实公众号页面与真实图片 CDN；与 Electron 版并存时的资产目录写入 | 常量频道待迁 35 / 字面量 33 全部登记 |
| 11-12 | 论坛连接器（对应 `import/{v2ex,discourse,appinn,linuxdo,twolibra,forum-post,forum-summary}.ts`、`shared/utils/forum-note.ts` 以及 `media.ipc.ts` 的 `regenerateForumSummary` / `refreshForumDiscussion`）：V2EX、小众软件论坛、LINUX DO（Discourse 标准接口、补拉缺失楼层、分类缓存、回复对象还原）、2Libra（平铺评论分页、信封解包、安全上限）的抓取与错误翻译，条目组装（元数据引用块、讨论总结、正文、逐楼回复），讨论总结（单发 / 分块 map-reduce、拟题协议、确定性清洗、弱标题判定），小节读写（拆分、解析、过滤、总结写回、讨论替换、过期提示）；导入分发里的论坛路由、重新生成总结与刷新讨论两个 IPC 已接入，替换了 `media_pending.go` 里的论坛桩 | 2 层：①TS 差分——`golden-forum.mts` 把真实 `forum-note` / `forum-summary` / `v2ex` / `discourse` / `twolibra` / `forum-post` 经 esbuild 打包（出站抓取与用量记账换成桩，抓取与模型调用由夹具脚本注入）录制 115 组：总结清洗 7、弱标题判定 14、分块 5、总结生成 10（单发、弱标题拟题、无协议行、空回复、空输出、仅分隔线、模型出错、分块归并、分块拟题、超过 8 块截断，逐次比较提示词与请求参数）、抓取 53（V2EX 完整 / 缺字段 / 空帖 / 非数组 / 回复失败 / 8 种错误文案 / 重试成功与耗尽；Discourse 补拉楼层、分类三级回退、cooked HTML、errors 字段、无 stream、补拉失败保留已得、10 种错误含登录会话降级成功与失败；2Libra 完整平铺评论含重复 / 删除 / 空内容 / 匿名 / 回复对象各形态、分页中途失败、超安全上限、信封错误、缺标题或节点、8 种错误含 429 重试，并比较每次请求的 URL 顺序）、小节读写 5 份正文×8 种操作、摘要 5、回复块 4、条目组装 14（有无模型、拟新标题、同名标题、总结为空、总结失败截断 120 码元、无回复、仅楼主、告警、抓取失败、取消、NodeSeek 未就绪，含阶段回调）；Go 全部一致。②手写——URL 导入分发（V2EX 端到端、NGA / NodeSeek 如实降级、帖子不存在）、小众软件论坛条目的刷新讨论（替换楼层、回复数更新）与重新生成总结（写回总结与落库、旧总结过期提示、抓取失败 / 不支持平台 / 未配置模型 / 总结为空 / 无讨论内容各错误文案）、回复数正则 5 种边界。已知差异：html-to-markdown 与 Turndown 的列表缩进（`-   ` 与 `- `）、围栏语言标注属已接受的字节差异（`ds-cooked-rich` 单独用 Go 期望串校验，其余字段仍严格对拍）；相邻列表不再写 `<!--THE END-->`（`WithListEndComment(false)`，公众号 Markdown 同步生效）；`Date.parse` 在 Go 里为严格 ISO（`legacyDateParse`）；传给总结的回复只含楼层 / 作者 / 正文。**未接入**：NodeSeek（需要浏览器宿主）、LINUX DO 的 Cloudflare 登录会话降级（`fetchAuthenticatedJSON` 注入点已留，渲染宿主接入后补）。未验证：真实站点（V2EX / LINUX DO / 小众软件论坛 / 2Libra 真实接口与反爬）、真实模型总结质量 | 常量频道待迁 35 / 字面量 33 全部登记 |
| 11-12 | NGA 论坛连接器（对应 `import/nga.ts` 与 `nga-entities.ts`）：lite=js 分页读取与 guestJs 握手（403 带挑战正文 → 合并 Cookie → 重读，分页中途会话失效同样处理）、非严格 JSON 的控制字符转义与解析、错误分类（访客无权、帖子过旧、被拒绝、结构缺失、网络，含瞬时错误退避重试）、总结采样（最多 12 页）、楼主按 authorid 分页拉取（最多 40 页，失败回退到采样内楼主发言）、按 pid 补拉父楼（缓存、30 次上限、403 握手）、BBCode → Markdown（折叠、强调色白名单、引用、列表、居中标题、字号标题、粗体、链接、图片、残留标签剥壳）、回复上下文抽取、附件图本地化（80 张上限、失败保留外链、按魔数定扩展名）与未内嵌附件的补图；`fetchThreadByPlatform` 的 nga 分支已接入，导入 NGA 链接不再降级 | 2 层：①TS 差分——`nga-cases.mts`（由 `golden-forum.mts` 引入）把真实 `nga.ts` 跑在 112 组夹具上，Go 全部一致：BBCode 转换 21（含图片映射与自定义附件基址）、回复上下文 11、JSON 解析 9、控制字符转义 6、Cookie 合并 6、图片地址解析 9、guestJs 抽取 5、标题实体解码 8、整帖抓取 37（完整帖含分页采样/楼主筛选/附件图/孤立附件/父楼引用、握手成功、访客被拒、15 与 5 开头错误码、其他错误码与详情截断、`__MESSAGE` 三种、无数据、无法解析、无主题、无楼层、6 种抓取错误、重试成功与耗尽、采样页失败、采样页数封顶、楼主筛选失败回退、楼主分页上限、无楼主 ID、从采样推断楼主、主楼缺失、父楼补拉缓存/失败/封顶/握手、图片上限与下载失败、`_ATTACH_BASE_VIEW` 带协议、`__F` 根名称、空回复跳过），并逐次比较请求 URL、Cookie、`acceptStatuses`、UA、Referer 与图片下载顺序；②手写——图片魔数识别（jpg/png/gif/webp/bmp 与回落）、导入分发里 NGA 走注入抓取（不触网）。**已知差异**：同论坛连接器，Markdown 以外的转换均为逐字移植；默认图片下载走 `safeFetcher.downloadToTempFile`（SSRF 防护），未做真实站点验证。未验证：真实 NGA 站点（guestJs 握手、反爬与真实编码）、真实图片 CDN、`__U` 里更多用户字段形态 | 常量频道待迁 35 / 字面量 33 全部登记 |
| 11-12 | 在线视频连接器·yt-dlp 平台（对应 `import/video-url.ts`、`video-captions.ts`、`media/video-retranscription.ts`）：B 站 / YouTube 的元数据（`--dump-json`）、平台字幕（发布者字幕优先、再自动字幕，清洗 VTT/SRT、按 `-orig` / 中文 / 英文排序）、音轨下载（`--max-filesize 300m`）、转写 → AI 排版 → 总结与 AI 标题（原标题入引用块）、转写失败的可操作文案（HTTP 401/403/404/429/5xx）、未安装 yt-dlp 与解析失败的降级、取消传播；重转写按保存的访问链接优先；导入分发的视频路由与 `downloadItemVideoAudio` 已接入，`media_pending.go` 删除 | 2 层：①TS 差分——`golden-video.mts` 加载真实 `video-url.ts` / `video-captions.ts`（electron 桩 + 隔离 APPDATA，与媒体基线同法），yt-dlp、转写、排版、总结全部脚本化注入，76 组 Go 全部一致：失败文案 11、时长格式 11、元数据解析 10、正文组装 6、字幕清洗 7、字幕下载 9（发布者/自动/出错/无字幕/目录被清/排序/语言解析）、整条流程 22（ASR 全链、字幕免转写、未配置模型、未安装、解析失败、音轨下载失败各类、转写失败/空、说话人分离、排版跳过/部分/失败、总结失败/同名/无原标题、缺标题与链接、频道回退），逐次比较阶段回调与每次 yt-dlp 调用的参数和超时；②手写——真实子进程执行（成功、退出码取 stderr 末 3 行、300 码元截断、超时杀进程、取消、找不到可执行文件）、本地引擎启动失败写成注记、四个阶段的取消传播不被写进注记、字幕失败回落 ASR、导入分发与阶段透传、重转写守卫、平台错误码抽取。**已知差异**：JSON 语法错误文本与 V8 不同；字幕文件排序的 `localeCompare` 用「不分大小写、平手小写在前」近似；取消统一以「已取消」上抛（TS 在字幕失败分支里 `throwIfAborted` 抛的是 AbortError，会被当作普通失败吞掉）；字幕不带 cue 时间轴（仅研究功能使用，冻结）。抖音、小红书与图文作品见下一行（本行写成时尚未接入）。未验证：真实 yt-dlp 对 B 站 / YouTube 的输出与字幕文件、真实音轨下载、真实 ffmpeg 转码 | 常量频道待迁 35 / 字面量 33 全部登记 |

对拍中发现的 JS 语义（Go 里必须显式复刻，否则静默偏差）：
| 11-12 | 抖音 / 小红书 / 图文作品连接器与图片 OCR（对应 `import/douyin.ts`、`xiaohongshu.ts`、`image-note-entry.ts`、`platform-parse-log.ts`、`media/ocr.ts`、`shared/utils/ocr-request.ts`）：抖音分享页 `window._ROUTER_DATA` 配平切片与解析（视频 / 图文判定、无水印地址、文案首行标题、不可用原因）、短链先解 ID；小红书 `__INITIAL_STATE__` 切片（字符串外裸 `undefined` 补 `null`）与解析（桌面 UA、`xsec_token` 404 重定向识别、话题标记清理、流地址 http→https 去重、毫秒 / capa 秒时长）；配图逐镜像下载（小红书按文件头定扩展名）、逐图 OCR（visionText 路由、OpenAI / Anthropic 请求体、按张记用量、最多 9 张）、图文条目组装（元数据引用块、纯文本转 Markdown、识别失败如实写入正文、作者标题保留 / 抖音 AI 重拟）；导入分发的抖音 / 小红书路由与重转写已接入，`video_pending.go` 删除 | 2 层：①TS 差分——`golden-video.mts` 经 `imagenote-cases.mts` / `douyin-cases.mts` / `xhs-cases.mts` 加载真实 TS 模块（下载、落盘、OCR、拟题、分享页抓取全部脚本化），Go 对拍 226 组全部一致：纯文本转 Markdown 24（行首标题 / 列表 / 引用 / 围栏 / 分隔线转义）、元数据行 3、OCR mime 11、OCR 请求体 12（含 Anthropic 拆 base64 块）、OCR 响应解析 15、图文条目整条流程 12（下载 / 落盘失败、OCR 部分 / 全部失败、超 9 张、拟题 null / 抛错 / 无素材、作者标题不重拟）；抖音 ID 抽取 14、分享链 2、扩展名 10、分享页解析 41（循环字面量、数字键顺序、过滤原因、时长取整、对象与数组形态）、抓取编排 9；小红书 ID 与规范链接 11、切片 9、话题清理 9、笔记解析 36、抓取编排 8；②手写——`httptest` 的 OCR 请求（路径、`Authorization` / `x-api-key`、`User-Agent: node`、data URL、HTTP 错误体截 300 码元、空文本、缺内容）、图文下载与 OCR 阶段取消传播、`resolveDouyinSource` / `resolveXiaohongshuSource` 视频与图文两路组装、小红书配图文件头嗅探与镜像降级（桌面 UA、`Referer`）、非数组形态的普通错误。**已知差异**：JSON 语法错误文本与 V8 不同；`images` / `imageList` 为非数组时 Go 的错误文案近似 V8 的 `is not a function`（不影响错误码判断）；OCR 超时 / 网络失败文案是 Go 的原因文案而非 undici 的 `fetch failed`。**离屏页面回退未迁移**：抖音分享页缺数据时 TS 会读离屏桌面页自身的详情响应，Go 在渲染宿主（WebView2 + CDP）迁移前直接把 `[structure_missing] 分享页未返回作品数据…` 报给用户，不静默产出空条目。未验证：真实抖音 / 小红书页面与 CDN、真实视觉模型 OCR、真实长图文的耗时 | 常量频道待迁 35 / 字面量 33 全部登记 |
| 11-12 | 本文问答（对应 `ipc/article-ask.ipc.ts`、`article-ask-context.ts`、`article-ask-search.ts`、`shared/utils/article-context.ts`、`reading-source.ts`、`themed-reading/search-service.ts`、`native-search*.ts`、`search-result-url.ts`）：`articleAsk:context`（正文 / 总结 / 文字稿 / OCR / 论坛讨论 / 网页快照六种视图的来源组装、选段一致性校验、指纹、原文补充、选段与关键词选块、1200 码元分块与 2200 / 2500 / 12000 预算）、`articleAsk:search` / `cancelSearch`（请求标识校验、1–2 个搜索词、同时只允许一个、90 秒超时、取消释放占用；串行检索、URL 去 hash 去重、最多 5 个来源、至多 3 次补抓、警告去重）；联网搜索服务（Tavily / AnySearch / 模型服务原生搜索，固定官方端点、不跟随重定向、不回显上游正文、4 / 8 MiB 上限、密钥经 safeStorage 同格式加密保存在 `.machine/reading-search.json`、非 Windows 仅进程内存）；`themedReading:searchConfig` 随之接入 | 2 层：①TS 差分——`golden-video.mts` 经 `article-cases.mts` 加载真实 TS 模块，Go 对拍 162 组全部一致：文本归一 12、选块 16（预算边界、选段锚点、emoji 分块、CRLF、超长单行）、图文分段 7、阅读来源 10、目标解析 24、整条 `articleContext` 24（临时 SQLite 写入八个条目后调用真实实现：各视图、已删除、缺失、选段缺失、问题超长）、公开地址判定 24、原生搜索响应解码 15（JSON / SSE / CRLF / 多行 data / 失败终态）、原生搜索结果 16、原生端点推导 14；②手写——`httptest` 的 Tavily / AnySearch 请求与解析（Bearer、请求体字段、5 条上限、60000 码元截断、内网地址过滤、HTTP 401 / 500 文案不泄露上游正文、重定向不跟随、未配密钥提示）、原生搜索 SSE 请求（`tool_choice: required`、`Accept: text/event-stream`、404 文案）、配置加密落盘与读回、清空密钥与参数校验、查证编排（补抓上限、去重、失败警告、无来源警告）、IPC 校验、并发占用与取消。**修正**：Go 版在全新数据目录保存搜索密钥时先按 Chromium 格式新建 `Local State` 主密钥（Electron 启动时总会有）。**未迁移**：`themed` 视图（AI 阅读页）的来源取自 AI 阅读页版本，待主题阅读迁移后接入，目前如实报错。未验证：真实 Tavily / AnySearch / 原生搜索服务、真实网页补抓 | 常量频道待迁 31 / 字面量 33 全部登记 |
| 11-12 | 主题阅读渲染层（对应 `themed-reading/content.ts`、`document.ts`、`v3-document.ts` 及其校验与净化依赖）：**决策（自主，需向用户披露）**——与 sanitizer 同理，纯渲染层以 TS 原文件经 esbuild 打成 IIFE 后托管在 goja（`spikes/mygo-js/build-themed.mts` → `mygo-shell/themed_engine.js`，go:embed；echarts / mermaid 等内置库由 `build-reading-libs.mts` 生成 `reading_libraries.json`，随 exe 旁放置、不入库），理由：它是安全边界（净化、CSP、脚本语法检查）、依赖链深（markdown-it / linkedom / css-tree 等）、要求与原版逐字节一致；校验器、数据库、运行时、流水线、素材、视图仍按 Go 重写。Go 入口 `themedCall`，`linkedom` 的可选依赖 `canvas` 按 Node 语义抛 MODULE_NOT_FOUND；脚本语法检查由 Go `goja.Compile` 提供 | TS 差分 156 组（`golden-themed.mts`）全部一致：正文分块 18、v1 / v3 文档 28、v3 校验 28、v3 内层文档 36（四种开关）、导出文档 18、小节规整 17、HTML 清理 3、脚本语法检查 8。**已接受差异**：脚本语法错误的引擎原文（goja 与 V8 措辞不同），比对时只保留「交互 xx 语法错误：」前缀、种类、单元与行号。未验证：goja 在数 MB 页面上的耗时 | 常量频道待迁 31 / 字面量 33（渲染层尚未接到任何频道） |
| 11-12 | 主题阅读数据层与同步频道（对应 `packages/db/src/themed-reading*.ts`、`ipc/themed-reading.ipc.ts` 的非运行时部分）：`ThemedReadingDB`（页面/版本/任务的读写、发布与回退、角色互斥、运行中任务中断、旧版页面/任务启动清理）、`themedReading:preview/references/state/get/listTasks/restorePrevious/remove/exportHtml`；**决策（自主，需向用户披露）**——版本与任务的结构校验器同样托管在 goja（TS 原文件 `validateVersion` / `validateTask`），数据库与 IPC 逻辑为 Go 重写；goja 缺全局 `URL` / `TextEncoder`，分别以 Go WHATWG 解析（`__gzURLParse`）与 JS 垫片补上（否则公开地址判定恒为假，v3 引用被误判无效） | 2 层 TS 差分：①数据层 66 步脚本（`golden-themed-db.mts`），保存/发布/回退/删除/中断/互斥/损坏版本/任务归属，比较返回值与全表终态；②IPC 75 组（`golden-themed-ipc.mts`，含 v1/v3 页面、预览、引用、图片完整性校验含篡改/尺寸不符/非图片、导出 HTML 内联资源、回退缺图、垃圾桶条目拒绝）+ 终态库比对，全部一致。**已接受差异**：脚本语法错误引擎原文；ENOENT 报错里的绝对路径掩码后比较；Go 桥无 owner-sender 校验（本壳单窗口）；图片健康缓存签名仅用 size+mtime（Node 另用 ino/dev/ctime）。未验证：goja 对超大版本 JSON 的耗时；运行时/流水线/视图/素材频道仍未迁（见 themedReading 余项） | 常量频道待迁 22 / 字面量 33 全部登记 |
| 11-12 | 网页渲染窗口（对应 `web-electron-renderer.ts`、`web-settle.ts`、`web-network-activity.ts`、`web-inert-resources.ts`、`web-subframe-policy.ts`、`web-rendered-document.ts`、`web-network.ts` 的子请求出口）：采集浏览器基础设施（`capture_browser.go`/`capture_cdp.go`，来自 mygo-capstress：归知自己的另一个实例作为子进程托管隐藏 MyGo 窗口 + WebView2 远程调试端口，主进程用 CDP 控制，窗口池 2 个、异常窗口整个替换、崩溃由连接断开立即发现并在下次渲染时重启、空闲 90 s 自动退出）与 `renderHost`（`render_host.go`/`render_net.go`/`render_settle.go`，接入 `Library.webRenderer`，网页采集里「静态判定需要渲染 / 解码失败 / 静态提取为空」的回退不再报「尚未接入渲染进程」）。安全模型沿用 Electron 版：浏览器不解析任何域名（`--host-resolver-rules=MAP * ~NOTFOUND`），所有 http(s) 请求在 CDP Fetch 域暂停，由 `safeFetcher`（SSRF 校验 + IP 钉扎 + 代理）取回后 `Fetch.fulfillRequest` 应答；跨源子框架不加载、主框架越出目录范围即终止、单页 ≤200 个请求与 50 MiB、图片以 1×1 透明 GIF 应答、响应头追加 `worker-src 'none'`、下载与弹窗一律拒绝；页面侧脚本（沉降探针、DOM 快照含 shadow root 展开与代码编辑器还原）由 `build-render-scripts.mts` 从 TS 原函数 `toString()` 生成 `render_scripts_gen.go`（esbuild 关闭 keepNames，否则序列化后页面里没有 `__name`）。任务结束清 Cookie / 缓存 / 本任务出现过的源的存储 | 手写端到端 6 组（真实 WebView2 隐藏窗口 + 本地 httptest 经 `site.test` 钉扎）：脚本渲染的单页应用（XHR 后写入正文）、服务端 302、脚本 `location.replace` 跳转、图片不出网、同源子框架加载而跨源不加载、`window.open` 与 `alert` 不卡住且弹窗页不被请求、任务间 Cookie / localStorage 不串、页面访问回环地址以 security 终止且出口未拨号、目录范围越界终止且范围外页面未被请求、260 个请求的洪泛触发预算、PDF 入口报非网页内容、洪泛后窗口替换仍可用、超时 1.5 s 内返回 timeout、2 并发 + 1 排队、进程被杀后 5 s 内报错并自动重启、整条 `captureWebPage`（静态→渲染→提取）；沉降等待 7 组假时钟（静止 / 空壳 / 持续变化 / 请求在途 / 盲等 / 固定 / 取消）。**已接受差异**：资源类型取自 CDP `resourceType`（TS 只能按 Accept 粗分，更准）；Accept-Encoding 不含 br；沉降滚动脚本失败时不中断（TS 会抛）；`Fetch` 事件里超大请求体（`hasPostData` 却无内联数据）按空处理；页面权限请求（摄像头等）在隐藏窗口里无人应答而非自动拒绝；每任务用池窗口而非独立 Session（靠任务后清理隔离，并发的两个任务共享同一 WebView2 配置文件，Cookie 在并发窗口内可能互见）。**未验证**：真实第三方站点（Cloudflare 挑战页、懒加载、反爬）、采集窗口池在长时间/大批量下的内存与窗口泄漏（第二轮只测了阅读页开合）、`GUIZHI_WEB_*` 环境开关、交互式网页验证窗口与 FlareSolverr（未迁）、渲染路径与 Electron 版的差异（第二轮对拍只覆盖同一份 HTML 的静态提取） | 常量频道待迁 22 / 字面量 33 全部登记（渲染窗口本身不对应频道） |
| 11-12 | 打包与端到端：`startup_paths.go`（没有 `-db` 即视为正式启动，用户数据目录按 `resolveInitialUserDataPath` 推断：自定义目录 > 历史默认目录 `%APPDATA%\GuiZhi` > 安装目录旁 data；dist 在 exe 旁或上一级；显式 `-db/-dist/-userdata/-config/-appdata` 的开发用法不变）、`spikes/build-shell.ps1`（vite 构建 preload 与 renderer → 生成内置阅读库 → `go build -trimpath -ldflags "-s -w -H=windowsgui -X main.packagedBuild=true -X main.appVersion=<apps/desktop 版本>"` → 汇总到 `spikes/mygo-shell/out/win-x64`，不碰 Electron 的 `out/`）、`e2e_test.go` | ①打包：产物 38.3 MB（GuiZhi.exe + dist + reading_libraries.json）；在临时 `APPDATA` 下无参数启动（`-shot` 离屏截图）：自动创建 `GuiZhi\data\knowledge.db` 并跑完迁移，渲染进程显示首次启动引导（截图 `%TEMP%\guizhi-pkg-run\pkg.png`）。②端到端（`TestEndToEndCaptureSearchAndStreamingAsk`，与 main.go 同一装配路径、经 Bridge 频道调用）：`import:enqueue` 一个本地 httptest 文章网址 → 静态采集提取入库 → `knowledge:list` 全文检索命中独有词且不命中无关词 → `articleAsk:context` 取正文上下文 → `ai:httpStream` 流式问答（本地 SSE 模型，已配置端点校验走真实 `ai-models.json`），逐块收到回答且模型收到的提示词含文章内容。`aiProxy` 新增测试用推送钩子 `emit`（无窗口时观察流式块，生产路径不变）。**未验证**：安装包形态（NSIS/签名/自动更新对接）；资源管理器/开始菜单启动路径；`dist` 嵌入 exe（当前随 exe 旁放置）；真实窗口里点击驱动的整条 UI 流程 | 常量频道待迁 22 / 字面量 33 全部登记 |
| 11-12 | 本文问答「AI 阅读页」来源：`themed_article_source.go`（`themedArticleSource` 不再报「尚未迁移」；版本经 goja 里的 TS `themedReadingDocument` 渲染后用 `x/net/html` 取可见文字，去 script/style/nav/button/input/textarea，块级元素末尾补空行；原文补充与最多 3 条草稿实际引用过的就绪资料同 TS） | `golden-themed-ipc.mts` 新增 13 条 `thmArticle` 基线（真实 TS `articleContext`，与同步频道共用种子库）：v3 当前页、显式 sourceKind、选段命中/不一致、sourceKind 不符、版本不属于本文、缺 versionId、不存在版本、v1 旧页、论坛旧页、working 空稿、回收站条目、问题超长，全部与 Go 一致。**未验证**：linkedom 与 `x/net/html` 在畸形 HTML 上的解析差异（基线只含渲染器自己产生的合法文档） | 常量频道待迁数不变 |
| 11-12 | 主题阅读生成管线（对应 `themed-reading/{pipeline,generation,tasks,verify}*.ts`）：**决策（自主，需向用户披露）**——整条异步流水线（计划 → 逐单元生成 → 校验 → 修复 → 发布）以 TS 原文件托管在第二个 goja 引擎（GZP，`themed_pipeline.js`，`build-themed.mts` 生成），Go 提供事件循环（`setTimeout`/microtask/Promise 宿主）与宿主桥（模型调用、联网搜索、素材读取、阅读页校验、取消、进度推送），数据库与任务调度由 Go 持有 | `golden-themed-pipeline.mts` / `themed_pipeline_test.go`：15 个脚本化场景（短文正常、编辑追加问题、截断后续跑、流中取消、编辑非 JSON、429 限流、设计小节无效、联网研究计划/搜索失败/限流/结果无关/采集失败/完整/缺口、含脚本的设计、修复成功/仍失败/语法错误）逐步比较宿主桥调用序列（参数）与最终数据库状态，Go 全部一致。**已接受差异**：goja 与 V8 的错误原文不同（比较时只保留种类）；定时器精度；模型流式分块边界不比较 | 未验证：真实模型驱动的长任务耗时与内存；goja 在大 prompt 下的开销 | 常量频道待迁数不变 |
| 11-12 | 主题阅读运行时与素材（对应 `themed-reading.ipc.ts` 的 `generate/cancel/resume/continueOffline/regenerateAsset`、素材落盘 `themed-assets*.ts`、生图 `image-generation/*.ts`）：任务启动/取消/继续/离线续做、重新生成素材、图片素材按内容哈希落盘并登记、生图（OpenAI 兼容 / 其他协议请求体、尺寸与质量、响应解码、失败归类）**自主决策**：生图客户端用 Go 重写，不托管 TS | 运行时 TS/Go 30 步差分（`golden-themed-runtime.mts` / `themed_runtime_test.go`：启动、重复启动拒绝、取消、续做、素材重生、状态推送顺序、库终态逐表比对）全部一致；生图用本地 httptest（`image_gen_test.go`）覆盖请求体（含修复 OpenAI 请求体序列化为空的缺陷）、b64/url 两种响应、HTTP 错误与超时文案，不访问真实服务、不使用真实密钥。**已接受差异**：生图 HTTP 错误体截断长度与 Node undici 文案不同；`compileReadingVisuals`（配图编排）属冻结的 reading-graphics 功能，不迁，生成流程里直接跳过。未验证：真实生图服务（各厂商参数差异）、运行时用量账本与 TS 的逐项对账 | 常量频道待迁数不变 |
| 11-12 | 主题阅读视图与阅读页运行检查（对应 `themed-reading/view-host.ts`、`reading-view-*.ts`、`reading-page-probe.ts`、`themedReading:viewCreate/viewUpdate/viewCommand/viewDestroy` 与 `readingView:bridge`）：Go 侧 `reading_views.go` 持有视图状态机（最多 3 个视图、ready/key/layout/selection/find/image/fault/heartbeat 消息的校验与限流、250 ms 心跳巡检、5 s 无心跳判故障并销毁）并生成带中继脚本的包装文档；渲染层由 `shim/reading-view-host.ts` 在 electron shim 内把视图实现为 `<iframe sandbox="allow-scripts" srcdoc>`（校验 source 与 viewId 后转发为 `readingView:bridge`，监听 `readingView:command`）；`reading_probe.go` 在隐藏 WebView2 + CDP 里加载草稿页做运行检查（`Runtime.addBinding`、心跳、6.5 s 上限、750 ms 最小停留、「阅读交互初始化未完成」），接入 `themedVerify`，修复步骤因此真正运行（此前被跳过）。**自主决策**：视图用 iframe 而非 Electron 的 `WebContentsView`；`probeReadingPage` 用 WebView2 | `reading_views_test.go`（命令/边界校验、消息校验、心跳故障、销毁清理、包装文档含中继脚本且无外链）与 `reading_probe_test.go`（本地合成页：正常初始化、不发心跳、同步死循环触发 5 s 心跳故障）全部通过；视图文档生成与 TS 的 `v3Inner`/`v3Wrapper` 同源。**已接受差异**：WebView2 的 sandboxed iframe 不一定做进程级隔离（Electron 的 `WebContentsView` 是独立渲染进程）；同步死循环仍由 5 s 心跳判故障，但拖慢宿主页面直到被销毁。**第二轮已验证**：真实 UI 里 shim 中继的完整往返（选区/查找/图片放大/Esc 转发，dpr 2 下宿主矩形与 iframe 矩形一致），事件为页内合成、非真实鼠标键盘。**仍未验证**：真实鼠标键盘穿过 iframe、窗口缩放/多显示器下的 `layout` 精度 | 常量频道待迁 0（含冻结 43） |
| 11-12 | 平台登录采集（对应 `platform-capture/{browser-capture,authenticated-platforms,platform-policy,nodeseek-*}.ts`、`douyin-detail` 与评论补采，频道 `platformCapture:status/login/cancelLogin/logout/clearAll/listComments/refreshComments`，及导入的「认证采集」策略；`discoverCreator/search/cancelDiscovery` 属冻结的发现功能，登记为桩）：每平台一份持久 WebView2 配置文件（`browser-capture/webview2-<platform>`）+ 同一 exe 的子进程 + CDP；域名白名单与登录流程/资源域名策略（`platform_policy.go`）；登录检测按 Cookie 名；登录页不加载媒体与信息流接口；`capturePage` 返回 HTML 与页面自己发出的 JSON 响应；`fetchJSONViaSession` 在登录会话里读接口；抖音详情响应、评论递归扫描（顶层评论、点赞排序、毫秒/秒时间、回复子树排除）、NodeSeek 帖子（验证页识别、楼层解析、分页上限 100 页、`fetchNodeseekThread`）；评论存入 `source_comments`；登录失效响应（`未登录`/`login_required` 等）清除登录标记；退出/清空时删除会话目录（Windows 句柄释放重试）。**自主决策**：平台会话用 WebView2 持久配置文件代替 Electron 的 `persist:` 分区，状态文件改名为 `session-status-webview2.json`——**用户在两种外壳间切换时需要重新登录一次**；`fetchJSONViaSession` 用页面导航读 JSON，而非 `session.fetch`，WebSocket 流量绕过 Fetch 拦截 | 无 TS 差分（行为全是浏览器会话与真实站点）。手写测试（本地 httptest，`window=offscreen`，策略替换为只放行本地 mock；不访问任何真实站点）：`TestPlatformSessionLifecycle`（登录检测靠页面脚本写 Cookie → 状态落盘 → **关闭浏览器后重启仍带 Cookie** 读接口 → 非 JSON 拒绝 → `capturePage` 取得页面 JSON → 登录失效响应清标记 → 评论补采（顶层、排序、时间换算）→ 非平台地址拒绝 → 退出登录清空会话目录并 403 → `clearAll`）、取消与超时、策略表与登录页请求拦截表、代理参数（口令不进命令行）、`parseNodeseekPage`/`fetchNodeseekThread`（验证页、结构变化、无主楼、相对链接补全、脚本剔除、分页、逐页串行、任一页失败整体失败）、`scanPlatformComments`/`indicatesLoginRequired`/`isLoginPage`/`platformFromAuthenticatedURL` 与 TS 逐条对照、`source_comments` 的 upsert 与排序。测试中发现并修复两个缺陷：持久会话目录复用时子进程读到上次遗留的 `DevToolsActivePort`（现启动前清除，并让 CDP 拨号对 connection refused 短暂重试）；退出登录时浏览器子进程句柄尚未释放导致目录删除失败（现重试）。**未验证**：真实小红书/抖音/LINUX DO/NodeSeek 的登录与采集（需要真实账号与真实站点，须用户确认才能做）；真实人机验证（Cloudflare Turnstile）；抖音详情页（地址写死为官方域名，无法用本地 mock 覆盖，只对拍响应提取函数）；与 Electron 版共存时的登录态（两者不互通）；DPI/显示器变化下的可见登录窗口位置 | 常量频道待迁 0（含冻结 43） |
- `new Map(entries).values()`：顺序取键**第一次**出现的位置，值取**最后一次**写入的值（`inbox:applyAiClassification` 的新建集合名因此是后出现的写法）。
- `toLocaleLowerCase` 与 Go 的 `strings.ToLower` 有两处不同：`İ` 展开为 `i̇`，词尾 `Σ` 变 `ς`。
- 字符串长度按 UTF-16 码元计，一个 emoji 算 2；`/\s+/` 的空白集合与 Go 的 `unicode.IsSpace` 不同（沿用 `jsSpace`）。
- 同一场景里前面的操作会改变后面操作的前置条件，校验类场景必须一场景一操作，否则会被前一步遮住（已用 `applyInvalid00…31` 避免）。

接下来的顺序（先 DB 内、后带副作用；每组先扩 `make-golden.mts` 再写 Go）：
1. （已完成）问答会话 `ask:*`、`askSession:*`：依赖有序 JSON（保持键顺序、与 `JSON.stringify` 同样的数字与转义规则）和 `evidenceText/evidenceUrl`；有序 JSON 是后面许多频道共用的基础件，先做。
2. `ai:usage*`、`semantic:*`、`wiki:*` 的库内读写、`knowledge:saveDraft/selection`、`export:*`。
3. 后台任务运行时、导入队列、备份与备份仓库频道、迁移检测、`config:*`、`mcp:*`、更新、快捷键、网络代理、可执行文件白名单。
4. 采集一族、媒体一族、主题阅读、Wiki 编译器。
5. 打包、端到端用例、`pnpm shot` 式的隔离离屏验证。

### 全量迁移中的自主决策清单（须向用户披露，2026-11-12）

用户目标是「全部迁移完成」，下列取舍是迁移过程中在没有逐项询问的情况下自行做出的（均已在对应切片行写明依据与差异），汇总如下，任何一项都可以推翻：

| # | 决策 | 理由 | 代价 / 可逆性 |
|---|---|---|---|
| a | 主题阅读纯渲染层（正文分块、文档/内层文档、导出文档）以 TS 原文件经 esbuild 托管在 goja | 它是安全边界（净化、CSP、脚本语法检查），依赖链深，要求与原版逐字节一致 | 升级依赖需重新打包；goja 在多 MB 页面上的耗时未验证；可逐步用 Go 重写 |
| b | 版本/任务的结构校验器（`validateVersion`/`validateTask`）托管在 goja | 校验规则多且与渲染层共用类型，手写移植易漂移 | 同上 |
| c | 主题阅读生成流水线以 TS 托管在第二个 goja 引擎，Go 提供事件循环与宿主桥 | 流水线是长异步编排，TS 原文件对拍成本最低 | 错误原文与 V8 不同（已比较种类）；需重新打包 `themed_pipeline.js` |
| d | 网页渲染用「同一 exe 子进程 + 隐藏 WebView2 窗口 + CDP」，请求在 Fetch 域暂停后由 Go 安全出口应答 | 沿用 Electron 版的安全模型（浏览器不解析域名） | 并发任务共享同一 WebView2 配置文件；真实站点上的稳定性未与 Electron 对比 |
| e | 跳过 `compileReadingVisuals` | 属冻结的 reading-graphics 功能 | 生成的阅读页不含该功能产出的配图 |
| f | 主题阅读视图用 shim 内沙箱 `iframe` 实现，Go 侧负责校验、心跳与故障 | MyGo 没有 `WebContentsView` 等价物 | WebView2 不保证对 iframe 做进程隔离；同步死循环靠 5 s 心跳判故障 |
| g | 阅读页运行检查（`probeReadingPage`）在隐藏 WebView2 + CDP 中实现 | 修复步骤依赖它 | 与 Electron 的检查窗口行为近似但不逐项对拍 |
| h | 平台发现频道（`discoverCreator`/`search`/`cancelDiscovery`）按冻结功能登记为桩 | 发现功能在冻结清单内 | 如要解冻需另行迁移 |
| i | 平台登录会话用 WebView2 持久配置文件，状态文件改名为 `session-status-webview2.json` | Electron 的 `persist:` 分区无法复用 | **用户在 Electron 版与 Go 版之间切换时需重新登录一次** |
| j | `fetchJSONViaSession` 用页面导航读 JSON，不用 `session.fetch` | CDP 下没有等价的会话级 fetch | WebSocket 流量绕过 Fetch 拦截 |
| k | 渲染进程的 `local-image://`/`local-video://` 由 Go 注册为 `http://<scheme>.localhost/`，并在 preload 里改写页面地址（2026-11-12 第二轮） | WebView2 不支持自定义协议，没有别的入口 | 改写依赖 preload 的属性/`srcdoc`/MutationObserver 覆盖面；`local-generation-image` 未处理（冻结功能） |
| l | 正文提取关闭 html-to-markdown 的 `<!--THE END-->` 列表分隔注释（第二轮） | 该注释会进入正文、全文索引与 AI 上下文；Turndown 没有这种行为 | 相邻列表在 Markdown 里可能被渲染器合并为一个列表（与 Turndown 一致） |

### 最终验收（2026-11-12）

| 验收项 | 结果 | 证据 |
|---|---|---|
| 渲染进程可调用的频道全部有处理器 | 214 个 invoke + 17 个 send，**0 个未实现**。清单由 `spikes/mygo-js/gen-channels.mts` 从 preload 的 `ipcRenderer.invoke/send` 生成（`ipc_channels_gen.go`），启动时自检并写入日志 `[ipc] …均已有处理器`；`migration-status.ps1` 另按字符串统计：230 个常量频道已登记 187 个、冻结 43 个、待迁 0，常量之外的字面量频道 33/33 | 离屏启动日志；`frozen_ipc_test.go` |
| 冻结功能的处理 | 研究 / 配图 / 发现视图 / 阅读配图 / 手机收集 / 阅读同步：读取列表类频道（`research:list`、`research:baselines`、`discoveryView:list`）返回空列表，其余明确报「该功能已冻结」。**例外**：配图风格预设的读写（`illustration:styles/builtInStyles/saveStyles/revealStylesFile`）有 Go 实现（属配置迁移的一部分，设置页可用）。此前 `research:list` 在启动时被调用 3 次且无处理器，渲染进程会进入错误态——这是验收时才发现并修复的缺口 | 同上 |
| 各频道组的 TS/Go 差分 | 全部通过，带全部 golden 环境变量的 `go test -count=1 ./...` 通过（约 125 s）。仍跳过的 4 项：`TestConfigExportEncryptedDump`（需要 `GUIZHI_DUMP_DIR`，仅用于人工查看）、`TestFunasrGoldenConsumesTsDisposable`（TS 一次性目录，每次生成的 golden 只能消费一次）、`TestVideoHelperProcess`（子进程辅助入口）、`TestEvidence*` 在本次补跑 `make-golden-evidence.mts` 后通过 | `go test` 输出 |
| 主界面在 Go 壳上的离屏验证 | `build-shell.ps1` 产出的 `GuiZhi.exe` 在隔离 `APPDATA`/userData、`-shot` 离屏下：空库显示首次引导；复制的黄金库显示完整资料库（22 条、集合/平台/标签侧栏），点击条目后右侧正文正确显示。`-shot` 新增 `-shotjs <脚本> -shotafter <时长>` 用于截图前执行交互脚本 | `%TEMP%\guizhi-final-shot2\pop.png`、`detail.png` |
| 打包产物 | `spikes\mygo-shell\out\win-x64`：`GuiZhi.exe`（Windows x64，GUI 子系统）+ `dist` + `reading_libraries.json`，约 39.8 MB；`build-themed.mts`、`build-pipeline.mts`、`build.mts` 重新生成后与已提交的 `themed_engine.js`、`themed_pipeline.js`、`websnap_sanitize.js` 无差异 | `git status` 干净 |
| 端到端：采集 → 提取 → 入库 → FTS → AI 流式问答 | `TestEndToEndCaptureSearchAndStreamingAsk` 通过（本地 httptest 站点与 SSE 模型） | `e2e_test.go` |
| 依赖 | `go mod tidy` 后 `github.com/coder/websocket` 由间接依赖转为直接依赖（CDP 连接使用），其余无变化 | `go.mod` |

**仍然没有验证的事（汇总；第二轮已补：真实 UI 备份/恢复/重启整链路、数据库损坏路径、3 万条大库耗时、阅读视图 iframe 往返、与 Electron 的正文对拍、阅读页开合 300 轮泄漏压力，见下文「收尾验证」）**：NGA 的完整导入结果与附件图 HTTP 567 / 楼主分页重定向循环的原因、B 站平台字幕（本轮只取到元数据）、小红书/抖音/LINUX DO/NodeSeek 登录（用户选择跳过）与真实 SOCKS5/TUN；真实云端 ASR/LLM/OCR/生图/搜索服务；真实 Python/pip/torch 的 FunASR 安装；Wiki 编译在渲染进程驱动下的真实模型循环；SSH 隧道 + FlareSolverr；安装包形态（NSIS、签名、自动更新对接）；与用户真实 Electron 实例并存时的租约与 `Local State`；主题阅读视图 iframe 在真实 UI 里的完整往返、WebView2 对 iframe 是否做进程隔离；goja 在数 MB 页面上的耗时；长时间运行下渲染窗口的内存与泄漏。这些都需要真实环境、真实账号或用户确认，没有在本轮自动化里做。

### 真实网站冒烟（2026-11-12，经用户确认；按当前系统网络直连，未改任何代理设置）

`real_net_test.go`（默认跳过，`GUIZHI_REAL_NET=1` 才运行）对 3 个公开页面各请求 1 次，渲染窗口离屏：

| 页面 | 路由 | 结果 |
|---|---|---|
| `https://example.com/` | 静态判定正文过短 → 渲染回退（`readability/1-electron`） | 3.5 s，标题与正文正确 |
| `https://en.wikipedia.org/wiki/SQLite` | 静态（`readability/1-static`） | 1.1 s，约 6 万字符，含信息框表格 |
| `https://quotes.toscrape.com/js/` | 需渲染（页面由脚本写入内容） | 7.1 s，名言正文被取到 |

只证明静态抓取、渲染回退和提取在真实站点上能跑通；与 Electron 版的正文对拍、论坛与视频站点、长时间运行见下一节「收尾验证」。

### 收尾验证（2026-11-12 第二轮，隔离沙箱 + 离屏；全部脚本在 `spikes/mygo-shell/verify/`）

用户在本轮从 6 项未验证事项里全部选中；登录类站点（小红书/抖音/LINUX DO/NodeSeek）明确选择**跳过**，安装包只做设计、不实施。每个沙箱在 `%TEMP%\guizhi-verify\<名>` 下，`APPDATA` 只设置在子进程上，没有碰用户的 electron:dev 实例、数据目录与 Vite 端口，没有按名称批量结束进程。

| 项 | 结果 | 复跑 |
|---|---|---|
| 备份 → 恢复 → 重启整链路（真实 UI 点击） | 22 条黄金库：点「立即备份」后快照 101 ms 出现在列表；之后新建 1 条（总数 23）；点「恢复」后进程重启，总数回到 22，被回滚条目全文命中 0、原有条目命中 1；自动留下 `knowledge-pre-restore-*.db`（含被回滚的条目，字节搜索确认），`error.log` 为空 | `run-backup-chain.ps1` |
| 数据库损坏的真实进程场景 | freelist 计数被改写：**自动修复**，22 条完整、留下 1 份完整性备份；中间 5 页被随机数据覆盖 / 截断一半 / 整个文件被文本替换：**拒绝启动**（退出码 1，0.1 s），写入 `logs/error.log`（`malformed (11)` / `not a database (26)`），交互启动时弹系统对话框（本脚本是自动化实例，不弹窗，所以对话框本身没有在真实桌面上观察过）；0 字节文件：当作空库启动（与 TS 一致，**不是**数据丢失告警，见下） | `run-corrupt-db.ps1` |
| 大库（3 万条 / 752 MB） | 冷启动（无干净关闭标记，触发 `quick_check`）：完整性检查 21.5 s，初始化合计 22.9 s，**期间主进程无响应**；热启动 43 ms。库内操作：首页 28–35 ms、翻页 22–33 ms、稀有词检索 6–54 ms、常见词检索约 6–7 s、带检索的分面计数 5.7–7.4 s、整库计数 3.8–4.3 s、单文件备份 6.9–12.1 s（784 MB）。启动补齐全文索引原本是 O(n²)，已修复（见下） | `run-bigdb.ps1`（夹具见脚本头） |
| 主题阅读视图 iframe 真实 UI 往返 | 条目点击 → 视图创建 0.5–1.0 s；`ready` 1.8 s；宿主矩形与 iframe 矩形一致（dpr 2，`sandbox="allow-scripts"`）；`selection` 事件后出现划选工具条；Ctrl+F 打开查找栏，命中 30、栏内显示 `1 / 30`；Esc 转发；`image` 事件打开放大层且 320×180 原图加载成功；遮挡（放大层）期间原生视图隐藏、关闭后恢复 `visible:true`；`faults` 为空。**发现并修复缺陷**：渲染进程里所有 `local-image://` 都是破图（见下） | `run-reading-view.ps1` |
| 与 Electron 版正文对拍（同一份真实 HTML，TS `extractWebArticle` 与 Go 各提取一次） | `example.com`、`en.wikipedia.org/wiki/SQLite`：来源、标题、readability/区域字数一致；维基页去标记后的词元序列 9132 = 9132 逐词一致。对拍发现 1 个真实缺陷（相邻列表间插入 `<!--THE END-->`，已修）。其余仅为已接受的写法差异（见下） | `verify/real-parity.mts` + `TestRealPageParity` |
| 长时间运行（泄漏压力，300 轮「打开阅读页 → 切到普通条目」，6 分钟） | 进程树（GuiZhi.exe + 6 个 WebView2 子进程）工作集 153 MB 起步 → 约 700 MB（首次渲染后）→ 峰值 832 MB，**最后约 100 秒持平在 817–825 MB**；JS 堆呈 GC 锯齿，谷值自第 130 轮起稳定在约 33–35 MB；DOM 节点数始终 875；收尾时残留 iframe 0、视图宿主 0；300 轮 0 失败、0 fault | `run-leak.ps1 -Cycles 300` |

**真实网站（用户确认，每站 1 次只读、不登录、直连）**：V2EX `/t/1` 导入成功；LINUX DO `/t/topic/1` 被 Cloudflare 挑战拦下，按设计如实降级、不入库空壳（Electron 版靠登录会话降级，Go 版 `fetchAuthenticatedJSON` 注入点仍未接渲染宿主）；YouTube `watch?v=jNQXAC9IVRw` 取到元数据与平台字幕（文字稿 217 字符）。**NGA（`bbs.nga.cn/read.php?tid=37194262`，用户提供）**：一次完整导入的**结果摘要没有取到**（我把命令输出截断了，测试不落盘，重跑要再请求一遍，用户选择不重跑），只看到日志里的现象：楼主发言分页拉到第 18 页报 `[network] 重定向次数过多`（按设计回退到采样内楼主发言）；几十张 `img.nga.cn` 附件图全部返回 **HTTP 567**，两个 `img.nga.178.com` 地址 EOF（正文保留外链）。**（原因已在第三轮查明，见下一节「收尾验证（第三轮）」：567 是 TLS 指纹，已修；重定向循环不是移植缺陷，触发原因未查明。以下为当时的推测。）** **原因未查明**：567 可能是 NGA 图床的防盗链/风控，也可能是 Go 版请求头与 TS 版在真实服务上的差异（夹具里 `Referer`/UA 与 TS 逐项一致，真实服务未对比）；重定向循环可能是限流或 guestJs 会话在翻页中失效后的重定向。这两点是**潜在缺陷或站点限制，尚未区分**，需要拿到完整日志与 TS 版同页对比后才能下结论。**离线排查（只读源码，未再访问 NGA）**：附件图下载与翻页的请求头在 TS 与 Go 两边逐项一致——UA 同为 NGA 专用 UA、`Referer` 同为 `https://bbs.nga.cn/`（翻页为帖子规范链接）、图片 `Accept: */*`、`Accept-Language: zh-CN,zh;q=0.9,en;q=0.8`、图片请求不带 Cookie、均不发 `Accept-Encoding`、重定向上限同为 5 跳且文案相同（`重定向次数过多`），所以 567 与重定向循环**不能归因于请求头或重定向逻辑的移植差异**。剩下的候选：Go 与 Node 的 TLS 握手特征 / HTTP 头顺序（Go 的头按键排序写出，Node 按插入顺序）、请求节奏、站点自身的防盗链与限流。要区分这些，只能用 TS 版在同一网络、同一时间对同一页做 A/B，这需要再次访问 NGA，用户选择暂不做。附带披露：NGA 一次导入按产品流程会发出采样页 + 楼主分页 + 数十张附件图的请求，远多于「1 个页面」，用户确认的是「每站访问一次」。
**B 站（`BV1NNrCBPEWC`，用户提供，yt-dlp 官方 exe 在隔离临时目录）**：10.5 s，元数据取到（标题、作者、时长 12:52、简介），阶段 `video-metadata`、`video-captions` 走完但**没有取到平台字幕**（B 站字幕通常需要登录，未验证原因），因未配置转写模型只保存视频信息并给出可操作提示，无降级原因。此站为减少输出重跑过一次（共 2 次导入请求，第一次的结果被我截断）。

披露：调 YouTube 字幕时我曾手工运行过一次 `yt-dlp --sub-langs "en.*"`，产生多次字幕请求并遇到 HTTP 429，超出了「每站 1 次」的约定；yt-dlp 官方 exe 只放在隔离临时目录，没有加入 PATH。

**本轮修复的缺陷（均先写失败测试再修；提交未 push）**
1. **`local-image://` / `local-video://` 在渲染进程里全部无法加载**（影响图片放大、阅读页以外的所有本地图片与视频）：WebView2 不认自定义协议，壳只静态服务渲染目录。现在 Go 用 `mygo.Protocol.Handle` 注册两个协议（WebView2 实际访问 `http://local-image.localhost/…`），路径校验照搬 `local-media-protocol.ts`（拒绝 `.`/`..`/控制字符/冒号/反斜杠开头、扩展名白名单、必须落在目录内），支持 Range/304/`nosniff`；preload 的 `shim/media-urls.ts` 把页面里的地址改写成 `.localhost` 形式（属性、`setAttribute`、`srcdoc` 内的地址与 CSP 源、动态插入的节点）。路径解析 17 例 + 处理器 6 组单测；真实 UI 里主文档与沙箱 `srcdoc` iframe 内的图片探针均加载成功。`local-generation-image://`（冻结的配图功能）未处理。
2. **正文提取在相邻列表之间插入字面量 `<!--THE END-->`**，会进入正文、全文索引与 AI 上下文：`commonmark.WithListEndComment(false)`，公众号 Markdown 同步生效。
3. 启动失败（数据库损坏、渲染产物缺失）原先 `log.Fatalf`，GUI 子系统无控制台，用户双击后进程静默消失：现在写 `logs/error.log`，交互启动时弹系统对话框并指出备份与日志目录（自动化实例 `-shot/-bench/-aitest` 不弹窗）。
4. 启动时的全文索引补齐用相关子查询，1.2 万条 23 s、3 万条超过 10 分钟都不出现：改为非相关 `NOT IN`，1.2 万条 0.7 s。

**本轮发现但未改的 TS 原版问题**：同一条补齐 SQL 在 TS 版里同样是 O(n²)（实测 4000 条 1.8 s、8000 条 9.2 s），属产品原有缺陷，未改 TS（一行级改动，需要时可单独做）；`PRAGMA quick_check` 不比较索引内容与表内容是否一致；0 字节数据库文件被静默当作空库（Go 与 TS 一致，用户磁盘写满后的残留会表现为「资料全没了」而非报错，建议另行讨论是否在已有备份时提示）。

**新增的已接受差异**（正文提取，Go html-to-markdown 与 TS Turndown，词元序列相同）：主机名无路径的网址 TS 写 `https://sqlite.org/`、Go 写 `https://sqlite.org`；有序列表 Go 写 `01.`、TS 写 `1.`；斜体链接嵌套 `[_x_](u)` 与 `_[x](u)_`；页脚句末句号位置；以及此前已接受的转义、百分号编码、列表标记空格。

**这轮验证的边界（务必注意）**
- 阅读视图的选区、Ctrl+F、Esc、图片点击是在 iframe 自己的文档里**合成事件**触发的，不是真实鼠标键盘穿过 WebView2 iframe；WebView2 是否对 sandboxed iframe 做进程隔离同样没有验证。
- `local-video` 只有 Go 处理器的单测（含 Range 206），没有用真实 `<video>` 验证；微信快照里的图片只用了同形态探针，没有经真实数据库快照。
- 正文对拍只比较「同一份 HTML 字节」的提取结果，不覆盖请求头、重定向、编码探测；`quotes.toscrape.com/js` 这类需要渲染的页面没有 Electron 对手可比，**渲染路径与 Electron 版的差异没有被验证**。
- 泄漏压力只覆盖「阅读页开合」这一条路径，6 分钟、300 轮；没有覆盖采集渲染窗口池的长跑（第三轮已补 1500 任务压测，见下），没有证明 8 小时级稳定。工作集前后段相差约 12% 是首次渲染与 GC 余量，不是线性增长，但这是 300 轮内的观察，不是证明。
- 大库的冷启动 21.5 s 无响应是 `quick_check` 的代价，TS 版（wasm）的同一检查耗时未对比。

**安装包形态的设计要点（仅设计，未实施；改动大，需先与用户定案）**
- 现状：Electron 版用 `electron-builder` 的 NSIS（`oneClick:false`、可选安装目录、`guid` 固定、`provider: github` 发布，`latest.yml` 由 builder 生成）；Go 版的 `updater.go` 已按同一流程（`releases.atom` → `latest.yml` → sha512 校验 → 以 `--updated --force-run` 启动安装包）实现，**缺的是产出同格式安装包与 `latest.yml` 的构建环节**，以及代码签名。
- 方案 A（推荐）：用 NSIS 脚本打包 `out\win-x64`（`GuiZhi.exe` + `dist` + `reading_libraries.json`），沿用现有 `guid` 与安装目录、卸载注册项，让它作为同一产品的「升级」覆盖 Electron 版安装；`latest.yml` 由脚本按 electron-updater 的格式生成（版本、文件名、sha512、size、releaseDate）。需先确认：覆盖安装是否保留 `%APPDATA%\GuiZhi` 用户数据（Go 版 `resolveInitialUserDataPath` 已按「自定义目录 > 历史默认目录」推断，应当沿用）、卸载是否询问删除数据、WebView2 运行时缺失时的引导（Win11 自带）。
- 方案 B：先只发便携 zip，不覆盖 Electron 安装，更新器暂不启用；风险最小，但用户得手工迁移。
- 签名：与现有流程同证书；未签名的 Go 单文件 exe 被 SmartScreen/杀毒误报的概率不低于 Electron 安装包，需实测。
- 切换风险：平台登录会话不互通（见决策 i）、`Local State`/租约在两个外壳并存时的交互未验证；覆盖安装前应提示备份。
- 验收标准：全新机器静默安装 → 启动 → 卸载干净；覆盖安装后数据完整；`latest.yml` 被现有更新器逻辑接受并通过 sha512 校验；这些都需要独立虚拟机，不在本轮范围。

### 收尾验证（2026-11-12 第三轮，隔离沙箱 + 离屏；提交在 `spike/mygo`，已 push）

本轮候选 7 项，用户选了 1–6（登录类站点继续跳过、保持未验证）。真实站点的每一次访问都经用户逐项确认：NGA 只做最小 A/B 与一次图片变体实验，B 站只做一次 `yt-dlp --list-subs`（不带 Cookie）；用户**没有**批准再访问 `bbs.nga.cn` 抓重定向循环的 Set-Cookie。没有改任何代理、DNS、路由设置，没有碰用户的 electron:dev、数据目录与 Vite 端口。

| 项 | 结论 | 证据与复跑 |
|---|---|---|
| 1a. img.nga.cn 附件图 HTTP 567 | **根因：TLS 指纹**。同一张图、同一出口、逐字节相同的请求头：TS 版（Electron 内的 Node）200 `image/jpeg`，Go 版 567 `text/html`（腾讯 EdgeOne 拦截页，Server=TencentEdgeOne）。只换 ClientHello 就恢复：v0 Go 原样、v1 Go 仅开 keep-alive 都是 567；v2 复刻 Electron ClientHello + `Connection: close`、v3 复刻 + keep-alive 都是 200（Server=tencent-cos）。所以与 Connection、请求头、头顺序无关。Go 默认 JA3 `9b7dcdf3…`，Electron/Node 的 JA3 `5355e385…` | 离线字节对比 `verify/wire/wire-compare.mts` 与 `wire_client_test.go`；真实 8 次请求 `verify/nga-ab/imglab`（`-hello` 用 wire-compare 产出的 `clienthello-ts-electron.hex`），日志在 `%TEMP%\guizhi-verify\imglab-real.log` |
| 1b. NGA 楼主分页「重定向次数过多」 | **不是移植缺陷**：同一 A/B 里 TS 版在第 8 页遇到 6 次 302 自重定向（每次约 33 ms，目标 URL 与来源相同），按 5 跳上限报同一错误；Go 版同一次运行 1–19 页全部通过。**触发原因未查明**：怀疑 302 带 Set-Cookie 而我们不回写，但当时追踪没有记录响应侧，现在已能记录（见下），当时用户选择不再访问 `bbs.nga.cn`；后续复测 24 次请求未复现，仍**未取得**（见「后续二」） | A/B 报告 `%TEMP%\guizhi-verify\nga-ab\ab-report.txt`（Go 24 次请求：`bbs.nga.cn` 403×1、200×20，`img.nga.cn` 567×3；TS 18 次：403×1、200×8、302×6，图片 200×3）。复跑：`nga-ab.mts <dry\|real> <目录>` |
| 2. B 站平台字幕 | **未登录取不到字幕轨**：`yt-dlp --list-subs`（2026.10.26，无 Cookie）只列出 `danmaku (xml)`，没有任何字幕语言；同时提示 1080P 60 帧需会员 Cookie。弹幕不是字幕，不会入库。**登录后是否有字幕：未取得**（用户没有提供登录态）。结论：已知限制，不修；产品已按「没取到字幕 → 无转写模型则只存视频信息」降级 | `%TEMP%\guizhi-verify\bili-listsubs.log` |
| 3. 启动补齐全文索引 O(n²)（产品 TS） | 已修并提交 `ac0ed93a`（在 `spike/mygo` 分支上，**需要时 cherry-pick 到主线，用户的主目录未动**）：缺失查询改为非相关 `NOT IN (… IS NOT NULL)`，计划里不再出现 `CORRELATED`。4000 条 1.8 s → 12 ms，8000 条 9.2 s → 22 ms；新增单测断言查询计划与 NULL 残留行为 | `knowledge-db.test.ts`、`wiki-db.test.ts` |
| 6. 采集窗口池长跑 | 1500 个任务、并发 4、用时 1179 s：0 个意外（9 类页面：SPA、重页面 2 万节点、80 张图、弹窗、跨源子框架、Cookie/localStorage/IndexedDB、页内占 64 MB、请求洪泛、挂起），洪泛与挂起各 75 次均按预期失败。Go 协程 3 → 3、堆 6 MB；进程树工作集 5 进程/184 MB 起步，稳定段约 800–960 MB（整段峰值 1148 MB，进程 2–12 个），**未见随任务数单调上升**；空闲 100 s 后浏览器被回收。**压测顺带抓到 2 个真实缺陷，见下** | `verify/run-pool-soak.ps1`（需要 PowerShell 7），结果在 `%TEMP%\guizhi-verify\pool-soak\` |

**本轮修复的缺陷（均先写失败测试、确认红，再修、确认绿；提交未 push）**
1. **img.nga.cn 等带 WAF 的站点对 Go 抓取层返回拦截页**（`de4472f1`）：`safeFetcher` 的 HTTPS 改用 utls 按内置样本握手（`tls_hello_electron.hex`，取自 Electron 45.2.3 / Node 24.22.2 实际发出的 ClientHello），只发 HTTP/1.1，证书链与主机名照常校验（测试里受信、主机名不符、未受信三种都覆盖）；IP 字面量主机不发 SNI，退回标准库握手。Go 的 `Transport` 对「经代理的 HTTPS」不使用自定义 TLS 拨号，所以 HTTPS 目标的代理隧道（HTTP CONNECT 带 Basic 认证、HTTPS 代理、SOCKS5）改为自建，域名仍由代理解析，隧道被拒时报「代理隧道失败：…」。测试：抓取层发出的 ClientHello JA3 与内置样本逐一致、SNI 正确；HTTP CONNECT / SOCKS5 两条代理路径的 CONNECT 目标是域名而非本地解析出的 IP。**新增依赖**：`github.com/refraction-networking/utls v1.8.2`（经用户同意）。这让 Go 版与现有 Electron 版的线上表现一致，不是新增能力；影响面是 `safeFetcher` 的全部 HTTPS 请求（含渲染窗口的页面请求），AI 请求与更新器的传输层不变。
2. **采集窗口池形同虚设**（`e188ec9e`）：每个任务结束时清理步骤 `Target.setAutoAttach(false)` 缺少 CDP 必填的 `waitForDebuggerOnStart`，恒报 Invalid parameters，窗口被判不健康、**每个任务都关窗再新开窗**。修复前 4 次正常渲染替换 4 次窗口，修复后 0 次；压测里被替换的 150 个窗口恰好是 75 次洪泛 + 75 次挂起的预期异常任务。
3. **采集浏览器停止后用户数据目录留在 `%TEMP%\guizhi-capture-*`**（`147aecc5`）：`stop()`/`kill()` 只调一次 `os.RemoveAll` 并吞掉错误，而 WebView2 子进程在宿主退出后还会短暂持有目录。每次空闲回收都会漏一个（实测 %TEMP% 里积了 85 个，7 KB–2 MB 不等，已按「创建超 30 分钟且无进程引用」清掉）。现在按 100 ms 间隔重试至多 5 s，仍失败则 `logAppError` 留痕；`Close` 实测约 260 ms。修复后 20 任务 + 空闲回收的压测残留 0 个。1500 任务那次压测早于此修复，当时收尾残留 1 个目录，就是这个缺陷。

**新增的诊断开关（只读环境变量，不是用户设置；`fetch_trace.go`）**：`GUIZHI_FETCH_TRACE=<文件>` 逐跳写 JSONL（序号、跳数、URL、请求头、状态、Location、Content-Type、响应头名、Set-Cookie **名**、Server、耗时、错误）；URL 查询里名字含 token/key/secret/sig/auth/pass/session/cookie/ticket/code 的值打码，Cookie 只记名不记值。`GUIZHI_FETCH_MIN_INTERVAL_MS=<毫秒>` 让两跳之间至少间隔这么久，取消时返回「已取消」。
**NGA 线上探针（默认跳过）**：`GUIZHI_REAL_NET=1 GUIZHI_REAL_NGA_PROBE=<摘要文件> go test -run TestRealNgaProbe`，摘要页 1、楼主分页最多 19、不拉父楼、不重试、图片只放行 3 次真实尝试。

**本轮仍然未验证 / 未取得**
- ~~修复 1 之后产品里的 Go 抓取层对真实 `img.nga.cn` 是否返回 200~~ **已确认（用户随后放行，只请求 3 张图、间隔 3 秒、不碰 `bbs.nga.cn`）**：产品的 `ngaDefaultDownloadImage` 对同样 3 张图全部成功（160439 / 116422 / 67758 字节，识别为 jpg），修复前同样 3 张全是 567。复跑：`GUIZHI_REAL_NET=1 GUIZHI_REAL_NGA_IMAGES=<url,...> go test -run TestRealNgaImages -v`，日志 `%TEMP%\guizhi-verify\nga-img-verify.log`。
- NGA 重定向循环的触发原因；B 站登录后的字幕；登录类站点（小红书/抖音/LINUX DO/NodeSeek）。
- 系统根证书校验只在 imglab 对真实站点验证过一次（utls 在本机取系统根，`img.nga.cn` 握手成功）；产品里的测试用的是测试根证书池。
- 压测没有覆盖 8 小时级运行，也不含真实站点的渲染；工作集是进程树合计，没有拆分 WebView2 各进程。

**后续（同一轮内用户追加放行，均已提交在 `spike/mygo`）**
- **主线回写 FTS 修复**：在独立 worktree `D:\Project\GuiZhi-fts-fix`（分支 `fix/fts-backfill-not-in`，基于主线 `0c5775db`，已 cherry-pick `ac0ed93a` 为 `43f10d0c`）。主目录没有开发服务在跑（只有 `guizhi-mcp.mjs` 的 MCP 进程）、工作区干净，**原目录与其分支 `fix/design-decisions` 零改动**。该 worktree 内 `pnpm install --frozen-lockfile --offline` 后：`knowledge-db.test.ts` + `wiki-db.test.ts` 74/74 通过，`packages/db` 的 `tsc --noEmit` 通过。**未跑全量 `pnpm test:unit` / 根 typecheck**；合并由用户在切换版本时决定。
- **0 字节数据库（Go 壳已实现，TS 产品未改）**：`initDatabaseFile` 在拿到租约后、完整性检查前调用 `refuseEmptyDatabaseWithBackups`。数据库文件存在且为 0 字节时，若有可恢复备份（`backups/` 里 manual/auto/pre-update/pre-restore 的非 0 字节文件，或库旁 `<db>.integrity-backup-*`）就**拒绝启动**，原因走现有的启动失败通道（写 `logs/error.log`，交互启动弹系统对话框并指出备份目录），说明里给出恢复或「删除这个空文件后重启即新建空库」两条出路；没有备份（或备份也是 0 字节）按空库启动，行为不变；两种情况都不改动该文件。先写失败测试：阻断 3 种备份来源 + 不可用备份仍启动 + 无备份目录仍启动 + 删除空文件后恢复启动，共 6 个子用例；真实进程（`verify/run-corrupt-db.ps1` 新增场景）：无备份退出码 0 / 0 条目，有备份退出码 1 且 `error.log` 含完整说明。**对话框本身没有在真实桌面上观察过**（自动化实例不弹窗）；**TS 产品版（`packages/db/src/init.ts`）没有做同样的改动**，那是主线产品行为变更，需要另行决定。
- **安装包（已能构建，未安装、未在虚拟机验证）**：`.\spikes\build-installer.ps1`（PowerShell 7）用 `electron-builder --prepackaged` 把 `build-shell.ps1` 的产物目录打成 `GuiZhi-Setup-0.28.0-x64.exe`（11.5 MB）并生成 `latest.yml`。配置 `spikes/mygo-shell/electron-builder.mygo.cjs` 直接复用 Electron 版的 appId、固定 guid、可选安装目录、快捷方式、`installer.nsh`、`publish` 与 `releaseInfo`，没有另写 NSIS，因此能以同一产品「升级」覆盖安装 Electron 版（模板先运行旧版卸载程序再装新文件）。另有 `installer-mygo-checks.nsh`（仅该配置下通过 `GUIZHI_MYGO_SHELL` 打开；`apps/desktop/resources/installer.nsh` 只在 `customInit` 末尾加了受 `!ifdef` 保护的 3 行钩子，Electron 版打包不受影响）：静默安装（自动更新）全部跳过；交互安装时检测 WebView2 运行时（`EdgeUpdate\Clients\{F3017226-…}` 的 `pv`，机器级含 WOW6432Node 或用户级），缺失则提示并可打开微软下载页、然后中止安装；检测到已登记的安装位置（含旧 Electron 版）则提示「资料在 `%APPDATA%\GuiZhi`，安装与卸载都不会删除，建议先在应用内备份」，可取消。不签名（`Get-AuthenticodeSignature` = `NotSigned`）。已验证：产物 sha512 与 `latest.yml` 一致；Go 更新器核心（`TestUpdaterAcceptsRealInstallerArtifacts`，本地 httptest，不执行安装包）接受这份 `latest.yml` 并通过下载与校验。**没有验证的**：安装器本身的运行（全新机器静默安装、覆盖安装 Electron 版数据是否完整、卸载是否干净、两个对话框的显示与文案、WebView2 缺失分支、SmartScreen 与杀毒反应）——**我没有在本机运行安装包**，因为它与真实安装共用同一个 guid，会改写本机已安装归知的卸载注册项。首次构建会下载 NSIS 工具到 `%LOCALAPPDATA%\electron-builder\Cache`。`latest.yml` 没有 `size` 字段，更新器按 sha512 校验，不依赖它。
- **测试稳定性**：`TestFetchTraceRecordsHopsAndMasksSecrets` 把秘密值 `77` 当子串断言日志里不出现，而日志时间戳偶然含 `77`，约 1/5 概率误报；已换成 `UIDVAL`，连跑 40 次 0 失败。

**后续二（用户再次放行：FTS 全量、TS 0 字节库、PR、虚拟机清单、NGA 重定向复测）**
- **FTS 修复全量验证（已取得）**：`D:\Project\GuiZhi-fts-fix` 上 `pnpm test:unit` 311 个文件 / 2696 个用例全部通过（194 s），根 `pnpm typecheck` 全部包通过。此前「未跑全量」一条作废。日志 `%TEMP%\guizhi-verify\fts-full-unit.log`、`fts-full-tc.log`。
- **0 字节库的 TS 产品版（已做在独立 worktree，未合并）**：`D:\Project\GuiZhi-zero-db`，分支 `fix/empty-db-guard`，基于 `0c5775db`，提交 `4442e023`，3 个文件 +122/−1；原目录与 `fix/design-decisions` 零改动。逻辑与 Go 壳一致：`packages/db/src/init.ts` 的 `initDatabase` 在完整性检查前调用 `refuseEmptyDatabaseWithBackups`，数据库是 0 字节且存在可恢复备份时抛出可操作的错误（给出备份路径、恢复与「删除空文件后重启」两条出路），不改文件；无可用备份仍按空库启动。备份来源由 `apps/desktop/src/main/database/index.ts` 通过钩子 `findRecoverableBackups` 注入（`listBackups()` 的路径），库旁 `<db>.integrity-backup-*` 在 db 包内直接枚举，0 字节备份不算。先写失败测试（红：4 失败），修后 `startup-integrity.test.ts` 12/12；备份相关 4 个文件 53/53；`packages/db` 与 `apps/desktop` 的 `tsc --noEmit`、`pnpm lint`、文件行数门禁通过。**没有跑全量 `pnpm test:unit`；错误在真实桌面上的对话框呈现没有观察过**（沿用现有启动失败通道，未改 UI）。
- **NGA 重定向循环复测（用户放行：`bbs.nga.cn` 话题 37194262 一次，24 次请求，间隔 1 s；已发生）**：**没有复现**。24 次请求里 403×1（首次无 Cookie，响应下发 `lastvisit`、`lastpath`、`ngaPassportUid`）→ 带上 `guestJs` 等重试 200×20，`img.nga.cn` 200×3，**3xx 为 0**，楼主分页 1–19 全部通过（378 条楼主回复）。因此 302 自重定向的触发原因**仍然未取得**：此前 TS 在第 8 页遇到的循环是临时性的，与会话 Cookie 是否回写的关系没有证据（本次没有 302 可供检查 Set-Cookie）。证据 `%TEMP%\guizhi-verify\nga-redirect\trace.jsonl`。后续若再出现循环，用同一探针即可捕获该跳的 Set-Cookie 名。
- **安装包虚拟机验收（清单与取证脚本，未执行）**：`spikes/mygo-shell/verify/installer-vm-checklist.md`（场景 A–G：全新安装、覆盖 Electron 版、静默覆盖、卸载、缺 WebView2、SmartScreen、更新后启动）与 `installer-vm-probe.ps1`（Snapshot / Check，只读；检测不到虚拟机直接拒绝，保护与真实安装共用的 guid）。Check 的断言用合成快照自测过（全通过与故意违反各一次，退出码 0/1），物理机拒绝路径实测生效。**所有场景都没有在虚拟机里跑过**。
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
- 已验证（见「采集进程的真实页面与并发压测」）：多个并发 target 的拦截隔离、崩溃恢复。还没验证：采集进程池的复用策略、真实互联网站点与反爬。CDP 新建的页面会漏掉 `window.open` 弹窗的首个请求，应改用带“拒绝新窗口”处理器的 MyGo 窗口池。
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
- 网络代理设置已接入（2026-11-12，见进度表「出站网络层」）：手动代理生效，其余直连；真实代理环境未验证。

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
- **有效的办法（已验证）**：弹窗要由宿主层拒绝。MyGo 的 `Page.SetWindowOpenHandler(func(WindowOpenRequest) *WindowOptions { return nil })` 会拒绝一切 `window.open` 与 `target=_blank`（含 `a.click()`、`form.submit()`、`<base target>`、`iframe.contentWindow.open`，共 5 条路径，全部不再弹窗、内网零命中，宿主页加载正常）。**但只对 MyGo 窗口里的页面生效**：用 `Target.createTarget` 新建的 CDP 页面（本次压测用的方式）不经过这个处理器，Chromium 自己开窗。所以产品里的采集页应当是采集进程内预先创建的隐藏 MyGo 窗口（池），而不是 `createTarget`；窗口复用时必须在任务结束时 `Fetch.disable` 并导航回空白页，否则残留的拦截没人处理，下一次导航会挂住（实测）。
  - **不设处理器是危险默认值**：MyGo 在没有处理器时，会把 `window.open` 的 http(s) 地址交给系统默认浏览器（`Shell.OpenExternal`），采集进程里必须显式设置“拒绝”。出于这个原因，没有做“MyGo 窗口 + 无处理器”的正向对照；5 条路径的正向对照来自 `createTarget` 页面（5/5 弹窗并命中内网）。
  - 池化方案已按此实现并复测，见下一小节。

### 采集页改为 MyGo 窗口池后的复测（`-pool 4 -poolmax 25`）

采集进程预建 4 个隐藏的 MyGo 窗口（父进程发 `NEW` 指令让子进程新建），每个窗口都显式设置了“拒绝新窗口”的处理器；任务复用窗口，用满 25 个任务、或出现卡死/崩溃/加载超时就整个关闭并重建。全部场景（串行、并发 3/8、规则隔离、10 项故障、安全探针、杀进程恢复、300 任务内存）**失败 0 项**，且与 `createTarget` 方式相比：

| 项 | `createTarget` | 窗口池（4 个窗口，25 次换新） |
|---|---|---|
| 59 页串行 | 3.5 页/秒 | 8.8 页/秒 |
| 并发 3（177 个任务） | 4.5 页/秒 | 10.8 页/秒 |
| 并发 8（177 个任务） | 4.3 页/秒 | 12.2 页/秒 |
| 弹窗（5 条路径） | 5/5 弹出并命中内网 | 0 弹出，内网零命中 |
| 300 个任务后工作集 | 约 800–850 MB（+284…+326 MB） | 约 1450 MB（起点 663 MB，+787 MB） |
| 杀进程后重启并恢复 | 0.45 秒 | 0.74 秒（含重建 4 个窗口） |

实现窗口池时踩到、已修的三个坑（都会让生产实现出错）：

1. **并发新建窗口必须串行**：靠“新建前后的 target 差集”认新窗口，两个并发新建会拿到同一个 target，两个任务共用一个窗口，结果互相破坏。
2. **load 事件要按导航的 `loaderId` 对应**：复用的窗口可能带着上一个文档的 load 事件，只看“收到 load”会在导航提交前就取值，得到空文档，表现为大量 `Uncaught` 脚本异常（并发 3 时 17/177 才成功）。
3. **复用前先 `Fetch.disable`、接受 `beforeunload` 弹窗**：否则残留的拦截没人处理，下一次导航会挂住；`beforeunload` 若被取消，导航回空白页会被它拦住 10 秒。

窗口池的内存更高：每个窗口的渲染进程会随页面累积，池越大越明显（8 个窗口且不换新时 300 个任务后 +2.1 GB）。所以池大小与并发数一致即可（实测并发 3 到 8 的吞吐差别不大），并保留“每 N 个任务换新”。N 取多少、真实站点下内存曲线，还需要用更长的任务序列测。
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

另有约 190 个频道未迁。设置里的「网络代理生效」已迁（出站网络层）；「可执行文件路径白名单」仍只接受空串（文件选择器随托管工具切片接入）。

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
2. 独立采集进程的并发压测已在本机语料上做完（见「采集进程的真实页面与并发压测」），真实互联网站点仍待验证，弹窗缺口的有效办法已找到（MyGo 窗口 + 拒绝新窗口的处理器），采集页改为窗口池后已重做压测（见「采集页改为 MyGo 窗口池后的复测」）；AI 流式请求（见「AI 流式请求切片」）与加密备份格式、换库与回滚（见「备份恢复与加密仓库切片」）已验证；备份恢复外围的编排（`pre-restore`、停后台任务、重启）还没验证。非 UTF-8 站点的处理策略要定：一律走渲染路径，或接受 `x/text` 对畸形字节的差异。
3. 如果继续迁：按「纯读库 → 写库 → 带副作用」的顺序，沿用差分对拍，每迁一组频道就补一组基准。
