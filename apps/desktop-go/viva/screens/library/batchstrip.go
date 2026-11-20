package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// batchStrip 是批量结果条（渲染层 LibraryBatchResults）：role=status，border-b border-border bg-muted/30 px-4 py-2 text-xs。
// 没有一句话也没有逐条结果时不渲染；卡片与表格两个视图把它放在批量条（或工具条）下面。
//
// 第一行是 flex items-center gap-2：说明（忙碌时「处理中，已核对 N 项」，否则是 Store 里的那句话）、
// 有失败项时的「仅重试失败项」、「收起」。下面是 <details>：逐条结果的清单，展开状态是条自己的（条卸载即折叠）。
func (s *Screen) batchStrip(c ui.C, v View) {
	l := s.Look
	t := l.T
	b := v.Batch
	if len(b.Results) == 0 {
		s.bulk.details = false // <details> 随逐条结果一起卸载，再出现时是折叠的
	}
	if !b.Visible() {
		return
	}
	ui.Column(c.Key("batch-strip")).FillWidth().Shrink(0).
		PadSides(l.Space(2), l.Space(4), l.Space(2)+1, l.Space(4)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.3)})
			l.PaintEdge(d, kit.EdgeBottom, 1, t.Border)
			// role="status"：礼貌的直播区域。
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LivePolite})
		}).Children(func() {
		ui.Row(c.Key("row")).FillWidth().Items(ui.Center).Gap(l.Space(2)).Children(func() {
			message := b.Notice
			if b.Busy {
				message = locales.LibraryWorkflowProcessing(len(b.Results))
			}
			l.Text(c.Key("message"), message, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped()).Grow(1).Shrink(1).MinWidth(0)
			if len(b.Failed()) > 0 {
				if l.Button(c.Key("retry"), kit.ButtonSpec{Label: locales.LibraryWorkflowRetryFailed(), Disabled: b.Busy, NoRing: true},
					l.UISecondaryButton(kit.UIButtonSM)).Clicked() {
					s.retryFailed(b)
				}
			}
			if l.Button(c.Key("dismiss"), kit.ButtonSpec{Label: locales.LibraryWorkflowDismiss(), Disabled: b.Busy},
				l.UIGhostButton(kit.UIButtonSM)).Clicked() {
				s.Store.DismissBatch()
			}
		})
		if len(b.Results) > 0 {
			s.batchDetails(c, b)
		}
	})
}

// retryFailed 响应「仅重试失败项」：删除命令的失败项要重新确认（彻底删除不可逆），其余命令直接重放
// （渲染层 command?.kind === "delete" ? setConfirm(true) : retryFailed()）。
func (s *Screen) retryFailed(b BatchState) {
	if b.Command != nil && b.Command.Kind == "delete" {
		s.openConfirm(confirmDeleteForever, b.Failed())
		return
	}
	s.Store.Spawn(func() { s.Store.RetryFailed(s.Ctx) })
}

// batchDetails 是 <details>：<summary class="cursor-pointer">「逐条结果（N）」</summary> 加 <ul class="max-h-36 overflow-auto">，
// 每条一行「ID：成功 / 失败原因」。summary 是行内标记 disclosure-closed / disclosure-open（kit.ChromeDisclosureMarker）加文字，
// 整行可点（块级），Enter 与空格切换。
func (s *Screen) batchDetails(c ui.C, b BatchState) {
	l := s.Look
	t := l.T
	open := s.bulk.details
	label := locales.LibraryWorkflowResults(len(b.Results))
	marker := kit.ChromeDisclosureMarker(l.Rem(kit.TextXS.Size), l.Rem(kit.TextXS.Line), l.Scale)

	sc := c.Key("summary")
	row := ui.Row(sc).FillWidth().Items(ui.Center).PadSides(0, 0, 0, marker.Advance).
		HitWith(ui.ID(sc), label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Expandable: true, Expanded: open}).
		Draw(func(d *ui.DrawContext) { kit.PaintDisclosure(d, d.Rect, marker, open, t.Foreground) })
	l.TabStop(row)
	row.Children(func() {
		l.Text(sc.Key("label"), label, kit.TextXS, kit.WeightNormal, t.Foreground)
	})
	if row.Clicked() {
		s.bulk.details = !open
	}
	if !open {
		return
	}
	// 渲染层把全部结果整表渲染；这里只声明视口里的行与上下各几行（kit.RowWindow，台账 B-056）：一次「全部符合筛选」的批量
	// 可以有上万条结果，整表声明时每帧要几百毫秒。行高大多是一行（成功与短的失败原因），过长的失败原因折成多行，所以用变高模式：
	// 没量过的行按已量行高的平均估计，全是单行时估计与实测一致。
	l.ScrollAreaView(c.Key("results"), [4]float32{}, func(view kit.ScrollView) {
		w := &s.bulk.results
		w.Seed = l.Rem(kit.TextXS.Line)
		w.Rows(c, kit.RowsSpec{
			Name: "results-rows", N: len(b.Results), View: view,
			Epoch: kit.RowEpoch{RootPx: l.RootPx, Variant: int(locales.CurrentLang())},
			Key:   func(i int) string { return b.Results[i].ID },
			Row:   func(i int) ui.Box { return s.resultRow(c, b.Results[i]) },
		})
	}).FillWidth().MaxHeight(l.Rem(9))
}

// resultRow 是逐条结果的一行：返回它的最外层盒子（窗口化的行列在绘制时读它布局后的高度）。
func (s *Screen) resultRow(c ui.C, r services.LibraryBatchResult) ui.Box {
	l := s.Look
	t := l.T
	outcome := locales.CommonSuccess()
	if !r.OK {
		outcome = r.Error
	}
	// 渲染层是 <li>{row.id}：{outcome}</li>：三个相邻的文字节点（编号、全角冒号、结果），采集到的文字行也是三段。
	// 这里三段并排：编号与冒号一行排完，结果占余下的宽度（过长的失败原因在结果自己的盒里折行）。
	key := c.Key("result-" + r.ID)
	return ui.Row(key).FillWidth().Items(ui.Start).Children(func() {
		l.Text(key.Key("id"), r.ID, kit.TextXS, kit.WeightNormal, t.Foreground)
		l.Text(key.Key("sep"), locales.NativeLibraryResultSeparator(), kit.TextXS, kit.WeightNormal, t.Foreground)
		l.Text(key.Key("outcome"), outcome, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped()).Grow(1).Shrink(1).MinWidth(0)
	})
}
