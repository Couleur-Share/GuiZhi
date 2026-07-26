<p align="center">
  <img src="apps/desktop/src/assets/icon.png" width="120" alt="归知 GuiZhi" />
</p>

<h1 align="center">归知 GuiZhi</h1>

<p align="center"><strong>让信息归于知识 —— 本地优先的 AI 个人知识库</strong></p>

<p align="center">
  <a href="https://github.com/Couleur-Share/GuiZhi/releases/latest"><img alt="Release" src="https://img.shields.io/github/v/release/Couleur-Share/GuiZhi?include_prereleases&style=for-the-badge&color=2ea043" /></a>
  <a href="./LICENSE"><img alt="License" src="https://img.shields.io/badge/license-AGPL--3.0-8250df?style=for-the-badge" /></a>
  <a href="#下载安装"><img alt="Platform" src="https://img.shields.io/badge/Windows_·_Linux-1f6feb?style=for-the-badge" /></a>
</p>

<p align="center">
  <img alt="Electron" src="https://img.shields.io/badge/Electron_33-47848F?style=flat-square&logo=electron&logoColor=white" />
  <img alt="React" src="https://img.shields.io/badge/React_18-61DAFB?style=flat-square&logo=react&logoColor=black" />
  <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript_5-3178C6?style=flat-square&logo=typescript&logoColor=white" />
  <img alt="Vite" src="https://img.shields.io/badge/Vite_6-646CFF?style=flat-square&logo=vite&logoColor=white" />
  <img alt="SQLite" src="https://img.shields.io/badge/SQLite_FTS5-003B57?style=flat-square&logo=sqlite&logoColor=white" />
</p>

---

一段文字、一个链接、一张截图、一段录音、一条 B 站视频——归知把它们统一收进本机的
SQLite，交给 AI 做摘要、打标签、编织成互相链接的 Wiki 页面网络，再用
「关键词 + 语义」混合检索把它们找回来，并基于你自己的资料回答问题。

不需要账号，不上传云端。只有你主动配置的模型调用会走网络，其余一切留在本地。

<p align="center">
  <img src="docs/images/library-card.png" alt="归知知识库：卡片视图与常驻详情面板" width="900" />
</p>

## 下载安装

前往 [Releases](https://github.com/Couleur-Share/GuiZhi/releases/latest) 下载对应安装包：

| 平台 | 安装包 |
| --- | --- |
| Windows x64 | `GuiZhi-Setup-<版本>-x64.exe` |
| Windows arm64 | `GuiZhi-Setup-<版本>-arm64.exe` |
| Linux x64 | `GuiZhi-<版本>-x64.AppImage` / `GuiZhi-<版本>-amd64.deb` |

安装包暂未做代码签名，Windows SmartScreen 弹提示时选择「更多信息 → 仍要运行」。
macOS 版本待签名证书就绪后提供。

首次启动会自动检测旧 .NET 版的 `guizhi.db`，并提供一键迁移。

## 功能

### 采集 · 收进来只要一步

- **快速采集**：快捷键 `Alt+Shift+N`（默认应用内生效，可改为全局）、托盘菜单、
  顶栏「新建」三种入口，粘贴文本或链接自动识别类型，也可以直接开一篇空白笔记
- **拖进来就行**：文本 `.txt/.md`、图片 `.png/.jpg/.webp/.gif/.bmp`、
  音频 `.mp3/.wav/.m4a/.aac/.ogg/.flac`、视频 `.mp4/.webm/.mkv/.mov/.avi`；
  媒体文件资产化入库，详情页内联预览与播放
- **网页正文抽取**：Readability 抽正文 → Turndown 转 Markdown，
  全程 SSRF 防护（每次重定向重新校验）；抓取失败自动降级为「保存链接」，不丢东西
- **在线视频**：B 站 / YouTube / 抖音 / 小红书链接自动识别，yt-dlp 拉元数据与音轨，
  配好转写模型后一路做到文字稿 + 结构化总结 + AI 标题
- **导入队列**：并发 2、落库持久化、重启自动恢复、可取消可重试
- **去重**：规范化 URL（剥离跟踪参数）+ 内容哈希双重判定，
  撞车时可以「打开已有条目」或「仍要创建副本」

### 知识库 · 两种视图，检索优先

- **卡片视图**：虚拟化列表 + 常驻详情面板，中间栏宽度可拖拽
- **列表视图**：全宽表格，列可显隐、可拖拽调宽、可分页，详情在全屏弹窗打开
- **范围与整理**：未分类 / 全部 / 收藏 / 归档 / 回收站，知识库（集合）+ 10 色标签，
  标签选择器里直接接 AI 建议。「未分类」= 还没归入任何知识库，归好类它自己就清零
- **批量操作**：Ctrl/Cmd 点选、Shift 范围选，批量移动 / 归档 / 回收站 / 恢复 / 彻底删除
- **编辑**：CodeMirror 6 Markdown 编辑与预览双模式，「正文 / 文字稿」双 Tab，
  防抖自动保存（可关掉改用 `Ctrl+S`）
- **检索**：SQLite FTS5 中文按字分词 + 英文前缀匹配，BM25 加权排序（标题 > 标签 > 正文）

<p align="center">
  <img src="docs/images/library-list.png" alt="归知知识库：列表视图与列配置" width="900" />
</p>

### AI · 五条模型路由，各司其职

在「设置 → AI」里配置服务商与模型，然后把模型分配给五条路由：

| 路由 | 用途 |
| --- | --- |
| 主文本模型 `mainText` | 知识问答、Wiki 编译、音视频总结 |
| 快速模型 `fastText` | 摘要、自动打标签、文字稿排版 |
| 视觉模型 `visionText` | 图片 OCR |
| 嵌入模型 `embedding` | 语义检索向量（不配则检索退化为纯全文） |
| 语音转写 `audioText` | 音视频转文字 |

在此之上提供的能力：

- **AI 问答**：Agent 工具循环（`search` / `read` / `answer` 动作协议，检索即推理），
  没读过任何来源就拒绝作答；回答带可跳转引用；协议失败自动回退单发 RAG；会话落库
- **混合检索**：FTS 关键词与 embedding 语义向量并行召回、RRF 融合，
  换个说法、换种语言也能命中；embedding 未配置或调用失败时静默退化为纯 FTS
- **摘要与标签**：短文单发、长文 map-reduce 分块；一键生成可点选的标签建议
- **图片 OCR**：识别结果作为「图中文字」写入正文，随即进入全文与语义索引
- **音视频转写**：远程 `/audio/transcriptions` 接口，或安装本地 FunASR 离线转写

支持 OpenAI 兼容 / Gemini / Anthropic 三种协议，内置 13 个服务商预设
（OpenAI、OpenAI-Response、Gemini、Anthropic、Azure OpenAI、New API、Ollama、
xAI、DeepSeek、Moonshot、智谱 AI、通义千问、豆包），另可填自定义端点。

### Wiki · 让条目长成知识网络

- AI 把知识条目**增量编译**为互链的 Wiki 页面（主题 / 实体 / 概念三类）
- `[[链接]]` 经白名单清洗（幻觉链接降级为纯文本），自动生成反向链接与来源条目回溯
- 指纹 = 素材哈希 + 提示词版本，只编译真正变过的部分；
  支持自动编译（默认关）、立即编译、全量重建
- **关系图谱**：力导向图展示页面网络，点节点直达页面
- 问答检索融合 Wiki：页面优先命中，读页面时它的出链与来源成为新的检索线索

### 本地工具链 · 应用内一键安装

三个可选外部工具都不打包进安装包，需要时在「设置 → 通用 → 采集」里一键装，
下载走官方源并自动回退国内镜像，遵循应用的代理设置：

| 工具 | 用途 | 平台 |
| --- | --- | --- |
| yt-dlp | 在线视频解析与音轨下载 | 全平台 |
| ffmpeg | 音频转码为 16kHz 单声道再送转写 | Windows 一键装，其他平台用包管理器 |
| FunASR (SenseVoiceSmall) | 完全离线的本地语音转写 | 仅 Windows |

FunASR 装完会自动写入内置 Provider 并接管 `audioText` 路由，不需要任何 API Key；
服务按需拉起（`127.0.0.1:8620`），不常驻后台。

### 数据 · 全在你自己手里

- **备份**：`VACUUM INTO` 在线一致性快照，手动「立即备份」+ 定时自动备份
  （间隔 12/24/72/168 小时，保留数 5/10/20/30 条，只清理自动备份）；
  应用内更新安装前自动快照一次
- **恢复**：完整性与表结构校验、导入任务运行守卫、恢复前再存一份当前数据，完成后自动重启
- **Markdown 导出**：每条一个 `.md` + YAML frontmatter（标题/类型/标签/知识库/来源/时间），
  按知识库分文件夹，可直接丢进 Obsidian；导出不携带任何机密
- **旧版迁移**：一键把 .NET 时代的 `guizhi.db` 全量迁入（条目/知识库/标签/来源/Wiki 四表），
  在临时拷贝上执行，原文件只读不动

### 界面

明色 / 暗色 / 跟随系统，6 套莫兰迪主题色加自定义色，3 档字号，
背景图（透明度 + 模糊可调），动效三档强度，简体中文 / English。

## 快速上手

1. 装好后先按 `Alt+Shift+N` 试一次快速采集：粘一个网页链接进去，
   看它抓成 Markdown 落进「未分类」
2. 打开「设置 → AI」，加一个服务商（填 Base URL 与 API Key），拉取模型列表
3. 至少把**主文本模型**和**快速模型**分配上，就能用摘要、标签和问答了；
   想要语义检索再配**嵌入模型**，想 OCR 配**视觉模型**，想转写配**语音转写**
4. 攒够几十条内容后，去 Wiki 模块点「立即编译」，看它把条目织成页面网络

没配 AI 也能用：采集、编辑、标签、全文检索、备份导出都不依赖模型。

## 快捷键

| 动作 | 默认组合 | 默认作用域 |
| --- | --- | --- |
| 显示 / 隐藏应用 | `Alt+Shift+P` | 全局 |
| 快速采集 | `Alt+Shift+N` | 应用内 |
| 搜索 | `Alt+Shift+F` | 应用内 |
| 打开设置 | `Alt+Shift+S` | 应用内 |

四个动作都可以在「设置 → 快捷键」里改组合键，并单独切换全局 / 应用内生效。

## 数据目录

```text
%APPDATA%/GuiZhi/            # Windows；Linux 为 ~/.config/GuiZhi
├─ data/
│  ├─ knowledge.db           # 主数据库（SQLite + FTS5 + 向量表）
│  ├─ guizhi.db              # 旧 .NET 版数据库（只读迁移源）
│  └─ assets/                # 导入的图片 / 音视频 / 附件
├─ config/
│  ├─ ai-models.json         # 服务商、模型与路由配置
│  └─ shortcuts.json
├─ backups/                  # knowledge-{manual|auto|pre-update|pre-restore}-*.db
├─ tools/                    # 按需安装的 yt-dlp / ffmpeg / FunASR
└─ logs/
```

数据根目录可以在「设置 → 数据」里迁到别处。

## 从源码构建

需要 Node.js ≥ 24、pnpm ≥ 11。

```bash
pnpm install

# 开发（Vite HMR + Electron）
pnpm electron:dev

# 质量门禁
pnpm lint          # eslint + 文件行数门禁
pnpm typecheck     # TS 全量检查
pnpm test:unit     # vitest 单测
pnpm test:e2e      # Playwright 真实 Electron 冒烟

# 打包
pnpm electron:build:win
pnpm electron:build:linux
```

## 仓库结构

```text
GuiZhi/
├── apps/desktop/
│   └── src/
│       ├── main/        # Electron 主进程：窗口/托盘/快捷键、SQLite、IPC、采集与媒体服务
│       ├── preload/     # contextBridge 白名单
│       └── renderer/    # React UI：library / ask / wiki / imports 四模块 + 设置
├── packages/
│   ├── core/            # AI 配置与客户端、运行时路径
│   ├── db/              # SQLite 适配器、schema、迁移
│   └── shared/          # 跨进程类型、IPC 频道常量、协议工具
├── .github/workflows/   # 质量检查 + 打 tag 构建发布
├── AGENTS.md            # 开发者与 AI 助手指南
├── CHANGELOG.md
└── NOTICE               # PromptHub 归属声明
```

渲染进程不直接碰 Node API，一切系统能力经 preload 白名单；
远程抓取一律走主进程的 SSRF 防护。

## 已知限制

- **macOS** 安装包暂缓，等签名与公证证书（`icon.icns` 与托盘模板图案也还是 fork 来的资产）
- **FunASR / ffmpeg 一键安装仅支持 Windows**，Linux 请用系统包管理器装 ffmpeg
- **OCR、嵌入、转写需要 OpenAI 兼容端点**：这三项按 OpenAI 的请求/响应格式实现，
  Gemini 与 Anthropic 目前只在对话补全上完整可用
- **语义检索是暴力余弦**：没有 ANN 索引，条目量非常大时会变慢
- **列表视图分页在客户端进行**，单次最多加载 200 条，超出部分需要靠搜索收窄
- **暂不提供多设备同步**，跨设备请用备份文件或 Markdown 导出中转
- **平台登录采集（cookies）** 未实现，需要登录才能看的内容抓不到

## 许可证与致谢

[GNU Affero General Public License v3.0](./LICENSE)（`AGPL-3.0-only`）。

归知的应用骨架 fork 自 [PromptHub](https://github.com/legeling/PromptHub) v0.5.9（AGPL-3.0）：
Electron 多进程结构、Liquid Glass 主题系统、设置框架、AI Provider 工作台与构建发布链。
业务域（知识采集、检索、AI、Wiki）为归知自研。详见 [NOTICE](./NOTICE)。
感谢 legeling 与 PromptHub 的所有贡献者。
