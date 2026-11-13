//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideChildWindow 对应 spawn 的 windowsHide: true：子进程不弹控制台窗口。
func hideChildWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
