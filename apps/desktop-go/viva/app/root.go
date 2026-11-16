package app

import (
	"sync"
	"sync/atomic"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// Root 是主窗口的根：把服务、外观与各页面装配起来，交给 Viva 的页面声明。
//
// 它不碰窗口 API（窗口由 Run 打开），所以能用无头会话测试。外观每帧按设置求值，设置变了整套令牌随之重装配；
// 页面主题与背景色以指针交给 ui.Page，原地更新即可，不必重建页面。
type Root struct {
	opts Options

	store    *settingsStore
	win      *shell.Window
	settings *settingsscreen.Shell

	look     *kit.Look
	lookSpec kit.Spec
	theme    gfx.Theme // ui.Page 读它的当前值
	bg       gfx.Color

	systemDark atomic.Bool // 系统当前是否深色（由系统外观监听更新）
	ready      sync.Once
}

// NewRoot 装配根。invalidate 在设置被别处改动后调用，请求重绘（可为空）。
func NewRoot(opts Options, invalidate func()) *Root {
	r := &Root{opts: opts}
	r.store = newSettingsStore(opts.Services.Settings, invalidate)
	r.settings = &settingsscreen.Shell{Store: r.store}
	r.win = &shell.Window{
		Settings:       r.settings,
		SearchShortcut: opts.SearchShortcut,
		OnModule: func(m shell.Module) {
			r.win.Module, r.win.Page = m, shell.PageHome
		},
		OnSettings: func() { r.win.Page = shell.PageSettings },
	}
	if opts.Module != "" {
		r.win.Module = opts.Module
	}
	r.win.Page = opts.Page
	r.systemDark.Store(opts.SystemDark)
	r.syncLook(nil)
	return r
}

// currentSpec 是此刻应当使用的外观：设置里的外观，自动化实例再叠加命令行覆盖。
func (r *Root) currentSpec() kit.Spec {
	spec := SpecFromSettings(r.store.Values().UI, r.systemDark.Load(), r.osReduceMotion())
	if r.opts.Appearance != nil {
		spec = r.opts.Appearance(spec)
	}
	return spec
}

// osReduceMotion 是系统是否要求减少动态效果。
func (r *Root) osReduceMotion() bool {
	if r.opts.OSReduceMotion != nil {
		return r.opts.OSReduceMotion()
	}
	return vapp.ReduceMotion()
}

// syncLook 在外观输入变化时重新装配 Look，并让页面读到新主题；c 非空时顺带取窗口缩放。
func (r *Root) syncLook(c ui.C) {
	if spec := r.currentSpec(); r.look == nil || spec != r.lookSpec {
		scale := float32(0)
		if r.look != nil {
			scale = r.look.Scale
		}
		r.look, r.lookSpec = kit.NewLook(spec), spec
		r.look.Scale = scale
		r.theme = r.look.Theme
		r.bg = r.look.Col(r.look.T.Background)
		r.win.Look, r.settings.Look = r.look, r.look
	}
	if c != nil {
		canvas := c.Canvas()
		r.look.Scale = float32(canvas.PixelScale())
		// 动效关闭档：让 Viva 的过渡与入退场直接到终态（它自己的「减少动态效果」开关就是这个语义）；
		// 其余档位播放动画，时长由 kit 按档位给出。
		canvas.SetReduceMotion(r.lookSpec.Motion == kit.MotionLevelOff)
	}
}

// Look 返回当前外观（窗口线程）。
func (r *Root) Look() *kit.Look { return r.look }

// ThemeRef 与 BackgroundRef 是交给 ui.PageConfig 的指针：Root 原地更新它们指向的值。
func (r *Root) ThemeRef() *gfx.Theme        { return &r.theme }
func (r *Root) BackgroundRef() *gfx.Color   { return &r.bg }
func (r *Root) Services() services.Services { return r.opts.Services }

// View 声明整个窗口内容。
func (r *Root) View(c ui.C) {
	r.syncLook(c)
	r.win.View(c)
	if r.opts.OnReady != nil {
		r.ready.Do(r.opts.OnReady)
	}
}

// Window 返回窗口页面（顶栏与外壳的状态在这里；测试与装配用）。
func (r *Root) Window() *shell.Window { return r.win }

// WatchSystemAppearance 在系统外观变化时更新缓存并请求重绘；返回停止函数。
func (r *Root) WatchSystemAppearance(invalidate func()) (stop func()) {
	r.systemDark.Store(vapp.SystemAppearance() == vapp.AppearanceDark)
	return vapp.WatchSystemAppearance(func() {
		r.systemDark.Store(vapp.SystemAppearance() == vapp.AppearanceDark)
		if invalidate != nil {
			invalidate()
		}
	})
}

// Close 释放订阅。
func (r *Root) Close() { r.store.Close() }
