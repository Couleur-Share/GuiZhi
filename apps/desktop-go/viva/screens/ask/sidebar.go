package ask

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// Panel 是二级侧栏里的内容（渲染层 SidebarAskPanel）：新对话钮、语义索引卡、历史会话的筛选与列表。
//
// 外层是 flex h-full min-h-0 flex-col overflow-y-auto px-2 pb-3：内容溢出时在这里滚动。
func (s *Screen) Panel(c ui.C) {
	l := s.Look
	s.enter()
	s.groups.Begin()
	s.syncSearch()
	v := s.Store.View()
	h := s.History.View()
	set := s.Setup.View()

	l.ScrollArea(c.Key("panel-scroll"), [4]float32{0, l.Space(2), l.Space(3), l.Space(2)}, func() {
		// pt-3：新对话钮与语义索引卡
		ui.Column(c.Key("top")).FillWidth().PadTop(l.Space(3)).Children(func() {
			s.newSessionButton(c)
			s.semanticCard(c, set)
		})
		s.sessionHeading(c)
		s.historyControls(c, h)
		s.sessionList(c, v, h)
	}).Grow(1).FillWidth().MinHeight(0)

	s.renameModal(c)
	s.deleteConfirm(c)
}

// newSessionButton 是「新对话」：flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-3 py-2 text-sm font-medium
// text-primary-foreground transition-colors hover:bg-primary/90。
func (s *Screen) newSessionButton(c ui.C) {
	l := s.Look
	t := l.T
	b := l.Button(c.Key("new"), kit.ButtonSpec{
		Label: locales.AskNewSession(), Icon: icons.MessageSquarePlus, Full: true, Center: true,
	}, kit.ButtonStyle{
		PadX: l.Space(3), PadY: l.Space(2), Radius: l.Rem(0.75), Gap: l.Space(2), Text: kit.TextSM, Weight: kit.WeightMedium,
		Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault,
	})
	if b.Clicked() {
		s.spawn(func() { s.Store.NewSession() })
	}
}

// semanticCard 是语义索引状态卡：仅在配置了 embedding 模型、且状态读到之后出现（渲染层 SemanticIndexCard）。
//
// mt-2 rounded-lg border border-sidebar-border/70 px-3 py-2：一行「语义索引 已索引/可索引」，
// 有待索引的条目（或正在索引）时下面一枚「索引 N 条新内容」按钮，索引中转圈并禁用。
func (s *Screen) semanticCard(c ui.C, set SetupView) {
	if !set.SemanticConfigured || !set.SemanticChecked {
		return
	}
	l := s.Look
	t := l.T
	status := set.Status
	pending := status.Pending()
	pad := func(n float32) float32 { return l.Space(n) + 1 } // 内边距加 1px 边框
	ui.Column(c.Key("semantic")).FillWidth().MarginTop(l.Space(2)).PadSides(pad(2), pad(3), pad(2), pad(3)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.SidebarBorder.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() {
			l.Text(c.Key("status"), locales.AskSemanticStatus(status.IndexedItems, status.EligibleItems),
				kit.TextXS, kit.WeightNormal, t.SidebarForeground.WithAlpha(0.6), kit.Wrapped())
			if pending <= 0 && !set.Indexing {
				return
			}
			spec := kit.ButtonSpec{
				Label: locales.AskSemanticRun(pending), Icon: icons.RefreshCw, IconSize: l.Rem(0.875), Full: true, Center: true,
			}
			if set.Indexing {
				spec = kit.ButtonSpec{
					Label: locales.AskSemanticIndexing(set.IndexedThisRun), Busy: true, BusySize: l.Rem(0.875),
					Full: true, Center: true, Disabled: true,
				}
			}
			// mt-1.5 flex w-full items-center justify-center gap-1.5 rounded-lg border border-sidebar-border px-3 py-1.5 text-xs
			// text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground disabled:opacity-60
			b := l.Button(c.Key("run"), spec, kit.ButtonStyle{
				PadX: l.Space(3), PadY: l.Space(1.5), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightNormal,
				Fg: t.SidebarForeground.WithAlpha(0.7), FgHover: t.SidebarForeground,
				BgHover: t.SidebarAccent.WithAlpha(0.5), Border: t.SidebarBorder, BorderW: 1,
				Transition: kit.DurationDefault, DisabledOpacity: 0.6,
			}).MarginTop(l.Space(1.5))
			if b.Clicked() {
				s.spawn(func() { s.Setup.RunIndexing() })
			}
		})
}

// sessionHeading 是「历史会话」分组标题：flex items-center gap-2 px-3 pb-1 pt-4，右边一条 h-px 的分隔线。
func (s *Screen) sessionHeading(c ui.C) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("heading")).FillWidth().Items(ui.Center).Gap(l.Space(2)).
		PadSides(l.Space(4), l.Space(3), l.Space(1), l.Space(3)).Children(func() {
		l.TextTracked(c.Key("label"), strings.ToUpper(locales.AskSessionList()), kit.TextXS, kit.WeightMedium,
			t.SidebarForeground.WithAlpha(0.8), 0.025)
		ui.Row(c.Key("rule")).Grow(1).Height(1).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.SidebarBorder.WithAlpha(0.6)})
		})
	})
}

// sessionList 是历史会话列表：读取失败的提示（带重试）、空态、会话行与「加载更早的会话」。
func (s *Screen) sessionList(c ui.C, v View, h HistoryView) {
	l := s.Look
	t := l.T
	if h.Error != "" {
		// p-2 text-xs text-destructive：文字后面紧跟一枚行内的「重试」按钮。
		ui.Flow(c.Key("history-error")).FillWidth().Items(ui.Center).Gap(l.Space(1)).LineGap(0).Pad(l.Space(2)).
			Draw(func(d *ui.DrawContext) { markAlert(d) }).
			Children(func() {
				l.Text(c.Key("text"), h.Error, kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped()).Shrink(1).MinWidth(0)
				if s.textButton(c.Key("retry"), locales.CommonRetry(), textBtn{Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.Destructive}) {
					s.spawn(func() { s.History.Load(false) })
				}
			})
	}
	if len(h.Entries) == 0 && !h.Loading && h.Error == "" {
		// px-3 py-1 text-xs text-sidebar-foreground/75
		ui.Column(c.Key("empty")).FillWidth().PadSides(l.Space(1), l.Space(3), l.Space(1), l.Space(3)).Children(func() {
			l.Text(c.Key("text"), locales.AskSessionListEmpty(), kit.TextXS, kit.WeightNormal,
				t.SidebarForeground.WithAlpha(0.75), kit.Wrapped())
		})
	} else {
		for _, entry := range h.Entries {
			s.sessionRow(c, v, entry)
		}
	}
	if h.HasMore {
		// m-2 rounded border border-border py-2 text-xs
		label := locales.AskLoadOlderSessions()
		if h.Loading {
			label = locales.CommonLoading()
		}
		ui.Column(c.Key("more-wrap")).FillWidth().Pad(l.Space(2)).Children(func() {
			b := l.Button(c.Key("more"), kit.ButtonSpec{Label: label, Full: true, Center: true, Disabled: h.Loading}, kit.ButtonStyle{
				PadY: l.Space(2), Radius: l.Rem(0.25), Text: kit.TextXS, Weight: kit.WeightNormal,
				Fg: t.SidebarForeground, Border: t.Border, BorderW: 1, DisabledOpacity: 0.5,
			})
			if b.Clicked() {
				s.spawn(func() { s.History.Load(true) })
			}
		})
	}
}

// sessionTitle 是会话行的标题：本文会话先写「本文问答 · 文章标题 · 」，置顶的写「置顶 · 」，空标题落到「新对话」。
func sessionTitle(e services.AskSessionMeta) string {
	var b strings.Builder
	if e.Scope == services.AskScopeArticle {
		b.WriteString(locales.ArticleAskHistoryBadge() + " · " + e.ArticleTitle + " · ")
	}
	if e.Pinned {
		b.WriteString(locales.AskSessionPinned() + " · ")
	}
	b.WriteString(sessionBareTitle(e))
	return b.String()
}

// sessionTitleRuns 画会话行的标题：一个 truncate 的 span 里相邻的文本节点（渲染层 SidebarAskPanel：本文前缀、置顶前缀、标题，
// 空串不成节点），可访问树与文字行按节点各记一条，所以拆成同样的几段；整行视觉上是一行，只在末端截断。
// 按上一帧量到的行宽依次排：前缀放得下就是一段定宽的文字（宽度取自然字宽，含行尾空格——与 kit.TextRuns 同口径），
// 放不下就由这一段带省略号收尾、后面的段（含标题）都被截掉不画（渲染层的文字行只收看得见的节点）；前缀都放得下，标题占剩下的宽度。
// 行宽还没量到（首帧）按放得下排。
func (s *Screen) sessionTitleRuns(c ui.C, e services.AskSessionMeta, fg tokens.Color) {
	l := s.Look
	var prefixes []string
	if e.Scope == services.AskScopeArticle {
		prefixes = append(prefixes, locales.ArticleAskHistoryBadge()+" · "+e.ArticleTitle+" · ")
	}
	if e.Pinned {
		prefixes = append(prefixes, locales.AskSessionPinned()+" · ")
	}
	canvas := c.Canvas()
	size := l.Rem(kit.TextSM.Size)
	left := s.titleWidth
	for i, run := range prefixes {
		w := canvas.TextWidthWeight(run, size, kit.WeightNormal)
		if left > 0 && w > left {
			// Chromium 把行尾空格算进文字宽度再判断截不截；Viva 的 Label 量宽时不计行尾的 ASCII 空格（trimSpaceRight），所以把它换成同宽的不换行空格（U+00A0）。
			pad := strings.Repeat("\u00a0", len(run)-len(strings.TrimRight(run, " ")))
			l.Text(c.Key("prefix"+strconv.Itoa(i)), strings.TrimRight(run, " ")+pad, kit.TextSM, kit.WeightNormal, fg, kit.Ellipsis()).Shrink(1).MinWidth(0)
			return
		}
		l.TextRuns(c.Key("prefix"+strconv.Itoa(i)), kit.TextSM, kit.WeightNormal, fg, run).Shrink(0)
		left -= w
	}
	l.Text(c.Key("title"), sessionBareTitle(e), kit.TextSM, kit.WeightNormal, fg, kit.Ellipsis()).Shrink(1).MinWidth(0)
}

// sessionBareTitle 是不带前缀的标题，空标题落到「新对话」。
func sessionBareTitle(e services.AskSessionMeta) string {
	if e.Title != "" {
		return e.Title
	}
	return locales.AskUntitledSession()
}

// sessionRow 是一行历史会话：group relative w-full py-0.5。
// 主按钮（标题与时间）切换到该会话；悬停或焦点在行内时右上角浮出删除钮（hidden group-hover:flex），
// 下面一行「重命名 / 置顶」在行内悬停或聚焦时才显现（opacity-0 group-hover:opacity-100，按钮一直在可访问树里）。
func (s *Screen) sessionRow(c ui.C, v View, e services.AskSessionMeta) {
	l := s.Look
	group := "session:" + e.ID
	active := e.ID == v.ActiveSessionID
	c = c.Key("session-" + e.ID)
	hot := s.groups.Active(c.Canvas(), group)

	ui.Column(c).FillWidth().PadSides(l.Space(0.5), 0, l.Space(0.5), 0).Children(func() {
		s.sessionMain(c, e, active, group)
		if hot {
			s.sessionDelete(c, e, active, group)
		}
		s.sessionActions(c, e, hot, group)
	})
}

// sessionMain 是会话行的主按钮：flex w-full flex-col items-start gap-0.5 rounded-lg px-3 py-2 text-left transition-all duration-smooth；
// 选中 bg-primary/15 text-foreground shadow-sm，否则文字 sidebar-foreground/80，悬停 bg-sidebar-accent/50 text-sidebar-foreground。
func (s *Screen) sessionMain(c ui.C, e services.AskSessionMeta, active bool, group string) {
	l := s.Look
	t := l.T
	c = c.Key("main")
	id := ui.ID(c)
	s.groups.Add(group, id)
	title := sessionTitle(e)
	when := kit.FormatItemTime(e.UpdatedAt, s.nowTime(), s.loc(), s.Locale)

	box := ui.Column(c).FillWidth().Items(ui.Start).Gap(l.Space(0.5)).
		PadSides(l.Space(2), l.Space(3), l.Space(2), l.Space(3)).
		HitWith(id, title+" "+when, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	ph := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationSmooth)
	pa := l.HoverProgress(c, id, 1, active, kit.DurationSmooth)
	bg := kit.MixToken(kit.MixToken(tokens.Transparent, t.SidebarAccent.WithAlpha(0.5), ph), t.Primary.WithAlpha(0.15), pa)
	fg := kit.MixToken(kit.MixToken(t.SidebarForeground.WithAlpha(0.8), t.SidebarForeground, ph), t.Foreground, pa)
	timeFg := kit.MixToken(t.SidebarForeground.WithAlpha(0.8), t.Foreground.WithAlpha(0.8), pa)

	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.75))
		}
		surf := kit.Surface{Bg: bg, Radius: l.Rem(0.75)}
		if pa > 0 {
			surf.Shadows = kit.ShadowSM
		}
		l.PaintSurface(d, surf)
	}).Children(func() {
		// w-full truncate pr-5 text-sm
		ui.Row(c.Key("title-wrap")).FillWidth().PadRight(l.Space(5)).Draw(func(d *ui.DrawContext) { s.titleWidth = d.Rect.Width - l.Space(5) }).Children(func() {
			s.sessionTitleRuns(c, e, fg)
		})
		l.Text(c.Key("time"), when, kit.TextXS, kit.WeightNormal, timeFg)
	})
	if box.Clicked() {
		id := e.ID
		s.spawn(func() { s.Store.SwitchSession(id) })
	}
}

// sessionDelete 是行内浮出的删除钮：absolute right-1.5 top-2 flex h-5 w-5 items-center justify-center rounded transition-colors；
// 选中行 text-foreground/80 hover:bg-primary/10 hover:text-foreground，其余 text-sidebar-foreground/75 hover:bg-sidebar-accent hover:text-destructive。
func (s *Screen) sessionDelete(c ui.C, e services.AskSessionMeta, active bool, group string) {
	l := s.Look
	t := l.T
	size := l.Rem(1.25)
	st := kit.ButtonStyle{
		Height: size, Width: size, Radius: l.Rem(0.25), Transition: kit.DurationDefault,
		Fg: t.SidebarForeground.WithAlpha(0.75), FgHover: t.Destructive, BgHover: t.SidebarAccent,
	}
	if active {
		st.Fg, st.FgHover, st.BgHover = t.Foreground.WithAlpha(0.8), t.Foreground, t.Primary.WithAlpha(0.1)
	}
	b := l.Button(c.Key("delete"), kit.ButtonSpec{
		Name: locales.AskDeleteSession(), Hint: locales.AskDeleteSession(), Icon: icons.X, IconSize: l.Rem(0.875),
		Center: true, Groups: &s.groups, Group: group,
	}, st).Absolute().Top(l.Space(2)).Right(l.Space(1.5))
	if b.Clicked() {
		entry := e
		s.deleting = &entry
	}
}

// sessionActions 是会话行下面的「重命名 / 置顶」：flex gap-3 px-3 text-[11px] text-muted-foreground，行内悬停或聚焦时才显现。
func (s *Screen) sessionActions(c ui.C, e services.AskSessionMeta, hot bool, group string) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("actions")).FillWidth().Gap(l.Space(3)).PadSides(0, l.Space(3), 0, l.Space(3)).Children(func() {
		base := textBtn{Px: 11, Line: 16.5, Weight: kit.WeightNormal, Fg: t.MutedForeground, Hidden: !hot, Groups: &s.groups, Group: group}
		if s.textButton(c.Key("rename"), locales.LibraryRename(), base) {
			entry := e
			s.rename.session = &entry
			s.rename.field.SetText(entry.Title)
		}
		pinLabel := locales.LibraryPin()
		if e.Pinned {
			pinLabel = locales.LibraryUnpin()
		}
		if s.textButton(c.Key("pin"), pinLabel, base) {
			id, pinned := e.ID, !e.Pinned
			s.spawn(func() { s.History.Update(id, services.AskSessionMetaPatch{Pinned: &pinned}) })
		}
	})
}

// deleteConfirm 是删除会话的确认框；确认后删除并刷新历史列表（渲染层 deleteSession(...).then(load)）。
func (s *Screen) deleteConfirm(c ui.C) {
	l := s.Look
	spec := kit.ConfirmSpec{
		ConfirmText: locales.CommonConfirm(), CancelText: locales.CommonCancel(), Destructive: true,
		Title: locales.AskDeleteSession(),
	}
	if s.deleting != nil {
		spec.Open = true
		spec.Message = locales.AskDeleteSessionConfirm(s.deleting.Title)
	}
	switch l.ConfirmDialog(c.Key("delete-confirm"), &s.confirm, spec) {
	case kit.ConfirmAccepted:
		if target := s.deleting; target != nil {
			id := target.ID
			// 删除成功后 Store 会经 OnSaved 通知刷新历史列表，这里不再重复读。
			s.spawn(func() { s.Store.DeleteSession(id) })
		}
		s.deleting = nil
	case kit.ConfirmDismissed:
		s.deleting = nil
	}
}
