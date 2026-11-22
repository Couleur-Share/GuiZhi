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
