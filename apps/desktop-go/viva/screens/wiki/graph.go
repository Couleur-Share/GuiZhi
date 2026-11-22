package wiki

import (
	"math"
	"unicode/utf16"

	"guizhi-mygo-shell/native/services"
)

// 图谱标签与力导向布局的常量。布局在单位空间 [-1, 1]² 里算：与视口大小、缩放、平移无关，
// 界面只负责把单位坐标映射到画布（同一份布局缩放时不用重算）。
const (
	// LabelMaxLength 是节点标题标签的最大长度（UTF-16 码元数，与渲染层 .length 一致）。
	LabelMaxLength = 14
	// LabelVisibleScale 是缩放到这个倍数以上才显示全部标题标签（选中节点始终显示）。
	LabelVisibleScale = 1.4

	layoutTicks = 150 // 与渲染层 cooldownTicks 一致
	goldenAngle = 2.399963229728653
)

// GraphNode 是图谱里的一个节点。X、Y 是单位空间里的布局坐标；Degree 是出链加入链的总数（已去重）。
type GraphNode struct {
	ID     string
	Title  string
	Kind   string
	Degree int
	X, Y   float64
}

// GraphEdge 是一条有向边（源页面引用目标页面），两端是 GraphModel.Nodes 的下标。
type GraphEdge struct{ From, To int }

// GraphModel 是图谱数据加布局，构建后不可变，可在多个 goroutine 间共享。
type GraphModel struct {
	Nodes []GraphNode
	Edges []GraphEdge
	// Total 是 Wiki 页面总数；比 len(Nodes) 大说明图谱按连接度被截断。
	Total int
}

// Truncated 报告图谱是否只显示了一部分页面（界面据此显示「按连接度显示 N / M 个页面」）。
func (m GraphModel) Truncated() bool { return m.Total > len(m.Nodes) }

// Empty 报告是否没有任何节点（界面显示「还没有页面关系」）。
func (m GraphModel) Empty() bool { return len(m.Nodes) == 0 }

// BuildGraphModel 把服务返回的图谱整理成可绘制的模型：重复的边与自环去掉，指向不在节点集合里的边（悬空）丢弃，
// 节点 ID 重复只留第一个，然后跑一遍确定性的力导向布局（同一份输入永远得到同一份坐标）。
func BuildGraphModel(g services.WikiGraph) GraphModel {
	index := make(map[string]int, len(g.Nodes))
	nodes := make([]GraphNode, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		if _, dup := index[n.ID]; dup {
			continue
		}
		index[n.ID] = len(nodes)
		nodes = append(nodes, GraphNode{ID: n.ID, Title: n.Title, Kind: n.Kind})
	}
	type pair struct{ a, b int }
	seen := map[pair]bool{}
	edges := make([]GraphEdge, 0, len(g.Links))
	for _, l := range g.Links {
		from, okFrom := index[l.Source]
		to, okTo := index[l.Target]
		if !okFrom || !okTo || from == to || seen[pair{from, to}] {
			continue
		}
		seen[pair{from, to}] = true
		edges = append(edges, GraphEdge{From: from, To: to})
		nodes[from].Degree++
		nodes[to].Degree++
	}
	m := GraphModel{Nodes: nodes, Edges: edges, Total: max(g.TotalNodes, len(nodes))}
	m.layout()
	return m
}

// layout 是 Fruchterman–Reingold 力导向布局：初始位置按黄金角螺旋铺开（确定性，不用随机数），
// 斥力 k²/d、边引力 d²/k，温度线性冷却；最后居中并缩放到单位圆内。节点数 ≤ 300，O(n²·ticks) 在毫秒级。
func (m *GraphModel) layout() {
	n := len(m.Nodes)
	if n == 0 {
		return
	}
	if n == 1 {
		m.Nodes[0].X, m.Nodes[0].Y = 0, 0
		return
	}
	for i := range m.Nodes {
		r := 0.9 * math.Sqrt((float64(i)+0.5)/float64(n))
		a := float64(i) * goldenAngle
		m.Nodes[i].X, m.Nodes[i].Y = r*math.Cos(a), r*math.Sin(a)
	}
	k := 2 / math.Sqrt(float64(n))
	dx, dy := make([]float64, n), make([]float64, n)
	for tick := 0; tick < layoutTicks; tick++ {
		temp := 0.2 * (1 - float64(tick)/layoutTicks)
		clear(dx)
		clear(dy)
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				ex, ey := m.Nodes[i].X-m.Nodes[j].X, m.Nodes[i].Y-m.Nodes[j].Y
				d := math.Hypot(ex, ey)
				if d < 1e-6 { // 重合时按下标错开，保持确定性
					ex, ey, d = math.Cos(float64(i)), math.Sin(float64(j)), 1
				}
				f := k * k / d
				fx, fy := ex/d*f, ey/d*f
				dx[i], dy[i] = dx[i]+fx, dy[i]+fy
				dx[j], dy[j] = dx[j]-fx, dy[j]-fy
			}
		}
		for _, e := range m.Edges {
			ex, ey := m.Nodes[e.From].X-m.Nodes[e.To].X, m.Nodes[e.From].Y-m.Nodes[e.To].Y
			d := math.Hypot(ex, ey)
			if d < 1e-6 {
				continue
			}
			f := d * d / k
			fx, fy := ex/d*f, ey/d*f
			dx[e.From], dy[e.From] = dx[e.From]-fx, dy[e.From]-fy
			dx[e.To], dy[e.To] = dx[e.To]+fx, dy[e.To]+fy
		}
		for i := range m.Nodes {
			// 向中心的弱引力：把互不连通的小团拉在一起，不至于被斥力推出画面。
			dx[i] -= m.Nodes[i].X * 0.5
			dy[i] -= m.Nodes[i].Y * 0.5
			d := math.Hypot(dx[i], dy[i])
			if d < 1e-9 {
				continue
			}
			step := math.Min(d, temp)
			m.Nodes[i].X += dx[i] / d * step
			m.Nodes[i].Y += dy[i] / d * step
		}
	}
	m.fitToUnit()
}

// fitToUnit 把布局平移到质心在原点，再整体缩放使最远的节点落在单位圆上（全部重合时保持原点）。
func (m *GraphModel) fitToUnit() {
	var cx, cy float64
	for _, n := range m.Nodes {
		cx += n.X
		cy += n.Y
	}
	cx, cy = cx/float64(len(m.Nodes)), cy/float64(len(m.Nodes))
	var far float64
	for i := range m.Nodes {
		m.Nodes[i].X -= cx
		m.Nodes[i].Y -= cy
		far = math.Max(far, math.Hypot(m.Nodes[i].X, m.Nodes[i].Y))
	}
	if far < 1e-9 {
		return
	}
	for i := range m.Nodes {
		m.Nodes[i].X /= far
		m.Nodes[i].Y /= far
	}
}

// NodeAt 返回距 (x, y)（单位空间坐标）最近、且在 radius 之内的节点下标；没有返回 -1。命中测试用。
func (m GraphModel) NodeAt(x, y, radius float64) int {
	best, bestD := -1, radius
	for i, n := range m.Nodes {
		if d := math.Hypot(n.X-x, n.Y-y); d <= bestD {
			best, bestD = i, d
		}
	}
	return best
}

// IndexOf 返回页面 ID 对应的节点下标；不在图谱里（如被截断）返回 -1。
func (m GraphModel) IndexOf(pageID string) int {
	for i, n := range m.Nodes {
		if n.ID == pageID {
			return i
		}
	}
	return -1
}

// KindIndex 是页面类型在图例里的序号（topic 0、entity 1、concept 2）；未知类型归入 topic，与渲染层的配色回退一致。
func KindIndex(kind string) int {
	switch kind {
	case services.WikiKindEntity:
		return 1
	case services.WikiKindConcept:
		return 2
	}
	return 0
}

// TruncateLabel 把节点标题截成标签：超过 LabelMaxLength 个 UTF-16 码元时截断并加省略号（不会把代理对劈成两半）。
func TruncateLabel(title string) string {
	units := utf16.Encode([]rune(title))
	if len(units) <= LabelMaxLength {
		return title
	}
	cut := units[:LabelMaxLength]
	if last := cut[len(cut)-1]; last >= 0xd800 && last <= 0xdbff {
		cut = cut[:len(cut)-1] // 末尾是半个代理对：丢掉，免得变成乱码
	}
	return string(utf16.Decode(cut)) + "…"
}
