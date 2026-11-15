package imports

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/locales"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
)

func TestMain(m *testing.M) {
	// 断言里的中文文案不能随运行机器的系统语言变化
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	m.Run()
}

// harness 把页面接到无窗口的 Tester 上；每次后台加载结束（Invalidate）发一个信号。
type harness struct {
	t      *testing.T
	fake   *servicestest.Imports
	screen *Screen
	tr     *ui.Tester
	done   chan struct{}
}

var fixedNow = time.Date(2026, 11, 15, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))

func newHarness(t *testing.T, page services.ImportPage, err error) *harness {
	t.Helper()
	h := &harness{t: t, fake: &servicestest.Imports{}, done: make(chan struct{}, 16)}
	h.fake.Set(page, err)
	h.screen = New(Options{
		Imports:    h.fake,
		Invalidate: func() { h.done <- struct{}{} },
		Now:        func() time.Time { return fixedNow },
		Debounce:   -1, // 立即查询，测试不依赖定时器
	})
	t.Cleanup(h.screen.Close)
	h.tr = ui.NewTester(h.screen.View, 900, 640)
	return h
}

// settle 等一次后台加载结束，再渲染一帧让视图读到新快照。
func (h *harness) settle() {
	h.t.Helper()
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		h.t.Fatal("等待加载完成超时")
	}
	h.tr.Frame()
}

func at(h, m int) int64 {
	return time.Date(2026, 11, 15, h, m, 0, 0, fixedNow.Location()).UnixMilli()
}

func samplePage() services.ImportPage {
	return services.ImportPage{
		Entries: []services.ImportTask{
			{ID: "c1", Status: "completed", SourceKind: "url", SourceInput: "https://www.v2ex.com/t/1253950", DisplayName: "原生 UI 迁移的坑", CreatedAt: at(9, 0), UpdatedAt: at(9, 5)},
			{ID: "c2", Status: "completed", SourceKind: "url", SourceInput: "https://example.com/a", DisplayName: "转写有缺失的视频", Warning: "语音转写失败", CreatedAt: at(8, 0), UpdatedAt: at(8, 30)},
			{ID: "f1", Status: "failed", SourceKind: "file", SourceInput: `D:\docs\notes\spec.pdf`, DisplayName: "spec.pdf", Error: "解析失败：文件已损坏", CreatedAt: at(7, 0), UpdatedAt: at(7, 1)},
		},
		Active: []services.ImportTask{
			{ID: "p1", Status: "processing", Stage: "extracting", SourceKind: "url", SourceInput: "https://github.com/egoist/mygo", DisplayName: "mygo 仓库", CreatedAt: at(11, 50), UpdatedAt: at(11, 55)},
		},
		Total:         3,
		Counts:        map[string]int64{"pending": 0, "processing": 1, "completed": 2, "failed": 1, "canceled": 0, "duplicate": 0},
		DegradedCount: 1,
	}
}

func TestLoadsAndShowsRows(t *testing.T) {
	h := newHarness(t, samplePage(), nil)
	h.tr.Frame()
	h.screen.Start()
	h.settle()

	for _, want := range []string{
		"原生 UI 迁移的坑", "mygo 仓库", "spec.pdf",
		"v2ex.com",                      // 域名去掉 www.
		"D:/docs/notes",                 // 文件任务显示所在目录
		"解析中",                           // 进行中任务的阶段文案
		"解析失败：文件已损坏",                    // 失败原因
		"语音转写失败",                        // 降级任务的警告
		locales.ImportsStatusDegraded(), // 降级的完成态单独成档
		"11:50",                         // 进行中的任务显示入队时间
		"09:05",                         // 已结束的任务显示更新时间
		locales.ImportsSummaryActive(3, 1),
	} {
		if !h.tr.HasText(want) {
			t.Errorf("界面缺少 %q", want)
		}
	}
}

func TestLoadErrorIsNotAnEmptyState(t *testing.T) {
	h := newHarness(t, services.ImportPage{}, errors.New("database is locked"))
	h.tr.Frame()
	h.screen.Start()
	h.settle()

	if !h.tr.HasText(locales.CommonLoadFailed()) || !h.tr.HasText("database is locked") {
		t.Fatal("加载失败应显示失败原因")
	}
	if h.tr.HasText(locales.ImportsEmpty()) || h.tr.HasText(locales.ImportsFilteredEmpty()) {
		t.Fatal("加载失败不能渲染成空态")
	}

	// 重试：修复后点「重试」得到数据
	h.fake.Set(samplePage(), nil)
	if err := h.tr.Click(locales.CommonRetry()); err != nil {
		t.Fatalf("点击重试：%v", err)
	}
	h.settle()
	if !h.tr.HasText("mygo 仓库") || h.tr.HasText(locales.CommonLoadFailed()) {
		t.Fatal("重试成功后应显示数据并清除错误")
	}
}

func TestEmptyQueueVersusFilteredEmpty(t *testing.T) {
	h := newHarness(t, services.ImportPage{Counts: map[string]int64{}}, nil)
	h.tr.Frame()
	if !h.tr.HasText(locales.CommonLoading()) {
		t.Fatal("首次加载结束前应显示加载中，而不是空态")
	}
	h.screen.Start()
	h.settle()
	if !h.tr.HasText(locales.ImportsEmpty()) {
		t.Fatal("队列为空应显示引导空态")
	}

	// 搜索后无结果：显示「当前筛选下没有任务」并给出查看全部的出口
	h.fake.Set(samplePage(), nil)
	h.screen.text = "不存在的关键词"
	h.tr.Frame() // View 发现条件变化 → 触发查询
	h.settle()
	if !h.tr.HasText(locales.ImportsFilteredEmpty()) {
		t.Fatal("筛选后无结果应显示筛选空态")
	}
	if err := h.tr.Click(locales.ImportsResetFilter()); err != nil {
		t.Fatalf("点击查看全部任务：%v", err)
	}
	h.settle()
	if !h.tr.HasText("mygo 仓库") {
		t.Fatal("清除筛选后应回到完整列表")
	}
}

func TestFilterChangeQueriesServerAndNarrowsRows(t *testing.T) {
	h := newHarness(t, samplePage(), nil)
	h.tr.Frame()
	h.screen.Start()
	h.settle()

	if err := h.tr.Click(locales.ImportsPanelFailed() + " 1"); err != nil {
		t.Fatalf("点击「失败」筛选：%v", err)
	}
	h.settle()
	qs := h.fake.Queries()
	if q := qs[len(qs)-1]; q.Status != services.ImportStatusFailed {
		t.Fatalf("筛选应带到查询里：%+v", q)
	}
	// 服务端回的 active 任务（processing）被客户端筛选挡掉
	if h.tr.HasText("mygo 仓库") || !h.tr.HasText("spec.pdf") {
		t.Fatal("「失败」筛选下应只剩失败任务")
	}
}

func TestStaleResponseIsDropped(t *testing.T) {
	h := newHarness(t, samplePage(), nil)
	h.tr.Frame()

	// 第一次查询卡住；第二次查询先返回；之后放开第一次——它的结果不能覆盖第二次
	release := h.fake.Hold()
	slow := make(chan struct{})
	go func() {
		h.screen.reload(query{filter: services.ImportFilterAll})
		close(slow)
	}()
	waitFor(t, func() bool { return len(h.fake.Queries()) == 1 })

	fresh := samplePage()
	fresh.Entries[0].DisplayName = "新数据"
	h.fake.Unhold()
	h.fake.Set(fresh, nil)
	h.screen.reload(query{filter: services.ImportFilterAll}) // 同步完成
	release()                                                // 旧查询返回的是 samplePage 的旧数据
	select {
	case <-slow:
	case <-time.After(3 * time.Second):
		t.Fatal("旧查询没有结束")
	}
	h.tr.Frame()
	if !h.tr.HasText("新数据") {
		t.Fatal("过期响应覆盖了新数据")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("等待条件超时")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestChangeEventRefreshes(t *testing.T) {
	h := newHarness(t, samplePage(), nil)
	h.tr.Frame()
	h.screen.Start()
	h.settle()

	updated := samplePage()
	updated.Active[0].DisplayName = "更新后的名字"
	h.fake.Set(updated, nil)
	if !h.fake.Fire() {
		t.Fatal("页面应已订阅 OnChange")
	}
	h.settle()
	if !h.tr.HasText("更新后的名字") {
		t.Fatal("收到任务变化事件后应刷新")
	}

	h.screen.Close()
	if h.fake.Subscribed() {
		t.Fatal("Close 应取消订阅")
	}
}

func TestCountsAndFiltersMatchRendererSemantics(t *testing.T) {
	p := samplePage()
	counts := countsFromPage(p)
	want := map[string]int64{"all": 4, "active": 1, "completed": 2, "degraded": 1, "duplicate": 0, "failed": 1, "canceled": 0}
	for k, v := range want {
		if counts[k] != v {
			t.Errorf("计数 %s = %d，期望 %d", k, counts[k], v)
		}
	}
	// 降级任务既属于 completed 也属于 degraded（有意重复计数：降级的任务同样是完成了的）
	deg := services.ImportTask{Status: "completed", Warning: "x"}
	if !matchesFilter(deg, "completed") || !matchesFilter(deg, "degraded") || matchesFilter(deg, "failed") {
		t.Error("降级任务的筛选归属不对")
	}
	// 搜索同时匹配原始输入：采集成功后显示名换成了标题，用手里的链接也要搜得到
	task := services.ImportTask{DisplayName: "真实标题", SourceInput: "https://example.com/x"}
	if !matchesQuery(task, "example.com") || matchesQuery(task, "nope") {
		t.Error("搜索应匹配显示名与原始输入")
	}
}

func TestMergeActiveFirstDeduplicates(t *testing.T) {
	a := []services.ImportTask{{ID: "1"}, {ID: "2"}}
	e := []services.ImportTask{{ID: "2"}, {ID: "3"}}
	got := mergeActiveFirst(a, e)
	if len(got) != 3 || got[0].ID != "1" || got[1].ID != "2" || got[2].ID != "3" {
		t.Fatalf("合并结果不对：%+v", got)
	}
}
