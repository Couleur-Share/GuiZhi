package scenes

import (
	"testing"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 写死的英文 Subject 与英文目录画出来的字一致（否则登记悄悄匹配不上）。语言是进程内的全局状态：结束时还原。
func TestEnglishToolsWaiverSubjectsMatchCatalog(t *testing.T) {
	locales.SetLang(locales.En)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
	if enToolsTags != locales.ArticleReaderTags() {
		t.Errorf("「Tags」登记写的是 %q，英文目录是 %q", enToolsTags, locales.ArticleReaderTags())
	}
	if enToolsVersions != locales.ArticleReaderSourceVersions() {
		t.Errorf("「Captured source versions」登记写的是 %q，英文目录是 %q", enToolsVersions, locales.ArticleReaderSourceVersions())
	}
}

// 英文小标题的字偶距登记只在英文变体所在的字号档、只指向 A-026、只放过 L2 的横向偏差，每条有上限；
// 中文界面里同一处的小标题一条都不命中。
func TestEnglishToolsWaiversOnlyEnglishMediumAndOnlyKerning(t *testing.T) {
	ws := enToolsWaivers()
	if len(ws) != 4 {
		t.Fatalf("应有 4 条（两个标题 × 宽度与墨迹），实际 %d 条", len(ws))
	}
	for _, w := range ws {
		if w.Variant != englishTier || w.Ledger != "A-026" || w.Layer != parity.LayerL2 || w.MaxDY != 0 || w.MaxDelta <= 0 {
			t.Errorf("登记 %s %s/%s %q 的范围不对：%+v", w.Ledger, w.Layer, w.Kind, w.Subject, w)
		}
	}
	is := func(kind, subject string, delta float64) parity.Issue {
		return parity.Issue{Layer: parity.LayerL2, Kind: kind, Subject: subject, Delta: delta}
	}
	cases := []struct {
		name    string
		is      parity.Issue
		variant string
		want    bool
	}{
		{"Tags 宽度差 1.67", is("text-width", "「Tags」", 1.67), tierMedium, true},
		{"Tags 墨迹左移 1 px", is("ink", "「Tags」", 1), tierMedium, true},
		{"Captured source versions 宽度差 1.43", is("text-width", "「Captured source versions」", 1.43), tierMedium, true},
		{"宽度差超过上限", is("text-width", "「Tags」", 2.5), tierMedium, false},
		{"小字号档不放过", is("text-width", "「Tags」", 1.67), tierSmall, false},
		{"中文小标题不命中（标签）", is("text-width", "「标签」", 1.67), tierMedium, false},
		{"中文小标题不命中（采集的来源版本）", is("ink", "「采集的来源版本」", 1), tierMedium, false},
		{"纵向错位不放过", parity.Issue{Layer: parity.LayerL2, Kind: "ink", Subject: "「Tags」", Delta: 1, DY: 1}, tierMedium, false},
	}
	for _, c := range cases {
		if got := anyWaives(ws, c.is, c.variant); got != c.want {
			t.Errorf("%s：放过 = %v，期望 %v（%+v）", c.name, got, c.want, c.is)
		}
	}
}

// TestVersionPaneWaiversAreDirectedAndBounded 守住 C-015 的三条放过条款：墨迹总量只放过 Viva 偏多、浅色与深色各有区间；
// 含汉字行的位置只放过 ≤1 px；混排行的水平位置只放过到 3.5 px 且纵向仍 ≤1 px；别的文字一概不放过。
func TestVersionPaneWaiversAreDirectedAndBounded(t *testing.T) {
	const darkMedium = "dark-royal-blue-medium-1280x800"
	ws := versionPaneWaivers()
	lead := "「" + versionPaneLead + " 本文拆解知识库的存储、索引与同步三层。」"
	mixed := "「" + versionPaneLead + " 本文拆解知识库的存储、索引、同步与冲突解决四层。 " + versionPaneMixed + "」"
	mass := func(subject string, delta float64, dir int8) parity.Issue {
		return parity.Issue{Layer: parity.LayerL2, Kind: "ink-mass", Subject: subject, Delta: delta, Dir: dir}
	}
	ink := func(subject string, delta, dy float64) parity.Issue {
		return parity.Issue{Layer: parity.LayerL2, Kind: "ink", Subject: subject, Delta: delta, DY: dy}
	}
	cases := []struct {
		name    string
		is      parity.Issue
		variant string
		want    bool
	}{
		{"浅色偏多 0.21", mass(lead, 0.21, 1), tierMedium, true},
		{"浅色偏多 0.15（下限附近）", mass(lead, 0.15, 1), tierMedium, true},
		{"浅色偏少不放过", mass(lead, 0.21, -1), tierMedium, false},
		{"浅色偏多 0.45 超上限", mass(lead, 0.45, 1), tierMedium, false},
		{"深色偏多 0.476", mass(lead, 0.476, 1), darkMedium, true},
		{"深色偏多 0.341（T1 新增读数，位置偏差被登记后才露出）", mass(lead, 0.341, 1), darkMedium, true},
		{"深色偏多 0.167（T2 小档读数）", mass(lead, 0.167, 1), "dark-royal-blue-small-1280x800", true},
		{"深色偏多 0.534（T2 大档读数）", mass(lead, 0.534, 1), "dark-royal-blue-large-1280x800", true},
		{"深色偏多 0.10 低于深色下限", mass(lead, 0.10, 1), darkMedium, false},
		{"深色偏多 0.6 超上限", mass(lead, 0.6, 1), darkMedium, false},
		{"别的文字不放过", mass("「别的文字」", 0.2, 1), tierMedium, false},
		{"汉字行位移 1 px", ink(lead, 1, 1), tierMedium, true},
		{"汉字行位移 2 px", ink(lead, 2, 0), tierMedium, false},
		{"汉字行纵向 2 px", ink(lead, 1, 2), tierMedium, false},
		{"混排行水平 3 px、纵向 0", ink(mixed, 3, 0), tierMedium, true},
		{"混排行水平 3 px、纵向 1 px", ink(mixed, 3, 1), tierMedium, true},
		{"混排行水平 4 px 超上限", ink(mixed, 4, 0), tierMedium, false},
		{"混排行纵向 2 px", ink(mixed, 3, 2), tierMedium, false},
		{"水平 3 px 不能借混排行的条款放过汉字行", ink(lead, 3, 0), tierMedium, false},
		{"别的文字的位置不放过", ink("「别的文字」", 1, 0), tierMedium, false},
	}
	for _, c := range cases {
		if got := anyWaives(ws, c.is, c.variant); got != c.want {
			t.Errorf("%s：放过 = %v，期望 %v（%+v）", c.name, got, c.want, c.is)
		}
	}
	// L1 与 L3 的问题、别的类别（text-width、box）不被这三条放过。
	for _, kind := range []string{"text-width", "box", "text-lines"} {
		is := parity.Issue{Layer: parity.LayerL2, Kind: kind, Subject: lead, Delta: 0.5}
		if anyWaives(ws, is, tierMedium) {
			t.Errorf("L2/%s 不应被放过", kind)
		}
	}
	if anyWaives(ws, parity.Issue{Layer: parity.LayerL1, Kind: "text", Subject: lead}, tierMedium) {
		t.Error("L1 的问题不应被放过")
	}
}
