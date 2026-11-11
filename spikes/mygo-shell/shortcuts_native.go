package main

// shortcutNative 的 MyGo 实现：把系统级快捷键接到 mygo.GlobalShortcut。
// 触发行为与 shortcuts.ts 一致：showApp 先切换窗口显隐并广播 window:visibility-changed，
// 随后所有动作都向渲染进程推 shortcut:triggered。

import (
	"log"
	"sync"

	"github.com/egoist/mygo"
)

type mygoShortcutNative struct {
	l   *Library
	win *mygo.Window

	mu         sync.Mutex
	registered []string
}

func newMygoShortcutNative(l *Library, win *mygo.Window) *mygoShortcutNative {
	return &mygoShortcutNative{l: l, win: win}
}

func (n *mygoShortcutNative) unregisterAll() {
	n.mu.Lock()
	accs := n.registered
	n.registered = nil
	n.mu.Unlock()
	for _, a := range accs {
		mygo.GlobalShortcut.Unregister(a)
	}
}

func (n *mygoShortcutNative) register(action, accelerator string) bool {
	if accelerator == "" {
		return false
	}
	err := mygo.GlobalShortcut.Register(accelerator, func() { n.fire(action) })
	if err != nil {
		// 与 Electron 一致：注册失败（被别的程序占用等）只记日志，不影响其余快捷键
		log.Printf("[shortcuts] 注册 %s（%s）失败：%v", action, accelerator, err)
		return false
	}
	n.mu.Lock()
	n.registered = append(n.registered, accelerator)
	n.mu.Unlock()
	return true
}

// toggleWindowForShowApp 对应 shortcuts.ts 的同名函数：最小化 → 还原并聚焦；可见 → 隐藏；否则显示并聚焦。
func toggleWindowForShowApp(win *mygo.Window, onVisibility func(bool)) {
	switch {
	case win.IsMinimized():
		win.Restore()
		win.Show()
		win.Focus()
		onVisibility(true)
	case win.IsVisible():
		win.Hide()
		onVisibility(false)
	default:
		win.Show()
		win.Focus()
		onVisibility(true)
	}
}

func (n *mygoShortcutNative) fire(action string) {
	if action == "showApp" {
		toggleWindowForShowApp(n.win, func(visible bool) { n.l.pushMain("window:visibility-changed", visible) })
	}
	n.l.pushMain("shortcut:triggered", action)
}
