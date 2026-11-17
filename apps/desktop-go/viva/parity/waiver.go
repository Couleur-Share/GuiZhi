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
	// MaxDelta 大于 0 时，只放过偏差大小（Issue.Delta，像素）不超过它的问题；没有记大小的问题不放过。
	// 用于「某个已登记的原因造成的次像素位移」：原因本身（如字距差）放过，同一处更大的偏差仍要判定。
	MaxDelta float64
	// MinDelta 大于 0 时，只放过偏差大小不小于它的问题：与 MaxDelta 一起把已登记的原因框在「它应当造成的大小」里。
	// 例如字距差必然使一行文字宽 字符数 × 字距，比这小得多的偏差是别的问题；同一段文字出现在别处（遮罩背后的任务行）
	// 时，那里的偏差也不会因为文字相同而被放过。
	MinDelta float64
	// Dir 非零时，只放过方向相同的墨迹总量问题（Issue.Dir：+1 Viva 偏多，-1 Viva 偏少）：
	// 灰度抗锯齿使 Viva 偏多（A-009），小字号拉丁笔画使 Viva 偏少（A-016），两类原因不能互相冒名。
	Dir int8
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
	if w.MaxDelta > 0 && (is.Delta <= 0 || is.Delta > w.MaxDelta+1e-9) {
		return false
	}
	if w.MinDelta > 0 && (is.Delta <= 0 || is.Delta < w.MinDelta-1e-9) {
		return false
	}
	if w.Dir != 0 && w.Dir != is.Dir {
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
