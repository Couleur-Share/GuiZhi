package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// InputSpec 描述一个与渲染层 <input> 对应的单行文本输入：字号、字重、颜色、框高，以及文字区左右的内边距。
// 内边距里已经包含渲染层 border（透明的 1px 边框也算，文字从边框内侧再加 padding 起）。
type InputSpec struct {
	Size   float32      // 字号（逻辑像素）
	Weight float32      // Viva 字重刻度（WeightNormal…）
	Color  tokens.Color // 输入内容的颜色
	Height float32      // 框高（逻辑像素），不小于 Size+6
	Left   float32      // 文字区左侧内边距
	Right  float32      // 文字区右侧内边距
	// Placeholder 是占位符的颜色。零值表示沿用主题的 MutedForeground（渲染层的 placeholder:text-muted-foreground），
	// 由 Viva 自己画；渲染层各处的占位符常带透明度（placeholder:text-muted-foreground/60），而 Viva 的占位符只认主题色，
	// 所以给了颜色时由这里在空输入上叠画——位置与 Viva 画输入内容的位置相同，输入法组合中（编辑器内容非空）自然不画。
	Placeholder tokens.Color
}

// Input 是去掉外观的单行文本输入（Viva 的 TextFieldBase）：只有可编辑的文字、占位符、插入符、选区与输入法，
// 底色、边框由调用方用 kit 的表面画。返回的盒子已按 spec.Height 定高，文字的基线落在渲染层 <input> 的位置
// （InputPad 的推导；占位符与输入内容走 Viva 同一条绘制路径，所以基线一致）。
//
// 文本输入是 V-a（中文输入法真机测试，推迟到整体迁移之后）的风险面：业务界面只经这里使用文本输入，
// 不直接碰 Viva 的 TextField / TextArea / CodeEditor（viva/inputguard_test.go 守卫）。
// 真机测试若不通过，整体替换这一处的实现，业务界面不用改。
//
// 给了 spec.Placeholder 时返回的盒子用了 DrawOver：调用方不要再对它调 DrawOver（会顶掉占位符的绘制）。
func (l *Look) Input(c ui.C, f *ui.Field, label, placeholder string, spec InputSpec) ui.Box {
	top, bottom := InputPad(spec.Weight, spec.Size, spec.Height, l.Scale)
	own := spec.Placeholder != (tokens.Color{}) && placeholder != ""
	shown := placeholder
	if own {
		shown = ""
	}
	box := ui.TextFieldBase(c, f, label, shown).
		Height(spec.Height).
		PadSides(top, spec.Right, bottom, spec.Left).
		FontSize(spec.Size).
		Weight(spec.Weight).
		Color(l.Col(spec.Color))
	if !own {
		return box
	}
	return box.DrawOver(func(d *ui.DrawContext) {
		if f.Text() != "" {
			return
		}
		// 与 Viva 画输入内容的行盒相同：内容区（框去掉内边距）里高 Size+6 的一行。
		line := gfx.Rect{X: d.Rect.X + spec.Left, Y: d.Rect.Y + top, Width: d.Rect.Width - spec.Left - spec.Right, Height: spec.Size + 6}
		d.Canvas.Clip(line)
		d.Canvas.Label(placeholder, line, gfx.TextStyle{Size: spec.Size, Weight: spec.Weight, Color: d.Fade(l.Col(spec.Placeholder))})
		d.Canvas.Unclip()
	})
}
