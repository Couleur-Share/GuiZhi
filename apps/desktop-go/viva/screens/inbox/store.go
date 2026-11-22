// Package inbox 是处理中心（渲染层 InboxWorkspace / SidebarInboxPanel / stores/inbox.store.ts）的状态与界面。
//
// Package inbox holds the state machine and UI of the processing center.
package inbox

import (
	"context"
	"strings"
	"sync"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// Filter 是侧栏筛选组的取值（渲染层 InboxFilter）：all 或某个分组。取值与 services.InboxKind* 一致。
type Filter string

const (
	FilterAll             Filter = "all"
	FilterReviewRequired  Filter = Filter(services.InboxKindReviewRequired)
	FilterUnclassified    Filter = Filter(services.InboxKindUnclassified)
	FilterImportIssue     Filter = Filter(services.InboxKindImportIssue)
	FilterDiscovery       Filter = Filter(services.InboxKindDiscoveryCandidate)
	FilterSemanticPending Filter = Filter(services.InboxKindSemanticPending)
	FilterWikiPending     Filter = Filter(services.InboxKindWikiPending)
)

// FilterOrder 是侧栏里各行的顺序（SidebarInboxPanel 的 ROWS）：「未归合集」是可选整理，「语义索引」「Wiki 编译」是后台任务，
// 各自在界面上另起一组。
var FilterOrder = []Filter{
	FilterAll, FilterReviewRequired, FilterImportIssue, FilterUnclassified, FilterSemanticPending, FilterWikiPending,
}

// RunningKind 标记哪一类聚合行正在执行（渲染层 runningKind，目前只有语义索引会占用）。
type RunningKind string

const (
	RunningNone     RunningKind = ""
	RunningSemantic RunningKind = RunningKind(services.InboxKindSemanticPending)
)

// SectionErrors 是不阻塞主列表的两块状态的读取失败原因：Wiki 候选与语义索引进度。空串表示没有失败。
type SectionErrors struct {
	Wiki     string
	Semantic string
}

// Any 报告是否有任何一块读取失败。
func (e SectionErrors) Any() bool { return e.Wiki != "" || e.Semantic != "" }

// ClassifyProgress 是 AI 智能归类的批进度；Total 为 0 表示没有在归类。
type ClassifyProgress struct {
	Completed, Total int
}

// View 是界面每帧读取的快照：切片都是写时复制的，拿到之后不会被后台改动。
type View struct {
	Items  []services.InboxItem // 全部条目：需要处理的行在前，后台任务的聚合行（语义索引、Wiki）在后
	Counts services.InboxCounts
	Total  int // 需人工处理的内容去重总数，不含后台任务
	Filter Filter

	// IsLoading 与 LoadError 只描述主列表。LoadError 非空时界面必须渲染成错误态，不能当作「已处理完」的空态。
	IsLoading     bool
	HasLoaded     bool
	LoadError     string
	SectionErrors SectionErrors

	// Wiki 是 Wiki 编译候选的各状态条数；nil 表示还没读到。读取失败时保留上一次的值（与渲染层一致），失败原因在 SectionErrors.Wiki。
	Wiki *services.InboxWikiCandidates
	// WikiModelReady 为假且有候选在等待时，界面说明「尚未配置文本模型」并给出配置入口，而不是显示像故障的「等待重试」。
	WikiModelReady bool
	WikiNeedsModel bool

	// Selection 是选中的条目 ID，按选中顺序。
	Selection []string

	// 由 Items、Filter、Selection 派生，每次变化时重算。
	Visible              []services.InboxItem // 当前分组可见的行：「全部」不含可选整理（未归合集），与角标口径一致
	SelectableVisibleIDs []string             // 可见行里能勾选的知识条目 ID（聚合行与采集问题行没有）
	SelectedVisibleCount int
	SelectedUnclassified []string // 选中的、仍未归合集的条目 ID：智能归类只处理这些

	// 批量整理条的输入与忙闲。Organizing 期间再次触发整理会被忽略（防止连点把同一批提交两次）。
	OrganizeCollectionID string
	OrganizeTagText      string
	Organizing           bool

	Running             RunningKind // 哪个聚合行在执行；非空时两个聚合行的按钮都禁用
	AcknowledgingTaskID string      // 正在处理「不再提醒」的采集任务
	Classifying         bool
	Classify            ClassifyProgress
}

// AllVisibleSelected 报告可见的可选行是否全部选中（「全选当前列表」的勾选状态）。
func (v View) AllVisibleSelected() bool {
	return len(v.SelectableVisibleIDs) > 0 && v.SelectedVisibleCount == len(v.SelectableVisibleIDs)
}

// SomeVisibleSelected 报告是否只选了一部分（「全选当前列表」的半选状态）。
func (v View) SomeVisibleSelected() bool {
	return v.SelectedVisibleCount > 0 && !v.AllVisibleSelected()
}

// IsSelected 报告条目是否在选中集合里。
func (v View) IsSelected(itemID string) bool {
	for _, id := range v.Selection {
		if id == itemID {
			return true
		}
	}
	return false
}

// CountOf 返回侧栏某一行的角标：「全部待处理」是去重总数，其余是分组计数。
func (v View) CountOf(f Filter) int {
	if f == FilterAll {
		return v.Total
	}
	return v.Counts.Of(string(f))
}

// HeaderCount 是顶栏「N 条内容待处理 / 待编译 / 待索引」的数字（与 CountOf 一致）。
func (v View) HeaderCount() int { return v.CountOf(v.Filter) }

// Store 是处理中心的状态（渲染层 inbox.store.ts，加上 InboxWorkspace 组件里的局部状态与动作）：
// 列表与计数、筛选、选择、批量整理、AI 智能归类、单行动作与聚合行的「立即执行」。
//
// 方法都是同步的、可从任意 goroutine 调用；界面线程只读 View。变化后调用 invalidate 请求重绘。
type Store struct {
	svc        services.Inbox
	guard      kit.Guard
	invalidate func()

	// Spawn 在后台执行一个动作；默认开 goroutine，测试注入同步执行以保持确定性。
	// Refresh 用它并发读取主列表、Wiki 状态与语义状态三块。
	Spawn func(fn func())

	// 以下钩子由 Root 接线；全部可空（空时对应动作什么都不做）。
	// OnOpenItem 在知识库里打开条目：切到知识库模块、范围设为全部、选中该条目（渲染层 openItem）。
	OnOpenItem func(ctx context.Context, itemID string)
	// OnOpenSettingsAI 打开设置的 AI 分区（渲染层 requestSettingsSection("ai")）。
	OnOpenSettingsAI func()
	// OnWikiPreview 打开 Wiki 编译预览（渲染层的 "wiki-compile-preview" 事件）。
	OnWikiPreview func()
	// OnClassified 在智能归类写入后刷新合集与知识库视图（渲染层 fetchCollections + refreshKnowledge）。
	OnClassified func(ctx context.Context)
	// CollectionNames 返回现有合集名称，智能归类时交给模型优先复用。
	CollectionNames func() []string
	// RefreshImports 在重试采集任务后刷新采集队列（渲染层 retryTask 里的 fetchTasks）。
	RefreshImports func(ctx context.Context)

	mu   sync.Mutex
	view View

	// 刷新代次：快速连续刷新时，旧一轮的结果不能覆盖新一轮（渲染层模块级的 refreshGeneration）。
	generation int
	// 进行中的 AI 归类；非 nil 表示正在归类，再次触发即为取消。
	classify *classifyRun
}

type classifyRun struct{ cancel context.CancelFunc }

// NewStore 创建处理中心状态。svc 不能为空。
func NewStore(svc services.Inbox, guard kit.Guard, invalidate func()) *Store {
	s := &Store{
		svc:        svc,
		guard:      guard,
		invalidate: invalidate,
		Spawn:      func(fn func()) { go fn() },
	}
	s.view = View{Filter: FilterAll, WikiModelReady: true}
	s.recomputeLocked()
	return s
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

// ---- 派生状态 ----

// VisibleItems 求某个分组可见的行：「全部」不含可选整理（未归合集），与角标口径一致。
func VisibleItems(items []services.InboxItem, f Filter) []services.InboxItem {
	out := make([]services.InboxItem, 0, len(items))
	for _, item := range items {
		if f == FilterAll {
			if item.Kind == services.InboxKindUnclassified {
				continue
			}
		} else if item.Kind != string(f) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// SelectableIDs 返回行里能勾选的知识条目 ID（只有复核与未归合集两类行带条目 ID）。
func SelectableIDs(items []services.InboxItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if id := item.KnowledgeItemID(); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// ImportTargetID 是采集问题行上「查看条目」要打开的条目：重复的那条优先，其次是采集结果；都没有返回空串。
func ImportTargetID(item services.InboxItem) string {
	if item.DuplicateItemID != "" {
		return item.DuplicateItemID
	}
	return item.ResultItemID
}

// recomputeLocked 在条目、分组或选择变化后重算派生字段。
func (s *Store) recomputeLocked() {
	v := &s.view
	v.Visible = VisibleItems(v.Items, v.Filter)
	v.SelectableVisibleIDs = SelectableIDs(v.Visible)
	selected := make(map[string]bool, len(v.Selection))
	for _, id := range v.Selection {
		selected[id] = true
	}
	count := 0
	for _, id := range v.SelectableVisibleIDs {
		if selected[id] {
			count++
		}
	}
	v.SelectedVisibleCount = count
	unclassified := make([]string, 0, len(v.Selection))
	for _, item := range v.Items {
		if item.Kind == services.InboxKindUnclassified && selected[item.ItemID] {
			unclassified = append(unclassified, item.ItemID)
		}
	}
	v.SelectedUnclassified = unclassified
	v.WikiNeedsModel = v.Wiki != nil && !v.WikiModelReady && v.Wiki.Waiting > 0
}

// ---- 筛选与选择 ----

// SetFilter 切换分组；选择随之清空（选中的行可能已不在新分组里）。
func (s *Store) SetFilter(f Filter) {
	s.mu.Lock()
	s.view.Filter, s.view.Selection = f, nil
	s.recomputeLocked()
	s.mu.Unlock()
	s.wake()
}

// ToggleSelection 切换一个条目的选中状态。
func (s *Store) ToggleSelection(itemID string) {
	s.mu.Lock()
	next := make([]string, 0, len(s.view.Selection)+1)
	found := false
	for _, id := range s.view.Selection {
		if id == itemID {
			found = true
			continue
		}
		next = append(next, id)
	}
	if !found {
		next = append(next, itemID)
	}
	s.view.Selection = next
	s.recomputeLocked()
	s.mu.Unlock()
	s.wake()
}

// SetSelection 用一组条目 ID 替换选择（去重，保持首次出现的顺序）。
func (s *Store) SetSelection(itemIDs []string) {
	s.mu.Lock()
	seen := make(map[string]bool, len(itemIDs))
	next := make([]string, 0, len(itemIDs))
	for _, id := range itemIDs {
		if !seen[id] {
			seen[id] = true
			next = append(next, id)
		}
	}
	s.view.Selection = next
	s.recomputeLocked()
	s.mu.Unlock()
	s.wake()
}

// ClearSelection 清空选择。
func (s *Store) ClearSelection() { s.SetSelection(nil) }

// SetAllVisibleSelected 是「全选当前列表」的勾选：勾上则选中当前可见的全部可选行（替换而不是追加，
// 其他分组里选中的行被丢弃），取消则清空选择。
func (s *Store) SetAllVisibleSelected(checked bool) {
	if !checked {
		s.ClearSelection()
		return
	}
	s.mu.Lock()
	ids := s.view.SelectableVisibleIDs
	s.mu.Unlock()
	s.SetSelection(ids)
}

// SetOrganizeCollection 设置批量整理要归入的合集（空串表示不改合集）。
func (s *Store) SetOrganizeCollection(collectionID string) {
	s.mu.Lock()
	s.view.OrganizeCollectionID = collectionID
	s.mu.Unlock()
	s.wake()
}

// SetOrganizeTagText 设置批量整理要添加的标签文本（逗号分隔，中英文逗号都行）。
func (s *Store) SetOrganizeTagText(text string) {
	s.mu.Lock()
	s.view.OrganizeTagText = text
	s.mu.Unlock()
	s.wake()
}

// ParseTagNames 把标签文本拆成标签名：按中英文逗号切开，去掉首尾空白与空项。
// 全角逗号写成转义（\uFF0C）：它是分隔符而不是界面文案，转义也让「Go 代码里没有中文文案」的检查保持干净。
func ParseTagNames(text string) []string {
	var tags []string
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\uFF0C' }) {
		if tag := strings.TrimSpace(part); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// ---- 刷新 ----

// describeLoadError 对应渲染层 describeLoadError：错误原文，没有时给「未知错误」。
func describeLoadError(err error) string {
	if message := strings.TrimSpace(err.Error()); message != "" {
		return message
	}
	return locales.CommonUnknownError()
}

// Refresh 重新读取主列表、Wiki 候选与语义索引进度（三块并发），全部结束后返回。
//
// 主列表失败记在 LoadError（界面渲染成错误态，不会画成「已处理完」的空态）；Wiki 与语义两块失败只记在 SectionErrors，
// 不影响主列表，对应聚合行按 0 处理。快速连续调用时，只有最后一次的结果会落到界面上。
func (s *Store) Refresh(ctx context.Context) {
	s.mu.Lock()
	s.generation++
	gen := s.generation
	s.view.IsLoading, s.view.LoadError, s.view.SectionErrors = true, "", SectionErrors{}
	s.mu.Unlock()
	s.wake()

	var wg sync.WaitGroup
	for _, section := range []func(ctx context.Context, gen int){s.refreshList, s.refreshWiki, s.refreshSemantic} {
		wg.Add(1)
		s.Spawn(func() {
			defer wg.Done()
			section(ctx, gen)
		})
	}
	wg.Wait()
}

func (s *Store) refreshList(ctx context.Context, gen int) {
	list, err := s.svc.List(ctx)
	s.mu.Lock()
	if gen != s.generation {
		s.mu.Unlock()
		return
	}
	s.view.IsLoading = false
	if err != nil {
		s.view.LoadError = describeLoadError(err)
		s.mu.Unlock()
		s.guard.Background(locales.InboxRefresh(), err)
		s.wake()
		return
	}
	// 聚合行不由主列表产出，沿用上一轮已读到的；计数里这两项同理
	entries := make([]services.InboxItem, 0, len(list.Items))
	alive := map[string]bool{}
	for _, item := range list.Items {
		if item.IsAggregate() {
			continue
		}
		entries = append(entries, item)
		if item.ItemID != "" {
			alive[item.ItemID] = true
		}
	}
	items := append(entries, aggregatesOf(s.view.Items)...)
	counts := list.Counts
	counts.WikiPending = s.view.Counts.WikiPending
	counts.SemanticPending = s.view.Counts.SemanticPending
	selection := make([]string, 0, len(s.view.Selection))
	for _, id := range s.view.Selection {
		if alive[id] {
			selection = append(selection, id)
		}
	}
	s.view.Items, s.view.Counts, s.view.Total, s.view.Selection, s.view.HasLoaded = items, counts, list.Total, selection, true
	s.recomputeLocked()
	s.mu.Unlock()
	s.wake()
}

func aggregatesOf(items []services.InboxItem) []services.InboxItem {
	var out []services.InboxItem
	for _, kind := range []string{services.InboxKindSemanticPending, services.InboxKindWikiPending} {
		for _, item := range items {
			if item.Kind == kind {
				out = append(out, item)
			}
		}
	}
	return out
}

// setAggregateLocked 写入（count 为 0 时移除）一个聚合行并同步计数。聚合行固定排在最后，语义索引在 Wiki 之前
// （渲染层按各自请求返回的先后追加，顺序不定；这里取确定的顺序）。
func (s *Store) setAggregateLocked(kind string, count int) {
	var entries, semantic, wiki []services.InboxItem
	for _, item := range s.view.Items {
		switch {
		case item.Kind == services.InboxKindSemanticPending:
			semantic = append(semantic, item)
		case item.Kind == services.InboxKindWikiPending:
			wiki = append(wiki, item)
		default:
			entries = append(entries, item)
		}
	}
	row := []services.InboxItem(nil)
	if count > 0 {
		row = []services.InboxItem{{Kind: kind, ID: "aggregate:" + kind, Count: count, CreatedAt: time.Now().UnixMilli()}}
	}
	if kind == services.InboxKindSemanticPending {
		semantic = row
	} else {
		wiki = row
	}
	items := append(entries, semantic...)
	s.view.Items = append(items, wiki...)
	s.view.Counts = s.view.Counts.With(kind, count)
	s.recomputeLocked()
}

func (s *Store) refreshWiki(ctx context.Context, gen int) {
	candidates, err := s.svc.WikiCandidates(ctx)
	ready, readyErr := s.svc.WikiModelReady(ctx)
	s.mu.Lock()
	if gen != s.generation {
		s.mu.Unlock()
		return
	}
	if readyErr == nil {
		s.view.WikiModelReady = ready
	}
	if err != nil {
		s.setAggregateLocked(services.InboxKindWikiPending, 0)
		s.view.SectionErrors.Wiki = describeLoadError(err)
	} else {
		s.view.Wiki = &candidates
		s.setAggregateLocked(services.InboxKindWikiPending, candidates.Ready)
	}
	s.recomputeLocked()
	s.mu.Unlock()
	if err != nil {
		s.guard.Background(locales.InboxFilterWikiPending(), err)
	}
	if readyErr != nil { // 读不到模型状态时按「已就绪」处理，免得误报「尚未配置」；原因留痕
		s.guard.Background(locales.InboxFilterWikiPending(), readyErr)
	}
	s.wake()
}

func (s *Store) refreshSemantic(ctx context.Context, gen int) {
	status, err := s.svc.SemanticStatus(ctx)
	s.mu.Lock()
	if gen != s.generation {
		s.mu.Unlock()
		return
	}
	if err != nil {
		s.setAggregateLocked(services.InboxKindSemanticPending, 0)
		s.view.SectionErrors.Semantic = describeLoadError(err)
	} else {
		s.setAggregateLocked(services.InboxKindSemanticPending, status.Pending())
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background(locales.InboxFilterSemanticPending(), err)
	}
	s.wake()
}
