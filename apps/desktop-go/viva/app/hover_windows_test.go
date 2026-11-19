//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services/servicestest"
)

// 测试里的 Win32 常量一律写字面值，不引用被测代码里的同名常量：被测常量写错时测试才能发现。
const (
	win32WMMouseMove = 0x0200
	win32VivaClass   = "VivaNativeWindow" // 与 Viva v0.6.4 的主窗口类名一致，由 TestVivaWindowClassMatchesTheVivaSource 核对
)

var (
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procUnregisterClassW    = user32.NewProc("UnregisterClassW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	moveMessages            atomic.Pointer[[]moveMessage] // 测试窗口收到的 WM_MOUSEMOVE
	registeredTestClassOnce atomic.Bool
)

type moveMessage struct{ wparam, lparam uintptr }

// WNDCLASSEXW 与 MSG 的布局（amd64）。
type testWindowClass struct {
	size, style                        uint32
	proc                               uintptr
	clsExtra, wndExtra                 int32
	instance, icon, cursor, background uintptr
	menu, class                        *uint16
	iconSm                             uintptr
}

type testMessage struct {
	hwnd    uintptr
	message uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	x, y    int32
	private uint32
}

var testWindowProc = syscall.NewCallback(func(hwnd, msg, wparam, lparam uintptr) uintptr {
	if msg == win32WMMouseMove {
		cur := moveMessages.Load()
		var next []moveMessage
		if cur != nil {
			next = append(next, *cur...)
		}
		next = append(next, moveMessage{wparam, lparam})
		moveMessages.Store(&next)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
})

// registerVivaClass 以 Viva 主窗口的类名注册一个测试用的窗口类（整个测试进程只注册一次）。
func registerVivaClass(t *testing.T) *uint16 {
	t.Helper()
	name, _ := syscall.UTF16PtrFromString(win32VivaClass)
	if registeredTestClassOnce.CompareAndSwap(false, true) {
		instance, _, _ := procGetModuleHandleW.Call(0)
		class := testWindowClass{size: uint32(unsafe.Sizeof(testWindowClass{})), proc: testWindowProc, instance: instance, class: name}
		if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
			t.Fatalf("注册测试窗口类失败：%v", err)
		}
	}
	return name
}

// createHiddenWindow 在当前（已锁定的）线程上建一个不可见的顶层窗口（无所有者），返回句柄。
// 不调用 testing.T，所以别的 goroutine 里也能用。
func createHiddenWindow(class *uint16) (uintptr, error) {
	instance, _, _ := procGetModuleHandleW.Call(0)
	const wsPopup = 0x80000000
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), 0, wsPopup, 0, 0, 100, 100, 0, 0, instance, 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("建测试窗口失败：%w", err)
	}
	return hwnd, nil
}

// pumpFor 取出并分发当前线程的消息，直到 done 返回真或超时。
func pumpFor(timeout time.Duration, done func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var msg testMessage
		for {
			if got, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1); got == 0 {
				break
			}
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
		if done() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return done()
}

func unpack(lparam uintptr) (x, y int16) {
	return int16(lparam & 0xffff), int16((lparam >> 16) & 0xffff)
}

// 逻辑坐标 → WM_MOUSEMOVE 的 lParam：乘缩放、四舍五入、两个有符号 16 位，超出范围夹住。
func TestClientPointPacksScaledDevicePixels(t *testing.T) {
	cases := []struct {
		x, y, scale float32
		wx, wy      int16
	}{
		{100, 50, 1, 100, 50},
		{100, 50, 2, 200, 100},
		{12.4, 7.6, 1, 12, 8},
		{0.5, 1.5, 1, 1, 2},
		{10, 10, 1.25, 13, 13}, // 12.5 → 13
		{-3, -4.4, 2, -6, -9},
		{20000, 20000, 2, 32767, 32767},
		{-20000, -20000, 2, -32768, -32768},
		{0, 0, 3, 0, 0},
	}
	for _, c := range cases {
		x, y := unpack(clientPoint(c.x, c.y, c.scale))
		if x != c.wx || y != c.wy {
			t.Errorf("clientPoint(%g, %g, %g) = (%d, %d)，期望 (%d, %d)", c.x, c.y, c.scale, x, y, c.wx, c.wy)
		}
	}
}

// DPI → 缩放倍数：96 为 1 倍（Windows 的 100%），0 表示没读到，按 1。
func TestScaleFromDPI(t *testing.T) {
	for dpi, want := range map[uintptr]float32{0: 1, 96: 1, 120: 1.25, 144: 1.5, 192: 2, 288: 3} {
		if got := scaleFromDPI(dpi); got != want {
			t.Errorf("scaleFromDPI(%d) = %g，期望 %g", dpi, got, want)
		}
	}
}

// 按键状态位（Win32 文档的值）：左、右、中键与 Shift、Ctrl 各对应 MK_* 位，互不串位。
// 虚拟键：VK_LBUTTON 0x01、VK_RBUTTON 0x02、VK_MBUTTON 0x04、VK_SHIFT 0x10、VK_CONTROL 0x11；
// MK_LBUTTON 0x1、MK_RBUTTON 0x2、MK_SHIFT 0x4、MK_CONTROL 0x8、MK_MBUTTON 0x10。
func TestMouseKeyStateMapsButtonsAndModifiers(t *testing.T) {
	cases := []struct {
		down []uintptr
		want uintptr
	}{
		{nil, 0},
		{[]uintptr{0x01}, 0x1},
		{[]uintptr{0x02}, 0x2},
		{[]uintptr{0x10}, 0x4},
		{[]uintptr{0x11}, 0x8},
		{[]uintptr{0x04}, 0x10},
		{[]uintptr{0x01, 0x11}, 0x9},
		{[]uintptr{0x01, 0x02, 0x04, 0x10, 0x11}, 0x1f},
		{[]uintptr{0x41, 0x12, 0x20}, 0}, // 别的键（A、Alt、空格）不影响
	}
	for _, c := range cases {
		held := map[uintptr]bool{}
		for _, vk := range c.down {
			held[vk] = true
		}
		if got := mouseKeyStateFrom(func(vk uintptr) bool { return held[vk] }); got != c.want {
			t.Errorf("按下 %#v：状态位 %#x，期望 %#x", c.down, got, c.want)
		}
	}
	_ = mouseKeyState() // 真实的 GetKeyState 过程名拼对了、调用不崩
}

// GetKeyState 的返回：第 15 位才是「此刻按下」；第 0 位是切换状态（大小写锁定），不算；高位以上不可信。
func TestKeyIsDownReadsOnlyTheDownBit(t *testing.T) {
	for state, want := range map[uintptr]bool{0: false, 1: false, 0x7fff: false, 0x8000: true, 0x8001: true, 0xffff8000: true, 0xffff0001: false} {
		if got := keyIsDown(state); got != want {
			t.Errorf("keyIsDown(%#x) = %v，期望 %v", state, got, want)
		}
	}
}

// 窗口类名是 Viva 的内部细节（没有导出的句柄，只能按类名找回主窗口）：直接读钉死版本的 Viva 源码核对，
// 升级 Viva 之后类名变了，这条先失败，而不是悬停刷新悄悄失效。
func TestVivaWindowClassMatchesTheVivaSource(t *testing.T) {
	if vivaWindowClass != win32VivaClass {
		t.Fatalf("vivaWindowClass = %q，期望 %q", vivaWindowClass, win32VivaClass)
	}
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/vivaui/viva").Output()
	if err != nil {
		t.Fatalf("找不到 Viva 的源码目录：%v", err)
	}
	dir := strings.TrimSpace(string(out))
	src, err := os.ReadFile(filepath.Join(dir, "internal", "platform", "windows", "window_windows.go"))
	if err != nil {
		t.Fatalf("读不到 Viva 的窗口源码：%v", err)
	}
	if want := fmt.Sprintf("UTF16PtrFromString(%q)", win32VivaClass); !strings.Contains(string(src), want) {
		t.Fatalf("Viva 的窗口源码里没有注册类名 %q（找 %s）：升级 Viva 之后窗口类名变了，更新 vivaWindowClass 并重新核对 B-031", win32VivaClass, want)
	}
}

// 系统实现找不到 Viva 窗口（升级 Viva 后类名变了之类）时，悬停不会再自动刷新：Root 记一条日志、只记一次，不悄悄失效。
// 注入了记录器（Options.HoverRefresh）的实例走自己的通道，不记；没有日志服务时也不崩。
func TestRootLogsOnceWhenTheSystemPosterFindsNoWindow(t *testing.T) {
	scrollTwice := func(f *rootFixture) {
		t.Helper()
		for i := 0; i < 2; i++ {
			if err := f.session.Scroll(gfx.ScrollEvent{X: 500, Y: 400, DeltaY: 60, Precise: true}); err != nil {
				t.Fatalf("滚动失败：%v", err)
			}
		}
	}
	log := &servicestest.Logger{}
	scrollTwice(openRootSize(t, 800, 600, func(o *Options) { o.HoverRefresh, o.Services.Log = nil, log }))
	entries := log.Entries()
	if len(entries) != 1 || entries[0].Scope != "native" || entries[0].Action != "hover-refresh" {
		t.Fatalf("没有窗口可发时应记且只记一条 native/hover-refresh 日志，实际 %+v", entries)
	}
	if entries[0].Message != errHoverRefreshNoWindow.Error() || !strings.Contains(entries[0].Message, "B-031") {
		t.Fatalf("日志内容应是找不到窗口的诊断信息（带台账编号，方便回查），实际 %q", entries[0].Message)
	}

	injected := &servicestest.Logger{}
	scrollTwice(openRootSize(t, 800, 600, func(o *Options) { o.Services.Log = injected }))
	if got := injected.Entries(); len(got) != 0 {
		t.Fatalf("注入了记录器的实例不该记日志，实际 %+v", got)
	}

	scrollTwice(openRootSize(t, 800, 600, func(o *Options) { o.HoverRefresh = nil })) // 没有日志服务：不崩
}

// 真窗口：补发的 WM_MOUSEMOVE 到达本线程上的 Viva 类窗口，lParam 是按窗口缩放换算后的客户区坐标；
// 另一个线程上的同类名窗口（别的实例）不会被命中；找不到窗口时返回 false。
func TestPostHoverRefreshReachesTheWindowOfThisThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class := registerVivaClass(t)
	t.Cleanup(func() { procUnregisterClassW.Call(uintptr(unsafe.Pointer(class)), 0) })
	moveMessages.Store(nil)

	if postHoverRefresh(10, 10) {
		t.Fatal("本线程没有 Viva 窗口，不该发出消息")
	}

	// 另一个线程上的同类名窗口：不是本线程的，不命中。
	ready := make(chan error, 1)
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		hwnd, err := createHiddenWindow(class)
		ready <- err
		if err != nil {
			return
		}
		pumpFor(10*time.Second, func() bool {
			select {
			case <-stop:
				return true
			default:
				return false
			}
		})
		procDestroyWindow.Call(hwnd)
	}()
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	if postHoverRefresh(10, 10) {
		t.Fatal("只有别的线程上有同类名窗口，不该命中")
	}
	close(stop)
	<-finished

	hwnd, err := createHiddenWindow(class)
	if err != nil {
		t.Fatal(err)
	}
	defer procDestroyWindow.Call(hwnd)
	if !postHoverRefresh(12.5, 30) {
		t.Fatal("本线程有 Viva 窗口，应当发出消息")
	}
	if !pumpFor(2*time.Second, func() bool { m := moveMessages.Load(); return m != nil && len(*m) > 0 }) {
		t.Fatal("2 秒内没有收到补发的 WM_MOUSEMOVE")
	}
	msgs := *moveMessages.Load()
	if len(msgs) != 1 {
		t.Fatalf("应当恰好收到一条，实际 %d 条", len(msgs))
	}
	if want := clientPoint(12.5, 30, windowScale(hwnd)); msgs[0].lparam != want {
		x, y := unpack(msgs[0].lparam)
		wx, wy := unpack(want)
		t.Fatalf("lParam 是 (%d,%d)，期望按窗口缩放 %g 换算的 (%d,%d)", x, y, windowScale(hwnd), wx, wy)
	}
	if msgs[0].wparam&^0x1f != 0 {
		t.Fatalf("wParam %#x 里有 MK_* 之外的位", msgs[0].wparam)
	}
}
