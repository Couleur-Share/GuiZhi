//go:build windows

package app

// shiftHeld 报告此刻 Shift 是否按着（GetKeyState；过程与键码、判定位沿用悬停补发里的那一套）。
// 窗口线程调用：读到的是正在处理的那条消息之后的键盘状态，Enter 引起的重绘发生在这条消息的处理里，Shift 此时仍按着。
func shiftHeld() bool {
	state, _, _ := procGetKeyState.Call(vkShift)
	return keyIsDown(state)
}
