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

论坛帖子采集（V2EX）：`import/v2ex.ts` 走官方 v1 只读接口
（`/api/topics/show.json` + `/api/replies/show.json`），无需 token，
限额 600 次/小时，一次采集用两次；回复接口不分页，总是一次返回整帖。
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
- 远程抓取必须走 `main/services/net-safety.ts` 的 SSRF 防护。
- 机密（API Key）不写入 localStorage 之外的明文位置；导出功能不得携带机密。
