package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 没有选中任何选项（取值对不上）时，触发按钮显示占位文字（渲染层 placeholder ?? t('common.select')），用弱化色，
// 可访问节点的取值也是占位文字；取值对得上时占位文字不出现。
func TestSelectShowsItsPlaceholderWhenNothingIsSelected(t *testing.T) {
	var st SelectState
	value := ""
	view := func(l *Look, c ui.C) {
		ui.Column(c).Fill().Pad(24).Children(func() {
			l.Select(c, &st, SelectSpec{
				Label: "原文版本", Value: value, Width: l.Rem(12), Placeholder: "选择",
				Options: []SelectOption{{Value: "a", Label: "甲版本"}, {Value: "b", Label: "乙版本"}},
			})
		})
	}
	h := openPage(t, still(), 400, 200, view)
	node, ok := h.node(gfx.RoleCombobox, "原文版本")
	if !ok || node.Value != "选择" {
		t.Fatalf("没选时触发按钮的取值 = %q（找到=%v），期望占位文字「选择」", node.Value, ok)
	}
	var found bool
	for _, text := range h.session.Snapshot().Texts {
		if text.Value != "选择" {
			continue
		}
		found = true
		if want := h.look.Col(h.look.T.MutedForeground); text.Style.Color != want {
			t.Errorf("占位文字的颜色 = %+v，期望弱化前景色 %+v", text.Style.Color, want)
		}
	}
	if !found {
		t.Fatal("没有画出占位文字「选择」")
	}

	value = "b"
	h.frames(3)
	node, _ = h.node(gfx.RoleCombobox, "原文版本")
	if node.Value != "乙版本" {
		t.Fatalf("选中后取值 = %q，期望「乙版本」", node.Value)
	}
	for _, text := range h.session.Snapshot().Texts {
		if text.Value == "选择" {
			t.Fatal("选中后不应再画占位文字")
		}
		if text.Value == "乙版本" && text.Style.Color != h.look.Col(h.look.T.Foreground) {
			t.Errorf("选中项文字的颜色 = %+v，期望前景色", text.Style.Color)
		}
	}
}

// 没有登记占位文字时保持老行为：对不上取值就什么也不显示。
func TestSelectWithoutPlaceholderShowsNothingWhenNothingIsSelected(t *testing.T) {
	var st SelectState
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Pad(24).Children(func() {
			l.Select(c, &st, SelectSpec{Label: "原文版本", Value: "", Width: l.Rem(12), Options: []SelectOption{{Value: "a", Label: "甲版本"}}})
		})
	})
	node, ok := h.node(gfx.RoleCombobox, "原文版本")
	if !ok || node.Value != "" {
		t.Fatalf("没有占位文字时取值 = %q（找到=%v），期望空", node.Value, ok)
	}
}

// 紧凑型触发器（SelectSpec.Trigger 非空）没选中任何选项时，占位文字同样用弱化前景色。
func TestCompactSelectShowsItsPlaceholderInMutedColor(t *testing.T) {
	var st SelectState
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Pad(24).Children(func() {
			l.Select(c, &st, SelectSpec{
				Label: "数量", Value: "", Placeholder: "选择",
				Options: []SelectOption{{Value: "20", Label: "20 条"}},
				Trigger: &SelectTriggerStyle{Height: 28, PadX: 8, Radius: 6, FontPx: 12, LinePx: 16, Bg: l.T.Background, Border: l.T.Border, BorderW: 1},
			})
		})
	})
	var found bool
	for _, text := range h.session.Snapshot().Texts {
		if text.Value != "选择" {
			continue
		}
		found = true
		if want := h.look.Col(h.look.T.MutedForeground); text.Style.Color != want {
			t.Errorf("紧凑型下拉占位文字的颜色 = %+v，期望弱化前景色 %+v", text.Style.Color, want)
		}
	}
	if !found {
		t.Fatal("紧凑型下拉没有画出占位文字「选择」")
	}
}
