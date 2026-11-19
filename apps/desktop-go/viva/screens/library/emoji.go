package library

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// emojiAdvanceEm 是彩色表情字形的前进宽度，单位 em。渲染层（Chromium + Segoe UI Emoji）实测 14px 时 19.23px、
// 16px 时 21.98px，约 1.3736em；Viva 的文字引擎量同一个字形是 1.3730em（无头会话里 14px 时 19.22px，差不到 0.01px），
// 所以两边的字形一样宽，不需要补宽度，这个常量只用来算「字形比容器宽出多少」。
const emojiAdvanceEm = 1.3736

// emojiOverflow 是表情标签上下各多留的像素：彩色字形的墨迹比 size 高的行盒还高（Segoe UI Emoji 在 16px 时内容区 20px，
// 🌊 的底边超出行盒 0.79px），Viva 的文字标签只栅格化标签自己的矩形，超出的一行被裁掉，Chromium 不裁。
// 标签多留 2×4px、外层上下各 −4px 的外边距，布局高度与基线位置不变（行高差是偶数整数，两边的基线都恰好整体下移 4px）。
const emojiOverflow float32 = 4

// emojiGlyph 画一个彩色表情字形，size 是字号（逻辑像素），slot 是它所在的居中容器的宽度（0 表示不在固定宽度的容器里）。
//
// 容器比字形窄时（侧栏行的图标槽 1rem 放 0.875rem 的表情，前进宽度 1.3736×14 = 19.23px）：CSS 的
// justify-content: center 让内容向两侧各溢出一半（起点 −1.6px），Viva 的居中把负的剩余空间当 0，溢出全落在右边
// （实测：字形起点 100.00，渲染层 98.39）。所以给字形两侧各加一半溢出量的负外边距，让它的外边距盒正好等于容器宽度，
// 居中之后起点就是 −溢出/2。
//
// 渲染层的彩色字形按文字色的 alpha 整体合成（行上的图标是 text-sidebar-foreground/70，所以表情是 70% 不透明），
// 而 Viva 的彩色字形不理会文字色的 alpha、总以满不透明绘制：着色器在这种纹理上直接取像素颜色，不乘文字色 alpha
// （`internal/platform/windows/shaders/scene.hlsl` 的 textFragment，textMode 0）。这条差异登记在 A-023；
// 这里仍把文字色取成不透明、alpha 交给外层盒的整体不透明度——单色字形（文字字体里的符号）得到同样的结果。
func (s *Screen) emojiGlyph(c ui.C, emoji string, size, slot float32, fg tokens.Color) {
	l := s.Look
	advance := size * emojiAdvanceEm
	box := ui.Row(c).Width(advance).Shrink(0).Opacity(fg.A).MarginTop(-emojiOverflow).MarginBottom(-emojiOverflow)
	if over := advance - slot; slot > 0 && over > 0 {
		box = box.MarginLeft(-over / 2).MarginRight(-over / 2)
	}
	box.Children(func() {
		l.TextPx(c.Key("emoji"), emoji, size, size+2*emojiOverflow, kit.WeightNormal, fg.WithAlpha(1))
	})
}
