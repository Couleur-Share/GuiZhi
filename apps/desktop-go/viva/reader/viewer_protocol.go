package reader

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// 图片查看岛的协议（v1）：与阅读岛共用版本号（ProtocolVersion）与外观（Theme、Lang），消息更少——
// 页面握手（ready）、宿主给图片与起始序号（init）、页面要求关闭（close）、页面的脚本错误（error）。
// 页面一侧见 apps/desktop/src/renderer/reader/viewer-protocol.ts，两侧字段名必须一致。
// 查看页渲染的图片来自用户的文章，页面本身仍按不可信处理：它发来的一切都先校验。

// maxViewerEventBytes 是查看页一条事件的最大字节数：事件只有握手、关闭与一句错误说明，远用不到阅读岛那样的上限。
const maxViewerEventBytes = 16 << 10

// ViewerEventKind 是查看页事件的种类。
type ViewerEventKind string

const (
	ViewerEventReady ViewerEventKind = "ready"
	ViewerEventClose ViewerEventKind = "close"
	ViewerEventError ViewerEventKind = "error"
)

// ViewerEvent 是校验过的查看页事件。
type ViewerEvent struct {
	Kind    ViewerEventKind
	Version int
	Message string
}

// ErrBadViewerEvent 是查看页事件校验失败。错误文字只描述哪里不对，不含页面给的内容。
var ErrBadViewerEvent = errors.New("reader: 查看页事件不合规")

type rawViewerEvent struct {
	T       string   `json:"t"`
	V       *float64 `json:"v"`
	Message *string  `json:"message"`
}

// ParseViewerEvent 校验并解析查看页经 window.viva.postMessage 发来的一条文本。
func ParseViewerEvent(text string) (ViewerEvent, error) {
	if len(text) > maxViewerEventBytes {
		return ViewerEvent{}, fmt.Errorf("%w：消息长度 %d 超过上限", ErrBadViewerEvent, len(text))
	}
	if !utf8.ValidString(text) {
		return ViewerEvent{}, fmt.Errorf("%w：消息不是合法的 UTF-8", ErrBadViewerEvent)
	}
	var raw rawViewerEvent
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return ViewerEvent{}, fmt.Errorf("%w：不是合法的 JSON 对象", ErrBadViewerEvent)
	}
	ev := ViewerEvent{Kind: ViewerEventKind(raw.T)}
	switch ev.Kind {
	case ViewerEventReady:
		version, ok := wholeNumber(raw.V, 0, 1<<20)
		if !ok {
			return ViewerEvent{}, fmt.Errorf("%w：ready 缺少协议版本", ErrBadViewerEvent)
		}
		ev.Version = int(version)
	case ViewerEventClose:
	case ViewerEventError:
		message, ok := boundedText(raw.Message, 0, maxErrorBytes)
		if !ok {
			return ViewerEvent{}, fmt.Errorf("%w：error 的说明不合规", ErrBadViewerEvent)
		}
		ev.Message = message
	default:
		return ViewerEvent{}, fmt.Errorf("%w：未知的事件种类", ErrBadViewerEvent)
	}
	return ev, nil
}

// 宿主 → 查看页的消息。
type (
	viewerInitMessage struct {
		T      string        `json:"t"`
		V      int           `json:"v"`
		Theme  Theme         `json:"theme"`
		Lang   Lang          `json:"lang"`
		Images []viewerImage `json:"images"`
		Index  int           `json:"index"`
	}
	viewerImage struct {
		Src string `json:"src"`
		Alt string `json:"alt,omitempty"`
	}
)