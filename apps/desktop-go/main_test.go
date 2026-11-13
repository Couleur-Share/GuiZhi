package main

import (
	"os"
	"testing"
)

// 渲染/采集浏览器测试会把测试二进制本身当作采集子进程拉起（见 capture_browser.go），这里负责接管。
func TestMain(m *testing.M) {
	if runCaptureChildIfRequested() {
		return
	}
	os.Exit(m.Run())
}
