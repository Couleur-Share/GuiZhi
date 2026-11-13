//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess 让安装包脱离本进程：本进程退出后安装器继续运行。
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
}
