package kit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/locales"
)

// newToastTester 起一个只有提示视口的窗口。
func newToastTester(t *testing.T, l *Look, n *Notifier) *ui.Tester {
	t.Helper()
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(l.Theme)
		ui.Column(c).Fill().Background(l.T.Background).Children(func() {})
		ToastViewport(c, l, n)
	}, 900, 500)
	tr.Frame()
	return tr
}

func lightLook() *Look { return NewLook(DefaultThemeSpec(), Env{}) }

func darkLook() *Look {
	spec := DefaultThemeSpec()
	spec.Mode = ModeDark
	return NewLook(spec, Env{})
}

func TestNotifierShowsToastFromAnotherGoroutine(t *testing.T) {
	var wakes atomic.Int32
	n := NewNotifier(func() { wakes.Add(1) })
	tr := newToastTester(t, lightLook(), n)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); n.Success("已保存") }()
	wg.Wait()
	if wakes.Load() == 0 {
		t.Fatal("后台 goroutine 入队后必须请求重绘，否则提示要等到下一次无关的重绘才出现")
	}
	tr.Frame()
	if !tr.HasText("已保存") {
		t.Fatal("入队的提示应在下一帧出现")
	}
}

func TestToastLifetimeByKind(t *testing.T) {
	n := NewNotifier(nil)
	tr := newToastTester(t, lightLook(), n)
	// 同时显示的上限是 3 条（MyGo），所以分两批各 ≤3 条
	n.Success("成功提示")
	n.Error("出错提示", "")
	n.Info("普通提示")
	tr.Frame()
	for _, s := range []string{"成功提示", "出错提示", "普通提示"} {
		if !tr.HasText(s) {
			t.Fatalf("%q 应已显示", s)
		}
	}

	// 3 秒后成功/提示淡出；错误留着。每次推进后建帧，让到期的提示关闭
	for i := 0; i < 8; i++ {
		tr.Advance(500 * time.Millisecond)
		tr.Frame()
	}
	if tr.HasText("成功提示") || tr.HasText("普通提示") {
		t.Error("成功与提示类应在约 3 秒后自行消失")
	}
	if !tr.HasText("出错提示") {
		t.Error("错误类不应自动消失：几秒就消失的报错等于没报")
	}

	n.Warning("警告提示")
	tr.Frame()
	for i := 0; i < 120; i++ { // 再过一分钟
		tr.Advance(500 * time.Millisecond)
		tr.Frame()
	}
	if !tr.HasText("出错提示") || !tr.HasText("警告提示") {
		t.Error("错误与警告一分钟后仍应在")
	}
}

func TestToastTimeoutOptions(t *testing.T) {
	n := NewNotifier(nil)
	tr := newToastTester(t, lightLook(), n)
	n.Show(ToastSuccess, "留着的成功", WithTimeout(-1))
	n.Show(ToastError, "限时的错误", WithTimeout(time.Second))
	tr.Frame()
	for i := 0; i < 6; i++ {
		tr.Advance(500 * time.Millisecond)
		tr.Frame()
	}
	if !tr.HasText("留着的成功") {
		t.Error("WithTimeout(-1) 应让成功提示留到手动关闭")
	}
	if tr.HasText("限时的错误") {
		t.Error("WithTimeout(1s) 应让错误提示 1 秒后消失")
	}
}

func TestToastDetailExpandsAndCopies(t *testing.T) {
	n := NewNotifier(nil)
	tr := newToastTester(t, lightLook(), n)
	const detail = "SQLITE_BUSY: database is locked\n  at import.enqueue"
	n.Error("入队失败", detail)
	tr.Frame()

	if !tr.HasText("入队失败") || !tr.HasText(locales.CommonViewDetail()) {
		t.Fatal("错误提示应显示概要和「查看详情」")
	}
	if tr.HasText("SQLITE_BUSY") {
		t.Fatal("详情默认折叠，不该直接铺开")
	}
	if err := tr.Click(locales.CommonViewDetail()); err != nil {
		t.Fatalf("点击「查看详情」：%v", err)
	}
	tr.Frame()
	if !tr.HasText("SQLITE_BUSY") || !tr.HasText(locales.CommonCollapse()) || !tr.HasText(locales.CommonCopy()) {
		t.Fatalf("展开后应显示原文、「收起」与「复制」，文字：%v", tr.Texts())
	}

	if err := tr.Click(locales.CommonCopy()); err != nil {
		t.Fatalf("点击「复制」：%v", err)
	}
	tr.Frame()
	if got := tr.Clipboard(); got != detail {
		t.Fatalf("剪贴板应是完整原文，得到 %q", got)
	}
	if !tr.HasText(locales.ToastCopied()) {
		t.Error("复制成功后按钮应显示「已复制」")
	}
	tr.Advance(2*time.Second + 100*time.Millisecond)
	tr.Frame()
	if tr.HasText(locales.ToastCopied()) || !tr.HasText(locales.CommonCopy()) {
		t.Error("2 秒后应换回「复制」")
	}

	if err := tr.Click(locales.CommonCollapse()); err != nil {
		t.Fatalf("点击「收起」：%v", err)
	}
	tr.Frame()
	if tr.HasText("SQLITE_BUSY") {
		t.Error("收起后原文应隐藏")
	}
}

func TestToastCopyFailureKeepsLabel(t *testing.T) {
	n := NewNotifier(nil)
	tr := newToastTester(t, lightLook(), n)
	n.Error("失败", "原因：磁盘已满")
	tr.Frame()
	_ = tr.Click(locales.CommonViewDetail())
	tr.Frame()
	tr.FailClipboard(errCopy)
	if err := tr.Click(locales.CommonCopy()); err != nil {
		t.Fatalf("点击「复制」：%v", err)
	}
	tr.Frame()
	if tr.HasText(locales.ToastCopied()) {
		t.Error("复制失败不能显示「已复制」")
	}
	if !tr.HasText("原因：磁盘已满") {
		t.Error("复制失败时原文仍应摊在界面上，用户照样读得到")
	}
}

type copyError struct{}

func (copyError) Error() string { return "剪贴板被占用" }

var errCopy = copyError{}

func TestToastUndoRunsActionAndCloses(t *testing.T) {
	n := NewNotifier(nil)
	tr := newToastTester(t, lightLook(), n)
	var undone atomic.Int32
	n.Undo("已移到回收站", locales.CommonUndo(), func() { undone.Add(1) })
	tr.Frame()
	if !tr.HasText("已移到回收站") || !tr.HasText(locales.CommonUndo()) {
		t.Fatal("应显示提示与「撤销」按钮")
	}
	// 撤销窗口比普通提示长：3 秒时仍在
	tr.Advance(4 * time.Second)
	tr.Frame()
	if !tr.HasText(locales.CommonUndo()) {
		t.Fatal("撤销窗口应长于 3 秒")
	}
	if err := tr.Click(locales.CommonUndo()); err != nil {
		t.Fatalf("点击「撤销」：%v", err)
	}
	tr.Frame()
	if undone.Load() != 1 {
		t.Fatalf("撤销动作应执行一次，实际 %d", undone.Load())
	}
	if tr.HasText("已移到回收站") {
		t.Error("点击撤销后提示应立即收起")
	}
}

func TestToastDismissAndCloseButton(t *testing.T) {
	n := NewNotifier(nil)
	tr := newToastTester(t, lightLook(), n)
	keep := n.Error("保留的错误", "")
	stale := n.Error("过时的错误", "")
	tr.Frame()
	if !tr.HasText("保留的错误") || !tr.HasText("过时的错误") {
		t.Fatal("两条错误都应显示")
	}
	n.Dismiss(stale)
	tr.Frame()
	if tr.HasText("过时的错误") {
		t.Error("Dismiss 应撤下指定的提示")
	}
	if !tr.HasText("保留的错误") {
		t.Error("Dismiss 不能误伤其他提示")
	}
	n.Dismiss("gz-toast-不存在") // 不存在的 ID（已自行消失）什么都不做
	tr.Frame()

	info, ok := tr.AccessByRole(ui.RoleButton, locales.CommonClose())
	if !ok || info.Name == "" {
		t.Fatal("关闭按钮必须有可访问名")
	}
	if err := tr.Click(locales.CommonClose()); err != nil {
		t.Fatalf("点击关闭：%v", err)
	}
	tr.Frame()
	if tr.HasText("保留的错误") {
		t.Error("点关闭应收起提示")
	}
	_ = keep
}

func TestToastBacklogIsNotLost(t *testing.T) {
	// 批量导入 10 个文件全失败：每条错误都不该在读到之前消失。
	// MyGo 同时只显示最新 3 条，更早的在队列里等；关掉一条，下一条露出来。
	n := NewNotifier(nil)
	tr := newToastTester(t, lightLook(), n)
	for i := 1; i <= 5; i++ {
		n.Error("文件 "+string(rune('0'+i))+" 导入失败", "")
	}
	tr.Frame()
	shown := 0
	for i := 1; i <= 5; i++ {
		if tr.HasText("文件 " + string(rune('0'+i)) + " 导入失败") {
			shown++
		}
	}
	if shown != 3 {
		t.Fatalf("同时应显示最新 3 条，实际 %d 条", shown)
	}
	for i := 0; i < 5; i++ {
		_ = tr.Click(locales.CommonClose())
		tr.Frame()
	}
	left := 0
	for i := 1; i <= 5; i++ {
		if tr.HasText("文件 " + string(rune('0'+i)) + " 导入失败") {
			left++
		}
	}
	if left != 0 {
		t.Errorf("逐条关闭后不应剩下提示，剩 %d 条", left)
	}
}

func TestToastTintsAreOpaqueAndDistinct(t *testing.T) {
	for name, l := range map[string]*Look{"light": lightLook(), "dark": darkLook()} {
		seen := map[ui.Color]ToastKind{}
		for _, kind := range []ToastKind{ToastSuccess, ToastError, ToastWarning, ToastInfo} {
			tint := l.toastTint(kind)
			if tint.fill.A != 255 {
				t.Errorf("%s/%s：提示底色必须不透明（没有背景模糊时半透明会透出下面的文字）", name, kind)
			}
			if prev, dup := seen[tint.fill]; dup {
				t.Errorf("%s：%s 与 %s 的底色相同", name, kind, prev)
			}
			seen[tint.fill] = kind
		}
	}
}

func TestOverComposites(t *testing.T) {
	base := ui.Color{R: 0, G: 0, B: 0, A: 255}
	white := ui.Color{R: 255, G: 255, B: 255, A: 255}
	if got := over(white.Alpha(0.5), base); got.R < 126 || got.R > 129 || got.A != 255 {
		t.Errorf("半透明白叠在黑上应约为 #808080，得到 %+v", got)
	}
	if got := over(white, base); got != white {
		t.Errorf("不透明色叠上去应原样，得到 %+v", got)
	}
	l := NewLook(ThemeSpec{Mode: ModeLight, Hue: 220, Saturation: 70, FontSize: DefaultFontSize, Wallpaper: DemoWallpaper(400, 300, 1, 0)}, Env{})
	if l.solid(l.T.UiPanelStrongBg).A != 255 {
		t.Error("壁纸模式下面板底色应合成成不透明")
	}
}

func TestTailwindPaletteParses(t *testing.T) {
	for name := range tailwindPalette {
		if c := tw(name); c.A != 255 {
			t.Errorf("%s 解析出的颜色不是不透明色：%+v", name, c)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("名字写错应 panic，而不是静默给一个黑色")
		}
	}()
	tw("no-such-color")
}
