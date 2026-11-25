//go:build windows

package main

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32WS                         = windows.NewLazySystemDLL("user32.dll")
	shcoreWS                         = windows.NewLazySystemDLL("shcore.dll")
	procEnumDisplayMonitors          = user32WS.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW              = user32WS.NewProc("GetMonitorInfoW")
	procSetThreadDpiAwarenessContext = user32WS.NewProc("SetThreadDpiAwarenessContext")
	procGetDpiForMonitor             = shcoreWS.NewProc("GetDpiForMonitor")
)

const (
	monitorInfoPrimary        = 0x1 // MONITORINFOF_PRIMARY
	mdtEffectiveDPI           = 0   // MDT_EFFECTIVE_DPI
	dpiContextPerMonitorAware = ^uintptr(3)
)

type winRect struct{ left, top, right, bottom int32 }

type winMonitorInfo struct {
	size      uint32
	rcMonitor winRect
	rcWork    winRect
	flags     uint32
}

// 枚举回调是进程级的（syscall.NewCallback 数量有上限，不能每次新建）；用互斥保护的收集目标传结果。
var (
	monitorEnumMu   sync.Mutex
	monitorEnumSink []monitorWorkArea
	monitorEnumProc = syscall.NewCallback(func(hMonitor, _, _, _ uintptr) uintptr {
		var info winMonitorInfo
		info.size = uint32(unsafe.Sizeof(info))
		if ok, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
			return 1
		}
		dpi := uint32(96)
		if procGetDpiForMonitor.Find() == nil {
			var dpiX, dpiY uint32
			if hr, _, _ := procGetDpiForMonitor.Call(hMonitor, mdtEffectiveDPI, uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY))); hr == 0 && dpiX > 0 {
				dpi = dpiX
			}
		}
		monitorEnumSink = append(monitorEnumSink, monitorWorkArea{
			area:    logicalWorkArea(info.rcWork.left, info.rcWork.top, info.rcWork.right, info.rcWork.bottom, dpi),
			primary: info.flags&monitorInfoPrimary != 0,
		})
		return 1 // 继续枚举
	})
)

// vivaDisplayWorkAreas 读各显示器的工作区（逻辑像素，主显示器在首位），对应 Electron 的 screen.getAllDisplays().map(d => d.workArea)。
// 不用 mygo.Screen：Viva 路径下 mygo 的事件循环不运行，调它会永久等待。
// 坐标系与 Viva 的 OnFrameChange / Config.Origin 一致：物理像素按该显示器的有效 DPI 缩放（原点坐标 / scale）。
// 进程可能尚未被声明为每显示器 DPI 感知（Viva 在事件循环启动时才设置），所以枚举期间临时把本线程切到每显示器感知，避免拿到虚拟化坐标。
func vivaDisplayWorkAreas() []windowBounds {
	monitorEnumMu.Lock()
	defer monitorEnumMu.Unlock()
	runtime.LockOSThread() // DPI 感知上下文是线程级的：设置、枚举、恢复必须在同一条系统线程上
	defer runtime.UnlockOSThread()
	if procSetThreadDpiAwarenessContext.Find() == nil {
		if old, _, _ := procSetThreadDpiAwarenessContext.Call(dpiContextPerMonitorAware); old != 0 {
			defer procSetThreadDpiAwarenessContext.Call(old)
		}
	}
	monitorEnumSink = nil
	procEnumDisplayMonitors.Call(0, 0, monitorEnumProc, 0)
	areas := monitorEnumSink
	monitorEnumSink = nil
	return primaryFirst(areas)
}
