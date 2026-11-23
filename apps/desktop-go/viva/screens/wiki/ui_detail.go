package wiki

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// detailUI 是页面详情自己的界面状态：编辑器、「更多」菜单、删除确认、页内目录、正文解析缓存与标题锚点。
type detailUI struct {
	more        moreMenu
	confirm     kit.ConfirmState
	confirmOpen bool

	draft     ui.Field // 编辑框
	editing   bool     // 上一帧是否在编辑：由不在变成在的那一帧把草稿灌进编辑框
	editFocus bool     // 编辑框是否有焦点（边框高亮）

	pageID  string // 上一帧显示的页面：换页时回到顶部
	tocOpen bool   // 页内目录是否展开（组件内状态，换页不重置）

	suggestion suggestionUI

	md      mdCache
	anchors map[string]gfx.Rect // 二、三级标题的锚点名 → 上一帧画出的矩形（窗口坐标）
	colTop  float32             // 内容列上沿（窗口坐标）：标题相对它的位置就是滚动偏移
}

// unmount 在详情离开画面时调用（切走、选中被清空）：收起菜单与确认框。
func (d *detailUI) unmount() {
	d.more.close()
	d.confirmOpen = false
}

// detailMaxWidth 是正文列的最大宽度（max-w-3xl）。
const detailMaxWidth = 48

// pageDetail 是右栏（渲染层 WikiPageDetail）：没选页、读取中、读取失败，或一页的完整内容。
func (s *Screen) pageDetail(c ui.C, v View) {
	l := s.Look
	t := l.T
	d := &s.detail
	c = c.Key("detail")
	switch {
	case v.SelectedPageID == "":
		d.unmount()
		ui.Row(c).Grow(1).MinWidth(0).FillHeight().Justify(ui.Center).Items(ui.Center).PadSides(0, l.Space(8), 0, l.Space(8)).Children(func() {
			l.Text(c.Key("none"), locales.WikiNoSelection(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped())
		})
	case v.Detail == nil && v.PageError != "":
		d.unmount()
		id := v.SelectedPageID
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Justify(ui.Center).Items(ui.Center).Children(func() {
			if l.LoadErrorState(c.Key("error"), v.PageError) {
				s.spawn(func() { s.Store.SelectPage(id) })
			}
		})
	case v.Detail == nil:
		d.unmount()
		ui.Row(c).Grow(1).MinWidth(0).FillHeight().Justify(ui.Center).Items(ui.Center).Children(func() {
			l.Spinner(c.Key("spin"), l.Rem(1), t.MutedForeground)
		})
	default:
		s.pageBody(c, v)
	}
	s.deleteConfirm(c, v)
}

// pageBody 是一页的内容：flex h-full min-h-0 flex-1 flex-col overflow-y-auto，里面 mx-auto w-full max-w-3xl px-6 py-5。
func (s *Screen) pageBody(c ui.C, v View) {
	l := s.Look
	d := &s.detail
	page := v.Detail.Page
	s.syncDraft(v)
	// 外面套一层列：右栏在两栏的行里，纵向的滚动区要放在定高的列里才会被夹住（内容超出视口时用 Grow(1) + MinHeight(0) 收住）。
	ui.Column(c.Key("pane")).Grow(1).MinWidth(0).MinHeight(0).FillHeight().Children(func() {
		l.ScrollAreaView(c.Key("scroll"), [4]float32{}, func(view kit.ScrollView) {
			if d.pageID != v.SelectedPageID {
				d.pageID = v.SelectedPageID
				d.anchors = map[string]gfx.Rect{}
				view.State.ScrollTo(0) // 切页回到顶部
			}
			ui.Column(c.Key("outer")).FillWidth().Items(ui.Center).Children(func() {
				ui.Column(c.Key("inner")).FillWidth().MaxWidth(l.Rem(detailMaxWidth)).
					PadSides(l.Space(5), l.Space(6), l.Space(5), l.Space(6)).
					Draw(func(dc *ui.DrawContext) { d.colTop = dc.Rect.Y }).
					Children(func() {
						rendered := RenderedBody(page.Body, page.Title)
						hasToc := !v.Editing && ShowToc(ExtractToc(rendered))
						// 正文（.prose）的第一个块的上外边距会与它前一个兄弟的 mb-4 折叠（取较大者）：正文之前的最后一个兄弟不留下外边距，
						// 由 wikiMarkdown 的 lead 参数一并折叠（编辑态后面是编辑框，没有折叠，各块照常 mb-4）。
						backlinks := len(v.Detail.Backlinks) > 0
						metaLast := !v.Editing && !backlinks && !hasToc
						s.pageHeader(c, v, page)
						s.pageMeta(c, v, page, metaLast)
						if backlinks {
							ui.Row(c.Key("backlinks")).FillWidth().MarginBottom(l.Space(4) * s.keepBottom(v.Editing || hasToc)).Children(func() {
								s.relatedChips(c.Key("backlinks-chips"), locales.WikiBacklinks(), icons.Link2, backlinkEntries(v.Detail.Backlinks), func(id string) {
									s.spawn(func() { s.Store.SelectPage(id) })
								})
							})
						}
						if v.Editing {
							s.editor(c, v)
						} else {
							s.pageToc(c, rendered, view)
							s.wikiMarkdown(c, rendered, l.Space(4), s.followAfterProse(v, page))
						}
						s.updateSuggestion(c, page)
						s.sourcesSection(c, v)
					})
			})
		}).FillWidth().Grow(1).MinHeight(0)
	})
}

// pageHeader 是标题行：mb-1.5 flex items-start gap-2，标题 text-xl font-semibold leading-tight，后面是类型徽标。
func (s *Screen) pageHeader(c ui.C, v View, page services.WikiPage) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("title-row")).FillWidth().Items(ui.Start).Gap(l.Space(2)).MarginBottom(l.Space(1.5)).Children(func() {
		l.Heading(c.Key("title"), 1, page.Title, kit.TextStyle{Size: 1.25, Line: 1.5625}, kit.WeightSemibold, t.Foreground, kit.Wrapped()).
			Grow(1).Shrink(1).MinWidth(0)
		s.kindBadge(c, page.Kind)
	})
}

// pageMeta 是出处行：生成或手动编辑的时间、手动编辑标记，行尾是「编辑」与「更多」。mb-4 text-xs text-muted-foreground。
func (s *Screen) pageMeta(c ui.C, v View, page services.WikiPage, last bool) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("meta")).FillWidth().Items(ui.Center).Gap(l.Space(2)).MarginBottom(l.Space(4) * s.keepBottom(!last)).Children(func() {
		text := ""
		if page.ManualEditedAt != nil {
			text = locales.WikiManualEditedAt(kit.FormatItemTime(*page.ManualEditedAt, s.nowTime(), s.loc(), s.Locale))
		} else {
			model := page.Model
			if model == "" {
				model = "AI"
			}
			text = locales.WikiGeneratedAt(model, kit.FormatItemTime(page.GeneratedAt, s.nowTime(), s.loc(), s.Locale))
		}
		l.Text(c.Key("time"), text, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).Shrink(1).MinWidth(0)
		if page.ManualEditedAt != nil {
			s.manualChip(c)
		}
		ui.Spacer(c.Key("auto")).Grow(1)
		if !v.Editing {
			s.editButton(c)
		}
		s.moreButton(c, v)
	})
}

// manualChip 是「手动编辑」标记：inline-flex gap-1 rounded-md border border-primary/40 bg-primary/10 px-1.5 py-0.5 text-[11px] text-primary，
// 悬停提示说明后续编译不会覆盖这一页。
func (s *Screen) manualChip(c ui.C) {
	l := s.Look
	t := l.T
	c = c.Key("manual")
	id := ui.ID(c)
	hint := locales.WikiManualEditedHint()
	ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(1)).
		PadSides(l.Space(0.5)+1, l.Space(1.5)+1, l.Space(0.5)+1, l.Space(1.5)+1).
		Draw(func(dc *ui.DrawContext) {
			l.PaintSurface(dc, kit.Surface{Bg: t.Primary.WithAlpha(0.1), Border: t.Primary.WithAlpha(0.4), BorderW: 1, Radius: s.radiusMD()})
			l.HintGroup(dc.Canvas, id, dc.Rect, locales.WikiManualEdited(), hint)
		}).Children(func() {
		l.Icon(c.Key("icon"), icons.Pencil, l.Rem(0.75), t.Primary)
		l.TextPx(c.Key("text"), locales.WikiManualEdited(), 11, px11Line, kit.WeightNormal, t.Primary)
	})
}

// editButton 是「编辑」：inline-flex h-7 gap-1 rounded-md border border-border px-2 text-[11px] text-foreground hover:bg-accent。
func (s *Screen) editButton(c ui.C) {
	l := s.Look
	t := l.T
	b := l.Button(c.Key("edit"), kit.ButtonSpec{Label: locales.WikiEditPage(), Icon: icons.Pencil, IconSize: l.Rem(0.75)}, kit.ButtonStyle{
		Height: l.Rem(1.75), PadX: l.Space(2), Radius: s.radiusMD(), Gap: l.Space(1), FontPx: 11, LinePx: px11Line, Weight: kit.WeightNormal,
		Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Accent, Transition: kit.DurationDefault,
	})
	if b.Clicked() {
		s.Store.BeginEdit()
	}
}

// relatedEntry 是一枚相关页面小块的内容。
type relatedEntry struct{ id, title string }

func backlinkEntries(in []services.WikiCatalogEntry) []relatedEntry {
	out := make([]relatedEntry, len(in))
	for i, e := range in {
		out[i] = relatedEntry{e.ID, e.Title}
	}
	return out
}

// relatedChips 是一排相关页面（RelatedChips）：flex flex-wrap items-center gap-1.5，行首是图标加说明（text-[11px] muted），后面是各页的小块。
// 没有条目时什么也不画。
func (s *Screen) relatedChips(c ui.C, label string, icon gfx.Icon, entries []relatedEntry, onOpen func(id string)) {
	if len(entries) == 0 {
		return
	}
	l := s.Look
	t := l.T
	ui.Flow(c).FillWidth().Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
		ui.Row(c.Key("label")).Shrink(0).Items(ui.Center).Gap(l.Space(1)).Children(func() {
			l.Icon(c.Key("icon"), icon, l.Rem(0.875), t.MutedForeground)
			l.TextPx(c.Key("text"), label, 11, px11Line, kit.WeightNormal, t.MutedForeground)
		})
		for _, e := range entries {
			if s.chipButton(c.Key("chip-"+e.id), e.title) {
				onOpen(e.id)
			}
		}
	})
}

// sourcesSection 是正文之后的来源：有待复核来源时先一句提示（role=status），再是来源条目的小块；点击跳到知识库里的那一条。
func (s *Screen) sourcesSection(c ui.C, v View) {
	l := s.Look
	t := l.T
	sources := v.Detail.Sources
	for _, src := range sources {
		if src.NeedsReview() {
			// my-3 text-sm text-muted-foreground：role=status 的一句提示。
			// 下面一定还有来源块（mt-6，24px）：它的上外边距与这里的 my-3 下外边距折叠，取较大者，所以这里不再留下外边距。
			ui.Row(c.Key("needs-review")).FillWidth().MarginTop(l.Space(3)).
				Draw(func(d *ui.DrawContext) { d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{}) }).
				Children(func() {
					l.Text(c.Key("text"), locales.WikiSourcesNeedReview(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
				})
			break
		}
	}
	if len(sources) == 0 {
		return
	}
	entries := make([]relatedEntry, len(sources))
	for i, src := range sources {
		entries[i] = relatedEntry{src.ItemID, src.Title}
	}
	// mt-6 border-t border-border/60 pt-4
	ui.Row(c.Key("sources")).FillWidth().MarginTop(l.Space(6)).PadSides(l.Space(4)+1, 0, 0, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			s.relatedChips(c.Key("sources-chips"), locales.WikiSources(), icons.BookOpen, entries, func(id string) { s.spawn(func() { s.Store.OpenSource(id) }) })
		})
}

// pageToc 是页内目录：标题多起来（至少 3 节）才出现，默认收起。mb-4 rounded-lg border border-border/60 bg-background/40。
func (s *Screen) pageToc(c ui.C, body string, view kit.ScrollView) {
	toc := ExtractToc(body)
	if !ShowToc(toc) {
		return
	}
	l := s.Look
	t := l.T
	d := &s.detail
	c = c.Key("toc")
	// 外框有 1px 边框（占布局）：里面的按钮与列表在框内缩进 1px。mb-4 不在这里留：正文第一块的上外边距与它折叠（见 pageBody）。
	ui.Column(c).FillWidth().Pad(1).
		Draw(func(dc *ui.DrawContext) {
			l.PaintSurface(dc, kit.Surface{Bg: t.Background.WithAlpha(0.4), Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: s.radiusLG()})
		}).Children(func() {
		label := locales.WikiToc(len(toc))
		id := ui.ID(c.Key("head"))
		head := ui.Row(c.Key("head-row")).FillWidth().Items(ui.Center).Gap(l.Space(1.5)).
			PadSides(l.Space(1.5), l.Space(3), l.Space(1.5), l.Space(3))
		head, hover := s.hitButton(c, id, head, label, "", false)
		fg := s.ghostFg(hover)
		open := d.tocOpen
		head.Draw(func(dc *ui.DrawContext) {
			if dc.State.FocusVisible {
				l.PaintRing(dc, 2, t.Primary.WithAlpha(0.5), s.radiusLG())
			}
		}).Children(func() {
			l.Icon(c.Key("icon"), icons.ListTree, l.Rem(0.875), fg).Shrink(0)
			l.TextPx(c.Key("text"), label, 11, px11Line, kit.WeightNormal, fg)
			ui.Spacer(c.Key("auto")).Grow(1)
			l.RotatingChevron(c.Key("chevron"), ui.ID(c.Key("chevron")), l.Rem(0.875), open, fg, kit.DurationQuick)
		})
		if head.Clicked() {
			d.tocOpen = !d.tocOpen
		}
		if !open {
			return
		}
		ui.Column(c.Key("entries")).FillWidth().Gap(l.Space(0.5)).PadSides(0, l.Space(3), l.Space(2), l.Space(3)).Children(func() {
			for _, e := range toc {
				if s.tocEntry(c.Key("e-"+e.Slug), e) {
					s.jumpTo(view, e.Slug)
				}
			}
		})
	})
}

// tocEntry 是目录里的一行：block w-full truncate rounded px-1.5 py-0.5 text-left text-xs text-muted-foreground hover:bg-accent/50 hover:text-foreground，
// 三级标题 pl-5。返回这一帧是否被点了。
func (s *Screen) tocEntry(c ui.C, e TocEntry) bool {
	l := s.Look
	t := l.T
	left := l.Space(1.5)
	if e.Level == 3 {
		left = l.Space(5)
	}
	id := ui.ID(c)
	box := ui.Row(c).FillWidth().PadSides(l.Space(0.5), l.Space(1.5), l.Space(0.5), left)
	box, hover := s.hitButton(c, id, box, e.Text, "", false)
	bg := kit.MixToken(tokens.Transparent, t.Accent.WithAlpha(0.5), hover)
	fg := s.ghostFg(hover)
	box.Draw(func(dc *ui.DrawContext) {
		if dc.State.FocusVisible {
			l.PaintRing(dc, 2, t.Primary.WithAlpha(0.5), s.radiusSM())
		}
		l.PaintSurface(dc, kit.Surface{Bg: bg, Radius: l.Rem(0.25)})
	}).Children(func() {
		l.Text(c.Key("text"), e.Text, kit.TextXS, kit.WeightNormal, fg, kit.Ellipsis()).Grow(1).Shrink(1).MinWidth(0)
	})
	return box.Clicked()
}

// jumpTo 把正文滚到某个标题（页内目录点击）：标题上沿落在可视区顶部。没记到这个标题的位置（还没画出）时不动。
func (s *Screen) jumpTo(view kit.ScrollView, slug string) {
	d := &s.detail
	r, ok := d.anchors[slug]
	if !ok || view.State == nil {
		return
	}
	view.State.ScrollTo(max(r.Y-d.colTop, 0))
}
