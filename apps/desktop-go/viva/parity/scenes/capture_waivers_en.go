package scenes

import "guizhi-mygo-shell/viva/parity"

// 快速采集对话框（M3-3）在英文变体里的登记。口径同 library_bulk_waivers.go / library_detail_waivers_en.go：
// 全部带 Variant: englishTier，写死的英文 Subject 与 locales 目录里英文界面画出的字一致（capture_waivers_en_test.go 核对），中文变体不会命中。
// 读数取自 vivaparity-m33.exe 的英文变体（React 参照 react-m33-en，capture.dlg.empty 与 capture.dlg.mixed 两个场景）。
// 没有新增登记类别：只是 A-013 / A-014 的英文 Subject 延伸，同样是待追认的新登记（台账 C-010）——删掉对应一行，那个英文场景就回到未通过。
//
// 英文界面里多行文本框的占位符会折成两行：行距差（Viva 21px、渲染层 20px，台账 B-062 同一成因）没有登记，而是在 kit.TextArea 里用
// PlaceholderLeading 校准掉了（占位符由 kit 自己画）。输入内容折成多行时的同一差距**没有**校准、也没有登记，是真实缺口，见 dialog_pitch_test.go。

// 英文界面里被登记的文字（Subject 的片段）。
const (
	// enCaptureTitle 是 capture.title：模态标题（tracking-tight，A-013；沿用 modalTitleWaivers，因为 Waivers 在 init 时构造，
	// 那时界面语言还是中文，取不到英文标题）。
	enCaptureTitle       = "Quick Capture"
	enCaptureNav         = "Capture & Transcription"                                                                                                                 // settings.captureAndTranscription：遮罩背后设置页 rail 里的分区名
	enAutoSaveDesc       = "Automatically save changes while editing; when off, save with the button or Ctrl+S"                                                      // settings.autoSaveDesc：遮罩背后设置页的说明
	enCaptureHint        = "Supports text, links, images and audio/video; common video and forum links are parsed automatically, share messages can be pasted as-is" // capture.hint
	enCapturePlaceholder = "Paste text, a link or a share message, or drop text / image / audio / video files here…"                                                 // capture.placeholder：草稿框的占位符（折成两行）
	enCaptureTagsHint    = "Tags (press Enter)"                                                                                                                      // capture.tagsPlaceholder：标签输入框的占位符
)

// noteKern400Capture：快速采集对话框英文变体的 A-014 延伸。
const noteKern400Capture = "字偶距（A-014 的英文延伸）：Chromium 对 400 字重做字偶距、Viva 不做，英文整行比渲染层宽 1.17–1.64px，沿行方向累积成墨迹的水平位移（只管横向）。" +
	"归因按方向（Viva 偏宽）与量级推断，没有逐个字偶对核对"

// captureEnglishWaivers 是快速采集对话框两个英文场景的全部字偶距登记，各场景统一附上（按 Subject 与变体匹配）。
// 读数：「Capture & Transcription」宽差 1.40px（React 148.44、Viva 149.84）；自动保存说明宽差 1.39px（471.94、473.33）；
// 采集说明宽差 1.17px（286.33、287.50）、墨迹右移 1 像素；标签占位符宽差 1.64px（100.24、101.88）、墨迹右移 1 像素；草稿框占位符墨迹右移 1 像素（宽度差在上限内）。
func captureEnglishWaivers() []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning400Ledger, "text-width", "「"+enCaptureNav+"」", kerningWidthCap, noteKern400Capture),
		englishKerning(kerning400Ledger, "text-width", "「"+enAutoSaveDesc+"」", kerningWidthCap, noteKern400Capture),
		englishKerning(kerning400Ledger, "text-width", "「"+enCaptureHint+"」", kerningWidthCap, noteKern400Capture),
		englishKerning(kerning400Ledger, "ink", "「"+enCaptureHint+"」", kerningInkCapLine, noteKern400Capture),
		englishKerning(kerning400Ledger, "ink", "「"+enCapturePlaceholder+"」", kerningInkCapLine, noteKern400Capture),
		englishKerning(kerning400Ledger, "text-width", "「"+enCaptureTagsHint+"」", kerningWidthCap, noteKern400Capture),
		englishKerning(kerning400Ledger, "ink", "「"+enCaptureTagsHint+"」", kerningInkCapLine, noteKern400Capture),
	}
}
