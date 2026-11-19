package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 表头的拖动把手（渲染层 ItemTableHeaderCell）：可调的列在右缘有一个 8px 宽的把手（absolute right-0 top-0 z-30 h-full w-2
// translate-x-1/2 cursor-col-resize），里面一条 2px 宽的圆头竖条（inset-y-1，居中）。
//
//   - 按下主键开始拖动，宽度 = 起点宽度 + 指针位移（夹在列的上下限里），拖动期间指针被捕获；松开时保存（按下后没动过不保存）；
//   - 把手可聚焦（role=separator，aria-orientation=vertical），←/→ 每次 16px 并立即保存；它没有聚焦环（focus:outline-none）；
//   - 竖条平时透明，悬停与键盘聚焦时 primary/70，拖动中 primary，颜色走 transition-colors duration-quick。

// handleKeyStep 是键盘调整列宽的步进（渲染层 KEYBOARD_STEP）。
const handleKeyStep = 16

// columnHandle 声明一列表头的拖动把手，绝对定位在单元格的右缘（越出单元格右缘 4px）。
//
// 键盘用 Box.Shortcut 注册，而具名的容器按「焦点落在容器里面」匹配，焦点就在容器自己身上时匹配不到（同 kit.ColumnResizer）；
// 所以绝对定位的外壳挂快捷键，把手本体（命中区、拖动、竖条）在里面拿焦点。
func (s *Screen) columnHandle(c ui.C, col tableColumn) {
	l := s.Look
	hc := c.Key("handle-" + string(col.ID))
	shell := ui.Column(hc).Absolute().Top(0).Bottom(0).Right(-l.Rem(0.25)).Width(l.Rem(0.5))
	if shell.Shortcut(0, "left") {
		s.setColumnWidth(col.ID, col.Width-handleKeyStep, true)
	}
	if shell.Shortcut(0, "right") {
		s.setColumnWidth(col.ID, col.Width+handleKeyStep, true)
	}
	shell.Children(func() { s.columnHandleBar(hc.Key("bar"), col) })
}

// columnHandleBar 是把手本体：占满外壳，带命中区、指针拖动与竖条的绘制。
func (s *Screen) columnHandleBar(c ui.C, col tableColumn) {
	l := s.Look
	t := l.T
	id := ui.ID(c)
	st := s.table.resizer(col.ID)
	name := locales.LibraryResizeColumn(col.Label())
	box := ui.Column(c).FillWidth().FillHeight().
		HitWith(id, name, gfx.HitOptions{
			Role: gfx.RoleSeparator, Cursor: gfx.PointerCursorResizeHorizontal, Hint: name,
			// 可聚焦的 separator 在浏览器的可访问树里带默认的取值范围（aria-valuenow 50、范围 0–100）。
			Range: gfx.ValueRange{Now: 50, Min: 0, Max: 100, Set: true},
		})
	state := box.State()
	hover := l.HoverProgress(c, id, 0, state.Hovered || st.dragging || state.FocusVisible, kit.DurationQuick)
	drag := l.HoverProgress(c, id, 1, st.dragging, kit.DurationQuick)
	bar := kit.MixToken(kit.MixToken(t.Primary.WithAlpha(0), t.Primary.WithAlpha(0.7), hover), t.Primary, drag)
	box.Draw(func(d *ui.DrawContext) {
		canvas := d.Canvas
		if ev, ok := canvas.PointerOn(id); ok {
			switch {
			case ev.Kind == gfx.PointerDown && ev.Button <= gfx.PointerButtonPrimary:
				st.dragging, st.moved, st.startX, st.startWidth = true, false, ev.X, col.Width
				canvas.Consume()
			case ev.Kind == gfx.PointerMove && st.dragging:
				st.moved = true
				s.setColumnWidth(col.ID, st.startWidth+ev.X-st.startX, false)
				canvas.Consume()
				canvas.RequestFrame()
			case ev.Kind == gfx.PointerUp && st.dragging:
				st.dragging = false
				// 点一下把手没有拖动：不保存（否则默认列宽被写成偏好，之后默认值再变也不会生效）。
				s.setColumnWidth(col.ID, st.startWidth+ev.X-st.startX, st.moved)
				canvas.Consume()
				canvas.RequestFrame()
			}
		}
		// absolute inset-y-1 left-1/2 w-0.5 -translate-x-1/2 rounded-full
		rect := gfx.Rect{X: d.Rect.X + d.Rect.Width/2 - 1, Y: d.Rect.Y + l.Space(1), Width: 2, Height: d.Rect.Height - 2*l.Space(1)}
		rect = kit.SnapRect(rect, canvas.PixelScale())
		if rect.Width > 0 && rect.Height > 0 && bar.A > 0 {
			canvas.Rounded(rect, rect.Width/2, d.Fade(l.Col(bar)))
		}
	})
}
