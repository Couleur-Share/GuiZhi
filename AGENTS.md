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
（`import/video-url.ts`，B站/YouTube/抖音/小红书，未安装时降级）。
M10 已完成：Wiki 关系图谱（`WikiGraphView.tsx`，react-force-graph-2d 懒加载）。

v0.4.0（2026-07，首个公开发布）在此之上重构了知识库界面：卡片 / 列表双视图、
表格列配置与分页（`library/ItemTableView.tsx`、`item-table-config.ts`）、
标签选择浮层（`TagPickerPopover.tsx`）、详情全屏弹窗（`ItemDetailModal.tsx`）。

待办：平台登录采集（cookies）延后；macOS 平台暂不考虑。

已知的技术债：OCR / embedding / 转写按 OpenAI 请求响应格式硬编码，
Gemini 与 Anthropic 只在对话补全上完整可用；语义检索是全量余弦扫描，
没有 ANN 索引；列表视图分页在客户端进行，单次最多加载 200 条。

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
