package scenes

import (
	"slices"
	"unicode/utf8"

	"guizhi-mygo-shell/viva/parity"
)

// M2-3（批量与整理）场景的两类登记：英文变体里的字偶距与墨迹偏差，以及比较器在投影渐变上的背景取样缺口（G-023）。
//
// 英文变体（台账 G-017：每个模块一个静态英文变体，浅色、中档、1280×800）只做中档字号档，所以下面登记一律带 `Variant: "medium"`，
// 不把依据只在这一档取得的范围扩到小、大字号档。写死的英文 Subject 与 locales 目录里英文界面画出来的字一致，
// library_bulk_waivers_test.go 核对；目录改了这里要跟着改。

// englishTier 是英文变体所在的字号档（变体键里的片段）。
const englishTier = "medium"

// 英文界面里被登记的文字（Subject 的片段；带「」的整段文字与台账登记时的读数见各登记的 Note）。
const (
	enResultsLabel  = "Per-item results ("             // library.workflowResults：逐条结果的折叠钮，数字随条数变，只取前缀
	enMenuFavorite  = "Favorite"                       // library.favorite：右键菜单的「收藏」
	enDeleteHint    = "Past answers keep the excerpts" // library.deleteEvidenceHint：确认框正文，只取开头
	enScopeTrash    = "Trash"                          // library.scopeTrash：侧栏「回收站」
	enDeleteForever = "Delete forever"                 // library.deleteForever：确认框标题
	enExistingTags  = "EXISTING TAGS"                  // library.tagsExisting：`uppercase` 之后画出来的小标题
	enSuggestedTags = "AI TAG SUGGESTIONS"             // library.aiTagsSuggest：同上
	enRetryFailed   = "Retry failed only"              // library.workflowRetryFailed
	enDismiss       = "Dismiss"                        // library.workflowDismiss
	enBulkMove      = "Move to collection"             // library.bulkMoveTitle：批量条上的钮
	enBulkAddTag    = "Add tag to selection"           // library.bulkAddTag
	enBulkFavorite  = "Favorite selection"             // library.bulkFavorite
)

// 字偶距登记所属的台账条目：A-014（Chromium 对 400 字重做字偶距、Viva 不做）与 A-026（Chromium 对 500 / 600 字重不做、Viva 做）。
const (
	kerning400Ledger = "A-014"
	kerning600Ledger = "A-026"
)

// 字偶距登记的大小上限（向外取整；实测读数见各登记的 Note 与台账）。
const (
	kerningWidthCap   = 2.0 // 单段文字的字偶距宽度差（实测最大 1.88 px）
	kerningInkCap     = 2.0 // 同一原因造成的墨迹水平位移（实测最大 2 px）
	kerningInkCapLine = 1.0 // 较短的单行文字或整段正文的墨迹水平位移（实测 1 px）
	kerningBoxMax     = 1.5 // 右对齐钮组里各钮宽度差累积出的外框偏移（实测最大 1.30 px）
)

// englishBulkWaivers 是 M2-3 场景在英文变体里的全部登记，各场景统一附上（按 Subject 与变体匹配，中文变体不会命中）。
func englishBulkWaivers() []parity.Waiver {
	var out []parity.Waiver
	// A-014 的英文延伸（400 字重，Viva 偏宽）：Chromium 对 400 字重做字偶距、Viva 不做，含 Pe、Fa 这类字偶对的英文整行比渲染层宽。
	const k400 = "字偶距（A-014 的英文延伸）：Chromium 对 400 字重做字偶距、Viva 不做，Viva 的英文整行偏宽，沿行方向累积成墨迹的水平位移"
	out = append(out,
		englishKerning(kerning400Ledger, "text-width", "「"+enResultsLabel, kerningWidthCap, k400),
		englishKerning(kerning400Ledger, "ink", "「"+enResultsLabel, kerningInkCap, k400),
		englishKerning(kerning400Ledger, "ink", "「"+enMenuFavorite+"」", kerningInkCapLine, k400),
		englishKerning(kerning400Ledger, "ink", "「"+enDeleteHint, kerningInkCapLine, k400),
	)
	// 同一原因在右对齐的批量条上的后果：条上每个钮的宽度都偏宽 0.10–0.64 px，右缘固定，越靠左的钮累积的偏移越大。
	const kBar = "字偶距（A-014 的英文延伸）：右对齐的钮组里每个钮因英文字偶距各宽出 0.10–0.64 px，右缘固定，左边的钮累积出最多 1.3 px 的外框偏移（钮内文字的宽度差在阈值之内）"
	for _, label := range []string{enBulkMove, enBulkAddTag, enBulkFavorite} {
		out = append(out, englishKerning(kerning400Ledger, "box", "button 「"+label+"」", kerningBoxMax, kBar))
	}
	// A-026 的英文延伸（500 / 600 字重，Viva 偏窄）：Viva 对 500 / 600 字重做字偶距、Chromium 不做，含 Tr、De 这类字偶对的文字整行偏窄。
	const k600 = "字偶距（A-026 的英文延伸）：Viva 对 500 / 600 字重做字偶距、Chromium 不做，含 Tr、ev 这类字偶对的英文整行偏窄"
	out = append(out,
		englishKerning(kerning600Ledger, "text-width", "「"+enScopeTrash+"」", kerningWidthCap, k600),
		englishKerning(kerning600Ledger, "ink", "「"+enScopeTrash+"」", kerningInkCap, k600),
		englishKerning(kerning600Ledger, "text-width", "「"+enDeleteForever+"」", kerningWidthCap, k600),
	)
	// 标签浮层的两个小标题：`uppercase tracking-wide`（A-013）加字偶距（500 字重，A-026 同类）。
	out = append(out, wideLatinTrackingWaivers(enExistingTags)...)
	out = append(out, wideLatinTrackingWaivers(enSuggestedTags)...)
	// A-009 的英文延伸：批量进行中禁用的两枚钮（disabled:opacity-50）。
	out = append(out, englishOpacityInkWaiver("「"+enRetryFailed+"」"), englishOpacityInkWaiver("「"+enDismiss+"」"))
	return out
}

// englishKerning 登记一处英文字偶距造成的偏差：kind 是 text-width（宽度差）、ink（墨迹水平位移，纵向不放过）或 box（钮组里累积的外框偏移）。
func englishKerning(ledger, kind, subject string, maxDelta float64, note string) parity.Waiver {
	return parity.Waiver{Ledger: ledger, Layer: parity.LayerL2, Kind: kind, Subject: subject, Variant: englishTier, MaxDelta: maxDelta, Note: note}
}

// englishOpacityInkWaiver 是 opacityInkWaiver 的英文版：同一原因（整组带透明度的文字在合成层里用灰度抗锯齿），只在英文变体所在的字号档放过。
func englishOpacityInkWaiver(subject string) parity.Waiver {
	w := opacityInkWaiver(subject)
	w.Variant = englishTier
	return w
}

// wideLatinTrackingWaivers 登记 A-013 在英文大写小标题（tracking-wide，0.025em）上的延伸：字距之外再加字偶距（latinHeadingKerningCap，
// 500 字重，A-026 同类）。中档字号档 text-xs 是 12px，每字 0.3px；宽度差框在 [字符数×0.2625−0.3, 字符数×0.3375+0.3+字偶距上限]。
// 实测（`vivaparity-m23h.exe`，英文变体）：「EXISTING TAGS」差 5.71px（字距应有 3.9、多出 1.81）、「AI TAG SUGGESTIONS」差 7.21px（应有 5.4、多出 1.81），
// 两处多出的数相同，都是 TA 字偶对；墨迹水平位移均为 −1。
func wideLatinTrackingWaivers(title string) []parity.Waiver {
	return wideTracking(title, latinHeadingKerningCap, englishTier,
		"tracking-wide 的字距（Viva 没有字距）加字偶距（Viva 对 500 字重做、Chromium 不做，A-026 同类）：英文大写小标题的绘制宽度窄 字符数 × 0.025em 再多 ≤ 2px（盒宽已补齐，后面的元素位置一致）")
}

// wideTracking 是 tracking-wide 小标题字距登记的共同写法：kerningCap 是字距之外允许多出的宽度差，variant 非空时只在该字号档放过。
func wideTracking(title string, kerningCap float64, variant, note string) []parity.Waiver {
	n := float64(utf8.RuneCountInString(title))
	subject := "「" + title + "」"
	return []parity.Waiver{
		{
			Ledger: "A-013", Layer: parity.LayerL2, Kind: "text-width", Subject: subject, Variant: variant,
			MinDelta: max(n*0.2625-0.3, 0.01), MaxDelta: n*0.3375 + 0.3 + kerningCap,
			Note: note,
		},
		{
			Ledger: "A-013", Layer: parity.LayerL2, Kind: "ink", Subject: subject, Variant: variant, MaxDelta: n*0.3375/2 + 1,
			Note: "同上：字距使墨迹整体位移最多宽度差的一半（只管横向，纵向错位照常判定）",
		},
	}
}

// shadowRingWaivers 登记 G-023（比较器的缺口，不是 Viva 的偏差）：墨迹量取「区域外圈的众数」当背景，区域落在浮层投影的渐变上时，
// 众数对 ±1 灰阶不稳定——Viva 与 React 的投影尾巴差一个灰阶，两边选出的背景色不同，墨迹总量比出现几十个百分点的假偏差。
// `table-tag-picker` 在 800×600 里有两处：标签浮层的投影落在表格最后一行的标签块「待读」上（比 1.340，外圈众数在 Viva 里变成灰色的上缘），
// 以及压在吸附操作列底下的类型格「文档」（比 1.156，背景 254 对 255）。用同一个背景色量：「待读」0.961，「文档」0.969 与 1.009，都在 [0.85, 1.15] 内，
// 两侧画面逐像素只差投影尾巴那一个灰阶；L3 与 L1 都通过。只放过这两段文字、800×600、墨迹总量偏多的方向，并按实测大小框住；
// 比较器的取样口径要改（比如渐变背景改取中位数）由用户追认，追认之前按这里登记放过。
func shadowRingWaivers() []parity.Waiver {
	const note = "比较器的背景取样缺口（G-023）：投影渐变上外圈众数不稳定，Viva 与 React 选出不同的背景色；用同一个背景色量时墨迹总量比在 [0.85, 1.15] 内"
	return []parity.Waiver{
		{Ledger: "G-023", Layer: parity.LayerL2, Kind: "ink-mass", Subject: "「待读」", Variant: "800x600", MinDelta: 0.30, MaxDelta: 0.38, Dir: 1, Note: note},
		{Ledger: "G-023", Layer: parity.LayerL2, Kind: "ink-mass", Subject: "「文档」", Variant: "800x600", MinDelta: 0.10, MaxDelta: 0.20, Dir: 1, Note: note},
	}
}

// withEnglishBulk 在 waivers 之外附上英文变体的登记，返回新切片（waivers 的底层数组被多个场景共用，不能原地追加）。
func withEnglishBulk(waivers []parity.Waiver) []parity.Waiver {
	return slices.Concat(waivers, englishBulkWaivers())
}
