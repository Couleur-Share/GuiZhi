package ask

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// examples 是示例问法：个人知识库的问题形态就这几类（找回某条内容、横向汇总、追一个具体结论），
// 给出模板比一句「向你的知识库提问」有用得多。
func examples() []string {
	return []string{locales.AskExample1(), locales.AskExample2(), locales.AskExample3()}
}

// openSettingsAI 去设置的 AI 分区（模型与 embedding 都在那里配）。
func (s *Screen) openSettingsAI() {
	if open := s.Store.OnOpenSettingsAI; open != nil {
		open()
	}
}

// pickExample 把示例问题填进输入框并聚焦（渲染层 pickExample）：只预填、不发送。
func (s *Screen) pickExample(text string) {
	s.setDraft(text)
	s.focusDraft = true
}

// emptyState 是没有消息时的引导（渲染层 AskEmptyState）：flex h-full flex-col items-center justify-center gap-5 px-6 text-center。
// 图标、标题与说明；没配文本模型时给「去配置模型」（示例问题点了也只会失败，所以不画）；配好之后是三条示例问题；
// 没配 embedding 时提示「当前只做关键词检索」——等状态查完再决定画不画，配好的用户否则会先看到提示再看它消失。
func (s *Screen) emptyState(c ui.C, set SetupView, view kit.ScrollView) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("empty")).FillWidth().Height(max(view.Viewport, 0)).Items(ui.Center).Justify(ui.Center).Gap(l.Space(5)).
		PadSides(0, l.Space(6), 0, l.Space(6)).Children(func() {
		// h-14 w-14 rounded-2xl bg-muted/60，里面 h-7 w-7 的 MessagesSquare（text-muted-foreground/60）
		ui.Row(c.Key("badge")).Width(l.Rem(3.5)).Height(l.Rem(3.5)).Justify(ui.Center).Items(ui.Center).
			Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.6), Radius: l.Rem(1)}) }).
			Children(func() {
				l.Icon(c.Key("icon"), icons.MessagesSquare, l.Rem(1.75), t.MutedForeground.WithAlpha(0.6))
			})

		// space-y-1.5：标题 text-sm font-medium，说明 max-w-md text-xs text-muted-foreground
		ui.Column(c.Key("text")).FillWidth().Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
			l.Text(c.Key("title"), locales.AskEmpty(), kit.TextSM, kit.WeightMedium, t.Foreground, kit.Centered(), kit.Wrapped())
			ui.Column(c.Key("hint-wrap")).FillWidth().MaxWidth(l.Rem(28)).Children(func() {
				l.Text(c.Key("hint"), locales.AskEmptyHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped())
			})
		})

		ready := set.QAChecked && set.QAReady
		if set.QAChecked && !set.QAReady {
			s.needModelCard(c)
		}
		if ready {
			// flex w-full max-w-md flex-col gap-1.5
			ui.Column(c.Key("examples")).FillWidth().MaxWidth(l.Rem(28)).Gap(l.Space(1.5)).Children(func() {
				for i, text := range examples() {
					s.exampleButton(c.Key("example-"+string(rune('1'+i))), text)
				}
			})
		}
		if ready && set.SemanticChecked && !set.SemanticConfigured {
			s.semanticHint(c)
		}
	})
}

// needModelCard 是「还没有配置文本模型」的引导卡：flex w-full max-w-md flex-col items-center gap-2 rounded-xl border border-border/70
// bg-background/50 px-4 py-4，一句说明与「去配置模型」钮。
func (s *Screen) needModelCard(c ui.C) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("need-model")).FillWidth().MaxWidth(l.Rem(28)).Items(ui.Center).Gap(l.Space(2)).Pad(l.Space(4) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.5), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() {
			l.Text(c.Key("text"), locales.AskNeedModel(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped())
			// inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-xs font-medium text-primary-foreground hover:bg-primary/90
			b := l.Button(c.Key("configure"), kit.ButtonSpec{Label: locales.AskGoConfigure(), Icon: icons.Sparkles, IconSize: l.Rem(0.875)}, kit.ButtonStyle{
				Height: l.Rem(2), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightMedium,
				Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault,
			})
			if b.Clicked() {
				s.openSettingsAI()
			}
		})
}

// exampleButton 是一条示例问题：w-full rounded-xl border border-border/70 bg-background/50 px-3 py-2 text-left text-xs text-muted-foreground
// transition-colors，悬停 border-primary/40 bg-primary/5 text-foreground。点击填进输入框。
func (s *Screen) exampleButton(c ui.C, text string) {
	l := s.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Column(c).FillWidth().Items(ui.Start).PadSides(l.Space(2)+1, l.Space(3)+1, l.Space(2)+1, l.Space(3)+1).
		HitWith(id, text, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	p := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	bg := kit.MixToken(t.Background.WithAlpha(0.5), t.Primary.WithAlpha(0.05), p)
	border := kit.MixToken(t.Border.WithAlpha(0.7), t.Primary.WithAlpha(0.4), p)
	fg := kit.MixToken(t.MutedForeground, t.Foreground, p)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.75))
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: l.Rem(0.75)})
	}).Children(func() {
		l.Text(c.Key("text"), text, kit.TextXS, kit.WeightNormal, fg, kit.Wrapped())
	})
	if box.Clicked() {
		s.pickExample(text)
	}
}

// semanticHint 是「未配置 embedding 模型，当前只做关键词检索」：没配 embedding 时检索只有全文检索，答不上来往往是这个原因，
// 不该让用户自己猜。inline-flex items-center gap-1.5 rounded-lg border border-border px-2.5 py-1 text-[11px] text-muted-foreground
// hover:bg-accent/60 hover:text-foreground。
func (s *Screen) semanticHint(c ui.C) {
	l := s.Look
	t := l.T
	b := l.Button(c.Key("semantic-hint"), kit.ButtonSpec{Label: locales.AskSemanticOffHint(), Icon: icons.Sparkles, IconSize: l.Rem(0.75)}, kit.ButtonStyle{
		PadX: l.Space(2.5), PadY: l.Space(1), Radius: l.Rem(0.75), Gap: l.Space(1.5), FontPx: 11, LinePx: 16.5, Weight: kit.WeightNormal,
		Fg: t.MutedForeground, FgHover: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Accent.WithAlpha(0.6),
		Transition: kit.DurationDefault,
	})
	if b.Clicked() {
		s.openSettingsAI()
	}
}
