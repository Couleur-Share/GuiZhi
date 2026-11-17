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

// overlayPanelRoles 是 React 一侧浮层面板的角色：对话框、下拉面板、菜单、气泡。面板是不透明的，压在它下面的页面文字没有墨迹可比。
var overlayPanelRoles = map[string]bool{"dialog": true, "alertdialog": true, "listbox": true, "menu": true, "tooltip": true}

// overlayPanelRects 是 React 一侧浮层面板的外框（取色器弹层、下拉面板、对话框、气泡）：位置与大小由外框比较保证两边一致，
// 所以用 React 一侧的就够了。
func overlayPanelRects(f *Facts) []Box {
	if f == nil {
		return nil
	}
	var out []Box
	for _, n := range f.Nodes {
		if overlayPanelRoles[n.Role] && len(n.Box) == 4 {
			out = append(out, Box{n.Box[0], n.Box[1], n.Box[2], n.Box[3]})
		}
	}
	return out
}

// occludedByOverlayInk 报告页面层元素 rect 的墨迹比较区域（checkInk 取的范围：外框向外取整再外扩 1 像素）
// 是否与某个浮层墨迹或浮层面板的外框重叠（只贴边不算）：面板整个盖住页面文字时（取色器弹层压在预览条上），
// 量墨迹的区域里是面板自己的内容；只盖住一部分时是面板的边与阴影。两种都不是这行文字的墨迹。
func occludedByOverlayInk(rect Box, overlayInk, panels []Box) bool {
	x0, y0 := math.Floor(rect.X())-1, math.Floor(rect.Y())-1
	x1, y1 := math.Ceil(rect.Right())+1, math.Ceil(rect.Bottom())+1
	for _, group := range [][]Box{overlayInk, panels} {
		for _, o := range group {
			if o.X() < x1 && x0 < o.Right() && o.Y() < y1 && y0 < o.Bottom() {
				return true
			}
		}
	}
	return false
}
