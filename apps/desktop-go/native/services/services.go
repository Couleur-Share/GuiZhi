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

	// Settings 与 Prefs 是设置与界面偏好；nil 时界面用默认值、改动只在内存里（测试里不关心设置时可以不给）。
	Settings Settings
	Prefs    Prefs

	// Log 是错误日志；nil 时界面只弹提示、不留痕（测试里不关心日志时可以不给）。
	Log Logger

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
