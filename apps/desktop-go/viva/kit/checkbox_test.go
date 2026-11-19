package kit

import (
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
