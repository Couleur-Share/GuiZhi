package wiki

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// syncDraft 让编辑框与 Store 里的草稿一致：刚进入编辑的那一帧把草稿灌进编辑框；之后用户打字就推给 Store。
// 切页、保存成功、采用建议等让 Store 退出编辑时，编辑框不再显示，下次进入重新灌入。
func (s *Screen) syncDraft(v View) {
	d := &s.detail
	if !v.Editing {
		d.editing = false
		return
	}
	if !d.editing {
		d.editing = true
		d.draft.SetText(v.Draft)
		return
	}
	if text := d.draft.Text(); text != v.Draft {
		s.Store.SetDraft(text)
	}
}

// editorRows 是编辑框的行数（rows={20}）。
const editorRows = 20

// editorLine 是编辑框每行的高度：渲染层 text-[13px] leading-relaxed（1.625 × 13）。
// 框高按它算，保证下面的说明与按钮落在与渲染层相同的位置；Viva 的多行框自己的行高是字号加 7，所以框内可见行数略少（已知差异，见 editor 的注释）。
const editorLine = 21.125

// editor 是正文编辑区：space-y-2，一块多行输入、一句说明、保存与取消。
// 输入框是 w-full rounded-xl border border-border bg-background/60 p-3，聚焦时边框 primary/50。
// 已知差异：渲染层是等宽 13px 字体、可拖拽改高；这里沿用 kit.TextArea 的正文字号与固定行数。
func (s *Screen) editor(c ui.C, v View) {
	l := s.Look
	t := l.T
	d := &s.detail
	c = c.Key("editor")
	ui.Column(c).FillWidth().Gap(l.Space(2)).Children(func() {
		// 渲染层 rows={20}：13px 等宽字、leading-relaxed（21.125px）× 20 行 + p-3 上下 + 边框 2px；框是 inline-block，块级父盒的行盒在它下面多留一截基线下沿（约 0.375rem）。
		height := editorRows*editorLine + 2*l.Space(3) + 2
		ui.Stack(c.Key("box")).FillWidth().Height(height).MarginBottom(l.Rem(0.375)).Children(func() {
			sid := ui.ID(c.Key("surface"))
			p := l.HoverProgress(c, sid, 0, d.editFocus, kit.DurationDefault)
			border := kit.MixToken(t.Border, t.Primary.WithAlpha(0.5), p)
			ui.Row(c.Key("surface-row")).Absolute().Inset(0).Draw(func(dc *ui.DrawContext) {
				l.PaintSurface(dc, kit.Surface{Bg: t.Background.WithAlpha(0.6), Border: border, BorderW: 1, Radius: l.Rem(0.75)})
			})
			input := l.TextArea(c.Key("textarea"), &d.draft, locales.WikiEditPage(), "", kit.TextAreaSpec{
				Height: height, PadX: l.Space(3) + 1, PadY: l.Space(3) + 1, Color: t.Foreground,
			}).Absolute().Inset(0)
			d.editFocus = input.State().Focused
		})
		l.Text(c.Key("hint"), locales.WikiEditPageHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		ui.Row(c.Key("actions")).Items(ui.Center).Gap(l.Space(2)).Children(func() {
			save := l.Button(c.Key("save"), kit.ButtonSpec{Label: locales.CommonSave()}, kit.ButtonStyle{
				Height: l.Rem(2), PadX: l.Space(3), Radius: s.radiusLG(), Gap: l.Space(1.5), Text: kit.TextSM, Weight: kit.WeightMedium,
				Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault,
			})
			if save.Clicked() {
				s.spawn(func() { s.Store.SaveDraft() })
			}
			cancel := l.Button(c.Key("cancel"), kit.ButtonSpec{Label: locales.CommonCancel()}, kit.ButtonStyle{
				Height: l.Rem(2), PadX: l.Space(3), Radius: s.radiusLG(), Text: kit.TextSM, Weight: kit.WeightNormal,
				Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationDefault,
			})
			if cancel.Clicked() {
				s.Store.CancelEdit()
			}
		})
	})
}
