package themed

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// 阅读面板（ThemedReadingPane.tsx）：知识库详情里「AI 阅读」版本的工具栏、提示条与主体。
// 版面判断在 PaneModel（reading_pane.go）；页面本身交给阅读岛（IslandView），岛画不了的状态（读取中、失败、任务进度、空态）在这里用原生控件画。

// toolsUI 是「阅读页设置」菜单与资料面板的界面状态。
type toolsUI struct {
	menu  kit.MenuState
	open  bool
	at    gfx.Point
	btn   gfx.Rect // 菜单按钮上一帧的外框：菜单的锚点与 Keep
	panel kit.ModalState
}

// Toolbar 画阅读工具条里属于 AI 阅读的那几个控件（知识库的工具条在「编辑」之后调用）：
// 查看新稿 / 返回当前版、切换到新版、「阅读页设置」菜单（有页面时）或「重新生成」（没有页面但有已结束的任务时）。
func (s *Screen) Toolbar(c ui.C) {
	if !s.Active() || !s.pane.Toolbar.Show {
		return
	}
	l := s.Look
	tb := s.pane.Toolbar
	c = c.Key("themed-toolbar")
	style := s.compactButton()
	if tb.PreviewToggle {
		label := locales.ThemedReadingReaderPreviewDraft()
		if tb.PreviewOpen {
			label = locales.ThemedReadingReaderBackToCurrent()
		}
		spec := kit.ButtonSpec{Label: label, Icon: icons.Eye, IconSize: l.Rem(0.875)}
		if l.Button(c.Key("preview"), spec, style).Clicked() {
			s.Reading.TogglePreview()
		}
	}
	if tb.SwitchLatest {
		spec := kit.ButtonSpec{Label: locales.ThemedReadingReaderSwitchLatest(), Icon: icons.RefreshCw, IconSize: l.Rem(0.875)}
		if l.Button(c.Key("latest"), spec, style).Clicked() {
			s.Reading.AdoptLatest()
		}
	}
	if tb.StartOver {
		spec := kit.ButtonSpec{
			Label: locales.ThemedReadingReaderStartOver(), Icon: icons.RotateCcw, IconSize: l.Rem(0.875),
			Disabled: tb.StartOverDisabled,
		}
		if l.Button(c.Key("start-over"), spec, style).Clicked() {
			s.Reading.OpenSetup(SetupNew)
		}
	}
	if tb.Tools {
		s.toolsButton(c.Key("tools"), style)
	}
}

// toolsButton 是「阅读页设置」按钮：点击展开菜单，菜单锚在按钮左下。
func (s *Screen) toolsButton(c ui.C, style kit.ButtonStyle) {
	l := s.Look
	u := &s.tools
	name := locales.ThemedReadingReaderSettings()
	spec := kit.ButtonSpec{
		Label: name, Icon: icons.SlidersHorizontal, IconSize: l.Rem(0.875), Disclosure: true, On: u.open,
	}
	box := l.Button(c, spec, style)
	box = box.DrawOver(func(d *ui.DrawContext) { u.btn = d.Rect })
	if box.Clicked() {
		if u.open {
			u.open = false
		} else {
			u.open, u.at = true, gfx.Point{X: u.btn.X, Y: u.btn.Y + u.btn.Height + 4}
		}
	}
}

// Dialogs 在每一帧都要经过（对话框与菜单靠它复位）：生成设置、删除确认、「阅读页设置」菜单、资料面板。
func (s *Screen) Dialogs(c ui.C) {
	c = c.Key("themed-dialogs")
	s.setupDialog(c.Key("setup"))
	s.deleteDialog(c.Key("delete"))
	s.toolsMenu(c.Key("menu"))
	s.panelDialog(c.Key("panel"))
}

// toolsMenu 声明菜单：项来自 ToolsMenu，点击后在后台执行（Invoke 会读写服务）。
func (s *Screen) toolsMenu(c ui.C) {
	u := &s.tools
	// 工具栏不画（切回原文、编辑中、详情关了）时，没人能再点菜单按钮，菜单也不该留在屏幕上。
	if !s.Active() || !s.pane.Toolbar.Show || !s.pane.Toolbar.Tools {
		u.open = false
	}
	var items []kit.MenuItem
	if u.open {
		readOnly := s.sub.ReadOnly
		for _, it := range s.view.ToolsMenu(readOnly) {
			id := it.ID
			items = append(items, kit.MenuItem{
				Label: it.Label, Disabled: it.Disabled, Destructive: it.Destructive, Icon: toolIcon(id),
				OnSelect: func() { s.spawn(func() { s.Reading.Invoke(id, readOnly) }) },
			})
		}
	}
	keep := u.btn
	if s.Look.ContextMenu(c, &u.menu, kit.MenuSpec{Open: u.open, X: u.at.X, Y: u.at.Y, Items: items, Keep: &keep}) {
		u.open = false
	}
}

func toolIcon(id ToolID) gfx.Icon {
	switch id {
	case ToolAdjust:
		return icons.Palette
	case ToolRegenerate:
		return icons.RefreshCw
	case ToolReferences:
		return icons.Globe
	case ToolAssets:
		return icons.Image
	case ToolExport, ToolExportText, ToolExportStatic:
		return icons.Download
	case ToolRestore:
		return icons.History
	case ToolNotes:
		return icons.Info
	case ToolDelete:
		return icons.Trash2
	}
	return gfx.Icon{}
}

// deleteDialog 是「删除 AI 阅读页」确认：删除期间确认钮转圈；失败保留对话框（错误提示已弹出），成功后退回原文。
func (s *Screen) deleteDialog(c ui.C) {
	switch s.Look.ConfirmDialog(c, &s.del, kit.ConfirmSpec{
		Open: s.attached && s.view.DeleteConfirm, Title: locales.ThemedReadingReaderDeleteTitle(),
		Message: locales.ThemedReadingDeleteHint(), ConfirmText: locales.ThemedReadingReaderDelete(),
		CancelText: locales.CommonCancel(), Destructive: true, Loading: s.view.Busy,
	}) {
	case kit.ConfirmAccepted:
		s.spawn(func() { s.Reading.ConfirmDelete() })
	case kit.ConfirmDismissed:
		s.Reading.CancelDelete()
	}
}

// ---- 阅读区 ----

// Area 是阅读区这一格（知识库在 AI 阅读时用它代替原文视图）：提示条在上，主体在下。
// island 是知识库提供的岛挂载函数（nil 表示没有宿主：对照与单测）：每一帧都调用一次，有页面文档时给 ViewSnapshot，
// 其余给 ViewNone（让岛收起）；它自己决定怎样占位。
func (s *Screen) Area(c ui.C, island func(c ui.C, v reader.View)) {
	if !s.bound {
		return
	}
	l := s.Look
	c = c.Key("themed-area")
	p := s.pane
	view, live := s.IslandView()
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		s.notices(c.Key("notices"))
		if live {
			if island != nil {
				island(c.Key("island"), view)
			} else {
				s.pageShell(c.Key("shell"))
			}
			return
		}
		if island != nil {
			island(c.Key("island"), reader.View{Kind: reader.ViewNone})
		}
		ui.Column(c.Key("body")).FillWidth().Grow(1).MinHeight(0).Items(ui.Center).Justify(ui.Center).
			Pad(l.Space(4)).Children(func() { s.nativeBody(c.Key("native"), p) })
	})
}

// notices 是页面上方的几条提示：只读、任务状态条、原文已更新、旧格式只读、图片省略。
func (s *Screen) notices(c ui.C) {
	p := s.pane
	l := s.Look
	show := p.ReadOnlyNote || (p.StatusStrip && s.view.Task != nil) || p.StaleNote || p.LegacyNote || s.docOmitted()
	if !show {
		return
	}
	ui.Column(c).FillWidth().Gap(l.Space(2)).PadSides(l.Space(3), l.Space(4), 0, l.Space(4)).Children(func() {
		if p.ReadOnlyNote {
			s.noticeLine(c.Key("readonly"), locales.ThemedReadingReaderReadOnly())
		}
		if p.StatusStrip && s.view.Task != nil {
			s.statusCard(c.Key("status"), *s.view.Task, s.statusOptions(false), s.statusActions())
		}
		if p.StaleNote {
			s.noticeLine(c.Key("stale"), locales.ThemedReadingReaderStale())
		}
		if p.LegacyNote {
			s.noticeLine(c.Key("legacy"), locales.ThemedReadingReaderLegacyReadOnly())
		}
		if s.docOmitted() {
			s.noticeLine(c.Key("omitted"), locales.NativeThemedImagesOmitted())
		}
	})
}

// docOmitted 报告这一帧的页面文档是不带图片的退路版本。
func (s *Screen) docOmitted() bool {
	return s.docWanted && s.docOK && s.doc.ImagesOmitted
}

// noticeLine 是一条提示：rounded-lg bg-muted px-3 py-2 text-xs。
func (s *Screen) noticeLine(c ui.C, text string) {
	l := s.Look
	t := l.T
	ui.Row(c).FillWidth().PadSides(l.Space(2), l.Space(3), l.Space(2), l.Space(3)).Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.Muted, Radius: l.Rem(0.5)})
	}).Children(func() {
		l.Text(c.Key("text"), text, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
	})
}

func (s *Screen) statusOptions(expanded bool) StatusOptions {
	return StatusOptions{ReadOnly: s.sub.ReadOnly, Expanded: expanded, HasPage: s.view.Page() != nil, OfflineAvailable: true}
}

// statusActions 把停止 / 继续 / 不联网继续接到状态层（都读写服务，放到后台）。
func (s *Screen) statusActions() statusActions {
	return statusActions{
		Busy:    s.view.Busy,
		Stop:    func() { s.spawn(func() { s.Reading.Cancel() }) },
		Resume:  func() { s.spawn(func() { s.Reading.Resume() }) },
		Offline: func() { s.spawn(func() { s.Reading.ContinueOffline() }) },
	}
}

// pageShell 是没有岛宿主时页面位置的占位（对照与单测）：一个文档容器，可访问名是页面的标题行，
// 好让无头测试断言「此刻显示的是 AI 阅读页」。
func (s *Screen) pageShell(c ui.C) {
	l := s.Look
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Draw(func(d *ui.DrawContext) {
		d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, s.subjectLine(), gfx.HitOptions{})
	}).Children(func() {
		l.Text(c.Key("title"), s.subjectLine(), kit.TextSM, kit.WeightMedium, l.T.MutedForeground, kit.Wrapped())
	})
}

// nativeBody 是岛画不了的主体状态。
func (s *Screen) nativeBody(c ui.C, p PaneModel) {
	l := s.Look
	switch {
	case p.ShowLoadError:
		if l.LoadErrorState(c.Key("load-error"), p.LoadError) {
			s.spawn(s.Reading.Reload)
		}
	case p.ShowLoading:
		s.statusLine(c.Key("loading"), locales.ThemedReadingLoading())
	case p.Body == BodyTaskStatus && s.view.Task != nil:
		s.statusCard(c.Key("status"), *s.view.Task, s.statusOptions(true), s.statusActions())
	case p.Body == BodyWaitDone:
		s.statusLine(c.Key("wait"), locales.ThemedReadingWaitDone())
	case p.Body == BodyEmpty:
		s.emptyState(c.Key("empty"))
	case p.Body == BodyNativePage || p.Body == BodyLegacyFrame:
		// 页面存在但文档还没到（渲染中）或渲染失败。
		doc, _ := s.pageDoc()
		switch {
		case doc.Error != "":
			if l.LoadErrorState(c.Key("doc-error"), doc.Error) {
				s.Docs.Retry()
			}
		default:
			s.statusLine(c.Key("doc-loading"), locales.ThemedReadingLoading())
		}
	}
}

// emptyState 是还没有阅读页时的引导：图标、说明与「生成阅读页」。
func (s *Screen) emptyState(c ui.C) {
	l := s.Look
	t := l.T
	disabled := s.view.Disabled(s.sub.ReadOnly) || s.view.Result == nil
	ui.Column(c).Items(ui.Center).Gap(l.Space(3)).MaxWidth(l.Rem(26)).Children(func() {
		l.Icon(c.Key("icon"), icons.Sparkles, l.Rem(2), t.Primary.WithAlpha(0.7))
		l.Text(c.Key("title"), locales.ThemedReadingReaderEmpty(), kit.TextBase, kit.WeightSemibold, t.Foreground, kit.Centered(), kit.Wrapped())
		l.Text(c.Key("hint"), locales.ThemedReadingReaderEmptyHint(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped())
		spec := kit.ButtonSpec{Label: locales.ThemedReadingReaderCreate(), Icon: icons.Sparkles, IconSize: l.Rem(0.875), Disabled: disabled}
		if l.Button(c.Key("create"), spec, s.compactPrimary()).Clicked() {
			s.Reading.OpenSetup(SetupNew)
		}
	})
}