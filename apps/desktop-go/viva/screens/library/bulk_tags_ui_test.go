package library

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测批量打标签浮层与单条标签选择浮层的界面级行为（台账 B-043）。数据与夹具同 bulk_bar_ui_test.go；
// 夹具里的标签是 go、rust、闲置，条目自带的标签是「产品设计」（每三条的第一条）。

// hasInList 报告工作区里（侧栏右边）有没有角色与名字都对得上的节点：侧栏里也有同名的标签行。
func (r *rig) hasInList(role gfx.Role, name string) bool {
	for _, n := range r.session.Access() {
		if n.Role == role && n.Label == name && n.Bounds.X >= 240 {
			return true
		}
	}
	return false
}

// tagField 是标签浮层里的输入框。
func (r *rig) tagField() (value string, focused bool) {
	r.t.Helper()
	n := r.mustNode(gfx.RoleTextField, locales.LibraryTagsNewPlaceholder())
	return n.Value, n.Focused
}

// ---- 批量打标签 ----

// 批量条的「打标签」：浮层一出现输入框就取得焦点；回车、点「添加」、点某个已有标签都是提交——
// 去掉首尾空白，空的不提交；提交就给选中的每一条追加这个标签并关闭浮层。
func TestBulkTagPopoverAddsTheTagToEverySelectedEntry(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		ids := r.ids(2, 4)
		hint := locales.LibraryBulkAddTagHint(3)
		open := func() {
			t.Helper()
			r.selectIDs(ids)
			r.click(gfx.RoleButton, locales.LibraryBulkAddTag())
			if !r.hasText(hint) {
				t.Fatalf("应打开打标签浮层（提示「%s」）；画面文字 %q", hint, r.session.Texts())
			}
		}
		addTags := func() []string {
			t.Helper()
			b := r.lastBatch()
			if b.Command.Kind != services.LibraryBatchUpdate || b.Command.Patch == nil || !slices.Equal(b.IDs, ids) {
				t.Fatalf("应对选中的每一条发批量更新，实际 %+v %v", b.Command, b.IDs)
			}
			return b.Command.Patch.AddTagNames
		}

		open()
		if _, focused := r.tagField(); !focused {
			t.Fatal("输入框应一出现就取得焦点")
		}
		if !r.mustNode(gfx.RoleButton, locales.CommonAdd()).Disabled {
			t.Fatal("草稿为空时「添加」应禁用")
		}
		r.keys("Enter")
		if r.batchCount() != 0 || !r.hasText(hint) {
			t.Fatal("空草稿回车不提交、浮层不关")
		}
		r.typeText("   ")
		r.keys("Enter")
		if r.batchCount() != 0 || !r.hasText(hint) {
			t.Fatal("只有空白也不提交")
		}

		r.typeText("  新标签  ")
		if r.mustNode(gfx.RoleButton, locales.CommonAdd()).Disabled {
			t.Fatal("有草稿时「添加」应可用")
		}
		r.keys("Enter")
		if got := addTags(); !slices.Equal(got, []string{"新标签"}) {
			t.Fatalf("回车应提交去掉首尾空白的标签，实际 %q", got)
		}
		if r.hasText(hint) {
			t.Fatal("提交后浮层应关闭")
		}

		open()
		if value, _ := r.tagField(); value != "" {
			t.Fatalf("每次打开草稿都是空的，实际 %q", value)
		}
		if !r.hasInList(gfx.RoleButton, "新标签") {
			t.Fatal("已有标签里应出现刚建的「新标签」（标签列表随批量更新重读）")
		}
		r.clickInList(gfx.RoleButton, "rust")
		if got := addTags(); !slices.Equal(got, []string{"rust"}) {
			t.Fatalf("点已有标签应提交它，实际 %q", got)
		}

		open()
		r.typeText("补充")
		r.click(gfx.RoleButton, locales.CommonAdd())
		if got := addTags(); !slices.Equal(got, []string{"补充"}) {
			t.Fatalf("点「添加」应提交草稿，实际 %q", got)
		}
		if len(r.svc.Batches()) != 3 {
			t.Fatalf("三次提交应发三个批次，实际 %d", len(r.svc.Batches()))
		}
	})
}

// 浮层里的已有标签是全部标签（不排除任何一个，含没有条目的），按 Esc 收起；打开时的草稿不带到下一次。
func TestBulkTagPopoverListsEveryTagAndEscapeClosesIt(t *testing.T) {
	t.Parallel()
	bothViews(t, func(t *testing.T, r *rig) {
		r.selectIDs(r.ids(2, 3))
		r.click(gfx.RoleButton, locales.LibraryBulkAddTag())
		for _, name := range []string{"go", "rust", "闲置"} {
			if !r.hasInList(gfx.RoleButton, name) {
				t.Fatalf("已有标签里缺「%s」", name)
			}
		}
		r.typeText("没提交的草稿")
		r.keys("Escape")
		if r.hasText(locales.LibraryBulkAddTagHint(2)) {
			t.Fatal("Esc 应收起浮层")
		}
		r.selectIDs(r.ids(2, 3))
		r.click(gfx.RoleButton, locales.LibraryBulkAddTag())
		if value, _ := r.tagField(); value != "" {
			t.Fatalf("没提交的草稿不该带到下一次，实际 %q", value)
		}
	})
}

// ---- 单条的标签选择（表格右键菜单的「编辑标签」）----

// 标签选择浮层：当前标签带移除钮，输入加「添加」与已有标签都立刻写回这一条；重名（不分大小写）与空的忽略；
// 添加后草稿清空；AI 建议在迁入之前给出可理解的说明，不假装成功。
func TestTagPickerEditsOneEntrysTags(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.toTable()
	e := r.view().List.Entries[4] // item-000003：自带「产品设计」
	if len(e.Tags) != 1 || e.Tags[0].Name != "产品设计" {
		t.Fatalf("夹具的第 4 条应带「产品设计」，实际 %+v", e.Tags)
	}
	r.rightClickEntry(e)
	r.click(gfx.RoleMenuItem, locales.LibraryEditTags())
	if !r.has(gfx.RoleButton, locales.LibraryRemoveTag("产品设计")) {
		t.Fatalf("应打开标签选择浮层，当前标签带移除钮；画面文字 %q", r.session.Texts())
	}
	if _, focused := r.tagField(); !focused {
		t.Fatal("输入框应一出现就取得焦点")
	}
	lastTags := func() []string {
		t.Helper()
		calls := r.svc.UpdateCalls()
		if len(calls) == 0 {
			t.Fatal("服务没有收到条目更新")
		}
		last := calls[len(calls)-1]
		if last.ID != e.ID {
			t.Fatalf("更新应落在这一条 %s，实际 %s", e.ID, last.ID)
		}
		return last.Patch.TagNames
	}

	r.typeText("新")
	r.keys("Enter")
	if got := lastTags(); !slices.Equal(got, []string{"产品设计", "新"}) {
		t.Fatalf("回车应追加到当前标签后面，实际 %q", got)
	}
	if value, _ := r.tagField(); value != "" {
		t.Fatalf("添加后草稿应清空，实际 %q", value)
	}
	if !r.has(gfx.RoleButton, locales.LibraryRemoveTag("新")) {
		t.Fatal("新标签应出现在当前标签里")
	}

	calls := len(r.svc.UpdateCalls())
	r.typeText("产品设计")
	r.keys("Enter")
	if len(r.svc.UpdateCalls()) != calls {
		t.Fatal("与当前标签重名不该再写回")
	}
	if value, _ := r.tagField(); value != "" {
		t.Fatalf("重名的草稿也应清空，实际 %q", value)
	}
	r.typeText("   ")
	r.keys("Enter")
	if len(r.svc.UpdateCalls()) != calls {
		t.Fatal("空草稿不该写回")
	}

	r.clickInList(gfx.RoleButton, "go")
	if got := lastTags(); !slices.Equal(got, []string{"产品设计", "新", "go"}) {
		t.Fatalf("点已有标签应追加它，实际 %q", got)
	}
	if r.hasInList(gfx.RoleButton, "go") {
		t.Fatal("已经带上的标签不该再出现在「选择已有标签」里")
	}

	r.click(gfx.RoleButton, locales.LibraryRemoveTag("产品设计"))
	if got := lastTags(); !slices.Equal(got, []string{"go", "新"}) && !slices.Equal(got, []string{"新", "go"}) {
		t.Fatalf("点移除钮应去掉这个标签，实际 %q", got)
	}
	if r.has(gfx.RoleButton, locales.LibraryRemoveTag("产品设计")) {
		t.Fatal("移除后当前标签里不该再有它")
	}
	if !r.hasInList(gfx.RoleButton, "产品设计") {
		t.Fatal("移除后它应回到「选择已有标签」里")
	}

	r.click(gfx.RoleButton, locales.LibraryTagsGenerate())
	if !r.hasText(locales.CommonOperationFailed(locales.LibraryAiTagsSuggest())) {
		t.Fatalf("AI 建议迁入之前应给出失败提示；画面文字 %q", r.session.Texts())
	}
	if !slices.ContainsFunc(r.log.logs, func(entry string) bool { return strings.Contains(entry, locales.LibraryAiTagsSuggest()) }) {
		t.Fatalf("失败应留痕，日志 %q", r.log.logs)
	}

	r.keys("Escape")
	if r.has(gfx.RoleButton, locales.LibraryRemoveTag("新")) {
		t.Fatal("Esc 应收起浮层")
	}
}

// 标签选择浮层在没有可选标签时写一句提示；条目不在本页了（读完新一页）浮层不画。
func TestTagPickerSaysWhenThereIsNothingToChooseAndDisappearsWithItsEntry(t *testing.T) {
	t.Parallel()
	r := wsRig(t, func(r *rig) {
		counts, collections, _ := libraryFixture()
		r.svc.Seed(counts, collections, nil) // 没有任何标签
	})
	r.toTable()
	e := r.view().List.Entries[4]
	r.rightClickEntry(e)
	r.click(gfx.RoleMenuItem, locales.LibraryEditTags())
	if !r.hasText(locales.LibraryTagsExistingEmpty()) {
		t.Fatalf("没有可选标签时应写提示；画面文字 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.LibraryNextPage())
	if r.has(gfx.RoleButton, locales.LibraryRemoveTag("产品设计")) || r.hasText(locales.LibraryTagsExistingEmpty()) {
		t.Fatal("条目不在本页了，浮层不该再画")
	}
}
