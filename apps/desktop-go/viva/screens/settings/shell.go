package settings

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// SectionID 是设置分区的 ID，取值与渲染层 SettingsSectionId 相同。
type SectionID string

const (
	SectionGeneral    SectionID = "general"
	SectionAppearance SectionID = "appearance"
	SectionData       SectionID = "data"
	SectionNetwork    SectionID = "network"
	SectionCapture    SectionID = "capture"
	SectionAI         SectionID = "ai"
	SectionMCP        SectionID = "mcp"
	SectionShortcuts  SectionID = "shortcuts"
	SectionAbout      SectionID = "about"
)

// menuEntry 是侧栏的一项（渲染层 SETTINGS_MENU）。
type menuEntry struct {
	id    SectionID
	label func() string
	icon  gfx.Icon
}

// sectionMenu 是侧栏的分区，顺序与图标同渲染层。
var sectionMenu = []menuEntry{
	{SectionGeneral, locales.SettingsGeneral, icons.Settings},
	{SectionAppearance, locales.SettingsAppearance, icons.Palette},
	{SectionData, locales.SettingsData, icons.Database},
	{SectionNetwork, locales.SettingsNetwork, icons.Wifi},
	{SectionCapture, locales.SettingsCaptureAndTranscription, icons.RadioTower},
	{SectionAI, locales.SettingsAi, icons.Brain},
	{SectionMCP, locales.SettingsMcp, icons.Plug},
	{SectionShortcuts, locales.SettingsShortcuts, icons.Keyboard},
	{SectionAbout, locales.SettingsAbout, icons.Info},
}

// compactBreakpoint 是 Tailwind sm 断点（640px 视口宽度）：窄于它时侧栏只剩图标。
const compactBreakpoint = 640

// Shell 是设置页的外壳（渲染层 SettingsPage）：左侧「返回」加分区菜单，右侧是当前分区的内容。
type Shell struct {
	Look  *kit.Look
	Store Store
	// Guard 是设置写入失败的通道（错误提示 + 日志），交给各分区页；零值时失败不弹提示。
	Guard kit.Guard
	// Active 是当前分区；空表示应用设置（打开设置页时的默认分区）。
	Active SectionID
	// OnBack 是点「返回」时的回调（回到知识库等模块页）。
	OnBack func()

	// PickBackground 与 Wake 交给「显示设置」页的背景图分区（见 Appearance 同名字段）。
	PickBackground func() (fileName string, err error)
	Wake           func()

	general    *General
	appearance *Appearance
}

// section 返回当前分区。
func (s *Shell) section() SectionID {
	if s.Active == "" {
		return SectionGeneral
	}
	return s.Active
}

// Page 声明整个设置页：侧栏与内容并排，占满父容器。
func (s *Shell) Page(c ui.C) {
	ui.Row(c).Fill().Children(func() {
		s.nav(c)
		s.content(c)
	})
}

// content 是右侧内容区：按分区分派；还没迁移的分区如实说明，而不是一个看起来能用的空页。
func (s *Shell) content(c ui.C) {
	l := s.Look
	if s.loadFailed(c) {
		return
	}
	switch s.section() {
	case SectionGeneral:
		if s.general == nil {
			s.general = &General{}
		}
		s.general.Look, s.general.Store, s.general.Guard = l, s.Store, s.Guard
		s.general.Page(c)
	case SectionAppearance:
		if s.appearance == nil {
			s.appearance = &Appearance{}
		}
		a := s.appearance
		a.Look, a.Store, a.Guard, a.PickBackground, a.Wake = l, s.Store, s.Guard, s.PickBackground, s.Wake
		a.Page(c)
	default:
		title := s.sectionTitle()
		l.SettingsContent(c, title, func() {
			l.Text(c, locales.NativeShellNotMigrated(title), kit.TextSM, kit.WeightMedium, l.T.Foreground)
			l.Text(c, locales.NativeShellNotMigratedHint(), kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped()).MarginTop(l.Space(2))
		})
	}
}

// sectionTitle 是当前分区在侧栏里的名字（内容区的 h1 标题）。
func (s *Shell) sectionTitle() string {
	for _, m := range sectionMenu {
		if m.id == s.section() {
			return m.label()
		}
	}
	return ""
}

// loadFailed 在设置首次读取失败时把内容区画成「读取失败」加重试，返回是否画了（台账 B-009）。
// 失败时 Store 里只有默认值：不能把它们当成真实设置渲染，否则用户会对着一页看似正常的默认开关做修改。
// 渲染层的设置页没有这个状态（设置存在渲染进程本地），这是 AGENTS.md「加载失败用 LoadErrorState」要求下的有意增强，
// 登记在台账「有意的增强」。重试在后台读取，失败走 Guard（日志与带原因的错误提示）。
func (s *Shell) loadFailed(c ui.C) bool {
	lr, ok := s.Store.(LoadReporter)
	if !ok {
		return false
	}
	err := lr.LoadError()
	if err == nil {
		return false
	}
	s.Look.SettingsContent(c, s.sectionTitle(), func() {
		if s.Look.LoadErrorState(c, err.Error()) {
			s.Guard.Go(locales.NativeSettingsLoadAction(), lr.Reload, nil)
		}
	})
	return true
}

// nav 是左侧栏：w-56（窄窗口 w-14）、面板底色、右侧 1px 面板边框；上面是「返回」，下面是分区菜单。
func (s *Shell) nav(c ui.C) {
	l := s.Look
	t := l.T
	wide := l.ViewportWidth(c) >= compactBreakpoint
	width := l.Rem(14)
	if !wide {
		width = l.Rem(3.5)
	}
	ui.Column(c).Width(width).Shrink(0).FillHeight().PadRight(1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.UiPanelBg, Shadow: t.UiPanelShadow})
			l.PaintEdge(d, kit.EdgeRight, 1, t.UiPanelBorder)
		}).
		Children(func() {
			s.back(c, wide)
			ui.Column(c).FillWidth().Grow(1).Pad(l.Space(2)).Gap(l.Space(1)).Children(func() {
				for _, m := range sectionMenu {
					if s.navItem(c.Key(string(m.id)), wide, m) {
						s.Active = m.id
					}
				}
			})
		})
}

// back 是侧栏顶部的「返回」按钮：p-3 border-b；按钮 gap-2 text-sm muted，悬停变前景色。窄窗口只有箭头。
func (s *Shell) back(c ui.C, wide bool) {
	l := s.Look
	t := l.T
	label := locales.CommonBack()
	pad := l.Space(3)
	ui.Column(c).FillWidth().PadSides(pad, pad, pad+1, pad).
		DrawOver(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			c := c.Key("back")
			id := ui.ID(c)
			box := ui.Row(c).Self(ui.Start).Items(ui.Center).Gap(l.Space(2)).
				HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton})
			color := t.MutedForeground
			if box.State().Hovered {
				color = t.Foreground
			}
			box.Children(func() {
				l.Icon(c, icons.ArrowLeft, l.Rem(1), color)
				if wide {
					l.Text(c, label, kit.TextSM, kit.WeightNormal, color)
				}
			})
			if box.Clicked() && s.OnBack != nil {
				s.OnBack()
			}
		})
}

// navItem 画一项分区按钮，返回是否被点击：w-full，gap-3，py-2，px-3（窄窗口 px-2 且居中），rounded-lg，text-[13px]。
// 选中：bg-primary text-primary-foreground shadow-sm；其余 text-foreground/80，悬停 bg-muted/70。
func (s *Shell) navItem(c ui.C, wide bool, m menuEntry) bool {
	l := s.Look
	t := l.T
	label := m.label()
	active := s.section() == m.id
	id := ui.ID(c)
	px := l.Space(3)
	justify := ui.Start
	if !wide {
		px, justify = l.Space(2), ui.Center
	}
	box := ui.Row(c).FillWidth().Items(ui.Center).Justify(justify).Gap(l.Space(3)).
		PadSides(l.Space(2), px, l.Space(2), px).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Selected: active})
	hovered := box.State().Hovered
	color := t.Foreground.WithAlpha(0.8)
	surface := kit.Surface{Radius: l.Rem(0.5)}
	switch {
	case active:
		color = t.PrimaryForeground
		surface.Bg, surface.Shadow = t.Primary, t.ShadowSm
	case hovered:
		surface.Bg = t.Muted.WithAlpha(0.7)
	}
	box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
		l.Icon(c, m.icon, l.Rem(1), color).Shrink(0)
		if wide {
			// text-[13px]：px 字面量，不随字号档缩放；没写行高，继承 1.5 倍。
			l.TextPx(c, label, 13, 13*1.5, kit.WeightNormal, color)
		}
	})
	return box.Clicked()
}
