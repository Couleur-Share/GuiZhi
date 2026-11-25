package main

// Viva 外壳接线（-viva-shell）：把 appLifecycle（关闭策略、托盘命令、窗口显隐，与 React 路径共用，见 app_lifecycle.go）
// 接到 Viva 主窗口、托盘、系统快捷键与单实例上，并把主进程的推送（关闭对话框、托盘与快捷键命令）转成界面命令。
//
// 线程：Attach 与托盘创建在事件循环线程；OnCloseRequest 也在事件循环线程；推送订阅者在推送方的 goroutine 上同步执行，
// 只做轻量工作（经 Host 的线程安全入口把事情排给界面线程）。策略本身（lifecycle）不持锁调用窗口，见 app_lifecycle.go。

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"path/filepath"
	"strings"
	"sync"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/singleinstance"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	vivaapp "guizhi-mygo-shell/viva/app"
)

// statusRemover 是托盘图标的句柄（*vapp.StatusItem 满足）。
type statusRemover interface{ Remove() }

// vivaHost 是外壳需要的主窗口与应用能力：viva/app 的 *Host 经 realVivaHost 满足，测试用假实现。
type vivaHost interface {
	Show()
	Hide()
	Close()
	Quit()
	Visible() bool
	Minimized() bool
	AddStatusItem(cfg vapp.StatusItemConfig) (statusRemover, error)
	ShowCloseDialog()
	Dispatch(cmd vivaapp.Command)
	RunOnUI(fn func())
}

// realVivaHost 把 *vivaapp.Host 适配成 vivaHost（只有 AddStatusItem 的返回类型不同）。
type realVivaHost struct{ *vivaapp.Host }

func (h realVivaHost) AddStatusItem(cfg vapp.StatusItemConfig) (statusRemover, error) {
	item, err := h.Host.AddStatusItem(cfg)
	if err != nil {
		return nil, err
	}
	return item, nil
}

// vivaWindow 把 Viva 主窗口适配成 lifecycleWindow。
type vivaWindow struct{ h vivaHost }

func (w vivaWindow) Show() { w.h.Show() }
func (w vivaWindow) Hide() { w.h.Hide() }

// Focus：Viva 的 Show 就是「显示或提升并聚焦」，没有单独的聚焦调用。
func (w vivaWindow) Focus()   {}
func (w vivaWindow) Close()   { w.h.Close() }
func (w vivaWindow) Restore() { w.h.Show() } // Show 会先还原最小化的窗口
// IsVisible 与 Electron 的 isVisible() 一致：被最小化的窗口不算可见（托盘菜单据此显示「显示归知」）。
func (w vivaWindow) IsVisible() bool   { return w.h.Visible() && !w.h.Minimized() }
func (w vivaWindow) IsMinimized() bool { return w.h.Minimized() }

// IsLoading 与 OnceLoaded：Viva 没有页面加载过程，界面一开始就绪。
func (w vivaWindow) IsLoading() bool      { return false }
func (w vivaWindow) OnceLoaded(fn func()) { fn() }

// vivaShell 实现 vivaapp.Shell。
type vivaShell struct {
	lc       *appLifecycle
	lib      *Library
	hub      *pushHub
	settings services.Settings
	// systemShortcuts 为真时注册系统级快捷键：只有真实实例这样做；截图、基准等自动化实例不抢用户正在用的组合键。
	systemShortcuts bool
	// registerShortcut 注册系统级快捷键；缺省 Viva 的 RegisterGlobalShortcut，测试注入记录器。
	registerShortcut func(vapp.GlobalShortcut, func()) (func(), error)
	instance         *vivaInstance // 单实例锁，可为空
	// manageLoginItem 为真时接开机自启（台账 B-024）：登录项由 lc.setLoginItem 写，随设置里的「开机自启」变化。
	// 只有真实实例（非自动化、没有显式 -userdata/-db）才开：临时测试运行绝不改写系统登录项。
	manageLoginItem bool
	// packaged 为真（安装包构建）且启动时设置已是开：启动期把旧版登记刷新到当前安装路径；初始为关时启动期不动注册表。
	packaged bool
	// startHidden 为真时窗口建好后直接驻留托盘（登录项拉起且设置了启动时隐藏，或带 --hidden）。
	startHidden bool
	// windowState 记录窗口位置与大小（台账 B-027）；为空表示不记。关闭请求时先落盘，即使这次关闭被拦下。
	windowState *vivaWindowStatePersister

	mu      sync.Mutex
	host    vivaHost
	tray    *vivaTray
	closers []func() // Shutdown 时按相反顺序撤销：订阅、快捷键
}

var _ vivaapp.Shell = (*vivaShell)(nil)

// Attach 实现 vivaapp.Shell（事件循环线程，窗口打开之后）。
func (s *vivaShell) Attach(h *vivaapp.Host) { s.attachHost(realVivaHost{h}) }

func (s *vivaShell) attachHost(h vivaHost) {
	lc := s.lc
	tray := &vivaTray{a: lc, host: h, labels: s.trayLabels}
	s.mu.Lock()
	s.host, s.tray = h, tray
	s.mu.Unlock()

	lc.mu.Lock()
	lc.win, lc.tray, lc.quit = vivaWindow{h}, tray, h.Quit
	lc.push = s.lib.pushMain
	lc.mu.Unlock()
	setQuitHook(h.Quit)

	// 关闭行为：启动时按设置生效（选了「最小化」就先把托盘建好），之后随设置变化。
	if v, err := s.settings.Snapshot(); err != nil {
		logAppErrorf("shell", "读取关闭行为", "%s", err.Error())
	} else {
		lc.setCloseAction(string(v.UI.CloseAction))
	}
	s.addCloser(s.settings.Subscribe(func(v settings.Values) { lc.setCloseAction(string(v.UI.CloseAction)) }))

	s.subscribePushes(h)
	if s.systemShortcuts {
		s.startSystemShortcuts()
	}
	if s.instance != nil {
		s.instance.SetHandler(lc.onSecondInstance)
	}
	if s.manageLoginItem {
		s.watchLoginItem()
	}
	if s.startHidden { // 登录项拉起：窗口直接驻留托盘，点托盘或第二个实例拉起即显示
		lc.createTray()
		h.Hide()
		lc.emitVisibility(false)
	}
}

// watchLoginItem 接开机自启（B-024）：安装包构建且启动时设置已开，刷新一次旧版登记（路径可能还指着旧安装位置）；
// 之后设置里的「开机自启」每次变化都写登录项。启动时设置为关不动注册表（用户可能在任务管理器里自己管理）。
func (s *vivaShell) watchLoginItem() {
	last := false
	if v, err := s.settings.Snapshot(); err != nil {
		logAppErrorf("app", "设置开机自启", "读取设置失败：%s", err.Error())
	} else {
		last = v.App.LaunchAtStartup
		if last && s.packaged {
			s.applyLoginItem(true)
		}
	}
	var mu sync.Mutex // 订阅者在写入方的 goroutine 上同步调用，可能并发
	s.addCloser(s.settings.Subscribe(func(v settings.Values) {
		mu.Lock()
		changed := v.App.LaunchAtStartup != last
		last = v.App.LaunchAtStartup
		mu.Unlock()
		if changed {
			s.applyLoginItem(v.App.LaunchAtStartup)
		}
	}))
}

// applyLoginItem 写登录项；失败只记日志（不阻断设置保存）。
func (s *vivaShell) applyLoginItem(open bool) {
	s.lc.mu.Lock()
	set := s.lc.setLoginItem
	s.lc.mu.Unlock()
	if set == nil {
		return
	}
	if err := set(open); err != nil {
		logAppErrorf("app", "设置开机自启", "%s", err.Error())
	}
}

// OnCloseRequest 实现 vivaapp.Shell：用户关窗口时，按关闭行为决定弹对话框、隐藏到托盘或放行退出。
func (s *vivaShell) OnCloseRequest() bool {
	s.windowState.flush() // 即使这次关闭被拦下（隐藏到托盘），也先保存窗口位置与大小
	handled := s.lc.onClose()
	if handled {
		log.Printf("[viva-shell] 关闭请求：已接管（隐藏到托盘，或弹出关闭对话框）")
	} else {
		log.Printf("[viva-shell] 关闭请求：放行，应用退出")
	}
	return handled
}

// CloseDialogResult 实现 vivaapp.Shell：关闭对话框里选了「最小化到托盘」或「退出应用」。
func (s *vivaShell) CloseDialogResult(action string, remember bool) {
	s.lc.closeDialogResult(action, remember)
}

// CloseDialogCancel 实现 vivaapp.Shell：只关掉了关闭对话框，复位等待标记。
func (s *vivaShell) CloseDialogCancel() { s.lc.clearPending() }

// Shutdown 在事件循环结束之后调用：撤销订阅与快捷键，摘掉托盘图标，释放单实例锁。幂等。
func (s *vivaShell) Shutdown() {
	s.mu.Lock()
	closers, tray := s.closers, s.tray
	s.closers, s.tray, s.host = nil, nil, nil
	s.mu.Unlock()
	for i := len(closers) - 1; i >= 0; i-- {
		closers[i]()
	}
	if tray != nil {
		tray.destroyNow() // 事件循环已经停了，RunOnUI 不会再执行
	}
	s.lc.mu.Lock()
	s.lc.win, s.lc.tray = nil, nil
	s.lc.mu.Unlock()
	setQuitHook(nil)
	s.instance.Close()
}

func (s *vivaShell) addCloser(fn func()) {
	s.mu.Lock()
	s.closers = append(s.closers, fn)
	s.mu.Unlock()
}

// trayLabels 是托盘菜单文案：界面语言取设置里的语言（Viva 界面显示的就是它），取不到按中文（首次安装的默认）。
func (s *vivaShell) trayLabels() trayMenuLabels {
	lang := ""
	if v, err := s.settings.Snapshot(); err == nil {
		lang = string(v.App.Language)
	}
	return getTrayMenuLabels(trayLocale(lang, "zh-CN"))
}

// subscribePushes 把主进程的推送转成界面命令：关闭对话框、托盘菜单与全局快捷键发来的 app:command / shortcut:triggered。
func (s *vivaShell) subscribePushes(h vivaHost) {
	s.addCloser(s.hub.Subscribe("window:showCloseDialog", func(any) { h.ShowCloseDialog() }))
	s.addCloser(s.hub.Subscribe("app:command", func(payload any) {
		if cmd, ok := appCommandFromPush(payload); ok {
			h.Dispatch(cmd)
		}
	}))
	s.addCloser(s.hub.Subscribe("shortcut:triggered", func(payload any) {
		if action, _ := payload.(string); action != "" {
			if cmd, ok := shortcutCommand(action); ok {
				h.Dispatch(cmd)
			}
		}
	}))
}

// startSystemShortcuts 按快捷键配置注册系统级快捷键（只注册模式为 global 的，见 shortcuts.go）。
func (s *vivaShell) startSystemShortcuts() {
	n := newVivaShortcutNative(s.lib, s.lc.toggleWindow)
	if s.registerShortcut != nil {
		n.registerKey = s.registerShortcut
	}
	s.lib.sc.mu.Lock()
	s.lib.sc.native = n
	s.lib.sc.mu.Unlock()
	s.lib.startShortcuts(false)
	s.addCloser(n.unregisterAll)
}

// appCommandFromPush 解出托盘菜单发来的 app:command（载荷是 {"type": "item:new" | "settings:open" | "updater:open"}）。
func appCommandFromPush(payload any) (vivaapp.Command, bool) {
	m, ok := payload.(map[string]any)
	if !ok {
		return "", false
	}
	typ, _ := m["type"].(string)
	switch typ {
	case "item:new":
		return vivaapp.CommandNewItem, true
	case "settings:open":
		return vivaapp.CommandOpenSettings, true
	case "updater:open":
		return vivaapp.CommandOpenUpdater, true
	}
	return "", false
}

// shortcutCommand 是 shortcut:triggered 的动作对应的界面命令（App.tsx 的 handleShortcutTriggered）；showApp 由主进程处理。
func shortcutCommand(action string) (vivaapp.Command, bool) {
	switch action {
	case "newItem":
		return vivaapp.CommandShortcutNewItem, true
	case "search":
		return vivaapp.CommandFocusSearch, true
	case "settings":
		return vivaapp.CommandOpenSettings, true
	}
	return "", false
}

// vivaInstance 是 Viva 外壳的单实例锁：同一数据目录只允许一个实例，后来者把窗口带到前台后退出。
type vivaInstance struct {
	claim *singleinstance.Instance

	mu       sync.Mutex
	onLaunch func()
}

// vivaInstanceID 是单实例锁的名字：按数据目录区分，隔离的验证实例之间、与真实安装之间互不干扰。
func vivaInstanceID(userData string) string {
	abs, err := filepath.Abs(userData)
	if err != nil {
		abs = userData
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(abs))))
	return "guizhi.viva." + hex.EncodeToString(sum[:8])
}

// claimVivaInstance 抢单实例锁。primary 为假表示已有实例在跑、本次的参数已转交给它，调用方应直接退出（不要碰数据库）；
// 抢锁本身失败时 primary 为真并返回错误：判定不了有没有别的实例，照常运行（无锁）。
func claimVivaInstance(userData string, args []string) (vi *vivaInstance, primary bool, err error) {
	vi = &vivaInstance{}
	claim, first, err := singleinstance.SingleInstance(vivaInstanceID(userData), args, func([]string) { vi.launched() })
	switch {
	case err != nil:
		return vi, true, err
	case !first:
		return nil, false, nil
	}
	vi.claim = claim
	return vi, true, nil
}

// SetHandler 登记「有后来者启动」时的处理（窗口建好后：把窗口带到前台）。窗口建好之前的后来者没有可聚焦的窗口，直接忽略。
func (vi *vivaInstance) SetHandler(fn func()) {
	vi.mu.Lock()
	vi.onLaunch = fn
	vi.mu.Unlock()
}

func (vi *vivaInstance) launched() {
	vi.mu.Lock()
	fn := vi.onLaunch
	vi.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// Close 释放锁（可对 nil 调用）。
func (vi *vivaInstance) Close() {
	if vi != nil {
		vi.claim.Close()
	}
}
