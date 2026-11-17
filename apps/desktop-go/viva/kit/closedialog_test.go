package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// closeDialogPage 是一个带关闭对话框的页面：对话框的结果记在 got 里，收到结果就把 open 置假（调用方的职责）。
type closeDialogPage struct {
	st   CloseDialogState
	open bool
	got  []CloseDialogResult
}

func (p *closeDialogPage) view(l *Look, c ui.C) {
	ui.Column(c).Fill().Pad(24).Children(func() {
		l.Text(c, "背后的页面", TextSM, WeightNormal, l.T.Foreground)
	})
	res := l.CloseDialog(c.Key("dlg"), &p.st, CloseDialogSpec{
		Open: p.open, Title: "关闭应用", Message: "您想要关闭应用还是最小化到系统托盘？",
		MinimizeText: "最小化到托盘", ExitText: "退出应用", RememberText: "记住我的选择", CloseLabel: "关闭",
	})
	if res.Choice != CloseDialogNone {
		p.got = append(p.got, res)
		p.open = false
	}
}

func openCloseDialogPage(t *testing.T) (*harness, *closeDialogPage) {
	t.Helper()
	p := &closeDialogPage{}
	h := openPage(t, still(), 1280, 800, p.view)
	return h, p
}

// open 打开对话框并出帧到稳定。
func (p *closeDialogPage) show(h *harness) {
	h.t.Helper()
	p.open = true
	h.frames(4)
}

// clickRememberLabel 点「记住我的选择」右边的文字（label 的 onClick：翻转，不取键盘焦点）。
func clickRememberLabel(h *harness) {
	h.t.Helper()
	n, ok := h.node(gfx.RoleCheckbox, "记住我的选择")
	if !ok {
		h.t.Fatal("没有找到复选框「记住我的选择」")
	}
	h.clickAt(n.Bounds.X+n.Bounds.Width+16, n.Bounds.Y+n.Bounds.Height/2)
}

func (p *closeDialogPage) last(t *testing.T) CloseDialogResult {
	t.Helper()
	if len(p.got) == 0 {
		t.Fatal("对话框没有给出任何结果")
	}
	return p.got[len(p.got)-1]
}

func TestCloseDialogClosedDrawsNothing(t *testing.T) {
	h, _ := openCloseDialogPage(t)
	if h.has(gfx.RoleGroup, "关闭应用") || h.has(gfx.RoleButton, "最小化到托盘") {
		t.Error("没有打开时不应出现对话框")
	}
}

// 打开后：面板是带名字与说明的分组（渲染层的 alertdialog），两枚选项与关闭钮、复选框都在。
func TestCloseDialogStructure(t *testing.T) {
	h, p := openCloseDialogPage(t)
	p.show(h)
	panel, ok := h.node(gfx.RoleGroup, "关闭应用")
	if !ok {
		t.Fatal("没有找到名为「关闭应用」的对话框面板")
	}
	if panel.Description != "您想要关闭应用还是最小化到系统托盘？" {
		t.Errorf("面板的说明 = %q", panel.Description)
	}
	for _, want := range []struct {
		role gfx.Role
		name string
	}{
		{gfx.RoleButton, "关闭"}, {gfx.RoleButton, "最小化到托盘"}, {gfx.RoleButton, "退出应用"}, {gfx.RoleCheckbox, "记住我的选择"},
	} {
		if !h.has(want.role, want.name) {
			t.Errorf("缺少 %v「%s」", want.role, want.name)
		}
	}
	// 几何（与渲染层实测一致）：max-w-sm = 384 宽，水平居中。
	if panel.Bounds.Width != 384 || panel.Bounds.X != (1280-384)/2 {
		t.Errorf("面板横向 (x=%g, 宽 %g)，期望宽 384、水平居中", panel.Bounds.X, panel.Bounds.Width)
	}
}

func TestCloseDialogOptionsReturnChoice(t *testing.T) {
	for _, tc := range []struct {
		button string
		want   CloseDialogChoice
	}{{"最小化到托盘", CloseDialogMinimize}, {"退出应用", CloseDialogExit}} {
		h, p := openCloseDialogPage(t)
		p.show(h)
		h.click(tc.button)
		got := p.last(t)
		if got.Choice != tc.want || got.Remember {
			t.Errorf("点「%s」：结果 %+v，期望选项 %v、不记住", tc.button, got, tc.want)
		}
		if h.has(gfx.RoleGroup, "关闭应用") {
			t.Errorf("点「%s」之后对话框应当关上", tc.button)
		}
	}
}

// 勾上「记住我的选择」再选：结果带着 Remember（渲染层 handleMinimize / handleExit 读的就是 rememberChoice）。
func TestCloseDialogRememberTravelsWithChoice(t *testing.T) {
	h, p := openCloseDialogPage(t)
	p.show(h)
	clickRememberLabel(h)
	if n, _ := h.node(gfx.RoleCheckbox, "记住我的选择"); !n.Checked {
		t.Fatal("点标签文字应当勾上复选框")
	}
	h.click("退出应用")
	if got := p.last(t); got.Choice != CloseDialogExit || !got.Remember {
		t.Errorf("结果 %+v，期望退出且记住", got)
	}
}

// 每次打开都把复选框复位（渲染层 useEffect([isOpen])）。
func TestCloseDialogRememberResetsOnReopen(t *testing.T) {
	h, p := openCloseDialogPage(t)
	p.show(h)
	clickRememberLabel(h)
	h.click("关闭") // Cancel：对话框关上
	if h.has(gfx.RoleGroup, "关闭应用") {
		t.Fatal("前提不成立：点关闭钮后对话框应当关上")
	}
	p.show(h)
	if n, ok := h.node(gfx.RoleCheckbox, "记住我的选择"); !ok || n.Checked {
		t.Errorf("再次打开时复选框应当复位为未勾选：%+v（找到=%v）", n, ok)
	}
}

// Esc、点遮罩、点关闭钮都是「只关对话框」；点在面板上不关。
func TestCloseDialogCancelPaths(t *testing.T) {
	cancel := func(name string, act func(h *harness)) {
		t.Run(name, func(t *testing.T) {
			h, p := openCloseDialogPage(t)
			p.show(h)
			act(h)
			if got := p.last(t); got.Choice != CloseDialogCancel {
				t.Errorf("结果 %+v，期望 Cancel", got)
			}
			if h.has(gfx.RoleGroup, "关闭应用") {
				t.Error("对话框应当关上")
			}
		})
	}
	cancel("Esc", func(h *harness) { h.keys("Escape") })
	cancel("遮罩", func(h *harness) { h.clickAt(40, 40) })
	cancel("关闭钮", func(h *harness) { h.click("关闭") })

	t.Run("点在面板上不关", func(t *testing.T) {
		h, p := openCloseDialogPage(t)
		p.show(h)
		panel, _ := h.node(gfx.RoleGroup, "关闭应用")
		h.clickAt(panel.Bounds.X+panel.Bounds.Width/2, panel.Bounds.Y+panel.Bounds.Height-6)
		if len(p.got) != 0 {
			t.Errorf("点在面板空白处不应有结果，实际 %+v", p.got)
		}
		if !h.has(gfx.RoleGroup, "关闭应用") {
			t.Error("点在面板上对话框应当保持打开")
		}
	})
}

// Tab 顺序与渲染层的 DOM 次序一致：关闭钮、最小化、退出、复选框；没有焦点陷阱不在这里测（渲染层不拦 Tab）。
func TestCloseDialogTabOrderAndSpaceToggles(t *testing.T) {
	h, p := openCloseDialogPage(t)
	p.show(h)
	order := []struct {
		role gfx.Role
		name string
	}{{gfx.RoleButton, "关闭"}, {gfx.RoleButton, "最小化到托盘"}, {gfx.RoleButton, "退出应用"}, {gfx.RoleCheckbox, "记住我的选择"}}
	for i, want := range order {
		h.keys("Tab")
		if n, ok := h.node(want.role, want.name); !ok || !n.Focused {
			t.Fatalf("第 %d 次 Tab 后焦点应在「%s」", i+1, want.name)
		}
	}
	h.keys("Space")
	if n, _ := h.node(gfx.RoleCheckbox, "记住我的选择"); !n.Checked {
		t.Error("复选框聚焦后按空格应当勾上")
	}
	h.keys("Space")
	if n, _ := h.node(gfx.RoleCheckbox, "记住我的选择"); n.Checked {
		t.Error("再按一次空格应当取消勾选")
	}
}

// 点标签文字翻转但不取键盘焦点（label 的 onClick 里 preventDefault 了）；点方框则翻转同一个值。
func TestCheckboxLabelAndSquareToggle(t *testing.T) {
	var checked bool
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Pad(24).Children(func() {
			l.Checkbox(c.Key("cb"), &checked, CheckboxSpec{Label: "记住我的选择"})
		})
	})
	n, ok := h.node(gfx.RoleCheckbox, "记住我的选择")
	if !ok {
		t.Fatal("没有找到复选框")
	}
	if n.Bounds.Width != 16 || n.Bounds.Height != 16 {
		t.Errorf("方框 %gx%g，期望 16x16（h-4 w-4）", n.Bounds.Width, n.Bounds.Height)
	}
	h.clickAt(n.Bounds.X+n.Bounds.Width+20, n.Bounds.Y+8)
	if !checked {
		t.Fatal("点标签文字应当勾上")
	}
	if n, _ := h.node(gfx.RoleCheckbox, "记住我的选择"); n.Focused {
		t.Error("点标签文字不应取键盘焦点")
	}
	h.clickAt(n.Bounds.X+8, n.Bounds.Y+8)
	if checked {
		t.Error("点方框应当取消勾选")
	}
	if n, _ := h.node(gfx.RoleCheckbox, "记住我的选择"); n.Checked {
		t.Error("可访问节点没有跟着取消勾选")
	}
}
