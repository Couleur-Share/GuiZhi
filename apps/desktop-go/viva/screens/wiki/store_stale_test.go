package wiki

import (
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
)

// 目录刷新：先发的读取迟到，不能盖掉后发的结果（也不能让加载态永远悬着）。
func TestRefreshStaleResultDropped(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	release, arrived := r.svc.Hold(servicestest.WikiOpCatalog)
	wait := inBackground(func() { r.store.Refresh() })
	<-arrived // 这一次读到的是 3 页的旧目录
	r.page("delta")
	r.store.Refresh() // 后发先至：4 页
	release()
	wait()
	if got := len(r.view().Catalog); got != 4 {
		t.Fatalf("旧响应迟到不能把目录退回 3 页：%d", got)
	}
}

func TestSearchStaleResultDropped(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	release, arrived := r.svc.Hold(servicestest.WikiOpSearch)
	wait := inBackground(func() { r.store.RunSearch("alpha") })
	<-arrived
	if v := r.view(); !v.Searching {
		t.Fatal("搜索中")
	}
	r.store.RunSearch("gamma")
	release()
	wait()
	v := r.view()
	wantIDs(t, v.Visible(), "gamma")
	if v.Searching {
		t.Fatalf("后发的结果落地后搜索态收尾：%+v", v)
	}
}

func TestClearingSearchWhileInFlightDropsLateHits(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	release, arrived := r.svc.Hold(servicestest.WikiOpSearch)
	wait := inBackground(func() { r.store.RunSearch("alpha") })
	<-arrived
	r.store.RunSearch("") // 用户清空了输入框
	release()
	wait()
	v := r.view()
	if v.SearchActive || v.Searching || len(v.Visible()) != 3 {
		t.Fatalf("清空后迟到的命中不能让搜索重新生效：%+v", v)
	}
}

func TestGraphStaleResultDropped(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	release, arrived := r.svc.Hold(servicestest.WikiOpGraph)
	wait := inBackground(func() { r.store.LoadGraph() })
	<-arrived
	r.page("delta")
	r.store.LoadGraph()
	release()
	wait()
	v := r.view()
	if v.Graph == nil || len(v.Graph.Nodes) != 4 || v.GraphLoading {
		t.Fatalf("%+v", v.Graph)
	}
}

func TestRefreshCompileCountsStaleDropped(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.SetPreview(previewOf(candidate("a", services.WikiCandidateReady, 1, 0)))
	r.ready()
	release, arrived := r.svc.Hold(servicestest.WikiOpPreview)
	wait := inBackground(func() { r.store.Refresh() })
	<-arrived // 读到 1 条待编译
	r.svc.SetPreview(previewOf(
		candidate("a", services.WikiCandidateReady, 1, 0),
		candidate("b", services.WikiCandidateReady, 1, 0)))
	r.store.Refresh() // 2 条
	release()
	wait()
	if v := r.view(); v.CompileCounts == nil || v.CompileCounts.Ready != 2 {
		t.Fatalf("旧的待编译计数迟到不能盖掉新的：%+v", v.CompileCounts)
	}
}
