package app

import (
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
