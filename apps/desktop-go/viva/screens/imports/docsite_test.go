package imports

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 对应渲染层 DocumentSitePanel 的行为：表单与范围、批次与页面明细的轮询、创建与批次操作、失败的呈现。
// 状态机的并发保护（代次、忙碌）用 servicestest.DocSite 的 HoldList / HoldGet 与可编排的创建来构造竞态。

// ---- 测试替身 ----

type fakeCollections struct {
	mu   sync.Mutex
	list []services.LibraryCollection
	err  error
}

func (f *fakeCollections) Collections(context.Context) ([]services.LibraryCollection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return append([]services.LibraryCollection(nil), f.list...), nil
}

func (f *fakeCollections) set(list []services.LibraryCollection, err error) {
	f.mu.Lock()
	f.list, f.err = list, err
	f.mu.Unlock()
}

// gatedDocSite 让 CreateJob 停在 entered 之后、等 release：构造「创建进行中」的忙碌窗口。
type gatedDocSite struct {
	*servicestest.DocSite
	entered chan struct{}
	release chan struct{}
}

func (g *gatedDocSite) CreateJob(ctx context.Context, in services.CrawlInput) (string, error) {
	close(g.entered)
	<-g.release
	return g.DocSite.CreateJob(ctx, in)
}

type docHarness struct {
	svc    *servicestest.DocSite
	cols   *fakeCollections
	store  *DocSiteStore
	clock  *manualClock
	log    *recLog
	notify *kit.Notifier
	wakes  atomic.Int32
}

func newDocHarness(t *testing.T) *docHarness {
	t.Helper()
	h := &docHarness{svc: &servicestest.DocSite{}, cols: &fakeCollections{}, clock: newManualClock(), log: &recLog{}, notify: kit.NewNotifier(nil)}
	h.store = NewDocSiteStore(h.svc, h.cols, kit.Guard{Notify: h.notify, Log: h.log}, func() { h.wakes.Add(1) })
	h.store.After = h.clock.After
	h.store.Spawn = func(fn func()) { fn() } // 默认同步；并发用例自己改
	return h
}

func (h *docHarness) calls(op string) int {
	n := 0
	for _, c := range h.svc.Calls() {
		if c == op || strings.HasPrefix(c, op+":") {
			n++
		}
	}
	return n
}

func (h *docHarness) timers() int {
	h.clock.mu.Lock()
	defer h.clock.mu.Unlock()
	return len(h.clock.timers)
}

func crawlJob(id, status string, counts map[string]int) services.CrawlJob {
	if counts == nil {
		counts = map[string]int{}
	}
	return services.CrawlJob{
		ID: id, Status: status, Counts: counts,
		Input: services.CrawlInput{Seeds: []services.CrawlSeed{{URL: "https://a.com/" + id + "/", Mode: services.CrawlSeedDirectory}}},
	}
}

func crawlPage(job, id, status string) services.CrawlPage {
	return services.CrawlPage{ID: id, JobID: job, URL: "https://a.com/" + id, Status: status}
}

var bg = context.Background()

// ---- 表单 ----

func TestDocSiteDefaultsAndForm(t *testing.T) {
	h := newDocHarness(t)
	v := h.store.View()
	if v.Form.MaxPages != DocSiteDefaultPages || v.Form.MaxDepth != DocSiteDefaultDepth || v.Form.Policy != services.CrawlDuplicateSkip {
		t.Fatalf("默认值不对：%+v", v.Form)
	}
	if v.Scope != "" || v.CanStart() || v.HasLoaded {
		t.Fatalf("初始不应能开始：%+v", v)
	}

	h.store.SetURL("https://docs.example.com/guide/intro.html")
	if got := h.store.View().Scope; got != "https://docs.example.com/guide/" {
		t.Fatalf("范围 = %q", got)
	}
	if !h.store.View().CanStart() {
		t.Fatal("有效范围应能开始")
	}
	h.store.SetDirectory("  /guide/api  ")
	if got := h.store.View().Scope; got != "https://docs.example.com/guide/api/" {
		t.Fatalf("目录两端的空白应被忽略，范围 = %q", got)
	}
	h.store.SetDirectory("guide")
	if h.store.View().Scope != "" || h.store.View().CanStart() {
		t.Fatal("目录无效时范围应为空、不能开始")
	}
	h.store.SetDirectory("")
	if h.store.View().Scope == "" {
		t.Fatal("清空目录后应回到入口所在目录")
	}

	h.store.SetPolicy("bogus")
	if h.store.View().Form.Policy != services.CrawlDuplicateSkip {
		t.Fatal("认不出的策略应按跳过重复")
	}
	h.store.SetPolicy(services.CrawlDuplicateUpdate)
	if h.store.View().Form.Policy != services.CrawlDuplicateUpdate {
		t.Fatal("更新策略没有生效")
	}
	if h.wakes.Load() == 0 {
		t.Fatal("编辑表单应请求重绘")
	}
}

// ---- 读取、轮询与离开页面 ----

func TestDocSiteAttachPollsAndDetaches(t *testing.T) {
	h := newDocHarness(t)
	h.svc.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil))
	h.svc.SetPages("j1", crawlPage("j1", "p1", services.CrawlPagePending))
	h.cols.set([]services.LibraryCollection{{ID: "c1", Name: "docs"}}, nil)
	h.store.Select(bg, "j1") // 挂载前先选中：轮询要连明细一起读

	h.store.Attach(bg)
	h.store.Attach(bg) // 重复挂载无效
	v := h.store.View()
	if !v.HasLoaded || len(v.Jobs) != 1 || len(v.Pages) != 1 || len(v.Collections) != 1 {
		t.Fatalf("挂载后应读到批次、明细与合集：%+v", v)
	}
	base := h.calls("list")
	if h.timers() != 1 {
		t.Fatalf("应只排一个轮询计时器，得到 %d", h.timers())
	}

	h.clock.Advance(time.Second)
	if h.calls("list") != base {
		t.Fatal("不到 2 秒不应再读")
	}
	h.svc.SetPages("j1", crawlPage("j1", "p1", services.CrawlPageAdded), crawlPage("j1", "p2", services.CrawlPageFailed))
	h.clock.Advance(time.Second)
	if h.calls("list") != base+1 {
		t.Fatalf("2 秒后应再读一次，读取次数 %d → %d", base, h.calls("list"))
	}
	if pages := h.store.View().Pages; len(pages) != 2 || pages[0].Status != services.CrawlPageAdded {
		t.Fatalf("轮询应刷新选中批次的明细：%+v", pages)
	}

	h.store.Detach()
	if h.timers() != 0 {
		t.Fatal("离开页面应取消轮询计时器")
	}
	h.clock.Advance(time.Minute)
	if h.calls("list") != base+1 {
		t.Fatal("离开页面后不应再读")
	}
	if !h.store.View().HasLoaded || len(h.store.View().Jobs) != 1 {
		t.Fatal("离开页面应保留旧快照")
	}

	h.store.Attach(bg) // 再次进入：恢复轮询
	if h.calls("list") != base+2 || h.timers() != 1 {
		t.Fatalf("再次挂载应立即读一次并重新排计时：list=%d timers=%d", h.calls("list"), h.timers())
	}
}

func TestDocSiteLoadErrorAndRecovery(t *testing.T) {
	h := newDocHarness(t)
	h.svc.FailOn("list", errors.New("db busy"))
	h.store.Load(bg)
	v := h.store.View()
	if v.HasLoaded || v.LoadError != "db busy" {
		t.Fatalf("首次读取失败应显示加载失败而不是空态：%+v", v)
	}
	h.store.Load(bg)
	h.store.Load(bg)
	if n := len(h.log.entries()); n != 1 {
		t.Fatalf("同一故障只应记一次日志，得到 %d 条：%v", n, h.log.entries())
	}
	if len(h.notify.Queued()) != 0 {
		t.Fatal("后台读取失败不应弹提示")
	}

	h.svc.FailOn("list", nil)
	h.svc.SetJobs(crawlJob("j1", services.CrawlJobCompleted, nil))
	h.store.Load(bg)
	v = h.store.View()
	if !v.HasLoaded || v.LoadError != "" || len(v.Jobs) != 1 {
		t.Fatalf("恢复后应清掉错误：%+v", v)
	}

	// 已加载后再失败：保留旧批次，同时显示错误；恢复后再失败会再记一次。
	h.svc.FailOn("list", errors.New("db busy"))
	h.store.Load(bg)
	if v := h.store.View(); len(v.Jobs) != 1 || v.LoadError == "" {
		t.Fatalf("失败应保留旧批次：%+v", v)
	}
	if n := len(h.log.entries()); n != 2 {
		t.Fatalf("恢复后的新故障应再记一次，得到 %d 条", n)
	}
}

func TestDocSiteLoadErrorWithoutText(t *testing.T) {
	h := newDocHarness(t)
	h.svc.FailOn("list", errors.New(""))
	h.store.Load(bg)
	if got := h.store.View().LoadError; got != locales.WebCaptureLoadJobsError() {
		t.Fatalf("错误没有文字时应用兜底文案，得到 %q", got)
	}
}

func TestDocSiteLoadDetailErrorKeepsSnapshot(t *testing.T) {
	h := newDocHarness(t)
	h.svc.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil))
	h.svc.SetPages("j1", crawlPage("j1", "p1", services.CrawlPagePending))
	h.store.Select(bg, "j1")
	h.svc.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil), crawlJob("j2", services.CrawlJobRunning, nil))
	h.svc.FailOn("get", errors.New("detail failed"))
	h.store.Load(bg)
	v := h.store.View()
	if v.LoadError != "detail failed" || len(v.Jobs) != 1 || len(v.Pages) != 1 {
		t.Fatalf("明细读取失败应整体保留旧快照（批次与明细要一致）：%+v", v)
	}
}

// 慢响应晚于离开页面：结果必须丢弃，不能盖掉状态。
func TestDocSiteStaleLoadAfterDetachIsDropped(t *testing.T) {
	h := newDocHarness(t)
	h.svc.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil))
	var wg sync.WaitGroup
	h.store.Spawn = func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}
	release := h.svc.HoldList()
	h.store.Attach(bg)
	waitFor(t, "读取已发出", func() bool { return h.calls("list") == 1 })

	h.store.Detach()
	release()
	wg.Wait()
	v := h.store.View()
	if v.HasLoaded || len(v.Jobs) != 0 || v.LoadError != "" {
		t.Fatalf("离开页面后到达的旧响应应被丢弃：%+v", v)
	}
	if h.timers() != 0 {
		t.Fatalf("离开页面后不应留下轮询计时器，得到 %d 个", h.timers())
	}
	if len(h.log.entries()) != 0 {
		t.Fatalf("被取消的读取不应记日志：%v", h.log.entries())
	}
}

// 慢响应晚于切换选中批次：旧批次的明细不能盖到新批次下面。
func TestDocSiteStaleLoadAfterSelectIsDropped(t *testing.T) {
	h := newDocHarness(t)
	h.svc.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil), crawlJob("j2", services.CrawlJobRunning, nil))
	h.svc.SetPages("j1", crawlPage("j1", "old", services.CrawlPageAdded))
	h.store.Spawn = func(func()) {} // Select 只改状态，不自动读，由测试控制

	h.store.Select(bg, "j1")
	release := h.svc.HoldGet()
	done := make(chan struct{})
	go func() { defer close(done); h.store.Load(bg) }()
	waitFor(t, "明细读取已发出", func() bool { return h.calls("get") == 1 })

	h.store.Select(bg, "j2")
	release()
	<-done
	v := h.store.View()
	if v.Selected != "j2" || len(v.Pages) != 0 || v.HasLoaded {
		t.Fatalf("切换后到达的旧响应应被丢弃：%+v", v)
	}
}

func TestDocSiteSelect(t *testing.T) {
	h := newDocHarness(t)
	h.svc.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil), crawlJob("j2", services.CrawlJobCompleted, nil))
	h.svc.SetPages("j1", crawlPage("j1", "a", services.CrawlPageAdded))
	h.svc.SetPages("j2", crawlPage("j2", "b", services.CrawlPageFailed), crawlPage("j2", "c", services.CrawlPageAdded))

	h.store.Select(bg, "j1")
	if v := h.store.View(); v.Selected != "j1" || len(v.Pages) != 1 || v.Pages[0].ID != "a" {
		t.Fatalf("选中 j1：%+v", v)
	}
	gets := h.calls("get")
	h.store.Select(bg, "j1")
	if h.calls("get") != gets {
		t.Fatal("重复选中同一批次不应再读")
	}
	h.store.Select(bg, "j2")
	if v := h.store.View(); v.Selected != "j2" || len(v.Pages) != 2 || v.Pages[0].ID != "b" {
		t.Fatalf("切到 j2：%+v", v)
	}
	h.store.Select(bg, "")
	if v := h.store.View(); v.Selected != "" || len(v.Pages) != 0 {
		t.Fatalf("取消选中应清空明细：%+v", v)
	}
}

// PagesLoaded 区分「页还没读到」与「读到了、但这个批次确实没有页」两种状态，防止读取失败时把未加载误当成空列表。
func TestDocSitePagesLoadedFlag(t *testing.T) {
	h := newDocHarness(t)
	h.svc.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil), crawlJob("j2", services.CrawlJobRunning, nil))
	h.svc.SetPages("j2", crawlPage("j2", "b", services.CrawlPageAdded))

	h.store.Select(bg, "j1")
	if v := h.store.View(); v.Selected != "j1" || !v.PagesLoaded || len(v.Pages) != 0 {
		t.Fatalf("读到了、确实没有页：%+v", v)
	}
	h.store.Select(bg, "j2")
	if v := h.store.View(); !v.PagesLoaded || len(v.Pages) != 1 {
		t.Fatalf("切到 j2 后读到一页：%+v", v)
	}
	h.svc.FailOn("get", errors.New("boom"))
	h.store.Select(bg, "j1")
	if v := h.store.View(); v.Selected != "j1" || v.PagesLoaded {
		t.Fatalf("切换后读取失败：不能把「没读到」当成「没有页」：%+v", v)
	}
}

// ---- 目标合集 ----

func TestDocSiteLoadCollections(t *testing.T) {
	h := newDocHarness(t)
	h.cols.set([]services.LibraryCollection{{ID: "c1", Name: "A"}, {ID: "c2", Name: "B"}}, nil)
	h.store.LoadCollections(bg)
	h.store.SetCollection("c2")
	if v := h.store.View(); len(v.Collections) != 2 || v.Form.CollectionID != "c2" {
		t.Fatalf("%+v", v)
	}

	h.cols.set(nil, errors.New("library offline"))
	h.store.LoadCollections(bg)
	if len(h.store.View().Collections) != 2 || h.store.View().Form.CollectionID != "c2" {
		t.Fatal("读取失败应保留原下拉与选择")
	}
	if len(h.log.entries()) != 1 || len(h.notify.Queued()) != 0 {
		t.Fatalf("读取失败只记日志不弹提示：%v", h.log.entries())
	}

	h.cols.set([]services.LibraryCollection{{ID: "c1", Name: "A"}}, nil)
	h.store.LoadCollections(bg)
	if v := h.store.View(); v.Form.CollectionID != "" || len(v.Collections) != 1 {
		t.Fatalf("选着的合集被删后应退回未归合集：%+v", v)
	}
}

func TestDocSiteWithoutCollectionSource(t *testing.T) {
	svc := &servicestest.DocSite{}
	s := NewDocSiteStore(svc, nil, kit.Guard{}, nil)
	s.Spawn = func(fn func()) { fn() }
	s.LoadCollections(bg)
	s.SetURL("https://a.com/x/")
	if s.View().Scope == "" {
		t.Fatal("没有合集来源与重绘回调也应正常工作")
	}
}

// ---- 创建 ----

func TestDocSiteStartBuildsCrawlInput(t *testing.T) {
	h := newDocHarness(t)
	h.cols.set([]services.LibraryCollection{{ID: "c1", Name: "docs"}}, nil)
	h.store.LoadCollections(bg)
	h.store.SetURL("  https://a.com/guide/intro  ")
	h.store.SetDirectory(" /guide/ ")
	h.store.SetMaxPages(30)
	h.store.SetMaxDepth(1)
	h.store.SetPolicy(services.CrawlDuplicateUpdate)
	h.store.SetCollection("c1")

	if !h.store.Start(bg) {
		t.Fatalf("应创建成功：%v", h.log.entries())
	}
	created := h.svc.Created()
	if len(created) != 1 {
		t.Fatalf("应创建一个批次，得到 %d", len(created))
	}
	in := created[0]
	if in.Purpose != "documents" || in.MaxPages != 30 || in.MaxDepth != 1 || in.DuplicatePolicy != services.CrawlDuplicateUpdate {
		t.Fatalf("输入不对：%+v", in)
	}
	if len(in.Seeds) != 1 || in.Seeds[0] != (services.CrawlSeed{URL: "https://a.com/guide/intro", Mode: services.CrawlSeedDirectory, Directory: "/guide/"}) {
		t.Fatalf("入口不对（网址与目录应去掉两端空白）：%+v", in.Seeds)
	}
	if in.CollectionID == nil || *in.CollectionID != "c1" {
		t.Fatalf("合集不对：%v", in.CollectionID)
	}
	if v := h.store.View(); v.Busy || len(v.Jobs) != 1 || !v.HasLoaded {
		t.Fatalf("创建后应读到新批次：%+v", v)
	}
}

func TestDocSiteStartWithoutCollectionOrDirectory(t *testing.T) {
	h := newDocHarness(t)
	h.store.SetURL("https://a.com/guide/intro")
	if !h.store.Start(bg) {
		t.Fatal("应创建成功")
	}
	in := h.svc.Created()[0]
	if in.CollectionID != nil || in.Seeds[0].Directory != "" || in.DuplicatePolicy != services.CrawlDuplicateSkip {
		t.Fatalf("未选合集与目录时不应传：%+v", in)
	}
}

func TestDocSiteStartInvalidScopeDoesNothing(t *testing.T) {
	h := newDocHarness(t)
	h.store.SetURL("ftp://a.com/x/")
	if h.store.Start(bg) {
		t.Fatal("范围无效不应创建")
	}
	if len(h.svc.Calls()) != 0 || len(h.notify.Queued()) != 0 {
		t.Fatalf("不应发请求也不弹提示：%v", h.svc.Calls())
	}
}

func TestDocSiteStartFailureIsReportedAndRetryable(t *testing.T) {
	h := newDocHarness(t)
	h.store.SetURL("https://a.com/guide/intro")
	h.store.SetMaxPages(9999)
	h.svc.FailOn("create", errors.New("maxPages out of range"))

	if h.store.Start(bg) {
		t.Fatal("失败不能报成功")
	}
	if q := h.notify.Queued(); len(q) != 1 || q[0].Kind != kit.ToastError || q[0].Detail != "maxPages out of range" {
		t.Fatalf("应弹一条详情带原因的错误提示：%+v", q)
	}
	if e := h.log.entries(); len(e) != 1 || !strings.Contains(e[0], "maxPages out of range") {
		t.Fatalf("日志应带原因：%v", e)
	}
	v := h.store.View()
	if v.Busy || v.Form.MaxPages != 9999 || v.Form.URL == "" {
		t.Fatalf("失败后表单应保持原样、按钮恢复：%+v", v)
	}
	if h.calls("list") != 0 {
		t.Fatal("失败后不应刷新批次列表")
	}

	h.svc.FailOn("create", nil)
	h.store.SetMaxPages(30)
	if !h.store.Start(bg) {
		t.Fatal("改好之后应能重试成功")
	}
}

func TestDocSiteBusyBlocksConcurrentMutations(t *testing.T) {
	h := newDocHarness(t)
	gated := &gatedDocSite{DocSite: h.svc, entered: make(chan struct{}), release: make(chan struct{})}
	h.store = NewDocSiteStore(gated, h.cols, kit.Guard{Notify: h.notify, Log: h.log}, func() { h.wakes.Add(1) })
	h.store.After = h.clock.After
	h.store.Spawn = func(fn func()) { fn() }
	h.svc.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil))
	h.store.Load(bg)
	h.store.SetURL("https://a.com/guide/intro")

	result := make(chan bool, 1)
	go func() { result <- h.store.Start(bg) }()
	<-gated.entered
	if v := h.store.View(); !v.Busy || v.CanStart() {
		t.Fatalf("创建进行中应忙碌且不能再开始：%+v", v)
	}
	if h.store.Start(bg) || h.store.Pause(bg, "j1") || h.store.Cancel(bg, "j1") || h.store.Resume(bg, "j1") {
		t.Fatal("忙碌时不应再发出第二个操作")
	}
	if h.calls("pause")+h.calls("cancel")+h.calls("resume") != 0 {
		t.Fatalf("忙碌时的操作不应到达服务：%v", h.svc.Calls())
	}

	close(gated.release)
	if !<-result {
		t.Fatal("第一个创建应成功")
	}
	if h.store.View().Busy || len(h.svc.Created()) != 1 {
		t.Fatalf("结束后应恢复，且只创建一个：created=%d", len(h.svc.Created()))
	}
}

// ---- 批次操作 ----

func TestDocSiteJobOperations(t *testing.T) {
	h := newDocHarness(t)
	h.svc.SetJobs(
		crawlJob("j1", services.CrawlJobRunning, nil),
		crawlJob("j2", services.CrawlJobPaused, nil),
		crawlJob("j3", services.CrawlJobRunning, nil),
	)
	h.store.Load(bg)

	status := func(id string) string {
		for _, j := range h.store.View().Jobs {
			if j.ID == id {
				return j.Status
			}
		}
		return ""
	}
	if !h.store.Pause(bg, "j1") || status("j1") != services.CrawlJobPaused {
		t.Fatalf("暂停后应读到新状态，得到 %q", status("j1"))
	}
	if !h.store.Resume(bg, "j2") || status("j2") != services.CrawlJobRunning {
		t.Fatalf("继续后应读到新状态，得到 %q", status("j2"))
	}
	if !h.store.Cancel(bg, "j3") || status("j3") != services.CrawlJobCanceled {
		t.Fatalf("取消后应读到新状态，得到 %q", status("j3"))
	}
	if len(h.notify.Queued()) != 0 {
		t.Fatal("成功不应弹错误提示")
	}
}

func TestDocSiteJobOperationFailuresAreReported(t *testing.T) {
	h := newDocHarness(t)
	h.svc.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil), crawlJob("j2", services.CrawlJobCanceled, nil))
	h.store.Load(bg)
	lists := h.calls("list")

	h.svc.FailOn("pause:j1", errors.New("pause refused"))
	if h.store.Pause(bg, "j1") {
		t.Fatal("暂停失败不能报成功")
	}
	if h.store.Resume(bg, "j2") { // 假服务与处理器一致：已取消的批次不能继续
		t.Fatal("继续已取消的批次应失败")
	}
	if len(h.notify.Queued()) != 2 || len(h.log.entries()) != 2 {
		t.Fatalf("两次失败应各弹一条提示并记日志：toast=%d log=%v", len(h.notify.Queued()), h.log.entries())
	}
	if !strings.Contains(h.log.entries()[0], "pause refused") || !strings.Contains(h.log.entries()[1], "already canceled") {
		t.Fatalf("日志应带各自的原因：%v", h.log.entries())
	}
	if h.calls("list") != lists {
		t.Fatal("失败后不应刷新列表")
	}
	if h.store.View().Busy {
		t.Fatal("失败后应恢复可操作")
	}
}

func TestDocSiteRetry(t *testing.T) {
	h := newDocHarness(t)
	h.svc.SetJobs(
		crawlJob("bad", services.CrawlJobCompleted, map[string]int{services.CrawlPageFailed: 2, services.CrawlPageAdded: 5}),
		crawlJob("good", services.CrawlJobCompleted, map[string]int{services.CrawlPageAdded: 7}),
	)
	h.store.Load(bg)

	if h.store.Retry(bg, "good") || h.store.Retry(bg, "missing") {
		t.Fatal("没有失败页的批次与不存在的批次不应重试")
	}
	if h.calls("retry") != 0 {
		t.Fatalf("不应发出重试请求：%v", h.svc.Calls())
	}
	if !h.store.Retry(bg, "bad") || h.calls("retry:bad") != 1 {
		t.Fatalf("有失败页的批次应重试：%v", h.svc.Calls())
	}

	h.svc.FailOn("retry", errors.New("queue full"))
	if h.store.Retry(bg, "bad") || len(h.notify.Queued()) != 1 {
		t.Fatal("重试失败应报失败并弹提示")
	}
}

// ---- 并发 ----

// 轮询、编辑表单与批次操作同时进行：-race 下不得有数据竞争，结束后状态自洽。
func TestDocSiteConcurrentUse(t *testing.T) {
	svc := &servicestest.DocSite{}
	svc.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil))
	svc.SetPages("j1", crawlPage("j1", "p", services.CrawlPageAdded))
	clock := newManualClock()
	s := NewDocSiteStore(svc, &fakeCollections{}, kit.Guard{Notify: kit.NewNotifier(nil), Log: &recLog{}}, func() {})
	s.After = clock.After
	var wg sync.WaitGroup
	s.Spawn = func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}

	s.Attach(bg)
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			for n := 0; n < 20; n++ {
				s.SetURL("https://a.com/guide/intro")
				s.Select(bg, []string{"j1", "", "j1"}[n%3])
				s.Pause(bg, "j1")
				s.Resume(bg, "j1")
				_ = s.View()
				clock.Advance(docSitePollInterval)
			}
		}(i)
	}
	workers.Wait()
	s.Detach()
	wg.Wait()
	s.Load(bg) // 离开页面会丢弃在途的读取，最后补读一次再核对
	if v := s.View(); v.Busy || !slices.ContainsFunc(v.Jobs, func(j services.CrawlJob) bool { return j.ID == "j1" }) {
		t.Fatalf("结束后状态不自洽：%+v", v)
	}
}
