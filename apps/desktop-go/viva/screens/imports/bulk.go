package imports

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// bulkBar 是批量操作条（渲染层 ImportsBulkBar）：有选中项时出现在筛选条下面。
// 按选中项的实际状态分组：重试只作用于失败或已取消的任务——队列的 retry 会把已完成任务重新跑一遍，
// 一股脑全发过去等于重复采集。
//
// flex h-9 shrink-0 items-center gap-1 border-b border-border bg-primary/5 px-4。
func (s *Screen) bulkBar(c ui.C, selected []services.ImportTask) {
	l := s.Look
	t := l.T
	var retryable, cancelable, removable []string
	for _, task := range selected {
		switch {
		case IsActive(task):
			cancelable = append(cancelable, task.ID)
		case task.Status == services.ImportStatusFailed || task.Status == services.ImportStatusCanceled:
			retryable = append(retryable, task.ID)
			removable = append(removable, task.ID)
		default:
			removable = append(removable, task.ID)
		}
	}

	ui.Row(c.Key("bulk")).FillWidth().Shrink(0).Height(l.Rem(2.25)).Items(ui.Center).Gap(l.Space(1)).
		PadSides(0, l.Space(4), 1, l.Space(4)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.05)})
			l.PaintEdge(d, kit.EdgeBottom, 1, t.Border)
		}).
		Children(func() {
			// 取消选择：inline-flex h-7 w-7 rounded-md text-muted-foreground hover:bg-muted/60 hover:text-foreground
			label := locales.ImportsClearSelection()
			size := l.Rem(1.75)
			clearBtn := l.Button(c.Key("clear"), kit.ButtonSpec{Name: label, Icon: icons.X, Center: true, Hint: label}, kit.ButtonStyle{
				Height: size, Width: size, Radius: l.Rem(0.75) - 2,
				Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationDefault,
			})
			if clearBtn.Clicked() {
				s.Store.ClearSelection()
			}

			// min-w-0 flex-1 truncate text-xs font-medium text-foreground
			l.Text(c.Key("count"), locales.ImportsSelectedCount(len(selected)), kit.TextXS, kit.WeightMedium, t.Foreground, kit.Ellipsis()).
				Grow(1).Shrink(1).MinWidth(0)

			if s.bulkAction(c, "select-visible", locales.ImportsSelectVisible(), -1, false, icons.ListChecks) {
				s.Store.SelectVisible()
			}
			if len(retryable) > 0 && s.bulkAction(c, "retry", locales.ImportsRetry(), len(retryable), false, icons.RotateCcw) {
				ids := retryable
				s.Store.Spawn(func() { s.Store.RetryTasks(s.Ctx, ids) })
			}
			if len(cancelable) > 0 && s.bulkAction(c, "cancel", locales.CommonCancel(), len(cancelable), false, icons.Ban) {
				ids := cancelable
				s.Store.Spawn(func() { s.Store.CancelTasks(s.Ctx, ids) })
			}
			if len(removable) > 0 && s.bulkAction(c, "remove", locales.ImportsRemoveTask(), len(removable), true, icons.X) {
				s.Store.RemoveWithUndo(s.Ctx, removable)
			}
		})
}

// bulkAction 是批量条里的一枚动作：inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2 text-xs transition-colors，
// 图标、名称与（有的话）灰色的计数；危险的删除是 text-destructive hover:bg-destructive/10，其余 text-foreground hover:bg-muted/60。
// count 为负表示没有计数。返回是否被点击。
func (s *Screen) bulkAction(c ui.C, key, label string, count int, destructive bool, icon gfx.Icon) bool {
	l := s.Look
	t := l.T
	spec := kit.ButtonSpec{Label: label, Icon: icon, SuffixColor: t.MutedForeground}
	if count >= 0 {
		spec.Suffix = strconv.Itoa(count)
	}
	st := kit.ButtonStyle{
		Height: l.Rem(1.75), PadX: l.Space(2), Radius: l.Rem(0.75) - 2, Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightNormal,
		Fg: t.Foreground, BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationDefault,
	}
	if destructive {
		st.Fg, st.BgHover = t.Destructive, t.Destructive.WithAlpha(0.1)
	}
	return l.Button(c.Key(key), spec, st).Clicked()
}
