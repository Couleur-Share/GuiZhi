package servicestest

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"guizhi-mygo-shell/native/services"
)

// 公众号网页快照（M2-5b）的假实现：读取快照、导出 HTML、补采任务与补采进度。规则取自真实处理器：
//   - 读取按（条目，版本）查预设的视图；版本为空读默认视图；没有预设的条目或版本是错误，不是空视图；
//   - 补采与旧的 SupplementWechat 走同一个批次记录（Supplemented），并返回预设的任务 ID（没预设就按条目逐个生成）；
//   - 任务状态按 ID 查预设（SeedImportStatuses），不存在的任务不在结果里；取消只对排队或处理中的任务生效。
//
// 全部数据与错误文本是 ASCII：native/ 下不放用户可见的中文字面量。

// 与真实处理器同义的失败（诊断文本，不是界面文案）。
var (
	errSnapshotItemMissing    = errors.New("item not found")
	errSnapshotVersionMissing = errors.New("version not found")
)

// SnapshotCall 是 WebSnapshot 与 ExportWebSnapshotHTML 收到的一次请求（含被 FailOn 拦下的）。
type SnapshotCall struct {
	ItemID    string
	VersionID string
}

// snapshotState 是网页快照的假实现状态；零值可用。
type snapshotState struct {
	views    map[string]services.LibraryWebSnapshotView
	exports  map[string]services.LibraryWebSnapshotExportResult
	tasks    []string
	statuses map[string]services.LibraryImportTaskStatus

	reads    []SnapshotCall
	exported []SnapshotCall
	canceled []string
	nextTask int

	// 各入口的闸门：让下一次调用卡住（HoldSnapshot 等）。
	readGate, exportGate, supplementGate, statusGate chan struct{}
}

func snapshotKey(itemID, versionID string) string { return itemID + "\x00" + versionID }

// cloneSnapshotView 返回深拷贝：预设值与调用方各持一份，互不影响。
func cloneSnapshotView(v services.LibraryWebSnapshotView) services.LibraryWebSnapshotView {
	if v.Version == nil {
		return v
	}
	version := *v.Version
	if version.Snapshot != nil {
		meta := *version.Snapshot
		meta.Assets = slices.Clone(meta.Assets)
		meta.Failures = slices.Clone(meta.Failures)
		meta.Warnings = slices.Clone(meta.Warnings)
		version.Snapshot = &meta
	}
	v.Version = &version
	return v
}

// SeedSnapshot 设定 WebSnapshot 对（条目，版本）的回复；versionID 为空是「不指定版本」的默认视图。
func (f *Library) SeedSnapshot(itemID, versionID string, view services.LibraryWebSnapshotView) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshot.views == nil {
		f.snapshot.views = map[string]services.LibraryWebSnapshotView{}
	}
	f.snapshot.views[snapshotKey(itemID, versionID)] = cloneSnapshotView(view)
}

// SeedSnapshotExport 设定 ExportWebSnapshotHTML 对某个条目（任何版本）的回复；没设定的条目导出成功并给一个固定路径。
func (f *Library) SeedSnapshotExport(itemID string, res services.LibraryWebSnapshotExportResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshot.exports == nil {
		f.snapshot.exports = map[string]services.LibraryWebSnapshotExportResult{}
	}
	f.snapshot.exports[itemID] = res
}

// SeedSupplementTasks 设定之后每次 SupplementWechatTasks 返回的任务 ID（整批原样返回）；不设定则每个条目生成一个。
func (f *Library) SeedSupplementTasks(taskIDs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot.tasks = slices.Clone(taskIDs)
}

// SeedImportStatuses 登记或更新导入任务的状态（按 ID 覆盖）；ImportTaskStatuses 只返回登记过的任务。
func (f *Library) SeedImportStatuses(statuses ...services.LibraryImportTaskStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshot.statuses == nil {
		f.snapshot.statuses = map[string]services.LibraryImportTaskStatus{}
	}
	for _, s := range statuses {
		f.snapshot.statuses[s.ID] = s
	}
}

// HoldSnapshot 让下一次 WebSnapshot 卡住（在读取之前），直到返回的函数被调用（只卡一次）。
func (f *Library) HoldSnapshot() (release func()) { return f.hold(&f.snapshot.readGate) }

// HoldSnapshotExport 让下一次 ExportWebSnapshotHTML 卡住（在导出之前）。
func (f *Library) HoldSnapshotExport() (release func()) { return f.hold(&f.snapshot.exportGate) }

// HoldSupplementTasks 让下一次 SupplementWechatTasks 卡住（在入队之前）。
func (f *Library) HoldSupplementTasks() (release func()) { return f.hold(&f.snapshot.supplementGate) }

// HoldImportStatuses 让下一次 ImportTaskStatuses 卡住（在读取之前）。
func (f *Library) HoldImportStatuses() (release func()) { return f.hold(&f.snapshot.statusGate) }

// SnapshotCalls 返回 WebSnapshot 收到的请求，按发生顺序（含被 FailOn 拦下的）。
func (f *Library) SnapshotCalls() []SnapshotCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.snapshot.reads)
}

// SnapshotExports 返回 ExportWebSnapshotHTML 收到的请求，按发生顺序（含被 FailOn 拦下的）。
func (f *Library) SnapshotExports() []SnapshotCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.snapshot.exported)
}

// CanceledImportTasks 返回 CancelImportTask 收到的任务 ID，按发生顺序（含没能取消的）。
func (f *Library) CanceledImportTasks() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.snapshot.canceled)
}

// WebSnapshot 实现 services.Library。
func (f *Library) WebSnapshot(ctx context.Context, itemID, versionID string) (services.LibraryWebSnapshotView, error) {
	err := f.begin("WebSnapshot:"+itemID, "WebSnapshot")
	f.mu.Lock()
	f.snapshot.reads = append(f.snapshot.reads, SnapshotCall{ItemID: itemID, VersionID: versionID})
	var gate chan struct{}
	if err == nil {
		gate = takeGate(&f.snapshot.readGate)
	}
	f.mu.Unlock()
	if err != nil {
		return services.LibraryWebSnapshotView{}, err
	}
	if gerr := waitGate(ctx, gate); gerr != nil {
		return services.LibraryWebSnapshotView{}, gerr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	view, ok := f.snapshot.views[snapshotKey(itemID, versionID)]
	if !ok {
		if versionID != "" {
			if _, hasItem := f.snapshot.views[snapshotKey(itemID, "")]; hasItem {
				return services.LibraryWebSnapshotView{}, errSnapshotVersionMissing
			}
		}
		return services.LibraryWebSnapshotView{}, errSnapshotItemMissing
	}
	return cloneSnapshotView(view), nil
}

// ExportWebSnapshotHTML 实现 services.Library。
func (f *Library) ExportWebSnapshotHTML(ctx context.Context, itemID, versionID string) (services.LibraryWebSnapshotExportResult, error) {
	err := f.begin("ExportWebSnapshotHTML:"+itemID, "ExportWebSnapshotHTML")
	f.mu.Lock()
	f.snapshot.exported = append(f.snapshot.exported, SnapshotCall{ItemID: itemID, VersionID: versionID})
	var gate chan struct{}
	if err == nil {
		gate = takeGate(&f.snapshot.exportGate)
	}
	f.mu.Unlock()
	if err != nil {
		return services.LibraryWebSnapshotExportResult{}, err
	}
	if gerr := waitGate(ctx, gate); gerr != nil {
		return services.LibraryWebSnapshotExportResult{}, gerr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if res, ok := f.snapshot.exports[itemID]; ok {
		return res, nil
	}
	return services.LibraryWebSnapshotExportResult{Path: "/export/" + itemID + "/" + versionID}, nil
}

// SupplementWechatTasks 实现 services.Library：与 SupplementWechat 共用 Supplemented 的批次记录。
func (f *Library) SupplementWechatTasks(ctx context.Context, ids []string) ([]string, error) {
	err := f.begin("SupplementWechatTasks", "SupplementWechatTasks")
	f.mu.Lock()
	f.ops.supplement = append(f.ops.supplement, slices.Clone(ids))
	var gate chan struct{}
	if err == nil {
		gate = takeGate(&f.snapshot.supplementGate)
	}
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if gerr := waitGate(ctx, gate); gerr != nil {
		return nil, gerr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshot.tasks != nil {
		return slices.Clone(f.snapshot.tasks), nil
	}
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] { // 处理器按 ID 去重
			continue
		}
		seen[id] = true
		f.snapshot.nextTask++
		out = append(out, fmt.Sprintf("snap-task-%d", f.snapshot.nextTask))
	}
	return out, nil
}

// ImportTaskStatuses 实现 services.Library：按请求的顺序返回登记过的任务。
func (f *Library) ImportTaskStatuses(ctx context.Context, taskIDs []string) ([]services.LibraryImportTaskStatus, error) {
	if err := f.begin("ImportTaskStatuses", "ImportTaskStatuses"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	gate := takeGate(&f.snapshot.statusGate)
	f.mu.Unlock()
	if err := waitGate(ctx, gate); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]services.LibraryImportTaskStatus, 0, len(taskIDs))
	for _, id := range taskIDs {
		if s, ok := f.snapshot.statuses[id]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// CancelImportTask 实现 services.Library：排队或处理中的任务变成已取消并返回真；其余返回假、状态不变。
func (f *Library) CancelImportTask(ctx context.Context, taskID string) (bool, error) {
	err := f.begin("CancelImportTask:"+taskID, "CancelImportTask")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot.canceled = append(f.snapshot.canceled, taskID)
	if err != nil {
		return false, err
	}
	s, ok := f.snapshot.statuses[taskID]
	if !ok || s.Finished() {
		return false, nil
	}
	s.Status = services.ImportStatusCanceled
	f.snapshot.statuses[taskID] = s
	return true, nil
}
