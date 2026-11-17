package parity

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/vivaui/viva/headless"

	"guizhi-mygo-shell/viva/kit"
)

// 遮罩背后的模糊（台账 A-007）
//
// 渲染层的模态遮罩（ConfirmDialog 的 backdrop-blur-sm，Modal 的 backdrop-blur-md）把背后的页面做高斯模糊；
// Viva 没有「模糊已画内容」的原语，遮罩背后的页面保持清晰。这是真实的、用户看得见的偏差（台账 A-007，状态：开）。
//
// 对照工具不能因此放过整个遮罩区：页面内容在遮罩背后的位置、颜色，遮罩色，以及面板阴影压在背景上的样子，
// 仍然应该一致。做法是在 Viva 一侧精确模拟渲染层的合成：
//
//	渲染层 = 面板 ⊕ 阴影 ⊕ 遮罩色 ⊕ 模糊(页面)
//	Viva   = 面板 ⊕ 阴影 ⊕ 遮罩色 ⊕       页面
//
// 1. 用同一个场景再渲染一次「对话框打开之前」的页面（BaseSteps），叠上遮罩色得到 Sharp（Viva 里遮罩背后的清晰背景），
//    再按渲染层的标准差高斯模糊得到 Blurred（渲染层里遮罩背后的背景；遮罩是均匀的半透明色，模糊是线性的，
//    「先叠遮罩再模糊」与「先模糊再叠遮罩」等价）。
// 2. 面板之外的像素：Viva 图 = Sharp × (1 − 阴影衰减)，所以衰减 = Viva / Sharp（逐通道，阴影只会变暗，上限 1），
//    渲染层的对应像素应为 Blurred × 衰减。把这张预测图与 React 图逐像素比较。
// 3. 面板之内：原样比较（面板不被模糊）。
//
// 阈值不变（容忍度与差异比例照旧）；背景上的文字与图标经模糊后抗锯齿差异已被抹平，所以面板之外不再遮掉文字与图标；
// 面板阴影的形状、颜色与位置也因此纳入逐像素比较，不需要像「遮掉面板外一圈」那样放弃这一圈的覆盖。

// BackdropSpec 是场景对「遮罩背后被模糊」的登记。
type BackdropSpec struct {
	// Sigma 是模糊的标准差（逻辑像素，即 CSS blur() 的取值）：ConfirmDialog 的 backdrop-blur-sm 是 4，Modal 的 backdrop-blur-md 是 12。
	Sigma float64
	// ScrimAlpha 是遮罩色（背景色令牌）的不透明度：bg-background/60 是 0.6。
	ScrimAlpha float32
	// PanelRole、PanelName 是遮罩之上、不被模糊的面板在 Viva 可访问树里的角色与名称（渲染层是 alertdialog / dialog，Viva 一侧映射成 group）。
	PanelRole, PanelName string
	// BaseSteps 把页面带到「对话框打开之前」的状态：与 Scene.Steps 里打开对话框之前的那部分一致，空表示不需要任何操作。
	BaseSteps func(s *headless.Session) error
}

// Backdrop 是一次比较里已解析的模糊区域。
type Backdrop struct {
	Sigma float64
	// Panel 是遮罩之上不被模糊的面板外框（区域内坐标）。
	Panel Box
	// Sharp 是 Viva 里遮罩背后的清晰背景（页面叠上遮罩色），Blurred 是它按 Sigma 模糊的结果；只在带图片的比较里有值。
	Sharp, Blurred *image.RGBA
}

// Resolve 在 Viva 事实里找到面板，得到本次比较用的 Backdrop；withImage 为真时再渲染一次对话框打开之前的页面，算出清晰与模糊的背景。
// 找不到面板是场景写错了，直接报错而不是悄悄不比较。
func (s *BackdropSpec) Resolve(scene Scene, viva *Facts, withImage bool) (*Backdrop, error) {
	if s == nil {
		return nil, nil
	}
	b := &Backdrop{Sigma: s.Sigma}
	found := false
	for _, n := range viva.Nodes {
		if n.Role != s.PanelRole || NormText(n.Name) != s.PanelName {
			continue
		}
		if box, ok := n.BoxOK(); ok {
			b.Panel, found = box, true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("Viva 事实里没有 %s「%s」，无法确定遮罩之上的面板", s.PanelRole, s.PanelName)
	}
	if !withImage {
		return b, nil
	}
	base := scene
	base.Steps, base.Prepare, base.Backdrop = s.BaseSteps, nil, nil
	_, page, err := Render(base, viva.Variant, viva.Region)
	if err != nil {
		return nil, fmt.Errorf("渲染对话框打开之前的页面失败：%w", err)
	}
	look := kit.NewLook(viva.Variant.Spec())
	b.Sharp = scrimOver(page, look.T.Background.R, look.T.Background.G, look.T.Background.B, float64(s.ScrimAlpha))
	b.Blurred = gaussianBlur(b.Sharp, s.Sigma)
	return b, nil
}

// scrimOver 把遮罩色（r、g、b，不透明度 alpha）叠在页面图上，每通道四舍五入，与浏览器合成同一像素时的 source-over 一致。
func scrimOver(page image.Image, r, g, b uint8, alpha float64) *image.RGBA {
	pb := page.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, pb.Dx(), pb.Dy()))
	mix := func(scrim uint8, under uint8) uint8 {
		return uint8(math.Round(float64(scrim)*alpha + float64(under)*(1-alpha)))
	}
	for y := 0; y < pb.Dy(); y++ {
		for x := 0; x < pb.Dx(); x++ {
			p := rgba8(page, pb.Min.X+x, pb.Min.Y+y)
			out.SetRGBA(x, y, color.RGBA{R: mix(r, p[0]), G: mix(g, p[1]), B: mix(b, p[2]), A: 0xff})
		}
	}
	return out
}

// panelRect 是面板的整像素外框。
func (b *Backdrop) panelRect() image.Rectangle {
	return image.Rect(int(math.Floor(b.Panel.X())), int(math.Floor(b.Panel.Y())), int(math.Ceil(b.Panel.Right())), int(math.Ceil(b.Panel.Bottom())))
}

// Blurs 报告一块区域是否（至少部分）在被模糊的背景上：只有完全落在面板外框里的区域才是清晰的。
// 对照图标墨迹时用：背后模糊的图标与清晰的图标没法比墨迹；被面板遮住一部分的图标，量墨迹的区域里也混着面板，同样没法比。
func (b *Backdrop) Blurs(r Box) bool {
	if b == nil {
		return false
	}
	p := b.Panel
	inside := r.X() >= p.X()-1 && r.Right() <= p.Right()+1 && r.Y() >= p.Y()-1 && r.Bottom() <= p.Bottom()+1
	return !inside
}

// backdropComposite 返回「实际拿去比较的 Viva 图」：面板内是原图，面板外是按渲染层合成方式预测的图（见文件头）。
// 同时改写 mask：面板外的文字与图标遮罩去掉（背景已模糊，抗锯齿差异被抹平）；面板内的遮罩保持。
func backdropComposite(viva image.Image, b *Backdrop, mask []bool, w, h int) image.Image {
	panel := b.panelRect()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	vb := viva.Bounds()
	predict := func(v, sharp, blurred uint8) uint8 {
		f := 1.0 // 阴影只会让背景变暗，衰减不超过 1
		if sharp > 0 {
			f = math.Min(float64(v)/float64(sharp), 1)
		}
		return round8(float32(float64(blurred) * f))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := rgba8(viva, vb.Min.X+x, vb.Min.Y+y)
			if image.Pt(x, y).In(panel) {
				out.SetRGBA(x, y, color.RGBA{R: v[0], G: v[1], B: v[2], A: 0xff})
				continue
			}
			mask[y*w+x] = false
			s, bl := b.Sharp.RGBAAt(x, y), b.Blurred.RGBAAt(x, y)
			out.SetRGBA(x, y, color.RGBA{
				R: predict(v[0], s.R, bl.R), G: predict(v[1], s.G, bl.G), B: predict(v[2], s.B, bl.B), A: 0xff,
			})
		}
	}
	return out
}

// gaussianBlur 对不透明图片做高斯模糊（sRGB 通道上逐通道做，与 Chromium 的滤镜口径一致），边缘按镜像延拓
// （超出边界的取样取对称位置，边缘像素重复一次，即 Skia 的 kMirror）。
func gaussianBlur(src image.Image, sigma float64) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	radius := int(math.Ceil(3 * sigma))
	kernel := make([]float32, 2*radius+1)
	var sum float64
	for i := -radius; i <= radius; i++ {
		v := math.Exp(-float64(i*i) / (2 * sigma * sigma))
		kernel[i+radius] = float32(v)
		sum += v
	}
	for i := range kernel {
		kernel[i] /= float32(sum)
	}
	mirror := func(i, n int) int {
		for i < 0 || i >= n {
			if i < 0 {
				i = -i - 1
			}
			if i >= n {
				i = 2*n - i - 1
			}
		}
		return i
	}
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	// 横向：结果存成 float32，免得两次取整累积误差。
	mid := make([]float32, w*h*3)
	for y := 0; y < h; y++ {
		row := rgba.Pix[y*rgba.Stride:]
		for x := 0; x < w; x++ {
			var r, g, bl float32
			for k := -radius; k <= radius; k++ {
				p := mirror(x+k, w) * 4
				wt := kernel[k+radius]
				r += wt * float32(row[p])
				g += wt * float32(row[p+1])
				bl += wt * float32(row[p+2])
			}
			o := (y*w + x) * 3
			mid[o], mid[o+1], mid[o+2] = r, g, bl
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, bl float32
			for k := -radius; k <= radius; k++ {
				p := (mirror(y+k, h)*w + x) * 3
				wt := kernel[k+radius]
				r += wt * mid[p]
				g += wt * mid[p+1]
				bl += wt * mid[p+2]
			}
			o := y*out.Stride + x*4
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = round8(r), round8(g), round8(bl), 0xff
		}
	}
	return out
}

func round8(v float32) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	}
	return uint8(v + 0.5)
}
