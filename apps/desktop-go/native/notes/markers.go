package notes

import (
	"fmt"
	"regexp"
)

// 笔记正文的存储格式标记：小节标题、元数据块的字段名，以及认这些标记的匹配式。
//
// 这些字符串是写进条目正文的数据格式（packages/shared 的 forum-note、image-note、video-meta、media-summary 与
// ocr-request 里同样写死的中文），与界面语言无关、不能翻译，不是给用户读的界面文案。所以全部集中在本文件，
// 并且本文件是 native/ 里唯一放行「中日韩字面量」守卫的地方（native/nocjk_test.go 的 cjkLiteralExemptFiles）；
// 本包其余文件里出现中文字面量，守卫照旧会拦。改这里的任何一个字符串都等于改存储格式，必须与 TS 一致。

// 论坛条目的小节标题。
const (
	ForumSummaryHeading = "## 讨论总结"
	ForumBodyHeading    = "## 正文"
	ForumRepliesHeading = "## 讨论"
)

// 阅读来源改写（reading-source.ts 的 replaceReadingSource）认的小节标题名（不含「## 」前缀）与全角括号：
// 那里的小节标题匹配式要按 JS 多行模式的语义手写（reading_source.go），不能用上面的 RE2 匹配式。
const (
	forumSummaryTitle = "讨论总结"
	forumBodyTitle    = "正文"
	forumTalkTitle    = "讨论"
	parenOpenFull     = "（"
	parenCloseFull    = "）"
)

// OCRSectionHeading 是图片条目「图中文字」小节的标题（ocr-request.ts 的 OCR_SECTION_HEADING）。
const OCRSectionHeading = "## 图中文字"

// 音视频条目的总结小节标题（media-summary.ts）。
const (
	VideoSummaryHeading = "## 视频总结"
	AudioSummaryHeading = "## 音频总结"
)

// 视频元数据引用块的字段名（video-meta.ts）与全角冒号：字段名后面的冒号半角、全角都认。
const (
	labelPlatform         = "平台"
	labelAuthor           = "作者"
	labelDuration         = "时长"
	labelOriginalTitle    = "原标题"
	labelDescription      = "简介"
	labelTranscriptSource = "文字稿来源"
	colonFull             = "："
)

// 论坛正文的匹配式（forum-note.ts）：小节标题行与逐楼回复的头部、引用行。
var (
	reRepliesHeading = regexp.MustCompile(`^##` + sp + `*讨论(?:（(?:楼主` + sp + `*)?\d+` + sp + `*条(?:` + sp + `*·` + sp + `*原帖共` + sp + `*\d+` + sp + `*条)?）)?$`)
	reSummaryHeading = regexp.MustCompile(`^##` + sp + `*讨论总结$`)
	reBodyHeading    = regexp.MustCompile(`^##` + sp + `*正文$`)
	reReplyHeadBold  = regexp.MustCompile(`^\*\*(\d+)` + sp + `*楼` + sp + `*·` + sp + `*(.+?)\*\*$`)
	reReplyHeadH3    = regexp.MustCompile(`^###` + sp + `*(\d+)` + sp + `*楼` + sp + `*·` + sp + `*(.+)$`)
	reReplyToLine    = regexp.MustCompile(`^>` + sp + `*回复` + sp + `*@(.+?)(?:（(\d+)` + sp + `*楼）)?：(.*)$`)
	reBoldFloor      = regexp.MustCompile(`(?m)^\*\*\d+` + sp + `*楼`)
)

// floorLabel 是楼层的「N 楼」写法（讨论过滤时与楼层号一起参与匹配）。
func floorLabel(floor int) string { return fmt.Sprintf("%d 楼", floor) }
