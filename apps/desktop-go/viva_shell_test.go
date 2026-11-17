package main

import (
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	vivaapp "guizhi-mygo-shell/viva/app"
)

// fakeStatusItem 是假的托盘图标句柄：数 Remove 次数。
type fakeStatusItem struct{ host *fakeVivaHost }

func (i *fakeStatusItem) Remove() { i.host.add("tray-remove") }

// fakeVivaHost 记下外壳对 Viva 主窗口做的事。RunOnUI 只排队，由 drain 按真实的「下一帧开头」语义执行。
type fakeVivaHost struct {
	mu                sync.Mutex
	calls             []string
	hidden, minimized bool
	items             []vapp.StatusItemConfig
	addErr            error
	dispatched        []vivaapp.Command
	tasks             []func()
}

func (h *fakeVivaHost) add(s string) {
	h.mu.Lock()
	h.calls = append(h.calls, s)
	h.mu.Unlock()
}

func (h *fakeVivaHost) count(s string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.calls {
		if c == s {
			n++
		}
	}
	return n
}

func (h *fakeVivaHost) Show() {
	h.add("show")
	h.mu.Lock()
	h.hidden, h.minimized = false, false
	h.mu.Unlock()
}

func (h *fakeVivaHost) Hide() {
	h.add("hide")
	h.mu.Lock()
	h.hidden = true
	h.mu.Unlock()
}
func (h *fakeVivaHost) Close() { h.add("close") }
func (h *fakeVivaHost) Quit()  { h.add("quit") }
func (h *fakeVivaHost) Visible() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.hidden
}

func (h *fakeVivaHost) Minimized() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.minimized
}

func (h *fakeVivaHost) AddStatusItem(cfg vapp.StatusItemConfig) (statusRemover, error) {
	h.add("tray-add")
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.addErr != nil {
		return nil, h.addErr
	}
	h.items = append(h.items, cfg)
	return &fakeStatusItem{host: h}, nil
}
func (h *fakeVivaHost) ShowCloseDialog() { h.add("close-dialog") }
func (h *fakeVivaHost) Dispatch(cmd vivaapp.Command) {
	h.mu.Lock()
	h.dispatched = append(h.dispatched, cmd)
	h.mu.Unlock()
}

func (h *fakeVivaHost) RunOnUI(fn func()) {
	h.mu.Lock()
	h.tasks = append(h.tasks, fn)
	h.mu.Unlock()
}

// drain 执行已排队的界面线程任务（任务里再排的也执行）。
func (h *fakeVivaHost) drain() {
	for {
		h.mu.Lock()
		tasks := h.tasks
		h.tasks = nil
		h.mu.Unlock()
		if len(tasks) == 0 {
			return
		}
		for _, fn := range tasks {
			fn()
		}
	}
}

func (h *fakeVivaHost) lastItem(t *testing.T) vapp.StatusItemConfig {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.items) == 0 {
		t.Fatal("托盘图标还没建")
	}
	return h.items[len(h.items)-1]
}

func (h *fakeVivaHost) commands() []vivaapp.Command {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]vivaapp.Command(nil), h.dispatched...)
}

// shellRig 是一套装配好的外壳：真实的 appLifecycle、推送集线器与设置假实现，窗口与托盘是假的。
type shellRig struct {
	t     *testing.T
	lc    *appLifecycle
	lib   *Library
	hub   *pushHub
	set   *servicestest.Settings
	host  *fakeVivaHost
	shell *vivaShell

	mu     sync.Mutex
	pushes []string // "频道" 或 "频道=载荷"
	keys   []vapp.GlobalShortcut
	stops  int
}

func newShellRig(t *testing.T, closeAction settings.CloseAction, systemShortcuts bool) *shellRig {
	t.Helper()
	dir := t.TempDir()
	r := &shellRig{t: t, lc: newAppLifecycle(true), hub: newPushHub(), set: &servicestest.Settings{}, host: &fakeVivaHost{}}
	r.lib = &Library{configDir: dir, paths: Paths{UserData: dir}}
	r.lib.setPush(r.hub.Dispatch)
	v := settings.Default()
	v.UI.CloseAction = closeAction
	r.set.Set(v)
	for _, ch := range []string{"window:visibility-changed", "shortcut:triggered"} {
		ch := ch
		r.hub.Subscribe(ch, func(p any) { r.record(ch, p) })
	}
	r.shell = &vivaShell{lc: r.lc, lib: r.lib, hub: r.hub, settings: r.set, systemShortcuts: systemShortcuts,
		registerShortcut: func(s vapp.GlobalShortcut, _ func()) (func(), error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.keys = append(r.keys, s)
			return func() { r.mu.Lock(); r.stops++; r.mu.Unlock() }, nil
		}}
	r.shell.attachHost(r.host)
	t.Cleanup(r.shell.Shutdown)
	return r
}

func (r *shellRig) record(ch string, p any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := p.(string); ok {
		ch += "=" + s
	} else if b, ok := p.(bool); ok {
		ch += "=" + map[bool]string{true: "true", false: "false"}[b]
	}
	r.pushes = append(r.pushes, ch)
}

func (r *shellRig) pushed() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.pushes...)
}

func (r *shellRig) closeAction() string {
	r.lc.mu.Lock()
	defer r.lc.mu.Unlock()
	return r.lc.closeAction
}

func (r *shellRig) pending() bool {
	r.lc.mu.Lock()
	defer r.lc.mu.Unlock()
	return r.lc.pendingCloseAction
}

func (r *shellRig) quitting() bool { return r.lc.isQuitting() }

func TestVivaWindowAdaptsHost(t *testing.T) {
	h := &fakeVivaHost{}
	w := vivaWindow{h}
	if !w.IsVisible() || w.IsMinimized() {
		t.Fatal("新窗口应可见、未最小化")
	}
	h.minimized = true
	if w.IsVisible() {
		t.Error("最小化的窗口不算可见（与 Electron 的 isVisible 一致，托盘菜单才显示「显示归知」）")
	}
	if !w.IsMinimized() {
		t.Error("IsMinimized 应跟随宿主")
	}
	w.Restore()
	if h.Minimized() || h.count("show") != 1 {
		t.Errorf("Restore 应当 Show 一次并还原：%v", h.calls)
	}
	w.Hide()
	if w.IsVisible() {
		t.Error("Hide 之后不可见")
	}
	w.Focus() // 无独立聚焦调用：不应有副作用
	w.Close()
	if h.count("close") != 1 || h.count("show") != 1 || h.count("hide") != 1 {
		t.Errorf("调用记录不对：%v", h.calls)
	}
	if w.IsLoading() {
		t.Error("Viva 没有页面加载过程")
	}
	called := false
	w.OnceLoaded(func() { called = true })
	if !called {
		t.Error("OnceLoaded 应当立刻回调")
	}
}

func TestVivaShellAttachWiresLifecycle(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	win, tray := r.lc.snapshotWin()
	if win == nil || tray == nil {
		t.Fatalf("Attach 之后生命周期应有窗口与托盘：win=%v tray=%v", win, tray)
	}
	if r.host.count("tray-add") != 0 {
		t.Error("关闭行为是 ask 时不该预先建托盘")
	}
	r.lc.quit()
	if r.host.count("quit") != 1 {
		t.Error("lc.quit 应当结束 Viva 的事件循环")
	}
	quitApp()
	if r.host.count("quit") != 2 {
		t.Error("quitApp（更新器、重启应用用）应当走 Viva 的退出")
	}
	r.shell.Shutdown()
	if quitHook.Load() != nil {
		t.Error("Shutdown 应当撤销退出钩子")
	}
	if win, tray := r.lc.snapshotWin(); win != nil || tray != nil {
		t.Error("Shutdown 之后生命周期不应再持有窗口与托盘")
	}
	r.shell.Shutdown() // 幂等
}

func TestVivaShellShutdownRemovesTrayDirectly(t *testing.T) {
	r := newShellRig(t, settings.CloseMinimize, false)
	r.host.drain() // 启动时已按「最小化」建好托盘
	if !r.shell.tray.exists() {
		t.Fatal("前提：托盘已建")
	}
	r.shell.Shutdown() // 事件循环已停：RunOnUI 不会再执行，必须直接摘掉图标，否则托盘里留下一个点不动的幽灵图标
	if r.host.count("tray-remove") != 1 {
		t.Errorf("Shutdown 应当直接摘掉托盘图标：%v", r.host.calls)
	}
	if len(r.host.tasks) != 0 {
		t.Errorf("不应再往界面线程排任务：%d", len(r.host.tasks))
	}
}

func TestVivaShellCloseActionFollowsSettings(t *testing.T) {
	r := newShellRig(t, settings.CloseMinimize, false)
	if r.closeAction() != "minimize" {
		t.Fatalf("启动时应按设置生效：%q", r.closeAction())
	}
	r.host.drain()
	if r.host.count("tray-add") != 1 {
		t.Fatal("关闭行为是最小化时，启动就要把托盘建好（窗口一关就收到托盘，得有地方点回来）")
	}
	if _, err := r.set.Update(func(v *settings.Values) { v.UI.CloseAction = settings.CloseExit }); err != nil {
		t.Fatal(err)
	}
	if r.closeAction() != "exit" {
		t.Errorf("设置变化应当随动：%q", r.closeAction())
	}
	if _, err := r.set.Update(func(v *settings.Values) { v.UI.CloseAction = settings.CloseAsk }); err != nil {
		t.Fatal(err)
	}
	if r.closeAction() != "ask" {
		t.Errorf("设置变化应当随动：%q", r.closeAction())
	}
	r.shell.Shutdown()
	if r.set.Subscribers() != 0 {
		t.Error("Shutdown 应当撤销设置订阅")
	}
}

func TestVivaShellCloseRequestAsk(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	if !r.shell.OnCloseRequest() {
		t.Fatal("ask：应当接管这次关闭")
	}
	if r.host.count("close-dialog") != 1 || !r.pending() {
		t.Errorf("应当弹出关闭对话框并记下等待标记：%v pending=%v", r.host.calls, r.pending())
	}
	// 对话框还开着时再点关闭：放行（与 Electron 版的 pendingCloseAction 逻辑一致）。
	if r.shell.OnCloseRequest() {
		t.Error("等待期间再次关闭应当放行")
	}
	if r.host.count("close-dialog") != 1 {
		t.Error("不应重复弹对话框")
	}
}

func TestVivaShellCloseRequestMinimizeAndExit(t *testing.T) {
	r := newShellRig(t, settings.CloseMinimize, false)
	r.host.drain()
	if !r.shell.OnCloseRequest() || r.host.count("hide") != 1 {
		t.Errorf("minimize：应当隐藏到托盘并接管：%v", r.host.calls)
	}
	if r.host.count("close-dialog") != 0 {
		t.Error("minimize 不弹对话框")
	}
	e := newShellRig(t, settings.CloseExit, false)
	if e.shell.OnCloseRequest() {
		t.Error("exit：应当放行，让窗口关闭")
	}
	if e.host.count("hide") != 0 || e.host.count("close-dialog") != 0 {
		t.Errorf("exit 不隐藏也不弹对话框：%v", e.host.calls)
	}
}

func TestVivaShellCloseDialogResult(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	r.shell.OnCloseRequest()
	r.shell.CloseDialogResult("minimize", true)
	r.host.drain()
	if r.host.count("hide") != 1 || r.host.count("tray-add") != 1 {
		t.Errorf("选最小化：应当隐藏窗口并建托盘：%v", r.host.calls)
	}
	if r.closeAction() != "minimize" || r.pending() {
		t.Errorf("勾了记住：关闭行为应记为 minimize 且复位等待标记：%q pending=%v", r.closeAction(), r.pending())
	}

	x := newShellRig(t, settings.CloseAsk, false)
	x.shell.OnCloseRequest()
	x.shell.CloseDialogResult("exit", false)
	if x.host.count("close") != 1 || !x.quitting() {
		t.Errorf("选退出：应当走窗口关闭路径并标记退出：%v quitting=%v", x.host.calls, x.quitting())
	}
	if x.closeAction() != "ask" {
		t.Errorf("没勾记住：关闭行为不变：%q", x.closeAction())
	}
	if x.shell.OnCloseRequest() {
		t.Error("标记退出之后的关闭请求应当放行（否则 Close 又被拦回对话框）")
	}
}

func TestVivaShellCloseDialogCancel(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	r.shell.OnCloseRequest()
	r.shell.CloseDialogCancel()
	if r.pending() {
		t.Fatal("取消应当复位等待标记")
	}
	if !r.shell.OnCloseRequest() || r.host.count("close-dialog") != 2 {
		t.Errorf("取消之后再点关闭应当再次弹出对话框：%v", r.host.calls)
	}
}

func TestVivaTrayCreateOnceAndDestroy(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	tray := r.shell.tray
	tray.Create()
	if r.host.count("tray-add") != 0 {
		t.Fatal("AddStatusItem 只能在事件循环线程上调用：Create 要排到界面线程，不能在调用方的线程上直接建")
	}
	tray.Create()
	r.host.drain()
	if r.host.count("tray-add") != 1 || !tray.exists() {
		t.Fatalf("重复创建只应建一个图标：%v", r.host.calls)
	}
	cfg := r.host.lastItem(t)
	if cfg.Tooltip != trayTooltip || cfg.Icon.Size != 32 || len(cfg.Icon.RGBA) != 32*32*4 {
		t.Errorf("图标配置不对：tooltip=%q size=%d bytes=%d", cfg.Tooltip, cfg.Icon.Size, len(cfg.Icon.RGBA))
	}
	tray.Destroy()
	if !tray.exists() {
		t.Error("Destroy 要排到界面线程，不应立刻生效")
	}
	r.host.drain()
	if tray.exists() || r.host.count("tray-remove") != 1 {
		t.Errorf("Destroy 之后应摘掉图标：%v", r.host.calls)
	}
	tray.Create()
	r.host.drain()
	if r.host.count("tray-add") != 2 {
		t.Error("摘掉之后应能重新创建")
	}
	// 先建后销的快速切换按调用顺序执行：最终没有图标。
	tray.Create()
	tray.Destroy()
	tray.Destroy()
	r.host.drain()
	if tray.exists() {
		t.Error("Create 之后紧跟 Destroy，最终不应有图标")
	}
}

func TestVivaTrayCreateFailureIsLoggedNotFatal(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	r.host.addErr = errors.New("没有通知区域")
	r.shell.tray.Create()
	r.host.drain()
	if r.shell.tray.exists() {
		t.Error("创建失败时不应记为已有图标")
	}
	r.host.addErr = nil
	r.shell.tray.Create()
	r.host.drain()
	if !r.shell.tray.exists() {
		t.Error("失败之后应当还能重试创建")
	}
	bad := &vivaTray{a: r.lc, host: r.host, labels: r.shell.trayLabels,
		icon: func() (vapp.StatusIcon, error) { return vapp.StatusIcon{}, errors.New("坏图标") }}
	bad.create()
	if bad.exists() {
		t.Error("图标解码失败不应建托盘")
	}
}

func TestVivaTrayClickTogglesWindow(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	r.shell.tray.Create()
	r.host.drain()
	click := r.host.lastItem(t).OnClick
	click(vapp.StatusClick{})
	if r.host.Visible() {
		t.Fatal("可见时点托盘应当隐藏")
	}
	click(vapp.StatusClick{})
	if !r.host.Visible() {
		t.Error("隐藏时点托盘应当显示")
	}
	got := r.pushed()
	want := []string{"window:visibility-changed=false", "window:visibility-changed=true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("显隐应当推送可见性变化：%v", got)
	}
}

func menuTitles(items []vapp.MenuItem) []string {
	var out []string
	for _, it := range items {
		if it.Separator {
			out = append(out, "-")
		} else {
			out = append(out, it.Title)
		}
	}
	return out
}

func TestVivaTrayMenuLabelsFollowVisibilityAndLanguage(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	r.shell.tray.Create()
	r.host.drain()
	menu := r.host.lastItem(t).Menu
	if got, want := menuTitles(menu()), []string{"新建知识条目…", "-", "隐藏归知", "检查更新…", "设置…", "-", "退出归知"}; !reflect.DeepEqual(got, want) {
		t.Errorf("可见时的菜单：%v", got)
	}
	r.host.Hide()
	if got := menuTitles(menu()); got[2] != "显示归知" {
		t.Errorf("窗口收到托盘后第三项应是「显示归知」：%v", got)
	}
	r.host.Show()
	r.host.minimized = true
	if got := menuTitles(menu()); got[2] != "显示归知" {
		t.Errorf("最小化的窗口也应显示「显示归知」：%v", got)
	}
	r.host.minimized = false
	if _, err := r.set.Update(func(v *settings.Values) { v.App.Language = settings.LangEn }); err != nil {
		t.Fatal(err)
	}
	if got, want := menuTitles(menu()), []string{"New Knowledge Item…", "-", "Hide GuiZhi", "Check for Updates…", "Settings…", "-", "Quit GuiZhi"}; !reflect.DeepEqual(got, want) {
		t.Errorf("英文菜单：%v", got)
	}
	// 每个可选项有互不相同的 ID（Windows 与 Linux 靠它认条目）。
	seen := map[string]bool{}
	for _, it := range menu() {
		if it.Separator {
			continue
		}
		if it.ID == "" || seen[it.ID] || it.OnSelect == nil {
			t.Errorf("条目 %q 的 ID 或动作不对：%q", it.Title, it.ID)
		}
		seen[it.ID] = true
	}
}

func TestVivaTrayMenuActions(t *testing.T) {
	pick := func(r *shellRig, title string) {
		t.Helper()
		for _, it := range r.host.lastItem(t).Menu() {
			if it.Title == title {
				it.OnSelect()
				return
			}
		}
		t.Fatalf("菜单里没有 %q", title)
	}
	setup := func() *shellRig {
		r := newShellRig(t, settings.CloseAsk, false)
		r.shell.tray.Create()
		r.host.drain()
		return r
	}

	r := setup()
	r.host.Hide()
	pick(r, "设置…")
	if !r.host.Visible() || r.host.count("show") == 0 {
		t.Error("托盘命令应先把窗口带到前台")
	}
	if got := r.host.commands(); !reflect.DeepEqual(got, []vivaapp.Command{vivaapp.CommandOpenSettings}) {
		t.Errorf("设置命令：%v", got)
	}
	pick(r, "新建知识条目…")
	pick(r, "检查更新…")
	if got := r.host.commands(); !reflect.DeepEqual(got, []vivaapp.Command{vivaapp.CommandOpenSettings, vivaapp.CommandNewItem, vivaapp.CommandOpenUpdater}) {
		t.Errorf("命令序列：%v", got)
	}

	r = setup()
	pick(r, "隐藏归知")
	if r.host.Visible() {
		t.Error("「隐藏归知」应当隐藏窗口")
	}
	pick(r, "显示归知")
	if !r.host.Visible() {
		t.Error("「显示归知」应当显示窗口")
	}

	r = setup()
	pick(r, "退出归知")
	if !r.quitting() || r.host.count("close") != 1 {
		t.Errorf("「退出」应当标记退出并走窗口关闭路径：%v", r.host.calls)
	}
	if r.shell.OnCloseRequest() {
		t.Error("退出过程中的关闭请求应当放行")
	}
}

func TestVivaShellPushesBecomeCommands(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	r.hub.Dispatch("app:command", map[string]any{"type": "updater:open"})
	r.hub.Dispatch("app:command", map[string]any{"type": "未知"})
	r.hub.Dispatch("app:command", "不是对象")
	r.hub.Dispatch("shortcut:triggered", "newItem")
	r.hub.Dispatch("shortcut:triggered", "search")
	r.hub.Dispatch("shortcut:triggered", "settings")
	r.hub.Dispatch("shortcut:triggered", "showApp") // 由主进程处理，界面不需要
	r.hub.Dispatch("shortcut:triggered", 42)
	want := []vivaapp.Command{vivaapp.CommandOpenUpdater, vivaapp.CommandShortcutNewItem, vivaapp.CommandFocusSearch, vivaapp.CommandOpenSettings}
	if got := r.host.commands(); !reflect.DeepEqual(got, want) {
		t.Errorf("推送到命令的映射：%v，期望 %v", got, want)
	}
	r.hub.Dispatch("window:showCloseDialog", nil)
	if r.host.count("close-dialog") != 1 {
		t.Error("window:showCloseDialog 应当让界面弹出关闭对话框")
	}
	r.shell.Shutdown()
	r.hub.Dispatch("app:command", map[string]any{"type": "settings:open"})
	if len(r.host.commands()) != len(want) {
		t.Error("Shutdown 之后不应再响应推送")
	}
}

func TestVivaShellTrayLabelsFallBackToChinese(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	r.set.Set(settings.Values{}) // 语言为空串
	if got := r.shell.trayLabels().QuitApp; got != "退出归知" {
		t.Errorf("读不到语言时按中文：%q", got)
	}
}

func TestParseGlobalAccelerator(t *testing.T) {
	ok := []struct {
		in   string
		mods gfx.Modifiers
		key  string
	}{
		{"Alt+Shift+P", gfx.ModifierAlt | gfx.ModifierShift, "P"},
		{"CommandOrControl+Shift+F5", gfx.ModifierControl | gfx.ModifierShift, "F5"},
		{"ctrl+alt+space", gfx.ModifierControl | gfx.ModifierAlt, "SPACE"},
		{"Control+Return", gfx.ModifierControl, "ENTER"},
		{"Alt+Esc", gfx.ModifierAlt, "ESCAPE"},
		{"Super+Tab", gfx.ModifierMeta, "TAB"},
		{" Alt + 7 ", gfx.ModifierAlt, "7"},
		{"CmdOrCtrl+Option+Meta+Shift+A", gfx.ModifierControl | gfx.ModifierAlt | gfx.ModifierMeta | gfx.ModifierShift, "A"},
	}
	for _, c := range ok {
		got, err := parseGlobalAccelerator(c.in)
		if err != nil || got.Modifiers != c.mods || got.Key != c.key {
			t.Errorf("%q → %+v, %v；期望 %v + %s", c.in, got, err, c.mods, c.key)
		}
	}
	for _, in := range []string{"", "P", "Alt+", "+P", "Alt+Shift+", "Hyper+P", "Alt++P"} {
		if sc, err := parseGlobalAccelerator(in); err == nil {
			t.Errorf("%q 应当解析失败，得到 %+v", in, sc)
		}
	}
}

func TestVivaShortcutNativeRegisterAndFire(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	var fired []func()
	var stops int
	n := newVivaShortcutNative(r.lib, r.lc.toggleWindow)
	n.registerKey = func(s vapp.GlobalShortcut, cb func()) (func(), error) {
		if s.Key == "BAD" {
			return nil, errors.New("被占用")
		}
		fired = append(fired, cb)
		return func() { stops++ }, nil
	}
	if !n.register("showApp", "Alt+Shift+P") || !n.register("search", "Alt+Shift+F") {
		t.Fatal("正常的组合键应当注册成功")
	}
	if n.register("settings", "Alt+Shift+BAD") || n.register("newItem", "") || n.register("newItem", "没有修饰键") {
		t.Error("注册失败、空串与不合法的组合键都应返回 false")
	}
	if len(fired) != 2 {
		t.Fatalf("应当注册了两个：%d", len(fired))
	}
	fired[0]() // showApp：切换显隐并推送
	if r.host.Visible() {
		t.Error("showApp 应当隐藏可见的窗口")
	}
	fired[1]() // search：只推送
	want := []string{"window:visibility-changed=false", "shortcut:triggered=showApp", "shortcut:triggered=search"}
	if got := r.pushed(); !reflect.DeepEqual(got, want) {
		t.Errorf("推送序列：%v，期望 %v", got, want)
	}
	n.unregisterAll()
	n.unregisterAll()
	if stops != 2 {
		t.Errorf("unregisterAll 应当撤销全部且只撤销一次：%d", stops)
	}
}

func TestVivaShortcutNativeUnsupportedKeyNeverReachesTheSystem(t *testing.T) {
	// 缺省注册函数是 Viva 的 RegisterGlobalShortcut；方向键 Viva 不支持，在校验处就被拒，不会碰系统。
	n := newVivaShortcutNative(&Library{}, nil)
	if n.register("showApp", "Alt+ARROWUP") {
		t.Error("Viva 不支持的主键应当注册失败")
	}
}

func TestVivaShellSystemShortcuts(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, true)
	// 默认配置里只有 showApp 是 global（其余是窗口内的局部快捷键）。
	r.mu.Lock()
	keys := append([]vapp.GlobalShortcut(nil), r.keys...)
	r.mu.Unlock()
	if len(keys) != 1 || keys[0].Key != "P" || keys[0].Modifiers != gfx.ModifierAlt|gfx.ModifierShift {
		t.Fatalf("默认应当只注册 showApp（Alt+Shift+P）：%+v", keys)
	}
	r.shell.Shutdown()
	r.mu.Lock()
	stops := r.stops
	r.mu.Unlock()
	if stops != 1 {
		t.Errorf("Shutdown 应当撤销系统级快捷键：%d", stops)
	}

	auto := newShellRig(t, settings.CloseAsk, false)
	auto.mu.Lock()
	n := len(auto.keys)
	auto.mu.Unlock()
	if n != 0 {
		t.Error("自动化实例不抢用户正在用的系统级快捷键")
	}
}

func TestVivaInstanceIDIsolatesDataDirs(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if vivaInstanceID(a) == vivaInstanceID(b) {
		t.Error("不同数据目录的锁名必须不同（验证实例与真实安装互不干扰）")
	}
	if vivaInstanceID(a) != vivaInstanceID(filepath.Join(a, ".")) || vivaInstanceID(a) != vivaInstanceID(toggleCase(a)) {
		t.Error("同一数据目录的不同写法（大小写、末尾的点）应得到同一个锁名")
	}
}

func toggleCase(s string) string {
	out := []rune(s)
	for i, c := range out {
		if c >= 'a' && c <= 'z' {
			out[i] = c - 32
		}
	}
	return string(out)
}

func TestClaimVivaInstanceHandsOverToFirst(t *testing.T) {
	dir := t.TempDir()
	first, primary, err := claimVivaInstance(dir, nil)
	if err != nil || !primary {
		t.Fatalf("第一个实例应当拿到锁：primary=%v err=%v", primary, err)
	}
	defer first.Close()
	// 窗口建好之前到来的后来者没有可聚焦的窗口：被忽略，不能崩。
	if second, primary, err := claimVivaInstance(dir, []string{"x"}); err != nil || primary || second != nil {
		t.Fatalf("第二个实例应当被拒并转交参数：primary=%v err=%v", primary, err)
	}
	launched := make(chan struct{}, 4)
	first.SetHandler(func() { launched <- struct{}{} })
	if _, primary, err := claimVivaInstance(dir, nil); err != nil || primary {
		t.Fatalf("第三个实例应当被拒：primary=%v err=%v", primary, err)
	}
	select {
	case <-launched:
	case <-time.After(5 * time.Second):
		t.Fatal("后来者启动时，第一个实例的处理函数应当被调用")
	}
	first.Close()
	again, primary, err := claimVivaInstance(dir, nil)
	if err != nil || !primary {
		t.Fatalf("释放之后应当能重新拿到锁：primary=%v err=%v", primary, err)
	}
	again.Close()
	(*vivaInstance)(nil).Close() // 对 nil 安全
}

func TestSecondInstanceBringsWindowToFront(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	r.host.Hide()
	r.host.minimized = true
	r.lc.onSecondInstance()
	if !r.host.Visible() || r.host.Minimized() {
		t.Error("后来者启动应当把隐藏的窗口带到前台")
	}
	if got := r.pushed(); len(got) == 0 || got[len(got)-1] != "window:visibility-changed=true" {
		t.Errorf("应当推送可见性变化：%v", got)
	}
}

func TestStatusIconFromPNG(t *testing.T) {
	icon, err := trayStatusIcon()
	if err != nil {
		t.Fatal(err)
	}
	if icon.Size != 32 || len(icon.RGBA) != 32*32*4 {
		t.Fatalf("托盘图标应是 32×32 的 RGBA：size=%d bytes=%d", icon.Size, len(icon.RGBA))
	}
	opaque := 0
	for i := 3; i < len(icon.RGBA); i += 4 {
		if icon.RGBA[i] != 0 {
			opaque++
		}
	}
	if opaque == 0 || opaque == 32*32 {
		t.Errorf("图标应当有不透明与透明两部分（透明度通道被正确保留）：不透明像素 %d", opaque)
	}
	if _, err := statusIconFromPNG([]byte("不是 PNG")); err == nil {
		t.Error("坏数据应当报错")
	}
}
