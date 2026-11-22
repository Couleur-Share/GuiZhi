package servicestest

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"guizhi-mygo-shell/native/services"
)

// WikiRunScript 描述一次 RunCompile 怎么演：按序推送 Progress 里的任务快照，然后（若有 Gate）等 Gate 关闭或被取消，
// 最后按 Err / Final 收尾。同一个 WikiFake 上排队的脚本按先进先出消费，没有脚本时一步完成。
type WikiRunScript struct {
	// Progress 是依次推给进度回调的任务快照（Status 通常是 running，Completed 递增）。
	Progress []services.WikiCompileJob
	// Gate 非 nil 时，推完进度后阻塞到它被关闭、ctx 被取消或 ControlCompile(cancelled)。
	Gate chan struct{}
	// Err 非 nil 时 RunCompile 以该错误返回，任务落为 failed。
	Err error
	// Final 是正常收尾时的任务状态，空取 completed。
	Final string
	// Arrived 非 nil 时在脚本开始演出时关闭，测试用来等「编译已经跑起来」。
	Arrived chan struct{}
}

type wikiCompileState struct {
	unconfigured bool
	preview      services.WikiCompilePreview
	reject       *services.WikiStartError
	jobs         []services.WikiCompileJob
	jobSeq       int
	touch        map[string]int64 // 任务 ID -> 最近更新的内部时钟
	scripts      []WikiRunScript
	activeID     string
	cancelRun    context.CancelFunc

	starts   [][]services.WikiCompileCandidate
	controls []WikiControlCall
	runs     []string
}

// WikiControlCall 是一次 ControlCompile 调用记录。
type WikiControlCall struct {
	JobID  string
	Status string
}

// SetCompileConfigured 设定 Wiki 编译用的模型是否已配置（默认已配置）。
func (f *WikiFake) SetCompileConfigured(ok bool) {
	f.with(func() { f.compile.unconfigured = !ok })
}

// SetPreview 设定下一次 CompilePreview 返回的预览（调用方负责 Counts 与 Entries 自洽）。
func (f *WikiFake) SetPreview(p services.WikiCompilePreview) {
	f.with(func() { f.compile.preview = p })
}

// RejectStart 让 StartCompile 一律被拒并带上给定预览（nil 取消）；对应处理器里「选择已过期」的拒绝。
func (f *WikiFake) RejectStart(e *services.WikiStartError) {
	f.with(func() { f.compile.reject = e })
}

// PutJob 摆一条任务记录（ID 为空时自动编号），返回落库的记录。
func (f *WikiFake) PutJob(j services.WikiCompileJob) services.WikiCompileJob {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.compile.touch == nil {
		f.compile.touch = map[string]int64{}
	}
	if j.ID == "" {
		f.compile.jobSeq++
		j.ID = "job-" + strconv.Itoa(f.compile.jobSeq)
	}
	if j.CreatedAt == 0 {
		j.CreatedAt = f.tick()
	}
	f.compile.touch[j.ID] = j.CreatedAt
	f.compile.jobs = append(f.compile.jobs, j)
	return j
}

// QueueRun 给之后的 RunCompile 排一个演出脚本。
func (f *WikiFake) QueueRun(s WikiRunScript) {
	f.with(func() { f.compile.scripts = append(f.compile.scripts, s) })
}

// Job 读出一条任务当前的记录；没有返回 false。
func (f *WikiFake) Job(id string) (j services.WikiCompileJob, ok bool) {
	f.with(func() {
		if i := f.jobIndex(id); i >= 0 {
			j, ok = f.compile.jobs[i], true
		}
	})
	return j, ok
}

// Starts 是收到的开始编译请求里选中的条目，按发生顺序。
func (f *WikiFake) Starts() (out [][]services.WikiCompileCandidate) {
	f.with(func() { out = slices.Clone(f.compile.starts) })
	return out
}

// Controls 是收到的任务控制请求，按发生顺序。
func (f *WikiFake) Controls() (out []WikiControlCall) {
	f.with(func() { out = slices.Clone(f.compile.controls) })
	return out
}

// Runs 是被跑过的任务 ID，按发生顺序。
func (f *WikiFake) Runs() (out []string) {
	f.with(func() { out = slices.Clone(f.compile.runs) })
	return out
}

func (f *WikiFake) jobIndex(id string) int {
	return slices.IndexFunc(f.compile.jobs, func(j services.WikiCompileJob) bool { return j.ID == id })
}

func (f *WikiFake) setJobLocked(j services.WikiCompileJob) services.WikiCompileJob {
	if f.compile.touch == nil {
		f.compile.touch = map[string]int64{}
	}
	f.compile.touch[j.ID] = f.tick()
	if i := f.jobIndex(j.ID); i >= 0 {
		f.compile.jobs[i] = j
	} else {
		f.compile.jobs = append(f.compile.jobs, j)
	}
	return j
}

// CompileConfigured 报告编译用的模型是否可用。
func (f *WikiFake) CompileConfigured(_ context.Context) (bool, error) {
	f.gate(WikiOpConfigured)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(WikiOpConfigured); err != nil {
		return false, err
	}
	return !f.compile.unconfigured, nil
}

// CompilePreview 返回摆好的预览（拷贝）。
func (f *WikiFake) CompilePreview(_ context.Context) (services.WikiCompilePreview, error) {
	f.gate(WikiOpPreview)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(WikiOpPreview); err != nil {
		return services.WikiCompilePreview{}, err
	}
	p := f.compile.preview
	p.Entries = slices.Clone(p.Entries)
	if p.Entries == nil {
		p.Entries = []services.WikiCompileCandidate{}
	}
	return p, nil
}

// CompileJobs 返回全部任务，最近更新在前。
func (f *WikiFake) CompileJobs(_ context.Context) ([]services.WikiCompileJob, error) {
	f.gate(WikiOpJobs)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(WikiOpJobs); err != nil {
		return nil, err
	}
	out := slices.Clone(f.compile.jobs)
	slices.SortStableFunc(out, func(a, b services.WikiCompileJob) int {
		switch {
		case f.compile.touch[a.ID] > f.compile.touch[b.ID]:
			return -1
		case f.compile.touch[a.ID] < f.compile.touch[b.ID]:
			return 1
		}
		return 0
	})
	if out == nil {
		out = []services.WikiCompileJob{}
	}
	return out, nil
}

// StartCompile 创建一条 running 任务：没选条目、有任务在跑、被 RejectStart 拦下时返回 *WikiStartError。
func (f *WikiFake) StartCompile(_ context.Context, selected []services.WikiCompileCandidate) (services.WikiCompileJob, error) {
	f.gate(WikiOpStart)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.compile.starts = append(f.compile.starts, slices.Clone(selected))
	if err := f.failure(WikiOpStart); err != nil {
		return services.WikiCompileJob{}, err
	}
	if f.compile.unconfigured {
		return services.WikiCompileJob{}, services.ErrAINotConfigured
	}
	if f.compile.reject != nil {
		e := *f.compile.reject
		return services.WikiCompileJob{}, &e
	}
	if len(selected) == 0 {
		return services.WikiCompileJob{}, &services.WikiStartError{Message: "select at least one item"}
	}
	for _, j := range f.compile.jobs {
		if j.Status == services.WikiJobRunning {
			return services.WikiCompileJob{}, &services.WikiStartError{Message: "a compile job is already running"}
		}
	}
	job := services.WikiCompileJob{Status: services.WikiJobRunning, Model: "fake-model"}
	for _, c := range selected {
		job.Total += c.Blocks
		job.Completed += c.Reusable
	}
	f.compile.jobSeq++
	job.ID = "job-" + strconv.Itoa(f.compile.jobSeq)
	job.CreatedAt = f.tick()
	return f.setJobLocked(job), nil
}

// RunCompile 按脚本演一次编译（阻塞）。ctx 取消视为取消任务：任务落为 cancelled，返回 nil 错误（与真实驱动一致）。
func (f *WikiFake) RunCompile(ctx context.Context, job services.WikiCompileJob, progress func(services.WikiCompileJob)) (services.WikiCompileJob, error) {
	f.gate(WikiOpRun)
	f.mu.Lock()
	f.compile.runs = append(f.compile.runs, job.ID)
	if err := f.failure(WikiOpRun); err != nil {
		f.mu.Unlock()
		return job, err
	}
	if f.compile.unconfigured {
		f.mu.Unlock()
		return job, services.ErrAINotConfigured
	}
	if f.compile.activeID != "" {
		f.mu.Unlock()
		return job, errors.New("a compile run is already active")
	}
	var script WikiRunScript
	if len(f.compile.scripts) > 0 {
		script = f.compile.scripts[0]
		f.compile.scripts = f.compile.scripts[1:]
	}
	runCtx, cancel := context.WithCancel(ctx)
	f.compile.activeID, f.compile.cancelRun = job.ID, cancel
	f.mu.Unlock()
	defer func() {
		cancel()
		f.with(func() { f.compile.activeID, f.compile.cancelRun = "", nil })
	}()

	if script.Arrived != nil {
		close(script.Arrived)
	}
	cur := job
	for _, p := range script.Progress {
		p.ID = job.ID
		f.with(func() { cur = f.setJobLocked(p) })
		if progress != nil {
			progress(cur)
		}
	}
	if script.Gate != nil {
		select {
		case <-script.Gate:
		case <-runCtx.Done():
		}
	}
	var out services.WikiCompileJob
	var err error
	f.with(func() {
		latest := cur
		if i := f.jobIndex(job.ID); i >= 0 {
			latest = f.compile.jobs[i]
		}
		switch {
		case runCtx.Err() != nil:
			// 取消：调用方可能已经把任务标成 cancelled，这里兜底落一次。
			latest.Status = services.WikiJobCancelled
		case script.Err != nil:
			latest.Status = services.WikiJobFailed
			err = script.Err
		default:
			latest.Status = services.WikiJobCompleted
			if script.Final != "" {
				latest.Status = script.Final
			}
			latest.Completed = latest.Total
		}
		out = f.setJobLocked(latest)
	})
	return out, err
}

// ControlCompile 改任务状态：不存在或已完成报错；改成 cancelled 时同时取消正在跑的那次 RunCompile；
// 改成 running（恢复）时若已有别的任务在跑则报错。
func (f *WikiFake) ControlCompile(_ context.Context, jobID, status string) (services.WikiCompileJob, error) {
	f.gate(WikiOpControl)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.compile.controls = append(f.compile.controls, WikiControlCall{JobID: jobID, Status: status})
	if err := f.failure(WikiOpControl); err != nil {
		return services.WikiCompileJob{}, err
	}
	i := f.jobIndex(jobID)
	if i < 0 {
		return services.WikiCompileJob{}, errors.New("compile job not found")
	}
	job := f.compile.jobs[i]
	if job.Status == services.WikiJobCompleted {
		return services.WikiCompileJob{}, errors.New("compile job already completed")
	}
	switch status {
	case services.WikiJobPaused, services.WikiJobCancelled, services.WikiJobRunning:
	default:
		return services.WikiCompileJob{}, errors.New("unsupported job status")
	}
	if status == services.WikiJobRunning {
		for _, other := range f.compile.jobs {
			if other.ID != jobID && other.Status == services.WikiJobRunning {
				return services.WikiCompileJob{}, errors.New("a compile job is already running")
			}
		}
	}
	job.Status = status
	if status == services.WikiJobCancelled && f.compile.activeID == jobID && f.compile.cancelRun != nil {
		f.compile.cancelRun()
	}
	return f.setJobLocked(job), nil
}

// ---- 更新建议 ----

// Suggestion 读一页的更新建议；没有返回 nil。
func (f *WikiFake) Suggestion(_ context.Context, pageID string) (*services.WikiSuggestion, error) {
	f.gate(WikiOpSuggestion)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(WikiOpSuggestion); err != nil {
		return nil, err
	}
	row := f.pages[pageID]
	if row == nil || row.sugg == nil {
		return nil, nil
	}
	s := *row.sugg
	s.SourceIDs = slices.Clone(s.SourceIDs)
	return &s, nil
}

// AcceptSuggestion 采用建议：令牌不匹配（建议已被换掉或清掉）返回 false 且不改页面；
// 采用时先存历史版本，再覆盖标题、摘要、正文并按建议里的出链目标重建出链，清掉建议；页面保持「手动维护」标记（与处理器一致），
// 后续来源更新继续走建议而不是覆盖。
func (f *WikiFake) AcceptSuggestion(_ context.Context, pageID, token string) (bool, error) {
	f.gate(WikiOpAccept)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accepts = append(f.accepts, WikiAcceptCall{PageID: pageID, Token: token})
	if err := f.failure(WikiOpAccept); err != nil {
		return false, err
	}
	row := f.pages[pageID]
	if row == nil || row.sugg == nil || row.sugg.Token != token {
		return false, nil
	}
	f.snapshotLocked(row)
	d := row.sugg.Draft
	row.page.Title, row.page.Summary, row.page.Body = d.Title, d.Summary, d.Body
	if d.Kind != "" {
		row.page.Kind = d.Kind
	}
	row.page.PromptVersion = services.WikiCompilerVersion
	row.page.UpdatedAt = f.tick()
	now := row.page.UpdatedAt
	row.page.ManualEditedAt = &now
	f.rebuildLinksLocked(pageID, d.LinkTargets)
	row.sugg = nil
	return true, nil
}
