package settings_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/tokens"
)

// openAppearance 用关闭动效的外观在 1280x800 里打开「显示设置」页（动效直接到终态）。
func openAppearance(t *testing.T, store sscreen.Store) (*rig, *sscreen.Appearance) {
	t.Helper()
	return openAppearanceWith(t, store, nil)
}

// openAppearanceWith 同上，打开之前让调用方先改一改页面（接日志、选图函数等）。
func openAppearanceWith(t *testing.T, store sscreen.Store, tweak func(a *sscreen.Appearance)) (*rig, *sscreen.Appearance) {
	t.Helper()
	fontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff
	look := kit.NewLook(spec)
	look.Scale = 1
	look.Viewport = 1280
	a := &sscreen.Appearance{Look: look, Store: store}
	if tweak != nil {
		tweak(a)
	}
	bg := look.Col(look.T.Background)
	page := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			a.Page(c)
		},
		Theme: &look.Theme, Background: &bg,
	})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: t.Name(), Width: 1280, Height: 800}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	r := &rig{t: t, s: session}
	r.frames(4)
	return r, a
}

// customStore 是已选「自定义」主题色的设置。
func customStore() *sscreen.MemStore {
	v := nsettings.Default()
	v.UI.ThemeColor = "custom"
	return sscreen.NewMemStore(v)
}

// named 按角色与名称取出所有可访问节点（同名的有好几个时，按阅读顺序）。
func (r *rig) named(role gfx.Role, name string) []app.AccessNode {
	var out []app.AccessNode
	for _, n := range r.s.Access() {
		if n.Role == role && n.Label == name {
			out = append(out, n)
		}
	}
	return out
}

// clickNth 点击第 n 个（从 0 起）同名控件的中心。
func (r *rig) clickNth(role gfx.Role, name string, n int) {
	r.t.Helper()
	all := r.named(role, name)
	if n >= len(all) {
		r.t.Fatalf("只有 %d 个 %v「%s」，取不到第 %d 个", len(all), role, name, n)
	}
	b := all[n].Bounds
	if err := r.s.Click(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
		r.t.Fatalf("点击「%s」失败：%v", name, err)
	}
	r.frames(3)
}

// last 取同名控件里阅读顺序最靠后的一个（「关闭」既是动画的选项也是背景图的开关，页面里两处同名）。
func (r *rig) last(role gfx.Role, name string) app.AccessNode {
	r.t.Helper()
	all := r.named(role, name)
	if len(all) == 0 {
		r.t.Fatalf("找不到 %v「%s」", role, name)
	}
	return all[len(all)-1]
}

// clickLast 点击最靠后的同名控件的中心。
func (r *rig) clickLast(role gfx.Role, name string) {
	r.t.Helper()
	b := r.last(role, name).Bounds
	if err := r.s.Click(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
		r.t.Fatalf("点击「%s」失败：%v", name, err)
	}
	r.frames(3)
}

// scrollToEnd 把设置内容滚到底：屏外的控件不在可访问树里，背景图分区要先滚到才点得到。
func (r *rig) scrollToEnd() {
	r.t.Helper()
	if err := r.s.Scroll(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: 4000, Precise: true}); err != nil {
		r.t.Fatalf("滚动失败：%v", err)
	}
	r.frames(3)
}

// focusByTab 用 Tab 把键盘送到指定控件（顺带验证它在 Tab 顺序里），最多按 limit 次。
func (r *rig) focusByTab(role gfx.Role, name string, limit int) {
	r.t.Helper()
	for i := 0; i < limit; i++ {
		if n, ok := r.node(role, name); ok && n.Focused {
			return
		}
		r.keys("tab")
	}
	r.t.Fatalf("按了 %d 次 Tab 仍没有走到 %v「%s」", limit, role, name)
}

// selected 报告按钮是否处于选中态。
func (r *rig) selected(role gfx.Role, name string) bool {
	r.t.Helper()
	n, ok := r.node(role, name)
	if !ok {
		r.t.Fatalf("找不到 %v「%s」", role, name)
	}
	return n.Selected
}

// logRecorder 记下写进日志的失败（kit.Logger 的测试替身）。
type logRecorder struct {
	mu   sync.Mutex
	logs []string
}

func (l *logRecorder) AppError(scope, action, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, scope+"|"+action+"|"+message)
}

func (l *logRecorder) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.logs...)
}

// hexToHs 与渲染层 settings-appearance.ts 的 hexToHs 逐条对照：期望值用该函数的原文在 node 里算出。
// 十六进制必须是恰好六位（可带一个前导 #、前后空白、大小写不限）；三位简写、位数不对、非十六进制字符都会让渲染层抛错。
func TestHexToHSMatchesRenderer(t *testing.T) {
	valid := []struct {
		in       string
		hue, sat float64
	}{
		{"#3b82f6", 217, 91}, {"#ff0000", 0, 100}, {"#00ff00", 120, 100}, {"#0000ff", 240, 100},
		{"#ffffff", 0, 0}, {"#000000", 0, 0}, {"#808080", 0, 0}, {"#ff00ff", 300, 100},
		{"#ffff00", 60, 100}, {"#00ffff", 180, 100}, {"#8b5cf6", 258, 90}, {"#ff0080", 330, 100},
		{"#f43f5e", 350, 89}, {"#123456", 210, 65}, {"#abcdef", 210, 68}, {"#3B82F6", 217, 91},
		{"  #3b82f6  ", 217, 91}, {"3b82f6", 217, 91}, {"#ff7f00", 30, 100}, {"#7f7f80", 240, 0},
		{"#010203", 210, 50}, {"#fefefe", 0, 0}, {"#ff0001", 0, 100}, {"#0f0f10", 240, 3},
	}
	for _, c := range valid {
		hue, sat, ok := sscreen.HexToHS(c.in)
		if !ok || hue != c.hue || sat != c.sat {
			t.Errorf("HexToHS(%q) = (%v, %v, %v)，渲染层给出 (%v, %v)", c.in, hue, sat, ok, c.hue, c.sat)
		}
	}
	for _, in := range []string{"", "#", "#fff", "#12345", "#1234567", "#gggggg", "##123456", "#12 3456", "rgb(1,2,3)"} {
		if _, _, ok := sscreen.HexToHS(in); ok {
			t.Errorf("HexToHS(%q) 应当判为不合法（渲染层会抛错）", in)
		}
	}
}

// 主题模式：三个分段，点谁就写谁；选中态跟着设置走（受控），只写这一项。
func TestThemeModeSegmentsWriteAndFollowTheSetting(t *testing.T) {
	modes := []struct {
		name string
		id   nsettings.ThemeMode
	}{
		{locales.SettingsLight(), nsettings.ThemeLight},
		{locales.SettingsDark(), nsettings.ThemeDark},
		{locales.SettingsSystem(), nsettings.ThemeSystem},
	}
	for _, target := range modes {
		t.Run(target.name, func(t *testing.T) {
			v := nsettings.Default()
			v.UI.ThemeMode = nsettings.ThemeDark
			if target.id == nsettings.ThemeDark {
				v.UI.ThemeMode = nsettings.ThemeLight
			}
			store := sscreen.NewMemStore(v)
			r, _ := openAppearance(t, store)
			if r.selected(gfx.RoleButton, target.name) {
				t.Fatal("点之前它不应是选中的")
			}
			r.clickNode(gfx.RoleButton, target.name)
			if got := store.Values().UI.ThemeMode; got != target.id {
				t.Errorf("点「%s」后 ThemeMode = %q，期望 %q", target.name, got, target.id)
			}
			if store.Updates != 1 {
				t.Errorf("应只写入一次，实际 %d 次", store.Updates)
			}
			for _, m := range modes {
				if want := m.id == target.id; r.selected(gfx.RoleButton, m.name) != want {
					t.Errorf("「%s」的选中态应为 %v", m.name, want)
				}
			}
		})
	}
}

// 字体大小：三个磁贴的可访问名是「名称, 像素px」；点谁写谁，选中态跟着走。
func TestFontSizeTilesWriteAndAreNamedWithPixels(t *testing.T) {
	labels := map[string]string{
		"small": locales.SettingsFontSmall(), "medium": locales.SettingsFontMedium(), "large": locales.SettingsFontLarge(),
	}
	for _, f := range tokens.FontSizes {
		name := labels[f.ID] + ", " + itoa(f.Px) + "px"
		t.Run(f.ID, func(t *testing.T) {
			v := nsettings.Default()
			v.UI.FontSize = nsettings.FontMedium
			if f.ID == "medium" {
				v.UI.FontSize = nsettings.FontSmall
			}
			store := sscreen.NewMemStore(v)
			r, _ := openAppearance(t, store)
			r.clickNode(gfx.RoleButton, name)
			if got := store.Values().UI.FontSize; got != nsettings.FontSize(f.ID) {
				t.Errorf("点「%s」后 FontSize = %q，期望 %q", name, got, f.ID)
			}
			if !r.selected(gfx.RoleButton, name) {
				t.Errorf("「%s」应当成为选中的", name)
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// 动画：四个分段，点谁写谁。
func TestMotionSegmentsWrite(t *testing.T) {
	options := []struct {
		name string
		id   nsettings.MotionPreference
	}{
		{locales.SettingsMotionSystem(), nsettings.MotionSystem},
		{locales.SettingsMotionOff(), nsettings.MotionOff},
		{locales.SettingsMotionReduced(), nsettings.MotionReduced},
		{locales.SettingsMotionStandard(), nsettings.MotionStandard},
	}
	for _, o := range options {
		t.Run(o.name, func(t *testing.T) {
			v := nsettings.Default()
			v.UI.MotionPreference = nsettings.MotionStandard
			if o.id == nsettings.MotionStandard {
				v.UI.MotionPreference = nsettings.MotionOff
			}
			store := sscreen.NewMemStore(v)
			r, _ := openAppearance(t, store)
			// 动画的「跟随系统」与主题模式的同名：动画分区在后面，取最后一个。
			r.clickLast(gfx.RoleButton, o.name)
			if got := store.Values().UI.MotionPreference; got != o.id {
				t.Errorf("点「%s」后 MotionPreference = %q，期望 %q", o.name, got, o.id)
			}
			if !r.last(gfx.RoleButton, o.name).Selected {
				t.Errorf("「%s」应当成为选中的", o.name)
			}
		})
	}
}

// 预设色板：点一个就同时写入主题色 ID 与它的色相、饱和度；不展开自定义面板；右上角显示预设名。
func TestPresetSwatchesWriteColorHueAndSaturation(t *testing.T) {
	names := map[string]string{
		"royal-blue": locales.SettingsColorRoyalBlue(), "blue": locales.SettingsColorBlue(), "purple": locales.SettingsColorPurple(),
		"green": locales.SettingsColorGreen(), "orange": locales.SettingsColorOrange(), "teal": locales.SettingsColorTeal(),
	}
	for _, p := range tokens.HuePresets {
		name := names[p.ID]
		t.Run(p.ID, func(t *testing.T) {
			v := nsettings.Default()
			v.UI.ThemeColor, v.UI.ThemeHue, v.UI.ThemeSaturation = "custom", 10, 20
			store := sscreen.NewMemStore(v)
			r, _ := openAppearance(t, store)
			r.clickNode(gfx.RoleButton, name)
			got := store.Values().UI
			if got.ThemeColor != p.ID || got.ThemeHue != p.Hue || got.ThemeSaturation != p.Saturation {
				t.Errorf("点「%s」后写入 (%q, %v, %v)，期望 (%q, %v, %v)", name, got.ThemeColor, got.ThemeHue, got.ThemeSaturation, p.ID, p.Hue, p.Saturation)
			}
			if !r.selected(gfx.RoleButton, name) {
				t.Errorf("「%s」应当成为选中的", name)
			}
			if r.has(gfx.RoleTextField, "#3b82f6") {
				t.Error("选了预设就不应再展开自定义面板")
			}
			if !r.s.HasText(name) {
				t.Errorf("右上角应显示颜色名「%s」", name)
			}
		})
	}
}

// 选「自定义」：色相与饱和度取已存的十六进制算出；展开面板（文本框与三格预览），右上角显示「自定义 #十六进制」。
func TestSelectCustomUsesStoredHexAndOpensThePanel(t *testing.T) {
	store := sscreen.NewMemStore(nsettings.Default())
	r, _ := openAppearance(t, store)
	if r.has(gfx.RoleTextField, "#3b82f6") {
		t.Fatal("没选自定义时不应有十六进制文本框")
	}
	r.clickNode(gfx.RoleButton, locales.SettingsCustomColor())
	got := store.Values().UI
	if got.ThemeColor != "custom" || got.ThemeHue != 217 || got.ThemeSaturation != 91 {
		t.Errorf("选自定义后写入 (%q, %v, %v)，期望 (custom, 217, 91)", got.ThemeColor, got.ThemeHue, got.ThemeSaturation)
	}
	field, ok := r.node(gfx.RoleTextField, "#3b82f6")
	if !ok || field.Value != "#3b82f6" {
		t.Fatalf("应出现十六进制文本框并显示已存的值：%+v ok=%v", field, ok)
	}
	for _, label := range []string{locales.SettingsPrimary(), locales.SettingsAccent(), locales.SettingsNeutral()} {
		if !r.s.HasText(label) {
			t.Errorf("预览条上应有「%s」", label)
		}
	}
	if !r.s.HasText(locales.SettingsCustomColor() + " #3b82f6") {
		t.Error("右上角应显示「自定义 #3b82f6」")
	}
	if n := len(r.named(gfx.RoleButton, locales.SettingsCustomColor())); n != 2 {
		t.Errorf("面板展开后应有色带里的色板与取色器触发钮两个同名按钮，实际 %d 个", n)
	}
}

// 已存的十六进制不合法时，选自定义什么也不写（渲染层 hexToHs 抛错、设置不变）。
func TestSelectCustomWithInvalidStoredHexWritesNothing(t *testing.T) {
	v := nsettings.Default()
	v.UI.CustomThemeHex = "#12345"
	store := sscreen.NewMemStore(v)
	r, _ := openAppearance(t, store)
	r.clickNode(gfx.RoleButton, locales.SettingsCustomColor())
	if store.Updates != 0 || store.Values().UI.ThemeColor != nsettings.DefaultThemeColor {
		t.Errorf("不合法的十六进制不应写入：写入 %d 次，ThemeColor=%q", store.Updates, store.Values().UI.ThemeColor)
	}
}

// 十六进制文本框是受控输入：合法写法（六位，可带 #）写入并同时把主题色切到自定义；非法写法被拒绝、文字退回原值。
func TestHexFieldAcceptsValidAndRevertsInvalid(t *testing.T) {
	store := customStore()
	r, _ := openAppearance(t, store)
	set := func(text string) {
		r.clickNode(gfx.RoleTextField, "#3b82f6")
		r.keys("cmd+a")
		if err := r.s.Type(text); err != nil {
			t.Fatalf("输入 %q 失败：%v", text, err)
		}
		r.frames(3)
	}
	value := func() string {
		n, ok := r.node(gfx.RoleTextField, "#3b82f6")
		if !ok {
			t.Fatal("找不到十六进制文本框")
		}
		return n.Value
	}

	set("#ff0000")
	got := store.Values().UI
	if got.CustomThemeHex != "#ff0000" || got.ThemeColor != "custom" || got.ThemeHue != 0 || got.ThemeSaturation != 100 {
		t.Errorf("输入 #ff0000 后写入 (%q, %q, %v, %v)", got.CustomThemeHex, got.ThemeColor, got.ThemeHue, got.ThemeSaturation)
	}
	if value() != "#ff0000" {
		t.Errorf("文本框应显示 #ff0000，得到 %q", value())
	}

	updates := store.Updates
	set("#12")
	if store.Updates != updates || store.Values().UI.CustomThemeHex != "#ff0000" {
		t.Errorf("非法写法不应写入：写入 %d 次，值 %q", store.Updates-updates, store.Values().UI.CustomThemeHex)
	}
	if value() != "#ff0000" {
		t.Errorf("非法写法被拒绝后文字应退回 #ff0000，得到 %q", value())
	}

	set("ABCDEF") // 不带 #：写入时补上，大小写按原文保留（渲染层存原文）
	if got := store.Values().UI.CustomThemeHex; got != "#ABCDEF" {
		t.Errorf("输入 ABCDEF 后应存成 #ABCDEF，得到 %q", got)
	}
	if hue, sat := store.Values().UI.ThemeHue, store.Values().UI.ThemeSaturation; hue != 210 || sat != 68 {
		t.Errorf("色相与饱和度应随之变成 (210, 68)，得到 (%v, %v)", hue, sat)
	}
}

// 取色器改色：写入十六进制并切到自定义；文本框的文字跟着变（设置从别处改了）。
func TestColorPickerChoiceFlowsIntoSettingsAndTheField(t *testing.T) {
	store := customStore()
	r, _ := openAppearance(t, store)
	r.clickNth(gfx.RoleButton, locales.SettingsCustomColor(), 1) // 触发钮是第二个同名按钮
	hue, ok := r.node(gfx.RoleSlider, locales.CommonColorHue())
	if !ok {
		t.Fatal("取色器没有展开")
	}
	// 按在色相条最左端：色相 0（饱和度与明度沿用 #3b82f6 的），写入的十六进制随之变成红色系。
	if err := r.s.Click(hue.Bounds.X+1, hue.Bounds.Y+hue.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	r.frames(3)
	got := store.Values().UI.CustomThemeHex
	if got == "#3b82f6" || !strings.HasPrefix(got, "#") || len(got) != 7 {
		t.Fatalf("取色后应写入新的十六进制，得到 %q", got)
	}
	if store.Values().UI.ThemeColor != "custom" {
		t.Errorf("取色后主题色应是 custom，得到 %q", store.Values().UI.ThemeColor)
	}
	if n, ok := r.node(gfx.RoleTextField, "#3b82f6"); !ok || n.Value != got {
		t.Errorf("面板里的十六进制文本框应跟着显示 %q，得到 %+v", got, n)
	}
}

// 写入失败：设置不变、界面保持原状（受控），失败进日志且带上原因。
func TestWriteFailureKeepsStateAndLogsTheReason(t *testing.T) {
	v := nsettings.Default()
	v.UI.ThemeMode = nsettings.ThemeLight
	store := sscreen.NewMemStore(v)
	logs := &logRecorder{}
	r, _ := openAppearanceWith(t, store, func(a *sscreen.Appearance) { a.Guard = kit.Guard{Log: logs} })
	store.FailNext = errors.New("disk full")
	r.clickNode(gfx.RoleButton, locales.SettingsDark())
	if store.Values().UI.ThemeMode != nsettings.ThemeLight || store.Updates != 0 {
		t.Errorf("写入失败时设置不应改变：ThemeMode=%q 写入 %d 次", store.Values().UI.ThemeMode, store.Updates)
	}
	if r.selected(gfx.RoleButton, locales.SettingsDark()) || !r.selected(gfx.RoleButton, locales.SettingsLight()) {
		t.Error("写入失败后选中态应仍停在原来的「浅色」上")
	}
	got := logs.all()
	if len(got) != 1 || !strings.Contains(got[0], "disk full") || !strings.Contains(got[0], locales.NativeSettingsSaveAction()) {
		t.Errorf("失败应记一条带动作名与原因的日志，得到 %q", got)
	}
}

// 背景图分区的状态：没有图时「选择图片」可点、开关禁用且没有启用提示；有图时按钮换成「更换图片」，开关随启用状态切换文案，
// 点开关只翻转 BackgroundImageEnabled。
func TestBackgroundSectionStates(t *testing.T) {
	t.Run("没有图", func(t *testing.T) {
		r, _ := openAppearance(t, sscreen.NewMemStore(nsettings.Default()))
		r.scrollToEnd()
		if n, ok := r.node(gfx.RoleButton, locales.SettingsSelectBackgroundImage()); !ok || n.Disabled {
			t.Errorf("「选择图片」应存在且可点：%+v ok=%v", n, ok)
		}
		if n, ok := r.node(gfx.RoleButton, locales.SettingsEnableBackgroundImage()); !ok || !n.Disabled {
			t.Errorf("没有图时「开启」开关应存在且禁用：%+v ok=%v", n, ok)
		}
		if r.s.HasText(locales.SettingsBackgroundImageEnabledHint()) || r.s.HasText(locales.SettingsBackgroundImageDisabledHint()) {
			t.Error("没有图时不应显示启用状态提示")
		}
		if !r.s.HasText(locales.SettingsBackgroundImageEmpty()) {
			t.Error("没有图时预览舞台应显示空态说明")
		}
	})
	t.Run("有图", func(t *testing.T) {
		v := nsettings.Default()
		v.UI.BackgroundImageFileName, v.UI.BackgroundImageEnabled = "wall.png", true
		store := sscreen.NewMemStore(v)
		r, _ := openAppearance(t, store)
		r.scrollToEnd()
		if !r.has(gfx.RoleButton, locales.SettingsChangeBackgroundImage()) {
			t.Error("有图时按钮应是「更换图片」")
		}
		if !r.s.HasText(locales.SettingsBackgroundImageEnabledHint()) {
			t.Error("启用时应显示启用提示")
		}
		r.clickLast(gfx.RoleButton, locales.SettingsDisableBackgroundImage())
		if store.Values().UI.BackgroundImageEnabled || store.Values().UI.BackgroundImageFileName != "wall.png" {
			t.Errorf("点「关闭」后应只把启用翻成假：%+v", store.Values().UI)
		}
		if !r.has(gfx.RoleButton, locales.SettingsEnableBackgroundImage()) || !r.s.HasText(locales.SettingsBackgroundImageDisabledHint()) {
			t.Error("关闭后按钮应换成「开启」并显示未启用提示")
		}
	})
}

// 选图：后台选图完成后写入文件名并自动启用；取消什么也不写；失败只进日志（带原因），界面不卡在「选图中」。
func TestPickBackgroundOutcomes(t *testing.T) {
	waitFor := func(r *rig, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatal("等待选图结果超时")
			}
			time.Sleep(5 * time.Millisecond)
			r.frames(1)
		}
	}
	t.Run("选中", func(t *testing.T) {
		v := nsettings.Default()
		v.UI.BackgroundImageEnabled = false
		store := sscreen.NewMemStore(v)
		r, _ := openAppearanceWith(t, store, func(a *sscreen.Appearance) {
			a.PickBackground = func() (string, error) { return "wallpaper.png", nil }
			a.Wake = func() {}
		})
		r.scrollToEnd()
		r.clickNode(gfx.RoleButton, locales.SettingsSelectBackgroundImage())
		waitFor(r, func() bool { return store.Values().UI.BackgroundImageFileName != "" })
		if got := store.Values().UI; got.BackgroundImageFileName != "wallpaper.png" || !got.BackgroundImageEnabled {
			t.Errorf("选图后应写入文件名并启用：%+v", got)
		}
	})
	t.Run("取消", func(t *testing.T) {
		store := sscreen.NewMemStore(nsettings.Default())
		picked := make(chan struct{}, 1)
		r, _ := openAppearanceWith(t, store, func(a *sscreen.Appearance) {
			a.PickBackground = func() (string, error) { picked <- struct{}{}; return "", nil }
			a.Wake = func() {}
		})
		r.scrollToEnd()
		r.clickNode(gfx.RoleButton, locales.SettingsSelectBackgroundImage())
		select {
		case <-picked:
		case <-time.After(2 * time.Second):
			t.Fatal("选图函数没有被调用")
		}
		time.Sleep(20 * time.Millisecond)
		r.frames(3)
		if store.Updates != 0 {
			t.Errorf("取消选图不应写入设置，写入 %d 次", store.Updates)
		}
		if n, ok := r.node(gfx.RoleButton, locales.SettingsSelectBackgroundImage()); !ok || n.Disabled {
			t.Error("取消后按钮应恢复可点")
		}
	})
	t.Run("失败", func(t *testing.T) {
		store := sscreen.NewMemStore(nsettings.Default())
		logs := &logRecorder{}
		r, _ := openAppearanceWith(t, store, func(a *sscreen.Appearance) {
			a.Guard = kit.Guard{Log: logs}
			a.PickBackground = func() (string, error) { return "", errors.New("copy failed: access denied") }
			a.Wake = func() {}
		})
		r.scrollToEnd()
		r.clickNode(gfx.RoleButton, locales.SettingsSelectBackgroundImage())
		waitFor(r, func() bool { return len(logs.all()) > 0 })
		if got := logs.all(); !strings.Contains(got[0], "access denied") {
			t.Errorf("失败原因应进日志，得到 %q", got)
		}
		if store.Updates != 0 {
			t.Errorf("选图失败不应写入设置，写入 %d 次", store.Updates)
		}
		waitFor(r, func() bool {
			n, ok := r.node(gfx.RoleButton, locales.SettingsSelectBackgroundImage())
			return ok && !n.Disabled
		})
	})
	t.Run("宿主没接上选图", func(t *testing.T) {
		logs := &logRecorder{}
		r, _ := openAppearanceWith(t, sscreen.NewMemStore(nsettings.Default()), func(a *sscreen.Appearance) { a.Guard = kit.Guard{Log: logs} })
		r.scrollToEnd()
		r.clickNode(gfx.RoleButton, locales.SettingsSelectBackgroundImage())
		if got := logs.all(); len(got) != 1 || !strings.Contains(got[0], "not available") {
			t.Errorf("没接上选图时应如实记录原因，得到 %q", got)
		}
	})
}

// 两个滑块：窗口不窄于 768 并排，否则上下叠放（Tailwind md 断点）。
func TestSlidersSideBySideFromMdBreakpoint(t *testing.T) {
	position := func(r *rig) (opacity, blur gfx.Rect) {
		o, ok1 := r.node(gfx.RoleSlider, locales.SettingsBackgroundImageOpacity())
		b, ok2 := r.node(gfx.RoleSlider, locales.SettingsBackgroundImageBlur())
		if !ok1 || !ok2 {
			r.t.Fatalf("找不到两个滑块：%v %v", ok1, ok2)
		}
		return o.Bounds, b.Bounds
	}
	r, a := openAppearance(t, sscreen.NewMemStore(nsettings.Default()))
	r.scrollToEnd()
	o, b := position(r)
	if o.Y != b.Y || b.X <= o.X {
		t.Errorf("1280 宽时应并排：可见度 %+v，虚化 %+v", o, b)
	}
	a.Look.Viewport = 600
	r.frames(3)
	o, b = position(r)
	if o.X != b.X || b.Y <= o.Y {
		t.Errorf("600 宽时应叠放：可见度 %+v，虚化 %+v", o, b)
	}
}

// 滑块的键盘：可见度 0–100%（步长 1，写入时除以 100），虚化 0–50px（步长 0.5）；写入前过 Clamp。
func TestSlidersWriteClampedValuesFromTheKeyboard(t *testing.T) {
	store := sscreen.NewMemStore(nsettings.Default())
	r, _ := openAppearance(t, store)
	r.scrollToEnd()

	r.focusByTab(gfx.RoleSlider, locales.SettingsBackgroundImageOpacity(), 80)
	r.keys("left") // 默认 100%，减一格
	if got := store.Values().UI.BackgroundImageOpacity; got != 0.99 {
		t.Errorf("可见度按一次 ← 应是 0.99，得到 %v", got)
	}
	if !r.s.HasText("99") {
		t.Error("数值标签应显示 99")
	}
	r.keys("right")
	if got := store.Values().UI.BackgroundImageOpacity; got != 1 {
		t.Errorf("再按一次 → 应回到 1，得到 %v", got)
	}
	r.keys("right") // 到顶之后不再变
	if got := store.Values().UI.BackgroundImageOpacity; got != 1 {
		t.Errorf("到顶后应仍是 1，得到 %v", got)
	}

	r.focusByTab(gfx.RoleSlider, locales.SettingsBackgroundImageBlur(), 5)
	r.keys("right") // 默认 0，加一格 0.5
	if got := store.Values().UI.BackgroundImageBlur; got != 0.5 {
		t.Errorf("虚化按一次 → 应是 0.5，得到 %v", got)
	}
	r.keys("end")
	if got := store.Values().UI.BackgroundImageBlur; got != 50 {
		t.Errorf("End 应到 50，得到 %v", got)
	}
}
