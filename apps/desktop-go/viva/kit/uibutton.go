package kit

import "guizhi-mygo-shell/viva/tokens"

// UIButtonSize 是渲染层通用按钮（components/ui/Button.tsx）的尺寸档。
type UIButtonSize uint8

const (
	// UIButtonSM 是 h-8 px-3 text-xs。
	UIButtonSM UIButtonSize = iota
	// UIButtonMD 是 h-10 px-5 text-sm。
	UIButtonMD
	// UIButtonLG 是 h-12 px-6 text-base。
	UIButtonLG
)

// UISecondaryButton 是通用按钮的 secondary 变体（app-wallpaper-surface border border-border text-foreground hover:bg-accent
// focus:ring-border）：app-wallpaper-surface 的 background、border-color、box-shadow 都带 !important，盖掉了 hover:bg-accent
// （悬停没有变化）与 border-border，也盖掉了 focus:ring-2（环是 box-shadow 实现的），所以聚焦时没有环——调用方给按钮传 NoRing。
// 其余同 ghost（尺寸、圆角、字重、过渡），与渲染层不同之处同样见台账 B-039 的按下缩放。
func (l *Look) UISecondaryButton(size UIButtonSize) ButtonStyle {
	t := l.T
	st := l.UIGhostButton(size)
	st.Bg, st.BgHover = t.UiSurfaceBg, t.UiSurfaceBg
	st.Border, st.BorderW, st.Shadow = t.UiSurfaceBorder, 1, t.UiSurfaceShadow
	st.Ring, st.FocusRing = tokens.Color{}, false
	return st
}

// UIGhostButton 是通用按钮的 ghost 变体（bg-transparent text-foreground hover:bg-accent focus:ring-border）：
// 基础类是 inline-flex items-center justify-center gap-2 font-semibold rounded-xl transition-all duration-base，
// 禁用时 opacity-50（ButtonStyle 的 DisabledOpacity 缺省 0.5）。
//
// 只做了 ghost 与 secondary：primary、danger 两个变体等到第一处用到它们的界面迁入时再加，由那里的对照守住。
// 与渲染层不同之处（台账 B-039）：聚焦环没有 ring-offset-2 的那一圈留白，按下时没有 active:scale-[0.98] 的缩放；
// 两者都只在键盘或鼠标按住的瞬间可见，静止的画面不受影响。
func (l *Look) UIGhostButton(size UIButtonSize) ButtonStyle {
	t := l.T
	st := ButtonStyle{
		Radius: l.Rem(0.75), Gap: l.Space(2), Weight: WeightSemibold,
		Fg: t.Foreground, Bg: tokens.Transparent, BgHover: t.Accent,
		Transition: DurationBase,
		Ring:       t.Border, FocusRing: true, // focus:ring-2 focus:ring-border：环跟 :focus 走，点击后仍留着
	}
	switch size {
	case UIButtonMD:
		st.Height, st.PadX, st.Text = l.Rem(2.5), l.Space(5), TextSM
	case UIButtonLG:
		st.Height, st.PadX, st.Text = l.Rem(3), l.Space(6), TextBase
	default:
		st.Height, st.PadX, st.Text = l.Rem(2), l.Space(3), TextXS
	}
	return st
}
