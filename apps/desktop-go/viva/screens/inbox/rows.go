package inbox

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// defaultCollectionIcon 是没有图标的合集在下拉里用的 emoji（渲染层 collection.icon ?? "📚"）。
const defaultCollectionIcon = "\U0001F4DA"

// reasonSeparator 是复核原因之间的分隔：全角分号（渲染层 reasons.join 的分隔符，不随界面语言变）。写成转义：它是分隔符不是文案。
const reasonSeparator = "\uFF1B"

// body 是页头与批量条下面的滚动区：min-h-0 flex-1 overflow-y-auto p-5。
// 里面先是 Wiki 提示条与局部错误条（不阻塞主列表），再按「读取失败、读取中、已处理完、列表」四选一画主体——
// 读取失败必须画成错误态，不能画成「已处理完」的空态。
//
// 列表不做行窗口化：服务端每组最多回 100 条，三组加两个聚合行不超过 302 行，整表声明的开销与采集页的几十行同量级。
func (s *Screen) body(c ui.C, v View) {
	l := s.Look
	l.ScrollArea(c.Key("body"), [4]float32{l.Space(5), l.Space(5), l.Space(5), l.Space(5)}, func() {
		s.wikiBanner(c, v)
		s.sectionErrors(c, v)
		switch {
		case v.LoadError != "":
			if l.LoadErrorState(c.Key("load-error"), v.LoadError) {
				s.refresh()
			}
		case !v.HasLoaded || (v.IsLoading && len(v.Items) == 0):
			// 第一次读取之前（含挂载后、读取开始前的那一帧）也画转圈，不先闪一下「已处理完」。
			s.loadingState(c)
		case len(v.Visible) == 0:
			s.emptyState(c)
		default:
			s.itemList(c, v)
		}
	}).FillWidth().Grow(1).MinHeight(0)
}

// loadingState 是读取中的指示：flex h-32 items-center justify-center，里面一枚 sm 的 muted 转圈。
func (s *Screen) loadingState(c ui.C) {
	l := s.Look
	ui.Row(c.Key("loading")).FillWidth().Height(l.Rem(8)).Justify(ui.Center).Items(ui.Center).Children(func() {
		l.Spinner(c, l.Rem(1), l.T.MutedForeground)
	})
}

// emptyState 是当前分组已处理完：flex h-52 flex-col items-center justify-center text-center。
func (s *Screen) emptyState(c ui.C) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("empty")).FillWidth().Height(l.Rem(13)).Justify(ui.Center).Items(ui.Center).Children(func() {
		l.Icon(c.Key("icon"), icons.Check, l.Rem(2.5), tokens.Tailwind("emerald-500"))
		l.Text(c.Key("text"), locales.InboxGroupDone(), kit.TextSM, kit.WeightMedium, t.Foreground, kit.Centered()).MarginTop(l.Space(3))
	})
}

// notice 是列表上方的提示条：mb-2 rounded-lg border border-border p-3 text-sm，里面一段文字加一枚行内的文字按钮。
// 文字按钮（text-primary hover:underline）紧跟在文字后面，放不下时折到下一行。alert 为真时它是带直播属性的区域（role="alert"）。
func (s *Screen) notice(c ui.C, key, text, action string, alert bool, onAction func()) {
	l := s.Look
	t := l.T
	ui.Flow(c.Key(key)).FillWidth().Items(ui.Center).Gap(l.Space(1)).Pad(s.pad(3)).MarginBottom(l.Space(2)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
			if alert {
				// alert 在可访问树里是一个带直播属性的区域；Viva 没有 alert 角色，用 status + assertive 表达（台账 A-012）。
				d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LiveAssertive})
			}
		}).
		Children(func() {
			l.Text(c.Key("text"), text, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped()).Shrink(1).MinWidth(0)
			if s.linkButton(c.Key("action"), action) {
				onAction()
			}
		})
}

// linkButton 是行内的文字按钮：text-sm text-primary hover:underline。返回这一帧是否被点了。
func (s *Screen) linkButton(c ui.C, label string) bool {
	l := s.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Row(c).Shrink(0).HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	hovered := box.State().Hovered
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.25))
		}
	}).Children(func() {
		l.Text(c.Key("text"), label, kit.TextSM, kit.WeightNormal, t.Primary, kit.Underline(hovered))
	})
	return box.Clicked()
}

// wikiBanner 是 Wiki 提示条：模型没配好且有候选在等时，说明原因并给出配置入口；否则有候选时给出各状态的条数与查看入口。
func (s *Screen) wikiBanner(c ui.C, v View) {
	if v.WikiNeedsModel {
		s.notice(c, "wiki-needs-model", locales.InboxWikiNeedsModel(v.Wiki.Waiting), locales.InboxWikiConfigureModel(), false, func() {
			if open := s.Store.OnOpenSettingsAI; open != nil {
				open()
			}
		})
		return
	}
	if v.Wiki == nil {
		return
	}
	parts := wikiParts(*v.Wiki)
	if len(parts) == 0 {
		return
	}
	s.notice(c, "wiki-candidates", locales.InboxWikiCandidates(strings.Join(parts, locales.InboxNameSeparator())), locales.InboxWikiCandidatesView(), false, s.Store.RunWiki)
}

// wikiParts 是 Wiki 候选各状态的文字，条数为 0 的不出现。
func wikiParts(w services.InboxWikiCandidates) []string {
	var parts []string
	if w.Ready > 0 {
		parts = append(parts, locales.InboxWikiPartReady(w.Ready))
	}
	if w.Review > 0 {
		parts = append(parts, locales.InboxWikiPartReview(w.Review))
	}
	if w.Waiting > 0 {
		parts = append(parts, locales.InboxWikiPartWaiting(w.Waiting))
	}
	if w.Upgrade > 0 {
		parts = append(parts, locales.InboxWikiPartUpgrade(w.Upgrade))
	}
	return parts
}

// sectionErrors 是 Wiki 状态与语义索引状态读取失败的局部错误条：role="alert"，带重试，不阻塞主列表。
func (s *Screen) sectionErrors(c ui.C, v View) {
	if msg := v.SectionErrors.Wiki; msg != "" {
		s.notice(c, "wiki-error", locales.InboxWikiStatusFailed(msg), locales.CommonRetry(), true, s.refresh)
	}
	if msg := v.SectionErrors.Semantic; msg != "" {
		s.notice(c, "semantic-error", locales.InboxSemanticStatusFailed(msg), locales.CommonRetry(), true, s.refresh)
	}
}

// itemList 是卡片列表：space-y-2。聚合行（语义索引、Wiki）是整块的大卡片，其余是带勾选框的条目卡片。
func (s *Screen) itemList(c ui.C, v View) {
	l := s.Look
	selected := make(map[string]bool, len(v.Selection))
	for _, id := range v.Selection {
		selected[id] = true
	}
	ui.Column(c.Key("list")).FillWidth().Gap(l.Space(2)).Children(func() {
		for _, item := range v.Visible {
			if item.IsAggregate() {
				s.aggregateCard(c, v, item)
				continue
			}
			s.itemCard(c, v, item, selected)
		}
	})
}

// itemKey 是一行的稳定键：优先用条目自己的 ID，没有时回退到序号之外的字段组合。
func itemKey(item services.InboxItem) string {
	if item.ID != "" {
		return item.ID
	}
	return item.Kind + ":" + item.ItemID + ":" + item.TaskID
}

// aggregateCard 是后台任务的聚合行：flex items-center gap-3 rounded-xl border border-primary/20 bg-primary/5 p-4。
func (s *Screen) aggregateCard(c ui.C, v View, item services.InboxItem) {
	l := s.Look
	t := l.T
	c = c.Key("agg-" + itemKey(item))
	semantic := item.Kind == services.InboxKindSemanticPending
	icon, title, desc := icons.Network, locales.InboxWikiAggregateTitle(), locales.InboxWikiAggregateDesc(item.Count)
	if semantic {
		icon, title, desc = icons.ScanSearch, locales.InboxSemanticAggregateTitle(), locales.InboxSemanticAggregateDesc(item.Count)
	}
	ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(3)).Pad(s.pad(4)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.05), Border: t.Primary.WithAlpha(0.2), BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() {
			l.Icon(c.Key("icon"), icon, l.Rem(1.25), t.Primary)
			ui.Column(c.Key("body")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				l.Text(c.Key("title"), title, kit.TextSM, kit.WeightMedium, t.Foreground, kit.Wrapped())
				l.Text(c.Key("desc"), desc, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			})
			s.aggregateButton(c, v, item, semantic)
		})
}

// aggregateButton 是聚合行的「立即执行」：inline-flex h-8 min-w-20 justify-center gap-1.5 rounded-lg border border-border px-3 text-xs
// hover:bg-muted disabled:opacity-60。任何一个聚合行在执行时，两个按钮都禁用；正在执行的那个换成转圈与进行时文字。
func (s *Screen) aggregateButton(c ui.C, v View, item services.InboxItem, semantic bool) {
	l := s.Look
	t := l.T
	running := v.Running == RunningKind(item.Kind)
	spec := kit.ButtonSpec{Label: locales.InboxRunNow(), Center: true, Disabled: v.Running != RunningNone}
	if running {
		label := locales.WikiCompilingShort()
		if semantic {
			label = locales.InboxIndexing()
		}
		spec.Label, spec.Busy, spec.BusySize = label, true, l.Rem(0.875)
	}
	b := l.Button(c.Key("run"), spec, kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightNormal,
		Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Muted, Transition: kit.DurationDefault,
		DisabledOpacity: 0.6,
	}).MinWidth(l.Rem(5))
	if !b.Clicked() {
		return
	}
	if semantic {
		s.Store.Spawn(func() { s.Store.RunSemanticNow(s.Ctx) })
		return
	}
	s.Store.RunWiki()
}

// itemCard 是一条待处理的内容：flex items-start gap-3 rounded-xl border border-border bg-background/60 p-4。
// 左边是勾选框（知识条目）或警示图标（采集问题），中间是标题与说明，右边是这一类行的动作。
func (s *Screen) itemCard(c ui.C, v View, item services.InboxItem, selected map[string]bool) {
	l := s.Look
	t := l.T
	c = c.Key("item-" + itemKey(item))
	itemID := item.KnowledgeItemID()
	ui.Row(c).FillWidth().Items(ui.Start).Gap(l.Space(3)).Pad(s.pad(4)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.6), Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() {
			if itemID != "" {
				// mt-1 shrink-0：勾选框的可访问名是「选择 标题」（用原始标题，不是「无标题」的回退）。
				checked := selected[itemID]
				before := checked
				ui.Row(c.Key("check")).Shrink(0).MarginTop(l.Space(1)).Children(func() {
					l.Checkbox(c.Key("sel"), &checked, kit.CheckboxSpec{Name: locales.InboxSelectItem(item.Title)})
				})
				if checked != before {
					s.Store.ToggleSelection(itemID)
				}
			} else {
				// mt-0.5 h-4 w-4 text-amber-500
				l.Icon(c.Key("warn"), icons.TriangleAlert, l.Rem(1), tokens.Tailwind("amber-500")).MarginTop(l.Space(0.5))
			}
			s.itemBody(c, item)
			s.itemActions(c, v, item, itemID)
		})
}

// itemBody 是卡片中间：标题（truncate text-sm font-medium）与说明（mt-1 text-xs text-muted-foreground，没有说明时只剩那 4px 的上边距）。
func (s *Screen) itemBody(c ui.C, item services.InboxItem) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("body")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
		title := item.Title
		if title == "" {
			title = locales.LibraryUntitled()
		}
		l.Text(c.Key("title"), title, kit.TextSM, kit.WeightMedium, t.Foreground, kit.Ellipsis())
		sub := itemSubtitle(item)
		if sub == "" {
			ui.Slot(c.Key("sub-gap")).FillWidth().Shrink(0).Height(l.Space(1))
			return
		}
		l.Text(c.Key("sub"), sub, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1))
	})
}

// itemSubtitle 是卡片的说明文字：复核行是各条原因，未归合集是固定的一句，采集问题是任务给的消息，其余为空。
func itemSubtitle(item services.InboxItem) string {
	switch item.Kind {
	case services.InboxKindReviewRequired:
		if reasons := strings.Join(item.Reasons, reasonSeparator); reasons != "" {
			return reasons
		}
		return locales.InboxReviewRequiredReason()
	case services.InboxKindUnclassified:
		return locales.InboxUnclassifiedReason()
	case services.InboxKindImportIssue:
		return item.Message
	}
	return ""
}

// cardButtonStyle 是卡片动作按钮的外观：h-8 rounded-lg px-2.5 text-xs；主按钮（标记已复核）是实心 primary，其余是 border-border 的描边钮。
func (s *Screen) cardButtonStyle(primary bool) kit.ButtonStyle {
	l := s.Look
	t := l.T
	st := kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Space(2.5), Radius: l.Rem(0.75), Text: kit.TextXS, Weight: kit.WeightNormal,
		Transition: kit.DurationDefault, DisabledOpacity: 0.6,
	}
	if primary {
		st.Fg, st.Bg = t.PrimaryForeground, t.Primary
		return st
	}
	st.Fg, st.Border, st.BorderW, st.BgHover = t.Foreground, t.Border, 1, t.Muted
	return st
}

// itemActions 是卡片右侧的动作：条目有 ID 时「打开」；复核行「标记已复核」；失败的采集问题「重试」；
// 有重复或结果条目的采集问题「查看条目」；已完成但带警告的采集问题「不再提醒」。
func (s *Screen) itemActions(c ui.C, v View, item services.InboxItem, itemID string) {
	l := s.Look
	ctx := s.Ctx
	if itemID != "" {
		if l.Button(c.Key("open"), kit.ButtonSpec{Label: locales.CommonOpen()}, s.cardButtonStyle(false)).Clicked() {
			s.Store.OpenItem(ctx, itemID)
		}
	}
	if item.Kind == services.InboxKindReviewRequired {
		if l.Button(c.Key("reviewed"), kit.ButtonSpec{Label: locales.LibraryMarkReviewed()}, s.cardButtonStyle(true)).Clicked() {
			ids := []string{item.ItemID}
			s.Store.Spawn(func() { s.Store.MarkReviewed(ctx, ids) })
		}
	}
	if item.Kind != services.InboxKindImportIssue {
		return
	}
	if item.Status == services.ImportStatusFailed {
		if l.Button(c.Key("retry"), kit.ButtonSpec{Label: locales.CommonRetry()}, s.cardButtonStyle(false)).Clicked() {
			taskID := item.TaskID
			s.Store.Spawn(func() { s.Store.RetryImportTask(ctx, taskID) }) // 之后的刷新（含采集队列）由 Store 做
		}
	}
	if target := ImportTargetID(item); target != "" {
		if l.Button(c.Key("view"), kit.ButtonSpec{Label: locales.InboxViewItem()}, s.cardButtonStyle(false)).Clicked() {
			s.Store.OpenItem(ctx, target)
		}
	}
	if item.Status == services.ImportStatusCompleted {
		busy := v.AcknowledgingTaskID == item.TaskID
		label := locales.InboxDismissWarning()
		if busy {
			label = locales.InboxProcessing()
		}
		if l.Button(c.Key("dismiss"), kit.ButtonSpec{Label: label, Disabled: busy}, s.cardButtonStyle(false)).Clicked() {
			taskID := item.TaskID
			s.Store.Spawn(func() { s.Store.DismissImportWarning(ctx, taskID) })
		}
	}
}
