package library

import (
	"context"
	"errors"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
)

// 本文件补 store_ai.go 的边角（M2-6c 变异检查的存活项）：入口的前置条件、过期请求的各条路径、唤醒界面与空通知器。
//
// 其中「列表选中项与详情条目不一致」「工具侧栏换代」在生产里由选中入口同步维护（commitSelectLocked / clearDetailLocked 同时改它们），
// 从界面走不到；这里用 desyncSelected / desyncItem / remountTools 直接改 Store 的状态来触发 showsLocked 与 current 的防御分支，
// 与 detail_shell_ui_test.go 的 setDetail 同一做法（只用在生产里界面走不到的状态上）。

// spawnLog 把后台动作记下来而不执行，由测试决定放行哪一个（Store.Spawn 的替身）。
type spawnLog struct{ fns []func() }

func (q *spawnLog) Spawn(fn func()) { q.fns = append(q.fns, fn) }

// desyncSelected 让列表选中项指向另一条，而详情仍是原来那条。
func (h *harness) desyncSelected(id string) {
	h.store.mu.Lock()
	h.store.view.List.SelectedID = id
	h.store.mu.Unlock()
}

// desyncItem 让详情里的条目换成另一个 id（列表选中项不动）。
func (h *harness) desyncItem(id string) {
	h.store.mu.Lock()
	item := *h.store.view.Detail.Item
	item.ID = id
	h.store.view.Detail.Item = &item
	h.store.mu.Unlock()
}

// remountTools 让工具侧栏换一代（等于重新挂载）：在途的请求回来发现代号变了。
func (h *harness) remountTools() {
	h.store.mu.Lock()
	h.store.resetToolsLocked()
	h.store.mu.Unlock()
}

// aiAction 是一项会发起后台请求的入口：摘要（note）或识别（img）。
type aiAction struct {
	name, id string
	start    func(h *harness)
	busy     func(h *harness) bool
	calls    string
	// applied 报告详情里的条目是否被写回的结果换掉了（摘要看 Summary，识别看正文）。
	applied func(h *harness) bool
}

func aiActions() []aiAction {
	return []aiAction{
		{"summary", "note", func(h *harness) { h.store.GenerateSummary(h.ctx) }, func(h *harness) bool { return h.tools().SummaryGenerating },
			"GenerateSummary", func(h *harness) bool { return h.detail().Item.Summary != "" || h.tools().SummaryExpandSeq != 0 }},
		{"ocr", "img", func(h *harness) { h.store.RecognizeImages(h.ctx) }, func(h *harness) bool { return h.tools().OcrRunning },
			"RecognizeImageText", func(h *harness) bool { return h.detail().Item.Content != imgContent }},
	}
}

// 详情读到之后、工具侧栏挂载之前（afterDetailLoaded 还排在后台）：摘要与识别都不能发起，也不置忙。
func TestAIEntryPointsWaitForTheToolsMount(t *testing.T) {
	for _, a := range aiActions() {
		t.Run(a.name, func(t *testing.T) {
			h := aiHarness(t)
			var q spawnLog
			h.store.Spawn = q.Spawn
			h.store.SelectItem(a.id)
			q.fns[0]() // 读取详情；挂载工具侧栏的后台动作排在它后面，还没放行
			if d := h.detail(); d.Item == nil || d.Item.ID != a.id || d.Tools.ItemID != "" {
				t.Fatalf("前提：详情已读到、工具侧栏还没挂载：%+v %+v", d.Item, d.Tools)
			}
			queued := len(q.fns)
			a.start(h)
			if a.busy(h) || len(q.fns) != queued || callsWithPrefix(h, a.calls) != 0 || len(h.queuedToasts()) != 0 {
				t.Fatalf("工具侧栏没挂载时不该发起：置忙 %v，后台动作 %d→%d", a.busy(h), queued, len(q.fns))
			}
			q.fns[1]() // 挂载之后同一个入口就能发起了
			a.start(h)
			if !a.busy(h) {
				t.Fatalf("挂载之后应能发起：%+v", h.tools())
			}
		})
	}
}

// 列表选中项与详情条目不一致（防御）：入口什么也不做。
func TestAIEntryPointsRequireTheListSelection(t *testing.T) {
	for _, a := range aiActions() {
		t.Run(a.name, func(t *testing.T) {
			h := aiHarness(t)
			h.open(a.id)
			h.desyncSelected("别的条目")
			a.start(h)
			if a.busy(h) || callsWithPrefix(h, a.calls) != 0 {
				t.Fatalf("选中项对不上不该发起：置忙 %v，调用 %v", a.busy(h), h.svc.Calls())
			}
		})
	}
}

// 发起时就唤醒界面（置忙后的「生成中」「识别中」要立刻画出来），不等后台动作。
func TestAIEntryPointsWakeTheUIWhenStarted(t *testing.T) {
	for _, a := range aiActions() {
		t.Run(a.name, func(t *testing.T) {
			h := aiHarness(t)
			h.open(a.id)
			q := &spawnQueue{}
			h.store.Spawn = q.Spawn
			before := h.wakeCount()
			a.start(h)
			if !a.busy(h) || h.wakeCount() <= before || callsWithPrefix(h, a.calls) != 0 {
				t.Fatalf("发起时应置忙并唤醒界面，后台动作还没跑：置忙 %v，唤醒 %d→%d", a.busy(h), before, h.wakeCount())
			}
			before = h.wakeCount()
			q.Run()
			if a.busy(h) || h.wakeCount() <= before {
				t.Fatalf("结果落定后应放下忙标记并唤醒界面：置忙 %v，唤醒 %d→%d", a.busy(h), before, h.wakeCount())
			}
		})
	}
}

// 在途期间详情不再显示这一条（选中项换了 / 详情里的条目换了 / 工具侧栏换代）：写回照样进服务端，但不碰详情与展开序号。
func TestAIResultOnlyTouchesTheDetailThatStillShowsTheItem(t *testing.T) {
	variants := []struct {
		name  string
		apply func(h *harness)
	}{
		{"选中项换了", func(h *harness) { h.desyncSelected("别的条目") }},
		{"详情里换成了别的条目", func(h *harness) { h.desyncItem("别的条目") }},
		{"工具侧栏换代", func(h *harness) { h.remountTools() }},
	}
	for _, a := range aiActions() {
		for _, v := range variants {
			t.Run(a.name+"/"+v.name, func(t *testing.T) {
				h := aiHarness(t)
				h.svc.SeedOcr(map[string]string{"a.png": "你好"}, nil, nil)
				h.open(a.id)
				q := &spawnQueue{}
				h.store.Spawn = q.Spawn
				a.start(h)
				v.apply(h)
				q.Run()
				if a.applied(h) {
					t.Fatalf("写回的结果不该落进详情：%+v %+v", h.detail().Item, h.tools())
				}
				if now, _ := h.svc.ItemNow(a.id); now.UpdatedAt == 1000 {
					t.Fatalf("写回应已进服务端：%+v", now)
				}
				if row := listRow(h, a.id); row.UpdatedAt == 1000 {
					t.Fatalf("列表行仍应跟上：%+v", row)
				}
			})
		}
	}
}

// 过期（换代）的未配置模型：只静默，不弹提示、不跳设置页、不留失败日志。
func TestAIStaleNotConfiguredIsSilent(t *testing.T) {
	cases := []struct {
		name  string
		seed  func(h *harness)
		id    string
		start func(h *harness)
	}{
		{"summary", func(h *harness) { h.svc.SeedSummary(services.LibrarySummaryResult{}, services.ErrAINotConfigured) }, "note",
			func(h *harness) { h.store.GenerateSummary(h.ctx) }},
		{"ocr", func(h *harness) { h.svc.SeedOcr(nil, nil, services.ErrAINotConfigured) }, "img",
			func(h *harness) { h.store.RecognizeImages(h.ctx) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := aiHarness(t)
			jumps := settingsJumps(h)
			c.seed(h)
			h.open(c.id)
			q := &spawnQueue{}
			h.store.Spawn = q.Spawn
			c.start(h)
			h.remountTools()
			q.Run()
			if len(h.queuedToasts()) != 0 || jumps.Load() != 0 || len(h.log.entries()) != 0 {
				t.Fatalf("过期的未配置模型不该提示或跳转：提示 %+v，跳转 %d，日志 %v", h.queuedToasts(), jumps.Load(), h.log.entries())
			}
		})
	}
}

// 空通知器（没有提示条的宿主）：截断警告不能因此崩溃；警告不带多余的详情。
func TestSummaryTruncatedWarningWithoutNotifier(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedSummary(services.LibrarySummaryResult{Text: "半篇", Truncated: true}, nil)
	h.open("note")
	h.store.GenerateSummary(h.ctx)
	if toast := onlyToast(t, h); toast.Kind != kit.ToastWarning || toast.Detail != "" {
		t.Fatalf("截断警告不带详情：%+v", toast)
	}

	h = aiHarness(t)
	h.svc.SeedSummary(services.LibrarySummaryResult{Text: "半篇", Truncated: true}, nil)
	h.open("note")
	h.store.guard.Notify = nil
	h.store.GenerateSummary(h.ctx) // 空通知器：不该 panic
	if now, _ := h.svc.ItemNow("note"); now.Summary != "半篇" {
		t.Fatalf("摘要照常写入：%+v", now)
	}
}

// 识别：全部没识别出文字（过期时不提示、也不放下新一代的忙标记；当前一代放下并可重试）。
func TestRecognizeImagesAllFailedStaleAndRetry(t *testing.T) {
	failAll := map[string]error{"a.png": errors.New("甲失败")}

	t.Run("过期：不提示、不动新一代的忙标记", func(t *testing.T) {
		h := aiHarness(t)
		h.svc.SeedOcr(nil, failAll, nil)
		h.open("img")
		q := &spawnQueue{}
		h.store.Spawn = q.Spawn
		h.store.RecognizeImages(h.ctx)
		h.remountTools()
		// 新一代自己有一次识别在途：旧一代落定不能把它的标记放下。
		h.store.mu.Lock()
		h.store.detail.tools.ocrRunning = true
		h.store.view.Detail.Tools.OcrRunning = true
		h.store.mu.Unlock()
		q.Run()
		if len(h.queuedToasts()) != 0 {
			t.Fatalf("过期的全失败不该弹提示：%+v", h.queuedToasts())
		}
		if !h.tools().OcrRunning {
			t.Fatal("旧一代落定不该放下新一代的忙标记（快照）")
		}
		h.store.mu.Lock()
		running := h.store.detail.tools.ocrRunning
		h.store.mu.Unlock()
		if !running {
			t.Fatal("旧一代落定不该放下新一代的忙标记（内部）")
		}
	})

	t.Run("当前一代：放下标记，可以重试", func(t *testing.T) {
		h := aiHarness(t)
		h.svc.SeedOcr(nil, failAll, nil)
		h.open("img")
		h.store.RecognizeImages(h.ctx)
		if toast := onlyToast(t, h); toast.Kind != kit.ToastError {
			t.Fatalf("提示：%+v", toast)
		}
		if h.tools().OcrRunning {
			t.Fatal("快照里的忙标记应放下")
		}
		h.store.RecognizeImages(h.ctx)
		if n := callsWithPrefix(h, "RecognizeImageText"); n != 2 {
			t.Fatalf("内部的在途标记也该放下，第二次点击应再识别一次，实际共 %d 次", n)
		}
	})

	t.Run("未配置模型：同样放下标记，可以重试", func(t *testing.T) {
		h := aiHarness(t)
		h.svc.SeedOcr(nil, nil, services.ErrAINotConfigured)
		h.open("img")
		h.store.RecognizeImages(h.ctx)
		h.store.RecognizeImages(h.ctx)
		if n := callsWithPrefix(h, "RecognizeImageText"); n != 2 {
			t.Fatalf("应能重试，实际共 %d 次", n)
		}
	})
}

// 识别：写回失败按「写回失败」处理，日志用同一个动作名；过期的写回失败只留日志。
func TestRecognizeImagesWriteBackFailureLogsOnceUnderTheAction(t *testing.T) {
	h := aiHarness(t)
	h.open("img")
	h.svc.FailOn("UpdateItemFields", errors.New("磁盘已满"))
	h.store.RecognizeImages(h.ctx)
	if got := h.loggedUnder("library.ocr"); len(got) != 1 {
		t.Fatalf("应恰好有一条动作名为 library.ocr 的日志：%v", h.log.entries())
	}

	stale := aiHarness(t)
	stale.open("img")
	q := &spawnQueue{}
	stale.store.Spawn = q.Spawn
	stale.svc.FailOn("UpdateItemFields", errors.New("磁盘已满"))
	stale.store.RecognizeImages(stale.ctx)
	stale.remountTools()
	q.Run()
	if len(stale.queuedToasts()) != 0 {
		t.Fatalf("过期的写回失败不该弹提示：%+v", stale.queuedToasts())
	}
	if got := stale.loggedUnder("library.ocr"); len(got) != 1 {
		t.Fatalf("过期的写回失败仍要留日志：%v", stale.log.entries())
	}
}

// 过期的摘要写回失败同样只留日志（现有用例覆盖的是生成失败）。
func TestSummaryStaleWriteBackFailureOnlyLogs(t *testing.T) {
	h := aiHarness(t)
	h.open("note")
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.svc.FailOn("UpdateItemFields", errors.New("磁盘已满"))
	h.store.GenerateSummary(h.ctx)
	h.remountTools()
	q.Run()
	if len(h.queuedToasts()) != 0 {
		t.Fatalf("过期的写回失败不该弹提示：%+v", h.queuedToasts())
	}
	if got := h.loggedUnder("library.aiSummary"); len(got) != 1 {
		t.Fatalf("应留日志：%v", h.log.entries())
	}
}

// syncContentBaseLocked：正文有待保存或在途保存的改动时不动基线；干净时同步；没有基线时什么也不做。
func TestSyncContentBaseLeavesInFlightContentAlone(t *testing.T) {
	h := aiHarness(t)
	h.open("img")
	h.edit(patchTitle("改过的标题")) // 记下基线
	updated := services.LibraryItem{ID: "img", Content: "服务端的新正文"}

	h.store.mu.Lock()
	h.store.detail.saving["img"] = patchContent("正在保存的正文")
	h.store.syncContentBaseLocked(updated)
	h.store.mu.Unlock()
	if b := h.baseContent("img"); b == nil || *b != imgContent {
		t.Fatalf("正文在途保存时不该动基线：%v", b)
	}

	h.store.mu.Lock()
	delete(h.store.detail.saving, "img")
	h.store.syncContentBaseLocked(updated)
	h.store.mu.Unlock()
	if b := h.baseContent("img"); b == nil || *b != "服务端的新正文" {
		t.Fatalf("干净时应同步基线：%v", b)
	}

	h.store.mu.Lock()
	h.store.syncContentBaseLocked(services.LibraryItem{ID: "没编辑过", Content: "x"})
	_, created := h.store.detail.bases["没编辑过"]
	h.store.mu.Unlock()
	if created {
		t.Fatal("没有基线的条目不该凭空建基线")
	}
}

// tagSpy 记下标签建议收到的标题与正文。
type tagSpy struct {
	services.Library
	mu             sync.Mutex
	title, content string
}

func (s *tagSpy) SuggestTags(ctx context.Context, title, content string) ([]string, error) {
	s.mu.Lock()
	s.title, s.content = title, content
	s.mu.Unlock()
	return s.Library.SuggestTags(ctx, title, content)
}

// 标签建议：标题与正文按顺序传给模型（服务端读到的版本）。
func TestSuggestItemTagsPassesTitleThenContent(t *testing.T) {
	h := aiHarness(t)
	spy := &tagSpy{Library: h.svc}
	h.store.svc = spy
	h.svc.SeedTagSuggestions([]string{"go"}, nil)
	if _, ok := h.store.SuggestItemTags(h.ctx, "note"); !ok {
		t.Fatal("应产出建议")
	}
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.title != "笔记" || spy.content != "笔记正文" {
		t.Fatalf("标题与正文传反或传错：%q %q", spy.title, spy.content)
	}
}

// 识别没有写回就收场（未配置模型、整批失败）时，也要放下忙标记并唤醒界面：这两条路不经写回，只有 releaseOcr 唤醒，不唤醒则按钮的忙态停在旧画面上。
func TestRecognizeImagesWakesTheUIWhenItEndsWithoutWriteBack(t *testing.T) {
	cases := []struct {
		name string
		seed func(h *harness)
	}{
		{"未配置模型", func(h *harness) { h.svc.SeedOcr(nil, nil, services.ErrAINotConfigured) }},
		{"整批失败", func(h *harness) { h.svc.SeedOcr(nil, map[string]error{"a.png": errors.New("甲失败")}, nil) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := aiHarness(t)
			c.seed(h)
			h.open("img")
			q := &spawnQueue{}
			h.store.Spawn = q.Spawn
			h.store.RecognizeImages(h.ctx)
			before := h.wakeCount()
			q.Run()
			if h.tools().OcrRunning || h.wakeCount() <= before {
				t.Fatalf("收场后应放下忙标记并唤醒界面：置忙 %v，唤醒 %d→%d", h.tools().OcrRunning, before, h.wakeCount())
			}
		})
	}
}