package reader

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/island"
)

// viewerScheme 是查看岛的自有 scheme：页面地址 gzviewer://doc/viewer.html。与阅读岛的 gzreader 区分开，排查时一眼看出是哪个岛。
const viewerScheme = "gzviewer"

// viewerFocusFrames 是岛第一次摆上之后、把键盘焦点交给页面之前要等的帧数：原生视图在摆放它的那一帧结束后才真正显示，
// 同一帧里要焦点，窗口还没有显示出来，焦点落不下去。
const viewerFocusFrames = 2

// ViewerHandler 接收查看岛的结局。窗口线程，且已校验。
type ViewerHandler interface {
	// Closed 是用户在页面里关闭了查看器（Esc、关闭钮、点背景）：宿主已销毁岛。
	Closed()
	// Failed 是查看岛打不开或页面不可用：宿主已销毁岛并结束这次显示。
	Failed(err error)
}

// Viewer 管理图片查看岛：阅读岛里点图片时，页面把图片列表交给宿主，宿主另开一个盖满窗口的岛，在里面渲染渲染层自己的
// ImageLightbox（滚轮缩放、拖拽平移、键盘与计数的行为与渲染层一致，不自写手势状态机）。渲染层里查看器是整窗浮层，岛只占阅读区那一块，
// 所以不能在阅读岛里画。
//
// 岛是按需开的：Show 之后由界面每帧在整窗浮层里调 Frame，它第一次被调到时才开岛；岛握手之前不摆放原生视图，
// 界面一侧的半透明遮罩（带淡入）顶着，用户立刻看到查看器出现；握手完成后摆放并交出焦点；用户关闭、失败或这一帧没人调 Frame 就销毁。
// 窗口线程独占，没有锁。
type Viewer struct {
	opts    Options
	handler ViewerHandler

	page     Page
	ready    bool
	deadline time.Time
	// placed 是岛握手后已被摆放的帧数；focused 表示焦点已交给页面。
	placed  int
	focused bool
	// gen 是页面的代数：每次打开或关闭加一，页面的回调带着打开时的代数，对不上就是旧页面迟到的东西。
	gen uint64

	// active 表示查看器要显示（含岛还在加载）；shown 表示这一帧有人调过 Frame。
	active bool
	shown  bool
	images []Image
	index  int
	theme  Theme
	lang   Lang
}

func newViewer(opts Options) *Viewer { return &Viewer{opts: opts} }

// SetHandler 设置结局的接收者。
func (v *Viewer) SetHandler(handler ViewerHandler) { v.handler = handler }

// Show 要求显示查看器：images 是要看的图（Src 是 local-image://<名称>），index 是起始序号（越界按 0）。
// 已经开着时换成新的图片。外观与语言取调用时的值（岛握手时发给页面）。
func (v *Viewer) Show(images []Image, index int, theme Theme, lang Lang) {
	if len(images) == 0 {
		v.diag(errors.New("reader: 查看器没有图片可显示"))
		return
	}
	if err := theme.Validate(); err != nil {
		v.fail(err)
		return
	}
	if index < 0 || index >= len(images) {
		index = 0
	}
	v.images = append(v.images[:0:0], images...)
	v.index, v.theme, v.lang = index, theme, lang
	// shown 先置真：Show 常在两帧之间（页面事件）被调，也可能在本帧的 EndFrame 之后，不给一帧的宽限就会被立刻收掉。
	v.active, v.shown = true, true
	if v.page != nil && v.ready {
		v.sendInit()
	}
}

// Hide 结束显示并销毁岛（界面一侧的 Esc 或点遮罩、窗口关闭、折叠到托盘前）。
func (v *Viewer) Hide() {
	v.active, v.shown = false, false
	v.closePage()
}

// Active 报告查看器是否要显示（岛还在加载时也是真：界面据此画遮罩）。
func (v *Viewer) Active() bool { return v.active }

// Open 报告岛此刻是否开着。
func (v *Viewer) Open() bool { return v.page != nil }

// Ready 报告页面是否已经完成握手（此后岛才被摆放）。
func (v *Viewer) Ready() bool { return v.page != nil && v.ready }

// Frame 在整窗浮层绘制时调用（只在 Active 时需要）：按需开岛；握手完成后把原生视图摆在 bounds 上，随后把焦点交给页面。
// 在岛握手之前什么也不摆，调用方此刻画的遮罩与转圈就是用户看到的东西。
func (v *Viewer) Frame(surface Surface, bounds gfx.Rect) {
	if !v.active {
		return
	}
	v.shown = true
	if v.page == nil && !v.open() {
		return
	}
	if !v.ready {
		if v.now().After(v.deadline) {
			v.fail(errors.New("reader: 查看页超时未完成握手"))
			return
		}
		surface.RequestFrameIn(readyPoll)
		return
	}
	v.page.Place(surface, bounds)
	v.placed++
	if v.focused {
		return
	}
	if v.placed >= viewerFocusFrames {
		v.focused = true
		v.page.Focus()
		return
	}
	surface.RequestFrameIn(time.Millisecond)
}

// endFrame 在每帧收尾调用（Host.EndFrame 转来）：查看器要显示、这一帧却没人调 Frame（切了模块、关了知识库……）就结束显示，
// 与渲染层里查看器随组件卸载一样。
func (v *Viewer) endFrame() {
	if v.active && !v.shown {
		v.Hide()
		return
	}
	v.shown = false
}

func (v *Viewer) diag(err error) {
	if v.opts.Diag != nil {
		v.opts.Diag(err)
	}
}

func (v *Viewer) now() time.Time {
	if v.opts.Now != nil {
		return v.opts.Now()
	}
	return time.Now()
}

func (v *Viewer) open() bool {
	if v.opts.Opener == nil {
		v.fail(errors.New("reader: 没有可用的 WebView 宿主"))
		return false
	}
	v.gen++
	gen := v.gen
	page, err := v.opts.Opener.Open(island.Config{
		Scheme: viewerScheme,
		Entry:  "viewer.html",
		Serve:  v.serve,
		CSP:    readerCSP,
		OnMessage: func(text string) {
			if gen == v.gen {
				v.onMessage(text)
			}
		},
		OnDenied: func(string, bool) {
			if gen == v.gen {
				v.diag(errors.New("reader: 查看页的一次导航被拒绝（地址不记入日志）"))
			}
		},
	}, v.pageClosed)
	if err != nil {
		v.fail(fmt.Errorf("reader: 打开查看岛失败：%w", err))
		return false
	}
	v.page, v.ready, v.placed, v.focused = page, false, 0, false
	timeout := v.opts.ReadyTimeout
	if timeout <= 0 {
		timeout = defaultReadyTimeout
	}
	v.deadline = v.now().Add(timeout)
	return true
}

// pageClosed 是岛被宿主以外的原因关闭（收托盘前 island.Group 统一销毁）：状态复位，要显示的仍然要显示，下一帧按需重开。
func (v *Viewer) pageClosed() {
	if v.page == nil {
		return
	}
	v.gen++
	v.page, v.ready, v.placed, v.focused = nil, false, 0, false
}

func (v *Viewer) closePage() {
	page := v.page
	if page == nil {
		return
	}
	v.gen++
	v.page, v.ready, v.placed, v.focused = nil, false, 0, false
	page.Close()
}

// fail 结束这次显示并通知 Handler：打不开的岛不重试（用户再点一次图片才会重新开始）。
func (v *Viewer) fail(err error) {
	v.Hide()
	v.diag(err)
	if v.handler != nil {
		v.handler.Failed(err)
	}
}

// serve 是 island.Config.Serve：页面文件、外观烘进 <html> 的入口页与本地媒体。路径的约定同 Host.serve（没有主机名、没有查询串）。
func (v *Viewer) serve(p string) (mime string, data []byte, ok bool) {
	p = strings.TrimPrefix(p, "/")
	switch {
	case p == "" || p == "viewer.html":
		raw, err := assets.ReadFile("assets/viewer/viewer.html")
		if err != nil {
			return "", nil, false
		}
		return "text/html; charset=utf-8", bakeHTML(raw, v.theme, v.lang), true
	case strings.HasPrefix(p, "media/"):
		return serveMedia(v.opts.Media, strings.TrimPrefix(p, "media/"))
	}
	return serveAssetIn("assets/viewer/", p)
}

// onMessage 处理查看页发来的一条文本：先校验；握手之前的 close 不认。
func (v *Viewer) onMessage(text string) {
	if v.page == nil {
		return
	}
	ev, err := ParseViewerEvent(text)
	if err != nil {
		v.diag(err)
		return
	}
	switch ev.Kind {
	case ViewerEventReady:
		if ev.Version != ProtocolVersion {
			v.fail(fmt.Errorf("reader: 查看页协议版本 %d 与宿主的 %d 不一致", ev.Version, ProtocolVersion))
			return
		}
		v.ready = true
		v.sendInit()
	case ViewerEventClose:
		if !v.ready {
			return
		}
		v.Hide()
		if v.handler != nil {
			v.handler.Closed()
		}
	case ViewerEventError:
		// 页面自己报的脚本错误：只记诊断，不把页面给的文字当界面文案。
		v.diag(fmt.Errorf("reader: 查看页报告脚本错误（说明 %d 字节，内容不记入日志）", len(ev.Message)))
	}
}

// sendInit 把图片、起始序号与外观发给页面（握手完成时与已开着换内容时）。
func (v *Viewer) sendInit() {
	images := make([]viewerImage, len(v.images))
	for i, image := range v.images {
		images[i] = viewerImage{Src: image.Src, Alt: image.Alt}
	}
	text, err := encodeMessage(viewerInitMessage{
		T: "init", V: ProtocolVersion, Theme: v.theme, Lang: normalizeLang(v.lang), Images: images, Index: v.index,
	})
	if err != nil {
		v.fail(fmt.Errorf("reader: 查看页消息编码失败：%w", err))
		return
	}
	v.page.Post(text)
}