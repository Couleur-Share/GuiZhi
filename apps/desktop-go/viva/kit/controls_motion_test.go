package kit

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 控件状态与过渡的 L4 测试（台账 B-004）。
//
// 参照是 2026-11-18 在渲染层测得的真实数值（scripts/parity/tools/probe-states.mjs 的输出）：
//
//	SettingItem 行  transition-colors，150 ms，cubic-bezier(0.4,0,0.2,1)，不随「减弱」档缩放；
//	开关与旋钮      transition-all duration-base，标准档 180 ms、减弱档 110 ms，标准曲线；
//	分段按钮选中项  transition-all duration-base；
//	分段按钮未选中  .app-settings-subtle 的 160 ms ease（只有背景、边框、阴影），不随「减弱」档缩放；
//	色板            transition-all duration-base；
//	关闭档          0.01 ms，即直接到位。
//
// 曲线用 cubicBezierY（独立于 Viva 自己的缓动实现）求值，不拿被测代码当参照。

// easeStandard 是 --motion-easing-standard：cubic-bezier(0.4, 0, 0.2, 1)。
func easeStandard(x float64) float64 { return cubicBezierY(0.4, 0, 0.2, 1, x) }

// easeCSS 是 CSS 关键字 ease：cubic-bezier(0.25, 0.1, 0.25, 1)。
func easeCSS(x float64) float64 { return cubicBezierY(0.25, 0.1, 0.25, 1, x) }

// progress 是 elapsed 在时长 d 内、经曲线 ease 之后的进度。
func progress(ease func(float64) float64, elapsed, d time.Duration) float64 {
	return ease(math.Min(float64(elapsed)/float64(d), 1))
}

// specFor 是指定动效档的外观。
func specFor(level MotionLevel) Spec {
	s := DefaultSpec()
	s.Motion = level
	return s
}

// rgbOf 把令牌颜色放到不透明背景上，得到 0..255 的三个通道（sRGB 空间里的普通 alpha 混合）。
func rgbOf(fg tokens.Color, over tokens.Color) [3]float64 {
	a := float64(fg.A)
	return [3]float64{
		float64(fg.R)*a + float64(over.R)*(1-a),
		float64(fg.G)*a + float64(over.G)*(1-a),
		float64(fg.B)*a + float64(over.B)*(1-a),
	}
}

// expectPixel 断言像素 got 与期望的 0..255 通道相差不超过 tol。
func expectPixel(t *testing.T, name string, got [4]uint8, want [3]float64, tol float64) {
	t.Helper()
	for i, w := range want {
		if math.Abs(float64(got[i])-w) > tol {
			t.Errorf("%s：像素 %v，期望 RGB≈(%.1f, %.1f, %.1f)（容差 %.1f）", name, got[:3], want[0], want[1], want[2], tol)
			return
		}
	}
}

// fillOf 找出当前帧里尺寸为 w×h 的实心圆角矩形的颜色（轨道、色板、分段按钮的底）；找不到 ok 为 false。
func fillOf(h *harness, w, hgt float32) (col gfx.Color, bounds gfx.Rect, ok bool) {
	for _, p := range h.session.Snapshot().Primitives {
		if p.Kind == 0 && math.Abs(float64(p.Bounds.Width-w)) < 0.01 && math.Abs(float64(p.Bounds.Height-hgt)) < 0.01 {
			return p.Color, p.Bounds, true
		}
	}
	return gfx.Color{}, gfx.Rect{}, false
}

// colorNear 断言 gfx 颜色与令牌颜色（可带 alpha）在每个通道上相差不超过 tol（0..1）。
func colorNear(t *testing.T, name string, got gfx.Color, want tokens.Color, tol float64) {
	t.Helper()
	w := gfxColor(want)
	for i, pair := range [][2]float32{{got.Red, w.Red}, {got.Green, w.Green}, {got.Blue, w.Blue}, {got.Alpha, w.Alpha}} {
		if math.Abs(float64(pair[0]-pair[1])) > tol {
			t.Errorf("%s：颜色 %v，期望 %v（通道 %d，容差 %.3f）", name, got, w, i, tol)
			return
		}
	}
}

// ---- 开关 ----

// switchPage 是一页只有一枚开关的页面，开关初始为关；返回页面与状态指针。轨道在 (20,20)，48×28。
func switchPage(t *testing.T, spec Spec) (*harness, *bool) {
	t.Helper()
	on := new(bool)
	h := openPage(t, spec, 400, 200, func(l *Look, c ui.C) {
		ui.Column(c).Pad(20).Children(func() {
			l.Switch(c, on, "开关")
		})
	})
	return h, on
}

// knobX 是旋钮（20×20 的实心圆）的左边缘；找不到返回 NaN。
func knobX(h *harness) float64 {
	if _, b, ok := fillOf(h, 20, 20); ok {
		return float64(b.X)
	}
	return math.NaN()
}

// 开关点一下之后：旋钮 left 从 2px 走到 22px，轨道底色从「未选中」走到主色，
// 时长 180 ms（标准档）、110 ms（减弱档），曲线是标准曲线；关闭档第一帧就是终态。
func TestSwitchTransition(t *testing.T) {
	cases := []struct {
		name  string
		level MotionLevel
		dur   time.Duration // 0 表示直接到位
	}{
		{"标准档", MotionLevelStandard, 180 * time.Millisecond},
		{"减弱档", MotionLevelReduced, 110 * time.Millisecond},
		{"关闭档", MotionLevelOff, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, on := switchPage(t, specFor(tc.level))
			th := h.look.T
			if *on {
				t.Fatal("初始应为关")
			}
			h.click("开关")
			if !*on {
				t.Fatal("点击后应为开")
			}
			elapsed := time.Duration(0)
			for _, step := range []time.Duration{0, 20, 20, 30, 30, 30, 30, 60} {
				if step > 0 {
					advance(h, step*time.Millisecond)
					elapsed += step * time.Millisecond
				}
				e := 1.0
				if tc.dur > 0 {
					e = progress(easeStandard, elapsed, tc.dur)
				}
				// 旋钮：外框左缘 20 + border 2 + left；旋钮按设备像素吸附，容差半个像素。
				wantX := 20 + 2 + 2 + 20*e
				if got := knobX(h); math.Abs(got-wantX) > 0.55 {
					t.Errorf("%v：旋钮左缘 %.2f，期望 %.2f（e=%.3f）", elapsed, got, wantX, e)
				}
				col, _, ok := fillOf(h, 48, 28)
				if !ok {
					t.Fatalf("%v：找不到轨道", elapsed)
				}
				colorNear(t, elapsed.String()+" 轨道底色", col, MixToken(th.Muted, th.Primary, float32(e)), 0.012)
			}
		})
	}
}

// 深色下未选中的开关用主色的半透明（primary/20、边框 primary/40、旋钮 primary/60），不是浅色的 muted。
func TestSwitchDarkOffColors(t *testing.T) {
	spec := specFor(MotionLevelOff)
	spec.Mode = tokens.ModeDark
	h, _ := switchPage(t, spec)
	th := h.look.T
	col, _, ok := fillOf(h, 48, 28)
	if !ok {
		t.Fatal("找不到轨道")
	}
	colorNear(t, "深色未选中轨道", col, th.Primary.WithAlpha(0.2), 0.012)
	knob, _, ok := fillOf(h, 20, 20)
	if !ok {
		t.Fatal("找不到旋钮")
	}
	colorNear(t, "深色未选中旋钮", knob, th.Primary.WithAlpha(0.6), 0.012)
}

// 键盘聚焦的开关长出聚焦环（全局 button:focus-visible 的 ring-2 ring-primary/50）：
// 环在轨道外侧、宽度 0→2px、不透明度 0→0.5，随 duration-base 补间；鼠标点击不出环。
func TestSwitchFocusRing(t *testing.T) {
	h, _ := switchPage(t, specFor(MotionLevelStandard))
	th := h.look.T
	bg := th.Background
	// 轨道在 (20,20)：上缘外两行像素 y=19、y=18 是环，y=17 是页面底色。
	above := func(y float32) [4]uint8 { return pixelAt(h, 44, y) }
	expectPixel(t, "聚焦前 y=19", above(19), rgbOf(bg, bg), 1.5)

	h.keys("Tab")
	elapsed := time.Duration(0)
	for _, step := range []time.Duration{0, 90, 40, 60} {
		if step > 0 {
			advance(h, step*time.Millisecond)
			elapsed += step * time.Millisecond
		}
		e := progress(easeStandard, elapsed, 180*time.Millisecond)
		width := 2 * e
		alpha := 0.5 * e
		// 紧贴轨道的一行像素（y=19）被环覆盖的比例 = min(1, 宽度)，再外一行（y=18）是 宽度 超出 1px 的部分（Chromium 对 spread 抗锯齿）。
		for _, row := range []struct {
			y   float32
			cov float64
		}{{19, math.Min(1, width)}, {18, math.Max(0, math.Min(1, width-1))}} {
			cov := row.cov * alpha
			want := rgbOf(bg, bg)
			for i, p := range []uint8{th.Primary.R, th.Primary.G, th.Primary.B} {
				want[i] = float64(p)*cov + want[i]*(1-cov)
			}
			expectPixel(t, fmt.Sprintf("%v y=%.0f", elapsed, row.y), above(row.y), want, 3)
		}
	}
	// 终态：y=19、y=18 都是半透明主色，y=17 仍是页面底色。
	final := rgbOf(th.Primary.WithAlpha(0.5), bg)
	expectPixel(t, "终态 y=18", above(18), final, 2)
	expectPixel(t, "终态 y=17", above(17), rgbOf(bg, bg), 1.5)
}

func TestSwitchMouseClickLeavesNoFocusRing(t *testing.T) {
	h, _ := switchPage(t, still())
	th := h.look.T
	h.click("开关")
	expectPixel(t, "鼠标点击后 y=19", pixelAt(h, 44, 19), rgbOf(th.Background, th.Background), 1.5)
}

// ---- SettingItem 行悬停 ----

// settingPage 是一页一行 SettingItem，右侧是开关；行铺满页宽 400，从 y=0 起。
func settingPage(t *testing.T, spec Spec) *harness {
	t.Helper()
	on := new(bool)
	return openPage(t, spec, 400, 200, func(l *Look, c ui.C) {
		l.SettingItem(c, SettingRow{Label: "后台任务", Description: "说明", Control: func() { l.Switch(c, on, "后台任务开关") }}, true)
	})
}

// 行悬停：底色 muted/20，transition-colors 150 ms（标准曲线，减弱档不缩放），关闭档直接到位。
func TestSettingItemHoverTransition(t *testing.T) {
	cases := []struct {
		name  string
		level MotionLevel
		dur   time.Duration
	}{
		{"标准档", MotionLevelStandard, 150 * time.Millisecond},
		{"减弱档", MotionLevelReduced, 150 * time.Millisecond},
		{"关闭档", MotionLevelOff, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := settingPage(t, specFor(tc.level))
			th := h.look.T
			bg := th.Background
			// 行里没有内容的一点（左侧文字与右侧开关之间）。
			if err := h.session.Move(200, 10); err != nil {
				t.Fatal(err)
			}
			h.frames(1)
			elapsed := time.Duration(0)
			for _, step := range []time.Duration{0, 30, 30, 30, 30, 60} {
				if step > 0 {
					advance(h, step*time.Millisecond)
					elapsed += step * time.Millisecond
				}
				e := 1.0
				if tc.dur > 0 {
					e = progress(easeStandard, elapsed, tc.dur)
				}
				want := rgbOf(th.Muted.WithAlpha(float32(0.2*e)), bg)
				expectPixel(t, elapsed.String()+" 行底色", pixelAt(h, 200, 10), want, 2)
			}
		})
	}
}

// 指针停在行内的开关上，行仍是悬停的（CSS 的 :hover 对祖先也成立）。
func TestSettingItemStaysHoveredOverItsControl(t *testing.T) {
	h := settingPage(t, still())
	th := h.look.T
	hit, ok := h.session.Hit("后台任务开关")
	if !ok {
		t.Fatal("找不到开关")
	}
	if err := h.session.Move(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	h.frames(2)
	want := rgbOf(th.Muted.WithAlpha(0.2), th.Background)
	expectPixel(t, "指针在开关上时行底色", pixelAt(h, 200, 10), want, 2)
}

// 指针停在行内任一种控件（开关、下拉触发器、按钮）上，整行都是悬停底色（CSS 的 :hover 对祖先也成立，台账 B-050），
// 且不改变控件自己的悬停与点击：控件仍是命中目标，点击照常触发。
func TestSettingItemHoveredOverInlineControls(t *testing.T) {
	type ctl struct {
		name string
		hit  string
		make func(l *Look, c ui.C, clicked *int)
	}
	ctls := []ctl{
		{"开关", "控件开关", func(l *Look, c ui.C, _ *int) { l.Switch(c, new(bool), "控件开关") }},
		{"下拉触发器", "控件下拉", func(l *Look, c ui.C, _ *int) { l.SelectTrigger(c, "控件下拉", "值", false, 140, false) }},
		{"按钮", "控件按钮", func(l *Look, c ui.C, clicked *int) {
			th := l.T
			b := l.Button(c.Key("btn"), ButtonSpec{Label: "控件按钮"}, ButtonStyle{
				Height: l.Rem(2.25), PadX: l.Space(3), Radius: l.Rem(0.75), Text: TextSM, Weight: WeightNormal,
				Fg: th.Foreground, Bg: th.Muted, Transition: DurationDefault,
			})
			if b.Clicked() {
				*clicked++
			}
		}},
	}
	for _, mode := range []tokens.Mode{tokens.ModeLight, tokens.ModeDark} {
		for _, k := range ctls {
			t.Run(fmt.Sprintf("%v/%s", mode, k.name), func(t *testing.T) {
				spec := still()
				spec.Mode = mode
				clicked := 0
				h := openPage(t, spec, 400, 200, func(l *Look, c ui.C) {
					l.SettingItem(c, SettingRow{Label: "后台任务", Description: "说明", Control: func() { k.make(l, c, &clicked) }}, true)
				})
				th := h.look.T
				before := rgbOf(th.Muted.WithAlpha(0), th.Background)
				expectPixel(t, "指针不在时行底色", pixelAt(h, 200, 10), before, 2)
				hit, ok := h.session.Hit(k.hit)
				if !ok {
					t.Fatalf("找不到 %s", k.hit)
				}
				if err := h.session.Move(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				h.frames(3)
				want := rgbOf(th.Muted.WithAlpha(0.2), th.Background)
				expectPixel(t, "指针在控件上时行底色", pixelAt(h, 200, 10), want, 1)
				// 指针离开行（页面下方的空白处）：底色撤掉。
				if err := h.session.Move(200, 150); err != nil {
					t.Fatal(err)
				}
				h.frames(3)
				expectPixel(t, "指针离开后行底色", pixelAt(h, 200, 10), before, 2)
				if k.name == "按钮" {
					h.click(k.hit)
					if clicked != 1 {
						t.Errorf("行的悬停判定不应吃掉控件的点击：按钮点击次数 %d", clicked)
					}
				}
			})
		}
	}
}

// ---- 分段按钮 ----

// segmentPage 是一排三枚分段按钮（甲乙丙），初始选中 sel；点击改选中。按钮网格从 (20,20) 起、宽 760，三格各 245.33。
func segmentPage(t *testing.T, spec Spec, sel *int) *harness {
	t.Helper()
	return openPage(t, spec, 800, 300, func(l *Look, c ui.C) {
		ui.Column(c).FillWidth().Pad(20).Children(func() {
			l.SegmentGrid(c, func() {
				for i, name := range []string{"甲", "乙", "丙"} {
					seg := l.Segment(c.Key(name), SegmentSpec{Name: name, On: *sel == i, Content: func(fg tokens.Color) {
						l.TextPx(c, name, 13, 19.5, WeightMedium, fg)
					}})
					if seg.Clicked() {
						*sel = i
					}
				}
			})
		})
	})
}

// segFill 是名为 name 的分段按钮的底（宽约 245、高 41.5 的实心圆角矩形）；col 是它当前的颜色。
func segFill(h *harness, name string) (col gfx.Color, ok bool) {
	hit, found := h.session.Hit(name)
	if !found {
		return gfx.Color{}, false
	}
	for _, p := range h.session.Snapshot().Primitives {
		if p.Kind == 0 && math.Abs(float64(p.Bounds.Y-hit.Bounds.Y)) < 1.01 && p.Bounds.Width > 240 && p.Bounds.Width < 250 &&
			p.Bounds.X > hit.Bounds.X-1 && p.Bounds.X < hit.Bounds.X+1 {
			return p.Color, true
		}
	}
	return gfx.Color{}, false
}

// textColorOf 是当前帧里文字 value 的颜色。
func textColorOf(h *harness, value string) (gfx.Color, bool) {
	for _, tx := range h.session.Snapshot().Texts {
		if tx.Value == value {
			return tx.Style.Color, true
		}
	}
	return gfx.Color{}, false
}

// centerOf 是命中区的中心点。
func hitCenter(t *testing.T, h *harness, name string) (x, y float32) {
	t.Helper()
	hit, ok := h.session.Hit(name)
	if !ok {
		t.Fatalf("找不到 %q", name)
	}
	return hit.Bounds.X + hit.Bounds.Width/2, hit.Bounds.Y + hit.Bounds.Height/2
}

// 悬停的未选中按钮整体上移 2px（hover:-translate-y-0.5），上移在未选中项上没有过渡、瞬间到位；
// 网格里其它按钮的位置与版面不动。
func TestSegmentHoverLiftUnselected(t *testing.T) {
	sel := 0
	h := segmentPage(t, specFor(MotionLevelStandard), &sel)
	before, _ := h.session.Hit("乙")
	other, _ := h.session.Hit("丙")
	x, y := hitCenter(t, h, "乙")
	if err := h.session.Move(x, y); err != nil {
		t.Fatal(err)
	}
	h.frames(1) // 时钟没走：没有过渡的量必须已经到位
	after, _ := h.session.Hit("乙")
	if got := before.Bounds.Y - after.Bounds.Y; math.Abs(float64(got)-2) > 0.01 {
		t.Errorf("悬停后上移 %.2f px，期望 2（%v → %v）", got, before.Bounds, after.Bounds)
	}
	if after.Bounds.Height != before.Bounds.Height || after.Bounds.X != before.Bounds.X {
		t.Errorf("上移只改 y：%v → %v", before.Bounds, after.Bounds)
	}
	if now, _ := h.session.Hit("丙"); now.Bounds != other.Bounds {
		t.Errorf("别的按钮不该动：%v → %v", other.Bounds, now.Bounds)
	}
}

// 选中的按钮悬停也上移 2px，但它的 transition 是 `all 180ms`，上移走标准曲线；按下（:active）时回到原位。
func TestSegmentHoverLiftSelectedIsAnimated(t *testing.T) {
	sel := 0
	h := segmentPage(t, specFor(MotionLevelStandard), &sel)
	rest, _ := h.session.Hit("甲")
	x, y := hitCenter(t, h, "甲")
	if err := h.session.Move(x, y); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	elapsed := time.Duration(0)
	for _, step := range []time.Duration{0, 30, 30, 30, 30, 60} {
		if step > 0 {
			advance(h, step*time.Millisecond)
			elapsed += step * time.Millisecond
		}
		hit, _ := h.session.Hit("甲")
		want := 2 * progress(easeStandard, elapsed, 180*time.Millisecond)
		if got := float64(rest.Bounds.Y - hit.Bounds.Y); math.Abs(got-want) > 0.05 {
			t.Errorf("%v：上移 %.3f px，期望 %.3f", elapsed, got, want)
		}
	}
}

// 按下时未选中的按钮立刻回到原位（:active 的 translate-y-0，没有过渡），松开后若指针还在，再上移。
func TestSegmentPressedDropsTheLift(t *testing.T) {
	sel := 0
	h := segmentPage(t, specFor(MotionLevelStandard), &sel)
	rest, _ := h.session.Hit("乙")
	x, y := hitCenter(t, h, "乙")
	if err := h.session.Move(x, y); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	if hit, _ := h.session.Hit("乙"); hit.Bounds.Y >= rest.Bounds.Y {
		t.Fatal("悬停应已上移")
	}
	if err := h.session.Press(x, y); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	if hit, _ := h.session.Hit("乙"); math.Abs(float64(hit.Bounds.Y-rest.Bounds.Y)) > 0.01 {
		t.Errorf("按下后应回到原位 y=%.2f，实际 %.2f", rest.Bounds.Y, hit.Bounds.Y)
	}
}

// 点另一枚：新选中的底色走 transition-all duration-base（180 ms、标准曲线），
// 文字色也在同一条过渡里；原选中项变成未选中：底色走 .app-settings-subtle 的 160 ms ease，文字色直接切换。
func TestSegmentSelectionTransition(t *testing.T) {
	sel := 0
	h := segmentPage(t, specFor(MotionLevelStandard), &sel)
	th := h.look.T
	x, y := hitCenter(t, h, "乙")
	if err := h.session.Click(x, y); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	if sel != 1 {
		t.Fatalf("点击后应选中乙，实际 %d", sel)
	}
	// 指针停在乙上：乙悬停（上移）、甲不悬停。
	elapsed := time.Duration(0)
	for _, step := range []time.Duration{0, 20, 30, 30, 40, 40, 40} {
		if step > 0 {
			advance(h, step*time.Millisecond)
			elapsed += step * time.Millisecond
		}
		eIn := progress(easeStandard, elapsed, 180*time.Millisecond)
		eOut := progress(easeCSS, elapsed, 160*time.Millisecond)

		bgIn, ok := segFill(h, "乙")
		if !ok {
			t.Fatalf("%v：找不到乙的底", elapsed)
		}
		colorNear(t, elapsed.String()+" 乙底色", bgIn, MixToken(th.SettingsSubtleBg, th.Primary, float32(eIn)), 0.012)
		fgIn, _ := textColorOf(h, "乙")
		colorNear(t, elapsed.String()+" 乙文字色", fgIn, MixToken(th.Foreground, th.PrimaryForeground, float32(eIn)), 0.012)

		bgOut, ok := segFill(h, "甲")
		if !ok {
			t.Fatalf("%v：找不到甲的底", elapsed)
		}
		colorNear(t, elapsed.String()+" 甲底色", bgOut, MixToken(th.Primary, th.SettingsSubtleBg, float32(eOut)), 0.012)
		fgOut, _ := textColorOf(h, "甲")
		colorNear(t, elapsed.String()+" 甲文字色（直接切换）", fgOut, th.Foreground, 0.012)
	}
}

// 减弱档：选中项的 duration-base 缩成 110 ms；未选中的 160 ms 是写死的毫秒数，不缩放。
func TestSegmentSelectionTransitionReduced(t *testing.T) {
	sel := 0
	h := segmentPage(t, specFor(MotionLevelReduced), &sel)
	th := h.look.T
	x, y := hitCenter(t, h, "乙")
	if err := h.session.Click(x, y); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	advance(h, 55*time.Millisecond)
	bgIn, _ := segFill(h, "乙")
	colorNear(t, "乙底色（减弱档 55ms）", bgIn, MixToken(th.SettingsSubtleBg, th.Primary, float32(progress(easeStandard, 55*time.Millisecond, 110*time.Millisecond))), 0.012)
	bgOut, _ := segFill(h, "甲")
	colorNear(t, "甲底色（减弱档 55ms，仍是 160ms 的 ease）", bgOut, MixToken(th.Primary, th.SettingsSubtleBg, float32(progress(easeCSS, 55*time.Millisecond, 160*time.Millisecond))), 0.012)
}

// 关闭档：点击后第一帧就是终态。
func TestSegmentSelectionOff(t *testing.T) {
	sel := 0
	h := segmentPage(t, still(), &sel)
	th := h.look.T
	h.click("乙")
	bgIn, _ := segFill(h, "乙")
	colorNear(t, "乙底色", bgIn, th.Primary, 0.012)
	bgOut, _ := segFill(h, "甲")
	colorNear(t, "甲底色", bgOut, th.SettingsSubtleBg, 0.012)
}

// 键盘聚焦的分段按钮长出聚焦环（ring-2 ring-primary/50，在按钮外侧），不是鼠标点击后留下的。
func TestSegmentFocusRing(t *testing.T) {
	sel := 2 // 选中丙：Tab 落在未选中的甲上，它上缘没有选中阴影的干扰
	h := segmentPage(t, still(), &sel)
	th := h.look.T
	hit, _ := h.session.Hit("甲")
	above := func(dy float32) [4]uint8 { return pixelAt(h, hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+dy) }
	expectPixel(t, "聚焦前", above(-1), rgbOf(th.Background, th.Background), 1.5)
	h.keys("Tab")
	expectPixel(t, "聚焦后紧邻上缘", above(-1), rgbOf(th.Primary.WithAlpha(0.5), th.Background), 2)
	expectPixel(t, "聚焦后上缘外第二行", above(-2), rgbOf(th.Primary.WithAlpha(0.5), th.Background), 2)
	expectPixel(t, "聚焦环之外", above(-3), rgbOf(th.Background, th.Background), 1.5)
}

// 悬停上移后命中区跟着上移 2px：指针停在原位置下沿内侧 1px 时，上移后指针落在新位置之外。
// 渲染层（Chromium）在命中区变动后会补发一次假的鼠标移动，悬停因此每帧来回切换——实测 600 ms 内 100 次翻转
// （scripts/parity/tools/probe-lift-edge.mjs，读数在 probes/lift-edge.json）。Viva 只在指针事件时重算悬停：
// 指针不动就不闪，每次真的移动才切换一次。这是已登记的偏差（台账 A-022，用户裁决接受，偏向更稳定的一侧），测试把现状钉住。
func TestSegmentLiftEdgeTogglesOnlyOnPointerMoves(t *testing.T) {
	sel := 0
	h := segmentPage(t, still(), &sel)
	hit, _ := h.session.Hit("乙")
	x := hit.Bounds.X + hit.Bounds.Width/2
	y := hit.Bounds.Y + hit.Bounds.Height - 1
	if err := h.session.Move(x, y); err != nil {
		t.Fatal(err)
	}
	var rows []float32
	for i := 0; i < 6; i++ { // 指针不动：多出几帧，位置必须稳定
		h.frames(1)
		now, _ := h.session.Hit("乙")
		rows = append(rows, now.Bounds.Y)
	}
	for i, v := range rows {
		if v != rows[0] {
			t.Fatalf("指针不动时第 %d 帧位置 %.2f 与第 0 帧 %.2f 不同：悬停在逐帧闪烁", i, v, rows[0])
		}
	}
	// 每次真的移动：悬停与上移各切换一次。
	last := rows[0]
	for i := 0; i < 4; i++ {
		if err := h.session.Move(x, y+float32(i%2)*0.1); err != nil {
			t.Fatal(err)
		}
		h.frames(2)
		now, _ := h.session.Hit("乙")
		if now.Bounds.Y == last {
			t.Errorf("第 %d 次移动后位置仍是 %.2f，期望切换", i, last)
		}
		last = now.Bounds.Y
	}
}

// ---- 色板 ----

// swatchPage 是一排两枚色板（红、蓝），sel 是选中的序号（-1 为都不选）；点击改选中。
func swatchPage(t *testing.T, spec Spec, sel *int) *harness {
	t.Helper()
	return openPage(t, spec, 400, 200, func(l *Look, c ui.C) {
		ui.Row(c).Pad(40).Gap(40).Children(func() {
			for i, name := range []string{"红", "蓝"} {
				fill := tokens.RGBA(220, 60, 60, 1)
				if i == 1 {
					fill = tokens.RGBA(60, 113, 221, 1)
				}
				if l.Swatch(c.Key(name), SwatchSpec{Name: name, Fill: fill, On: *sel == i, HoverOpacity: 0.9}).Clicked() {
					*sel = i
				}
			}
		})
	})
}

// above 是色板上缘外 dy 像素处的像素（水平居中）。
func swatchAbove(t *testing.T, h *harness, name string, dy float32) [4]uint8 {
	t.Helper()
	x, _ := hitCenter(t, h, name)
	hit, _ := h.session.Hit(name)
	return pixelAt(h, x, hit.Bounds.Y-dy)
}

// 选中的色板有两层环：ring-offset-2（页面底色，里面 2px）与 ring-2（主色，再外 2px，合计离色板 4px）。
func TestSwatchSelectedRing(t *testing.T) {
	sel := 0
	h := swatchPage(t, still(), &sel)
	th := h.look.T
	bg := rgbOf(th.Background, th.Background)
	expectPixel(t, "偏移环（1px 处）", swatchAbove(t, h, "红", 1), bg, 1.5)
	expectPixel(t, "主色环（3px 处）", swatchAbove(t, h, "红", 3), rgbOf(th.Primary, th.Background), 1.5)
	expectPixel(t, "环之外（5px 处）", swatchAbove(t, h, "红", 5), bg, 1.5)
	expectPixel(t, "未选中的色板没有环", swatchAbove(t, h, "蓝", 3), bg, 1.5)
}

// 键盘聚焦时，全局的 button:focus-visible（ring-2 ring-primary/50 ring-offset-0）比色板自己的 ring 类优先级高：
// 整组环被取代——选中的色板聚焦后，选中环缩成一圈半透明的 2px 细环，不是叠加（渲染层实测，台账 B-004）。
func TestSwatchFocusRingReplacesTheSelectionRing(t *testing.T) {
	sel := 0
	h := swatchPage(t, still(), &sel)
	th := h.look.T
	h.keys("Tab") // 第一个可聚焦的是选中的红
	half := rgbOf(th.Primary.WithAlpha(0.5), th.Background)
	expectPixel(t, "聚焦环（1px 处）", swatchAbove(t, h, "红", 1), half, 2)
	expectPixel(t, "聚焦环（2px 处）", swatchAbove(t, h, "红", 2), half, 2)
	expectPixel(t, "选中环消失（3px 处）", swatchAbove(t, h, "红", 3), rgbOf(th.Background, th.Background), 1.5)

	// 未选中的色板聚焦：同样只有细环。
	h.keys("Tab")
	expectPixel(t, "未选中聚焦（1px 处）", swatchAbove(t, h, "蓝", 1), half, 2)
	expectPixel(t, "未选中聚焦（3px 处）", swatchAbove(t, h, "蓝", 3), rgbOf(th.Background, th.Background), 1.5)
}

// 点一枚未选中的色板：环从无长到选中环——ring-offset 与 ring 两层的宽度（0→2px、0→4px）和不透明度一起补间，
// 时长 duration-base、标准曲线。取距色板 2~3px 的一行（终态里属于主色环、不在偏移环里）：
// 主色环此刻宽 4e、不透明度 e，这一行被覆盖的比例是 clamp(4e-2, 0, 1)。
func TestSwatchSelectionRingGrows(t *testing.T) {
	sel := 0
	h := swatchPage(t, specFor(MotionLevelStandard), &sel)
	th := h.look.T
	x, y := hitCenter(t, h, "蓝")
	if err := h.session.Click(x, y); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	if sel != 1 {
		t.Fatalf("点击后应选中蓝，实际 %d", sel)
	}
	// 指针停在蓝上：蓝的 hover:opacity 在选中后不再适用，整枚元素不透明度回到 1，对环无影响。
	elapsed := time.Duration(0)
	for _, step := range []time.Duration{0, 30, 30, 30, 30, 60} {
		if step > 0 {
			advance(h, step*time.Millisecond)
			elapsed += step * time.Millisecond
		}
		e := progress(easeStandard, elapsed, 180*time.Millisecond)
		cov := math.Max(0, math.Min(1, 4*e-2)) * e
		want := rgbOf(th.Background, th.Background)
		for i, p := range []uint8{th.Primary.R, th.Primary.G, th.Primary.B} {
			want[i] = float64(p)*cov + want[i]*(1-cov)
		}
		// 描边的最外一行像素 Viva 只按约 96% 覆盖（三角形外沿的羽化），终态下主色环外沿也是 (35,75,156) 而不是 (27,68,152)，所以容差取 10 级。
		expectPixel(t, elapsed.String()+" 距色板 3px 的一行", swatchAbove(t, h, "蓝", 3), want, 10)
	}
}

// 悬停未选中的色板：整枚 hover:opacity-90（transition-all duration-base），选中的色板不变淡。
func TestSwatchHoverOpacity(t *testing.T) {
	sel := 0
	h := swatchPage(t, specFor(MotionLevelStandard), &sel)
	th := h.look.T
	fill := tokens.RGBA(60, 113, 221, 1)
	x, y := hitCenter(t, h, "蓝")
	if err := h.session.Move(x, y); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	elapsed := time.Duration(0)
	for _, step := range []time.Duration{0, 30, 30, 60, 60} {
		if step > 0 {
			advance(h, step*time.Millisecond)
			elapsed += step * time.Millisecond
		}
		op := 1 - 0.1*progress(easeStandard, elapsed, 180*time.Millisecond)
		want := rgbOf(fill.WithAlpha(float32(op)), th.Background)
		expectPixel(t, elapsed.String()+" 色板中心", pixelAt(h, x, y), want, 2)
	}
	// 选中的色板悬停不变淡。
	x0, y0 := hitCenter(t, h, "红")
	if err := h.session.Move(x0, y0); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	advance(h, 300*time.Millisecond)
	// 中心有对勾：取中心偏左的一点避开图标（图标 16px，色板 40px，偏左 14px 在图标之外）。
	expectPixel(t, "选中的色板悬停", pixelAt(h, x0-14, y0), rgbOf(tokens.RGBA(220, 60, 60, 1), th.Background), 2)
}

// 悬停未选中的按钮多出一层 shadow（0 1px 3px 0 / 0 1px 2px -1px，黑 10%），随 .app-settings-subtle 的 160 ms ease 长出来：
// 终态之前单调变深，160 ms 时到位。（阴影的几何也在补间，中间值没有简单的解析式，这里验证端点与单调。）
func TestSegmentHoverShadowGrows(t *testing.T) {
	sel := 0
	h := segmentPage(t, specFor(MotionLevelStandard), &sel)
	hit, _ := h.session.Hit("乙")
	x := hit.Bounds.X + hit.Bounds.Width/2
	// 上移 2px 之后按钮下沿是 hit.Bounds.Y-2+41.5；取下沿外 1.5px 的一行像素（落在阴影的偏移与模糊范围内）。
	below := func() [4]uint8 { return pixelAt(h, x, hit.Bounds.Y-2+hit.Bounds.Height+1.5) }
	bgR := int(rgbOf(h.look.T.Background, h.look.T.Background)[0])
	cx, cy := hitCenter(t, h, "乙")
	if err := h.session.Move(cx, cy); err != nil {
		t.Fatal(err)
	}
	h.frames(1)
	prev := bgR // 按钮上移之后这行像素落在按钮之外：t=0 时还没有阴影，是页面底色
	elapsed := time.Duration(0)
	for _, step := range []time.Duration{0, 20, 20, 40, 40, 40, 60} {
		if step > 0 {
			advance(h, step*time.Millisecond)
			elapsed += step * time.Millisecond
		}
		got := int(below()[0])
		if got > prev {
			t.Errorf("%v：下沿外像素 R=%d 比前一次 %d 更亮，阴影应单调变深", elapsed, got, prev)
		}
		prev = got
	}
	if final := int(below()[0]); final >= bgR {
		t.Errorf("终态下沿外像素 R=%d 应比页面底色 %d 更暗（出现了阴影）", final, bgR)
	}
	// 到位时间：160 ms 之后不再变化。
	at160 := int(below()[0])
	advance(h, 100*time.Millisecond)
	if after := int(below()[0]); after != at160 {
		t.Errorf("200 ms 与 260 ms 的读数不同（%d vs %d）：阴影应在 160 ms 内到位", at160, after)
	}
}
