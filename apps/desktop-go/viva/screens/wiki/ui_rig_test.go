package wiki

import (
	"context"
	"encoding/json"
	"strings"
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
	"guizhi-mygo-shell/viva/locales"
)

// 本文件是 Wiki 界面的无头测试台（L4）：不开真窗口、不碰 WebView2，毫秒级。提示条不画（用例经 notifier.Queued() 读提示），
// 后台动作默认同步执行（Store.Spawn），每一步之后的画面是确定的；搜索防抖用手动时钟推进。侧栏与工作区并排画在一个窗口里，
// 滚轮钩子与生产同一处接线（Look.WheelHook → Screen.GraphWheel）。

var uiFontOnce sync.Once

// uiNow 是对照用的固定时钟。
var uiNow = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

// memPrefs 是内存里的偏好存取。
type memPrefs struct {
	mu   sync.Mutex
	data map[string]json.RawMessage
}

func (m *memPrefs) Get(key string) (json.RawMessage, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	return v, ok
}

func (m *memPrefs) Set(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = map[string]json.RawMessage{}
	}
	m.data[key] = raw
	return nil
}

type uiRig struct {
	t       *testing.T
	session *headless.Session
	svc     *servicestest.WikiFake
	clock   *servicestest.ManualClock
	prefs   *memPrefs

	store    *Store
	screen   *Screen
	look     *kit.Look
	notifier *kit.Notifier
	log      *recLog
	tips     kit.TooltipState
	canvas   gfx.Canvas

	hidden bool // 为真时整个模块不画（模拟切到别的模块）

	mu        sync.Mutex
	settings  int      // OpenSettingsAI 的调用次数
	general   int      // OpenSettingsGeneral 的调用次数
	external  []string // OpenExternal 收到的链接
	autoOn    bool     // 设置里的自动编译开关
	sources   []string // OnOpenSource 收到的条目
	extraWire func()
}

// newUIRig 在 1280×800、缩放 1 的无头会话里打开 Wiki 模块；mutate 在首帧之前调整服务与界面。
func newUIRig(t *testing.T, mutate ...func(*uiRig)) *uiRig {
	t.Helper()
	uiFontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	r := &uiRig{
		t: t, svc: &servicestest.WikiFake{}, clock: servicestest.NewManualClock(),
		prefs: &memPrefs{}, notifier: kit.NewNotifier(nil), log: &recLog{},
	}
	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff // 动效直接到终态，断言不受动画时钟影响
	r.look = kit.NewLook(spec)
	r.look.Scale = 1
	r.look.Viewport = 1280

	guard := kit.Guard{Notify: r.notifier, Log: r.log}
	r.store = NewStore(r.svc, guard, nil)
	r.store.Spawn = func(fn func()) { fn() }
	r.store.Ctx = context.Background()
	r.store.OnOpenSource = func(id string) { r.locked(func() { r.sources = append(r.sources, id) }) }
	r.screen = &Screen{
		Look: r.look, Store: r.store, Ctx: context.Background(), Guard: guard, Prefs: r.prefs,
		Now: func() time.Time { return uiNow }, Loc: time.UTC, After: r.clock.After,
		Hooks: Hooks{
			OpenSettingsAI:      func() { r.locked(func() { r.settings++ }) },
			OpenSettingsGeneral: func() { r.locked(func() { r.general++ }) },
			AutoCompileEnabled:  func() bool { return r.autoEnabled() },
			OpenExternal:        func(url string) error { r.locked(func() { r.external = append(r.external, url) }); return nil },
		},
	}
	r.look.WheelHook = r.screen.GraphWheel
	for _, m := range mutate {
		m(r)
	}

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
			})
			r.look.Tooltips(c, &r.tips)
			r.screen.EndFrame()
		},
		Theme: &r.look.Theme, Background: &bg,
	})
	config := pg.Config(app.Config{Title: t.Name(), Width: 1280, Height: 800})
	config.OnKey = kit.KeyFirst(r.look.OnKey, config.OnKey)
	// 与窗口同一处接线：滚轮先问窗口级钩子（图谱缩放），再交给页面。
	next := config.OnScroll
	config.OnScroll = func(e gfx.ScrollEvent) gfx.EventResult {
		if r.look.WheelHook != nil {
			if res := r.look.WheelHook(e); res.Handled {
				return res
			}
		}
		if next == nil {
			return gfx.EventResult{}
		}
		return next(e)
	}
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

func (r *uiRig) autoEnabled() (on bool) { r.locked(func() { on = r.autoOn }); return on }

// withAutoCompile 在首帧之前打开设置里的自动编译开关。
func withAutoCompile(r *uiRig) { r.autoOn = true }

// withSeed 在首帧之前摆好三页（Alpha → Beta 一条链接，Beta 手动编辑过，Gamma 是实体）。
func withSeed(r *uiRig) {
	r.svc.PutPage(services.WikiPage{ID: "alpha", Title: "Alpha", Summary: "first page", Body: "body of alpha"})
	now := int64(5)
	r.svc.PutPage(services.WikiPage{ID: "beta", Title: "Beta", ManualEditedAt: &now, Body: "body of beta"})
	r.svc.PutPage(services.WikiPage{ID: "gamma", Title: "Gamma", Kind: services.WikiKindEntity, Body: "body of gamma"})
	r.svc.PutLink("alpha", "beta")
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

// goroutines 让后台动作跑在真正的 goroutine 里（观察编译进行中的中间态用）。
func goroutines(r *uiRig) { r.store.Spawn = func(fn func()) { go fn() } }

// settle 在后台跑的用例里等条件成立，每轮出一帧让画面跟上。
func (r *uiRig) settle(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.frames(1)
		if cond() {
			break
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("等待超时：%s；画面上有 %q", what, r.session.Texts())
		}
		time.Sleep(time.Millisecond)
	}
	r.frames(2)
}

// noSpace 去掉全部空白：折行的长句整句比对时，断点处的空格不一定保留。
func noSpace(s string) string { return strings.Join(strings.Fields(s), "") }

// top 是节点上沿的 y 坐标（比较排列先后用）；找不到就失败。
func (r *uiRig) top(role gfx.Role, name string) float32 {
	r.t.Helper()
	n, ok := r.node(role, name)
	if !ok {
		r.t.Fatalf("找不到 %v「%s」；画面上有 %q", role, name, r.session.Texts())
	}
	return n.Bounds.Y
}

// focusSearch 点一下目录的搜索框，让它拿到键盘。
func (r *uiRig) focusSearch() {
	r.t.Helper()
	r.click(gfx.RoleTextField, locales.WikiFilterPlaceholder())
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

func (r *uiRig) scrollBy(x, y, dy float32) {
	r.t.Helper()
	if err := r.session.Scroll(gfx.ScrollEvent{X: x, Y: y, DeltaY: dy, Precise: true}); err != nil {
		r.t.Fatalf("滚动失败：%v", err)
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

func (r *uiRig) toasts() []kit.QueuedToast { return r.notifier.Queued() }

// wantToast 断言最近一条提示的类别、标题与详情。
func (r *uiRig) wantToast(kind kit.ToastKind, message, detail string) {
	r.t.Helper()
	all := r.toasts()
	if len(all) == 0 {
		r.t.Fatalf("期望有一条提示 %q，实际没有", message)
	}
	got := all[len(all)-1]
	if got.Kind != kind || got.Message != message || got.Detail != detail {
		r.t.Fatalf("提示不对：\n got  [%s] %q (detail %q)\n want [%s] %q (detail %q)", got.Kind, got.Message, got.Detail, kind, message, detail)
	}
}

func (r *uiRig) settingsOpened() (n int) { r.locked(func() { n = r.settings }); return n }
func (r *uiRig) generalOpened() (n int)  { r.locked(func() { n = r.general }); return n }

func (r *uiRig) openedSources() (out []string) {
	r.locked(func() { out = append(out, r.sources...) })
	return out
}

func (r *uiRig) openedLinks() (out []string) {
	r.locked(func() { out = append(out, r.external...) })
	return out
}
