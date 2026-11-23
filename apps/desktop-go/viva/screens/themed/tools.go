package themed

// 阅读页工具（ThemedReadingPageTools.tsx）：「阅读页设置」菜单有哪些项、各项是否可用、点击后做什么，
// 以及参考资料 / 页面图片 / 版本说明三个资料面板的内容。

import (
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// ToolsPanel 是资料面板。
type ToolsPanel int

const (
	PanelNone ToolsPanel = iota
	PanelReferences
	PanelAssets
	PanelNotes
)

// RefsState 是参考资料面板的数据。Error 非空时界面要渲染成可重试的错误态（再次 OpenPanel），不能当作「没有资料」。
type RefsState struct {
	Items   []services.ThemedReference
	Loading bool
	Error   string
}

// ToolID 是菜单项标识。
type ToolID int

const (
	ToolAdjust ToolID = iota
	ToolRegenerate
	ToolReferences
	ToolAssets
	ToolExport
	ToolExportText
	ToolExportStatic
	ToolRestore
	ToolNotes
	ToolDelete
)

// ToolItem 是菜单里的一项。
type ToolItem struct {
	ID          ToolID
	Label       string
	Disabled    bool
	Destructive bool
}

// ToolsMenu 返回菜单项，顺序与渲染层一致；没有页面时没有菜单（nil）。
// 调整 / 重新生成 / 恢复 / 删除受 Disabled(readOnly) 约束（生成中、操作中、回收站里不可用），导出只在操作中禁用。
func (v ReadingView) ToolsMenu(readOnly bool) []ToolItem {
	page := v.Page()
	if page == nil {
		return nil
	}
	disabled := v.Disabled(readOnly)
	var items []ToolItem
	if !page.Legacy() {
		items = append(items, ToolItem{ID: ToolAdjust, Label: locales.ThemedReadingReaderAdjust(), Disabled: disabled})
	}
	items = append(items,
		ToolItem{ID: ToolRegenerate, Label: locales.ThemedReadingReaderRegenerate(), Disabled: disabled},
		ToolItem{ID: ToolReferences, Label: locales.ThemedReadingReaderReferences()},
		ToolItem{ID: ToolAssets, Label: locales.ThemedReadingAssets()},
		ToolItem{ID: ToolExport, Label: locales.ThemedReadingExport(), Disabled: v.Busy},
		ToolItem{ID: ToolExportText, Label: locales.ThemedReadingExportText(), Disabled: v.Busy},
	)
	if page.FormatVersion == 3 {
		items = append(items, ToolItem{ID: ToolExportStatic, Label: locales.ThemedReadingReaderExportStatic(), Disabled: v.Busy})
	}
	if v.Result != nil && v.Result.Previous {
		items = append(items, ToolItem{ID: ToolRestore, Label: locales.ThemedReadingReaderRestore(), Disabled: disabled})
	}
	if len(page.Warnings) > 0 || (v.Result != nil && v.Result.Stale) {
		items = append(items, ToolItem{ID: ToolNotes, Label: locales.ThemedReadingReaderPageNotes()})
	}
	items = append(items, ToolItem{ID: ToolDelete, Label: locales.ThemedReadingReaderDelete(), Disabled: disabled, Destructive: true})
	return items
}

// Invoke 执行一个菜单项；不在菜单里或被禁用的项什么也不做。返回是否执行。
func (s *ReadingStore) Invoke(id ToolID, readOnly bool) bool {
	allowed := false
	for _, it := range s.View().ToolsMenu(readOnly) {
		if it.ID == id {
			allowed = !it.Disabled
		}
	}
	if !allowed {
		return false
	}
	switch id {
	case ToolAdjust:
		s.OpenSetup(SetupAdjust)
	case ToolRegenerate:
		s.OpenSetup(SetupNew)
	case ToolReferences:
		s.OpenPanel(PanelReferences)
	case ToolAssets:
		s.OpenPanel(PanelAssets)
	case ToolNotes:
		s.OpenPanel(PanelNotes)
	case ToolExport:
		s.ExportHTML(false, false)
	case ToolExportText:
		s.ExportHTML(true, false)
	case ToolExportStatic:
		s.ExportHTML(false, true)
	case ToolRestore:
		s.Restore()
	case ToolDelete:
		s.RequestDelete()
	}
	return true
}

// OpenPanel 打开资料面板；参考资料面板同时读取资料（失败显示可重试的错误态，再次 OpenPanel 重试）。没有页面时不打开。
func (s *ReadingStore) OpenPanel(p ToolsPanel) {
	s.mu.Lock()
	if s.view.Page() == nil {
		s.mu.Unlock()
		return
	}
	s.view.Panel = p
	s.mu.Unlock()
	s.wake()
	if p == PanelReferences {
		s.loadRefs()
	}
}

// ClosePanel 关闭资料面板。
func (s *ReadingStore) ClosePanel() {
	s.mu.Lock()
	s.view.Panel = PanelNone
	s.refsRev++
	s.mu.Unlock()
	s.wake()
}

// loadRefs 读取参考资料：只认最后一次，切换内容后作废。
func (s *ReadingStore) loadRefs() {
	s.mu.Lock()
	s.refsRev++
	rev, gen, req := s.refsRev, s.selectGen, s.request()
	s.view.Refs.Loading, s.view.Refs.Error = true, ""
	s.mu.Unlock()
	s.wake()

	items, err := s.svc.References(s.ctx(), req)

	s.mu.Lock()
	if rev != s.refsRev || gen != s.selectGen {
		s.mu.Unlock()
		return
	}
	s.view.Refs.Loading = false
	if err != nil {
		s.view.Refs.Error = err.Error()
	} else {
		s.view.Refs.Items = items
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("themedReading.references", err)
	}
	s.wake()
}

// ReferenceDate 是参考资料的「获取于」日期：渲染层的 new Date(capturedAt).toLocaleDateString()，
// 跟系统区域走（zh 为 2026/11/14，en 为 11/14/2026，月日不补零），时区取 loc（nil 为本机）。
func ReferenceDate(r services.ThemedReference, loc *time.Location, locale kit.DateLocale) string {
	return locales.ThemedReadingReaderCapturedAt(kit.FormatDate(r.CapturedAt, loc, locale))
}

// AssetRow 是页面图片面板里的一行。
type AssetRow struct {
	Asset services.ThemedAsset
	// Caption 是图片用途（空则用替代文字）；Kind 是「AI 生成插画 / 原有图片」。
	Caption, Kind string
	// ThumbFile 非空时显示缩略图（本地图片文件名）；否则显示占位。
	ThumbFile string
	// Redo 为 true 时出现「重做这张图 / 重试原图」按钮（旧格式页面没有；原图只在失败时重试）；RedoLabel 是按钮文字；RedoDisabled 是不可点。
	Redo, RedoDisabled bool
	RedoLabel          string
}

// AssetRows 返回页面图片面板的行；没有页面时为 nil。
func (v ReadingView) AssetRows(readOnly bool) []AssetRow {
	page := v.Page()
	if page == nil {
		return nil
	}
	disabled := v.Disabled(readOnly)
	rows := make([]AssetRow, 0, len(page.Assets))
	for _, a := range page.Assets {
		row := AssetRow{Asset: a, Caption: a.Purpose}
		if row.Caption == "" {
			row.Caption = a.Alt
		}
		row.Kind = locales.ThemedReadingOriginalAsset()
		if a.Generated() {
			row.Kind = locales.ThemedReadingGeneratedAsset()
		}
		if a.Ready() {
			row.ThumbFile = a.FileName
		}
		if !page.Legacy() && (a.Generated() || a.Status != services.ThemedAssetReady) {
			row.Redo, row.RedoDisabled = true, disabled
			row.RedoLabel = locales.ThemedReadingRetryOriginal()
			if a.Generated() {
				row.RedoLabel = locales.ThemedReadingRedraw()
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// NotesModel 是版本说明面板的内容：原文已更新的说明，和生成时留下的提示。
type NotesModel struct {
	StaleDetail string
	Warnings    []string
}

// Notes 返回版本说明；没有页面时为零值。
func (v ReadingView) Notes() NotesModel {
	page := v.Page()
	if page == nil {
		return NotesModel{}
	}
	m := NotesModel{Warnings: page.Warnings}
	if v.Result != nil && v.Result.Stale {
		m.StaleDetail = locales.ThemedReadingReaderStaleDetail()
	}
	return m
}
