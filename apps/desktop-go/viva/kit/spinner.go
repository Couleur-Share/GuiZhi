package kit

import (
	"math"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// spinPeriod 是 Tailwind 的 animate-spin：1 秒一圈，匀速，无限循环。
const spinPeriod = time.Second

// Spinner 画一枚旋转的加载图标（渲染层的 Loader2 + animate-spin）。size 是边长，color 是令牌颜色。
//
// 动效「关闭」档下不转：globals.css 对关闭档把所有动画压成 0.01ms，无限循环的旋转在那里没有可看的中间帧；
// 静止的图标是它唯一合理的读法。其余档位匀速转动，每帧都请求重绘（只在加载指示器可见期间）。
// 与渲染层一致，图标本身仍走 Look.Icon 的外框登记（IconSink），对照工具按外框配对。
func (l *Look) Spinner(c ui.C, size float32, color tokens.Color) ui.Box {
	col := l.Col(color)
	animated := l.Spec.Motion.Animated()
	return ui.Slot(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
		if sink := l.IconSink; sink != nil {
			sink(d.Rect)
		}
		tint := d.Fade(col)
		if !animated || !d.Canvas.HasClock() {
			PaintIcon(d.Canvas, icons.LoaderCircle, d.Rect, tint)
			return
		}
		turns := float64(d.Canvas.Now()%spinPeriod) / float64(spinPeriod)
		d.Canvas.DrawIconTransformed(icons.LoaderCircle, d.Rect, tint, gfx.IconTransform{Rotate: float32(2 * math.Pi * turns)})
		d.Canvas.RequestFrame()
	})
}
