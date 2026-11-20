package servicestest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"unicode/utf8"

	"guizhi-mygo-shell/native/services"
)

// 条目操作的假实现：规则取自真实处理器（knowledge_batch.go 的 executeBatchItem、knowledge_write.go 的更新）里与界面行为有关的那部分——
// 逐条独立记账、同一批次的回执（重试复用已提交的、换命令被拒）、回收站里的条目不能改、只有回收站里的条目能彻底删除。
// 写操作都按「整体替换」改条目全集（List 在锁外遍历它拿到的那份）。

// 与真实处理器同文的失败原因。
var (
	errBatchGone      = errors.New("条目已删除")
	errBatchInTrash   = errors.New("条目已在回收站")
	errBatchNotTrash  = errors.New("只能彻底删除回收站中的条目")
	errBatchBadKind   = errors.New("批量操作类型无效")
	errBatchChangedOp = errors.New("同一批次不能更换操作，请重新发起")
)

// defaultNow 是没有设定 SetNow 时写给「移入回收站」与更新时间的时刻（毫秒）。
const defaultNow int64 = 1_794_790_800_000

// BatchCall 是收到的一次批量执行。
type BatchCall struct {
	RunID   string
	IDs     []string
	Command services.LibraryBatchCommand
}

// UpdateCall 是收到的一次条目更新（UpdateEntry）。
type UpdateCall struct {
	ID    string
	Patch services.LibraryEntryPatch
}

// ExportCall 是收到的一次另存交接稿。
type ExportCall struct{ Title, Text string }

// receipt 是同一批次里一条的回执。
type receipt struct {
	command string
	ok      bool
}

// opsState 是条目操作的内部状态；零值可用。
type opsState struct {
	now        int64
	batches    []BatchCall
	receipts   map[string]receipt // runID + "\x00" + id
	itemFails  map[string]string  // id → 批量里这一条的注入失败
	updates    []string           // UpdateEntry 的调用记录：id
	updateCall []UpdateCall       // 同上，带补丁
	cleared    []string           // 带「清证据」彻底删除了的 ID
	supplement [][]string
	handoffs   map[string][2]services.LibraryHandoff // id → [完整版, 精简版]
	handoffErr map[string]error
	exports    []ExportCall
	exported   services.LibraryExportResult
	exportSet  bool
	freezes    []services.LibraryListQuery
	nextTag    int
	// freezeGate、batchGate、updateGate 让下一次 Freeze、ExecuteBatch、UpdateEntry 卡住（HoldFreeze、HoldBatch、HoldUpdate）。
	freezeGate, batchGate, updateGate chan struct{}
}

// SetNow 设定写操作用的「现在」（毫秒），让对照与断言不依赖真实时钟。
func (f *Library) SetNow(ms int64) {
	f.mu.Lock()
	f.ops.now = ms
	f.mu.Unlock()
}

func (f *Library) nowMs() int64 {
	if f.ops.now != 0 {
		return f.ops.now
	}
	return defaultNow
}

// HoldFreeze 让下一次 Freeze 卡住，直到返回的函数被调用（只卡一次）：用来在冻结期间改筛选，验证晚到的冻结不会当成新筛选的选择。
func (f *Library) HoldFreeze() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.ops.freezeGate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// HoldBatch 让下一次 ExecuteBatch 卡住（在改任何条目之前），直到返回的函数被调用（只卡一次）：用来观察「批量进行中」的状态。
func (f *Library) HoldBatch() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.ops.batchGate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// HoldUpdate 让下一次 UpdateEntry 卡住（在改条目之前），直到返回的函数被调用（只卡一次）：用来验证连点收藏、置顶时以在途的值为准。
func (f *Library) HoldUpdate() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.ops.updateGate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// waitGate 等 gate 被放行；gate 为 nil 时立即返回。上下文取消时返回它的错误。
func waitGate(ctx context.Context, gate chan struct{}) error {
	if gate == nil {
		return nil
	}
	select {
	case <-gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// FailBatchItem 让批量里 id 这一条失败（原因 reason）；reason 为空取消注入。其余条目照常执行。
func (f *Library) FailBatchItem(id, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ops.itemFails == nil {
		f.ops.itemFails = map[string]string{}
	}
	if reason == "" {
		delete(f.ops.itemFails, id)
		return
	}
	f.ops.itemFails[id] = reason
}

// Batches 返回迄今收到的批量执行，按发生顺序。
func (f *Library) Batches() []BatchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ops.batches)
}

// Updates 返回 UpdateEntry 收到的条目 ID，按发生顺序。
func (f *Library) Updates() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ops.updates)
}

// UpdateCalls 返回 UpdateEntry 收到的更新（带补丁），按发生顺序。
func (f *Library) UpdateCalls() []UpdateCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ops.updateCall)
}

// ClearedEvidence 返回带「清证据」彻底删除了的条目 ID。
func (f *Library) ClearedEvidence() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ops.cleared)
}

// Freezes 返回 Freeze 收到的查询。
func (f *Library) Freezes() []services.LibraryListQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ops.freezes)
}

// Supplemented 返回补采请求收到的 ID 批次。
func (f *Library) Supplemented() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ops.supplement)
}

// Exports 返回另存交接稿收到的请求。
func (f *Library) Exports() []ExportCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ops.exports)
}

// EntriesNow 返回此刻的条目全集（断言写入结果用）。
func (f *Library) EntriesNow() []services.LibraryEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.entries)
}

// SeedHandoff 设定一条的交接稿：full 是完整版，brief 是精简版。没设定的条目用确定性的默认文本。
func (f *Library) SeedHandoff(id string, full, brief services.LibraryHandoff) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ops.handoffs == nil {
		f.ops.handoffs = map[string][2]services.LibraryHandoff{}
	}
	f.ops.handoffs[id] = [2]services.LibraryHandoff{full, brief}
}

// FailHandoff 让一条的取交接稿失败（如 services.ErrHandoffEmpty）；err 为 nil 取消。
func (f *Library) FailHandoff(id string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ops.handoffErr == nil {
		f.ops.handoffErr = map[string]error{}
	}
	if err == nil {
		delete(f.ops.handoffErr, id)
		return
	}
	f.ops.handoffErr[id] = err
}

// SetExportResult 设定之后另存交接稿返回的结果；不设定时是成功并给一个确定性的路径。
func (f *Library) SetExportResult(r services.LibraryExportResult) {
	f.mu.Lock()
	f.ops.exported, f.ops.exportSet = r, true
	f.mu.Unlock()
}

// ---- 条目全集的改动 ----

// findTag 按名称找标签，没有就建一个（灰色），保持标签表按名称排序（与服务端一致）。mu 由调用方持有。
func (f *Library) findTag(name string) services.LibraryTag {
	for _, t := range f.tags {
		if t.Name == name {
			return t
		}
	}
	f.ops.nextTag++
	created := services.LibraryTag{ID: fmt.Sprintf("tag-new-%d", f.ops.nextTag), Name: name, ColorKey: "gray"}
	f.tags = append(slices.Clone(f.tags), created)
	sort.SliceStable(f.tags, func(a, b int) bool { return f.tags[a].Name < f.tags[b].Name })
	return created
}

// withTags 返回 e 加上 names 之后的标签（按名称排序）。mu 由调用方持有。
func (f *Library) withTags(e services.LibraryEntry, add []string, replace bool) []services.LibraryTag {
	tags := slices.Clone(e.Tags)
	if replace {
		tags = nil
	}
	for _, name := range add {
		if !slices.ContainsFunc(tags, func(t services.LibraryTag) bool { return t.Name == name }) {
			tags = append(tags, f.findTag(name))
		}
	}
	sort.SliceStable(tags, func(a, b int) bool { return tags[a].Name < tags[b].Name })
	if tags == nil {
		tags = []services.LibraryTag{}
	}
	return tags
}

// applyPatch 把补丁落到条目上。mu 由调用方持有。
func (f *Library) applyPatch(e services.LibraryEntry, p services.LibraryEntryPatch) services.LibraryEntry {
	if p.IsFavorite != nil {
		e.IsFavorite = *p.IsFavorite
	}
	if p.IsPinned != nil {
		e.IsPinned = *p.IsPinned
	}
	if p.CollectionID != nil {
		e.CollectionID = *p.CollectionID
	}
	if p.TagNames != nil {
		e.Tags = f.withTags(e, p.TagNames, true)
	}
	if len(p.AddTagNames) > 0 {
		e.Tags = f.withTags(e, p.AddTagNames, false)
	}
	if p.ReviewStatus != nil {
		e.ReviewStatus = *p.ReviewStatus
	}
	if p.ReviewReasons != nil {
		e.ReviewReasons = slices.Clone(p.ReviewReasons)
	}
	e.UpdatedAt = f.nowMs()
	return e
}

func (f *Library) indexOf(entries []services.LibraryEntry, id string) int {
	return slices.IndexFunc(entries, func(e services.LibraryEntry) bool { return e.ID == id })
}

// ---- services.Library 的条目操作 ----

func (f *Library) Freeze(ctx context.Context, q services.LibraryListQuery) ([]string, error) {
	if err := f.begin("Freeze", "Freeze"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.ops.freezes = append(f.ops.freezes, q)
	gate := f.ops.freezeGate
	f.ops.freezeGate = nil
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	hits := orderedHits(f.entries, q)
	ids := make([]string, 0, len(hits))
	for _, e := range hits {
		ids = append(ids, e.ID)
	}
	return ids, nil
}

// commandKey 是命令的规范形式：同一批次里换命令靠它判断（真实处理器比较命令的 JSON）。
func commandKey(cmd services.LibraryBatchCommand) string {
	raw, _ := json.Marshal(cmd)
	return string(raw)
}

func (f *Library) ExecuteBatch(ctx context.Context, runID string, ids []string, cmd services.LibraryBatchCommand) ([]services.LibraryBatchResult, error) {
	if err := f.begin("ExecuteBatch:"+cmd.Kind, "ExecuteBatch"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.ops.batches = append(f.ops.batches, BatchCall{RunID: runID, IDs: slices.Clone(ids), Command: cmd})
	gate := f.ops.batchGate
	f.ops.batchGate = nil
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ops.receipts == nil {
		f.ops.receipts = map[string]receipt{}
	}
	entries := slices.Clone(f.entries)
	key := commandKey(cmd)
	var results []services.LibraryBatchResult
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] { // 处理器按 [...new Set(ids)] 去重
			continue
		}
		seen[id] = true
		err := f.executeOne(&entries, runID, id, cmd, key)
		res := services.LibraryBatchResult{ID: id, OK: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		results = append(results, res)
	}
	f.entries = entries
	return results, nil
}

// executeOne 执行一条并记回执；返回 nil 表示成功（含「同一批次已提交过」）。mu 由调用方持有。
func (f *Library) executeOne(entries *[]services.LibraryEntry, runID, id string, cmd services.LibraryBatchCommand, key string) error {
	rkey := runID + "\x00" + id
	if runID != "" {
		if prev, ok := f.ops.receipts[rkey]; ok {
			if prev.command != key {
				return errBatchChangedOp // 回执原样保留
			}
			if prev.ok {
				return nil // 已提交过：复用，不重做
			}
		}
	}
	err := f.applyOne(entries, id, cmd)
	if runID != "" {
		f.ops.receipts[rkey] = receipt{command: key, ok: err == nil}
	}
	return err
}

func (f *Library) applyOne(entries *[]services.LibraryEntry, id string, cmd services.LibraryBatchCommand) error {
	i := f.indexOf(*entries, id)
	if i < 0 {
		return errBatchGone
	}
	e := (*entries)[i]
	deleted := e.DeletedAt != 0
	if cmd.Kind != services.LibraryBatchRestore && cmd.Kind != services.LibraryBatchDelete && deleted {
		return errBatchInTrash
	}
	if reason := f.ops.itemFails[id]; reason != "" {
		return errors.New(reason)
	}
	switch cmd.Kind {
	case services.LibraryBatchUpdate:
		if cmd.Patch == nil {
			return errors.New("Cannot read properties of undefined (reading 'addTagNames')")
		}
		e = f.applyPatch(e, *cmd.Patch)
	case services.LibraryBatchStatus:
		e.Status, e.UpdatedAt = cmd.Status, f.nowMs()
	case services.LibraryBatchTrash:
		e.DeletedAt, e.UpdatedAt = f.nowMs(), f.nowMs()
	case services.LibraryBatchRestore:
		if deleted {
			e.DeletedAt, e.UpdatedAt = 0, f.nowMs()
		}
	case services.LibraryBatchDelete:
		if !deleted {
			return errBatchNotTrash
		}
		if cmd.ClearEvidence {
			f.ops.cleared = append(f.ops.cleared, id)
		}
		*entries = slices.Delete(*entries, i, i+1)
		return nil
	default:
		return errBatchBadKind
	}
	(*entries)[i] = e
	return nil
}

func (f *Library) UpdateEntry(ctx context.Context, id string, patch services.LibraryEntryPatch) (services.LibraryEntry, bool, error) {
	if err := f.begin("UpdateEntry:"+id, "UpdateEntry"); err != nil {
		return services.LibraryEntry{}, false, err
	}
	f.mu.Lock()
	f.ops.updates = append(f.ops.updates, id)
	f.ops.updateCall = append(f.ops.updateCall, UpdateCall{ID: id, Patch: patch})
	gate := f.ops.updateGate
	f.ops.updateGate = nil
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return services.LibraryEntry{}, false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entries := slices.Clone(f.entries)
	i := f.indexOf(entries, id)
	if i < 0 {
		return services.LibraryEntry{}, false, nil
	}
	entries[i] = f.applyPatch(entries[i], patch)
	f.entries = entries
	updated := entries[i]
	updated.Snippet = "" // 真实的处理器返回完整条目（没有「摘要」这个字段）
	return updated, true, nil
}

func (f *Library) SupplementWechat(ctx context.Context, ids []string) error {
	if err := f.begin("SupplementWechat", "SupplementWechat"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops.supplement = append(f.ops.supplement, slices.Clone(ids))
	return nil
}

func (f *Library) AiHandoff(ctx context.Context, id string, full bool) (services.LibraryHandoff, error) {
	if err := f.begin("AiHandoff:"+id, "AiHandoff"); err != nil {
		return services.LibraryHandoff{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.ops.handoffErr[id]; err != nil {
		return services.LibraryHandoff{}, err
	}
	i := f.indexOf(f.entries, id)
	if i < 0 {
		return services.LibraryHandoff{}, services.ErrLibraryItemMissing
	}
	if pair, ok := f.ops.handoffs[id]; ok {
		if full {
			return pair[0], nil
		}
		return pair[1], nil
	}
	kind := "brief"
	if full {
		kind = "full"
	}
	text := fmt.Sprintf("handoff:%s:%s", id, kind)
	return services.LibraryHandoff{Title: f.entries[i].Title, Text: text, CharCount: utf8.RuneCountInString(text)}, nil
}

func (f *Library) ExportAiHandoff(ctx context.Context, title, text string) (services.LibraryExportResult, error) {
	if err := f.begin("ExportAiHandoff", "ExportAiHandoff"); err != nil {
		return services.LibraryExportResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops.exports = append(f.ops.exports, ExportCall{Title: title, Text: text})
	if f.ops.exportSet {
		return f.ops.exported, nil
	}
	return services.LibraryExportResult{Success: true, FilePath: "exported/" + title + ".md"}, nil
}
