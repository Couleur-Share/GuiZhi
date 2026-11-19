package kit

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// flexShrinkCase 是渲染层实测的一例：容器定高、各子项，以及 Chromium 给出的每个子项的上缘与高度（都是 1/64 px）。
type flexShrinkCase struct {
	room  int32
	nodes []flexNode
	isRow []bool     // 与 nodes 一一对应：行（min-h-[32px]，可压到 32）还是 fix（min-height:auto 的块，压不过内容高）
	want  [][2]int32 // [上缘, 高]
}

// loadFlexShrinkProbe 读 testdata/flex-shrink-react.json（scripts/parity/tools/probe-flex-shrink.mjs 量出）。
func loadFlexShrinkProbe(t *testing.T) []flexShrinkCase {
	t.Helper()
	data, err := os.ReadFile("testdata/flex-shrink-react.json")
	if err != nil {
		t.Fatalf("读不到定高 flex 列探针数据：%v", err)
	}
	var raw struct {
		Unit  int     `json:"unit"`
		Cases [][]any `json:"cases"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("定高 flex 列探针数据解析失败：%v", err)
	}
	if raw.Unit != luPerPx || len(raw.Cases) < 100 {
		t.Fatalf("探针数据不完整：unit=%d cases=%d", raw.Unit, len(raw.Cases))
	}
	num := func(v any) int32 { return int32(v.(float64)) }
	out := make([]flexShrinkCase, 0, len(raw.Cases))
	for _, c := range raw.Cases {
		fc := flexShrinkCase{room: num(c[0])}
		for _, it := range c[1].([]any) {
			f := it.([]any)
			switch f[0].(string) {
			case "row":
				fc.nodes = append(fc.nodes, flexNode{base: num(f[1]), min: num(f[2]), shrink: true})
				fc.isRow = append(fc.isRow, true)
			case "fix":
				// fix 是没写 shrink-0 的普通块：flex-shrink 为默认的 1，min-height:auto 即内容高——参与收缩的计算，只是压不下去。
				fc.nodes = append(fc.nodes, flexNode{base: num(f[1]), min: num(f[1]), shrink: true, mt: num(f[2]), mb: num(f[3]), auto: num(f[4]) == 1})
				fc.isRow = append(fc.isRow, false)
			default:
				t.Fatalf("未知的子项种类 %v", f[0])
			}
		}
		for _, w := range c[2].([]any) {
			p := w.([]any)
			fc.want = append(fc.want, [2]int32{num(p[0]), num(p[1])})
		}
		out = append(out, fc)
	}
	return out
}

// 渲染层实测的 525 例：行（内容 28–40.5、min 32）与分区标题、说明、页签、mt-auto 的回收站块混排，容器高从富余 30 px
// 一路到比下限总高还小一半，覆盖「不收缩 / 部分收缩（带 1/64 的取整余数）/ 全部压到下限 / 溢出」。
// 子项的上缘与高度逐单位一致，包括取整余数落在哪几行。
func TestFlexColumnMatchesChromiumShrink(t *testing.T) {
	cases := loadFlexShrinkProbe(t)
	bad := 0
	for i, c := range cases {
		nodes := append([]flexNode(nil), c.nodes...)
		resolveFlexColumn(nodes, c.room, c.room)
		for j, n := range nodes {
			if got := [2]int32{n.top, n.size}; got != c.want[j] {
				if bad++; bad <= 5 {
					t.Errorf("第 %d 例（容器高 %d 单位，%d 个子项）第 %d 项 [上缘 高]：期望 %v 得到 %v", i, c.room, len(nodes), j, c.want[j], got)
				}
				break
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d / %d 例与渲染层不一致", bad, len(cases))
	}
}

// min-height:auto 的子项不是「不参与」：它们带着权重参加每一轮分摊，份额四舍五入为 0 时一直留在未冻结的行列里。
// 把它们当成一开始就冻结（只让行参与），多数情形碰巧一致，容器只差几个 1/64 px 的几例（38 与 40 行、缺口 0.5 px）
// 就会错位一行——这正是实现过的一个错误版本。本测试保证探针数据里一直留着能把这两种实现区分开的例子。
func TestFlexColumnAutoMinItemsKeepTheirWeight(t *testing.T) {
	var discriminating int
	for _, c := range loadFlexShrinkProbe(t) {
		nodes := append([]flexNode(nil), c.nodes...)
		for i := range nodes {
			if !c.isRow[i] {
				nodes[i].shrink = false // 错误的做法：一开始就冻结
			}
		}
		resolveFlexColumn(nodes, c.room, c.room)
		for j, n := range nodes {
			if ([2]int32{n.top, n.size}) != c.want[j] {
				discriminating++
				break
			}
		}
	}
	if discriminating < 3 {
		t.Errorf("探针里只剩 %d 例能区分「min:auto 子项参与分摊」与「一开始就冻结」，应不少于 3 例", discriminating)
	}
}

// 这批探针必须真的练到收缩的各个分支：部分收缩、压到下限、溢出各有足够的例子（防止数据过松，用例退化成「没收缩」）。
func TestFlexShrinkProbeCoversEveryBranch(t *testing.T) {
	var partial, atMin, overflow, grown int
	for _, c := range loadFlexShrinkProbe(t) {
		var rows, shrunk, pinned int
		var natural int32
		for j, n := range c.nodes {
			natural += n.mt + n.mb + max(n.base, n.min)
			if !c.isRow[j] {
				continue
			}
			rows++
			size := c.want[j][1]
			switch {
			case size < n.base && size > n.min:
				shrunk++
			case size == n.min && n.base > n.min:
				pinned++
			}
		}
		switch {
		case rows == 0:
		case shrunk > 0:
			partial++
		case pinned == rows:
			atMin++
			if c.room*2 <= natural+1 { // 容器只有内容自然高度的一半：压到下限也放不下，要滚动
				overflow++
			}
		default:
			grown++ // 没有收缩（容器够高，或行的内容本来就不高于下限）
		}
	}
	if partial < 40 || atMin < 40 || overflow < 5 || grown < 40 {
		t.Errorf("探针覆盖不足：部分收缩 %d、压到下限 %d、溢出 %d、没有收缩 %d", partial, atMin, overflow, grown)
	}
}

// 解算的边界：没有子项、容器高未知（首帧）、全部不收缩、行的内容低于下限、富余空间的分配。
func TestResolveFlexColumnEdges(t *testing.T) {
	px := func(v float32) int32 { return pxToLU(v) }
	row := func(content float32) flexNode { return flexNode{base: px(content), min: px(32), shrink: true} }
	fix := func(h float32) flexNode { return flexNode{base: px(h)} }

	if used := resolveFlexColumn(nil, px(100), px(100)); used != 0 {
		t.Errorf("没有子项：总高 期望 0 得到 %d", used)
	}

	// 容器高未知（首帧布局之前，room 为 0）：不收缩，行保持内容高
	nodes := []flexNode{fix(54), row(36), row(36)}
	if used := resolveFlexColumn(nodes, 0, 0); used != px(54+72) || nodes[1].size != px(36) {
		t.Errorf("容器高未知：总高 %d 行高 %d，应不收缩（%d、%d）", used, nodes[1].size, px(54+72), px(36))
	}

	// 全部不收缩：容器再小也保持内容高，总高超出容器（滚动）
	nodes = []flexNode{fix(54), fix(20), fix(30)}
	if used := resolveFlexColumn(nodes, px(50), px(50)); used != px(104) || nodes[2].top != px(74) {
		t.Errorf("全部不收缩：总高 %d、末项上缘 %d，期望 %d、%d", used, nodes[2].top, px(104), px(74))
	}

	// 内容高低于下限（小字号档：行内容 28、下限 32）：行取下限，不会因容器富余而变高
	nodes = []flexNode{row(28), row(28)}
	resolveFlexColumn(nodes, px(500), px(500))
	if nodes[0].size != px(32) || nodes[1].top != px(32) {
		t.Errorf("内容低于下限：行高 %d、第二行上缘 %d，期望 %d、%d", nodes[0].size, nodes[1].top, px(32), px(32))
	}

	// 富余空间归 mt-auto 的子项：容器 300，内容 54 + 32 + 54，回收站块被推到底
	trash := fix(54)
	trash.auto = true
	nodes = []flexNode{fix(54), row(32), trash}
	if used := resolveFlexColumn(nodes, px(300), px(300)); used != px(140) || nodes[2].top != px(300-54) {
		t.Errorf("mt-auto：总高 %d、回收站上缘 %d，期望 %d、%d", used, nodes[2].top, px(140), px(300-54))
	}
	// 没有富余时 mt-auto 不起作用（不会变成负数）
	nodes = []flexNode{fix(54), row(36), trash}
	resolveFlexColumn(nodes, px(100), px(100))
	if nodes[2].top != nodes[1].top+nodes[1].size {
		t.Errorf("没有富余时回收站应紧接着上一项：上缘 %d，上一项末端 %d", nodes[2].top, nodes[1].top+nodes[1].size)
	}
}

// 探针里只有「行」与「fix」两种子项、而且都参与收缩，没有 shrink-0 的标题，也没有内容高各不相同的收缩项。
// 这里按 CSS 的弹性算法手算期望值，补上三条规则（变异测试 F01、F03，2026-11-19）：
//   - 权重是 flex-shrink × 内容高：高的子项分到的不足按比例多，不是平均分；
//   - shrink-0 的子项在多轮分摊里始终冻结在内容高，不会因为别的子项违例、从头再分时被「解冻」；
//   - 违例（被压到下限以下）的冻结在下限，其余的子项就剩下的空间重分。
func TestResolveFlexColumnMixedWeightsAndFrozenItems(t *testing.T) {
	px := pxToLU
	shrink0 := func(h float32) flexNode { return flexNode{base: px(h)} }                              // 标题：shrink-0
	loose := func(h float32) flexNode { return flexNode{base: px(h), shrink: true} }                  // 没有下限，可压到 0
	row := func(h, min float32) flexNode { return flexNode{base: px(h), min: px(min), shrink: true} } // 显式 min-height
	autoMin := func(h float32) flexNode { return flexNode{base: px(h), min: px(h), shrink: true} }    // min-height:auto
	cases := []struct {
		name  string
		nodes []flexNode
		room  float32
		want  [][2]float32 // [上缘, 高]，px
		used  float32
	}{
		{
			// 不足 27px 按权重 36:18 分摊：后一项 -9、前一项 -18。平均分（各 -13.5）会得到 22.5 与 4.5。
			name:  "权重按内容高比例",
			nodes: []flexNode{loose(36), loose(18)}, room: 27,
			want: [][2]float32{{0, 18}, {18, 9}}, used: 27,
		},
		{
			// 标题 54（shrink-0）、两行 36（下限 32）、一个 min:auto 的 20：第一轮三项都被压到各自下限以下（25.83、25.83、14.35），
			// 全部冻结在下限，总高 138 超出容器 120，要滚动；标题始终是 54，不会被拿去分摊那 18px 的缺口。
			name:  "shrink-0 的标题在多轮之后仍保持内容高",
			nodes: []flexNode{shrink0(54), row(36, 32), row(36, 32), autoMin(20)}, room: 120,
			want: [][2]float32{{0, 54}, {54, 32}, {86, 32}, {118, 20}}, used: 138,
		},
		{
			// 标题 40、行 34（下限 32）、行 60（下限 32），容器 110：第一轮 60 的行分到 -15.3（44.7，没违例），
			// 34 的行分到 -8.7（25.3，违例）冻结在 32；第二轮剩 110-40-32 = 38 给 60 的行，它收到 38（-22）。
			name:  "违例的冻结在下限，其余的重分",
			nodes: []flexNode{shrink0(40), row(34, 32), row(60, 32)}, room: 110,
			want: [][2]float32{{0, 40}, {40, 32}, {72, 38}}, used: 110,
		},
	}
	for _, c := range cases {
		nodes := append([]flexNode(nil), c.nodes...)
		used := resolveFlexColumn(nodes, px(c.room), px(c.room))
		if used != px(c.used) {
			t.Errorf("%s：总高 %v px，期望 %v px", c.name, luToPx(used), c.used)
		}
		for i, n := range nodes {
			if got := [2]int32{n.top, n.size}; got != [2]int32{px(c.want[i][0]), px(c.want[i][1])} {
				t.Errorf("%s：第 %d 项 [上缘 高] = [%v %v] px，期望 %v px", c.name, i, luToPx(n.top), luToPx(n.size), c.want[i])
			}
		}
	}
}

// 取整规则：从最后一项往前，逐项取「剩余 ÷ 剩余权重」四舍五入、半数远离零。
func TestRoundDivAway(t *testing.T) {
	cases := []struct{ num, den, want int64 }{
		{-1, 2, -1}, {1, 2, 1}, {-3, 2, -2}, {3, 2, 2}, {-4571, 1000, -5}, {4571, 1000, 5}, {-4499, 1000, -4}, {0, 5, 0}, {-32, 7, -5},
	}
	for _, c := range cases {
		if got := roundDivAway(c.num, c.den); got != c.want {
			t.Errorf("roundDivAway(%d, %d) = %d，期望 %d", c.num, c.den, got, c.want)
		}
	}
}

// flexPageCase 是一页里的一例：容器定高 room，里面按顺序声明的子项。
type flexPageCase struct {
	room  float32
	items []FlexItem
	high  []float32 // 各子项的内容高
}

// 在无头会话里真排一遍：自定义版面把子项的高度、上缘和外边距按解算结果摆好，与渲染层实测一致。
// 取探针里的几例（带小数的容器高），每例放进一个定高的列。
func TestFlexColumnLaysOutThroughViva(t *testing.T) {
	probe := loadFlexShrinkProbe(t)
	var picks []flexShrinkCase
	for _, c := range probe {
		var rows int
		for _, isRow := range c.isRow {
			if isRow {
				rows++
			}
		}
		// 挑行数不多的，且容器高落在「部分收缩」「压到下限」「溢出」里；同一形状取少数几例，免得页面过高
		if rows >= 3 && rows <= 7 && len(c.nodes) <= 12 && c.room > 0 && c.room < 500*luPerPx {
			picks = append(picks, c)
		}
	}
	// 每 N 例取一例，凑到 9 例左右
	step := max(len(picks)/9, 1)
	var cases []flexShrinkCase
	for i := 0; i < len(picks) && len(cases) < 9; i += step {
		cases = append(cases, picks[i])
	}
	if len(cases) < 6 {
		t.Fatalf("可用的探针例太少：%d", len(cases))
	}

	var layouts []*FlexColumn
	var total float32
	for _, c := range cases {
		total += luToPx(c.room) + 20
		layouts = append(layouts, &FlexColumn{})
	}
	h := openPage(t, still(), 400, int(total)+40, func(l *Look, c ui.C) {
		ui.Column(c).Children(func() {
			for ci, fc := range cases {
				fx := layouts[ci]
				fx.Begin(luToPx(fc.room))
				ui.Column(c.Key(fmt.Sprintf("case%d", ci))).Height(luToPx(fc.room) + 20).Children(func() {
					fx.Box(c.Key("fx")).Children(func() {
						for ni, n := range fc.nodes {
							it := FlexItem{Shrink: true, MarginBottom: luToPx(n.mb), MarginTopAuto: n.auto}
							if fc.isRow[ni] {
								it.MinHeight = luToPx(n.min)
							} else {
								it.MinAuto = true
							}
							if !n.auto {
								it.MarginTop = luToPx(n.mt)
							}
							fx.Item(it)
							name := fmt.Sprintf("c%di%d", ci, ni)
							cc := c.Key(name)
							// 子项的内容高：用一个定高的孩子撑起外层（外层本身的高由版面决定）
							ui.Column(cc).HitWith(ui.ID(cc), name, gfx.HitOptions{Role: gfx.RoleButton}).Children(func() {
								ui.Row(c.Key(name + "-in")).Height(luToPx(n.base)).Width(10).Children(func() {})
							})
						}
					})
				})
			}
		})
	})

	var yOffset float32
	for ci, fc := range cases {
		if layouts[ci].Mismatch() {
			t.Errorf("第 %d 例：登记的子项数与声明的孩子数不符", ci)
		}
		for ni, want := range fc.want {
			n, ok := h.node(gfx.RoleButton, fmt.Sprintf("c%di%d", ci, ni))
			if !ok {
				t.Fatalf("找不到第 %d 例第 %d 项", ci, ni)
			}
			gotTop := unitsOf(t, n.Bounds.Y-yOffset)
			gotH := unitsOf(t, n.Bounds.Height)
			if gotTop != want[0] || gotH != want[1] {
				t.Errorf("第 %d 例（容器高 %d 单位）第 %d 项 [上缘 高]：期望 %v 得到 [%d %d]", ci, fc.room, ni, want, gotTop, gotH)
			}
			// align-items: stretch：孩子撑满容器的宽度（容器占满页面的 400）。
			if n.Bounds.Width != 400 || n.Bounds.X != 0 {
				t.Errorf("第 %d 例第 %d 项应撑满容器宽度 [x=0 w=400]，得到 [x=%v w=%v]", ci, ni, n.Bounds.X, n.Bounds.Width)
			}
		}
		yOffset += luToPx(fc.room) + 20
	}
}

// 登记的子项数与声明的孩子数不符：退回「全部不收缩」，不崩，并报告不符。
func TestFlexColumnMismatchFallsBackToNoShrink(t *testing.T) {
	fx := &FlexColumn{}
	h := openPage(t, still(), 300, 200, func(l *Look, c ui.C) {
		fx.Begin(60) // 容器很矮：若按行收缩，行会被压到 32
		fx.Box(c.Key("fx")).Children(func() {
			fx.Item(FlexItem{Shrink: true, MinHeight: 32}) // 只登记了一个，实际声明两个
			for i := 0; i < 2; i++ {
				name := fmt.Sprintf("m%d", i)
				cc := c.Key(name)
				ui.Row(cc).Height(36).HitWith(ui.ID(cc), name, gfx.HitOptions{Role: gfx.RoleButton}).Children(func() {})
			}
		})
	})
	if !fx.Mismatch() {
		t.Errorf("子项数不符应被报告")
	}
	for i := 0; i < 2; i++ {
		n, ok := h.node(gfx.RoleButton, fmt.Sprintf("m%d", i))
		if !ok || n.Bounds.Height != 36 {
			t.Errorf("退回时第 %d 个孩子应保持 36 的高度：%v %v", i, ok, n.Bounds.Height)
		}
	}
}
