package library

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件补 M2-4 Step 3 变异检查里存活过的几处：「已保存」的判定规则与定时器作废、卸载复位、附属状态在换条目与取消选中时的清空、
// 重试读取的中间态、采用来源版本的前置核对。每条用例都能区分对错的实现（对应的变异见开发文档 §8.1.1 的 M2-4 记录）。

// 判定规则（渲染层 effect 里的条件）：上一次观察到在保存中，现在没在保存、没失败、没有待保存。
func TestSavedTrackFinishesOnlyWhenASaveEndsCleanly(t *testing.T) {
	cases := []struct {
		name      string
		prev, cur savedTrack
		want      bool
	}{
		{"保存结束且什么都不剩", savedTrack{saving: true}, savedTrack{}, true},
		{"保存结束且待保存、错误都清了", savedTrack{saving: true, unsaved: true}, savedTrack{}, true},
		{"保存失败", savedTrack{saving: true}, savedTrack{saveError: "磁盘满"}, false},
		{"保存结束但还有待保存", savedTrack{saving: true}, savedTrack{unsaved: true}, false},
		{"保存失败且有待保存", savedTrack{saving: true}, savedTrack{unsaved: true, saveError: "磁盘满"}, false},
		{"没在保存过，只是待保存清空", savedTrack{unsaved: true}, savedTrack{}, false},
		{"没在保存过，只是失败原因清掉", savedTrack{saveError: "磁盘满"}, savedTrack{}, false},
		{"还在保存中，别的依赖变了", savedTrack{saving: true, unsaved: true}, savedTrack{saving: true}, false},
		{"一直没动", savedTrack{}, savedTrack{}, false},
	}
	for _, c := range cases {
		if got := c.prev.finishedTo(c.cur); got != c.want {
			t.Errorf("%s：%+v → %+v 判成 %v，应为 %v", c.name, c.prev, c.cur, got, c.want)
		}
	}
}

// 头部卸载（换条目、取消选中）丢掉观察记录：新头部第一次看到的干净状态不是「保存刚结束」。
func TestResetSavedDropsTheObservationAndTheLight(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	s := h.store
	s.mu.Lock()
	s.view.Detail.Saving = true
	s.syncSavedLocked() // 旧头部观察到在保存中
	s.resetSavedLocked()
	s.view.Detail.Saving = false
	s.syncSavedLocked() // 新头部第一次观察：干净状态
	lit := s.view.Detail.JustSaved
	s.mu.Unlock()
	if lit {
		t.Fatal("卸载之后观察记录应清空，新头部不该继承上一个头部「在保存中」的观察")
	}
}

// 作废的旧定时器（取消没拦住、回调已在锁后排队）拿到锁时不能把后来的提示灭掉。
func TestAStaleJustSavedTimerCannotPutOutALaterLight(t *testing.T) {
	h := detailHarness(t)
	h.store.AutoSave = func() bool { return false } // 手动保存，没有防抖定时器
	var timers []func()
	h.store.After = func(_ time.Duration, fn func()) func() {
		timers = append(timers, fn)
		return func() {} // 取消不掉
	}
	h.open("a")
	h.edit(patchTitle("一"))
	if !h.store.FlushPendingSave(h.ctx) || !h.detail().JustSaved || len(timers) != 1 {
		t.Fatalf("前提：保存之后亮并排上一个定时器：%+v %d", h.detail(), len(timers))
	}
	h.edit(patchTitle("二")) // 依赖变了，定时器作废
	timers[0]()             // 旧回调这时才拿到锁
	if !h.detail().JustSaved {
		t.Fatal("作废的旧定时器不该灭掉提示")
	}
}

// 换条目时新条目还在读取：上一条的来源版本、转写能力、已保存提示都不能留着；筛选把选中项挤出列表同理。
func TestDetailExtrasAreClearedWhenTheSelectionChangesOrGoesAway(t *testing.T) {
	h := detailHarness(t)
	h.svc.SeedSourceVersions("a", []services.LibrarySourceVersion{sourceVersion("v1", "旧", "旧正文", "", "[]", 1)})
	h.svc.SetDiarization(true)
	lit := func() DetailView {
		h.open("a")
		h.edit(patchTitle("甲改" + strings.Repeat("改", len(h.svc.Drafts()))))
		h.clock.Advance(autoSaveDebounce)
		d := h.detail()
		if !d.JustSaved || len(d.Revisions.Rows) != 1 || !d.CanDiarize {
			t.Fatalf("前提：详情带着来源版本、转写能力与已保存提示：%+v", d)
		}
		return d
	}

	lit()
	release := h.svc.HoldGet()
	done := make(chan struct{})
	go func() { h.store.SelectItem("b"); close(done) }()
	waitFor(t, "b 的读取发出", func() bool { return slices.Contains(h.svc.Gets(), "b") })
	d := h.detail()
	if !d.Loading || d.Item != nil || d.Revisions.ItemID != "" || len(d.Revisions.Rows) != 0 || d.Revisions.Error != "" || d.CanDiarize || d.JustSaved {
		t.Errorf("换条目读取中：附属状态应清空：%+v", d)
	}
	release()
	<-done

	lit()
	h.store.SetScope(h.ctx, services.LibraryScopeFavorites) // 选中项被挤出列表
	if d = h.detail(); d.Item != nil || d.Revisions.ItemID != "" || len(d.Revisions.Rows) != 0 || d.CanDiarize || d.JustSaved {
		t.Errorf("取消选中：附属状态应清空：%+v", d)
	}
}

// 重试读取来源版本：发出之后、答复回来之前，错误先清掉回到读取中。
func TestRetryRevisionsGoesBackToLoadingBeforeTheAnswerArrives(t *testing.T) {
	h := detailHarness(t)
	h.svc.FailOn("SourceVersions", errors.New("读取失败"))
	h.open("a")
	if r := h.detail().Revisions; r.Error != "读取失败" {
		t.Fatalf("前提：读取失败：%+v", r)
	}
	h.svc.FailOn("SourceVersions", nil)
	var queued []func()
	h.store.Spawn = func(fn func()) { queued = append(queued, fn) }
	h.store.RetryRevisions(h.ctx)
	if r := h.detail().Revisions; r.Error != "" || len(r.Rows) != 0 || r.ItemID != "a" {
		t.Fatalf("重试发出之后应回到读取中：%+v", r)
	}
	for _, fn := range queued {
		fn()
	}
	if r := h.detail().Revisions; r.Error != "" || r.ItemID != "a" {
		t.Fatalf("答复回来之后：%+v", r)
	}
}

// 采用来源版本的第一道核对：不是正在看的那一条（或选中了还在读取），原因是「请先保存当前草稿」（渲染层原文），并且不替当前条目落盘。
func TestAdoptForAnotherItemSaysSaveFirstAndDoesNotFlush(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("甲改")) // 防抖还没到，待保存里有一桶
	drafts := len(h.svc.Drafts())
	expectRefusal := func(what string) {
		t.Helper()
		logs := len(h.log.entries())
		if h.adopt("b", sourceVersion("v1", "新", "新正文", "", "[]", 1)) {
			t.Fatalf("%s不该采用", what)
		}
		entries := h.log.entries()
		if len(entries) != logs+1 || !strings.Contains(entries[len(entries)-1], locales.LibraryActionAdoptSource()) ||
			!strings.Contains(entries[len(entries)-1], "请先保存当前草稿") {
			t.Fatalf("%s：应留痕、原因是请先保存当前草稿：%v", what, entries[logs:])
		}
	}

	expectRefusal("不是正在看的那一条")
	if got := len(h.svc.Drafts()); got != drafts {
		t.Fatalf("不是这一条就不该替当前条目落盘：草稿保存 %d → %d 次", drafts, got)
	}

	// 选中了 b、详情还在读取：选中项对得上、详情还没到，同样不能采用。
	release := h.svc.HoldGet()
	done := make(chan struct{})
	go func() { h.store.SelectItem("b"); close(done) }()
	waitFor(t, "b 的读取发出", func() bool { return slices.Contains(h.svc.Gets(), "b") })
	expectRefusal("选中了还在读取的那一条")
	release()
	<-done
}
