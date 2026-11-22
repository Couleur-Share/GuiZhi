package ask

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// reasonSeparator 是复核原因之间的分隔：全角分号（渲染层 reviewReasons.join 的分隔符，不随界面语言变）。写成转义：它是分隔符不是文案。
const reasonSeparator = "\uFF1B"

// evidenceExcerpt 是来源 chip 展开的「回答时的证据」摘录（渲染层 EvidenceExcerpt）：
// mt-3 space-y-2 rounded-lg border border-border bg-muted/30 p-3 text-xs，是带名字的区域。
//
// 标题（来源已清除 / 片段标题 / 未保存历史片段）；清除后只说明已清除；否则是回答时的采集时间、待复核提示（带原因）与片段正文
// （最高 16rem、超出滚动、保留换行）；没有保存证据时说明当前版本可能已变化。
// 底部「查看当前版本」（已清除的来源没有）与「关闭」。
func (s *Screen) evidenceExcerpt(c ui.C, st *cardState, m services.AskMessage, src services.AskSource, group string) {
	l := s.Look
	t := l.T
	ev := src.Evidence
	removed := src.Cleared || (ev != nil && ev.Cleared != nil && *ev.Cleared)
	ui.Column(c.Key("excerpt")).FillWidth().MarginTop(l.Space(3)).Gap(l.Space(2)).Pad(l.Space(3) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.3), Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
			d.Canvas.Container(d.Rect, gfx.RoleGroup, locales.AskEvidenceTitle())
		}).
		Children(func() {
			title := locales.AskEvidenceNotSaved()
			switch {
			case removed:
				title = locales.AskEvidenceCleared()
			case ev != nil && ev.Title != "":
				title = ev.Title
			}
			l.Text(c.Key("title"), title, kit.TextXS, kit.WeightMedium, t.Foreground, kit.Wrapped())
			switch {
			case removed:
				l.Text(c.Key("cleared"), locales.AskEvidenceClearedHint(), kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped())
			case ev != nil:
				s.evidenceBody(c, ev.CapturedAt, ev.ReviewStatus, ev.ReviewReasons, ev.Text)
			default:
				l.Text(c.Key("missing"), locales.AskEvidenceMissing(), kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped())
			}
			// 两枚文字按钮在同一行里：「查看当前版本」text-primary underline，「关闭」ml-3 text-muted-foreground。
			ui.Flow(c.Key("buttons")).FillWidth().LineGap(0).Items(ui.Center).Children(func() {
				if !removed {
					if s.textButton(c.Key("current"), locales.AskEvidenceViewCurrent(), textBtn{
						Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.Primary, Underline: true, Groups: &s.turns, Group: group,
					}) && s.Hooks.OpenSource != nil {
						s.Hooks.OpenSource(src)
					}
				}
				ui.Row(c.Key("close-wrap")).MarginLeft(l.Space(3)).Children(func() {
					if s.textButton(c.Key("close"), locales.CommonClose(), textBtn{
						Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.MutedForeground, Groups: &s.turns, Group: group,
					}) {
						st.choice = nil
					}
				})
			})
		})
}

// evidenceBody 是摘录里的采集时间、复核提示与片段正文。
func (s *Screen) evidenceBody(c ui.C, capturedAt int64, reviewStatus string, reasons []string, text string) {
	l := s.Look
	t := l.T
	when := kit.FormatDateTime(capturedAt, s.loc(), s.Locale)
	l.Text(c.Key("captured"), locales.AskEvidenceCapturedAt(when), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
	if reviewStatus == "needs_review" {
		reason := strings.Join(reasons, reasonSeparator)
		if reason == "" {
			reason = locales.AskEvidenceUnverified()
		}
		ui.Column(c.Key("review")).FillWidth().Draw(func(d *ui.DrawContext) { markStatus(d) }).Children(func() {
			l.Text(c.Key("text"), locales.AskEvidenceNeedsReview(reason), kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped())
		})
	}
	// max-h-64 overflow-auto whitespace-pre-wrap break-words
	l.ScrollArea(c.Key("text-scroll"), [4]float32{}, func() {
		l.Text(c.Key("text"), text, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped())
	}).FillWidth().MaxHeight(l.Rem(16))
}
