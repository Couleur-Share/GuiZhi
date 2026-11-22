package library

import (
	"context"
	"errors"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// CreateItem 新建一条空白笔记并选中它（渲染层 knowledge.store 的 createItem）：快速采集的「空白笔记」。
//
// 顺序：先把当前草稿落盘（落不了盘就不新建，免得丢编辑），新建后重新读取列表与计数，再落盘一次（读取期间可能又有输入）并选中新条目、
// 把光标放进标题（TitleFocusID）。新建之后又选了别的条目、或改了筛选，新条目仍然建成、列表照常刷新，但不再抢走选中。
// collectionID 为空表示不归入合集。阻塞到全部完成，不能在界面线程调用；失败返回原因（调用方交给 kit.Guard）。
func (s *Store) CreateItem(ctx context.Context, collectionID string) error {
	s.mu.Lock()
	s.detail.seq++
	seq := s.detail.seq
	s.mu.Unlock()

	if !s.FlushPendingSave(ctx) {
		return errors.New(locales.ThemedReadingSaveEditFirst())
	}
	item, err := s.svc.CreateItem(ctx, services.LibraryCreateInput{CollectionID: collectionID})
	if err != nil {
		return err
	}
	s.RefreshAll(ctx)
	if !s.FlushPendingSave(ctx) {
		return nil // 条目已建成；读取期间的新编辑没能落盘，失败原因在详情头部，不再改选中
	}

	s.mu.Lock()
	if seq != s.detail.seq {
		s.mu.Unlock()
		return nil
	}
	s.commitSelectLocked(item.ID)
	d := &s.view.Detail
	d.Item, d.Loading, d.LoadError, d.TitleFocusID = &item, false, "", item.ID
	s.mu.Unlock()
	s.wake()
	return nil
}
