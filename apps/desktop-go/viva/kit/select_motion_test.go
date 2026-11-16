package kit

import (
	"math"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

// cubicBezierY 是 CSS cubic-bezier(x1, y1, x2, y2) 在时间进度 x 处的输出，用二分法解参数——
// 独立于 Viva 自己的缓动实现，当作渲染层动效的参照。
func cubicBezierY(x1, y1, x2, y2, x float64) float64 {
	bez := func(a, b, s float64) float64 { return 3*(1-s)*(1-s)*s*a + 3*(1-s)*s*s*b + s*s*s }
	lo, hi := 0.0, 1.0
	for i := 0; i < 60; i++ {
		mid := (lo + hi) / 2
		if bez(x1, x2, mid) < x {
			lo = mid
		} else {
			hi = mid
		}
	}
	return bez(y1, y2, (lo+hi)/2)
}

// easeEnter 是渲染层 --motion-easing-enter：cubic-bezier(0, 0, 0.2, 1)。
func easeEnter(x float64) float64 { return cubicBezierY(0, 0, 0.2, 1, x) }

// textAlpha 返回当前帧里文字 value 的不透明度（入场淡入时小于 1）；没有这行文字时 ok 为 false。
func textAlpha(h *harness, value string) (alpha float32, ok bool) {
	for _, tx := range h.session.Snapshot().Texts {
		if tx.Value == value {
			return tx.Style.Color.Alpha, true
		}
	}
	return 0, false
}

// panelWidth 是当前帧里下拉面板外边框（带圆角的最大矩形）的宽度；找不到返回 0。
func panelWidth(h *harness) float32 {
	var w float32
	for _, pr := range h.session.Snapshot().Primitives {
		if pr.Bounds.Width > 100 && pr.Bounds.Width < 200 && pr.Bounds.Height > 100 && pr.Bounds.Width > w {
			w = pr.Bounds.Width
		}
	}
	return w
}

func openMotionSelect(t *testing.T, spec Spec) *harness {
	t.Helper()
	p := &selectPage{value: "ask"}
	h := openPage(t, spec, 800, 600, p.view)
	if err := h.session.ClickLabel("关闭窗口行为"); err != nil {
		t.Fatal(err)
	}
	h.frames(1) // 不推进时钟：入场动画从 0 开始
	return h
}

func advance(h *harness, d time.Duration) {
	h.t.Helper()
	if err := h.session.Advance(d); err != nil {
		h.t.Fatal(err)
	}
	h.frames(1)
}

// 面板入场：淡入 + 从 95% 放大（animate-in fade-in-0 zoom-in-95 duration-quick ease-enter）。
// 标准档 quick = 120ms；曲线 cubic-bezier(0, 0, 0.2, 1)。
func TestSelectEnterAnimationStandard(t *testing.T) {
	spec := DefaultSpec()
	spec.Motion = MotionLevelStandard
	h := openMotionSelect(t, spec)
	elapsed := time.Duration(0)
	for _, step := range []time.Duration{0, 20, 20, 20, 20, 20, 20, 40} {
		if step > 0 {
			advance(h, step*time.Millisecond)
			elapsed += step * time.Millisecond
		}
		e := easeEnter(math.Min(float64(elapsed)/float64(120*time.Millisecond), 1))
		alpha, ok := textAlpha(h, "最小化到托盘")
		if !ok {
			t.Fatalf("%v：入场期间面板里的文字应当已经在画", elapsed)
		}
		if math.Abs(float64(alpha)-e) > 0.02 {
			t.Errorf("%v：不透明度 %.3f，期望 %.3f（cubic-bezier(0,0,.2,1)，120ms）", elapsed, alpha, e)
		}
		wantW := 180 * (0.95 + 0.05*e)
		if got := panelWidth(h); math.Abs(float64(got)-wantW) > 0.6 {
			t.Errorf("%v：面板宽 %.2f，期望 %.2f（zoom-in-95 从 95%% 放大到 100%%）", elapsed, got, wantW)
		}
	}
}

// 「减弱」档：quick = 80ms。
func TestSelectEnterAnimationReduced(t *testing.T) {
	spec := DefaultSpec()
	spec.Motion = MotionLevelReduced
	h := openMotionSelect(t, spec)
	advance(h, 40*time.Millisecond)
	alpha, _ := textAlpha(h, "最小化到托盘")
	if want := easeEnter(40.0 / 80.0); math.Abs(float64(alpha)-want) > 0.02 {
		t.Errorf("减弱档 40ms：不透明度 %.3f，期望 %.3f（80ms）", alpha, want)
	}
	advance(h, 50*time.Millisecond)
	if alpha, _ = textAlpha(h, "最小化到托盘"); alpha < 0.999 {
		t.Errorf("减弱档 90ms 应已完成，不透明度 %.3f", alpha)
	}
}

// 关闭档：直接到终态，第一帧就是完整的面板。
func TestSelectEnterAnimationOff(t *testing.T) {
	h := openMotionSelect(t, still())
	if alpha, ok := textAlpha(h, "最小化到托盘"); !ok || alpha < 0.999 {
		t.Errorf("关闭档第一帧：ok=%v 不透明度 %.3f，期望完整显示", ok, alpha)
	}
	if got := panelWidth(h); math.Abs(float64(got)-180) > 0.01 {
		t.Errorf("关闭档面板宽 %.2f，期望 180（不缩放）", got)
	}
}

// 关闭后的「残影」：Viva 的浮层退场用最后一帧快照重放，最短也要出一帧（台账 A-008）。
// 渲染层的 Select 没有退场动画、点一下立刻消失，所以这是已登记的偏差；这里把行为钉住，
// 升级 Viva 后若残影消失或变长，测试会提醒复核台账。残影只是一张图：不在无障碍树里、不接收点击。
func TestSelectLeavesAOneFrameGhost(t *testing.T) {
	spec := DefaultSpec()
	spec.Motion = MotionLevelStandard
	h := openMotionSelect(t, spec)
	advance(h, 200*time.Millisecond) // 入场走完
	if err := h.session.Keys("Escape"); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	if h.has(gfx.RoleListBox, "关闭窗口行为") {
		t.Error("Esc 之后面板不应再出现在无障碍树里（残影只是一张图）")
	}
	if _, ok := textAlpha(h, "最小化到托盘"); !ok {
		t.Error("Esc 后的第一帧应仍画着面板的残影（A-008）；若这条失败，说明 Viva 行为变了，复核台账")
	}
	advance(h, time.Millisecond)
	if _, ok := textAlpha(h, "最小化到托盘"); ok {
		t.Error("时钟前进 1ms 之后残影应当消失（退场时长是 1ns）")
	}
}
