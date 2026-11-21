package scenes

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 以下守住 library_detail_waivers_en.go 的英文登记（台账 C-010，待追认）：
//  1. 写死的英文 Subject 与 locales 目录里英文界面画出的字一致（否则登记悄悄匹配不上）；
//  2. 只在英文变体所在的字号档、只指向 A-009 / A-014 / A-026、只放过 L2 的横向偏差，每条有上限；中文界面里的同类文字一条都不命中；
//  3. 每一组只登记在出现它的场景里。
// 语言是进程内的全局状态：与目录核对的测试不并行，结束时还原。

func TestEnglishDetailWaiverSubjectsMatchCatalog(t *testing.T) {
	locales.SetLang(locales.En)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
	cases := []struct{ name, got, want string }{
		{"文字稿页签", enTabTranscript, locales.LibraryTranscript()},
		{"文字稿字数", enTabTranscriptLen, locales.LibraryTranscriptLength(49)},
		{"图中文字页签", enTabRecognized, locales.LibraryRecognizedSection()},
		{"待复核条标题", enImportIncomplete, locales.ArticleReaderImportIncomplete()},
		{"采用来源版本的钮", enRevisionAdopt, locales.LibrarySourceRevisionAdopt()},
		{"保留我的修改并保存", enConflictKeepMine, locales.LibraryDraftConflictKeepMine()},
		{"退出专注", enExitFocusShort, locales.LibraryExitFocusReadingShort()},
		{"生成文字稿", enGenerateTranscript, locales.LibraryTranscribeGenerate()},
		{"正文场景的字数", strconv.Itoa(transcriptBodyChars()) + " chars", locales.LibraryTranscriptLength(transcriptBodyChars())},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s：登记写的是 %q，英文目录画出来的是 %q", c.name, c.got, c.want)
		}
	}
	if !strings.HasPrefix(locales.LibrarySourceRevisionsHint(), enRevisionsHint) {
		t.Errorf("来源版本说明以 %q 打头的假设不成立：目录里是 %q", enRevisionsHint, locales.LibrarySourceRevisionsHint())
	}
	if !strings.HasPrefix(locales.LibraryDraftConflict(), enConflictNotice) {
		t.Errorf("冲突提示以 %q 打头的假设不成立：目录里是 %q", enConflictNotice, locales.LibraryDraftConflict())
	}
}

func TestEnglishDetailWaiversOnlyEnglishMediumAndOnlyTheirCause(t *testing.T) {
	ws := englishDetailWaivers()
	if len(ws) == 0 {
		t.Fatal("英文登记不应为空")
	}
	ledgers := map[string]bool{"A-009": true, "A-014": true, "A-026": true}
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
		switch w.Kind {
		case "ink-mass":
			if w.Dir == 0 || w.MinDelta <= 0 || w.MaxDelta <= w.MinDelta || w.MaxDelta > 0.65 {
				t.Errorf("墨迹总量登记 %q 要有方向与 (0, 0.65] 内的上下限：%+v", w.Subject, w)
			}
		case "text-width", "box", "ink":
			if w.MaxDelta <= 0 || w.MaxDelta > kerningWidthCap {
				t.Errorf("登记 %s %q 要有 (0, %v] 内的上限：%+v", w.Kind, w.Subject, kerningWidthCap, w)
			}
		default:
			t.Errorf("登记 %q 的类别 %q 不在预期里（text-width / box / ink / ink-mass）", w.Subject, w.Kind)
		}
	}
}

// TestEnglishDetailWaiversDoNotTouchChineseOrOtherTiers：中文界面里同一位置的文字、别的字号档、别的类别都不命中。
func TestEnglishDetailWaiversDoNotTouchChineseOrOtherTiers(t *testing.T) {
	locales.SetLang(locales.Zh)
	ws := englishDetailWaivers()
	zh := []string{
		"「" + locales.LibraryTranscript() + "」", "「" + locales.LibraryRecognizedSection() + "」", "「" + locales.ArticleReaderImportIncomplete() + "」",
		"「" + locales.LibrarySourceRevisionAdopt() + "」", "「" + locales.LibraryDraftConflictKeepMine() + "」", "「" + locales.LibraryExitFocusReadingShort() + "」",
		"button 「" + locales.LibraryTranscript() + " ",
	}
	for _, subject := range zh {
		for _, kind := range []string{"text-width", "ink", "box"} {
			is := parity.Issue{Layer: parity.LayerL2, Kind: kind, Subject: subject, Delta: 1}
			if anyWaives(ws, is, tierMedium) {
				t.Errorf("中文文字 %s（%s）被英文登记放过了", subject, kind)
			}
		}
		if anyWaives(ws, inkIssue(subject, 1.2, false), tierMedium) {
			t.Errorf("中文文字 %s（ink-mass）被英文登记放过了", subject)
		}
	}
	// 英文文字：中档放过（实测值与上限），小、大档不放过，超过上限不放过，类别不对不放过。
	tab := parity.Issue{Layer: parity.LayerL2, Kind: "text-width", Subject: "「" + enTabTranscript + "」", Delta: 1.68}
	if !anyWaives(ws, tab, tierMedium) {
		t.Error("「Transcript」的实测宽度差 1.68 应放过")
	}
	for name, v := range map[string]string{"小档": tierSmall, "大档": tierLarge} {
		if anyWaives(ws, tab, v) {
			t.Errorf("%s没有取得英文读数，不该放过", name)
		}
	}
	over := tab
	over.Delta = kerningWidthCap + 0.5
	if anyWaives(ws, over, tierMedium) {
		t.Error("超过上限的宽度差不该放过")
	}
	wrongKind := tab
	wrongKind.Kind = "ink-mass"
	if anyWaives(ws, wrongKind, tierMedium) {
		t.Error("字偶距登记不该放过墨迹总量")
	}
	// 钮的外框只匹配页签（「button 「Transcript 」打头），别处的「Transcript」框不放过。
	box := parity.Issue{Layer: parity.LayerL2, Kind: "box", Subject: "button 「" + enTabTranscript + " " + enTabTranscriptLen + "」", Delta: 1.74}
	if !anyWaives(ws, box, tierMedium) {
		t.Error("页签的外框差 1.74 应放过")
	}
	other := box
	other.Subject = "group 「" + enTabTranscript + "」"
	if anyWaives(ws, other, tierMedium) {
		t.Error("不是页签按钮的外框不该放过")
	}
	// 「Exit focus」的墨迹总量：Viva 偏多放过（实测 1.240），偏少不放过。
	if !anyWaives(ws, inkIssue("「"+enExitFocusShort+"」", 1.240, false), tierMedium) {
		t.Error("「Exit focus」的实测墨迹总量比 1.240 应放过")
	}
	if anyWaives(ws, inkIssue("「"+enExitFocusShort+"」", 0.76, false), tierMedium) {
		t.Error("Viva 偏少不该放过")
	}
}

// TestEnglishDetailWaiversOnlyOnTheirScenes：每一组只登记在出现它的场景里。
func TestEnglishDetailWaiversOnlyOnTheirScenes(t *testing.T) {
	const prefix = "app.window.library."
	groups := []struct {
		name   string
		ws     []parity.Waiver
		scenes []string
	}{
		{"文字稿页签", enTranscriptTabWaivers(), []string{prefix + "detail-video", prefix + "detail-video-transcript", prefix + "detail-transcript-empty", prefix + "detail-transcript-body"}},
		{"文字稿空态", enTranscriptEmptyWaivers(), []string{prefix + "detail-transcript-empty"}},
		{"文字稿字数", enTranscriptCountWaivers(transcriptBodyChars()), []string{prefix + "detail-transcript-body"}},
		{"图中文字页签", enRecognizedTabWaivers(), []string{prefix + "detail-image", prefix + "detail-image-text"}},
		{"待复核条", enImportReviewWaivers(), []string{prefix + "detail-review", prefix + "detail-review-open"}},
		{"来源版本折叠区", enRevisionsWaivers(), []string{prefix + "detail-revisions-open"}},
		{"冲突条", enConflictWaivers(), []string{prefix + "detail-conflict", prefix + "detail-conflict-open"}},
		{"专注阅读悬浮钮", enFocusWaivers(), []string{prefix + "detail-focus"}},
	}
	for _, g := range groups {
		for _, id := range IDs() {
			scene, err := Get(id)
			if err != nil {
				t.Fatal(err)
			}
			want := slices.Contains(g.scenes, id)
			for _, w := range g.ws {
				if got := slices.Contains(scene.Waivers, w); got != want {
					t.Errorf("%s：场景 %s 登记了 = %v，期望 %v（%s %q）", g.name, id, got, want, w.Kind, w.Subject)
				}
			}
		}
	}
}
