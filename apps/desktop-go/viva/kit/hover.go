package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// HoverProgress 是状态过渡的显示进度（0..1）：目标由 hovered 决定，时长取 n 档（DurationNone 直接切换）。
// 渲染层的 transition-colors、transition-opacity 都是「状态一变就在这个时长里补间」，业务界面据此给颜色或不透明度做插值；
// slot 区分同一个命中 ID 上的多个动画量（背景、文字、不透明度各占一个）。
func (l *Look) HoverProgress(c ui.C, id gfx.ID, slot uint8, hovered bool, n DurationName) float32 {
	return l.hoverTo(c.Canvas(), id, slot, hovered, n)
}

// Fade 把令牌颜色的不透明度乘以 factor（渲染层 opacity-50、opacity-0 之类作用在整个元素上时，逐个图元乘）。
func Fade(c tokens.Color, factor float32) tokens.Color { return fade(c, factor) }
