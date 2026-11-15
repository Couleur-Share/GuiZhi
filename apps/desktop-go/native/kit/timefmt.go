package kit

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/i18n"
)

// FormatItemTime 对应渲染层 formatItemTime：当天只显示时分，同年显示「月日」，跨年用全数字日期
// （比「2025年11月7日」短，窄列里不会被截断）。格式由 MyGo 的 CLDR 数据给出，与 Intl 一致。
//
// 与渲染层的一处有意差异：渲染层用 toLocaleString(undefined)，跟系统区域设置走；这里跟应用语言走，
// 避免「应用设为英文、日期仍是中文格式」。locale 取 i18n.Locale() 的值（zh-Hans、en…）。
func FormatItemTime(ms int64, now time.Time, locale string) string {
	t := time.UnixMilli(ms).In(now.Location())
	switch {
	case sameDay(t, now):
		// {hour: "2-digit", minute: "2-digit"}：小时取该语言的制式，两位补零
		return i18n.FormatDate(locale, t, i18n.DateOptions{Skeleton: "jjmm"})
	case t.Year() == now.Year():
		// {month: "short", day: "numeric"}
		return i18n.FormatDate(locale, t, i18n.DateOptions{Skeleton: "MMMd"})
	default:
		// {year: "numeric", month: "numeric", day: "numeric"}
		return i18n.FormatDate(locale, t, i18n.DateOptions{Skeleton: "yMd"})
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// FormatDuration 把毫秒格式化为 M:SS / H:MM:SS，对应渲染层 formatDuration。
// 走时钟写法而不是「3 分 7 秒」，免掉一套单复数与中英差异的文案。
func FormatDuration(ms int64) string {
	total := ms / 1000
	if total < 0 {
		total = 0
	}
	sec, min, hour := total%60, (total/60)%60, total/3600
	if hour > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hour, min, sec)
	}
	return fmt.Sprintf("%d:%02d", min, sec)
}
