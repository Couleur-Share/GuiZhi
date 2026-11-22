package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// ViewportWidth 是 Tailwind 响应式断点所依据的窗口内容宽度（逻辑像素）。外壳里就是窗口宽度；
// 阶段 1 把单个屏幕的区域当校准载体时，区域比窗口窄，由 Look.Viewport 显式给出窗口宽度。
func (l *Look) ViewportWidth(c ui.C) float32 {
	if l.Viewport > 0 {
		return l.Viewport
	}
	return c.Bounds().Width
}

// SettingsContent 是设置页的内容区：SettingsPage 里的滚动容器（px-3 py-5，sm:px-6 xl:px-8 2xl:px-10，
// app-wallpaper-section 底色）、内容壳（max-w-[1024px] xl:max-w-[1152px] 2xl:max-w-[1280px]）与 h1 标题。
// 断点按窗口宽度判定（640 / 1280 / 1536），与区域宽度无关。
func (l *Look) SettingsContent(c ui.C, title string, body func()) {
	vw := l.ViewportWidth(c)
	px, maxWidth := l.Rem(0.75), float32(1024)
	switch {
	case vw >= 1536:
		px, maxWidth = l.Rem(2.5), 1280
	case vw >= 1280:
		px, maxWidth = l.Rem(2), 1152
	case vw >= 640:
		px = l.Rem(1.5)
	}
	py := l.Rem(1.25)
	l.ScrollArea(c, [4]float32{py, px, py, px}, func() {
		ui.Column(c).FillWidth().MaxWidth(maxWidth).Children(func() {
			l.Heading(c, 1, title, TextLG, WeightSemibold, l.T.Foreground).MarginBottom(l.Rem(1))
			body()
		})
	}).FillWidth().Grow(1).Background(l.Col(l.T.UiSectionBg))
}

// SettingSections 把若干分区竖排（渲染层的 space-y-6）。
func (l *Look) SettingSections(c ui.C, body func()) {
	ui.Column(c).FillWidth().Gap(l.Rem(1.5)).Children(body)
}

// SettingRow 是 SettingSection 里的一项：左侧标签与说明，右侧控件。
type SettingRow struct {
	Label, Description string
	Control            func()
}

// SettingSection 是设置分区：h3 标题（text-sm font-medium muted）加 app-settings-card 卡片，
// 卡片里是 SettingItem 行，除最后一行外都有 border-b border-border/70。
func (l *Look) SettingSection(c ui.C, title string, rows []SettingRow) {
	l.SettingSectionBody(c, title, func() {
		for i, row := range rows {
			l.SettingItem(c.Key(row.Label), row, i == len(rows)-1)
		}
	})
}

// SettingSectionBody 是内容自定的设置分区：h3 标题加 app-settings-card 卡片，卡片里放 body 声明的任意内容
// （渲染层的 SettingSection 带任意 children：显示设置里的按钮网格、色带、滑块都在这里面）。
func (l *Look) SettingSectionBody(c ui.C, title string, body func()) {
	ui.Column(c).FillWidth().Gap(l.Rem(0.75)).Children(func() {
		l.Heading(c, 3, title, TextSM, WeightMedium, l.T.MutedForeground)
		l.SettingsCard(c, body)
	})
}

// SettingsCard 复刻 .app-settings-card：令牌背景、1px 令牌边框、1rem 圆角与令牌阴影。
// 表面由 PaintSurface 按 Chromium 的像素吸附规则画出，子元素在 1px 边框之内。
func (l *Look) SettingsCard(c ui.C, body func()) {
	t := l.T
	surface := Surface{Bg: t.SettingsCardBg, Border: t.SettingsCardBorder, BorderW: 1, Radius: l.Rem(1), Shadow: t.SettingsCardShadow}
	ui.Column(c).FillWidth().Pad(1).Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, surface)
	}).Children(body)
}

// SettingItem 复刻渲染层的 SettingItem：flex items-center justify-between gap-4 px-4 py-3，
// 非末行带 border-b border-border/70，悬停 bg-muted/20。左侧标签 text-sm font-medium，
// 说明 text-xs muted mt-0.5；左侧可收缩（min-w-0），右侧控件不收缩。
func (l *Look) SettingItem(c ui.C, row SettingRow, last bool) {
	t := l.T
	id := ui.ID(c)
	pad := [4]float32{l.Space(3), l.Space(4), l.Space(3), l.Space(4)} // py-3 px-4
	if !last {
		pad[2]++ // border-b 占 1px，画在行盒的最底部
	}
	box := ui.Row(c).FillWidth().
		Items(ui.Center).
		Justify(ui.Between).
		Gap(l.Space(4)).
		PadSides(pad[0], pad[1], pad[2], pad[3]).
		HitWith(id, "", gfx.HitOptions{SkipFocus: true}).
		// hover:bg-muted/20：指针在行的矩形内（含行内控件的命中区）就悬停，见 hoverInside。
		// transition-colors：Tailwind 默认时长 150 ms、标准曲线（不随「减弱」档缩放，台账 B-004）。
		Draw(func(d *ui.DrawContext) {
			p := l.hoverTo(d.Canvas, id, 0, l.hoverInside(d.Canvas, id), DurationDefault)
			if p > 0 {
				d.Canvas.Fill(d.Rect, d.Fade(l.Col(t.Muted.WithAlpha(0.2*p))))
			}
		}).
		DrawOver(func(d *ui.DrawContext) {
			l.noteInside(d.Canvas, id, d.Rect)
			if last {
				return
			}
			line := gfx.Rect{X: d.Rect.X, Y: d.Rect.Y + d.Rect.Height - 1, Width: d.Rect.Width, Height: 1}
			d.Canvas.Fill(line, d.Fade(l.Col(t.Border.WithAlpha(0.7))))
		})
	box.Children(func() {
		ui.Column(c).Shrink(1).MinWidth(0).Children(func() { // min-w-0：说明长了可以收缩折行，把宽度让给 w-full 的右侧控件
			l.Text(c, row.Label, TextSM, WeightMedium, t.Foreground)
			if row.Description != "" {
				l.Text(c, row.Description, TextXS, WeightNormal, t.MutedForeground, Wrapped()).MarginTop(l.Space(0.5))
			}
		})
		if row.Control != nil {
			row.Control()
		}
	})
}
