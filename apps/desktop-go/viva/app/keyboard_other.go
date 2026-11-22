//go:build !windows

package app

// shiftHeld 在没有桌面外壳的平台上恒为否（官方只支持 Windows，这里只为让包能编译）。
func shiftHeld() bool { return false }
