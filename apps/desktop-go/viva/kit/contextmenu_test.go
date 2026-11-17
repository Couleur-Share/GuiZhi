package kit

import (
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
)

const (
	menuItemCapture = "快速采集 粘贴文本、网页或视频链接 Alt+Shift+N"
	menuItemBlank   = "空白笔记 直接新建一条空白条目"
	menuItemFiles   = "采集文件 选择文档、图片或音视频加入采集队列"
)

// menuPage 是一个带上下文菜单的页面：触发按钮用来观察「点触发只收起」，背后的按钮用来观察「点外面会不会传下去」。
type menuPage struct {
	st     MenuState
	open   bool
	at     gfx.Point
	items  []MenuItem
	picked []string
	closed int // ContextMenu 要求关闭的次数
	behind int // 背后那枚按钮被点中的次数
	opens  int // 触发按钮收到点击的次数
	keep   gfx.Rect
	// behindRect 是背后那枚按钮上一帧的外框：菜单打开时整窗背景层盖住了它，按标签点不到，只能按坐标点。
	behindRect gfx.Rect
}

func (p *menuPage) view(l *Look, c ui.C) {
	ui.Column(c).Fill().Pad(24).Gap(24).Children(func() {
		tc, bc := c.Key("trigger"), c.Key("behind")
		trigger := ui.Row(tc).Size(120, 40).HitWith(ui.ID(tc), "触发", gfx.HitOptions{Role: gfx.RoleButton}).
			DrawOver(func(d *ui.DrawContext) { p.keep = d.Rect })
		if trigger.Clicked() {
			p.opens++
			p.open = !p.open
		}
		behind := ui.Row(bc).Size(120, 40).HitWith(ui.ID(bc), "背后的按钮", gfx.HitOptions{Role: gfx.RoleButton}).
			DrawOver(func(d *ui.DrawContext) { p.behindRect = d.Rect })
		if behind.Clicked() {
			p.behind++
		}
	})
	keep := p.keep
	if l.ContextMenu(c, &p.st, MenuSpec{Open: p.open, X: p.at.X, Y: p.at.Y, Items: p.items, Keep: &keep}) {
		p.open = false
		p.closed++
	}
}

// testMenuItems 是三项：与顶栏创建方式菜单同形（图标、说明，第一项带快捷键提示）。
func testMenuItems(p *menuPage) []MenuItem {
	pick := func(name string) func() { return func() { p.picked = append(p.picked, name) } }
	return []MenuItem{
		{Label: "快速采集", Description: "粘贴文本、网页或视频链接", Icon: icons.Sparkles, Shortcut: "Alt+Shift+N", OnSelect: pick("快速采集")},
		{Label: "空白笔记", Description: "直接新建一条空白条目", Icon: icons.StickyNote, OnSelect: pick("空白笔记")},
		{Label: "采集文件", Description: "选择文档、图片或音视频加入采集队列", Icon: icons.FileUp, OnSelect: pick("采集文件")},
	}
}

func openMenuPage(t *testing.T, spec Spec, configure func(p *menuPage)) (*harness, *menuPage) {
	t.Helper()
	p := &menuPage{at: gfx.Point{X: 300, Y: 100}}
	p.items = testMenuItems(p)
	if configure != nil {
		configure(p)
	}
	return openPage(t, spec, 800, 600, p.view), p
}

// openMenu 点触发按钮让菜单出现。
func openMenu(h *harness) {
	h.t.Helper()
	h.click("触发")
}

func menuNodes(h *harness) (menu, first, second, third nodeLite) {
	h.t.Helper()
	get := func(role gfx.Role, name string) nodeLite {
		n, ok := h.node(role, name)
		if !ok {
			h.t.Fatalf("没有找到 %v「%s」", role, name)
		}
		return nodeLite{Bounds: n.Bounds, Focused: n.Focused, Disabled: n.Disabled}
	}
	return get(gfx.RoleMenu, ""), get(gfx.RoleMenuItem, menuItemCapture), get(gfx.RoleMenuItem, menuItemBlank), get(gfx.RoleMenuItem, menuItemFiles)
}

// nodeLite 是测试关心的可访问节点字段。
type nodeLite struct {
	Bounds   gfx.Rect
	Focused  bool
	Disabled bool
}

func focusedItem(h *harness) string {
	h.t.Helper()
	for _, name := range []string{menuItemCapture, menuItemBlank, menuItemFiles} {
		if n, ok := h.node(gfx.RoleMenuItem, name); ok && n.Focused {
			return name
		}
	}
	return ""
}

func TestContextMenuClosedDrawsNothing(t *testing.T) {
	h, _ := openMenuPage(t, still(), nil)
	if h.has(gfx.RoleMenu, "") || h.has(gfx.RoleMenuItem, menuItemCapture) {
		t.Error("没有打开时不应出现菜单")
	}
}

// 几何：左上角就是锚点；三项各 48 高（py-1.5 + 20 + 16），面板 = 项高之和 + py-1 + 边框 = 154（与渲染层实测一致）；
// 项宽 = 面板宽 − 2（边框）；面板宽取最宽一项的内容宽（w-max），不小于 160。
func TestContextMenuGeometry(t *testing.T) {
	h, _ := openMenuPage(t, still(), nil)
	openMenu(h)
	menu, first, second, third := menuNodes(h)
	if menu.Bounds.X != 300 || menu.Bounds.Y != 100 {
		t.Errorf("面板左上角 (%g,%g)，期望锚点 (300,100)", menu.Bounds.X, menu.Bounds.Y)
	}
	if menu.Bounds.Height != 154 {
		t.Errorf("面板高 %g，期望 154", menu.Bounds.Height)
	}
	if menu.Bounds.Width < menuMinWidth {
		t.Errorf("面板宽 %g，小于 min-w 160", menu.Bounds.Width)
	}
	for i, n := range []nodeLite{first, second, third} {
		if n.Bounds.Height != 48 {
			t.Errorf("第 %d 项高 %g，期望 48", i+1, n.Bounds.Height)
		}
		if n.Bounds.X != menu.Bounds.X+1 || math.Abs(float64(n.Bounds.Width-(menu.Bounds.Width-2))) > 0.01 {
			t.Errorf("第 %d 项横向 (%g,宽 %g)，期望贴着面板边框内侧、宽 %g", i+1, n.Bounds.X, n.Bounds.Width, menu.Bounds.Width-2)
		}
		if want := menu.Bounds.Y + 1 + 4 + float32(i)*48; n.Bounds.Y != want {
			t.Errorf("第 %d 项 y=%g，期望 %g（边框 1 + py-1 4 + 前面各项）", i+1, n.Bounds.Y, want)
		}
	}
}

// 面板宽度由最宽一项决定：只有短标签时取 min-w 160；加一个很长的说明就变宽（w-max）。
func TestContextMenuWidthFollowsContent(t *testing.T) {
	h, _ := openMenuPage(t, still(), func(p *menuPage) { p.items = []MenuItem{{Label: "删除"}} })
	openMenu(h)
	menu, _ := h.node(gfx.RoleMenu, "")
	if menu.Bounds.Width != menuMinWidth {
		t.Errorf("短标签的面板宽 %g，期望 160", menu.Bounds.Width)
	}
	if menu.Bounds.Height != 2+8+32 {
		t.Errorf("无说明的一项应 32 高：面板高 %g，期望 42", menu.Bounds.Height)
	}
	h2, _ := openMenuPage(t, still(), func(p *menuPage) {
		p.items = []MenuItem{{Label: "删除", Description: strings.Repeat("很长的说明", 12)}}
	})
	openMenu(h2)
	wide, _ := h2.node(gfx.RoleMenu, "")
	if wide.Bounds.Width <= menuMinWidth+100 {
		t.Errorf("长说明的面板宽 %g，应明显超过 160", wide.Bounds.Width)
	}
}

// 打开后第一个未禁用项自动聚焦；程序聚焦时不画聚焦环（鼠标打开不显示环）。
func TestContextMenuFocusesFirstEnabledItem(t *testing.T) {
	h, _ := openMenuPage(t, still(), nil)
	openMenu(h)
	if got := focusedItem(h); got != menuItemCapture {
		t.Errorf("打开后焦点在 %q，期望第一项", got)
	}

	h2, _ := openMenuPage(t, still(), func(p *menuPage) { p.items[0].Disabled = true })
	openMenu(h2)
	if got := focusedItem(h2); got != menuItemBlank {
		t.Errorf("第一项禁用时焦点在 %q，期望第二项", got)
	}
}

// ↑ ↓ 在未禁用项里循环；Home / End 跳到首尾。
func TestContextMenuArrowKeys(t *testing.T) {
	h, _ := openMenuPage(t, still(), nil)
	openMenu(h)
	for i, step := range []struct {
		key  string
		want string
	}{
		{"down", menuItemBlank},
		{"down", menuItemFiles},
		{"down", menuItemCapture}, // 末项再 ↓ 回到首项
		{"up", menuItemFiles},     // 首项再 ↑ 回到末项
		{"up", menuItemBlank},
		{"home", menuItemCapture},
		{"end", menuItemFiles},
	} {
		h.keys(step.key)
		if got := focusedItem(h); got != step.want {
			t.Fatalf("第 %d 步按 %s：焦点在 %q，期望 %q", i+1, step.key, got, step.want)
		}
	}
}

// 方向键跳过禁用项（渲染层 :not(:disabled)）。
func TestContextMenuArrowKeysSkipDisabled(t *testing.T) {
	h, _ := openMenuPage(t, still(), func(p *menuPage) { p.items[1].Disabled = true })
	openMenu(h)
	h.keys("down")
	if got := focusedItem(h); got != menuItemFiles {
		t.Errorf("↓ 应跳过禁用的第二项落到第三项，实际 %q", got)
	}
	h.keys("up")
	if got := focusedItem(h); got != menuItemCapture {
		t.Errorf("↑ 应跳过禁用的第二项回到第一项，实际 %q", got)
	}
}

func TestContextMenuEscapeCloses(t *testing.T) {
	h, p := openMenuPage(t, still(), nil)
	openMenu(h)
	h.keys("escape")
	if p.open || p.closed != 1 || len(p.picked) != 0 {
		t.Errorf("Esc：open=%v closed=%d picked=%v，期望关闭且没有选中任何项", p.open, p.closed, p.picked)
	}
	if h.has(gfx.RoleMenu, "") {
		t.Error("关闭后菜单不应还在")
	}
}

// 点菜单外面：关闭，并且这一下继续交给下面的控件（渲染层的 mousedown 外部点击不拦截）。
func TestContextMenuClickOutsideClosesAndPassesThrough(t *testing.T) {
	h, p := openMenuPage(t, still(), nil)
	openMenu(h)
	h.clickAt(p.behindRect.X+p.behindRect.Width/2, p.behindRect.Y+p.behindRect.Height/2)
	if p.open || p.closed != 1 {
		t.Errorf("点外面：open=%v closed=%d，期望关闭", p.open, p.closed)
	}
	if p.behind != 1 {
		t.Errorf("点外面这一下应当继续落到背后的按钮：behind=%d", p.behind)
	}
}

// 点触发按钮只收起：不传给触发按钮（否则会立刻再展开一次）。
func TestContextMenuClickTriggerOnlyCloses(t *testing.T) {
	h, p := openMenuPage(t, still(), nil)
	openMenu(h)
	if p.opens != 1 {
		t.Fatalf("打开时触发按钮应收到一次点击：opens=%d", p.opens)
	}
	h.clickAt(p.keep.X+p.keep.Width/2, p.keep.Y+p.keep.Height/2) // 再点一次触发按钮（被背景层盖着，按坐标点）
	if p.open || p.closed != 1 || p.opens != 1 {
		t.Errorf("再点触发按钮：open=%v closed=%d opens=%d，期望收起且触发按钮没有收到这一下", p.open, p.closed, p.opens)
	}
}

// 点项：先 OnSelect、再关闭。
func TestContextMenuClickItemSelectsThenCloses(t *testing.T) {
	h, p := openMenuPage(t, still(), nil)
	openMenu(h)
	h.click(menuItemBlank)
	if len(p.picked) != 1 || p.picked[0] != "空白笔记" {
		t.Fatalf("点第二项：picked=%v", p.picked)
	}
	if p.open || p.closed != 1 {
		t.Errorf("选中后应关闭：open=%v closed=%d", p.open, p.closed)
	}
}

// 键盘：焦点在项上按 Enter 等同点击。
func TestContextMenuEnterSelectsFocusedItem(t *testing.T) {
	h, p := openMenuPage(t, still(), nil)
	openMenu(h)
	h.keys("down")
	h.keys("enter")
	if len(p.picked) != 1 || p.picked[0] != "空白笔记" || p.open {
		t.Errorf("↓ Enter：picked=%v open=%v，期望选中第二项并关闭", p.picked, p.open)
	}
}

// 禁用项：点了没有反应、菜单不关闭（pointer-events-none）；无障碍里是禁用的。
func TestContextMenuDisabledItemIgnoresClick(t *testing.T) {
	h, p := openMenuPage(t, still(), func(p *menuPage) { p.items[1].Disabled = true })
	openMenu(h)
	n, ok := h.node(gfx.RoleMenuItem, menuItemBlank)
	if !ok || !n.Disabled {
		t.Fatalf("禁用项应在无障碍树里标为禁用：存在=%v disabled=%v", ok, n.Disabled)
	}
	h.clickAt(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
	if len(p.picked) != 0 {
		t.Errorf("点禁用项不应触发 OnSelect：%v", p.picked)
	}
}

// 面板是不透明的：点在面板边距（项之外的 py-1 与边框）上既不选项、也不关闭、更不落到下面。
func TestContextMenuPanelPaddingIsOpaque(t *testing.T) {
	h, p := openMenuPage(t, still(), nil)
	openMenu(h)
	menu, _ := h.node(gfx.RoleMenu, "")
	h.clickAt(menu.Bounds.X+menu.Bounds.Width/2, menu.Bounds.Y+2) // 上边距的 py-1 内，不在任何项上
	if !p.open || p.closed != 0 || len(p.picked) != 0 {
		t.Errorf("点面板边距：open=%v closed=%d picked=%v，期望什么也不发生", p.open, p.closed, p.picked)
	}
}

// 窗口尺寸变化即关闭（渲染层 window resize → onClose）。
func TestContextMenuClosesOnResize(t *testing.T) {
	h, p := openMenuPage(t, still(), nil)
	openMenu(h)
	if err := h.session.Resize(760, 560); err != nil {
		t.Fatalf("改窗口尺寸失败：%v", err)
	}
	h.frames(3)
	if p.open || p.closed != 1 {
		t.Errorf("改尺寸后：open=%v closed=%d，期望关闭", p.open, p.closed)
	}
}

// 越界时的挪法与渲染层一致：越过右（下）缘就挪到「视口宽（高）− 面板宽（高）− 8」，不越界就保持锚点。
func TestMenuRectClampsToViewport(t *testing.T) {
	cases := []struct {
		name       string
		x, y, w, h float32
		want       gfx.Rect
	}{
		{"不越界", 100, 100, 280, 154, gfx.Rect{X: 100, Y: 100, Width: 280, Height: 154}},
		{"右缘越界", 700, 100, 280, 154, gfx.Rect{X: 800 - 280 - 8, Y: 100, Width: 280, Height: 154}},
		{"下缘越界", 100, 500, 280, 154, gfx.Rect{X: 100, Y: 600 - 154 - 8, Width: 280, Height: 154}},
		{"恰好贴边", 520, 446, 280, 154, gfx.Rect{X: 520, Y: 446, Width: 280, Height: 154}},
	}
	for _, c := range cases {
		if got := menuRect(c.x, c.y, c.w, c.h, 800, 600); got != c.want {
			t.Errorf("%s：得到 %+v，期望 %+v", c.name, got, c.want)
		}
	}
}

// pixelAt 取窗口截图里某个逻辑像素的颜色（缩放 1）。
func pixelAt(h *harness, x, y float32) [4]uint8 {
	h.t.Helper()
	img, err := h.session.Image()
	if err != nil {
		h.t.Fatalf("截图失败：%v", err)
	}
	r, g, b, a := img.At(int(x), int(y)).RGBA()
	return [4]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

// 聚焦环（全局的 button:focus-visible ring-2）只在键盘移动焦点之后出现：鼠标点开菜单时第一项虽有焦点却没有环，
// 按 ↓ 之后第二项带环，环画在项外面、压住面板左边框（落在面板左缘外 1px 的像素上）。
func TestContextMenuFocusRingOnlyAfterKeyboard(t *testing.T) {
	h, _ := openMenuPage(t, still(), nil)
	openMenu(h)
	menu, first, second, _ := menuNodes(h)
	probe := func(item nodeLite) [4]uint8 { return pixelAt(h, menu.Bounds.X-1, item.Bounds.Y+item.Bounds.Height/2) }
	beforeFirst, beforeSecond := probe(first), probe(second)
	if beforeFirst != beforeSecond {
		t.Fatalf("鼠标点开后两处像素应当相同（都没有环）：%v 与 %v", beforeFirst, beforeSecond)
	}
	h.keys("down")
	if got := probe(second); got == beforeSecond {
		t.Errorf("↓ 之后第二项应画出聚焦环，像素仍是 %v", got)
	}
	if got := probe(first); got != beforeFirst {
		t.Errorf("第一项失去焦点后不应有环：像素 %v，之前 %v", got, beforeFirst)
	}
}
