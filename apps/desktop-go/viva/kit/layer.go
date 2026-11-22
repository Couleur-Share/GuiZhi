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
// 永远在它们之上；见 ui/layers.go），LayerPaneChrome(1) 与 LayerDrag(2) 留给 Viva 自己的约定，这里不用：
// 3…9 七层已经用满，再要新层得先合并或复用其中一层。
const (
	bandModal         ui.Band = 3 // Modal：背景与面板在同一层，面板在背景之后声明
	bandSelectDismiss ui.Band = 4 // Select 的整窗背景：高于 Modal，下拉才能开在对话框里
	bandSelectPanel   ui.Band = 5
	// bandToastUnder 是 Toast 卡片「背后」的垫层：渲染层卡片是 backdrop-blur 的半透明底色，Viva 没有模糊原语（A-007），
	// 深色下的卡片底色只有 20% 不透明度，要垫一块不透明的页面底色才不会透出背后的文字。垫层必须在所有卡片的阴影之下
	// （同一层里先画全部形状再画文字，所以不能与卡片同层，否则上一张卡片的阴影会被下一张的垫层盖掉）。
	bandToastUnder ui.Band = 6
	bandToast      ui.Band = 7
	bandConfirm    ui.Band = 8 // ConfirmDialog：背景与面板在同一层
	bandTooltip    ui.Band = 9
)

// 盖在原生视图（阅读岛）之上的非模态控件用的层号：原生视图只在声明它的那一层上「开洞」，同层与更低层画的东西都被它盖住，
// 所以阅读区上方的悬浮控件（查找条、退出专注钮）要抬到更高的一层。本应用没有用 Viva 的 LayerPaneChrome（1），这里借它：
//   - BandOverIsland：详情在页面层（卡片视图、专注阅读）时用；低于 Modal（3），模态遮罩与对话框仍然盖得住它；
//   - BandOverIslandInModal：详情在 Modal 里（表格视图的详情浮层）时用；原生视图在 Modal 的层上，所以要比它高一层。
const (
	BandOverIsland        ui.Band = 1
	BandOverIslandInModal         = bandSelectDismiss
)

// BandViewer 是图片查看器（整窗浮层，渲染层的 ImageLightbox）用的层号：渲染层里它的 z-index 与 Modal、Select 同为 9999 但后进 DOM，
// 所以盖在它们之上，又在 Toast 与 ConfirmDialog（99999）、Tooltip（100000）之下。原生视图（查看岛）只在声明它的这一层上「开洞」，
// 所以它与 Select 的面板共用第 5 层（打开查看器时不会有 Select 面板；有也被它盖住，与渲染层一致），
// 6…9 层的提示、确认与悬停说明仍然盖得住它。
const BandViewer = bandSelectPanel

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
	// ByKeyboard 非空：关闭这一帧是由键盘（Esc）引起时写入 true，点背景引起时写入 false。
	// 取色器据此决定要不要把焦点还给触发按钮（渲染层只在 Esc 时 triggerRef.focus()）。
	ByKeyboard *bool
	// InModal 为真：背景层的命中区归入打开着的 Modal 的焦点作用域。Modal 把指针与焦点限制在自己的作用域里，
	// 声明在 Modal 里的浮层按层号画在页面树之后、已在作用域之外，不借用就够不着（Select 开在对话框里需要它）。
	InModal bool
}

// fadePresence 让一个浮层淡入淡出：入场用 enter，入场播完之后换成 exit，撤掉时按它回放（Modal 的遮罩）。
func fadePresence(canvas *gfx.Canvas, id gfx.ID, enter, exit gfx.Motion, pivot gfx.Point) {
	motion := exit
	if _, playing := canvas.PlayOnce(id, 0, true, enter.Duration); playing {
		motion = enter
	}
	canvas.BeginPresence(id, 0, gfx.Presence{Motion: motion, Fade: true}, pivot)
}

// dismissLayer 声明整窗的背景层并返回这一帧是否被要求关闭（点了背景，或按了 Esc）。
func (l *Look) dismissLayer(c ui.C, s dismissSpec) bool {
	id := ui.ID(c)
	var popScope func()
	backdrop := gfx.HitOptions{
		Role: gfx.RoleButton, SkipFocus: true, Backdrop: true,
		PassPrimary: s.PassPress, PassSecondary: s.PassPress,
	}
	// 盒子自带的命中在 Draw 钩子之前登记，进不了 Modal 的焦点作用域；在 Modal 里时改在钩子里（作用域之内）登记。
	inModal := s.InModal && !l.modalScope.IsZero()
	layer := ui.Overlay(c, ui.OverlaySpec{Band: s.Band, Bounds: c.Bounds()})
	if !inModal {
		layer = layer.HitWith(id, "", backdrop)
	}
	layer = layer.
		Draw(func(d *ui.DrawContext) {
			if inModal {
				popScope = l.pushModalScope(d.Canvas)
				d.Canvas.HitWithOptions(id, d.Rect, "", backdrop)
			}
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
		}).
		DrawOver(func(d *ui.DrawContext) {
			if popScope != nil {
				popScope()
				popScope = nil
			}
		})
	clicked := false
	if inModal {
		clicked = c.Canvas().Clicked(id) // 没有盒子自带的命中，按身份读
	} else {
		clicked = layer.Clicked()
	}
	if clicked && s.ByKeyboard != nil {
		*s.ByKeyboard = c.Canvas().ClickedByKey(id)
	}
	return clicked
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
	enterShape(canvas, id, slot, motion, gfx.Presence{Fade: true, Scale: scale}, pivot)
}

// enterShape 是 enterPresence 的一般形：shape 给出入场的形状（淡入、缩放、滑入），时长与曲线取 motion。
func enterShape(canvas *gfx.Canvas, id gfx.ID, slot uint8, motion gfx.Motion, shape gfx.Presence, pivot gfx.Point) {
	if motion.Duration <= 0 {
		canvas.BeginPresence(id, slot, gfx.Presence{}, pivot)
		return
	}
	if _, playing := canvas.PlayOnce(id, slot, true, motion.Duration); !playing {
		motion.Duration = time.Nanosecond
	}
	shape.Motion = motion
	canvas.BeginPresence(id, slot, shape, pivot)
}

// EnterSlide 给页面里的一块内容加入场过渡：渲染层的 nimate-in fade-in slide-in-from-bottom-N，
// 即淡入的同时从下方 fromY 处滑回原位（tailwindcss-animate 的 enter 关键帧：版面不动，只是视觉变换，命中区随绘制一起挪）。
// 内容第一次被绘制时播放；按 box 的 Draw 钩子画 draw，并在 DrawOver 里收尾。没有退场：内容撤掉即消失。
// id 须唯一且在内容存在期间稳定；motion 通常是 l.Motion(DurationBase, EaseDefault)（animation-timing-function 默认 ease）。
func (l *Look) EnterSlide(box ui.Box, id gfx.ID, fromY float32, motion gfx.Motion, draw func(d *ui.DrawContext)) ui.Box {
	return box.Draw(func(d *ui.DrawContext) {
		enterShape(d.Canvas, id, 0, motion, gfx.Presence{Fade: true, SlideIn: gfx.Point{Y: fromY}}, center(d.Rect))
		draw(d)
	}).DrawOver(func(d *ui.DrawContext) { d.Canvas.EndPresence() })
}

// center 是矩形的中心点。
func center(r gfx.Rect) gfx.Point {
	return gfx.Point{X: r.X + r.Width/2, Y: r.Y + r.Height/2}
}
