package themed

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 阅读页资料面板（ThemedReadingPageTools.tsx）：参考资料、页面图片、版本说明三个对话框。数据在 ReadingView（tools.go）。

// panelDialog 每一帧都要经过（Modal 靠它复位）：Panel 为 PanelNone 时什么也不画。
func (s *Screen) panelDialog(c ui.C) {
	l := s.Look
	panel := s.view.Panel
	open := s.attached && panel != PanelNone && s.view.Page() != nil
	title := ""
	switch panel {
	case PanelReferences:
		title = locales.ThemedReadingReaderReferences()
	case PanelAssets:
		title = locales.ThemedReadingAssets()
	case PanelNotes:
		title = locales.ThemedReadingReaderPageNotes()
	}
	closed := l.Modal(c, &s.tools.panel, kit.ModalSpec{
		Open: open, Size: kit.ModalLG, Title: title, CloseLabel: locales.CommonClose(),
		Body: func(c ui.C) {
			ui.Column(c).FillWidth().Gap(l.Space(3)).Children(func() {
				switch panel {
				case PanelReferences:
					s.referencesPanel(c.Key("refs"))
				case PanelAssets:
					s.assetsPanel(c.Key("assets"))
				case PanelNotes:
					s.notesPanel(c.Key("notes"))
				}
			})
		},
	})
	if closed {
		s.Reading.ClosePanel()
	}
}

// safeURL 报告链接能否交给系统浏览器：只放行 http / https（与阅读岛的链接校验一致）。
func safeURL(u string) bool {
	low := strings.ToLower(strings.TrimSpace(u))
	return strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "http://")
}

// openLink 用系统浏览器打开参考资料；失败带原因提示。
func (s *Screen) openLink(url string) {
	if s.Hooks.OpenExternal == nil || !safeURL(url) {
		return
	}
	if err := s.Hooks.OpenExternal(url); err != nil {
		s.Guard.Fail(locales.ThemedReadingReaderReferences(), "themedReading.openLink", err)
	}
}

// referencesPanel 是参考资料列表：读取中、读取失败（可重试）、没有资料、逐条资料。
func (s *Screen) referencesPanel(c ui.C) {
	l := s.Look
	t := l.T
	refs := s.view.Refs
	switch {
	case refs.Error != "":
		if l.LoadErrorState(c.Key("error"), refs.Error) {
			s.spawn(func() { s.Reading.OpenPanel(PanelReferences) })
		}
		return
	case refs.Loading:
		s.statusLine(c.Key("loading"), locales.ThemedReadingLoading())
		return
	case len(refs.Items) == 0:
		s.bodyText(c.Key("none"), locales.ThemedReadingReaderNoReferences(), t.MutedForeground)
		return
	}
	for i, r := range refs.Items {
		kc := c.Key("ref" + strconv.Itoa(i))
		// <article className="border-b border-border/50 pb-3 text-sm">：链接标题、下面一行「获取于 日期」，没有网址行与外链图标。
		ui.Column(kc).FillWidth().PadSides(0, 0, l.Space(3)+1, 0).Draw(func(d *ui.DrawContext) {
			l.PaintEdge(d, kit.EdgeBottom, 1, t.Border.WithAlpha(0.5))
		}).Children(func() {
			if safeURL(r.URL) && s.Hooks.OpenExternal != nil {
				ui.Row(kc.Key("link")).Height(l.Rem(1.25)).Items(ui.Center).Children(func() {
					if s.textLinkAs(kc.Key("title"), refTitle(r.Title, r.URL), kit.TextSM, gfx.RoleLink, l.Rem(1.125)) {
						s.openLink(r.URL)
					}
				})
			} else {
				l.Text(kc.Key("title"), refTitle(r.Title, r.URL), kit.TextSM, kit.WeightNormal, t.Primary, kit.Wrapped())
			}
			s.smallText(kc.Key("date"), ReferenceDate(r, s.Loc, s.Locale)).MarginTop(l.Space(1))
		})
	}
}

func refTitle(title, url string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	return url
}

// assetsPanel 是页面图片列表：缩略图占位、用途、来源（AI 生成 / 原有）、失败原因与单张重做。
// 原生界面没有本地图片读取设施，缩略图位置先画占位图标；图片本身在页面里显示。
func (s *Screen) assetsPanel(c ui.C) {
	l := s.Look
	t := l.T
	rows := s.view.AssetRows(s.sub.ReadOnly)
	if len(rows) == 0 {
		s.bodyText(c.Key("none"), locales.ThemedReadingNoAssets(), t.MutedForeground)
		return
	}
	for i, row := range rows {
		kc := c.Key("asset" + strconv.Itoa(i))
		s.card(kc, func() {
			ui.Row(kc.Key("row")).FillWidth().Gap(l.Space(3)).Children(func() {
				ui.Row(kc.Key("thumb")).Size(l.Rem(4), l.Rem(4)).Shrink(0).Items(ui.Center).Justify(ui.Center).
					Draw(func(d *ui.DrawContext) {
						l.PaintSurface(d, kit.Surface{Bg: t.Muted, Radius: l.Rem(0.5)})
					}).Children(func() {
					l.Icon(kc.Key("icon"), icons.Image, l.Rem(1.25), t.MutedForeground)
				})
				ui.Column(kc.Key("info")).Grow(1).Shrink(1).MinWidth(0).Gap(l.Space(1)).Children(func() {
					if row.Caption != "" {
						s.bodyText(kc.Key("caption"), row.Caption, t.Foreground)
					}
					s.smallText(kc.Key("kind"), row.Kind)
					if row.Asset.Error != "" {
						s.alertLine(kc.Key("error"), row.Asset.Error)
					}
					if row.Redo {
						id := row.Asset.ID
						spec := kit.ButtonSpec{Label: row.RedoLabel, Icon: icons.RotateCcw, IconSize: l.Rem(0.75), Disabled: row.RedoDisabled}
						if l.Button(kc.Key("redo"), spec, s.compactButton()).Clicked() {
							s.spawn(func() { s.Reading.RegenerateAsset(id) })
						}
					}
				})
			})
		})
	}
}

// notesPanel 是版本说明：原文已更新的说明与生成时留下的提示。
func (s *Screen) notesPanel(c ui.C) {
	t := s.Look.T
	n := s.view.Notes()
	if n.StaleDetail != "" {
		s.bodyText(c.Key("stale"), n.StaleDetail, t.Foreground)
	}
	for i, w := range n.Warnings {
		s.bodyText(c.Key("warn"+strconv.Itoa(i)), w, t.MutedForeground)
	}
}
