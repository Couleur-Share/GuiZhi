package parity

import (
	"fmt"
	"image"
	"strings"
)

// Layer 是对等性的层：L1 结构、L2 度量、L3 像素。
type Layer string

const (
	LayerL1 Layer = "L1"
	LayerL2 Layer = "L2"
	LayerL3 Layer = "L3"
)

// Issue 是一处不一致。Subject 是涉及的节点或文字（便于定位），Message 是人读的说明。
type Issue struct {
	Layer   Layer  `json:"layer"`
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Message string `json:"message"`
	// Overlay 表示问题出在浮层里的文字上（Viva 把它画在浮层层级）；已登记的浮层文字偏差据此匹配。
	Overlay bool `json:"overlay,omitempty"`
}

// L1Result 汇总结构比较。
type L1Result struct {
	ReactNodes   int      `json:"reactNodes"`
	VivaNodes    int      `json:"vivaNodes"`
	NodesMatched int      `json:"nodesMatched"`
	ReactTexts   int      `json:"reactTexts"`
	VivaTexts    int      `json:"vivaTexts"`
	TextsMatched int      `json:"textsMatched"`
	Unmapped     []string `json:"unmapped,omitempty"`
}

// InkShift 记录一行文字墨迹相对 React 的整像素位移（只记非零的，供排查；是否超限由阈值另行判定）。
type InkShift struct {
	Subject string `json:"subject"`
	DX      int    `json:"dx"`
	DY      int    `json:"dy"`
}

// L2Result 汇总度量比较。
type L2Result struct {
	BoxPairs      int        `json:"boxPairs"`
	BoxWithin     int        `json:"boxWithin"`
	MaxBoxDelta   float64    `json:"maxBoxDelta"`
	AlignmentRate float64    `json:"alignmentRate"` // 已对齐节点数 / React 节点数
	TextLines     int        `json:"textLines"`
	TextWidthOK   int        `json:"textWidthOK"`
	MaxWidthDelta float64    `json:"maxWidthDelta"`
	InkChecked    int        `json:"inkChecked"`
	InkOK         int        `json:"inkOK"`
	MaxInkDX      int        `json:"maxInkDX"`
	MaxInkDY      int        `json:"maxInkDY"`
	MinInkMass    float64    `json:"minInkMass"`
	MaxInkMass    float64    `json:"maxInkMass"`
	InkShifts     []InkShift `json:"inkShifts,omitempty"`
	// 图标（见 compare_icons.go）：React 的 SVG 数、与 Viva 配上的数、配上的图标外框最大差。
	Icons        int     `json:"icons"`
	IconsMatched int     `json:"iconsMatched"`
	MaxIconDelta float64 `json:"maxIconDelta"`
	// IconInk 是每枚配上的图标量到的墨迹比与位移，供标定与趋势分析。
	IconInk []InkMeasure `json:"iconInk,omitempty"`
}

// Cluster 是一片相连的差异像素（8 邻接）的外框与像素数，用来定位差异落在哪。
type Cluster struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"w"`
	Height int `json:"h"`
	Pixels int `json:"pixels"`
}

// L3Result 汇总像素比较。
type L3Result struct {
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	Masked       int     `json:"masked"`
	Compared     int     `json:"compared"`
	Diff         int     `json:"diff"`
	Ratio        float64 `json:"ratio"`
	MaxDiffRatio float64 `json:"maxDiffRatio"`
	// Clusters 是像素最多的几片差异（至多 8 片），按像素数降序。
	Clusters []Cluster `json:"clusters,omitempty"`
}

// Report 是一个场景在一个变体下的对照结果。
type Report struct {
	Scene            string   `json:"scene"`
	Variant          string   `json:"variant"`
	ThresholdsDigest string   `json:"thresholdsDigest"`
	ThresholdsFrozen string   `json:"thresholdsFrozenAt"`
	L1               L1Result `json:"l1"`
	L2               L2Result `json:"l2"`
	L3               L3Result `json:"l3"`
	Issues           []Issue  `json:"issues"`
	// Waived 是被台账登记的偏差覆盖、不计入未通过的问题（见 waiver.go）。
	Waived []Waived `json:"waived,omitempty"`
	Pass   bool     `json:"pass"`
}

func (r *Report) add(layer Layer, kind, subject, format string, args ...any) {
	r.Issues = append(r.Issues, Issue{Layer: layer, Kind: kind, Subject: subject, Message: fmt.Sprintf(format, args...)})
}

// Failed 返回某一层上的问题数。
func (r *Report) Failed(layer Layer) int {
	n := 0
	for _, is := range r.Issues {
		if is.Layer == layer {
			n++
		}
	}
	return n
}

// Inputs 是一次比较的输入：两侧的事实与图片。
type Inputs struct {
	React, Viva       *Facts
	ReactImg, VivaImg image.Image
	// FactsOnly 只比 L1 与 L2 里不依赖图片的部分（外框、文字行数与宽度、折行行距），跳过墨迹与 L3 像素。
	// 单元测试用它：React 参照的 PNG 不入库，入库的只有事实 JSON。
	FactsOnly bool
	// Waivers 是场景登记的已知偏差；覆盖到的问题进 Report.Waived，不使场景失败。
	Waivers []Waiver
}

// Compare 做 L1–L3 比较，返回报告与差异图（react | viva | diff 三联图）。
func Compare(in Inputs, th Thresholds) (*Report, *image.RGBA) {
	rep := &Report{
		Scene:            in.React.Scene,
		Variant:          in.React.Variant.Key(),
		ThresholdsDigest: ThresholdsDigest(),
		ThresholdsFrozen: th.FrozenAt,
		Issues:           []Issue{},
	}
	if in.React.Scene != in.Viva.Scene || in.React.Variant != in.Viva.Variant {
		rep.add(LayerL1, "pairing", rep.Scene, "两侧事实不是同一场景同一变体：react %s/%s，viva %s/%s",
			in.React.Scene, in.React.Variant.Key(), in.Viva.Scene, in.Viva.Variant.Key())
	}
	if in.React.Region != in.Viva.Region {
		rep.add(LayerL1, "region", rep.Scene, "区域不一致：react %+v，viva %+v", in.React.Region, in.Viva.Region)
	}
	if !in.React.Capture.ForcedDSF {
		rep.add(LayerL1, "stale-reference", rep.Scene, "React 参照没有 forcedDsf 标记：它在未钉死设备缩放的采集器下生成，文字基线可能带宿主 DPI 印记，请重新采集")
	}
	nodePairs, textPairs := compareL1(rep, in)
	compareL2(rep, in, th, nodePairs, textPairs)
	compareIcons(rep, in, th)
	var montage *image.RGBA
	if !in.FactsOnly {
		montage = compareL3(rep, in, th)
	}
	applyWaivers(rep, in.Waivers)
	rep.Pass = len(rep.Issues) == 0
	return rep, montage
}

// Markdown 把报告写成人读的摘要。
func (r *Report) Markdown() string {
	var b strings.Builder
	status := "通过"
	if !r.Pass {
		status = "未通过"
	}
	fmt.Fprintf(&b, "# %s · %s：%s\n\n", r.Scene, r.Variant, status)
	fmt.Fprintf(&b, "阈值摘要 `%s`", r.ThresholdsDigest[:12])
	if r.ThresholdsFrozen == "" {
		b.WriteString("（校准中，未冻结）")
	} else {
		fmt.Fprintf(&b, "（冻结于 %s）", r.ThresholdsFrozen)
	}
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "- L1 结构：可访问节点 React %d / Viva %d，对齐 %d；文字 React %d / Viva %d，对齐 %d；未映射角色 %v\n",
		r.L1.ReactNodes, r.L1.VivaNodes, r.L1.NodesMatched, r.L1.ReactTexts, r.L1.VivaTexts, r.L1.TextsMatched, r.L1.Unmapped)
	fmt.Fprintf(&b, "- L2 度量：外框 %d/%d 在阈值内（最大差 %.2fpx，对齐率 %.1f%%）；文字行 %d，宽度合格 %d（最大差 %.2fpx）；墨迹合格 %d/%d（最大位移 ΔX %d、ΔY %d；墨迹量比 %.3f–%.3f）\n",
		r.L2.BoxWithin, r.L2.BoxPairs, r.L2.MaxBoxDelta, r.L2.AlignmentRate*100, r.L2.TextLines, r.L2.TextWidthOK, r.L2.MaxWidthDelta,
		r.L2.InkOK, r.L2.InkChecked, r.L2.MaxInkDX, r.L2.MaxInkDY, r.L2.MinInkMass, r.L2.MaxInkMass)
	if r.L2.Icons > 0 {
		lo, hi := 0.0, 0.0
		for i, m := range r.L2.IconInk {
			if i == 0 || m.Ratio < lo {
				lo = m.Ratio
			}
			if i == 0 || m.Ratio > hi {
				hi = m.Ratio
			}
		}
		fmt.Fprintf(&b, "- L2 图标：React %d 枚，与 Viva 配上 %d 枚（外框最大差 %.2fpx）；配上的图标墨迹量比 %.3f–%.3f\n", r.L2.Icons, r.L2.IconsMatched, r.L2.MaxIconDelta, lo, hi)
	}
	fmt.Fprintf(&b, "- L3 像素：%dx%d，遮罩 %d，比较 %d，差异 %d，比例 %.4f%%（上限 %.4f%%）\n\n",
		r.L3.Width, r.L3.Height, r.L3.Masked, r.L3.Compared, r.L3.Diff, r.L3.Ratio*100, r.L3.MaxDiffRatio*100)
	if len(r.L3.Clusters) > 0 {
		b.WriteString("L3 最大的差异片（区域内坐标 x,y 宽×高，像素数）：")
		for i, c := range r.L3.Clusters[:min(len(r.L3.Clusters), 5)] {
			if i > 0 {
				b.WriteString("；")
			}
			fmt.Fprintf(&b, "(%d,%d %d×%d，%d)", c.X, c.Y, c.Width, c.Height, c.Pixels)
		}
		b.WriteString("\n\n")
	}
	if len(r.Issues) == 0 {
		b.WriteString("无不一致。\n")
		b.WriteString(r.waivedMarkdown())
		return b.String()
	}
	fmt.Fprintf(&b, "## 不一致（%d）\n\n", len(r.Issues))
	for _, is := range r.Issues {
		fmt.Fprintf(&b, "- [%s/%s] %s：%s\n", is.Layer, is.Kind, is.Subject, is.Message)
	}
	b.WriteString(r.waivedMarkdown())
	return b.String()
}
