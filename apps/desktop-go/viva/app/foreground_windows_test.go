//go:build windows

package app

import "testing"

// 冒烟：user32 的两个过程名拼对了、调用不崩（过程名拼错只会在第一次调用时 panic）。
// 结果取决于测试时的前台窗口，不断言真假。
func TestOwnProcessForegroundCallsUser32(t *testing.T) {
	_ = ownProcessForeground()
}
