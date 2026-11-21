package library

import (
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
	"guizhi-mygo-shell/viva/tokens"
)

// 图片查看器（渲染层 components/library/ImageLightbox：yet-another-react-lightbox 的整窗浮层，滚轮缩放、拖拽平移、键盘、计数）。
//
// 渲染层里它盖住整个窗口，而阅读岛只占阅读区那一块，所以查看器是另一个盖满窗口的岛（reader.Viewer），页面渲染的就是渲染层自己的
// ImageLightbox。这里是界面一侧：阅读页点图片之后（readerEvents.Lightbox）要求显示，每帧在整窗浮层里把岛摆上去；
// 岛加载期间（冷启动约 0.7 s）画一层带淡入的半透明遮罩顶着，再叠一枚延迟登场的转圈，岛握手之后原生视图盖住它们；
// 加载期间点遮罩或按 Esc 就收起（渲染层 closeOnBackdropClick 与 Esc），岛就绪之后键盘与点击都归页面。
//
// 没有接宿主（Screen.readerHost 为空：对照与单测）时没有查看器。

// viewerFade 是遮罩的淡入时长：yet-another-react-lightbox 的 portal 淡入 0.25 s、ease。
const viewerFade = 250 * time.Millisecond

// viewerBackdrop 是 ImageLightbox 容器的底色：rgba(0, 0, 0, .88)（渲染层 ImageLightbox 的 styles.container）。
var viewerBackdrop = tokens.RGBA(0, 0, 0, 0.88)

// viewerEvents 是查看岛的结局的接收者。
type viewerEvents struct{ s *Screen }

// Closed 是用户在页面里关闭了查看器：岛已销毁，键盘焦点交还给阅读页（岛销毁时焦点落回窗口本身，方向键与空格就没人收了）。
func (e viewerEvents) Closed() { e.s.readerHost.Focus() }

// Failed 是查看岛打不开或页面不可用：弹带原因的失败提示（宿主已写诊断日志）。
func (e viewerEvents) Failed(err error) { e.s.Guard.Report(locales.NativeIslandViewerAction(), err) }

// showViewer 打开图片查看器（阅读页点了图片）。
func (s *Screen) showViewer(images []reader.Image, index int) {
	if s.readerHost == nil {
		return
	}
	s.readerHost.Viewer().Show(images, index, readerTheme(s.Look), readerLang())
}

// viewerLayer 声明查看器的整窗浮层：每帧都要调（没有要显示的就只复位转圈的计时）。
// 浮层在 BandViewer 层上：页面里画的东西都在它之下；岛就绪后原生视图（盖满窗口）把这一层及以下都盖住。
func (s *Screen) viewerLayer(c ui.C) {
	host := s.readerHost
	if host == nil {
		return
	}
	l := s.Look
	v := host.Viewer()
	if !v.Active() {
		s.detail.viewerWait.Opacity(l, c.Canvas(), false)
		return
	}
	k := c.Key("viewer")
	id := ui.ID(k)
	layer := ui.Overlay(k, ui.OverlaySpec{Band: kit.BandViewer, Bounds: c.Bounds()})
	// 背景的命中区与 Esc 目标登记在 Draw 钩子里（不用盒子自带的命中：它在钩子之前登记，进不了下面的焦点作用域）。
	cover := func(d *ui.DrawContext) {
		d.Canvas.HitWithOptions(id, d.Rect, "", gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true, Backdrop: true})
		d.Canvas.SetEscapeTarget(id)
	}
	// 原生视图要在这一层上声明：放在所有子元素之后，遮罩与转圈先画，岛握手之后再盖住它们。
	place := func(d *ui.DrawContext) { v.Frame(d.Canvas, d.Rect) }
	if s.detail.modal.open {
		// 详情在表格视图的浮层里：浮层的焦点陷阱把指针与 Esc 限制在它的作用域里，查看器要进到作用域里才点得到、按得到。
		layer = l.WithinModalHooks(&s.detail.modal.state, layer, cover, place)
	} else {
		layer = layer.Draw(cover).DrawOver(place)
	}
	fade := gfx.Motion{Easing: kit.EaseDefault}
	if l.Spec.Motion.Animated() {
		fade.Duration = viewerFade
	}
	layer.Children(func() {
		scrim := ui.Row(k.Key("scrim")).Absolute().Inset(0).Justify(ui.Center).Items(ui.Center)
		l.EnterSlide(scrim, ui.ID(k.Key("scrim-fade")), 0, fade, func(d *ui.DrawContext) {
			d.Canvas.Fill(d.Rect, l.Col(viewerBackdrop))
		}).Children(func() {
			alpha := s.detail.viewerWait.Opacity(l, c.Canvas(), !v.Ready())
			ui.Row(k.Key("wait")).Opacity(alpha).Children(func() {
				l.Spinner(k.Key("spinner"), l.Rem(3), tokens.White.WithAlpha(0.8))
			})
		})
	})
	if c.Canvas().Clicked(id) {
		// 点遮罩或 Esc：岛没就绪之前的收起（就绪之后点击与按键都落在岛上，到不了这里）。
		v.Hide()
		host.Focus()
	}
}