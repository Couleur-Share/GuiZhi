package library

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// readerToolbarOn 报告工具栏里画不画读者工具条：接了阅读岛（生产）或对照场景要求单独比较这一组控件时画。
func (s *Screen) readerToolbarOn() bool { return s.readerHost != nil || s.ReaderToolbar }

// readerButtonStyle 是渲染层 readerButton 的样式：inline-flex h-8 shrink-0 items-center rounded-md px-2 text-xs text-muted-foreground
// hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary；没有 transition-*，状态直接切换。
func (s *Screen) readerButtonStyle(height float32) kit.ButtonStyle {
	l := s.Look
	t := l.T
	return kit.ButtonStyle{
		Height: height, PadX: l.Space(2), Radius: l.Rem(0.75) - 2, Text: kit.TextXS,
		Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent,
		Ring: t.Primary, Transition: kit.DurationNone,
	}
}

// themeButtonStyle 是渲染层 themeButton 的样式：inline-flex min-h-7 items-center justify-center gap-1 rounded-lg border border-border px-2.5 py-1
// text-xs hover:bg-accent disabled:pointer-events-none disabled:opacity-50；没有 transition-*，状态直接切换。
// 高度是 min-h-7（28px）：一行 text-xs 加 py-1 与边框不到 28px，所以恒为 28。
func (s *Screen) themeButtonStyle() kit.ButtonStyle {
	l := s.Look
	t := l.T
	return kit.ButtonStyle{
		Height: l.Rem(1.75), PadX: l.Space(2.5), Radius: l.Rem(0.75), Gap: l.Space(1), Text: kit.TextXS,
		Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Accent,
		Transition: kit.DurationNone, Surface: t.UiPanelBg,
	}
}

// editActions 是编辑态下读者工具条的内容（渲染层 mode.editing 的分支）：「正在编辑原文」与「完成编辑」；保存期间钮禁用、文字换成「正在保存…」。
func (s *Screen) editActions(c ui.C, item services.LibraryItem, active ReadingTab, kind string) {
	l := s.Look
	t := l.T
	saving := s.detail.edit.saveKey == draftKey(item.ID, kind)
	// px-2 text-xs text-muted-foreground
	ui.Row(c.Key("editing")).Items(ui.Center).PadSides(0, l.Space(2), 0, l.Space(2)).Children(func() {
		l.Text(c.Key("editing-text"), locales.ThemedReadingReaderEditing(), kit.TextXS, kit.WeightNormal, t.MutedForeground)
	})
	label := locales.ThemedReadingReaderFinishEditing()
	if saving {
		label = locales.ThemedReadingReaderSaving()
	}
	if l.Button(c.Key("finish-edit"), kit.ButtonSpec{Label: label, Disabled: saving, Center: true}, s.themeButtonStyle()).Clicked() {
		s.finishEdit(item, active)
	}
}

// readerToolbar 是工具栏最前面的读者工具条（渲染层 ReconstructionReader 的 actions，经门户放进 setReaderToolbar 的容器）：
//
//	flex shrink-0 items-center gap-1.5
//	  分段控件（flex gap-0.5 rounded-lg border border-border/60 bg-muted/40 p-0.5）：原文 / AI 阅读（!h-7，选中的 bg-background font-medium shadow-sm）
//	  分隔线（mx-1 h-4 w-px bg-border/70）
//	  目录（只在原文版本里；展开时 bg-muted）、编辑（回收站里没有）
//
// 编辑态下整组换成「正在编辑原文」与「完成编辑」（editActions）。「AI 阅读」的功能不在本里程碑：点了弹失败提示并写明归属（台账 B-064），不假装成功。
func (s *Screen) readerToolbar(c ui.C, item services.LibraryItem, caps ItemCaps, active ReadingTab) {
	l := s.Look
	t := l.T
	kind := sourceKindOfActive(active)
	s.syncToc(item.ID, kind)
	tocOpen := s.detail.panel.tocOpen
	editing := s.editing(item, active)
	ui.Row(c).Items(ui.Center).Gap(l.Space(1.5)).Shrink(0).Children(func() {
		if editing {
			s.editActions(c, item, active, kind)
			return
		}
		ui.Row(c.Key("version")).Items(ui.Center).Gap(l.Space(0.5)).PadSides(l.Space(0.5)+1, l.Space(0.5)+1, l.Space(0.5)+1, l.Space(0.5)+1).
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.4), Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: l.Rem(0.75)})
			}).
			Children(func() {
				picked := s.readerButtonStyle(l.Rem(1.75))
				picked.Bg, picked.Fg, picked.Weight, picked.Shadows = t.Background, t.Foreground, kit.WeightMedium, kit.ShadowSM
				// 原文是选中的版本（这里没有 AI 阅读的状态：AI 阅读不在本里程碑）。
				l.Button(c.Key("original"), kit.ButtonSpec{Label: locales.ThemedReadingReaderOriginal(), Toggle: true, On: true}, picked)
				if l.Button(c.Key("ai"), kit.ButtonSpec{Label: locales.ThemedReadingReaderAi(), Toggle: true}, s.readerButtonStyle(l.Rem(1.75))).Clicked() {
					s.notMigrated(locales.ThemedReadingReaderAi(), "M3", "B-064")
				}
			})
		ui.Row(c.Key("sep")).Width(1).Height(l.Rem(1)).MarginX(l.Space(1)).Shrink(0).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Border.WithAlpha(0.7)})
		})
		toc := s.readerButtonStyle(l.Rem(2))
		toc.BgOn, toc.FgOn = t.Muted, t.Foreground
		if l.Button(c.Key("toc"), kit.ButtonSpec{Label: locales.LibraryCatalog(), Toggle: true, On: tocOpen}, toc).Clicked() {
			s.setTocOpen(!tocOpen)
		}
		if !caps.Trashed {
			name := locales.ThemedReadingReaderEditOriginal()
			if kind == sourceSummary {
				name = locales.ThemedReadingReaderEditSummary()
			}
			if l.Button(c.Key("edit"), kit.ButtonSpec{Label: locales.ThemedReadingReaderEdit(), Name: name}, s.readerButtonStyle(l.Rem(2))).Clicked() {
				s.startEdit(item, active)
			}
		}
	})
}
