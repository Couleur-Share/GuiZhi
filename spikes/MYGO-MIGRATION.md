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
2. **网络层拦截缺口。** MyGo 里没找到按请求改写头部或读取状态码的接口，只有 `PerformanceObserver` 能拿到 URL、类型和耗时。采集链路里依赖这类拦截的部分需要重新评估。
3. **新旧版本不能同时打开同一个数据库。** 旧版 wasm 驱动用 `mkdir <db>.lock` 目录锁，与原生 SQLite 的字节范围锁不兼容；MCP 服务也用 wasm 驱动。灰度期要么互斥，要么统一驱动。
4. **机密与加密。** `ai-models.json` 里 `apiKey` 以明文保存在数据目录（与 TS 一致，迁移时没改）；`safeStorage`（DPAPI）保存的凭证、`electron-updater` 都需要单独的迁移方案。
5. **`settings:set` 的半写入特性。** 与 TS 一致：先写 AI 配置文件，再校验并写库，同一批里 AI 合法而 `flareSolverr` 非法时会出现「AI 已落盘、其余未写入」。正式迁移时建议改成先校验、后写入。
6. **窗口选项的顺序依赖。** MyGo 的 `ThrottlingOff` 必须给第一个窗口或 web view，后建的窗口设置无效。

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

## 需要定的服务边界

按 `docs/product-scope.md`，冻结中的功能（手机收集、Harmony、FlareSolverr、研究、发现、插图等）只修安全、崩溃与数据问题。建议先决定它们的归宿，再评估总体工作量：

- **留在旧版 Electron**：新版只做核心闭环（采集、整理、检索、问答）。旧版继续发安全修复。代价是长期维护两套，且需要解决上面第 3 条的数据库互斥。
- **全部迁到 Go**：一次性成本最大，但只剩一套代码。

## 建议的下一步

1. 先定上面的服务边界，再决定是否继续迁移。
2. 解决备份（第 1 条）和网络拦截（第 2 条）两个已知硬缺口的方案验证，它们决定采集链路和备份能不能迁。
3. 如果继续迁：按「纯读库 → 写库 → 带副作用」的顺序，沿用差分对拍，每迁一组频道就补一组基准。
