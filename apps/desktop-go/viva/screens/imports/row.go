package imports

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 渲染层行内用到的 Tailwind 色阶（不是主题令牌，浅色与深色各取一档）。
func (s *Screen) tone(light, dark string) tokens.Color {
	if s.Look.Dark {
		return tokens.Tailwind(dark)
	}
	return tokens.Tailwind(light)
}

// openableItemID 是任务结果能打开的条目：完成取结果条目，重复取已有条目，其余没有。
func openableItemID(task services.ImportTask) string {
	switch task.Status {
	case services.ImportStatusCompleted:
		return task.ResultItemID
	case services.ImportStatusDuplicate:
		return task.DuplicateItemID
	}
	return ""
}

// taskRow 是一行导入任务（渲染层 ImportTaskRow）。
//
// group/row relative rounded-xl border px-3 py-2 transition-colors：选中 border-primary/50 bg-primary/10，
// 否则 border-border/70 bg-background/50，悬停 bg-accent/30。悬停与「焦点在行内」影响前导槽与浮出的动作条，
// 整行的命中区只用来感知悬停（不进可访问树）。返回行的最外层盒子：窗口化的行列（kit.RowWindow）布局后读它的高度。
func (s *Screen) taskRow(c ui.C, v View, task services.ImportTask, now int64) ui.Box {
	l := s.Look
	t := l.T
	canvas := c.Canvas()
	checked := v.IsSelected(task.ID)
	hasSelection := len(v.Selected) > 0
	group := task.ID

	c = c.Key("row-" + task.ID)
	id := ui.ID(c)
	s.rowHover.Add(group, id)
	hovered := s.rowHover.Hovered(canvas) == group
	active := s.rowHover.Active(canvas, group)

	// 悬停与选中各一个进度量：悬停中被选中时底色从 accent/30 补间到 primary/10。
	ph := l.HoverProgress(c, id, 0, hovered, kit.DurationDefault)
	pc := l.HoverProgress(c, id, 1, checked, kit.DurationDefault)
	bg := kit.MixToken(kit.MixToken(t.Background.WithAlpha(0.5), t.Accent.WithAlpha(0.3), ph), t.Primary.WithAlpha(0.1), pc)
	border := kit.MixToken(t.Border.WithAlpha(0.7), t.Primary.WithAlpha(0.5), pc)

	pad := func(n float32) float32 { return l.Space(n) + 1 } // 内边距加 1px 边框
	return ui.Column(c).FillWidth().PadSides(pad(2), pad(3), pad(2), pad(3)).
		HitWith(id, "", gfx.HitOptions{SkipFocus: true, SkipTab: true}).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() {
			// flex items-start gap-2.5
			ui.Row(c.Key("main")).FillWidth().Items(ui.Start).Gap(l.Space(2.5)).Children(func() {
				s.leadingSlot(c, task, checked, hasSelection, hovered, active)
				ui.Column(c.Key("body")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					s.rowTitle(c, task)
					s.rowMeta(c, task, now)
					if task.Error != "" {
						l.Text(c.Key("error"), FormatTaskError(task.Error), kit.TextXS, kit.WeightNormal,
							t.Destructive.WithAlpha(0.9), kit.Wrapped()).MarginTop(l.Space(1))
					}
					if task.Warning != "" {
						l.Text(c.Key("warning"), FormatTaskWarning(task.Warning), kit.TextXS, kit.WeightNormal,
							s.tone("amber-600", "amber-400"), kit.Wrapped()).MarginTop(l.Space(1))
					}
				})
				s.timeLabel(c, task)
			})
			s.actionBar(c, task, active)
		})
}

// timeLabel 是行尾的时间：mt-0.5 shrink-0 text-xs tabular-nums text-muted-foreground；悬停气泡写入队与更新两个时刻。
// 运行中的任务显示入队时间，其余显示更新时间。
func (s *Screen) timeLabel(c ui.C, task services.ImportTask) {
	l := s.Look
	ms := task.UpdatedAt
	if IsActive(task) {
		ms = task.CreatedAt
	}
	label := kit.FormatItemTime(ms, s.nowTime(), s.loc(), s.Locale)
	tip := locales.ImportsTimeTooltip(
		kit.FormatDateTime(task.CreatedAt, s.loc(), s.Locale),
		kit.FormatDateTime(task.UpdatedAt, s.loc(), s.Locale),
	)
	// 提示（title）挂在一个有身份的元素上：包一层只负责悬停的命中区（无名、不可聚焦，不进可访问树；
	// 渲染层的 title 属性同样不产生节点）。命中区也算「行内」，悬停它时整行仍处于悬停。
	c = c.Key("time")
	id := ui.ID(c)
	s.rowHover.Add(task.ID, id)
	ui.Row(c).Shrink(0).MarginTop(l.Space(0.5)).HitWith(id, "", gfx.HitOptions{SkipFocus: true, SkipTab: true, Hint: tip}).
		Children(func() {
			l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, l.T.MutedForeground)
		})
}

// taskIcon 是前导槽里的类型图标：有条目类型取类型图标，否则按来源种类（网址、文件、文本）。
func taskIcon(task services.ImportTask) gfx.Icon {
	if task.ItemType != "" {
		return kit.GetItemTypeMeta(task.ItemType).Icon
	}
	switch task.SourceKind {
	case "url":
		return icons.Globe
	case "file":
		return icons.FileText
	}
	return icons.Type
}

// leadingSlot 是前导槽（渲染层 LeadingSlot）：默认显示条目类型图标，悬停或进入多选后原地换成勾选框。
// 图标与勾选框叠在同一个 16px 方格里交叉淡入，行内元素不会因此位移。
//
// 三档互斥：已勾选 border-primary bg-primary；多选中未勾选 border-border；平时 opacity-0，悬停或焦点在行内时显出。
func (s *Screen) leadingSlot(c ui.C, task services.ImportTask, checked, hasSelection, hovered, active bool) {
	l := s.Look
	t := l.T
	name := locales.ImportsSelect()
	hint := name // title 取条目类型名（没有类型时取「选择」）：悬停气泡的文字
	desc := ""   // 与名称相同时在可访问树里不算描述
	if task.ItemType != "" {
		hint = kit.GetItemTypeMeta(task.ItemType).Label
		if hint != name {
			desc = hint
		}
	}
	c = c.Key("lead")
	id := ui.ID(c)
	s.rowHover.Add(task.ID, id)
	size := l.Rem(1)
	box := ui.Stack(c).Size(size, size).Shrink(0).MarginTop(l.Space(0.5)).
		HitWith(id, name, gfx.HitOptions{Role: gfx.RoleButton, Selected: checked, Description: l.HintDescription(id, desc), Hint: hint, Cursor: gfx.PointerCursorHand})

	boxVisible := checked || hasSelection
	showIcon := !boxVisible && !hovered // opacity-100 group-hover/row:opacity-0
	showBox := boxVisible || active     // group-hover 或 group-focus-within
	pi := l.HoverProgress(c, id, 0, showIcon, kit.DurationQuick)
	pb := l.HoverProgress(c, id, 1, showBox, kit.DurationQuick)

	surface := kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.25)}
	if checked {
		surface.Bg, surface.Border = t.Primary, t.Primary
	}
	box.Children(func() {
		ui.Row(c.Key("type")).Absolute().Inset(0).Justify(ui.Center).Items(ui.Center).Opacity(pi).Children(func() {
			l.Icon(c, taskIcon(task), l.Rem(0.875), t.MutedForeground)
		})
		ui.Row(c.Key("check")).Absolute().Inset(0).Justify(ui.Center).Items(ui.Center).Opacity(pb).
			Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).
			Children(func() {
				if checked {
					l.Icon(c, icons.Check, l.Rem(0.75), t.PrimaryForeground)
				}
			})
	})
	if box.Clicked() {
		if info, ok := c.Canvas().ClickInfo(); ok && info.Modifiers&gfx.ModifierShift != 0 {
			s.Store.RangeSelectTo(task.ID)
		} else {
			s.Store.ToggleSelection(task.ID)
		}
	}
}

// rowTitle 是任务标题：能打开结果的是文字按钮（悬停变主色加下划线），其余是普通文字；都 truncate text-sm。
// 显示名顶掉原始地址时，原始地址是它的描述（title 气泡）。
func (s *Screen) rowTitle(c ui.C, task services.ImportTask) {
	l := s.Look
	t := l.T
	name := task.DisplayName
	if name == "" {
		name = task.SourceInput
	}
	itemID := openableItemID(task)
	desc := ""
	if task.DisplayName != "" {
		desc = task.SourceInput
	}
	if itemID == "" {
		if desc == "" {
			l.Text(c.Key("title"), name, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Ellipsis())
			return
		}
		// 提示（title）挂在一个有身份的元素上：包一层只负责悬停的命中区（无名、不可聚焦，不进可访问树），命中区也算「行内」。
		c = c.Key("title")
		id := ui.ID(c)
		s.rowHover.Add(task.ID, id)
		ui.Row(c).FillWidth().HitWith(id, "", gfx.HitOptions{SkipFocus: true, SkipTab: true, Hint: desc}).Children(func() {
			l.Text(c.Key("text"), name, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Ellipsis()).Grow(1).Shrink(1).MinWidth(0)
		})
		return
	}
	c = c.Key("title")
	id := ui.ID(c)
	s.rowHover.Add(task.ID, id)
	box := ui.Row(c).FillWidth().HitWith(id, name, gfx.HitOptions{Role: gfx.RoleButton, Description: l.HintDescription(id, desc), Hint: desc, Cursor: gfx.PointerCursorHand})
	hovered := box.State().Hovered
	p := l.HoverProgress(c, id, 0, hovered, kit.DurationDefault)
	color := kit.MixToken(t.Foreground, t.Primary, p)
	box.Children(func() {
		// hover:text-primary hover:underline：颜色有 150ms 过渡，下划线随悬停立即出现。
		l.Text(c.Key("text"), name, kit.TextSM, kit.WeightNormal, color, kit.Ellipsis(), kit.Underline(hovered)).Grow(1).Shrink(1).MinWidth(0)
	})
	if box.Clicked() && s.Hooks.OpenItem != nil {
		s.Hooks.OpenItem(itemID)
	}
}

// rowMeta 是标题下面的一行元信息：mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground。
func (s *Screen) rowMeta(c ui.C, task services.ImportTask, now int64) {
	l := s.Look
	t := l.T
	ui.Flow(c.Key("meta")).FillWidth().Items(ui.Center).Gap(l.Space(2)).LineGap(l.Space(1)).MarginTop(l.Space(1)).Children(func() {
		s.statusBadge(c, task)
		if task.Status == services.ImportStatusCompleted && task.ResultItemID != "" {
			s.viewResultButton(c, task)
		}
		s.originLabel(c, task, t.MutedForeground)
		if host, ok := ResolveTaskHost(task); ok {
			l.Text(c.Key("host"), host, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).Shrink(1).MinWidth(0)
		}
		if folder, ok := ResolveTaskFolder(task); ok {
			l.Text(c.Key("folder"), folder, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).
				Shrink(1).MinWidth(0).MaxWidth(l.Rem(18))
		}
		if task.ItemType != "" {
			l.Text(c.Key("type"), kit.GetItemTypeMeta(task.ItemType).Label, kit.TextXS, kit.WeightNormal, t.MutedForeground)
		}
		s.progressHint(c, task, now)
		s.stageSummary(c, task)
	})
}

// badgeStyle 是状态徽标的外观。
type badgeStyle struct {
	bg, fg tokens.Color
	icon   gfx.Icon
	spin   bool
	label  string
}

// badgeFor 按任务状态挑徽标。入库了但缺了文字稿这类东西时，绿色的「已完成」等于骗人，所以带警告的完成单独是琥珀色。
func (s *Screen) badgeFor(task services.ImportTask) badgeStyle {
	t := s.Look.T
	amber := badgeStyle{bg: tokens.Tailwind("amber-500").WithAlpha(0.1), fg: s.tone("amber-600", "amber-400")}
	switch task.Status {
	case services.ImportStatusPending:
		return badgeStyle{bg: t.Muted, fg: t.MutedForeground, label: locales.ImportsStatusPending()}
	case services.ImportStatusProcessing:
		return badgeStyle{bg: t.Primary.WithAlpha(0.1), fg: t.Primary, icon: icons.LoaderCircle, spin: true, label: StageLabel(task.Stage)}
	case services.ImportStatusCompleted:
		if task.Warning != "" {
			amber.icon, amber.label = icons.TriangleAlert, locales.ImportsStatusDegraded()
			return amber
		}
		return badgeStyle{
			bg: tokens.Tailwind("green-500").WithAlpha(0.1), fg: s.tone("green-600", "green-400"),
			icon: icons.CircleCheck, label: locales.ImportsStatusCompleted(),
		}
	case services.ImportStatusDuplicate:
		amber.icon, amber.label = icons.Copy, locales.ImportsStatusDuplicate()
		return amber
	case services.ImportStatusCanceled:
		return badgeStyle{bg: t.Muted, fg: t.MutedForeground, icon: icons.Ban, label: locales.ImportsStatusCanceled()}
	}
	return badgeStyle{bg: t.Destructive.WithAlpha(0.1), fg: t.Destructive, icon: icons.TriangleAlert, label: locales.ImportsStatusFailed()}
}

// statusBadge 是状态徽标：inline-flex shrink-0 items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] leading-none。
func (s *Screen) statusBadge(c ui.C, task services.ImportTask) {
	l := s.Look
	b := s.badgeFor(task)
	ui.Row(c.Key("status")).Shrink(0).Items(ui.Center).Gap(l.Space(1)).
		PadSides(l.Space(0.5), l.Space(1.5), l.Space(0.5), l.Space(1.5)).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: b.bg, Radius: 9999}) }).
		Children(func() {
			icon := l.Rem(0.625) // h-2.5 w-2.5
			switch {
			case b.spin:
				l.Spinner(c.Key("icon"), icon, b.fg)
			case b.icon.ViewBox > 0:
				l.Icon(c.Key("icon"), b.icon, icon, b.fg)
			}
			l.TextPx(c.Key("label"), b.label, 10, 10, kit.WeightNormal, b.fg)
		})
}

// viewResultButton 是刚完成任务状态旁常驻的「查看成果」：把用户带到入库事实与后续动作。
// inline-flex h-5 items-center rounded-md bg-primary/10 px-1.5 text-[10px] font-medium text-primary transition-colors hover:bg-primary/15。
func (s *Screen) viewResultButton(c ui.C, task services.ImportTask) {
	l := s.Look
	t := l.T
	b := l.Button(c.Key("view-result"), kit.ButtonSpec{Label: locales.ImportsViewResult(), Groups: &s.rowHover, Group: task.ID}, kit.ButtonStyle{
		Height: l.Rem(1.25), PadX: l.Space(1.5), Radius: l.Rem(0.75) - 2, FontPx: 10, LinePx: l.Rem(1), Weight: kit.WeightMedium,
		Fg: t.Primary, Bg: t.Primary.WithAlpha(0.1), BgHover: t.Primary.WithAlpha(0.15), Transition: kit.DurationDefault,
		Ring: t.Primary.WithAlpha(0.3), FocusRing: true, // focus:outline-none focus:ring-2 focus:ring-primary/30
	})
	if b.Clicked() {
		s.openDetail(task.ID)
	}
}

// originLabel 是提交来源：inline-flex shrink-0 items-center gap-1，图标 h-3 w-3，文字继承 text-xs，图标与文字同色（currentColor）。
// 行里是次要色，详情弹窗的字段值里是正文色。
func (s *Screen) originLabel(c ui.C, task services.ImportTask, color tokens.Color) {
	l := s.Look
	icon, label := icons.Monitor, locales.ImportsOriginDesktopLabel()
	if task.Origin == OriginMobile {
		icon, label = icons.Smartphone, locales.ImportsOriginMobileLabel()
	}
	ui.Row(c.Key("origin")).Shrink(0).Items(ui.Center).Gap(l.Space(1)).Children(func() {
		l.Icon(c.Key("icon"), icon, l.Rem(0.75), color)
		l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, color)
	})
}

// progressHint 是进度信息。视频链路可跑几十分钟，只有一个转圈无法区分「在推进」和「卡死」，所以给出任务已用时长；
// 单个阶段超过阈值还没动静时补一条本阶段耗时。排队与处理分开报：排在后面的任务能等上一个钟头，
// 把这段算进「已用」等于在说一件没发生的事。
func (s *Screen) progressHint(c ui.C, task services.ImportTask, now int64) {
	l := s.Look
	t := l.T
	switch task.Status {
	case services.ImportStatusPending:
		l.Text(c.Key("queued"), locales.ImportsQueuedFor(FormatDuration(max(0, now-task.CreatedAt))),
			kit.TextXS, kit.WeightNormal, t.MutedForeground)
	case services.ImportStatusProcessing:
		l.Text(c.Key("elapsed"), locales.ImportsElapsed(FormatDuration(ResolveWorkElapsed(task, now))),
			kit.TextXS, kit.WeightNormal, t.MutedForeground)
		if inStage := max(0, now-task.UpdatedAt); inStage >= StallThresholdMs {
			l.Text(c.Key("stalled"), locales.ImportsStageElapsed(FormatDuration(inStage)),
				kit.TextXS, kit.WeightNormal, s.tone("amber-600", "amber-400"))
		}
	}
}

// stageSummary 是行上常驻的耗时摘要「共 X · 最慢 Y」：任务一结束就让耗时信息消失，「排版慢了八分钟」这种异常只能靠翻数据库发现。
// 运行中的任务由 progressHint 负责；没有阶段统计的任务不显示。
// inline-flex items-center gap-1 rounded text-[11px] text-muted-foreground hover:text-foreground hover:underline。
func (s *Screen) stageSummary(c ui.C, task services.ImportTask) {
	if IsActive(task) || len(task.StageStats) == 0 {
		return
	}
	l := s.Look
	t := l.T
	stats := task.StageStats
	total, slowest := SummarizeStages(stats)
	first := locales.ImportsTotalElapsed(FormatDuration(total))
	second := ""
	if len(stats) > 1 {
		// 只有一个阶段时「最慢」等于「全部」，说了等于没说
		second = "\u00b7 " + locales.ImportsSlowestStage(StageLabel(slowest.Stage), FormatDuration(slowest.Ms))
	}
	name := first
	if second != "" {
		name += " " + second
	}
	c = c.Key("summary")
	id := ui.ID(c)
	s.rowHover.Add(task.ID, id)
	box := ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(1)).
		HitWith(id, name, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	hovered := box.State().Hovered
	p := l.HoverProgress(c, id, 0, hovered, kit.DurationDefault)
	color := kit.MixToken(t.MutedForeground, t.Foreground, p)
	box.Children(func() {
		// hover:underline：两段文字各是一个弹性项，下划线各画各的，间隙里没有线。
		l.TextPx(c.Key("total"), first, 11, l.Rem(1), kit.WeightNormal, color, kit.Underline(hovered))
		if second != "" {
			l.TextPx(c.Key("slowest"), second, 11, l.Rem(1), kit.WeightNormal, color, kit.Underline(hovered))
		}
	})
	if box.Clicked() {
		s.openDetail(task.ID)
	}
}
