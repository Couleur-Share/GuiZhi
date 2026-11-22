package servicestest

import (
	"context"
	"slices"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// Inbox 是 services.Inbox 的假实现。零值可用：没有条目、计数全是零、Wiki 与语义都没有待办、模型未就绪。
//
// 可编程的部分：
//   - 种子：SetList、SetWikiCandidates、SetWikiModelReady、SetSemanticStatus、SetSources、SetAssignments、SetApplyResult、
//     SetChanged、SetSemanticNotice；
//   - 错误注入：FailOn("方法") 或 FailOn("方法:参数")（针对某个任务或条目），优先于整个方法的注入；
//   - 延迟与取消：Hold("方法") 让下一次调用卡住直到放行，卡住期间 ctx 取消会让调用返回 ctx.Err()；
//     HoldClassifyAfter(n) 让归类在回报 n 批进度之后卡住（构造「进行到一半被取消」）；
//   - 调用记录：Calls 与各 *Calls 访问器，按发生顺序。
type Inbox struct {
	mu sync.Mutex

	list       services.InboxList
	wiki       services.InboxWikiCandidates
	modelReady bool
	semantic   services.InboxSemanticStatus

	sources     []services.InboxAISource
	sourcesSet  bool
	assignments map[string]string // 条目 ID → 合集名；缺省时每条归入 defaultCollection
	applyResult services.InboxAIApplyResult
	applySet    bool
	batches     int // Classify 回报的总批数；0 时按每 20 条一批算（与 aiflow 一致）
	changed     map[string]int
	notice      *services.InboxSemanticNotice
	noticeSet   bool
	indexing    bool

	failures map[string]error
	gates    map[string]chan struct{}
	// classifyHoldAfter 大于 0 时，Classify 在回报这么多批进度之后卡住（只卡一次）。
	classifyHoldAfter int
	classifyGate      chan struct{}

	calls       []string
	organizes   []services.InboxOrganizeInput
	reviewed    [][]string
	acked       []string
	retried     []string
	sourceReqs  [][]string
	classifies  []ClassifyCall
	applied     [][]services.InboxAIAssignment
	indexRuns   int
	retryAbsent map[string]bool
}

var _ services.Inbox = (*Inbox)(nil)

// defaultCollection 是没有 SetAssignments 时归类计划写给每个条目的合集名。
const defaultCollection = "auto-collection"

// ClassifyCall 是收到的一次 Classify。
type ClassifyCall struct {
	Sources  []services.InboxAISource
	Existing []string
}

// ---- 种子 ----

// SetList 设置之后 List 返回的结果。
func (f *Inbox) SetList(list services.InboxList) {
	f.mu.Lock()
	f.list = cloneInboxList(list)
	f.mu.Unlock()
}

// SetWikiCandidates 设置之后 WikiCandidates 返回的各状态条数。
func (f *Inbox) SetWikiCandidates(c services.InboxWikiCandidates) {
	f.mu.Lock()
	f.wiki = c
	f.mu.Unlock()
}

// SetWikiModelReady 设置 Wiki 场景的文本模型是否就绪。
func (f *Inbox) SetWikiModelReady(ready bool) {
	f.mu.Lock()
	f.modelReady = ready
	f.mu.Unlock()
}

// SetSemanticStatus 设置之后 SemanticStatus 返回的语义索引进度。
func (f *Inbox) SetSemanticStatus(s services.InboxSemanticStatus) {
	f.mu.Lock()
	f.semantic = s
	f.mu.Unlock()
}

// SetSources 设置之后 AIClassificationSources 返回的摘要（不论传入哪些 ID）；不调用时按传入的 ID 各生成一条。
func (f *Inbox) SetSources(sources []services.InboxAISource) {
	f.mu.Lock()
	f.sources, f.sourcesSet = slices.Clone(sources), true
	f.mu.Unlock()
}

// SetAssignments 设置归类计划：条目 ID → 合集名；计划里没有的条目归入默认合集。
func (f *Inbox) SetAssignments(byItem map[string]string) {
	f.mu.Lock()
	f.assignments = make(map[string]string, len(byItem))
	for k, v := range byItem {
		f.assignments[k] = v
	}
	f.mu.Unlock()
}

// SetApplyResult 设置之后 ApplyAIClassification 返回的结果；不调用时按分类的条数返回。
func (f *Inbox) SetApplyResult(r services.InboxAIApplyResult) {
	f.mu.Lock()
	f.applyResult, f.applySet = r, true
	f.mu.Unlock()
}

// SetClassifyBatches 设置 Classify 回报的总批数（缺省按每 20 条一批）。
func (f *Inbox) SetClassifyBatches(total int) {
	f.mu.Lock()
	f.batches = total
	f.mu.Unlock()
}

// SetChanged 设置写入类方法返回的「改动条数」：method 取 "Organize"、"MarkReviewed"、"AcknowledgeImportWarning"。
// 缺省时 Organize 与 MarkReviewed 返回传入的条数，AcknowledgeImportWarning 返回 1。
func (f *Inbox) SetChanged(method string, n int) {
	f.mu.Lock()
	if f.changed == nil {
		f.changed = map[string]int{}
	}
	f.changed[method] = n
	f.mu.Unlock()
}

// SetSemanticNotice 设置之后 RunSemanticIndexing 返回的回执；传 nil 表示「已有一轮在跑、没有回执」。不调用时返回「没有需要索引的内容」。
func (f *Inbox) SetSemanticNotice(n *services.InboxSemanticNotice) {
	f.mu.Lock()
	f.notice, f.noticeSet = n, true
	f.mu.Unlock()
}

// SetSemanticIndexing 设定此刻是否有一轮语义索引在跑（SemanticIndexing 的返回值；RunSemanticIndexing 进行中也会置为真）。
func (f *Inbox) SetSemanticIndexing(on bool) {
	f.mu.Lock()
	f.indexing = on
	f.mu.Unlock()
}

// SetRetryAbsent 让某个任务的 RetryImportTask 返回 found=false（任务已不存在）。
func (f *Inbox) SetRetryAbsent(taskID string) {
	f.mu.Lock()
	if f.retryAbsent == nil {
		f.retryAbsent = map[string]bool{}
	}
	f.retryAbsent[taskID] = true
	f.mu.Unlock()
}

// ---- 错误注入与闸门 ----

// FailOn 让某个方法（"List"、"WikiCandidates"、"WikiModelReady"、"SemanticStatus"、"Organize"、"MarkReviewed"、
// "AcknowledgeImportWarning"、"RetryImportTask"、"AIClassificationSources"、"Classify"、"ApplyAIClassification"、
// "RunSemanticIndexing"）返回 err；err 为 nil 取消注入。写成 "方法:参数"（如 "RetryImportTask:t1"）时只让针对那个参数的调用失败。
func (f *Inbox) FailOn(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures == nil {
		f.failures = map[string]error{}
	}
	if err == nil {
		delete(f.failures, method)
		return
	}
	f.failures[method] = err
}

// Hold 让某个方法的下一次调用卡住（在读取种子与记录结果之前），直到返回的函数被调用（只卡一次）。
// 卡住期间 ctx 被取消，调用返回 ctx.Err()。
func (f *Inbox) Hold(method string) (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	if f.gates == nil {
		f.gates = map[string]chan struct{}{}
	}
	f.gates[method] = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// HoldClassifyAfter 让下一次 Classify 在回报了 batches 批进度之后卡住，直到返回的函数被调用（只卡一次）：
// 用来观察「归类进行到第几批」的进度，或在中途取消。
func (f *Inbox) HoldClassifyAfter(batches int) (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.classifyHoldAfter, f.classifyGate = batches, gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// ---- 调用记录 ----

// Calls 返回迄今的调用记录（"方法" 或 "方法:参数"），按发生顺序。
func (f *Inbox) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// CallCount 返回名为 method 的调用次数（不含参数部分）。
func (f *Inbox) CallCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == method || len(c) > len(method) && c[:len(method)] == method && c[len(method)] == ':' {
			n++
		}
	}
	return n
}

// OrganizeCalls 返回收到的批量整理请求。
func (f *Inbox) OrganizeCalls() []services.InboxOrganizeInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]services.InboxOrganizeInput, len(f.organizes))
	for i, in := range f.organizes {
		in.ItemIDs, in.AddTagNames = slices.Clone(in.ItemIDs), slices.Clone(in.AddTagNames)
		out[i] = in
	}
	return out
}

// ReviewedCalls 返回收到的「标记已复核」请求（每次的条目 ID）。
func (f *Inbox) ReviewedCalls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneStrings2(f.reviewed)
}

// AcknowledgedCalls 返回收到的「不再提醒」请求（任务 ID）。
func (f *Inbox) AcknowledgedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.acked)
}

// RetriedCalls 返回收到的采集任务重试请求（任务 ID）。
func (f *Inbox) RetriedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.retried)
}

// SourceRequests 返回 AIClassificationSources 收到的条目 ID 列表。
func (f *Inbox) SourceRequests() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneStrings2(f.sourceReqs)
}

// ClassifyCalls 返回收到的归类请求。
func (f *Inbox) ClassifyCalls() []ClassifyCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ClassifyCall, len(f.classifies))
	for i, c := range f.classifies {
		out[i] = ClassifyCall{Sources: slices.Clone(c.Sources), Existing: slices.Clone(c.Existing)}
	}
	return out
}

// AppliedCalls 返回收到的「应用归类计划」请求。
func (f *Inbox) AppliedCalls() [][]services.InboxAIAssignment {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]services.InboxAIAssignment, len(f.applied))
	for i, a := range f.applied {
		out[i] = slices.Clone(a)
	}
	return out
}

// IndexRuns 返回 RunSemanticIndexing 真正开始执行的次数（已有一轮在跑而直接返回的不算）。
func (f *Inbox) IndexRuns() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.indexRuns
}

// ---- 实现 ----

// begin 记一次调用，返回注入的失败与要等的闸门；mu 由调用方不持有。
func (f *Inbox) begin(call, method string) (gate chan struct{}, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	gate = f.gates[method]
	delete(f.gates, method)
	if e, ok := f.failures[call]; ok {
		return gate, e
	}
	return gate, f.failures[method]
}

// enter 是每个方法的开头：记调用、等闸门（可被 ctx 取消）、再返回注入的失败。
func (f *Inbox) enter(ctx context.Context, call, method string) error {
	gate, injected := f.begin(call, method)
	if err := waitGate(ctx, gate); err != nil {
		return err
	}
	return injected
}

// 读取类方法在收到请求时就取下种子的快照、再等闸门：被卡住的请求放行后返回的是「发出时」的数据，
// 与真实的慢响应一致——这样才能构造「旧请求晚到」的竞态。
func (f *Inbox) List(ctx context.Context) (services.InboxList, error) {
	f.mu.Lock()
	snapshot := cloneInboxList(f.list)
	f.mu.Unlock()
	if err := f.enter(ctx, "List", "List"); err != nil {
		return services.InboxList{}, err
	}
	return snapshot, nil
}

func (f *Inbox) WikiCandidates(ctx context.Context) (services.InboxWikiCandidates, error) {
	f.mu.Lock()
	snapshot := f.wiki
	f.mu.Unlock()
	if err := f.enter(ctx, "WikiCandidates", "WikiCandidates"); err != nil {
		return services.InboxWikiCandidates{}, err
	}
	return snapshot, nil
}

func (f *Inbox) WikiModelReady(ctx context.Context) (bool, error) {
	f.mu.Lock()
	snapshot := f.modelReady
	f.mu.Unlock()
	if err := f.enter(ctx, "WikiModelReady", "WikiModelReady"); err != nil {
		return false, err
	}
	return snapshot, nil
}

func (f *Inbox) SemanticStatus(ctx context.Context) (services.InboxSemanticStatus, error) {
	f.mu.Lock()
	snapshot := f.semantic
	f.mu.Unlock()
	if err := f.enter(ctx, "SemanticStatus", "SemanticStatus"); err != nil {
		return services.InboxSemanticStatus{}, err
	}
	return snapshot, nil
}

// changedOr 取写入类方法的返回值：种子优先，否则 def。mu 由调用方持有。
func (f *Inbox) changedOr(method string, def int) int {
	if n, ok := f.changed[method]; ok {
		return n
	}
	return def
}

func (f *Inbox) Organize(ctx context.Context, in services.InboxOrganizeInput) (int, error) {
	if err := f.enter(ctx, "Organize", "Organize"); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.organizes = append(f.organizes, in)
	return f.changedOr("Organize", len(in.ItemIDs)), nil
}

func (f *Inbox) MarkReviewed(ctx context.Context, itemIDs []string) (int, error) {
	if err := f.enter(ctx, "MarkReviewed", "MarkReviewed"); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reviewed = append(f.reviewed, slices.Clone(itemIDs))
	return f.changedOr("MarkReviewed", len(itemIDs)), nil
}

func (f *Inbox) AcknowledgeImportWarning(ctx context.Context, taskID string) (int, error) {
	if err := f.enter(ctx, "AcknowledgeImportWarning:"+taskID, "AcknowledgeImportWarning"); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acked = append(f.acked, taskID)
	return f.changedOr("AcknowledgeImportWarning", 1), nil
}

func (f *Inbox) RetryImportTask(ctx context.Context, taskID string) (bool, error) {
	if err := f.enter(ctx, "RetryImportTask:"+taskID, "RetryImportTask"); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retried = append(f.retried, taskID)
	return !f.retryAbsent[taskID], nil
}

func (f *Inbox) AIClassificationSources(ctx context.Context, itemIDs []string) ([]services.InboxAISource, error) {
	if err := f.enter(ctx, "AIClassificationSources", "AIClassificationSources"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sourceReqs = append(f.sourceReqs, slices.Clone(itemIDs))
	if f.sourcesSet {
		return slices.Clone(f.sources), nil
	}
	out := make([]services.InboxAISource, len(itemIDs))
	for i, id := range itemIDs {
		out[i] = services.InboxAISource{ItemID: id, Title: "title " + id, Excerpt: "excerpt " + id}
	}
	return out, nil
}

func (f *Inbox) Classify(ctx context.Context, sources []services.InboxAISource, existing []string, onProgress func(completed, total int)) ([]services.InboxAIAssignment, error) {
	if err := f.enter(ctx, "Classify", "Classify"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.classifies = append(f.classifies, ClassifyCall{Sources: slices.Clone(sources), Existing: slices.Clone(existing)})
	total := f.batches
	if total <= 0 {
		total = (len(sources) + 19) / 20
	}
	holdAfter, holdGate := f.classifyHoldAfter, f.classifyGate
	f.classifyHoldAfter, f.classifyGate = 0, nil
	plan := make(map[string]string, len(f.assignments))
	for k, v := range f.assignments {
		plan[k] = v
	}
	f.mu.Unlock()

	for done := 1; done <= total; done++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if onProgress != nil {
			onProgress(done, total)
		}
		if holdGate != nil && done == holdAfter {
			if err := waitGate(ctx, holdGate); err != nil {
				return nil, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]services.InboxAIAssignment, len(sources))
	for i, s := range sources {
		name, ok := plan[s.ItemID]
		if !ok {
			name = defaultCollection
		}
		out[i] = services.InboxAIAssignment{ItemID: s.ItemID, CollectionName: name}
	}
	return out, nil
}

func (f *Inbox) ApplyAIClassification(ctx context.Context, assignments []services.InboxAIAssignment) (services.InboxAIApplyResult, error) {
	if err := f.enter(ctx, "ApplyAIClassification", "ApplyAIClassification"); err != nil {
		return services.InboxAIApplyResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, slices.Clone(assignments))
	if f.applySet {
		r := f.applyResult
		r.CreatedCollectionNames = slices.Clone(r.CreatedCollectionNames)
		return r, nil
	}
	return services.InboxAIApplyResult{Classified: len(assignments), CreatedCollectionNames: []string{}}, nil
}

func (f *Inbox) SemanticIndexing() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.indexing
}

func (f *Inbox) RunSemanticIndexing(ctx context.Context) (*services.InboxSemanticNotice, error) {
	f.mu.Lock()
	if f.indexing {
		f.calls = append(f.calls, "RunSemanticIndexing")
		f.mu.Unlock()
		return nil, nil
	}
	f.indexing = true
	f.indexRuns++
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.indexing = false
		f.mu.Unlock()
	}()

	if err := f.enter(ctx, "RunSemanticIndexing", "RunSemanticIndexing"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.noticeSet {
		return &services.InboxSemanticNotice{Kind: services.InboxSemanticNothing}, nil
	}
	if f.notice == nil {
		return nil, nil
	}
	n := *f.notice
	return &n, nil
}

func cloneInboxList(l services.InboxList) services.InboxList {
	items := make([]services.InboxItem, len(l.Items))
	for i, it := range l.Items {
		it.Reasons = slices.Clone(it.Reasons)
		items[i] = it
	}
	l.Items = items
	return l
}

func cloneStrings2(in [][]string) [][]string {
	out := make([][]string, len(in))
	for i, s := range in {
		out[i] = slices.Clone(s)
	}
	return out
}
