package kit

import (
	"strconv"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 上下文菜单的子菜单（渲染层 ContextMenu 的 children，台账 B-023 (1)）
//
// 渲染层里每个根项外面是一个 div.relative，里面是 button 与（展开时）绝对定位的子菜单外层：
//
//	div.relative            ← onMouseEnter / onMouseLeave：进入带子菜单的项就打开，进入别的项立刻关闭，离开带子菜单的项排 140 ms 后关闭
//	  button[role=menuitem]
//	  div.absolute.top-0.z-[100000] (left-full ml-1 -translate-x-2 pl-2 | right-full mr-1 translate-x-2 pr-2)
//	                         ← 悬停桥：比面板向父项一侧多出 0.5rem，自己也有 enter（取消排队中的关闭）/ leave（再排 140 ms）
//	    div.min-w-[220px].rounded-md.border.bg-popover.py-1.shadow-lg  ← 子面板，子项是 px-3 py-2
//
// 子菜单是 DOM 后代，所以鼠标从父项滑进悬停桥或子面板并没有离开 div.relative；从子面板回到父项按钮倒是离开了悬停桥，
// 排的 140 ms 关闭没有人取消，子菜单随后关闭（渲染层的既有行为，探针实测，照做）。
//
// Viva 里的对应：
//   - 整个菜单仍是一个 Overlay。带子菜单的菜单里，每个根项外面包一个 CustomLayout（menuWrapLayout，对应 div.relative），
//     子面板是它的第二个孩子，被版面摆到根面板外面。命中区、可访问树、FocusGroup 的成员都按绘制顺序登记，所以
//     子项紧跟在父项后面——与渲染层 DOM 顺序一致，↑ ↓ Home End 因而由 FocusGroup 原样给出渲染层的循环（子菜单展开时含子项）。
//   - 悬停桥是一块不可聚焦的命中区：面板本体那一块在子面板里登记（压在子项之下），向父项一侧伸出的那一条在根面板绘制收尾时登记，
//     这样它压在后面的根项之上（渲染层里桥的 z-index 高于所有根项）。
//   - 鼠标进出由每帧的指针位置推出：先离开、后进入，与浏览器同一次移动里 mouseleave 先于 mouseenter 一致。

const (
	submenuCloseDelay          = 140 * time.Millisecond // scheduleSubmenuClose 的延迟
	submenuMinWidth    float32 = 220                    // min-w-[220px]（border-box）
	submenuEstWidth    float32 = 256                    // handleOpenSubmenu 的估算宽，固定 px，不随字号缩放
	submenuEstHeight   float32 = 280                    // 同上，估算高
	submenuTopDefault  float32 = -4                     // style.top 默认 -4px：子面板比父项行高 4px 起
	submenuViewportPad float32 = 8                      // window.innerWidth - 8 与 8 - rect.top 里的 8
)

// menuSub 是子菜单的状态（渲染层的 openSubmenu、closeSubmenuTimerRef 与指针所在位置）。
type menuSub struct {
	open   bool
	parent int     // 父项在 Items 里的序号
	left   bool    // 向左展开（direction === 'left'）
	top    float32 // 外层相对父项行顶的偏移（px，可为负）

	// 渲染层只有一个定时器：closeAt 非零就是排队中的关闭，closeFor 是它要关的那一项
	// （到点时只关「仍然是这一项」的子菜单）。
	closeAt  time.Duration
	closeFor int

	// 上一帧指针落在哪个根项里（序号 + 1；0 表示都不在，含它的子菜单）以及是否在它的子菜单里：
	// 与这一帧比较，推出 mouseenter / mouseleave。
	hoverRow1 int
	inSub     bool

	// focusFirst：键盘（→）打开了子菜单，子面板出现时把键盘交给第一个未禁用的子项。
	focusFirst bool
}

// menuHits 是上一帧登记的命中区身份：用来把这一帧的悬停目标归到「哪个根项、在不在子菜单里」。
// 身份只取决于序号，打开期间不变；每帧声明时重建。
type menuHits struct {
	rows    []gfx.ID // 根项按钮（含禁用项）
	wraps   []gfx.ID // 根项外层 div.relative 的命中区：禁用的按钮 pointer-events-none，指针落在它外层上
	subWrap gfx.ID   // 子菜单外层（悬停桥与面板本体）
	kids    []gfx.ID // 子项（含禁用项）
}

func (h *menuHits) begin() {
	h.rows, h.wraps, h.kids = h.rows[:0], h.wraps[:0], h.kids[:0]
	h.subWrap = gfx.ID{}
}

// locate 回答指针此刻在哪个根项（含它的子菜单）里、是否在子菜单里；row 为 -1 表示不在任何一项里。
func (h *menuHits) locate(hovered gfx.ID, parent int) (row int, inSub bool) {
	if hovered.IsZero() {
		return -1, false
	}
	for i, id := range h.rows {
		if id == hovered {
			return i, false
		}
	}
	for i, id := range h.wraps {
		if id == hovered {
			return i, false
		}
	}
	if hovered == h.subWrap {
		return parent, true
	}
	for _, id := range h.kids {
		if id == hovered {
			return parent, true
		}
	}
	return -1, false
}

// menuHasSub 报告菜单里有没有带子菜单的项。
func menuHasSub(items []MenuItem) bool {
	for i := range items {
		if len(items[i].Children) > 0 {
			return true
		}
	}
	return false
}

// submenuPlacement 是 handleOpenSubmenu 里的方向与竖直偏移：item 是父项外层（div.relative）的外框，
// vw、vh 是视口。估算的宽 256、高 280 是渲染层写死的常数，子菜单比 280 高时会超出视口底部——照做。
func submenuPlacement(item gfx.Rect, vw, vh float32) (left bool, top float32) {
	left = item.X+item.Width+submenuEstWidth > vw-submenuViewportPad
	top = submenuTopDefault
	if available := vh - item.Y - submenuEstHeight - submenuViewportPad; available < submenuTopDefault {
		top = available
	}
	return left, max(submenuViewportPad-item.Y, top)
}

// subMenuSize 是子面板的宽高：宽是最宽一项的内容宽加边框（不小于 220），高是各子项（px-3 py-2）加 py-1 与边框。
func (l *Look) subMenuSize(canvas *gfx.Canvas, children []MenuItem) (width, height float32) {
	height = 2 + 2*l.Space(1)
	width = submenuMinWidth
	for i := range children {
		w, h := l.menuItemSize(canvas, children[i], l.Space(2), false)
		width = max(width, w+2)
		height += h
	}
	return width, height
}

// menuItemRect 是第 index 个根项外层（div.relative）的外框：贴着面板边框内侧，宽 = 面板宽 − 2，
// 竖向从边框 + py-1 起累加各项的高。
func (l *Look) menuItemRect(canvas *gfx.Canvas, items []MenuItem, root gfx.Rect, index int) gfx.Rect {
	y := root.Y + 1 + l.Space(1)
	for i := 0; i < index; i++ {
		_, h := l.menuItemSize(canvas, items[i], l.Space(1.5), len(items[i].Children) > 0)
		y += h
	}
	_, h := l.menuItemSize(canvas, items[index], l.Space(1.5), len(items[index].Children) > 0)
	return gfx.Rect{X: root.X + 1, Y: y, Width: root.Width - 2, Height: h}
}

// openSub 打开第 index 项的子菜单（handleOpenSubmenu）：取消排队中的关闭，按父项的位置定方向与竖直偏移。
func (l *Look) openSub(canvas *gfx.Canvas, st *MenuState, items []MenuItem, root gfx.Rect, index int) {
	s := &st.sub
	s.closeAt = 0
	left, top := submenuPlacement(l.menuItemRect(canvas, items, root, index), st.viewW, st.viewH)
	s.open, s.parent, s.left, s.top = true, index, left, top
}

// scheduleClose 排一个 140 ms 后的关闭（scheduleSubmenuClose）：只有一个定时器，重排即重新计时。
func (s *menuSub) scheduleClose(canvas *gfx.Canvas, index int) {
	s.closeAt, s.closeFor = canvas.Now()+submenuCloseDelay, index
	canvas.RequestFrameIn(submenuCloseDelay)
}

// subStep 处理这一帧的定时器与指针进出，在声明菜单之前调用。
func (l *Look) subStep(canvas *gfx.Canvas, st *MenuState, items []MenuItem, root gfx.Rect) {
	s := &st.sub

	// 定时器到点：只关「仍然是排队时那一项」的子菜单（setOpenSubmenu 的函数式更新）。
	if s.closeAt != 0 {
		if now := canvas.Now(); now >= s.closeAt {
			if s.open && s.parent == s.closeFor {
				s.open = false
			}
			s.closeAt = 0
		} else {
			canvas.RequestFrameIn(s.closeAt - now)
		}
	}

	row, inSub := st.hits.locate(canvas.Hovered(), s.parent)
	prevRow, prevInSub := s.hoverRow1-1, s.inSub
	s.hoverRow1, s.inSub = row+1, inSub

	hasChildren := func(i int) bool { return i >= 0 && i < len(items) && len(items[i].Children) > 0 }

	// 先离开：离开带子菜单的项，或离开子菜单本身（含从子菜单回到它的父项按钮）都排关闭。
	leaveRow := prevRow >= 0 && prevRow != row
	leaveSub := prevInSub && !(inSub && row == prevRow)
	if (leaveRow && hasChildren(prevRow)) || leaveSub {
		s.scheduleClose(canvas, prevRow)
	}
	// 再进入：进入带子菜单的项就打开；进入别的项立刻关闭（含禁用项外层，渲染层的 div 不看 disabled）；进入子菜单取消关闭。
	if row >= 0 && row != prevRow {
		if hasChildren(row) {
			l.openSub(canvas, st, items, root, row)
		} else {
			s.open = false
		}
	}
	if inSub && !(prevInSub && row == prevRow) {
		s.closeAt = 0
	}
}

// subKeys 处理 → 与 ←（渲染层 document keydown 里的 ArrowRight / ArrowLeft）。↑ ↓ Home End 交给 FocusGroup。
//
//   - → 落在带子菜单的根项上：打开子菜单并把键盘交给第一个未禁用的子项；落在子项或没有子菜单的项上没有作用；
//   - ← 落在子项上：关闭子菜单并聚焦父项；落在根项上没有作用。
//
// 键盘所在的行由上一帧登记的命中区身份判断。
func (l *Look) subKeys(canvas *gfx.Canvas, st *MenuState, items []MenuItem, root gfx.Rect, right, left bool) {
	focused := canvas.Focused()
	if focused.IsZero() {
		return
	}
	if right {
		for i, id := range st.hits.rows {
			if id == focused && i < len(items) && len(items[i].Children) > 0 {
				l.openSub(canvas, st, items, root, i)
				st.sub.focusFirst = true
				return
			}
		}
	}
	if left {
		for _, id := range st.hits.kids {
			if id == focused {
				st.sub.open = false
				st.sub.closeAt = 0
				if p := st.sub.parent; p < len(st.hits.rows) {
					canvas.RequestFocus(st.hits.rows[p])
				}
				return
			}
		}
	}
}

// menuWrapLayout 是根项外层 div.relative 的版面（只在带子菜单的菜单里用）：
// 孩子 0 是项按钮，撑满外层；孩子 1（展开时才有）是子面板，摆在外层外面——
// 向右展开时面板左缘 = 外层右缘 + ml-1，向左展开时面板右缘 = 外层左缘 − mr-1
// （悬停桥的 −translate-x-2 与 pl-2 在面板位置上互相抵消，只多出一条命中区）；
// 竖直方向面板上缘 = 外层上缘 + top（handleOpenSubmenu 算出的偏移）。
type menuWrapLayout struct {
	left           bool
	top            float32
	gap            float32 // ml-1 / mr-1
	panelW, panelH float32
}

func (m *menuWrapLayout) MeasureWidth(ctx *ui.LayoutContext) float32 { return ctx.Width(0) }

func (m *menuWrapLayout) PlaceWidth(ctx *ui.LayoutContext, bounds ui.Rect) {
	ctx.SetX(0, bounds.X)
	ctx.SetWidth(0, bounds.Width)
	if ctx.Count() > 1 {
		x := bounds.X + bounds.Width + m.gap
		if m.left {
			x = bounds.X - m.gap - m.panelW
		}
		ctx.SetX(1, x)
		ctx.SetWidth(1, m.panelW)
	}
}

func (m *menuWrapLayout) MeasureHeight(ctx *ui.LayoutContext) float32 { return ctx.Height(0) }

func (m *menuWrapLayout) PlaceHeight(ctx *ui.LayoutContext, bounds ui.Rect) {
	ctx.SetY(0, bounds.Y)
	ctx.SetHeight(0, bounds.Height)
	if ctx.Count() > 1 {
		ctx.SetY(1, bounds.Y+m.top)
		ctx.SetHeight(1, m.panelH)
	}
}

// bridgeStrip 是悬停桥里向父项一侧伸出的那一条（panel 是子面板的外框）：宽 pl-2 / pr-2 = 0.5rem。
func (l *Look) bridgeStrip(panel gfx.Rect, left bool) gfx.Rect {
	w := l.Space(2)
	if left {
		return gfx.Rect{X: panel.X + panel.Width, Y: panel.Y, Width: w, Height: panel.Height}
	}
	return gfx.Rect{X: panel.X - w, Y: panel.Y, Width: w, Height: panel.Height}
}

// subPanel 声明展开的子菜单面板（menuWrapLayout 的孩子 1）：画表面、登记面板本体的悬停桥命中区，再声明各子项。
// 返回这一帧被点中的子项（没有则 nil）。
func (l *Look) subPanel(c ui.C, st *MenuState, children []MenuItem) *MenuItem {
	padY := l.Space(1)
	surface := l.menuSurface()
	subWrap := ui.ID(c.Key("sub-wrap"))
	st.hits.subWrap = subWrap

	var chosen *MenuItem
	var firstID gfx.ID
	box := ui.Column(c.Key("sub")).
		PadSides(1+padY, 1, 1+padY, 1).
		Draw(func(d *ui.DrawContext) {
			st.subRect = d.Rect
			l.PaintSurface(d, surface)
			// 面板本体也算子菜单外层的一部分：压在子项之下，子项登记在它之后。
			d.Canvas.HitWithOptions(subWrap, d.Rect, "", gfx.HitOptions{SkipFocus: true})
		})
	box.Children(func() {
		for j := range children {
			child := &children[j]
			id, clicked := l.menuRow(c.Key("kid"+strconv.Itoa(j)), *child, menuRowSpec{padY: l.Space(2)})
			st.hits.kids = append(st.hits.kids, id)
			if child.Disabled {
				continue
			}
			if firstID.IsZero() {
				firstID = id
			}
			if clicked && chosen == nil {
				chosen = child
			}
		}
	})
	if st.sub.focusFirst {
		st.sub.focusFirst = false
		if !firstID.IsZero() {
			c.Canvas().RequestFocus(firstID)
		}
	}
	return chosen
}
