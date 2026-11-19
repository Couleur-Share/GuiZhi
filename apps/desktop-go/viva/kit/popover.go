package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 非模态弹层（渲染层里 portal 到 body、fixed 定位的 role=dialog 面板，如「组合筛选」）
//
// 与 Select 的下拉面板同属一类：点面板外关闭（这一下继续交给下面的控件，点触发钮则只收起），Esc 关闭；
// 区别是面板里有任意内容（按钮、文字），高度由内容决定。几何取自渲染层：
//   - 锚点在展开那一刻取一次（触发钮的 left 与 bottom + 间距），之后布局变了面板也不跟随；
//   - 面板宽度固定（w-72），再夹到视口宽度减两侧留白（max-w-[calc(100vw-16px)]）；
//   - 左上角夹回视口：left ∈ [m, vw − 宽 − m]，top ∈ [m, vh − 高 − m]（高度是面板布局之后量到的）；
//   - 焦点离开面板与触发钮这一组时关闭（onBlur）。
//
// 列设置菜单（ColumnConfigMenu）是另一类：fixed + right 定位、不夹回视口，只在点外面与 Esc 时收起——
// 焦点离开、窗口缩放都不关。这一类用 PopoverSpec.Persistent 表达，右缘对齐用 AnchorRight，内边距只有上下用 Insets，
// 面板的可访问角色用 Role（menu）。
//
// 与渲染层的差异：面板的可访问角色 dialog 在 Viva 里只有 group（台账 A-004）；
// 面板内容的高度要量过才知道，首帧先透明画一次（不让一块矮面板闪一帧）。

// PopoverState 是一块非模态弹层的界面状态，由持有它的屏幕保存。展开与收起用 Show、Close（它们复位内部状态）。
type PopoverState struct {
	Open bool
	// Anchor 是展开那一刻取的面板左上角（页面坐标）。
	Anchor gfx.Point
	// Group 登记面板与触发钮里的命中区（Button 的 Groups 字段，或自己调 Add）：焦点离开整组就关闭面板。
	// 每帧在声明触发钮之前调用 Group.Begin。
	Group HoverGroups

	contentH float32    // 面板内容上一帧布局后的高度；0 表示还没量过
	inside   bool       // 上一帧焦点是否在组里
	viewport *gfx.Point // 展开时的视口尺寸：变了就关闭（渲染层的 resize 监听）
}

// Show 展开弹层；anchor 是展开那一刻取的面板左上角（触发钮的 left 与 bottom + 间距）。
func (st *PopoverState) Show(anchor gfx.Point) {
	st.Close()
	st.Open, st.Anchor = true, anchor
}

// Close 收起弹层并复位内部状态：下次展开重新量内容、重新记视口尺寸。
func (st *PopoverState) Close() {
	st.Open, st.contentH, st.inside, st.viewport = false, 0, false, nil
}

// PopoverGroup 是 PopoverState.Group 里登记用的组名：触发钮与面板里的控件都登记到它。
const PopoverGroup = "popover"

// PopoverSpec 描述一块弹层。
type PopoverSpec struct {
	Name    string  // 面板的可访问名（aria-label）
	Width   float32 // 面板宽度（w-72 = 18rem）
	Margin  float32 // 夹回视口的留白，也是 max-w 的左右留白（8）
	Pad     float32 // 内边距（p-3），不含 1px 边框
	Radius  float32
	Shadows []tokens.Shadow
	// Insets 是内边距的四边（上、右、下、左，不含 1px 边框）；nil 取四边都是 Pad。
	Insets *[4]float32
	// Role 是面板的可访问角色；零值取 group（渲染层的 dialog 在 Viva 里只有 group，台账 A-004）。
	Role gfx.Role
	// AnchorRight 让 Anchor.X 是面板的右缘（渲染层 fixed + right 定位），而不是左缘。
	AnchorRight bool
	// Persistent 让弹层只在点外面与 Esc 时收起：焦点离开、窗口缩放都不关，也不把面板夹回视口
	//（ColumnConfigMenu 没有 onBlur 与 resize 监听，位置是展开那一刻的 top/right）。
	Persistent bool
	// Trigger 是触发钮上一帧的外框：点它只关闭，不再往下传（否则收起会被传给触发钮、再展开一次）。
	Trigger gfx.Rect
	// Content 声明面板里的内容（一个竖排，宽度撑满面板的内容区）。
	Content func()
}

// Popover 声明整窗背景层与面板；返回这一帧是否被要求关闭，以及这次关闭是不是由键盘（Esc）引起的
// （渲染层只在 Esc 时把焦点还给触发钮，调用方据此决定）。关闭时已把 st.Open 置为假。
func (l *Look) Popover(c ui.C, st *PopoverState, spec PopoverSpec) (closed, byKeyboard bool) {
	t := l.T
	canvas := c.Canvas()
	bounds := c.Bounds()

	// 窗口尺寸变了：关闭（closeOnResize）。
	if st.viewport == nil {
		st.viewport = &gfx.Point{X: bounds.Width, Y: bounds.Height}
	} else if !spec.Persistent && (st.viewport.X != bounds.Width || st.viewport.Y != bounds.Height) {
		st.Close()
		return true, false
	}
	// 焦点曾在组里、现在不在了：关闭（onBlur 里 relatedTarget 既不在面板也不在触发钮）。
	in := st.Group.Focused(canvas) != ""
	if !spec.Persistent && st.inside && !in {
		st.Close()
		return true, false
	}
	st.inside = in

	var byKey bool
	if l.dismissLayer(c.Key("dismiss"), dismissSpec{Band: bandSelectDismiss, PassPress: true, Keep: &spec.Trigger, ByKeyboard: &byKey}) {
		st.Close()
		return true, byKey
	}

	inset := [4]float32{spec.Pad, spec.Pad, spec.Pad, spec.Pad}
	if spec.Insets != nil {
		inset = *spec.Insets
	}
	width := spec.Width
	if !spec.Persistent {
		width = min(width, bounds.Width-2*spec.Margin)
	}
	height := 2 + inset[0] + inset[2] + st.contentH
	left, top := st.Anchor.X, st.Anchor.Y
	if spec.AnchorRight {
		left -= width
	}
	if !spec.Persistent {
		left = max(spec.Margin, min(left, bounds.Width-width-spec.Margin))
		top = max(spec.Margin, min(top, bounds.Height-height-spec.Margin))
	}
	rect := gfx.Rect{X: left, Y: top, Width: width, Height: height}

	surface := Surface{Bg: t.Popover, Border: t.Border, BorderW: 1, Radius: spec.Radius, Shadows: spec.Shadows}
	role := spec.Role
	if role == 0 {
		role = gfx.RoleGroup
	}
	panelC := c.Key("panel")
	panel := l.floatingPanel(panelC, bandSelectPanel, rect).PadSides(1+inset[0], 1+inset[1], 1+inset[2], 1+inset[3])
	if st.contentH == 0 {
		panel = panel.Opacity(0)
	}
	panel.Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, surface)
		// 面板是不透明的：按在它的边距上不能落到下面的背景层（那会关闭面板）。
		d.Canvas.Block(d.Rect)
		d.Canvas.ContainerWithOptions(d.Rect, role, spec.Name, gfx.HitOptions{})
	}).Children(func() {
		ui.Column(panelC.Key("body")).FillWidth().Draw(func(d *ui.DrawContext) {
			if h := d.Rect.Height; h > 0 && abs32(h-st.contentH) > 0.01 {
				st.contentH = h
				d.Canvas.RequestFrame()
			}
		}).Children(spec.Content)
	})
	return false, false
}
