package inbox

import (
	"context"
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

// 本文件是处理中心界面的无头测试台（L4）：不开真窗口、不碰 WebView2，毫秒级。提示条不画（Toasts 会把排队的提示领走），
// 用例经 notifier.Queued() 直接读到每一条提示的标题与详情；提示条本身的呈现由 kit 的测试负责。
// 后台动作默认同步执行（Store.Spawn），所以每一步之后的画面是确定的；要观察「进行中」的中间态时，用例用 goroutines 改回真正的后台，
// 再用服务替身的 Hold 卡住某个调用。

var uiFontOnce sync.Once

// uiRig 是无头会话里的处理中心：左边二级侧栏，右边工作区，右下角是提示条。
type uiRig struct {
	t        *testing.T
	session  *headless.Session
	svc      *servicestest.Inbox
	store    *Store
	screen   *Screen
	notifier *kit.Notifier
	log      *recLog
	tips     kit.TooltipState

	hidden      bool                         // 为真时整个模块不画（模拟切到别的模块）
	collections []services.LibraryCollection // Hooks.Collections 的返回
	collLoads   int                          // Hooks.LoadCollections 的调用次数
	opened      []string                     // Store.OnOpenItem 收到的条目
	settingsAI  int                          // Store.OnOpenSettingsAI 的调用次数
	wikiOpened  int                          // Store.OnWikiPreview 的调用次数
}

// newUIRig 在 1280×800、缩放 1 的无头会话里打开处理中心，列表由 items 给出；mutate 在首帧之前调整服务与界面。
func newUIRig(t *testing.T, items []services.InboxItem, mutate ...func(*uiRig)) *uiRig {
	t.Helper()
	uiFontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	r := &uiRig{t: t, svc: &servicestest.Inbox{}, notifier: kit.NewNotifier(nil), log: &recLog{}}
	r.svc.SetList(listOf(items...))
	r.notifier.Clipboard = func(string) error { return nil } // 测试不碰系统剪贴板

	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff // 动效直接到终态，断言不受动画时钟影响
	look := kit.NewLook(spec)
	look.Scale = 1
	look.Viewport = 1280

	guard := kit.Guard{Notify: r.notifier, Log: r.log}
	r.store = NewStore(r.svc, guard, nil)
	r.store.Spawn = func(fn func()) { fn() }
	r.store.OnOpenItem = func(_ context.Context, id string) { r.opened = append(r.opened, id) }
	r.store.OnOpenSettingsAI = func() { r.settingsAI++ }
	r.store.OnWikiPreview = func() { r.wikiOpened++ }
	r.screen = &Screen{
		Look: look, Store: r.store, Ctx: context.Background(), Guard: guard,
		Hooks: Hooks{
			Collections:     func() []services.LibraryCollection { return r.collections },
			LoadCollections: func(context.Context) { r.collLoads++ },
		},
	}
	for _, m := range mutate {
		m(r)
	}

	bg := look.Col(look.T.Background)
	pg := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			ui.Row(c).Fill().Children(func() {
				if !r.hidden {
					ui.Column(c.Key("panel")).Width(look.Rem(15)).FillHeight().Children(func() { r.screen.Panel(c) })
					ui.Column(c.Key("main")).Grow(1).FillHeight().MinWidth(0).Children(func() { r.screen.Workspace(c) })
				}
			})
			look.Tooltips(c, &r.tips)
			r.screen.EndFrame()
		},
		Theme: &look.Theme, Background: &bg,
	})
	var canvas gfx.Canvas
	session, err := headless.Open(pg.Config(app.Config{Title: t.Name(), Width: 1280, Height: 800}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	r.session = session
	r.frames(3)
	return r
}

// goroutines 让后台动作跑在真正的 goroutine 里（观察进行中的中间态用）。
func goroutines(r *uiRig) { r.store.Spawn = func(fn func()) { go fn() } }

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

// count 是角色与名称都匹配的节点个数。
func (r *uiRig) count(role gfx.Role, name string) int {
	n := 0
	for _, node := range r.session.Access() {
		if node.Role == role && node.Label == name {
			n++
		}
	}
	return n
}

// hasText 报告画面上是否有恰好等于 text 的一行文字。
func (r *uiRig) hasText(text string) bool {
	for _, got := range r.session.Texts() {
		if got == text {
			return true
		}
	}
	return false
}

// allText 是画面上全部文字按绘制顺序连起来的结果：折行的长句用它整句比对。
func (r *uiRig) allText() string { return strings.Join(r.session.Texts(), "") }

func (r *uiRig) clickAt(x, y float32) {
	r.t.Helper()
	if err := r.session.Click(x, y); err != nil {
		r.t.Fatalf("点击 (%.0f, %.0f) 失败：%v", x, y, err)
	}
	r.frames(3)
}

// click 点击角色加名称对应控件的中心。
func (r *uiRig) click(role gfx.Role, name string) {
	r.t.Helper()
	n, ok := r.node(role, name)
	if !ok {
		r.t.Fatalf("找不到 %v「%s」；画面上有 %q", role, name, r.session.Texts())
	}
	r.clickAt(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
}

// clickButton 点击名为 name 的按钮。
func (r *uiRig) clickButton(name string) {
	r.t.Helper()
	r.click(gfx.RoleButton, name)
}

// clickNthButton 点击第 n 个（从 0 起，按无障碍树顺序）名为 name 的按钮。
func (r *uiRig) clickNthButton(name string, n int) {
	r.t.Helper()
	seen := 0
	for _, node := range r.session.Access() {
		if node.Role == gfx.RoleButton && node.Label == name {
			if seen == n {
				r.clickAt(node.Bounds.X+node.Bounds.Width/2, node.Bounds.Y+node.Bounds.Height/2)
				return
			}
			seen++
		}
	}
	r.t.Fatalf("找不到第 %d 个按钮「%s」（只有 %d 个）", n, name, seen)
}

// clickText 点击第 n 个（从 0 起）内容恰好等于 text 的文字的中心。
func (r *uiRig) clickText(text string, n int) {
	r.t.Helper()
	seen := 0
	for _, got := range r.session.Snapshot().Texts {
		if got.Value != text {
			continue
		}
		if seen == n {
			r.clickAt(got.Bounds.X+got.Bounds.Width/2, got.Bounds.Y+got.Bounds.Height/2)
			return
		}
		seen++
	}
	r.t.Fatalf("画面上没有第 %d 个文字 %q；画面上有 %q", n, text, r.session.Texts())
}

func (r *uiRig) typeText(text string) {
	r.t.Helper()
	if err := r.session.Type(text); err != nil {
		r.t.Fatalf("输入 %q 失败：%v", text, err)
	}
	r.frames(3)
}

// settle 在后台跑的用例里等条件成立，每轮出一帧让画面跟上。
func (r *uiRig) settle(what string, cond func() bool) {
	r.t.Helper()
	waitFor(r.t, what, func() bool {
		r.frames(1)
		return cond()
	})
	r.frames(2)
}
