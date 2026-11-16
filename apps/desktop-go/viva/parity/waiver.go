package parity

import (
	"fmt"
	"strings"
)

// Waiver 把一类已在台账登记的偏差从「不一致」里分出来：问题照样出现在报告里（附台账编号），但不使场景失败。
//
// 它不是放宽阈值：只放过「同一层、同一类、涉及同一对象」的问题，其余问题照常判定。
// 每个 Waiver 必须指向台账里真实存在的条目（scenes 包的测试核对），所以不会有没登记的偏差溜过去。
type Waiver struct {
	// Ledger 是台账条目的编号，如 "A-006"。
	Ledger string
	// Layer 与 Kind 限定被放过的问题类别（Issue.Layer / Issue.Kind）；Kind 为空表示该层的任意类别。
	Layer Layer
	Kind  string
	// Subject 非空时，只放过 Issue.Subject 里含有这段文字的问题（例如某一行文字、某一枚图标）。
	Subject string
	// Overlay 为真时，只放过出在浮层文字上的问题（Issue.Overlay）：同一段文字在页面层里出的问题照常判定。
	Overlay bool
	// Note 说明偏差的内容与范围，写进报告，供阅读报告的人不必翻台账。
	Note string
}

// Waived 是被放过的一条问题及其依据。
type Waived struct {
	Issue  Issue  `json:"issue"`
	Ledger string `json:"ledger"`
	Note   string `json:"note,omitempty"`
}

// matches 判断 w 是否覆盖 issue。
func (w Waiver) matches(is Issue) bool {
	if w.Layer != is.Layer {
		return false
	}
	if w.Kind != "" && w.Kind != is.Kind {
		return false
	}
	if w.Overlay && !is.Overlay {
		return false
	}
	return w.Subject == "" || strings.Contains(is.Subject, w.Subject)
}

// applyWaivers 把报告里被 waivers 覆盖的问题移到 Waived。
func applyWaivers(rep *Report, waivers []Waiver) {
	if len(waivers) == 0 {
		return
	}
	kept := rep.Issues[:0]
	for _, is := range rep.Issues {
		waived := false
		for _, w := range waivers {
			if w.matches(is) {
				rep.Waived = append(rep.Waived, Waived{Issue: is, Ledger: w.Ledger, Note: w.Note})
				waived = true
				break
			}
		}
		if !waived {
			kept = append(kept, is)
		}
	}
	rep.Issues = kept
}

// waivedMarkdown 把被放过的问题按台账编号分组写成一节。
func (r *Report) waivedMarkdown() string {
	if len(r.Waived) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n## 已登记的偏差（%d，不计入未通过）\n\n", len(r.Waived))
	for _, w := range r.Waived {
		fmt.Fprintf(&b, "- [%s] [%s/%s] %s：%s\n", w.Ledger, w.Issue.Layer, w.Issue.Kind, w.Issue.Subject, w.Issue.Message)
	}
	return b.String()
}
