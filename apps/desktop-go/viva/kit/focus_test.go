package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// FocusOf 读键盘焦点所在命中区的种类（窗口级键盘钩子的两个判断，台账 B-045）：
// 渲染层 use-item-keyboard.ts 里焦点在输入框时按键不归列表、焦点在按钮上时 Enter 与空格不归列表。

func TestFocusOfTellsTextFieldsFromButtons(t *testing.T) {
	var canvas *gfx.Canvas
	var field ui.Field
	var st ButtonStyle
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		canvas = c.Canvas()
		if st.Height == 0 {
			st = l.UIGhostButton(UIButtonSM)
		}
		ui.Column(c).Fill().Gap(12).Children(func() {
			l.FieldInput(c.Key("name"), &field, FieldSpec{Name: "名称", Placeholder: "名称"})
			l.Button(c.Key("ok"), ButtonSpec{Label: "确定"}, st)
		})
	})
	if got := FocusOf(canvas); got.Held {
		t.Fatalf("还没有焦点就报告有焦点：%+v", got)
	}
	if got := FocusOf(nil); got.Held {
		t.Fatalf("nil 画布报告有焦点：%+v", got)
	}

	if err := h.session.ClickLabel("名称"); err != nil {
		t.Fatalf("点输入框失败：%v", err)
	}
	h.frames(3)
	got := FocusOf(canvas)
	if !got.Held || got.Role != gfx.RoleTextField || !got.Editable() || got.Activatable() || got.Floating() {
		t.Fatalf("焦点在输入框里：%+v（应是页面层的可编辑文本框，不是可激活控件）", got)
	}

	h.click("确定")
	got = FocusOf(canvas)
	if !got.Held || got.Role != gfx.RoleButton || got.Editable() || !got.Activatable() || got.Floating() {
		t.Fatalf("焦点在按钮上：%+v（应是页面层的可激活控件，不是可编辑的）", got)
	}
}

func TestFocusKinds(t *testing.T) {
	cases := []struct {
		role                 gfx.Role
		editable, activatble bool
	}{
		{gfx.RoleTextField, true, false},
		{gfx.RoleCheckbox, true, false}, // 渲染层的复选框是原生 input
		{gfx.RoleSwitch, false, true},   // 开关是 button role=switch
		{gfx.RoleButton, false, true},
		{gfx.RoleLink, false, true},
		{gfx.RoleMenuItem, false, true},
		{gfx.RoleCombobox, false, true}, // 自绘下拉的触发器是 button
		{gfx.RoleOption, false, true},
		{gfx.RoleGroup, false, false},
		{0, false, false}, // 命中区找不到
	}
	for _, c := range cases {
		f := FocusInfo{Held: true, Role: c.role}
		if f.Editable() != c.editable || f.Activatable() != c.activatble {
			t.Errorf("角色 %v：Editable=%v Activatable=%v，应为 %v、%v", c.role, f.Editable(), f.Activatable(), c.editable, c.activatble)
		}
	}
	if (FocusInfo{Layer: 0}).Floating() || !(FocusInfo{Layer: 1}).Floating() || !(FocusInfo{Layer: 2}).Floating() {
		t.Error("Floating 应按层判断：0 是页面层，1 起都是浮层")
	}
}
