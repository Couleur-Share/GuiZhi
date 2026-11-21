package scenes

import (
	"strings"
	"unicode/utf8"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 文字稿页签（M2-6d）对照里新增的放过项，都是 A-009（渲染层合成层灰度抗锯齿 vs Viva 整窗 LCD）在新场景里的延伸。
//
// 页签字数「N 字」是 `text-[10px] opacity-60`，整段带透明度，依据同 opacityInkWaiver，直接复用。
// 查找条计数「1 / N」：查找条是 `backdrop-blur-sm`，同占位符的 compositedInkWaiver 依据，但计数文字只有 10px 左右、
// 笔画更细，LCD 彩边占墨迹的比例更大：浅色读数 1.224，深色 1.492（2026-11-22，detail-transcript-find 中档 1280×800），
// 超出 compositedInkWaiver 的 0.4 上限，所以单独框一个更宽的区间（仍不超过守卫的 0.65 上限）。
// 查找框里输入的查找词也在这个合成层里：浅色大字号档里「本地优先」的墨迹总量比 1.256–1.259（6 个色相逐个相同），
// 小、中档与深色不超阈值，所以限 `-large-`、Viva 偏多、[0.2, 0.3]（实测向外取整）。该 Subject 与文字稿里被高亮的同一个词同名：
// 高亮的词不在合成层里，落在 [0.2, 0.3] 的才会被放过，位置、字重、颜色、字号照常判定。

// transcriptBodyText 是文字稿正文场景里的文字稿全文（夹具里的 bodyItem），transcriptBodyChars 是它的字数（渲染层页签上的数字）。
func transcriptBodyText() string {
	cfg := libraryDetailFixture()
	return cfg.Details[cfg.Transcripts.BodyItem].Transcript
}

func transcriptBodyChars() int { return utf8.RuneCountInString(transcriptBodyText()) }

// transcriptCountWaivers 返回页签字数与查找条计数两条放过项的 Subject 已按当前语言排好的登记。
// body 是正文场景里文字稿的全文，query 是查找词；计数取 UTF-16 之外的字符数（夹具里全是 BMP 字符，与渲染层的 length 一致）。
func transcriptCountWaivers(body, query string) (tabCount, findCount parity.Waiver) {
	// 查找框里的查找词在下面单独登记（findInputWaiver）。
	n := utf8.RuneCountInString(body)
	tabCount = opacityInkWaiver("「" + locales.LibraryTranscriptLength(n) + "」")
	findCount = parity.Waiver{
		Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass",
		Subject:  "「" + locales.LibraryPanelFindMatchIndex(1, strings.Count(body, query)) + "」",
		MinDelta: 0.1, MaxDelta: 0.55, Dir: 1,
		Note: "查找条（backdrop-blur-sm）里的匹配计数在合成层里用灰度抗锯齿，Viva 整窗 LCD，墨迹总量偏大（位置、字重、颜色、字号均一致）",
	}
	return tabCount, findCount
}

// findInputWaiver 登记查找框里输入的查找词（`backdrop-blur-sm` 查找条里的 <input>）的墨迹总量：依据见文件头。
func findInputWaiver(query string) parity.Waiver {
	return parity.Waiver{
		Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass", Subject: "「" + query + "」", Variant: "-large-", MinDelta: 0.2, MaxDelta: 0.3, Dir: 1,
		Note: "查找条（backdrop-blur-sm）里输入框的文字在合成层里用灰度抗锯齿，Viva 整窗 LCD，浅色大字号档墨迹总量偏大（位置、字重、颜色、字号均一致）",
	}
}

// findNavIconWaiver 登记查找条里「上一个 / 下一个」两枚 14px 箭头在 800×600 里的墨迹总量（台账 A-007 的延伸）。
// 窄窗口里文字稿滚到命中处，查找条正压在一行文字上：渲染层的 `backdrop-blur-sm` 把那行字糊成一层淡淡的雾，雾也进了量墨迹的区域
// （React 一侧墨迹量偏多）；Viva 没有模糊原语，查找条改垫一块不透明的页面底色（kit.Surface.Backdrop），背后没有雾。
// 读数（2026-11-22，浅色皇家蓝中档 800×600）：两枚箭头 Viva / React = 0.831、0.789，所以限 `-800x600`、Viva 偏少、
// 偏差 [0.16, 0.22]（实测向外取整）。别的窗口大小、别的偏差、别的图标都不放过。
func findNavIconWaiver() parity.Waiver {
	return parity.Waiver{
		Ledger: "A-007", Layer: parity.LayerL2, Kind: "ink-mass", Subject: "14.00 14.00]", Variant: "-800x600", MinDelta: 0.16, MaxDelta: 0.22, Dir: -1,
		Note: "查找条压在文字上时，渲染层的模糊在背后留一层雾、进了墨迹区域；Viva 垫不透明底色、没有雾，14px 箭头的墨迹总量偏少（外框、位置一致）",
	}
}

// transcriptLatinLead 是 detail-video-transcript 里文字稿那一段（夹具 item-004，「Transformer」夹在汉字里）的开头。
const transcriptLatinLead = "「大家好，今天我们来讲 Transformer"

// transcriptLatinWaivers 登记 A-014 在文字稿正文里的延伸：汉字夹一个拉丁词「Transformer」（400 字重）。Chromium 对拉丁字符做字偶距、
// Viva 不做，这一行比渲染层宽 1.52px（1920×1080 的 text-width 读数，React 677.34、Viva 678.87）；累积成这一行墨迹的水平位移 +1。
// 大字号档（根字号 18px）里行宽再多这一点，折行点差一个汉字：末行宽度差恰好是一个汉字的宽度 18px（React 234、Viva 252，大档 12 个变体逐个相同），
// 所以大档的宽度另框 [17.5, 18.5]，别的字号档宽度差仍只放过字偶距的 ≤2px。位置纵向、字重、颜色照常判定（墨迹只放过横向 1px）。
func transcriptLatinWaivers() []parity.Waiver {
	const note = "字偶距（A-014 的延伸）：Chromium 对拉丁字符做字偶距、Viva 不做，含「Transformer」的这一段比渲染层宽，累积成墨迹的水平位移（只管横向）"
	return []parity.Waiver{
		{Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: transcriptLatinLead, MaxDelta: 2, Note: note},
		{Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: transcriptLatinLead, MaxDelta: 1, Note: note},
		{
			Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: transcriptLatinLead, Variant: "-large-", MinDelta: 17.5, MaxDelta: 18.5,
			Note: note + "；大字号档的折行点因此差一个汉字，末行宽度差恰为 18px",
		},
	}
}
