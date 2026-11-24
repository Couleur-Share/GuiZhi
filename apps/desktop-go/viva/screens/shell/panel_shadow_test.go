package shell

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// 台账 B-048：侧栏右缘外阴影的目标条按被更高层不透明条盖住的纵向区间切成上下两段，其余照旧。
func TestShadowStripParts(t *testing.T) {
	strip := gfx.Rect{X: 100, Y: 48, Width: 4, Height: 752} // 纵向 48–800
	r := func(y, h float32) gfx.Rect { return gfx.Rect{X: 100, Y: y, Width: 4, Height: h} }
	cases := []struct {
		name        string
		top, bottom float32
		occluded    bool
		want        []gfx.Rect
	}{
		{"没有遮挡：整条", 0, 0, false, []gfx.Rect{strip}},
		{"报告为假时忽略区间", 100, 200, false, []gfx.Rect{strip}},
		{"空区间：整条", 200, 200, true, []gfx.Rect{strip}},
		{"倒置区间：整条", 300, 200, true, []gfx.Rect{strip}},
		{"中间一段：切成上下两段", 100, 140, true, []gfx.Rect{r(48, 52), r(140, 660)}},
		{"盖住顶部：只剩下段", 0, 140, true, []gfx.Rect{r(140, 660)}},
		{"盖住底部：只剩上段", 700, 900, true, []gfx.Rect{r(48, 652)}},
		{"全部盖住：一段也没有", 0, 900, true, nil},
		{"贴着上沿：只剩下段", 48, 90, true, []gfx.Rect{r(90, 710)}},
		{"完全在条上方：整条", 0, 40, true, []gfx.Rect{strip}},
		{"完全在条下方：整条", 810, 900, true, []gfx.Rect{strip}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shadowStripParts(strip, c.top, c.bottom, c.occluded)
			if len(got) != len(c.want) {
				t.Fatalf("得到 %d 段 %v，期望 %d 段 %v", len(got), got, len(c.want), c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("第 %d 段 = %v，期望 %v", i, got[i], c.want[i])
				}
			}
		})
	}
}

// 上下两段加上被盖住的区间正好拼成整条：切口处不重叠、不留缝（投影盒子不变，只收窄裁剪条，高斯投影才严丝合缝）。
func TestShadowStripPartsAreSeamless(t *testing.T) {
	strip := gfx.Rect{X: 0, Y: 10, Width: 3, Height: 500}
	parts := shadowStripParts(strip, 120, 175.5, true)
	if len(parts) != 2 {
		t.Fatalf("应切成两段：%v", parts)
	}
	covered := parts[0].Height + (175.5 - 120) + parts[1].Height
	if covered != strip.Height {
		t.Errorf("两段与被盖区间的高度之和 %v，应等于整条 %v", covered, strip.Height)
	}
	if parts[0].Y+parts[0].Height != 120 || parts[1].Y != 175.5 {
		t.Errorf("切口应落在被盖区间的两端：上段下沿 %v、下段上沿 %v", parts[0].Y+parts[0].Height, parts[1].Y)
	}
}
