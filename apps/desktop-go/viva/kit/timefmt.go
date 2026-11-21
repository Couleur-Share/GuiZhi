package kit

import (
	"fmt"
	"strings"
	"time"
)

// DateLocale 是时间格式用的区域：渲染层的 toLocaleTimeString(undefined)、toLocaleDateString(undefined)
// 跟浏览器（即系统）的区域设置走，不跟应用语言走。这里只实现应用支持的两种语言对应的两套格式：
// 中文（zh-*）与其余一律按英文（en-US）。其它区域（日文、德文……）的格式没有复刻，见台账 B-011。
type DateLocale string

const (
	DateLocaleZH DateLocale = "zh-CN"
	DateLocaleEN DateLocale = "en-US"
)

// zh 报告是否按中文写法输出。空值取中文：界面默认语言是中文，调用方没接系统区域设置时，不能让中文界面里冒出 02:05 PM 这样的英文时间。
func (l DateLocale) zh() bool { return l == DateLocaleZH || l == "" }

// ParseDateLocale 把系统区域名（zh-CN、en-GB、de-DE……）归到上面两套格式之一。
func ParseDateLocale(tag string) DateLocale {
	if strings.HasPrefix(strings.ToLower(tag), "zh") {
		return DateLocaleZH
	}
	return DateLocaleEN
}

// FormatItemTime 对应渲染层 formatItemTime（library/type-meta.tsx）：当天只显示时分，同年显示「月日」，
// 跨年用全数字日期（比「2025年11月7日」短，窄列里不会被截断）。
//
//   - 当天：{hour: "2-digit", minute: "2-digit"} → zh 14:05、en 02:05 PM；
//   - 同年：{month: "short", day: "numeric"} → zh 11月7日、en Nov 7；
//   - 跨年：{year, month, day: "numeric"} → zh 2025/11/7、en 11/7/2025。
//
// ms 是毫秒时间戳；now 与 loc 决定「当天」「同年」与时区（渲染层用浏览器的当前时间与时区）。
func FormatItemTime(ms int64, now time.Time, loc *time.Location, locale DateLocale) string {
	if loc == nil {
		loc = time.Local
	}
	t, n := time.UnixMilli(ms).In(loc), now.In(loc)
	zh := locale.zh()
	switch {
	case sameDay(t, n):
		if zh {
			return t.Format("15:04")
		}
		return t.Format("03:04 PM")
	case t.Year() == n.Year():
		if zh {
			return fmt.Sprintf("%d\u6708%d\u65e5", int(t.Month()), t.Day())
		}
		return t.Format("Jan 2")
	default:
		if zh {
			return fmt.Sprintf("%d/%d/%d", t.Year(), int(t.Month()), t.Day())
		}
		return fmt.Sprintf("%d/%d/%d", int(t.Month()), t.Day(), t.Year())
	}
}

// FormatDateTime 对应渲染层 Date.prototype.toLocaleString()（不带参数）：日期加时间，用于提示气泡与详情里的时间。
// zh：2026/11/16 11:50:00；en：11/16/2026, 11:50:00 AM。
func FormatDateTime(ms int64, loc *time.Location, locale DateLocale) string {
	if loc == nil {
		loc = time.Local
	}
	t := time.UnixMilli(ms).In(loc)
	if locale.zh() {
		return fmt.Sprintf("%d/%d/%d %02d:%02d:%02d", t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
	}
	return fmt.Sprintf("%d/%d/%d, %s", int(t.Month()), t.Day(), t.Year(), t.Format("3:04:05 PM"))
}

// FormatDate 对应渲染层 Date.prototype.toLocaleDateString()（不带参数）：只有日期，用于评论的发布日期。
// zh：2026/11/16；en：11/16/2026。
func FormatDate(ms int64, loc *time.Location, locale DateLocale) string {
	if loc == nil {
		loc = time.Local
	}
	t := time.UnixMilli(ms).In(loc)
	if locale.zh() {
		return fmt.Sprintf("%d/%d/%d", t.Year(), int(t.Month()), t.Day())
	}
	return fmt.Sprintf("%d/%d/%d", int(t.Month()), t.Day(), t.Year())
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
