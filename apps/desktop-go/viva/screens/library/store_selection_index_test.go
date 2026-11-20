package library

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件核对多选集合的成员索引（ListView.selected，台账 B-056）：IsSelected 每帧每个可见行都要问，
// 「全部符合筛选」能选中上万条，线性查找会让每帧慢到几毫秒。索引只靠 setSelection 与 Selection 一起整体换新，
// 所以每个会改多选集合的入口之后都核对一遍两者一致——有人直接给 Selection 赋值，索引就会过期，这里能立刻发现。

// assertSelectionIndex 核对索引与 Selection 完全一致：索引的成员恰好是 Selection 的成员（不多不少），
// 空选择没有索引，IsSelected 在本页条目、选中的条目与一个不存在的 ID 上都与线性查找的结果相同。
func assertSelectionIndex(t *testing.T, where string, v ListView) {
	t.Helper()
	want := map[string]struct{}{}
	for _, id := range v.Selection {
		want[id] = struct{}{}
	}
	if len(v.Selection) == 0 && v.selected != nil {
		t.Fatalf("%s：选择为空时不应留着索引（%d 项）", where, len(v.selected))
	}
	if len(v.selected) != len(want) {
		t.Fatalf("%s：索引有 %d 项，Selection 去重后有 %d 项", where, len(v.selected), len(want))
	}
	for id := range want {
		if _, ok := v.selected[id]; !ok {
			t.Fatalf("%s：%s 在 Selection 里，索引里没有", where, id)
		}
	}
	for _, id := range append(entryIDs(v), "不存在的条目") {
		if got, lin := v.IsSelected(id), slices.Contains(v.Selection, id); got != lin {
			t.Fatalf("%s：IsSelected(%s) = %v，线性查找 %v", where, id, got, lin)
		}
	}
}

// 每个会改多选集合的入口之后，索引都与 Selection 一致：Ctrl 点击（选入、取消）、Shift 范围（两个分支）、整体设定、
// 本页并入、本页切换（两个方向）、清空、全部符合筛选、批量之后只留失败项、搜索与换筛选时的重置。
func TestSelectionIndexFollowsEveryMutation(t *testing.T) {
	h := batchHarness(t)
	ids := entryIDs(h.store.View().List)
	check := func(where string) { t.Helper(); assertSelectionIndex(t, where, h.store.View().List) }

	check("初始")
	h.store.ToggleSelection(ids[2])
	check("Ctrl 点击选入")
	h.store.ToggleSelection(ids[5])
	h.store.ToggleSelection(ids[2])
	check("Ctrl 点击取消")
	if v := h.store.View().List; v.IsSelected(ids[2]) || !v.IsSelected(ids[5]) {
		t.Fatalf("取消的那条不能还算选中、没动的那条仍是：%v", v.Selection)
	}
	h.store.RangeSelectTo(ids[8])
	check("Shift 范围")
	h.store.RangeSelectTo("不在本页")
	check("Shift 目标不在本页")
	h.store.SetSelection([]string{ids[1], ids[3]})
	check("整体设定")
	h.store.SelectPage()
	check("本页并入")
	h.store.TogglePageSelection() // 本页已全部选中：取消本页
	check("本页切换（取消）")
	h.store.TogglePageSelection() // 再选回来
	check("本页切换（选入）")
	h.store.ClearSelection()
	check("清空")

	if !h.store.SelectAllMatching(h.ctx) {
		t.Fatal("冻结应成功")
	}
	check("全部符合筛选")
	if v := h.store.View().List; len(v.selected) != listTotal || !v.IsSelected(ids[0]) {
		t.Fatalf("全部符合筛选应选中 %d 条：索引 %d 项", listTotal, len(v.selected))
	}

	// 批量之后选择只留失败的条目：索引也只剩它们。
	h.svc.FailBatchItem(ids[4], "写入失败")
	h.store.BulkUpdate(h.ctx, selection(h.store.View()), services.LibraryEntryPatch{IsFavorite: services.BoolPtr(true)})
	check("批量之后只留失败项")
	if v := h.store.View().List; !slices.Equal(v.Selection, []string{ids[4]}) || v.IsSelected(ids[5]) {
		t.Fatalf("批后应只剩失败的那条：%v", v.Selection)
	}

	h.store.SetSelection([]string{ids[0]})
	h.store.SetSearch(h.ctx, "条目")
	check("搜索词变了清空选择")
	h.store.SetSelection([]string{ids[0]})
	h.store.SetScope(h.ctx, services.LibraryScopeFavorites)
	check("换筛选清空选择")
	if v := h.store.View().List; len(v.Selection) != 0 || v.IsSelected(ids[0]) {
		t.Fatalf("换筛选之后选择应清空：%v", v.Selection)
	}
}

// 索引不随 ListView 的复制而分叉：View() 给出的是按值复制的视图，旧快照不受后来变化的影响（索引整体换新、不就地改）。
func TestSelectionIndexSnapshotsAreIndependent(t *testing.T) {
	h := batchHarness(t)
	ids := entryIDs(h.store.View().List)
	h.store.ToggleSelection(ids[1])
	before := h.store.View().List
	h.store.ToggleSelection(ids[2])
	h.store.ToggleSelection(ids[1])
	if !before.IsSelected(ids[1]) || before.IsSelected(ids[2]) {
		t.Fatalf("旧快照应保持当时的选择：%v", before.Selection)
	}
	assertSelectionIndex(t, "旧快照", before)
	assertSelectionIndex(t, "新快照", h.store.View().List)
}

// 直接构造的视图（没有索引）仍然能回答成员问题：退回线性查找，结果与有索引时相同。
func TestIsSelectedFallsBackToALinearScanWithoutAnIndex(t *testing.T) {
	v := ListView{Selection: []string{"a", "b"}}
	if !v.IsSelected("b") || v.IsSelected("c") {
		t.Fatalf("没有索引时的成员判断不对：%v", v.Selection)
	}
}

// 选中很多条不拖慢每一帧（台账 B-056）：「全部符合筛选」能选中上万条，而列表每帧要问每个可见行「选中了吗」、
// 批量条每帧要数选中数与找选中的公众号条目——这些都不能随选中数增长。
// 用比值而不是绝对耗时：同一进程里先量选 10 条的帧、再量选 20 万条（绝大多数不在库里）的帧，各取几批里最快的一批的均值，
// 机器快慢与噪声两边一样；线性查找时后者是前者的十几倍，索引之后只差一点点（两者同样画本页 20 行与同一个批量条）。
// 不并入 t.Parallel：并行测试要等串行测试都跑完才开始，这个测试因此是在没有别的测试争抢 CPU 时量的。
func TestFramesWithAHugeSelectionCostAboutAsMuchAsWithASmallOne(t *testing.T) {
	for _, view := range []string{"card", "table"} {
		t.Run(view, func(t *testing.T) {
			r := wsRig(t)
			if view == "table" {
				r.click(gfx.RoleButton, locales.LibraryViewList())
				r.frames(4)
			}
			small := r.ids(0, 9)
			huge := slices.Clone(small)
			for i := 0; i < 200000; i++ {
				// 与库里条目的 ID（item-000000…）等长：字符串比较先比长度，不等长的比较几乎不花时间，会让线性查找看起来很便宜。
				huge = append(huge, fmt.Sprintf("item-%06d", 100000+i))
			}
			fastest := func(ids []string) time.Duration {
				t.Helper()
				r.store.SetSelection(ids)
				r.frames(4)
				best := time.Duration(1<<63 - 1)
				for batch := 0; batch < 7; batch++ {
					stats, err := frametime.Measure(20, func(int) error { return r.session.Frame() })
					if err != nil {
						t.Fatalf("出帧失败：%v", err)
					}
					best = min(best, stats.Mean)
				}
				return best
			}
			cheap, costly := fastest(small), fastest(huge)
			t.Logf("%s：选 %d 条每帧 %v，选 %d 条每帧 %v", view, len(small), cheap, len(huge), costly)
			if costly > 3*cheap+time.Millisecond {
				t.Fatalf("选中 %d 条的帧（%v）不该比选中 %d 条的帧（%v）慢这么多：每帧里有随选中数增长的工作", len(huge), costly, len(small), cheap)
			}
		})
	}
}

// 补采要的 ID 是「选中的、本页上的、公众号来源的」，按选入顺序（渲染层 selectionIds.filter(...)）：
// 不在本页的选中项（跨页选择）、不是公众号来源的、不在选择里的公众号条目都不算。
func TestWechatSelectedKeepsTheSelectionOrderAndSkipsEverythingElse(t *testing.T) {
	v := View{List: ListView{
		Entries: []services.LibraryEntry{
			{ID: "a", Platform: "wechat"}, {ID: "b", Platform: "web"}, {ID: "c", Platform: "wechat"}, {ID: "d", Platform: "wechat"},
		},
		Selection: []string{"c", "b", "other-page", "a"}, // d 在本页是公众号来源、但没选中
	}}
	if got := wechatSelected(v); !slices.Equal(got, []string{"c", "a"}) {
		t.Fatalf("应是 c、a（选入顺序）：%v", got)
	}
	if got := wechatSelected(View{}); len(got) != 0 {
		t.Fatalf("没有选择时应为空：%v", got)
	}
}
