//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32           = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

// openPathNative 用系统默认程序打开一个已存在的本地文件或目录（目录交给资源管理器，.exe 安装包交给系统运行）。
// 不能用 vapp.OpenPath：它出于安全拒绝目录与可执行文件，而这里的调用方正是「打开数据目录」「运行已下载的安装包」，
// 路径已由调用方校验（见 data_path.go 的 openDirectoryPath 与更新器的下载目录约束）。
// 这里只做通用的前置检查：非空、无 NUL、绝对路径、存在；然后直接 ShellExecuteW，不依赖任何事件循环。
func openPathNative(p string) error {
	if p == "" || strings.IndexByte(p, 0) >= 0 {
		return fmt.Errorf("无效的路径")
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("不是绝对路径：%s", p)
	}
	if _, err := os.Stat(p); err != nil {
		return err
	}
	return shellExecuteOpen(p)
}

func shellExecuteOpen(p string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return err
	}
	// SW_SHOWNORMAL = 1；返回值 <= 32 表示失败（其值即错误码）。
	ret, _, callErr := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, 1)
	if ret <= 32 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return fmt.Errorf("ShellExecute 失败（%d）：%w", ret, callErr)
		}
		return fmt.Errorf("ShellExecute 失败（%d）", ret)
	}
	return nil
}
