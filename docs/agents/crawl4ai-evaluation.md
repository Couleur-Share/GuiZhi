# 随包 Crawl4AI 去留评估

2026-11-07 只读对照评估，未改源码。逐页明细、脚本与缓存留在评估时的临时目录，不随仓库保存；本文保留结论、依据与移除门槛。去留尚待产品决定，见 [产品边界](../product-scope.md)。

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
3. 换提取器会让同一网页的 `contentHash` 整体变化。先定策略（按引擎版本重建基线或提示用户），避免重新采集时批量出现「待采用版本」。

## 移除涉及的其他改动

- 网页组件状态与修复界面（`web-runtime.ts`、`web-runtime-integrity.ts`，设置 → 采集与转写 → 内置网页采集）、文档站与网页研究的「组件可用」门槛、`engineVersion` 字符串 `crawl4ai/0.9.3-*`。
- 打包与 CI：`scripts/build-crawl4ai.py`、`apps/desktop/scripts/crawl4ai-package.cjs`、`config/crawl4ai/*`、体积预算的 runtime 项、NOTICE 与第三方许可、[Crawl4AI 文档](../crawl4ai.md)。

## 未覆盖的风险

- 渲染输入来自 Playwright 的 Chromium，不是产品内的 Electron；网络出口、跨源子框架与每页 200 请求的预算都不同。
- 固定样本都是技术文档，补充样本同一天抓取；R5 的阈值在同一批数据上校准，有过拟合风险。
- 未测非 UTF-8、需登录或付费、超过 200k 字符、CodeMirror / shadow DOM、RTL 页面。
- 现有 40 网址验收只比对关键词，区分度很低（各路径 35–36/37 命中）；无论去留，以后都应改用结构与覆盖指标加人工抽检。
