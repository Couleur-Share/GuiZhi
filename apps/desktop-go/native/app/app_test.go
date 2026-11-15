package app

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit"
	"guizhi-mygo-shell/native/locales"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
)

func TestMain(m *testing.M) {
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	m.Run()
}

// fakeHost 记录外壳对窗口与平台提出的请求。
type fakeHost struct {
	invalidations atomic.Int32
	systemDark    atomic.Bool
	appearance    atomic.Value // 最近一次 ApplyAppearance 的 kit.Mode
	applyCount    atomic.Int32
}

func (h *fakeHost) Invalidate()      { h.invalidations.Add(1) }
func (h *fakeHost) SystemDark() bool { return h.systemDark.Load() }
func (h *fakeHost) ApplyAppearance(m kit.Mode) {
	h.appearance.Store(m)
	h.applyCount.Add(1)
}

func (h *fakeHost) lastAppearance() kit.Mode {
	m, _ := h.appearance.Load().(kit.Mode)
	return m
}

func newTestApp(t *testing.T, opts Options) (*App, *servicestest.Imports, *ui.Tester) {
	t.Helper()
	a, fake, tr, _ := newTestAppWithHost(t, opts)
	return a, fake, tr
}

func newTestAppWithHost(t *testing.T, opts Options) (*App, *servicestest.Imports, *ui.Tester, *fakeHost) {
	t.Helper()
	fake := &servicestest.Imports{}
	fake.Set(services.ImportPage{
		Entries: []services.ImportTask{{ID: "a", Status: "completed", SourceKind: "url", SourceInput: "https://example.com/p", DisplayName: "示例文章", CreatedAt: 1, UpdatedAt: 2}},
		Total:   1, Counts: map[string]int64{"completed": 1},
	}, nil)
	opts.Services = services.Services{Imports: fake, Now: func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC) }}
	a := New(opts)
	host := &fakeHost{}
	a.AttachHost(host)
	t.Cleanup(a.Close)
	return a, fake, ui.NewTester(a.View, 1100, 720), host
}

func TestRailNavigatesBetweenModules(t *testing.T) {
	a, _, tr := newTestApp(t, Options{})
	tr.Frame()

	// 默认进入知识库：尚未迁移，必须如实说明而不是给一个空页面
	if a.Route() != RouteLibrary || !tr.HasText(locales.NativeShellNotMigrated(locales.NavLibrary())) {
		t.Fatalf("默认页应是知识库的未迁移占位，当前 %q", a.Route())
	}

	if err := tr.Click(locales.NavImports()); err != nil {
		t.Fatalf("点击「采集」：%v", err)
	}
	if a.Route() != RouteImports {
		t.Fatalf("点击后应进入采集页，当前 %q", a.Route())
	}
	a.Start()
	waitFor(t, func() bool { tr.Frame(); return tr.HasText("示例文章") })

	if err := tr.Click(locales.NavWiki()); err != nil {
		t.Fatalf("点击 Wiki：%v", err)
	}
	if !tr.HasText(locales.NativeShellNotMigrated(locales.NavWiki())) {
		t.Fatal("切到 Wiki 应显示未迁移占位")
	}
}

func TestRailControlsAreNamedForAssistiveTech(t *testing.T) {
	_, _, tr := newTestApp(t, Options{})
	tr.Frame()
	for _, name := range []string{locales.NavLibrary(), locales.NavInbox(), locales.NavAsk(), locales.NavWiki(), locales.NavImports(), locales.HeaderSettings()} {
		if info, ok := tr.AccessByRole(ui.RoleButton, name); !ok {
			t.Errorf("找不到名为 %q 的按钮", name)
		} else if info.Name == "" {
			t.Errorf("按钮 %q 没有可访问名", name)
		}
	}
}

func TestSettingsGearNavigates(t *testing.T) {
	a, _, tr := newTestApp(t, Options{})
	tr.Frame()
	if err := tr.Click(locales.HeaderSettings()); err != nil {
		t.Fatalf("点击设置：%v", err)
	}
	if a.Route() != RouteSettings {
		t.Fatalf("应进入设置页，当前 %q", a.Route())
	}
}

func TestReadyFollowsFirstLoadOfStartRoute(t *testing.T) {
	// 未迁移的占位页没有数据要等，立即就绪
	placeholder, _, _ := newTestApp(t, Options{Route: RouteWiki})
	if !placeholder.Ready() {
		t.Error("占位页应立即就绪")
	}

	// 采集页：首次加载有结果才就绪；卡住期间不能就绪
	a, fake, tr := newTestApp(t, Options{Route: RouteImports})
	release := fake.Hold()
	a.Start()
	tr.Frame()
	time.Sleep(50 * time.Millisecond)
	if a.Ready() {
		t.Error("首次加载尚未返回，不应就绪")
	}
	release()
	waitFor(t, a.Ready)

	// 加载失败也算「首屏已出」：错误态是真实内容，-bench 不能因此一直等下去
	failing, failFake, _ := newTestApp(t, Options{Route: RouteImports})
	failFake.Set(services.ImportPage{}, errors.New("数据库不可用"))
	failing.Start()
	waitFor(t, failing.Ready)
}

func darkSpec() kit.ThemeSpec {
	s := kit.DefaultThemeSpec()
	s.Mode = kit.ModeDark
	return s
}

func TestDarkThemeIsApplied(t *testing.T) {
	a, _, tr, host := newTestAppWithHost(t, Options{Theme: darkSpec()})
	tr.Frame()
	if !a.applied.Dark {
		t.Fatal("深色规格应装配深色主题")
	}
	// 系统绘制的窗口按钮要跟着应用外观，否则深色应用里会出现深色字形压深色底
	if host.lastAppearance() != kit.ModeDark {
		t.Fatalf("应让系统外观来源切到 dark，实际 %q", host.lastAppearance())
	}
}

func TestZeroThemeOptionMeansDefaultLook(t *testing.T) {
	a, _, tr := newTestApp(t, Options{})
	tr.Frame()
	def := kit.DefaultThemeSpec()
	if a.spec != def {
		t.Fatalf("零值 Options.Theme 应取默认外观，实际 %+v", a.spec)
	}
}

func TestSystemModeFollowsHostAndRebuildsOnChange(t *testing.T) {
	a, _, tr, host := newTestAppWithHost(t, Options{})
	tr.Frame()
	if a.applied.Dark {
		t.Fatal("系统浅色时不应是深色")
	}
	first := a.applied

	// 外观输入没变：沿用同一次装配，不白白重建主题
	tr.Frame()
	if a.applied != first {
		t.Fatal("输入没变却重新装配了外观")
	}

	host.systemDark.Store(true) // 系统切到深色（真实运行里由 Theme.OnUpdated 触发重绘）
	tr.Frame()
	if !a.applied.Dark || a.applied == first {
		t.Fatal("系统切到深色后应重新装配为深色")
	}
}

func TestExplicitModeIgnoresSystem(t *testing.T) {
	spec := kit.DefaultThemeSpec()
	spec.Mode = kit.ModeLight
	a, _, tr, host := newTestAppWithHost(t, Options{Theme: spec})
	host.systemDark.Store(true)
	tr.Frame()
	if a.applied.Dark {
		t.Fatal("显式浅色不应受系统深色影响")
	}
}

func TestSetAppearanceAppliesModeZoomAndGlass(t *testing.T) {
	a, _, tr, host := newTestAppWithHost(t, Options{})
	tr.Frame()
	invalidations := host.invalidations.Load()
	applies := host.applyCount.Load()

	spec := kit.DefaultThemeSpec()
	spec.Mode, spec.FontSize = kit.ModeDark, "large"
	spec.Wallpaper = kit.DemoWallpaper(64, 40, 1, 0)
	a.SetAppearance(spec)

	if host.invalidations.Load() == invalidations {
		t.Error("换外观应请求重绘")
	}
	if host.applyCount.Load() == applies || host.lastAppearance() != kit.ModeDark {
		t.Errorf("明暗变了应通知平台，实际 %q", host.lastAppearance())
	}
	tr.Frame()
	if !a.applied.Dark || !a.applied.Glass {
		t.Fatalf("应是深色玻璃外观：Dark=%v Glass=%v", a.applied.Dark, a.applied.Glass)
	}
	if want := float32(18) / 16; a.appliedZoom != want {
		t.Fatalf("大字号档应整窗缩放到 %v，实际 %v", want, a.appliedZoom)
	}

	// 同样的外观再设一次：不触发任何事
	invalidations, applies = host.invalidations.Load(), host.applyCount.Load()
	a.SetAppearance(spec)
	if host.invalidations.Load() != invalidations || host.applyCount.Load() != applies {
		t.Error("外观没变，不应重绘或通知平台")
	}
}

func TestFontSizeZoomChangesLayoutSize(t *testing.T) {
	// 字号档位是整窗缩放：同一个窗口里，内容的 DIP 宽度与缩放成反比
	spec := kit.DefaultThemeSpec()
	spec.FontSize = "large"
	a, _, tr := newTestApp(t, Options{Theme: spec})
	tr.Frame()
	if a.appliedZoom <= 1 {
		t.Fatalf("大字号档应放大，zoom=%v", a.appliedZoom)
	}
	if !tr.HasText(locales.NativeShellNotMigrated(locales.NavLibrary())) {
		t.Fatal("放大后内容仍应正常绘制")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("等待条件超时")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
