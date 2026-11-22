package wiki

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// compileRig 摆好一份有可编译资料的假服务并打开编译对话框。
func compileRig(t *testing.T, entries ...services.WikiCompileCandidate) *rig {
	t.Helper()
	r := newRig(t)
	r.seed()
	r.svc.SetPreview(previewOf(entries...))
	r.ready()
	r.compile().Open()
	return r
}

func TestCompileOpenLoadsPreviewJobsAndDefaultSelection(t *testing.T) {
	r := newRig(t)
	r.svc.PutJob(services.WikiCompileJob{Status: services.WikiJobCompleted, Total: 3, Completed: 3})
	entries := append(manyReady(14),
		candidate("up", services.WikiCandidateUpgrade, 1, 0),
		candidate("rv", services.WikiCandidateReview, 1, 0),
		candidate("cur", services.WikiCandidateCurrent, 1, 1))
	r.svc.SetPreview(previewOf(entries...))
	r.compile().Open()
	v := r.cview()
	if !v.Open || v.Loading || v.Error != "" || v.Preview == nil || !v.ModelReady || len(v.Jobs) != 1 {
		t.Fatalf("%+v", v)
	}
	if len(v.Selected) != 10 || v.Selected[0] != "i000" || v.Selected[9] != "i009" {
		t.Fatalf("默认只勾前 10 条可选资料：%v", v.Selected)
	}
	if v.CanStart() != true {
		t.Fatal("可以开始")
	}
	r.compile().Close()
	if r.cview().Open {
		t.Fatal("关闭")
	}
}

func TestCompileDefaultSelectionSkipsUnselectable(t *testing.T) {
	r := compileRig(t,
		candidate("w", services.WikiCandidateWaiting, 1, 0),
		candidate("x", services.WikiCandidateExcluded, 1, 0),
		candidate("c", services.WikiCandidateCurrent, 1, 1),
		candidate("u", services.WikiCandidateUpgrade, 1, 0),
		candidate("ok", services.WikiCandidateReady, 1, 0))
	if got := r.cview().Selected; !slices.Equal(got, []string{"u", "ok"}) {
		t.Fatalf("只有可执行与待升级能默认勾选：%v", got)
	}
}

func TestCompileToggleRules(t *testing.T) {
	r := compileRig(t,
		candidate("a", services.WikiCandidateReady, 1, 0),
		candidate("b", services.WikiCandidateReady, 1, 0),
		candidate("w", services.WikiCandidateWaiting, 1, 0))
	c := r.compile()
	c.Toggle("a", false)
	if got := r.cview().Selected; !slices.Equal(got, []string{"b"}) {
		t.Fatalf("取消勾选：%v", got)
	}
	c.Toggle("a", true)
	c.Toggle("a", true) // 重复勾选不重复入列
	if got := r.cview().Selected; !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("%v", got)
	}
	c.Toggle("w", true) // 不可选状态
	c.Toggle("ghost", true)
	c.Toggle("zzz", false)
	if got := r.cview().Selected; len(got) != 2 {
		t.Fatalf("不可选与不存在的条目忽略：%v", got)
	}
	if v := r.cview(); !v.IsSelected("a") || v.IsSelected("w") {
		t.Fatal("IsSelected")
	}
	if got := ids2(r.cview().Chosen()); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Chosen 按预览顺序：%v", got)
	}
	c.Toggle("a", false)
	c.Toggle("b", false)
	if r.cview().CanStart() {
		t.Fatal("没有勾选不能开始")
	}
}

func ids2(es []services.WikiCompileCandidate) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

func TestCompileFilterLimitAndShowMore(t *testing.T) {
	entries := manyReady(120)
	entries = append(entries, candidate("cur1", services.WikiCandidateCurrent, 1, 1))
	entries[7].Title = "Special Needle"
	r := compileRig(t, entries...)
	c := r.compile()
	v := r.cview()
	if len(v.Visible()) != 120 || len(v.Shown()) != 50 || !v.HasMore() {
		t.Fatalf("已最新的不列：可见 %d 显示 %d", len(v.Visible()), len(v.Shown()))
	}
	c.ShowMore()
	c.ShowMore()
	if v := r.cview(); len(v.Shown()) != 120 || v.HasMore() {
		t.Fatalf("展开两页后全部可见：%d", len(v.Shown()))
	}
	c.SetFilter("needle")
	v = r.cview()
	if len(v.Visible()) != 1 || v.Visible()[0].ID != "i007" || v.Limit != 50 {
		t.Fatalf("筛选不区分大小写且回到第一页：%v limit=%d", ids2(v.Visible()), v.Limit)
	}
	c.SetFilter("no such item")
	if v := r.cview(); len(v.Visible()) != 0 || len(v.Shown()) != 0 || v.HasMore() {
		t.Fatal("筛不到时为空")
	}
}

func TestCompileViewBeforePreviewIsEmpty(t *testing.T) {
	r := newRig(t)
	v := r.cview()
	if v.Visible() != nil || v.Chosen() != nil || len(v.Shown()) != 0 || v.HasMore() || v.CanStart() {
		t.Fatalf("%+v", v)
	}
	if n, b, lo, hi := v.Estimate(); n+b+lo+hi != 0 {
		t.Fatal("估算为零")
	}
	c := r.compile()
	c.Toggle("a", true) // 没有预览时忽略
	if len(r.cview().Selected) != 0 {
		t.Fatal("无预览不勾选")
	}
}

func TestCompileEstimate(t *testing.T) {
	r := compileRig(t,
		candidate("a", services.WikiCandidateReady, 4, 0),
		candidate("b", services.WikiCandidateUpgrade, 3, 2))
	n, blocks, lo, hi := r.cview().Estimate()
	if n != 2 || blocks != 7 || lo != 5 || hi != 10 {
		t.Fatalf("条数 %d 分块 %d 调用 %d-%d（可复用的块不调模型，上限翻倍）", n, blocks, lo, hi)
	}
	r.compile().Toggle("b", false)
	n, blocks, lo, hi = r.cview().Estimate()
	if n != 1 || blocks != 4 || lo != 4 || hi != 8 {
		t.Fatalf("%d %d %d %d", n, blocks, lo, hi)
	}
}

func TestCompileRefreshFailureKeepsOldPreview(t *testing.T) {
	for _, op := range []string{servicestest.WikiOpPreview, servicestest.WikiOpJobs, servicestest.WikiOpConfigured} {
		r := compileRig(t, candidate("a", services.WikiCandidateReady, 1, 0))
		r.svc.Fail(op, errors.New("down: "+op))
		r.compile().Refresh()
		v := r.cview()
		if v.Error != "down: "+op || v.Loading || v.Preview == nil || len(v.Selected) != 1 {
			t.Fatalf("%s：%+v", op, v)
		}
		r.wantNoToast()
		r.svc.Fail(op, nil)
		r.compile().Refresh()
		if r.cview().Error != "" {
			t.Fatalf("%s：重试成功后错误消失", op)
		}
	}
}

func TestCompileRefreshStaleResponseDropped(t *testing.T) {
	r := compileRig(t, candidate("old", services.WikiCandidateReady, 1, 0))
	release, arrived := r.svc.Hold(servicestest.WikiOpPreview)
	wait := inBackground(func() { r.compile().Refresh() })
	<-arrived // 这次读到的是旧预览，卡在入口
	r.svc.SetPreview(previewOf(candidate("new", services.WikiCandidateReady, 1, 0)))
	r.compile().Refresh() // 后发先至
	release()
	wait()
	v := r.cview()
	if len(v.Preview.Entries) != 1 || v.Preview.Entries[0].ID != "new" || v.Loading {
		t.Fatalf("先发的响应迟到不能盖掉新的：%+v", v.Preview.Entries)
	}
}

func TestCompileStartRunsToCompletion(t *testing.T) {
	r := compileRig(t,
		candidate("a", services.WikiCandidateReady, 2, 0),
		candidate("b", services.WikiCandidateReady, 3, 0))
	r.svc.QueueRun(servicestest.WikiRunScript{Progress: []services.WikiCompileJob{
		{Status: services.WikiJobRunning, Total: 5, Completed: 2},
		{Status: services.WikiJobRunning, Total: 5, Completed: 4},
	}})
	if !r.compile().Start() {
		t.Fatalf("应启动：%+v", r.cview())
	}
	v := r.cview()
	if v.Busy || v.Error != "" {
		t.Fatalf("同步 Spawn 下任务已经跑完：%+v", v)
	}
	if len(v.Jobs) != 1 || v.Jobs[0].Status != services.WikiJobCompleted || v.Jobs[0].Completed != v.Jobs[0].Total {
		t.Fatalf("任务列表：%+v", v.Jobs)
	}
	if got := r.svc.Starts(); len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("开始请求带上勾选的两条：%+v", got)
	}
	if r.view().IsCompiling {
		t.Fatal("结束后主状态不再是编译中")
	}
}

func TestCompileStartBusyWhileRunningAndBlocksTogglesAndSecondStart(t *testing.T) {
	r := compileRig(t,
		candidate("a", services.WikiCandidateReady, 2, 0),
		candidate("b", services.WikiCandidateReady, 2, 0))
	r.useGoroutines()
	gate, arrived := make(chan struct{}), make(chan struct{})
	r.svc.QueueRun(servicestest.WikiRunScript{Gate: gate, Arrived: arrived,
		Progress: []services.WikiCompileJob{{Status: services.WikiJobRunning, Total: 4, Completed: 1}}})
	if !r.compile().Start() {
		t.Fatal("应启动")
	}
	<-arrived
	v := r.cview()
	if !v.Busy || v.CanStart() || !r.view().IsCompiling {
		t.Fatalf("跑动期间：%+v", v)
	}
	if r.compile().Start() {
		t.Fatal("有任务在跑不能再开始")
	}
	before := slices.Clone(v.Selected)
	r.compile().Toggle("a", false)
	if !slices.Equal(r.cview().Selected, before) {
		t.Fatal("有任务在跑时勾选不改动")
	}
	if len(r.svc.Starts()) != 1 {
		t.Fatalf("第二次 Start 不应发请求：%d", len(r.svc.Starts()))
	}
	close(gate)
	eventually(t, func() bool { return !r.cview().Busy && !r.view().IsCompiling })
	if j := r.cview().Jobs; len(j) != 1 || j[0].Status != services.WikiJobCompleted {
		t.Fatalf("%+v", j)
	}
}

func TestCompileStartGuards(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 1, 0))
	r.compile().Toggle("a", false)
	if r.compile().Start() || len(r.svc.Starts()) != 0 {
		t.Fatal("没有勾选不开始")
	}
	r.compile().Toggle("a", true)
	r.svc.SetCompileConfigured(false)
	r.compile().Refresh()
	if v := r.cview(); v.ModelReady || v.CanStart() {
		t.Fatalf("模型未配置：%+v", v)
	}
	if r.compile().Start() || len(r.svc.Starts()) != 0 {
		t.Fatal("未配置模型不发请求")
	}
}

func TestCompileStartNoModelAtServerShowsGuidance(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 1, 0))
	r.svc.Fail(servicestest.WikiOpStart, services.ErrAINotConfigured)
	if r.compile().Start() {
		t.Fatal("应失败")
	}
	if v := r.cview(); v.Error != locales.WikiCompileNoModel() || v.Busy {
		t.Fatalf("%+v", v)
	}
	if got := r.log.entries(); len(got) == 0 || !strings.HasPrefix(got[0], "wiki.compileStart|") {
		t.Fatalf("日志：%v", got)
	}
}

func TestCompileStartRejectedReplacesPreviewAndSelection(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 1, 0))
	fresh := previewOf(
		candidate("n1", services.WikiCandidateReady, 1, 0),
		candidate("n2", services.WikiCandidateUpgrade, 1, 0),
		candidate("n3", services.WikiCandidateWaiting, 1, 0))
	r.svc.RejectStart(&services.WikiStartError{Message: "sources changed since preview", Preview: &fresh})
	if r.compile().Start() {
		t.Fatal("应被拒绝")
	}
	v := r.cview()
	if v.Error != "sources changed since preview" || v.Busy || len(v.Preview.Entries) != 3 {
		t.Fatalf("%+v", v)
	}
	if !slices.Equal(v.Selected, []string{"n1", "n2"}) {
		t.Fatalf("勾选改成新预览里全部可选的资料：%v", v.Selected)
	}
	if len(v.Jobs) != 0 {
		t.Fatal("被拒绝不产生任务")
	}
}

func TestCompileStartRejectedWithoutPreviewKeepsOld(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 1, 0))
	r.svc.RejectStart(&services.WikiStartError{Message: "busy elsewhere"})
	if r.compile().Start() {
		t.Fatal("应被拒绝")
	}
	if v := r.cview(); v.Error != "busy elsewhere" || len(v.Preview.Entries) != 1 || !slices.Equal(v.Selected, []string{"a"}) {
		t.Fatalf("%+v", v)
	}
}

func TestCompileRunErrorShowsErrorAndPausesJob(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 2, 0))
	r.svc.QueueRun(servicestest.WikiRunScript{Err: errors.New("provider returned 500")})
	if !r.compile().Start() {
		t.Fatal("任务创建成功就返回 true")
	}
	v := r.cview()
	if v.Busy || v.Error != "provider returned 500" || r.view().IsCompiling {
		t.Fatalf("%+v", v)
	}
	ctl := r.svc.Controls()
	if len(ctl) != 1 || ctl[0].Status != services.WikiJobPaused {
		t.Fatalf("出错的任务要暂停，让用户可以继续：%+v", ctl)
	}
	if j := r.cview().Jobs; len(j) != 1 || j[0].Status != services.WikiJobPaused || !j[0].Resumable() {
		t.Fatalf("刷新后任务列表显示暂停：%+v", j)
	}
	r.wantNoToast()
}

func TestCompileRunErrorPauseFailureIsLoggedOnly(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 2, 0))
	r.svc.QueueRun(servicestest.WikiRunScript{Err: errors.New("run broke")})
	r.svc.Fail(servicestest.WikiOpControl, errors.New("pause broke"))
	r.compile().Start()
	if v := r.cview(); v.Error != "run broke" || v.Busy {
		t.Fatalf("%+v", v)
	}
	if got := r.log.entries(); !slices.Contains(got, "wiki.compilePause|pause broke") {
		t.Fatalf("%v", got)
	}
}

func TestCompileFinalErrorIsShown(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 1, 0))
	r.svc.QueueRun(servicestest.WikiRunScript{Final: services.WikiJobFailed})
	r.compile().Start()
	if v := r.cview(); v.Busy || v.Jobs[0].Status != services.WikiJobFailed {
		t.Fatalf("%+v", v)
	}
}

func TestCompileRunRefreshesCatalogAndPreview(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 1, 0))
	r.page("compiled-new")
	r.svc.SetPreview(previewOf(candidate("a", services.WikiCandidateCurrent, 1, 1)))
	r.compile().Start()
	if len(r.view().Catalog) != 4 {
		t.Fatal("编译结束后目录刷新")
	}
	v := r.cview()
	if len(v.Visible()) != 0 || len(v.Selected) != 0 {
		t.Fatalf("编译结束后预览重读：已最新的不再列出：%+v", v.Preview.Entries)
	}
	if pending, _ := r.view().CompileButton(); pending != 0 {
		t.Fatal("工具栏待编译数同步")
	}
}

func TestCompileControlPauseCancelAndResume(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 2, 0))
	job := r.svc.PutJob(services.WikiCompileJob{Status: services.WikiJobPaused, Total: 2})
	r.compile().Refresh()

	if !r.compile().Control(job.ID, services.WikiJobCancelled) {
		t.Fatal("取消")
	}
	if j := r.cview().Jobs[0]; j.ID != job.ID || j.Status != services.WikiJobCancelled {
		t.Fatalf("原位更新：%+v", j)
	}
	if !r.compile().Control(job.ID, services.WikiJobPaused) {
		t.Fatal("暂停")
	}
	if got, _ := r.svc.Job(job.ID); got.Status != services.WikiJobPaused {
		t.Fatal("后端状态已改")
	}
	r.svc.QueueRun(servicestest.WikiRunScript{})
	if !r.compile().Control(job.ID, services.WikiJobRunning) {
		t.Fatal("继续")
	}
	v := r.cview()
	if v.Busy || v.Jobs[0].Status != services.WikiJobCompleted {
		t.Fatalf("继续后跑完：%+v", v.Jobs)
	}
	if got := r.svc.Runs(); !slices.Equal(got, []string{job.ID}) {
		t.Fatalf("%v", got)
	}
}

func TestCompileControlFailureAndBusyResume(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 1, 0))
	if r.compile().Control("ghost", services.WikiJobPaused) {
		t.Fatal("不存在的任务")
	}
	if v := r.cview(); v.Error == "" {
		t.Fatal("失败写进 Error")
	}
	r.svc.Fail(servicestest.WikiOpControl, errors.New("control broke"))
	if r.compile().Control("x", services.WikiJobPaused) {
		t.Fatal("应失败")
	}
	if r.cview().Error != "control broke" {
		t.Fatalf("%q", r.cview().Error)
	}
	r.svc.Fail(servicestest.WikiOpControl, nil)

	paused := r.svc.PutJob(services.WikiCompileJob{Status: services.WikiJobPaused, Total: 1})
	r.compile().Refresh()
	r.useGoroutines()
	gate, arrived := make(chan struct{}), make(chan struct{})
	r.svc.QueueRun(servicestest.WikiRunScript{Gate: gate, Arrived: arrived})
	if !r.compile().Start() {
		t.Fatal("启动")
	}
	<-arrived
	before := len(r.svc.Controls())
	if r.compile().Control(paused.ID, services.WikiJobRunning) {
		t.Fatal("有任务在跑时不能继续另一个")
	}
	if len(r.svc.Controls()) != before {
		t.Fatal("不发请求")
	}
	close(gate)
	eventually(t, func() bool { return !r.cview().Busy })
}

func TestCompileCancelMidRun(t *testing.T) {
	r := compileRig(t, candidate("a", services.WikiCandidateReady, 3, 0))
	r.useGoroutines()
	gate, arrived := make(chan struct{}), make(chan struct{})
	r.svc.QueueRun(servicestest.WikiRunScript{Gate: gate, Arrived: arrived,
		Progress: []services.WikiCompileJob{{Status: services.WikiJobRunning, Total: 3, Completed: 1}}})
	r.compile().Start()
	<-arrived
	jobID := r.cview().Jobs[0].ID
	if !r.compile().Control(jobID, services.WikiJobCancelled) {
		t.Fatal("取消应成功")
	}
	eventually(t, func() bool { return !r.cview().Busy && !r.view().IsCompiling })
	if v := r.cview(); v.Jobs[0].Status != services.WikiJobCancelled || v.Error != "" {
		t.Fatalf("取消不是错误：%+v", v)
	}
	if !r.cview().CanStart() {
		t.Fatal("取消后可以再开始")
	}
}

func TestJobsShownCapsList(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 15; i++ {
		r.svc.PutJob(services.WikiCompileJob{Status: services.WikiJobCompleted})
	}
	r.compile().Open()
	if v := r.cview(); len(v.Jobs) != 15 || len(v.JobsShown()) != 10 {
		t.Fatalf("%d / %d", len(v.Jobs), len(v.JobsShown()))
	}
	if got := (CompileView{}).JobsShown(); len(got) != 0 {
		t.Fatal("空列表")
	}
}

func TestUpsertAndReplaceJob(t *testing.T) {
	a, b := services.WikiCompileJob{ID: "a", Completed: 1}, services.WikiCompileJob{ID: "b"}
	got := upsertJob([]services.WikiCompileJob{a, b}, services.WikiCompileJob{ID: "b", Completed: 9})
	if len(got) != 2 || got[0].ID != "b" || got[0].Completed != 9 || got[1].ID != "a" {
		t.Fatalf("同 ID 去重并放最前：%+v", got)
	}
	orig := []services.WikiCompileJob{a, b}
	rep := replaceJob(orig, services.WikiCompileJob{ID: "b", Completed: 5})
	if rep[1].Completed != 5 || orig[1].Completed != 0 {
		t.Fatal("原位替换且不改原切片")
	}
	if got := replaceJob(orig, services.WikiCompileJob{ID: "zzz"}); len(got) != 2 {
		t.Fatal("不存在则原样")
	}
}

func TestCompileConcurrentToggleAndViewRace(t *testing.T) {
	entries := manyReady(30)
	r := compileRig(t, entries...)
	r.useGoroutines()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("i%03d", i)
			r.compile().Toggle(id, i%2 == 0)
			r.compile().SetFilter("item")
			_ = r.cview().Shown()
			r.compile().ShowMore()
			r.compile().Refresh()
		}()
	}
	wg.Wait()
	if v := r.cview(); v.Loading || v.Preview == nil {
		t.Fatalf("%+v", v)
	}
}
