package extract

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reTZSuffix  = regexp.MustCompile(`[+-]\d\d:\d\d$`)
	reTZCompact = regexp.MustCompile(`([+-]\d\d)(\d\d)$`)
	reISOWithTZ = regexp.MustCompile(`^([+-]\d{6}|\d{4})-(\d\d)-(\d\d)[T ](\d\d):(\d\d)(?::(\d\d)(?:\.(\d+))?)?([+-])(\d\d):(\d\d)$`)
	reTrailingZ = regexp.MustCompile(`(?i)Z$`)
)

// publishedTime 对应 TS 的 publishedTime：只接受带时区的 ISO 时间，返回毫秒时间戳。
// Date.parse 的 V8 行为按实测复刻：日期溢出（02-30 → 03-01）与 24:00 顺延、空格可替代 T、毫秒之后的小数位截断；
// 不接受 RFC 2822 等遗留写法（V8 会接受，这里返回 nil，article:published_time 里几乎不会出现）。
func publishedTime(value string) *int64 {
	text := jsTrim(value)
	text = reTrailingZ.ReplaceAllString(text, "+00:00")
	text = reTZCompact.ReplaceAllString(text, "$1:$2")
	if text == "" || !reTZSuffix.MatchString(text) {
		return nil
	}
	m := reISOWithTZ.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	num := func(s string) int { n, _ := strconv.Atoi(s); return n }
	year := num(m[1])
	if strings.HasPrefix(m[1], "-") && year == 0 { // -000000 非法
		return nil
	}
	month, day, hour, minute, second := num(m[2]), num(m[3]), num(m[4]), num(m[5]), num(m[6])
	ms := 0
	if m[7] != "" {
		frac := (m[7] + "000")[:3]
		ms = num(frac)
	}
	offH, offM := num(m[9]), num(m[10])
	switch {
	case month < 1 || month > 12, day < 1 || day > 31, hour > 24, minute > 59, second > 59, offH > 23, offM > 59:
		return nil
	case hour == 24 && (minute != 0 || second != 0 || ms != 0):
		return nil
	}
	off := (offH*60 + offM) * 60
	if m[8] == "-" {
		off = -off
	}
	t := time.Date(year, time.Month(month), day, hour, minute, second, ms*int(time.Millisecond), time.FixedZone("", off))
	v := t.UnixMilli()
	return &v
}
