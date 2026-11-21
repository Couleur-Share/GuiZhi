package library

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 条目详情与草稿保存（渲染层 stores/knowledge.store.ts 的 selectItem、updateSelected、persistPatch、flushPendingSave、
// resolveSaveConflict、mergeServerItem、applyServerItem，以及 setItemTags 里选中条目的那条路径）。
//
// 编辑先在本地即时回显、再防抖落盘：改动按条目进「待保存」桶（pending），落盘时搬进「在途」桶（saving）并带上编辑起点的基线（bases）；
// 失败退回待保存，成功后用服务端版本重新叠上还没落盘的字段（mergeServerItemLocked）。
// 选中另一条、改筛选、关闭之前都先 FlushPendingSave——保存失败就不往下走，改动不丢。
// 服务端发现这条已在别处改过时返回冲突、什么也不写，等用户选保留哪一版（ResolveSaveConflict）。

// autoSaveDebounce 是编辑停下来之后自动保存前的等待（渲染层 AUTO_SAVE_DEBOUNCE_MS）。
const autoSaveDebounce = 800 * time.Millisecond

// DraftConflict 是草稿保存撞上另一入口的修改之后、等用户选保留哪一版的状态（渲染层 saveConflict）。
type DraftConflict struct {
	ItemID string
	// Server 是服务端此刻的版本；Fields 是冲突的字段（services.LibraryDraftFieldTitle、services.LibraryDraftFieldContent）。
	Server services.LibraryItem
	Fields []string
}

// ConflictChoice 是冲突提示上的两个选择（渲染层 resolveSaveConflict 的 "local" | "server"）。
type ConflictChoice uint8

const (
	// KeepMine 保留我的修改：以服务端版本为新基线，再把我的字段写回去。
	KeepMine ConflictChoice = iota
	// UseSaved 采用已保存的版本：丢掉冲突字段上我的修改。
	UseSaved
)

// DetailView 是右侧详情背后的状态（渲染层 knowledge.store 的 selectedItem、detailLoading、detailError、titleFocusItemId、
// saveConflict、isSaving、hasUnsavedChanges、saveError）。
type DetailView struct {
	// Item 是详情在看的条目，含尚未落盘的编辑；没有选中、读取中与读取失败时为 nil。
	// Store 每次改动都换一个新对象，界面线程拿到的快照不会再变。
	Item *services.LibraryItem
	// Loading 是详情读取中；LoadError 是读取失败的原因（空表示没有失败）。
	Loading   bool
	LoadError string
	// TitleFocusID 是刚新建的空白条目 ID：详情头部据此把光标放进标题，消费后清空（ClearTitleFocus）。
	TitleFocusID string
	// Conflict 非空时，详情头部显示冲突提示并等用户选择。
	Conflict *DraftConflict
	// Saving 是保存请求在途；Unsaved 是有尚未落盘的编辑（自动保存关闭时由保存按钮与 Ctrl+S 落盘）；
	// SaveError 是上次保存失败的原因（改动已退回待保存队列，可重试），保存成功后清空。
	Saving    bool
	Unsaved   bool
	SaveError string
	// JustSaved 是保存刚结束的短暂「已保存」提示（渲染层 ItemDetailHeader 的 justSaved；规则见 store_detail_extras.go）。
	// 头部还要求没有未落盘的编辑（自动保存关着时）、没有保存中与失败才画出来。
	JustSaved bool
	// Revisions 是「更新的来源版本」背后的数据（SourceCaptureRevisions）；CanDiarize 是转写路由支持区分说话人
	// （useTranscriptActions 的 canDiarize）；两者都在读到条目之后读取，换条目重置。
	Revisions  RevisionsView
	CanDiarize bool
	// Tools 是文章工具侧栏背后的状态（来源评论、网页来源版本、交接稿忙标记，store_tools.go），读到条目之后挂载，换条目重置。
	Tools ToolsView
	// Media 是转写、文字稿排版、总结与刷新论坛讨论的状态（use-media-actions.ts 的 hook 状态，store_media.go），换条目重置。
	Media MediaView
	// Snapshot 是公众号网页快照面板背后的状态（读取、补采、store_snapshot.go），由界面进入快照视图时发起读取，换条目重置。
	Snapshot SnapshotView
	// MarkingReviewedID 是正在标记为已复核的条目 ID（ReviewRequiredNotice 的 isMarking；空表示没有）。
	MarkingReviewedID string
}

// patchSet 是按条目分桶、按首次写入顺序遍历的待保存改动（渲染层 pendingPatches 这个 Map：遍历顺序就是写入顺序，
// 改已有的键不换位置，删掉再写则排到最后）。零值可用。
type patchSet struct {
	m     map[string]services.LibraryDraftFields
	order []string
}

func (p *patchSet) get(id string) (services.LibraryDraftFields, bool) {
	f, ok := p.m[id]
	return f, ok
}

func (p *patchSet) put(id string, f services.LibraryDraftFields) {
	if p.m == nil {
		p.m = map[string]services.LibraryDraftFields{}
	}
	if _, ok := p.m[id]; !ok {
		p.order = append(p.order, id)
	}
	p.m[id] = f
}

func (p *patchSet) remove(id string) {
	if _, ok := p.m[id]; !ok {
		return
	}
	delete(p.m, id)
	p.order = slices.DeleteFunc(p.order, func(x string) bool { return x == id })
}

func (p *patchSet) len() int { return len(p.m) }

func (p *patchSet) ids() []string { return slices.Clone(p.order) }

// flushRun 是正在进行的一次「把待保存全部落盘」：并发的调用者共用它的结果（渲染层 flushRun 这个 Promise）。
type flushRun struct {
	done chan struct{}
	ok   bool
}

// detailState 是详情的内部状态（渲染层模块级的 pendingPatches、savingPatches、draftBases、flushRun、detailRequestSeq、saveTimer）。
// mu 保护它。
type detailState struct {
	// seq 是详情请求的序号：选中、改筛选都会让它加一，在途的旧请求回来发现序号变了就丢弃（渲染层 detailRequestSeq）。
	seq     int
	pending patchSet
	saving  map[string]services.LibraryDraftFields
	bases   map[string]services.LibraryDraftFields
	flush   *flushRun
	// stop 取消自动保存的定时器；没有排定时为 nil。
	stop func()
	// saved、savedGen、savedStop 是「已保存」提示的观察记录、定时器代号与取消函数（store_detail_extras.go）。
	saved     savedTrack
	savedGen  int
	savedStop func()
	// tools 是文章工具侧栏的内部状态（挂载代号与在途标记，store_tools.go）。
	tools toolsState
	// media 是媒体动作的内部状态（切条目代号，store_media.go）。
	media mediaState
	// snapshot 是网页快照的内部状态（挂载代号、读取序号与补采轮询，store_snapshot.go）。
	snapshot snapshotState
}

func newDetailState() detailState {
	return detailState{
		saving: map[string]services.LibraryDraftFields{},
		bases:  map[string]services.LibraryDraftFields{},
	}
}

// overlayFields 返回 base 上叠 top 的结果：top 里有的字段盖掉 base 的（渲染层的 {...a, ...b}）。
func overlayFields(base, top services.LibraryDraftFields) services.LibraryDraftFields {
	out := base
	if top.Title != nil {
		out.Title = top.Title
	}
	if top.Content != nil {
		out.Content = top.Content
	}
	if top.TagNames != nil {
		out.TagNames = top.TagNames
	}
	return out
}

// fieldsOf 取条目当前的可编辑字段，作为编辑的基线（渲染层 fieldsOf）。
func fieldsOf(item services.LibraryItem) services.LibraryDraftFields {
	return services.LibraryDraftFields{
		Title:    services.StringPtr(item.Title),
		Content:  services.StringPtr(item.Content),
		TagNames: item.TagNames(),
	}
}

// entryOf 取条目完整形态里列表行用得着的字段（applyEntry 只读这些）。
func entryOf(it services.LibraryItem) services.LibraryEntry {
	return services.LibraryEntry{
		ID: it.ID, Title: it.Title, Status: it.Status, CollectionID: it.CollectionID,
		IsFavorite: it.IsFavorite, IsPinned: it.IsPinned, UpdatedAt: it.UpdatedAt, Tags: it.Tags,
	}
}

// itemWithEntry 把服务端返回的列表形态并进详情里的条目：条目级字段取服务端的，正文、摘要、文字稿与来源保持原样。
// 渲染层此处拿到的是完整条目（applyServerItem），正文以服务端为准；列表形态没有正文，外部改了正文要等下一次读取详情才会跟上。
func itemWithEntry(cur services.LibraryItem, e services.LibraryEntry) services.LibraryItem {
	cur.Title, cur.Status, cur.CollectionID = e.Title, e.Status, e.CollectionID
	cur.IsFavorite, cur.IsPinned, cur.UpdatedAt, cur.DeletedAt = e.IsFavorite, e.IsPinned, e.UpdatedAt, e.DeletedAt
	cur.ReviewStatus, cur.ReviewReasons, cur.Tags = e.ReviewStatus, e.ReviewReasons, e.Tags
	return cur
}

// reconcileOptimisticTags 是本地即时回显的标签列表：已存在的标签沿用原对象，新名字生成临时占位（渲染层同名函数）。
// 不回显的话，标签浮层下一次 onChange 仍基于旧的标签做全量覆盖，防抖窗口内连加两个标签，第一个会被第二个的补丁覆盖掉。
func reconcileOptimisticTags(existing []services.LibraryTag, names []string) []services.LibraryTag {
	byName := make(map[string]services.LibraryTag, len(existing))
	for _, t := range existing {
		byName[services.JSLower(t.Name)] = t
	}
	now := time.Now().UnixMilli()
	out := make([]services.LibraryTag, len(names))
	for i, name := range names {
		key := services.JSLower(name)
		if t, ok := byName[key]; ok {
			out[i] = t
			continue
		}
		out[i] = services.LibraryTag{ID: "pending:" + key, Name: name, ColorKey: "gray", CreatedAt: now, UpdatedAt: now}
	}
	return out
}

// describeLoadError 取读取失败的原因给界面显示（渲染层 describeLoadError）：去掉首尾空白，空的换成「未知错误」。
func describeLoadError(err error) string {
	if msg := strings.TrimSpace(err.Error()); msg != "" {
		return msg
	}
	return locales.CommonUnknownError()
}

// backgroundCtx 是 Store 自己发起的读取与保存用的 context（Load 时记下的那个）；没有时用 Background。
func (s *Store) backgroundCtx() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bg == nil {
		return context.Background()
	}
	return s.bg
}

// needsFlushLocked 报告是否有要先落盘的编辑（待保存非空或一次落盘正在进行）。mu 由调用方持有。
func (s *Store) needsFlushLocked() bool {
	return s.detail.pending.len() > 0 || s.detail.flush != nil
}

// ---- 选中并读取详情 ----

// selectItem 选中一条并读取它的详情（渲染层 selectItem）。空串取消选中。不阻塞：落盘与读取都放到后台。
//
// 先把上一条的编辑落盘：没有要落盘的就当场选中；有的话放到后台，落盘失败就不动选中（改动仍在待保存队列里，失败原因在详情头部）。
// 序号让「选中之后又选了别的」「选中之后筛选变了」的旧请求作废。
func (s *Store) selectItem(ctx context.Context, id string) { s.selectFlow(ctx, id, false) }

// SelectItemWait 与 selectItem 相同，但阻塞到落盘与读取都落定，返回是否成功（渲染层 `await selectItem(id)` 的布尔结果）：
// 上一条的草稿落不了盘、被后来的选中或筛选改动取代、条目读不出来，都返回 false；取消选中（空串）与读到了返回 true。
// 连续整理据此决定能不能继续（B-052）。会阻塞，不能在界面线程调用。
func (s *Store) SelectItemWait(ctx context.Context, id string) bool {
	return s.selectFlow(ctx, id, true)
}

// selectFlow 是 selectItem 与 SelectItemWait 共用的流程；wait 为假时返回值没有意义（恒为 true）。
func (s *Store) selectFlow(ctx context.Context, id string, wait bool) bool {
	s.mu.Lock()
	s.detail.seq++
	seq := s.detail.seq
	if !s.needsFlushLocked() {
		s.commitSelectLocked(id)
		s.mu.Unlock()
		s.wake()
		if id == "" {
			return true
		}
		if wait {
			return s.loadDetail(ctx, id, seq)
		}
		s.Spawn(func() { s.loadDetail(ctx, id, seq) })
		return true
	}
	s.mu.Unlock()
	run := func() bool {
		if !s.FlushPendingSave(ctx) {
			return false
		}
		s.mu.Lock()
		if seq != s.detail.seq {
			s.mu.Unlock()
			return false
		}
		s.commitSelectLocked(id)
		s.mu.Unlock()
		s.wake()
		if id == "" {
			return true
		}
		return s.loadDetail(ctx, id, seq)
	}
	if wait {
		return run()
	}
	s.Spawn(func() { run() })
	return true
}

// commitSelectLocked 落定选中项并把详情置为读取中（没有选中时不读取）。mu 由调用方持有。
func (s *Store) commitSelectLocked(id string) {
	s.view.List.SelectedID = id
	d := &s.view.Detail
	d.Item, d.Loading, d.LoadError = nil, id != "", ""
	d.Revisions, d.CanDiarize = RevisionsView{}, false
	s.resetToolsLocked()
	s.resetMediaLocked()
	s.resetSnapshotLocked()
	s.resetSavedLocked()
}

// clearDetailLocked 清掉选中与详情，并让在途的详情读取作废（筛选与搜索把选中项挤出列表时）。mu 由调用方持有。
func (s *Store) clearDetailLocked() {
	s.detail.seq++
	s.view.List.SelectedID = ""
	d := &s.view.Detail
	d.Item, d.Loading, d.LoadError = nil, false, ""
	d.Revisions, d.CanDiarize = RevisionsView{}, false
	s.resetToolsLocked()
	s.resetMediaLocked()
	s.resetSnapshotLocked()
	s.resetSavedLocked()
}

// loadDetail 读取详情；序号变了就丢弃结果，失败记下原因（渲染层 selectItem 的读取部分）。
// 返回读到了并且没有被取代（渲染层：被取代、读取失败都是 false）。
func (s *Store) loadDetail(ctx context.Context, id string, seq int) bool {
	item, found, err := s.svc.GetItem(ctx, id)
	if err == nil && !found {
		err = errors.New("条目不存在或已被删除")
	}
	s.mu.Lock()
	current := seq == s.detail.seq
	switch {
	case !current:
	case err != nil:
		s.view.Detail.Loading, s.view.Detail.LoadError = false, describeLoadError(err)
	default:
		merged := s.mergeServerItemLocked(item)
		s.view.Detail.Item, s.view.Detail.Loading = &merged, false
		// 头部挂载：已保存提示的观察记录是干净的（换条目时重置过），此刻的保存状态也是干净的——换条目之前先落盘
		// （selectFlow 的 flush），所以不会有在途的保存，不需要在这里记观察起点。
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("library.detail", fmt.Errorf("加载条目详情失败：%w", err))
	}
	if current {
		s.wake()
	}
	if current && err == nil {
		s.Spawn(func() { s.afterDetailLoaded(ctx, id, seq) })
	}
	return current && err == nil
}

// ClearTitleFocus 消费「新建后把光标放进标题」的请求（渲染层 clearTitleFocus）。
func (s *Store) ClearTitleFocus() {
	s.mu.Lock()
	s.view.Detail.TitleFocusID = ""
	s.mu.Unlock()
	s.wake()
}

// ---- 本地编辑与防抖保存 ----

// UpdateSelected 即时更新选中条目并排定防抖保存（渲染层 updateSelected）。没有选中的详情时什么也不做。
// 第一次编辑某条时记下它此刻的字段作为基线，服务端据此判断这期间有没有被别处改过。
func (s *Store) UpdateSelected(ctx context.Context, patch services.LibraryDraftFields) {
	s.mu.Lock()
	cur := s.view.Detail.Item
	if cur == nil || s.view.List.SelectedID != cur.ID {
		s.mu.Unlock()
		return
	}
	if _, ok := s.detail.bases[cur.ID]; !ok {
		s.detail.bases[cur.ID] = fieldsOf(*cur)
	}
	next := *cur
	if patch.Title != nil {
		next.Title = *patch.Title
	}
	if patch.Content != nil {
		next.Content = *patch.Content
	}
	if patch.TagNames != nil {
		next.Tags = reconcileOptimisticTags(cur.Tags, patch.TagNames)
	}
	s.view.Detail.Item = &next
	pending, _ := s.detail.pending.get(cur.ID)
	s.detail.pending.put(cur.ID, overlayFields(pending, patch.Clone()))
	s.scheduleSaveLocked(ctx)
	s.syncSavedLocked()
	s.mu.Unlock()
	s.wake()
}

// scheduleSaveLocked 标记有未落盘的编辑，并重新排定自动保存。自动保存关闭时不排定时器：改动留在待保存里，
// 由保存按钮、Ctrl+S、切换条目与改筛选时的 FlushPendingSave 落盘。mu 由调用方持有。
func (s *Store) scheduleSaveLocked(ctx context.Context) {
	s.view.Detail.Unsaved = true
	s.cancelSaveTimerLocked()
	if s.AutoSave != nil && !s.AutoSave() {
		return
	}
	s.detail.stop = s.After(autoSaveDebounce, func() {
		s.Spawn(func() { s.FlushPendingSave(ctx) })
	})
}

func (s *Store) cancelSaveTimerLocked() {
	if s.detail.stop != nil {
		s.detail.stop()
		s.detail.stop = nil
	}
}

// FlushPendingSave 立即把全部待保存的改动落盘（渲染层 flushPendingSave：Ctrl+S、切换条目、改筛选与关闭之前），
// 返回是否全部成功。已经有一次落盘在进行时，等它并共用结果。失败不自动重试。任意 goroutine 可调用；会阻塞到落盘结束。
func (s *Store) FlushPendingSave(ctx context.Context) bool {
	s.mu.Lock()
	s.cancelSaveTimerLocked()
	if run := s.detail.flush; run != nil {
		s.mu.Unlock()
		select {
		case <-run.done:
			return run.ok
		case <-ctx.Done():
			return false
		}
	}
	run := &flushRun{done: make(chan struct{})}
	s.detail.flush = run
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.detail.flush = nil
		s.mu.Unlock()
		close(run.done)
	}()
	run.ok = s.drainPending(ctx)
	return run.ok
}

// drainPending 顺序清空各条目的待保存桶；落盘期间新输入的留到下一轮，一桶失败就停（渲染层 flushPendingSave 的 run）。
func (s *Store) drainPending(ctx context.Context) bool {
	for {
		s.mu.Lock()
		ids := s.detail.pending.ids()
		s.mu.Unlock()
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			if !s.persistPatch(ctx, id) {
				return false
			}
		}
	}
	s.mu.Lock()
	s.view.Detail.Unsaved = false
	s.syncSavedLocked()
	s.mu.Unlock()
	s.wake()
	return true
}

// persistPatch 落盘一个条目的待保存改动；失败只退回它自己那一桶（渲染层 persistPatch）。
func (s *Store) persistPatch(ctx context.Context, id string) bool {
	s.mu.Lock()
	patch, ok := s.detail.pending.get(id)
	s.detail.pending.remove(id)
	if !ok || patch.IsEmpty() {
		s.mu.Unlock()
		return true
	}
	s.detail.saving[id] = patch
	s.view.Detail.Saving, s.view.Detail.SaveError = true, ""
	s.syncSavedLocked()
	base := s.detail.bases[id].Clone()
	s.mu.Unlock()
	s.wake()

	err := s.saveDraft(ctx, id, patch, base)

	s.mu.Lock()
	delete(s.detail.saving, id)
	if _, queued := s.detail.pending.get(id); !queued {
		delete(s.detail.bases, id)
	}
	s.view.Detail.Saving = false
	s.view.Detail.Unsaved = s.detail.pending.len() > 0
	s.syncSavedLocked()
	s.mu.Unlock()
	s.wake()
	if err != nil {
		s.guard.Background("library.saveDraft", fmt.Errorf("保存条目失败：%w", err))
		return false
	}
	return true
}

// saveDraft 发一次草稿保存并把结果并进状态：失败退回待保存、记下原因（撞上别处的修改还要记下冲突）；
// 成功用服务端版本换基线、回写列表、重新叠上还没落盘的字段。
func (s *Store) saveDraft(ctx context.Context, id string, patch, base services.LibraryDraftFields) error {
	res, err := s.svc.SaveDraft(ctx, services.LibraryDraftInput{ID: id, RequestID: s.newID(), Base: base, Patch: patch.Clone()})
	if err == nil && (!res.OK || res.Item == nil) {
		if len(res.Conflicts) > 0 && res.Item != nil {
			s.mu.Lock()
			s.view.Detail.Conflict = &DraftConflict{ItemID: id, Server: *res.Item, Fields: slices.Clone(res.Conflicts)}
			s.mu.Unlock()
		}
		if res.Error != "" {
			err = errors.New(res.Error)
		} else {
			// 渲染层同一处（knowledge.store.ts 的 saveDraft）抛的就是这句中文原文，它没有翻译，所以照搬、不进 i18n。
			err = errors.New("条目不存在或已被删除")
		}
	}
	if err != nil {
		s.mu.Lock()
		latest, _ := s.detail.pending.get(id)
		s.detail.pending.put(id, overlayFields(patch, latest)) // 退回待保存；落盘期间输入的更晚的字段以新的为准
		s.view.Detail.SaveError = err.Error()
		s.syncSavedLocked()
		s.mu.Unlock()
		return err
	}

	updated := *res.Item
	s.mu.Lock()
	later, hasLater := s.detail.pending.get(id)
	prev := s.detail.bases[id]
	next := fieldsOf(updated)
	if hasLater {
		if later.TagNames != nil {
			from := prev.TagNames
			if patch.TagNames != nil {
				from = patch.TagNames
			}
			later.TagNames = services.MergeDraftTags(from, later.TagNames, updated.TagNames())
			s.detail.pending.put(id, later)
		}
		// 落盘期间又改了、而这次没带的字段：基线还是编辑起点那份，不能换成服务端刚返回的。
		if later.Title != nil && patch.Title == nil {
			next.Title = prev.Title
		}
		if later.Content != nil && patch.Content == nil {
			next.Content = prev.Content
		}
	}
	s.detail.bases[id] = next
	delete(s.detail.saving, id)
	s.applyEntryLocked(entryOf(updated))
	if s.view.List.SelectedID == id {
		merged := s.mergeServerItemLocked(updated)
		s.view.Detail.Item = &merged
	}
	if c := s.view.Detail.Conflict; c != nil && c.ItemID == id {
		s.view.Detail.Conflict = nil
	}
	s.mu.Unlock()
	s.wake()
	if patch.TagNames != nil { // 可能新建了标签，侧栏的标签列表与计数都要跟上
		s.FetchTags(ctx)
		s.RefreshCounts(ctx)
	}
	return nil
}

// mergeServerItemLocked 把服务端的条目叠上还没落盘的字段（在途的、待保存的，后者盖前者），得到界面该显示的版本（渲染层 mergeServerItem）。
// 标签按相对编辑起点的增删合并，保留另一入口新增的标签。mu 由调用方持有。
func (s *Store) mergeServerItemLocked(item services.LibraryItem) services.LibraryItem {
	pending, _ := s.detail.pending.get(item.ID)
	patch := overlayFields(s.detail.saving[item.ID], pending)
	if patch.Title != nil {
		item.Title = *patch.Title
	}
	if patch.Content != nil {
		item.Content = *patch.Content
	}
	if patch.TagNames != nil {
		merged := services.MergeDraftTags(s.detail.bases[item.ID].TagNames, patch.TagNames, item.TagNames())
		item.Tags = reconcileOptimisticTags(item.Tags, merged)
	}
	return item
}

// ---- 冲突与服务端写回 ----

// ResolveSaveConflict 处理冲突提示上的选择（渲染层 resolveSaveConflict），返回随后的落盘是否成功。
// 两种选择都以服务端版本为新基线，标签先按旧基线重放增删再换基线；采用已保存的版本时还要丢掉冲突字段上我的修改。
func (s *Store) ResolveSaveConflict(ctx context.Context, choice ConflictChoice) bool {
	s.mu.Lock()
	conflict := s.view.Detail.Conflict
	if conflict == nil {
		s.mu.Unlock()
		return false
	}
	item := conflict.Server
	id := item.ID
	if pending, ok := s.detail.pending.get(id); ok && pending.TagNames != nil {
		pending.TagNames = services.MergeDraftTags(s.detail.bases[id].TagNames, pending.TagNames, item.TagNames())
		s.detail.pending.put(id, pending)
	}
	s.detail.bases[id] = fieldsOf(item)
	if choice == UseSaved {
		remaining, _ := s.detail.pending.get(id)
		for _, field := range conflict.Fields {
			switch field {
			case services.LibraryDraftFieldTitle:
				remaining.Title = nil
			case services.LibraryDraftFieldContent:
				remaining.Content = nil
			}
		}
		if remaining.IsEmpty() {
			s.detail.pending.remove(id)
			delete(s.detail.bases, id)
		} else {
			s.detail.pending.put(id, remaining)
		}
	}
	d := &s.view.Detail
	d.Conflict, d.SaveError, d.Unsaved = nil, "", s.detail.pending.len() > 0
	if s.view.List.SelectedID == id {
		merged := s.mergeServerItemLocked(item)
		d.Item = &merged
	}
	s.syncSavedLocked()
	s.mu.Unlock()
	s.wake()
	return s.FlushPendingSave(ctx)
}

// ApplyServerEntry 把服务端返回的条目并回列表行，选中的就是它时也并进详情（渲染层 applyServerItem）：
// 详情里还没落盘的编辑照样叠在上面。收藏、置顶、状态、复核标记这些即时落盘的操作用它。
func (s *Store) ApplyServerEntry(updated services.LibraryEntry) {
	s.mu.Lock()
	s.applyEntryLocked(updated)
	if cur := s.view.Detail.Item; cur != nil && cur.ID == updated.ID && s.view.List.SelectedID == updated.ID {
		merged := s.mergeServerItemLocked(itemWithEntry(*cur, updated))
		s.view.Detail.Item = &merged
	}
	s.mu.Unlock()
	s.wake()
}
