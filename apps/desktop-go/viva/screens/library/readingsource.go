package library

import (
	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
)

// 阅读来源（渲染层 packages/shared/utils/reading-source.ts）：主题阅读与编辑原文的「原文」取条目正文的哪一段。
// 取文与改写都在 native/notes（ReadingSourceText、ReplaceReadingSource，与 TS 逐字节对拍），这里只做条目类型到正文形态的换算。
//
//   - 论坛条目：正文或讨论总结两个小节（视频元数据块之内）；
//   - 图片条目：「文案」小节（元数据块之内，图片引用与「图中文字」不算）；
//   - 其余：去掉视频元数据块之后的全文。
//
// 来源只有 "body" 与 "summary" 两种；非论坛条目没有讨论总结（渲染层这里是抛错，调用方不会走到：总结标签只在论坛条目上出现），
// 取文返回 ok=false。

const (
	sourceBody    = notes.SourceBody
	sourceSummary = notes.SourceSummary
)

// shapeOf 是条目类型对应的正文形态。
func shapeOf(itemType string) notes.Shape {
	switch itemType {
	case services.LibraryTypeForum:
		return notes.ShapeForum
	case services.LibraryTypeImage:
		return notes.ShapeImage
	}
	return notes.ShapePlain
}

// readingSourceText 取条目在来源 kind 下的原文。
func readingSourceText(item services.LibraryItem, kind string) (text string, ok bool) {
	text, err := notes.ReadingSourceText(item.Content, shapeOf(item.ItemType), kind)
	return text, err == nil
}
