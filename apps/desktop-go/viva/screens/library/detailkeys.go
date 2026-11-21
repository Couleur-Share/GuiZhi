package library

import (
	"strings"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/kit"
)

// 详情相关的窗口级键盘（渲染层各组件挂在 window 上的 keydown 监听）：
//
//   - Ctrl/Meta+S：立即保存（ItemDetail 挂载期间，任何焦点，preventDefault）；
//   - Ctrl/Meta+F：打开查找条（工具栏摆着查找入口期间，任何焦点，preventDefault）；
//   - Alt+Z：切换专注阅读（知识库工作区挂载期间，任何焦点；按住 Ctrl / Meta 时不动，Shift 照常）；
//   - Esc：焦点在查找框里时关闭查找条且不再往外传（stopPropagation）；焦点在文章工具侧栏里时收起侧栏且不再往外传；
//     否则专注阅读模式下退出专注（焦点在浮层里时那一层自己处理）；
//   - Enter / Shift+Enter：查找框里下一个 / 上一个命中；
//   - Alt+← / Alt+→：详情浮层里的连续整理（上一条 / 下一条），焦点在输入框、文本域、可编辑区里时不动。
//
// 这些监听互相独立（渲染层各自 addEventListener），所以除了「查找框与工具侧栏里的 Esc」会拦下按键，其余都只做自己那一件事、不拦截 Esc。
//
// 键经两条路进来（Viva 的窗口把它们分开）：带修饰键的可打印字符（Ctrl+S、Ctrl+F、Alt+Z）走 Config.OnShortcut（detailShortcut），
// 具名键（Esc、Enter、方向键）走 Config.OnKey（detailKey）。两条路都只在知识库工作区挂载时才有作用。

// ListShortcut 是 Look.ShortcutHook：窗口里带修饰键的可打印字符。工作区没挂载时不插手；
// 不带 Ctrl / Alt / Meta 的普通字符（每敲一个字都会先来这里问一遍）立刻放行，不做任何事。
func (s *Screen) ListShortcut(e gfx.KeyEvent) gfx.EventResult {
	if !s.wsActive || e.Release || e.Modifiers&(gfx.ModifierControl|gfx.ModifierAlt|gfx.ModifierMeta) == 0 {
		return gfx.EventResult{}
	}
	return s.detailShortcut(e)
}

// detailShortcut 处理 Ctrl/Meta+S、Ctrl/Meta+F 与 Alt+Z。Handled 为真表示拦下了按键（渲染层的 preventDefault）。
func (s *Screen) detailShortcut(e gfx.KeyEvent) gfx.EventResult {
	mod := e.Modifiers
	ctrl := mod&(gfx.ModifierControl|gfx.ModifierMeta) != 0
	alt := mod&gfx.ModifierAlt != 0
	text := strings.ToLower(e.Text)
	handled := gfx.EventResult{Handled: true, Repaint: true}
	switch {
	case ctrl && text == "s":
		if s.detailActive {
			s.flushSave()
			return handled
		}
	case ctrl && text == "f":
		if s.detailActive && s.detail.panel.canFind {
			s.detail.panel.findOpen = true
			return handled
		}
	case alt && !ctrl && text == "z":
		s.toggleFocusReading()
		return handled
	}
	return gfx.EventResult{}
}

// detailKey 处理具名键。Handled 为真表示这一环拦下了按键（调用方直接返回）；Handled 为假而 Repaint 为真表示只改了界面状态，
// 按键仍交给后面的环（调用方把 Repaint 并进最终结果）。
func (s *Screen) detailKey(e gfx.KeyEvent, canvas *gfx.Canvas) gfx.EventResult {
	if e.Release {
		return gfx.EventResult{}
	}
	p := &s.detail.panel
	if res := s.detailFindKey(e); res.Handled {
		return res
	}
	focus := kit.FocusOf(canvas)
	alt := e.Modifiers&gfx.ModifierAlt != 0
	ctrl := e.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) != 0
	switch {
	case e.Key == gfx.KeyEscape:
		if p.findOpen && p.findFocused {
			p.findOpen = false
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if s.toolsEscape(focus) {
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if s.detail.focusReading && !focus.Floating() {
			s.SetFocusReading(false)
			return gfx.EventResult{Repaint: true} // 不拦截：浮层、菜单与批量条自己的 Esc 照常运行
		}
	case alt && !ctrl && (e.Key == gfx.KeyLeft || e.Key == gfx.KeyRight):
		if s.detail.modal.open && !focus.Editable() {
			dir := -1
			if e.Key == gfx.KeyRight {
				dir = 1
			}
			s.Store.Spawn(func() { s.Store.ReviewNavigate(s.Ctx, dir) })
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return gfx.EventResult{}
}

// detailFindKey 处理查找框里的 Enter（下一个）与 Shift+Enter（上一个）：渲染层在 input 的 onKeyDown 里 preventDefault。
// 焦点不在查找框里时不动。
func (s *Screen) detailFindKey(e gfx.KeyEvent) gfx.EventResult {
	p := &s.detail.panel
	if e.Key != gfx.KeyEnter || !p.findOpen || !p.findFocused {
		return gfx.EventResult{}
	}
	if e.Modifiers&gfx.ModifierShift != 0 {
		s.findStep(-1)
	} else {
		s.findStep(1)
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}
