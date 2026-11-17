package imports

import (
	"strings"
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// fetchDebounce 是搜索词变化后重新读取列表的防抖（渲染层 ImportsWorkspace 的 150 ms 定时器）。
const fetchDebounce = 150 * time.Millisecond

// Workspace 是主区域（渲染层 ImportsWorkspace）：任务队列，或「采集文档站」。
func (s *Screen) Workspace(c ui.C) {
	if !s.mounted {
		// 挂载时读一次列表（渲染层 useEffect([fetchTasks, query]) 的首次执行）。
		s.mounted = true
		s.scheduleFetch()
	}
	if s.documents {
		s.documentSite(c)
		return
	}
	s.tasksWorkspace(c)
}

// scheduleFetch 防抖地重新读取列表：搜索词每变一次重置计时，停下 150 ms 后读一次。
func (s *Screen) scheduleFetch() {
	if s.queryStop != nil {
		s.queryStop()
	}
	s.queryStop = s.Store.After(fetchDebounce, func() { s.Store.Spawn(func() { s.Store.Fetch(s.Ctx) }) })
}

// syncQuery 让搜索输入框与 Store 里的搜索词一致：Store 一侧变了（「查看全部任务」重置了筛选）输入框跟着改；
// 用户在输入框里打字则推给 Store，并排一次防抖读取。
func (s *Screen) syncQuery(v View) {
	if v.Query != s.queryPushed {
		s.search.SetText(v.Query)
		s.queryPushed = v.Query
		return
	}
	if text := s.search.Text(); text != s.queryPushed {
		s.Store.SetQuery(text)
		s.queryPushed = text
		s.scheduleFetch()
	}
}

// adoptPending 取走后台预览出来的确认框内容（界面状态只在界面线程里改）。
func (s *Screen) adoptPending() {
	s.mu.Lock()
	pending := s.pendingClear
	s.pendingClear = nil
	s.mu.Unlock()
	if pending != nil {
		s.clear = pending
	}
}

// taskStats 是工作区从视图里算出来的几个量：选中的任务、失败的任务 id、进行中的数量。
type taskStats struct {
	selected  []services.ImportTask
	failedIDs []string
	active    int
}

func statsOf(v View) taskStats {
	var st taskStats
	for _, task := range v.Visible {
		if v.IsSelected(task.ID) {
			st.selected = append(st.selected, task)
		}
		if task.Status == services.ImportStatusFailed {
			st.failedIDs = append(st.failedIDs, task.ID)
		}
	}
	// 进行中的数量不受状态筛选影响（只跟搜索与来源走）：它是页头「共 N 条 · 进行中 M」里的 M。
	for _, task := range FilterTasks(v.Tasks, FilterAll, v.Query, v.Origin) {
		if IsActive(task) {
			st.active++
		}
	}
	return st
}

// tasksWorkspace 是任务队列页：页头、来源筛选、批量条（有选中时）、任务列表。
// flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section。
func (s *Screen) tasksWorkspace(c ui.C) {
	l := s.Look
	t := l.T
	s.adoptPending()
	v := s.Store.View()
	s.syncQuery(v)
	st := statsOf(v)
	now := s.nowTime().UnixMilli()
	s.rowHover.Begin()
	if st.active > 0 {
		// 有任务在跑时才起秒级 tick：行内的「已用时长」全靠它推进。
		c.Canvas().RequestFrameIn(time.Second)
	}
	if len(v.Selected) > 0 && ui.Shortcut(c, 0, "escape") {
		s.Store.ClearSelection() // 有选中时 Esc 取消选择
	}

	ui.Column(c.Key("workspace")).FillWidth().Grow(1).MinHeight(0).Clip().
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.UiSectionBg}) }).
		Children(func() {
			s.header(c, v, st)
			s.originFilter(c, v)
			if len(st.selected) > 0 {
				s.bulkBar(c, st.selected)
			}
			s.listArea(c, v, now)
		})
	s.clearDialog(c)
	s.detailDialog(c, v, now)
}

// headerButtonStyle 是页头按钮的外观：inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border border-border px-3 text-sm
// text-foreground transition-colors hover:bg-muted/60；最后一枚「清理所有来源终态」是 px-2 text-xs text-muted-foreground。
func (s *Screen) headerButtonStyle(quiet bool) kit.ButtonStyle {
	l := s.Look
	t := l.T
	st := kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextSM, Weight: kit.WeightNormal,
		Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationDefault,
	}
	if quiet {
		st.PadX, st.Text, st.Fg, st.FgHover = l.Space(2), kit.TextXS, t.MutedForeground, t.Foreground
	}
	return st
}

// header 是页头：标题、条数摘要、搜索框与一排批量动作。
// flex min-h-12 shrink-0 flex-wrap items-center gap-2 border-b border-border px-5 py-2。
func (s *Screen) header(c ui.C, v View, st taskStats) {
	l := s.Look
	t := l.T
	ui.Flow(c.Key("header")).FillWidth().Shrink(0).MinHeight(l.Rem(3)).Items(ui.Center).
		Gap(l.Space(2)).LineGap(l.Space(2)).
		PadSides(l.Space(2), l.Space(5), l.Space(2)+1, l.Space(5)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			l.Heading(c.Key("title"), 2, locales.ImportsTitle(), kit.TextSM, kit.WeightSemibold, t.Foreground).Shrink(0)
			// 队列读出来之前不报数：否则会先写「共 0 条」再跳成真实条数。
			if v.HasLoaded {
				summary := locales.ImportsSummaryTotal(v.Total)
				switch {
				case v.Queue.Paused:
					summary = locales.ImportsSummaryPaused(v.Queue.PendingCount)
				case st.active > 0:
					summary = locales.ImportsSummaryActive(v.Total, st.active)
				}
				l.Text(c.Key("summary"), summary, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).
					Shrink(1).MinWidth(0)
			}
			s.searchBox(c, v)
			s.headerButtons(c, v, st)
		})
}

// headerButtons 是搜索框右边的一排动作：重试失败（有失败时）、采集文档站、暂停/继续队列（队列里有任务或已暂停时）、
// 清理当前筛选、清理所有来源终态。
func (s *Screen) headerButtons(c ui.C, v View, st taskStats) {
	l := s.Look
	if n := len(st.failedIDs); n > 0 {
		b := l.Button(c.Key("retry-failed"), kit.ButtonSpec{Label: locales.ImportsRetryVisibleFailed(n), Icon: icons.RotateCcw}, s.headerButtonStyle(false))
		if b.Clicked() {
			ids := st.failedIDs
			s.Store.Spawn(func() { s.Store.RetryTasks(s.Ctx, ids) })
		}
	}
	if l.Button(c.Key("documents"), kit.ButtonSpec{Label: locales.WebCaptureImportSite(), Icon: icons.Globe}, s.headerButtonStyle(false)).Clicked() {
		s.documents = true
	}
	if v.Queue.PendingCount > 0 || v.Queue.Paused {
		label, icon := locales.ImportsPauseQueue(), icons.Pause
		if v.Queue.Paused {
			label, icon = locales.ImportsResumeQueue(), icons.Play
		}
		b := l.Button(c.Key("pause"), kit.ButtonSpec{Label: label, Icon: icon, Toggle: true, On: v.Queue.Paused}, s.headerButtonStyle(false))
		if b.Clicked() {
			s.Store.Spawn(func() { s.Store.TogglePaused(s.Ctx) })
		}
	}
	if l.Button(c.Key("clear-filtered"), kit.ButtonSpec{Label: locales.ImportsClearFilteredSource(), Icon: icons.Trash2}, s.headerButtonStyle(false)).Clicked() {
		s.requestClear(v, "filtered")
	}
	if l.Button(c.Key("clear-all"), kit.ButtonSpec{Label: locales.ImportsClearAllSources()}, s.headerButtonStyle(true)).Clicked() {
		s.requestClear(v, "all")
	}
}

// searchBox 是页头的搜索框：relative ml-auto w-56 min-w-[7rem] max-w-[14rem] flex-1，里面 h-8 的输入框、左边放大镜、
// 有内容时右边一枚清除按钮。输入框 rounded-lg border border-border bg-background/60 pl-8 pr-7 text-xs，聚焦时边框 primary/50。
//
// ml-auto（自动外边距）在 Viva 里没有对应物：CSS 先让 flex-1 的搜索框长到 max-w-[14rem]，剩下的空间才落到自动外边距上，
// 把搜索框与后面的按钮推到行尾。这里把搜索框放进一个吃掉整行剩余空间的外壳，外壳里先放一枚几乎不生长的空白，
// 搜索框长到上限之后剩余空间才轮到空白——效果相同；空白在外壳里，所以折行时它也跟着搜索框走，与自动外边距属于搜索框一致。
func (s *Screen) searchBox(c ui.C, v View) {
	l := s.Look
	height := l.Rem(2)
	ui.Row(c.Key("search-slot")).Grow(1).Shrink(1).Basis(0).MinWidth(l.Rem(7)).Height(height).Children(func() {
		ui.Spacer(c.Key("auto")).Grow(1e-6).Basis(0)
		s.searchField(c, v, height)
	})
}

// searchField 是搜索框本体（见 searchBox）。
func (s *Screen) searchField(c ui.C, v View, height float32) {
	l := s.Look
	t := l.T
	c = c.Key("search")
	ui.Stack(c).Grow(1).Shrink(1).Basis(0).MinWidth(l.Rem(7)).MaxWidth(l.Rem(14)).Height(height).Children(func() {
		// 底层的面板：边框颜色随聚焦在 150 ms 里补间。
		surfaceKey := c.Key("surface")
		surfaceID := ui.ID(surfaceKey)
		p := l.HoverProgress(c, surfaceID, 0, s.searchFocus, kit.DurationDefault)
		border := kit.MixToken(t.Border, t.Primary.WithAlpha(0.5), p)
		ui.Row(c).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.6), Border: border, BorderW: 1, Radius: l.Rem(0.75)})
		})

		placeholder := locales.ImportsSearchPlaceholder()
		input := l.Input(c.Key("input"), &s.search, placeholder, placeholder, kit.InputSpec{
			Size: l.Rem(0.75), Weight: kit.WeightNormal, Color: t.Foreground, Placeholder: t.MutedForeground.WithAlpha(0.6),
			Height: height, Left: l.Rem(2) + 1, Right: l.Rem(1.75) + 1,
		}).Absolute().Inset(0)
		s.searchFocus = input.State().Focused

		// 放大镜：pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground/60
		ui.Row(c.Key("icon")).Absolute().Left(l.Space(2.5)).Top(0).Bottom(0).Items(ui.Center).Children(func() {
			l.Icon(c, icons.Search, l.Rem(0.875), t.MutedForeground.WithAlpha(0.6))
		})
		if v.Query != "" {
			s.clearSearchButton(c)
		}
	})
}

// clearSearchButton 是搜索框右端的清除按钮：absolute right-1.5 top-1/2 flex h-5 w-5 -translate-y-1/2 rounded，图标 h-3 w-3。
func (s *Screen) clearSearchButton(c ui.C) {
	l := s.Look
	t := l.T
	label := locales.HeaderClearSearch()
	ui.Row(c.Key("clear")).Absolute().Right(l.Space(1.5)).Top(0).Bottom(0).Items(ui.Center).Children(func() {
		size := l.Rem(1.25)
		b := l.Button(c.Key("button"), kit.ButtonSpec{
			Name: label, Icon: icons.X, IconSize: l.Rem(0.75), Center: true, TranslateCentered: true, Hint: label,
		}, kit.ButtonStyle{
			Height: size, Width: size, Radius: l.Rem(0.25),
			Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationDefault,
		})
		if b.Clicked() {
			s.search.SetText("")
		}
	})
}

// originFilter 是提交来源筛选条：flex shrink-0 flex-wrap items-center gap-3 border-b border-border px-5 py-2，
// 里面一组 inline-flex gap-1 rounded-lg bg-muted/40 p-1 的切换按钮。
func (s *Screen) originFilter(c ui.C, v View) {
	l := s.Look
	t := l.T
	options := []struct{ value, label string }{
		{OriginAll, locales.ImportsOriginAll()},
		{OriginDesktop, locales.ImportsOriginDesktop()},
		{OriginMobile, locales.ImportsOriginMobile()},
	}
	ui.Flow(c.Key("origin")).FillWidth().Shrink(0).Items(ui.Center).Gap(l.Space(3)).
		PadSides(l.Space(2), l.Space(5), l.Space(2)+1, l.Space(5)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			name, hint := locales.ImportsOriginFilter(), locales.ImportsOriginScopeHint()
			gc := c.Key("group")
			ui.Row(gc).Shrink(0).Gap(l.Space(1)).Pad(l.Space(1)).
				Draw(func(d *ui.DrawContext) {
					l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.4), Radius: l.Rem(0.75)})
					// title 挂在容器上：指针在筛选组里任何一处（含各选项按钮）都出这条提示。
					l.HintGroup(d.Canvas, ui.ID(gc), d.Rect, name, hint)
				}).
				Children(func() {
					for _, opt := range options {
						// rounded-md px-3 py-1 text-xs transition-colors；选中 bg-primary/15 font-medium text-foreground，
						// 否则 text-muted-foreground hover:bg-muted/60 hover:text-foreground。
						b := l.Button(c.Key(opt.value), kit.ButtonSpec{Label: opt.label, Toggle: true, On: v.Origin == opt.value}, kit.ButtonStyle{
							PadX: l.Space(3), PadY: l.Space(1), Radius: l.Rem(0.75) - 2, Text: kit.TextXS, Weight: kit.WeightNormal,
							Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Muted.WithAlpha(0.6),
							FgOn: t.Foreground, BgOn: t.Primary.WithAlpha(0.15), WeightOn: kit.WeightMedium,
							Transition: kit.DurationDefault,
						})
						if b.Clicked() {
							s.Store.SetOrigin(s.Ctx, opt.value)
						}
					}
				})
		})
}

// listArea 是页头下面的滚动区：min-h-0 flex-1 overflow-y-auto px-5 py-3。里面按「读取中、读取失败、没有任何任务、筛选后为空、列表」
// 五选一——读取失败必须画成错误态，不能画成空态。
func (s *Screen) listArea(c ui.C, v View, now int64) {
	l := s.Look
	if v.HasLoaded {
		s.loading.Opacity(l, c.Canvas(), false) // 复位：下次再读取时重新计时
	}
	l.ScrollArea(c.Key("list"), [4]float32{l.Space(3), l.Space(5), l.Space(3), l.Space(5)}, func() {
		switch {
		case !v.HasLoaded:
			s.loadingState(c)
		case v.LoadError != "":
			if l.LoadErrorState(c, v.LoadError) {
				s.Store.Spawn(func() { s.Store.Fetch(s.Ctx) })
			}
		case len(v.Tasks) == 0 && v.Origin == OriginAll && v.Filter == FilterAll && strings.TrimSpace(v.Query) == "":
			s.emptyState(c)
		case len(v.Visible) == 0:
			s.filteredEmpty(c)
		default:
			s.taskList(c, v, now)
		}
	}).FillWidth().Grow(1).MinHeight(0)
}

// taskList 是任务行与底部的「加载更早的任务」：space-y-1.5。
func (s *Screen) taskList(c ui.C, v View, now int64) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("tasks")).FillWidth().Gap(l.Space(1.5)).Children(func() {
		for _, task := range v.Visible {
			s.taskRow(c, v, task, now)
		}
		if v.NextCursor == "" {
			return
		}
		// mt-3 被 space-y-1.5 的 margin-top 盖掉（后者选择器的优先级更高），所以它与上一行只隔 gap。
		// flex h-9 w-full items-center justify-center gap-2 rounded-lg border border-border text-sm text-muted-foreground
		// hover:bg-muted/60 disabled:opacity-50，没有 transition。
		b := l.Button(c.Key("load-more"), kit.ButtonSpec{
			Label: locales.ImportsLoadMoreHistory(), Full: true, Disabled: v.LoadingMore, Busy: v.LoadingMore, BusySize: l.Rem(0.875),
		}, kit.ButtonStyle{
			Height: l.Rem(2.25), Radius: l.Rem(0.75), Gap: l.Space(2), Text: kit.TextSM, Weight: kit.WeightNormal,
			Fg: t.MutedForeground, Border: t.Border, BorderW: 1, BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationNone,
		})
		if b.Clicked() {
			s.Store.Spawn(func() { s.Store.LoadMore(s.Ctx) })
		}
	})
}

// requestClear 先预览清理范围内会删除多少条终态任务，再打开确认框（渲染层 requestClear）。
// 预览是后台请求：失败按「操作失败」提示，成功后在下一帧由界面线程打开确认框。
func (s *Screen) requestClear(v View, scope string) {
	q := services.ImportClearQuery{Scope: scope, Origin: v.Origin, Status: string(v.Filter), Query: v.Query}
	label := locales.ImportsOriginAll()
	if scope != "all" {
		origin := locales.ImportsOriginAll()
		switch v.Origin {
		case OriginMobile:
			origin = locales.ImportsOriginMobile()
		case OriginDesktop:
			origin = locales.ImportsOriginDesktop()
		}
		label = origin + " \u00b7 " + locales.ImportsCurrentFilter()
	}
	s.Store.Spawn(func() {
		count, err := s.Store.PreviewClearTerminal(s.Ctx, q)
		if err != nil {
			s.Guard.Report(locales.ImportsClearPreviewFailed(), err)
			return
		}
		s.mu.Lock()
		s.pendingClear = &clearConfirm{query: q, label: label, count: count}
		s.mu.Unlock()
		s.Store.Wake()
	})
}

// clearDialog 是「清理采集任务历史」确认框：范围里没有可清理的任务时只剩一个「关闭」按钮。
func (s *Screen) clearDialog(c ui.C) {
	l := s.Look
	spec := kit.ConfirmSpec{Open: s.clear != nil, CancelText: locales.CommonCancel()}
	if s.clear != nil {
		spec.Title = locales.ImportsClearConfirmTitle()
		spec.Message = locales.ImportsClearConfirmSource(s.clear.label, s.clear.count)
		spec.ConfirmText = locales.CommonClose()
		if s.clear.count > 0 {
			spec.ConfirmText = locales.ImportsClearCount(s.clear.count)
			spec.Destructive = true
		}
	}
	switch l.ConfirmDialog(c.Key("clear-confirm"), &s.confirm, spec) {
	case kit.ConfirmAccepted:
		if confirm := s.clear; confirm != nil && confirm.count > 0 {
			s.Store.Spawn(func() { s.Store.ClearTerminal(s.Ctx, confirm.query) })
		}
		s.clear = nil
	case kit.ConfirmDismissed:
		s.clear = nil
	}
}
