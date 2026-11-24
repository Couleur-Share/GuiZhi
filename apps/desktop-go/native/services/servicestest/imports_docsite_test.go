package servicestest

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

// 创建批次：新批次排在最前、带收到的输入副本；之后读出的是副本，改它不影响假实现里的数据。
func TestDocSiteFakeCreateAndSnapshots(t *testing.T) {
	ctx := t.Context()
	f := &DocSite{}
	col := "c1"
	in := services.CrawlInput{
		Purpose: "documents", MaxPages: 50, MaxDepth: 2, DuplicatePolicy: services.CrawlDuplicateSkip, CollectionID: &col,
		Seeds: []services.CrawlSeed{{URL: "https://docs.example.com/guide/", Mode: services.CrawlSeedDirectory, Directory: "/guide/"}},
	}
	first, err := f.CreateJob(ctx, in)
	if err != nil || first != "job-1" {
		t.Fatalf("创建：%q %v", first, err)
	}
	second, _ := f.CreateJob(ctx, in)
	in.Seeds[0].URL = "https://changed.example.com/" // 调用方改自己的输入，不影响已记录的
	col = "changed"

	jobs, err := f.ListJobs(ctx)
	if err != nil || len(jobs) != 2 || jobs[0].ID != second || jobs[1].ID != first {
		t.Fatalf("列表顺序：%+v %v", jobs, err)
	}
	if jobs[0].Input.Seeds[0].URL != "https://docs.example.com/guide/" || *jobs[0].Input.CollectionID != "c1" {
		t.Fatalf("输入应是创建时的副本：%+v", jobs[0].Input)
	}
	jobs[0].Counts["added"] = 99
	jobs[0].Input.Seeds[0].URL = "x"
	if again, _ := f.ListJobs(ctx); again[0].Counts["added"] != 0 || again[0].Input.Seeds[0].URL == "x" {
		t.Fatalf("读出的批次应是副本：%+v", again[0])
	}
	if created := f.Created(); len(created) != 2 || created[0].Seeds[0].URL != "https://docs.example.com/guide/" {
		t.Fatalf("Created：%+v", created)
	}
}

// 状态迁移：暂停、继续、重试、取消按批次生效；缺失批次与已取消批次的继续带原因报错；GetJob 缺失是 nil 而不是错误。
func TestDocSiteFakeTransitions(t *testing.T) {
	ctx := t.Context()
	f := &DocSite{}
	f.SetJobs(services.CrawlJob{ID: "a", Status: services.CrawlJobRunning})
	f.SetPages("a", services.CrawlPage{ID: "p1", JobID: "a", URL: "https://docs.example.com/guide/x", Status: services.CrawlPageFailed})

	steps := []struct {
		name string
		run  func() error
		want string
	}{
		{"pause", func() error { return f.PauseJob(ctx, "a") }, services.CrawlJobPaused},
		{"resume", func() error { return f.ResumeJob(ctx, "a") }, services.CrawlJobRunning},
		{"retry", func() error { return f.RetryJob(ctx, "a") }, services.CrawlJobRunning},
		{"cancel", func() error { return f.CancelJob(ctx, "a") }, services.CrawlJobCanceled},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			t.Fatalf("%s：%v", s.name, err)
		}
		if job, _ := f.Job("a"); job.Status != s.want {
			t.Fatalf("%s 之后状态 %q，应为 %q", s.name, job.Status, s.want)
		}
	}
	if err := f.ResumeJob(ctx, "a"); err == nil {
		t.Fatal("已取消的批次不可继续")
	}
	if err := f.PauseJob(ctx, "missing"); err == nil || err.Error() == "" {
		t.Fatalf("缺失批次应带原因报错：%v", err)
	}
	detail, err := f.GetJob(ctx, "missing")
	if err != nil || detail.Job != nil || detail.Pages == nil {
		t.Fatalf("缺失批次的明细：%+v %v", detail, err)
	}
	detail, _ = f.GetJob(ctx, "a")
	if detail.Job == nil || len(detail.Pages) != 1 || detail.Pages[0].ID != "p1" {
		t.Fatalf("明细：%+v", detail)
	}
}

// 失败注入：整个操作或只针对某个批次；调用无论成败都记录。
func TestDocSiteFakeFailureInjection(t *testing.T) {
	ctx := t.Context()
	f := &DocSite{}
	f.SetJobs(services.CrawlJob{ID: "a", Status: services.CrawlJobRunning}, services.CrawlJob{ID: "b", Status: services.CrawlJobRunning})
	boom := errors.New("robots.txt unavailable")

	f.FailOn("pause:a", boom)
	if err := f.PauseJob(ctx, "a"); !errors.Is(err, boom) {
		t.Fatalf("针对 a 的注入：%v", err)
	}
	if err := f.PauseJob(ctx, "b"); err != nil {
		t.Fatalf("b 不受影响：%v", err)
	}
	f.FailOn("list", boom)
	if _, err := f.ListJobs(ctx); !errors.Is(err, boom) {
		t.Fatalf("list 注入：%v", err)
	}
	f.FailOn("list", nil)
	if _, err := f.ListJobs(ctx); err != nil {
		t.Fatalf("取消注入后：%v", err)
	}
	f.FailOn("create", boom)
	if _, err := f.CreateJob(ctx, services.CrawlInput{}); !errors.Is(err, boom) {
		t.Fatalf("create 注入：%v", err)
	}
	if got := fmt.Sprint(f.Calls()); got != "[pause:a pause:b list list create]" {
		t.Fatalf("调用记录：%s", got)
	}
}

// 卡住的读取：HoldList / HoldGet 卡住直到释放；ctx 取消也能放走。
func TestDocSiteFakeHoldAndContext(t *testing.T) {
	f := &DocSite{}
	f.SetJobs(services.CrawlJob{ID: "a"})
	release := f.HoldList()
	done := make(chan []services.CrawlJob, 1)
	go func() {
		jobs, _ := f.ListJobs(t.Context())
		done <- jobs
	}()
	select {
	case <-done:
		t.Fatal("释放之前不应返回")
	case <-time.After(30 * time.Millisecond):
	}
	release()
	select {
	case jobs := <-done:
		if len(jobs) != 1 {
			t.Fatalf("释放后的结果：%+v", jobs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("释放后应返回")
	}

	f.HoldGet()
	ctx, cancel := contextWithCancel(t)
	errc := make(chan error, 1)
	go func() {
		_, err := f.GetJob(ctx, "a")
		errc <- err
	}()
	cancel()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("ctx 取消应返回错误")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消应放走被卡住的读取")
	}
}

// 并发：多个 goroutine 同时创建、读取、改状态，-race 下不得报竞态，创建的 id 互不相同。
func TestDocSiteFakeConcurrent(t *testing.T) {
	ctx := t.Context()
	f := &DocSite{}
	const workers = 8
	ids := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := f.CreateJob(ctx, services.CrawlInput{Purpose: "documents"})
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = id
			_, _ = f.ListJobs(ctx)
			_, _ = f.GetJob(ctx, id)
			_ = f.PauseJob(ctx, id)
			_ = f.ResumeJob(ctx, id)
			_ = f.Calls()
			_ = f.Created()
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			t.Fatalf("批次 id 应互不相同：%v", ids)
		}
		seen[id] = true
	}
	if jobs, _ := f.ListJobs(ctx); len(jobs) != workers {
		t.Fatalf("批次数：%d", len(jobs))
	}
}
