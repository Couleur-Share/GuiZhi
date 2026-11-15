package kit

import "time"

// MotionPref 是设置里的动效偏好（motionPreference）。
type MotionPref string

const (
	MotionSystem   MotionPref = "system"   // 跟随系统：系统要求减少动态效果时近乎瞬切，否则标准
	MotionOff      MotionPref = "off"      // 关闭：近乎瞬切
	MotionReduced  MotionPref = "reduced"  // 减弱：时长约为标准的 60%
	MotionStandard MotionPref = "standard" // 标准：无视系统的减少动态效果
)

// MotionLevel 是偏好与系统「减少动态效果」合并后的实际档位。
type MotionLevel int

const (
	MotionLevelOff MotionLevel = iota
	MotionLevelReduced
	MotionLevelStandard
)

// ResolveMotion 复刻 globals.css 的合并规则：
//   - standard：始终标准，包括系统要求减少动态效果时（用户在应用内明确选了标准，CSS 用 :not([data-motion="standard"]) 让系统降级不生效）；
//   - off：近乎瞬切；
//   - reduced 与 system：系统要求减少动态效果时近乎瞬切（媒体查询对 :not([data-motion="standard"]) 生效），
//     否则 reduced 取减弱档、system 取标准档。
func ResolveMotion(pref MotionPref, osReduce bool) MotionLevel {
	switch pref {
	case MotionStandard:
		return MotionLevelStandard
	case MotionOff:
		return MotionLevelOff
	case MotionReduced:
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

// DurationName 是动效时长的档位名，对应 --motion-duration-*。
type DurationName int

const (
	DurationInstant DurationName = iota
	DurationQuick
	DurationBase
	DurationSmooth
	DurationSlow
)

// LoadingDelay 是加载指示器延迟登场的时长。本地 SQLite 走一趟通常几十毫秒就回来，
// 立刻画转圈再抹掉，这一下闪现本身就是闪烁；低于它人眼注意不到等待。
// 延迟不属于装饰，动效关闭时也不压缩。
const LoadingDelay = 250 * time.Millisecond

// Duration 返回档位在当前动效档下的时长；关闭档返回 0，调用方据此直接跳到终态而不起动画。
func (l MotionLevel) Duration(n DurationName) time.Duration {
	var ms *[len(motionStandardMs)]int
	switch l {
	case MotionLevelStandard:
		ms = &motionStandardMs
	case MotionLevelReduced:
		ms = &motionReducedMs
	default:
		return 0
	}
	if n < 0 || int(n) >= len(ms) {
		return 0
	}
	return time.Duration(ms[n]) * time.Millisecond
}

// Animated 报告是否应该起动画。
func (l MotionLevel) Animated() bool { return l != MotionLevelOff }
