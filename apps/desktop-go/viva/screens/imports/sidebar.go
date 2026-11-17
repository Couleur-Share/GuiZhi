package imports

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// filterRowSpec 是侧栏筛选组里的一行（渲染层 SidebarImportsPanel 的 rows）。
type filterRowSpec struct {
	filter Filter
	label  string
	icon   gfx.Icon
	// tint 是图标自带的颜色（text-emerald-500 等）；零值表示图标取行的文字色（currentColor）。
	tint tokens.Color
	// spin 为真时图标是转圈的加载指示（进行中且有任务在跑：animate-spin text-primary）。
	spin bool
}

// filterRows 按渲染层的顺序给出七档筛选。degraded 紧跟 completed：它是那一档的子集，隔开摆会让人以为是另一种终态。
func (s *Screen) filterRows(counts Counts) []filterRowSpec {
	l := s.Look
	t := l.T
	rows := []filterRowSpec{
		{filter: FilterAll, label: locales.ImportsPanelAll(), icon: icons.Inbox},
		{filter: FilterActive, label: locales.ImportsPanelActive(), icon: icons.LoaderCircle},
		{filter: FilterCompleted, label: locales.ImportsPanelCompleted(), icon: icons.CircleCheck, tint: tokens.Tailwind("emerald-500")},
		{filter: FilterDegraded, label: locales.ImportsPanelDegraded(), icon: icons.TriangleAlert, tint: tokens.Tailwind("amber-500")},
		{filter: FilterDuplicate, label: locales.ImportsPanelDuplicate(), icon: icons.Copy, tint: tokens.Tailwind("amber-500")},
		{filter: FilterFailed, label: locales.ImportsPanelFailed(), icon: icons.CircleX, tint: t.Destructive},
		{filter: FilterCanceled, label: locales.ImportsPanelCanceled(), icon: icons.Ban},
	}
	rows[1].spin = counts.Active > 0
	if rows[1].spin {
		rows[1].tint = t.Primary
	}
	return rows
}

// Panel 是二级侧栏里的内容（渲染层 SidebarImportsPanel）：采集入口与状态筛选。
// 这几行原本只是计数展示，长得像导航却点不动；现在它们就是列表的筛选器。
//
// 外层是 flex h-full min-h-0 flex-col overflow-y-auto px-2 pb-3：内容溢出时在这里滚动。
func (s *Screen) Panel(c ui.C) {
	l := s.Look
	t := l.T
	v := s.Store.View()
	l.ScrollArea(c.Key("panel-scroll"), [4]float32{0, l.Space(2), l.Space(3), l.Space(2)}, func() {
		// 两枚入口按钮：space-y-1.5 pt-3
		ui.Column(c.Key("entry")).FillWidth().Gap(l.Space(1.5)).PadTop(l.Space(3)).Children(func() {
			s.entryButtons(c)
		})

		// 「任务概览」分组标题：flex items-center gap-2 px-3 pb-1 pt-4，右边一条 h-px 的分隔线。
		ui.Row(c.Key("overview")).FillWidth().Items(ui.Center).Gap(l.Space(2)).
			PadSides(l.Space(4), l.Space(3), l.Space(1), l.Space(3)).Children(func() {
			l.TextTracked(c.Key("overview-label"), locales.ImportsPanelOverview(), kit.TextXS, kit.WeightMedium,
				t.SidebarForeground.WithAlpha(0.8), 0.025)
			ui.Row(c.Key("overview-rule")).Grow(1).Height(1).Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.SidebarBorder.WithAlpha(0.6)})
			})
		})

		// 筛选行：space-y-0.5
		ui.Column(c.Key("filters")).FillWidth().Gap(l.Space(0.5)).Children(func() {
			for _, row := range s.filterRows(v.Counts) {
				s.filterRow(c, row, v.Counts.Of(row.filter), v.Filter == row.filter)
			}
		})
	}).Grow(1).FillWidth().MinHeight(0)
}

// entryButtons 是侧栏顶部的两枚入口：实心的「快速采集」与虚线框的「采集文件」。
func (s *Screen) entryButtons(c ui.C) {
	l := s.Look
	t := l.T
	lg := l.Rem(0.75)
	// flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-3 py-2 text-sm font-medium text-primary-foreground
	// transition-colors hover:bg-primary/90
	capture := l.Button(c.Key("capture"), kit.ButtonSpec{Label: locales.ImportsPanelCapture(), Icon: icons.Plus, Full: true}, kit.ButtonStyle{
		PadX: l.Space(3), PadY: l.Space(2), Radius: lg, Gap: l.Space(2), Text: kit.TextSM, Weight: kit.WeightMedium,
		Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault,
	})
	if capture.Clicked() && s.Hooks.QuickCapture != nil {
		s.Hooks.QuickCapture()
	}
	// flex w-full items-center justify-center gap-2 rounded-lg border border-dashed border-sidebar-border px-3 py-2 text-sm
	// text-sidebar-foreground/70 transition-colors hover:border-primary/50 hover:text-sidebar-foreground
	files := l.Button(c.Key("files"), kit.ButtonSpec{Label: locales.HeaderNewImportFiles(), Icon: icons.FolderOpen, Full: true}, kit.ButtonStyle{
		PadX: l.Space(3), PadY: l.Space(2), Radius: lg, Gap: l.Space(2), Text: kit.TextSM, Weight: kit.WeightNormal,
		Fg: t.SidebarForeground.WithAlpha(0.7), FgHover: t.SidebarForeground,
		Border: t.SidebarBorder, BorderHover: t.Primary.WithAlpha(0.5), BorderW: 1, Dashed: true,
		Transition: kit.DurationDefault,
	})
	if files.Clicked() {
		s.importFiles()
	}
}

// filterRow 是一行筛选：图标、名称与计数徽标。整行是切换钮（aria-pressed）。
//
// flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-sm transition-colors duration-smooth；
// 选中 bg-primary/15 text-foreground，否则 text-sidebar-foreground/80，悬停 bg-sidebar-accent/50 text-sidebar-foreground。
func (s *Screen) filterRow(c ui.C, row filterRowSpec, count int, active bool) {
	l := s.Look
	t := l.T
	c = c.Key("filter-" + string(row.filter))
	id := ui.ID(c)
	box := ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2)).
		PadSides(l.Space(1.5), l.Space(3), l.Space(1.5), l.Space(3)).
		HitWith(id, row.label+" "+strconv.Itoa(count), gfx.HitOptions{Role: gfx.RoleButton, Selected: active, Cursor: gfx.PointerCursorHand})

	// 选中与悬停各用一个进度量：悬停中点选时底色从 accent/50 补间到 primary/15，与 CSS 的过渡一致。
	ph := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationSmooth)
	pa := l.HoverProgress(c, id, 1, active, kit.DurationSmooth)
	bg := kit.MixToken(kit.MixToken(tokens.Transparent, t.SidebarAccent.WithAlpha(0.5), ph), t.Primary.WithAlpha(0.15), pa)
	fg := kit.MixToken(kit.MixToken(t.SidebarForeground.WithAlpha(0.8), t.SidebarForeground, ph), t.Foreground, pa)

	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.75))
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Radius: l.Rem(0.75)})
	}).Children(func() {
		// 图标槽：flex h-4 w-4 shrink-0 items-center justify-center
		tint := fg
		if row.tint != (tokens.Color{}) {
			tint = row.tint
		}
		if row.spin {
			l.Spinner(c.Key("icon"), l.Rem(1), tint)
		} else {
			l.Icon(c.Key("icon"), row.icon, l.Rem(1), tint)
		}
		weight := kit.WeightNormal
		if active {
			weight = kit.WeightMedium
		}
		// 名称：min-w-0 flex-1 truncate text-left
		l.Text(c.Key("label"), row.label, kit.TextSM, weight, fg, kit.Ellipsis()).Grow(1).Shrink(1).MinWidth(0)
		s.countChip(c, count, active)
	})
	if box.Clicked() {
		s.Store.SetFilter(s.Ctx, row.filter)
	}
}

// countChip 是计数徽标：rounded-full border px-1.5 py-0.5 text-xs tabular-nums。
func (s *Screen) countChip(c ui.C, count int, active bool) {
	l := s.Look
	t := l.T
	bg, border, fg := t.SidebarAccent.WithAlpha(0.8), t.SidebarBorder, t.SidebarForeground.WithAlpha(0.8)
	if active {
		bg, border, fg = t.Primary.WithAlpha(0.1), t.Primary.WithAlpha(0.2), t.Foreground
	}
	ui.Row(c.Key("count")).Shrink(0).Items(ui.Center).PadSides(l.Space(0.5)+1, l.Space(1.5)+1, l.Space(0.5)+1, l.Space(1.5)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: 9999})
		}).Children(func() {
		l.Text(c, strconv.Itoa(count), kit.TextXS, kit.WeightNormal, fg)
	})
}
