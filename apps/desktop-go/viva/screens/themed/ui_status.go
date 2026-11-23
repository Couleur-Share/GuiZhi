package themed

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 任务状态卡（ThemedReadingStatus.tsx）：标题与阶段进度、失败原因（可复制）、停止 / 继续 / 不联网继续、「生成详情」。
// 阅读面板（紧凑条与居中大卡）和任务列表共用同一块；内容判断在 BuildStatus（status.go），这里只画。

// statusUI 是状态卡的界面状态：每张卡的「生成详情」展开与否，按任务标识记。
type statusUI struct {
	open map[string]bool
}

// statusActions 是状态卡上按钮的回调。回调为空的按钮不出现（任务列表里没有「不联网继续」）。
type statusActions struct {
	Stop, Resume, Offline func()
	// Busy 为一次操作进行中：按钮禁用。
	Busy bool
}

// statusCard 画一张任务状态卡。key 区分同一帧里的多张卡。
func (s *Screen) statusCard(c ui.C, task services.ThemedTask, o StatusOptions, act statusActions) {
	l := s.Look
	t := l.T
	m := BuildStatus(task, o)
	expanded := o.Expanded
	pad, radius, gap := l.Space(3), l.Rem(0.75), l.Space(2)
	surface := kit.Surface{Bg: t.Muted.WithAlpha(0.3), Border: t.Border, BorderW: 1, Radius: radius}
	if expanded {
		pad, gap = l.Space(5), l.Space(3)
		surface = kit.Surface{Bg: t.Background.WithAlpha(0.6), Border: t.Border, BorderW: 1, Radius: l.Rem(1)}
	}
	col := ui.Column(c).FillWidth().Gap(gap).Pad(pad + 1).Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, surface)
		// 状态变化（排队→执行→完成）要被读屏播报；失败原因另有 alert。
		d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, m.Title, gfx.HitOptions{Live: gfx.LivePolite})
	})
	if expanded {
		col = col.MaxWidth(l.Rem(32))
	}
	col.Children(func() {
		if m.Headline != "" {
			l.Text(c.Key("headline"), m.Headline, kit.TextXS, kit.WeightMedium, t.Primary)
		}
		ui.Row(c.Key("title-row")).FillWidth().Items(ui.Center).Gap(l.Space(2)).Children(func() {
			s.statusIcon(c.Key("icon"), m, task)
			ui.Row(c.Key("title")).Grow(1).MinWidth(0).Children(func() {
				l.Text(c.Key("text"), m.Title, kit.TextSM, kit.WeightSemibold, t.Foreground, kit.Wrapped())
			})
			if m.StageCount != "" {
				l.Text(c.Key("count"), m.StageCount, kit.TextXS, kit.WeightNormal, t.MutedForeground)
			}
		})
		if m.Detail != "" {
			s.smallText(c.Key("detail"), m.Detail)
		}
		if len(m.Steps) > 0 {
			s.statusSteps(c.Key("steps"), m.Steps)
		}
		if m.Context != "" {
			s.smallText(c.Key("context"), m.Context)
		}
		if m.Error != "" {
			s.errorBlock(c.Key("error"), m.Error)
		}
		s.statusButtons(c.Key("buttons"), m, act)
		if expanded && m.BackgroundHint != "" {
			s.smallText(c.Key("hint"), m.BackgroundHint)
		}
		s.statusDetails(c.Key("details"), task.ID, m, expanded)
	})
}

// statusIcon 是标题前的图标：执行中转圈，完成打勾，失败 / 取消 / 中断是警示。
func (s *Screen) statusIcon(c ui.C, m StatusModel, task services.ThemedTask) {
	l := s.Look
	t := l.T
	size := l.Rem(1)
	switch {
	case m.Active:
		l.SpinIcon(c, icons.RefreshCw, size, t.Primary)
	case task.State == services.ThemedTaskCompleted:
		l.Icon(c, icons.Check, size, t.Primary)
	default:
		l.Icon(c, icons.CircleAlert, size, t.MutedForeground)
	}
}

// statusSteps 是阶段进度：已完成的带勾，当前的转圈，其余的是空心点；折行排列。
func (s *Screen) statusSteps(c ui.C, steps []StatusStep) {
	l := s.Look
	t := l.T
	ui.Flow(c).FillWidth().Gap(l.Space(3)).LineGap(l.Space(1)).Children(func() {
		for _, st := range steps {
			kc := c.Key(st.Stage)
			ui.Row(kc).Items(ui.Center).Gap(l.Space(1)).Children(func() {
				ink := t.MutedForeground
				switch {
				case st.Done:
					l.Icon(kc.Key("icon"), icons.Check, l.Rem(0.75), t.Primary)
					ink = t.Foreground
				case st.Current:
					l.SpinIcon(kc.Key("icon"), icons.RefreshCw, l.Rem(0.75), t.Primary)
					ink = t.Foreground
				default:
					ui.Row(kc.Key("dot")).Size(l.Rem(0.75), l.Rem(0.75)).Shrink(0).Items(ui.Center).Justify(ui.Center).Children(func() {
						ui.Row(kc.Key("ring")).Size(l.Rem(0.375), l.Rem(0.375)).Draw(func(d *ui.DrawContext) {
							l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.1875)})
						})
					})
				}
				l.Text(kc.Key("label"), st.Label, kit.TextXS, kit.WeightNormal, ink)
			})
		}
	})
}

// errorBlock 是失败原因：危险色、可读屏播报，下面一个「复制错误」钮。
func (s *Screen) errorBlock(c ui.C, message string) {
	l := s.Look
	ui.Column(c).FillWidth().Gap(l.Space(1.5)).Children(func() {
		s.alertLine(c.Key("text"), message)
		if s.Guard.Notify == nil {
			return
		}
		spec := kit.ButtonSpec{Label: locales.ThemedReadingReaderCopyError(), Icon: icons.Copy, IconSize: l.Rem(0.875)}
		if l.Button(c.Key("copy"), spec, s.compactButton()).Clicked() {
			s.copyText(message)
		}
	})
}

// copyText 复制文字到剪贴板，并给出成功或带原因的失败提示。
func (s *Screen) copyText(text string) {
	if s.Guard.Notify == nil {
		return
	}
	if err := s.Guard.Notify.CopyText(text); err != nil {
		s.Guard.Fail(locales.ThemedReadingReaderCopyFailed(), "themedReading.copy", err)
		return
	}
	s.Guard.Succeed(locales.ThemedReadingReaderCopied())
}

// statusButtons 是停止 / 继续 / 不联网继续。都是后台调用服务，所以回调里自己 spawn。
func (s *Screen) statusButtons(c ui.C, m StatusModel, act statusActions) {
	if !(m.ShowStop && act.Stop != nil) && !(m.ShowResume && act.Resume != nil) && !(m.ShowOffline && act.Offline != nil) {
		return
	}
	l := s.Look
	ui.Flow(c).FillWidth().Gap(l.Space(2)).LineGap(l.Space(2)).Children(func() {
		if m.ShowStop && act.Stop != nil {
			spec := kit.ButtonSpec{Label: locales.ThemedReadingStop(), Icon: icons.Square, IconSize: l.Rem(0.75), Disabled: act.Busy}
			if l.Button(c.Key("stop"), spec, s.compactButton()).Clicked() {
				act.Stop()
			}
		}
		if m.ShowResume && act.Resume != nil {
			spec := kit.ButtonSpec{Label: m.ResumeLabel, Icon: icons.Play, IconSize: l.Rem(0.75), Disabled: act.Busy}
			if l.Button(c.Key("resume"), spec, s.compactButton()).Clicked() {
				act.Resume()
			}
		}
		if m.ShowOffline && act.Offline != nil {
			spec := kit.ButtonSpec{Label: locales.ThemedReadingReaderOffline(), Disabled: act.Busy}
			if l.Button(c.Key("offline"), spec, s.compactButton()).Clicked() {
				act.Offline()
			}
		}
	})
}

// statusDetails 是折叠的「生成详情」：各行统计与耗时；活动中的紧凑条把「可以切换文章」放在里面。
func (s *Screen) statusDetails(c ui.C, id string, m StatusModel, expanded bool) {
	lines := m.Lines
	if !expanded && m.BackgroundHint != "" {
		lines = append([]string{m.BackgroundHint}, lines...)
	}
	if len(lines) == 0 {
		return
	}
	l := s.Look
	t := l.T
	if s.status.open == nil {
		s.status.open = map[string]bool{}
	}
	open := s.status.open[id]
	did := ui.ID(c.Key("toggle-id"))
	box, _ := s.hitDisclosure(c, did, ui.Row(c.Key("toggle")).Items(ui.Center).Gap(l.Space(1)), locales.ThemedReadingReaderTaskDetails(), open)
	hovered := box.State().Hovered
	box.Children(func() {
		ink := kit.MixToken(t.MutedForeground, t.Foreground, boolF(hovered))
		l.Icon(c.Key("chevron"), icons.ChevronDown, l.Rem(0.75), ink)
		l.Text(c.Key("label"), locales.ThemedReadingReaderTaskDetails(), kit.TextXS, kit.WeightNormal, ink)
	})
	if box.Clicked() {
		s.status.open[id] = !open
	}
	if !open {
		return
	}
	ui.Column(c.Key("lines")).FillWidth().Gap(l.Space(1)).Children(func() {
		for i, line := range lines {
			s.smallText(c.Key("line"+strconv.Itoa(i)), line)
		}
	})
}

func boolF(b bool) float32 {
	if b {
		return 1
	}
	return 0
}
