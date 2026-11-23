package library

import (
	"strconv"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/reader"
	"guizhi-mygo-shell/viva/screens/themed"
)

// 主题阅读（AI 阅读，台账 B-064）在知识库详情里的接线：阅读区在「原文」与「AI 阅读」两个版本间切换。
// 页面本身由阅读岛显示（themed.Screen.IslandView 给出 ViewSnapshot），状态、对话框与任务状态是 themed 包的原生界面。
// Themed 为 nil（没接线）时本文件的入口都是空操作，工具栏的「AI 阅读」钮退回成「尚未迁移」的说明。

// themedOn 报告这一帧阅读区显示的是 AI 阅读页（Bind 之后才有意义）。
func (s *Screen) themedOn() bool { return s.Themed != nil && s.Themed.Active() }

// bindThemed 在内容面板每一帧画阅读区之前调用：把「正在读什么」交给主题阅读。
// 编辑原文时 AI 阅读退出显示；回收站里的条目只读（页面可看可导出，不能生成、调整、删除）。
func (s *Screen) bindThemed(item services.LibraryItem, caps ItemCaps, active ReadingTab) {
	if s.Themed == nil {
		return
	}
	text, _ := readingSourceText(item, sourceKindOfActive(active))
	s.Themed.Bind(themed.Subject{
		ItemID: item.ID, Title: item.Title, Tab: string(active),
		Enabled:  s.detail.panel.themedAvailable(active),
		ReadOnly: caps.Trashed,
		Editing:  s.editing(item, active),
		// 原文改了（更新时间与正文长度变了）要重读：过期标记随之变化。
		ContentKey: strconv.FormatInt(item.UpdatedAt, 10) + "/" + strconv.Itoa(len(text)),
	})
}

// themedArea 画 AI 阅读的阅读区：提示条与主体归 themed.Screen.Area；页面文档就绪时它回调这里，把页面放进阅读岛。
// 没接宿主时（对照与单测）island 为 nil，Area 画页面的无头占位。
func (s *Screen) themedArea(c ui.C, item services.LibraryItem, caps ItemCaps, active ReadingTab) {
	col := ui.Column(c.Key("reader")).FillWidth().Grow(1).MinHeight(0)
	host := s.readerHost
	var island func(c ui.C, v reader.View)
	if host != nil {
		req := s.readerRequest(c, item, caps, active)
		failure := host.Failure(req)
		island = func(c ui.C, v reader.View) {
			if v.Kind == reader.ViewNone {
				return // 这一帧没有页面可显示：不调 Frame，宿主在帧末收起岛
			}
			paint := func(d *ui.DrawContext) {
				if failure == nil {
					host.Frame(d.Canvas, d.Rect, req)
				}
			}
			ui.Column(c).FillWidth().Grow(1).MinHeight(0).Draw(paint).Children(func() {
				if failure != nil && s.Look.LoadErrorState(c.Key("reader-error"), failure.Error()) {
					host.Retry()
				}
			})
		}
	}
	col.Children(func() { s.Themed.Area(c, island) })
}

// ShowThemedTask 是处理中心「主题页任务」里的「查看主题页」的知识库一侧：把条目的阅读标签记成任务对应的那份内容
// （正文或讨论总结）并标记它在看 AI 阅读；随后调用方选中条目（渲染层 patchContentReadingMemory + selectItem）。
// 真正切到 AI 阅读由 themed.Screen 在条目显示出来时完成。
func (s *Screen) ShowThemedTask(itemID, sourceKind string) {
	tab := TabBody
	if sourceKind == sourceSummary {
		tab = TabSummary
	}
	s.patchReading(itemID, ReadingPatch{Tab: tab, ThemedBySource: map[string]bool{sourceKind: true}})
}
