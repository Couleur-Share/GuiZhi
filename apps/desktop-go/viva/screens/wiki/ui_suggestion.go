package wiki

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// suggestionUI 是更新建议区块自己的界面状态：折叠面板的展开（按建议令牌，建议一换就收起）。
type suggestionUI struct {
	token string
	open  bool
}

// updateSuggestion 是正文之后的更新建议区块（渲染层 WikiUpdateSuggestion）：space-y-2 text-sm。
// 依次是旧版覆盖的提示（带「选择资料升级」入口）、读取或采用失败的原因、待审阅的建议（折叠面板，可「接受更新并保留当前版本」）。
// 渲染层没有「忽略建议」：建议留着，直到被采用或被下一轮编译取代，这里同样不做。
func (s *Screen) updateSuggestion(c ui.C, page services.WikiPage) {
	l := s.Look
	t := l.T
	sv := s.Store.Suggestion().View()
	legacy := LegacyCoverage(page.PromptVersion)
	if !s.suggestionShown(page) {
		return
	}
	c = c.Key("suggestion")
	ui.Column(c).FillWidth().Gap(l.Space(2)).Children(func() {
		if legacy {
			ui.Flow(c.Key("legacy")).FillWidth().Items(ui.Center).Gap(0).LineGap(0).Children(func() {
				l.Text(c.Key("text"), locales.WikiLegacyCoverage(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).Shrink(1).MinWidth(0)
				if s.textLink(c.Key("upgrade"), locales.WikiUpgradeSources(), kit.TextSM, true, l.Space(2)) { // ml-2
					s.OpenCompileDialog()
				}
			})
		}
		if sv.Error != "" {
			// role=alert text-destructive：固定提示加可复制的原因。
			s.alertLine(c.Key("error"), locales.WikiSuggestionLoadFailed())
			l.Text(c.Key("error-detail"), sv.Error, kit.TextSM, kit.WeightNormal, t.Destructive, kit.Wrapped())
		}
		if sv.Stale {
			s.alertLine(c.Key("stale"), locales.NativeWikiSuggestionStale())
		}
		if sv.Suggestion != nil {
			s.suggestionPanel(c, *sv.Suggestion)
		}
	})
}

// suggestionShown 报告更新建议区块这一帧有没有内容：旧版覆盖的提示、读取失败、建议已过期或有待审阅的建议，任一为真。
// 渲染层区块自己的外框（space-y-2）一直在，没内容时是个空块，它两侧的外边距会穿过它折叠。
func (s *Screen) suggestionShown(page services.WikiPage) bool {
	sv := s.Store.Suggestion().View()
	return LegacyCoverage(page.PromptVersion) || sv.Error != "" || sv.Stale || sv.Suggestion != nil
}

// followAfterProse 是正文之后下一个有外边距的兄弟的上外边距，与正文最后一块的下外边距折叠（取较大者，见 kit.ProseFollowed）：
// 更新建议区块有内容时它本身没有外边距，两者不折叠；没内容时它是空块，外边距穿过它：先是待复核提示（my-3），再是来源块（mt-6）。
func (s *Screen) followAfterProse(v View, page services.WikiPage) float32 {
	l := s.Look
	if s.suggestionShown(page) {
		return 0
	}
	sources := v.Detail.Sources
	for _, src := range sources {
		if src.NeedsReview() {
			return l.Space(3)
		}
	}
	if len(sources) > 0 {
		return l.Space(6)
	}
	return 0
}

// alertLine 是一行带直播属性（role=alert）的危险色文字。
func (s *Screen) alertLine(c ui.C, text string) {
	l := s.Look
	l.Text(c, text, kit.TextSM, kit.WeightNormal, l.T.Destructive, kit.Wrapped()).
		Draw(func(d *ui.DrawContext) {
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LiveAssertive})
		})
}

// suggestionPanel 是一条建议：rounded border border-border p-3 的折叠面板（details），摘要是建议的原因；
// 展开后是说明、建议摘要、建议正文（至多 18rem 高，可滚动）与「接受更新并保留当前版本」。
func (s *Screen) suggestionPanel(c ui.C, sg services.WikiSuggestion) {
	l := s.Look
	t := l.T
	u := &s.detail.suggestion
	if u.token != sg.Token {
		u.token, u.open = sg.Token, false
	}
	c = c.Key("panel")
	ui.Column(c).FillWidth().Pad(l.Space(3) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.25)})
		}).Children(func() {
		id := ui.ID(c.Key("summary"))
		// <summary> 的标记是浏览器自带的 disclosure 三角（行内，list-style-position: inside），不是图标：文字让出它的宽度。
		marker := kit.ChromeDisclosureMarker(l.Rem(kit.TextSM.Size), l.Rem(kit.TextSM.Line), l.Scale)
		row := ui.Row(c.Key("summary-row")).FillWidth().Items(ui.Center).PadSides(0, 0, 0, marker.Advance)
		row, _ = s.hitDisclosure(c, id, row, sg.Reason, u.open)
		row.Draw(func(d *ui.DrawContext) {
			kit.PaintDisclosure(d, d.Rect, marker, u.open, t.Foreground)
			if d.State.FocusVisible {
				l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.25))
			}
		}).Children(func() {
			l.Text(c.Key("reason"), sg.Reason, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped()).Grow(1).Shrink(1).MinWidth(0)
		})
		if row.Clicked() {
			u.open = !u.open
		}
		if !u.open {
			return
		}
		// my-2 text-muted-foreground：接受后的去向。
		l.Text(c.Key("hint"), locales.WikiSuggestionHint(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).
			MarginTop(l.Space(2)).MarginBottom(l.Space(2))
		l.Text(c.Key("summary-text"), sg.Draft.Summary, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped())
		// my-3 max-h-72 overflow-auto whitespace-pre-wrap：正文在自己的滚动区里。
		l.ScrollArea(c.Key("body"), [4]float32{}, func() {
			l.Text(c.Key("body-text"), sg.Draft.Body, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped())
		}).FillWidth().MaxHeight(l.Rem(18)).MarginTop(l.Space(3)).MarginBottom(l.Space(3))
		accept := l.Button(c.Key("accept"), kit.ButtonSpec{Label: locales.WikiSuggestionAccept()}, s.uiPrimaryStyle()).Self(ui.Start) // 按钮不拉伸：details 里是块级盒内的 inline-block
		if accept.Clicked() {
			s.spawn(func() { s.Store.Suggestion().Accept() })
		}
	})
}
