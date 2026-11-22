package wiki

import (
	"fmt"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// readPage 并行读取一页的详情与历史版本；详情为 nil 表示页面不存在。
func (s *Store) readPage(id string) (detail *services.WikiPageDetail, revisions []services.WikiRevision, err error) {
	var (
		errDetail, errRevisions error
		wg                      sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); detail, errDetail = s.svc.GetPage(s.ctx(), id) }()
	go func() { defer wg.Done(); revisions, errRevisions = s.svc.ListRevisions(s.ctx(), id) }()
	wg.Wait()
	if err = firstError(errDetail, errRevisions); err != nil {
		return nil, nil, err
	}
	return detail, revisions, nil
}

// clearSelectionLocked 取消选中并丢弃详情、历史版本与草稿（持有 s.mu 时调用）；选页代次加一，在途的读取作废。
func (s *Store) clearSelectionLocked() {
	s.selectGen++
	v := &s.view
	v.SelectedPageID, v.Detail, v.Revisions = "", nil, nil
	v.PageLoading, v.PageError = false, ""
	v.Editing, v.Draft = false, ""
}

// followSuggestion 让更新建议状态跟随当前页：detail 为 nil 表示没有选中页。
func (s *Store) followSuggestion(detail *services.WikiPageDetail) {
	if detail == nil {
		s.suggestion.clear()
		return
	}
	s.suggestion.follow(detail.Page.ID, detail.Page.UpdatedAt)
}

// SelectPage 选中一页并读取详情与历史版本；id 为空取消选中。
// 切到另一页时旧详情与未保存的草稿立即丢弃（避免把 A 的编辑写进 B、也不让界面在 B 的标题下显示 A 的内容）；
// 等待期间又点了别的页，这次结果作废。读取失败设置 PageError（带重试：再次 SelectPage）；页面已不存在则取消选中。
func (s *Store) SelectPage(id string) {
	if id == "" {
		s.mu.Lock()
		s.clearSelectionLocked()
		s.mu.Unlock()
		s.wake()
		s.suggestion.clear()
		return
	}
	s.mu.Lock()
	s.selectGen++
	gen := s.selectGen
	v := &s.view
	if v.SelectedPageID != id {
		v.Detail, v.Revisions = nil, nil
		v.Editing, v.Draft = false, ""
	}
	v.SelectedPageID, v.PageLoading, v.PageError = id, true, ""
	s.mu.Unlock()
	s.wake()

	detail, revisions, err := s.readPage(id)

	s.mu.Lock()
	if gen != s.selectGen {
		s.mu.Unlock()
		return // 已被更新的选择取代：它负责收尾（含 PageLoading）
	}
	switch {
	case err != nil:
		s.view.PageLoading, s.view.PageError = false, err.Error()
	case detail == nil:
		s.clearSelectionLocked()
	default:
		s.view.Detail, s.view.Revisions, s.view.PageLoading = detail, revisions, false
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("wiki.selectPage", err)
	}
	s.wake()
	if err == nil {
		s.followSuggestion(detail)
	}
}

// reloadIfSelected 在页面被改写后重读它——但只在它仍是选中页时：保存期间用户可能已经切到别的页，不能把视图拉回来。
func (s *Store) reloadIfSelected(pageID string) {
	s.mu.Lock()
	still := s.view.SelectedPageID == pageID
	s.mu.Unlock()
	if still {
		s.SelectPage(pageID)
	}
}

// OpenByLinkTarget 按 [[链接]] 目标跳转：规范化标题精确匹配优先，其次别名。找不到弹出「页面不存在」提示并返回 false。
func (s *Store) OpenByLinkTarget(target string) bool {
	s.mu.Lock()
	catalog := s.view.Catalog
	s.mu.Unlock()
	id, ok := ResolveTarget(catalog, target)
	if !ok {
		s.linkNotFound(target)
		return false
	}
	s.SelectPage(id)
	return true
}

// ---- 编辑 ----

// BeginEdit 进入正文编辑，草稿取当前正文。没有详情（还在加载、读取失败）或已在编辑时忽略。
func (s *Store) BeginEdit() {
	s.mu.Lock()
	if s.view.Detail == nil || s.view.Editing {
		s.mu.Unlock()
		return
	}
	s.view.Editing, s.view.Draft = true, s.view.Detail.Page.Body
	s.mu.Unlock()
	s.wake()
}

// SetDraft 更新编辑框内容；不在编辑中时忽略。
func (s *Store) SetDraft(text string) {
	s.mu.Lock()
	if !s.view.Editing {
		s.mu.Unlock()
		return
	}
	s.view.Draft = text
	s.mu.Unlock()
	s.wake()
}

// CancelEdit 放弃草稿，退出编辑。
func (s *Store) CancelEdit() {
	s.mu.Lock()
	s.view.Editing, s.view.Draft = false, ""
	s.mu.Unlock()
	s.wake()
}

// discardDraftFor 在页面内容被回滚、采用建议等整体改写后丢弃草稿（草稿基于旧正文）；只对仍选中的那一页生效。
func (s *Store) discardDraftFor(pageID string) {
	s.mu.Lock()
	if s.view.SelectedPageID == pageID {
		s.view.Editing, s.view.Draft = false, ""
	}
	s.mu.Unlock()
}

// SaveDraft 保存编辑框内容（落标记后下一轮编译不再覆盖这一页）。成功弹提示、退出编辑并返回 true；
// 失败保留草稿、弹出带原因的错误提示并返回 false。保存期间用户又改了草稿，则保存的是点击那一刻的内容，新改动留在编辑框里。
func (s *Store) SaveDraft() bool {
	s.mu.Lock()
	editing, draft, pageID := s.view.Editing, s.view.Draft, s.view.SelectedPageID
	s.mu.Unlock()
	if !editing {
		return false
	}
	if !s.SavePageBody(draft, false) {
		return false
	}
	s.mu.Lock()
	if s.view.SelectedPageID == pageID && s.view.Draft == draft {
		s.view.Editing, s.view.Draft = false, ""
	}
	s.mu.Unlock()
	s.wake()
	s.guard.Succeed(locales.WikiPageSaved())
	return true
}

// ReleaseToAuto 把手动编辑过的页面交回自动编译：按当前正文原样再存一次并清除手动标记。
func (s *Store) ReleaseToAuto() bool {
	s.mu.Lock()
	detail := s.view.Detail
	s.mu.Unlock()
	if detail == nil {
		s.guard.Reject(locales.WikiPageSaveFailed())
		return false
	}
	if !s.SavePageBody(detail.Page.Body, true) {
		return false
	}
	s.guard.Succeed(locales.WikiReleasedToAuto())
	return true
}

// ---- 写操作 ----

// attempt 执行一次写操作并统一回执：异常（含 panic）与「目标不存在」（false 且无错误）都弹出失败提示，
// 前者带可复制的详情；返回是否真的改成功。
func (s *Store) attempt(title, logAction string, fn func() (bool, error)) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			s.guard.Fail(title, logAction, fmt.Errorf("panic: %v", r))
			ok = false
		}
	}()
	done, err := fn()
	if err != nil {
		return s.guard.Fail(title, logAction, err)
	}
	if !done {
		s.guard.Reject(title)
		return false
	}
	return true
}

// SavePageBody 改写当前选中页的正文（releaseToAuto 为 true 时同时清除手动编辑标记）：出链按新正文重建，
// 否则图谱与反向链接停留在改动之前。成功后重读这一页（仍选中才读）与目录；不弹成功提示（由 SaveDraft / ReleaseToAuto 各自提示）。
func (s *Store) SavePageBody(body string, releaseToAuto bool) bool {
	s.mu.Lock()
	pageID, catalog := s.view.SelectedPageID, s.view.Catalog
	s.mu.Unlock()
	if pageID == "" {
		s.guard.Reject(locales.WikiPageSaveFailed())
		return false
	}
	in := services.WikiUpdatePageInput{PageID: pageID, Body: body, LinkTargets: LinkTargets(body, catalog), ReleaseToAuto: releaseToAuto}
	if !s.attempt(locales.WikiPageSaveFailed(), "wiki.savePage", func() (bool, error) { return s.svc.UpdatePage(s.ctx(), in) }) {
		return false
	}
	s.reloadIfSelected(pageID)
	s.Refresh()
	return true
}

// DeletePage 删除一页（清理残留知识不必再清空全库重编）。成功弹提示、取消选中（若删的是当前页）并刷新目录。
func (s *Store) DeletePage(pageID string) bool {
	if !s.attempt(locales.WikiPageDeleteFailed(), "wiki.deletePage", func() (bool, error) { return s.svc.DeletePage(s.ctx(), pageID) }) {
		return false
	}
	s.mu.Lock()
	if s.view.SelectedPageID == pageID {
		s.clearSelectionLocked()
	}
	s.mu.Unlock()
	s.wake()
	s.suggestion.clearIf(pageID)
	s.Refresh()
	s.guard.Succeed(locales.WikiPageDeleted())
	return true
}

// RestorePreviousRevision 把当前页回滚到最近一次被覆盖前的内容。回滚本身也会存一份快照，所以回滚后重读详情与版本列表。
func (s *Store) RestorePreviousRevision() bool {
	s.mu.Lock()
	pageID := s.view.SelectedPageID
	var latest string
	if len(s.view.Revisions) > 0 {
		latest = s.view.Revisions[0].ID
	}
	s.mu.Unlock()
	if pageID == "" || latest == "" {
		s.guard.Reject(locales.WikiRevisionRestoreFailed())
		return false
	}
	if !s.attempt(locales.WikiRevisionRestoreFailed(), "wiki.restoreRevision", func() (bool, error) { return s.svc.RestoreRevision(s.ctx(), latest) }) {
		return false
	}
	s.discardDraftFor(pageID)
	s.reloadIfSelected(pageID)
	s.Refresh()
	s.guard.Succeed(locales.WikiRevisionRestored())
	return true
}
