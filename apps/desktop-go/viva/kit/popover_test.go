package kit

import (
	"strconv"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// popoverRig 是一块弹层页：页面上两枚按钮（「触发」登记进弹层的组，「页面按钮」不在组里），弹层里 rows 行文字。
type popoverRig struct {
	st   PopoverState
	rows int
	spec PopoverSpec
}

func openPopoverPage(t *testing.T, rig *popoverRig, width, height int) *harness {
	t.Helper()
	style := func(l *Look) ButtonStyle {
		return ButtonStyle{Height: 28, PadX: 8, Text: TextSM, Fg: l.T.Foreground}
	}
	return openPage(t, still(), width, height, func(l *Look, c ui.C) {
		rig.st.Group.Begin()
		ui.Column(c.Key("page")).FillWidth().Gap(8).Children(func() {
			l.Button(c.Key("trigger"), ButtonSpec{Label: "触发", Groups: &rig.st.Group, Group: PopoverGroup}, style(l))
			l.Button(c.Key("other"), ButtonSpec{Label: "页面按钮"}, style(l))
		})
		if !rig.st.Open {
			return
		}
		spec := rig.spec
		spec.Content = func() {
			for i := 0; i < rig.rows; i++ {
				l.Text(c.Key("row"+strconv.Itoa(i)), "行"+strconv.Itoa(i), TextSM, WeightNormal, l.T.Foreground)
			}
		}
		l.Popover(c.Key("pop"), &rig.st, spec)
	})
}

// 标签浮层（BulkTagPopover、TagPickerPopover）只在点外面与 Esc 时收起：焦点离开不关闭（StayOnBlur）。
// 标准弹层（组合筛选）则在焦点曾在组里、之后离开整组时关闭。
func TestPopoverStayOnBlurKeepsOpenWhenFocusLeaves(t *testing.T) {
	for _, stay := range []bool{true, false} {
		name := "默认：焦点离开整组即关闭"
		if stay {
			name = "StayOnBlur：焦点离开不关闭"
		}
		t.Run(name, func(t *testing.T) {
			rig := &popoverRig{rows: 2, spec: PopoverSpec{Name: "测试面板", Width: 200, Margin: 8, Pad: 12, Radius: 12, StayOnBlur: stay}}
			h := openPopoverPage(t, rig, 400, 300)
			rig.st.Show(gfx.Point{X: 20, Y: 100})
			h.frames(3)
			h.keys("Tab") // 焦点落到「触发」：在组里
			if !rig.st.Open {
				t.Fatal("焦点进组之后弹层不该关闭")
			}
			h.keys("Tab") // 焦点移到「页面按钮」：离开整组
			switch {
			case stay && !rig.st.Open:
				t.Error("StayOnBlur 的弹层不该因为焦点离开而关闭")
			case !stay && rig.st.Open:
				t.Error("标准弹层应当在焦点离开整组后关闭")
			}
		})
	}
}

// StayOnBlur 的弹层窗口缩放也不关闭（渲染层的标签浮层没有 resize 监听），标准弹层则关闭。
func TestPopoverStayOnBlurSurvivesResize(t *testing.T) {
	for _, stay := range []bool{true, false} {
		rig := &popoverRig{rows: 2, spec: PopoverSpec{Name: "测试面板", Width: 200, Margin: 8, Pad: 12, Radius: 12, StayOnBlur: stay}}
		h := openPopoverPage(t, rig, 400, 300)
		rig.st.Show(gfx.Point{X: 20, Y: 100})
		h.frames(3)
		if err := h.session.Resize(500, 320); err != nil {
			t.Fatalf("改变窗口尺寸失败：%v", err)
		}
		h.frames(3)
		if stay != rig.st.Open {
			t.Errorf("StayOnBlur=%v：缩放之后弹层 Open=%v", stay, rig.st.Open)
		}
	}
}
