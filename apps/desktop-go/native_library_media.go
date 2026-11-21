package main

// 知识库媒体动作（转写、AI 排版、总结、刷新论坛讨论）及其进度推送的原生服务适配器。
//
// 四个动作经 Bridge.call 调 media_ipc.go 已登记的处理器，与渲染进程共用一套实现与错误文案；
// 进度订阅复用 pushHub（Library.pushMain 的单监听推送经 hub.Dispatch 分发给多个订阅者），
// 与导入队列的 import:changed 是同一条路径，不新增推送通道。

import (
	"context"
	"encoding/json"

	"guizhi-mygo-shell/native/services"
)

// mediaActionWire 是 media:* 处理器的回复形状（MediaTranscribeResult）：只输出被设置的键。
type mediaActionWire struct {
	Success       bool                  `json:"success"`
	Item          *services.LibraryItem `json:"item"`
	Warning       string                `json:"warning"`
	NotConfigured bool                  `json:"notConfigured"`
	Error         string                `json:"error"`
}

// result 把回复收成 LibraryMediaActionResult，并保证「成功必带条目」：
// 渲染层同样只在 success 且带 item 时才算成功，其余一律按失败处理；
// 处理器没给原因时补一条兜底原因，不让界面拿到一个既不成功、也没有原因的结果。
func (w mediaActionWire) result() services.LibraryMediaActionResult {
	res := services.LibraryMediaActionResult{Warning: w.Warning, NotConfigured: w.NotConfigured, Error: w.Error}
	switch {
	case w.Success && w.Item != nil:
		res.Item = w.Item
	case w.Success:
		res.Error = "the media action reported success but returned no item"
	case !w.NotConfigured && w.Error == "":
		res.Error = "the media action failed without a reason"
	}
	return res
}

func (a libraryAdapter) mediaAction(ctx context.Context, channel string, args ...any) (services.LibraryMediaActionResult, error) {
	wire, err := callAs[mediaActionWire](ctx, a.b, channel, args...)
	if err != nil {
		return services.LibraryMediaActionResult{}, err
	}
	return wire.result(), nil
}

func (a libraryAdapter) TranscribeMedia(ctx context.Context, itemID string, diarize bool) (services.LibraryMediaActionResult, error) {
	return a.mediaAction(ctx, chMediaTranscribe, itemID, map[string]any{"diarize": diarize})
}

func (a libraryAdapter) FormatTranscript(ctx context.Context, itemID string, allowLong bool) (services.LibraryMediaActionResult, error) {
	return a.mediaAction(ctx, chMediaFormatTranscript, itemID, map[string]any{"allowLong": allowLong})
}

func (a libraryAdapter) SummarizeMedia(ctx context.Context, itemID string) (services.LibraryMediaActionResult, error) {
	return a.mediaAction(ctx, chMediaSummarize, itemID)
}

func (a libraryAdapter) RefreshForumDiscussion(ctx context.Context, itemID string) (services.LibraryMediaActionResult, error) {
	return a.mediaAction(ctx, chMediaRefreshForum, itemID)
}

// decodePush 把推送载荷（处理器侧的 map）按 JSON 往返解成 DTO；解不出或缺少条目 ID 的推送视为无效。
func decodePush[T any](payload any, itemID func(T) string) (T, bool) {
	var out T
	raw, err := json.Marshal(payload)
	if err != nil || json.Unmarshal(raw, &out) != nil || itemID(out) == "" {
		var zero T
		return zero, false
	}
	return out, true
}

// SubscribeMediaProgress 订阅主进程推送的 media:transcribeProgress 与 media:formatProgress。
// 没有挂推送总线（hub 为空）时什么也不订阅，返回空操作的取消函数。
func (a libraryAdapter) SubscribeMediaProgress(onTranscribe func(services.LibraryTranscribeProgress), onFormat func(services.LibraryTranscriptFormatProgress)) func() {
	var cancels []func()
	if a.hub != nil {
		if onTranscribe != nil {
			cancels = append(cancels, a.hub.Subscribe(chMediaTranscribeProgress, func(payload any) {
				if p, ok := decodePush(payload, func(p services.LibraryTranscribeProgress) string { return p.ItemID }); ok {
					onTranscribe(p)
				}
			}))
		}
		if onFormat != nil {
			cancels = append(cancels, a.hub.Subscribe(chMediaFormatProgress, func(payload any) {
				if p, ok := decodePush(payload, func(p services.LibraryTranscriptFormatProgress) string { return p.ItemID }); ok {
					onFormat(p)
				}
			}))
		}
	}
	return func() {
		for _, cancel := range cancels {
			cancel()
		}
	}
}
