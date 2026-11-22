package wiki

import (
	"context"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// View 是 Wiki 模块每帧读取的快照：切片、指针指向的数据都是整体替换、从不就地修改的，拿到之后不会被后台改动。
type View struct {
	Catalog        []services.WikiCatalogEntry
	BacklinkCounts services.WikiBacklinkCounts
	// Status 是顶栏状态行的三个数字；nil 表示还没读到。
	Status *services.WikiStatus

	Filter CatalogFilter
	Sort   CatalogSort
	// SearchQuery 是搜索框里的原文（含首尾空白）。SearchActive 为 true 表示目录被搜索命中集合限定，
	// SearchHitIDs 是命中的页面 ID（按相关度排序，可以为空：搜到零条）。
	SearchQuery  string
	SearchActive bool
	SearchHitIDs []string
	Searching    bool
	// SearchError 非空时目录列必须显示搜索失败，不能画成「无结果」。
	SearchError string

	SelectedPageID string
	Detail         *services.WikiPageDetail
	Revisions      []services.WikiRevision
	PageLoading    bool
	// PageError 是选中页读取失败的原因（带重试：再次 SelectPage）。
	PageError string

	// HasLoaded 为首次加载是否已结束（无论成败）。目录为空既可能是「真没有页面」也可能是「还没读出来」，
	// 界面必须先能区分：否则每次进 Wiki 都先铺一屏空态，再被两栏布局整块顶掉。
	HasLoaded bool
	// LoadError 非空时界面必须渲染成错误态（带重试：Refresh），不能当作空态。
	LoadError string

	// IsCompiling 是编译进行中（手动编译的进度细节在 Store.Compile() 里）。
	IsCompiling bool
	// CompileCounts 是全文编译预览的分类计数；nil 表示未知或读取失败。
	CompileCounts *services.WikiCompileCounts

	Mode         ViewMode
	Graph        *GraphModel
	GraphLoading bool
	GraphError   string

	// Editing 为正文编辑中，Draft 是编辑框里的内容（切页、删除、回滚、采用建议时丢弃）。
	Editing bool
	Draft   string
}

// Visible 是目录列当前可见的页面（筛选、排序与搜索叠加后的结果）。
func (v View) Visible() []services.WikiCatalogEntry {
	return SelectVisible(VisibleParams{
		Catalog: v.Catalog, BacklinkCounts: v.BacklinkCounts, Filter: v.Filter, Sort: v.Sort,
		SearchActive: v.SearchActive, SearchHitIDs: v.SearchHitIDs,
	})
}

// Counts 是侧栏筛选轴各档的计数。
func (v View) Counts() CatalogCounts { return CountByFilter(v.Catalog, v.BacklinkCounts) }

// Dirty 报告编辑框里是否有尚未保存的改动。
func (v View) Dirty() bool { return v.Editing && v.Detail != nil && v.Draft != v.Detail.Page.Body }

// CompileButton 是工具栏编译按钮的口径：待编译数与能否打开对话框（见 CompileButtonState）。
func (v View) CompileButton() (pending int, actionable bool) {
	return CompileButtonState(v.CompileCounts)
}

// Store 是 Wiki 模块的状态（wiki.store.ts）：目录与筛选、搜索、页面详情与编辑、图谱，并持有编译与更新建议两个子状态。
//
// 方法都是同步的、可从任意 goroutine 调用；界面线程只读 View。变化后调用 invalidate 请求重绘。
// 异步结果都带代次：筛选、搜索、选页、刷新各自只认最后发出的那一次，晚到的旧响应不会覆盖新状态。
type Store struct {
	svc        services.Wiki
	guard      kit.Guard
	invalidate func()

	// Ctx 是发给服务的请求的根上下文；nil 取 context.Background()。
	Ctx context.Context
	// Spawn 在后台执行一个动作（预览计数、图谱加载、编译运行）；默认开 goroutine，测试注入同步执行以保持确定性。
	Spawn func(fn func())
	// OnOpenSource 打开来源条目：切到知识库模块并选中它（详情页的来源列表）。可空。
	OnOpenSource func(itemID string)

	compile    *CompileStore
	suggestion *SuggestionStore

	mu          sync.Mutex
	view        View
	initialized bool
	// 各类异步请求的代次：同类请求只认最后发出的那一次。
	refreshGen, searchGen, selectGen, graphGen, countsGen int
}

// NewStore 创建 Wiki 状态。svc 不能为空。
func NewStore(svc services.Wiki, guard kit.Guard, invalidate func()) *Store {
	s := &Store{
		svc: svc, guard: guard, invalidate: invalidate,
		Spawn: func(fn func()) { go fn() },
		view:  View{Filter: FilterAll, Sort: SortRecent, Mode: ViewCatalog},
	}
	s.compile = newCompileStore(s)
	s.suggestion = newSuggestionStore(s)
	return s
}

// Compile 返回全文编译对话框的状态。
func (s *Store) Compile() *CompileStore { return s.compile }

// Suggestion 返回当前选中页的更新建议状态。
func (s *Store) Suggestion() *SuggestionStore { return s.suggestion }

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

func (s *Store) ctx() context.Context {
	if s.Ctx != nil {
		return s.Ctx
	}
	return context.Background()
}

func (s *Store) spawn(fn func()) {
	if s.Spawn != nil {
		s.Spawn(fn)
		return
	}
	go fn()
}

// ---- 加载 ----

// Initialize 首次进入 Wiki 时加载目录。重复调用无副作用；失败后再次调用会重试（界面的重试按钮直接调 Refresh 也行）。
func (s *Store) Initialize() {
	s.mu.Lock()
	if s.initialized {
		s.mu.Unlock()
		return
	}
	s.initialized = true
	s.mu.Unlock()
	if err := s.refresh(); err != nil {
		s.mu.Lock()
		s.initialized = false
		s.mu.Unlock()
	}
}

// Refresh 重读目录、状态与反向链接计数（并后台重算编译预览计数）；选中页与图谱跟着同步。
// 失败设置 LoadError（带重试），不会渲染成空态。
func (s *Store) Refresh() { _ = s.refresh() }

func (s *Store) refresh() error {
	s.mu.Lock()
	s.refreshGen++
	gen := s.refreshGen
	s.view.LoadError = ""
	s.mu.Unlock()
	s.wake()
	// 预览要逐条算指纹，大库较慢：与目录并行，不挡住目录渲染。
	s.spawn(s.refreshCompileCounts)

	var (
		catalog    []services.WikiCatalogEntry
		status     services.WikiStatus
		backlinks  services.WikiBacklinkCounts
		errCatalog error
		errStatus  error
		errLinks   error
		wg         sync.WaitGroup
	)
	wg.Add(3)
	go func() { defer wg.Done(); catalog, errCatalog = s.svc.Catalog(s.ctx()) }()
	go func() { defer wg.Done(); status, errStatus = s.svc.Status(s.ctx()) }()
	go func() { defer wg.Done(); backlinks, errLinks = s.svc.BacklinkCounts(s.ctx()) }()
	wg.Wait()

	err := firstError(errCatalog, errStatus, errLinks)
	reloadGraph := false
	if err == nil {
		s.mu.Lock()
		if gen == s.refreshGen {
			s.view.Catalog, s.view.Status, s.view.BacklinkCounts = catalog, &status, backlinks
			reloadGraph = s.view.Mode == ViewGraph
		}
		s.mu.Unlock()
		s.wake()
		if reloadGraph {
			s.spawn(func() { s.LoadGraph() })
		}
		err = s.syncSelected()
	}

	s.mu.Lock()
	if err != nil && gen == s.refreshGen {
		s.view.LoadError = err.Error()
	}
	// 失败也要放行：否则界面会一直停在加载态，用户连空态引导都看不到。
	s.view.HasLoaded = true
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("wiki.refresh", err)
	}
	s.wake()
	return err
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// syncSelected 在目录刷新后同步选中页的详情与历史版本：页面可能已被重建清掉（读不到就取消选中）。
// 读取期间用户又选了别的页，这次结果作废。
func (s *Store) syncSelected() error {
	s.mu.Lock()
	id, gen := s.view.SelectedPageID, s.selectGen
	s.mu.Unlock()
	if id == "" {
		return nil
	}
	detail, revisions, err := s.readPage(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.selectGen != gen || s.view.SelectedPageID != id {
		s.mu.Unlock()
		return nil
	}
	if detail == nil {
		s.clearSelectionLocked()
	} else {
		s.view.Detail, s.view.Revisions = detail, revisions
	}
	s.mu.Unlock()
	s.wake()
	s.followSuggestion(detail)
	return nil
}

// refreshCompileCounts 后台重算全文编译预览的分类计数；读取失败记日志并把计数置为未知（按钮不拦，口径见 CompileButtonState）。
func (s *Store) refreshCompileCounts() {
	s.mu.Lock()
	s.countsGen++
	gen := s.countsGen
	s.mu.Unlock()
	preview, err := s.svc.CompilePreview(s.ctx())
	s.mu.Lock()
	if gen != s.countsGen {
		s.mu.Unlock()
		return
	}
	if err != nil {
		s.view.CompileCounts = nil
	} else {
		counts := preview.Counts
		s.view.CompileCounts = &counts
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("wiki.compilePreview", err)
	}
	s.wake()
}

// SetCompiling 标记编译进行中（手动编译由 CompileStore 设置；后台自动编译的调度器也用它）。
func (s *Store) SetCompiling(on bool) {
	s.mu.Lock()
	s.view.IsCompiling = on
	s.mu.Unlock()
	s.wake()
}

// OnContributionsUpdated 在一轮编译结束（无论成败）后调用：重读目录并重新读当前页的更新建议
// （为手动改过的页面生成建议不会改动页面本身，光看 updatedAt 发现不了）。
func (s *Store) OnContributionsUpdated() {
	s.suggestion.Reload()
	s.Refresh()
}

// ---- 筛选与搜索 ----

// SetFilter 切换目录筛选轴。
func (s *Store) SetFilter(f CatalogFilter) {
	s.mu.Lock()
	s.view.Filter = f
	s.mu.Unlock()
	s.wake()
}

// SetSort 切换目录排序轴（搜索结果按相关度排，不受它影响）。
func (s *Store) SetSort(sort CatalogSort) {
	s.mu.Lock()
	s.view.Sort = sort
	s.mu.Unlock()
	s.wake()
}

// RunSearch 目录列搜索：走全文索引（覆盖正文）。空白查询清空命中集合；只认最后一次输入对应的结果。
// 失败时命中集合置空并设置 SearchError——只清空的话搜索失败会被画成「无结果」，最误导。
func (s *Store) RunSearch(query string) {
	trimmed := jsTrim(query)
	s.mu.Lock()
	s.searchGen++
	gen := s.searchGen
	s.view.SearchQuery = query
	if trimmed == "" {
		s.view.SearchActive, s.view.SearchHitIDs, s.view.Searching, s.view.SearchError = false, nil, false, ""
		s.mu.Unlock()
		s.wake()
		return
	}
	s.view.Searching, s.view.SearchError = true, ""
	s.mu.Unlock()
	s.wake()

	hits, err := s.svc.Search(s.ctx(), trimmed, SearchLimit)

	s.mu.Lock()
	if gen != s.searchGen {
		s.mu.Unlock()
		return // 已被更新的输入取代：它负责收尾（含 Searching）
	}
	s.view.Searching = false
	if err != nil {
		s.view.SearchActive, s.view.SearchHitIDs, s.view.SearchError = true, []string{}, err.Error()
	} else {
		ids := make([]string, len(hits))
		for i, h := range hits {
			ids[i] = h.ID
		}
		s.view.SearchActive, s.view.SearchHitIDs = true, ids
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("wiki.search", err)
	}
	s.wake()
}

// ---- 图谱 ----

// SetViewMode 切换目录 / 图谱视图；切到图谱时后台加载图谱数据。
func (s *Store) SetViewMode(mode ViewMode) {
	s.mu.Lock()
	s.view.Mode = mode
	s.mu.Unlock()
	s.wake()
	if mode == ViewGraph {
		s.spawn(func() { s.LoadGraph() })
	}
}

// LoadGraph 读取图谱并算好布局。失败设置 GraphError（带重试：再次 LoadGraph），旧图谱保留在原地。
func (s *Store) LoadGraph() {
	s.mu.Lock()
	s.graphGen++
	gen := s.graphGen
	s.view.GraphLoading, s.view.GraphError = true, ""
	s.mu.Unlock()
	s.wake()

	g, err := s.svc.Graph(s.ctx())
	var model *GraphModel
	if err == nil {
		m := BuildGraphModel(g)
		model = &m
	}

	s.mu.Lock()
	if gen != s.graphGen {
		s.mu.Unlock()
		return
	}
	s.view.GraphLoading = false
	if err != nil {
		s.view.GraphError = err.Error()
	} else {
		s.view.Graph = model
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("wiki.graph", err)
	}
	s.wake()
}

// OpenFromGraph 点击图谱节点：回到目录视图并打开那一页。
func (s *Store) OpenFromGraph(pageID string) {
	s.SetViewMode(ViewCatalog)
	s.SelectPage(pageID)
}

// OpenSource 打开页面的来源条目（切到知识库模块）；没有接线时什么也不做。
func (s *Store) OpenSource(itemID string) {
	if s.OnOpenSource != nil {
		s.OnOpenSource(itemID)
	}
}

// linkNotFound 弹出「页面不存在」的提示（[[链接]] 指向不存在的页面）。
func (s *Store) linkNotFound(target string) {
	if s.guard.Notify != nil {
		s.guard.Notify.Info(locales.WikiLinkNotFound(target))
	}
}
