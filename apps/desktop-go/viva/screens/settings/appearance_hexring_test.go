package settings_test

import (
	"math"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// pixelOf 取窗口截图里某个逻辑像素的颜色（缩放 1）。
func (r *rig) pixelOf(x, y int) [3]float64 {
	r.t.Helper()
	img, err := r.s.Image()
	if err != nil {
		r.t.Fatalf("截图失败：%v", err)
	}
	cr, cg, cb, _ := img.At(x, y).RGBA()
	return [3]float64{float64(cr >> 8), float64(cg >> 8), float64(cb >> 8)}
}

// ringPixel 是在底色 bg 上盖了覆盖率 cov、不透明度 alpha 的主色之后的像素。
func ringPixel(bg [3]float64, primary tokens.Color, cov, alpha float64) [3]float64 {
	a := cov * alpha
	return [3]float64{
		bg[0]*(1-a) + float64(primary.R)*a,
		bg[1]*(1-a) + float64(primary.G)*a,
		bg[2]*(1-a) + float64(primary.B)*a,
	}
}

// 十六进制色值框聚焦时长出聚焦环：全局规则 input:focus-visible { ring-2 ring-primary/50 }，环在边框盒外侧；
// 文本框任何方式聚焦都匹配 :focus-visible，所以鼠标点进去也出环（按钮只有键盘聚焦才出）。
// 套着 .app-settings-input，box-shadow 走它自己写的 160 ms ease，环宽 0→2px 与不透明度 0→0.5 一起补间，
// 不随「减弱」档缩放（同 .app-settings-subtle），关闭档直接到位；失焦后环原样退回。
// 参照：react-st-t1 里 settings.appearance.hex-focus 的采集（主色 50% 的 2px 环，紧贴边框盒）。
func TestHexInputFocusRing(t *testing.T) {
	cases := []struct {
		name  string
		level kit.MotionLevel
		dur   time.Duration // 0 表示直接到位
	}{
		{"标准档", kit.MotionLevelStandard, 160 * time.Millisecond},
		{"减弱档", kit.MotionLevelReduced, 160 * time.Millisecond},
		{"关闭档", kit.MotionLevelOff, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, a := openAppearanceMotion(t, customStore(), tc.level, nil)
			// 页面一打开「自定义」面板就在入场（淡入 180 ms），环画在面板里会跟着淡：先让入场走完再量环。
			if err := r.s.Advance(time.Second); err != nil {
				t.Fatal(err)
			}
			r.frames(2)
			th := a.Look.T
			field, ok := r.node(gfx.RoleTextField, "#3b82f6")
			if !ok {
				t.Fatal("找不到十六进制色值框")
			}
			// 边框盒按像素取整落笔（与渲染层一致）：环的内缘就是取整后的上缘，往上一行、两行是环。
			top := int(math.Floor(float64(field.Bounds.Y) + 0.5))
			x := int(field.Bounds.X + field.Bounds.Width/2)
			bg := r.pixelOf(x, top-6) // 离边框盒足够远的同一块底色
			rows := func(elapsed time.Duration) {
				e := 1.0
				if tc.dur > 0 {
					e = cubicBezierY(0.25, 0.1, 0.25, 1, math.Min(float64(elapsed)/float64(tc.dur), 1))
				}
				width, alpha := 2*e, 0.5*e
				// 紧贴边框盒的一行被环覆盖的比例 = min(1, 环宽)，再外一行是环宽超出 1px 的部分。
				for _, row := range []struct {
					y   int
					cov float64
				}{{top - 1, math.Min(1, width)}, {top - 2, math.Max(0, math.Min(1, width-1))}} {
					want := ringPixel(bg, th.Primary, row.cov, alpha)
					got := r.pixelOf(x, row.y)
					for i := range want {
						if math.Abs(got[i]-want[i]) > 3 {
							t.Errorf("%v y=top%+d：像素 %v，期望 %.1f（e=%.3f）", elapsed, row.y-top, got, want, e)
							break
						}
					}
				}
			}

			for _, y := range []int{top - 1, top - 2} { // 聚焦前没有环
				if got := r.pixelOf(x, y); got != bg {
					t.Fatalf("聚焦前 y=top%+d：像素 %v，期望底色 %v", y-top, got, bg)
				}
			}
			r.clickNode(gfx.RoleTextField, "#3b82f6")
			if tc.dur == 0 {
				rows(0)
			} else {
				elapsed := time.Duration(0)
				for _, step := range []time.Duration{0, 40, 40, 40, 60} {
					if step > 0 {
						if err := r.s.Advance(step * time.Millisecond); err != nil {
							t.Fatal(err)
						}
						r.frames(1)
						elapsed += step * time.Millisecond
					}
					rows(elapsed)
				}
			}
			final := ringPixel(bg, th.Primary, 1, 0.5)
			if got := r.pixelOf(x, top-2); math.Abs(got[0]-final[0]) > 3 || math.Abs(got[2]-final[2]) > 3 {
				t.Fatalf("终态外一行：像素 %v，期望 %.1f", got, final)
			}

			// 点页面的空白处失焦：环原样退回（同一条过渡）。
			if err := r.s.Click(4, 4); err != nil {
				t.Fatal(err)
			}
			r.frames(3)
			if err := r.s.Advance(time.Second); err != nil {
				t.Fatal(err)
			}
			r.frames(2)
			for _, y := range []int{top - 1, top - 2} {
				got, want := r.pixelOf(x, y), ringPixel(bg, th.Primary, 0, 0)
				for i := range want {
					if math.Abs(got[i]-want[i]) > 1.5 {
						t.Errorf("失焦后 y=top%+d：像素 %v，期望底色 %.1f", y-top, got, want)
						break
					}
				}
			}
		})
	}
}
