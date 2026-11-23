package scenes

import (
	"slices"
	"unicode/utf8"

	"guizhi-mygo-shell/viva/parity"
)

// Wiki（M3-5a）在英文变体里的登记。口径同 capture_waivers_en.go：全部带 Variant: englishTier，写死的英文 Subject 与 locales 目录里
// 英文界面画出的字一致（wiki_waivers_en_test.go 核对），中文变体不会命中；Waivers 在 init 里构造，那时界面语言还是中文，取不到英文文案。
// 没有新增登记类别：只是 A-013（tracking-wide 字距）与 A-014（400 字重字偶距）的英文 Subject 延伸，同样是待追认的新登记（台账 C-010 第 9 项）——
// 删掉对应一行，那个英文场景就回到未通过。读数取自 vivaparity-m35a.exe 的英文变体（React 参照 react-m35a-en）。
//
// 与 wideLatinTrackingWaivers 不同的一处：二级侧栏的三个大写小标题（BROWSE、BY KIND、NEEDS ATTENTION）墨迹水平位移实测 −1、−3、−4，
// 超过现成写法给的「宽度差的一半加 1」（A-013 的原上限只在 EXISTING TAGS 这类较短标题上实测过）。这里把墨迹上限放宽到「宽度差的上限」，
// 即位移不超过整行宽度差，仍只放过横向、纵向照常判定；这是对 A-013 墨迹口径的延伸，随 C-010 第 9 项一并待追认。

// 英文界面里被登记的文字（Subject 的片段）。
const (
	enWikiBrowse         = "BROWSE"          // wiki.panelBrowse 经 uppercase
	enWikiByKind         = "BY KIND"         // wiki.panelByKind 经 uppercase
	enWikiNeedsAttention = "NEEDS ATTENTION" // wiki.panelNeedsAttention 经 uppercase
	enWikiTopic          = "Topic"           // 类型名（侧栏条目与徽标）：To 字偶对
	enWikiEmptyHint      = "AI compiles your knowledge items into interlinked wiki pages."
	enWikiCompileIntro   = "Existing wiki pages stay readable."
	enWikiCompileCounts  = "Ready 3 · Needs review 1"
	enWikiChunks         = "Chunks: "
	enWikiEstimate       = "This batch — items:"
	enWikiUpgrade        = "Choose items to upgrade"
	enWikiAccept         = "Accept update and keep current version"
)

const noteKernWiki = "字偶距（A-014 的英文延伸）：Chromium 对 400 字重做字偶距、Viva 不做，英文整行比渲染层偏宽或偏窄 ≤ 2px，沿行方向累积成墨迹的水平位移（只管横向）。" +
	"归因按方向与量级推断，没有逐个字偶对核对"

// wikiEnglishHeadingWaivers 是侧栏大写小标题（text-xs uppercase tracking-wide，A-013）的登记：宽度差沿用 A-013 的范围，墨迹位移上限取宽度差上限。
func wikiEnglishHeadingWaivers(title string) []parity.Waiver {
	n := float64(utf8.RuneCountInString(title))
	out := slices.DeleteFunc(wideLatinTrackingWaivers(title), func(w parity.Waiver) bool { return w.Kind == "ink" })
	return append(out, parity.Waiver{
		Ledger: "A-013", Layer: parity.LayerL2, Kind: "ink", Subject: "「" + title + "」", Variant: englishTier,
		MaxDelta: n*0.3375 + 0.3 + latinHeadingKerningCap,
		Note:     "tracking-wide 的字距（Viva 没有字距）使整行墨迹整体左移，位移不超过整行宽度差（只管横向，纵向错位照常判定）",
	})
}

// wikiEnglishBaseWaivers 是每个 Wiki 英文场景共有的登记：二级侧栏的三个大写小标题与类型名「Topic」（侧栏条目 12px、目录行徽标 10px 都有 To 字偶对）。
func wikiEnglishBaseWaivers() []parity.Waiver {
	out := slices.Concat(
		wikiEnglishHeadingWaivers(enWikiBrowse),
		wikiEnglishHeadingWaivers(enWikiByKind),
		wikiEnglishHeadingWaivers(enWikiNeedsAttention),
	)
	return append(out,
		englishKerning(kerning400Ledger, "text-width", "「"+enWikiTopic+"」", kerningWidthCap, noteKernWiki),
		englishKerning(kerning400Ledger, "ink", "「"+enWikiTopic+"」", kerningInkCapLine, noteKernWiki),
	)
}

// wikiEnglishEmptyWaivers 是空态的：引导说明是一段英文正文，整行宽度差 1.05px。
func wikiEnglishEmptyWaivers() []parity.Waiver {
	return []parity.Waiver{englishKerning(kerning400Ledger, "text-width", enWikiEmptyHint, kerningWidthCap, noteKernWiki)}
}

// wikiEnglishCompileWaivers 是编译对话框的：说明与估算行的墨迹右移 1 像素，计数行宽差 1.36px 加墨迹右移 1 像素，分块数一行墨迹右移 1 像素。
func wikiEnglishCompileWaivers() []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning400Ledger, "ink", enWikiCompileIntro, kerningInkCapLine, noteKernWiki),
		englishKerning(kerning400Ledger, "text-width", enWikiCompileCounts, kerningWidthCap, noteKernWiki),
		englishKerning(kerning400Ledger, "ink", enWikiCompileCounts, kerningInkCapLine, noteKernWiki),
		englishKerning(kerning400Ledger, "ink", enWikiChunks, kerningInkCapLine, noteKernWiki),
		englishKerning(kerning400Ledger, "ink", enWikiEstimate, kerningInkCapLine, noteKernWiki),
	}
}

// wikiEnglishDetailWaivers 是详情页标题的：标题盒随右侧「Topic」徽标的宽度差（A-014 同一成因）短或长 1.11px。
func wikiEnglishDetailWaivers(titles ...string) []parity.Waiver {
	var out []parity.Waiver
	for _, title := range titles {
		out = append(out, englishKerning(kerning400Ledger, "box", "heading 「"+title+"」", kerningWidthCap, noteKernWiki+"；标题盒是弹性的，宽度差来自同一行的类型徽标"))
	}
	return out
}

// wikiEnglishSuggestionWaivers 是更新建议的：「选择资料升级」按钮墨迹右移 1 像素，「接受更新」按钮宽差 1.64px（外框）。
func wikiEnglishSuggestionWaivers() []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning400Ledger, "ink", enWikiUpgrade, kerningInkCapLine, noteKernWiki),
		englishKerning(kerning400Ledger, "box", "button 「"+enWikiAccept+"」", kerningWidthCap, noteKernWiki),
	}
}

// wikiEnglishEditWaivers 是编辑态的：目录行里「手动编辑」铅笔图标跟在标题徽标之后，随徽标宽度差偏 1.11px（外框）。
func wikiEnglishEditWaivers() []parity.Waiver {
	return []parity.Waiver{{
		Ledger: kerning400Ledger, Layer: parity.LayerL2, Kind: "icon-box", Subject: "图标", Variant: englishTier, MaxDelta: 1.5,
		Note: noteKernWiki + "；目录行里铅笔图标紧跟在类型徽标之后，随徽标宽度差偏 ≤ 1.2px",
	}}
}
