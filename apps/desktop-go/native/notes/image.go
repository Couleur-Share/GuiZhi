package notes

// 图片条目正文的分段（packages/shared/utils/image-note.ts）：文案 / 图片 / 图中文字。
// 三者存在同一份 Markdown 里（全文检索与语义索引都指望它），详情页据此拆成标签页分开看。

import (
	"regexp"
	"strings"
)

// OCRSectionHeading 是「图中文字」小节的标题（ocr-request.ts 的 OCR_SECTION_HEADING）。
const OCRSectionHeading = "## 图中文字"

var (
	// reImageOnly 对应「整段只由图片引用构成（可能连着几张）」。
	reImageOnly   = regexp.MustCompile(`^(?:!\[[^\]]*\]\([^)]+\)` + sp + `*)+$`)
	reBlankBlocks = regexp.MustCompile(`\n{2,}`)
)

// ImageSections 是图片条目正文按小节切出的两段。
type ImageSections struct {
	// Caption 是文案：元数据引用块、正文文案、状态注记（图片引用单独展示，不在其中）。
	Caption string
	// Recognized 是「图中文字」小节（含小节标题）；未识别时为空。
	Recognized string
}

// SplitImageSections 切出文案与图中文字（splitImageNoteSections）。
func SplitImageSections(content string) ImageSections {
	idx := strings.Index(content, OCRSectionHeading)
	head := content
	if idx >= 0 {
		head = content[:idx]
	}
	var kept []string
	for _, block := range reBlankBlocks.Split(head, -1) {
		if b := jsTrim(block); b != "" && !reImageOnly.MatchString(b) {
			kept = append(kept, b)
		}
	}
	out := ImageSections{Caption: strings.Join(kept, "\n\n")}
	if idx >= 0 {
		out.Recognized = jsTrim(content[idx:])
	}
	return out
}
