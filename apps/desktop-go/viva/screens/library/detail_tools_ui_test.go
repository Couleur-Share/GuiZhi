package library

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测文章工具侧栏的界面接线（detailtools.go、detailtags.go、detailhandoff.go、detailcomments.go、detailversions.go，台账 B-060）：
// 谁在什么条件下出现、点什么触发了什么。评论、版本与交接稿背后的异步规则在 store_tools_test.go 里测过，这里只测界面接对了没有。

// openTools 选中一条、从头部「更多」菜单打开工具侧栏。
func (r *rig) openTools(id string) {
	r.t.Helper()
	r.pick(id)
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.ArticleReaderTools())
	if !r.toolsShown() {
		r.t.Fatal("夹具失效：点「文章信息与工具」应打开侧栏")
	}
}

// toolsShown 报告工具侧栏此刻有没有画出来（它是一个带名字的区域）。
func (r *rig) toolsShown() bool { return r.has(gfx.RoleGroup, locales.ArticleReaderTools()) }

// panelRect 是工具侧栏此刻的外框。
func (r *rig) panelRect() gfx.Rect {
	r.t.Helper()
	return r.mustNode(gfx.RoleGroup, locales.ArticleReaderTools()).Bounds
}

// inside 报告 inner 是否整个落在 outer 里（容差 1 像素）。
func inside(inner, outer gfx.Rect) bool {
	return inner.X >= outer.X-1 && inner.Y >= outer.Y-1 &&
		inner.X+inner.Width <= outer.X+outer.Width+1 && inner.Y+inner.Height <= outer.Y+outer.Height+1
}

// nodeIn 取外框落在 area 里的第一个 role、name 节点：侧栏与列表里有同名的按钮（标签名、文字相同的小块）时认准面板里的那一个。
func (r *rig) nodeIn(area gfx.Rect, role gfx.Role, name string) (app.AccessNode, bool) {
	for _, n := range r.session.Access() {
		if n.Role == role && n.Label == name && inside(n.Bounds, area) {
			return n, true
		}
	}
	return app.AccessNode{}, false
}

// clickIn 点 area 里的 role、name 节点。
func (r *rig) clickIn(area gfx.Rect, role gfx.Role, name string) {
	r.t.Helper()
	n, ok := r.nodeIn(area, role, name)
	if !ok {
		r.t.Fatalf("在 %+v 里找不到 %v「%s」", area, role, name)
	}
	x, y := centerOf(n)
	r.clickAt(x, y)
}

// hasTextIn 报告 area 里有没有恰好等于 text 的一行文字：居中的文字外框的 X 是字形起点、宽是整行的宽（右缘可以超出），
// 所以只看左上角落在 area 里。
func (r *rig) hasTextIn(area gfx.Rect, text string) bool {
	for _, tx := range r.session.Snapshot().Texts {
		if tx.Value == text && inside(gfx.Rect{X: tx.Bounds.X, Y: tx.Bounds.Y, Width: 0, Height: tx.Bounds.Height}, area) {
			return true
		}
	}
	return false
}

// moreItems 是此刻选中条目的头部「更多」菜单内容。侧栏开着时「更多」钮被它盖住、点不到（渲染层同：面板 z-30 盖住头部右侧，
// 只能键盘走到），所以开着时的菜单内容直接读函数的结果；comments 为真时按「来源支持评论」算。
func (r *rig) moreItems(comments bool) []kit.MenuItem {
	r.t.Helper()
	d := r.view().Detail
	return r.screen.moreMenuItems(*d.Item, ItemCaps{CommentsSupported: comments}, d.Tools)
}

// rigCalls 是服务收到的、以 prefix 开头的调用数。
func rigCalls(r *rig, prefix string) int {
	n := 0
	for _, call := range r.svc.Calls() {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

// 头部「更多」菜单第一项打开侧栏：焦点落在收起钮上，菜单项变成「收起文章工具」，选它收起；侧栏开着时「更多」钮被面板盖住。
func TestToolsPanelOpensFromTheMoreMenuAndTakesTheFocus(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.pick(detailNoteID)
	if r.toolsShown() {
		t.Fatal("没打开之前不该有侧栏")
	}
	if got := r.moreItems(false)[0].Label; got != locales.ArticleReaderTools() {
		t.Fatalf("侧栏关着时菜单项应是「%s」，实际「%s」", locales.ArticleReaderTools(), got)
	}
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.ArticleReaderTools())
	if !r.toolsShown() {
		t.Fatal("点菜单项应打开侧栏")
	}
	if !r.focusedIs(gfx.RoleButton, locales.ArticleReaderCloseTools()) {
		t.Fatal("侧栏一打开焦点应落在收起钮上")
	}
	items := r.moreItems(false)
	if items[0].Label != locales.ArticleReaderCloseTools() {
		t.Fatalf("侧栏开着时菜单项应是「%s」，实际「%s」", locales.ArticleReaderCloseTools(), items[0].Label)
	}
	items[0].OnSelect()
	r.frames(3)
	if r.toolsShown() {
		t.Fatal("选「收起文章工具」应收起侧栏")
	}
}

// 侧栏盖住头部右侧：点「更多」钮的中心落在面板上，打不开菜单（渲染层 z-30 同）；面板自己的收起钮与 Esc 是出口。
func TestToolsPanelCoversTheMoreButton(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	more, ok := r.detailNode(gfx.RoleCombobox, locales.LibraryMoreActions())
	if !ok {
		t.Fatal("头部应还有「更多」钮（只是被面板盖住）")
	}
	x, y := centerOf(more)
	r.clickAt(x, y)
	if r.has(gfx.RoleMenuItem, locales.ArticleReaderCloseTools()) {
		t.Fatalf("「更多」钮 %+v 在面板 %+v 之下，点它不该打开菜单", more.Bounds, r.panelRect())
	}
	if !r.toolsShown() {
		t.Fatal("点在面板上不该收起它")
	}
}

// 面板贴在文章栏右上：右缘、上缘与下缘各缩进 1rem，宽 26rem。
func TestToolsPanelSitsAtTheTopRightOfTheArticle(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	panel := r.panelRect()
	area := r.screen.detail.tools.area
	if area.Width <= 0 {
		t.Fatal("文章栏的外框应已量到")
	}
	if want := r.look.Rem(26); panel.Width != want {
		t.Errorf("面板宽应是 26rem（%.0f），实际 %.1f", want, panel.Width)
	}
	if got, want := area.X+area.Width-(panel.X+panel.Width), r.look.Space(4); got != want {
		t.Errorf("右缘应离文章栏 1rem（%.0f），实际 %.1f", want, got)
	}
	if got, want := panel.Y-area.Y, r.look.Space(4); got != want {
		t.Errorf("上缘应离文章栏 1rem（%.0f），实际 %.1f", want, got)
	}
	if got, want := area.Y+area.Height-(panel.Y+panel.Height), r.look.Space(4); got != want {
		t.Errorf("下缘应离文章栏 1rem（%.0f），实际 %.1f", want, got)
	}
}

// 面板宽 = min(26rem, 栏宽 − 2rem)；栏宽没量到（首帧）取 26rem。
func TestToolsPanelWidthYieldsToANarrowArticle(t *testing.T) {
	t.Parallel()
	look := kit.NewLook(kit.DefaultSpec())
	look.Scale = 1
	for _, tc := range []struct {
		area gfx.Rect
		want float32
	}{
		{gfx.Rect{}, 416},
		{gfx.Rect{Width: 900}, 416},
		{gfx.Rect{Width: 448}, 416},
		{gfx.Rect{Width: 447}, 415},
		{gfx.Rect{Width: 300}, 268},
		{gfx.Rect{Width: 20}, 0},
	} {
		if got := toolsPanelWidth(look, tc.area); got != tc.want {
			t.Errorf("栏宽 %.0f：面板宽应是 %.0f，实际 %.1f", tc.area.Width, tc.want, got)
		}
	}
}

// 收起钮与 Esc 都收起侧栏；焦点在侧栏之外时 Esc 不收它；专注阅读里侧栏里的 Esc 只收侧栏、不退出专注（stopPropagation）。
func TestToolsPanelClosesFromTheButtonAndEscapeOnlyWhileTheFocusIsInside(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	r.click(gfx.RoleButton, locales.ArticleReaderCloseTools())
	if r.toolsShown() {
		t.Fatal("收起钮应收起侧栏")
	}

	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.ArticleReaderTools())
	r.keys("escape")
	if r.toolsShown() {
		t.Fatal("焦点在收起钮上时 Esc 应收起侧栏")
	}

	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.ArticleReaderTools())
	r.click(gfx.RoleTextField, locales.LibraryTitlePlaceholder())
	r.keys("escape")
	if !r.toolsShown() {
		t.Fatal("焦点在标题框里时 Esc 不该收起侧栏")
	}

	// 专注阅读：侧栏里的 Esc 只收侧栏，下一个 Esc 才退出专注。
	f := shellRig(t)
	f.pick(detailNoteID)
	f.screen.SetFocusReading(true)
	f.frames(3)
	f.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	f.click(gfx.RoleMenuItem, locales.ArticleReaderTools())
	f.keys("escape")
	if f.toolsShown() || !f.screen.FocusReading() {
		t.Fatalf("专注阅读里侧栏的 Esc 只收侧栏：侧栏开着=%v 专注=%v", f.toolsShown(), f.screen.FocusReading())
	}
	f.keys("escape")
	if f.screen.FocusReading() {
		t.Fatal("下一个 Esc 应退出专注阅读")
	}
}

// 关闭侧栏后焦点哪里也不在：渲染层 effect 清理里的 previous.focus() 拿到的是 body（ContextMenu 在选中菜单项时被摘掉，焦点已落到 body），
// 收起钮与 Esc 两条路都一样。这是照原样复刻，不是为键盘用户设计的去向（候选改进见台账 C-014）。
func TestToolsPanelLeavesNoFocusWhenItCloses(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Esc", "收起钮"} {
		r := shellRig(t)
		r.openTools(detailNoteID)
		if !r.focusedIs(gfx.RoleButton, locales.ArticleReaderCloseTools()) {
			t.Fatal("夹具失效：打开时焦点应在收起钮上")
		}
		if name == "Esc" {
			r.keys("escape")
		} else {
			r.click(gfx.RoleButton, locales.ArticleReaderCloseTools())
		}
		if r.toolsShown() {
			t.Fatalf("%s：应已收起", name)
		}
		for _, n := range r.session.Access() {
			if n.Focused {
				t.Errorf("%s：关闭后不该有控件拿着焦点，实际 %v「%s」", name, n.Role, n.Label)
			}
		}
	}
}

// 侧栏属于打开它的那一条：换条目它就不在，换回来又在（渲染层 toolsItemId 是条目 ID）；回收站里的条目没有侧栏；
// 进出专注阅读是 ItemDetail 的重新挂载，侧栏随之回到关闭。
func TestToolsPanelBelongsToItsItemAndHidesInTheTrash(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	r.pick(detailForumID)
	if r.toolsShown() {
		t.Fatal("换了条目，侧栏不该跟过去")
	}
	r.pick(detailNoteID)
	if !r.toolsShown() {
		t.Fatal("换回原条目，侧栏还是开着的（toolsItemId 没变）")
	}
	r.screen.SetFocusReading(true)
	r.frames(3)
	if r.toolsShown() {
		t.Fatal("进入专注阅读是重新挂载，侧栏应回到关闭")
	}

	tr := shellRig(t)
	tr.pickTrashed(shellTrashNoteID)
	tr.screen.detail.tools.openID = shellTrashNoteID
	tr.frames(3)
	if tr.toolsShown() {
		t.Fatal("回收站里的条目没有工具侧栏")
	}
}

// 文章信息：内容类型、正文长度（去空白后的字数）、最近更新三列，加标签与内容处理两个小标题；头部第二行是条目标题。
func TestToolsPanelShowsTheArticleInfo(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	item := r.view().Detail.Item
	panel := r.panelRect()
	for _, text := range []string{
		locales.ArticleReaderTools(), item.Title,
		locales.ArticleReaderType(), kit.GetItemTypeMeta(item.ItemType).Label,
		locales.ArticleReaderLength(), locales.LibraryWordCount(wordCount(item.Content)),
		locales.ArticleReaderUpdated(), kit.FormatItemTime(item.UpdatedAt, r.screen.nowTime(), r.screen.loc(), r.screen.Locale),
		locales.ArticleReaderTags(), locales.ArticleReaderProcessing(),
	} {
		if !r.hasTextIn(panel, text) {
			t.Errorf("面板里应有「%s」；画面文字 %q", text, r.session.Texts())
		}
	}
}

// ---- 标签 ----

// 没有标签时「+」带「添加标签」文字；点开浮层，输入加回车就写进详情的草稿，标签块出现在行里；再点标签上的 X 移除。
func TestTagEditorAddsAndRemovesATag(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	panel := r.panelRect()
	if !r.hasTextIn(panel, locales.LibraryAddTag()) {
		t.Fatal("没有标签时「+」钮应带「添加标签」文字")
	}
	r.clickIn(panel, gfx.RoleButton, locales.LibraryAddTag())
	if !r.has(gfx.RoleGroup, locales.LibraryEditTags()) {
		t.Fatal("点「+」应打开标签选择浮层")
	}
	r.typeText("新标签")
	r.keys("enter")
	tags := r.view().Detail.Item.Tags
	if len(tags) != 1 || tags[0].Name != "新标签" {
		t.Fatalf("回车应把输入加成标签并写进草稿：%+v", tags)
	}
	if !r.view().Detail.Unsaved {
		t.Fatal("改标签是草稿改动：应有未保存标记")
	}
	r.keys("escape") // 收起浮层
	if r.has(gfx.RoleGroup, locales.LibraryEditTags()) {
		t.Fatal("Esc 应收起浮层")
	}
	if r.hasTextIn(panel, locales.LibraryAddTag()) {
		t.Fatal("有标签之后「+」钮是纯图标，不再带文字")
	}
	r.clickIn(panel, gfx.RoleButton, locales.LibraryRemoveTag("新标签"))
	if tags := r.view().Detail.Item.Tags; len(tags) != 0 {
		t.Fatalf("点标签上的 X 应移除它：%+v", tags)
	}
}

// 浮层开着时再点「+」是收起，不会收起再展开；选已有标签直接加上；重名（不分大小写）与空白不加。
func TestTagEditorPlusTogglesAndPicksExistingTags(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	panel := r.panelRect()
	r.clickIn(panel, gfx.RoleButton, locales.LibraryAddTag())
	r.clickIn(panel, gfx.RoleButton, locales.LibraryAddTag())
	if r.has(gfx.RoleGroup, locales.LibraryEditTags()) {
		t.Fatal("浮层开着时再点「+」应收起")
	}
	r.clickIn(panel, gfx.RoleButton, locales.LibraryAddTag())
	picker := r.mustNode(gfx.RoleGroup, locales.LibraryEditTags()).Bounds
	r.clickIn(picker, gfx.RoleButton, "go")
	if tags := r.view().Detail.Item.Tags; len(tags) != 1 || tags[0].Name != "go" {
		t.Fatalf("点已有标签「go」应加上它：%+v", tags)
	}
	// 点选标签后焦点跟着被点的小块走（它随即从可选列表里消失），要再输入得先点回输入框。
	r.click(gfx.RoleTextField, locales.LibraryTagsNewPlaceholder())
	r.typeText("GO")
	r.keys("enter")
	r.typeText("   ")
	r.keys("enter")
	if tags := r.view().Detail.Item.Tags; len(tags) != 1 {
		t.Fatalf("重名与空白不该再加：%+v", tags)
	}
}

// ---- 复制给 AI ----

// 主钮复制完整版，箭头菜单里三项：复制完整版、复制精简版、另存为；空条目两枚都禁用。
func TestHandoffButtonCopiesAndOffersTheOtherExports(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	r.click(gfx.RoleButton, locales.LibraryAiHandoffCopyFull())
	if want := "handoff:" + detailNoteID + ":full"; !slices.Equal(r.copied, []string{want}) {
		t.Fatalf("主钮应复制完整版 %q，实际 %q", want, r.copied)
	}
	r.click(gfx.RoleCombobox, locales.LibraryAiHandoffMore())
	for _, label := range []string{locales.LibraryAiHandoffCopyFull(), locales.LibraryAiHandoffCopyBrief(), locales.LibraryAiHandoffSave()} {
		if !r.has(gfx.RoleMenuItem, label) {
			t.Fatalf("箭头菜单缺「%s」", label)
		}
	}
	// 箭头钮离窗口右缘只有 1rem，菜单从钮的左缘往右展开放不下，被夹回窗口里（右缘贴窗口、不超出）。
	more := r.mustNode(gfx.RoleCombobox, locales.LibraryAiHandoffMore())
	first := r.mustNode(gfx.RoleMenuItem, locales.LibraryAiHandoffCopyFull())
	if first.Bounds.Y < more.Bounds.Y+more.Bounds.Height || first.Bounds.X+first.Bounds.Width > 1280 {
		t.Errorf("菜单应开在箭头钮之下、且不超出窗口：箭头 %+v 第一项 %+v", more.Bounds, first.Bounds)
	}
	r.click(gfx.RoleMenuItem, locales.LibraryAiHandoffCopyBrief())
	if want := "handoff:" + detailNoteID + ":brief"; len(r.copied) != 2 || r.copied[1] != want {
		t.Fatalf("精简版应写进剪贴板 %q，实际 %q", want, r.copied)
	}
	r.click(gfx.RoleCombobox, locales.LibraryAiHandoffMore())
	r.click(gfx.RoleMenuItem, locales.LibraryAiHandoffSave())
	if got := r.svc.Exports(); len(got) != 1 || got[0].Text != "handoff:"+detailNoteID+":full" {
		t.Fatalf("另存为应导出完整版：%+v", got)
	}

	e := shellRig(t)
	e.openTools(shellBlankID)
	for _, node := range []struct {
		role  gfx.Role
		label string
	}{{gfx.RoleButton, locales.LibraryAiHandoffCopyFull()}, {gfx.RoleCombobox, locales.LibraryAiHandoffMore()}} {
		if !e.mustNode(node.role, node.label).Disabled {
			t.Errorf("空条目的「%s」应禁用", node.label)
		}
	}
}

// 任务在途时两枚按钮禁用，不会重复开始。
func TestHandoffButtonsAreDisabledWhileATaskRuns(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	r.setDetail(func(d *DetailView) { d.Tools.HandoffBusy = true })
	for _, node := range []struct {
		role  gfx.Role
		label string
	}{{gfx.RoleButton, locales.LibraryAiHandoffCopyFull()}, {gfx.RoleCombobox, locales.LibraryAiHandoffMore()}} {
		if !r.mustNode(node.role, node.label).Disabled {
			t.Errorf("任务在途时「%s」应禁用", node.label)
		}
	}
}

// ---- AI 摘要入口 ----

// 非音视频、没有摘要的条目摆「生成 AI 摘要」，点了弹说明归属里程碑的失败提示；有摘要或是音视频时这里留白。
func TestSummaryEntryAppearsOnlyWhereTheCardWould(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	r.click(gfx.RoleButton, locales.LibraryAiSummaryGenerate())
	text := r.toastText()
	for _, want := range []string{locales.LibraryAiSummaryGenerate(), "M2-6c", "B-060"} {
		if !strings.Contains(text, want) {
			t.Fatalf("应弹失败提示并写明「%s」：%q", want, text)
		}
	}

	v := shellRig(t)
	v.openTools(detailVideoID)
	if v.has(gfx.RoleButton, locales.LibraryAiSummaryGenerate()) {
		t.Fatal("音视频条目没有 AI 摘要入口")
	}

	s := shellRig(t, func(r *rig) {
		r.svc.SeedDetail(detailNoteID, servicestest.DetailBody{Content: "正文", Summary: "已有摘要"})
	})
	s.openTools(detailNoteID)
	if s.has(gfx.RoleButton, locales.LibraryAiSummaryGenerate()) {
		t.Fatal("已有摘要时不画「生成 AI 摘要」（摘要卡片随 M2-6c 迁入）")
	}
}

// ---- 来源评论 ----

func douyinComments() []services.LibrarySourceComment {
	published := int64(1_700_000_000_000)
	return []services.LibrarySourceComment{
		{ID: "k1", ItemID: shellDouyinID, Platform: "douyin", ExternalID: "e1", AuthorName: "张三", Content: "第一行\n第二行", LikeCount: 9, PublishedAt: &published},
		{ID: "k2", ItemID: shellDouyinID, Platform: "douyin", ExternalID: "e2", AuthorName: "", Content: "匿名评论", LikeCount: 1},
	}
}

// 支持评论的条目：卡片没有评论又收着时不画，从头部菜单「采集评论」打开侧栏并展开卡片；采集后列表出现、按钮变「刷新」；
// 采集数量档位随下拉走；已有评论时菜单项是「查看来源评论」。
func TestCommentsCardCollectsAndLists(t *testing.T) {
	t.Parallel()
	r := shellRig(t, func(r *rig) { r.svc.SeedCapture(shellDouyinID, douyinComments()) })
	r.pick(shellDouyinID)
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.ArticleReaderTools())
	panel := r.panelRect()
	if r.hasTextIn(panel, locales.LibrarySourceComments()) {
		t.Fatal("没有评论、没展开时评论卡片不该出现")
	}
	r.click(gfx.RoleButton, locales.ArticleReaderCloseTools())
	if r.toolsShown() {
		t.Fatal("夹具失效：应已收起")
	}

	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.LibraryCollectSourceComments())
	panel = r.panelRect()
	if !r.hasTextIn(panel, locales.LibrarySourceComments()) || !r.hasTextIn(panel, locales.LibraryNoSourceComments()) {
		t.Fatalf("「采集评论」应打开侧栏并展开卡片，显示「还没有来源评论」；画面文字 %q", r.session.Texts())
	}
	r.click(gfx.RoleCombobox, locales.LibraryCommentCaptureLimit())
	r.click(gfx.RoleOption, "50")
	r.clickIn(panel, gfx.RoleButton, locales.LibraryCollectSourceComments())
	if got := r.svc.RefreshCalls(); len(got) != 1 || got[0].ItemID != shellDouyinID || got[0].Limit != 50 {
		t.Fatalf("应按所选档位 50 采集一次：%+v", got)
	}
	for _, text := range []string{
		"张三", locales.LibraryUnknownAuthor(), locales.LibraryCommentLikes(9), locales.LibraryCommentLikes(1),
		kit.FormatDate(1_700_000_000_000, r.screen.loc(), r.screen.Locale), "匿名评论", "2",
	} {
		if !r.hasTextIn(panel, text) {
			t.Errorf("评论卡片里应有「%s」；画面文字 %q", text, r.session.Texts())
		}
	}
	if _, ok := r.nodeIn(panel, gfx.RoleButton, locales.LibraryRefreshComments()); !ok {
		t.Fatal("有评论之后按钮应是「刷新」")
	}
	if _, ok := r.nodeIn(panel, gfx.RoleButton, locales.LibraryCollectSourceComments()); ok {
		t.Fatal("有评论之后不再是「采集评论」")
	}
	// 展开钮可以折叠；折叠起来头部还在（有评论就不隐藏）。
	r.clickIn(panel, gfx.RoleButton, locales.LibrarySourceComments()+" 2")
	if r.hasTextIn(panel, "匿名评论") || !r.hasTextIn(panel, locales.LibrarySourceComments()) {
		t.Fatal("折叠后只剩头部")
	}
	// 菜单项：已有评论写「查看来源评论」；侧栏开着时它先展开卡片、再照样开合侧栏——把侧栏收起（渲染层 onClick 的次序，
	// 只有键盘走得到：侧栏开着时「更多」钮被盖住）。
	items := r.moreItems(true)
	if len(items) < 2 || items[1].Label != locales.LibraryViewSourceComments() {
		t.Fatalf("已有评论时第二项应是「查看来源评论」：%+v", items)
	}
	items[1].OnSelect()
	r.frames(3)
	if r.toolsShown() {
		t.Fatal("侧栏开着时选「查看来源评论」是把侧栏收起")
	}
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	if r.has(gfx.RoleMenuItem, locales.LibraryCollectSourceComments()) || !r.has(gfx.RoleMenuItem, locales.LibraryViewSourceComments()) {
		t.Fatal("已有评论时菜单项应是「查看来源评论」")
	}
	r.click(gfx.RoleMenuItem, locales.LibraryViewSourceComments())
	if !r.toolsShown() || !r.hasTextIn(r.panelRect(), "匿名评论") {
		t.Fatal("再选一次打开侧栏，评论卡片展开着")
	}
}

// 采集失败：失败原因留在卡片里，按钮仍可重试；读取评论失败是错误态加重试。
func TestCommentsCardShowsFailuresAndRetries(t *testing.T) {
	t.Parallel()
	r := shellRig(t, func(r *rig) {
		r.svc.SeedSourceComments(shellDouyinID, douyinComments()[:1])
		r.svc.SeedCapture(shellDouyinID, douyinComments())
		r.svc.FailOn("RefreshSourceComments", errors.New("采集被拒绝"))
	})
	r.pick(shellDouyinID)
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.LibraryViewSourceComments())
	panel := r.panelRect()
	r.clickIn(panel, gfx.RoleButton, locales.LibraryRefreshComments())
	if !strings.Contains(r.screenText(), "采集被拒绝") {
		t.Fatalf("失败原因应留在卡片里；画面文字 %q", r.session.Texts())
	}
	r.svc.FailOn("RefreshSourceComments", nil)
	r.clickIn(panel, gfx.RoleButton, locales.LibraryRefreshComments())
	if strings.Contains(r.screenText(), "采集被拒绝") || !r.hasTextIn(panel, "匿名评论") {
		t.Fatalf("重试成功后失败原因应消失、列表更新；画面文字 %q", r.session.Texts())
	}

	f := shellRig(t, func(r *rig) { r.svc.FailOn("SourceComments", errors.New("读取评论失败了")) })
	f.pick(shellDouyinID)
	f.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	f.click(gfx.RoleMenuItem, locales.ArticleReaderTools())
	if !strings.Contains(f.screenText(), "读取评论失败了") || !f.has(gfx.RoleButton, locales.CommonRetry()) {
		t.Fatalf("读取失败应显示原因与「重试」：%q", f.session.Texts())
	}
	f.svc.FailOn("SourceComments", nil)
	f.click(gfx.RoleButton, locales.CommonRetry())
	if strings.Contains(f.screenText(), "读取评论失败了") {
		t.Fatal("重试成功后错误态应消失")
	}
}

// 没有来源的条目没有评论卡片；换条目再换回来，卡片的展开状态与档位回到默认（提供者随条目重新挂载），面板本身仍开着。
func TestCommentsCardResetsWhenTheItemChanges(t *testing.T) {
	t.Parallel()
	r := shellRig(t, func(r *rig) { r.svc.SeedSourceComments(shellDouyinID, douyinComments()) })
	r.openTools(detailNoteID)
	if r.hasTextIn(r.panelRect(), locales.LibrarySourceComments()) {
		t.Fatal("没有来源的笔记没有评论卡片")
	}
	r.pick(shellDouyinID)
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.LibraryViewSourceComments())
	panel := r.panelRect()
	if !r.hasTextIn(panel, "匿名评论") {
		t.Fatal("夹具失效：卡片应已展开")
	}
	r.click(gfx.RoleCombobox, locales.LibraryCommentCaptureLimit())
	r.click(gfx.RoleOption, "10")
	r.pick(detailNoteID)
	r.pick(shellDouyinID)
	panel = r.panelRect()
	if !r.hasTextIn(panel, locales.LibrarySourceComments()) || r.hasTextIn(panel, "匿名评论") {
		t.Fatalf("换回来之后卡片回到折叠（已有评论，头部仍在）；画面文字 %q", r.session.Texts())
	}
	if got := r.screen.detail.tools.comments.limitValue(); got != services.LibraryCommentLimitDefault {
		t.Fatalf("档位应回到默认 %d，实际 %d", services.LibraryCommentLimitDefault, got)
	}
}

// ---- 来源版本 ----

func reviewVersions() services.LibraryWebVersions {
	return services.LibraryWebVersions{
		Title: "待复核的网页", Content: "网页正文。", ContentHash: "h0", PendingVersion: true, SummaryStale: true,
		Versions: []services.LibraryWebVersion{
			{ID: "v-new", ItemID: detailReviewID, Title: "新版标题", Markdown: "新版正文", Complete: true, Kind: services.WebVersionRemote, EngineVersion: "engine-2", CapturedAt: 1_700_000_000_000},
			{ID: "v-bad", ItemID: detailReviewID, Title: "残缺版", Markdown: "残缺", Complete: false, Kind: services.WebVersionLocal, CapturedAt: 1_690_000_000_000},
		},
	}
}

// versionsChipName 是展开钮的可访问名：版本数、有新版本可比较、摘要需重新整理。
func versionsChipName() string {
	return locales.WebCaptureVersionsCount(2) + " · " + locales.WebCaptureVersionReady() + locales.WebCaptureSummaryStale()
}

// 网页条目才有「采集的来源版本」；展开钮的文字带版本数、待比较与摘要过期；展开后说明、下拉、所选版本的两栏与采用钮；
// 不完整的版本不能采用；采用把比较时的哈希与标题交给服务、重新读取条目，侧栏随之重新挂载（展开与所选回到初始）。
func TestVersionsSectionComparesAndAdopts(t *testing.T) {
	t.Parallel()
	r := shellRig(t, func(r *rig) { r.svc.SeedWebVersions(detailReviewID, reviewVersions()) })
	r.openTools(detailReviewID)
	panel := r.panelRect()
	if !r.hasTextIn(panel, locales.ArticleReaderSourceVersions()) {
		t.Fatalf("网页条目应有「采集的来源版本」；画面文字 %q", r.session.Texts())
	}
	if _, ok := r.nodeIn(panel, gfx.RoleButton, versionsChipName()); !ok {
		t.Fatalf("应有展开钮「%s」；画面文字 %q", versionsChipName(), r.session.Texts())
	}
	if r.hasTextIn(panel, locales.WebCaptureAdoptHint()) {
		t.Fatal("没展开时不显示说明")
	}
	before := rigCalls(r, "WebVersions:")
	r.clickIn(panel, gfx.RoleButton, versionsChipName())
	if rigCalls(r, "WebVersions:") != before+1 {
		t.Fatal("展开时重新读一次版本")
	}
	if !r.hasTextIn(panel, locales.WebCaptureAdoptHint()) || !r.has(gfx.RoleCombobox, locales.WebCaptureVersions()) {
		t.Fatalf("展开后应有说明与版本下拉；画面文字 %q", r.session.Texts())
	}
	if r.has(gfx.RoleButton, locales.WebCaptureAdoptSelected()) {
		t.Fatal("没选版本不显示采用钮")
	}

	badLabel := kit.FormatDateTime(1_690_000_000_000, r.screen.loc(), r.screen.Locale) + " · " + locales.WebCaptureLocalSnapshot() + locales.WebCaptureIncomplete()
	goodLabel := kit.FormatDateTime(1_700_000_000_000, r.screen.loc(), r.screen.Locale) + " · engine-2"
	r.click(gfx.RoleCombobox, locales.WebCaptureVersions())
	if !r.has(gfx.RoleOption, badLabel) || !r.has(gfx.RoleOption, goodLabel) {
		t.Fatalf("下拉选项应是「%s」「%s」；画面文字 %q", goodLabel, badLabel, r.session.Texts())
	}
	r.click(gfx.RoleOption, badLabel)
	if !r.mustNode(gfx.RoleButton, locales.WebCaptureAdoptSelected()).Disabled {
		t.Fatal("不完整的版本不能采用")
	}
	r.click(gfx.RoleCombobox, locales.WebCaptureVersions())
	r.click(gfx.RoleOption, goodLabel)
	// 右栏标题是两个相邻的文本节点（「所选版本 ·」与版本标题，kit.TextFlow 各画一个 Label）。
	for _, text := range []string{locales.WebCaptureCurrentContent(), locales.WebCaptureSelectedVersion(), "新版标题"} {
		if !r.hasTextIn(panel, text) {
			t.Errorf("两栏标题应有「%s」；画面文字 %q", text, r.session.Texts())
		}
	}
	if !strings.Contains(r.screenText(), "网页正文。") || !strings.Contains(r.screenText(), "新版正文") {
		t.Fatalf("两栏应各是当前正文与所选版本的正文：%q", r.session.Texts())
	}
	if r.mustNode(gfx.RoleButton, locales.WebCaptureAdoptSelected()).Disabled {
		t.Fatal("完整的版本可以采用")
	}
	r.click(gfx.RoleButton, locales.WebCaptureAdoptSelected())
	adopts := r.svc.Adopts()
	if len(adopts) != 1 || adopts[0].VersionID != "v-new" || adopts[0].ExpectedContentHash != "h0" || adopts[0].ExpectedTitle != "待复核的网页" {
		t.Fatalf("采用应带上比较时的哈希与标题：%+v", adopts)
	}
	if got := r.view().Detail.Item.Content; got != "新版正文" {
		t.Fatalf("采用后详情应重新读到新正文，实际 %q", got)
	}
	if r.hasText(locales.WebCaptureAdoptHint()) {
		t.Fatal("采用后侧栏重新挂载：版本面板应回到收起")
	}
}

// 读取版本失败：面板（即使没展开）显示错误态与重试；重试成功后消失。
func TestVersionsLoadFailureShowsAnErrorStateWithRetry(t *testing.T) {
	t.Parallel()
	r := shellRig(t, func(r *rig) {
		r.svc.SeedWebVersions(detailReviewID, reviewVersions())
		r.svc.FailOn("WebVersions", errors.New("读版本失败了"))
	})
	r.openTools(detailReviewID)
	if !strings.Contains(r.screenText(), "读版本失败了") || !r.has(gfx.RoleButton, locales.CommonRetry()) {
		t.Fatalf("读取失败应显示原因与「重试」：%q", r.session.Texts())
	}
	r.svc.FailOn("WebVersions", nil)
	r.click(gfx.RoleButton, locales.CommonRetry())
	if strings.Contains(r.screenText(), "读版本失败了") {
		t.Fatal("重试成功后错误态应消失")
	}
}

// 非网页条目没有来源版本一块。
func TestVersionsSectionIsForWebpagesOnly(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.openTools(detailNoteID)
	if r.hasTextIn(r.panelRect(), locales.ArticleReaderSourceVersions()) {
		t.Fatal("笔记没有「采集的来源版本」")
	}
}

// 展开钮是 inline-flex、max-w-full 的按钮：英文里整行（待比较与摘要过期两项状态都有时约 450px）比面板宽，外框就收在面板的内容区之内，
// 文字折成两行、每行各自居中（渲染层读数：第一行 290.8px、第二行 154.1px，中心线都在 1056.0）；中文放得下，仍是一行、不折。
// 修之前英文的外框 398px 超出面板、文字被裁掉半截。语言是进程内的全局状态，不并行。
func TestVersionsChipWrapsInsideThePanelInEnglishAndStaysOneLineInChinese(t *testing.T) {
	for _, c := range []struct {
		name  string
		lang  locales.Lang
		lines int
	}{{"英文", locales.En, 2}, {"中文", locales.Zh, 1}} {
		t.Run(c.name, func(t *testing.T) {
			langAtStart(t, c.lang)
			r := shellRig(t, func(r *rig) { r.svc.SeedWebVersions(detailReviewID, reviewVersions()) })
			r.openTools(detailReviewID)
			r.frames(4) // 文字盒的宽度要出一帧才量得到，再按它折行
			panel := r.panelRect()
			chip, ok := r.nodeIn(panel, gfx.RoleButton, versionsChipName())
			if !ok {
				t.Fatalf("应有展开钮「%s」；画面文字 %q", versionsChipName(), r.session.Texts())
			}
			const panelPad = 16 // 侧栏内边距 p-4
			if right, limit := chip.Bounds.X+chip.Bounds.Width, panel.X+panel.Width-panelPad; right > limit+0.5 {
				t.Errorf("展开钮右缘 %.2f 超出面板内容区右缘 %.2f（max-w-full）", right, limit)
			}
			if want := r.look.Rem(1.75); math.Abs(float64(chip.Bounds.Height-want)) > 0.01 {
				t.Errorf("展开钮高 %.2f，期望 h-7 = %.2f（折成两行也不撑高，文字上下溢出）", chip.Bounds.Height, want)
			}
			// 展开钮里的文字：中心落在外框（上下各放宽一行的一半，折行后两行都比 28px 的框高）里的 Label，按行归并。
			type span struct{ left, right float32 }
			rows := map[float32]*span{}
			var ys []float32
			for _, tx := range r.session.Snapshot().Texts {
				mid := gfx.Point{X: tx.Bounds.X + tx.Bounds.Width/2, Y: tx.Bounds.Y + tx.Bounds.Height/2}
				if mid.X < chip.Bounds.X || mid.X > chip.Bounds.X+chip.Bounds.Width || mid.Y < chip.Bounds.Y-8 || mid.Y > chip.Bounds.Y+chip.Bounds.Height+8 {
					continue
				}
				s, seen := rows[tx.Bounds.Y]
				if !seen {
					s = &span{left: tx.Bounds.X, right: tx.Bounds.X + tx.Bounds.Width}
					rows[tx.Bounds.Y] = s
					ys = append(ys, tx.Bounds.Y)
				}
				s.left, s.right = min(s.left, tx.Bounds.X), max(s.right, tx.Bounds.X+tx.Bounds.Width)
			}
			if len(ys) != c.lines {
				t.Fatalf("展开钮里的文字应是 %d 行，实际 %d 行（%v）；画面文字 %q", c.lines, len(ys), ys, r.session.Texts())
			}
			if c.lines == 1 {
				return
			}
			slices.Sort(ys)
			if gap, want := ys[1]-ys[0], r.look.Rem(kit.TextXS.Line); math.Abs(float64(gap-want)) > 0.01 {
				t.Errorf("两行相隔 %.2f，期望一个行高 %.2f", gap, want)
			}
			mid0, mid1 := (rows[ys[0]].left+rows[ys[0]].right)/2, (rows[ys[1]].left+rows[ys[1]].right)/2
			if math.Abs(float64(mid0-mid1)) > 0.05 {
				t.Errorf("两行的中心线应对齐：第一行 %.2f，第二行 %.2f（text-align: center）", mid0, mid1)
			}
			// 英文折行时，图标与文字三项按基准尺寸一起收窄（Shrink(1)）：若左右图标不收窄（Shrink(0)），文字盒会比现在窄约 7.5px。
			padX := r.look.Space(2.5) + 1
			gap := r.look.Space(1.5)
			iconFull := r.look.Rem(0.875)
			unshrunkLabelMax := chip.Bounds.Width - 2*padX - 2*gap - 2*iconFull
			if got := r.screen.detail.tools.versions.chipW; got <= unshrunkLabelMax+5 {
				t.Errorf("英文折行时左右两枚图标应一起收窄：文字盒宽 %.2f，未明显超过图标不收窄时的上限 %.2f", got, unshrunkLabelMax)
			}
		})
	}
}

// 窗口够宽（默认 1280 >= 1024）时两栏必须并排，窄窗口（800 < 1024）时必须上下叠放：
// 按真实视口宽度断言，不读内部 viewportW（防止每帧不记 viewportW 时测试自洽逃逸）。
func TestVersionPanesSitSideBySideOnlyOnAWideWindow(t *testing.T) {
	t.Parallel()
	r := shellRig(t, func(r *rig) { r.svc.SeedWebVersions(detailReviewID, reviewVersions()) })
	r.openTools(detailReviewID)
	r.clickIn(r.panelRect(), gfx.RoleButton, versionsChipName())
	r.click(gfx.RoleCombobox, locales.WebCaptureVersions())
	r.click(gfx.RoleOption, kit.FormatDateTime(1_700_000_000_000, r.screen.loc(), r.screen.Locale)+" · engine-2")
	readTitles := func() (cur, sel gfx.Rect) {
		for _, n := range r.session.Snapshot().Texts {
			switch n.Value {
			case locales.WebCaptureCurrentContent():
				cur = n.Bounds
			case locales.WebCaptureSelectedVersion(): // 右栏标题的第一段（后一段是版本标题，接在它后面）
				sel = n.Bounds
			}
		}
		if cur.Width == 0 || sel.Width == 0 {
			t.Fatalf("两栏标题应都画出来了：%+v %+v", cur, sel)
		}
		return cur, sel
	}
	cur, sel := readTitles()
	if cur.Y != sel.Y {
		t.Fatalf("默认宽窗口（>=1024）下两栏应并排，实际当前 %+v 所选 %+v", cur, sel)
	}
	r.look.Viewport = 800
	r.session.Resize(800, 600)
	r.frames(4)
	cur, sel = readTitles()
	if cur.Y == sel.Y {
		t.Fatalf("窄窗口（800<1024）下两栏应上下叠放，实际仍在同行：当前 %+v 所选 %+v", cur, sel)
	}
}

// 所选版本带网页快照且条目不在回收站时，显示「围绕此版本提问」钮，点击弹带 M3 与 B-060 的失败提示。
func TestVersionAskButtonShowsNotMigratedToast(t *testing.T) {
	t.Parallel()
	versions := reviewVersions()
	versions.Versions[0].Snapshot = []byte(`{"html":"<p>ok</p>"}`)
	r := shellRig(t, func(r *rig) { r.svc.SeedWebVersions(detailReviewID, versions) })
	r.openTools(detailReviewID)
	r.clickIn(r.panelRect(), gfx.RoleButton, versionsChipName())
	r.click(gfx.RoleCombobox, locales.WebCaptureVersions())
	r.click(gfx.RoleOption, kit.FormatDateTime(1_700_000_000_000, r.screen.loc(), r.screen.Locale)+" · engine-2")
	r.clickIn(r.panelRect(), gfx.RoleButton, locales.ArticleAskAskVersion())
	text := r.toastText()
	for _, want := range []string{locales.ArticleAskAskVersion(), "M3", "B-060"} {
		if !strings.Contains(text, want) {
			t.Fatalf("点击「围绕此版本提问」应弹失败提示并写明「%s」：%q", want, text)
		}
	}
}

// ---- 字号档 ----

// textNamed 是画面上值恰好等于 value 的第一段文字（要它的字号与行高，Snapshot().Texts 里才有）。
func textNamed(t *testing.T, r *rig, value string) gfx.Text {
	t.Helper()
	for _, tx := range r.session.Snapshot().Texts {
		if tx.Value == value {
			return tx
		}
	}
	t.Fatalf("画面上找不到文字「%s」；画面文字 %q", value, r.session.Texts())
	return gfx.Text{}
}

func near(got, want float32) bool { return math.Abs(float64(got-want)) <= 0.01 }

// 评论正文是 text-xs leading-relaxed（0.75rem、行高 1.625 倍）、作者与赞数是 text-[11px]：前者随字号档（根字号 14、16、18）缩放，
// 后者是 px 字面量、不缩放。修之前正文写死 12px 与 19.5px，T2 的小、大字号档上行距、字宽与墨迹位置都对不上渲染层，中档读不出来。
func TestCommentBodyScalesWithTheFontTierButTheMetaLineDoesNot(t *testing.T) {
	t.Parallel()
	for _, root := range []float32{14, 16, 18} {
		t.Run(fmt.Sprintf("根字号%.0f", root), func(t *testing.T) {
			t.Parallel()
			r := shellRig(t, func(r *rig) {
				r.look.RootPx = root
				r.svc.SeedCapture(shellDouyinID, []services.LibrarySourceComment{
					{ID: "k1", ItemID: shellDouyinID, Platform: "douyin", ExternalID: "e1", AuthorName: "张三", Content: "单行评论正文", LikeCount: 9},
				})
			})
			r.pick(shellDouyinID)
			r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
			r.click(gfx.RoleMenuItem, locales.LibraryCollectSourceComments())
			r.clickIn(r.panelRect(), gfx.RoleButton, locales.LibraryCollectSourceComments())

			body := textNamed(t, r, "单行评论正文")
			if want := r.look.Rem(0.75); !near(body.Style.Size, want) {
				t.Errorf("评论正文字号 %.2f，期望 0.75rem = %.2f", body.Style.Size, want)
			}
			if want := r.look.Rem(0.75) * 1.625; !near(body.Bounds.Height, want) {
				t.Errorf("评论正文行高 %.2f，期望 1.625 倍 = %.2f", body.Bounds.Height, want)
			}
			author := textNamed(t, r, "张三")
			if !near(author.Style.Size, 11) || !near(author.Bounds.Height, 16.5) {
				t.Errorf("作者行是 text-[11px]、行高 16.5，不随字号档变：字号 %.2f、行高 %.2f", author.Style.Size, author.Bounds.Height)
			}
		})
	}
}

// 版本窗格里的原文是 font-mono text-xs（0.75rem、行高 1rem）：随字号档缩放。修之前写死 12px 与 16px（同上）。
func TestVersionPaneTextScalesWithTheFontTier(t *testing.T) {
	t.Parallel()
	for _, root := range []float32{14, 16, 18} {
		t.Run(fmt.Sprintf("根字号%.0f", root), func(t *testing.T) {
			t.Parallel()
			r := shellRig(t, func(r *rig) {
				r.look.RootPx = root
				r.svc.SeedWebVersions(detailReviewID, reviewVersions())
			})
			r.openTools(detailReviewID)
			r.clickIn(r.panelRect(), gfx.RoleButton, versionsChipName())
			r.click(gfx.RoleCombobox, locales.WebCaptureVersions())
			r.click(gfx.RoleOption, kit.FormatDateTime(1_700_000_000_000, r.screen.loc(), r.screen.Locale)+" · engine-2")
			for _, line := range []string{"网页正文。", "新版正文"} {
				tx := textNamed(t, r, line)
				wantSize := r.look.Rem(0.75)
				if root == 18 {
					wantSize = 14 // 大档 13.5px 下新宋体位图吸附到 14px（台账 B-072）
				}
				if !near(tx.Style.Size, wantSize) {
					t.Errorf("「%s」字号 %.2f，期望 %.2f", line, tx.Style.Size, wantSize)
				}
				if want := r.look.Rem(1); !near(tx.Bounds.Height, want) {
					t.Errorf("「%s」行高 %.2f，期望 1rem = %.2f", line, tx.Bounds.Height, want)
				}
			}
		})
	}
}
