//go:build windows

package main

import "golang.org/x/sys/windows"

const stillActive = 259

// processAlive 对应 TS 的 process.kill(pid, 0)：进程存在（含拒绝访问）即视为存活。
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}
