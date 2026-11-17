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
