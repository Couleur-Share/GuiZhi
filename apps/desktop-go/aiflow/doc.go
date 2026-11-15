// Package aiflow 在 Go 进程内无 WebView 地编排归知的 AI 工作流，取代渲染层 services/ai*.ts 与
// services/knowledge-ai 里的编排逻辑：对话客户端、摘要/标签/OCR/收件箱分类、语义索引与混合检索、
// Wiki 编译、文章问答与全局 Ask，以及后台任务执行器。
//
// 设计约束：
//   - 与渲染层逐字节对拍。请求体、提示词、错误文案都沿用 TS 版（含中文原文）；
//     apps/desktop/tests/unit/knowledge-ai/aiflow-golden-*.test.ts 驱动真实的 TS 代码生成夹具，
//     本包的 *_test.go 用同样的输入重放，任何一侧漂移都会让测试失败。
//   - 无副作用依赖。传输（Transport）、配置（ConfigSource）、用量统计（UsageSink）与各类存储
//     都是窄接口，生产实现在主包 native_aiflow.go 里复用壳现成的 AI 代理与 IPC 处理器；
//     本包不 import 主包，也不启动 WebView2，go test ./aiflow/... 可以在任何环境运行。
//   - JS 语义显式化。字符串长度/切片按 UTF-16、trim 与 \s 用 JS 的空白集、大小写与 NFKC 走 Unicode 全量规则
//     （见 jsx.go）；与 JS 不可能完全一致之处记录在 docs/agents/native-ui-development.md。
package aiflow
