package kit

import "github.com/vivaui/viva/gfx"

// HoverGroups 把若干命中区归到命名的分组里，回答「指针此刻在不在某个分组里」「键盘焦点在不在某个分组里」，
// 对应 Tailwind 的 group-hover/<名> 与 group-focus-within/<名>（CSS :hover 与 :focus-within 对后代也成立）。
//
// Viva 的悬停是按单个命中区算的，没有「后代里有悬停」这回事；这里让一个分组的各命中区在声明时各自登记，
// 下一帧起用 Canvas 当前的悬停/焦点 ID 去查它属于哪个分组。登记是逐帧重建的：用上一帧的登记回答本帧的查询，
// 所以悬停状态到达绘制总比指针晚一帧，与 Box.State() 的口径一致，对用户不可见。
//
// 零值可用。一个列表一个 HoverGroups，每帧在声明列表之前调用 Begin。
type HoverGroups struct {
	prev, cur map[gfx.ID]string
}

// Begin 开始新一帧的登记：本帧的登记变成「上一帧」，供本帧的查询使用。
func (g *HoverGroups) Begin() {
	g.prev, g.cur = g.cur, g.prev
	if g.cur == nil {
		g.cur = map[gfx.ID]string{}
	}
	clear(g.cur)
}

// Add 把命中区 id 登记到 group 里。重复登记无害。
func (g *HoverGroups) Add(group string, id gfx.ID) {
	if g.cur == nil {
		g.cur = map[gfx.ID]string{}
	}
	g.cur[id] = group
}

// Hovered 返回指针此刻所在命中区所属的分组（空串表示不在任何分组里）。
func (g *HoverGroups) Hovered(canvas *gfx.Canvas) string {
	return g.prev[canvas.Hovered()]
}

// Focused 返回键盘焦点此刻所在命中区所属的分组（空串表示焦点不在任何分组里）。
func (g *HoverGroups) Focused(canvas *gfx.Canvas) string {
	return g.prev[canvas.Focused()]
}

// Active 报告 group 此刻是否处于悬停或焦点在内的状态（group-hover 或 group-focus-within 任一成立）。
func (g *HoverGroups) Active(canvas *gfx.Canvas, group string) bool {
	return g.Hovered(canvas) == group || g.Focused(canvas) == group
}
