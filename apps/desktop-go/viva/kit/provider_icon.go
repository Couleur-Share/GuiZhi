package kit

// AI 厂商品牌图标（对应渲染层 components/ui/ModelIcons.tsx 的 getCategoryIcon）。
//
// 品牌 logo 是 15 张 120×120 透明底 PNG（scripts/gen-viva-provider-icons.mjs 由渲染层 SVG 栅格化，产物入库），
// go:embed 嵌入、各解一次。显示时按「边长 × 设备像素比」做面积平均缩小：位图采样没有 mipmap，
// 直接把 120px 的图缩到 16px 画会闪烁发虚；预缩小后一像素对一像素。预缩小的位图按（品牌，像素边长）缓存，
// ID 由这两者确定（不随帧变），原生渲染器据此复用纹理；缓存有上限，超出整体清空（再生成的位图 ID 不变，无副作用）。

import (
	"bytes"
	"embed"
	"image"
	"image/draw"
	"image/png"
	"sync"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

//go:embed assets/providers/*.png
var providerIconFS embed.FS

// providerIconFiles 是品牌分类 → 图标文件名（不含扩展名），与 ModelIcons.tsx 的 CATEGORY_ICON_SRC 一一对应：
// Spark 与 Hunyuan 共用 tencent-cloud-ti；ERNIE 在渲染层映射到空串（没有图标，退成首字母圆），所以不在表里。
var providerIconFiles = map[string]string{
	"GPT":          "openai",
	"Azure OpenAI": "azureai",
	"Claude":       "anthropic",
	"Gemini":       "gemini",
	"DeepSeek":     "deepseek",
	"Qwen":         "qwen",
	"Doubao":       "doubao",
	"GLM":          "zhipu",
	"Moonshot":     "moonshot",
	"Mistral":      "mistral",
	"Yi":           "zero-one",
	"Spark":        "tencent-cloud-ti",
	"Hunyuan":      "tencent-cloud-ti",
	"New API":      "newapi",
	"Llama":        "ollama",
	"Grok":         "grok",
}

// providerIconNames 是 15 个图标文件名，顺序固定：下标进位图 ID，改顺序会让纹理标识变化（无害，但无谓）。
var providerIconNames = []string{
	"openai", "anthropic", "azureai", "gemini", "deepseek", "qwen", "doubao", "zhipu",
	"moonshot", "mistral", "zero-one", "tencent-cloud-ti", "newapi", "ollama", "grok",
}

const (
	// providerMasterSize 是内嵌 PNG 的边长（像素）。
	providerMasterSize = 120
	// providerIconIDBase 是品牌图标位图 ID 的高位；低 32 位 = 下标 << 16 | 像素边长。
	providerIconIDBase uint64 = 0x475a_5052_0000_0000
	// providerSizedCap 是预缩小位图缓存的条目上限（15 个品牌 × 常用的 5–6 档边长，远低于它）。
	providerSizedCap = 192
)

// providerSpecial 是 Custom / Other 的圆角方块配色（渲染层 SPECIAL_CATEGORY_ICON_STYLE）。
type providerSpecial struct {
	icon       gfx.Icon
	from, to   tokens.Color // linear-gradient(135deg, from 0%, to 100%)
	border     tokens.Color // 1px 边框
	foreground tokens.Color // 图标色
}

var providerSpecials = map[string]providerSpecial{
	"Custom": {
		icon: icons.SlidersHorizontal,
		from: tokens.RGBA(0xee, 0xf2, 0xff, 1), to: tokens.RGBA(0xdb, 0xea, 0xfe, 1),
		border: tokens.RGBA(37, 99, 235, 0.18), foreground: tokens.RGBA(0x25, 0x63, 0xeb, 1),
	},
	"Other": {
		icon: icons.CircleDotDashed,
		from: tokens.RGBA(0xf8, 0xfa, 0xfc, 1), to: tokens.RGBA(0xe2, 0xe8, 0xf0, 1),
		border: tokens.RGBA(71, 85, 105, 0.18), foreground: tokens.RGBA(0x47, 0x55, 0x69, 1),
	},
}

// HasDedicatedProviderIcon 报告分类是否有专属图标（品牌 logo 或 Custom / Other），对应渲染层 hasDedicatedCategoryIcon。
func HasDedicatedProviderIcon(category string) bool {
	if _, ok := providerIconFiles[category]; ok {
		return true
	}
	_, ok := providerSpecials[category]
	return ok
}

// providerMasters 是各品牌 120×120 的预乘 BGRA 位图（解一次）。
var providerMasters struct {
	once sync.Once
	pix  map[string][]byte // 文件名 → 预乘 RGBA（解码后的原始像素，缩小用）
}

func loadProviderMasters() {
	providerMasters.once.Do(func() {
		providerMasters.pix = make(map[string][]byte, len(providerIconNames))
		for _, name := range providerIconNames {
			data, err := providerIconFS.ReadFile("assets/providers/" + name + ".png")
			if err != nil {
				continue
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil || img.Bounds().Dx() != providerMasterSize || img.Bounds().Dy() != providerMasterSize {
				continue
			}
			rgba := image.NewRGBA(image.Rect(0, 0, providerMasterSize, providerMasterSize)) // 预乘 RGBA
			draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
			providerMasters.pix[name] = rgba.Pix
		}
	})
}

type providerSizedKey struct {
	index int
	px    int
}

var providerSized struct {
	mu  sync.Mutex
	bmp map[providerSizedKey]gfx.Bitmap
}

// providerIconBitmap 返回品牌图标缩成 px×px 的位图（预乘 BGRA，ID 与 Version 固定）。
// px 不小于母版边长时直接用母版（放大交给显示端采样，不在这里插值）。文件缺失或损坏时 ok 为假。
func providerIconBitmap(name string, px int) (gfx.Bitmap, bool) {
	index := -1
	for i, n := range providerIconNames {
		if n == name {
			index = i
			break
		}
	}
	if index < 0 {
		return gfx.Bitmap{}, false
	}
	px = min(max(px, 1), providerMasterSize)
	loadProviderMasters()
	master, ok := providerMasters.pix[name]
	if !ok {
		return gfx.Bitmap{}, false
	}
	key := providerSizedKey{index, px}
	providerSized.mu.Lock()
	defer providerSized.mu.Unlock()
	if bmp, ok := providerSized.bmp[key]; ok {
		return bmp, true
	}
	row := func(y int, dst []byte) { copy(dst, master[y*providerMasterSize*4:(y+1)*providerMasterSize*4]) }
	pix := areaResample(providerMasterSize, providerMasterSize, px, px, row)
	for i := 0; i+3 < len(pix); i += 4 { // RGBA → BGRA
		pix[i], pix[i+2] = pix[i+2], pix[i]
	}
	bmp := gfx.Bitmap{
		Pix: pix, Width: px, Height: px, Stride: px * 4,
		ID: providerIconIDBase | uint64(index)<<16 | uint64(px), Version: 1,
	}
	if providerSized.bmp == nil || len(providerSized.bmp) >= providerSizedCap {
		providerSized.bmp = make(map[providerSizedKey]gfx.Bitmap)
	}
	providerSized.bmp[key] = bmp
	return bmp, true
}

// ProviderBrandIcon 画模型分类的图标（边长 size 逻辑像素），与渲染层 getCategoryIcon 同一优先级：
//
//  1. "nanobananai 🍌"：黄色渐变圆角方块 + 香蕉字符；
//  2. 有品牌 logo 的分类：6px 圆角、contain 的位图；
//  3. Custom / Other：蓝 / 灰渐变圆角方块 + lucide 图标（边长 0.68×）；
//  4. 其余（含 ERNIE，渲染层映射为空）：首字母圆（灰色 135° 渐变，字号 0.55×、字重 600）。
//
// 图标资源坏了（嵌入文件缺失或损坏）时退到第 4 种，不让这一格留白。
func (l *Look) ProviderBrandIcon(c ui.C, category string, size float32) ui.Box {
	if category == "nanobananai 🍌" {
		return l.providerBanana(c, size)
	}
	if name, ok := providerIconFiles[category]; ok {
		scale := c.Canvas().PixelScale()
		if scale <= 0 {
			scale = 1
		}
		if bmp, ok := providerIconBitmap(name, int(size*scale+0.5)); ok {
			return ui.Image(c, bmp).Size(size, size).Shrink(0).Radius(6).Clip()
		}
	} else if sp, ok := providerSpecials[category]; ok {
		return l.providerSpecialBox(c, sp, size)
	}
	return l.providerLetter(c, category, size)
}

// providerGradientBox 是带 135° 渐变底、可选边框与阴影的 6px / 圆形盒（盒内内容由 children 放，居中）。
func (l *Look) providerGradientBox(c ui.C, size, radius float32, from, to, border tokens.Color, shadow []tokens.Shadow, children func()) ui.Box {
	box := ui.Column(c).Size(size, size).Shrink(0).Center().Draw(func(d *ui.DrawContext) {
		if len(shadow) > 0 {
			sub := *d
			l.PaintSurface(&sub, Surface{Shadows: shadow, Radius: radius})
		}
		r := SnapRect(d.Rect, d.Canvas.PixelScale())
		d.Canvas.FillGradient(r, radius, 135, []gfx.GradientStop{
			gfx.Stop(0, d.Fade(l.Col(from))), gfx.Stop(1, d.Fade(l.Col(to))),
		})
		if border.A > 0 {
			sub := *d
			l.PaintSurface(&sub, Surface{Border: border, BorderW: 1, Radius: radius})
		}
	})
	return box.Children(children)
}

func (l *Look) providerSpecialBox(c ui.C, sp providerSpecial, size float32) ui.Box {
	shadow := []tokens.Shadow{{X: 0, Y: 1, Blur: 2, Color: tokens.RGBA(15, 23, 42, 0.06)}}
	return l.providerGradientBox(c, size, 6, sp.from, sp.to, sp.border, shadow, func() {
		l.Icon(c, sp.icon, size*0.68, sp.foreground)
	})
}

func (l *Look) providerBanana(c ui.C, size float32) ui.Box {
	shadow := []tokens.Shadow{{X: 0, Y: 1, Blur: 2, Color: tokens.RGBA(0, 0, 0, 0.05)}}
	return l.providerGradientBox(c, size, 6, tokens.RGBA(0xfe, 0xfc, 0xe8, 1), tokens.RGBA(0xfe, 0xf0, 0x8a, 1),
		tokens.RGBA(0xfd, 0xe0, 0x47, 1), shadow, func() {
			// line-height: 1：行高等于字号。
			l.TextPx(c, "🍌", size*0.75, size*0.75, WeightNormal, tokens.RGBA(0x0f, 0x17, 0x2a, 1), Centered())
		})
}

// providerLetter 是首字母圆：取分类名的第一个字符（空串时是 "?"）。
func (l *Look) providerLetter(c ui.C, category string, size float32) ui.Box {
	letter := "?"
	if r, n := utf8.DecodeRuneInString(category); n > 0 && r != utf8.RuneError {
		letter = string(r)
	}
	slate := func(a float32) tokens.Color { return tokens.RGBA(148, 163, 184, a) }
	return l.providerGradientBox(c, size, size/2, slate(0.9), slate(0.4), tokens.Color{}, nil, func() {
		fs := size * 0.55
		l.TextPx(c, letter, fs, fs*1.5, WeightSemibold, tokens.RGBA(0x0f, 0x17, 0x2a, 1), Centered())
	})
}
