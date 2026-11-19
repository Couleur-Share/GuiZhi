package library

import (
	"cmp"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// countOf 是映射里某个键的计数；计数还没读到（Counts 为 nil）或没有这个键时是 0。
func countOf(counts *services.LibraryCounts, pick func(*services.LibraryCounts) map[string]int, key string) int {
	if counts == nil {
		return 0
	}
	return pick(counts)[key]
}

func byCollection(c *services.LibraryCounts) map[string]int { return c.ByCollection }
func byTag(c *services.LibraryCounts) map[string]int        { return c.ByTag }
func byPlatform(c *services.LibraryCounts) map[string]int   { return c.ByPlatform }

// sectionHeading 是分区标题（渲染层 SectionHeading）：mb-1 mt-4 flex shrink-0 items-center justify-between gap-2 px-3，
// 左边是 truncate text-xs font-semibold uppercase tracking-wider text-sidebar-foreground/80 的名称，右边是可选的小按钮。
func (s *Screen) sectionHeading(c ui.C, key, label string, action func(c ui.C)) {
	s.headingRow(c, key, label, action, false)
}

// headingRow 声明分区标题一行。shrinkable 为真是智能视图那一份：渲染层 SmartViewsSection 自己写的标题漏了 shrink-0，
// 是个 min-height:auto 的普通弹性项——它带着权重参加溢出时的收缩分摊，只是压不下去（台账 B-034，见 kit.FlexColumn）。
func (s *Screen) headingRow(c ui.C, key, label string, action func(c ui.C), shrinkable bool) {
	l := s.Look
	t := l.T
	s.flex.Item(kit.FlexItem{Shrink: shrinkable, MinAuto: shrinkable, MarginTop: l.Space(4), MarginBottom: l.Space(1)})
	ui.Row(c.Key("heading-"+key)).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(2)).
		PadSides(0, l.Space(3), 0, l.Space(3)).Children(func() {
		// uppercase：Latin 字母大写显示，中文不受影响；字距 tracking-wider = 0.05em。
		l.TextTracked(c.Key("label"), strings.ToUpper(label), kit.TextXS, kit.WeightSemibold, t.SidebarForeground.WithAlpha(0.8), 0.05)
		if action != nil {
			action(c.Key("action"))
		}
	})
}

// headingButton 是分区标题右边的小按钮：rounded-lg p-1 text-sidebar-foreground/50（/80）transition-colors
// hover:bg-sidebar-accent hover:text-primary，14px 的图标；名称与 title 都是 label。
func (s *Screen) headingButton(c ui.C, icon gfx.Icon, label string, rest float32) ui.Box {
	l := s.Look
	t := l.T
	return l.Button(c, kit.ButtonSpec{Name: label, Icon: icon, IconSize: l.Rem(0.875), Hint: label}, kit.ButtonStyle{
		PadX: l.Space(1), PadY: l.Space(1), Radius: l.Rem(0.75),
		Fg: t.SidebarForeground.WithAlpha(rest), FgHover: t.Primary, BgHover: t.SidebarAccent,
		Transition: kit.DurationDefault,
	})
}

// hint 是分区里没有内容时的说明：px-3 py-1 text-xs text-sidebar-foreground/75。是 min-height:auto 的普通弹性项。
func (s *Screen) hint(c ui.C, key, text string) {
	l := s.Look
	s.flex.Item(kit.FlexItem{Shrink: true, MinAuto: true})
	ui.Row(c.Key("hint-"+key)).FillWidth().PadSides(l.Space(1), l.Space(3), l.Space(1), l.Space(3)).Children(func() {
		l.Text(c.Key("text"), text, kit.TextXS, kit.WeightNormal, l.T.SidebarForeground.WithAlpha(0.75), kit.Wrapped())
	})
}

// loadFailed 在分区的位置声明读取失败的错误态（台账 B-036；渲染层把读不出来画成空态，这里按 AGENTS.md 画成错误态）：
// 原因加「重试」，重试重新读取三份数据。是 min-height:auto 的普通弹性项，key 区分各分区里的那一个。
func (s *Screen) loadFailed(c ui.C, key, message string) {
	s.flex.Item(kit.FlexItem{Shrink: true, MinAuto: true})
	ui.Column(c.Key("failed-" + key)).FillWidth().Children(func() {
		if s.Look.LoadErrorState(c, message) {
			s.Store.Load(s.Ctx)
		}
	})
}

// collectionGlyph 是合集行的图标：有 emoji 画 emoji（text-sm leading-none），否则是 16px 的文件夹。
func (s *Screen) collectionGlyph(icon string) func(c ui.C, fg tokens.Color) {
	l := s.Look
	if emoji := jsTrim(icon); emoji != "" {
		return func(c ui.C, fg tokens.Color) { s.emojiGlyph(c, emoji, l.Rem(0.875), l.Rem(1), fg) } // 图标槽 h-4 w-4
	}
	return func(c ui.C, fg tokens.Color) { l.Icon(c, icons.Folder, l.Rem(1), fg) }
}

// collectionsSection 是「合集」分区：标题带「新建合集」按钮，下面是各合集行，没有合集时给一句说明。
// 合集行数没有上限，只声明看得见的几行（台账 B-035，见 kit.FlexColumn.Run）。
func (s *Screen) collectionsSection(c ui.C, v View, view kit.ScrollView) {
	s.sectionHeading(c, "collections", locales.LibraryCollections(), func(c ui.C) {
		if s.headingButton(c, icons.FolderPlus, locales.LibraryNewCollection(), 0.5).Clicked() {
			s.openEdit(editCreateCollection, services.LibraryCollection{}, services.LibraryTag{})
		}
	})
	if v.CollectionsError != "" { // 从没读到过：不能画成「还没有合集」（B-036）
		s.loadFailed(c, "collections", v.CollectionsError)
		return
	}
	if len(v.Collections) == 0 {
		s.hint(c, "collections", locales.LibraryNoCollections())
		return
	}
	s.sidebarRun(c, "collections", len(v.Collections), view, func(i int) {
		col := v.Collections[i]
		s.rowBox(c, rowSpec{
			Key: "collection:" + col.ID, Icon: s.collectionGlyph(col.Icon), Label: col.Name,
			Count:  countOf(v.Counts, byCollection, col.ID),
			Active: v.Filters.CollectionID == col.ID,
			More:   locales.LibraryMoreActions(),
			OnClick: func() {
				s.Store.SelectCollection(s.Ctx, col.ID)
			},
			OnMore:    func(e moreEvent) { s.toggleMenu(e, s.collectionMenuItems(col)) },
			OnContext: func(at gfx.Point) { s.popMenu(at, s.collectionMenuItems(col)) },
		})
	})
}

// platformsSection 是「平台」分区（采集来源，派生分组，没有行菜单）：只列当前分面下有条目的平台，已选的保留；
// 顺序固定，不按数量排，免得采集一条就重排一次。
func (s *Screen) platformsSection(c ui.C, v View) {
	l := s.Look
	s.sectionHeading(c, "platforms", locales.LibraryPlatforms(), nil)
	if v.CountsError != "" { // 平台来自计数；计数从没读到过时不能画成「采集网页或视频后按来源分组」（B-036）
		s.loadFailed(c, "platforms", v.CountsError)
		return
	}
	var shown []string
	for _, p := range SourcePlatforms {
		if countOf(v.Counts, byPlatform, p) > 0 || p == v.Filters.Platform {
			shown = append(shown, p)
		}
	}
	if len(shown) == 0 {
		s.hint(c, "platforms", locales.LibraryNoPlatforms())
		return
	}
	for _, p := range shown {
		s.sidebarRow(c, rowSpec{
			Key: "platform:" + p, Label: kit.PlatformLabel(p),
			Icon:   func(c ui.C, fg tokens.Color) { l.PlatformIcon(c, p, l.Rem(1), fg) },
			Count:  countOf(v.Counts, byPlatform, p),
			Active: v.Filters.Platform == p,
			OnClick: func() {
				s.Store.SelectPlatform(s.Ctx, p)
			},
		})
	}
}

// tagVisible 报告标签这一行要不要列出（渲染层 isTagVisible）：只列有条目的，判据取 counts.byTag（行上的数字就是它，
// 与 tag.itemCount 口径不同——后者含归档）；例外是当前正筛着的那个，把用户所在的位置藏掉等于一个没有高亮的空列表。
func tagVisible(v View, tag services.LibraryTag) bool {
	return countOf(v.Counts, byTag, tag.ID) > 0 || tag.ID == v.Filters.TagID
}

// tagsSection 是「标签」分区：标题上的眼睛按钮切换「显示未使用的标签」（没有可露出的就不摆；开着时保留，否则关不回去）。
// 开关刻意不持久化：它是「清理一下」时才用的临时视图。标签行与合集行一样只声明看得见的几行（台账 B-035）。
func (s *Screen) tagsSection(c ui.C, v View, view kit.ScrollView) {
	l := s.Look
	// 标签列表或计数从没读到过：哪些标签有条目是由计数定的，读不出来时整个分区画成错误态，不画成「在条目详情中添加标签」（B-036）。
	if reason := cmp.Or(v.TagsError, v.CountsError); reason != "" {
		s.sectionHeading(c, "tags", locales.LibraryTags(), nil)
		s.loadFailed(c, "tags", reason)
		return
	}
	var unused int
	for _, tag := range v.Tags {
		if !tagVisible(v, tag) {
			unused++
		}
	}
	toggleLabel := locales.LibraryShowUnusedTags(unused)
	icon := icons.Eye
	if s.showUnused {
		toggleLabel, icon = locales.LibraryHideUnusedTags(), icons.EyeOff
	}
	var action func(c ui.C)
	if unused > 0 || s.showUnused {
		action = func(c ui.C) {
			if s.headingButton(c, icon, toggleLabel, 0.5).Clicked() {
				s.showUnused = !s.showUnused
			}
		}
	}
	s.sectionHeading(c, "tags", locales.LibraryTags(), action)

	var shown []services.LibraryTag
	for _, tag := range v.Tags {
		if s.showUnused || tagVisible(v, tag) {
			shown = append(shown, tag)
		}
	}
	if len(shown) == 0 {
		s.hint(c, "tags", locales.LibraryNoTags())
		return
	}
	s.sidebarRun(c, "tags", len(shown), view, func(i int) {
		tag := shown[i]
		dot := tagDotColor(tag.ColorKey)
		s.rowBox(c, rowSpec{
			Key: "tag:" + tag.ID, Label: tag.Name,
			Icon: func(c ui.C, _ tokens.Color) {
				size := l.Rem(0.625) // h-2.5 w-2.5 rounded-full
				ui.Row(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
					l.PaintSurface(d, kit.Surface{Bg: dot, Radius: size / 2})
				})
			},
			Count:  countOf(v.Counts, byTag, tag.ID),
			Active: v.Filters.TagID == tag.ID,
			More:   locales.LibraryMoreActions(),
			OnClick: func() {
				s.Store.SelectTag(s.Ctx, tag.ID)
			},
			OnMore:    func(e moreEvent) { s.toggleMenu(e, s.tagMenuItems(tag)) },
			OnContext: func(at gfx.Point) { s.popMenu(at, s.tagMenuItems(tag)) },
		})
	})
}

// trashSection 是底部的回收站一行：mt-auto pt-4 的块，内容不满一屏时贴着面板底部（auto 上外边距拿走富余高度）；
// 块本身是 min-height:auto 的普通弹性项，行是块里的内容，不是列的直接孩子。
func (s *Screen) trashSection(c ui.C, v View) {
	l := s.Look
	count := 0
	if v.Counts != nil {
		count = v.Counts.Trash
	}
	s.flex.Item(kit.FlexItem{Shrink: true, MinAuto: true, MarginTopAuto: true})
	ui.Column(c.Key("trash")).FillWidth().PadTop(l.Space(4)).Children(func() {
		if v.CountsError != "" { // 回收站的数字读不出来：不画成没有计数的一行（B-036）
			if l.LoadErrorState(c, v.CountsError) {
				s.Store.Load(s.Ctx)
			}
			return
		}
		s.rowBox(c, rowSpec{
			Key:   "trash",
			Icon:  func(c ui.C, fg tokens.Color) { l.Icon(c, icons.Trash2, l.Rem(1), fg) },
			Label: locales.LibraryScopeTrash(), Count: count,
			Active: v.Filters.IsTrashActive(),
			OnClick: func() {
				s.Store.SetScope(s.Ctx, services.LibraryScopeTrash)
			},
		})
	})
}
