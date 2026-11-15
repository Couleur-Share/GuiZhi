package kit

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/locales"
)

// 快捷键在配置里存成 Electron 风格的加速键字符串："CommandOrControl+Shift+K"、"Alt+Shift+P"。
// 这是历史格式（shortcuts.json 与导出的设置清单都是它），也是 mygo.GlobalShortcut.Register 认的格式，
// 所以原生界面录制出来的组合必须仍是它。本文件只做三件事：把一次按键写成这个字符串、
// 把它显示给人看、把它归一化以便比较冲突。

// 修饰键的规范名与显示名。顺序固定为 Ctrl、Alt、Shift、Win：渲染层录制时也是这个顺序（它没有 Win）。
const (
	modCtrl  = "CommandOrControl"
	modAlt   = "Alt"
	modShift = "Shift"
	modWin   = "Meta"
)

var modOrder = []string{modCtrl, modAlt, modShift, modWin}

var modDisplay = map[string]string{modCtrl: "Ctrl", modAlt: "Alt", modShift: "Shift", modWin: "Win"}

// modAliases 是各种写法到规范名的映射（小写）。Windows 上 Control 与 CommandOrControl 是同一个键，
// 归并之后比较冲突才不会把「Ctrl+K」与「CommandOrControl+K」当成两个组合。
var modAliases = map[string]string{
	"commandorcontrol": modCtrl, "cmdorctrl": modCtrl, "control": modCtrl, "ctrl": modCtrl,
	"alt": modAlt, "option": modAlt,
	"shift": modShift,
	"meta":  modWin, "super": modWin, "win": modWin, "cmd": modWin, "command": modWin,
}

// keyAliases 把渲染层历史录制出的键名（e.key.toUpperCase()）和其他常见写法归到规范键名。
// 渲染层录出的 "ARROWUP" 并不是合法的加速键名，这里归一化后至少比较冲突时是对的。
var keyAliases = map[string]string{
	"arrowup": "Up", "up": "Up", "arrowdown": "Down", "down": "Down",
	"arrowleft": "Left", "left": "Left", "arrowright": "Right", "right": "Right",
	"escape": "Escape", "esc": "Escape", "enter": "Enter", "return": "Enter",
	"space": "Space", "spacebar": "Space", "tab": "Tab", "backspace": "Backspace",
	"delete": "Delete", "del": "Delete", "insert": "Insert", "ins": "Insert",
	"home": "Home", "end": "End", "pageup": "PageUp", "pagedown": "PageDown",
	"plus": "+",
}

// namedKeys 是不能用单个字符表示的键。
var namedKeys = map[ui.Key]string{
	ui.KeyEnter: "Enter", ui.KeyEscape: "Escape", ui.KeyBackspace: "Backspace", ui.KeyTab: "Tab",
	ui.KeySpace: "Space", ui.KeyDelete: "Delete", ui.KeyInsert: "Insert",
	ui.KeyHome: "Home", ui.KeyEnd: "End", ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
	ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyUp: "Up", ui.KeyDown: "Down",
	ui.KeyF1: "F1", ui.KeyF2: "F2", ui.KeyF3: "F3", ui.KeyF4: "F4", ui.KeyF5: "F5", ui.KeyF6: "F6",
	ui.KeyF7: "F7", ui.KeyF8: "F8", ui.KeyF9: "F9", ui.KeyF10: "F10", ui.KeyF11: "F11", ui.KeyF12: "F12",
	ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".", ui.KeySlash: "/",
	ui.KeySemicolon: ";", ui.KeyQuote: "'", ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]",
	ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
}

// extraFunctionKeys 是 ui.Key 没有常量的 F13–F24，只能靠键位码认出。
var extraFunctionKeys = map[ui.KeyCode]string{
	ui.CodeF13: "F13", ui.CodeF14: "F14", ui.CodeF15: "F15", ui.CodeF16: "F16", ui.CodeF17: "F17", ui.CodeF18: "F18",
	ui.CodeF19: "F19", ui.CodeF20: "F20", ui.CodeF21: "F21", ui.CodeF22: "F22", ui.CodeF23: "F23", ui.CodeF24: "F24",
}

// KeyToken 返回按键对应的加速键名。键盘布局打不出这个键（如西里尔布局下的 S 位）时退回它的物理位置；
// 修饰键本身、锁定键、媒体键等不能作主键，返回 false。
func KeyToken(key, physical ui.Key, code ui.KeyCode) (string, bool) {
	for _, k := range []ui.Key{key, physical} {
		if name, ok := namedKeys[k]; ok {
			return name, true
		}
		if k >= ui.KeyA && k <= ui.KeyZ {
			return string(rune('A' + (k - ui.KeyA))), true
		}
		if k >= ui.Key0 && k <= ui.Key9 {
			return string(rune('0' + (k - ui.Key0))), true
		}
	}
	if name, ok := extraFunctionKeys[code]; ok {
		return name, true
	}
	return "", false
}

// IsModifierCode 报告键位码是否是修饰键本身（左右 Ctrl/Alt/Shift/Win）：它们按下时没有主键，只更新录制中的显示。
func IsModifierCode(code ui.KeyCode) bool {
	switch code {
	case ui.CodeControlLeft, ui.CodeControlRight, ui.CodeShiftLeft, ui.CodeShiftRight,
		ui.CodeAltLeft, ui.CodeAltRight, ui.CodeSuperLeft, ui.CodeSuperRight:
		return true
	}
	return false
}

// modifierTokens 把按住的修饰键写成规范名，顺序固定。
func modifierTokens(mods ui.Modifiers) []string {
	var out []string
	if mods&ui.Ctrl != 0 {
		out = append(out, modCtrl)
	}
	if mods&ui.Alt != 0 {
		out = append(out, modAlt)
	}
	if mods&ui.Shift != 0 {
		out = append(out, modShift)
	}
	if mods&ui.Super != 0 {
		out = append(out, modWin)
	}
	return out
}

// Accelerator 把「修饰键 + 主键」写成加速键字符串。至少要有一个修饰键——
// 单独一个字母、F 键当快捷键会吃掉正常输入，渲染层同样要求组合；没有修饰键或主键不可用返回 false。
func Accelerator(mods ui.Modifiers, key, physical ui.Key, code ui.KeyCode) (string, bool) {
	name, ok := KeyToken(key, physical, code)
	if !ok || mods&(ui.Ctrl|ui.Alt|ui.Shift|ui.Super) == 0 {
		return "", false
	}
	return strings.Join(append(modifierTokens(mods), name), "+"), true
}

// splitAccelerator 把加速键拆成修饰键（规范名，去重，固定顺序）与主键。
// 主键为空表示没写主键；"Ctrl++" 这种以加号作主键的写法按 "+" 处理。
func splitAccelerator(acc string) (mods []string, key string) {
	acc = strings.TrimSpace(acc)
	var parts []string
	if strings.HasSuffix(acc, "++") {
		parts = append(strings.Split(strings.TrimSuffix(acc, "++"), "+"), "+")
	} else {
		parts = strings.Split(acc, "+")
	}
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if m, ok := modAliases[strings.ToLower(p)]; ok {
			seen[m] = true
			continue
		}
		key = canonicalKey(p)
	}
	for _, m := range modOrder {
		if seen[m] {
			mods = append(mods, m)
		}
	}
	return mods, key
}

func canonicalKey(k string) string {
	if name, ok := keyAliases[strings.ToLower(k)]; ok {
		return name
	}
	if r := []rune(k); len(r) == 1 {
		return strings.ToUpper(k)
	}
	if len(k) >= 2 && (k[0] == 'f' || k[0] == 'F') { // f5 → F5
		return "F" + k[1:]
	}
	return k
}

// NormalizeAccelerator 返回加速键的规范形式，供比较冲突用：修饰键别名归并（Control、Ctrl、CmdOrCtrl → CommandOrControl）、
// 顺序固定、键名统一（ARROWUP → Up、p → P）。空串得到空串。
func NormalizeAccelerator(acc string) string {
	mods, key := splitAccelerator(acc)
	if len(mods) == 0 && key == "" {
		return ""
	}
	parts := mods
	if key != "" {
		parts = append(parts, key)
	}
	return strings.Join(parts, "+")
}

// FormatAccelerator 把加速键写成给人看的样子："CommandOrControl+Shift+K" → "Ctrl + Shift + K"；
// 空串是「未设置」。认不出的片段原样保留，不丢信息。
func FormatAccelerator(acc string) string {
	mods, key := splitAccelerator(acc)
	if len(mods) == 0 && key == "" {
		return locales.SettingsNotSet()
	}
	parts := make([]string, 0, len(mods)+1)
	for _, m := range mods {
		parts = append(parts, modDisplay[m])
	}
	if key != "" {
		parts = append(parts, key)
	}
	return strings.Join(parts, " + ")
}

// FormatHeld 写出录制过程中已按住的修饰键与（有的话）主键；什么都没按是「按下快捷键…」。
func FormatHeld(mods ui.Modifiers, key string) string {
	var parts []string
	for _, m := range modifierTokens(mods) {
		parts = append(parts, modDisplay[m])
	}
	if key != "" {
		parts = append(parts, key)
	}
	if len(parts) == 0 {
		return locales.SettingsPressKeys()
	}
	return strings.Join(parts, " + ")
}
