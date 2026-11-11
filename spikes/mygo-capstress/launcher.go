package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 离屏运行：在一个独立的 Win32 桌面里启动压测本体。
// 该桌面不是用户正在使用的桌面，里面的任何窗口（包括采集进程的 WebView2 窗口、页面弹窗）
// 都不会出现在屏幕上、任务栏里，也抢不到用户的焦点。子进程默认继承父进程的桌面，所以采集进程也在其中。

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procCreateDesktop = user32.NewProc("CreateDesktopW")
	procCloseDesktop  = user32.NewProc("CloseDesktop")
)

func runOffscreen(args []string, logPath string, timeout time.Duration) int {
	name := fmt.Sprintf("guizhi-capstress-%d", os.Getpid())
	np, _ := windows.UTF16PtrFromString(name)
	const genericAll = 0x10000000
	h, _, err := procCreateDesktop.Call(uintptr(unsafe.Pointer(np)), 0, 0, 0, genericAll, 0)
	if h == 0 {
		fmt.Println("[FAIL] 创建离屏桌面失败：", err)
		return 2
	}
	defer procCloseDesktop.Call(h)

	exe, _ := os.Executable()
	cmdline, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{exe, "-inner", "-log", logPath}, args...)))
	si := windows.StartupInfo{Desktop: np}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(nil, cmdline, nil, nil, false, 0, nil, nil, &si, &pi); err != nil {
		fmt.Println("[FAIL] 在离屏桌面启动压测失败：", err)
		return 2
	}
	defer windows.CloseHandle(pi.Process)
	defer windows.CloseHandle(pi.Thread)
	fmt.Printf("压测已在离屏桌面 %q 中运行（pid %d），日志：%s\n", name, pi.ProcessId, logPath)
	ev, _ := windows.WaitForSingleObject(pi.Process, uint32(timeout/time.Millisecond))
	if ev == uint32(windows.WAIT_TIMEOUT) {
		windows.TerminateProcess(pi.Process, 3)
		fmt.Println("[FAIL] 外层超时，已结束压测进程")
		return 3
	}
	var code uint32
	windows.GetExitCodeProcess(pi.Process, &code)
	return int(code)
}
