<p align="center">
  <img src="apps/desktop/src/assets/icon.png" width="120" alt="归知 GuiZhi" />
</p>

<h1 align="center">归知 GuiZhi</h1>

<p align="center"><strong>让信息归于知识 —— 本地优先的 AI 个人知识库</strong></p>

<p align="center">
  <a href="https://github.com/Couleur-Share/GuiZhi/releases/latest"><img alt="Release" src="https://img.shields.io/github/v/release/Couleur-Share/GuiZhi?include_prereleases&style=for-the-badge&color=2ea043" /></a>
  <a href="./LICENSE"><img alt="License" src="https://img.shields.io/badge/license-AGPL--3.0-8250df?style=for-the-badge" /></a>
  <a href="#下载安装"><img alt="Platform" src="https://img.shields.io/badge/Windows_·_macOS_·_Linux-1f6feb?style=for-the-badge" /></a>
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
| macOS Apple Silicon | `GuiZhi-<版本>-arm64.dmg` |
| macOS Intel | `GuiZhi-<版本>-x64.dmg` |
| Linux x64 | `GuiZhi-<版本>-x64.AppImage` / `GuiZhi-<版本>-amd64.deb` |

安装包暂未做正式代码签名。Windows SmartScreen 弹提示时选择「更多信息 → 仍要运行」。
macOS 包仅做 ad-hoc 签名、未经 Apple 公证：若提示无法验证开发者，到「系统设置 →
隐私与安全性」选择仍要打开；若提示「已损坏」，在终端执行
`xattr -dr com.apple.quarantine /Applications/GuiZhi.app`。首次启动若检测到旧
.NET 版 `guizhi.db`，会提示一键迁移。

## 快速上手

1. 按 `Alt+Shift+N` 粘一个网页或 V2EX 链接，看它落进「未分类」
2. 「设置 → 模型服务」至少配好**主文本**与**快速**模型
3. 按需再配嵌入 / 视觉 / 转写 / 正文配图；可选「MCP 接入」接到 Cursor
4. 攒够几十条后，去 Wiki 模块点「立即编译」

没配 AI 也能用：采集、编辑、标签、全文检索、备份导出都不依赖模型。更细的步骤与快捷键见 [快速上手](./docs/getting-started.md)。

## 功能一览

| 能力 | 要点 |
| --- | --- |
| 采集 | 链接 / 分享口令 / 本地文件；抖音·B 站·小红书·YouTube·V2EX·网页；导入队列可重试 |
| 知识库 | 卡片 / 列表双视图；知识库·标签·平台分区；FTS5 + 可选语义检索；AI 交接稿 |
| AI | 六条模型路由；问答带引用；摘要标签；OCR；转写；正文配图 |
| Wiki | AI 增量编译互链页面 + 关系图谱 |
| MCP | 独立只读进程；Cursor 一键接入 |
| 数据 | 备份恢复、Markdown 导出、配置迁移；全在本机 |

平台对照表、能力边界与工具链说明见 [文档](./docs/README.md)。

<p align="center">
  <img src="docs/images/library-list.png" alt="归知知识库：列表视图与列配置" width="900" />
</p>

## 文档

完整说明在仓库 [`docs/`](./docs/README.md)，随版本一起维护：

- [快速上手](./docs/getting-started.md) · [采集平台](./docs/capture-platforms.md) · [功能说明](./docs/features.md)
- [模型路由](./docs/ai-models.md) · [MCP 接入](./docs/mcp.md) · [数据与备份](./docs/data.md)
- [已知限制](./docs/known-limitations.md) · [从源码构建](./docs/building.md)

开发者约定见 [AGENTS.md](./AGENTS.md)；变更记录见 [CHANGELOG.md](./CHANGELOG.md)。

## 从源码构建

需要 Node.js ≥ 24、pnpm ≥ 11。

```bash
pnpm install
pnpm electron:dev          # 开发
pnpm electron:build:win    # Windows 打包（须含 MCP 产物，勿只跑 vite build）
pnpm electron:build:mac    # macOS 打包（须在 macOS 上）
pnpm electron:build:linux  # Linux 打包
```

质量门禁与仓库结构见 [从源码构建](./docs/building.md)。

## 许可证与致谢

[GNU Affero General Public License v3.0](./LICENSE)（`AGPL-3.0-only`）。

归知的应用骨架 fork 自 [PromptHub](https://github.com/legeling/PromptHub) v0.5.9（AGPL-3.0）：
Electron 多进程结构、Liquid Glass 主题系统、设置框架、AI Provider 工作台与构建发布链。
业务域（知识采集、检索、AI、Wiki）为归知自研。详见 [NOTICE](./NOTICE)。
感谢 legeling 与 PromptHub 的所有贡献者。
