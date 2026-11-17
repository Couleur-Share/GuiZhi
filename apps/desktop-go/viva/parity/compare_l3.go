package parity

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"sort"
)

// compareL3 做像素比较：文字区域遮掉（两个引擎的字形抗锯齿不同），其余逐像素比较，并画出三联图。
func compareL3(rep *Report, in Inputs, th Thresholds) *image.RGBA {
	rep.L3.MaxDiffRatio = th.Pixel.MaxDiffRatio
	if in.ReactImg == nil || in.VivaImg == nil {
		rep.add(LayerL3, "image", rep.Scene, "缺少图片，无法做像素比较")
		return nil
	}
	rb, vb := in.ReactImg.Bounds(), in.VivaImg.Bounds()
	rep.L3.Width, rep.L3.Height = rb.Dx(), rb.Dy()
	if rb.Dx() != vb.Dx() || rb.Dy() != vb.Dy() {
		rep.add(LayerL3, "size", rep.Scene, "图片尺寸不一致：React %dx%d，Viva %dx%d", rb.Dx(), rb.Dy(), vb.Dx(), vb.Dy())
		return nil
	}
	w, h := rb.Dx(), rb.Dy()
	mask := textMask(in, w, h, th.Pixel.MaskPad)
	viva := in.VivaImg
	if in.Backdrop != nil {
		// 遮罩背后被模糊（台账 A-007）：面板外换成按渲染层合成方式预测的图（模糊的背景叠上面板阴影），不遮文字与图标。
		viva = backdropComposite(in.VivaImg, in.Backdrop, mask, w, h)
		vb = viva.Bounds()
	}
	diff := make([]bool, w*h)
	tol := th.Pixel.Tolerance
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if mask[i] {
				rep.L3.Masked++
				continue
			}
			rep.L3.Compared++
			a := rgba8(in.ReactImg, rb.Min.X+x, rb.Min.Y+y)
			b := rgba8(viva, vb.Min.X+x, vb.Min.Y+y)
			if rgbDistance(a, b) > tol {
				diff[i] = true
				rep.L3.Diff++
			}
		}
	}
	if rep.L3.Compared > 0 {
		rep.L3.Ratio = float64(rep.L3.Diff) / float64(rep.L3.Compared)
	}
	rep.L3.Clusters = diffClusters(diff, w, h, 8)
	if rep.L3.Ratio > th.Pixel.MaxDiffRatio {
		rep.add(LayerL3, "pixel", rep.Scene, "差异像素 %d / %d = %.4f%%，上限 %.4f%%", rep.L3.Diff, rep.L3.Compared, rep.L3.Ratio*100, th.Pixel.MaxDiffRatio*100)
	}
	return montage(in.ReactImg, viva, mask, diff, w, h)
}

// diffClusters 把差异像素按 8 邻接分片，返回像素最多的 limit 片。
func diffClusters(diff []bool, w, h, limit int) []Cluster {
	seen := make([]bool, len(diff))
	var all []Cluster
	var stack []int
	for start := range diff {
		if !diff[start] || seen[start] {
			continue
		}
		seen[start] = true
		stack = append(stack[:0], start)
		c := Cluster{X: start % w, Y: start / w, Width: 1, Height: 1}
		x1, y1 := c.X, c.Y
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := i%w, i/w
			c.Pixels++
			c.X, c.Y = min(c.X, x), min(c.Y, y)
			x1, y1 = max(x1, x), max(y1, y)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || ny < 0 || nx >= w || ny >= h {
						continue
					}
					j := ny*w + nx
					if diff[j] && !seen[j] {
						seen[j] = true
						stack = append(stack, j)
					}
				}
			}
		}
		c.Width, c.Height = x1-c.X+1, y1-c.Y+1
		all = append(all, c)
	}
	sort.Slice(all, func(a, b int) bool { return all[a].Pixels > all[b].Pixels })
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

// textMask 把两侧所有文字行矩形与图标外框外扩 pad 像素，标成遮罩（文字与图标的抗锯齿逐像素不同，改由 L2 按墨迹比较）。
func textMask(in Inputs, w, h, pad int) []bool {
	mask := make([]bool, w*h)
	mark := func(b Box) {
		x0 := int(math.Floor(b.X())) - pad
		y0 := int(math.Floor(b.Y())) - pad
		x1 := int(math.Ceil(b.Right())) + pad
		y1 := int(math.Ceil(b.Bottom())) + pad
		for y := max(y0, 0); y < min(y1, h); y++ {
			for x := max(x0, 0); x < min(x1, w); x++ {
				mask[y*w+x] = true
			}
		}
	}
	for _, r := range in.React.TextRects {
		mark(r)
	}
	for _, t := range in.Viva.Texts {
		for _, r := range t.Rects {
			mark(r)
		}
	}
	for _, r := range maskedIcons(in) {
		mark(r)
	}
	return mask
}

// montage 画三联图：左 React，中 Viva，右差异（差异像素红色，遮罩区淡蓝，其余是淡化的 React）。
func montage(react, viva image.Image, mask, diff []bool, w, h int) *image.RGBA {
	const gap = 8
	out := image.NewRGBA(image.Rect(0, 0, w*3+gap*2, h))
	draw.Draw(out, out.Bounds(), &image.Uniform{C: color.RGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xff}}, image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(0, 0, w, h), react, react.Bounds().Min, draw.Src)
	draw.Draw(out, image.Rect(w+gap, 0, w*2+gap, h), viva, viva.Bounds().Min, draw.Src)
	rb := react.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			base := rgba8(react, rb.Min.X+x, rb.Min.Y+y)
			c := color.RGBA{R: fade(base[0]), G: fade(base[1]), B: fade(base[2]), A: 0xff}
			switch {
			case diff[i]:
				c = color.RGBA{R: 0xff, G: 0x20, B: 0x20, A: 0xff}
			case mask[i]:
				c = color.RGBA{R: 0xd8, G: 0xe6, B: 0xff, A: 0xff}
			}
			out.SetRGBA((w+gap)*2+x, y, c)
		}
	}
	return out
}

func fade(v uint8) uint8 { return uint8(255 - (255-int(v))/4) }

// WritePNG 把图写成 PNG 文件。
func WritePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// LoadPNG 读 PNG 文件。
func LoadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}
