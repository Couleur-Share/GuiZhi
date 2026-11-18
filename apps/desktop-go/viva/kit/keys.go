package kit

import "github.com/vivaui/viva/gfx"

// OnKey 是窗口 OnKey 的入口：按键先让悬停提示收起（渲染层 TooltipLayer 的 keydown 监听，台账 B-017），
// 再交给 Tab 可达（TabKey，台账 B-030）。没有提示可收、也不是要处理的 Tab 时返回零值，按键交给路由器照常处理。
//
// 已知的覆盖范围：键经 Config.OnKey 到达的才算（Tab、方向键、Enter、Space、Escape、功能键等）；
// 文字输入框里敲字时，Viva 的 Windows 窗口把编辑键直接交给文字系统、不走 OnKey（读 internal/platform/windows/window_windows.go，
// 没有真窗口验证），所以 Look.Input 另外在文字被编辑时补记一次按键；光标移动键与单按的修饰键不收起提示。
func (l *Look) OnKey(e gfx.KeyEvent) gfx.EventResult {
	res := l.TabKey(e)
	if !e.Release && l.tips != nil && l.tips.noteKey() {
		res.Repaint = true
	}
	return res
}
