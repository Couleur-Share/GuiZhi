package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// IconFilled 画「描边加填充」的图标：渲染层给 lucide 图标加 fill-* 类（星标条目的 fill-amber-400 text-amber-400），
// SVG 先按 fill 填满闭合路径、再按 stroke 描边，两者同色。
//
// Viva 的图标只描边；Stroke 为 0 时按填充画，所以先用同一条路径的填充版垫底，再画原本的描边。
// 颜色不透明时两层叠在一起与 SVG 的结果一致；带透明度的颜色会在重叠处加深，这里只用于不透明的强调色。
func (l *Look) IconFilled(c ui.C, ic gfx.Icon, size float32, color tokens.Color) ui.Box {
	col := l.Col(color)
	solid := ic
	solid.Stroke = 0
	return ui.Slot(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
		mark := l.markIcon(d)
		tint := d.Fade(col)
		PaintIcon(d.Canvas, solid, d.Rect, tint)
		PaintIcon(d.Canvas, ic, d.Rect, tint)
		l.reportIcon(d, mark)
	})
}
