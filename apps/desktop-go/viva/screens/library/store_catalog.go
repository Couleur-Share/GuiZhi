package library

import (
	"context"
	"errors"
	"fmt"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 合集与标签（渲染层 collection.store、tag.store）。两者的读取、改写与「先隐藏、窗口内可撤销、之后才提交」的删除
// 是同一套规则，这里只写一遍，按种类取各自的服务调用。

type catalogKind uint8

const (
	kindCollection catalogKind = iota
	kindTag
)

// pendingKey 标识一次撤销窗口内的删除。
type pendingKey struct {
	kind catalogKind
	id   string
}

// Deletion 是一次待提交的删除：窗口内目标只是从界面隐藏。
type Deletion struct {
	// Undo 在窗口内撤销：目标原样回来。已经提交或撤销过时什么都不做。
	Undo func()
	// Commit 立即提交；窗口结束时自动调用。
	Commit func()
}

// catalogOps 是一种目录（合集或标签）的差异：提示里的动作名、服务调用与重新读取。
type catalogOps struct {
	label   func() string
	exists  func(v View, id string) bool
	remove  func(ctx context.Context, id string) (bool, error)
	missing func() error // 服务端说目标已不存在时的错误（渲染层是写死的中文 Error，不走 i18n）
	refetch func(ctx context.Context)
}

func (s *Store) ops(k catalogKind) catalogOps {
	if k == kindCollection {
		return catalogOps{
			label: locales.LibraryDeleteCollection,
			exists: func(v View, id string) bool {
				for _, c := range v.Collections {
					if c.ID == id {
						return true
					}
				}
				return false
			},
			remove:  s.svc.DeleteCollection,
			missing: func() error { return errors.New("合集已不存在，请刷新列表") },
			refetch: s.FetchCollections,
		}
	}
	return catalogOps{
		label: locales.LibraryDeleteTag,
		exists: func(v View, id string) bool {
			for _, t := range v.Tags {
				if t.ID == id {
					return true
				}
			}
			return false
		},
		remove:  s.svc.DeleteTag,
		missing: func() error { return errors.New("标签已不存在，请刷新列表") },
		refetch: s.FetchTags,
	}
}

// visibleCollectionsLocked 去掉撤销窗口里已隐藏的合集（渲染层 hidePendingDeletions）；没有隐藏的原样返回。
func (s *Store) visibleCollectionsLocked(all []services.LibraryCollection) []services.LibraryCollection {
	if len(s.pending) == 0 {
		return all
	}
	out := make([]services.LibraryCollection, 0, len(all))
	for _, c := range all {
		if _, hidden := s.pending[pendingKey{kindCollection, c.ID}]; !hidden {
			out = append(out, c)
		}
	}
	return out
}

// visibleTagsLocked 是标签的 visibleCollectionsLocked。
func (s *Store) visibleTagsLocked(all []services.LibraryTag) []services.LibraryTag {
	if len(s.pending) == 0 {
		return all
	}
	out := make([]services.LibraryTag, 0, len(all))
	for _, t := range all {
		if _, hidden := s.pending[pendingKey{kindTag, t.ID}]; !hidden {
			out = append(out, t)
		}
	}
	return out
}

// ---- 读取与改写 ----

// FetchCollections 重新读取合集。读取失败不打扰用户（没有用户动作），只进日志。
func (s *Store) FetchCollections(ctx context.Context) {
	list, err := s.svc.Collections(ctx)
	if err != nil {
		s.guard.Background("library.collections", fmt.Errorf("加载集合列表失败：%w", err))
		return
	}
	s.mu.Lock()
	s.view.Collections = s.visibleCollectionsLocked(list)
	s.mu.Unlock()
	s.wake()
}

// FetchTags 重新读取标签。
func (s *Store) FetchTags(ctx context.Context) {
	list, err := s.svc.Tags(ctx)
	if err != nil {
		s.guard.Background("library.tags", fmt.Errorf("加载标签列表失败：%w", err))
		return
	}
	s.mu.Lock()
	s.view.Tags = s.visibleTagsLocked(list)
	s.mu.Unlock()
	s.wake()
}

// CreateCollection 新建合集并刷新列表；失败原样返回，由调用方（新建弹窗）提示并保持打开。icon 为空表示默认图标。
func (s *Store) CreateCollection(ctx context.Context, name, icon string) (services.LibraryCollection, error) {
	created, err := s.svc.CreateCollection(ctx, name, icon)
	if err != nil {
		return services.LibraryCollection{}, err
	}
	s.FetchCollections(ctx)
	return created, nil
}

// UpdateCollection 改合集的名称与图标并刷新列表；失败原样返回。
func (s *Store) UpdateCollection(ctx context.Context, id, name, icon string) error {
	if err := s.svc.UpdateCollection(ctx, id, name, icon); err != nil {
		return err
	}
	s.FetchCollections(ctx)
	return nil
}

// UpdateTag 改标签并刷新列表；失败原样返回。
func (s *Store) UpdateTag(ctx context.Context, id string, patch services.LibraryTagPatch) error {
	if err := s.svc.UpdateTag(ctx, id, patch); err != nil {
		return err
	}
	s.FetchTags(ctx)
	return nil
}

// RenameTag 给标签改名：改完刷新全部（条目列表上的标签名要跟着变；渲染层 submitEditModal 的 rename-tag 分支）。
func (s *Store) RenameTag(ctx context.Context, id, name string) error {
	if err := s.UpdateTag(ctx, id, services.LibraryTagPatch{Name: &name}); err != nil {
		return err
	}
	s.RefreshAll(ctx)
	return nil
}

// SetTagColor 换标签颜色并刷新全部（渲染层标签菜单的颜色项：updateTag 之后 refreshAll）。
// 渲染层里这一步失败是未处理的 Promise 拒绝，用户什么都看不到；这里走变更失败通道，让用户知道没改成（台账 C-005）。
func (s *Store) SetTagColor(ctx context.Context, id, colorKey string) bool {
	return s.guard.Run(locales.LibraryActionSetTags(), func() error {
		if err := s.UpdateTag(ctx, id, services.LibraryTagPatch{ColorKey: &colorKey}); err != nil {
			return err
		}
		s.RefreshAll(ctx)
		return nil
	})
}

// ---- 删除（先隐藏、窗口内可撤销、之后才提交）----

// ScheduleDeleteCollection 删除合集（其中条目保留为未归合集）：先从界面隐藏，窗口内可撤销，之后才提交。
// 合集不存在或已在撤销窗口内时返回 nil。delay 非正取 DeletionUndoWindow。
func (s *Store) ScheduleDeleteCollection(ctx context.Context, id string, delay time.Duration) *Deletion {
	return s.scheduleDelete(ctx, kindCollection, id, delay)
}

// ScheduleDeleteTag 删除标签（条目保留，只摘掉标签）：规则同 ScheduleDeleteCollection。
func (s *Store) ScheduleDeleteTag(ctx context.Context, id string, delay time.Duration) *Deletion {
	return s.scheduleDelete(ctx, kindTag, id, delay)
}

func (s *Store) scheduleDelete(ctx context.Context, k catalogKind, id string, delay time.Duration) *Deletion {
	if delay <= 0 {
		delay = DeletionUndoWindow
	}
	op := s.ops(k)
	key := pendingKey{k, id}

	s.mu.Lock()
	if _, hidden := s.pending[key]; hidden || !op.exists(s.view, id) {
		s.mu.Unlock()
		return nil
	}
	s.pending[key] = struct{}{}
	s.view.Collections = s.visibleCollectionsLocked(s.view.Collections)
	s.view.Tags = s.visibleTagsLocked(s.view.Tags)
	commitID := s.nextCommit
	s.nextCommit++
	s.mu.Unlock()
	s.wake()

	// settle 把这次删除标为已了结并取消定时器；已经了结过的返回 false（提交与撤销只有一个能生效）。
	var (
		settled  bool
		stopTick func()
	)
	settle := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if settled {
			return false
		}
		settled = true
		if stopTick != nil {
			stopTick()
		}
		return true
	}
	release := func() {
		s.mu.Lock()
		delete(s.pending, key)
		delete(s.pendingCommits, commitID)
		s.mu.Unlock()
	}
	commit := func() {
		if !settle() {
			return
		}
		// 提交期间保持隐藏，避免提交中的刷新让被删的行闪回。
		s.guard.Run(op.label(), func() error {
			removed, err := op.remove(ctx, id)
			if err != nil {
				return err
			}
			if !removed {
				return op.missing()
			}
			return nil
		})
		release()
		// 提交失败的目标会随这次刷新回到列表，并已有原因提示。
		op.refetch(ctx)
		s.RefreshAll(ctx)
	}
	undo := func() {
		if !settle() {
			return
		}
		release()
		op.refetch(ctx)
	}
	stop := s.After(delay, commit)
	s.mu.Lock()
	stopTick = stop
	s.pendingCommits[commitID] = commit
	s.mu.Unlock()
	return &Deletion{Undo: undo, Commit: commit}
}

// FlushRemovals 立即提交所有尚在撤销窗口内的删除；窗口关闭前调用，避免关窗口把「已删除」的行又带回来。
func (s *Store) FlushRemovals() {
	s.mu.Lock()
	commits := make([]func(), 0, len(s.pendingCommits))
	for _, commit := range s.pendingCommits {
		commits = append(commits, commit)
	}
	s.mu.Unlock()
	for _, commit := range commits {
		commit()
	}
}
