// Package services 定义原生界面依赖的 Go 能力（窄接口）与数据传输类型。
//
// 分层规则（见 docs/agents/native-ui-development.md）：
//   - native/screens、native/app 只依赖本包与 native/kit，不依赖 package main；
//   - package main 提供适配器，把 Library 的现有能力接成这里的接口；
//   - 测试用假实现，所以 go test ./native/... 不拉起 WebView2、不需要数据库。
//
// Package services declares the narrow Go capabilities the native UI depends on.
package services

import "time"

// Services 汇总原生界面依赖的全部 Go 能力：每个模块一个窄接口。
type Services struct {
	Imports Imports

	// Library 是知识库侧栏的数据（计数、合集、标签）；nil 时知识库模块按「尚未迁移」处理。
	Library Library

	// Inbox 是处理中心的数据（列表与计数、整理、AI 归类、语义索引与 Wiki 状态）；nil 时处理中心按「尚未迁移」处理。
	Inbox Inbox

	// Settings 与 Prefs 是设置与界面偏好；nil 时界面用默认值、改动只在内存里（测试里不关心设置时可以不给）。
	Settings Settings
	Prefs    Prefs

	// Shortcuts、Solver 与 Updater 是设置页「快捷键」「网络与 FlareSolverr」「关于」分区的系统能力（见 settings_system.go）；
	// nil 时对应的动作按「不可用」处理：快捷键显示默认值、测试连接与检查更新弹失败提示（测试与对照里不接线）。
	Shortcuts Shortcuts
	Solver    FlareSolverr
	Updater   Updater

	// Data、Backup 与 ConfigTransfer 是设置页「数据与备份」分区的系统能力（见 data_backup.go）；
	// nil 时对应的动作按「不可用」处理：目录按钮禁用，备份、导出与导入弹失败提示（测试与对照里不接线）。
	Data           Data
	Backup         Backup
	ConfigTransfer ConfigTransfer

	// CaptureTools 与 MCP 是设置页「采集与转写」「MCP 设置」分区的系统能力（见 capture_tools.go、mcp.go）；
	// nil 时对应分区的动作按「不可用」处理：引擎与账号状态读不出来、动作弹失败提示（测试与对照里不接线）。
	CaptureTools CaptureTools
	MCP          MCP

	// AI 是设置页「AI 模型服务」分区的系统能力（见 ai_settings.go）；
	// nil 时分区读不出配置、显示加载失败（测试与对照里不接线）。
	AI AISettings

	// Ask 是 AI 问答（全局问答工作区、围绕本文提问）的数据层（见 ask.go）；
	// nil 时问答模块按「尚未迁移」处理，测试与对照里不关心问答时可以不给。
	Ask Ask

	// Wiki 是 Wiki 模块（目录、搜索、详情与编辑、关系图谱、全文编译、更新建议）的数据层（见 wiki.go）；
	// nil 时 Wiki 模块按「尚未迁移」处理，测试与对照里不关心 Wiki 时可以不给。
	Wiki Wiki
	// Themed 是主题阅读（AI 阅读页：生成、状态、任务、设置引导与导出）的数据层（见 themed.go）；
	// nil 时「AI 阅读」入口按「尚未迁移」处理，测试与对照里不关心主题阅读时可以不给。
	Themed ThemedReading

	// Migration 是旧版（.NET）数据库的探测与一次性迁移（见 migration.go）；
	// nil 时不探测、不弹迁移对话框，测试与对照里不关心迁移时可以不给。
	Migration Migration

	// Log 是错误日志；nil 时界面只弹提示、不留痕（测试里不关心日志时可以不给）。
	Log Logger

	// OpenExternal 用系统默认浏览器打开链接（条目详情里的来源链接）；只接受 http / https，其余协议返回错误。
	// nil 时点击弹失败提示（测试与对照里不接线）。
	OpenExternal func(url string) error

	// Now 返回当前时间；测试注入固定值，nil 时用 time.Now。
	Now func() time.Time
}

// Clock 返回当前时间。
func (s Services) Clock() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
