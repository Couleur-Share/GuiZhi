package reader

import (
	"errors"
	"sync"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/island"
)

// IslandOpener 是 Opener 的真实实现：经 island 包在 Viva 的 WebHost 上开页面，并登记到 Group
// （窗口折叠到托盘前由 Group.CloseAll 统一销毁，托盘态的空闲内存才不含任何 WebView）。
type IslandOpener struct {
	// Web 是窗口配置里的 WebHost（app.Config.Web）。为空时 Open 报错，阅读区按岛打不开处理。
	Web *app.WebHost
	// Group 可以为空（测试或不需要统一销毁时）。
	Group *island.Group
}

// Open 实现 Opener。
func (o IslandOpener) Open(cfg island.Config, closed func()) (Page, error) {
	if o.Web == nil {
		return nil, errors.New("reader: 窗口没有配置 WebHost")
	}
	isl, err := island.Open(o.Web, cfg)
	if err != nil {
		return nil, err
	}
	page := &islandPage{isl: isl, group: o.Group, closed: closed}
	if o.Group != nil {
		o.Group.Track(page)
	}
	return page, nil
}

// islandPage 把一个 island.Island 适配成 Page。
type islandPage struct {
	isl    *island.Island
	group  *island.Group
	closed func()
	once   sync.Once
}

func (p *islandPage) Post(text string) { p.isl.View().PostMessage(text) }

func (p *islandPage) Place(surface Surface, bounds gfx.Rect) {
	surface.NativeView(p.isl.View().ID(), bounds)
}

// Close 销毁页面并通知宿主（宿主自己关页面时它已经先把页面置空，通知是空操作）。
func (p *islandPage) Close() {
	p.once.Do(func() {
		if p.group != nil {
			p.group.Forget(p)
		}
		p.isl.Close()
		if p.closed != nil {
			p.closed()
		}
	})
}
