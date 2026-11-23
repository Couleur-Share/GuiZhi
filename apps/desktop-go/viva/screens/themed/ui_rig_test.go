package themed

import (
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
	"guizhi-mygo-shell/viva/reader"
)

// 本文件是主题阅读界面的无头测试台（L4）：不开真窗口、不碰 WebView2。测试台扮演「知识库详情」「处理中心」「设置页」三个宿主之一，
// 每一帧按生产同样的顺序调用 Bind → Toolbar → Area → Dialogs（或 TasksPage / SearchPanel），再调 EndFrame。
// 后台动作同步执行（SetSpawn），预览节流用手动计时器；提示条不画，用例经 notifier.Queued() 读提示。

var uiFontOnce sync.Once

type hostMode int

const (
	hostLibrary hostMode = iota // 知识库详情里的阅读区
	hostTasks                   // 处理中心的任务页
	hostSearch                  // 设置 AI 分区里的联网搜索
	hostNone                    // 什么也不画（模拟切走）
)

type uiRig struct {
	t       *testing.T
	session *headless.Session
	svc     *servicestest.ThemedFake
	look    *kit.Look
	notify  *kit.Notifier
	log     *recLog
	timers  *timers
	screen  *Screen
	canvas  gfx.Canvas

	host    hostMode
	subject Subject
	// bind 为假时库详情这一帧没有绑定（模拟详情关了、切到别的条目前的空档）。
	bind bool
	// island 为真时测试台提供阅读岛挂载函数（记录收到的视图）；为假时 Area 画无头占位。
	island bool
	// back 记「返回处理中心」被点了几次。
	back int

	mu        sync.Mutex
	settings  int
	external  []string
	opened    []services.ThemedTask
	islands   []reader.View
	clipboard []string
}

func withHost(h hostMode) func(*uiRig) { return func(r *uiRig) { r.host = h } }
func withIsland(r *uiRig)              { r.island = true }

// newUIRig 在 1280×800 的无头会话里打开测试台；mutate 在首帧之前调整服务与宿主。
func newUIRig(t *testing.T, mutate ...func(*uiRig)) *uiRig {
	t.Helper()
	uiFontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	r := &uiRig{
		t: t, svc: &servicestest.ThemedFake{}, notify: kit.NewNotifier(nil), log: &recLog{}, timers: &timers{},
		bind: true, subject: Subject{ItemID: reqA.ItemID, Title: "Article A", Tab: TabBody, Enabled: true, ContentKey: "k1"},
	}
	r.notify.Clipboard = func(text string) error {
		r.locked(func() { r.clipboard = append(r.clipboard, text) })
		return nil
	}
	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff
	r.look = kit.NewLook(spec)
	r.look.Scale = 1
	r.look.Viewport = 1280

	guard := kit.Guard{Notify: r.notify, Log: r.log}
	r.screen = New(r.svc, guard, nil)
	r.screen.Look = r.look
	r.screen.SetSpawn(runNow)
	r.screen.Reading.After = r.timers.after
	r.screen.Reading.NewInstanceID = func() string { return "inst" }
	r.screen.Hooks = Hooks{
		OpenSettingsAI: func() { r.locked(func() { r.settings++ }) },
		OpenExternal:   func(url string) error { r.locked(func() { r.external = append(r.external, url) }); return nil },
		OpenItem: func(itemID, kind string) {
			r.locked(func() { r.opened = append(r.opened, services.ThemedTask{ItemID: itemID, SourceKind: kind}) })
		},
	}
	for _, m := range mutate {
		m(r)
	}

	bg := r.look.Col(r.look.T.Background)
	pg := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			r.look.BeginPaint()
			ui.Column(c).Fill().Children(func() { r.draw(c) })
			r.screen.EndFrame()
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

// draw 是宿主这一帧的内容。
func (r *uiRig) draw(c ui.C) {
	switch r.host {
	case hostLibrary:
		if r.bind {
			r.screen.Bind(r.subject)
		}
		ui.Row(c.Key("toolbar")).Items(ui.Center).Gap(8).Children(func() { r.screen.Toolbar(c) })
		var island func(c ui.C, v reader.View)
		if r.island {
			island = func(c ui.C, v reader.View) {
				r.locked(func() { r.islands = append(r.islands, v) })
				if v.Kind != reader.ViewNone {
					ui.Column(c).FillWidth().Grow(1).MinHeight(0)
				}
			}
		}
		r.screen.Area(c, island)
		r.screen.Dialogs(c)
	case hostTasks:
		r.screen.TasksPage(c, func() { r.back++ })
	case hostSearch:
		ui.Column(c.Key("settings")).FillWidth().Grow(1).MinHeight(0).Children(func() { r.screen.SearchPanel(c) })
	}
}

func (r *uiRig) locked(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

func (r *uiRig) frames(n int) {
	r.t.Helper()
	for i := 0; i < n; i++ {
		if err := r.session.Frame(); err != nil {
			r.t.Fatalf("出帧失败：%v", err)
		}
	}
}

// settle 出几帧让画面稳定（Bind 里的读取是同步的，下一帧才会画出结果）。
func (r *uiRig) settle() { r.frames(4) }

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

// textHas 报告画面全部文字连起来（去掉空白）是否含 sub：折行的长句用它比对。
func (r *uiRig) textHas(sub string) bool {
	return strings.Contains(noSpace(strings.Join(r.session.Texts(), "")), noSpace(sub))
}

func noSpace(s string) string { return strings.Join(strings.Fields(s), "") }

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

func (r *uiRig) typeText(text string) {
	r.t.Helper()
	if err := r.session.Type(text); err != nil {
		r.t.Fatalf("输入 %q 失败：%v", text, err)
	}
	r.frames(3)
}

func (r *uiRig) dump() {
	for _, n := range r.session.Access() {
		r.t.Logf("%v %q %v", n.Role, n.Label, n.Bounds)
	}
	r.t.Logf("texts: %q", r.session.Texts())
}

func (r *uiRig) toasts() []kit.QueuedToast { return r.notify.Queued() }

func (r *uiRig) lastToast() kit.QueuedToast {
	r.t.Helper()
	all := r.toasts()
	if len(all) == 0 {
		r.t.Fatal("期望有一条提示，实际没有")
	}
	return all[len(all)-1]
}

func (r *uiRig) settingsOpened() (n int) { r.locked(func() { n = r.settings }); return n }

func (r *uiRig) links() (out []string) {
	r.locked(func() { out = append(out, r.external...) })
	return out
}

func (r *uiRig) openedItems() (out []services.ThemedTask) {
	r.locked(func() { out = append(out, r.opened...) })
	return out
}

func (r *uiRig) copied() (out []string) {
	r.locked(func() { out = append(out, r.clipboard...) })
	return out
}

// lastIsland 是最近一帧阅读岛收到的视图。
func (r *uiRig) lastIsland() (reader.View, bool) {
	var v reader.View
	var ok bool
	r.locked(func() {
		if n := len(r.islands); n > 0 {
			v, ok = r.islands[n-1], true
		}
	})
	return v, ok
}

// aiOn 报告阅读模式是否在「AI 阅读」。不用 Screen.Active：它只在绑定的那一帧里有效，帧结束后恒为假。
func (r *uiRig) aiOn() bool { return r.screen.Mode.View().Active }

// activate 选中「AI 阅读」并出帧，让读取结果画出来。
func (r *uiRig) activate() {
	r.t.Helper()
	r.screen.Mode.Select(true)
	r.settle()
}

// putPage 摆一份当前格式页面。
func (r *uiRig) putPage(mutate ...func(*services.ThemedPage)) services.ThemedPage {
	p := services.ThemedPage{ID: "page-1", Assets: []services.ThemedAsset{}}
	for _, m := range mutate {
		m(&p)
	}
	return r.svc.PutPage(reqA, p)
}

// settleUntil 在条件成立前一直出帧（计时器、后台读取之类）。
func (r *uiRig) settleUntil(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.frames(1)
		if cond() {
			r.frames(2)
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("等待超时：%s；画面上有 %q", what, r.session.Texts())
		}
		time.Sleep(time.Millisecond)
	}
}
