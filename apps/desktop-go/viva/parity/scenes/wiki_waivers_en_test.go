package scenes

import (
	"slices"
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 守住 wiki_waivers_en.go：写死的英文 Subject 是英文目录画出的字的片段；字偶距与字距登记只在英文变体所在字号档、只指向 A-013 / A-014、
// 只放过 L2，每条有上限；中文界面里的文字一条都不命中。语言是进程内全局状态：测试不并行，结束时还原。

func TestEnglishWikiWaiverSubjectsMatchCatalog(t *testing.T) {
	locales.SetLang(locales.En)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
	cases := []struct{ name, got, text string }{
		{"侧栏小标题 BROWSE", enWikiBrowse, strings.ToUpper(locales.WikiPanelBrowse())},
		{"侧栏小标题 BY KIND", enWikiByKind, strings.ToUpper(locales.WikiPanelByKind())},
		{"侧栏小标题 NEEDS ATTENTION", enWikiNeedsAttention, strings.ToUpper(locales.WikiPanelNeedsAttention())},
		{"类型名", enWikiTopic, locales.WikiKindTopic()},
		{"空态说明", enWikiEmptyHint, locales.WikiEmptyHint()},
		{"编译说明", enWikiCompileIntro, locales.WikiCompileIntro()},
		{"编译计数", enWikiCompileCounts, locales.WikiCompileCounts(3, 1, 0, 0)},
		{"编译分块数", enWikiChunks, locales.WikiCompileEntryBlocks(3)},
		{"编译估算", enWikiEstimate, locales.WikiCompileEstimate(4, 6, 1, 2)},
		{"选择资料升级", enWikiUpgrade, locales.WikiUpgradeSources()},
		{"接受更新", enWikiAccept, locales.WikiSuggestionAccept()},
	}
	for _, c := range cases {
		if c.got == "" || !strings.Contains(c.text, c.got) {
			t.Errorf("%s：登记写的是 %q，英文目录画出来的是 %q", c.name, c.got, c.text)
		}
	}
}

func TestEnglishWikiWaiversAreBounded(t *testing.T) {
	all := slices.Concat(wikiEnglishBaseWaivers(), wikiEnglishEmptyWaivers(), wikiEnglishCompileWaivers(),
		wikiEnglishDetailWaivers("标题"), wikiEnglishSuggestionWaivers(), wikiEnglishEditWaivers())
	for _, w := range all {
		if w.Variant != englishTier || w.Layer != parity.LayerL2 || w.MaxDelta <= 0 {
			t.Errorf("登记 %s %s/%s %q 的范围不对：%+v", w.Ledger, w.Layer, w.Kind, w.Subject, w)
		}
		if w.Ledger != kerning400Ledger && w.Ledger != "A-013" {
			t.Errorf("登记 %q 指向 %s，超出字距与字偶距延伸的范围", w.Subject, w.Ledger)
		}
		switch w.Kind {
		case "text-width", "ink", "box", "icon-box":
		default:
			t.Errorf("登记 %q 的类别 %q 超出字偶距延伸的范围", w.Subject, w.Kind)
		}
	}
	// 中文界面画出的字不含任何一条英文登记的 Subject 片段（图标一条是中文 Subject，另按范围限定在英文变体）。
	zh := []string{
		locales.WikiPanelBrowse(), locales.WikiPanelByKind(), locales.WikiPanelNeedsAttention(), locales.WikiEmptyHint(),
		locales.WikiCompileIntro(), locales.WikiUpgradeSources(), locales.WikiSuggestionAccept(), locales.WikiKindTopic(),
	}
	for _, w := range all {
		for _, text := range zh {
			if w.Subject != "" && strings.Contains(text, strings.Trim(w.Subject, "「」")) && strings.Trim(w.Subject, "「」") != "" {
				t.Errorf("登记 %q 命中了中文界面的文字 %q", w.Subject, text)
			}
		}
	}
}
