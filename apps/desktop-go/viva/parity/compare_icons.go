package parity

import (
	"fmt"
	"math"
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
			rep.add(LayerL2, "icon-box", subject, "外框差 %.2fpx（上限 %.2f）：React %s，Viva %s", d, th.Metrics.MaxDeltaPx, fmtBox(p.React), fmtBox(p.Viva))
			continue
		}
		if !in.FactsOnly && in.ReactImg != nil && in.VivaImg != nil {
			if m := checkInk(rep, in, th, subject, p.React, false); m != nil {
				rep.L2.IconInk = append(rep.L2.IconInk, *m)
			}
		}
	}
}
