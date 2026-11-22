package kit

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"
	"sync"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// appIconPNG 是应用图标（与渲染层「关于」页引用的 assets/icon.png 同一份，256×256）。
//
//go:embed assets/appicon.png
var appIconPNG []byte

// appIconID 是图标位图的缓存标识（原生渲染器据此识别未变的内容，不必每帧重新上传）。
const appIconID uint64 = 0x475a_4943_4f4e_0001

var appIcon struct {
	once sync.Once
	bmp  gfx.Bitmap
	ok   bool
}

// loadAppIcon 把内嵌的 PNG 解成 Viva 的位图（预乘 BGRA）；只解一次，解不出来时 ok 为假。
func loadAppIcon() (gfx.Bitmap, bool) {
	appIcon.once.Do(func() {
		img, err := png.Decode(bytes.NewReader(appIconPNG))
		if err != nil {
			return
		}
		b := img.Bounds()
		rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy())) // 预乘 RGBA
		draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
		pix := rgba.Pix
		for i := 0; i+3 < len(pix); i += 4 { // RGBA → BGRA
			pix[i], pix[i+2] = pix[i+2], pix[i]
		}
		appIcon.bmp = gfx.Bitmap{Pix: pix, Width: rgba.Rect.Dx(), Height: rgba.Rect.Dy(), Stride: rgba.Stride, ID: appIconID, Version: 1}
		appIcon.ok = true
	})
	return appIcon.bmp, appIcon.ok
}

// AppIcon 画应用图标（边长 size，圆角 radius）：关于页的身份区。图标资源坏了就退成一块主色方块，不让这一格留白。
func (l *Look) AppIcon(c ui.C, size, radius float32) ui.Box {
	if bmp, ok := loadAppIcon(); ok {
		return ui.Image(c, bmp).Size(size, size).Shrink(0).Radius(radius).Clip()
	}
	return ui.Slot(c).Size(size, size).Shrink(0).Radius(radius).Background(l.Col(l.T.Primary))
}
