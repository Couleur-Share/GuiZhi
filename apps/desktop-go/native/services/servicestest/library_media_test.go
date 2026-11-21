package servicestest

import (
	"errors"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

// 假实现的媒体动作：默认成功并回写只有 ID 的条目；预设回复、调用记录、FailOn、闸门与进度订阅都按约定工作。
func TestLibraryMediaFake(t *testing.T) {
	ctx := t.Context()
	f := &Library{}

	res, err := f.TranscribeMedia(ctx, "a", true)
	if err != nil || !res.OK() || res.Item.ID != "a" {
		t.Fatalf("默认应成功：%+v %v", res, err)
	}
	f.SeedFormatTranscript(services.LibraryMediaActionResult{NotConfigured: true, Error: "no model"}, nil)
	if res, err = f.FormatTranscript(ctx, "a", true); err != nil || !res.NotConfigured || res.OK() {
		t.Fatalf("预设未配置：%+v %v", res, err)
	}
	f.SeedSummarizeMedia(services.LibraryMediaActionResult{}, errors.New("boom"))
	if _, err = f.SummarizeMedia(ctx, "a"); err == nil {
		t.Fatal("预设的调用失败应返回 error")
	}
	f.SeedRefreshForum(services.LibraryMediaActionResult{Item: &services.LibraryItem{ID: "a", Title: "T"}, Warning: "w"}, nil)
	if res, err = f.RefreshForumDiscussion(ctx, "a"); err != nil || res.Item.Title != "T" || res.Warning != "w" {
		t.Fatalf("预设成功：%+v %v", res, err)
	}
	f.FailOn(MediaMethodRefresh+":b", errors.New("only b"))
	if _, err = f.RefreshForumDiscussion(ctx, "b"); err == nil {
		t.Fatal("按参数注入的失败应生效")
	}

	want := []MediaCall{
		{Method: MediaMethodTranscribe, ItemID: "a", Diarize: true},
		{Method: MediaMethodFormat, ItemID: "a", AllowLong: true},
		{Method: MediaMethodSummarize, ItemID: "a"},
		{Method: MediaMethodRefresh, ItemID: "a"},
		{Method: MediaMethodRefresh, ItemID: "b"},
	}
	got := f.MediaCalls()
	if len(got) != len(want) {
		t.Fatalf("调用记录：%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 条调用记录：%+v，期望 %+v", i, got[i], want[i])
		}
	}

	// 闸门只卡一次
	release := f.HoldTranscribe()
	done := make(chan struct{})
	go func() {
		_, _ = f.TranscribeMedia(ctx, "a", false)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("闸门未放开前不该返回")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	<-done
	if _, err = f.TranscribeMedia(ctx, "a", false); err != nil {
		t.Fatalf("闸门只卡一次：%v", err)
	}

	// 进度订阅：两路各自触发，取消后不再回调且可重复取消
	var trans, fmts int
	unsub := f.SubscribeMediaProgress(
		func(services.LibraryTranscribeProgress) { trans++ },
		func(services.LibraryTranscriptFormatProgress) { fmts++ },
	)
	if f.MediaSubscribers() != 1 {
		t.Fatalf("订阅数：%d", f.MediaSubscribers())
	}
	f.EmitTranscribeProgress(services.LibraryTranscribeProgress{ItemID: "a", Stage: services.LibraryTranscribeStageFormatting})
	f.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: "a", Current: 1, Total: 2})
	unsub()
	unsub()
	f.EmitTranscribeProgress(services.LibraryTranscribeProgress{ItemID: "a"})
	f.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: "a"})
	if trans != 1 || fmts != 1 || f.MediaSubscribers() != 0 {
		t.Fatalf("trans=%d fmts=%d subs=%d", trans, fmts, f.MediaSubscribers())
	}
}
