package servicestest

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// ThemedFake 是 services.ThemedReading 的假实现：页面（当前版与上一版）、任务、模型与搜索状态存在内存里，
// 生成 / 取消 / 继续 / 重做图片 / 恢复上一版 / 删除的可见效果与真实处理器一致（同一份内容同时只有一个活动任务、
// 没有文本模型不能生成、要生图却没有生图模型被拒、旧格式页面不能调整、列表按最近更新倒序）。
//
// 任务由测试驱动：Generate 只建一个 queued 任务并推送，之后用 UpdateTask 推进度、CompleteTask 发布页面、FailTask 失败；
// 每次状态变化都会推给 OnProgress 的订阅者（与真实运行时的 themedReading:progress 一样是整份最新快照）。
// 每次调用都有记录，任意操作都能注入失败（Fail），也能让下一次调用卡住（Hold）以构造「请求晚到」的竞态。
//
// 零值可用：文本模型与生图模型都算已配置，搜索状态为「未配置」，导出走「用户取消」。假实现不含任何给用户看的文案：
// 标题、错误原因由测试摆放，或是英文占位。
type ThemedFake struct {
	mu    sync.Mutex
	clock int64
	seq   int

	pages    map[string]*themedRow
	tasks    []*services.ThemedTask
	taskOpts map[string]services.ThemedOptions

	textModel, imageModel *string
	modelsSet             bool
	search                services.ThemedSearchStatus
	exportPath            string

	subs    map[int]func(services.ThemedTask)
	nextSub int

	failOps map[string]error
	holds   map[string]*themedHold

	states      []services.ThemedRequest
	gets        []services.ThemedGetInput
	references  []services.ThemedRequest
	previews    []ThemedPreviewCall
	listCalls   int
	generates   []ThemedGenerateCall
	cancels     []string
	resumes     []string
	offlines    []string
	regenerates []ThemedRegenerateCall
	restores    []services.ThemedRequest
	removes     []services.ThemedRequest
	exports     []services.ThemedExportInput
	renders     []services.ThemedRenderInput
	searchCfgs  []string

	imagesOmitted bool
}

// ThemedPreviewCall 是一次 Preview 调用记录。
type ThemedPreviewCall struct {
	Req    services.ThemedRequest
	TaskID string
}

// ThemedGenerateCall 是一次 Generate 调用记录。
type ThemedGenerateCall struct {
	Req  services.ThemedRequest
	Opts services.ThemedOptions
}

// ThemedRegenerateCall 是一次 RegenerateAsset 调用记录。
type ThemedRegenerateCall struct {
	Req     services.ThemedRequest
	AssetID string
}

type themedRow struct {
	current  *services.ThemedPage
	previous *services.ThemedPage
	stale    bool
	document string
	refs     []services.ThemedReference
	preview  *services.ThemedPreview
}

type themedHold struct {
	arrived chan struct{}
	release chan struct{}
}

var _ services.ThemedReading = (*ThemedFake)(nil)

// 可注入失败、可卡住的操作名（Fail / Hold 的参数）。
const (
	ThemedOpState           = "state"
	ThemedOpGet             = "get"
	ThemedOpReferences      = "references"
	ThemedOpPreview         = "preview"
	ThemedOpListTasks       = "listTasks"
	ThemedOpGenerate        = "generate"
	ThemedOpCancel          = "cancel"
	ThemedOpResume          = "resume"
	ThemedOpContinueOffline = "continueOffline"
	ThemedOpRegenerateAsset = "regenerateAsset"
	ThemedOpRestorePrevious = "restorePrevious"
	ThemedOpRemove          = "remove"
	ThemedOpExport          = "export"
	ThemedOpRender          = "render"
	ThemedOpSearchStatus    = "searchStatus"
	ThemedOpConfigureSearch = "configureSearch"
)

func themedKey(req services.ThemedRequest) string { return req.ItemID + "|" + req.SourceKind }

func (f *ThemedFake) with(fn func()) {
	f.mu.Lock()
	fn()
	f.mu.Unlock()
}

func (f *ThemedFake) tick() int64 {
	f.clock += 1000
	return f.clock
}

// Fail 让某个操作持续失败（err 为 nil 取消）；失败时不改任何状态，调用仍会被记录。
func (f *ThemedFake) Fail(op string, err error) {
	f.with(func() {
		if f.failOps == nil {
			f.failOps = map[string]error{}
		}
		if err == nil {
			delete(f.failOps, op)
			return
		}
		f.failOps[op] = err
	})
}

// Hold 让某个操作的下一次调用卡在入口（只卡一次），直到 release 被调用；arrived 在那次调用到达入口时关闭。
func (f *ThemedFake) Hold(op string) (release func(), arrived <-chan struct{}) {
	h := &themedHold{arrived: make(chan struct{}), release: make(chan struct{})}
	f.with(func() {
		if f.holds == nil {
			f.holds = map[string]*themedHold{}
		}
		f.holds[op] = h
	})
	return func() { close(h.release) }, h.arrived
}

// gate 在操作入口处执行 Hold 设下的卡点。
func (f *ThemedFake) gate(op string) {
	f.mu.Lock()
	h := f.holds[op]
	delete(f.holds, op)
	f.mu.Unlock()
	if h != nil {
		close(h.arrived)
		<-h.release
	}
}

func (f *ThemedFake) failure(op string) error { return f.failOps[op] }

func (f *ThemedFake) row(req services.ThemedRequest) *themedRow {
	if f.pages == nil {
		f.pages = map[string]*themedRow{}
	}
	k := themedKey(req)
	if f.pages[k] == nil {
		f.pages[k] = &themedRow{}
	}
	return f.pages[k]
}

// ---- 摆放 ----

// PutPage 摆一份当前页面（不记为一次生成）：ID 为空取内部序号，FormatVersion 为 0 取 3，Role 取 current，
// ItemID / SourceKind 取 req，时间戳为 0 取内部递增时钟。返回落到库里的页面。
func (f *ThemedFake) PutPage(req services.ThemedRequest, p services.ThemedPage) services.ThemedPage {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	if p.ID == "" {
		p.ID = "version-" + strconv.Itoa(f.seq)
	}
	if p.FormatVersion == 0 {
		p.FormatVersion = 3
	}
	p.Role = services.ThemedRoleCurrent
	p.ItemID, p.SourceKind = req.ItemID, req.SourceKind
	if p.CreatedAt == 0 {
		p.CreatedAt = f.tick()
	}
	if p.UpdatedAt == 0 {
		p.UpdatedAt = p.CreatedAt
	}
	if p.Assets == nil {
		p.Assets = []services.ThemedAsset{}
	}
	if p.Warnings == nil {
		p.Warnings = []string{}
	}
	f.row(req).current = &p
	return p
}

// PutPrevious 摆一份上一版页面（Role 取 previous）。
func (f *ThemedFake) PutPrevious(req services.ThemedRequest, p services.ThemedPage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	if p.ID == "" {
		p.ID = "version-" + strconv.Itoa(f.seq)
	}
	if p.FormatVersion == 0 {
		p.FormatVersion = 3
	}
	p.Role = services.ThemedRolePrevious
	p.ItemID, p.SourceKind = req.ItemID, req.SourceKind
	if p.Assets == nil {
		p.Assets = []services.ThemedAsset{}
	}
	if p.Warnings == nil {
		p.Warnings = []string{}
	}
	f.row(req).previous = &p
}

// SetStale 设定这份内容的页面是否已过期（正文改过）。
func (f *ThemedFake) SetStale(req services.ThemedRequest, stale bool) {
	f.with(func() { f.row(req).stale = stale })
}

// SetDocument 设定旧格式页面的整页文档（Get 在非 MetadataOnly 且页面是旧格式时返回）。
func (f *ThemedFake) SetDocument(req services.ThemedRequest, doc string) {
	f.with(func() { f.row(req).document = doc })
}

// SetReferences 设定这份内容的参考资料。
func (f *ThemedFake) SetReferences(req services.ThemedRequest, refs ...services.ThemedReference) {
	f.with(func() { f.row(req).refs = slices.Clone(refs) })
}

// SetPreview 设定这份内容生成途中的静态预览（nil 表示没有可预览的章节）。
func (f *ThemedFake) SetPreview(req services.ThemedRequest, p *services.ThemedPreview) {
	f.with(func() { f.row(req).preview = p })
}

// SetModels 设定模型名；空串表示没有配置。
func (f *ThemedFake) SetModels(text, image string) {
	f.with(func() {
		f.modelsSet = true
		f.textModel, f.imageModel = nil, nil
		if text != "" {
			f.textModel = &text
		}
		if image != "" {
			f.imageModel = &image
		}
	})
}

// SetSearch 设定联网搜索状态。
func (f *ThemedFake) SetSearch(s services.ThemedSearchStatus) {
	f.with(func() { f.search = s })
}

// SetExportPath 设定导出写出的路径；空串表示用户在保存对话框里取消。
func (f *ThemedFake) SetExportPath(path string) {
	f.with(func() { f.exportPath = path })
}

// PutTask 直接摆一个任务（不推送）：ID 为空取内部序号，时间戳为 0 取内部递增时钟，State 为空取 completed。
func (f *ThemedFake) PutTask(t services.ThemedTask) services.ThemedTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	if t.ID == "" {
		t.ID = "task-" + strconv.Itoa(f.seq)
	}
	if t.State == "" {
		t.State = services.ThemedTaskCompleted
	}
	if t.Stage == "" {
		t.Stage = services.ThemedStageDone
	}
	if t.CreatedAt == 0 {
		t.CreatedAt = f.tick()
	}
	if t.UpdatedAt == 0 {
		t.UpdatedAt = t.CreatedAt
	}
	c := t
	f.tasks = append(f.tasks, &c)
	return t
}

// ---- 驱动任务 ----

func (f *ThemedFake) findTask(id string) *services.ThemedTask {
	for _, t := range f.tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// publish 把任务的最新快照推给全部订阅者（在锁外同步回调，与真实推送一样在推送方 goroutine 上执行）。
func (f *ThemedFake) publish(t services.ThemedTask) {
	f.mu.Lock()
	fns := make([]func(services.ThemedTask), 0, len(f.subs))
	for _, fn := range f.subs {
		fns = append(fns, fn)
	}
	f.mu.Unlock()
	for _, fn := range fns {
		fn(t)
	}
}

// UpdateTask 修改一个任务并推送最新快照（UpdatedAt 随内部时钟前进）；任务不存在时返回 false。
func (f *ThemedFake) UpdateTask(id string, mutate func(*services.ThemedTask)) bool {
	f.mu.Lock()
	t := f.findTask(id)
	if t == nil {
		f.mu.Unlock()
		return false
	}
	mutate(t)
	t.UpdatedAt = f.tick()
	snap := *t
	f.mu.Unlock()
	f.publish(snap)
	return true
}

// CompleteTask 让任务完成并发布页面：页面取生成时的选项，当前页退为上一版；替换图片的任务只把那一张图置为 ready。
// 任务不存在时返回 false。
func (f *ThemedFake) CompleteTask(id string) bool {
	f.mu.Lock()
	t := f.findTask(id)
	if t == nil {
		f.mu.Unlock()
		return false
	}
	req := services.ThemedRequest{ItemID: t.ItemID, SourceKind: t.SourceKind}
	row := f.row(req)
	f.seq++
	if t.AssetID != "" && row.current != nil {
		page := *row.current
		page.Assets = slices.Clone(page.Assets)
		for i, a := range page.Assets {
			if a.ID == t.AssetID {
				a.Status, a.Error, a.FileName = services.ThemedAssetReady, "", "asset-"+strconv.Itoa(f.seq)+".png"
				page.Assets[i] = a
			}
		}
		page.UpdatedAt = f.tick()
		row.current = &page
	} else {
		page := services.ThemedPage{
			ID: "version-" + strconv.Itoa(f.seq), ItemID: t.ItemID, SourceKind: t.SourceKind, Role: services.ThemedRoleCurrent,
			FormatVersion: 3, Options: f.taskOpts[id], Assets: []services.ThemedAsset{}, Warnings: []string{},
			Source: services.ThemedSource{Title: t.Title}, CreatedAt: f.tick(),
		}
		page.UpdatedAt = page.CreatedAt
		if row.current != nil {
			prev := *row.current
			prev.Role = services.ThemedRolePrevious
			row.previous = &prev
		}
		row.current = &page
		row.stale = false
		t.VersionID = page.ID
	}
	t.State, t.Stage = services.ThemedTaskCompleted, services.ThemedStageDone
	t.Error = ""
	t.UpdatedAt = f.tick()
	snap := *t
	f.mu.Unlock()
	f.publish(snap)
	return true
}

// FailTask 让任务失败并带上原因（原因由测试给出，界面原样显示）。任务不存在时返回 false。
func (f *ThemedFake) FailTask(id, reason string) bool {
	return f.UpdateTask(id, func(t *services.ThemedTask) {
		t.State, t.Error = services.ThemedTaskFailed, reason
	})
}

// ---- 读取 ----

func (f *ThemedFake) latestTask(req services.ThemedRequest) *services.ThemedTask {
	var best *services.ThemedTask
	for _, t := range f.tasks {
		if t.ItemID != req.ItemID || t.SourceKind != req.SourceKind {
			continue
		}
		if best == nil || t.CreatedAt > best.CreatedAt || (t.CreatedAt == best.CreatedAt && t.ID > best.ID) {
			best = t
		}
	}
	if best == nil {
		return nil
	}
	c := *best
	return &c
}

func (f *ThemedFake) State(ctx context.Context, req services.ThemedRequest) (services.ThemedState, error) {
	f.with(func() { f.states = append(f.states, req) })
	f.gate(ThemedOpState)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(ThemedOpState); err != nil {
		return services.ThemedState{}, err
	}
	if err := ctx.Err(); err != nil {
		return services.ThemedState{}, err
	}
	row := f.row(req)
	st := services.ThemedState{Task: f.latestTask(req)}
	if row.current != nil {
		st.HasPage, st.VersionID, st.FormatVersion, st.Stale = true, row.current.ID, row.current.FormatVersion, row.stale
	}
	return st, nil
}

func (f *ThemedFake) models() services.ThemedModels {
	if !f.modelsSet {
		text, image := "fake-text-model", "fake-image-model"
		return services.ThemedModels{Text: &text, Image: &image}
	}
	return services.ThemedModels{Text: f.textModel, Image: f.imageModel}
}

func (f *ThemedFake) Get(ctx context.Context, in services.ThemedGetInput) (services.ThemedReadResult, error) {
	f.with(func() { f.gets = append(f.gets, in) })
	f.gate(ThemedOpGet)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(ThemedOpGet); err != nil {
		return services.ThemedReadResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return services.ThemedReadResult{}, err
	}
	row := f.row(in.ThemedRequest)
	search := f.search
	out := services.ThemedReadResult{
		Previous: row.previous != nil, Task: f.latestTask(in.ThemedRequest), Models: f.models(), Search: &search,
	}
	if row.current != nil {
		page := *row.current
		out.Page = &page
		out.Stale = row.stale
		if page.Legacy() && !in.MetadataOnly {
			out.Document = row.document
		}
	}
	return out, nil
}

func (f *ThemedFake) References(ctx context.Context, req services.ThemedRequest) ([]services.ThemedReference, error) {
	f.with(func() { f.references = append(f.references, req) })
	f.gate(ThemedOpReferences)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(ThemedOpReferences); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]services.ThemedReference{}, f.row(req).refs...), nil
}

func (f *ThemedFake) Preview(ctx context.Context, req services.ThemedRequest, taskID string) (*services.ThemedPreview, error) {
	f.with(func() { f.previews = append(f.previews, ThemedPreviewCall{Req: req, TaskID: taskID}) })
	f.gate(ThemedOpPreview)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(ThemedOpPreview); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t := f.findTask(taskID)
	if t == nil || t.ItemID != req.ItemID || t.SourceKind != req.SourceKind {
		return nil, errors.New("the task does not belong to this article")
	}
	if p := f.row(req).preview; p != nil {
		c := *p
		return &c, nil
	}
	return nil, nil
}

func (f *ThemedFake) ListTasks(ctx context.Context) ([]services.ThemedTask, error) {
	f.with(func() { f.listCalls++ })
	f.gate(ThemedOpListTasks)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(ThemedOpListTasks); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]services.ThemedTask, len(f.tasks))
	for i, t := range f.tasks {
		out[i] = *t
	}
	slices.SortStableFunc(out, func(a, b services.ThemedTask) int {
		if a.UpdatedAt != b.UpdatedAt {
			if a.UpdatedAt > b.UpdatedAt {
				return -1
			}
			return 1
		}
		return -strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (f *ThemedFake) OnProgress(fn func(services.ThemedTask)) func() {
	f.mu.Lock()
	if f.subs == nil {
		f.subs = map[int]func(services.ThemedTask){}
	}
	f.nextSub++
	id := f.nextSub
	f.subs[id] = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		delete(f.subs, id)
		f.mu.Unlock()
	}
}

// Subscribers 返回当前订阅 OnProgress 的数量（测试用来确认退订）。
func (f *ThemedFake) Subscribers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs)
}

// ---- 生成与任务控制 ----

func (f *ThemedFake) activeFor(req services.ThemedRequest) bool {
	for _, t := range f.tasks {
		if t.ItemID == req.ItemID && t.SourceKind == req.SourceKind && t.Active() {
			return true
		}
	}
	return false
}

// newTask 建一个 queued 任务（调用方持锁）；Title 取条目标识，旧任务不会被覆盖。
func (f *ThemedFake) newTask(req services.ThemedRequest, opts services.ThemedOptions, assetID string) services.ThemedTask {
	f.seq++
	now := f.tick()
	t := &services.ThemedTask{
		ID: "task-" + strconv.Itoa(f.seq), ItemID: req.ItemID, SourceKind: req.SourceKind, Title: req.ItemID,
		State: services.ThemedTaskQueued, Stage: services.ThemedStagePrepare, AssetID: assetID, CreatedAt: now, UpdatedAt: now,
	}
	if f.taskOpts == nil {
		f.taskOpts = map[string]services.ThemedOptions{}
	}
	f.taskOpts[t.ID] = opts
	f.tasks = append(f.tasks, t)
	return *t
}

func (f *ThemedFake) Generate(ctx context.Context, req services.ThemedRequest, opts services.ThemedOptions) (services.ThemedTask, error) {
	f.with(func() { f.generates = append(f.generates, ThemedGenerateCall{Req: req, Opts: opts}) })
	f.gate(ThemedOpGenerate)
	f.mu.Lock()
	if err := f.failure(ThemedOpGenerate); err != nil {
		f.mu.Unlock()
		return services.ThemedTask{}, err
	}
	if err := ctx.Err(); err != nil {
		f.mu.Unlock()
		return services.ThemedTask{}, err
	}
	m := f.models()
	switch {
	case f.activeFor(req):
		f.mu.Unlock()
		return services.ThemedTask{}, errors.New("a theme task is already active for this item")
	case m.Text == nil:
		f.mu.Unlock()
		return services.ThemedTask{}, errors.New("text model is not configured")
	case opts.GenerateImages && opts.MaxImages < 1:
		f.mu.Unlock()
		return services.ThemedTask{}, errors.New("image limit must be at least one")
	case opts.GenerateImages && m.Image == nil:
		f.mu.Unlock()
		return services.ThemedTask{}, errors.New("image model is not configured")
	}
	if cur := f.row(req).current; cur != nil && cur.Legacy() && (opts.Action == services.ThemedActionRevise || opts.Action == services.ThemedActionRedesign || opts.FromCurrent) {
		f.mu.Unlock()
		return services.ThemedTask{}, errors.New("legacy pages are read-only")
	}
	t := f.newTask(req, opts, "")
	f.mu.Unlock()
	f.publish(t)
	return t, nil
}

// control 是取消 / 继续 / 不联网继续的公共入口：找到任务、检查前置，再由 apply 改状态。
func (f *ThemedFake) control(ctx context.Context, op string, record *[]string, id string, apply func(t *services.ThemedTask) (changed bool, err error)) (services.ThemedTask, error) {
	f.with(func() { *record = append(*record, id) })
	f.gate(op)
	f.mu.Lock()
	if err := f.failure(op); err != nil {
		f.mu.Unlock()
		return services.ThemedTask{}, err
	}
	if err := ctx.Err(); err != nil {
		f.mu.Unlock()
		return services.ThemedTask{}, err
	}
	t := f.findTask(id)
	if t == nil {
		f.mu.Unlock()
		return services.ThemedTask{}, errors.New("task not found")
	}
	changed, err := apply(t)
	if err != nil {
		f.mu.Unlock()
		return services.ThemedTask{}, err
	}
	if changed {
		t.UpdatedAt = f.tick()
	}
	snap := *t
	f.mu.Unlock()
	if changed {
		f.publish(snap)
	}
	return snap, nil
}

func (f *ThemedFake) Cancel(ctx context.Context, taskID string) (services.ThemedTask, error) {
	return f.control(ctx, ThemedOpCancel, &f.cancels, taskID, func(t *services.ThemedTask) (bool, error) {
		if !t.Active() {
			return false, nil
		}
		t.State, t.Error = services.ThemedTaskCancelled, "cancelled"
		return true, nil
	})
}

func (f *ThemedFake) Resume(ctx context.Context, taskID string) (services.ThemedTask, error) {
	return f.control(ctx, ThemedOpResume, &f.resumes, taskID, func(t *services.ThemedTask) (bool, error) {
		switch {
		case t.LegacyFormat:
			return false, errors.New("legacy pages are read-only")
		case t.Active():
			return false, nil // 与真实运行时一致：活动任务原样返回，不报错
		case f.activeFor(services.ThemedRequest{ItemID: t.ItemID, SourceKind: t.SourceKind}):
			return false, errors.New("a theme task is already active for this item")
		}
		t.State, t.Error = services.ThemedTaskQueued, ""
		return true, nil
	})
}

func (f *ThemedFake) ContinueOffline(ctx context.Context, taskID string) (services.ThemedTask, error) {
	return f.control(ctx, ThemedOpContinueOffline, &f.offlines, taskID, func(t *services.ThemedTask) (bool, error) {
		if t.Stage != services.ThemedStageResearch || t.Active() {
			return false, errors.New("the task is not paused in research")
		}
		t.State, t.Error = services.ThemedTaskQueued, ""
		return true, nil
	})
}
func (f *ThemedFake) RegenerateAsset(ctx context.Context, req services.ThemedRequest, assetID string) (services.ThemedTask, error) {
	f.with(func() { f.regenerates = append(f.regenerates, ThemedRegenerateCall{Req: req, AssetID: assetID}) })
	f.gate(ThemedOpRegenerateAsset)
	f.mu.Lock()
	if err := f.failure(ThemedOpRegenerateAsset); err != nil {
		f.mu.Unlock()
		return services.ThemedTask{}, err
	}
	if err := ctx.Err(); err != nil {
		f.mu.Unlock()
		return services.ThemedTask{}, err
	}
	cur := f.row(req).current
	switch {
	case cur == nil || !slices.ContainsFunc(cur.Assets, func(a services.ThemedAsset) bool { return a.ID == assetID }):
		f.mu.Unlock()
		return services.ThemedTask{}, errors.New("the theme page or the image does not exist")
	case cur.Legacy():
		f.mu.Unlock()
		return services.ThemedTask{}, errors.New("legacy pages are read-only")
	case f.activeFor(req):
		f.mu.Unlock()
		return services.ThemedTask{}, errors.New("a theme task is already active for this item")
	}
	opts := cur.Options
	opts.FromCurrent, opts.GenerateImages = true, false
	t := f.newTask(req, opts, assetID)
	f.mu.Unlock()
	f.publish(t)
	return t, nil
}

func (f *ThemedFake) RestorePrevious(ctx context.Context, req services.ThemedRequest) error {
	f.with(func() { f.restores = append(f.restores, req) })
	f.gate(ThemedOpRestorePrevious)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(ThemedOpRestorePrevious); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	row := f.row(req)
	if row.previous == nil {
		return errors.New("there is no previous version")
	}
	cur := row.current
	restored := *row.previous
	restored.Role = services.ThemedRoleCurrent
	row.current = &restored
	row.previous = nil
	if cur != nil {
		prev := *cur
		prev.Role = services.ThemedRolePrevious
		row.previous = &prev
	}
	return nil
}

func (f *ThemedFake) Remove(ctx context.Context, req services.ThemedRequest) error {
	f.with(func() { f.removes = append(f.removes, req) })
	f.gate(ThemedOpRemove)
	f.mu.Lock()
	if err := f.failure(ThemedOpRemove); err != nil {
		f.mu.Unlock()
		return err
	}
	if err := ctx.Err(); err != nil {
		f.mu.Unlock()
		return err
	}
	var cancelled []services.ThemedTask
	for _, t := range f.tasks {
		if t.ItemID == req.ItemID && t.SourceKind == req.SourceKind && t.Active() {
			t.State, t.Error, t.UpdatedAt = services.ThemedTaskCancelled, "cancelled", f.tick()
			cancelled = append(cancelled, *t)
		}
	}
	row := f.row(req)
	row.current, row.previous, row.stale = nil, nil, false
	f.mu.Unlock()
	for _, t := range cancelled {
		f.publish(t)
	}
	return nil
}

func (f *ThemedFake) ExportHTML(ctx context.Context, in services.ThemedExportInput) (services.ThemedExportResult, error) {
	f.with(func() { f.exports = append(f.exports, in) })
	f.gate(ThemedOpExport)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(ThemedOpExport); err != nil {
		return services.ThemedExportResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return services.ThemedExportResult{}, err
	}
	if f.row(in.ThemedRequest).current == nil {
		return services.ThemedExportResult{}, errors.New("there is no theme page to export")
	}
	if f.exportPath == "" {
		return services.ThemedExportResult{Cancelled: true}, nil
	}
	return services.ThemedExportResult{Path: f.exportPath}, nil
}

// SetImagesOmitted 让之后的 Render 都带「图片未放入」标记（素材还没就绪时的情形）。
func (f *ThemedFake) SetImagesOmitted(omitted bool) { f.with(func() { f.imagesOmitted = omitted }) }

// Render 返回一份确定的假文档：正文里写着渲染的版本（或预览的版本号）与实例标识，测试据此认出是哪一份。
func (f *ThemedFake) Render(ctx context.Context, in services.ThemedRenderInput) (services.ThemedRender, error) {
	f.with(func() { f.renders = append(f.renders, in) })
	f.gate(ThemedOpRender)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(ThemedOpRender); err != nil {
		return services.ThemedRender{}, err
	}
	if err := ctx.Err(); err != nil {
		return services.ThemedRender{}, err
	}
	row := f.row(in.ThemedRequest)
	if in.TaskID != "" {
		t := f.findTask(in.TaskID)
		if t == nil || t.ItemID != in.ItemID || t.SourceKind != in.SourceKind {
			return services.ThemedRender{}, errors.New("the task does not belong to this article")
		}
		if row.preview == nil {
			return services.ThemedRender{}, errors.New("there is no section to preview")
		}
		return services.ThemedRender{
			Document:  "<html>preview " + row.preview.Page.ID + " rev " + strconv.Itoa(row.preview.Revision) + "</html>",
			VersionID: row.preview.Page.ID, ImagesOmitted: f.imagesOmitted,
		}, nil
	}
	page := row.current
	if in.VersionID != "" && row.previous != nil && row.previous.ID == in.VersionID {
		page = row.previous
	}
	if page == nil {
		return services.ThemedRender{}, errors.New("there is no theme page to render")
	}
	return services.ThemedRender{
		Document: "<html>page " + page.ID + " instance " + in.InstanceID + "</html>", VersionID: page.ID, ImagesOmitted: f.imagesOmitted,
	}, nil
}

// ---- 搜索设置 ----

func (f *ThemedFake) SearchStatus(ctx context.Context) (services.ThemedSearchStatus, error) {
	f.gate(ThemedOpSearchStatus)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(ThemedOpSearchStatus); err != nil {
		return services.ThemedSearchStatus{}, err
	}
	if err := ctx.Err(); err != nil {
		return services.ThemedSearchStatus{}, err
	}
	return f.search, nil
}

// ConfigureSearch 改搜索状态：Provider 非空时切换，APIKey 非空时该服务商算已配置、指向空串时清除，DefaultEnabled 非 nil 时覆盖；
// 记录里只存隐去密钥的文本。
func (f *ThemedFake) ConfigureSearch(ctx context.Context, in services.ThemedSearchConfigInput) (services.ThemedSearchStatus, error) {
	f.with(func() { f.searchCfgs = append(f.searchCfgs, in.String()) })
	f.gate(ThemedOpConfigureSearch)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(ThemedOpConfigureSearch); err != nil {
		return services.ThemedSearchStatus{}, err
	}
	if err := ctx.Err(); err != nil {
		return services.ThemedSearchStatus{}, err
	}
	s := f.search
	s.ConfiguredProviders = slices.Clone(s.ConfiguredProviders)
	if in.Provider != "" {
		s.Provider = in.Provider
	}
	if in.APIKey != nil && s.Provider != "" {
		if *in.APIKey == "" {
			s.ConfiguredProviders = slices.DeleteFunc(s.ConfiguredProviders, func(p string) bool { return p == s.Provider })
		} else if !slices.Contains(s.ConfiguredProviders, s.Provider) {
			s.ConfiguredProviders = append(s.ConfiguredProviders, s.Provider)
		}
	}
	if in.DefaultEnabled != nil {
		s.DefaultEnabled = *in.DefaultEnabled
	}
	s.Configured = s.Provider == "native" && s.NativeModel != "" || slices.Contains(s.ConfiguredProviders, s.Provider)
	f.search = s
	return s, nil
}

// ---- 调用记录 ----

// ThemedCalls 是各操作的调用次数与参数快照。
type ThemedCalls struct {
	States      []services.ThemedRequest
	Gets        []services.ThemedGetInput
	References  []services.ThemedRequest
	Previews    []ThemedPreviewCall
	ListTasks   int
	Generates   []ThemedGenerateCall
	Cancels     []string
	Resumes     []string
	Offlines    []string
	Regenerates []ThemedRegenerateCall
	Restores    []services.ThemedRequest
	Removes     []services.ThemedRequest
	Exports     []services.ThemedExportInput
	Renders     []services.ThemedRenderInput
	// SearchConfigs 是 ConfigureSearch 的入参文本（已隐去密钥）。
	SearchConfigs []string
}

// Calls 返回调用记录的快照。
func (f *ThemedFake) Calls() ThemedCalls {
	f.mu.Lock()
	defer f.mu.Unlock()
	return ThemedCalls{
		States: slices.Clone(f.states), Gets: slices.Clone(f.gets), References: slices.Clone(f.references),
		Previews: slices.Clone(f.previews), ListTasks: f.listCalls, Generates: slices.Clone(f.generates),
		Cancels: slices.Clone(f.cancels), Resumes: slices.Clone(f.resumes), Offlines: slices.Clone(f.offlines),
		Regenerates: slices.Clone(f.regenerates), Restores: slices.Clone(f.restores), Removes: slices.Clone(f.removes),
		Exports: slices.Clone(f.exports), Renders: slices.Clone(f.renders), SearchConfigs: slices.Clone(f.searchCfgs),
	}
}

// Task 返回一个任务的当前快照。
func (f *ThemedFake) Task(id string) (services.ThemedTask, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t := f.findTask(id); t != nil {
		return *t, true
	}
	return services.ThemedTask{}, false
}
