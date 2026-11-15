package kit

import "github.com/egoist/mygo/ui"

// Backdrop 画壁纸层：窗口根容器里最先建立的子元素，绝对定位铺满，垫在后面建立的内容之下。
// 做法与渲染层 BackgroundImageBackdrop 一致：图片 object-cover 居中，整层按设置的不透明度淡出，
// 有模糊时整层放大 1.03 倍盖住模糊在边缘产生的透明带，最后再罩一层 blanket（--app-ui-blanket-bg）。
//
// 与渲染层的有意差异：MyGo 没有 backdrop-filter（背景模糊），渲染层的 app-ui-*-backdrop 令牌
// 在壁纸模式下本来就是 none，所以这里不缺东西；只有 .app-left-rail-glass 上的 blur 会没有对应物。
// 无壁纸时什么都不画。
func Backdrop(c *ui.Context, l *Look) {
	w := l.Spec.Wallpaper
	if w == nil || w.Image == nil {
		return
	}
	layer := ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).ZIndex(-1).Clip().Opacity(float32(w.Opacity))
	layer.Children(func() {
		img := ui.Image(c, w.Image).Fill().Fit(ui.Cover)
		if w.Blur > 0 {
			img.Blur(float32(w.Blur)).Scale(1.03, 1.03)
		}
	})
	ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).ZIndex(-1).Background(l.T.UiBlanketBg)
}
