package wiki

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// compileUI 是全文编译对话框自己的界面状态：模态窗、筛选输入框与开合的上升沿。
type compileUI struct {
	modal  kit.ModalState
	filter ui.Field
	opened bool // 上一帧对话框是否开着：由关到开的那一帧把筛选框回到 Store 里的筛选
}

// compileDialog 是「全文编译与历史升级」对话框（渲染层 WikiCompileDialog，xl 档模态窗）。
// 每一帧都要经过 Modal：关着的那一帧它负责复位。关闭对话框不会停掉后台的编译，进度仍在顶栏。
func (s *Screen) compileDialog(c ui.C) {
	l := s.Look
	cv := s.Store.Compile().View()
	d := &s.compile
	if cv.Open && !d.opened {
		d.filter.SetText(cv.Filter)
	}
	d.opened = cv.Open
	if cv.Open {
		if text := d.filter.Text(); text != cv.Filter {
			s.Store.Compile().SetFilter(text)
		}
	}
	closed := l.Modal(c.Key("compile-modal"), &d.modal, kit.ModalSpec{
		Open: cv.Open, Size: kit.ModalXL, Title: locales.WikiCompileDialog(), CloseLabel: locales.CommonClose(),
		Body: func(c ui.C) { s.compileBody(c, cv) },
	})
	if closed {
		s.Store.Compile().Close()
	}
}

// compileBody 是对话框内容：space-y-4 text-sm。说明、失败原因、预览（范围、筛选、资料列表、估算、开始）与任务列表。
func (s *Screen) compileBody(c ui.C, cv CompileView) {
	l := s.Look
	t := l.T
	c = c.Key("compile")
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		s.bodyText(c.Key("intro"), locales.WikiCompileIntro(), t.Foreground)
		if cv.Error != "" {
			// role=alert whitespace-pre-wrap text-destructive：多行原因原样显示。
			s.alertLine(c.Key("error"), cv.Error)
		}
		if cv.Preview != nil {
			s.compilePreview(c, cv)
		} else {
			s.compilePreparing(c, cv)
		}
		if len(cv.Jobs) > 0 {
			s.compileJobs(c, cv)
		}
	})
}

// bodyText 是对话框里的一段文字：text-sm，折行。
func (s *Screen) bodyText(c ui.C, text string, color tokens.Color) {
	s.Look.Text(c, text, kit.TextSM, kit.WeightNormal, color, kit.Wrapped())
}

// compilePreparing 是预览还没算出来：读取中是 role=status 的一句话；读取失败（Error 已在上面显示）时给一个重试入口。
func (s *Screen) compilePreparing(c ui.C, cv CompileView) {
	l := s.Look
	t := l.T
	c = c.Key("preparing")
	if cv.Loading || cv.Error == "" {
		s.bodyText(c.Key("text"), locales.WikiCompilePreparing(), t.Foreground)
		return
	}
	b := l.Button(c.Key("retry"), kit.ButtonSpec{Label: locales.CommonRetry(), NoRing: true}, l.UISecondaryButton(kit.UIButtonMD))
	if b.Clicked() {
		s.spawn(func() { s.Store.Compile().Refresh() })
	}
}

// compilePreview 是预览区：范围计数、没有模型时的引导、筛选框、资料列表、本批估算与「开始所选资料」。
func (s *Screen) compilePreview(c ui.C, cv CompileView) {
	l := s.Look
	t := l.T
	pc := cv.Preview.Counts
	c = c.Key("preview")
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		s.bodyText(c.Key("counts"), locales.WikiCompileCounts(pc.Ready, pc.Review, pc.Waiting, pc.Upgrade), t.Foreground)
		if !cv.ModelReady {
			s.noModelHint(c, cv)
		}
		s.filterBox(c)
		s.entryList(c, cv)
		items, blocks, lo, hi := cv.Estimate()
		s.bodyText(c.Key("estimate"), locales.WikiCompileEstimate(items, blocks, lo, hi), t.Foreground)
		start := l.Button(c.Key("start"), kit.ButtonSpec{Label: locales.WikiCompileStart(), Disabled: !cv.CanStart()}, s.uiPrimaryStyle()).Self(ui.Start)
		if start.Clicked() {
			s.spawn(func() { s.Store.Compile().Start() })
		}
	})
}

// noModelHint 是没有配置 Wiki 模型时的引导：一句说明，后面跟「前往配置」（去设置页的 AI 分区，同时收起对话框）。
func (s *Screen) noModelHint(c ui.C, cv CompileView) {
	l := s.Look
	t := l.T
	ui.Flow(c.Key("no-model")).FillWidth().Items(ui.Center).Gap(l.Space(2)).LineGap(0).Children(func() {
		l.Text(c.Key("text"), locales.WikiCompileNoModel(), kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped()).Shrink(1).MinWidth(0)
		if s.Hooks.OpenSettingsAI == nil {
			return
		}
		if s.textLink(c.Key("configure"), locales.ArticleAskConfigure(), kit.TextSM, true, 0) {
			s.Store.Compile().Close()
			s.Hooks.OpenSettingsAI()
		}
	})
}

// filterBox 是按标题筛选资料的输入框：w-full rounded border border-border bg-background px-3 py-2。
func (s *Screen) filterBox(c ui.C) {
	l := s.Look
	t := l.T
	d := &s.compile
	c = c.Key("filter")
	height := l.Rem(2.25) + 2 // py-2（rem 计，16）+ text-sm 行高 20（rem 计）+ 上下边框 2（固定 px）= 38；小字号档 33.5
	ui.Stack(c).FillWidth().Height(height).Children(func() {
		ui.Row(c.Key("surface")).Absolute().Inset(0).Draw(func(dc *ui.DrawContext) {
			l.PaintSurface(dc, kit.Surface{Bg: t.Background, Border: t.Border, BorderW: 1, Radius: l.Rem(0.25)})
		})
		label := locales.WikiCompileFilter()
		placeholder := locales.WikiCompileFilterPlaceholder()
		l.Input(c.Key("input"), &d.filter, label, placeholder, kit.InputSpec{
			Size: l.Rem(0.875), Weight: kit.WeightNormal, Color: t.Foreground, Placeholder: tokens.Tailwind("gray-400"), // 渲染层的 <input> 没有 placeholder 类：Chromium 的默认占位色 gray-400
			Height: height, Left: l.Space(3) + 1, Right: l.Space(3) + 1, PadTop: l.Space(2) + 1, LineHeight: l.Rem(1.25), // 内容区恰是一行高：<input> 的内部编辑器顶 = 框顶 + py-2 + 边框
		}).Absolute().Inset(0)
	})
}

// entryList 是资料列表：max-h-60 space-y-2 overflow-auto rounded border border-border p-3。
// 每条是复选框（标题、分块数与可复用块数）加可选的原因；不可选的状态禁用；列表尾有「显示更多资料」。
func (s *Screen) entryList(c ui.C, cv CompileView) {
	l := s.Look
	t := l.T
	c = c.Key("entries")
	shown := cv.Shown()
	l.ScrollArea(c, [4]float32{l.Space(3) + 1, l.Space(3) + 1, l.Space(3) + 1, l.Space(3) + 1}, func() {
		ui.Column(c.Key("col")).FillWidth().Gap(l.Space(2)).Children(func() {
			for _, e := range shown {
				s.entryRow(c, cv, e)
			}
			if cv.HasMore() {
				more := l.Button(c.Key("more"), kit.ButtonSpec{Label: locales.WikiCompileShowMore()}, l.UIGhostButton(kit.UIButtonMD))
				if more.Clicked() {
					s.Store.Compile().ShowMore()
				}
			}
		})
	}).FillWidth().MaxHeight(l.Rem(15)).Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.25)})
	})
}

// entryRow 是一条资料：复选框（可选且没有任务在跑才能勾）与它下面缩进的原因（ml-6 text-xs muted）。
func (s *Screen) entryRow(c ui.C, cv CompileView, e services.WikiCompileCandidate) {
	l := s.Look
	t := l.T
	c = c.Key("entry-" + e.ID)
	title := e.Title
	if title == "" {
		title = locales.LibraryUntitled()
	}
	label := title + " · " + locales.WikiCompileEntryBlocks(e.Blocks)
	if e.Reusable > 0 {
		label += locales.WikiCompileEntryReusable(e.Reusable)
	}
	checked := cv.IsSelected(e.ID)
	before := checked
	ui.Column(c).FillWidth().Children(func() {
		l.Checkbox(c.Key("check"), &checked, kit.CheckboxSpec{Label: label, Disabled: cv.Busy || !e.Selectable()})
		if e.Reason != "" {
			l.Text(c.Key("reason"), e.Reason, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).
				PadSides(0, 0, 0, l.Space(6)).FillWidth()
		}
	})
	if checked != before {
		s.Store.Compile().Toggle(e.ID, checked)
	}
}

// compileJobs 是任务列表：border-t border-border pt-3 space-y-3，最多列最近 10 个。
func (s *Screen) compileJobs(c ui.C, cv CompileView) {
	l := s.Look
	t := l.T
	c = c.Key("jobs")
	ui.Column(c).FillWidth().Gap(l.Space(3)).PadSides(l.Space(3)+1, 0, 0, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).
		Children(func() {
			l.Text(c.Key("title"), locales.WikiCompileJobs(), kit.TextSM, kit.WeightMedium, t.Foreground)
			for _, job := range cv.JobsShown() {
				s.jobCard(c, cv, job)
			}
		})
}

// jobStatusLabel 是任务状态的名称；未知状态原样显示。
func jobStatusLabel(status string) string {
	switch status {
	case services.WikiJobRunning:
		return locales.WikiCompileStatusRunning()
	case services.WikiJobPaused:
		return locales.WikiCompileStatusPaused()
	case services.WikiJobCancelled:
		return locales.WikiCompileStatusCancelled()
	case services.WikiJobInterrupted:
		return locales.WikiCompileStatusInterrupted()
	case services.WikiJobFailed:
		return locales.WikiCompileStatusFailed()
	case services.WikiJobCompleted:
		return locales.WikiCompileStatusCompleted()
	}
	return status
}

// jobCard 是一个任务：rounded border border-border p-3 space-y-2。状态加进度（有失败块时加失败数）、失败原因，
// 运行中给「暂停后续请求」与「取消任务」，没完成的其余任务给「继续未完成分块」。
func (s *Screen) jobCard(c ui.C, cv CompileView, job services.WikiCompileJob) {
	l := s.Look
	t := l.T
	c = c.Key("job-" + job.ID)
	line := jobStatusLabel(job.Status) + " · " + locales.WikiCompileJobProgress(job.Completed, job.Total)
	if job.Failed > 0 {
		line += " · " + locales.WikiCompileJobFailed(job.Failed)
	}
	ui.Column(c).FillWidth().Gap(l.Space(2)).Pad(l.Space(3) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.25)})
		}).Children(func() {
		s.bodyText(c.Key("line"), line, t.Foreground)
		if job.Error != "" {
			s.bodyText(c.Key("error"), job.Error, t.Destructive)
		}
		id := job.ID
		switch {
		case job.Status == services.WikiJobRunning:
			ui.Row(c.Key("actions")).Gap(l.Space(2)).Children(func() {
				pause := l.Button(c.Key("pause"), kit.ButtonSpec{Label: locales.WikiCompilePause(), NoRing: true}, l.UISecondaryButton(kit.UIButtonMD))
				if pause.Clicked() {
					s.spawn(func() { s.Store.Compile().Control(id, services.WikiJobPaused) })
				}
				cancel := l.Button(c.Key("cancel"), kit.ButtonSpec{Label: locales.WikiCompileCancel()}, l.UIGhostButton(kit.UIButtonMD))
				if cancel.Clicked() {
					s.spawn(func() { s.Store.Compile().Control(id, services.WikiJobCancelled) })
				}
			})
		case job.Resumable():
			resume := l.Button(c.Key("resume"), kit.ButtonSpec{Label: locales.WikiCompileResume(), Disabled: cv.Busy, NoRing: true}, l.UISecondaryButton(kit.UIButtonMD))
			if resume.Clicked() {
				s.spawn(func() { s.Store.Compile().Control(id, services.WikiJobRunning) })
			}
		}
	})
}
