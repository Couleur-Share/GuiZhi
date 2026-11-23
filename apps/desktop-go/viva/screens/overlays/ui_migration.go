package overlays

// 旧版（.NET）归知数据迁移对话框的界面（渲染层 components/migration/MigrationDialog.tsx）：md 档模态窗，
// 确认页（探测到的条目数、来源路径与迁移范围、暂不迁移 / 立即迁移）、迁移中（转圈，不可关闭）、完成页（各类数据数量、开始使用）、
// 失败页（原因、关闭 / 重试）。阶段与服务调用在 Migration（migration.go）；这里只画并把点击交给它。

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// migrationUI 是迁移对话框自己的界面状态：模态窗。
type migrationUI struct {
	modal kit.ModalState
}

// migrationDialog 声明迁移对话框。迁移进行中不可关闭（Esc、点遮罩与关闭钮都不响应，渲染层 onClose 换成空函数）。
// 每一帧都要经过 Modal：关着的那一帧它负责复位。
func (s *Screen) migrationDialog(c ui.C) {
	v := s.Migration.View()
	running := v.Phase == MigrationRunning
	if s.Look.Modal(c.Key("migration-modal"), &s.migrate.modal, kit.ModalSpec{
		Open: v.Open, Size: kit.ModalMD, Title: locales.MigrationTitle(), CloseLabel: locales.CommonClose(),
		NoClose: running, NoEscape: running, NoBackdropClose: running,
		Body: func(c ui.C) { s.migrationBody(c, v) },
	}) {
		s.Migration.Close()
	}
}

// migrationBody 按阶段画内容。
func (s *Screen) migrationBody(c ui.C, v MigrationView) {
	c = c.Key("migration")
	switch v.Phase {
	case MigrationRunning:
		s.migrationRunning(c)
	case MigrationDone:
		s.migrationDone(c, v)
	case MigrationFailed:
		s.migrationFailed(c, v)
	default:
		s.migrationConfirm(c, v)
	}
}

// migrationPath 是确认页与失败页显示的来源：用户另选过就用另选的，否则是探测到的默认路径。
func migrationPath(v MigrationView) string {
	if v.SourcePath != "" {
		return v.SourcePath
	}
	return v.Path
}

// migrationCard 是带图标的说明卡：flex items-start gap-3 rounded-xl border px-4 py-3，图标 20px（mt-0.5），右边是 text-sm 的内容。
func (s *Screen) migrationCard(c ui.C, bg, border tokens.Color, icon func(c ui.C), body func(c ui.C)) {
	l := s.Look
	ui.Row(c).FillWidth().Items(ui.Start).Gap(l.Space(3)).PadSides(l.Space(3)+1, l.Space(4)+1, l.Space(3)+1, l.Space(4)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		ui.Column(c.Key("icon")).Shrink(0).MarginTop(l.Space(0.5)).Children(func() { icon(c) })
		ui.Column(c.Key("body")).Grow(1).Shrink(1).MinWidth(0).Gap(l.Space(1)).Children(func() { body(c) })
	})
}

// migrationConfirm 是确认页：探测到的旧库说明、来源路径、「暂不迁移」与「立即迁移」。
func (s *Screen) migrationConfirm(c ui.C, v MigrationView) {
	l := s.Look
	t := l.T
	c = c.Key("confirm")
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		s.migrationCard(c.Key("card"), t.Background.WithAlpha(0.6), t.Border.WithAlpha(0.7),
			func(c ui.C) { l.Icon(c.Key("icon"), icons.Database, l.Rem(1.25), t.Primary) },
			func(c ui.C) {
				l.Text(c.Key("detected"), locales.MigrationDetected(v.ItemCount), kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped())
				l.Text(c.Key("scope"), locales.MigrationScope(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
				if path := migrationPath(v); path != "" {
					l.Text(c.Key("path"), locales.NativeMigrationSourcePath(path), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
				}
			})
		ui.Row(c.Key("actions")).FillWidth().Items(ui.Center).Justify(ui.End).Gap(l.Space(2)).Children(func() {
			if s.migrationOutline(c.Key("skip"), locales.MigrationSkip()) {
				s.Migration.Close()
			}
			if l.Button(c.Key("start"), kit.ButtonSpec{Label: locales.MigrationStart(), Icon: icons.ArrowRight}, s.migrationPrimary()).Clicked() {
				s.Migration.Run()
			}
		})
	})
}

// migrationRunning 是迁移中：flex flex-col items-center gap-3 py-6，32px 转圈加一句话。
func (s *Screen) migrationRunning(c ui.C) {
	l := s.Look
	t := l.T
	c = c.Key("running")
	ui.Column(c).FillWidth().Items(ui.Center).Gap(l.Space(3)).PadSides(l.Space(6), 0, l.Space(6), 0).Children(func() {
		l.Spinner(c.Key("spinner"), l.Rem(2), t.Primary)
		l.Text(c.Key("text"), locales.MigrationRunning(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped(), kit.Centered())
	})
}

// migrationDone 是完成页：绿色卡片（border-green-500/20 bg-green-500/5）、各类数据数量、「开始使用」。
func (s *Screen) migrationDone(c ui.C, v MigrationView) {
	l := s.Look
	t := l.T
	green := tokens.Tailwind("green-600")
	if l.Dark {
		green = tokens.Tailwind("green-400")
	}
	st := v.Stats
	c = c.Key("done")
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		s.migrationCard(c.Key("card"), tokens.Tailwind("green-500").WithAlpha(0.05), tokens.Tailwind("green-500").WithAlpha(0.2),
			func(c ui.C) { l.Icon(c.Key("icon"), icons.CircleCheckBig, l.Rem(1.25), green) },
			func(c ui.C) {
				l.Text(c.Key("title"), locales.MigrationDone(), kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped())
				l.Text(c.Key("stats"), locales.MigrationDoneStats(st.Items, st.Collections, st.Tags, st.Sources, st.WikiPages), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			})
		ui.Row(c.Key("actions")).FillWidth().Justify(ui.End).Children(func() {
			if l.Button(c.Key("finish"), kit.ButtonSpec{Label: locales.MigrationFinish()}, s.migrationPrimary()).Clicked() {
				s.Migration.Close()
			}
		})
	})
}

// migrationFailed 是失败页：红色卡片（border-destructive/20 bg-destructive/5）、失败原因原文、「关闭」与「重试」。
func (s *Screen) migrationFailed(c ui.C, v MigrationView) {
	l := s.Look
	t := l.T
	c = c.Key("failed")
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		s.migrationCard(c.Key("card"), t.Destructive.WithAlpha(0.05), t.Destructive.WithAlpha(0.2),
			func(c ui.C) { l.Icon(c.Key("icon"), icons.TriangleAlert, l.Rem(1.25), t.Destructive) },
			func(c ui.C) {
				l.Text(c.Key("title"), locales.MigrationFailed(), kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped())
				l.Text(c.Key("reason"), v.Error, kit.TextXS, kit.WeightNormal, t.Destructive.WithAlpha(0.9), kit.Wrapped())
				if path := migrationPath(v); path != "" {
					l.Text(c.Key("path"), locales.NativeMigrationSourcePath(path), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
				}
			})
		ui.Row(c.Key("actions")).FillWidth().Items(ui.Center).Justify(ui.End).Gap(l.Space(2)).Children(func() {
			if s.migrationOutline(c.Key("close"), locales.CommonClose()) {
				s.Migration.Close()
			}
			if l.Button(c.Key("retry"), kit.ButtonSpec{Label: locales.ImportsRetry()}, s.migrationPrimary()).Clicked() {
				s.Migration.Run()
			}
		})
	})
}

// migrationPrimary 是主按钮：inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90。
func (s *Screen) migrationPrimary() kit.ButtonStyle {
	l := s.Look
	t := l.T
	return kit.ButtonStyle{
		Height: l.Rem(2.25), PadX: l.Space(4), Radius: l.Rem(0.5), Gap: l.Space(1.5), Text: kit.TextSM, Weight: kit.WeightMedium,
		Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault, Surface: t.UiPanelStrongBg,
	}
}

// migrationOutline 是次要按钮：inline-flex h-9 items-center rounded-lg border border-border px-4 text-sm text-foreground hover:bg-muted/60。
func (s *Screen) migrationOutline(c ui.C, label string) bool {
	l := s.Look
	t := l.T
	st := kit.ButtonStyle{
		Height: l.Rem(2.25), PadX: l.Space(4), Radius: l.Rem(0.5), Text: kit.TextSM, Weight: kit.WeightNormal,
		Fg: t.Foreground, BgHover: t.Muted.WithAlpha(0.6), Border: t.Border, BorderW: 1, Transition: kit.DurationDefault,
	}
	return l.Button(c, kit.ButtonSpec{Label: label}, st).Clicked()
}
