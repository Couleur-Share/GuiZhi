# 归知 GuiZhi — 开发者与 AI 助手指南

归知是本地优先的 AI 个人知识库。本仓库是 v0.3 起的 Electron 技术栈实现，
应用骨架 fork 自 PromptHub v0.5.9（AGPL-3.0，见 NOTICE），业务域为归知自研。

## 技术栈

- Electron 33（main / preload / renderer 三进程）
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

## 里程碑路线（v0.3 重构）

v0.3.0-alpha.1（2026-07，内部预发布）：M0 骨架收缩 → M1 核心域
（DB schema + 三栏知识库 + FTS）→ M2 采集管线（导入队列 + 网页抓取 + 去重）→
M3 AI（摘要/标签/问答）→ M4 Wiki（AI 增量编译互链页面网络）→
M5 旧数据迁移（guizhi.db 一键迁入）→ M6 打包发布（CI 构建 + GitHub Release）。

M7 已完成：本地备份/恢复/定时备份 + Markdown 导出（`main/services/backup.ts`、
`export-markdown.ts`）、问答会话持久化（`ask_sessions` 表）、批量多选、
三模块侧栏面板、autoSave 接线、e2e 冒烟。
M8 已完成：embedding 语义检索——`knowledge_embeddings` 表存归一化 Float32 BLOB，
渲染进程分块嵌入（`renderer/services/knowledge-ai/semantic-*.ts`），
主进程余弦 top-k（`main/services/semantic.ts`），问答检索走 FTS+语义 RRF 融合
（`hybrid-search.ts`），未配置 embedding 模型时静默退化为纯 FTS。
M9 已完成：媒体采集——本地图片/音视频资产化导入（`import/media-files.ts`）、
图片 OCR（`knowledge-ai/ocr.ts`，visionText 路由）、音视频转写
（`media/transcribe.ts`，新增 audioText 路由）、yt-dlp 在线视频解析
（`import/video-url.ts`，B站/YouTube/小红书，未安装时降级）。抖音例外，
见下方「抖音不走 yt-dlp」。
M10 已完成：Wiki 关系图谱（`WikiGraphView.tsx`，react-force-graph-2d 懒加载）。

v0.4.0（2026-07，首个公开发布）在此之上重构了知识库界面：卡片 / 列表双视图、
表格列配置与分页（`library/ItemTableView.tsx`、`item-table-config.ts`）、
标签选择浮层（`TagPickerPopover.tsx`）、详情全屏弹窗（`ItemDetailModal.tsx`）。

v0.5.0（2026-07）是一轮集中整改：知识库列表改服务端分页（`page`/`pageSize`
进 store 直落 SQL）、AI 请求打通取消链路（`ai:httpCancel` + signal 贯穿到
`runScenarioChat`）、Wiki 编译不再整体覆盖未进上下文的页面并新增
`wiki_page_revisions` 快照、补上 `schema_migrations` 执行器与 `user_version`
版本戳、Electron 侧加 CSP 与 `will-navigate` 拦截、settings 双向字段白名单。

v0.6.0（2026-07）围绕抖音采集：抖音脱离 yt-dlp（见下），图文作品补成完整能力
（配图入资产库 + 逐图 OCR + `ImageLightbox` 查看器 + 正文拆成文案/图片/图中文字
三个标签，`shared/utils/image-note.ts`）。顺带修了两个影响全应用的渲染缺陷——
`.prose` 没恢复被 preflight 清掉的 `list-style`（列表没序号），以及 rehype-sanitize
与 react-markdown 的 `urlTransform` 双双拦掉 `local-image://`（正文图是破图）。

正文配图（AI 文生图）：把长文里的一个认知锚点画成一张插图，插进正文对应段落之后。
新增 `imageGen` 路由与 `imageGeneration` 能力位（第六条路由，专用模型、不参与对话）。
两段式，与「先出 shot list 再逐张生成」的做法一致：`illustration/plan.ts` 走 mainText
读正文出配图规格（JSON），用户在面板里勾选与改词后，`illustration/runner.ts` 串行
逐张生成。串行不是偷懒——生图按张计费且每张几十秒，串行才有稳定进度、可中途停止，
也不容易撞 provider 的每分钟图片数限额；中途停止会把已经画好的照常写进正文
（钱已经花了），不能连同已生成的一起丢掉。
策划张数必须是确定的：面板上有张数选择器，「自动」按可配图段落数推导
（约每 3 段一张，`deriveShotTarget`），选了具体数字就要求恰好那么多。
提示词给的是「本次要 N 张（恰好这么多）」而不是「最多 N 张」——只给上限
再叠一句「宁少勿滥」，同一篇文章两次策划会给出 3 张和 4 张。策划这步的
temperature 是 0.4 而不是 0.6，创意留在 composition / elements 的措辞里，
别让它去影响「挑哪几段、出几张」。
另一个隐蔽的张数来源是解析层：`snapToAllowed` 把越界序号贴到最近的候选，
必须贴到最近的**未占用**候选——按「最近」贴的话两条撞号，后一条会被整条
丢掉，模型出了 4 张用户拿到 3 张，界面上还看不出少了什么。
4 张成 3 张时失败那张要留得住：生成结束后只清掉成功的 shot，没成的连同原因
留在面板里单独补生成——重新策划整篇既费一次调用，出来的也不是同一张图。
留的时候必须用主进程回传的 `IllustrationFailure.afterBlock`：同批成功的图
每张都是一个新段落块，会把它后面的段落序号整体顶后，拿旧序号去补会插到
相邻的错误段落，同批成功得越多偏得越远（`runner.ts` 的 `shiftFailedAnchors`，
有落位回归测试）。
shot 规格里的 `elements`（画面里出现哪些具体物件）不能省，必须要求取自原文。
放任图像模型自己编例子，画出来的东西会和正文自相矛盾——实测「四象限理性消费」
那篇，正文把手机归在「买好的」，图里却把手机画进了「谨慎购买」那一格。
面板把这个字段做成可编辑，就是让人在花钱之前扫一眼。
`scene` 刻意用物理情境命名（分拣 / 衡量取舍 / 过滤漏斗 / 卡住与通过…）而不是
图表体裁（流程 / 分层 / 路径），后者等于在请模型画流程图。「要插画不要图表」
（禁坐标轴、2×2 矩阵、表格、网格、正式流程图）写在
`illustration-prompt.ts` 的 `BASE_IMAGE_CONSTRAINTS` 里而不是预设的 `negative`：
预设文件首次运行就落到用户机器上、之后不再被覆盖，写在那里的修正到不了
已经在用的人手上；何况这是功能本身的立场，不是某套画风的偏好。
预设的 `negative` 还有一层不直观的代价：图像接口根本没有 negative 参数，
这段话是**拼进正向提示词**的，安全分类器读到的是 anime、manga、chibi、
children's book 这些词本身而不是「不要它们」，而它们恰好挨着最容易误伤的
那几类。内置预设因此换成不带这些词的说法（`comic-book` / `big-headed` /
`picture-book`），`BUILT_IN_ILLUSTRATION_STYLES` 上方留了注释、另有测试挡回潮。
但同样因为预设不被升级覆盖，这份修正到不了已经在用的人手上，真正的兜底
只能在代码里：`runner.ts` 的 `buildFallbackPrompt` 在确认被内容安全拦截时
去掉排除项重画一次（被拦下的请求不出图也不计费，多试这一次不额外花钱）。
做成「只在真被拦时才动」而不是每次都清洗是有意的——「这些词会触发拦截」
目前只是合理怀疑，实测带着它们连打 8 次全过，为一个未证实的假设去静默删改
用户写在预设里的东西不划算；做成兜底，它自己就是这个假设的验证。
策划顺带给一个风格建议：模型本来就在读正文，把其余风格的「名称 + 适用说明」一并
塞进提示词，零额外调用。为此输出协议从裸数组改成 `{styleId, shots}`，解析层两种
都收——这层兼容不是为了旧数据，是因为模型经常无视格式指令，为此整次策划失败不值得；
单条 shot 的裸数组切出来正好是个合法对象，靠「有没有 shots 数组」区分。
建议只做提示不自动切换（`resolveSuggestedStyle` 还会挡掉编出来的 id 与「建议的就是
当前这套」），用户可能是有意选的那一套，悄悄替他改掉最招人烦。
位置用**块序号**表达而非原文片段模糊匹配：`shared/utils/illustration-note.ts` 的
`splitContentBlocks` 同时供策划提示词与插入定位使用，两边编号必然一致。该切分是
围栏感知的，否则带空行的代码块会被从中间切开、图插进代码里。
「全部清空」走 `illustration:clear` 一次删完：逐张调 remove 要走 N 趟 IPC、写 N 次
正文，中途失败还会留下删一半的结果。图片文件跟着 `cleanupOrphanAssets` 一起回收，
删了找不回来，所以必须过 `ConfirmDialog`。
成图以 `local-image://gen-*` 写进 `content`，这不是选择题——`cleanupOrphanAssets`
的引用集合来自对 content 的 `LIKE '%local-image://%'` 扫描，不在正文里的资产会在
下一次彻底删除时被当成孤儿删掉；导出与全文检索同样只认正文。`gen-` 前缀把 AI 配图
与采集导入的 `import-` 资产区分开，详情页据此只管理前者。
风格是数据不是代码：`config/illustration-styles.json`（`core/illustration-styles.ts`）
首次运行播下六套内置预设后即归用户所有，改画法、换配色、加一个每张图都出场的固定
角色都不用改代码。编辑有两个入口——条目配图面板里的「编辑风格」弹窗与「设置 →
正文配图」——但只有一份实现：草稿、校验、落盘全在 `components/illustration/`
的 `use-style-drafts.ts`，界面主体是 `StyleWorkbench.tsx`，两个外壳只差
「要不要 Modal、关闭时要不要拦未保存」。风格的 `description` 必须进选择器的下拉项
（`SelectOption.triggerLabel` 让触发器仍只显示名字）：光看「淡墨速写」这类名字，
用户选不出该用哪套。`group` 是自由文本不是枚举——内容类型会一直长，枚举一定不够用。
预设文件（v2）里的 `dismissedBuiltIns` 是这套东西可持续的关键：升级新增的内置风格
在 `read()` 里增量补进用户的文件，删过的记在名单里不复活。名单每次保存按「内置里
当前列表没有的那些」重算而不是累加，所以点一次「恢复内置预设」它自己就清空了，
不会留下永久拉黑的记录。老文件（没有这个字段）按「缺的都补上」处理：代价是更新前
删过内置风格的人会被退回来一次，换的是这个机制上线当天就对所有老用户生效。
单套风格可导出成 JSON 进剪贴板、粘贴导入（`style-transfer.ts`）：分享风格的实际
形态就是贴一段 JSON，比走文件对话框顺手；导入不读剪贴板，`readText` 在 Electron
里未必拿得到权限，失败还是得回落到粘贴框。六套刻意在**底色 / 媒介 / 有没有人**三个维度上分开：最早那三套
（手绘笔记 / 极简色块 / 淡墨速写）全是「白底 + 极简线条」，只在笔触上有差别，
于是技术类与生活类看着一个样，讲关系的文章还画不出人（`character` 全空）。
后补的四套各占一个空位——深色蓝图（深底，系统与链路）、暖调生活（暖色，健康与
消费类的日常物件）、双色小人（唯一带 `character`，靠姿态与距离讲关系）、
等距场景（设备摆位与连接）。同时删掉了极简色块：它的「结构与对比」职责被后两套
接管，而它自己有前科——本来叫「极简图解」，因为一直往流程图上滑才改名并补了
`never to build a diagram`，留着等于在列表里摆一个天生倾向于画出本功能明确不要的
东西的选项。
风格选择按知识库记在 localStorage（`use-illustrations.ts` 的
`guizhi-illustration-style-by-collection`）：`styleId` 是面板的组件状态、面板一关
就卸载，不记的话每次都落回第一套，同一个库里的东西还得一篇篇重选。只有用户主动
选才写记忆，打开面板时的回落不写；记住的那套若已在编辑器里被删掉，落回第一套。改它走面板里的「编辑风格」（`IllustrationStyleEditor.tsx`）——
这个按钮此前是 `shell.openPath` 那份 JSON，而 Windows 上 `.json` 多半没有默认关联
程序，点下去弹的是系统的「选择一个应用」框，等于请用户拿记事本去改一份英文提示词。
「在文件夹中显示」（`showItemInFolder`，不是 `openPath`）留给真要手改 JSON 的人。
写入比读取严：读到坏条目静默丢掉是对的（用户手改坏了不该连累整个功能），保存时
再沿用这套宽容逻辑就成了「点了保存、界面说成功、重开发现那套风格没了」——
`write` 因此逐条校验并指名道姓地回错，id 撞号顺延后缀。「恢复内置预设」只换本地
草稿不落盘，否则它和旁边那颗「取消」自相矛盾。
`illustration/image-gen.ts` 里协议分裂是主要复杂度来源：OpenAI 的 `gpt-image-*`
**不接受** `response_format`（传了报 invalid_request_error），它本来就只回 base64；
而 `dall-e` 系与多数中转站模型不显式要 `b64_json` 就会回一个 60 分钟过期的 URL——
按模型名分流，两边都得照顾。真正的 16:9 只有 `gpt-image-2`（size 可任意，边长须为
16 的倍数、长短边比 ≤3:1，`1536x864` 正好）与 Gemini（`imageConfig.aspectRatio`）
给得出，其余 OpenAI 模型只有三档固定尺寸、横版最宽 3:2。Gemini 走原生
`generateContent` 而不是 OpenAI 兼容层，因为只有原生请求体带得了 aspectRatio。
超时设 240 秒：`ai:httpRequest` 那 30 秒的默认值对生图远远不够。
设置页的「测试当前配置」按能力分派（`runModelConnectionTest`），文生图必须走
`illustration:testModel` 而不是 chat completions——后者会被 provider 直接回
`model_not_supported`。探测是真画一张 1024×1024 最低质量档的图：文生图最常见的
失败是模型名不对或账号没开通该模型，只打 `/v1/models` 一概查不出来。这一次会真实
计费，能力卡片的说明里写明了。
生图必须重试，而且不能看状态码下结论。中转站是一池上游渠道按成功率轮询
（云雾 API 那套 new-api 会回 `x-routing-priority: success_rate`），渠道之间的
内容安全严格程度并不一致：同一份提示词落到 Azure 部署的渠道被判 safety、
落到直连渠道就正常出图——实测同一条提示词连打 8 次全成，而用户那一批 5 张
偏偏挂了 1 张。这类失败是随机的、和写了什么关系不大，重发一次多半就过。
更麻烦的是状态码不可信：官方的 `moderation_blocked` 是 400，实测却拿到过
`HTTP 429: Your request was rejected by the safety system`，读作限流就完全
跑偏了。`describeImageHttpFailure` 因此必须把 `error.code` 带进消息——它是
「换个渠道重发就好」与「这句话改不了就是过不去」之间唯一的分界，只给状态码，
用户只会一遍遍地点重试。
重试只按状态码判：5xx 与 429 重发（`RETRY_DELAYS_MS = [2000, 6000]`，两次），
其余 4xx 一概不重，模型名不对、余额不足、400 moderation_blocked 重发一万次
也是同一个结果。`RETRY_TIME_BUDGET_MS` 兜住最坏情况：单次超时上限 240 秒，
不设总预算的话三次尝试能把一张图拖到 12 分钟、一批五张拖掉一小时。探测
（probe）不重试，它按张真实计费，一次连接测试悄悄变三次不合适。抓一个免费
网页的 `import/v2ex.ts` 都退避重试两次，全应用最慢最贵的这一步此前一次都没有。
逐张失败还要写进 `error.log`（`runner.ts` 调 `logAppError`，策划 / 整批 /
单张重生成三处 IPC 出口同样记）：此前只有 `console.warn` 加一个关掉就没的
toast，用户报「失败率特别高」时双方都拿不出数。记录带上「第几张 / 共几张」，
失败率的分母才有着落；代价是图题会进日志，而日志是会被发出来求助的东西。

文字稿排版（`media/transcript-format.ts`）：转写稿是整条链路的地基——排版、
总结、embedding 分块、问答引用全建在它上面，所以这一步的失败不能是静默的。
`formatTranscript` 返回 `{ text, skippedReason? }` 而不是裸字符串：超长跳过时
也要回原文，只给字符串的话调用方分不清「排好了」和「太长没排」，会把没排版的
稿子当成功写库。自动链路（导入 / 重新转写）的失败与跳过都过 `logAppError`
进 `error.log`；未配置文本模型是常态不是故障，不记，否则日志会被刷满。
本地转写的速度上限在 funasr 手里：CPU 上它把批处理整个关掉了
（`auto_model.py` 里 device 为 cpu 时直接 `batch_size = 0`），每个 VAD 段
单独跑一次前向，而且这个判断不经 kwargs、改不了。剩下唯一的旋钮是 `ncpu`
（它会 `torch.set_num_threads`），默认只有 4。`resolve_ncpu()` 改成按机器核数
取一半、下限 4 上限 12。实测 301 秒音频：4 线程 14.6s、12 线程 12.6s、
16 线程 10.8s，带说话人分离时 4 线程 30.3s、12 线程 26.1s——收益有限
（每段单独前向是延迟受限不是吞吐受限），所以不占满机器，转写要跑几分钟，
期间整台机器不该卡。

转写进度只报「已用 / 停滞」，不报百分比。转写是一个不返回中间结果的长请求
（20 分钟的访谈带分离要在 CPU 上跑好几分钟），只给一个转圈的按钮，用户分不出
「在跑」和「卡死」。但 funasr 给不出可用的分母——`progress_callback` 在 VAD
路径下每段单独成批，实测 `total` 恒为 1，只有回调次数是真的。写死的分母只会
骗人（与导入阶段不给「第 N 步 / 共 M 步」是同一条理由），所以服务脚本每处理
完一段往 stdout 打一条 `[guizhi-asr] tick`（节流到每秒一条，一小时音频几百段
会把日志尾巴冲掉），`funasr-service.ts` 记下时刻，主进程按秒回报已用时长与
距上次心跳的间隔，超过一分钟没动静才在界面上说出来。走 stdout 而不是加一个
HTTP 进度端点，是因为端点里的 `generate()` 是阻塞调用、会把事件循环占住，
要轮询就得先把端点挪进线程池并加锁，为一个进度条改并发模型不划算。
云端转写没有心跳，此时只报已用时长。
计时必须覆盖**整条链路**（转写 → 排版 → 总结）并带上当前阶段：这三步都以
分钟计，只给转写计时的话，后两步期间界面会停在最后一个数字上、文案还写着
「正在转写」——看起来和卡死一模一样（实测被当成 bug 报过一次）。

超时与时间预算：单块 `CHUNK_TIMEOUT_MS` 是 240 秒，不是 120。原来的 120 秒
实测偏紧——同一条短提示词在云雾的 qwen3.5-flash 上耗时 75 / 117 / 130 秒
都出现过，还撞到过两次 280 秒不返回，成因与生图那边记的是同一个（中转站按
成功率在一池上游之间轮询，快慢随机）。120 秒正好卡在正常区间的上沿，于是
「本来会成功」的请求被自己的超时掐掉，重试再掐，整块作废。
放宽单块超时就必须同时加总预算，否则 32 块 × 2 次尝试 × 240 秒能拖两个多小时；
预算按块摊（`BUDGET_PER_CHUNK_MS` 90 秒/块，下限 5 分钟）而不是取固定值，
因为 32 块和 63 块（用户确认过代价的超长稿）需要的额度差一倍不止。
块之间是并发的（`FORMAT_CONCURRENCY` 3 路）：块互不依赖、按下标归位，而中转站
单次要几十秒，串行会让排版成为仅次于转写的一段耗时。并发改变了「部分保留」的
判据——不再是「成功了几块」，而是**第一个没完成的块之前**那一段；它后面即便
有块排好了也用不上，中间缺一块就接不回原文。
预算耗尽或中途某块失败时**不整篇作废**：已排好的块留下，剩下的按原文接回去
（用字符游标切原文，不拿 chunks 反拼——反拼会丢掉切分点上的空白，把两个
英文词黏成一个），回一个 `partialReason`。这些请求的钱已经花了，半篇排好的
稿子严格优于一篇没排的，理由与配图「中途停止也把画好的写进正文」相同。
但**第一块就失败仍然抛错**：一块都没成多半是模型名或鉴权配错，降级成
「部分成功」会把真问题盖住。用户主动取消同样直接抛，不留半成品。
`TRANSCRIPT_FORMAT_LONG_CHARS`（5 万字，约 3 小时口播）是**自动**链路的上限，
不是能力上限：超过它的稿子恰恰最需要分段，所以详情页的按钮允许用户确认后越过
（`allowLong`），确认框里写明字数与预估请求次数。按块进度必须上报
（`media:formatProgress`）——5 万字是 32 次串行请求，不报进度的话按钮要静默
转好几分钟，和配图那边记过的那笔债是同一种。

专名表（`extractGlossaryTerms`）：本地引擎中英混杂时会毁掉专有名词，而专名
正是全文检索与语义召回最依赖的那批词。实测（SenseVoiceSmall，TTS 样本）纯中文
逐字正确，混英文时 `Docker → dacker`、`useState → us state`、
`TypeScript → typepescript`，错误全部落在拉丁词上，中文只在交界处被吞。
解法没有选择换 ASR 模型（原因见下），而是把条目标题与简介里的拉丁词抓成一份
封闭术语表，塞进**本来就要发的**那次排版调用，等于在文本层补上热词——
SenseVoice 不支持热词，funasr 里只有 paraformer 系支持。
A/B 实测过，且必须用冷门专名才看得出差别：`React`、`Docker` 这类词模型自己
就能改对，带不带表都一样；而 `seaco paraformer` 被听成 `cicopowerformer` 时，
不带表的模型**编出一个像模像样的错名**「CICOPowerFormer」，带表的才还原成
`seaco-paraformer`。编出来的错名比留着乱码更糟——它看着像真的，检索时却指向
一个不存在的东西。指令因此收紧到「把文中已有的错误形式改成表里的写法」，
并明写「表以外的词不要改动」：同一次实测里表外的 `cam plus plus`，不带表的
模型擅自改成了 `CAM++`（这次碰巧对），带表的按约定没动。放开成自由改写
就是拿幻觉换错字。

本地转写不换模型（查过了，别再查一遍）：帖子里常被推荐的 Qwen3-ASR、
FireRedASR2、X-ASR 在我们的约束下都够不着。funasr 1.3.29 确实注册了
`Qwen/Qwen3-ASR-0.6B` 与 `1.7B`，但要额外装 `qwen-asr` 与 `accelerate`，
且把 `transformers` 钉在 `4.57.6`（funasr 自己带的是 5.x，差一个大版本），
默认 `device="cuda:0"`，代码注释写明 0.6B 约需 4GB 显存、1.7B 约需 8GB——
我们装的是 `torch+cpu`。Fun-ASR-Nano 是 800M 的 LLM-ASR，目录里全是
`inference_vllm*.py`。FireRedASR2 / X-ASR / Dolphin 在 funasr 里根本没有
对应实现，接它们等于再引一套推理栈。栈内唯一 CPU 跑得动的替代是
Paraformer-zh，但它定位是纯中文、厂商公布的 CER 反而更差，且不像 SenseVoice
原生出标点，还得再挂一个 290MB 的 `ct-punc`。要更强的转写，现成出路是
audioText 路由本来就接受任意 OpenAI 兼容地址，有 GPU 的用户自己跑一个指过来。

说话人分离：`spk_model="cam++"`（下载 27.5MB，
README 写的 7.2M 是参数量不是文件大小），不换 ASR 模型也能用。两条硬约束都是
实测出来的。其一，**cam++ 不能常驻**：`AutoModel` 只要带了它，每次推理都会对
每个 VAD 子段跑声纹提取（funasr 只判模型在不在，`return_spk_res` 只管最后
输不输出标签），28 秒音频多花约 2.6 秒且随时长线性增长——所以服务端按请求
切换、模式变了就重建实例（本地已有模型文件，重载约 6.5 秒）。其二，**分离
粒度等于 VAD 段**，而且**不要为此去降 VAD 的收尾静音阈值**。曾经从默认 800 毫秒
降到 400，依据是合成语音的实测（换人间隔 0.9 秒的 TTS 对话在 800 下整段切不开）。
换真实音频一测，这个依据是假的：同一段访谈在 800 下照样分出两个说话人，而 400
把 5 分钟从 65 段切成 85 段，**切点上开始丢字**——「花了很多很多年」丢成
「了很多很多年」，更狠的一处把「想明白了」识别成了「相？帰了」（片段太短，
语种判定跑到日文）。切点越多，丢字的机会越多，而转写稿是检索、嵌入、总结的
共同地基，为说话人标签去糟蹋它是稳亏的买卖。
残留的代价是换人发生在 VAD 段内部时（对方接话没有明显停顿），整段会被判给
同一个人。标签错看得见、也不进检索；文字错是静默的、且改不回来。
正文形态是「说话人 N：」对话体，相邻同一说话人合并成一段——400 毫秒阈值下
不合并会碎成几十行半句话。这一步放在 Python 服务端而不是主进程，因为它要读
`sentence_info`，而 `transcribe.ts` 只读 `text`。另注意开了 spk 之后
`sentence_info` 的文本字段名从 `text` 变成 `sentence`，两种都得认。
前缀必须活过 AI 排版。排版是要重写正文的，不明确要求保留，模型会把
「说话人 N：」当成口播赘语删掉或把相邻段落合并，分离就白做了。所以正文带前缀时
提示词追加一条保全指令，`rejectFormattedChunk` 再数一遍——原文几处、输出就得
几处，少了是删了或合并了，多了是模型自己编的，两种都判不合格重试。识别标记的
`shared/utils/speaker-note.ts` 与 Python 侧的 `speaker_label` 是同一个格式的
两端，改一处要同步另一处。
界面只在支持时才摆入口：`media:capabilities` 回当前 audioText 路由能不能分离，
不能就不显示那个按钮——摆一个点了必然报错的按钮不如不摆。真发起了却不支持时
主进程仍会明确报错，不静默忽略。
两个入口：详情页的按钮（对已有条目重新转写）与「设置 → 采集」的
`transcribeDiarize` 开关（导入时生效，默认关）。做成全局开关而不是逐条选，
是因为它对单人内容没有收益却让转写慢一倍，而绝大多数采集是单人视频；
真正需要的人（常导访谈、会议）设一次就够。
已知边界：材料太少会塌成一个人（实测 15 秒 3 轮的对话全判成说话人 1，
同样音色的 28 秒 5 轮则完全正确），所以只出来一个说话人时回一条 warning 而不是
绿色的「已生成」——它既可能是真的单人，也可能是没分出来，得让用户自己判断；
重叠说话在 vad_segment 模式下无解；真人同性别未验证，实测样本是合成的男女声，
属最容易的情形。

句级时间轴目前无人消费：`funasr-server-script.ts` 开了 `sentence_timestamp`，
实测 `sentence_info` 是真实的（起止时刻对得上停顿），但**粒度是 VAD 段而非
句子**——连续说话不断句就是一整段，上限是 `max_single_segment_time` 的 30 秒。
而 `transcribe.ts` 请求的是 `response_format: "json"`，只读 `text`，
所以 `build_segments` 那条分支在生产里走不到。将来要做「点段落跳到视频位置」
得先解决一个矛盾：AI 排版会重写正文（删口头语、重新分段），排完的段落与原始
VAD 段不再一一对应，得在排版前存下映射或做模糊对齐。

论坛帖子采集（V2EX）：`import/v2ex.ts` 走官方 v1 只读接口
（`/api/topics/show.json` + `/api/replies/show.json`），无需 token，
限额 600 次/小时，一次采集用两次；回复接口不分页，总是一次返回整帖。
接口的 5xx 与连接超时会退避重试两次（1.5s / 4s，`RETRY_DELAYS_MS`）——实际
撞到过 Cloudflare 522（边缘节点收下请求但 V2EX 源站没应答），帖子本身没问题，
隔几秒再打就是 200。4xx 一律不重试：帖子不存在、限额用尽、被风控拦下，
立刻重试只会更快撞上限。`describeFetchError` 负责把状态码翻成用户能据此
行动的说法，导入列表上不该出现裸的「HTTP 522」。
不解析 HTML 是有实测依据的——同一帖 Readability 只抽到 107 条回复里的 72 条
（丢掉最早的 1~5 楼），且 37% 的字符是头像图片 URL 这类噪音。
条目类型是新增的 `forum`，正文结构为「元数据引用块 + `## 讨论总结` +
`## 正文` + `## 讨论（N 条）`」，三个小节标题同时是详情页分段锚点
（`shared/utils/forum-note.ts`），改一处要同步另一处。元数据两行必须紧邻，
中间空行会让 `parseVideoMetaBlock` 只吃掉首行、把「发布」漏进正文。
讨论总结（`import/forum-summary.ts`）与视频总结分开：口播稿顺着讲一遍即可，
论坛帖要按方案聚类、统计支持人数、交代共识与分歧，走 mainText 路由，
未配置模型时静默跳过并在正文注明。排版协议是 `### 小标题 + 「- 」列表`，
`sanitizeForumSummary` 会把模型吐出的 `#`/`##`/独占一行的 `**加粗**` 统一升成 `###`：
`##` 会和 `## 正文`/`## 讨论` 这两个分段锚点撞车；而「**标题**」独占一行时，
Markdown 只认单换行会把它和下一行正文渲染进同一段，页面上就是
「**方案名** 多人推荐此方案…」黏成一坨（v0.6 实际踩到过）。
提示词管不住模型排版，这层确定性清洗不能省。
论坛条目默认沿用原帖标题，不像视频那样一律 AI 拟题——帖子标题是人写的，
通常就是问题本身，改写只会丢掉可辨识度。只有 `needsAiTitle` 判定标题压根没
描述内容时（剥掉「求推荐」「在线等」这类套话后不剩几个字、或抓取兜底的
`V2EX 帖子 123456`）才让模型重拟，拟题指令追加在讨论总结的提示词末尾，
不额外发一次调用；换掉的原标题经 `appendOriginalTitleNote` 记进元数据引用块，
来源 chip 仍显示得出、全文检索仍找得到。
详情页可以重新生成讨论总结：复用 `media:summarize` 频道，主进程按 `itemType`
分派到 `regenerateForumSummary`。素材用 `parseForumReplies` 从库中正文还原逐楼
回复，不重新抓网页——原帖可能已删或又多了几十楼，条目自己那份才与用户看到的一致。
回写走 `upsertForumSummarySection`，顺带清掉采集期留下的「未配置文本模型」
「生成失败」注记，免得和新总结自相矛盾。
没做通用论坛协议：Discourse 的 `/t/{id}.json` 在 linux.do 会被 Cloudflare
挡成 403，逐站适配是唯一可行路径，`detectForumPlatform` 因此是白名单式判定。

抖音不走 yt-dlp：yt-dlp 的 Douyin 提取器打 `douyin.com/aweme/v1/web/aweme/detail/`，
该接口对没有签名 cookie（`__ac_signature` / `ttwid`，由页面 JS 挑战生成）的请求
返回空 body，报「Fresh cookies are needed」。`import/douyin.ts` 改走
`iesdouyin.com` 的分享页——移动端 UA 下服务端渲染，作品信息全在
`window._ROUTER_DATA`，播放地址 `playwm` 换 `play` 即无水印源，全程无需 cookie。
桌面 UA 会被 302 到 `douyin.com`，UA 是这条路的前提，别改。
图文作品走 `import/douyin-note.ts`：配图入资产库 + 逐图 OCR（`media/ocr.ts`
读 visionText 路由，与渲染进程共用 `shared/utils/ocr-request`）。判定图文只看
`images` 字段——抖音给图文也生成了 play_addr（图片合成的幻灯片视频）。

待办：平台登录采集（cookies）延后；macOS 平台暂不考虑。

已知的技术债：
- 语义检索仍是全量余弦扫描，没有 ANN 索引。已改为分批取用并在批间让出
  事件循环（不再阻塞主进程），但总耗时随索引规模线性增长。
- embedding 仍按 OpenAI 请求响应格式硬编码（Anthropic 有防护会明确报错，
  Gemini 走 OpenAI 兼容层）。OCR 已适配 Anthropic，转写在不支持的协议上
  会给出可读提示而非撞 404。
- `ai:httpRequest` 只做到「已配置 host + 回环放行、未知目标限速」，没有
  真正的端点白名单。彻底收敛需要把连接测试与模型列表拉取搬进主进程。
- 主密码功能没有任何内容接入（`encryptText` / `decryptText` 零调用方），
  设置页已如实说明，但功能本身仍是半成品。
- 媒体文件按内容哈希去重尚未实现：同一文件重复导入会产生多份磁盘拷贝
  （删除条目时会回收，但导入期间不去重）。
- `import/douyin.ts` 依赖未公开的页面结构（历史上叫过 `RENDER_DATA` 且是
  URL 编码的），抖音改版就要跟着修。小红书仍走 yt-dlp，同样会被登录墙拦下。
- 图文采集的 OCR 按张调用视觉模型，默认上限 9 张（`OCR_IMAGE_LIMIT`），
 超出的图片只入库不识别。上限是硬编码常量，没有做成设置项。
- 论坛采集只认 V2EX。超长帖的总结按回复分块，上限 8 块（`MAX_CHUNKS`），
 再长的部分不进总结素材，但回复本身完整入库。
- 文字稿排版的超时与预算都是一刀切的常量，没有按模型类型区分：思考类模型
 和普通模型的耗时差一个量级，前者需要 240 秒，后者十几秒就够，现在两者
 共用同一套阈值。`fastText` 路由在设置页写的是「低成本对话模型」，但没有
 任何地方拦着用户把思考类模型挂上去，也不会提示这样会慢很多。
- 专名表只覆盖标题与简介里出现过的拉丁词：纯口播提到、元数据没写的术语帮不上，
 中文专名被听错也不在射程内（实测中文是逐字正确的，暂时不是问题）。术语上限
 40 条、简介只取前 500 字，都是硬编码常量。
- 正文配图只适配了 OpenAI 的 `/images/generations` 与 Gemini 的
 `generateContent` 两条路，Anthropic 无文生图 API（给可读提示而非撞 404）。
 图上的中文标注全靠模型自己写，老一代模型会出错字，预设把标注数压到 4~6 条
 就是为了少出错；应用不做自动 QA（回看一眼要再花一次视觉模型调用），
 不满意只能手动「换一张」。风格预设文件被改坏时静默退回内置预设并只打一条
  warn，界面上看不出来。配图不计入 `ai_usage_daily`——`recordAiUsage` 只有渲染
  进程那几条链路在调，主进程的总结、转写、OCR 同样不记；配图是其中最贵的一项，
  这个缺口现在更值得补上。生图的重试次数不进 `IllustrationProgress`：快速失败
  只多花几秒，界面上看不出区别，但撞上 240 秒超时再重试一轮时，面板会在
  「正在生成第 N/M 张」上停留好几分钟而不给任何解释。

fork 遗留的 WebDAV / S3 同步通道已在 v0.4.1 整体删除（主进程 transport、
preload 白名单、settings 的 36 个字段与 69 条 i18n 文案）。归知目前不提供
多设备同步，跨设备走备份文件或 Markdown 导出。

v0.4.0 发布时重置了 git 历史（孤儿提交），PromptHub 的原始提交与
`prompthub-v0.5.9-base` tag 不再在本仓库中，需要时从上游
[PromptHub](https://github.com/legeling/PromptHub) v0.5.9 查阅。

## 常用命令

```bash
pnpm electron:dev        # 开发（Vite HMR + Electron）
pnpm typecheck           # TS 全量检查
pnpm test:unit           # vitest 单测
pnpm lint                # eslint + 文件行数门禁
pnpm electron:build:win  # Windows 打包
```

## 编码约定

- TreatWarnings 严格：eslint `--max-warnings 0`；文件行数上限见
  `config/file-line-limit-baseline.json`（`pnpm lint:file-size`）。
- 生成的代码注释与日志用中文（可中英双语），标识符用英文。
- 渲染进程不得直接访问 Node API；一切系统能力经 preload 白名单。
- 界面里不出现原生控件。`<select>` 的下拉列表、`<input type="color">`、
  `alert/confirm/prompt` 都由操作系统绘制，CSS 完全够不着，一律换成
  `components/ui` 下的 `Select` / `ColorPicker` / `ConfirmDialog`；eslint 的
  `no-alert` 与 `no-restricted-syntax` 会挡住回潮。滑块、数字步进箭头、搜索框
  清除叉这些浏览器自绘的默认外观在 `globals.css` 统一收敛。`title` 提示由
  `ui/TooltipLayer` 全局接管（悬停时摘走 `title` 自绘、移开再装回），
  所以写 `title` 无需改动即可获得主题化气泡。
- `title` 必须给出元素自己没写出来的信息。纯图标控件该写（气泡是它唯一的名字）；
  气泡内容与可见文字不同的该写（绝对时间对相对时间、URL 对显示名、置灰原因、
  快捷键）。而 `title={x}` 配 `{x}` 这种把同一句话再念一遍的一律不写——即便
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
- 机密（API Key）不写入 localStorage 之外的明文位置；导出功能不得携带机密。
