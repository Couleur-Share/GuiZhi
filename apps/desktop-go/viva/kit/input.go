package kit

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// InputSpec 描述一个与渲染层 <input> 对应的单行文本输入：字号、字重、颜色、框高，以及文字区左右的内边距。
// 内边距里已经包含渲染层 border（透明的 1px 边框也算，文字从边框内侧再加 padding 起）。
type InputSpec struct {
	Size   float32      // 字号（逻辑像素）
	Weight float32      // Viva 字重刻度（WeightNormal…）
	Color  tokens.Color // 输入内容的颜色；占位符固定用 MutedForeground（渲染层 placeholder:text-muted-foreground）
	Height float32      // 框高（逻辑像素），不小于 Size+6
	Left   float32      // 文字区左侧内边距
	Right  float32      // 文字区右侧内边距
}

// Input 是去掉外观的单行文本输入（Viva 的 TextFieldBase）：只有可编辑的文字、占位符、插入符、选区与输入法，
// 底色、边框由调用方用 kit 的表面画。返回的盒子已按 spec.Height 定高，文字的基线落在渲染层 <input> 的位置
// （InputPad 的推导；占位符与输入内容走 Viva 同一条绘制路径，所以基线一致）。
//
// 文本输入是 V-a（中文输入法真机测试，推迟到整体迁移之后）的风险面：业务界面只经这里使用文本输入，
// 不直接碰 Viva 的 TextField / TextArea / CodeEditor（viva/inputguard_test.go 守卫）。
// 真机测试若不通过，整体替换这一处的实现，业务界面不用改。
func (l *Look) Input(c ui.C, f *ui.Field, label, placeholder string, spec InputSpec) ui.Box {
	top, bottom := InputPad(spec.Weight, spec.Size, spec.Height, l.Scale)
	return ui.TextFieldBase(c, f, label, placeholder).
		Height(spec.Height).
		PadSides(top, spec.Right, bottom, spec.Left).
		FontSize(spec.Size).
		Weight(spec.Weight).
		Color(l.Col(spec.Color))
}
