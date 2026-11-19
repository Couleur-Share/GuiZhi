package scenes

import (
	"fmt"
	"strings"
	"testing"
)

// TestWordmarkIconWaiverSubjectsMatchReportFormat 守住「登记的主题要对得上报告里的写法」。
//
// 图标问题的主题由 parity 的 fmtBox 写成 "图标 [x y w h]"（四个数都是两位小数）。登记里的 Subject 靠子串匹配，
// 写成别的样子不会报错，只是永远匹配不上，对照里就留着一片「本该登记」的失败：M2-2 加 12.25、15.75 px 两行时，
// 曾把边长拼成 "12.25.00"，整个表格 T2 多出两百来处本该放过的小红书图标墨迹量，是靠重跑最终二进制才发现的。
func TestWordmarkIconWaiverSubjectsMatchReportFormat(t *testing.T) {
	waivers := wordmarkIconWaivers()
	for _, size := range []float64{12.25, 14, 15.75, 16, 18} {
		subject := fmt.Sprintf("图标 [%.2f %.2f %.2f %.2f]", 715.27, 14.38, size, size)
		hits := 0
		for _, w := range waivers {
			if strings.Contains(subject, w.Subject) {
				hits++
			}
		}
		if hits == 0 {
			t.Errorf("边长 %.2f 的图标主题 %q 没有任何登记能匹配（Subject 的写法与报告不一致？）", size, subject)
		}
	}
	// 没有登记的边长不应被误放过：子串匹配不能因为数字相邻（112.25 与 12.25）而串味。
	for _, size := range []float64{2.25, 4, 13, 20, 112.25} {
		subject := fmt.Sprintf("图标 [%.2f %.2f %.2f %.2f]", 715.27, 14.38, size, size)
		for _, w := range waivers {
			if strings.Contains(subject, w.Subject) {
				t.Errorf("边长 %.2f 没有登记，却被 Subject %q 匹配", size, w.Subject)
			}
		}
	}
}
