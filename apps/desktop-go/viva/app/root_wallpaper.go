package app

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
)

// 背景图（渲染层 App.tsx 的 BackgroundImageBackdrop 与显示设置的选图，台账 B-020）的外壳接线：
//   - 显示设置页的选图与读图接到 Services.BackgroundImages；没有装配时选图如实报「选图不可用」，读图按失败画损坏的回退图标；
//   - 整窗壁纸的位图缓存 r.wallpaper：壁纸启用（Look.Glass）时按窗口尺寸渲染并画在根背景之上，关闭时清空释放。

// mountWallpaper 接上选图与读图，并建好整窗壁纸的缓存。invalidate 在后台读完图后请求重绘。
func (r *Root) mountWallpaper(invalidate func()) {
	var load func(fileName string) ([]byte, error)
	if svc := r.opts.Services.BackgroundImages; svc != nil {
		r.settings.PickBackground = func() (string, error) { return svc.PickAndSave(r.ctx) }
		load = func(fileName string) ([]byte, error) { return svc.Read(r.ctx, fileName) }
		r.settings.LoadBackground = load
	}
	// load 为空时缓存把文件读取记为失败：整窗壁纸照 LocalImage 画损坏的回退，而不是悄悄什么都不显示。
	r.wallpaper = kit.NewWallpaperCache(load, invalidate)
}

// paintWallpaper 是 shell.Window.Wallpaper：在窗口根背景之上画整窗壁纸（d.Rect 是整个窗口）。
// 壁纸没有启用（没选图、选了但关闭、外观被覆盖成非玻璃）时清空缓存释放内存，什么也不画。
func (r *Root) paintWallpaper(d *ui.DrawContext) {
	l := r.look
	cur := r.store.Values().UI
	if !l.Glass || cur.BackgroundImageFileName == "" {
		r.wallpaper.Bitmap("", 0, 0, 0, 0)
		return
	}
	l.PaintWallpaperLayer(d, kit.WallpaperLayer{
		Cache:   r.wallpaper,
		File:    cur.BackgroundImageFileName,
		Opacity: settings.ClampOpacity(cur.BackgroundImageOpacity),
		Blur:    settings.ClampBlur(cur.BackgroundImageBlur),
		Tokens:  l.T,
	})
}

// closeWallpaper 在关窗口时释放整窗壁纸与设置页预览持有的位图。
func (r *Root) closeWallpaper() {
	r.wallpaper.Close()
	r.settings.Release()
}
