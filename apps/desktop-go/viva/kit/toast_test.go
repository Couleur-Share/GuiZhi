package kit

import (
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/locales"
)

// 提示与失败通道的行为测试（L4）。时长、上限与退场卸载都按渲染层 Toast.tsx 的常量断言。

func openToasts(t *testing.T, spec Spec, n *Notifier) *harness {
	t.Helper()
	return openPage(t, spec, 1280, 800, func(l *Look, c ui.C) { l.Toasts(c, n) })
}

// toastMessages 是窗口线程上现有提示的文字（出现顺序，含正在退场的）。
func toastMessages(n *Notifier) []string {
	out := make([]string, 0, len(n.toasts))
	for _, t := range n.toasts {
		out = append(out, t.message)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 成功/提示 3 秒、带撤销 8 秒后自己消失；错误与警告留到手动关闭。消失 = 开始退场，140 ms 后卸载。
func TestToastAutoDismissTimes(t *testing.T) {
	n := NewNotifier(nil)
	n.Success("成功")
	n.Warning("警告")
	n.Undo("已删除", locales.CommonUndo(), func() {})
	n.Error("失败", "")
	h := openToasts(t, still(), n)
	if n.Len() != 4 {
		t.Fatalf("应有 4 条提示，实际 %d", n.Len())
	}
	steps := []struct {
		advance time.Duration
		want    []string
		note    string
	}{
		{2900 * time.Millisecond, []string{"成功", "警告", "已删除", "失败"}, "2.9 s：都还在"},
		{200 * time.Millisecond, []string{"成功", "警告", "已删除", "失败"}, "3.1 s：成功已开始退场，退场 140 ms 内仍在版面里"},
		{200 * time.Millisecond, []string{"警告", "已删除", "失败"}, "3.3 s：成功已卸载"},
		{4600 * time.Millisecond, []string{"警告", "已删除", "失败"}, "7.9 s：撤销提示还在（8 s）"},
		{200 * time.Millisecond, []string{"警告", "已删除", "失败"}, "8.1 s：撤销提示开始退场"},
		{200 * time.Millisecond, []string{"警告", "失败"}, "8.3 s：撤销提示已卸载"},
		{10 * time.Minute, []string{"警告", "失败"}, "10 分钟后：错误与警告不自动消失"},
	}
	for _, s := range steps {
		advance(h, s.advance)
		if got := toastMessages(n); !sameStrings(got, s.want) {
			t.Fatalf("%s：期望 %v，实际 %v", s.note, s.want, got)
		}
	}
}

// 总数超过 5 条时的挤法（渲染层 trimToasts）：先挤最老的「会自己消失」的那条，都不会消失才挤最老的。
func TestToastTrimPrefersDisappearingOnes(t *testing.T) {
	cases := []struct {
		name  string
		show  func(n *Notifier)
		wants []string
	}{
		{"会消失的先走", func(n *Notifier) {
			n.Error("e1", "")
			n.Error("e2", "")
			n.Error("e3", "")
			n.Success("s1")
			n.Success("s2")
			n.Info("i1")
		}, []string{"e1", "e2", "e3", "s2", "i1"}},
		{"全是错误时挤最老的", func(n *Notifier) {
			for _, m := range []string{"e1", "e2", "e3", "e4", "e5", "e6"} {
				n.Error(m, "")
			}
		}, []string{"e2", "e3", "e4", "e5", "e6"}},
		{"新来的是唯一会消失的，它自己被挤掉", func(n *Notifier) {
			for _, m := range []string{"e1", "e2", "e3", "e4", "e5"} {
				n.Error(m, "")
			}
			n.Success("s1")
		}, []string{"e1", "e2", "e3", "e4", "e5"}},
	}
	for _, c := range cases {
		n := NewNotifier(nil)
		h := openToasts(t, still(), n)
		c.show(n)
		h.frames(2)
		if got := toastMessages(n); !sameStrings(got, c.wants) {
			t.Errorf("%s：期望 %v，实际 %v", c.name, c.wants, got)
		}
	}
}

// 任意 goroutine 都能 Show：入队、请求重绘，下一帧由窗口线程领走。
func TestNotifierAcceptsToastsFromAnyGoroutine(t *testing.T) {
	var mu sync.Mutex
	wakes := 0
	n := NewNotifier(func() { mu.Lock(); wakes++; mu.Unlock() })
	h := openToasts(t, still(), n)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Warning("来自后台")
		}()
	}
	wg.Wait()
	mu.Lock()
	got := wakes
	mu.Unlock()
	if got != 20 {
		t.Errorf("每次 Show 都应请求重绘，实际 %d 次", got)
	}
	h.frames(2)
	if n.Len() != toastMaxVisible {
		t.Errorf("20 条警告压到上限 %d 条，实际 %d", toastMaxVisible, n.Len())
	}
}

// 动作按钮：点击后执行回调并收起这条提示，其余提示不受影响。
func TestToastActionRunsAndCloses(t *testing.T) {
	n := NewNotifier(nil)
	calls := 0
	n.Undo("已从列表删除（3 项）", locales.CommonUndo(), func() { calls++ })
	n.Error("另一个错误", "")
	h := openToasts(t, still(), n)
	if !h.has(gfx.RoleButton, locales.CommonUndo()) {
		t.Fatal("带动作的提示应有「撤销」按钮")
	}
	h.click(locales.CommonUndo())
	if calls != 1 {
		t.Errorf("回调应执行 1 次，实际 %d", calls)
	}
	advance(h, 200*time.Millisecond)
	if got := toastMessages(n); !sameStrings(got, []string{"另一个错误"}) {
		t.Errorf("点击动作后这条提示应卸载、另一条保留，实际 %v", got)
	}
}

// 关闭按钮（名称「关闭」）收起提示；Dismiss 按 ID 收起，ID 不存在时什么都不做。
func TestToastCloseAndDismiss(t *testing.T) {
	n := NewNotifier(nil)
	n.Error("会被关闭按钮收起", "")
	h := openToasts(t, still(), n)
	h.click(locales.CommonClose())
	advance(h, 200*time.Millisecond)
	if n.Len() != 0 {
		t.Fatalf("点关闭后应卸载，实际还有 %d 条", n.Len())
	}

	keep := n.Error("保留", "")
	gone := n.Error("被撤下", "")
	h.frames(2)
	n.Dismiss(gone)
	n.Dismiss("gz-toast-不存在")
	h.frames(1) // 队列里的操作在这一帧领走，退场从这一帧开始计时
	advance(h, 200*time.Millisecond)
	if got := toastMessages(n); !sameStrings(got, []string{"保留"}) {
		t.Errorf("Dismiss 只撤下指定的那条，实际 %v（保留的 ID %s）", got, keep)
	}
}

const sqliteDetail = "SQLITE_BUSY: database is locked\n  at updateItems (items.go:214)\nfailed to update 12 items: C:/Users/someone/AppData/Roaming/GuiZhi/data/knowledge.db: database is locked (5)"

// 报错详情：「查看详情」展开原文，复制拿到的是未折行的完整原文；「已复制」2 秒后恢复，
// 每次点击各起一个定时器，先到点的先把它收掉（渲染层 ToastDetail 的 setTimeout）。
func TestToastDetailExpandAndCopy(t *testing.T) {
	n := NewNotifier(nil)
	var copied []string
	n.Clipboard = func(s string) error { copied = append(copied, s); return nil }
	n.Error("批量更新失败", sqliteDetail)
	h := openToasts(t, still(), n)

	if !h.has(gfx.RoleButton, locales.CommonViewDetail()) {
		t.Fatal("有详情的提示应有「查看详情」")
	}
	if h.has(gfx.RoleButton, locales.CommonCopy()) {
		t.Fatal("折叠时不该有「复制」按钮")
	}
	h.click(locales.CommonViewDetail())
	if node, ok := h.node(gfx.RoleButton, locales.CommonCollapse()); !ok || !node.Expanded {
		t.Fatalf("展开后按钮应改叫「收起」且为展开态：%+v ok=%v", node, ok)
	}
	if !snapshotHasText(h, "SQLITE_BUSY") {
		t.Error("展开后应看到详情原文")
	}

	h.click(locales.CommonCopy())
	if len(copied) != 1 || copied[0] != sqliteDetail {
		t.Fatalf("复制应拿到完整原文（不含折行），实际 %q", copied)
	}
	if !h.has(gfx.RoleButton, locales.ToastCopied()) {
		t.Error("点击后按钮应显示「已复制」")
	}
	advance(h, 1500*time.Millisecond)
	h.click(locales.ToastCopied()) // 1.5 s 时再点一次：第二个定时器 3.5 s 到点
	advance(h, 600*time.Millisecond)
	if !h.has(gfx.RoleButton, locales.CommonCopy()) {
		t.Error("2.1 s：第一个定时器已到点，先到点的把「已复制」收掉，按钮应恢复为「复制」")
	}
	if len(copied) != 2 {
		t.Errorf("两次点击应复制两次，实际 %d", len(copied))
	}
	advance(h, 1500*time.Millisecond) // 3.6 s：第二个定时器（3.5 s）也已到点
	if h.has(gfx.RoleButton, locales.ToastCopied()) {
		t.Error("3.6 s：两个定时器都到点后不应再显示「已复制」")
	}
	h.click(locales.CommonCollapse())
	if !h.has(gfx.RoleButton, locales.CommonViewDetail()) {
		t.Error("点「收起」后应回到折叠态（按钮改叫「查看详情」）")
	}
}

// 剪贴板写不进去也没关系：原文就摊在上面，按钮保持「复制」，不报成已复制。
func TestToastCopyFailureKeepsLabel(t *testing.T) {
	n := NewNotifier(nil)
	n.Clipboard = func(string) error { return errors.New("剪贴板被占用") }
	n.Error("失败", sqliteDetail)
	h := openToasts(t, still(), n)
	h.click(locales.CommonViewDetail())
	h.click(locales.CommonCopy())
	if h.has(gfx.RoleButton, locales.ToastCopied()) {
		t.Error("复制失败不能显示「已复制」")
	}
	if !h.has(gfx.RoleButton, locales.CommonCopy()) {
		t.Error("复制失败后按钮应仍是「复制」，用户可以重试")
	}
}

func snapshotHasText(h *harness, part string) bool {
	for _, tx := range h.session.Snapshot().Texts {
		if strings.Contains(tx.Value, part) {
			return true
		}
	}
	return false
}

// 入场：右侧滑入 + 淡入，180 ms，曲线 cubic-bezier(0, 0, 0.2, 1)（animate-in slide-in-from-right-10 fade-in duration-base ease-enter）。
func TestToastEnterMotion(t *testing.T) {
	spec := DefaultSpec()
	spec.Motion = MotionLevelStandard
	n := NewNotifier(nil)
	n.Success("设置已保存")
	h := openToasts(t, spec, n)
	type sample struct {
		at    time.Duration
		alpha float32
		x     float32
	}
	var samples []sample
	elapsed := time.Duration(0)
	for _, step := range []time.Duration{0, 30, 30, 30, 30, 30, 30, 40} {
		if step > 0 {
			advance(h, step*time.Millisecond)
			elapsed += step * time.Millisecond
		}
		alpha, ok := textAlpha(h, "设置已保存")
		if !ok {
			t.Fatalf("%v：入场期间提示应当已经在画", elapsed)
		}
		samples = append(samples, sample{elapsed, alpha, textX(h, "设置已保存")})
	}
	finalX := samples[len(samples)-1].x
	for _, s := range samples {
		e := easeEnter(math.Min(float64(s.at)/float64(180*time.Millisecond), 1))
		if math.Abs(float64(s.alpha)-e) > 0.03 {
			t.Errorf("%v：不透明度 %.3f，期望 %.3f（180 ms，ease-enter）", s.at, s.alpha, e)
		}
		// 滑入的偏移是 10 × 0.25rem = 40px（根字号 16px），与淡入同一条曲线
		if want := float64(finalX) + 40*(1-e); math.Abs(float64(s.x)-want) > 1 {
			t.Errorf("%v：文字左缘 %.2f，期望 %.2f（从右侧 40px 滑入）", s.at, s.x, want)
		}
	}
}

// textX 是当前帧里文字 value 的左边缘；没有这行文字时返回 0。
func textX(h *harness, value string) float32 {
	for _, tx := range h.session.Snapshot().Texts {
		if tx.Value == value {
			return tx.Bounds.X
		}
	}
	return 0
}

// 退场：向右滑出 + 淡出 120 ms（ease-exit），再过 20 ms 卸载；动画播完到卸载之间卡片以原样回到画面里一下
// （tailwindcss-animate 不设 fill-mode，渲染层的真实行为）。
func TestToastLeaveMotion(t *testing.T) {
	spec := DefaultSpec()
	spec.Motion = MotionLevelStandard
	n := NewNotifier(nil)
	id := n.Error("退场", "")
	h := openToasts(t, spec, n)
	advance(h, 300*time.Millisecond) // 入场走完
	if a, ok := textAlpha(h, "退场"); !ok || a < 0.999 {
		t.Fatalf("入场走完后应完整显示：ok=%v alpha=%.3f", ok, a)
	}
	n.Dismiss(id)
	h.frames(1) // 退场从这一帧开始计时
	elapsed := time.Duration(0)
	for _, step := range []time.Duration{30, 30, 30} {
		advance(h, step*time.Millisecond)
		elapsed += step * time.Millisecond
		want := 1 - cubicBezierY(0.4, 0, 1, 1, float64(elapsed)/float64(120*time.Millisecond))
		alpha, ok := textAlpha(h, "退场")
		if !ok {
			t.Fatalf("退场 %v：回放期间文字应还在画", elapsed)
		}
		if math.Abs(float64(alpha)-want) > 0.05 {
			t.Errorf("退场 %v：不透明度 %.3f，期望 %.3f（120 ms，ease-exit）", elapsed, alpha, want)
		}
	}
	advance(h, 40*time.Millisecond) // 130 ms：回放结束、尚未卸载
	if a, ok := textAlpha(h, "退场"); !ok || a < 0.999 {
		t.Errorf("130 ms：动画播完到卸载之间卡片应以原样回到画面里：ok=%v alpha=%.3f", ok, a)
	}
	advance(h, 40*time.Millisecond) // 170 ms
	if n.Len() != 0 {
		t.Errorf("退场 140 ms 后应卸载，实际还有 %d 条", n.Len())
	}
	if _, ok := textAlpha(h, "退场"); ok {
		t.Error("卸载后不该再画")
	}
}

// 记录日志的替身。
type recordLog struct {
	scope, action, message []string
}

func (r *recordLog) AppError(scope, action, message string) {
	r.scope = append(r.scope, scope)
	r.action = append(r.action, action)
	r.message = append(r.message, message)
}

// Guard.Run：成功只返回 true；失败记日志、弹带原因的错误提示、返回 false；fn 里的 panic 同样兜住。
func TestGuardRun(t *testing.T) {
	n := NewNotifier(nil)
	log := &recordLog{}
	g := Guard{Notify: n, Log: log}

	if !g.Run("保存设置", func() error { return nil }) {
		t.Error("成功应返回 true")
	}
	n.advance(0)
	if n.Len() != 0 || len(log.action) != 0 {
		t.Fatalf("成功不该有提示或日志：提示 %d 条，日志 %d 条", n.Len(), len(log.action))
	}

	if g.Run("批量更新", func() error { return errors.New("database is locked") }) {
		t.Error("失败应返回 false")
	}
	n.advance(0)
	if n.Len() != 1 {
		t.Fatalf("失败应弹出 1 条错误提示，实际 %d", n.Len())
	}
	got := n.toasts[0]
	if got.kind != ToastError || got.message != locales.CommonOperationFailed("批量更新") || got.detail != "database is locked" {
		t.Errorf("错误提示不对：%+v", got)
	}
	if len(log.action) != 1 || log.scope[0] != logScope || log.action[0] != "批量更新" || log.message[0] != "database is locked" {
		t.Errorf("日志不对：%+v", log)
	}

	if g.Run("导入", func() error { panic("boom") }) {
		t.Error("panic 应当被兜住并返回 false")
	}
	n.advance(0)
	if last := n.toasts[len(n.toasts)-1]; !strings.Contains(last.detail, "panic: boom") {
		t.Errorf("panic 的原因应进入详情：%q", last.detail)
	}
}

// Guard.Go 在后台执行并回调结果；Background 只记日志不弹提示；没有 Notify 或 Log 时各自退化而不崩。
func TestGuardGoAndBackground(t *testing.T) {
	n := NewNotifier(nil)
	log := &recordLog{}
	g := Guard{Notify: n, Log: log}

	done := make(chan bool, 1)
	g.Go("后台保存", func() error { return errors.New("磁盘已满") }, func(ok bool) { done <- ok })
	select {
	case ok := <-done:
		if ok {
			t.Error("失败的后台操作应回调 false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("后台操作没有回调")
	}
	n.advance(0)
	if n.Len() != 1 {
		t.Errorf("后台失败也要弹提示，实际 %d 条", n.Len())
	}

	g.Background("定时刷新", errors.New("查询超时"))
	g.Background("定时刷新", nil)
	n.advance(0)
	if n.Len() != 1 {
		t.Errorf("Background 不弹提示，实际 %d 条", n.Len())
	}
	if len(log.action) != 2 || log.action[1] != "定时刷新" {
		t.Errorf("Background 的非空错误应记一条日志，nil 不记：%+v", log)
	}

	var bare Guard
	if bare.Run("无通道", func() error { return errors.New("x") }) {
		t.Error("没有 Notify 与 Log 也应返回 false")
	}
	bare.Background("无通道", errors.New("x"))
}

var _ ui.C // 保持 ui 导入：openToasts 的视图签名

// Queued 给没有窗口的测试读排队中的提示：按入队顺序给出类型、标题与折叠的详情；关闭操作不算；窗口领走之后就不在队里了。
func TestNotifierQueuedReportsPendingToasts(t *testing.T) {
	n := NewNotifier(nil)
	if got := n.Queued(); len(got) != 0 {
		t.Fatalf("新通道应为空：%+v", got)
	}
	n.Success("已保存")
	id := n.Error("保存失败", "磁盘满")
	n.Warning("提醒", WithDetail("细节"))
	n.Dismiss(id)
	want := []QueuedToast{
		{Kind: ToastSuccess, Message: "已保存"},
		{Kind: ToastError, Message: "保存失败", Detail: "磁盘满"},
		{Kind: ToastWarning, Message: "提醒", Detail: "细节"},
	}
	got := n.Queued()
	if len(got) != len(want) {
		t.Fatalf("应有 %d 条（关闭不算）：%+v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条：%+v，期望 %+v", i, got[i], want[i])
		}
	}
	h := openToasts(t, still(), n)
	h.frames(2)
	if got := n.Queued(); len(got) != 0 {
		t.Errorf("窗口领走之后队列应空：%+v", got)
	}
}
