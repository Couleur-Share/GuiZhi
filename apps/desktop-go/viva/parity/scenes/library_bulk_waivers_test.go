package scenes

import (
	"testing"

	"guizhi-mygo-shell/viva/parity"
)

// TestResultsDisclosureWaiversCoverOnlyTheResultsBlock 守住 G-022 的登记范围：只放过 L1 里「逐条结果」这一块的三种未映射角色
// 与那一枚折叠钮；别的未映射角色、别的多出的节点、别的层的同名问题照常判定。
// Subject 靠子串匹配，写成别的样子不会报错、只是永远匹配不上或放过得太宽，所以把报告里的写法钉在这里。
func TestResultsDisclosureWaiversCoverOnlyTheResultsBlock(t *testing.T) {
	const variant = "light-royal-blue-medium-1280x800"
	covers := func(is parity.Issue) bool {
		for _, w := range resultsDisclosureWaivers {
			if w.MatchesIn(is, variant) {
				return true
			}
		}
		return false
	}
	l1 := func(kind, subject string) parity.Issue {
		return parity.Issue{Layer: parity.LayerL1, Kind: kind, Subject: subject}
	}
	for _, role := range []string{"DisclosureTriangle", "list", "listitem"} {
		if !covers(l1("unmapped-role", role)) {
			t.Errorf("未映射角色 %q 应被 G-022 放过", role)
		}
	}
	if !covers(l1("extra-node", "button 「逐条结果（2）」")) || !covers(l1("extra-node", "button 「逐条结果（12）」")) {
		t.Error("多出的「逐条结果（N）」折叠钮应被 G-022 放过")
	}
	// 范围之外：别的未映射角色、别的多出的节点、别的角色的同名节点、别的层、别的问题类别。
	// （Subject 是子串匹配：名字里含 list 的别的角色，如 tablist、listmarker，会被 list 这一条一并放过——这四个场景的 React 参照里
	// 没有出现过，换了参照出现了也会在报告的「未映射角色」一栏里列出；这里不把它们当作范围外的用例。）
	for _, role := range []string{"figure", "progressbar", "article", "tab"} {
		if covers(l1("unmapped-role", role)) {
			t.Errorf("未映射角色 %q 不在 G-022 的范围内，却被放过", role)
		}
	}
	for _, subject := range []string{"button 「批量收藏」", "button 「收起」", "menuitem 「逐条结果（2）」", "button 「逐条结果」"} {
		if covers(l1("extra-node", subject)) {
			t.Errorf("多出的节点 %q 不在 G-022 的范围内，却被放过", subject)
		}
	}
	for _, is := range []parity.Issue{
		{Layer: parity.LayerL2, Kind: "unmapped-role", Subject: "DisclosureTriangle"},
		l1("missing-node", "button 「逐条结果（2）」"),
		l1("text-missing", "「逐条结果（2）」"),
	} {
		if covers(is) {
			t.Errorf("%s/%s %q 不在 G-022 的范围内，却被放过", is.Layer, is.Kind, is.Subject)
		}
	}
}
