package parity

import "testing"

func TestWaiverMatchesOnlyWhatItNames(t *testing.T) {
	issue := Issue{Layer: LayerL2, Kind: "text-width", Subject: "「YouTube」", Delta: 2.26}
	cases := []struct {
		name string
		w    Waiver
		want bool
	}{
		{"层、类别、主题都对", Waiver{Layer: LayerL2, Kind: "text-width", Subject: "YouTube"}, true},
		{"不写类别与主题则整层放过", Waiver{Layer: LayerL2}, true},
		{"层不对", Waiver{Layer: LayerL1, Kind: "text-width"}, false},
		{"类别不对", Waiver{Layer: LayerL2, Kind: "ink"}, false},
		{"主题不含", Waiver{Layer: LayerL2, Subject: "抖音"}, false},
		{"偏差在上限内", Waiver{Layer: LayerL2, MaxDelta: 2.5}, true},
		{"偏差超过上限", Waiver{Layer: LayerL2, MaxDelta: 2}, false},
		{"偏差达到下限", Waiver{Layer: LayerL2, MinDelta: 2}, true},
		{"偏差不到下限", Waiver{Layer: LayerL2, MinDelta: 3}, false},
		{"偏差落在上下限框出的范围内", Waiver{Layer: LayerL2, MinDelta: 2, MaxDelta: 2.5}, true},
		{"偏差落在范围之上", Waiver{Layer: LayerL2, MinDelta: 0.5, MaxDelta: 2}, false},
		{"偏差落在范围之下", Waiver{Layer: LayerL2, MinDelta: 2.4, MaxDelta: 3}, false},
		{"只管浮层，问题不在浮层", Waiver{Layer: LayerL2, Overlay: true}, false},
	}
	for _, c := range cases {
		if got := c.w.matches(issue); got != c.want {
			t.Errorf("%s：matches = %v，期望 %v", c.name, got, c.want)
		}
	}
	// 没有偏差大小的问题（如缺节点）不被带上下限的登记放过：没法证明它在范围里。
	noDelta := Issue{Layer: LayerL2, Kind: "text-width", Subject: "「YouTube」"}
	if (Waiver{Layer: LayerL2, MinDelta: 1}).matches(noDelta) || (Waiver{Layer: LayerL2, MaxDelta: 5}).matches(noDelta) {
		t.Error("没有偏差大小的问题不应被带上下限的登记放过")
	}
	overlay := issue
	overlay.Overlay = true
	if !(Waiver{Layer: LayerL2, Overlay: true}).matches(overlay) {
		t.Error("浮层里的问题应被只管浮层的登记放过")
	}
}

// 台账 G-012：Delta 取水平与纵向的较大值，字距、字偶距的登记（MaxDelta 2）如果不限纵向，
// 会把纵向错 1 像素的基线问题一并放过；所以缺省（MaxDY 为 0）一概不放过纵向错位，确有已登记的纵向偏差才显式写上限。
func TestWaiverDefaultDoesNotHideVerticalShifts(t *testing.T) {
	horizontal := Issue{Layer: LayerL2, Kind: "ink", Subject: "「YouTube」", Delta: 3} // 水平 +3、垂直 0（DY 为 0）
	baseline := Issue{Layer: LayerL2, Kind: "ink", Subject: "「YouTube」", Delta: 1, DY: 1}
	both := Issue{Layer: LayerL2, Kind: "ink", Subject: "「YouTube」", Delta: 4, DY: 1} // 水平 4、垂直 1
	twoRows := Issue{Layer: LayerL2, Kind: "ink", Subject: "「YouTube」", Delta: 2, DY: 2}
	strict := Waiver{Layer: LayerL2, Kind: "ink", Subject: "YouTube", MaxDelta: 4}
	withRow := strict
	withRow.MaxDY = 1
	for _, c := range []struct {
		name  string
		w     Waiver
		issue Issue
		want  bool
	}{
		{"缺省放过横向错位", strict, horizontal, true},
		{"缺省不放过纵向错位（G-012 要堵的口子）", strict, baseline, false},
		{"横向与纵向都错，缺省也不放过", strict, both, false},
		{"显式 MaxDY=1 放过纵向 1 像素", withRow, baseline, true},
		{"显式 MaxDY=1 放过横向 4 加纵向 1", withRow, both, true},
		{"显式 MaxDY=1 不放过纵向 2 像素", withRow, twoRows, false},
		{"MaxDY 不放宽 MaxDelta：偏差大小超限照样不放过", withRow, Issue{Layer: LayerL2, Kind: "ink", Subject: "「YouTube」", Delta: 5, DY: 1}, false},
	} {
		if got := c.w.matches(c.issue); got != c.want {
			t.Errorf("%s：matches = %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestWaiverEmojiScopeOnlyCoversPictographs(t *testing.T) {
	w := Waiver{Layer: LayerL2, Kind: "text-width", Emoji: true, MinDelta: 2.6, MaxDelta: 4.4}
	cases := []struct {
		subject string
		want    bool
	}{
		{"「💼」", true},
		{"「✈️」", true},       // 基本字符 + U+FE0F
		{"「⭐」", true},        // 杂项符号和箭头区
		{"「⌨️」", true},       // 杂项技术符号区
		{"「👍🏽」", true},       // 肤色修饰符
		{"「👨‍👩‍👧」", true},    // ZWJ 序列
		{"「YouTube」", false}, // 普通文字
		{"「💼 工作」", false},    // 表情加文字不是单个表情
		{"「12」", false},
		{"「」", false},
		{"图标 [100.00 500.00 16.00 16.00]", false},
	}
	for _, c := range cases {
		is := Issue{Layer: LayerL2, Kind: "text-width", Subject: c.subject, Delta: 3.23}
		if got := w.matches(is); got != c.want {
			t.Errorf("主题 %q：matches = %v，期望 %v", c.subject, got, c.want)
		}
	}
	// 表情的偏差落在框出的范围之外时照常判定：宽度差 8px 不是前进宽度之差造成的。
	if w.matches(Issue{Layer: LayerL2, Kind: "text-width", Subject: "「💼」", Delta: 8}) {
		t.Error("超出上限的表情宽度差不应被放过")
	}
	if w.matches(Issue{Layer: LayerL2, Kind: "ink", Subject: "「💼」", Delta: 3.23}) {
		t.Error("类别不同的问题不应被放过")
	}
}
