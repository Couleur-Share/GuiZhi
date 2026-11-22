package ask

import (
	"context"
	"errors"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// Semantic 是问答模块用到的语义索引能力：处理中心的服务（services.Inbox）同时满足它，
// 侧栏的「语义索引」卡与空态里的「未配置 embedding」提示都靠它（渲染层 useSemanticStore）。
type Semantic interface {
	// SemanticStatus 读取语义索引进度；没有配置 embedding 模型时 Configured 为假、不是错误。
	SemanticStatus(ctx context.Context) (services.InboxSemanticStatus, error)
	// SemanticIndexing 报告此刻是否有一轮语义索引在跑。
	SemanticIndexing() bool
	// RunSemanticIndexing 手动执行一轮语义索引并返回回执；已有一轮在跑时返回 nil。
	RunSemanticIndexing(ctx context.Context) (*services.InboxSemanticNotice, error)
}

// SetupView 是问答的前置条件快照：文本模型是否可用、embedding 与语义索引进度。
type SetupView struct {
	// QAChecked 表示文本模型是否可用已经查过一次（查之前空态不画「去配置模型」与示例问题，免得先闪后消）。
	QAChecked bool
	// QAReady 对应渲染层 isScenarioConfigured("qa")：问答场景已有可用模型。
	QAReady bool
	// SemanticChecked 表示语义索引状态是否查过（空态里「未配置 embedding」提示等它查完再决定画不画）。
	SemanticChecked bool
	// SemanticConfigured 为真表示配置了 embedding 模型：侧栏才出现语义索引卡。
	SemanticConfigured bool
	Status             services.InboxSemanticStatus
	// Indexing 为真表示一轮语义索引正在跑（本界面发起的，或服务层报告的）。
	Indexing bool
	// IndexedThisRun 是本轮已索引条数。服务层只在结束时给总数、不报过程进度，所以运行中恒为 0
	// （渲染层 semantic.store 的 indexedThisRun 同样只在结束后才有值）。
	IndexedThisRun int
}

// Setup 是问答的前置条件状态（渲染层 useSemanticStore 加 isScenarioConfigured("qa")）：
// 文本模型是否就绪、语义索引进度与手动索引。方法同步、可从任意 goroutine 调用；界面线程只读 View。
type Setup struct {
	ask        services.Ask
	semantic   Semantic
	guard      kit.Guard
	invalidate func()

	// Ctx 是发给服务的请求的根上下文；nil 取 context.Background()。
	Ctx context.Context
	// OnOpenSettingsAI 打开设置的 AI 分区（索引回执为「未配置」时引导过去）；可空。
	OnOpenSettingsAI func()

	mu   sync.Mutex
	view SetupView
	// running 是本界面发起的索引是否在跑：与服务层的 SemanticIndexing 合起来给 View.Indexing。
	running bool
}

// NewSetup 创建前置条件状态。semantic 可空（没有接线语义索引的宿主、测试）：此时不出语义索引卡。
func NewSetup(svc services.Ask, semantic Semantic, guard kit.Guard, invalidate func()) *Setup {
	return &Setup{ask: svc, semantic: semantic, guard: guard, invalidate: invalidate}
}

// View 返回当前快照。
func (s *Setup) View() SetupView {
	s.mu.Lock()
	v := s.view
	running := s.running
	s.mu.Unlock()
	v.Indexing = running || (s.semantic != nil && s.semantic.SemanticIndexing())
	return v
}

func (s *Setup) wake() {
	if s.invalidate != nil {
		s.invalidate()
	}
}

func (s *Setup) ctx() context.Context {
	if s.Ctx != nil {
		return s.Ctx
	}
	return context.Background()
}

// Refresh 重读文本模型是否就绪与语义索引进度。进入模块时调用：从设置里配好模型回来，「尚未配置」的提示不会滞后。
// 读取失败不弹提示（它只决定提示条与卡片的显隐），只记后台日志；失败时沿用上一次的值。
func (s *Setup) Refresh() {
	ready, err := s.ask.QAConfigured(s.ctx())
	if err != nil {
		s.guard.Background(locales.AskTitle(), err)
	}
	s.mu.Lock()
	if err == nil {
		s.view.QAChecked, s.view.QAReady = true, ready
	}
	s.mu.Unlock()
	s.refreshSemantic()
	s.wake()
}

// refreshSemantic 重读语义索引进度。没有配置 embedding 时 Configured 为假，不是错误。
func (s *Setup) refreshSemantic() {
	if s.semantic == nil {
		s.mu.Lock()
		s.view.SemanticChecked, s.view.SemanticConfigured = true, false
		s.mu.Unlock()
		return
	}
	status, err := s.semantic.SemanticStatus(s.ctx())
	if err != nil {
		s.guard.Background(locales.AskSemanticStatus(0, 0), err)
		return
	}
	s.mu.Lock()
	s.view.SemanticChecked = true
	s.view.SemanticConfigured = status.Configured
	s.view.Status = status
	s.mu.Unlock()
}

// RunIndexing 手动执行一轮语义索引，结束后按回执弹提示并重读进度（渲染层 runIndexing 与 SemanticIndexCard 的回执 effect）。
// 已有一轮在跑（本界面或服务层）时直接返回。
func (s *Setup) RunIndexing() {
	if s.semantic == nil {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	s.wake()

	notice, err := s.semantic.RunSemanticIndexing(s.ctx())

	s.mu.Lock()
	s.running = false
	if err == nil && notice != nil {
		s.view.IndexedThisRun = notice.Indexed
	}
	s.mu.Unlock()
	switch {
	case err != nil:
		s.reportFailure(err.Error())
	case notice != nil:
		s.report(*notice)
	}
	s.refreshSemantic()
	s.wake()
}

// reportFailure 报告连配置都读不到之类的前置失败。
func (s *Setup) reportFailure(message string) {
	s.guard.Background(locales.AskSemanticRun(0), errors.New(message))
	if n := s.guard.Notify; n != nil {
		n.Error(locales.AskSemanticFailed(message), message)
	}
}

// report 把一轮索引的回执翻成提示（SemanticIndexCard 的回执 effect）：成功、部分失败、失败、未配置（并引导去设置）、没有可索引的内容。
func (s *Setup) report(notice services.InboxSemanticNotice) {
	n := s.guard.Notify
	message := notice.Message
	switch notice.Kind {
	case services.InboxSemanticDone:
		if n != nil {
			n.Success(locales.AskSemanticDone(notice.Indexed))
		}
	case services.InboxSemanticPartial:
		reason := message
		if reason == "" {
			reason = locales.CommonUnknownError()
		}
		s.guard.Background(locales.AskSemanticRun(notice.Indexed), errors.New(reason))
		if n != nil {
			n.Error(locales.AskSemanticPartial(notice.Indexed, notice.Failed, message), message)
		}
	case services.InboxSemanticFailed:
		if message == "" {
			message = locales.CommonUnknownError()
		}
		s.guard.Background(locales.AskSemanticRun(notice.Indexed), errors.New(message))
		if n != nil {
			n.Error(locales.AskSemanticFailed(message), message)
		}
	case services.InboxSemanticNotConfigured:
		if n != nil {
			n.Error(locales.AskNotConfigured(), "")
		}
		if s.OnOpenSettingsAI != nil {
			s.OnOpenSettingsAI()
		}
	default:
		if n != nil {
			n.Info(locales.AskSemanticNothing())
		}
	}
}
