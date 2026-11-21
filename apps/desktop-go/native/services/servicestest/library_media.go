package servicestest

import (
	"context"
	"slices"

	"guizhi-mygo-shell/native/services"
)

// 媒体动作（M2-6d）的方法名，同时是 Calls / FailOn 里的名字与 MediaCall.Method 的取值。
const (
	MediaMethodTranscribe = "TranscribeMedia"
	MediaMethodFormat     = "FormatTranscript"
	MediaMethodSummarize  = "SummarizeMedia"
	MediaMethodRefresh    = "RefreshForumDiscussion"
)

// MediaCall 是媒体动作收到的一次请求（含被 FailOn 拦下的）。
type MediaCall struct {
	Method string
	ItemID string
	// Diarize 只对 TranscribeMedia 有意义；AllowLong 只对 FormatTranscript 有意义。
	Diarize   bool
	AllowLong bool
}

// mediaSeed 是某个动作的预设回复。
type mediaSeed struct {
	res services.LibraryMediaActionResult
	err error
	set bool
}

// mediaState 是媒体动作的假实现状态；零值可用。
type mediaState struct {
	seeds map[string]mediaSeed
	gates map[string]chan struct{}
	calls []MediaCall

	nextSub  int
	onTrans  map[int]func(services.LibraryTranscribeProgress)
	onFormat map[int]func(services.LibraryTranscriptFormatProgress)
}

func (f *Library) seedMedia(method string, res services.LibraryMediaActionResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.media.seeds == nil {
		f.media.seeds = map[string]mediaSeed{}
	}
	f.media.seeds[method] = mediaSeed{res: res, err: err, set: true}
}

// SeedTranscribe 配置之后 TranscribeMedia 的回复（对所有条目生效）；err 非空表示调用本身失败。
func (f *Library) SeedTranscribe(res services.LibraryMediaActionResult, err error) {
	f.seedMedia(MediaMethodTranscribe, res, err)
}

// SeedFormatTranscript 配置之后 FormatTranscript 的回复。
func (f *Library) SeedFormatTranscript(res services.LibraryMediaActionResult, err error) {
	f.seedMedia(MediaMethodFormat, res, err)
}

// SeedSummarizeMedia 配置之后 SummarizeMedia 的回复。
func (f *Library) SeedSummarizeMedia(res services.LibraryMediaActionResult, err error) {
	f.seedMedia(MediaMethodSummarize, res, err)
}

// SeedRefreshForum 配置之后 RefreshForumDiscussion 的回复。
func (f *Library) SeedRefreshForum(res services.LibraryMediaActionResult, err error) {
	f.seedMedia(MediaMethodRefresh, res, err)
}

func (f *Library) holdMedia(method string) (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	if f.media.gates == nil {
		f.media.gates = map[string]chan struct{}{}
	}
	f.media.gates[method] = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// HoldTranscribe 让下一次 TranscribeMedia 卡住，直到返回的函数被调用（只卡一次）。
func (f *Library) HoldTranscribe() (release func()) { return f.holdMedia(MediaMethodTranscribe) }

// HoldFormatTranscript 让下一次 FormatTranscript 卡住。
func (f *Library) HoldFormatTranscript() (release func()) { return f.holdMedia(MediaMethodFormat) }

// HoldSummarizeMedia 让下一次 SummarizeMedia 卡住。
func (f *Library) HoldSummarizeMedia() (release func()) { return f.holdMedia(MediaMethodSummarize) }

// HoldRefreshForum 让下一次 RefreshForumDiscussion 卡住。
func (f *Library) HoldRefreshForum() (release func()) { return f.holdMedia(MediaMethodRefresh) }

// MediaCalls 返回媒体动作收到的请求，按发生顺序（含被 FailOn 拦下的）。
func (f *Library) MediaCalls() []MediaCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.media.calls)
}

// MediaSubscribers 返回此刻仍有效的进度订阅数（取消后减少）。
func (f *Library) MediaSubscribers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := map[int]struct{}{}
	for id := range f.media.onTrans {
		ids[id] = struct{}{}
	}
	for id := range f.media.onFormat {
		ids[id] = struct{}{}
	}
	return len(ids)
}

// EmitTranscribeProgress 模拟主进程推送一次转写进度：同步交给全部订阅者（在锁外回调）。
func (f *Library) EmitTranscribeProgress(p services.LibraryTranscribeProgress) {
	f.mu.Lock()
	fns := make([]func(services.LibraryTranscribeProgress), 0, len(f.media.onTrans))
	for _, fn := range f.media.onTrans {
		fns = append(fns, fn)
	}
	f.mu.Unlock()
	for _, fn := range fns {
		fn(p)
	}
}

// EmitFormatProgress 模拟主进程推送一次排版进度。
func (f *Library) EmitFormatProgress(p services.LibraryTranscriptFormatProgress) {
	f.mu.Lock()
	fns := make([]func(services.LibraryTranscriptFormatProgress), 0, len(f.media.onFormat))
	for _, fn := range f.media.onFormat {
		fns = append(fns, fn)
	}
	f.mu.Unlock()
	for _, fn := range fns {
		fn(p)
	}
}

// runMedia 是四个动作的共同流程：记录请求、取注入的失败、等闸门、返回预设回复。
func (f *Library) runMedia(ctx context.Context, call MediaCall) (services.LibraryMediaActionResult, error) {
	f.mu.Lock()
	f.media.calls = append(f.media.calls, call)
	f.mu.Unlock()
	if err := f.begin(call.Method+":"+call.ItemID, call.Method); err != nil {
		return services.LibraryMediaActionResult{}, err
	}
	f.mu.Lock()
	gate := f.media.gates[call.Method]
	delete(f.media.gates, call.Method)
	seed := f.media.seeds[call.Method]
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return services.LibraryMediaActionResult{}, err
	}
	if seed.err != nil {
		return services.LibraryMediaActionResult{}, seed.err
	}
	if !seed.set { // 没配置：成功并回写一个只有 ID 的条目
		return services.LibraryMediaActionResult{Item: &services.LibraryItem{ID: call.ItemID}}, nil
	}
	res := seed.res
	if res.Item != nil {
		item := *res.Item
		res.Item = &item
	}
	return res, nil
}

func (f *Library) TranscribeMedia(ctx context.Context, itemID string, diarize bool) (services.LibraryMediaActionResult, error) {
	return f.runMedia(ctx, MediaCall{Method: MediaMethodTranscribe, ItemID: itemID, Diarize: diarize})
}

func (f *Library) FormatTranscript(ctx context.Context, itemID string, allowLong bool) (services.LibraryMediaActionResult, error) {
	return f.runMedia(ctx, MediaCall{Method: MediaMethodFormat, ItemID: itemID, AllowLong: allowLong})
}

func (f *Library) SummarizeMedia(ctx context.Context, itemID string) (services.LibraryMediaActionResult, error) {
	return f.runMedia(ctx, MediaCall{Method: MediaMethodSummarize, ItemID: itemID})
}

func (f *Library) RefreshForumDiscussion(ctx context.Context, itemID string) (services.LibraryMediaActionResult, error) {
	return f.runMedia(ctx, MediaCall{Method: MediaMethodRefresh, ItemID: itemID})
}

// SubscribeMediaProgress 登记进度回调；取消函数可重复调用。
func (f *Library) SubscribeMediaProgress(onTranscribe func(services.LibraryTranscribeProgress), onFormat func(services.LibraryTranscriptFormatProgress)) func() {
	f.mu.Lock()
	f.media.nextSub++
	id := f.media.nextSub
	if onTranscribe != nil {
		if f.media.onTrans == nil {
			f.media.onTrans = map[int]func(services.LibraryTranscribeProgress){}
		}
		f.media.onTrans[id] = onTranscribe
	}
	if onFormat != nil {
		if f.media.onFormat == nil {
			f.media.onFormat = map[int]func(services.LibraryTranscriptFormatProgress){}
		}
		f.media.onFormat[id] = onFormat
	}
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		delete(f.media.onTrans, id)
		delete(f.media.onFormat, id)
		f.mu.Unlock()
	}
}
