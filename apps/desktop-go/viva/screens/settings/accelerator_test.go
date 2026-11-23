package settings

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestEventAccelerator(t *testing.T) {
	cases := []struct {
		name string
		e    gfx.KeyEvent
		want string
		ok   bool
	}{
		{"字母加 Alt 与 Shift", gfx.KeyEvent{Key: gfx.KeyUnknown, Text: "n", Modifiers: gfx.ModifierAlt | gfx.ModifierShift}, "Alt+Shift+N", true},
		{"大写字母", gfx.KeyEvent{Key: gfx.KeyUnknown, Text: "N", Modifiers: gfx.ModifierAlt | gfx.ModifierShift}, "Alt+Shift+N", true},
		{"三个修饰键的顺序", gfx.KeyEvent{Key: gfx.KeyUnknown, Text: "k", Modifiers: gfx.ModifierShift | gfx.ModifierAlt | gfx.ModifierControl}, "CommandOrControl+Alt+Shift+K", true},
		{"Meta 记为 CommandOrControl", gfx.KeyEvent{Key: gfx.KeyUnknown, Text: "k", Modifiers: gfx.ModifierMeta}, "CommandOrControl+K", true},
		{"具名键", gfx.KeyEvent{Key: gfx.KeyF5, Modifiers: gfx.ModifierAlt}, "Alt+F5", true},
		{"空格", gfx.KeyEvent{Key: gfx.KeyUnknown, Text: " ", Modifiers: gfx.ModifierControl}, "CommandOrControl+Space", true},
		{"没有修饰键不是组合键", gfx.KeyEvent{Key: gfx.KeyUnknown, Text: "n"}, "", false},
		{"非 ASCII 字符", gfx.KeyEvent{Key: gfx.KeyUnknown, Text: "é", Modifiers: gfx.ModifierAlt}, "", false},
		{"没有文字也不是命名键", gfx.KeyEvent{Key: gfx.KeyUnknown, Modifiers: gfx.ModifierAlt}, "", false},
	}
	for _, c := range cases {
		got, ok := EventAccelerator(c.e)
		if got != c.want || ok != c.ok {
			t.Errorf("%s：得到 (%q, %v)，期望 (%q, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// 录制与匹配必须出同一份格式：录制框存下的写法与 EventAccelerator 对同一按键的输出一致。
func TestEventAcceleratorMatchesRecorderFormat(t *testing.T) {
	for _, ch := range recorderChords[:200] {
		e := gfx.KeyEvent{Key: gfx.KeyUnknown, Text: ch.key, Modifiers: ch.mods}
		got, ok := EventAccelerator(e)
		if !ok {
			continue // 命名键（Enter、方向键等）没有文字，由 OnKey 路径递送
		}
		if got != ch.accel {
			t.Fatalf("按键 %q + %v：EventAccelerator=%q，录制框=%q", ch.key, ch.mods, got, ch.accel)
		}
	}
}
