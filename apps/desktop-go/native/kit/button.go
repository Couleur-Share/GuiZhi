package kit

import (
	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
)

// OutlineButton 是带图标的描边次级按钮（渲染层的「h-8 rounded-lg border border-border」，
// 面是 app-ui-surface-bg，悬停 bg-accent），返回本帧是否被点击。
// 需要键盘焦点与朗读时由 ButtonBase 提供，名字取 label。
//
// 注意：在构建视图时读取 Hovered/Pressed，指针进出会让整个视图重建一次（MyGo 的行为）；
// 只在少量静态控件上这样用，长列表的行内悬停效果交给控件自身或 Draw 回调。
func OutlineButton(c *ui.Context, l *Look, icon icons.Name, label string) bool {
	b := ui.ButtonBase(c)
	bg := l.Surface
	switch {
	case b.Pressed():
		bg = l.SurfacePressed
	case b.Hovered():
		bg = l.SurfaceHover
	}
	b.Gap(6).Padding(0, 12).Height(32).Radius(8).Border(1, l.Border).Background(bg).Children(func() {
		Icon(c, icon, 14).TextColor(l.Text)
		ui.Text(c, label).FontSize(12).TextColor(l.Text)
	})
	return b.Clicked()
}
