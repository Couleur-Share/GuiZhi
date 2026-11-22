package inbox

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 stores/inbox.store.ts 与 InboxWorkspace.tsx 的状态机：刷新的三块并发与代次保护、加载失败不伪装成空态、
// 分组与选择。动作（整理、归类、单行与聚合行）见 store_actions_test.go。

// ---- 测试替身 ----

// recLog 记录写进日志的失败（Guard 在弹提示的同时写日志）。
type recLog struct {
	mu   sync.Mutex
	logs []string
}

func (r *recLog) AppError(scope, action, message string) {
	r.mu.Lock()
	r.logs = append(r.logs, action+"|"+message)
	r.mu.Unlock()
}

func (r *recLog) entries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.logs)
}

// rig 把假服务、提示通道、日志与各个钩子的调用记录接在一个 Store 上。Spawn 默认同步，保持确定性；
// 并发用例用 useGoroutines 改回真正的 goroutine。
type rig struct {
	t      *testing.T
	svc    *servicestest.Inbox
	store  *Store
	notify *kit.Notifier
	log    *recLog
	wakes  atomic.Int32

	mu              sync.Mutex
	opened          []string
	settingsAI      int
	wikiPreviews    int
	classified      int
	importRefreshes int
	collections     []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, svc: &servicestest.Inbox{}, notify: kit.NewNotifier(nil), log: &recLog{}}
	r.store = NewStore(r.svc, kit.Guard{Notify: r.notify, Log: r.log}, func() { r.wakes.Add(1) })
	r.store.Spawn = func(fn func()) { fn() }
	r.store.OnOpenItem = func(_ context.Context, id string) { r.locked(func() { r.opened = append(r.opened, id) }) }
	r.store.OnOpenSettingsAI = func() { r.locked(func() { r.settingsAI++ }) }
	r.store.OnWikiPreview = func() { r.locked(func() { r.wikiPreviews++ }) }
	r.store.OnClassified = func(context.Context) { r.locked(func() { r.classified++ }) }
	r.store.RefreshImports = func(context.Context) { r.locked(func() { r.importRefreshes++ }) }
	r.store.CollectionNames = func() []string {
		r.mu.Lock()
		defer r.mu.Unlock()
		return slices.Clone(r.collections)
	}
	return r
}

func (r *rig) locked(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

func (r *rig) useGoroutines() { r.store.Spawn = func(fn func()) { go fn() } }

func (r *rig) view() View { return r.store.View() }

func (r *rig) seed(items ...services.InboxItem) { r.svc.SetList(listOf(items...)) }

// refreshed 先 seed 再刷新，返回刷新后的快照。
func (r *rig) refreshed(items ...services.InboxItem) View {
	r.t.Helper()
	r.seed(items...)
	r.store.Refresh(context.Background())
	return r.view()
}

func (r *rig) toasts() []kit.QueuedToast { return r.notify.Queued() }

// lastToast 取最近一条提示。
func (r *rig) lastToast() kit.QueuedToast {
	r.t.Helper()
	all := r.toasts()
	if len(all) == 0 {
		r.t.Fatal("期望有一条提示，实际没有")
	}
	return all[len(all)-1]
}

func (r *rig) wantToast(kind kit.ToastKind, message string) kit.QueuedToast {
	r.t.Helper()
	got := r.lastToast()
	if got.Kind != kind || got.Message != message {
		r.t.Fatalf("提示不对：\n got  [%s] %q (detail %q)\n want [%s] %q", got.Kind, got.Message, got.Detail, kind, message)
	}
	return got
}

func (r *rig) wantNoNewToast(before int) {
	r.t.Helper()
	if got := r.toasts(); len(got) != before {
		r.t.Fatalf("不应弹出新提示：%+v", got[before:])
	}
}

func review(id, title string, reasons ...string) services.InboxItem {
	return services.InboxItem{Kind: services.InboxKindReviewRequired, ID: "review:" + id, ItemID: id, Title: title, Reasons: reasons}
}

func unclassified(id, title string) services.InboxItem {
	return services.InboxItem{Kind: services.InboxKindUnclassified, ID: "unclassified:" + id, ItemID: id, Title: title}
}

func importIssue(taskID, status, message string) services.InboxItem {
	return services.InboxItem{Kind: services.InboxKindImportIssue, ID: "import:" + taskID, TaskID: taskID, Title: "任务 " + taskID, Status: status, Message: message}
}

// listOf 按条目算出计数；Total 取复核与采集问题之和（测试里不构造「同一内容被两处计入」的情形）。
func listOf(items ...services.InboxItem) services.InboxList {
	l := services.InboxList{Items: items}
	for _, it := range items {
		l.Counts = l.Counts.With(it.Kind, l.Counts.Of(it.Kind)+1)
	}
	l.Total = l.Counts.ReviewRequired + l.Counts.ImportIssue
	return l
}

func rowIDs(items []services.InboxItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

func waitDone(t *testing.T, what string, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("等待超时：%s", what)
	}
}

// ---- 刷新：三块数据 ----

func TestRefreshLoadsListCountsAndAggregates(t *testing.T) {
	r := newRig(t)
	r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 3, Review: 1})
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 10, IndexedItems: 7})
	v := r.refreshed(review("a", "甲"), unclassified("b", "乙"), importIssue("t1", "failed", "解析失败"))

	if !v.HasLoaded || v.IsLoading || v.LoadError != "" || v.SectionErrors.Any() {
		t.Fatalf("加载完成后的状态不对：%+v", v)
	}
	// 需要处理的行在前，聚合行在后，语义索引在 Wiki 之前
	if got, want := rowIDs(v.Items), []string{"review:a", "unclassified:b", "import:t1", "aggregate:semantic-pending", "aggregate:wiki-pending"}; !slices.Equal(got, want) {
		t.Fatalf("行的顺序不对：got %v want %v", got, want)
	}
	want := services.InboxCounts{ReviewRequired: 1, Unclassified: 1, ImportIssue: 1, SemanticPending: 3, WikiPending: 3}
	if v.Counts != want || v.Total != 2 {
		t.Fatalf("计数不对：%+v total=%d", v.Counts, v.Total)
	}
	sem, wiki := v.Items[3], v.Items[4]
	if sem.Count != 3 || wiki.Count != 3 || !sem.IsAggregate() || sem.CreatedAt == 0 {
		t.Fatalf("聚合行不对：%+v %+v", sem, wiki)
	}
	if v.Wiki == nil || v.Wiki.Ready != 3 || v.Wiki.Review != 1 {
		t.Fatalf("Wiki 候选应保存下来：%+v", v.Wiki)
	}
}

func TestAggregateRowsAbsentWhenNothingPending(t *testing.T) {
	r := newRig(t)
	r.svc.SetWikiCandidates(services.InboxWikiCandidates{Waiting: 2, Review: 1}) // 没有可编译的（ready 为 0）
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 4, IndexedItems: 4})
	v := r.refreshed(review("a", "甲"))
	if len(v.Items) != 1 || v.Counts.SemanticPending != 0 || v.Counts.WikiPending != 0 {
		t.Fatalf("没有待办时不应有聚合行：%v %+v", rowIDs(v.Items), v.Counts)
	}
}

func TestSemanticNotConfiguredCountsAsZeroNotError(t *testing.T) {
	r := newRig(t)
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: false, EligibleItems: 9})
	v := r.refreshed(review("a", "甲"))
	if v.Counts.SemanticPending != 0 || v.SectionErrors.Semantic != "" {
		t.Fatalf("未配置 embedding 模型是可选增强，不是故障：%+v %+v", v.Counts, v.SectionErrors)
	}
}

func TestSemanticPendingNeverNegative(t *testing.T) {
	r := newRig(t)
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 2, IndexedItems: 5})
	if v := r.refreshed(); v.Counts.SemanticPending != 0 || len(v.Items) != 0 {
		t.Fatalf("已索引多于可索引时待办按 0：%+v", v.Counts)
	}
}

// ---- 刷新：失败 ----

// 主列表失败必须是错误态，而不是「已处理完」的空态；恢复后清掉。
func TestListFailureIsLoadErrorNotEmptyState(t *testing.T) {
	r := newRig(t)
	r.svc.FailOn("List", errors.New("数据库已锁定"))
	r.seed(review("a", "甲"))
	r.store.Refresh(context.Background())
	v := r.view()
	if v.LoadError != "数据库已锁定" || v.IsLoading || v.HasLoaded || len(v.Items) != 0 {
		t.Fatalf("应是加载错误而不是空列表：%+v", v)
	}
	if logs := r.log.entries(); len(logs) != 1 {
		t.Fatalf("加载失败要留日志：%v", logs)
	}
	if len(r.toasts()) != 0 {
		t.Fatalf("加载类失败不弹提示（用户可能只是切了页签）：%+v", r.toasts())
	}

	r.svc.FailOn("List", nil)
	r.store.Refresh(context.Background())
	if v = r.view(); v.LoadError != "" || !v.HasLoaded || len(v.Items) != 1 {
		t.Fatalf("重试成功后应清掉错误：%+v", v)
	}
}

// 刷新失败时保留上一轮的内容：错误态下界面仍可能显示旧数据，不能把它清空。
func TestListFailureKeepsPreviousItems(t *testing.T) {
	r := newRig(t)
	r.refreshed(review("a", "甲"))
	r.svc.FailOn("List", errors.New("超时"))
	r.store.Refresh(context.Background())
	v := r.view()
	if v.LoadError != "超时" || len(v.Items) != 1 || v.Total != 1 {
		t.Fatalf("失败不应清掉旧数据：%+v", v)
	}
}

func TestEmptyErrorMessageBecomesUnknownError(t *testing.T) {
	r := newRig(t)
	r.svc.FailOn("List", errors.New("  \n "))
	r.store.Refresh(context.Background())
	if got := r.view().LoadError; got != locales.CommonUnknownError() {
		t.Fatalf("空的错误原文应换成「未知错误」，实际 %q", got)
	}
}

// Wiki 状态失败不影响主列表；聚合行按 0 处理，失败原因进 SectionErrors；上一次读到的候选保留（与渲染层一致）。
func TestWikiFailureIsSectionErrorOnly(t *testing.T) {
	r := newRig(t)
	r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 2})
	r.refreshed(review("a", "甲"))
	if r.view().Counts.WikiPending != 2 {
		t.Fatal("前置：应先读到 2 条可编译")
	}

	r.svc.FailOn("WikiCandidates", errors.New("预览失败"))
	v := r.refreshed(review("a", "甲"), review("b", "乙"))
	if v.SectionErrors.Wiki != "预览失败" || v.SectionErrors.Semantic != "" || v.LoadError != "" {
		t.Fatalf("只应有 Wiki 一块失败：%+v load=%q", v.SectionErrors, v.LoadError)
	}
	if v.Counts.WikiPending != 0 || len(v.Items) != 2 || v.Counts.ReviewRequired != 2 {
		t.Fatalf("主列表照常、Wiki 聚合行归零：%v %+v", rowIDs(v.Items), v.Counts)
	}
	if v.Wiki == nil || v.Wiki.Ready != 2 {
		t.Fatalf("上一次读到的候选应保留：%+v", v.Wiki)
	}
	if len(r.toasts()) != 0 {
		t.Fatalf("区块读取失败不弹提示（界面用行内提示加重试）：%+v", r.toasts())
	}
}

func TestSemanticFailureIsSectionErrorOnly(t *testing.T) {
	r := newRig(t)
	r.svc.FailOn("SemanticStatus", errors.New("状态读取失败"))
	r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 1})
	v := r.refreshed(review("a", "甲"))
	if v.SectionErrors.Semantic != "状态读取失败" || v.SectionErrors.Wiki != "" || v.Counts.SemanticPending != 0 {
		t.Fatalf("只应有语义一块失败：%+v %+v", v.SectionErrors, v.Counts)
	}
	if v.Counts.WikiPending != 1 || len(v.Items) != 2 {
		t.Fatalf("Wiki 一块不受影响：%v", rowIDs(v.Items))
	}
}

func TestSectionErrorsResetOnNextRefresh(t *testing.T) {
	r := newRig(t)
	r.svc.FailOn("WikiCandidates", errors.New("x"))
	r.svc.FailOn("SemanticStatus", errors.New("y"))
	if v := r.refreshed(); !v.SectionErrors.Any() {
		t.Fatal("前置：两块都应失败")
	}
	r.svc.FailOn("WikiCandidates", nil)
	r.svc.FailOn("SemanticStatus", nil)
	r.store.Refresh(context.Background())
	if v := r.view(); v.SectionErrors.Any() {
		t.Fatalf("下一次刷新成功后应清掉区块错误：%+v", v.SectionErrors)
	}
}

func TestWikiNeedsModelBanner(t *testing.T) {
	r := newRig(t)
	cases := []struct {
		name  string
		cands services.InboxWikiCandidates
		ready bool
		want  bool
	}{
		{"没配模型且有等待的候选：提示去配置", services.InboxWikiCandidates{Waiting: 4}, false, true},
		{"配好了模型：等待就是等待重试", services.InboxWikiCandidates{Waiting: 4}, true, false},
		{"没配模型但没有等待的：不提示", services.InboxWikiCandidates{Review: 2}, false, false},
	}
	for _, c := range cases {
		r.svc.SetWikiCandidates(c.cands)
		r.svc.SetWikiModelReady(c.ready)
		if got := r.refreshed().WikiNeedsModel; got != c.want {
			t.Fatalf("%s：WikiNeedsModel 应是 %v", c.name, c.want)
		}
	}
}

// 读不到模型状态时按「已就绪」处理，不误报「尚未配置」；原因留日志。
func TestWikiModelReadyErrorDoesNotClaimUnconfigured(t *testing.T) {
	r := newRig(t)
	r.svc.SetWikiCandidates(services.InboxWikiCandidates{Waiting: 4})
	r.svc.SetWikiModelReady(false)
	r.svc.FailOn("WikiModelReady", errors.New("设置读取失败"))
	v := r.refreshed()
	if v.WikiNeedsModel || !v.WikiModelReady {
		t.Fatalf("读不到状态时不应显示「尚未配置模型」：%+v", v)
	}
	if len(r.log.entries()) != 1 {
		t.Fatalf("读取失败要留日志：%v", r.log.entries())
	}
}

// ---- 刷新：并发与代次 ----

// 旧一轮的列表晚到，不能覆盖新一轮已落到界面上的结果，也不能把 IsLoading 提前清掉。
func TestStaleRefreshDoesNotOverwriteNewerResult(t *testing.T) {
	r := newRig(t)
	r.useGoroutines()
	r.seed(review("old", "旧"))
	release := r.svc.Hold("List")
	firstDone := make(chan struct{})
	go func() {
		r.store.Refresh(context.Background())
		close(firstDone)
	}()
	waitFor(t, "第一轮的列表请求发出", func() bool { return r.svc.CallCount("List") == 1 })
	if !r.view().IsLoading {
		t.Fatal("请求在途时 IsLoading 应为真")
	}

	r.seed(review("new", "新"))
	r.store.Refresh(context.Background())
	if got := rowIDs(r.view().Items); !slices.Equal(got, []string{"review:new"}) || r.view().IsLoading {
		t.Fatalf("第二轮应已落地：%v loading=%v", got, r.view().IsLoading)
	}

	release()
	waitDone(t, "第一轮结束", firstDone)
	if got := rowIDs(r.view().Items); !slices.Equal(got, []string{"review:new"}) {
		t.Fatalf("旧一轮晚到的结果不应覆盖新结果：%v", got)
	}
}

// 旧一轮的失败同样不能污染新一轮的界面。
func TestStaleRefreshFailureIsIgnored(t *testing.T) {
	r := newRig(t)
	r.useGoroutines()
	r.svc.FailOn("List", errors.New("旧请求失败"))
	release := r.svc.Hold("List")
	firstDone := make(chan struct{})
	go func() {
		r.store.Refresh(context.Background())
		close(firstDone)
	}()
	waitFor(t, "第一轮的列表请求发出", func() bool { return r.svc.CallCount("List") == 1 })

	r.svc.FailOn("List", nil)
	r.refreshed(review("a", "甲"))
	release()
	waitDone(t, "第一轮结束", firstDone)
	if v := r.view(); v.LoadError != "" || len(v.Items) != 1 {
		t.Fatalf("旧请求的失败不应落到界面：%+v", v)
	}
}

// 聚合行与计数来自另外两块：主列表重新读取时沿用上一轮的，不会闪成 0。
func TestListReloadKeepsAggregatesFromPreviousRound(t *testing.T) {
	r := newRig(t)
	r.useGoroutines()
	r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 5})
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 3})
	r.refreshed(review("a", "甲"))

	release := r.svc.Hold("List")
	done := make(chan struct{})
	go func() {
		r.store.Refresh(context.Background())
		close(done)
	}()
	waitFor(t, "三块请求发出", func() bool { return r.svc.CallCount("SemanticStatus") == 2 })
	v := r.view()
	if v.Counts.WikiPending != 5 || v.Counts.SemanticPending != 3 || len(v.Items) != 3 {
		t.Fatalf("主列表还在读取时，聚合行不应消失：%v %+v", rowIDs(v.Items), v.Counts)
	}
	release()
	waitDone(t, "刷新结束", done)
	if v = r.view(); len(v.Items) != 3 || v.Counts.WikiPending != 5 {
		t.Fatalf("刷新结束后聚合行仍在：%v", rowIDs(v.Items))
	}
}

func TestRefreshPrunesSelectionToSurvivingItems(t *testing.T) {
	r := newRig(t)
	r.refreshed(review("a", "甲"), unclassified("b", "乙"), unclassified("c", "丙"))
	r.store.SetSelection([]string{"c", "a", "b"})
	r.refreshed(review("a", "甲"), unclassified("c", "丙"))
	if got := r.view().Selection; !slices.Equal(got, []string{"c", "a"}) {
		t.Fatalf("消失的条目应从选择里剔除，其余保持顺序：%v", got)
	}
}

// 采集问题行没有条目 ID：不会被误当作条目留在选择里。
func TestRefreshPrunesSelectionDoesNotKeepImportRows(t *testing.T) {
	r := newRig(t)
	r.refreshed(review("a", "甲"))
	r.store.SetSelection([]string{"a", "t1"})
	r.refreshed(review("a", "甲"), importIssue("t1", "failed", "x"))
	if got := r.view().Selection; !slices.Equal(got, []string{"a"}) {
		t.Fatalf("采集任务 ID 不是条目 ID，刷新后应被剔除：%v", got)
	}
}

// 聚合行混在主列表里返回时（不该发生）也只认主列表的普通行，聚合行以另两块的结果为准。
func TestRefreshIgnoresAggregateRowsInList(t *testing.T) {
	r := newRig(t)
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 1})
	v := r.refreshed(
		review("a", "甲"),
		services.InboxItem{Kind: services.InboxKindSemanticPending, ID: "aggregate:semantic-pending", Count: 99},
	)
	if len(v.Items) != 2 || v.Items[1].Count != 1 {
		t.Fatalf("聚合行应以语义状态为准：%+v", v.Items)
	}
}

func TestRefreshWakesTheUI(t *testing.T) {
	r := newRig(t)
	before := r.wakes.Load()
	r.refreshed(review("a", "甲"))
	if r.wakes.Load() <= before {
		t.Fatal("刷新应请求重绘")
	}
}

// ---- 分组与可见范围 ----

func mixedItems() []services.InboxItem {
	return []services.InboxItem{
		review("r1", "复核一"), unclassified("u1", "未归一"), unclassified("u2", "未归二"),
		importIssue("t1", "failed", "失败"), importIssue("t2", "completed", "警告"),
	}
}

// 「全部待处理」不含可选整理（未归合集），与角标口径一致。
func TestFilterAllExcludesUnclassified(t *testing.T) {
	r := newRig(t)
	v := r.refreshed(mixedItems()...)
	if got, want := rowIDs(v.Visible), []string{"review:r1", "import:t1", "import:t2"}; !slices.Equal(got, want) {
		t.Fatalf("全部分组的可见行不对：%v want %v", got, want)
	}
}

func TestFilterEachKindShowsOnlyThatKind(t *testing.T) {
	r := newRig(t)
	r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 2})
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 4})
	r.refreshed(mixedItems()...)
	cases := []struct {
		filter Filter
		want   []string
	}{
		{FilterReviewRequired, []string{"review:r1"}},
		{FilterUnclassified, []string{"unclassified:u1", "unclassified:u2"}},
		{FilterImportIssue, []string{"import:t1", "import:t2"}},
		{FilterSemanticPending, []string{"aggregate:semantic-pending"}},
		{FilterWikiPending, []string{"aggregate:wiki-pending"}},
		{FilterDiscovery, []string{}},
	}
	for _, c := range cases {
		r.store.SetFilter(c.filter)
		got := rowIDs(r.view().Visible)
		if !slices.Equal(got, c.want) {
			t.Fatalf("分组 %s 的可见行：got %v want %v", c.filter, got, c.want)
		}
	}
}

func TestSetFilterClearsSelection(t *testing.T) {
	r := newRig(t)
	r.refreshed(mixedItems()...)
	r.store.SetSelection([]string{"r1", "u1"})
	r.store.SetFilter(FilterUnclassified)
	if v := r.view(); len(v.Selection) != 0 || v.Filter != FilterUnclassified {
		t.Fatalf("切换分组应清空选择：%+v", v)
	}
}

func TestSelectableVisibleIDsExcludeAggregatesAndImportRows(t *testing.T) {
	r := newRig(t)
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 1})
	v := r.refreshed(mixedItems()...)
	if got := v.SelectableVisibleIDs; !slices.Equal(got, []string{"r1"}) {
		t.Fatalf("全部分组里只有复核行可勾选（聚合行与采集问题行没有条目 ID）：%v", got)
	}
	r.store.SetFilter(FilterUnclassified)
	if got := r.view().SelectableVisibleIDs; !slices.Equal(got, []string{"u1", "u2"}) {
		t.Fatalf("未归合集分组的可勾选行：%v", got)
	}
	r.store.SetFilter(FilterSemanticPending)
	if got := r.view().SelectableVisibleIDs; len(got) != 0 {
		t.Fatalf("聚合行不可勾选：%v", got)
	}
}

func TestCountOfAndHeaderCount(t *testing.T) {
	r := newRig(t)
	r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 6})
	r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 9, IndexedItems: 4})
	v := r.refreshed(mixedItems()...)
	want := map[Filter]int{
		FilterAll: 3, FilterReviewRequired: 1, FilterImportIssue: 2, FilterUnclassified: 2, FilterSemanticPending: 5, FilterWikiPending: 6,
	}
	for _, f := range FilterOrder {
		if got := v.CountOf(f); got != want[f] {
			t.Fatalf("侧栏 %s 的角标应是 %d，实际 %d", f, want[f], got)
		}
	}
	r.store.SetFilter(FilterWikiPending)
	if got := r.view().HeaderCount(); got != 6 {
		t.Fatalf("顶栏数字应跟随当前分组：%d", got)
	}
	r.store.SetFilter(FilterAll)
	if got := r.view().HeaderCount(); got != 3 {
		t.Fatalf("全部分组的顶栏数字是去重总数：%d", got)
	}
}

func TestFilterOrderMatchesSidebar(t *testing.T) {
	want := []Filter{FilterAll, FilterReviewRequired, FilterImportIssue, FilterUnclassified, FilterSemanticPending, FilterWikiPending}
	if !slices.Equal(FilterOrder, want) {
		t.Fatalf("侧栏顺序：%v", FilterOrder)
	}
}

// ---- 选择 ----

func TestToggleSelectionAddsAndRemovesInOrder(t *testing.T) {
	r := newRig(t)
	r.refreshed(mixedItems()...)
	for _, id := range []string{"u2", "r1", "u1"} {
		r.store.ToggleSelection(id)
	}
	if got := r.view().Selection; !slices.Equal(got, []string{"u2", "r1", "u1"}) {
		t.Fatalf("按点选顺序：%v", got)
	}
	r.store.ToggleSelection("r1")
	if got := r.view().Selection; !slices.Equal(got, []string{"u2", "u1"}) || r.view().IsSelected("r1") || !r.view().IsSelected("u1") {
		t.Fatalf("再点一次取消：%v", got)
	}
}

func TestSetSelectionDeduplicatesKeepingFirstOrder(t *testing.T) {
	r := newRig(t)
	r.store.SetSelection([]string{"b", "a", "b", "c", "a"})
	if got := r.view().Selection; !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Fatalf("去重并保持首次出现的顺序：%v", got)
	}
	r.store.ClearSelection()
	if got := r.view().Selection; len(got) != 0 {
		t.Fatalf("清空：%v", got)
	}
}

// 全选替换而不是追加：其他分组里选中的行被丢弃；取消勾选清空。
func TestSetAllVisibleSelectedReplacesSelection(t *testing.T) {
	r := newRig(t)
	r.refreshed(mixedItems()...)
	r.store.SetSelection([]string{"u1"}) // 不在「全部」分组的可见行里
	r.store.SetAllVisibleSelected(true)
	v := r.view()
	if !slices.Equal(v.Selection, []string{"r1"}) || !v.AllVisibleSelected() || v.SomeVisibleSelected() {
		t.Fatalf("全选应替换为当前可见的可选行：%+v", v)
	}
	r.store.SetAllVisibleSelected(false)
	if v = r.view(); len(v.Selection) != 0 || v.AllVisibleSelected() {
		t.Fatalf("取消全选应清空：%+v", v)
	}
}

func TestAllAndSomeVisibleSelectedStates(t *testing.T) {
	r := newRig(t)
	r.refreshed(unclassified("u1", "一"), unclassified("u2", "二"), unclassified("u3", "三"))
	r.store.SetFilter(FilterUnclassified)

	if v := r.view(); v.AllVisibleSelected() || v.SomeVisibleSelected() {
		t.Fatal("什么都没选时既不是全选也不是半选")
	}
	r.store.ToggleSelection("u1")
	if v := r.view(); v.AllVisibleSelected() || !v.SomeVisibleSelected() || v.SelectedVisibleCount != 1 {
		t.Fatalf("选了一部分应是半选：%+v", v)
	}
	r.store.ToggleSelection("u2")
	r.store.ToggleSelection("u3")
	if v := r.view(); !v.AllVisibleSelected() || v.SomeVisibleSelected() {
		t.Fatalf("全部选中应是全选：%+v", v)
	}
}

// 没有可勾选的行时（比如聚合行分组）不是「全选」，哪怕选择为空。
func TestNoSelectableRowsIsNeverAllSelected(t *testing.T) {
	r := newRig(t)
	r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 1})
	r.refreshed()
	r.store.SetFilter(FilterWikiPending)
	if v := r.view(); v.AllVisibleSelected() || v.SomeVisibleSelected() {
		t.Fatalf("空的可选集合不应算全选：%+v", v)
	}
}

// 选中的未归合集条目按列表顺序给出（智能归类按这个顺序请求），复核条目不在其中。
func TestSelectedUnclassifiedFollowsListOrder(t *testing.T) {
	r := newRig(t)
	r.refreshed(review("r1", "复核"), unclassified("u1", "一"), unclassified("u2", "二"), unclassified("u3", "三"))
	r.store.SetSelection([]string{"u3", "r1", "u1"})
	if got := r.view().SelectedUnclassified; !slices.Equal(got, []string{"u1", "u3"}) {
		t.Fatalf("应按列表顺序且不含复核条目：%v", got)
	}
}

func TestParseTagNames(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a,b", []string{"a", "b"}},
		{"甲，乙", []string{"甲", "乙"}},
		{" a , ,b，， c ", []string{"a", "b", "c"}},
		{"单个标签", []string{"单个标签"}},
		{",,，", nil},
	}
	for _, c := range cases {
		if got := ParseTagNames(c.in); !slices.Equal(got, c.want) {
			t.Fatalf("ParseTagNames(%q) = %v，期望 %v", c.in, got, c.want)
		}
	}
}

func TestImportTargetPrefersDuplicate(t *testing.T) {
	cases := []struct {
		item services.InboxItem
		want string
	}{
		{services.InboxItem{DuplicateItemID: "d", ResultItemID: "r"}, "d"},
		{services.InboxItem{ResultItemID: "r"}, "r"},
		{services.InboxItem{}, ""},
	}
	for _, c := range cases {
		if got := ImportTargetID(c.item); got != c.want {
			t.Fatalf("ImportTargetID(%+v) = %q，期望 %q", c.item, got, c.want)
		}
	}
}

// 快照是写时复制的：拿到之后不会被后台改动。
func TestViewSnapshotIsStable(t *testing.T) {
	r := newRig(t)
	r.refreshed(unclassified("u1", "一"), unclassified("u2", "二"))
	r.store.SetSelection([]string{"u1"})
	snap := r.view()
	r.store.ToggleSelection("u2")
	r.refreshed(unclassified("u3", "三"))
	if !slices.Equal(snap.Selection, []string{"u1"}) || !slices.Equal(rowIDs(snap.Items), []string{"unclassified:u1", "unclassified:u2"}) {
		t.Fatalf("旧快照被后台改动了：%+v", snap)
	}
}

// 并发读写不出数据竞争或死锁（go test -race 下更有意义）。
func TestConcurrentUseDoesNotDeadlock(t *testing.T) {
	r := newRig(t)
	r.useGoroutines()
	r.seed(mixedItems()...)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); r.store.Refresh(context.Background()) }()
		go func() { defer wg.Done(); r.store.ToggleSelection("u1"); r.store.SetFilter(FilterAll) }()
		go func() { defer wg.Done(); _ = r.view() }()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	waitDone(t, "并发操作结束", done)
}
