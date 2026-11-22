package wiki

import (
	"errors"
	"slices"
	"strings"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

const (
	// defaultSelectCount 是打开对话框时默认选中的资料数（确认范围后才调用模型）。
	defaultSelectCount = 10
	// pageSize 是资料列表一页显示的条数，「显示更多」每次加一页。
	pageSize = 50
	// maxJobsShown 是对话框里最多列出的任务数。
	maxJobsShown = 10
)

// CompileView 是全文编译对话框每帧读取的快照（写时复制）。
type CompileView struct {
	Open bool
	// Preview 为 nil 且 Loading 为 true 表示「正在计算编译范围」。
	Preview *services.WikiCompilePreview
	// Selected 是已勾选的资料 ID（只会含可选状态的条目）。
	Selected []string
	Jobs     []services.WikiCompileJob
	Filter   string
	// Limit 是资料列表当前显示的条数上限（筛选变化时回到一页）。
	Limit int
	// Busy 为有任务在跑（本对话框启动或继续的）：开始、继续与勾选都禁用。
	Busy    bool
	Loading bool
	// Error 非空时对话框顶部显示这段原文（多行，可复制）。
	Error string
	// ModelReady 为 Wiki 模型已配置；未配置时开始按钮禁用并显示引导。
	ModelReady bool
}

// Visible 是资料列表可筛选的全部条目：已最新的不列，其余按标题（不区分大小写）筛选。
func (v CompileView) Visible() []services.WikiCompileCandidate {
	if v.Preview == nil {
		return nil
	}
	needle := strings.ToLower(v.Filter)
	var out []services.WikiCompileCandidate
	for _, e := range v.Preview.Entries {
		if e.State != services.WikiCandidateCurrent && strings.Contains(strings.ToLower(e.Title), needle) {
			out = append(out, e)
		}
	}
	return out
}

// Shown 是资料列表实际显示的条目（前 Limit 条）；HasMore 为还有更多可以展开。
func (v CompileView) Shown() []services.WikiCompileCandidate {
	all := v.Visible()
	return all[:min(len(all), v.Limit)]
}

// HasMore 报告资料列表是否还有被折叠的条目。
func (v CompileView) HasMore() bool { return len(v.Visible()) > v.Limit }

// IsSelected 报告某条资料是否已勾选。
func (v CompileView) IsSelected(id string) bool { return slices.Contains(v.Selected, id) }

// Chosen 是已勾选的条目（按预览顺序）。
func (v CompileView) Chosen() []services.WikiCompileCandidate {
	if v.Preview == nil {
		return nil
	}
	var out []services.WikiCompileCandidate
	for _, e := range v.Preview.Entries {
		if v.IsSelected(e.ID) {
			out = append(out, e)
		}
	}
	return out
}

// Estimate 是本批的规模估算：条数、分块数与模型调用次数区间（可复用的块不调模型；含至多一次格式纠错，上限翻倍）。
func (v CompileView) Estimate() (items, blocks, minRequests, maxRequests int) {
	chosen := v.Chosen()
	requests := 0
	for _, e := range chosen {
		blocks += e.Blocks
		requests += e.Blocks - e.Reusable
	}
	return len(chosen), blocks, requests, requests * 2
}

// CanStart 报告「开始所选资料」是否可用。
func (v CompileView) CanStart() bool { return !v.Busy && len(v.Selected) > 0 && v.ModelReady }

// JobsShown 是对话框里列出的任务（最近的若干个）。
func (v CompileView) JobsShown() []services.WikiCompileJob {
	return v.Jobs[:min(len(v.Jobs), maxJobsShown)]
}

// CompileStore 是全文编译对话框的状态（WikiCompileDialog）：预览与勾选、开始、暂停、取消、继续，以及任务列表与进度。
// 它挂在 Store 下：一轮编译结束后刷新目录，并告诉主状态「编译中」的起止。
type CompileStore struct {
	root *Store

	mu   sync.Mutex
	view CompileView
	// gen 让过时的预览响应失效（对话框重开、编译结束后又刷新了一次）。
	gen int
}

func newCompileStore(root *Store) *CompileStore {
	return &CompileStore{root: root, view: CompileView{Limit: pageSize}}
}

// View 返回当前快照。
func (c *CompileStore) View() CompileView {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.view
}

func (c *CompileStore) wake() { c.root.wake() }

// Open 打开对话框并读取预览与任务列表。
func (c *CompileStore) Open() {
	c.mu.Lock()
	c.view.Open, c.view.Error = true, ""
	c.mu.Unlock()
	c.wake()
	c.Refresh()
}

// Close 关闭对话框（后台的编译照常跑完）。
func (c *CompileStore) Close() {
	c.mu.Lock()
	c.view.Open = false
	c.mu.Unlock()
	c.wake()
}

// defaultSelection 是默认勾选：前 10 条可执行或可升级的资料。
func defaultSelection(p *services.WikiCompilePreview) []string {
	var ids []string
	for _, e := range p.Entries {
		if e.Selectable() && len(ids) < defaultSelectCount {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

// selectableIDs 是预览里全部可选的资料。
func selectableIDs(p *services.WikiCompilePreview) []string {
	var ids []string
	for _, e := range p.Entries {
		if e.Selectable() {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

// Refresh 并行读取预览、任务列表与模型配置，并把勾选重置成默认的前 10 条。失败写进 Error（预览保留旧的），
// 成功则清掉之前的错误（用户点「重试」后不应还看着旧的失败原因）。
func (c *CompileStore) Refresh() { c.reload(true) }

// reload 是 Refresh 的实现；clearError 为 false 时成功也保留 Error——一轮编译收尾时要让这轮的失败原因留在对话框里。
func (c *CompileStore) reload(clearError bool) {
	c.mu.Lock()
	c.gen++
	gen := c.gen
	c.view.Loading = true
	c.mu.Unlock()
	c.wake()

	var (
		preview             services.WikiCompilePreview
		jobs                []services.WikiCompileJob
		ready               bool
		errPreview, errJobs error
		errReady            error
		wg                  sync.WaitGroup
	)
	wg.Add(3)
	go func() { defer wg.Done(); preview, errPreview = c.root.svc.CompilePreview(c.root.ctx()) }()
	go func() { defer wg.Done(); jobs, errJobs = c.root.svc.CompileJobs(c.root.ctx()) }()
	go func() { defer wg.Done(); ready, errReady = c.root.svc.CompileConfigured(c.root.ctx()) }()
	wg.Wait()
	err := firstError(errPreview, errJobs, errReady)

	c.mu.Lock()
	if gen != c.gen {
		c.mu.Unlock()
		return
	}
	c.view.Loading = false
	if err != nil {
		c.view.Error = err.Error()
	} else {
		c.view.Preview, c.view.Jobs, c.view.ModelReady = &preview, jobs, ready
		c.view.Selected = defaultSelection(&preview)
		if clearError {
			c.view.Error = ""
		}
	}
	c.mu.Unlock()
	if err != nil {
		c.root.guard.Background("wiki.compileRefresh", err)
	}
	c.wake()
}

// SetFilter 按标题筛选资料（列表回到第一页）。
func (c *CompileStore) SetFilter(text string) {
	c.mu.Lock()
	c.view.Filter, c.view.Limit = text, pageSize
	c.mu.Unlock()
	c.wake()
}

// ShowMore 展开一页更多的资料。
func (c *CompileStore) ShowMore() {
	c.mu.Lock()
	c.view.Limit += pageSize
	c.mu.Unlock()
	c.wake()
}

// Toggle 勾选或取消勾选一条资料；只有可选状态（可执行 / 待升级）的条目能勾，有任务在跑时不改动。
func (c *CompileStore) Toggle(id string, checked bool) {
	c.mu.Lock()
	defer func() { c.mu.Unlock(); c.wake() }()
	if c.view.Busy || c.view.Preview == nil {
		return
	}
	i := slices.IndexFunc(c.view.Preview.Entries, func(e services.WikiCompileCandidate) bool { return e.ID == id })
	if i < 0 || !c.view.Preview.Entries[i].Selectable() {
		return
	}
	selected := slices.Clone(c.view.Selected)
	has := slices.Contains(selected, id)
	switch {
	case checked && !has:
		selected = append(selected, id)
	case !checked && has:
		selected = slices.DeleteFunc(selected, func(s string) bool { return s == id })
	default:
		return
	}
	c.view.Selected = selected
}

// setError 写入对话框顶部的错误原文；记日志（原文含失败原因，不含密钥：适配层已抹掉）。
func (c *CompileStore) setError(action string, err error) {
	msg := err.Error()
	if errors.Is(err, services.ErrAINotConfigured) {
		msg = locales.WikiCompileNoModel()
	}
	c.mu.Lock()
	c.view.Error = msg
	c.mu.Unlock()
	c.root.guard.Background(action, err)
}

// Start 开始所选资料：创建任务成功后在后台跑（经 Store.Spawn），返回 true；没有选择、有任务在跑或被拒绝返回 false。
// 被拒绝（资料或范围在预览之后变了）时用后端带回的新预览替换，勾选改成其中全部可选资料，原因写进 Error。
func (c *CompileStore) Start() bool {
	c.mu.Lock()
	if c.view.Busy || !c.view.ModelReady || len(c.view.Selected) == 0 {
		c.mu.Unlock()
		return false
	}
	chosen := c.view.Chosen()
	c.view.Error = ""
	c.mu.Unlock()
	c.wake()

	job, err := c.root.svc.StartCompile(c.root.ctx(), chosen)
	if err != nil {
		var rejected *services.WikiStartError
		if errors.As(err, &rejected) && rejected.Preview != nil {
			c.mu.Lock()
			c.view.Preview, c.view.Selected = rejected.Preview, selectableIDs(rejected.Preview)
			c.mu.Unlock()
		}
		c.setError("wiki.compileStart", err)
		c.wake()
		return false
	}
	return c.launch(job)
}

// launch 登记运行状态并在后台驱动任务；已有任务在跑返回 false。
func (c *CompileStore) launch(job services.WikiCompileJob) bool {
	c.mu.Lock()
	if c.view.Busy {
		c.mu.Unlock()
		return false
	}
	c.view.Busy = true
	c.view.Jobs = upsertJob(c.view.Jobs, job)
	c.mu.Unlock()
	c.root.SetCompiling(true)
	c.root.spawn(func() { c.run(job) })
	return true
}

// upsertJob 把任务放到列表最前（同 ID 的旧记录去掉），返回新切片。
func upsertJob(jobs []services.WikiCompileJob, job services.WikiCompileJob) []services.WikiCompileJob {
	out := make([]services.WikiCompileJob, 0, len(jobs)+1)
	out = append(out, job)
	for _, j := range jobs {
		if j.ID != job.ID {
			out = append(out, j)
		}
	}
	return out
}

// replaceJob 原地替换同 ID 的任务（位置不变），返回新切片；列表里没有则原样返回。
func replaceJob(jobs []services.WikiCompileJob, job services.WikiCompileJob) []services.WikiCompileJob {
	out := slices.Clone(jobs)
	if i := slices.IndexFunc(out, func(j services.WikiCompileJob) bool { return j.ID == job.ID }); i >= 0 {
		out[i] = job
	}
	return out
}

// run 驱动一个任务到结束（阻塞）：进度推给任务列表；出错时报告并把任务暂停（让用户可以继续）；
// 无论成败都收尾——Busy 复位、通知主状态、重读预览与目录。
func (c *CompileStore) run(job services.WikiCompileJob) {
	final, err := c.root.svc.RunCompile(c.root.ctx(), job, func(next services.WikiCompileJob) {
		c.mu.Lock()
		c.view.Jobs = upsertJob(c.view.Jobs, next)
		c.mu.Unlock()
		c.wake()
	})
	switch {
	case err != nil:
		c.setError("wiki.compileRun", err)
		if _, perr := c.root.svc.ControlCompile(c.root.ctx(), job.ID, services.WikiJobPaused); perr != nil {
			c.root.guard.Background("wiki.compilePause", perr)
		}
	default:
		c.mu.Lock()
		c.view.Jobs = upsertJob(c.view.Jobs, final)
		if final.Error != "" {
			c.view.Error = final.Error
		}
		c.mu.Unlock()
	}
	c.mu.Lock()
	c.view.Busy = false
	c.mu.Unlock()
	c.root.SetCompiling(false)
	c.reload(false)
	c.root.OnContributionsUpdated()
}

// Control 暂停、取消或继续一个任务：running 继续——先把检查点改回运行，再在后台驱动；paused / cancelled 只改状态
// 并原位更新任务记录（正在跑的那次驱动在当前分块结束后停下）。失败写进 Error 并返回 false。
func (c *CompileStore) Control(jobID, status string) bool {
	if status == services.WikiJobRunning {
		c.mu.Lock()
		busy := c.view.Busy
		c.mu.Unlock()
		if busy {
			return false
		}
	}
	job, err := c.root.svc.ControlCompile(c.root.ctx(), jobID, status)
	if err != nil {
		c.setError("wiki.compileControl", err)
		c.wake()
		return false
	}
	if status == services.WikiJobRunning {
		return c.launch(job)
	}
	c.mu.Lock()
	c.view.Jobs = replaceJob(c.view.Jobs, job)
	c.mu.Unlock()
	c.wake()
	return true
}
