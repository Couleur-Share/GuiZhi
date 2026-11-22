package settings

import (
	"strconv"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// funasrStep 是更新流程里的一步（渲染层 FunasrUpdateProgress 的 steps）。
type funasrStep struct {
	id, label, title, hint string
}

func funasrSteps(prog *services.FunasrProgress) []funasrStep {
	prepare := locales.SettingsFunasrUpdatePreparing()
	if prog != nil && prog.Phase == services.FunasrPhasePrepare {
		prepare = locales.SettingsFunasrUpdateStorage()
	}
	return []funasrStep{
		{services.FunasrPhasePrepare, locales.SettingsFunasrStepPrepare(), prepare, locales.SettingsFunasrUpdatePreparingHint()},
		{services.FunasrPhaseBackup, locales.SettingsFunasrStepBackup(), locales.SettingsFunasrPhaseBackup(), locales.SettingsFunasrUpdateBackupHint()},
		{services.FunasrPhaseDeps, locales.SettingsFunasrStepUpdate(), locales.SettingsFunasrUpdateDependencies(), locales.SettingsFunasrUpdateDependenciesHint()},
		{services.FunasrPhaseVerify, locales.SettingsFunasrStepVerify(), locales.SettingsFunasrPhaseVerify(), locales.SettingsFunasrUpdateVerifyHint()},
	}
}

// updatePanel 是更新期间主行下方的分阶段进度（FunasrUpdateProgress）：更新阶段是实际事件，没有总量的阶段只报耗时，不推算完成百分比。
// mx-4 mb-3 space-y-3 rounded-xl border border-primary/20 bg-primary/5 p-4。
func (f *funasrEngine) updatePanel(c ui.C) {
	l := f.look
	t := l.T
	prog := f.prog
	steps := funasrSteps(prog)
	rollback := prog != nil && prog.Phase == services.FunasrPhaseRollback
	index := -1
	phase := services.FunasrPhasePrepare
	if prog != nil {
		phase = prog.Phase
	}
	for i, s := range steps {
		if s.id == phase {
			index = i
		}
	}
	cur := steps[max(index, 0)]
	title, hint := cur.title, cur.hint
	if rollback {
		title, hint = locales.SettingsFunasrPhaseRollback(), locales.SettingsFunasrUpdateRollbackHint()
	}

	outer := ui.Row(c).FillWidth().PadSides(0, l.Space(4), l.Space(3), l.Space(4))
	outer.Children(func() {
		ui.Column(c.Key("panel")).FillWidth().Gap(l.Space(3)).Pad(l.Space(4) + 1).
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.05), Border: t.Primary.WithAlpha(0.2), BorderW: 1, Radius: l.Rem(1)})
			}).Children(func() {
			ui.Row(c.Key("head")).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(2)).Children(func() {
				ui.Row(c.Key("title")).Items(ui.Center).Gap(l.Space(2)).Shrink(1).MinWidth(0).Children(func() {
					if rollback {
						l.Icon(c.Key("icon"), icons.RotateCcw, l.Rem(1), t.Primary)
					} else {
						l.Spinner(c.Key("icon"), l.Rem(1), t.Primary)
					}
					l.Text(c.Key("text"), title, kit.TextSM, kit.WeightMedium, t.Foreground, kit.Wrapped()).MinWidth(0)
				})
				l.Text(c.Key("elapsed"), locales.SettingsFunasrUpdateElapsed(elapsedText(f.now().Sub(f.since))), kit.TextXS, kit.WeightNormal, t.MutedForeground).Shrink(0)
			})
			if !rollback {
				f.stepList(c.Key("steps"), steps, index)
			}
			if prog != nil && prog.Percent != nil {
				pct := min(max(*prog.Percent, 0), 100)
				ui.Row(c.Key("percent")).FillWidth().Items(ui.Center).Gap(l.Space(3)).Children(func() {
					ui.Row(c.Key("bar")).Grow(1).MinWidth(0).Children(func() { progressBar(l, c, pct, t.Primary.WithAlpha(0.1), t.Primary) })
					l.Text(c.Key("value"), strconv.Itoa(pct)+"%", kit.TextXS, kit.WeightNormal, t.MutedForeground).Shrink(0)
				})
			}
			l.Text(c.Key("hint"), hint, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			if prog != nil && prog.Detail != "" {
				f.details(c.Key("details"), prog.Detail)
			}
		})
	})
}

// stepList 是步骤条：不窄于 640 的窗口四步并排（sm:grid-cols-4），否则两列。
func (f *funasrEngine) stepList(c ui.C, steps []funasrStep, index int) {
	l := f.look
	per := 2
	if l.ViewportWidth(c) >= 640 {
		per = 4
	}
	ui.Column(c).FillWidth().Gap(l.Space(2)).Children(func() {
		for start := 0; start < len(steps); start += per {
			ui.Row(c.Key("row" + strconv.Itoa(start))).FillWidth().Gap(l.Space(2)).Children(func() {
				for i := start; i < start+per && i < len(steps); i++ {
					f.step(c.Key(steps[i].id), steps[i], i, index)
				}
			})
		}
	})
}

// step 是一步：flex items-center gap-2 rounded-lg px-2.5 py-2 text-xs；当前步 bg-primary/10 font-medium text-primary，
// 已完成 text-foreground，未开始 muted；序号圆点 h-5 w-5，已完成画对勾。
func (f *funasrEngine) step(c ui.C, s funasrStep, i, index int) {
	l := f.look
	t := l.T
	ink, weight, bg := t.MutedForeground, kit.WeightNormal, t.Muted.WithAlpha(0)
	switch {
	case i == index:
		ink, weight, bg = t.Primary, kit.WeightMedium, t.Primary.WithAlpha(0.1)
	case i < index:
		ink = t.Foreground
	}
	dotBg, dotInk := t.Muted, ink
	if i <= index {
		dotBg, dotInk = t.Primary.WithAlpha(0.15), t.Primary
	}
	ui.Row(c).Grow(1).Basis(0).MinWidth(0).Items(ui.Center).Gap(l.Space(2)).PadSides(l.Space(2), l.Space(2.5), l.Space(2), l.Space(2.5)).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: bg, Radius: l.Rem(0.75)}) }).
		Children(func() {
			ui.Row(c.Key("dot")).Width(l.Rem(1.25)).Height(l.Rem(1.25)).Shrink(0).Items(ui.Center).Justify(ui.Center).
				Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: dotBg, Radius: d.Rect.Height / 2}) }).
				Children(func() {
					if i < index {
						l.Icon(c.Key("check"), icons.Check, l.Rem(0.75), dotInk)
						return
					}
					l.Text(c.Key("n"), strconv.Itoa(i+1), kit.TextXS, kit.WeightNormal, dotInk)
				})
			l.Text(c.Key("label"), s.label, kit.TextXS, weight, ink)
		})
}

// details 是「查看运行详情」折叠块：展开后是 max-h-28 的等宽原文（可滚动在渲染层；这里按高度裁切）。
func (f *funasrEngine) details(c ui.C, detail string) {
	l := f.look
	t := l.T
	ui.Column(c).FillWidth().Children(func() {
		if summaryRow(l, c.Key("summary"), locales.SettingsFunasrUpdateDetails(), f.detail, t.MutedForeground) {
			f.detail = !f.detail
		}
		if !f.detail {
			return
		}
		ui.Column(c.Key("pre")).FillWidth().MaxHeight(l.Rem(7)).Clip().Pad(l.Space(2)).MarginTop(l.Space(2)).
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.5), Radius: l.Rem(0.75)})
			}).
			Children(func() {
				l.TextBreakAll(c, detail, kit.TextXS, kit.WeightNormal, t.MutedForeground)
			})
	})
}
