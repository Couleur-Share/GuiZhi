package main

// 生命周期策略的单测：用假窗口/假托盘固定关闭行为、托盘命令派发、IPC 参数校验。
// 这些逻辑在 TS 里位于 index.ts 内部，无法直接差分，期望值按 index.ts / tray-command-dispatcher.ts 的分支手写。

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

type fakeWin struct {
	calls     []string
	visible   bool
	minimized bool
	loading   bool
	onLoaded  []func()
}

func (w *fakeWin) rec(s string) { w.calls = append(w.calls, s) }
func (w *fakeWin) Show()        { w.rec("show"); w.visible = true }
func (w *fakeWin) Hide()        { w.rec("hide"); w.visible = false }
func (w *fakeWin) Focus()       { w.rec("focus") }
func (w *fakeWin) Close()       { w.rec("close") }
func (w *fakeWin) Restore()     { w.rec("restore"); w.minimized = false }
func (w *fakeWin) IsVisible() bool {
	return w.visible
}
func (w *fakeWin) IsMinimized() bool    { return w.minimized }
func (w *fakeWin) IsLoading() bool      { return w.loading }
func (w *fakeWin) OnceLoaded(fn func()) { w.onLoaded = append(w.onLoaded, fn) }

type fakeTray struct{ calls []string }

func (t *fakeTray) Create()  { t.calls = append(t.calls, "create") }
func (t *fakeTray) Destroy() { t.calls = append(t.calls, "destroy") }
func (t *fakeTray) Refresh() { t.calls = append(t.calls, "refresh") }

type pushed struct {
	Channel string
	Payload any
}

type lcRig struct {
	lc     *appLifecycle
	win    *fakeWin
	tray   *fakeTray
	b      *Bridge
	pushes []pushed
	quits  int
	login  []bool
}

func newLcRig(isWindows bool) *lcRig {
	r := &lcRig{win: &fakeWin{visible: true}, tray: &fakeTray{}}
	r.lc = newAppLifecycle(isWindows)
	r.lc.win, r.lc.tray = r.win, r.tray
	r.lc.push = func(ch string, p any) { r.pushes = append(r.pushes, pushed{ch, p}) }
	r.lc.quit = func() { r.quits++ }
	r.lc.setLoginItem = func(open bool) error { r.login = append(r.login, open); return nil }
	r.b = &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}}
	r.lc.register(r.b)
	return r
}

func (r *lcRig) send(ch string, args ...any) {
	var raws []json.RawMessage
	for _, a := range args {
		j, _ := json.Marshal(a)
		raws = append(raws, j)
	}
	f := r.b.sends[ch]
	if f == nil {
		panic("未注册的 send 频道 " + ch)
	}
	f(raws)
}

func TestCloseActionPolicyWindows(t *testing.T) {
	t.Run("ask：第一次拦下并弹对话框，对话框未决时再次关闭放行", func(t *testing.T) {
		r := newLcRig(true)
		if !r.lc.onClose() {
			t.Fatal("ask 模式第一次关闭应被拦下")
		}
		if len(r.pushes) != 1 || r.pushes[0].Channel != "window:showCloseDialog" {
			t.Fatalf("应推送关闭对话框：%+v", r.pushes)
		}
		// index.ts：closeAction==="ask" && !pendingCloseAction 才拦；对话框未决时再点关闭会真的关掉
		if r.lc.onClose() {
			t.Fatal("对话框未决时第二次关闭应放行")
		}
	})
	t.Run("minimize：隐藏并拦下", func(t *testing.T) {
		r := newLcRig(true)
		r.send("app:setCloseAction", "minimize")
		if !r.lc.onClose() || !reflect.DeepEqual(r.win.calls, []string{"hide"}) {
			t.Fatalf("calls=%v", r.win.calls)
		}
	})
	t.Run("exit：直接放行", func(t *testing.T) {
		r := newLcRig(true)
		r.send("app:setCloseAction", "exit")
		if r.lc.onClose() || len(r.win.calls) != 0 || len(r.pushes) != 0 {
			t.Fatal("exit 模式应直接关闭")
		}
	})
	t.Run("退出流程中一律放行", func(t *testing.T) {
		r := newLcRig(true)
		r.lc.setQuitting()
		if r.lc.onClose() || len(r.pushes) != 0 {
			t.Fatal("quitting 时不能再拦")
		}
	})
	t.Run("minimizeToTray 在 Windows 上不参与关闭决策", func(t *testing.T) {
		r := newLcRig(true)
		r.send("app:setCloseAction", "exit")
		r.send("app:setMinimizeToTray", true)
		if r.lc.onClose() {
			t.Fatal("Windows 只看 closeAction")
		}
	})
}

func TestCloseActionPolicyOtherPlatforms(t *testing.T) {
	r := newLcRig(false)
	if r.lc.onClose() {
		t.Fatal("非 Windows 默认直接关闭（ask 只对 Windows 生效）")
	}
	r.send("app:setMinimizeToTray", true)
	if !r.lc.onClose() || !reflect.DeepEqual(r.win.calls, []string{"hide"}) {
		t.Fatalf("开了最小化到托盘应隐藏：%v", r.win.calls)
	}
	if !reflect.DeepEqual(r.tray.calls, []string{"create"}) {
		t.Fatalf("托盘调用：%v", r.tray.calls)
	}
	r.send("app:setMinimizeToTray", false)
	if r.tray.calls[len(r.tray.calls)-1] != "destroy" {
		t.Fatalf("关闭后应销毁托盘：%v", r.tray.calls)
	}
}

func TestSetCloseActionAndTrayCreation(t *testing.T) {
	r := newLcRig(true)
	for _, bad := range []any{"bogus", 5, nil, true, "ASK"} {
		r.send("app:setCloseAction", bad)
	}
	if r.lc.closeAction != "ask" || len(r.tray.calls) != 0 {
		t.Fatalf("非法 action 必须忽略：%s %v", r.lc.closeAction, r.tray.calls)
	}
	r.send("app:setCloseAction", "exit")
	if r.lc.closeAction != "exit" || len(r.tray.calls) != 0 {
		t.Fatal("exit 不建托盘")
	}
	r.send("app:setCloseAction", "minimize")
	if !reflect.DeepEqual(r.tray.calls, []string{"create"}) {
		t.Fatalf("Windows 上 minimize 要建托盘：%v", r.tray.calls)
	}
	o := newLcRig(false)
	o.send("app:setCloseAction", "minimize")
	if len(o.tray.calls) != 0 {
		t.Fatal("非 Windows 上 minimize 不建托盘")
	}
}

func TestCloseDialogResult(t *testing.T) {
	arm := func(r *lcRig) { r.lc.onClose(); r.pushes = nil }
	t.Run("minimize：隐藏、建托盘、复位等待标记", func(t *testing.T) {
		r := newLcRig(true)
		arm(r)
		r.send("window:closeDialogResult", map[string]any{"action": "minimize", "remember": false})
		if !reflect.DeepEqual(r.win.calls, []string{"hide"}) || !reflect.DeepEqual(r.tray.calls, []string{"create"}) {
			t.Fatalf("win=%v tray=%v", r.win.calls, r.tray.calls)
		}
		if r.lc.pendingCloseAction || r.lc.closeAction != "ask" {
			t.Fatal("未勾选记住时 closeAction 不变")
		}
	})
	t.Run("exit + remember：记住、置退出并关窗", func(t *testing.T) {
		r := newLcRig(true)
		arm(r)
		r.send("window:closeDialogResult", map[string]any{"action": "exit", "remember": true})
		if r.lc.closeAction != "exit" || !r.lc.isQuitting() || !reflect.DeepEqual(r.win.calls, []string{"close"}) {
			t.Fatalf("action=%s quitting=%v calls=%v", r.lc.closeAction, r.lc.isQuitting(), r.win.calls)
		}
		if r.lc.onClose() {
			t.Fatal("置退出后关闭不应再被拦")
		}
	})
	t.Run("remember 按 JS 真值：1 / 非空串都算记住", func(t *testing.T) {
		for _, v := range []any{1, "yes", true} {
			r := newLcRig(true)
			arm(r)
			r.send("window:closeDialogResult", map[string]any{"action": "minimize", "remember": v})
			if r.lc.closeAction != "minimize" {
				t.Fatalf("remember=%v 应记住", v)
			}
		}
		for _, v := range []any{0, "", nil, false} {
			r := newLcRig(true)
			arm(r)
			r.send("window:closeDialogResult", map[string]any{"action": "minimize", "remember": v})
			if r.lc.closeAction != "ask" {
				t.Fatalf("remember=%v 不应记住", v)
			}
		}
	})
	t.Run("非法载荷只复位等待标记，不动窗口", func(t *testing.T) {
		for _, bad := range []any{nil, "x", 5, []any{}, map[string]any{}, map[string]any{"action": "ask"}, map[string]any{"action": 3}} {
			r := newLcRig(true)
			arm(r)
			r.send("window:closeDialogResult", bad)
			if r.lc.pendingCloseAction || len(r.win.calls) != 0 || len(r.tray.calls) != 0 || r.lc.isQuitting() {
				t.Fatalf("载荷 %v 处理有误：pending=%v win=%v", bad, r.lc.pendingCloseAction, r.win.calls)
			}
		}
	})
	t.Run("取消：复位后下次关闭会再次弹出", func(t *testing.T) {
		r := newLcRig(true)
		arm(r)
		r.send("window:closeDialogCancel")
		if !r.lc.onClose() {
			t.Fatal("取消后再次关闭应再次拦下")
		}
	})
}

func TestAutoLaunchAndDebugMode(t *testing.T) {
	r := newLcRig(true)
	r.send("app:setAutoLaunch", true, true)
	r.send("app:setAutoLaunch", false)
	for _, bad := range []any{"true", 1, nil} {
		r.send("app:setAutoLaunch", bad)
	}
	if !reflect.DeepEqual(r.login, []bool{true, false}) {
		t.Fatalf("登录项调用：%v", r.login)
	}
	r.lc.setLoginItem = func(bool) error { return errors.New("注册表被拒绝") }
	r.send("app:setAutoLaunch", true) // 失败只记日志，不能抛
	r.send("app:setDebugMode", 1)
	if !r.lc.debugMode {
		t.Fatal("setDebugMode 取 JS 真值")
	}
	r.send("app:setDebugMode", 0)
	if r.lc.debugMode {
		t.Fatal("setDebugMode(0) 应关闭")
	}
}

func TestToggleAndTrayCommands(t *testing.T) {
	t.Run("toggle：可见→隐藏并推送 false 且刷新托盘", func(t *testing.T) {
		r := newLcRig(true)
		r.send("window:toggleVisibility")
		if !reflect.DeepEqual(r.win.calls, []string{"hide"}) {
			t.Fatalf("%v", r.win.calls)
		}
		if len(r.pushes) != 1 || r.pushes[0] != (pushed{"window:visibility-changed", false}) || !reflect.DeepEqual(r.tray.calls, []string{"refresh"}) {
			t.Fatalf("pushes=%+v tray=%v", r.pushes, r.tray.calls)
		}
	})
	t.Run("toggle：最小化→还原显示聚焦", func(t *testing.T) {
		r := newLcRig(true)
		r.win.minimized = true
		r.lc.toggleWindow()
		if !reflect.DeepEqual(r.win.calls, []string{"restore", "show", "focus"}) || r.pushes[0].Payload != true {
			t.Fatalf("%v %+v", r.win.calls, r.pushes)
		}
	})
	t.Run("命令：最小化的窗口先还原再显示聚焦，最后发命令", func(t *testing.T) {
		r := newLcRig(true)
		r.win.minimized, r.win.visible = true, false
		if !r.lc.dispatchAppCommand(map[string]any{"type": "settings:open"}) {
			t.Fatal("应成功派发")
		}
		if !reflect.DeepEqual(r.win.calls, []string{"restore", "show", "focus"}) {
			t.Fatalf("%v", r.win.calls)
		}
		want := []pushed{{"window:visibility-changed", true}, {"app:command", map[string]any{"type": "settings:open"}}}
		if !reflect.DeepEqual(r.pushes, want) {
			t.Fatalf("pushes=%+v", r.pushes)
		}
	})
	t.Run("命令：页面加载中先不发，加载完成后才发", func(t *testing.T) {
		r := newLcRig(true)
		r.win.loading = true
		r.lc.dispatchAppCommand(map[string]any{"type": "item:new"})
		for _, p := range r.pushes {
			if p.Channel == "app:command" {
				t.Fatal("加载中不应发命令")
			}
		}
		if len(r.win.onLoaded) != 1 {
			t.Fatal("应登记加载完成回调")
		}
		r.win.onLoaded[0]()
		if last := r.pushes[len(r.pushes)-1]; last.Channel != "app:command" {
			t.Fatalf("加载完成后应发命令：%+v", last)
		}
	})
	t.Run("命令：没有窗口返回 false", func(t *testing.T) {
		r := newLcRig(true)
		r.lc.win = nil
		if r.lc.dispatchAppCommand(map[string]any{"type": "item:new"}) {
			t.Fatal("没有窗口不能派发")
		}
	})
	t.Run("退出：有窗口走关闭路径，没有窗口直接退出", func(t *testing.T) {
		r := newLcRig(true)
		r.lc.trayQuit()
		if !r.lc.isQuitting() || !reflect.DeepEqual(r.win.calls, []string{"close"}) || r.quits != 0 {
			t.Fatalf("calls=%v quits=%d", r.win.calls, r.quits)
		}
		r2 := newLcRig(true)
		r2.lc.win = nil
		r2.lc.trayQuit()
		if r2.quits != 1 {
			t.Fatal("没有窗口应直接退出应用")
		}
	})
	t.Run("第二实例：带到前台并推送可见", func(t *testing.T) {
		r := newLcRig(true)
		r.win.minimized, r.win.visible = true, false
		r.lc.onSecondInstance()
		if !reflect.DeepEqual(r.win.calls, []string{"restore", "show", "focus"}) || r.pushes[0] != (pushed{"window:visibility-changed", true}) {
			t.Fatalf("%v %+v", r.win.calls, r.pushes)
		}
	})
}

func newSettingsLib(t *testing.T, rows map[string]string) *Library {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "s.db"), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Execute(ctx, "CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	for k, v := range rows {
		if _, err := db.Execute(ctx, "INSERT INTO settings VALUES (?, ?)", k, v); err != nil {
			t.Fatal(err)
		}
	}
	return &Library{db: db}
}

func TestSettingReaders(t *testing.T) {
	ctx := context.Background()
	l := newSettingsLib(t, map[string]string{
		"minimizeOnLaunch": "true", "bad": "{oops", "str": `"true"`, "no": "false", "num": "1",
		"language": `"zh"`,
	})
	cases := []struct {
		key        string
		val, found bool
	}{{"minimizeOnLaunch", true, true}, {"bad", false, true}, {"str", false, true}, {"no", false, true}, {"num", false, true}, {"absent", false, false}}
	for _, c := range cases {
		if v, f := l.readBoolSetting(ctx, c.key); v != c.val || f != c.found {
			t.Errorf("%s: got (%v,%v) want (%v,%v)", c.key, v, f, c.val, c.found)
		}
	}
	langs := map[string]string{`"zh"`: "zh", `"en"`: "en", `"fr"`: "", "zh": "zh", "ja": "", `5`: "", `null`: "", `{oops`: ""}
	for raw, want := range langs {
		l2 := newSettingsLib(t, map[string]string{"language": raw})
		if got := l2.readLanguageSetting(ctx); got != want {
			t.Errorf("language %q: got %q want %q", raw, got, want)
		}
	}
	if got := newSettingsLib(t, nil).readLanguageSetting(ctx); got != "" {
		t.Errorf("没有 language 设置应返回空：%q", got)
	}
	if trayLocale("zh", "en-US") != "zh" || trayLocale("", "en-US") != "en-US" || trayLocale("  ", "ja") != "ja" {
		t.Error("trayLocale 优先用已存语言，空白回退系统区域")
	}
}
