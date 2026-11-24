package kit

// 壁纸的面积平均缩小（盒式滤波）：逐行流式处理，内存只有 O(目标宽)，所以 8K 大图缩到 1920 也不会像通用缩放库那样
// 先分配「目标宽 × 源高」的浮点中间缓冲（数百兆）。只用于缩小（目标不大于源）；放大交给显示端的双线性采样。

import (
	"image"
	"image/color"
	"math"

	"github.com/vivaui/viva/gfx"
)

// rowFunc 把源图第 y 行（0 起）填成预乘的 8 位 RGBA（dst 长度 = 源宽 × 4）。
type rowFunc func(y int, dst []byte)

// span 是一个目标像素在源图一维上覆盖的区间：从 first 起的若干源像素，权重之和为 1。
type span struct {
	first int
	w     []float32
}

// makeSpans 计算把 sn 个源像素面积平均成 dn 个目标像素的权重（dn <= sn）。
func makeSpans(sn, dn int) []span {
	spans := make([]span, dn)
	step := float64(sn) / float64(dn)
	for i := range spans {
		lo, hi := float64(i)*step, float64(i+1)*step
		first := int(math.Floor(lo))
		last := min(int(math.Ceil(hi))-1, sn-1)
		w := make([]float32, last-first+1)
		for x := first; x <= last; x++ {
			overlap := math.Min(hi, float64(x+1)) - math.Max(lo, float64(x))
			w[x-first] = float32(math.Max(overlap, 0) / step)
		}
		spans[i] = span{first: first, w: w}
	}
	return spans
}

// areaResample 把 sw×sh 的源图缩成 dw×dh（均不大于源），返回紧凑的预乘 RGBA 像素。
func areaResample(sw, sh, dw, dh int, row rowFunc) []byte {
	out := make([]byte, dw*dh*4)
	if dw == sw && dh == sh {
		for y := 0; y < sh; y++ {
			row(y, out[y*dw*4:(y+1)*dw*4])
		}
		return out
	}
	cols, rows := makeSpans(sw, dw), makeSpans(sh, dh)
	line := make([]byte, sw*4)
	hline := make([]float32, dw*4) // 当前源行水平缩小后的结果
	acc := make([]float32, dw*4)
	cached := -1
	horizontal := func(y int) {
		if y == cached {
			return
		}
		cached = y
		row(y, line)
		for i, s := range cols {
			var r, g, b, a float32
			px := line[s.first*4:]
			for k, w := range s.w {
				r += w * float32(px[k*4])
				g += w * float32(px[k*4+1])
				b += w * float32(px[k*4+2])
				a += w * float32(px[k*4+3])
			}
			hline[i*4], hline[i*4+1], hline[i*4+2], hline[i*4+3] = r, g, b, a
		}
	}
	for j, s := range rows {
		clear(acc)
		for k, w := range s.w {
			horizontal(s.first + k)
			for i, v := range hline {
				acc[i] += w * v
			}
		}
		dst := out[j*dw*4 : (j+1)*dw*4]
		for i, v := range acc {
			dst[i] = byte(min(v+0.5, 255))
		}
	}
	return out
}

// imageRows 按图片的内存布局返回读行函数（常见格式走快路径，其余经 At）。b 是图片的边界。
func imageRows(img image.Image) rowFunc {
	b := img.Bounds()
	switch m := img.(type) {
	case *image.YCbCr:
		return func(y int, dst []byte) {
			for x := 0; x < b.Dx(); x++ {
				yi, ci := m.YOffset(b.Min.X+x, b.Min.Y+y), m.COffset(b.Min.X+x, b.Min.Y+y)
				r, g, bl := color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
				dst[x*4], dst[x*4+1], dst[x*4+2], dst[x*4+3] = r, g, bl, 255
			}
		}
	case *image.RGBA:
		return func(y int, dst []byte) {
			off := m.PixOffset(b.Min.X, b.Min.Y+y)
			copy(dst, m.Pix[off:off+b.Dx()*4])
		}
	case *image.NRGBA:
		return func(y int, dst []byte) {
			off := m.PixOffset(b.Min.X, b.Min.Y+y)
			src := m.Pix[off : off+b.Dx()*4]
			for i := 0; i+3 < len(src); i += 4 {
				a := uint32(src[i+3])
				dst[i] = byte((uint32(src[i])*a + 127) / 255)
				dst[i+1] = byte((uint32(src[i+1])*a + 127) / 255)
				dst[i+2] = byte((uint32(src[i+2])*a + 127) / 255)
				dst[i+3] = byte(a)
			}
		}
	case *image.Paletted:
		pal := make([][4]byte, len(m.Palette))
		for i, c := range m.Palette {
			r, g, bl, a := c.RGBA()
			pal[i] = [4]byte{byte(r >> 8), byte(g >> 8), byte(bl >> 8), byte(a >> 8)}
		}
		return func(y int, dst []byte) {
			off := m.PixOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < b.Dx(); x++ {
				if idx := int(m.Pix[off+x]); idx < len(pal) {
					copy(dst[x*4:], pal[idx][:])
				} else {
					copy(dst[x*4:], []byte{0, 0, 0, 0})
				}
			}
		}
	}
	return func(y int, dst []byte) {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA() // 预乘的 16 位
			dst[x*4], dst[x*4+1], dst[x*4+2], dst[x*4+3] = byte(r>>8), byte(g>>8), byte(bl>>8), byte(a>>8)
		}
	}
}

// bitmapRows 读已有位图的行（四个通道各自线性处理，BGRA 与 RGBA 的顺序不影响缩小结果）。
func bitmapRows(src gfx.Bitmap) rowFunc {
	return func(y int, dst []byte) { copy(dst, src.Pix[y*src.Stride:y*src.Stride+src.Width*4]) }
}
