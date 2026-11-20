package scenes

import "guizhi-mygo-shell/viva/parity"

// 详情（M2-4）对照里新增的几处放过项。
//
// 它们是 2026-11-21 过夜自主期间登记的，依据是 T2（36 个外观变体）的实测：范围一律框在「方向 + 上限 + 下限」里
// （实测的最小、最大值向外取整到 0.01），只有单个字号档或主题取得了证据的，再用 Variant 限在那一档、那种主题里。
// **它们都待用户追认（台账 C-010）**：追认之前在代码里生效、报告里照常标着台账编号；
// 不追认就删掉这里的条目，对应场景会重新出现这些墨迹总量问题（位置、宽度、颜色、L3 像素不受影响，照常判定）。

// imageTabIconWaivers 登记 A-003 在 12px 描边图标上的延伸：图中文字页签上的 `ScanText`（`h-3 w-3`，中档字号档 12px，描边宽 1px）。
// 细笔画在 Skia 的采样量化里偏淡，Viva 按精确面积覆盖，墨迹总量比渲染层偏多：中档 24 处实测 0.194–0.213（明暗 × 6 色相，
// detail-image 与 detail-image-text），小、大字号档的 10.5px、13.5px 同一枚图标不超阈值，所以不登记。
// 图标的主题只有外框、没有身份，所以按边长（Subject 写法同 wordmarkIconWaivers）加方向、区间框定。
func imageTabIconWaivers() []parity.Waiver {
	return []parity.Waiver{{
		Ledger: "A-003", Layer: parity.LayerL2, Kind: "ink-mass", Subject: " 12.00 12.00]", Dir: 1, MinDelta: 0.19, MaxDelta: 0.22,
		Note: "12px 描边图标（图中文字页签的 ScanText）：细笔画在 Skia 里偏淡，Viva 按精确面积覆盖，墨迹总量偏多约 0.19–0.21（外框、位置一致）",
	}}
}

// middleDotWaivers 登记 G-016 在行内分隔点「·」上的延伸：版本记录行里「标题 · 时间」的间隔点，笔画只有一个小圆点，
// 墨迹总量由亚像素相位与两个引擎的灰度校正决定。位置、宽度、颜色与字号一致（文字起点差不到 0.001px，宽度差不超过 0.011px）。
// 与「.」（Viva 偏多）相反，这里 Viva 偏少，而且与字号档有关：小档 0.2105–0.2420、大档 0.3059–0.3442（detail-revisions-open，
// 明暗 × 6 色相各 24 处）；中档不超阈值。所以分两档各框各的实测区间，方向固定为「Viva 偏少」。
func middleDotWaivers() []parity.Waiver {
	rows := []struct {
		variant string
		lo, hi  float64
	}{
		{"-small-", 0.21, 0.25},
		{"-large-", 0.30, 0.35},
	}
	out := make([]parity.Waiver, 0, len(rows))
	for _, r := range rows {
		out = append(out, parity.Waiver{
			Ledger: "G-016", Layer: parity.LayerL2, Kind: "ink-mass", Subject: "「·」", Dir: -1, Variant: r.variant, MinDelta: r.lo, MaxDelta: r.hi,
			Note: "行内分隔点「·」：孤立的小字形，墨迹总量由亚像素相位与灰度校正决定，Viva 偏少（位置、宽度、颜色、字号均一致）",
		})
	}
	return out
}

// overlayThinGlyphWaivers 登记 A-009 在浮层里一像素细笔画字形上的延伸：表格详情浮层顶部整理导航条的位置「1 / 12」
// （`LibraryWorkflow`，text-xs）里的「/」与首位数字「1」。浮层文字在渲染层里是灰度抗锯齿，Viva 整窗 LCD（A-009）；
// 笔画只有一个像素宽的字形上，LCD 的彩边占墨迹的比例更大，墨迹总量比超出 overlayInkWaiver 的上限 1.5。
// 证据（2026-11-21，深色皇家蓝小档 1280×800 的 table-detail-modal，裁切放大 12 倍，
// `D:\Project\GuiZhi-bench\m24-wip\img\nav-react.png` 与 `nav-viva.png`，不入库）：渲染层的「1 / 39」只有灰阶，Viva 的同一行
// 每个笔画列带蓝色与橙色彩边；两侧文字的字号、字重、颜色相同，起点差不到 0.02px。
// 深色主题下读数：「/」小档 1.5747、中档 1.6282，「1」小档 1.5081（各 6 个色相逐个相同），浅色主题与大档不超阈值，
// 所以限深色、浮层里、Viva 偏多，各框各的实测区间（向外取整到 0.01）：「/」[0.57, 0.63]，「1」[0.50, 0.51]。
func overlayThinGlyphWaivers() []parity.Waiver {
	rows := []struct {
		subject string
		lo, hi  float64
	}{
		{"「/」", 0.57, 0.63},
		{"「1」", 0.50, 0.51},
	}
	out := make([]parity.Waiver, 0, len(rows))
	for _, r := range rows {
		out = append(out, parity.Waiver{
			Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass", Subject: r.subject, Overlay: true, Variant: "dark-", Dir: 1, MinDelta: r.lo, MaxDelta: r.hi,
			Note: "浮层里一像素细笔画的字形：灰度抗锯齿对 LCD 彩边，Viva 偏多超出 overlayInkWaiver 的上限（位置、宽度、颜色、字号均一致）",
		})
	}
	return out
}
