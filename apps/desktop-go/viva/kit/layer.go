package kit

import (
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 浮层基础设施
//
// 渲染层的浮层（Select 下拉、Modal、ConfirmDialog、Toast、Tooltip、ContextMenu）都是 portal 到 body 的 fixed 元素，
// 不占文档流。Viva 里对应的是 ui.Overlay：整窗坐标、不参与布局、画在页面树之后、不被所在容器裁剪。
//
// Viva 自带的 Popover / Dialog / Select 按它自己的外观与几何画（32px 的选项行、448px 的对话框、固定的内边距），
// 与渲染层对不上，而且几何是写死在包里的，没法改。所以这里只借用 Overlay 的分层与命中，外观与几何由 kit 自己画：
//   - 背景层：整窗的一块命中区，点一下关闭浮层（可选：这一下继续交给下面的控件），Esc 同样关闭；
//     模态对话框在它上面再铺一层遮罩色。
//   - 面板层：面板本身，层级高于背景层，所以面板里的点击不会落到背景上。
//
// 层级先后复刻渲染层的 z-index：Select 与 Modal 是 9999，Toast 与 ConfirmDialog 是 99999（ConfirmDialog 的 portal 后插入 DOM，
// 同级时在 Toast 之上），Tooltip 是 100000。Viva 的 Band 是 uint16，应用自己的浮层用 1…9（Viva 自带控件的层从 10 起，
// 永远在它们之上；见 ui/layers.go），LayerPaneChrome(1) 与 LayerDrag(2) 留给 Viva 自己的约定，这里不用。
const (
	bandModal         ui.Band = 3 // Modal：背景与面板在同一层，面板在背景之后声明
	bandSelectDismiss ui.Band = 4 // Select 的整窗背景：高于 Modal，下拉才能开在对话框里
	bandSelectPanel   ui.Band = 5
	bandToast         ui.Band = 6
	bandConfirm       ui.Band = 7 // ConfirmDialog：背景与面板在同一层
	bandTooltip       ui.Band = 8
)

// OverlayLayerMin 是浮层的最低层号：画在这一层及以上的文字属于浮层（对照工具据此区分浮层文字，见台账 A-009）。
const OverlayLayerMin = uint16(bandModal)

// dismissSpec 描述整窗的背景层。
type dismissSpec struct {
	Band ui.Band
	// Scrim 非空：先把整窗填成这个颜色（模态对话框的遮罩，bg-background/60）。
	Scrim *tokens.Color
	// PassPress 为真：点背景的这一下关闭浮层，并在浮层消失后交给下面的控件（非模态弹层的行为，Select 的「点外面关闭」）；
	// 为假：这一下只关闭，不再往下传（模态：背后的页面够不着）。
	PassPress bool
	// Keep 非空：这块区域（触发器）里的点击只关闭、不往下传——否则「点触发器收起」会被传给触发器再展开一次。
	Keep *gfx.Rect
	// NoEscape 为真：Esc 不关闭（确认对话框加载中：渲染层的键盘处理整个跳过）。点背景仍然关闭。
	NoEscape bool
}

// dismissLayer 声明整窗的背景层并返回这一帧是否被要求关闭（点了背景，或按了 Esc）。
func (l *Look) dismissLayer(c ui.C, s dismissSpec) bool {
	id := ui.ID(c)
	layer := ui.Overlay(c, ui.OverlaySpec{Band: s.Band, Bounds: c.Bounds()}).
		HitWith(id, "", gfx.HitOptions{
			Role: gfx.RoleButton, SkipFocus: true, Backdrop: true,
			PassPrimary: s.PassPress, PassSecondary: s.PassPress,
		}).
		Draw(func(d *ui.DrawContext) {
			if s.Scrim != nil {
				d.Canvas.Fill(d.Rect, d.Fade(l.Col(*s.Scrim)))
			}
			if s.Keep != nil && s.Keep.Width > 0 && s.Keep.Height > 0 {
				d.Canvas.HitWithOptions(id, *s.Keep, "", gfx.HitOptions{
					Role: gfx.RoleButton, SkipFocus: true, Backdrop: true, PassSecondary: s.PassPress,
				})
			}
			// Esc 作用于最后一个声明的目标：多个浮层同时存在时，最上面（最后声明）的先关。
			if !s.NoEscape {
				d.Canvas.SetEscapeTarget(id)
			}
		})
	return layer.Clicked()
}

// floatingPanel 声明面板层里的一个矩形：r 是页面坐标里的位置与大小。
// 返回的 Box 就是面板本身：调用方在它上面 Draw 表面、再 Children 声明内容。
func (l *Look) floatingPanel(c ui.C, band ui.Band, r gfx.Rect) ui.Box {
	return ui.Overlay(c, ui.OverlaySpec{Band: band, Bounds: r})
}

// enterPresence 在面板这一帧的绘制开头开启入场过渡（渲染层的 animate-in fade-in zoom-in-*），配套 d.Canvas.EndPresence()。
// scale 是入场起点的大小比例（zoom-in-95 是 0.95）；pivot 取面板中心（transform-origin 的默认值）。
//
// 渲染层的 Select 与 ConfirmDialog 只有入场、没有退场动画（关闭即卸载）。Viva 的入退场用同一条 Motion，
// 退场时按撤掉前最后一帧的快照回放；所以入场播完之后把 Motion 的时长改成最短的 1ns，
// 撤掉面板时退场立即结束（只多回放一帧，台账 A-008）。
func enterPresence(canvas *gfx.Canvas, id gfx.ID, slot uint8, motion gfx.Motion, scale float32, pivot gfx.Point) {
	if motion.Duration <= 0 {
		canvas.BeginPresence(id, slot, gfx.Presence{}, pivot)
		return
	}
	if _, playing := canvas.PlayOnce(id, slot, true, motion.Duration); !playing {
		motion.Duration = time.Nanosecond
	}
	canvas.BeginPresence(id, slot, gfx.Presence{Motion: motion, Fade: true, Scale: scale}, pivot)
}

// center 是矩形的中心点。
func center(r gfx.Rect) gfx.Point {
	return gfx.Point{X: r.X + r.Width/2, Y: r.Y + r.Height/2}
}
