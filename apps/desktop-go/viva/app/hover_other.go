//go:build !windows

package app

// postHoverRefresh：没有桌面外壳的平台什么也不做（官方只支持 Windows），按「已处理」返回，不触发找不到窗口的日志。
func postHoverRefresh(x, y float32) bool { return true }

// postHoverRefreshHere 同上：在指针此刻的位置补一次指针移动（台账 B-037）。
func postHoverRefreshHere() bool { return true }
