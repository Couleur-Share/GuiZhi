package kit

import (
	"strings"

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
	// Mono 让内容用等宽字体（font-mono）。
	Mono bool
	// Upper 让内容显示成大写而编辑器里的文字不变（text-transform: uppercase：可访问名里的值、复制出去的文字仍是原样）。
	// Viva 的输入框没有文字变换，所以编辑器的文字画成透明（插入符、选区、输入法照常），在同一位置叠画大写的一份；
	// 这只在等宽字体下位置才与插入符严丝合缝（大小写字格同宽），所以要求 Mono（不等宽时 Upper 不生效）。
	// 局限：内容长到超出输入框宽度而横向滚动时，叠画的一份不跟着滚；十六进制色值这类几个字符的输入不会遇到。
	Upper bool
}

// invisibleInkAlpha 是「看不见的文字」的不透明度（见 Input 的大写叠画）；对照工具把低于 VisibleInkAlpha 的文字当作不存在。
const invisibleInkAlpha = 0.001

// VisibleInkAlpha 是对照工具判断文字「看得见」的不透明度下限：低于它的文字（如大写叠画下的编辑器文字）不参与比较。
const VisibleInkAlpha = 0.01

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
	// 大写叠画：内容非空时编辑器的文字画成透明，由 DrawOver 在同一位置画大写的一份。
	overlay := spec.Upper && spec.Mono && f.Text() != ""
	ink := spec.Color
	if overlay {
		// 透明不能用零值：Viva 把全零的文字颜色当作「没设」，改回主题前景色。千分之一的不透明度在 8 位通道里
		// 与背景相差不到半级，看不出来（对照工具也据此把它当作「看不见的文字」，parity/render.go）。
		ink = tokens.RGBA(0, 0, 0, invisibleInkAlpha)
	}
	box := ui.TextFieldBase(c, f, label, shown).
		Height(spec.Height).
		PadSides(top, spec.Right, bottom, spec.Left).
		FontSize(spec.Size).
		Weight(spec.Weight).
		Color(l.Col(ink))
	if spec.Mono {
		box = box.Mono()
	}
	if !own && !overlay {
		return box
	}
	return box.DrawOver(func(d *ui.DrawContext) {
		// 与 Viva 画输入内容的行盒相同：内容区（框去掉内边距）里高 Size+6 的一行。
		line := gfx.Rect{X: d.Rect.X + spec.Left, Y: d.Rect.Y + top, Width: d.Rect.Width - spec.Left - spec.Right, Height: spec.Size + 6}
		text := f.Text()
		switch {
		case text == "" && own:
			d.Canvas.Clip(line)
			d.Canvas.Label(placeholder, line, gfx.TextStyle{Size: spec.Size, Weight: spec.Weight, Color: d.Fade(l.Col(spec.Placeholder))})
			d.Canvas.Unclip()
		case text != "" && overlay:
			d.Canvas.Clip(line)
			d.Canvas.Label(strings.ToUpper(text), line, gfx.TextStyle{Size: spec.Size, Weight: spec.Weight, Mono: true, Color: d.Fade(l.Col(spec.Color))})
			d.Canvas.Unclip()
		}
	})
}
