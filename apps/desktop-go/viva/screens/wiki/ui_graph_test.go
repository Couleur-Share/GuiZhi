package wiki

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 关系图谱（L4）：原生画布的渲染（节点色、标签随缩放出现）、点击命中、拖动平移、滚轮以指针为中心缩放、截断提示、读取失败。

// openGraph 切到图谱视图并确认模型已读出。
func (r *uiRig) openGraph() *GraphModel {
	r.t.Helper()
	r.clickButton(locales.WikiViewGraph())
	m := r.store.View().Graph
	if m == nil || m.Empty() {
		r.t.Fatalf("图谱应已读出：%+v", r.store.View())
	}
	return m
}

// nodeXY 是页面节点此刻在窗口里的位置。
func (r *uiRig) nodeXY(m *GraphModel, pageID string) (float32, float32) {
	r.t.Helper()
	i := m.IndexOf(pageID)
	if i < 0 {
		r.t.Fatalf("图谱里没有节点 %q", pageID)
	}
	return r.screen.graph.nodeScreen(m, i)
}

// pixel 读窗口截图上一个点的颜色。
func (r *uiRig) pixel(x, y float32) (uint8, uint8, uint8) {
	r.t.Helper()
	img, err := r.session.Image()
	if err != nil {
		r.t.Fatalf("截图失败：%v", err)
	}
	c := img.RGBAAt(int(math.Round(float64(x))), int(math.Round(float64(y))))
	return c.R, c.G, c.B
}

func near(a, b uint8) bool { return math.Abs(float64(a)-float64(b)) <= 4 }

// drag 在 (x0,y0) 按下、挪到 (x1,y1) 再抬起。
func (r *uiRig) drag(x0, y0, x1, y1 float32) {
	r.t.Helper()
	if err := r.session.Press(x0, y0); err != nil {
		r.t.Fatal(err)
	}
	r.frames(2)
	if err := r.session.Move(x1, y1); err != nil {
		r.t.Fatal(err)
	}
	r.frames(2)
	if err := r.session.Release(x1, y1); err != nil {
		r.t.Fatal(err)
	}
	r.frames(3)
}

func TestUIGraphPaintsNodesByKind(t *testing.T) {
	r := newUIRig(t, withSeed)
	m := r.openGraph()
	if len(m.Nodes) != 3 || len(m.Edges) != 1 {
		t.Fatalf("三个节点一条边：%d %d", len(m.Nodes), len(m.Edges))
	}
	// 主题蓝 #3b82f6（Alpha、Beta），实体绿 #10b981（Gamma）。
	for _, c := range []struct {
		id      string
		r, g, b uint8
	}{{"alpha", 0x3b, 0x82, 0xf6}, {"beta", 0x3b, 0x82, 0xf6}, {"gamma", 0x10, 0xb9, 0x81}} {
		x, y := r.nodeXY(m, c.id)
		pr, pg, pb := r.pixel(x, y)
		if !near(pr, c.r) || !near(pg, c.g) || !near(pb, c.b) {
			t.Errorf("节点 %s 的颜色不对：got (%d,%d,%d) want (%d,%d,%d)", c.id, pr, pg, pb, c.r, c.g, c.b)
		}
	}
	// 初始缩放下不画标题标签；图例（主题、实体、概念）在左下。
	if r.hasText("Alpha") || r.hasText("Gamma") {
		t.Errorf("缩放不到 1.4 倍不该画标签：%q", r.session.Texts())
	}
	for _, name := range []string{locales.WikiKindTopic(), locales.WikiKindEntity(), locales.WikiKindConcept()} {
		if !r.hasText(name) {
			t.Errorf("图例缺少「%s」", name)
		}
	}
}

func TestUIGraphLabelsAppearWhenZoomedIn(t *testing.T) {
	r := newUIRig(t, withSeed)
	m := r.openGraph()
	x, y := r.nodeXY(m, "alpha")
	r.scrollBy(x, y, -500) // exp(0.75) ≈ 2.1 倍
	if z := r.screen.graph.zoom; z < LabelVisibleScale {
		t.Fatalf("应放大到标签可见的倍数：%.2f", z)
	}
	// Gamma 离得远，放大后可能已在画布之外（视口外的节点不画）：只断言留在视野里的两个。
	for _, title := range []string{"Alpha", "Beta"} {
		if !r.hasText(title) {
			t.Errorf("放大后应画标签「%s」：%q", title, r.session.Texts())
		}
	}
}

func TestUIGraphSelectedNodeHasLabelAtDefaultZoom(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton("Alpha")
	r.openGraph()
	if !r.hasText("Alpha") {
		t.Fatalf("选中节点任何缩放下都有标签：%q", r.session.Texts())
	}
	if r.hasText("Gamma") {
		t.Error("未选中的节点在默认缩放下不画标签")
	}
}

func TestUIGraphClickNodeOpensPageInCatalog(t *testing.T) {
	r := newUIRig(t, withSeed)
	m := r.openGraph()
	x, y := r.nodeXY(m, "gamma")
	r.clickAt(x, y)
	v := r.store.View()
	if v.Mode != ViewCatalog || v.SelectedPageID != "gamma" {
		t.Fatalf("点节点应回到目录并打开 gamma：mode=%v selected=%q", v.Mode, v.SelectedPageID)
	}
	if !r.has(gfx.RoleHeading, "Gamma") {
		t.Fatalf("详情应显示 Gamma：%q", r.session.Texts())
	}
}

func TestUIGraphClickOnEmptyCanvasDoesNothing(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.openGraph()
	// 画布左上角附近远离所有节点。
	r.clickAt(r.screen.graph.rect.X+30, r.screen.graph.rect.Y+120)
	if v := r.store.View(); v.Mode != ViewGraph || v.SelectedPageID != "" {
		t.Fatalf("点空白不该跳转：%+v", v.Mode)
	}
}

func TestUIGraphDragPansWithoutOpeningPage(t *testing.T) {
	r := newUIRig(t, withSeed)
	m := r.openGraph()
	x0, y0 := r.nodeXY(m, "alpha")
	// 在节点上按下并拖开：是平移，不是点击。
	r.drag(x0, y0, x0+60, y0+40)
	g := &r.screen.graph
	if math.Abs(g.panX-60) > 1 || math.Abs(g.panY-40) > 1 {
		t.Fatalf("平移量应跟着拖动：(%.1f, %.1f)", g.panX, g.panY)
	}
	if v := r.store.View(); v.Mode != ViewGraph || v.SelectedPageID != "" {
		t.Fatalf("拖动不该打开页面：%v %q", v.Mode, v.SelectedPageID)
	}
	x1, y1 := r.nodeXY(m, "alpha")
	if math.Abs(float64(x1-x0)-60) > 1 || math.Abs(float64(y1-y0)-40) > 1 {
		t.Fatalf("节点应整体平移：(%.1f,%.1f) → (%.1f,%.1f)", x0, y0, x1, y1)
	}
	// 平移之后节点在新位置上仍可点击。
	r.clickAt(x1, y1)
	if r.store.View().SelectedPageID != "alpha" {
		t.Fatalf("平移后点新位置应命中 alpha：%q", r.store.View().SelectedPageID)
	}
}

func TestUIGraphTinyMoveStillCountsAsClick(t *testing.T) {
	r := newUIRig(t, withSeed)
	m := r.openGraph()
	x, y := r.nodeXY(m, "beta")
	r.drag(x, y, x+2, y+1) // 不超过 3 px 的抖动
	if r.store.View().SelectedPageID != "beta" {
		t.Fatalf("极小的抖动仍是点击：%q", r.store.View().SelectedPageID)
	}
}

func TestUIGraphWheelZoomKeepsPointerFixed(t *testing.T) {
	r := newUIRig(t, withSeed)
	m := r.openGraph()
	x0, y0 := r.nodeXY(m, "beta")
	r.scrollBy(x0, y0, -400)
	x1, y1 := r.nodeXY(m, "beta")
	if math.Abs(float64(x1-x0)) > 0.5 || math.Abs(float64(y1-y0)) > 0.5 {
		t.Fatalf("指针下的节点缩放时不该移动：(%.1f,%.1f) → (%.1f,%.1f)", x0, y0, x1, y1)
	}
	if r.screen.graph.zoom <= 1 {
		t.Fatalf("向上滚应放大：%.2f", r.screen.graph.zoom)
	}
	r.scrollBy(x0, y0, 4000)
	if z := r.screen.graph.zoom; math.Abs(z-minZoom) > 1e-9 {
		t.Fatalf("缩小应止于下限 %v：%v", minZoom, z)
	}
	r.scrollBy(x0, y0, -40000)
	if z := r.screen.graph.zoom; math.Abs(z-maxZoom) > 1e-9 {
		t.Fatalf("放大应止于上限 %v：%v", maxZoom, z)
	}
}

func TestUIGraphWheelOutsideCanvasIsNotTaken(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.openGraph()
	// 左侧栏上方：不属于画布，滚轮交还给页面。
	if res := r.screen.GraphWheel(gfx.ScrollEvent{X: 20, Y: 400, DeltaY: -300}); res.Handled {
		t.Fatal("画布之外的滚轮不该被图谱接走")
	}
	if r.screen.graph.zoom != 1 {
		t.Fatalf("缩放不该变：%v", r.screen.graph.zoom)
	}
}

func TestUIGraphWheelIgnoredWhenGraphNotShown(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.frames(2)
	if res := r.screen.GraphWheel(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: -300}); res.Handled {
		t.Fatal("目录视图下滚轮不该被图谱接走")
	}
	r.openGraph()
	r.clickButton(locales.WikiViewCatalog())
	r.frames(2)
	if res := r.screen.GraphWheel(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: -300}); res.Handled {
		t.Fatal("离开图谱之后滚轮不该再被接走")
	}
}

func TestUIGraphViewpointResetsOnRemount(t *testing.T) {
	r := newUIRig(t, withSeed)
	m := r.openGraph()
	x, y := r.nodeXY(m, "alpha")
	r.scrollBy(x, y, -500)
	r.drag(x, y, x+30, y)
	if r.screen.graph.zoom == 1 {
		t.Fatal("前置：应已放大")
	}
	r.clickButton(locales.WikiViewCatalog())
	r.clickButton(locales.WikiViewGraph())
	if g := r.screen.graph; g.zoom != 1 || g.panX != 0 || g.panY != 0 {
		t.Fatalf("重新进入图谱视角应回到初始：%+v", g)
	}
}

func TestUIGraphTruncatedShowsBadge(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) {
		for i := 0; i < 305; i++ {
			id := fmt.Sprintf("p%03d", i)
			r.svc.PutPage(services.WikiPage{ID: id, Title: id, Body: id})
		}
	})
	m := r.openGraph()
	if !m.Truncated() {
		t.Fatalf("305 页应被截断：%d / %d", len(m.Nodes), m.Total)
	}
	if !r.hasText(locales.WikiGraphTruncated(len(m.Nodes), m.Total)) {
		t.Fatalf("应有截断提示：%q", r.session.Texts())
	}
}

func TestUIGraphLoadFailureShowsRetry(t *testing.T) {
	r := newUIRig(t, withSeed, func(r *uiRig) { r.svc.Fail(servicestest.WikiOpGraph, errors.New("graph boom")) })
	r.clickButton(locales.WikiViewGraph())
	if r.store.View().Graph != nil || !r.hasButton(locales.CommonRetry()) {
		t.Fatalf("图谱读取失败应有重试，不能画成空图：%q", r.session.Texts())
	}
	r.svc.Fail(servicestest.WikiOpGraph, nil)
	r.clickButton(locales.CommonRetry())
	if m := r.store.View().Graph; m == nil || m.Empty() {
		t.Fatalf("重试成功后应有图谱：%q", r.session.Texts())
	}
}
