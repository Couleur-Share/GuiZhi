// Package overlays 是全局浮层的状态层：命令面板、更新对话框、首次使用设置清单、旧版数据迁移对话框，
// 以及窗口内（局部）快捷键的匹配与分发（台账 B-008、B-016、B-026，M3-6a）。
//
// 与 themed、inbox 等屏幕包同一做法：每个 Store 自己持有互斥锁、对外给不可变的快照（View），
// 服务调用一律经 Spawn 放到后台，结果经 invalidate 唤醒窗口重绘；时钟、计时器、协程都可注入，测试里是同步与手动触发。
// 界面（对话框的绘制与挂载）在后续步骤里放进 Root；本包不依赖窗口、不画任何东西。
//
// 渲染层对应：components/app/GlobalCommandPalette.tsx、DesktopAppCommandBridge.tsx、UpdateDialog.tsx、
// setup/SetupChecklistDialog.tsx、migration/MigrationDialog.tsx，以及 App.tsx 里的局部快捷键、自动更新检查、迁移探测与设置清单的自动弹出。
package overlays

import (
	"context"
	"time"
)

// Env 是各 Store 共用的运行环境注入点，零值可用（生产默认）。
type Env struct {
	// Ctx 是发给服务的请求的根上下文（应用生命周期）；nil 取 context.Background()。
	Ctx context.Context
	// Spawn 在后台执行一个动作；默认开 goroutine，测试注入同步执行以保持确定性。
	Spawn func(fn func())
	// After 在 d 之后后台执行 fn（防抖、定时检查）；默认 time.AfterFunc，测试注入手动触发。
	After func(d time.Duration, fn func())
	// Now 取当前时间；nil 取 time.Now。
	Now func() time.Time
}

func (e *Env) context() context.Context {
	if e.Ctx != nil {
		return e.Ctx
	}
	return context.Background()
}

func (e *Env) spawn(fn func()) {
	if e.Spawn != nil {
		e.Spawn(fn)
		return
	}
	go fn()
}

func (e *Env) after(d time.Duration, fn func()) {
	if e.After != nil {
		e.After(d, fn)
		return
	}
	time.AfterFunc(d, fn)
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// wake 在 invalidate 非 nil 时调用它（后台结果落定之后请求窗口重绘）。
func wake(invalidate func()) {
	if invalidate != nil {
		invalidate()
	}
}
