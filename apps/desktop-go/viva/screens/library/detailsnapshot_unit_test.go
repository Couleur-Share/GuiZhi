package library

import (
	"slices"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件是 detailsnapshot.go 的纯逻辑单测：snapshotUI.edited、Screen.syncSnapshot 的「可信读取结果」判定、snapshotMenuItems 的补采项。
// 不经过渲染（不起 rig、不画帧），直接喂状态、读状态，确定且很快；界面一侧的整体行为在 detail_snapshot_ui_test.go。

// syncRig 是只带 Store 的最小 Screen：选中条目 a（快照夹具见 snapshotHarness），面板切到「网页快照」页。
func syncRig(t *testing.T) (*Screen, *harness, services.LibraryItem) {
	t.Helper()
	h := snapshotHarness(t)
	s := &Screen{Store: h.store, Ctx: h.ctx}
	s.detail.panel.snapshotView = true
	return s, h, services.LibraryItem{ID: "a", Content: "甲的正文"}
}

var wechatCaps = ItemCaps{IsWechat: true}

// 「已编辑」只在看当前版本时成立：看旧版本（version 非空）即使快照标了 Edited 也不算。
func TestSnapshotUIEditedOnlyForTheCurrentVersion(t *testing.T) {
	t.Parallel()
	edited := &services.LibraryWebSnapshotView{Edited: true}
	cases := []struct {
		name string
		ui   snapshotUI
		want bool
	}{
		{"当前版本已编辑", snapshotUI{view: edited}, true},
		{"旧版本已编辑也不算", snapshotUI{view: edited, version: "v1"}, false},
		{"当前版本未编辑", snapshotUI{view: &services.LibraryWebSnapshotView{}}, false},
		{"没有读到快照", snapshotUI{}, false},
	}
	for _, c := range cases {
		if got := c.ui.edited(); got != c.want {
			t.Errorf("%s：edited() = %v，应为 %v", c.name, got, c.want)
		}
	}
}

// 读取结果的版本与面板在看的版本不一致时不可信（版本切换的空档里 Store 手里还是上一个版本的结果）：面板仍是读取中，不采用它。
func TestSyncSnapshotIgnoresAResultForAnotherVersionOrItem(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name            string
		itemID, version string
	}{
		{"同条目、别的版本", "a", "v1"},
		{"别的条目、同版本", "b", ""},
	} {
		s, h, item := syncRig(t)
		s.syncSnapshot(h.store.View(), item, wechatCaps) // 第一帧发起读取，之后同键不再重读
		foreign := wsView("other")
		frame := h.store.View()
		frame.Detail.Snapshot = SnapshotView{ItemID: c.itemID, VersionID: c.version, View: &foreign}
		s.syncSnapshot(frame, item, wechatCaps)
		sn := &s.detail.panel.snap
		if sn.view != nil || !sn.loading || sn.err != "" {
			t.Errorf("%s：不该采用别的读取结果：view=%v loading=%v err=%q", c.name, sn.view, sn.loading, sn.err)
		}
	}
}

// 发起读取那一刻界面手里的那一帧还带着上一次的快照：它不是新读取的结果，要等下一帧 Store 里新的结果才可信。
func TestSyncSnapshotDoesNotTrustTheSnapshotLeftBeforeTheRead(t *testing.T) {
	t.Parallel()
	s, h, item := syncRig(t)
	h.store.LoadWebSnapshot(h.ctx, "a", "") // 先留一份旧快照在 Store 里
	left := h.store.View().Detail.Snapshot.View
	if left == nil {
		t.Fatal("夹具应已读到当前版本的快照")
	}
	s.syncSnapshot(h.store.View(), item, wechatCaps) // 这一帧 View 是发起读取之前的快照
	sn := &s.detail.panel.snap
	if sn.view != nil || !sn.loading {
		t.Fatalf("发起读取的那一帧不能把遗留的旧快照当结果：view=%v loading=%v", sn.view, sn.loading)
	}
	s.syncSnapshot(h.store.View(), item, wechatCaps) // 下一帧是新读取的结果
	if sn.view == nil || sn.view == left || sn.loading {
		t.Fatalf("下一帧应采用新读取的结果：view=%p（旧 %p）loading=%v", sn.view, left, sn.loading)
	}
}

// 回收站里的条目不能补采：「更多」菜单没有补采项；活着的条目有。
func TestSnapshotMenuHasNoSupplementForATrashedItem(t *testing.T) {
	t.Parallel()
	s := &Screen{}
	labels := func(item services.LibraryItem) []string {
		var out []string
		for _, it := range s.snapshotMenuItems(item) {
			out = append(out, it.Label)
		}
		return out
	}
	if got, want := labels(services.LibraryItem{ID: "a"}), []string{locales.LibrarySnapshotSupplementUpdate()}; !slices.Equal(got, want) {
		t.Errorf("活着的条目菜单 = %v，应为 %v", got, want)
	}
	if got := labels(services.LibraryItem{ID: "a", DeletedAt: 1700000000000}); len(got) != 0 {
		t.Errorf("回收站条目不该有补采项：%v", got)
	}
	// 回收站条目有快照时仍能导出，只是没有补采。
	v := wsView("v2")
	s.detail.panel.snap = snapshotUI{view: &v, simple: true} // 标准排版，菜单里没有「切换排版」项
	if got, want := labels(services.LibraryItem{ID: "a", DeletedAt: 1}), []string{locales.LibrarySnapshotExportHtml()}; !slices.Equal(got, want) {
		t.Errorf("回收站条目有快照时菜单 = %v，应为 %v", got, want)
	}
}
