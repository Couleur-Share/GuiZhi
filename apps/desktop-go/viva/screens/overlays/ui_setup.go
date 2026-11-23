package overlays

// 首次使用设置清单的界面（渲染层 components/setup/SetupChecklistDialog.tsx）：md 档模态窗，
// 一句说明、文本模型主卡（唯一的必做项）、折叠的「更多可选设置」（转写与语义检索）、「稍后再说」与「开始采集」。
// 关闭对话框（Esc、点遮罩、关闭钮）等于「稍后再说」。状态与偏好在 Setup（setup.go）；这里只画并把点击交给它。

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// setupUI 是设置清单自己的界面状态：模态窗。
type setupUI struct {
	modal kit.ModalState
}

// setupCopy 是清单项的标题与说明（渲染层 ITEM_COPY）。
func setupCopy(id SetupItemID) (title, desc string) {
	switch id {
	case SetupTextModel:
		return locales.SetupItemTextModel(), locales.SetupItemTextModelDesc()
	case SetupTranscription:
		return locales.SetupItemTranscription(), locales.SetupItemTranscriptionDesc()
	}
	return locales.SetupItemEmbedding(), locales.SetupItemEmbeddingDesc()
}

// setupDialog 声明设置清单对话框。每一帧都要经过 Modal：关着的那一帧它负责复位。
func (s *Screen) setupDialog(c ui.C) {
	v := s.Setup.View()
	if s.Look.Modal(c.Key("setup-modal"), &s.setup.modal, kit.ModalSpec{
		Open: v.Open, Size: kit.ModalMD, Title: locales.SetupTitle(), CloseLabel: locales.CommonClose(),
		Body: func(c ui.C) { s.setupBody(c, v) },
	}) {
		s.Setup.DismissLater()
	}
}

// setupBody 是内容：space-y-4。
func (s *Screen) setupBody(c ui.C, v SetupView) {
	l := s.Look
	t := l.T
	c = c.Key("setup")
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		l.Text(c.Key("intro"), locales.SetupIntro(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		if v.LoadError != "" {
			// AI 配置读不出来：清单按「都没配」显示，如实说明，不能让用户以为自己真的没配。
			l.Text(c.Key("load-error"), locales.NativeSetupLoadError(v.LoadError), kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped())
		}
		s.setupMainCard(c.Key("main"), v)
		s.setupOptional(c.Key("optional"), v)
		s.setupFooter(c.Key("footer"), v)
	})
}

// setupMainCard 是文本模型主卡：flex items-start gap-3 rounded-xl border border-border/70 bg-background/60 px-4 py-3.5，
// 左边标题（text-sm font-medium）与说明（text-xs leading-relaxed），右边是「已完成」或「快速配置」。
func (s *Screen) setupMainCard(c ui.C, v SetupView) {
	l := s.Look
	t := l.T
	title, desc := setupCopy(SetupTextModel)
	ui.Row(c).FillWidth().Items(ui.Start).Gap(l.Space(3)).PadSides(l.Space(3.5)+1, l.Space(4)+1, l.Space(3.5)+1, l.Space(4)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.6), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		ui.Column(c.Key("text")).Grow(1).Shrink(1).MinWidth(0).Gap(l.Space(1)).Children(func() {
			l.Text(c.Key("title"), title, kit.TextSM, kit.WeightMedium, t.Foreground, kit.Wrapped())
			l.TextPx(c.Key("desc"), desc, l.Rem(0.75), l.Rem(0.75)*1.625, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		})
		if v.TextModelReady {
			s.setupDone(c.Key("done"))
			return
		}
		if l.Button(c.Key("quick"), kit.ButtonSpec{Label: locales.SetupQuickSetup()}, s.uiPrimary(kit.UIButtonSM)).Shrink(0).Self(ui.Center).Clicked() {
			s.Setup.GoConfigure(SetupTextModel)
		}
	})
}

// setupDone 是主卡右侧的「已完成」：inline-flex items-center gap-1 text-xs text-emerald-700 dark:text-emerald-400，16px 对勾。
func (s *Screen) setupDone(c ui.C) {
	l := s.Look
	ink := tokens.Tailwind("emerald-700")
	if l.Dark {
		ink = tokens.Tailwind("emerald-400")
	}
	ui.Row(c).Shrink(0).Self(ui.Center).Items(ui.Center).Gap(l.Space(1)).Children(func() {
		l.Icon(c.Key("icon"), icons.CircleCheckBig, l.Rem(1), ink)
		l.Text(c.Key("text"), locales.SetupDone(), kit.TextXS, kit.WeightNormal, ink)
	})
}

// setupOptional 是折叠区：一枚展开钮（「更多可选设置（n）」）与展开后的可选项列表。
func (s *Screen) setupOptional(c ui.C, v SetupView) {
	l := s.Look
	t := l.T
	ui.Column(c).FillWidth().Children(func() {
		icon := icons.ChevronRight
		if v.OptionalOpen {
			icon = icons.ChevronDown // 渲染层是 ChevronRight 转 90°
		}
		st := kit.ButtonStyle{
			PadY: l.Space(1), Radius: l.Rem(0.375), Gap: l.Space(1), Text: kit.TextXS, Weight: kit.WeightNormal,
			Fg: t.MutedForeground, FgHover: t.Foreground, Transition: kit.DurationDefault,
		}
		spec := kit.ButtonSpec{Label: locales.SetupMoreOptional(len(v.Optional)), Icon: icon, IconSize: l.Rem(0.875), Disclosure: true, On: v.OptionalOpen}
		if l.Button(c.Key("toggle"), spec, st).Self(ui.Start).Clicked() {
			s.Setup.ToggleOptional()
		}
		if !v.OptionalOpen {
			return
		}
		ui.Column(c.Key("list")).FillWidth().Gap(l.Space(2)).MarginTop(l.Space(2)).Children(func() {
			for _, it := range v.Optional {
				s.setupOptionalRow(c.Key("item-"+string(it.ID)), v, it)
			}
		})
	})
}

// setupOptionalRow 是一个可选项：flex items-start gap-3 rounded-xl border border-border/60 px-3 py-2.5，
// 右边是「已完成」、探测中的转圈或「去配置」链接。
func (s *Screen) setupOptionalRow(c ui.C, v SetupView, it SetupItem) {
	l := s.Look
	t := l.T
	title, desc := setupCopy(it.ID)
	probing := v.Probing && it.ID == SetupTranscription
	ui.Row(c).FillWidth().Items(ui.Start).Gap(l.Space(3)).PadSides(l.Space(2.5)+1, l.Space(3)+1, l.Space(2.5)+1, l.Space(3)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		ui.Column(c.Key("text")).Grow(1).Shrink(1).MinWidth(0).Gap(l.Space(0.5)).Children(func() {
			l.Text(c.Key("title"), title, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped())
			l.TextPx(c.Key("desc"), desc, l.Rem(0.75), l.Rem(0.75)*1.625, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		})
		switch {
		case it.Ready:
			l.Text(c.Key("done"), locales.SetupDone(), kit.TextXS, kit.WeightNormal, t.MutedForeground).Shrink(0).Self(ui.Center)
		case probing:
			l.Spinner(c.Key("probing"), l.Rem(1), t.MutedForeground).Self(ui.Center)
		default:
			if s.setupLink(c.Key("configure"), locales.SetupConfigure()) {
				s.Setup.GoConfigure(it.ID)
			}
		}
	})
}

// setupLink 是「去配置」：shrink-0 self-center rounded-md px-1.5 py-1 text-xs text-primary hover:underline。
func (s *Screen) setupLink(c ui.C, label string) bool {
	l := s.Look
	t := l.T
	radius := l.Rem(0.375)
	id := ui.ID(c)
	box := ui.Row(c).Shrink(0).Self(ui.Center).Items(ui.Center).PadSides(l.Space(1), l.Space(1.5), l.Space(1), l.Space(1.5)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	hovered := box.State().Hovered
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), radius)
		}
	}).Children(func() {
		l.Text(c.Key("text"), label, kit.TextXS, kit.WeightNormal, t.Primary, kit.Underline(hovered))
	})
	return box.Clicked()
}

// setupFooter 是底部操作：flex items-center justify-end gap-2 pt-1，「稍后再说」（ghost）与文本模型就绪后才有的「开始采集」。
func (s *Screen) setupFooter(c ui.C, v SetupView) {
	l := s.Look
	ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.End).Gap(l.Space(2)).PadSides(l.Space(1), 0, 0, 0).Children(func() {
		if l.Button(c.Key("later"), kit.ButtonSpec{Label: locales.SetupLater()}, l.UIGhostButton(kit.UIButtonSM)).Clicked() {
			s.Setup.DismissLater()
		}
		if v.TextModelReady {
			if l.Button(c.Key("start"), kit.ButtonSpec{Label: locales.SetupStartCapture()}, s.uiPrimary(kit.UIButtonSM)).Clicked() {
				s.Setup.StartCapture()
			}
		}
	})
}
