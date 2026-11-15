package kit

import (
	"testing"
	"time"
)

func TestResolveMotion(t *testing.T) {
	cases := []struct {
		pref     MotionPref
		osReduce bool
		want     MotionLevel
	}{
		{MotionSystem, false, MotionLevelStandard},
		{MotionSystem, true, MotionLevelOff},        // 系统要求减少动态效果
		{MotionStandard, true, MotionLevelStandard}, // 应用内明确选了标准：无视系统
		{MotionStandard, false, MotionLevelStandard},
		{MotionOff, false, MotionLevelOff},
		{MotionOff, true, MotionLevelOff},
		{MotionReduced, false, MotionLevelReduced},
		{MotionReduced, true, MotionLevelOff}, // CSS 里系统降级对 reduced 同样生效
		{"", false, MotionLevelStandard},      // 未知值按跟随系统
	}
	for _, c := range cases {
		if got := ResolveMotion(c.pref, c.osReduce); got != c.want {
			t.Errorf("ResolveMotion(%q, %v) = %v，期望 %v", c.pref, c.osReduce, got, c.want)
		}
	}
}

func TestMotionDurationsMatchCSS(t *testing.T) {
	// 值来自 globals.css 的 --motion-duration-*（标准档）与 html[data-motion="reduced"]（减弱档）
	std := []time.Duration{80, 120, 180, 280, 420}
	red := []time.Duration{50, 80, 110, 170, 250}
	for i := DurationInstant; i <= DurationSlow; i++ {
		if got := MotionLevelStandard.Duration(i); got != std[i]*time.Millisecond {
			t.Errorf("标准档 %d = %v", i, got)
		}
		if got := MotionLevelReduced.Duration(i); got != red[i]*time.Millisecond {
			t.Errorf("减弱档 %d = %v", i, got)
		}
		if got := MotionLevelOff.Duration(i); got != 0 {
			t.Errorf("关闭档 %d = %v，应为 0", i, got)
		}
	}
	if MotionLevelStandard.Duration(DurationName(99)) != 0 || MotionLevelStandard.Duration(-1) != 0 {
		t.Error("越界档位应返回 0")
	}
	if !MotionLevelReduced.Animated() || MotionLevelOff.Animated() {
		t.Error("Animated 与档位不符")
	}
}

func TestLookFollowsOSReduceMotion(t *testing.T) {
	spec := DefaultThemeSpec()
	if got := NewLook(spec, Env{}).Level; got != MotionLevelStandard {
		t.Fatalf("默认应为标准档，实际 %v", got)
	}
	var env Env
	env.Prefs.ReduceMotion = true
	if got := NewLook(spec, env).Level; got != MotionLevelOff {
		t.Fatalf("系统要求减少动态效果时应关闭，实际 %v", got)
	}
}
