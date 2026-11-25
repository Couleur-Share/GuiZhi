package inbox

import (
	"context"
	"errors"
	"slices"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 InboxWorkspace.tsx 里的动作：批量整理、AI 智能归类（开始 / 取消 / 失败）、单行动作、聚合行「立即执行」。
// 失败路径与渲染层最大的区别：渲染层有几处是未处理的 Promise 拒绝（用户什么都看不到），这里一律弹出带原因的错误提示。

var bg = context.Background()

// ---- 批量整理 ----

func organizeRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.refreshed(review("r1", "复核"), unclassified("u1", "一"), unclassified("u2", "二"))
	r.store.SetSelection([]string{"u1", "u2"})
	return r
}

func TestOrganizeSendsSelectionCollectionAndTags(t *testing.T) {
	r := organizeRig(t)
	r.store.SetOrganizeCollection("c1")
	r.store.SetOrganizeTagText(" 阅读 ，待办, ")
	r.seed(review("r1", "复核")) // 整理之后 u1、u2 不再出现在列表里
	listCalls := r.svc.CallCount("List")

	if !r.store.ApplyOrganization(bg) {
		t.Fatal("整理应成功")
	}
	calls := r.svc.OrganizeCalls()
	if len(calls) != 1 || !slices.Equal(calls[0].ItemIDs, []string{"u1", "u2"}) || calls[0].CollectionID != "c1" || !slices.Equal(calls[0].AddTagNames, []string{"阅读", "待办"}) {
		t.Fatalf("发给服务的整理请求不对：%+v", calls)
	}
	v := r.view()
	if len(v.Selection) != 0 || v.OrganizeTagText != "" || v.OrganizeCollectionID != "c1" || v.Organizing {
		t.Fatalf("成功后清空选择与标签输入、保留合集、忙标记复位：%+v", v)
	}
	if r.svc.CallCount("List") != listCalls+1 || len(v.Visible) != 1 {
		t.Fatalf("成功后应刷新列表：list 调用 %d→%d，可见 %v", listCalls, r.svc.CallCount("List"), rowIDs(v.Visible))
	}
	r.wantToast(kit.ToastSuccess, locales.InboxOrganized(2))
}

// 提示里的条数以服务回报的实际改动数为准，而不是选中的条数。
func TestOrganizeToastReportsChangedCount(t *testing.T) {
	r := organizeRig(t)
	r.svc.SetChanged("Organize", 1)
	r.store.ApplyOrganization(bg)
	r.wantToast(kit.ToastSuccess, locales.InboxOrganized(1))
}

func TestOrganizeOmitsEmptyOptionalFields(t *testing.T) {
	r := organizeRig(t)
	r.store.SetOrganizeCollection("c1")
	r.store.ApplyOrganization(bg)
	r.store.SetSelection([]string{"u1"})
	r.store.SetOrganizeCollection("")
	r.store.SetOrganizeTagText("仅标签")
	r.store.ApplyOrganization(bg)

	calls := r.svc.OrganizeCalls()
	if len(calls) != 2 {
		t.Fatalf("应有两次请求：%+v", calls)
	}
	if calls[0].CollectionID != "c1" || len(calls[0].AddTagNames) != 0 {
		t.Fatalf("只选合集时不带标签：%+v", calls[0])
	}
	if calls[1].CollectionID != "" || !slices.Equal(calls[1].AddTagNames, []string{"仅标签"}) {
		t.Fatalf("只写标签时不带合集：%+v", calls[1])
	}
}

func TestOrganizeFailureKeepsSelectionAndReportsReason(t *testing.T) {
	r := organizeRig(t)
	r.store.SetOrganizeCollection("c1")
	r.store.SetOrganizeTagText("标签")
	r.svc.FailOn("Organize", errors.New("合集不存在"))

	if r.store.ApplyOrganization(bg) {
		t.Fatal("失败时应返回 false")
	}
	v := r.view()
	if !slices.Equal(v.Selection, []string{"u1", "u2"}) || v.OrganizeTagText != "标签" || v.Organizing {
		t.Fatalf("失败后选择与输入都应保留，方便重试：%+v", v)
	}
	got := r.wantToast(kit.ToastError, locales.CommonOperationFailed(locales.InboxOrganizeSelected()))
	if got.Detail != "合集不存在" {
		t.Fatalf("详情应带原因：%+v", got)
	}
	if logs := r.log.entries(); len(logs) != 1 {
		t.Fatalf("失败要留日志：%v", logs)
	}
	if len(r.toasts()) != 1 {
		t.Fatalf("失败时不应再弹「已整理」：%+v", r.toasts())
	}

	r.svc.FailOn("Organize", nil)
	if !r.store.ApplyOrganization(bg) {
		t.Fatal("重试应成功")
	}
}

func TestOrganizeWithoutSelectionIsNoop(t *testing.T) {
	r := newRig(t)
	r.refreshed(unclassified("u1", "一"))
	if r.store.ApplyOrganization(bg) {
		t.Fatal("没有选中条目时不应整理")
	}
	if r.svc.CallCount("Organize") != 0 || len(r.toasts()) != 0 {
		t.Fatalf("没有选中条目时什么都不做：%v %+v", r.svc.Calls(), r.toasts())
	}
}

// 整理期间再点一次被忽略：同一批不会提交两次。
func TestOrganizeIgnoresReentry(t *testing.T) {
	r := organizeRig(t)
	release := r.svc.Hold("Organize")
	done := make(chan struct{})
	go func() {
		r.store.ApplyOrganization(bg)
		close(done)
	}()
	waitFor(t, "整理请求发出", func() bool { return r.view().Organizing && r.svc.CallCount("Organize") == 1 })

	if r.store.ApplyOrganization(bg) {
		t.Fatal("整理进行中再次触发应被忽略")
	}
	release()
	waitDone(t, "整理结束", done)
	if n := r.svc.CallCount("Organize"); n != 1 {
		t.Fatalf("服务只应收到一次整理请求：%d", n)
	}
	if r.view().Organizing {
		t.Fatal("结束后忙标记应复位")
	}
}

// ---- 标记已复核 ----

func TestMarkReviewedRefreshesList(t *testing.T) {
	r := newRig(t)
	r.refreshed(review("r1", "甲"), review("r2", "乙"))
	r.seed(review("r2", "乙"))
	toasts := len(r.toasts())

	if !r.store.MarkReviewed(bg, []string{"r1"}) {
		t.Fatal("标记应成功")
	}
	if got := r.svc.ReviewedCalls(); len(got) != 1 || !slices.Equal(got[0], []string{"r1"}) {
		t.Fatalf("发给服务的 ID 不对：%v", got)
	}
	if got := rowIDs(r.view().Items); !slices.Equal(got, []string{"review:r2"}) {
		t.Fatalf("标记后应刷新列表：%v", got)
	}
	r.wantNoNewToast(toasts)
}

func TestMarkReviewedFailureReportsAndDoesNotRefresh(t *testing.T) {
	r := newRig(t)
	r.refreshed(review("r1", "甲"))
	listCalls := r.svc.CallCount("List")
	r.svc.FailOn("MarkReviewed", errors.New("数据库只读"))

	if r.store.MarkReviewed(bg, []string{"r1"}) {
		t.Fatal("失败时应返回 false")
	}
	got := r.wantToast(kit.ToastError, locales.CommonOperationFailed(locales.LibraryMarkReviewed()))
	if got.Detail != "数据库只读" {
		t.Fatalf("详情应带原因：%+v", got)
	}
	if r.svc.CallCount("List") != listCalls {
		t.Fatal("失败时不刷新")
	}
	if len(r.view().Items) != 1 {
		t.Fatal("失败时条目仍在列表里")
	}
}

// ---- 重试采集任务 ----

func TestRetryImportTaskRefreshesQueueAndInbox(t *testing.T) {
	r := newRig(t)
	r.refreshed(importIssue("t1", "failed", "解析失败"))
	listCalls := r.svc.CallCount("List")
	toasts := len(r.toasts())

	if !r.store.RetryImportTask(bg, "t1") {
		t.Fatal("重试应成功")
	}
	if got := r.svc.RetriedCalls(); !slices.Equal(got, []string{"t1"}) {
		t.Fatalf("重试的任务不对：%v", got)
	}
	if r.importRefreshes != 1 || r.svc.CallCount("List") != listCalls+1 {
		t.Fatalf("应刷新采集队列与处理中心：imports=%d list %d→%d", r.importRefreshes, listCalls, r.svc.CallCount("List"))
	}
	r.wantNoNewToast(toasts)
}

func TestRetryImportTaskMissingExplainsAndStillRefreshes(t *testing.T) {
	r := newRig(t)
	r.refreshed(importIssue("t1", "failed", "x"))
	listCalls := r.svc.CallCount("List")
	r.svc.SetRetryAbsent("t1")

	if r.store.RetryImportTask(bg, "t1") {
		t.Fatal("任务不存在应返回 false")
	}
	got := r.wantToast(kit.ToastError, locales.CommonOperationFailed(locales.ImportsActionRetry()))
	if got.Detail != "任务已不存在，请刷新列表" {
		t.Fatalf("应说明任务已不存在：%+v", got)
	}
	if r.importRefreshes != 1 || r.svc.CallCount("List") != listCalls+1 {
		t.Fatal("失败后仍要刷新（任务可能已被清掉）")
	}
}

func TestRetryImportTaskFailureReports(t *testing.T) {
	r := newRig(t)
	r.refreshed(importIssue("t1", "failed", "x"), importIssue("t2", "failed", "y"))
	r.svc.FailOn("RetryImportTask:t1", errors.New("队列已暂停"))

	if r.store.RetryImportTask(bg, "t1") {
		t.Fatal("应失败")
	}
	if got := r.lastToast(); got.Kind != kit.ToastError || got.Detail != "队列已暂停" {
		t.Fatalf("应弹出带原因的错误：%+v", got)
	}
	if !r.store.RetryImportTask(bg, "t2") { // 只让 t1 失败
		t.Fatal("别的任务不受影响")
	}
}

// ---- 不再提醒 ----

func TestDismissImportWarningRemovesRow(t *testing.T) {
	r := newRig(t)
	r.refreshed(importIssue("t1", "completed", "语音转写失败"))
	r.seed()

	if !r.store.DismissImportWarning(bg, "t1") {
		t.Fatal("应成功")
	}
	if got := r.svc.AcknowledgedCalls(); !slices.Equal(got, []string{"t1"}) {
		t.Fatalf("处理的任务不对：%v", got)
	}
	v := r.view()
	if len(v.Items) != 0 || v.AcknowledgingTaskID != "" {
		t.Fatalf("成功后应刷新、忙标记复位：%+v", v)
	}
	r.wantToast(kit.ToastSuccess, locales.InboxWarningAcknowledged())
}

func TestDismissImportWarningAlreadyHandledIsInfo(t *testing.T) {
	r := newRig(t)
	r.refreshed(importIssue("t1", "completed", "x"))
	r.svc.SetChanged("AcknowledgeImportWarning", 0)
	r.store.DismissImportWarning(bg, "t1")
	r.wantToast(kit.ToastInfo, locales.InboxWarningAlreadyHandled())
}

func TestDismissImportWarningFailureReportsWithDetail(t *testing.T) {
	r := newRig(t)
	r.refreshed(importIssue("t1", "completed", "x"))
	listCalls := r.svc.CallCount("List")
	r.svc.FailOn("AcknowledgeImportWarning:t1", errors.New("写入失败"))

	if r.store.DismissImportWarning(bg, "t1") {
		t.Fatal("应失败")
	}
	got := r.wantToast(kit.ToastError, locales.InboxActionFailed("写入失败"))
	if got.Detail != "写入失败" {
		t.Fatalf("详情应带原因：%+v", got)
	}
	if r.view().AcknowledgingTaskID != "" || r.svc.CallCount("List") != listCalls || len(r.log.entries()) != 1 {
		t.Fatalf("失败后忙标记复位、不刷新、留日志：%+v logs=%v", r.view(), r.log.entries())
	}
}

func TestDismissImportWarningShowsBusyTask(t *testing.T) {
	r := newRig(t)
	r.refreshed(importIssue("t1", "completed", "x"), importIssue("t2", "completed", "y"))
	release := r.svc.Hold("AcknowledgeImportWarning")
	done := make(chan struct{})
	go func() {
		r.store.DismissImportWarning(bg, "t2")
		close(done)
	}()
	waitFor(t, "处理中", func() bool { return r.view().AcknowledgingTaskID == "t2" })
	release()
	waitDone(t, "处理结束", done)
	if r.view().AcknowledgingTaskID != "" {
		t.Fatal("结束后应复位")
	}
}

func TestOpenItemUsesHook(t *testing.T) {
	r := newRig(t)
	r.store.OpenItem(bg, "")
	r.store.OpenItem(bg, "a")
	if !slices.Equal(r.opened, []string{"a"}) {
		t.Fatalf("空 ID 不应打开：%v", r.opened)
	}
	r.store.OnOpenItem = nil
	r.store.OpenItem(bg, "b") // 没接线时什么都不做、不崩溃
}

// ---- 聚合行：语义索引与 Wiki ----

func semanticRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 6})
	r.refreshed(review("r1", "甲"))
	return r
}

func TestRunSemanticDoneRefreshesAndReports(t *testing.T) {
	r := semanticRig(t)
	r.svc.SetSemanticNotice(&services.InboxSemanticNotice{Kind: services.InboxSemanticDone, Indexed: 6})
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 6, IndexedItems: 6})
	listCalls := r.svc.CallCount("List")

	r.store.RunSemanticNow(bg)
	r.wantToast(kit.ToastSuccess, locales.InboxSemanticIndexed(6))
	v := r.view()
	if v.Running != RunningNone || r.svc.IndexRuns() != 1 {
		t.Fatalf("结束后占用标记复位：%+v runs=%d", v.Running, r.svc.IndexRuns())
	}
	if r.svc.CallCount("List") != listCalls+1 || v.Counts.SemanticPending != 0 {
		t.Fatalf("结束后应刷新，语义索引聚合行消失：%+v", v.Counts)
	}
}

func TestRunSemanticPartialWarnsWithReason(t *testing.T) {
	r := semanticRig(t)
	r.svc.SetSemanticNotice(&services.InboxSemanticNotice{Kind: services.InboxSemanticPartial, Indexed: 3, Failed: 2, Message: "额度不足"})
	r.store.RunSemanticNow(bg)
	got := r.wantToast(kit.ToastWarning, locales.InboxSemanticPartial(3, 2, "额度不足"))
	if got.Detail != "额度不足" || len(r.log.entries()) != 1 {
		t.Fatalf("原因应进详情与日志：%+v %v", got, r.log.entries())
	}
}

func TestRunSemanticPartialWithoutMessageUsesUnknownError(t *testing.T) {
	r := semanticRig(t)
	r.svc.SetSemanticNotice(&services.InboxSemanticNotice{Kind: services.InboxSemanticPartial, Indexed: 1, Failed: 1})
	r.store.RunSemanticNow(bg)
	r.wantToast(kit.ToastWarning, locales.InboxSemanticPartial(1, 1, locales.CommonUnknownError()))
}

func TestRunSemanticFailedShowsError(t *testing.T) {
	r := semanticRig(t)
	r.svc.SetSemanticNotice(&services.InboxSemanticNotice{Kind: services.InboxSemanticFailed, Failed: 6, Message: "HTTP 500"})
	r.store.RunSemanticNow(bg)
	got := r.wantToast(kit.ToastError, locales.InboxSemanticFailed("HTTP 500"))
	if got.Detail != "HTTP 500" || len(r.log.entries()) != 1 {
		t.Fatalf("原因应进详情与日志：%+v %v", got, r.log.entries())
	}
}

func TestRunSemanticNotConfiguredOpensAISettings(t *testing.T) {
	r := semanticRig(t)
	r.svc.SetSemanticNotice(&services.InboxSemanticNotice{Kind: services.InboxSemanticNotConfigured})
	r.store.RunSemanticNow(bg)
	r.wantToast(kit.ToastError, locales.InboxSemanticNotConfigured())
	if r.settingsAI != 1 {
		t.Fatalf("应打开设置的 AI 分区：%d", r.settingsAI)
	}
}

func TestRunSemanticNothingIsInfo(t *testing.T) {
	r := semanticRig(t) // 假服务缺省回执就是「没有需要索引的内容」
	r.store.RunSemanticNow(bg)
	r.wantToast(kit.ToastInfo, locales.InboxSemanticNothing())
}

// 没有回执：索引没能启动（渲染层的 notice 为空）。
func TestRunSemanticWithoutNoticeIsStartFailure(t *testing.T) {
	r := semanticRig(t)
	r.svc.SetSemanticNotice(nil)
	r.store.RunSemanticNow(bg)
	r.wantToast(kit.ToastError, locales.InboxSemanticStartFailed())
	if len(r.log.entries()) != 1 {
		t.Fatalf("启动失败要留日志：%v", r.log.entries())
	}
}

func TestRunSemanticAlreadyIndexingOnlyInforms(t *testing.T) {
	r := semanticRig(t)
	r.svc.SetSemanticIndexing(true)
	listCalls := r.svc.CallCount("List")
	r.store.RunSemanticNow(bg)
	r.wantToast(kit.ToastInfo, locales.InboxSemanticRunning())
	if r.svc.IndexRuns() != 0 || r.svc.CallCount("List") != listCalls || r.view().Running != RunningNone {
		t.Fatal("已有一轮在跑时不应再起一轮，也不刷新")
	}
}

func TestRunSemanticErrorShowsToastWithoutRefresh(t *testing.T) {
	r := semanticRig(t)
	r.svc.FailOn("RunSemanticIndexing", errors.New("设置读取失败"))
	listCalls := r.svc.CallCount("List")
	r.store.RunSemanticNow(bg)
	got := r.wantToast(kit.ToastError, locales.InboxSemanticError("设置读取失败"))
	if got.Detail != "设置读取失败" || r.svc.CallCount("List") != listCalls || r.view().Running != RunningNone {
		t.Fatalf("执行失败：详情带原因、不刷新、占用标记复位：%+v running=%q", got, r.view().Running)
	}
}

// 执行期间 Running 非空：再次点击（含另一个聚合行的按钮）被忽略。
func TestRunSemanticBusyIgnoresSecondRun(t *testing.T) {
	r := semanticRig(t)
	release := r.svc.Hold("RunSemanticIndexing")
	done := make(chan struct{})
	go func() {
		r.store.RunSemanticNow(bg)
		close(done)
	}()
	waitFor(t, "索引开始", func() bool { return r.view().Running == RunningSemantic && r.svc.CallCount("RunSemanticIndexing") == 1 })

	toasts := len(r.toasts())
	r.store.RunSemanticNow(bg)
	r.wantNoNewToast(toasts)
	if n := r.svc.CallCount("RunSemanticIndexing"); n != 1 {
		t.Fatalf("不应再发起一轮：%d", n)
	}
	release()
	waitDone(t, "索引结束", done)
	if r.view().Running != RunningNone {
		t.Fatal("结束后占用标记复位")
	}
}

func TestRunWikiOpensPreviewHook(t *testing.T) {
	r := newRig(t)
	r.store.RunWiki()
	r.store.RunWiki()
	if r.wikiPreviews != 2 {
		t.Fatalf("应触发 Wiki 编译预览：%d", r.wikiPreviews)
	}
	r.store.OnWikiPreview = nil
	r.store.RunWiki() // 没接线时不崩溃
}

// ---- AI 智能归类 ----

func classifyRig(t *testing.T, unclassifiedIDs ...string) *rig {
	t.Helper()
	r := newRig(t)
	items := []services.InboxItem{review("r1", "复核")}
	for _, id := range unclassifiedIDs {
		items = append(items, unclassified(id, "条目 "+id))
	}
	r.refreshed(items...)
	r.store.SetSelection(append([]string{"r1"}, unclassifiedIDs...))
	return r
}

func TestSmartClassifyHappyPath(t *testing.T) {
	r := classifyRig(t, "u1", "u2")
	r.collections = []string{"阅读", "工作"}
	r.svc.SetAssignments(map[string]string{"u1": "技术", "u2": "生活"})
	r.svc.SetApplyResult(services.InboxAIApplyResult{Classified: 2, CreatedCollectionNames: []string{"技术", "生活"}})
	r.seed(review("r1", "复核")) // 归类之后 u1、u2 离开处理中心
	listCalls := r.svc.CallCount("List")

	r.store.ToggleSmartClassify(bg)

	if got := r.svc.SourceRequests(); len(got) != 1 || !slices.Equal(got[0], []string{"u1", "u2"}) {
		t.Fatalf("只应取未归合集的条目（复核条目不参与）：%v", got)
	}
	calls := r.svc.ClassifyCalls()
	if len(calls) != 1 || len(calls[0].Sources) != 2 || !slices.Equal(calls[0].Existing, []string{"阅读", "工作"}) {
		t.Fatalf("归类请求应带现有合集名称：%+v", calls)
	}
	applied := r.svc.AppliedCalls()
	if len(applied) != 1 || !slices.Equal(applied[0], []services.InboxAIAssignment{{ItemID: "u1", CollectionName: "技术"}, {ItemID: "u2", CollectionName: "生活"}}) {
		t.Fatalf("应把 AI 的计划原样写入：%+v", applied)
	}
	if r.classified != 1 || r.svc.CallCount("List") != listCalls+1 {
		t.Fatalf("写入后应刷新合集与知识库、再刷新处理中心：classified=%d", r.classified)
	}
	got := r.wantToast(kit.ToastSuccess, locales.InboxClassifiedWithNew(2, 2))
	if got.Detail != "技术"+locales.InboxNameSeparator()+"生活" {
		t.Fatalf("新建的合集名称应放进详情：%q", got.Detail)
	}
	v := r.view()
	if v.Classifying || v.Classify != (ClassifyProgress{}) || !slices.Equal(v.Selection, []string{"r1"}) {
		t.Fatalf("结束后进度复位，已归类的条目从选择里剔除：%+v", v)
	}
}

func TestSmartClassifyReusedCollectionsHasNoDetail(t *testing.T) {
	r := classifyRig(t, "u1")
	r.svc.SetApplyResult(services.InboxAIApplyResult{Classified: 1})
	r.store.ToggleSmartClassify(bg)
	got := r.wantToast(kit.ToastSuccess, locales.InboxClassifiedReused(1))
	if got.Detail != "" {
		t.Fatalf("没有新建合集就没有详情：%q", got.Detail)
	}
}

func TestSmartClassifyReportsSkippedItems(t *testing.T) {
	r := classifyRig(t, "u1", "u2")
	r.svc.SetApplyResult(services.InboxAIApplyResult{Classified: 1, Skipped: 1})
	r.store.ToggleSmartClassify(bg)
	r.wantToast(kit.ToastSuccess, locales.InboxClassifySkipped(locales.InboxClassifiedReused(1), 1))
}

func TestSmartClassifyNothingClassifiedIsInfo(t *testing.T) {
	r := classifyRig(t, "u1", "u2")
	r.svc.SetApplyResult(services.InboxAIApplyResult{Classified: 0, Skipped: 2})
	r.store.ToggleSmartClassify(bg)
	r.wantToast(kit.ToastInfo, locales.InboxClassifySkipped(locales.InboxNothingToClassify(), 2))
}

// 选中的条目都已变更：不请求 AI、不写入，但仍刷新（把过期的行去掉）并通知刷新合集。
func TestSmartClassifyAllSourcesGoneSkipsAIButRefreshes(t *testing.T) {
	r := classifyRig(t, "u1", "u2")
	r.svc.SetSources([]services.InboxAISource{})
	r.seed(review("r1", "复核"))
	listCalls := r.svc.CallCount("List")

	r.store.ToggleSmartClassify(bg)
	if r.svc.CallCount("Classify") != 0 || len(r.svc.AppliedCalls()) != 0 {
		t.Fatalf("没有可归类的条目时不应请求 AI 或写入：%v", r.svc.Calls())
	}
	r.wantToast(kit.ToastInfo, locales.InboxClassifySkipped(locales.InboxNothingToClassify(), 2))
	if r.svc.CallCount("List") != listCalls+1 || len(r.view().Items) != 1 {
		t.Fatalf("应刷新列表：%v", rowIDs(r.view().Items))
	}
	if r.classified != 1 {
		t.Fatalf("仍应刷新合集与知识库：%d", r.classified)
	}
}

func TestSmartClassifyWithoutSelectedUnclassifiedIsNoop(t *testing.T) {
	r := newRig(t)
	r.refreshed(review("r1", "复核"), unclassified("u1", "一"))
	r.store.SetSelection([]string{"r1"}) // 只选了复核条目
	calls := len(r.svc.Calls())
	r.store.ToggleSmartClassify(bg)
	if len(r.svc.Calls()) != calls || len(r.toasts()) != 0 || r.view().Classifying {
		t.Fatalf("没有选中未归合集的条目时什么都不做：%v", r.svc.Calls())
	}
}

// 未在归类时「停止」是 no-op：即使已选中未归合集的条目，也不能因帧内视图过期而启动新一轮归类。
func TestStopSmartClassifyWhenIdleIsNoOp(t *testing.T) {
	r := classifyRig(t, "u1", "u2")
	calls := len(r.svc.Calls())

	r.store.StopSmartClassify()

	if r.svc.CallCount("Classify") != 0 || len(r.svc.AppliedCalls()) != 0 || r.classified != 0 {
		t.Fatalf("空闲时停止不应请求 AI 或写入分类：%v", r.svc.Calls())
	}
	if len(r.svc.Calls()) != calls || len(r.toasts()) != 0 {
		t.Fatalf("空闲时停止什么都不做：%v", r.svc.Calls())
	}
	if v := r.view(); v.Classifying || v.Classify != (ClassifyProgress{}) {
		t.Fatalf("空闲时停止后仍应是未归类状态：%+v", v)
	}
}

// 开始时按每批 20 条估算总批数；每批完成后更新进度。
func TestSmartClassifyShowsEstimatedTotalThenProgress(t *testing.T) {
	ids := make([]string, 45)
	items := make([]services.InboxItem, len(ids))
	for i := range ids {
		ids[i] = "u" + string(rune('A'+i/26)) + string(rune('a'+i%26))
		items[i] = unclassified(ids[i], "条目")
	}
	r := newRig(t)
	r.refreshed(items...)
	r.store.SetSelection(ids)

	releaseSources := r.svc.Hold("AIClassificationSources")
	r.svc.SetClassifyBatches(3)
	releaseClassify := r.svc.HoldClassifyAfter(2)
	done := make(chan struct{})
	go func() {
		r.store.ToggleSmartClassify(bg)
		close(done)
	}()
	waitFor(t, "开始归类", func() bool { return r.view().Classifying })
	if got := r.view().Classify; got != (ClassifyProgress{Completed: 0, Total: 3}) {
		t.Fatalf("45 条按每批 20 条应估算 3 批：%+v", got)
	}

	releaseSources()
	waitFor(t, "第 2 批完成", func() bool { return r.view().Classify == ClassifyProgress{Completed: 2, Total: 3} })
	releaseClassify()
	waitDone(t, "归类结束", done)
	if v := r.view(); v.Classifying || v.Classify != (ClassifyProgress{}) {
		t.Fatalf("结束后进度复位：%+v", v)
	}
}

// 归类进行中再点一次就是取消：不写入任何分类，选择保留，提示说明「已停止」。
func TestSmartClassifySecondToggleCancelsWithoutWriting(t *testing.T) {
	r := classifyRig(t, "u1", "u2")
	r.svc.SetClassifyBatches(3)
	release := r.svc.HoldClassifyAfter(1)
	defer release()
	done := make(chan struct{})
	go func() {
		r.store.ToggleSmartClassify(bg)
		close(done)
	}()
	waitFor(t, "第 1 批完成", func() bool { return r.view().Classify == ClassifyProgress{Completed: 1, Total: 3} })

	r.store.ToggleSmartClassify(bg) // 停止
	waitDone(t, "归类结束", done)

	if len(r.svc.AppliedCalls()) != 0 || r.classified != 0 {
		t.Fatalf("取消后不应写入任何分类：%+v", r.svc.AppliedCalls())
	}
	r.wantToast(kit.ToastInfo, locales.InboxClassifyStopped())
	v := r.view()
	if v.Classifying || v.Classify != (ClassifyProgress{}) || !slices.Equal(v.Selection, []string{"r1", "u1", "u2"}) {
		t.Fatalf("取消后进度复位、选择保留：%+v", v)
	}
	// 取消不是失败：不留错误日志
	if len(r.log.entries()) != 0 {
		t.Fatalf("取消不应记为错误：%v", r.log.entries())
	}
}

// AI 已经返回、写库之前才取消：同样不写入（渲染层这里不看取消信号，写入后却提示「尚未写入」）。
func TestSmartClassifyCancelAfterAIReturnedStillWritesNothing(t *testing.T) {
	r := classifyRig(t, "u1")
	r.store.svc = &cancelAfterClassify{Inbox: r.svc, cancel: func() { r.store.ToggleSmartClassify(bg) }}

	r.store.ToggleSmartClassify(bg)
	if len(r.svc.AppliedCalls()) != 0 {
		t.Fatalf("取消发生在写库之前就不应写入：%+v", r.svc.AppliedCalls())
	}
	r.wantToast(kit.ToastInfo, locales.InboxClassifyStopped())
	if r.view().Classifying {
		t.Fatal("结束后进度复位")
	}
}

// cancelAfterClassify 让 Classify 返回前先触发一次「停止」。
type cancelAfterClassify struct {
	*servicestest.Inbox
	cancel func()
}

func (c *cancelAfterClassify) Classify(ctx context.Context, sources []services.InboxAISource, existing []string, onProgress func(completed, total int)) ([]services.InboxAIAssignment, error) {
	plan, err := c.Inbox.Classify(ctx, sources, existing, onProgress)
	c.cancel()
	return plan, err
}

func TestSmartClassifyNotConfiguredOpensAISettings(t *testing.T) {
	r := classifyRig(t, "u1")
	r.svc.FailOn("Classify", services.ErrAINotConfigured)
	r.store.ToggleSmartClassify(bg)
	r.wantToast(kit.ToastError, locales.InboxAiNotConfigured())
	if r.settingsAI != 1 || len(r.svc.AppliedCalls()) != 0 || r.classified != 0 {
		t.Fatalf("未配置模型：打开 AI 设置、不写入：settings=%d", r.settingsAI)
	}
	if v := r.view(); v.Classifying || !slices.Equal(v.Selection, []string{"r1", "u1"}) {
		t.Fatalf("失败后进度复位、选择保留：%+v", v)
	}
}

func TestSmartClassifyAIFailureReportsWithDetail(t *testing.T) {
	r := classifyRig(t, "u1")
	r.svc.FailOn("Classify", errors.New("AI 分类结果漏掉 1 条内容"))
	r.store.ToggleSmartClassify(bg)
	got := r.wantToast(kit.ToastError, locales.InboxClassifyFailed("AI 分类结果漏掉 1 条内容"))
	if got.Detail != "AI 分类结果漏掉 1 条内容" || len(r.log.entries()) != 1 {
		t.Fatalf("原因应进详情与日志：%+v %v", got, r.log.entries())
	}
	if len(r.svc.AppliedCalls()) != 0 || r.classified != 0 || r.view().Classifying {
		t.Fatal("失败时不写入、进度复位")
	}
}

func TestSmartClassifySourceLookupFailureReports(t *testing.T) {
	r := classifyRig(t, "u1")
	r.svc.FailOn("AIClassificationSources", errors.New("一次最多智能归类 100 条内容"))
	r.store.ToggleSmartClassify(bg)
	got := r.wantToast(kit.ToastError, locales.InboxClassifyFailed("一次最多智能归类 100 条内容"))
	if got.Detail == "" || r.svc.CallCount("Classify") != 0 {
		t.Fatalf("取摘要失败：弹出原因、不请求 AI：%+v", got)
	}
}

func TestSmartClassifyApplyFailureReports(t *testing.T) {
	r := classifyRig(t, "u1")
	r.svc.FailOn("ApplyAIClassification", errors.New("事务失败"))
	r.store.ToggleSmartClassify(bg)
	got := r.wantToast(kit.ToastError, locales.InboxClassifyFailed("事务失败"))
	if got.Detail != "事务失败" || r.classified != 0 {
		t.Fatalf("写入失败：弹出原因、不刷新合集：%+v classified=%d", got, r.classified)
	}
	if v := r.view(); v.Classifying || !slices.Equal(v.Selection, []string{"r1", "u1"}) {
		t.Fatalf("写入失败后选择保留，可重试：%+v", v)
	}
}

// 失败后可以直接重试成功（进度与取消句柄已复位）。
func TestSmartClassifyRetryAfterFailureSucceeds(t *testing.T) {
	r := classifyRig(t, "u1")
	r.svc.FailOn("Classify", errors.New("网络错误"))
	r.store.ToggleSmartClassify(bg)
	r.svc.FailOn("Classify", nil)
	r.store.ToggleSmartClassify(bg)
	if len(r.svc.AppliedCalls()) != 1 {
		t.Fatalf("重试应写入：%+v", r.svc.AppliedCalls())
	}
}

// 合集名称在点击时取一次快照，交给模型优先复用；没接线时按没有现有合集处理。
func TestSmartClassifyWithoutCollectionNamesHook(t *testing.T) {
	r := classifyRig(t, "u1")
	r.store.CollectionNames = nil
	r.store.ToggleSmartClassify(bg)
	calls := r.svc.ClassifyCalls()
	if len(calls) != 1 || len(calls[0].Existing) != 0 {
		t.Fatalf("没有合集名称接线时应传空：%+v", calls)
	}
}

// 界面卸载时取消进行中的归类，不写入。
func TestCloseCancelsRunningClassification(t *testing.T) {
	r := classifyRig(t, "u1")
	release := r.svc.HoldClassifyAfter(1)
	defer release()
	done := make(chan struct{})
	go func() {
		r.store.ToggleSmartClassify(bg)
		close(done)
	}()
	waitFor(t, "开始归类", func() bool { return r.view().Classify.Completed == 1 })
	r.store.Close()
	waitDone(t, "归类结束", done)
	if len(r.svc.AppliedCalls()) != 0 || r.view().Classifying {
		t.Fatal("卸载后不应写入、进度复位")
	}
	r.store.Close() // 没有进行中的归类时也安全
}

func TestSmartClassifyWakesTheUI(t *testing.T) {
	r := classifyRig(t, "u1")
	before := r.wakes.Load()
	r.store.ToggleSmartClassify(bg)
	if r.wakes.Load() <= before {
		t.Fatal("归类应请求重绘")
	}
}
