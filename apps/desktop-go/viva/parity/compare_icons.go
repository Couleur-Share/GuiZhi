package parity

import (
	"fmt"
	"math"
	"slices"
	"sort"
)

// 矢量图标的比较
//
// 渲染层的图标是 lucide 的 SVG，Viva 的图标由自带光栅化器画成三角形：边缘的抗锯齿灰度无法逐像素一致
// （台账 A-003）。所以图标和文字一样：L3 的逐像素比较遮掉图标区域，改在 L2 里按「位置与尺寸」「墨迹总量与位置」比较——
// 图标缺了、多了、错位、变粗变细、换成另一枚形状不同的图标，都会在这里暴露。

// reactIcons 返回 React 事实里全部 SVG 图标的外框（paints 里 tag 为 svg 的项），按文档顺序。
func reactIcons(f *Facts) []Box {
	var out []Box
	for _, p := range f.Paints {
		if p.Tag == "svg" {
			out = append(out, p.Box)
		}
	}
	return out
}

// invisibleReactIcons 返回 React 一侧「看不见」的图标外框：自身或祖先 opacity 为 0（悬停才显现的行内工具条等），
// 采集时标了 Paint.Invisible。同一个外框上只要还有一枚看得见的图标，就不算看不见。
// 看不见的图标没有自己的墨迹：外框与数量照常比较，但不比墨迹，L3 也不遮罩它们——底下的内容（常是别的文字、背景）仍要如实比。
func invisibleReactIcons(f *Facts) map[Box]bool {
	hidden := map[Box]bool{}
	visible := map[Box]bool{}
	for _, p := range f.Paints {
		if p.Tag != "svg" {
			continue
		}
		if p.Invisible {
			hidden[p.Box] = true
		} else {
			visible[p.Box] = true
		}
	}
	for b := range visible {
		delete(hidden, b)
	}
	return hidden
}

// maskedIcons 返回 L3 要遮掉的图标外框：两侧的图标，去掉 React 一侧看不见的图标和与之配对的 Viva 图标。
func maskedIcons(in Inputs) []Box {
	react := reactIcons(in.React)
	hidden := invisibleReactIcons(in.React)
	pairs, _, _ := pairIcons(react, in.Viva.Icons)
	pairedHidden := map[Box]bool{}
	for _, p := range pairs {
		if hidden[p.React] {
			pairedHidden[p.Viva] = true
		}
	}
	var out []Box
	for _, b := range react {
		if !hidden[b] {
			out = append(out, b)
		}
	}
	for _, b := range in.Viva.Icons {
		if !pairedHidden[b] {
			out = append(out, b)
		}
	}
	return out
}

// maxIconPairDistance 是配对时两个图标中心点允许的最大距离（逻辑像素）：超过就认为是不同的图标。
const maxIconPairDistance = 3.0

type iconPair struct{ React, Viva Box }

// pairIcons 把 React 与 Viva 的图标按中心距离贪心配对（距离小者先配），返回配对结果与两侧没配上的图标。
func pairIcons(react, viva []Box) (pairs []iconPair, missing, extra []Box) {
	type cand struct {
		r, v int
		d    float64
	}
	var cands []cand
	center := func(b Box) (float64, float64) { return b.X() + b.W()/2, b.Y() + b.H()/2 }
	for i, r := range react {
		rx, ry := center(r)
		for j, v := range viva {
			vx, vy := center(v)
			if d := math.Hypot(rx-vx, ry-vy); d <= maxIconPairDistance {
				cands = append(cands, cand{i, j, d})
			}
		}
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].d < cands[b].d })
	rUsed, vUsed := make([]bool, len(react)), make([]bool, len(viva))
	for _, c := range cands {
		if rUsed[c.r] || vUsed[c.v] {
			continue
		}
		rUsed[c.r], vUsed[c.v] = true, true
		pairs = append(pairs, iconPair{react[c.r], viva[c.v]})
	}
	sort.SliceStable(pairs, func(a, b int) bool {
		if pairs[a].React.Y() != pairs[b].React.Y() {
			return pairs[a].React.Y() < pairs[b].React.Y()
		}
		return pairs[a].React.X() < pairs[b].React.X()
	})
	for i, r := range react {
		if !rUsed[i] {
			missing = append(missing, r)
		}
	}
	for j, v := range viva {
		if !vUsed[j] {
			extra = append(extra, v)
		}
	}
	return pairs, missing, extra
}

// compareIcons 比较图标：数量与配对、外框、墨迹。
func compareIcons(rep *Report, in Inputs, th Thresholds) {
	react := reactIcons(in.React)
	rep.L2.Icons = len(react)
	hidden := invisibleReactIcons(in.React)
	overlayInk := overlayInkRects(in.Viva)
	pairs, missing, extra := pairIcons(react, in.Viva.Icons)
	for _, b := range missing {
		rep.add(LayerL2, "icon-missing", "图标 "+fmtBox(b), "React 有这枚图标，Viva 没有画（或画在别处，中心距离超过 %.0fpx）", maxIconPairDistance)
	}
	for _, b := range extra {
		rep.add(LayerL2, "icon-extra", "图标 "+fmtBox(b), "Viva 画了这枚图标，React 没有")
	}
	for _, p := range pairs {
		rep.L2.IconsMatched++
		subject := fmt.Sprintf("图标 %s", fmtBox(p.React))
		d := boxDelta(p.React, p.Viva)
		rep.L2.MaxIconDelta = math.Max(rep.L2.MaxIconDelta, d)
		if d > th.Metrics.MaxDeltaPx+1e-9 {
			rep.addDelta(d, LayerL2, "icon-box", subject, "外框差 %.2fpx（上限 %.2f）：React %s，Viva %s", d, th.Metrics.MaxDeltaPx, fmtBox(p.React), fmtBox(p.Viva))
			continue
		}
		if hidden[p.React] {
			rep.L2.IconsInvisible++ // 看不见的图标没有墨迹可比：外框已比过，见 invisibleReactIcons
			continue
		}
		if !in.FactsOnly && in.ReactImg != nil && in.VivaImg != nil {
			overlay := slices.Contains(in.Viva.OverlayIcons, p.Viva)
			if in.Backdrop != nil && !overlay {
				// 与页面层文字同一规则（compareText）：有遮罩的场景里页面层的图标在遮罩背后，被模糊，或整个被面板盖住，
				// 量墨迹的区域里是面板自己的内容，不比墨迹（台账 A-007）；面板自己的图标（overlay）照常比。
				rep.L2.InkBlurred++
				continue
			}
			if !overlay && occludedByOverlayInk(p.React, overlayInk) {
				rep.L2.InkOccluded++ // 图标被浮层里的文字或图标盖住一部分：区域里是浮层自己的墨迹，同页面文字的规则（compare_occlusion.go）
				continue
			}
			if m := checkInk(rep, in, th, subject, p.React, false, overlay); m != nil {
				rep.L2.IconInk = append(rep.L2.IconInk, *m)
			}
		}
	}
}
