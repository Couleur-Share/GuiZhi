package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// TextAreaSpec 描述一个与渲染层 <textarea> 对应的多行文本输入：框高、文字区的内边距、文字与占位符的颜色。
// 字号固定为 text-sm（Look.Theme 的 FontSizeM，Viva 的多行框只认主题字号），行高是 Viva 的 size+7。
type TextAreaSpec struct {
	Height float32      // 框高（逻辑像素）：渲染层按 rows 与行高算出的值由调用方换算
	PadX   float32      // 文字区左右内边距
	PadY   float32      // 文字区上下内边距
	Color  tokens.Color // 输入内容的颜色
	// Placeholder 是占位符的颜色（渲染层 placeholder:text-muted-foreground/70）：Viva 的占位符只认主题的弱化色，
	// 所以由这里在空输入上叠画，按框宽折行，与输入内容共用同一套行盒；输入法组合中（编辑器内容非空）自然不画。
	Placeholder tokens.Color
}

// TextArea 是去掉外观的多行文本输入（Viva 的 TextArea 去掉底色、边框、圆角与阴影）：只有可编辑的文字、占位符、插入符、选区与输入法，
// 回车换行，长行折行，行随插入符滚动；表面（底色、边框）由调用方用 kit 的表面画。返回的盒子已按 spec.Height 定高。
//
// 与 Input 一样是 V-a（中文输入法真机测试）的风险面：业务界面只经这里使用多行输入，不直接碰 Viva 的 TextArea
// （viva/inputguard_test.go 守卫）。真机测试若不通过，整体替换这一处的实现，业务界面不用改。
// 给了 spec.Placeholder 时返回的盒子用了 DrawOver：调用方不要再对它调 DrawOver（会顶掉占位符的绘制）。
//
// 已知差异：渲染层 text-sm 的行高是 20px，Viva 多行框的行高是字号加 7（14px 字号时 21px），所以同样的框高里可见的行数略少于渲染层。
func (l *Look) TextArea(c ui.C, f *ui.Field, label, placeholder string, spec TextAreaSpec) ui.Box {
	clear := tokens.RGBA(0, 0, 0, 0)
	own := spec.Placeholder != (tokens.Color{}) && placeholder != ""
	shown := placeholder
	if own {
		shown = ""
	}
	box := ui.TextArea(c, f, label, shown).
		Height(spec.Height).
		PadSides(spec.PadY, spec.PadX, spec.PadY, spec.PadX).
		Background(l.Col(clear)).
		Border(0, l.Col(clear)).
		Radius(0).
		Shadow(gfx.ShadowNone).
		Color(l.Col(spec.Color))
	l.noteStop(box)
	if box.Changed() && l.tips != nil {
		l.tips.noteKey() // 文字被编辑了：键没有经过 OnKey（文字系统直接收了），补记一次按键以收起悬停提示（台账 B-017）
	}
	if !own {
		return box
	}
	return box.DrawOver(func(d *ui.DrawContext) {
		if f.Text() != "" {
			return
		}
		size := l.Theme.Metrics.FontSizeM
		lineHeight := size + 7
		content := gfx.Rect{X: d.Rect.X + spec.PadX, Y: d.Rect.Y + spec.PadY, Width: d.Rect.Width - 2*spec.PadX, Height: d.Rect.Height - 2*spec.PadY}
		style := gfx.TextStyle{Size: size, Weight: gfx.WeightRegular, Color: d.Fade(l.Col(spec.Placeholder))}
		d.Canvas.Clip(content)
		from, y := 0, content.Y
		for from >= 0 && y < content.Y+content.Height {
			row, next := d.Canvas.WrapRow(placeholder, from, size, gfx.WeightRegular, content.Width)
			d.Canvas.Label(placeholder[row.Start:row.End], gfx.Rect{X: content.X, Y: y, Width: content.Width, Height: lineHeight}, style)
			from, y = next, y+lineHeight
		}
		d.Canvas.Unclip()
	})
}
