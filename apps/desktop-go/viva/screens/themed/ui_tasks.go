package themed

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 主题页任务列表（ThemedReadingTasks.tsx）：处理中心的「主题页任务」页。任务的状态卡与阅读面板共用（ui_status.go）。

// tasksUI 记任务列表是否在显示：这一帧画了才算挂载，下一帧没画就退订（EndFrame）。
type tasksUI struct {
	shown, attached bool
}

// TasksPage 画任务列表页（填满父容器）。back 是「返回处理中心」。每一帧由处理中心在它显示这一页时调用。
func (s *Screen) TasksPage(c ui.C, back func()) {
	l := s.Look
	t := l.T
	u := &s.tasks
	u.shown = true
	if !u.attached {
		u.attached = true
		s.Tasks.Attach()
	}
	v := s.Tasks.View()
	c = c.Key("themed-tasks")
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		// 页眉：h-12 border-b px-5，返回、标题、刷新。
		ui.Row(c.Key("head")).FillWidth().Height(l.Rem(3)).Shrink(0).Items(ui.Center).Gap(l.Space(3)).
			PadSides(0, l.Space(5), 1, l.Space(5)).Draw(func(d *ui.DrawContext) {
			l.PaintEdge(d, kit.EdgeBottom, 1, t.Border)
		}).Children(func() {
			if l.Button(c.Key("back"), kit.ButtonSpec{Label: locales.ThemedReadingBackToInbox()}, s.compactButton()).Clicked() && back != nil {
				back()
			}
			ui.Row(c.Key("title")).Grow(1).Children(func() {
				l.Heading(c.Key("h2"), 2, locales.ThemedReadingTasks(), kit.TextSM, kit.WeightSemibold, t.Foreground)
			})
			if l.Button(c.Key("refresh"), kit.ButtonSpec{Label: locales.CommonRefresh(), Disabled: v.Loading}, s.compactButton()).Clicked() {
				s.spawn(s.Tasks.Reload)
			}
		})
		switch {
		case v.Error != "":
			if l.LoadErrorState(c.Key("error"), v.Error) {
				s.spawn(s.Tasks.Reload)
			}
		case v.Loading:
			ui.Column(c.Key("loading")).FillWidth().Pad(l.Space(6)).Children(func() {
				s.statusLine(c.Key("text"), locales.ThemedReadingLoadingTasks())
			})
		default:
			l.ScrollArea(c.Key("scroll"), [4]float32{l.Space(5), l.Space(5), l.Space(5), l.Space(5)}, func() {
				s.taskCards(c.Key("cards"), v)
			}).Grow(1)
		}
	})
}

// taskCards 是任务卡片列表；没有任务时是一句引导。
func (s *Screen) taskCards(c ui.C, v TaskListView) {
	l := s.Look
	t := l.T
	ui.Column(c).FillWidth().Gap(l.Space(3)).Children(func() {
		if v.Empty() {
			ui.Row(c.Key("empty")).FillWidth().PadSides(l.Space(12), 0, l.Space(12), 0).Justify(ui.Center).Children(func() {
				l.Text(c.Key("text"), locales.ThemedReadingEmptyTasks(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped())
			})
			return
		}
		for _, task := range v.Tasks {
			s.taskCard(c.Key("task-"+task.ID), task, v.IsBusy(task.ID))
		}
	})
}

// taskCard 是一条任务：标题与来源、「查看主题页」，下面是状态卡。
func (s *Screen) taskCard(c ui.C, task services.ThemedTask, busy bool) {
	l := s.Look
	t := l.T
	kind := locales.LibraryBodySection()
	if task.SourceKind == "summary" {
		kind = locales.LibraryForumSummarySection()
	}
	id := task.ID
	// <article className="space-y-3 rounded-xl border border-border p-4">
	s.cardWith(c, l.Space(4)+1, l.Space(3), func() {
		ui.Row(c.Key("head")).FillWidth().Gap(l.Space(3)).Children(func() {
			ui.Column(c.Key("info")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				l.Heading(c.Key("title"), 3, task.Title, kit.TextSM, kit.WeightMedium, t.Foreground, kit.Wrapped())
				s.smallText(c.Key("kind"), kind).MarginTop(l.Space(1))
			})
			if l.Button(c.Key("open"), kit.ButtonSpec{Label: locales.ThemedReadingOpen()}, s.compactButton()).Clicked() {
				s.Tasks.Open(id)
			}
		})
		s.statusCard(c.Key("status"), task, StatusOptions{ReadOnly: false, Expanded: false, HasPage: false}, statusActions{
			Busy:   busy,
			Stop:   func() { s.spawn(func() { s.Tasks.Cancel(id) }) },
			Resume: func() { s.spawn(func() { s.Tasks.Resume(id) }) },
		})
	})
}

// endTasks 在每一帧结束时检查：这一帧没画任务列表，就退订进度并复位。
func (s *Screen) endTasks() {
	u := &s.tasks
	if !u.shown && u.attached {
		u.attached = false
		s.Tasks.Detach()
	}
	u.shown = false
}
