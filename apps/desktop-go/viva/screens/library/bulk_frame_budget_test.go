package library

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// TestBulkFrameBudget 读出批量状态下的帧耗时（M2-3 完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）。
//
// 两组状态是 M2-3 新增的、原来的 TestListFrameBudget 量不到的：
//  1. 「全部符合筛选」之后的批量条与状态条：库 1 万条，选中的是整个筛选结果（「全部」范围 8000 条），列表只画本页（页大小 100），
//     卡片与表格各量稳态帧、悬停一步、滚轮一步；
//  2. 批量结果条展开后的「逐条结果」清单：渲染层整表渲染 <ul class="max-h-36 overflow-auto">，清单只有 9rem 高、一次看得见几行；
//     Viva 一侧用 kit.RowWindow 只声明看得见的行（B-056：窗口化之前整表声明，1000 条结果稳态帧 6.88 ms，8000 条 138.97 ms）。
//     结果数取 100 / 1000 / 8000（对 1 万条的库做「全部符合筛选」再批量收藏的情形），量展开之后的稳态帧与清单内滚轮一步。
//
// 无头会话的 Frame() 只记录绘制命令、不栅格化，量的是界面线程的出帧成本（布局 + 记录）。
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/library -run TestBulkFrameBudget -v -count=1。
// 每个读数前先核对操作确实改变了画面，不让「什么都没做」的空操作混进读数。
func TestBulkFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	for _, view := range []string{"card", "table"} {
		name := fmt.Sprintf("%s_10000_entries_page_100_all_matching_selected", view)
		t.Run(name, func(t *testing.T) { measureSelectedFrames(t, name, view == "table") })
	}
	for _, total := range []int{125, 1250, 10000} { // 「全部」范围是其中 80%：100、1000、8000 条
		name := fmt.Sprintf("results_%d_expanded", total*8/10)
		t.Run(name, func(t *testing.T) { measureResultsFrames(t, name, total) })
	}
}

// textsFingerprint 是画面上全部文字（值与位置）的指纹：逐条结果的行不是可访问节点，滚动是否生效要看文字的位置。
func textsFingerprint(r *rig) string {
	var b strings.Builder
	for _, t := range r.session.Snapshot().Texts {
		fmt.Fprintf(&b, "%s@%.1f,%.1f;", t.Value, t.Bounds.X, t.Bounds.Y)
	}
	return b.String()
}

func measureSelectedFrames(t *testing.T, name string, table bool) {
	r := wsRig(t, func(r *rig) { r.svc.SeedEntries(servicestest.GenerateEntries(10000)) })
	if table {
		r.click(gfx.RoleButton, locales.LibraryViewList())
		r.frames(4)
	}
	r.store.SetPageSize(r.screen.Ctx, 100)
	r.frames(4)
	if !r.store.SelectAllMatching(r.screen.Ctx) {
		t.Fatal("全部符合筛选应成功")
	}
	r.frames(4)
	if got := len(r.view().List.Selection); got != r.view().List.Total || got < 1000 {
		t.Fatalf("应选中整个筛选结果（%d 条），实际 %d 条", r.view().List.Total, got)
	}
	if !r.has(gfx.RoleStatus, "") && !r.hasText(r.view().Batch.Notice) {
		t.Fatal("状态条应写冻结了多少项，读数无效")
	}
	first, ok := cardOrNamed(r, r.view().List.Entries[0], "")
	if !ok {
		t.Fatal("找不到第一行（夹具失效）")
	}
	second, ok := cardOrNamed(r, r.view().List.Entries[1], "")
	if !ok {
		t.Fatal("找不到第二行（夹具失效）")
	}
	cx, cy := centerOf(first)
	_, cy2 := centerOf(second)

	idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录）：%s", name, idle)

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
		t.Fatal("向下滚动 800 px 之后画面没有变化，滚动读数无效")
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
}

func measureResultsFrames(t *testing.T, name string, total int) {
	heapBefore := frametime.HeapInUse()
	r := wsRig(t, func(r *rig) { r.svc.SeedEntries(servicestest.GenerateEntries(total)) })
	if !r.store.SelectAllMatching(r.screen.Ctx) {
		t.Fatal("全部符合筛选应成功")
	}
	r.frames(4)
	r.click(gfx.RoleButton, locales.LibraryBulkFavorite())
	r.frames(4)
	n := len(r.view().Batch.Results)
	if n != r.view().List.Total || n < 100 {
		t.Fatalf("应有 %d 条逐条结果，实际 %d 条", r.view().List.Total, n)
	}
	label := locales.LibraryWorkflowResults(n)
	closed := textsFingerprint(r)
	r.click(gfx.RoleButton, label)
	r.frames(4)
	if textsFingerprint(r) == closed {
		t.Fatal("展开「逐条结果」之后画面没有变化，读数无效")
	}
	t.Logf("%s 展开后 Go 堆增量（含无头会话）：%d KiB", name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	idle, err := frametime.Measure(100, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录，%d 条结果）：%s", name, n, idle)

	// 清单内滚轮一步：指针落在「逐条结果」那一行下面的清单里（清单 9rem 高）。
	summary := r.mustNode(gfx.RoleButton, label)
	sx, sy := centerOf(summary)
	scrollStep := func(dy float32) error {
		if err := r.session.Scroll(gfx.ScrollEvent{X: sx, Y: sy + 40, DeltaY: dy, Precise: true}); err != nil {
			return err
		}
		return r.session.Frame()
	}
	before := textsFingerprint(r)
	for i := 0; i < 10; i++ {
		if err := scrollStep(40); err != nil {
			t.Fatalf("预检滚动失败：%v", err)
		}
	}
	if textsFingerprint(r) == before {
		t.Fatal("在清单里向下滚动之后画面没有变化，滚动读数无效")
	}
	for i := 0; i < 10; i++ {
		if err := scrollStep(-40); err != nil {
			t.Fatalf("预检回滚失败：%v", err)
		}
	}
	scroll, err := frametime.Measure(100, func(i int) error {
		if (i/10)%2 == 1 {
			return scrollStep(-40)
		}
		return scrollStep(40)
	})
	if err != nil {
		t.Fatalf("滚动帧失败：%v", err)
	}
	t.Logf("%s 清单内滚轮一步（滚动+出帧）：%s", name, scroll)
}
