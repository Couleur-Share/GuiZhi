package servicestest

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// DocSite 是 services.DocSite 的假实现：批次与页面明细放在内存里，记录调用、可注入失败、可让读取卡住。
// 零值可用；所有方法并发安全，读出的批次与页面都是副本（调用方改动不会回写，后台再写也不会影响已读出的快照）。
//
// 状态变化只做到「界面看得见」的程度：暂停 → paused、继续 → running、重试 → running、取消 → canceled，
// 页面计数与明细由测试用 SetJobs / SetPages 直接给定。
type DocSite struct {
	mu       sync.Mutex
	jobs     []services.CrawlJob
	pages    map[string][]services.CrawlPage
	failures map[string]error
	calls    []string
	created  []services.CrawlInput
	nextID   int

	listGate chan struct{}
	getGate  chan struct{}
}

var _ services.DocSite = (*DocSite)(nil)

// SetJobs 整体替换批次列表（按给定顺序返回）。
func (f *DocSite) SetJobs(jobs ...services.CrawlJob) {
	f.mu.Lock()
	f.jobs = cloneJobs(jobs)
	f.mu.Unlock()
}

// SetPages 设置一个批次的页面明细（整体替换）。
func (f *DocSite) SetPages(jobID string, pages ...services.CrawlPage) {
	f.mu.Lock()
	if f.pages == nil {
		f.pages = map[string][]services.CrawlPage{}
	}
	f.pages[jobID] = append([]services.CrawlPage(nil), pages...)
	f.mu.Unlock()
}

// FailOn 让指定操作失败：op 是 create、list、get、pause、resume、retry、cancel 之一，
// 也可以写成 op:id（只让针对该批次的调用失败，例如 "pause:job-1"）。err 为 nil 取消注入。
func (f *DocSite) FailOn(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures == nil {
		f.failures = map[string]error{}
	}
	if err == nil {
		delete(f.failures, op)
		return
	}
	f.failures[op] = err
}

// HoldList 让之后的 ListJobs 卡住，直到返回的函数被调用；用来构造「旧响应晚于新响应」的竞态。
func (f *DocSite) HoldList() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.listGate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// HoldGet 同 HoldList，针对 GetJob。
func (f *DocSite) HoldGet() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.getGate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// Calls 返回调用记录，每项形如 "create"、"list"、"get:job-1"、"pause:job-1"。
func (f *DocSite) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// Created 返回迄今创建批次时收到的输入。
func (f *DocSite) Created() []services.CrawlInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]services.CrawlInput, len(f.created))
	for i, in := range f.created {
		out[i] = cloneInput(in)
	}
	return out
}

// Job 读一个批次的当前内容（测试断言用，不记入调用）。
func (f *DocSite) Job(id string) (services.CrawlJob, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.indexLocked(id); i >= 0 {
		return cloneJob(f.jobs[i]), true
	}
	return services.CrawlJob{}, false
}

// record 记一次调用并返回应当注入的失败（先查带 id 的键，再查整个操作）。
func (f *DocSite) record(op, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry := op
	if id != "" {
		entry = op + ":" + id
	}
	f.calls = append(f.calls, entry)
	if err := f.failures[entry]; err != nil {
		return err
	}
	return f.failures[op]
}

func (f *DocSite) indexLocked(id string) int {
	for i := range f.jobs {
		if f.jobs[i].ID == id {
			return i
		}
	}
	return -1
}

func (f *DocSite) CreateJob(_ context.Context, in services.CrawlInput) (string, error) {
	if err := f.record("create", ""); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("job-%d", f.nextID)
	f.created = append(f.created, cloneInput(in))
	f.jobs = append([]services.CrawlJob{{ID: id, Input: cloneInput(in), Status: services.CrawlJobPending, Counts: map[string]int{}}}, f.jobs...)
	return id, nil
}

func (f *DocSite) ListJobs(ctx context.Context) ([]services.CrawlJob, error) {
	err := f.record("list", "")
	f.mu.Lock()
	gate := f.listGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneJobs(f.jobs), nil
}

func (f *DocSite) GetJob(ctx context.Context, id string) (services.CrawlJobDetail, error) {
	err := f.record("get", id)
	f.mu.Lock()
	gate := f.getGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return services.CrawlJobDetail{}, ctx.Err()
		}
	}
	if err != nil {
		return services.CrawlJobDetail{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.indexLocked(id)
	if i < 0 {
		return services.CrawlJobDetail{Pages: []services.CrawlPage{}}, nil
	}
	job := cloneJob(f.jobs[i])
	return services.CrawlJobDetail{Job: &job, Pages: append([]services.CrawlPage{}, f.pages[id]...)}, nil
}

// transition 是暂停、继续、重试与取消的共同部分：记录、注入失败、批次不存在报错，再按 next 改状态。
// canceledBlocks 为真时已取消的批次不可操作（继续的处理器是这样）。
func (f *DocSite) transition(op, id, next string, canceledBlocks bool) error {
	if err := f.record(op, id); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.indexLocked(id)
	if i < 0 {
		return errors.New("crawl job not found")
	}
	if canceledBlocks && f.jobs[i].Status == services.CrawlJobCanceled {
		return errors.New("crawl job not found or already canceled")
	}
	f.jobs[i].Status = next
	return nil
}

func (f *DocSite) PauseJob(_ context.Context, id string) error {
	return f.transition("pause", id, services.CrawlJobPaused, false)
}

func (f *DocSite) ResumeJob(_ context.Context, id string) error {
	return f.transition("resume", id, services.CrawlJobRunning, true)
}

func (f *DocSite) RetryJob(_ context.Context, id string) error {
	return f.transition("retry", id, services.CrawlJobRunning, false)
}

func (f *DocSite) CancelJob(_ context.Context, id string) error {
	return f.transition("cancel", id, services.CrawlJobCanceled, false)
}

func cloneInput(in services.CrawlInput) services.CrawlInput {
	out := in
	out.Seeds = append([]services.CrawlSeed(nil), in.Seeds...)
	if in.CollectionID != nil {
		c := *in.CollectionID
		out.CollectionID = &c
	}
	return out
}

func cloneJob(j services.CrawlJob) services.CrawlJob {
	out := j
	out.Input = cloneInput(j.Input)
	out.Counts = make(map[string]int, len(j.Counts))
	for k, v := range j.Counts {
		out.Counts[k] = v
	}
	return out
}

func cloneJobs(jobs []services.CrawlJob) []services.CrawlJob {
	out := make([]services.CrawlJob, len(jobs))
	for i, j := range jobs {
		out[i] = cloneJob(j)
	}
	return out
}
