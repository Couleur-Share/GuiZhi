//go:build !windows

package app

// ownProcessForeground：没有桌面外壳的平台恒为真（不探测失焦）。
func ownProcessForeground() bool { return true }
