package kit

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	confirmTitle   = "启用后台任务"
	confirmMessage = "启用后会让归知在关闭窗口后继续留在托盘里运行。"
	confirmOK      = "启用"
	confirmCancel  = "取消"
)

// confirmPage 是一个带确认对话框的页面：两枚页面按钮用来观察「焦点回到哪」与「点遮罩会不会落到背后」。
type confirmPage struct {
	st          ConfirmState
	open        bool
	loading     bool
	destructive bool
	untitled    bool
	accepted    int
	dismissed   int
	behind      int // 背后那枚按钮被点中的次数
}

func (p *confirmPage) view(l *Look, c ui.C) {
	ui.Column(c).Fill().Pad(24).Gap(24).Children(func() {
		openC, behindC := c.Key("open"), c.Key("behind")
		opener := ui.Row(openC).Size(120, 40).HitWith(ui.ID(openC), "打开", gfx.HitOptions{Role: gfx.RoleButton})
		if opener.Clicked() {
			p.open = true
		}
		behind := ui.Row(behindC).Size(120, 40).HitWith(ui.ID(behindC), "背后的按钮", gfx.HitOptions{Role: gfx.RoleButton})
		if behind.Clicked() {
			p.behind++
		}
	})
	title := confirmTitle
	if p.untitled {
		title = ""
	}
	switch l.ConfirmDialog(c, &p.st, ConfirmSpec{
		Open: p.open, Title: title, Message: confirmMessage,
		ConfirmText: confirmOK, CancelText: confirmCancel,
		Destructive: p.destructive, Loading: p.loading,
	}) {
	case ConfirmAccepted:
		p.accepted++
		p.open = false
	case ConfirmDismissed:
		p.dismissed++
		p.open = false
	}
}

func openConfirmPage(t *testing.T, spec Spec, configure func(p *confirmPage)) (*harness, *confirmPage) {
	t.Helper()
	p := &confirmPage{}
	if configure != nil {
		configure(p)
	}
	return openPage(t, spec, 800, 600, p.view), p
}

// openConfirm 点「打开」让对话框出现。
func openConfirm(h *harness) {
	h.t.Helper()
	h.click("打开")
}

func TestConfirmClosedDrawsNothing(t *testing.T) {
	h, _ := openConfirmPage(t, still(), nil)
	if h.has(gfx.RoleGroup, confirmTitle) || h.has(gfx.RoleButton, confirmOK) || h.has(gfx.RoleButton, confirmCancel) {
		t.Error("没有打开时不应出现对话框的任何控件")
	}
}

// 打开：模态面板居中，名称是标题、描述是说明文字；取消按钮拿到键盘焦点（渲染层 autoFocus 取消）。
func TestConfirmOpensCenteredWithCancelFocused(t *testing.T) {
	h, _ := openConfirmPage(t, still(), nil)
	openConfirm(h)
	panel, ok := h.node(gfx.RoleGroup, confirmTitle)
	if !ok {
		t.Fatal("没有找到对话框面板（group）")
	}
	if panel.Description != confirmMessage {
		t.Errorf("面板描述 %q，期望说明文字", panel.Description)
	}
	// 窗口 800x600：面板水平居中，宽度取 max-w-sm = 24rem = 384。
	b := panel.Bounds
	if math.Abs(float64(b.Width-384)) > 0.01 {
		t.Errorf("面板宽 %g，期望 384（max-w-sm）", b.Width)
	}
	if mid := b.X + b.Width/2; math.Abs(float64(mid-400)) > 0.5 {
		t.Errorf("面板水平中心 %g，期望 400", mid)
	}
	if mid := b.Y + b.Height/2; math.Abs(float64(mid-300)) > 0.5 {
		t.Errorf("面板垂直中心 %g，期望 300", mid)
	}
	cancel, ok := h.node(gfx.RoleButton, confirmCancel)
	if !ok || !cancel.Focused {
		t.Errorf("打开后取消按钮应当持有焦点：存在=%v focused=%v", ok, cancel.Focused)
	}
	confirm, ok := h.node(gfx.RoleButton, confirmOK)
	if !ok || confirm.Focused {
		t.Errorf("确认按钮不应持有焦点：存在=%v focused=%v", ok, confirm.Focused)
	}
	// 两枚按钮同高（h-10 = 40）、中间隔 gap-3 = 12；flex-1 的基准是 0%，border-box 下外尺寸不小于内边距加边框：
	// 取消（带 1px 边框）基准 34、确认 32，空闲空间 334 − 12 − 66 = 256 再平分，所以取消 162、确认 160（与渲染层的实测一致）。
	if cancel.Bounds.Height != 40 || confirm.Bounds.Height != 40 {
		t.Errorf("按钮应 40 高：取消 %+v 确认 %+v", cancel.Bounds, confirm.Bounds)
	}
	if cancel.Bounds.Width != 162 || confirm.Bounds.Width != 160 {
		t.Errorf("按钮宽度：取消 %g、确认 %g，期望 162 与 160", cancel.Bounds.Width, confirm.Bounds.Width)
	}
	if gap := confirm.Bounds.X - (cancel.Bounds.X + cancel.Bounds.Width); math.Abs(float64(gap-12)) > 0.01 {
		t.Errorf("按钮间距 %g，期望 12（gap-3）", gap)
	}
	// 面板内边距：p-6（24）加 1px 边框。
	if left := cancel.Bounds.X - b.X; math.Abs(float64(left-25)) > 0.01 {
		t.Errorf("按钮左边到面板左边 %g，期望 25", left)
	}
}

// 无标题时，面板的可访问名取确认按钮的文字。
func TestConfirmWithoutTitleUsesConfirmText(t *testing.T) {
	h, _ := openConfirmPage(t, still(), func(p *confirmPage) { p.untitled = true })
	openConfirm(h)
	if !h.has(gfx.RoleGroup, confirmOK) {
		t.Error("无标题时面板名称应取确认按钮文字")
	}
}

func TestConfirmEscapeDismisses(t *testing.T) {
	h, p := openConfirmPage(t, still(), nil)
	openConfirm(h)
	h.keys("Escape")
	if p.dismissed != 1 || p.accepted != 0 || p.open {
		t.Errorf("Esc：dismissed=%d accepted=%d open=%v，期望关闭且按取消处理", p.dismissed, p.accepted, p.open)
	}
}

// Enter 在文档级监听：焦点在取消按钮上时按 Enter 也是「确认」，不是激活取消。
func TestConfirmEnterAcceptsEvenWithCancelFocused(t *testing.T) {
	h, p := openConfirmPage(t, still(), nil)
	openConfirm(h)
	h.keys("Enter")
	if p.accepted != 1 || p.dismissed != 0 || p.open {
		t.Errorf("Enter：accepted=%d dismissed=%d open=%v，期望确认并关闭", p.accepted, p.dismissed, p.open)
	}
}

func TestConfirmButtonsAndScrim(t *testing.T) {
	t.Run("点确认", func(t *testing.T) {
		h, p := openConfirmPage(t, still(), nil)
		openConfirm(h)
		h.click(confirmOK)
		if p.accepted != 1 || p.open {
			t.Errorf("accepted=%d open=%v", p.accepted, p.open)
		}
	})
	t.Run("点取消", func(t *testing.T) {
		h, p := openConfirmPage(t, still(), nil)
		openConfirm(h)
		h.click(confirmCancel)
		if p.dismissed != 1 || p.open {
			t.Errorf("dismissed=%d open=%v", p.dismissed, p.open)
		}
	})
	t.Run("点遮罩关闭，且不落到背后的控件", func(t *testing.T) {
		h, p := openConfirmPage(t, still(), nil)
		openConfirm(h)
		// (60, 44) 正是背后那枚按钮的中心（页面内边距 24，第二枚按钮在 y=88..128），但在遮罩的区域里。
		behind, _ := h.node(gfx.RoleButton, "背后的按钮")
		h.clickAt(behind.Bounds.X+behind.Bounds.Width/2, behind.Bounds.Y+behind.Bounds.Height/2)
		if p.dismissed != 1 || p.open || p.behind != 0 {
			t.Errorf("点遮罩：dismissed=%d open=%v behind=%d，期望关闭且不触发背后的按钮", p.dismissed, p.open, p.behind)
		}
	})
	t.Run("点面板里的文字不关闭", func(t *testing.T) {
		h, p := openConfirmPage(t, still(), nil)
		openConfirm(h)
		panel, _ := h.node(gfx.RoleGroup, confirmTitle)
		h.clickAt(panel.Bounds.X+panel.Bounds.Width/2, panel.Bounds.Y+panel.Bounds.Height/2-20)
		if !p.open || p.dismissed != 0 {
			t.Errorf("点面板内部：open=%v dismissed=%d，期望保持打开", p.open, p.dismissed)
		}
	})
}

// 加载中：两枚按钮禁用；Esc 与 Enter 不响应；点遮罩仍然关闭（渲染层没有禁它）。
func TestConfirmLoading(t *testing.T) {
	setup := func(t *testing.T) (*harness, *confirmPage) {
		h, p := openConfirmPage(t, still(), func(p *confirmPage) { p.loading = true })
		openConfirm(h)
		return h, p
	}
	t.Run("按钮禁用", func(t *testing.T) {
		h, _ := setup(t)
		for _, name := range []string{confirmCancel, confirmOK} {
			if n, ok := h.node(gfx.RoleButton, name); !ok || !n.Disabled {
				t.Errorf("%q：存在=%v disabled=%v，期望禁用", name, ok, n.Disabled)
			}
		}
	})
	t.Run("Esc 与 Enter 不响应", func(t *testing.T) {
		h, p := setup(t)
		h.keys("Escape")
		h.keys("Enter")
		if !p.open || p.accepted != 0 || p.dismissed != 0 {
			t.Errorf("加载中：open=%v accepted=%d dismissed=%d，期望无变化", p.open, p.accepted, p.dismissed)
		}
	})
	t.Run("点遮罩仍然关闭", func(t *testing.T) {
		h, p := setup(t)
		h.clickAt(5, 5)
		if p.open || p.dismissed != 1 {
			t.Errorf("加载中点遮罩：open=%v dismissed=%d，期望关闭", p.open, p.dismissed)
		}
	})
}

// 关闭后焦点回到打开之前持有它的控件。
func TestConfirmReturnsFocusToOpener(t *testing.T) {
	h, _ := openConfirmPage(t, still(), nil)
	openConfirm(h) // 点击「打开」使它拿到焦点，随后对话框把焦点移到取消按钮
	if n, _ := h.node(gfx.RoleButton, "打开"); n.Focused {
		t.Fatal("对话框打开后焦点不应还在页面按钮上")
	}
	h.keys("Escape")
	if n, ok := h.node(gfx.RoleButton, "打开"); !ok || !n.Focused {
		t.Errorf("关闭后焦点应回到「打开」按钮：存在=%v focused=%v", ok, n.Focused)
	}
}

// 入场动画：淡入 + 从 95% 放大，180 ms（duration-base），曲线 ease-default。关闭档直接到终态。
func TestConfirmEnterAnimation(t *testing.T) {
	open := func(t *testing.T, spec Spec) *harness {
		h, _ := openConfirmPage(t, spec, nil)
		if err := h.session.ClickLabel("打开"); err != nil {
			t.Fatal(err)
		}
		h.frames(1) // 不推进时钟
		return h
	}
	panelWidthNow := func(h *harness) float32 {
		n, ok := h.node(gfx.RoleGroup, confirmTitle)
		if !ok {
			h.t.Fatal("对话框没有出现")
		}
		return n.Bounds.Width
	}
	t.Run("标准档", func(t *testing.T) {
		spec := DefaultSpec()
		spec.Motion = MotionLevelStandard
		h := open(t, spec)
		elapsed := time.Duration(0)
		for _, step := range []time.Duration{0, 30, 30, 30, 30, 30, 60} {
			if step > 0 {
				advance(h, step*time.Millisecond)
				elapsed += step * time.Millisecond
			}
			// animate-in 不带 ease-* 类：用 CSS 关键字 ease，即 cubic-bezier(0.25, 0.1, 0.25, 1)。
			e := cubicBezierY(0.25, 0.1, 0.25, 1, math.Min(float64(elapsed)/float64(180*time.Millisecond), 1))
			alpha, ok := textAlpha(h, confirmMessage)
			if !ok {
				t.Fatalf("%v：入场期间说明文字应当已经在画", elapsed)
			}
			if math.Abs(float64(alpha)-e) > 0.03 {
				t.Errorf("%v：不透明度 %.3f，期望 %.3f（180ms ease-default）", elapsed, alpha, e)
			}
		}
	})
	t.Run("减弱档", func(t *testing.T) {
		spec := DefaultSpec()
		spec.Motion = MotionLevelReduced
		h := open(t, spec)
		advance(h, 120*time.Millisecond) // 减弱档 base = 110ms
		if alpha, _ := textAlpha(h, confirmMessage); alpha < 0.999 {
			t.Errorf("减弱档 120ms 应已完成，不透明度 %.3f", alpha)
		}
	})
	t.Run("关闭档", func(t *testing.T) {
		h := open(t, still())
		if alpha, ok := textAlpha(h, confirmMessage); !ok || alpha < 0.999 {
			t.Errorf("关闭档第一帧：ok=%v 不透明度 %.3f，期望完整显示", ok, alpha)
		}
		if w := panelWidthNow(h); math.Abs(float64(w-384)) > 0.01 {
			t.Errorf("关闭档面板宽 %g，期望 384", w)
		}
	})
}

// 危险确认：顶部多一个警示圈，面板因此更高；确认按钮的外观走危险色（颜色由 L3 像素比对守护）。
func TestConfirmDestructiveAddsIcon(t *testing.T) {
	plain, _ := openConfirmPage(t, still(), nil)
	openConfirm(plain)
	p1, _ := plain.node(gfx.RoleGroup, confirmTitle)
	danger, _ := openConfirmPage(t, still(), func(p *confirmPage) { p.destructive = true })
	openConfirm(danger)
	p2, _ := danger.node(gfx.RoleGroup, confirmTitle)
	// 图标圈 w-12 h-12（48）加下边距 mb-4（16）。
	if got := p2.Bounds.Height - p1.Bounds.Height; math.Abs(float64(got-64)) > 0.01 {
		t.Errorf("危险确认多出的高度 %g，期望 64（48 + mb-4）", got)
	}
}

// 加载中的确认按钮里有转圈（Loader2 + animate-spin）：动效开启时随时钟转动，关闭档静止。
// Viva 的快照不暴露图标的旋转角，所以比较相隔 250 ms 的两帧图元：转动的转圈两帧不同，静止的完全相同。
func TestSpinnerTurnsOnlyWhenMotionAllowed(t *testing.T) {
	primitives := func(spec Spec) (before, after []app.Primitive) {
		h, _ := openConfirmPage(t, spec, func(p *confirmPage) { p.loading = true })
		openConfirm(h)
		advance(h, 400*time.Millisecond) // 入场动画走完，页面上只剩转圈在动
		before = append(before, h.session.Snapshot().Primitives...)
		advance(h, 250*time.Millisecond)
		after = append(after, h.session.Snapshot().Primitives...)
		return before, after
	}
	t.Run("标准档转动", func(t *testing.T) {
		spec := DefaultSpec()
		spec.Motion = MotionLevelStandard
		before, after := primitives(spec)
		if reflect.DeepEqual(before, after) {
			t.Error("标准档下相隔 250 ms 的两帧完全相同：转圈没有转")
		}
	})
	t.Run("减弱档仍转动", func(t *testing.T) {
		// 渲染层：animate-spin 在「减弱」档不受影响（只有「关闭」档把动画压成 0.01ms）。
		spec := DefaultSpec()
		spec.Motion = MotionLevelReduced
		before, after := primitives(spec)
		if reflect.DeepEqual(before, after) {
			t.Error("减弱档下转圈应当照常转动")
		}
	})
	t.Run("关闭档静止", func(t *testing.T) {
		before, after := primitives(still())
		if !reflect.DeepEqual(before, after) {
			t.Error("关闭档下转圈应当静止")
		}
	})
}

// confirmTextBounds 是当前帧里某段文字的外框；没有则 ok 为假。
func confirmTextBounds(h *harness, value string) (gfx.Rect, bool) {
	for _, tx := range h.session.Snapshot().Texts {
		if tx.Value == value {
			return tx.Bounds, true
		}
	}
	return gfx.Rect{}, false
}

// 说明区可以换成任意内容（彻底删除的确认框：两段话加一个复选框，靠左、子项间距 12、下边距 24）：
// Message 不再声明，对话框的可访问描述取 Description，复选框翻转的是调用方的值。
func TestConfirmBodyReplacesTheMessage(t *testing.T) {
	const first, second, option = "第一段说明", "第二段说明", "同时清除相关证据"
	var open, cleared bool
	var st ConfirmState
	h := openPage(t, still(), 800, 600, func(l *Look, c ui.C) {
		openC := c.Key("open")
		if ui.Row(openC).Size(120, 40).HitWith(ui.ID(openC), "打开", gfx.HitOptions{Role: gfx.RoleButton}).Clicked() {
			open = true
		}
		l.ConfirmDialog(c, &st, ConfirmSpec{
			Open: open, Title: confirmTitle, Message: confirmMessage,
			Description: first + " " + second + " " + option,
			Body: func(c ui.C) {
				l.Text(c.Key("p1"), first, TextSM, WeightNormal, l.T.MutedForeground, Wrapped())
				l.Text(c.Key("p2"), second, TextSM, WeightNormal, l.T.MutedForeground, Wrapped())
				l.Checkbox(c.Key("cb"), &cleared, CheckboxSpec{Label: option})
			},
			ConfirmText: confirmOK, CancelText: confirmCancel, Destructive: true,
		})
	})
	openConfirm(h)
	if _, ok := confirmTextBounds(h, confirmMessage); ok {
		t.Error("给了 Body 时不应再画 Message")
	}
	panel, ok := h.node(gfx.RoleGroup, confirmTitle)
	if !ok {
		t.Fatal("没有找到对话框面板")
	}
	if want := first + " " + second + " " + option; panel.Description != want {
		t.Errorf("面板描述 %q，期望 %q", panel.Description, want)
	}
	p1, ok1 := confirmTextBounds(h, first)
	p2, ok2 := confirmTextBounds(h, second)
	if !ok1 || !ok2 {
		t.Fatalf("两段话都应当画出来：%v %v", ok1, ok2)
	}
	// 靠左（text-left）：从面板内容区左缘起（1px 边框加 p-6），而不是居中。
	for name, b := range map[string]gfx.Rect{first: p1, second: p2} {
		if math.Abs(float64(b.X-(panel.Bounds.X+25))) > 0.01 {
			t.Errorf("%q 左缘 %g，期望贴着内容区左缘 %g", name, b.X, panel.Bounds.X+25)
		}
	}
	// space-y-3：两段话之间隔 12。
	if gap := p2.Y - (p1.Y + p1.Height); math.Abs(float64(gap-12)) > 0.01 {
		t.Errorf("两段话之间隔 %g，期望 12（space-y-3）", gap)
	}
	box, ok := h.node(gfx.RoleCheckbox, option)
	if !ok {
		t.Fatal("没有找到复选框")
	}
	// 复选框行（行高 20，方框 16 居中）与上一段隔 12。
	if gap := (box.Bounds.Y - 2) - (p2.Y + p2.Height); math.Abs(float64(gap-12)) > 0.01 {
		t.Errorf("复选框行与上一段之间隔 %g，期望 12", gap)
	}
	// 说明区下边距 mb-6：按钮行顶上留 24。
	cancel, _ := h.node(gfx.RoleButton, confirmCancel)
	if gap := cancel.Bounds.Y - (box.Bounds.Y + 16 + 2); math.Abs(float64(gap-24)) > 0.01 {
		t.Errorf("说明区到按钮行 %g，期望 24（mb-6）", gap)
	}
	h.clickAt(box.Bounds.X+8, box.Bounds.Y+8)
	if !cleared {
		t.Error("点复选框应翻转调用方的值")
	}
}

// 没有 Body 时描述仍取 Message；给了 Description 时描述取它（Body 的文字不在 Message 里）。
func TestConfirmDescriptionDefaultsToTheMessage(t *testing.T) {
	h, _ := openConfirmPage(t, still(), nil)
	openConfirm(h)
	panel, _ := h.node(gfx.RoleGroup, confirmTitle)
	if panel.Description != confirmMessage {
		t.Errorf("没有给 Description 时面板描述 %q，期望取 Message", panel.Description)
	}
}
