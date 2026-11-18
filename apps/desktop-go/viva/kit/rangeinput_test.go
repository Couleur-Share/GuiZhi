package kit

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 滑块 L4：指针与键盘取值逐项对照渲染层原生 <input type="range"> 的实测数据（台账 B-019）。
// 数据由 scripts/parity/tools/probe-range-pointer.mjs 在 React 参照里量出，存在 testdata/range-pointer-react.json：
// 两枚滑块（背景可见度 0–100 步长 1，虚化强度 0–50 步长 0.5）× 三种输入框宽度（400、313、200），
// 点击、拖动、抓取偏移、按在滑块头上、滑块头命中网格、键盘。

// rangeProbe 是探针 JSON 的形状（数值在 JSON 里都是字符串，原样取自 el.value）。
type rangeProbe struct {
	Sliders []struct {
		Min  string `json:"min"`
		Max  string `json:"max"`
		Step string `json:"step"`
		Runs []struct {
			Width  int `json:"width"`
			Clicks []struct {
				Dx    float64 `json:"dx"`
				Value string  `json:"value"`
			} `json:"clicks"`
			Drags []struct {
				To    float64 `json:"to"`
				Value string  `json:"value"`
			} `json:"drags"`
			Grab struct {
				DownAt float64 `json:"downAt"`
				MoveTo float64 `json:"moveTo"`
				Value  string  `json:"value"`
			} `json:"grab"`
			Nudge struct {
				DownAt float64 `json:"downAt"`
				MoveTo float64 `json:"moveTo"`
				Value  string  `json:"value"`
			} `json:"nudge"`
			ThumbPress []struct {
				At        float64 `json:"at"`
				AfterDown string  `json:"afterDown"`
				AfterUp   string  `json:"afterUp"`
			} `json:"thumbPress"`
			ThumbHit *struct {
				Dxs    []float64 `json:"dxs"`
				Dys    []float64 `json:"dys"`
				Mid    string    `json:"mid"`
				Inside [][]bool  `json:"inside"`
			} `json:"thumbHit"`
			Keys []struct {
				Key   string `json:"key"`
				Start string `json:"start"`
				From  string `json:"from"` // 按键之后的值（字段名沿用探针的写法）
			} `json:"keys"`
		} `json:"runs"`
	} `json:"sliders"`
}

func loadRangeProbe(t *testing.T) rangeProbe {
	t.Helper()
	data, err := os.ReadFile("testdata/range-pointer-react.json")
	if err != nil {
		t.Fatalf("读不到滑块探针数据：%v", err)
	}
	var p rangeProbe
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("滑块探针数据解析失败：%v", err)
	}
	if len(p.Sliders) != 2 {
		t.Fatalf("探针里应有 2 枚滑块，得到 %d", len(p.Sliders))
	}
	return p
}

func num(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("%q 不是数：%v", s, err)
	}
	return v
}

// rangeRig 是只放一枚滑块的页：四周留白 rangePad，滑块宽 width；记下滑块输入框的位置与 RangeInput 报告「变了」的次数。
type rangeRig struct {
	*harness
	state     *RangeState
	spec      RangeSpec
	left, top float32 // 滑块输入框的左缘与上缘（逻辑像素）
	height    float32
	changes   int // RangeInput 返回 changed 的次数
}

const rangePad = 60

func openRange(t *testing.T, spec RangeSpec, width float32) *rangeRig {
	t.Helper()
	r := &rangeRig{state: &RangeState{Value: spec.Min}, spec: spec}
	r.harness = openPage(t, still(), 900, 200, func(l *Look, c ui.C) {
		ui.Column(c).Pad(rangePad).Children(func() {
			ui.Column(c).Width(width).Children(func() {
				if l.RangeInput(c, r.state, spec) {
					r.changes++
				}
			})
		})
	})
	n, ok := r.node(gfx.RoleSlider, spec.Name)
	if !ok {
		t.Fatalf("找不到滑块 %q", spec.Name)
	}
	r.left, r.top, r.height = n.Bounds.X, n.Bounds.Y, n.Bounds.Height
	if math.Abs(float64(n.Bounds.Width-width)) > 0.01 {
		t.Fatalf("滑块宽 %v，期望 %v", n.Bounds.Width, width)
	}
	return r
}

func (r *rangeRig) cy() float32 { return r.top + r.height/2 }

// set 把受控值直接写进状态（与探针里按 Home/End 放好滑块头同一用意），出一帧让它画出来。
func (r *rangeRig) set(v float32) {
	r.t.Helper()
	r.state.Value = v
	r.frames(1)
}

func (r *rangeRig) press(x, y float32) {
	r.t.Helper()
	if err := r.session.Press(x, y); err != nil {
		r.t.Fatalf("在 (%g,%g) 按下失败：%v", x, y, err)
	}
	r.frames(2)
}

func (r *rangeRig) moveTo(x, y float32) {
	r.t.Helper()
	if err := r.session.Move(x, y); err != nil {
		r.t.Fatalf("移到 (%g,%g) 失败：%v", x, y, err)
	}
	r.frames(2)
}

func (r *rangeRig) release(x, y float32) {
	r.t.Helper()
	if err := r.session.Release(x, y); err != nil {
		r.t.Fatalf("在 (%g,%g) 松开失败：%v", x, y, err)
	}
	r.frames(2)
}

// expect 断言状态里的值。
func (r *rangeRig) expect(want float64, what string) {
	r.t.Helper()
	if got := r.state.Value; got != float32(want) {
		r.t.Errorf("%s：期望 %v，得到 %v", what, want, got)
	}
}

// eachRun 对探针里每枚滑块的每个宽度开一页并交给 fn。
func eachRun(t *testing.T, fn func(t *testing.T, r *rangeRig, spec RangeSpec, si, ri int)) {
	t.Helper()
	probe := loadRangeProbe(t)
	names := []string{"可见度", "虚化"}
	for si, s := range probe.Sliders {
		spec := RangeSpec{Name: names[si], Min: float32(num(t, s.Min)), Max: float32(num(t, s.Max)), Step: float32(num(t, s.Step))}
		for ri, run := range s.Runs {
			r := openRange(t, spec, float32(run.Width))
			fn(t, r, spec, si, ri)
		}
	}
}

// 点轨道：滑块头先放到对端，保证点击落在轨道上；点哪里，滑块头中心就跳到哪里（行程是宽度减 14px）。
func TestRangeClickMatchesReact(t *testing.T) {
	probe := loadRangeProbe(t)
	n := 0
	eachRun(t, func(t *testing.T, r *rangeRig, spec RangeSpec, si, ri int) {
		run := probe.Sliders[si].Runs[ri]
		for _, c := range run.Clicks {
			if c.Dx >= float64(run.Width) {
				continue // 落在输入框之外，探针里的这一条不是采样
			}
			if c.Dx < float64(run.Width)/2 {
				r.set(spec.Max)
			} else {
				r.set(spec.Min)
			}
			x := r.left + float32(c.Dx)
			r.press(x, r.cy())
			r.release(x, r.cy())
			r.expect(num(t, c.Value), "滑块"+strconv.Itoa(si)+" 宽 "+strconv.Itoa(run.Width)+" 点 "+strconv.FormatFloat(c.Dx, 'f', -1, 64))
			n++
		}
	})
	if n < 100 {
		t.Errorf("对照的点击太少：%d", n)
	}
}

// 拖动：按在滑块头中心，逐点移动，值跟着指针走（含拖出输入框之外、夹在两端）。
func TestRangeDragMatchesReact(t *testing.T) {
	probe := loadRangeProbe(t)
	n := 0
	eachRun(t, func(t *testing.T, r *rangeRig, spec RangeSpec, si, ri int) {
		run := probe.Sliders[si].Runs[ri]
		r.set(spec.Min)
		r.press(r.left+7, r.cy()) // Home 时滑块头中心在左缘 + 7
		r.expect(float64(spec.Min), "按在滑块头中心不该变")
		for _, d := range run.Drags {
			r.moveTo(r.left+float32(d.To), r.cy())
			r.expect(num(t, d.Value), "滑块"+strconv.Itoa(si)+" 宽 "+strconv.Itoa(run.Width)+" 拖到 "+strconv.FormatFloat(d.To, 'f', -1, 64))
			n++
		}
		r.release(r.left+float32(run.Drags[len(run.Drags)-1].To), r.cy())
	})
	if n < 30 {
		t.Errorf("对照的拖动太少：%d", n)
	}
}

// 没有抓取偏移：按在滑块头中心偏右处再拖，第一次移动就让中心对齐到指针（Viva 的 SliderBase 会带着偏移，这是它的差别之一）；
// 按在滑块头上不动不跳；按住后只动 1px，值立刻按指针重算。
func TestRangeThumbPressHasNoGrabOffset(t *testing.T) {
	probe := loadRangeProbe(t)
	eachRun(t, func(t *testing.T, r *rangeRig, spec RangeSpec, si, ri int) {
		run := probe.Sliders[si].Runs[ri]
		tag := "滑块" + strconv.Itoa(si) + " 宽 " + strconv.Itoa(run.Width)

		r.set(spec.Min)
		r.press(r.left+float32(run.Grab.DownAt), r.cy())
		r.moveTo(r.left+float32(run.Grab.MoveTo), r.cy())
		r.expect(num(t, run.Grab.Value), tag+" 抓取偏移")
		r.release(r.left+float32(run.Grab.MoveTo), r.cy())

		for _, p := range run.ThumbPress {
			r.set(spec.Min)
			r.press(r.left+float32(p.At), r.cy())
			r.expect(num(t, p.AfterDown), tag+" 按在滑块头 "+strconv.FormatFloat(p.At, 'f', -1, 64)+" 之后")
			r.release(r.left+float32(p.At), r.cy())
			r.expect(num(t, p.AfterUp), tag+" 在滑块头 "+strconv.FormatFloat(p.At, 'f', -1, 64)+" 松开之后")
		}

		r.set(spec.Min)
		r.press(r.left+float32(run.Nudge.DownAt), r.cy())
		r.moveTo(r.left+float32(run.Nudge.MoveTo), r.cy())
		r.expect(num(t, run.Nudge.Value), tag+" 按在滑块头上再动 1px")
		r.release(r.left+float32(run.Nudge.MoveTo), r.cy())
	})
}

// 滑块头的命中范围：值放正中，在滑块头周围的 (dx, dy) 网格上按下不动，值不变说明按在滑块头上、变了说明点了轨道。
// 对 912 个采样点拟合，渲染层的命中区是一个半径 8.5px 的圆，圆心比滑块头名义中心偏 (-0.5, -0.375)px（4 个点对不上）；
// 这个偏移疑似来自参照窗口的小数坐标（滑块上沿在 723.75），不复刻。所以与圆边相距不到 1px 的点不逐个比，
// 只统计不同的个数并设上限（半径少 0.66 是 106 个、多 0.74 是 142 个，当前取值是 70 个）；其余每个点都必须与渲染层一致。
// |dx| < 2 的点跳与不跳读数相同，网格里没有。
func TestRangeThumbHitMatchesReact(t *testing.T) {
	probe := loadRangeProbe(t)
	const hitR, band = 8.5, 1.0
	checked, rim, rimWrong := 0, 0, 0
	eachRun(t, func(t *testing.T, r *rangeRig, spec RangeSpec, si, ri int) {
		hit := probe.Sliders[si].Runs[ri].ThumbHit
		if hit == nil {
			return
		}
		centerX := float64(r.left) + 7 + 0.5*(float64(probe.Sliders[si].Runs[ri].Width)-14)
		for i, dy := range hit.Dys {
			if dy == 0 || dy >= float64(r.height) {
				continue // 在输入框之外（探针里 y=20 是输入框下沿之外）
			}
			for k, dx := range hit.Dxs {
				mid := float32(num(t, hit.Mid))
				r.state.Value = mid // 事件帧自己会用这个值画，不必先出帧
				x, y := float32(centerX+dx), r.top+float32(dy)
				if err := r.session.Press(x, y); err != nil {
					t.Fatalf("按下失败：%v", err)
				}
				inside := r.state.Value == mid
				if err := r.session.Release(x, y); err != nil {
					t.Fatalf("松开失败：%v", err)
				}
				d := math.Hypot(dx, dy-float64(r.height)/2)
				if math.Abs(d-hitR) < band {
					rim++
					if inside != hit.Inside[i][k] {
						rimWrong++
					}
					continue
				}
				checked++
				if inside != hit.Inside[i][k] {
					t.Errorf("滑块%d 点 (dx=%v, dy=%v)（距中心 %.2f）：渲染层 %s，这里 %s", si, dx, dy, d,
						map[bool]string{true: "按在滑块头上", false: "点了轨道"}[hit.Inside[i][k]],
						map[bool]string{true: "按在滑块头上", false: "点了轨道"}[inside])
				}
			}
		}
	})
	t.Logf("网格对照：比了 %d 个点全部一致；边缘 %d 个点不比（其中与渲染层不同的 %d 个）", checked, rim, rimWrong)
	if checked < 500 || rim == 0 {
		t.Errorf("网格对照数量不对：比了 %d，边缘跳过 %d", checked, rim)
	}
	if rimWrong > 80 {
		t.Errorf("边缘带里与渲染层不同的点有 %d 个（共 %d 个），超过上限 80：命中圆的半径或圆心偏了", rimWrong, rim)
	}
}

// rangeKeySpec 把探针里 Playwright 的键名换成 headless.Session.Keys 的写法。
func rangeKeySpec(name string) string {
	keys := map[string]string{
		"ArrowRight": "right", "ArrowLeft": "left", "ArrowUp": "up", "ArrowDown": "down",
		"PageUp": "pageup", "PageDown": "pagedown", "Home": "home", "End": "end",
	}
	mods := map[string]string{"Shift": "shift", "Control": "ctrl", "Alt": "alt"}
	parts := strings.Split(name, "+")
	out := make([]string, 0, len(parts))
	for _, p := range parts[:len(parts)-1] {
		out = append(out, mods[p])
	}
	return strings.Join(append(out, keys[parts[len(parts)-1]]), "+")
}

// 键盘：方向键一个步长，PageUp/PageDown 取 max((max − min)/10, step)（Viva 的 SliderBase 是 10 个值单位，
// 虚化滑块上差一倍），Home/End 到两端；Shift、Ctrl、Alt 不改变结果（SliderBase 的 Shift+方向键是大步）。
func TestRangeKeysMatchReact(t *testing.T) {
	probe := loadRangeProbe(t)
	n := 0
	eachRun(t, func(t *testing.T, r *rangeRig, spec RangeSpec, si, ri int) {
		keys := probe.Sliders[si].Runs[ri].Keys
		if len(keys) == 0 {
			return
		}
		r.keys("tab") // 页里只有这一个可聚焦的控件
		if node, _ := r.node(gfx.RoleSlider, spec.Name); !node.Focused {
			t.Fatal("Tab 之后滑块应有焦点")
		}
		for _, k := range keys {
			r.set(float32(num(t, k.Start)))
			r.keys(rangeKeySpec(k.Key))
			r.expect(num(t, k.From), "滑块"+strconv.Itoa(si)+" 从 "+k.Start+" 按 "+k.Key)
			n++
		}
	})
	if n < 40 {
		t.Errorf("对照的按键太少：%d", n)
	}
}

// changed 只在这一帧用户真的改了值时报告：键盘、点轨道、拖动各一次；到头再按、按在滑块头上不动、松开都不报。
func TestRangeReportsChangeOnlyWhenTheValueMoved(t *testing.T) {
	r := openRange(t, RangeSpec{Name: "可见度", Min: 0, Max: 100, Step: 1}, 400)
	r.set(50)
	r.changes = 0
	r.keys("tab")
	r.keys("right")
	if r.changes != 1 {
		t.Errorf("按一次 →，changed 应报告 1 次，得到 %d", r.changes)
	}
	r.keys("end")
	r.changes = 0
	r.keys("end")
	r.keys("right")
	if r.changes != 0 {
		t.Errorf("到头再按不该报告，得到 %d 次", r.changes)
	}
	r.set(0)
	r.changes = 0
	x := r.left + 200
	r.press(x, r.cy()) // 点轨道
	if r.changes != 1 {
		t.Errorf("点轨道应报告 1 次，得到 %d", r.changes)
	}
	r.release(x, r.cy())
	r.press(x, r.cy()) // 滑块头现在就在这里：按住不动
	r.release(x, r.cy())
	if r.changes != 1 {
		t.Errorf("按在滑块头上、松开都不该报告，累计得到 %d", r.changes)
	}
	r.press(x, r.cy())
	r.moveTo(x+30, r.cy())
	if r.changes != 2 {
		t.Errorf("拖动应再报告 1 次，累计得到 %d", r.changes)
	}
	r.moveTo(x+30, r.cy()) // 原地不动
	if r.changes != 2 {
		t.Errorf("没动不该报告，累计得到 %d", r.changes)
	}
	r.release(x+30, r.cy())
	r.moveTo(x+90, r.cy()) // 松开之后移动不再改值
	if r.changes != 2 {
		t.Errorf("松开之后移动不该报告，累计得到 %d", r.changes)
	}
}

// 只有主键能点、能拖：右键按下不改值、不开始拖动（原生只响应左键）。
func TestRangeIgnoresNonPrimaryButtons(t *testing.T) {
	r := openRange(t, RangeSpec{Name: "可见度", Min: 0, Max: 100, Step: 1}, 400)
	x := r.left + 300
	if err := r.session.Driver().Press(x, r.cy(), gfx.PointerButtonSecondary); err != nil {
		t.Fatalf("右键按下失败：%v", err)
	}
	r.frames(2)
	r.expect(0, "右键按在轨道上不该改值")
	r.moveTo(x+20, r.cy())
	r.expect(0, "右键按住后移动不该拖动")
	if err := r.session.Driver().Release(x+20, r.cy(), gfx.PointerButtonSecondary); err != nil {
		t.Fatalf("右键松开失败：%v", err)
	}
}

// 无障碍沿用 SliderBase 的写法：role=slider、名称、值文字与范围，值变了跟着变；窗口外的写入也落回范围与步长。
func TestRangeKeepsTheAccessibleSliderShape(t *testing.T) {
	spec := RangeSpec{Name: "虚化", Min: 0, Max: 50, Step: 0.5}
	r := openRange(t, spec, 400)
	r.set(12.5)
	n, ok := r.node(gfx.RoleSlider, spec.Name)
	if !ok || n.Value != "12.5" {
		t.Fatalf("滑块的值文字应是 12.5，得到 %+v（找到：%v）", n, ok)
	}
	if want := (gfx.ValueRange{Now: 12.5, Min: 0, Max: 50, Step: 0.5, Set: true}); n.Range != want {
		t.Errorf("范围不对：%+v，期望 %+v", n.Range, want)
	}
	r.set(999) // 受控值超出范围：落回上限
	if n, _ = r.node(gfx.RoleSlider, spec.Name); n.Value != "50" {
		t.Errorf("越界的值应落回 50，得到 %q", n.Value)
	}
	r.set(12.3) // 不在步长网格上：落到最近的格点
	if n, _ = r.node(gfx.RoleSlider, spec.Name); n.Value != "12.5" {
		t.Errorf("不在网格上的值应落到 12.5，得到 %q", n.Value)
	}
	if !n.Focusable {
		t.Error("滑块应可聚焦")
	}
}

// rangeSnap 的边界：夹范围、四舍五入到格点、超过上限退一格（范围不是步长的整数倍时）、连续滑块只夹范围。
func TestRangeSnap(t *testing.T) {
	cases := []struct {
		name string
		spec RangeSpec
		in   float64
		want float64
	}{
		{"整数步长的中点向上取整", RangeSpec{Min: 0, Max: 100, Step: 1}, 49.5, 50},
		{"半步长", RangeSpec{Min: 0, Max: 50, Step: 0.5}, 12.26, 12.5},
		{"低于下限", RangeSpec{Min: 0, Max: 100, Step: 1}, -3, 0},
		{"高于上限", RangeSpec{Min: 0, Max: 100, Step: 1}, 130, 100},
		{"起点不是 0：格点从 Min 起算", RangeSpec{Min: 3, Max: 20, Step: 5}, 12, 13},
		{"范围不是步长整数倍：舍入到上限之外时退一格", RangeSpec{Min: 0, Max: 10, Step: 4}, 10, 8},
		{"范围不是步长整数倍：没越过上限时照常舍入", RangeSpec{Min: 0, Max: 10, Step: 4}, 9.9, 8},
		{"最大值小于最小值：按最小值", RangeSpec{Min: 5, Max: 3, Step: 1}, 4, 5},
		{"连续滑块只夹范围", RangeSpec{Min: 0, Max: 1, Step: 0}, 0.123456789, 0.123457},
		{"0.1 步长不留浮点尾巴", RangeSpec{Min: 0, Max: 1, Step: 0.1}, 0.30000000000000004, 0.3},
	}
	for _, c := range cases {
		if got := rangeSnap(c.in, c.spec); got != c.want {
			t.Errorf("%s：rangeSnap(%v) = %v，期望 %v", c.name, c.in, got, c.want)
		}
	}
}

// 拖动状态跟着指针的按住与松开：按下为真，拖出输入框很远仍为真，松开为假。
// 它决定移动是否改值，也决定拖出输入框后滑块头是否保持放大。
func TestRangeDragFlagFollowsThePointer(t *testing.T) {
	r := openRange(t, RangeSpec{Name: "可见度", Min: 0, Max: 100, Step: 1}, 400)
	x := r.left + 200
	if r.state.drag {
		t.Fatal("还没按下就在拖动")
	}
	r.press(x, r.cy())
	if !r.state.drag {
		t.Error("按下之后应在拖动")
	}
	r.moveTo(x+300, r.cy())
	if !r.state.drag {
		t.Error("拖出输入框之后仍应在拖动")
	}
	r.release(x+300, r.cy())
	if r.state.drag {
		t.Error("松开之后应结束拖动")
	}
}

// thumbSize 是当前帧快照里滑块头（边长 13～17px 的正方形实心圆角矩形）的边长。快照按像素取整：常态 14px，放大到 1.12 倍后是 16。
func (r *rangeRig) thumbSize() float32 {
	for _, p := range r.session.Snapshot().Primitives {
		if p.Kind == 0 && p.Bounds.Width == p.Bounds.Height && p.Bounds.Width > 13 && p.Bounds.Width < 17 {
			return p.Bounds.Width
		}
	}
	return 0
}

// 悬停反馈：指针在输入框上，或按住拖出输入框之外时，滑块头放大到 1.12 倍、光标是手形；松开、指针在外面就回到常态。
// 渲染层的放大写在 input[type=range]:hover::-webkit-slider-thumb 上；探针量得：拖出输入框之后输入框仍匹配 :hover，松开后失去
// （probe-range-pointer.mjs 的 dragHover），光标是 cursor: pointer（globals.css）。
func TestRangeHoverFeedback(t *testing.T) {
	r := openRange(t, RangeSpec{Name: "可见度", Min: 0, Max: 100, Step: 1}, 400)
	away := r.left + 640 // 输入框右边很远的空白处
	check := func(what string, size float32, cursor app.Cursor) {
		t.Helper()
		if got := r.thumbSize(); got != size {
			t.Errorf("%s：滑块头边长应是 %v，得到 %v", what, size, got)
		}
		if got := r.session.Snapshot().Cursor; got != cursor {
			t.Errorf("%s：光标应是 %v，得到 %v", what, cursor, got)
		}
	}
	check("指针还没进来", 14, app.CursorArrow)
	r.moveTo(r.left+200, r.cy())
	check("指针在输入框上", 16, app.CursorHand)
	r.moveTo(away, r.cy())
	check("指针移开", 14, app.CursorArrow)
	r.moveTo(r.left+7, r.cy())
	r.press(r.left+7, r.cy()) // 按在滑块头上
	r.moveTo(away, r.cy())    // 拖到输入框外面很远
	if got := r.thumbSize(); got != 16 {
		t.Errorf("按住拖出输入框之后滑块头应保持放大：边长 %v", got)
	}
	r.release(away, r.cy())
	check("在输入框外松开", 14, app.CursorArrow)
}

// 滑块是 Tab 停靠点：滚动区里滚出视野的滑块，从前一个停靠点（视野里的按钮）按 Tab，把它滚进视野并聚焦
// （台账 B-030；Viva 自己的 Tab 环不含滚出视野的控件，reach.go 要有出发点才接手）。
func TestRangeIsATabStopInScrollAreas(t *testing.T) {
	const viewH = 110
	state := &RangeState{}
	h := openPage(t, still(), 300, viewH, func(l *Look, c ui.C) {
		l.ScrollArea(c.Key("area"), [4]float32{}, func() {
			reachButton(l, c, "first", "起点")
			ui.Column(c.Key("pad")).FillWidth().Height(400).Children(func() {})
			ui.Column(c.Key("range")).FillWidth().Children(func() {
				l.RangeInput(c, state, RangeSpec{Name: "可见度", Min: 0, Max: 100, Step: 1})
			})
		}).Fill()
	})
	h.keys("tab")
	if got := h.focusedName(); got != "起点" {
		t.Fatalf("第一次 Tab 应当聚焦起点按钮，实际 %q", got)
	}
	h.keys("tab")
	n, ok := h.focused()
	if !ok || n.Role != gfx.RoleSlider {
		t.Fatalf("Tab 应当聚焦滑块，实际 %+v（有焦点：%v）", n, ok)
	}
	if n.Bounds.Y < 0 || n.Bounds.Y+n.Bounds.Height > viewH {
		t.Errorf("聚焦的滑块应被滚进视野 0..%d，实际在 %+v", viewH, n.Bounds)
	}
}

// 连续滑块（Step 为 0，对应原生的 step="any"）：方向键走范围的百分之一，PageDown 走十分之一，
// 指针取值不落格，无障碍步长回退为 1。数据取自 Chromium 源码的读法，没有实测——应用里的两枚滑块都有步长。
func TestRangeContinuousSlider(t *testing.T) {
	spec := RangeSpec{Name: "连续", Min: 0, Max: 1, Step: 0}
	r := openRange(t, spec, 400)
	r.set(0.5)
	r.keys("tab")
	r.keys("right")
	r.expect(0.51, "连续滑块按一次 →")
	r.keys("pagedown")
	r.expect(0.41, "连续滑块按一次 PageDown")

	x := r.left + 7 + float32(0.3*(400-14)) // 点在行程的 30% 处
	r.press(x, r.cy())
	r.release(x, r.cy())
	r.expect(0.3, "连续滑块点轨道不落格")

	if n, _ := r.node(gfx.RoleSlider, spec.Name); n.Range.Step != 1 {
		t.Errorf("连续滑块的无障碍步长应回退为 1，得到 %v", n.Range.Step)
	}
}

// 退化的几何：输入框窄得放不下滑块头时取最小值；最大值等于最小值时位置为 0，不出现 NaN。
func TestRangeDegenerateGeometry(t *testing.T) {
	spec := RangeSpec{Min: 2, Max: 8, Step: 1}
	if got := rangePointerValue(5, gfx.Rect{X: 0, Y: 0, Width: 10, Height: 20}, 14, spec); got != 2 {
		t.Errorf("宽度不够放滑块头：期望取最小值 2，得到 %v", got)
	}
	if got := rangeFraction(5, RangeSpec{Min: 3, Max: 3}); got != 0 {
		t.Errorf("范围为空：位置应为 0，得到 %v", got)
	}
}
