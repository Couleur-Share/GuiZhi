package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// ShortcutRow 是快捷键设置行的外壳（渲染层 ShortcutItem 的最外层
// `flex items-center justify-between px-4 py-5 border-b border-border/50 last:border-0 hover:bg-muted/10`）：
// 与 SettingItem 的差别是内边距 py-5、分隔线 border-border/50、悬停 bg-muted/10，左侧 `flex-1 min-w-0 mr-8`，
// 右侧 `flex items-center gap-4 flex-shrink-0`。left 与 right 分别声明两侧内容。
func (l *Look) ShortcutRow(c ui.C, last bool, left, right func()) {
	t := l.T
	id := ui.ID(c)
	pad := [4]float32{l.Space(5), l.Space(4), l.Space(5), l.Space(4)} // py-5 px-4
	if !last {
		pad[2]++ // border-b 占 1px，画在行盒的最底部
	}
	ui.Row(c).FillWidth().
		Items(ui.Center).
		Justify(ui.Between).
		PadSides(pad[0], pad[1], pad[2], pad[3]).
		HitWith(id, "", gfx.HitOptions{SkipFocus: true}).
		Draw(func(d *ui.DrawContext) {
			p := l.hoverTo(d.Canvas, id, 0, l.hoverInside(d.Canvas, id), DurationDefault)
			if p > 0 {
				d.Canvas.Fill(d.Rect, d.Fade(l.Col(t.Muted.WithAlpha(0.1*p))))
			}
		}).
		DrawOver(func(d *ui.DrawContext) {
			l.noteInside(d.Canvas, id, d.Rect)
			if last {
				return
			}
			line := gfx.Rect{X: d.Rect.X, Y: d.Rect.Y + d.Rect.Height - 1, Width: d.Rect.Width, Height: 1}
			d.Canvas.Fill(line, d.Fade(l.Col(t.Border.WithAlpha(0.5))))
		}).
		Children(func() {
			ui.Column(c.Key("left")).Grow(1).Shrink(1).Basis(0).MinWidth(0).MarginRight(l.Rem(2)).Children(left) // flex-1 min-w-0 mr-8
			ui.Row(c.Key("right")).Shrink(0).Items(ui.Center).Gap(l.Rem(1)).Children(right)                      // gap-4 flex-shrink-0
		})
}
