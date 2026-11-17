package parity

import (
	"strings"
)

// nodePair 是两侧对齐上的一对归一化节点。
type nodePair struct{ React, Viva Norm }

// textPair 是 React 的一段文字与 Viva 里拼起来等于它的若干段文字。
type textPair struct {
	React Text
	Viva  []Text
}

// compareL1 比较结构：可访问节点序列（角色、名称、状态、层级）与文字序列。返回对齐上的节点对与文字对，供 L2 使用。
func compareL1(rep *Report, in Inputs) ([]nodePair, []textPair) {
	reactNodes, unmapped := NormalizeReact(in.React)
	vivaNodes := NormalizeViva(in.Viva)
	rep.L1.ReactNodes, rep.L1.VivaNodes = len(reactNodes), len(vivaNodes)
	rep.L1.Unmapped = unmapped
	for _, role := range unmapped {
		rep.add(LayerL1, "unmapped-role", role, "React 角色 %q 没有映射规则，也不在忽略表里：先在 a11y_map.go 里明确决定", role)
	}

	ra := make([]string, len(reactNodes))
	for i, n := range reactNodes {
		ra[i] = n.Key()
	}
	va := make([]string, len(vivaNodes))
	for i, n := range vivaNodes {
		va[i] = n.Key()
	}
	pairs, onlyReact, onlyViva := alignKeys(ra, va)
	for _, i := range onlyReact {
		n := reactNodes[i]
		rep.add(LayerL1, "missing-node", describe(n), "React 有、Viva 没有")
	}
	for _, j := range onlyViva {
		n := vivaNodes[j]
		rep.add(LayerL1, "extra-node", describe(n), "Viva 有、React 没有")
	}
	var matched []nodePair
	for _, p := range pairs {
		rn, vn := reactNodes[p[0]], vivaNodes[p[1]]
		if rn.StateString() != vn.StateString() {
			rep.add(LayerL1, "state", describe(rn), "状态不一致：React「%s」，Viva「%s」", rn.StateString(), vn.StateString())
			continue
		}
		if rn.Description != vn.Description {
			rep.add(LayerL1, "description", describe(rn), "可访问描述不一致：React「%s」，Viva「%s」", rn.Description, vn.Description)
			continue
		}
		matched = append(matched, nodePair{React: rn, Viva: vn})
	}
	rep.L1.NodesMatched = len(matched)

	textPairs := compareTexts(rep, in)
	return matched, textPairs
}

func describe(n Norm) string {
	return n.Role + " " + quote(n.Name)
}

func quote(s string) string { return "「" + s + "」" }

// isEllipsisCut 报告 react 与 viva 是不是同一段文字的两种省略号截断：两边都以「…」收尾，
// 去掉省略号后其中一个是另一个的前缀（截在不同的字上），且不完全相同。
func isEllipsisCut(react, viva string) bool {
	const ellipsis = "…"
	if !strings.HasSuffix(react, ellipsis) || !strings.HasSuffix(viva, ellipsis) {
		return false
	}
	a, b := strings.TrimSuffix(react, ellipsis), strings.TrimSuffix(viva, ellipsis)
	return a != b && a != "" && b != "" && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
}

// compareTexts 比较文字序列。折行时 Viva 把一段文字拆成多行多个条目，所以按「去掉空白后的拼接」逐段消费。
//
// 区域裁剪：React 参照只记录区域内可见的文字；一段折行文字可能第一行在区域内、后几行在区域外。
// 所以匹配时 Viva 的区域外的行也可以用来补全一段文字（拼接要等于 React 的整段文字），但区域外的行
// 不参与后续的行数、宽度、墨迹比较（textPair.Viva 只留区域内的行），区域外没被消费的行也不算多余。
// 被滚动容器裁掉、没有画出来的行（Text.Hidden，A-010）同理：不算呈现出来的文字，但可以补全折行的原文。
func compareTexts(rep *Report, in Inputs) []textPair {
	region := in.React.Region.Box()
	var reactTexts []Text
	for _, t := range in.React.Texts {
		if SquashText(t.Text) != "" {
			reactTexts = append(reactTexts, t)
		}
	}
	type vivaLine struct {
		line    Text
		visible bool
	}
	var vivaTexts []vivaLine
	visibleCount := 0
	for _, t := range in.Viva.Texts {
		if SquashText(t.Text) == "" {
			continue
		}
		visible := false
		for _, r := range t.Rects {
			if c := r.Clip(region); c.W() > 0 && c.H() > 0 && !t.Hidden {
				visible = true
			}
		}
		if visible {
			visibleCount++
		}
		vivaTexts = append(vivaTexts, vivaLine{line: t, visible: visible})
	}
	rep.L1.ReactTexts, rep.L1.VivaTexts = len(reactTexts), visibleCount

	// consume 从 from 起累加 Viva 行，直到拼接等于 target；group 只收看得见的行。
	// 整段都看不见（group 为空）不算命中：React 记录的文字至少有一行在区域内可见。
	consume := func(from int, target string) (group []Text, end int, ok bool) {
		acc := ""
		for end = from; end < len(vivaTexts); {
			next := acc + SquashText(vivaTexts[end].line.Text)
			if !strings.HasPrefix(target, next) {
				break
			}
			acc = next
			if vivaTexts[end].visible {
				group = append(group, vivaTexts[end].line)
			}
			end++
			if acc == target {
				return group, end, len(group) > 0
			}
		}
		return nil, end, false
	}

	var out []textPair
	vi := 0
	for _, rt := range reactTexts {
		target := SquashText(rt.Text)
		// 当前位置起第一条看得见的 Viva 行。它之前看不见的行有两种可能：不属于任何 React 文字（区域外、
		// 滚出容器的零散文字），或者是一段折行文字滚出去的开头几行；所以起点从 vi 到 first 逐个试，
		// 但不能越过看得见的行——跳过一条看得见的行就等于吞掉一处不一致。
		first := vi
		for first < len(vivaTexts) && !vivaTexts[first].visible {
			first++
		}
		if first >= len(vivaTexts) {
			rep.add(LayerL1, "missing-text", quote(NormText(rt.Text)), "React 有、Viva 没有")
			continue
		}
		matched := false
		for start := vi; start <= first; start++ {
			if group, end, ok := consume(start, target); ok {
				out = append(out, textPair{React: rt, Viva: group})
				vi = end
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		vivaText := NormText(vivaTexts[first].line.Text)
		if isEllipsisCut(NormText(rt.Text), vivaText) {
			// 同一段文字被省略号截断、只是截在不同的字上：单独成类，便于登记（如字距使可见前缀长短不一，A-013）。
			rep.add(LayerL1, "ellipsis-cut", quote(NormText(rt.Text)), "省略号截断点不同：React 看得见的是「%s」，Viva 是「%s」", NormText(rt.Text), vivaText)
		} else {
			rep.add(LayerL1, "text", quote(NormText(rt.Text)), "文字不一致：Viva 对应位置是「%s」", vivaText)
		}
		rep.Issues[len(rep.Issues)-1].Overlay = vivaTexts[first].line.Overlay
		// 已经吻合了前缀的行算作这一段，剩下的才留给后面的 React 文字；一行都没吻合就跳过这一条，免得序列错位。
		_, end, _ := consume(first, target)
		vi = max(first+1, end)
	}
	for ; vi < len(vivaTexts); vi++ {
		if vivaTexts[vi].visible {
			rep.add(LayerL1, "extra-text", quote(NormText(vivaTexts[vi].line.Text)), "Viva 有、React 没有")
		}
	}
	rep.L1.TextsMatched = len(out)
	return out
}
