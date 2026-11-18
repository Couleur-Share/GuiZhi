//go:build windows

package app

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
)

// ownProcessForeground 报告前台窗口是否属于本进程（渲染层 window blur 的近似，见 kit.TooltipState.Active）。
// 前台窗口为空（切换窗口的瞬间）按不在前台算。本进程的其他顶层窗口（系统对话框等）在前台时也算在前台。
func ownProcessForeground() bool {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid == uint32(os.Getpid())
}
