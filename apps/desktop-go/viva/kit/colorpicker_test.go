package kit

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/locales"
)

// 取色器的 L4 行为测试：对照渲染层 components/ui/ColorPicker.tsx 逐项钉死（定位、取色、拖动、方向键、十六进制提交、回声、关闭）。
// 全部在无头会话里跑，不开真窗口。

const pickerName = "自定义"

// 渲染层 updatePosition 的定位算法：往哪边翻只看 248px 的估高，真正锚的是紧挨触发钮的那条边。
func TestPickerPanelRect(t *testing.T) {
	const h = 230 // 实际高度：2 + 24 + 136 + 12 + 12 + 12 + 32
	cases := []struct {
		name   string
		anchor gfx.Rect
		vw, vh float32
		want   gfx.Rect
	}{
		{"下方放得下：贴在触发钮下方 8px，弹层右缘与触发钮对齐", gfx.Rect{X: 1000, Y: 100, Width: 40, Height: 36}, 1280, 800, gfx.Rect{X: 808, Y: 144, Width: 232, Height: h}},
		{"左缘夹到视口留白 12", gfx.Rect{X: 100, Y: 100, Width: 40, Height: 36}, 1280, 800, gfx.Rect{X: 12, Y: 144, Width: 232, Height: h}},
		{"右侧越界：夹回 视口宽 − 面板宽 − 12", gfx.Rect{X: 1250, Y: 100, Width: 40, Height: 36}, 1280, 800, gfx.Rect{X: 1036, Y: 144, Width: 232, Height: h}},
		{"下方恰好 248：不翻", gfx.Rect{X: 1000, Y: 100, Width: 40, Height: 36}, 1280, 100 + 36 + 12 + 248, gfx.Rect{X: 808, Y: 144, Width: 232, Height: h}},
		{"下方 247 且上方更宽裕：向上展开，锚定弹层底边", gfx.Rect{X: 1000, Y: 600, Width: 40, Height: 36}, 1280, 600 + 36 + 12 + 247, gfx.Rect{X: 808, Y: 600 - 8 - h, Width: 232, Height: h}},
		{"两侧都放不下：仍向下（上方不更宽裕）", gfx.Rect{X: 1000, Y: 100, Width: 40, Height: 36}, 1280, 300, gfx.Rect{X: 808, Y: 144, Width: 232, Height: h}},
	}
	for _, c := range cases {
		if got := pickerPanelRect(c.anchor, c.vw, c.vh, h); got != c.want {
			t.Errorf("%s：得到 %+v，期望 %+v", c.name, got, c.want)
		}
	}
}

// 方向键微调：色块的饱和度与明度夹在 0–1；色相首尾相接。
func TestPickerNudgeMath(t *testing.T) {
	if got := nudgePad(HSV{H: 217, S: 0.99, V: 0.01}, 0.02, -0.02); got != (HSV{H: 217, S: 1, V: 0}) {
		t.Errorf("饱和度、明度应夹在 0–1：得到 %+v", got)
	}
	if got := nudgePad(HSV{H: 217, S: 0.5, V: 0.5}, -0.1, 0.1); math.Abs(got.S-0.4) > 1e-9 || math.Abs(got.V-0.6) > 1e-9 || got.H != 217 {
		t.Errorf("Shift 微调 ±10%%：得到 %+v", got)
	}
	if got := nudgeHue(HSV{H: 1, S: 1, V: 1}, -2); got.H != 359 {
		t.Errorf("色相 1° 再减 2° 应绕回 359°：得到 %v", got.H)
	}
	if got := nudgeHue(HSV{H: 359, S: 1, V: 1}, 10); got.H != 9 {
		t.Errorf("色相 359° 再加 10° 应绕回 9°：得到 %v", got.H)
	}
	if got := nudgeHue(HSV{H: 217, S: 0.3, V: 0.4}, 2); got != (HSV{H: 219, S: 0.3, V: 0.4}) {
		t.Errorf("微调色相不应改动饱和度与明度：得到 %+v", got)
	}
}

// 行内块元素的基线间隙：根字号 14 / 16 / 18px 三档量到的是 6 / 6 / 7px（见 inlinegap.go）。
func TestStrutDescentPerFontTier(t *testing.T) {
	for _, c := range []struct {
		tier string
		want float32
	}{{"small", 6}, {"medium", 6}, {"large", 7}} {
		spec := still()
		spec.FontSize = c.tier
		if got := NewLook(spec).StrutDescent(); got != c.want {
			t.Errorf("%s 档：行内块下方的间隙得到 %g，期望 %g", c.tier, got, c.want)
		}
	}
}

// pickerPage 是一个带取色器的页面：右上角取色器，左上角一个普通按钮用来确认「点外面」会传给下面的控件。
type pickerPage struct {
	st      ColorPickerState
	value   string
	emits   []string
	pressed int
	bottom  bool // 取色器贴在窗口底部（弹层要向上翻）
}

func (p *pickerPage) view(l *Look, c ui.C) {
	col := ui.Column(c).Fill().Pad(24).Gap(24)
	if p.bottom {
		col = col.Justify(ui.End)
	}
	col.Children(func() {
		btn := ui.Row(c).Size(120, 40).Hit(ui.ID(c), "其他按钮")
		if btn.Clicked() {
			p.pressed++
		}
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if hex, ok := l.ColorPicker(c.Key("picker"), &p.st, ColorPickerSpec{Name: pickerName, Value: p.value}); ok {
				p.emits = append(p.emits, hex)
				p.value = hex
			}
		})
	})
}

func openPicker(t *testing.T, value string) (*harness, *pickerPage) {
	t.Helper()
	p := &pickerPage{value: value}
	return openPage(t, still(), 800, 600, p.view), p
}

// openedPicker 打开页面并点开取色器。
func openedPicker(t *testing.T, value string) (*harness, *pickerPage) {
	t.Helper()
	h, p := openPicker(t, value)
	h.clickAt(centerOf(h.trigger()))
	return h, p
}

func centerOf(r gfx.Rect) (float32, float32) { return r.X + r.Width/2, r.Y + r.Height/2 }

func (h *harness) trigger() gfx.Rect {
	h.t.Helper()
	n, ok := h.node(gfx.RoleButton, pickerName)
	if !ok {
		h.t.Fatal("没有找到取色器触发钮")
	}
	return n.Bounds
}

func (h *harness) mustNode(role gfx.Role, name string) gfx.Rect {
	h.t.Helper()
	n, ok := h.node(role, name)
	if !ok {
		h.t.Fatalf("没有找到 %v「%s」", role, name)
	}
	return n.Bounds
}

func (h *harness) pad() gfx.Rect {
	return h.mustNode(gfx.RoleGroup, locales.CommonColorSaturationBrightness())
}
func (h *harness) hue() gfx.Rect { return h.mustNode(gfx.RoleSlider, locales.CommonColorHue()) }

func (h *harness) hueNow() float64 {
	h.t.Helper()
	n, ok := h.node(gfx.RoleSlider, locales.CommonColorHue())
	if !ok {
		h.t.Fatal("没有找到色相条")
	}
	return n.Range.Now
}

func (h *harness) press(x, y float32) {
	h.t.Helper()
	if err := h.session.Press(x, y); err != nil {
		h.t.Fatalf("按下 (%g,%g) 失败：%v", x, y, err)
	}
	h.frames(2)
}

func (h *harness) release(x, y float32) {
	h.t.Helper()
	if err := h.session.Release(x, y); err != nil {
		h.t.Fatalf("松开 (%g,%g) 失败：%v", x, y, err)
	}
	h.frames(2)
}

// lastEmit 是取色器最近发出的颜色；还没发过时失败。
func (p *pickerPage) lastEmit(t *testing.T) string {
	t.Helper()
	if len(p.emits) == 0 {
		t.Fatal("取色器没有发出任何颜色")
	}
	return p.emits[len(p.emits)-1]
}

func wantHex(t *testing.T, p *pickerPage, hsv HSV, what string) {
	t.Helper()
	if got, want := p.lastEmit(t), HSVToHex(hsv); got != want {
		t.Errorf("%s：发出 %s，期望 %s（HSV %+v）", what, got, want, hsv)
	}
}

func TestColorPickerClosedByDefault(t *testing.T) {
	h, _ := openPicker(t, "#3b82f6")
	n, ok := h.node(gfx.RoleButton, pickerName)
	if !ok {
		t.Fatal("没有找到触发钮")
	}
	if n.Expanded || !n.Expandable {
		t.Errorf("触发钮：expanded=%v expandable=%v，期望 false / true（aria-haspopup=dialog、aria-expanded=false）", n.Expanded, n.Expandable)
	}
	if h.has(gfx.RoleGroup, pickerName) {
		t.Error("默认不应出现弹层")
	}
}

func TestColorPickerOpensBelowTriggerWithItsParts(t *testing.T) {
	h, _ := openedPicker(t, "#3b82f6")
	trig := h.trigger()
	n, _ := h.node(gfx.RoleButton, pickerName)
	if !n.Expanded {
		t.Error("点开后触发钮应处于展开状态")
	}
	panel := h.mustNode(gfx.RoleGroup, pickerName)
	if panel.Width != 232 || panel.Height != 230 {
		t.Errorf("弹层大小 %gx%g，期望 232x230", panel.Width, panel.Height)
	}
	if panel.Y != trig.Y+trig.Height+8 || panel.X+panel.Width != trig.X+trig.Width {
		t.Errorf("弹层位置 (%g,%g)，期望在触发钮下方 8px、右缘对齐：(%g,%g)", panel.X, panel.Y, trig.X+trig.Width-232, trig.Y+trig.Height+8)
	}
	// 里面的三样：色块（border 1 + p-3 12 的内缩）、色相条、十六进制框。
	pad := h.pad()
	if pad.X != panel.X+13 || pad.Y != panel.Y+13 || pad.Width != 206 || pad.Height != 136 {
		t.Errorf("色块 %+v，期望 {%g %g 206 136}", pad, panel.X+13, panel.Y+13)
	}
	if got := h.hueNow(); got != 217 {
		t.Errorf("色相条的当前值 %g，期望 217（#3b82f6 的色相）", got)
	}
	if hex, ok := h.node(gfx.RoleTextField, locales.CommonColorHex()); !ok || hex.Value != "#3b82f6" {
		t.Errorf("十六进制框 %+v（找到 %v），期望值 #3b82f6", hex.Value, ok)
	}
}

func TestColorPickerFlipsUpwardNearWindowBottom(t *testing.T) {
	p := &pickerPage{value: "#3b82f6", bottom: true}
	h := openPage(t, still(), 800, 600, p.view)
	h.clickAt(centerOf(h.trigger()))
	trig, panel := h.trigger(), h.mustNode(gfx.RoleGroup, pickerName)
	if panel.Y+panel.Height+8 != trig.Y {
		t.Errorf("下方放不下时应向上展开、底边距触发钮 8px：弹层 %+v，触发钮 %+v", panel, trig)
	}
}

func TestColorPickerEscapeClosesAndReturnsFocusToTrigger(t *testing.T) {
	// 焦点在触发钮上直接按 Esc。
	h, _ := openedPicker(t, "#3b82f6")
	h.keys("escape")
	if h.has(gfx.RoleGroup, pickerName) {
		t.Fatal("按 Esc 应关闭弹层")
	}
	if n, _ := h.node(gfx.RoleButton, pickerName); !n.Focused || n.Expanded {
		t.Errorf("Esc 关闭后焦点应回到触发钮：focused=%v expanded=%v", n.Focused, n.Expanded)
	}

	// 焦点在十六进制框里时同样（渲染层的 keydown 监听在 document 上）。
	h, _ = openedPicker(t, "#3b82f6")
	h.clickAt(centerOf(h.mustNode(gfx.RoleTextField, locales.CommonColorHex())))
	if n, _ := h.node(gfx.RoleTextField, locales.CommonColorHex()); !n.Focused {
		t.Fatal("点击十六进制框后它应有焦点")
	}
	h.keys("escape")
	if h.has(gfx.RoleGroup, pickerName) {
		t.Fatal("在十六进制框里按 Esc 也应关闭弹层")
	}
	if n, _ := h.node(gfx.RoleButton, pickerName); !n.Focused {
		t.Error("Esc 关闭后焦点应回到触发钮")
	}
}

// 点弹层外面：关闭，而且这一下按压照常落到下面的控件上（不是被遮罩吃掉）；点弹层里面（包括边距）不关。
func TestColorPickerOutsidePressClosesAndPassesThrough(t *testing.T) {
	h, p := openedPicker(t, "#3b82f6")
	panel := h.mustNode(gfx.RoleGroup, pickerName)
	h.clickAt(panel.X+4, panel.Y+4) // 弹层的边距
	if !h.has(gfx.RoleGroup, pickerName) {
		t.Fatal("点弹层自己的边距不应关闭它")
	}
	if len(p.emits) != 0 {
		t.Errorf("点边距不应取色：发出 %v", p.emits)
	}
	h.clickAt(centerOf(h.mustNode(gfx.RoleButton, "其他按钮")))
	if h.has(gfx.RoleGroup, pickerName) {
		t.Error("点弹层外面应关闭弹层")
	}
	if p.pressed != 1 {
		t.Errorf("关闭弹层的这一下点击应落到下面的按钮上：按钮被点 %d 次，期望 1", p.pressed)
	}
}

// 再点触发钮是收起，不是「先关再开」。
func TestColorPickerTriggerToggles(t *testing.T) {
	h, _ := openedPicker(t, "#3b82f6")
	h.clickAt(centerOf(h.trigger()))
	if h.has(gfx.RoleGroup, pickerName) {
		t.Error("再点触发钮应收起弹层")
	}
	if n, _ := h.node(gfx.RoleButton, pickerName); n.Expanded {
		t.Error("收起后触发钮不应再是展开状态")
	}
	h.clickAt(centerOf(h.trigger()))
	if !h.has(gfx.RoleGroup, pickerName) {
		t.Error("第三下应再次展开")
	}
}

// 色块按下即取色：横向 = 饱和度，纵向 = 明度（上方更亮）；自己发出去的值绕回来不会冲掉内部的色相。
func TestColorPickerPadPressPicksAndKeepsHueOnEcho(t *testing.T) {
	h, p := openedPicker(t, "#3b82f6")
	base, _ := HexToHSV("#3b82f6")
	pad := h.pad()

	h.press(pad.X+pad.Width*0.25, pad.Y+pad.Height*0.5)
	h.release(pad.X+pad.Width*0.25, pad.Y+pad.Height*0.5)
	wantHex(t, p, HSV{H: base.H, S: 0.25, V: 0.5}, "按在色块四分之一宽、一半高")

	// 按到左上角：饱和度 0、明度 1，即白色；若回声被当成外部改色，色相会被重置成 0。
	h.press(pad.X, pad.Y)
	h.release(pad.X, pad.Y)
	if got := p.lastEmit(t); got != "#ffffff" {
		t.Errorf("按在色块左上角：发出 %s，期望 #ffffff", got)
	}
	if got := h.hueNow(); got != math.Round(base.H) {
		t.Errorf("白色绕回来之后色相条应仍是 %v（回声不能重置色相），得到 %v", math.Round(base.H), got)
	}
	if hex, _ := h.node(gfx.RoleTextField, locales.CommonColorHex()); hex.Value != "#ffffff" {
		t.Errorf("十六进制框应跟着变成 #ffffff，得到 %q", hex.Value)
	}
}

// 按住拖动持续取色，拖出控件外仍然有效（指针捕获），值夹在边界；松开之后的移动不再取色。
func TestColorPickerPadDragIsCapturedAndClamped(t *testing.T) {
	h, p := openedPicker(t, "#3b82f6")
	base, _ := HexToHSV("#3b82f6")
	pad := h.pad()
	cx, cy := centerOf(pad)

	h.press(cx, cy)
	n := len(p.emits)
	h.moveTo(pad.X+pad.Width*0.75, pad.Y+pad.Height*0.25)
	if len(p.emits) <= n {
		t.Fatal("按住拖动应持续取色")
	}
	wantHex(t, p, HSV{H: base.H, S: 0.75, V: 0.75}, "拖到四分之三宽、四分之一高")

	h.moveTo(pad.X+pad.Width+300, pad.Y+pad.Height+200) // 远在色块与弹层之外
	wantHex(t, p, HSV{H: base.H, S: 1, V: 0}, "拖出色块右下方：饱和度与明度夹在边界")

	h.release(pad.X+pad.Width+300, pad.Y+pad.Height+200)
	n = len(p.emits)
	h.moveTo(cx, cy)
	if len(p.emits) != n {
		t.Error("松开之后的移动不应再取色")
	}
}

// 色相条：横向 0–360°，按下即取，拖动持续；饱和度与明度保持不变。
func TestColorPickerHuePressAndDrag(t *testing.T) {
	h, p := openedPicker(t, "#3b82f6")
	base, _ := HexToHSV("#3b82f6")
	hue := h.hue()
	midY := hue.Y + hue.Height/2

	h.press(hue.X, midY)
	wantHex(t, p, HSV{H: 0, S: base.S, V: base.V}, "按在色相条最左端")
	h.moveTo(hue.X+hue.Width/2, midY)
	wantHex(t, p, HSV{H: 180, S: base.S, V: base.V}, "拖到色相条中点")
	if got := h.hueNow(); got != 180 {
		t.Errorf("色相条的当前值 %g，期望 180", got)
	}
	h.moveTo(hue.X+hue.Width+200, midY)
	wantHex(t, p, HSV{H: 360, S: base.S, V: base.V}, "拖出右端：色相夹在 360°")
	if got := h.hueNow(); got != 360 {
		t.Errorf("夹到右端时色相条的当前值 %g，期望 360（aria-valuenow = round(h)）", got)
	}
	h.release(hue.X+hue.Width+200, midY)
}

// 方向键微调：色块 ±2%（Shift ±10%），色相 ±2°（Shift ±10°）；色块与色相条各自要有焦点才响应。
func TestColorPickerKeyboardNudges(t *testing.T) {
	h, p := openedPicker(t, "#3b82f6")
	base, _ := HexToHSV("#3b82f6")
	pad := h.pad()
	cx, cy := centerOf(pad)

	h.press(cx, cy)
	h.release(cx, cy)
	cur := HSV{H: base.H, S: 0.5, V: 0.5}
	wantHex(t, p, cur, "先点一下色块")

	h.keys("right")
	cur.S = 0.52
	wantHex(t, p, cur, "→ 饱和度 +2%")
	h.keys("shift+up")
	cur.V = 0.6
	wantHex(t, p, cur, "Shift+↑ 明度 +10%")
	h.keys("left")
	cur.S = 0.5
	wantHex(t, p, cur, "← 饱和度 −2%")
	h.keys("down")
	cur.V = 0.58
	wantHex(t, p, cur, "↓ 明度 −2%")

	// 夹在边界：从 S=1 再按 → 不会越界。
	h.press(pad.X+pad.Width-0.01, cy)
	h.release(pad.X+pad.Width-0.01, cy)
	h.keys("shift+right")
	if got, _ := HexToHSV(p.lastEmit(t)); got.S < 0.999 {
		t.Errorf("饱和度到顶后再按 → 应仍是 1，得到 %v", got.S)
	}

	// 色相条：左端 0°，← 绕回 358°；Shift+→ 加 10°；↓ 同 ←。
	hue := h.hue()
	midY := hue.Y + hue.Height/2
	h.press(hue.X, midY)
	h.release(hue.X, midY)
	h.keys("left")
	if got := h.hueNow(); got != 358 {
		t.Errorf("0° 再按 ← 应绕回 358°，得到 %g", got)
	}
	h.keys("shift+right")
	if got := h.hueNow(); got != 8 {
		t.Errorf("358° 再按 Shift+→ 应是 8°，得到 %g", got)
	}
	h.keys("down")
	if got := h.hueNow(); got != 6 {
		t.Errorf("8° 再按 ↓ 应是 6°，得到 %g", got)
	}
	h.keys("up")
	if got := h.hueNow(); got != 8 {
		t.Errorf("6° 再按 ↑ 应是 8°，得到 %g", got)
	}
}

// typeHex 点进十六进制框、全选、输入 text（不提交）。
func (h *harness) typeHex(text string) {
	h.t.Helper()
	h.clickAt(centerOf(h.mustNode(gfx.RoleTextField, locales.CommonColorHex())))
	h.keys("cmd+a")
	if err := h.session.Type(text); err != nil {
		h.t.Fatalf("输入 %q 失败：%v", text, err)
	}
	h.frames(2)
}

func (h *harness) hexValue() string {
	h.t.Helper()
	n, ok := h.node(gfx.RoleTextField, locales.CommonColorHex())
	if !ok {
		h.t.Fatal("没有找到十六进制框")
	}
	return n.Value
}

// 十六进制框：回车提交；三位简写补齐；色相条与色块跟着走。
func TestColorPickerHexCommitsOnEnter(t *testing.T) {
	h, p := openedPicker(t, "#3b82f6")
	h.typeHex("#FF0000")
	if len(p.emits) != 0 {
		t.Fatalf("输入过程中不应提交：发出 %v", p.emits)
	}
	h.keys("enter")
	if got := p.lastEmit(t); got != "#ff0000" {
		t.Errorf("回车提交 #FF0000：发出 %s，期望 #ff0000（小写规范化）", got)
	}
	if got := h.hueNow(); got != 0 {
		t.Errorf("提交红色后色相条应是 0°，得到 %g", got)
	}
}

func TestColorPickerHexCommitsOnBlurAndExpandsShorthand(t *testing.T) {
	h, p := openedPicker(t, "#3b82f6")
	h.typeHex("0f0")
	h.keys("tab") // 失去焦点
	if got := p.lastEmit(t); got != "#00ff00" {
		t.Errorf("失焦提交 0f0：发出 %s，期望 #00ff00（三位简写补齐，可不带 #）", got)
	}
	if got := h.hueNow(); got != 120 {
		t.Errorf("提交绿色后色相条应是 120°，得到 %g", got)
	}
}

// 认不出的写法：不发出，文字退回当前色。
func TestColorPickerHexInvalidRevertsToCurrentColor(t *testing.T) {
	h, p := openedPicker(t, "#3b82f6")
	h.typeHex("zz12")
	h.keys("enter")
	if len(p.emits) != 0 {
		t.Errorf("无法识别的写法不应发出：%v", p.emits)
	}
	if got := h.hexValue(); got != "#3b82f6" {
		t.Errorf("输入框应退回当前色 #3b82f6，得到 %q", got)
	}
}

// 外部改色（不是自己发出去的）：色相条与十六进制框跟着重置。
func TestColorPickerExternalChangeResetsHSV(t *testing.T) {
	h, p := openedPicker(t, "#3b82f6")
	p.value = "#00ff00"
	h.frames(3)
	if got := h.hueNow(); got != 120 {
		t.Errorf("外部把值改成 #00ff00 后色相条应是 120°，得到 %g", got)
	}
	if got := h.hexValue(); got != "#00ff00" {
		t.Errorf("外部改色后十六进制框应是 #00ff00，得到 %q", got)
	}
	if len(p.emits) != 0 {
		t.Errorf("外部改色不应被当成用户选色而发出：%v", p.emits)
	}
}

// 调用方给的值认不出：触发钮的色样取内部的起点色（217°、76%、96%），点开之后色相条也是它。
func TestColorPickerUnrecognizedValueFallsBack(t *testing.T) {
	h, _ := openedPicker(t, "not-a-color")
	if got := h.hueNow(); got != 217 {
		t.Errorf("值认不出时色相应取起点 217°，得到 %g", got)
	}
	if got, want := h.hexValue(), ""; got != want {
		t.Errorf("值认不出时十六进制框应为空（草稿只在值有效时同步），得到 %q", got)
	}
}
