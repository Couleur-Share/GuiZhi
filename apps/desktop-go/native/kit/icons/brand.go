package icons

import (
	"encoding/base64"
	"log"
	"sort"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Brand 是品牌图标的键（来源平台 logo、模型厂商图标），常量见 brand_gen.go。
// 与 lucide 的 Name 分开：品牌图标要保持自己的颜色（或按品牌色着色），不跟随文字色。
type Brand string

// brandSource 是一个品牌图标的源：矢量（svg）或内嵌位图（raster）。
type brandSource struct {
	svg    string
	mono   bool // 根 fill 是 currentColor：用文字色（或调用方指定的品牌色）着色；否则按 SVG 自带颜色画
	raster *rasterSource
}

// rasterSource 是内嵌位图型的品牌图标。豆包的 SVG 只是给一张 PNG 套了壳，而 MyGo 不画 SVG 里的 <image>，
// 所以直接解码 PNG：画布是正方形，图片居中，边长 size（画布其余部分透明）。
type rasterSource struct {
	png    string // base64
	canvas float32
	size   float32
}

var (
	brandMu      sync.Mutex
	brandSVGs    = map[Brand]*ui.SVG{}
	brandBitmaps = map[Brand]*ui.Bitmap{}
)

// BrandSVG 返回矢量品牌图标；位图型、未知键或解析失败返回 nil（并记一条日志）。首次使用时才解析。
func BrandSVG(b Brand) *ui.SVG {
	brandMu.Lock()
	defer brandMu.Unlock()
	if svg, ok := brandSVGs[b]; ok {
		return svg
	}
	src, ok := brandSources[b]
	if !ok || src.svg == "" {
		brandSVGs[b] = nil
		return nil
	}
	svg, err := ui.ParseSVG([]byte(src.svg))
	if err != nil {
		log.Printf("[icons] 解析品牌图标 %q 失败：%v", b, err)
		svg = nil
	}
	brandSVGs[b] = svg
	return svg
}

// BrandBitmap 返回位图型品牌图标，以及图片边长占画布边长的比例；非位图型、未知键或解码失败返回 nil。
func BrandBitmap(b Brand) (bm *ui.Bitmap, frac float32) {
	brandMu.Lock()
	defer brandMu.Unlock()
	src, ok := brandSources[b]
	if !ok || src.raster == nil {
		return nil, 0
	}
	frac = src.raster.size / src.raster.canvas
	if bm, ok := brandBitmaps[b]; ok {
		return bm, frac
	}
	data, err := base64.StdEncoding.DecodeString(src.raster.png)
	if err == nil {
		bm, err = ui.DecodeBitmap(data)
	}
	if err != nil {
		log.Printf("[icons] 解码品牌图标 %q 失败：%v", b, err)
		bm = nil
	}
	brandBitmaps[b] = bm
	return bm, frac
}

// BrandMono 报告品牌图标是否单色（用文字色或品牌色着色，而不是自带颜色）。
func BrandMono(b Brand) bool { return brandSources[b].mono }

// Brands 返回全部品牌键（排序后；测试与对账用）。
func Brands() []Brand {
	out := make([]Brand, 0, len(brandSources))
	for b := range brandSources {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
