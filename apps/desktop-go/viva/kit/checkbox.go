package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// Checkbox（渲染层 components/ui/Checkbox.tsx）
//
// <label class="flex items-center gap-2 cursor-pointer select-none">：一个 16px 的方框加一行 text-sm text-foreground 的文字。
//
//   - 方框（span）：h-4 w-4 rounded border border-border bg-background，transition-colors duration-base；
//     选中：bg-primary border-primary，里面 12px 的对勾（text-primary-foreground，stroke 2，圆头圆角）淡入；
//     未选中时悬停：border-primary/50——悬停写在方框这个 span 上，不在 label 上，所以指针停在文字上不会让边框变色；
//   - 真正的 <input> 是 sr-only：键盘聚焦它时，方框画 outline-2 outline-primary/50（只在 :focus-visible）；
//   - 点方框或文字都切换（label 的 onClick）。文字不是控件：点它不取键盘焦点（onClick 里 preventDefault 了）。
//
// Viva 里整个 label 是一块只管点击的命中区（含方框周围与间隙），方框是叠在它上面的控件（ui.CheckboxBase：命中区、键盘、
// 可访问的 checkbox 都在它身上；后声明的命中区优先）。点 label 的其余部分翻转同一个 *checked——这样方框的悬停只由方框自己决定，
// 与渲染层一致。
//
// 渲染层的 indeterminate 与 disabled 还没有使用方，随用到它们的界面一起补。

// CheckboxSpec 描述一个复选框。
type CheckboxSpec struct {
	// Label 是文字标签，也是可访问名；空表示没有文字（渲染层只给 ariaLabel 的复选框，如表格的行选择框）。
	Label string
	// Name 是没有文字标签时的可访问名（渲染层的 ariaLabel）。
	Name string
}

// checkMark 是对勾：viewBox 12，M2 6 L5 9 L10 3，笔画 2（渲染层 Checkbox 里的内联 SVG）。
var checkMark = gfx.Icon{
	ViewBox: 12,
	Stroke:  2,
	Cap:     gfx.CapRound,
	Join:    gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(2, 6),
		gfx.LineTo(5, 9),
		gfx.LineTo(10, 3),
	},
}

// Checkbox 画一个复选框，*checked 在点击与按键时翻转，返回时已含本帧的翻转（调用方可以立刻与旧值比较）。
// 返回方框这个控件（Clicked / Changed 照常可用）。
func (l *Look) Checkbox(c ui.C, checked *bool, spec CheckboxSpec) ui.Box {
	t := l.T
	var box ui.Box
	// label 的点击要在声明方框之前读出来：方框按这一刻的 *checked 声明，翻转之后才不会被它盖回去。
	labelID := ui.ID(c)
	if c.Canvas().Clicked(labelID) {
		*checked = !*checked
	}
	ui.Row(c).Items(ui.Center).Gap(l.Space(2)).
		HitWith(labelID, "", gfx.HitOptions{SkipFocus: true, Cursor: gfx.PointerCursorHand}).
		Children(func() {
			// 悬停与选中的过渡挂在专用的动画 ID 上：CheckboxBase 的命中 ID 由 *checked 的地址决定，取不到。
			anim := ui.ID(c.Key("anim"))
			side, markSide, radius := l.Rem(1), l.Rem(0.75), l.Rem(0.25)
			name := spec.Label
			if name == "" {
				name = spec.Name
			}
			box = ui.CheckboxBase(c.Key("box"), checked, name).Size(side, side).Shrink(0).HitOutset(0, 0, 0, 0).
				Items(ui.Center).Justify(ui.Center)
			l.noteStop(box)
			canvas := c.Canvas()
			hover := l.hoverTo(canvas, anim, 0, box.State().Hovered, DurationBase)
			active := l.hoverTo(canvas, anim, 1, *checked, DurationBase)
			box.Draw(func(d *ui.DrawContext) {
				// 未选中的悬停边框先混，再按选中进度混向 primary：选中后悬停不再变化，取消选中时回到悬停色，与 CSS 过渡的终点一致。
				border := MixToken(MixToken(t.Border, t.Primary.WithAlpha(0.5), hover), t.Primary, active)
				bg := MixToken(t.Background, t.Primary, active)
				if d.State.FocusVisible {
					l.PaintRing(d, 2, l.ringColor(), radius)
				}
				l.PaintSurface(d, Surface{Bg: bg, Border: border, BorderW: 1, Radius: radius})
			}).Children(func() {
				// 对勾 h-3 w-3，居中；不透明度随选中进度（transition-opacity duration-base）。
				// 未选中时也要声明（全透明、不画任何图元）：渲染层那枚 SVG 一直在，只是 opacity-0；对照按外框与数量比，
				// 看不见的图标只是不比墨迹（compare_icons.go 的 invisibleReactIcons）。
				l.Icon(c.Key("mark"), checkMark, markSide, t.PrimaryForeground.WithAlpha(t.PrimaryForeground.A*active))
			})
			if spec.Label != "" {
				l.Text(c.Key("label"), spec.Label, TextSM, WeightNormal, t.Foreground)
			}
		})
	// 方框翻转的值要在返回前就落到 *checked 上：ui.CheckboxBase 只在第一次问 Clicked / Changed 或本页结束时才写入，
	// 调用方在同一帧里拿 *checked 与调用前比较（表格的行选择框、表头的全选框）会一直看到旧值，点击就丢了。
	// 悬停与选中的过渡目标在上面已按旧值算过，所以画面的时间线不变：点击这一帧还是旧状态，下一帧才是新状态。
	_ = box.Changed()
	return box
}
