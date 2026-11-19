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
//   - 按下主键开始拖动，宽度 = 起点宽度 + 指针位移（夹在列的上下限里），拖动期间指针被捕获；松开时保存；
//   - 把手可聚焦（role=separator，aria-orientation=vertical），←/→ 每次 16px 并立即保存；它没有聚焦环（focus:outline-none）；
//   - 竖条平时透明，悬停与键盘聚焦时 primary/70，拖动中 primary，颜色走 transition-colors duration-quick。

// handleKeyStep 是键盘调整列宽的步进（渲染层 KEYBOARD_STEP）。
const handleKeyStep = 16

// columnHandle 声明一列表头的拖动把手，绝对定位在单元格的右缘（越出单元格右缘 4px）。
func (s *Screen) columnHandle(c ui.C, col tableColumn) {
	l := s.Look
	t := l.T
	hc := c.Key("handle-" + string(col.ID))
	id := ui.ID(hc)
	st := s.table.resizer(col.ID)
	name := locales.LibraryResizeColumn(col.Label())
	box := ui.Column(hc).Absolute().Top(0).Bottom(0).Right(-l.Rem(0.25)).Width(l.Rem(0.5)).
		HitWith(id, name, gfx.HitOptions{
			Role: gfx.RoleSeparator, Cursor: gfx.PointerCursorResizeHorizontal, Hint: name,
			// 可聚焦的 separator 在浏览器的可访问树里带默认的取值范围（aria-valuenow 50、范围 0–100）。
			Range: gfx.ValueRange{Now: 50, Min: 0, Max: 100, Set: true},
		})
	if box.Shortcut(0, "left") {
		s.setColumnWidth(col.ID, col.Width-handleKeyStep, true)
	}
	if box.Shortcut(0, "right") {
		s.setColumnWidth(col.ID, col.Width+handleKeyStep, true)
	}
	state := box.State()
	hover := l.HoverProgress(hc, id, 0, state.Hovered || st.dragging || state.FocusVisible, kit.DurationQuick)
	drag := l.HoverProgress(hc, id, 1, st.dragging, kit.DurationQuick)
	bar := kit.MixToken(kit.MixToken(t.Primary.WithAlpha(0), t.Primary.WithAlpha(0.7), hover), t.Primary, drag)
	box.Draw(func(d *ui.DrawContext) {
		canvas := d.Canvas
		if ev, ok := canvas.PointerOn(id); ok {
			switch {
			case ev.Kind == gfx.PointerDown && ev.Button <= gfx.PointerButtonPrimary:
				st.dragging, st.startX, st.startWidth = true, ev.X, col.Width
				canvas.Consume()
			case ev.Kind == gfx.PointerMove && st.dragging:
				s.setColumnWidth(col.ID, st.startWidth+ev.X-st.startX, false)
				canvas.Consume()
				canvas.RequestFrame()
			case ev.Kind == gfx.PointerUp && st.dragging:
				st.dragging = false
				s.setColumnWidth(col.ID, st.startWidth+ev.X-st.startX, true)
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
