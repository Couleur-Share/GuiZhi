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
	// Delta 是偏差的大小（像素），只有外框、文字宽度、图标外框与墨迹位移这几类度量问题带；其余为 0。
	Delta float64 `json:"delta,omitempty"`
	// Dir 是墨迹总量问题的方向：+1 表示 Viva 的墨迹比 React 多，-1 表示少；其余问题为 0。
	// 已登记的偏差有方向（灰度抗锯齿使 Viva 偏多，小字号笔画使 Viva 偏少），据此分开匹配（Waiver.Dir）。
	Dir int8 `json:"dir,omitempty"`
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
	// Inert 是按台账 A-015 补回 Viva 一侧的节点数：模态让背后的节点退出可访问树，比对前按「对话框打开之前」的渲染补回。
	Inert int `json:"inertNodes,omitempty"`
}

// InkShift 记录一行文字墨迹相对 React 的整像素位移（只记非零的，供排查；是否超限由阈值另行判定）。
type InkShift struct {
	Subject string `json:"subject"`
	DX      int    `json:"dx"`
	DY      int    `json:"dy"`
}

// L2Result 汇总度量比较。
type L2Result struct {
	BoxPairs      int     `json:"boxPairs"`
	BoxWithin     int     `json:"boxWithin"`
	MaxBoxDelta   float64 `json:"maxBoxDelta"`
	AlignmentRate float64 `json:"alignmentRate"` // 已对齐节点数 / React 节点数
	TextLines     int     `json:"textLines"`
	TextWidthOK   int     `json:"textWidthOK"`
	MaxWidthDelta float64 `json:"maxWidthDelta"`
	InkChecked    int     `json:"inkChecked"`
	InkOK         int     `json:"inkOK"`
	// InkBlurred 是因遮罩背后模糊而没有比较墨迹的文字行与图标数（台账 A-007）。
	InkBlurred int `json:"inkBlurred,omitempty"`
	// InkOccluded 是因墨迹区域被浮层里的文字或图标侵入而没有比较墨迹的页面文字行与图标数（台账 A-009，见 compare_occlusion.go）。
	InkOccluded int `json:"inkOccluded,omitempty"`
	// InkClipped 是因大半落在截图区域之外而没有比较墨迹的文字行数（区域里只剩几行像素，墨迹总量比不可靠；位置与宽度照常比）。
	InkClipped int        `json:"inkClipped,omitempty"`
	MaxInkDX   int        `json:"maxInkDX"`
	MaxInkDY   int        `json:"maxInkDY"`
	MinInkMass float64    `json:"minInkMass"`
	MaxInkMass float64    `json:"maxInkMass"`
	InkShifts  []InkShift `json:"inkShifts,omitempty"`
	// 图标（见 compare_icons.go）：React 的 SVG 数、与 Viva 配上的数、配上的图标外框最大差。
	Icons        int     `json:"icons"`
	IconsMatched int     `json:"iconsMatched"`
	MaxIconDelta float64 `json:"maxIconDelta"`
	// IconsInvisible 是配上了、但 React 一侧看不见（祖先 opacity 为 0）因而没有比墨迹的图标数。
	IconsInvisible int `json:"iconsInvisible,omitempty"`
	// IconInk 是每枚配上的图标量到的墨迹比与位移，供标定与趋势分析。
	IconInk []InkMeasure `json:"iconInk,omitempty"`
}

// Cluster 是一片相连的差异像素（8 邻接）的外框、像素数与最大通道差，用来定位差异落在哪、判断是抗锯齿噪声还是实打实的错位。
type Cluster struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"w"`
	Height int `json:"h"`
	Pixels int `json:"pixels"`
	// MaxDelta 是片内像素的最大通道差（0–255）。圆角与图标边缘的抗锯齿差通常只有几十，整条线、整块底色的差是上百。
	MaxDelta int `json:"maxDelta"`
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
	// Strongest 是最大通道差最高的几片差异（至多 8 片，先按最大通道差、再按像素数降序）：小而强的差（一条分隔线的底色、
	// 一圈聚焦环）在整窗比例里几乎不占分量，Clusters 里排不上，靠这一份才看得见。
	Strongest []Cluster `json:"strongest,omitempty"`
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

// addDelta 与 add 相同，另记下偏差的大小（像素）：已登记的偏差可以按大小设上限（Waiver.MaxDelta），
// 这样「某个原因造成的次像素位移」只放过次像素的，同一处更大的偏差照常判定。
func (r *Report) addDelta(delta float64, layer Layer, kind, subject, format string, args ...any) {
	r.add(layer, kind, subject, format, args...)
	r.Issues[len(r.Issues)-1].Delta = delta
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
	// Backdrop 非空：场景里有被遮罩背后模糊的区域（台账 A-007），L2 墨迹对这些区域的文字与图标不比较（改由 L3 模糊后比较），
	// L3 对这些区域改比「Viva 图模糊后」。nil 表示没有遮罩。
	Backdrop *Backdrop
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
	if in.Backdrop != nil && len(in.Backdrop.Inert) > 0 {
		// 模态让背后的节点惰性化（台账 A-015）：把它们按「对话框打开之前」的渲染补回 Viva 一侧，背景照常逐节点比对。
		in.Viva = withInertNodes(in.Viva, in.Backdrop.Inert)
		rep.L1.Inert = len(in.Backdrop.Inert)
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
	if r.L1.Inert > 0 {
		fmt.Fprintf(&b, "- L1 惰性背景：Viva 的模态让背后 %d 个节点退出可访问树（台账 A-015），上面的 Viva 节点数已含按「对话框打开之前」的渲染补回的这些节点\n", r.L1.Inert)
	}
	fmt.Fprintf(&b, "- L2 度量：外框 %d/%d 在阈值内（最大差 %.2fpx，对齐率 %.1f%%）；文字行 %d，宽度合格 %d（最大差 %.2fpx）；墨迹合格 %d/%d（最大位移 ΔX %d、ΔY %d；墨迹量比 %.3f–%.3f）\n",
		r.L2.BoxWithin, r.L2.BoxPairs, r.L2.MaxBoxDelta, r.L2.AlignmentRate*100, r.L2.TextLines, r.L2.TextWidthOK, r.L2.MaxWidthDelta,
		r.L2.InkOK, r.L2.InkChecked, r.L2.MaxInkDX, r.L2.MaxInkDY, r.L2.MinInkMass, r.L2.MaxInkMass)
	if r.L2.InkBlurred > 0 {
		fmt.Fprintf(&b, "- L2 墨迹：%d 行文字与图标在遮罩背后被模糊，不比较墨迹（台账 A-007），改由 L3 在 Viva 图模糊后比较\n", r.L2.InkBlurred)
	}
	if r.L2.InkOccluded > 0 {
		fmt.Fprintf(&b, "- L2 墨迹：%d 行文字与图标的墨迹区域被浮层里的文字或图标侵入，不比较墨迹（台账 A-009）；位置与宽度照常比\n", r.L2.InkOccluded)
	}
	if r.L2.InkClipped > 0 {
		fmt.Fprintf(&b, "- L2 墨迹：%d 行文字有大半在截图区域之外（被窗口边缘裁掉），不比较墨迹；位置与宽度照常比\n", r.L2.InkClipped)
	}
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
		if r.L2.IconsInvisible > 0 {
			fmt.Fprintf(&b, "- L2 图标：其中 %d 枚在 React 一侧看不见（祖先 opacity 为 0，悬停才显现），只比外框、不比墨迹，L3 也不遮罩\n", r.L2.IconsInvisible)
		}
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
	if len(r.L3.Strongest) > 0 {
		b.WriteString("L3 差得最狠的差异片（区域内坐标 x,y 宽×高，像素数，最大通道差）：")
		for i, c := range r.L3.Strongest[:min(len(r.L3.Strongest), 5)] {
			if i > 0 {
				b.WriteString("；")
			}
			fmt.Fprintf(&b, "(%d,%d %d×%d，%d，%d)", c.X, c.Y, c.Width, c.Height, c.Pixels, c.MaxDelta)
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
