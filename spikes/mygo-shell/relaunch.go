package main

// 重启握手：旧进程拉起新进程后立即退出，新进程要等旧进程真正退出（释放单实例锁、数据库文件句柄）再继续，
// 否则会被单实例锁判成「已有实例」直接退出，用户看到应用消失却没有重启。

import (
	"os"
	"strconv"
	"time"
)

const relaunchParentEnv = "GUIZHI_RELAUNCH_PARENT"

// waitForRelaunchParent 若本进程由重启拉起，最多等旧进程 15 秒退出。
func waitForRelaunchParent() {
	pid, err := strconv.Atoi(os.Getenv(relaunchParentEnv))
	os.Unsetenv(relaunchParentEnv)
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return // Windows 上进程已不存在时 FindProcess 直接报错
	}
	done := make(chan struct{})
	go func() { _, _ = p.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
	}
}
