package app

// Root 的外壳接线：跨线程任务队列、关闭对话框、界面命令与网页岛的收拢。
// 窗口 API（显隐、托盘）在 Host（shell.go）里，这里是界面线程一侧。

import (
	"sync"
	"sync/atomic"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/island"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/screens/shell"
)

// shellState 是 Root 里和外壳接线有关的状态。
type shellState struct {
	// invalidate 请求重绘（任意 goroutine；可为空）。窗口隐藏时 Viva 仍会在重绘之前调用 OnWake，所以任务不会卡在隐藏的窗口里。
	invalidate func()

	// islands 登记当前开着的网页岛：窗口收到托盘时先销毁它们（WebView 子进程只在窗口可见时存在）。
	islands island.Group

	// pendingCloseDialog 是外壳（任意 goroutine）要求弹出关闭对话框的请求；界面线程在下一次排空时消费。
	pendingCloseDialog atomic.Bool

	tasksMu sync.Mutex
	tasks   []func()
}

// wake 请求重绘（也就是请求一次 OnWake）。
func (r *Root) wake() {
	if r.invalidate != nil {
		r.invalidate()
	}
}

// runOnUI 把 fn 排到界面线程上执行：窗口线程在下一次 OnWake 或下一帧开头排空队列。任意 goroutine。
func (r *Root) runOnUI(fn func()) {
	r.tasksMu.Lock()
	r.tasks = append(r.tasks, fn)
	r.tasksMu.Unlock()
	r.wake()
}

// DrainShell 在窗口线程上执行排队的任务，并把外壳「弹关闭对话框」的请求落到窗口状态上。
// Run 把它挂在 Config.OnWake（窗口隐藏时也会跑）上，View 每帧开头再调一次（无头测试没有 OnWake）。
func (r *Root) DrainShell() {
	r.tasksMu.Lock()
	tasks := r.tasks
	r.tasks = nil
	r.tasksMu.Unlock()
	for _, fn := range tasks {
		fn()
	}
	if r.pendingCloseDialog.Swap(false) {
		r.win.CloseDialogOpen = true
	}
}

// closeIslands 销毁全部网页岛（窗口收到托盘之前）。
func (r *Root) closeIslands() { r.islands.CloseAll() }

// showCloseDialog 让界面弹出关闭对话框（渲染层 window:showCloseDialog → setShowCloseDialog(true)）。任意 goroutine。
func (r *Root) showCloseDialog() {
	r.pendingCloseDialog.Store(true)
	r.wake()
}

// dispatch 把外壳命令排到界面线程上执行。任意 goroutine。
func (r *Root) dispatch(cmd Command) {
	r.runOnUI(func() { r.applyCommand(cmd) })
}

// applyCommand 在界面线程上执行外壳命令。
func (r *Root) applyCommand(cmd Command) {
	switch cmd {
	case CommandOpenSettings:
		r.win.Page = shell.PageSettings
		return
	case CommandNewItem:
		r.win.Page = shell.PageHome // 渲染层先回到主页，再请求快速采集
		if r.openCapture() {
			return
		}
	case CommandShortcutNewItem:
		// 快捷键只发事件、不切页面：停在设置页时它不会把人带回主页，对话框直接盖在当前页上。
		if r.openCapture() {
			return
		}
	}
	// 更新对话框与搜索聚焦依赖的界面在 M3 才迁移（台账 B-008）；没有装配采集时快速采集也一样：命令先留痕，不假装执行了。
	if log := r.opts.Services.Log; log != nil {
		log.AppError("native", locales.NativeShellCommandAction(), locales.NativeShellCommandNotMigrated(string(cmd)))
	}
}

// closeGate 是 Config.OnCloseRequest：外壳接管了这次关闭（隐藏到托盘、弹关闭对话框）就不关；没接管照常关闭。
// 没有外壳（截图、对照与无头测试）时返回 nil：不拦，关窗口就是关闭。
func closeGate(sh Shell) func(allow func()) {
	if sh == nil {
		return nil
	}
	return func(allow func()) {
		if sh.OnCloseRequest() {
			return
		}
		allow()
	}
}

// onCloseDialog 收关闭对话框的结果（界面线程；渲染层 CloseDialog 的 handleMinimize / handleExit / handleCancel）。
//
// 选了最小化或退出：勾了「记住我的选择」就先把关闭行为写进设置（渲染层 setCloseAction），再交外壳执行；
// 写入失败只提示、不拦着这次操作（外壳在内存里记住了本次的选择）。只关对话框则通知外壳复位等待标记，否则下次点关闭不会再弹。
func (r *Root) onCloseDialog(res kit.CloseDialogResult) {
	sh := r.opts.Shell
	switch res.Choice {
	case kit.CloseDialogMinimize, kit.CloseDialogExit:
		action := "minimize"
		next := settings.CloseMinimize
		if res.Choice == kit.CloseDialogExit {
			action, next = "exit", settings.CloseExit
		}
		if res.Remember {
			r.guard.Run(locales.NativeSettingsSaveAction(), func() error {
				return r.store.Update(func(v *settings.Values) { v.UI.CloseAction = next })
			})
		}
		if sh != nil {
			sh.CloseDialogResult(action, res.Remember)
		}
	case kit.CloseDialogCancel:
		if sh != nil {
			sh.CloseDialogCancel()
		}
	}
}
