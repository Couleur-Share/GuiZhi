package kit

// ScrollNearest 复刻 scrollIntoView({ block: "nearest", inline: "nearest" }) 在一个方向上的取值（CSSOM View 的
// scroll an element into view 算法）：元素的两条边 [a, b] 与滚动视口的两条边 [sa, sb]（同一坐标系）→ 要加到滚动偏移上的量。
//
//   - 两端都在视口之内：不动；两端都在视口之外（元素把视口整个盖住）：不动；
//   - 起边在视口之外且元素比视口窄，或终边在视口之外且元素比视口宽：对齐起边（元素更宽时看到的是它的开头）；
//   - 终边在视口之外且元素比视口窄，或起边在视口之外且元素比视口宽：对齐终边。
//
// 所以表格行（比视口宽时）被滚进视口，横向总是把行首对齐到视口左缘；纵向则是「出去多少补多少」。
func ScrollNearest(a, b, sa, sb float32) float32 {
	width, size := b-a, sb-sa
	switch {
	case a < sa && b > sb:
		return 0
	case a < sa && width < size, b > sb && width > size:
		return a - sa
	case b > sb && width < size, a < sa && width > size:
		return b - sb
	}
	return 0
}
