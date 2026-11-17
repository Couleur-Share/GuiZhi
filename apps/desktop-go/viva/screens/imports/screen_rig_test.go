package imports

import (
	"context"
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
)

// 本文件是采集界面的无头测试台：不开真窗口、不碰 WebView2，毫秒级。
// 防抖与撤销窗口用手动时钟推进（manualClock，见 store_test.go），后台动作同步执行，所以每一步之后的画面是确定的。

// testNow 是测试里固定的「现在」（与渲染层 import-task-row.test.tsx 的 NOW 相同）。
const testNow int64 = 1_800_000_000_000

var rigFontOnce sync.Once

// rig 是无头会话里的采集工作区：左边二级侧栏，右边任务队列，右下角是提示条。
type rig struct {
	t        *testing.T
	session  *headless.Session
	svc      *servicestest.Imports
	store    *Store
	screen   *Screen
	clock    *manualClock
	notifier *kit.Notifier
	log      *recLog
	tips     kit.TooltipState // 悬停提示层（窗口根上的那一份），screen_hint_test.go 用它

	opened      []string // Hooks.OpenItem 收到的条目
	sections    []string // Hooks.OpenSettingsSection 收到的设置节
	quickOpened int      // Hooks.QuickCapture 的调用次数
	solver      bool     // Hooks.WebSolverEnabled 的返回值
	authRetried []string // Hooks.RetryAuthenticated 收到的「任务 id:平台」
	authOK      bool     // Hooks.RetryAuthenticated 的返回值：是否已发起登录态重试

	asked     []string // Hooks.AskAboutItem 收到的草稿
	nodeErr   error    // Hooks.VerifyNodeseek 的返回值
	nodeCalls []string // 「verify:任务 id」「cancel」，按调用顺序

	nowMs   int64    // 界面看到的「现在」（毫秒）；测试可以推进它来模拟时间流逝
	copied  []string // 写进剪贴板的文字，按顺序
	clipErr error    // 非空时写剪贴板失败
}

// newRig 在 1280×800、缩放 1 的无头会话里打开采集工作区，页面由 page 给出；mutate 在首帧之前调整界面。
func newRig(t *testing.T, page services.ImportPage, mutate ...func(*rig)) *rig {
	t.Helper()
	rigFontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	r := &rig{t: t, svc: &servicestest.Imports{}, clock: newManualClock(), log: &recLog{}, nowMs: testNow}
	r.svc.Set(page, nil)
	r.svc.SetQueueState(services.ImportQueueState{Concurrency: 2})
	r.notifier = kit.NewNotifier(nil)
	r.notifier.Clipboard = func(text string) error { // 测试不碰系统剪贴板
		if r.clipErr != nil {
			return r.clipErr
		}
		r.copied = append(r.copied, text)
		return nil
	}

	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff // 动效直接到终态，断言不受动画时钟影响
	look := kit.NewLook(spec)
	look.Scale = 1
	look.Viewport = 1280

	guard := kit.Guard{Notify: r.notifier, Log: r.log}
	r.store = NewStore(r.svc, guard, nil)
	r.store.After = r.clock.After
	r.store.Spawn = func(fn func()) { fn() }
	r.screen = &Screen{
		Look: look, Store: r.store, Ctx: context.Background(), Guard: guard,
		Hooks: Hooks{
			QuickCapture:        func() { r.quickOpened++ },
			OpenItem:            func(id string) { r.opened = append(r.opened, id) },
			CaptureShortcut:     func() string { return "Ctrl+N" },
			OpenSettingsSection: func(section string) { r.sections = append(r.sections, section) },
			WebSolverEnabled:    func() bool { return r.solver },
			RetryAuthenticated: func(task services.ImportTask, platform string) bool {
				r.authRetried = append(r.authRetried, task.ID+":"+platform)
				return r.authOK
			},
			AskAboutItem: func(draft string) { r.asked = append(r.asked, draft) },
			AppInfo:      func() (string, string) { return "0.28.7", "win32" },
			VerifyNodeseek: func(_ context.Context, task services.ImportTask) error {
				r.nodeCalls = append(r.nodeCalls, "verify:"+task.ID)
				return r.nodeErr
			},
			CancelNodeseek: func() error {
				r.nodeCalls = append(r.nodeCalls, "cancel")
				return nil
			},
		},
		Now:    func() time.Time { return time.UnixMilli(r.nowMs) },
		Loc:    time.FixedZone("Asia/Shanghai", 8*3600),
		Locale: kit.ParseDateLocale("zh-CN"),
	}
	for _, m := range mutate {
		m(r)
	}

	bg := look.Col(look.T.Background)
	pg := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			ui.Row(c).Fill().Children(func() {
				ui.Column(c.Key("panel")).Width(look.Rem(15)).FillHeight().Children(func() { r.screen.Panel(c) })
				ui.Column(c.Key("main")).Grow(1).FillHeight().MinWidth(0).Children(func() { r.screen.Workspace(c) })
			})
			look.Toasts(c, r.notifier)
			look.Tooltips(c, &r.tips)
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

// loaded 是首次读取完成之后的工作区：首帧排了 150 ms 的防抖读取，推进时钟让它发生。
func loaded(t *testing.T, page services.ImportPage, mutate ...func(*rig)) *rig {
	t.Helper()
	r := newRig(t, page, mutate...)
	r.debounce()
	return r
}

func (r *rig) frames(n int) {
	r.t.Helper()
	for i := 0; i < n; i++ {
		if err := r.session.Frame(); err != nil {
			r.t.Fatalf("出帧失败：%v", err)
		}
	}
}

// debounce 让搜索词的防抖到点（读取随之发生，后台动作是同步的），再出几帧让画面跟上。
func (r *rig) debounce() {
	r.t.Helper()
	r.clock.Advance(fetchDebounce)
	r.frames(3)
}

func (r *rig) node(role gfx.Role, name string) (app.AccessNode, bool) {
	return r.session.AccessByRole(role, name)
}

func (r *rig) has(role gfx.Role, name string) bool {
	_, ok := r.node(role, name)
	return ok
}

// count 是角色与名称都匹配的节点个数。
func (r *rig) count(role gfx.Role, name string) int {
	n := 0
	for _, node := range r.session.Access() {
		if node.Role == role && node.Label == name {
			n++
		}
	}
	return n
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

// hasTextLike 报告画面上是否有任何一行文字包含 sub。
func (r *rig) hasTextLike(sub string) bool { return r.session.HasText(sub) }

func (r *rig) clickAt(x, y float32) {
	r.t.Helper()
	if err := r.session.Click(x, y); err != nil {
		r.t.Fatalf("点击 (%.0f, %.0f) 失败：%v", x, y, err)
	}
	r.frames(3)
}

// click 点击角色加名称对应控件的中心。
func (r *rig) click(role gfx.Role, name string) {
	r.t.Helper()
	n, ok := r.node(role, name)
	if !ok {
		r.t.Fatalf("找不到 %v「%s」", role, name)
	}
	r.clickAt(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
}

// clickNth 点击第 n 个（从 0 起）角色加名称都匹配的控件。
func (r *rig) clickNth(role gfx.Role, name string, n int) {
	r.t.Helper()
	seen := 0
	for _, node := range r.session.Access() {
		if node.Role == role && node.Label == name {
			if seen == n {
				r.clickAt(node.Bounds.X+node.Bounds.Width/2, node.Bounds.Y+node.Bounds.Height/2)
				return
			}
			seen++
		}
	}
	r.t.Fatalf("找不到第 %d 个 %v「%s」（只有 %d 个）", n, role, name, seen)
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

// scrollBy 在 (x, y) 处滚动 dy 个逻辑像素（正值向下，模拟触控板的精确滚动），再出几帧。
// 内容超出的部分在无障碍树里不可见（被裁剪），要点到它得先滚过去。
func (r *rig) scrollBy(x, y, dy float32) {
	r.t.Helper()
	if err := r.session.Scroll(gfx.ScrollEvent{X: x, Y: y, DeltaY: dy, Precise: true}); err != nil {
		r.t.Fatalf("滚动失败：%v", err)
	}
	r.frames(3)
}

// calls 是服务收到的变更类调用（"retry:t1"、"pause"、"clear:all" 等）；读队列状态的 "state" 是每次刷新都会发生的后台读取，不算。
func (r *rig) calls() []string {
	var out []string
	for _, call := range r.svc.Calls() {
		if call != "state" {
			out = append(out, call)
		}
	}
	return out
}

// countText 是画面上恰好等于 text 的文字行数（侧栏与行里可能各有一处同名文字）。
func (r *rig) countText(text string) int {
	n := 0
	for _, got := range r.session.Texts() {
		if got == text {
			n++
		}
	}
	return n
}

// allText 是画面上全部文字按绘制顺序连起来的结果：折行的长句用它整句比对。
func (r *rig) allText() string { return strings.Join(r.session.Texts(), "") }

// textCenter 是第 n 个（从 0 起，按绘制顺序）内容恰好等于 text 的文字的中心。
func (r *rig) textCenter(text string, n int) (x, y float32, ok bool) {
	seen := 0
	for _, got := range r.session.Snapshot().Texts {
		if got.Value != text {
			continue
		}
		if seen == n {
			return got.Bounds.X + got.Bounds.Width/2, got.Bounds.Y + got.Bounds.Height/2, true
		}
		seen++
	}
	return 0, 0, false
}

// hoverText 把指针移到第 n 个等于 text 的文字上，再出几帧让悬停落定。
func (r *rig) hoverText(text string, n int) {
	r.t.Helper()
	x, y, ok := r.textCenter(text, n)
	if !ok {
		r.t.Fatalf("画面上没有第 %d 个文字 %q；画面上有 %q", n, text, r.session.Texts())
	}
	if err := r.session.Move(x, y); err != nil {
		r.t.Fatalf("移动指针失败：%v", err)
	}
	r.frames(3)
}

// clickText 点击第 n 个等于 text 的文字的中心。
func (r *rig) clickText(text string, n int) {
	r.t.Helper()
	x, y, ok := r.textCenter(text, n)
	if !ok {
		r.t.Fatalf("画面上没有第 %d 个文字 %q；画面上有 %q", n, text, r.session.Texts())
	}
	r.clickAt(x, y)
}

// toasts 是此刻提示条里全部消息（含详情之外的标题）；用画面上的文字判断更接近用户所见，这里只给「有没有」的快速查询。
func (r *rig) toastShown(message string) bool { return r.hasText(message) }

// ---- 夹具 ----

// mkTask 造一个与渲染层 makeTask 同样的默认任务（B 站视频、转写阶段、10 分钟前入队），再用 patch 改。
func mkTask(id string, patch func(*services.ImportTask)) services.ImportTask {
	t := services.ImportTask{
		ID: id, SourceKind: "url", SourceInput: "https://www.bilibili.com/video/BV1xx",
		DisplayName: "手写一个混合检索 RAG 系统", Status: services.ImportStatusProcessing, Stage: "transcribing",
		ItemType: "video", CaptureStrategy: "standard", CreatedAt: testNow - 600_000, UpdatedAt: testNow - 10_000,
	}
	if patch != nil {
		patch(&t)
	}
	return t
}

// pageOf 把任务排成服务端返回的页：条目按给定顺序，进行中的另列，计数与降级数按状态算。
func pageOf(tasks ...services.ImportTask) services.ImportPage {
	page := services.ImportPage{Entries: tasks, Total: int64(len(tasks)), Counts: map[string]int64{}}
	for _, t := range tasks {
		page.Counts[t.Status]++
		if IsActive(t) {
			page.Active = append(page.Active, t)
		}
		if t.Status == services.ImportStatusCompleted && t.Warning != "" {
			page.DegradedCount++
		}
	}
	return page
}
