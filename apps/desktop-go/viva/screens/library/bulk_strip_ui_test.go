package library

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测 Delete 键与批量结果条的界面级行为（台账 B-040、B-043、B-045）。数据与夹具同 bulk_bar_ui_test.go。

// ---- Delete 键 ----

// Delete：有多选删多选，没有多选删当前条目，其余范围移到回收站并给撤销；按键被接走。
// 没有可删的条目、按住 Ctrl 时放行。
func TestDeleteKeyTrashesTheSelectionOrTheCurrentEntry(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		del := gfx.KeyEvent{Key: gfx.KeyDelete}
		if res := r.screen.ListKey(del, &r.canvas); res.Handled || r.batchCount() != 0 {
			t.Fatalf("没有选中任何条目时 Delete 应放行，Handled=%v 批次 %d", res.Handled, r.batchCount())
		}

		current := r.view().List.Entries[3]
		r.store.SelectItem(current.ID)
		r.frames(2)
		ctrl := gfx.KeyEvent{Key: gfx.KeyDelete, Modifiers: gfx.ModifierControl}
		if res := r.screen.ListKey(ctrl, &r.canvas); res.Handled || r.batchCount() != 0 {
			t.Fatalf("按住 Ctrl 时 Delete 不动，Handled=%v 批次 %d", res.Handled, r.batchCount())
		}
		r.keys("Delete")
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchTrash || !slices.Equal(b.IDs, []string{current.ID}) {
			t.Fatalf("没有多选时应删当前条目，实际 %+v %v", b.Command, b.IDs)
		}
		if !r.hasText(locales.LibraryMovedToTrash(1)) {
			t.Fatalf("应弹「已移到回收站」；画面文字 %q", r.session.Texts())
		}

		ids := r.ids(2, 4)
		r.selectIDs(ids)
		r.keys("Delete")
		if b := r.lastBatch(); b.Command.Kind != services.LibraryBatchTrash || !slices.Equal(b.IDs, ids) {
			t.Fatalf("有多选时应删多选（不是当前条目），实际 %+v %v", b.Command, b.IDs)
		}
		if !r.hasText(locales.LibraryMovedToTrash(3)) {
			t.Fatalf("应弹「已移到回收站（3 项）」；画面文字 %q", r.session.Texts())
		}
	})
}

// 回收站里 Delete 是彻底删除：先开确认框，不直接请求后端；Esc 关框。
func TestDeleteKeyInTheTrashAsksBeforeDeletingForever(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.goTrash()
		ids := r.ids(0, 1)
		r.selectIDs(ids)
		r.keys("Delete")
		if !r.confirmOpen(locales.LibraryDeleteForever()) || !r.hasText(locales.LibraryDeleteForeverConfirmMany(2)) {
			t.Fatalf("回收站里 Delete 应开确认框（2 条）；画面文字 %q", r.session.Texts())
		}
		if r.batchCount() != 0 {
			t.Fatal("确认之前不该请求后端")
		}
		r.keys("Escape")
		if r.confirmOpen(locales.LibraryDeleteForever()) || r.batchCount() != 0 {
			t.Fatal("Esc 应只关框")
		}
	})
}

// 焦点在输入框里时 Delete 是删字，不是删条目（渲染层：焦点在可编辑区时整个键盘钩子不动）。
func TestDeleteKeyInsideATextFieldDoesNotDeleteEntries(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	ids := r.ids(2, 3)
	r.selectIDs(ids)
	r.click(gfx.RoleButton, locales.LibraryBulkAddTag()) // 标签浮层里的输入框一出现就取得焦点
	if !r.focusedIs(gfx.RoleTextField, locales.LibraryTagsNewPlaceholder()) {
		t.Fatal("标签输入框应已取得焦点")
	}
	r.keys("Delete")
	if r.batchCount() != 0 {
		t.Fatalf("焦点在输入框里，Delete 不该删条目，实际 %d 个批次", r.batchCount())
	}
}

// ---- 批量结果条 ----

// 有失败项的批次：结果条写汇总、给「仅重试失败项」与「收起」；重试复用同一批次、只带失败项；
// 展开「逐条结果」列出每一条的结果；收起之后条消失。
func TestBatchResultsStripShowsTheOutcomeAndRetriesOnlyTheFailures(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		ids := r.ids(2, 4)
		r.svc.FailBatchItem(ids[1], "磁盘已满")
		r.selectIDs(ids)
		r.click(gfx.RoleButton, locales.LibraryArchive())

		notice := r.view().Batch.Notice
		if notice == "" || !r.hasText(notice) {
			t.Fatalf("结果条应写汇总 %q；画面文字 %q", notice, r.session.Texts())
		}
		for _, label := range []string{locales.LibraryWorkflowRetryFailed(), locales.LibraryWorkflowDismiss(), locales.LibraryWorkflowResults(3)} {
			if !r.has(gfx.RoleButton, label) {
				t.Fatalf("结果条缺「%s」", label)
			}
		}
		okLine, failLine := ids[0]+"："+locales.CommonSuccess(), ids[1]+"：磁盘已满"
		if r.hasText(okLine) || r.hasText(failLine) {
			t.Fatal("「逐条结果」应是折叠的")
		}
		r.click(gfx.RoleButton, locales.LibraryWorkflowResults(3))
		if !r.hasText(okLine) || !r.hasText(failLine) || !r.hasText(ids[2]+"："+locales.CommonSuccess()) {
			t.Fatalf("展开后应逐条列出结果；画面文字 %q", r.session.Texts())
		}
		if !r.mustNode(gfx.RoleButton, locales.LibraryWorkflowResults(3)).Expanded {
			t.Fatal("展开后可访问状态应是已展开")
		}
		r.click(gfx.RoleButton, locales.LibraryWorkflowResults(3))
		if r.hasText(okLine) {
			t.Fatal("再点一次应折叠")
		}

		r.svc.FailBatchItem(ids[1], "")
		r.click(gfx.RoleButton, locales.LibraryWorkflowRetryFailed())
		calls := r.svc.Batches()
		if len(calls) != 2 || calls[1].RunID != calls[0].RunID || !slices.Equal(calls[1].IDs, []string{ids[1]}) || calls[1].Command.Status != "archived" {
			t.Fatalf("重试应复用同一批次、只带失败的那一条，实际 %+v", calls)
		}
		if r.has(gfx.RoleButton, locales.LibraryWorkflowRetryFailed()) {
			t.Fatal("全部成功后不该再有「仅重试失败项」")
		}

		// 「逐条结果」的展开状态属于这一轮结果：展开着把结果条收起，再来一轮批量，新的结果条仍是折叠的。
		r.click(gfx.RoleButton, locales.LibraryWorkflowResults(3))
		r.click(gfx.RoleButton, locales.LibraryWorkflowDismiss())
		if r.view().Batch.Visible() || r.has(gfx.RoleButton, locales.LibraryWorkflowDismiss()) {
			t.Fatal("点「收起」应收起结果条")
		}
		r.selectIDs(r.ids(6, 7))
		r.click(gfx.RoleButton, locales.LibraryArchive())
		if r.mustNode(gfx.RoleButton, locales.LibraryWorkflowResults(2)).Expanded {
			t.Fatal("新一轮结果的「逐条结果」应是折叠的，不该沿用上一轮的展开状态")
		}
	})
}

// 重试执行期间结果条写「处理中，已核对 N 项」，「收起」与「仅重试失败项」都禁用；放行后写汇总、解除禁用。
// （第一批刚发出、还没有任何结果时结果条不画——渲染层 `!notice && !results.length` 同样返回空；所以只能在重试时看到它。）
func TestBatchResultsStripIsBusyWhileTheRetryRuns(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	ids := r.ids(2, 3)
	r.svc.FailBatchItem(ids[0], "磁盘已满")
	r.selectIDs(ids)
	r.click(gfx.RoleButton, locales.LibraryArchive())
	if len(r.view().Batch.Failed()) != 1 {
		t.Fatalf("第一批应有一条失败，实际 %+v", r.view().Batch)
	}
	r.svc.FailBatchItem(ids[0], "")
	r.goAsync()
	release := r.holdBatch()
	r.click(gfx.RoleButton, locales.LibraryWorkflowRetryFailed())
	r.waitUntil("重试批次到达服务", func() bool { return r.batchCount() == 2 })
	r.waitUntil("结果条写处理中", func() bool {
		return r.view().Batch.Busy && r.hasText(locales.LibraryWorkflowProcessing(2))
	})
	if !r.mustNode(gfx.RoleButton, locales.LibraryWorkflowDismiss()).Disabled || !r.mustNode(gfx.RoleButton, locales.LibraryWorkflowRetryFailed()).Disabled {
		t.Fatal("处理中「收起」与「仅重试失败项」应禁用")
	}
	release()
	r.waitUntil("批次结束", func() bool { return !r.view().Batch.Busy })
	r.frames(3)
	if r.has(gfx.RoleButton, locales.LibraryWorkflowRetryFailed()) {
		t.Fatal("重试全部成功后不该再有「仅重试失败项」")
	}
	if r.mustNode(gfx.RoleButton, locales.LibraryWorkflowDismiss()).Disabled {
		t.Fatal("结束后「收起」应可用")
	}
}

// 单条与第一批刚发出时都没有结果：结果条不画（不能画一条空的）。
func TestBatchResultsStripIsHiddenWithoutNoticeOrResults(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	if r.has(gfx.RoleButton, locales.LibraryWorkflowDismiss()) {
		t.Fatal("没有批量操作时不该有结果条")
	}
	r.goAsync()
	release := r.holdBatch()
	r.selectIDs(r.ids(2, 3))
	r.click(gfx.RoleButton, locales.LibraryArchive())
	r.waitUntil("批次到达服务", func() bool { return r.batchCount() == 1 })
	r.frames(3)
	if r.has(gfx.RoleButton, locales.LibraryWorkflowDismiss()) {
		t.Fatal("第一批还没有任何结果时结果条不画")
	}
	release()
	r.waitUntil("结果条出现", func() bool { return !r.view().Batch.Busy && r.view().Batch.Visible() })
}

// 结果条是礼貌的直播区域（role=status）：读屏软件会念出汇总。
func TestBatchResultsStripIsAPoliteStatusRegion(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	ids := r.ids(2, 3)
	r.svc.FailBatchItem(ids[0], "磁盘已满")
	r.selectIDs(ids)
	r.click(gfx.RoleButton, locales.LibraryArchive())
	found := false
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleStatus && n.Live == gfx.LivePolite {
			found = true
		}
	}
	if !found {
		t.Fatal("结果条应声明为礼貌的 status 区域")
	}
}
