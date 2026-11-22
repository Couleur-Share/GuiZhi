package wiki

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

func gnode(id string) services.WikiGraphNode {
	return services.WikiGraphNode{ID: id, Title: "T-" + id, Kind: services.WikiKindTopic}
}

func glink(a, b string) services.WikiGraphLink { return services.WikiGraphLink{Source: a, Target: b} }

func TestBuildGraphModelCleansEdges(t *testing.T) {
	m := BuildGraphModel(services.WikiGraph{
		Nodes: []services.WikiGraphNode{gnode("a"), gnode("b"), gnode("c"), gnode("a")}, // 重复节点只留第一个
		Links: []services.WikiGraphLink{
			glink("a", "b"), glink("a", "b"), // 重复边
			glink("b", "a"),                          // 反向边是另一条
			glink("c", "c"),                          // 自环
			glink("a", "ghost"), glink("ghost", "b"), // 悬空
			glink("b", "c"),
		},
		TotalNodes: 3,
	})
	if len(m.Nodes) != 3 {
		t.Fatalf("节点：%+v", m.Nodes)
	}
	want := []GraphEdge{{0, 1}, {1, 0}, {1, 2}}
	if !slices.Equal(m.Edges, want) {
		t.Fatalf("边：got %+v want %+v", m.Edges, want)
	}
	if m.Nodes[0].Degree != 2 || m.Nodes[1].Degree != 3 || m.Nodes[2].Degree != 1 {
		t.Fatalf("度数：%d %d %d", m.Nodes[0].Degree, m.Nodes[1].Degree, m.Nodes[2].Degree)
	}
	if m.Truncated() || m.Empty() {
		t.Fatal("不截断也不为空")
	}
}

func TestBuildGraphModelTruncationAndEmpty(t *testing.T) {
	m := BuildGraphModel(services.WikiGraph{Nodes: []services.WikiGraphNode{gnode("a")}, TotalNodes: 305})
	if !m.Truncated() || m.Total != 305 {
		t.Fatalf("总数大于节点数说明被截断：%+v", m)
	}
	under := BuildGraphModel(services.WikiGraph{Nodes: []services.WikiGraphNode{gnode("a"), gnode("b")}, TotalNodes: 0})
	if under.Total != 2 || under.Truncated() {
		t.Fatalf("总数偏小时取节点数，不会出现负的「被截断」：%+v", under)
	}
	empty := BuildGraphModel(services.WikiGraph{})
	if !empty.Empty() || empty.Truncated() || len(empty.Nodes) != 0 {
		t.Fatalf("空图谱：%+v", empty)
	}
	if empty.NodeAt(0, 0, 10) != -1 || empty.IndexOf("x") != -1 {
		t.Fatal("空图谱命中不到任何节点")
	}
}

func TestLayoutIsDeterministic(t *testing.T) {
	g := ringGraph(40)
	a, b := BuildGraphModel(g), BuildGraphModel(g)
	for i := range a.Nodes {
		if a.Nodes[i].X != b.Nodes[i].X || a.Nodes[i].Y != b.Nodes[i].Y {
			t.Fatalf("同一份输入应得到同一份布局：节点 %d (%v,%v) vs (%v,%v)", i, a.Nodes[i].X, a.Nodes[i].Y, b.Nodes[i].X, b.Nodes[i].Y)
		}
	}
}

func ringGraph(n int) services.WikiGraph {
	g := services.WikiGraph{TotalNodes: n}
	for i := 0; i < n; i++ {
		g.Nodes = append(g.Nodes, gnode(fmt.Sprintf("n%d", i)))
		g.Links = append(g.Links, glink(fmt.Sprintf("n%d", i), fmt.Sprintf("n%d", (i+1)%n)))
	}
	return g
}

func TestLayoutStaysInUnitCircleAndIsFinite(t *testing.T) {
	for _, n := range []int{2, 3, 7, 50, 300} {
		m := BuildGraphModel(ringGraph(n))
		var far float64
		for _, node := range m.Nodes {
			if math.IsNaN(node.X) || math.IsNaN(node.Y) || math.IsInf(node.X, 0) || math.IsInf(node.Y, 0) {
				t.Fatalf("n=%d：坐标不是有限数：%+v", n, node)
			}
			far = math.Max(far, math.Hypot(node.X, node.Y))
		}
		if far > 1+1e-9 || far < 1-1e-9 {
			t.Fatalf("n=%d：最远节点应正好落在单位圆上，实际 %v", n, far)
		}
	}
}

func TestLayoutSeparatesNodes(t *testing.T) {
	m := BuildGraphModel(ringGraph(30))
	minDist := math.MaxFloat64
	for i := range m.Nodes {
		for j := i + 1; j < len(m.Nodes); j++ {
			minDist = math.Min(minDist, math.Hypot(m.Nodes[i].X-m.Nodes[j].X, m.Nodes[i].Y-m.Nodes[j].Y))
		}
	}
	if minDist < 0.02 {
		t.Fatalf("节点不应挤成一团：最近两点相距 %v", minDist)
	}
}

func TestLayoutNoEdgesAndSingle(t *testing.T) {
	one := BuildGraphModel(services.WikiGraph{Nodes: []services.WikiGraphNode{gnode("solo")}, TotalNodes: 1})
	if one.Nodes[0].X != 0 || one.Nodes[0].Y != 0 {
		t.Fatalf("单个节点在原点：%+v", one.Nodes[0])
	}
	g := services.WikiGraph{TotalNodes: 20}
	for i := 0; i < 20; i++ {
		g.Nodes = append(g.Nodes, gnode(fmt.Sprintf("n%d", i)))
	}
	m := BuildGraphModel(g)
	seen := map[[2]float64]bool{}
	for _, n := range m.Nodes {
		if math.IsNaN(n.X) || math.IsNaN(n.Y) {
			t.Fatal("没有边时坐标也必须有限")
		}
		seen[[2]float64{n.X, n.Y}] = true
	}
	if len(seen) != 20 {
		t.Fatalf("互不连通的节点不应重合：%d 个不同位置", len(seen))
	}
}

func TestLayoutCompletesFastAtCap(t *testing.T) {
	g := ringGraph(300)
	for i := 0; i < 300; i += 3 {
		g.Links = append(g.Links, glink(fmt.Sprintf("n%d", i), fmt.Sprintf("n%d", (i+100)%300)))
	}
	start := time.Now()
	m := BuildGraphModel(g)
	if len(m.Nodes) != 300 {
		t.Fatalf("节点数：%d", len(m.Nodes))
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("300 个节点的布局应在秒级内完成，实际 %v", d)
	}
}

func TestNodeAtAndIndexOf(t *testing.T) {
	m := BuildGraphModel(ringGraph(5))
	target := m.Nodes[3]
	if got := m.NodeAt(target.X, target.Y, 0.01); got != 3 {
		t.Fatalf("NodeAt 精确命中：%d", got)
	}
	if got := m.NodeAt(target.X+0.005, target.Y-0.005, 0.05); got != 3 {
		t.Fatalf("NodeAt 容差内命中：%d", got)
	}
	if got := m.NodeAt(5, 5, 0.1); got != -1 {
		t.Fatalf("远处没有节点：%d", got)
	}
	if m.IndexOf("n2") != 2 || m.IndexOf("missing") != -1 {
		t.Fatal("IndexOf")
	}
}

func TestNodeAtPicksNearest(t *testing.T) {
	m := GraphModel{Nodes: []GraphNode{{ID: "a", X: 0, Y: 0}, {ID: "b", X: 0.1, Y: 0}}}
	if got := m.NodeAt(0.08, 0, 0.5); got != 1 {
		t.Fatalf("两个都在半径内时取最近的：%d", got)
	}
}

func TestKindIndexAndTruncateLabel(t *testing.T) {
	if KindIndex(services.WikiKindTopic) != 0 || KindIndex(services.WikiKindEntity) != 1 || KindIndex(services.WikiKindConcept) != 2 || KindIndex("mystery") != 0 {
		t.Fatal("类型序号；未知类型归入 topic")
	}
	if TruncateLabel("short") != "short" || TruncateLabel("") != "" {
		t.Fatal("短标题原样")
	}
	exact := strings.Repeat("a", LabelMaxLength)
	if TruncateLabel(exact) != exact {
		t.Fatal("恰好 14 个码元不截断")
	}
	if got := TruncateLabel(strings.Repeat("a", LabelMaxLength+1)); got != strings.Repeat("a", LabelMaxLength)+"…" {
		t.Fatalf("超长截断：%q", got)
	}
	cjk := strings.Repeat("概", 20)
	if got := TruncateLabel(cjk); got != strings.Repeat("概", 14)+"…" {
		t.Fatalf("汉字按码元计：%q", got)
	}
	// 第 14 个码元落在代理对中间：丢掉半个，不留乱码。
	emoji := strings.Repeat("a", 13) + "😀" + "tail"
	if got := TruncateLabel(emoji); got != strings.Repeat("a", 13)+"…" {
		t.Fatalf("不劈开代理对：%q", got)
	}
}
