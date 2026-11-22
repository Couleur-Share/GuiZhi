package reader

import (
	"errors"
	"fmt"
	"math"

	"github.com/vivaui/viva/gfx"
)

// 阅读区的小数像素版面（台账 B-068）。
//
// 渲染层里阅读区的起点与尺寸可以带小数（根字号 18 px 时是 698, 222.75, 582×577.25），Chromium 按小数起点排版、再把盒子吸附到设备像素。
// 岛是真子窗口，Viva 的 Windows 平台把子窗口边界按物理像素四舍五入（Left=round(X·scale)，Right=round((X+W)·scale)，
// web_windows.go 的 layout），页面从整数像素起点画——小数部分丢了，吸附结果与渲染层不同。
//
// 做法：子窗口保持 Viva 的取整（绝不向外扩大，免得盖住相邻边线的像素），宿主把「真实起点减去取整起点」（可为负）与精确的逻辑尺寸
// 发给页面，页面把根节点用相对定位挪这个偏移、按精确尺寸设宽高（reader/layout.ts）。用版面偏移而不是 transform：
// 版面偏移之后盒子照常吸附到设备像素，与渲染层同一条路径；transform 会让盒子落在小数设备像素上被抗锯齿成一圈灰边。
//
// 偏移由宿主按画布的 PixelScale 与 Viva 同一个取整公式算出，页面不读 devicePixelRatio：页面的 DPR 与 Viva 的缩放在换显示器的瞬间可能短暂不一致，
// 由宿主一处算准更稳，页面一侧没有任何取整逻辑。

// Layout 是阅读区在小数像素上的版面（逻辑像素）：DX、DY 是真实起点减去子窗口取整起点（页面把根节点挪这么多），
// Width、Height 是真实的逻辑尺寸（页面按它设根节点的宽高，超出子窗口的部分被窗口裁掉）。
//
// 阅读区正好落在设备像素上时是零值 Layout{}（合法的版面尺寸都为正，所以零值不会与真版面混淆）：宿主不发消息，
// 页面保持默认的满视口，没有任何额外样式。每帧的比较与暂存都是值拷贝，没有分配。
type Layout struct {
	DX     float64 `json:"dx"`
	DY     float64 `json:"dy"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

const (
	// alignedEpsilon 是「落在设备像素上」的容差（设备像素）：float32 的坐标在几千的量级上有 1e-4 的噪声，不能把它当成小数起点。
	alignedEpsilon = 1e-3
	// maxLayoutOffset 是偏移的上限（逻辑像素）：取整最多差半个设备像素，缩放不小于 0.5 时不会超过 1。
	maxLayoutOffset = 1.0
	maxLayoutSize   = 20000.0
)

// vivaRound 与 Viva Windows 平台的取整一致（web_windows.go 的 round：四舍五入，负数对称）。
func vivaRound(value float32) float32 {
	if value < 0 {
		return -vivaRound(-value)
	}
	return float32(int64(value + 0.5))
}

// LayoutFor 算出阅读区 bounds（逻辑像素）在显示缩放 scale 下的小数像素版面；四条边都在设备像素上时返回零值。
// 取整照 Viva 的写法逐字复刻（float32 的先加后乘、四舍五入），所以这里认为「对齐」的边，子窗口就一定恰好落在上面。
func LayoutFor(bounds gfx.Rect, scale float32) Layout {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return Layout{} // Canvas.NativeView 不摆尺寸为零的视图，没有版面可言
	}
	if scale <= 0 {
		scale = 1
	}
	left, top := vivaRound(bounds.X*scale), vivaRound(bounds.Y*scale)
	right, bottom := vivaRound((bounds.X+bounds.Width)*scale), vivaRound((bounds.Y+bounds.Height)*scale)
	off := func(logical, device float32) float64 { return float64(logical)*float64(scale) - float64(device) }
	dx, dy := off(bounds.X, left), off(bounds.Y, top)
	dr, db := off(bounds.X+bounds.Width, right), off(bounds.Y+bounds.Height, bottom)
	aligned := func(v float64) bool { return math.Abs(v) <= alignedEpsilon }
	if aligned(dx) && aligned(dy) && aligned(dr) && aligned(db) {
		return Layout{}
	}
	layout := Layout{Width: float64(bounds.Width), Height: float64(bounds.Height)}
	if !aligned(dx) {
		layout.DX = dx / float64(scale)
	}
	if !aligned(dy) {
		layout.DY = dy / float64(scale)
	}
	return layout
}

// IsZero 报告这是不是「没有版面」（阅读区落在设备像素上）。
func (l Layout) IsZero() bool { return l == Layout{} }

// Validate 检查一份真版面（非零值）：数值有限、偏移在取整误差之内、尺寸为正且不超过页面接受的上限。页面对坏值同样会丢弃，宿主先拦下才能给出原因。
func (l Layout) Validate() error {
	for _, v := range []float64{l.DX, l.DY, l.Width, l.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("reader: 版面数值不是有限数")
		}
	}
	if math.Abs(l.DX) > maxLayoutOffset || math.Abs(l.DY) > maxLayoutOffset {
		return fmt.Errorf("reader: 版面偏移 (%v, %v) 超出 ±%v", l.DX, l.DY, maxLayoutOffset)
	}
	if l.Width <= 0 || l.Height <= 0 || l.Width > maxLayoutSize || l.Height > maxLayoutSize {
		return fmt.Errorf("reader: 版面尺寸 %vx%v 超出 (0, %v]", l.Width, l.Height, maxLayoutSize)
	}
	return nil
}
