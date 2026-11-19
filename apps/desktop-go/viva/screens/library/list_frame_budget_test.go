package library

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// TestListFrameBudget 读出知识库列表（卡片与表格）在大库上的帧耗时（模块完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）。
//
// 「1 万条」的含义：库里有 1 万条（「全部」范围 8000 条），列表一次只读一页（每页 20、50 或 100 条；10 条的一档比 20 条更轻，不量），
// 界面画的是本页的行，卡片与表格都是定高窗口（kit.RowWindow，台账 B-029），每帧只声明视口里的行与上下余量。
// 所以耗时应与库的总条数无关、只随页大小增长；这里把「库规模 × 页大小 × 视图」交叉量一遍，既证明无关，也给出最坏的一档。
// 另量键盘一步（↓ 选行并把它滚进视野）、翻页一步（整页内容替换）、悬停一步（指针在两行之间来回）。
//
// 无头会话的 Frame() 只记录绘制命令、不栅格化，所以量的是界面线程的出帧成本（布局 + 记录）；
// 栅格化加回读另量一次（Image()），作为真窗口里绘制成本的上界。
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/library -run TestListFrameBudget -v -count=1。
// 结果记进台账 B-029 与开发文档 §8.4。每个读数前先核对操作确实改变了画面，不让「什么都没做」的空操作混进读数。
func TestListFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	for _, view := range []string{"card", "table"} {
		for _, total := range []int{100, 10000} {
			for _, page := range []int{20, 50, 100} {
				name := fmt.Sprintf("%s_%d_entries_page_%d", view, total, page)
				t.Run(name, func(t *testing.T) { measureListFrames(t, name, view == "table", total, page, "") })
			}
		}
	}
	// 搜索命中词高亮（台账 B-041）：夹具里每条的标题（「条目 N」）与摘要都含「条目」，所以本页每一行的标题与摘要都带 mark，
	// 是高亮排版最重的情形；与上面同规模、同页大小的无搜索读数并排看。
	for _, view := range []string{"card", "table"} {
		for _, page := range []int{20, 100} {
			name := fmt.Sprintf("%s_10000_entries_page_%d_search", view, page)
			t.Run(name, func(t *testing.T) { measureListFrames(t, name, view == "table", 10000, page, "条目") })
		}
	}
}

// cardOrNamed 找一行的按钮：表格行的按钮名是标题（r.cardOf）；卡片在搜索时的名字是「标题 + 空格 + 摘要」（cardName）。
func cardOrNamed(r *rig, e services.LibraryEntry, search string) (app.AccessNode, bool) {
	if n, ok := r.cardOf(e); ok {
		return n, true
	}
	title := e.Title
	if title == "" {
		title = locales.LibraryUntitled()
	}
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleButton && n.Label == cardName(title, e, search) {
			return n, true
		}
	}
	return app.AccessNode{}, false
}

// search 非空时先搜索，并核对本页确实画出了命中高亮，免得「没有高亮」的读数冒充「有高亮」的读数。
func measureListFrames(t *testing.T, name string, table bool, total, pageSize int, search string) {
	heapBefore := frametime.HeapInUse()
	r := wsRig(t, func(r *rig) { r.svc.SeedEntries(servicestest.GenerateEntries(total)) })
	if table {
		r.click(gfx.RoleButton, locales.LibraryViewList())
		r.frames(4)
	}
	if pageSize != r.view().List.PageSize {
		r.store.SetPageSize(r.screen.Ctx, pageSize)
		r.frames(4)
	}
	if search != "" {
		r.store.SetSearch(r.screen.Ctx, search)
		r.frames(4)
		if len(r.markedTexts()) == 0 {
			t.Fatalf("搜索 %q 之后本页没有画出命中高亮，读数无效", search)
		}
	}
	// 「全部」范围只数在用的条目（库规模 100 时 80 条，1 万时 8000 条），所以一页至多是 min(页大小, 总数) 条。
	n := len(r.view().List.Entries)
	if want := min(pageSize, r.view().List.Total); n != want {
		t.Fatalf("夹具应读到一页 %d 条，实际 %d（库规模 %d，符合筛选 %d）", want, n, total, r.view().List.Total)
	}
	t.Logf("%s 打开列表后 Go 堆增量（含无头会话）：%d KiB", name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	first, ok := cardOrNamed(r, r.view().List.Entries[0], search)
	if !ok {
		t.Fatal("找不到第一行（夹具失效）")
	}
	second, ok := cardOrNamed(r, r.view().List.Entries[1], search)
	if !ok {
		t.Fatal("找不到第二行（夹具失效）")
	}
	cx, cy := centerOf(first)
	_, cy2 := centerOf(second)

	// 稳态帧：画面不变，连出 300 帧。
	idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录）：%s", name, idle)

	// 悬停一步：指针在前两行之间来回，每次移动都改变悬停态。
	hover, err := frametime.Measure(200, func(i int) error {
		y := cy
		if i%2 == 1 {
			y = cy2
		}
		if err := r.session.Move(cx, y); err != nil {
			return err
		}
		return r.session.Frame()
	})
	if err != nil {
		t.Fatalf("悬停帧失败：%v", err)
	}
	t.Logf("%s 悬停一步（移动指针+出帧）：%s", name, hover)

	// 滚轮一步：每步 40 逻辑像素。先预检——向下 20 步（800 px）画面必须变，再原路滚回。
	scrollStep := func(dy float32) error {
		if err := r.session.Scroll(gfx.ScrollEvent{X: cx, Y: 400, DeltaY: dy, Precise: true}); err != nil {
			return err
		}
		return r.session.Frame()
	}
	top := frametime.Fingerprint(r.session.Access())
	for i := 0; i < 20; i++ {
		if err := scrollStep(40); err != nil {
			t.Fatalf("预检滚动失败：%v", err)
		}
	}
	if top == frametime.Fingerprint(r.session.Access()) {
		t.Fatalf("向下滚动 800 px 之后画面没有变化，滚动读数无效")
	}
	for i := 0; i < 20; i++ {
		if err := scrollStep(-40); err != nil {
			t.Fatalf("预检回滚失败：%v", err)
		}
	}
	scroll, err := frametime.Measure(200, func(i int) error {
		if (i/20)%2 == 1 {
			return scrollStep(-40)
		}
		return scrollStep(40)
	})
	if err != nil {
		t.Fatalf("滚动帧失败：%v", err)
	}
	t.Logf("%s 滚轮一步（滚动+出帧）：%s", name, scroll)

	// 键盘一步：↓ 选下一行并把它滚进视野（选中项越过视口时列表滚动），到底之后 ↑ 再回来；每一步之前核对选中项确实在动。
	r.store.SelectItem("")
	r.frames(2)
	keyStep := func(spec string) error {
		if err := r.session.Keys(spec); err != nil {
			return err
		}
		return r.session.Frame()
	}
	before := r.view().List.SelectedID
	if err := keyStep("down"); err != nil {
		t.Fatalf("预检按键失败：%v", err)
	}
	if r.view().List.SelectedID == before {
		t.Fatalf("按 ↓ 之后选中项没有变化，键盘读数无效")
	}
	keys, err := frametime.Measure(2*(n-2), func(i int) error {
		if i < n-2 {
			return keyStep("down")
		}
		return keyStep("up")
	})
	if err != nil {
		t.Fatalf("按键帧失败：%v", err)
	}
	t.Logf("%s 键盘一步（按键+出帧）：%s", name, keys)

	// 翻页一步：整页内容替换（总页数至少 2 页：库规模 100 的「全部」范围 80 条，每页 100 条时只有 1 页，不量）。
	if l := r.view().List; l.Total > pageSize {
		// 预检：翻一页之后画面必须变（标题序列整体换了），再翻回来。
		before := frametime.Fingerprint(r.session.Access())
		r.store.SetPage(r.screen.Ctx, 2)
		r.frames(2)
		if got := r.view().List.Page; got != 2 || before == frametime.Fingerprint(r.session.Access()) {
			t.Fatalf("翻到第 2 页之后画面没有变化（页 %d），翻页读数无效", got)
		}
		r.store.SetPage(r.screen.Ctx, 1)
		r.frames(2)
		flips, err := frametime.Measure(20, func(i int) error {
			r.store.SetPage(r.screen.Ctx, 2-i%2) // 2、1、2、1……
			return r.session.Frame()
		})
		if err != nil {
			t.Fatalf("翻页帧失败：%v", err)
		}
		t.Logf("%s 翻页一步（整页替换+出帧）：%s", name, flips)
	}

	// 栅格化加回读：真窗口里绘制成本的上界。
	raster, err := frametime.Measure(30, func(int) error {
		_, err := r.session.Image()
		return err
	})
	if err != nil {
		t.Fatalf("栅格化失败：%v", err)
	}
	t.Logf("%s 栅格化+回读（Image，上界）：%s", name, raster)
}
