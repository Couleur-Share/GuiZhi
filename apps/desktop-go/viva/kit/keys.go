package kit

import "github.com/vivaui/viva/gfx"

// OnKey 是窗口 OnKey 的入口：按键先让悬停提示收起（渲染层 TooltipLayer 的 keydown 监听，台账 B-017），
// 再交给 Tab 可达（TabKey，台账 B-030）。没有提示可收、也不是要处理的 Tab 时返回零值，按键交给路由器照常处理。
// 界面的窗口级键盘（KeyHook）最后看到同一次按键，结果合并进来。
//
// 已知的覆盖范围：键经 Config.OnKey 到达的才算（Tab、方向键、Enter、Space、Escape、功能键等）；
// 文字输入框里敲字时，Viva 的 Windows 窗口把编辑键直接交给文字系统、不走 OnKey（读 internal/platform/windows/window_windows.go，
// 没有真窗口验证），所以 Look.Input 另外在文字被编辑时补记一次按键；光标移动键与单按的修饰键不收起提示。
func (l *Look) OnKey(e gfx.KeyEvent) gfx.EventResult {
	if !e.Release && l.tips != nil {
		l.tips.endTabScroll() // 上一次「Tab 先滚动」的压制到此为止；这次按键若又是滚动的 Tab，TabKey 会重新置位（B-032）
	}
	res := l.TabKey(e)
	if !e.Release && l.tips != nil && l.tips.noteKey() {
		res.Repaint = true
	}
	if l.KeyHook != nil {
		res = MergeKey(res, l.KeyHook(e))
	}
	return res
}

// OnShortcut 是窗口 OnShortcut 的入口：带修饰键的可打印字符（Ctrl+S、Alt+Z 一类）与每一个不带修饰键的普通字符都先来这里问一遍，
// 所以没有接钩子时立即返回零值、有钩子时由钩子自己尽快放行。Viva 把这些键与 OnKey 的具名键分成两条路（app.Config.OnShortcut）。
func (l *Look) OnShortcut(e gfx.KeyEvent) gfx.EventResult {
	if l.ShortcutHook == nil {
		return gfx.EventResult{}
	}
	return l.ShortcutHook(e)
}

// KeyFirst 把窗口按键钩子 first 排在页面自己的按键处理 page（page.Config 装上的 OnKey：拖动取消、焦点组与菜单里方向键的移动、
// 下拉的键盘、快捷键、字段的 OnKey……，再交给 base）之前：钩子先看到每一次按键（Tab 要在页面遍历之前滚进视野，列表键盘在菜单
// 打开时也要看到方向键），它没有处理（Handled 为假）的，再交给页面，两边的结果合并。
//
// 不能直接把 config.OnKey 换成钩子：page.Config 之后的 OnKey 就是页面的按键路由，换掉它菜单、下拉里的方向键就不动了。
// 生产窗口（app.bindInput）、对照（parity）与各测试夹具共用这一处，测的就是生产的接线。page 为空时就是钩子本身。
func KeyFirst(first, page func(gfx.KeyEvent) gfx.EventResult) func(gfx.KeyEvent) gfx.EventResult {
	if page == nil {
		return first
	}
	return func(e gfx.KeyEvent) gfx.EventResult {
		res := first(e)
		if res.Handled {
			return res
		}
		return MergeKey(res, page(e))
	}
}

// MergeKey 把两个按键处理结果合成一个：布尔项取或，焦点、剪贴板文字、激活目标取先给出的那个。
// Look.OnKey 用它把 TabKey、提示收起与界面的窗口级键盘（KeyHook）的结果合在一起。
func MergeKey(a, b gfx.EventResult) gfx.EventResult {
	a.Handled = a.Handled || b.Handled
	a.Repaint = a.Repaint || b.Repaint
	a.Blur = a.Blur || b.Blur
	a.ContinueTraversal = a.ContinueTraversal || b.ContinueTraversal
	a.FocusVisible = a.FocusVisible || b.FocusVisible
	if a.Focus.IsZero() {
		a.Focus = b.Focus
	}
	if a.ClipboardText == "" {
		a.ClipboardText = b.ClipboardText
	}
	if a.Activate.IsZero() {
		a.Activate = b.Activate
	}
	return a
}
