package wiki

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

func TestInitializeLoadsCatalogStatusAndCounts(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.SetStatusCounts(2, 5)
	r.svc.SetPreview(previewOf(candidate("i1", services.WikiCandidateReady, 3, 0), candidate("i2", services.WikiCandidateUpgrade, 1, 0)))

	if v := r.view(); v.HasLoaded || v.Mode != ViewCatalog || v.Filter != FilterAll || v.Sort != SortRecent {
		t.Fatalf("初始状态：%+v", v)
	}
	r.ready()
	v := r.view()
	wantIDs(t, v.Catalog, "gamma", "beta", "alpha")
	if v.Status == nil || *v.Status != (services.WikiStatus{PageCount: 3, CompiledItemCount: 2, EligibleItemCount: 5}) {
		t.Fatalf("状态：%+v", v.Status)
	}
	if v.BacklinkCounts["beta"] != 1 || v.BacklinkCounts["alpha"] != 0 {
		t.Fatalf("反向链接计数：%v", v.BacklinkCounts)
	}
	if pending, actionable := v.CompileButton(); pending != 1 || !actionable || v.CompileCounts == nil || v.CompileCounts.Upgrade != 1 {
		t.Fatalf("编译预览计数：%+v", v.CompileCounts)
	}
	if got := v.Counts(); got.All != 3 || got.Entity != 1 || got.Manual != 1 || got.Orphan != 2 {
		t.Fatalf("筛选计数：%+v", got)
	}
	if r.wakes.Load() == 0 {
		t.Fatal("状态变化应请求重绘")
	}
}

func TestInitializeIsIdempotent(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.Initialize()
	r.store.Initialize()
	if n := r.svc.CatalogCalls(); n != 1 {
		t.Fatalf("重复 Initialize 不应重复加载：目录读了 %d 次", n)
	}
}

func TestInitializeFailureShowsLoadErrorAndRetries(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.Fail(servicestest.WikiOpCatalog, errors.New("db locked"))
	r.store.Initialize()
	v := r.view()
	if v.LoadError != "db locked" || !v.HasLoaded || len(v.Catalog) != 0 {
		t.Fatalf("失败要显示错误态而不是空态，且放行加载态：%+v", v)
	}
	if got := r.log.entries(); len(got) == 0 || !strings.Contains(got[0], "wiki.refresh|db locked") {
		t.Fatalf("后台失败应留痕：%v", got)
	}
	r.wantNoToast() // 没有用户操作，不弹提示

	r.svc.Fail(servicestest.WikiOpCatalog, nil)
	r.store.Initialize() // 失败后再次进入会重试
	if v := r.view(); v.LoadError != "" || len(v.Catalog) != 3 {
		t.Fatalf("重试成功：%+v", v)
	}
}

func TestRefreshFailureFromAnyOfTheThreeReads(t *testing.T) {
	for _, op := range []string{servicestest.WikiOpCatalog, servicestest.WikiOpStatus, servicestest.WikiOpBacklinks} {
		r := newRig(t)
		r.seed()
		r.svc.Fail(op, errors.New("boom-"+op))
		r.store.Refresh()
		v := r.view()
		if v.LoadError != "boom-"+op || !v.HasLoaded {
			t.Fatalf("%s：%+v", op, v)
		}
		if len(v.Catalog) != 0 || v.Status != nil {
			t.Fatalf("%s：失败时不应半途应用部分结果：%+v", op, v)
		}
	}
}

func TestRefreshKeepsOldDataOnFailure(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.svc.Fail(servicestest.WikiOpStatus, errors.New("later failure"))
	r.store.Refresh()
	v := r.view()
	if v.LoadError == "" || len(v.Catalog) != 3 {
		t.Fatalf("刷新失败要有错误态，旧目录保留：%+v", v)
	}
}

func TestRefreshCompileCountsFailureIsUnknownNotBlocking(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.SetPreview(previewOf(candidate("i1", services.WikiCandidateReady, 1, 0)))
	r.ready()
	if r.view().CompileCounts == nil {
		t.Fatal("先有计数")
	}
	r.svc.Fail(servicestest.WikiOpPreview, errors.New("preview failed"))
	r.store.Refresh()
	v := r.view()
	if v.CompileCounts != nil || v.LoadError != "" {
		t.Fatalf("预览失败：计数置为未知，但不影响目录加载：%+v", v)
	}
	if pending, actionable := v.CompileButton(); pending != 0 || !actionable {
		t.Fatal("预览未知时按钮不拦")
	}
	found := false
	for _, e := range r.log.entries() {
		found = found || strings.Contains(e, "wiki.compilePreview|preview failed")
	}
	if !found {
		t.Fatalf("预览失败应留痕：%v", r.log.entries())
	}
}

func TestRefreshSyncsSelectedPageAndRevisions(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	if v := r.view(); v.Detail == nil || v.Detail.Page.Body != "body of alpha" || len(v.Revisions) != 0 {
		t.Fatalf("选中：%+v", v)
	}
	// 页面在别处被改写（比如编译）：刷新后详情与历史版本都跟上。
	if _, err := r.svc.UpdatePage(t.Context(), services.WikiUpdatePageInput{PageID: "alpha", Body: "rewritten"}); err != nil {
		t.Fatal(err)
	}
	r.store.Refresh()
	v := r.view()
	if v.Detail.Page.Body != "rewritten" || len(v.Revisions) != 1 {
		t.Fatalf("刷新应同步选中页：%+v", v.Detail.Page)
	}
}

func TestRefreshClearsSelectionWhenPageGone(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("gamma")
	r.store.BeginEdit()
	if _, err := r.svc.DeletePage(t.Context(), "gamma"); err != nil {
		t.Fatal(err)
	}
	r.store.Refresh()
	v := r.view()
	if v.SelectedPageID != "" || v.Detail != nil || v.Editing || v.Draft != "" || len(v.Revisions) != 0 {
		t.Fatalf("选中页被删后应取消选中并丢弃草稿：%+v", v)
	}
	if s := r.sview(); s.PageID != "" {
		t.Fatalf("更新建议状态也应清空：%+v", s)
	}
}

func TestSetFilterAndSortDriveVisible(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	wantIDs(t, r.view().Visible(), "gamma", "beta", "alpha")

	r.store.SetFilter(FilterEntity)
	wantIDs(t, r.view().Visible(), "gamma")
	r.store.SetFilter(FilterManual)
	wantIDs(t, r.view().Visible(), "beta")
	r.store.SetFilter(FilterOrphan)
	wantIDs(t, r.view().Visible(), "gamma", "alpha") // beta 被 alpha 引用，不是孤立页

	r.store.SetFilter(FilterAll)
	r.store.SetSort(SortTitle)
	wantIDs(t, r.view().Visible(), "alpha", "beta", "gamma")
	r.store.SetSort(SortLinked)
	wantIDs(t, r.view().Visible(), "beta", "gamma", "alpha")
}

func TestRunSearchSuccessAndClear(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.page("delta", func(p *services.WikiPage) { p.Title = "Delta"; p.Body = "mentions alpha somewhere" })
	r.ready()

	r.store.RunSearch("  alpha ")
	v := r.view()
	if !v.SearchActive || v.Searching || v.SearchError != "" || v.SearchQuery != "  alpha " {
		t.Fatalf("搜索后：%+v", v)
	}
	wantIDs(t, v.Visible(), "alpha", "delta") // 标题命中在前
	if got := r.svc.Searches(); len(got) != 1 || got[0].Query != "alpha" || got[0].Limit != SearchLimit {
		t.Fatalf("搜索请求应去掉首尾空白并带上限：%+v", got)
	}

	r.store.SetFilter(FilterEntity)
	if got := r.view().Visible(); len(got) != 0 {
		t.Fatalf("搜索命中与筛选叠加：%v", ids(got))
	}
	r.store.SetFilter(FilterAll)

	r.store.RunSearch("   ")
	v = r.view()
	if v.SearchActive || v.SearchHitIDs != nil || v.SearchQuery != "   " || v.Searching {
		t.Fatalf("空白查询清空命中集合：%+v", v)
	}
	if len(r.svc.Searches()) != 1 {
		t.Fatal("空白查询不发请求")
	}
	if len(v.Visible()) != 4 {
		t.Fatal("清空搜索后回到完整目录")
	}
}

func TestRunSearchZeroHitsIsNotTheFullCatalog(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.RunSearch("nothing matches this")
	v := r.view()
	if !v.SearchActive || len(v.SearchHitIDs) != 0 || len(v.Visible()) != 0 || v.SearchError != "" {
		t.Fatalf("搜到零条：%+v", v)
	}
}

func TestRunSearchFailureIsNotRenderedAsNoResults(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.svc.Fail(servicestest.WikiOpSearch, errors.New("fts offline"))
	r.store.RunSearch("alpha")
	v := r.view()
	if v.SearchError != "fts offline" || !v.SearchActive || len(v.SearchHitIDs) != 0 || v.Searching {
		t.Fatalf("搜索失败要有错误态：%+v", v)
	}
	r.wantNoToast()
	if got := r.log.entries(); !slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "wiki.search|fts offline") }) {
		t.Fatalf("应留痕：%v", got)
	}
	r.svc.Fail(servicestest.WikiOpSearch, nil)
	r.store.RunSearch("alpha")
	if v := r.view(); v.SearchError != "" || len(v.SearchHitIDs) == 0 {
		t.Fatalf("重试成功后错误清除：%+v", v)
	}
}

func TestSetViewModeGraphLoadsGraph(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SetViewMode(ViewGraph)
	v := r.view()
	if v.Mode != ViewGraph || v.Graph == nil || len(v.Graph.Nodes) != 3 || len(v.Graph.Edges) != 1 || v.GraphLoading || v.GraphError != "" {
		t.Fatalf("图谱：%+v", v)
	}
	r.store.SetViewMode(ViewCatalog)
	if v := r.view(); v.Mode != ViewCatalog || v.Graph == nil {
		t.Fatal("切回目录保留已加载的图谱")
	}
	if r.svc.GraphCalls() != 1 {
		t.Fatalf("切回目录不重新加载：%d", r.svc.GraphCalls())
	}
}

func TestLoadGraphFailureKeepsOldGraphAndRetries(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SetViewMode(ViewGraph)
	r.svc.Fail(servicestest.WikiOpGraph, errors.New("graph query failed"))
	r.store.LoadGraph()
	v := r.view()
	if v.GraphError != "graph query failed" || v.Graph == nil || v.GraphLoading {
		t.Fatalf("失败：错误态 + 旧图谱保留：%+v", v)
	}
	r.svc.Fail(servicestest.WikiOpGraph, nil)
	r.store.LoadGraph()
	if v := r.view(); v.GraphError != "" || v.Graph == nil {
		t.Fatalf("重试成功：%+v", v)
	}
}

func TestRefreshInGraphModeReloadsGraph(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SetViewMode(ViewGraph)
	r.page("delta")
	r.store.Refresh()
	if v := r.view(); len(v.Graph.Nodes) != 4 {
		t.Fatalf("图谱视图打开时刷新应同步节点：%d", len(v.Graph.Nodes))
	}
	r.store.SetViewMode(ViewCatalog)
	before := r.svc.GraphCalls()
	r.store.Refresh()
	if r.svc.GraphCalls() != before {
		t.Fatal("目录视图下刷新不重读图谱")
	}
}

func TestOpenFromGraphSelectsPageAndReturnsToCatalog(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SetViewMode(ViewGraph)
	r.store.OpenFromGraph("beta")
	v := r.view()
	if v.Mode != ViewCatalog || v.SelectedPageID != "beta" || v.Detail == nil || v.Detail.Page.ID != "beta" {
		t.Fatalf("%+v", v)
	}
}

func TestSetCompilingAndOpenSource(t *testing.T) {
	r := newRig(t)
	r.store.SetCompiling(true)
	if !r.view().IsCompiling {
		t.Fatal("编译中标记")
	}
	r.store.SetCompiling(false)
	if r.view().IsCompiling {
		t.Fatal("编译中标记复位")
	}
	r.store.OpenSource("item-9")
	if got := r.openedSources(); !slices.Equal(got, []string{"item-9"}) {
		t.Fatalf("打开来源：%v", got)
	}
	r.store.OnOpenSource = nil
	r.store.OpenSource("item-10") // 没有接线不应 panic
}

func TestOnContributionsUpdatedRefreshesCatalog(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.page("delta")
	r.store.OnContributionsUpdated()
	if len(r.view().Catalog) != 4 {
		t.Fatal("一轮编译结束后应重读目录")
	}
}

func TestViewDirtyAndLocalesAreWired(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	if r.view().Dirty() {
		t.Fatal("没进编辑不脏")
	}
	r.store.BeginEdit()
	if r.view().Dirty() {
		t.Fatal("刚进编辑草稿等于正文，不脏")
	}
	r.store.SetDraft("changed")
	if !r.view().Dirty() {
		t.Fatal("草稿与正文不同即脏")
	}
	r.store.SetDraft("body of alpha")
	if r.view().Dirty() {
		t.Fatal("改回去就不脏了")
	}
	if locales.WikiPageSaved() == "" || locales.WikiPageSaveFailed() == "" {
		t.Fatal("本地化键缺失")
	}
}
