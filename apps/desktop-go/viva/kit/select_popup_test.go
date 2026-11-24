package kit

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 渲染层 Select 的定位算法（Select.tsx updatePosition）：逐项钉死。
func TestSelectPanelRect(t *testing.T) {
	anchor := gfx.Rect{X: 100, Y: 100, Width: 160, Height: 40}
	cases := []struct {
		name     string
		anchor   gfx.Rect
		vw, vh   float32
		contentH float32
		minW     float32
		alignEnd bool
		want     gfx.Rect
	}{
		{"下方放得下：贴在触发器下方 4px，宽度取 180（面板最小宽度）", anchor, 1280, 800, 118, 180, false, gfx.Rect{X: 100, Y: 144, Width: 180, Height: 118}},
		{"触发器比最小宽度宽：宽度跟触发器", gfx.Rect{X: 100, Y: 100, Width: 300, Height: 40}, 1280, 800, 118, 180, false, gfx.Rect{X: 100, Y: 144, Width: 300, Height: 118}},
		{"align=end：右边缘与触发器对齐", anchor, 1280, 800, 118, 180, true, gfx.Rect{X: 80, Y: 144, Width: 180, Height: 118}},
		{"左边缘夹到视口留白 12", gfx.Rect{X: 2, Y: 100, Width: 160, Height: 40}, 1280, 800, 118, 180, false, gfx.Rect{X: 12, Y: 144, Width: 180, Height: 118}},
		{"右侧越界：夹回 视口宽 − 面板宽 − 12", gfx.Rect{X: 1200, Y: 100, Width: 60, Height: 40}, 1280, 800, 118, 180, false, gfx.Rect{X: 1088, Y: 144, Width: 180, Height: 118}},
		{"下方 < 160 且上方更宽裕：向上展开，锚定底边（高度由选项决定）", gfx.Rect{X: 100, Y: 600, Width: 160, Height: 40}, 1280, 700, 118, 180, false, gfx.Rect{X: 100, Y: 478, Width: 180, Height: 118}},
		{"两侧都放不下：仍向下（上方不更宽裕）", gfx.Rect{X: 100, Y: 100, Width: 160, Height: 40}, 1280, 250, 118, 180, false, gfx.Rect{X: 100, Y: 144, Width: 180, Height: 118}},
		{"选项多：高度取 280 上限", anchor, 1280, 1000, 600, 180, false, gfx.Rect{X: 100, Y: 144, Width: 180, Height: 280}},
		{"选项多、下方可用空间 244：高度取可用空间", anchor, 1280, 400, 600, 180, false, gfx.Rect{X: 100, Y: 144, Width: 180, Height: 244}},
		{"选项多且空间不足 160：高度至少 160", anchor, 1280, 300, 600, 180, false, gfx.Rect{X: 100, Y: 144, Width: 180, Height: 160}},
		{"窄触发器可调小最小宽度", gfx.Rect{X: 100, Y: 100, Width: 70, Height: 32}, 1280, 800, 100, 96, false, gfx.Rect{X: 100, Y: 136, Width: 96, Height: 100}},
	}
	for _, c := range cases {
		got := selectPanelRect(c.anchor, c.vw, c.vh, c.contentH, c.minW, c.alignEnd)
		if got != c.want {
			t.Errorf("%s：得到 %+v，期望 %+v", c.name, got, c.want)
		}
	}
}

// selectPage 是一个带下拉的页面：上面一个普通按钮用来确认「点外面」会传给下面的控件。
type selectPage struct {
	st      SelectState
	value   string
	picks   []string
	pressed int
}

func (p *selectPage) view(l *Look, c ui.C) {
	ui.Column(c).Fill().Pad(24).Gap(24).Children(func() {
		btn := ui.Row(c).Size(120, 40).Hit(ui.ID(c), "其他按钮")
		if btn.Clicked() {
			p.pressed++
		}
		ui.Row(c).FillWidth().Children(func() {
			if v, ok := l.Select(c, &p.st, SelectSpec{
				Label: "关闭窗口行为", Value: p.value, Width: l.Rem(10),
				Options: []SelectOption{{Value: "ask", Label: "每次询问"}, {Value: "minimize", Label: "最小化到托盘"}, {Value: "exit", Label: "退出应用"}},
			}); ok {
				p.picks = append(p.picks, v)
				p.value = v
			}
		})
	})
}

func openSelectPage(t *testing.T) (*harness, *selectPage) {
	p := &selectPage{value: "ask"}
	return openPage(t, still(), 800, 600, p.view), p
}

func TestSelectClosedByDefault(t *testing.T) {
	h, _ := openSelectPage(t)
	trigger, ok := h.node(gfx.RoleCombobox, "关闭窗口行为")
	if !ok {
		t.Fatal("没有找到下拉触发器")
	}
	if trigger.Expanded || trigger.Value != "每次询问" {
		t.Errorf("触发器：expanded=%v value=%q，期望 false / 每次询问", trigger.Expanded, trigger.Value)
	}
	if h.has(gfx.RoleListBox, "关闭窗口行为") {
		t.Error("默认不应出现选项面板")
	}
}

func TestSelectOpensWithOptionsBelowTrigger(t *testing.T) {
	h, _ := openSelectPage(t)
	h.click("关闭窗口行为")
	trigger, _ := h.node(gfx.RoleCombobox, "关闭窗口行为")
	if !trigger.Expanded {
		t.Error("点击后触发器应处于展开状态")
	}
	list, ok := h.node(gfx.RoleListBox, "关闭窗口行为")
	if !ok {
		t.Fatal("点击后没有出现选项面板（listbox）")
	}
	for _, name := range []string{"每次询问", "最小化到托盘", "退出应用"} {
		if !h.has(gfx.RoleOption, name) {
			t.Errorf("选项面板缺少 %q", name)
		}
	}
	// 几何：在触发器下方 4px，宽度 max(触发器, 180)，高度 = 边框 2 + py-1 共 8 + 三行 × 2.25rem(36)。
	tb, lb := trigger.Bounds, list.Bounds
	if math.Abs(float64(lb.Y-(tb.Y+tb.Height+4))) > 0.01 || math.Abs(float64(lb.X-tb.X)) > 0.01 {
		t.Errorf("面板位置 (%g,%g)，期望在触发器下方 4px：(%g,%g)", lb.X, lb.Y, tb.X, tb.Y+tb.Height+4)
	}
	if lb.Width != 180 || math.Abs(float64(lb.Height-(2+8+3*36))) > 0.01 {
		t.Errorf("面板大小 %gx%g，期望 180x%d", lb.Width, lb.Height, 2+8+3*36)
	}
	// 当前值带对勾并标记选中。
	if o, _ := h.node(gfx.RoleOption, "每次询问"); !o.Selected {
		t.Error("当前值对应的选项应标记为选中")
	}
}

func TestSelectPickClosesAndReports(t *testing.T) {
	h, p := openSelectPage(t)
	h.click("关闭窗口行为")
	h.click("最小化到托盘")
	if len(p.picks) != 1 || p.picks[0] != "minimize" {
		t.Fatalf("选择结果 %v，期望 [minimize]", p.picks)
	}
	if h.has(gfx.RoleListBox, "关闭窗口行为") {
		t.Error("选中后面板应关闭")
	}
	if trigger, _ := h.node(gfx.RoleCombobox, "关闭窗口行为"); trigger.Value != "最小化到托盘" || trigger.Expanded {
		t.Errorf("选中后触发器：value=%q expanded=%v", trigger.Value, trigger.Expanded)
	}
}

func TestSelectEscapeCloses(t *testing.T) {
	h, p := openSelectPage(t)
	h.click("关闭窗口行为")
	h.keys("Escape")
	if h.has(gfx.RoleListBox, "关闭窗口行为") {
		t.Error("Esc 后面板应关闭")
	}
	if len(p.picks) != 0 {
		t.Errorf("Esc 不应产生选择：%v", p.picks)
	}
}

// 点面板外：关闭，并且这一下继续交给下面的控件（网页 mousedown 外部点击的行为）。
func TestSelectOutsideClickClosesAndPassesThrough(t *testing.T) {
	h, p := openSelectPage(t)
	h.click("关闭窗口行为")
	btn, ok := h.node(gfx.RoleButton, "其他按钮")
	if !ok {
		t.Fatal("没有找到「其他按钮」")
	}
	h.clickAt(btn.Bounds.X+10, btn.Bounds.Y+10)
	if h.has(gfx.RoleListBox, "关闭窗口行为") {
		t.Error("点面板外后面板应关闭")
	}
	if p.pressed != 1 {
		t.Errorf("点面板外的这一下应继续交给下面的按钮：按下次数 %d，期望 1", p.pressed)
	}
}

// 面板打开时再点触发器：收起，不能被传给触发器再展开一次。
func TestSelectTriggerClickWhileOpenClosesOnce(t *testing.T) {
	h, _ := openSelectPage(t)
	h.click("关闭窗口行为")
	trigger, _ := h.node(gfx.RoleCombobox, "关闭窗口行为")
	h.clickAt(trigger.Bounds.X+20, trigger.Bounds.Y+trigger.Bounds.Height/2)
	if h.has(gfx.RoleListBox, "关闭窗口行为") {
		t.Error("再点触发器应收起面板（而不是收起后又被展开）")
	}
	if trigger, _ = h.node(gfx.RoleCombobox, "关闭窗口行为"); trigger.Expanded {
		t.Error("触发器仍显示展开")
	}
}

// 按在面板的边距上（不是某个选项）：面板保持打开。
func TestSelectPanelPaddingPressKeepsOpen(t *testing.T) {
	h, p := openSelectPage(t)
	h.click("关闭窗口行为")
	list, _ := h.node(gfx.RoleListBox, "关闭窗口行为")
	h.clickAt(list.Bounds.X+list.Bounds.Width/2, list.Bounds.Y+2) // 第一行之上的内边距
	if !h.has(gfx.RoleListBox, "关闭窗口行为") {
		t.Error("按在面板内边距上不应关闭面板")
	}
	if len(p.picks) != 0 {
		t.Errorf("按在内边距上不应选中任何项：%v", p.picks)
	}
}

// 选项行的前导图标（SelectOption.Leading）：画在文字左侧、与文字相隔 gap-2（8px）、垂直居中、不进可访问树与文字清单；
// 没有 Leading 的行布局不变（文字紧贴 px-3 的左内边距）。
func TestSelectOptionLeadingIconSitsBeforeTheLabel(t *testing.T) {
	var st SelectState
	st.Open = true
	var lead gfx.Rect
	view := func(l *Look, c ui.C) {
		ui.Column(c).Fill().Pad(24).Children(func() {
			l.Select(c, &st, SelectSpec{
				Label: "供应商", Value: "", Width: l.Rem(10), // 不选中任何项：触发按钮不显示选项文字，下面数的就只有面板里的行
				Options: []SelectOption{
					{Value: "plain", Label: "无图标"},
					{Value: "icon", Label: "有图标", Leading: func(c ui.C) {
						ui.Slot(c).Size(16, 16).Shrink(0).Background(l.Col(l.T.Primary)).DrawOver(func(d *ui.DrawContext) { lead = d.Rect })
					}},
				},
			})
		})
	}
	h := openPage(t, still(), 600, 400, view)
	plain, ok1 := h.node(gfx.RoleOption, "无图标")
	icon, ok2 := h.node(gfx.RoleOption, "有图标")
	if !ok1 || !ok2 {
		t.Fatal("两个选项都应出现，可访问名是各自的文字")
	}
	var plainX, iconX float32 = -1, -1
	for _, text := range h.session.Snapshot().Texts {
		switch text.Value {
		case "无图标":
			plainX = text.Bounds.X
		case "有图标":
			iconX = text.Bounds.X
		}
	}
	if plainX < 0 || iconX < 0 {
		t.Fatalf("没有找到选项文字：%v %v", plainX, iconX)
	}
	if want := plain.Bounds.X + 12; math.Abs(float64(plainX-want)) > 0.01 {
		t.Errorf("无前导图标的行文字起点 %g，应贴在 px-3 左内边距 %g", plainX, want)
	}
	if want := icon.Bounds.X + 12; math.Abs(float64(lead.X-want)) > 0.01 || lead.Width != 16 || lead.Height != 16 {
		t.Errorf("前导图标 %+v，应从 px-3 处开始、16×16", lead)
	}
	if want := lead.X + lead.Width + 8; math.Abs(float64(iconX-want)) > 0.01 {
		t.Errorf("有前导图标的行文字起点 %g，应在图标右侧 gap-2：%g", iconX, want)
	}
	if mid, rowMid := lead.Y+lead.Height/2, icon.Bounds.Y+icon.Bounds.Height/2; math.Abs(float64(mid-rowMid)) > 0.5 {
		t.Errorf("前导图标应垂直居中：%g vs %g", mid, rowMid)
	}
	var texts int
	for _, text := range h.session.Snapshot().Texts {
		if text.Value == "有图标" || text.Value == "无图标" {
			texts++
		}
	}
	if texts != 2 {
		t.Errorf("前导图标不应多出文字节点：选项文字出现 %d 次，期望 2", texts)
	}
}

// 前导图标不改变选择行为：点有图标的行照常选中并关闭。
func TestSelectOptionWithLeadingStillPicks(t *testing.T) {
	var st SelectState
	st.Open = true
	var picked []string
	view := func(l *Look, c ui.C) {
		ui.Column(c).Fill().Pad(24).Children(func() {
			if v, ok := l.Select(c, &st, SelectSpec{
				Label: "供应商", Value: "a", Width: l.Rem(10),
				Options: []SelectOption{
					{Value: "a", Label: "甲"},
					{Value: "b", Label: "乙", Leading: func(c ui.C) { ui.Slot(c).Size(16, 16).Shrink(0) }},
				},
			}); ok {
				picked = append(picked, v)
			}
		})
	}
	h := openPage(t, still(), 600, 400, view)
	h.click("乙")
	if len(picked) != 1 || picked[0] != "b" || st.Open {
		t.Fatalf("应选中 b 并关闭：%v open=%v", picked, st.Open)
	}
}
