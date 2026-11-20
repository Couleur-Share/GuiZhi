package library

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测「彻底删除 / 清空回收站」确认框、Delete 键与批量结果条的界面级行为（台账 B-040、B-043、B-045）。
// 数据与夹具同 bulk_bar_ui_test.go。

// goAsync 让之后的后台动作真的放到别的协程（夹具里的 Spawn 默认同步执行）：用来观察「进行中」。
// 必须在首屏读完之后调用——首屏的读取要在调用之前同步完成。
func (r *rig) goAsync() {
	r.store.Spawn = func(fn func()) { go fn() }
}

// waitUntil 出帧直到 cond 成立（后台协程的结果经快照回到界面），最多等 3 秒。
func (r *rig) waitUntil(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			r.t.Fatalf("等不到：%s", what)
		}
		r.frames(1)
		time.Sleep(5 * time.Millisecond)
	}
}

// holdBatch 让下一次批量执行卡住，返回的放行函数可以重复调用，测试结束时自动放行（免得协程悬着）。
func (r *rig) holdBatch() (release func()) {
	var once sync.Once
	gate := r.svc.HoldBatch()
	release = func() { once.Do(gate) }
	r.t.Cleanup(release)
	return release
}

// hasTextWithPrefix 报告画面上是否有以 prefix 开头的一行文字（长段落会折成几行，整句对不上）。
func (r *rig) hasTextWithPrefix(prefix string) bool {
	for _, got := range r.session.Texts() {
		if strings.HasPrefix(got, prefix) {
			return true
		}
	}
	return false
}

// confirmOpen 报告标题为 title 的确认框是不是开着。
func (r *rig) confirmOpen(title string) bool { return r.has(gfx.RoleHeading, title) }

// batchCount 是服务收到的批量执行次数。
func (r *rig) batchCount() int { return len(r.svc.Batches()) }

// ---- 彻底删除 ----

// 彻底删除先问：点批量条的「彻底删除」只开确认框，不请求后端；取消与 Esc 都只关框；
// 勾选「同时清除历史问答中的相关证据」每次打开都复位；确认才发批次（带不带清证据看复选框），成功后关框。
func TestDeleteForeverConfirmFlow(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.goTrash()
		ids := r.ids(0, 1)
		open := func() {
			t.Helper()
			r.selectIDs(ids)
			r.click(gfx.RoleButton, locales.LibraryDeleteForever())
			if !r.confirmOpen(locales.LibraryDeleteForever()) {
				t.Fatalf("应打开「彻底删除」确认框；画面文字 %q", r.session.Texts())
			}
		}
		evidence := locales.LibraryDeleteEvidenceClear()

		open()
		if !r.hasText(locales.LibraryDeleteForeverConfirmMany(2)) || !r.hasTextWithPrefix(string([]rune(locales.LibraryDeleteEvidenceHint())[:8])) {
			t.Fatalf("确认框应写明条数与证据说明；画面文字 %q", r.session.Texts())
		}
		if r.batchCount() != 0 {
			t.Fatal("确认之前不该请求后端")
		}
		r.click(gfx.RoleButton, locales.CommonCancel())
		if r.confirmOpen(locales.LibraryDeleteForever()) || r.batchCount() != 0 {
			t.Fatal("取消只关框，不请求后端")
		}

		open()
		r.keys("Escape")
		if r.confirmOpen(locales.LibraryDeleteForever()) || r.batchCount() != 0 {
			t.Fatal("Esc 只关框，不请求后端")
		}

		open()
		r.click(gfx.RoleCheckbox, evidence)
		if !r.mustNode(gfx.RoleCheckbox, evidence).Checked {
			t.Fatal("点复选框应勾上")
		}
		r.click(gfx.RoleButton, locales.CommonCancel())
		open()
		if r.mustNode(gfx.RoleCheckbox, evidence).Checked {
			t.Fatal("再次打开时「同时清除证据」应复位为未勾选")
		}

		r.click(gfx.RoleCheckbox, evidence)
		r.click(gfx.RoleButton, locales.CommonConfirm())
		b := r.lastBatch()
		if b.Command.Kind != services.LibraryBatchDelete || !b.Command.ClearEvidence || !slices.Equal(b.IDs, ids) {
			t.Fatalf("确认应发带清证据的彻底删除批次，实际 %+v %v", b.Command, b.IDs)
		}
		if r.confirmOpen(locales.LibraryDeleteForever()) {
			t.Fatal("全部成功后应关框")
		}
		if cleared := r.svc.ClearedEvidence(); !slices.Equal(cleared, ids) {
			t.Fatalf("服务应收到清证据的条目 %v，实际 %v", ids, cleared)
		}
	})
}

// 回车确认；不勾证据就是不清证据；单条的说明用单数。
func TestDeleteForeverEnterConfirmsWithoutClearingEvidence(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.goTrash()
		id := r.ids(0, 0)
		r.selectIDs(id)
		r.click(gfx.RoleButton, locales.LibraryDeleteForever())
		if !r.hasText(locales.LibraryDeleteForeverConfirm()) {
			t.Fatalf("单条的说明应是单数；画面文字 %q", r.session.Texts())
		}
		r.keys("Enter")
		b := r.lastBatch()
		if b.Command.Kind != services.LibraryBatchDelete || b.Command.ClearEvidence || !slices.Equal(b.IDs, id) {
			t.Fatalf("回车应确认，且不勾证据时不清证据，实际 %+v %v", b.Command, b.IDs)
		}
		if len(r.svc.ClearedEvidence()) != 0 {
			t.Fatalf("没勾证据，服务不该清证据，实际 %v", r.svc.ClearedEvidence())
		}
	})
}

// 有一条删失败：框留着、解除忙碌，可以再试；失败原因进结果条；重试失败项会重新确认（彻底删除不可逆），不直接重放。
func TestDeleteForeverStaysOpenWhenItFailsAndRetryAsksAgain(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.goTrash()
		ids := r.ids(0, 1)
		r.svc.FailBatchItem(ids[0], "磁盘已满")
		r.selectIDs(ids)
		r.click(gfx.RoleButton, locales.LibraryDeleteForever())
		r.click(gfx.RoleButton, locales.CommonConfirm())
		if !r.confirmOpen(locales.LibraryDeleteForever()) {
			t.Fatal("有失败时框应留着")
		}
		if r.mustNode(gfx.RoleButton, locales.CommonConfirm()).Disabled || r.mustNode(gfx.RoleCheckbox, locales.LibraryDeleteEvidenceClear()).Disabled {
			t.Fatal("失败后应解除忙碌，可以再试")
		}
		b := r.view().Batch
		if len(b.Failed()) != 1 || b.Failed()[0] != ids[0] || b.Notice == "" {
			t.Fatalf("结果条应记下失败的一条，实际 %+v", b)
		}

		r.click(gfx.RoleButton, locales.CommonCancel())
		before := r.batchCount()
		r.click(gfx.RoleButton, locales.LibraryWorkflowRetryFailed())
		if !r.confirmOpen(locales.LibraryDeleteForever()) || !r.hasText(locales.LibraryDeleteForeverConfirm()) {
			t.Fatalf("重试删除的失败项要重新确认（单条）；画面文字 %q", r.session.Texts())
		}
		if r.batchCount() != before {
			t.Fatal("确认之前不该重放")
		}
		r.svc.FailBatchItem(ids[0], "")
		r.click(gfx.RoleButton, locales.CommonConfirm())
		if r.confirmOpen(locales.LibraryDeleteForever()) {
			t.Fatal("这次成功了，框应关闭")
		}
		if last := r.lastBatch(); !slices.Equal(last.IDs, []string{ids[0]}) || last.Command.Kind != services.LibraryBatchDelete {
			t.Fatalf("重试应只带失败的那一条，实际 %+v %v", last.Command, last.IDs)
		}
	})
}

// 执行期间确认钮转圈、复选框禁用，Esc 与回车不再起作用，也不会重复提交；放行之后成功才关框。
func TestDeleteForeverIsBusyWhileTheBatchRuns(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.goTrash()
	id := r.ids(0, 0)
	r.selectIDs(id)
	r.click(gfx.RoleButton, locales.LibraryDeleteForever())
	evidence := locales.LibraryDeleteEvidenceClear()
	r.goAsync()
	release := r.holdBatch()
	r.click(gfx.RoleButton, locales.CommonConfirm())
	r.waitUntil("批次到达服务", func() bool { return r.batchCount() == 1 })
	r.frames(3)

	if !r.mustNode(gfx.RoleCheckbox, evidence).Disabled {
		t.Fatal("执行期间「同时清除证据」应禁用")
	}
	r.keys("Enter")
	r.keys("Escape")
	if !r.confirmOpen(locales.LibraryDeleteForever()) {
		t.Fatal("执行期间 Esc 不该关框")
	}
	if r.batchCount() != 1 {
		t.Fatalf("执行期间回车不该重复提交，实际 %d 个批次", r.batchCount())
	}

	release()
	r.waitUntil("成功后关框", func() bool { return !r.confirmOpen(locales.LibraryDeleteForever()) })
	if r.batchCount() != 1 {
		t.Fatalf("只该有一个批次，实际 %d", r.batchCount())
	}
}

// 确认框的动作还在后台跑时工作区被卸载：上一轮的结果作废，之后打开的新确认框不能被它关掉。
func TestStaleConfirmOutcomeDoesNotCloseALaterDialog(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.goTrash()
	r.selectIDs(r.ids(0, 1))
	r.click(gfx.RoleButton, locales.LibraryDeleteForever())
	r.goAsync()
	release := r.holdBatch()
	r.click(gfx.RoleButton, locales.CommonConfirm())
	r.waitUntil("批次到达服务", func() bool { return r.batchCount() == 1 })

	r.wsHidden = true
	r.frames(3)
	r.wsHidden = false
	r.frames(3)
	if r.confirmOpen(locales.LibraryDeleteForever()) {
		t.Fatal("工作区卸载再挂载后，确认框应已丢掉")
	}
	r.click(gfx.RoleButton, locales.LibraryDeleteForever()) // 选择还在 Store 里：再开一个新的确认框
	if !r.confirmOpen(locales.LibraryDeleteForever()) {
		t.Fatal("应能再开一个新的确认框")
	}
	release()
	r.waitUntil("上一批结束", func() bool { return !r.view().Batch.Busy })
	r.frames(5)
	if !r.confirmOpen(locales.LibraryDeleteForever()) {
		t.Fatal("上一轮的结果不能关掉新开的确认框")
	}
}

// 清空回收站：先确认；确认后先冻结回收站现有的全部条目，再逐条彻底删除。
func TestEmptyTrashConfirmsThenFreezesAndDeletesEverything(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.goTrash()
		r.click(gfx.RoleButton, locales.LibraryEmptyTrash())
		if !r.confirmOpen(locales.LibraryEmptyTrash()) || !r.hasText(locales.LibraryEmptyTrashConfirm()) {
			t.Fatalf("应打开「清空回收站」确认框；画面文字 %q", r.session.Texts())
		}
		if r.batchCount() != 0 || len(r.svc.Freezes()) != 0 {
			t.Fatal("确认之前不该请求后端")
		}
		r.click(gfx.RoleButton, locales.CommonConfirm())
		freezes := r.svc.Freezes()
		if len(freezes) != 1 || freezes[0].Scope != services.LibraryScopeTrash {
			t.Fatalf("应冻结回收站范围，实际 %+v", freezes)
		}
		b := r.lastBatch()
		if b.Command.Kind != services.LibraryBatchDelete || len(b.IDs) != 5 {
			t.Fatalf("应对回收站里的 5 条发彻底删除，实际 %+v %v", b.Command, b.IDs)
		}
		if r.confirmOpen(locales.LibraryEmptyTrash()) {
			t.Fatal("成功后应关框")
		}
	})
}
