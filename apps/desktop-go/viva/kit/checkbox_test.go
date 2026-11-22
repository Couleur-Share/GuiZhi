package kit

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 复选框的返回契约：调用一返回，*checked 就含本帧的翻转。表格的行选择框每帧拿一个局部副本，
// 返回之后立刻与旧值比较并同步给外部状态（勾选集合）；如果值要等到本页结束才写入，这个比较永远只看到旧值，
// 点击就整个丢了——界面级测试补上之前，表格的行首复选框与表头全选框一直点不动。
func TestCheckboxValueIsWrittenBeforeTheCallReturns(t *testing.T) {
	var stored bool // 外部状态（表格里是 Store 的勾选集合）
	flips := 0
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		on := stored // 每帧一个新的局部副本，与 tableRow 的写法相同
		ui.Column(c).Fill().Pad(24).Children(func() {
			l.Checkbox(c.Key("cb"), &on, CheckboxSpec{Name: "选择本行"})
		})
		if on != stored {
			flips++
			stored = on
		}
	})
	n, ok := h.node(gfx.RoleCheckbox, "选择本行")
	if !ok {
		t.Fatal("没有找到复选框")
	}
	h.clickAt(n.Bounds.X+8, n.Bounds.Y+8)
	if flips != 1 || !stored {
		t.Fatalf("点一次：调用返回后应立刻看到翻转（flips=%d stored=%v，期望 1 / true）", flips, stored)
	}
	h.clickAt(n.Bounds.X+8, n.Bounds.Y+8)
	if flips != 2 || stored {
		t.Fatalf("再点一次应翻回去（flips=%d stored=%v，期望 2 / false）", flips, stored)
	}
	if got, _ := h.node(gfx.RoleCheckbox, "选择本行"); got.Checked {
		t.Error("可访问节点应跟着取消勾选")
	}
}

// disabled 的复选框（渲染层 disabled：label 上 opacity-50，点击直接返回，<input> 禁用）：点不动、整体淡到 50%、
// 节点标成禁用；启用时照常翻转。
func TestCheckboxDisabledIsInertAndFaded(t *testing.T) {
	var on, disabled bool
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Pad(24).Children(func() {
			l.Checkbox(c.Key("cb"), &on, CheckboxSpec{Label: "同时清除", Disabled: disabled})
		})
	})
	alpha := func() float32 {
		a, _ := textAlpha(h, "同时清除")
		return a
	}
	n, ok := h.node(gfx.RoleCheckbox, "同时清除")
	if !ok {
		t.Fatal("没有找到复选框")
	}
	if n.Disabled || alpha() < 0.999 {
		t.Fatalf("启用时不应禁用也不应变淡：disabled=%v 文字不透明度 %.3f", n.Disabled, alpha())
	}
	h.clickAt(n.Bounds.X+8, n.Bounds.Y+8)
	if !on {
		t.Fatal("启用时点一下应当勾选")
	}
	disabled = true
	h.frames(3)
	n, _ = h.node(gfx.RoleCheckbox, "同时清除")
	if !n.Disabled {
		t.Error("禁用时节点应标成禁用")
	}
	if a := alpha(); math.Abs(float64(a-0.5)) > 0.01 {
		t.Errorf("禁用时文字不透明度 %.3f，期望 0.5（opacity-50）", a)
	}
	h.clickAt(n.Bounds.X+8, n.Bounds.Y+8) // 点方框
	if !on {
		t.Error("禁用时点方框不应改变值")
	}
	label, _ := confirmTextBounds(h, "同时清除")
	h.clickAt(label.X+4, label.Y+4) // 点文字（label 的点击）
	if !on {
		t.Error("禁用时点文字不应改变值")
	}
	disabled = false
	h.frames(3)
	h.clickAt(label.X+4, label.Y+4)
	if on {
		t.Error("恢复启用后点文字应翻转（取消勾选）")
	}
}

// indeterminate（半选）：可访问状态是 mixed 而不是 checked；点方框与点文字都按 *checked 翻转（半选时 *checked 为假，所以点一下是「选中」）；
// 半选解除之后回到普通复选框。
func TestCheckboxIndeterminateIsMixedAndClickFlipsTheBoundValue(t *testing.T) {
	var on, mixed bool
	mixed = true
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Pad(24).Children(func() {
			l.Checkbox(c.Key("cb"), &on, CheckboxSpec{Label: "全选", Indeterminate: mixed})
		})
	})
	n, ok := h.node(gfx.RoleCheckbox, "全选")
	if !ok {
		t.Fatal("没有找到复选框")
	}
	if !n.Mixed || n.Checked {
		t.Fatalf("半选时应是 mixed、不是 checked：%+v", n)
	}
	h.clickAt(n.Bounds.X+8, n.Bounds.Y+8) // 点方框
	if !on {
		t.Fatal("半选时点方框应翻转 *checked（选中）")
	}
	on, mixed = false, true
	h.frames(3)
	label, _ := confirmTextBounds(h, "全选")
	h.clickAt(label.X+4, label.Y+4) // 点文字
	if !on {
		t.Fatal("半选时点文字也应翻转 *checked（选中）")
	}
	on, mixed = true, false
	h.frames(3)
	if n, _ = h.node(gfx.RoleCheckbox, "全选"); n.Mixed || !n.Checked {
		t.Fatalf("解除半选并选中后应是 checked：%+v", n)
	}
}