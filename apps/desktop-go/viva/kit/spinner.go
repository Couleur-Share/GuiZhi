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
	return l.SpinIcon(c, icons.LoaderCircle, size, color)
}

// SpinIcon 让任意图标按 animate-spin 转动（刷新钮的 RefreshCw 在读取期间转圈）；规则与 Spinner 相同：动效关闭档不转。
// 不转的时候调用方改用 Icon 画静止的图标；两者的外框登记一致。
func (l *Look) SpinIcon(c ui.C, ic gfx.Icon, size float32, color tokens.Color) ui.Box {
	col := l.Col(color)
	animated := l.Spec.Motion.Animated()
	return ui.Slot(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
		mark := l.markIcon(d)
		tint := d.Fade(col)
		if !animated || !d.Canvas.HasClock() {
			PaintIcon(d.Canvas, ic, d.Rect, tint)
			l.reportIcon(d, mark)
			return
		}
		turns := float64(d.Canvas.Now()%spinPeriod) / float64(spinPeriod)
		d.Canvas.DrawIconTransformed(ic, d.Rect, tint, gfx.IconTransform{Rotate: float32(2 * math.Pi * turns)})
		l.reportIcon(d, mark)
		d.Canvas.RequestFrame()
	})
}
