package kit

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/locales"
)

const fieldAction = "全局搜索"

var fieldLabel = locales.SettingsShortcutInputLabel(fieldAction)

// newFieldTester 起一个前后各有一个按钮的窗口，中间画 build 的内容：用来验证 Tab 把焦点交给前后的控件。
func newFieldTester(t *testing.T, l *Look, build func(c *ui.Context)) *ui.Tester {
	t.Helper()
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(l.Theme)
		ui.Column(c).Fill().Background(l.T.Background).Padding(24).Gap(16).AlignItems(ui.Start).Children(func() {
			ui.ButtonBase(c).Label("页面按钮").Padding(8, 16).Children(func() { ui.Text(c, "页面按钮") })
			build(c)
			ui.ButtonBase(c).Label("后一个按钮").Padding(8, 16).Children(func() { ui.Text(c, "后一个按钮") })
		})
	}, 900, 500)
	tr.Frame()
	return tr
}

// fieldHarness 把一个录制框接到一个可改的当前值上。
type fieldHarness struct {
	f       *ShortcutField
	value   string
	changes []string
	cleared int
}

func newFieldHarness(value string) *fieldHarness {
	return &fieldHarness{f: NewShortcutField(), value: value}
}

func (h *fieldHarness) view(c *ui.Context, l *Look) {
	h.f.View(c, l, ShortcutFieldOptions{
		Label: fieldAction, Value: h.value,
		OnChange: func(acc string) { h.changes = append(h.changes, acc); h.value = acc },
		OnClear:  func() { h.cleared++; h.value = "" },
	})
}

// resetCapture 防止一个失败的测试把全局的录制计数留给后面的测试。
func resetCapture(t *testing.T) {
	t.Helper()
	keyCapture.Store(0)
	t.Cleanup(func() { keyCapture.Store(0) })
}

func TestShortcutFieldShowsValueAndNotSet(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("CommandOrControl+Shift+K")
	tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })

	if !tr.HasText("Ctrl + Shift + K") {
		t.Fatalf("应显示格式化后的组合：%v", tr.Texts())
	}
	a, ok := tr.AccessByRole(ui.RoleButton, fieldLabel)
	if !ok || a.Description != "Ctrl + Shift + K" {
		t.Errorf("读屏应读到输入框名与当前组合：%+v ok=%v", a, ok)
	}
	if _, ok := tr.AccessByRole(ui.RoleButton, locales.SettingsClearShortcutFor(fieldAction)); !ok {
		t.Error("已有值时应有清除钮，且读屏名含动作名")
	}

	h.value = ""
	tr.Frame()
	if !tr.HasText(locales.SettingsNotSet()) {
		t.Errorf("没有值时应显示「%s」：%v", locales.SettingsNotSet(), tr.Texts())
	}
	if _, ok := tr.AccessByRole(ui.RoleButton, locales.SettingsClearShortcutFor(fieldAction)); ok {
		t.Error("没有值时不应有清除钮")
	}
}

func TestShortcutFieldRecordsACombination(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("Alt+Shift+P")
	tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })

	if err := tr.Click(fieldLabel); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if !h.f.Recording() || !KeyCaptureActive() {
		t.Fatalf("点击应开始录制：录制 %v，捕获 %v", h.f.Recording(), KeyCaptureActive())
	}
	if !tr.HasText(locales.SettingsPressKeys()) {
		t.Errorf("录制中应提示「%s」：%v", locales.SettingsPressKeys(), tr.Texts())
	}

	tr.Key(ui.Ctrl|ui.Shift, ui.KeyK)
	tr.Frame()
	if len(h.changes) != 1 || h.changes[0] != "CommandOrControl+Shift+K" {
		t.Fatalf("应录到 CommandOrControl+Shift+K：%v", h.changes)
	}
	if h.f.Recording() || KeyCaptureActive() {
		t.Error("录完应结束录制并释放捕获")
	}
	if !tr.HasText("Ctrl + Shift + K") {
		t.Errorf("应显示新组合：%v", tr.Texts())
	}
}

func TestShortcutFieldNeedsAModifier(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("")
	tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })
	_ = tr.Click(fieldLabel)

	tr.Key(0, ui.KeyK)
	tr.Frame()
	if len(h.changes) != 0 || !h.f.Recording() {
		t.Fatalf("单个字母不是快捷键，应继续录制：%v 录制 %v", h.changes, h.f.Recording())
	}
	if !tr.HasText("K") {
		t.Errorf("应显示已按下的键提示继续：%v", tr.Texts())
	}
	tr.Key(ui.Alt, ui.KeyK)
	tr.Frame()
	if len(h.changes) != 1 || h.changes[0] != "Alt+K" {
		t.Errorf("补上修饰键后应录到 Alt+K：%v", h.changes)
	}
}

func TestShortcutFieldRecordsAcceleratorsMyGoAccepts(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	cases := []struct {
		mods ui.Modifiers
		key  ui.Key
		want string
	}{
		{ui.Alt | ui.Shift, ui.KeyUp, "Alt+Shift+Up"}, // 渲染层录出的是非法的 ARROWUP
		{ui.Ctrl, ui.KeyEnter, "CommandOrControl+Enter"},
		{ui.Ctrl, ui.KeySpace, "CommandOrControl+Space"},
		{ui.Super, ui.KeyK, "Meta+K"}, // 渲染层把 Win 键当成 Ctrl
		{ui.Ctrl | ui.Alt, ui.KeyF9, "CommandOrControl+Alt+F9"},
		{ui.Ctrl, ui.KeyEscape, "CommandOrControl+Escape"},
	}
	for _, c := range cases {
		h := newFieldHarness("")
		tr := newFieldTester(t, l, func(cx *ui.Context) { h.view(cx, l) })
		_ = tr.Click(fieldLabel)
		tr.Key(c.mods, c.key)
		tr.Frame()
		if len(h.changes) != 1 || h.changes[0] != c.want {
			t.Errorf("%v+%v：期望 %q，得到 %v", c.mods, c.key, c.want, h.changes)
		}
	}
}

func TestShortcutFieldLayoutIndependentKey(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("")
	tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })
	_ = tr.Click(fieldLabel)
	tr.KeyAt(ui.Alt|ui.Shift, ui.KeyS) // 西里尔布局：Key 未知，物理位置是 S
	tr.Frame()
	if len(h.changes) != 1 || h.changes[0] != "Alt+Shift+S" {
		t.Errorf("布局打不出 US 键时应按物理位置录：%v", h.changes)
	}
}

func TestShortcutFieldEscapeCancels(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("Alt+Shift+P")
	tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })
	_ = tr.Click(fieldLabel)
	tr.Key(0, ui.KeyEscape)
	tr.Frame()
	if h.f.Recording() || KeyCaptureActive() {
		t.Error("Esc 应取消录制")
	}
	if len(h.changes) != 0 || h.value != "Alt+Shift+P" {
		t.Errorf("取消不应改动原值：%v %q", h.changes, h.value)
	}
	if !tr.HasText("Alt + Shift + P") {
		t.Errorf("取消后应恢复显示原值：%v", tr.Texts())
	}
}

func TestShortcutFieldTabLeavesInsteadOfTrapping(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("Alt+Shift+P")
	tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })
	_ = tr.Click(fieldLabel)
	tr.Key(0, ui.KeyTab)
	tr.Frame()
	if h.f.Recording() || KeyCaptureActive() {
		t.Fatal("Tab 应结束录制")
	}
	if len(h.changes) != 0 {
		t.Errorf("Tab 不是要录的键：%v", h.changes)
	}
	// 有值时输入框后面紧跟清除钮，焦点交给它
	if !tr.Focused(locales.SettingsClearShortcutFor(fieldAction)) {
		t.Error("Tab 应把焦点交给下一个控件，而不是留在输入框里")
	}

	// 反方向：Shift+Tab 回到前一个控件
	_ = tr.Click(fieldLabel)
	tr.Key(ui.Shift, ui.KeyTab)
	tr.Frame()
	if h.f.Recording() || !tr.Focused("页面按钮") {
		t.Errorf("Shift+Tab 应结束录制并把焦点交给前一个控件：录制 %v", h.f.Recording())
	}

	// 没有值时没有清除钮：Tab 直接到后面的按钮
	h.value = ""
	tr.Frame()
	_ = tr.Click(fieldLabel)
	tr.Key(0, ui.KeyTab)
	tr.Frame()
	if h.f.Recording() || !tr.Focused("后一个按钮") {
		t.Errorf("没有清除钮时 Tab 应到后一个按钮：录制 %v", h.f.Recording())
	}
}

func TestShortcutFieldKeyboardStartsRecordingWithEnter(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("")
	tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })

	// Tab 经过：焦点到了输入框，但不录制（渲染层是「获得焦点就录」，Tab 进来就出不去）
	_ = tr.Click("页面按钮")
	tr.Key(0, ui.KeyTab)
	tr.Frame()
	if !tr.Focused(fieldLabel) {
		t.Fatal("Tab 应把焦点交给录制框")
	}
	if h.f.Recording() {
		t.Fatal("仅获得焦点不应开始录制")
	}
	tr.Key(0, ui.KeyEnter)
	tr.Frame()
	if !h.f.Recording() {
		t.Fatal("在焦点上按 Enter 应开始录制")
	}
	if len(h.changes) != 0 {
		t.Errorf("启动录制的那下 Enter 不是要录的键：%v", h.changes)
	}
	tr.Key(ui.Ctrl, ui.KeyJ)
	tr.Frame()
	if len(h.changes) != 1 || h.changes[0] != "CommandOrControl+J" {
		t.Errorf("录到：%v", h.changes)
	}
}

func TestShortcutFieldShowsHeldModifiers(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("")
	tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })
	_ = tr.Click(fieldLabel)

	tr.CodeDown(ui.Ctrl, ui.CodeControlLeft)
	tr.Frame()
	if !tr.HasText("Ctrl") {
		t.Errorf("按住 Ctrl 应显示：%v", tr.Texts())
	}
	tr.CodeDown(ui.Ctrl|ui.Shift, ui.CodeShiftLeft)
	tr.Frame()
	if !tr.HasText("Ctrl + Shift") {
		t.Errorf("再按住 Shift 应显示：%v", tr.Texts())
	}
	tr.CodeUp(ui.Ctrl, ui.CodeShiftLeft)
	tr.Frame()
	if !tr.HasText("Ctrl") || tr.HasText("Ctrl + Shift") {
		t.Errorf("松开 Shift 应回退：%v", tr.Texts())
	}
	if !h.f.Recording() || len(h.changes) != 0 {
		t.Error("只按修饰键不应结束录制或录到东西")
	}
}

func TestShortcutFieldBlurCancels(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("Alt+Shift+P")
	tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })

	_ = tr.Click(fieldLabel)
	_ = tr.Click("页面按钮")
	tr.Frame()
	if h.f.Recording() || KeyCaptureActive() {
		t.Error("点到别处应取消录制")
	}

	_ = tr.Click(fieldLabel)
	tr.SetFocused(false) // 切到别的窗口
	tr.Frame()
	if h.f.Recording() || KeyCaptureActive() {
		t.Error("窗口失去焦点应取消录制：回来时不能还停在录制态")
	}
}

func TestShortcutFieldUnmountReleasesCapture(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("")
	show := true
	tr := newFieldTester(t, l, func(c *ui.Context) {
		if show {
			h.view(c, l)
		}
	})
	_ = tr.Click(fieldLabel)
	if !KeyCaptureActive() {
		t.Fatal("应在录制")
	}
	show = false // 设置页换走了：录制框不再被画出
	tr.Frame()
	tr.Frame()
	if KeyCaptureActive() {
		t.Error("录制框消失后必须释放捕获，否则应用的窗口快捷键会被永远关掉")
	}
}

func TestShortcutFieldClearButton(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("Alt+Shift+P")
	tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })
	if err := tr.Click(locales.SettingsClearShortcutFor(fieldAction)); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if h.cleared != 1 || h.value != "" {
		t.Errorf("清除：次数 %d，值 %q", h.cleared, h.value)
	}
	if !tr.HasText(locales.SettingsNotSet()) {
		t.Errorf("清除后应显示「%s」：%v", locales.SettingsNotSet(), tr.Texts())
	}
}

func TestAppShortcutsStandDownWhileRecording(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("")
	opened := 0
	guarded := true
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(l.Theme)
		// 应用层的窗口快捷键：与 kit.KeyCaptureActive 配合的约定写法
		if (!guarded || !KeyCaptureActive()) && c.Shortcut(ui.Ctrl, ui.KeyK) {
			opened++
		}
		ui.Column(c).Fill().Background(l.T.Background).Padding(24).Children(func() { h.view(c, l) })
	}, 900, 500)
	tr.Frame()

	tr.Key(ui.Ctrl, ui.KeyK)
	if opened != 1 {
		t.Fatalf("没在录制时窗口快捷键应生效：%d", opened)
	}

	_ = tr.Click(fieldLabel)
	tr.Key(ui.Ctrl, ui.KeyK)
	tr.Frame()
	if opened != 1 {
		t.Errorf("录制中窗口快捷键应让路：%d", opened)
	}
	if len(h.changes) != 1 || h.changes[0] != "CommandOrControl+K" {
		t.Errorf("Ctrl+K 应被录下：%v", h.changes)
	}

	// 对照：不让路的窗口快捷键会抢走这个键——这正是 KeyCaptureActive 存在的理由
	guarded, h.value, h.changes = false, "", nil
	_ = tr.Click(fieldLabel)
	tr.Key(ui.Ctrl, ui.KeyK)
	tr.Frame()
	if opened != 2 || len(h.changes) != 0 {
		t.Errorf("不让路时窗口快捷键应先于输入框拿到键（MyGo 的优先级）：opened=%d changes=%v", opened, h.changes)
	}
}

func TestShortcutFieldInDialogEscapeOnlyCancelsRecording(t *testing.T) {
	resetCapture(t)
	l := lightLook()
	h := newFieldHarness("Alt+Shift+P")
	open := true
	tr := newFieldTester(t, l, func(c *ui.Context) {
		ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
			backdrop.Background(l.T.Background.Alpha(0.6))
			panel.Column().Padding(24).Background(l.T.Card)
			h.view(c, l)
		})
	})
	_ = tr.Click(fieldLabel)
	tr.Key(0, ui.KeyEscape)
	tr.Frame()
	if h.f.Recording() {
		t.Error("Esc 应取消录制")
	}
	if !open {
		t.Error("录制中的 Esc 只该取消录制，不该顺手把对话框关了")
	}
	tr.Key(0, ui.KeyEscape)
	tr.Frame()
	if open {
		t.Error("不在录制时 Esc 照常关闭对话框")
	}
}

func TestShortcutFieldRendersInBothModes(t *testing.T) {
	resetCapture(t)
	for _, l := range []*Look{lightLook(), darkLook()} {
		h := newFieldHarness("Alt+Shift+P")
		tr := newFieldTester(t, l, func(c *ui.Context) { h.view(c, l) })
		if !tr.HasText("Alt + Shift + P") {
			t.Fatal("应画出输入框")
		}
		_ = tr.Click(fieldLabel)
		tr.Frame()
		if !tr.HasText(locales.SettingsPressKeys()) {
			t.Fatal("录制态应画出提示")
		}
	}
}
