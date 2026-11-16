package parity

import (
	"strings"
	"testing"
)

func svgPaint(b Box) Paint { return Paint{Tag: "svg", Box: b, Opacity: 1} }

func iconFacts(react []Box, viva []Box) Inputs {
	r := &Facts{Scene: "t", Region: Region{Width: 100, Height: 100}}
	for _, b := range react {
		r.Paints = append(r.Paints, svgPaint(b))
	}
	// 不是 svg 的绘制项不算图标。
	r.Paints = append(r.Paints, Paint{Tag: "div", Box: Box{0, 0, 100, 100}, Opacity: 1})
	v := &Facts{Scene: "t", Region: r.Region, Icons: viva}
	return Inputs{React: r, Viva: v, FactsOnly: true}
}

func issueKinds(rep *Report) string {
	var kinds []string
	for _, is := range rep.Issues {
		kinds = append(kinds, is.Kind)
	}
	return strings.Join(kinds, ",")
}

func TestPairIconsMatchesNearestAndReportsLeftovers(t *testing.T) {
	react := []Box{{10, 10, 16, 16}, {40, 10, 16, 16}, {70, 10, 16, 16}}
	viva := []Box{{40.5, 10, 16, 16}, {10, 10.5, 16, 16}, {90, 50, 16, 16}}
	pairs, missing, extra := pairIcons(react, viva)
	if len(pairs) != 2 || len(missing) != 1 || len(extra) != 1 {
		t.Fatalf("配对 %d、缺 %d、多 %d", len(pairs), len(missing), len(extra))
	}
	if pairs[0].React != react[0] || pairs[0].Viva != viva[1] || pairs[1].React != react[1] || pairs[1].Viva != viva[0] {
		t.Errorf("应按位置最近配对并按 React 的位置排序：%+v", pairs)
	}
	if missing[0] != react[2] || extra[0] != viva[2] {
		t.Errorf("缺少的应是第三枚、多出的应是远处那枚：missing %v extra %v", missing, extra)
	}
}

func TestPairIconsIsOneToOne(t *testing.T) {
	// 两枚 React 图标挨得很近、Viva 只有一枚：只能配上一枚，另一枚算缺。
	pairs, missing, extra := pairIcons([]Box{{10, 10, 16, 16}, {11, 10, 16, 16}}, []Box{{10.2, 10, 16, 16}})
	if len(pairs) != 1 || len(missing) != 1 || len(extra) != 0 {
		t.Fatalf("配对 %d、缺 %d、多 %d", len(pairs), len(missing), len(extra))
	}
}

func TestCompareIconsFlagsMissingExtraAndMisplaced(t *testing.T) {
	th, err := DefaultThresholds()
	if err != nil {
		t.Fatal(err)
	}
	same := Box{10, 10, 16, 16}
	cases := []struct {
		name        string
		react, viva []Box
		want        string
	}{
		{"完全一致", []Box{same}, []Box{same}, ""},
		{"差 1px 在阈值内", []Box{same}, []Box{{11, 10, 16, 16}}, ""},
		{"差 1.5px 超过阈值", []Box{same}, []Box{{11.5, 10, 16, 16}}, "icon-box"},
		{"尺寸不同", []Box{same}, []Box{{10, 10, 18, 18}}, "icon-box"},
		{"缺图标", []Box{same, {40, 10, 16, 16}}, []Box{same}, "icon-missing"},
		{"多图标", []Box{same}, []Box{same, {40, 10, 16, 16}}, "icon-extra"},
		{"相距太远算缺加多", []Box{same}, []Box{{10, 20, 16, 16}}, "icon-missing,icon-extra"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := iconFacts(c.react, c.viva)
			rep := &Report{}
			compareIcons(rep, in, th)
			if got := issueKinds(rep); got != c.want {
				t.Errorf("问题 %q，期望 %q", got, c.want)
			}
			if rep.L2.Icons != len(c.react) {
				t.Errorf("React 图标数 %d，期望 %d", rep.L2.Icons, len(c.react))
			}
		})
	}
}

// 图标的外框进了 L3 遮罩，遮罩比外框大 pad 像素；图标之外的像素不在遮罩里。
func TestTextMaskCoversIconsOnBothSides(t *testing.T) {
	in := iconFacts([]Box{{10, 10, 16, 16}}, []Box{{50, 50, 16, 16}})
	const w, h, pad = 100, 100, 1
	mask := textMask(in, w, h, pad)
	at := func(x, y int) bool { return mask[y*w+x] }
	for _, p := range [][2]int{{10, 10}, {25, 25}, {9, 9}, {26, 26}, {50, 50}, {65, 65}, {49, 49}, {66, 66}} {
		if !at(p[0], p[1]) {
			t.Errorf("(%d,%d) 应在遮罩里", p[0], p[1])
		}
	}
	for _, p := range [][2]int{{7, 7}, {28, 28}, {47, 47}, {68, 68}, {0, 0}, {99, 99}} {
		if at(p[0], p[1]) {
			t.Errorf("(%d,%d) 不应在遮罩里", p[0], p[1])
		}
	}
}

func TestReactIconsOnlyTakesSvgPaints(t *testing.T) {
	in := iconFacts([]Box{{1, 2, 3, 4}, {5, 6, 7, 8}}, nil)
	got := reactIcons(in.React)
	if len(got) != 2 || got[0] != (Box{1, 2, 3, 4}) || got[1] != (Box{5, 6, 7, 8}) {
		t.Errorf("应只取 svg 绘制项且保持顺序：%v", got)
	}
}
