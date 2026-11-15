package aiflow

// 后台任务执行器：取代渲染层的 hooks/useBackgroundJobClient.ts，连同它调用的 stores/wiki.store.ts 的
// runBackgroundWikiCompile 与 stores/semantic.store.ts 的 runIndexing 里与界面无关的部分。
//
// 主进程的持久化调度器（主包的 BackgroundJobRuntime）持有调度权与重试状态，只把需要 AI 配置的两类任务
// （wiki-compile、semantic-index）交出来：渲染层版本由一直挂载的浏览器侧客户端领取、执行、续租并回报，
// 原生版空闲时没有渲染进程，改在本进程里执行。执行器自己不排程——到期领取、失败退避、租约过期后重新领取
// 都留在调度器里；它只做三件事：
//   - SyncSettings：按设置算出两类任务是否启用，告诉调度器；
//   - Handle：收到一个任务就在后台 goroutine 里执行，期间每 30 秒续租，结束后回报完成或失败；
//   - Close：关闭时取消在途任务，不回报，让租约自然过期后由调度器重新领取。
//
// 界面上手动触发的 Wiki 编译与语义索引与后台任务共用同一把「正在执行」的守卫（TS 里是 store 的
// isCompiling / isIndexing）：已有同类工作在跑时，后台这一轮静默跳过并回报完成。守卫的入口是
// RunWikiCompile 与 RunSemanticIndexing；原生界面的手动入口必须走它们，不能绕开直接调用驱动器。

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// 调度器里需要执行器参与的两类任务。
const (
	JobKindWikiCompile   = "wiki-compile"
	JobKindSemanticIndex = "semantic-index"
)

// DefaultHeartbeatInterval 是续租间隔。调度器的租约是 90 秒，间隔 30 秒留有三次机会。
const DefaultHeartbeatInterval = 30 * time.Second

const (
	wikiCompileLogScope  = "wiki-compile"
	wikiCompileLogAction = "后台 Wiki 编译"
)

// BackgroundJob 是调度器交给执行器的任务，只含执行器用到的字段。
type BackgroundJob struct {
	ID   string
	Kind string
}

// JobScheduler 是主进程调度器对执行器暴露的面，对应 window.api.backgroundJob 的
// syncRenderer / renew / complete / fail。Renew、Complete、Fail 返回 false 表示租约已不属于本执行器
// （过期后被重新领取等），执行器不需要处理。
type JobScheduler interface {
	Sync(ctx context.Context, wikiEnabled, semanticEnabled bool) error
	Renew(ctx context.Context, id string) (bool, error)
	Complete(ctx context.Context, id string) (bool, error)
	// Fail 回报失败，由调度器按退避策略安排重试（不暂停任务）。
	Fail(ctx context.Context, id, message string) (bool, error)
}

// BackgroundSwitches 是决定后台任务是否启用的两项设置。
type BackgroundSwitches struct {
	BackgroundTasksEnabled bool // 后台任务总开关（backgroundTasksEnabled）
	WikiCompileEnabled     bool // 自动编译 Wiki（wikiCompileEnabled）
}

// WikiCompiler 是后台一轮 Wiki 编译的执行方，*WikiDriver 满足它。
type WikiCompiler interface {
	CompileReady(ctx context.Context, onProgress func(title string, current, total int)) (WikiCompileRoundResult, error)
}

// SemanticIndexer 是语义索引一轮的执行方，*Runner 满足它。
type SemanticIndexer interface {
	RunSemanticIndexing(ctx context.Context, store SemanticIndexStore, onProgress func(indexed int)) (SemanticIndexRunResult, error)
}

// SemanticNoticeKind 对应 SemanticRunNotice.kind。
type SemanticNoticeKind string

const (
	NoticeDone          SemanticNoticeKind = "done"
	NoticePartial       SemanticNoticeKind = "partial"
	NoticeFailed        SemanticNoticeKind = "failed"
	NoticeNothing       SemanticNoticeKind = "nothing"
	NoticeNotConfigured SemanticNoticeKind = "not-configured"
)

// SemanticRunNotice 对应 SemanticRunNotice：一轮索引的结果，手动触发时供界面一次性提示。
type SemanticRunNotice struct {
	Kind    SemanticNoticeKind
	Indexed int
	Failed  int
	// Message 是失败原因，空串表示没有。
	Message string
}

// BackgroundClient 执行调度器交来的后台任务。字段在首次使用前填好，之后不要再改；
// 零值的 HeartbeatInterval 取 DefaultHeartbeatInterval。
type BackgroundClient struct {
	Scheduler JobScheduler
	// Switches 读取当前设置里的两个开关；为 nil 视为都关闭。
	Switches func() BackgroundSwitches
	// Config 用来判断 embedding 是否已配置（语义索引启用的前提）。
	Config   ConfigSource
	Wiki     WikiCompiler
	Semantic SemanticIndexer
	Store    SemanticIndexStore
	// LogError 对应 window.api.log.appError：后台任务的失败要留痕，否则「为什么这条一直没进 Wiki」无从查起。
	LogError func(scope, action, message string)
	Log      Logger // 诊断日志，可为 nil
	// HeartbeatInterval 是续租间隔。
	HeartbeatInterval time.Duration

	// 以下两个字段供测试注入：假的续租计时器；每个任务 goroutine 退出前（续租已停止、回报已结束）的回调。
	newTicker func(d time.Duration) heartbeatTicker
	jobDone   func()

	initOnce sync.Once
	ctx      context.Context
	cancel   context.CancelFunc

	mu        sync.Mutex // 保护下面三项
	closed    bool
	compiling bool
	indexing  bool

	jobs sync.WaitGroup
}

type heartbeatTicker interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }

func (c *BackgroundClient) init() {
	c.initOnce.Do(func() { c.ctx, c.cancel = context.WithCancel(context.Background()) })
}

func (c *BackgroundClient) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log.Warnf(format, args...)
	}
}

func (c *BackgroundClient) logError(scope, action, message string) {
	if c.LogError != nil {
		c.LogError(scope, action, message)
	}
}

// SyncSettings 把两类任务是否启用同步给调度器：总开关开着，且 Wiki 编译已开启（或 embedding 已配置）。
// 总开关、Wiki 开关、embedding 路由与模型变化后都要调用一次；调度器侧是幂等的。
func (c *BackgroundClient) SyncSettings(ctx context.Context) error {
	var switches BackgroundSwitches
	if c.Switches != nil {
		switches = c.Switches()
	}
	semanticConfigured := false
	if c.Config != nil {
		_, semanticConfigured = c.Config.AISettings().ResolveEmbeddingConfig()
	}
	return c.Scheduler.Sync(ctx,
		switches.BackgroundTasksEnabled && switches.WikiCompileEnabled,
		switches.BackgroundTasksEnabled && semanticConfigured)
}

// Handle 接手调度器交来的任务，立即返回，任务在后台 goroutine 里执行。
// 不是这两类任务的、以及关闭之后收到的都直接忽略。
//
// 调度器在持有自己的锁时调用 Handle，而完成与失败的回报又要回到调度器拿同一把锁，
// 所以这里绝不能同步执行，更不能同步回报。
func (c *BackgroundClient) Handle(job BackgroundJob) {
	if job.Kind != JobKindWikiCompile && job.Kind != JobKindSemanticIndex {
		return
	}
	c.init()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.jobs.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.jobs.Done()
		c.runJob(job)
	}()
}

// Close 停止接收新任务，取消在途任务并等它们退出。被打断的任务不回报：回报失败会让调度器把它推进
// 退避，回报完成会让一轮没跑完的任务看上去已经完成；不回报的话租约在 90 秒内过期，之后调度器会重新领取。
// 已经发出的回报（完成、失败、续租）不受取消影响，会跑完。
func (c *BackgroundClient) Close() {
	c.init()
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	c.jobs.Wait()
}

// runJob 对应 handleAvailable 的 executeRendererJob(...).then(complete).catch(fail).finally(clearInterval)：
// 完成调用本身出错时同样改走失败回报，续租直到回报结束才停止。
func (c *BackgroundClient) runJob(job BackgroundJob) {
	if c.jobDone != nil {
		defer c.jobDone() // 最先注册、最后执行：此时续租已停止
	}
	// 调度器的调用不随关闭取消：IPC 一旦发出就要完成，与 TS 里 IPC 不接收 signal 一致。
	report := context.WithoutCancel(c.ctx)
	stopHeartbeat := c.startHeartbeat(report, job.ID)
	defer stopHeartbeat()

	err := c.execute(job)
	if c.ctx.Err() != nil {
		// 关闭打断了这一轮。下游遇到取消可能报错，也可能提前、无错地返回部分结果（Runner 的语义索引就是后者），
		// 两种情况都不能当作完成或失败回报：不回报，租约过期后由调度器重新领取。
		return
	}
	if err == nil {
		if _, err = c.Scheduler.Complete(report, job.ID); err == nil {
			return
		}
	}
	if _, failErr := c.Scheduler.Fail(report, job.ID, err.Error()); failErr != nil {
		c.logf("[background] 回报任务失败时出错（%s）：%v", job.ID, failErr)
	}
}

// execute 对应 executeRendererJob；panic 转成错误，免得下游的一个 bug 带走整个进程。
func (c *BackgroundClient) execute(job BackgroundJob) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	if job.Kind == JobKindWikiCompile {
		return c.RunWikiCompile(c.ctx)
	}
	notice := c.RunSemanticIndexing(c.ctx, nil)
	if notice != nil && (notice.Kind == NoticeFailed || notice.Kind == NoticePartial) {
		message := notice.Message
		if message == "" {
			message = fmt.Sprintf("语义索引失败 %d 项", notice.Failed)
		}
		return errors.New(message)
	}
	return nil
}

// startHeartbeat 对应 setInterval(renew, 30_000)：返回的 stop 在最后一次回报之后调用，会等在途的那次续租
// 返回，所以 stop 返回之后不会再有续租发出（Close 返回时也就没有遗留的 goroutine）。
// 续租失败只留诊断日志，下一个间隔再试。
func (c *BackgroundClient) startHeartbeat(ctx context.Context, id string) (stop func()) {
	interval := c.HeartbeatInterval
	if interval <= 0 {
		interval = DefaultHeartbeatInterval
	}
	var ticker heartbeatTicker
	if c.newTicker != nil {
		ticker = c.newTicker(interval)
	} else {
		ticker = realTicker{time.NewTicker(interval)}
	}

	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-done:
				return
			case <-ticker.C():
				if _, err := c.Scheduler.Renew(ctx, id); err != nil {
					c.logf("[background] 任务续租失败（%s）：%v", id, err)
				}
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
		<-finished
	}
}

// RunWikiCompile 对应 runBackgroundWikiCompile({ rethrow: true })：执行后台一轮 Wiki 编译。
// 已有编译在跑（含界面上手动触发的）时静默跳过，视为成功；逐条失败与整体错误都留痕，整体错误返回给调用方。
func (c *BackgroundClient) RunWikiCompile(ctx context.Context) error {
	c.mu.Lock()
	if c.compiling {
		c.mu.Unlock()
		return nil
	}
	c.compiling = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.compiling = false
		c.mu.Unlock()
	}()

	round, err := c.Wiki.CompileReady(ctx, nil)
	if err != nil {
		// 被取消（关闭执行器）不算编译失败，不写进应用日志；TS 里没有取消这回事
		if ctx.Err() == nil {
			c.logError(wikiCompileLogScope, wikiCompileLogAction, err.Error())
		}
		return err
	}
	// 后台轮次不弹提示，但逐条失败要留痕
	for _, failure := range round.Failures {
		c.logError(wikiCompileLogScope, wikiCompileLogAction, failure.Title+"："+failure.Reason)
	}
	return nil
}

// RunSemanticIndexing 对应 semantic.store 的 runIndexing 里与界面无关的部分：执行一轮语义索引并归类结果。
// 已有索引在跑（含界面上手动触发的）时返回 nil（没有回执）；执行出错归类为 failed，原因放进 Message。
// onProgress 收到本轮已完成的条目数，可为 nil。
func (c *BackgroundClient) RunSemanticIndexing(ctx context.Context, onProgress func(indexed int)) *SemanticRunNotice {
	c.mu.Lock()
	if c.indexing {
		c.mu.Unlock()
		return nil
	}
	c.indexing = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.indexing = false
		c.mu.Unlock()
	}()

	result, err := c.Semantic.RunSemanticIndexing(ctx, c.Store, onProgress)
	if err != nil {
		if ctx.Err() == nil { // 被取消（关闭执行器）不是索引出了问题
			c.logf("[background] 语义索引执行失败：%v", err)
		}
		return &SemanticRunNotice{Kind: NoticeFailed, Message: err.Error()}
	}
	switch {
	case result.Skipped:
		return &SemanticRunNotice{Kind: NoticeNotConfigured}
	case result.Failed > 0 && result.Indexed == 0:
		return &SemanticRunNotice{Kind: NoticeFailed, Failed: result.Failed, Message: result.LastError}
	case result.Failed > 0:
		return &SemanticRunNotice{Kind: NoticePartial, Indexed: result.Indexed, Failed: result.Failed, Message: result.LastError}
	case result.Indexed > 0:
		return &SemanticRunNotice{Kind: NoticeDone, Indexed: result.Indexed}
	default:
		return &SemanticRunNotice{Kind: NoticeNothing}
	}
}
