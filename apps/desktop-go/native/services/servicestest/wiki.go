package servicestest

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// WikiFake 是 services.Wiki 的假实现：页面、链接、历史版本、来源与更新建议存在内存里，目录、反向链接计数、搜索、图谱、
// 保存与删除的可见效果与真实处理器一致（保存前存一份历史版本、手动编辑标记、出链按目标页规范化标题重建）；
// 全文编译由脚本驱动（见 wiki_compile.go：可推送进度、可卡住、可取消、可注入错误）。每次调用都有记录，
// 任意操作都能注入失败（Fail），也能让下一次调用卡住（Hold）以构造「请求晚到」的竞态。
//
// 零值可用：Wiki 模型算已配置，没有脚本时编译一步完成。假实现不含任何给用户看的文案：
// 标题、正文、错误原因都由测试摆放，或是英文占位。
type WikiFake struct {
	mu     sync.Mutex
	clock  int64
	revSeq int

	pages map[string]*wikiRow
	// links 是有向边：源页面 → 目标页面集合。
	links map[string]map[string]bool

	compiled, eligible int

	failOps map[string]error
	holds   map[string]*wikiHold

	gets      []string
	searches  []WikiSearchCall
	updates   []services.WikiUpdatePageInput
	deletes   []string
	restores  []string
	accepts   []WikiAcceptCall
	catalogs  int
	listCalls []string
	graphs    int

	compile wikiCompileState
}

// WikiSearchCall 是一次 Search 调用记录。
type WikiSearchCall struct {
	Query string
	Limit int
}

// WikiAcceptCall 是一次 AcceptSuggestion 调用记录。
type WikiAcceptCall struct {
	PageID string
	Token  string
}

type wikiRow struct {
	page      services.WikiPage
	sources   []services.WikiPageSource
	revisions []services.WikiRevision // 新的在前
	sugg      *services.WikiSuggestion
}

type wikiHold struct {
	arrived chan struct{}
	release chan struct{}
}

var _ services.Wiki = (*WikiFake)(nil)

// 可注入失败、可卡住的操作名（Fail / Hold 的参数）。
const (
	WikiOpCatalog       = "catalog"
	WikiOpBacklinks     = "backlinks"
	WikiOpStatus        = "status"
	WikiOpSearch        = "search"
	WikiOpGetPage       = "getPage"
	WikiOpListRevisions = "listRevisions"
	WikiOpGraph         = "graph"
	WikiOpUpdatePage    = "updatePage"
	WikiOpDeletePage    = "deletePage"
	WikiOpRestore       = "restore"
	WikiOpConfigured    = "configured"
	WikiOpPreview       = "preview"
	WikiOpJobs          = "jobs"
	WikiOpStart         = "start"
	WikiOpRun           = "run"
	WikiOpControl       = "control"
	WikiOpSuggestion    = "suggestion"
	WikiOpAccept        = "accept"
)

func (f *WikiFake) with(fn func()) {
	f.mu.Lock()
	fn()
	f.mu.Unlock()
}

func (f *WikiFake) tick() int64 {
	f.clock += 1000
	return f.clock
}

// Fail 让某个操作持续失败（err 为 nil 取消）；失败时不改任何状态，调用仍会被记录。
func (f *WikiFake) Fail(op string, err error) {
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
func (f *WikiFake) Hold(op string) (release func(), arrived <-chan struct{}) {
	h := &wikiHold{arrived: make(chan struct{}), release: make(chan struct{})}
	f.with(func() {
		if f.holds == nil {
			f.holds = map[string]*wikiHold{}
		}
		f.holds[op] = h
	})
	return func() { close(h.release) }, h.arrived
}

// gate 在操作入口处执行 Hold 设下的卡点。
func (f *WikiFake) gate(op string) {
	f.mu.Lock()
	h := f.holds[op]
	delete(f.holds, op)
	f.mu.Unlock()
	if h != nil {
		close(h.arrived)
		<-h.release
	}
}

func (f *WikiFake) failure(op string) error { return f.failOps[op] }

// ---- 摆放 ----

// PutPage 摆一页（不记为一次保存）：UpdatedAt / GeneratedAt / CreatedAt 为 0 时取内部递增时钟，
// NormalizedTitle 为空时取大写的标题，PromptVersion 为空时取当前编译器版本。返回落到库里的页面。
func (f *WikiFake) PutPage(p services.WikiPage) services.WikiPage {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.UpdatedAt == 0 {
		p.UpdatedAt = f.tick()
	}
	if p.GeneratedAt == 0 {
		p.GeneratedAt = p.UpdatedAt
	}
	if p.CreatedAt == 0 {
		p.CreatedAt = p.UpdatedAt
	}
	if p.NormalizedTitle == "" {
		p.NormalizedTitle = strings.ToUpper(strings.TrimSpace(p.Title))
	}
	if p.Kind == "" {
		p.Kind = services.WikiKindTopic
	}
	if p.PromptVersion == "" {
		p.PromptVersion = services.WikiCompilerVersion
	}
	if f.pages == nil {
		f.pages = map[string]*wikiRow{}
	}
	if row := f.pages[p.ID]; row != nil {
		row.page = p
	} else {
		f.pages[p.ID] = &wikiRow{page: p}
	}
	return p
}

// PutSources 设置一页的来源条目。
func (f *WikiFake) PutSources(pageID string, sources ...services.WikiPageSource) {
	f.with(func() {
		if row := f.pages[pageID]; row != nil {
			row.sources = slices.Clone(sources)
		}
	})
}

// PutLink 摆一条有向边（from 引用 to）。
func (f *WikiFake) PutLink(from, to string) {
	f.with(func() {
		if f.links == nil {
			f.links = map[string]map[string]bool{}
		}
		if f.links[from] == nil {
			f.links[from] = map[string]bool{}
		}
		f.links[from][to] = true
	})
}

// PutRevision 摆一份历史版本（新的在前：后摆的排在前面）；CreatedAt 为 0 时取内部递增时钟。
func (f *WikiFake) PutRevision(rev services.WikiRevision) {
	f.with(func() {
		if rev.CreatedAt == 0 {
			rev.CreatedAt = f.tick()
		}
		if rev.ID == "" {
			f.revSeq++
			rev.ID = rev.PageID + "-rev-" + strconv.Itoa(f.revSeq)
		}
		if row := f.pages[rev.PageID]; row != nil {
			row.revisions = append([]services.WikiRevision{rev}, row.revisions...)
		}
	})
}

// PutSuggestion 摆一页的更新建议（nil 表示清掉）。
func (f *WikiFake) PutSuggestion(pageID string, s *services.WikiSuggestion) {
	f.with(func() {
		if row := f.pages[pageID]; row != nil {
			row.sugg = s
		}
	})
}

// SetStatusCounts 设定状态行里的「已编译条目 / 可编译条目」（页数取实际页面数）。
func (f *WikiFake) SetStatusCounts(compiled, eligible int) {
	f.with(func() { f.compiled, f.eligible = compiled, eligible })
}

// ---- 读取实际状态（不记录调用） ----

// Page 读出一页；没有返回 false。
func (f *WikiFake) Page(id string) (p services.WikiPage, ok bool) {
	f.with(func() {
		if row := f.pages[id]; row != nil {
			p, ok = row.page, true
		}
	})
	return p, ok
}

// PageCount 是库里的页面数。
func (f *WikiFake) PageCount() (n int) { f.with(func() { n = len(f.pages) }); return n }

// Revisions 读出一页的历史版本（新的在前）。
func (f *WikiFake) Revisions(pageID string) (out []services.WikiRevision) {
	f.with(func() {
		if row := f.pages[pageID]; row != nil {
			out = slices.Clone(row.revisions)
		}
	})
	return out
}

// OutLinks 读出一页引用了哪些页面（按 ID 排序）。
func (f *WikiFake) OutLinks(pageID string) (out []string) {
	f.with(func() {
		for to := range f.links[pageID] {
			out = append(out, to)
		}
	})
	slices.Sort(out)
	return out
}

// ---- 调用记录 ----

// Gets 是读取过的页面 ID，按发生顺序。
func (f *WikiFake) Gets() (out []string) { f.with(func() { out = slices.Clone(f.gets) }); return out }

// Searches 是收到的搜索请求，按发生顺序。
func (f *WikiFake) Searches() (out []WikiSearchCall) {
	f.with(func() { out = slices.Clone(f.searches) })
	return out
}

// Updates 是收到的保存请求（含被注入失败拦下的），按发生顺序。
func (f *WikiFake) Updates() (out []services.WikiUpdatePageInput) {
	f.with(func() { out = slices.Clone(f.updates) })
	return out
}

// Deletes 是收到的删除请求（页面 ID），按发生顺序。
func (f *WikiFake) Deletes() (out []string) {
	f.with(func() { out = slices.Clone(f.deletes) })
	return out
}

// Restores 是收到的回滚请求（版本 ID），按发生顺序。
func (f *WikiFake) Restores() (out []string) {
	f.with(func() { out = slices.Clone(f.restores) })
	return out
}

// Accepts 是收到的采用建议请求，按发生顺序。
func (f *WikiFake) Accepts() (out []WikiAcceptCall) {
	f.with(func() { out = slices.Clone(f.accepts) })
	return out
}

// CatalogCalls 是目录被读取的次数。
func (f *WikiFake) CatalogCalls() (n int) { f.with(func() { n = f.catalogs }); return n }

// GraphCalls 是图谱被读取的次数。
func (f *WikiFake) GraphCalls() (n int) { f.with(func() { n = f.graphs }); return n }

// RevisionLists 是读取过历史版本的页面 ID，按发生顺序。
func (f *WikiFake) RevisionLists() (out []string) {
	f.with(func() { out = slices.Clone(f.listCalls) })
	return out
}

// ---- 读取接口 ----

func catalogEntryOf(p services.WikiPage) services.WikiCatalogEntry {
	return services.WikiCatalogEntry{
		ID: p.ID, Title: p.Title, NormalizedTitle: p.NormalizedTitle, Kind: p.Kind, Summary: p.Summary,
		AliasesJSON: p.AliasesJSON, ManualEditedAt: p.ManualEditedAt, UpdatedAt: p.UpdatedAt,
	}
}

// byUpdated 是目录顺序：最近更新在前，同刻按 ID 稳定排序。
func byUpdated(a, b services.WikiPage) int {
	switch {
	case a.UpdatedAt != b.UpdatedAt:
		if a.UpdatedAt > b.UpdatedAt {
			return -1
		}
		return 1
	}
	return strings.Compare(a.ID, b.ID)
}

func (f *WikiFake) orderedLocked() []services.WikiPage {
	out := make([]services.WikiPage, 0, len(f.pages))
	for _, row := range f.pages {
		out = append(out, row.page)
	}
	slices.SortFunc(out, byUpdated)
	return out
}

// Catalog 是全部页面，最近更新在前。
func (f *WikiFake) Catalog(_ context.Context) ([]services.WikiCatalogEntry, error) {
	f.gate(WikiOpCatalog)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.catalogs++
	if err := f.failure(WikiOpCatalog); err != nil {
		return nil, err
	}
	out := []services.WikiCatalogEntry{}
	for _, p := range f.orderedLocked() {
		out = append(out, catalogEntryOf(p))
	}
	return out, nil
}

// BacklinkCounts 是每页被多少页引用（没有引用的页面不在表里）。
func (f *WikiFake) BacklinkCounts(_ context.Context) (services.WikiBacklinkCounts, error) {
	f.gate(WikiOpBacklinks)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(WikiOpBacklinks); err != nil {
		return nil, err
	}
	out := services.WikiBacklinkCounts{}
	for _, tos := range f.links {
		for to := range tos {
			out[to]++
		}
	}
	return out, nil
}

// Status 是状态行的三个数字。
func (f *WikiFake) Status(_ context.Context) (services.WikiStatus, error) {
	f.gate(WikiOpStatus)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failure(WikiOpStatus); err != nil {
		return services.WikiStatus{}, err
	}
	return services.WikiStatus{PageCount: len(f.pages), CompiledItemCount: f.compiled, EligibleItemCount: f.eligible}, nil
}

// Search 按「标题命中 > 摘要命中 > 正文命中」的相关度排序（同档按目录顺序），查询为空白时返回空切片。
func (f *WikiFake) Search(_ context.Context, query string, limit int) ([]services.WikiSearchHit, error) {
	f.gate(WikiOpSearch)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searches = append(f.searches, WikiSearchCall{Query: query, Limit: limit})
	if err := f.failure(WikiOpSearch); err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	out := []services.WikiSearchHit{}
	if needle == "" {
		return out, nil
	}
	type scored struct {
		hit  services.WikiSearchHit
		rank int
	}
	var found []scored
	for _, p := range f.orderedLocked() {
		rank := -1
		switch {
		case strings.Contains(strings.ToLower(p.Title), needle):
			rank = 0
		case strings.Contains(strings.ToLower(p.Summary), needle):
			rank = 1
		case strings.Contains(strings.ToLower(p.Body), needle):
			rank = 2
		}
		if rank >= 0 {
			found = append(found, scored{services.WikiSearchHit{ID: p.ID, Title: p.Title, Kind: p.Kind, Summary: p.Summary}, rank})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int { return a.rank - b.rank })
	for _, s := range found {
		if limit > 0 && len(out) >= limit {
			break
		}
		out = append(out, s.hit)
	}
	return out, nil
}

// GetPage 读一页（带反向链接与来源）；不存在返回 nil。
func (f *WikiFake) GetPage(_ context.Context, id string) (*services.WikiPageDetail, error) {
	f.gate(WikiOpGetPage)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, id)
	if err := f.failure(WikiOpGetPage); err != nil {
		return nil, err
	}
	row := f.pages[id]
	if row == nil {
		return nil, nil
	}
	detail := &services.WikiPageDetail{Page: row.page, Backlinks: []services.WikiCatalogEntry{}, Sources: slices.Clone(row.sources)}
	if detail.Sources == nil {
		detail.Sources = []services.WikiPageSource{}
	}
	for _, p := range f.orderedLocked() {
		if f.links[p.ID][id] {
			detail.Backlinks = append(detail.Backlinks, catalogEntryOf(p))
		}
	}
	return detail, nil
}

// ListRevisions 是一页的历史版本（新的在前）；页面不存在时是空切片。
func (f *WikiFake) ListRevisions(_ context.Context, pageID string) ([]services.WikiRevision, error) {
	f.gate(WikiOpListRevisions)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls = append(f.listCalls, pageID)
	if err := f.failure(WikiOpListRevisions); err != nil {
		return nil, err
	}
	out := []services.WikiRevision{}
	if row := f.pages[pageID]; row != nil {
		out = append(out, row.revisions...)
	}
	return out, nil
}

// wikiGraphCap 与处理器一致：图谱最多取连接度最高的 300 个节点。
const wikiGraphCap = 300

// Graph 按连接度（出链加入链）倒序、同度按最近更新取前 300 个节点，只保留两端都可见的边。
func (f *WikiFake) Graph(_ context.Context) (services.WikiGraph, error) {
	f.gate(WikiOpGraph)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.graphs++
	if err := f.failure(WikiOpGraph); err != nil {
		return services.WikiGraph{}, err
	}
	degree := map[string]int{}
	for from, tos := range f.links {
		for to := range tos {
			degree[from]++
			degree[to]++
		}
	}
	ordered := f.orderedLocked()
	slices.SortStableFunc(ordered, func(a, b services.WikiPage) int { return degree[b.ID] - degree[a.ID] })
	g := services.WikiGraph{Nodes: []services.WikiGraphNode{}, Links: []services.WikiGraphLink{}, TotalNodes: len(ordered)}
	visible := map[string]bool{}
	for _, p := range ordered {
		if len(g.Nodes) >= wikiGraphCap {
			break
		}
		visible[p.ID] = true
		g.Nodes = append(g.Nodes, services.WikiGraphNode{ID: p.ID, Title: p.Title, Kind: p.Kind})
	}
	froms := make([]string, 0, len(f.links))
	for from := range f.links {
		froms = append(froms, from)
	}
	slices.Sort(froms)
	for _, from := range froms {
		tos := make([]string, 0, len(f.links[from]))
		for to := range f.links[from] {
			tos = append(tos, to)
		}
		slices.Sort(tos)
		for _, to := range tos {
			if visible[from] && visible[to] {
				g.Links = append(g.Links, services.WikiGraphLink{Source: from, Target: to})
			}
		}
	}
	return g, nil
}

// ---- 写入接口 ----

// snapshotLocked 把页面当前内容存成一份历史版本（每页最多保留 10 份）。
func (f *WikiFake) snapshotLocked(row *wikiRow) {
	p := row.page
	f.revSeq++
	rev := services.WikiRevision{
		ID: p.ID + "-rev-" + strconv.Itoa(f.revSeq), PageID: p.ID, Title: p.Title, Kind: p.Kind, Summary: p.Summary,
		Body: p.Body, AliasesJSON: p.AliasesJSON, Model: p.Model, PromptVersion: p.PromptVersion, CreatedAt: f.tick(),
	}
	row.revisions = append([]services.WikiRevision{rev}, row.revisions...)
	if len(row.revisions) > 10 {
		row.revisions = row.revisions[:10]
	}
}

// rebuildLinksLocked 按目标页规范化标题重建一页的出链（不存在的目标与自己被忽略）。
func (f *WikiFake) rebuildLinksLocked(pageID string, targets []string) {
	delete(f.links, pageID)
	for _, t := range targets {
		for _, row := range f.pages {
			if row.page.NormalizedTitle == t && row.page.ID != pageID {
				if f.links == nil {
					f.links = map[string]map[string]bool{}
				}
				if f.links[pageID] == nil {
					f.links[pageID] = map[string]bool{}
				}
				f.links[pageID][row.page.ID] = true
			}
		}
	}
}

// UpdatePage 改写正文：先存历史版本，再写正文、更新时间与手动编辑标记（ReleaseToAuto 时清除标记），按 LinkTargets 重建出链。
func (f *WikiFake) UpdatePage(_ context.Context, in services.WikiUpdatePageInput) (bool, error) {
	f.gate(WikiOpUpdatePage)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, in)
	if err := f.failure(WikiOpUpdatePage); err != nil {
		return false, err
	}
	row := f.pages[in.PageID]
	if row == nil {
		return false, nil
	}
	f.snapshotLocked(row)
	now := f.tick()
	row.page.Body = in.Body
	row.page.UpdatedAt = now
	if in.ReleaseToAuto {
		row.page.ManualEditedAt = nil
	} else {
		row.page.ManualEditedAt = &now
	}
	f.rebuildLinksLocked(in.PageID, in.LinkTargets)
	return true, nil
}

// DeletePage 删除一页及与它相关的全部链接；不存在返回 false。
func (f *WikiFake) DeletePage(_ context.Context, id string) (bool, error) {
	f.gate(WikiOpDeletePage)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, id)
	if err := f.failure(WikiOpDeletePage); err != nil {
		return false, err
	}
	if f.pages[id] == nil {
		return false, nil
	}
	delete(f.pages, id)
	delete(f.links, id)
	for _, tos := range f.links {
		delete(tos, id)
	}
	return true, nil
}

// RestoreRevision 把一页回滚到某份历史版本：先给当前内容存快照，再覆盖标题、类型、摘要、正文与别名，并标记为手动编辑。
func (f *WikiFake) RestoreRevision(_ context.Context, revisionID string) (bool, error) {
	f.gate(WikiOpRestore)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restores = append(f.restores, revisionID)
	if err := f.failure(WikiOpRestore); err != nil {
		return false, err
	}
	for _, row := range f.pages {
		for _, rev := range row.revisions {
			if rev.ID != revisionID {
				continue
			}
			f.snapshotLocked(row)
			now := f.tick()
			row.page.Title, row.page.Kind, row.page.Summary, row.page.Body, row.page.AliasesJSON = rev.Title, rev.Kind, rev.Summary, rev.Body, rev.AliasesJSON
			row.page.UpdatedAt, row.page.ManualEditedAt = now, &now
			return true, nil
		}
	}
	return false, nil
}
