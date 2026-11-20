package scenes

import (
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 以下守住 library_bulk_waivers.go 的两类登记：写死的英文 Subject 要与 locales 目录里英文界面画出的字一致（否则登记悄悄匹配不上），
// 以及每条登记的范围——只放过台账里记下的那一类、那个大小、那个字号档，别的照常判定。
// 语言是进程内的全局状态：与目录核对的测试不并行，结束时还原。

const (
	tierMedium     = "light-royal-blue-medium-1280x800"
	tierSmall      = "light-royal-blue-small-1280x800"
	tierLarge      = "light-royal-blue-large-1280x800"
	viewportNarrow = "light-royal-blue-medium-800x600"
)

// anyWaives 报告 ws 里是否有一条在 variant 里放过 is。
func anyWaives(ws []parity.Waiver, is parity.Issue, variant string) bool {
	for _, w := range ws {
		if w.MatchesIn(is, variant) {
			return true
		}
	}
	return false
}

// TestEnglishBulkWaiverSubjectsMatchCatalog 核对写死的英文 Subject 与英文目录一致。
func TestEnglishBulkWaiverSubjectsMatchCatalog(t *testing.T) {
	locales.SetLang(locales.En)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
	cases := []struct{ name, got, want string }{
		{"逐条结果的折叠钮", enResultsLabel, prefixOf(locales.LibraryWorkflowResults)},
		{"右键菜单的收藏", enMenuFavorite, locales.LibraryFavorite()},
		{"回收站", enScopeTrash, locales.LibraryScopeTrash()},
		{"永久删除", enDeleteForever, locales.LibraryDeleteForever()},
		{"已有标签（uppercase）", enExistingTags, strings.ToUpper(locales.LibraryTagsExisting())},
		{"AI 建议标签（uppercase）", enSuggestedTags, strings.ToUpper(locales.LibraryAiTagsSuggest())},
		{"仅重试失败项", enRetryFailed, locales.LibraryWorkflowRetryFailed()},
		{"收起", enDismiss, locales.LibraryWorkflowDismiss()},
		{"批量移动", enBulkMove, locales.LibraryBulkMoveTitle()},
		{"批量加标签", enBulkAddTag, locales.LibraryBulkAddTag()},
		{"批量收藏", enBulkFavorite, locales.LibraryBulkFavorite()},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s：登记写的是 %q，英文目录画出来的是 %q", c.name, c.got, c.want)
		}
	}
	if !strings.HasPrefix(locales.LibraryDeleteEvidenceHint(), enDeleteHint) {
		t.Errorf("确认框正文以 %q 打头的假设不成立：目录里是 %q", enDeleteHint, locales.LibraryDeleteEvidenceHint())
	}
}

// TestEnglishBulkWaiversOnlyEnglishMediumAndOnlyTheirCause 守住范围：全部带中档字号档限制、只指向四个台账条目、只放过 L2 且不放过纵向位移，
// 中文界面里的同类文字（中文变体）一条都不命中。
func TestEnglishBulkWaiversOnlyEnglishMediumAndOnlyTheirCause(t *testing.T) {
	ws := englishBulkWaivers()
	if len(ws) == 0 {
		t.Fatal("英文登记不应为空")
	}
	ledgers := map[string]bool{"A-009": true, "A-013": true, "A-014": true, "A-026": true}
	for _, w := range ws {
		if w.Variant != englishTier {
			t.Errorf("登记 %s %s/%s %q 没有限在 %q 字号档：%+v", w.Ledger, w.Layer, w.Kind, w.Subject, englishTier, w)
		}
		if !ledgers[w.Ledger] {
			t.Errorf("登记 %q 指向了不在预期里的台账条目 %s", w.Subject, w.Ledger)
		}
		if w.Layer != parity.LayerL2 {
			t.Errorf("登记 %q 放过了 %s 层：英文变体只放过 L2 的文字与外框偏差", w.Subject, w.Layer)
		}
		if w.MaxDY != 0 {
			t.Errorf("登记 %q 放过了纵向位移（MaxDY=%v）：字偶距只沿行方向起作用", w.Subject, w.MaxDY)
		}
	}
	for _, zh := range []string{"「仅重试失败项」", "「收起」", "「回收站」", "「永久删除」", "「选择已有标签」", "「收藏」"} {
		for _, kind := range []string{"text-width", "ink", "ink-mass", "box"} {
			is := parity.Issue{Layer: parity.LayerL2, Kind: kind, Subject: zh, Delta: 0.3, Dir: 1}
			if anyWaives(ws, is, tierMedium) {
				t.Errorf("中文文字 %s（%s）被英文登记放过了", zh, kind)
			}
		}
	}
}

// TestEnglishKerningWaiversAreFramedByTheirMeasuredSize 守住字偶距登记的大小：实测读数放过，超过上限一点点的不放过，别的字号档不放过。
func TestEnglishKerningWaiversAreFramedByTheirMeasuredSize(t *testing.T) {
	ws := englishBulkWaivers()
	issue := func(kind, subject string, delta, dy float64) parity.Issue {
		return parity.Issue{Layer: parity.LayerL2, Kind: kind, Subject: subject, Delta: delta, DY: dy}
	}
	cases := []struct {
		name    string
		is      parity.Issue
		variant string
		want    bool
	}{
		// 400 字重（A-014）：实测读数——折叠钮宽度差 1.56、墨迹 +2；菜单项「Favorite」+1；确认框正文 +1。
		{"折叠钮宽度差 1.56", issue("text-width", "「Per-item results (2)」", 1.56, 0), tierMedium, true},
		{"折叠钮宽度差 2.5 超上限", issue("text-width", "「Per-item results (2)」", 2.5, 0), tierMedium, false},
		{"折叠钮墨迹 +2", issue("ink", "「Per-item results (2)」", 2, 0), tierMedium, true},
		{"折叠钮墨迹纵向错 1 不放过", issue("ink", "「Per-item results (2)」", 1, 1), tierMedium, false},
		{"折叠钮小字号档不放过", issue("text-width", "「Per-item results (2)」", 1.56, 0), tierSmall, false},
		{"Favorite 墨迹 +1", issue("ink", "「Favorite」", 1, 0), tierMedium, true},
		{"Favorite 墨迹 +2 超上限", issue("ink", "「Favorite」", 2, 0), tierMedium, false},
		{"Favorites 是另一段文字", issue("ink", "「Favorites」", 1, 0), tierMedium, false},
		{"确认框正文墨迹 +1", issue("ink", "「Past answers keep the excerpts they cited by default. The option below only clears related evidence and links; the answer text stays unchanged.」", 1, 0), tierMedium, true},
		{"确认框正文墨迹 +2 超上限", issue("ink", "「Past answers keep the excerpts they cited by default.」", 2, 0), tierMedium, false},
		// 右对齐的批量条（A-014 的后果）：三个钮的外框偏移 1.30、1.14、1.04。
		{"Move to collection 外框 1.30", issue("box", "button 「Move to collection」", 1.30, 0), tierMedium, true},
		{"Add tag to selection 外框 1.14", issue("box", "button 「Add tag to selection」", 1.14, 0), tierMedium, true},
		{"Favorite selection 外框 1.04", issue("box", "button 「Favorite selection」", 1.04, 0), tierMedium, true},
		{"外框 1.6 超上限", issue("box", "button 「Move to collection」", 1.6, 0), tierMedium, false},
		{"别的钮的外框偏移不放过", issue("box", "button 「Archive」", 1.04, 0), tierMedium, false},
		// 500 / 600 字重（A-026）：回收站宽度差 1.88、墨迹 −2；确认框标题「Delete forever」宽度差 1.60。
		{"Trash 宽度差 1.88", issue("text-width", "「Trash」", 1.88, 0), tierMedium, true},
		{"Trash 宽度差 2.2 超上限", issue("text-width", "「Trash」", 2.2, 0), tierMedium, false},
		{"Trash 墨迹 −2", issue("ink", "「Trash」", 2, 0), tierMedium, true},
		{"Delete forever 宽度差 1.60", issue("text-width", "「Delete forever」", 1.60, 0), tierMedium, true},
		{"Delete forever 的墨迹位移没有登记", issue("ink", "「Delete forever」", 1, 0), tierMedium, false},
		{"Trash 大字号档不放过", issue("text-width", "「Trash」", 1.88, 0), tierLarge, false},
	}
	for _, c := range cases {
		if got := anyWaives(ws, c.is, c.variant); got != c.want {
			t.Errorf("%s：放过 = %v，期望 %v", c.name, got, c.want)
		}
	}
}

// TestEnglishWideTrackingWaiversFrameTheHeadings 守住标签浮层两个英文小标题的登记：实测宽度差 5.71 与 7.21 放过，
// 低于字距应有的下限（别的问题）与高于「字距 + 字偶距 2px」的不放过，墨迹位移只管横向。
func TestEnglishWideTrackingWaiversFrameTheHeadings(t *testing.T) {
	ws := englishBulkWaivers()
	width := func(title string, delta float64) parity.Issue {
		return parity.Issue{Layer: parity.LayerL2, Kind: "text-width", Subject: "「" + title + "」", Delta: delta}
	}
	ink := func(title string, delta, dy float64) parity.Issue {
		return parity.Issue{Layer: parity.LayerL2, Kind: "ink", Subject: "「" + title + "」", Delta: delta, DY: dy}
	}
	cases := []struct {
		name string
		is   parity.Issue
		want bool
	}{
		{"EXISTING TAGS 5.71", width(enExistingTags, 5.71), true},
		{"EXISTING TAGS 低于字距下限", width(enExistingTags, 1.0), false},
		{"EXISTING TAGS 高于上限", width(enExistingTags, 7.5), false},
		{"EXISTING TAGS 墨迹 1", ink(enExistingTags, 1, 0), true},
		{"EXISTING TAGS 墨迹纵向 1", ink(enExistingTags, 1, 1), false},
		{"AI TAG SUGGESTIONS 7.21", width(enSuggestedTags, 7.21), true},
		{"AI TAG SUGGESTIONS 高于上限", width(enSuggestedTags, 9), false},
		{"AI TAG SUGGESTIONS 墨迹 1", ink(enSuggestedTags, 1, 0), true},
	}
	for _, c := range cases {
		if got := anyWaives(ws, c.is, tierMedium); got != c.want {
			t.Errorf("%s：放过 = %v，期望 %v", c.name, got, c.want)
		}
	}
	if anyWaives(ws, width(enExistingTags, 5.71), tierSmall) {
		t.Error("小字号档不应放过英文小标题的宽度差")
	}
}

// TestEnglishOpacityInkWaiversOnlyOverweightAndMediumTier 守住禁用钮的 A-009 英文登记：只放过 Viva 偏多的方向、
// 只在中档、只给两枚钮（实测 1.266 与 1.286）。
func TestEnglishOpacityInkWaiversOnlyOverweightAndMediumTier(t *testing.T) {
	ws := englishBulkWaivers()
	mass := func(subject string, delta float64, dir int8) parity.Issue {
		return parity.Issue{Layer: parity.LayerL2, Kind: "ink-mass", Subject: subject, Delta: delta, Dir: dir}
	}
	cases := []struct {
		name    string
		is      parity.Issue
		variant string
		want    bool
	}{
		{"Retry failed only 1.266", mass("「Retry failed only」", 0.266, 1), tierMedium, true},
		{"Dismiss 1.286", mass("「Dismiss」", 0.286, 1), tierMedium, true},
		{"偏少的方向不放过", mass("「Dismiss」", 0.286, -1), tierMedium, false},
		{"超过 0.4 不放过", mass("「Dismiss」", 0.5, 1), tierMedium, false},
		{"小字号档不放过", mass("「Dismiss」", 0.286, 1), tierSmall, false},
		{"别的按钮不放过", mass("「Retry」", 0.286, 1), tierMedium, false},
	}
	for _, c := range cases {
		if got := anyWaives(ws, c.is, c.variant); got != c.want {
			t.Errorf("%s：放过 = %v，期望 %v", c.name, got, c.want)
		}
	}
}

// TestShadowRingWaiversOnlyTheTwoMeasuredTextsAtNarrowViewport 守住 G-023 的登记：只放过 800×600 里「待读」「文档」这两段文字
// 墨迹总量偏多、且大小落在实测读数（0.340、0.156）附近的问题；别的视口、别的文字、别的方向、别的层都照常判定。
func TestShadowRingWaiversOnlyTheTwoMeasuredTextsAtNarrowViewport(t *testing.T) {
	ws := shadowRingWaivers()
	mass := func(subject string, delta float64, dir int8) parity.Issue {
		return parity.Issue{Layer: parity.LayerL2, Kind: "ink-mass", Subject: subject, Delta: delta, Dir: dir}
	}
	cases := []struct {
		name    string
		is      parity.Issue
		variant string
		want    bool
	}{
		{"待读 0.340 @800×600", mass("「待读」", 0.340, 1), viewportNarrow, true},
		{"文档 0.156 @800×600", mass("「文档」", 0.156, 1), viewportNarrow, true},
		{"待读 在 1280×800 不放过", mass("「待读」", 0.340, 1), tierMedium, false},
		{"文档 在 1280×800 不放过", mass("「文档」", 0.156, 1), tierMedium, false},
		{"待读 0.2 低于实测范围", mass("「待读」", 0.2, 1), viewportNarrow, false},
		{"待读 0.45 高于实测范围", mass("「待读」", 0.45, 1), viewportNarrow, false},
		{"文档 0.3 高于实测范围", mass("「文档」", 0.3, 1), viewportNarrow, false},
		{"偏少的方向不放过", mass("「待读」", 0.340, -1), viewportNarrow, false},
		{"别的文字不放过", mass("「待办」", 0.340, 1), viewportNarrow, false},
		{"别的类别不放过", parity.Issue{Layer: parity.LayerL2, Kind: "ink", Subject: "「待读」", Delta: 0.340, Dir: 1}, viewportNarrow, false},
		{"别的层不放过", parity.Issue{Layer: parity.LayerL1, Kind: "text", Subject: "「待读」", Delta: 0, Dir: 1}, viewportNarrow, false},
	}
	for _, c := range cases {
		if got := anyWaives(ws, c.is, c.variant); got != c.want {
			t.Errorf("%s：放过 = %v，期望 %v", c.name, got, c.want)
		}
	}
}

// TestWithEnglishBulkDoesNotAliasItsInput 守住「附上英文登记返回新切片」：场景之间共用同一份登记切片，原地追加会互相串。
func TestWithEnglishBulkDoesNotAliasItsInput(t *testing.T) {
	base := make([]parity.Waiver, 1, 64) // 有足够的余量：错误的原地追加会直接写进它
	base[0] = parity.Waiver{Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass"}
	a := withEnglishBulk(base)
	b := withEnglishBulk(base)
	if len(base) != 1 {
		t.Fatalf("输入切片的长度被改了：%d", len(base))
	}
	if len(a) != len(b) || len(a) != 1+len(englishBulkWaivers()) {
		t.Fatalf("两次追加的长度应一致且等于 1 + 英文登记数：%d、%d", len(a), len(b))
	}
	a[1].Ledger = "X"
	if b[1].Ledger == "X" || base[:2][1].Ledger == "X" {
		t.Error("两次追加的结果共用了底层数组")
	}
}
