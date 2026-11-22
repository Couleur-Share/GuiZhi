package inbox

import (
	"context"
	"errors"
	"strings"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// classifyBatchSize 是 AI 归类每批的条数（aiflow 的批大小）；顶栏按它估算总批数。
const classifyBatchSize = 20

// failToast 是「用户点了某个按钮、结果失败」的统一出口：标题给用户读，原文折叠在详情里可复制，并且留日志。
// 与 Guard.Report 的区别只是标题可以自定（沿用渲染层各动作专用的失败文案）。
func (s *Store) failToast(action, title string, err error) {
	s.guard.Background(action, err)
	if s.guard.Notify != nil {
		s.guard.Notify.Error(title, err.Error())
	}
}

func (s *Store) notify() *kit.Notifier { return s.guard.Notify }

// ---- 批量整理 ----

// ApplyOrganization 把选中的条目按「归入合集 / 添加标签」整理（渲染层 applyOrganization）。成功后清空选择与标签输入、
// 刷新列表，再弹出「已整理 N 条内容」；失败弹出带原因的错误提示，选择保留，可直接重试。
// 没有选中条目、或上一次整理还没结束时什么都不做。返回是否整理成功。
func (s *Store) ApplyOrganization(ctx context.Context) bool {
	s.mu.Lock()
	if s.view.Organizing || len(s.view.Selection) == 0 {
		s.mu.Unlock()
		return false
	}
	in := services.InboxOrganizeInput{
		ItemIDs:      append([]string(nil), s.view.Selection...),
		CollectionID: s.view.OrganizeCollectionID,
		AddTagNames:  ParseTagNames(s.view.OrganizeTagText),
	}
	s.view.Organizing = true
	s.mu.Unlock()
	s.wake()

	var changed int
	ok := s.guard.Run(locales.InboxOrganizeSelected(), func() error {
		n, err := s.svc.Organize(ctx, in)
		changed = n
		return err
	})
	if ok {
		s.ClearSelection()
		s.Refresh(ctx)
	}
	s.mu.Lock()
	s.view.Organizing = false
	if ok {
		s.view.OrganizeTagText = ""
	}
	s.mu.Unlock()
	s.wake()
	if ok && s.notify() != nil {
		s.notify().Success(locales.InboxOrganized(changed))
	}
	return ok
}

// ---- 单行动作 ----

// MarkReviewed 把条目标为已复核并刷新列表（渲染层 markReviewed）。失败弹出带原因的错误提示（渲染层这里是未处理的
// Promise 拒绝，用户什么都看不到）。返回是否成功。
func (s *Store) MarkReviewed(ctx context.Context, itemIDs []string) bool {
	ok := s.guard.Run(locales.LibraryMarkReviewed(), func() error {
		_, err := s.svc.MarkReviewed(ctx, itemIDs)
		return err
	})
	if ok {
		s.Refresh(ctx)
	}
	return ok
}

// RetryImportTask 重试一个失败的采集任务，之后刷新采集队列与处理中心（渲染层 retryTask(id).then(refresh)：
// 不论重试是否成功都刷新）。任务已不存在时给出可理解的原因。返回是否成功。
func (s *Store) RetryImportTask(ctx context.Context, taskID string) bool {
	ok := s.guard.Run(locales.ImportsActionRetry(), func() error {
		found, err := s.svc.RetryImportTask(ctx, taskID)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("任务已不存在，请刷新列表")
		}
		return nil
	})
	if s.RefreshImports != nil {
		s.RefreshImports(ctx)
	}
	s.Refresh(ctx)
	return ok
}

// DismissImportWarning 对带警告的已完成采集任务点「不再提醒」：保留采集记录，只从处理中心移除。
// 处理期间 AcknowledgingTaskID 指向该任务（按钮显示「处理中…」并禁用）。返回是否成功。
func (s *Store) DismissImportWarning(ctx context.Context, taskID string) bool {
	s.mu.Lock()
	s.view.AcknowledgingTaskID = taskID
	s.mu.Unlock()
	s.wake()
	defer func() {
		s.mu.Lock()
		s.view.AcknowledgingTaskID = ""
		s.mu.Unlock()
		s.wake()
	}()

	changed, err := s.svc.AcknowledgeImportWarning(ctx, taskID)
	if err != nil {
		s.failToast(locales.InboxDismissWarning(), locales.InboxActionFailed(err.Error()), err)
		return false
	}
	s.Refresh(ctx)
	if s.notify() != nil {
		if changed > 0 {
			s.notify().Success(locales.InboxWarningAcknowledged())
		} else {
			s.notify().Info(locales.InboxWarningAlreadyHandled())
		}
	}
	return true
}

// OpenItem 在知识库里打开条目（渲染层 openItem）。
func (s *Store) OpenItem(ctx context.Context, itemID string) {
	if s.OnOpenItem != nil && itemID != "" {
		s.OnOpenItem(ctx, itemID)
	}
}

// ---- 聚合行 ----

// RunWiki 是 Wiki 聚合行的「立即执行」：打开 Wiki 编译预览（真正的编译在预览里确认范围后开始）。
func (s *Store) RunWiki() {
	if s.OnWikiPreview != nil {
		s.OnWikiPreview()
	}
}

// RunSemanticNow 是语义索引聚合行的「立即执行」（渲染层 runSemantic）：执行一轮索引，按回执弹出对应提示，再刷新。
// 已有一轮在跑时只提示「语义索引正在执行」；任何聚合行在执行期间，按钮都不可用（Running 非空）。
func (s *Store) RunSemanticNow(ctx context.Context) {
	s.mu.Lock()
	if s.view.Running != RunningNone {
		s.mu.Unlock()
		return
	}
	s.view.Running = RunningSemantic
	s.mu.Unlock()
	s.wake()
	defer func() {
		s.mu.Lock()
		s.view.Running = RunningNone
		s.mu.Unlock()
		s.wake()
	}()

	if s.svc.SemanticIndexing() {
		if s.notify() != nil {
			s.notify().Info(locales.InboxSemanticRunning())
		}
		return
	}
	notice, err := s.svc.RunSemanticIndexing(ctx)
	if err != nil {
		s.failToast(locales.InboxFilterSemanticPending(), locales.InboxSemanticError(err.Error()), err)
		return
	}
	s.reportSemanticNotice(notice)
	s.Refresh(ctx)
}

func (s *Store) reportSemanticNotice(notice *services.InboxSemanticNotice) {
	n := s.notify()
	if notice == nil { // 没有回执：索引没能启动
		if n != nil {
			n.Error(locales.InboxSemanticStartFailed(), "")
		}
		s.guard.Background(locales.InboxFilterSemanticPending(), errors.New("语义索引未能启动"))
		return
	}
	message := notice.Message
	if message == "" {
		message = locales.CommonUnknownError()
	}
	switch notice.Kind {
	case services.InboxSemanticDone:
		if n != nil {
			n.Success(locales.InboxSemanticIndexed(notice.Indexed))
		}
	case services.InboxSemanticPartial:
		s.guard.Background(locales.InboxFilterSemanticPending(), errors.New(message))
		if n != nil {
			n.Warning(locales.InboxSemanticPartial(notice.Indexed, notice.Failed, message), kit.WithDetail(message))
		}
	case services.InboxSemanticFailed:
		s.guard.Background(locales.InboxFilterSemanticPending(), errors.New(message))
		if n != nil {
			n.Error(locales.InboxSemanticFailed(message), message)
		}
	case services.InboxSemanticNotConfigured:
		if n != nil {
			n.Error(locales.InboxSemanticNotConfigured(), "")
		}
		if s.OnOpenSettingsAI != nil {
			s.OnOpenSettingsAI()
		}
	default:
		if n != nil {
			n.Info(locales.InboxSemanticNothing())
		}
	}
}

// ---- AI 智能归类 ----

// ToggleSmartClassify 是「AI 智能归类 / 停止归类」按钮（渲染层 runSmartClassification）：
// 没有在归类时对选中的未归合集条目开始归类；正在归类时再次触发就是取消，取消后不写入任何分类。
// 开始后同步执行到结束（调用方放在后台）。
func (s *Store) ToggleSmartClassify(ctx context.Context) {
	s.mu.Lock()
	if s.classify != nil {
		cancel := s.classify.cancel
		s.mu.Unlock()
		cancel()
		return
	}
	ids := append([]string(nil), s.view.SelectedUnclassified...)
	if len(ids) == 0 {
		s.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	run := &classifyRun{cancel: cancel}
	s.classify = run
	s.view.Classifying = true
	s.view.Classify = ClassifyProgress{Total: (len(ids) + classifyBatchSize - 1) / classifyBatchSize}
	s.mu.Unlock()
	s.wake()

	defer func() {
		cancel()
		s.mu.Lock()
		if s.classify == run {
			s.classify, s.view.Classifying, s.view.Classify = nil, false, ClassifyProgress{}
		}
		s.mu.Unlock()
		s.wake()
	}()

	var existing []string
	if s.CollectionNames != nil {
		existing = s.CollectionNames()
	}
	result, err := s.classifyItems(runCtx, run, ids, existing)
	if err != nil {
		s.reportClassifyFailure(runCtx, err)
		return
	}
	if s.OnClassified != nil {
		s.OnClassified(ctx)
	}
	s.reportClassifyResult(result)
}

// classifyItems 是 inbox.store.ts 的 smartClassify：取摘要 → 分批请求 AI → 应用计划 → 刷新。
//
// 与渲染层的一处有意差异：取消只在「写入之前」生效。AI 返回之后、写库之前检查一次取消——渲染层的写库步骤不看取消信号，
// 用户点了「停止」却照样写入，而提示又说「尚未写入任何分类」；写库开始后则不再被取消打断（一个事务，要么全写要么不写）。
func (s *Store) classifyItems(ctx context.Context, run *classifyRun, itemIDs, existing []string) (services.InboxAIApplyResult, error) {
	sources, err := s.svc.AIClassificationSources(ctx, itemIDs)
	if err != nil {
		return services.InboxAIApplyResult{}, err
	}
	if len(sources) == 0 { // 选中的条目都已变更：不用请求 AI；列表仍要刷新，把过期的行去掉
		s.Refresh(ctx)
		return services.InboxAIApplyResult{Skipped: len(itemIDs), CreatedCollectionNames: []string{}}, nil
	}
	plan, err := s.svc.Classify(ctx, sources, existing, func(completed, total int) {
		s.mu.Lock()
		if s.classify == run {
			s.view.Classify = ClassifyProgress{Completed: completed, Total: total}
		}
		s.mu.Unlock()
		s.wake()
	})
	if err != nil {
		return services.InboxAIApplyResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return services.InboxAIApplyResult{}, err
	}
	result, err := s.svc.ApplyAIClassification(context.WithoutCancel(ctx), plan)
	if err != nil {
		return services.InboxAIApplyResult{}, err
	}
	s.Refresh(ctx)
	return result, nil
}

func (s *Store) reportClassifyFailure(ctx context.Context, err error) {
	n := s.notify()
	switch {
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		if n != nil {
			n.Info(locales.InboxClassifyStopped())
		}
	case errors.Is(err, services.ErrAINotConfigured):
		if n != nil {
			n.Error(locales.InboxAiNotConfigured(), "")
		}
		if s.OnOpenSettingsAI != nil {
			s.OnOpenSettingsAI()
		}
	default:
		s.failToast("inbox.smartClassify", locales.InboxClassifyFailed(err.Error()), err)
	}
}

func (s *Store) reportClassifyResult(result services.InboxAIApplyResult) {
	n := s.notify()
	if n == nil {
		return
	}
	created := result.CreatedCollectionNames
	var summary string
	switch {
	case result.Classified > 0 && len(created) > 0:
		summary = locales.InboxClassifiedWithNew(result.Classified, len(created))
	case result.Classified > 0:
		summary = locales.InboxClassifiedReused(result.Classified)
	default:
		summary = locales.InboxNothingToClassify()
	}
	message := summary
	if result.Skipped > 0 {
		message = locales.InboxClassifySkipped(summary, result.Skipped)
	}
	var opts []kit.ToastOption
	if len(created) > 0 {
		opts = append(opts, kit.WithDetail(strings.Join(created, locales.InboxNameSeparator())))
	}
	kind := kit.ToastInfo
	if result.Classified > 0 {
		kind = kit.ToastSuccess
	}
	n.Show(kind, message, opts...)
}

// Close 在界面卸载时取消进行中的 AI 归类（渲染层卸载时的 abort）。
func (s *Store) Close() {
	s.mu.Lock()
	run := s.classify
	s.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}
