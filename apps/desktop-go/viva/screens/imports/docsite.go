package imports

import (
	"context"
	"strings"
	"sync"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 采集文档站整页的状态（渲染层 DocumentSitePanel）：表单、批次列表、选中批次的页面明细与操作。
//
// 流程只有一条：填入口与范围 → 创建批次 → 批次在主进程里发现并逐页入库 → 界面每 2 秒轮询批次与选中批次的明细，
// 批次上可以暂停 / 继续 / 重试失败项 / 取消。没有「先扫描、再勾选、再批量入队」这一步（渲染层没有）。
//
// 方法都是同步的、可从任意 goroutine 调用；界面线程只读 DocSiteView。变化后调用 invalidate 请求重绘。

// docSitePollInterval 是轮询间隔（渲染层 setInterval 2000）。
const docSitePollInterval = 2 * time.Second

// CollectionSource 是目标合集下拉的数据来源（services.Library 满足）。
type CollectionSource interface {
	Collections(ctx context.Context) ([]services.LibraryCollection, error)
}

// DocSiteForm 是表单的输入。数值照实保存，范围由主进程校验（见 DocSiteMinPages 等常量的说明）。
type DocSiteForm struct {
	URL          string // 入口网址
	Directory    string // 目录范围；空表示入口所在目录
	MaxPages     int
	MaxDepth     int
	CollectionID string // 空表示未归合集
	Policy       string // services.CrawlDuplicateSkip 或 CrawlDuplicateUpdate
}

// DocSiteView 是界面每帧读取的快照：切片都是写时复制的，拿到之后不会被后台改动。
type DocSiteView struct {
	Form DocSiteForm
	// Scope 是「实际范围」（origin + 目录）；入口或目录无效时为空，开始按钮据此禁用。
	Scope       string
	Jobs        []services.CrawlJob
	Collections []services.LibraryCollection
	// Selected 是展开了页面明细的批次 id；Pages 是它的页面（刚选中、还没读到时为空）。
	Selected string
	Pages    []services.CrawlPage
	// PagesLoaded 报告 Pages 是否已是选中批次读回来的结果：刚选中、还没读到之前为假，界面据此区分「还在读」与「这个批次还没有页面」。
	PagesLoaded bool
	// HasLoaded 为假且 LoadError 非空时界面整页显示加载失败，而不是「没有批次」。
	HasLoaded bool
	LoadError string
	// Busy 表示有一个创建或批次操作正在进行：开始与重试按钮据此禁用，其余操作不再并发发出。
	Busy bool
}

// CanStart 报告「开始采集」按钮是否可点。
func (v DocSiteView) CanStart() bool { return !v.Busy && v.Scope != "" }

// DocSiteStore 是采集文档站整页的状态。
type DocSiteStore struct {
	svc   services.DocSite
	cols  CollectionSource
	guard kit.Guard

	invalidate func()

	// After 注册一个延时回调并返回取消函数；默认是真实定时器，测试注入可手动触发的假实现。
	After func(d time.Duration, fn func()) (stop func())
	// Spawn 在后台执行一个动作；默认开 goroutine，测试注入同步执行以保持确定性。
	Spawn func(fn func())

	mu   sync.Mutex
	view DocSiteView

	// 读取代次：每次读取开始、切换选中批次、离开页面都加一，响应回来时代次对不上就丢弃，旧响应不能盖掉新状态。
	loadRev int

	attached  bool
	attachGen int
	ctx       context.Context // 页面挂载期间的上下文：离开页面时取消在途的读取
	cancel    context.CancelFunc
	stopTimer func()
}

// NewDocSiteStore 创建文档站状态。svc 不能为空；cols 为空时目标合集只有「未归合集」。
func NewDocSiteStore(svc services.DocSite, cols CollectionSource, guard kit.Guard, invalidate func()) *DocSiteStore {
	s := &DocSiteStore{
		svc:        svc,
		cols:       cols,
		guard:      guard,
		invalidate: invalidate,
		After: func(d time.Duration, fn func()) func() {
			t := time.AfterFunc(d, fn)
			return func() { t.Stop() }
		},
		Spawn: func(fn func()) { go fn() },
	}
	s.view.Form = DocSiteForm{MaxPages: DocSiteDefaultPages, MaxDepth: DocSiteDefaultDepth, Policy: services.CrawlDuplicateSkip}
	return s
}

// View 返回当前快照。
func (s *DocSiteStore) View() DocSiteView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view
}

func (s *DocSiteStore) wake() {
	if s.invalidate != nil {
		s.invalidate()
	}
}

// ---- 表单 ----

// 以下设置器改一个字段并重算实际范围；都立即生效。

func (s *DocSiteStore) SetURL(v string)       { s.edit(func(f *DocSiteForm) { f.URL = v }) }
func (s *DocSiteStore) SetDirectory(v string) { s.edit(func(f *DocSiteForm) { f.Directory = v }) }
func (s *DocSiteStore) SetMaxPages(n int)     { s.edit(func(f *DocSiteForm) { f.MaxPages = n }) }
func (s *DocSiteStore) SetMaxDepth(n int)     { s.edit(func(f *DocSiteForm) { f.MaxDepth = n }) }

// SetCollection 选目标合集；空串是「未归合集」。
func (s *DocSiteStore) SetCollection(id string) { s.edit(func(f *DocSiteForm) { f.CollectionID = id }) }

// SetPolicy 选重复处理；认不出的取值按「跳过重复」（与 crawl:create 对 duplicatePolicy 的处理一致）。
func (s *DocSiteStore) SetPolicy(policy string) {
	if policy != services.CrawlDuplicateUpdate {
		policy = services.CrawlDuplicateSkip
	}
	s.edit(func(f *DocSiteForm) { f.Policy = policy })
}

func (s *DocSiteStore) edit(change func(*DocSiteForm)) {
	s.mu.Lock()
	change(&s.view.Form)
	s.view.Scope = scopeOf(s.view.Form)
	s.mu.Unlock()
	s.wake()
}

// scopeOf 求表单当前的实际范围；目录两端的空白不算（粘贴进来的路径常带空格，带着空格的目录没有合法用途）。
func scopeOf(f DocSiteForm) string {
	scope, err := DocSiteScope(f.URL, strings.TrimSpace(f.Directory))
	if err != nil {
		return ""
	}
	return scope
}

// buildCrawlInput 把表单换成 crawl:create 的输入（渲染层 webCapture.create 的参数）：用途固定 documents、入口模式固定 directory。
func buildCrawlInput(f DocSiteForm) services.CrawlInput {
	in := services.CrawlInput{
		Purpose:         "documents",
		Seeds:           []services.CrawlSeed{{URL: strings.TrimSpace(f.URL), Mode: services.CrawlSeedDirectory, Directory: strings.TrimSpace(f.Directory)}},
		MaxPages:        f.MaxPages,
		MaxDepth:        f.MaxDepth,
		DuplicatePolicy: services.CrawlDuplicateSkip,
	}
	if f.Policy == services.CrawlDuplicateUpdate {
		in.DuplicatePolicy = services.CrawlDuplicateUpdate
	}
	if f.CollectionID != "" {
		id := f.CollectionID
		in.CollectionID = &id
	}
	return in
}

// ---- 挂载、读取与轮询 ----

// Attach 在页面显示时调用：读目标合集、立刻读一次批次，之后每 2 秒轮询；重复调用无效。ctx 是应用生命周期。
func (s *DocSiteStore) Attach(ctx context.Context) {
	s.mu.Lock()
	if s.attached {
		s.mu.Unlock()
		return
	}
	s.attached = true
	s.attachGen++
	gen := s.attachGen
	s.ctx, s.cancel = context.WithCancel(ctx)
	live := s.ctx
	s.mu.Unlock()
	s.Spawn(func() { s.LoadCollections(live) })
	s.Spawn(func() { s.poll(gen) })
}

// Detach 在页面离开时调用：停掉轮询、取消在途的读取、丢弃它们的结果。快照保留，下次进来先显示旧的再刷新。
func (s *DocSiteStore) Detach() {
	s.mu.Lock()
	s.attached = false
	s.attachGen++
	s.loadRev++
	cancel, stop := s.cancel, s.stopTimer
	s.cancel, s.stopTimer = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if stop != nil {
		stop()
	}
}

// poll 读一次再排下一次：读完才排，所以慢请求不会叠起来。
func (s *DocSiteStore) poll(gen int) {
	s.mu.Lock()
	ctx, current := s.ctx, s.attached && s.attachGen == gen
	s.mu.Unlock()
	if !current {
		return
	}
	s.Load(ctx)
	stop := s.After(docSitePollInterval, func() { s.Spawn(func() { s.poll(gen) }) })
	s.mu.Lock()
	if !s.attached || s.attachGen != gen {
		s.mu.Unlock()
		stop()
		return
	}
	s.stopTimer = stop
	s.mu.Unlock()
}

// Load 读一次批次列表与选中批次的明细（渲染层 load）。失败只设 LoadError、保留旧快照；
// 后台读取的失败只在刚出错时记一次日志（每 2 秒一次的轮询不能把日志刷满）。
func (s *DocSiteStore) Load(ctx context.Context) {
	s.mu.Lock()
	s.loadRev++
	rev, selected := s.loadRev, s.view.Selected
	s.mu.Unlock()

	jobs, err := s.svc.ListJobs(ctx)
	var detail services.CrawlJobDetail
	if err == nil && selected != "" {
		detail, err = s.svc.GetJob(ctx, selected)
	}

	s.mu.Lock()
	if rev != s.loadRev {
		s.mu.Unlock()
		return
	}
	first := false
	if err != nil {
		first = s.view.LoadError == ""
		s.view.LoadError = err.Error()
		if s.view.LoadError == "" {
			s.view.LoadError = locales.WebCaptureLoadJobsError()
		}
	} else {
		s.view.Jobs, s.view.HasLoaded, s.view.LoadError = jobs, true, ""
		if selected != "" {
			s.view.Pages, s.view.PagesLoaded = detail.Pages, true
		}
	}
	s.mu.Unlock()
	if first {
		s.guard.Background("webCapture.load", err)
	}
	s.wake()
}

// LoadCollections 读目标合集（渲染层 fetchCollections）。失败只记日志、下拉里只剩「未归合集」；
// 表单选着的合集已经不在了（被删）就退回「未归合集」，免得带着不存在的合集去创建。
func (s *DocSiteStore) LoadCollections(ctx context.Context) {
	if s.cols == nil {
		return
	}
	cols, err := s.cols.Collections(ctx)
	if err != nil {
		s.guard.Background("webCapture.collections", err)
		return
	}
	s.mu.Lock()
	s.view.Collections = cols
	if id := s.view.Form.CollectionID; id != "" {
		found := false
		for _, c := range cols {
			if c.ID == id {
				found = true
				break
			}
		}
		if !found {
			s.view.Form.CollectionID = ""
		}
	}
	s.mu.Unlock()
	s.wake()
}

// Select 展开一个批次的页面明细并立刻读取；先清掉上一个批次的页面，免得短暂地把别的批次的页面显示在它下面。
func (s *DocSiteStore) Select(ctx context.Context, id string) {
	s.mu.Lock()
	if s.view.Selected == id {
		s.mu.Unlock()
		return
	}
	s.view.Selected, s.view.Pages, s.view.PagesLoaded = id, nil, false
	s.loadRev++
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.Load(ctx) })
}

// ---- 操作 ----

// Start 创建批次（渲染层「开始采集」）：范围无效或有操作在进行时不动；失败弹带原因的提示、表单保持原样便于改了重试；
// 成功后读一次批次列表。返回是否创建成功。
func (s *DocSiteStore) Start(ctx context.Context) bool {
	s.mu.Lock()
	if s.view.Busy || s.view.Scope == "" {
		s.mu.Unlock()
		return false
	}
	input := buildCrawlInput(s.view.Form)
	s.mu.Unlock()
	return s.mutate(ctx, func() error {
		_, err := s.svc.CreateJob(ctx, input)
		return err
	})
}

// Pause、Resume、Retry、Cancel 是批次上的四个操作；失败弹带原因的提示并返回 false，成功后读一次批次列表。
func (s *DocSiteStore) Pause(ctx context.Context, id string) bool {
	return s.mutate(ctx, func() error { return s.svc.PauseJob(ctx, id) })
}
func (s *DocSiteStore) Resume(ctx context.Context, id string) bool {
	return s.mutate(ctx, func() error { return s.svc.ResumeJob(ctx, id) })
}
func (s *DocSiteStore) Cancel(ctx context.Context, id string) bool {
	return s.mutate(ctx, func() error { return s.svc.CancelJob(ctx, id) })
}

// Retry 只对有失败页的批次有意义（按钮在没有失败页时禁用）；没有失败页的批次不发请求。
func (s *DocSiteStore) Retry(ctx context.Context, id string) bool {
	s.mu.Lock()
	failed := false
	for _, job := range s.view.Jobs {
		if job.ID == id {
			failed = ActionsFor(job).CanRetry
			break
		}
	}
	s.mu.Unlock()
	if !failed {
		return false
	}
	return s.mutate(ctx, func() error { return s.svc.RetryJob(ctx, id) })
}

// mutate 执行一次变更（渲染层 mutate）：进行中不再并发发出第二个；成功后读一次批次列表。
func (s *DocSiteStore) mutate(ctx context.Context, run func() error) bool {
	s.mu.Lock()
	if s.view.Busy {
		s.mu.Unlock()
		return false
	}
	s.view.Busy = true
	s.mu.Unlock()
	s.wake()

	ok := s.guard.Run(locales.WebCaptureOperation(), run)

	s.mu.Lock()
	s.view.Busy = false
	s.mu.Unlock()
	s.wake()
	if ok {
		s.Load(ctx)
	}
	return ok
}
