package ask

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
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

// 本文件是全局问答界面的无头测试台（L4）：不开真窗口、不碰 WebView2，毫秒级。提示条不画（用例经 notifier.Queued() 读提示），
// 后台动作默认同步执行（Store.Spawn），每一步之后的画面是确定的；要观察「进行中」的中间态时，用例用 goroutines 改回真正的后台，
// 再用 AskScript 的 Gate 卡住回答。侧栏与工作区并排画在一个窗口里。

var uiFontOnce sync.Once

// uiNow 是对照用的固定时钟。
var uiNow = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

type uiRig struct {
	t       *testing.T
	session *headless.Session
	svc     *servicestest.AskFake
	inbox   *servicestest.Inbox
	clock   *servicestest.ManualClock
	prefs   *memPrefs

	store    *Store
	history  *HistoryStore
	setup    *Setup
	screen   *Screen
	look     *kit.Look
	notifier *kit.Notifier
	log      *recLog
	tips     kit.TooltipState
	canvas   gfx.Canvas

	hidden bool // 为真时整个模块不画（模拟切到别的模块）
	// articleSpec 非空时在右侧画一块围绕本文面板（模拟知识库详情的右侧栏），与模块是否隐藏无关。
	articleSpec *ArticleSpec
	shiftDown   atomic.Bool // 模拟 Shift 是否按着（ShiftHeld 读它）

	mu         sync.Mutex
	opened     []services.AskSource // Hooks.OpenSource 收到的来源
	settingsAI int                  // OnOpenSettingsAI 的调用次数
	copied     []string             // 剪贴板收到的文字
}

// newUIRig 在 1280×800、缩放 1 的无头会话里打开问答模块；mutate 在首帧之前调整服务与界面。
func newUIRig(t *testing.T, mutate ...func(*uiRig)) *uiRig {
	t.Helper()
	uiFontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	r := &uiRig{
		t: t, svc: &servicestest.AskFake{}, inbox: &servicestest.Inbox{}, clock: servicestest.NewManualClock(),
		prefs: &memPrefs{}, notifier: kit.NewNotifier(nil), log: &recLog{},
	}
	r.inbox.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 10, IndexedItems: 4})
	r.notifier.Clipboard = func(text string) error {
		r.mu.Lock()
		r.copied = append(r.copied, text)
		r.mu.Unlock()
		return nil
	}

	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff // 动效直接到终态，断言不受动画时钟影响
	r.look = kit.NewLook(spec)
	r.look.Scale = 1
	r.look.Viewport = 1280

	guard := kit.Guard{Notify: r.notifier, Log: r.log}
	r.store = NewStore(r.svc, guard, nil)
	r.store.Prefs = r.prefs
	r.store.Spawn = func(fn func()) { fn() }
	seq := 0
	r.store.NewID = func(prefix string) string { seq++; return prefix + "-" + string(rune('0'+seq)) }
	r.store.OnOpenSettingsAI = func() { r.locked(func() { r.settingsAI++ }) }
	r.history = NewHistoryStore(r.svc, guard, nil)
	// 与根同一处接线：会话保存或删除之后刷新历史列表。
	r.store.OnSaved = func() { r.history.Load(false) }
	r.setup = NewSetup(r.svc, r.inbox, guard, nil)
	r.setup.OnOpenSettingsAI = r.store.OnOpenSettingsAI
	r.screen = &Screen{
		Look: r.look, Store: r.store, History: r.history, Setup: r.setup, Ctx: context.Background(), Guard: guard,
		Now: func() time.Time { return uiNow }, Loc: time.UTC, After: r.clock.After,
		Hooks: Hooks{OpenSource: func(s services.AskSource) { r.locked(func() { r.opened = append(r.opened, s) }) }},
	}
	for _, m := range mutate {
		m(r)
	}

	// Enter 与 Shift+Enter 的区别靠 Shift 状态：生产读系统键盘，这里由用例按需按下。
	r.screen.ShiftHeld = r.shiftDown.Load

	bg := r.look.Col(r.look.T.Background)
	pg := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			r.look.BeginPaint()
			ui.Row(c).Fill().Children(func() {
				if !r.hidden {
					ui.Column(c.Key("panel")).Width(r.look.Rem(15)).FillHeight().Children(func() { r.screen.Panel(c) })
					ui.Column(c.Key("main")).Grow(1).FillHeight().MinWidth(0).Children(func() { r.screen.Workspace(c) })
				}
				if r.articleSpec != nil {
					ui.Column(c.Key("aside")).Width(r.look.Rem(25)).FillHeight().Shrink(0).Children(func() {
						r.screen.ArticlePanel(c, *r.articleSpec)
					})
				}
			})
			r.look.Tooltips(c, &r.tips)
			r.screen.EndFrame()
		},
		Theme: &r.look.Theme, Background: &bg,
	})
	config := pg.Config(app.Config{Title: t.Name(), Width: 1280, Height: 800})
	config.OnKey = kit.KeyFirst(r.look.OnKey, config.OnKey) // 与窗口同一处接线：钩子在先，页面的按键路由在后
	session, err := headless.Open(config, &r.canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	r.session = session
	r.frames(4)
	return r
}

func (r *uiRig) locked(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

// goroutines 让后台动作跑在真正的 goroutine 里（观察进行中的中间态用）。
func goroutines(r *uiRig) { r.store.Spawn = func(fn func()) { go fn() } }

// withSessions 在首帧之前往库里摆几条全局会话（每条一轮问答）。
func withSessions(ids ...string) func(*uiRig) {
	return func(r *uiRig) {
		for _, id := range ids {
			putSession(r.t, r.svc, id, done("m-"+id, "question "+id, "answer "+id))
		}
	}
}

// putSession 摆一条全局会话到库里。
func putSession(t *testing.T, svc *servicestest.AskFake, id string, msgs ...services.AskMessage) services.AskSessionRecord {
	t.Helper()
	body, err := marshalAskMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	rec := services.AskSessionRecord{MessagesJSON: body}
	rec.ID, rec.Title = id, buildSessionTitle(msgs)
	return svc.PutSession(rec)
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

// allText 是画面上全部文字按绘制顺序连起来的结果：折行的长句用它整句比对。
func (r *uiRig) allText() string { return strings.Join(r.session.Texts(), "") }

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

// 超出视口的内容在无障碍树里不可见（被裁剪），要点到它得先滚过去。
func (r *uiRig) scrollBy(x, y, dy float32) {
	r.t.Helper()
	if err := r.session.Scroll(gfx.ScrollEvent{X: x, Y: y, DeltaY: dy, Precise: true}); err != nil {
		r.t.Fatalf("滚动失败：%v", err)
	}
	r.frames(3)
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

// hover 把指针移到角色加名称对应控件的中心（悬停显现的动作行、悬停高亮用）。
func (r *uiRig) hover(role gfx.Role, name string) {
	r.t.Helper()
	n, ok := r.node(role, name)
	if !ok {
		r.t.Fatalf("找不到 %v「%s」", role, name)
	}
	if err := r.session.Move(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2); err != nil {
		r.t.Fatal(err)
	}
	r.frames(3)
}

func (r *uiRig) keys(spec string) {
	r.t.Helper()
	if err := r.session.Keys(spec); err != nil {
		r.t.Fatalf("按键 %q 失败：%v", spec, err)
	}
	r.frames(3)
}

// keysWithShift 按着 Shift 发一个按键（系统键盘状态由 shiftDown 模拟，按键本身带 shift 修饰）。
func (r *uiRig) keysWithShift(spec string) {
	r.t.Helper()
	r.shiftDown.Store(true)
	defer r.shiftDown.Store(false)
	r.keys("shift+" + spec)
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

// dump 列出无障碍树（调试用例时看画面上有什么）。
func (r *uiRig) dump() {
	for _, n := range r.session.Access() {
		r.t.Logf("%v %q %v", n.Role, n.Label, n.Bounds)
	}
}

func (r *uiRig) toasts() []kit.QueuedToast { return r.notifier.Queued() }

func (r *uiRig) copiedTexts() (out []string) {
	r.locked(func() { out = append(out, r.copied...) })
	return out
}

func (r *uiRig) settingsOpened() (n int) { r.locked(func() { n = r.settingsAI }); return n }

func (r *uiRig) openedSources() (out []services.AskSource) {
	r.locked(func() { out = append(out, r.opened...) })
	return out
}
