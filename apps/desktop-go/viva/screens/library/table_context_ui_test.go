package library

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/locales"
)

// 本文件测表格行首复选框格上的右键（台账 B-057）。
//
// 渲染层的 onContextMenu 挂在整个 <tr> 上（ItemTableRow.tsx），复选框与它所在的格子都没有拦截 contextmenu 事件，
// 所以在这一格上点右键同样弹出这一行的条目菜单。2026-11-21 在渲染层参照里实测（probe-table-checkbox-context.mjs）：
// 方框正中、格子里方框之外的空白处，两处都出同一份菜单，菜单开在光标处。

// rightClickAt 在 (x, y) 点右键。
func (r *rig) rightClickAt(x, y float32) {
	r.t.Helper()
	if err := r.session.RightClick(x, y); err != nil {
		r.t.Fatalf("右键 (%.0f, %.0f) 失败：%v", x, y, err)
	}
	r.frames(3)
}

// 复选框方框正中与格子空白处点右键，都在光标处出这一行的菜单；右键不改选择，也不翻转复选框。
func TestRightClickOnTheRowCheckboxCellOpensTheEntryMenu(t *testing.T) {
	t.Parallel()
	for _, spot := range []string{"方框正中", "格子空白处"} {
		t.Run(spot, func(t *testing.T) {
			t.Parallel()
			r := wsRig(t)
			r.toTable()
			e := r.view().List.Entries[3] // 排在前两条置顶之后：既没置顶也没收藏
			box := r.mustNode(gfx.RoleCheckbox, locales.LibrarySelectRow(e.Title))
			x, y := centerOf(box)
			if spot == "格子空白处" {
				x = box.Bounds.X - 14 // 复选框格 px-4：格子左缘在方框左边 16 像素处，渲染层的实测点取左缘内 2 像素
			}
			r.rightClickAt(x, y)
			for _, label := range []string{
				locales.LibraryFavorite(), locales.LibraryPin(), locales.LibraryAiHandoff(), locales.LibraryArchive(),
				locales.LibraryMoveToTrash(), locales.LibraryEditTags(),
			} {
				if !r.has(gfx.RoleMenuItem, label) {
					t.Fatalf("在复选框格上右键应出条目菜单，缺「%s」", label)
				}
			}
			first := r.mustNode(gfx.RoleMenuItem, locales.LibraryFavorite())
			if first.Bounds.X < x || first.Bounds.X > x+12 || first.Bounds.Y < y || first.Bounds.Y > y+12 {
				t.Fatalf("菜单应开在光标 (%.0f, %.0f) 处，第一项在 %+v", x, y, first.Bounds)
			}
			if got := r.view().List.Selection; len(got) != 0 {
				t.Fatalf("右键不改选择，实际多选 %v", got)
			}
			if r.mustNode(gfx.RoleCheckbox, locales.LibrarySelectRow(e.Title)).Checked {
				t.Fatal("右键不该翻转复选框")
			}
		})
	}
}

// 已勾选的行上，复选框格上的右键与其它位置一样作用在整组所选（渲染层：多选时菜单是整组的）。
func TestRightClickOnACheckedRowCheckboxOpensTheGroupMenu(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.toTable()
	entries := r.view().List.Entries
	r.selectIDs([]string{entries[3].ID, entries[4].ID})
	box := r.mustNode(gfx.RoleCheckbox, locales.LibrarySelectRow(entries[3].Title))
	x, y := centerOf(box)
	r.rightClickAt(x, y)
	if !r.has(gfx.RoleMenuItem, locales.LibraryBulkMoveToTrash(2)) {
		t.Fatal("在已勾选行的复选框上右键应出整组的菜单（写着 2 条）")
	}
	r.click(gfx.RoleMenuItem, locales.LibraryBulkArchive(2))
	if b := r.lastBatch(); len(b.IDs) != 2 {
		t.Fatalf("多选时菜单应作用在整组 2 条，实际 %d 条", len(b.IDs))
	}
}
