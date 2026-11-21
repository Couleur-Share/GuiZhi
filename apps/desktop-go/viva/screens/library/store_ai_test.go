package library

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 AiSummaryCard（摘要生成与写回）、AiOcrCard（图中文字识别）、AiTagSuggest（标签建议）背后的状态。
// 条目：note、note2（普通笔记）、blank（正文空白）、img（一张配图）、img2（两张配图）、img-plain（图片类型但没有配图引用）、
// note-ref（笔记类型但正文里有配图引用）。

const (
	imgContent  = "文案\n\n![图](local-image://a.png)"
	img2Content = "![1](local-image://a.png)\n\n![2](local-image://b.png)"
)

func aiHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	entry := func(id, title, itemType string) services.LibraryEntry {
		return services.LibraryEntry{
			ID: id, Title: title, ItemType: itemType, Status: services.LibraryStatusActive,
			ReviewStatus: "clear", ReviewReasons: []string{}, UpdatedAt: 1000, CreatedAt: 1000, Tags: []services.LibraryTag{},
		}
	}
	h.svc.Seed(services.LibraryCounts{All: 7}, nil, nil)
	h.svc.SeedEntries([]services.LibraryEntry{
		entry("note", "笔记", services.LibraryTypeNote),
		entry("note2", "第二条", services.LibraryTypeNote),
		entry("blank", "空白", services.LibraryTypeNote),
		entry("img", "图片", services.LibraryTypeImage),
		entry("img2", "两张图", services.LibraryTypeImage),
		entry("img-plain", "无图", services.LibraryTypeImage),
		entry("note-ref", "笔记配图", services.LibraryTypeNote),
	})
	h.svc.SeedDetail("note", servicestest.DetailBody{Content: "笔记正文"})
	h.svc.SeedDetail("note2", servicestest.DetailBody{Content: "第二条正文"})
	h.svc.SeedDetail("blank", servicestest.DetailBody{Content: "  \n\t "})
	h.svc.SeedDetail("img", servicestest.DetailBody{Content: imgContent})
	h.svc.SeedDetail("img2", servicestest.DetailBody{Content: img2Content})
	h.svc.SeedDetail("img-plain", servicestest.DetailBody{Content: "只有文案"})
	h.svc.SeedDetail("note-ref", servicestest.DetailBody{Content: imgContent})
	h.store.Load(h.ctx)
	return h
}

// settingsJumps 记录跳转设置页 AI 分区的次数。
func settingsJumps(h *harness) *atomic.Int32 {
	var n atomic.Int32
	h.store.OnOpenSettingsAI = func() { n.Add(1) }
	return &n
}

// onlyToast 要求恰好弹了一条提示并返回它。
func onlyToast(t *testing.T, h *harness) kit.QueuedToast {
	t.Helper()
	toasts := h.queuedToasts()
	if len(toasts) != 1 {
		t.Fatalf("期望恰好一条提示：%+v", toasts)
	}
	return toasts[0]
}

func listRow(h *harness, id string) services.LibraryEntry {
	for _, e := range h.store.View().List.Entries {
		if e.ID == id {
			return e
		}
	}
	h.t.Fatalf("列表里没有 %s", id)
	return services.LibraryEntry{}
}

func (h *harness) baseContent(id string) *string {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	return h.store.detail.bases[id].Content
}

// ---- AI 摘要 ----

// 正常生成：写回摘要、列表行跟上、展开序号加一、成功提示；再生成一次序号继续加。
func TestGenerateSummaryWritesBackAndRequestsExpand(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedSummary(services.LibrarySummaryResult{Text: "摘要一"}, nil)
	h.open("note")
	h.store.GenerateSummary(h.ctx)
	d := h.detail()
	if d.Item == nil || d.Item.Summary != "摘要一" || d.Tools.SummaryExpandSeq != 1 || d.Tools.SummaryGenerating {
		t.Fatalf("详情：%+v %+v", d.Item, d.Tools)
	}
	if now, _ := h.svc.ItemNow("note"); now.Summary != "摘要一" || listRow(h, "note").UpdatedAt != now.UpdatedAt || now.UpdatedAt == 1000 {
		t.Fatalf("应写回服务端、列表行跟上：%+v 行 %+v", now, listRow(h, "note"))
	}
	if toast := onlyToast(t, h); toast.Kind != kit.ToastSuccess || toast.Message != locales.LibraryAiSummaryDone() {
		t.Fatalf("提示：%+v", toast)
	}
	h.svc.SeedSummary(services.LibrarySummaryResult{Text: "摘要二"}, nil)
	h.store.GenerateSummary(h.ctx)
	if d = h.detail(); d.Item.Summary != "摘要二" || d.Tools.SummaryExpandSeq != 2 {
		t.Fatalf("再生成：%+v %+v", d.Item, d.Tools)
	}
}

// 截断的摘要照常写入，但弹警告而不是成功。
func TestGenerateSummaryTruncatedWarns(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedSummary(services.LibrarySummaryResult{Text: "半篇", Truncated: true}, nil)
	h.open("note")
	h.store.GenerateSummary(h.ctx)
	if now, _ := h.svc.ItemNow("note"); now.Summary != "半篇" || h.detail().Item.Summary != "半篇" {
		t.Fatalf("截断的摘要也应写入：%+v", now)
	}
	if toast := onlyToast(t, h); toast.Kind != kit.ToastWarning || toast.Message != locales.LibraryAiSummaryTruncated() {
		t.Fatalf("提示：%+v", toast)
	}
}

// 正文空白（含未落盘的编辑清空了正文）：提示内容为空，不发请求、不置忙。
func TestGenerateSummaryRejectsBlankContent(t *testing.T) {
	h := aiHarness(t)
	h.open("blank")
	h.store.GenerateSummary(h.ctx)
	if toast := onlyToast(t, h); toast.Kind != kit.ToastError || toast.Message != locales.LibraryAiSummaryEmpty() {
		t.Fatalf("提示：%+v", toast)
	}
	if callsWithPrefix(h, "GenerateSummary") != 0 || h.detail().Tools.SummaryGenerating {
		t.Fatalf("不该发请求或置忙：%v %+v", h.svc.Calls(), h.detail().Tools)
	}

	h.open("note")
	h.edit(patchContent("   "))
	h.store.GenerateSummary(h.ctx)
	if callsWithPrefix(h, "GenerateSummary") != 0 || len(h.queuedToasts()) != 2 {
		t.Fatalf("草稿清空正文也该拦下：%v %+v", h.svc.Calls(), h.queuedToasts())
	}
}

// 没有详情时什么也不做。
func TestGenerateSummaryWithoutDetailDoesNothing(t *testing.T) {
	h := aiHarness(t)
	h.store.GenerateSummary(h.ctx)
	if callsWithPrefix(h, "GenerateSummary") != 0 || len(h.queuedToasts()) != 0 {
		t.Fatalf("不该有动作：%v %+v", h.svc.Calls(), h.queuedToasts())
	}
}

// 未配置模型：提示「尚未配置 AI 服务」并跳设置页；不写回、不留失败日志、忙标记放下。
func TestGenerateSummaryNotConfiguredOpensSettings(t *testing.T) {
	h := aiHarness(t)
	jumps := settingsJumps(h)
	h.svc.SeedSummary(services.LibrarySummaryResult{}, fmt.Errorf("包了一层：%w", services.ErrAINotConfigured))
	h.open("note")
	h.store.GenerateSummary(h.ctx)
	if toast := onlyToast(t, h); toast.Kind != kit.ToastError || toast.Message != locales.AskNotConfigured() || toast.Detail != "" {
		t.Fatalf("提示：%+v", toast)
	}
	if jumps.Load() != 1 || callsWithPrefix(h, "UpdateItemFields") != 0 || h.detail().Tools.SummaryGenerating || len(h.log.entries()) != 0 {
		t.Fatalf("跳转 %d 次，日志 %v，调用 %v", jumps.Load(), h.log.entries(), h.svc.Calls())
	}
}

// 生成失败：留日志、错误提示带可复制的原因；不写回、不展开、忙标记放下，可再次生成。
func TestGenerateSummaryFailureIsReported(t *testing.T) {
	h := aiHarness(t)
	jumps := settingsJumps(h)
	h.svc.SeedSummary(services.LibrarySummaryResult{}, errors.New("模型超时"))
	h.open("note")
	h.store.GenerateSummary(h.ctx)
	if toast := onlyToast(t, h); toast.Kind != kit.ToastError || toast.Message != locales.LibraryAiSummaryFailed("模型超时") || toast.Detail != "模型超时" {
		t.Fatalf("提示：%+v", toast)
	}
	d := h.detail()
	if !h.logged("library.aiSummary", "模型超时") || d.Tools.SummaryGenerating || d.Tools.SummaryExpandSeq != 0 || d.Item.Summary != "" || jumps.Load() != 0 {
		t.Fatalf("日志 %v，工具 %+v，跳转 %d", h.log.entries(), d.Tools, jumps.Load())
	}
	h.svc.SeedSummary(services.LibrarySummaryResult{Text: "好了"}, nil)
	h.store.GenerateSummary(h.ctx)
	if h.detail().Item.Summary != "好了" {
		t.Fatalf("失败后应能重试：%+v", h.detail().Item)
	}
}

// 写回失败：同样走失败提示，摘要不进详情、展开序号不动。
func TestGenerateSummaryWriteBackFailure(t *testing.T) {
	h := aiHarness(t)
	h.open("note")
	h.svc.FailOn("UpdateItemFields", errors.New("磁盘已满"))
	h.store.GenerateSummary(h.ctx)
	if toast := onlyToast(t, h); toast.Message != locales.LibraryAiSummaryFailed("磁盘已满") || toast.Detail != "磁盘已满" {
		t.Fatalf("提示：%+v", toast)
	}
	d := h.detail()
	if d.Item.Summary != "" || d.Tools.SummaryExpandSeq != 0 || d.Tools.SummaryGenerating || !h.logged("library.aiSummary", "磁盘已满") {
		t.Fatalf("详情：%+v %+v 日志 %v", d.Item, d.Tools, h.log.entries())
	}
}

// 未落盘的标题与正文草稿：生成用的是草稿，写回只带摘要，详情里的草稿原样保留、仍是待保存。
func TestGenerateSummaryKeepsUnsavedDraft(t *testing.T) {
	h := aiHarness(t)
	h.open("note")
	h.edit(patchTitle("改过的标题"))
	h.edit(patchContent("改过的正文"))
	h.store.GenerateSummary(h.ctx)
	d := h.detail()
	if d.Item.Title != "改过的标题" || d.Item.Content != "改过的正文" || !d.Unsaved {
		t.Fatalf("草稿应保留：%+v 未保存 %v", d.Item, d.Unsaved)
	}
	now, _ := h.svc.ItemNow("note")
	if now.Summary != "AI summary: 改过的标题" {
		t.Fatalf("应以草稿标题生成：%q", now.Summary)
	}
	if now.Title != "笔记" || now.Content != "笔记正文" {
		t.Fatalf("写回不该带草稿字段：%+v", now)
	}
	if d.Item.Summary != now.Summary {
		t.Fatalf("详情摘要：%q", d.Item.Summary)
	}
}

// 在途时再点被挡住，只发一次请求；落定后忙标记放下。
func TestGenerateSummaryIsNotReentrant(t *testing.T) {
	h := aiHarness(t)
	h.store.Spawn = func(fn func()) { go fn() }
	h.openAsync("note")
	release := h.svc.HoldSummary()
	h.store.GenerateSummary(h.ctx)
	if !h.tools().SummaryGenerating {
		t.Fatalf("生成中应亮：%+v", h.tools())
	}
	h.store.GenerateSummary(h.ctx)
	release()
	waitUntil(t, "摘要落定", func() bool { return !h.tools().SummaryGenerating && h.tools().SummaryExpandSeq == 1 })
	if n := callsWithPrefix(h, "GenerateSummary"); n != 1 {
		t.Fatalf("在途时再点应被挡住，实际请求 %d 次", n)
	}
	if n := callsWithPrefix(h, "UpdateItemFields"); n != 1 {
		t.Fatalf("写回应只有一次：%d", n)
	}
}

// 生成在途时换了条目：旧请求完成后不碰新条目的忙标记、展开序号与详情，也不弹提示；但服务端已写回、列表行跟上。
func TestGenerateSummaryResultForAReplacedItemIsDropped(t *testing.T) {
	h := aiHarness(t)
	h.store.Spawn = func(fn func()) { go fn() }
	jumps := settingsJumps(h)
	h.openAsync("note")
	release := h.svc.HoldSummary()
	h.store.GenerateSummary(h.ctx)
	h.openAsync("note2")
	release()
	waitUntil(t, "旧摘要写回", func() bool { n, _ := h.svc.ItemNow("note"); return n.Summary != "" })
	time.Sleep(20 * time.Millisecond)
	d := h.detail()
	if d.Item == nil || d.Item.ID != "note2" || d.Item.Summary != "" || d.Tools.ItemID != "note2" || d.Tools.SummaryGenerating || d.Tools.SummaryExpandSeq != 0 {
		t.Fatalf("旧结果不该落到新条目上：%+v %+v", d.Item, d.Tools)
	}
	if len(h.queuedToasts()) != 0 || jumps.Load() != 0 {
		t.Fatalf("过期的结果不该弹提示：%+v", h.queuedToasts())
	}
	if listRow(h, "note").UpdatedAt == 1000 {
		t.Fatalf("写回成功，列表行仍应跟上：%+v", listRow(h, "note"))
	}
	// 新条目可以立刻生成：旧请求没有占着它的在途标记。
	h.store.GenerateSummary(h.ctx)
	waitUntil(t, "新条目摘要", func() bool { return h.detail().Tools.SummaryExpandSeq == 1 })
}

// 过期请求失败：只留日志，不弹提示、不跳设置页。
func TestGenerateSummaryStaleFailureOnlyLogs(t *testing.T) {
	h := aiHarness(t)
	h.store.Spawn = func(fn func()) { go fn() }
	jumps := settingsJumps(h)
	h.svc.SeedSummary(services.LibrarySummaryResult{}, errors.New("旧条目的失败"))
	h.openAsync("note")
	release := h.svc.HoldSummary()
	h.store.GenerateSummary(h.ctx)
	h.openAsync("note2")
	release()
	waitUntil(t, "旧失败留痕", func() bool { return h.logged("library.aiSummary", "旧条目的失败") })
	if len(h.queuedToasts()) != 0 || jumps.Load() != 0 {
		t.Fatalf("过期的失败不该弹提示：%+v", h.queuedToasts())
	}
}

// ---- 图中文字识别 ----

// 单张图：识别文字作为「图中文字」小节追加到正文；列表行跟上、成功提示、忙标记放下。
func TestRecognizeImagesSingleImage(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedOcr(map[string]string{"a.png": "你好"}, nil, nil)
	h.open("img")
	h.store.RecognizeImages(h.ctx)
	want := notes.ApplyOcrTextToContent(imgContent, "你好")
	now, _ := h.svc.ItemNow("img")
	d := h.detail()
	if now.Content != want || d.Item.Content != want || d.Tools.OcrRunning {
		t.Fatalf("正文 %q，详情 %q，工具 %+v", now.Content, d.Item.Content, d.Tools)
	}
	if !strings.Contains(want, notes.OCRSectionHeading+"\n\n你好") || strings.Contains(want, "### ") {
		t.Fatalf("单图不加小标题：%q", want)
	}
	if listRow(h, "img").UpdatedAt != now.UpdatedAt {
		t.Fatalf("列表行应跟上：%+v", listRow(h, "img"))
	}
	if toast := onlyToast(t, h); toast.Kind != kit.ToastSuccess || toast.Message != locales.LibraryOcrDone() {
		t.Fatalf("提示：%+v", toast)
	}
}

// 已有「图中文字」小节再识别：整节替换，不重复追加。
func TestRecognizeImagesReplacesTheExistingSection(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedDetail("img", servicestest.DetailBody{Content: imgContent + "\n\n" + notes.OCRSectionHeading + "\n\n旧文字"})
	h.svc.SeedOcr(map[string]string{"a.png": "新文字"}, nil, nil)
	h.open("img")
	h.store.RecognizeImages(h.ctx)
	got := h.detail().Item.Content
	if strings.Count(got, notes.OCRSectionHeading) != 1 || strings.Contains(got, "旧文字") || !strings.HasSuffix(got, "新文字") {
		t.Fatalf("应整节替换：%q", got)
	}
}

// 多张图全部成功：每张加 `### 图 N` 小标题，成功提示。
func TestRecognizeImagesMultipleImages(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedOcr(map[string]string{"a.png": "甲", "b.png": "乙"}, nil, nil)
	h.open("img2")
	h.store.RecognizeImages(h.ctx)
	got := h.detail().Item.Content
	if !strings.HasSuffix(got, notes.OCRSectionHeading+"\n\n### 图 1\n\n甲\n\n### 图 2\n\n乙") {
		t.Fatalf("正文：%q", got)
	}
	if toast := onlyToast(t, h); toast.Kind != kit.ToastSuccess || toast.Message != locales.LibraryOcrDone() || toast.Detail != "" {
		t.Fatalf("提示：%+v", toast)
	}
}

// 部分失败：成功的写回、失败的留日志，警告带张数与逐张原因；失败的图不出现在小节里。
func TestRecognizeImagesPartialFailure(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedOcr(map[string]string{"a.png": "甲"}, map[string]error{"b.png": errors.New("识别超时")}, nil)
	h.open("img2")
	h.store.RecognizeImages(h.ctx)
	got := h.detail().Item.Content
	if !strings.Contains(got, "### 图 1\n\n甲") || strings.Contains(got, "### 图 2") {
		t.Fatalf("正文：%q", got)
	}
	toast := onlyToast(t, h)
	if toast.Kind != kit.ToastWarning || toast.Message != locales.LibraryOcrPartial(1) || toast.Detail != locales.LibraryImageIndex(2)+": 识别超时" {
		t.Fatalf("提示：%+v", toast)
	}
	if !h.logged("library.ocr", "识别超时") || h.tools().OcrRunning {
		t.Fatalf("日志 %v，工具 %+v", h.log.entries(), h.tools())
	}
}

// 全部失败：错误提示附逐张原因，不写回，忙标记放下。
func TestRecognizeImagesAllFailed(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedOcr(nil, map[string]error{"a.png": errors.New("甲失败"), "b.png": errors.New("乙失败")}, nil)
	h.open("img2")
	h.store.RecognizeImages(h.ctx)
	toast := onlyToast(t, h)
	wantDetail := locales.LibraryImageIndex(1) + ": 甲失败\n" + locales.LibraryImageIndex(2) + ": 乙失败"
	if toast.Kind != kit.ToastError || toast.Message != locales.LibraryOcrFailed(locales.LibraryOcrNoTextRecognized()) || toast.Detail != wantDetail {
		t.Fatalf("提示：%+v", toast)
	}
	if callsWithPrefix(h, "UpdateItemFields") != 0 || h.tools().OcrRunning || h.detail().Item.Content != img2Content {
		t.Fatalf("不该写回：%v %+v", h.svc.Calls(), h.tools())
	}
}

// 单张图失败：同样是全失败的提示，原因里带「图 1」。
func TestRecognizeImagesSingleImageFailed(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedOcr(nil, map[string]error{"a.png": errors.New("模型拒绝")}, nil)
	h.open("img")
	h.store.RecognizeImages(h.ctx)
	if toast := onlyToast(t, h); toast.Message != locales.LibraryOcrFailed(locales.LibraryOcrNoTextRecognized()) || toast.Detail != locales.LibraryImageIndex(1)+": 模型拒绝" {
		t.Fatalf("提示：%+v", toast)
	}
}

// 未配置视觉模型：立刻中止（后面的图不再请求）、提示未配置并跳设置页、不写回。
func TestRecognizeImagesNotConfiguredAbortsAndOpensSettings(t *testing.T) {
	h := aiHarness(t)
	jumps := settingsJumps(h)
	h.svc.SeedOcr(nil, nil, services.ErrAINotConfigured)
	h.open("img2")
	h.store.RecognizeImages(h.ctx)
	if n := callsWithPrefix(h, "RecognizeImageText"); n != 1 {
		t.Fatalf("应在第一张就中止，实际请求 %d 次", n)
	}
	if toast := onlyToast(t, h); toast.Kind != kit.ToastError || toast.Message != locales.LibraryOcrNotConfigured() {
		t.Fatalf("提示：%+v", toast)
	}
	if jumps.Load() != 1 || callsWithPrefix(h, "UpdateItemFields") != 0 || h.tools().OcrRunning {
		t.Fatalf("跳转 %d，调用 %v，工具 %+v", jumps.Load(), h.svc.Calls(), h.tools())
	}
}

// 不是图片类型、或没有本地配图引用的条目：什么也不做。
func TestRecognizeImagesIgnoresItemsWithoutLocalImages(t *testing.T) {
	h := aiHarness(t)
	for _, id := range []string{"note", "note-ref", "img-plain"} {
		h.open(id)
		h.store.RecognizeImages(h.ctx)
		if h.tools().OcrRunning {
			t.Fatalf("%s 不该置忙", id)
		}
	}
	h.store.SelectItem("")
	h.store.RecognizeImages(h.ctx)
	if callsWithPrefix(h, "RecognizeImageText") != 0 || len(h.queuedToasts()) != 0 {
		t.Fatalf("不该有动作：%v %+v", h.svc.Calls(), h.queuedToasts())
	}
}

// 写回失败：错误提示带原因，详情正文不变，忙标记放下。
func TestRecognizeImagesWriteBackFailure(t *testing.T) {
	h := aiHarness(t)
	h.open("img")
	h.svc.FailOn("UpdateItemFields", errors.New("磁盘已满"))
	h.store.RecognizeImages(h.ctx)
	if toast := onlyToast(t, h); toast.Kind != kit.ToastError || toast.Message != locales.LibraryOcrFailed("磁盘已满") || toast.Detail != "磁盘已满" {
		t.Fatalf("提示：%+v", toast)
	}
	if h.detail().Item.Content != imgContent || h.tools().OcrRunning || !h.logged("library.ocr", "磁盘已满") {
		t.Fatalf("详情 %q，工具 %+v，日志 %v", h.detail().Item.Content, h.tools(), h.log.entries())
	}
}

// 没有未保存的正文改动：正文基线同步成服务端版本（保存别的字段时带的基线不过期），草稿标题保留。
func TestRecognizeImagesSyncsTheContentBaseWhenContentIsClean(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedOcr(map[string]string{"a.png": "你好"}, nil, nil)
	h.open("img")
	h.edit(patchTitle("新标题"))
	if b := h.baseContent("img"); b == nil || *b != imgContent {
		t.Fatalf("前提：基线正文应是编辑起点的：%v", b)
	}
	h.store.RecognizeImages(h.ctx)
	want := notes.ApplyOcrTextToContent(imgContent, "你好")
	if b := h.baseContent("img"); b == nil || *b != want {
		t.Fatalf("基线正文应同步为服务端版本：%v", b)
	}
	d := h.detail()
	if d.Item.Title != "新标题" || d.Item.Content != want || !d.Unsaved {
		t.Fatalf("详情：%+v 未保存 %v", d.Item, d.Unsaved)
	}
	if !h.store.FlushPendingSave(h.ctx) {
		t.Fatalf("随后的保存应成功：%+v", h.detail())
	}
	if drafts := h.svc.Drafts(); len(drafts) != 1 || drafts[0].Base.Content == nil || *drafts[0].Base.Content != want {
		t.Fatalf("保存带的基线正文应是服务端版本：%+v", drafts)
	}
	if now, _ := h.svc.ItemNow("img"); now.Title != "新标题" || now.Content != want {
		t.Fatalf("服务端：%+v", now)
	}
}

// 有未保存的正文改动：不动基线、详情里保留草稿正文；识别结果基于发起时的草稿正文写进服务端。
func TestRecognizeImagesKeepsUnsavedContentDraft(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedOcr(map[string]string{"a.png": "你好"}, nil, nil)
	h.open("img")
	draft := imgContent + "\n\n补充一句"
	h.edit(patchContent(draft))
	h.store.RecognizeImages(h.ctx)
	if b := h.baseContent("img"); b == nil || *b != imgContent {
		t.Fatalf("基线正文不该动：%v", b)
	}
	d := h.detail()
	if d.Item.Content != draft || !d.Unsaved {
		t.Fatalf("草稿正文应保留：%q 未保存 %v", d.Item.Content, d.Unsaved)
	}
	if now, _ := h.svc.ItemNow("img"); now.Content != notes.ApplyOcrTextToContent(draft, "你好") {
		t.Fatalf("服务端应基于草稿正文：%q", now.Content)
	}
}

// 在途时再点被挡住，只识别一次。
func TestRecognizeImagesIsNotReentrant(t *testing.T) {
	h := aiHarness(t)
	h.store.Spawn = func(fn func()) { go fn() }
	h.openAsync("img")
	release := h.svc.HoldOcr()
	h.store.RecognizeImages(h.ctx)
	if !h.tools().OcrRunning {
		t.Fatalf("识别中应亮：%+v", h.tools())
	}
	h.store.RecognizeImages(h.ctx)
	release()
	waitUntil(t, "识别落定", func() bool { return !h.tools().OcrRunning && len(h.queuedToasts()) == 1 })
	if n := callsWithPrefix(h, "RecognizeImageText"); n != 1 {
		t.Fatalf("在途时再点应被挡住，实际请求 %d 次", n)
	}
}

// 识别在途时换了条目：旧请求完成后不碰新条目的忙标记与详情、不弹提示；服务端已写回、列表行跟上。
func TestRecognizeImagesResultForAReplacedItemIsDropped(t *testing.T) {
	h := aiHarness(t)
	h.store.Spawn = func(fn func()) { go fn() }
	h.openAsync("img")
	release := h.svc.HoldOcr()
	h.store.RecognizeImages(h.ctx)
	h.openAsync("note")
	release()
	waitUntil(t, "旧识别写回", func() bool {
		n, _ := h.svc.ItemNow("img")
		return strings.Contains(n.Content, notes.OCRSectionHeading)
	})
	time.Sleep(20 * time.Millisecond)
	d := h.detail()
	if d.Item == nil || d.Item.ID != "note" || d.Item.Content != "笔记正文" || d.Tools.ItemID != "note" || d.Tools.OcrRunning {
		t.Fatalf("旧结果不该落到新条目上：%+v %+v", d.Item, d.Tools)
	}
	if len(h.queuedToasts()) != 0 {
		t.Fatalf("过期的结果不该弹提示：%+v", h.queuedToasts())
	}
	if listRow(h, "img").UpdatedAt == 1000 {
		t.Fatalf("写回成功，列表行仍应跟上：%+v", listRow(h, "img"))
	}
}

// ---- 标签建议 ----

// 正常返回候选标签；用的是服务端的标题与正文。
func TestSuggestItemTagsReturnsCandidates(t *testing.T) {
	h := aiHarness(t)
	h.svc.SeedTagSuggestions([]string{"go", "rust"}, nil)
	tags, ok := h.store.SuggestItemTags(h.ctx, "note")
	if !ok || strings.Join(tags, ",") != "go,rust" {
		t.Fatalf("结果：%v %v", tags, ok)
	}
	if callsWithPrefix(h, "SuggestTags") != 1 || len(h.queuedToasts()) != 0 {
		t.Fatalf("调用 %v，提示 %+v", h.svc.Calls(), h.queuedToasts())
	}
	// 模型给了空列表也算产出（浮层据此提示「建议的标签都已存在」）。
	h.svc.SeedTagSuggestions(nil, nil)
	if tags, ok = h.store.SuggestItemTags(h.ctx, "note"); !ok || len(tags) != 0 {
		t.Fatalf("空列表：%v %v", tags, ok)
	}
}

// 条目不存在：静默返回，不请求模型、不弹提示。
func TestSuggestItemTagsMissingItemIsSilent(t *testing.T) {
	h := aiHarness(t)
	tags, ok := h.store.SuggestItemTags(h.ctx, "nope")
	if ok || tags != nil || callsWithPrefix(h, "SuggestTags") != 0 || len(h.queuedToasts()) != 0 || len(h.log.entries()) != 0 {
		t.Fatalf("结果 %v %v，调用 %v，提示 %+v", tags, ok, h.svc.Calls(), h.queuedToasts())
	}
}

// 未配置模型：提示并跳设置页，不留失败日志。
func TestSuggestItemTagsNotConfiguredOpensSettings(t *testing.T) {
	h := aiHarness(t)
	jumps := settingsJumps(h)
	h.svc.SeedTagSuggestions(nil, services.ErrAINotConfigured)
	tags, ok := h.store.SuggestItemTags(h.ctx, "note")
	if ok || tags != nil || jumps.Load() != 1 || len(h.log.entries()) != 0 {
		t.Fatalf("结果 %v %v，跳转 %d，日志 %v", tags, ok, jumps.Load(), h.log.entries())
	}
	if toast := onlyToast(t, h); toast.Kind != kit.ToastError || toast.Message != locales.AskNotConfigured() {
		t.Fatalf("提示：%+v", toast)
	}
}

// 失败：留日志，错误提示带原因与可复制详情；读取条目失败同样处理。
func TestSuggestItemTagsFailureIsReported(t *testing.T) {
	h := aiHarness(t)
	jumps := settingsJumps(h)
	h.svc.SeedTagSuggestions(nil, errors.New("模型出错"))
	if tags, ok := h.store.SuggestItemTags(h.ctx, "note"); ok || tags != nil {
		t.Fatalf("结果：%v %v", tags, ok)
	}
	if toast := onlyToast(t, h); toast.Kind != kit.ToastError || toast.Message != locales.LibraryAiTagsFailed("模型出错") || toast.Detail != "模型出错" {
		t.Fatalf("提示：%+v", toast)
	}
	if !h.logged("library.suggestTags", "模型出错") || jumps.Load() != 0 {
		t.Fatalf("日志 %v，跳转 %d", h.log.entries(), jumps.Load())
	}

	h.svc.FailOn("GetItem:note", errors.New("读取失败"))
	if _, ok := h.store.SuggestItemTags(h.ctx, "note"); ok {
		t.Fatal("读取条目失败应返回 false")
	}
	if toasts := h.queuedToasts(); len(toasts) != 2 || toasts[1].Message != locales.LibraryAiTagsFailed("读取失败") {
		t.Fatalf("提示：%+v", toasts)
	}
	if callsWithPrefix(h, "SuggestTags") != 1 {
		t.Fatalf("读取失败时不该请求模型：%v", h.svc.Calls())
	}
}

// 写回时条目已被删除（UpdateItemFields 返回 found=false, err=nil）：摘要与识别都必须按失败提示并留日志，不能报成功。
func TestAIWriteBackMissingItemReportsFailure(t *testing.T) {
	t.Run("summary", func(t *testing.T) {
		h := aiHarness(t)
		h.store.Spawn = func(fn func()) { go fn() }
		h.openAsync("note")
		release := h.svc.HoldSummary()
		h.store.GenerateSummary(h.ctx)
		h.svc.SeedEntries(nil)
		release()
		waitUntil(t, "摘要写回失败提示", func() bool { return !h.detail().Tools.SummaryGenerating && len(h.queuedToasts()) > 0 })
		toast := onlyToast(t, h)
		if toast.Kind != kit.ToastError || !strings.Contains(toast.Message, "条目不存在或已被删除") {
			t.Fatalf("应报错误提示，实际：%+v", toast)
		}
		if !h.logged("library.aiSummary", "条目不存在或已被删除") || h.detail().Tools.SummaryExpandSeq != 0 {
			t.Fatalf("应留日志且不递增展开序号：日志=%v seq=%d", h.log.entries(), h.detail().Tools.SummaryExpandSeq)
		}
	})

	t.Run("ocr", func(t *testing.T) {
		h := aiHarness(t)
		h.store.Spawn = func(fn func()) { go fn() }
		h.openAsync("img")
		release := h.svc.HoldOcr()
		h.store.RecognizeImages(h.ctx)
		h.svc.SeedEntries(nil)
		release()
		waitUntil(t, "识别写回失败提示", func() bool { return !h.detail().Tools.OcrRunning && len(h.queuedToasts()) > 0 })
		toast := onlyToast(t, h)
		if toast.Kind != kit.ToastError || !strings.Contains(toast.Message, "条目不存在或已被删除") {
			t.Fatalf("应报错误提示，实际：%+v", toast)
		}
		if !h.logged("library.ocr", "条目不存在或已被删除") {
			t.Fatalf("应留日志：%v", h.log.entries())
		}
	})
}
