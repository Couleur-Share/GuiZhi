package inbox

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// TestInboxFrameBudget 读出处理中心在 500 条混合待处理项下的帧耗时（口径同 viva/screens/library 的 TestDetailFrameBudget）。
//
// 500 条是：200 条复核（原因 0～3 条不等）、150 条未归合集、150 条采集问题（失败可重试、已完成带警告、重复各占三分之一），
// 外加两个聚合行（语义索引、Wiki 编译）与 Wiki 候选提示条；勾选其中 60 条复核与 12 条未归合集，批量条（归入合集、标签、整理、智能归类、取消）常驻，
// 页头全选框处于半选。服务端实际每组最多回 100 条（rows.go 的 body 注释），这里是超出线上上限的压力口径：列表做行窗口化（rows.go 的 itemList），只声明视口附近的卡片。
// 完成定义的帧预算：稳态 mean ≤ 2 ms、滚轮一步 mean ≤ 6 ms（窗口化之前 202 行 2.7～2.9 ms / 5.4 ms、352 行 4.4～4.6 ms / 9.1～9.5 ms，见 frame-budget-m31-inbox.txt）。
// 量四个动作：稳态帧、滚轮一步（列表主体上来回滚）、悬停一步（页头刷新钮与第一张卡片的「标记已复核」之间来回）、栅格化加回读（上界）。
// 再给出打开后的 Go 堆增量（含无头会话）。
//
// 无头会话的 Frame() 只记录绘制命令、不栅格化，所以量的是界面线程的出帧成本（布局 + 记录）；栅格化加回读另量一次作为上界。
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/inbox -run TestInboxFrameBudget -v -count=1。
// 每个读数前先核对操作确实改变了画面，不让「什么都没做」的空操作混进读数。
func TestInboxFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	t.Run("inbox_500_mixed_all", func(t *testing.T) { measureInboxFrames(t, "inbox_500_mixed_all", FilterAll, budgetMix500) })
	t.Run("inbox_500_mixed_unclassified", func(t *testing.T) {
		measureInboxFrames(t, "inbox_500_mixed_unclassified", FilterUnclassified, budgetMix500)
	})
	// 对照组：服务端每组 100 条的实际上限（三组共 300 条），线上口径下的读数。
	t.Run("inbox_300_service_cap_all", func(t *testing.T) { measureInboxFrames(t, "inbox_300_service_cap_all", FilterAll, budgetMixServiceCap) })
}

// budgetMix 是三类待处理项的条数。
type budgetMix struct{ reviews, unclassified, imports int }

var (
	budgetMix500        = budgetMix{reviews: 200, unclassified: 150, imports: 150}
	budgetMixServiceCap = budgetMix{reviews: 100, unclassified: 100, imports: 100}
)

const (
	budgetSelectedReviews      = 60
	budgetSelectedUnclassified = 12
)

// budgetItems 造 500 条混合待处理项（不含聚合行：聚合行由刷新时的 Wiki 候选与语义状态产出）。
func budgetItems(mix budgetMix) []services.InboxItem {
	items := make([]services.InboxItem, 0, mix.reviews+mix.unclassified+mix.imports)
	for i := 0; i < mix.reviews; i++ {
		id := fmt.Sprintf("rv-%03d", i)
		var reasons []string
		for k := 0; k < i%4; k++ {
			reasons = append(reasons, fmt.Sprintf("原因 %d：正文过短或标题缺失", k+1))
		}
		items = append(items, review(id, fmt.Sprintf("待复核的内容 %03d：一条标题偏长、需要省略号截断的知识条目标题", i), reasons...))
	}
	for i := 0; i < mix.unclassified; i++ {
		items = append(items, unclassified(fmt.Sprintf("uc-%03d", i), fmt.Sprintf("尚未归类的内容 %03d", i)))
	}
	for i := 0; i < mix.imports; i++ {
		task := fmt.Sprintf("t-%03d", i)
		row := importIssue(task, services.ImportStatusFailed, "抓取失败：连接超时，已重试 3 次仍无响应，请检查网络或稍后重试。")
		switch i % 3 {
		case 1:
			row.Status, row.Message, row.ResultItemID = services.ImportStatusCompleted, "文字稿缺失：视频没有可用字幕", "res-"+task
		case 2:
			row.Status, row.Message, row.DuplicateItemID = services.ImportStatusDuplicate, "与已有内容重复", "dup-"+task
		}
		items = append(items, row)
	}
	return items
}

// budgetSelection 是预置的选中：前 60 条复核加前 12 条未归合集。
func budgetSelection() []string {
	ids := make([]string, 0, budgetSelectedReviews+budgetSelectedUnclassified)
	for i := 0; i < budgetSelectedReviews; i++ {
		ids = append(ids, fmt.Sprintf("rv-%03d", i))
	}
	for i := 0; i < budgetSelectedUnclassified; i++ {
		ids = append(ids, fmt.Sprintf("uc-%03d", i))
	}
	return ids
}

// measureInboxFrames 在 500 条混合项的处理中心里（筛选为 filter、带选中态批量条）逐项量帧。
func measureInboxFrames(t *testing.T, name string, filter Filter, mix budgetMix) {
	heapBefore := frametime.HeapInUse()
	r := newUIRig(t, budgetItems(mix), func(r *uiRig) {
		r.svc.SetWikiModelReady(true) // 模型就绪：提示条是候选摘要加「查看范围和任务」入口，而不是「去配置模型」
		r.svc.SetWikiCandidates(services.InboxWikiCandidates{Ready: 3, Review: 1, Waiting: 2, Upgrade: 1})
		r.svc.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 480, IndexedItems: 455})
		r.collections = []services.LibraryCollection{{ID: "c1", Name: "阅读", Icon: "\U0001F4D6"}, {ID: "c2", Name: "工作"}}
	})
	r.store.SetFilter(filter)
	r.store.SetSelection(budgetSelection())
	r.frames(4)

	// 夹具核对：条数、聚合行、Wiki 提示条、批量条与半选都在。
	v := r.store.View()
	if want := mix.reviews + mix.unclassified + mix.imports + 2; len(v.Items) != want {
		t.Fatalf("夹具应是 %d 条加两个聚合行，实际 %d 行", want-2, len(v.Items))
	}
	if aggregateCount(v, services.InboxKindSemanticPending) <= 0 || aggregateCount(v, services.InboxKindWikiPending) <= 0 {
		t.Fatalf("夹具应带两个聚合行：%v", rowIDs(v.Items))
	}
	wantVisible := mix.reviews + mix.imports + 2
	if filter == FilterUnclassified {
		wantVisible = mix.unclassified
	}
	if len(v.Visible) != wantVisible {
		t.Fatalf("筛选 %q 下可见行应是 %d，实际 %d", filter, wantVisible, len(v.Visible))
	}
	if !v.SomeVisibleSelected() {
		t.Fatalf("页头全选框应处于半选：已选 %d / 可选 %d", v.SelectedVisibleCount, len(v.SelectableVisibleIDs))
	}
	if !r.has(gfx.RoleButton, locales.InboxOrganizeSelected()) || !r.has(gfx.RoleButton, locales.InboxSmartClassify(budgetSelectedUnclassified)) {
		t.Fatalf("选中后应出现批量条（整理、智能归类）；画面上有 %q", r.session.Texts())
	}
	if filter == FilterAll && !r.has(gfx.RoleButton, locales.InboxWikiCandidatesView()) {
		t.Fatalf("应有 Wiki 候选提示条；画面上有 %q", r.session.Texts())
	}
	t.Logf("%s 共 %d 行（可见 %d、已选 %d）；打开后 Go 堆增量（含无头会话）：%d KiB", name,
		len(v.Items), len(v.Visible), len(v.Selection), (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	// 稳态帧：画面不变，连出 300 帧。
	idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录）：%s", name, idle)

	// 悬停一步：指针在页头刷新钮与第一张卡片的主按钮之间来回，每次移动都改变悬停态。
	refresh, ok := r.node(gfx.RoleButton, locales.InboxRefresh())
	if !ok {
		t.Fatal("找不到页头刷新钮")
	}
	cardName := locales.LibraryMarkReviewed()
	if filter == FilterUnclassified {
		cardName = locales.CommonOpen()
	}
	card, ok := r.node(gfx.RoleButton, cardName)
	if !ok {
		t.Fatalf("找不到第一张卡片的「%s」钮", cardName)
	}
	ax, ay := refresh.Bounds.X+refresh.Bounds.Width/2, refresh.Bounds.Y+refresh.Bounds.Height/2
	bx, by := card.Bounds.X+card.Bounds.Width/2, card.Bounds.Y+card.Bounds.Height/2
	hover, err := frametime.Measure(200, func(i int) error {
		x, y := ax, ay
		if i%2 == 1 {
			x, y = bx, by
		}
		if err := r.session.Move(x, y); err != nil {
			return err
		}
		return r.session.Frame()
	})
	if err != nil {
		t.Fatalf("悬停帧失败：%v", err)
	}
	t.Logf("%s 悬停一步（移动指针+出帧）：%s", name, hover)

	// 滚轮一步：在列表主体上来回滚；先核对无障碍指纹确实变了（画面真的滚动了）。
	wx, wy := float32(760), float32(500)
	before := frametime.Fingerprint(r.session.Access())
	if err := r.session.Scroll(gfx.ScrollEvent{X: wx, Y: wy, DeltaY: 400, Precise: true}); err != nil {
		t.Fatalf("预检滚轮失败：%v", err)
	}
	r.frames(2)
	if frametime.Fingerprint(r.session.Access()) == before {
		t.Fatal("滚轮之后列表画面无变化，滚动读数无效")
	}
	scroll, err := frametime.Measure(60, func(i int) error {
		dy := float32(120)
		if i%2 == 1 {
			dy = -120
		}
		if err := r.session.Scroll(gfx.ScrollEvent{X: wx, Y: wy, DeltaY: dy, Precise: true}); err != nil {
			return err
		}
		return r.session.Frame()
	})
	if err != nil {
		t.Fatalf("滚轮帧失败：%v", err)
	}
	t.Logf("%s 滚轮一步（滚轮+出帧）：%s", name, scroll)

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
