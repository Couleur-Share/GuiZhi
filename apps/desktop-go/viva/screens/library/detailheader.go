package library

import (
	"math"
	"net/url"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 详情头部（渲染层 components/library/ItemDetailHeader）：标题独占一行，下面一排元信息 chip 与右对齐的动作区。
// 回收站条目只提供「恢复」。

const (
	// moreMenuWidth 是「更多」菜单对齐按钮右缘用的宽度（渲染层 MORE_MENU_WIDTH_PX，逻辑像素，不随字号档缩放）。
	moreMenuWidth float32 = 160
	// chipFont 与 chipLine 是 chip 里文字的字号与行高：text-[11px]，行高继承页面的 1.5。
	chipFont float32 = 11
	chipLine float32 = 16.5
)

// chipTone 是元信息 chip 的色调（渲染层 CHIP_TONES）。
type chipTone uint8

const (
	chipMuted chipTone = iota
	chipWarning
	chipDanger
)

// chipInk 返回色调对应的边框与文字颜色：muted border-border/70 text-muted-foreground；warning border-amber-500/40
// text-amber-500；danger border-destructive/50 text-destructive。
func (s *Screen) chipInk(tone chipTone) (border, fg tokens.Color) {
	t := s.Look.T
	switch tone {
	case chipWarning:
		amber := tokens.Tailwind("amber-500")
		return amber.WithAlpha(0.4), amber
	case chipDanger:
		return t.Destructive.WithAlpha(0.5), t.Destructive
	}
	return t.Border.WithAlpha(0.7), t.MutedForeground
}

// detailHeader 是详情头部：shrink-0 px-6 pb-3 pt-5。
func (s *Screen) detailHeader(c ui.C, v View, item services.LibraryItem, caps ItemCaps, trashed bool, onClose func()) {
	l := s.Look
	c = c.Key("header")
	ui.Column(c).FillWidth().Shrink(0).PadSides(l.Space(5), l.Space(6), l.Space(3), l.Space(6)).Children(func() {
		s.detailTitle(c, v, item, trashed)
		// mt-2.5 flex flex-wrap items-start gap-x-3 gap-y-2：空间不足时整组操作换行，避免把元信息挤成碎片。
		// 动作区的 ml-auto：Viva 没有自动外边距，改让 Flow 的每一行靠行尾排（Justify 对每一行各自生效）。chip 行 Grow(1) 在自己那一行吃掉剩余空间，
		// 与动作区同一行时它们照旧分摊；动作区折到下一行单独成行时，靠右（渲染层 ml-auto 的效果）。
		ui.Flow(c.Key("meta")).FillWidth().MarginTop(l.Space(2.5)).Items(ui.Start).Justify(ui.End).Gap(l.Space(3)).LineGap(l.Space(2)).Children(func() {
			s.detailChips(c, v, item, trashed)
			s.detailActions(c, v, item, caps, trashed, onClose)
		})
	})
}

// detailTitle 是标题框（渲染层 textarea[data-testid=item-title-input]）：
// block w-full resize-none border-none bg-transparent rounded-sm pt-0.5 text-[1.375rem] font-semibold leading-snug。
//
// 渲染层的标题框随内容增高、超过三行再滚动，Enter 把光标交给正文编辑器；这里是单行输入：长标题横向滚动，
// Enter 暂不转移焦点（台账 B-062 与 B-060，编辑器随 M2-6）。回收站条目只读：输入被丢掉，文字退回原值。
func (s *Screen) detailTitle(c ui.C, v View, item services.LibraryItem, trashed bool) {
	l := s.Look
	t := l.T
	d := &s.detail
	size := l.Rem(1.375)
	// pt-0.5 加一行 leading-snug；渲染层按 scrollHeight 定高，scrollHeight 取整（32.25 → 32），所以框高是整数像素。
	height := float32(math.Round(float64(l.Space(0.5) + size*1.375)))
	// 新建的空白条目：光标直接落进标题（渲染层 titleFocusItemId）。每个条目一个输入控件：切换条目就是新控件，
	// AutoFocus 在它第一次出现的那一帧生效。
	key := c.Key("title-" + item.ID)
	focus := v.Detail.TitleFocusID == item.ID
	if d.titleItem != item.ID {
		d.titleItem = item.ID
		d.title.SetText(item.Title)
	}
	box := l.Input(key, &d.title, locales.LibraryTitlePlaceholder(), locales.LibraryTitlePlaceholder(), kit.InputSpec{
		Size: size, Weight: kit.WeightSemibold, Color: t.Foreground, Height: height,
		// 文字落在渲染层 textarea 首行的位置：行盒顶在 pt-0.5 之下，行高 leading-snug（框顶带小数时与 input 居中差一行）。
		PadTop: l.Space(0.5), LineHeight: size * 1.375,
		Placeholder: t.MutedForeground.WithAlpha(0.6),
	}).FillWidth()
	if focus {
		box = box.AutoFocus()
		s.Store.ClearTitleFocus()
	}
	switch {
	case box.Changed() && !trashed:
		next := d.title.Text()
		s.Store.UpdateSelected(s.Ctx, services.LibraryDraftFields{Title: &next})
	case d.title.Text() != item.Title:
		d.title.SetText(item.Title) // 受控：外部改了标题（采用来源版本、冲突解决），或只读条目的输入被丢掉
	}
}

// saveChip 是头部保存状态的两枚 chip 与保存钮要用的几个量（渲染层 ItemDetailHeader 里的 isDirty 等）。
type saveStatus struct {
	// dirty 是「有未落盘的改动要提示」：自动保存开着时，改动正在排队不算脏；保存失败时改动退回待保存队列，必须提示。
	dirty bool
	// label 与 tone 是状态 chip 的文字与色调。
	label string
	tone  chipTone
}

// saveStatusOf 复刻渲染层的状态文字规则：保存中、保存失败、未保存，其余是「更新于 …」（只在有状态要提示时才显示 chip）。
func (s *Screen) saveStatusOf(det DetailView, item services.LibraryItem) saveStatus {
	st := saveStatus{
		dirty: det.Unsaved && (!s.Store.AutoSaveEnabled() || det.SaveError != ""),
		label: locales.LibraryUpdatedAt(kit.FormatItemTime(item.UpdatedAt, s.nowTime(), s.loc(), s.Locale)),
		tone:  chipMuted,
	}
	switch {
	case det.Saving:
		st.label = locales.LibrarySaving()
	case det.SaveError != "":
		st.tone, st.label = chipDanger, locales.LibrarySaveFailed()
	case st.dirty:
		st.tone, st.label = chipWarning, locales.LibraryUnsaved()
	}
	return st
}

// detailChips 是头部左边的 chip 行：flex min-w-0 flex-[1_1_16rem] flex-wrap items-center gap-1.5。
func (s *Screen) detailChips(c ui.C, v View, item services.LibraryItem, trashed bool) {
	l := s.Look
	t := l.T
	det := v.Detail
	st := s.saveStatusOf(det, item)
	ui.Flow(c.Key("chips")).Grow(1).Shrink(1).Basis(l.Rem(16)).MinWidth(0).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
		s.collectionChip(c, v, item, trashed)
		s.sourceChip(c, item)
		if words := s.derivedOf(item).words; words > 0 {
			// px-1 text-xs text-muted-foreground
			ui.Row(c.Key("words")).Shrink(0).PadSides(0, l.Space(1), 0, l.Space(1)).Children(func() {
				l.Text(c.Key("words-text"), locales.LibraryWordCount(words), kit.TextXS, kit.WeightNormal, t.MutedForeground)
			})
		}
		if item.IsPinned {
			s.metaChip(c.Key("pinned"), icons.Pin, locales.LibraryPinnedBadge(), chipMuted, "")
		}
		if item.Status == services.LibraryStatusArchived {
			s.metaChip(c.Key("archived"), icons.Archive, locales.LibraryArchivedBadge(), chipMuted, "")
		}
		if det.JustSaved && !st.dirty && !det.Saving && det.SaveError == "" {
			s.metaChip(c.Key("saved"), icons.Check, locales.LibrarySaved(), chipMuted, "")
		}
		if st.dirty || det.Saving || det.SaveError != "" {
			s.metaChip(c.Key("status"), icons.Clock, st.label, st.tone, det.SaveError)
		}
		if st.dirty && !trashed {
			s.saveButton(c)
		}
	})
}

// wordCount 是正文去掉首尾空白后的字数（渲染层 item.content.trim().length：UTF-16 码元数）。
func wordCount(content string) int {
	n := 0
	for _, r := range jsTrim(content) {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// metaChip 是一枚只读的元信息 chip（渲染层 MetaChip）：inline-flex h-6 items-center gap-1 rounded-md border px-2 text-[11px]，
// 图标 h-3.5 w-3.5，文字最宽 12rem 超出截断；title 非空时悬停出气泡。
func (s *Screen) metaChip(c ui.C, ic gfx.Icon, label string, tone chipTone, title string) {
	l := s.Look
	border, fg := s.chipInk(tone)
	row := ui.Row(c).Height(l.Rem(1.5)).Shrink(0).Items(ui.Center).Gap(l.Space(1)).
		PadSides(0, l.Space(2)+1, 0, l.Space(2)+1)
	if title != "" {
		// 只有悬停气泡：渲染层的 <span title> 在可访问树里是无名的 generic，不是分组，不进可访问节点。
		row = row.HitWith(ui.ID(c), "", gfx.HitOptions{SkipFocus: true, SkipTab: true, Hint: title})
	}
	row.Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Border: border, BorderW: 1, Radius: l.Rem(0.75) - 2})
	}).Children(func() {
		l.Icon(c.Key("icon"), ic, l.Rem(0.875), fg)
		l.TextPx(c.Key("label"), label, chipFont, chipLine, kit.WeightNormal, fg, kit.Ellipsis()).MaxWidth(l.Rem(12)).Shrink(1)
	})
}

// saveButton 是未保存时出现的「保存」钮：inline-flex h-6 items-center gap-1 rounded-md bg-primary px-2 text-[11px] font-medium
// text-primary-foreground hover:bg-primary/90，图标 h-3 w-3，title 是「保存 (Ctrl+S)」。
func (s *Screen) saveButton(c ui.C) {
	l := s.Look
	t := l.T
	box := l.Button(c.Key("save"), kit.ButtonSpec{
		Label: locales.LibrarySave(), Icon: icons.Save, IconSize: l.Rem(0.75), Hint: locales.LibrarySaveNow(), Description: locales.LibrarySaveNow(),
	}, kit.ButtonStyle{
		Height: l.Rem(1.5), PadX: l.Space(2), Radius: l.Rem(0.75) - 2, Gap: l.Space(1), FontPx: chipFont, LinePx: chipLine, Weight: kit.WeightMedium,
		Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault,
	})
	if box.Clicked() {
		s.flushSave()
	}
}

// flushSave 立即保存（保存钮、Ctrl+S）：落盘在后台，失败由状态 chip 说出来。
func (s *Screen) flushSave() {
	s.Store.Spawn(func() { s.Store.FlushPendingSave(s.Ctx) })
}

// collectionChip 是所属合集 chip（渲染层 CollectionChip）：点击弹出合集列表。
// 走「移到合集」命令而不是防抖保存队列——分类是过滤条件，改完侧栏计数与当前列表都要立刻跟上。
func (s *Screen) collectionChip(c ui.C, v View, item services.LibraryItem, trashed bool) {
	l := s.Look
	t := l.T
	d := &s.detail
	var current *services.LibraryCollection
	for i := range v.Collections {
		if v.Collections[i].ID == item.CollectionID && item.CollectionID != "" {
			current = &v.Collections[i]
			break
		}
	}
	name := locales.LibraryNoCollection()
	emoji := ""
	if current != nil {
		name = current.Name
		emoji = jsTrim(current.Icon)
	}
	cc := c.Key("collection")
	box := s.inlineButton(cc, inlineSpec{
		Name: name, Value: name, Role: gfx.RoleCombobox, Popup: true, Open: d.collection.open, Disabled: trashed, DisabledOpacity: 0.6,
		Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent.WithAlpha(0.6),
		Border: t.Border.WithAlpha(0.7), BorderHover: t.Border, BorderW: 1,
		Radius: l.Rem(0.75) - 2, Height: l.Rem(1.5), PadX: l.Space(2) + 1, Gap: l.Space(1), Rect: &d.collectionRect,
	}, func(fg tokens.Color) {
		if emoji != "" {
			// aria-hidden leading-none：表情按 chip 的 11px 字号画。
			s.emojiGlyph(cc.Key("emoji"), emoji, chipFont, 0, fg)
		} else {
			l.Icon(cc.Key("icon"), icons.Folder, l.Rem(0.875), fg)
		}
		l.TextPx(cc.Key("name"), name, chipFont, chipLine, kit.WeightNormal, fg, kit.Ellipsis()).MaxWidth(l.Rem(8)).Shrink(1)
		l.Icon(cc.Key("chevron"), icons.ChevronDown, l.Rem(0.75), fg)
	})
	if box.Clicked() {
		if d.collection.open {
			d.collection.close()
		} else {
			r := d.collectionRect
			d.collection.show(gfx.Point{X: r.X, Y: r.Y + r.Height + 4})
		}
	}
	s.collectionMenuView(c, v, item)
}

// collectionMenuView 声明合集菜单：「未归合集」加每个合集一项，选中即移动。
func (s *Screen) collectionMenuView(c ui.C, v View, item services.LibraryItem) {
	l := s.Look
	d := &s.detail
	items := []kit.MenuItem{{Label: locales.LibraryNoCollection(), OnSelect: func() { s.moveToCollection([]string{item.ID}, "") }}}
	for _, col := range v.Collections {
		id := col.ID
		items = append(items, kit.MenuItem{Label: col.Name, OnSelect: func() { s.moveToCollection([]string{item.ID}, id) }})
	}
	keep := d.collectionRect
	if l.ContextMenu(c.Key("collection-menu"), &d.collection.st, kit.MenuSpec{
		Open: d.collection.open, X: d.collection.at.X, Y: d.collection.at.Y, Items: items, Keep: &keep,
	}) {
		d.collection.close()
	}
}

// sourceView 是来源 chip 要显示的内容（渲染层 SourceChip 的前半）。
type sourceView struct {
	// label 为空表示没有来源 chip；href 为空表示来源不是 http(s) 链接（只显示文字，不可点）。
	label, href string
	// tooltip 是悬停说明：视频元数据（原标题、简介、文字稿来源）与链接。
	tooltip string
	// wechat 表示来源是微信公众号（刷新走「补全」而不是重新采集）。
	wechat bool
}

// resolveSafeSourceURL 只放行 http(s) 的来源地址（渲染层 resolveSafeSourceUrl）。
func resolveSafeSourceURL(raw string) *url.URL {
	trimmed := jsTrim(raw)
	if trimmed == "" {
		return nil
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	return u
}

// sourceViewOf 复刻 SourceChip 的取值：视频条目显示正文元数据块里的平台、作者、时长，其余退回域名；公众号写「微信公众号」。
func sourceViewOf(item services.LibraryItem) sourceView {
	meta := notes.ParseVideoMeta(item.Content)
	u := resolveSafeSourceURL(item.SourceURI)
	host := ""
	if u != nil {
		host = u.Hostname()
	}
	var facts []string
	if meta != nil {
		for _, f := range []string{meta.Platform, meta.Author, meta.Duration} {
			if f != "" {
				facts = append(facts, f)
			}
		}
	}
	sv := sourceView{wechat: host == "mp.weixin.qq.com"}
	switch {
	case sv.wechat:
		sv.label = locales.LibraryPlatformWechat()
	case len(facts) > 0:
		sv.label = strings.Join(facts, " · ")
	default:
		sv.label = host
	}
	if sv.label == "" {
		return sourceView{}
	}
	var lines []string
	sep := locales.NativeLibraryResultSeparator() // 渲染层把全角冒号写死在模板字符串里，中英文界面都一样
	if meta != nil {
		if meta.OriginalTitle != "" {
			lines = append(lines, locales.LibraryMediaMetaOriginalTitle()+sep+meta.OriginalTitle)
		}
		if meta.Description != "" {
			lines = append(lines, locales.LibraryMediaMetaDescription()+sep+meta.Description)
		}
		if meta.TranscriptSource != "" {
			lines = append(lines, locales.LibraryMediaMetaTranscriptSource()+sep+meta.TranscriptSource)
		}
	}
	if u != nil {
		sv.href = u.String()
		lines = append(lines, sv.href)
	}
	sv.tooltip = strings.Join(lines, "\n")
	return sv
}

// sourceChip 是来源 chip（渲染层 SourceChip）：不可点的只有文字；链接那种旁边还有一枚刷新钮（回收站条目没有）。
func (s *Screen) sourceChip(c ui.C, item services.LibraryItem) {
	sv := s.derivedOf(item).source
	if sv.label == "" {
		return
	}
	l := s.Look
	t := l.T
	cc := c.Key("source")
	border, fg := s.chipInk(chipMuted)
	body := func(color tokens.Color, link bool) {
		l.Icon(cc.Key("icon"), icons.Link, l.Rem(0.875), color)
		l.TextPx(cc.Key("label"), sv.label, chipFont, chipLine, kit.WeightNormal, color, kit.Ellipsis()).MaxWidth(l.Rem(14)).Shrink(1)
		if link {
			l.Icon(cc.Key("external"), icons.ExternalLink, l.Rem(0.75), color)
		}
	}
	if sv.href == "" {
		row := ui.Row(cc).Height(l.Rem(1.5)).Shrink(0).Items(ui.Center).Gap(l.Space(1)).PadSides(0, l.Space(2)+1, 0, l.Space(2)+1)
		if sv.tooltip != "" {
			row = row.HitWith(ui.ID(cc), "", gfx.HitOptions{SkipFocus: true, SkipTab: true, Hint: sv.tooltip})
		}
		row.Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: border, BorderW: 1, Radius: l.Rem(0.75) - 2})
		}).Children(func() { body(fg, false) })
		return
	}
	// inline-flex items-center gap-1：链接 chip 加（有来源且未删除时的）刷新钮。
	ui.Row(c.Key("source-wrap")).Shrink(0).Items(ui.Center).Gap(l.Space(1)).Children(func() {
		hint := sv.tooltip
		if hint == "" {
			hint = locales.LibraryMediaMetaOpenSource()
		}
		link := s.inlineButton(cc, inlineSpec{
			Name: sv.label, Role: gfx.RoleLink, Hint: hint, Description: hint,
			Fg: fg, FgHover: t.Foreground, BgHover: t.Accent.WithAlpha(0.6), Border: border, BorderHover: t.Border, BorderW: 1,
			Radius: l.Rem(0.75) - 2, Height: l.Rem(1.5), PadX: l.Space(2) + 1, Gap: l.Space(1),
		}, func(color tokens.Color) { body(color, true) })
		if link.Clicked() {
			s.openSource(sv.href)
		}
		if item.DeletedAt == 0 {
			refresh := s.inlineButton(c.Key("source-refresh"), inlineSpec{
				Name: locales.LibraryRefreshSource(), Hint: locales.LibraryRefreshSource(),
				Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent,
				Radius: l.Rem(0.75) - 2, Height: l.Rem(1.5), Width: l.Rem(1.5),
			}, func(color tokens.Color) { l.Icon(c.Key("source-refresh-icon"), icons.RefreshCw, l.Rem(0.875), color) })
			if refresh.Clicked() {
				s.refreshSource(item, sv)
			}
		}
	})
}

// actionButtonStyle 是头部图标按钮的外观（渲染层 ACTION_BUTTON_BASE 与 IDLE / 选中）：h-8 w-8 rounded-lg，
// 聚焦环是实心主色 ring-2 ring-primary；按下（aria-pressed）时 bg-primary/15 text-primary。
func (s *Screen) actionButtonStyle() kit.ButtonStyle {
	l := s.Look
	t := l.T
	return kit.ButtonStyle{
		Height: l.Rem(2), Width: l.Rem(2), Radius: l.Rem(0.75),
		Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent.WithAlpha(0.6),
		FgOn: t.Primary, BgOn: t.Primary.WithAlpha(0.15),
		Ring: t.Primary, Transition: kit.DurationDefault,
	}
}

// detailActions 是头部右边的动作区：-mt-1 ml-auto flex shrink-0 items-center gap-0.5（按钮 32px、chip 24px，上提 4px 才与第一行 chip 对齐）。
func (s *Screen) detailActions(c ui.C, v View, item services.LibraryItem, caps ItemCaps, trashed bool, onClose func()) {
	l := s.Look
	t := l.T
	// ml-auto：与 chip 行同一行时 chip 行吃掉剩余空间，这一组贴着行尾；折到下一行时靠右（由外层 Flow 的 Justify(End) 承担，见 detailHeader）。
	ui.Row(c.Key("actions")).Shrink(0).Grow(0).MarginTop(-l.Space(1)).Items(ui.Center).Gap(l.Space(0.5)).Children(func() {
		if trashed {
			s.restoreButton(c, item)
		} else {
			s.liveActions(c, v, item, caps)
		}
		if onClose != nil {
			// mx-1 h-4 w-px bg-border，后面是关闭钮。
			ui.Row(c.Key("sep")).Size(1, l.Rem(1)).Shrink(0).MarginX(l.Space(1)).Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Border})
			})
			box := l.Button(c.Key("close"), kit.ButtonSpec{
				Icon: icons.X, IconSize: l.Rem(1), Center: true, Name: locales.LibraryCloseDetail(), Hint: locales.LibraryCloseDetail(),
			}, s.actionButtonStyle())
			if box.Clicked() {
				onClose()
			}
		}
	})
}

// restoreButton 是回收站条目唯一的动作：inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium
// text-primary-foreground hover:bg-primary/90。
func (s *Screen) restoreButton(c ui.C, item services.LibraryItem) {
	l := s.Look
	t := l.T
	box := l.Button(c.Key("restore"), kit.ButtonSpec{Label: locales.LibraryRestore(), Icon: icons.RotateCcw, IconSize: l.Rem(1)}, kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextSM, Weight: kit.WeightMedium,
		Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault,
	})
	if box.Clicked() {
		s.restore([]string{item.ID})
	}
}

// liveActions 是未删除条目的动作：提问、专注阅读、收藏、更多。
func (s *Screen) liveActions(c ui.C, v View, item services.LibraryItem, caps ItemCaps) {
	l := s.Look
	t := l.T
	if caps.AskAvailable {
		// mr-2 inline-flex h-8 items-center gap-1.5 rounded-lg px-2.5 text-xs font-medium；展开时 bg-primary/15 text-primary，
		// 否则 text-foreground hover:bg-accent/60。提问栏随 M3 迁入：点击说明归属（台账 B-060）。
		box := l.Button(c.Key("ask"), kit.ButtonSpec{
			Label: locales.ArticleAskAction(), Icon: icons.MessageCircle, IconSize: l.Rem(1), Name: locales.ArticleAskTitle(),
		}, kit.ButtonStyle{
			Height: l.Rem(2), PadX: l.Space(2.5), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightMedium,
			Fg: t.Foreground, BgHover: t.Accent.WithAlpha(0.6), Ring: t.Primary, Transition: kit.DurationDefault,
		}).MarginRight(l.Space(2))
		if box.Clicked() {
			s.notMigrated(locales.ArticleAskTitle(), "M3", "B-060")
		}
	}
	// 专注阅读：aria-pressed 的切换钮，图标随状态换。
	focus := s.detail.focusReading
	hint := locales.LibraryEnterFocusReading()
	ic := icons.Maximize2
	if focus {
		hint, ic = locales.LibraryExitFocusReading(), icons.Minimize2
	}
	if l.Button(c.Key("focus"), kit.ButtonSpec{Icon: ic, IconSize: l.Rem(1), Center: true, Name: hint, Hint: hint, Toggle: true, On: focus}, s.actionButtonStyle()).Clicked() {
		s.toggleFocusReading()
	}
	// 收藏：星标填充琥珀色。
	fav := locales.LibraryFavorite()
	if item.IsFavorite {
		fav = locales.LibraryUnfavorite()
	}
	favBox := l.Button(c.Key("favorite"), kit.ButtonSpec{Center: true, Name: fav, Hint: fav, Toggle: true, On: item.IsFavorite}, s.actionButtonStyle())
	favBox.Children(func() {
		if item.IsFavorite {
			l.IconFilled(c.Key("favorite-icon"), icons.Star, l.Rem(1), tokens.Tailwind("amber-400"))
		} else {
			l.Icon(c.Key("favorite-icon"), icons.Star, l.Rem(1), t.MutedForeground)
		}
	})
	if favBox.Clicked() {
		s.toggleFavorite(item.ID)
	}
	s.detailMore(c, item, caps)
}
