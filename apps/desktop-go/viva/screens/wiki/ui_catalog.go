package wiki

import (
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// catalogList 是目录列（渲染层 WikiCatalogList）：搜索框、计数与排序、页面列表。
// 搜索走全文索引（覆盖正文），输入停 250 ms 后才在后台发起查询；失败画成错误态而不是「没有匹配」。
func (s *Screen) catalogList(c ui.C, v View) {
	l := s.Look
	s.catalogSeen = true
	if !s.catalogLive {
		// 目录列刚挂载：搜索框回到 Store 里的查询（渲染层 useState(searchQuery)）。
		s.catalogLive = true
		s.search.SetText(v.SearchQuery)
		s.searchSeen = v.SearchQuery
	}
	s.watchSearch(v)
	visible := v.Visible()
	ui.Column(c.Key("catalog")).FillWidth().FillHeight().Children(func() {
		s.catalogHead(c, v, len(visible))
		l.ScrollAreaView(c.Key("catalog-scroll"), [4]float32{0, l.Space(2), l.Space(2), l.Space(2)}, func(view kit.ScrollView) {
			s.catalogRows(c, v, visible, view)
		}).FillWidth().Grow(1).MinHeight(0)
	})
}

// watchSearch 盯着搜索框：文字变了就重排防抖（渲染层的 useEffect：input 与 searchQuery 不同才排，卸载或再输入时清掉）。
func (s *Screen) watchSearch(v View) {
	text := s.search.Text()
	if text == s.searchSeen {
		return
	}
	s.searchSeen = text
	s.stopSearchTimer()
	if text == v.SearchQuery {
		return
	}
	s.searchStop = s.after(searchDebounce, func() {
		s.spawn(func() { s.Store.RunSearch(text) })
	})
}

// stopSearchTimer 取消还没触发的防抖。
func (s *Screen) stopSearchTimer() {
	if s.searchStop != nil {
		s.searchStop()
		s.searchStop = nil
	}
}

// clearSearch 清空搜索框并立刻回到全部页面（清除钮：不等防抖）。
func (s *Screen) clearSearch() {
	s.search.SetText("")
	s.searchSeen = ""
	s.stopSearchTimer()
	s.spawn(func() { s.Store.RunSearch("") })
}

// catalogHead 是目录列顶部：shrink-0 space-y-1.5 px-3 py-2，搜索框一行、计数与排序一行。
func (s *Screen) catalogHead(c ui.C, v View, shown int) {
	l := s.Look
	c = c.Key("head")
	ui.Column(c).FillWidth().Shrink(0).Gap(l.Space(1.5)).PadSides(l.Space(2), l.Space(3), l.Space(2), l.Space(3)).Children(func() {
		s.searchBox(c, v)
		s.countAndSort(c, v, shown)
	})
}

// searchBox 是搜索框：flex h-8 items-center gap-1.5 rounded-lg border border-border bg-background/60 px-2.5。
// 里面放大镜、输入框、读取中的小转圈与清除钮（有文字时）。
func (s *Screen) searchBox(c ui.C, v View) {
	l := s.Look
	t := l.T
	c = c.Key("search")
	ui.Row(c).FillWidth().Height(l.Rem(2)).Items(ui.Center).Gap(l.Space(1.5)).PadSides(0, l.Space(2.5)+1, 0, l.Space(2.5)+1). // px-2.5 加 1px 边框（边框画在盒内，不占布局）
																	Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.6), Border: t.Border, BorderW: 1, Radius: s.radiusLG()})
		}).Children(func() {
		l.Icon(c.Key("icon"), icons.Search, l.Rem(0.875), t.MutedForeground.WithAlpha(0.7)).Shrink(0)
		placeholder := locales.WikiFilterPlaceholder()
		l.Input(c.Key("input"), &s.search, placeholder, placeholder, kit.InputSpec{
			Size: l.Rem(0.75), Weight: kit.WeightNormal, Color: t.Foreground, Placeholder: t.MutedForeground.WithAlpha(0.6),
			Height: l.Rem(1), // 渲染层的 <input> 没有内边距，高度就是 text-xs 的行高 16px（Viva 的输入框至少 Size+6 高，居中后文字偏低；MarginBottom(1) 把它托回，小/中/大三档都对上）
		}).Grow(1).Shrink(1).MinWidth(0).MarginBottom(1)
		if v.Searching {
			l.Spinner(c.Key("spin"), l.Rem(0.875), t.MutedForeground)
		}
		if s.search.Text() != "" {
			b := l.Button(c.Key("clear"), kit.ButtonSpec{
				Name: locales.HeaderClearSearch(), Hint: locales.HeaderClearSearch(), Icon: icons.X, IconSize: l.Rem(0.75), Center: true,
			}, kit.ButtonStyle{
				Height: l.Rem(1), Width: l.Rem(1), Radius: l.Rem(0.25),
				Fg: t.MutedForeground, FgHover: t.Foreground, Transition: kit.DurationDefault,
			})
			if b.Clicked() {
				s.clearSearch()
			}
		}
	})
}

// countAndSort 是计数与排序行：flex items-center justify-between gap-2 px-0.5。搜索命中时按相关度排，不显示排序钮。
func (s *Screen) countAndSort(c ui.C, v View, shown int) {
	l := s.Look
	t := l.T
	c = c.Key("count-sort")
	ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2)).PadSides(0, l.Space(0.5), 0, l.Space(0.5)).Children(func() {
		l.Text(c.Key("count"), locales.WikiCatalogCount(shown), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).Shrink(1).MinWidth(0)
		ui.Spacer(c.Key("auto")).Grow(1)
		if v.SearchActive {
			return
		}
		ui.Row(c.Key("sorts")).Shrink(0).Items(ui.Center).Gap(l.Space(0.5)).Children(func() {
			for _, sort := range Sorts {
				label := sortLabel(sort)
				b := l.Button(c.Key("sort-"+string(sort)), kit.ButtonSpec{Label: label, Toggle: true, On: v.Sort == sort}, kit.ButtonStyle{
					FontPx: 11, LinePx: px11Line, PadX: l.Space(1.5), PadY: l.Space(0.5), Radius: l.Rem(0.25), Weight: kit.WeightNormal,
					Fg: t.MutedForeground.WithAlpha(0.6), FgHover: t.Foreground, FgOn: t.Primary, BgOn: t.Primary.WithAlpha(0.1),
					Transition: kit.DurationDefault,
				})
				if b.Clicked() {
					s.Store.SetSort(sort)
				}
			}
		})
	})
}

// sortLabel 是排序钮的名称。
func sortLabel(sort CatalogSort) string {
	switch sort {
	case SortLinked:
		return locales.WikiSortLinked()
	case SortTitle:
		return locales.WikiSortTitle()
	}
	return locales.WikiSortRecent()
}

// catalogRows 是列表区：搜索失败（错误态）、没有页面（空态）或窗口化的页面行（space-y-0.5）。
func (s *Screen) catalogRows(c ui.C, v View, visible []services.WikiCatalogEntry, view kit.ScrollView) {
	l := s.Look
	t := l.T
	switch {
	case v.SearchError != "":
		// 搜索失败此前只是清空命中集合，画出来和「没有匹配的页面」一模一样：必须画成错误态。
		if l.LoadErrorState(c.Key("search-error"), v.SearchError) {
			query := v.SearchQuery
			s.spawn(func() { s.Store.RunSearch(query) })
		}
	case len(visible) == 0:
		text := locales.WikiFilterEmpty()
		if len(v.Catalog) == 0 {
			text = locales.WikiCatalogEmpty()
		}
		ui.Row(c.Key("empty")).FillWidth().Justify(ui.Center).PadSides(l.Space(6), l.Space(2), l.Space(6), l.Space(2)).Children(func() {
			l.Text(c.Key("text"), text, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped())
		})
	default:
		s.window.Seed = l.Rem(4.5)
		s.window.Rows(c, kit.RowsSpec{
			Name: "pages", N: len(visible), Gap: l.Space(0.5), View: view, MeasureAll: true,
			Epoch: kit.RowEpoch{RootPx: l.RootPx, Variant: int(locales.CurrentLang()), Width: float32(int(s.pane.Width))},
			Key:   func(i int) string { return visible[i].ID },
			Row:   func(i int) ui.Box { return s.catalogRow(c, v, visible[i]) },
		})
	}
}

// catalogRow 是一页：w-full rounded-lg px-2.5 py-2 text-left；选中 bg-primary/10，悬停 bg-accent/50。
// 标题一行（选中时 font-medium text-primary），手动编辑过的页加一枚铅笔，再是类型徽标；有摘要时最多两行；最后一行是被引用数。
func (s *Screen) catalogRow(c ui.C, v View, e services.WikiCatalogEntry) ui.Box {
	l := s.Look
	t := l.T
	selected := e.ID == v.SelectedPageID
	// 行在窗口里的序号会变：命中区与盒子都按页面 ID 命名，状态才不会串到别的行上。
	id := ui.ID(c.Key("hit-" + e.ID))
	box := ui.Column(c.Key("row-"+e.ID)).FillWidth().PadSides(l.Space(2), l.Space(2.5), l.Space(2), l.Space(2.5))
	backlinks := v.BacklinkCounts[e.ID]
	box, hover := s.hitButton(c, id, box, catalogRowName(e, backlinks), "", false)
	bg := kit.MixToken(tokens.Transparent, t.Accent.WithAlpha(0.5), hover)
	if selected {
		bg = t.Primary.WithAlpha(0.1)
	}
	titleColor, titleWeight := t.Foreground, float32(kit.WeightNormal)
	if selected {
		titleColor, titleWeight = t.Primary, kit.WeightMedium
	}
	box = box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), s.radiusLG())
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Radius: s.radiusLG()})
	}).Children(func() {
		ui.Row(c.Key("title-row")).FillWidth().Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
			l.Text(c.Key("title"), e.Title, kit.TextSM, titleWeight, titleColor, kit.Ellipsis()).Grow(1).Shrink(1).MinWidth(0)
			if e.ManualEditedAt != nil {
				l.Icon(c.Key("manual"), icons.Pencil, l.Rem(0.75), t.Primary.WithAlpha(0.7)).Shrink(0)
			}
			s.kindBadge(c, e.Kind)
		})
		if e.Summary != "" {
			// mt-0.5 line-clamp-2 text-xs leading-snug text-muted-foreground/80
			l.TextPx(c.Key("summary"), e.Summary, l.Rem(0.75), l.Rem(0.75)*1.375, kit.WeightNormal, t.MutedForeground.WithAlpha(0.8),
				kit.Wrapped(), kit.LineClamp(2)).FillWidth().MarginTop(l.Space(0.5))
		}
		// mt-1 flex items-center gap-1 text-[10px] text-muted-foreground/60
		ui.Row(c.Key("links")).FillWidth().Items(ui.Center).Gap(l.Space(1)).MarginTop(l.Space(1)).Children(func() {
			dim := t.MutedForeground.WithAlpha(0.6)
			l.Icon(c.Key("icon"), icons.Link2, l.Rem(0.625), dim).Shrink(0)
			text := locales.WikiOrphanHint()
			if backlinks > 0 {
				text = locales.WikiBacklinkCount(backlinks)
			}
			l.TextPx(c.Key("text"), text, 10, px10Line, kit.WeightNormal, dim, kit.Ellipsis()).Shrink(1).MinWidth(0)
		})
	})
	if box.Clicked() {
		id := e.ID
		s.spawn(func() { s.Store.SelectPage(id) })
	}
	return box
}

// catalogRowName 是页面行的可访问名：渲染层的行是一个按钮，名字由它的内容拼成——标题、手动编辑的铅笔（图片名）、类型徽标、摘要、被引用数，
// 以空格相连。对照的 L1 按整串名字配对，只写标题会配不上。
func catalogRowName(e services.WikiCatalogEntry, backlinks int) string {
	parts := []string{e.Title}
	if e.ManualEditedAt != nil {
		parts = append(parts, locales.WikiManualEdited())
	}
	parts = append(parts, kindLabel(e.Kind))
	if e.Summary != "" {
		parts = append(parts, e.Summary)
	}
	if backlinks > 0 {
		parts = append(parts, locales.WikiBacklinkCount(backlinks))
	} else {
		parts = append(parts, locales.WikiOrphanHint())
	}
	return strings.Join(parts, " ")
}
