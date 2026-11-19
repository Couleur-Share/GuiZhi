package kit

import (
	"math"

	"github.com/vivaui/viva/ui"
)

// 窗口化的行段（台账 B-035）：知识库侧栏的合集与标签没有数量上限，行数多时 Viva 的界面每帧重新声明，
// 布局求解与绘制随行数超线性增长（100 + 300 行时稳态帧 7.9 ms）；渲染层整表渲染，滚动与悬停不重算布局。
// FlexColumn.Run 让一段连续、同规格的行只声明看得见的几行（视口上下各多几行），其余由两个占位孩子代表：
// 占位孩子在解算里展开成同样多个节点，所以每一行的高度、上缘、收缩取整与整段都声明时分毫不差。
//
// 窗口按「上一次布局的几何」算：这一段第一行的上缘与每行的平均占高，布局之后记下；视口跨度取滚动区上一帧的几何。
// 几何变了（数据增减、字号档变化）而窗口没盖住看得见的行时，布局之后再要一帧。
//
// 前提：FlexColumn 是滚动区里的第一个孩子、上沿对齐内容区 0 点（侧栏就是这样）；段里的行没有外边距。
// 与 RowWindow 的区别：Run 不把滚动区标成「窗口化」，Tab 可达（reach.go）继续工作——窗口总是多声明几行，
// 键盘聚焦跟着滚动，下一个停靠点总在已声明的行里；焦点所在的行被滚出窗口之后不再声明，焦点随之丢失（台账 B-029 的 (3)）。

// FlexRun 描述 FlexColumn 里的一段窗口化的行。
type FlexRun struct {
	// Key 是这一段在列里的名字，唯一；几何的登记与占位孩子的名字都取它。
	Key string
	// N 是这一段的总行数；0 什么也不声明。
	N int
	// Item 是每一行的 flex 属性（Shrink、MinHeight、MinAuto）；不支持外边距与 MarginTopAuto。
	Item FlexItem
	// View 是滚动区上一帧的几何（ScrollAreaView 给出）。
	View ScrollView
	// Overscan 是视口上下各多声明几行；0 取默认（3）。
	Overscan int
	// Estimate 是还没有几何（首帧、这一段刚出现）时一行的估计高度，只决定首帧声明多少行；0 取 32。
	Estimate float32
	// Row 声明第 i 行。Run 已经登记好它的 flex 属性，调用方不要再调 Item。
	Row func(i int)
}

// runState 是一段行的跨帧状态。
type runState struct {
	key string

	// 上一次布局的几何：第一行的上缘（容器内容区坐标）与每行的平均占高。
	top, pitch float64
	valid      bool

	// 本帧声明的窗口与当时的视口跨度，布局之后用来核对窗口是否还盖得住看得见的行。
	declared    bool
	n           int
	first, last int
	lo, hi      float32
}

func (f *FlexColumn) runFor(key string) *runState {
	for i := range f.runs {
		if f.runs[i].key == key {
			return &f.runs[i]
		}
	}
	f.runs = append(f.runs, runState{key: key})
	return &f.runs[len(f.runs)-1]
}

// Run 声明一段窗口化的行：窗口之前的行用一个占位孩子代表，窗口里的行逐个声明（r.Row），窗口之后的行再用一个占位孩子代表。
// 必须在 Box 的 Children 里、按列里的顺序调用。
func (f *FlexColumn) Run(c ui.C, r FlexRun) {
	if r.N <= 0 {
		return
	}
	rs := f.runFor(r.Key)
	overscan := r.Overscan
	if overscan <= 0 {
		overscan = defaultRowOverscan
	}
	lo, hi := r.View.Span()
	first, last := 0, 0
	if rs.valid && rs.pitch > 0 && r.View.Viewport > 0 {
		first, last = runSpan(r.N, lo, hi, rs.top, rs.pitch, overscan)
	} else { // 还没有几何：声明最前面够铺满一屏的几行
		est := float64(r.Estimate)
		if est <= 0 {
			est = 32
		}
		view := float64(r.View.Viewport)
		if view <= 0 {
			view = float64(defaultRowFallback)
		}
		first, last = 0, min(int(math.Ceil(view/est))+overscan, r.N)
	}
	rs.declared, rs.n, rs.first, rs.last, rs.lo, rs.hi = true, r.N, first, last, lo, hi

	row := r.Item
	row.MarginTop, row.MarginBottom, row.MarginTopAuto = 0, 0, false
	row.runKey, row.hidden = r.Key, 0
	if first > 0 {
		f.placeholder(c, row, first, r.Key+"-pre")
	}
	for i := first; i < last; i++ {
		f.items = append(f.items, row)
		r.Row(i)
	}
	if last < r.N {
		f.placeholder(c, row, r.N-last, r.Key+"-post")
	}
}

// placeholder 登记并声明一个占位孩子：高度由版面按它代表的行数摆放，声明时给 0。
func (f *FlexColumn) placeholder(c ui.C, row FlexItem, hidden int, name string) {
	row.hidden = hidden
	f.items = append(f.items, row)
	ui.Slot(c.Key(name)).FillWidth().Shrink(0).Height(0)
}

// runSpan 是第 n 行里与视口跨度 [lo, hi) 相交的行区间，再向两侧各扩 overscan 行：第一行的上缘是 top，每行占 pitch。
// 偏移落在内容之外（列表缩短之后的那一帧）时从末行起；至少一行。
func runSpan(n int, lo, hi float32, top, pitch float64, overscan int) (first, last int) {
	f := min(max(int(math.Floor((float64(lo)-top)/pitch)), 0), n-1)
	l := int(math.Ceil((float64(hi) - top) / pitch))
	return max(f-overscan, 0), min(max(l, f+1)+overscan, n)
}

// noteRuns 在布局之后记下每一段的几何，并核对本帧声明的窗口是否还盖得住看得见的行。
func (f *FlexColumn) noteRuns(nodes []flexNode) {
	if f.mismatch || len(f.runs) == 0 {
		return
	}
	for ri := range f.runs {
		rs := &f.runs[ri]
		if !rs.declared {
			continue
		}
		var count int
		var sum int64
		top := int32(-1)
		for i, it := range f.items {
			if it.runKey != rs.key {
				continue
			}
			sp := f.spans[i]
			if top < 0 {
				top = nodes[sp.first].top
			}
			count += sp.count
			for k := sp.first; k < sp.first+sp.count; k++ {
				sum += int64(nodes[k].size)
			}
		}
		if count == 0 || sum <= 0 {
			rs.valid = false
			continue
		}
		rs.top, rs.pitch, rs.valid = float64(top)/flexUnit, float64(sum)/flexUnit/float64(count), true
		// 看得见的行（不含 overscan）有没有落在声明的窗口之外：几何变了而窗口还是按旧几何算的。
		vf, vl := runSpan(rs.n, rs.lo, rs.hi, rs.top, rs.pitch, 0)
		if vf < rs.first || vl > rs.last {
			f.dirty = true
		}
	}
}

// requestWhenStale 是容器的 Draw：布局之后发现窗口没盖住看得见的行，就再要一帧，让下一帧按新的几何声明。
func (f *FlexColumn) requestWhenStale(d *ui.DrawContext) {
	if f.dirty {
		f.dirty = false
		d.Canvas.RequestFrame()
	}
}
