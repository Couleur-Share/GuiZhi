package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// 设置页的文本框（渲染层 `h-10 rounded-lg app-settings-input px-3 text-sm`）：令牌底色与边框、0.75rem 圆角、
// 聚焦时外面一圈 2px 的 primary/50 环（全局 input:focus-visible）。与表单文本框 FieldInput（bg-muted/50、px-4、rounded-xl）不是同一个控件。
//
// 业务界面的文本输入一律经 Look.Input（V-a 守卫），这里只是在它外面加上这一套表面。

// SettingsInputSpec 描述一个设置页文本框。
type SettingsInputSpec struct {
	// Name 是可访问名；空取 Placeholder（渲染层的 <input> 没有 aria-label 时，名称来自 placeholder）。
	Name        string
	Placeholder string
	// PlaceholderInk 非零时占位符用这个颜色（placeholder:text-muted-foreground/60）；零值沿用主题的弱化前景色。
	PlaceholderInk tokens.Color
	// Width 大于 0 时定宽（w-24）；Grow 为真时占满行内剩余（flex-1 min-w-0）；都没有时撑满容器宽（w-full）。
	Width float32
	Grow  bool
	// Mono 让内容用等宽字体（font-mono）。
	Mono bool
	// RightPad 大于 0 时替换右侧内边距（pr-10 给显示/隐藏口令的按钮留位），单位逻辑像素，不含边框。
	RightPad float32
	// Overlay 在文本框之上再声明内容（绝对定位的按钮），与文本框同一个容器。
	Overlay func(c ui.C)
}

// SettingsInput 声明设置页文本框，返回里面的输入元素：调用方用 Changed() 感知编辑、State() 读焦点。文字在 f 里，调用方自己决定何时 SetText。
func (l *Look) SettingsInput(c ui.C, f *ui.Field, spec SettingsInputSpec) ui.Box {
	t := l.T
	height := l.Rem(2.5) // h-10
	surface := Surface{Bg: t.SettingsInputBg, Border: t.SettingsInputBorder, BorderW: 1, Radius: l.Rem(0.75)}
	pad := l.Space(3) + 1 // px-3 加 1px 边框
	right := pad
	if spec.RightPad > 0 {
		right = spec.RightPad + 1
	}
	name := spec.Name
	if name == "" {
		name = spec.Placeholder
	}
	wrap := ui.Stack(c).Height(height)
	switch {
	case spec.Width > 0:
		wrap = wrap.Width(spec.Width).Shrink(0)
	case spec.Grow:
		wrap = wrap.Grow(1).Shrink(1).Basis(0).MinWidth(0)
	default:
		wrap = wrap.FillWidth()
	}
	var box ui.Box
	var ring float32 // 聚焦环的进度：底板先声明、输入后声明，环要等输入的聚焦状态，所以由绘制闭包在绘制时读
	wrap.Children(func() {
		anim := ui.ID(c.Key("ring"))
		ui.Row(c.Key("surface")).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
			l.PaintInputRing(d, ring, surface.Radius)
			l.PaintSurface(d, surface)
		})
		box = l.Input(c.Key("input"), f, name, spec.Placeholder, InputSpec{
			Size: l.Rem(0.875), Weight: WeightNormal, Color: t.Foreground, Height: height, Left: pad, Right: right,
			Placeholder: spec.PlaceholderInk, Mono: spec.Mono,
		}).Absolute().Inset(0)
		ring = l.InputRing(c, anim, box.State().Focused)
		if spec.Overlay != nil {
			spec.Overlay(c)
		}
	})
	return box
}

// SettingsPasswordInput 是带「显示/隐藏口令」按钮的设置页口令框（渲染层 shared.tsx 的 PasswordInput）：
// 输入框 pr-10，右侧 right-3 处是 1rem 的眼睛图标按钮（弱化前景色，悬停变前景色），按下切换明文与圆点。
// show 是显示状态（由持有它的屏幕保存）；showName 与 hideName 是按钮在隐藏与显示状态下的可访问名。
func (l *Look) SettingsPasswordInput(c ui.C, f *ui.Field, show *bool, spec SettingsInputSpec, showName, hideName string) ui.Box {
	f.Secure = !*show
	spec.RightPad = l.Rem(2.5) // pr-10
	spec.Overlay = func(c ui.C) {
		size := l.Rem(1)
		name, icon := showName, icons.Eye
		if *show {
			name, icon = hideName, icons.EyeOff
		}
		height := l.Rem(2.5)
		ec := c.Key("eye")
		eye := ui.Row(ec).Absolute().Right(l.Space(3)).Top((height-size)/2).Size(size, size).
			HitWith(ui.ID(ec), name, gfx.HitOptions{Role: gfx.RoleButton, Selected: *show, Cursor: gfx.PointerCursorHand})
		th := l.T
		color := th.MutedForeground
		if eye.State().Hovered {
			color = th.Foreground
		}
		eye.Children(func() { l.Icon(c.Key("eye-icon"), icon, size, color) })
		if eye.Clicked() {
			*show = !*show
			f.Secure = !*show
		}
	}
	return l.SettingsInput(c, f, spec)
}
