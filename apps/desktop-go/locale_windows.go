//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// systemLocale 对应 app.getLocale()：用户默认区域名，如 "zh-CN"；取不到返回 "en"。
func systemLocale() string {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	buf := make([]uint16, 85) // LOCALE_NAME_MAX_LENGTH
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return "en"
	}
	return syscall.UTF16ToString(buf)
}
