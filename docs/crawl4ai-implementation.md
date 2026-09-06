# Crawl4AI 实施与验收记录

日期：2026-09-06。范围调整：用户明确将本次验收收敛为 Windows x64，以联网真实流程验证作为最后一步；Windows ARM64、macOS 和 Linux 验收暂缓，不再作为本次工作的完成条件。其他平台的构建配置保留，但不得宣传为已经验收。源码、干净 Windows 沙盒及联网流程的分项结论见 [Windows 验收记录](crawl4ai-p5-windows.md)；本次不公开发布。

后续升级快照与来源分组修复、打包应用升级/恢复证据及离线验收包，见 [Windows x64 升级与恢复验收](crawl4ai-p5-windows.md)。下文第一轮候选数据属于历史记录，当前应使用 P5 修复候选。

## 工作区与交付边界

实现位于隔离工作树 `D:\Project\GuiZhi-crawl4ai`，分支 `codex/crawl4ai-main-engine`。基准提交为 `ba75e41935489464bdd54a6b0b0cc7df1c70b3bd`；开始前已有的未提交工作一并复制后保留。原始 `D:\Project\GuiZhi` 不作为本任务的编辑或运行目录。本次不提交、不推送、不打标签、不公开发布，也不连接用户真实知识库启动应用。

## 固定运行包

`config/crawl4ai/runtime-lock.json` 固定 Crawl4AI 0.9.3（提交 `4bcd5fa8a56000ce103dd499e8ecdff2439f3e9c`）、Python 3.12.14 / python-build-standalone 20260901，以及 Chromium 151.0.7922.34 / revision 1234。四份平台锁文件固定 96 个 Python distribution 的版本与候选 wheel 哈希；发布构建使用 `--require-hashes --only-binary`，缺失匹配 wheel 或未锁依赖会失败。

| 应用安装架构 | 采集运行包 | 当前证据 |
| --- | --- | --- |
| Windows x64 | win32-x64 | 完整运行包已生成，独立 Python/Chromium 受控与公开网页流程通过 |
| Windows ARM64 | win32-x64 | 构建配置复用 x64 包，仿真真机验收待完成 |
| macOS x64 | darwin-x64 | 固定下载与 wheel 哈希、原生 CI 构建步骤已建立；未在原生系统执行 |
| macOS ARM64 | darwin-arm64 | 固定下载与 wheel 哈希、原生 CI 构建步骤已建立；未在原生系统执行 |
| Linux x64 | linux-x64 | 固定下载与 wheel 哈希、原生 CI 构建步骤已建立；未在原生系统执行 |

构建入口：`python scripts/build-crawl4ai.py --target win32-x64`（其他原生系统替换目标）。输出目录必须不存在，已有候选保留后在干净工作区重建。下载归档有 SHA-256 校验，随包资源清单再次覆盖分发文件。macOS 签名会改变二进制，因此 custom sign 先完成嵌套签名，再计算清单并重新封装外层签名，最后交给 builder 公证；不能在公证后的 afterSign 改包。此分支仍待 macOS 实机签名验证。

Electron 外部资源包含完整 Python、site-packages、Chromium、依赖许可和受限 worker。Windows ARM64 只复用采集包，不改变应用 Electron 架构。第三方归属见 `NOTICE`、`config/crawl4ai/licenses` 和随包许可目录。

## 关键实现位置与约束

- `main/services/web-capture/web-worker.ts`：版本化有界 stdio、任务 ID、独立浏览器/CDP、初始化取消、任务取消、空闲回收和所属进程退出。
- `resources/crawl4ai-worker/broker.py`：CDP Fetch 在首次导航前安装，逐请求/逐重定向暂停；iframe 子目标也安装拦截。上层 Playwright route 不保证拦截后续 HTTP 重定向，实际复现后改为 CDP 实现。
- `web-network.ts`：统一主进程出口、现有代理、逐跳公网解析钉扎、TLS 校验、压缩前后 10 MiB 上限；整个页面最多 200 次请求、50 MiB 响应，请求池最多 8 个。浏览器使用黑洞代理兜住遗漏请求，不用直连修复兼容性。
- `extract.py`：Crawl4AI 的 LXML 清洗与 raw Markdown，保留中文短文、表格、代码和链接，不使用英文词数或 fit_markdown 作为入库正文。渲染等待与滚动有界，正文上限 200,000 字符。
- `crawl-service.ts`、`packages/db/src/crawl-job.ts`：有界发现、持久队列、robots、逐页结果与终态导入记录。成功页入库与队列完成状态在事务内写入。
- `web-source.ts`：采集哈希、采用哈希、标题基线、版本快照、CAS 采用与检索失效。原文版本属于知识条目，研究快照单独持久化。
- `web-research.ts` 与现有 research 模块：指定入口、候选内容去重、本轮正文复用、时间模式与冻结证据。旧平台读取路径仍受原有登录/平台约束。
- shared IPC 常量、main 注册与 preload 白名单：网页状态、批次操作、原文版本和研究参数；renderer 不接收 hooks、任意脚本、Python 文件路径或上游配置对象。

迁移 `0030-web-capture` 新增四张网页表，添加研究时间模式，并在迁移框架事务内重建两个来源 CHECK 表，保留原列、索引和记录数。新表进入现有 SQLite 文件备份；中断记录恢复后等待用户继续。

## 已执行验证

源码门禁：完整单元测试 203 个文件、1845 个测试通过；typecheck、lint（含文件行数门禁）和生产构建通过。隔离离屏脚本覆盖实际组件状态、窄窗口、纯网页不限时间，以及注入到临时测试环境的批次部分失败、加载失败、英文表单和原文并排比较；后者是 UI 状态夹具，不冒充真实第三方失败流程。

完整随包 Python 和 Chromium 的离线受控流程覆盖动态中文、表格、代码、相对链接、有效短文、访问限制、HTTP 重定向、跨范围重定向拒绝、跨域 iframe、fetch 重定向、取消与退出。请求由测试夹具替换主进程出口，未关闭安全边界，也不依赖系统 Python 或浏览器。

50 页受控批次通过：约 123.4 秒保存 50 个独立条目和 50 个原文版本，外键检查通过；空闲约 60.6 秒后 worker 状态停止且所属缓存目录清空。此为本机受控性能，不能替代低配机器/其他平台性能或独占物理内存测量。重复、暂停恢复、失败重试与编辑保护另有数据库单元测试。

从 x64 候选目录直接使用生产资源路径，再次通过完整受控流程及 worker 崩溃后重新初始化测试，包含实际随包清单和 worker 校验。此轮首次采集（含完整性验证与初始化）约 24.0 秒，后续短文约 2.1 秒；两页之后，所属 Python、Playwright 驱动和 Chromium 进程树的 Windows `WorkingSetPrivate` 快照合计约 356.6 MiB。该计数排除共享驻留页，不是峰值，也不含 Electron。机器同时在压缩安装包，未控制操作系统文件缓存，因此这些数据仅供本机运行成本参考，不能称为冷启动基准。原始逐进程数据在 `artifacts/crawl4ai/runtime-metrics.json`；首次启动成本仍需在各平台验收中评估。

固定公开样本 `config/crawl4ai/public-samples.json` 共 40 个网址：37 个正文页面匹配预期关键片段；403、404 和重定向至 JSON 三个边界样本匹配具体错误。对照中未出现旧 Readability 已匹配关键片段而新流程失配的样本。测试仅在隔离进程显式使用本机已有代理；机器直连 DNS 返回 fake-IP 时被正确拒绝，未改用户代理设置。关键词匹配不等于全部正文质量已完成人工验收。

真实站点结果与旧流程对照、受控批次指标、离屏截图和本地安装产物写入忽略的 `artifacts/crawl4ai/`。动态正文、表格和代码的具体结构由受控集断言；实际网站内容可能变化。

最终 Windows 候选位于 `artifacts/crawl4ai/final-candidates/{x64,arm64}`。文件名沿用当前应用版本 0.22.0，但这些是本地未发布候选，不代表该版本已发布此功能。`scripts/check-crawl4ai-package.py --root artifacts/crawl4ai/final-candidates` 核对两套 Electron 的 PE 架构、共用 x64 Python/Chromium、完整资源文件集、逐文件哈希与安装包 SHA-256，并生成 `package-check.json`。安装包生成和资源运行测试不等于干净机器安装或 Windows ARM64 仿真真机验收。

两个 Windows 候选均已通过上述检查：x64 安装包 366,248,680 字节，ARM64 安装包 366,556,512 字节；各含 16,232 个已校验运行包文件和 3 个 worker 文件，采集运行包展开后 1,079,028,905 字节。未在同一条件重建无 Crawl4AI 的安装包，因此不将这些总量称为安装包增量。候选使用本机压缩级别 5，正式构建仍使用默认设置。

本任务代码清单为 `artifacts/crawl4ai/task-paths.json`，相对开始时隔离副本的独立补丁为 `task-changes.patch`，最终门禁日志在 `evidence/`。补丁不包含继承的未提交工作或忽略的二进制运行包；完整 Windows 运行包保留在开发资源和两个候选目录。原始工作区的 918 个基线文件逐项核对后没有正文变更；checkout 引起的既有行尾差异单独记账，未回写原始目录。

## 可复现命令

```powershell
corepack pnpm typecheck
corepack pnpm test:unit
corepack pnpm lint
corepack pnpm --filter @guizhi/desktop build
$env:GUIZHI_TEST_BUNDLED_CRAWLER='1'
# 如需核对候选包而非开发资源，先设置：
# $env:GUIZHI_TEST_RUNTIME_RESOURCES='D:\Project\GuiZhi-crawl4ai\artifacts\crawl4ai\final-candidates\x64\win-unpacked\resources'
corepack pnpm --filter @guizhi/desktop exec vitest run tests/integration/web-capture-runtime.test.ts
$env:GUIZHI_TEST_50_PAGE_BATCH='1'
corepack pnpm --filter @guizhi/desktop exec vitest run tests/integration/web-capture-batch.test.ts
corepack pnpm shot --steps D:/Project/GuiZhi-crawl4ai/scripts/crawl4ai-ui-steps.mjs --out D:/Project/GuiZhi-crawl4ai/artifacts/crawl4ai/ui
corepack pnpm shot --steps D:/Project/GuiZhi-crawl4ai/scripts/crawl4ai-ui-states.mjs --out D:/Project/GuiZhi-crawl4ai/artifacts/crawl4ai/ui-states
```

公开网站复测另设 `GUIZHI_TEST_LIVE_CRAWLER=1`。测试环境需要显式代理时设置 `GUIZHI_TEST_HTTP_PROXY`；不要为了测试修改真实设置或跳过公网校验。`GUIZHI_TEST_RESUME=1` 从结果文件继续，`GUIZHI_TEST_RETRY_FAILURES=1` 只复测已有失败记录。测试进程完成本身仅证明样本遍历完成，应单独检查每行 `expectedMatched`。

## 初始全平台待办（历史，后续范围已调整）

以下为初始计划的开放项；Windows 安装和联网的最新分项结论见 [联网验收记录](crawl4ai-live-validation.md)。用户已暂缓其他架构验收，不再将其作为本次工作的完成门槛。

1. 五种安装架构在各自受支持系统验证：尤其 Windows ARM64 的 x64 仿真真机，以及 macOS 原生签名/重签名、Linux 系统库。仅下载校验或安装包生成不算运行验收。
2. 干净机器安装、断网初始化、升级前备份、上一发布版本真实数据库升级/恢复，以及恢复后中断批次等待用户继续。当前数据库夹具验证不能代替真实升级样本。
3. 登录平台、视频下载/字幕/转写、论坛、手机收集的真实端到端回归；当前没有复用用户登录态做这些操作。
4. 真实模型的规划、报告质量、混合来源研究和报告导出人工验收。单元测试与本轮网页快照测试不等于真实模型质量通过。
5. 每个平台的安装包增量、首次/后续启动、独占物理内存和残留进程测量。不能把所有进程工作集直接相加当独占物理内存。
6. 更广的恶意页面、证书链/代理认证/系统代理实网、worker 崩溃与初始化中应用退出故障注入。现有安全单元测试和浏览器隔离测试是部分矩阵，不是全量发布认证。

未验收项继续如实保留，不宣传为已支持的实测平台。提交、推送、标签及公开发布等待后续明确指令。
