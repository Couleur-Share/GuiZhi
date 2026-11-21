package library

import (
	"encoding/json"
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

// 详情头部上方的三块通知（渲染层 DraftConflictNotice、SourceCaptureRevisions、ReviewRequiredNotice）。
// 折叠都是浏览器原生的 <details>：summary 是行内的 disclosure 标记加文字，整行可点，Enter 与空格切换。

// noticeUI 是通知条的折叠状态：条一卸载（换条目、解决冲突）就回到折叠，同渲染层 <details> 的 open 属性。
type noticeUI struct {
	conflictOpen  bool
	revisionsOpen bool
	// revisionOpen 是各来源版本行的折叠；itemID 记下这些状态属于哪个条目，换条目就清空。
	revisionOpen map[string]bool
	itemID       string
	// reviewOpen 是复核提醒的折叠。
	reviewOpen bool
	// conflictW 是冲突比较里各栏（「字段/mine」「字段/saved」）上一帧的宽度：等宽原文按它折行（conflictField）。
	conflictW map[string]float32
}

// syncNotices 在条目换了时清空折叠状态：渲染层的通知组件随条目卸载，下一条的 <details> 是新元素。
func (s *Screen) syncNotices(itemID string) {
	n := &s.detail.notice
	if n.itemID == itemID {
		return
	}
	*n = noticeUI{itemID: itemID}
}

// summaryRow 声明一行 <summary>：行内的 disclosure 标记加文字，整行可点；返回是否被点击（调用方切换展开）。
// name 是可访问名；parts 是文字的分段：渲染层 JSX 里相邻的文本节点（`{title} · {date}`）各是一段，对照按文本节点逐段比，
// 所以分段画；空表示整段 name 一段。padY 是 summary 自己的上下内边距（py-2 之类）。
func (s *Screen) summaryRow(c ui.C, name string, parts []string, open bool, size kit.TextStyle, padY float32, fg tokens.Color) bool {
	l := s.Look
	marker := kit.ChromeDisclosureMarker(l.Rem(size.Size), l.Rem(size.Line), l.Scale)
	sc := c.Key("summary")
	if len(parts) == 0 {
		parts = []string{name}
	}
	row := ui.Row(sc).FillWidth().Items(ui.Center).PadSides(padY, 0, padY, marker.Advance).
		HitWith(ui.ID(sc), name, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Expandable: true, Expanded: open}).
		Draw(func(d *ui.DrawContext) {
			// 标记画在第一行文字的行盒里（行盒顶 = 外框顶 + 上内边距），不是整个外框的顶：summary 带 py-2 时差 8 px。
			line := d.Rect
			line.Y += padY
			line.Height -= 2 * padY
			kit.PaintDisclosure(d, line, marker, open, fg)
		})
	l.TabStop(row)
	row.Children(func() {
		for i, part := range parts {
			l.Text(sc.Key("label-"+strconv.Itoa(i)), part, size, kit.WeightNormal, fg)
		}
	})
	return row.Clicked()
}

// conflictNotice 是保存冲突的提醒（渲染层 DraftConflictNotice）：
// section[role=alert] border-b border-border bg-muted p-3 text-sm。两份内容在用户明确选择前都保留，比较区只读。
func (s *Screen) conflictNotice(c ui.C, v View, item services.LibraryItem) {
	conflict := v.Detail.Conflict
	if conflict == nil || conflict.ItemID != item.ID {
		return
	}
	l := s.Look
	t := l.T
	n := &s.detail.notice
	c = c.Key("conflict")
	// border-b 的 1 px 在盒内（Viva 的边线画在外框里面，不占位）：下内边距多留 1 px，整块 113 px 同渲染层。
	ui.Column(c).FillWidth().Shrink(0).PadSides(l.Space(3), l.Space(3), l.Space(3)+1, l.Space(3)).Draw(func(d *ui.DrawContext) {
		d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LiveAssertive})
		l.PaintSurface(d, kit.Surface{Bg: t.Muted})
		l.PaintEdge(d, kit.EdgeBottom, 1, t.Border)
	}).Children(func() {
		l.Text(c.Key("text"), locales.LibraryDraftConflict(), kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped())
		if s.summaryRow(c.Key("compare"), locales.LibraryDraftConflictCompare(), nil, n.conflictOpen, kit.TextSM, l.Space(2), t.Foreground) {
			n.conflictOpen = !n.conflictOpen
		}
		if n.conflictOpen {
			for _, field := range conflict.Fields {
				s.conflictField(c.Key("field-"+field), field, item, conflict.Server)
			}
		}
		// flex gap-4：两个 sm 的次要按钮。
		ui.Row(c.Key("actions")).Gap(l.Space(4)).Children(func() {
			if l.Button(c.Key("keep"), kit.ButtonSpec{Label: locales.LibraryDraftConflictKeepMine()}, l.UISecondaryButton(kit.UIButtonSM)).Clicked() {
				s.resolveConflict(KeepMine)
			}
			if l.Button(c.Key("use"), kit.ButtonSpec{Label: locales.LibraryDraftConflictUseSaved()}, l.UISecondaryButton(kit.UIButtonSM)).Clicked() {
				s.resolveConflict(UseSaved)
			}
		})
	})
}

// conflictPaneScrollbar 是 pre 里出竖向滚动条时它占去的内容宽度（与 versionPane 同一个取值）。
const conflictPaneScrollbar = 8

// conflictField 是一个冲突字段的两栏比较：grid grid-cols-2 gap-3，每栏一个 pre（max-h-48 overflow-auto whitespace-pre-wrap，等宽字体）。
//
// pre 里是 `{label}{value}` 两个相邻的文本节点（台账 B-063）：折行按拼起来的整段做（kit.WrapMonoRuns，等宽字格按渲染层的度量，
// 汉字回退到新宋体的前进量），再把每一行拆回标签与内容各画一个 Label（kit.MonoParagraphRuns），第二行回到 pre 的左缘。
// 折行宽度取这一栏上一帧量到的宽度（首帧按硬换行排、量到之后出一帧）；长到超过最高处就竖向滚动，滚动条占去的宽度要重新折。
func (s *Screen) conflictField(c ui.C, field string, local, server services.LibraryItem) {
	l := s.Look
	t := l.T
	n := &s.detail.notice
	size, line := l.Rem(0.875), l.Rem(1.25) // pre 的字体是等宽 1em（14px），行高继承 text-sm 的 20px
	maxH := l.Rem(12)                       // max-h-48
	pre := func(key, label, text string) {
		wkey := field + "/" + key
		ui.Column(c.Key(key)).Grow(1).Basis(0).MinWidth(0).Draw(func(d *ui.DrawContext) {
			if w := d.Rect.Width; w > 0 && abs32(w-n.conflictW[wkey]) > 0.01 {
				if n.conflictW == nil {
					n.conflictW = map[string]float32{}
				}
				n.conflictW[wkey] = w
				d.Canvas.RequestFrame()
			}
		}).Children(func() {
			runs := []string{label, text}
			w := n.conflictW[wkey]
			lines := kit.WrapMonoRuns(runs, w, size)
			if float32(len(lines))*line > maxH+0.01 && w > 0 {
				lines = kit.WrapMonoRuns(runs, w-conflictPaneScrollbar, size)
			}
			// max-h-48 overflow-auto whitespace-pre-wrap
			l.ScrollArea(c.Key(key+"-scroll"), [4]float32{}, func() {
				l.MonoParagraphRuns(c.Key(key+"-pre"), lines, size, line, t.Foreground)
			}).MaxHeight(maxH)
		})
	}
	ui.Row(c).FillWidth().Gap(l.Space(3)).Items(ui.Start).Children(func() {
		pre("mine", locales.LibraryDraftConflictMine(), conflictFieldText(local, field))
		pre("saved", locales.LibraryDraftConflictSaved(), conflictFieldText(server, field))
	})
}

// conflictFieldText 取冲突字段在条目里的值（渲染层 local[field]：标题、正文，其它字段按空处理）。
func conflictFieldText(item services.LibraryItem, field string) string {
	switch field {
	case "title":
		return item.Title
	case "content":
		return item.Content
	}
	return ""
}

// resolveConflict 解决保存冲突（「保留我的修改并保存」与「使用已保存版本」）：落盘在后台，失败走变更失败通道。
func (s *Screen) resolveConflict(choice ConflictChoice) {
	s.Store.Spawn(func() { s.Store.ResolveSaveConflict(s.Ctx, choice) })
}

// revisionsNotice 是「更新的来源版本」折叠块（渲染层 SourceCaptureRevisions）：没有版本也没有错误时整块不存在。
// details.mx-4.my-2.shrink-0.rounded.border.border-border.p-2.text-xs。
func (s *Screen) revisionsNotice(c ui.C, v View, item services.LibraryItem) {
	rev := v.Detail.Revisions
	if rev.ItemID != item.ID || (len(rev.Rows) == 0 && rev.Error == "") {
		return
	}
	l := s.Look
	t := l.T
	n := &s.detail.notice
	c = c.Key("revisions")
	ui.Column(c).FillWidth().Shrink(0).MarginSides(l.Space(2), l.Space(4), l.Space(2), l.Space(4)).Pad(l.Space(2) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.25)})
		}).Children(func() {
		label := locales.LibrarySourceRevisions(len(rev.Rows))
		if s.summaryRow(c.Key("head"), label, nil, n.revisionsOpen, kit.TextXS, 0, t.Foreground) {
			n.revisionsOpen = !n.revisionsOpen
		}
		if !n.revisionsOpen {
			return
		}
		// 纵向外边距在块布局里相邻会折叠：说明文字 p.my-2 的下边距与随后的 p[role=alert]（无边距）、details.my-2 的上边距、
		// 各 details.my-2 之间的上下边距，都折成 8 px 一道，不是各自相加的 16 px；最后一块的下边距在容器内边距之内，留着。
		// 所以说明、读取失败行与每个版本都只在上面留 8 px，最后一个版本（没有版本时是说明本身不留）再留 8 px 在下面。
		// p.my-2.text-muted-foreground：说明文字。
		ui.Row(c.Key("hint")).FillWidth().MarginSides(l.Space(2), 0, 0, 0).Children(func() {
			l.Text(c.Key("hint-text"), locales.LibrarySourceRevisionsHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).Shrink(1).MinWidth(0)
		})
		if rev.Error != "" {
			s.revisionsError(c, rev.Error)
		}
		for i, row := range rev.Rows {
			s.revisionRow(c.Key("row-"+row.ID), item, row, i == len(rev.Rows)-1)
		}
	})
}

// revisionsError 是读取失败的一行：p[role=alert] 里是原因文字与一个 ghost sm 的「重试」。
func (s *Screen) revisionsError(c ui.C, message string) {
	l := s.Look
	t := l.T
	// 上面留 8 px：是说明文字 p.my-2 的下边距（折叠后），错误行自己没有边距。
	ui.Row(c.Key("error")).FillWidth().MarginSides(l.Space(2), 0, 0, 0).Items(ui.Center).Draw(func(d *ui.DrawContext) {
		d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LiveAssertive})
	}).Children(func() {
		l.Text(c.Key("error-text"), message, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped()).Shrink(1).MinWidth(0)
		if l.Button(c.Key("retry"), kit.ButtonSpec{Label: locales.CommonRetry()}, l.UIGhostButton(kit.UIButtonSM)).Clicked() {
			s.Store.RetryRevisions(s.Ctx)
		}
	})
}

// revisionRow 是一个来源版本（details.my-2）：summary 是「标题 · 采集时间」，展开后是正文预览、缺口原因与「采用」钮。
// last 为真时下面再留 my-2 的 8 px（上下边距折叠后只有最后一个版本的下边距落在容器里，见 revisionsNotice）。
func (s *Screen) revisionRow(c ui.C, item services.LibraryItem, row services.LibrarySourceVersion, last bool) {
	l := s.Look
	t := l.T
	n := &s.detail.notice
	if n.revisionOpen == nil {
		n.revisionOpen = map[string]bool{}
	}
	open := n.revisionOpen[row.ID]
	bottom := float32(0)
	if last {
		bottom = l.Space(2)
	}
	ui.Column(c).FillWidth().MarginSides(l.Space(2), 0, bottom, 0).Children(func() {
		// JSX 的 `{row.title} · {time}` 是三个相邻文本节点，对照按节点逐段比：分三段画（中间一段带前后空格，宽度同整段文字）。
		when := kit.FormatDateTime(row.CapturedAt, s.loc(), s.Locale)
		head := row.Title + " · " + when
		if s.summaryRow(c.Key("head"), head, []string{row.Title, " · ", when}, open, kit.TextXS, 0, t.Foreground) {
			n.revisionOpen[row.ID] = !open
		}
		if !open {
			return
		}
		body := row.Content
		if row.Transcript != "" {
			body += "\n\n" + locales.LibrarySourceRevisionTranscript() + "\n" + row.Transcript
		}
		// pre.max-h-48.overflow-auto.whitespace-pre-wrap.font-sans：字号与行高继承 details 的 text-xs；Wrapped 认 \n 为硬换行。
		l.ScrollArea(c.Key("body-scroll"), [4]float32{}, func() {
			l.Text(c.Key("body"), body, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped()).FillWidth()
		}).MaxHeight(l.Rem(12))
		if row.ReviewJSON != "[]" {
			var reasons []string
			_ = json.Unmarshal([]byte(row.ReviewJSON), &reasons)
			l.Text(c.Key("missing"), locales.LibrarySourceRevisionMissing(strings.Join(reasons, locales.NativeLibraryReasonSeparator())), kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped())
		}
		// inline-flex：按钮按内容宽、靠左，不拉满一行。
		if l.Button(c.Key("adopt"), kit.ButtonSpec{Label: locales.LibrarySourceRevisionAdopt()}, l.UISecondaryButton(kit.UIButtonSM)).Self(ui.Start).Clicked() {
			s.adoptRevision(item.ID, row)
		}
	})
}

// adoptRevision 采用一个来源版本的正文（落盘与标待复核在后台，失败走变更失败通道）。
func (s *Screen) adoptRevision(itemID string, row services.LibrarySourceVersion) {
	s.Store.Spawn(func() { s.Store.AdoptSourceVersion(s.Ctx, itemID, row) })
}

// reviewNotice 是「需要人工复核」提醒（渲染层 ReviewRequiredNotice）：
// section.shrink-0.border-b.border-border/50.bg-muted/25.px-6.py-2.5.text-sm；条目不是待复核时整块不存在。
// disabled 为真（回收站）时不给「标记已复核」。
func (s *Screen) reviewNotice(c ui.C, v View, item services.LibraryItem, disabled bool) {
	if item.ReviewStatus != services.LibraryReviewNeedsReview {
		return
	}
	l := s.Look
	t := l.T
	n := &s.detail.notice
	c = c.Key("review")
	// border-b 的 1 px 在盒内（同 conflictNotice）：下内边距多留 1 px。
	ui.Column(c).FillWidth().Shrink(0).PadSides(l.Space(2.5), l.Space(6), l.Space(2.5)+1, l.Space(6)).Draw(func(d *ui.DrawContext) {
		d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, locales.LibraryReviewRequired(), gfx.HitOptions{})
		l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.25)})
		l.PaintEdge(d, kit.EdgeBottom, 1, t.Border.WithAlpha(0.5))
	}).Children(func() {
		s.reviewSummary(c, n)
		if !n.reviewOpen {
			return
		}
		s.reviewBody(c, v, item, disabled)
	})
}

// reviewSummary 是 summary：flex flex-wrap items-center gap-2 text-xs，警示图标、两段文字、右对齐的「查看缺口与复核」。
func (s *Screen) reviewSummary(c ui.C, n *noticeUI) {
	l := s.Look
	t := l.T
	amber := tokens.Tailwind("amber-600")
	if l.Dark {
		amber = tokens.Tailwind("amber-400")
	}
	sc := c.Key("summary")
	// 可访问名是 summary 里全部文字（标题、说明、右边的「查看缺口与复核」）按空格连起来，同渲染层的 DisclosureTriangle。
	name := strings.Join([]string{locales.ArticleReaderImportIncomplete(), locales.ArticleReaderImportReadable(), locales.ArticleReaderReviewDetails()}, " ")
	row := ui.Flow(sc).FillWidth().Items(ui.Center).Gap(l.Space(2)).
		HitWith(ui.ID(sc), name, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Expandable: true, Expanded: n.reviewOpen})
	l.TabStop(row)
	row.Children(func() {
		l.Icon(sc.Key("icon"), icons.TriangleAlert, l.Rem(1), amber)
		l.Text(sc.Key("title"), locales.ArticleReaderImportIncomplete(), kit.TextXS, kit.WeightMedium, t.Foreground)
		l.Text(sc.Key("readable"), locales.ArticleReaderImportReadable(), kit.TextXS, kit.WeightNormal, t.MutedForeground)
		// ml-auto：自动外边距吃掉这一行的剩余宽度，文字靠行尾；折到下一行时它独占一行、同样靠右。Viva 没有自动外边距，
		// 用一个可增长的包装盒、内容靠行尾来模拟。
		ui.Row(sc.Key("details-wrap")).Grow(1).Justify(ui.End).Children(func() {
			l.Text(sc.Key("details"), locales.ArticleReaderReviewDetails(), kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
	})
	if row.Clicked() {
		n.reviewOpen = !n.reviewOpen
	}
}

// reviewBody 是展开后的内容：max-h-56 overflow-y-auto space-y-3 pb-1 pl-6 pt-3。
func (s *Screen) reviewBody(c ui.C, v View, item services.LibraryItem, disabled bool) {
	l := s.Look
	t := l.T
	// 滚动区自己不报内容高度，要给上限：max-h-56。
	l.ScrollArea(c.Key("body"), [4]float32{l.Space(3), 0, l.Space(1), l.Space(6)}, func() {
		ui.Column(c.Key("stack")).FillWidth().Gap(l.Space(3)).Children(func() {
			// p.text-xs.leading-5
			l.TextPx(c.Key("hint"), locales.LibraryReviewRequiredHint(), l.Rem(0.75), l.Rem(1.25), kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			if reasons := nonEmpty(item.ReviewReasons); len(reasons) > 0 {
				// ul.list-disc.space-y-1.pl-4.text-xs.leading-relaxed：圆点项目符号在内容左侧的 1rem 内边距里。
				ui.Column(c.Key("reasons")).FillWidth().Gap(l.Space(1)).Children(func() {
					for i, reason := range reasons {
						s.reasonItem(c.Key("reason-"+strconv.Itoa(i)), reason)
					}
				})
			}
			if !disabled {
				s.markReviewedButton(c, v, item)
			}
		})
	}).MaxHeight(l.Rem(14))
}

// nonEmpty 去掉空串（渲染层 reviewReasons?.filter(Boolean)）。
func nonEmpty(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// reasonItem 是一条复核原因（li.select-text.break-words，list-disc：圆点在内容左侧）。
func (s *Screen) reasonItem(c ui.C, reason string) {
	l := s.Look
	t := l.T
	ui.Row(c).FillWidth().PadSides(0, 0, 0, l.Space(4)).Children(func() {
		l.TextPx(c.Key("text"), reason, l.Rem(0.75), l.Rem(0.75)*1.625, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).FillWidth()
	}).Draw(func(d *ui.DrawContext) {
		// 圆点：直径约 0.33em，垂直落在首行的 x 高度附近；位置随对照调到与渲染层一致。
		r := l.Rem(0.75) * 0.17
		cx := d.Rect.X + l.Space(4) - l.Rem(0.75)*0.95
		cy := d.Rect.Y + l.Rem(0.75)*1.625/2
		d.Canvas.Rounded(gfx.Rect{X: cx - r, Y: cy - r, Width: 2 * r, Height: 2 * r}, r, d.Fade(l.Col(t.MutedForeground)))
	})
}

// markReviewedButton 是「标记已复核」：inline-flex min-h-8 items-center gap-1.5 rounded-lg border border-border bg-background px-3 text-xs
// font-medium hover:bg-accent disabled:opacity-60；进行中图标换成转圈、按钮禁用。
func (s *Screen) markReviewedButton(c ui.C, v View, item services.LibraryItem) {
	l := s.Look
	t := l.T
	busy := v.Detail.MarkingReviewedID == item.ID
	spec := kit.ButtonSpec{Label: locales.LibraryMarkReviewed(), Disabled: busy, Busy: busy, BusySize: l.Rem(0.875)}
	if !busy {
		spec.Icon, spec.IconSize = icons.Check, l.Rem(0.875)
	}
	box := l.Button(c.Key("mark-reviewed"), spec, kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightMedium,
		Fg: t.Foreground, Bg: t.Background, BgHover: t.Accent, Border: t.Border, BorderW: 1, Transition: kit.DurationDefault, DisabledOpacity: 0.6,
		Surface: t.Muted.WithAlpha(0.25).Over(t.UiPanelBg),
	}).Self(ui.Start)
	if box.Clicked() {
		s.Store.Spawn(func() { s.Store.MarkReviewed(s.Ctx, item.ID) })
	}
}
