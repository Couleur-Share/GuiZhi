package overlays

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
)

type shortcutRig struct {
	*rig
	l     *LocalShortcuts
	svc   *servicestest.Shortcuts
	calls []string
}

func newShortcutRig(t *testing.T) *shortcutRig {
	t.Helper()
	sr := &shortcutRig{rig: newRig(t), svc: &servicestest.Shortcuts{}}
	l := NewLocalShortcuts(sr.guard)
	l.Env = sr.env()
	l.Service = sr.svc
	rec := func(name string) func() { return func() { sr.calls = append(sr.calls, name) } }
	l.Hooks = ShortcutHooks{ToggleWindow: rec("toggle"), NewItem: rec("newItem"), Search: rec("search"), Settings: rec("settings")}
	sr.l = l
	return sr
}

// chord 构造一次带修饰键的可打印字符按键（Viva 走 OnShortcut 的那一类）。
func chord(text string, mods gfx.Modifiers) gfx.KeyEvent {
	return gfx.KeyEvent{Key: gfx.KeyUnknown, Text: text, Modifiers: mods}
}

const altShift = gfx.ModifierAlt | gfx.ModifierShift

func TestCanonicalAccelerator(t *testing.T) {
	cases := map[string]string{
		"Alt+Shift+N":                  "Alt+Shift+N",
		"shift+alt+n":                  "Alt+Shift+N",
		"Ctrl+K":                       "CommandOrControl+K",
		"Control+Alt+K":                "CommandOrControl+Alt+K",
		"CmdOrCtrl+Shift+K":            "CommandOrControl+Shift+K",
		"CommandOrControl+Alt+Shift+K": "CommandOrControl+Alt+Shift+K",
		"Option+Up":                    "Alt+UP",
		"Alt+ArrowUp":                  "Alt+UP",
		"Alt+Return":                   "Alt+ENTER",
		"Ctrl+Ctrl+X":                  "CommandOrControl+X",
		" Alt + F5 ":                   "Alt+F5",
		"":                             "",
		"Alt":                          "",
		"N":                            "",
		"Alt+Shift":                    "",
		"Alt+A+B":                      "",
		"Alt++":                        "",
	}
	for in, want := range cases {
		if got := CanonicalAccelerator(in); got != want {
			t.Errorf("CanonicalAccelerator(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestLocalShortcutsDefaultsMatchLocalActions(t *testing.T) {
	sr := newShortcutRig(t)
	// 默认：showApp 是全局（局部不响应），newItem / search / settings 是局部
	for _, c := range []struct {
		text string
		want string
	}{{"n", "newItem"}, {"f", "search"}, {"s", "settings"}} {
		sr.calls = nil
		if !sr.l.Handle(chord(c.text, altShift), FocusNone) || !slices.Equal(sr.calls, []string{c.want}) {
			t.Errorf("Alt+Shift+%s 应分发 %s：%v", c.text, c.want, sr.calls)
		}
	}
	sr.calls = nil
	if sr.l.Handle(chord("p", altShift), FocusNone) || len(sr.calls) != 0 {
		t.Error("showApp 默认是全局模式，局部不应响应（交给系统级快捷键）")
	}
}

func TestLocalShortcutsUppercaseTextStillMatches(t *testing.T) {
	sr := newShortcutRig(t)
	if !sr.l.Handle(chord("N", altShift), FocusNone) || !slices.Equal(sr.calls, []string{"newItem"}) {
		t.Errorf("Shift 会让文字变成大写，仍应匹配：%v", sr.calls)
	}
}

func TestLocalShortcutsShowAppInLocalMode(t *testing.T) {
	sr := newShortcutRig(t)
	sr.l.Update(services.ShortcutConfig{
		Accelerators: services.DefaultShortcuts(),
		Modes:        map[string]string{services.ShortcutShowApp: services.ShortcutModeLocal},
	})
	if !sr.l.Handle(chord("p", altShift), FocusNone) || !slices.Equal(sr.calls, []string{"toggle"}) {
		t.Errorf("局部模式的 showApp 应切换窗口：%v", sr.calls)
	}
	if sr.l.Mode(services.ShortcutNewItem) != services.ShortcutModeLocal {
		t.Error("没设置模式的动作按局部")
	}
}

func TestLocalShortcutsGlobalModeIsNotHandledLocally(t *testing.T) {
	sr := newShortcutRig(t)
	sr.l.Update(services.ShortcutConfig{
		Accelerators: services.DefaultShortcuts(),
		Modes:        map[string]string{services.ShortcutNewItem: services.ShortcutModeGlobal},
	})
	if sr.l.Handle(chord("n", altShift), FocusNone) || len(sr.calls) != 0 {
		t.Error("全局模式由系统注册，窗口里不应再分发一次")
	}
	sr.l.Trigger(services.ShortcutNewItem) // 系统级触发到达
	if !slices.Equal(sr.calls, []string{"newItem"}) {
		t.Errorf("全局触发应分发：%v", sr.calls)
	}
	sr.calls = nil
	sr.l.Trigger(services.ShortcutShowApp)
	if len(sr.calls) != 0 {
		t.Error("showApp 的全局触发由外壳自己处理，不应再切换一次")
	}
	sr.l.Trigger("unknown-action") // 不崩溃
}

func TestLocalShortcutsCustomAndClearedAccelerators(t *testing.T) {
	sr := newShortcutRig(t)
	sr.l.Update(services.ShortcutConfig{Accelerators: map[string]string{
		services.ShortcutNewItem:  "ctrl+shift+k",
		services.ShortcutSearch:   "",
		services.ShortcutSettings: "Alt+F5",
	}})
	if !sr.l.Handle(chord("k", gfx.ModifierControl|gfx.ModifierShift), FocusNone) || !slices.Equal(sr.calls, []string{"newItem"}) {
		t.Errorf("自定义组合键应生效（别名归一）：%v", sr.calls)
	}
	sr.calls = nil
	if sr.l.Handle(chord("f", altShift), FocusNone) || len(sr.calls) != 0 {
		t.Error("清空的组合键不应再匹配旧的默认值")
	}
	if !sr.l.Handle(gfx.KeyEvent{Key: gfx.KeyF5, Modifiers: gfx.ModifierAlt}, FocusNone) || !slices.Equal(sr.calls, []string{"settings"}) {
		t.Errorf("具名键（OnKey 路）也应匹配：%v", sr.calls)
	}
	// Meta 与 Control 都对应 CommandOrControl
	sr.calls = nil
	if !sr.l.Handle(chord("k", gfx.ModifierMeta|gfx.ModifierShift), FocusNone) {
		t.Error("Meta 应等价于 CommandOrControl")
	}
}

func TestLocalShortcutsFocusPassThrough(t *testing.T) {
	sr := newShortcutRig(t)
	if sr.l.Handle(chord("n", altShift), FocusRecorder) || len(sr.calls) != 0 {
		t.Error("录制框里一律不匹配：录制框要收下包括已有快捷键在内的所有组合")
	}
	if !sr.l.Handle(chord("n", altShift), FocusTextInput) || !slices.Equal(sr.calls, []string{"newItem"}) {
		t.Errorf("输入框里带 Alt 的组合不是在打字，应响应：%v", sr.calls)
	}

	// 仅 Shift（打大写字母）在输入框里必须放行，即使被设成了快捷键
	sr.l.Update(services.ShortcutConfig{Accelerators: map[string]string{services.ShortcutSearch: "Shift+X"}})
	sr.calls = nil
	if sr.l.Handle(chord("X", gfx.ModifierShift), FocusTextInput) || len(sr.calls) != 0 {
		t.Error("输入框里打大写字母不应触发")
	}
	if !sr.l.Handle(chord("X", gfx.ModifierShift), FocusNone) {
		t.Error("没有输入焦点时仅 Shift 的组合键仍按配置响应")
	}
}

func TestLocalShortcutsIgnoreReleaseAndSwallowRepeat(t *testing.T) {
	sr := newShortcutRig(t)
	rel := chord("n", altShift)
	rel.Release = true
	if sr.l.Handle(rel, FocusNone) || len(sr.calls) != 0 {
		t.Error("松开事件不处理")
	}
	rep := chord("n", altShift)
	rep.Repeat = true
	if !sr.l.Handle(rep, FocusNone) || len(sr.calls) != 0 {
		t.Error("按住不放的重复应消化但不重复触发")
	}
}

func TestLocalShortcutsPlainAndUnknownKeysPassThrough(t *testing.T) {
	sr := newShortcutRig(t)
	for _, e := range []gfx.KeyEvent{
		chord("n", 0),                                 // 不带修饰键
		chord("é", gfx.ModifierAlt),                   // 非 ASCII
		chord("", gfx.ModifierAlt),                    // 没有文字
		{Key: gfx.KeyEnter},                           // 具名键无修饰
		chord("z", gfx.ModifierControl),               // 没配置的组合
		chord("n", gfx.ModifierAlt),                   // 缺 Shift
		chord("n", altShift|gfx.ModifierControl),      // 多了 Ctrl
		{Key: gfx.KeyTab, Modifiers: gfx.ModifierAlt}, // 未配置的具名键
	} {
		if sr.l.Handle(e, FocusNone) {
			t.Errorf("不应匹配：%+v", e)
		}
	}
	if len(sr.calls) != 0 {
		t.Errorf("不应分发：%v", sr.calls)
	}
}

func TestLocalShortcutsSameChordOnTwoActionsDispatchesInStableOrder(t *testing.T) {
	sr := newShortcutRig(t)
	sr.l.Update(services.ShortcutConfig{Accelerators: map[string]string{
		services.ShortcutSettings: "Alt+Shift+Q", services.ShortcutNewItem: "Alt+Shift+Q",
	}})
	sr.l.Handle(chord("q", altShift), FocusNone)
	if !slices.Equal(sr.calls, []string{"newItem", "settings"}) {
		t.Errorf("同一组合键绑了两个动作时应按固定顺序都分发：%v", sr.calls)
	}
}

func TestLocalShortcutsReload(t *testing.T) {
	sr := newShortcutRig(t)
	sr.svc.Set(services.ShortcutConfig{
		Accelerators: map[string]string{services.ShortcutSearch: "Alt+Shift+G"},
		Modes:        map[string]string{services.ShortcutSearch: services.ShortcutModeLocal},
	})
	sr.l.Reload()
	if !sr.l.Handle(chord("g", altShift), FocusNone) || !slices.Equal(sr.calls, []string{"search"}) {
		t.Errorf("重新读取后应使用已保存的配置：%v", sr.calls)
	}
	sr.calls = nil
	if sr.l.Handle(chord("f", altShift), FocusNone) {
		t.Error("旧的默认组合键不再有效")
	}
}

func TestLocalShortcutsReloadFailureKeepsCurrentConfig(t *testing.T) {
	sr := newShortcutRig(t)
	sr.svc.FailGet(errors.New("shortcuts file unreadable"))
	sr.l.Reload()
	if !sr.l.Handle(chord("n", altShift), FocusNone) {
		t.Error("读取失败应保留当前（默认）配置")
	}
	if logs := sr.log.entries(); len(logs) != 1 || !strings.Contains(logs[0], "shortcuts file unreadable") {
		t.Errorf("应留日志：%v", logs)
	}
	if len(sr.toasts()) != 0 {
		t.Error("后台重读失败不弹提示")
	}
	sr.l.Service = nil
	sr.l.Reload() // 没接服务：无事可做
}

func TestLocalShortcutsNilHooksStillConsume(t *testing.T) {
	sr := newShortcutRig(t)
	sr.l.Hooks = ShortcutHooks{}
	if !sr.l.Handle(chord("n", altShift), FocusNone) {
		t.Error("钩子为空时匹配到的按键仍应被消化")
	}
}
