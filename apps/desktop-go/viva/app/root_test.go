package app

import (
	"errors"
	"sync/atomic"
	"testing"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/screens/shell"
	"guizhi-mygo-shell/viva/tokens"
)

// rootFixture 是无头会话里的一个 Root：不开真窗口，不碰 WebView2，毫秒级。
type rootFixture struct {
	root    *Root
	session *headless.Session
	fake    *servicestest.Settings
	redraws atomic.Int32 // 设置被别处改动后请求重绘的次数
}

// openRoot 在 1280×800、缩放 1 的无头会话里打开 Root，默认直接进设置页。
func openRoot(t *testing.T, mutate func(*Options)) *rootFixture {
	t.Helper()
	vapp.SetInterfaceFontFamily(InterfaceFont)
	f := &rootFixture{fake: &servicestest.Settings{}}
	opts := Options{
		Services: services.Services{Settings: f.fake}, Page: shell.PageSettings,
		// 结果不随运行机器的系统「减少动态效果」变化；默认关闭动效，让画面总在终态（动画时钟只在无头会话里手动推进）。
		OSReduceMotion: func() bool { return false },
		Appearance:     func(s kit.Spec) kit.Spec { s.Motion = kit.MotionLevelOff; return s },
	}
	if mutate != nil {
		mutate(&opts)
	}
	f.root = NewRoot(opts, func() { f.redraws.Add(1) })
	t.Cleanup(f.root.Close)
	page := ui.NewPage(ui.PageConfig{View: f.root.View, Theme: f.root.ThemeRef(), Background: f.root.BackgroundRef()})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(vapp.Config{Title: "test", Width: 1280, Height: 800}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	f.session = session
	f.frames(t, 4)
	return f
}

// frames 连续出帧（版面与转场要靠前一帧的结果，一帧不够）。
func (f *rootFixture) frames(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := f.session.Frame(); err != nil {
			t.Fatalf("出帧失败：%v", err)
		}
	}
}

func TestSpecFromSettings(t *testing.T) {
	ui := settings.Default().UI
	ui.ThemeMode, ui.ThemeHue, ui.ThemeSaturation, ui.FontSize = settings.ThemeSystem, 120, 55, settings.FontLarge
	want := kit.Spec{
		Mode: tokens.ModeSystem, SystemDark: true, Hue: 120, Saturation: 55, FontSize: "large",
		Motion: kit.MotionLevelStandard,
	}
	if got := SpecFromSettings(ui, true, false); got != want {
		t.Errorf("SpecFromSettings = %+v，期望 %+v", got, want)
	}
	// 「跟随系统」才看系统明暗；明确选了浅色时，系统深色不能影响外观。
	ui.ThemeMode = settings.ThemeLight
	if kit.NewLook(SpecFromSettings(ui, true, false)).Dark {
		t.Error("明确选浅色时，系统深色不应让界面变深")
	}
	ui.ThemeMode = settings.ThemeSystem
	if !kit.NewLook(SpecFromSettings(ui, true, false)).Dark || kit.NewLook(SpecFromSettings(ui, false, false)).Dark {
		t.Error("跟随系统时，界面明暗应与系统一致")
	}
}

// 动效偏好与系统「减少动态效果」合并成实际档位（复刻 globals.css 的规则）。
func TestSpecFromSettingsMotion(t *testing.T) {
	cases := []struct {
		pref     settings.MotionPreference
		osReduce bool
		want     kit.MotionLevel
	}{
		{settings.MotionSystem, false, kit.MotionLevelStandard},
		{settings.MotionSystem, true, kit.MotionLevelOff},
		{settings.MotionStandard, false, kit.MotionLevelStandard},
		{settings.MotionStandard, true, kit.MotionLevelStandard}, // 用户在应用内明确选了标准：无视系统降级
		{settings.MotionReduced, false, kit.MotionLevelReduced},
		{settings.MotionReduced, true, kit.MotionLevelOff},
		{settings.MotionOff, false, kit.MotionLevelOff},
		{settings.MotionOff, true, kit.MotionLevelOff},
	}
	for _, c := range cases {
		ui := settings.Default().UI
		ui.MotionPreference = c.pref
		if got := SpecFromSettings(ui, false, c.osReduce).Motion; got != c.want {
			t.Errorf("动效偏好 %q、系统减少动态效果=%v：档位 %v，期望 %v", c.pref, c.osReduce, got, c.want)
		}
	}
}

func TestRootRendersSettingsPage(t *testing.T) {
	f := openRoot(t, nil)
	for _, text := range []string{"应用设置", "启动设置", "编辑器设置"} {
		if !f.session.HasText(text) {
			t.Errorf("设置页缺少文字 %q", text)
		}
	}
	if _, ok := f.session.AccessByRole(gfx.RoleSwitch, "自动保存"); !ok {
		t.Error("没有找到「自动保存」开关")
	}
}

func TestRootToggleWritesThroughService(t *testing.T) {
	f := openRoot(t, nil)
	if f.fake.Values().UI.ShowLineNumbers {
		t.Fatal("前提不成立：显示行号默认应为关")
	}
	if err := f.session.ClickLabel("显示行号"); err != nil {
		t.Fatalf("点击「显示行号」失败：%v", err)
	}
	f.frames(t, 3)
	if got := f.fake.Updates(); got != 1 {
		t.Fatalf("经服务落盘的次数 = %d，期望 1", got)
	}
	if !f.fake.Values().UI.ShowLineNumbers {
		t.Error("服务里的「显示行号」没有变成开")
	}
	node, ok := f.session.AccessByRole(gfx.RoleSwitch, "显示行号")
	if !ok || !node.Checked {
		t.Errorf("界面上的「显示行号」开关没有跟着变成开：%+v（找到=%v）", node, ok)
	}
}

func TestRootFollowsSettingsChangedElsewhere(t *testing.T) {
	f := openRoot(t, nil)
	light := *f.root.BackgroundRef()
	if f.root.Look().Dark {
		t.Fatal("前提不成立：默认应为浅色")
	}
	// 别处（比如另一个入口或导入）改了设置：外观整套重装配，并请求重绘。
	if _, err := f.fake.Update(func(v *settings.Values) { v.UI.ThemeMode = settings.ThemeDark }); err != nil {
		t.Fatal(err)
	}
	if f.redraws.Load() == 0 {
		t.Error("设置被别处改动后没有请求重绘")
	}
	f.frames(t, 2)
	if !f.root.Look().Dark {
		t.Fatal("改成深色后，Look 没有重装配")
	}
	if dark := *f.root.BackgroundRef(); dark == light {
		t.Error("页面背景色没有随主题原地更新")
	}
	// 页面读的是指针：同一个页面对象（不重建）就能画出深色，窗口底色取自主题背景。
	img, err := f.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	px := img.RGBAAt(img.Bounds().Dx()-6, img.Bounds().Dy()-6) // 内容区右下角附近：整页背景
	bg := f.root.Look().Col(f.root.Look().T.Background)
	if int(px.R) > int(bg.Red*255)+8 || int(px.R) < int(bg.Red*255)-8 {
		t.Errorf("右下角像素 %v 不是深色背景（期望红色分量约 %.0f）", px, bg.Red*255)
	}
}

func TestRootAppearanceOverride(t *testing.T) {
	f := openRoot(t, func(o *Options) {
		o.Appearance = func(s kit.Spec) kit.Spec { s.Mode, s.FontSize = tokens.ModeDark, "large"; return s }
	})
	if !f.root.Look().Dark || f.root.Look().Spec.FontSize != "large" {
		t.Errorf("命令行覆盖没有生效：%+v", f.root.Look().Spec)
	}
	// 覆盖叠在设置之上：设置里改色相，覆盖的明暗与字号不变。
	if _, err := f.fake.Update(func(v *settings.Values) { v.UI.ThemeHue = 150 }); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 2)
	if got := f.root.Look().Spec; got.Hue != 150 || got.Mode != tokens.ModeDark || got.FontSize != "large" {
		t.Errorf("覆盖与设置的叠加不对：%+v", got)
	}
}

func TestRootStartPage(t *testing.T) {
	f := openRoot(t, func(o *Options) { o.Page, o.Module = shell.PageHome, shell.ModuleImports })
	if f.root.Window().Page != shell.PageHome || f.root.Window().Module != shell.ModuleImports {
		t.Fatalf("起始页不对：%v / %v", f.root.Window().Page, f.root.Window().Module)
	}
	// 点齿轮进设置页，点 rail 上的模块回到工作区。
	if err := f.session.ClickLabel("设置"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if f.root.Window().Page != shell.PageSettings {
		t.Error("点「设置」没有进设置页")
	}
	if err := f.session.ClickLabel("Wiki"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if f.root.Window().Page != shell.PageHome || f.root.Window().Module != shell.ModuleWiki {
		t.Errorf("点 rail 的 Wiki 后状态不对：%v / %v", f.root.Window().Page, f.root.Window().Module)
	}
}

func TestRootOnReadyOnce(t *testing.T) {
	var ready atomic.Int32
	f := openRoot(t, func(o *Options) { o.OnReady = func() { ready.Add(1) } })
	f.frames(t, 5)
	if got := ready.Load(); got != 1 {
		t.Errorf("OnReady 调用了 %d 次，期望只在第一帧后调用 1 次", got)
	}
}

// failingSettings 让首次读取失败，用来确认加载失败不会被当成默认值悄悄渲染。
type failingSettings struct{ servicestest.Settings }

func (f *failingSettings) Snapshot() (settings.Values, error) {
	return settings.Values{}, errors.New("config 目录不可读")
}

func TestSettingsStoreLoadError(t *testing.T) {
	svc := &failingSettings{}
	store := newSettingsStore(svc, nil)
	defer store.Close()
	if store.LoadError() == nil {
		t.Fatal("首次读取失败时 LoadError 应该非空")
	}
	// 服务恢复后订阅到新值：错误清除，缓存换成真实值。
	if _, err := svc.Update(func(v *settings.Values) { v.UI.AutoSave = false }); err != nil {
		t.Fatal(err)
	}
	if err := store.LoadError(); err != nil {
		t.Errorf("订阅到新值后 LoadError 应清除：%v", err)
	}
	if store.Values().UI.AutoSave {
		t.Error("订阅到新值后缓存没有更新")
	}
}

func TestSettingsStoreUpdateFailureKeepsValues(t *testing.T) {
	svc := &servicestest.Settings{}
	store := newSettingsStore(svc, nil)
	defer store.Close()
	svc.FailWith(errors.New("磁盘已满"))
	err := store.Update(func(v *settings.Values) { v.UI.AutoSave = false })
	if err == nil {
		t.Fatal("服务写入失败时 Update 应返回错误")
	}
	if !store.Values().UI.AutoSave {
		t.Error("写入失败后缓存里的值不应改变")
	}
}

func TestSettingsStoreCloseUnsubscribes(t *testing.T) {
	svc := &servicestest.Settings{}
	store := newSettingsStore(svc, nil)
	if svc.Subscribers() != 1 {
		t.Fatalf("订阅数 = %d，期望 1", svc.Subscribers())
	}
	store.Close()
	store.Close() // 重复关闭不出错
	if svc.Subscribers() != 0 {
		t.Errorf("Close 后订阅数 = %d，期望 0", svc.Subscribers())
	}
}

func TestSettingsStoreWithoutService(t *testing.T) {
	store := newSettingsStore(nil, nil)
	defer store.Close()
	if err := store.Update(func(v *settings.Values) { v.UI.ShowLineNumbers = true }); err != nil {
		t.Fatal(err)
	}
	if !store.Values().UI.ShowLineNumbers {
		t.Error("没有服务时应只在内存里读写")
	}
}

func TestRunRejectsEmptyClass(t *testing.T) {
	if err := Run(Options{}); err == nil {
		t.Error("Class 为空时应拒绝启动（WebView2 profile 必须用专用名）")
	}
}
