//go:build windows

package app

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services/servicestest"
)

// 本文件测「在指针此刻的位置补一次指针移动」（台账 B-037）：键盘滚动列表、Tab 滚动、列表换内容之后悬停要跟上。
// 真光标不动（测试不得移动用户的光标），所以系统实现只测「光标不在本窗口上时什么也不发」与找不到窗口时的失败；
// 客户区命中与 lParam 打包是纯函数，用字面值对拍。

// 客户区命中：左、上边界含，右、下边界不含（RECT 的约定）；负坐标与越界都不算。
func TestClientMoveParamIsInsideOnlyForTheClientArea(t *testing.T) {
	client := winRect{left: 0, top: 0, right: 800, bottom: 600}
	cases := []struct {
		pt     winPoint
		inside bool
		wx, wy int16
	}{
		{winPoint{0, 0}, true, 0, 0},
		{winPoint{799, 599}, true, 799, 599},
		{winPoint{800, 300}, false, 0, 0},
		{winPoint{400, 600}, false, 0, 0},
		{winPoint{-1, 10}, false, 0, 0},
		{winPoint{10, -1}, false, 0, 0},
		{winPoint{123, 456}, true, 123, 456},
	}
	for _, c := range cases {
		lparam, inside := clientMoveParam(c.pt, client)
		if inside != c.inside {
			t.Errorf("clientMoveParam(%+v) inside = %v，期望 %v", c.pt, inside, c.inside)
			continue
		}
		if !inside {
			continue
		}
		if x, y := unpack(lparam); x != c.wx || y != c.wy {
			t.Errorf("clientMoveParam(%+v) = (%d, %d)，期望 (%d, %d)", c.pt, x, y, c.wx, c.wy)
		}
	}
	// 客户区不从 0 起（带边框的窗口不会这样，但函数不该假设）：按点相对于 RECT 的范围判断，坐标原样打包。
	if _, inside := clientMoveParam(winPoint{5, 5}, winRect{left: 10, top: 10, right: 100, bottom: 100}); inside {
		t.Error("点在 RECT 左上角之外不算命中")
	}
}

// 补不补、补在哪：光标下面的窗口必须就是本窗口，且落在客户区里；两个条件缺一不可。
func TestHoverTargetNeedsTheCursorOverTheWindowAndInsideItsClientArea(t *testing.T) {
	const hwnd, other = 0x1234, 0x5678
	area := winRect{right: 800, bottom: 600}
	inside := winPoint{x: 120, y: 80}
	lparam, post := hoverTarget(hwnd, hwnd, inside, area)
	if !post {
		t.Fatal("光标在本窗口上、落在客户区里，应当补")
	}
	if x, y := unpack(lparam); x != 120 || y != 80 {
		t.Fatalf("lParam 应是 (120, 80)，实际 (%d, %d)", x, y)
	}
	if _, post := hoverTarget(hwnd, other, inside, area); post {
		t.Error("光标下是别的窗口，不该补")
	}
	if _, post := hoverTarget(hwnd, 0, inside, area); post {
		t.Error("光标下没有窗口（0），不该补")
	}
	if _, post := hoverTarget(hwnd, hwnd, winPoint{x: 800, y: 80}, area); post {
		t.Error("坐标在客户区之外，不该补")
	}
}

// 补发的流程（refreshHoverAt）用替身走一遍：问的顺序与坐标系（窗口按屏幕坐标问、客户区按换算后的坐标判）、
// 各个系统调用失败时的返回、只有光标在本窗口上且在客户区里才投递。窗口放在屏幕 (100, 50)，客户区 800×600。
func TestRefreshHoverAtFollowsTheProbeAnswers(t *testing.T) {
	const hwnd, other = 0x1234, 0x5678
	origin := winPoint{x: 100, y: 50}
	type posted struct{ hwnd, lparam uintptr }
	// run 用给定的屏幕光标位置、光标下的窗口跑一遍，返回结果、投递记录与 windowAt 被问到的坐标。
	run := func(cursor winPoint, under uintptr, fail func(*hoverProbe)) (ok bool, sent []posted, asked []winPoint) {
		p := hoverProbe{
			cursor:   func() (winPoint, bool) { return cursor, true },
			windowAt: func(pt winPoint) uintptr { asked = append(asked, pt); return under },
			toClient: func(h uintptr, pt winPoint) (winPoint, bool) {
				if h != hwnd {
					t.Errorf("换算客户区坐标问的是别的窗口 %#x", h)
				}
				return winPoint{x: pt.x - origin.x, y: pt.y - origin.y}, true
			},
			client: func(h uintptr) (winRect, bool) { return winRect{right: 800, bottom: 600}, true },
			post:   func(h, lparam uintptr) bool { sent = append(sent, posted{h, lparam}); return true },
		}
		if fail != nil {
			fail(&p)
		}
		return refreshHoverAt(hwnd, p), sent, asked
	}

	cursor := winPoint{x: 220, y: 130} // 客户区坐标 (120, 80)
	ok, sent, asked := run(cursor, hwnd, nil)
	if !ok || len(sent) != 1 {
		t.Fatalf("光标在本窗口上、客户区里：应投递一次并返回 true，实际 ok=%v 投递 %d 次", ok, len(sent))
	}
	if x, y := unpack(sent[0].lparam); sent[0].hwnd != hwnd || x != 120 || y != 80 {
		t.Fatalf("应向本窗口投递客户区坐标 (120, 80)，实际 %#x (%d, %d)", sent[0].hwnd, x, y)
	}
	if len(asked) != 1 || asked[0] != cursor {
		t.Fatalf("问光标下的窗口要用屏幕坐标 %+v，实际 %+v", cursor, asked)
	}

	if ok, sent, _ := run(cursor, other, nil); !ok || len(sent) != 0 {
		t.Fatalf("光标下是别的窗口：不投递、按已处理返回，实际 ok=%v 投递 %d 次", ok, len(sent))
	}
	if ok, sent, _ := run(winPoint{x: 950, y: 130}, hwnd, nil); !ok || len(sent) != 0 { // 客户区坐标 (850, 80)
		t.Fatalf("光标在客户区之外：不投递、按已处理返回，实际 ok=%v 投递 %d 次", ok, len(sent))
	}

	failures := []struct {
		name string
		fail func(*hoverProbe)
	}{
		{"读不到光标", func(p *hoverProbe) { p.cursor = func() (winPoint, bool) { return winPoint{}, false } }},
		{"换算客户区坐标失败", func(p *hoverProbe) {
			p.toClient = func(uintptr, winPoint) (winPoint, bool) { return winPoint{}, false }
		}},
		{"读不到客户区", func(p *hoverProbe) { p.client = func(uintptr) (winRect, bool) { return winRect{}, false } }},
		{"投递失败", func(p *hoverProbe) { p.post = func(uintptr, uintptr) bool { return false } }},
	}
	for _, f := range failures {
		if ok, _, _ := run(cursor, hwnd, f.fail); ok {
			t.Errorf("%s：应返回 false（调用方据此记日志）", f.name)
		}
	}
}

// 本线程没有 Viva 窗口：找不到窗口，返回 false（调用方据此记一条日志）。
func TestPostHoverRefreshHereReportsAMissingWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if postHoverRefreshHere() {
		t.Fatal("本线程没有 Viva 窗口，应当返回 false")
	}
}

// 光标不在本窗口上（隐藏的窗口永远不在光标下面）：什么也不发，按「已处理」返回——
// Chromium 在指针不在视图上时同样不补假的鼠标移动，也不能向别人的窗口里投消息。
func TestPostHoverRefreshHereSendsNothingWhenTheCursorIsNotOverTheWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class := registerVivaClass(t)
	hwnd, err := createHiddenWindow(class)
	if err != nil {
		t.Fatal(err)
	}
	defer procDestroyWindow.Call(hwnd)
	moveMessages.Store(nil)
	if !postHoverRefreshHere() {
		t.Fatal("找得到窗口、光标不在它上面：应按已处理返回 true")
	}
	if pumpFor(150*time.Millisecond, func() bool { m := moveMessages.Load(); return m != nil && len(*m) > 0 }) {
		t.Fatalf("光标不在窗口上，不该收到 WM_MOUSEMOVE：%+v", *moveMessages.Load())
	}
}

// 系统实现找不到 Viva 窗口时，与滚轮一样记一条日志、只记一次，不悄悄失效；内容带台账编号，方便回查。
func TestRootLogsOnceWhenTheHereFinderFindsNoWindow(t *testing.T) {
	log := &servicestest.Logger{}
	f := openRootSize(t, 800, 600, func(o *Options) { o.HoverRefreshHere, o.Services.Log = nil, log })
	look := f.root.Look()
	look.RequestHoverRefresh()
	look.RequestHoverRefresh()
	entries := log.Entries()
	if len(entries) != 1 || entries[0].Scope != "native" || entries[0].Action != "hover-refresh" {
		t.Fatalf("没有窗口可发时应记且只记一条 native/hover-refresh 日志，实际 %+v", entries)
	}
	if !strings.Contains(entries[0].Message, "B-037") {
		t.Fatalf("日志应带台账编号 B-037，实际 %q", entries[0].Message)
	}
}
