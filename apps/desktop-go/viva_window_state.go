package main

// Viva 窗口的位置与大小记忆（台账 B-027）：userData/window-state.json，格式与 Electron 版、React 版互通
// （读写与校验沿用 window_state.go 的 readWindowLaunchState / captureWindowState / writeWindowState）。
//
// 坐标系：全部是逻辑像素，与 Viva 的 Config.Origin、OnFrameChange 一致（Windows 上 = 物理像素 / 窗口所在显示器的缩放）。
// Viva v0.6.4 没有程序化最大化窗口的接口，所以 launch.ShouldMaximize 不会被恢复成最大化窗口（台账 B-027 / A-005 既定设计）；
// 但用户把窗口最大化时照实写入 isMaximized，并保留最近一次的普通边界，别的壳读它仍然正确。

import (
	"log"
	"sync"
	"time"

	"github.com/vivaui/viva/gfx"

	vivaapp "guizhi-mygo-shell/viva/app"
)

// monitorWorkArea 是一台显示器的工作区（逻辑像素）及是否主显示器。
type monitorWorkArea struct {
	area    windowBounds
	primary bool
}

// logicalWorkArea 把物理像素的工作区矩形按显示器的有效 DPI 换算成逻辑像素（dpi 为 0 按 96 处理）。
func logicalWorkArea(left, top, right, bottom int32, dpi uint32) windowBounds {
	if dpi == 0 {
		dpi = 96
	}
	scale := float64(dpi) / 96.0
	return windowBounds{
		X:      jsRound(float64(left) / scale),
		Y:      jsRound(float64(top) / scale),
		Width:  jsRound(float64(right-left) / scale),
		Height: jsRound(float64(bottom-top) / scale),
	}
}

// primaryFirst 把主显示器排到首位（其余保持枚举顺序）并取出工作区：fitBoundsToDisplays 在并列时取先出现的屏幕。
func primaryFirst(areas []monitorWorkArea) []windowBounds {
	out := make([]windowBounds, 0, len(areas))
	for _, a := range areas {
		if a.primary {
			out = append(out, a.area)
		}
	}
	for _, a := range areas {
		if !a.primary {
			out = append(out, a.area)
		}
	}
	return out
}

// vivaWindowGeometry 是窗口初次打开的几何：内容区大小，以及（有记忆位置时）内容区左上角的屏幕位置。
type vivaWindowGeometry struct {
	Width, Height int
	Origin        *gfx.Point
}

// planVivaWindow 决定初次打开的几何：命令行显式给了 -viva-width / -viva-height 时按命令行（对照、截图要确定的大小，不带位置）；
// 否则用记忆的窗口状态——大小取 launch.Bounds，记忆里有位置（HasPosition，已按当前显示器校正）时同时给出 Origin。
func planVivaWindow(launch windowLaunchState, sizeGiven bool, width, height int) vivaWindowGeometry {
	if sizeGiven {
		return vivaWindowGeometry{Width: width, Height: height}
	}
	g := vivaWindowGeometry{Width: launch.Bounds.Width, Height: launch.Bounds.Height}
	if launch.HasPosition {
		g.Origin = &gfx.Point{X: float32(launch.Bounds.X), Y: float32(launch.Bounds.Y)}
	}
	return g
}

// vivaWindowStatePersister 记录 Viva 窗口的普通边界与最大化状态，防抖写盘，关闭前同步落盘。
type vivaWindowStatePersister struct {
	file     string
	debounce time.Duration

	mu         sync.Mutex
	timer      *time.Timer
	lastNormal windowBounds
	haveNormal bool
	maximized  bool
	fullscreen bool

	saveMu sync.Mutex // 串行化写盘：防抖定时器与 flush 不会同时写文件
}

// newVivaWindowStatePersister 建记录器。initial 非空时用它预填「最近一次普通边界」（启动时按记忆打开的窗口），
// 这样启动后没动过窗口就直接最大化，也能落盘有效的普通边界。
func newVivaWindowStatePersister(file string, initial *windowBounds) *vivaWindowStatePersister {
	p := &vivaWindowStatePersister{file: file, debounce: windowStateSaveDebounce}
	if initial != nil {
		p.lastNormal, p.haveNormal = *initial, true
	}
	return p
}

// bind 把记录器的三个回调接到 Viva 窗口选项上（窗口线程调用）。
func (p *vivaWindowStatePersister) bind(o *vivaapp.Options) {
	o.OnFrameChange, o.OnMaximize, o.OnFullscreen = p.onFrame, p.onMaximize, p.onFullscreen
}

// onFrame 记下内容区的新边界：Viva 在最大化、最小化、全屏期间不报，收到的就是普通窗口；过小的边界（不会被记忆接受）忽略。
func (p *vivaWindowStatePersister) onFrame(r gfx.Rect) {
	b := windowBounds{X: jsRound(float64(r.X)), Y: jsRound(float64(r.Y)), Width: jsRound(float64(r.Width)), Height: jsRound(float64(r.Height))}
	if b.Width < minWindowWidth || b.Height < minWindowHeight {
		return
	}
	p.mu.Lock()
	p.lastNormal, p.haveNormal = b, true
	p.mu.Unlock()
	p.schedule()
}

func (p *vivaWindowStatePersister) onMaximize(maximized bool) {
	p.mu.Lock()
	p.maximized = maximized
	p.mu.Unlock()
	p.schedule()
}

// onFullscreen 记下全屏状态：全屏期间不写盘（captureWindowState 对全屏返回空），退出全屏后恢复。
func (p *vivaWindowStatePersister) onFullscreen(fullscreen bool) {
	p.mu.Lock()
	p.fullscreen = fullscreen
	p.mu.Unlock()
	if !fullscreen {
		p.schedule()
	}
}

// schedule 合并移动/缩放期间的连续事件：最后一次事件之后 250ms 写一次。
func (p *vivaWindowStatePersister) schedule() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(p.debounce, p.save)
}

// flush 取消待定的防抖并同步落盘。可对 nil 调用；可重复调用。
func (p *vivaWindowStatePersister) flush() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.mu.Unlock()
	p.save()
}

func (p *vivaWindowStatePersister) save() {
	p.mu.Lock()
	normal, ok, maximized, fullscreen := p.lastNormal, p.haveNormal, p.maximized, p.fullscreen
	p.mu.Unlock()
	if !ok {
		return
	}
	st := captureWindowState(normal, maximized, fullscreen, nil)
	if st == nil {
		return
	}
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	if err := writeWindowState(p.file, *st); err != nil {
		log.Printf("[window-state] 写入窗口状态失败：%v", err)
	}
}
