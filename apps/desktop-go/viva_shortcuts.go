package main

// Viva 路径的系统级快捷键：shortcutNative 的 Viva 实现，接到 app.RegisterGlobalShortcut。
// 触发行为与 mygoShortcutNative 一致（shortcuts.ts）：showApp 先切换窗口显隐并广播 window:visibility-changed，
// 随后所有动作都推 shortcut:triggered；注册失败（被别的程序占用、按键 Viva 不支持）只记日志，不影响其余快捷键。

import (
	"fmt"
	"log"
	"strings"
	"sync"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// parseGlobalAccelerator 把 Electron 风格的组合键（"Alt+Shift+P"、"CommandOrControl+Shift+F5"）解成 Viva 的全局快捷键。
// 修饰键不分大小写；Windows 上 CommandOrControl 与 Control 都是 Ctrl，Super/Meta 是 Windows 键。
// 主键只支持 Viva 能注册的：字母、数字、F1–F24、Space、Enter、Escape、Tab；其余（方向键、标点等）返回错误。
func parseGlobalAccelerator(accelerator string) (vapp.GlobalShortcut, error) {
	parts := strings.Split(accelerator, "+")
	var mods gfx.Modifiers
	for _, raw := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "commandorcontrol", "cmdorctrl", "control", "ctrl":
			mods |= gfx.ModifierControl
		case "alt", "option":
			mods |= gfx.ModifierAlt
		case "shift":
			mods |= gfx.ModifierShift
		case "super", "meta", "command", "cmd":
			mods |= gfx.ModifierMeta
		default:
			return vapp.GlobalShortcut{}, fmt.Errorf("组合键 %q 里有不认识的修饰键 %q", accelerator, strings.TrimSpace(raw))
		}
	}
	key := strings.ToUpper(strings.TrimSpace(parts[len(parts)-1]))
	switch key {
	case "RETURN":
		key = "ENTER"
	case "ESC":
		key = "ESCAPE"
	}
	if key == "" {
		return vapp.GlobalShortcut{}, fmt.Errorf("组合键 %q 缺少主键", accelerator)
	}
	if mods == 0 {
		return vapp.GlobalShortcut{}, fmt.Errorf("组合键 %q 至少需要一个修饰键", accelerator)
	}
	return vapp.GlobalShortcut{Modifiers: mods, Key: key}, nil
}

// vivaShortcutNative 实现 shortcutNative。
type vivaShortcutNative struct {
	l      *Library
	toggle func() // showApp：切换窗口显隐（appLifecycle.toggleWindow）
	// registerKey 注册一个系统级快捷键，返回撤销函数；缺省为 Viva 的 RegisterGlobalShortcut，测试注入记录器。
	registerKey func(vapp.GlobalShortcut, func()) (func(), error)

	mu    sync.Mutex
	stops []func()
}

func newVivaShortcutNative(l *Library, toggle func()) *vivaShortcutNative {
	return &vivaShortcutNative{l: l, toggle: toggle, registerKey: vapp.RegisterGlobalShortcut}
}

func (n *vivaShortcutNative) unregisterAll() {
	n.mu.Lock()
	stops := n.stops
	n.stops = nil
	n.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

func (n *vivaShortcutNative) register(action, accelerator string) bool {
	if accelerator == "" {
		return false
	}
	sc, err := parseGlobalAccelerator(accelerator)
	if err == nil {
		var stop func()
		stop, err = n.registerKey(sc, func() { n.fire(action) })
		if err == nil {
			n.mu.Lock()
			n.stops = append(n.stops, stop)
			n.mu.Unlock()
			return true
		}
	}
	// 与 Electron 一致：注册失败（被别的程序占用、Viva 不支持的按键等）只记日志，不影响其余快捷键
	log.Printf("[shortcuts] 注册 %s（%s）失败：%v", action, accelerator, err)
	return false
}

func (n *vivaShortcutNative) fire(action string) {
	if action == "showApp" && n.toggle != nil {
		n.toggle()
	}
	n.l.pushMain("shortcut:triggered", action)
}
