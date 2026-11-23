// Package themed 是主题阅读（AI 阅读页）的状态与纯逻辑层（M3-5b）：知识库详情里的「AI 阅读」模式、生成与状态、页面工具、
// 联网搜索设置，以及处理中心的「主题页任务」面板。
//
// 行为来源：渲染层 components/themed-reading 下的 use-themed-reading.ts、use-themed-reading-mode.ts、ThemedReadingPane / Setup /
// Status / PageTools / Tasks 与 ReadingSearchSettings。这里只含不依赖界面的状态机与判断（Store + 纯函数），界面接线在后续步骤。
//
// 与渲染层的一处结构性差别：渲染层的 hook 随组件挂载，切换文章就是重新挂载；这里的 Store 是长生命周期的，所以每次异步请求
// 都带「选择代次」（selectGen）：切换文章后，旧文章的晚到响应、进度推送与操作收尾一律作废，不会串到新文章上。
package themed

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// previewMinInterval 是静态预览两次拉取之间的最短间隔（渲染层的 500 ms 节流）。
const previewMinInterval = 500 * time.Millisecond

// SetupMode 是生成设置对话框的打开方式。
type SetupMode int

const (
	SetupNone   SetupMode = iota
	SetupNew              // 生成（没有页面、重新生成）
	SetupAdjust           // 调整已有页面（只对当前格式页面）
)

// ReadingView 是一篇内容的阅读状态快照：指针指向的数据都是整体替换、从不就地修改的，拿到之后不会被后台改动。
type ReadingView struct {
	ItemID     string
	SourceKind string
	// InstanceID 是这次阅读实例的标识（旧格式页面的文档按它过滤 iframe 消息）；每次切换内容都会换新。
	InstanceID string
	// ShowPage 为 false 时只读元数据（不校验图片、不生成文档）：页面不显示，只要任务与模型信息。
	ShowPage bool

	// Result 是界面当前显示的读取结果；nil 表示还没读到。生成期间页面被「固定」在生成开始时的版本上
	// （Result.Page / Document 是固定的那一份，其余字段是最新的），新版完成后在 Latest 里等用户切换。
	Result *services.ThemedReadResult
	Latest *services.ThemedReadResult
	// Task 是这份内容最近的任务；进度推送先于读取结果更新它。
	Task *services.ThemedTask
	// LoadError 非空时界面必须渲染成可重试的错误态（再次 Reload），不能当作空态。
	LoadError string
	// Busy 为一次变更操作进行中（同一时间只允许一个）。
	Busy bool

	// Preview 是生成途中的静态预览（已有章节拼出的页面）；PreviewOpen 为已有页面时用户选择查看新稿。
	Preview     *services.ThemedPreview
	PreviewOpen bool

	// Setup 与 DeleteConfirm 是两个对话框的开关。
	Setup         SetupMode
	DeleteConfirm bool

	// Panel 是「阅读页设置」菜单打开的资料面板；Refs 是参考资料面板的数据（打开时读取）。
	Panel ToolsPanel
	Refs  RefsState
}

// Page 是当前显示的页面（可能是固定的旧版）；nil 表示没有页面。
func (v ReadingView) Page() *services.ThemedPage {
	if v.Result == nil {
		return nil
	}
	return v.Result.Page
}

// Active 报告这份内容的任务是否还在排队或执行（此时页面不可调整，也不可再发起新的生成）。
func (v ReadingView) Active() bool { return v.Task != nil && v.Task.Active() }

// Disabled 报告页面级操作（调整、重新生成、恢复、删除、重做图片）是否应被禁用。
func (v ReadingView) Disabled(readOnly bool) bool { return v.Busy || v.Active() || readOnly }

// ShowingPreview 报告当前画面是否是静态预览：有预览，且（还没有页面，或用户选择查看新稿）。
func (v ReadingView) ShowingPreview() bool {
	return v.Preview != nil && (v.Page() == nil || v.PreviewOpen)
}

// ReadingPage 是实际渲染的页面：预览优先，否则是当前页面。
func (v ReadingView) ReadingPage() *services.ThemedPage {
	if v.ShowingPreview() {
		return &v.Preview.Page
	}
	return v.Page()
}

// ReadingStore 是一篇内容的 AI 阅读状态（use-themed-reading.ts）：读取页面与任务、订阅进度、
// 执行生成 / 停止 / 继续 / 重做图片 / 恢复 / 删除 / 导出，并管理生成设置与删除确认两个对话框。
//
// 任务由主进程持有：关闭面板只退订（Detach），绝不隐式取消已付费的生成。方法都是同步的、可从任意 goroutine 调用；
// 界面线程只读 View。异步结果各带代次：同类请求只认最后发出的那一次，切换内容后旧响应一律作废。
type ReadingStore struct {
	svc        services.ThemedReading
	guard      kit.Guard
	invalidate func()

	// Ctx 是发给服务的请求的根上下文；nil 取 context.Background()。
	Ctx context.Context
	// Spawn 在后台执行一个动作（读取、进度后的刷新）；默认开 goroutine，测试注入同步执行以保持确定性。
	Spawn func(fn func())
	// After 在 d 之后后台执行 fn（预览节流）；默认 time.AfterFunc，测试注入手动触发。
	After func(d time.Duration, fn func())
	// Now 取当前时间（预览节流用）；nil 取 time.Now。
	Now func() time.Time
	// NewInstanceID 生成阅读实例标识；nil 取随机十六进制串。
	NewInstanceID func() string
	// PreGenerate 在发起生成前执行（保存未落盘的编辑并确认没有未保存改动）；返回错误则不生成，错误原因进失败提示。可空。
	PreGenerate func(ctx context.Context) error
	// OnPageRemoved 在删除主题页成功后调用（界面据此退回原文）。可空。
	OnPageRemoved func()

	mu     sync.Mutex
	view   ReadingView
	unsub  func()
	pinned *services.ThemedReadResult

	contentKey    string
	autoSetupDone bool
	// selectGen 是选择代次：每次切换内容加一。revision 只认最后一次读取，taskRevision 标记「进度推送比读取更新」。
	selectGen, revision, taskRevision int
	// 预览：previewKey 是已排程的 (任务, 版本号, 是否需要) 组合，避免重复排程；previewGen 让旧排程作废。
	previewKey     string
	previewGen     int
	lastPreviewAt  time.Time
	previewPending bool
	// refsRev 让参考资料的读取只认最后一次。
	refsRev int
}

// NewReadingStore 创建阅读状态。svc 不能为空。
func NewReadingStore(svc services.ThemedReading, guard kit.Guard, invalidate func()) *ReadingStore {
	return &ReadingStore{
		svc: svc, guard: guard, invalidate: invalidate,
		Spawn: func(fn func()) { go fn() },
		After: func(d time.Duration, fn func()) { time.AfterFunc(d, fn) },
	}
}

// View 返回当前快照。
func (s *ReadingStore) View() ReadingView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view
}

func (s *ReadingStore) wake() {
	if s.invalidate != nil {
		s.invalidate()
	}
}

func (s *ReadingStore) ctx() context.Context {
	if s.Ctx != nil {
		return s.Ctx
	}
	return context.Background()
}

func (s *ReadingStore) spawn(fn func()) {
	if s.Spawn != nil {
		s.Spawn(fn)
		return
	}
	go fn()
}

func (s *ReadingStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *ReadingStore) instanceID() string {
	if s.NewInstanceID != nil {
		return s.NewInstanceID()
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *ReadingStore) request() services.ThemedRequest {
	return services.ThemedRequest{ItemID: s.view.ItemID, SourceKind: s.view.SourceKind}
}

// ---- 选择与加载 ----

// Open 选中要阅读的内容。内容不变（同一条目、同一来源）时只在 contentKey 或 showPage 变化后重读，状态保留；
// 换了内容就整体重置（固定的页面、任务、预览、对话框、读取错误都清掉），并作废所有在途请求，然后后台读取。
// contentKey 是标题、来源与正文的摘要：正文改了要重读（过期标记随之变化）。showPage 为 false 时只读元数据。
func (s *ReadingStore) Open(itemID, sourceKind, contentKey string, showPage bool) {
	s.mu.Lock()
	same := s.view.ItemID == itemID && s.view.SourceKind == sourceKind
	reload := !same
	if same {
		reload = s.contentKey != contentKey || s.view.ShowPage != showPage
		s.view.ShowPage = showPage
	} else {
		s.selectGen++
		s.revision++
		s.taskRevision++
		s.previewGen++
		s.pinned = nil
		s.previewKey, s.previewPending = "", false
		s.autoSetupDone = false
		s.view = ReadingView{ItemID: itemID, SourceKind: sourceKind, InstanceID: s.instanceID(), ShowPage: showPage}
	}
	s.contentKey = contentKey
	s.mu.Unlock()
	s.wake()
	if reload {
		s.spawn(s.Reload)
	}
}

// Attach 订阅任务进度（重复调用无副作用）。回调在推送方 goroutine 上执行，只做轻量更新与安排刷新。
func (s *ReadingStore) Attach() {
	s.mu.Lock()
	if s.unsub != nil {
		s.mu.Unlock()
		return
	}
	s.unsub = func() {} // 占位：订阅期间推送可能立刻到达
	s.mu.Unlock()
	cancel := s.svc.OnProgress(s.onProgress)
	s.mu.Lock()
	s.unsub = cancel
	s.mu.Unlock()
}

// Detach 退订进度并作废在途的读取：只是不再关心结果，绝不取消已在执行的任务。
func (s *ReadingStore) Detach() {
	s.mu.Lock()
	cancel := s.unsub
	s.unsub = nil
	s.revision++
	s.previewGen++
	s.previewKey, s.previewPending = "", false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Reload 重读页面、任务与模型信息（读取结果与进度推送谁更新就认谁）。失败设置 LoadError（带重试：再次 Reload）。
func (s *ReadingStore) Reload() {
	s.mu.Lock()
	if s.view.ItemID == "" {
		s.mu.Unlock()
		return
	}
	s.revision++
	req, gen, taskAtReq := s.revision, s.selectGen, s.taskRevision
	in := services.ThemedGetInput{ThemedRequest: s.request(), InstanceID: s.view.InstanceID, MetadataOnly: !s.view.ShowPage}
	s.view.LoadError = ""
	s.mu.Unlock()
	s.wake()

	next, err := s.svc.Get(s.ctx(), in)

	s.mu.Lock()
	if gen != s.selectGen || req != s.revision {
		s.mu.Unlock()
		return // 已被更新的读取或内容切换取代
	}
	if err != nil {
		s.view.LoadError = err.Error()
		s.mu.Unlock()
		s.guard.Background("themedReading.get", err)
		s.wake()
		return
	}
	if s.pinned == nil && next.Page != nil && next.Task != nil && next.Task.Active() {
		cp := next
		s.pinned = &cp
	}
	if s.pinned != nil && s.pinned.Page != nil && next.Page != nil && s.pinned.Page.ID != next.Page.ID {
		latest := next
		merged := next
		merged.Page, merged.Document = s.pinned.Page, s.pinned.Document
		s.view.Latest, s.view.Result = &latest, &merged
	} else {
		res := next
		s.view.Result, s.view.Latest = &res, nil
	}
	if taskAtReq == s.taskRevision {
		s.setTaskLocked(next.Task)
	}
	if s.view.Page() == nil {
		s.view.Panel = PanelNone // 没有页面就没有页面工具
	}
	refresh := s.view.Panel == PanelReferences
	s.mu.Unlock()
	s.wake()
	s.schedulePreview()
	if refresh {
		s.loadRefs()
	}
}

// setTaskLocked 更新最近任务；任务换了（标识不同）就丢掉旧任务的预览与「查看新稿」选择。
func (s *ReadingStore) setTaskLocked(t *services.ThemedTask) {
	oldID, newID := "", ""
	if s.view.Task != nil {
		oldID = s.view.Task.ID
	}
	if t != nil {
		newID = t.ID
	}
	s.view.Task = t
	if oldID != newID {
		s.view.Preview, s.view.PreviewOpen = nil, false
		s.previewGen++
		s.previewKey, s.previewPending = "", false
	}
}

// onProgress 处理一条任务进度推送：只认当前内容；任务开始时把当前页面固定住；结束后重读页面。
func (s *ReadingStore) onProgress(next services.ThemedTask) {
	s.mu.Lock()
	if next.ItemID != s.view.ItemID || next.SourceKind != s.view.SourceKind || s.view.ItemID == "" {
		s.mu.Unlock()
		return
	}
	if next.Active() && s.pinned == nil && s.view.Result != nil && s.view.Result.Page != nil {
		s.pinned = s.view.Result
	}
	s.taskRevision++
	t := next
	s.setTaskLocked(&t)
	s.mu.Unlock()
	s.wake()
	if !next.Active() {
		s.spawn(s.Reload)
	}
	s.schedulePreview()
}

// AdoptLatest 切换到固定期间完成的新版页面（「新版已完成 · 切换」）。
func (s *ReadingStore) AdoptLatest() {
	s.mu.Lock()
	s.pinned = nil
	if s.view.Latest != nil {
		s.view.Result = s.view.Latest
	}
	s.view.Latest, s.view.PreviewOpen = nil, false
	s.mu.Unlock()
	s.wake()
}

// ConsumeAutoSetup 决定首次读到结果后要不要自动打开「生成」对话框：每次选中内容只会返回一次 true，
// 条件是没有原文可退回、没有页面、没有任务、条目不是只读。original 表示界面有可退回的原文视图。
func (s *ReadingStore) ConsumeAutoSetup(original, readOnly bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.view.Result == nil || s.autoSetupDone {
		return false
	}
	s.autoSetupDone = true
	if !original && s.view.Result.Page == nil && s.view.Task == nil && !readOnly {
		s.view.Setup = SetupNew
		return true
	}
	return false
}

// ---- 变更操作 ----

// outcome 是一次变更操作的结果：服务返回的任务（有的话先于重读更新任务状态），以及「用户在对话框里取消」。
type outcome struct {
	task      *services.ThemedTask
	cancelled bool
}

func taskOutcome(t services.ThemedTask, err error) (outcome, error) {
	if err != nil {
		return outcome{}, err
	}
	return outcome{task: &t}, nil
}

// opState 是操作开始那一刻的快照：请求对应的内容、当时显示的页面版本、选择代次。
// 操作可能耗时很久（生图、导出），期间用户可能已经切到别的内容——请求必须发给开始时的那份内容，而不是此刻选中的。
type opState struct {
	gen    int
	req    services.ThemedRequest
	pageID string
}

// run 执行一次变更操作：同一时间只允许一个；成功后更新任务并重读页面，再弹成功提示（用户取消的导出不弹）；
// 失败弹带原因的错误提示并记日志。切换内容后，旧操作的收尾（更新状态、重读、释放忙标记）都作废。
func (s *ReadingStore) run(label, action string, do func(op opState) (outcome, error), success string) bool {
	s.mu.Lock()
	if s.view.Busy || s.view.ItemID == "" {
		s.mu.Unlock()
		return false
	}
	s.view.Busy = true
	op := opState{gen: s.selectGen, req: s.request()}
	if p := s.view.Page(); p != nil {
		op.pageID = p.ID
	}
	s.mu.Unlock()
	s.wake()

	out, err := do(op)

	s.mu.Lock()
	live := op.gen == s.selectGen
	if live && err == nil && out.task != nil {
		s.taskRevision++
		s.setTaskLocked(out.task)
	}
	s.mu.Unlock()
	s.wake()

	ok := err == nil
	if ok {
		if live {
			s.Reload()
		}
		if success != "" && !out.cancelled {
			s.guard.Succeed(success)
		}
	} else {
		s.guard.Fail(locales.ThemedReadingActionFailed(label), action, err)
	}

	s.mu.Lock()
	if op.gen == s.selectGen {
		s.view.Busy = false
	}
	s.mu.Unlock()
	s.wake()
	return ok
}

// pin 在发起生成前把当前页面固定住（有页面才固定）；restore / remove 则相反，清掉固定。内容已切换时不动。
func (s *ReadingStore) pin(op opState, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if op.gen != s.selectGen {
		return
	}
	if on && s.view.Result != nil && s.view.Result.Page != nil {
		s.pinned = s.view.Result
		return
	}
	s.pinned = nil
}

// Generate 发起一次生成（或调整）：先执行 PreGenerate（保存编辑），再建任务排队，立即返回；进度经推送更新。
func (s *ReadingStore) Generate(opts services.ThemedOptions) bool {
	return s.run(locales.ThemedReadingGenerate(), "themedReading.generate", func(op opState) (outcome, error) {
		if s.PreGenerate != nil {
			if err := s.PreGenerate(s.ctx()); err != nil {
				return outcome{}, err
			}
		}
		s.pin(op, true)
		return taskOutcome(s.svc.Generate(s.ctx(), op.req, opts))
	}, "")
}

// taskAction 是停止 / 继续 / 不联网继续的公共入口：没有任务时什么也不做。
func (s *ReadingStore) taskAction(label, action string, call func(ctx context.Context, id string) (services.ThemedTask, error)) bool {
	s.mu.Lock()
	task := s.view.Task
	s.mu.Unlock()
	if task == nil {
		return false
	}
	id := task.ID
	return s.run(label, action, func(opState) (outcome, error) { return taskOutcome(call(s.ctx(), id)) }, "")
}

// Cancel 停止当前任务（已发出的模型请求可能已计费）。
func (s *ReadingStore) Cancel() bool {
	return s.taskAction(locales.ThemedReadingStop(), "themedReading.cancel", s.svc.Cancel)
}

// ContinueOffline 把停在联网查证阶段的任务改为不联网继续。
func (s *ReadingStore) ContinueOffline() bool {
	return s.taskAction(locales.ThemedReadingReaderSwitchOffline(), "themedReading.continueOffline", s.svc.ContinueOffline)
}

// Resume 继续未完成的部分。
func (s *ReadingStore) Resume() bool {
	return s.taskAction(locales.ThemedReadingResume(), "themedReading.resume", s.svc.Resume)
}

// RegenerateAsset 只重做（或重试）一张图，不重新设计页面。
func (s *ReadingStore) RegenerateAsset(assetID string) bool {
	return s.run(locales.ThemedReadingRedraw(), "themedReading.regenerateAsset", func(op opState) (outcome, error) {
		return taskOutcome(s.svc.RegenerateAsset(s.ctx(), op.req, assetID))
	}, "")
}

// Restore 把上一版恢复为当前版。
func (s *ReadingStore) Restore() bool {
	return s.run(locales.ThemedReadingRestore(), "themedReading.restore", func(op opState) (outcome, error) {
		s.pin(op, false)
		return outcome{}, s.svc.RestorePrevious(s.ctx(), op.req)
	}, locales.ThemedReadingRestored())
}

// Remove 删除这份内容的主题页（正文保留）。
func (s *ReadingStore) Remove() bool {
	return s.run(locales.ThemedReadingDelete(), "themedReading.delete", func(op opState) (outcome, error) {
		s.pin(op, false)
		return outcome{}, s.svc.Remove(s.ctx(), op.req)
	}, locales.ThemedReadingRemoved())
}

// ExportHTML 导出当前版本为单文件 HTML；用户在保存对话框里取消不算失败也不弹成功提示。
func (s *ReadingStore) ExportHTML(withoutImages, staticOnly bool) bool {
	return s.run(locales.ThemedReadingExport(), "themedReading.export", func(op opState) (outcome, error) {
		res, err := s.svc.ExportHTML(s.ctx(), services.ThemedExportInput{
			ThemedRequest: op.req, WithoutImages: withoutImages, StaticOnly: staticOnly, VersionID: op.pageID,
		})
		return outcome{cancelled: res.Cancelled}, err
	}, locales.ThemedReadingExported())
}