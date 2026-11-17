package imports

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// rowAction 是行尾浮出的动作条里的一枚图标按钮。
type rowAction struct {
	key         string
	label       string
	icon        gfx.Icon
	destructive bool
	run         func()
}

// rowActions 按渲染层的顺序列出一个任务当前可做的动作：取消、重试、登录态重试、详情、装工具、打开条目、更新来源、仍要创建副本、删除。
//
// 失败与取消的任务多半没有耗时摘要可点，而它们恰恰最需要看清楚，所以详情入口对所有任务常驻。
func (s *Screen) rowActions(task services.ImportTask) []rowAction {
	var out []rowAction
	running := IsActive(task)
	add := func(a rowAction) { out = append(out, a) }

	if running {
		add(rowAction{key: "cancel", label: locales.CommonCancel(), icon: icons.Ban, run: func() {
			s.Store.Spawn(func() { s.Store.CancelTask(s.Ctx, task.ID) })
		}})
	}
	if task.Status == services.ImportStatusFailed || task.Status == services.ImportStatusCanceled {
		label := locales.ImportsRetry()
		needsVerify := NeedsWebVerification(task)
		if needsVerify {
			label = locales.ImportsWebVerifyRetry()
			if s.Hooks.WebSolverEnabled != nil && s.Hooks.WebSolverEnabled() {
				label = locales.ImportsWebSolverRetry()
			}
		}
		add(rowAction{key: "retry", label: label, icon: icons.RotateCcw, run: func() {
			if needsVerify {
				s.openDetail(task.ID)
				return
			}
			s.Store.Spawn(func() { s.Store.RetryTask(s.Ctx, task.ID, services.ImportRetryOptions{}) })
		}})
	}
	if platform := AuthenticatedRetryPlatform(task); platform != "" {
		label := locales.ImportsAuthenticatedRetry()
		if platform == "nodeseek" {
			label = locales.ImportsNodeseekOpenVerification()
		}
		add(rowAction{key: "login", label: label, icon: icons.LogIn, run: func() {
			if platform == "nodeseek" {
				s.openDetail(task.ID)
				return
			}
			if s.Hooks.RetryAuthenticated != nil {
				s.Hooks.RetryAuthenticated(task, platform)
			}
		}})
	}
	add(rowAction{key: "detail", label: locales.ImportsTaskDetail(), icon: icons.Info, run: func() { s.openDetail(task.ID) }})
	if NeedsCaptureToolSetup(task.Error) {
		add(rowAction{key: "setup", label: locales.ImportsGoToCaptureSettings(), icon: icons.Settings, run: func() {
			if s.Hooks.OpenSettingsSection != nil {
				s.Hooks.OpenSettingsSection("general")
			}
		}})
	}
	if itemID := openableItemID(task); itemID != "" {
		label := locales.ImportsOpenItem()
		if task.Status == services.ImportStatusDuplicate {
			label = locales.ImportsOpenExisting()
		}
		add(rowAction{key: "open", label: label, icon: icons.ExternalLink, run: func() {
			if s.Hooks.OpenItem != nil {
				s.Hooks.OpenItem(itemID)
			}
		}})
	}
	if task.Status == services.ImportStatusDuplicate && task.DuplicateItemID != "" {
		add(rowAction{key: "refresh", label: locales.ImportsRefreshSourceKeepEdits(), icon: icons.RefreshCw, run: func() {
			s.Store.Spawn(func() {
				s.Guard.Run(locales.ImportsRefreshSourceKeepEdits(), func() error {
					_, err := s.Store.Enqueue(s.Ctx, []services.EnqueueImportInput{{
						Kind: task.SourceKind, Input: task.SourceInput, RefreshOfItemID: task.DuplicateItemID, ForceDuplicate: true,
					}})
					return err
				})
			})
		}})
	}
	if task.Status == services.ImportStatusDuplicate {
		add(rowAction{key: "copy", label: locales.ImportsCreateCopy(), icon: icons.CopyPlus, run: func() {
			s.Store.Spawn(func() { s.Store.RetryTask(s.Ctx, task.ID, services.ImportRetryOptions{ForceDuplicate: true}) })
		}})
	}
	if !running {
		add(rowAction{key: "remove", label: locales.ImportsRemoveTask(), icon: icons.X, destructive: true, run: func() {
			s.Store.RemoveWithUndo(s.Ctx, []string{task.ID})
		}})
	}
	return out
}

// actionBar 是行尾浮出的动作条：悬停或焦点在行内时淡入，盖住时间列。
//
// pointer-events-none absolute right-2 top-1.5 flex items-center gap-0.5 rounded-lg border border-border bg-popover
// px-1 py-0.5 opacity-0 shadow-sm transition-opacity duration-quick，group-hover / group-focus-within 时 opacity-100。
// 条里的按钮一直在可访问树里（渲染层只是透明），所以始终声明，只用不透明度表现显隐。
func (s *Screen) actionBar(c ui.C, task services.ImportTask, active bool) {
	l := s.Look
	t := l.T
	c = c.Key("actions")
	id := ui.ID(c)
	p := l.HoverProgress(c, id, 0, active, kit.DurationQuick)
	pad := func(n float32) float32 { return l.Space(n) + 1 }
	// CSS 的绝对定位相对祖先的内边距盒（行 1px 边框的内侧），Viva 的 Absolute 相对外框：top / right 各补一条边框的宽度。
	ui.Row(c).Absolute().Top(l.Space(1.5)+1).Right(l.Space(2)+1).Items(ui.Center).Gap(l.Space(0.5)).
		PadSides(pad(0.5), pad(1), pad(0.5), pad(1)).Opacity(p).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Popover, Border: t.Border, BorderW: 1, Radius: l.Rem(0.75), Shadow: t.ShadowSm})
		}).
		Children(func() {
			for _, a := range s.rowActions(task) {
				s.rowActionButton(c, task, a)
			}
		})
}

// rowActionButton 是一枚动作：inline-flex h-6 w-6 rounded-md，muted 图标，悬停 bg-accent/60 text-foreground（删除是 bg-destructive/10 text-destructive）。
func (s *Screen) rowActionButton(c ui.C, task services.ImportTask, a rowAction) {
	l := s.Look
	t := l.T
	size := l.Rem(1.5)
	st := kit.ButtonStyle{
		Height: size, Width: size, Radius: l.Rem(0.75) - 2,
		Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent.WithAlpha(0.6), Transition: kit.DurationDefault,
	}
	if a.destructive {
		st.FgHover, st.BgHover = t.Destructive, t.Destructive.WithAlpha(0.1)
	}
	b := l.Button(c.Key(a.key), kit.ButtonSpec{
		Name: a.label, Icon: a.icon, IconSize: l.Rem(0.875), Center: true, Groups: &s.rowHover, Group: task.ID, Hint: a.label,
	}, st)
	if b.Clicked() {
		a.run()
	}
}
