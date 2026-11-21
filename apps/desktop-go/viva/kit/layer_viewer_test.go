package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 图片查看器的整窗浮层要盖在 Modal 与 Select 之上（渲染层里它们 z-index 同为 9999，查看器后进 DOM），
// 又在 Toast、ConfirmDialog、Tooltip 之下（99999 与 100000）：层号次序一变，原生视图（查看岛）的开洞关系就变了。
func TestBandViewerSitsBetweenSelectPanelsAndToasts(t *testing.T) {
	t.Parallel()
	if BandViewer <= bandModal || BandViewer <= bandSelectDismiss {
		t.Errorf("查看器要在 Modal 与 Select 背景之上：%d 对 %d、%d", BandViewer, bandModal, bandSelectDismiss)
	}
	for name, above := range map[string]uint16{"Toast 垫层": uint16(bandToastUnder), "Toast": uint16(bandToast), "ConfirmDialog": uint16(bandConfirm), "Tooltip": uint16(bandTooltip)} {
		if uint16(BandViewer) >= above {
			t.Errorf("查看器要在%s之下：%d 对 %d", name, BandViewer, above)
		}
	}
}

// hooksPage 是 WithinModalHooks 的试验页：一个带「打开」钮的页面、一个 Modal，加一块 BandViewer 层上的浮层，
// 浮层在 draw 钩子里登记一个命中区（与查看器登记背景命中的方式相同），并数 draw / over 两个钩子各被调了几次。
type hooksPage struct {
	st                           ModalState
	open                         bool
	draws, overs, clicks, closed int
}

func (p *hooksPage) view(l *Look, c ui.C) {
	ui.Column(c).Fill().Pad(24).Children(func() {
		oc := c.Key("open")
		if ui.Row(oc).Size(120, 40).HitWith(ui.ID(oc), "打开", gfx.HitOptions{Role: gfx.RoleButton}).Clicked() {
			p.open = true
		}
	})
	spec := ModalSpec{Open: p.open, Title: "窗口", DialogLabel: "对话框", CloseLabel: "关闭", Body: func(ui.C) {}}
	if l.Modal(c, &p.st, spec) {
		p.closed++
		p.open = false
	}
	lc := c.Key("layer")
	id := ui.ID(lc)
	box := ui.Overlay(lc, ui.OverlaySpec{Band: BandViewer, Bounds: gfx.Rect{X: 300, Y: 480, Width: 120, Height: 40}})
	draw := func(d *ui.DrawContext) {
		p.draws++
		d.Canvas.HitWithOptions(id, d.Rect, "浮层按钮", gfx.HitOptions{Role: gfx.RoleButton})
	}
	over := func(*ui.DrawContext) { p.overs++ }
	l.WithinModalHooks(&p.st, box, draw, over).Children(func() {})
	if c.Canvas().Clicked(id) {
		p.clicks++
	}
	// 之后声明的、没有进作用域的浮层：Modal 开着时指针到不了它；作用域要在钩子结束时弹出，不能漏给后面的东西。
	oc := c.Key("outside")
	ui.Overlay(oc, ui.OverlaySpec{Band: BandViewer + 1, Bounds: gfx.Rect{X: 500, Y: 480, Width: 120, Height: 40}}).
		Draw(func(d *ui.DrawContext) {
			d.Canvas.HitWithOptions(ui.ID(oc), d.Rect, "外部浮层按钮", gfx.HitOptions{Role: gfx.RoleButton})
		}).Children(func() {})
}

// 没有 Modal 时（作用域为零）两个钩子照样挂上、照样跑，命中区照样点得到。
func TestWithinModalHooksRunWithoutAModal(t *testing.T) {
	p := &hooksPage{}
	h := openPage(t, still(), 800, 600, func(l *Look, c ui.C) { p.view(l, c) })
	h.frames(2)
	if p.draws == 0 || p.overs == 0 {
		t.Fatalf("没有 Modal 时钩子也要跑：draw=%d over=%d", p.draws, p.overs)
	}
	h.click("浮层按钮")
	if p.clicks != 1 {
		t.Fatalf("没有 Modal 时命中区要点得到：%d", p.clicks)
	}
	if !h.has(gfx.RoleButton, "外部浮层按钮") {
		t.Fatal("对照：没有 Modal 时，后声明的浮层的控件是可达的")
	}
}

// Modal 开着时：浮层声明在 Modal 之外，焦点陷阱本会把指针挡在作用域之外；经 WithinModalHooks 钩子里登记的命中区进了作用域，
// 点得到，并且这一下不会落到 Modal 的遮罩上把窗口关掉。
func TestWithinModalHooksPutTheLayerInsideTheModalScope(t *testing.T) {
	p := &hooksPage{}
	h := openPage(t, still(), 800, 600, func(l *Look, c ui.C) { p.view(l, c) })
	h.click("打开")
	if !p.open {
		t.Fatal("前提：Modal 应已打开")
	}
	draws, overs := p.draws, p.overs
	h.frames(2)
	if p.draws <= draws || p.overs <= overs {
		t.Fatalf("Modal 开着时钩子也要每帧都跑：draw %d→%d over %d→%d", draws, p.draws, overs, p.overs)
	}
	h.click("浮层按钮")
	if p.clicks != 1 {
		t.Fatalf("Modal 开着时浮层的命中区要点得到（进了它的焦点作用域）：%d", p.clicks)
	}
	if !p.open || p.closed != 0 {
		t.Fatalf("点浮层不能落到 Modal 的遮罩上：open=%v closed=%d", p.open, p.closed)
	}
	// 没进作用域的浮层仍被 Modal 的陷阱挡在外面（连控件列表里都没有）：钩子结束时要弹出作用域，不能漏给后面声明的东西。
	if h.has(gfx.RoleButton, "外部浮层按钮") {
		t.Fatal("作用域漏给了钩子之后声明的浮层（外部浮层按钮不该在 Modal 开着时可达）")
	}
}