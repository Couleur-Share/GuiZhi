package kit

import (
	"fmt"
	"testing"
	"time"
)

// FormatDate 对应 toLocaleDateString()：只有日期、数字不补零、zh 年在前、en 月在前；时区决定落在哪一天。
func TestFormatDate(t *testing.T) {
	t.Parallel()
	utc := time.UTC
	east8 := time.FixedZone("UTC+8", 8*3600)
	ms := func(y int, m time.Month, d, h int, loc *time.Location) int64 {
		return time.Date(y, m, d, h, 0, 0, 0, loc).UnixMilli()
	}
	for _, tc := range []struct {
		name   string
		ms     int64
		loc    *time.Location
		locale DateLocale
		want   string
	}{
		{"zh 不补零", ms(2026, time.March, 5, 12, utc), utc, DateLocaleZH, "2026/3/5"},
		{"en 月在前", ms(2026, time.March, 5, 12, utc), utc, DateLocaleEN, "3/5/2026"},
		{"两位的月日", ms(2026, time.November, 16, 12, utc), utc, DateLocaleZH, "2026/11/16"},
		{"空区域按 zh", ms(2026, time.November, 16, 12, utc), utc, "", "2026/11/16"},
		{"东八区跨日（UTC 22 点是次日 6 点）", ms(2023, time.November, 14, 22, utc), east8, DateLocaleZH, "2023/11/15"},
		{"同一时刻 UTC 仍是前一天", ms(2023, time.November, 14, 22, utc), utc, DateLocaleEN, "11/14/2023"},
		{"年末跨年", ms(2025, time.December, 31, 20, utc), east8, DateLocaleEN, "1/1/2026"},
	} {
		if got := FormatDate(tc.ms, tc.loc, tc.locale); got != tc.want {
			t.Errorf("%s：FormatDate = %q，应为 %q", tc.name, got, tc.want)
		}
	}
}

// loc 为空时取本机时区（与渲染层「浏览器的当前时区」同义）。
func TestFormatDateWithoutALocationUsesTheLocalZone(t *testing.T) {
	t.Parallel()
	now := time.Now()
	local := now.In(time.Local)
	want := fmt.Sprintf("%d/%d/%d", local.Year(), int(local.Month()), local.Day())
	if got := FormatDate(now.UnixMilli(), nil, DateLocaleZH); got != want {
		t.Fatalf("FormatDate(nil) = %q，应为 %q", got, want)
	}
}

// FormatDateTime 的两套格式（评论的发布日期与它共用同一套日期写法，这里一并守住）。
func TestFormatDateTime(t *testing.T) {
	t.Parallel()
	utc := time.UTC
	at := time.Date(2026, time.November, 16, 14, 5, 9, 0, utc).UnixMilli()
	if got, want := FormatDateTime(at, utc, DateLocaleZH), "2026/11/16 14:05:09"; got != want {
		t.Errorf("zh：%q，应为 %q", got, want)
	}
	if got, want := FormatDateTime(at, utc, DateLocaleEN), "11/16/2026, 2:05:09 PM"; got != want {
		t.Errorf("en：%q，应为 %q", got, want)
	}
}
