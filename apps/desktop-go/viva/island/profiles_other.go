//go:build !windows

package island

// processAlive 在非 Windows 平台上无法判断，一律按「活着」：不会删除别的进程留下的目录。
// 岛只在 Windows 上运行（官方只支持 Windows 11 x64），这里只为让包在其他平台上能编译。
func processAlive(int) bool { return true }
