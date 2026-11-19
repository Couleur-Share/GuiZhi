package library

import (
	"fmt"
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

// 本文件测知识库列表的窗口级键盘（keyboard.go，台账 B-045）与它带出来的悬停刷新（B-037）：
// 谁在什么条件下触发了什么。渲染层的读数（探针 scripts/parity/tools/probe-list-keyboard.mjs）里的滚动数值，
// 在 kit/rowscroll_test.go 里逐值对过；这里测接线。数据用 servicestest.GenerateEntries(100)：「全部」范围下 80 条，每页 20 条。

// wsRig 是侧栏加工作区（卡片列表）的无头夹具，已读完第一页。
func wsRig(t *testing.T, mutate ...func(*rig)) *rig {
	t.Helper()
	return newRig(t, append([]func(*rig){func(r *rig) {
		r.workspace = true
		r.svc.SeedEntries(servicestest.GenerateEntries(100))
	}}, mutate...)...)
}

// selectedIndex 是选中条目在本页里的序号；没有选中返回 -1。
func (r *rig) selectedIndex() int {
	l := r.view().List
	return slices.IndexFunc(l.Entries, func(e services.LibraryEntry) bool { return e.ID == l.SelectedID })
}

// cardOf 找条目的卡片：可访问名就是标题（没有标题时是「无标题」）。窗口化之后不在画面附近的卡片没有声明，找不到。
func (r *rig) cardOf(e services.LibraryEntry) (app.AccessNode, bool) {
	title := e.Title
	if title == "" {
		title = locales.LibraryUntitled()
	}
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleButton && n.Label == title {
			return n, true
		}
	}
	return app.AccessNode{}, false
}

// viewport 是条目滚动区的上下沿（窗口坐标）：上沿是滚动到顶时第一张卡片的上沿减去 pt-1.5 的 6 px，
// 下沿是分页条的上沿（分页条是 py-2 加 1 px 上边线，「下一页」按钮顶在里面）。
func (r *rig) viewport() (top, bottom float32) {
	r.t.Helper()
	first, ok := r.cardOf(r.view().List.Entries[0])
	if !ok {
		r.t.Fatal("找不到第一张卡片（夹具失效，或者列表已经滚动）")
	}
	next := r.mustNode(gfx.RoleButton, locales.LibraryNextPage())
	return first.Bounds.Y - 6, next.Bounds.Y - 9
}

// press 按一个键（带修饰键的写法见 headless.Session.Keys）。
func (r *rig) press(spec string) {
	r.t.Helper()
	r.keys(spec)
}

// 方向键、Home、End 在卡片列表里移动选中项：没有选中时 ↓ 选第一条、↑ 选最后一条，到头不循环（渲染层 use-item-keyboard.ts 的 moveTo）。
func TestListKeysMoveTheSelectionWithoutWrapping(t *testing.T) {
	r := wsRig(t)
	n := len(r.view().List.Entries)
	if n != 20 {
		t.Fatalf("夹具应读到一页 20 条，实际 %d", n)
	}
	if got := r.selectedIndex(); got != -1 {
		t.Fatalf("一开始不该有选中项，选中序号 %d", got)
	}
	r.press("down")
	if got := r.selectedIndex(); got != 0 {
		t.Fatalf("没有选中时 ↓ 应选第一条，实际序号 %d", got)
	}
	r.press("down")
	r.press("down")
	if got := r.selectedIndex(); got != 2 {
		t.Fatalf("连按三次 ↓ 应选到第三条，实际序号 %d", got)
	}
	r.press("up")
	if got := r.selectedIndex(); got != 1 {
		t.Fatalf("↑ 应回到第二条，实际序号 %d", got)
	}
	r.press("end")
	if got := r.selectedIndex(); got != n-1 {
		t.Fatalf("End 应选最后一条，实际序号 %d", got)
	}
	r.press("down")
	if got := r.selectedIndex(); got != n-1 {
		t.Fatalf("在最后一条按 ↓ 不循环，实际序号 %d", got)
	}
	r.press("home")
	if got := r.selectedIndex(); got != 0 {
		t.Fatalf("Home 应选第一条，实际序号 %d", got)
	}
	r.press("up")
	if got := r.selectedIndex(); got != 0 {
		t.Fatalf("在第一条按 ↑ 不循环，实际序号 %d", got)
	}
}

// 没有选中时 ↑ 选最后一条（渲染层：currentIndex 为 -1 时 ↑ 落在末尾）。
func TestListUpWithNothingSelectedPicksTheLastEntry(t *testing.T) {
	r := wsRig(t)
	r.press("up")
	if got, want := r.selectedIndex(), len(r.view().List.Entries)-1; got != want {
		t.Fatalf("没有选中时 ↑ 应选最后一条（序号 %d），实际 %d", want, got)
	}
}

// 键盘只改选中项，不碰多选集合；多选里已有勾选时方向键也照常移动选中项（渲染层不看多选）。
func TestListKeysLeaveTheMultiSelectionAlone(t *testing.T) {
	r := wsRig(t)
	entries := r.view().List.Entries
	r.store.ToggleSelection(entries[3].ID)
	r.frames(2)
	r.press("down")
	r.press("down")
	if got := r.view().List.Selection; !slices.Equal(got, []string{entries[3].ID}) {
		t.Fatalf("方向键不该改多选集合，实际 %v", got)
	}
	if got := r.selectedIndex(); got != 1 {
		t.Fatalf("多选里有勾选时 ↓ 也移动选中项，实际序号 %d", got)
	}
}

// Ctrl、Alt、Meta 任一按住时不动；Shift 照常（Shift+↓ 与 ↓ 一样）。
func TestListKeysIgnoreChordsButNotShift(t *testing.T) {
	r := wsRig(t)
	r.press("down") // 选中第一条
	for _, chord := range []string{"ctrl+down", "alt+down", "cmd+down", "ctrl+end", "alt+home"} {
		r.press(chord)
		if got := r.selectedIndex(); got != 0 {
			t.Fatalf("%s 不该移动选中项，实际序号 %d", chord, got)
		}
	}
	r.press("shift+down")
	if got := r.selectedIndex(); got != 1 {
		t.Fatalf("Shift+↓ 应与 ↓ 一样，实际序号 %d", got)
	}
}

// 焦点在文本输入框里时不动：方向键、Home、End 归输入框（光标移动），空格是输入的字符。
func TestListKeysLeaveTextFieldsAlone(t *testing.T) {
	r := wsRig(t)
	r.click(gfx.RoleButton, locales.LibraryNewCollection())
	field := r.mustNode(gfx.RoleTextField, locales.LibraryNamePlaceholder())
	if !field.Focused {
		t.Fatal("新建合集弹窗打开后名称框应有焦点（夹具失效）")
	}
	if !kit.FocusOf(&r.canvas).Editable() {
		t.Fatal("名称框应算可编辑焦点")
	}
	for _, spec := range []string{"down", "up", "end", "home"} {
		r.press(spec)
		if got := r.selectedIndex(); got != -1 {
			t.Fatalf("焦点在输入框里时 %s 不该移动选中项，实际序号 %d", spec, got)
		}
	}
	// 钩子自己的判断（不经 Viva 的按键分发：编辑键多半先被文字系统接走，到不了钩子，但到了也不该动）。
	for _, key := range []gfx.Key{gfx.KeyDown, gfx.KeyUp, gfx.KeyHome, gfx.KeyEnd} {
		res := r.screen.ListKey(gfx.KeyEvent{Key: key}, &r.canvas)
		if res.Handled || r.selectedIndex() != -1 {
			t.Fatalf("焦点在输入框里时钩子不该处理 %v：%+v，选中序号 %d", key, res, r.selectedIndex())
		}
	}
	r.store.SelectItem(r.view().List.Entries[2].ID) // 有选中项，空格才有东西可勾选
	res := r.screen.ListKey(gfx.KeyEvent{Key: gfx.KeySpace}, &r.canvas)
	if res.Handled || len(r.view().List.Selection) != 0 {
		t.Fatalf("焦点在输入框里时空格不该被列表拦截或勾选：%+v，多选 %v", res, r.view().List.Selection)
	}
}

// 空格切换选中条目的多选勾选；没有选中项时空格不动。
func TestListSpaceTogglesTheSelectedEntry(t *testing.T) {
	r := wsRig(t)
	r.press("space")
	if len(r.view().List.Selection) != 0 {
		t.Fatalf("没有选中项时空格不该勾选任何东西：%v", r.view().List.Selection)
	}
	r.press("down")
	id := r.view().List.SelectedID
	r.press("space")
	if got := r.view().List.Selection; !slices.Equal(got, []string{id}) {
		t.Fatalf("空格应勾选选中的条目 %s，实际 %v", id, got)
	}
	r.press("space")
	if got := r.view().List.Selection; len(got) != 0 {
		t.Fatalf("再按一次空格应取消勾选，实际 %v", got)
	}
}

// 焦点在按钮（卡片本身就是按钮）上时空格让给它：浏览器里那是一次点击，不是多选勾选。
func TestListSpaceYieldsToAFocusedButton(t *testing.T) {
	r := wsRig(t)
	entries := r.view().List.Entries
	card, ok := r.cardOf(entries[1])
	if !ok {
		t.Fatal("找不到第二张卡片")
	}
	x, y := centerOf(card)
	r.clickAt(x, y)
	if got := r.selectedIndex(); got != 1 {
		t.Fatalf("点击应选中第二条，实际序号 %d", got)
	}
	if !kit.FocusOf(&r.canvas).Activatable() {
		t.Fatal("点击卡片之后焦点应在按钮上（夹具失效）")
	}
	r.press("space")
	if got := r.view().List.Selection; len(got) != 0 {
		t.Fatalf("焦点在按钮上时空格不该勾选，实际 %v", got)
	}
}

// 本页没有条目时不动，也不拦截按键。
func TestListKeysDoNothingWithoutEntries(t *testing.T) {
	r := wsRig(t, func(r *rig) { r.svc.SeedEntries(nil) })
	if len(r.view().List.Entries) != 0 {
		t.Fatal("夹具应没有条目")
	}
	for _, key := range []gfx.Key{gfx.KeyDown, gfx.KeyUp, gfx.KeyHome, gfx.KeyEnd, gfx.KeySpace} {
		if res := r.screen.ListKey(gfx.KeyEvent{Key: key}, &r.canvas); res.Handled || res.Repaint {
			t.Fatalf("没有条目时 %v 不该被拦截：%+v", key, res)
		}
	}
	if r.view().List.SelectedID != "" {
		t.Fatal("没有条目时不该有选中项")
	}
}

// 与列表无关的键、按键松开、工作区没挂载时一律放行；这一帧没画工作区（切到别的模块）时钩子整个不动。
func TestListKeysPassOtherKeysAndInactiveWorkspaces(t *testing.T) {
	r := wsRig(t)
	for _, key := range []gfx.Key{gfx.KeyEnter, gfx.KeyDelete, gfx.KeyTab, gfx.KeyLeft, gfx.KeyRight, gfx.KeyPageDown, gfx.KeyEscape} {
		if res := r.screen.ListKey(gfx.KeyEvent{Key: key}, &r.canvas); res.Handled {
			t.Fatalf("%v 不属于列表键盘（Enter 随详情浮层、Delete 随批量与删除迁入），不该被拦截", key)
		}
	}
	if res := r.screen.ListKey(gfx.KeyEvent{Key: gfx.KeyDown, Release: true}, &r.canvas); res.Handled || r.selectedIndex() != -1 {
		t.Fatalf("按键松开不该处理：%+v，选中序号 %d", res, r.selectedIndex())
	}
	if res := r.screen.ListKey(gfx.KeyEvent{Key: gfx.KeyDown}, &r.canvas); !res.Handled || r.selectedIndex() != 0 {
		t.Fatalf("↓ 应被拦截并选中第一条（对照用例）：%+v，选中序号 %d", res, r.selectedIndex())
	}
	r.wsHidden = true
	r.frames(2)
	r.press("down")
	if got := r.selectedIndex(); got != 0 {
		t.Fatalf("工作区没挂载时钩子不该动，实际序号 %d", got)
	}
	if res := r.screen.ListKey(gfx.KeyEvent{Key: gfx.KeyDown}, &r.canvas); res.Handled {
		t.Fatalf("工作区没挂载时不该拦截：%+v", res)
	}
	r.wsHidden = false
	r.frames(2)
	r.press("down")
	if got := r.selectedIndex(); got != 1 {
		t.Fatalf("工作区回来之后应恢复，实际序号 %d", got)
	}
}

// 浮层里的按键：渲染层的菜单与下拉不 stopPropagation，列表的窗口监听照样收到，所以排序菜单打开时按 ↓ 选中行也往下走（探针 E1）；
// 但钩子不拦截——浮层自己的按键处理（菜单里的焦点移动）照常运行。
func TestListKeysInsideAFloatingLayerMoveTheSelectionButAreNotConsumed(t *testing.T) {
	r := wsRig(t)
	r.press("down")
	sort := firstNode(r, gfx.RoleCombobox, locales.LibrarySortBy())
	x, y := centerOf(sort)
	r.clickAt(x, y)
	focus := kit.FocusOf(&r.canvas)
	if !focus.Held || !focus.Floating() {
		t.Fatalf("排序菜单打开后焦点应在浮层里：%+v", focus)
	}
	before := focusedLabel(r)
	res := r.screen.ListKey(gfx.KeyEvent{Key: gfx.KeyDown}, &r.canvas)
	if res.Handled {
		t.Fatal("焦点在浮层里时钩子不该拦截按键")
	}
	if got := r.selectedIndex(); got != 1 {
		t.Fatalf("浮层里按 ↓ 选中行也该往下走，实际序号 %d", got)
	}
	// 经真实按键路径：菜单自己移动焦点，选中行再走一格。
	r.press("down")
	if after := focusedLabel(r); after == before {
		t.Fatalf("菜单应自己处理 ↓（焦点从「%s」移走），但焦点没动", before)
	}
	if got := r.selectedIndex(); got != 2 {
		t.Fatalf("真实按键之后选中序号应是 2，实际 %d", got)
	}
}

func firstNode(r *rig, role gfx.Role, prefix string) app.AccessNode {
	r.t.Helper()
	for _, n := range r.session.Access() {
		if n.Role == role && strings.HasPrefix(n.Label, prefix) {
			return n
		}
	}
	r.t.Fatalf("找不到以「%s」开头的 %v 节点", prefix, role)
	return app.AccessNode{}
}

func focusedLabel(r *rig) string {
	for _, n := range r.session.Access() {
		if n.Focused {
			return n.Label
		}
	}
	return ""
}

// 选中行被键盘移出视野时滚进视野（渲染层 ItemList 的 effect 里的 scrollToIndex，align auto）：
// 一路 ↓ 下去，选中的卡片始终完整落在滚动区里；End 滚到底，Home 滚回顶。
func TestListSelectionIsScrolledIntoView(t *testing.T) {
	r := wsRig(t)
	top, bottom := r.viewport()
	entries := r.view().List.Entries
	visible := func(step string) {
		t.Helper()
		sel := entries[r.selectedIndex()]
		card, ok := r.cardOf(sel)
		if !ok {
			t.Fatalf("%s：选中的卡片「%s」没有声明（窗口化之后不在滚动区附近）", step, sel.Title)
		}
		if card.Bounds.Y < top-1 || card.Bounds.Y+card.Bounds.Height > bottom+1 {
			t.Fatalf("%s：选中的卡片纵向 [%.1f, %.1f] 不在滚动区 [%.1f, %.1f] 里",
				step, card.Bounds.Y, card.Bounds.Y+card.Bounds.Height, top, bottom)
		}
	}
	scrolled := false
	for i := 0; i < len(entries); i++ {
		r.press("down")
		visible(fmt.Sprintf("向下第 %d 步", i+1))
		if first, ok := r.cardOf(entries[0]); !ok || first.Bounds.Y < top+6-1 {
			scrolled = true
		}
	}
	if !scrolled {
		t.Fatal("一路 ↓ 到最后一条，列表却没有滚动（夹具失效：20 条应超出视口）")
	}
	r.press("home")
	visible("Home")
	if first, ok := r.cardOf(entries[0]); !ok || first.Bounds.Y < top+6-1 || first.Bounds.Y > top+6+1 {
		t.Fatalf("Home 之后应滚回顶部（第一张卡片在 %.1f，应在 %.1f）", first.Bounds.Y, top+6)
	}
	r.press("end")
	visible("End")
	last, _ := r.cardOf(entries[len(entries)-1])
	// 最后一条：滚到底，卡片下沿离滚动区下沿一个 pb-2（8 px）。
	if got, want := last.Bounds.Y+last.Bounds.Height, bottom-8; got < want-1.5 || got > want+1.5 {
		t.Fatalf("End 之后最后一张卡片的下沿应在 %.1f（滚到底，留 pb-2），实际 %.1f", want, got)
	}
}

// 选中行已经完整在视野里：不滚动（auto 对齐：不动）。
func TestListSelectionInViewDoesNotScroll(t *testing.T) {
	r := wsRig(t)
	entries := r.view().List.Entries
	first, _ := r.cardOf(entries[0])
	y0 := first.Bounds.Y
	for i := 0; i < 3; i++ {
		r.press("down")
	}
	if now, _ := r.cardOf(entries[0]); now.Bounds.Y != y0 {
		t.Fatalf("前三张卡片本来就完整在视野里，不该滚动：第一张从 %.1f 变成 %.1f", y0, now.Bounds.Y)
	}
}

// 点击选中的行已经在视野里（或只露出一部分）：渲染层同一个 effect 也会把它滚进视野；点击完整可见的行不滚动。
func TestListClickingAVisibleCardDoesNotScroll(t *testing.T) {
	r := wsRig(t)
	entries := r.view().List.Entries
	first, _ := r.cardOf(entries[0])
	y0 := first.Bounds.Y
	card, _ := r.cardOf(entries[2])
	x, y := centerOf(card)
	r.clickAt(x, y)
	if now, _ := r.cardOf(entries[0]); now.Bounds.Y != y0 {
		t.Fatalf("点击完整可见的卡片不该滚动：第一张从 %.1f 变成 %.1f", y0, now.Bounds.Y)
	}
}

// 条目数组换了而选中项没变（排序换了、重新读取）：选中行在新数组里的位置变了，照样滚进视野——
// 渲染层那个 effect 的依赖是 selectedId 与 entries 两个，不是只看选中项。
func TestListRevealFollowsTheSelectionWhenTheEntriesAreReordered(t *testing.T) {
	r := wsRig(t)
	top, bottom := r.viewport()            // 先量：一页放下全部条目之后只有一页，分页条里没有翻页按钮了（高度不变）
	r.store.SetPageSize(r.screen.Ctx, 100) // 排序只改变选中项的位置
	r.frames(3)
	for i := 0; i < 4; i++ {
		r.press("down")
	}
	sel := r.view().List.SelectedID
	r.store.SetSort(r.screen.Ctx, services.LibrarySortUpdatedAt, services.LibraryOrderAsc)
	r.frames(4)
	l := r.view().List
	if l.SelectedID != sel {
		t.Fatalf("排序不该改变选中项：%q → %q", sel, l.SelectedID)
	}
	idx := r.selectedIndex()
	if idx < 30 {
		t.Fatalf("夹具失效：反序之后选中项应远离顶部，实际序号 %d", idx)
	}
	card, ok := r.cardOf(l.Entries[idx])
	if !ok {
		t.Fatal("排序之后选中的卡片应已滚进画面")
	}
	if card.Bounds.Y < top-1 || card.Bounds.Y+card.Bounds.Height > bottom+1 {
		t.Fatalf("选中的卡片纵向 [%.1f, %.1f] 不在滚动区 [%.1f, %.1f] 里", card.Bounds.Y, card.Bounds.Y+card.Bounds.Height, top, bottom)
	}
}

// 切到别的模块再回来：渲染层的列表整个卸载重挂，挂载时那个 effect 把选中行重新滚进视野；
// 这里卸载时重置 reveal 的记忆，重挂之后同样把选中行滚回画面里。
func TestListRevealsTheSelectionAgainWhenTheWorkspaceIsMountedAgain(t *testing.T) {
	r := wsRig(t)
	top, bottom := r.viewport()
	r.press("end") // 选中本页最后一条，列表滚到底
	l := r.view().List
	if r.selectedIndex() != len(l.Entries)-1 {
		t.Fatalf("夹具失效：End 之后应选中最后一条，实际序号 %d", r.selectedIndex())
	}
	last := l.Entries[len(l.Entries)-1]
	if _, ok := r.cardOf(l.Entries[0]); ok {
		t.Fatal("夹具失效：滚到底之后第一张卡片应当已经在窗口之外")
	}
	r.wsHidden = true
	r.frames(3)
	r.wsHidden = false
	r.frames(4)
	card, ok := r.cardOf(last)
	if !ok {
		t.Fatal("切回来之后选中的卡片应已滚进画面")
	}
	if card.Bounds.Y < top-1 || card.Bounds.Y+card.Bounds.Height > bottom+1 {
		t.Fatalf("选中的卡片纵向 [%.1f, %.1f] 不在滚动区 [%.1f, %.1f] 里", card.Bounds.Y, card.Bounds.Y+card.Bounds.Height, top, bottom)
	}
}

// 表格视图共用同一个窗口级键盘：方向键、End 移动选中项，选中行越过视口时由表格自己的「选中行滚进视野」接手（tablereveal.go）。
func TestListKeysAlsoDriveTheTableView(t *testing.T) {
	r := wsRig(t)
	r.click(gfx.RoleButton, locales.LibraryViewList())
	r.frames(4)
	entries := r.view().List.Entries
	last := entries[len(entries)-1]
	if _, ok := r.cardOf(last); ok {
		t.Fatal("夹具失效：表格第一屏里不该有本页最后一行（20 行放不进一屏）")
	}
	r.press("down")
	r.press("down")
	if got := r.selectedIndex(); got != 1 {
		t.Fatalf("表格视图里两次 ↓ 应选中第二行，实际序号 %d", got)
	}
	r.press("end")
	r.frames(4)
	if got := r.selectedIndex(); got != len(entries)-1 {
		t.Fatalf("表格视图里 End 应选中最后一行，实际序号 %d", got)
	}
	if _, ok := r.cardOf(last); !ok {
		t.Fatal("End 之后选中的最后一行应已滚进视口")
	}
	if _, ok := r.cardOf(entries[0]); ok {
		t.Fatal("滚到底之后第一行应当已经在视口之外")
	}
}

// 悬停刷新（B-037）：键盘把选中行滚动了，请求在指针此刻的位置补一次指针移动；没有滚动的移动不请求。
func TestListScrollingRequestsAHoverRefresh(t *testing.T) {
	r := wsRig(t)
	posts := 0
	r.look.HoverRefresh = func() { posts++ }
	for i := 0; i < 3; i++ {
		r.press("down")
	}
	if posts != 0 {
		t.Fatalf("没有滚动时不该请求悬停刷新，实际 %d 次", posts)
	}
	r.press("end")
	if posts == 0 {
		t.Fatal("End 把列表滚到了底，应当请求悬停刷新")
	}
	posts = 0
	r.press("home")
	if posts == 0 {
		t.Fatal("Home 把列表滚回了顶，应当请求悬停刷新")
	}
}

// 列表内容整体换了（翻页）：指针底下多半是另一行，请求补一次悬停刷新；内容没变的帧不请求。
func TestListReplacingTheEntriesRequestsAHoverRefresh(t *testing.T) {
	r := wsRig(t)
	posts := 0
	r.look.HoverRefresh = func() { posts++ }
	r.frames(3)
	if posts != 0 {
		t.Fatalf("内容没变的帧不该请求悬停刷新，实际 %d 次", posts)
	}
	r.store.SetPage(r.screen.Ctx, 2)
	r.frames(3)
	if posts == 0 {
		t.Fatal("翻页换了整页内容，应当请求悬停刷新")
	}
}

// 翻页换掉整页内容：选中项（上一页的）不在新的一页里，选中行滚进视野的 effect 找不到序号就什么也不做；
// 选中项保留（渲染层 setPage 不清选中）、滚动偏移保留（ItemList 没有翻页回顶的逻辑）。之后按 ↓ 从「本页没有选中」起步，
// 选中新页第一条并把它滚进视野。
func TestListKeysAfterPagingStartFromTheNewPage(t *testing.T) {
	r := wsRig(t)
	top, bottom := r.viewport()
	r.press("end")
	old := r.view().List.SelectedID
	r.store.SetPage(r.screen.Ctx, 2)
	r.frames(3)
	l := r.view().List
	if l.Page != 2 || l.SelectedID != old {
		t.Fatalf("翻页应保留选中项：页 %d，选中 %q（原来 %q）", l.Page, l.SelectedID, old)
	}
	if got := r.selectedIndex(); got != -1 {
		t.Fatalf("上一页的选中项不在新页里，序号应为 -1，实际 %d", got)
	}
	r.press("down")
	if got := r.selectedIndex(); got != 0 {
		t.Fatalf("本页没有选中项时 ↓ 应选新页第一条，实际序号 %d", got)
	}
	card, ok := r.cardOf(r.view().List.Entries[0])
	if !ok {
		t.Fatal("选中的新页第一张卡片应已滚进画面")
	}
	if card.Bounds.Y < top-1 || card.Bounds.Y+card.Bounds.Height > bottom+1 {
		t.Fatalf("选中的卡片纵向 [%.1f, %.1f] 不在滚动区 [%.1f, %.1f] 里", card.Bounds.Y, card.Bounds.Y+card.Bounds.Height, top, bottom)
	}
}
