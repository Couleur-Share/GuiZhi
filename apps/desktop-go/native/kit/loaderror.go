package kit

import (
	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
	"guizhi-mygo-shell/native/locales"
)

// LoadErrorState 是列表读取失败的占位，对应渲染层的 LoadErrorState。
//
// 存在的理由：「空」和「读不出来」长得一样，失败静默渲染成空态会让用户以为东西没了。
// 这里明说读取失败，给出可选中复制的原因和重试入口。retry 为 nil 时不显示重试按钮。
func LoadErrorState(c *ui.Context, message string, retry func()) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(10).Padding(40, 24).Role(ui.RoleAlert).Children(func() {
		Icon(c, icons.TriangleAlert, 28).TextColor(t.Danger.Alpha(0.6))
		ui.Text(c, locales.CommonLoadFailed()).FontSize(14).FontWeight(500)
		ui.Text(c, message).FontSize(12).TextColor(t.TextMuted).Selectable().MaxWidth(448).Wrap()
		if retry != nil && OutlineButton(c, icons.RotateCcw, locales.CommonRetry()) {
			retry()
		}
	})
}
