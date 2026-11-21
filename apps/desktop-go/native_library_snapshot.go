package main

// 公众号网页快照（viva/screens/library 的快照面板）与主包之间的适配层：读取快照、导出 HTML、补采入队，
// 以及轮询补采进度所需的导入任务状态与取消。与渲染进程走同一份处理器、同一套错误；
// DTO 与处理器输出的对应关系由适配器测试用真实处理器守住（native_library_snapshot_test.go）。
//
// IPC 映射：
//   WebSnapshot            → web:snapshot（回复壳 { ok, data, error }；data 即 WebSnapshotView）
//   ExportWebSnapshotHTML  → web:snapshot-export（回复壳；data 为 { canceled | path, incomplete }）
//   SupplementWechatTasks  → web:snapshot-enqueue（回复壳；data 为新建的导入任务数组，只取 id）
//   ImportTaskStatuses     → import:list（对象参数走分页，取 entries 与 active，只留补采进度要的字段）
//   CancelImportTask       → import:cancel（回复 boolean）

import (
	"context"
	"errors"

	"guizhi-mygo-shell/native/services"
)

// 轮询补采进度时翻页的上限：每页 100 条、最多 10 页。补采任务总是刚建的，通常在第一页；
// 翻页只是为了在之后又入队了大量任务时仍能找到较早的补采任务，不为遍历整个历史。
const (
	importStatusPageSize = 100
	importStatusMaxPages = 10
)

// webData 解开 web:* 处理器的回复壳（preload 的 WebResponse）：ok 为假时带处理器给的原因，没给用 fallback。
// ok 为真但没有 data 返回 nil，由调用方决定怎么处理。
func webData[T any](ctx context.Context, b *Bridge, channel, fallback string, args ...any) (*T, error) {
	reply, err := callAs[struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Data  *T     `json:"data"`
	}](ctx, b, channel, args...)
	if err != nil {
		return nil, err
	}
	if !reply.OK {
		if reply.Error != "" {
			return nil, errors.New(reply.Error)
		}
		return nil, errors.New(fallback)
	}
	return reply.Data, nil
}

// normalizeSnapshotMeta 让三个切片永不为 nil：处理器只在有内容时输出键，界面按长度判断缺失。
func normalizeSnapshotMeta(m *services.LibraryWebSnapshotMeta) {
	if m == nil {
		return
	}
	if m.Assets == nil {
		m.Assets = []services.LibraryWebSnapshotAsset{}
	}
	if m.Failures == nil {
		m.Failures = []services.LibraryWebSnapshotFailure{}
	}
	if m.Warnings == nil {
		m.Warnings = []string{}
	}
}

// WebSnapshot 对应 web:snapshot。versionID 为空表示不指定版本（处理器把 null 与缺省都当作默认版本）。
// 快照损坏等业务状态在回复 data 的 error 里，原样放进视图的 Error，不是 Go 错误；条目或版本不存在才是 error。
func (a libraryAdapter) WebSnapshot(ctx context.Context, itemID, versionID string) (services.LibraryWebSnapshotView, error) {
	args := []any{itemID}
	if versionID != "" {
		args = append(args, versionID)
	}
	view, err := webData[services.LibraryWebSnapshotView](ctx, a.b, "web:snapshot", "读取网页快照失败", args...)
	if err != nil {
		return services.LibraryWebSnapshotView{}, err
	}
	if view == nil {
		return services.LibraryWebSnapshotView{}, errors.New("读取网页快照失败")
	}
	if view.Version != nil {
		normalizeSnapshotMeta(view.Version.Snapshot)
	}
	return *view, nil
}

// ExportWebSnapshotHTML 对应 web:snapshot-export：没有目录选择框或用户取消都回 canceled。
func (a libraryAdapter) ExportWebSnapshotHTML(ctx context.Context, itemID, versionID string) (services.LibraryWebSnapshotExportResult, error) {
	res, err := webData[services.LibraryWebSnapshotExportResult](ctx, a.b, "web:snapshot-export", "导出网页快照失败", itemID, versionID)
	if err != nil {
		return services.LibraryWebSnapshotExportResult{}, err
	}
	if res == nil {
		return services.LibraryWebSnapshotExportResult{}, errors.New("导出网页快照失败")
	}
	return *res, nil
}

// SupplementWechatTasks 对应 web:snapshot-enqueue：回复的 data 是新建的导入任务数组，这里只取 ID。
func (a libraryAdapter) SupplementWechatTasks(ctx context.Context, ids []string) ([]string, error) {
	tasks, err := webData[[]struct {
		ID string `json:"id"`
	}](ctx, a.b, "web:snapshot-enqueue", "补采公众号网页失败", ids)
	if err != nil {
		return nil, err
	}
	out := []string{}
	if tasks != nil {
		for _, t := range *tasks {
			if t.ID != "" {
				out = append(out, t.ID)
			}
		}
	}
	return out, nil
}

// ImportTaskStatuses 对应 import:list：用对象参数走分页（不传参数的形式只回第一页数组），
// 合并 entries 与 active（active 是全部排队与处理中的任务，不受分页影响），按请求的顺序返回找到的任务。
func (a libraryAdapter) ImportTaskStatuses(ctx context.Context, taskIDs []string) ([]services.LibraryImportTaskStatus, error) {
	out := []services.LibraryImportTaskStatus{}
	if len(taskIDs) == 0 {
		return out, nil
	}
	want := make(map[string]bool, len(taskIDs))
	for _, id := range taskIDs {
		want[id] = true
	}
	found := map[string]services.LibraryImportTaskStatus{}
	collect := func(tasks []services.ImportTask) {
		for _, t := range tasks {
			if want[t.ID] {
				found[t.ID] = services.LibraryImportTaskStatus{
					ID: t.ID, Status: t.Status, RefreshOfItemID: t.RefreshOfItemID, Error: t.Error, Warning: t.Warning,
				}
			}
		}
	}
	cursor := ""
	for page := 0; page < importStatusMaxPages; page++ {
		arg := map[string]any{"origin": "all", "status": "all", "pageSize": importStatusPageSize}
		if cursor != "" {
			arg["cursor"] = cursor
		}
		res, err := callAs[services.ImportPage](ctx, a.b, "import:list", arg)
		if err != nil {
			return nil, err
		}
		collect(res.Active)
		collect(res.Entries)
		if len(found) == len(want) || res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	seen := map[string]bool{}
	for _, id := range taskIDs {
		if s, ok := found[id]; ok && !seen[id] {
			seen[id] = true
			out = append(out, s)
		}
	}
	return out, nil
}

// CancelImportTask 对应 import:cancel：回复 true 表示取消了，false 表示任务已结束或不存在。
func (a libraryAdapter) CancelImportTask(ctx context.Context, taskID string) (bool, error) {
	return callAs[bool](ctx, a.b, "import:cancel", taskID)
}
