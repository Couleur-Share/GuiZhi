// Package reader 是阅读区「岛」的宿主：知识库详情里的阅读内容（Markdown 预览、论坛楼层讨论、图片画廊）由浏览器引擎渲染，
// 页面源码在 apps/desktop/src/renderer/reader/（复用渲染层的阅读组件），构建产物嵌在 assets/ 里（jsgen/build-reader.mts 生成）。
//
// 本包只做三件事：
//   - 协议（protocol.go）：宿主 → 页面的消息怎么编码，页面 → 宿主的事件怎么校验。页面渲染的是不可信内容
//     （台账 A-018），所以页面发来的一切都按不可信输入处理：长度上限、字段类型、取值白名单，校验不过的整条丢弃。
//   - 宿主（host.go）：岛的按需开关、状态同步（只发变了的）、事件分派。窗口线程独占，没有锁。
//   - 资源（serve.go）：页面文件与本地媒体（local-image:// 改写后的 ./media/…，路径与扩展名由调用方提供的 Media 校验）。
//
// Package reader hosts the reading-area island: a WebView2 page that renders article Markdown, forum floors and image
// galleries with the renderer's own components. The page is untrusted (ledger A-018): everything it sends is validated.
package reader

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"
)

// ProtocolVersion 与 apps/desktop/src/renderer/reader/protocol.ts 的 READER_PROTOCOL 一致：页面 ready 时带上它，对不上就拒绝。
// v2：复制带回执（copy 带 id，宿主回 copyResult）。
const ProtocolVersion = 2

// 页面事件的各项上限。超过上限的整条丢弃（页面不会正常超过它们）。
const (
	// MaxEventBytes 是一条页面事件的最大字节数（JSON 文本）。代码块复制可以很长，所以放宽到 4 MiB。
	MaxEventBytes = 4 << 20
	// MaxCopyBytes 是一次复制的最大文本长度。
	MaxCopyBytes = 2 << 20
	// MaxLinkBytes 是一个链接的最大长度。
	MaxLinkBytes = 8 << 10
	// MaxLightboxImages 是查看器一次最多接收的图片数。
	MaxLightboxImages = 2000
	maxImageSrcBytes  = 512
	maxImageAltBytes  = 1 << 10
	maxToastBytes     = 512
	maxErrorBytes     = 2 << 10
	maxCount          = 1 << 30
	maxScrollTop      = 1 << 30
)

// Lang 是页面的界面语言。
type Lang string

const (
	LangZh Lang = "zh"
	LangEn Lang = "en"
)

// Motion 是动效档位：与渲染层写到 <html data-motion> 上的取值一致。
type Motion string

const (
	MotionStandard Motion = "standard"
	MotionReduced  Motion = "reduced"
	MotionOff      Motion = "off"
)

// Theme 是页面的外观输入：与渲染层 applyTheme 写到 <html> 上的东西一一对应，页面据此用同一份样式表算出全部令牌。
type Theme struct {
	Dark       bool    `json:"dark"`
	Hue        float64 `json:"hue"`
	Saturation float64 `json:"saturation"`
	// BasePx 是根字号（px），设置里的 14 / 16 / 18 三档。
	BasePx float64 `json:"basePx"`
	Motion Motion  `json:"motion"`
	// ViewportWidth 是窗口宽度（逻辑像素）：渲染层的响应式样式（sm / md / lg / xl / 2xl）按窗口宽度判定，岛的视口只有阅读区那么宽，
	// 页面据此写 <html data-win-ge>，样式表里的 min-width 断点改按它判定（页面一侧见 reader/breakpoints.ts）。0 表示不给，页面取自身宽度。
	ViewportWidth float64 `json:"viewportWidth,omitempty"`
}

// Validate 检查外观输入：数值有限、根字号在页面接受的范围内、动效档位已知。页面对坏值同样会丢弃，宿主先拦下才能给出原因。
func (t Theme) Validate() error {
	for _, v := range []float64{t.Hue, t.Saturation, t.BasePx, t.ViewportWidth} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("reader: 外观数值不是有限数")
		}
	}
	if t.ViewportWidth != 0 && (t.ViewportWidth < 100 || t.ViewportWidth > 20000) {
		return fmt.Errorf("reader: 窗口宽度 %v 超出 100–20000", t.ViewportWidth)
	}
	if t.BasePx < 8 || t.BasePx > 40 {
		return fmt.Errorf("reader: 根字号 %v 超出 8–40", t.BasePx)
	}
	switch t.Motion {
	case MotionStandard, MotionReduced, MotionOff:
		return nil
	}
	return fmt.Errorf("reader: 未知的动效档位 %q", t.Motion)
}

// Tab 是内容面板的标签页（与渲染层 ReadingPanelTab、页面 ReaderTab 同一套取值）。
type Tab string

const (
	TabBody       Tab = "body"
	TabTranscript Tab = "transcript"
	TabImages     Tab = "images"
	TabRecognized Tab = "recognized"
	TabSummary    Tab = "summary"
	TabReplies    Tab = "replies"
)

func validTab(tab Tab) bool {
	switch tab {
	case TabBody, TabTranscript, TabImages, TabRecognized, TabSummary, TabReplies:
		return true
	}
	return false
}

// ViewKind 是阅读区此刻要显示的东西的种类，每种对应渲染层 ContentPanel 里 data-article-reader 内的一个分支。
type ViewKind string

const (
	// ViewNone 什么也不显示（宿主此时不开岛）。
	ViewNone ViewKind = "none"
	// ViewOriginal 是主题阅读的「原文」：带目录浮层的 Markdown 预览。
	ViewOriginal ViewKind = "original"
	// ViewMarkdown 是纯 Markdown 预览（图片条目的识别文字）。
	ViewMarkdown ViewKind = "markdown"
	// ViewReplies 是论坛讨论：楼层卡片与楼层目录。
	ViewReplies ViewKind = "replies"
	// ViewGallery 是图片条目的配图画廊。
	ViewGallery ViewKind = "gallery"
)

// View 是阅读区的内容。Tab 只用于 ViewMarkdown，SourceKind（"body" 或 "summary"）只用于 ViewOriginal。
type View struct {
	Kind       ViewKind
	Content    string
	SourceKind string
	Tab        Tab
}

// MarshalJSON 按页面 parseView 期望的形状编码：每种只带它的字段，content 为空串时仍然要带（页面据此判断形状）。
func (v View) MarshalJSON() ([]byte, error) {
	switch v.Kind {
	case ViewNone:
		return []byte(`{"kind":"none"}`), nil
	case ViewOriginal:
		if v.SourceKind != "body" && v.SourceKind != "summary" {
			return nil, fmt.Errorf("reader: 原文的来源 %q 只能是 body 或 summary", v.SourceKind)
		}
		return json.Marshal(struct {
			Kind       ViewKind `json:"kind"`
			Content    string   `json:"content"`
			SourceKind string   `json:"sourceKind"`
		}{v.Kind, v.Content, v.SourceKind})
	case ViewMarkdown:
		if !validTab(v.Tab) {
			return nil, fmt.Errorf("reader: Markdown 视图的标签 %q 未知", v.Tab)
		}
		return json.Marshal(struct {
			Kind    ViewKind `json:"kind"`
			Content string   `json:"content"`
			Tab     Tab      `json:"tab"`
		}{v.Kind, v.Content, v.Tab})
	case ViewReplies, ViewGallery:
		return json.Marshal(struct {
			Kind    ViewKind `json:"kind"`
			Content string   `json:"content"`
		}{v.Kind, v.Content})
	}
	return nil, fmt.Errorf("reader: 未知的视图种类 %q", v.Kind)
}

// 宿主 → 页面的消息。字段名与页面 protocol.ts 的 HostMessage 一致。
type (
	initMessage struct {
		T     string `json:"t"`
		V     int    `json:"v"`
		Theme Theme  `json:"theme"`
		Lang  Lang   `json:"lang"`
	}
	themeMessage struct {
		T     string `json:"t"`
		Theme Theme  `json:"theme"`
		Lang  Lang   `json:"lang"`
	}
	viewMessage struct {
		T           string   `json:"t"`
		Seq         uint64   `json:"seq"`
		ItemID      string   `json:"itemId"`
		Tab         Tab      `json:"tab"`
		View        View     `json:"view"`
		RestoreTop  *float64 `json:"restoreTop"`
		CatalogOpen bool     `json:"catalogOpen"`
		TocOpen     bool     `json:"tocOpen"`
	}
	findMessage struct {
		T     string `json:"t"`
		Query string `json:"query"`
		Index int    `json:"index"`
	}
	uiMessage struct {
		T           string `json:"t"`
		CatalogOpen bool   `json:"catalogOpen"`
		TocOpen     bool   `json:"tocOpen"`
	}
	// copyResultMessage 是复制的回执：宿主写完系统剪贴板后告诉页面成败（页面的 writeText 据此成功或拒绝，台账 B-069）。
	copyResultMessage struct {
		T  string `json:"t"`
		ID uint64 `json:"id"`
		OK bool   `json:"ok"`
	}
)

func encodeMessage(message any) (string, error) {
	data, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// EventKind 是页面事件的种类。
type EventKind string

const (
	EventReady     EventKind = "ready"
	EventRendered  EventKind = "rendered"
	EventMatches   EventKind = "matches"
	EventScroll    EventKind = "scroll"
	EventCatalog   EventKind = "catalog"
	EventToc       EventKind = "toc"
	EventLink      EventKind = "link"
	EventCopy      EventKind = "copy"
	EventLightbox  EventKind = "lightbox"
	EventToast     EventKind = "toast"
	EventKey       EventKind = "key"
	EventFindReset EventKind = "findReset"
	EventError     EventKind = "error"
)

// Image 是查看器里的一张图。Src 一定是 local-image://<名称>（校验保证）。
type Image struct {
	Src string
	Alt string
}

// Event 是校验过的页面事件。只有 Kind 对应的字段有意义，其余是零值。
type Event struct {
	Kind EventKind
	// Seq 是页面上报时所在内容的序号（ready 没有，为 0）。
	Seq     uint64
	Version int
	Count   int
	Top     float64
	Open    bool
	URL     string
	Text    string
	// ID 是复制的编号（页面每次复制递增，回执 copyResult 带回它）；只有 copy 事件有。
	ID     uint64
	Images []Image
	Index  int
	// Key 是小写的按键名（白名单内），Ctrl、Alt、Shift 是修饰键。
	Key              string
	Ctrl, Alt, Shift bool
	Message          string
}

// ErrBadEvent 是页面事件校验失败。错误文字只描述哪里不对，不含页面给的内容（那是不可信的，也可能很长）。
var ErrBadEvent = errors.New("reader: 页面事件不合规")

type rawImage struct {
	Src *string `json:"src"`
	Alt *string `json:"alt"`
}

// rawEvent 用指针字段区分「没给」与「零值」：类型不对（比如 seq 是字符串）Unmarshal 就报错，整条丢弃。
type rawEvent struct {
	T       string     `json:"t"`
	V       *float64   `json:"v"`
	Seq     *float64   `json:"seq"`
	ID      *float64   `json:"id"`
	Count   *float64   `json:"count"`
	Top     *float64   `json:"top"`
	Open    *bool      `json:"open"`
	URL     *string    `json:"url"`
	Text    *string    `json:"text"`
	Kind    *string    `json:"kind"`
	Images  []rawImage `json:"images"`
	Index   *float64   `json:"index"`
	Key     *string    `json:"key"`
	Ctrl    *bool      `json:"ctrl"`
	Alt     *bool      `json:"alt"`
	Shift   *bool      `json:"shift"`
	Message *string    `json:"message"`
}

// knownEvents 是认得的事件种类：先于其余校验排除未知种类，之后错误文字里可以放心带上种类名（它一定是下列之一）。
var knownEvents = map[EventKind]bool{
	EventReady: true, EventRendered: true, EventMatches: true, EventScroll: true, EventCatalog: true, EventToc: true,
	EventLink: true, EventCopy: true, EventLightbox: true, EventToast: true, EventKey: true, EventFindReset: true, EventError: true,
}

// forwardedKeys 是页面可以转交宿主的按键：与页面 ReaderApp 里的 FORWARDED_KEYS 一致。
var forwardedKeys = map[string]bool{"f": true, "s": true, "z": true, "arrowleft": true, "arrowright": true}

// ParseEvent 校验并解析页面经 window.viva.postMessage 发来的一条文本。校验不过返回 ErrBadEvent，调用方丢弃并（脱敏地）记日志。
func ParseEvent(text string) (Event, error) {
	if len(text) > MaxEventBytes {
		return Event{}, fmt.Errorf("%w：消息长度 %d 超过上限", ErrBadEvent, len(text))
	}
	if !utf8.ValidString(text) {
		return Event{}, fmt.Errorf("%w：消息不是合法的 UTF-8", ErrBadEvent)
	}
	var raw rawEvent
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return Event{}, fmt.Errorf("%w：不是合法的 JSON 对象", ErrBadEvent)
	}
	ev := Event{Kind: EventKind(raw.T)}
	if !knownEvents[ev.Kind] {
		return Event{}, fmt.Errorf("%w：未知的事件种类", ErrBadEvent)
	}
	if ev.Kind == EventReady {
		version, ok := wholeNumber(raw.V, 0, 1<<20)
		if !ok {
			return Event{}, fmt.Errorf("%w：ready 缺少协议版本", ErrBadEvent)
		}
		ev.Version = int(version)
		return ev, nil
	}
	seq, ok := wholeNumber(raw.Seq, 0, 1<<53-1)
	if !ok {
		return Event{}, fmt.Errorf("%w：%s 缺少序号", ErrBadEvent, raw.T)
	}
	ev.Seq = uint64(seq)
	switch ev.Kind {
	case EventRendered, EventFindReset:
	case EventMatches:
		count, ok := wholeNumber(raw.Count, 0, maxCount)
		if !ok {
			return Event{}, fmt.Errorf("%w：matches 的命中数不合规", ErrBadEvent)
		}
		ev.Count = int(count)
	case EventScroll:
		if raw.Top == nil || math.IsNaN(*raw.Top) || math.IsInf(*raw.Top, 0) {
			return Event{}, fmt.Errorf("%w：scroll 的位置不是有限数", ErrBadEvent)
		}
		ev.Top = math.Min(math.Max(*raw.Top, 0), maxScrollTop)
	case EventCatalog, EventToc:
		if raw.Open == nil {
			return Event{}, fmt.Errorf("%w：%s 缺少开合状态", ErrBadEvent, raw.T)
		}
		ev.Open = *raw.Open
	case EventLink:
		link, ok := boundedText(raw.URL, 1, MaxLinkBytes)
		if !ok {
			return Event{}, fmt.Errorf("%w：link 的地址不合规", ErrBadEvent)
		}
		ev.URL = link
	case EventCopy:
		copied, ok := boundedText(raw.Text, 0, MaxCopyBytes)
		if !ok {
			return Event{}, fmt.Errorf("%w：copy 的文本不合规", ErrBadEvent)
		}
		ev.Text = copied
		id, ok := wholeNumber(raw.ID, 1, 1<<53-1)
		if !ok {
			return Event{}, fmt.Errorf("%w：copy 缺少编号", ErrBadEvent)
		}
		ev.ID = uint64(id)
	case EventLightbox:
		return parseLightbox(ev, raw)
	case EventToast:
		if raw.Kind == nil || *raw.Kind != "warning" {
			return Event{}, fmt.Errorf("%w：toast 只支持 warning", ErrBadEvent)
		}
		toast, ok := boundedText(raw.Text, 1, maxToastBytes)
		if !ok {
			return Event{}, fmt.Errorf("%w：toast 的文字不合规", ErrBadEvent)
		}
		ev.Text = toast
	case EventKey:
		if raw.Key == nil || !forwardedKeys[*raw.Key] {
			return Event{}, fmt.Errorf("%w：key 不在转发白名单内", ErrBadEvent)
		}
		ev.Key = *raw.Key
		ev.Ctrl, ev.Alt, ev.Shift = flag(raw.Ctrl), flag(raw.Alt), flag(raw.Shift)
	case EventError:
		message, ok := boundedText(raw.Message, 0, maxErrorBytes)
		if !ok {
			return Event{}, fmt.Errorf("%w：error 的说明不合规", ErrBadEvent)
		}
		ev.Message = message
	default:
		return Event{}, fmt.Errorf("%w：未知的事件种类", ErrBadEvent)
	}
	return ev, nil
}

func parseLightbox(ev Event, raw rawEvent) (Event, error) {
	if len(raw.Images) == 0 || len(raw.Images) > MaxLightboxImages {
		return Event{}, fmt.Errorf("%w：lightbox 的图片数 %d 不合规", ErrBadEvent, len(raw.Images))
	}
	index, ok := wholeNumber(raw.Index, 0, float64(len(raw.Images)-1))
	if !ok {
		return Event{}, fmt.Errorf("%w：lightbox 的序号越界", ErrBadEvent)
	}
	ev.Index = int(index)
	ev.Images = make([]Image, 0, len(raw.Images))
	for _, image := range raw.Images {
		src, ok := boundedText(image.Src, 1, maxImageSrcBytes)
		if !ok || !strings.HasPrefix(src, "local-image://") || len(src) == len("local-image://") {
			return Event{}, fmt.Errorf("%w：lightbox 的图片地址不是本地图片", ErrBadEvent)
		}
		alt := ""
		if image.Alt != nil {
			if alt, ok = boundedText(image.Alt, 0, maxImageAltBytes); !ok {
				return Event{}, fmt.Errorf("%w：lightbox 的图片说明不合规", ErrBadEvent)
			}
		}
		ev.Images = append(ev.Images, Image{Src: src, Alt: alt})
	}
	return ev, nil
}

func flag(value *bool) bool { return value != nil && *value }

// wholeNumber 取 [lo, hi] 内的整数；没给、不是整数、越界都返回 false。
func wholeNumber(value *float64, lo, hi float64) (float64, bool) {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value != math.Trunc(*value) {
		return 0, false
	}
	if *value < lo || *value > hi {
		return 0, false
	}
	return *value, true
}

// boundedText 取长度（字节）在 [min, max] 内的合法 UTF-8 文本。
func boundedText(value *string, min, max int) (string, bool) {
	if value == nil || len(*value) < min || len(*value) > max || !utf8.ValidString(*value) {
		return "", false
	}
	return *value, true
}

// SafeLink 校验页面请求打开的链接：只放行 http 与 https、有主机名、没有用户信息、没有控制字符。
// 返回规整后的地址；不合规的返回 false（调用方不打开，也不要把地址原样写进日志）。
func SafeLink(raw string) (string, bool) {
	if raw == "" || len(raw) > MaxLinkBytes || strings.ContainsAny(raw, "\x00\r\n\t") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", false
	}
	return u.String(), true
}
