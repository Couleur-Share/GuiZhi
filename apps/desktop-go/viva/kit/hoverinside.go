package kit

import (
	"slices"

	"github.com/vivaui/viva/gfx"
)

// insideHits 登记每个命中区「整个包在自己外框里的后代命中区」，让祖先也能回答「指针此刻在不在我里面」。
//
// CSS 的 :hover 对祖先也成立（行里的下拉框、开关、按钮上悬停时整行仍是悬停）；Viva 的悬停只落在指针下最上层的那一个命中区
// （Canvas.Hovered 是单个 ID），后代盖住祖先时祖先不再是悬停目标（台账 B-050）。
// 祖先在自己的底色绘制之前看不到后代的命中区（后代的命中区在子树绘制时才登记），所以在祖先的 DrawOver 里（子树已画完）
// 扫一遍本帧的命中区、把外框完全落在祖先外框里的记下来，下一帧起用这份登记回答（后代的版面一帧之内不会变）。
// 与 HoverGroups 同口径：悬停状态到达绘制总比指针晚一帧，用 RequestFrame 补上这一帧，对用户不可见。
//
// 外框完全在祖先里就足以断定指针在祖先里；浮层（展开的下拉列表、菜单）的外框越出所在的行，不会被误当成行内控件。
type insideHits struct {
	m map[gfx.ID][]gfx.ID
}

// hoverInside 报告指针是否在命中区 id 之内：直接落在 id 上，或落在登记过的后代命中区上。
func (l *Look) hoverInside(canvas *gfx.Canvas, id gfx.ID) bool {
	hovered := canvas.Hovered()
	if hovered.IsZero() {
		return false
	}
	return hovered == id || slices.Contains(l.inside.m[id], hovered)
}

// noteInside 在祖先 id（外框 rect）画完子树之后重建它的后代登记；指针所在的命中区是否算「在内」变了，就请求再画一帧。
func (l *Look) noteInside(canvas *gfx.Canvas, id gfx.ID, rect gfx.Rect) {
	if l.inside.m == nil {
		l.inside.m = map[gfx.ID][]gfx.ID{}
	}
	hovered := canvas.Hovered()
	was := slices.Contains(l.inside.m[id], hovered)
	list := l.inside.m[id][:0]
	for _, h := range canvas.Hits() {
		b := h.Bounds
		if h.ID.IsZero() || h.ID == id || b.X < rect.X || b.Y < rect.Y || b.X+b.Width > rect.X+rect.Width || b.Y+b.Height > rect.Y+rect.Height {
			continue
		}
		list = append(list, h.ID)
	}
	l.inside.m[id] = list
	if !hovered.IsZero() && hovered != id && was != slices.Contains(list, hovered) {
		canvas.RequestFrame()
	}
}
