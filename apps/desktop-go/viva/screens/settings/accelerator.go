package settings

import (
	"strings"

	"github.com/vivaui/viva/gfx"
)

// EventAccelerator 把一次按键事件换成 accelerator 字符串（`CommandOrControl+Alt+Shift+键` 的顺序，与录制框存下的写法一致）。
// 窗口内的局部快捷键用它与已保存的组合键比较：录制与匹配共用同一份格式化，两边不会各写一套而对不上。
// 事件不是完整和弦（只有键没有修饰键）或不是能注册的键（输入法组字、非 ASCII 字符）时返回 false。
func EventAccelerator(e gfx.KeyEvent) (string, bool) {
	name, ok := keyAccelerator(e)
	if !ok {
		return "", false
	}
	mods := modifierNames(e.Modifiers)
	if len(mods) == 0 {
		return "", false
	}
	return strings.Join(append(mods, name), "+"), true
}
