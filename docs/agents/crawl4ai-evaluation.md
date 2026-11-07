# 随包 Crawl4AI 去留评估与移除记录

2026-11-07 先做只读对照评估，随后按产品决定实施：补齐规则、两套样本回归达标后，从安装包移除随包 Crawl4AI（分支 `feat/readability-extractor`），见 [产品边界](../product-scope.md) 与 [网页采集](../web-capture.md)。逐页明细、脚本与缓存留在评估时的临时目录，不随仓库保存；本文保留结论、依据、门槛与实施结果，实施结果见文末。

## 结论

建议**有条件移除**：保留 Electron 渲染器与静态分流，正文提取改回 Node 侧 Readability + Turndown；先补齐下文规则并通过留出样本回归，再从安装包删除 Python 运行时。总体置信度中。

## 依据

样本 59 页：37 个技术文档固定样本，加 22 个博客、新闻、维基页。两条路径吃完全相同的输入（静态页用原始 HTML，动态页用渲染后 HTML），只比较提取器本身。

- **结构**（置信度高）：Crawl4AI 明显好于旧路径。代码围栏 861 对 277，表格 17 页对 0 页，旧路径在 46 页留下 3,201 个相对链接。原因在旧路径的 Turndown 配置，不在正文识别；补齐规则的原型（约 150 行）达到 835、15 页、2 个，差距基本消除。
- **正文选择**（置信度中）：互有胜负。人工判定 Crawl4AI 明显更好 7 页、略好 6 页，Readability 明显更好 6 页、略好 8 页，相当 32 页。
  - 文档页 Crawl4AI 更稳：Readability 在 MDN Fetch、Ars Technica、Martin Fowler 上截断或选错，并会丢 MDN 浏览器兼容表。
  - 新闻博客页 Readability 更干净：Crawl4AI 在 IT之家、中国新闻网回落整页（一半以上是导航与新闻列表），在 Guardian、GitHub Blog 夹带推荐与订阅推广，在爱范儿整篇丢失（站点把 `id="article-content"` 给了文末标签块）。
- **动态页**：收益来自 Electron 渲染器，不来自 Crawl4AI。开源中国渲染后 Readability 同样取到正文。
- **成本**：运行包 193.7 MiB、8,099 个文件（`config/package-size-budget.json`），压缩后估算约 41.6 MiB，约占 125.9 MiB 安装包的三分之一（估算，未重建安装包）。空闲 60 秒后首次采集多等约 6.5–12 秒（完整性校验加 Python 导入）；worker 常驻约 105–148 MiB。单页提取两条路径同量级，中位约 0.1 秒。

## 移除前必须完成

| 规则 | 内容 | 原型效果 |
| --- | --- | --- |
| R1 | 所有 `<pre>` 输出围栏代码块，不转义，语言取自 class | 代码块 277 → 835（Crawl4AI 861） |
| R2 | GFM 管道表格；仓库没有插件，自写约 25 行或新增依赖 | 0 → 15 页（Crawl4AI 17 页） |
| R3 | 用最终网址把 `href`/`src` 解析为绝对地址；linkedom 文档没有基址 | 3,201 → 2 |
| R4 | 保留 h1（低优先级，标题另存于条目） | 部分恢复 |
| R5 | Readability 取到的文字不足清洗后语义区域的 60%（区域至少 500 字）时回退到该区域；区域判定只按 class 匹配并设长度下限 | 修复 MDN Fetch 两页与 Ars；阈值需留出样本复核 |
| R6 | 提取放到主进程之外（worker_threads 或 utilityProcess），沿用 45 秒超时与 16 MiB 上限 | — |
| R7 | 保持 `WebExtracted` 契约与错误分类（login / restricted / network / captcha / empty、200,000 字符截断） | — |

建议补但不阻塞：补回 Sphinx 提示框与 MDN 兼容表，预清理少量模板短语。已知仍会退化：Martin Fowler 导语与致谢、asyncio 模块索引等少数部分截断页。

改动落点：新建共享 Node 提取模块（例如 `main/services/web-capture/web-node-extractor.ts`），由 `web-electron-capture.ts` 与 `import/connectors.ts` 的降级路径共用。

## 门禁与验收

1. R1–R7 完成后，在这 59 页上回归，并新增至少 40 页不参与阈值校准的留出样本：MDN、Sphinx / Docusaurus / VitePress / mdBook、无语义标记的中文新闻、英文新闻、维基、GBK 编码站点、超长页、CodeMirror 或 shadow DOM 页面。
2. 预期片段命中不低于 Crawl4AI；代码块不少于其 95%、表格不少于其 90%、相对链接为 0；没有正文缺失超过 10% 的页面（已知类别除外）；每次触发回退都逐页人工复核。
3. 换提取器会让同一网页的 `contentHash` 整体变化。先定策略（按引擎版本重建基线或提示用户），避免重新采集时批量出现「待采用版本」。（已定，见文末「内容哈希与重新采集」。）

## 移除涉及的其他改动

- 网页组件状态与修复界面（`web-runtime.ts`、`web-runtime-integrity.ts`，设置 → 采集与转写 → 内置网页采集）、文档站与网页研究的「组件可用」门槛、`engineVersion` 字符串 `crawl4ai/0.9.3-*`。
- 打包与 CI：`scripts/build-crawl4ai.py`、`apps/desktop/scripts/crawl4ai-package.cjs`、`config/crawl4ai/*`、体积预算的 runtime 项、NOTICE 与第三方许可、Crawl4AI 文档（已改写为 [网页采集](../web-capture.md)）。

## 未覆盖的风险

- 渲染输入来自 Playwright 的 Chromium，不是产品内的 Electron；网络出口、跨源子框架与每页 200 请求的预算都不同。
- 固定样本都是技术文档，补充样本同一天抓取；R5 的阈值在同一批数据上校准，有过拟合风险。
- 未测非 UTF-8、需登录或付费、超过 200k 字符、CodeMirror / shadow DOM、RTL 页面。
- 现有 40 网址验收只比对关键词，区分度很低（各路径 35–36/37 命中）；无论去留，以后都应改用结构与覆盖指标加人工抽检。

## 实施结果（2026-11-07，分支 feat/readability-extractor）

### 代码

- 正文提取：`web-extract-article.ts`（选正文、语义区域回退、结果字段与错误分类）与 `web-extract-markdown.ts`（Turndown 规则）。网页采集与兼容采集（`connectors.ts` 降级路径）共用 `web-node-extractor.ts` 的同一个提取线程；`engineVersion` 改为 `readability/1-<路由>`。
- 线程：`worker_threads`，45 秒超时、16 MiB 输入/输出上限、1 GiB 堆上限；超时、取消、协议错误与内存超限都回收线程，空闲 60 秒结束。worker 不能直接加载 `app.asar` 内的文件，脚本单独打包为只引用 Node 内置模块的单文件（`out/web-extract-worker`），由主进程读出后以内联脚本启动。线程内替换 net/tls/http(s)/http2/dgram/dns/fetch/WebSocket，单测确认 9 种联网方式全部被拦下，解析带外部资源的页面不产生连接。
- 冷启动：Electron 45（Node 模式）读脚本到首页结果约 110 ms；在真实主进程里按产品方式启动并提取 Ars 页约 150 ms。原 Python worker 冷启动 6.5–12 s。
- 单页提取（不含网络）：59 页中位 62 ms、p90 387 ms、最大 0.75 s；留出样本中位 115 ms、p90 709 ms、最大 1.7 s（WHATWG 解析章节，0.86 MB）。

### 规则与调整轮次

R1–R7 按上表实现，另有四轮调整（第 4 轮经用户批准超出 3 轮上限）：

1. 59 页首轮回归后：内联 SVG 的文字不删（Martin Fowler 的图表文字是正文）；Readability `linkDensityModifier` 放宽 0.1（收回「标题链接 + 说明」卡片、Sphinx「See also」、React 结尾，新闻页推荐区不受影响）；R5 的比例只比表格之外的文字，兼容表这类大表不左右判断；区域回退时把单元格里的 `<button>` 拆成文字（MDN 兼容表的数据在按钮里）。
2. 留出样本首轮回归后（这几条来自留出样本的失败，留出样本因此不再完全独立）：展开标签页面板与 `<details>` 里的隐藏内容；渲染快照里逐行的 CodeMirror DOM 拼回代码块；标题区（`main`/`article` 内、主标题所在的小容器）被 Readability 当页框删掉时补回 h1 与导语，徽标、折叠框里的说明不算导语；先删脚本与样式再统计文字量。另试过「不计链接文字」的比例（为排除 Svelte 误回退），会让 MDN 中文指南页逼近阈值，未采用。
3. 用户确认表格口径后（最后一轮）：R5 的比例改为只计表格之外 `<p>` 里的文字。Svelte 教程 `main` 里没标成导航的整份章节目录不再撑大区域，误回退消失；两套样本里正常页的比例最低 0.94（59 页）与 0.85（留出），真正截断的页面不高于 0.52。
4. 移除后的集成测试暴露（用户批准）：极短页面里 Readability 所有尝试都不足 100 字，会退回整页 body 并去掉 class，`main` 之外的 `<nav>` 和 `class="sidebar"` 的文字进了正文。此时改用按语义区域规则清理页框后的整页，并先按 Readability 的可见性规则删掉内联样式隐藏的元素（第一版漏了这一步，全量单测「其他站点不将同名隐藏容器当作公众号正文」随即失败，补上后通过）。两套样本 109 个输出文件逐字不变，新规则在 106 页上一次也未触发。

### 回归门槛

C＝随包 Crawl4AI，N＝新提取器，二者吃同一输入（按产品分流取静态或渲染 HTML）。留出样本 59 个网址：首次采集脚本以 tsx 加载产品模块，被插入的 `__name` 辅助函数在页面里报错，13 个网址只存下静态 HTML；其中 9 个应走渲染的页面无法评估，按「每个网址只抓一次」不再重抓，另补同类网址。最终可评估 47 页，另有 3 页 HTTP 429 只用于核对错误分类（C、N 都报 restricted）。

下表为第 3 轮之后的最终结果。

| 门槛 | 59 页 | 留出 47 页 |
| --- | --- | --- |
| 预期片段命中不低于 C | N 36/37，C 36/37 ✓ | N 44/47，C 42/47 ✓ |
| 代码块不少于 C 的 95% | 843/861＝97.9% ✓ | 902/869＝103.8% ✓ |
| 表格不少于 C 的 90%（按内容表，口径见下） | 原始 36/44＝81.8%；内容表 36/36＝100% ✓ | 原始 52/65＝80.0%；内容表 N 52、C 49 ✓ |
| 相对链接为 0 | 0 ✓ | 0 ✓ |
| 正文缺失 >10% 的页面 | 8 页，逐页核对均非 N 漏正文，见下 | 9 页，同上 |
| R5 回退逐页复核 | 3 页（MDN Fetch 中英、Ars），正文完整 | 2 页（codemirror.net、MDN IntersectionObserver），正文完整 |

- 预期片段：留出样本每页两句（正文开头与结尾附近），只依据输入 HTML 的 DOM 文本挑选，挑选时未看提取输出；命中＝两句都在。C 漏 NPR（只取到 247 字的播放器文字）与 Redux（报 empty），三个超长页两者都因 20 万字截断漏结尾句。
- 表格见下节「表格门槛的口径」。
- 相对链接按修正口径统计：去掉代码块与行内代码里的字面量（如 ``` `![image]()` ```）。原始口径各剩 1 处，均在行内代码里，C 同样有。
- 正文缺失按句子覆盖统计，归因到模板区的不计。超过 10% 的页面逐页核对：
  - 已知类别：asyncio 模块索引（17.5%）；MDN 兼容表：去掉兼容表行后 MDN 各页剩余缺失 0.5%–5.8%，多为「在 GitHub 上查看」之类的页脚。
  - C 自身的问题：爱范儿（C 只剩 3 个标签链接）、IT 之家、中国新闻网、新华网、太平洋电脑网（C 带入整页导航与新闻列表）、NPR（C 几乎没有正文）、rust Iterator（C 多出试运行链接里的编码代码）。
  - 模板：Guardian 的「最多阅读」与订阅推广；Al Jazeera 的题图说明、署名与分享按钮。
  - 中文维基：导航框、「取自」页脚，以及红链后的「（英语：…）」注释（Readability 按 class 含 comment 删掉了这些注释，正文句子本身都在）。
- 误回退（第 3 轮已修）：Svelte 教程的 `main` 里有没标成导航的整份章节目录，区域文字被撑大，比例 0.24，回退结果开头多出约 200 行目录。第 3 轮后比例 0.98，走 Readability。
- N 相对旧 Readability 路径新增的内容逐页看过，以收回的正文为主；仍带入的模板：MDN 页脚、Arch Wiki 的维护提示、BBC 的「Skip … and continue reading」。

### 表格门槛的口径

门槛按**内容表**判定（用户抽查中文维基 HTTP 与 mkdocs-material 代码块参考页后确认）。两种计数都保留：

- **原始计数**：Markdown 输出里的 GFM 分隔行（`| --- |`）个数，一行分隔线算一张表。机械、可复算，但 C 会把版面表、维基导航框和带行号的代码块也输出成管道表，数量偏高。
- **内容表计数**：原始计数减去逐张核对后不承载正文数据的表。剔除四类：页面排版用的版面表；维基导航框、规范控制框和维护提示框；拆出来只有一两行或空表头的碎片；带行号代码块渲染成的空表。信息框、语法对照表等承载数据的表计入，即使两边行数不同。
- **逐页依据**：只有下列页面两边的原始计数不同，表中逐张核对；其余页面两边张数相同，未逐张比对。

| 页面 | C | N | 差异说明 |
| --- | --- | --- | --- |
| 59 页 · 中国新闻网 | 1 | 0 | C：空表头、0 数据行的版面表 |
| 59 页 · 中文维基 SQLite | 6 | 2 | C 多 4 张：「数据库管理系统」导航框与信息框、导航框拆出的一两行碎片；信息框与「扩展名」表两边都有，N 的信息框是完整的 12 行，C 只有 4 行 |
| 59 页 · 英文维基 SQLite | 3 | 2 | C 多 1 张：页尾导航框的空表头碎片 |
| 59 页 · Paul Graham | 2 | 0 | C：页面排版用的两张单行表 |
| 留出 · mkdocs-material 代码块参考 | 3 | 0 | C：带行号代码块渲染成的空表（行号锚点噪声）；N 把这些示例保留为完整代码块 |
| 留出 · 英文维基 Markdown | 7 | 3 | C 多 4 张：「Find sources」维护提示框、两个导航框与规范控制框；信息框、CommonMark 信息框与语法对照表两边都有，N 分别为 12、11、4 行，C 为 7、7、1 行 |
| 留出 · 中文维基 HTTP | 9 | 1 | C 多 8 张：「语义网」「统一资源标志符方案」「Web 接口」等导航框及空表头碎片；唯一的内容表「请求方法」（9 行）两边都有 |
| 留出 · Python 内置类型 | 11 | 13 | N 多 2 张内容表：bytes 格式化一节的 Flag 与 Conversion 表 |
| 留出 · Rust Iterator | 0 | 1 | N 多 1 张内容表：fold 示例的逐步计算表 |
| 留出 · 太平洋电脑网 | 1 | 0 | C：把一段正文包成表头、0 数据行的版面表；这段文字 N 作为段落保留 |

合计：59 页 C 44 − 8 ＝ 36 张内容表，N 36 张；留出样本 C 65 − 16 ＝ 49 张，N 52 张（N 多出的 3 张都是内容表）。

### 第 3 轮前后对比

同一输入、同一指标脚本，只换提取器源码。

| 指标 | 59 页：第 2 轮 → 第 3 轮 | 留出 47 页：第 2 轮 → 第 3 轮 |
| --- | --- | --- |
| 预期片段命中（N） | 36/37 → 36/37 | 44/47 → 44/47 |
| 代码块（N / C） | 843 / 861 → 843 / 861 | 903 / 869 → 902 / 869 |
| 表格原始计数（N / C） | 36 / 44 → 36 / 44 | 52 / 65 → 52 / 65 |
| 相对链接 | 0 → 0 | 0 → 0 |
| 正文句子单元合计（N） | 188,982 → 188,982 | 136,373 → 136,021 |
| 语义区域回退 | 3 页 → 同样 3 页 | 3 页 → 2 页（Svelte 不再回退，无新增） |
| 正文缺失 >10% 的页面 | 8 页 → 同一批 8 页 | 9 页 → 同一批 9 页 |

59 页的输出逐字不变。留出样本只有 Svelte 一页变化：句子单元 466 → 114（C 476），去掉约 200 行章节目录；代码块 5 → 4，少的是右侧编辑器面板里的初始代码 `<h1>Hello world!</h1>`，教程正文的 4 个示例都在；相对 C 的非模板缺失 0% → 4.1%（约 100 字）。

### 已知差异与未覆盖

- MDN 兼容表：走 Readability 的页面里按钮被删，表格只剩结构；走区域回退的页面带数据。属于已知类别。
- 标签页与 `<details>` 的全部内容都会进正文（C 也如此），各标签之间没有标签名。
- shadow DOM 里的 CodeMirror 只能拿到已渲染的行，长代码可能不全（C 同样）；轻 DOM 里的编辑器仍由渲染快照从完整文档还原。
- 留出样本的预期片段由人工挑选，未做双人复核；渲染输入来自 Playwright Chromium 156 级别的近似，不是产品窗口本身。

### 内容哈希与重新采集

- `initialize()`：新条目首次入库时调用（导入、文档站新页面），不受影响。
- `attach()`：只在公众号补采时调用（`wechat-html/1`，不经过本提取器）。
- `check()`：只在文档站批次（`purpose: "documents"`）遇到已存在条目、且批次的「重复处理」选为「更新」时调用。这是一次操作重采整批页面：换提取器后每页 Markdown 都不同，未编辑、没有网页快照的条目会被自动改写为新正文（旧正文存为本地版本，向量与 Wiki 摄取作废，摘要标为过期）；编辑过的条目与没有基线的旧条目出现「待采用版本」。没有定时或启动时自动重采：中断的批次启动时只标为 interrupted，继续与重试都要用户操作。
- 单条「更新来源（保留编辑正文）」走 `saveSourceRevision`，生成待采用修订，不改正文。
- 调研结论：存在由用户一次操作触发的批量重采，需要产品决定。
- 决定：本次与基线版本的 `engineVersion` 不同时，只保存新版本并标为待采用，不改正文；引擎相同时行为不变；采用只能逐条进行，不新增批量采用入口。
- 实施（`packages/db/src/web-source.ts` 的 `check()`）：比较对象是当前正文所依据的基线版本，找不到时退回上一个远端版本；只比较 `engineVersion` 的提取器部分，去掉 `-static`、`-electron`、`-verified-electron` 等路由后缀（用户后续决定，最初按完整字符串比较）。正文、基线、向量与 Wiki 摄取都不动。
- 单测（`tests/unit/web-capture/web-storage.test.ts`）：引擎相同时，未编辑且无快照的条目返回 updated 并改写正文，编辑过的返回 pending-version、正文不变；引擎变化时，未编辑条目也返回 pending-version，第二次同引擎重采仍是 pending-version，正文、基线版本、`summary_stale`、`knowledge_embeddings` 与 `wiki_ingestions` 都不变，最新远端版本是新引擎结果；逐条采用后，同引擎重采恢复自动更新；同一提取器只换路由（静态、渲染、人工验证）照常自动更新，提取器版本变化仍只待采用。

### 移除清单

- 界面与 IPC：设置 → 采集与转写 → 内置网页采集（`WebCaptureSettings.tsx`）、关于页的 Crawl4AI 署名、文档站与网页研究的组件不可用提示；`web:status`、`web:repair` 频道，`WebRuntimeStatus` 类型，`unavailable` / `damaged` 错误码，13 个语言包键；导入阶段名「准备网页组件」改为「准备网页采集」。
- 主进程：`web-runtime.ts`、`web-runtime-integrity.ts`、`web-cache.ts`、`web-html-extractor.ts`；导入、文档站、网页研究里的组件状态检查与回退原因。
- 打包与 CI：`resources/crawl4ai-worker/*.py`、`apps/desktop/scripts/crawl4ai-package.cjs`、`scripts/build-crawl4ai.py`、`scripts/check-crawl4ai-package.py`、`config/crawl4ai/` 下的锁文件与许可清单（公网样本移到 `config/web-capture/`）、Windows Sandbox 验收包 `scripts/crawl4ai-acceptance/`、`scripts/crawl4ai-ui-states.mjs`、`scripts/lib/web-runtime.mjs`；electron-builder 的运行包资源、架构校验、分发清单与 macOS 自定义签名；质量工作流的三组 Python 测试，发版工作流的 Python 安装、运行包构建与体积检查；体积预算的 runtime 项；NOTICE 中的 Crawl4AI 署名。
- 随 Crawl4AI 失去意义而删除的测试：单测 `extractor-lifecycle`、`runtime-integrity`、`runtime-status`、`web-cache`；脚本测试 `crawl4ai-package.test.mjs`；Python 测试 `test_article_cleanup.py`、`test_article_cleanup_comments.py`、`test_crawl4ai_acceptance_kit.py`、`test_crawl4ai_build_encoding.py`、`test_crawl4ai_prune.py`、`test_extract_only_protocol.py`；`package-size.test.mjs` 中统计与判定运行包的两项；50 页批次集成测试里检查 Crawl4AI 缓存目录清空的断言。
- 保留：研究记录里已持久化的来源标识 `crawl4ai-public-web`（改名会让旧记录对不上）；Git、ESLint 与行数门禁对 `apps/desktop/resources/crawl4ai` 的忽略（旧检出里可能残留约 194 MiB 的运行包，可手动删除）。

### 体积（Windows x64，Electron 45.2.3，同一版本号 0.26.0）

| 项目 | 移除前 | 移除后 | 变化 |
| --- | --- | --- | --- |
| 安装包 `GuiZhi-Setup-0.26.0-x64.exe` | 159,948,002 字节（152.5 MiB） | 118,119,137 字节（112.6 MiB） | −41,828,865 字节（−26.2%） |
| 安装后（`win-unpacked`） | 626,791,382 字节 / 8,209 个文件 | 424,024,807 字节 / 105 个文件 | −202,766,575 字节（−193.4 MiB） / −8,104 个文件 |
| 其中 `resources` | 252,755,372 字节 | 49,988,797 字节 | 运行包 203,122,983 字节与 worker 23,532 字节移除；提取线程约 0.47 MB 打进 `app.asar` |

移除前数据取自 Electron 45 升级时的本地打包；移除后为合并 `chore/review-followups` 后的 `pnpm electron:build:win` x64 产物（合并前为 118,117,982 字节，差 1,155 字节）。安装包预算按新实测改为 128,000,000 字节（约 +8%）。

### 合并后的最终验证（2026-11-07，合并 `chore/review-followups` 之后）

全部在本分支工作副本与系统临时目录中执行，属于源码与隔离测试，没有发布或安装到系统。

| 检查 | 结果 |
| --- | --- |
| `pnpm typecheck`、`pnpm lint`（含行数门禁与 `lint:i18n`，2117 个键） | 通过 |
| `pnpm test:unit` | 通过：脚本测试 20 项，Vitest 302 个文件 / 2,632 项 |
| `pnpm build:isolated`、`pnpm test:e2e` | 通过；e2e 6/6 |
| `web-capture-runtime`（`GUIZHI_TEST_WEB_CAPTURE=1`） | 默认与 `GUIZHI_WEB_SUBFRAMES=allow` 各一次通过（极短页面由第 4 轮修复，子框架断言由合入的按场景设置策略修复） |
| 50 页批次（`GUIZHI_TEST_50_PAGE_BATCH=1`，页面都是极短页，会走第 4 轮的整页清理） | 通过：50 页 4.5 秒，空闲 60.8 秒后提取线程退出 |
| 出口、沉降、请求身份、惰性图片探测 | 全部通过；出口另跑 `allow`、请求身份另跑 `none` 基线，均符合预期；沉降 `timer700` 为已知限制 |
| 默认 `pnpm shot` 与 `reading-v3-acceptance`、`reading-graphics-shots`（`GUIZHI_GRAPHICS_VALIDATION=1`）、`article-reader-redesign-shots`、`article-ask-frames-shots` | 全部通过，共 75 张截图逐张看过，证据文件 `success: true`、无错误、无真实模型或搜索调用 |
| `heavy-user-index-shots --gpu` 两次 | 通过；输入延迟 P95 5.3、5.4 ms（阈值 100 ms），渲染路径 ANGLE D3D11（Intel Arc） |
| `pnpm electron:build:win` 与体积门禁 | 通过：x64 安装包 112.6 MiB，上限 122.1 MiB |
| `pnpm shot --executable` | 采集界面步骤与请求身份探测（静态、渲染、UA 门禁三次真实采集，提取线程从 `app.asar` 内启动）通过 |
| 打包产物首次启动语言（全新 userData） | 系统中文：引导弹窗为中文，`guizhi-settings` 与主进程保存 `zh`；`--lang=en-US` 模拟英文系统：英文界面，保存 `en` |
| Linux 预检（德国共享构建器） | 通过并经 `check:linux:verify` 核验，源码指纹 `7a18ef678f8729422801371ff2898155556d27fbfac30a2d31c5a6691b13d0f9`，Node 24.15.0、107 项中转测试 |

截图中留意到但与本次改动无关、也未判定为缺陷的两处：`reading-v3` 夹具里条目列表显示「共 0 个」而列表已有 1 条（同组两条时显示正确）；`reading-graphics` 的 1440 宽浅色「流程图」截图停在页面上部，流程图在视口下方，同组深色与 360 宽截图正常、脚本断言通过。

### 遗留与未验证

- 未运行 NSIS 安装程序：安装包与用户已安装的归知是同一 appId，安装会覆盖或升级用户的应用。首次启动语言用打包产物 `win-unpacked`（安装包的同一份载荷）验证。
- 公网对照集成测试（`GUIZHI_TEST_LIVE_CRAWLER=1`）只做了加载检查：按「每个网址只抓一次」不重抓 40 个公网样本。
