package reader

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// 台账 B-068：阅读区的起点与尺寸带小数时，宿主把「真实起点减去子窗口取整起点」与精确尺寸发给页面。

func TestLayoutForFractionalReaderArea(t *testing.T) {
	tests := []struct {
		name   string
		bounds gfx.Rect
		scale  float32
		want   Layout
	}{
		// 台账 B-068 的实测值：根字号 18 px、1280×800 里阅读区是 698, 222.75, 582×577.25。
		// 子窗口取整成 698, 223 → 偏移 (0, 222.75-223=-0.25)；底边 800.00 本来就在像素上。
		{"large font 1280x800", gfx.Rect{X: 698, Y: 222.75, Width: 582, Height: 577.25}, 1, Layout{DX: 0, DY: -0.25, Width: 582, Height: 577.25}},
		// 四条边都在像素上：零值（不发消息）。
		{"integer at 100%", gfx.Rect{X: 10, Y: 20, Width: 600, Height: 400}, 1, Layout{}},
		{"integer at 200%", gfx.Rect{X: 10, Y: 20, Width: 600, Height: 400}, 2, Layout{}},
		// 小数的逻辑坐标在缩放之后恰好落在整数设备像素上：150% 下 100.666…·1.5 = 151（float32 噪声在容差之内）。
		{"fraction on device grid", gfx.Rect{X: 400.6667, Y: 100, Width: 300, Height: 200}, 1.5, Layout{}},
		{"half device pixel on grid", gfx.Rect{X: 100.5, Y: 7.5, Width: 200, Height: 100}, 2, Layout{}},
		// 整数的逻辑坐标在 125% 下也可能不在像素上：10·1.25 = 12.5 → 取整 13，偏移 (12.5-13)/1.25 = -0.4；右边 762.5 → 763 同样不对齐。
		{"integer logical at 125%", gfx.Rect{X: 10, Y: 20, Width: 600, Height: 400}, 1.25, Layout{DX: -0.4, DY: 0, Width: 600, Height: 400}},
		// 只有尺寸带小数：起点对齐（偏移为 0），但仍要告诉页面精确尺寸，窗口比内容多出的那一点由页面的裁切负责。
		{"fractional size only", gfx.Rect{X: 100, Y: 100, Width: 200.5, Height: 100}, 1, Layout{DX: 0, DY: 0, Width: 200.5, Height: 100}},
		// 四舍五入的平局向远离零的方向（Viva 的 round）：0.5 → 1，偏移为负；-0.5 → -1，偏移为正。
		{"tie rounds up", gfx.Rect{X: 0.5, Y: 0, Width: 100, Height: 100}, 1, Layout{DX: -0.5, DY: 0, Width: 100, Height: 100}},
		{"tie rounds away from zero", gfx.Rect{X: -0.5, Y: 0, Width: 100, Height: 100}, 1, Layout{DX: 0.5, DY: 0, Width: 100, Height: 100}},
		// 取整向下：222.25 → 222，偏移为正。
		{"rounds down", gfx.Rect{X: 0, Y: 222.25, Width: 100, Height: 100}, 1, Layout{DX: 0, DY: 0.25, Width: 100, Height: 100}},
		// 没有缩放信息（0 或负）按 1。
		{"no scale means 1", gfx.Rect{X: 0, Y: 222.75, Width: 100, Height: 100.25}, 0, Layout{DX: 0, DY: -0.25, Width: 100, Height: 100.25}},
		// 尺寸为零的视图 Canvas.NativeView 不摆：没有版面。
		{"empty bounds", gfx.Rect{X: 0.5, Y: 0.5, Width: 0, Height: 10}, 1, Layout{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LayoutFor(tt.bounds, tt.scale)
			if !layoutNear(got, tt.want) {
				t.Errorf("LayoutFor(%v, %v) = %+v，期望 %+v", tt.bounds, tt.scale, got, tt.want)
			}
			if got.IsZero() != tt.want.IsZero() {
				t.Errorf("IsZero = %v，期望 %v", got.IsZero(), tt.want.IsZero())
			}
		})
	}
}

// 偏移加上子窗口的取整起点必须回到真实起点：这是页面把内容摆回正确位置的全部依据（符号、缩放换算都在这里被锁住）。
func TestLayoutOffsetPlusWindowOriginIsTheTrueOrigin(t *testing.T) {
	for _, scale := range []float32{1, 1.25, 1.5, 1.75, 2, 2.5} {
		for _, x := range []float32{0, 0.1, 10, 222.75, 698.333, 1279.9} {
			bounds := gfx.Rect{X: x, Y: x / 2, Width: 100.4, Height: 50.6}
			layout := LayoutFor(bounds, scale)
			if layout.IsZero() {
				continue
			}
			left := float64(vivaRound(bounds.X*scale)) / float64(scale)
			top := float64(vivaRound(bounds.Y*scale)) / float64(scale)
			if math.Abs(left+layout.DX-float64(bounds.X)) > 1e-4 || math.Abs(top+layout.DY-float64(bounds.Y)) > 1e-4 {
				t.Errorf("scale=%v x=%v：窗口起点 (%v, %v) + 偏移 (%v, %v) ≠ 真实起点 (%v, %v)", scale, x, left, top, layout.DX, layout.DY, bounds.X, bounds.Y)
			}
			if math.Abs(layout.DX) > 0.5/float64(scale)+1e-6 || math.Abs(layout.DY) > 0.5/float64(scale)+1e-6 {
				t.Errorf("scale=%v x=%v：偏移 (%v, %v) 超过半个设备像素", scale, x, layout.DX, layout.DY)
			}
			if err := layout.Validate(); err != nil {
				t.Errorf("scale=%v x=%v：算出的版面不合规：%v", scale, x, err)
			}
		}
	}
}

func TestLayoutValidate(t *testing.T) {
	ok := Layout{DX: -0.25, DY: 0.5, Width: 582, Height: 577.25}
	if err := ok.Validate(); err != nil {
		t.Fatalf("正常版面被拒绝：%v", err)
	}
	bad := map[string]Layout{
		"NaN 偏移":  {DX: math.NaN(), Width: 1, Height: 1},
		"无穷尺寸":    {Width: math.Inf(1), Height: 1},
		"偏移过大":    {DX: 1.5, Width: 1, Height: 1},
		"垂直偏移过大":  {DY: -2, Width: 1, Height: 1},
		"零宽":      {Width: 0, Height: 1},
		"负高":      {Width: 1, Height: -1},
		"尺寸超出上限":  {Width: maxLayoutSize + 1, Height: 1},
		"高度超出上限":  {Width: 1, Height: maxLayoutSize + 1},
		"零值不是真版面": {},
	}
	for name, l := range bad {
		if err := l.Validate(); err == nil {
			t.Errorf("%s：应该被拒绝", name)
		}
	}
}

func TestLayoutMessageEncoding(t *testing.T) {
	text, err := encodeMessage(layoutMessage{T: "layout", Layout: &Layout{DX: 0, DY: -0.25, Width: 582, Height: 577.25}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"t":"layout","layout":{"dx":0,"dy":-0.25,"width":582,"height":577.25}}`; text != want {
		t.Errorf("编码 = %s，期望 %s", text, want)
	}
	// nil 版面编码成 null（页面据此回到满视口），不能省略字段：页面按 `layout: null` 与「缺字段」区分。
	text, err = encodeMessage(layoutMessage{T: "layout"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"t":"layout","layout":null}`; text != want {
		t.Errorf("清除 = %s，期望 %s", text, want)
	}
}

// ---- 宿主的差量发送 ----

// frameAt 以 bounds 与显示缩放画一帧（rig.frame 固定用整数的 testBounds 与 100% 缩放）。
func (r *rig) frameAt(bounds gfx.Rect, scale float32) {
	r.surface.scale = scale
	r.host.Frame(r.surface, bounds, r.req)
}

func layoutOf(t *testing.T, p *fakePage, i int) map[string]any {
	t.Helper()
	m := p.msg(t, i)
	if m["t"] != "layout" {
		t.Fatalf("第 %d 条不是 layout：%v", i, m)
	}
	if m["layout"] == nil {
		return nil
	}
	layout, ok := m["layout"].(map[string]any)
	if !ok {
		t.Fatalf("layout 字段形状不对：%v", m)
	}
	return layout
}

var fractionalBounds = gfx.Rect{X: 698, Y: 222.75, Width: 582, Height: 577.25}

func TestAlignedReaderAreaSendsNoLayoutMessage(t *testing.T) {
	r := newRig(t)
	p := r.ready() // testBounds 是整数、缩放 100%：四条边都在像素上
	for range 3 {
		r.frame()
	}
	if got, want := p.kinds(t), []string{"init", "view"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("落在设备像素上的阅读区不该有 layout 消息：%v", got)
	}
}

func TestFractionalReaderAreaSendsLayoutBeforeTheView(t *testing.T) {
	r := newRig(t)
	r.frameAt(fractionalBounds, 1)
	p := r.page()
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	if got, want := p.kinds(t), []string{"init", "layout", "view"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("消息顺序 %v，期望 %v（版面先于内容，第一份内容渲染时根节点已在位）", got, want)
	}
	layout := layoutOf(t, p, 1)
	if layout["dx"] != 0.0 || layout["dy"] != -0.25 || layout["width"] != 582.0 || layout["height"] != 577.25 {
		t.Errorf("layout = %v", layout)
	}
	if len(p.places) != 1 || p.places[0] != fractionalBounds {
		t.Errorf("子窗口摆放必须仍是原始 bounds（取整留给 Viva，宿主不向外扩大）：%v", p.places)
	}
}

func TestLayoutIsPostedOnlyWhenItChanges(t *testing.T) {
	r := newRig(t)
	r.frameAt(fractionalBounds, 1)
	p := r.page()
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	n := len(p.posts)
	for range 3 {
		r.frameAt(fractionalBounds, 1)
	}
	if len(p.posts) != n {
		t.Fatalf("版面没变不该重发：多了 %v", p.posts[n:])
	}
	// 窗口缩放：起点与尺寸变了，发一条新的。
	moved := gfx.Rect{X: 698, Y: 222.25, Width: 582, Height: 577.75}
	r.frameAt(moved, 1)
	r.frameAt(moved, 1)
	if got := p.kinds(t)[n:]; !reflect.DeepEqual(got, []string{"layout"}) {
		t.Fatalf("版面变了只发一条 layout：%v", got)
	}
	if layout := layoutOf(t, p, n); layout["dy"] != 0.25 || layout["height"] != 577.75 {
		t.Errorf("layout = %v", layout)
	}
	// 回到整数像素：发一条 layout: null，页面回到满视口。
	r.frameAt(testBounds, 1)
	r.frameAt(testBounds, 1)
	if got := p.kinds(t)[n+1:]; !reflect.DeepEqual(got, []string{"layout"}) {
		t.Fatalf("回到像素上要发一条清除：%v", got)
	}
	if layoutOf(t, p, n+1) != nil {
		t.Errorf("清除消息的 layout 应为 null：%v", p.msg(t, n+1))
	}
	if len(p.posts) != n+2 {
		t.Errorf("清除之后不再重发：%v", p.posts[n+2:])
	}
}

// 整数的逻辑坐标在 125% 缩放下也可能不在设备像素上：版面必须按画布的缩放算，不能按「坐标是不是整数」判断。
func TestLayoutFollowsTheDisplayScale(t *testing.T) {
	r := newRig(t)
	r.frameAt(testBounds, 1.25)
	p := r.page()
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	if got, want := p.kinds(t), []string{"init", "layout", "view"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("125%% 缩放下整数坐标也要发 layout：%v", got)
	}
	if layout := layoutOf(t, p, 1); math.Abs(layout["dx"].(float64)+0.4) > 1e-6 {
		t.Errorf("dx = %v，期望 -0.4", layout["dx"])
	}
	// 换到 100% 的显示器（缩放变了、坐标没变）：清除。
	r.frameAt(testBounds, 1)
	if got := p.kinds(t); got[len(got)-1] != "layout" || layoutOf(t, p, len(got)-1) != nil {
		t.Errorf("缩放回到 100%% 要清除版面：%v", got)
	}
}

func TestLayoutIsResentAfterThePageHandshakesAgain(t *testing.T) {
	r := newRig(t)
	r.frameAt(fractionalBounds, 1)
	p := r.page()
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion}) // 页面被刷新：什么状态都没有
	want := []string{"init", "layout", "view", "init", "layout", "view"}
	if got := p.kinds(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("重新握手要从头发齐，包括版面：%v，期望 %v", got, want)
	}
}

func TestLayoutIsNotSentBeforeTheHandshake(t *testing.T) {
	r := newRig(t)
	r.frameAt(fractionalBounds, 1)
	r.frameAt(fractionalBounds, 1)
	if posts := r.page().posts; len(posts) != 0 {
		t.Fatalf("握手之前不能发消息：%v", posts)
	}
}

func TestInvalidLayoutFailsThePageInsteadOfPostingGarbage(t *testing.T) {
	for name, bounds := range map[string]gfx.Rect{
		"尺寸超出上限":  {X: 0.5, Y: 0, Width: 30000, Height: 10},
		"坐标不是有限数": {X: float32(math.NaN()), Y: 0, Width: 10, Height: 10},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.frameAt(bounds, 1)
			p := r.page()
			p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
			for _, kind := range p.kinds(t) {
				if kind == "layout" || kind == "view" {
					t.Fatalf("不合规的版面不该发给页面：%v", p.kinds(t))
				}
			}
			if r.host.Open() || r.host.Failure(r.req) == nil {
				t.Errorf("不合规的版面要按页面失败处理：open=%v failure=%v", r.host.Open(), r.host.Failure(r.req))
			}
			if len(r.diags) == 0 || !strings.Contains(r.diags[len(r.diags)-1], "版面") {
				t.Errorf("诊断 = %v", r.diags)
			}
		})
	}
}

func layoutNear(a, b Layout) bool {
	near := func(x, y float64) bool { return math.Abs(x-y) < 1e-4 }
	return near(a.DX, b.DX) && near(a.DY, b.DY) && near(a.Width, b.Width) && near(a.Height, b.Height)
}
