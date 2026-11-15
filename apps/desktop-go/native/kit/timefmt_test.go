package kit

import (
	"testing"
	"time"
)

func TestFormatItemTime(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, loc)
	at := func(y int, m time.Month, d, h, mi int) int64 {
		return time.Date(y, m, d, h, mi, 0, 0, loc).UnixMilli()
	}
	cases := []struct {
		name, locale string
		ms           int64
		want         string
	}{
		{"zh 当天", "zh-Hans", at(2026, 11, 15, 9, 5), "09:05"},
		{"zh 当天下午", "zh-Hans", at(2026, 11, 15, 14, 30), "14:30"},
		{"en 当天", "en", at(2026, 11, 15, 14, 30), "02:30 PM"},
		{"zh 同年", "zh-Hans", at(2026, 3, 7, 8, 0), "3月7日"},
		{"en 同年", "en", at(2026, 3, 7, 8, 0), "Mar 7"},
		{"zh 跨年", "zh-Hans", at(2025, 11, 7, 8, 0), "2025/11/7"},
		{"en 跨年", "en", at(2025, 11, 7, 8, 0), "11/7/2025"},
		// 当天零点边界：23:59 与次日 00:00 不是同一天
		{"zh 昨天深夜", "zh-Hans", at(2026, 11, 14, 23, 59), "11月14日"},
	}
	for _, c := range cases {
		if got := FormatItemTime(c.ms, now, c.locale); got != c.want {
			t.Errorf("%s：得到 %q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int64]string{
		-5:         "0:00",
		0:          "0:00",
		999:        "0:00",
		1000:       "0:01",
		187_000:    "3:07",
		3_599_000:  "59:59",
		3_600_000:  "1:00:00",
		36_125_000: "10:02:05",
	}
	for ms, want := range cases {
		if got := FormatDuration(ms); got != want {
			t.Errorf("FormatDuration(%d) = %q，期望 %q", ms, got, want)
		}
	}
}
