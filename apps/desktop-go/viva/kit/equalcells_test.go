package kit

import (
	"fmt"
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// equalRow 是一排 EqualCells 的期望：版面、容器宽，以及渲染层实测的各孩子左缘（1/64 px，相对容器左缘）与各格宽。
type equalRow struct {
	cells  *EqualCells
	w      int32
	child  int32 // 居中档的孩子宽；撑满档为 0
	items  []int32
	widths []int32 // 撑满档：各格宽
}

const (
	equalRowHeight = 40 // 每排容器的高度；孩子高 20，竖向居中后上缘在 10 处
	equalRowIndent = 17 // 每排容器离页面左缘的距离：让「版面忽略容器左缘」这类错误露出来
)

// declareEqualRow 在页面里声明第 r 排：左边留 equalRowIndent，然后是宽 row.w 的列，里面是一个 EqualCells 容器。
func declareEqualRow(c ui.C, r int, row equalRow) {
	ui.Row(c.Key(fmt.Sprintf("line%d", r))).Children(func() {
		ui.Row(c.Key("indent")).Width(equalRowIndent).Height(1).Children(func() {})
		ui.Column(c.Key(fmt.Sprintf("row%d", r))).Width(luToPx(row.w)).Children(func() {
			EqualCellsBox(c, row.cells).Height(equalRowHeight).Children(func() {
				for j := range row.items {
					name := fmt.Sprintf("r%dc%d", r, j)
					cc := c.Key(name)
					box := ui.Row(cc).Height(20)
					if row.child > 0 {
						box = box.Width(luToPx(row.child))
					}
					box.HitWith(ui.ID(cc), name, gfx.HitOptions{Role: gfx.RoleButton}).Children(func() {})
				}
			})
		})
	})
}

// unitsOf 把版面里的像素换成 1/64 单位，并要求它恰好是整数倍（EqualCells 摆出的位置都在 1/64 的格点上）。
func unitsOf(t *testing.T, px float32) int32 {
	t.Helper()
	v := float64(px) * luPerPx
	if math.Abs(v-math.Round(v)) > 1e-3 {
		t.Errorf("%v 不是 1/64 的整数倍", px)
	}
	return int32(math.Round(v))
}

// 在无头会话里真排一遍：EqualCells 摆出的孩子左缘、宽度与渲染层实测（testdata/flex-units-react.json）逐单位一致。
// 一页放多排，避免每个用例开一个会话（kit 包的测试时间贴着 10 s 的线）。
func TestEqualCellsPlacesChildrenLikeChromium(t *testing.T) {
	p := loadFlexProbe(t)
	var rows []equalRow
	// 居中档：每 12 例抽一例，跳过孩子宽为 0 的（零尺寸没有可访问节点；纯函数的用例已覆盖）
	for i := 0; i < len(p.Even); i += 12 {
		c := p.Even[i]
		if c.Child == 0 {
			continue
		}
		rows = append(rows, equalRow{cells: &EqualCells{Gap: luToPx(c.Gap), Center: true}, w: c.W, child: c.Child, items: c.Items})
	}
	centered := len(rows)
	// 撑满档：孩子就是弹性项，左缘 = 累计的格宽加间距
	for i := 5; i < len(p.Even); i += 48 {
		c := p.Even[i]
		items := make([]int32, c.N)
		var x int32
		for j, w := range c.Cells {
			items[j] = x
			x += w + c.Gap
		}
		rows = append(rows, equalRow{cells: &EqualCells{Gap: luToPx(c.Gap)}, w: c.W, items: items, widths: c.Cells})
	}
	if centered < 20 || len(rows)-centered < 8 {
		t.Fatalf("抽样太少：居中 %d 排，撑满 %d 排", centered, len(rows)-centered)
	}

	h := openPage(t, still(), 1100, 30+len(rows)*equalRowHeight, func(l *Look, c ui.C) {
		ui.Column(c).Children(func() {
			for r, row := range rows {
				declareEqualRow(c, r, row)
			}
		})
	})

	for r, row := range rows {
		for j, want := range row.items {
			name := fmt.Sprintf("r%dc%d", r, j)
			n, ok := h.node(gfx.RoleButton, name)
			if !ok {
				t.Fatalf("找不到 %s", name)
			}
			if got := unitsOf(t, n.Bounds.X) - equalRowIndent*luPerPx; got != want {
				t.Errorf("%s（W=%d 单位，间距 %v，居中=%v）：左缘 期望 %d 得到 %d", name, row.w, row.cells.Gap, row.cells.Center, want, got)
			}
			if !row.cells.Center {
				if got := unitsOf(t, n.Bounds.Width); got != row.widths[j] {
					t.Errorf("%s（撑满）：宽 期望 %d 得到 %d", name, row.widths[j], got)
				}
			}
			// 交叉轴居中：容器高 40、孩子高 20，上缘落在本排顶部下 10 处
			if want := float32(r*equalRowHeight + 10); n.Bounds.Y != want {
				t.Errorf("%s：上缘 期望 %v 得到 %v", name, want, n.Bounds.Y)
			}
		}
	}
}

// 显示设置页色带的情形（渲染层 dark-green-large-1280x800 实测）：内容宽 802、七枚 45 px 的色板各居中在一格里，
// 内容区左缘 64，七枚的左缘是 98.78125、213.34375、327.921875、442.484375（精确值 442.5，差在这一枚）、557.0625、671.625、786.203125。
func TestEqualCellsSwatchRowMatchesTheMeasuredReactRow(t *testing.T) {
	want := []float32{98.78125, 213.34375, 327.921875, 442.484375, 557.0625, 671.625, 786.203125}
	cells := &EqualCells{Center: true}
	h := openPage(t, still(), 1100, 120, func(l *Look, c ui.C) {
		ui.Row(c).Children(func() {
			ui.Row(c.Key("margin")).Width(64).Height(1).Children(func() {})
			ui.Column(c.Key("band")).Width(802).Children(func() {
				EqualCellsBox(c, cells).Children(func() {
					for i := range want {
						name := fmt.Sprintf("sw%d", i)
						cc := c.Key(name)
						ui.Row(cc).Width(45).Height(45).HitWith(ui.ID(cc), name, gfx.HitOptions{Role: gfx.RoleButton}).Children(func() {})
					}
				})
			})
		})
	})
	for i, x := range want {
		n, ok := h.node(gfx.RoleButton, fmt.Sprintf("sw%d", i))
		if !ok {
			t.Fatalf("找不到第 %d 枚色板", i)
		}
		if n.Bounds.X != x || n.Bounds.Width != 45 {
			t.Errorf("第 %d 枚：左缘 %v 宽 %v，渲染层是 %v 宽 45", i, n.Bounds.X, n.Bounds.Width, x)
		}
	}
}

// 没有孩子、只有一个孩子、格子比孩子窄（居中向两边溢出）、容器比间距还窄，都不崩、不出 NaN。
func TestEqualCellsDegenerateShapes(t *testing.T) {
	cases := []struct {
		name  string
		width float32
		gap   float32
		n     int
		child float32
	}{
		{"empty", 300, 0, 0, 45},
		{"single", 300, 0, 1, 45},
		{"narrow cells overflow both sides", 90, 0, 4, 45},
		{"gap wider than the container", 20, 30, 3, 10},
		{"zero width", 0, 0, 3, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cells := &EqualCells{Gap: tc.gap, Center: true}
			h := openPage(t, still(), 400, 100, func(l *Look, c ui.C) {
				ui.Column(c).Width(tc.width).Children(func() {
					EqualCellsBox(c, cells).Children(func() {
						for i := 0; i < tc.n; i++ {
							name := fmt.Sprintf("d%d", i)
							cc := c.Key(name)
							ui.Row(cc).Width(tc.child).Height(10).HitWith(ui.ID(cc), name, gfx.HitOptions{Role: gfx.RoleButton}).Children(func() {})
						}
					})
				})
			})
			for i := 0; i < tc.n; i++ {
				n, ok := h.node(gfx.RoleButton, fmt.Sprintf("d%d", i))
				if !ok {
					continue // 容器宽为 0 时可能没有节点
				}
				for _, v := range []float32{n.Bounds.X, n.Bounds.Y, n.Bounds.Width, n.Bounds.Height} {
					if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
						t.Fatalf("第 %d 个孩子的外框有 NaN / Inf：%+v", i, n.Bounds)
					}
				}
			}
			if tc.name == "narrow cells overflow both sides" {
				// 4 格各 22.5，孩子宽 45：居中偏移 (22.5−45)/2 = −11.25，向零取整到 1/64 = −11.25（正好是 1/64 的整数倍）
				n, _ := h.node(gfx.RoleButton, "d0")
				if n.Bounds.X != -11.25 {
					t.Errorf("窄格子里的孩子应向左溢出 11.25，得到左缘 %v", n.Bounds.X)
				}
			}
		})
	}
}

// 容器高度由最高的孩子撑起，矮的孩子在交叉轴上居中（items-center）；后面的兄弟紧挨着容器下缘。
func TestEqualCellsHeightFollowsTheTallestChild(t *testing.T) {
	heights := []float32{10, 30, 20}
	cells := &EqualCells{Center: true}
	h := openPage(t, still(), 400, 100, func(l *Look, c ui.C) {
		ui.Column(c).Width(300).Children(func() {
			EqualCellsBox(c, cells).Children(func() {
				for i, hh := range heights {
					name := fmt.Sprintf("h%d", i)
					cc := c.Key(name)
					ui.Row(cc).Width(20).Height(hh).HitWith(ui.ID(cc), name, gfx.HitOptions{Role: gfx.RoleButton}).Children(func() {})
				}
			})
			cc := c.Key("after")
			ui.Row(cc).Width(20).Height(5).HitWith(ui.ID(cc), "after", gfx.HitOptions{Role: gfx.RoleButton}).Children(func() {})
		})
	})
	wantTop := []float32{10, 0, 5} // (30 − 10) ÷ 2、0、(30 − 20) ÷ 2
	for i, want := range wantTop {
		n, ok := h.node(gfx.RoleButton, fmt.Sprintf("h%d", i))
		if !ok {
			t.Fatalf("找不到第 %d 个孩子", i)
		}
		if n.Bounds.Y != want {
			t.Errorf("第 %d 个孩子（高 %v）：上缘 期望 %v 得到 %v", i, heights[i], want, n.Bounds.Y)
		}
	}
	after, ok := h.node(gfx.RoleButton, "after")
	if !ok {
		t.Fatal("找不到后面的兄弟")
	}
	if after.Bounds.Y != 30 {
		t.Errorf("兄弟应紧挨着容器下缘（最高的孩子 30），上缘得到 %v", after.Bounds.Y)
	}
}
