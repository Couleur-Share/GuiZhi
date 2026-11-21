package scenes

import "guizhi-mygo-shell/viva/parity"

// 版本比较两栏里的等宽原文（`pre`：font-mono、overflow-auto）：两个字体回退的偏差，登记范围待追认（台账 C-015）。
//
//  1. 墨迹总量（A-009 的延伸）：渲染层的 `overflow-auto` 的 pre 是合成层里的滚动容器，文字用灰度抗锯齿；Viva 整窗 LCD，墨迹总量偏大。
//     对照图上同一栏里「当前正文」标题（在滚动容器之外）两边都是 LCD，只有 pre 里的字没有彩边——证据见台账 C-015。
//  2. 含汉字的行位置（±1px）：渲染层的汉字回退到界面字体，落在首选字体 Consolas 定的基线上，字形比 Viva 的同一字体高一行像素
//     （12px 下 12 行对 11 行）；kit.MonoParagraph 已把整行下移到底边对齐，墨迹剖面仍有 ±1px 错位。只放过 ≤1px 的位移（横纵都不超过 1）。
//  3. 混排行（C-015）：行里的拉丁字符在渲染层是 Consolas，在 Viva 里随整行用界面字体画，字形与宽度不同，行内水平错位到 3px 左右。
//
// Subject 取两栏文字共有的开头与混排行里的一小段，范围只到这两栏的原文。
const (
	versionPaneLead  = "本地优先的软件把数据放在用户自己的设备上，同步只是附加能力。"
	versionPaneMixed = "新增：CRDT 与最后写入者胜出的取舍。"
)

// 英文界面里工具侧栏的两个小标题（text-sm font-medium，500 字重）：Viva 对 500 字重做字偶距、Chromium 不做（A-026 的英文延伸），
// 含 Ta 这类字偶对的整行偏窄、墨迹左移 1 像素。读数（M2-6b，英文变体）：「Tags」宽度差 1.67px（React 30.88、Viva 29.20），
// 「Captured source versions」1.43px（React 165.23、Viva 163.80）。只在英文变体所在的字号档放过。
const (
	enToolsTags     = "Tags"                     // articleReader.tags
	enToolsVersions = "Captured source versions" // articleReader.sourceVersions
)

func enToolsWaivers() []parity.Waiver {
	var out []parity.Waiver
	for _, title := range []string{enToolsTags, enToolsVersions} {
		subject := "「" + title + "」"
		out = append(out,
			englishKerning(kerning600Ledger, "text-width", subject, kerningWidthCap, noteKern600),
			englishKerning(kerning600Ledger, "ink", subject, kerningInkCap, noteKern600),
		)
	}
	return out
}

func versionPaneWaivers() []parity.Waiver {
	return []parity.Waiver{
		{
			Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass", Subject: versionPaneLead, Variant: "light-", MinDelta: 0.1, MaxDelta: 0.4, Dir: 1,
			Note: "版本比较的原文栏是合成层里的滚动容器，文字用灰度抗锯齿；Viva 整窗 LCD，墨迹总量偏大（位置、字重、颜色、字号均一致）",
		},
		{
			Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass", Subject: versionPaneLead, Variant: "dark-", MinDelta: 0.15, MaxDelta: 0.55, Dir: 1,
			Note: "同上，深色主题：浅字压深底，灰度与 LCD 的墨迹总量差随字号档分布更宽（T2 三档实测 0.167–0.534）",
		},
		{
			Ledger: "C-015", Layer: parity.LayerL2, Kind: "ink", Subject: versionPaneLead, MaxDelta: 1, MaxDY: 1,
			Note: "含汉字的等宽原文行：汉字字形在渲染层比 Viva 高一行像素（回退字体落在 Consolas 的基线上），按底边对齐后墨迹剖面仍有 ≤1px 错位",
		},
		{
			Ledger: "C-015", Layer: parity.LayerL2, Kind: "ink", Subject: versionPaneMixed, MaxDelta: 3.5, MaxDY: 1,
			Note: "汉字与拉丁字符混排的行：行内拉丁字符在渲染层是 Consolas，在 Viva 里随整行用界面字体画，水平方向累计错位约 3px",
		},
	}
}
