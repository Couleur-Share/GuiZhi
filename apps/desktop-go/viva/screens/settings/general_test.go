package settings_test

import (
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// rig 是无头会话里的「应用设置」页：不开真窗口、不碰 WebView2。
type rig struct {
	t *testing.T
	s *headless.Session
}

var fontOnce sync.Once

// openGeneral 用关闭动效的外观在 1280x800 里打开「应用设置」页（动效直接到终态，断言不受动画时钟影响）。
func openGeneral(t *testing.T, store sscreen.Store) *rig {
	t.Helper()
	fontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff
	look := kit.NewLook(spec)
	look.Scale = 1
	look.Viewport = 1280
	g := &sscreen.General{Look: look, Store: store}
	bg := look.Col(look.T.Background)
	page := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			g.Page(c)
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
	return r
}

func (r *rig) frames(n int) {
	r.t.Helper()
	for i := 0; i < n; i++ {
		if err := r.s.Frame(); err != nil {
			r.t.Fatalf("出帧失败：%v", err)
		}
	}
}

// node 按角色与名称取可访问节点。
func (r *rig) node(role gfx.Role, name string) (app.AccessNode, bool) {
	return r.s.AccessByRole(role, name)
}

func (r *rig) has(role gfx.Role, name string) bool {
	_, ok := r.node(role, name)
	return ok
}

// clickNode 点击角色加名称对应控件的中心。
func (r *rig) clickNode(role gfx.Role, name string) {
	r.t.Helper()
	n, ok := r.node(role, name)
	if !ok {
		r.t.Fatalf("找不到 %v「%s」", role, name)
	}
	if err := r.s.Click(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2); err != nil {
		r.t.Fatalf("点击「%s」失败：%v", name, err)
	}
	r.frames(3)
}

func (r *rig) keys(spec string) {
	r.t.Helper()
	if err := r.s.Keys(spec); err != nil {
		r.t.Fatalf("按键 %q 失败：%v", spec, err)
	}
	r.frames(3)
}

func (r *rig) switchChecked(name string) bool {
	r.t.Helper()
	n, ok := r.node(gfx.RoleSwitch, name)
	if !ok {
		r.t.Fatalf("找不到开关「%s」", name)
	}
	return n.Checked
}

func newStore() *sscreen.MemStore { return sscreen.NewMemStore(nsettings.Default()) }

// 打开「后台任务」先弹确认（它会同时改开机启动、隐藏启动与关闭行为），确认之前设置不变、开关仍是关的。
func TestBackgroundTasksAsksBeforeEnabling(t *testing.T) {
	store := newStore()
	r := openGeneral(t, store)
	name := locales.SettingsBackgroundTasks()
	r.clickNode(gfx.RoleSwitch, name)
	if !r.has(gfx.RoleGroup, locales.SettingsBackgroundTasksConfirmTitle()) {
		t.Fatal("打开「后台任务」应当先弹出确认对话框")
	}
	if store.Updates != 0 || store.Values().App.BackgroundTasksEnabled {
		t.Errorf("确认之前不应写入设置：写入 %d 次，BackgroundTasksEnabled=%v", store.Updates, store.Values().App.BackgroundTasksEnabled)
	}
	if r.switchChecked(name) {
		t.Error("确认之前开关应当仍是关的（受控开关）")
	}
}

// 取消、按 Esc、点遮罩都只关闭对话框，不写入。
func TestBackgroundTasksDismissKeepsSettings(t *testing.T) {
	dismiss := map[string]func(r *rig){
		"取消按钮": func(r *rig) { r.clickNode(gfx.RoleButton, locales.CommonCancel()) },
		"Esc":  func(r *rig) { r.keys("Escape") },
		"点遮罩": func(r *rig) {
			if err := r.s.Click(8, 8); err != nil { // 窗口左上角：遮罩上、面板之外
				r.t.Fatalf("点击遮罩失败：%v", err)
			}
			r.frames(3)
		},
	}
	for how, do := range dismiss {
		t.Run(how, func(t *testing.T) {
			store := newStore()
			r := openGeneral(t, store)
			r.clickNode(gfx.RoleSwitch, locales.SettingsBackgroundTasks())
			title := locales.SettingsBackgroundTasksConfirmTitle()
			if !r.has(gfx.RoleGroup, title) {
				t.Fatal("对话框没有打开")
			}
			do(r)
			if r.has(gfx.RoleGroup, title) {
				t.Error("对话框应当已关闭")
			}
			if store.Updates != 0 || store.Values().App.BackgroundTasksEnabled {
				t.Errorf("关闭对话框不应写入设置：写入 %d 次", store.Updates)
			}
		})
	}
}

// 确认后一次写入四项：后台任务、开机启动、隐藏启动、关闭按钮最小化到托盘（渲染层 setBackgroundTasksEnabled(true)）。
func TestBackgroundTasksConfirmWritesFourFields(t *testing.T) {
	confirm := map[string]func(r *rig){
		"点启用":     func(r *rig) { r.clickNode(gfx.RoleButton, locales.CommonEnable()) },
		"按 Enter": func(r *rig) { r.keys("Enter") },
	}
	for how, do := range confirm {
		t.Run(how, func(t *testing.T) {
			store := newStore()
			r := openGeneral(t, store)
			r.clickNode(gfx.RoleSwitch, locales.SettingsBackgroundTasks())
			do(r)
			v := store.Values()
			if !v.App.BackgroundTasksEnabled || !v.App.LaunchAtStartup || !v.App.MinimizeOnLaunch || v.UI.CloseAction != nsettings.CloseMinimize {
				t.Errorf("确认后应当同时开启四项：后台任务=%v 开机启动=%v 隐藏启动=%v 关闭行为=%q",
					v.App.BackgroundTasksEnabled, v.App.LaunchAtStartup, v.App.MinimizeOnLaunch, v.UI.CloseAction)
			}
			if store.Updates != 1 {
				t.Errorf("四项应在一次写入里完成，实际 %d 次", store.Updates)
			}
			if r.has(gfx.RoleGroup, locales.SettingsBackgroundTasksConfirmTitle()) {
				t.Error("确认后对话框应当关闭")
			}
			if !r.switchChecked(locales.SettingsBackgroundTasks()) {
				t.Error("确认后「后台任务」开关应当是开的")
			}
		})
	}
}

// 关闭后台任务不需要确认，立即写入，且只改这一项。
func TestBackgroundTasksDisableWritesImmediately(t *testing.T) {
	initial := nsettings.Default()
	initial.App.BackgroundTasksEnabled = true
	initial.App.LaunchAtStartup = true
	store := sscreen.NewMemStore(initial)
	r := openGeneral(t, store)
	name := locales.SettingsBackgroundTasks()
	if !r.switchChecked(name) {
		t.Fatal("初始值是开，开关应当是开的")
	}
	r.clickNode(gfx.RoleSwitch, name)
	if r.has(gfx.RoleGroup, locales.SettingsBackgroundTasksConfirmTitle()) {
		t.Error("关闭不应弹确认")
	}
	v := store.Values()
	if v.App.BackgroundTasksEnabled || !v.App.LaunchAtStartup {
		t.Errorf("只应关闭后台任务这一项：后台任务=%v 开机启动=%v", v.App.BackgroundTasksEnabled, v.App.LaunchAtStartup)
	}
	if r.switchChecked(name) {
		t.Error("关闭后开关应当是关的")
	}
}

// 普通开关点击后立即写入对应字段。
func TestPlainTogglesWriteTheirField(t *testing.T) {
	cases := []struct {
		name string
		got  func(nsettings.Values) bool
	}{
		{locales.SettingsLaunchAtStartup(), func(v nsettings.Values) bool { return v.App.LaunchAtStartup }},
		{locales.SettingsMinimizeOnLaunch(), func(v nsettings.Values) bool { return v.App.MinimizeOnLaunch }},
		{locales.SettingsShowLineNumbers(), func(v nsettings.Values) bool { return v.UI.ShowLineNumbers }},
		{locales.SettingsWikiAutoCompile(), func(v nsettings.Values) bool { return v.UI.WikiCompileEnabled }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newStore()
			r := openGeneral(t, store)
			before := c.got(store.Values())
			r.clickNode(gfx.RoleSwitch, c.name)
			if c.got(store.Values()) == before {
				t.Errorf("点击「%s」后设置应当翻转（原值 %v）", c.name, before)
			}
			if r.switchChecked(c.name) == before {
				t.Errorf("点击「%s」后开关状态应当翻转", c.name)
			}
		})
	}
}
