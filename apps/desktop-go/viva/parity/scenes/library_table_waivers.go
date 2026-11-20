package scenes

import (
	"maps"
	"slices"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 知识库表格场景共有的已登记偏差（台账 A-009 / A-014 / A-026，以及 A-027 的场景约定）。

// tableBaseWaivers 是表格整窗场景都带上的登记：卡片标题的字偶距（A-026）、吸附表头的灰度抗锯齿（A-009）、
// 摘要列的字偶距（A-014）与同行图标的偏移（A-014）。
func tableBaseWaivers() []parity.Waiver {
	out := semiboldKerningWaivers()
	out = append(out, headerInkWaivers()...)
	out = append(out, snippetKerningWaivers()...)
	out = append(out, sourceEllipsisWaivers()...)
	out = append(out, parity.Waiver{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "icon-box", MaxDelta: 1.5,
		Note: "居中的来源单元格里，「YouTube」的字偶距使文字比渲染层宽 2.3px，同一格里的图标偏半个宽度差（≤ 1.3px）",
	})
	return append(out, sharedInkWaiver)
}

// sharedInkWaiver 登记 G-018（B-046 (4)）：吸附的「操作」列压住「更新时间」表头时，量「更新时间」墨迹的区域里整个包着「操作」那行文字，
// 而「操作」的左缘在小数像素上（1205.5）：墨迹剖面是两行文字的合集，Viva 整像素落位、渲染层子像素定位（A-001）的半像素位移属于被包着的那一行，
// 让外面这行的剖面对齐偏 1 列。前提由比较器按事实判定（Issue.Shared），所以同一行文字单独出现、或包着的那行左缘是整数像素时照常判定；
// 只放过水平 1 像素（MaxDelta 1），纵向错位不放过（MaxDY 缺省 0）。
var sharedInkWaiver = parity.Waiver{
	Ledger: "G-018", Layer: parity.LayerL2, Kind: "ink", Shared: true, MaxDelta: 1,
	Note: "量墨迹的区域里整个包着另一行小数左缘的文字（吸附列压住的表头）：剖面是两行的合集，被包着那行的半像素位移（A-001）让外面这行对齐偏 1 列",
}

// tableScrolledWaivers 是 table-scrolled 在 tableBaseWaivers 之外多带的两条（B-046 (5)(7)(b)）：
//   - A-003 的延伸：小字号档里被面板下沿裁成半截的 14px 行内图标，看得见的只剩上半截，墨迹的大头是边缘的抗锯齿，Viva 比渲染层少约 0.25；
//     前提（Issue.Clipped）由比较器按事实判定，没被裁的同一枚图标照常判定；只管小字号档、只放过偏少的方向、偏差不超过 0.26。
//   - A-028：大字号档吸附的「操作」表头标签比同一行别的表头标签高一行，只放过这一个标签、只在大字号档、只放过 1 像素以内。
func tableScrolledWaivers() []parity.Waiver {
	return []parity.Waiver{
		{
			Ledger: "A-003", Layer: parity.LayerL2, Kind: "ink-mass", Subject: " 14.00 14.00]", Dir: -1, Clipped: true, Variant: "-small-", MaxDelta: 0.26,
			Note: "被面板下沿裁成半截的行内图标：看得见的只剩上半截，边缘抗锯齿占墨迹的大头，Viva 的墨迹总量比渲染层少 0.24–0.25（A-003 的延伸）",
		},
		{
			Ledger: "A-028", Layer: parity.LayerL2, Kind: "ink", Subject: "「" + locales.LibraryColumnActions() + "」", Variant: "-large-", MaxDelta: 1, MaxDY: 1,
			Note: "大字号档吸附的「操作」表头标签，渲染层把它画得比同一行别的表头标签高一行（合成层原点取整，A-028）；Viva 与同行标签同一行",
		},
	}
}

// headerInkWaivers 登记 A-009 在吸附表头上的延伸：表头是 `sticky` 元素，Chromium 把它放进独立的合成层，
// 层里的文字用灰度抗锯齿（实测深色主题下表头文字的 RGB 色差只有 24–25，页面上别的文字有 100 以上的彩边）；
// Viva 整窗 LCD，墨迹总量偏大（深色主题下 Viva / React 为 1.255–1.345，浅色主题在阈值之内），位置、字重、颜色、字号都一致。
// 只放过「Viva 比 React 多」且偏差落在 [0.1, 0.4] 的方向；每个列名一条，Subject 带引号，不会误放行里同名的文字。
func headerInkWaivers() []parity.Waiver {
	labels := []string{
		locales.LibraryColumnTitle(), locales.LibraryColumnSnippet(), locales.LibraryColumnTags(), locales.LibraryColumnType(),
		locales.LibraryColumnSource(), locales.LibraryColumnCollection(), locales.LibraryColumnStatus(),
		locales.LibraryColumnCreatedAt(), locales.LibraryColumnUpdatedAt(), locales.LibraryColumnActions(),
	}
	out := make([]parity.Waiver, 0, len(labels))
	for _, label := range labels {
		out = append(out, parity.Waiver{
			Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass", Subject: "「" + label + "」", MinDelta: 0.1, MaxDelta: 0.4, Dir: 1,
			Note: "吸附表头在合成层里用灰度抗锯齿，Viva 整窗 LCD，墨迹总量偏大（位置、字重、颜色、字号均一致）",
		})
	}
	return out
}

// snippetKerningWaivers 登记 A-014 在摘要列上的延伸：摘要是 `text-xs` 400 字重的单行文字，含拉丁字母时 Chromium 做字偶距、Viva 不做，
// Viva 整行偏宽（「A talk about CRDTs, sync e…」差 1.55px：Ts −0.97、DT −0.58）；摘要被省略号截断，Viva 因此少放一个字符
// （「container-type、@containe…」对「@contain…」）。可见文字是「前缀 + …」，所以 Subject 取摘要的开头 10 个字符。
// 宽度差与墨迹位移都带上限，同一处更大的偏差照常判定。
func snippetKerningWaivers() []parity.Waiver {
	fix, err := parity.LoadLibraryFixture()
	if err != nil {
		panic(err) // 夹具是编译进来的常量，解码失败是程序错误
	}
	heads := map[string]bool{}
	for _, variant := range fix.Variants {
		for _, e := range variant.Entries {
			if hasLatinLetter(e.Snippet) {
				heads[headRunes(e.Snippet, 10)] = true
			}
		}
	}
	var out []parity.Waiver
	for _, head := range slices.Sorted(maps.Keys(heads)) {
		out = append(out,
			parity.Waiver{
				Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: head, MaxDelta: 3,
				Note: "字偶距：Chromium 做、Viva 不做，含拉丁字母的摘要整行偏宽（≤ 字偶对之和）",
			},
			parity.Waiver{
				Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: head, MaxDelta: 2,
				Note: "同上：整行宽度差累积成墨迹剖面的最佳对齐位移（只管横向）",
			},
			parity.Waiver{
				Ledger: "A-014", Layer: parity.LayerL1, Kind: "ellipsis-cut", Subject: head,
				Note: "同上：Viva 的字宽没有字偶距，省略号前少放一两个字符",
			})
	}
	return out
}

// sourceEllipsisWaivers 登记 A-014 在来源列上的另一处后果（B-046 (7)(a)）：大字号档里居中的来源单元格放不下「YouTube」，
// 文字被省略号截断。400 字重下 Chromium 做字偶距、Viva 不做（Yo、Tu 两个字偶对共 −0.1886em，13.5 px 时 2.55 px），
// 不做字偶距的「YouTu…」比单元格的内容宽多，所以渲染层截成「YouTu…」、Viva 截成「YouT…」。
// 只放过渲染层截成「YouTu…」这一种结果（Subject 是报告里带引号的写法），别的截断点照常判定。
func sourceEllipsisWaivers() []parity.Waiver {
	return []parity.Waiver{{
		Ledger: "A-014", Layer: parity.LayerL1, Kind: "ellipsis-cut", Subject: "「YouTu…」",
		Note: "字偶距：Chromium 做、Viva 不做，来源列的「YouTube」在 Viva 里宽出一截，省略号前少放一个字符（「YouTu…」对「YouT…」）",
	}}
}

// headRunes 取 s 的前 n 个字符。
func headRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		runes = runes[:n]
	}
	return string(runes)
}
