package kit

import (
	"math"

	"github.com/vivaui/viva/ui"
)

// FlexItem 是 FlexColumn 里一个子项的 CSS flex 属性（渲染层 Tailwind 类的对应）。零值是 shrink-0、没有显式下限、没有外边距：
// 子项保持内容高。
type FlexItem struct {
	// Shrink 为 true 表示 flex-shrink: 1（CSS 的默认值）；零值对应 Tailwind 的 shrink-0。
	Shrink bool
	// MinHeight 是显式的 min-height（逻辑像素，如 min-h-[32px]）：内容高低于它时撑到它，内容超出容器时压到它为止。
	// 0 表示没有显式下限（min-height: 0，如 overflow-hidden 的元素可以压到 0）。
	MinHeight float32
	// MinAuto 对应 min-height: auto：下限是内容高——CSS 对非滚动元素的默认值，所以子项不会被压到内容以下。
	// 为 true 时忽略 MinHeight。注意它仍然「参与」收缩的计算（权重 = 内容高），只是分到的份额会被下限顶回去，见 FlexColumn 的说明。
	MinAuto bool
	// MarginTop 与 MarginBottom 是子项的外边距（mt-4、mb-1 这类）。Viva 的 CustomLayout 不应用子项自己的 Margin，
	// 由这里按 CSS 的规则计入总高与位置。
	MarginTop, MarginBottom float32
	// MarginTopAuto 对应 mt-auto：容器有富余高度时，富余全部归这个子项的上外边距（把它推到容器末端）；MarginTop 此时忽略。
	MarginTopAuto bool
}

// FlexColumn 复刻渲染层 `flex h-full min-h-0 flex-col overflow-y-auto` 的列：容器有定高（滚动区的视口高），
// 内容总高超出时，按 CSS Flexbox 的「解析弹性长度」把不足分摊给 flex-shrink 为 1 的子项；压到下限还放不下才滚动。
//
// Viva 的滚动区从不收缩它的孩子（内容超出就滚动），与渲染层的这条规则不同：知识库侧栏在大字号档下行的内容高是 36px、
// min-h-[32px]，内容多到溢出时渲染层的每一行都被压到 32px（台账 B-034）。FlexColumn 是一个 Viva 的 CustomLayout，
// 放进滚动区里当内容：它按子项的首选高度与登记的 FlexItem 解算，容器高度取内容总高与视口高的较大者。
//
// 解算按 Chromium LayoutNG 的 flex 算法（LineFlexer）的行为实现：规则出自对该算法的了解与渲染层实测的反推，没有逐行对照
// Chromium 源码；数值是 1/64 px 的定点数（probe-flex-shrink.mjs 在渲染层实测 525 例，testdata/flex-shrink-react.json，
// 上缘与高度逐单位一致，这批实测才是准绳）：
//   - 每子项的「份额」= 自己的权重（flex-shrink × 内容高）÷ 自己及其之前所有未冻结子项的权重和；
//   - 从最后一项往前，每项取 round(剩余空间 × 份额)（半数远离零），剩余空间随之扣掉；第一个未冻结子项取走全部剩余，保证总和对得上；
//   - 被下限顶住的子项（min-height 违例）冻结在下限，其余的从头再分一遍，直到没有违例。
//
// 要点是 min-height:auto 的子项（标题、说明、页签这类）并不「不参与」：它们带着权重参加每一轮，只是分到非零份额时会被顶回内容高、
// 随即冻结；分到的份额四舍五入为 0 的则一直留在未冻结的行列里，继续占着权重。丢掉这一点，容器只差几个 1/64 px 时
// 各行的取整余数就会错位一行（探针里 38 与 40 行、缺口 0.5 px 的几例）。
//
// 用法（每帧、在滚动区的声明里）：
//
//	s.flex.Begin(view.Viewport)
//	s.flex.Box(c.Key("body")).Children(func() {
//		s.flex.Item(kit.FlexItem{})        // 紧接着声明的那个孩子（一一对应、同序）
//		ui.Row(c.Key("a")).FillWidth()...
//	})
//
// 版面要放在模型里跨帧保留，不要每帧新建；孩子的 Width / Grow / Shrink / Margin 由本版面接管，调用方不要设。
// 孩子的宽度一律撑满容器（flex 列的 align-items: stretch）。
type FlexColumn struct {
	room  float32
	items []FlexItem

	nodes    []flexNode // 草稿：解算用，不在绘制路径上分配
	mismatch bool       // 最近一次版面发现登记的子项数与实际声明的不符
}

// Begin 开始新一帧的声明：room 是容器的定高（滚动区的视口高，首帧布局之前为 0——此时不收缩）；之前登记的子项清空。
func (f *FlexColumn) Begin(room float32) {
	f.room = max(room, 0)
	f.items = f.items[:0]
}

// Item 登记紧接着要声明的那个孩子的 flex 属性：登记与孩子必须一一对应、同序。
func (f *FlexColumn) Item(item FlexItem) { f.items = append(f.items, item) }

// Box 声明容器：占满可用宽度，至少与视口一样高（mt-auto 的子项靠它贴底）。调用方继续链式设置后自己调用 Children。
func (f *FlexColumn) Box(c ui.C) ui.Box {
	return ui.CustomLayout(c, f).FillWidth().MinHeight(f.room)
}

// Mismatch 报告最近一次版面里，登记的子项数是否与实际声明的孩子数不符。不符时版面退回「全部不收缩、没有外边距」，
// 界面上不会崩，但与渲染层有出入；测试里断言它为 false。
func (f *FlexColumn) Mismatch() bool { return f.mismatch }

// flexNode 是一个子项在解算中的状态，尺寸单位都是 1/64 px。
type flexNode struct {
	base   int32 // flex base size：内容高
	min    int32 // 解析后的 min-height：显式值，或 min-height:auto 时等于内容高
	mt, mb int32 // 外边距（mt-auto 的子项 mt 为 0，由 auto 记）
	auto   bool  // margin-top: auto
	shrink bool  // flex-shrink: 1

	size     int32   // 解算结果：目标主尺寸（高）
	top      int32   // 解算结果：上缘，相对容器内容区上缘（外边距之后）
	frozen   bool    // 已冻结：不再参与分摊
	violated bool    // 本轮被下限顶住
	fraction float64 // 份额：自己的权重 ÷ 自己及其之前未冻结子项的权重和
}

// flexUnit 是 1 px 的定点单位数，浮点形式用于份额的计算（LayoutUnit 与 double 的换算）。
const flexUnit = float64(luPerPx)

// resolveFlexColumn 解算各子项的高度与上缘，返回内容总高（含外边距，1/64 px）。room 是容器的定高；room ≤ 0 表示还不知道
// （首帧布局之前），此时不收缩。container 是容器的实际高度（富余空间归 mt-auto 的子项），传 0 表示按 room。
func resolveFlexColumn(nodes []flexNode, room, container int32) int32 {
	// hypothetical 尺寸：内容高夹在 min-height 之内。
	var hypothetical, margins int64
	for i := range nodes {
		n := &nodes[i]
		n.size = max(n.base, n.min)
		n.frozen, n.violated = false, false
		margins += int64(n.mt) + int64(n.mb)
		hypothetical += int64(n.size)
	}

	// 只有总的 hypothetical 尺寸不小于容器才是收缩（否则是增长，而 flex-grow 都是 0，什么都不变）。
	if room > 0 && hypothetical+margins >= int64(room) {
		// 起始冻结：不收缩的，以及内容高低于显式下限的（小字号档的行：内容 28、下限 32，固定在 32）。
		var free int64
		freeze := func(should func(*flexNode) bool) {
			free = int64(room)
			var totalWeight float64
			for i := range nodes {
				n := &nodes[i]
				n.frozen = n.frozen || should(n)
				if n.frozen {
					free -= int64(n.size) + int64(n.mt) + int64(n.mb)
					continue
				}
				n.violated = false
				n.size = max(n.base, n.min)
				weight := float64(n.base) / flexUnit // flex-shrink（1）× 内容高，单位 px
				totalWeight += weight
				n.fraction = weight / totalWeight
				free -= int64(n.base) + int64(n.mt) + int64(n.mb)
			}
		}
		freeze(func(n *flexNode) bool { return !n.shrink || n.base < n.size })

		// 解析弹性长度：每轮从最后一项往前分，有违例就冻结违例的子项、从头再分；轮数不会超过子项数加一。
		for pass := 0; pass <= len(nodes)+1 && free < 0; pass++ {
			var violation int64
			for i := len(nodes) - 1; i >= 0; i-- {
				n := &nodes[i]
				if n.frozen {
					continue
				}
				var extra int64
				if n.fraction == 1.0 {
					extra = free // 第一个未冻结的子项取走全部剩余，保证总和对得上
				} else if e := float64(free) / flexUnit * n.fraction; !math.IsNaN(e) && !math.IsInf(e, 0) {
					extra = int64(math.Round(e * flexUnit)) // 半数远离零
				}
				free -= extra
				size := int64(n.base) + extra
				if size < int64(n.min) {
					violation += int64(n.min) - size
					n.violated = true
					size = int64(n.min)
				}
				n.size = int32(size)
			}
			if violation == 0 {
				break
			}
			freeze(func(n *flexNode) bool { return n.violated })
		}
	}

	var used int64
	for i := range nodes {
		used += int64(nodes[i].size)
	}
	used += margins

	// 位置：mt-auto 的子项拿走富余空间（容器比内容高时）。
	autos := 0
	for i := range nodes {
		if nodes[i].auto {
			autos++
		}
	}
	free := int64(0)
	if autos > 0 {
		if container <= 0 {
			container = room
		}
		free = max(int64(container)-used, 0)
	}
	var y int64
	for i := range nodes {
		n := &nodes[i]
		y += int64(n.mt)
		if n.auto {
			share := roundDivAway(free, int64(autos)) // 只有一个 auto 外边距时就是 free 本身
			y += share
			free -= share
			autos--
		}
		n.top = int32(y)
		y += int64(n.size) + int64(n.mb)
	}
	return int32(used)
}

// roundDivAway 是 num ÷ den 四舍五入，半数远离零（C 的 round）。den > 0。
func roundDivAway(num, den int64) int64 {
	if num >= 0 {
		return (2*num + den) / (2 * den)
	}
	return -((2*-num + den) / (2 * den))
}

// load 把登记的子项与版面给出的首选高度合成解算用的节点。数目不符时全部按「不收缩、无外边距」处理。
func (f *FlexColumn) load(ctx *ui.LayoutContext) []flexNode {
	n := ctx.Count()
	if cap(f.nodes) < n {
		f.nodes = make([]flexNode, n)
	} else {
		f.nodes = f.nodes[:n]
	}
	f.mismatch = len(f.items) != n
	for i := 0; i < n; i++ {
		node := flexNode{base: pxToLU(max(ctx.Height(i), 0))}
		if !f.mismatch {
			it := f.items[i]
			node.shrink = it.Shrink
			node.min = pxToLU(max(it.MinHeight, 0))
			if it.MinAuto {
				node.min = node.base
			}
			node.auto = it.MarginTopAuto
			if !it.MarginTopAuto {
				node.mt = pxToLU(it.MarginTop)
			}
			node.mb = pxToLU(it.MarginBottom)
		}
		f.nodes[i] = node
	}
	return f.nodes
}

// MeasureWidth 是内容想要的宽度：容器占满可用宽度，用不到。
func (f *FlexColumn) MeasureWidth(*ui.LayoutContext) float32 { return 0 }

// PlaceWidth 让每个孩子撑满容器（align-items: stretch）。
func (f *FlexColumn) PlaceWidth(ctx *ui.LayoutContext, bounds ui.Rect) {
	for i := 0; i < ctx.Count(); i++ {
		ctx.SetWidth(i, bounds.Width)
		ctx.SetX(i, bounds.X)
	}
}

// MeasureHeight 是内容的总高：各孩子收缩之后的高度加外边距（容器的最小高度另由 Box 的 MinHeight 保证）。
func (f *FlexColumn) MeasureHeight(ctx *ui.LayoutContext) float32 {
	nodes := f.load(ctx)
	return luToPx(resolveFlexColumn(nodes, pxToLU(f.room), 0))
}

// PlaceHeight 按解算结果摆放每个孩子的高度与上缘；容器比内容高时，富余空间归 mt-auto 的孩子。
func (f *FlexColumn) PlaceHeight(ctx *ui.LayoutContext, bounds ui.Rect) {
	nodes := f.load(ctx)
	resolveFlexColumn(nodes, pxToLU(f.room), pxToLU(bounds.Height))
	for i := range nodes {
		ctx.SetHeight(i, luToPx(nodes[i].size))
		ctx.SetY(i, bounds.Y+luToPx(nodes[i].top))
	}
}

var _ ui.Layout = (*FlexColumn)(nil)
