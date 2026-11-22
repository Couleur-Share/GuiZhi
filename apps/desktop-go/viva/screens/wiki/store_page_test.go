package wiki

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// inBackground 在 goroutine 里跑 fn，返回等待它结束的函数；用来在调用卡住时插入别的操作。
func inBackground(fn func()) (wait func()) {
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	return func() { <-done }
}

func TestSelectPageLoadsDetailAndRevisions(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSources("alpha", services.WikiPageSource{ItemID: "item-1", Title: "Source One"})
	r.svc.PutRevision(services.WikiRevision{PageID: "alpha", Body: "old"})
	r.ready()
	r.store.SelectPage("alpha")
	v := r.view()
	if v.SelectedPageID != "alpha" || v.PageLoading || v.PageError != "" || v.Detail == nil {
		t.Fatalf("%+v", v)
	}
	if len(v.Detail.Sources) != 1 || len(v.Revisions) != 1 || len(v.Detail.Backlinks) != 0 {
		t.Fatalf("详情：%+v 历史：%d", v.Detail, len(v.Revisions))
	}
	r.store.SelectPage("beta")
	if v := r.view(); len(v.Detail.Backlinks) != 1 || v.Detail.Backlinks[0].ID != "alpha" {
		t.Fatalf("Beta 被 Alpha 引用：%+v", v.Detail.Backlinks)
	}
}

func TestSelectPageSwitchDropsOldDetailAndDraftImmediately(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	r.store.BeginEdit()
	r.store.SetDraft("unsaved alpha edit")

	release, arrived := r.svc.Hold(servicestest.WikiOpGetPage)
	wait := inBackground(func() { r.store.SelectPage("beta") })
	<-arrived
	v := r.view()
	if v.SelectedPageID != "beta" || v.Detail != nil || len(v.Revisions) != 0 || !v.PageLoading || v.Editing || v.Draft != "" {
		t.Fatalf("加载 Beta 期间不能显示 Alpha 的内容或草稿：%+v", v)
	}
	release()
	wait()
	if v := r.view(); v.Detail == nil || v.Detail.Page.ID != "beta" || v.PageLoading || v.Editing {
		t.Fatalf("%+v", v)
	}
}

func TestSelectSamePageKeepsDetailAndDraft(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	r.store.BeginEdit()
	r.store.SetDraft("work in progress")
	r.store.SelectPage("alpha") // 重试 / 刷新同一页不丢草稿
	if v := r.view(); !v.Editing || v.Draft != "work in progress" || v.Detail == nil {
		t.Fatalf("%+v", v)
	}
}

func TestSelectPageStaleResultIsDropped(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	release, arrived := r.svc.Hold(servicestest.WikiOpGetPage)
	waitA := inBackground(func() { r.store.SelectPage("alpha") })
	<-arrived
	r.store.SelectPage("beta") // 后发先至
	release()
	waitA()
	v := r.view()
	if v.SelectedPageID != "beta" || v.Detail == nil || v.Detail.Page.ID != "beta" || v.PageLoading {
		t.Fatalf("先发的 Alpha 迟到后不能覆盖 Beta：%+v", v)
	}
	if s := r.sview(); s.PageID != "beta" {
		t.Fatalf("更新建议也跟随 Beta：%+v", s)
	}
}

func TestSelectPageErrorShowsPageErrorAndRetries(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	for _, op := range []string{servicestest.WikiOpGetPage, servicestest.WikiOpListRevisions} {
		r.svc.Fail(op, errors.New("read failed: "+op))
		r.store.SelectPage("alpha")
		v := r.view()
		if v.PageError != "read failed: "+op || v.PageLoading || v.Detail != nil || v.SelectedPageID != "alpha" {
			t.Fatalf("%s：%+v", op, v)
		}
		r.svc.Fail(op, nil)
	}
	r.wantNoToast()
	r.store.SelectPage("alpha")
	if v := r.view(); v.PageError != "" || v.Detail == nil {
		t.Fatalf("重试成功：%+v", v)
	}
}

func TestSelectPageMissingClearsSelection(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("ghost")
	if v := r.view(); v.SelectedPageID != "" || v.Detail != nil || v.PageLoading || v.PageError != "" {
		t.Fatalf("页面不存在：取消选中而不是卡在加载态：%+v", v)
	}
}

func TestSelectEmptyIDClears(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	r.store.BeginEdit()
	r.store.SelectPage("")
	v := r.view()
	if v.SelectedPageID != "" || v.Detail != nil || v.Editing || v.Draft != "" {
		t.Fatalf("%+v", v)
	}
	if r.sview().PageID != "" {
		t.Fatal("更新建议状态也清空")
	}
}

func TestClearDuringLoadDropsResult(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	release, arrived := r.svc.Hold(servicestest.WikiOpGetPage)
	wait := inBackground(func() { r.store.SelectPage("alpha") })
	<-arrived
	r.store.SelectPage("")
	release()
	wait()
	if v := r.view(); v.SelectedPageID != "" || v.Detail != nil || v.PageLoading {
		t.Fatalf("取消选中后迟到的结果不能把详情带回来：%+v", v)
	}
}

func TestOpenByLinkTarget(t *testing.T) {
	r := newRig(t)
	r.page("alpha", func(p *services.WikiPage) { p.Title = "Alpha"; p.NormalizedTitle = "ALPHA" })
	a := `["Short"]`
	r.page("beta", func(p *services.WikiPage) { p.Title = "Beta Page"; p.NormalizedTitle = "BETA PAGE"; p.AliasesJSON = &a })
	r.ready()

	if !r.store.OpenByLinkTarget("alpha") {
		t.Fatal("标题（忽略大小写）应命中")
	}
	if r.view().SelectedPageID != "alpha" {
		t.Fatal("选中 alpha")
	}
	if !r.store.OpenByLinkTarget("Short") || r.view().SelectedPageID != "beta" {
		t.Fatal("别名应命中")
	}
	r.wantNoToast()
	if r.store.OpenByLinkTarget("nonexistent") {
		t.Fatal("找不到应返回 false")
	}
	r.wantToast(kit.ToastInfo, locales.WikiLinkNotFound("nonexistent"), "")
	if r.view().SelectedPageID != "beta" {
		t.Fatal("找不到时不动当前选中")
	}
}

func TestBeginEditNeedsDetailAndIsIdempotent(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.BeginEdit()
	if r.view().Editing {
		t.Fatal("没有详情不能编辑")
	}
	r.store.SetDraft("x")
	if r.view().Draft != "" {
		t.Fatal("不在编辑中忽略 SetDraft")
	}
	r.store.SelectPage("alpha")
	r.store.BeginEdit()
	r.store.SetDraft("typed")
	r.store.BeginEdit() // 已在编辑：不重置草稿
	if v := r.view(); !v.Editing || v.Draft != "typed" {
		t.Fatalf("%+v", v)
	}
	r.store.CancelEdit()
	if v := r.view(); v.Editing || v.Draft != "" {
		t.Fatalf("%+v", v)
	}
	if len(r.svc.Updates()) != 0 {
		t.Fatal("取消编辑不写库")
	}
}

func TestSaveDraftSuccess(t *testing.T) {
	r := newRig(t)
	r.page("alpha", func(p *services.WikiPage) { p.Title = "Alpha"; p.NormalizedTitle = "ALPHA" })
	r.page("beta", func(p *services.WikiPage) { p.Title = "Beta"; p.NormalizedTitle = "BETA" })
	r.ready()
	r.store.SelectPage("alpha")
	r.store.BeginEdit()
	r.store.SetDraft("see [[Beta]] and [[Nowhere]]")
	if !r.store.SaveDraft() {
		t.Fatalf("保存应成功：%+v", r.toasts())
	}
	v := r.view()
	if v.Editing || v.Draft != "" || v.Detail.Page.Body != "see [[Beta]] and [[Nowhere]]" || len(v.Revisions) != 1 {
		t.Fatalf("保存后退出编辑并重读：%+v", v)
	}
	r.wantToast(kit.ToastSuccess, locales.WikiPageSaved(), "")
	ups := r.svc.Updates()
	if len(ups) != 1 || ups[0].PageID != "alpha" || ups[0].ReleaseToAuto || !slices.Equal(ups[0].LinkTargets, []string{"BETA"}) {
		t.Fatalf("写入入参（出链只含能解析到的页面）：%+v", ups)
	}
	if got := r.svc.OutLinks("alpha"); !slices.Equal(got, []string{"beta"}) {
		t.Fatalf("出链已按新正文重建：%v", got)
	}
	if v.Detail.Page.ManualEditedAt == nil {
		t.Fatal("手动保存落手动标记")
	}
}

func TestSaveDraftFailureKeepsDraftAndShowsDetail(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	r.store.BeginEdit()
	r.store.SetDraft("precious words")
	r.svc.Fail(servicestest.WikiOpUpdatePage, errors.New("disk full"))
	if r.store.SaveDraft() {
		t.Fatal("应失败")
	}
	v := r.view()
	if !v.Editing || v.Draft != "precious words" {
		t.Fatalf("失败不丢草稿：%+v", v)
	}
	r.wantToast(kit.ToastError, locales.WikiPageSaveFailed(), "disk full")
	if got := r.log.entries(); !slices.Contains(got, "wiki.savePage|disk full") {
		t.Fatalf("日志：%v", got)
	}
	r.svc.Fail(servicestest.WikiOpUpdatePage, nil)
	if !r.store.SaveDraft() {
		t.Fatal("可重试")
	}
}

func TestSaveDraftTargetGone(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	r.store.BeginEdit()
	if _, err := r.svc.DeletePage(t.Context(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if r.store.SaveDraft() {
		t.Fatal("页面已被删，保存应失败而不是报成功")
	}
	r.wantToast(kit.ToastError, locales.WikiPageSaveFailed(), "")
	if !r.view().Editing {
		t.Fatal("草稿保留")
	}
}

func TestSaveDraftNotEditingOrNoSelection(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	if r.store.SaveDraft() {
		t.Fatal("不在编辑中")
	}
	r.wantNoToast()
	if r.store.SavePageBody("x", false) {
		t.Fatal("没有选中页")
	}
	r.wantToast(kit.ToastError, locales.WikiPageSaveFailed(), "")
	if len(r.svc.Updates()) != 0 {
		t.Fatal("没发请求")
	}
}

func TestSaveDraftKeepsEditsTypedDuringSave(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	r.store.BeginEdit()
	r.store.SetDraft("first version")
	release, arrived := r.svc.Hold(servicestest.WikiOpUpdatePage)
	var ok bool
	wait := inBackground(func() { ok = r.store.SaveDraft() })
	<-arrived
	r.store.SetDraft("first version plus more") // 保存期间继续输入
	release()
	wait()
	if !ok {
		t.Fatal("保存应成功")
	}
	v := r.view()
	if !v.Editing || v.Draft != "first version plus more" {
		t.Fatalf("新输入必须留在编辑框：%+v", v)
	}
	if ups := r.svc.Updates(); len(ups) != 1 || ups[0].Body != "first version" {
		t.Fatalf("保存的是点击那一刻的内容：%+v", ups)
	}
}

func TestSaveDraftAfterSwitchingPagesDoesNotPullViewBack(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	r.store.BeginEdit()
	r.store.SetDraft("alpha new body")
	release, arrived := r.svc.Hold(servicestest.WikiOpUpdatePage)
	wait := inBackground(func() { r.store.SaveDraft() })
	<-arrived
	r.store.SelectPage("gamma")
	release()
	wait()
	v := r.view()
	if v.SelectedPageID != "gamma" || v.Detail == nil || v.Detail.Page.ID != "gamma" || v.Editing {
		t.Fatalf("保存期间切走：视图留在 Gamma：%+v", v)
	}
	if p, _ := r.svc.Page("alpha"); p.Body != "alpha new body" {
		t.Fatal("Alpha 仍然保存成功")
	}
}

func TestReleaseToAuto(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("beta")
	if r.view().Detail.Page.ManualEditedAt == nil {
		t.Fatal("前置：Beta 是手动页")
	}
	if !r.store.ReleaseToAuto() {
		t.Fatalf("%+v", r.toasts())
	}
	r.wantToast(kit.ToastSuccess, locales.WikiReleasedToAuto(), "")
	v := r.view()
	if v.Detail.Page.ManualEditedAt != nil || v.Detail.Page.Body != "body of beta" {
		t.Fatalf("正文原样、手动标记清除：%+v", v.Detail.Page)
	}
	ups := r.svc.Updates()
	if len(ups) != 1 || !ups[0].ReleaseToAuto || ups[0].Body != "body of beta" {
		t.Fatalf("%+v", ups)
	}
	if got := v.Counts(); got.Manual != 0 {
		t.Fatalf("目录里的手动计数同步减少：%+v", got)
	}
}

func TestReleaseToAutoWithoutDetail(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	if r.store.ReleaseToAuto() {
		t.Fatal("没有详情")
	}
	r.wantToast(kit.ToastError, locales.WikiPageSaveFailed(), "")
}

func TestDeletePage(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("gamma")
	if !r.store.DeletePage("alpha") { // 删的不是当前页：选中不变
		t.Fatal("应成功")
	}
	v := r.view()
	if v.SelectedPageID != "gamma" || v.Detail == nil {
		t.Fatalf("删别的页不影响当前选中：%+v", v)
	}
	wantIDs(t, v.Catalog, "gamma", "beta")
	if r.sview().PageID != "gamma" {
		t.Fatal("建议状态仍跟 Gamma")
	}
	if v.BacklinkCounts["beta"] != 0 {
		t.Fatalf("Alpha 的出链随页面一起消失：%v", v.BacklinkCounts)
	}

	if !r.store.DeletePage("gamma") {
		t.Fatal("应成功")
	}
	v = r.view()
	if v.SelectedPageID != "" || v.Detail != nil {
		t.Fatalf("删当前页取消选中：%+v", v)
	}
	if r.sview().PageID != "" {
		t.Fatal("建议状态清空")
	}
	r.wantToast(kit.ToastSuccess, locales.WikiPageDeleted(), "")
}

func TestDeletePageFailureAndMissing(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	r.svc.Fail(servicestest.WikiOpDeletePage, errors.New("locked"))
	if r.store.DeletePage("alpha") {
		t.Fatal("应失败")
	}
	r.wantToast(kit.ToastError, locales.WikiPageDeleteFailed(), "locked")
	if v := r.view(); v.SelectedPageID != "alpha" || len(v.Catalog) != 3 {
		t.Fatalf("失败不改任何状态：%+v", v)
	}
	r.svc.Fail(servicestest.WikiOpDeletePage, nil)
	if r.store.DeletePage("ghost") {
		t.Fatal("不存在的页面不能报成功")
	}
	r.wantToast(kit.ToastError, locales.WikiPageDeleteFailed(), "")
}

func TestRestorePreviousRevision(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("alpha")
	if r.store.RestorePreviousRevision() {
		t.Fatal("没有历史版本")
	}
	r.wantToast(kit.ToastError, locales.WikiRevisionRestoreFailed(), "")

	r.store.BeginEdit()
	r.store.SetDraft("v2")
	if !r.store.SaveDraft() {
		t.Fatal("保存")
	}
	r.store.BeginEdit()
	r.store.SetDraft("half typed")
	if !r.store.RestorePreviousRevision() {
		t.Fatalf("%+v", r.toasts())
	}
	v := r.view()
	if v.Detail.Page.Body != "body of alpha" || v.Editing || v.Draft != "" {
		t.Fatalf("回滚后正文恢复且草稿（基于旧正文）被丢弃：%+v", v)
	}
	if len(v.Revisions) != 2 {
		t.Fatalf("回滚本身也存一份快照：%d", len(v.Revisions))
	}
	r.wantToast(kit.ToastSuccess, locales.WikiRevisionRestored(), "")
}

func TestRestoreFailureAndNoSelection(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	if r.store.RestorePreviousRevision() {
		t.Fatal("没有选中页")
	}
	r.store.SelectPage("alpha")
	r.svc.PutRevision(services.WikiRevision{PageID: "alpha", Body: "older"})
	r.store.SelectPage("alpha")
	r.svc.Fail(servicestest.WikiOpRestore, errors.New("restore broke"))
	if r.store.RestorePreviousRevision() {
		t.Fatal("应失败")
	}
	r.wantToast(kit.ToastError, locales.WikiRevisionRestoreFailed(), "restore broke")
	if p, _ := r.svc.Page("alpha"); p.Body != "body of alpha" {
		t.Fatal("失败不改页面")
	}
}

func TestAttemptRecoversFromPanic(t *testing.T) {
	r := newRig(t)
	if r.store.attempt("title", "wiki.test", func() (bool, error) { panic("kaboom") }) {
		t.Fatal("panic 视为失败")
	}
	r.wantToast(kit.ToastError, "title", "panic: kaboom")
}

// ---- 并发（配合 -race）----

func TestConcurrentSelectsAndReadsSettleOnLastSelection(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.useGoroutines()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		id := []string{"alpha", "beta", "gamma"}[i%3]
		go func() {
			defer wg.Done()
			r.store.SelectPage(id)
			_ = r.view()
			r.store.Refresh()
		}()
	}
	wg.Wait()
	r.store.SelectPage("beta") // 最后一次选择在全部并发结束后，状态必须落在它上面
	v := r.view()
	if v.SelectedPageID != "beta" || v.Detail == nil || v.Detail.Page.ID != "beta" || v.PageLoading {
		t.Fatalf("%+v", v)
	}
}

func TestConcurrentSearchAndFilterAndEdit(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.useGoroutines()
	r.store.SelectPage("alpha")
	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}
	for i := 0; i < 20; i++ {
		run(func() { r.store.RunSearch("alpha") })
		run(func() { r.store.RunSearch("") })
		run(func() { r.store.SetFilter(FilterAll); _ = r.view().Visible() })
		run(func() { r.store.SetSort(SortTitle) })
		run(func() { r.store.BeginEdit(); r.store.SetDraft("d"); r.store.CancelEdit() })
		run(func() { r.store.SetViewMode(ViewGraph) })
		run(func() { r.store.SetViewMode(ViewCatalog) })
	}
	wg.Wait()
	// SetViewMode 的图谱加载由 Spawn 放到后台，等它们收尾；之后不能残留加载态。
	eventually(t, func() bool { v := r.view(); return !v.Searching && !v.GraphLoading })
}

// eventually 轮询等待条件成立（后台任务收尾用），超时判失败。
func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("等待条件成立超时")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
