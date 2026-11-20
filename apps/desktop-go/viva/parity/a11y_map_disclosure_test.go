package parity

import "testing"

// TestDisclosureAndListRolesNormalize 守住台账 G-022（用户 2026-11-20 追认）的映射：<summary> 导出的 DisclosureTriangle
// 按 button 比较（名称与位置照常比），<ul> / <li> 导出的 list / listitem 只把条目分行、不进可访问节点序列，也不再被报为「未映射」；
// 别的没有映射的角色照常报告。
func TestDisclosureAndListRolesNormalize(t *testing.T) {
	f := &Facts{Scene: "s", Region: Region{Width: 800, Height: 600}, Nodes: []Node{
		{Role: "DisclosureTriangle", Name: "逐条结果（2）", Box: []float64{10, 10, 120, 20}},
		{Role: "list", Name: "", Box: []float64{10, 40, 300, 40}},
		{Role: "listitem", Name: "", Box: []float64{10, 40, 300, 20}},
		{Role: "listitem", Name: "", Box: []float64{10, 60, 300, 20}},
		{Role: "figure", Name: "别的角色", Box: []float64{10, 90, 30, 30}},
	}}
	out, unmapped := NormalizeReact(f)
	if len(out) != 1 || out[0].Role != "button" || out[0].Name != "逐条结果（2）" {
		t.Fatalf("应只剩一个 button「逐条结果（2）」：%+v", out)
	}
	if len(unmapped) != 1 || unmapped[0] != "figure" {
		t.Fatalf("DisclosureTriangle / list / listitem 不应再被报为未映射，别的角色照常报：%v", unmapped)
	}
}

// TestDisclosureTriangleEntersNodeComparison 是同一条映射的反向对照：折叠标题进了逐节点比较之后，名称一致就对齐、
// 不一致就被报出来，而不是像补映射之前那样在场景里靠放过条款掩盖（场景 list-batch-* 的 L1 原先没有逐节点比较）。
func TestDisclosureTriangleEntersNodeComparison(t *testing.T) {
	th, err := DefaultThresholds()
	if err != nil {
		t.Fatal(err)
	}
	region := Region{Width: 400, Height: 100}
	box := []float64{10, 10, 120, 20}
	compare := func(reactName, vivaName string) *Report {
		react := &Facts{Scene: "t", Region: region, Capture: Capture{ForcedDSF: true}, Nodes: []Node{
			{Role: "DisclosureTriangle", Name: reactName, Box: box},
			{Role: "list", Box: []float64{10, 40, 300, 20}},
			{Role: "listitem", Box: []float64{10, 40, 300, 20}},
		}}
		viva := &Facts{Scene: "t", Region: region, Nodes: []Node{{Role: "button", Name: vivaName, Box: box}}}
		rep, _ := Compare(Inputs{React: react, Viva: viva, FactsOnly: true}, th)
		return rep
	}
	if rep := compare("逐条结果（2）", "逐条结果（2）"); len(rep.Issues) != 0 || rep.L1.NodesMatched != 1 {
		t.Fatalf("名称一致应对齐、没有问题：问题 [%s]，对齐 %d", issueKinds(rep), rep.L1.NodesMatched)
	}
	rep := compare("逐条结果（2）", "逐条结果（3）")
	kinds := map[string]bool{}
	for _, is := range rep.Issues {
		kinds[is.Kind] = true
	}
	if !kinds["missing-node"] || !kinds["extra-node"] || kinds["unmapped-role"] {
		t.Fatalf("名称不一致应报 missing-node 与 extra-node，且不再有 unmapped-role：[%s]", issueKinds(rep))
	}
}
