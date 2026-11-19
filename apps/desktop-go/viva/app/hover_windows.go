//go:build windows

package app

import (
	"math"
	"syscall"
	"unsafe"
)

const (
	// vivaWindowClass 是 Viva v0.6.4 主窗口的窗口类名（internal/platform/windows/window_windows.go 里注册）。
	// Viva 没有导出窗口句柄，只能按类名加线程找回来；升级 Viva 时核对这个名字（找不到时 Root 会记一条日志，见 Root.OnScroll）。
	vivaWindowClass = "VivaNativeWindow"

	wmMouseMove = 0x0200

	// WM_MOUSEMOVE 的 wParam 里的按键状态位与对应的虚拟键。
	mkLButton, mkRButton, mkShift, mkControl, mkMButton = 0x1, 0x2, 0x4, 0x8, 0x10
	vkLButton, vkRButton, vkShift, vkControl, vkMButton = 0x01, 0x02, 0x10, 0x11, 0x04
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
	procFindWindowExW      = user32.NewProc("FindWindowExW")
	procGetDpiForWindow    = user32.NewProc("GetDpiForWindow")
	procPostMessageW       = user32.NewProc("PostMessageW")
	procGetKeyState        = user32.NewProc("GetKeyState")
)

// postHoverRefresh 向本线程上的 Viva 主窗口发一条 WM_MOUSEMOVE，坐标是 (x, y)（逻辑像素、窗口坐标），
// 让 Viva 自己的指针路径重算悬停（台账 B-031，见 kit.RefreshHoverAfterWheel）。
// 只能在窗口线程调用（滚轮处理里）；返回 false 表示没有找到窗口或消息没有发出去。
//
// 用 PostMessage 而不是直接 SendMessage：补发的移动要等当前这次滚轮处理（连同它的重绘）结束之后才被取出处理，
// 与渲染层「滚动之后刷新悬停」的先后一致，也不会在滚轮的处理里重入窗口过程。
// 按键状态位取当前真实的键盘与鼠标按键，窗口过程据此判断是否在拖动（标题栏的拖动判断读 MK_LBUTTON）。
func postHoverRefresh(x, y float32) bool {
	hwnd := findOwnWindow()
	if hwnd == 0 {
		return false
	}
	ok, _, _ := procPostMessageW.Call(hwnd, wmMouseMove, mouseKeyState(), clientPoint(x, y, windowScale(hwnd)))
	return ok != 0
}

// findOwnWindow 找本线程上的 Viva 主窗口：同类名、属于当前线程、没有所有者的顶层窗口。找不到返回 0。
// 同一个进程里别的 Viva 窗口（只有主窗口）与别的进程的实例（用户的归知）都不会命中：线程不同。
func findOwnWindow() uintptr {
	class, _ := syscall.UTF16PtrFromString(vivaWindowClass)
	thread, _, _ := procGetCurrentThreadID.Call()
	var after uintptr
	for {
		hwnd, _, _ := procFindWindowExW.Call(0, after, uintptr(unsafe.Pointer(class)), 0)
		if hwnd == 0 {
			return 0
		}
		owner, _, _ := procGetWindowThreadProcessID.Call(hwnd, 0)
		if owner == thread {
			return hwnd
		}
		after = hwnd
	}
}

// windowScale 是窗口的显示缩放（Viva 的 state.scale 同样取 GetDpiForWindow / 96）。读不到按 1。
func windowScale(hwnd uintptr) float32 {
	if procGetDpiForWindow.Find() != nil {
		return 1
	}
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	return scaleFromDPI(dpi)
}

// scaleFromDPI 把 DPI 换成缩放倍数（96 为 1 倍）；0 表示没读到，按 1。
func scaleFromDPI(dpi uintptr) float32 {
	if dpi == 0 {
		return 1
	}
	return float32(dpi) / 96
}

// clientPoint 把逻辑坐标换成 WM_MOUSEMOVE 的 lParam：客户区的设备像素，x 在低 16 位、y 在高 16 位（有符号，超出范围时夹住）。
func clientPoint(x, y, scale float32) uintptr {
	cx, cy := clampInt16(float64(x)*float64(scale)), clampInt16(float64(y)*float64(scale))
	return uintptr(uint32(uint16(cx)) | uint32(uint16(cy))<<16)
}

func clampInt16(v float64) int16 {
	return int16(math.Max(math.MinInt16, math.Min(math.MaxInt16, math.Round(v))))
}

// mouseKeyState 是此刻 WM_MOUSEMOVE 的 wParam：鼠标左、右、中键与 Shift、Ctrl 是否按下。
func mouseKeyState() uintptr {
	return mouseKeyStateFrom(func(vk uintptr) bool {
		state, _, _ := procGetKeyState.Call(vk)
		return keyIsDown(state)
	})
}

// keyIsDown 读 GetKeyState 的结果：第 15 位是「此刻按下」，第 0 位只是切换状态（大小写锁定之类），不算。
// 返回值是 16 位的 SHORT，高位以上的内容不可信，只看这一位。
func keyIsDown(state uintptr) bool { return state&0x8000 != 0 }

// mouseKeyStateFrom 按 down（某个虚拟键是否按下）拼出 wParam 的按键状态位。
func mouseKeyStateFrom(down func(vk uintptr) bool) uintptr {
	var state uintptr
	for _, k := range [...]struct{ vk, mk uintptr }{
		{vkLButton, mkLButton}, {vkRButton, mkRButton}, {vkMButton, mkMButton}, {vkShift, mkShift}, {vkControl, mkControl},
	} {
		if down(k.vk) {
			state |= k.mk
		}
	}
	return state
}
