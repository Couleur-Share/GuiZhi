package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
)

// mediaAdapterRig 把真实的媒体处理器（media_ipc.go）、临时库与 httptest 的 ASR / 对话服务接到 libraryAdapter，
// 推送经 pushHub 分发（与生产装配 native_boot.go / viva_boot.go 一致）。
type mediaAdapterRig struct {
	*mediaEnv
	svc libraryAdapter
	hub *pushHub
}

func newMediaAdapterRig(t *testing.T, withModels bool) *mediaAdapterRig {
	t.Helper()
	e := newMediaEnv(t, withModels)
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}}
	e.l.registerMedia(b)
	hub := newPushHub()
	e.l.setPush(hub.Dispatch)
	return &mediaAdapterRig{mediaEnv: e, svc: libraryAdapter{b: b, hub: hub}, hub: hub}
}

// 写入一个假的本地音频文件并建一个引用它的音频条目（ASR 由 httptest 服务应答，不需要真音频）。
func (r *mediaAdapterRig) audioItem(title, file string) string {
	r.t.Helper()
	if err := os.MkdirAll(r.l.paths.VideosDir(), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.l.paths.VideosDir(), file), []byte("ID3fake"), 0o600); err != nil {
		r.t.Fatal(err)
	}
	return r.create("audio", title, "![x](local-video://"+file+")", "")
}

// 没配置模型：四个动作各自返回 NotConfigured（而不是 error）；业务失败带 Error 但不是 NotConfigured；条目都不会被改。
func TestLibraryMediaAdapterNotConfiguredAndBusinessErrors(t *testing.T) {
	ctx := t.Context()
	r := newMediaAdapterRig(t, false)
	audioID := r.create("audio", "录音", "![x](local-video://a.mp3)", "")
	videoID := r.create("video", "视频", "", "已有文字稿")
	noteID := r.create("note", "笔记", "正文", "")

	res, err := r.svc.TranscribeMedia(ctx, audioID, false)
	if err != nil || !res.NotConfigured || res.OK() || res.Item != nil || res.Error == "" {
		t.Fatalf("转写未配置应返回 NotConfigured：%+v err=%v", res, err)
	}
	for name, run := range map[string]func(context.Context, string) (services.LibraryMediaActionResult, error){
		"排版": func(ctx context.Context, id string) (services.LibraryMediaActionResult, error) {
			return r.svc.FormatTranscript(ctx, id, false)
		},
		"总结": r.svc.SummarizeMedia,
	} {
		res, err := run(ctx, videoID)
		if err != nil || !res.NotConfigured || res.OK() || res.Item != nil {
			t.Fatalf("%s未配置应返回 NotConfigured：%+v err=%v", name, res, err)
		}
	}

	// 业务失败：不是调用失败（err 为空），也不是未配置
	biz := map[string]services.LibraryMediaActionResult{}
	biz["转写缺条目"], _ = r.svc.TranscribeMedia(ctx, "nope", false)
	biz["转写非媒体"], _ = r.svc.TranscribeMedia(ctx, noteID, false)
	biz["排版无文字稿"], _ = r.svc.FormatTranscript(ctx, audioID, false)
	biz["总结缺条目"], _ = r.svc.SummarizeMedia(ctx, "nope")
	biz["刷新缺条目"], _ = r.svc.RefreshForumDiscussion(ctx, "nope")
	biz["刷新非论坛"], _ = r.svc.RefreshForumDiscussion(ctx, noteID)
	for name, got := range biz {
		if got.Error == "" || got.NotConfigured || got.OK() || got.Item != nil {
			t.Errorf("%s：应是带原因的业务失败：%+v", name, got)
		}
	}
	if biz["转写缺条目"].Error != "条目不存在" || biz["排版无文字稿"].Error != "该条目还没有文字稿" {
		t.Errorf("原因应原样透传：%q / %q", biz["转写缺条目"].Error, biz["排版无文字稿"].Error)
	}
	if got := r.item(videoID)["transcript"]; got != "已有文字稿" {
		t.Errorf("失败不应改条目：%v", got)
	}
}

// 参数传递：diarize 与 allowLong 经 map 参数送达处理器，真的改变了处理器的行为。
func TestLibraryMediaAdapterPassesArguments(t *testing.T) {
	ctx := t.Context()
	r := newMediaAdapterRig(t, true)

	// diarize：外部转写接口不支持区分说话人，处理器据此拒绝；不带 diarize 时走到「文件已不存在」。
	id := r.create("audio", "录音", "![x](local-video://gone.mp3)", "")
	res, err := r.svc.TranscribeMedia(ctx, id, true)
	if err != nil || !strings.Contains(res.Error, "区分说话人只有内置本地转写引擎支持") || res.NotConfigured {
		t.Fatalf("diarize=true 应被拒绝：%+v err=%v", res, err)
	}
	res, err = r.svc.TranscribeMedia(ctx, id, false)
	if err != nil || res.Error != "媒体文件已不存在" {
		t.Fatalf("diarize=false 应走到文件检查：%+v err=%v", res, err)
	}
	if r.asrN != 0 {
		t.Fatalf("两次都不该发出转写请求，实际 %d", r.asrN)
	}

	// allowLong：超过自动排版上限的文字稿，不确认时跳过（不发请求），确认后才开始逐块请求。
	long := r.create("video", "长稿", "", strings.Repeat("字", transcriptFormatLongChars+1))
	res, err = r.svc.FormatTranscript(ctx, long, false)
	if err != nil || !strings.Contains(res.Error, "超过自动排版上限") || res.OK() {
		t.Fatalf("allowLong=false 应跳过长稿：%+v err=%v", res, err)
	}
	r.mu.Lock()
	before := r.chatN
	r.mu.Unlock()
	if before != 0 {
		t.Fatalf("跳过时不该发对话请求，实际 %d", before)
	}
	if _, err = r.svc.FormatTranscript(ctx, long, true); err != nil {
		t.Fatalf("allowLong=true：%v", err)
	}
	r.mu.Lock()
	after := r.chatN
	r.mu.Unlock()
	if after == 0 {
		t.Fatal("allowLong=true 应放行并开始逐块排版")
	}
}

// 排版与总结：Item / 标题 / 文字稿从真实处理器的回复里解码出来，排版进度经 hub 转发给订阅者。
func TestLibraryMediaAdapterFormatSummarizeAndProgress(t *testing.T) {
	ctx := t.Context()
	r := newMediaAdapterRig(t, true)
	r.chatOut = []string{"整理后的文字稿。", "标题：新标题\n\n- 要点一"}
	id := r.create("video", "老标题", "> 平台：抖音\n\n正文", "未分段的文字稿 内容")

	var (
		mu     sync.Mutex
		format []services.LibraryTranscriptFormatProgress
	)
	unsub := r.svc.SubscribeMediaProgress(nil, func(p services.LibraryTranscriptFormatProgress) {
		mu.Lock()
		format = append(format, p)
		mu.Unlock()
	})
	defer unsub()

	res, err := r.svc.FormatTranscript(ctx, id, false)
	if err != nil || !res.OK() || res.Warning != "" {
		t.Fatalf("排版：%+v err=%v", res, err)
	}
	if res.Item.ID != id || res.Item.Transcript != "整理后的文字稿。" || res.Item.ItemType != "video" {
		t.Fatalf("排版回复的条目不对：%+v", res.Item)
	}
	mu.Lock()
	got := append([]services.LibraryTranscriptFormatProgress(nil), format...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatal("缺少排版进度推送")
	}
	for _, p := range got {
		if p.ItemID != id || p.Total < 1 || p.Current < 1 || p.Current > p.Total {
			t.Fatalf("排版进度不对：%+v", p)
		}
	}

	res, err = r.svc.SummarizeMedia(ctx, id)
	if err != nil || !res.OK() {
		t.Fatalf("总结：%+v err=%v", res, err)
	}
	if res.Item.Title != "新标题" || !strings.Contains(res.Item.Content, "- 要点一") || !strings.Contains(res.Item.Content, "老标题") {
		t.Fatalf("总结回复的条目不对：title=%q content=%q", res.Item.Title, res.Item.Content)
	}
}

// 转写全链路（转写 → 排版 → 总结）：结果解码、阶段进度经 hub 转发，取消订阅后不再收到。
func TestLibraryMediaAdapterTranscribeEndToEndWithProgress(t *testing.T) {
	ctx := t.Context()
	r := newMediaAdapterRig(t, true)
	r.chatOut = []string{"你好，世界。这是一段口播。", "标题：口播精要\n\n## 要点\n- 问候"}
	id := r.audioItem("原始录音名", "talk.mp3")

	var (
		mu     sync.Mutex
		calls  int
		stages = map[string]bool{}
	)
	unsub := r.svc.SubscribeMediaProgress(func(p services.LibraryTranscribeProgress) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if p.ItemID == id {
			stages[p.Stage] = true
		}
	}, nil)

	res, err := r.svc.TranscribeMedia(ctx, id, false)
	if err != nil || !res.OK() || res.Warning != "" {
		t.Fatalf("转写：%+v err=%v", res, err)
	}
	if res.Item.Transcript != "你好，世界。这是一段口播。" || res.Item.Title != "口播精要" {
		t.Fatalf("转写回复的条目不对：%+v", res.Item)
	}
	mu.Lock()
	seen := calls
	formatting, summarizing := stages[services.LibraryTranscribeStageFormatting], stages[services.LibraryTranscribeStageSummarizing]
	mu.Unlock()
	if !formatting || !summarizing {
		t.Fatalf("应收到排版与总结阶段的进度，实际 %v", stages)
	}

	// 取消订阅（可重复）后，同一频道的推送不再回调
	unsub()
	unsub()
	r.hub.Dispatch(chMediaTranscribeProgress, map[string]any{"itemId": id, "stage": "transcribing", "elapsedMs": 1})
	mu.Lock()
	defer mu.Unlock()
	if calls != seen {
		t.Fatalf("取消订阅后不该再收到推送：%d -> %d", seen, calls)
	}
}

// 进度推送的解码：stalledMs 可有可无（只有本地引擎转写阶段有）；缺 itemId 或形状不对的推送被忽略；
// 两路各自订阅、互不串；取消订阅只撤销自己那一份。
func TestLibraryMediaAdapterProgressDecodeAndUnsubscribe(t *testing.T) {
	hub := newPushHub()
	svc := libraryAdapter{b: &Bridge{}, hub: hub}

	var (
		mu    sync.Mutex
		trans []services.LibraryTranscribeProgress
		fmts  []services.LibraryTranscriptFormatProgress
	)
	unsub := svc.SubscribeMediaProgress(
		func(p services.LibraryTranscribeProgress) { mu.Lock(); trans = append(trans, p); mu.Unlock() },
		func(p services.LibraryTranscriptFormatProgress) { mu.Lock(); fmts = append(fmts, p); mu.Unlock() },
	)
	other := 0
	unsubOther := svc.SubscribeMediaProgress(func(services.LibraryTranscribeProgress) { mu.Lock(); other++; mu.Unlock() }, nil)

	hub.Dispatch(chMediaTranscribeProgress, map[string]any{"itemId": "a", "stage": "transcribing", "elapsedMs": 2500, "stalledMs": 1200})
	hub.Dispatch(chMediaTranscribeProgress, map[string]any{"itemId": "a", "stage": "formatting", "elapsedMs": 3500})
	hub.Dispatch(chMediaTranscribeProgress, map[string]any{"stage": "formatting", "elapsedMs": 1}) // 缺 itemId
	hub.Dispatch(chMediaTranscribeProgress, "not an object")
	hub.Dispatch(chMediaTranscribeProgress, nil)
	hub.Dispatch(chMediaFormatProgress, map[string]any{"itemId": "a", "current": 2, "total": 5})
	hub.Dispatch(chMediaFormatProgress, map[string]any{"current": 1, "total": 5}) // 缺 itemId

	mu.Lock()
	if len(trans) != 2 || len(fmts) != 1 || other != 2 {
		t.Fatalf("事件数不对：trans=%d fmts=%d other=%d", len(trans), len(fmts), other)
	}
	if trans[0].Stage != "transcribing" || trans[0].ElapsedMs != 2500 || trans[0].StalledMs == nil || *trans[0].StalledMs != 1200 {
		t.Fatalf("带 stalledMs 的进度解码不对：%+v", trans[0])
	}
	if trans[1].StalledMs != nil {
		t.Fatalf("没有 stalledMs 时应为 nil：%+v", trans[1])
	}
	if fmts[0] != (services.LibraryTranscriptFormatProgress{ItemID: "a", Current: 2, Total: 5}) {
		t.Fatalf("排版进度解码不对：%+v", fmts[0])
	}
	mu.Unlock()

	unsub()
	hub.Dispatch(chMediaTranscribeProgress, map[string]any{"itemId": "a", "stage": "summarizing", "elapsedMs": 4500})
	hub.Dispatch(chMediaFormatProgress, map[string]any{"itemId": "a", "current": 3, "total": 5})
	mu.Lock()
	if len(trans) != 2 || len(fmts) != 1 {
		t.Fatalf("取消后不该再回调：trans=%d fmts=%d", len(trans), len(fmts))
	}
	if other != 3 {
		t.Fatalf("取消一个订阅不该影响另一个：other=%d", other)
	}
	mu.Unlock()
	unsubOther()

	// 没有推送总线：订阅是空操作，取消也不会崩
	noHub := libraryAdapter{b: &Bridge{}}
	noHub.SubscribeMediaProgress(func(services.LibraryTranscribeProgress) {}, func(services.LibraryTranscriptFormatProgress) {})()
}

// 回复形状的收口：Warning 解码；success 却没有条目、失败却没有原因，都不能当成功交给界面。
func TestLibraryMediaAdapterReplyNormalization(t *testing.T) {
	ctx := t.Context()
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}}
	svc := libraryAdapter{b: b}
	reply := map[string]any{}
	var gotArgs []string
	b.handlers[chMediaSummarize] = func(_ context.Context, a []json.RawMessage) (any, error) {
		gotArgs = gotArgs[:0]
		for _, raw := range a {
			gotArgs = append(gotArgs, string(raw))
		}
		return reply, nil
	}

	reply = map[string]any{"success": true, "warning": "partial", "item": map[string]any{"id": "x", "title": "T", "transcript": "S", "tags": []any{}}}
	res, err := svc.SummarizeMedia(ctx, "x")
	if err != nil || !res.OK() || res.Warning != "partial" || res.Item.Title != "T" || res.Item.Transcript != "S" {
		t.Fatalf("Warning 与 Item 应解码：%+v err=%v", res, err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != `"x"` {
		t.Fatalf("总结只带条目 ID：%v", gotArgs)
	}

	reply = map[string]any{"success": true}
	if res, err = svc.SummarizeMedia(ctx, "x"); err != nil || res.OK() || res.Error == "" || res.Item != nil {
		t.Fatalf("success 无条目不能当成功：%+v err=%v", res, err)
	}
	reply = map[string]any{"success": false}
	if res, err = svc.SummarizeMedia(ctx, "x"); err != nil || res.OK() || res.Error == "" {
		t.Fatalf("失败无原因应补兜底原因：%+v err=%v", res, err)
	}
	reply = map[string]any{"success": false, "notConfigured": true, "error": "no model"}
	if res, err = svc.SummarizeMedia(ctx, "x"); err != nil || res.OK() || !res.NotConfigured || res.Error != "no model" {
		t.Fatalf("未配置应原样保留：%+v err=%v", res, err)
	}

	// 调用本身失败（处理器报错、频道未登记、回复不是对象）作为 error，不伪装成业务失败
	b.handlers[chMediaTranscribe] = func(context.Context, []json.RawMessage) (any, error) { return nil, context.DeadlineExceeded }
	if _, err = svc.TranscribeMedia(ctx, "x", false); err == nil {
		t.Fatal("处理器报错应作为 error 返回")
	}
	if _, err = svc.FormatTranscript(ctx, "x", false); err == nil {
		t.Fatal("频道未登记应作为 error 返回")
	}
	b.handlers[chMediaRefreshForum] = func(context.Context, []json.RawMessage) (any, error) { return "oops", nil }
	if _, err = svc.RefreshForumDiscussion(ctx, "x"); err == nil {
		t.Fatal("回复不是对象应作为解码 error 返回")
	}

	// 参数形状：转写与排版带选项对象
	b.handlers[chMediaTranscribe] = func(_ context.Context, a []json.RawMessage) (any, error) {
		gotArgs = gotArgs[:0]
		for _, raw := range a {
			gotArgs = append(gotArgs, string(raw))
		}
		return map[string]any{"success": false, "error": "e"}, nil
	}
	_, _ = svc.TranscribeMedia(ctx, "x", true)
	if len(gotArgs) != 2 || gotArgs[0] != `"x"` || gotArgs[1] != `{"diarize":true}` {
		t.Fatalf("转写参数：%v", gotArgs)
	}
	b.handlers[chMediaFormatTranscript] = b.handlers[chMediaTranscribe]
	_, _ = svc.FormatTranscript(ctx, "x", true)
	if len(gotArgs) != 2 || gotArgs[1] != `{"allowLong":true}` {
		t.Fatalf("排版参数：%v", gotArgs)
	}
}
