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

- **论坛目前支持 V2EX、NGA、LINUX DO、小众软件与 2Libra**；其它站点暂无专用连接器，会退回通用网页抓取（效果通常较差）。LINUX DO 与小众软件复用 Discourse 连接器，2Libra 走公开只读 API。NGA 公开帖无需登录，需登录版块仍采不到；它不镜像整帖水楼（讨论区只留楼主回复，并尽量附带被回复楼摘要），也不做泥潭像素级复刻。附件图只处理主楼与楼主回复，上限 80 张，超出保留外链。详情页重新生成总结时，NGA 条目只能基于已入库的楼主回复。讨论里点「回复 @某人」时，被回复楼若未入库（NGA 常见）或旧条目无楼层号且作者不唯一，只能提示无法定位，不会为此重采旧帖
- **抖音 / 小红书依赖未公开的页面 SSR 状态**（`_ROUTER_DATA` / `__INITIAL_STATE__`），平台改版后需跟修解析器；失败时任务错误带稳定码（如 `[structure_missing]`），日志含 marker 是否出现与页面哈希前缀。发版前或排障可手动跑 `node apps/desktop/scripts/probe-platform-parsers.mjs`（需自备样例 URL 环境变量，**不进默认 CI**）
- **抖音 / 小红书热门评论是限量采集**（任务可选 10 / 20 / 50 条），不是评论区完整镜像；小红书的公开免登录链路须用带 `xsec_token` 的分享链接，否则 404
- **内置登录采集目前只覆盖抖音、小红书与 LINUX DO**；其它平台需要登录才能看的内容仍抓不到。登录态保存在归知自己的 Electron session 中，不复用系统浏览器 cookies
- 侧栏「平台」只覆盖采集管线登记过来源的条目；手工粘贴的笔记没有来源分组
- 平台与知识库、标签互斥而非叠加，问不出「这个知识库里来自抖音的那些」

## AI 与媒体

- **正文配图**目前适配 OpenAI 文生图与 Gemini；Anthropic 无对应 API
- **嵌入**按 OpenAI 请求格式实现（Gemini 可走兼容层；Anthropic 无 embeddings）
- **语义检索是精确全量余弦**（进程内向量缓存，避免每次解码 BLOB）；没有 ANN。触发 ANN 的门槛：`totalChunks >= 50_000`，或缓存命中后最近检索中位耗时仍 **> 500ms**——届时倾向纯 JS/WASM 的 HNSW 侧车文件，不引入 sqlite 原生扩展。问答侧栏会显示分块数与最近检索耗时
- 用量面板只数调用次数与 token，不折算费用；生图与转写接口本就不回报 token 时会显示「N 次 · 0」
- 设置页的文生图 / 转写连接测试会真实计费

## 数据与同步

- **暂不提供多设备同步**，跨设备请用 [备份文件 + 配置迁移](./data.md) 中转
- 配置迁移是整份替换，没有选择性导入或新旧合并
- AI 交接稿一次只导一条；配图本身不随产物走（换成占位说明）
- 详情阅读位置（标签 / 滚动 / 讨论搜索词 / 楼层目录开关）记在本机 `localStorage`，不随 `.db` 备份或配置迁移带走

## MCP

- 只有 FTS 检索（无语义）、只读
- 客户端一键安装目前覆盖 Cursor（Codex 给命令）
- 可访问范围只到知识库粒度；界面上看不出「某条目当前对 AI 是否可见」

更细的技术债与设计取舍写在 [AGENTS.md](../AGENTS.md) 末尾，面向贡献者而非终端用户。
