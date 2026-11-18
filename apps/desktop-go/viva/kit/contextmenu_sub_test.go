package kit

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/internal/frametime"
)

// 子菜单的 L4 测试（台账 B-023 (1)）：事实全部来自 React 参照里侧栏标签行菜单的探针
// （scripts/parity/tools/probe-submenu.mjs → testdata/submenu-react.json），几何、键盘序列与悬停行为逐项对照。
// 关闭延迟 140 ms 取自渲染层源码（探针没有量到毫秒数，见探针头注释），用虚拟时钟断言。

type reactItem struct {
	Label    string     `json:"label"`
	Rect     [4]float32 `json:"rect"`
	Haspopup *string    `json:"haspopup"`
	Expanded *string    `json:"expanded"`
}

type reactSubCase struct {
	ID       string      `json:"id"`
	Font     string      `json:"font"`
	Anchor   [2]float32  `json:"anchor"`
	Viewport [2]float32  `json:"viewport"`
	Root     [4]float32  `json:"root"`
	Items    []reactItem `json:"items"`
	Sub      struct {
		Parent   string      `json:"parent"`
		Left     bool        `json:"left"`
		Wrapper  [4]float32  `json:"wrapper"`
		Panel    [4]float32  `json:"panel"`
		Children []reactItem `json:"children"`
	} `json:"sub"`
}

type reactKeyStep struct {
	Key      string    `json:"key"`
	Label    *string   `json:"label"`
	Index    *int      `json:"index"`
	Total    *int      `json:"total"`
	Expanded []*string `json:"expanded"`
}

type reactSubData struct {
	Cases    []reactSubCase             `json:"cases"`
	Keyboard map[string]json.RawMessage `json:"keyboard"`
}

func loadSubmenuReact(t *testing.T) *reactSubData {
	t.Helper()
	raw, err := os.ReadFile("testdata/submenu-react.json")
	if err != nil {
		t.Fatalf("读取 React 子菜单事实失败：%v", err)
	}
	var data reactSubData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("解析 React 子菜单事实失败：%v", err)
	}
	return &data
}

func (d *reactSubData) sequence(t *testing.T, name string) []reactKeyStep {
	t.Helper()
	var steps []reactKeyStep
	if err := json.Unmarshal(d.Keyboard[name], &steps); err != nil {
		t.Fatalf("解析键盘序列 %s 失败：%v", name, err)
	}
	return steps
}

var tagColorNames = []string{"红", "橙", "琥珀", "绿", "青", "蓝", "靛", "紫", "粉", "灰"}

// tagMenuItems 是侧栏标签行菜单的同形菜单：重命名 / 颜色（十个子项）/ 删除标签（危险项）。
func tagMenuItems(p *menuPage) []MenuItem {
	pick := func(name string) func() { return func() { p.picked = append(p.picked, name) } }
	kids := make([]MenuItem, len(tagColorNames))
	for i, name := range tagColorNames {
		kids[i] = MenuItem{Label: name, OnSelect: pick("颜色:" + name)}
	}
	return []MenuItem{
		{Label: "重命名", Icon: icons.Pencil, OnSelect: pick("重命名")},
		{Label: "颜色", Icon: icons.Tag, Children: kids, OnSelect: pick("颜色(父项)")},
		{Label: "删除标签", Icon: icons.CircleX, Destructive: true, OnSelect: pick("删除标签")},
	}
}

// openTagMenu 在 vw×vh 的窗口里、锚点处打开标签行菜单（第一帧就开着）。
func openTagMenu(t *testing.T, font string, vw, vh int, anchor gfx.Point, configure func(p *menuPage)) (*harness, *menuPage) {
	t.Helper()
	spec := still()
	spec.FontSize = font
	p := &menuPage{at: anchor, open: true}
	p.items = tagMenuItems(p)
	if configure != nil {
		configure(p)
	}
	return openPage(t, spec, vw, vh, p.view), p
}

// menuItemNodes 按可访问树的顺序列出菜单项（渲染层的 DOM 顺序：子菜单的子项紧跟在父项后面）。
func menuItemNodes(h *harness) []app.AccessNode {
	var out []app.AccessNode
	for _, n := range h.session.Access() {
		if n.Role == gfx.RoleMenuItem {
			out = append(out, n)
		}
	}
	return out
}

// focusedMenuItem 返回键盘所在菜单项在 menuItemNodes 里的序号、标签与总数；没有焦点时序号为 -1。
func focusedMenuItem(h *harness) (index int, label string, total int) {
	nodes := menuItemNodes(h)
	for i, n := range nodes {
		if n.Focused {
			return i, n.Label, len(nodes)
		}
	}
	return -1, "", len(nodes)
}

func nodeByLabel(h *harness, label string) (app.AccessNode, bool) {
	for _, n := range menuItemNodes(h) {
		if n.Label == label {
			return n, true
		}
	}
	return app.AccessNode{}, false
}

// hoverItem 把指针移到某个菜单项的中心。
func hoverItem(h *harness, label string) {
	h.t.Helper()
	n, ok := nodeByLabel(h, label)
	if !ok {
		h.t.Fatalf("没有菜单项「%s」", label)
	}
	x, y := centerOf(n.Bounds)
	h.moveTo(x, y)
}

// submenuOpen 报告子菜单是否展开（父项的 aria-expanded）。
func submenuOpen(h *harness) bool {
	n, ok := nodeByLabel(h, "颜色")
	return ok && n.Expanded
}

func rectNear(got gfx.Rect, want [4]float32, tol float64) bool {
	for i, g := range [4]float32{got.X, got.Y, got.Width, got.Height} {
		if math.Abs(float64(g-want[i])) > tol {
			return false
		}
	}
	return true
}

// 几何：八个案例（三个字号档、向右 / 向左展开、顶部上移、两者并用）里，根面板、根项、子面板与悬停桥、各子项的外框
// 与渲染层的实测一致。方向与竖直偏移（256 / 280 / 8 / −4 这些写死的估算）都由这些案例覆盖。
func TestSubmenuGeometryMatchesReact(t *testing.T) {
	data := loadSubmenuReact(t)
	const tol = 0.05
	for _, tc := range data.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			h, p := openTagMenu(t, tc.Font, int(tc.Viewport[0]), int(tc.Viewport[1]), gfx.Point{X: tc.Anchor[0], Y: tc.Anchor[1]}, nil)
			hoverItem(h, tc.Sub.Parent)
			h.frames(2)

			menu, ok := h.node(gfx.RoleMenu, "")
			if !ok || !rectNear(menu.Bounds, tc.Root, tol) {
				t.Errorf("根面板 %v，期望 %v", menu.Bounds, tc.Root)
			}
			for _, want := range tc.Items {
				n, ok := nodeByLabel(h, want.Label)
				if !ok || !rectNear(n.Bounds, want.Rect, tol) {
					t.Errorf("根项「%s」%v，期望 %v", want.Label, n.Bounds, want.Rect)
				}
			}
			if !submenuOpen(h) {
				t.Fatal("悬停父项后子菜单没有展开")
			}
			for _, want := range tc.Sub.Children {
				n, ok := nodeByLabel(h, want.Label)
				if !ok || !rectNear(n.Bounds, want.Rect, tol) {
					t.Errorf("子项「%s」%v，期望 %v", want.Label, n.Bounds, want.Rect)
				}
			}
			if !rectNear(p.st.subRect, tc.Sub.Panel, tol) {
				t.Errorf("子面板 %v，期望 %v", p.st.subRect, tc.Sub.Panel)
			}
			if p.st.sub.left != tc.Sub.Left {
				t.Errorf("向左展开 = %v，期望 %v", p.st.sub.left, tc.Sub.Left)
			}
			// 悬停桥外层 = 子面板与向父项一侧伸出的那一条（pl-2 / pr-2）的并集。
			strip := h.look.bridgeStrip(p.st.subRect, p.st.sub.left)
			left := min(p.st.subRect.X, strip.X)
			right := max(p.st.subRect.X+p.st.subRect.Width, strip.X+strip.Width)
			wrapper := gfx.Rect{X: left, Y: p.st.subRect.Y, Width: right - left, Height: p.st.subRect.Height}
			if !rectNear(wrapper, tc.Sub.Wrapper, tol) {
				t.Errorf("子菜单外层 %v，期望 %v", wrapper, tc.Sub.Wrapper)
			}
		})
	}
}

// 方向与竖直偏移的公式：越过视口右缘（256 估算）翻到左边；底部不够（280 估算）上移；顶部不让出视口（8 − 行顶）。
func TestSubmenuPlacementFormula(t *testing.T) {
	item := func(x, y float32) gfx.Rect { return gfx.Rect{X: x, Y: y, Width: 158, Height: 32} }
	for _, tc := range []struct {
		name     string
		item     gfx.Rect
		vw, vh   float32
		wantLeft bool
		wantTop  float32
	}{
		{"中部向右", item(151, 337), 1280, 800, false, -4},
		{"右缘刚好放得下", item(1280-8-256-158, 337), 1280, 800, false, -4},
		{"右缘差 1px 翻到左边", item(1280-8-256-158+1, 337), 1280, 800, true, -4},
		{"底部上移", item(151, 723), 1280, 800, false, 800 - 723 - 280 - 8},
		{"底部刚好不用上移", item(151, 800-280-8+4), 1280, 800, false, -4},
		{"顶部不出视口", item(151, 5), 1280, 800, false, 3},
		{"右下两者并用", item(1101, 723), 1280, 800, true, -211},
	} {
		left, top := submenuPlacement(tc.item, tc.vw, tc.vh)
		if left != tc.wantLeft || top != tc.wantTop {
			t.Errorf("%s：left=%v top=%g，期望 left=%v top=%g", tc.name, left, top, tc.wantLeft, tc.wantTop)
		}
	}
}

// 键盘：三段探针序列（→ 展开并聚焦第一个子项、← 回到父项、↑ ↓ Home End 在「按 DOM 顺序展开的全部项」里循环）逐步对照。
func TestSubmenuKeyboardMatchesReact(t *testing.T) {
	data := loadSubmenuReact(t)
	keyName := map[string]string{
		"ArrowDown": "down", "ArrowUp": "up", "ArrowLeft": "left", "ArrowRight": "right", "Home": "home", "End": "end",
	}
	check := func(t *testing.T, h *harness, step reactKeyStep, at string) {
		t.Helper()
		index, label, total := focusedMenuItem(h)
		if step.Label == nil || label != *step.Label || index != *step.Index || total != *step.Total {
			t.Fatalf("%s：焦点 %q（第 %d 项，共 %d），期望 %q（第 %d 项，共 %d）", at, label, index, total, *step.Label, *step.Index, *step.Total)
		}
		n, _ := nodeByLabel(h, "颜色")
		wantOpen := step.Expanded[1] != nil && *step.Expanded[1] == "true"
		if n.Expanded != wantOpen {
			t.Fatalf("%s：「颜色」展开 = %v，期望 %v", at, n.Expanded, wantOpen)
		}
	}
	run := func(t *testing.T, name string, prepare func(h *harness)) {
		h, _ := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
		if prepare != nil {
			prepare(h)
		}
		steps := data.sequence(t, name)
		for i, step := range steps {
			if step.Key != "(打开)" {
				key, ok := keyName[step.Key]
				if !ok {
					t.Fatalf("未知的按键 %q", step.Key)
				}
				h.keys(key)
			}
			check(t, h, step, fmt.Sprintf("序列 %s 第 %d 步（%s）", name, i+1, step.Key))
		}
	}
	// A：从打开状态用键盘一路走（含 → 展开、End、Home、← 在根项上没有作用、→ 在子项上没有作用）。
	t.Run("A", func(t *testing.T) { run(t, "sequenceA", nil) })
	// B：子菜单由悬停打开、键盘仍在第一个根项上（序列里第一步已是 ↓）。
	t.Run("B", func(t *testing.T) {
		run(t, "sequenceB", func(h *harness) {
			hoverItem(h, "颜色")
			h.frames(2)
			// 悬停打开子菜单不移动键盘：焦点仍在第一项。
			if i, label, _ := focusedMenuItem(h); i != 0 || label != "重命名" {
				t.Fatalf("悬停打开子菜单后焦点在 %q（第 %d 项），期望仍在「重命名」", label, i)
			}
		})
	})
	// C：→ 在没有子菜单的项上没有作用；← 关闭子菜单并聚焦父项；再 → 重新打开。
	t.Run("C", func(t *testing.T) { run(t, "sequenceC", nil) })
}

// Esc 在子菜单里关闭整个菜单（渲染层 setOpenSubmenu(null) 加 onClose）。
func TestSubmenuEscapeClosesTheWholeMenu(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	h.keys("down")
	h.keys("right")
	if i, _, _ := focusedMenuItem(h); i != 2 {
		t.Fatalf("→ 之后焦点在第 %d 项，期望第一个子项（第 2 项）", i)
	}
	h.keys("escape")
	if p.open || p.closed != 1 || h.has(gfx.RoleMenu, "") {
		t.Errorf("Esc：open=%v closed=%d，菜单是否还在 %v；期望整个菜单关闭", p.open, p.closed, h.has(gfx.RoleMenu, ""))
	}
}

// 点子项：先调子项的 OnSelect、再关闭整个菜单；父项自己的 OnSelect 不会被调用。
func TestSubmenuClickChildSelectsThenCloses(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	hoverItem(h, "颜色")
	n, _ := nodeByLabel(h, "琥珀")
	x, y := centerOf(n.Bounds)
	h.moveTo(x, y)
	h.clickAt(x, y)
	if len(p.picked) != 1 || p.picked[0] != "颜色:琥珀" || p.closed != 1 || p.open {
		t.Errorf("点子项后 picked=%v closed=%d open=%v，期望只选中「颜色:琥珀」并关闭", p.picked, p.closed, p.open)
	}
}

// 点父项本身没有反应：不选中、不关闭菜单（渲染层 onClick 里 hasChildren 直接 return）；Enter 同理（它就是按钮的点击）。
func TestSubmenuClickParentDoesNothing(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	n, _ := nodeByLabel(h, "颜色")
	x, y := centerOf(n.Bounds)
	h.moveTo(x, y)
	h.clickAt(x, y)
	if len(p.picked) != 0 || p.closed != 0 || !p.open {
		t.Errorf("点父项后 picked=%v closed=%d open=%v，期望什么都没发生", p.picked, p.closed, p.open)
	}
	if !submenuOpen(h) {
		t.Error("点父项不应收起子菜单（鼠标仍在父项上，没有离开事件）")
	}
	h.keys("enter")
	if len(p.picked) != 0 || p.closed != 0 || !p.open {
		t.Errorf("Enter 落在父项上后 picked=%v closed=%d open=%v，期望什么都没发生", p.picked, p.closed, p.open)
	}
}

// 悬停打开：进入父项即展开，没有延迟；父项带 aria-haspopup / aria-expanded。
func TestSubmenuOpensOnHoverAndExposesExpandable(t *testing.T) {
	h, _ := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	parent, _ := nodeByLabel(h, "颜色")
	if !parent.Expandable || parent.Expanded {
		t.Fatalf("展开前父项 Expandable=%v Expanded=%v，期望可展开且未展开", parent.Expandable, parent.Expanded)
	}
	if rename, _ := nodeByLabel(h, "重命名"); rename.Expandable {
		t.Error("没有子菜单的项不应标 Expandable")
	}
	if _, ok := nodeByLabel(h, "红"); ok {
		t.Fatal("没有悬停时不应有子项")
	}
	hoverItem(h, "颜色")
	parent, _ = nodeByLabel(h, "颜色")
	if !parent.Expanded {
		t.Error("悬停父项后应当展开")
	}
	if _, ok := nodeByLabel(h, "红"); !ok {
		t.Error("展开后应有子项")
	}
}

// 进入别的根项：立刻关闭（渲染层 onMouseEnter 里 setOpenSubmenu(null)），不等 140 ms。
func TestSubmenuClosesAtOnceOnEnteringAnotherRootItem(t *testing.T) {
	for _, target := range []string{"删除标签", "重命名"} {
		h, _ := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
		hoverItem(h, "颜色")
		if !submenuOpen(h) {
			t.Fatal("悬停父项后没有展开")
		}
		hoverItem(h, target)
		if submenuOpen(h) {
			t.Errorf("移到「%s」后子菜单应当立刻关闭", target)
		}
	}
}

// 悬停桥：指针在父项与子面板之间的缝里（子菜单外层的 pl-2）时保持展开，久留也不关；
// 桥压在下面那个根项的最右 4px 上：指针落在那里仍算在子菜单里。
func TestSubmenuHoverBridgeKeepsItOpen(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	hoverItem(h, "颜色")
	parent, _ := nodeByLabel(h, "颜色")
	_, cy := centerOf(parent.Bounds)
	gapX := p.st.subRect.X - 2 // 子面板左缘之前 2px：桥里
	h.moveTo(gapX, cy)
	h.advance(500 * time.Millisecond)
	h.frames(2)
	if !submenuOpen(h) {
		t.Error("指针在悬停桥里，子菜单不应关闭")
	}
	del, _ := nodeByLabel(h, "删除标签")
	_, delY := centerOf(del.Bounds)
	h.moveTo(p.st.subRect.X-h.look.Space(2)+1, delY)
	h.advance(500 * time.Millisecond)
	h.frames(2)
	if !submenuOpen(h) {
		t.Error("指针落在压住「删除标签」右缘的悬停桥里，子菜单应当保持（桥高于根项）")
	}
	// 同一位置上「删除标签」的悬停底色不应亮起（它被桥盖住了）。
	if rowTinted(h, "删除标签") {
		t.Error("悬停桥盖住的根项不应显示悬停底色")
	}
}

// rowTinted 报告菜单项左侧空白处的像素是否异于面板底色（悬停 / 聚焦时才会有底色）。
func rowTinted(h *harness, label string) bool {
	h.t.Helper()
	menu, _ := h.node(gfx.RoleMenu, "")
	row, ok := nodeByLabel(h, label)
	if !ok {
		h.t.Fatalf("没有菜单项「%s」", label)
	}
	plain := pixelAt(h, menu.Bounds.X+menu.Bounds.Width-14, menu.Bounds.Y+2) // 上内边距里：纯面板底色
	return pixelAt(h, row.Bounds.X+3, row.Bounds.Y+row.Bounds.Height-3) != plain
}

// 进入禁用的根项：渲染层的禁用按钮 pointer-events-none，指针落在它外层的 div.relative 上，照样触发 mouseenter，子菜单立刻关闭。
func TestSubmenuClosesOnEnteringDisabledRootItem(t *testing.T) {
	h, _ := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, func(p *menuPage) { p.items[2].Disabled = true })
	hoverItem(h, "颜色")
	if !submenuOpen(h) {
		t.Fatal("悬停父项后没有展开")
	}
	hoverItem(h, "删除标签")
	if submenuOpen(h) {
		t.Error("进入禁用的根项后子菜单应当立刻关闭")
	}
}

// 指针停在子面板里（子项上、面板的上下内边距里）：久留也不关闭；在内边距里点一下也不关菜单（它是菜单的一部分）。
func TestSubmenuStaysOpenWhilePointerIsInsideThePanel(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	hoverItem(h, "颜色")
	panel := p.st.subRect
	kid, _ := nodeByLabel(h, "绿")
	kx, ky := centerOf(kid.Bounds)
	for name, at := range map[string]gfx.Point{
		"子项上":      {X: kx, Y: ky},
		"面板上内边距里":  {X: panel.X + panel.Width/2, Y: panel.Y + 2},
		"面板下内边距里":  {X: panel.X + panel.Width/2, Y: panel.Y + panel.Height - 2},
		"面板左缘的边框上": {X: panel.X + 0.5, Y: ky},
	} {
		h.moveTo(at.X, at.Y)
		h.advance(500 * time.Millisecond)
		h.frames(2)
		if !submenuOpen(h) {
			t.Errorf("指针在%s，子菜单不应关闭", name)
		}
	}
	h.clickAt(panel.X+panel.Width/2, panel.Y+2)
	if !p.open || p.closed != 0 || len(p.picked) != 0 {
		t.Errorf("点在子面板内边距里后 open=%v closed=%d picked=%v，期望什么都没发生", p.open, p.closed, p.picked)
	}
}

// 从子面板出去又在 140 ms 之内回到子面板：排队的关闭被取消。
func TestSubmenuReEnteringThePanelCancelsTheClose(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	hoverItem(h, "颜色")
	inside := gfx.Point{X: p.st.subRect.X + 40, Y: p.st.subRect.Y + 40}
	h.moveTo(inside.X, inside.Y)
	h.moveTo(900, 700) // 出去：子面板、父项都不在了，排 140 ms
	h.advance(100 * time.Millisecond)
	h.moveTo(inside.X, inside.Y) // 回来
	h.advance(500 * time.Millisecond)
	h.frames(2)
	if !submenuOpen(h) {
		t.Error("140 ms 之内回到子面板，排队的关闭应当被取消")
	}
}

// 两个父项：悬停谁就展开谁的子菜单，同一时刻只有一个；从一个父项直接移到另一个，前一个立刻让位。
func TestSubmenuTwoParentsExpandOneAtATime(t *testing.T) {
	h, _ := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, func(p *menuPage) {
		p.items = []MenuItem{
			{Label: "甲", Icon: icons.Tag, Children: []MenuItem{{Label: "甲一"}, {Label: "甲二"}}},
			{Label: "乙", Icon: icons.Tag, Children: []MenuItem{{Label: "乙一"}}},
			{Label: "丙", Icon: icons.Pencil},
		}
	})
	expanded := func(label string) bool {
		n, ok := nodeByLabel(h, label)
		return ok && n.Expanded
	}
	hoverItem(h, "甲")
	if !expanded("甲") || expanded("乙") {
		t.Fatalf("悬停甲：甲展开 = %v，乙展开 = %v，期望只有甲", expanded("甲"), expanded("乙"))
	}
	if _, ok := nodeByLabel(h, "甲二"); !ok {
		t.Error("甲的子项应当出现")
	}
	hoverItem(h, "乙")
	if expanded("甲") || !expanded("乙") {
		t.Fatalf("移到乙：甲展开 = %v，乙展开 = %v，期望只有乙", expanded("甲"), expanded("乙"))
	}
	if _, ok := nodeByLabel(h, "甲一"); ok {
		t.Error("乙展开后不应留着甲的子项")
	}
	if _, ok := nodeByLabel(h, "乙一"); !ok {
		t.Error("乙的子项应当出现")
	}
	// 键盘：焦点在乙上按 → 展开乙的子菜单，焦点落到「乙一」。
	h.keys("down")
	h.keys("down")
	h.keys("right")
	if _, label, _ := focusedMenuItem(h); label != "乙一" {
		t.Errorf("焦点在乙上按 → 后焦点在 %q，期望「乙一」", label)
	}
}

// 带子菜单的父项行尾多一个 ChevronRight：ml-2 加 gap-2 加 16px，面板宽取最宽一项，所以宽标签的父项把面板撑宽 32px。
func TestSubmenuParentRowIsWiderByTheChevron(t *testing.T) {
	const long = "一二三四五六七八九十一二三四五六七八九十"
	width := func(children []MenuItem) float32 {
		h, _ := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, func(p *menuPage) {
			p.items = []MenuItem{{Label: long, Icon: icons.Tag, Children: children}, {Label: "短", Icon: icons.Pencil}}
		})
		menu, _ := h.node(gfx.RoleMenu, "")
		return menu.Bounds.Width
	}
	plain, withChildren := width(nil), width([]MenuItem{{Label: "子"}})
	if plain <= 160 {
		t.Fatalf("测试前提：宽标签的菜单宽应当大于最小宽 160，得到 %g", plain)
	}
	if got := withChildren - plain; got != 32 {
		t.Errorf("父项比普通项宽 %g，期望 32（gap-2 8 + ml-2 8 + 图标 16）", got)
	}
}

// 命中区身份每帧重建：不会一帧一帧地累积（累积会让内存无界增长，也会让按序号取身份取错）。
func TestSubmenuHitIdentitiesDoNotAccumulate(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	hoverItem(h, "颜色")
	h.frames(30)
	if got := len(p.st.hits.rows); got != 3 {
		t.Errorf("根项命中区 %d 个，期望 3", got)
	}
	if got := len(p.st.hits.wraps); got != 3 {
		t.Errorf("根项外层命中区 %d 个，期望 3", got)
	}
	if got := len(p.st.hits.kids); got != len(tagColorNames) {
		t.Errorf("子项命中区 %d 个，期望 %d", got, len(tagColorNames))
	}
}

// reactCloseDelay 是渲染层 scheduleSubmenuClose 的 140 ms（源码常量）。测试里写字面量，不引用实现里的常量，
// 否则改了实现的常量，测试会跟着一起改。
const reactCloseDelay = 140 * time.Millisecond

// pointerTo 只移动指针、只出一帧：窗口里一次移动就是一帧，之后没人再喊重绘——定时关闭要靠实现自己约好下一帧。
// （moveTo 连出三帧，会替实现补上这个约定。）
func (h *harness) pointerTo(x, y float32) {
	h.t.Helper()
	if err := h.session.Move(x, y); err != nil {
		h.t.Fatalf("移动指针到 (%g,%g) 失败：%v", x, y, err)
	}
}

// 离开父项与子菜单：140 ms 后关闭（差 1 ms 都还开着）；回来就取消。
// 推进时钟之后不手动出帧：虚拟时钟只在实现约好的帧上重绘，与窗口一致。
func TestSubmenuClosesAfterLeaveDelay(t *testing.T) {
	h, _ := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	hoverItem(h, "颜色")
	h.pointerTo(700, 700) // 离开：远在菜单之外
	if !submenuOpen(h) {
		t.Fatal("刚离开子菜单就关了，应当排 140 ms")
	}
	h.advance(reactCloseDelay - time.Millisecond)
	if !submenuOpen(h) {
		t.Error("离开 139 ms 时子菜单应当还开着")
	}
	h.advance(2 * time.Millisecond)
	if submenuOpen(h) {
		t.Error("离开 141 ms 时子菜单应当已关闭（要有一帧在 140 ms 处重绘）")
	}

	// 回来：在 140 ms 之内重新进入父项，取消排队中的关闭。
	hoverItem(h, "颜色")
	h.pointerTo(700, 700)
	h.advance(100 * time.Millisecond)
	hoverItem(h, "颜色")
	h.advance(500 * time.Millisecond)
	h.frames(2)
	if !submenuOpen(h) {
		t.Error("140 ms 之内回到父项，排队的关闭应当被取消")
	}
}

// 排队期间又有别的帧（指针在窗口外挪了一下）：到点关闭的时刻不受影响，仍是离开后 140 ms。
// （Viva 的帧请求取最早的、到点才作废，中间的帧不会用掉它；实现里「未到点就重新约一帧」是给真窗口的早醒兜底，
// 虚拟时钟下不可观察，见变异记录 S47。）
func TestSubmenuCloseSurvivesAnInterveningFrame(t *testing.T) {
	h, _ := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	hoverItem(h, "颜色")
	h.pointerTo(700, 700)
	h.advance(50 * time.Millisecond)
	h.pointerTo(710, 700) // 仍在窗口外：不产生进出，只多一帧
	h.advance(reactCloseDelay - 50*time.Millisecond - time.Millisecond)
	if !submenuOpen(h) {
		t.Fatal("离开 139 ms 时子菜单应当还开着")
	}
	h.advance(2 * time.Millisecond)
	if submenuOpen(h) {
		t.Error("中间多了一帧，离开 141 ms 时子菜单仍应关闭（到点的那一帧要重新约好）")
	}
}

// 子面板里的来回：从子面板回到父项按钮（排了关闭），140 ms 之内又进子面板——进入子面板取消关闭。
// 这条路上行分不变（父项还是同一个），取消只能靠「进入子菜单」。
func TestSubmenuBouncingBackIntoThePanelCancelsTheClose(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	hoverItem(h, "颜色")
	inside := gfx.Point{X: p.st.subRect.X + 30, Y: p.st.subRect.Y + 30}
	h.moveTo(inside.X, inside.Y)
	hoverItem(h, "颜色") // 回到父项按钮：排 140 ms
	h.advance(100 * time.Millisecond)
	h.moveTo(inside.X, inside.Y) // 再进子面板
	h.advance(500 * time.Millisecond)
	h.frames(2)
	if !submenuOpen(h) {
		t.Error("回到父项后 140 ms 之内又进入子面板，排队的关闭应当被取消")
	}
}

// 向左展开时悬停桥伸在面板的右侧（pr-2）：指针停在面板与根面板之间的缝里，子菜单不关。
func TestSubmenuBridgeWorksWhenOpeningToTheLeft(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 1100, Y: 300}, nil)
	hoverItem(h, "颜色")
	if !p.st.sub.left {
		t.Fatal("测试前提：靠右的菜单应当向左展开")
	}
	parent, _ := nodeByLabel(h, "颜色")
	_, cy := centerOf(parent.Bounds)
	strip := h.look.bridgeStrip(p.st.subRect, true)
	h.moveTo(strip.X+1, cy) // 面板右缘外 1px：在缝里，不在根面板里
	menu, _ := h.node(gfx.RoleMenu, "")
	if strip.X+1 >= menu.Bounds.X {
		t.Fatalf("测试前提：这一点应当落在根面板之外，实际 x=%g、根面板左缘 %g", strip.X+1, menu.Bounds.X)
	}
	h.advance(500 * time.Millisecond)
	h.frames(2)
	if !submenuOpen(h) {
		t.Error("指针在向左展开的悬停桥里，子菜单不应关闭")
	}
}

// 从子面板回到父项按钮：离开了悬停桥、没有离开 div.relative，所以排了关闭却没人取消——子菜单随后关闭
// （渲染层的既有行为，探针里 hover.rootToRoot.afterParentClick 的真正原因）。
func TestSubmenuBackOntoTheParentButtonClosesLater(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	hoverItem(h, "颜色")
	h.moveTo(p.st.subRect.X+30, p.st.subRect.Y+30)
	if !submenuOpen(h) {
		t.Fatal("指针进入子面板后子菜单应当开着")
	}
	hoverItem(h, "颜色") // 回到父项按钮
	if !submenuOpen(h) {
		t.Fatal("回到父项的那一刻不应立刻关闭")
	}
	h.advance(reactCloseDelay + 10*time.Millisecond)
	h.frames(2)
	if submenuOpen(h) {
		t.Error("回到父项按钮 140 ms 后子菜单应当关闭")
	}
}

// 选项：禁用的子项不响应点击、方向键跳过它；带子菜单的父项禁用时，键盘到不了它。
func TestSubmenuDisabledChildren(t *testing.T) {
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, func(p *menuPage) {
		p.items[1].Children[0].Disabled = true // 红
		p.items[1].Children[1].Disabled = true // 橙
	})
	h.keys("down")
	h.keys("right")
	if _, label, _ := focusedMenuItem(h); label != "琥珀" {
		t.Errorf("→ 展开后焦点在 %q，期望第一个未禁用的子项「琥珀」", label)
	}
	h.keys("up")
	if _, label, _ := focusedMenuItem(h); label != "颜色" {
		t.Errorf("在第一个可用子项上按 ↑ 应回到父项（跳过禁用的红、橙），实际 %q", label)
	}
	n, _ := nodeByLabel(h, "红")
	x, y := centerOf(n.Bounds)
	h.clickAt(x, y)
	if len(p.picked) != 0 || p.closed != 0 {
		t.Errorf("点禁用的子项后 picked=%v closed=%d，期望没有反应", p.picked, p.closed)
	}
}

// 父项禁用：键盘到不了它，→ 因此打不开子菜单。
func TestSubmenuDisabledParentCannotBeOpenedByKeyboard(t *testing.T) {
	h, _ := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, func(p *menuPage) { p.items[1].Disabled = true })
	h.keys("down")
	if _, label, _ := focusedMenuItem(h); label != "删除标签" {
		t.Fatalf("↓ 应跳过禁用的父项，焦点在 %q", label)
	}
	h.keys("right")
	if submenuOpen(h) {
		t.Error("焦点不在父项上，→ 不应展开子菜单")
	}
}

// 没有任何子菜单的菜单走原来的路径：不包外层、没有悬停桥，创建方式菜单的行为不变（其余用例在 contextmenu_test.go）。
func TestMenuWithoutChildrenKeepsPlainLayout(t *testing.T) {
	h, p := openMenuPage(t, still(), nil)
	openMenu(h)
	if !p.st.hits.subWrap.IsZero() || len(p.st.wraps) != 0 {
		t.Errorf("没有子菜单的菜单不应登记外层与版面：wraps=%d", len(p.st.wraps))
	}
}

// TestSubmenuFrameBudget 读出子菜单展开时的帧耗时（模块完成定义第 4 条：稳态帧 ≤ 2 ms）：
// 稳态帧，以及指针在十个子项之间来回移动的每一步（含出帧）。无头会话的 Frame() 只含布局与记录；
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/kit -run TestSubmenuFrameBudget -v -count=1。读数记进台账 B-023。
func TestSubmenuFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	h, p := openTagMenu(t, "medium", 1280, 800, gfx.Point{X: 150, Y: 300}, nil)
	hoverItem(h, "颜色")
	if !submenuOpen(h) {
		t.Fatal("悬停父项后没有展开")
	}
	idle, err := frametime.Measure(300, func(int) error { return h.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("子菜单展开 稳态帧（布局+记录）：%s", idle)

	var ys []float32
	for _, name := range tagColorNames {
		n, _ := nodeByLabel(h, name)
		_, y := centerOf(n.Bounds)
		ys = append(ys, y)
	}
	x := p.st.subRect.X + p.st.subRect.Width/2
	move, err := frametime.Measure(200, func(i int) error {
		return h.session.Move(x, ys[i%len(ys)])
	})
	if err != nil {
		t.Fatalf("移动指针失败：%v", err)
	}
	t.Logf("子菜单展开 指针在子项间移动一步（移动+出帧）：%s", move)
}
