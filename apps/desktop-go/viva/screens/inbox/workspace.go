package inbox

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// Workspace 是主区域（渲染层 InboxWorkspace）：页头、批量条（有选中时）与待办列表。
//
// flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section。
func (s *Screen) Workspace(c ui.C) {
	l := s.Look
	t := l.T
	s.enter()
	v := s.Store.View()
	s.syncTags(v)
	if s.themedTasks && s.Hooks.ThemedTasks != nil {
		// 主题页任务页整个代替工作区（渲染层 `showThemedTasks ? <ThemedReadingTasks/> : …`）。
		ui.Column(c.Key("workspace-themed")).FillWidth().Grow(1).MinHeight(0).Clip().
			Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.UiSectionBg}) }).
			Children(func() { s.Hooks.ThemedTasks(c, func() { s.themedTasks = false }) })
		return
	}
	ui.Column(c.Key("workspace")).FillWidth().Grow(1).MinHeight(0).Clip().
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.UiSectionBg}) }).
		Children(func() {
			s.header(c, v)
			if len(v.Selection) > 0 {
				s.bulkBar(c, v)
			} else {
				s.collection.Open = false
			}
			s.body(c, v)
		})
}

// syncTags 让标签输入框与 Store 里的标签文本一致：Store 一侧变了（批量整理成功后清空）输入框跟着改；
// 用户在输入框里打字则推给 Store。
func (s *Screen) syncTags(v View) {
	if v.OrganizeTagText != s.tagsPushed {
		s.tags.SetText(v.OrganizeTagText)
		s.tagsPushed = v.OrganizeTagText
		return
	}
	if text := s.tags.Text(); text != s.tagsPushed {
		s.Store.SetOrganizeTagText(text)
		s.tagsPushed = text
	}
}

// pad 是内边距加 1px 边框（border-box）。
func (s *Screen) pad(n float32) float32 { return s.Look.Space(n) + 1 }

// countText 是页头的摘要：「N 条内容待处理」，Wiki 与语义索引两组换成各自的量词。
func countText(v View) string {
	n := v.HeaderCount()
	switch v.Filter {
	case FilterWikiPending:
		return locales.InboxCountWikiPending(n)
	case FilterSemanticPending:
		return locales.InboxCountSemanticPending(n)
	}
	return locales.InboxCountPending(n)
}

// header 是页头：flex h-12 shrink-0 items-center gap-3 border-b border-border px-5。
// 标题、「主题页任务」、条数摘要，然后是推到行尾的全选框（有可选行时）与刷新钮。
func (s *Screen) header(c ui.C, v View) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("header")).FillWidth().Shrink(0).Height(l.Rem(3)).Items(ui.Center).Gap(l.Space(3)).
		PadSides(0, l.Space(5), 1, l.Space(5)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			l.Heading(c.Key("title"), 2, locales.NavInbox(), kit.TextSM, kit.WeightSemibold, t.Foreground).Shrink(0)
			s.themedTasksButton(c)
			l.Text(c.Key("summary"), countText(v), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).Shrink(1).MinWidth(0)
			// ml-auto：把后面的全选框与刷新钮推到行尾。
			ui.Spacer(c.Key("auto")).Grow(1)
			if len(v.SelectableVisibleIDs) > 0 {
				checked := v.AllVisibleSelected()
				before := checked
				l.Checkbox(c.Key("select-all"), &checked, kit.CheckboxSpec{
					Label: locales.InboxSelectAllVisible(), Indeterminate: v.SomeVisibleSelected(),
				})
				if checked != before {
					s.Store.SetAllVisibleSelected(checked)
				}
			}
			s.refreshButton(c, v.IsLoading)
		})
}

// themedTasksButton 是「主题页任务」入口：rounded-lg border border-border px-2.5 py-1 text-xs hover:bg-accent。
// 接了主题阅读（Hooks.ThemedTasks，台账 B-064）就打开任务页；没接时点击弹一条说明归属的提示，不崩溃、不假装有内容。
func (s *Screen) themedTasksButton(c ui.C) {
	l := s.Look
	t := l.T
	label := locales.ThemedReadingTasks()
	b := l.Button(c.Key("themed"), kit.ButtonSpec{Label: label}, kit.ButtonStyle{
		PadX: l.Space(2.5), PadY: l.Space(1), Radius: l.Rem(0.75), Text: kit.TextXS, Weight: kit.WeightNormal,
		Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Accent, Transition: kit.DurationDefault,
	})
	if b.Clicked() {
		if s.Hooks.ThemedTasks != nil {
			s.themedTasks = true
			return
		}
		s.notMigrated(label, themedTasksMilestone, themedTasksLedger)
	}
}

// refreshButton 是页头的刷新钮：rounded-lg p-2 text-muted-foreground hover:bg-muted hover:text-foreground，
// 里面 h-4 w-4 的 RefreshCw，读取期间转圈（animate-spin）。可访问名是「刷新处理中心」，悬停提示是「刷新」
// （title 属性：名称来自 aria-label 时 Chromium 把它报成可访问描述，所以 Hint 与 Description 都给）。
func (s *Screen) refreshButton(c ui.C, loading bool) {
	l := s.Look
	t := l.T
	c = c.Key("refresh")
	id := ui.ID(c)
	box := ui.Row(c).Shrink(0).Items(ui.Center).Justify(ui.Center).Pad(l.Space(2)).
		HitWith(id, locales.InboxRefresh(), gfx.HitOptions{Role: gfx.RoleButton, Hint: locales.CommonRefresh(), Description: locales.CommonRefresh(), Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	p := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	bg := kit.MixToken(tokens.Transparent, t.Muted, p)
	fg := kit.MixToken(t.MutedForeground, t.Foreground, p)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.75))
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Radius: l.Rem(0.75)})
	}).Children(func() {
		if loading {
			l.SpinIcon(c.Key("icon"), icons.RefreshCw, l.Rem(1), fg)
		} else {
			l.Icon(c.Key("icon"), icons.RefreshCw, l.Rem(1), fg)
		}
	})
	if box.Clicked() {
		s.refresh()
	}
}

// bulkBar 是批量条：flex shrink-0 flex-wrap items-center gap-2 border-b border-border bg-primary/5 px-5 py-2。
// 已选条数、归入合集、标签、批量整理、AI 智能归类（选中里有未归合集的条目时）、取消。
func (s *Screen) bulkBar(c ui.C, v View) {
	l := s.Look
	t := l.T
	ui.Flow(c.Key("bulk")).FillWidth().Shrink(0).Items(ui.Center).Gap(l.Space(2)).LineGap(l.Space(2)).
		PadSides(l.Space(2), l.Space(5), s.pad(2), l.Space(5)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.05)})
			l.PaintEdge(d, kit.EdgeBottom, 1, t.Border)
		}).
		Children(func() {
			l.Text(c.Key("count"), locales.InboxSelectedCount(len(v.Selection)), kit.TextSM, kit.WeightNormal, t.Foreground)
			s.collectionSelect(c, v)
			s.tagsInput(c)
			s.organizeButtons(c, v)
		})
}

// collectionSelect 是「归入合集」下拉：w-44，选项是「图标 名称」，没有图标的合集用 📚。
func (s *Screen) collectionSelect(c ui.C, v View) {
	l := s.Look
	var options []kit.SelectOption
	if s.Hooks.Collections != nil {
		for _, col := range s.Hooks.Collections() {
			icon := col.Icon
			if icon == "" {
				icon = defaultCollectionIcon
			}
			options = append(options, kit.SelectOption{Value: col.ID, Label: icon + " " + col.Name})
		}
	}
	label := locales.InboxMoveToCollection()
	picked, changed := l.Select(c.Key("collection"), &s.collection, kit.SelectSpec{
		Label: label, Value: v.OrganizeCollectionID, Options: options, Width: l.Rem(11), Placeholder: label,
	})
	if changed {
		s.Store.SetOrganizeCollection(picked)
	}
}

// tagsInput 是标签输入框：h-9 w-52 rounded-lg border border-border bg-background px-3 text-sm，聚焦时边框 primary/50。
// 渲染层没有给占位符写颜色类，用的是 Tailwind preflight 的 input::placeholder（gray-400，两种主题相同），不是 muted-foreground。
func (s *Screen) tagsInput(c ui.C) {
	l := s.Look
	t := l.T
	height := l.Rem(2.25)
	c = c.Key("tags")
	ui.Stack(c).Shrink(0).Width(l.Rem(13)).Height(height).Children(func() {
		// 底层的面板：边框颜色随聚焦在 150 ms 里补间。
		surfaceID := ui.ID(c.Key("surface"))
		p := l.HoverProgress(c, surfaceID, 0, s.tagsFocus, kit.DurationDefault)
		border := kit.MixToken(t.Border, t.Primary.WithAlpha(0.5), p)
		ui.Row(c).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background, Border: border, BorderW: 1, Radius: l.Rem(0.75)})
		})
		placeholder := locales.InboxAddTagsPlaceholder()
		input := l.Input(c.Key("input"), &s.tags, placeholder, placeholder, kit.InputSpec{
			Size: l.Rem(0.875), Weight: kit.WeightNormal, Color: t.Foreground, Placeholder: tokens.Tailwind("gray-400"),
			Height: height, Left: l.Rem(0.75) + 1, Right: l.Rem(0.75) + 1,
		}).Absolute().Inset(0)
		s.tagsFocus = input.State().Focused
	})
}

// organizeButtons 是批量条里的三枚动作：批量整理、AI 智能归类 / 停止归类、取消。都是 h-9 text-sm。
func (s *Screen) organizeButtons(c ui.C, v View) {
	l := s.Look
	t := l.T
	base := kit.ButtonStyle{
		Height: l.Rem(2.25), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextSM,
		Transition: kit.DurationDefault,
	}

	// inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground
	organize := base
	organize.Weight, organize.Fg, organize.Bg = kit.WeightMedium, t.PrimaryForeground, t.Primary
	if l.Button(c.Key("organize"), kit.ButtonSpec{Label: locales.InboxOrganizeSelected(), Icon: icons.FolderInput}, organize).Clicked() {
		s.Store.Spawn(func() { s.Store.ApplyOrganization(s.Ctx) })
	}

	if len(v.SelectedUnclassified) > 0 {
		// border border-primary/30 font-medium text-primary hover:bg-primary/10；归类中换成转圈与「停止归类 已完成/总批数」，再点一次就是取消。
		smart := base
		smart.Weight, smart.Fg = kit.WeightMedium, t.Primary
		smart.Border, smart.BorderW, smart.BgHover = t.Primary.WithAlpha(0.3), 1, t.Primary.WithAlpha(0.1)
		spec := kit.ButtonSpec{Label: locales.InboxSmartClassify(len(v.SelectedUnclassified)), Icon: icons.Sparkles}
		if v.Classifying {
			spec = kit.ButtonSpec{
				Label: locales.InboxStopClassify(v.Classify.Completed, v.Classify.Total), Busy: true,
			}
		}
		if l.Button(c.Key("smart"), spec, smart).Clicked() {
			if v.Classifying {
				// 停止归类：StopSmartClassify 只加锁调 cancel() 就返回、不阻塞，同步调用才能立刻取消；
				// 丢进后台 goroutine 会让取消与正在收尾的批次竞态，导致取消后仍写入分类。
				// 不用 ToggleSmartClassify：帧初的 v.Classifying 可能已过期（归类刚自然结束），
				// Toggle 会落入启动分支在 UI 线程同步跑完整轮归类；Stop 在此时是 no-op。
				s.Store.StopSmartClassify()
			} else {
				s.Store.Spawn(func() { s.Store.ToggleSmartClassify(s.Ctx) }) // 开始归类要等模型批次，放后台
			}
		}
	}

	// h-9 rounded-lg px-3 text-sm text-muted-foreground hover:bg-muted
	cancel := base
	cancel.Weight, cancel.Fg, cancel.BgHover = kit.WeightNormal, t.MutedForeground, t.Muted
	if l.Button(c.Key("cancel"), kit.ButtonSpec{Label: locales.CommonCancel()}, cancel).Clicked() {
		s.Store.ClearSelection()
	}
}
