// Package viva 是归知原生界面（Viva 路线，长期分支 feat/viva-ui）的根包，本身不含代码，只定下边界。
//
// React 渲染层（apps/desktop/src/renderer）是设计与行为的唯一来源：这里不重新设计、不"改进"、
// 不删减功能、不改文案。"完美复刻"的 L1–L4 定义与偏差台账见 docs/agents/viva-ui-development.md
// 与 docs/agents/viva-parity-ledger.md。
//
// 子包与依赖方向（只能向下依赖，绝不 import package main）：
//
//	viva/app      外壳：窗口与自绘标题栏、rail、路由、托盘、快捷键、通知、单实例
//	viva/kit      统一封装：主题、文本输入（唯一允许直接用 Viva 输入控件的地方）、Toast、对话框、图标
//	viva/screens  每个模块一个子包：界面与视图模型
//	viva/island   W1 岛封装：按需开的 WebView2 页面（阅读视图、Wiki 图谱、mermaid、媒体）
//	viva/locales  生成物：文案目录的普通 Go 输出（不手改）
//	viva/parity   对等基础设施：Viva 侧采集器、比较库、阈值、React 事实 JSON
//
// 数据与行为经 native/services 的窄接口取得，不复制业务逻辑；`go test ./viva/...` 不拉起 WebView2。
package viva
