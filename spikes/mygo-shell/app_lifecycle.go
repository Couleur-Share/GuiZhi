package main

// 应用生命周期：关闭行为策略、托盘命令派发、窗口可见性推送、开机自启与设置读取。
// 对应 apps/desktop/src/main/index.ts 里 mainWindow 的 close/show/hide 处理、
// window:* 与 app:set* 频道，以及 tray-command-dispatcher.ts / tray-controller.ts。
//
// 与 mygo 解耦：窗口、托盘、开机自启都经接口注入，策略本身可用假实现单测。
// 约束：任何调用窗口/托盘的地方都不能持有 mu——OnClose 在主线程跑，窗口方法又要切到主线程，持锁会死锁。

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

// lifecycleWindow 是生命周期逻辑需要的窗口能力（mygo.Window 满足前七个，其余由适配器提供）。
type lifecycleWindow interface {
	Show()
	Hide()
	Focus()
	Close()
	Restore()
	IsVisible() bool
	IsMinimized() bool
	// IsLoading / OnceLoaded 对应 webContents.isLoading() 与 once("did-finish-load")。
	IsLoading() bool
	OnceLoaded(fn func())
}

// trayPort 是托盘控制器的最小接口。
type trayPort interface {
	Create()
	Destroy()
	Refresh()
}

type appLifecycle struct {
	mu                 sync.Mutex
	win                lifecycleWindow
	tray               trayPort
	push               func(channel string, payload any)
	quit               func()
	setLoginItem       func(open bool) error
	isWindows          bool
	closeAction        string // ask | minimize | exit
	minimizeToTray     bool
	pendingCloseAction bool
	quitting           bool
	debugMode          bool
}

func newAppLifecycle(isWindows bool) *appLifecycle {
	return &appLifecycle{closeAction: "ask", isWindows: isWindows, push: func(string, any) {}, quit: func() {}}
}

func (a *appLifecycle) snapshotWin() (lifecycleWindow, trayPort) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.win, a.tray
}

func (a *appLifecycle) setQuitting() {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
}

func (a *appLifecycle) isQuitting() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.quitting
}

// emitVisibility 对应 emitWindowVisibility：推给渲染进程并刷新托盘菜单（「显示/隐藏」文案取决于可见性）。
func (a *appLifecycle) emitVisibility(visible bool) {
	a.push("window:visibility-changed", visible)
	if _, t := a.snapshotWin(); t != nil {
		t.Refresh()
	}
}

func (a *appLifecycle) createTray() {
	if _, t := a.snapshotWin(); t != nil {
		t.Create()
	}
}

func (a *appLifecycle) destroyTray() {
	if _, t := a.snapshotWin(); t != nil {
		t.Destroy()
	}
}

// onClose 返回 true 表示阻止这次关闭。Windows：ask 弹对话框、minimize 隐藏；其他平台看 minimizeToTray。
func (a *appLifecycle) onClose() bool {
	a.mu.Lock()
	if a.quitting {
		a.mu.Unlock()
		return false
	}
	win := a.win
	if a.isWindows {
		switch {
		case a.closeAction == "ask" && !a.pendingCloseAction:
			a.pendingCloseAction = true
			a.mu.Unlock()
			a.push("window:showCloseDialog", nil)
			return true
		case a.closeAction == "minimize":
			a.mu.Unlock()
			if win != nil {
				win.Hide()
			}
			return true
		}
	} else if a.minimizeToTray {
		a.mu.Unlock()
		if win != nil {
			win.Hide()
		}
		return true
	}
	a.mu.Unlock()
	return false
}

// toggleWindow 对应托盘左键 / window:toggleVisibility：窗口不存在时无可重建（Windows 版关最后一个窗口即退出）。
func (a *appLifecycle) toggleWindow() {
	if win, _ := a.snapshotWin(); win != nil {
		toggleWindowForShowApp(win, a.emitVisibility)
	}
}

// trayQuit 对应托盘「退出」：走窗口关闭路径，让渲染进程的 beforeunload 在数据库关闭前落盘。
func (a *appLifecycle) trayQuit() {
	a.setQuitting()
	if win, _ := a.snapshotWin(); win != nil {
		win.Close()
		return
	}
	a.quit()
}

// showWindow 把窗口带到前台：最小化先还原，再显示并聚焦。
func (a *appLifecycle) showWindow(win lifecycleWindow) {
	if win.IsMinimized() {
		win.Restore()
	}
	win.Show()
	win.Focus()
}

// onSecondInstance 对应 second-instance：聚焦已有窗口。
func (a *appLifecycle) onSecondInstance() {
	win, _ := a.snapshotWin()
	if win == nil {
		return
	}
	a.showWindow(win)
	a.emitVisibility(true)
}

// dispatchAppCommand 对应 dispatchTrayAppCommand：先把窗口带到前台，页面还在加载就等加载完再发。
func (a *appLifecycle) dispatchAppCommand(command map[string]any) bool {
	win, _ := a.snapshotWin()
	if win == nil {
		return false
	}
	a.showWindow(win)
	a.emitVisibility(true)
	send := func() { a.push("app:command", command) }
	if win.IsLoading() {
		win.OnceLoaded(send)
		return true
	}
	send()
	return true
}

// ── IPC：均为 send（无返回值），参数错误只记日志，与 TS 的 console.error 一致 ──

func firstArg(a []json.RawMessage, i int) any {
	if i >= len(a) {
		return nil
	}
	var v any
	if json.Unmarshal(a[i], &v) != nil {
		return nil
	}
	return v
}

func (a *appLifecycle) register(b *Bridge) {
	b.sends["window:toggleVisibility"] = func([]json.RawMessage) { a.toggleWindow() }
	b.sends["app:setMinimizeToTray"] = func(args []json.RawMessage) {
		enabled := jsTruthy(firstArg(args, 0))
		a.mu.Lock()
		a.minimizeToTray = enabled
		a.mu.Unlock()
		if enabled {
			a.createTray()
		} else {
			a.destroyTray()
		}
	}
	b.sends["app:setCloseAction"] = func(args []json.RawMessage) {
		action, _ := firstArg(args, 0).(string)
		if action != "ask" && action != "minimize" && action != "exit" {
			logAppErrorf("app", "设置关闭行为", "app:setCloseAction requires action to be 'ask', 'minimize', or 'exit'")
			return
		}
		a.mu.Lock()
		a.closeAction = action
		isWin := a.isWindows
		a.mu.Unlock()
		if action == "minimize" && isWin {
			a.createTray()
		}
	}
	b.sends["app:setDebugMode"] = func(args []json.RawMessage) {
		enabled := jsTruthy(firstArg(args, 0))
		a.mu.Lock()
		a.debugMode = enabled
		a.mu.Unlock()
	}
	b.sends["app:setAutoLaunch"] = func(args []json.RawMessage) {
		enabled, ok := firstArg(args, 0).(bool)
		if !ok {
			logAppErrorf("app", "设置开机自启", "app:setAutoLaunch requires enabled to be a boolean")
			return
		}
		if a.setLoginItem == nil {
			return
		}
		if err := a.setLoginItem(enabled); err != nil {
			logAppErrorf("app", "设置开机自启", "app:setAutoLaunch failed to apply login item settings: %s", err.Error())
		}
	}
	b.sends["window:closeDialogResult"] = func(args []json.RawMessage) {
		data, ok := firstArg(args, 0).(map[string]any)
		if !ok {
			// 数组、字符串、null 都不是合法的 data 对象；与 TS 一样只复位等待标记。
			logAppErrorf("app", "关闭对话框", "window:closeDialogResult requires a non-null data object")
			a.clearPending()
			return
		}
		action, _ := data["action"].(string)
		if action != "minimize" && action != "exit" {
			logAppErrorf("app", "关闭对话框", "window:closeDialogResult requires action to be 'minimize' or 'exit'")
			a.clearPending()
			return
		}
		a.mu.Lock()
		a.pendingCloseAction = false
		if jsTruthy(data["remember"]) {
			a.closeAction = action
		}
		if action == "exit" {
			a.quitting = true
		}
		win := a.win
		a.mu.Unlock()
		if action == "minimize" {
			if win != nil {
				win.Hide()
			}
			a.createTray()
			return
		}
		if win != nil {
			win.Close()
		}
	}
	b.sends["window:closeDialogCancel"] = func([]json.RawMessage) { a.clearPending() }
}

func (a *appLifecycle) clearPending() {
	a.mu.Lock()
	a.pendingCloseAction = false
	a.mu.Unlock()
}

// ── 设置读取（settings-readers.ts / language-setting.ts）──

// readBoolSetting 对应 readBooleanSetting：没有该键或读取失败返回 (false, false)；值必须解析为 JSON true 才算真。
func (l *Library) readBoolSetting(ctx context.Context, key string) (value, found bool) {
	rows, err := l.db.Query(ctx, "SELECT value FROM settings WHERE key = ?", key)
	if err != nil || len(rows.Values) == 0 {
		return false, false
	}
	s, _ := rows.Values[0][0].(string)
	var v any
	if json.Unmarshal([]byte(s), &v) != nil {
		return false, true
	}
	b, _ := v.(bool)
	return b, true
}

// readLanguageSetting 对应 readLanguageSetting：只认 en / zh，其余（含读取失败）返回 ""。
func (l *Library) readLanguageSetting(ctx context.Context) string {
	rows, err := l.db.Query(ctx, "SELECT value FROM settings WHERE key = ?", "language")
	if err != nil || len(rows.Values) == 0 {
		return ""
	}
	raw, _ := rows.Values[0][0].(string)
	lang := raw
	var parsed any
	if json.Unmarshal([]byte(raw), &parsed) == nil {
		s, ok := parsed.(string)
		if !ok {
			return ""
		}
		lang = s
	}
	if lang == "en" || lang == "zh" {
		return lang
	}
	return ""
}

// trayLanguage 对应 tray-controller 的 getStoredLanguage() ?? getLocale()。
func trayLocale(stored, systemLocale string) string {
	if strings.TrimSpace(stored) != "" {
		return stored
	}
	return systemLocale
}
