package overlays

import (
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
)

// 本文件是全局浮层界面的无头测试台（L4）：不开真窗口、不碰 WebView2，毫秒级。
// 各 Store 沿用同目录的 rig（同步执行、手动计时器），Screen 在 1280×800、缩放 1 的窗口里每帧声明全部浮层。
// 提示条不画（用例经 notifier.Queued() 读提示）。

var uiFontOnce sync.Once

// uiRig 是一个浮层界面测试台。Store 为 nil 表示该浮层没装配；用 withPalette 等选项在首帧之前装配。
type uiRig struct {
	*rig
	t       *testing.T
	session *headless.Session
	canvas  gfx.Canvas
	look    *kit.Look
	screen  *Screen
	tips    kit.TooltipState

	// 各浮层的服务替身
	lib     *servicestest.Library
	wiki    *servicestest.WikiFake
	imports *servicestest.Imports
	backup  *servicestest.Backup
	updater *servicestest.Updater
	ai      *servicestest.AISettingsFake
	prefs   *servicestest.Prefs
	capture *servicestest.Capture
	migrate *servicestest.Migration

	mu       sync.Mutex
	calls    []string // 钩子调用记录
	external []string // OpenExternal 收到的链接
}

// newUIRig 打开无头会话；mutate 在首帧之前装配 Store。
func newUIRig(t *testing.T, mutate ...func(*uiRig)) *uiRig {
	t.Helper()
	uiFontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	r := &uiRig{
		rig: newRig(t), t: t,
		lib: &servicestest.Library{}, wiki: &servicestest.WikiFake{}, imports: &servicestest.Imports{}, backup: &servicestest.Backup{},
		updater: &servicestest.Updater{}, ai: &servicestest.AISettingsFake{}, prefs: &servicestest.Prefs{},
		capture: &servicestest.Capture{}, migrate: &servicestest.Migration{},
	}
	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff // 动效直接到终态，断言不受动画时钟影响
	r.look = kit.NewLook(spec)
	r.look.Scale = 1
	r.look.Viewport = 1280
	r.screen = &Screen{
		Look: r.look, Guard: r.guard,
		OpenExternal: func(url string) error { r.record(&r.external, url); return nil },
	}
	for _, m := range mutate {
		m(r)
	}

	bg := r.look.Col(r.look.T.Background)
	pg := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			r.look.BeginPaint()
			r.screen.View(c)
			r.look.Tooltips(c, &r.tips)
		},
		Theme: &r.look.Theme, Background: &bg,
	})
	config := pg.Config(app.Config{Title: t.Name(), Width: 1280, Height: 800})
	config.OnKey = kit.KeyFirst(r.look.OnKey, config.OnKey)
	session, err := headless.Open(config, &r.canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	r.session = session
	r.frames(4)
	return r
}

func (r *uiRig) record(into *[]string, v string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*into = append(*into, v)
}

func (r *uiRig) called() (out []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append(out, r.calls...)
}

func (r *uiRig) links() (out []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append(out, r.external...)
}

// withPalette 装配命令面板（不自动打开）。
func withPalette(r *uiRig) {
	p := NewPalette(r.guard, r.invalidate)
	p.Env = r.env()
	p.Library, p.Wiki, p.Imports, p.Backup = r.lib, r.wiki, r.imports, r.backup
	rec := func(name string) func() { return func() { r.record(&r.calls, name) } }
	rec1 := func(name string) func(string) { return func(arg string) { r.record(&r.calls, name+":"+arg) } }
	p.Hooks = PaletteHooks{
		QuickCapture: rec("capture"), OpenInbox: rec("inbox"), BuildSemanticIndex: rec("index"), CompileWiki: rec("wiki-compile"),
		OpenLibraryItem: rec1("item"), OpenWikiPage: rec1("wiki"), SearchImportTasks: rec1("task"), OpenSettingsSection: rec1("settings"),
	}
	r.screen.Palette = p
}

// withUpdate 装配更新对话框（不自动打开）。
func withUpdate(r *uiRig) {
	u := NewUpdate(r.updater, r.guard, r.invalidate)
	u.Env = r.env()
	u.Options = func() services.UpdateCheckOptions { return services.UpdateCheckOptions{} }
	u.CurrentVersion = func() string { return "1.0.0" }
	u.AutoCheckEnabled = func() bool { return true }
	u.WindowVisible = func() bool { return true }
	u.Online = func() bool { return true }
	u.Attach()
	r.t.Cleanup(u.Detach)
	r.screen.Update = u
}

// withSetup 装配首次使用设置清单（不自动打开）。
func withSetup(r *uiRig) {
	s := NewSetup(r.guard, r.invalidate)
	s.Env = r.env()
	s.AI, s.Prefs, s.CaptureTools = r.ai, r.prefs, r.capture
	r.capture.SetFunasrStatus(services.FunasrStatus{})
	s.Hooks = SetupHooks{
		StartCapture:        func() { r.record(&r.calls, "capture") },
		OpenSettingsSection: func(section string) { r.record(&r.calls, "section:"+section) },
		RequestAIQuickSetup: func() { r.record(&r.calls, "quick") },
	}
	r.screen.Setup = s
}

// withMigration 装配旧版数据迁移对话框（不自动打开）。
func withMigration(r *uiRig) {
	m := NewMigration(r.guard, r.invalidate)
	m.Env = r.env()
	m.Service, m.Prefs = r.migrate, r.prefs
	m.OnMigrated = func() { r.record(&r.calls, "migrated") }
	m.OnClose = func() { r.record(&r.calls, "closed") }
	r.screen.Migration = m
}

func (r *uiRig) frames(n int) {
	r.t.Helper()
	for i := 0; i < n; i++ {
		if err := r.session.Frame(); err != nil {
			r.t.Fatalf("出帧失败：%v", err)
		}
	}
}

func (r *uiRig) node(role gfx.Role, name string) (app.AccessNode, bool) {
	return r.session.AccessByRole(role, name)
}

func (r *uiRig) has(role gfx.Role, name string) bool {
	_, ok := r.node(role, name)
	return ok
}

func (r *uiRig) hasButton(name string) bool { return r.has(gfx.RoleButton, name) }

// hasText 报告画面上是否有恰好等于 text 的一行文字。
func (r *uiRig) hasText(text string) bool {
	for _, got := range r.session.Texts() {
		if got == text {
			return true
		}
	}
	return false
}

// hasTextContaining 报告画面上是否有一行文字含 part。
func (r *uiRig) hasTextContaining(part string) bool {
	for _, got := range r.session.Texts() {
		if strings.Contains(got, part) {
			return true
		}
	}
	return false
}

func (r *uiRig) clickAt(x, y float32) {
	r.t.Helper()
	if err := r.session.Click(x, y); err != nil {
		r.t.Fatalf("点击 (%.0f, %.0f) 失败：%v", x, y, err)
	}
	r.frames(3)
}

func (r *uiRig) click(role gfx.Role, name string) {
	r.t.Helper()
	n, ok := r.node(role, name)
	if !ok {
		r.t.Fatalf("找不到 %v「%s」；画面上有 %q", role, name, r.session.Texts())
	}
	r.clickAt(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
}

func (r *uiRig) clickButton(name string) {
	r.t.Helper()
	r.click(gfx.RoleButton, name)
}

func (r *uiRig) keys(spec string) {
	r.t.Helper()
	if err := r.session.Keys(spec); err != nil {
		r.t.Fatalf("按键 %q 失败：%v", spec, err)
	}
	r.frames(3)
}

func (r *uiRig) typeText(text string) {
	r.t.Helper()
	if err := r.session.Type(text); err != nil {
		r.t.Fatalf("输入 %q 失败：%v", text, err)
	}
	r.frames(3)
}

// dump 列出无障碍树（调试用例时看画面上有什么）。
func (r *uiRig) dump() {
	for _, n := range r.session.Access() {
		r.t.Logf("%v %q %v", n.Role, n.Label, n.Bounds)
	}
	r.t.Logf("texts: %q", r.session.Texts())
}

// nodesOf 是角色与名称都匹配的全部节点（按无障碍树顺序）。
func nodesOf(r *uiRig, role gfx.Role, name string) (out []app.AccessNode) {
	for _, n := range r.session.Access() {
		if n.Role == role && n.Label == name {
			out = append(out, n)
		}
	}
	return out
}

func countNodes(r *uiRig, role gfx.Role, name string) int { return len(nodesOf(r, role, name)) }

// hasWrapped 报告画面上是否有这句话：折行的长句整句比对（断点处的空白不一定保留）。
func (r *uiRig) hasWrapped(text string) bool {
	return strings.Contains(noSpace(strings.Join(r.session.Texts(), "")), noSpace(text))
}

// noSpace 去掉全部空白。
func noSpace(s string) string { return strings.Join(strings.Fields(s), "") }