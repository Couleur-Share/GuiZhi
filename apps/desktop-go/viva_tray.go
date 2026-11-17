package main

// Viva 路径的托盘：把 appLifecycle 的托盘接口（trayPort）接到 Viva 的状态栏图标（AddStatusItem）上。
// 行为与 mygoTray 一致（tray-controller.ts）：左键切换窗口显隐，右键弹出菜单，菜单文案随界面语言。
//
// AddStatusItem 只能在事件循环线程上调用，所以创建与销毁都经 RunOnUI 排到界面线程、按调用顺序执行：
// 先建后销（或反过来）的快速切换不会因线程不同而乱序。进程退出时事件循环已经停了，用 destroyNow 直接摘掉图标。

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"log"
	"sync"

	vapp "github.com/vivaui/viva/app"
)

// trayTooltip 是托盘图标的悬停提示（与 mygoTray 相同）。
const trayTooltip = "归知 GuiZhi"

// vivaTray 实现 trayPort。
type vivaTray struct {
	a      *appLifecycle
	host   vivaHost
	labels func() trayMenuLabels           // 当前界面语言的菜单文案
	icon   func() (vapp.StatusIcon, error) // 图标；测试注入，缺省 trayStatusIcon

	mu   sync.Mutex
	item statusRemover
}

// Create 在界面线程上建好托盘图标（已有则不重复建）。任意 goroutine。
func (t *vivaTray) Create() { t.host.RunOnUI(t.create) }

// Destroy 在界面线程上摘掉托盘图标。任意 goroutine。
func (t *vivaTray) Destroy() { t.host.RunOnUI(t.destroyNow) }

// Refresh：菜单每次打开时现建，文案和「显示/隐藏」始终是最新的，无需主动刷新。
func (t *vivaTray) Refresh() {}

func (t *vivaTray) create() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.item != nil {
		return
	}
	iconFn := t.icon
	if iconFn == nil {
		iconFn = trayStatusIcon
	}
	icon, err := iconFn()
	if err != nil {
		logAppErrorf("tray", "创建托盘", "%s", err.Error())
		return
	}
	item, err := t.host.AddStatusItem(vapp.StatusItemConfig{
		Tooltip: trayTooltip,
		Icon:    icon,
		// 有菜单时 Windows 的主键按下仍交给 OnClick，次键才弹菜单：左键切换、右键菜单，与 Electron 版一致。
		OnClick: func(vapp.StatusClick) { t.a.toggleWindow() },
		Menu:    t.menu,
	})
	if err != nil {
		logAppErrorf("tray", "创建托盘", "%s", err.Error())
		return
	}
	t.item = item
	log.Printf("[viva-shell] 托盘图标已创建")
}

// destroyNow 摘掉托盘图标（幂等；Remove 可在任意 goroutine 调用）。
func (t *vivaTray) destroyNow() {
	t.mu.Lock()
	item := t.item
	t.item = nil
	t.mu.Unlock()
	if item != nil {
		item.Remove()
		log.Printf("[viva-shell] 托盘图标已移除")
	}
}

// exists 报告托盘图标当前是否在系统托盘区。
func (t *vivaTray) exists() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.item != nil
}

// menu 是托盘菜单（每次打开时调用，事件循环线程）：显示/隐藏的文案取决于窗口此刻是否可见。
func (t *vivaTray) menu() []vapp.MenuItem {
	win, _ := t.a.snapshotWin()
	visible := win != nil && win.IsVisible()
	var items []vapp.MenuItem
	for _, it := range buildTrayMenuItems(visible, t.labels()) {
		if it.Separator {
			items = append(items, vapp.MenuItem{Separator: true})
			continue
		}
		action := it.Action
		items = append(items, vapp.MenuItem{Title: it.Label, ID: action, OnSelect: func() { t.a.runTrayAction(action) }})
	}
	return items
}

// trayStatusIcon 把内嵌的托盘 PNG（assets/tray-icon.png，32×32）解成 Viva 要的非预乘 RGBA。
func trayStatusIcon() (vapp.StatusIcon, error) {
	return statusIconFromPNG(trayIconPNG)
}

func statusIconFromPNG(data []byte) (vapp.StatusIcon, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return vapp.StatusIcon{}, fmt.Errorf("解码托盘图标失败：%w", err)
	}
	b := img.Bounds()
	if b.Dx() != b.Dy() || b.Dx() == 0 {
		return vapp.StatusIcon{}, errors.New("托盘图标必须是正方形")
	}
	size := b.Dx()
	rgba := make([]byte, 0, size*size*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			rgba = append(rgba, c.R, c.G, c.B, c.A)
		}
	}
	return vapp.StatusIcon{Size: size, RGBA: rgba}, nil
}
