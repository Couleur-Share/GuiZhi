package wiki

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 关系图谱的绘制与交互（渲染层 WikiGraphView，那边用 react-force-graph-2d）。
// 这里用 Viva 的画布直接画：布局由 BuildGraphModel 在单位空间 [-1, 1]² 里算好，界面只管把它映射到画布，
// 缩放（滚轮）与平移（拖动）改的是映射，不重算布局。
//
// 与渲染层一致的地方：节点按类型着色（主题蓝、实体绿、概念橙），半径 4.5（选中 6，选中还描一圈 1.5 的标签色边），
// 边是半透明灰线加指向目标的箭头，缩放到 1.4 倍以上（或节点被选中）才画标题标签（截成 14 个字），点击节点回到目录并打开那一页，
// 图例在左下、被截断的提示在左上。
// 已知差异：没有悬停时的节点标题气泡（Viva 没有指针位置的读取接口，每个节点单独登记命中区又会拦住拖动）；节点不能被拖动。

// 渲染层的节点半径、命中半径、边与箭头尺寸（图坐标，随缩放一起放大）。
const (
	nodeRadius         = 4.5
	nodeRadiusSelected = 6
	nodeHitRadius      = 8
	selectedStroke     = 1.5
	arrowLength        = 3
	arrowBackoff       = 4 // react-force-graph 把箭头尖放在离目标节点中心 nodeRelSize(4) 处
	labelFontPx        = 12
	labelGap           = 2

	// 画布到单位空间的比例：单位圆占画布短边的这一部分。
	graphFill = 0.45

	minZoom, maxZoom = 0.1, 20
	wheelSensitivity = 0.0015
	// clickSlop 是按下到抬起之间指针挪动不超过它就算点击，否则是拖动平移。
	clickSlop = 3
)

// graphEdgeColor 是边与箭头的颜色（rgba(148,163,184,0.35)）。
var graphEdgeColor = tokens.RGBA(148, 163, 184, 0.35)

// graphKindColors 是类型的节点色，下标是 KindIndex（主题、实体、概念）。
var graphKindColors = [3]tokens.Color{
	tokens.RGBA(0x3b, 0x82, 0xf6, 1),
	tokens.RGBA(0x10, 0xb9, 0x81, 1),
	tokens.RGBA(0xf5, 0x9e, 0x0b, 1),
}

// graphUI 是图谱的界面状态：缩放与平移、画布矩形、拖动与「是否在画面上」的记号。
type graphUI struct {
	zoom       float64
	panX, panY float64 // 单位圆心相对画布中心的平移（屏幕像素）
	rect       gfx.Rect

	dragging bool
	moved    bool
	startX   float32
	startY   float32
	panX0    float64
	panY0    float64

	mounted bool // 上一帧画了图谱：卸载再挂载时视角回到初始（渲染层每次进入图谱视图都是新挂载的画布）
	drawn   bool // 这一帧画了图谱（EndFrame 据此更新 live 与 mounted）
	live    bool // 上一整帧画了图谱：滚轮据此判断要不要接
}

// reset 让视角回到初始（缩放 1、居中）。
func (g *graphUI) reset() {
	g.zoom, g.panX, g.panY = 1, 0, 0
	g.dragging, g.moved = false, false
}

// xform 是单位空间到窗口坐标的映射。
type xform struct{ cx, cy, scale float64 }

// xform 取当前视角在画布矩形上的映射：单位圆占短边的 45%，再乘缩放，加平移。
func (g *graphUI) xform() xform {
	r := g.rect
	base := math.Min(float64(r.Width), float64(r.Height)) * graphFill
	return xform{cx: float64(r.X+r.Width/2) + g.panX, cy: float64(r.Y+r.Height/2) + g.panY, scale: base * g.zoom}
}

func (x xform) screen(ux, uy float64) (float64, float64) { return x.cx + ux*x.scale, x.cy + uy*x.scale }
func (x xform) unit(sx, sy float64) (float64, float64) {
	return (sx - x.cx) / x.scale, (sy - x.cy) / x.scale
}

// nodeScreen 是第 i 个节点此刻在窗口里的位置（测试用：按它去点节点）。
func (g *graphUI) nodeScreen(m *GraphModel, i int) (float32, float32) {
	x, y := g.xform().screen(m.Nodes[i].X, m.Nodes[i].Y)
	return float32(x), float32(y)
}

// GraphZoom 是图谱此刻的缩放倍数（界面线程读；Root 的接线测试用它确认滚轮确实缩放了画布）。
func (s *Screen) GraphZoom() float64 { return s.graph.zoom }

// GraphWheel 是窗口级的滚轮钩子（Look.WheelHook）：指针在图谱画布上时，滚轮以指针处为中心缩放，不再滚动背后的内容。
// 事件坐标是窗口坐标；不在画布上（或图谱不在画面上）返回零值，交还给滚动区。
func (s *Screen) GraphWheel(e gfx.ScrollEvent) gfx.EventResult {
	g := &s.graph
	r := g.rect
	if !g.live || r.Width <= 0 || r.Height <= 0 || e.DeltaY == 0 || e.X < r.X || e.X > r.X+r.Width || e.Y < r.Y || e.Y > r.Y+r.Height {
		return gfx.EventResult{}
	}
	next := math.Min(math.Max(g.zoom*math.Exp(-float64(e.DeltaY)*wheelSensitivity), minZoom), maxZoom)
	// 让指针下的那一点不动：新平移 = 指针相对中心的偏移 − (旧偏移 − 旧平移) × 缩放比。
	dx, dy := float64(e.X-(r.X+r.Width/2)), float64(e.Y-(r.Y+r.Height/2))
	k := next / g.zoom
	g.panX, g.panY = dx-(dx-g.panX)*k, dy-(dy-g.panY)*k
	g.zoom = next
	return gfx.EventResult{Handled: true, Repaint: true}
}

// graphView 是关系图谱区（WikiGraphView）：读取中、读取失败、还没有页面关系，或画布加图例与截断提示。
func (s *Screen) graphView(c ui.C, v View) {
	l := s.Look
	t := l.T
	g := &s.graph
	g.drawn = true
	if !g.mounted {
		g.mounted = true
		g.reset()
		if v.Graph == nil && !v.GraphLoading && v.GraphError == "" {
			s.spawn(func() { s.Store.LoadGraph() })
		}
	}
	c = c.Key("graph")
	switch {
	case v.Graph == nil && v.GraphError != "":
		g.rect = gfx.Rect{}
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Justify(ui.Center).Items(ui.Center).Children(func() {
			if l.LoadErrorState(c.Key("error"), v.GraphError) {
				s.spawn(func() { s.Store.LoadGraph() })
			}
		})
	case v.Graph == nil:
		g.rect = gfx.Rect{}
		ui.Row(c).Grow(1).MinWidth(0).FillHeight().Justify(ui.Center).Items(ui.Center).Children(func() {
			l.Spinner(c.Key("spin"), l.Rem(1), t.MutedForeground)
		})
	case v.Graph.Empty():
		g.rect = gfx.Rect{}
		ui.Row(c).Grow(1).MinWidth(0).FillHeight().Justify(ui.Center).Items(ui.Center).PadSides(0, l.Space(6), 0, l.Space(6)).Children(func() {
			l.Text(c.Key("empty"), locales.WikiGraphEmpty(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped())
		})
	default:
		s.graphCanvas(c, v)
	}
}

// graphCanvas 是画布：relative min-h-0 flex-1 overflow-hidden，铺满的一块绘制区，上面叠图例与截断提示。
func (s *Screen) graphCanvas(c ui.C, v View) {
	g := &s.graph
	model := v.Graph
	ui.Stack(c).Grow(1).MinWidth(0).FillHeight().Clip().Children(func() {
		id := ui.ID(c.Key("canvas"))
		cursor := gfx.PointerCursorDefault
		if g.dragging && g.moved {
			cursor = gfx.PointerCursorGrabbing
		}
		ui.Row(c.Key("canvas-box")).Absolute().Inset(0).
			HitWith(id, locales.WikiViewGraph(), gfx.HitOptions{Role: gfx.RoleGroup, Cursor: cursor}).
			Draw(func(d *ui.DrawContext) {
				g.rect = d.Rect
				s.graphPointer(d, id, model)
				s.paintGraph(d, model, v.SelectedPageID)
			})
		if model.Truncated() {
			s.truncatedBadge(c, model)
		}
		s.graphLegend(c)
	})
}

// graphPointer 处理画布上的指针事件（只在事件遍里有）：按下记下起点；拖动平移；抬起时没怎么动就是点击，点中节点就回到目录并打开那一页。
func (s *Screen) graphPointer(d *ui.DrawContext, id gfx.ID, model *GraphModel) {
	g := &s.graph
	ev, ok := d.Canvas.PointerOn(id)
	if !ok {
		return
	}
	switch ev.Kind {
	case gfx.PointerDown:
		if ev.Button != gfx.PointerButtonPrimary {
			return
		}
		g.dragging, g.moved = true, false
		g.startX, g.startY, g.panX0, g.panY0 = ev.X, ev.Y, g.panX, g.panY
		d.Canvas.Consume()
	case gfx.PointerMove:
		if !g.dragging {
			return
		}
		dx, dy := ev.X-g.startX, ev.Y-g.startY
		if !g.moved && math.Hypot(float64(dx), float64(dy)) > clickSlop {
			g.moved = true
		}
		if g.moved {
			g.panX, g.panY = g.panX0+float64(dx), g.panY0+float64(dy)
		}
		d.Canvas.Consume()
	case gfx.PointerUp:
		if !g.dragging {
			return
		}
		clicked := !g.moved
		g.dragging, g.moved = false, false
		d.Canvas.Consume()
		if !clicked {
			return
		}
		x := g.xform()
		ux, uy := x.unit(float64(ev.X), float64(ev.Y))
		if i := model.NodeAt(ux, uy, nodeHitRadius/(x.scale/g.zoom)); i >= 0 {
			pageID := model.Nodes[i].ID
			d.Canvas.AfterPaint(func() { s.spawn(func() { s.Store.OpenFromGraph(pageID) }) })
		}
	}
}

// paintGraph 画整张图：先是所有边（一条描边路径）与箭头（一条填充路径），再是节点与标签。
func (s *Screen) paintGraph(d *ui.DrawContext, m *GraphModel, selectedID string) {
	l := s.Look
	g := &s.graph
	x := g.xform()
	z := g.zoom
	r := d.Rect
	d.Canvas.Clip(r)
	defer d.Canvas.Unclip()

	// 边：窗口坐标换成相对画布矩形的坐标交给路径；视图框取画布大小。
	edges := d.Canvas.PathBuilder()
	for _, e := range m.Edges {
		ax, ay := x.screen(m.Nodes[e.From].X, m.Nodes[e.From].Y)
		bx, by := x.screen(m.Nodes[e.To].X, m.Nodes[e.To].Y)
		edges.MoveTo(float32(ax)-r.X, float32(ay)-r.Y)
		edges.LineTo(float32(bx)-r.X, float32(by)-r.Y)
	}
	edgeColor := d.Fade(l.Col(graphEdgeColor))
	if edges.Len() > 0 {
		d.Canvas.DrawPath(edges.Path(r.Width, r.Height), r, gfx.PathStyle{Stroke: edgeColor, StrokeWidth: 1})
	}
	arrows := d.Canvas.PathBuilder()
	for _, e := range m.Edges {
		ax, ay := x.screen(m.Nodes[e.From].X, m.Nodes[e.From].Y)
		bx, by := x.screen(m.Nodes[e.To].X, m.Nodes[e.To].Y)
		dx, dy := bx-ax, by-ay
		dist := math.Hypot(dx, dy)
		length := arrowLength * z
		if dist < 1e-6 || dist < length+arrowBackoff*z {
			continue
		}
		ux, uy := dx/dist, dy/dist
		tipX, tipY := bx-ux*arrowBackoff*z, by-uy*arrowBackoff*z
		baseX, baseY := tipX-ux*length, tipY-uy*length
		half := length / 1.6 / 2
		arrows.MoveTo(float32(tipX)-r.X, float32(tipY)-r.Y)
		arrows.LineTo(float32(baseX-uy*half)-r.X, float32(baseY+ux*half)-r.Y)
		arrows.LineTo(float32(baseX+uy*half)-r.X, float32(baseY-ux*half)-r.Y)
		arrows.Close()
	}
	if arrows.Len() > 0 {
		d.Canvas.DrawPath(arrows.Path(r.Width, r.Height), r, gfx.PathStyle{Fill: edgeColor})
	}

	labelColor := tokens.RGBA(51, 65, 85, 0.85)
	if l.Dark {
		labelColor = tokens.RGBA(226, 232, 240, 0.85)
	}
	ink := d.Fade(l.Col(labelColor))
	for i := range m.Nodes {
		n := &m.Nodes[i]
		sx, sy := x.screen(n.X, n.Y)
		selected := n.ID == selectedID
		radius := nodeRadius
		if selected {
			radius = nodeRadiusSelected
		}
		pr := math.Max(float64(radius)*z, 1)
		if sx < float64(r.X)-pr || sx > float64(r.X+r.Width)+pr || sy < float64(r.Y)-pr || sy > float64(r.Y+r.Height)+pr {
			continue
		}
		box := gfx.Rect{X: float32(sx - pr), Y: float32(sy - pr), Width: float32(2 * pr), Height: float32(2 * pr)}
		d.Canvas.Rounded(box, float32(pr), d.Fade(l.Col(graphKindColors[KindIndex(n.Kind)])))
		if selected {
			d.Canvas.RoundedOutline(box, float32(pr), float32(math.Max(selectedStroke*z, 0.5)), ink)
		}
		if z >= LabelVisibleScale || selected {
			text := TruncateLabel(n.Title)
			w := d.Canvas.TextWidth(text, labelFontPx)
			label := gfx.Rect{X: float32(sx) - w/2, Y: float32(sy + pr + labelGap*z), Width: w + 2, Height: labelFontPx * 1.4}
			d.Canvas.Label(text, label, gfx.TextStyle{Size: labelFontPx, Weight: gfx.WeightRegular, Color: ink})
		}
	}
}

// truncatedBadge 是左上角的「按连接度显示 N / M 个页面」：pointer-events-none absolute left-3 top-3 rounded-lg border border-border/70
// bg-background/85 px-2.5 py-1 text-[11px] text-muted-foreground。
func (s *Screen) truncatedBadge(c ui.C, m *GraphModel) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("truncated")).Absolute().Top(l.Space(3)).Left(l.Space(3)).Items(ui.Center).
		PadSides(l.Space(1)+1, l.Space(2.5)+1, l.Space(1)+1, l.Space(2.5)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.85), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: s.radiusLG()})
		}).Children(func() {
		l.TextPx(c.Key("text"), locales.WikiGraphTruncated(len(m.Nodes), m.Total), 11, px11Line, kit.WeightNormal, t.MutedForeground)
	})
}

// graphLegend 是左下角的图例：pointer-events-none absolute bottom-3 left-3 flex items-center gap-3 rounded-lg border border-border/60
// bg-background/70 px-3 py-1.5，每项是一枚 10px 的色点加类型名（text-[11px]，间距 1.5）。
func (s *Screen) graphLegend(c ui.C) {
	l := s.Look
	t := l.T
	names := [3]string{locales.WikiKindTopic(), locales.WikiKindEntity(), locales.WikiKindConcept()}
	ui.Row(c.Key("legend")).Absolute().Bottom(l.Space(3)).Left(l.Space(3)).Items(ui.Center).Gap(l.Space(3)).
		PadSides(l.Space(1.5)+1, l.Space(3)+1, l.Space(1.5)+1, l.Space(3)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.7), Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: s.radiusLG()})
		}).Children(func() {
		for i, name := range names {
			color := graphKindColors[i]
			ui.Row(c.Key("legend-" + name)).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
				ui.Slot(c.Key("dot")).Size(l.Space(2.5), l.Space(2.5)).Shrink(0).Draw(func(d *ui.DrawContext) {
					d.Canvas.Rounded(d.Rect, d.Rect.Width/2, d.Fade(l.Col(color)))
				})
				l.TextPx(c.Key("text"), name, 11, px11Line, kit.WeightNormal, t.MutedForeground)
			})
		}
	})
}
