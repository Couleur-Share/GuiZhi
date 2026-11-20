package library

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 侧栏滚动区里的 Tab 停靠点（台账 B-030）：Viva 的可访问树里没有滚出视野的控件，Tab 走到它之前得先有人把它滚进来；
// 行的主按钮、「更多」按钮与范围页签都要登记成停靠点，kit 才知道下一个停靠点在视野之外、先滚再聚焦。
//
// 变异测试（2026-11-19）显示：行有「更多」按钮时，主按钮的登记是冗余的（同一行、同一纵向位置，滚「更多」进来主按钮也就进来了），
// 所以只有没有「更多」按钮的行——平台行、回收站——才能检验主按钮的登记；它们要成串出现、总高超过半个视野，
// 而且后面还得有足够长的内容（否则滚到最大偏移时它们恰好被一并带进视野），少登记才会让滚动一步跨过去
// （13 个平台行每行 32px、共 416px，比 800px 窗口里侧栏视野的一半还高；后面接 32 个标签行）。
// 范围页签紧贴在它下面的「保存当前筛选为智能视图」上方，滚那个按钮进来页签也就跟着进来，
// 只有「局部露出时滚到完全可见」这条规则才检验得出页签的登记（见末一个测试）。

// manyRowsRig 是侧栏要滚动的会话：40 个合集（每行 32px），全部 13 个平台各有一条（平台行没有「更多」按钮），再加 32 个标签。
// 返回合集与平台行的名称，按侧栏里的顺序。
func manyRowsRig(t *testing.T) (r *rig, collections, platforms []string) {
	t.Helper()
	r = newRig(t, func(r *rig) {
		counts, _, tags := libraryFixture()
		var cols []services.LibraryCollection
		for i := 1; i <= 40; i++ {
			name := fmt.Sprintf("合集%02d", i)
			collections = append(collections, name)
			cols = append(cols, services.LibraryCollection{ID: fmt.Sprintf("c%02d", i), Name: name})
		}
		counts.ByPlatform = map[string]int{}
		for _, p := range SourcePlatforms {
			platforms = append(platforms, kit.PlatformLabel(p))
			counts.ByPlatform[p] = 1
		}
		for i := 1; i <= 30; i++ { // 平台行之后接一长串标签：滚动停靠点之间隔得远，没登记的行才会被跨过去
			id := fmt.Sprintf("tx%02d", i)
			tags = append(tags, services.LibraryTag{ID: id, Name: fmt.Sprintf("标签%02d", i), ColorKey: "blue"})
			counts.ByTag[id] = 1
		}
		r.svc.Seed(counts, cols, tags)
	})
	return r, collections, platforms
}

// focusedLabel 是键盘焦点所在节点的名称与是否整个落在窗口里。
func (r *rig) focusedLabel() (label string, inView bool, ok bool) {
	for _, n := range r.session.Access() {
		if n.Focused {
			b := n.Bounds
			return n.Label, b.Y >= 0 && b.Y+b.Height <= 800, true
		}
	}
	return "", false, false
}

// 一路按 Tab：每个合集行、每个平台行的主按钮都轮得到，而且每次聚焦的控件都在视野里（滚动区跟着滚）。
func TestTabVisitsEveryRowOfAScrolledSidebar(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	r, collections, platforms := manyRowsRig(t)
	visited := map[string]bool{}
	last := platforms[len(platforms)-1]
	for i := 0; i < 400 && !visited[last]; i++ {
		r.keys("Tab")
		label, inView, ok := r.focusedLabel()
		if !ok {
			continue
		}
		if !inView {
			t.Fatalf("第 %d 次 Tab：聚焦的「%s」不在视野里，滚动区没有跟着滚", i+1, label)
		}
		visited[label] = true
	}
	for _, name := range append(append([]string{}, collections...), platforms...) {
		if !visited[name] {
			t.Errorf("按 Tab 一路走下去没有轮到「%s」（滚出视野的行被跳过了）", name)
		}
	}
}

// 走到底（回收站，侧栏的最后一个停靠点）之后一路按 Shift+Tab 回头：能回到最上面的范围页签（页签此时早已滚出视野），
// 聚焦时它在视野里。
func TestShiftTabReachesTheScopeTabsFromTheBottom(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	r, _, _ := manyRowsRig(t)
	const bottom = "回收站"
	for i := 0; i < 400; i++ {
		r.keys("Tab")
		if label, _, ok := r.focusedLabel(); ok && label == bottom {
			break
		}
	}
	if label, _, _ := r.focusedLabel(); label != bottom {
		t.Fatalf("没能用 Tab 走到最后一个停靠点「%s」，焦点在「%s」", bottom, label)
	}
	first := "未归合集 3" // 范围页签的第一枚：名称带计数
	found := false
	for i := 0; i < 400; i++ {
		r.keys("shift+Tab")
		label, inView, ok := r.focusedLabel()
		if ok && label == first {
			found = true
			if !inView {
				t.Errorf("回到页签「%s」时它不在视野里，滚动区没有跟着滚", first)
			}
			break
		}
	}
	if !found {
		t.Errorf("一路按 Shift+Tab 没有回到范围页签「%s」（滚出视野的页签被跳过了）", first)
	}
}

// 侧栏刚好往下滚了一点，最上面一排页签只露出一部分：键盘把焦点交给它时，滚动区要把它完全滚进视野
// （浏览器对聚焦元素的对齐是 CenterIfNeeded：露出一部分就近靠边）。页签没有登记成停靠点，这一步就不会发生。
func TestTabScrollsAPartlyHiddenScopeTabFullyIntoView(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	for _, dy := range []float32{20, 30} {
		t.Run(fmt.Sprintf("滚%.0f", dy), func(t *testing.T) {
			r, _, _ := manyRowsRig(t)
			first := "未归合集 3"
			full := r.mustNode(gfx.RoleButton, first).Bounds.Height
			if err := r.session.Scroll(gfx.ScrollEvent{X: 120, Y: 400, DeltaY: dy, Precise: true}); err != nil {
				t.Fatal(err)
			}
			if h := r.mustNode(gfx.RoleButton, first).Bounds.Height; h >= full {
				t.Fatalf("前提不成立：滚 %.0f 之后页签应只露出一部分（高 %.0f），实际高 %.0f", dy, full, h)
			}
			r.keys("Tab")
			label, _, ok := r.focusedLabel()
			if !ok || label != first {
				t.Fatalf("按 Tab 应落在第一枚页签「%s」，实际 %q（有焦点 %v）", first, label, ok)
			}
			if h := r.mustNode(gfx.RoleButton, first).Bounds.Height; h < full {
				t.Errorf("聚焦之后页签仍只露出 %.0f / %.0f：滚动区没有把它滚到完全可见", h, full)
			}
		})
	}
}

// ---- 卡片列表（台账 B-045）----

// focusedBounds 是键盘焦点所在节点的外框。
func (r *rig) focusedBounds() (gfx.Rect, bool) {
	for _, n := range r.session.Access() {
		if n.Focused {
			return n.Bounds, true
		}
	}
	return gfx.Rect{}, false
}

// 卡片列表一页 20 张（默认）：窗口多声明视口上下各 8 行（与渲染层虚拟列表的 overscan 同值），整页都有 Tab 停靠点。
// 点第一张卡片后连按 Tab，焦点依次走过本页第 2…20 张卡片、每张聚焦时都在视野里（滚动区跟着滚），随后是分页条的「每页」「下一页」
// ——渲染层读数 C1（scripts/parity/tools/probe-list-keyboard.mjs：行 1…19 的按钮，随后是分页条）。
func TestCardTabWalksEveryCardOfADefaultPageThenThePagination(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	entries := r.view().List.Entries
	if first, last := r.screen.pane.window.Declared(); first != 0 || last != len(entries) {
		t.Fatalf("前提不成立：默认一页 %d 张卡片应被窗口整页声明，实际 [%d, %d)", len(entries), first, last)
	}
	title := func(e services.LibraryEntry) string {
		if e.Title == "" {
			return locales.LibraryUntitled()
		}
		return e.Title
	}
	top, bottom := r.viewport()
	first, ok := r.cardOf(entries[0])
	if !ok {
		t.Fatal("找不到第一张卡片")
	}
	x, y := centerOf(first)
	r.clickAt(x, y)
	if label, _, ok := r.focusedLabel(); !ok || label != title(entries[0]) {
		t.Fatalf("点第一张卡片之后焦点应在它身上，实际 %q（有焦点 %v）", label, ok)
	}
	for i := 1; i < len(entries); i++ {
		r.keys("Tab")
		label, _, ok := r.focusedLabel()
		if !ok || label != title(entries[i]) {
			t.Fatalf("第 %d 次 Tab：焦点应在第 %d 张卡片「%s」，实际 %q（有焦点 %v）", i, i+1, title(entries[i]), label, ok)
		}
		b, _ := r.focusedBounds()
		if b.Y < top-1 || b.Y+b.Height > bottom+1 {
			t.Fatalf("第 %d 次 Tab 聚焦「%s」时它不在视野里（%.1f..%.1f，视口 %.1f..%.1f）：滚动区没有跟着滚",
				i, label, b.Y, b.Y+b.Height, top, bottom)
		}
	}
	for _, want := range []string{locales.LibraryPageSize(), locales.LibraryNextPage()} {
		r.keys("Tab")
		if label, _, ok := r.focusedLabel(); !ok || label != want {
			t.Fatalf("走完本页的卡片之后下一个停靠点应是分页条的「%s」，实际 %q（有焦点 %v）", want, label, ok)
		}
	}
}
