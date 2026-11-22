package servicestest

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"guizhi-mygo-shell/native/services"
)

// 条目详情的假实现：规则取自真实处理器（knowledge_draft.go 的 knowledgeSaveDraft、knowledge:get）里与界面行为有关的那部分——
// 回收站里的条目不能存草稿、title 与 content 带基线比较（基线与服务端不同、补丁也与服务端不同才算冲突，冲突时什么也不写）、
// 标签按相对基线的增删合并。条目的共有字段（标题、标签、收藏……）取自条目全集（List 看到的是同一份），
// 只有列表投影里没有的正文、摘要、文字稿与来源放在这里。

// DetailBody 是列表投影里没有的那部分字段。
type DetailBody struct {
	Content, Summary, Transcript, SourceURI string
}

// 与真实处理器同文的失败原因。经 errors.New 构造：它们是诊断文本、不是界面文案，native 的中日韩字面量守卫对这类调用放行。
var (
	draftGoneReason     = errors.New("条目不存在或已移到回收站").Error()
	draftConflictReason = errors.New("该内容已在其他操作中修改，请选择要保留的版本").Error()
)

// detailState 是条目详情的内部状态；零值可用。
type detailState struct {
	bodies   map[string]DetailBody
	versions map[string][]services.LibrarySourceVersion
	gets     []string
	creates  []services.LibraryCreateInput
	drafts   []services.LibraryDraftInput
	// getGate、saveGate 让下一次 GetItem、SaveDraft 卡住（HoldGet、HoldSave）。
	getGate, saveGate chan struct{}
	// draftFailure 非空时 SaveDraft 返回业务失败（OK 为假、Error 为它），不写任何东西。
	draftFailure string
	// diarization 是媒体能力里「支持区分说话人」（SetDiarization）；默认假，与没配置转写引擎时的真实处理器一致。
	diarization bool
}

// SeedDetail 设定一条的正文、摘要、文字稿与来源（条目本身要另外用 SeedEntries 放进条目全集）。
func (f *Library) SeedDetail(id string, body DetailBody) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.detail.bodies == nil {
		f.detail.bodies = map[string]DetailBody{}
	}
	f.detail.bodies[id] = body
}

// SeedSourceVersions 设定一条的来源版本（新的在前）。
func (f *Library) SeedSourceVersions(id string, versions []services.LibrarySourceVersion) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.detail.versions == nil {
		f.detail.versions = map[string][]services.LibrarySourceVersion{}
	}
	f.detail.versions[id] = slices.Clone(versions)
}

// HoldGet 让下一次 GetItem 卡住（在读取之前），直到返回的函数被调用（只卡一次）：用来构造「旧详情晚于新选中」的竞态。
func (f *Library) HoldGet() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.detail.getGate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// HoldSave 让下一次 SaveDraft 卡住（在写入之前），直到返回的函数被调用（只卡一次）：用来构造「保存在途时又输入」的场景。
func (f *Library) HoldSave() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.detail.saveGate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// FailDraft 让之后的 SaveDraft 返回业务失败（OK 为假、Error 为 reason），与 FailOn("SaveDraft") 的调用失败区分；reason 为空取消。
func (f *Library) FailDraft(reason string) {
	f.mu.Lock()
	f.detail.draftFailure = reason
	f.mu.Unlock()
}

// Gets 返回 GetItem 收到的条目 ID，按发生顺序。
func (f *Library) Gets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.detail.gets)
}

// Drafts 返回 SaveDraft 收到的请求，按发生顺序（只含真正到达处理器的，被 FailOn 拦下的也记）。
func (f *Library) Drafts() []services.LibraryDraftInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]services.LibraryDraftInput, len(f.detail.drafts))
	for i, d := range f.detail.drafts {
		d.Base, d.Patch = d.Base.Clone(), d.Patch.Clone()
		out[i] = d
	}
	return out
}

// ItemNow 返回一条此刻的完整形态（断言写入结果用）。
func (f *Library) ItemNow(id string) (services.LibraryItem, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.itemLocked(id)
}

// EditExternally 模拟另一入口改了这条（让草稿保存撞上冲突）：edit 拿到此刻的完整形态、改完写回。
// 标题、标签、收藏等共有字段写回条目全集，正文、摘要、文字稿与来源写回这里。
func (f *Library) EditExternally(id string, edit func(*services.LibraryItem)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item, ok := f.itemLocked(id)
	if !ok {
		return
	}
	edit(&item)
	entries := slices.Clone(f.entries)
	i := f.indexOf(entries, id)
	e := entries[i]
	e.Title, e.Status, e.CollectionID = item.Title, item.Status, item.CollectionID
	e.IsFavorite, e.IsPinned, e.DeletedAt = item.IsFavorite, item.IsPinned, item.DeletedAt
	e.ReviewStatus, e.ReviewReasons = item.ReviewStatus, slices.Clone(item.ReviewReasons)
	e.Tags = f.withTags(e, item.TagNames(), true)
	e.UpdatedAt = f.nowMs()
	entries[i] = e
	f.entries = entries
	if f.detail.bodies == nil {
		f.detail.bodies = map[string]DetailBody{}
	}
	f.detail.bodies[id] = DetailBody{Content: item.Content, Summary: item.Summary, Transcript: item.Transcript, SourceURI: item.SourceURI}
}

// itemLocked 由条目全集里的一条加上正文拼出完整形态；mu 由调用方持有。
func (f *Library) itemLocked(id string) (services.LibraryItem, bool) {
	i := f.indexOf(f.entries, id)
	if i < 0 {
		return services.LibraryItem{}, false
	}
	e := f.entries[i]
	body := f.detail.bodies[id]
	return services.LibraryItem{
		ID: e.ID, Title: e.Title, Content: body.Content, Summary: body.Summary, Transcript: body.Transcript,
		ItemType: e.ItemType, Status: e.Status, CollectionID: e.CollectionID,
		IsFavorite: e.IsFavorite, IsPinned: e.IsPinned,
		ReviewStatus: e.ReviewStatus, ReviewReasons: slices.Clone(e.ReviewReasons),
		SourceURI: body.SourceURI, DeletedAt: e.DeletedAt, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
		Tags: slices.Clone(e.Tags),
	}, true
}

// GetItem 实现 services.Library。
func (f *Library) GetItem(ctx context.Context, id string) (services.LibraryItem, bool, error) {
	if err := f.begin("GetItem:"+id, "GetItem"); err != nil {
		return services.LibraryItem{}, false, err
	}
	f.mu.Lock()
	f.detail.gets = append(f.detail.gets, id)
	gate := f.detail.getGate
	f.detail.getGate = nil
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return services.LibraryItem{}, false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	item, ok := f.itemLocked(id)
	return item, ok, nil
}

// CreateItem 实现 services.Library：新建一条空白笔记并放进条目全集（List 看到的是同一份），与真实处理器的默认值一致
// （note、active、无标题无正文、复核状态 clear）。每次调用都记下参数（Creates），被 FailOn 拦下的也记。
func (f *Library) CreateItem(ctx context.Context, in services.LibraryCreateInput) (services.LibraryItem, error) {
	err := f.begin("CreateItem", "CreateItem")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detail.creates = append(f.detail.creates, in)
	if err != nil {
		return services.LibraryItem{}, err
	}
	f.nextID++
	now := f.nowMs()
	entry := services.LibraryEntry{
		ID: fmt.Sprintf("item-new-%d", f.nextID), ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
		CollectionID: in.CollectionID, ReviewStatus: services.LibraryReviewClear, CreatedAt: now, UpdatedAt: now,
	}
	f.entries = append(slices.Clone(f.entries), entry)
	item, _ := f.itemLocked(entry.ID)
	return item, nil
}

// Creates 返回 CreateItem 收到的参数，按发生顺序。
func (f *Library) Creates() []services.LibraryCreateInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.detail.creates)
}

// SaveDraft 实现 services.Library。
func (f *Library) SaveDraft(ctx context.Context, in services.LibraryDraftInput) (services.LibraryDraftResult, error) {
	err := f.begin("SaveDraft:"+in.ID, "SaveDraft")
	f.mu.Lock()
	f.detail.drafts = append(f.detail.drafts, in)
	gate := f.detail.saveGate
	if err == nil {
		f.detail.saveGate = nil
	}
	f.mu.Unlock()
	if err != nil {
		return services.LibraryDraftResult{}, err
	}
	if gerr := waitGate(ctx, gate); gerr != nil {
		return services.LibraryDraftResult{}, gerr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.detail.draftFailure != "" {
		return services.LibraryDraftResult{Error: f.detail.draftFailure}, nil
	}
	current, ok := f.itemLocked(in.ID)
	if !ok || current.DeletedAt != 0 {
		return services.LibraryDraftResult{Error: draftGoneReason}, nil
	}
	var conflicts []string
	check := func(field string, base, patch *string, cur string) {
		if patch != nil && (base == nil || *base != cur) && *patch != cur {
			conflicts = append(conflicts, field)
		}
	}
	check(services.LibraryDraftFieldTitle, in.Base.Title, in.Patch.Title, current.Title)
	check(services.LibraryDraftFieldContent, in.Base.Content, in.Patch.Content, current.Content)
	if len(conflicts) > 0 {
		return services.LibraryDraftResult{Item: &current, Conflicts: conflicts, Error: draftConflictReason}, nil
	}

	entries := slices.Clone(f.entries)
	i := f.indexOf(entries, in.ID)
	e := entries[i]
	if in.Patch.Title != nil {
		e.Title = *in.Patch.Title
	}
	if in.Patch.TagNames != nil {
		merged := services.MergeDraftTags(in.Base.TagNames, in.Patch.TagNames, current.TagNames())
		e.Tags = f.withTags(e, merged, true)
	}
	e.UpdatedAt = f.nowMs()
	entries[i] = e
	f.entries = entries
	if in.Patch.Content != nil {
		if f.detail.bodies == nil {
			f.detail.bodies = map[string]DetailBody{}
		}
		body := f.detail.bodies[in.ID]
		body.Content = *in.Patch.Content
		f.detail.bodies[in.ID] = body
	}
	updated, _ := f.itemLocked(in.ID)
	return services.LibraryDraftResult{OK: true, Item: &updated}, nil
}

// SourceVersions 实现 services.Library。
func (f *Library) SourceVersions(ctx context.Context, id string) ([]services.LibrarySourceVersion, error) {
	if err := f.begin("SourceVersions:"+id, "SourceVersions"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.detail.versions[id]), nil
}

// SetDiarization 设定 MediaCapabilities 回复里「支持区分说话人」的值。
func (f *Library) SetDiarization(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detail.diarization = on
}

// MediaCapabilities 实现 services.Library：按 SetDiarization 的值回复。
func (f *Library) MediaCapabilities(ctx context.Context) (services.LibraryMediaCapabilities, error) {
	if err := f.begin("MediaCapabilities", "MediaCapabilities"); err != nil {
		return services.LibraryMediaCapabilities{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return services.LibraryMediaCapabilities{Diarization: f.detail.diarization}, nil
}
