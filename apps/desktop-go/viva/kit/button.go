package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// ButtonStyle 是一枚按钮在各状态下的外观（渲染层 className 里的取值）。
// 尺寸是逻辑像素（调用方用 Look.Rem / Look.Space 换算），颜色是令牌，可以带透明度。
// 悬停色字段的零值表示「悬停时不变」。
type ButtonStyle struct {
	Height     float32 // 定高（h-8）；0 表示由内边距加行高决定
	Width      float32 // 定宽（w-6）；0 表示由内容决定
	PadX, PadY float32
	Radius     float32
	Gap        float32
	Text       TextStyle
	// FontPx 与 LinePx 非零时盖过 Text：渲染层 text-[10px] 这类 px 字面量不随字号档缩放，也没有 Tailwind 字号档的行高。
	FontPx, LinePx float32
	Weight         float32

	Fg, FgHover         tokens.Color
	Bg, BgHover         tokens.Color
	Border, BorderHover tokens.Color
	BorderW             float32
	// FgOn、BgOn、BorderOn、WeightOn 是切换钮处于按下（On）时的外观，零值表示与未按下相同。
	// 按下态盖过悬停态（渲染层选中类里没有 hover:），切换时与悬停一样走过渡。
	FgOn, BgOn, BorderOn tokens.Color
	WeightOn             float32
	// Dashed 让边框画成虚线（border-dashed），见 dashed.go。
	Dashed bool
	Shadow tokens.Shadow
	// Ring 是聚焦环的颜色；零值取全局 button:focus-visible 的 ring-primary/50。
	// FocusRing 为真时环跟 :focus 走（点击后仍留着），否则只在键盘聚焦（:focus-visible）时画。
	Ring      tokens.Color
	FocusRing bool

	// Transition 是状态变化的过渡档位；DurationNone 表示直接切换（渲染层没有写 transition-*）。
	Transition DurationName
	// DisabledOpacity 是禁用时整枚按钮的不透明度（disabled:opacity-50）；0 取 0.5。
	DisabledOpacity float32
}

// ButtonSpec 描述一枚按钮的内容与状态。
type ButtonSpec struct {
	Label string
	// Name 是可访问名；空则取 Label（纯图标按钮必须给）。
	Name string
	// Suffix 是标签后面另一段文字（批量按钮的计数），颜色取 SuffixColor；可访问名是「标签 后缀」。
	Suffix      string
	SuffixColor tokens.Color
	// Icon 是文字前的图标；ViewBox 为 0 表示没有。IconSize 缺省 1rem。
	Icon     gfx.Icon
	IconSize float32
	// Full 让按钮横向撑满（w-full），内容居中。
	Full bool
	// Center 让内容在按钮里居中（justify-center），纯图标的定宽按钮用。
	Center bool
	// Grow 让按钮在行内按比例占满空余（flex-1）。
	Grow float32
	// Toggle 表示它是切换钮（aria-pressed），On 是它此刻的按下状态。
	Toggle, On bool
	Disabled   bool
	// Busy 在文字前放一枚转圈（disabled 常与它同用）。
	Busy bool
	// NoRing 去掉键盘聚焦环（少数控件自己有聚焦样式）。
	NoRing bool
	// Autofocus 让按钮在它第一次出现的那一帧取得键盘焦点（弹层打开时 panel.querySelector("button")?.focus()）；
	// 一直在画面上的按钮不会被它拉回焦点。
	Autofocus bool
	// Description 是可访问描述（title 属性在可访问树里的副产物）。
	Description string
	// Groups 非空时，按钮把自己的命中区登记到名为 Group 的悬停分组里（group-hover/<名> 用）。
	Groups *HoverGroups
	Group  string
	// BusySize 是转圈的边长；0 取 1rem（Spinner 的 xs 档是 0.875rem）。
	BusySize float32
	// Hint 是渲染层的 title：悬停或聚焦 320 ms 后由窗口根上的提示层（Look.Tooltips）出气泡，也是按钮的可访问描述。
	Hint string
	// TranslateCentered 表示渲染层里这枚按钮自己是 absolute top-1/2 -translate-y-1/2 居中的：Chromium 对变换容器里的 SVG 图标
	// 按「取整后的绘制偏移」落笔，位置与版面位置差半个像素以内（见 icon_snap.go）。纯图标按钮的图标据此偏移、不吸附整像素。
	TranslateCentered bool
}

// ringColor 是全局 button:focus-visible 的 ring-2 ring-primary/50。
func (l *Look) ringColor() tokens.Color { return l.T.Primary.WithAlpha(0.5) }

// orSame 取悬停色；零值表示悬停不变，返回 base。
func orSame(hover, base tokens.Color) tokens.Color {
	if hover == (tokens.Color{}) {
		return base
	}
	return hover
}

// Button 画一枚按钮：圆角矩形底 + 可选边框，里面是图标与文字，行内居中。返回按钮盒，调用方用 Clicked() 取点击。
//
// 命中区按渲染层的 button 声明：角色 button、手形光标；键盘聚焦时画 ring-2 ring-primary/50（全局 button:focus-visible）。
// 悬停与禁用的颜色变化走过渡（Transition 档位）；禁用时整枚按钮按 DisabledOpacity 变淡。
// 调用方若要给按钮命名（列表里的行），在传入的 c 上先 Key（Button 的第一步就是取命中 ID，会消耗这个键）。
func (l *Look) Button(c ui.C, spec ButtonSpec, st ButtonStyle) ui.Box {
	id := ui.ID(c)
	if spec.Groups != nil {
		spec.Groups.Add(spec.Group, id)
	}
	name := spec.Name
	if name == "" {
		name = spec.Label
		if spec.Suffix != "" {
			name += " " + spec.Suffix
		}
	}
	// 边框占盒内的空间（box-sizing: border-box）：文字要让出它，没有定高时它也计入盒高。
	padY, padX := st.PadY+st.BorderW, st.PadX+st.BorderW
	row := ui.Row(c).Items(ui.Center).Gap(st.Gap).PadSides(padY, padX, padY, padX)
	if st.Height > 0 {
		row = row.Height(st.Height)
	}
	if st.Width > 0 {
		row = row.Width(st.Width)
	}
	switch {
	case spec.Full:
		row = row.FillWidth().Justify(ui.Center)
	case spec.Grow > 0:
		row = row.Grow(spec.Grow).Shrink(1).Justify(ui.Center)
	default:
		row = row.Shrink(0)
	}
	if spec.Center {
		row = row.Justify(ui.Center)
	}
	opts := gfx.HitOptions{Role: gfx.RoleButton, Disabled: spec.Disabled, Selected: spec.Toggle && spec.On, Description: spec.Description, Hint: spec.Hint, Autofocus: spec.Autofocus}
	if !spec.Disabled {
		opts.Cursor = gfx.PointerCursorHand
	}
	box := row.HitWith(id, name, opts)
	if !spec.Disabled {
		l.noteStop(box)
	}
	hovered := box.State().Hovered && !spec.Disabled
	// 过渡进度在声明时取一次：文字与图标的颜色要在声明里定下，不能等到绘制。
	p := l.hoverTo(c.Canvas(), id, 0, hovered, st.Transition)
	on := float32(0)
	if spec.Toggle {
		on = l.hoverTo(c.Canvas(), id, 1, spec.On, st.Transition)
	}
	opacity := float32(1)
	if spec.Disabled {
		opacity = st.DisabledOpacity
		if opacity == 0 {
			opacity = 0.5
		}
	}
	// 先按悬停混，再按「按下」混：按下态在终点盖过悬停，悬停中被点选时底色从悬停色补间到按下色。
	pick := func(base, hover, pressed tokens.Color) tokens.Color {
		mixed := MixToken(base, orSame(hover, base), p)
		return fade(MixToken(mixed, orSame(pressed, mixed), on), opacity)
	}
	fg := pick(st.Fg, st.FgHover, st.FgOn)
	bg := pick(st.Bg, st.BgHover, st.BgOn)
	border := pick(st.Border, st.BorderHover, st.BorderOn)
	weight := st.Weight
	if spec.Toggle && spec.On && st.WeightOn != 0 {
		weight = st.WeightOn
	}
	// 变换居中的按钮：图标的落笔偏移由按钮自己的外框算出（绘制时才知道外框），图标子元素在它之后绘制时读取。
	var iconShift gfx.Point
	iconSize := spec.IconSize
	if iconSize == 0 {
		iconSize = l.Rem(1)
	}
	box = box.Draw(func(d *ui.DrawContext) {
		if spec.TranslateCentered {
			iconShift.X, iconShift.Y = translatedSnapShift(d.Rect, 0, -d.Rect.Height/2, iconSize)
		}
		ringOn := d.State.FocusVisible
		if st.FocusRing {
			ringOn = d.State.Focused
		}
		if !spec.NoRing && ringOn {
			ring := st.Ring
			if ring == (tokens.Color{}) {
				ring = l.ringColor()
			}
			l.PaintRing(d, 2, ring, st.Radius)
		}
		surface := Surface{Bg: bg, Radius: st.Radius, Shadow: st.Shadow}
		if st.BorderW > 0 && !st.Dashed {
			surface.Border, surface.BorderW = border, st.BorderW
		}
		l.PaintSurface(d, surface)
		if st.BorderW > 0 && st.Dashed {
			l.PaintDashedBorder(d, border, st.BorderW, st.Radius)
		}
	})
	return box.Children(func() {
		if spec.Busy {
			size := spec.BusySize
			if size == 0 {
				size = l.Rem(1)
			}
			l.Spinner(c.Key("spinner"), size, fg)
		}
		if spec.Icon.ViewBox > 0 {
			if spec.TranslateCentered {
				l.IconShifted(c.Key("icon"), spec.Icon, iconSize, fg, &iconShift)
			} else {
				l.Icon(c.Key("icon"), spec.Icon, iconSize, fg)
			}
		}
		if spec.Label != "" {
			if st.FontPx > 0 {
				l.TextPx(c.Key("label"), spec.Label, st.FontPx, st.LinePx, weight, fg)
			} else {
				l.Text(c.Key("label"), spec.Label, st.Text, weight, fg)
			}
		}
		if spec.Suffix != "" {
			l.Text(c.Key("suffix"), spec.Suffix, st.Text, weight, fade(spec.SuffixColor, opacity))
		}
	})
}
