package scenes

import (
	"slices"
	"testing"

	"guizhi-mygo-shell/viva/parity"
)

// 以下守住 library_detail_waivers.go 的几处放过项（台账 C-010，待追认）：
//  1. 每条都有方向与上下限（ink-mass 的共同规则），上限不超过 0.65：不会放过「Viva 少了一半」或「多了一倍」的缺陷；
//  2. 范围就是实测取到的那一档、那种主题、那个方向，别处同类偏差照常判定；
//  3. 只登记在出现它的场景里，别的场景一条都不命中。

const (
	darkTierMedium = "dark-royal-blue-medium-1280x800"
	darkTierSmall  = "dark-teal-small-1280x800"
	darkTierLarge  = "dark-royal-blue-large-1280x800"
)

// detailInkWaivers 是本文件登记的全部放过项（含查找条占位符那条 compositedInkWaiver）。
func detailInkWaivers() []parity.Waiver {
	out := slices.Concat(imageTabIconWaivers(), middleDotWaivers(), overlayThinGlyphWaivers())
	return append(out, compositedInkWaiver("「在当前页查找…」"))
}

func TestDetailInkWaiversAreDirectedBoundedAndPointToTheLedger(t *testing.T) {
	ledgers := map[string]bool{"A-003": true, "A-009": true, "G-016": true}
	for _, w := range detailInkWaivers() {
		if w.Layer != parity.LayerL2 || w.Kind != "ink-mass" {
			t.Errorf("登记 %q 不是 L2 的墨迹总量：%+v", w.Subject, w)
		}
		if w.Dir == 0 {
			t.Errorf("登记 %q 没有方向（Dir）：Viva 偏少的缺陷也会被放过", w.Subject)
		}
		if w.MinDelta <= 0 || w.MaxDelta <= 0 || w.MaxDelta > 0.65 || w.MinDelta >= w.MaxDelta {
			t.Errorf("登记 %q 的区间 [%v, %v] 不合规则（要有下限与上限，上限不超过 0.65）", w.Subject, w.MinDelta, w.MaxDelta)
		}
		if w.MaxDY != 0 {
			t.Errorf("登记 %q 放过了纵向位移（MaxDY=%v）：墨迹总量的偏差与位置无关", w.Subject, w.MaxDY)
		}
		if !ledgers[w.Ledger] {
			t.Errorf("登记 %q 指向了不在预期里的台账条目 %s", w.Subject, w.Ledger)
		}
	}
}

// TestImageTabIconWaiverCoversOnlyTheTwelvePixelIcon：边长 12px、Viva 偏多、[0.19, 0.22]（实测 0.194–0.213）；
// 别的边长、方向相反、区间之外都不放过，子串匹配不会因为数字相邻（112.00）而串味。
func TestImageTabIconWaiverCoversOnlyTheTwelvePixelIcon(t *testing.T) {
	ws := imageTabIconWaivers()
	icon := func(size string, ratio float64) parity.Issue {
		return inkIssue("图标 [836.00 168.00 "+size+" "+size+"]", ratio, false)
	}
	cases := []struct {
		name  string
		issue parity.Issue
		want  bool
	}{
		{"实测最小", icon("12.00", 1.194), true},
		{"实测最大", icon("12.00", 1.213), true},
		{"下限含边界", icon("12.00", 1.19), true},
		{"上限含边界", icon("12.00", 1.22), true},
		{"比下限更小（是别的问题）", icon("12.00", 1.18), false},
		{"超过上限", icon("12.00", 1.23), false},
		{"Viva 偏少", icon("12.00", 0.8), false},
		{"13.50 的图标没有登记", icon("13.50", 1.2), false},
		{"112.00 不能串味", icon("112.00", 1.2), false},
	}
	for _, c := range cases {
		if got := anyWaives(ws, c.issue, tierMedium); got != c.want {
			t.Errorf("%s：放过 = %v，期望 %v（%+v）", c.name, got, c.want, c.issue)
		}
	}
	// 位置问题（ink）与 L3 不被墨迹总量的登记放过。
	other := icon("12.00", 1.2)
	other.Kind = "ink"
	if anyWaives(ws, other, tierMedium) {
		t.Error("墨迹位置（ink）问题不应被墨迹总量的登记放过")
	}
}

// TestMiddleDotWaiversFollowTheirOwnTier：小档 [0.21, 0.25]、大档 [0.30, 0.35]，方向固定为 Viva 偏少；
// 一档的区间不能放过另一档的值，中档与没有记大小的问题不放过。
func TestMiddleDotWaiversFollowTheirOwnTier(t *testing.T) {
	ws := middleDotWaivers()
	dot := func(ratio float64) parity.Issue { return inkIssue("「·」", ratio, false) }
	cases := []struct {
		name    string
		ratio   float64
		variant string
		want    bool
	}{
		{"小档实测最小", 1 - 0.2105, tierSmall, true},
		{"小档实测最大", 1 - 0.2420, tierSmall, true},
		{"大档实测最小", 1 - 0.3059, tierLarge, true},
		{"大档实测最大", 1 - 0.3442, tierLarge, true},
		{"深色小档同样适用", 1 - 0.23, darkTierSmall, true},
		{"小档的区间不放过大档的值", 1 - 0.33, tierSmall, false},
		{"大档的区间不放过小档的值", 1 - 0.22, tierLarge, false},
		{"小档超过上限", 1 - 0.26, tierSmall, false},
		{"大档超过上限", 1 - 0.36, tierLarge, false},
		{"中档没有取得证据", 1 - 0.22, tierMedium, false},
		{"Viva 偏多（「.」的方向）", 1.3, tierSmall, false},
	}
	for _, c := range cases {
		if got := anyWaives(ws, dot(c.ratio), c.variant); got != c.want {
			t.Errorf("%s：放过 = %v，期望 %v", c.name, got, c.want)
		}
	}
	noSize := dot(0.77)
	noSize.Delta = 0
	if anyWaives(ws, noSize, tierSmall) {
		t.Error("没有记大小的问题不应被放过")
	}
	if anyWaives(ws, inkIssue("「,」", 0.77, false), tierSmall) {
		t.Error("别的字符不应被「·」的登记放过")
	}
}

// TestOverlayThinGlyphWaiversCoverOnlyTheDarkOverlay：深色、浮层里、Viva 偏多；「/」[0.57, 0.63]（实测 0.5747、0.6282），
// 「1」[0.50, 0.51]（实测 0.5081）；浅色主题、页面层、超出区间、方向相反、别的字符都不放过，两个字符的区间互不串味。
func TestOverlayThinGlyphWaiversCoverOnlyTheDarkOverlay(t *testing.T) {
	ws := overlayThinGlyphWaivers()
	glyph := func(subject string, ratio float64, overlay bool) parity.Issue {
		return inkIssue(subject, ratio, overlay)
	}
	cases := []struct {
		name    string
		issue   parity.Issue
		variant string
		want    bool
	}{
		{"「/」深色中档实测", glyph("「/」", 1.6282, true), darkTierMedium, true},
		{"「/」深色小档实测", glyph("「/」", 1.5747, true), darkTierSmall, true},
		{"「1」深色小档实测", glyph("「1」", 1.5081, true), darkTierSmall, true},
		{"「1」下限含边界", glyph("「1」", 1.50, true), darkTierSmall, true},
		{"「/」浅色主题", glyph("「/」", 1.6, true), tierMedium, false},
		{"「1」浅色主题", glyph("「1」", 1.508, true), tierSmall, false},
		{"页面层的同类文字", glyph("「/」", 1.6, false), darkTierMedium, false},
		{"「/」超过上限", glyph("「/」", 1.64, true), darkTierMedium, false},
		{"「1」超过上限", glyph("「1」", 1.52, true), darkTierSmall, false},
		{"「/」低于下限（overlayInkWaiver 已经管着的范围）", glyph("「/」", 1.4, true), darkTierMedium, false},
		{"「1」的区间不放过「/」的值", glyph("「1」", 1.6, true), darkTierSmall, false},
		{"「/」的区间不放过「1」的值", glyph("「/」", 1.508, true), darkTierSmall, false},
		{"Viva 偏少", glyph("「/」", 0.4, true), darkTierMedium, false},
		{"别的字符", glyph("「2」", 1.508, true), darkTierSmall, false},
	}
	for _, c := range cases {
		if got := anyWaives(ws, c.issue, c.variant); got != c.want {
			t.Errorf("%s：放过 = %v，期望 %v", c.name, got, c.want)
		}
	}
	// 实测的值对 overlayInkWaiver（上限 0.5）确实够不着，这批登记才有存在的理由。
	for _, c := range []parity.Issue{glyph("「/」", 1.5747, true), glyph("「1」", 1.5081, true)} {
		if overlayInkWaiver.MatchesIn(c, darkTierSmall) {
			t.Errorf("overlayInkWaiver 已经放过 %s：不需要单独登记", c.Subject)
		}
	}
}

// TestDetailInkWaiversOnlyOnTheirScenes：每条登记只出现在出现它的场景里。
func TestDetailInkWaiversOnlyOnTheirScenes(t *testing.T) {
	const prefix = "app.window.library."
	groups := []struct {
		name   string
		ws     []parity.Waiver
		scenes []string
	}{
		{"图中文字页签图标", imageTabIconWaivers(), []string{prefix + "detail-image", prefix + "detail-image-text"}},
		{"间隔点", middleDotWaivers(), []string{prefix + "detail-revisions-open"}},
		{"整理导航条的斜线与首位数字", overlayThinGlyphWaivers(), []string{prefix + "table-detail-modal"}},
		{"查找条占位符", []parity.Waiver{compositedInkWaiver("「在当前页查找…」")}, []string{prefix + "detail-find"}},
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
					t.Errorf("%s：场景 %s 登记了 = %v，期望 %v（%q）", g.name, id, got, want, w.Subject)
				}
			}
		}
	}
}
