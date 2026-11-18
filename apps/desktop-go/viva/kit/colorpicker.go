package kit

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 取色器（渲染层 components/ui/ColorPicker.tsx）
//
// 触发钮是一枚 h-9 w-10 的小按钮（rounded-lg border p-1，悬停边框变 primary/50），里面是当前色；
// 点开后是 232px 宽的弹层（rounded-xl border bg-popover p-3 shadow-lg，淡入加 zoom-in-95）：
// 136px 高的饱和度/明度色块、12px 高的色相条、一枚 28px 的色样加 32px 高的十六进制输入框。
//
// 行为与渲染层逐项一致：
//   - 色块与色相条按下即取色，按住拖动持续取色（指针捕获：拖出控件外仍然有效）；方向键微调（色块 ±2% / Shift ±10%，
//     色相 ±2° / Shift ±10°，色相首尾相接）；
//   - 十六进制输入框在回车或失去焦点时提交：认得出（可带 #，三位简写补齐）就发出去，认不出就退回当前色；
//   - 自己发出去的值会绕一圈从调用方回来，记下来才能区分「外部改色」和「回声」（回声不重置内部的 HSV，
//     否则灰阶与黑色附近色相、饱和度会被往返换算冲掉）；
//   - 点弹层外或按 Esc 关闭；Esc 关闭时焦点回到触发钮（点外面不动焦点）。
//
// 与渲染层的差异（Viva 的限制）：
//   - 十六进制输入框的大写显示是叠画的（InputSpec.Upper）；
//   - 面板的可访问角色 dialog 与色块的 application 在 Viva 里只有 group，见台账 A-004；
//   - 弹层跟随触发器的位置只在打开的那一帧与窗口尺寸变化时重算（渲染层还监听 scroll）：设置页的滚动容器在弹层打开时
//     不应滚动，需要时由调用方在滚动时关闭它。

const (
	pickerPanelWidth    = 232 // PANEL_WIDTH（px 字面量）
	pickerFlipHeight    = 248 // PANEL_HEIGHT：只用来判断往哪边翻，不是实际高度
	pickerViewportGap   = 12  // 面板与视口边缘的最小留白
	pickerTriggerGap    = 8   // 面板与触发器的间距
	pickerPadHeight     = 136 // h-[136px]（px 字面量）
	pickerNudge         = 0.02
	pickerNudgeShift    = 0.1
	pickerHueNudge      = 2.0
	pickerHueNudgeShift = 10.0
)

// pickerFallback 是调用方给的值认不出时取色器内部的起点（FALLBACK_HSV）。
var pickerFallback = HSV{H: 217, S: 0.76, V: 0.96}

// hueStops 是色相条的底图：linear-gradient(to right, #f00 0%, #ff0 17%, #0f0 33%, #0ff 50%, #00f 67%, #f0f 83%, #f00 100%)。
var hueStops = [...]struct {
	at      float32
	r, g, b uint8
}{
	{0, 255, 0, 0}, {0.17, 255, 255, 0}, {0.33, 0, 255, 0}, {0.5, 0, 255, 255},
	{0.67, 0, 0, 255}, {0.83, 255, 0, 255}, {1, 255, 0, 0},
}

type pickerDrag uint8

const (
	dragNone pickerDrag = iota
	dragPad
	dragHue
)

// ColorPickerState 是一个取色器的界面状态：由持有它的屏幕保存。
type ColorPickerState struct {
	Open bool

	// anchor 是触发钮上一帧的外框（页面坐标）：弹层据此定位。
	anchor gfx.Rect
	// trigger 是触发钮的标识：Esc 关闭时把焦点还给它。触发钮在外壳容器里面，弹层这一层的 ui.C 算不出同一个标识，
	// 所以声明触发钮时记下来。
	trigger gfx.ID
	// padRect、hueRect 是色块与色相条上一帧的外框：把指针位置换算成取值用（按下与拖动发生在两次绘制之间）。
	padRect, hueRect gfx.Rect

	hsv     HSV
	draft   ui.Field
	ready   bool
	synced  string // 上一帧看到的规范化值（对应 useEffect 的依赖）
	emitted string // 自己最近发出去的值；空表示还没发过（lastEmittedRef）
	drag    pickerDrag
	hexOn   bool // 十六进制输入框上一帧是否有键盘（失去焦点时提交）
}

// ColorPickerSpec 描述一个取色器。
type ColorPickerSpec struct {
	// Name 是触发钮与弹层共用的可访问名（ariaLabel）。
	Name string
	// Value 是当前十六进制色，形如 #3b82f6；认不出的写法按「没有值」处理（色样取内部的 HSV）。
	Value string
}

// ColorPicker 画触发钮，展开时画弹层。返回这一帧用户选出的颜色（`#rrggbb` 小写）与是否发生了选择；
// 调用方把它写入设置，下一帧通过 Value 传回来（回声由取色器自己识别）。
func (l *Look) ColorPicker(c ui.C, st *ColorPickerState, spec ColorPickerSpec) (hex string, changed bool) {
	normalized, valid := NormalizeHex(spec.Value)
	st.sync(normalized, valid)
	swatch := normalized
	if !valid {
		swatch = HSVToHex(st.hsv)
	}

	// 包装 div（relative）里是一个 inline-block 的按钮：按钮的基线在内容盒下沿，边框盒下沿的内边距与边框悬在基线之下，
	// 行盒的下沿取它与支撑文字下降量中较大的一个，所以包装比按钮高 max(0, StrutDescent − 悬出量)，见 inlinegap.go。
	hang := l.Space(1) + 1
	slotH := l.Rem(2.25) + max(0, l.StrutDescent()-hang)
	var trigger ui.Box
	ui.Column(c.Key("slot")).Width(l.Rem(2.5)).Height(slotH).Shrink(0).Children(func() {
		trigger = l.pickerTrigger(c.Key("trigger"), &st.trigger, spec.Name, hexToken(swatch), st.Open).
			DrawOver(func(d *ui.DrawContext) { st.anchor = d.Rect })
	})
	if trigger.Clicked() {
		st.Open = !st.Open
	}
	if !st.Open {
		st.drag, st.hexOn = dragNone, false
		return "", false
	}
	return l.pickerPanel(c, st, spec, normalized, valid, swatch)
}

// sync 是渲染层的 useState 初值与 useEffect([normalizedValue])：外部改色时（不是自己发出去的回声）重置 HSV 与草稿。
func (st *ColorPickerState) sync(normalized string, valid bool) {
	if !st.ready {
		st.ready = true
		st.hsv = pickerFallback
		if valid {
			if h, ok := HexToHSV(normalized); ok {
				st.hsv = h
			}
			st.draft.SetText(normalized)
		}
	}
	if normalized == st.synced {
		return
	}
	st.synced = normalized
	if !valid || normalized == st.emitted {
		return
	}
	if h, ok := HexToHSV(normalized); ok {
		st.hsv = h
	}
	st.draft.SetText(normalized)
}

// hexToken 把 `#rrggbb` 换成不透明的令牌颜色；认不出时返回全透明。
func hexToken(hex string) tokens.Color {
	c, ok := hexToRGB(hex)
	if !ok {
		return tokens.Transparent
	}
	return tokens.RGBA(uint8(c.R), uint8(c.G), uint8(c.B), 1)
}

// pickerTrigger 是 40×36 的触发钮：1px 边框，4px 内边距，里面一块 rounded-md 的色样。
func (l *Look) pickerTrigger(c ui.C, idOut *gfx.ID, name string, fill tokens.Color, open bool) ui.Box {
	t := l.T
	id := ui.ID(c)
	*idOut = id           // 调用方（Esc 关闭时还焦点）需要的就是这个标识：ui.ID 取一次就把 Key 用掉了，不能在外面再算一遍
	radius := l.Rem(0.75) // rounded-lg
	box := ui.Column(c).Width(l.Rem(2.5)).Height(l.Rem(2.25)).Shrink(0).Pad(l.Space(1)+1).
		HitWith(id, name, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Expandable: true, Expanded: open})
	l.noteStop(box)
	p := l.hoverTo(c.Canvas(), id, 0, box.State().Hovered, DurationDefault)
	border := MixToken(t.Border, t.Primary.WithAlpha(0.5), p)
	return box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), radius)
		}
		l.PaintSurface(d, Surface{Border: border, BorderW: 1, Radius: radius})
	}).Children(func() {
		ui.Column(c.Key("fill")).FillWidth().Grow(1).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, Surface{Bg: fill, Radius: l.Rem(0.75) - 2}) // rounded-md
		})
	})
}

// pickerPanelHeight 是弹层的实际高度：边框 2 + 内边距 2×12 + 色块 136 + 色相条（mt-3 + h-3）+ 输入行（mt-3 + h-8）。
func (l *Look) pickerPanelHeight() float32 {
	return 2 + 2*l.Space(3) + pickerPadHeight + l.Space(3) + l.Space(3) + l.Space(3) + l.Rem(2)
}

// pickerPanelRect 按渲染层的算法定位弹层（ColorPicker.tsx 的 updatePosition）：
// 下方放不下（< 248px，只用来判断往哪边翻）且上方更宽裕时向上展开；右边缘与触发钮对齐，再夹回视口内。
// 真正的锚点是紧挨触发钮的那条边：向上展开时锚定弹层的底边。
func pickerPanelRect(anchor gfx.Rect, vw, vh, height float32) gfx.Rect {
	spaceBelow := vh - (anchor.Y + anchor.Height) - pickerViewportGap
	openUp := spaceBelow < pickerFlipHeight && anchor.Y > spaceBelow
	maxLeft := max(float32(pickerViewportGap), vw-pickerPanelWidth-pickerViewportGap)
	left := min(max(float32(pickerViewportGap), anchor.X+anchor.Width-pickerPanelWidth), maxLeft)
	top := anchor.Y + anchor.Height + pickerTriggerGap
	if openUp {
		top = anchor.Y - pickerTriggerGap - height
	}
	return gfx.Rect{X: left, Y: top, Width: pickerPanelWidth, Height: height}
}

// pickerPanel 声明整窗背景层与弹层；返回这一帧选出的颜色。
func (l *Look) pickerPanel(c ui.C, st *ColorPickerState, spec ColorPickerSpec, normalized string, valid bool, swatch string) (hex string, changed bool) {
	t := l.T
	bounds := c.Bounds()
	rect := pickerPanelRect(st.anchor, bounds.Width, bounds.Height, l.pickerPanelHeight())

	var byKey bool
	if l.dismissLayer(c.Key("dismiss"), dismissSpec{Band: bandSelectDismiss, PassPress: true, Keep: &st.anchor, ByKeyboard: &byKey}) {
		st.Open = false
		st.drag, st.hexOn = dragNone, false
		if byKey {
			c.Canvas().RequestFocus(st.trigger) // triggerRef.current?.focus()
		}
		return "", false
	}

	emit := func(value string) {
		st.emitted = value
		st.draft.SetText(value)
		hex, changed = value, true
	}
	apply := func(next HSV) {
		st.hsv = next
		emit(HSVToHex(next))
	}
	fill := hexToken(swatch)

	panelC := c.Key("panel")
	id := ui.ID(panelC)
	motion := l.Motion(DurationQuick, EaseEnter)
	surface := Surface{Bg: t.Popover, Border: t.Border, BorderW: 1, Radius: l.Rem(0.75), Shadows: ShadowLG} // rounded-xl
	l.floatingPanel(panelC, bandSelectPanel, rect).
		Pad(1 + l.Space(3)).
		Draw(func(d *ui.DrawContext) {
			enterPresence(d.Canvas, id, 0, motion, 0.95, center(d.Rect))
			l.PaintSurface(d, surface)
			// 弹层是不透明的：按在它的边距上不能落到下面的背景层（那会关闭弹层）。
			d.Canvas.Block(d.Rect)
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, spec.Name, gfx.HitOptions{})
		}).
		DrawOver(func(d *ui.DrawContext) { d.Canvas.EndPresence() }).
		Children(func() {
			ui.Column(panelC).FillWidth().Children(func() {
				l.pickerPad(panelC.Key("pad"), st, fill, apply)
				l.pickerHue(panelC.Key("hue"), st, apply)
				l.pickerHexRow(panelC.Key("row"), st, fill, normalized, valid, emit)
			})
		})
	return hex, changed
}

// nudgePad 是色块的方向键微调：饱和度、明度各自夹在 0–1。
func nudgePad(hsv HSV, deltaS, deltaV float64) HSV {
	return HSV{H: hsv.H, S: clampUnit(hsv.S+deltaS, 0, 1), V: clampUnit(hsv.V+deltaV, 0, 1)}
}

// nudgeHue 是色相条的方向键微调：首尾相接（(h − step + 360) % 360）。
func nudgeHue(hsv HSV, delta float64) HSV {
	h := math.Mod(hsv.H+delta+360, 360)
	return HSV{H: h, S: hsv.S, V: hsv.V}
}

// padKeys 是色块响应的四个方向键：每次按键对饱和度与明度的增量方向。
var padKeys = [...]struct {
	key    string
	dS, dV float64
}{
	{"left", -1, 0}, {"right", 1, 0}, {"up", 0, 1}, {"down", 0, -1},
}

// hueKeys 是色相条响应的四个方向键：左与下减小，右与上增大。
var hueKeys = [...]struct {
	key  string
	sign float64
}{
	{"left", -1}, {"down", -1}, {"right", 1}, {"up", 1},
}

// pickerPad 是饱和度/明度色块（role=application，h-[136px] w-full cursor-crosshair rounded-lg）。
//
// 方向键用 Box.Shortcut 注册，而具名的容器按「焦点落在容器里面」匹配，焦点就在容器自己身上时匹配不到；
// 所以色块放进一层同大小的外壳，快捷键挂在外壳上，色块自己才是拿焦点的那一个。
func (l *Look) pickerPad(c ui.C, st *ColorPickerState, fill tokens.Color, apply func(HSV)) {
	shell := ui.Column(c).FillWidth().Height(pickerPadHeight)
	for _, k := range padKeys {
		if shell.Shortcut(0, k.key) {
			apply(nudgePad(st.hsv, k.dS*pickerNudge, k.dV*pickerNudge))
		}
		if shell.Shortcut(ui.Shift, k.key) {
			apply(nudgePad(st.hsv, k.dS*pickerNudgeShift, k.dV*pickerNudgeShift))
		}
	}
	shell.Children(func() { l.pickerPadSurface(c.Key("surface"), st, fill, apply) })
}

// pickerPadSurface 是色块本体：按下即取色、按住拖动持续取色，并画出渐变与取色点。
func (l *Look) pickerPadSurface(c ui.C, st *ColorPickerState, fill tokens.Color, apply func(HSV)) {
	id := ui.ID(c)
	box := ui.Column(c).FillWidth().Grow(1).
		HitWith(id, locales.CommonColorSaturationBrightness(), gfx.HitOptions{Role: gfx.RoleGroup, Cursor: gfx.PointerCursorCrosshair})

	canvas := c.Canvas()
	if ev, ok := canvas.PointerOn(id); ok {
		update := false
		switch ev.Kind {
		case gfx.PointerDown:
			// 渲染层的 pointerdown 对任何键都取色并捕获指针，之后的 pointermove 只认主键按住（buttons === 1）。
			st.drag, update = dragNone, true
			if ev.Button == gfx.PointerButtonPrimary {
				st.drag = dragPad
			}
		case gfx.PointerMove:
			update = st.drag == dragPad
		case gfx.PointerUp:
			st.drag = dragNone
		}
		if r := st.padRect; update && r.Width > 0 && r.Height > 0 {
			apply(HSV{
				H: st.hsv.H,
				S: clampUnit(float64((ev.X-r.X)/r.Width), 0, 1),
				V: 1 - clampUnit(float64((ev.Y-r.Y)/r.Height), 0, 1),
			})
		}
		canvas.Consume()
	}

	box.Draw(func(d *ui.DrawContext) {
		st.padRect = d.Rect
		l.paintPad(d, st.hsv, fill)
	})
}

// paintPad 画色块：底色是纯色相 hsl(h 100% 50%)，上面叠白（向右渐隐）与黑（向上渐隐）两层渐变，再画取色点。
func (l *Look) paintPad(d *ui.DrawContext, hsv HSV, fill tokens.Color) {
	canvas := d.Canvas
	r := SnapRect(d.Rect, canvas.PixelScale())
	radius := l.Rem(0.75) // rounded-lg
	if d.State.FocusVisible {
		l.PaintRingBand(d, 2, l.T.Primary.WithAlpha(0.5), radius)
	}
	canvas.Rounded(r, radius, d.Fade(l.Col(tokens.HSL(hsv.H, 100, 50))))
	white, black, clear := l.Col(tokens.White), l.Col(tokens.RGBA(0, 0, 0, 1)), l.Col(tokens.Transparent)
	canvas.FillGradient(r, radius, 90, []gfx.GradientStop{gfx.Stop(0, d.Fade(white)), gfx.Stop(1, clear)})
	canvas.FillGradient(r, radius, 0, []gfx.GradientStop{gfx.Stop(0, d.Fade(black)), gfx.Stop(1, clear)})
	cx := d.Rect.X + float32(hsv.S)*d.Rect.Width
	cy := d.Rect.Y + float32(1-hsv.V)*d.Rect.Height
	l.paintKnob(d, cx, cy, l.Rem(0.875), fill)
}

// paintKnob 画取色点（h-3.5 w-3.5 或 h-4 w-4 的圆，border-2 border-white shadow ring-1 ring-black/30），以 (cx, cy) 为中心。
// box-shadow 的次序是：1px 黑环在上，shadow 的两层在下，圆本身最后画。
func (l *Look) paintKnob(d *ui.DrawContext, cx, cy, size float32, fill tokens.Color) {
	rect := gfx.Rect{X: cx - size/2, Y: cy - size/2, Width: size, Height: size}
	sub := *d
	sub.Rect = rect
	l.PaintSurface(&sub, Surface{Shadows: ShadowBase, Radius: size / 2})
	l.PaintRingBand(&sub, 1, tokens.RGBA(0, 0, 0, 0.3), size/2)
	l.PaintSurface(&sub, Surface{Bg: fill, Border: tokens.White, BorderW: 2, Radius: size / 2})
}

// pickerHue 是色相条（role=slider，mt-3 h-3 w-full cursor-pointer rounded-full）。外壳的缘由同 pickerPad。
func (l *Look) pickerHue(c ui.C, st *ColorPickerState, apply func(HSV)) {
	shell := ui.Column(c).FillWidth().Height(l.Space(3)).MarginTop(l.Space(3))
	for _, k := range hueKeys {
		if shell.Shortcut(0, k.key) {
			apply(nudgeHue(st.hsv, k.sign*pickerHueNudge))
		}
		if shell.Shortcut(ui.Shift, k.key) {
			apply(nudgeHue(st.hsv, k.sign*pickerHueNudgeShift))
		}
	}
	shell.Children(func() { l.pickerHueSlider(c.Key("slider"), st, apply) })
}

// pickerHueSlider 是色相条本体：按下即取、按住拖动持续取，并画出渐变与取色点。
func (l *Look) pickerHueSlider(c ui.C, st *ColorPickerState, apply func(HSV)) {
	id := ui.ID(c)
	box := ui.Column(c).FillWidth().Grow(1).
		HitWith(id, locales.CommonColorHue(), gfx.HitOptions{
			Role: gfx.RoleSlider, Cursor: gfx.PointerCursorHand,
			Range: gfx.ValueRange{Now: jsRound(st.hsv.H), Min: 0, Max: 360, Set: true},
		})

	canvas := c.Canvas()
	if ev, ok := canvas.PointerOn(id); ok {
		update := false
		switch ev.Kind {
		case gfx.PointerDown:
			st.drag, update = dragNone, true
			if ev.Button == gfx.PointerButtonPrimary {
				st.drag = dragHue
			}
		case gfx.PointerMove:
			update = st.drag == dragHue
		case gfx.PointerUp:
			st.drag = dragNone
		}
		if r := st.hueRect; update && r.Width > 0 {
			apply(HSV{H: clampUnit(float64((ev.X-r.X)/r.Width), 0, 1) * 360, S: st.hsv.S, V: st.hsv.V})
		}
		canvas.Consume()
	}

	box.Draw(func(d *ui.DrawContext) {
		st.hueRect = d.Rect
		radius := d.Rect.Height / 2 // rounded-full
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, l.T.Primary.WithAlpha(0.5), radius)
		}
		r := SnapRect(d.Rect, d.Canvas.PixelScale())
		stops := make([]gfx.GradientStop, len(hueStops))
		for i, s := range hueStops {
			stops[i] = gfx.Stop(s.at, d.Fade(l.Col(tokens.RGBA(s.r, s.g, s.b, 1))))
		}
		d.Canvas.FillGradient(r, radius, 90, stops)
		cx := d.Rect.X + float32(st.hsv.H/360)*d.Rect.Width
		l.paintKnob(d, cx, d.Rect.Y+d.Rect.Height/2, l.Rem(1), tokens.HSL(st.hsv.H, 100, 50))
	})
}

// pickerHexRow 是最下面一行：28px 的色样与十六进制输入框（gap-2）。
func (l *Look) pickerHexRow(c ui.C, st *ColorPickerState, fill tokens.Color, normalized string, valid bool, emit func(string)) {
	t := l.T
	radius := l.Rem(0.75) - 2 // rounded-md
	ui.Row(c).FillWidth().MarginTop(l.Space(3)).Items(ui.Center).Gap(l.Space(2)).Children(func() {
		ui.Column(c.Key("swatch")).Width(l.Rem(1.75)).Height(l.Rem(1.75)).Shrink(0).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, Surface{Bg: fill, Border: t.Border, BorderW: 1, Radius: radius})
		})
		// 输入框的底板与输入元素是两个元素：Box.Draw 画在输入内容之上，会把文字盖住（与搜索框同一做法）。
		pad := l.Space(2) + 1 // px-2 加 1px 边框
		var input ui.Box
		ui.Stack(c.Key("field")).Grow(1).Basis(0).MinWidth(0).Height(l.Rem(2)).Children(func() {
			ui.Row(c.Key("surface")).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
				if st.hexOn {
					l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.3), radius) // focus:ring-2 focus:ring-primary/30
				}
				l.PaintSurface(d, Surface{Bg: t.Background, Border: t.Border, BorderW: 1, Radius: radius})
			})
			input = l.Input(c.Key("input"), &st.draft, locales.CommonColorHex(), "", InputSpec{
				Size: l.Rem(0.75), Weight: WeightNormal, Color: t.Foreground, Height: l.Rem(2),
				Left: pad, Right: pad, Mono: true, Upper: true,
			}).Absolute().Inset(0)
		})

		commit := func() {
			next, ok := NormalizeHex(st.draft.Text())
			if !ok {
				// 认不出：草稿退回当前色（setHexDraft(normalizedValue ?? hsvToHex(hsv))）。
				back := normalized
				if !valid {
					back = HSVToHex(st.hsv)
				}
				st.draft.SetText(back)
				return
			}
			if h, ok := HexToHSV(next); ok {
				st.hsv = h
			}
			emit(next)
		}
		focused := input.State().Focused
		switch {
		case input.Submitted():
			commit()
		case st.hexOn && !focused:
			commit() // onBlur
		}
		st.hexOn = focused
	})
}
