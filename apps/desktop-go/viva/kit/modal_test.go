package kit

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	modalTitle    = "任务详情"
	modalSubtitle = "来源平台"
	modalDialog   = "对话框"
	modalClose    = "关闭"
)

// modalPage 是一个带 Modal 的页面：两枚页面按钮用来观察「焦点回到哪」与「点遮罩会不会落到背后」。
type modalPage struct {
	st         ModalState
	open       bool
	title      string
	subtitle   bool
	actions    bool
	long       bool
	raw        bool
	size       ModalSize
	noBackdrop bool
	noEscape   bool
	noClose    bool
	closed     int
	behind     int
	bodyClicks int
	actClicks  int
	toasts     *Notifier
}

func (p *modalPage) view(l *Look, c ui.C) {
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
	spec := ModalSpec{
		Open: p.open, Size: p.size, Title: p.title, DialogLabel: modalDialog, CloseLabel: modalClose,
		NoBackdropClose: p.noBackdrop, NoEscape: p.noEscape, NoClose: p.noClose, RawBody: p.raw,
		Body: func(c ui.C) {
			bc := c.Key("body-button")
			b := ui.Row(bc).Size(100, 32).HitWith(ui.ID(bc), "正文按钮", gfx.HitOptions{Role: gfx.RoleButton})
			if b.Clicked() {
				p.bodyClicks++
			}
			if p.long {
				for i := 0; i < 40; i++ {
					l.Text(c.Key("line"+strconv.Itoa(i)), "正文第 "+strconv.Itoa(i)+" 行", TextSM, WeightNormal, l.T.Foreground)
				}
			}
		},
	}
	if p.subtitle {
		spec.SubtitleText = modalSubtitle
		spec.Subtitle = func(c ui.C) {
			l.Text(c.Key("sub"), modalSubtitle, TextXS, WeightNormal, l.T.MutedForeground)
		}
	}
	if p.actions {
		spec.HeaderActions = func(c ui.C) {
			ac := c.Key("action")
			a := ui.Row(ac).Size(80, 32).HitWith(ui.ID(ac), "页眉操作", gfx.HitOptions{Role: gfx.RoleButton})
			if a.Clicked() {
				p.actClicks++
			}
		}
	}
	if l.Modal(c, &p.st, spec) {
		p.closed++
		p.open = false
	}
	if p.toasts != nil {
		l.Toasts(c, p.toasts) // 与外壳一致：提示声明在窗口之后
	}
}

func openModalPage(t *testing.T, spec Spec, width, height int, configure func(p *modalPage)) (*harness, *modalPage) {
	t.Helper()
	p := &modalPage{title: modalTitle}
	if configure != nil {
		configure(p)
	}
	return openPage(t, spec, width, height, p.view), p
}

func openModal(h *harness) {
	h.t.Helper()
	h.click("打开")
}

func TestModalClosedDrawsNothing(t *testing.T) {
	h, _ := openModalPage(t, still(), 800, 600, nil)
	if h.has(gfx.RoleGroup, modalTitle) || h.has(gfx.RoleButton, modalClose) || h.has(gfx.RoleHeading, modalTitle) {
		t.Error("没有打开时不应出现窗口的任何控件")
	}
}

// 打开：面板居中，名称是标题、描述是副标题；页眉里有二级标题与关闭钮；md 尺寸宽 500。
func TestModalOpensCenteredWithHeader(t *testing.T) {
	h, _ := openModalPage(t, still(), 800, 600, func(p *modalPage) { p.subtitle = true })
	openModal(h)
	panel, ok := h.node(gfx.RoleGroup, modalTitle)
	if !ok {
		t.Fatal("没有找到窗口面板（group）")
	}
	if panel.Description != modalSubtitle {
		t.Errorf("面板描述 %q，期望副标题", panel.Description)
	}
	b := panel.Bounds
	if math.Abs(float64(b.Width-500)) > 0.01 {
		t.Errorf("面板宽 %g，期望 500（md）", b.Width)
	}
	if mid := b.X + b.Width/2; math.Abs(float64(mid-400)) > 0.5 {
		t.Errorf("面板水平中心 %g，期望 400", mid)
	}
	if mid := b.Y + b.Height/2; math.Abs(float64(mid-300)) > 0.5 {
		t.Errorf("面板垂直中心 %g，期望 300", mid)
	}
	heading, ok := h.node(gfx.RoleHeading, modalTitle)
	if !ok || heading.Level != 2 {
		t.Errorf("页眉标题应是二级标题：存在=%v level=%d", ok, heading.Level)
	}
	closeBtn, ok := h.node(gfx.RoleButton, modalClose)
	if !ok {
		t.Fatal("没有关闭钮")
	}
	// 关闭钮 p-2 + 20px 图标 = 36 见方；页眉 py-4 加 1px 边框；右边距 px-6 加 1px 面板边框。
	if closeBtn.Bounds.Width != 36 || closeBtn.Bounds.Height != 36 {
		t.Errorf("关闭钮 %gx%g，期望 36x36", closeBtn.Bounds.Width, closeBtn.Bounds.Height)
	}
	if right := b.X + b.Width - (closeBtn.Bounds.X + closeBtn.Bounds.Width); math.Abs(float64(right-25)) > 0.01 {
		t.Errorf("关闭钮右边到面板右边 %g，期望 25", right)
	}
	// 有副标题：左列 28 + 2 + 16 = 46 高，比关闭钮高；页眉 = 16 + 46 + 16 + 1 的下边框，再加面板的 1px 上边框。
	if top := closeBtn.Bounds.Y - b.Y; top < 17 || top > 17+(46-36)/2+0.01 {
		t.Errorf("关闭钮上边到面板上边 %g，期望在 17~22", top)
	}
}

// 没有标题：没有页眉也没有关闭钮，可访问名取 DialogLabel。
func TestModalWithoutTitleHasNoHeader(t *testing.T) {
	h, _ := openModalPage(t, still(), 800, 600, func(p *modalPage) { p.title = "" })
	openModal(h)
	if !h.has(gfx.RoleGroup, modalDialog) {
		t.Error("无标题时面板名称应取 DialogLabel")
	}
	if h.has(gfx.RoleButton, modalClose) {
		t.Error("无标题时不应有关闭钮")
	}
}

func TestModalCloseWays(t *testing.T) {
	t.Run("Esc", func(t *testing.T) {
		h, p := openModalPage(t, still(), 800, 600, nil)
		openModal(h)
		h.keys("Escape")
		if p.closed != 1 || p.open {
			t.Errorf("Esc：closed=%d open=%v", p.closed, p.open)
		}
	})
	t.Run("关闭钮", func(t *testing.T) {
		h, p := openModalPage(t, still(), 800, 600, nil)
		openModal(h)
		h.click(modalClose)
		if p.closed != 1 || p.open {
			t.Errorf("关闭钮：closed=%d open=%v", p.closed, p.open)
		}
	})
	t.Run("点遮罩关闭，且不落到背后的控件", func(t *testing.T) {
		h, p := openModalPage(t, still(), 800, 600, nil)
		openModal(h)
		behind, _ := h.node(gfx.RoleButton, "背后的按钮")
		h.clickAt(behind.Bounds.X+behind.Bounds.Width/2, behind.Bounds.Y+behind.Bounds.Height/2)
		if p.closed != 1 || p.open || p.behind != 0 {
			t.Errorf("点遮罩：closed=%d open=%v behind=%d，期望关闭且不触发背后的按钮", p.closed, p.open, p.behind)
		}
	})
	t.Run("点面板不关闭", func(t *testing.T) {
		h, p := openModalPage(t, still(), 800, 600, nil)
		openModal(h)
		h.click("正文按钮")
		if !p.open || p.closed != 0 || p.bodyClicks != 1 {
			t.Errorf("点正文按钮：open=%v closed=%d bodyClicks=%d", p.open, p.closed, p.bodyClicks)
		}
		panel, _ := h.node(gfx.RoleGroup, modalTitle)
		h.clickAt(panel.Bounds.X+panel.Bounds.Width-40, panel.Bounds.Y+panel.Bounds.Height-10)
		if !p.open {
			t.Error("点面板内的空白处不应关闭")
		}
	})
	t.Run("关掉点遮罩：遮罩只吸收点击，Esc 照常", func(t *testing.T) {
		h, p := openModalPage(t, still(), 800, 600, func(p *modalPage) { p.noBackdrop = true })
		openModal(h)
		h.clickAt(5, 5)
		if !p.open || p.closed != 0 || p.behind != 0 {
			t.Fatalf("点遮罩：open=%v closed=%d behind=%d，期望不关闭、也不落到背后", p.open, p.closed, p.behind)
		}
		h.keys("Escape")
		if p.open || p.closed != 1 {
			t.Errorf("Esc：open=%v closed=%d，期望关闭", p.open, p.closed)
		}
	})
	t.Run("关掉 Esc：点遮罩照常", func(t *testing.T) {
		h, p := openModalPage(t, still(), 800, 600, func(p *modalPage) { p.noEscape = true })
		openModal(h)
		h.keys("Escape")
		if !p.open || p.closed != 0 {
			t.Fatalf("Esc：open=%v closed=%d，期望不响应", p.open, p.closed)
		}
		h.clickAt(5, 5)
		if p.open || p.closed != 1 {
			t.Errorf("点遮罩：open=%v closed=%d，期望关闭", p.open, p.closed)
		}
	})
	t.Run("隐藏关闭钮", func(t *testing.T) {
		h, _ := openModalPage(t, still(), 800, 600, func(p *modalPage) { p.noClose = true })
		openModal(h)
		if h.has(gfx.RoleButton, modalClose) {
			t.Error("NoClose 时不应有关闭钮")
		}
	})
}

// Tab 在面板内循环：页眉操作 → 关闭钮 → 正文按钮 → 回到页眉操作，焦点永远不会走到背后的页面。
func TestModalTrapsFocus(t *testing.T) {
	h, _ := openModalPage(t, still(), 800, 600, func(p *modalPage) { p.actions = true })
	openModal(h)
	focusedName := func() string {
		for _, n := range h.session.Access() {
			if n.Focused {
				return n.Label
			}
		}
		return ""
	}
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		h.keys("Tab")
		name := focusedName()
		seen[name] = true
		if name == "打开" || name == "背后的按钮" {
			t.Fatalf("Tab 第 %d 次：焦点跑到了背后的页面（%q）", i+1, name)
		}
	}
	for _, want := range []string{"页眉操作", modalClose, "正文按钮"} {
		if !seen[want] {
			t.Errorf("Tab 循环里没有经过 %q（经过：%v）", want, seen)
		}
	}
}

// 提示在窗口之上：窗口打开时提示里的按钮仍然点得到，并且点它不会关闭窗口、也不会落到背后的页面。
// （Modal 把指针限制在自己的焦点作用域里，提示要借用同一个作用域。）
func TestToastStaysClickableAboveModal(t *testing.T) {
	n := NewNotifier(nil)
	undone := 0
	n.Undo("已删除", "撤销", func() { undone++ })
	h, p := openModalPage(t, still(), 1000, 700, func(p *modalPage) { p.toasts = n })
	openModal(h)
	if !h.has(gfx.RoleButton, "撤销") {
		t.Fatal("窗口打开时提示应仍然显示")
	}
	h.click("撤销")
	if undone != 1 {
		t.Errorf("点「撤销」应执行回调 1 次，实际 %d（提示被窗口的指针陷阱挡住了？）", undone)
	}
	if !p.open || p.closed != 0 || p.behind != 0 {
		t.Errorf("点提示不该影响窗口与背后的页面：open=%v closed=%d behind=%d", p.open, p.closed, p.behind)
	}
}

// 关闭后焦点回到打开之前持有它的控件。
func TestModalReturnsFocusToOpener(t *testing.T) {
	h, _ := openModalPage(t, still(), 800, 600, nil)
	openModal(h)
	h.keys("Escape")
	if n, ok := h.node(gfx.RoleButton, "打开"); !ok || !n.Focused {
		t.Errorf("关闭后焦点应回到「打开」按钮：存在=%v focused=%v", ok, n.Focused)
	}
}

// 面板高度随内容增长，最高 85vh；超出的部分在内容区里滚动。
func TestModalHeightGrowsWithContentUpToViewportLimit(t *testing.T) {
	short, _ := openModalPage(t, still(), 800, 600, nil)
	openModal(short)
	sp, _ := short.node(gfx.RoleGroup, modalTitle)
	// 短内容：页眉 69（16 + 36 + 16 + 1）加 1px 上下边框加正文（p-6 内一枚 32 高的按钮）= 69 + 2 + 24 + 32 + 24。
	if want := float32(69 + 2 + 24 + 32 + 24); math.Abs(float64(sp.Bounds.Height-want)) > 0.51 {
		t.Errorf("短内容的面板高 %g，期望 %g", sp.Bounds.Height, want)
	}

	long, _ := openModalPage(t, still(), 800, 600, func(p *modalPage) { p.long = true })
	openModal(long)
	long.frames(3)
	lp, _ := long.node(gfx.RoleGroup, modalTitle)
	if want := float32(600 * 0.85); math.Abs(float64(lp.Bounds.Height-want)) > 0.51 {
		t.Errorf("长内容的面板高 %g，期望 85vh = %g", lp.Bounds.Height, want)
	}
	// 滚到底之后最后一行可见。
	if err := long.session.Scroll(gfx.ScrollEvent{X: 400, Y: 300, DeltaY: 5000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	long.frames(3)
	if !long.session.HasText("正文第 39 行") {
		t.Error("滚到底之后应能看到最后一行")
	}
}

// 窗口窄于最大宽度加 p-4 时，面板宽 = 窗口宽 − 32。
func TestModalNarrowWindow(t *testing.T) {
	h, _ := openModalPage(t, still(), 360, 600, nil)
	openModal(h)
	p, _ := h.node(gfx.RoleGroup, modalTitle)
	if math.Abs(float64(p.Bounds.Width-328)) > 0.01 {
		t.Errorf("面板宽 %g，期望 328（360 − 2×16）", p.Bounds.Width)
	}
}

// 尺寸档：xl 宽 800。
func TestModalSizes(t *testing.T) {
	cases := []struct {
		size ModalSize
		want float32
	}{{ModalSM, 400}, {ModalMD, 500}, {ModalLG, 600}, {ModalXL, 800}, {Modal2XL, 1000}, {ModalFull, 1200}}
	for _, tc := range cases {
		h, _ := openModalPage(t, still(), 1400, 900, func(p *modalPage) { p.size = tc.size })
		openModal(h)
		p, _ := h.node(gfx.RoleGroup, modalTitle)
		if math.Abs(float64(p.Bounds.Width-tc.want)) > 0.01 {
			t.Errorf("尺寸档 %d：面板宽 %g，期望 %g", tc.size, p.Bounds.Width, tc.want)
		}
	}
}

// 全屏：中号字（根字号 16px）下四周各留 64px。
func TestModalFullscreen(t *testing.T) {
	h, _ := openModalPage(t, still(), 1000, 700, func(p *modalPage) { p.size = ModalFullscreen })
	openModal(h)
	p, _ := h.node(gfx.RoleGroup, modalTitle)
	want := gfx.Rect{X: 64, Y: 64, Width: 1000 - 128, Height: 700 - 128}
	if p.Bounds != want {
		t.Errorf("全屏面板 %+v，期望 %+v", p.Bounds, want)
	}
}

// 全屏随字号档：面板的最大宽高是 px（100vw-128、100vh-128），外层 p-16 是 rem（56/64/72）。
// 渲染层实测（1280×800）：小号 [64 64 1152 672]、中号 [64 64 1152 672]、大号 [72 72 1136 672]——大号宽被内容区夹窄，高不夹、顶边贴内容区上沿。
func TestModalFullscreenFollowsFontTiers(t *testing.T) {
	cases := []struct {
		tier string
		want gfx.Rect
	}{
		{"small", gfx.Rect{X: 64, Y: 64, Width: 1152, Height: 672}},
		{"medium", gfx.Rect{X: 64, Y: 64, Width: 1152, Height: 672}},
		{"large", gfx.Rect{X: 72, Y: 72, Width: 1136, Height: 672}},
	}
	for _, tc := range cases {
		spec := still()
		spec.FontSize = tc.tier
		h, _ := openModalPage(t, spec, 1280, 800, func(p *modalPage) { p.size = ModalFullscreen })
		openModal(h)
		p, _ := h.node(gfx.RoleGroup, modalTitle)
		if p.Bounds != tc.want {
			t.Errorf("%s 档全屏面板 %+v，期望 %+v", tc.tier, p.Bounds, tc.want)
		}
	}
}

// 入场：淡入 + 从 96% 放大，180 ms；关闭档直接到终态。
func TestModalEnterAnimation(t *testing.T) {
	spec := DefaultSpec()
	spec.Motion = MotionLevelStandard
	h, _ := openModalPage(t, spec, 800, 600, nil)
	if err := h.session.ClickLabel("打开"); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	// 面板表面是宽度在 400 到窗口宽之间、高度不小于 100 的最小矩形图元（遮罩更宽，阴影更大；可访问树里的外框不含入场变换）。
	width := func() float32 {
		var w float32
		for _, pr := range h.session.Snapshot().Primitives {
			if pr.Bounds.Width >= 400 && pr.Bounds.Width < 790 && pr.Bounds.Height >= 100 && (w == 0 || pr.Bounds.Width < w) {
				w = pr.Bounds.Width
			}
		}
		if w == 0 {
			t.Fatal("没有找到面板表面")
		}
		return w
	}
	if w := width(); w > 500*modalEnterScale+1 {
		t.Errorf("入场起点面板宽 %g，期望约 %g（96%%）", w, 500*modalEnterScale)
	}
	advance(h, 90*time.Millisecond)
	mid := width()
	if mid <= 500*modalEnterScale || mid >= 500 {
		t.Errorf("入场中途面板宽 %g，应在 %g 与 500 之间", mid, 500*modalEnterScale)
	}
	advance(h, 120*time.Millisecond)
	if w := width(); math.Abs(float64(w-500)) > 0.01 {
		t.Errorf("入场结束面板宽 %g，期望 500", w)
	}
}
