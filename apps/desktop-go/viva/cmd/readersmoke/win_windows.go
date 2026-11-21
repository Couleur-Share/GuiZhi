//go:build windows

package main

import (
	"errors"
	"image"
	"syscall"
	"unsafe"
)

// 这里是 readersmoke 用的一点 Win32：找到本进程的 Viva 主窗口、取它的客户区在屏幕上的位置与缩放、
// 从屏幕上截图（DWM 合成之后的结果，原生视图盖住/被盖住的样子才看得见），以及问「屏幕上某一点最上面是哪个窗口」。
// Viva 没有导出窗口句柄（与 viva/app/hover_windows.go 同一个原因），按类名与进程找回来。

const vivaWindowClass = "VivaNativeWindow"

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	gdi32                   = syscall.NewLazyDLL("gdi32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procFindWindowExW       = user32.NewProc("FindWindowExW")
	procGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	procGetCurrentProcessID = kernel32.NewProc("GetCurrentProcessId")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procGetDpiForWindow     = user32.NewProc("GetDpiForWindow")
	procWindowFromPoint     = user32.NewProc("WindowFromPoint")
	procGetAncestor         = user32.NewProc("GetAncestor")
	procGetClassNameW       = user32.NewProc("GetClassNameW")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procBitBlt              = gdi32.NewProc("BitBlt")
	procGetDIBits           = gdi32.NewProc("GetDIBits")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	errCapture              = errors.New("截屏失败")
)

const (
	gaRoot     = 2
	srcCopy    = 0x00CC0020
	captureBlt = 0x40000000
)

type winRect struct{ left, top, right, bottom int32 }
type winPoint struct{ x, y int32 }

// findWindow 找本进程的 Viva 主窗口；找不到返回 0。
func findWindow() uintptr {
	class, _ := syscall.UTF16PtrFromString(vivaWindowClass)
	self, _, _ := procGetCurrentProcessID.Call()
	var after uintptr
	for {
		hwnd, _, _ := procFindWindowExW.Call(0, after, uintptr(unsafe.Pointer(class)), 0)
		if hwnd == 0 {
			return 0
		}
		var pid uint32
		procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if uintptr(pid) == self {
			return hwnd
		}
		after = hwnd
	}
}

// clientOnScreen 是窗口客户区在屏幕上的位置与大小（设备像素）。
func clientOnScreen(hwnd uintptr) (x, y, w, h int, ok bool) {
	var rc winRect
	if r, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); r == 0 {
		return 0, 0, 0, 0, false
	}
	pt := winPoint{}
	if r, _, _ := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt))); r == 0 {
		return 0, 0, 0, 0, false
	}
	return int(pt.x), int(pt.y), int(rc.right - rc.left), int(rc.bottom - rc.top), true
}

// windowScale 是窗口的显示缩放（96 DPI 为 1）。
func windowScale(hwnd uintptr) float64 {
	if procGetDpiForWindow.Find() != nil {
		return 1
	}
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// screenSize 是主显示器的大小（设备像素）。
func screenSize() (w, h int) {
	cx, _, _ := procGetSystemMetrics.Call(0)
	cy, _, _ := procGetSystemMetrics.Call(1)
	return int(cx), int(cy)
}

func className(hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

// windowAt 回答屏幕上 (x, y)（设备像素）最上面的窗口：leaf 是它的类名，root 是它的顶层祖先是否就是 hwnd。
func windowAt(hwnd uintptr, x, y int) (leafClass string, ownedByHwnd bool) {
	leaf, _, _ := procWindowFromPoint.Call(uintptr(uint32(int32(x))) | uintptr(uint32(int32(y)))<<32)
	if leaf == 0 {
		return "", false
	}
	root, _, _ := procGetAncestor.Call(leaf, gaRoot)
	return className(leaf), root == hwnd
}

// captureScreen 从屏幕上截 (x, y, w, h)（设备像素）的一块，要求这一块在屏幕内。
func captureScreen(x, y, w, h int) (*image.NRGBA, error) {
	if w <= 0 || h <= 0 {
		return nil, errCapture
	}
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, errCapture
	}
	defer procReleaseDC.Call(0, screen)
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	if mem == 0 {
		return nil, errCapture
	}
	defer procDeleteDC.Call(mem)
	bmp, _, _ := procCreateCompatibleBmp.Call(screen, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, errCapture
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(mem, bmp)
	ok, _, _ := procBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), screen, uintptr(x), uintptr(y), srcCopy|captureBlt)
	procSelectObject.Call(mem, old)
	if ok == 0 {
		return nil, errCapture
	}
	// BITMAPINFOHEADER：自上而下（负高度）的 32 位 BGRA。
	header := struct {
		size                        uint32
		width, height               int32
		planes, bits                uint16
		compression, sizeImage      uint32
		xPels, yPels                int32
		colorsUsed, colorsImportant uint32
	}{size: 40, width: int32(w), height: -int32(h), planes: 1, bits: 32}
	buf := make([]byte, w*h*4)
	if n, _, _ := procGetDIBits.Call(screen, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&header)), 0); n == 0 {
		return nil, errCapture
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4+0], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = buf[i*4+2], buf[i*4+1], buf[i*4+0], 0xFF
	}
	return img, nil
}
