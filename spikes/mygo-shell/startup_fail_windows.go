//go:build windows

package main

import "golang.org/x/sys/windows"

// showStartupErrorDialog 用系统 MessageBox 展示启动错误（不依赖窗口框架，此时它还没起来）。
func showStartupErrorDialog(title, text string) {
	t, err1 := windows.UTF16PtrFromString(text)
	c, err2 := windows.UTF16PtrFromString(title)
	if err1 != nil || err2 != nil {
		return
	}
	// MB_OK | MB_ICONERROR | MB_SETFOREGROUND
	_, _ = windows.MessageBox(0, t, c, 0x0|0x10|0x10000)
}
