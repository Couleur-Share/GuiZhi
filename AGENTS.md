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
（`import/video-url.ts`，B站/YouTube，未安装时降级）。抖音与小红书例外，
见下方「抖音不走 yt-dlp」「小红书不走 yt-dlp」。
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

侧栏「平台」分区（知识库与标签之间）：按采集来源分组，抖音 / 哔哩哔哩 / 小红书 /
YouTube / V2EX 各一行，外加网页与本地文件两个兜底桶——少了后两个，通用抓取与本地
导入的条目在这个分区里一条都点不到。判定收敛在
`shared/utils/source-platforms.ts` 的 `resolveSourcePlatform`，采集落库
（`import-service.ts` 写 `source_records.platform`）与老库回填（迁移
`0009-source-platform`）共用它，且它内部直接复用连接器分流用的
`detectVideoPlatform` / `detectForumPlatform`：采集当时走了哪条抽取路径，事后回填
就归到哪个平台，两边算不出不同的结果（B 站专栏页不是视频页，两边一致地落进网页桶）。
该函数回传的是这两个检测函数的枚举值而非就地写死的字面量，新增平台时忘了加进
`SOURCE_PLATFORMS` 会直接编译不过。
`platform` 这一列建表时就在，但采集管线从来没写过，全库皆为 NULL；唯一写过的是旧版
.NET 迁移，落进去的是老应用自己的一套取值。回填因此是**全部重算**而不是「只补
NULL」：留着老取值会在分区里多出几个用户认不出来的分组，而这一列此前没有任何读取方，
重算不弄丢任何在用的数据。来源是 1:N（旧版迁移可能给同一条目带进多条记录），
过滤用 EXISTS、计数用 `COUNT(DISTINCT item_id)`，换成 JOIN 会让条目在列表里重复
出现、总数虚高。分区只列有条目的平台且按常量表的固定顺序排（不按数量），
从不用抖音的人不必盯着五个 0 找自己那一行，采集一条也不会让侧栏重排。
平台是派生分组、不可增删改名，所以行上没有「更多」菜单；它与知识库、标签同为互斥的
导航轴，四条轴的复位逻辑收在 store 的 `navigateTo` 里（此前三处各抄了一遍）。
表格视图另有一列「来源」（默认显示，紧挨「类型」）：光有筛选轴不够，在「全部」
视图里仍然分不出哪条来自哪儿。它需要 `platform` 进 `KnowledgeItemListEntry`，
由 list() 的相关子查询带出（与 get() 取 source_uri 同一形状，走 `idx_sources_item`）。
查表用 `getSourcePlatformMeta` 而不是直接索引：取值来自数据库，新版本写入的平台
在旧版本里查不到，`getItemTypeMeta` 当年就是为这个加的兜底——那一次抛异常的后果是
整个知识库列表白屏。
这两处的图标是各平台的真品牌 logo（`ui/PlatformLogos.tsx`），不是形态近似的通用
图标。此前抖音取音符、B 站取电视、小红书取书，得先读懂旁边的文字才对得上号，
等于没帮上忙，而这一列的全部用处就是不看文字也能扫出哪行是哪个平台。
路径取自 Simple Icons v16（CC0-1.0，商标归各平台，NOTICE 里有声明），**内联**而不
装 `@icons-pack/react-simple-icons`：只用得上五个 glyph，为此引一个 3400 图标的依赖
不划算，何况这里要按主题换色，包给的组件也帮不上忙。lucide 靠不住——v1 已经把
全部品牌图标删了（法务压力），官方迁移指南指向的正是 Simple Icons，我们那个
`YoutubeIcon` 是锁在 0.460 才还在。
抖音用 TikTok 的音符：字节这两个产品共用同一个符号，Simple Icons 也只收了一份
（没有 douyin slug），不是拿相近的东西凑数。
着色分两类，分界不是好看不好看而是看不看得见：有彩色标准色的写品牌色（`text-[#…]`，
刻意不走语义令牌——品牌色随主题漂移就不再是那个平台的颜色了），抖音（#000000）与
V2EX（#1F1F1F）的标准色是近黑，写死会在深色主题下整个消失，改用 `text-foreground`
跟着主题走。着色 class 与图标绑在一起由 `PlatformIcon` 拼，调用方只管尺寸：分散到
各处自己拼的话，漏掉 colorClass 的表现是那一个平台悄悄退回黑白，不报错也没人发现。
svg 的 viewBox 从 `0 0 24 24` 撑到 `-2 -2 28 28`，是给图形补出 lucide 那样的内边距——
Simple Icons 是实心填充且铺满画布，与描边图标同尺寸摆在一起明显显大显重，同一列里
就参差不齐了。
两个已知的取舍。其一，小红书的官方 mark 是「小红书」三个字的艺术字，16px 下必然糊成
一个红团（实测过 14/16/18/20px 与「品牌色底 + 白 glyph」的方块式，都救不回来，方块式
还让 glyph 更小、且深色下抖音的黑底会融进背景）；留着它是因为颜色对、旁边有文字，
仍严格优于一个通用的书本图标。其二，兜底桶（网页 / 本地文件）保留 lucide 描边图标并
继承行的文字色，比品牌 logo 淡一档——这正合它们「归不了类才落这儿」的地位。
表格「来源」列默认宽度随之从 96 提到 108：多出 logo 那 14px，96 会让「哔哩哔哩」
默认就被截断。

知识库图标从 11 个扩到 80 个，按用途分八组落在
`library/collection-icons.ts`，选择器是 `CollectionIconPicker.tsx`。分组名按
「用户拿知识库装什么」切（学习 / 工作 / 技术 / 创作 / 生活 / 财务 / 旅行 /
标记），不按 Unicode 自己的表情/物件/符号分类——后者是给输入法用的，在这里
等于没分。做成一条带小标题的滚动列表而不是分类标签页：「🎓 算学习还是生活」
只有作者自己清楚，逼用户先猜对分类才看得见图标，比一片平铺还慢；小标题纯粹
是扫视辅助，猜错了往下看就行。每组十个，一组正好一行，这是弹窗从 sm 提到 md
的唯一理由（sm 放不下十格，会折成参差的两行；只改名字的标签仍走 sm）。
只收 Unicode 11 及以前的老码位：新 emoji 在 Windows 10 的 Segoe UI Emoji 上
是豆腐块，而这一格的全部用处就是一眼认出来。
两条改动约束有测试挡着。其一，v0.6 那 11 个预设必须留在表里——删掉哪个，正用着
它的知识库在选择器里一格都不亮，用户看到的是「图标没了」而库里的值其实还在。
其二，全表不得有重复，重复的那个被选中时会有两格同时高亮。
`collections.icon` 是自由文本，旧 .NET 库迁移进来的取值与这份目录没有交集，
所以目录外的当前图标要单独摆一格并显示为已选。这一格记的是**打开弹窗时**那个
值而不是当前值：跟着当前值走的话，点一下别的图标它就消失，原图标再也选不回来。
重命名时还要把已选的那格滚进视野（八组八行，选中的多半在框外，不滚看到的
同样是「什么都没选」）；用容器坐标手算而不是 `scrollIntoView`，后者会连带
滚动弹窗自己的滚动容器。

条目详情头部（`ItemDetailHeader.tsx`）：标题独占一行，动作区并进下面那排元信息
chip 的右端——与 `WikiPageDetail` 同形态。此前标题与五个图标同排，动作区固定
184px，而卡片视图的详情栏常在 390px 上下，一行标题只剩七八个字，长标题撑满三行
还要被 `TITLE_MAX_HEIGHT_PX` 截掉后半截。动作区下移不新增一行高度：chip 本来
就会换行，让出的宽度由它们吸收。
留在外面的只有「复制给 AI」与收藏，置顶 / 归档 / 移到回收站收进「更多」菜单
（`MoreActionsButton`）。这一步不是顺手做的：只搬不收的话动作区仍占 144px，
chip 会碎成四行，头部反而比改之前更高（实测 390px 下 4 行 chip vs 3 行）。
菜单按钮贴着面板右缘，锚点必须按 `rect.right - MORE_MENU_WIDTH_PX` 右对齐，
否则先开到窗口外、再被 ContextMenu 的越界回弹拽回来，闪一下。
置顶与归档随之改由 chip 表达：状态不能只活在菜单项的文案里。归档态原本仅靠
`ArchiveIcon` / `ArchiveRestoreIcon` 两个长得极像的图标区分，本来就看不出来。

逐模型采样参数（「高级参数」：temperature / maxTokens / topP / topK / 两个 penalty /
stream / enableThinking / customParams）在 v0.13 整个删掉了，别再加回来。它以
`AIModelConfig.chatParams` 的形态存在过，UI 是设置页「模型配置」下的第三个面板加
模型编辑弹窗里的折叠区。删的理由是它**从来就没真正生效过**，而一个半通的旋钮比没有
旋钮更坏：八个字段里，temperature 与 maxTokens 被每个场景显式传参覆盖
（摘要、标签 0.3、Wiki 编译 0.2、问答各有各的值，`options?.x ?? chatParams?.x` 的
右边永远轮不到），stream 由「调用方给没给 `onDelta`」决定，enableThinking 被
`runScenarioChat` 写死成 false；剩下 topP / topK / 两个 penalty / customParams 只在
「OpenAI 兼容或 Gemini 协议」且「走渲染进程那四个场景」时才进请求体——anthropic
分支另起一个请求体，连 temperature 都不发，而主进程那整条链路（排版、内容总结、
拟题、论坛总结、配图策划）压根不读它，`packages/core` 的 `normalizeModelConfig`
逐字段重建对象、写盘时就把它丢了。
真要重做，先解决那个绕不过去的设计问题：场景的 temperature 是有意调的（Wiki 编译
要稳定、标签要收敛），用户配的值该不该盖掉它？而表单给每个字段都铺了默认值，
分不出「用户没动过」与「用户就是想要 0.7」，`??` 兜底那套写法在这里必然失效。
需要给单个模型塞特殊请求字段（`reasoning_effort` 这类）的话，`customParams` 那个
逃生舱也一并没了，得在 `services/ai.ts` 的 `chatCompletion` 里按模型名硬编码——
`enable_thinking` 已经是这么处理的。

AI 用量记账覆盖主进程：`main/services/ai-usage.ts` 的 `recordMainAiUsage`。
此前 `recordAiUsage` 只有渲染进程那三条链路在调，主进程的配图、总结、排版、转写、
OCR 一条都不记——而配图按张计费，是全应用最贵的一项，面板给出的不是「少一点」
而是把大头整个漏掉，比不显示更误导。
记账绝不能连累主流程：句柄走 `tryGetDatabase()`（新加的非抛出版本），拿不到就静默
跳过。「库还没初始化」与「写入失败」必须分开——前者在单测和备份恢复期间都是常态，
用 `getDatabase()` 会抛，而按错误文本去认它太脆；不分开的话，一篇长稿的三十多个
排版块会往控制台刷三十多条无意义的 warn。
记账的位置有讲究：生图按「一张图」记而不是按「一次 HTTP」记，重试只发生在
5xx / 429 上，那些请求没出图也不计费，按尝试次数记会凭空放大账单；排版反过来，
请求成功就记，哪怕验收判它不合格——钱已经花了，「重试一次」花的是第二笔。
内置本地转写引擎（`isManagedFunasrUrl`）不记：它跑在用户自己的 CPU 上，
记进面板等于报出一笔不存在的开销。
`AIChatResult` 补了 `usage`，此前主进程这条链路把 token 丢在 `chatCompletion`
里没带出来，一篇长稿只能显示「32 次 · 0」。解析放在 `ai-protocol.ts` 的
`extractUsageFromChatResponse`，与旁边的文本提取同源——字段名逐协议不同
（anthropic 是 input/output_tokens，gemini 原生是 usageMetadata），取错的表现是
恒为 0，而恒为 0 在面板上和「接口不回报」长得一模一样。
用量场景词表因此从 settings 的 `AIUsageScenario` 拆了出来，落在
`shared/types/ai-usage.ts` 的 `AI_USAGE_SCENARIOS`：那个类型的含义是「用户可以为
其单独指定模型的场景」，而新增的排版 / 配图 / embedding 各自绑死在一条路由上，
做成可指定模型没有意义，但它们实实在在地花钱，面板上必须有一栏。
顺带修了一个一直在的缺陷：`ai.ipc.ts` 的 usage handler 从来没把 `failed` 转给 DAO，
于是 `failed_calls` 列、DAO 入参、迁移 0005 三样都齐备，统计出来却恒为 0。

媒体资产按内容哈希去重：`saveMediaAsset` 的文件名从随机 UUID 改成
`computeFileHash` 的前 16 位（`content-hash.ts` 里新增的**流式**二进制哈希，与
旁边那个文本哈希是两码事——文本那个会压空白、小写化，拿去处理二进制直接改坏字节，
何况视频上限 1GB，一次性读进内存不可行）。名字形状不变，协议解析、清理白名单、
`gen-` 前缀的区分都不受影响。落盘用 `COPYFILE_EXCL` 而不是先 access 再 copy：
两个导入任务并发落同一份文件时，先查后写会撞在一起，EEXIST 直接当成命中已有资产。
共享资产不会被误删——清理判据本来就是「全库正文里还有没有人引用这个文件名」
（`listReferencedAssets`），`asset-cleanup.test.ts` 早有一条用例锁着这个性质。
一个连带的行为变化值得知道：资产名确定之后，同一文件重复导入产出的**正文逐字相同**，
`computeContentHash` 随之命中，队列会把第二次标成「重复」而不是再建一条条目
（此前资产名随机，两次正文必然不同，永远判不出重复）。确实想要两条的人走
「仍要创建副本」，两条条目共用一份资产文件，引用判定照常成立。

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
视频简介里的链接单独占元数据引用块的一行（`> 相关链接：`），从**未截断**的
简介原文里抠（`shared/utils/url-text.ts` 的 `extractUrlsFromText`，那组尾部修剪
规则原本长在采集框里，主进程要用才上提到 shared——分裂成两份迟早只修好一份）。
简介写进正文时压成单行并截到 300 字，作者放在末尾的仓库地址、文档链接就这么没了，
而那恰恰是「这条视频能不能直接用上」最关键的东西：实测一条讲 Agent 的视频，作者
反复说「源码和配套文档都在下面」，记录里一个链接都没有。上限 5 条，抖音的推广
文案能堆十几个短链，全列进去反成噪音。链接行必须仍在那个连续的引用块内——
`parseVideoMetaBlock` 只吃连续的 `>` 行，中间夹一个空行就会让解析提前截断
（有回归用例锁着）。

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

「探测不通」不等于「服务没了」，批量导入撞的就是这一条。健康探测打的是
`/v1/models`，而服务脚本的 `generate()` 是阻塞调用，一条转写跑着的时候端点一概
不应答，三秒探测必然超时；队列并发是 2，后一条任务据此判定服务挂了，再拉一个
python 撞端口占用退出，抛出一句「本地转写服务启动失败」。实测过一批抖音视频：
前一条 15:36:52 入队跑了 8 分 26 秒，后一条 15:37:57 入队 41 秒就「完成」了，
正文里只有元数据加一行报错。`ensureFunasrService` 因此在探测失败后先问一句端口
有没有人监听（`isFunasrPortListening`）：TCP 握手由内核完成、不需要应用层 accept，
实测阻塞中的服务 1ms 就答得出来，而同一时刻的 HTTP 探测 3016ms 超时——这一毫秒
与三秒之差就是「在忙」与「没了」的分界。占着就认下这个实例。判端口与设
`startingPromise` 之间不能有 await，否则并发调用会各自穿过检查、各拉一个进程。
认下之后必须同时把转写请求串行化（`runExclusiveLocalTranscription`）：服务端本来
就一次只做一条，客户端不排队的话，后到的请求会带着自己那 10 分钟的超时预算干等
前一条做完，失败看着像「转写超时」、实际是排队排掉的。超时信号因此在拿到锁之后
才创建，而不是在函数入口。
启动失败的报错也得能看。原来取 `logTail` 的最后 300 字，而 tqdm 用 `\r` 原地刷
进度、一秒几十次，切出来整段都是 `rtf_avg: 0.043: 100%|███| 1/1 [00:01, 1.28s/it]`；
更糟的是旧进程的 stdout 监听器从来没摘过，`logTail` 重置后仍在灌前一条任务的推理
输出，真正的原因（端口占用）连一个字都没留下。`attachLogging` 换进程即摘旧的，
`summarizeProcessLog` 剥掉 ANSI、每行只取最后一次回车之后那一段、丢掉进度条再取
末尾几行；子进程另带 `PYTHONIOENCODING=utf-8`——不指定的话 Windows 上 Python 按
ANSI 代码页写管道（简中是 cp936），这边按 UTF-8 解出来是一片乱码。

导入任务因此多了一个 `warning`（`import_tasks.warning`，迁移 0010），与 `error`
的分工是「条目在不在库里」：`error` 意味着没做成、重试是唯一出路，`warning` 的条目
已经在库里、只是少了最值钱的那部分。转写失败不阻断导入是对的（元数据齐全的视频
仍然有用），但那行注记此前只写进正文，列表上照旧是一枚绿色的「已完成」——用户点开
才发现是空壳，一批三十条里还挑不出是哪几条。现在徽标换成琥珀色的「完成（有缺失）」、
原因写在行里，侧栏「有缺失」单列一档：它是「已完成」的**子集**而不是另一种终态，
两档重复计数，所以各档之和不等于「全部任务」。重试要连 `warning` 一起清，否则上一轮
的提示会赖在这一轮成功的任务上。转写失败同时进 `error.log`——此前只有排版失败在记，
而排版失败顶多让稿子难读，转写失败掏空的是整条条目。

主进程的 `chatCompletion` 给 Qwen 系补上 `enable_thinking: false`。Qwen3 系是混合
推理模型、默认开思考，而主进程这几条链路（文字稿排版、内容总结、拟题）一律非流式，
思考过程既读不到也用不上，纯粹白烧 token 和时间：实测给 1600 字文字稿补标点分段，
思维链能写到两万字、单块 77 秒（还撞到过 300 秒不返回），关掉后 10 秒，输出反而
分得更细。渲染进程那份客户端（`renderer/services/ai.ts`）早就这么做了，
`packages/core` 这份从 fork 剥出来的精简实现一直漏着——排版占掉一次导入 79% 时长
的根因就在这里，`CHUNK_TIMEOUT_MS` 从 120 提到 240 秒治的是它的症状。
判定按模型名与 provider 两头认（与渲染进程同一套）：中转站的 provider 一律是
custom，模型名是唯一可靠的线索，而 dashscope 上的部署又常被改成不带 qwen 的名字。
anthropic 协议不带这个字段，那边是另一套请求体形状——中转站上挂着 qwen 名字的
anthropic 端点是真实存在的，只看模型名会栽。
端点不认识它时**摘掉重发一次**，判据是「报错文本里点名了这个参数」而不是状态码：
官方 OpenAI 对未知字段回 `Unrecognized request argument supplied: enable_thinking`，
各家中转站措辞不一但都会带上字段名；按状态码判会把限流、余额不足一并卷进来白重发
一次，而那些错重来一万次也是同一个结果。重发仍失败时抛第二次的原话，不能粉饰成
参数问题——真正的原因（模型不存在、鉴权不对）才是用户能据此行动的东西。

阶段耗时与 AI 开销（`import_tasks.stage_stats`，迁移 0011）：一次导入里排版占掉
79% 的时长，而这件事只能靠翻数据库和 grep 日志才发现——`ProgressHint` 在非运行态
直接 `return null`，任务一结束耗时信息全部消失，连总时长都得拿两个时间戳的气泡去减。
落点是任务行本身而不是「每次导入生成一份报告」：32 条任务就是 32 份没人会打开的
文件，而用户的原话恰恰是「在使用时没发现」，需要主动打开才看得见的东西治不了这个。
行上常驻「共 X · 最慢 Y」供扫视，完整明细在详情弹窗里。
明细的主体是**一条堆叠时间条**而不是一列数字：阶段是顺序的、各段之和恰好等于总耗时，
所以堆叠条就是这条任务的时间线，段宽即占比。九行数字要逐个比大小才知道谁最贵。
颜色只承载一个信息——哪段最慢（琥珀，与「完成（有缺失）」同一套语言）；其余相邻段
交替两档 `primary` 透明度只为分开边界，条与图例的对应关系靠**位置**不靠颜色，
所以不需要九种可分辨的色值，也就不必绕过语义令牌。
四舍五入到 0:00 的阶段折成一行「另有 N 个」（可点开看全部）：单独占行是纯噪音，
一字不提又像漏了阶段。图例整块共用**一个** grid、逐行只吐单元格——每行各自成 grid
的话名称列的 `auto` 按各自内容取宽就对不齐，而给死宽度在中英两种语言下必有一边难看
（中文四到六个字，英文最长的 `Extracting text from images` 有 27 个字符）。
详情弹窗（`ImportTaskDetailModal`）由**工作区持有单例**、按 id 取任务，不是每行挂一个：
一屏几十行意味着几十份 toast 与 store 订阅，而同时只可能开一个；按 id 而不是存快照，
任务还在跑时弹窗才跟着更新。入口有两个——耗时摘要，以及动作条上常驻的「任务详情」，
后者不能省：失败与取消的任务多半没有耗时摘要可点，而它们恰恰最需要看清楚。
「复制诊断信息」把这些一次性带走（`import-task-report.ts`，纯函数好测）。它与界面显示
有一处**故意的不同**：报告列出全部阶段、不折叠亚秒的那几个——折叠是为了扫视时的信噪比，
而报告的读者是排查的人或模型，少一行就是少一条线索。取不到应用版本时省掉那一行而不是
留一个空的「应用：」；单元格里的 `|` 要转义，否则表格被从中间切断。
写库时机是白捡的：`updateAndNotify` 是全部 12 个阶段的唯一收口且本来就在写库，
统计搭同一次 UPDATE 走，零额外 SQL 往返；字段挂在 `ImportTask` 上，`import:list`
与 `import:changed` 自动带过去，不新增 IPC。
**记结果不记参数**：一次 1600 字排版输出 1570 字却烧掉 8271 个 completion token，
这个比值自明；而记一栏「思考：已关闭」只说明我们发了什么，说明不了模型做了什么
（实测 deepseek 收下 `enable_thinking:false` 照样吐 reasoning），何况参数是上一个
缺陷的形状，防不住下一个。**不判快慢**：什么叫慢取决于模型、网络与内容长度，
写死阈值在慢渠道上天天误报，与「不给第 N 步 / 共 M 步」同一条理由。
AI 调用归属走 `AsyncLocalStorage`（`main/services/ai-call-context.ts`）：队列并发是
2，模块级的「当前任务」变量必然串账；而显式穿参要经 extract → connectors →
video-url → transcript-format 四五层、再乘视频 / 图文 / 论坛三条子链，那些层没有
一层关心记账。汇报点只有 `recordMainAiUsage` 一处（它本就是主进程 AI 调用的唯一
收口），且必须排在 `tryGetDatabase()` 之前——任务侧的统计只写在任务行上，不该因为
`ai_usage_daily` 不可用而跟着丢。`ai_usage_daily` 帮不上忙：主键是
`(day, scenario, model)`、写入即聚合，任务维度在落库那一刻就被抹平了。
重试必须清空这一列：队列排满时任务会长时间停在 pending，挂着上一轮的数字就是骗人。

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

专名表除了标题与简介抓来的，还常驻一份通用技术词
（`COMMON_TECH_TERMS`，两份分开列而不是合并——模型得分得清哪些是作者真说过的
专名）。加它的依据是一次 MCP 消费侧实测：一条讲 Agent 的抖音视频，**毁得最狠的
恰恰是没写进标题的通用词**——SQL 听成 `circle`、function call 成了「方声扣」、
Python 成了「拍摄」（"很多同学擅长的其实是拍摄"）、Claude Code 成了 `C code`、
schema 成了 `sma`/`scama`、harness 成了「哈尼斯」，甚至混进了日文假名。而项目名
`pi-agent` 反倒保住了，因为它写在简介里。词表收得克制：只放「几乎必然出现 +
音译后完全不可读」的那批，泛用词（data、model、user）不收——它们即便听错也还原
得出来，反倒容易误伤。这段指令此前只在抓到特有专名时才附，现在总是给。
排版的删减范围同时收窄了一次：只删**不含信息**的寒暄套话，并明写「提到资源、
链接、版本、后续计划的句子一律保留」。结尾那段「源码在 GitHub / 我做了 Python 版
/ 有 PDF 版」看着像推广，实际是全篇最能直接用上的信息；而且 `rejectFormattedChunk`
卡着 `ratio < 0.65`，真去裁整段推广会触发「输出长度异常」→重试→仍不合格→退回
原文，白花一次调用。

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
判定图文只看 `images` 字段——抖音给图文也生成了 play_addr（图片合成的幻灯片
视频）。图文的条目组装走下面那份两个平台共用的 `import/image-note-entry.ts`。

抖音评论不采（查过了，别再查一遍）。动机是充分的——作者常把资源链接放在评论区，
简介里反而没有，而正文里一句「链接在评论区」对读到的人是条死路。但四种 UA 打同一个
`douyin.com/video/<id>` 实测下来，只有一条路走得通：自报家门的 `GuiZhi/0.11.0`、
普通 Chrome、移动端 Safari 三者拿到的 HTML **字节数完全相同**（72,914），
都不含评论；只有 Googlebot UA 多吐出一段 `application/ld+json`（86,192），
里面是 Schema.org 标准格式的 `{"@type":"Comment", text, creator, interactionStatistic}`。
现走的 `iesdouyin.com` 分享页也补不上：`item_list[0]` 里 `comment_list` 这个字段
确实在，但值恒为 `null`（同一份数据里 `statistics.comment_count` 是有值的，78），
所以这不是「多读一个字段」而是「换一条抓取路径」。
不做的理由不是技术上做不到，而是这与上面那条移动端 UA 性质不同：移动端 UA 声明的是
「我用手机浏览器访问」，那是一个真实存在的客户端形态，没有谁被冒名；Googlebot UA
声明的是「我是 Google 的爬虫」，抖音给它多吐数据是基于「这是 Google、会带来搜索
流量」的商业约定，冒用它是拿一个不属于我们的身份换特权访问，而反查来源 IP 是否落在
Google 的地址段是识别这件事的标准手段。归知是公开发行的软件，被识破的代价是 IP 被封、
连带现有抖音采集一起挂掉，收益却只是偶尔多捡到一个链接（触发这次调研的那条视频，
评论区恰恰没有链接，作者说的是「发太多疑似限流了」）。
小红书不走 yt-dlp：yt-dlp 的 XiaoHongShu 提取器只出视频 formats，而小红书的
主体内容是图文笔记（`type: "normal"`），那条路对图文一律报「No video formats
found」；它的 `_VALID_URL` 也不认 `xhslink.com` 短链，分享口令里的链接直接落空；
何况 yt-dlp 默认还没装，绝大多数人第一次采集拿到的是「未安装 yt-dlp」。
`import/xiaohongshu.ts` 改走笔记页自己的服务端渲染数据
（`window.__INITIAL_STATE__` 的 `note.noteDetailMap[id].note`），无需 cookie。
三个约束都是实测出来的，改动前先想清楚：
其一，**必须桌面 UA**。移动 UA 拿到的是个空壳页面（14 万字符、没有
noteDetailMap），与抖音正好相反——所以两边的 UA 各钉各的，别图省事合并。
其二，**必须保留 `xsec_token`**。去掉当场 302 到站内 `/404`，笔记数据一点不给，
所以采集用的链接只能是分享面板复制出来的那个。而 404 页**同样带
`__INITIAL_STATE__`**，不先按 finalUrl 认出来，报错就会变成含糊的「没有笔记
内容」——`fetchXiaohongshuNote` 因此先判 `/404` 再解析，报错里点名 xsec_token。
其三，`__INITIAL_STATE__` 不是合法 JSON，里面有几十处裸 `undefined` 字面量。
`sliceInitialStateJson` 在按花括号配平切的同时，只把**字符串外**的换成 `null`：
整段无脑替换会把文案里的 `undefined` 一起改掉，而这是个知识库应用，正文里出现
这个词再正常不过（有回归用例锁着）。
`sourceUri` 收敛成不带 token 的 `explore/<id>`（og:url 就是这个形态）。
`normalizeUrl` 不认识 `xsec_token`，留着它同一条笔记分享两次就是两条条目。
代价是在没登录的浏览器里点开会 404，但去重比这个重要。
配图的扩展名**按文件头判**（`sniffImageExtension`）而不是按 URL：小红书的图片
地址结尾是 `!nd_dft_wlteh_jpg_3` 这样的处理指令，压根没有扩展名，`imageExtensionFromUrl`
会一律回落到 `.webp`；而 OCR 的 mime 是 `imageMimeFromFileName` 按扩展名推的，
猜错等于给视觉模型送一张标着 webp 的 JPEG，Anthropic 会直接判非法。
话题在文案里的原始形态是 `#AI漫剧[话题]#`，`cleanNoteText` 还原成界面上的
`#AI漫剧`，否则一篇笔记结尾拖着十几个 `[话题]#`。
视频笔记的 `imageList` 装的是封面，所以图文/视频按平台自报的 `type` 分，不按
「有没有配图」分——按图片数判会把每条视频都判成图文。播放地址取
`video.media.stream.*[].masterUrl` 加 `backupUrls`，逐个降级。备源不是可有可无的
冗余：主源带 `sign` 与到期时间戳 `t`，备源（`sns-bak-*.xhscdn.com`）是裸地址，
签名过期时就靠它。
文案里的 `[微笑R]` `[举手R]` 是小红书的内置表情占位符，原样保留——映射成真表情
要一张几百条的对照表，而按 `[…R]` 的形状去删是在拿用户正文赌一个没验证过的
命名空间。

图文条目的组装（配图入资产库 + 逐图 OCR）两个平台共用
`import/image-note-entry.ts` 的 `buildImageNoteEntry`：平台差异只剩三处，
全部收在 `ImageNoteSource` 里——正文写哪个平台名、单张图怎么下（UA / Referer /
扩展名判定各不相同）、以及标题要不要 AI 重拟。最后那个 `authoredTitle` 不是
可有可无的开关：小红书有独立的标题字段，那是人写的，重拟只会丢掉可辨识度
（与论坛条目沿用原帖标题同一条理由）；抖音没有标题字段，所谓标题只是文案首行、
往往是半句话，才需要重拟。小红书的标题字段可以为空，此时退回文案首行并把
`authoredTitle` 置 false，重新走上抖音那条路。

分享口令（「0.02 复制打开抖音，看看【…】 https://v.douyin.com/xxx/ :3pm 12/15」）
整段粘进快速采集框就能采：`capture-utils.ts` 的 `extractUrlsFromText` 把链接抠
出来。链路下游本来就通——`v.douyin.com` 命中 `detectVideoPlatform` 的
douyin.com 子域分支，`fetchDouyinAweme` 也早有跟随短链重定向拿 aweme id 的路径，
断的只有第一道闸：`parseCaptureDraft` 要求**每一个**空白分隔的 token 都是合法
链接，混进一个中文字就整段存成文本笔记，视频一条也没采到。
提取的字符集必须排除全角句读：小红书手机版的口令是
`http://xhslink.com/a/xxx，复制本条信息…`，逗号紧贴着链接，按空白切会把后半句
一起吞进 URL；半角句读与多出来的右括号在尾部单独修剪（`/wiki/Foo_(bar)` 这类
配平的括号要留）。小红书 PC 版的口令是另一种形状（`84 【标题 - 作者 | 小红书…】
😆 5wtLOclJhAjzJzW 😆 <链接>`），链接尾部带着 `xsec_token=…I=`——那个 `=` 是
token 本身的一部分，修剪尾巴时不能连它一起削掉，削了就打不开笔记。
「文字里夹着链接」不自动一律采集：口令里那段中文是平台生成的样板，丢掉无损，
而「明天看这个 <链接>」里的说明是用户自己写的，丢掉才是误判。两者形状一样，
只能按链接指向哪儿分——有专用连接器（`detectVideoPlatform` /
`detectForumPlatform`）的默认采集，其余默认存文本。默认判错的代价由提示栏兜住：
它实时显示提取到的链接与将执行的动作，旁边一颗按钮一键改判
（`CaptureDraft` 的 `mixed` 形态 + `resolveCaptureAction`），两个方向都不静默。
不支持省略协议的裸域名（`v.douyin.com/xxx`）：口令尾部的 `12/15`、`FhB:/`
这类噪音与它长得太像，误判的代价是凭空建一条采不到的任务。

配置迁移（设置 → 数据 → 配置迁移）：把全部软件设置导出成一个 JSON，在新设备上
导入，省掉逐个重配模型和重贴 Key。与备份的分工是「备份装条目、配置装设置」，
换机的完整路径是两样都做一遍。三条 IPC 在 `ipc/config-transfer.ipc.ts`，
逻辑在 `services/config-transfer/`。
读（`config:read`）与应用（`config:apply`）刻意拆成两步：导入会覆盖本机全部设置，
用户得先看清文件里有什么（几个服务商、几个模型、带没带 Key）再决定，选完文件就
已经改完了不行。apply 只接受上一次 read 记下的那个路径——路径由渲染进程传回来，
不校验就是一个能读任意文件的口子（同一条理由下备份恢复也只认列表里查得到的文件名）。
机密加密而不是明文，且**只加密机密字段**：文件因此仍然打得开、看得懂，忘了密码
也只丢 Key，其余配置照常导入（`decryptConfigSecrets` 先解一段固定明文的 canary，
密码错了当场说清楚，而不是让一堆解不开的密文被当成 Key 写进配置——那种失败要到
下一次调模型时才暴露）。算法复用 `main/security.ts` 的 AES-256-GCM，那两个函数
在此之前零调用方。scrypt 参数随文件走，所以 `parseConfigTransferFile` 必须卡住
上界，否则一份构造过的文件靠一个巨大的 N 就能把主进程拖死。
采集必须由渲染进程发起：主进程读不到 localStorage，而 `guizhi-settings` 才是 AI
配置的真相源（比 `ai-models.json` 多出 `scenarioModelDefaults`，
`normalizeModelConfig` 是逐字段重建对象、写入时直接丢弃它）。主进程补上配图
风格、快捷键与 MCP 可访问范围这三份它独有的 JSON。反过来，应用导入必须落到主进程，
因为要做 funasr 对账、要写 config 目录、也因为渲染进程写完 localStorage 就重启了。
注意「localStorage 是超集」这件事本身就是维护成本：每次启动
`loadSettingsFromMainProcess` 都拿主进程那份去整体覆盖 store，凡是只活在渲染进程的
模型字段都活不过一次重启，得单独接回来。v0.13 删掉逐模型采样参数（见下）的一半理由
就是它——那是当时唯一需要这层保护的字段。
MCP 可访问范围（`config/mcp.json`）比另外两份更需要小心：它是隐私边界，不带过去
是**静默放开**——用户在旧机器上把私人日记那个库排除掉，新机器上却是默认全开，而
界面上没有任何迹象。带过去最坏是过度收紧（知识库 id 在没恢复备份的机器上会失配，
表现为 MCP 什么都搜不到），那是 fail-closed、看得见也改得回，所以选带；`selected`
模式另记一条 warning 点名去设置页核对，否则没人会想到这一层。
`ConfigTransferFile.mcpScope` **缺省与「明确写了 all」必须分开**：`parseMcpScope`
对坏数据回的是「全部可见」，拿它去处理旧版本导出的、压根没有这个字段的文件，
一次导入就能把本机收紧过的范围放开——正是这个字段要防的事。所以解析层先判字段
在不在（`isPlainRecord`），apply 也只在 `file.mcpScope` 存在时才写。
四类东西不导出，每一类都有具体的坏处：`ytDlpPath` / `ffmpegPath` 是绝对路径且
`isAcceptableBinaryPath` 只认文件选择器当场登记过的值，导进去必然被拒——留着它
只会让人以为配好了；`backgroundImageFileName` 存的是文件名，图片实体在
`data/assets/images/` 下不随配置走，导过去是破图；`launchAtStartup` 要写系统
注册表，是逐台设备的决定；本地转写引擎（funasr）那两条指向 127.0.0.1:8620。
名单在 `shared/utils/config-transfer.ts` 的 `NON_PORTABLE_SETTINGS_KEYS`，
导出与导入两个方向都过一遍——文件是外来的，手改能把它们塞回来。
funasr 对账（`ai-reconcile.ts`）是这里最容易漏的一处：那两条记录只在安装时写入、
卸载时移除，**没有任何自愈路径**，而导入走的是 `coreAIConfigService.replace()`
整份覆盖。不把本机的捞回来，装了本地引擎的机器导一次配置就把它弄丢、只能重装。
识别同时看固定 id 与 `isManagedFunasrUrl`（手工添加的也算），本机若原本把
audioText 指向它、而导入方没配这条路由，就接回去。
脏数据也在这里筛掉：`normalizeModelConfig` 遇到空的 id / provider / apiUrl / model
会抛 `AIConfigError`，而 `replace()` 是一次性写整份——一个坏条目会让整次导入失败，
逐条筛掉并记进 warnings 才有用。`normalizeRoutes` 不检查路由目标存不存在，
指向已被筛掉模型的那些也要一起清，否则是界面上看着配好了、用起来找不到模型。
导入前把 config 目录下那 5 个 JSON 拷进 `config/pre-import-<时间戳>/`（留最近 3 份）：
导错一份文件能把 Key 一次性弄没，而 Key 是这堆东西里唯一重配代价高的。快照落在
同一个目录、那些文件本来就是明文，不改变任何安全态势，却让「导错了」变成可恢复的。
落地顺序是「主进程写文件 → 渲染进程写 localStorage → `settings.set` 写 SQLite
白名单键 → 重启」。第三步不能带 AI 键，否则 `persistSharedAIConfig` 会再做一次
整份替换、把刚对账好的 funasr 条目重新抹掉。SQLite 那一步也不能省：备份调度器、
`getMinimizeOnLaunchSetting`、代理都直接读表，而重启后的 `rehydrateSettingsState`
只同步 `networkProxy` 和 `language` 两个——不写表的话，界面显示「12 小时备份一次」
而调度器还按旧值跑。最后整体重启而不是逐项热更新：热更新做得到，但漏掉一处的表现
是「界面改了、行为没改」，排查成本远高于两秒钟。

AI 交接稿（详情页头部的机器人按钮 / 条目右键「复制给 AI」）：把一条条目序列化成
一段自包含的 Markdown，粘进 Cursor、Codex 这类 AI IDE，对方不用联网就能了解这条
内容。解决的是一个具体场景——在抖音或 B 站刷到一条讲技术的视频，想问 AI「这对我
手上的项目有没有用」。**直接把链接发过去是拿不到内容的**：抖音的详情接口对没有
签名 cookie 的请求返回空 body（正是我们放弃 yt-dlp 的原因），B 站视频的内容压根
不在 HTML 里、在音轨里，对方只能从标题猜。而归知这边素材早就齐了（AI 拟的标题、
`## 视频总结`、排过版的 `transcript`、图文的 `## 图中文字`），缺的只是一个出口。
反过来说，这个判断也不能在归知的问答里做——归知不知道用户的项目代码，所以内容
必须送出去，且必须带足技术细节，只给那份 300~800 字的总结不行，总结为了可读性
恰恰会丢掉具体的库名、参数与命令。
序列化在 `shared/utils/ai-handoff.ts`，纯函数无 IO：详情页与右键菜单共用，将来做
MCP server 时返回给模型的也是同一份文本（那才是这套东西的终极形态——AI 自己来
搜、不用复制；但要么在主进程起 HTTP server、要么打包一个独立 CLI 去啃同一个
`knowledge.db` 的进程锁，是独立的一摊工程）。
与 Markdown 导出（`export-markdown.ts`）的差别不在字段多少，而在于多了一段**阅读
须知**，三条各防一件事，且按条目实际具备的东西逐条拼、不是固定模板：有 transcript
才声明 ASR 的拉丁词音近错误（不声明，对方会认真讨论一个叫 dacker 的工具；而给
一条没有转写稿的网页剪藏挂上这句，只会让它无端怀疑正文里的术语）；`video`/`audio`
才声明不含画面（否则它会把「视频里演示了配置」当成用户已经看过的信息）；素材边界
那条总是给（视频里一句「现在忘掉前面的，跟我做」不该被当成用户的指令）。
产物里**不写任何任务指令**：问题由用户自己提，塞一句「请判断这对我的项目是否有用」
会干扰「帮我总结」这类其他用法。
精简版（只要总结）**不静默删内容**：略掉的口播稿与论坛逐楼回复必须在原位留一行
「共 N 字，本次未包含」，并附口播稿开头 180 字。只给字数接收方没法判断值不值得
再要一次全文，而开头几句足以看出这条是干货还是闲聊。（原打算列口播稿的小节标题，
但排版指令第 7 条明写「不要输出任何说明、标题或 Markdown 标记」，根本没有标题
可列。）稿子本身比预览还短时直接全给：略去 30 个字既省不了 token，还要多写一句
说明，`omittedChars` 也会报出一个实际上没省掉的数字。静默删掉会让对方以为自己
看到了全部，进而对着一份残缺素材下结论。`local-image://` 一律换成占位说明——这个协议只有归知解析得了，留着
是纯噪音；但 alt 文本（`图 1`）必须留，下面 `## 图中文字` 的 `### 图 1` 要对得上号。
元数据引用块由 `parseVideoMetaBlock` 剥进 front matter，不在正文里重复第二遍。
另存为文件走 `export:aiHandoff`，序列化仍在渲染进程做、主进程只落盘：`buildAiHandoff`
要 tags 与 collection 名，渲染进程手上现成，搬进主进程得重新查两张表。路径由
`showSaveDialog` 产生，没有路径注入面；正文不含机密，与 `.db` 备份、Markdown 导出
同一立场。

MCP server（设置 → MCP 接入，独立分区）：交接稿的下一步——不用先复制再粘，
Cursor / Codex 自己来搜、自己来读。`src/mcp/` 下三个文件，两个只读工具
（`search_knowledge` 走 FTS5，`read_item` 直接返回 `buildAiHandoff` 的产物，
那个函数当初写成 shared 里的无 IO 纯函数就是为了这一天）。
架构照抄 dbx（同为带 GUI 的本地应用）：MCP server 是**独立进程直读数据库、
不要求归知在跑**——用户在 IDE 里写代码时归知不一定开着，要求它开着等于砍掉
一半场景。dbx 也只有 `open_table` 这类遥控界面的工具才要求应用运行。
**驱动不能换，这是实测出来的**。`node-sqlite3-wasm` 的 VFS 把锁实现成
`mkdir <db>.lock` 目录且忽略锁级别，而 Node 24 自带的 `node:sqlite` 用的是
真 SQLite 的字节范围锁，两套机制互不认识：实测归知的写事务尚未提交时，
node:sqlite 3ms 就读到了那条未提交的行；以读写方式打开它还会把归知正在写的
journal 当成崩溃残留的 hot journal 去回滚，直接摧毁对方的事务。省掉打包麻烦
的那条捷径走不得。
同驱动的并发代价则可以忽略，也是实测：锁是**语句级**不是连接级（归知空闲时
第二个进程 27ms 就读到了，`.lock` 随语句结束消失）；归知处于写事务时读者退避
等待约 3 秒后成功而不是失败（`busy_timeout` 生效）；归知每 50ms 一条查询、
MCP 同时做一次 38ms 的重查询，归知侧最慢 18ms、中位 3ms、零报错。
**必须注册客户端租约**（`acquireDatabaseClientLease`）。归知主进程带
`recoverUnregisteredLock: true` 启动（它有 Electron 单实例门兜底），不在
`<db>.clients/` 下登记的话，归知启动时会把 MCP 正持有的 `.lock` 目录当成孤儿
删掉。这套租约机制在此之前零调用方，MCP 是第一个真正的用户；失败路径也要
`release()`，否则留下一个没人认领的锁。只读打开（`readOnly: true`）杜绝写坏
数据，也不触发 hot journal 回滚；不能调 `initDatabase()`——那是写路径（建表、
跑迁移、ANALYZE）。`PRAGMA user_version` 大于打包时的 `SCHEMA_VERSION` 就明确
拒绝，拿旧表结构硬查只会报出看不懂的 SQL 错误。
数据库路径复用主进程那套四级优先级（`main/data-path.ts` 只 import fs/path，
对 electron 零依赖），不另写一份：算错的表现是「MCP 搜不到但归知里明明有」，
两份实现只会越走越偏。`GUIZHI_DATA_DIR` 可覆盖（便携版与调试用，对应 dbx 的
`DBX_DATA_DIR`）。
检索默认走 `searchMode: "recall"` 而不是 phrase：调用方是模型，给进来的多半是
词组甚至整句，phrase 会把中文长句编译成一个要求逐字连续出现的短语，必然零命中。
`includeArchived: true`——归档只是「处理完了」，不是移出知识库。

`makeSnippet` 会先剥掉正文开头的元数据引用块再压平（`parseVideoMetaBlock`）。
`SNIPPET_SOURCE_LENGTH` 的注释里一直写着「会先剥掉元数据引用块」，但实现只去掉
了 `>` 标记、把内容留下了，于是 MCP 检索结果的每一条都以「平台：抖音 · 作者：X
· 时长：2:01 简介：…」开头——平台在上一行的 `platform=` 里已经给过，而那条最长
300 字的简介能把 160 字的摘要占满，正文一个字都露不出来（界面列表同理，它旁边
还另有平台与类型两列）。剥必须在**压平之前**做：那时换行还在，边界交给现成的
解析器判断；压平后再猜边界会吃掉正文开头，那比多几十字冗余更糟。剥完为空的
（只采到元数据、正文还没生成）回退到原文，列表上显示元数据也好过一片空白。
分发不发 npm 包，用应用本体当 Node 运行时：`command` 指向 GuiZhi.exe、
`args` 给脚本路径、`env` 带 `ELECTRON_RUN_AS_NODE=1`，用户机器上不需要另装
Node。产物走 `extraResources` 落在 asar 外（要按绝对路径引用），
`node-sqlite3-wasm` 整包复制到产物旁的 `node_modules/` 下靠 Node 常规解析找到，
比手拼 `app.asar.unpacked` 路径可靠，代价是多 1.25MB 副本。
打包前必须跑 `pnpm build` 而不是 `pnpm vite build`：MCP 产物由另一份 vite 配置
（`build:mcp`）产出，后者不含它。`release.yml` 当年就是这么写的、加 MCP 时没跟着
改，于是 v0.11 ~ v0.13 三个安装包里 `guizhi-mcp.mjs` 整个缺席，装了归知的人在设置页
只看得到一句「组件缺失」，而 CI 从头到尾绿灯——`extraResources` 的源不存在时
electron-builder **只是跳过那一条，不报错也不中止**（同目录的 `node-sqlite3-wasm`
源在、照常复制，所以 `resources/mcp/` 还真的存在，只是空剩一个 node_modules）。
质量流水线跑的是 `pnpm build`、但不打包，两条路各错一半，谁也照不见谁。
守卫因此加在 `beforePack`（`assertExtraResourcesExist`）而不是 CI 脚本里：
逐条确认 `from` 存在，缺了当场抛错，本地直接调 electron-builder 也拦得住。
用的是 v1 世代的 API 形态但装的是 **v2**（`@modelcontextprotocol/server`，
与 2026-07-28 spec 同日发布）。选它的风险是客户端未必跟上，已实测排除：
用 `2025-06-18` 旧协议版本握手，server 正常协商并回同一版本。`inputSchema` 收
`z.object(...)`（v1 那种 raw-shape 字面量已 deprecated）。
一条死规矩：**stdout 是 JSON-RPC 通道，日志只能走 stderr**，往 stdout 写一个
字节就会让客户端解析失败并断连——这个文件及其依赖里出现 `console.log` 就是 bug。
开库延迟到首次工具调用（启动时开的话，「没装过归知」只会表现为 server 启动
失败，客户端界面上就一个红点，用户看不到原因）。

MCP 单独占一个设置分区，紧跟「模型服务」「正文配图」，而不是挂在「数据」下面：
那一栏讲的是把内容搬出去与搬回来（备份、导出、配置迁移），MCP 是让外部工具接
进来读，方向相反；找它时的心理模型也是「跟 AI 有关的设置」。

MCP 可访问范围（设置页的「可访问的知识库」）：知识库里可能有私人日记、体检
报告这类并不想让 IDE 里的 AI 读到的东西，而 MCP 默认整库可见。范围落在
`config/mcp.json`——**不能放 localStorage**，MCP server 是独立进程读不到。
判定收在 `shared/utils/mcp-scope.ts`，DAO 侧是 `KnowledgeItemQuery` 新增的
`collectionScope`（与 `collectionId` 叠加而非二选一：前者是「在看哪个库」，
后者是「最多能看见哪些库」）。
四处细节都有具体的坏处兜着。其一，**每次工具调用重读文件、不缓存**：用户在
界面上改完范围期望立刻生效，而 MCP server 是被 IDE 拉起来后一直驻留的进程，
缓存住要等重启 IDE——「改了没反应」最难自查（有跨进程冒烟验证过改完即时生效）。
其二，**坏数据退回「全部可见」而不是「全部不可见」**：这个文件用户手改得到，
改坏了让 MCP 静默搜不到任何东西，比多看见几条难查得多，收紧只要点两下。
其三，**「选了指定范围但一个都没勾」是有效配置**，`search_knowledge` 会明说
「没有向 MCP 开放任何知识库」而不是回「没有找到」——后者会让人以为检索坏了；
同理 `read_item` 撞上范围外的条目明说「不在开放范围内」，不伪装成「不存在」，
范围是用户自己在本机设的，含糊其辞只会让他以为条目丢了。其四，未分类单列一个
开关而不是塞进 id 列表用哨兵值：归知的「未分类」是待整理队列，数量往往比任何
一个库都多，混进数组会让「清空选择」顺手把它带走，而用户根本没在列表里见过它。
范围外的知识库名字也不出现在「现有知识库」提示里。

客户端接入（`shared/utils/mcp-clients.ts` + `settings/mcp/client-presets.ts`）：
只接 Cursor 与 Codex，两家格式差异大且**踩错一律静默失败**，这正是让归知代劳
的理由。Cursor 是 JSON 的 `mcpServers`，Codex 是 **TOML** 的 `mcp_servers`
（snake_case，写成 `mcpServers` 整段被忽略、不报错）。`codex mcp add` 的
`--env` 排在服务器名**之后**、`--` 之前，而 `claude mcp add` 恰好相反（flag 必须
在名字之前）——两边抄串了同样不报错，所以将来加 Claude Code 时别照着 Codex 那条抄。
TOML 里的 Windows 路径走**单引号字面量字符串**：它不处理转义，反斜杠原样写，
比双引号里满屏的 `\\` 好读也不易错；值里真含单引号才回退到双引号串。
Cursor 的一键安装是**直接写 `~/.cursor/mcp.json`**，不是 deeplink。
最初走的是官方那条 `cursor://anysphere.cursor-deeplink/mcp/install?name=…&config=…`，
实测点下去只把 Cursor 的 MCP 设置页调到前台、不弹安装确认框。格式与官方示例
对得上（解码官方示例可见 `config` 是**裸的 server 对象**——不带 `mcpServers`
外层也不带名字，名字走 URL 的 `name`；base64 且不 URL 转义），失败的根因没继续
追，因为那条路上叠了三个不受我们控制的变量：Cursor 的 deeplink 实现社区报过
多次失效、`shell.openExternal` 到协议 handler 的传参、以及新版正在把 MCP 迁进
「Customize」。写文件则路径、格式、合并规则全在自己手里。
写入用 jsonc-parser 的 `modify` + `applyEdits` 而不是 parse → 改 → stringify：
后者会把用户手写的注释和排版全冲掉，而这是个人人都会手改的文件（该依赖此前
零调用方）。三条保护：文件解析不过就**拒绝写入并原样保留**（里面可能躺着配了
很久的一堆 server，覆盖掉换来的是「一键安装顺便清空了我的 MCP」）；空文件按
空对象处理而不是按坏文件（客户端建了没写过是常态，对着空文件说「不是合法
JSON」只会让人一头雾水）；覆盖前留一份 `.guizhi-backup`。
路径解析与写入规则都在主进程，渲染进程只发一个客户端 id——它既拿不到文件路径，
也没有任何写任意文件的余地。Codex 那边保持给 `codex mcp add` 命令：官方 CLI 比
我们代写 TOML（还要保住注释与格式）可靠。
面板刻意**没有** dbx 那排「当前版本 / 最新版本 / NODE.JS / NPM / 升级命令」：
那是因为 dbx 的 MCP server 是独立 npm 包要用户自己 `npm install -g` 维护，
而归知的随应用打包、跟着应用升级，也不要用户装 Node，照搬只会摆一排常量。
dbx 的权限矩阵同理——归知只有两个只读工具，没有分级，一句话说明就够。
范围选择器也没做成 dbx 那种左右搬运的双栏：那个形态是为几十上百个数据库连接
设计的，归知的知识库通常十来个，双栏只是把「点一下」变成「找到它再点一下」。

待办：平台登录采集（cookies）延后；macOS 已发未签名安装包（electron-builder
`identity: "-"` ad-hoc，≥26）；Developer ID 签名/公证与 icon.iconset 归知化仍待。

已知的技术债：
- 语义检索仍是全量余弦扫描，没有 ANN 索引。已改为分批取用并在批间让出
  事件循环（不再阻塞主进程），但总耗时随索引规模线性增长。
- embedding 仍按 OpenAI 请求响应格式硬编码（Anthropic 有防护会明确报错，
  Gemini 走 OpenAI 兼容层）。OCR 已适配 Anthropic，转写在不支持的协议上
  会给出可读提示而非撞 404。
- `ai:httpRequest` 只做到「已配置 host + 回环放行、未知目标限速」，没有
  真正的端点白名单。彻底收敛需要把连接测试与模型列表拉取搬进主进程。
- 主密码设置页已撤（避免假安全感）。`security.ts` 仍保留 AES-GCM 与休眠的主密码
  存储实现；`encryptText` / `decryptText` 现由配置迁移使用（传输密码派生密钥，
  与主密码无关）。应用锁 / 私密库加密尚未做。
- 用量面板只数调用次数与 token，不折算费用——单价随模型与渠道变动，写死一份
  价目表很快就是错的，而错的金额比没有金额更糟。生图与转写接口本就不回报
  token，那两栏永远是「N 次 · 0」。设置页的两处连接测试（文生图、转写）会
  真实计费，也确实记进了用量，但界面上不会额外提示这一次是探测。
  `failedCalls` 照常入库（失败也烧钱，漏记会低估），但不挂在面板上——重试与
  限流会把它堆高，数字吓人却推不出下一步，原因仍看 error.log。按模型维度只认
  API 模型名字符串：同一模型名挂在两个供应商下会合并成一行。
- 媒体资产去重只按整份文件的字节哈希：同一张图被重新编码过（换质量、
  改过 EXIF）就是两份，感知哈希那一步没做。跨条目共享的资产也没有任何
  界面提示，用户看不出两条条目指着同一个文件。
- 配置迁移是整份替换，没有「只导这几个模型」的选择性导入，也没有
  「新旧合并」——重名的服务商与模型一律以文件为准。回滚只能手工：从
  `config/pre-import-<时间戳>/` 把 JSON 拷回去，而快照只覆盖 config 目录下
  那 5 个文件，localStorage 里的外观与偏好没有快照（那些重设代价低）。
  导出不含背景图片本身，主密码也不迁移（它只是个校验值，且目前没有任何
  内容接入）。MCP 可访问范围带得过去，但它存的是知识库 id：没先恢复备份就导入
  配置的话，勾中的库在本机一个都不存在，MCP 会什么都搜不到——只有一条 warning
  提示去核对，应用不校验这些 id 在不在（导入时机上库可能还没恢复，此时校验只会
  把有效配置误判成脏数据）。
- AI 交接稿一次只导一条：批量多选后没有「一起导出这几条」，想让 AI 横向比较
  三条视频只能复制三遍。配图本身不随产物走（换成了占位说明），所以接收方看不到
  画面，图里没被 OCR 识别到的信息就是丢了。产物是一次性快照，条目之后改了不会
  跟着变；`.md` 存进项目目录后与库里那条也没有任何关联。
- MCP 只有 FTS 检索，没有语义检索：query 向量要调 embedding 接口，而 AI 配置的
  真相源在渲染进程的 localStorage 里，独立进程读不到。同理没有任何写操作
  （「帮我把这个链接采集进去」做不到，那需要归知在跑）。检索结果里的正文片段
  沿用列表投影的 snippet。配置里的路径是绝对路径，应用换安装位置后要回设置页
  重新写入一次；也没有发 npm 包，所以没法 `npx` 一行接入。
- 改了 `src/mcp/` 下的代码，光 `pnpm build:mcp` 不够：MCP server 是被 IDE 拉起来
  的长驻进程，用的是加载那一刻的 bundle，得在客户端里重启它（Cursor 是重启应用
  或在 MCP 面板把开关切一下）。这与可访问范围正相反——那个每次工具调用都重读
  文件，改完立刻生效。调试时被这条绊住会误以为改动没生效。
- MCP 客户端只接了 Cursor 与 Codex，Claude Code / VS Code / Windsurf 的格式都已
  查证（分别是 `mcpServers` + `claude mcp add`、`servers` + 必需的
  `"type": "stdio"` + `code --add-mcp`、`~/.codeium/windsurf/mcp_config.json`），
  但没做成标签页。一键安装只有 Cursor 有；VS Code 也支持 deeplink
  （`vscode://mcp/install?<urlencoded>`），补的时候连带把它一起加上。
- MCP 可访问范围只到知识库粒度：不能按标签、平台或单条目放行，也没有「除了这
  几个库以外都可见」的反选。范围只约束 MCP，不影响归知自己的问答与 Wiki。
  界面上看不出「某条目当前对 AI 是否可见」，得回设置页对照。
- 侧栏「平台」只认得出采集管线登记过来源的条目：手工粘贴的笔记没有
  `source_records`，不属于任何平台，也没有「无来源」这一组可点。平台与
  知识库、标签互斥而非叠加，问不出「这个知识库里来自抖音的那些」。
  网页桶是按域名之外的一切归并的，站点再多也只有一行。
- `import/douyin.ts` 与 `import/xiaohongshu.ts` 都依赖未公开的页面结构
  （抖音那个历史上叫过 `RENDER_DATA` 且是 URL 编码的），平台改版就要跟着修。
- 小红书只采得到分享面板复制出来的链接：地址栏手抠的 `explore/<id>` 没有
  `xsec_token`，一律 404。条目存的规范链接也不带 token（为了去重），所以详情页
  点「打开原链接」时，没登录的浏览器会看到 404。评论区没有采集，`comments`
  就在同一份 `__INITIAL_STATE__` 里但没有读。合集（多篇连载）也没有识别。
- 图文采集的 OCR 按张调用视觉模型，默认上限 9 张（`OCR_IMAGE_LIMIT`），
 超出的图片只入库不识别。上限是硬编码常量，没有做成设置项。
- 论坛采集只认 V2EX。超长帖的总结按回复分块，上限 8 块（`MAX_CHUNKS`），
 再长的部分不进总结素材，但回复本身完整入库。
- 文字稿排版的超时与预算都是一刀切的常量，没有按模型类型区分：思考类模型
 和普通模型的耗时差一个量级，前者需要 240 秒，后者十几秒就够，现在两者
 共用同一套阈值。`fastText` 路由在设置页写的是「低成本对话模型」，但没有
 任何地方拦着用户把思考类模型挂上去，也不会提示这样会慢很多。
- 评论区一律没采，而作者常把仓库地址、配套文档放在置顶评论里。**抖音这条是能做
  的**（此处先前记成「做不到，评论接口要签名 cookie」，那是照着 detail 接口的
  结论推的，没实测）：用 Googlebot UA 直接请求 `douyin.com/video/<id>`，抖音会
  返回带评论的 SSR 页面，实测抓到了前若干条。注意它与归知现在走的
  `iesdouyin.com` 分享页 + 移动端 UA 是**两条路径**，移动端那份 `_ROUTER_DATA`
  里有没有评论尚未验证，真要做得先测。小红书那条数据就在同一份
  `__INITIAL_STATE__` 的 `comments` 里，只是没读——没做是因为手上没有带
  `xsec_token` 的有效链接可验证结构，盲写一个依赖未公开页面结构的解析器是在赌。
  评论的价值实测得到过印证：一条视频的评论里有人问「评论区没有地址」，作者回
  「我发太多都疑似限流了，某站找吧」——这解释了为什么正文里一个链接都没有，
  而这个信息在视频与简介里都不存在。
- 专名表覆盖标题与简介里的拉丁词，外加一份固定的通用技术词。纯口播提到、
  又不在这两处的冷门术语仍然帮不上；中文专名被听错也不在射程内（实测中文是
  逐字正确的，暂时不是问题）。标题/简介的术语上限 40 条（通用词另算，不占这个
  额度）、简介只取前 500 字，都是硬编码常量。
- 正文配图只适配了 OpenAI 的 `/images/generations` 与 Gemini 的
 `generateContent` 两条路，Anthropic 无文生图 API（给可读提示而非撞 404）。
 图上的中文标注全靠模型自己写，老一代模型会出错字，预设把标注数压到 4~6 条
 就是为了少出错；应用不做自动 QA（回看一眼要再花一次视觉模型调用），
 不满意只能手动「换一张」。风格预设文件被改坏时静默退回内置预设并只打一条
  warn，界面上看不出来。生图的重试次数不进 `IllustrationProgress`：快速失败
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
pnpm shot                # 界面截图（窗口在屏幕外，不打扰用户）
pnpm electron:build:win  # Windows 打包
```

### 界面截图不要打扰用户

改了 UI 想看效果时走 `pnpm shot`（`apps/desktop/scripts/screenshot.mjs`），
不要手写一次性的启动脚本，也不要让用户自己去开应用截图。它拉起真实 Electron、
截图、退出，全程约 5 秒，窗口不出现在屏幕上也不抢焦点——这些脚本经常在用户
正干活时被跑起来，弹出来的窗口会把用户手上的东西整个顶掉。数据目录是一次性
临时目录，碰不到用户的库，也不与用户正开着的归知抢单实例门（`GUIZHI_E2E=1`
本来就绕过它）。默认截首屏；`--steps <file>` 传一个默认导出
`async ({ win, app, shot }) => {}` 的模块，就能先点到目标界面再截。

**窗口是「挪到屏幕外」而不是「隐藏」，这条改不得**（查过社区做法了，别再查
一遍）。`main/testing/window-mode.ts` 的做法是保持 `visible`、把窗口挪到全体
显示器边界之外、`setSkipTaskbar` + `setOpacity(0)`、并用 `showInactive()` 显示。
实测这样截出来的首屏与正常显示时**逐字节相同**（同一 sha256），渲染路径没有
差异；系统前台窗口自始至终不变，焦点不易主。透明度归零是第二道保险，防的是
Windows 在 DPI 变化或显示器热插拔时把屏幕外的窗口拽回可见区域。

社区主流方案是另一套，且**在这里不适用**——不了解这一点的人迟早会拿它来
「修正」这段代码。Electron 没有 headless 模式（Playwright 团队在
microsoft/playwright#16851 里的原话是 "it should be always visible"），
issue #13288 下被反复引用的解法是在 `ready-to-show` 里直接 `return`、
根本不调用 `show()`。它对**纯 DOM 断言**的 e2e 完全够用：Playwright 的
`click` / `waitFor` / `fill` 走 CDP 与 DOM，不需要合成帧——实测那种状态下
点击 239ms 正常完成。但截图需要一帧新的合成结果，而隐藏窗口的合成器是停摆的：
实测 `requestAnimationFrame` 两秒只推进 **2** 帧（正常是 323 帧），
`page.screenshot()` 三次里两次 15 秒超时、一次侥幸 1173ms 返回。
`setBackgroundThrottling(false)` 救不了（Electron 文档说它能让 visibility
state 保持 visible，但实测 rAF 仍是 2 帧、截图仍是两超时一成功）。
所以「不 show」与「截图」不可兼得，我们要的是后者。
另一种失败形态别混为一谈：`show()` 之后再 `hide()` 比从未 show 更糟，
连 `locator.click()` 都会超时（实测 20s / 10s 双双失败）。
真正的 headless 只有 Linux CI 上的 Xvfb，Windows 上没有对应物；VS Code 的
Electron 冒烟测试干脆就让窗口正常显示，只在 README 里叮嘱测试别依赖焦点态。
「屏幕外 + showInactive」也不是我们独创：`EchoTechFE/dimina-kit` 的
electron-deck 为了截图走的是同一套（`setPosition(-3000, -3000)` +
`showInactive()`），注释写的就是 *forces a real paint ... WITHOUT focus*。
我们只是把固定坐标换成按显示器边界算，多屏布局下才不会露出来。

截图默认带 `animations: "disabled"` + `caret: "hide"`：不加的话，界面就绪后
立刻截的**第一张**会抓到淡入过渡的中间态——实测同一界面连截三张，默认选项下
首张与后两张不一致，加上之后三张逐字节相同，耗时不变。

将来做 macOS 时这套要重新验证：`showInactive()` 在那边虽不给焦点，却硬编码
把窗口带到前台（electron#49393，2026-01 仍未合入可选项），全屏状态下还会
触发切换桌面（electron#24703）。窗口在屏幕外时未必看得出来，但别假设它同样安静。

`GUIZHI_WINDOW_MODE` 两个方向都能显式指定：`visible` 让 e2e 恢复可见（人工
盯着跑时用，`pnpm shot --visible` 就是它），`offscreen` 反过来可用于
`electron:dev`。E2E 同时跳过全局快捷键注册——自动化实例与用户正在运行的归知
并存，抢注 `Alt+Shift+P` 会把用户那个夺走，而注册失败只打一条 warn，两边都
发现不了。

脚本带产物陈旧检测：源码比 `out/` 新时直接拒跑并提示先 build。少了这一步，
改完忘了构建就会截到上一版界面，而截图看着完全正常——这种「改动像是没生效」
最难自查（与 `src/mcp/` 那条债同源）。确认无所谓时用 `--stale-ok` 跳过。

### 改主进程文件时把编辑攒成一次

`electron:dev` 常年挂在用户的终端里，而 `vite-plugin-electron` 的 main 入口走的是
默认 `onstart`——**每一次落盘都会杀掉整个 Electron 再开一个新窗口**，抢焦点，连的
还是用户的正式数据目录。preload 只刷新渲染进程，`src/renderer/**`（含 i18n 的
json）只走 HMR，都不打扰人；会触发重启的是 `src/main/**` 以及它 import 的
`packages/{shared,db,core}`。

所以改这些路径下的文件要先把整个文件的改动想清楚再一次写完，别对着同一个文件连发
十几次小编辑。实测过一次代价：给导入任务加阶段统计那回，20 次分散的写入换来
`startup.log` 里三分钟 20 次重启（21:03 四次、21:04 九次、21:05 七次），光
`import-queue.ts` 就占了 7 次；按「每个文件一次」算本该只有 6 次。用户当时正开着
应用，看到的就是窗口疯狂开合。

这与上面那套离屏截图**不是一回事，别混为一谈**：`GUIZHI_WINDOW_MODE` 管的是「我主动
拉起一个实例」，而这里是「用户自己的 dev server 被我的文件改动驱动」，那个环境变量
对它不生效。排查时也别看错日志——截图实例的 `userData` 被重定向到临时目录
（`configureE2ETestProfile`），`getLogsDir()` 跟着走，所以它的启动记录压根不会出现在
用户的 `startup.log` 里；那里面出现的每一条都是真实启动。

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
- `title` 必须给出元素自己没写出来的信息。纯图标控件该写（气泡是它唯一的名字），
  但图标本身已是通用词汇的除外——侧栏底部那颗齿轮挂「设置」等于把图标念一遍，
  可访问名交给 `aria-label` 就够了；气泡内容与可见文字不同的该写（绝对时间对
  相对时间、URL 对显示名、置灰原因、快捷键）。而 `title={x}` 配 `{x}` 这种把同一句话再念一遍的一律不写——即便
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
- 机密（API Key、`networkProxy.password`）只允许出现在两个位置：渲染进程的
 localStorage `guizhi-settings`，与主进程的 `config/ai-models.json`——它们都在
 用户自己的数据目录里。**离开数据目录的机密必须加密**。
 唯一带得走机密的功能是配置迁移（`config:export`），它用一次性的传输密码
 派生密钥、只加密机密字段；除它之外，导出一律不得携带机密：Markdown 导出只碰
 条目正文，`.db` 备份不含 AI 配置（Key 不在 SQLite 里），两者都不要为了「方便」
 破这条。日志同理，`scrubMessage` 只抹主目录，别把 Key 写进报错原文。
