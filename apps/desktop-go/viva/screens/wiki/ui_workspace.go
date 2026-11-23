package wiki

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// Workspace 是主区域（渲染层 WikiWorkspace）：顶栏（标题、状态行、视图切换与编译控件）和下面的内容——
// 目录加详情、关系图谱，或读取中、读取失败、还没有页面这几种占位。
//
// flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section。
func (s *Screen) Workspace(c ui.C) {
	l := s.Look
	t := l.T
	s.enter()
	s.loadPane()
	v := s.Store.View()
	ui.Column(c.Key("workspace")).FillWidth().Grow(1).MinHeight(0).Clip().
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.UiSectionBg}) }).
		Children(func() {
			s.header(c, v)
			s.body(c, v)
		})
	s.compileDialog(c)
}

// header 是顶栏：flex h-12 shrink-0 items-center gap-2 border-b border-border px-5。
// 视图控件与编译控件分属两类，之间一条竖线；数据到位之前编译控件先不画，免得首帧之后工具栏横向弹一下。
func (s *Screen) header(c ui.C, v View) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("header")).FillWidth().Shrink(0).Height(l.Rem(3)).Items(ui.Center).Gap(l.Space(2)).
		PadSides(0, l.Space(5), 1, l.Space(5)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			l.Heading(c.Key("title"), 2, locales.NavWiki(), kit.TextSM, kit.WeightSemibold, t.Foreground).Shrink(0)
			if v.Status != nil {
				l.Text(c.Key("status"), locales.WikiStatusLine(v.Status.PageCount, v.Status.CompiledItemCount, v.Status.EligibleItemCount),
					kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).Shrink(1).MinWidth(0)
			}
			ui.Spacer(c.Key("auto")).Grow(1)
			s.viewModeToggle(c, v)
			if v.HasLoaded {
				ui.Row(c.Key("sep")).Shrink(0).Width(1).Height(l.Rem(1)).Draw(func(d *ui.DrawContext) {
					d.Canvas.Fill(d.Rect, d.Fade(l.Col(t.Border)))
				})
				s.autoCompileIndicator(c)
				s.compileControls(c, v)
			}
		})
}

// viewModeToggle 是目录与图谱的切换（ViewModeToggle）：flex rounded-lg border border-border p-0.5，
// 两枚 h-6 w-7 的图标钮；选中 bg-primary/15 text-primary，否则 text-muted-foreground hover:text-foreground。
func (s *Screen) viewModeToggle(c ui.C, v View) {
	l := s.Look
	t := l.T
	c = c.Key("modes")
	ui.Row(c).Shrink(0).Items(ui.Center).Pad(l.Space(0.5) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: s.radiusLG()})
		}).Children(func() {
		modes := []struct {
			mode  ViewMode
			label string
		}{
			{ViewCatalog, locales.WikiViewCatalog()},
			{ViewGraph, locales.WikiViewGraph()},
		}
		for _, m := range modes {
			ic := icons.List
			if m.mode == ViewGraph {
				ic = icons.Network
			}
			b := l.Button(c.Key(string(m.mode)), kit.ButtonSpec{
				Name: m.label, Hint: m.label, Icon: ic, IconSize: l.Rem(0.875), Center: true, Toggle: true, On: v.Mode == m.mode,
			}, kit.ButtonStyle{
				Height: l.Rem(1.5), Width: l.Rem(1.75), Radius: s.radiusMD(),
				Fg: t.MutedForeground, FgHover: t.Foreground, FgOn: t.Primary, BgOn: t.Primary.WithAlpha(0.15),
				Transition: kit.DurationDefault,
			})
			if b.Clicked() {
				mode := m.mode
				s.spawn(func() { s.Store.SetViewMode(mode) })
			}
		}
	})
}

// autoCompileIndicator 是自动编译状态指示：开关本体在设置页，这里只在开启时留一枚只读的小块，点击回到设置页。
func (s *Screen) autoCompileIndicator(c ui.C) {
	if s.Hooks.AutoCompileEnabled == nil || !s.Hooks.AutoCompileEnabled() {
		return
	}
	l := s.Look
	t := l.T
	b := l.Button(c.Key("auto-compile"), kit.ButtonSpec{
		Label: locales.WikiAutoCompile(), Hint: locales.WikiAutoCompileOnHint(), Icon: icons.Timer, IconSize: l.Rem(0.875),
	}, kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Space(2.5), Radius: s.radiusLG(), Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightNormal,
		Fg: t.Primary, Bg: t.Primary.WithAlpha(0.1), BgHover: t.Primary.WithAlpha(0.2),
		Border: t.Primary.WithAlpha(0.4), BorderW: 1, Transition: kit.DurationDefault,
	})
	if b.Clicked() && s.Hooks.OpenSettingsGeneral != nil {
		s.Hooks.OpenSettingsGeneral()
	}
}

// compileControls 是编译控件：编译中是「编译中…」的状态块加停止钮；否则是主按钮（文案随待编译数）与有页面时的「全文编译」入口。
func (s *Screen) compileControls(c ui.C, v View) {
	l := s.Look
	t := l.T
	if v.IsCompiling {
		ui.Row(c.Key("compiling")).Shrink(1).MaxWidth(l.Rem(20)).Height(l.Rem(2)).Items(ui.Center).Gap(l.Space(1.5)).
			PadSides(0, l.Space(3), 0, l.Space(3)).
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.1), Radius: s.radiusLG()})
			}).Children(func() {
			l.SpinIcon(c.Key("spin"), icons.LoaderCircle, l.Rem(0.875), t.Primary)
			l.Text(c.Key("text"), locales.WikiCompilingShort(), kit.TextXS, kit.WeightNormal, t.Primary, kit.Ellipsis()).Shrink(1).MinWidth(0)
		})
		s.outlineIconButton(c.Key("stop"), locales.WikiStopCompile(), icons.Square, l.Rem(0.875))
		return
	}
	pending, actionable := v.CompileButton()
	label := locales.WikiCompileNow()
	if pending > 0 {
		label = locales.WikiCompilePending(pending)
	}
	spec := kit.ButtonSpec{Label: label, Icon: icons.Sparkles, IconSize: l.Rem(1), Disabled: !actionable}
	if !actionable {
		spec.Hint = locales.WikiCompileUpToDate()
	}
	b := l.Button(c.Key("compile"), spec, kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Space(3), Radius: s.radiusLG(), Gap: l.Space(1.5), Text: kit.TextSM, Weight: kit.WeightMedium,
		Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9),
		Transition: kit.DurationDefault, DisabledOpacity: 0.4,
	})
	if b.Clicked() {
		s.OpenCompileDialog()
	}
	if len(v.Catalog) > 0 {
		s.outlineIconButton(c.Key("dialog"), locales.WikiCompileDialog(), icons.RefreshCw, l.Rem(1))
	}
}

// outlineIconButton 是顶栏里的描边图标钮：h-8 w-8 rounded-lg border border-border text-muted-foreground hover:bg-muted/60 hover:text-foreground。
// 它们都是打开编译对话框的入口（编译中那枚是「停止编译」，同样进对话框操作）。
func (s *Screen) outlineIconButton(c ui.C, name string, icon gfx.Icon, size float32) {
	l := s.Look
	t := l.T
	b := l.Button(c, kit.ButtonSpec{Name: name, Hint: name, Icon: icon, IconSize: size, Center: true}, kit.ButtonStyle{
		Height: l.Rem(2), Width: l.Rem(2), Radius: s.radiusLG(),
		Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Muted.WithAlpha(0.6),
		Border: t.Border, BorderW: 1, Transition: kit.DurationDefault,
	})
	if b.Clicked() {
		s.OpenCompileDialog()
	}
}

// body 是顶栏下面的内容区：读取中、读取失败、还没有页面、关系图谱、目录加详情。
func (s *Screen) body(c ui.C, v View) {
	l := s.Look
	t := l.T
	switch {
	case !v.HasLoaded:
		// 目录读出来之前不画空态：否则每次进 Wiki 都先铺一屏「还是空的」，再被两栏布局整块顶掉。
		alpha := s.loading.Opacity(l, c.Canvas(), true)
		ui.Row(c.Key("loading")).FillWidth().Grow(1).Justify(ui.Center).Items(ui.Center).Opacity(alpha).Children(func() {
			l.Spinner(c, l.Rem(1.25), t.MutedForeground)
		})
		return
	case v.LoadError != "" && len(v.Catalog) == 0:
		s.loading.Opacity(l, c.Canvas(), false)
		ui.Column(c.Key("load-error")).FillWidth().Grow(1).Justify(ui.Center).Items(ui.Center).Children(func() {
			if l.LoadErrorState(c, v.LoadError) {
				s.spawn(func() { s.Store.Refresh() })
			}
		})
		return
	case len(v.Catalog) == 0 && !v.IsCompiling:
		s.loading.Opacity(l, c.Canvas(), false)
		s.emptyState(c)
		return
	}
	s.loading.Opacity(l, c.Canvas(), false)
	if v.Mode == ViewGraph {
		ui.Row(c.Key("graph-row")).FillWidth().Grow(1).MinHeight(0).Children(func() { s.graphView(c, v) })
		return
	}
	s.catalogAndDetail(c, v)
}

// emptyState 是还没有任何页面时的引导：flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center。
func (s *Screen) emptyState(c ui.C) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("empty")).FillWidth().Grow(1).Justify(ui.Center).Items(ui.Center).Gap(l.Space(3)).
		PadSides(0, l.Space(6), 0, l.Space(6)).Children(func() {
		l.Icon(c.Key("icon"), icons.Network, l.Rem(2.5), t.MutedForeground.WithAlpha(0.4))
		l.Text(c.Key("title"), locales.WikiEmpty(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Centered())
		l.Text(c.Key("hint"), locales.WikiEmptyHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped()).MaxWidth(l.Rem(28))
	})
}

// catalogAndDetail 是目录加详情的两栏：左栏定宽、右缘 1px 分隔线并叠一条可拖的把手，右栏是页面详情。
func (s *Screen) catalogAndDetail(c ui.C, v View) {
	l := s.Look
	t := l.T
	if !s.pane.Dragging() {
		s.pane.Width = s.stored
	}
	ui.Row(c.Key("two-pane")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Row(c.Key("catalog-pane")).Width(s.pane.Width).Shrink(0).MinHeight(0).FillHeight().PadSides(0, 1, 0, 0).
			Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeRight, 1, t.Border) }).
			Children(func() {
				ui.Column(c.Key("catalog-col")).Grow(1).MinWidth(0).MinHeight(0).FillHeight().Children(func() { s.catalogList(c, v) })
				// absolute inset-y-0 right-0 z-10 flex：里面一条 ColumnResizer。
				ui.Row(c.Key("catalog-resizer")).Absolute().Top(0).Right(0).Bottom(0).Children(func() {
					l.ColumnResizer(c, &s.pane, kit.ColumnResizerSpec{
						Name: locales.WikiResizeCatalog(), Min: float32(catalogPaneBounds.Min), Max: float32(catalogPaneBounds.Max),
						Default: float32(catalogPaneBounds.Default), OnCommit: s.savePaneWidth,
					})
				})
			})
		s.pageDetail(c, v)
	})
}
