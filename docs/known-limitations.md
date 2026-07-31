# 已知限制

提 [issue](https://github.com/Couleur-Share/GuiZhi/issues) 或功能请求前，请先确认问题不在下列边界内。

## 平台与安装

- **macOS** 安装包已发布，但仅 ad-hoc 签名、未经 Apple 公证；首次打开需在
  「隐私与安全性」手动允许，或对「已损坏」提示执行 `xattr -dr com.apple.quarantine`
- **FunASR 一键安装**：Windows（Python SenseVoice，约 3GB，含说话人分离）；
  **macOS Apple Silicon**（FunASR llama.cpp / GGUF，约 300MB，无说话人分离）。
  Intel Mac / Linux 请在「模型服务」配置云端 `audioText`。**ffmpeg 一键安装仅支持 Windows**；macOS 设置页提供
  `brew install ffmpeg` 复制入口，也可依赖 PATH / 自定义路径；Linux 请用系统包管理器
- Windows / Linux 安装包暂未做代码签名，SmartScreen 可能拦截

## 采集

- **论坛目前只有 V2EX**；其它站点暂无专用连接器，会退回通用网页抓取（效果通常较差）
- **抖音 / 小红书不采评论区**；小红书须用分享链接（带 `xsec_token`），否则 404
- **需要登录才能看的内容**抓不到（平台 cookies 采集未实现）
- 侧栏「平台」只覆盖采集管线登记过来源的条目；手工粘贴的笔记没有来源分组
- 平台与知识库、标签互斥而非叠加，问不出「这个知识库里来自抖音的那些」

## AI 与媒体

- **正文配图**目前适配 OpenAI 文生图与 Gemini；Anthropic 无对应 API
- **嵌入**按 OpenAI 请求格式实现（Gemini 可走兼容层；Anthropic 无 embeddings）
- **语义检索是暴力余弦**：没有 ANN 索引，条目量非常大时会变慢
- 用量面板只数调用次数与 token，不折算费用；生图与转写接口本就不回报 token 时会显示「N 次 · 0」
- 设置页的文生图 / 转写连接测试会真实计费

## 数据与同步

- **暂不提供多设备同步**，跨设备请用 [备份文件 + 配置迁移](./data.md) 中转
- 配置迁移是整份替换，没有选择性导入或新旧合并
- AI 交接稿一次只导一条；配图本身不随产物走（换成占位说明）

## MCP

- 只有 FTS 检索（无语义）、只读
- 客户端一键安装目前覆盖 Cursor（Codex 给命令）
- 可访问范围只到知识库粒度；界面上看不出「某条目当前对 AI 是否可见」

更细的技术债与设计取舍写在 [AGENTS.md](../AGENTS.md) 末尾，面向贡献者而非终端用户。
