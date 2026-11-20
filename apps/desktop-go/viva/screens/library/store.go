// Package library 是「知识库」模块的界面（渲染层 components/library）：
// 二级侧栏里的范围页签、智能视图、合集、平台、标签与回收站；工作区随后续里程碑迁入。
package library

import (
	"context"
	"fmt"
	"sync"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// Filters 是知识库的筛选（渲染层 knowledge.store 的 scope 加三个分面）。
// 范围与合集、平台、标签是正交的：「收藏的抖音条目」与「工作库里的某标签」都是每天会用的查询，不能互相清空。
// 分面的空串表示没有选。
type Filters struct {
	Scope        string
	CollectionID string
	TagID        string
	Platform     string
}

// IsTrashActive 报告回收站这一行是否选中：范围是回收站且没有叠加任何分面（渲染层 isTrashActive）。
func (f Filters) IsTrashActive() bool {
	return f.Scope == services.LibraryScopeTrash && f.CollectionID == "" && f.TagID == "" && f.Platform == ""
}

// FacetQuery 是侧栏计数用的查询（渲染层 buildFacetCountsQuery）。
func (f Filters) FacetQuery() services.LibraryFacetQuery {
	return services.LibraryFacetQuery{Scope: f.Scope, CollectionID: f.CollectionID, TagID: f.TagID, Platform: f.Platform}
}

// View 是 Store 此刻的快照，界面线程只读它；切片与 Counts 在 Store 里是整体替换的，快照拿到之后不会被改。
type View struct {
	Filters Filters
	// Counts 是最近一次读到的侧栏计数；nil 表示还没读到（渲染层 counts: null）。
	Counts      *services.LibraryCounts
	Collections []services.LibraryCollection
	Tags        []services.LibraryTag
	// 三份读取各自的失败原因（台账 B-036，渲染层没有这个状态）：只在「这份数据从没成功读到过、且最近一次读取失败」时非空，
	// 侧栏据此在对应分区的位置显示错误态而不是空态；读到过之后的刷新失败只留日志，不清已有的数据，也不进这里。
	CountsError, CollectionsError, TagsError string
	// List 是条目列表的状态（store_list.go）。
	List ListView
	// Batch 是批量状态条背后的状态（store_batch.go）。
	Batch BatchState
	// Detail 是右侧详情背后的状态（store_detail.go）。
	Detail DetailView
	// Review 是连续整理的顺序与进度（store_review.go）。
	Review ReviewState
}

// DeletionUndoWindow 是删除合集、标签后的撤销窗口，与撤销提示条的停留时长一致（渲染层 DELETION_UNDO_WINDOW_MS）。
const DeletionUndoWindow = 8 * time.Second

// Store 是知识库侧栏背后的状态（渲染层 knowledge.store 的筛选与计数部分、collection.store、tag.store）。
//
// 方法都是同步的、可从任意 goroutine 调用；界面线程只读 View。变化后调用 invalidate 请求重绘。
// 筛选的变化立即生效，随后的重新读取放在后台（Spawn）。
type Store struct {
	svc        services.Library
	guard      kit.Guard
	invalidate func()

	// After 注册一个延时回调并返回取消函数；默认是真实定时器，测试注入可手动触发的假实现。
	After func(d time.Duration, fn func()) (stop func())
	// Spawn 在后台执行一个动作；默认开 goroutine，测试注入同步执行以保持确定性。
	Spawn func(fn func())
	// AutoSave 报告设置里的自动保存是否开着（渲染层 useSettingsStore 的 autoSave）；关着时编辑只进待保存队列，
	// 由保存按钮、Ctrl+S、切换条目与改筛选时落盘。nil 按开着处理。
	AutoSave func() bool

	mu   sync.Mutex
	view View
	// detail 是详情的内部状态（store_detail.go）；bg 是 Store 自己发起读取与保存时用的 context（Load 时记下）。
	detail detailState
	bg     context.Context
	// 合集与标签是否成功读到过（B-036：只有从没读到过时，读取失败才进错误态）。
	collectionsLoaded, tagsLoaded bool

	// countsSeq 是计数请求的序号：请求可能乱序返回，不能让旧筛选的数字覆盖新筛选（渲染层 countsRequestSeq）。
	countsSeq int
	list      listState

	// 撤销窗口内已从界面隐藏、尚未提交给后端的删除。重新读取列表时必须绕开它，否则被删的行会在窗口内闪回来。
	pending map[pendingKey]struct{}
	// 尚未提交的删除；退出窗口前统一提交，避免关窗口把「已删除」的行又带回来。
	pendingCommits map[int]func()
	nextCommit     int

	// OnEvidenceCleared 在彻底删除并选了「同时清除问答证据」之后，带着真正删掉的条目 ID 调用（渲染层 clearCachedConversationEvidence）。
	// 问答随 M3 迁入，在此之前为 nil（台账 B-054）。
	OnEvidenceCleared func(ids []string)

	// batchRun 是当前批次的回执键；newID 生成它（测试注入固定值）。
	batchRun string
	newID    func() string
	// pendingFlags 是在途的收藏、置顶翻转（键为 字段+条目 ID）：连点两次要以在途的值为准，不能都发同一个值。
	pendingFlags map[string]bool
}

// NewStore 创建一个侧栏状态。svc 不能为空。
func NewStore(svc services.Library, guard kit.Guard, invalidate func()) *Store {
	return &Store{
		svc:        svc,
		guard:      guard,
		invalidate: invalidate,
		After: func(d time.Duration, fn func()) func() {
			t := time.AfterFunc(d, fn)
			return func() { t.Stop() }
		},
		Spawn:          func(fn func()) { go fn() },
		view:           View{Filters: Filters{Scope: services.LibraryScopeAll}, List: initialList()},
		detail:         newDetailState(),
		list:           listState{cursors: map[int]string{1: ""}},
		pending:        map[pendingKey]struct{}{},
		pendingCommits: map[int]func(){},
		newID:          randomID,
		pendingFlags:   map[string]bool{},
	}
}

// View 返回当前快照。
func (s *Store) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view
}

func (s *Store) wake() {
	if s.invalidate != nil {
		s.invalidate()
	}
}

// ---- 筛选 ----

// update 改筛选并在后台重新读取（渲染层 updateFilters）：没有变化也照样重新读取。
// 同一刻列表回到第一页、丢掉全部分页游标、清掉选中与多选，并让旧筛选在途的读取作废（台账 B-033 的 (2)(3)）。
// 详情里有没落盘的编辑时，先落盘再换筛选；落盘失败筛选不变（改动留在待保存队列里，失败原因在详情头部）——台账 B-033 的 (1)。
func (s *Store) update(ctx context.Context, mutate func(*Filters)) {
	s.mu.Lock()
	flush := s.needsFlushLocked()
	s.mu.Unlock()
	if !flush {
		s.applyFilters(ctx, mutate)
		return
	}
	s.Spawn(func() {
		if s.FlushPendingSave(ctx) {
			s.applyFilters(ctx, mutate)
		}
	})
}

// applyFilters 是 update 的落定部分：筛选立即生效，随后的重新读取放到后台。
func (s *Store) applyFilters(ctx context.Context, mutate func(*Filters)) {
	s.mu.Lock()
	prev := s.view.Filters
	next := prev
	mutate(&next)
	s.view.Filters = next
	if len(s.view.List.Selection) > 0 && next != prev {
		s.view.Batch.Notice = locales.LibraryWorkflowSelectionCleared()
	}
	s.resetPaging()
	s.resetSelection()
	s.list.seq++
	s.view.List.Loading = true
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() { s.RefreshAll(ctx) })
}

// SetScope 切换范围；分面保持不变。
func (s *Store) SetScope(ctx context.Context, scope string) {
	s.update(ctx, func(f *Filters) { f.Scope = scope })
}

// toggled 是分面的切换规则：点已选中的那一项取消选择（渲染层 selectCollection / selectTag / selectPlatform）。
func toggled(current, target string) string {
	if target != "" && current == target {
		return ""
	}
	return target
}

// SelectCollection 选中合集；再选一次同一个就取消。空串清掉合集筛选。
func (s *Store) SelectCollection(ctx context.Context, id string) {
	s.update(ctx, func(f *Filters) { f.CollectionID = toggled(f.CollectionID, id) })
}

// SelectTag 选中标签；再选一次同一个就取消。
func (s *Store) SelectTag(ctx context.Context, id string) {
	s.update(ctx, func(f *Filters) { f.TagID = toggled(f.TagID, id) })
}

// SelectPlatform 选中平台；再选一次同一个就取消。
func (s *Store) SelectPlatform(ctx context.Context, platform string) {
	s.update(ctx, func(f *Filters) { f.Platform = toggled(f.Platform, platform) })
}

// ClearFacetFilters 清掉三个分面，范围保持不变。
func (s *Store) ClearFacetFilters(ctx context.Context) {
	s.update(ctx, func(f *Filters) { f.CollectionID, f.TagID, f.Platform = "", "", "" })
}

// ApplyFilters 一次设定范围与全部分面（智能视图）。
func (s *Store) ApplyFilters(ctx context.Context, target Filters) {
	s.update(ctx, func(f *Filters) { *f = target })
}

// ---- 读取 ----

// RefreshCounts 重新读取侧栏计数。只认最后发出的那次请求：旧请求晚到不覆盖，失败也只对最后一次留痕。
// 读取失败不打扰用户（计数是背景信息，没有用户动作），只进日志。
func (s *Store) RefreshCounts(ctx context.Context) {
	s.mu.Lock()
	s.countsSeq++
	seq := s.countsSeq
	query := s.facetQuery()
	s.mu.Unlock()

	counts, err := s.svc.Counts(ctx, query)

	s.mu.Lock()
	if seq != s.countsSeq {
		s.mu.Unlock()
		return
	}
	switch {
	case err == nil:
		s.view.Counts, s.view.CountsError = &counts, ""
	case s.view.Counts == nil: // 从没读到过：侧栏要显示错误态（B-036）；读到过的数字留着
		s.view.CountsError = err.Error()
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("library.counts", fmt.Errorf("加载侧栏计数失败：%w", err))
	}
	s.wake()
}

// Load 是知识库工作区挂载时的读取（渲染层 LibraryWorkspace 的挂载 effect）：计数、合集、标签各在后台读一遍。
func (s *Store) Load(ctx context.Context) {
	s.mu.Lock()
	s.bg = ctx
	s.view.List.Loading = true // 第一帧就是加载态，不是「暂无条目」
	s.mu.Unlock()
	s.Spawn(func() { s.RefreshAll(ctx) })
	s.Spawn(func() { s.FetchCollections(ctx) })
	s.Spawn(func() { s.FetchTags(ctx) })
}

// OnImportCompleted 在采集任务完成时调用（渲染层 import.store 的 handleChanged）：刷新筛选相关的数据；
// 任务带了标签时再重读标签列表（新标签要出现在侧栏里）。
func (s *Store) OnImportCompleted(ctx context.Context, task services.ImportTask) {
	s.Spawn(func() { s.RefreshAll(ctx) })
	if len(task.TagNames) > 0 {
		s.Spawn(func() { s.FetchTags(ctx) })
	}
}
