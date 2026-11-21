package library

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// 本文件测公众号网页快照在详情里的界面一侧（detailsnapshot.go 与阅读区、版本面板的接线）：工具条、通知条、排版模式、宽度档、目录钮、
// 「更多」菜单与版本面板里的入口。读取与补采本身在 store_snapshot_test.go；岛里页面（srcDoc、CSP、本地图片）由 readersmoke 的 snapshot 场景在真实 WebView2 里验证，
// 这里页面由测试扮演（同 detail_reader_test.go），不覆盖真实渲染（台账 R-15）。
//
// 夹具：条目 r1（detailReviewID）是公众号文章；各测试给它预设快照后，点「查看网页快照」进入快照页。

// snapshotRig 给 r1 预设当前版本的快照 view，打开它的卡片。
func snapshotRig(t *testing.T, view services.LibraryWebSnapshotView, mutate ...func(*rig)) *readerRig {
	t.Helper()
	rr := newReaderRig(t, append([]func(*rig){func(r *rig) { r.svc.SeedSnapshot(detailReviewID, "", view) }}, mutate...)...)
	rr.openCard(detailReviewID)
	return rr
}

// enterSnapshot 点「查看网页快照」进入快照页，并让岛里的页面完成握手；岛里没有内容（标准排版也是岛）时仍返回页面。
func (rr *readerRig) enterSnapshot() *fakeReaderPage {
	rr.t.Helper()
	rr.click(gfx.RoleButton, locales.ArticleAskViewSnapshot())
	rr.frames(2)
	return rr.ready()
}

// viewOf 是页面最近收到的 view 消息里的视图。
func (rr *readerRig) viewOf(p *fakeReaderPage) map[string]any {
	rr.t.Helper()
	view, _ := p.last(rr.t, "view")["view"].(map[string]any)
	if view == nil {
		rr.t.Fatalf("view 消息里没有视图：%v", p.last(rr.t, "view"))
	}
	return view
}

func snapView(version string, edit ...func(*services.LibraryWebSnapshotView)) services.LibraryWebSnapshotView {
	v := wsView(version)
	for _, f := range edit {
		f(&v)
	}
	return v
}

func (rr *readerRig) hasToolbarButton(name string) bool { return rr.has(gfx.RoleButton, name) }

// ---- 进入与显示 ----

// 进入快照页：工具条在标签页那一行里（模式文字、宽度下拉、「更多」），岛里显示原文快照（整页文档、实例标识、宽度档、图片清单）。
func TestSnapshotPageShowsTheOriginalSnapshotInTheIsland(t *testing.T) {
	t.Parallel()
	view := snapView("v2", func(v *services.LibraryWebSnapshotView) {
		v.Version.Snapshot.Assets = []services.LibraryWebSnapshotAsset{{FileName: "a b.png", SHA256: "h", Bytes: 1}}
	})
	rr := snapshotRig(t, view)
	p := rr.enterSnapshot()
	got := rr.viewOf(p)
	if got["kind"] != "snapshot" || got["document"] != "<p>v2</p>" || got["instanceId"] != "inst-v2" || got["width"] != "fluid" {
		t.Fatalf("岛里应是原文快照：%v", got)
	}
	if assets, _ := got["assets"].([]any); len(assets) != 1 || assets[0] != "local-image://a b.png" {
		t.Errorf("图片清单应是快照里的本地图片：%v", got["assets"])
	}
	for _, name := range []string{locales.LibrarySnapshotWidth(), locales.CommonMore()} {
		if !rr.has(gfx.RoleCombobox, name) {
			t.Errorf("原文快照的工具条应有「%s」", name)
		}
	}
	if !rr.hasText(locales.ArticleAskViewSnapshot()) {
		t.Errorf("模式文字应是「%s」：%q", locales.ArticleAskViewSnapshot(), rr.session.Texts())
	}
	if rr.hasToolbarButton(locales.LibrarySnapshotViewOriginal()) || rr.hasToolbarButton(locales.LibrarySnapshotBackToEdited()) {
		t.Error("没改过正文时，工具条没有排版切换钮")
	}
	for _, name := range []string{locales.LibrarySnapshotMissing(), locales.LibrarySnapshotLoading(), locales.LibrarySnapshotEditedHint(), locales.LibrarySnapshotOriginalHint()} {
		if rr.hasText(name) {
			t.Errorf("正常的快照没有通知条「%s」", name)
		}
	}
	if calls := rr.svc.SnapshotCalls(); len(calls) != 1 || calls[0].ItemID != detailReviewID || calls[0].VersionID != "" {
		t.Errorf("进入快照页读一次当前版本：%+v", calls)
	}
}

// 离开再进入：面板是重新挂载，重新读取；回阅读时岛里换回正文。
func TestSnapshotPageRereadsWhenEnteredAgain(t *testing.T) {
	t.Parallel()
	rr := snapshotRig(t, snapView("v2"))
	rr.enterSnapshot()
	rr.click(gfx.RoleButton, locales.LibraryBackToReading())
	rr.click(gfx.RoleButton, locales.ArticleAskViewSnapshot())
	if n := len(rr.svc.SnapshotCalls()); n != 2 {
		t.Fatalf("再进入应重新读取：读了 %d 次", n)
	}
	if rr.screen.detail.panel.snap.simple || !rr.has(gfx.RoleCombobox, locales.LibrarySnapshotWidth()) {
		t.Error("再进入应是全新的面板：按默认排版显示原文快照")
	}
}

// 读取中：只有「正在读取」通知条，工具条没有宽度下拉；读完后消失。
func TestSnapshotLoadingNoticeUntilTheReadFinishes(t *testing.T) {
	t.Parallel()
	var q spawnQueue
	rr := snapshotRig(t, snapView("v2"))
	rr.store.Spawn = q.Spawn // 条目已选中、列表已读完，之后的读取才排队
	rr.click(gfx.RoleButton, locales.ArticleAskViewSnapshot())
	if !rr.hasText(locales.LibrarySnapshotLoading()) {
		t.Fatalf("读取中应有「%s」：%q", locales.LibrarySnapshotLoading(), rr.session.Texts())
	}
	if rr.has(gfx.RoleCombobox, locales.LibrarySnapshotWidth()) {
		t.Error("读取中没有文档，不该有宽度下拉")
	}
	q.Run()
	rr.frames(3)
	if rr.hasText(locales.LibrarySnapshotLoading()) || !rr.has(gfx.RoleCombobox, locales.LibrarySnapshotWidth()) {
		t.Fatalf("读完后应是原文快照的工具条：%q", rr.session.Texts())
	}
}

// 读取失败：显示原因与「重试读取」（assertive 状态），重试成功后条消失、岛里出现快照。
func TestSnapshotLoadFailureShowsTheReasonAndRetry(t *testing.T) {
	t.Parallel()
	rr := snapshotRig(t, snapView("v2"), func(r *rig) { r.svc.FailOn("WebSnapshot", errors.New("磁盘读不出来")) })
	rr.click(gfx.RoleButton, locales.ArticleAskViewSnapshot())
	if !rr.hasText("磁盘读不出来") || !rr.has(gfx.RoleButton, locales.LibrarySnapshotRetryLoad()) {
		t.Fatalf("应显示失败原因与重试钮：%q", rr.session.Texts())
	}
	if rr.hasText(locales.LibrarySnapshotLoading()) || rr.hasText(locales.LibrarySnapshotMissing()) {
		t.Error("读取失败不能渲染成读取中或没有快照")
	}
	live := false
	for _, n := range rr.session.Access() {
		live = live || (n.Role == gfx.RoleStatus && n.Live == gfx.LiveAssertive)
	}
	if !live {
		t.Error("读取失败条应是 assertive 的状态")
	}
	rr.svc.FailOn("WebSnapshot", nil)
	rr.click(gfx.RoleButton, locales.LibrarySnapshotRetryLoad())
	if rr.hasText("磁盘读不出来") || !rr.has(gfx.RoleCombobox, locales.LibrarySnapshotWidth()) {
		t.Fatalf("重试成功后应进入快照：%q", rr.session.Texts())
	}
}

// 没有保存过快照：显示标准排版与「尚未保存网页快照」条，点「补采」走补采入队；工具条没有宽度下拉。
func TestSnapshotMissingOffersSupplement(t *testing.T) {
	t.Parallel()
	rr := snapshotRig(t, services.LibraryWebSnapshotView{Document: "", InstanceID: "none"})
	rr.click(gfx.RoleButton, locales.ArticleAskViewSnapshot())
	if !rr.hasText(locales.LibrarySnapshotMissing()) {
		t.Fatalf("应显示「%s」：%q", locales.LibrarySnapshotMissing(), rr.session.Texts())
	}
	if rr.has(gfx.RoleCombobox, locales.LibrarySnapshotWidth()) {
		t.Error("没有快照时没有宽度下拉")
	}
	rr.click(gfx.RoleButton, locales.LibrarySnapshotSupplement())
	if !slices.Contains(rr.svc.Calls(), "SupplementWechatTasks") {
		t.Fatalf("点补采应入队：%v", rr.svc.Calls())
	}
	if !rr.hasText(locales.LibrarySnapshotSupplementing()) {
		t.Errorf("补采在途应有「%s」提示：%q", locales.LibrarySnapshotSupplementing(), rr.session.Texts())
	}
	if !rr.mustNode(gfx.RoleButton, locales.LibrarySnapshotSupplement()).Disabled {
		t.Error("补采在途时补采钮不可再点")
	}
}

// 缺失资源与警告：琥珀色的 polite 状态条，原因用分隔符连起来；缺失原因不丢。
func TestSnapshotIncompleteBarListsEveryReason(t *testing.T) {
	t.Parallel()
	view := snapView("v2", func(v *services.LibraryWebSnapshotView) {
		v.Version.Snapshot.Warnings = []string{"字体未保存"}
		v.Version.Snapshot.Failures = []services.LibraryWebSnapshotFailure{{URL: "https://x/a.png", Reason: "图片 404"}}
	})
	rr := snapshotRig(t, view)
	rr.click(gfx.RoleButton, locales.ArticleAskViewSnapshot())
	want := "字体未保存" + locales.NativeLibraryReasonSeparator() + "图片 404"
	if !rr.hasText(want) {
		t.Fatalf("应有缺失条「%s」：%q", want, rr.session.Texts())
	}
	polite := false
	for _, n := range rr.session.Access() {
		polite = polite || (n.Role == gfx.RoleStatus && n.Live == gfx.LivePolite)
	}
	if !polite {
		t.Error("缺失条应是 polite 的状态")
	}
}

// ---- 已编辑：默认看标准排版，可来回切换 ----

func TestEditedSnapshotStartsInTheStandardLayoutAndSwitches(t *testing.T) {
	t.Parallel()
	rr := snapshotRig(t, snapView("v2", func(v *services.LibraryWebSnapshotView) { v.Edited = true }))
	p := rr.enterSnapshot()
	if got := rr.viewOf(p); got["kind"] == "snapshot" {
		t.Fatalf("正文改过时默认看标准排版：%v", got)
	}
	if !rr.hasText(locales.LibrarySnapshotEditedHint()) || !rr.hasText(locales.LibrarySnapshotModeEdited()) {
		t.Fatalf("标准排版应有已编辑的模式文字与提示：%q", rr.session.Texts())
	}
	if rr.has(gfx.RoleCombobox, locales.LibrarySnapshotWidth()) {
		t.Error("标准排版没有宽度下拉")
	}
	rr.click(gfx.RoleButton, locales.LibrarySnapshotViewOriginal())
	if got := rr.viewOf(p); got["kind"] != "snapshot" || got["document"] != "<p>v2</p>" {
		t.Fatalf("点「%s」后岛里应是原文快照：%v", locales.LibrarySnapshotViewOriginal(), got)
	}
	if !rr.hasText(locales.LibrarySnapshotOriginalHint()) || rr.hasText(locales.LibrarySnapshotEditedHint()) {
		t.Errorf("原文快照应换成「不含修改」提示：%q", rr.session.Texts())
	}
	rr.click(gfx.RoleButton, locales.LibrarySnapshotBackToEdited())
	if got := rr.viewOf(p); got["kind"] == "snapshot" {
		t.Fatalf("返回编辑后正文：%v", got)
	}
}

// 「更多」菜单里也有切换：原文快照时是「查看编辑后正文」，选了就是标准排版。
func TestMoreMenuSwitchesToTheEditedText(t *testing.T) {
	t.Parallel()
	rr := snapshotRig(t, snapView("v2", func(v *services.LibraryWebSnapshotView) { v.Edited = true }))
	p := rr.enterSnapshot()
	rr.click(gfx.RoleButton, locales.LibrarySnapshotViewOriginal())
	rr.click(gfx.RoleCombobox, locales.CommonMore())
	if !rr.has(gfx.RoleMenuItem, locales.LibrarySnapshotViewEdited()) {
		t.Fatalf("已编辑时菜单项应叫「%s」", locales.LibrarySnapshotViewEdited())
	}
	rr.click(gfx.RoleMenuItem, locales.LibrarySnapshotViewEdited())
	if got := rr.viewOf(p); got["kind"] == "snapshot" {
		t.Fatalf("选了之后岛里应是标准排版：%v", got)
	}
}

// ---- 宽度档 ----

// 宽度下拉三档；选了写进偏好、给岛的是增量 ui 消息（不重发整页）；下次进入读回偏好。
func TestWidthSelectPersistsAndDrivesTheIsland(t *testing.T) {
	t.Parallel()
	rr := snapshotRig(t, snapView("v2"))
	p := rr.enterSnapshot()
	views := len(p.messages(t, "view"))
	rr.click(gfx.RoleCombobox, locales.LibrarySnapshotWidth())
	for _, name := range []string{locales.LibrarySnapshotWidthOriginal(), locales.LibrarySnapshotWidthFluid(), locales.LibrarySnapshotWidthWide()} {
		if !rr.has(gfx.RoleOption, name) {
			t.Errorf("宽度下拉应有「%s」", name)
		}
	}
	rr.click(gfx.RoleOption, locales.LibrarySnapshotWidthWide())
	if ui := p.last(t, "ui"); ui["snapshotWidth"] != "wide" {
		t.Fatalf("岛应收到宽度 wide：%v", ui)
	}
	if len(p.messages(t, "view")) != views {
		t.Error("只改宽度不该重发整页")
	}
	raw, ok := rr.prefs.Get(settings.PrefWechatWidth)
	var saved string
	if !ok || json.Unmarshal(raw, &saved) != nil || saved != "wide" {
		t.Fatalf("宽度偏好应存 wide：%s %v", raw, ok)
	}
	// 离开再进入：从偏好读回。
	rr.click(gfx.RoleButton, locales.LibraryBackToReading())
	rr.click(gfx.RoleButton, locales.ArticleAskViewSnapshot())
	if got := rr.viewOf(rr.page()); got["width"] != "wide" {
		t.Fatalf("再进入应读回偏好：%v", got)
	}
}

func TestSnapshotWidthPreferenceFallsBackToFluid(t *testing.T) {
	t.Parallel()
	if loadSnapshotWidth(nil) != reader.SnapshotWidthFluid {
		t.Error("没有偏好存储时自适应")
	}
	for raw, want := range map[string]reader.SnapshotWidth{
		`"original"`: reader.SnapshotWidthOriginal, `"wide"`: reader.SnapshotWidthWide,
		`"fluid"`: reader.SnapshotWidthFluid, `"giant"`: reader.SnapshotWidthFluid, `7`: reader.SnapshotWidthFluid,
	} {
		rr := newRig(t, func(r *rig) { _ = r.prefs.Set(settings.PrefWechatWidth, json.RawMessage(raw)) })
		if got := loadSnapshotWidth(rr.prefs); got != want {
			t.Errorf("偏好 %s：得到 %q，应为 %q", raw, got, want)
		}
	}
}

// ---- 目录钮：只在目录没有常驻侧栏时摆 ----

func TestTocButtonFollowsThePageLayoutReport(t *testing.T) {
	t.Parallel()
	rr := snapshotRig(t, snapView("v2"))
	p := rr.enterSnapshot()
	toc := locales.LibraryCatalog()
	if !rr.has(gfx.RoleButton, toc) {
		t.Fatal("页面还没回报常驻侧栏时，窄窗口的「目录」钮应在")
	}
	rr.event(p, map[string]any{"t": "snapshotState", "wideRail": true, "open": false})
	if rr.has(gfx.RoleButton, toc) {
		t.Fatal("目录常驻侧栏时没有「目录」钮")
	}
	rr.event(p, map[string]any{"t": "snapshotState", "wideRail": false, "open": false})
	if !rr.has(gfx.RoleButton, toc) || rr.mustNode(gfx.RoleButton, toc).Expanded {
		t.Fatal("窄窗口：有「目录」钮，收起")
	}
	rr.click(gfx.RoleButton, toc)
	if ui := p.last(t, "ui"); ui["tocOpen"] != true {
		t.Fatalf("点「目录」应让岛展开目录：%v", ui)
	}
	if !rr.mustNode(gfx.RoleButton, toc).Expanded {
		t.Error("展开后钮是 aria-expanded")
	}
	// 页面自己关掉目录浮层（点了条目或遮罩）：回报 open=false，钮跟着收起。
	rr.event(p, map[string]any{"t": "snapshotState", "wideRail": false, "open": false})
	if rr.mustNode(gfx.RoleButton, toc).Expanded {
		t.Error("页面收起目录后，钮也应收起")
	}
}

// ---- 「更多」菜单 ----

func TestMoreMenuOffersSupplementExportAndCancel(t *testing.T) {
	t.Parallel()
	rr := snapshotRig(t, snapView("v2"))
	rr.enterSnapshot()
	rr.click(gfx.RoleCombobox, locales.CommonMore())
	for _, name := range []string{locales.LibrarySnapshotSupplementUpdate(), locales.LibrarySnapshotExportHtml()} {
		if !rr.has(gfx.RoleMenuItem, name) {
			t.Errorf("菜单应有「%s」", name)
		}
	}
	if rr.has(gfx.RoleMenuItem, locales.LibrarySnapshotCancelSupplement()) {
		t.Error("没在补采时没有「取消补采」")
	}
	rr.click(gfx.RoleMenuItem, locales.LibrarySnapshotExportHtml())
	if calls := rr.svc.SnapshotExports(); len(calls) != 1 || calls[0].ItemID != detailReviewID || calls[0].VersionID != "v2" {
		t.Fatalf("导出应带上快照的版本：%+v", calls)
	}
	if !strings.Contains(rr.toastText(), locales.LibrarySnapshotExported()) {
		t.Errorf("导出成功应有提示：%q", rr.toastText())
	}

	rr.click(gfx.RoleCombobox, locales.CommonMore())
	rr.click(gfx.RoleMenuItem, locales.LibrarySnapshotSupplementUpdate())
	if !slices.Contains(rr.svc.Calls(), "SupplementWechatTasks") {
		t.Fatalf("补采应入队：%v", rr.svc.Calls())
	}
	rr.click(gfx.RoleCombobox, locales.CommonMore())
	if !rr.mustNode(gfx.RoleMenuItem, locales.LibrarySnapshotSupplementUpdate()).Disabled {
		t.Error("补采在途时「补采」菜单项不可再点")
	}
	rr.click(gfx.RoleMenuItem, locales.LibrarySnapshotCancelSupplement())
	if len(rr.svc.CanceledImportTasks()) != 1 {
		t.Fatalf("取消补采应取消在途任务：%v", rr.svc.CanceledImportTasks())
	}
}

// 有缺失资源时菜单项叫「重试缺失资源」（不再叫「补采」）。
func TestMoreMenuRetryMissingReplacesSupplementWhenAssetsFailed(t *testing.T) {
	t.Parallel()
	view := snapView("v2", func(v *services.LibraryWebSnapshotView) {
		v.Version.Snapshot.Failures = []services.LibraryWebSnapshotFailure{{URL: "https://x/a.png", Reason: "图片 404"}}
	})
	rr := snapshotRig(t, view)
	rr.enterSnapshot()
	rr.click(gfx.RoleCombobox, locales.CommonMore())
	if !rr.has(gfx.RoleMenuItem, locales.LibrarySnapshotRetryMissing()) || rr.has(gfx.RoleMenuItem, locales.LibrarySnapshotSupplementUpdate()) {
		t.Fatal("有缺失资源时应是「重试缺失资源」")
	}
}

// ---- 版本面板：切到那个版本的快照 ----

// 版本面板里带快照的版本有「查看网页快照」：点了阅读区切到这一版本的快照（按版本读取），工具侧栏收起。
func TestVersionPanelButtonShowsThatVersionsSnapshot(t *testing.T) {
	t.Parallel()
	versions := reviewVersions()
	versions.Versions[0].Snapshot = []byte(`{"html":"<p>ok</p>"}`)
	rr := newReaderRig(t, func(r *rig) {
		r.svc.SeedWebVersions(detailReviewID, versions)
		r.svc.SeedSnapshot(detailReviewID, "v-new", snapView("v-new"))
	})
	rr.openTools(detailReviewID)
	rr.clickIn(rr.panelRect(), gfx.RoleButton, versionsChipName())
	rr.click(gfx.RoleCombobox, locales.WebCaptureVersions())
	rr.click(gfx.RoleOption, kitDate(rr.rig, 1_700_000_000_000)+" · engine-2")
	rr.clickIn(rr.panelRect(), gfx.RoleButton, locales.LibrarySnapshotViewOriginal())
	if calls := rr.svc.SnapshotCalls(); len(calls) == 0 || calls[len(calls)-1].VersionID != "v-new" {
		t.Fatalf("应按版本读取快照：%+v", calls)
	}
	if rr.screen.detail.tools.openID != "" {
		t.Error("切到快照时工具侧栏应收起")
	}
	p := rr.ready()
	if got := rr.viewOf(p); got["kind"] != "snapshot" || got["versionId"] != "v-new" || got["instanceId"] != "inst-v-new" {
		t.Fatalf("阅读区应是这一版本的快照：%v", got)
	}
	if rr.hasText(locales.LibrarySnapshotOriginalHint()) || rr.hasText(locales.LibrarySnapshotEditedHint()) {
		t.Error("看历史版本时不提示「已编辑」（渲染层 edited && !versionId）")
	}
}

// 没有快照的版本不摆这个入口。
func TestVersionPanelHasNoSnapshotButtonWithoutASnapshot(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t, func(r *rig) { r.svc.SeedWebVersions(detailReviewID, reviewVersions()) })
	rr.openTools(detailReviewID)
	rr.clickIn(rr.panelRect(), gfx.RoleButton, versionsChipName())
	rr.click(gfx.RoleCombobox, locales.WebCaptureVersions())
	rr.click(gfx.RoleOption, kitDate(rr.rig, 1_700_000_000_000)+" · engine-2")
	if _, ok := rr.nodeIn(rr.panelRect(), gfx.RoleButton, locales.LibrarySnapshotViewOriginal()); ok {
		t.Fatal("没有快照的版本没有「查看网页快照」")
	}
}

// ---- 切条目 ----

// 换条目：面板状态（排版、读取结果）随之清掉，回到阅读。
func TestSnapshotPaneResetsWhenTheItemChanges(t *testing.T) {
	t.Parallel()
	rr := snapshotRig(t, snapView("v2"))
	rr.enterSnapshot()
	rr.pick(detailNoteID)
	if rr.has(gfx.RoleCombobox, locales.LibrarySnapshotWidth()) || rr.screen.snapshotPaneOn(ItemCaps{IsWechat: true}) {
		t.Fatal("换到别的条目后不该还在快照页")
	}
	if sn := rr.screen.detail.panel.snap; sn.keyed || sn.view != nil || sn.version != "" {
		t.Fatalf("面板状态应已清空：%+v", sn)
	}
}

// 非公众号条目没有快照入口，也不读取快照。
func TestNonWechatItemNeverReadsASnapshot(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	if rr.has(gfx.RoleButton, locales.ArticleAskViewSnapshot()) {
		t.Fatal("笔记没有「查看网页快照」")
	}
	if len(rr.svc.SnapshotCalls()) != 0 {
		t.Fatalf("不读快照：%+v", rr.svc.SnapshotCalls())
	}
}

func kitDate(r *rig, ms int64) string {
	return kit.FormatDateTime(ms, r.screen.loc(), r.screen.Locale)
}
