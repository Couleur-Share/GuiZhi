package kit

// 壁纸一层的绘制：位图铺满，或读不出来时画损坏的回退态。预览舞台与整窗壁纸共用。
//
// 对照渲染层：
//
//	BackgroundImageBackdrop：opacity / blur / scale(1.03) 的容器里放 LocalImage（位图渲染见 wallpaper.go）；
//	LocalImage 加载失败：<span class="flex items-center justify-center bg-muted/30 text-muted-foreground/30"><ImageIcon class="w-8 h-8 opacity-50"/></span>
//	——容器的 opacity 同样作用在回退层上，所以回退色与图标按不透明度整体淡出；回退态的模糊不复刻（只有一个淡到 15% 的图标，模糊后看不见）。

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// WallpaperLayer 描述要画的一层壁纸。
type WallpaperLayer struct {
	// Cache 是这一层的异步位图缓存（预览与整窗各持一份）；File 是应用图片目录里的纯文件名。
	Cache *WallpaperCache
	File  string
	// Opacity 是 0–1 的可见度，Blur 是 0–50 的虚化像素（调用方已经按渲染层钳过值）。
	Opacity, Blur float64
	// Tokens 是回退态取色用的语义令牌：整窗用当前外观（已是壁纸模式），预览舞台用壁纸模式的令牌（它自带 app-background-mode-image）。
	Tokens tokens.Tokens
}

// PaintWallpaperLayer 把壁纸画满 d.Rect（画在当前层，不是 DrawBackdrop：整窗壁纸要画在根背景之上、毛玻璃遮罩之下，
// 而 DrawBackdrop 画在整帧最底层，会被根背景整个盖住）。
//
//   - 位图就绪：按 object-cover 居中铺满；模糊大于 0 时再放大 1.03 倍（见 PaintWallpaper）。位图已把不透明度预乘进像素。
//   - 还在读取与解码：什么也不画（<img> 没加载出来之前是空的）。
//   - 读取或解码失败：回退层，muted 30% 的底加居中的 2rem 图标（muted-foreground 30% × 图标自带的 50% = 15%）。
//
// 文件名为空时清空缓存（释放内存）并不绘制。
func (l *Look) PaintWallpaperLayer(d *ui.DrawContext, w WallpaperLayer) {
	if w.Cache == nil {
		return
	}
	rect := d.Rect
	bmp, ok := w.Cache.Bitmap(w.File, float32(w.Opacity), float32(w.Blur), rect.Width, rect.Height)
	if ok {
		PaintWallpaper(d.Canvas, bmp, rect, w.Blur > 0, false)
		return
	}
	if !w.Cache.Failed() || rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	opacity := clampF(float32(w.Opacity), 0, 1, 1)
	d.Canvas.Fill(rect, d.Fade(l.Col(Fade(w.Tokens.Muted, 0.3*opacity))))
	side := l.Rem(2)
	box := gfx.Rect{X: rect.X + (rect.Width-side)/2, Y: rect.Y + (rect.Height-side)/2, Width: side, Height: side}
	PaintIcon(d.Canvas, icons.Image, box, d.Fade(l.Col(Fade(w.Tokens.MutedForeground, 0.15*opacity))))
}
