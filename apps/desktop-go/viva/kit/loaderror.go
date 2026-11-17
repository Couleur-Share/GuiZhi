package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/locales"
)

// LoadErrorState 是列表读取失败的占位（渲染层 components/ui/LoadErrorState.tsx）：
// 明确说「读取失败」并给出原因与重试入口，不能渲染成「还没有数据」的空态。
// 返回用户这一帧是否点了「重试」。
//
// 结构：role=alert 的纵向居中块（gap-2.5 px-6 py-10 text-center）：警示图标（h-7 w-7，destructive/60）、
// 标题（text-sm font-medium）、原因（max-w-md text-xs leading-relaxed，muted-foreground，可折行）、重试钮。
func (l *Look) LoadErrorState(c ui.C, message string) (retry bool) {
	t := l.T
	ui.Column(c).FillWidth().Items(ui.Center).Gap(l.Space(2.5)).
		PadSides(l.Space(10), l.Space(6), l.Space(10), l.Space(6)).
		Draw(func(d *ui.DrawContext) {
			// alert 在可访问树里是一个带直播属性的区域；Viva 没有 alert 角色，用 status + assertive 表达（台账 A-012）。
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LiveAssertive})
		}).
		Children(func() {
			l.Icon(c.Key("icon"), icons.TriangleAlert, l.Rem(1.75), t.Destructive.WithAlpha(0.6))
			l.Text(c.Key("title"), locales.CommonLoadFailed(), TextSM, WeightMedium, t.Foreground, Centered())
			ui.Column(c.Key("detail")).FillWidth().MaxWidth(l.Rem(28)).Children(func() {
				l.Text(c.Key("message"), message, TextStyle{Size: 0.75, Line: 0.75 * 1.625}, WeightNormal, t.MutedForeground, Centered(), Wrapped())
			})
			style := ButtonStyle{
				Height: l.Rem(2), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5),
				Text: TextXS, Weight: WeightNormal,
				Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Muted.WithAlpha(0.6),
				Transition: DurationDefault,
			}
			spec := ButtonSpec{Label: locales.CommonRetry(), Icon: icons.RotateCcw, IconSize: l.Rem(0.875)}
			if l.Button(c.Key("retry"), spec, style).MarginTop(l.Space(1)).Clicked() {
				retry = true
			}
		})
	return retry
}
