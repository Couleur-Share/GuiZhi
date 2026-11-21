package scenes

import (
	"slices"
	"strconv"

	"guizhi-mygo-shell/viva/parity"
)

// M2-4（详情外壳与正文）场景在英文变体里的登记。英文变体的口径同台账 G-017（用户 lang=limited）：浅色、中档、1280×800 的静态场景；
// 做法同 library_bulk_waivers.go：全部带 Variant: englishTier，写死的英文 Subject 与 locales 目录里英文界面画出的字一致
// （library_detail_waivers_en_test.go 核对）。读数取自 vivaparity-m24r.exe 的英文变体（React 参照 react-m24-en，22 个场景）。
// 全部是待追认的新登记（台账 C-010）：删掉对应一行，那个英文场景就回到未通过。

// 英文界面里被登记的文字（Subject 的片段）。
const (
	enTabTranscript      = "Transcript"                           // library.transcript：视频条目的页签
	enTabTranscriptLen   = "49 chars"                             // library.transcriptLength：页签上的字数（夹具里文字稿 49 字）
	enTabRecognized      = "Text in image"                        // library.recognizedSection：图片条目的页签
	enImportIncomplete   = "Captured content needs review"        // article.reader.importIncomplete：待复核条的标题
	enRevisionAdopt      = "Use this version's text in the draft" // library.sourceRevisionAdopt：采用来源版本正文的钮
	enRevisionsHint      = "Newly captured content is kept here"  // library.sourceRevisionsHint 的开头
	enConflictNotice     = "This content was changed elsewhere."  // library.draftConflict 的第一句
	enConflictKeepMine   = "Keep my changes and save"             // library.draftConflictKeepMine：冲突条上的钮
	enExitFocusShort     = "Exit focus"                           // library.exitFocusReadingShort：专注阅读的悬浮钮
	enGenerateTranscript = "Generate transcript"                  // library.transcribeGenerate：文字稿空态里的主色钮（M2-6d）
)

// 两条字偶距登记的说明（A-014 与 A-026 的英文延伸，成因同 library_bulk_waivers.go）。
const (
	noteKern600 = "字偶距（A-026 的英文延伸）：Viva 对 500 / 600 字重做字偶距、Chromium 不做，含 Tr、Te 这类字偶对的英文整行偏窄；" +
		"盒宽随文字变窄，同一行里居中或接在后面的文字沿行方向累积成墨迹的水平位移（只管横向，纵向错位照常判定）。归因按量级推断，没有逐个字偶对核对"
	noteKern400 = "字偶距（A-014 的英文延伸）：Chromium 对 400 字重做字偶距、Viva 不做，英文整段比渲染层宽，沿行方向累积成墨迹的水平位移（只管横向）。归因按量级推断，没有逐个字偶对核对"
)

// enTranscriptTabWaivers：视频条目的「Transcript 49 chars」页签（500 字重，Viva 偏窄）。
// 读数：整个钮宽差 1.74px、「Transcript」绘制宽度差 1.68px（React 56.53、Viva 54.85），「Transcript」与「49 chars」墨迹都左移 2 像素。
func enTranscriptTabWaivers() []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning600Ledger, "text-width", "「"+enTabTranscript+"」", kerningWidthCap, noteKern600),
		englishKerning(kerning600Ledger, "box", "button 「"+enTabTranscript+" ", kerningWidthCap, noteKern600),
		englishKerning(kerning600Ledger, "ink", "「"+enTabTranscript+"」", kerningInkCap, noteKern600),
		englishKerning(kerning600Ledger, "ink", "「"+enTabTranscriptLen+"」", kerningInkCap, noteKern600),
	}
}

// enTranscriptEmptyWaivers：文字稿空态（M2-6d）里的两处 500 字重文字——只有页签（没有字数时钮的外框以「Transcript」收尾，
// 不是 enTranscriptTabWaivers 里「Transcript 」打头的那一条）与主色钮「Generate transcript」，Viva 偏窄。
// 读数：页签钮宽差 1.68px、「Transcript」文字宽差 1.68px（同 enTranscriptTabWaivers）；「Generate transcript」钮宽差 1.38px、文字宽差 1.38px
// （React 126.84、Viva 125.47）。
func enTranscriptEmptyWaivers() []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning600Ledger, "box", "button 「"+enTabTranscript+"」", kerningWidthCap, noteKern600),
		englishKerning(kerning600Ledger, "text-width", "「"+enGenerateTranscript+"」", kerningWidthCap, noteKern600),
		englishKerning(kerning600Ledger, "box", "button 「"+enGenerateTranscript+"」", kerningWidthCap, noteKern600),
	}
}

// enTranscriptCountWaivers：文字稿正文场景（M2-6d）页签上的字数「N chars」（500 字重页签里的第二段，墨迹左移 2 像素）。
// 页签本身那几条是 enTranscriptTabWaivers，只有字数的 Subject 随文字稿长度而不同。
func enTranscriptCountWaivers(n int) []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning600Ledger, "ink", "「"+strconv.Itoa(n)+" chars」", kerningInkCap, noteKern600),
	}
}

// enRecognizedTabWaivers：图片条目的「Text in image」页签（500 字重，Viva 偏窄）。读数：钮宽差与文字宽度差都是 1.27px（React 74.78、Viva 73.51）。
func enRecognizedTabWaivers() []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning600Ledger, "text-width", "「"+enTabRecognized+"」", kerningWidthCap, noteKern600),
		englishKerning(kerning600Ledger, "box", "button 「"+enTabRecognized+"」", kerningWidthCap, noteKern600),
	}
}

// enImportReviewWaivers：待复核条的标题「Captured content needs review」（500 字重，Viva 偏窄）。读数：宽度差 1.08px（React 175.02、Viva 173.94）。
func enImportReviewWaivers() []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning600Ledger, "text-width", "「"+enImportIncomplete+"」", kerningWidthCap, noteKern600),
	}
}

// enRevisionsWaivers：来源版本折叠区里的两处——说明段落（400 字重，Viva 偏宽，墨迹右移 1 像素，A-014）与「采用」钮
// （500 字重，Viva 偏窄：钮宽差与文字宽度差都是 1.55px，墨迹左移 1 像素，A-026）。
func enRevisionsWaivers() []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning400Ledger, "ink", "「"+enRevisionsHint, kerningInkCapLine, noteKern400),
		englishKerning(kerning600Ledger, "text-width", "「"+enRevisionAdopt+"」", kerningWidthCap, noteKern600),
		englishKerning(kerning600Ledger, "box", "button 「"+enRevisionAdopt+"」", kerningWidthCap, noteKern600),
		englishKerning(kerning600Ledger, "ink", "「"+enRevisionAdopt+"」", kerningInkCapLine, noteKern600),
	}
}

// enConflictWaivers：冲突条里的两处——提示句（400 字重，Viva 偏宽，墨迹右移 1 像素，A-014）与「保留我的修改并保存」钮（500 字重，墨迹左移 1 像素，A-026）。
func enConflictWaivers() []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning400Ledger, "ink", "「"+enConflictNotice, kerningInkCapLine, noteKern400),
		englishKerning(kerning600Ledger, "ink", "「"+enConflictKeepMine+"」", kerningInkCapLine, noteKern600),
	}
}

// enFocusWaivers：专注阅读右下角的悬浮钮「Exit focus」（backdrop-blur-sm 的合成层，A-009 的延伸，同 compositedInkWaiver）。
// 读数：墨迹总量比 1.240（中文「退出专注」1.165–1.174；英文字形笔画更细，LCD 彩边占的比例更大，仍在 compositedInkWaiver 的 [0.1, 0.4] 之内）。
func enFocusWaivers() []parity.Waiver {
	return []parity.Waiver{englishCompositedInkWaiver("「" + enExitFocusShort + "」")}
}

// englishCompositedInkWaiver 是 compositedInkWaiver 的英文版：同一原因，只在英文变体所在的字号档放过。
func englishCompositedInkWaiver(subject string) parity.Waiver {
	w := compositedInkWaiver(subject)
	w.Variant = englishTier
	return w
}

// englishDetailWaivers 是上面各组的合集（测试用；场景各自只带自己那一组）。
func englishDetailWaivers() []parity.Waiver {
	return slices.Concat(
		enTranscriptTabWaivers(), enRecognizedTabWaivers(), enImportReviewWaivers(),
		enRevisionsWaivers(), enConflictWaivers(), enFocusWaivers(),
		enTranscriptEmptyWaivers(), enTranscriptCountWaivers(transcriptBodyChars()),
	)
}
