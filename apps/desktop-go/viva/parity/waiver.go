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
	// Emoji 为真时，只放过主题是单个彩色表情字形的问题（Issue.Subject 形如「💼」）：
	// 表情目录有上百个，逐个写 Subject 既冗长又会漏；按「是不是表情」圈定范围，普通文字的同类偏差照常判定。
	Emoji bool
	// MaxDelta 大于 0 时，只放过偏差大小（Issue.Delta，像素）不超过它的问题；没有记大小的问题不放过。
	// 用于「某个已登记的原因造成的次像素位移」：原因本身（如字距差）放过，同一处更大的偏差仍要判定。
	MaxDelta float64
	// MinDelta 大于 0 时，只放过偏差大小不小于它的问题：与 MaxDelta 一起把已登记的原因框在「它应当造成的大小」里。
	// 例如字距差必然使一行文字宽 字符数 × 字距，比这小得多的偏差是别的问题；同一段文字出现在别处（遮罩背后的任务行）
	// 时，那里的偏差也不会因为文字相同而被放过。
	MinDelta float64
	// MaxDY 是墨迹错位里纵向位移（Issue.DY）允许的最大值；缺省 0 表示不放过任何纵向错位（台账 G-012）。
	// 字距、字偶距、字格取整这类原因只会让文字沿行方向错位；Delta 取水平与纵向的较大值，所以「横向错 2 像素」的登记
	// 如果不限纵向，会把「纵向错 1 像素」的基线问题一并放过（M2-1 里先后藏过分组标题与表情的两处真缺陷）。
	// 确有已登记的纵向偏差时（如等宽快捷键提示的基线，B-022），显式写出它的上限，让纵向偏差也有名有姓。
	MaxDY float64
	// Dir 非零时，只放过方向相同的墨迹总量问题（Issue.Dir：+1 Viva 偏多，-1 Viva 偏少）：
	// 灰度抗锯齿使 Viva 偏多（A-009），小字号拉丁笔画使 Viva 偏少（A-016），两类原因不能互相冒名。
	Dir int8
	// Shared 为真时，只放过量墨迹的区域里整个包着另一行小数左缘文字的墨迹问题（Issue.Shared，运行时由比较器按事实判定）：
	// 同一行文字单独出现时的同类偏差照常判定。
	Shared bool
	// Clipped 为真时，只放过文字或图标被祖先裁剪线截掉一截的墨迹问题（Issue.Clipped，运行时由比较器按事实判定）：
	// 没被裁的同类图标、文字照常判定。
	Clipped bool
	// Variant 非空时，只在变体键（如 "light-royal-blue-large-1280x800"）含有这段文字的变体里放过：
	// 登记依据只在某一档字号、某种主题下取得时，不把它扩到别的变体。
	Variant string
	// Note 说明偏差的内容与范围，写进报告，供阅读报告的人不必翻台账。
	Note string
}

// Waived 是被放过的一条问题及其依据。
type Waived struct {
	Issue  Issue  `json:"issue"`
	Ledger string `json:"ledger"`
	Note   string `json:"note,omitempty"`
}

// matches 判断 w 是否覆盖 issue（不限变体：带 Variant 的登记在这里不匹配，见 matchesIn）。
func (w Waiver) matches(is Issue) bool { return w.matchesIn(is, "") }

// matchesIn 判断 w 是否覆盖 variant 这个变体里的 issue。
func (w Waiver) matchesIn(is Issue, variant string) bool {
	if w.Variant != "" && !strings.Contains(variant, w.Variant) {
		return false
	}
	if w.Shared && !is.Shared {
		return false
	}
	if w.Clipped && !is.Clipped {
		return false
	}
	if w.Layer != is.Layer {
		return false
	}
	if w.Kind != "" && w.Kind != is.Kind {
		return false
	}
	if w.Overlay && !is.Overlay {
		return false
	}
	if w.Emoji && !emojiSubject(is.Subject) {
		return false
	}
	if w.MaxDelta > 0 && (is.Delta <= 0 || is.Delta > w.MaxDelta+1e-9) {
		return false
	}
	if w.MinDelta > 0 && (is.Delta <= 0 || is.Delta < w.MinDelta-1e-9) {
		return false
	}
	if is.DY > w.MaxDY+1e-9 {
		return false
	}
	if w.Dir != 0 && w.Dir != is.Dir {
		return false
	}
	return w.Subject == "" || strings.Contains(is.Subject, w.Subject)
}

// emojiSubject 报告 Issue.Subject（「文字」的写法）里的文字是不是单个彩色表情：
// 一个表情基本字符，后面可跟变体选择符 U+FE0F、肤色修饰符与 ZWJ 连接的序列。
// 范围取表情所在的区块（Emoticons 与补充图形区 U+1F000–1FAFF、杂项符号与装饰符号 U+2600–27BF、
// 杂项技术符号 U+2300–23FF、杂项符号和箭头 U+2B00–2BFF）；普通文字、数字与标点都不算。
func emojiSubject(subject string) bool {
	text := strings.TrimSuffix(strings.TrimPrefix(subject, "「"), "」")
	base := 0
	for _, r := range text {
		switch {
		case r == 0xFE0F || r == 0x200D || (r >= 0x1F3FB && r <= 0x1F3FF):
			// 变体选择符、ZWJ、肤色修饰符：只能跟在表情之后。
		case (r >= 0x1F000 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF) ||
			(r >= 0x2300 && r <= 0x23FF) || (r >= 0x2B00 && r <= 0x2BFF):
			base++
		default:
			return false
		}
	}
	return base > 0
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
			if w.matchesIn(is, rep.Variant) {
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
