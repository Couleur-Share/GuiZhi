package library

import (
	"context"
	"slices"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 条目的写操作（渲染层 knowledge.store 的 moveToTrash、restoreItems、deleteForever、emptyTrash、setStatus、bulkUpdate、
// bulkMoveToCollection、toggleFavorite、togglePinned、setItemTags）。
//
// 对若干条的操作都经 store_batch.go 的 execute（逐条记账、可重试）；单条的收藏、置顶与改标签直接走 UpdateEntry，
// 先在本地行上回显、再重取列表与计数；返回的条目同时并进详情（ApplyServerEntry），详情里还没落盘的编辑叠在上面。
// 选中条目的改标签不同：它走详情的草稿（store_detail.go），与正文、标题的编辑一起带基线保存。

// MoveToTrash 把条目移到回收站，返回是否全部成功——调用方据此决定要不要弹「已移到回收站」的撤销提示，
// 否则删失败了还会弹一句成功文案，用户以为删掉了。
func (s *Store) MoveToTrash(ctx context.Context, ids []string) bool {
	return s.execute(ctx, ids, services.LibraryBatchCommand{Kind: services.LibraryBatchTrash}, false)
}

// RestoreItems 把回收站里的条目恢复。
func (s *Store) RestoreItems(ctx context.Context, ids []string) bool {
	return s.execute(ctx, ids, services.LibraryBatchCommand{Kind: services.LibraryBatchRestore}, false)
}

// DeleteForever 彻底删除（只对回收站里的条目有效）；clearEvidence 为真时同时清掉问答里引用它们的证据。
func (s *Store) DeleteForever(ctx context.Context, ids []string, clearEvidence bool) bool {
	return s.execute(ctx, ids, services.LibraryBatchCommand{Kind: services.LibraryBatchDelete, ClearEvidence: clearEvidence}, false)
}

// EmptyTrash 清空回收站：先冻结回收站里现有的全部条目，再逐条彻底删除（渲染层 emptyTrash）。冻结失败走变更失败通道。
func (s *Store) EmptyTrash(ctx context.Context, clearEvidence bool) bool {
	ids, err := s.svc.Freeze(ctx, services.LibraryListQuery{Scope: services.LibraryScopeTrash})
	if err != nil {
		return s.guard.Report(locales.LibraryEmptyTrash(), err)
	}
	return s.DeleteForever(ctx, ids, clearEvidence)
}

// SetStatus 改状态（归档、取消归档）。
func (s *Store) SetStatus(ctx context.Context, ids []string, status string) bool {
	return s.execute(ctx, ids, services.LibraryBatchCommand{Kind: services.LibraryBatchStatus, Status: status}, false)
}

// BulkUpdate 批量改字段（收藏、分类、追加标签…）。追加标签可能建了新标签，侧栏的标签列表要跟上——
// 与渲染层一样，不论这一批成败都重读。
func (s *Store) BulkUpdate(ctx context.Context, ids []string, patch services.LibraryEntryPatch) bool {
	ok := s.execute(ctx, ids, services.LibraryBatchCommand{Kind: services.LibraryBatchUpdate, Patch: &patch}, false)
	if len(patch.AddTagNames) > 0 {
		s.FetchTags(ctx)
	}
	return ok
}

// BulkMoveToCollection 把条目移到合集；collectionID 为空表示移出合集（未归合集）。
func (s *Store) BulkMoveToCollection(ctx context.Context, ids []string, collectionID string) bool {
	return s.BulkUpdate(ctx, ids, services.LibraryEntryPatch{CollectionID: services.StringPtr(collectionID)})
}

// ---- 单条 ----

// flagKey 是在途翻转的键。
func flagKey(favorite bool, id string) string {
	if favorite {
		return "favorite:" + id
	}
	return "pinned:" + id
}

// ToggleFavorite 翻转收藏。
func (s *Store) ToggleFavorite(ctx context.Context, id string) bool {
	return s.toggleFlag(ctx, id, true)
}

// TogglePinned 翻转置顶。
func (s *Store) TogglePinned(ctx context.Context, id string) bool {
	return s.toggleFlag(ctx, id, false)
}

// toggleFlag 翻转收藏或置顶（渲染层 toggleFlag）。
//
// 两者原本一个只改本地行、一个整表重取：在「收藏」范围里取消收藏，条目会留在列表里不走；而置顶会让整个列表重排、
// 滚动位置丢失。统一成：本地先回显，再重取列表与计数（两者都会改变过滤与排序结果）。
// 翻转值以在途请求为准，连点两次不会都发同一个值。
func (s *Store) toggleFlag(ctx context.Context, id string, favorite bool) bool {
	return s.guard.Run(locales.LibraryActionSetFlag(), func() error {
		key := flagKey(favorite, id)
		s.mu.Lock()
		known := false
		if sel := s.view.Detail.Item; sel != nil && sel.ID == id {
			known = sel.IsPinned // 选中的条目以详情里的值为准
			if favorite {
				known = sel.IsFavorite
			}
		} else if i := slices.IndexFunc(s.view.List.Entries, func(e services.LibraryEntry) bool { return e.ID == id }); i >= 0 {
			e := s.view.List.Entries[i]
			known = e.IsPinned
			if favorite {
				known = e.IsFavorite
			}
		}
		if inflight, ok := s.pendingFlags[key]; ok {
			known = inflight
		}
		next := !known
		s.pendingFlags[key] = next
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			if v, ok := s.pendingFlags[key]; ok && v == next {
				delete(s.pendingFlags, key)
			}
			s.mu.Unlock()
		}()

		patch := services.LibraryEntryPatch{IsPinned: services.BoolPtr(next)}
		if favorite {
			patch = services.LibraryEntryPatch{IsFavorite: services.BoolPtr(next)}
		}
		updated, found, err := s.svc.UpdateEntry(ctx, id, patch)
		if err != nil {
			return err
		}
		if found {
			s.ApplyServerEntry(updated)
		}
		s.RefreshAll(ctx)
		return nil
	})
}

// SetItemTags 整体替换一条的标签（渲染层 setItemTags）：
// 选中的条目走详情的草稿（写进待保存并立即落盘，失败显示在详情头部，不弹提示）；
// 其余条目直接写回这一行，再重读标签与计数——可能新建了标签。
func (s *Store) SetItemTags(ctx context.Context, id string, tagNames []string) bool {
	if tagNames == nil {
		tagNames = []string{} // nil 表示「不改」，空表示「清空」
	}
	s.mu.Lock()
	selected := s.view.Detail.Item != nil && s.view.Detail.Item.ID == id && s.view.List.SelectedID == id
	s.mu.Unlock()
	if selected {
		s.UpdateSelected(ctx, services.LibraryDraftFields{TagNames: tagNames})
		return s.FlushPendingSave(ctx)
	}
	return s.guard.Run(locales.LibraryActionSetTags(), func() error {
		updated, found, err := s.svc.UpdateEntry(ctx, id, services.LibraryEntryPatch{TagNames: tagNames})
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		s.ApplyServerEntry(updated)
		s.FetchTags(ctx)
		s.RefreshCounts(ctx)
		return nil
	})
}

// SupplementWechat 把公众号文章补采为网页快照（渲染层 supplementWechat）；按 50 一块排队。
func (s *Store) SupplementWechat(ctx context.Context, ids []string) bool {
	return s.guard.Run(locales.LibrarySnapshotSupplementWechatBulk(), func() error {
		for offset := 0; offset < len(ids); offset += batchChunk {
			if err := s.svc.SupplementWechat(ctx, ids[offset:min(offset+batchChunk, len(ids))]); err != nil {
				return err
			}
		}
		return nil
	})
}

// applyEntry 把服务端返回的条目并回列表里的同一行（渲染层 applyItemToList）：只换标题、状态、分类、收藏、置顶、更新时间与标签，
// 摘要、类型等列表专属字段保持原样。Entries 整体替换，EntriesRev 加一。
func (s *Store) applyEntry(updated services.LibraryEntry) {
	s.mu.Lock()
	s.applyEntryLocked(updated)
	s.mu.Unlock()
	s.wake()
}

// applyEntryLocked 是 applyEntry 的持锁版本，不唤醒界面。mu 由调用方持有。
func (s *Store) applyEntryLocked(updated services.LibraryEntry) {
	l := &s.view.List
	entries := slices.Clone(l.Entries)
	for i := range entries {
		if entries[i].ID != updated.ID {
			continue
		}
		e := &entries[i]
		e.Title, e.Status, e.CollectionID = updated.Title, updated.Status, updated.CollectionID
		e.IsFavorite, e.IsPinned, e.UpdatedAt, e.Tags = updated.IsFavorite, updated.IsPinned, updated.UpdatedAt, updated.Tags
	}
	l.Entries = entries
	l.EntriesRev++
}
