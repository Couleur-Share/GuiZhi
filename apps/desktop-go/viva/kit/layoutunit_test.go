package kit

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"testing"
)

// flexProbe 是 testdata/flex-units-react.json：scripts/parity/tools/probe-flex-units.mjs 在渲染层（Chromium）量出的
// 弹性布局位置，单位 1/64 px（LayoutUnit），相对容器左缘。每类的列顺序见探针的注释。
type flexProbe struct {
	Unit    int           `json:"unit"`
	Even    []evenCase    `json:"even"`
	Between []betweenCase `json:"between"`
	Center  [][3]int32    `json:"center"`
	Grow    []growCase    `json:"grow"`
}

// evenCase：n 个 flex: 1 1 0% 的格子，每格居中放一个定宽孩子。
type evenCase struct {
	W, N, Gap, Child int32
	Cells, Items     []int32 // 各格宽、各孩子左缘
}

func (c *evenCase) UnmarshalJSON(b []byte) error {
	var raw [6]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for i, dst := range []any{&c.W, &c.N, &c.Gap, &c.Child, &c.Cells, &c.Items} {
		if err := json.Unmarshal(raw[i], dst); err != nil {
			return fmt.Errorf("even 第 %d 列：%w", i, err)
		}
	}
	return nil
}

// betweenCase：justify-content: space-between 的 n 个定宽孩子。
type betweenCase struct {
	W, N, Child int32
	Items       []int32
}

func (c *betweenCase) UnmarshalJSON(b []byte) error {
	var raw [4]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for i, dst := range []any{&c.W, &c.N, &c.Child, &c.Items} {
		if err := json.Unmarshal(raw[i], dst); err != nil {
			return fmt.Errorf("between 第 %d 列：%w", i, err)
		}
	}
	return nil
}

// growCase：定宽（flex-basis）与弹性项混排；Bases 里 -1 表示弹性项（flex: 1 1 0%）。
type growCase struct {
	W, Gap int32
	Bases  []int32
	Items  [][2]int32 // 各项 [左缘, 宽]
}

func (c *growCase) UnmarshalJSON(b []byte) error {
	var raw [4]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for i, dst := range []any{&c.W, &c.Gap, &c.Bases, &c.Items} {
		if err := json.Unmarshal(raw[i], dst); err != nil {
			return fmt.Errorf("grow 第 %d 列：%w", i, err)
		}
	}
	return nil
}

func loadFlexProbe(t *testing.T) flexProbe {
	t.Helper()
	data, err := os.ReadFile("testdata/flex-units-react.json")
	if err != nil {
		t.Fatalf("读不到弹性布局探针数据：%v", err)
	}
	var p flexProbe
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("弹性布局探针数据解析失败：%v", err)
	}
	if p.Unit != luPerPx || len(p.Even) == 0 || len(p.Between) == 0 || len(p.Center) == 0 || len(p.Grow) == 0 {
		t.Fatalf("探针数据不完整：unit=%d even=%d between=%d center=%d grow=%d", p.Unit, len(p.Even), len(p.Between), len(p.Center), len(p.Grow))
	}
	return p
}

// 渲染层实测的 1464 例：均分格（含居中的孩子，含剩余为负的向零取整）、两端对齐、居中、定宽加弹性的混排。
func TestFlexRulesMatchChromium(t *testing.T) {
	p := loadFlexProbe(t)
	t.Run("even", func(t *testing.T) {
		bad := 0
		for _, c := range p.Even {
			cells := distributeLU(nil, c.W-c.Gap*(c.N-1), int(c.N))
			items := make([]int32, c.N)
			var x int32
			for i, w := range cells {
				items[i] = x + centerOffsetLU(w-c.Child)
				x += w + c.Gap
			}
			if !reflect.DeepEqual(cells, c.Cells) || !reflect.DeepEqual(items, c.Items) {
				if bad++; bad <= 3 {
					t.Errorf("W=%d n=%d gap=%d child=%d：格宽 期望 %v 得到 %v；孩子左缘 期望 %v 得到 %v", c.W, c.N, c.Gap, c.Child, c.Cells, cells, c.Items, items)
				}
			}
		}
		if bad > 0 {
			t.Errorf("%d / %d 例与渲染层不一致", bad, len(p.Even))
		}
	})
	t.Run("between", func(t *testing.T) {
		bad := 0
		for _, c := range p.Between {
			free := c.W - c.Child*c.N
			items := make([]int32, c.N)
			for k := range items {
				items[k] = int32(k)*c.Child + betweenOffsetLU(free, k, int(c.N))
			}
			if !reflect.DeepEqual(items, c.Items) {
				if bad++; bad <= 3 {
					t.Errorf("W=%d n=%d child=%d：期望 %v 得到 %v", c.W, c.N, c.Child, c.Items, items)
				}
			}
		}
		if bad > 0 {
			t.Errorf("%d / %d 例与渲染层不一致", bad, len(p.Between))
		}
	})
	t.Run("center", func(t *testing.T) {
		bad := 0
		for _, c := range p.Center {
			if got := centerOffsetLU(c[0] - c[1]); got != c[2] {
				if bad++; bad <= 3 {
					t.Errorf("W=%d child=%d：期望 %d 得到 %d", c[0], c[1], c[2], got)
				}
			}
		}
		if bad > 0 {
			t.Errorf("%d / %d 例与渲染层不一致", bad, len(p.Center))
		}
	})
	t.Run("grow", func(t *testing.T) {
		bad := 0
		for _, c := range p.Grow {
			n := int32(len(c.Bases))
			free, flex := c.W-c.Gap*(n-1), 0
			for _, b := range c.Bases {
				if b < 0 {
					flex++
				} else {
					free -= b
				}
			}
			shares := distributeLU(nil, free, flex)
			got := make([][2]int32, n)
			var x int32
			next := 0
			for i, b := range c.Bases {
				w := b
				if b < 0 {
					w = shares[next]
					next++
				}
				got[i] = [2]int32{x, w}
				x += w + c.Gap
			}
			if !reflect.DeepEqual(got, c.Items) {
				if bad++; bad <= 3 {
					t.Errorf("W=%d gap=%d 基准 %v：期望 %v 得到 %v", c.W, c.Gap, c.Bases, c.Items, got)
				}
			}
		}
		if bad > 0 {
			t.Errorf("%d / %d 例与渲染层不一致", bad, len(p.Grow))
		}
	})
}

// Viva 的版面是 float32：802 可能是 801.99994，换算要落在最近的 1/64 单位上，不能向下取整差一个单位。
func TestPxToLURoundsToTheNearestUnit(t *testing.T) {
	cases := []struct {
		px   float32
		want int32
	}{
		{802, 51328}, {801.99994, 51328}, {802.00006, 51328}, {0.5, 32}, {442.484375, 28319}, {-171.0 / 64, -171}, {-2.6796875 + 1e-6, -171}, {0, 0},
	}
	for _, c := range cases {
		if got := pxToLU(c.px); got != c.want {
			t.Errorf("pxToLU(%v) = %d，期望 %d", c.px, got, c.want)
		}
	}
	if got := luToPx(28319); got != 442.484375 {
		t.Errorf("luToPx(28319) = %v，期望 442.484375", got)
	}
}

// QuantizeLU 抹掉 float32 的一个 ulp 噪声（912 量成 912.00006、801.99994），但保留真正的 1/64 小数。
func TestQuantizeLURemovesTheFloatNoiseAndKeepsRealFractions(t *testing.T) {
	cases := []struct{ in, want float32 }{
		{912, 912}, {math.Nextafter32(912, 1000), 912}, {math.Nextafter32(802, 0), 802}, {801.99994, 802},
		{442.484375, 442.484375}, {0.5, 0.5}, {0, 0}, {1.0 / 64, 1.0 / 64},
		{912 + 1.0/64, 912 + 1.0/64}, // 真的差 1/64 px 的宽度不能被抹掉：渲染层的 ResizeObserver 也给这个值
	}
	for _, c := range cases {
		if got := QuantizeLU(c.in); got != c.want {
			t.Errorf("QuantizeLU(%v) = %v，期望 %v", c.in, got, c.want)
		}
	}
}

// 分配不丢不多：总和恰好等于剩余，每项只在 floor 与 ceil 之间；负的剩余按 0 算。
func TestDistributeConservesTheFreeSpace(t *testing.T) {
	var scratch []int32
	for free := int32(-5); free <= 3000; free += 7 {
		for n := 1; n <= 12; n++ {
			scratch = distributeLU(scratch, free, n)
			if len(scratch) != n {
				t.Fatalf("free=%d n=%d：得到 %d 项", free, n, len(scratch))
			}
			want := max(free, 0)
			var sum int32
			for _, w := range scratch {
				sum += w
				if w < want/int32(n) || w > (want+int32(n)-1)/int32(n) {
					t.Fatalf("free=%d n=%d：有一项 %d 超出 floor/ceil", free, n, w)
				}
			}
			if sum != want {
				t.Fatalf("free=%d n=%d：总和 %d，期望 %d", free, n, sum, want)
			}
		}
	}
	if got := distributeLU(scratch, 100, 0); len(got) != 0 {
		t.Errorf("0 项应得到空切片，得到 %v", got)
	}
}

// 绘制路径上不分配：草稿切片容量够就复用。
func TestDistributeReusesTheScratchSlice(t *testing.T) {
	scratch := make([]int32, 0, 16)
	allocs := testing.AllocsPerRun(100, func() { scratch = distributeLU(scratch, 51328, 7) })
	if allocs != 0 {
		t.Errorf("容量够时 distributeLU 不该分配，得到 %v 次", allocs)
	}
}

// 两端对齐在剩余不为正或只有一个孩子时按起点对齐（CSS：space-between 在这两种情形等同 flex-start）。
func TestBetweenFallsBackToStart(t *testing.T) {
	for _, c := range []struct {
		free    int32
		k, n    int
		wantOff int32
	}{{0, 1, 3, 0}, {-64, 2, 3, 0}, {640, 0, 1, 0}, {640, 0, 3, 0}, {640, 2, 3, 640}, {641, 1, 3, 321}} {
		if got := betweenOffsetLU(c.free, c.k, c.n); got != c.wantOff {
			t.Errorf("free=%d k=%d n=%d：期望 %d 得到 %d", c.free, c.k, c.n, c.wantOff, got)
		}
	}
}
