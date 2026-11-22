package main

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

// Wiki 适配器的集成测试：真实 SQLite（临时数据目录）、真实 wiki:* 与 wiki:compiler 处理器、本地假 AI 服务；
// 不碰真实的 %APPDATA%\GuiZhi。

func newWikiAdapterForTest(t *testing.T) (*wikiAdapter, *Bridge, *Library) {
	t.Helper()
	_, b, l := newLibraryAIAdapterForTest(t)
	l.registerWiki(b)
	l.registerWikiCompiler(b)
	return newWikiAdapter(b), b, l
}

func wikiPagesReply(pages ...map[string]any) *askLLMReply {
	raw, _ := json.Marshal(map[string]any{"pages": pages})
	return &askLLMReply{Chunks: []string{string(raw)}}
}

// wikiTestScript 按来源条目回放页面：Source Alpha → "Alpha Topic"，Source Beta → "Beta Topic"（正文互相引用）。
func wikiTestScript(_ int, body string) *askLLMReply {
	switch {
	case strings.Contains(body, "Source Alpha"):
		return wikiPagesReply(map[string]any{"title": "Alpha Topic", "kind": "topic", "summary": "Alpha summary", "body": "Alpha overview. See [[Beta Topic]] for details.", "aliases": []string{"Alpha Alias"}})
	case strings.Contains(body, "Source Beta"):
		return wikiPagesReply(map[string]any{"title": "Beta Topic", "kind": "concept", "summary": "Beta summary", "body": "Beta overview. Related: [[Alpha Topic]].", "aliases": []string{}})
	}
	return wikiPagesReply()
}

type wikiFixture struct {
	a         *wikiAdapter
	b         *Bridge
	l         *Library
	llm       *askFakeLLM
	alpha     string
	beta      string
	completed services.WikiCompileJob
}

const wikiItemFiller = " This is long enough source material for the wiki compiler to treat it as a valid, compilable knowledge item."

// newWikiFixture 配好假模型、建两个条目，但还没有编译。
func newWikiFixture(t *testing.T, script func(int, string) *askLLMReply) *wikiFixture {
	t.Helper()
	a, b, l := newWikiAdapterForTest(t)
	llm := newAskFakeLLM(t, script)
	configureAITestModels(t, b, llm.srv.URL, "openai", "gpt-4o-mini")
	return &wikiFixture{
		a: a, b: b, l: l, llm: llm,
		alpha: createOpsItem(t, b, map[string]any{"title": "Source Alpha", "content": "Alpha content." + wikiItemFiller}),
		beta:  createOpsItem(t, b, map[string]any{"title": "Source Beta", "content": "Beta content." + wikiItemFiller}),
	}
}

// compile 预览全部可执行条目、开始并跑完一个编译任务。
func (f *wikiFixture) compile(t *testing.T) services.WikiCompileJob {
	t.Helper()
	ctx := t.Context()
	preview, err := f.a.CompilePreview(ctx)
	if err != nil {
		t.Fatalf("预览：%v", err)
	}
	var selected []services.WikiCompileCandidate
	for _, e := range preview.Entries {
		if e.Selectable() {
			selected = append(selected, e)
		}
	}
	if len(selected) == 0 {
		t.Fatalf("没有可编译的条目：%+v", preview)
	}
	job, err := f.a.StartCompile(ctx, selected)
	if err != nil {
		t.Fatalf("开始：%v", err)
	}
	done, err := f.a.RunCompile(ctx, job, nil)
	if err != nil || done.Status != services.WikiJobCompleted {
		t.Fatalf("运行：%+v %v", done, err)
	}
	return done
}

func compiledWikiFixture(t *testing.T) *wikiFixture {
	t.Helper()
	f := newWikiFixture(t, wikiTestScript)
	f.completed = f.compile(t)
	return f
}

func (f *wikiFixture) pageIDByTitle(t *testing.T, title string) string {
	t.Helper()
	cat, err := f.a.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range cat {
		if e.Title == title {
			return e.ID
		}
	}
	t.Fatalf("目录里没有 %q：%+v", title, cat)
	return ""
}

func TestNativeWikiEmptyAndUnconfigured(t *testing.T) {
	ctx := t.Context()
	a, _, _ := newWikiAdapterForTest(t)

	cat, err := a.Catalog(ctx)
	if err != nil || cat == nil || len(cat) != 0 {
		t.Fatalf("空库目录应是空切片：%+v %v", cat, err)
	}
	if st, err := a.Status(ctx); err != nil || st != (services.WikiStatus{}) {
		t.Fatalf("空库状态：%+v %v", st, err)
	}
	if hits, err := a.Search(ctx, "anything", 10); err != nil || len(hits) != 0 {
		t.Fatalf("空库搜索：%+v %v", hits, err)
	}
	if hits, err := a.Search(ctx, "   ", 10); err != nil || hits == nil || len(hits) != 0 {
		t.Fatalf("空白查询应返回空切片：%+v %v", hits, err)
	}
	if detail, err := a.GetPage(ctx, "nope"); err != nil || detail != nil {
		t.Fatalf("不存在的页面应是 nil：%+v %v", detail, err)
	}
	if ok, err := a.UpdatePage(ctx, services.WikiUpdatePageInput{PageID: "nope", Body: "x"}); err != nil || ok {
		t.Fatalf("不存在的页面保存应返回 false：%v %v", ok, err)
	}
	if ok, err := a.DeletePage(ctx, "nope"); err != nil || ok {
		t.Fatalf("不存在的页面删除应返回 false：%v %v", ok, err)
	}
	if ok, err := a.RestoreRevision(ctx, "nope"); err != nil || ok {
		t.Fatalf("不存在的版本回滚应返回 false：%v %v", ok, err)
	}
	if revs, err := a.ListRevisions(ctx, "nope"); err != nil || revs == nil || len(revs) != 0 {
		t.Fatalf("历史版本应是空切片：%+v %v", revs, err)
	}
	g, err := a.Graph(ctx)
	if err != nil || g.TotalNodes != 0 || g.Nodes == nil || g.Links == nil {
		t.Fatalf("空图谱：%+v %v", g, err)
	}
	counts, err := a.BacklinkCounts(ctx)
	if err != nil || counts == nil || len(counts) != 0 {
		t.Fatalf("空库反向链接计数：%+v %v", counts, err)
	}
	if s, err := a.Suggestion(ctx, "nope"); err != nil || s != nil {
		t.Fatalf("没有建议应是 nil：%+v %v", s, err)
	}
	if ok, err := a.AcceptSuggestion(ctx, "nope", "tok"); err != nil || ok {
		t.Fatalf("没有建议时采用应返回 false：%v %v", ok, err)
	}
	jobs, err := a.CompileJobs(ctx)
	if err != nil || jobs == nil || len(jobs) != 0 {
		t.Fatalf("没有任务：%+v %v", jobs, err)
	}

	if ok, err := a.CompileConfigured(ctx); err != nil || ok {
		t.Fatalf("没有模型时编译未配置：%v %v", ok, err)
	}
	if _, err := a.StartCompile(ctx, []services.WikiCompileCandidate{{ID: "x"}}); !errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("未配置开始编译应返回 ErrAINotConfigured：%v", err)
	}
	if _, err := a.RunCompile(ctx, services.WikiCompileJob{ID: "j", Model: "m"}, nil); !errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("未配置运行编译应返回 ErrAINotConfigured：%v", err)
	}
	if preview, err := a.CompilePreview(ctx); err != nil || preview.Entries == nil {
		t.Fatalf("未配置也能预览（没有可执行条目）：%+v %v", preview, err)
	}
}

func TestNativeWikiCompileEndToEnd(t *testing.T) {
	ctx := t.Context()
	f := newWikiFixture(t, wikiTestScript)
	a := f.a

	if ok, err := a.CompileConfigured(ctx); err != nil || !ok {
		t.Fatalf("配置模型后编译已配置：%v %v", ok, err)
	}
	preview, err := a.CompilePreview(ctx)
	if err != nil || preview.Counts.Ready != 2 || len(preview.Entries) != 2 {
		t.Fatalf("预览应有两个可执行条目：%+v %v", preview, err)
	}
	for _, e := range preview.Entries {
		if e.State != services.WikiCandidateReady || e.Fingerprint == "" || e.Blocks < 1 || !e.Selectable() {
			t.Fatalf("候选条目：%+v", e)
		}
	}
	if preview.Requests.Min < 2 {
		t.Fatalf("请求次数估算：%+v", preview.Requests)
	}

	job, err := a.StartCompile(ctx, preview.Entries)
	if err != nil || job.Status != services.WikiJobRunning || job.ID == "" || job.Total < 2 {
		t.Fatalf("开始：%+v %v", job, err)
	}
	var (
		mu       sync.Mutex
		progress []services.WikiCompileJob
	)
	done, err := a.RunCompile(ctx, job, func(j services.WikiCompileJob) {
		mu.Lock()
		progress = append(progress, j)
		mu.Unlock()
	})
	if err != nil || done.Status != services.WikiJobCompleted || done.Completed != done.Total || done.Failed != 0 {
		t.Fatalf("运行：%+v %v", done, err)
	}
	mu.Lock()
	if len(progress) == 0 || progress[len(progress)-1].Completed != done.Total {
		t.Fatalf("进度回调应覆盖到最后一块：%+v", progress)
	}
	mu.Unlock()
	if f.llm.requestCount() < 2 {
		t.Fatalf("每个分块都应调一次模型：%d", f.llm.requestCount())
	}

	jobs, err := a.CompileJobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID || jobs[0].Status != services.WikiJobCompleted {
		t.Fatalf("任务列表：%+v %v", jobs, err)
	}
	st, err := a.Status(ctx)
	if err != nil || st.PageCount != 2 || st.CompiledItemCount != 2 || st.EligibleItemCount != 2 {
		t.Fatalf("状态：%+v %v", st, err)
	}
	if after, err := a.CompilePreview(ctx); err != nil || after.Counts.Ready != 0 || after.Counts.Current != 2 {
		t.Fatalf("编译后两个条目都应是已最新：%+v %v", after, err)
	}
}

func TestNativeWikiReadsAgainstCompiledPages(t *testing.T) {
	ctx := t.Context()
	f := compiledWikiFixture(t)
	a := f.a
	alpha, beta := f.pageIDByTitle(t, "Alpha Topic"), f.pageIDByTitle(t, "Beta Topic")

	cat, _ := a.Catalog(ctx)
	byTitle := map[string]services.WikiCatalogEntry{}
	for _, e := range cat {
		byTitle[e.Title] = e
	}
	if byTitle["Alpha Topic"].Kind != services.WikiKindTopic || byTitle["Beta Topic"].Kind != services.WikiKindConcept {
		t.Fatalf("类型：%+v", byTitle)
	}
	if byTitle["Alpha Topic"].AliasesJSON == nil || !strings.Contains(*byTitle["Alpha Topic"].AliasesJSON, "Alpha Alias") || byTitle["Alpha Topic"].ManualEditedAt != nil {
		t.Fatalf("别名与手动编辑标记：%+v", byTitle["Alpha Topic"])
	}

	detail, err := a.GetPage(ctx, alpha)
	if err != nil || detail == nil || detail.Page.Title != "Alpha Topic" || detail.Page.Provider == "" || detail.Page.PromptVersion != services.WikiCompilerVersion {
		t.Fatalf("详情：%+v %v", detail, err)
	}
	if len(detail.Sources) != 1 || detail.Sources[0].ItemID != f.alpha || detail.Sources[0].Title != "Source Alpha" || detail.Sources[0].NeedsReview() {
		t.Fatalf("来源：%+v", detail.Sources)
	}
	if detail.Backlinks == nil || detail.Sources[0].ReviewReasons == nil {
		t.Fatalf("反向链接与复核原因应是空切片而不是 null：%+v", detail)
	}

	counts, err := a.BacklinkCounts(ctx)
	if err != nil || counts[alpha]+counts[beta] < 1 {
		t.Fatalf("至少一个方向的链接：%+v %v", counts, err)
	}
	g, err := a.Graph(ctx)
	if err != nil || len(g.Nodes) != 2 || g.TotalNodes != 2 || len(g.Links) < 1 {
		t.Fatalf("图谱：%+v %v", g, err)
	}
	for _, l := range g.Links {
		if !(l.Source == alpha && l.Target == beta || l.Source == beta && l.Target == alpha) {
			t.Fatalf("边的两端应是两页：%+v", l)
		}
	}

	hits, err := a.Search(ctx, "Beta", 10)
	if err != nil || len(hits) == 0 || hits[0].ID != beta || hits[0].Title != "Beta Topic" {
		t.Fatalf("标题命中应排在正文命中前：%+v %v", hits, err)
	}
	if hits, _ = a.Search(ctx, "overview", 1); len(hits) != 1 {
		t.Fatalf("limit：%+v", hits)
	}
}

func TestNativeWikiUpdateRestoreDelete(t *testing.T) {
	ctx := t.Context()
	f := compiledWikiFixture(t)
	a := f.a
	alpha, beta := f.pageIDByTitle(t, "Alpha Topic"), f.pageIDByTitle(t, "Beta Topic")
	original, _ := a.GetPage(ctx, alpha)

	ok, err := a.UpdatePage(ctx, services.WikiUpdatePageInput{PageID: alpha, Body: "Edited by hand. See [[Beta Topic]].", LinkTargets: []string{"BETA TOPIC"}})
	if err != nil || !ok {
		t.Fatalf("保存：%v %v", ok, err)
	}
	edited, _ := a.GetPage(ctx, alpha)
	if edited.Page.Body != "Edited by hand. See [[Beta Topic]]." || edited.Page.ManualEditedAt == nil || edited.Page.UpdatedAt < original.Page.UpdatedAt {
		t.Fatalf("保存后：%+v", edited.Page)
	}
	betaDetail, _ := a.GetPage(ctx, beta)
	if !slices.ContainsFunc(betaDetail.Backlinks, func(e services.WikiCatalogEntry) bool { return e.ID == alpha }) {
		t.Fatalf("Beta 的反向链接应含 Alpha：%+v", betaDetail.Backlinks)
	}
	if hits, _ := a.Search(ctx, "hand", 5); len(hits) != 1 || hits[0].ID != alpha {
		t.Fatalf("保存后全文索引应更新：%+v", hits)
	}

	revs, err := a.ListRevisions(ctx, alpha)
	if err != nil || len(revs) != 1 || revs[0].Body != original.Page.Body || revs[0].PageID != alpha {
		t.Fatalf("保存前应存一份历史版本：%+v %v", revs, err)
	}

	// 交还自动：清掉手动编辑标记。
	if ok, err = a.UpdatePage(ctx, services.WikiUpdatePageInput{PageID: alpha, Body: "Back to auto.", ReleaseToAuto: true}); err != nil || !ok {
		t.Fatalf("交还自动：%v %v", ok, err)
	}
	if released, _ := a.GetPage(ctx, alpha); released.Page.ManualEditedAt != nil {
		t.Fatalf("交还自动后应清除标记：%+v", released.Page)
	}

	// 回滚到最早的版本（编译产出的原文）。
	revs, _ = a.ListRevisions(ctx, alpha)
	if len(revs) != 2 || revs[0].CreatedAt < revs[1].CreatedAt {
		t.Fatalf("历史版本新的在前：%+v", revs)
	}
	oldest := revs[len(revs)-1]
	if ok, err = a.RestoreRevision(ctx, oldest.ID); err != nil || !ok {
		t.Fatalf("回滚：%v %v", ok, err)
	}
	restored, _ := a.GetPage(ctx, alpha)
	if restored.Page.Body != original.Page.Body || restored.Page.ManualEditedAt == nil {
		t.Fatalf("回滚后应是原文并标为手动编辑：%+v", restored.Page)
	}
	if revs, _ = a.ListRevisions(ctx, alpha); len(revs) != 3 {
		t.Fatalf("回滚本身也存一份快照：%d", len(revs))
	}

	if ok, err = a.DeletePage(ctx, beta); err != nil || !ok {
		t.Fatalf("删除：%v %v", ok, err)
	}
	if gone, err := a.GetPage(ctx, beta); err != nil || gone != nil {
		t.Fatalf("删除后应读不到：%+v %v", gone, err)
	}
	if st, _ := a.Status(ctx); st.PageCount != 1 {
		t.Fatalf("删除后页数：%+v", st)
	}
	if counts, _ := a.BacklinkCounts(ctx); counts[beta] != 0 {
		t.Fatalf("删除后不应再有指向它的计数：%+v", counts)
	}
	if hits, _ := a.Search(ctx, "Beta", 5); slices.ContainsFunc(hits, func(h services.WikiSearchHit) bool { return h.ID == beta }) {
		t.Fatalf("删除后搜索不应命中已删页面：%+v", hits)
	}
}

func TestNativeWikiStartRejectedWhenSourcesChanged(t *testing.T) {
	ctx := t.Context()
	f := newWikiFixture(t, wikiTestScript)
	preview, err := f.a.CompilePreview(ctx)
	if err != nil || len(preview.Entries) != 2 {
		t.Fatalf("预览：%+v %v", preview, err)
	}
	// 预览之后资料变了：指纹对不上，开始会被拒绝并带回刷新后的预览。
	if _, err := f.b.call(ctx, "knowledge:update", f.alpha, map[string]any{"content": "Changed after the preview." + wikiItemFiller}); err != nil {
		t.Fatal(err)
	}
	_, err = f.a.StartCompile(ctx, preview.Entries)
	var rejected *services.WikiStartError
	if !errors.As(err, &rejected) || rejected.Message == "" || rejected.Preview == nil {
		t.Fatalf("应返回带预览的 WikiStartError：%v", err)
	}
	fresh := slices.IndexFunc(rejected.Preview.Entries, func(e services.WikiCompileCandidate) bool { return e.ID == f.alpha })
	old := slices.IndexFunc(preview.Entries, func(e services.WikiCompileCandidate) bool { return e.ID == f.alpha })
	if fresh < 0 || rejected.Preview.Entries[fresh].Fingerprint == preview.Entries[old].Fingerprint {
		t.Fatalf("带回的预览应是新指纹：%+v", rejected.Preview.Entries)
	}
	if jobs, _ := f.a.CompileJobs(ctx); len(jobs) != 0 {
		t.Fatalf("被拒绝不应创建任务：%+v", jobs)
	}
	// 没选条目同样是 WikiStartError。
	if _, err = f.a.StartCompile(ctx, nil); !errors.As(err, &rejected) || rejected.Message == "" {
		t.Fatalf("没选条目：%v", err)
	}
	// 用刷新后的预览重试成功。
	if job, err := f.a.StartCompile(ctx, freshSelectable(f, t)); err != nil || job.ID == "" {
		t.Fatalf("按新预览重试：%+v %v", job, err)
	}
	// 已有任务在跑：再开始被拒。
	if _, err = f.a.StartCompile(ctx, freshSelectable(f, t)); !errors.As(err, &rejected) {
		t.Fatalf("已有任务在跑应被拒：%v", err)
	}
}

func freshSelectable(f *wikiFixture, t *testing.T) []services.WikiCompileCandidate {
	t.Helper()
	preview, err := f.a.CompilePreview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var out []services.WikiCompileCandidate
	for _, e := range preview.Entries {
		if e.Selectable() {
			out = append(out, e)
		}
	}
	return out
}

func TestNativeWikiCancelThenResume(t *testing.T) {
	ctx := t.Context()
	var hang sync.Mutex
	hanging := true
	f := newWikiFixture(t, func(n int, body string) *askLLMReply {
		hang.Lock()
		defer hang.Unlock()
		if hanging {
			return &askLLMReply{Hang: true}
		}
		return wikiTestScript(n, body)
	})
	job, err := f.a.StartCompile(ctx, freshSelectable(f, t))
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		job services.WikiCompileJob
		err error
	}
	done := make(chan result, 1)
	go func() {
		j, err := f.a.RunCompile(ctx, job, nil)
		done <- result{j, err}
	}()
	f.llm.waitArrived(t)

	cancelled, err := f.a.ControlCompile(ctx, job.ID, services.WikiJobCancelled)
	if err != nil || cancelled.Status != services.WikiJobCancelled {
		t.Fatalf("取消：%+v %v", cancelled, err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.job.Status != services.WikiJobCancelled {
			t.Fatalf("取消应中止进行中的请求并以 cancelled 收尾：%+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("取消没有中止运行")
	}
	jobs, _ := f.a.CompileJobs(ctx)
	if len(jobs) != 1 || jobs[0].Status != services.WikiJobCancelled || !jobs[0].Resumable() {
		t.Fatalf("取消后的任务应可继续：%+v", jobs)
	}

	// 继续：先改回 running，再 RunCompile 跑完剩下的分块。
	hang.Lock()
	hanging = false
	hang.Unlock()
	resumed, err := f.a.ControlCompile(ctx, job.ID, services.WikiJobRunning)
	if err != nil || resumed.Status != services.WikiJobRunning {
		t.Fatalf("恢复：%+v %v", resumed, err)
	}
	finished, err := f.a.RunCompile(ctx, resumed, nil)
	if err != nil || finished.Status != services.WikiJobCompleted {
		t.Fatalf("继续：%+v %v", finished, err)
	}
	if st, _ := f.a.Status(ctx); st.PageCount != 2 {
		t.Fatalf("继续后应产出两页：%+v", st)
	}
	if _, err = f.a.ControlCompile(ctx, job.ID, services.WikiJobPaused); err == nil {
		t.Fatal("已完成的任务不可再控制")
	}
	if _, err = f.a.ControlCompile(ctx, "missing", services.WikiJobPaused); err == nil {
		t.Fatal("不存在的任务应报错")
	}
}

func TestNativeWikiPauseNotStartedJobAndResume(t *testing.T) {
	ctx := t.Context()
	f := newWikiFixture(t, wikiTestScript)
	job, err := f.a.StartCompile(ctx, freshSelectable(f, t))
	if err != nil {
		t.Fatal(err)
	}
	paused, err := f.a.ControlCompile(ctx, job.ID, services.WikiJobPaused)
	if err != nil || paused.Status != services.WikiJobPaused || !paused.Resumable() {
		t.Fatalf("暂停：%+v %v", paused, err)
	}
	// 暂停后可以再开一个任务（没有任务在跑），但不能同时恢复两个。
	if _, err = f.a.ControlCompile(ctx, job.ID, services.WikiJobRunning); err != nil {
		t.Fatalf("恢复：%v", err)
	}
	if done, err := f.a.RunCompile(ctx, job, nil); err != nil || done.Status != services.WikiJobCompleted {
		t.Fatalf("运行：%+v %v", done, err)
	}
}

func TestNativeWikiRunDetectsModelChange(t *testing.T) {
	ctx := t.Context()
	f := newWikiFixture(t, wikiTestScript)
	job, err := f.a.StartCompile(ctx, freshSelectable(f, t))
	if err != nil {
		t.Fatal(err)
	}
	// 任务创建之后换了模型：实时配置应当发现，不能继续用旧任务的检查点。
	configureAITestModels(t, f.b, f.llm.srv.URL, "openai", "another-model")
	_, err = f.a.RunCompile(ctx, job, nil)
	if err == nil || errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("模型变化应返回带原因的错误：%v", err)
	}
	if f.llm.requestCount() != 0 {
		t.Fatalf("模型变化时不应调用模型：%d", f.llm.requestCount())
	}
}

func TestNativeWikiMapErrScrubsAPIKey(t *testing.T) {
	f := newWikiFixture(t, nil)
	err := f.a.mapErr(errors.New("request failed: bad key " + askTestSecret + " rejected"))
	if err == nil || strings.Contains(err.Error(), askTestSecret) || !strings.Contains(err.Error(), "***") {
		t.Fatalf("错误文本应抹掉 API Key：%v", err)
	}
	if got := f.a.mapErr(nil); got != nil {
		t.Fatalf("nil 保持 nil：%v", got)
	}
}

// 任务检查点里的失败原因来自服务商的原始响应，可能回显密钥：解码出来的任务与进度回调里的任务都要脱敏，
// 没有 Error 的任务原样返回。
func TestNativeWikiScrubJobErrorField(t *testing.T) {
	f := newWikiFixture(t, nil)
	job := services.WikiCompileJob{ID: "j1", Status: services.WikiJobFailed, Total: 3, Error: "401 bad key " + askTestSecret + " rejected"}
	got := f.a.scrubJob(job)
	if strings.Contains(got.Error, askTestSecret) || !strings.Contains(got.Error, "***") {
		t.Fatalf("任务 Error 应抹掉 API Key：%q", got.Error)
	}
	if got.ID != job.ID || got.Status != job.Status || got.Total != job.Total {
		t.Fatalf("其余字段不变：%+v", got)
	}
	if clean := (services.WikiCompileJob{ID: "j2"}); f.a.scrubJob(clean) != clean {
		t.Fatal("没有 Error 的任务应原样返回")
	}
}

// 端到端验证：服务商错误响应回显 API Key 时，RunCompile 返回的终态任务、进度回调以及 CompileJobs 读回的记录
// 都不能泄漏明文密钥。
func TestNativeWikiCompileJobsAndRunScrubAPIKey(t *testing.T) {
	ctx := t.Context()
	f := newWikiFixture(t, func(int, string) *askLLMReply {
		return &askLLMReply{Status: 401, Raw: "401 unauthorized key=" + askTestSecret}
	})
	preview, err := f.a.CompilePreview(ctx)
	if err != nil || len(preview.Entries) == 0 {
		t.Fatalf("预览：%v %+v", err, preview)
	}
	job, err := f.a.StartCompile(ctx, preview.Entries[:1])
	if err != nil {
		t.Fatalf("开始：%v", err)
	}
	var seenProgress []services.WikiCompileJob
	done, runErr := f.a.RunCompile(ctx, job, func(j services.WikiCompileJob) {
		seenProgress = append(seenProgress, j)
	})
	if runErr != nil && strings.Contains(runErr.Error(), askTestSecret) {
		t.Fatalf("RunCompile 错误文本泄漏 API Key：%v", runErr)
	}
	if strings.Contains(done.Error, askTestSecret) {
		t.Fatalf("RunCompile 终态 Error 泄漏 API Key：%q", done.Error)
	}
	for _, p := range seenProgress {
		if strings.Contains(p.Error, askTestSecret) {
			t.Fatalf("进度回调 Error 泄漏 API Key：%q", p.Error)
		}
	}
	jobs, err := f.a.CompileJobs(ctx)
	if err != nil || len(jobs) == 0 {
		t.Fatalf("CompileJobs：%v %+v", err, jobs)
	}
	for _, j := range jobs {
		if strings.Contains(j.Error, askTestSecret) {
			t.Fatalf("CompileJobs Error 泄漏 API Key：%q", j.Error)
		}
	}
}

// 手动改过的页面在来源更新后不会被直接覆盖：新内容存成更新建议，令牌失效后采用返回 false，令牌有效时采用并存快照。
func TestNativeWikiSuggestionFlow(t *testing.T) {
	ctx := t.Context()
	f := compiledWikiFixture(t)
	a := f.a
	alpha := f.pageIDByTitle(t, "Alpha Topic")

	if s, err := a.Suggestion(ctx, alpha); err != nil || s != nil {
		t.Fatalf("编译完还没有建议：%+v %v", s, err)
	}
	if ok, err := a.UpdatePage(ctx, services.WikiUpdatePageInput{PageID: alpha, Body: "Hand written body."}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	// 来源变了、重新编译：手动改过的页面只产生建议。
	if _, err := f.b.call(ctx, "knowledge:update", f.alpha, map[string]any{"content": "Alpha changed content." + wikiItemFiller}); err != nil {
		t.Fatal(err)
	}
	f.compile(t)

	sugg, err := a.Suggestion(ctx, alpha)
	if err != nil || sugg == nil || sugg.Token == "" || sugg.Reason == "" || sugg.Draft.Title != "Alpha Topic" || !strings.Contains(sugg.Draft.Body, "Alpha overview") {
		t.Fatalf("应生成更新建议：%+v %v", sugg, err)
	}
	if !slices.Contains(sugg.SourceIDs, f.alpha) {
		t.Fatalf("建议应记录来源：%+v", sugg.SourceIDs)
	}
	if page, _ := a.GetPage(ctx, alpha); page.Page.Body != "Hand written body." {
		t.Fatalf("建议不应改动手写页面：%+v", page.Page)
	}

	if ok, err := a.AcceptSuggestion(ctx, alpha, "stale-token"); err != nil || ok {
		t.Fatalf("过期令牌应返回 false 且不报错：%v %v", ok, err)
	}
	if ok, err := a.AcceptSuggestion(ctx, alpha, ""); err != nil || ok {
		t.Fatalf("空令牌应返回 false：%v %v", ok, err)
	}
	if page, _ := a.GetPage(ctx, alpha); page.Page.Body != "Hand written body." {
		t.Fatalf("过期令牌不应改动页面：%+v", page.Page)
	}

	if ok, err := a.AcceptSuggestion(ctx, alpha, sugg.Token); err != nil || !ok {
		t.Fatalf("采用：%v %v", ok, err)
	}
	page, _ := a.GetPage(ctx, alpha)
	if page.Page.Body != sugg.Draft.Body || page.Page.ManualEditedAt == nil {
		t.Fatalf("采用后应换成建议正文，页面仍标为手动维护（后续来源更新继续走建议）：%+v", page.Page)
	}
	if revs, _ := a.ListRevisions(ctx, alpha); len(revs) == 0 || revs[0].Body != "Hand written body." {
		t.Fatalf("采用前应保存手写版本：%+v", revs)
	}
	if s, _ := a.Suggestion(ctx, alpha); s != nil {
		t.Fatalf("采用后建议应被清掉：%+v", s)
	}
	if ok, _ := a.AcceptSuggestion(ctx, alpha, sugg.Token); ok {
		t.Fatal("同一令牌不能采用两次")
	}
}
