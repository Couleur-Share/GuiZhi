package library

import (
	"unicode/utf16"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 内容面板的版面（渲染层 ContentPanel 的 return）：
//
//	flex min-h-0 flex-1 flex-col（article-content）
//	  relative flex min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-panel
//	    工具栏（标签页 + 工具）、复核提醒、查找条（绝对定位）、阅读区
//
// 阅读区由岛里的页面渲染（detailreader.go；没接宿主时留白）；工具栏上需要 AI、转写、总结、讨论刷新的动作画出来但未接线（台账 B-060）。

// catalogAutoOpenMin 是渲染层 ForumFloorCatalog 的 CATALOG_AUTO_OPEN_MIN：讨论条数不少于它时，楼层目录默认展开。
const catalogAutoOpenMin = 20

// defaultCatalogOpen 是没有阅读记忆时楼层目录的开关。
func defaultCatalogOpen(replyCount int) bool { return replyCount >= catalogAutoOpenMin }

// editorPreview 读设置里「编辑器默认预览」（每次现读）；没接线时取默认值（开）。
func (s *Screen) editorPreview() bool {
	if s.EditorPreview == nil {
		return true
	}
	return s.EditorPreview()
}

// contentPanel 是详情下半部的内容面板。
func (s *Screen) contentPanel(c ui.C, v View, item services.LibraryItem, caps ItemCaps) {
	l := s.Look
	t := l.T
	p := &s.detail.panel
	s.syncPanel(item, caps)
	active := p.activeTab(caps)
	s.persistPanel(item, active)
	showFind := p.showFindBar(active, caps)
	p.canFind = showFind
	if !showFind {
		p.findOpen = false // 渲染层 effect：查找入口没了就关掉查找条
	}
	live := s.readerLive(item, caps, active)
	ui.Column(c.Key("content")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c.Key("panel")).FillWidth().Grow(1).MinHeight(0).Clip().
			Draw(func(d *ui.DrawContext) {
				p.panelW, p.panelRect = d.Rect.Width, d.Rect
				l.PaintSurface(d, kit.Surface{Bg: t.UiPanelBg, Shadow: t.UiPanelShadow})
			}).
			Children(func() {
				s.panelToolbar(c.Key("toolbar"), item, caps, active, showFind)
				s.reviewNotice(c, v, item, caps.Trashed)
				if p.findOpen && showFind {
					s.findBar(c.Key("find"), active, live)
				} else {
					p.findFocused = false
				}
				// min-h-0 flex-1（data-article-reader）：阅读区。
				s.readerArea(c, item, caps, active)
			})
	})
}

// panelToolbar 是工具栏：flex min-h-12 shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-y border-border/50 px-6 py-1.5。
func (s *Screen) panelToolbar(c ui.C, item services.LibraryItem, caps ItemCaps, active ReadingTab, showFind bool) {
	l := s.Look
	t := l.T
	pad := l.Space(1.5) + 1 // py-1.5 加上 border-y 的 1px
	// 渲染层是 flex-wrap 容器：min-h-12 比一行高时（align-content 缺省 stretch）这一行被拉到内容盒那么高、里面的项居中；
	// 折成多行时内容已经高过 min-h，不再有余量。「整块在内容盒里垂直居中」对这两种情形都成立。
	ui.Column(c).FillWidth().Shrink(0).MinHeight(l.Rem(3)).Justify(ui.Center).PadSides(pad, l.Space(6), pad, l.Space(6)).
		Draw(func(d *ui.DrawContext) {
			border := t.Border.WithAlpha(0.5)
			l.PaintEdge(d, kit.EdgeTop, 1, border)
			l.PaintEdge(d, kit.EdgeBottom, 1, border)
		}).
		Children(func() {
			ui.Flow(c.Key("line")).FillWidth().Items(ui.Center).Justify(ui.End).Gap(l.Space(3)).LineGap(l.Space(1)).Children(func() {
				s.tabGroup(c.Key("tabs"), item, caps, active)
				s.toolGroup(c.Key("tools"), item, caps, active, showFind)
			})
		})
}

// tabGroup 是标签页那一组：flex min-w-0 max-w-full flex-[1_0_auto] items-center gap-1 overflow-x-auto。
// 横向滚动（标签多到放不下）没有做：超出部分被裁掉（台账 B-059 的备注）。
func (s *Screen) tabGroup(c ui.C, item services.LibraryItem, caps ItemCaps, active ReadingTab) {
	l := s.Look
	p := &s.detail.panel
	wechatPreview := caps.IsWechat && p.snapshotView
	ui.Row(c).Items(ui.Center).Gap(l.Space(1)).Grow(1).Shrink(0).MinWidth(0).MaxWidthPercent(100).Clip().Children(func() {
		if caps.IsWechat {
			label := locales.ArticleAskViewSnapshot()
			if p.snapshotView {
				label = locales.LibraryBackToReading()
			}
			if s.panelTab(c.Key("snapshot"), label, gfx.Icon{}, p.snapshotView, "") {
				p.snapshotView = !p.snapshotView
				p.findActive, p.findCount = 0, 0
			}
		}
		if wechatPreview {
			// 公众号网页快照的工具条挂载点（flex min-w-0 items-center）：内容随 M2-5，这里是空的。
			ui.Row(c.Key("snapshot-toolbar")).Items(ui.Center).MinWidth(0)
			return
		}
		tab := func(key string, tab ReadingTab, label string, icon gfx.Icon, suffix string) {
			if s.panelTab(c.Key(key), label, icon, active == tab, suffix) {
				s.changeTab(item, caps, tab)
			}
		}
		if caps.ShowForumSummaryTab {
			tab("summary", TabSummary, locales.LibraryForumSummarySection(), icons.ScrollText, "")
		}
		body := locales.LibraryBodySection()
		if caps.ShowImagesTab {
			body = locales.LibraryCaptionSection() // sections 非空（图片条目）时正文页签叫「文案」
		}
		tab("body", TabBody, body, gfx.Icon{}, "")
		if caps.ShowRepliesTab {
			tab("replies", TabReplies, locales.LibraryForumRepliesSection(), icons.MessagesSquare, "")
		}
		if caps.ShowImagesTab {
			tab("images", TabImages, locales.LibraryImagesSection(), icons.Image, "")
		}
		if caps.ShowRecognizedTab {
			tab("recognized", TabRecognized, locales.LibraryRecognizedSection(), icons.ScanText, "")
		}
		if caps.ShowTranscriptTab {
			suffix := ""
			if caps.Transcript != "" {
				suffix = locales.LibraryTranscriptLength(len(utf16.Encode([]rune(caps.Transcript))))
			}
			tab("transcript", TabTranscript, locales.LibraryTranscript(), icons.AudioLines, suffix)
		}
	})
}

// panelTab 是一枚标签页按钮（渲染层 TabButton）：inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-xs
// font-medium；选中 bg-muted text-foreground，否则 text-muted-foreground hover:bg-accent/60 hover:text-foreground。
// suffix 是文字稿标签后面的字数（text-[10px] opacity-60）。
func (s *Screen) panelTab(c ui.C, label string, icon gfx.Icon, on bool, suffix string) bool {
	l := s.Look
	t := l.T
	box := l.Button(c, kit.ButtonSpec{
		Label: label, Icon: icon, IconSize: l.Rem(0.75), Toggle: true, On: on,
		Suffix: suffix, SuffixPx: 10, SuffixFade: 0.6,
	}, kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Space(2.5), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightMedium,
		Fg: t.MutedForeground, FgHover: t.Foreground, FgOn: t.Foreground,
		BgHover: t.Accent.WithAlpha(0.6), BgOn: t.Muted,
		Transition: kit.DurationDefault,
	})
	return box.Clicked()
}

// toolGroup 是工具那一组：ml-auto flex min-w-0 flex-wrap items-center gap-1。
// 主题阅读可用时（themedAvailable）工具栏最前面是读者工具条的挂载点（渲染层 setReaderToolbar 的容器，原文 / AI 阅读 / 目录 / 编辑由阅读区
// 经门户放进来，见 detailreadertoolbar.go）：没有接阅读岛（也没开 ReaderToolbar）时它是空的，但仍占一个弹性子项（相邻的 gap 照算）；
// 这时不摆「显示原文 / Markdown 渲染」（台账 B-059）。
func (s *Screen) toolGroup(c ui.C, item services.LibraryItem, caps ItemCaps, active ReadingTab, showFind bool) {
	l := s.Look
	p := &s.detail.panel
	themed := p.themedAvailable(active)
	ui.Flow(c).Items(ui.Center).Gap(l.Space(1)).LineGap(l.Space(1)).Shrink(1).MinWidth(0).Children(func() {
		if themed {
			if s.readerToolbarOn() {
				s.readerToolbar(c.Key("reader-toolbar"), item, caps, active)
			} else {
				ui.Row(c.Key("reader-toolbar")).Items(ui.Center).MinWidth(0)
			}
		}
		if showFind {
			label := locales.LibraryPanelFindOpen()
			if s.toolButton(c.Key("find"), label, icons.Search, false, false) {
				p.findOpen = true
			}
		}
		if active != TabTranscript {
			if !caps.Trashed && active == TabReplies && caps.RefreshAvailable {
				if s.textToolButton(c.Key("refresh"), locales.LibraryForumDiscussionRefresh(), icons.RefreshCw, false) {
					s.notMigrated(locales.LibraryForumDiscussionRefresh(), "M2-6", "B-060")
				}
			}
			if !caps.Trashed && caps.SummaryAvailable {
				if s.toolButton(c.Key("summary"), caps.SummaryLabel, icons.ScrollText, false, false) {
					s.notMigrated(caps.SummaryLabel, "M2-6", "B-060")
				}
			}
			if !caps.Trashed && !themed {
				label := locales.LibraryRenderMarkdown()
				if p.isPreview {
					label = locales.LibraryShowSource()
				}
				if s.textToolButton(c.Key("preview"), label, gfx.Icon{}, false) {
					p.isPreview = !p.isPreview
				}
			}
			return
		}
		if caps.Transcript != "" {
			label := locales.LibraryTranscriptFormat()
			if s.toolButton(c.Key("format"), label, icons.Sparkles, false, false) {
				s.notMigrated(label, "M2-6", "B-060")
			}
		}
		if caps.CanTranscribe && caps.Transcript != "" {
			if s.detailCanDiarize() {
				label := locales.LibraryTranscribeDiarize()
				if s.toolButton(c.Key("diarize"), label, icons.Users, false, false) {
					s.notMigrated(label, "M2-6", "B-060")
				}
			}
			label := locales.LibraryTranscribeRegenerate()
			if s.toolButton(c.Key("regenerate"), label, icons.RotateCcw, false, false) {
				s.notMigrated(label, "M2-6", "B-060")
			}
		}
	})
}

// detailCanDiarize 是转写路由支持区分说话人（渲染层 useTranscriptActions 的 canDiarize）。
func (s *Screen) detailCanDiarize() bool { return s.Store.View().Detail.CanDiarize }

// toolButton 是工具栏的图标钮（渲染层 ToolButton）：inline-flex h-8 w-8 shrink-0 rounded-lg text-muted-foreground
// hover:bg-accent/60 hover:text-foreground，图标 h-3.5 w-3.5；忙或禁用时整枚淡出（disabled:opacity-50）且不响应。
func (s *Screen) toolButton(c ui.C, label string, icon gfx.Icon, busy, disabled bool) bool {
	l := s.Look
	t := l.T
	spec := kit.ButtonSpec{Name: label, Hint: label, Center: true, Disabled: busy || disabled, Busy: busy, BusySize: l.Rem(0.875)}
	if !busy {
		spec.Icon, spec.IconSize = icon, l.Rem(0.875)
	}
	box := l.Button(c, spec, kit.ButtonStyle{
		Height: l.Rem(2), Width: l.Rem(2), Radius: l.Rem(0.75),
		Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent.WithAlpha(0.6),
		Transition: kit.DurationDefault, Surface: t.UiPanelBg,
	})
	return box.Clicked()
}

// textToolButton 是工具栏的小号文字钮（刷新讨论、显示原文 / Markdown 渲染）：inline-flex h-6 shrink-0 items-center gap-1
// rounded-md border border-border/70 px-2 text-[11px] text-muted-foreground hover:border-border hover:bg-accent/60 hover:text-foreground。
func (s *Screen) textToolButton(c ui.C, label string, icon gfx.Icon, busy bool) bool {
	l := s.Look
	t := l.T
	spec := kit.ButtonSpec{Label: label, Disabled: busy, Busy: busy, BusySize: l.Rem(0.875)}
	if !busy {
		spec.Icon, spec.IconSize = icon, l.Rem(0.875)
	}
	box := l.Button(c, spec, kit.ButtonStyle{
		Height: l.Rem(1.5), PadX: l.Space(2), Radius: l.Rem(0.75) - 2, Gap: l.Space(1), FontPx: 11, LinePx: 16.5,
		Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent.WithAlpha(0.6),
		Border: t.Border.WithAlpha(0.7), BorderHover: t.Border, BorderW: 1,
		Transition: kit.DurationDefault, Surface: t.UiPanelBg,
	})
	return box.Clicked()
}

// findBar 是查找条（渲染层 PanelFindBar，外面一层 absolute right-2 top-11 z-20 flex w-full max-w-[calc(100%-1rem)] justify-end）：
// flex h-8 w-72 max-w-[calc(100%-1rem)] items-center gap-1 rounded-lg border border-border bg-background/95 p-1 shadow-lg。
// 命中数由阅读区的页面回报（p.findCount）；没有接岛时恒为 0：输入非空时显示「无匹配」，上一个 / 下一个禁用。
//
// lifted 为真时（阅读岛在场）条画在更高的一层：原生视图盖住同层与更低层画的东西，所以条改用 ui.Overlay 放在面板的右上角（窗口坐标、取上一帧
// 量到的面板矩形，首帧没有就下一帧再画）；详情在表格视图的详情浮层里时，原生视图在浮层的层上，条要再高一层（kit.BandOverIslandInModal）。
func (s *Screen) findBar(c ui.C, active ReadingTab, lifted bool) {
	l := s.Look
	t := l.T
	p := &s.detail.panel
	filtering := jsTrim(p.findQuery) != ""
	hasMatches := filtering && p.findCount > 0
	placeholder := locales.LibraryPanelFindPlaceholder()
	if active == TabReplies {
		placeholder = locales.LibraryForumRepliesFilterPlaceholder()
	}
	bar := func(row ui.Box) ui.Box {
		return row.Height(l.Rem(2)).Items(ui.Center).Gap(l.Space(1)).
			PadSides(0, l.Space(1)+1, 0, l.Space(1)+1). // p-1 加 border 的 1px；竖向的 24px 子项在 32px 里居中，与 22px 内容盒里溢出居中同位
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.95), Border: t.Border, BorderW: 1, Radius: l.Rem(0.75), Shadows: kit.ShadowLG})
			})
	}
	body := func() {
		ui.Row(c.Key("field")).Grow(1).Basis(0).MinWidth(0).Height(l.Rem(1.5)).Items(ui.Center).Gap(l.Space(1)).
			PadSides(0, l.Space(1.5), 0, l.Space(1.5)).Children(func() {
			l.Icon(c.Key("icon"), icons.Search, l.Rem(0.75), t.MutedForeground.WithAlpha(0.7))
			// text-[11px]：<input> 的内容高是继承来的 1.5 倍行高 16.5px，Viva 的输入框至少 Size+6，取 17。
			// 渲染层的全局规则 input:focus-visible 给文本框 ring-2 ring-primary/50（框外 2 px，直角：<input> 没有圆角）；
			// 底板与输入是两个元素（Box.Draw 画在输入内容之上），环由先声明的底板读后声明的输入的聚焦状态来画（同设置页的十六进制框）。
			var input ui.Box
			var ring float32
			ui.Stack(c.Key("input-wrap")).Grow(1).Basis(0).MinWidth(0).Height(17).Children(func() {
				// 环围着 <input> 的边框盒，它高 16.5（11px 的 1.5 倍行高）；Viva 的输入框取 17，环按 16.5 画、与输入框同一中线（上下各让 0.25），
				// 否则框顶带小数的字号档里环的上下沿差一像素行（落进文字行矩形外扩 1 像素的墨迹区域，台账 G-028）。
				ui.Row(c.Key("ring")).Absolute().Top(0.25).Right(0).Bottom(0.25).Left(0).Draw(func(d *ui.DrawContext) { l.PaintInputRing(d, ring, 0) })
				input = l.Input(c.Key("input"), &p.find, placeholder, placeholder, kit.InputSpec{
					Size: 11, Weight: kit.WeightNormal, Color: t.Foreground, Height: 17, Placeholder: t.MutedForeground.WithAlpha(0.6),
				}).Absolute().Inset(0).AutoFocus()
				ring = l.InputRing(c, ui.ID(c.Key("ring-anim")), input.State().Focused)
			})
			p.findFocused = input.State().Focused
			if input.Changed() {
				p.findQuery = p.find.Text()
				p.findActive = 0
			}
			if p.findQuery != "" {
				if s.inputClear(c.Key("clear")) {
					p.find.SetText("")
					p.findQuery, p.findActive = "", 0
				}
			}
		})
		if filtering {
			text := locales.LibraryPanelFindEmptyShort()
			if hasMatches {
				text = locales.LibraryPanelFindMatchIndex(p.findActive+1, p.findCount)
			}
			l.TextPx(c.Key("count"), text, 10, 15, kit.WeightNormal, t.MutedForeground.WithAlpha(0.8)).Shrink(0)
			if s.findNavButton(c.Key("prev"), locales.LibraryPanelFindPrevMatch(), icons.ChevronUp, !hasMatches) {
				s.findStep(-1)
			}
			if s.findNavButton(c.Key("next"), locales.LibraryPanelFindNextMatch(), icons.ChevronDown, !hasMatches) {
				s.findStep(1)
			}
		}
		if s.findNavButton(c.Key("close"), locales.CommonClose(), icons.X, false) {
			p.findOpen = false
		}
	}
	width := findBarWidth(l, p.panelW)
	if !lifted {
		bar(ui.Row(c).Absolute().Top(l.Space(11)).Right(l.Space(2)).Width(width)).Children(body)
		return
	}
	r := p.panelRect
	if r.Width <= 0 {
		c.Canvas().RequestFrame() // 首帧还没有面板的矩形：下一帧再画
		return
	}
	band := kit.BandOverIsland
	if s.detail.modal.open {
		band = kit.BandOverIslandInModal
	}
	bounds := gfx.Rect{X: r.X + r.Width - l.Space(2) - width, Y: r.Y + l.Space(11), Width: width, Height: l.Rem(2)}
	layer := ui.Overlay(c, ui.OverlaySpec{Band: band, Bounds: bounds})
	if s.detail.modal.open {
		layer = l.WithinModal(&s.detail.modal.state, layer) // 在详情浮层里：焦点陷阱要放它进去
	}
	layer.Children(func() {
		bar(ui.Row(c.Key("bar")).FillWidth()).Children(body)
	})
}

// findBarWidth 是查找条的宽：外层包装是 w-full max-w-[calc(100%-1rem)]、条本身再取 max-w-[calc(100%-1rem)]，
// 所以条宽 = min(18rem, 面板宽 − 2rem)，右缘离面板 0.5rem；面板宽还没量到（首帧，panelW 为 0）时取 18rem。
func findBarWidth(l *kit.Look, panelW float32) float32 {
	width := l.Rem(18)
	if panelW > 0 {
		width = min(width, panelW-l.Rem(2))
	}
	return width
}

// findStep 在命中之间循环移动（渲染层 goPrev / goNext）：没有命中时什么也不做。
func (s *Screen) findStep(delta int) {
	p := &s.detail.panel
	if jsTrim(p.findQuery) == "" || p.findCount <= 0 {
		return
	}
	p.findActive = ((p.findActive+delta)%p.findCount + p.findCount) % p.findCount
}

// inputClear 是输入框里的清除钮：flex h-4 w-4 shrink-0 rounded text-muted-foreground hover:text-foreground，图标 h-3 w-3。
func (s *Screen) inputClear(c ui.C) bool {
	l := s.Look
	t := l.T
	return l.Button(c, kit.ButtonSpec{Icon: icons.X, IconSize: l.Rem(0.75), Center: true, Name: locales.HeaderClearSearch()}, kit.ButtonStyle{
		Height: l.Rem(1), Width: l.Rem(1), Radius: l.Rem(0.25),
		Fg: t.MutedForeground, FgHover: t.Foreground, Transition: kit.DurationDefault,
	}).Clicked()
}

// findNavButton 是查找条上的 24px 图标钮：flex h-6 w-6 shrink-0 rounded-md text-muted-foreground hover:bg-accent/60 hover:text-foreground
// disabled:opacity-40，图标 h-3.5 w-3.5。
func (s *Screen) findNavButton(c ui.C, label string, icon gfx.Icon, disabled bool) bool {
	l := s.Look
	t := l.T
	return l.Button(c, kit.ButtonSpec{Icon: icon, IconSize: l.Rem(0.875), Center: true, Name: label, Disabled: disabled}, kit.ButtonStyle{
		Height: l.Rem(1.5), Width: l.Rem(1.5), Radius: l.Rem(0.75) - 2,
		Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent.WithAlpha(0.6),
		Transition: kit.DurationDefault, DisabledOpacity: 0.4,
	}).Clicked()
}
