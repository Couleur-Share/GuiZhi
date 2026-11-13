package main

// 生命周期的 mygo 适配：窗口/托盘/开机自启的真实实现。逻辑在 app_lifecycle.go，这里只做转接。

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// 托盘图标：由 apps/desktop/resources/icon.png 缩到 32×32（Electron 版用 icon.ico 缩到 16×16）。
//
//go:embed assets/tray-icon.png
var trayIconPNG []byte

// mygoHost 把 *mygo.Window 适配成 lifecycleWindow。
type mygoHost struct{ win *mygo.Window }

func (h mygoHost) Show()             { h.win.Show() }
func (h mygoHost) Hide()             { h.win.Hide() }
func (h mygoHost) Focus()            { h.win.Focus() }
func (h mygoHost) Close()            { h.win.Close() }
func (h mygoHost) Restore()          { h.win.Restore() }
func (h mygoHost) IsVisible() bool   { return h.win.IsVisible() }
func (h mygoHost) IsMinimized() bool { return h.win.IsMinimized() }
func (h mygoHost) IsLoading() bool   { return h.win.Page().IsLoading() }
func (h mygoHost) OnceLoaded(fn func()) {
	var once sync.Once
	var off func()
	off = h.win.Page().OnDidFinishLoad(func() {
		once.Do(func() {
			if off != nil {
				off()
			}
			fn()
		})
	})
}

// mygoTray 是托盘控制器（tray-controller.ts）：左键切换窗口，右键弹出菜单；菜单文案随界面语言。
type mygoTray struct {
	a      *appLifecycle
	lib    *Library
	locale func() string
	mu     sync.Mutex
	tray   *mygo.Tray
}

func (t *mygoTray) labels() trayMenuLabels {
	stored := ""
	if t.lib != nil && t.lib.db != nil {
		stored = t.lib.readLanguageSetting(context.Background())
	}
	return getTrayMenuLabels(trayLocale(stored, t.locale()))
}

func (t *mygoTray) menu() *mygo.Menu {
	win, _ := t.a.snapshotWin()
	visible := win != nil && win.IsVisible()
	var items []*mygo.MenuItem
	for _, it := range buildTrayMenuItems(visible, t.labels()) {
		if it.Separator {
			items = append(items, mygo.Separator())
			continue
		}
		action := it.Action
		items = append(items, &mygo.MenuItem{Label: it.Label, Click: func(*mygo.MenuItem, *mygo.Window) { t.run(action) }})
	}
	return mygo.NewMenu(items)
}

func (t *mygoTray) run(action string) {
	switch action {
	case "toggle":
		t.a.toggleWindow()
	case "quit":
		t.a.trayQuit()
	case "cmd:item:new":
		t.a.dispatchAppCommand(map[string]any{"type": "item:new"})
	case "cmd:settings:open":
		t.a.dispatchAppCommand(map[string]any{"type": "settings:open"})
	case "cmd:updater:open":
		t.a.dispatchAppCommand(map[string]any{"type": "updater:open"})
	}
}

func (t *mygoTray) Create() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tray != nil {
		return
	}
	tr, err := mygo.NewTray(mygo.TrayOptions{ID: "guizhi", Icon: trayIconPNG, ToolTip: "归知 GuiZhi"})
	if err != nil {
		logAppErrorf("tray", "创建托盘", "%s", err.Error())
		return
	}
	tr.OnClick(func() { t.a.toggleWindow() })
	// 有菜单时左键会直接弹菜单；Electron 版是左键切换、右键菜单，所以只在右键时临时挂上菜单。
	tr.OnRightClick(func() {
		tr.SetMenu(t.menu())
		tr.PopUpMenu()
		tr.SetMenu(nil)
	})
	t.tray = tr
}

func (t *mygoTray) Destroy() {
	t.mu.Lock()
	tr := t.tray
	t.tray = nil
	t.mu.Unlock()
	if tr != nil {
		tr.Destroy()
	}
}

// Refresh：菜单每次右键现建，文案和「显示/隐藏」始终是最新的，无需主动刷新。
func (t *mygoTray) Refresh() {}

// setLoginItemNative 用 mygo 的登录项（Windows：HKCU Run）。mygo 不能给启动命令加自定义参数，
// 所以 TS 里的 `--hidden` 改为「系统登录项拉起」+「启动时最小化」设置在启动时判定（见 launchOpenedAtLogin）。
func setLoginItemNative(open bool) error {
	if err := mygo.App.SetOpenAtLogin(open); err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}

// windowStatePersister 对应 attachWindowStatePersistence：移动/缩放期间合并为 250ms 一次写盘，关闭前同步落盘。
// Electron 能直接读「还原后的普通边界」；mygo 没有这个接口，所以只在窗口既不最大化、不最小化、不全屏时记下边界，
// 最大化后沿用最近一次的普通边界。
type windowStatePersister struct {
	win  *mygo.Window
	file string

	mu         sync.Mutex
	timer      *time.Timer
	lastNormal windowBounds
	haveNormal bool
}

const windowStateSaveDebounce = 250 * time.Millisecond

func (p *windowStatePersister) schedule() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(windowStateSaveDebounce, p.save)
}

func (p *windowStatePersister) flush() {
	p.mu.Lock()
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.mu.Unlock()
	p.save()
}

func (p *windowStatePersister) save() {
	if p.win.IsDestroyed() || p.win.IsFullScreen() {
		return
	}
	maximized := p.win.IsMaximized()
	p.mu.Lock()
	if !maximized && !p.win.IsMinimized() {
		r := p.win.Bounds()
		p.lastNormal = windowBounds{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}
		p.haveNormal = true
	}
	normal, ok := p.lastNormal, p.haveNormal
	p.mu.Unlock()
	if !ok {
		return
	}
	if st := captureWindowState(normal, maximized, false, nil); st != nil {
		if err := writeWindowState(p.file, *st); err != nil {
			log.Printf("[window-state] 写入窗口状态失败：%v", err)
		}
	}
}

func (p *windowStatePersister) attach() {
	p.win.OnResize(p.schedule)
	p.win.OnMove(p.schedule)
	p.win.OnMaximize(p.schedule)
	p.win.OnUnmaximize(p.schedule)
}

// displayWorkAreas 对应 screen.getAllDisplays().map(d => d.workArea)。
func displayWorkAreas() []windowBounds {
	var out []windowBounds
	for _, d := range mygo.Screen.Displays() {
		out = append(out, windowBounds{X: d.WorkArea.X, Y: d.WorkArea.Y, Width: d.WorkArea.Width, Height: d.WorkArea.Height})
	}
	return out
}
