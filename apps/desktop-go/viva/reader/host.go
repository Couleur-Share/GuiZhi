package reader

import (
	"errors"
	"fmt"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/island"
)

// Surface 是宿主在一帧里需要的画布能力：*gfx.Canvas 满足它，测试用假的（不必构造真画布）。
type Surface interface {
	// NativeView 在当前层把原生视图 id 放在 bounds 上（gfx.Canvas.NativeView）。
	NativeView(id gfx.NativeViewID, bounds gfx.Rect)
	// RequestFrameIn 要求至少 delay 之后再出一帧（gfx.Canvas.RequestFrameIn）。
	RequestFrameIn(delay time.Duration)
}

// Page 是岛里的一个页面（真实实现包着 island.Island，测试用假的）。方法只在窗口线程调用。
type Page interface {
	// Post 把一条文本交给页面的 window.viva.onmessage。
	Post(text string)
	// Place 把页面的原生视图放在这一帧的 bounds 上。
	Place(surface Surface, bounds gfx.Rect)
	// Focus 把键盘焦点交给页面（WebView.Focus）。
	Focus()
	// Close 销毁页面。可以重复调用。
	Close()
}

// Opener 打开一个岛页面。closed 在页面被宿主以外的原因关闭时调用（比如收托盘前 island.Group 统一销毁）。
type Opener interface {
	Open(cfg island.Config, closed func()) (Page, error)
}

// Media 按名称取本地媒体：scheme 是 local-image 或 local-video，name 是 scheme 之后的相对路径（已解码）。
// 路径遍历、扩展名白名单与大小上限由实现负责（主包用 resolveLocalMediaPath）；不存在或不允许返回 ok=false。
type Media func(scheme, name string) (mime string, data []byte, ok bool)

// Handler 接收页面的事件。全部在窗口线程、且都是校验过的：
// 事件携带的 itemID 与 tab 是页面上报时所在内容的身份（不是此刻的界面状态），宿主已经丢掉了属于旧内容的迟到事件。
type Handler interface {
	// Matches 是查找的命中数。
	Matches(count int)
	// Scroll 是阅读位置（页面节流后上报；换内容或收起前会补报一次）。
	Scroll(itemID string, tab Tab, top float64)
	// Catalog 与 Toc 是页面里目录浮层、楼层目录的开合。
	Catalog(open bool)
	Toc(open bool)
	// Link 是要用系统浏览器打开的链接（已校验为 http/https）。
	Link(url string)
	// Copy 是要写进剪贴板的文本；返回写入是否成功，宿主据此给页面回执（页面的 writeText 成功或拒绝，台账 B-069）。
	// 失败原因由实现自己向用户说明，这里只需要一个 error。
	Copy(text string) error
	// Edit 是编辑器里文档的当前全文（每次改动一条，不合并）。itemID 与 tab 是改动所属的内容（与 Scroll 同理：
	// 换内容之前页面的最后一次改动，到达时界面可能已经在看别的条目，它仍然属于原来的草稿）。
	Edit(itemID string, tab Tab, text string)
	// Lightbox 是要打开的图片查看器。
	Lightbox(images []Image, index int)
	// Toast 是页面要给用户看的警告。
	Toast(text string)
	// Key 是页面转交的快捷键（白名单内）。
	Key(key string, ctrl, alt, shift bool)
	// FindReset 是页面里点了清除、要求宿主清空查找词。
	FindReset()
	// Failed 是岛打不开或页面不可用：宿主已关掉页面，同一份内容不再重试。
	Failed(err error)
}

// Options 是宿主的依赖。
type Options struct {
	Opener Opener
	Media  Media
	// Diag 接收诊断（已脱敏：只有原因，没有页面给的内容）。可以为空。
	Diag func(err error)
	// Now 是时钟（握手超时用）；空取 time.Now。
	Now func() time.Time
	// ReadyTimeout 是页面从打开到发出 ready 的最长等待；空取 15 秒。超时按岛打不开处理。
	ReadyTimeout time.Duration
}

const (
	defaultReadyTimeout = 15 * time.Second
	// readyPoll 是等页面握手期间的出帧间隔：空闲时没有别的帧，超时要靠它才会被发现。
	readyPoll = 250 * time.Millisecond
)

// Request 是阅读区这一帧想要的状态：宿主与页面当前状态的差异由宿主算出来，调用方每帧给出完整期望即可。
type Request struct {
	ItemID string
	Tab    Tab
	View   View
	// RestoreTop 非空：这份内容第一次显示时滚到这个位置（只在页面里的内容身份变化时发送；
	// 渲染层只在切换条目时恢复阅读位置，切标签页不恢复，所以调用方只在条目刚打开时给出）。
	RestoreTop  *float64
	CatalogOpen bool
	TocOpen     bool
	// FindQuery 与 FindIndex 是页内查找：词为空表示没有在查找；Index 是当前命中的序号（从 0 起）。
	FindQuery string
	FindIndex int
	Theme     Theme
	Lang      Lang
}

// 页面里内容身份的键：条目、标签页与视图种类任一变化，页面里的内容就换一份（页面的 key 同此）。
type viewKey struct {
	itemID string
	tab    Tab
	kind   ViewKind
}

// viewContext 记住发给页面的每一个视图序号对应的内容身份，页面事件按序号找回它。
type viewContext struct {
	seq    uint64
	itemID string
	tab    Tab
	kind   ViewKind
}

const maxViewContexts = 16

// sent 是宿主所知道的页面当前状态：只发与它不同的部分。
type sent struct {
	themeSet    bool
	theme       Theme
	lang        Lang
	viewSet     bool
	key         viewKey
	view        View
	findSet     bool
	findQuery   string
	findIndex   int
	catalogOpen bool
	tocOpen     bool
}

// Host 管理阅读区的岛：需要时才开、不显示就关（空页面的浏览器环境约 120 MB，台账 CP2 风险）、把期望状态同步给页面。
// 窗口线程独占，没有锁。
type Host struct {
	opts    Options
	handler Handler

	page      Page
	ready     bool
	failedKey viewKey
	failed    bool
	failErr   error
	deadline  time.Time

	req   Request
	shown bool

	// viewer 是图片查看岛（viewer.go）：与阅读岛共用 Opener、Media 与 Diag，生命周期随宿主。
	viewer *Viewer

	// gen 是页面的代数：每次打开或关闭加一，页面的回调带着打开时的代数，对不上就是旧页面迟到的东西。
	gen      uint64
	seq      uint64
	contexts []viewContext
	sent     sent
	rendered bool
}

// New 创建宿主。Opener 为空时 Frame 只会报 Failed（没有 WebView 可用）。
func New(opts Options) *Host { return &Host{opts: opts, viewer: newViewer(opts)} }

// Viewer 是图片查看岛的宿主：阅读岛里点图片时，由界面一侧用它另开一个盖满窗口的岛。
func (h *Host) Viewer() *Viewer { return h.viewer }

// SetHandler 设置事件接收者。
func (h *Host) SetHandler(handler Handler) { h.handler = handler }

// Frame 在每帧绘制阅读区时调用：canvas 与 bounds 是阅读区这一帧的画布与位置，req 是期望状态。
// 页面此刻还没开就按需打开；还没 ready 时只记下期望，ready 之后一次发齐。
func (h *Host) Frame(canvas Surface, bounds gfx.Rect, req Request) {
	h.shown = true
	h.req = req
	if req.View.Kind == ViewNone || req.View.Kind == "" {
		h.closePage()
		return
	}
	key := viewKey{req.ItemID, req.Tab, req.View.Kind}
	if h.page == nil {
		if h.failed && h.failedKey == key {
			return
		}
		if !h.open(key) {
			return
		}
	}
	h.page.Place(canvas, bounds)
	if h.ready {
		h.flush()
		return
	}
	// 页面还在加载：隔一会儿出一帧，直到握手完成或超时。
	if h.now().After(h.deadline) {
		h.fail(key, errors.New("reader: 页面超时未完成握手"))
		return
	}
	canvas.RequestFrameIn(readyPoll)
}

// EndFrame 在每帧收尾调用：这一帧没有调 Frame（阅读区没画出来：切了模块、关了详情、折叠到托盘前……），就关掉岛；
// 查看岛同理（Viewer.endFrame）。
func (h *Host) EndFrame() {
	if !h.shown {
		h.closePage()
	}
	h.shown = false
	h.viewer.endFrame()
}

// Close 关闭岛（含查看岛）并丢弃状态（窗口关闭、折叠到托盘前）。
func (h *Host) Close() {
	h.closePage()
	h.shown = false
	h.viewer.Hide()
}

// Focus 把键盘焦点交还给阅读岛（查看器关闭之后：岛销毁时焦点落回窗口本身，页面里的方向键、空格就没人收了）。
// 岛没开或没握手时什么也不做。
func (h *Host) Focus() {
	if h.page != nil && h.ready {
		h.page.Focus()
	}
}

// FocusEditor 把键盘焦点交给编辑态里的编辑器（标题框里按 Enter：渲染层 ItemDetailHeader 直接聚焦 `.cm-content`）。
// 岛没开、没握手或页面里此刻不是编辑视图时什么也不做并返回 false；编辑器还在加载时页面会记下请求，挂好之后聚焦。
func (h *Host) FocusEditor() bool {
	if h.page == nil || !h.ready || !h.sent.viewSet || h.sent.view.Kind != ViewEditor {
		return false
	}
	h.page.Focus()
	return h.post(focusMessage{T: "focus"})
}

// Failure 返回 req 这份内容的岛为什么不可用（打不开、握手超时、协议不符）；没有失败或失败属于别的内容时为 nil。
// 调用方据此画「读取失败 + 重试」而不是空白（AGENTS.md：加载失败不能渲染成正常空态）。
func (h *Host) Failure(req Request) error {
	if h.failed && h.failedKey == (viewKey{req.ItemID, req.Tab, req.View.Kind}) {
		return h.failErr
	}
	return nil
}

// Retry 清掉失败记录：下一帧重新尝试打开。
func (h *Host) Retry() { h.failed, h.failErr = false, nil }

// Open 报告岛此刻是否开着。
func (h *Host) Open() bool { return h.page != nil }

// Ready 报告页面是否已经完成握手。
func (h *Host) Ready() bool { return h.page != nil && h.ready }

// Rendered 报告当前内容是否已经渲染出来（页面发过 rendered）。
func (h *Host) Rendered() bool { return h.page != nil && h.rendered }

// Seq 是最近一次发给页面的视图序号（0 表示还没发过）。
func (h *Host) Seq() uint64 { return h.seq }

func (h *Host) diag(err error) {
	if h.opts.Diag != nil {
		h.opts.Diag(err)
	}
}

func (h *Host) now() time.Time {
	if h.opts.Now != nil {
		return h.opts.Now()
	}
	return time.Now()
}

func (h *Host) open(key viewKey) bool {
	if h.opts.Opener == nil {
		h.fail(key, errors.New("reader: 没有可用的 WebView 宿主"))
		return false
	}
	if err := h.req.Theme.Validate(); err != nil {
		h.fail(key, err)
		return false
	}
	h.gen++
	gen := h.gen
	page, err := h.opts.Opener.Open(island.Config{
		Scheme: scheme,
		Entry:  "index.html",
		Serve:  h.serve,
		CSP:    readerCSP,
		OnMessage: func(text string) {
			if gen == h.gen {
				h.onMessage(text)
			}
		},
		OnDenied: func(string, bool) {
			if gen == h.gen {
				h.diag(errors.New("reader: 页面的一次导航被拒绝（地址不记入日志）"))
			}
		},
	}, h.pageClosed)
	if err != nil {
		h.fail(key, fmt.Errorf("reader: 打开阅读岛失败：%w", err))
		return false
	}
	h.page, h.ready, h.rendered = page, false, false
	h.failed, h.failErr = false, nil
	h.sent = sent{}
	timeout := h.opts.ReadyTimeout
	if timeout <= 0 {
		timeout = defaultReadyTimeout
	}
	h.deadline = h.now().Add(timeout)
	return true
}

// pageClosed 是页面被宿主以外的原因关闭：状态复位，下一帧按需重开。宿主自己关页面时 page 已先置空，这里什么也不做。
func (h *Host) pageClosed() {
	if h.page == nil {
		return
	}
	h.gen++
	h.page, h.ready, h.rendered = nil, false, false
	h.sent = sent{}
}

func (h *Host) closePage() {
	page := h.page
	if page == nil {
		return
	}
	h.gen++
	h.page, h.ready, h.rendered = nil, false, false
	h.sent = sent{}
	page.Close()
}

// fail 关掉页面并通知 Handler；同一份内容不再重试（避免每帧反复开）。
func (h *Host) fail(key viewKey, err error) {
	h.closePage()
	h.failed, h.failedKey, h.failErr = true, key, err
	h.diag(err)
	if h.handler != nil {
		h.handler.Failed(err)
	}
}

func (h *Host) post(message any) bool {
	text, err := encodeMessage(message)
	if err != nil {
		h.fail(viewKey{h.req.ItemID, h.req.Tab, h.req.View.Kind}, fmt.Errorf("reader: 消息编码失败：%w", err))
		return false
	}
	h.page.Post(text)
	return true
}

// flush 把期望状态里与页面不同的部分发过去：外观、内容、查找、目录开合。
func (h *Host) flush() {
	req := h.req
	if !h.sent.themeSet || h.sent.theme != req.Theme || h.sent.lang != req.Lang {
		if err := req.Theme.Validate(); err != nil {
			h.fail(viewKey{req.ItemID, req.Tab, req.View.Kind}, err)
			return
		}
		if !h.post(themeMessage{T: "theme", Theme: req.Theme, Lang: normalizeLang(req.Lang)}) {
			return
		}
		h.sent.themeSet, h.sent.theme, h.sent.lang = true, req.Theme, req.Lang
	}
	key := viewKey{req.ItemID, req.Tab, req.View.Kind}
	if !h.sent.viewSet || h.sent.key != key || h.sent.view != req.View {
		opening := !h.sent.viewSet || h.sent.key != key
		var restore *float64
		if opening {
			restore = req.RestoreTop
		}
		seq := h.seq + 1
		if !h.post(viewMessage{T: "view", Seq: seq, ItemID: req.ItemID, Tab: req.Tab, View: req.View,
			RestoreTop: restore, CatalogOpen: req.CatalogOpen, TocOpen: req.TocOpen}) {
			return
		}
		h.seq = seq
		h.remember(viewContext{seq: seq, itemID: req.ItemID, tab: req.Tab, kind: req.View.Kind})
		h.sent.viewSet, h.sent.key, h.sent.view = true, key, req.View
		h.sent.catalogOpen, h.sent.tocOpen = req.CatalogOpen, req.TocOpen
		h.rendered = false
	}
	switch {
	case !h.sent.findSet && req.FindQuery == "" && req.FindIndex == 0:
		h.sent.findSet = true // 页面初始就是「没有在查找」，不必发
	case !h.sent.findSet || h.sent.findQuery != req.FindQuery || h.sent.findIndex != req.FindIndex:
		if !h.post(findMessage{T: "find", Query: req.FindQuery, Index: req.FindIndex}) {
			return
		}
		h.sent.findSet, h.sent.findQuery, h.sent.findIndex = true, req.FindQuery, req.FindIndex
	}
	if h.sent.catalogOpen != req.CatalogOpen || h.sent.tocOpen != req.TocOpen {
		if !h.post(uiMessage{T: "ui", CatalogOpen: req.CatalogOpen, TocOpen: req.TocOpen}) {
			return
		}
		h.sent.catalogOpen, h.sent.tocOpen = req.CatalogOpen, req.TocOpen
	}
}

func normalizeLang(lang Lang) Lang {
	if lang == LangEn {
		return LangEn
	}
	return LangZh
}

func (h *Host) remember(ctx viewContext) {
	h.contexts = append(h.contexts, ctx)
	if len(h.contexts) > maxViewContexts {
		h.contexts = append(h.contexts[:0], h.contexts[len(h.contexts)-maxViewContexts:]...)
	}
}

func (h *Host) context(seq uint64) (viewContext, bool) {
	for i := len(h.contexts) - 1; i >= 0; i-- {
		if h.contexts[i].seq == seq {
			return h.contexts[i], true
		}
	}
	return viewContext{}, false
}

// onMessage 处理页面发来的一条文本。页面渲染的是不可信内容，所以一律先校验；
// 校验不过、协议版本不对、属于已经换掉的内容的迟到事件都直接丢弃。
func (h *Host) onMessage(text string) {
	if h.page == nil {
		return
	}
	ev, err := ParseEvent(text)
	if err != nil {
		if errors.Is(err, ErrEditTooLarge) && h.ready && h.sent.viewSet && h.sent.view.Kind == ViewEditor {
			// 页面里的文档没能同步到宿主：不能让草稿悄悄落后于页面（「完成编辑」会存旧内容），按页面失败处理，
			// 界面画出失败原因与重试；重试以宿主已有的草稿重开编辑器。fail 自带诊断。
			h.fail(viewKey{h.req.ItemID, h.req.Tab, h.req.View.Kind}, err)
			return
		}
		h.diag(err)
		return
	}
	if ev.Kind == EventReady {
		h.onReady(ev)
		return
	}
	if !h.ready || h.seq == 0 {
		return
	}
	if ev.Kind == EventScroll {
		// 阅读位置按它所属的内容记：换内容之前页面补报的那一次，到达时可能已经是下一份内容的序号之后。
		if ctx, ok := h.context(ev.Seq); ok && h.handler != nil {
			h.handler.Scroll(ctx.itemID, ctx.tab, ev.Top)
		}
		return
	}
	if ev.Kind == EventEdit {
		// 改动按它所属的编辑视图记（同阅读位置）：属于旧内容的迟到改动仍然是旧草稿的最后一笔，丢掉就少存了字。
		ctx, ok := h.context(ev.Seq)
		if !ok || ctx.kind != ViewEditor {
			return
		}
		if ev.Seq == h.seq && h.sent.viewSet && h.sent.view.Kind == ViewEditor {
			// 页面里的文档此刻就是这份：期望里的草稿与它一致时，下一帧不必把整篇再发回去。
			h.sent.view.Content = ev.Text
		}
		if h.handler != nil {
			h.handler.Edit(ctx.itemID, ctx.tab, ev.Text)
		}
		return
	}
	if ev.Seq != h.seq {
		return
	}
	h.dispatch(ev)
}

func (h *Host) onReady(ev Event) {
	key := viewKey{h.req.ItemID, h.req.Tab, h.req.View.Kind}
	if ev.Version != ProtocolVersion {
		h.fail(key, fmt.Errorf("reader: 页面协议版本 %d 与宿主的 %d 不一致", ev.Version, ProtocolVersion))
		return
	}
	if err := h.req.Theme.Validate(); err != nil {
		h.fail(key, err)
		return
	}
	// 页面（重新）握手：它什么状态都没有，从头发。
	h.ready, h.rendered = true, false
	h.sent = sent{}
	if !h.post(initMessage{T: "init", V: ProtocolVersion, Theme: h.req.Theme, Lang: normalizeLang(h.req.Lang)}) {
		return
	}
	h.sent.themeSet, h.sent.theme, h.sent.lang = true, h.req.Theme, h.req.Lang
	h.flush()
}

func (h *Host) dispatch(ev Event) {
	handler := h.handler
	switch ev.Kind {
	case EventRendered:
		h.rendered = true
	case EventMatches:
		if handler != nil {
			handler.Matches(ev.Count)
		}
	case EventCatalog:
		h.sent.catalogOpen = ev.Open
		if handler != nil {
			handler.Catalog(ev.Open)
		}
	case EventToc:
		h.sent.tocOpen = ev.Open
		if handler != nil {
			handler.Toc(ev.Open)
		}
	case EventLink:
		link, ok := SafeLink(ev.URL)
		if !ok {
			h.diag(errors.New("reader: 页面请求打开的链接不是 http/https，已拒绝（地址不记入日志）"))
			return
		}
		if handler != nil {
			handler.Link(link)
		}
	case EventCopy:
		var err error = errors.New("reader: 没有事件接收者")
		if handler != nil {
			err = handler.Copy(ev.Text)
		}
		// 回执：页面等它才亮「已复制」勾（渲染层 writeText 成功之后才亮）。接收者在处理期间可能把页面关了，那就没有人等回执。
		if h.page != nil {
			h.post(copyResultMessage{T: "copyResult", ID: ev.ID, OK: err == nil})
		}
	case EventLightbox:
		if handler != nil {
			handler.Lightbox(ev.Images, ev.Index)
		}
	case EventToast:
		if handler != nil {
			handler.Toast(ev.Text)
		}
	case EventKey:
		if handler != nil {
			handler.Key(ev.Key, ev.Ctrl, ev.Alt, ev.Shift)
		}
	case EventFindReset:
		if handler != nil {
			handler.FindReset()
		}
	case EventError:
		// 页面自己报的错（渲染异常）：只记诊断，不把页面给的文字当界面文案。
		h.diag(fmt.Errorf("reader: 页面报告渲染错误（说明 %d 字节，内容不记入日志）", len(ev.Message)))
	}
}
