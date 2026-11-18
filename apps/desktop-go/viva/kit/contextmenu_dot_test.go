package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// MenuItem.Dot 对照渲染层标签颜色子菜单里的色板（探针 scripts/parity/tools/probe-menu-dot.mjs 的实测，2026-11-18）：
// 16×16 的图标槽里居中一枚 12×12 的实心圆，标签从槽右侧 8px 之后起笔——与带 lucide 图标的项排版一致。

// dotMenu 打开一个只有色点项、图标项与无图标项的菜单。
func dotMenu(t *testing.T) (*harness, *menuPage) {
	t.Helper()
	p := &menuPage{at: gfx.Point{X: 40, Y: 40}, open: true}
	p.items = []MenuItem{
		{Label: "红", Dot: tokens.Tailwind("red-500")},
		{Label: "重命名", Icon: icons.Pencil},
		{Label: "删除"},
	}
	return openPage(t, still(), 400, 300, p.view), p
}

// menuWidthOf 打开只有一项的菜单，返回面板宽度：面板宽随内容收缩，所以它反映这一项的内容宽。
func menuWidthOf(t *testing.T, item MenuItem) float32 {
	t.Helper()
	p := &menuPage{at: gfx.Point{X: 40, Y: 40}, open: true, items: []MenuItem{item}}
	h := openPage(t, still(), 400, 300, p.view)
	menu, ok := h.node(gfx.RoleMenu, "")
	if !ok {
		t.Fatal("没有打开菜单")
	}
	return menu.Bounds.Width
}

func TestMenuItemDotReservesTheIconSlot(t *testing.T) {
	const label = "同样长的标签文字，菜单宽度取决于它"
	dot := menuWidthOf(t, MenuItem{Label: label, Dot: tokens.Tailwind("red-500")})
	icon := menuWidthOf(t, MenuItem{Label: label, Icon: icons.Pencil})
	plain := menuWidthOf(t, MenuItem{Label: label})
	if dot != icon {
		t.Errorf("色点项的面板宽 %v，应与图标项的 %v 相同（都占 16px 的槽加 8px 间距）", dot, icon)
	}
	if got := dot - plain; got != 24 {
		t.Errorf("色点项比无图标项宽 %v，期望 24（16px 槽 + 8px 间距）", got)
	}
}
func TestMenuItemDotIsATwelvePixelCircleCenteredInTheSlot(t *testing.T) {
	h, _ := dotMenu(t)
	row, ok := nodeByLabel(h, "红")
	if !ok {
		t.Fatal("没有「红」这一项")
	}
	// 槽：行左缘 + px-3（12px）起、16px 见方、竖向居中；圆心就是槽心。
	cx, cy := row.Bounds.X+12+8, row.Bounds.Y+row.Bounds.Height/2
	red := [4]uint8{239, 68, 68, 255}
	bg := pixelAt(h, row.Bounds.X+row.Bounds.Width-4, cy) // 行右端一块没有内容的底色
	if bg == red {
		t.Fatal("取到的底色不该是红色")
	}
	// 紧贴 12px 圆的边缘（半径 6）的像素带抗锯齿，允许少量混色，但必须仍明显是红色：圆没有缩成 8px 或 10px。
	for _, dx := range []float32{-5.5, 5.5} {
		got := pixelAt(h, cx+dx, cy)
		if got[0] < 230 || got[1] > 90 || got[2] > 90 {
			t.Errorf("圆缘内侧偏 %v：像素 %v 不是红色，圆比 12px 小", dx, got)
		}
	}
	for _, p := range []struct {
		name   string
		dx, dy float32
		want   [4]uint8
	}{
		{"圆心", 0, 0, red},
		{"右缘内侧（偏 4.5）", 4.5, 0, red},
		{"左缘内侧", -4.5, 0, red},
		{"上缘内侧", 0, -4.5, red},
		{"下缘内侧", 0, 4.5, red},
		{"槽内圆外（半径 7.5，在 12px 圆之外、16px 槽之内）", 7.5, 0, bg},
		{"槽的角（斜向 7.5, 7.5）", 7.5, 7.5, bg},
	} {
		if got := pixelAt(h, cx+p.dx, cy+p.dy); got != p.want {
			t.Errorf("%s：像素 %v，期望 %v", p.name, got, p.want)
		}
	}
}

func TestMenuItemDotDoesNotChangeOnHoverAndIconWins(t *testing.T) {
	h, p := dotMenu(t)
	p.items[1].Dot = tokens.Tailwind("blue-500") // 图标与色点同时给：画图标
	h.frames(2)
	row, _ := nodeByLabel(h, "红")
	hoverItem(h, "红")
	h.frames(2)
	cx, cy := row.Bounds.X+12+8, row.Bounds.Y+row.Bounds.Height/2
	if got := pixelAt(h, cx, cy); got != [4]uint8{239, 68, 68, 255} {
		t.Errorf("悬停后色点变了：%v（色点是固定的品牌色，不随悬停变化）", got)
	}
	icon, _ := nodeByLabel(h, "重命名")
	if got := pixelAt(h, icon.Bounds.X+12+8, icon.Bounds.Y+icon.Bounds.Height/2); got == [4]uint8{59, 130, 246, 255} {
		t.Error("图标与色点同时给时应画图标，画成了色点")
	}
}
