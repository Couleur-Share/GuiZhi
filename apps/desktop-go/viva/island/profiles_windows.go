//go:build windows

package island

import (
	"errors"
	"syscall"
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259 // GetExitCodeProcess 对仍在运行的进程返回 STILL_ACTIVE

	// errorInvalidParameter 是 OpenProcess 对不存在的进程号返回的 ERROR_INVALID_PARAMETER（标准库 syscall 没有这个常量）。
	errorInvalidParameter = syscall.Errno(87)
)

// processAlive 报告进程号 pid 是否还有一个在运行的进程。
// 读不到信息时按「活着」处理：这个判断只用来决定要不要删别的实例的目录，宁可少删。
// 进程号被别的程序复用时同样按活着处理（留着一个目录，下次再清）。
func processAlive(pid int) bool {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// 没有这个进程号（ERROR_INVALID_PARAMETER）= 已退出；没有权限打开 = 进程存在
		return !errors.Is(err, errorInvalidParameter)
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}
