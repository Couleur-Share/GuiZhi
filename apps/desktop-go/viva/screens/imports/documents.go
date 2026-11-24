package imports

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// documentSite 是工作区里的「采集文档站」页（渲染层 ImportsWorkspace 的 view === "documents"）。
// 装配了 DocSite 就画整页界面（docsite_view.go）；没有（自动化实例、旧单测）时仍是「尚未迁移」的占位。
func (s *Screen) documentSite(c ui.C) {
	if s.DocSite != nil {
		s.docSitePage(c)
		return
	}
	s.documentSitePlaceholder(c)
}

// documentSitePlaceholder 如实说明这一页没有接上，并留一个返回任务队列的出口，而不是一个看起来能用的空页。
func (s *Screen) documentSitePlaceholder(c ui.C) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("documents")).FillWidth().Grow(1).MinHeight(0).Items(ui.Center).Justify(ui.Center).Gap(l.Space(3)).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.UiSectionBg}) }).
		Children(func() {
			l.Text(c.Key("title"), locales.NativeShellNotMigrated(locales.WebCaptureImportSite()), kit.TextSM, kit.WeightMedium, t.Foreground)
			l.Text(c.Key("hint"), locales.NativeShellNotMigratedHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground)
			b := l.Button(c.Key("back"), kit.ButtonSpec{Label: locales.CommonBack()}, s.headerButtonStyle(false))
			if b.Clicked() {
				s.documents = false
			}
		})
}
