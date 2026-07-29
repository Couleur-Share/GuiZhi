# 快速上手

## 安装

从 [Releases](https://github.com/Couleur-Share/GuiZhi/releases/latest) 下载对应安装包：

| 平台 | 安装包 |
| --- | --- |
| Windows x64 | `GuiZhi-Setup-<版本>-x64.exe` |
| Windows arm64 | `GuiZhi-Setup-<版本>-arm64.exe` |
| Linux x64 | `GuiZhi-<版本>-x64.AppImage` / `GuiZhi-<版本>-amd64.deb` |

安装包暂未做代码签名，Windows SmartScreen 弹提示时选择「更多信息 → 仍要运行」。
macOS 版本待签名证书就绪后提供。

首次启动若检测到旧 .NET 版的 `guizhi.db`，会提示一键迁移。

## 五步走通

1. 按 `Alt+Shift+N` 试一次快速采集：粘一个网页链接或 V2EX 帖子地址，看它落进「未分类」
2. 打开「设置 → 模型服务」，加一个服务商，至少把**主文本**和**快速**模型分配上
3. 想要语义检索再配**嵌入**，想 OCR 配**视觉**，想转写配**语音转写**，想正文配图配 **imageGen**（详见 [模型路由](./ai-models.md)）
4. （可选）「设置 → MCP 接入」把归知接到 Cursor，让 IDE 里的 AI 直接搜库（详见 [MCP](./mcp.md)）
5. 攒够几十条后，去 Wiki 模块点「立即编译」

没配 AI 也能用：采集、编辑、标签、全文检索、备份导出都不依赖模型。

采集平台与口令粘贴说明见 [采集平台](./capture-platforms.md)。

## 快捷键

| 动作 | 默认组合 | 默认作用域 |
| --- | --- | --- |
| 显示 / 隐藏应用 | `Alt+Shift+P` | 全局 |
| 快速采集 | `Alt+Shift+N` | 应用内 |
| 搜索 | `Alt+Shift+F` | 应用内 |
| 打开设置 | `Alt+Shift+S` | 应用内 |

四个动作都可以在「设置 → 快捷键」里改组合键，并单独切换全局 / 应用内生效。
