package parity

import (
	"image"
	"math"
)

// markInkContext 给墨迹问题记下发生处的两项事实（Issue.Shared、Issue.Clipped），供已登记的偏差按事实匹配，
// 而不是按场景、字号这类间接线索放宽：同一类偏差出现在别处、前提不成立时，照常判定。
func markInkContext(is *Issue, f *Facts, rect Box, area image.Rectangle, clip []float64) {
	is.Shared = sharedWithFractionalText(f, rect, area)
	is.Clipped = clipTruncates(rect, clip)
}

// sharedWithFractionalText 报告量墨迹的区域 area 里是否整个包着另一行 React 文字（外框与 self 不同），
// 且那行文字的左缘落在小数像素上（台账 G-018）。
//
// 这时区域里的墨迹是两行文字的合集：Viva 整像素落位、渲染层按小数左缘定位，水平最多差半个像素，
// 这是被包着的那一行的位移，却会让外面这行文字的剖面对齐偏 1 列（吸附列压住的表头里，「更新时间」整个在「操作」底下）。
func sharedWithFractionalText(f *Facts, self Box, area image.Rectangle) bool {
	if f == nil {
		return false
	}
	const eps = 1e-3
	for _, r := range f.TextRects {
		if r.W() <= 0 || r.H() <= 0 || sameBox(r, self, eps) {
			continue
		}
		inside := r.X() >= float64(area.Min.X)-eps && r.Right() <= float64(area.Max.X)+eps &&
			r.Y() >= float64(area.Min.Y)-eps && r.Bottom() <= float64(area.Max.Y)+eps
		if inside && math.Abs(r.X()-math.Round(r.X())) > eps {
			return true
		}
	}
	return false
}

// clipTruncates 报告 clip（看得见的区域，Text.Clip / Paint.Clip）是否把 rect 截掉了一截：
// 比外框矮或窄超过 1 像素。差 1 像素以内是半个像素行压在裁剪线上的取整，不算被截。clip 为空表示没被裁。
func clipTruncates(rect Box, clip []float64) bool {
	if len(clip) != 4 {
		return false
	}
	return clip[2] < rect.W()-1 || clip[3] < rect.H()-1
}

func sameBox(a, b Box, eps float64) bool {
	return math.Abs(a.X()-b.X()) < eps && math.Abs(a.Y()-b.Y()) < eps &&
		math.Abs(a.W()-b.W()) < eps && math.Abs(a.H()-b.H()) < eps
}
