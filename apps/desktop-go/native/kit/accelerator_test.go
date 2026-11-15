package kit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/locales"
)

// acceptedByMyGo 复刻 mygo internal/accelerator 的 parseKey/modifier 词汇表（v0.9.1），
// 用来保证录制器写出的每个键名与修饰键名都是 mygo.GlobalShortcut.Register 认得的——
// 写出一个注册时才报「unknown key」的组合，用户要到重启后才发现快捷键没生效。
// 升级 MyGo 时对照 internal/accelerator/accelerator.go 复核这里。
func acceptedKey(name string) bool {
	if len([]rune(name)) == 1 {
		return true
	}
	switch strings.ToLower(name) {
	case "plus", "space", "tab", "capslock", "numlock", "scrolllock", "backspace", "delete", "del", "insert", "ins",
		"return", "enter", "up", "down", "left", "right", "home", "end", "pageup", "pagedown", "escape", "esc",
		"volumeup", "volumedown", "volumemute", "medianexttrack", "mediaprevioustrack", "mediastop", "mediaplaypause",
		"printscreen", "numdec", "numadd", "numsub", "nummult", "numdiv":
		return true
	}
	var n int
	if lower := strings.ToLower(name); len(lower) >= 2 && lower[0] == 'f' {
		if _, err := fmt.Sscanf(lower[1:], "%d", &n); err == nil && n >= 1 && n <= 24 && fmt.Sprint(n) == lower[1:] {
			return true
		}
	}
	return false
}

func acceptedModifier(name string) bool {
	switch strings.ToLower(name) {
	case "command", "cmd", "super", "meta", "win", "control", "ctrl", "commandorcontrol", "cmdorctrl", "alt", "option", "altgr", "shift":
		return true
	}
	return false
}

func TestKeyTokenLettersAndDigits(t *testing.T) {
	letters := []ui.Key{ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyG, ui.KeyH, ui.KeyI, ui.KeyJ, ui.KeyK, ui.KeyL, ui.KeyM,
		ui.KeyN, ui.KeyO, ui.KeyP, ui.KeyQ, ui.KeyR, ui.KeyS, ui.KeyT, ui.KeyU, ui.KeyV, ui.KeyW, ui.KeyX, ui.KeyY, ui.KeyZ}
	for i, k := range letters {
		want := string(rune('A' + i))
		if got, ok := KeyToken(k, ui.KeyUnknown, ui.CodeUnknown); !ok || got != want {
			t.Errorf("字母键 %d：得到 %q ok=%v，期望 %q", i, got, ok, want)
		}
	}
	digits := []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}
	for i, k := range digits {
		want := string(rune('0' + i))
		if got, ok := KeyToken(k, ui.KeyUnknown, ui.CodeUnknown); !ok || got != want {
			t.Errorf("数字键 %d：得到 %q ok=%v，期望 %q", i, got, ok, want)
		}
	}
}

func TestEveryRecordableKeyIsAcceptedByMyGo(t *testing.T) {
	names := map[string]bool{}
	for _, n := range namedKeys {
		names[n] = true
	}
	for _, n := range extraFunctionKeys {
		names[n] = true
	}
	if len(names) < 40 {
		t.Fatalf("可录制的键名只有 %d 个，表被误删了？", len(names))
	}
	for n := range names {
		if !acceptedKey(n) {
			t.Errorf("键名 %q 不在 MyGo 加速键词汇表里：录进去会在注册时失败", n)
		}
	}
	for _, m := range modOrder {
		if !acceptedModifier(m) {
			t.Errorf("修饰键名 %q 不在 MyGo 词汇表里", m)
		}
	}
}

func TestKeyTokenFallsBackToPhysicalKeyAndExtraFunctionKeys(t *testing.T) {
	// 西里尔布局下 S 位打不出 US 键：Key 未知，用物理位置
	if got, ok := KeyToken(ui.KeyUnknown, ui.KeyS, ui.CodeKeyS); !ok || got != "S" {
		t.Errorf("物理位置回退：%q ok=%v", got, ok)
	}
	if got, ok := KeyToken(ui.KeyUnknown, ui.KeyUnknown, ui.CodeF13); !ok || got != "F13" {
		t.Errorf("F13：%q ok=%v", got, ok)
	}
	if got, ok := KeyToken(ui.KeyUnknown, ui.KeyUnknown, ui.CodeF24); !ok || got != "F24" {
		t.Errorf("F24：%q ok=%v", got, ok)
	}
	for _, code := range []ui.KeyCode{ui.CodeControlLeft, ui.CodeShiftRight, ui.CodeAltLeft, ui.CodeSuperRight, ui.CodeCapsLock} {
		if _, ok := KeyToken(ui.KeyUnknown, ui.KeyUnknown, code); ok {
			t.Errorf("键位码 %v 不能作主键", code)
		}
	}
}

func TestIsModifierCode(t *testing.T) {
	for _, code := range []ui.KeyCode{ui.CodeControlLeft, ui.CodeControlRight, ui.CodeShiftLeft, ui.CodeShiftRight,
		ui.CodeAltLeft, ui.CodeAltRight, ui.CodeSuperLeft, ui.CodeSuperRight} {
		if !IsModifierCode(code) {
			t.Errorf("%v 应是修饰键", code)
		}
	}
	for _, code := range []ui.KeyCode{ui.CodeKeyA, ui.CodeCapsLock, ui.CodeEscape, ui.CodeUnknown} {
		if IsModifierCode(code) {
			t.Errorf("%v 不是修饰键", code)
		}
	}
}

func TestAcceleratorFromKeys(t *testing.T) {
	cases := []struct {
		mods ui.Modifiers
		key  ui.Key
		want string
		ok   bool
	}{
		{ui.Ctrl | ui.Shift, ui.KeyK, "CommandOrControl+Shift+K", true},
		{ui.Alt | ui.Shift, ui.KeyP, "Alt+Shift+P", true},
		{ui.Ctrl | ui.Alt | ui.Shift | ui.Super, ui.KeyF5, "CommandOrControl+Alt+Shift+Meta+F5", true},
		{ui.Super, ui.KeyK, "Meta+K", true},
		{ui.Ctrl, ui.KeyUp, "CommandOrControl+Up", true},
		{ui.Alt, ui.KeyPageDown, "Alt+PageDown", true},
		{ui.Ctrl, ui.KeyComma, "CommandOrControl+,", true},
		{ui.Ctrl, ui.KeyBackslash, "CommandOrControl+\\", true},
		{ui.Ctrl, ui.KeyEnter, "CommandOrControl+Enter", true},
		{ui.Ctrl, ui.KeySpace, "CommandOrControl+Space", true},
		{0, ui.KeyK, "", false},             // 没有修饰键：会吃掉正常输入
		{0, ui.KeyF5, "", false},            // F 键单独也不行（渲染层同样要求组合）
		{ui.Ctrl, ui.KeyUnknown, "", false}, // 没有主键
	}
	for _, c := range cases {
		got, ok := Accelerator(c.mods, c.key, ui.KeyUnknown, ui.CodeUnknown)
		if ok != c.ok || got != c.want {
			t.Errorf("Accelerator(%v, %v) = %q, %v；期望 %q, %v", c.mods, c.key, got, ok, c.want, c.ok)
		}
		if ok {
			for _, part := range strings.Split(got, "+") {
				if part == "" { // "Ctrl++" 这类以加号作键的写法；录制器自己不会产生
					continue
				}
				if !acceptedModifier(part) && !acceptedKey(part) {
					t.Errorf("%q 里的 %q 不被 MyGo 接受", got, part)
				}
			}
		}
	}
}

func TestNormalizeAccelerator(t *testing.T) {
	cases := map[string]string{
		"":                               "",
		"   ":                            "",
		"Alt+Shift+P":                    "Alt+Shift+P",
		"shift+alt+p":                    "Alt+Shift+P",
		"ctrl+k":                         "CommandOrControl+K",
		"Control+K":                      "CommandOrControl+K",
		"CmdOrCtrl+K":                    "CommandOrControl+K",
		"CommandOrControl+Control+K":     "CommandOrControl+K",
		"Control+Shift+ARROWUP":          "CommandOrControl+Shift+Up", // 渲染层历史录制出的写法
		"Alt+ARROWLEFT":                  "Alt+Left",
		"Alt+ESCAPE":                     "Alt+Escape",
		"Alt+PAGEDOWN":                   "Alt+PageDown",
		"Alt+f5":                         "Alt+F5",
		"Super+K":                        "Meta+K",
		"Win+K":                          "Meta+K",
		"Ctrl++":                         "CommandOrControl++",
		"CommandOrControl+Alt+Shift+F12": "CommandOrControl+Alt+Shift+F12",
	}
	for in, want := range cases {
		if got := NormalizeAccelerator(in); got != want {
			t.Errorf("NormalizeAccelerator(%q) = %q，期望 %q", in, got, want)
		}
	}
	// 归一化是幂等的，并且是比较冲突的依据
	for _, want := range cases {
		if got := NormalizeAccelerator(want); got != want {
			t.Errorf("归一化应幂等：%q → %q", want, got)
		}
	}
	if NormalizeAccelerator("Ctrl+K") != NormalizeAccelerator("CommandOrControl+K") {
		t.Error("Ctrl 与 CommandOrControl 在 Windows 上是同一个键，应判为同一组合")
	}
	if NormalizeAccelerator("Alt+Shift+P") == NormalizeAccelerator("Alt+Shift+N") {
		t.Error("不同的键不能判为同一组合")
	}
}

func TestFormatAccelerator(t *testing.T) {
	cases := map[string]string{
		"CommandOrControl+Shift+K": "Ctrl + Shift + K",
		"Alt+Shift+P":              "Alt + Shift + P",
		"Meta+K":                   "Win + K",
		"Control+ARROWUP":          "Ctrl + Up",
		"alt+f5":                   "Alt + F5",
		"Ctrl++":                   "Ctrl + +",
	}
	for in, want := range cases {
		if got := FormatAccelerator(in); got != want {
			t.Errorf("FormatAccelerator(%q) = %q，期望 %q", in, got, want)
		}
	}
	if got := FormatAccelerator(""); got != locales.SettingsNotSet() {
		t.Errorf("空串应显示「%s」，得到 %q", locales.SettingsNotSet(), got)
	}
}

func TestFormatHeld(t *testing.T) {
	if got := FormatHeld(0, ""); got != locales.SettingsPressKeys() {
		t.Errorf("什么都没按应提示「%s」：%q", locales.SettingsPressKeys(), got)
	}
	if got := FormatHeld(ui.Ctrl|ui.Shift, ""); got != "Ctrl + Shift" {
		t.Errorf("只按了修饰键：%q", got)
	}
	if got := FormatHeld(ui.Ctrl|ui.Alt|ui.Super, "K"); got != "Ctrl + Alt + Win + K" {
		t.Errorf("修饰键与主键：%q", got)
	}
	if got := FormatHeld(0, "K"); got != "K" {
		t.Errorf("只按了主键：%q", got)
	}
}

func TestDefaultShortcutsAreCanonicalAndAccepted(t *testing.T) {
	// shortcuts.go 的默认组合：归一化后不变（不会被误判为冲突或被改写），且 MyGo 认得
	for _, acc := range []string{"Alt+Shift+P", "Alt+Shift+N", "Alt+Shift+F", "Alt+Shift+S"} {
		if got := NormalizeAccelerator(acc); got != acc {
			t.Errorf("默认组合 %q 被归一化成 %q", acc, got)
		}
		for _, part := range strings.Split(acc, "+") {
			if !acceptedModifier(part) && !acceptedKey(part) {
				t.Errorf("默认组合 %q 里的 %q 不被 MyGo 接受", acc, part)
			}
		}
	}
}
