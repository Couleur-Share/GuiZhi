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
	"slices"
	"strings"
	"unicode/utf8"
)

// ProtocolVersion 与 apps/desktop/src/renderer/reader/protocol.ts 的 READER_PROTOCOL 一致：页面 ready 时带上它，对不上就拒绝。
// v2：复制带回执（copy 带 id，宿主回 copyResult）；编辑态（editor 视图、页面的 edit 事件、宿主的 focus 消息）。
// v3：网页快照视图（snapshot 视图、页面的 snapshotState 事件、宿主 ui 消息里的 snapshotWidth、转发键增加 escape）。
const ProtocolVersion = 3

// 页面事件的各项上限。超过上限的整条丢弃（页面不会正常超过它们）；编辑事件另有一档（见 MaxEditBytes）。
const (
	// MaxEventBytes 是一条页面事件的最大字节数（JSON 文本）。代码块复制可以很长，所以放宽到 4 MiB。
	MaxEventBytes = 4 << 20
	// MaxEditBytes 是一条编辑事件（edit）的最大字节数：事件带着文档全文，文档可以比普通事件大得多。
	// 超出它的编辑事件不能悄悄丢掉（草稿会落后于页面里的文档，「完成编辑」就会存旧内容），宿主把它按页面失败处理（ErrEditTooLarge）。
	MaxEditBytes = 64 << 20
	// MaxEditTextBytes 是编辑事件里文档全文的最大字节数（UTF-8）。界面一侧用 MaxEditChars 在进入编辑态时拦下超限的文档：
	// 8 Mi 个 UTF-16 码元的文档，JSON 编码后最坏 6 字节每码元（控制字符的 \u00XX 转义），仍在 MaxEditBytes 之内。
	MaxEditTextBytes = 24 << 20
	// MaxEditChars 是能进编辑态的文档的最大长度（UTF-16 码元，渲染层的字符串长度口径；台账 B-070）。
	MaxEditChars = 8 << 20
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

// 网页快照视图的上限（宿主 → 页面，宿主是可信方，但超限仍整条拒绝：消息过大会拖垮页面，也说明上游出了问题）。
const (
	// MaxSnapshotDocBytes 是快照整页 HTML 的最大字节数。公众号文章的净化后 HTML 通常在几百 KB 到数 MB，
	// 16 MiB 之内；JSON 编码最坏（全是控制字符）约 6 倍，仍在 WebView2 单条消息能承受的范围内。
	MaxSnapshotDocBytes = 16 << 20
	maxSnapshotIDBytes  = 256
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
	// ViewEditor 是主题阅读「原文」的编辑态：整个阅读区是一个 CodeMirror Markdown 编辑器（渲染层 ReconstructionReader 的 MarkdownEditor），
	// Content 是草稿；文档由页面里的编辑器持有，每次改动经 edit 事件交回宿主（台账 C-012：编辑器用岛，不用 Viva 自带的代码编辑器）。
	ViewEditor ViewKind = "editor"
	// ViewSnapshot 是公众号网页快照：整页 HTML 放进沙箱 iframe（srcDoc）里显示，外加目录浮层（渲染层 WebSnapshotPane 的 original 分支）。
	// 文档与 iframe 的消息都不可信，页面一侧沿用 WebSnapshotPane 的沙箱与来源校验（台账 A-018）。
	ViewSnapshot ViewKind = "snapshot"
)

// SnapshotWidth 是网页快照在阅读区里的宽度档（渲染层 wechat-reader-width 的三档）：
// original 固定 390 px（手机网页宽度），fluid 取舒适阅读宽度，wide 铺满。空值按 fluid 处理。
type SnapshotWidth string

const (
	SnapshotWidthOriginal SnapshotWidth = "original"
	SnapshotWidthFluid    SnapshotWidth = "fluid"
	SnapshotWidthWide     SnapshotWidth = "wide"
)

// normalized 把空值规整成 fluid；未知取值原样返回（由 validate 拒绝）。
func (w SnapshotWidth) normalized() SnapshotWidth {
	if w == "" {
		return SnapshotWidthFluid
	}
	return w
}

func (w SnapshotWidth) validate() error {
	switch w.normalized() {
	case SnapshotWidthOriginal, SnapshotWidthFluid, SnapshotWidthWide:
		return nil
	}
	return fmt.Errorf("reader: 网页快照宽度 %q 未知", string(w))
}

// View 是阅读区的内容。Tab 只用于 ViewMarkdown，SourceKind（"body" 或 "summary"）只用于 ViewOriginal 与 ViewEditor。
// Snapshot* 只用于 ViewSnapshot：SnapshotDoc 是整页 HTML（srcDoc），SnapshotInstanceID 是这次读取的实例标识（iframe 消息按它过滤），
// SnapshotVersionID 是来源版本（阅读位置按「条目 + 版本」记忆，空表示当前版本），SnapshotAssets 是快照里的本地图片（local-image://…，
// 点图时页面按它找序号交给图片查看岛）。含切片，所以 View 不可直接比较，比较用 sameContent。
type View struct {
	Kind       ViewKind
	Content    string
	SourceKind string
	Tab        Tab

	SnapshotDoc        string
	SnapshotInstanceID string
	SnapshotVersionID  string
	SnapshotWidth      SnapshotWidth
	SnapshotAssets     []string
}

// sameContent 报告两个视图要页面显示的东西是否相同。SnapshotWidth 不算：宽度变化走 ui 消息增量同步，
// 不能因为它重发整页 HTML（iframe 一重载，阅读位置与查找高亮全丢）。
func (v View) sameContent(o View) bool {
	return v.Kind == o.Kind && v.Content == o.Content && v.SourceKind == o.SourceKind && v.Tab == o.Tab &&
		v.SnapshotDoc == o.SnapshotDoc && v.SnapshotInstanceID == o.SnapshotInstanceID &&
		v.SnapshotVersionID == o.SnapshotVersionID && slices.Equal(v.SnapshotAssets, o.SnapshotAssets)
}

// clone 复制切片字段：宿主记下的「已发给页面的视图」不能与调用方每帧重建的 Request 共用底层数组。
func (v View) clone() View {
	v.SnapshotAssets = slices.Clone(v.SnapshotAssets)
	return v
}

// marshalSnapshot 编码快照视图并校验各字段：文档非空且不超限、标识长度合规、宽度已知、图片都是本地图片且不超过查看器的上限。
// 错误文字只描述哪里不对，不含文档内容。
func (v View) marshalSnapshot() ([]byte, error) {
	if v.SnapshotDoc == "" {
		return nil, errors.New("reader: 网页快照文档为空")
	}
	if len(v.SnapshotDoc) > MaxSnapshotDocBytes {
		return nil, fmt.Errorf("reader: 网页快照文档 %d 字节超过上限 %d", len(v.SnapshotDoc), MaxSnapshotDocBytes)
	}
	if !utf8.ValidString(v.SnapshotDoc) {
		return nil, errors.New("reader: 网页快照文档不是合法的 UTF-8")
	}
	// 适配到岛里的 srcdoc（本地图片地址与文档自带的 <meta> 策略，见 snapshot_adapt.go）；适配后的文档略长，上限按适配后的算。
	doc := AdaptSnapshotDocument(v.SnapshotDoc)
	if len(doc) > MaxSnapshotDocBytes {
		return nil, fmt.Errorf("reader: 网页快照文档 %d 字节超过上限 %d", len(doc), MaxSnapshotDocBytes)
	}
	if _, ok := boundedText(&v.SnapshotInstanceID, 1, maxSnapshotIDBytes); !ok {
		return nil, errors.New("reader: 网页快照的实例标识不合规")
	}
	if _, ok := boundedText(&v.SnapshotVersionID, 0, maxSnapshotIDBytes); !ok {
		return nil, errors.New("reader: 网页快照的版本标识不合规")
	}
	if err := v.SnapshotWidth.validate(); err != nil {
		return nil, err
	}
	if len(v.SnapshotAssets) > MaxLightboxImages {
		return nil, fmt.Errorf("reader: 网页快照的图片数 %d 超过上限 %d", len(v.SnapshotAssets), MaxLightboxImages)
	}
	assets := make([]string, 0, len(v.SnapshotAssets))
	for _, asset := range v.SnapshotAssets {
		if _, ok := boundedText(&asset, len("local-image://")+1, maxImageSrcBytes); !ok || !strings.HasPrefix(asset, "local-image://") {
			return nil, errors.New("reader: 网页快照的图片地址不是本地图片")
		}
		assets = append(assets, asset)
	}
	return json.Marshal(struct {
		Kind       ViewKind      `json:"kind"`
		Document   string        `json:"document"`
		InstanceID string        `json:"instanceId"`
		VersionID  string        `json:"versionId"`
		Width      SnapshotWidth `json:"width"`
		Assets     []string      `json:"assets"`
	}{v.Kind, doc, v.SnapshotInstanceID, v.SnapshotVersionID, v.SnapshotWidth.normalized(), assets})
}

// MarshalJSON 按页面 parseView 期望的形状编码：每种只带它的字段，content 为空串时仍然要带（页面据此判断形状）。
func (v View) MarshalJSON() ([]byte, error) {
	switch v.Kind {
	case ViewNone:
		return []byte(`{"kind":"none"}`), nil
	case ViewOriginal, ViewEditor:
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
	case ViewSnapshot:
		return v.marshalSnapshot()
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
		// SnapshotWidth 只在网页快照视图下带：宽度变化不重发整页文档，走这条增量消息。
		SnapshotWidth SnapshotWidth `json:"snapshotWidth,omitempty"`
	}
	// copyResultMessage 是复制的回执：宿主写完系统剪贴板后告诉页面成败（页面的 writeText 据此成功或拒绝，台账 B-069）。
	copyResultMessage struct {
		T  string `json:"t"`
		ID uint64 `json:"id"`
		OK bool   `json:"ok"`
	}
	// focusMessage 要求页面把键盘焦点交给编辑器（标题框里按 Enter）；当前不是编辑视图时页面忽略。
	focusMessage struct {
		T string `json:"t"`
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
	EventReady    EventKind = "ready"
	EventRendered EventKind = "rendered"
	EventMatches  EventKind = "matches"
	EventScroll   EventKind = "scroll"
	EventCatalog  EventKind = "catalog"
	EventToc      EventKind = "toc"
	// EventSnapshotState 是网页快照页面的版面状态：目录是否因窗口够宽而常驻侧栏（宿主据此决定原生工具栏要不要显示「目录」钮）与目录开合。
	EventSnapshotState EventKind = "snapshotState"
	EventLink          EventKind = "link"
	EventCopy          EventKind = "copy"
	EventEdit          EventKind = "edit"
	EventLightbox      EventKind = "lightbox"
	EventToast         EventKind = "toast"
	EventKey           EventKind = "key"
	EventFindReset     EventKind = "findReset"
	EventError         EventKind = "error"
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
	// WideRail 只有 snapshotState 事件有：目录是否常驻在正文侧边的窄轨上（窗口够宽时）。
	WideRail bool
	URL      string
	// Text 是 copy 复制的文本、toast 的提示文字、edit 编辑器里的文档全文。
	Text string
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

// ErrEditTooLarge 是编辑事件超过 MaxEditBytes 或 MaxEditTextBytes：与 ErrBadEvent 同时成立（errors.Is 都为真），
// 区别在于它代表「页面里的文档没能同步到宿主」，宿主据此把页面按失败处理，而不是当作一条无关紧要的坏事件丢掉。
var ErrEditTooLarge = errors.New("reader: 编辑内容超过宿主能接收的上限")

// editEventPrefix 是页面编码的编辑事件的开头（JSON.stringify 按插入顺序写字段，t 在最前）：只有它放宽到 MaxEditBytes。
const editEventPrefix = `{"t":"edit",`

type rawImage struct {
	Src *string `json:"src"`
	Alt *string `json:"alt"`
}

// rawEvent 用指针字段区分「没给」与「零值」：类型不对（比如 seq 是字符串）Unmarshal 就报错，整条丢弃。
type rawEvent struct {
	T        string     `json:"t"`
	V        *float64   `json:"v"`
	Seq      *float64   `json:"seq"`
	ID       *float64   `json:"id"`
	Count    *float64   `json:"count"`
	Top      *float64   `json:"top"`
	Open     *bool      `json:"open"`
	WideRail *bool      `json:"wideRail"`
	URL      *string    `json:"url"`
	Text     *string    `json:"text"`
	Kind     *string    `json:"kind"`
	Images   []rawImage `json:"images"`
	Index    *float64   `json:"index"`
	Key      *string    `json:"key"`
	Ctrl     *bool      `json:"ctrl"`
	Alt      *bool      `json:"alt"`
	Shift    *bool      `json:"shift"`
	Message  *string    `json:"message"`
}

// knownEvents 是认得的事件种类：先于其余校验排除未知种类，之后错误文字里可以放心带上种类名（它一定是下列之一）。
var knownEvents = map[EventKind]bool{
	EventReady: true, EventRendered: true, EventMatches: true, EventScroll: true, EventCatalog: true, EventToc: true,
	EventLink: true, EventCopy: true, EventEdit: true, EventLightbox: true, EventToast: true, EventKey: true, EventFindReset: true,
	EventError: true, EventSnapshotState: true,
}

// forwardedKeys 是页面可以转交宿主的按键：与页面 ReaderApp 里的 FORWARDED_KEYS 一致，外加 escape——
// 网页快照的 iframe 里按 Esc（退出专注阅读）时，窗口收不到按键，页面转交（SnapshotIslandView）。
var forwardedKeys = map[string]bool{"f": true, "s": true, "z": true, "arrowleft": true, "arrowright": true, "escape": true}

// ParseEvent 校验并解析页面经 window.viva.postMessage 发来的一条文本。校验不过返回 ErrBadEvent，调用方丢弃并（脱敏地）记日志。
func ParseEvent(text string) (Event, error) {
	if strings.HasPrefix(text, editEventPrefix) {
		if len(text) > MaxEditBytes {
			return Event{}, fmt.Errorf("%w（%w）：编辑事件长度 %d 超过上限", ErrBadEvent, ErrEditTooLarge, len(text))
		}
	} else if len(text) > MaxEventBytes {
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
	case EventSnapshotState:
		if raw.Open == nil || raw.WideRail == nil {
			return Event{}, fmt.Errorf("%w：snapshotState 缺少版面状态", ErrBadEvent)
		}
		ev.Open, ev.WideRail = *raw.Open, *raw.WideRail
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
	case EventEdit:
		if raw.Text != nil && len(*raw.Text) > MaxEditTextBytes {
			return Event{}, fmt.Errorf("%w（%w）：edit 的文档全文长度 %d 超过上限", ErrBadEvent, ErrEditTooLarge, len(*raw.Text))
		}
		doc, ok := boundedText(raw.Text, 0, MaxEditTextBytes)
		if !ok {
			return Event{}, fmt.Errorf("%w：edit 的文档全文不合规", ErrBadEvent)
		}
		ev.Text = doc
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
