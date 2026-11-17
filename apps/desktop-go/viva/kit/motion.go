package kit

import (
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/tokens"
)

// 动效档位
//
// 设置里的「动效偏好」（system / off / reduced / standard）与系统的「减少动态效果」合并成实际档位，
// 规则复刻渲染层 globals.css：
//   - standard：始终标准，包括系统要求减少动态效果时（用户在应用内明确选了标准）；
//   - off：近乎瞬切；
//   - reduced 与 system：系统要求减少动态效果时近乎瞬切，否则 reduced 取减弱档、system 取标准档。
//
// 档位决定每一档时长（--motion-duration-*，见 tokens.MotionStandardMs / MotionReducedMs）；
// 关闭档时长为 0，调用方据此直接跳到终态而不起动画。

// MotionLevel 是偏好与系统「减少动态效果」合并后的实际档位。
type MotionLevel int

const (
	MotionLevelOff MotionLevel = iota
	MotionLevelReduced
	MotionLevelStandard
)

// ResolveMotion 把动效偏好与系统「减少动态效果」合并成实际档位。pref 取 "system"、"off"、"reduced"、"standard"，认不出的按 system。
func ResolveMotion(pref string, osReduce bool) MotionLevel {
	switch pref {
	case "standard":
		return MotionLevelStandard
	case "off":
		return MotionLevelOff
	case "reduced":
		if osReduce {
			return MotionLevelOff
		}
		return MotionLevelReduced
	default:
		if osReduce {
			return MotionLevelOff
		}
		return MotionLevelStandard
	}
}

// DurationName 是动效时长的档位名，对应 --motion-duration-*（Tailwind 的 duration-instant 等）。
type DurationName int

// DurationNone 表示没有过渡：渲染层的控件没写 transition-*，状态直接切换。
const DurationNone DurationName = -1

const (
	DurationInstant DurationName = iota // 80 ms：悬停变色
	DurationQuick                       // 120 ms：下拉、退场
	DurationBase                        // 180 ms：对话框入场、提示入场
	DurationSmooth                      // 280 ms
	DurationSlow                        // 420 ms
	// DurationDefault 是 Tailwind 自带的 transition 时长（150 ms）：`transition-colors`、`transition-opacity` 等不带 duration-* 的类用它。
	// 它不是 --motion-duration-* 变量，所以「减弱」档不缩放它（globals.css 只改变量），只有「关闭」档把它压成 0。
	DurationDefault
)

// tailwindDefaultDuration 是 DurationDefault 的时长。
const tailwindDefaultDuration = 150 * time.Millisecond

// LoadingDelay 是加载指示器延迟登场的时长。本地 SQLite 走一趟通常几十毫秒就回来，
// 立刻画转圈再抹掉，这一下闪现本身就是闪烁；低于它人眼注意不到等待。
// 延迟不属于装饰，动效关闭时也不压缩。
const LoadingDelay = 250 * time.Millisecond

// Duration 返回档位在当前动效档下的时长；关闭档返回 0。
func (m MotionLevel) Duration(n DurationName) time.Duration {
	if m == MotionLevelOff || n < 0 {
		return 0
	}
	if n == DurationDefault {
		return tailwindDefaultDuration
	}
	if int(n) >= len(tokens.MotionStandardMs) {
		return 0
	}
	return time.Duration(tokens.DurationMs(int(n), m == MotionLevelReduced)) * time.Millisecond
}

// Animated 报告是否应该起动画。
func (m MotionLevel) Animated() bool { return m != MotionLevelOff }

// Dur 是外观当前动效档下 n 档的时长。
func (l *Look) Dur(n DurationName) time.Duration { return l.Spec.Motion.Duration(n) }

// 渲染层 tailwind.config.js 的缓动：--motion-easing-standard / enter / exit，对应 Viva 内置的三条曲线。
// CSS 关键字 ease（animate-in 不带 ease-* 类时的默认曲线）是 gfx.EaseCSS。
const (
	EaseStandard = gfx.EaseInOut // cubic-bezier(0.4, 0, 0.2, 1)
	EaseEnter    = gfx.EaseOut   // cubic-bezier(0, 0, 0.2, 1)
	EaseExit     = gfx.EaseIn    // cubic-bezier(0.4, 0, 1, 1)
	EaseDefault  = gfx.EaseCSS   // ease：cubic-bezier(0.25, 0.1, 0.25, 1)
)

// Motion 是一次变化的时长与缓动。
func (l *Look) Motion(n DurationName, ease gfx.Easing) gfx.Motion {
	return gfx.Motion{Duration: l.Dur(n), Easing: ease}
}

// hoverTo 是悬停过渡：返回 0..1 的显示进度，目标由 hovered 决定。渲染层的 transition-colors 用 duration-instant、
// 默认曲线 cubic-bezier(0.4, 0, 0.2, 1)；slot 区分同一个命中 ID 上的多个动画量。
func (l *Look) hoverTo(c *gfx.Canvas, id gfx.ID, slot uint8, hovered bool, n DurationName) float32 {
	target := float32(0)
	if hovered {
		target = 1
	}
	return c.Animate(id, slot, target, l.Motion(n, EaseStandard))
}
