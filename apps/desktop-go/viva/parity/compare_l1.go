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

// compareTexts 比较文字序列。折行时 Viva 把一段文字拆成多行多个条目，所以按「去掉空白后的拼接」逐段消费。
//
// 区域裁剪：React 参照只记录区域内可见的文字；一段折行文字可能第一行在区域内、后几行在区域外。
// 所以匹配时 Viva 的区域外的行也可以用来补全一段文字（拼接要等于 React 的整段文字），但区域外的行
// 不参与后续的行数、宽度、墨迹比较（textPair.Viva 只留区域内的行），区域外没被消费的行也不算多余。
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
			if c := r.Clip(region); c.W() > 0 && c.H() > 0 {
				visible = true
			}
		}
		if visible {
			visibleCount++
		}
		vivaTexts = append(vivaTexts, vivaLine{line: t, visible: visible})
	}
	rep.L1.ReactTexts, rep.L1.VivaTexts = len(reactTexts), visibleCount

	var out []textPair
	vi := 0
	for _, rt := range reactTexts {
		// 起点之前区域外的 Viva 行不属于任何 React 文字，直接跳过。
		for vi < len(vivaTexts) && !vivaTexts[vi].visible {
			vi++
		}
		target := SquashText(rt.Text)
		if vi >= len(vivaTexts) {
			rep.add(LayerL1, "missing-text", quote(NormText(rt.Text)), "React 有、Viva 没有")
			continue
		}
		var group []Text
		acc := ""
		start := vi
		for vi < len(vivaTexts) {
			next := acc + SquashText(vivaTexts[vi].line.Text)
			if !strings.HasPrefix(target, next) {
				break
			}
			acc = next
			if vivaTexts[vi].visible {
				group = append(group, vivaTexts[vi].line)
			}
			vi++
			if acc == target {
				break
			}
		}
		if acc != target {
			rep.add(LayerL1, "text", quote(NormText(rt.Text)), "文字不一致：Viva 对应位置是「%s」", NormText(vivaTexts[start].line.Text))
			if vi == start {
				vi++ // 不匹配的一条 Viva 文字跳过，免得整个序列错位
			}
			continue
		}
		out = append(out, textPair{React: rt, Viva: group})
	}
	for ; vi < len(vivaTexts); vi++ {
		if vivaTexts[vi].visible {
			rep.add(LayerL1, "extra-text", quote(NormText(vivaTexts[vi].line.Text)), "Viva 有、React 没有")
		}
	}
	rep.L1.TextsMatched = len(out)
	return out
}
