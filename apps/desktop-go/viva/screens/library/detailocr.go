package library

import (
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 图中文字卡片（渲染层 AiOcrCard）：图片条目的 OCR 入口，一枚动作钮；识别结果作为「图中文字」小节写进正文。
// 只对图片条目、且正文里有本地配图引用时出现；识别、写回与提示在 Store（store_ai.go）。

// ocrCard 声明图中文字动作钮。
func (s *Screen) ocrCard(c ui.C, v View, item services.LibraryItem) {
	if item.ItemType != services.LibraryTypeImage || len(notes.ExtractLocalAssetRefs(item.Content, notes.LocalImage)) == 0 {
		return
	}
	l := s.Look
	running := v.Detail.Tools.ItemID == item.ID && v.Detail.Tools.OcrRunning
	label := locales.LibraryOcrGenerate()
	switch {
	case running:
		label = locales.LibraryOcrRunning()
	case strings.Contains(item.Content, notes.OCRSectionHeading):
		label = locales.LibraryOcrRegenerate()
	}
	spec := kit.ButtonSpec{Label: label, Disabled: running, Busy: running, BusySize: l.Rem(0.875)}
	if !running {
		spec.Icon, spec.IconSize = icons.ScanText, l.Rem(0.875)
	}
	if box := l.Button(c, spec, s.actionChipStyle()); box.Clicked() {
		s.Store.RecognizeImages(s.Ctx)
	}
}
