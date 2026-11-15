package kit

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"

	"github.com/egoist/mygo/ui"
)

// Wallpaper 是壁纸（玻璃）模式的输入，对应设置里的 backgroundImage*：
// 图片按 object-cover 居中铺满，Opacity 是壁纸层的不透明度（0–1），Blur 是壁纸层的高斯模糊（0–50，单位同 CSS 的 px）。
// 有壁纸时界面改用「壁纸模式」令牌：面板底色变成半透明，壁纸从后面透出来。
type Wallpaper struct {
	Image   *ui.Bitmap
	Opacity float64
	Blur    float64
}

// 与 settings-appearance.ts 的 clampBackgroundImageOpacity / clampBackgroundImageBlur 一致。
const (
	maxWallpaperBlur  = 50
	maxWallpaperBytes = 64 << 20 // 超过 64 MiB 的壁纸拒绝读入：解码会把它整张展开成像素
)

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return lo
	}
	return math.Max(lo, math.Min(hi, v))
}

// NewWallpaper 把已解码的图片与强度装配成 Wallpaper（强度按设置里的范围收敛）。
func NewWallpaper(img *ui.Bitmap, opacity, blur float64) *Wallpaper {
	return &Wallpaper{Image: img, Opacity: clamp(opacity, 0, 1), Blur: clamp(blur, 0, maxWallpaperBlur)}
}

// LoadWallpaper 从文件读入壁纸（PNG、JPEG、GIF 首帧、WebP、BMP）。
func LoadWallpaper(path string, opacity, blur float64) (*Wallpaper, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("读取壁纸失败：%w", err)
	}
	if st.Size() > maxWallpaperBytes {
		return nil, fmt.Errorf("壁纸文件过大（%d MiB，上限 %d MiB）", st.Size()>>20, maxWallpaperBytes>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取壁纸失败：%w", err)
	}
	bmp, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil, fmt.Errorf("解码壁纸失败：%w", err)
	}
	return NewWallpaper(bmp, opacity, blur), nil
}

// DemoWallpaper 生成一张确定性的演示壁纸（青绿到紫色的对角渐变，加两处柔和光斑），
// 供截图与测试使用：不依赖外部图片，同样的尺寸总是同样的像素。
func DemoWallpaper(w, h int, opacity, blur float64) *Wallpaper {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	from, to := [3]float64{34, 197, 176}, [3]float64{124, 77, 255}
	spots := []struct{ x, y, r, gain float64 }{{0.22, 0.28, 0.32, 70}, {0.78, 0.72, 0.28, 55}}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			t := (fx + fy) / 2
			var px [3]float64
			for i := range px {
				px[i] = from[i] + (to[i]-from[i])*t
			}
			for _, s := range spots {
				d := math.Hypot(fx-s.x, (fy-s.y)*float64(h)/float64(w)) / s.r
				if d < 1 {
					k := (1 - d) * (1 - d) * s.gain
					for i := range px {
						px[i] += k
					}
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{R: clamp8(px[0]), G: clamp8(px[1]), B: clamp8(px[2]), A: 255})
		}
	}
	return NewWallpaper(ui.NewBitmap(img), opacity, blur)
}

func clamp8(v float64) uint8 { return uint8(clamp(v, 0, 255) + 0.5) }
