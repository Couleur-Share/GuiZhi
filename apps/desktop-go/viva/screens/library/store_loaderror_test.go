package library

import (
	"errors"
	"testing"

	"guizhi-mygo-shell/native/services"
)

// 读取失败的暴露规则（台账 B-036）：某份数据从没成功读到过、且最近一次读取失败时，View 里带失败原因；
// 一次成功读取清掉它；读到过之后的失败只留日志，不进 View，也不清已有的数据。

func TestFirstReadFailureIsExposedUntilARetrySucceeds(t *testing.T) {
	h := newHarness(t)
	h.svc.FailOn("Counts", errors.New("counts boom"))
	h.svc.FailOn("Collections", errors.New("collections boom"))
	h.svc.FailOn("Tags", errors.New("tags boom"))
	h.store.Load(h.ctx)
	v := h.store.View()
	if v.CountsError != "counts boom" || v.CollectionsError != "collections boom" || v.TagsError != "tags boom" {
		t.Fatalf("第一次读取全部失败：View 应带三个原因，实际 %q %q %q", v.CountsError, v.CollectionsError, v.TagsError)
	}
	if v.Counts != nil || len(v.Collections) != 0 || len(v.Tags) != 0 {
		t.Errorf("读取失败不该带数据：%+v", v)
	}

	// 再失败一次：原因跟着最近一次走。
	h.svc.FailOn("Counts", errors.New("counts boom again"))
	h.store.Load(h.ctx)
	if got := h.store.View().CountsError; got != "counts boom again" {
		t.Errorf("CountsError = %q，应是最近一次的原因", got)
	}

	// 一次成功的读取清掉对应的原因，其余照旧。
	h.svc.FailOn("Collections", nil)
	h.store.FetchCollections(h.ctx)
	v = h.store.View()
	if v.CollectionsError != "" || v.CountsError == "" || v.TagsError == "" {
		t.Errorf("合集读取成功后只该清掉合集的原因：%q %q %q", v.CountsError, v.CollectionsError, v.TagsError)
	}
}

// 失败也要唤醒界面重绘：错误态就是这次失败带出来的，不重绘用户看不到。
func TestFirstReadFailureWakesTheScreen(t *testing.T) {
	h := newHarness(t)
	h.svc.FailOn("Counts", errors.New("counts boom"))
	h.svc.FailOn("Collections", errors.New("collections boom"))
	h.svc.FailOn("Tags", errors.New("tags boom"))
	for name, fetch := range map[string]func(){
		"计数": func() { h.store.RefreshCounts(h.ctx) },
		"合集": func() { h.store.FetchCollections(h.ctx) },
		"标签": func() { h.store.FetchTags(h.ctx) },
	} {
		before := h.wakeCount()
		fetch()
		if h.wakeCount() == before {
			t.Errorf("%s读取失败之后没有唤醒界面重绘", name)
		}
	}
}

func TestFailuresAfterASuccessfulReadAreNotExposed(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.svc.FailOn("Counts", errors.New("counts boom"))
	h.svc.FailOn("Collections", errors.New("collections boom"))
	h.svc.FailOn("Tags", errors.New("tags boom"))
	h.store.Load(h.ctx)
	v := h.store.View()
	if v.CountsError != "" || v.CollectionsError != "" || v.TagsError != "" {
		t.Errorf("读到过之后的失败不该进 View：%q %q %q", v.CountsError, v.CollectionsError, v.TagsError)
	}
	if v.Counts == nil || len(v.Collections) != 2 || len(v.Tags) != 2 {
		t.Errorf("读到过之后的失败不该清掉已有的数据：%+v", v)
	}
	if got := len(h.log.entries()); got != 3 {
		t.Errorf("三次失败都应留日志，实际 %d 条：%v", got, h.log.entries())
	}
}

// 计数读取只认最后发出的那次：晚到的旧请求（成功或失败）都不该碰 CountsError。
func TestStaleCountsFailureDoesNotSetTheError(t *testing.T) {
	h := newHarness(t)
	h.svc.SetCounts(services.LibraryCounts{All: 1})
	h.svc.FailOn("Counts", errors.New("旧请求失败"))
	release := h.svc.HoldCounts()
	done := make(chan struct{})
	go func() { // 第一次请求带着失败卡住，随后被一次更新的、成功的请求超过
		h.store.RefreshCounts(h.ctx)
		close(done)
	}()
	waitFor(t, "第一次计数请求发出", func() bool { return len(h.svc.CountQueries()) == 1 })
	h.svc.FailOn("Counts", nil)
	h.store.RefreshCounts(h.ctx)
	release()
	<-done
	if v := h.store.View(); v.CountsError != "" || v.Counts == nil || v.Counts.All != 1 {
		t.Errorf("旧请求晚到的失败不该出现在 View 里，也不该盖掉新结果：CountsError = %q，Counts = %+v", v.CountsError, v.Counts)
	}
}
