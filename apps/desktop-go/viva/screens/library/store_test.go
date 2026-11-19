package library

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
)

// 本文件对应渲染层 stores/knowledge.store.ts（筛选与计数）、collection.store.ts、tag.store.ts 里侧栏用到的行为：
// 筛选的切换规则、计数请求的乱序保护、撤销窗口内的隐藏与提交。

// recLog 记录写进日志的失败（Guard 在弹提示的同时写日志，测试以日志断言失败）。
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
	return append([]string(nil), r.logs...)
}

type harness struct {
	t     *testing.T
	svc   *servicestest.Library
	store *Store
	clock *servicestest.ManualClock
	log   *recLog
	ctx   context.Context

	wakeMu sync.Mutex
	wakes  int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, svc: &servicestest.Library{}, clock: servicestest.NewManualClock(), log: &recLog{}, ctx: context.Background()}
	h.store = NewStore(h.svc, kit.Guard{Notify: kit.NewNotifier(nil), Log: h.log}, func() {
		h.wakeMu.Lock()
		h.wakes++
		h.wakeMu.Unlock()
	})
	h.store.After = h.clock.After
	h.store.Spawn = func(fn func()) { fn() } // 默认同步；并发用例自己改回 goroutine
	return h
}

// spawnQueue 让后台动作排队而不执行，由测试决定何时放行（Store.Spawn 的替身）。
// 放行期间新产生的后台动作（RefreshAll 里与计数并行的列表读取）就地执行：队列里的任务等着的正是它们，
// 排到队尾只会让放行永远卡在 WaitGroup 上。
type spawnQueue struct {
	queued   []func()
	draining bool
}

func (q *spawnQueue) Spawn(fn func()) {
	if q.draining {
		fn()
		return
	}
	q.queued = append(q.queued, fn)
}

// Run 依次执行队列里已有的任务。
func (q *spawnQueue) Run() {
	q.draining = true
	defer func() { q.draining = false }()
	for _, fn := range q.queued {
		fn()
	}
	q.queued = nil
}

func (h *harness) wakeCount() int {
	h.wakeMu.Lock()
	defer h.wakeMu.Unlock()
	return h.wakes
}

// seed 装入一组合集与标签，并让 Store 读一遍。
func (h *harness) seed() {
	h.t.Helper()
	h.svc.Seed(
		services.LibraryCounts{All: 12, ByCollection: map[string]int{"c1": 5}, ByTag: map[string]int{"t1": 3}},
		[]services.LibraryCollection{{ID: "c1", Name: "工作"}, {ID: "c2", Name: "阅读", Icon: "📚"}},
		[]services.LibraryTag{{ID: "t1", Name: "go", ColorKey: "blue"}, {ID: "t2", Name: "rust", ColorKey: "red"}},
	)
	h.store.Load(h.ctx)
}

func collectionIDs(v View) []string {
	out := make([]string, len(v.Collections))
	for i, c := range v.Collections {
		out[i] = c.ID
	}
	return out
}

func tagIDs(v View) []string {
	out := make([]string, len(v.Tags))
	for i, tag := range v.Tags {
		out[i] = tag.ID
	}
	return out
}

func (h *harness) lastQuery() services.LibraryFacetQuery {
	h.t.Helper()
	qs := h.svc.CountQueries()
	if len(qs) == 0 {
		h.t.Fatal("还没有发出过计数查询")
	}
	return qs[len(qs)-1]
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

// ---- 初始状态 ----

func TestNewStoreStartsAtAllScopeWithoutCounts(t *testing.T) {
	h := newHarness(t)
	v := h.store.View()
	if v.Filters != (Filters{Scope: services.LibraryScopeAll}) {
		t.Errorf("初始筛选 %+v，期望范围「全部」、没有分面", v.Filters)
	}
	if v.Counts != nil {
		t.Error("还没读到计数时 Counts 应为 nil（渲染层 counts: null），否则页签会显示读到的零")
	}
	if len(v.Collections) != 0 || len(v.Tags) != 0 {
		t.Errorf("初始不该有合集或标签：%+v", v)
	}
}

func TestLoadReadsCountsCollectionsAndTags(t *testing.T) {
	h := newHarness(t)
	h.seed()
	v := h.store.View()
	if v.Counts == nil || v.Counts.All != 12 || v.Counts.ByCollection["c1"] != 5 {
		t.Errorf("计数 %+v", v.Counts)
	}
	if got := collectionIDs(v); !slices.Equal(got, []string{"c1", "c2"}) {
		t.Errorf("合集 %v", got)
	}
	if got := tagIDs(v); !slices.Equal(got, []string{"t1", "t2"}) {
		t.Errorf("标签 %v", got)
	}
	if h.wakeCount() == 0 {
		t.Error("读到数据后没有请求重绘")
	}
}

// ---- 筛选 ----

func TestFilterChangeAppliesImmediatelyAndRecountsWithTheNewQuery(t *testing.T) {
	h := newHarness(t)
	// 让后台读取排队不执行，证明筛选在读取之前就已经生效（渲染层 updateFilters 先 set 再 void refreshAll）。
	var queue spawnQueue
	h.store.Spawn = queue.Spawn
	h.store.SetScope(h.ctx, services.LibraryScopeFavorites)
	if got := h.store.View().Filters.Scope; got != services.LibraryScopeFavorites {
		t.Fatalf("筛选没有立即生效：%q", got)
	}
	if len(h.svc.CountQueries()) != 0 {
		t.Fatal("读取不该在界面线程里同步发生")
	}
	queue.Run()
	if q := h.lastQuery(); q.Scope != services.LibraryScopeFavorites {
		t.Errorf("计数查询的范围 %q", q.Scope)
	}
}

func TestSelectFacetsAreOrthogonalToScopeAndSendTheWholeQuery(t *testing.T) {
	h := newHarness(t)
	h.store.SetScope(h.ctx, services.LibraryScopeArchived)
	h.store.SelectCollection(h.ctx, "c1")
	h.store.SelectTag(h.ctx, "t2")
	h.store.SelectPlatform(h.ctx, "douyin")
	want := Filters{Scope: services.LibraryScopeArchived, CollectionID: "c1", TagID: "t2", Platform: "douyin"}
	if got := h.store.View().Filters; got != want {
		t.Fatalf("筛选 %+v，期望 %+v（范围与三个分面互不清空）", got, want)
	}
	q := h.lastQuery()
	if q.Scope != "archived" || q.CollectionID != "c1" || q.TagID != "t2" || q.Platform != "douyin" {
		t.Errorf("计数查询 %+v 没带齐范围与分面", q)
	}
	// 换范围不清分面
	h.store.SetScope(h.ctx, services.LibraryScopeTrash)
	if got := h.store.View().Filters; got.CollectionID != "c1" || got.TagID != "t2" || got.Platform != "douyin" {
		t.Errorf("换范围清掉了分面：%+v", got)
	}
}

func TestSelectingTheSameFacetAgainClearsIt(t *testing.T) {
	cases := []struct {
		name   string
		pick   func(*Store, context.Context, string)
		get    func(Filters) string
		target string
	}{
		{"合集", (*Store).SelectCollection, func(f Filters) string { return f.CollectionID }, "c1"},
		{"标签", (*Store).SelectTag, func(f Filters) string { return f.TagID }, "t1"},
		{"平台", (*Store).SelectPlatform, func(f Filters) string { return f.Platform }, "bilibili"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.pick(h.store, h.ctx, tc.target)
			if got := tc.get(h.store.View().Filters); got != tc.target {
				t.Fatalf("第一次选中后是 %q", got)
			}
			tc.pick(h.store, h.ctx, tc.target)
			if got := tc.get(h.store.View().Filters); got != "" {
				t.Errorf("再选同一个应取消，仍是 %q", got)
			}
			tc.pick(h.store, h.ctx, tc.target)
			tc.pick(h.store, h.ctx, "other")
			if got := tc.get(h.store.View().Filters); got != "other" {
				t.Errorf("换选另一个应直接切过去，得到 %q", got)
			}
			tc.pick(h.store, h.ctx, "")
			if got := tc.get(h.store.View().Filters); got != "" {
				t.Errorf("传空串应清掉分面，得到 %q", got)
			}
		})
	}
}

func TestClearFacetFiltersKeepsScope(t *testing.T) {
	h := newHarness(t)
	h.store.ApplyFilters(h.ctx, Filters{Scope: "favorites", CollectionID: "c1", TagID: "t1", Platform: "web"})
	h.store.ClearFacetFilters(h.ctx)
	if got := h.store.View().Filters; got != (Filters{Scope: "favorites"}) {
		t.Errorf("清分面后 %+v，范围应保留", got)
	}
}

func TestApplyFiltersSetsAllFourAtOnceAndDropsTheRest(t *testing.T) {
	h := newHarness(t)
	h.store.ApplyFilters(h.ctx, Filters{Scope: "favorites", CollectionID: "c1", TagID: "t1", Platform: "web"})
	h.store.ApplyFilters(h.ctx, Filters{Scope: "all", TagID: "t2"})
	want := Filters{Scope: "all", TagID: "t2"}
	if got := h.store.View().Filters; got != want {
		t.Errorf("智能视图应一次设定四项（未列出的分面清掉）：%+v，期望 %+v", got, want)
	}
}

func TestFilterChangeAlwaysRefreshesEvenWhenNothingChanged(t *testing.T) {
	h := newHarness(t)
	h.store.SetScope(h.ctx, "all")
	h.store.SetScope(h.ctx, "all")
	if got := len(h.svc.CountQueries()); got != 2 {
		t.Errorf("重复点同一个范围发出 %d 次计数查询，期望 2（渲染层不去重）", got)
	}
}

func TestIsTrashActiveRequiresNoFacets(t *testing.T) {
	cases := []struct {
		f    Filters
		want bool
	}{
		{Filters{Scope: "trash"}, true},
		{Filters{Scope: "trash", CollectionID: "c1"}, false},
		{Filters{Scope: "trash", TagID: "t1"}, false},
		{Filters{Scope: "trash", Platform: "web"}, false},
		{Filters{Scope: "all"}, false},
	}
	for _, tc := range cases {
		if got := tc.f.IsTrashActive(); got != tc.want {
			t.Errorf("%+v：IsTrashActive=%v，期望 %v", tc.f, got, tc.want)
		}
	}
}

// ---- 计数 ----

func TestRefreshCountsIgnoresAnOutOfOrderOlderResponse(t *testing.T) {
	h := newHarness(t)
	h.svc.SetCounts(services.LibraryCounts{All: 1})
	release := h.svc.HoldCounts()
	done := make(chan struct{})
	go func() { // 第一次请求带着旧数据卡住
		h.store.RefreshCounts(h.ctx)
		close(done)
	}()
	waitFor(t, "第一次计数请求发出", func() bool { return len(h.svc.CountQueries()) == 1 })

	h.svc.SetCounts(services.LibraryCounts{All: 2})
	h.store.RefreshCounts(h.ctx) // 第二次请求先返回
	if v := h.store.View(); v.Counts == nil || v.Counts.All != 2 {
		t.Fatalf("第二次的结果没有落下：%+v", v.Counts)
	}
	release()
	<-done
	if v := h.store.View(); v.Counts == nil || v.Counts.All != 2 {
		t.Errorf("旧请求晚到覆盖了新结果：%+v（渲染层 countsRequestSeq）", v.Counts)
	}
}

func TestRefreshCountsFailureLogsOnlyAndKeepsTheOldNumbers(t *testing.T) {
	h := newHarness(t)
	h.svc.SetCounts(services.LibraryCounts{All: 7})
	h.store.RefreshCounts(h.ctx)
	h.svc.FailOn("Counts", errors.New("db locked"))
	h.store.RefreshCounts(h.ctx)
	if v := h.store.View(); v.Counts == nil || v.Counts.All != 7 {
		t.Errorf("失败后旧数字没保留：%+v", v.Counts)
	}
	logs := h.log.entries()
	if len(logs) != 1 || !strings.HasPrefix(logs[0], "library.counts|") || !strings.Contains(logs[0], "db locked") {
		t.Errorf("日志 %v，期望一条带原因的 library.counts 记录", logs)
	}
}

func TestStaleCountsFailureIsNotLogged(t *testing.T) {
	h := newHarness(t)
	h.svc.SetCounts(services.LibraryCounts{All: 1})
	h.svc.FailOn("Counts", errors.New("旧请求失败"))
	release := h.svc.HoldCounts()
	done := make(chan struct{})
	go func() {
		h.store.RefreshCounts(h.ctx)
		close(done)
	}()
	waitFor(t, "第一次计数请求发出", func() bool { return len(h.svc.CountQueries()) == 1 })
	h.svc.FailOn("Counts", nil)
	h.store.RefreshCounts(h.ctx)
	release()
	<-done
	if logs := h.log.entries(); len(logs) != 0 {
		t.Errorf("已被新请求取代的旧请求失败不该留痕：%v", logs)
	}
}

// ---- 合集与标签 ----

func TestFetchFailuresAreBackgroundOnly(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.svc.FailOn("Collections", errors.New("c boom"))
	h.svc.FailOn("Tags", errors.New("t boom"))
	h.store.FetchCollections(h.ctx)
	h.store.FetchTags(h.ctx)
	v := h.store.View()
	if len(v.Collections) != 2 || len(v.Tags) != 2 {
		t.Errorf("读取失败不该清掉已有的合集与标签：%+v", v)
	}
	logs := h.log.entries()
	if len(logs) != 2 || !strings.Contains(logs[0], "c boom") || !strings.Contains(logs[1], "t boom") {
		t.Errorf("日志 %v", logs)
	}
}

func TestCreateCollectionRefreshesTheListAndReturnsTheNewOne(t *testing.T) {
	h := newHarness(t)
	h.seed()
	created, err := h.store.CreateCollection(h.ctx, "新合集", "🎯")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "新合集" || created.Icon != "🎯" || created.ID == "" {
		t.Errorf("返回的合集 %+v", created)
	}
	if got := collectionIDs(h.store.View()); !slices.Contains(got, created.ID) {
		t.Errorf("新合集没出现在列表里：%v", got)
	}
}

func TestCreateCollectionFailureReturnsTheErrorAndKeepsTheList(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.svc.FailOn("CreateCollection", errors.New("名称重复"))
	if _, err := h.store.CreateCollection(h.ctx, "工作", ""); err == nil || err.Error() != "名称重复" {
		t.Fatalf("错误应原样返回：%v", err)
	}
	if got := collectionIDs(h.store.View()); len(got) != 2 {
		t.Errorf("失败后列表变了：%v", got)
	}
}

func TestUpdateCollectionAndTagRefreshTheirLists(t *testing.T) {
	h := newHarness(t)
	h.seed()
	if err := h.store.UpdateCollection(h.ctx, "c1", "工作台", ""); err != nil {
		t.Fatal(err)
	}
	if got := h.store.View().Collections[0]; got.Name != "工作台" || got.Icon != "" {
		t.Errorf("合集没改过来：%+v", got)
	}
	name := "golang"
	if err := h.store.UpdateTag(h.ctx, "t1", services.LibraryTagPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if got := h.store.View().Tags[0]; got.Name != "golang" || got.ColorKey != "blue" {
		t.Errorf("标签没改过来或颜色被带掉了：%+v", got)
	}
	h.svc.FailOn("UpdateCollection", errors.New("boom"))
	if err := h.store.UpdateCollection(h.ctx, "c1", "x", ""); err == nil {
		t.Error("失败应返回错误，由弹窗提示并保持打开")
	}
}

func TestRenameTagRefreshesCountsToo(t *testing.T) {
	h := newHarness(t)
	h.seed()
	before := len(h.svc.CountQueries())
	if err := h.store.RenameTag(h.ctx, "t1", "golang"); err != nil {
		t.Fatal(err)
	}
	if got := len(h.svc.CountQueries()); got != before+1 {
		t.Errorf("改名后计数查询 %d 次，期望 %d（渲染层 rename-tag 之后 refreshAll）", got, before+1)
	}
	h.svc.FailOn("UpdateTag", errors.New("boom"))
	if err := h.store.RenameTag(h.ctx, "t1", "x"); err == nil {
		t.Error("改名失败应返回错误")
	}
}

func TestSetTagColorRefreshesAndReportsFailure(t *testing.T) {
	h := newHarness(t)
	h.seed()
	before := len(h.svc.CountQueries())
	if !h.store.SetTagColor(h.ctx, "t1", "pink") {
		t.Fatal("换色应成功")
	}
	if got := h.store.View().Tags[0].ColorKey; got != "pink" {
		t.Errorf("颜色 %q", got)
	}
	if got := len(h.svc.CountQueries()); got != before+1 {
		t.Errorf("换色后计数查询 %d 次，期望 %d", got, before+1)
	}
	h.svc.FailOn("UpdateTag", errors.New("磁盘已满"))
	if h.store.SetTagColor(h.ctx, "t1", "red") {
		t.Fatal("失败时应返回 false")
	}
	logs := h.log.entries()
	if len(logs) != 1 || !strings.Contains(logs[0], "磁盘已满") {
		t.Errorf("失败没有进日志：%v", logs)
	}
	if got := h.store.View().Tags[0].ColorKey; got != "pink" {
		t.Errorf("失败后颜色不该变：%q", got)
	}
}

// ---- 删除（撤销窗口）----

func TestScheduleDeleteHidesNowAndCommitsAfterTheWindow(t *testing.T) {
	h := newHarness(t)
	h.seed()
	d := h.store.ScheduleDeleteCollection(h.ctx, "c1", 0)
	if d == nil {
		t.Fatal("合集存在，应返回待提交的删除")
	}
	if got := collectionIDs(h.store.View()); !slices.Equal(got, []string{"c2"}) {
		t.Errorf("窗口内应已隐藏：%v", got)
	}
	if slices.Contains(h.svc.Calls(), "DeleteCollection:c1") {
		t.Fatal("窗口内不该提交给后端")
	}
	// 撤销窗口是 8 秒（渲染层 collection.store.ts / tag.store.ts 的 DELETION_UNDO_WINDOW_MS = 8000）：
	// 字面量写在这里而不引用常量，常量被改了测试才会红。
	h.clock.Advance(8*time.Second - time.Millisecond)
	if slices.Contains(h.svc.Calls(), "DeleteCollection:c1") {
		t.Fatal("窗口还没到就提交了")
	}
	queriesBefore := len(h.svc.CountQueries())
	h.clock.Advance(time.Millisecond)
	if !slices.Contains(h.svc.Calls(), "DeleteCollection:c1") {
		t.Fatal("窗口结束后没有提交")
	}
	if got := collectionIDs(h.store.View()); !slices.Equal(got, []string{"c2"}) {
		t.Errorf("提交后列表 %v", got)
	}
	if got := len(h.svc.CountQueries()); got != queriesBefore+1 {
		t.Errorf("提交后应刷新计数（渲染层 refreshAll），查询 %d 次，期望 %d", got, queriesBefore+1)
	}
	if h.clock.Pending() != 0 {
		t.Error("提交后不该留着计时器")
	}
}

func TestUndoRestoresTheRowAndNeverCallsTheBackend(t *testing.T) {
	h := newHarness(t)
	h.seed()
	d := h.store.ScheduleDeleteTag(h.ctx, "t1", 0)
	if got := tagIDs(h.store.View()); !slices.Equal(got, []string{"t2"}) {
		t.Fatalf("窗口内应已隐藏：%v", got)
	}
	queriesBefore := len(h.svc.CountQueries())
	d.Undo()
	if got := tagIDs(h.store.View()); !slices.Equal(got, []string{"t1", "t2"}) {
		t.Errorf("撤销后列表 %v", got)
	}
	h.clock.Advance(2 * DeletionUndoWindow)
	if slices.Contains(h.svc.Calls(), "DeleteTag:t1") {
		t.Error("撤销后窗口到期仍提交了删除")
	}
	if h.clock.Pending() != 0 {
		t.Error("撤销后不该留着计时器")
	}
	if got := len(h.svc.CountQueries()); got != queriesBefore {
		t.Errorf("撤销不该刷新计数（渲染层 undo 只重取列表）：查询 %d → %d", queriesBefore, got)
	}
}

func TestRefetchDuringTheWindowKeepsTheRowHidden(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.store.ScheduleDeleteCollection(h.ctx, "c1", 0)
	h.store.FetchCollections(h.ctx) // 后端还没删：返回里仍有 c1
	if got := collectionIDs(h.store.View()); !slices.Equal(got, []string{"c2"}) {
		t.Errorf("窗口内重读让被删的行闪回来了：%v", got)
	}
	h.store.Load(h.ctx)
	if got := collectionIDs(h.store.View()); !slices.Equal(got, []string{"c2"}) {
		t.Errorf("窗口内整体重读让被删的行闪回来了：%v", got)
	}
}

func TestCommitFailureBringsTheRowBackAndReportsTheReason(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.svc.FailOn("DeleteTag", errors.New("数据库被占用"))
	d := h.store.ScheduleDeleteTag(h.ctx, "t2", 0)
	d.Commit()
	if got := tagIDs(h.store.View()); !slices.Equal(got, []string{"t1", "t2"}) {
		t.Errorf("提交失败后被删的标签应随刷新回来：%v", got)
	}
	logs := h.log.entries()
	if len(logs) != 1 || !strings.Contains(logs[0], "数据库被占用") {
		t.Errorf("失败原因没有进日志：%v", logs)
	}
}

func TestCommitReportsAnAlreadyMissingTarget(t *testing.T) {
	h := newHarness(t)
	h.seed()
	d := h.store.ScheduleDeleteCollection(h.ctx, "c1", 0)
	// 窗口内别处把它删掉了：后端报告没有删到任何行。
	if removed, err := h.svc.DeleteCollection(h.ctx, "c1"); err != nil || !removed {
		t.Fatalf("准备失败：%v %v", removed, err)
	}
	d.Commit()
	logs := h.log.entries()
	if len(logs) != 1 || !strings.Contains(logs[0], "合集已不存在，请刷新列表") {
		t.Errorf("没有给出「已不存在」的提示：%v", logs)
	}
	tagLogsBefore := len(logs)
	td := h.store.ScheduleDeleteTag(h.ctx, "t1", 0)
	if _, err := h.svc.DeleteTag(h.ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	td.Commit()
	logs = h.log.entries()
	if len(logs) != tagLogsBefore+1 || !strings.Contains(logs[len(logs)-1], "标签已不存在，请刷新列表") {
		t.Errorf("标签没有给出「已不存在」的提示：%v", logs)
	}
}

func TestScheduleDeleteRejectsMissingOrAlreadyPendingTargets(t *testing.T) {
	h := newHarness(t)
	h.seed()
	if h.store.ScheduleDeleteCollection(h.ctx, "nope", 0) != nil {
		t.Error("不存在的合集应返回 nil")
	}
	if h.store.ScheduleDeleteCollection(h.ctx, "c1", 0) == nil {
		t.Fatal("第一次应成功")
	}
	if h.store.ScheduleDeleteCollection(h.ctx, "c1", 0) != nil {
		t.Error("已在撤销窗口里的合集再删应返回 nil（也因为它已不在列表里）")
	}
	// 标签与合集的 ID 空间互不影响
	if h.store.ScheduleDeleteTag(h.ctx, "c1", 0) != nil {
		t.Error("合集 ID 不该匹配到标签")
	}
}

func TestCommitAndUndoOnlyTakeEffectOnce(t *testing.T) {
	h := newHarness(t)
	h.seed()
	d := h.store.ScheduleDeleteCollection(h.ctx, "c1", 0)
	d.Commit()
	d.Commit()
	d.Undo()
	count := 0
	for _, call := range h.svc.Calls() {
		if call == "DeleteCollection:c1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("后端收到 %d 次删除，期望 1", count)
	}
	if got := collectionIDs(h.store.View()); !slices.Equal(got, []string{"c2"}) {
		t.Errorf("提交之后的撤销不该把行带回来：%v", got)
	}
}

func TestFlushRemovalsCommitsEveryPendingDeletionNow(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.store.ScheduleDeleteCollection(h.ctx, "c1", 0)
	h.store.ScheduleDeleteTag(h.ctx, "t1", 0)
	h.store.FlushRemovals()
	calls := h.svc.Calls()
	if !slices.Contains(calls, "DeleteCollection:c1") || !slices.Contains(calls, "DeleteTag:t1") {
		t.Errorf("关窗口前没有提交：%v", calls)
	}
	if h.clock.Pending() != 0 {
		t.Error("提交后不该留着计时器")
	}
	h.store.FlushRemovals() // 再来一次什么都不做
}

// ---- 采集完成 ----

func TestImportCompletionRefreshesCountsAndOnlyTagsWhenTheTaskCarriedThem(t *testing.T) {
	h := newHarness(t)
	h.store.OnImportCompleted(h.ctx, services.ImportTask{ID: "a"})
	if slices.Contains(h.svc.Calls(), "Tags") {
		t.Error("任务没带标签，不该重读标签列表")
	}
	if got := len(h.svc.CountQueries()); got != 1 {
		t.Fatalf("采集完成应刷新计数：%d 次", got)
	}
	h.store.OnImportCompleted(h.ctx, services.ImportTask{ID: "b", TagNames: []string{"新标签"}})
	if !slices.Contains(h.svc.Calls(), "Tags") {
		t.Error("任务带了标签，应重读标签列表")
	}
}
