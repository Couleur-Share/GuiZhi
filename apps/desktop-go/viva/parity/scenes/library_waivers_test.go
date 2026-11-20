package scenes

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
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

// TestSourceEllipsisWaiverMatchesOnlyTheReactCut 守住 B-046 (7)(a) 的登记：Subject 要对得上 L1 报告里带引号的写法
// （「YouTu…」），并且只放过渲染层截成「YouTu…」这一种结果，别的截断点照常判定。
func TestSourceEllipsisWaiverMatchesOnlyTheReactCut(t *testing.T) {
	waivers := sourceEllipsisWaivers()
	matches := func(subject string) bool {
		for _, w := range waivers {
			if strings.Contains(subject, w.Subject) {
				return true
			}
		}
		return false
	}
	if !matches("「YouTu…」") {
		t.Errorf("渲染层截成「YouTu…」时应被登记放过（Subject 的写法与报告不一致？）")
	}
	for _, other := range []string{"「YouT…」", "「YouTub…」", "「YouTube」", "「Bilibili」"} {
		if matches(other) {
			t.Errorf("%s 没有登记，却被放过", other)
		}
	}
}

// TestEnglishSidebarHeadingWaiversMatchCatalog 守住 A-013 英文延伸的登记：Subject 里写死的英文大写标题要与 locales 目录里
// 英文界面真正画出来的字（`strings.ToUpper` 之后）一致；目录改了而这里没改，登记会悄悄匹配不上，英文变体里留下一片本该放过的失败。
// 本测试临时把进程语言切到英文，不并行、结束时还原（`TestScenesMatchReactFacts` 的并行子测试在它之前就已结束）。
func TestEnglishSidebarHeadingWaiversMatchCatalog(t *testing.T) {
	locales.SetLang(locales.En)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
	want := []string{
		strings.ToUpper(locales.LibrarySmartViews()), strings.ToUpper(locales.LibraryCollections()),
		strings.ToUpper(locales.LibraryPlatforms()), strings.ToUpper(locales.LibraryTags()),
	}
	if strings.Join(englishSidebarHeadings, "|") != strings.Join(want, "|") {
		t.Fatalf("englishSidebarHeadings = %q，目录里英文界面画出来的是 %q", englishSidebarHeadings, want)
	}
}

// TestEnglishHeadingWaiversAreMediumTierOnlyAndEnglishOnly 守住登记的范围：英文标题的字偶距放宽只在中档字号档、只对英文标题有效；
// 中文标题的登记照旧不带字号档限制，也没有被放宽。
func TestEnglishHeadingWaiversAreMediumTierOnlyAndEnglishOnly(t *testing.T) {
	width := func(ws []parity.Waiver) parity.Waiver {
		for _, w := range ws {
			if w.Kind == "text-width" {
				return w
			}
		}
		t.Fatal("没有 text-width 登记")
		return parity.Waiver{}
	}
	latin := width(headingLatinTrackingWaivers("TAGS"))
	zh := width(headingTrackingWaivers("标签"))
	if latin.Variant != "medium" {
		t.Errorf("英文标题的登记应只限中档字号档，Variant = %q", latin.Variant)
	}
	if zh.Variant != "" {
		t.Errorf("中文标题的登记不应带字号档限制，Variant = %q", zh.Variant)
	}
	// TAGS（4 个字符）：字距 0.525–0.675 px/字符 → [1.8, 3.0]，英文再放宽字偶距 2px → 上限 5.0；中文标题的上限照旧不放宽。
	if got, want := latin.MaxDelta, 4*0.675+0.3+latinHeadingKerningCap; math.Abs(got-want) > 1e-9 {
		t.Errorf("英文 TAGS 的宽度差上限 = %v，期望 %v", got, want)
	}
	if got, want := zh.MaxDelta, 2*0.675+0.3; math.Abs(got-want) > 1e-9 {
		t.Errorf("中文「标签」的宽度差上限 = %v，期望 %v（不应放宽）", got, want)
	}
	// 范围匹配：实测的四个英文标题宽度差（中档）都落在登记里，超过上限一点点的不放过，别的字号档不放过。
	cases := []struct {
		title   string
		delta   float64
		variant string
		want    bool
	}{
		{"SMART VIEWS", 7.62, "light-royal-blue-medium-1280x800", true},
		{"COLLECTIONS", 6.87, "light-royal-blue-medium-1280x800", true},
		{"PLATFORMS", 6.47, "light-royal-blue-medium-1280x800", true},
		{"TAGS", 3.84, "light-royal-blue-medium-1280x800", true},
		{"TAGS", 5.2, "light-royal-blue-medium-1280x800", false}, // 超过 字距上限 + 字偶距 2px
		{"TAGS", 1.0, "light-royal-blue-medium-1280x800", false}, // 小于字距应有的下限：是别的问题
		{"TAGS", 3.84, "light-royal-blue-small-1280x800", false}, // 小字号档没有实测，不放过
		{"TAGS", 3.84, "light-royal-blue-large-1280x800", false},
	}
	for _, c := range cases {
		is := parity.Issue{Layer: parity.LayerL2, Kind: "text-width", Subject: "「" + c.title + "」", Delta: c.delta}
		got := false
		for _, w := range headingLatinTrackingWaivers(c.title) {
			if w.MatchesIn(is, c.variant) {
				got = true
			}
		}
		if got != c.want {
			t.Errorf("%s 宽度差 %.2f（%s）：放过 = %v，期望 %v", c.title, c.delta, c.variant, got, c.want)
		}
	}
}

// TestEnglishTableHeaderWaiversMatchCatalog 守住英文表头登记的 Subject：写死的英文标签要与 locales 目录里英文界面画出的字一致
// （顺序同 headerInkWaivers 的中文标签）；目录改了而这里没改，登记会悄悄匹配不上。语言处理同上一个目录测试：不并行、结束时还原。
func TestEnglishTableHeaderWaiversMatchCatalog(t *testing.T) {
	locales.SetLang(locales.En)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
	want := []string{
		locales.LibraryColumnTitle(), locales.LibraryColumnSnippet(), locales.LibraryColumnTags(), locales.LibraryColumnType(),
		locales.LibraryColumnSource(), locales.LibraryColumnCollection(), locales.LibraryColumnStatus(),
		locales.LibraryColumnCreatedAt(), locales.LibraryColumnUpdatedAt(), locales.LibraryColumnActions(),
	}
	if strings.Join(englishTableHeaders, "|") != strings.Join(want, "|") {
		t.Fatalf("englishTableHeaders = %q，目录里英文界面画出来的是 %q", englishTableHeaders, want)
	}
}

// TestEnglishHeaderWaiversOnlyMediumTierAndOnlyTheirLabels 守住英文表头登记的范围：A-009 的墨迹总量登记只在中档、只放过偏多的方向；
// A-026 的字偶距登记只给「Tags」「Type」、只在中档，宽度差上限 2px；中文表头的登记不带字号档限制。
func TestEnglishHeaderWaiversOnlyMediumTierAndOnlyTheirLabels(t *testing.T) {
	const medium, small = "light-royal-blue-medium-1280x800", "light-royal-blue-small-1280x800"
	anyMatch := func(ws []parity.Waiver, is parity.Issue, variant string) bool {
		for _, w := range ws {
			if w.MatchesIn(is, variant) {
				return true
			}
		}
		return false
	}
	ink := headerInkWaivers()
	mass := func(label string, delta float64, dir int8) parity.Issue {
		return parity.Issue{Layer: parity.LayerL2, Kind: "ink-mass", Subject: "「" + label + "」", Delta: delta, Dir: dir}
	}
	cases := []struct {
		label   string
		delta   float64
		dir     int8
		variant string
		want    bool
	}{
		{"Actions", 0.169, 1, medium, true},   // 实测最大
		{"Title", 0.153, 1, medium, true},     // 实测最小
		{"Actions", 0.169, 1, small, false},   // 小字号档没有英文读数
		{"Actions", 0.169, -1, medium, false}, // 偏少的方向不放过
		{"Actions", 0.45, 1, medium, false},   // 超出 A-009 的范围
		{"操作", 0.169, 1, small, true},         // 中文表头不带字号档限制
	}
	for _, c := range cases {
		if got := anyMatch(ink, mass(c.label, c.delta, c.dir), c.variant); got != c.want {
			t.Errorf("%s 墨迹总量偏差 %.3f（方向 %d，%s）：放过 = %v，期望 %v", c.label, c.delta, c.dir, c.variant, got, c.want)
		}
	}
	kern := englishHeaderKerningWaivers()
	width := func(label string, delta float64) parity.Issue {
		return parity.Issue{Layer: parity.LayerL2, Kind: "text-width", Subject: "「" + label + "」", Delta: delta}
	}
	for _, c := range []struct {
		label   string
		delta   float64
		variant string
		want    bool
	}{
		{"Tags", 1.44, medium, true},
		{"Type", 1.27, medium, true},
		{"Tags", 2.5, medium, false},   // 超过 2px 上限
		{"Tags", 1.44, small, false},   // 只在中档
		{"Title", 1.44, medium, false}, // 没有字偶对的标签不登记
	} {
		if got := anyMatch(kern, width(c.label, c.delta), c.variant); got != c.want {
			t.Errorf("%s 宽度差 %.2f（%s）：放过 = %v，期望 %v", c.label, c.delta, c.variant, got, c.want)
		}
	}
}
