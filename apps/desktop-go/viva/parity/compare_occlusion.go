package parity

import "math"

// 页面层的文字与图标被浮层（悬停提示、下拉面板）盖住一部分时，量墨迹的区域里混进了浮层自己的字形：
// 两边的浮层文字按 A-009（灰度抗锯齿）本来就有墨迹总量的偏差，这个偏差不是页面文字的偏差。
// 这类元素不比墨迹——区域里量到的不是它自己的墨迹；它的位置与宽度仍照常比（外框、文字宽度），
// 它在没有浮层的场景里的墨迹也有别的场景覆盖。台账 A-009 记着这条规则；报告里单独计数（L2Result.InkOccluded），不悄悄丢掉。

// overlayInkRects 是 Viva 一侧画在浮层层级里的文字行与图标的外框：气泡、下拉面板、对话框自己的墨迹。
// 被滚动容器或窗口边缘裁得一点不剩的文字没有画出来，不算。
func overlayInkRects(f *Facts) []Box {
	if f == nil {
		return nil
	}
	var out []Box
	for _, t := range f.Texts {
		if t.Overlay && !t.Hidden {
			out = append(out, t.Rects...)
		}
	}
	return append(out, f.OverlayIcons...)
}

// occludedByOverlayInk 报告页面层元素 rect 的墨迹比较区域（checkInk 取的范围：外框向外取整再外扩 1 像素）
// 是否与某个浮层墨迹的外框重叠（只贴边不算）。
func occludedByOverlayInk(rect Box, overlayInk []Box) bool {
	x0, y0 := math.Floor(rect.X())-1, math.Floor(rect.Y())-1
	x1, y1 := math.Ceil(rect.Right())+1, math.Ceil(rect.Bottom())+1
	for _, o := range overlayInk {
		if o.X() < x1 && x0 < o.Right() && o.Y() < y1 && y0 < o.Bottom() {
			return true
		}
	}
	return false
}
