package library

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 表格行列的窗口化（台账 B-047）：渲染层的表格没有窗口化，Viva 一页 100 行时整页声明每帧要 7 ms；
// 这里每帧只声明视口附近的行，而画面必须与整页声明时逐像素相同。
//
// 做法是参照对拍：同一份数据开两个会话，一个是生产的窗口，一个把窗口放宽到整页（声明的行与窗口化之前完全一样，
// tableState.overscan），每做一步操作就比较两边栅格化出来的整幅画面。夹具是 400 条库（「全部」范围 320 条），每页 100 行。
//
// 这种参照与窗口共用同一套行高记录（行的键、tableEpoch），所以证明不了「该重量的时候重量了」：
// 那几项（翻页、列宽、条目就地改了标题）的参照是一个直接以最终状态打开的新会话，没有任何历史可以带着旧的行高。

// tableOpts 是 openTable 的选项。
type tableOpts struct {
	pageSize  int                     // 每页行数
	page      int                     // 先翻到第几页；0 是第 1 页
	preselect int                     // 不小于 0：先在卡片视图里选中本页序号不小于它的第一个有标题的行（挂载时已有选中行）；没标题的行可访问名重名，测试里不好认
	reference bool                    // 窗口放宽到整页：声明的行与窗口化之前完全一样
	entries   []services.LibraryEntry // 库里的条目；nil 取 GenerateEntries(400)
	setup     func(s *Screen)         // 表格挂载之前调整界面（列配置）
}

// openTable 打开一个表格会话（已切到列表视图并稳定）。
func openTable(t *testing.T, o tableOpts) *rig {
	t.Helper()
	entries := o.entries
	if entries == nil {
		entries = servicestest.GenerateEntries(400)
	}
	r := wsRig(t, func(r *rig) {
		r.svc.SeedEntries(entries)
		if o.reference {
			r.screen.table.overscan = 1_000_000 // 窗口放宽到整页：与窗口化之前的声明相同
		}
	})
	r.store.SetPageSize(r.screen.Ctx, o.pageSize)
	if o.page > 1 {
		r.store.SetPage(r.screen.Ctx, o.page)
	}
	r.frames(4)
	if o.preselect >= 0 {
		list := r.view().List.Entries
		i := o.preselect
		for i < len(list) && list[i].Title == "" {
			i++
		}
		r.store.SelectItem(list[i].ID)
		r.frames(4)
	}
	if o.setup != nil {
		o.setup(r.screen)
		r.frames(2) // setup 可能改了版面（字号档）：点击按钮之前先按新版面出几帧，否则点的是旧版面里的位置
	}
	r.click(gfx.RoleButton, locales.LibraryViewList())
	r.frames(6)
	return r
}

// tablePair 是同一份数据的两个表格会话：win 是生产的窗口，ref 是参照。
type tablePair struct{ win, ref *rig }

// openTablePair 打开一对：参照是整页声明的同一份数据。
func openTablePair(t *testing.T, o tableOpts) tablePair {
	t.Helper()
	ref := o
	ref.reference = true
	return tablePair{win: openTable(t, o), ref: openTable(t, ref)}
}

func (p tablePair) each(f func(r *rig)) {
	f(p.win)
	f(p.ref)
}

// scroll 在两个会话里各滚一次滚轮（落在表格区里）。
func (p tablePair) scroll(t *testing.T, dy float32) {
	t.Helper()
	p.each(func(r *rig) { r.wheel(t, dy) })
}

func (r *rig) wheel(t *testing.T, dy float32) {
	t.Helper()
	if err := r.session.Scroll(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: dy, Precise: true}); err != nil {
		t.Fatalf("滚动失败：%v", err)
	}
	r.frames(4)
}

// pixels 是栅格化之后整幅画面的像素。
func (r *rig) pixels() (pix []byte, w, h int) {
	r.t.Helper()
	img, err := r.session.Image()
	if err != nil {
		r.t.Fatalf("栅格化失败：%v", err)
	}
	b := img.Bounds()
	return img.Pix, b.Dx(), b.Dy()
}

// same 比较两个会话的整幅画面；不同时报出不同的像素数、范围与第一处，方便定位。
func (p tablePair) same(t *testing.T, step string) {
	t.Helper()
	p.each(func(r *rig) { r.frames(2) })
	a, w, h := p.win.pixels()
	b, w2, h2 := p.ref.pixels()
	if w != w2 || h != h2 {
		t.Fatalf("%s：画面尺寸不同 %dx%d / %dx%d", step, w, h, w2, h2)
	}
	if bytes.Equal(a, b) {
		return
	}
	diff, minX, minY, maxX, maxY := 0, w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			if !bytes.Equal(a[i:i+4], b[i:i+4]) {
				diff++
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			}
		}
	}
	first, last := p.win.screen.table.window.Declared()
	t.Fatalf("%s：窗口化与参照的画面有 %d 个像素不同，范围 x[%d,%d] y[%d,%d]（窗口声明了第 [%d, %d) 行）", step, diff, minX, maxX, minY, maxY, first, last)
}

// 一页 100 行：窗口每帧只声明视口附近的行，并且每一行都量过（整页声明那一帧量的）。
func TestTableDeclaresOnlyTheRowsNearTheViewport(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	p := openTablePair(t, tableOpts{pageSize: 100, preselect: -1})
	n := len(p.win.view().List.Entries)
	if n != 100 {
		t.Fatalf("夹具应读到一页 100 行，实际 %d", n)
	}
	first, last := p.win.screen.table.window.Declared()
	if first != 0 || last >= 40 {
		t.Fatalf("顶部的窗口应是 [0, 约 28)，实际 [%d, %d)", first, last)
	}
	if got := p.win.screen.table.window.Measured(); got != n {
		t.Fatalf("每一行都该量过（整页声明那一帧），实际 %d / %d", got, n)
	}
	if f, l := p.ref.screen.table.window.Declared(); f != 0 || l != n {
		t.Fatalf("参照应整页声明，实际 [%d, %d)", f, l)
	}
	entries := p.win.view().List.Entries
	if _, ok := p.win.cardOf(entries[0]); !ok {
		t.Fatal("第一行应在画面里")
	}
	lastTitled := n - 1 // 没标题的行可访问名重名（都叫「无标题」），找最后一个有标题的行
	for entries[lastTitled].Title == "" {
		lastTitled--
	}
	if _, ok := p.win.cardOf(entries[lastTitled]); ok {
		t.Fatalf("第 %d 行不该在顶部的画面里", lastTitled)
	}
	p.same(t, "顶部")
}

// 滚轮：小步、大步、跳到底、跳回顶，每一步都与整页声明逐像素相同（吸附的表头与操作列也在其中）。
func TestTableWindowMatchesTheFullTableWhileScrolling(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	p := openTablePair(t, tableOpts{pageSize: 100, preselect: -1})
	for _, dy := range []float32{37, 300, 1700, 5000, -2200, 100000, -100000, 450, 9, 1e7, -1e7} {
		p.scroll(t, dy)
		p.same(t, fmt.Sprintf("滚 %.0f 之后", dy))
	}
	// 走到中段时窗口确实不在顶部，两侧都裁了行。
	p.scroll(t, 2600)
	first, last := p.win.screen.table.window.Declared()
	if first == 0 || last >= 100 {
		t.Fatalf("滚到中段时窗口应裁掉两侧的行，实际 [%d, %d)", first, last)
	}
	p.same(t, "中段")
}

// 键盘：方向键、End、Home 移动选中行，选中行按 scrollIntoView({block:"nearest"}) 滚进视口，落点与整页声明一致。
func TestTableKeysMatchTheFullTable(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	p := openTablePair(t, tableOpts{pageSize: 100, preselect: -1})
	for _, key := range []string{"down", "down", "down", "end", "up", "up", "home", "down", "end"} {
		p.each(func(r *rig) { r.press(key) })
		p.same(t, "按 "+key)
	}
	entries := p.win.view().List.Entries
	if got := p.win.selectedIndex(); got != len(entries)-1 {
		t.Fatalf("End 应选中最后一行，实际序号 %d", got)
	}
	last := len(entries) - 1
	for entries[last].Title == "" {
		last--
	}
	if _, ok := p.win.cardOf(entries[last]); !ok {
		t.Fatal("End 之后最后几行（它们原先在窗口之外）应已滚进视口")
	}
}

// 挂载时已有选中行（先在卡片视图里选了、再切到表格）：纵向按「最近」滚进视口，与整页声明一致；选中的行在窗口之外也一样。
func TestTableRevealsAPreselectedRowOutsideTheWindow(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	p := openTablePair(t, tableOpts{pageSize: 100, preselect: 70})
	p.same(t, "挂载")
	first, last := p.win.screen.table.window.Declared()
	selected := p.win.selectedIndex()
	if selected < 70 || selected < first || selected >= last {
		t.Fatalf("选中的行（序号 %d，不小于 70）应在窗口 [%d, %d) 里", selected, first, last)
	}
	e := p.win.view().List.Entries[selected]
	node, ok := p.win.cardOf(e)
	if !ok {
		t.Fatal("挂载时选中的行应已滚进视口")
	}
	next := p.win.mustNode(gfx.RoleButton, locales.LibraryNextPage())
	if node.Bounds.Y+node.Bounds.Height > next.Bounds.Y-8 {
		t.Fatalf("选中行的下沿 %.1f 压到了分页条（上沿约 %.1f）", node.Bounds.Y+node.Bounds.Height, next.Bounds.Y-9)
	}
}

// 选中行滚进视口的绝对落点（参照对拍看不出两边同时错的接线）：带着选中行切到表格时，表格横向滚过内边距那一段，
// 渲染层 scrollIntoView 的真实呈现，默认列下 17px；以及末行只露出下沿（nearest），不靠到最大滚动量——后面还有 13px 的内边距与边框。
func TestTableRevealsTheSelectedRowTheWayTheBrowserDoes(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	// 夹具的工作区（1040px）比默认列的最小宽度宽、表格不溢出；标题列加宽之后才有横向的可滚范围。
	plain := openTable(t, tableOpts{pageSize: 100, preselect: -1, setup: widenTitle(480)})
	selected := openTable(t, tableOpts{pageSize: 100, preselect: 0, setup: widenTitle(480)})
	e := plain.view().List.Entries[0]
	a, okA := plain.cardOf(e)
	b, okB := selected.cardOf(e)
	if !okA || !okB {
		t.Fatalf("第 0 行的标题按钮应在两个会话的画面里（%v、%v）", okA, okB)
	}
	if shift := a.Bounds.X - b.Bounds.X; math.Abs(float64(shift)-17) > 0.5 {
		t.Fatalf("带着选中行切到表格时应横向滚过 17px，实际 %.2f", shift)
	}

	// 末行：选中之后下沿贴着视口下沿，距最大滚动量还差下内边距加边框。
	last := len(plain.view().List.Entries) - 1
	plain.store.SelectItem(plain.view().List.Entries[last].ID)
	plain.frames(8)
	rowY := func() float32 {
		top, _, ok := plain.screen.table.window.ItemSpan(last)
		if !ok {
			t.Fatal("末行的位置应可读")
		}
		return plain.screen.table.reveal.rowsY + top
	}
	before := rowY()
	plain.wheel(t, 1e7) // 再滚到底
	if rest := before - rowY(); math.Abs(float64(rest-(plain.look.Space(tableAreaPadY)+1))) > 0.5 {
		t.Fatalf("选中末行之后距最大滚动量应还差 %.1f（下内边距加边框），实际 %.2f", plain.look.Space(tableAreaPadY)+1, rest)
	}
}

// 复选框是套件控件，不在行的悬停分组里，行的悬停底色靠 checkHover 补：指针停在复选框上时标记要一直留着，
// 不能被「行滚出窗口」的清理误清掉。
func TestTableKeepsTheCheckboxHoverWhileThePointerStaysOnIt(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	r := openTable(t, tableOpts{pageSize: 100, preselect: -1})
	var e services.LibraryEntry
	for _, c := range r.view().List.Entries {
		if c.Title != "" {
			e = c
			break
		}
	}
	r.hover(gfx.RoleCheckbox, locales.LibrarySelectRow(e.Title))
	r.frames(4)
	if got := r.screen.table.checkHover; got != e.ID {
		t.Fatalf("指针停在第一个有标题的行的复选框上，checkHover 应是 %q，实际 %q", e.ID, got)
	}
}

// 换页：整页内容换了，行高全部作废、整页声明一帧量完再窗口化。参照是直接打开第 2 页的新会话（没有第 1 页留下的任何东西）。
func TestTableMeasuresEveryRowAgainAfterPaging(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	win := openTable(t, tableOpts{pageSize: 100, preselect: -1})
	win.wheel(t, 1500)
	win.store.SetPage(win.screen.Ctx, 2)
	win.frames(6)
	ref := openTable(t, tableOpts{pageSize: 100, page: 2, preselect: -1, reference: true})
	ref.wheel(t, 1500) // 翻页之后视口留在原来的偏移（浏览器里滚动容器也是这样），参照滚到同一处
	p := tablePair{win: win, ref: ref}
	p.same(t, "翻到第 2 页")
	if got, n := win.screen.table.window.Measured(), len(win.view().List.Entries); got != n {
		t.Fatalf("翻页之后每一行都该重新量过，实际 %d / %d", got, n)
	}
	first, last := win.screen.table.window.Declared()
	if last-first >= 60 {
		t.Fatalf("量完之后窗口应只声明视口附近的行，实际 [%d, %d)", first, last)
	}
	p.scroll(t, 100000)
	p.same(t, "第 2 页滚到底")
}

// widenTitle 把标题列的宽度设成 width（在表格挂载之前）。
func widenTitle(width float32) func(s *Screen) {
	return func(s *Screen) {
		s.loadColumns()
		s.setColumnWidth(colTitle, width, false)
	}
}

// 列宽变了：标题列的宽度决定标题折成几行，行高跟着变；窗口据此重量（tableEpoch），不能拿旧的高度。
// 参照是直接以那个列宽打开的新会话。
func TestTableMeasuresEveryRowAgainWhenAColumnChangesWidth(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	win := openTable(t, tableOpts{pageSize: 100, preselect: -1})
	win.wheel(t, 1800)
	for _, width := range []float32{150, 520, 280} {
		win.screen.setColumnWidth(colTitle, width, false)
		win.frames(6)
		ref := openTable(t, tableOpts{pageSize: 100, preselect: -1, reference: true, setup: widenTitle(width)})
		ref.wheel(t, 1800)
		p := tablePair{win: win, ref: ref}
		p.same(t, fmt.Sprintf("标题列 %.0f 之后", width))
	}
}

// 列的显隐变了：表格比最小宽度宽时各列按比例放大，隐藏一列让其余列（含标题列）变宽、标题折行随之变；
// 列宽自己没变，窗口靠列的 ID 认出这次变化。参照是直接以那组列打开的新会话。
func TestTableMeasuresEveryRowAgainWhenAColumnIsHidden(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	win := openTable(t, tableOpts{pageSize: 100, preselect: -1})
	win.wheel(t, 1800)
	win.screen.toggleColumn(colSnippet)
	win.frames(6)
	ref := openTable(t, tableOpts{pageSize: 100, preselect: -1, reference: true, setup: func(s *Screen) {
		s.loadColumns()
		s.toggleColumn(colSnippet)
	}})
	ref.wheel(t, 1800)
	tablePair{win: win, ref: ref}.same(t, "隐藏摘要列之后")
}

// 字号档变了（根字号 16 → 18）：行高随之变，窗口靠 Epoch.RootPx 重量；参照是直接以那个字号档打开的新会话。
func TestTableMeasuresEveryRowAgainWhenTheFontTierChanges(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	win := openTable(t, tableOpts{pageSize: 100, preselect: -1})
	win.wheel(t, 1800)
	win.look.RootPx = 18
	win.frames(6)
	ref := openTable(t, tableOpts{pageSize: 100, preselect: -1, reference: true, setup: func(s *Screen) { s.Look.RootPx = 18 }})
	ref.wheel(t, 1800)
	tablePair{win: win, ref: ref}.same(t, "字号档 18 之后")
}

// 各列宽度此消彼长、总宽不变（表格宽度也不变）：行列只核对自己的宽度，看不出这次变化，靠 epoch 里的各列宽度；
// 标题列的有效宽度变了（260 → 360px），折行随之变。参照是直接以那组列宽打开的新会话。
func TestTableMeasuresEveryRowAgainWhenColumnWidthsTradeOffWithoutChangingTheTableWidth(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	entries := servicestest.GenerateEntries(400)
	for i := range entries {
		if entries[i].Title != "" {
			entries[i].Title = strings.Repeat("宽", 16) // 默认标题列里折两行，加宽之后一行
		}
	}
	tradeOff := func(s *Screen) {
		s.loadColumns()
		s.setColumnWidth(colTitle, 360, false)   // +100
		s.setColumnWidth(colSnippet, 120, false) // -80
		s.setColumnWidth(colTags, 100, false)    // -20
	}
	win := openTable(t, tableOpts{pageSize: 100, preselect: -1, entries: entries})
	win.wheel(t, 1800)
	rowH := func(r *rig) float32 {
		a, _ := r.screen.RowContentTop(0)
		b, _ := r.screen.RowContentTop(1)
		return b - a
	}
	before := rowH(win)
	tradeOff(win.screen)
	win.frames(6)
	if after := rowH(win); before-after < 10 {
		t.Fatalf("前提不成立：加宽标题列之后行高应从两行（约 %.1f）变成一行，实际 %.1f", before, after)
	}
	ref := openTable(t, tableOpts{pageSize: 100, preselect: -1, reference: true, entries: entries, setup: tradeOff})
	ref.wheel(t, 1800)
	tablePair{win: win, ref: ref}.same(t, "各列宽度此消彼长之后")
}

// 条目就地改了标题（同一个 ID、内容换了）：行的键没变，靠 EntriesRev 让窗口重量；参照是直接以改后的数据打开的新会话。
func TestTableMeasuresEveryRowAgainWhenTitlesAreEditedInPlace(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	original := servicestest.GenerateEntries(400)
	edited := servicestest.GenerateEntries(400)
	for i := range edited {
		if i%3 == 1 && edited[i].Title != "" { // 把一部分标题改成要折两行的长标题
			edited[i].Title = strings.Repeat("就地改长的标题", 12)
		}
	}
	win := openTable(t, tableOpts{pageSize: 100, preselect: -1, entries: original})
	win.svc.SeedEntries(edited)
	win.store.FetchList(win.screen.Ctx)
	win.frames(6)
	ref := openTable(t, tableOpts{pageSize: 100, preselect: -1, reference: true, entries: edited})
	tablePair{win: win, ref: ref}.same(t, "就地改标题之后")
}

// RowContentTop（对照场景用它算要滚多远）在窗口里与整页声明一致，窗口不在顶部时也一样；
// 而窗口用的行列上沿（tableTop + headHeight）与实际画出来的位置相符。
func TestTableRowContentTopMatchesTheFullTable(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	p := openTablePair(t, tableOpts{pageSize: 100, preselect: -1})
	top0, ok := p.win.screen.RowContentTop(0)
	if !ok {
		t.Fatal("量完之后第 0 行的位置应可读")
	}
	if want := p.win.screen.tableTop() + p.win.screen.headHeight(); math.Abs(float64(top0-want)) > 0.5 {
		t.Fatalf("第 0 行上沿 %.2f，窗口用的行列上沿 %.2f（tableTop + headHeight）：两者应相符", top0, want)
	}
	check := func(step string) {
		t.Helper()
		for _, i := range []int{0, 2, 17, 60, 99} {
			a, okA := p.win.screen.RowContentTop(i)
			b, okB := p.ref.screen.RowContentTop(i)
			if !okA || !okB || math.Abs(float64(a-b)) > 0.01 {
				t.Fatalf("%s：第 %d 行上沿 窗口化 %.2f（%v），整页 %.2f（%v）", step, i, a, okA, b, okB)
			}
		}
	}
	check("顶部")
	// 绝对位置（对拍看不出两边同时错）：第 2 行与第 0 行的上缘差，等于画面里两个单行标题按钮的上沿差（格内偏移相同）。
	entries := p.win.view().List.Entries
	n0, ok0 := p.win.cardOf(entries[0])
	n2, ok2 := p.win.cardOf(entries[2])
	t0, _ := p.win.screen.RowContentTop(0)
	t2, _ := p.win.screen.RowContentTop(2)
	if !ok0 || !ok2 || math.Abs(float64((t2-t0)-(n2.Bounds.Y-n0.Bounds.Y))) > 0.5 {
		t.Fatalf("第 2 行与第 0 行的上缘差 %.2f，画面里两行标题按钮的上沿差 %.2f（%v、%v）", t2-t0, n2.Bounds.Y-n0.Bounds.Y, ok0, ok2)
	}
	p.scroll(t, 2600) // 第一条声明的行不是第 0 行：位置要靠窗口的位置表还原
	if first, _ := p.win.screen.table.window.Declared(); first == 0 {
		t.Fatal("前提不成立：滚到中段之后窗口应从第 0 行之后开始")
	}
	check("中段")
	p.scroll(t, 100000)
	check("底部")
	if _, ok := p.win.screen.RowContentTop(100); ok {
		t.Fatal("没有第 100 行，不该返回位置")
	}
}

// 整页内容换了（翻页）：行高全部作废，下一帧整页声明、行高要到绘制之后才量到——这一帧结束时位置只是估计，
// RowContentTop 不能把它当真（对照场景据此决定滚多远）；再出一帧量完之后才可读。
func TestTableRowContentTopWaitsForEveryRowToBeMeasured(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	r := openTable(t, tableOpts{pageSize: 100, preselect: -1})
	if _, ok := r.screen.RowContentTop(0); !ok {
		t.Fatal("前提不成立：稳定之后第 0 行的位置应可读")
	}
	r.store.SetPage(r.screen.Ctx, 2)
	r.frames(1)
	if top, ok := r.screen.RowContentTop(0); ok {
		t.Fatalf("翻页后的第一帧行高还没量完就返回了位置 %.2f", top)
	}
	r.frames(6)
	if _, ok := r.screen.RowContentTop(0); !ok {
		t.Fatal("量完之后第 0 行的位置应可读")
	}
}

// 复选框的悬停记在行外（checkHover）：它所在的行滚出窗口、不再声明时标记要清掉，
// 否则滚回来的第一帧那一行会凭旧标记亮一下（行自己的悬停处理没机会清它）。
func TestTableForgetsTheCheckboxHoverOfARowThatLeftTheWindow(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	r := openTable(t, tableOpts{pageSize: 100, preselect: -1})
	entries := r.view().List.Entries
	r.screen.table.checkHover = entries[3].ID
	r.wheel(t, 4000)
	if first, _ := r.screen.table.window.Declared(); first <= 3 {
		t.Fatalf("前提不成立：滚过 4000 之后第 3 行应已在窗口之外，窗口从第 %d 行起", first)
	}
	if got := r.screen.table.checkHover; got != "" {
		t.Fatalf("行滚出窗口之后复选框的悬停标记还留着：%q", got)
	}
}

// 一页 20 行（默认）：窗口把整页都声明了，滚动区不被标成窗口化，Tab 照常走遍每一行并把它滚进视野——与窗口化之前一样（台账 B-030）。
func TestTableTabReachesEveryRowOfADefaultPage(t *testing.T) {
	t.Parallel() // 每个会话自带外观与页面、互不共享状态；并行压低整包耗时（表格窗口化的对拍测试占了约 7 s）
	r := wsRig(t)
	r.click(gfx.RoleButton, locales.LibraryViewList())
	r.frames(6)
	entries := r.view().List.Entries
	if first, last := r.screen.table.window.Declared(); first != 0 || last != len(entries) {
		t.Fatalf("前提不成立：默认一页 %d 行应被窗口整页声明，实际 [%d, %d)", len(entries), first, last)
	}
	var target services.LibraryEntry
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Title != "" {
			target = entries[i]
			break
		}
	}
	if _, ok := r.cardOf(target); ok {
		t.Fatalf("前提不成立：最后一个有标题的行「%s」在第一屏里", target.Title)
	}
	for i := 0; i < 400; i++ {
		r.keys("Tab")
		label, _, ok := r.focusedLabel()
		if !ok || label != target.Title {
			continue
		}
		node := r.mustNode(gfx.RoleButton, target.Title)
		next := r.mustNode(gfx.RoleButton, locales.LibraryNextPage())
		if node.Bounds.Y < 0 || node.Bounds.Y+node.Bounds.Height > next.Bounds.Y-8 {
			t.Fatalf("第 %d 次 Tab 聚焦「%s」时它不在视野里（%.1f..%.1f，分页条约 %.1f）：滚动区没有跟着滚",
				i+1, target.Title, node.Bounds.Y, node.Bounds.Y+node.Bounds.Height, next.Bounds.Y-9)
		}
		return
	}
	t.Fatalf("按 Tab 一路走下去没有轮到最后一个有标题的行「%s」", target.Title)
}
