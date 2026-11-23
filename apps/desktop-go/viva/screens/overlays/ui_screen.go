package overlays

// 全局浮层的界面：命令面板、更新对话框、首次使用设置清单与旧版数据迁移对话框（台账 B-008、B-016、B-026，M3-6a Step 2）。
// 状态与服务调用在各 Store 里（palette.go、update.go、setup.go、migration.go）；Screen 持有它们和各自的界面状态，
// 每一帧经 View 声明全部浮层，没打开的浮层什么也不画（Modal 靠每帧经过来复位）。

import (
	"fmt"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// Screen 是全局浮层的界面。字段由 Root 装配；某个 Store 为 nil 表示没有那个浮层（装配不全的自动化实例）。
type Screen struct {
	Look      *kit.Look
	Palette   *Palette
	Update    *Update
	Setup     *Setup
	Migration *Migration
	// OpenExternal 用系统浏览器打开更新说明里的链接；nil 时链接只画样式、不可点。
	OpenExternal func(url string) error
	// Guard 是点击处理里失败时的提示通道（打开链接失败）。
	Guard kit.Guard

	palette paletteUI
	update  updateUI
	setup   setupUI
	migrate migrationUI
}

// View 声明全部浮层。Window.Dialogs 每帧调用它：层级高低由各浮层自己的层号决定（对话框在第 3 层，命令面板在最高层），
// 同层里后声明的先收到点击与 Esc，所以更新对话框（用户主动点开）排在引导与迁移之后。
func (s *Screen) View(c ui.C) {
	if s.Setup != nil {
		s.setupDialog(c)
	}
	if s.Migration != nil {
		s.migrationDialog(c)
	}
	if s.Update != nil {
		s.updateDialog(c)
	}
	if s.Palette != nil {
		s.paletteView(c)
	}
}

// dlgKind 是对话框里按钮的外观。
type dlgKind uint8

const (
	// dlgPrimary 是 rounded-xl bg-primary px-4 py-2.5 text-sm font-medium text-primary-foreground shadow-sm hover:bg-primary/90。
	dlgPrimary dlgKind = iota
	// dlgSecondary 是 rounded-xl border border-border bg-background/80 px-4 py-2.5 text-sm font-medium hover:bg-muted/60。
	dlgSecondary
	// dlgMuted 是 rounded-xl bg-muted px-4 py-2.5 text-sm font-medium hover:bg-muted/80。
	dlgMuted
	// dlgSuccess 是 rounded-xl bg-green-600 px-4 py-2.5 text-sm font-medium text-white shadow-sm hover:bg-green-700。
	dlgSuccess
)

// dlgStyle 是更新对话框按钮的样式：h 由 py-2.5 加 text-sm 行高（20）得到 40，带边框的再加上下各 1px。
func (s *Screen) dlgStyle(k dlgKind) kit.ButtonStyle {
	l := s.Look
	t := l.T
	st := kit.ButtonStyle{
		Height: l.Rem(2.5), PadX: l.Space(4), Radius: l.Rem(0.75), Gap: l.Space(2), Text: kit.TextSM, Weight: kit.WeightMedium,
		Fg: t.Foreground, Transition: kit.DurationDefault, Surface: t.UiPanelStrongBg,
	}
	switch k {
	case dlgPrimary:
		st.Fg, st.Bg, st.BgHover, st.Shadows = t.PrimaryForeground, t.Primary, t.Primary.WithAlpha(0.9), kit.ShadowSM
	case dlgSecondary:
		st.Height += 2
		st.Bg, st.BgHover, st.Border, st.BorderW = t.Background.WithAlpha(0.8), t.Muted.WithAlpha(0.6), t.Border, 1
	case dlgMuted:
		st.Bg, st.BgHover = t.Muted, t.Muted.WithAlpha(0.8)
	case dlgSuccess:
		st.Fg, st.Bg, st.BgHover, st.Shadows = tokens.RGBA(255, 255, 255, 1), tokens.Tailwind("green-600"), tokens.Tailwind("green-700"), kit.ShadowSM
	}
	return st
}

// dlgButton 画一枚对话框按钮并返回是否被点击。
func (s *Screen) dlgButton(c ui.C, k dlgKind, spec kit.ButtonSpec) bool {
	return s.Look.Button(c, spec, s.dlgStyle(k)).Clicked()
}

// uiPrimary 是通用按钮（components/ui/Button.tsx）的 primary 变体：与 kit 的 ghost 同尺寸，换主色底（kit 只做了 ghost 与 secondary，
// 见 kit/uibutton.go 里的台账 B-039 说明；与渲染层的差异：没有主色阴影与悬停放大）。
func (s *Screen) uiPrimary(size kit.UIButtonSize) kit.ButtonStyle {
	t := s.Look.T
	st := s.Look.UIGhostButton(size)
	st.Fg, st.Bg, st.BgHover = t.PrimaryForeground, t.Primary, t.Primary
	st.Ring = t.Primary
	return st
}

// formatPercent 是下载百分比的显示：保留一位小数（渲染层 percent.toFixed(1)）。
func formatPercent(p float64) string { return fmt.Sprintf("%.1f", p) }