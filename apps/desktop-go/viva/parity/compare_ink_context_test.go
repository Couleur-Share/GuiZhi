package parity

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// 台账 G-018：量墨迹的区域里整个包着另一行小数左缘文字，墨迹剖面是两行文字的合集。
func TestSharedWithFractionalText(t *testing.T) {
	self := Box{100, 10, 42, 14}
	area := image.Rect(99, 9, 143, 25)
	cases := []struct {
		name  string
		rects []Box
		want  bool
	}{
		{"另一行整个在区域里、左缘是小数", []Box{self, {105.5, 10, 21, 14}}, true},
		{"另一行左缘是整数像素", []Box{self, {105, 10, 21, 14}}, false},
		{"另一行只有一部分在区域里", []Box{self, {135.5, 10, 21, 14}}, false},
		{"另一行在区域之外", []Box{self, {300.5, 10, 21, 14}}, false},
		{"另一行比区域高：只压住一部分", []Box{self, {105.5, 10, 21, 40}}, false},
		{"另一行的左缘越出区域", []Box{self, {98.5, 10, 21, 14}}, false},
		{"另一行的上缘越出区域", []Box{self, {105.5, 8, 21, 14}}, false},
		{"事实里只有自己", []Box{self}, false},
		{"同一外框重复出现不算另一行", []Box{self, {100, 10, 42, 14}}, false},
		{"零宽的外框不算", []Box{self, {105.5, 10, 0, 14}}, false},
		{"自己之外有多行，只要一行成立", []Box{self, {300.5, 10, 21, 14}, {105.5, 10, 21, 14}}, true},
	}
	for _, c := range cases {
		f := &Facts{TextRects: c.rects}
		if got := sharedWithFractionalText(f, self, area); got != c.want {
			t.Errorf("%s：sharedWithFractionalText = %v，期望 %v", c.name, got, c.want)
		}
	}
	if sharedWithFractionalText(nil, self, area) {
		t.Error("没有事实时不应判为共用")
	}
	// 自己的左缘也在小数像素上时，自己不能算作「另一行」（外框相同的那条要被排除）。
	fractional := Box{100.5, 10, 42, 14}
	if sharedWithFractionalText(&Facts{TextRects: []Box{fractional}}, fractional, area) {
		t.Error("事实里只有自己（左缘是小数）：不应判为共用")
	}
}

func TestClipTruncates(t *testing.T) {
	rect := Box{1222.125, 745.5, 14, 14}
	cases := []struct {
		name string
		clip []float64
		want bool
	}{
		{"没被裁", nil, false},
		{"裁成一半高", []float64{1222.125, 745.5, 14, 7}, true},
		{"裁成一半宽", []float64{1222.125, 745.5, 7, 14}, true},
		{"只差半个像素行的取整", []float64{1222.125, 745.5, 14, 13.5}, false},
		{"恰好少 1 像素：仍算取整", []float64{1222.125, 745.5, 14, 13}, false},
		{"少 1.25 像素：被截", []float64{1222.125, 745.5, 14, 12.75}, true},
		{"裁剪区比外框大", []float64{1200, 700, 100, 100}, false},
		{"长度不对的 clip 当作没裁", []float64{1, 2, 3}, false},
	}
	for _, c := range cases {
		if got := clipTruncates(rect, c.clip); got != c.want {
			t.Errorf("%s：clipTruncates = %v，期望 %v", c.name, got, c.want)
		}
	}
}

// inkFixture 画一对同一行文字的墨迹：React 是 [10,20)×[10,14) 的黑块，Viva 右移 shift 列、颜色为 gray，都在白底上。
func inkFixture(shift int, gray uint8) (react, viva image.Image) {
	const w, h = 40, 30
	draw := func(x0 int, c uint8) image.Image {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.Set(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
		for y := 10; y < 14; y++ {
			for x := x0; x < x0+10; x++ {
				img.Set(x, y, color.RGBA{c, c, c, 255})
			}
		}
		return img
	}
	return draw(10, 0), draw(10+shift, gray)
}

func checkInkFixture(t *testing.T, reactImg, vivaImg image.Image, textRects []Box, clip []float64) Issue {
	t.Helper()
	th, err := DefaultThresholds()
	if err != nil {
		t.Fatal(err)
	}
	rect := Box{10, 10, 10, 4} // 整数左缘：水平位移上限 0
	in := Inputs{React: &Facts{TextRects: textRects}, Viva: &Facts{}, ReactImg: reactImg, VivaImg: vivaImg}
	rep := &Report{Issues: []Issue{}}
	checkInk(rep, in, th, "「文字」", rect, true, false, clip)
	if len(rep.Issues) != 1 {
		t.Fatalf("应报出一条墨迹问题，得到 %+v", rep.Issues)
	}
	return rep.Issues[0]
}

// checkInk 报出的墨迹问题带着发生处的事实：有另一行小数左缘文字包在区域里（Shared）。
func TestCheckInkMarksShared(t *testing.T) {
	reactImg, vivaImg := inkFixture(1, 0) // 右移 1 列：墨迹错位
	rect := Box{10, 10, 10, 4}
	other := Box{12.5, 10, 4, 4} // 整个在区域 [9,21)×[9,15) 里，左缘 12.5
	is := checkInkFixture(t, reactImg, vivaImg, []Box{rect}, nil)
	if is.Kind != "ink" || is.Shared || is.Clipped {
		t.Errorf("没有另一行文字、没被裁：%+v，期望 ink 且 Shared、Clipped 都为假", is)
	}
	if is := checkInkFixture(t, reactImg, vivaImg, []Box{rect, other}, nil); !is.Shared || is.Clipped {
		t.Errorf("区域里包着另一行小数左缘文字：Shared %v、Clipped %v，期望 true、false", is.Shared, is.Clipped)
	}
	if is := checkInkFixture(t, reactImg, vivaImg, []Box{rect, {12, 10, 4, 4}}, nil); is.Shared {
		t.Error("另一行左缘是整数像素：不应判为共用")
	}
}

// 「一侧没有墨迹」同样带着事实：被包着另一行小数左缘文字时，量到的有无墨迹也是两行的合集。
func TestCheckInkNoInkIssueCarriesContext(t *testing.T) {
	reactImg, vivaImg := inkFixture(0, 255) // Viva 的块是白色：这一侧没有墨迹
	rect := Box{10, 10, 10, 4}
	other := Box{12.5, 10, 4, 4}
	is := checkInkFixture(t, reactImg, vivaImg, []Box{rect}, nil)
	if is.Kind != "ink" || !strings.Contains(is.Message, "一侧没有墨迹") || is.Shared || is.Clipped {
		t.Errorf("没有另一行文字、没被裁：%+v，期望「一侧没有墨迹」且 Shared、Clipped 都为假", is)
	}
	if is := checkInkFixture(t, reactImg, vivaImg, []Box{rect, other}, nil); !is.Shared {
		t.Error("一侧没有墨迹，区域里又包着另一行小数左缘文字：期望 Shared 为真")
	}
	if is := checkInkFixture(t, reactImg, vivaImg, []Box{rect}, []float64{10, 8, 7, 10}); !is.Clipped {
		t.Error("一侧没有墨迹，文字又被裁线截掉三列：期望 Clipped 为真")
	}
}

// 墨迹总量问题同样带着事实：文字被祖先的裁剪线截掉一截（Clipped）。
func TestCheckInkMarksClipped(t *testing.T) {
	reactImg, vivaImg := inkFixture(0, 128) // 位置相同、Viva 颜色淡一半：墨迹总量比约 0.5
	rect := Box{10, 10, 10, 4}
	is := checkInkFixture(t, reactImg, vivaImg, []Box{rect}, nil)
	if is.Kind != "ink-mass" || is.Dir != -1 || is.Clipped || is.Shared {
		t.Errorf("没被裁：%+v，期望 ink-mass、Dir -1，Clipped、Shared 都为假", is)
	}
	// 看得见的区域只有左边 7 列（外框宽 10）：被裁；区域取向内取整的裁剪区，背景行仍在区域里，墨迹总量照常量得出。
	if is := checkInkFixture(t, reactImg, vivaImg, []Box{rect}, []float64{10, 8, 7, 10}); is.Kind != "ink-mass" || !is.Clipped {
		t.Errorf("被裁线截掉三列：%+v，期望 ink-mass 且 Clipped 为真", is)
	}
	if is := checkInkFixture(t, reactImg, vivaImg, []Box{rect}, []float64{0, 0, 40, 30}); is.Clipped {
		t.Error("裁剪区比外框大：不应判为被裁")
	}
}

// 带 Shared、Clipped、Variant 的登记只在前提成立时放过；前提不成立的同类问题照常判定（含没有偏差大小的与纵向错位）。
func TestWaiverPremisesDecideTheScope(t *testing.T) {
	ink := Issue{Layer: LayerL2, Kind: "ink", Subject: "「更新时间」", Delta: 1}
	shared := ink
	shared.Shared = true
	clipped := Issue{Layer: LayerL2, Kind: "ink-mass", Subject: "图标 [1222.12 745.50 14.00 14.00]", Delta: 0.25, Dir: -1, Clipped: true}
	free := clipped
	free.Clipped = false
	flipped := clipped
	flipped.Dir = 1
	sharedRule := Waiver{Layer: LayerL2, Kind: "ink", Shared: true, MaxDelta: 1}
	clippedRule := Waiver{Layer: LayerL2, Kind: "ink-mass", Subject: " 14.00 14.00]", Dir: -1, Clipped: true, MaxDelta: 0.26}
	cases := []struct {
		name    string
		w       Waiver
		is      Issue
		variant string
		want    bool
	}{
		{"Shared 的登记放过共用的问题", sharedRule, shared, "", true},
		{"Shared 的登记不放过没共用的同类问题", sharedRule, ink, "", false},
		{"Shared 的登记照样受 MaxDelta 约束", sharedRule, Issue{Layer: LayerL2, Kind: "ink", Delta: 2, Shared: true}, "", false},
		{"Shared 的登记照样受缺省 MaxDY 约束", sharedRule, Issue{Layer: LayerL2, Kind: "ink", Delta: 1, DY: 1, Shared: true}, "", false},
		{"Clipped 的登记放过被裁的图标", clippedRule, clipped, "", true},
		{"Clipped 的登记不放过没被裁的同一枚图标", clippedRule, free, "", false},
		{"Clipped 的登记不放过方向相反的问题", clippedRule, flipped, "", false},
		{"Variant 的登记在名字含它的变体里放过", Waiver{Layer: LayerL2, Kind: "ink", Variant: "-large-"}, ink, "light-royal-blue-large-1280x800", true},
		{"Variant 的登记在别的字号里不放过", Waiver{Layer: LayerL2, Kind: "ink", Variant: "-large-"}, ink, "light-royal-blue-small-1280x800", false},
		{"Variant 的登记在没有变体名时不放过", Waiver{Layer: LayerL2, Kind: "ink", Variant: "-large-"}, ink, "", false},
		{"不写 Variant 的登记不限变体", Waiver{Layer: LayerL2, Kind: "ink"}, ink, "dark-royal-blue-small-800x600", true},
	}
	for _, c := range cases {
		if got := c.w.matchesIn(c.is, c.variant); got != c.want {
			t.Errorf("%s：matchesIn = %v，期望 %v", c.name, got, c.want)
		}
	}
	// 经 applyWaivers 走完整条路径：变体取自报告，被放过的问题记在 Waived 里。
	rep := &Report{Variant: "light-royal-blue-large-1280x800", Issues: []Issue{ink, shared}}
	applyWaivers(rep, []Waiver{{Ledger: "G-018", Layer: LayerL2, Kind: "ink", Shared: true, MaxDelta: 1, Variant: "-large-"}})
	if len(rep.Issues) != 1 || rep.Issues[0].Shared || len(rep.Waived) != 1 || !rep.Waived[0].Issue.Shared {
		t.Errorf("只有共用的那条应被放过：剩 %+v，放过 %+v", rep.Issues, rep.Waived)
	}
}
