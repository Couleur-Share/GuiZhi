package app

import (
	"errors"
	"slices"
	"sync"
	"testing"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/screens/shell"
)

// fakeShell 记下外壳收到的调用：关闭请求、关闭对话框的结果。
type fakeShell struct {
	mu            sync.Mutex
	handle        bool // OnCloseRequest 的返回值
	closeRequests int
	results       []closeResult
	cancels       int
}

type closeResult struct {
	action   string
	remember bool
}

func (s *fakeShell) Attach(*Host) {}

func (s *fakeShell) OnCloseRequest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeRequests++
	return s.handle
}

func (s *fakeShell) CloseDialogResult(action string, remember bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results = append(s.results, closeResult{action, remember})
}

func (s *fakeShell) CloseDialogCancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancels++
}

func (s *fakeShell) got() ([]closeResult, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.results), s.cancels
}

const closeDialogTitle = "关闭应用"

func (f *rootFixture) dialogOpen() bool { return f.session.HasText(closeDialogTitle) }

// clickRemember 点「记住我的选择」右边的文字：翻转，不取键盘焦点。
func (f *rootFixture) clickRemember(t *testing.T) {
	t.Helper()
	n, ok := f.session.AccessByRole(gfx.RoleCheckbox, locales.CloseDialogRememberChoice())
	if !ok {
		t.Fatal("没有找到复选框「记住我的选择」")
	}
	if err := f.session.Click(n.Bounds.X+n.Bounds.Width+16, n.Bounds.Y+n.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
}

// 外壳要求弹出关闭对话框（window:showCloseDialog）：下一帧出现；选「最小化到托盘」把结果交给外壳，不写设置（没勾记住）。
func TestRootCloseDialogRoutesMinimize(t *testing.T) {
	sh := &fakeShell{}
	f := openRoot(t, func(o *Options) { o.Shell = sh })
	if f.dialogOpen() {
		t.Fatal("前提不成立：一开始不应显示关闭对话框")
	}
	f.root.showCloseDialog() // 任意 goroutine 都可以
	f.frames(t, 4)
	if !f.dialogOpen() {
		t.Fatal("外壳要求之后应显示关闭对话框")
	}
	if err := f.session.ClickLabel(locales.CloseDialogMinimizeToTray()); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	results, cancels := sh.got()
	if len(results) != 1 || results[0] != (closeResult{"minimize", false}) || cancels != 0 {
		t.Errorf("外壳收到 %+v（取消 %d 次），期望一次 minimize、不记住", results, cancels)
	}
	if f.dialogOpen() {
		t.Error("选择之后对话框应当关上")
	}
	if got := f.fake.Values().UI.CloseAction; got != settings.CloseAsk || f.fake.Updates() != 0 {
		t.Errorf("没勾记住时不应写设置：CloseAction=%q，写入 %d 次", got, f.fake.Updates())
	}
}

// 勾上「记住我的选择」再选「退出应用」：先把关闭行为写进设置（渲染层 setCloseAction），再交外壳（remember=true）。
func TestRootCloseDialogRememberWritesSettings(t *testing.T) {
	sh := &fakeShell{}
	f := openRoot(t, func(o *Options) { o.Shell = sh })
	f.root.showCloseDialog()
	f.frames(t, 4)
	f.clickRemember(t)
	if err := f.session.ClickLabel(locales.CloseDialogExitApp()); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	results, _ := sh.got()
	if len(results) != 1 || results[0] != (closeResult{"exit", true}) {
		t.Errorf("外壳收到 %+v，期望一次 exit 且记住", results)
	}
	if got := f.fake.Values().UI.CloseAction; got != settings.CloseExit {
		t.Errorf("设置里的关闭行为 = %q，期望 exit", got)
	}
	if f.fake.Updates() != 1 {
		t.Errorf("写设置 %d 次，期望 1", f.fake.Updates())
	}
}

// 记住选择时写设置失败（AGENTS.md：失败要可理解）：弹错误提示并记日志；这次操作仍交给外壳（外壳在内存里记住了本次选择）。
func TestRootCloseDialogRememberWriteFailureStillProceeds(t *testing.T) {
	sh := &fakeShell{}
	log := &servicestest.Logger{}
	f := openRoot(t, func(o *Options) { o.Shell, o.Services.Log = sh, log })
	f.root.showCloseDialog()
	f.frames(t, 4)
	f.clickRemember(t)
	f.fake.FailWith(errors.New("磁盘已满"))
	if err := f.session.ClickLabel(locales.CloseDialogMinimizeToTray()); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	results, _ := sh.got()
	if len(results) != 1 || results[0] != (closeResult{"minimize", true}) {
		t.Errorf("写入失败时仍应把结果交给外壳：%+v", results)
	}
	if f.root.Notifier().Len() != 1 {
		t.Errorf("写入失败应弹出 1 条错误提示，实际 %d 条", f.root.Notifier().Len())
	}
	entries := log.Entries()
	if len(entries) != 1 || entries[0].Action != locales.NativeSettingsSaveAction() || entries[0].Message != "磁盘已满" {
		t.Errorf("日志不对：%+v", entries)
	}
}

// 只关对话框（Esc、遮罩、关闭钮）：通知外壳复位等待标记，否则下次点关闭不会再弹。
func TestRootCloseDialogCancelResetsShell(t *testing.T) {
	sh := &fakeShell{}
	f := openRoot(t, func(o *Options) { o.Shell = sh })
	f.root.showCloseDialog()
	f.frames(t, 4)
	if err := f.session.Keys("Escape"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	results, cancels := sh.got()
	if len(results) != 0 || cancels != 1 {
		t.Errorf("外壳收到结果 %+v、取消 %d 次，期望只有 1 次取消", results, cancels)
	}
	if f.dialogOpen() {
		t.Error("Esc 之后对话框应当关上")
	}
	// 之后外壳可以再次要求弹出，且复选框是复位的。
	f.root.showCloseDialog()
	f.frames(t, 4)
	if !f.dialogOpen() {
		t.Fatal("再次要求之后应当再次显示")
	}
	if n, ok := f.session.AccessByRole(gfx.RoleCheckbox, locales.CloseDialogRememberChoice()); !ok || n.Checked {
		t.Errorf("再次打开时复选框应复位：%+v 找到=%v", n, ok)
	}
}

// 没有外壳（截图、对照）时，对话框的结果无处可交，也不能崩。
func TestRootCloseDialogWithoutShell(t *testing.T) {
	f := openRoot(t, nil)
	f.root.showCloseDialog()
	f.frames(t, 4)
	if err := f.session.ClickLabel(locales.CloseDialogMinimizeToTray()); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if f.dialogOpen() {
		t.Error("没有外壳时选择之后对话框也应当关上")
	}
}

// runOnUI：任意 goroutine 排队，窗口线程下一帧开头按次序执行，并请求重绘。
func TestRootRunOnUIRunsInOrderOnNextFrame(t *testing.T) {
	f := openRoot(t, nil)
	var order []int
	before := f.redraws.Load()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		f.root.runOnUI(func() { order = append(order, 1) })
		f.root.runOnUI(func() { order = append(order, 2) })
	}()
	wg.Wait()
	if len(order) != 0 {
		t.Fatal("排队的任务不应在调用线程上立刻执行")
	}
	if f.redraws.Load() < before+2 {
		t.Errorf("排队应请求重绘：%d → %d", before, f.redraws.Load())
	}
	f.frames(t, 1)
	if !slices.Equal(order, []int{1, 2}) {
		t.Errorf("执行顺序 %v，期望 [1 2]", order)
	}
	f.frames(t, 2)
	if len(order) != 2 {
		t.Errorf("任务只应执行一次：%v", order)
	}
}

// 界面命令：settings:open 打开设置页；还没迁移的命令留痕（不假装执行了），页面不变。
func TestRootDispatchCommands(t *testing.T) {
	log := &servicestest.Logger{}
	f := openRoot(t, func(o *Options) { o.Page, o.Module, o.Services.Log = shell.PageSettings, shell.ModuleImports, log })

	// 托盘「新建」：渲染层先回到主页再请求采集——页面切换是真的，采集对话框还没迁移，要留痕而不是假装做了。
	f.root.dispatch(CommandNewItem)
	f.frames(t, 2)
	if f.root.Window().Page != shell.PageHome {
		t.Error("item:new 应当先回到主页")
	}
	entries := log.Entries()
	if len(entries) != 1 || entries[0].Action != locales.NativeShellCommandAction() || entries[0].Message != locales.NativeShellCommandNotMigrated("item:new") {
		t.Errorf("日志不对：%+v", entries)
	}

	// 快捷键只发事件、不切页面：停在设置页时 newItem 快捷键不会把人带回主页（渲染层 shortcut:newItem 只是一个事件）。
	f.root.dispatch(CommandOpenSettings)
	f.frames(t, 2)
	if f.root.Window().Page != shell.PageSettings {
		t.Fatal("settings:open 应当打开设置页")
	}
	f.root.dispatch(CommandShortcutNewItem)
	f.frames(t, 2)
	if f.root.Window().Page != shell.PageSettings {
		t.Error("newItem 快捷键不应切换页面")
	}
	entries = log.Entries()
	if len(entries) != 2 || entries[1].Message != locales.NativeShellCommandNotMigrated("shortcut:newItem") {
		t.Errorf("快捷键命令也要留痕：%+v", entries)
	}
}

// fakeHostWindow 与 fakeHostApp 记下 Host 对窗口与应用做的事（带次序，用来核对「先销毁岛、再隐藏」）。
type hostTrace struct{ calls []string }

func (h *hostTrace) add(s string) { h.calls = append(h.calls, s) }

type fakeHostWindow struct {
	trace             *hostTrace
	hidden, minimized bool
}

func (w *fakeHostWindow) Show()           { w.trace.add("show"); w.hidden = false }
func (w *fakeHostWindow) Hide()           { w.trace.add("hide"); w.hidden = true }
func (w *fakeHostWindow) Close()          { w.trace.add("close") }
func (w *fakeHostWindow) Hidden() bool    { return w.hidden }
func (w *fakeHostWindow) Minimized() bool { return w.minimized }

type fakeHostApp struct{ trace *hostTrace }

func (a *fakeHostApp) Quit() { a.trace.add("quit") }
func (a *fakeHostApp) AddStatusItem(vapp.StatusItemConfig) (*vapp.StatusItem, error) {
	a.trace.add("status")
	return nil, nil
}

type traceCloser struct{ trace *hostTrace }

func (c *traceCloser) Close() { c.trace.add("island-closed") }

// 收到托盘：先销毁网页岛（WebView 子进程只在窗口可见时存在），再隐藏窗口。
func TestHostHideClosesIslandsBeforeHiding(t *testing.T) {
	f := openRoot(t, nil)
	trace := &hostTrace{}
	win := &fakeHostWindow{trace: trace}
	host := &Host{app: &fakeHostApp{trace}, win: win, root: f.root}
	f.root.islands.Track(&traceCloser{trace})
	f.root.islands.Track(&traceCloser{trace})

	host.Hide()
	if want := []string{"island-closed", "island-closed", "hide"}; !slices.Equal(trace.calls, want) {
		t.Errorf("调用次序 %v，期望 %v", trace.calls, want)
	}
	if host.Visible() {
		t.Error("隐藏之后 Visible 应为假")
	}
	win.minimized = true
	if !host.Minimized() {
		t.Error("Minimized 应反映窗口状态")
	}
	host.Show()
	if !host.Visible() {
		t.Error("Show 之后 Visible 应为真")
	}
	host.Close()
	host.Quit()
	if tail := trace.calls[len(trace.calls)-3:]; !slices.Equal(tail, []string{"show", "close", "quit"}) {
		t.Errorf("Show/Close/Quit 没有如实转给窗口与应用：%v", tail)
	}
}

// 关根（窗口关闭）时也销毁仍开着的岛，不留 WebView 子进程。
func TestRootCloseClosesIslands(t *testing.T) {
	f := openRoot(t, nil)
	trace := &hostTrace{}
	f.root.islands.Track(&traceCloser{trace})
	f.root.Close()
	if len(trace.calls) != 1 {
		t.Errorf("Close 应销毁登记的岛：%v", trace.calls)
	}
}

// OnCloseRequest：外壳接管了就不关，没接管照常关闭；没有外壳时不设处理函数（关窗口就是关闭）。
func TestCloseGate(t *testing.T) {
	if closeGate(nil) != nil {
		t.Fatal("没有外壳时不应设置关闭处理函数")
	}
	for _, handled := range []bool{true, false} {
		sh := &fakeShell{handle: handled}
		allowed := 0
		closeGate(sh)(func() { allowed++ })
		if sh.closeRequests != 1 {
			t.Errorf("handled=%v：外壳应收到 1 次关闭请求，实际 %d", handled, sh.closeRequests)
		}
		if (allowed == 1) == handled {
			t.Errorf("handled=%v：放行 %d 次——接管了就不该放行，没接管就该放行", handled, allowed)
		}
	}
}
