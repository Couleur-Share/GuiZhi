package app

import (
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/screens/shell"
)

// 变异检查（m31）补充：监听的三个计数（未归合集、全部、回收站）里，只有回收站变了也要合并刷新处理中心。
// TestRootInboxRefreshesWhenLibraryCountsChange 里「全部」与「未归合集」同时变，挡不住回收站被漏掉。
func TestRootInboxRefreshesWhenOnlyTheTrashCountChanges(t *testing.T) {
	old := inboxCountsDelay
	inboxCountsDelay = 20 * time.Millisecond
	t.Cleanup(func() { inboxCountsDelay = old })

	f, svc, lib := openInboxRoot(t, shell.ModuleLibrary)
	store := f.root.library.Store
	f.waitFrames(t, "知识库计数与处理中心都读到", func() bool { return store.View().Counts != nil && svc.CallCount("List") > 0 })
	f.frames(t, 3) // 让监听记下首次计数
	before := svc.CallCount("List")

	// 「全部」与「未归合集」都不变，只有回收站从 0 变成 1。
	lib.SetCounts(services.LibraryCounts{All: 2, Uncategorized: 2, Trash: 1})
	store.RefreshCounts(f.root.ctx)
	f.waitFrames(t, "回收站计数变化后处理中心再读一次", func() bool { return svc.CallCount("List") > before })
}
