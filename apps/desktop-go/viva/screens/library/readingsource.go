package library

import (
	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
)

// 阅读来源（渲染层 packages/shared/utils/reading-source.ts 的 readingSourceText）：主题阅读的「原文」取条目正文的哪一段。
//
//   - 论坛条目：正文或讨论总结两个小节（视频元数据块之内）；
//   - 图片条目：「文案」小节（元数据块之内，图片引用与「图中文字」不算）；
//   - 其余：去掉视频元数据块之后的全文。
//
// 来源只有 "body" 与 "summary" 两种；非论坛条目没有讨论总结（渲染层这里是抛错，调用方不会走到：总结标签只在论坛条目上出现），
// 返回 ok=false。

const (
	sourceBody    = "body"
	sourceSummary = "summary"
)

// readingSourceText 取条目在来源 kind 下的原文。
func readingSourceText(item services.LibraryItem, kind string) (text string, ok bool) {
	if item.ItemType == services.LibraryTypeForum {
		sections := notes.SplitForumSections(item.Content)
		if kind == sourceSummary {
			return sections.Summary, true
		}
		return sections.Body, true
	}
	if kind == sourceSummary {
		return "", false
	}
	body := item.Content
	if stripped, has := notes.VideoMetaBody(item.Content); has {
		body = stripped
	}
	if item.ItemType == services.LibraryTypeImage {
		return notes.SplitImageSections(body).Caption, true
	}
	return body, true
}
