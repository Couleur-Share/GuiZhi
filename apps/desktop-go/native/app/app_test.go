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

type fakeHost struct{ invalidations atomic.Int32 }

func (h *fakeHost) Invalidate() { h.invalidations.Add(1) }

func newTestApp(t *testing.T, opts Options) (*App, *servicestest.Imports, *ui.Tester) {
	t.Helper()
	fake := &servicestest.Imports{}
	fake.Set(services.ImportPage{
		Entries: []services.ImportTask{{ID: "a", Status: "completed", SourceKind: "url", SourceInput: "https://example.com/p", DisplayName: "示例文章", CreatedAt: 1, UpdatedAt: 2}},
		Total:   1, Counts: map[string]int64{"completed": 1},
	}, nil)
	opts.Services = services.Services{Imports: fake, Now: func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC) }}
	a := New(opts)
	a.AttachHost(&fakeHost{})
	t.Cleanup(a.Close)
	return a, fake, ui.NewTester(a.View, 1100, 720)
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

func TestDarkThemeIsApplied(t *testing.T) {
	a, _, tr := newTestApp(t, Options{Theme: kit.ThemeSpec{Dark: true}})
	tr.Frame()
	if !a.theme.Dark {
		t.Fatal("深色规格应装配深色主题")
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
