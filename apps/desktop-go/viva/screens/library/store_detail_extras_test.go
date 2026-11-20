package library

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层详情的附属状态：ItemDetailHeader 的「已保存」提示、SourceCaptureRevisions 的读取与采用、
// ReviewRequiredNotice 的复核确认、useTranscriptActions 的 canDiarize。条目同 detailHarness 的 a、b、c。

// reactJustSavedWindow 是渲染层 setTimeout 里写死的 2500 毫秒——测试不引用实现里的常量，免得常量改了测试跟着改而放过。
const reactJustSavedWindow = 2500 * time.Millisecond

func (h *harness) queuedToasts() []kit.QueuedToast { return h.store.guard.Notify.Queued() }

// ---- 已保存提示 ----
//
// 规则照渲染层 ItemDetailHeader 的 effect：依赖 (isSaving, saveError, hasUnsavedChanges) 任何一个变化都先收起 2.5 秒定时器，
// 再看「上一次在保存中、现在没在保存、没失败、没待保存」就亮并重新计时；justSaved 只被定时器到点或换条目熄灭。

// 保存完成（没失败、没有待保存）后亮「已保存」2.5 秒，然后灭；没保存过不亮。
func TestJustSavedLightsUpForTheWindowAfterASave(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	if h.detail().JustSaved {
		t.Fatal("没保存过不该亮")
	}
	h.edit(patchTitle("甲改"))
	if h.detail().JustSaved {
		t.Fatal("只是输入了还没保存，不该亮")
	}
	h.clock.Advance(autoSaveDebounce)
	if d := h.detail(); !d.JustSaved || d.Unsaved || d.Saving || d.SaveError != "" {
		t.Fatalf("保存完应亮「已保存」：%+v", d)
	}
	h.clock.Advance(reactJustSavedWindow - time.Millisecond)
	if !h.detail().JustSaved {
		t.Fatal("2.5 秒之前不该灭")
	}
	h.clock.Advance(time.Millisecond)
	if h.detail().JustSaved {
		t.Fatal("2.5 秒到了应灭")
	}
}

// 提示亮着时又输入了：未落盘状态一变，定时器就被收起，但「亮着」本身不变——不会自己灭，
// 直到下一次保存结束再过 2.5 秒（渲染层 effect 的清理函数只清定时器；头部另外要求没有未落盘的编辑才画出来）。
func TestJustSavedStaysLitOnceItsTimerIsCancelledByAnEdit(t *testing.T) {
	h := detailHarness(t)
	h.store.AutoSave = func() bool { return false } // 手动保存，省掉防抖定时器的干扰
	h.open("a")
	h.edit(patchTitle("一"))
	if !h.store.FlushPendingSave(h.ctx) || !h.detail().JustSaved {
		t.Fatalf("第一次保存之后应亮：%+v", h.detail())
	}
	h.edit(patchTitle("二"))
	h.clock.Advance(10 * time.Second)
	if d := h.detail(); !d.JustSaved || !d.Unsaved {
		t.Fatalf("输入收起了定时器，亮着的状态不该变：%+v", d)
	}
	if !h.store.FlushPendingSave(h.ctx) {
		t.Fatal("第二次保存应成功")
	}
	h.clock.Advance(reactJustSavedWindow - time.Millisecond)
	if !h.detail().JustSaved {
		t.Fatal("第二次保存结束后应重新计时")
	}
	h.clock.Advance(time.Millisecond)
	if h.detail().JustSaved {
		t.Fatal("重新计时的 2.5 秒到了应灭")
	}
}

// 第一次的定时器不会灭掉第二次保存之后的提示；切到别的条目立刻灭，旧的定时器不会再把它点亮或在新条目上乱灭。
func TestJustSavedRestartsAndResetsWhenTheItemChanges(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("一"))
	h.clock.Advance(autoSaveDebounce)
	h.clock.Advance(2 * time.Second)
	h.edit(patchTitle("二"))
	h.clock.Advance(autoSaveDebounce) // 第二次保存，重新计时
	h.clock.Advance(reactJustSavedWindow - time.Millisecond)
	if !h.detail().JustSaved {
		t.Fatal("第二次保存之后应重新计时，不该被第一次的定时器灭掉")
	}
	h.open("b")
	if h.detail().JustSaved {
		t.Fatal("切到别的条目应灭")
	}
	h.clock.Advance(10 * time.Second)
	if h.detail().JustSaved {
		t.Fatal("旧的定时器不该把它点亮")
	}
}

// 保存失败不亮；重试成功才亮。保存在途时又输入了，这一次保存结束时还有待保存，也不亮，全部落盘后才亮。
func TestJustSavedIsSkippedOnFailureAndWhileEditsArePending(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.svc.FailDraft("磁盘满")
	h.edit(patchTitle("甲改"))
	h.clock.Advance(autoSaveDebounce)
	if d := h.detail(); d.JustSaved || d.SaveError != "磁盘满" {
		t.Fatalf("失败不该亮：%+v", d)
	}
	h.svc.FailDraft("")

	first := h.svc.HoldSave()
	flushed := make(chan bool)
	go func() { flushed <- h.store.FlushPendingSave(h.ctx) }()
	waitFor(t, "第一次保存发出", func() bool { return len(h.svc.Drafts()) == 2 })
	h.edit(patchTitle("甲再改"))
	second := h.svc.HoldSave()
	first()
	waitFor(t, "第二次保存发出", func() bool { return len(h.svc.Drafts()) == 3 })
	if d := h.detail(); d.JustSaved || !d.Saving {
		t.Fatalf("还有待保存时不该亮：%+v", d)
	}
	second()
	if !<-flushed || !h.detail().JustSaved {
		t.Fatalf("全部落盘之后应亮：%+v", h.detail())
	}
}

// 换条目、取消选中都让头部卸载：观察记录清空，新条目上第一次观察不算「保存刚结束」。
func TestJustSavedStartsFreshWhenTheHeaderIsRemounted(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("甲改"))
	h.clock.Advance(autoSaveDebounce)
	if !h.detail().JustSaved {
		t.Fatal("先亮")
	}
	h.store.SelectItem("")
	if d := h.detail(); d.JustSaved || d.Item != nil {
		t.Fatalf("取消选中应灭：%+v", d)
	}
	h.open("b")
	if h.detail().JustSaved {
		t.Fatal("重新挂载的头部不该一上来就亮")
	}
}

// AutoSaveEnabled 跟着设置：没接线按开着，接了就听它的。
func TestAutoSaveEnabledFollowsTheSetting(t *testing.T) {
	h := newHarness(t)
	if !h.store.AutoSaveEnabled() {
		t.Fatal("没接线应按开着")
	}
	on := false
	h.store.AutoSave = func() bool { return on }
	if h.store.AutoSaveEnabled() {
		t.Fatal("设置关着应为关")
	}
	on = true
	if !h.store.AutoSaveEnabled() {
		t.Fatal("设置开着应为开")
	}
}

// ---- 来源版本 ----

func sourceVersion(id, title, content, transcript, review string, at int64) services.LibrarySourceVersion {
	return services.LibrarySourceVersion{ID: id, Title: title, Content: content, Transcript: transcript, ReviewJSON: review, CapturedAt: at}
}

// 读到条目之后读它的来源版本；切到没有版本的条目是空的；失败记下原因（原文不去空白，空原因换成固定文案），重试能恢复。
func TestSourceRevisionsLoadWithTheDetailAndRetry(t *testing.T) {
	h := detailHarness(t)
	h.svc.SeedSourceVersions("a", []services.LibrarySourceVersion{sourceVersion("v2", "新", "新正文", "", "[]", 2), sourceVersion("v1", "旧", "旧正文", "", "[]", 1)})
	h.open("a")
	r := h.detail().Revisions
	if r.ItemID != "a" || len(r.Rows) != 2 || r.Rows[0].ID != "v2" || r.Error != "" {
		t.Fatalf("a 的来源版本：%+v", r)
	}
	h.open("b")
	if r = h.detail().Revisions; r.ItemID != "b" || len(r.Rows) != 0 || r.Error != "" {
		t.Fatalf("b 没有来源版本：%+v", r)
	}

	h.svc.FailOn("SourceVersions", errors.New("读取失败"))
	h.open("a")
	if r = h.detail().Revisions; r.ItemID != "a" || len(r.Rows) != 0 || r.Error != "读取失败" {
		t.Fatalf("失败：%+v", r)
	}
	if !h.logged("library.sourceVersions", "读取失败") {
		t.Fatalf("后台失败应留痕：%v", h.log.entries())
	}
	h.svc.FailOn("SourceVersions", errors.New(""))
	h.store.RetryRevisions(h.ctx)
	if r = h.detail().Revisions; r.Error != locales.LibrarySourceRevisionsLoadFailed() {
		t.Fatalf("空原因应换成固定文案：%q", r.Error)
	}
	h.svc.FailOn("SourceVersions", nil)
	h.store.RetryRevisions(h.ctx)
	if r = h.detail().Revisions; r.Error != "" || len(r.Rows) != 2 {
		t.Fatalf("重试之后：%+v", r)
	}
}

// 没有详情时重试什么也不做；晚到的旧请求（序号变了）不覆盖新条目的来源版本。
func TestSourceRevisionsIgnoreStaleAnswersAndRetryWithoutDetail(t *testing.T) {
	h := detailHarness(t)
	h.store.RetryRevisions(h.ctx)
	if slices.ContainsFunc(h.svc.Calls(), func(c string) bool { return strings.HasPrefix(c, "SourceVersions:") }) {
		t.Fatalf("没有详情时不该读：%v", h.svc.Calls())
	}
	h.svc.SeedSourceVersions("a", []services.LibrarySourceVersion{sourceVersion("v1", "旧", "旧正文", "", "[]", 1)})
	h.open("b")
	h.store.mu.Lock()
	seq := h.store.detail.seq
	h.store.mu.Unlock()
	h.store.loadRevisions(h.ctx, "a", seq-1)
	if r := h.detail().Revisions; r.ItemID != "b" || len(r.Rows) != 0 {
		t.Fatalf("旧请求不该覆盖：%+v", r)
	}
}

// ---- 采用来源版本 ----

func (h *harness) adopt(id string, v services.LibrarySourceVersion) bool {
	return h.store.AdoptSourceVersion(h.ctx, id, v)
}

// 完整版本（没有缺口）：正文进草稿并排定保存，不动复核状态；版本正文为空时取文字稿。
func TestAdoptASourceVersionPutsItsContentIntoTheDraft(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	if !h.adopt("a", sourceVersion("v1", "新", "新正文", "稿", "[]", 1)) {
		t.Fatal("采用应成功")
	}
	d := h.detail()
	if d.Item.Content != "新正文" || !d.Unsaved || len(h.svc.UpdateCalls()) != 0 {
		t.Fatalf("正文进草稿、不碰复核：%+v %v", d, h.svc.UpdateCalls())
	}
	h.clock.Advance(autoSaveDebounce)
	if got, _ := h.svc.ItemNow("a"); got.Content != "新正文" {
		t.Fatalf("应落盘：%q", got.Content)
	}
	if !h.adopt("a", sourceVersion("v2", "无正文", "", "只有文字稿", "[]", 2)) || h.detail().Item.Content != "只有文字稿" {
		t.Fatalf("正文为空应取文字稿：%q", h.detail().Item.Content)
	}
}

// 残缺的版本（带缺口原因）：先把条目标成待复核并带上原因，再进草稿。
func TestAdoptAnIncompleteVersionMarksTheItemForReviewFirst(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	if !h.adopt("a", sourceVersion("v1", "新", "残缺正文", "", `["正文过短","缺少封面"]`, 1)) {
		t.Fatal("采用应成功")
	}
	calls := h.svc.UpdateCalls()
	if len(calls) != 1 || calls[0].ID != "a" || calls[0].Patch.ReviewStatus == nil || *calls[0].Patch.ReviewStatus != services.LibraryReviewNeedsReview ||
		!slices.Equal(calls[0].Patch.ReviewReasons, []string{"正文过短", "缺少封面"}) {
		t.Fatalf("应先标成待复核：%+v", calls)
	}
	d := h.detail()
	if d.Item.ReviewStatus != services.LibraryReviewNeedsReview || d.Item.Content != "残缺正文" {
		t.Fatalf("详情应带待复核与新正文：%+v", d.Item)
	}
}

// 当前草稿落不了盘就不采用；不是正在看的那一条也不采用；缺口原因不是合法 JSON 同样失败；都走变更失败通道。
func TestAdoptRefusesWhenTheDraftCannotBeSavedOrTheItemIsNotShown(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("甲改"))
	h.svc.FailDraft("磁盘满")
	if h.adopt("a", sourceVersion("v1", "新", "新正文", "", "[]", 1)) {
		t.Fatal("草稿落不了盘时不该采用")
	}
	if !h.logged(locales.LibraryActionAdoptSource(), "请先保存当前草稿") || h.detail().Item.Content != "甲的正文" {
		t.Fatalf("应说明原因且正文不变：%v %q", h.log.entries(), h.detail().Item.Content)
	}
	h.svc.FailDraft("")
	if h.adopt("b", sourceVersion("v1", "新", "新正文", "", "[]", 1)) || h.detail().Item.Content != "甲的正文" {
		t.Fatal("不是正在看的那一条不该采用")
	}
	if h.adopt("a", sourceVersion("v1", "新", "新正文", "", "不是 JSON", 1)) {
		t.Fatal("缺口原因解析失败应报错")
	}
	if len(h.svc.UpdateCalls()) != 0 {
		t.Fatalf("失败时不该改复核状态：%v", h.svc.UpdateCalls())
	}
}

// 标待复核之后条目已被删掉：报「条目已删除」；标记期间正文又被改了：停下，保留来源版本、不覆盖我刚输入的。
func TestAdoptStopsWhenTheItemVanishesOrTheContentChanges(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.svc.FailOn("UpdateEntry", errors.New("写入失败"))
	if h.adopt("a", sourceVersion("v1", "新", "残缺正文", "", `["缺口"]`, 1)) || !h.logged(locales.LibraryActionAdoptSource(), "写入失败") {
		t.Fatalf("标记失败应报错：%v", h.log.entries())
	}
	h.svc.FailOn("UpdateEntry", nil)

	h.store.Spawn = func(fn func()) { go fn() }
	release := h.svc.HoldUpdate()
	done := make(chan bool)
	go func() { done <- h.adopt("a", sourceVersion("v1", "新", "残缺正文", "", `["缺口"]`, 1)) }()
	waitFor(t, "标记发出", func() bool { return len(h.svc.UpdateCalls()) == 1 }) // 上一次注入失败的调用不记录
	h.edit(patchContent("我刚输入的"))
	release()
	if <-done {
		t.Fatal("标记期间正文被改了，不该继续采用")
	}
	if !h.logged(locales.LibraryActionAdoptSource(), "编辑内容已变化，来源版本仍保留，请重新核对后采用") || h.detail().Item.Content != "我刚输入的" {
		t.Fatalf("应保留我刚输入的并说明原因：%v %q", h.log.entries(), h.detail().Item.Content)
	}
}

// 条目在别处被彻底删掉（详情还留着）：标记待复核时读不到它，报「条目已删除」，正文不动。
func TestAdoptReportsAnItemDeletedElsewhere(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	for _, cmd := range []services.LibraryBatchCommand{{Kind: services.LibraryBatchTrash}, {Kind: services.LibraryBatchDelete}} {
		if _, err := h.svc.ExecuteBatch(h.ctx, "run-"+cmd.Kind, []string{"a"}, cmd); err != nil {
			t.Fatal(err)
		}
	}
	if h.adopt("a", sourceVersion("v1", "新", "残缺正文", "", `["缺口"]`, 1)) {
		t.Fatal("条目已被删除，不该成功")
	}
	if !h.logged(locales.LibraryActionAdoptSource(), "条目已删除") || h.detail().Item.Content != "甲的正文" {
		t.Fatalf("应说明原因且正文不变：%v %q", h.log.entries(), h.detail().Item.Content)
	}
}

// ---- 标记已复核 ----

// 成功：只清复核标记（状态 clear、原因清空），详情跟上，弹「已标记为复核完成」。
func TestMarkReviewedClearsTheFlagAndToasts(t *testing.T) {
	h := detailHarness(t)
	h.svc.EditExternally("a", func(it *services.LibraryItem) {
		it.ReviewStatus, it.ReviewReasons = services.LibraryReviewNeedsReview, []string{"正文过短"}
	})
	h.open("a")
	if h.detail().Item.ReviewStatus != services.LibraryReviewNeedsReview {
		t.Fatalf("先是待复核：%+v", h.detail().Item)
	}
	if !h.store.MarkReviewed(h.ctx, "a") {
		t.Fatal("应成功")
	}
	calls := h.svc.UpdateCalls()
	if len(calls) != 1 || calls[0].Patch.ReviewStatus == nil || *calls[0].Patch.ReviewStatus != services.LibraryReviewClear || calls[0].Patch.ReviewReasons == nil || len(calls[0].Patch.ReviewReasons) != 0 {
		t.Fatalf("应清掉状态与原因：%+v", calls)
	}
	d := h.detail()
	if d.Item.ReviewStatus != services.LibraryReviewClear || len(d.Item.ReviewReasons) != 0 || d.MarkingReviewedID != "" {
		t.Fatalf("详情应跟上：%+v", d)
	}
	if q := h.queuedToasts(); len(q) != 1 || q[0].Kind != kit.ToastSuccess || q[0].Message != locales.LibraryReviewCleared() {
		t.Fatalf("成功提示：%+v", q)
	}
}

// 失败：标题是「更新复核状态失败」、原因在详情里；条目已不存在也是失败（原因「条目已不存在」）；成功提示不弹。
func TestMarkReviewedReportsFailures(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.svc.FailOn("UpdateEntry", errors.New(" 写入失败\n"))
	if h.store.MarkReviewed(h.ctx, "a") {
		t.Fatal("失败应返回 false")
	}
	q := h.queuedToasts()
	if len(q) != 1 || q[0].Kind != kit.ToastError || q[0].Message != locales.LibraryReviewClearFailed() || q[0].Detail != "写入失败" {
		t.Fatalf("失败提示：%+v", q)
	}
	if !h.logged("library.markReviewed", "写入失败") || h.detail().MarkingReviewedID != "" {
		t.Fatalf("应留痕并放下进行中的标记：%v %+v", h.log.entries(), h.detail())
	}
	h.svc.FailOn("UpdateEntry", nil)
	if h.store.MarkReviewed(h.ctx, "ghost") {
		t.Fatal("条目不存在应失败")
	}
	if q = h.queuedToasts(); len(q) != 2 || q[1].Detail != "条目已不存在" {
		t.Fatalf("不存在的原因：%+v", q)
	}
}

// 进行中不能再点：第二次直接返回 false、不再发请求、不弹提示。
func TestMarkReviewedIgnoresASecondClickWhileRunning(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	release := h.svc.HoldUpdate()
	done := make(chan bool)
	go func() { done <- h.store.MarkReviewed(h.ctx, "a") }()
	waitFor(t, "请求发出", func() bool { return len(h.svc.UpdateCalls()) == 1 })
	if d := h.detail(); d.MarkingReviewedID != "a" {
		t.Fatalf("进行中应标记：%+v", d)
	}
	if h.store.MarkReviewed(h.ctx, "a") || len(h.svc.UpdateCalls()) != 1 {
		t.Fatal("进行中的第二次应被忽略")
	}
	release()
	if !<-done || len(h.queuedToasts()) != 1 {
		t.Fatalf("第一次应成功、只弹一条：%+v", h.queuedToasts())
	}
}

// ---- 转写能力 ----

// 读到条目之后读转写能力：支持区分说话人才为真，失败当作不支持；换条目重置并重读。
func TestMediaCapabilitiesFollowTheDetail(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	if h.detail().CanDiarize {
		t.Fatal("默认不支持")
	}
	h.svc.SetDiarization(true)
	h.open("b")
	if !h.detail().CanDiarize {
		t.Fatal("支持区分说话人时应为真")
	}
	h.svc.FailOn("MediaCapabilities", errors.New("读取失败"))
	h.open("c")
	if h.detail().CanDiarize {
		t.Fatal("读取失败应当作不支持")
	}
	h.svc.FailOn("MediaCapabilities", nil)
	h.store.mu.Lock()
	seq := h.store.detail.seq
	h.store.mu.Unlock()
	h.store.loadMediaCapabilities(h.ctx, seq-1)
	if h.detail().CanDiarize {
		t.Fatal("晚到的旧请求不该改当前条目的能力")
	}
}
