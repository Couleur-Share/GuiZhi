package library

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
)

// 本文件是知识库侧栏的无头测试台：不开真窗口、不碰 WebView2，毫秒级。
// 撤销窗口用手动时钟推进，后台动作同步执行，所以每一步之后的画面是确定的。

var rigFontOnce sync.Once

// rigNow 是测试里固定的「现在」。
var rigNow = time.UnixMilli(1_800_000_000_000)

// rig 是无头会话里的知识库侧栏，右下角是提示条。
type rig struct {
	t        *testing.T
	session  *headless.Session
	svc      *servicestest.Library
	prefs    *servicestest.Prefs
	store    *Store
	screen   *Screen
	clock    *servicestest.ManualClock
	notifier *kit.Notifier
	log      *recLog
	look     *kit.Look
	// hidden 为真时这一帧不画侧栏（模拟切到别的模块）。
	hidden bool
	// workspace 为真时侧栏右边画工作区（列表栏）；wsHidden 为真时这一帧不画它（模拟切到别的模块，侧栏仍在）。
	workspace, wsHidden bool
	canvas              gfx.Canvas
	nextID              int
	copied              []string
}

// libraryFixture 是常用的数据：两个合集、三个标签，其中一个标签没有条目。
func libraryFixture() (services.LibraryCounts, []services.LibraryCollection, []services.LibraryTag) {
	counts := services.LibraryCounts{
		Uncategorized: 3, All: 12, Favorites: 2, Archived: 1, Trash: 4,
		ByCollection: map[string]int{"c1": 5, "c2": 120},
		ByTag:        map[string]int{"t1": 3, "t2": 1},
		ByPlatform:   map[string]int{"bilibili": 4, "youtube": 2},
	}
	collections := []services.LibraryCollection{{ID: "c1", Name: "工作"}, {ID: "c2", Name: "阅读", Icon: "📚"}}
	tags := []services.LibraryTag{
		{ID: "t1", Name: "go", ColorKey: "blue"},
		{ID: "t2", Name: "rust", ColorKey: "red"},
		{ID: "t3", Name: "闲置", ColorKey: "green"},
	}
	return counts, collections, tags
}

// newRig 在 1280×800、缩放 1 的无头会话里打开侧栏（宽 240），装入 fixture 数据；mutate 在首帧之前调整。
func newRig(t *testing.T, mutate ...func(*rig)) *rig {
	t.Helper()
	rigFontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	r := &rig{t: t, svc: &servicestest.Library{}, prefs: &servicestest.Prefs{}, clock: servicestest.NewManualClock(), log: &recLog{}}
	r.svc.Seed(libraryFixture())
	r.notifier = kit.NewNotifier(nil)
	r.notifier.Clipboard = func(text string) error { r.copied = append(r.copied, text); return nil }

	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff // 动效直接到终态，断言不受动画时钟影响
	r.look = kit.NewLook(spec)
	r.look.Scale = 1
	r.look.Viewport = 1280

	guard := kit.Guard{Notify: r.notifier, Log: r.log}
	r.store = NewStore(r.svc, guard, nil)
	r.store.After = r.clock.After
	r.store.Spawn = func(fn func()) { fn() }
	r.screen = &Screen{
		Look: r.look, Store: r.store, Ctx: context.Background(), Guard: guard, Prefs: r.prefs,
		Now:   func() time.Time { return rigNow },
		NewID: func() string { r.nextID++; return "sv-" + string(rune('0'+r.nextID)) },
	}
	for _, m := range mutate {
		m(r)
	}

	// 窗口级键盘（方向键、Home、End、空格，B-045）与生产同一处接线：经 Look.KeyHook，焦点从画布读。
	r.look.KeyHook = func(e gfx.KeyEvent) gfx.EventResult { return r.screen.ListKey(e, &r.canvas) }

	bg := r.look.Col(r.look.T.Background)
	pg := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			r.look.BeginPaint()
			ui.Row(c).Fill().Children(func() {
				ui.Column(c.Key("panel")).Width(r.look.Rem(15)).FillHeight().Children(func() {
					if !r.hidden {
						r.screen.Panel(c)
					}
				})
				if r.workspace && !r.wsHidden {
					ui.Column(c.Key("ws")).Grow(1).MinWidth(0).FillHeight().Children(func() { r.screen.Workspace(c) })
				}
			})
			r.screen.EndFrame()
			r.look.Toasts(c, r.notifier)
		},
		Theme: &r.look.Theme, Background: &bg,
	})
	config := pg.Config(app.Config{Title: t.Name(), Width: 1280, Height: 800})
	config.OnKey = kit.KeyFirst(r.look.OnKey, config.OnKey) // 与窗口同一处接线（app.bindInput）：钩子在先，页面的按键路由在后
	session, err := headless.Open(config, &r.canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	r.session = session
	r.frames(4)
	return r
}

func (r *rig) frames(n int) {
	r.t.Helper()
	for i := 0; i < n; i++ {
		if err := r.session.Frame(); err != nil {
			r.t.Fatalf("出帧失败：%v", err)
		}
		// 侧栏列的每个孩子都要登记 flex 属性（一一对应、同序）；漏登记会退回「全部不收缩」，与渲染层有出入。
		if !r.hidden && r.screen.flex.Mismatch() {
			r.t.Fatalf("侧栏列登记的 flex 项数与实际声明的孩子数不符（漏了 s.flex.Item？）")
		}
	}
}

func (r *rig) node(role gfx.Role, name string) (app.AccessNode, bool) {
	return r.session.AccessByRole(role, name)
}

func (r *rig) has(role gfx.Role, name string) bool {
	_, ok := r.node(role, name)
	return ok
}

// mustNode 取节点，找不到就失败，并列出同角色的节点帮助定位。
func (r *rig) mustNode(role gfx.Role, name string) app.AccessNode {
	r.t.Helper()
	n, ok := r.node(role, name)
	if !ok {
		var names []string
		for _, node := range r.session.Access() {
			if node.Role == role {
				names = append(names, node.Label)
			}
		}
		r.t.Fatalf("找不到 %v「%s」；同角色的有 %q", role, name, names)
	}
	return n
}

func centerOf(n app.AccessNode) (float32, float32) {
	return n.Bounds.X + n.Bounds.Width/2, n.Bounds.Y + n.Bounds.Height/2
}

func (r *rig) clickAt(x, y float32) {
	r.t.Helper()
	if err := r.session.Click(x, y); err != nil {
		r.t.Fatalf("点击 (%.0f, %.0f) 失败：%v", x, y, err)
	}
	r.frames(3)
}

func (r *rig) click(role gfx.Role, name string) {
	r.t.Helper()
	x, y := centerOf(r.mustNode(role, name))
	r.clickAt(x, y)
}

func (r *rig) rightClick(role gfx.Role, name string) {
	r.t.Helper()
	x, y := centerOf(r.mustNode(role, name))
	if err := r.session.RightClick(x, y); err != nil {
		r.t.Fatalf("右键 %q 失败：%v", name, err)
	}
	r.frames(3)
}

func (r *rig) hover(role gfx.Role, name string) {
	r.t.Helper()
	x, y := centerOf(r.mustNode(role, name))
	if err := r.session.Move(x, y); err != nil {
		r.t.Fatalf("移动指针失败：%v", err)
	}
	r.frames(3)
}

func (r *rig) keys(spec string) {
	r.t.Helper()
	if err := r.session.Keys(spec); err != nil {
		r.t.Fatalf("按键 %q 失败：%v", spec, err)
	}
	r.frames(3)
}

func (r *rig) typeText(text string) {
	r.t.Helper()
	if err := r.session.Type(text); err != nil {
		r.t.Fatalf("输入 %q 失败：%v", text, err)
	}
	r.frames(3)
}

// hasText 报告画面上是否有恰好等于 text 的一行文字。
func (r *rig) hasText(text string) bool {
	for _, got := range r.session.Texts() {
		if got == text {
			return true
		}
	}
	return false
}

// inkAlpha 是画面上第一处恰好等于 text 的文字的不透明度（含所在分组的 Opacity，与对照工具判断「看不看得见」同一个量）；
// 没有这行文字时 ok 为假。
func (r *rig) inkAlpha(text string) (alpha float32, ok bool) {
	for _, t := range r.session.Snapshot().Texts {
		if t.Value == text {
			return t.Style.Color.Alpha, true
		}
	}
	return 0, false
}

// focusedIs 报告键盘焦点是否正落在 role、name 的节点上。
func (r *rig) focusedIs(role gfx.Role, name string) bool {
	for _, n := range r.session.Access() {
		if n.Focused && n.Role == role && n.Label == name {
			return true
		}
	}
	return false
}

// view 是 Store 此刻的快照。
func (r *rig) view() View { return r.store.View() }

// mutating 是服务收到的变更类调用（读取类的 Counts / Collections / Tags / List 不算）。
func (r *rig) mutating() []string {
	var out []string
	for _, call := range r.svc.Calls() {
		switch call {
		case "Counts", "Collections", "Tags", "List":
		default:
			out = append(out, call)
		}
	}
	return out
}
