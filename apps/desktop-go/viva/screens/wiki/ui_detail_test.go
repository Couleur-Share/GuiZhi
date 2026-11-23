package wiki

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 详情（L4）：正文里的 [[链接]] 与外链、页内目录、来源与关联、编辑保存、历史版本、交回自动编译、删除确认、读取失败。

// withLinkedBodies 摆三页：Alpha 的正文有两个 wiki 链接（一个存在、一个不存在）和一个外链，Beta 被 Alpha 引用。
func withLinkedBodies(r *uiRig) {
	r.svc.PutPage(services.WikiPage{
		ID: "alpha", Title: "Alpha",
		Body: "see [[Beta]] and [[Nowhere]] and [site](https://example.com/x) end",
	})
	r.svc.PutPage(services.WikiPage{ID: "beta", Title: "Beta", Body: "body of beta"})
	r.svc.PutLink("alpha", "beta")
	r.svc.PutSources("alpha",
		services.WikiPageSource{ItemID: "i1", Title: "Source One"},
		services.WikiPageSource{ItemID: "i2", Title: "Source Two"})
}

func TestUIDetailShowsHeaderBodyAndMeta(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton("Beta")
	if !r.has(gfx.RoleHeading, "Beta") || !r.hasText("body of beta") {
		t.Fatalf("详情应显示标题与正文：%q", r.session.Texts())
	}
	// Beta 手动编辑过：出处行是手动编辑时间，并带「手动编辑」标记。
	if !r.hasText(locales.WikiManualEdited()) {
		t.Errorf("手动编辑过的页面应有标记：%q", r.session.Texts())
	}
	if !r.hasButton(locales.WikiEditPage()) && !r.hasButton("编辑") {
		t.Error("应有编辑按钮")
	}
	// Beta 被 Alpha 引用：详情里有反向链接区。
	if !r.hasText(locales.WikiBacklinks()) || !r.hasButton("Alpha") {
		t.Errorf("应列出反向链接 Alpha：%q", r.session.Texts())
	}
}

func TestUIWikiLinkOpensTargetPage(t *testing.T) {
	r := newUIRig(t, withLinkedBodies)
	r.clickButton("Alpha")
	r.click(gfx.RoleButton, "Beta") // 站内链接在渲染层是 <button>
	if got := r.store.View().SelectedPageID; got != "beta" {
		t.Fatalf("点 [[Beta]] 应打开 beta：%q", got)
	}
	if !r.has(gfx.RoleHeading, "Beta") {
		t.Fatalf("详情应换成 Beta：%q", r.session.Texts())
	}
}

func TestUIWikiLinkToMissingPageSaysNotFound(t *testing.T) {
	r := newUIRig(t, withLinkedBodies)
	r.clickButton("Alpha")
	r.click(gfx.RoleButton, "Nowhere")
	r.wantNoSelectionChange("alpha")
	r.wantToast(kit.ToastInfo, locales.WikiLinkNotFound("Nowhere"), "")
}

func TestUIExternalLinkGoesToHook(t *testing.T) {
	r := newUIRig(t, withLinkedBodies)
	r.clickButton("Alpha")
	r.click(gfx.RoleLink, "site")
	if got := r.openedLinks(); len(got) != 1 || got[0] != "https://example.com/x" {
		t.Fatalf("外链应交给 OpenExternal：%v", got)
	}
	r.wantNoSelectionChange("alpha")
}

func TestUISourceChipOpensLibraryItem(t *testing.T) {
	r := newUIRig(t, withLinkedBodies)
	r.clickButton("Alpha")
	r.clickButton("Source Two")
	if got := r.openedSources(); len(got) != 1 || got[0] != "i2" {
		t.Fatalf("来源应跳到知识库条目 i2：%v", got)
	}
}

func TestUINeedsReviewSourceShowsStatus(t *testing.T) {
	r := newUIRig(t, withLinkedBodies, func(r *uiRig) {
		r.svc.PutSources("alpha", services.WikiPageSource{ItemID: "i1", Title: "Source One", ReviewStatus: "needs_review"})
	})
	r.clickButton("Alpha")
	if !r.has(gfx.RoleStatus, "") && !strings.Contains(r.allText(), locales.WikiSourcesNeedReview()) {
		t.Fatalf("有待复核来源时应提示：%q", r.session.Texts())
	}
}

func TestUIBacklinkChipOpensPage(t *testing.T) {
	r := newUIRig(t, withLinkedBodies)
	r.clickButton("Beta")
	// 目录里有 Alpha 一行（可访问名是整行内容，以「Alpha 」开头），详情的反向链接区里还有一个名为 Alpha 的芯片：点最后一个同名的（详情里的）。
	if n := r.count(gfx.RoleButton, "Alpha"); n != 1 {
		t.Fatalf("反向链接区应有一个名为 Alpha 的芯片：%d", n)
	}
	if _, ok := r.node(gfx.RoleButton, "Alpha"); !ok {
		t.Fatal("应能找到 Alpha")
	}
	r.clickNth(gfx.RoleButton, "Alpha", r.count(gfx.RoleButton, "Alpha")-1)
	if got := r.store.View().SelectedPageID; got != "alpha" {
		t.Fatalf("点反向链接应打开 alpha：%q", got)
	}
}

func TestUITocExpandsAndListsHeadings(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) {
		r.svc.PutPage(services.WikiPage{ID: "doc", Title: "Doc", Body: "# Doc\n\n## One\n\ntext\n\n## Two\n\ntext\n\n## Three\n\ntext\n"})
	})
	r.clickButton("Doc")
	toc := locales.WikiToc(3)
	if !r.hasButton(toc) {
		t.Fatalf("三节标题应出现页内目录：%q", r.session.Texts())
	}
	if r.hasButton("Two") {
		t.Fatal("目录默认收起")
	}
	r.clickButton(toc)
	for _, name := range []string{"One", "Two", "Three"} {
		if !r.hasButton(name) {
			t.Errorf("展开后应有「%s」", name)
		}
	}
	r.clickButton("Three") // 点条目跳到该节，不应崩，目录保持可用
	if !r.has(gfx.RoleHeading, "Three") {
		t.Errorf("跳转后「Three」标题应仍在：%q", r.session.Texts())
	}
}

func TestUIShortDocHasNoToc(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) {
		r.svc.PutPage(services.WikiPage{ID: "doc", Title: "Doc", Body: "## One\n\ntext\n\n## Two\n\ntext\n"})
	})
	r.clickButton("Doc")
	if r.hasButton(locales.WikiToc(2)) {
		t.Fatal("不足三节不出页内目录")
	}
}

// ---- 编辑 ----

func (r *uiRig) beginEdit() {
	r.t.Helper()
	r.clickButton(locales.WikiEditPage())
	if !r.store.View().Editing {
		r.t.Fatalf("应进入编辑：%q", r.session.Texts())
	}
}

// appendDraft 点进编辑框（光标落在原正文末尾），接着输入 text。无头会话里没有「全选」，所以断言的草稿是原正文加输入。
func (r *uiRig) appendDraft(text string) {
	r.t.Helper()
	r.click(gfx.RoleTextField, locales.WikiEditPage())
	r.keys("ctrl+end")
	r.typeText(text)
}

// menuItem 找标签以 label 开头的菜单项（带说明的菜单项，无障碍名是「标题 说明」）。
func (r *uiRig) menuItem(label string) (app.AccessNode, bool) {
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleMenuItem && (n.Label == label || strings.HasPrefix(n.Label, label+" ")) {
			return n, true
		}
	}
	return app.AccessNode{}, false
}

func (r *uiRig) hasMenuItem(label string) bool { _, ok := r.menuItem(label); return ok }

func (r *uiRig) clickMenuItem(label string) {
	r.t.Helper()
	n, ok := r.menuItem(label)
	if !ok {
		r.t.Fatalf("找不到菜单项「%s」；画面上有 %q", label, r.session.Texts())
	}
	r.clickAt(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
}

func TestUIEditSavesBodyAndLeavesEditing(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton("Alpha")
	r.beginEdit()
	r.appendDraft(" with [[Beta]]")
	if got := r.store.View().Draft; got != "body of alpha with [[Beta]]" {
		t.Fatalf("草稿应跟着输入：%q", got)
	}
	r.clickButton(locales.CommonSave())
	ups := r.svc.Updates()
	if len(ups) != 1 || ups[0].PageID != "alpha" || ups[0].Body != "body of alpha with [[Beta]]" {
		t.Fatalf("保存请求不对：%+v", ups)
	}
	if len(ups[0].LinkTargets) != 1 {
		t.Errorf("链接目标应从正文解析出来：%v", ups[0].LinkTargets)
	}
	if r.store.View().Editing {
		t.Fatal("保存成功后应退出编辑")
	}
	r.wantToast(kit.ToastSuccess, locales.WikiPageSaved(), "")
	if !r.hasButton("编辑") && !r.hasButton(locales.WikiEditPage()) {
		t.Error("退出编辑后应回到阅读视图")
	}
}

func TestUIEditCancelDiscardsDraft(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton("Alpha")
	r.beginEdit()
	r.appendDraft(" discard me")
	r.clickButton(locales.CommonCancel())
	if r.store.View().Editing || len(r.svc.Updates()) != 0 {
		t.Fatalf("取消不该保存：%+v", r.svc.Updates())
	}
	if !r.hasText("body of alpha") {
		t.Errorf("取消后正文还是原来的：%q", r.session.Texts())
	}
	// 再次进入编辑，编辑框应是原正文而不是上次的草稿。
	r.beginEdit()
	if got := r.store.View().Draft; got != "body of alpha" {
		t.Fatalf("再次编辑应从原正文开始：%q", got)
	}
}

func TestUIEditSaveFailureKeepsDraftAndReports(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton("Alpha")
	r.beginEdit()
	r.appendDraft(" keep me")
	r.svc.Fail(servicestest.WikiOpUpdatePage, errors.New("disk full"))
	r.clickButton(locales.CommonSave())
	if !r.store.View().Editing || r.store.View().Draft != "body of alpha keep me" {
		t.Fatalf("保存失败应留在编辑并保住草稿：%+v", r.store.View())
	}
	r.wantToast(kit.ToastError, locales.WikiPageSaveFailed(), "disk full")
}

// ---- 更多菜单 ----

func TestUIMoreMenuRestorePreviousRevision(t *testing.T) {
	r := newUIRig(t, withSeed, func(r *uiRig) {
		r.svc.PutRevision(services.WikiRevision{ID: "rev-1", PageID: "alpha", Title: "Alpha", Body: "older body", CreatedAt: 1})
	})
	r.clickButton("Alpha")
	r.clickButton(locales.CommonMore())
	if !r.hasMenuItem(locales.WikiRestorePrevious()) {
		t.Fatalf("有历史版本应有「恢复上一版」：%q", r.session.Texts())
	}
	r.clickMenuItem(locales.WikiRestorePrevious())
	if got := r.svc.Restores(); len(got) != 1 || got[0] != "rev-1" {
		t.Fatalf("应恢复 rev-1：%v", got)
	}
	r.wantToast(kit.ToastSuccess, locales.WikiRevisionRestored(), "")
	if !r.hasText("older body") {
		t.Errorf("恢复后应显示旧正文：%q", r.session.Texts())
	}
}

func TestUIMoreMenuHidesRestoreWithoutRevisions(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton("Alpha")
	r.clickButton(locales.CommonMore())
	if r.hasMenuItem(locales.WikiRestorePrevious()) {
		t.Fatal("没有历史版本不该有「恢复上一版」")
	}
	if r.hasMenuItem(locales.WikiReleaseToAuto()) {
		t.Fatal("没手动编辑过不该有「交回自动编译」")
	}
	if !r.hasMenuItem(locales.WikiDeletePage()) {
		t.Fatalf("删除页面始终在：%q", r.session.Texts())
	}
}

func TestUIMoreMenuReleaseToAuto(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton("Beta") // Beta 手动编辑过
	r.clickButton(locales.CommonMore())
	r.clickMenuItem(locales.WikiReleaseToAuto())
	ups := r.svc.Updates()
	if len(ups) != 1 || ups[0].PageID != "beta" || !ups[0].ReleaseToAuto {
		t.Fatalf("应按原正文存一次并清除手动标记：%+v", ups)
	}
	r.wantToast(kit.ToastSuccess, locales.WikiReleasedToAuto(), "")
	if r.hasText(locales.WikiManualEdited()) {
		t.Error("交回之后不该再有手动编辑标记")
	}
}

func TestUIDeleteAsksForConfirmationFirst(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton("Gamma")
	r.clickButton(locales.CommonMore())
	r.clickMenuItem(locales.WikiDeletePage())
	if len(r.svc.Deletes()) != 0 {
		t.Fatal("确认之前不该删除")
	}
	if !strings.Contains(noSpace(r.allText()), noSpace(locales.WikiDeletePageConfirm("Gamma"))) {
		t.Fatalf("应有删除确认：%q", r.session.Texts())
	}
	r.clickButton(locales.CommonCancel())
	if len(r.svc.Deletes()) != 0 || !r.hasButton("Gamma") {
		t.Fatalf("取消确认不该删除：%v", r.svc.Deletes())
	}
	r.clickButton(locales.CommonMore())
	r.clickMenuItem(locales.WikiDeletePage())
	r.clickButton(locales.CommonDelete())
	if got := r.svc.Deletes(); len(got) != 1 || got[0] != "gamma" {
		t.Fatalf("确认后应删除 gamma：%v", got)
	}
	r.wantToast(kit.ToastSuccess, locales.WikiPageDeleted(), "")
	if r.hasButton("Gamma") || !r.hasText(locales.WikiNoSelection()) {
		t.Fatalf("删除后目录不再有 Gamma，详情回到未选中：%q", r.session.Texts())
	}
}

func TestUIDeleteFailureKeepsPageAndReports(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton("Gamma")
	r.svc.Fail(servicestest.WikiOpDeletePage, errors.New("locked"))
	r.clickButton(locales.CommonMore())
	r.clickMenuItem(locales.WikiDeletePage())
	r.clickButton(locales.CommonDelete())
	r.wantToast(kit.ToastError, locales.WikiPageDeleteFailed(), "locked")
	if !r.hasButton("Gamma") {
		t.Fatal("删除失败页面应还在")
	}
}

// ---- 读取失败 ----

func TestUIPageLoadFailureShowsRetry(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.svc.Fail(servicestest.WikiOpGetPage, errors.New("bad page"))
	r.clickButton("Alpha")
	if !r.hasButton(locales.CommonRetry()) {
		t.Fatalf("页面读取失败应有重试：%q", r.session.Texts())
	}
	r.svc.Fail(servicestest.WikiOpGetPage, nil)
	r.clickButton(locales.CommonRetry())
	if !r.hasText("body of alpha") {
		t.Fatalf("重试成功后应显示正文：%q", r.session.Texts())
	}
}

// ---- 辅助 ----

// clickNth 点击第 n 个（从 0 起，按无障碍树顺序）角色与名称都匹配的节点。
func (r *uiRig) clickNth(role gfx.Role, name string, n int) {
	r.t.Helper()
	seen := 0
	for _, node := range r.session.Access() {
		if node.Role == role && node.Label == name {
			if seen == n {
				r.clickAt(node.Bounds.X+node.Bounds.Width/2, node.Bounds.Y+node.Bounds.Height/2)
				return
			}
			seen++
		}
	}
	r.t.Fatalf("找不到第 %d 个 %v「%s」（只有 %d 个）", n, role, name, seen)
}

// wantNoSelectionChange 断言选中的页面没有变。
func (r *uiRig) wantNoSelectionChange(id string) {
	r.t.Helper()
	if got := r.store.View().SelectedPageID; got != id {
		r.t.Fatalf("选中页面不该变：got %q want %q", got, id)
	}
}

// 正文里多个段落、列表项各带链接：每段的链接命中区键不能重名（曾因键只含行号与片段号，同一容器里两段都有首行第二截链接而 panic）。
func TestUIWikiLinksInSeveralBlocksDoNotCollide(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) {
		r.svc.PutPage(services.WikiPage{
			ID: "alpha", Title: "Alpha",
			Body: "see [[Beta]] first\n\n- item [[Beta]]\n- item [[Beta]]\n\nlast [[Beta]]",
		})
		r.svc.PutPage(services.WikiPage{ID: "beta", Title: "Beta", Body: "body of beta"})
	})
	r.clickButton("Alpha")
	if !r.has(gfx.RoleHeading, "Alpha") {
		t.Fatalf("详情应显示 Alpha：%q", r.session.Texts())
	}
}

// 站内链接小块（渲染层是 <button>，px-0.5）的命中区与可访问外框连两端的 2px 内边距一起：行首的链接，外框左缘就是行盒左缘，
// 而不是文字起点（曾比渲染层窄 4px、左缘偏 2px）。
func TestUIWikiLinkChipBoundsIncludePadding(t *testing.T) {
	r := newUIRig(t, func(r *uiRig) {
		r.svc.PutPage(services.WikiPage{ID: "alpha", Title: "Alpha", Body: "[[Beta]] opens"})
		r.svc.PutPage(services.WikiPage{ID: "beta", Title: "Beta", Body: "body of beta"})
	})
	r.clickButton("Alpha")
	title, ok := r.node(gfx.RoleHeading, "Alpha")
	if !ok {
		t.Fatalf("应有标题 Alpha：%q", r.session.Texts())
	}
	chip, ok := r.node(gfx.RoleButton, "Beta")
	if !ok {
		t.Fatalf("行首的 [[Beta]] 应是按钮：%q", r.session.Texts())
	}
	if chip.Bounds.X != title.Bounds.X {
		t.Errorf("行首链接小块的外框左缘应在行盒左缘：chip %.2f，标题 %.2f", chip.Bounds.X, title.Bounds.X)
	}
}

// 正文很长时右栏在自己的滚动区里滚，不把整个工作区撑高（曾因右栏直接放在两栏的行里没有被夹住，内容超出视口时整页被撑出窗口）。
func TestUILongPageScrollsInsideDetail(t *testing.T) {
	var body strings.Builder
	for i := range 80 {
		body.WriteString("paragraph ")
		body.WriteString(strings.Repeat("x", i%7+3))
		body.WriteString("\n\n")
	}
	r := newUIRig(t, func(r *uiRig) {
		r.svc.PutPage(services.WikiPage{ID: "long", Title: "Long", Body: body.String()})
	})
	r.clickButton("Long")
	before := r.top(gfx.RoleButton, locales.WikiEditPage())
	r.scrollBy(900, 400, 300)
	// 滚过之后编辑钮要么滚出视口（出了无障碍树），要么在更靠上的位置。
	if edit, ok := r.node(gfx.RoleButton, locales.WikiEditPage()); ok && edit.Bounds.Y >= before {
		t.Errorf("长页应能在右栏里滚动：编辑钮 y 滚前 %.1f，滚后 %.1f", before, edit.Bounds.Y)
	}
}
