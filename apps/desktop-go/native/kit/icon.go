package kit

import (
	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
)

// Icon 画一枚 Lucide 图标：边长 size（DIP），颜色取上下文的文字色（用 TextColor 覆盖）。
// 装饰性图标不进可访问树；需要朗读时调用方对返回值 .Label(...)。
func Icon(c *ui.Context, name icons.Name, size float32) ui.Element {
	return ui.Icon(c, icons.Get(name)).Size(size, size)
}
