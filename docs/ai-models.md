# 模型路由

在「设置 → 模型服务」里配置服务商与模型，然后把模型分配给六条路由：

| 路由 | 用途 |
| --- | --- |
| 主文本模型 `mainText` | 知识问答、Wiki 编译、音视频 / 论坛总结 |
| 快速模型 `fastText` | 摘要、自动打标签、文字稿排版 |
| 视觉模型 `visionText` | 图片 OCR |
| 嵌入模型 `embedding` | 语义检索向量（不配则检索退化为纯全文） |
| 语音转写 `audioText` | 音视频转文字 |
| 正文配图 `imageGen` | 文生图（专用，不参与对话） |

## 能力一览

- **AI 问答**：Agent 工具循环（`search` / `read` / `answer`），没读过来源就拒绝作答；回答带可跳转引用；会话落库
- **混合检索**：FTS 与 embedding 并行召回、RRF 融合；embedding 未配置时静默退化为纯 FTS
- **摘要与标签**：短文单发、长文 map-reduce；一键生成可点选的标签建议
- **图片 OCR**：识别结果作为「图中文字」写入正文，进入全文与语义索引
- **音视频转写**：远程 `/audio/transcriptions`，或安装本地 FunASR（Windows Python /
  macOS Apple Silicon GGUF）；可选说话人分离（仅 Windows Python 引擎，默认关）
- **正文配图**：先策划后逐张生成，风格可编辑；设置页「正文配图」与条目面板共用同一套预设

支持 OpenAI 兼容 / Gemini / Anthropic 三种协议，内置多家服务商预设，也可填自定义端点。

最少只需配好 `mainText` 与 `fastText`；其余按需。未配置时相关能力会静默跳过或退化为非 AI 路径，不影响采集与全文检索。

协议与能力边界见 [已知限制](./known-limitations.md)。
