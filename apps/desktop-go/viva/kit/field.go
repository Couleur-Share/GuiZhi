package kit

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 表单文本框（渲染层 components/ui/Input.tsx，不带 label 与 error 的形态）：
//
//	w-full h-10 px-4 rounded-xl bg-muted/50 border border-border/70 text-sm placeholder:text-muted-foreground
//	focus:outline-none focus:ring-2 focus:ring-primary/30 focus:bg-background transition-all duration-base
//
// 聚焦（点击与 Tab 都算）时底色换成不透明的背景色、外面出一圈 2px 的 primary/30 环，两者与边框一起按 duration-base 过渡。
// 全局 input:focus-visible 的 ring-primary/50 在 @layer base，被工具类的 focus:ring-primary/30 盖掉，所以环是 /30。
//
// 业务界面的文本输入一律经 Look.Input（V-a 守卫），这里只是在它外面加上这一套表面。

// FieldSpec 描述一个表单文本框。
type FieldSpec struct {
	// Name 是可访问名；空取 Placeholder（渲染层的 <input> 没有 aria-label 时，名称来自 placeholder）。
	Name        string
	Placeholder string
	// AutoFocus 请求在出现的那一帧取得键盘焦点（渲染层 autoFocus）；Viva 只在元素刚出现时认这个请求，
	// 用户挪走焦点之后不会拉回来，所以每一帧都可以传。
	AutoFocus bool
	// PlaceholderInk 非零时占位符由本控件自己画（裁切、不加省略号，与浏览器 <input> 的占位符一致）；
	// 零值沿用 Viva 文本框自带的占位符（过长时会画省略号）。
	PlaceholderInk tokens.Color
}

// FieldInput 声明表单文本框，返回里面的输入元素：调用方用 Changed() 感知编辑、Submitted() 感知回车。
// 文字在 f 里，调用方自己决定何时 SetText。
func (l *Look) FieldInput(c ui.C, f *ui.Field, spec FieldSpec) ui.Box {
	t := l.T
	name := spec.Name
	if name == "" {
		name = spec.Placeholder
	}
	radius := l.Rem(0.75) // rounded-xl
	pad := l.Space(4) + 1 // px-4 加 1px 边框
	height := l.Rem(2.5)  // h-10

	var box ui.Box
	var focus float32 // 聚焦进度：底板先声明、输入后声明，要等输入的聚焦状态，所以由绘制闭包在绘制时读
	ui.Stack(c.Key("field")).FillWidth().Height(height).Shrink(0).Children(func() {
		ui.Row(c.Key("surface")).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
			// 环是 box-shadow，只出现在边框盒外侧：用空心环，半透明的底色才不会透出环色。
			l.PaintRingBand(d, 2*focus, Fade(t.Primary.WithAlpha(0.3), focus), radius)
			l.PaintSurface(d, Surface{
				Bg:     MixToken(t.Muted.WithAlpha(0.5), t.Background, focus),
				Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: radius,
			})
		})
		box = l.Input(c.Key("input"), f, name, spec.Placeholder, InputSpec{
			Size: l.Rem(0.875), Weight: WeightNormal, Color: t.Foreground, Height: height, Left: pad, Right: pad,
			Placeholder: spec.PlaceholderInk,
		}).Absolute().Inset(0)
		target := float32(0)
		if box.State().Focused {
			target = 1
		}
		focus = c.Canvas().Animate(ui.ID(c.Key("focus")), 0, target, l.Motion(DurationBase, EaseDefault))
		if spec.AutoFocus {
			box = box.AutoFocus()
		}
	})
	return box
}
