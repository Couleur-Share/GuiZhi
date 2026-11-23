package servicestest

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

var (
	themedReqA = services.ThemedRequest{ItemID: "item-a", SourceKind: services.ThemedKindBody}
	themedReqB = services.ThemedRequest{ItemID: "item-b", SourceKind: services.ThemedKindBody}
)

func themedOpts() services.ThemedOptions {
	return services.ThemedOptions{Style: "plain", MaxImages: 2}
}

// themedRecorder 收集 OnProgress 的推送。
type themedRecorder struct {
	mu    sync.Mutex
	tasks []services.ThemedTask
}

func (r *themedRecorder) fn(t services.ThemedTask) {
	r.mu.Lock()
	r.tasks = append(r.tasks, t)
	r.mu.Unlock()
}

func (r *themedRecorder) states() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.tasks))
	for i, t := range r.tasks {
		out[i] = t.State + "/" + t.Stage
	}
	return out
}

func TestThemedFakeZeroValueState(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	st, err := f.State(ctx, themedReqA)
	if err != nil || st.HasPage || st.Task != nil || st.Stale {
		t.Fatalf("零值应是「没有页面、没有任务」：%+v %v", st, err)
	}
	got, err := f.Get(ctx, services.ThemedGetInput{ThemedRequest: themedReqA})
	if err != nil || got.Page != nil || got.Previous || got.Models.Text == nil || got.Models.Image == nil || got.Search == nil {
		t.Fatalf("零值 Get：两个模型都算已配置、搜索状态给零值：%+v %v", got, err)
	}
	if refs, err := f.References(ctx, themedReqA); err != nil || refs == nil || len(refs) != 0 {
		t.Fatalf("没有参考资料应是空切片：%+v %v", refs, err)
	}
	if tasks, err := f.ListTasks(ctx); err != nil || len(tasks) != 0 {
		t.Fatalf("没有任务：%+v %v", tasks, err)
	}
	if _, err := f.ExportHTML(ctx, services.ThemedExportInput{ThemedRequest: themedReqA}); err == nil {
		t.Fatal("没有页面时导出应被拒绝")
	}
}

func TestThemedFakeGenerateValidation(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	f.SetModels("", "")
	if _, err := f.Generate(ctx, themedReqA, themedOpts()); err == nil || !strings.Contains(err.Error(), "text model") {
		t.Fatalf("没有文本模型不能生成：%v", err)
	}
	f.SetModels("text", "")
	withImages := themedOpts()
	withImages.GenerateImages = true
	if _, err := f.Generate(ctx, themedReqA, withImages); err == nil || !strings.Contains(err.Error(), "image model") {
		t.Fatalf("要生图却没有生图模型：%v", err)
	}
	withImages.MaxImages = 0
	f.SetModels("text", "image")
	if _, err := f.Generate(ctx, themedReqA, withImages); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("开了生图却把上限设为 0：%v", err)
	}

	task, err := f.Generate(ctx, themedReqA, themedOpts())
	if err != nil || task.State != services.ThemedTaskQueued || task.ItemID != themedReqA.ItemID || !task.Active() {
		t.Fatalf("应建一个排队中的任务：%+v %v", task, err)
	}
	if _, err := f.Generate(ctx, themedReqA, themedOpts()); err == nil || !strings.Contains(err.Error(), "already active") {
		t.Fatalf("同一份内容已有活动任务：%v", err)
	}
	other, err := f.Generate(ctx, themedReqB, themedOpts())
	if err != nil || other.ID == task.ID {
		t.Fatalf("另一份内容不受影响：%+v %v", other, err)
	}
	calls := f.Calls()
	if len(calls.Generates) != 6 || calls.Generates[3].Opts.Style != "plain" {
		t.Fatalf("每次调用都应被记录（含被拒的）：%+v", calls.Generates)
	}
}

func TestThemedFakeProgressAndCompletion(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	var rec themedRecorder
	cancel := f.OnProgress(rec.fn)

	// 先有一版页面：新版发布后旧的退为上一版。
	old := f.PutPage(themedReqA, services.ThemedPage{})
	task, _ := f.Generate(ctx, themedReqA, themedOpts())
	f.UpdateTask(task.ID, func(tk *services.ThemedTask) {
		tk.State, tk.Stage, tk.Completed, tk.Total = services.ThemedTaskRunning, services.ThemedStageWrite, 1, 4
	})
	if !f.CompleteTask(task.ID) || f.CompleteTask("missing") {
		t.Fatal("完成存在的任务返回 true，不存在的返回 false")
	}
	if got := rec.states(); !slices.Equal(got, []string{"queued/prepare", "running/write", "completed/done"}) {
		t.Fatalf("推送序列：%v", got)
	}
	res, _ := f.Get(ctx, services.ThemedGetInput{ThemedRequest: themedReqA})
	if res.Page == nil || res.Page.ID == old.ID || !res.Previous || res.Task == nil || res.Task.State != services.ThemedTaskCompleted {
		t.Fatalf("发布后：新页面、有上一版、最近任务已完成：%+v", res)
	}
	st, _ := f.State(ctx, themedReqA)
	if !st.HasPage || st.VersionID != res.Page.ID || st.FormatVersion != 3 {
		t.Fatalf("状态应反映新版本：%+v", st)
	}

	// 退订后不再收到推送；订阅数归零。
	cancel()
	cancel()
	if f.Subscribers() != 0 {
		t.Fatalf("退订后订阅数应为 0：%d", f.Subscribers())
	}
	f.FailTask(task.ID, "boom")
	if n := len(rec.states()); n != 3 {
		t.Fatalf("退订后不应再收到推送：%d", n)
	}
}

func TestThemedFakeCancelResumeOffline(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	var rec themedRecorder
	f.OnProgress(rec.fn)
	task, _ := f.Generate(ctx, themedReqA, themedOpts())

	if same, err := f.Resume(ctx, task.ID); err != nil || same.State != services.ThemedTaskQueued || len(rec.states()) != 1 {
		t.Fatalf("活动任务原样返回、不报错、不推送：%+v %v %v", same, err, rec.states())
	}
	cancelled, err := f.Cancel(ctx, task.ID)
	if err != nil || cancelled.State != services.ThemedTaskCancelled || cancelled.Error == "" {
		t.Fatalf("取消：%+v %v", cancelled, err)
	}
	n := len(rec.states())
	if again, err := f.Cancel(ctx, task.ID); err != nil || again.State != services.ThemedTaskCancelled || len(rec.states()) != n {
		t.Fatalf("再取消一次：状态不变、不再推送：%+v %v", again, err)
	}
	if _, err := f.Cancel(ctx, "missing"); err == nil {
		t.Fatal("取消不存在的任务应报错")
	}
	resumed, err := f.Resume(ctx, task.ID)
	if err != nil || resumed.State != services.ThemedTaskQueued || resumed.Error != "" {
		t.Fatalf("继续：%+v %v", resumed, err)
	}

	// 不联网继续只对停在查证阶段的任务有效。
	if _, err := f.ContinueOffline(ctx, task.ID); err == nil {
		t.Fatal("活动任务不能「不联网继续」")
	}
	f.UpdateTask(task.ID, func(tk *services.ThemedTask) {
		tk.State, tk.Stage = services.ThemedTaskPartial, services.ThemedStageResearch
	})
	if off, err := f.ContinueOffline(ctx, task.ID); err != nil || off.State != services.ThemedTaskQueued {
		t.Fatalf("不联网继续：%+v %v", off, err)
	}
	calls := f.Calls()
	if len(calls.Cancels) != 3 || len(calls.Resumes) != 2 || len(calls.Offlines) != 2 {
		t.Fatalf("调用记录：%+v", calls)
	}

	legacy := f.PutTask(services.ThemedTask{ItemID: "item-c", SourceKind: "body", State: services.ThemedTaskFailed, LegacyFormat: true})
	if _, err := f.Resume(ctx, legacy.ID); err == nil {
		t.Fatal("旧格式任务不能继续")
	}
}

func TestThemedFakeRegenerateAssetAndRestore(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	if _, err := f.RegenerateAsset(ctx, themedReqA, "img-1"); err == nil {
		t.Fatal("没有页面时不能重做图片")
	}
	f.PutPage(themedReqA, services.ThemedPage{Assets: []services.ThemedAsset{
		{ID: "img-1", Role: services.ThemedAssetGenerated, Status: services.ThemedAssetFailed, Error: "failed"},
	}})
	if _, err := f.RegenerateAsset(ctx, themedReqA, "nope"); err == nil {
		t.Fatal("图片不存在应报错")
	}
	task, err := f.RegenerateAsset(ctx, themedReqA, "img-1")
	if err != nil || task.AssetID != "img-1" {
		t.Fatalf("重做图片任务：%+v %v", task, err)
	}
	if _, err := f.RegenerateAsset(ctx, themedReqA, "img-1"); err == nil {
		t.Fatal("已有活动任务时不能再发起")
	}
	f.CompleteTask(task.ID)
	got, _ := f.Get(ctx, services.ThemedGetInput{ThemedRequest: themedReqA})
	if got.Page == nil || !got.Page.Assets[0].Ready() || got.Page.Assets[0].Error != "" || got.Previous {
		t.Fatalf("只把那张图置为 ready，不产生新版本：%+v", got.Page)
	}

	// 恢复上一版：当前与上一版互换。
	f.PutPrevious(themedReqA, services.ThemedPage{ID: "prev-1"})
	if err := f.RestorePrevious(ctx, themedReqA); err != nil {
		t.Fatal(err)
	}
	got, _ = f.Get(ctx, services.ThemedGetInput{ThemedRequest: themedReqA})
	if got.Page.ID != "prev-1" || got.Page.Role != services.ThemedRoleCurrent || !got.Previous {
		t.Fatalf("上一版应成为当前版，原当前版退为上一版：%+v", got)
	}
	if err := f.RestorePrevious(ctx, themedReqB); err == nil {
		t.Fatal("没有上一版应报错")
	}
}

func TestThemedFakeRemoveCancelsActive(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	var rec themedRecorder
	f.OnProgress(rec.fn)
	f.PutPage(themedReqA, services.ThemedPage{})
	task, _ := f.Generate(ctx, themedReqA, themedOpts())
	if err := f.Remove(ctx, themedReqA); err != nil {
		t.Fatal(err)
	}
	got, _ := f.Task(task.ID)
	if got.State != services.ThemedTaskCancelled {
		t.Fatalf("删除应先取消进行中的任务：%+v", got)
	}
	if st, _ := f.State(ctx, themedReqA); st.HasPage {
		t.Fatalf("页面应已删除：%+v", st)
	}
	if got := rec.states(); !slices.Equal(got, []string{"queued/prepare", "cancelled/prepare"}) {
		t.Fatalf("取消应推送：%v", got)
	}
}

func TestThemedFakeLegacyDocumentAndStale(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	f.PutPage(themedReqA, services.ThemedPage{FormatVersion: 1})
	f.SetDocument(themedReqA, "doc")
	f.SetStale(themedReqA, true)
	got, _ := f.Get(ctx, services.ThemedGetInput{ThemedRequest: themedReqA})
	if got.Document != "doc" || !got.Stale || got.Page == nil || !got.Page.Legacy() {
		t.Fatalf("旧格式页面带整页文档与过期标记：%+v", got)
	}
	got, _ = f.Get(ctx, services.ThemedGetInput{ThemedRequest: themedReqA, MetadataOnly: true})
	if got.Document != "" {
		t.Fatalf("MetadataOnly 不给文档：%q", got.Document)
	}
	if _, err := f.Generate(ctx, themedReqA, services.ThemedOptions{Style: "x", Action: services.ThemedActionRevise}); err == nil {
		t.Fatal("旧格式页面不能调整")
	}
	if st, _ := f.State(ctx, themedReqA); !st.Stale || st.FormatVersion != 1 {
		t.Fatalf("状态：%+v", st)
	}
}

func TestThemedFakeListTasksOrderAndPreview(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	first, _ := f.Generate(ctx, themedReqA, themedOpts())
	second, _ := f.Generate(ctx, themedReqB, themedOpts())
	f.UpdateTask(first.ID, func(tk *services.ThemedTask) { tk.Completed = 1 })
	tasks, err := f.ListTasks(ctx)
	if err != nil || len(tasks) != 2 || tasks[0].ID != first.ID || tasks[1].ID != second.ID {
		t.Fatalf("按最近更新倒序：%+v %v", tasks, err)
	}

	if p, err := f.Preview(ctx, themedReqA, first.ID); err != nil || p != nil {
		t.Fatalf("没有预览：%+v %v", p, err)
	}
	f.SetPreview(themedReqA, &services.ThemedPreview{Revision: 3})
	if p, err := f.Preview(ctx, themedReqA, first.ID); err != nil || p == nil || p.Revision != 3 {
		t.Fatalf("预览：%+v %v", p, err)
	}
	if _, err := f.Preview(ctx, themedReqA, second.ID); err == nil {
		t.Fatal("任务不属于这份内容应报错")
	}
}

func TestThemedFakeExportAndSearchConfig(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	f.PutPage(themedReqA, services.ThemedPage{})
	if r, err := f.ExportHTML(ctx, services.ThemedExportInput{ThemedRequest: themedReqA}); err != nil || !r.Cancelled || r.Path != "" {
		t.Fatalf("默认走用户取消：%+v %v", r, err)
	}
	f.SetExportPath("out.html")
	if r, err := f.ExportHTML(ctx, services.ThemedExportInput{ThemedRequest: themedReqA, StaticOnly: true}); err != nil || r.Path != "out.html" || r.Cancelled {
		t.Fatalf("导出：%+v %v", r, err)
	}

	key, enabled := "sk-secret-value", true
	st, err := f.ConfigureSearch(ctx, services.ThemedSearchConfigInput{Provider: "tavily", APIKey: &key, DefaultEnabled: &enabled})
	if err != nil || !st.Configured || !st.DefaultEnabled || !slices.Equal(st.ConfiguredProviders, []string{"tavily"}) {
		t.Fatalf("配置搜索：%+v %v", st, err)
	}
	empty := ""
	st, _ = f.ConfigureSearch(ctx, services.ThemedSearchConfigInput{APIKey: &empty})
	if st.Configured || len(st.ConfiguredProviders) != 0 {
		t.Fatalf("清除密钥后应变为未配置：%+v", st)
	}
	for _, rec := range f.Calls().SearchConfigs {
		if strings.Contains(rec, key) {
			t.Fatalf("调用记录不能带出密钥明文：%q", rec)
		}
	}
}

func TestThemedFakeFailAndHold(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	boom := errors.New("injected")
	f.Fail(ThemedOpGenerate, boom)
	if _, err := f.Generate(ctx, themedReqA, themedOpts()); !errors.Is(err, boom) {
		t.Fatalf("注入失败：%v", err)
	}
	if tasks, _ := f.ListTasks(ctx); len(tasks) != 0 {
		t.Fatalf("失败时不改状态：%+v", tasks)
	}
	f.Fail(ThemedOpGenerate, nil)

	release, arrived := f.Hold(ThemedOpGet)
	done := make(chan services.ThemedReadResult, 1)
	go func() {
		r, _ := f.Get(ctx, services.ThemedGetInput{ThemedRequest: themedReqA})
		done <- r
	}()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("调用没有到达卡点")
	}
	f.PutPage(themedReqA, services.ThemedPage{ID: "late"})
	release()
	select {
	case r := <-done:
		if r.Page == nil || r.Page.ID != "late" {
			t.Fatalf("卡住的调用在放行后读到最新状态：%+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("放行后调用没有返回")
	}
}

// 并发调用在 -race 下不应有数据竞争，任务标识不重复。
func TestThemedFakeConcurrent(t *testing.T) {
	ctx := t.Context()
	f := &ThemedFake{}
	var rec themedRecorder
	f.OnProgress(rec.fn)
	var wg sync.WaitGroup
	ids := make(chan string, 32)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := services.ThemedRequest{ItemID: fmt.Sprintf("item-%d", i), SourceKind: services.ThemedKindBody}
			task, err := f.Generate(ctx, req, themedOpts())
			if err != nil {
				t.Error(err)
				return
			}
			ids <- task.ID
			f.UpdateTask(task.ID, func(tk *services.ThemedTask) { tk.State = services.ThemedTaskRunning })
			_, _ = f.ListTasks(ctx)
			_, _ = f.State(ctx, req)
			f.CompleteTask(task.ID)
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("任务标识重复：%s", id)
		}
		seen[id] = true
	}
	if len(seen) != 16 || len(rec.states()) != 16*3 {
		t.Fatalf("任务数 %d，推送数 %d", len(seen), len(rec.states()))
	}
}
