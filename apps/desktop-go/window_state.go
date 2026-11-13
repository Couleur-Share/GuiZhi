package main

// 窗口状态、启动可见性与托盘菜单文案的纯逻辑，一一对应 window-state.ts 与 tray-menu.ts。
// 状态文件格式与 Electron 版相同（userData/window-state.json），新旧壳层可互相读取。

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultWindowWidth  = 1200
	defaultWindowHeight = 800
	minWindowWidth      = 800
	minWindowHeight     = 600
	windowStateVersion  = 1
)

type windowBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type persistedWindowState struct {
	Version     int          `json:"version"`
	Bounds      windowBounds `json:"bounds"`
	IsMaximized bool         `json:"isMaximized"`
}

type windowLaunchState struct {
	Bounds         windowBounds `json:"bounds"`
	ShouldMaximize bool         `json:"shouldMaximize"`
	// HasPosition 为假时只有宽高有效（默认窗口，由系统居中），对应 TS 里 bounds 缺 x/y。
	HasPosition bool `json:"-"`
}

// jsRound 对应 Math.round：四舍五入，.5 向正无穷。
func jsRound(f float64) int { return int(math.Floor(f + 0.5)) }

func finiteNum(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok && !math.IsInf(f, 0) && !math.IsNaN(f)
}

// parsePersistedWindowState 对应同名函数：任何一项不合格都整体作废。
func parsePersistedWindowState(value any) *persistedWindowState {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	ver, vok := obj["version"].(float64)
	maxd, mok := obj["isMaximized"].(bool)
	bounds, bok := obj["bounds"].(map[string]any)
	if !vok || ver != windowStateVersion || !mok || !bok {
		return nil
	}
	x, xok := finiteNum(bounds["x"])
	y, yok := finiteNum(bounds["y"])
	w, wok := finiteNum(bounds["width"])
	h, hok := finiteNum(bounds["height"])
	if !xok || !yok || !wok || !hok || w < minWindowWidth || h < minWindowHeight {
		return nil
	}
	return &persistedWindowState{
		Version:     windowStateVersion,
		Bounds:      windowBounds{X: jsRound(x), Y: jsRound(y), Width: jsRound(w), Height: jsRound(h)},
		IsMaximized: maxd,
	}
}

func overlapArea(a, b windowBounds) int64 {
	w := min(a.X+a.Width, b.X+b.Width) - max(a.X, b.X)
	h := min(a.Y+a.Height, b.Y+b.Height) - max(a.Y, b.Y)
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return int64(w) * int64(h)
}

// fitBoundsToDisplays 把旧窗口放回与它重叠最多的屏幕；没有重叠返回 nil。并列时取先出现的屏幕（JS 的稳定排序）。
func fitBoundsToDisplays(b windowBounds, work []windowBounds) *windowBounds {
	best := -1
	var bestOverlap int64
	for i, wa := range work {
		if o := overlapArea(b, wa); best < 0 || o > bestOverlap {
			best, bestOverlap = i, o
		}
	}
	if best < 0 || bestOverlap == 0 {
		return nil
	}
	wa := work[best]
	width := min(max(b.Width, minWindowWidth), wa.Width)
	height := min(max(b.Height, minWindowHeight), wa.Height)
	maxX := wa.X + wa.Width - width
	maxY := wa.Y + wa.Height - height
	return &windowBounds{
		X:      min(max(b.X, wa.X), maxX),
		Y:      min(max(b.Y, wa.Y), maxY),
		Width:  width,
		Height: height,
	}
}

func resolveWindowLaunchState(persisted any, work []windowBounds) windowLaunchState {
	state := parsePersistedWindowState(persisted)
	var fitted *windowBounds
	if state != nil {
		fitted = fitBoundsToDisplays(state.Bounds, work)
	}
	if state == nil || fitted == nil {
		return windowLaunchState{Bounds: windowBounds{Width: defaultWindowWidth, Height: defaultWindowHeight}, ShouldMaximize: true}
	}
	return windowLaunchState{Bounds: *fitted, ShouldMaximize: state.IsMaximized, HasPosition: true}
}

type launchVisibility struct {
	StartHidden         bool `json:"startHidden"`
	MaximizeNow         bool `json:"maximizeNow"`
	MaximizeOnFirstShow bool `json:"maximizeOnFirstShow"`
}

// resolveLaunchVisibility：只有系统登录项拉起（--hidden，或 macOS 登录项且开了「启动时最小化」）才驻留托盘。
func resolveLaunchVisibility(argv []string, openedAtLogin, minimizeOnLaunch, shouldMaximize bool) launchVisibility {
	hidden := openedAtLogin && minimizeOnLaunch
	for _, a := range argv {
		if a == "--hidden" {
			hidden = true
		}
	}
	return launchVisibility{StartHidden: hidden, MaximizeNow: shouldMaximize && !hidden, MaximizeOnFirstShow: shouldMaximize && hidden}
}

func readWindowLaunchState(stateFile string, work []windowBounds) windowLaunchState {
	var persisted any
	if raw, err := os.ReadFile(stateFile); err == nil {
		if json.Unmarshal(raw, &persisted) != nil {
			persisted = nil
		}
	}
	return resolveWindowLaunchState(persisted, work)
}

// captureWindowState：全屏不记录；其余按「还原后的普通边界 + 是否最大化」生成并过一遍校验（过小的窗口不记录）。
func captureWindowState(normal windowBounds, maximized, fullScreen bool, rawBounds *[4]float64) *persistedWindowState {
	if fullScreen {
		return nil
	}
	b := map[string]any{"x": float64(normal.X), "y": float64(normal.Y), "width": float64(normal.Width), "height": float64(normal.Height)}
	if rawBounds != nil {
		b = map[string]any{"x": rawBounds[0], "y": rawBounds[1], "width": rawBounds[2], "height": rawBounds[3]}
	}
	return parsePersistedWindowState(map[string]any{"version": float64(windowStateVersion), "bounds": b, "isMaximized": maximized})
}

func writeWindowState(stateFile string, s persistedWindowState) error {
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile, append(raw, '\n'), 0o644)
}

// ── 托盘菜单文案（tray-menu.ts）──

type trayMenuLabels struct {
	NewItem      string `json:"newItem"`
	ShowApp      string `json:"showApp"`
	HideApp      string `json:"hideApp"`
	CheckUpdates string `json:"checkUpdates"`
	Settings     string `json:"settings"`
	QuitApp      string `json:"quitApp"`
}

var trayLabels = map[string]trayMenuLabels{
	"en": {NewItem: "New Knowledge Item…", ShowApp: "Show GuiZhi", HideApp: "Hide GuiZhi", CheckUpdates: "Check for Updates…", Settings: "Settings…", QuitApp: "Quit GuiZhi"},
	"zh": {NewItem: "新建知识条目…", ShowApp: "显示归知", HideApp: "隐藏归知", CheckUpdates: "检查更新…", Settings: "设置…", QuitApp: "退出归知"},
}

func normalizeTrayMenuLanguage(locale string) string {
	if strings.HasPrefix(strings.ToLower(jsTrim(locale)), "zh") {
		return "zh"
	}
	return "en"
}

func getTrayMenuLabels(locale string) trayMenuLabels {
	return trayLabels[normalizeTrayMenuLanguage(locale)]
}

// trayMenuItem 是托盘菜单的中立描述（不依赖 mygo，便于对拍与测试）。
type trayMenuItem struct {
	Label     string
	Separator bool
	// Command 为空表示 toggle / quit 之外没有命令；Action 取 "cmd:item:new" 之类，与 TS 对拍记录一致。
	Action string
}

func buildTrayMenuItems(isWindowVisible bool, l trayMenuLabels) []trayMenuItem {
	toggle := l.ShowApp
	if isWindowVisible {
		toggle = l.HideApp
	}
	return []trayMenuItem{
		{Label: l.NewItem, Action: "cmd:item:new"},
		{Separator: true},
		{Label: toggle, Action: "toggle"},
		{Label: l.CheckUpdates, Action: "cmd:updater:open"},
		{Label: l.Settings, Action: "cmd:settings:open"},
		{Separator: true},
		{Label: l.QuitApp, Action: "quit"},
	}
}
