package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// Modal 把指针限制在自己的焦点作用域里；下拉面板画在 Modal 之后（层号更高），要借用这个作用域才点得到。
// 没有这一层，面板能看见却点不到，点下去还落到 Modal 的背景上把对话框关掉。
type selectInModalPage struct {
	modal  ModalState
	st     SelectState
	value  string
	closed int
}

func (p *selectInModalPage) view(l *Look, c ui.C) {
	ui.Column(c).Fill().Children(func() {})
	spec := ModalSpec{
		Open: true, Title: modalTitle, DialogLabel: modalDialog, CloseLabel: modalClose,
		Body: func(c ui.C) {
			if v, ok := l.Select(c.Key("select"), &p.st, SelectSpec{
				Label: "关闭窗口行为", Value: p.value, Width: l.Rem(10),
				Options: []SelectOption{{Value: "ask", Label: "每次询问"}, {Value: "minimize", Label: "最小化到托盘"}, {Value: "exit", Label: "退出应用"}},
			}); ok {
				p.value = v
			}
		},
	}
	if l.Modal(c, &p.modal, spec) {
		p.closed++
	}
}

func TestSelectInsideModalCanBePickedWithoutClosingTheModal(t *testing.T) {
	p := &selectInModalPage{value: "ask"}
	h := openPage(t, still(), 800, 600, p.view)
	h.click("关闭窗口行为")
	if !p.st.Open {
		t.Fatal("点触发器应展开下拉")
	}
	h.click("最小化到托盘")
	if p.value != "minimize" || p.st.Open {
		t.Fatalf("应选中并收起：value=%q open=%v", p.value, p.st.Open)
	}
	if p.closed != 0 || !h.has(gfx.RoleHeading, modalTitle) {
		t.Fatalf("选择不该关掉对话框：closed=%d", p.closed)
	}
}

// 下拉开着时点面板之外：只收起下拉，对话框仍在（背景层同样要在作用域里，否则这一下直接落到 Modal 背景）。
func TestSelectInsideModalClosesOnOutsidePressOnly(t *testing.T) {
	p := &selectInModalPage{value: "ask"}
	h := openPage(t, still(), 800, 600, p.view)
	h.click("关闭窗口行为")
	title, _ := h.node(gfx.RoleHeading, modalTitle)
	h.clickAt(title.Bounds.X+title.Bounds.Width/2, title.Bounds.Y+title.Bounds.Height/2)
	if p.st.Open {
		t.Fatal("点面板外应收起下拉")
	}
}
