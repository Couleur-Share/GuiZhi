package inbox

import (
	"sort"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件验证卡片列表的行窗口化（rows.go 的 itemList，kit.RowWindow 的 MeasureAll 模式）：
// 三百多行时每帧只声明视口附近的少数几张卡片，滚到底能看见最后一行，滚动途中画面上没有空洞，换筛选后行高缓存随行键作废。

// windowMix 是窗口化用例的夹具：200 条复核（原因 0～3 条不等，行高不一）、50 条未归合集、70 条采集问题，加两个聚合行，「全部」下可见 322 行。
var windowMix = budgetMix{reviews: 200, unclassified: 50, imports: 70}

// lastCardText 是列表最后一行在画面上会出现的标题文字。
func lastCardText(item services.InboxItem) string {
	switch {
	case item.Kind == services.InboxKindSemanticPending:
		return locales.InboxSemanticAggregateTitle()
	case item.IsAggregate():
		return locales.InboxWikiAggregateTitle()
	}
	return item.Title
}

// listTextGap 是列表区里相邻两条文字之间最大的纵向空隙：窗口声明漏了行时，这里会出现一整张卡片以上的空洞。
// 只看工作区里（右侧）、页头以下的文字。
func (r *uiRig) listTextGap() (gap float32, texts int) {
	r.t.Helper()
	type span struct{ top, bottom float32 }
	var spans []span
	for _, got := range r.session.Snapshot().Texts {
		b := got.Bounds
		if b.X < 300 || b.Y < 170 || b.Y+b.Height > 800 {
			continue
		}
		spans = append(spans, span{b.Y, b.Y + b.Height})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].top < spans[j].top })
	for i := 1; i < len(spans); i++ {
		gap = max(gap, spans[i].top-spans[i-1].bottom)
	}
	return gap, len(spans)
}

func TestItemListWindowDeclaresOnlyRowsNearViewport(t *testing.T) {
	r := newUIRig(t, budgetItems(windowMix), func(r *uiRig) {
		r.svc.SetWikiModelReady(true)
		r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 3, Review: 1}) // 提示条在列表之上：窗口要扣掉它的高度
		r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 480, IndexedItems: 455})
	})
	r.frames(6) // 首帧整表声明量行高，之后进入窗口

	v := r.store.View()
	n := len(v.Visible)
	if want := windowMix.reviews + windowMix.imports + 2; n != want {
		t.Fatalf("夹具应有 %d 行可见，实际 %d", want, n)
	}
	first, last := r.screen.window.Declared()
	if declared := last - first; declared < 1 || declared > 30 {
		t.Fatalf("%d 行里只该声明视口附近的少数几行（1～30），实际声明 [%d, %d) 共 %d 行", n, first, last, declared)
	}
	if first != 0 {
		t.Fatalf("滚动偏移为 0 时窗口应从第 0 行起，实际 first=%d", first)
	}
	if r.screen.window.Measured() != n {
		t.Fatalf("整表声明之后每一行都应有实测高度：%d / %d", r.screen.window.Measured(), n)
	}
	if r.screen.leadH <= 0 {
		t.Fatal("Wiki 提示条在列表之上，leadH 应大于 0")
	}
	if got := r.count(gfx.RoleButton, locales.LibraryMarkReviewed()); got > last-first {
		t.Fatalf("画面上的「标记已复核」钮（%d）不可能比声明的行数（%d）多", got, last-first)
	}

	// 一路滚到底：每一步的窗口都很小，画面里没有空洞；最后声明到末行，末行的标题在画面上。
	const wx, wy = float32(760), float32(500)
	maxDeclared, atEnd := 0, false
	for step := 0; step < 600 && !atEnd; step++ {
		if err := r.session.Scroll(gfx.ScrollEvent{X: wx, Y: wy, DeltaY: 240, Precise: true}); err != nil {
			t.Fatalf("滚轮失败：%v", err)
		}
		r.frames(2)
		f, l := r.screen.window.Declared()
		maxDeclared = max(maxDeclared, l-f)
		if gap, texts := r.listTextGap(); texts < 3 || gap > 120 {
			t.Fatalf("第 %d 步（声明 [%d, %d)）列表区出现空洞：相邻文字最大间隔 %.0f px，文字 %d 条", step, f, l, gap, texts)
		}
		atEnd = l == n
	}
	if !atEnd {
		first, last = r.screen.window.Declared()
		t.Fatalf("滚了 600 步仍没声明到末行：声明 [%d, %d) / %d", first, last, n)
	}
	r.frames(4) // 末端的偏移收敛
	if maxDeclared > 30 {
		t.Fatalf("滚动途中最多声明了 %d 行，窗口没有起作用", maxDeclared)
	}
	if f, _ := r.screen.window.Declared(); f == 0 {
		t.Fatal("滚到底之后窗口不该还包含第 0 行")
	}
	if want := lastCardText(v.Visible[n-1]); !r.hasText(want) {
		t.Fatalf("滚到底应能看见末行（%q）；画面上有 %q", want, r.session.Texts())
	}
	if r.hasText(lastCardText(v.Visible[0])) {
		t.Fatal("滚到底之后第一行不该还在画面上")
	}
}

// 换筛选：行键整体换了，旧的实测作废；换完回到列表顶，新的第一行在画面上，窗口声明的是新列表的头几行。
func TestItemListWindowResetsOnFilterChange(t *testing.T) {
	r := newUIRig(t, budgetItems(windowMix))
	r.frames(6)
	for i := 0; i < 40; i++ { // 先滚到深处，让偏移不为 0
		if err := r.session.Scroll(gfx.ScrollEvent{X: 760, Y: 500, DeltaY: 600, Precise: true}); err != nil {
			t.Fatalf("滚轮失败：%v", err)
		}
		r.frames(2)
	}
	r.store.SetFilter(FilterUnclassified)
	r.frames(6)

	v := r.store.View()
	if len(v.Visible) != windowMix.unclassified {
		t.Fatalf("筛选后应剩 %d 行，实际 %d", windowMix.unclassified, len(v.Visible))
	}
	first, last := r.screen.window.Declared()
	if last <= first || last > len(v.Visible) {
		t.Fatalf("换筛选后声明区间 [%d, %d) 不合法（共 %d 行）", first, last, len(v.Visible))
	}
	if r.screen.window.Measured() != len(v.Visible) {
		t.Fatalf("换筛选后应重新量完每一行：%d / %d", r.screen.window.Measured(), len(v.Visible))
	}
	if gap, texts := r.listTextGap(); texts < 3 || gap > 120 {
		t.Fatalf("换筛选后列表区出现空洞：相邻文字最大间隔 %.0f px，文字 %d 条", gap, texts)
	}
	// 滚动区不随筛选重建：偏移留在原处，由布局夹取到新内容的末端（与浏览器一致，kit.RowsSpec.MeasureAll 的说明），所以画面上是新列表的末行。
	if want := v.Visible[len(v.Visible)-1].Title; last != len(v.Visible) || !r.hasText(want) {
		t.Fatalf("偏移夹取到新列表末端后应声明到末行并画出它（%q）：声明 [%d, %d)；画面上有 %q", want, first, last, r.session.Texts())
	}
	if err := r.session.Scroll(gfx.ScrollEvent{X: 760, Y: 500, DeltaY: -100000, Precise: true}); err != nil {
		t.Fatalf("滚轮失败：%v", err)
	}
	r.frames(4)
	if want := v.Visible[0].Title; !r.hasText(want) {
		t.Fatalf("滚回顶端应画出新列表的第一行（%q）；画面上有 %q", want, r.session.Texts())
	}
}
