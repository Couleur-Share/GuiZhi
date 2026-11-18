package kit

import (
	"math"
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 滑块（渲染层的 <input type="range">，样式见 globals.css「原生控件外观收敛」）
//
// 外观：输入框高 1.25rem、背景透明；轨道高 0.375rem、全圆角、`muted-foreground/0.25`；滑块头 0.875rem 的圆，
// 2px 页面底色描边、主色填充、`0 1px 3px rgba(0,0,0,.25)` 阴影，与轨道垂直居中；悬停时滑块头放大到 1.12 倍
// （quick 档，标准缓动）；光标是手形（cursor: pointer）。轨道没有「已选」填充段。
//
// 取值与交互全部自己接管，不用 Viva 的 SliderBase：它按 16px 的滑块头换算行程，点滑块头时带着抓取偏移，
// Shift+方向键与 PageUp/PageDown 按「10 个值单位」走，三处都与 Chromium 的 <input type="range"> 不同
// （台账 B-019；渲染层实测见 scripts/parity/tools/probe-range-pointer.mjs，数据在 testdata/range-pointer-react.json）：
//   - 指针：滑块头中心在 [左缘 + d/2, 右缘 − d/2] 上移动（d 是滑块头直径 14px，行程 = 宽度 − d）。按在轨道上，中心跳到
//     指针处并开始拖动；按在滑块头上（命中区是以滑块头中心为圆心的圆）不跳，此后每次移动都让中心对齐到指针，
//     没有抓取偏移；值取最近的步长倍数并夹在范围内。
//   - 键盘：方向键一个步长（右、上加，左、下减），PageUp/PageDown 取 max((max − min)/10, step)，Home/End 到两端；
//     Shift、Ctrl、Alt 不改变结果。
//
// 无障碍沿用 SliderBase 的写法（role=slider、值文字、范围），可访问树不变。

// 滑块的几何（rem）与命中参数。
const (
	rangeHeightRem = 1.25
	rangeTrackRem  = 0.375
	rangeThumbRem  = 0.875
	// rangeHoverGrow 是悬停时滑块头多放大的比例（transform: scale(1.12)）。
	rangeHoverGrow = 0.12
	// rangeHitSlop 是命中圆在滑块头半径之外多出的像素。渲染层实测（二维网格，两枚滑块共 912 个采样点）拟合出的命中圆
	// 半径是 8.5px，悬停放大后的滑块头半径是 7.84px。拟合出的圆心比名义中心偏 (-0.5, -0.375)px，疑似来自参照窗口的
	// 小数坐标，不复刻；所以边缘 1px 内的点不逐个对上（364 个边缘采样点里有 70 个不同，其余全部一致）。
	rangeHitSlop = 0.66
)

// RangeSpec 描述一个滑块。
type RangeSpec struct {
	// Name 是可访问名（渲染层里由 <label htmlFor> 提供）。
	Name string
	// Min、Max、Step 是取值范围与步长；Step 为 0 表示连续。
	Min, Max, Step float32
}

// RangeState 是一枚滑块的状态，由调用方持有。Value 是此刻的值，由调用方在声明前写入（受控）；
// 其余是指针拖动的簿记。零值可用。
type RangeState struct {
	Value float32

	area gfx.Rect // 上一次绘制时输入框的矩形（指针取值用，与 Viva 的 SliderState.area 同一用法）
	drag bool     // 正在拖动滑块头
}

// rangeKeys 是键盘动作：键名按 keymap.Parse（与 ui.Box.Shortcut）的写法，move 给出按键之后的新值（尚未落格）。
var rangeKeys = [...]struct {
	name string
	move func(cur, lo, hi, step, page float64) float64
}{
	{"right", func(cur, _, _, step, _ float64) float64 { return cur + step }},
	{"up", func(cur, _, _, step, _ float64) float64 { return cur + step }},
	{"left", func(cur, _, _, step, _ float64) float64 { return cur - step }},
	{"down", func(cur, _, _, step, _ float64) float64 { return cur - step }},
	{"page_up", func(cur, _, _, _, page float64) float64 { return cur + page }},
	{"page_down", func(cur, _, _, _, page float64) float64 { return cur - page }},
	{"home", func(_, lo, _, _, _ float64) float64 { return lo }},
	{"end", func(_, _, hi, _, _ float64) float64 { return hi }},
}

// rangeModifierSets 是修饰键（Shift、Ctrl、Alt）的所有组合数：渲染层对它们一视同仁，所以每个键各登记一遍。
const rangeModifierSets = 8

// rangeSnap 把值夹进 [Min, Max]，并落到以 Min 为起点的步长网格上：四舍五入到最近的格点，超过上限则退一格
// （Chromium StepRange::ClampValue）。Step 为 0 表示连续，只夹范围。结果取 6 位小数，免得 0.1 一类的步长累积出浮点尾巴。
func rangeSnap(v float64, spec RangeSpec) float64 {
	lo, hi := float64(spec.Min), float64(spec.Max)
	if hi < lo {
		hi = lo
	}
	v = math.Min(math.Max(v, lo), hi)
	if step := float64(spec.Step); step > 0 {
		v = lo + math.Floor((v-lo)/step+0.5)*step
		if v > hi {
			v -= step
		}
		v = math.Min(math.Max(v, lo), hi)
	}
	return math.Round(v*1e6) / 1e6
}

// rangeFraction 是值在范围里走了多远，0 到 1。
func rangeFraction(v float64, spec RangeSpec) float64 {
	lo, hi := float64(spec.Min), float64(spec.Max)
	if hi <= lo {
		return 0
	}
	return math.Min(math.Max((v-lo)/(hi-lo), 0), 1)
}

// rangePointerValue 把指针的横坐标换算成值：滑块头中心的行程是 [左缘 + d/2, 右缘 − d/2]（d 是滑块头直径）。
func rangePointerValue(x float32, area gfx.Rect, thumbD float32, spec RangeSpec) float64 {
	travel := float64(area.Width) - float64(thumbD)
	if travel <= 0 {
		return float64(spec.Min)
	}
	fraction := math.Min(math.Max((float64(x)-float64(area.X)-float64(thumbD)/2)/travel, 0), 1)
	return rangeSnap(float64(spec.Min)+fraction*(float64(spec.Max)-float64(spec.Min)), spec)
}

// rangeOnThumb 报告指针是否按在滑块头上：命中区是以滑块头中心为圆心、半径 hitR 的圆。
func rangeOnThumb(x, y float32, area gfx.Rect, thumbD float32, hitR float64, value float64, spec RangeSpec) bool {
	cx := float64(area.X) + float64(thumbD)/2 + rangeFraction(value, spec)*(float64(area.Width)-float64(thumbD))
	cy := float64(area.Y) + float64(area.Height)/2
	dx, dy := float64(x)-cx, float64(y)-cy
	return dx*dx+dy*dy <= hitR*hitR
}

// RangeInput 画一个撑满宽度的滑块。state.Value 是此刻的值，由调用方在声明前写入（受控）；
// 返回的 changed 表示这一帧用户改了它（拖动、点轨道、键盘），新值在 state.Value 里。
func (l *Look) RangeInput(c ui.C, state *RangeState, spec RangeSpec) (changed bool) {
	// 调用方写入的受控值也要落回范围与步长（原生 input 的取值清理）。
	state.Value = float32(rangeSnap(float64(state.Value), spec))
	apply := func(v float64) {
		if next := float32(rangeSnap(v, spec)); next != state.Value {
			state.Value, changed = next, true
		}
	}
	// 键盘：快捷键挂在外壳上，滑块本体才是拿焦点的那一个（理由同取色器的色相条 pickerHue：具名容器按「焦点落在容器里面」匹配）。
	shell := ui.Column(c).FillWidth().Height(l.Rem(rangeHeightRem))
	lo, hi := float64(spec.Min), float64(spec.Max)
	step := float64(spec.Step)
	if step <= 0 {
		step = (hi - lo) / 100 // 连续滑块：原生按范围的百分之一走
	}
	page := math.Max((hi-lo)/10, step)
	for _, k := range rangeKeys {
		for mods := 0; mods < rangeModifierSets; mods++ {
			if shell.Shortcut(rangeModifiers(mods), k.name) {
				apply(k.move(float64(state.Value), lo, hi, step, page))
			}
		}
	}
	shell.Children(func() { l.rangeTrack(c.Key("slider"), state, spec, apply) })
	return changed
}

// rangeModifiers 把 0..7 的位组合换成 Shift、Ctrl、Alt。
func rangeModifiers(bits int) gfx.Modifiers {
	var m gfx.Modifiers
	if bits&1 != 0 {
		m |= ui.Shift
	}
	if bits&2 != 0 {
		m |= ui.Ctrl
	}
	if bits&4 != 0 {
		m |= ui.Alt
	}
	return m
}

// rangeTrack 是滑块本体：接指针、画轨道与滑块头，并以 role=slider 登记给无障碍。
func (l *Look) rangeTrack(c ui.C, state *RangeState, spec RangeSpec, apply func(float64)) {
	t := l.T
	id := ui.ID(c)
	canvas := c.Canvas()
	trackH, thumbD := l.Rem(rangeTrackRem), l.Rem(rangeThumbRem)
	p := l.hoverTo(canvas, id, 0, canvas.Hovered() == id || state.drag, DurationQuick)

	if ev, ok := canvas.PointerOn(id); ok {
		switch ev.Kind {
		case gfx.PointerDown:
			state.drag = ev.Button <= gfx.PointerButtonPrimary
			hitR := float64(thumbD)*(1+rangeHoverGrow*float64(p))/2 + rangeHitSlop
			if state.drag && !rangeOnThumb(ev.X, ev.Y, state.area, thumbD, hitR, float64(state.Value), spec) {
				apply(rangePointerValue(ev.X, state.area, thumbD, spec)) // 点在轨道上：滑块头中心跳到指针处
			}
		case gfx.PointerMove:
			if state.drag {
				apply(rangePointerValue(ev.X, state.area, thumbD, spec))
			}
		case gfx.PointerUp:
			state.drag = false
		}
		canvas.Consume()
	}

	step := float64(spec.Step)
	if step <= 0 {
		step = 1
	}
	box := ui.Column(c).FillWidth().Grow(1).HitWith(id, spec.Name, gfx.HitOptions{
		Role: gfx.RoleSlider, Cursor: gfx.PointerCursorHand,
		Value: strconv.FormatFloat(float64(state.Value), 'f', -1, 32),
		Range: gfx.ValueRange{Now: float64(state.Value), Min: float64(spec.Min), Max: float64(spec.Max), Step: step, Set: true},
	})
	l.noteStop(box)
	box.Draw(func(d *ui.DrawContext) {
		state.area = d.Rect
		r := d.Rect
		track := gfx.Rect{X: r.X, Y: r.Y + (r.Height-trackH)/2, Width: r.Width, Height: trackH}
		l.PaintSurfaceAt(d, track, Surface{Bg: t.MutedForeground.WithAlpha(0.25), Radius: trackH / 2})
		// 滑块头：行程是宽度减去它自己的直径；悬停放大以自身中心为原点。
		scale := 1 + rangeHoverGrow*p
		size := thumbD * scale
		cx := r.X + thumbD/2 + float32(rangeFraction(float64(state.Value), spec))*(r.Width-thumbD)
		cy := track.Y + trackH/2
		thumb := gfx.Rect{X: cx - size/2, Y: cy - size/2, Width: size, Height: size}
		l.PaintSurfaceAt(d, thumb, Surface{
			Bg: t.Primary, Border: t.Background, BorderW: 2 * scale, Radius: size / 2,
			Shadow: tokens.Shadow{X: 0, Y: scale, Blur: 3 * scale, Spread: 0, Color: tokens.RGBA(0, 0, 0, 0.25)},
		})
	})
}

// PaintSurfaceAt 在 r 上画表面（PaintSurface 的指定矩形版：控件内部的一块子区域，如滑块的轨道）。
func (l *Look) PaintSurfaceAt(d *ui.DrawContext, r gfx.Rect, s Surface) {
	sub := *d
	sub.Rect = r
	l.PaintSurface(&sub, s)
}
