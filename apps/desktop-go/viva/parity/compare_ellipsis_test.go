package parity

import "testing"

func TestIsEllipsisCut(t *testing.T) {
	cases := []struct {
		react, viva string
		want        bool
	}{
		{"abcdef…", "abcd…", true}, // 截在不同的字上，Viva 短
		{"abcd…", "abcdef…", true}, // Viva 长
		{"abcd…", "abcd…", false},  // 完全相同不是「截断点不同」
		{"abcdef", "abcd…", false}, // 一边没截断
		{"abcd…", "abXd…", false},  // 不是前缀关系：是别的文字
		{"…", "abcd…", false},      // 只剩省略号
		{"abcd…", "…", false},
	}
	for _, c := range cases {
		if got := isEllipsisCut(c.react, c.viva); got != c.want {
			t.Errorf("isEllipsisCut(%q, %q) = %v，期望 %v", c.react, c.viva, got, c.want)
		}
	}
}

// 同一段标题截在不同的字上：报成 ellipsis-cut（带浮层标记），不是笼统的 text；登记过的能被放过，没登记的照常报。
func TestEllipsisCutIsReportedAsItsOwnKindAndCanBeWaived(t *testing.T) {
	th, err := DefaultThresholds()
	if err != nil {
		t.Fatal(err)
	}
	region := Region{Width: 400, Height: 100}
	react := &Facts{Scene: "t", Region: region, Capture: Capture{ForcedDSF: true}, Texts: []Text{
		{Text: "https://example.com/very/long…", Rects: []Box{{10, 10, 200, 16}}},
	}}
	viva := &Facts{Scene: "t", Region: region, Texts: []Text{
		{Text: "https://example.com/very/lo…", Rects: []Box{{10, 10, 190, 16}}, Overlay: true},
	}}
	rep, _ := Compare(Inputs{React: react, Viva: viva, FactsOnly: true}, th)
	if got := issueKinds(rep); got != "ellipsis-cut" {
		t.Fatalf("问题类别 = %q，期望 ellipsis-cut", got)
	}
	if !rep.Issues[0].Overlay {
		t.Error("Viva 的文字在浮层里，问题应带浮层标记")
	}

	waiver := Waiver{Ledger: "A-013", Layer: LayerL1, Kind: "ellipsis-cut", Subject: "https://ex", Overlay: true, Note: "测试"}
	rep, _ = Compare(Inputs{React: react, Viva: viva, FactsOnly: true, Waivers: []Waiver{waiver}}, th)
	if len(rep.Issues) != 0 || len(rep.Waived) != 1 {
		t.Fatalf("登记过的应被放过：问题 %d，放过 %d", len(rep.Issues), len(rep.Waived))
	}

	// 不在浮层里的同样偏差（页面上的文字）不被只管浮层的登记放过。
	viva.Texts[0].Overlay = false
	rep, _ = Compare(Inputs{React: react, Viva: viva, FactsOnly: true, Waivers: []Waiver{waiver}}, th)
	if got := issueKinds(rep); got != "ellipsis-cut" {
		t.Fatalf("页面层的截断点偏差不应被浮层的登记放过：%q", got)
	}
}
