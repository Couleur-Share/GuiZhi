package servicestest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

func wikiSeed(t *testing.T) *WikiFake {
	t.Helper()
	f := &WikiFake{}
	f.PutPage(services.WikiPage{ID: "p1", Title: "Alpha", Summary: "first", Body: "alpha body about gamma"})
	f.PutPage(services.WikiPage{ID: "p2", Title: "Beta", Summary: "mentions alpha", Body: "beta body"})
	f.PutPage(services.WikiPage{ID: "p3", Title: "Gamma", Kind: services.WikiKindEntity, Body: "gamma body"})
	return f
}

func TestWikiFakeCatalogOrderAndStatus(t *testing.T) {
	ctx := t.Context()
	f := wikiSeed(t)
	f.SetStatusCounts(4, 9)
	cat, err := f.Catalog(ctx)
	if err != nil || len(cat) != 3 || cat[0].ID != "p3" || cat[2].ID != "p1" {
		t.Fatalf("目录应按最近更新倒序：%+v %v", cat, err)
	}
	if cat[2].NormalizedTitle != "ALPHA" || cat[0].Kind != services.WikiKindEntity {
		t.Fatalf("规范化标题与类型默认值：%+v", cat)
	}
	st, err := f.Status(ctx)
	if err != nil || st != (services.WikiStatus{PageCount: 3, CompiledItemCount: 4, EligibleItemCount: 9}) {
		t.Fatalf("状态：%+v %v", st, err)
	}
}

func TestWikiFakeSearchRanksTitleSummaryBody(t *testing.T) {
	ctx := t.Context()
	f := wikiSeed(t)
	hits, err := f.Search(ctx, " ALPHA ", 10)
	if err != nil || len(hits) != 2 || hits[0].ID != "p1" || hits[1].ID != "p2" {
		t.Fatalf("标题命中应排在摘要命中前：%+v %v", hits, err)
	}
	if hits, _ = f.Search(ctx, "gamma", 1); len(hits) != 1 || hits[0].ID != "p3" {
		t.Fatalf("limit 与标题优先：%+v", hits)
	}
	if hits, err = f.Search(ctx, "   ", 10); err != nil || hits == nil || len(hits) != 0 {
		t.Fatalf("空白查询应返回空切片：%+v %v", hits, err)
	}
	if got := f.Searches(); len(got) != 3 || got[0].Query != " ALPHA " || got[1].Limit != 1 {
		t.Fatalf("搜索记录：%+v", got)
	}
}

func TestWikiFakeUpdateSnapshotsAndRebuildsLinks(t *testing.T) {
	ctx := t.Context()
	f := wikiSeed(t)
	ok, err := f.UpdatePage(ctx, services.WikiUpdatePageInput{PageID: "p1", Body: "new [[Beta]] [[Nope]] [[Alpha]]", LinkTargets: []string{"BETA", "NOPE", "ALPHA"}})
	if err != nil || !ok {
		t.Fatalf("保存：%v %v", ok, err)
	}
	p, _ := f.Page("p1")
	if p.Body != "new [[Beta]] [[Nope]] [[Alpha]]" || p.ManualEditedAt == nil {
		t.Fatalf("保存后应写入正文并打手动编辑标记：%+v", p)
	}
	if got := f.OutLinks("p1"); !slices.Equal(got, []string{"p2"}) {
		t.Fatalf("出链应只含存在的、非自身的目标：%v", got)
	}
	revs := f.Revisions("p1")
	if len(revs) != 1 || revs[0].Body != "alpha body about gamma" {
		t.Fatalf("保存前应存快照：%+v", revs)
	}
	counts, _ := f.BacklinkCounts(ctx)
	if counts["p2"] != 1 || counts["p1"] != 0 {
		t.Fatalf("反向链接计数：%v", counts)
	}
	detail, _ := f.GetPage(ctx, "p2")
	if len(detail.Backlinks) != 1 || detail.Backlinks[0].ID != "p1" {
		t.Fatalf("反向链接：%+v", detail.Backlinks)
	}
	if _, err = f.UpdatePage(ctx, services.WikiUpdatePageInput{PageID: "p1", Body: "auto", ReleaseToAuto: true}); err != nil {
		t.Fatal(err)
	}
	if p, _ = f.Page("p1"); p.ManualEditedAt != nil || len(f.OutLinks("p1")) != 0 {
		t.Fatalf("交还自动后应清标记并重建出链：%+v %v", p, f.OutLinks("p1"))
	}
	if ok, err = f.UpdatePage(ctx, services.WikiUpdatePageInput{PageID: "missing"}); ok || err != nil {
		t.Fatalf("不存在的页面应返回 false：%v %v", ok, err)
	}
}

func TestWikiFakeRevisionsCappedAtTen(t *testing.T) {
	ctx := t.Context()
	f := wikiSeed(t)
	for i := range 13 {
		if _, err := f.UpdatePage(ctx, services.WikiUpdatePageInput{PageID: "p1", Body: fmt.Sprintf("v%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	revs, _ := f.ListRevisions(ctx, "p1")
	if len(revs) != 10 || revs[0].Body != "v11" {
		t.Fatalf("历史版本最多 10 份、新的在前：%d %+v", len(revs), revs[0])
	}
	ids := map[string]bool{}
	for _, r := range revs {
		ids[r.ID] = true
	}
	if len(ids) != 10 {
		t.Fatalf("版本 ID 应唯一：%v", ids)
	}
}

func TestWikiFakeDeleteCleansLinks(t *testing.T) {
	ctx := t.Context()
	f := wikiSeed(t)
	f.PutLink("p1", "p2")
	f.PutLink("p2", "p3")
	f.PutLink("p3", "p1")
	if ok, err := f.DeletePage(ctx, "p2"); !ok || err != nil {
		t.Fatalf("删除：%v %v", ok, err)
	}
	if len(f.OutLinks("p1")) != 0 || len(f.OutLinks("p2")) != 0 || !slices.Equal(f.OutLinks("p3"), []string{"p1"}) {
		t.Fatalf("删除应清掉两端的链接：%v %v %v", f.OutLinks("p1"), f.OutLinks("p2"), f.OutLinks("p3"))
	}
	if ok, _ := f.DeletePage(ctx, "p2"); ok {
		t.Fatal("重复删除应返回 false")
	}
	if got := f.Deletes(); len(got) != 2 {
		t.Fatalf("删除记录：%v", got)
	}
}

func TestWikiFakeRestoreRevision(t *testing.T) {
	ctx := t.Context()
	f := wikiSeed(t)
	if _, err := f.UpdatePage(ctx, services.WikiUpdatePageInput{PageID: "p1", Body: "edited"}); err != nil {
		t.Fatal(err)
	}
	revs := f.Revisions("p1")
	ok, err := f.RestoreRevision(ctx, revs[0].ID)
	if err != nil || !ok {
		t.Fatalf("回滚：%v %v", ok, err)
	}
	p, _ := f.Page("p1")
	if p.Body != "alpha body about gamma" || p.ManualEditedAt == nil || len(f.Revisions("p1")) != 2 {
		t.Fatalf("回滚应覆盖正文、标手动编辑并为当前内容存快照：%+v %d", p, len(f.Revisions("p1")))
	}
	if ok, _ = f.RestoreRevision(ctx, "nope"); ok {
		t.Fatal("未知版本应返回 false")
	}
}

func TestWikiFakeGraphCapAndVisibleLinks(t *testing.T) {
	ctx := t.Context()
	f := &WikiFake{}
	for i := range 305 {
		f.PutPage(services.WikiPage{ID: fmt.Sprintf("n%03d", i), Title: fmt.Sprintf("N%d", i)})
	}
	f.PutLink("n000", "n001")
	f.PutLink("n000", "n304") // 304 号最新，同度时靠前，也会进入前 300
	f.PutLink("n299", "n003")
	g, err := f.Graph(ctx)
	if err != nil || len(g.Nodes) != 300 || g.TotalNodes != 305 {
		t.Fatalf("图谱截断：%d/%d %v", len(g.Nodes), g.TotalNodes, err)
	}
	if len(g.Links) != 3 {
		t.Fatalf("有连接的节点优先进入图谱，三条边都可见：%+v", g.Links)
	}
}

func TestWikiFakeFailAndHoldAreOneShotHoldAndRecorded(t *testing.T) {
	ctx := t.Context()
	f := wikiSeed(t)
	boom := errors.New("boom")
	f.Fail(WikiOpGetPage, boom)
	if _, err := f.GetPage(ctx, "p1"); !errors.Is(err, boom) {
		t.Fatalf("注入失败：%v", err)
	}
	f.Fail(WikiOpGetPage, nil)

	release, arrived := f.Hold(WikiOpSearch)
	done := make(chan []services.WikiSearchHit, 1)
	go func() {
		hits, _ := f.Search(ctx, "alpha", 5)
		done <- hits
	}()
	<-arrived
	select {
	case <-done:
		t.Fatal("被 Hold 的调用不应提前返回")
	case <-time.After(30 * time.Millisecond):
	}
	// Hold 期间其他操作与再次搜索不受影响（只卡一次）。
	if _, err := f.GetPage(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := f.Search(ctx, "gamma", 5); len(hits) == 0 {
		t.Fatal("第二次搜索不应被卡住")
	}
	release()
	if hits := <-done; len(hits) != 2 {
		t.Fatalf("释放后应得到结果：%+v", hits)
	}
}

func TestWikiFakeStartCompileRules(t *testing.T) {
	ctx := t.Context()
	f := &WikiFake{}
	cands := []services.WikiCompileCandidate{{ID: "i1", Blocks: 3, Reusable: 1, State: services.WikiCandidateReady}, {ID: "i2", Blocks: 2}}
	var se *services.WikiStartError
	if _, err := f.StartCompile(ctx, nil); !errors.As(err, &se) {
		t.Fatalf("没选条目应是 WikiStartError：%v", err)
	}
	job, err := f.StartCompile(ctx, cands)
	if err != nil || job.Status != services.WikiJobRunning || job.Total != 5 || job.Completed != 1 || job.ID == "" {
		t.Fatalf("开始：%+v %v", job, err)
	}
	if _, err = f.StartCompile(ctx, cands); !errors.As(err, &se) {
		t.Fatalf("已有任务在跑应被拒：%v", err)
	}
	f.RejectStart(&services.WikiStartError{Message: "stale", Preview: &services.WikiCompilePreview{Counts: services.WikiCompileCounts{Ready: 7}}})
	_, err = f.StartCompile(ctx, cands)
	if !errors.As(err, &se) || se.Preview == nil || se.Preview.Counts.Ready != 7 {
		t.Fatalf("RejectStart 应带预览：%v", err)
	}
	f.RejectStart(nil)
	f.SetCompileConfigured(false)
	if _, err = f.StartCompile(ctx, cands); !errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("未配置模型：%v", err)
	}
	if ok, _ := f.CompileConfigured(ctx); ok {
		t.Fatal("应报告未配置")
	}
	if len(f.Starts()) != 5 {
		t.Fatalf("开始记录：%d", len(f.Starts()))
	}
}

func TestWikiFakeRunCompileScript(t *testing.T) {
	ctx := t.Context()
	f := &WikiFake{}
	job, _ := f.StartCompile(ctx, []services.WikiCompileCandidate{{ID: "i1", Blocks: 4}})
	f.QueueRun(WikiRunScript{Progress: []services.WikiCompileJob{
		{Status: services.WikiJobRunning, Total: 4, Completed: 1},
		{Status: services.WikiJobRunning, Total: 4, Completed: 3},
	}})
	var seen []int
	out, err := f.RunCompile(ctx, job, func(j services.WikiCompileJob) { seen = append(seen, j.Completed) })
	if err != nil || out.Status != services.WikiJobCompleted || out.Completed != out.Total || !slices.Equal(seen, []int{1, 3}) {
		t.Fatalf("运行：%+v %v %v", out, err, seen)
	}
	if got, _ := f.Job(job.ID); got.Status != services.WikiJobCompleted {
		t.Fatalf("任务表应同步：%+v", got)
	}
}

func TestWikiFakeRunCompileFailureAndNotConfigured(t *testing.T) {
	ctx := t.Context()
	f := &WikiFake{}
	job, _ := f.StartCompile(ctx, []services.WikiCompileCandidate{{ID: "i1", Blocks: 1}})
	boom := errors.New("model down")
	f.QueueRun(WikiRunScript{Err: boom})
	out, err := f.RunCompile(ctx, job, nil)
	if !errors.Is(err, boom) || out.Status != services.WikiJobFailed {
		t.Fatalf("失败：%+v %v", out, err)
	}
	f.SetCompileConfigured(false)
	if _, err = f.RunCompile(ctx, job, nil); !errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("未配置：%v", err)
	}
}

func TestWikiFakeControlCancelAbortsActiveRun(t *testing.T) {
	ctx := t.Context()
	f := &WikiFake{}
	job, _ := f.StartCompile(ctx, []services.WikiCompileCandidate{{ID: "i1", Blocks: 4}})
	arrived := make(chan struct{})
	f.QueueRun(WikiRunScript{Gate: make(chan struct{}), Arrived: arrived})
	type result struct {
		job services.WikiCompileJob
		err error
	}
	done := make(chan result, 1)
	go func() {
		j, err := f.RunCompile(ctx, job, nil)
		done <- result{j, err}
	}()
	<-arrived
	if _, err := f.RunCompile(ctx, job, nil); err == nil {
		t.Fatal("同时只允许一次运行")
	}
	if got, err := f.ControlCompile(ctx, job.ID, services.WikiJobCancelled); err != nil || got.Status != services.WikiJobCancelled {
		t.Fatalf("取消：%+v %v", got, err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.job.Status != services.WikiJobCancelled {
			t.Fatalf("取消应让运行以 cancelled、无错误收尾：%+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消没有中止运行")
	}
	if _, err := f.ControlCompile(ctx, "missing", services.WikiJobPaused); err == nil {
		t.Fatal("未知任务应报错")
	}
	if _, err := f.ControlCompile(ctx, job.ID, "bogus"); err == nil {
		t.Fatal("非法状态应报错")
	}
}

func TestWikiFakeRunHonorsContextCancel(t *testing.T) {
	f := &WikiFake{}
	job, _ := f.StartCompile(context.Background(), []services.WikiCompileCandidate{{ID: "i1", Blocks: 4}})
	arrived := make(chan struct{})
	f.QueueRun(WikiRunScript{Gate: make(chan struct{}), Arrived: arrived})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan services.WikiCompileJob, 1)
	go func() {
		j, _ := f.RunCompile(ctx, job, nil)
		done <- j
	}()
	<-arrived
	cancel()
	select {
	case j := <-done:
		if j.Status != services.WikiJobCancelled {
			t.Fatalf("ctx 取消应落为 cancelled：%+v", j)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消没有中止运行")
	}
}

func TestWikiFakeControlResumeRejectsSecondRunning(t *testing.T) {
	ctx := t.Context()
	f := &WikiFake{}
	paused := f.PutJob(services.WikiCompileJob{Status: services.WikiJobPaused})
	f.PutJob(services.WikiCompileJob{Status: services.WikiJobRunning})
	if _, err := f.ControlCompile(ctx, paused.ID, services.WikiJobRunning); err == nil {
		t.Fatal("已有任务在跑时不能再恢复一个")
	}
	done := f.PutJob(services.WikiCompileJob{Status: services.WikiJobCompleted})
	if _, err := f.ControlCompile(ctx, done.ID, services.WikiJobCancelled); err == nil {
		t.Fatal("已完成的任务不可控制")
	}
	jobs, _ := f.CompileJobs(ctx)
	if len(jobs) != 3 {
		t.Fatalf("任务列表：%+v", jobs)
	}
}

func TestWikiFakeSuggestionAcceptAndStaleToken(t *testing.T) {
	ctx := t.Context()
	f := wikiSeed(t)
	f.PutSuggestion("p1", &services.WikiSuggestion{Token: "t1", Draft: services.WikiSuggestionDraft{Title: "Alpha 2", Summary: "s2", Body: "b2", LinkTargets: []string{"BETA"}}, SourceIDs: []string{"i1"}})
	got, err := f.Suggestion(ctx, "p1")
	if err != nil || got == nil || got.Token != "t1" {
		t.Fatalf("读取建议：%+v %v", got, err)
	}
	if none, _ := f.Suggestion(ctx, "p2"); none != nil {
		t.Fatalf("没有建议应是 nil：%+v", none)
	}
	if ok, _ := f.AcceptSuggestion(ctx, "p1", "old"); ok {
		t.Fatal("过期令牌应返回 false")
	}
	if p, _ := f.Page("p1"); p.Body != "alpha body about gamma" {
		t.Fatalf("过期令牌不应改页面：%+v", p)
	}
	ok, err := f.AcceptSuggestion(ctx, "p1", "t1")
	if err != nil || !ok {
		t.Fatalf("采用：%v %v", ok, err)
	}
	p, _ := f.Page("p1")
	if p.Title != "Alpha 2" || p.Body != "b2" || p.ManualEditedAt == nil || len(f.Revisions("p1")) != 1 {
		t.Fatalf("采用后应覆盖内容、保持手动维护标记并存快照：%+v", p)
	}
	if !slices.Equal(f.OutLinks("p1"), []string{"p2"}) {
		t.Fatalf("采用后应按建议重建出链：%v", f.OutLinks("p1"))
	}
	if again, _ := f.Suggestion(ctx, "p1"); again != nil {
		t.Fatal("采用后建议应被清掉")
	}
	if acc := f.Accepts(); len(acc) != 2 || acc[1].Token != "t1" {
		t.Fatalf("采用记录：%+v", acc)
	}
}

func TestWikiFakeConcurrentUse(t *testing.T) {
	ctx := t.Context()
	f := wikiSeed(t)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 20 {
				switch (i + j) % 6 {
				case 0:
					_, _ = f.UpdatePage(ctx, services.WikiUpdatePageInput{PageID: "p1", Body: "x", LinkTargets: []string{"BETA"}})
				case 1:
					_, _ = f.Search(ctx, "alpha", 5)
				case 2:
					_, _ = f.Catalog(ctx)
				case 3:
					_, _ = f.Graph(ctx)
				case 4:
					_, _ = f.GetPage(ctx, "p2")
				default:
					_, _ = f.BacklinkCounts(ctx)
				}
			}
		}()
	}
	wg.Wait()
	if len(f.Updates()) == 0 || f.CatalogCalls() == 0 || f.GraphCalls() == 0 {
		t.Fatal("并发调用都应被记录")
	}
}
