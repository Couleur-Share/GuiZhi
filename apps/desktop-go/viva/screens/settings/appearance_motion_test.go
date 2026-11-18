package settings_test

import (
	"math"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// cubicBezierY 是 CSS cubic-bezier(x1, y1, x2, y2) 在时间进度 x 处的输出，用二分法解参数——
// 独立于 Viva 自己的缓动实现，当作渲染层动效的参照。
func cubicBezierY(x1, y1, x2, y2, x float64) float64 {
	bez := func(a, b, s float64) float64 { return 3*(1-s)*(1-s)*s*a + 3*(1-s)*s*s*b + s*s*s }
	lo, hi := 0.0, 1.0
	for i := 0; i < 60; i++ {
		mid := (lo + hi) / 2
		if bez(x1, x2, mid) < x {
			lo = mid
		} else {
			hi = mid
		}
	}
	return bez(y1, y2, (lo+hi)/2)
}

// textAlphaOf 是当前帧里文字 value 的不透明度（入场淡入时小于 1）；没有这行文字时 ok 为 false。
func textAlphaOf(r *rig, value string) (float32, bool) {
	for _, tx := range r.s.Snapshot().Texts {
		if tx.Value == value {
			return tx.Style.Color.Alpha, true
		}
	}
	return 0, false
}

// textTopOf 是当前帧里文字 value 的上缘（入场时随绘制一起平移）；没有这行文字时 ok 为 false。
func textTopOf(r *rig, value string) (float32, bool) {
	for _, tx := range r.s.Snapshot().Texts {
		if tx.Value == value {
			return tx.Bounds.Y, true
		}
	}
	return 0, false
}

// openCustomPanel 打开显示设置页并点「自定义」，返回刚展开面板的那一帧（时钟没动）。
func openCustomPanel(t *testing.T, level kit.MotionLevel) *rig {
	t.Helper()
	r, _ := openAppearanceMotion(t, sscreen.NewMemStore(nsettings.Default()), level, nil)
	r.clickNode(gfx.RoleButton, locales.SettingsCustomColor())
	return r
}

// 选中「自定义」后展开的面板有入场过渡：渲染层 `animate-in fade-in slide-in-from-bottom-2 duration-base`——
// 淡入的同时从下方 0.5rem（中档 8px）滑回原位；tailwindcss-animate 的 enter 关键帧没有指定曲线，用 CSS 默认的 ease；
// 时长是 duration-base（标准档 180 ms、减弱档 110 ms），关闭档直接到位。
// 版面不动（面板一出现就占好位置），滑动只是视觉变换：文字与表面一起平移、一起淡入。
// 参照读数：probes/states-*.json 里 customPanel 的 animationDuration 0.18 s、animationTimingFunction ease。
func TestCustomPanelEnterAnimation(t *testing.T) {
	cases := []struct {
		name  string
		level kit.MotionLevel
		dur   time.Duration // 0 表示直接到位
	}{
		{"标准档", kit.MotionLevelStandard, 180 * time.Millisecond},
		{"减弱档", kit.MotionLevelReduced, 110 * time.Millisecond},
		{"关闭档", kit.MotionLevelOff, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 终态位置：另开一个会话，入场走完之后再读。
			done := openCustomPanel(t, tc.level)
			if err := done.s.Advance(time.Second); err != nil {
				t.Fatal(err)
			}
			done.frames(2)
			finalTop, ok := textTopOf(done, locales.SettingsPrimary())
			if !ok {
				t.Fatal("入场走完后应有预览条文字")
			}
			if alpha, _ := textAlphaOf(done, locales.SettingsPrimary()); alpha < 0.999 {
				t.Fatalf("入场走完后文字应完全不透明，得到 %.3f", alpha)
			}

			r := openCustomPanel(t, tc.level)
			elapsed := time.Duration(0)
			for _, step := range []time.Duration{0, 20, 20, 30, 30, 30, 60} {
				if step > 0 {
					if err := r.s.Advance(step * time.Millisecond); err != nil {
						t.Fatal(err)
					}
					r.frames(1)
					elapsed += step * time.Millisecond
				}
				e := 1.0
				if tc.dur > 0 {
					e = cubicBezierY(0.25, 0.1, 0.25, 1, math.Min(float64(elapsed)/float64(tc.dur), 1))
				}
				top, ok := textTopOf(r, locales.SettingsPrimary())
				if !ok {
					t.Fatalf("%v：入场期间预览条文字应已在画", elapsed)
				}
				// 从下方 8px 滑回：位置 = 终态 + 8·(1-e)。
				if got, want := float64(top-finalTop), 8*(1-e); math.Abs(got-want) > 0.6 { // 文字按整像素落笔，位置最多差半个像素
					t.Errorf("%v：比终态低 %.2f px，期望 %.2f（e=%.3f）", elapsed, got, want, e)
				}
				alpha, _ := textAlphaOf(r, locales.SettingsPrimary())
				if math.Abs(float64(alpha)-e) > 0.03 {
					t.Errorf("%v：不透明度 %.3f，期望 %.3f", elapsed, alpha, e)
				}
			}
		})
	}
}
