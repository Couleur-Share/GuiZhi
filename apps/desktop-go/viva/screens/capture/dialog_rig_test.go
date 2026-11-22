package capture

import (
	"context"
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件是快速采集对话框的无头测试台：不开真窗口、不碰 WebView2，毫秒级。
// 后台动作（读合集与标签、选文件、入队）同步执行，所以每一步之后的画面是确定的。

var rigFontOnce sync.Once

// recLog 记下写进诊断日志的错误（Guard 的日志通道）。
type recLog struct {
	mu   sync.Mutex
	logs []string
}

func (l *recLog) AppError(scope, action, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, scope+"|"+action+"|"+message)
}

func (l *recLog) entries() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.logs...)
}

// rig 是无头会话里的快速采集对话框。
type rig struct {
	t        *testing.T
	session  *headless.Session
	imports  *servicestest.Imports
	library  *servicestest.Library
	dialog   *Dialog
	notifier *kit.Notifier
	look     *kit.Look
	log      *recLog

	enqueued   int      // Hooks.OnEnqueued 的调用次数
	blankNotes []string // Hooks.BlankNote 收到的合集 ID，按顺序
}

// 夹具：三个合集、几个标签（含大小写与子串关系，用来验证建议的过滤）。
var (
	rigCollections = []services.LibraryCollection{{ID: "c1", Name: "工作"}, {ID: "c2", Name: "灵感"}}
	rigTags        = []services.LibraryTag{
		{ID: "t1", Name: "golang"}, {ID: "t2", Name: "Go语言"}, {ID: "t3", Name: "rust"}, {ID: "t4", Name: "GoLand"},
		{ID: "t5", Name: "gopher"}, {ID: "t6", Name: "gorm"}, {ID: "t7", Name: "gofmt"}, {ID: "t8", Name: "gin"},
	}
)

// newRig 在 1280×800、缩放 1 的无头会话里搭好对话框（还没打开）；mutate 在首帧之前调整。
func newRig(t *testing.T, mutate ...func(*rig)) *rig {
	t.Helper()
	rigFontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	r := &rig{t: t, imports: &servicestest.Imports{}, library: &servicestest.Library{}, log: &recLog{}}
	r.library.Seed(services.LibraryCounts{}, rigCollections, rigTags)
	r.notifier = kit.NewNotifier(nil)
	r.notifier.Clipboard = func(string) error { return nil } // 测试不碰系统剪贴板

	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff // 动效直接到终态，断言不受动画时钟影响
	r.look = kit.NewLook(spec)
	r.look.Scale = 1
	r.look.Viewport = 1280

	guard := kit.Guard{Notify: r.notifier, Log: r.log}
	r.dialog = &Dialog{
		Look: r.look, Ctx: context.Background(), Guard: guard, Imports: r.imports, Library: r.library,
		Hooks: Hooks{
			OnEnqueued: func() { r.enqueued++ },
			BlankNote:  func(collectionID string) { r.blankNotes = append(r.blankNotes, collectionID) },
		},
		Spawn: func(fn func()) { fn() },
	}
	for _, m := range mutate {
		m(r)
	}

	bg := r.look.Col(r.look.T.Background)
	pg := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			ui.Column(c).Fill().Draw(func(d *ui.DrawContext) {
				r.look.PaintSurface(d, kit.Surface{Bg: r.look.T.Background})
			})
			r.dialog.View(c)
			r.look.Toasts(c, r.notifier)
		},
		Theme: &r.look.Theme, Background: &bg,
	})
	var canvas gfx.Canvas
	config := pg.Config(app.Config{Title: t.Name(), Width: 1280, Height: 800})
	config.OnFileDrop = r.dialog.FileDrop // 与 Root 的接线一致：系统文件拖放交给对话框
	session, err := headless.Open(config, &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	r.session = session
	r.frames(3)
	return r
}

// opened 是打开了对话框的测试台。
func opened(t *testing.T, mutate ...func(*rig)) *rig {
	t.Helper()
	r := newRig(t, mutate...)
	r.dialog.Open()
	r.frames(4)
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

// node 找角色加名称对应的控件：先查无障碍树，查不到再查命中区域。
// 模态框之上的浮层（下拉面板、标签建议）不在无障碍树的模态范围内，但命中区域里有它们，点得到。
func (r *rig) node(role gfx.Role, name string) (app.AccessNode, bool) {
	if n, ok := r.session.AccessByRole(role, name); ok {
		return n, true
	}
	hits := r.session.Snapshot().Hits
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		if h.Role == role && h.Label == name && !h.Block && !h.ID.IsZero() {
			return app.AccessNode{ID: h.ID, Role: h.Role, Bounds: h.Bounds, Label: h.Label}, true
		}
	}
	return app.AccessNode{}, false
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

func (r *rig) hasText(text string) bool {
	for _, got := range r.session.Texts() {
		if got == text {
			return true
		}
	}
	return false
}

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

// draftBox 是草稿框（无障碍名是占位文字）。
func (r *rig) draftBox() app.AccessNode {
	r.t.Helper()
	n, ok := r.node(gfx.RoleTextField, locales.CapturePlaceholder())
	if !ok {
		r.t.Fatalf("找不到草稿框；画面上有 %q", r.session.Texts())
	}
	return n
}

// tagInput 是标签输入框。
func (r *rig) tagInput() app.AccessNode {
	r.t.Helper()
	n, ok := r.node(gfx.RoleTextField, locales.CaptureTagsPlaceholder())
	if !ok {
		r.t.Fatalf("找不到标签输入框；画面上有 %q", r.session.Texts())
	}
	return n
}

// draftWith 点一下草稿框并输入 text。
func (r *rig) draftWith(text string) {
	r.t.Helper()
	box := r.draftBox()
	r.clickAt(box.Bounds.X+box.Bounds.Width/2, box.Bounds.Y+20)
	r.typeText(text)
}

func (r *rig) focusTags() {
	r.t.Helper()
	box := r.tagInput()
	r.clickAt(box.Bounds.X+box.Bounds.Width/2, box.Bounds.Y+box.Bounds.Height/2)
}

// submitButton 是「采集」按钮。
func (r *rig) submitButton() app.AccessNode {
	r.t.Helper()
	n, ok := r.node(gfx.RoleButton, locales.CaptureSubmit())
	if !ok {
		r.t.Fatalf("找不到「采集」按钮；画面上有 %q", r.session.Texts())
	}
	return n
}

// dialogOpen 报告画面上是否有对话框的标题。
func (r *rig) dialogOpen() bool { return r.has(gfx.RoleHeading, locales.CaptureTitle()) }

// chips 是画面上的标签小块（以移除钮的名字计）。
func (r *rig) chipRemoved(name string) bool {
	return !r.has(gfx.RoleButton, locales.LibraryRemoveTag(name))
}

// optionNames 是已有标签建议里各项的名字，按绘制顺序；浮层里只有标签选项（合集下拉关着时没有别的 option）。
func (r *rig) optionNames() []string {
	var out []string
	for _, h := range r.session.Snapshot().Hits {
		if h.Role == gfx.RoleOption && !h.Block {
			out = append(out, h.Label)
		}
	}
	return out
}
