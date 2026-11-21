package library

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层文章工具侧栏背后的异步状态：SourceCommentsContext（评论读取与采集）、WebSourceVersions（版本读取与采用）、
// AiHandoffButton 的忙标记。条目：note（没有来源）、xhs（小红书笔记）、dy-trash（回收站里的抖音视频）、web（网页）、plain-web（另一篇网页）。

const (
	xhsURL = "https://www.xiaohongshu.com/explore/abc"
	dyURL  = "https://www.douyin.com/video/1"
)

func toolsHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	entry := func(id, title, itemType string, deleted int64) services.LibraryEntry {
		return services.LibraryEntry{
			ID: id, Title: title, ItemType: itemType, Status: services.LibraryStatusActive, DeletedAt: deleted,
			ReviewStatus: "clear", ReviewReasons: []string{}, UpdatedAt: 1000, CreatedAt: 1000, Tags: []services.LibraryTag{},
		}
	}
	h.svc.Seed(services.LibraryCounts{All: 5}, nil, nil)
	h.svc.SeedEntries([]services.LibraryEntry{
		entry("note", "手工笔记", services.LibraryTypeNote, 0),
		entry("xhs", "小红书笔记", services.LibraryTypeNote, 0),
		entry("dy-trash", "抖音视频", services.LibraryTypeNote, 2000),
		entry("web", "网页", services.LibraryTypeWebpage, 0),
		entry("web2", "另一篇网页", services.LibraryTypeWebpage, 0),
	})
	h.svc.SeedDetail("note", servicestest.DetailBody{Content: "手工正文"})
	h.svc.SeedDetail("xhs", servicestest.DetailBody{Content: "笔记正文", SourceURI: xhsURL})
	h.svc.SeedDetail("dy-trash", servicestest.DetailBody{Content: "视频", SourceURI: dyURL})
	h.svc.SeedDetail("web", servicestest.DetailBody{Content: "网页旧正文", SourceURI: "https://example.com/a"})
	h.svc.SeedDetail("web2", servicestest.DetailBody{Content: "网页二", SourceURI: "https://example.com/b"})
	h.store.Load(h.ctx)
	return h
}

func (h *harness) tools() ToolsView { return h.store.View().Detail.Tools }

func comment(id string, likes int64) services.LibrarySourceComment {
	return services.LibrarySourceComment{ID: "id-" + id, ItemID: "xhs", Platform: "xiaohongshu", ExternalID: id, AuthorName: "作者" + id, Content: "评论" + id, LikeCount: likes}
}

func callsWithPrefix(h *harness, prefix string) int {
	n := 0
	for _, c := range h.svc.Calls() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// waitUntil 轮询到条件成立（并发用例里后台动作落定要一点时间）。
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时：%s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// openAsync 在后台 Spawn 的并发用例里选中一条：等详情与工具侧栏都挂载好（评论读取另起后台，用 settled 再等）。
func (h *harness) openAsync(id string) {
	h.t.Helper()
	h.store.SelectItem(id)
	waitUntil(h.t, "详情与工具侧栏挂载 "+id, func() bool {
		d := h.detail()
		return d.Item != nil && d.Item.ID == id && !d.Loading && d.Tools.ItemID == id
	})
}

// settled 等评论读取落定：读取请求至少发过 n 次且读取中已熄灭。
func (h *harness) settled(id string, n int) {
	h.t.Helper()
	waitUntil(h.t, "评论读取落定 "+id, func() bool {
		return callsWithPrefix(h, "SourceComments:"+id) >= n && !h.tools().Comments.Reading
	})
}

// ---- 挂载与读取 ----

// 读到条目之后才挂载：评论只对抖音与小红书、没删除的条目读取；来源版本只对没删除的网页条目读取；别的条目一个请求也不发。
func TestToolsMountReadsOnlyWhatTheItemSupports(t *testing.T) {
	h := toolsHarness(t)
	h.svc.SeedSourceComments("xhs", []services.LibrarySourceComment{comment("c1", 5), comment("c2", 1)})
	h.svc.SeedWebVersions("web", services.LibraryWebVersions{Title: "网页", Content: "网页旧正文", ContentHash: "h0",
		Versions: []services.LibraryWebVersion{{ID: "v1", Complete: true, Kind: services.WebVersionRemote}}})

	h.open("xhs")
	if v := h.tools(); v.ItemID != "xhs" || !v.Comments.Supported || len(v.Comments.Comments) != 2 || v.Comments.Reading || v.Comments.LoadError != "" || v.Versions.Baseline != nil {
		t.Fatalf("小红书条目：%+v", v)
	}
	if callsWithPrefix(h, "WebVersions:") != 0 {
		t.Fatalf("非网页条目不该读版本：%v", h.svc.Calls())
	}

	h.open("note")
	if v := h.tools(); v.ItemID != "note" || v.Comments.Supported || len(v.Comments.Comments) != 0 {
		t.Fatalf("没有来源的条目不支持评论：%+v", v)
	}
	if callsWithPrefix(h, "SourceComments:note") != 0 {
		t.Fatalf("不支持的条目不该读评论：%v", h.svc.Calls())
	}

	h.open("dy-trash")
	if v := h.tools(); v.Comments.Supported || callsWithPrefix(h, "SourceComments:dy-trash") != 0 {
		t.Fatalf("回收站里的条目不开放评论：%+v %v", v, h.svc.Calls())
	}

	h.open("web")
	v := h.tools()
	if v.ItemID != "web" || v.Comments.Supported || v.Versions.Baseline == nil || len(v.Versions.Versions) != 1 || v.Versions.Baseline.ContentHash != "h0" || v.Versions.Error != "" {
		t.Fatalf("网页条目读到版本与基线：%+v", v)
	}
}

// 换条目与取消选中都让工具侧栏的状态整体清空（渲染层按条目重建）。
func TestToolsResetWhenTheItemChangesOrIsCleared(t *testing.T) {
	h := toolsHarness(t)
	h.svc.SeedSourceComments("xhs", []services.LibrarySourceComment{comment("c1", 5)})
	h.open("xhs")
	if len(h.tools().Comments.Comments) != 1 {
		t.Fatalf("前提：读到评论：%+v", h.tools())
	}
	h.store.SelectItem("")
	if v := h.tools(); v.ItemID != "" || v.Comments.Supported || len(v.Comments.Comments) != 0 {
		t.Fatalf("取消选中应清空：%+v", v)
	}
	h.open("note")
	if v := h.tools(); v.ItemID != "note" || len(v.Comments.Comments) != 0 {
		t.Fatalf("换条目应清空：%+v", v)
	}
}

// 评论读取失败：原因进 LoadError、读取中熄灭、后台失败留痕；重试读到之后错误清空。
func TestCommentsLoadFailureAndRetry(t *testing.T) {
	h := toolsHarness(t)
	h.svc.SeedSourceComments("xhs", []services.LibrarySourceComment{comment("c1", 5)})
	h.svc.FailOn("SourceComments", errors.New("库被锁了"))
	h.open("xhs")
	c := h.tools().Comments
	if c.LoadError != "库被锁了" || c.Reading || len(c.Comments) != 0 {
		t.Fatalf("读取失败：%+v", c)
	}
	if !h.logged("library.sourceComments", "库被锁了") {
		t.Fatalf("后台失败应留痕：%v", h.log.entries())
	}
	h.svc.FailOn("SourceComments", nil)
	h.store.RetryComments(h.ctx)
	if c = h.tools().Comments; c.LoadError != "" || c.Reading || len(c.Comments) != 1 {
		t.Fatalf("重试之后：%+v", c)
	}
}

// ---- 重新采集 ----

// 采集成功：列表换成采集之后的全部评论（按点赞降序），记下「试过」，采集中熄灭；请求带着所选档位。
func TestRefreshCommentsReplacesTheListAndRemembersTheAttempt(t *testing.T) {
	h := toolsHarness(t)
	h.svc.SeedSourceComments("xhs", []services.LibrarySourceComment{comment("c1", 5)})
	h.svc.SeedCapture("xhs", []services.LibrarySourceComment{comment("c2", 9)})
	h.open("xhs")
	h.store.RefreshComments(h.ctx, 50)
	c := h.tools().Comments
	if c.Loading || c.Error != "" || !c.Attempted || len(c.Comments) != 2 || c.Comments[0].ExternalID != "c2" {
		t.Fatalf("采集之后：%+v", c)
	}
	if calls := h.svc.RefreshCalls(); !slices.Equal(calls, []servicestest.RefreshCall{{ItemID: "xhs", Limit: 50}}) {
		t.Fatalf("请求：%+v", calls)
	}
}

// 采集失败：原因留在卡片（Error），弹一条带原因的提示，采集中熄灭，之后还能再采；成功后清掉上次的原因。
func TestRefreshCommentsFailureShowsTheReasonAndCanBeRetried(t *testing.T) {
	h := toolsHarness(t)
	h.open("xhs")
	h.svc.FailOn("RefreshSourceComments", errors.New("需要先登录"))
	h.store.RefreshComments(h.ctx, 20)
	c := h.tools().Comments
	if c.Error != "需要先登录" || c.Loading || !c.Attempted {
		t.Fatalf("失败：%+v", c)
	}
	toasts := h.queuedToasts()
	if len(toasts) != 1 || toasts[0].Message != locales.LibraryCommentsCaptureFailed() || toasts[0].Detail != "需要先登录" {
		t.Fatalf("提示：%+v", toasts)
	}
	if !h.logged("library.refreshComments", "需要先登录") {
		t.Fatalf("失败应留痕：%v", h.log.entries())
	}
	h.svc.FailOn("RefreshSourceComments", nil)
	h.svc.SeedCapture("xhs", []services.LibrarySourceComment{comment("c1", 1)})
	h.store.RefreshComments(h.ctx, 20)
	if c = h.tools().Comments; c.Error != "" || len(c.Comments) != 1 {
		t.Fatalf("再采一次应清掉上次的原因：%+v", c)
	}
}

// 不支持、读取失败时点采集什么也不做；采集在途时再点被挡住（渲染层 capturing ref）。
func TestRefreshCommentsIsGuarded(t *testing.T) {
	h := toolsHarness(t)
	h.open("note")
	h.store.RefreshComments(h.ctx, 20)
	if len(h.svc.RefreshCalls()) != 0 {
		t.Fatalf("不支持的条目不该采：%+v", h.svc.RefreshCalls())
	}

	h.svc.FailOn("SourceComments", errors.New("读失败"))
	h.open("xhs")
	h.store.RefreshComments(h.ctx, 20)
	if len(h.svc.RefreshCalls()) != 0 {
		t.Fatalf("读取失败时不该采：%+v", h.svc.RefreshCalls())
	}
	h.svc.FailOn("SourceComments", nil)

	h.store.Spawn = func(fn func()) { go fn() }
	before := callsWithPrefix(h, "SourceComments:xhs")
	h.openAsync("xhs")
	h.settled("xhs", before+1)
	release := h.svc.HoldRefreshComments()
	h.svc.SeedCapture("xhs", []services.LibrarySourceComment{comment("c1", 1)})
	h.store.RefreshComments(h.ctx, 10)
	if c := h.tools().Comments; !c.Loading {
		t.Fatalf("采集中应亮：%+v", c)
	}
	h.store.RefreshComments(h.ctx, 10)
	release()
	waitUntil(t, "采集落定", func() bool { return !h.tools().Comments.Loading })
	if n := len(h.svc.RefreshCalls()); n != 1 {
		t.Fatalf("在途时再点应被挡住，实际请求 %d 次", n)
	}
}

// 采集在途时换了条目：旧一代的结果整个丢弃，不覆盖新条目、不弹提示，也不把新一代的「采集中」标记清掉。
func TestRefreshCommentsResultForAReplacedItemIsDropped(t *testing.T) {
	h := toolsHarness(t)
	h.store.Spawn = func(fn func()) { go fn() }
	h.svc.SeedWebVersions("web", services.LibraryWebVersions{Title: "网页"})
	h.openAsync("xhs")
	h.settled("xhs", 1)
	h.svc.FailOn("RefreshSourceComments", errors.New("旧条目的失败"))
	release := h.svc.HoldRefreshComments()
	h.store.RefreshComments(h.ctx, 20)
	h.openAsync("web")
	release()
	waitUntil(t, "旧请求到达", func() bool { return len(h.svc.RefreshCalls()) == 1 })
	time.Sleep(20 * time.Millisecond)
	if v := h.tools(); v.ItemID != "web" || v.Comments.Error != "" || v.Comments.Loading {
		t.Fatalf("旧结果不该落到新条目上：%+v", v)
	}
	if len(h.queuedToasts()) != 0 {
		t.Fatalf("旧条目的失败不该弹提示：%+v", h.queuedToasts())
	}
}

// ---- 网页来源版本 ----

// 读取失败：原因进 Error（空原因换成固定文案），保留已读到的数据；再读成功清掉错误。
func TestWebVersionsLoadFailureKeepsWhatWasRead(t *testing.T) {
	h := toolsHarness(t)
	h.svc.SeedWebVersions("web", services.LibraryWebVersions{Title: "网页", ContentHash: "h0", Versions: []services.LibraryWebVersion{{ID: "v1", Complete: true}}})
	h.open("web")
	h.svc.FailOn("WebVersions", errors.New("服务暂不可用"))
	h.store.LoadWebVersions(h.ctx)
	v := h.tools().Versions
	if v.Error != "服务暂不可用" || v.Baseline == nil || len(v.Versions) != 1 {
		t.Fatalf("失败时保留已读到的：%+v", v)
	}
	if !h.logged("library.webVersions", "服务暂不可用") {
		t.Fatalf("后台失败应留痕：%v", h.log.entries())
	}
	h.svc.FailOn("WebVersions", errors.New(""))
	h.store.LoadWebVersions(h.ctx)
	if v = h.tools().Versions; v.Error != locales.WebCaptureLoadVersionsError() {
		t.Fatalf("空原因应换成固定文案：%q", v.Error)
	}
	h.svc.FailOn("WebVersions", nil)
	h.store.LoadWebVersions(h.ctx)
	if v = h.tools().Versions; v.Error != "" || len(v.Versions) != 1 {
		t.Fatalf("再读成功：%+v", v)
	}
}

func adoptable(h *harness) {
	h.svc.SeedWebVersions("web", services.LibraryWebVersions{Title: "网页", Content: "网页旧正文", ContentHash: "h0", PendingVersion: true,
		Versions: []services.LibraryWebVersion{
			{ID: "v2", Title: "新", Markdown: "网页新正文", Complete: true, Kind: services.WebVersionRemote},
			{ID: "v1", Title: "残", Markdown: "残缺", Complete: false, Kind: services.WebVersionRemote},
		}})
}

// 采用：带着比较时的正文哈希与标题；成功后重新读取条目（正文换成版本的）、工具侧栏随之重新挂载（基线是采用之后的）。
func TestAdoptWebVersionSendsTheBaselineAndReloadsTheItem(t *testing.T) {
	h := toolsHarness(t)
	adoptable(h)
	h.open("web")
	getsBefore := len(h.svc.Gets())
	if !h.store.AdoptWebVersion(h.ctx, "v2") {
		t.Fatalf("应成功：%+v", h.queuedToasts())
	}
	want := services.LibraryAdoptWebVersionInput{ItemID: "web", VersionID: "v2", ExpectedContentHash: "h0", ExpectedTitle: "网页"}
	if adopts := h.svc.Adopts(); !slices.Equal(adopts, []services.LibraryAdoptWebVersionInput{want}) {
		t.Fatalf("请求：%+v", adopts)
	}
	if len(h.svc.Gets()) != getsBefore+1 {
		t.Fatalf("采用后应重新读取条目：%v", h.svc.Gets())
	}
	d := h.detail()
	if d.Item == nil || d.Item.Content != "网页新正文" {
		t.Fatalf("正文应是版本的：%+v", d.Item)
	}
	if b := d.Tools.Versions.Baseline; b == nil || b.Content != "网页新正文" || b.PendingVersion {
		t.Fatalf("工具侧栏应带着新基线重新挂载：%+v", d.Tools.Versions)
	}
}

// 采用失败走变更失败通道：提示标题是「采用原文版本」、原因进详情；不重新读取条目。基线过期时处理器拒绝。
func TestAdoptWebVersionFailureIsReportedAndNothingReloads(t *testing.T) {
	h := toolsHarness(t)
	adoptable(h)
	h.open("web")
	h.svc.FailOn("AdoptWebVersion", errors.New("正文或标题已在比较期间发生变化"))
	getsBefore := len(h.svc.Gets())
	if h.store.AdoptWebVersion(h.ctx, "v2") {
		t.Fatal("应失败")
	}
	toasts := h.queuedToasts()
	if len(toasts) != 1 || !strings.Contains(toasts[0].Message, locales.WebCaptureAdoptVersion()) || toasts[0].Detail != "正文或标题已在比较期间发生变化" {
		t.Fatalf("提示：%+v", toasts)
	}
	if len(h.svc.Gets()) != getsBefore {
		t.Fatalf("失败时不该重新读取条目：%v", h.svc.Gets())
	}
	if h.store.AdoptWebVersion(h.ctx, "ghost") {
		t.Fatal("没有这个版本不该有任何动作")
	}
	if n := len(h.svc.Adopts()); n != 1 {
		t.Fatalf("没有这个版本不该发请求：%d", n)
	}
}

// 当前草稿落不了盘：不采用，但照渲染层的写法仍算「成功」并重新选中——重新选中本身也要先落盘，所以条目没动。
func TestAdoptWebVersionDoesNothingWhenTheDraftCannotBeSaved(t *testing.T) {
	h := toolsHarness(t)
	adoptable(h)
	h.store.AutoSave = func() bool { return false }
	h.open("web")
	h.edit(patchTitle("改了标题"))
	h.svc.FailOn("SaveDraft", errors.New("磁盘满了"))
	h.store.AdoptWebVersion(h.ctx, "v2")
	if n := len(h.svc.Adopts()); n != 0 {
		t.Fatalf("草稿没落盘不该采用：%d", n)
	}
	if d := h.detail(); d.Item == nil || d.Item.Content != "网页旧正文" || d.Item.Title != "改了标题" {
		t.Fatalf("条目不该动：%+v", d.Item)
	}
}

// ---- 交接稿忙标记 ----

// 任务在途时忙标记为真、再点被忽略；落定后熄灭。换了条目：旧任务落定不影响新条目的标记。
func TestRunHandoffMarksBusyAndIgnoresRepeats(t *testing.T) {
	h := toolsHarness(t)
	h.store.Spawn = func(fn func()) { go fn() }
	h.openAsync("note")
	gate := make(chan struct{})
	started := make(chan struct{})
	h.store.RunHandoff(func() { close(started); <-gate })
	<-started
	if !h.tools().HandoffBusy {
		t.Fatal("在途时应忙")
	}
	ran := false
	h.store.RunHandoff(func() { ran = true })
	close(gate)
	waitUntil(t, "任务落定", func() bool { return !h.tools().HandoffBusy })
	if ran {
		t.Fatal("在途时再点应被忽略")
	}

	gate2, started2 := make(chan struct{}), make(chan struct{})
	h.store.RunHandoff(func() { close(started2); <-gate2 })
	<-started2
	h.openAsync("xhs")
	if h.tools().HandoffBusy {
		t.Fatal("换条目之后新条目不忙")
	}
	h.store.RunHandoff(func() {}) // 新条目自己的任务
	waitUntil(t, "新条目的任务落定", func() bool { return !h.tools().HandoffBusy })
	close(gate2)
	time.Sleep(20 * time.Millisecond)
	if h.tools().HandoffBusy {
		t.Fatal("旧任务落定不该改新条目的标记")
	}
}
