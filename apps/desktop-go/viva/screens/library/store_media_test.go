package library

import (
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 use-media-actions.ts 里 useTranscriptActions（转写、区分说话人、文字稿排版与长稿确认）、
// useMediaSummaryAction、useForumDiscussionRefreshAction 背后的状态（store_media.go）。
// 条目：aud（音频，没有文字稿）、talk（音频，有短文字稿）、other（普通笔记，切条目用）、
// forum / forum-nosum（Linux.do 论坛帖，有 / 没有讨论总结）、forum-appinn、forum-2libra（另外两个支持刷新的站点）、
// forum-v2ex（不支持刷新的论坛）、forum-nosrc（没有来源的论坛）、aud-linuxdo（来源是 Linux.do 的音频）。

const (
	forumWithSummary = "## 讨论总结\n\n旧总结\n\n## 正文\n\n主楼\n\n## 讨论\n\n回复"
	forumNoSummary   = "## 正文\n\n主楼\n\n## 讨论\n\n回复"
)

func mediaHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	entry := func(id, title, itemType string) services.LibraryEntry {
		return services.LibraryEntry{
			ID: id, Title: title, ItemType: itemType, Status: services.LibraryStatusActive,
			ReviewStatus: "clear", ReviewReasons: []string{}, UpdatedAt: 1000, CreatedAt: 1000, Tags: []services.LibraryTag{},
		}
	}
	h.svc.Seed(services.LibraryCounts{All: 11}, nil, nil)
	h.svc.SeedEntries([]services.LibraryEntry{
		entry("aud", "录音", services.LibraryTypeAudio),
		entry("talk", "访谈", services.LibraryTypeAudio),
		entry("other", "笔记", services.LibraryTypeNote),
		entry("forum", "帖子", services.LibraryTypeForum),
		entry("forum-nosum", "没总结的帖子", services.LibraryTypeForum),
		entry("forum-appinn", "小众帖", services.LibraryTypeForum),
		entry("forum-2libra", "二库帖", services.LibraryTypeForum),
		entry("forum-v2ex", "V2EX 帖", services.LibraryTypeForum),
		entry("forum-nosrc", "无来源帖", services.LibraryTypeForum),
		entry("aud-linuxdo", "来源是论坛的音频", services.LibraryTypeAudio),
	})
	h.svc.SeedDetail("aud", servicestest.DetailBody{Content: "音频正文"})
	h.svc.SeedDetail("talk", servicestest.DetailBody{Content: "音频正文", Transcript: "短文字稿"})
	h.svc.SeedDetail("other", servicestest.DetailBody{Content: "笔记正文"})
	h.svc.SeedDetail("forum", servicestest.DetailBody{Content: forumWithSummary, SourceURI: "https://linux.do/t/topic/12345"})
	h.svc.SeedDetail("forum-nosum", servicestest.DetailBody{Content: forumNoSummary, SourceURI: "https://linux.do/t/topic/12346"})
	h.svc.SeedDetail("forum-appinn", servicestest.DetailBody{Content: forumNoSummary, SourceURI: "https://meta.appinn.net/t/topic/123"})
	h.svc.SeedDetail("forum-2libra", servicestest.DetailBody{Content: forumNoSummary, SourceURI: "https://2libra.com/post/tech/AbC_123"})
	h.svc.SeedDetail("forum-v2ex", servicestest.DetailBody{Content: forumNoSummary, SourceURI: "https://www.v2ex.com/t/123456"})
	h.svc.SeedDetail("forum-nosrc", servicestest.DetailBody{Content: forumNoSummary})
	h.svc.SeedDetail("aud-linuxdo", servicestest.DetailBody{Content: "音频正文", SourceURI: "https://linux.do/t/topic/12345"})
	h.store.Load(h.ctx)
	return h
}

func (h *harness) media() MediaView { return h.store.View().Detail.Media }

// setTranscript 换掉一条的文字稿（正文与来源不变）；要在选中它之前调用。
func (h *harness) setTranscript(id, text string) {
	h.svc.SeedDetail(id, servicestest.DetailBody{Content: "音频正文", Transcript: text})
}

// writtenItem 是动作成功后服务端回的完整条目：标题、正文、文字稿都换成新的，更新时间 5000。
func writtenItem(id, itemType string) services.LibraryItem {
	item := services.LibraryItem{
		ID: id, Title: "新标题", Content: "服务端新正文", Transcript: "新文字稿", ItemType: itemType,
		Status: services.LibraryStatusActive, UpdatedAt: 5000, Tags: []services.LibraryTag{},
	}
	if itemType == services.LibraryTypeForum {
		item.SourceURI = "https://linux.do/t/topic/12345"
	}
	return item
}

func okResult(id, itemType string) services.LibraryMediaActionResult {
	item := writtenItem(id, itemType)
	return services.LibraryMediaActionResult{Item: &item}
}

func mediaCalls(h *harness) []servicestest.MediaCall { return h.svc.MediaCalls() }

// goSpawn 让后台动作真的并发执行（默认的同步 Spawn 做不出「在途期间用户又改了」）。
func goSpawn(h *harness) { h.store.Spawn = func(fn func()) { go fn() } }

func syncSpawn(h *harness) { h.store.Spawn = func(fn func()) { fn() } }

// mediaAction 是一项会发起后台请求的媒体动作。
type mediaAction struct {
	name, id, itemType string
	start              func(h *harness)
	busy               func(m MediaView) bool
	mark               func(m *MediaView) // 把这个动作的忙标记置真（构造「新条目自己有一次在途」）
	method             string
	seed               func(h *harness, res services.LibraryMediaActionResult, err error)
	logAction          string
	flushes            bool // 发起前先把未落盘的编辑落盘
	done               string
	notConfigured      string // 空表示这个动作没有「未配置模型」这一支
	failTitle          func(msg string) string
}

func mediaActions() []mediaAction {
	return []mediaAction{
		{
			name: "transcribe", id: "aud", itemType: services.LibraryTypeAudio,
			start:     func(h *harness) { h.store.Transcribe(h.ctx, false) },
			busy:      func(m MediaView) bool { return m.RunningAction != "" },
			mark:      func(m *MediaView) { m.RunningAction = MediaActionTranscribe },
			method:    servicestest.MediaMethodTranscribe,
			seed:      func(h *harness, res services.LibraryMediaActionResult, err error) { h.svc.SeedTranscribe(res, err) },
			logAction: "library.transcribe", flushes: true,
			done: locales.LibraryTranscribeDone(), notConfigured: locales.LibraryTranscribeNotConfigured(),
			failTitle: func(msg string) string { return locales.LibraryTranscribeFailed(msg) },
		},
		{
			name: "format", id: "talk", itemType: services.LibraryTypeAudio,
			start:  func(h *harness) { h.store.FormatTranscript(h.ctx) },
			busy:   func(m MediaView) bool { return m.Formatting },
			mark:   func(m *MediaView) { m.Formatting = true },
			method: servicestest.MediaMethodFormat,
			seed: func(h *harness, res services.LibraryMediaActionResult, err error) {
				h.svc.SeedFormatTranscript(res, err)
			},
			logAction: "library.formatTranscript",
			done:      locales.LibraryTranscriptFormatDone(), notConfigured: locales.LibraryTranscriptFormatNotConfigured(),
			failTitle: func(string) string { return locales.LibraryTranscriptFormatFailed() },
		},
		{
			name: "summarize", id: "talk", itemType: services.LibraryTypeAudio,
			start:     func(h *harness) { h.store.SummarizeMedia(h.ctx) },
			busy:      func(m MediaView) bool { return m.Summarizing },
			mark:      func(m *MediaView) { m.Summarizing = true },
			method:    servicestest.MediaMethodSummarize,
			seed:      func(h *harness, res services.LibraryMediaActionResult, err error) { h.svc.SeedSummarizeMedia(res, err) },
			logAction: "library.summarizeMedia", flushes: true,
			done: locales.LibraryMediaSummaryDone(), notConfigured: locales.LibraryMediaSummaryNotConfigured(),
			failTitle: func(msg string) string { return locales.LibraryMediaSummaryFailed(msg) },
		},
		{
			name: "refresh", id: "forum", itemType: services.LibraryTypeForum,
			start:     func(h *harness) { h.store.RefreshForumDiscussion(h.ctx) },
			busy:      func(m MediaView) bool { return m.RefreshingDiscussion },
			mark:      func(m *MediaView) { m.RefreshingDiscussion = true },
			method:    servicestest.MediaMethodRefresh,
			seed:      func(h *harness, res services.LibraryMediaActionResult, err error) { h.svc.SeedRefreshForum(res, err) },
			logAction: "library.refreshForumDiscussion", flushes: true,
			done:      locales.LibraryForumDiscussionRefreshedSummaryStale(), // forum 带讨论总结
			failTitle: func(msg string) string { return locales.LibraryForumDiscussionRefreshFailed(msg) },
		},
	}
}

func (a mediaAction) calls(h *harness) int { return callsWithPrefix(h, a.method) }

// forEachMediaAction 对每个动作各跑一个子用例（选中它的条目）。
func forEachMediaAction(t *testing.T, body func(t *testing.T, h *harness, a mediaAction)) {
	for _, a := range mediaActions() {
		t.Run(a.name, func(t *testing.T) {
			h := mediaHarness(t)
			h.open(a.id)
			body(t, h, a)
		})
	}
}

// ---- 四个动作共有的行为 ----

// 正常：发请求、写回的条目换进详情、列表行跟上、成功提示；忙标记放下。
func TestMediaActionsWriteBackAndToast(t *testing.T) {
	forEachMediaAction(t, func(t *testing.T, h *harness, a mediaAction) {
		a.seed(h, okResult(a.id, a.itemType), nil)
		a.start(h)
		if a.calls(h) != 1 {
			t.Fatalf("应恰好发一次请求：%v", h.svc.Calls())
		}
		d := h.detail()
		if d.Item == nil || d.Item.Title != "新标题" || d.Item.Content != "服务端新正文" || d.Item.Transcript != "新文字稿" {
			t.Fatalf("详情应换成写回的版本：%+v", d.Item)
		}
		if row := listRow(h, a.id); row.UpdatedAt != 5000 || row.Title != "新标题" {
			t.Fatalf("列表行应跟上：%+v", row)
		}
		if toast := onlyToast(t, h); toast.Kind != kit.ToastSuccess || toast.Message != a.done || toast.Detail != "" {
			t.Fatalf("提示：%+v", toast)
		}
		if a.busy(d.Media) || d.Media != (MediaView{}) {
			t.Fatalf("忙标记应放下：%+v", d.Media)
		}
	})
}

// 在途期间同一个入口再点什么也不做；落定之后可以再发起。
func TestMediaActionsRejectReentry(t *testing.T) {
	forEachMediaAction(t, func(t *testing.T, h *harness, a mediaAction) {
		a.seed(h, okResult(a.id, a.itemType), nil) // 写回的是完整条目（刷新讨论写回后仍是同一个来源，才能再刷新）
		q := &spawnQueue{}
		h.store.Spawn = q.Spawn
		a.start(h)
		if !a.busy(h.media()) {
			t.Fatalf("发起后应置忙：%+v", h.media())
		}
		a.start(h)
		if len(q.queued) != 1 {
			t.Fatalf("在途时再点不该再排后台动作：%d", len(q.queued))
		}
		q.Run()
		if a.calls(h) != 1 || a.busy(h.media()) {
			t.Fatalf("只发一次，落定后放下：%d %+v", a.calls(h), h.media())
		}
		a.start(h)
		q.Run()
		if a.calls(h) != 2 {
			t.Fatalf("落定之后应能再发起：%d", a.calls(h))
		}
	})
}

// 发起时就唤醒界面（置忙后的转圈要立刻画出来），落定后再唤醒一次；请求在后台动作里才发。
func TestMediaActionsWakeTheUI(t *testing.T) {
	forEachMediaAction(t, func(t *testing.T, h *harness, a mediaAction) {
		q := &spawnQueue{}
		h.store.Spawn = q.Spawn
		before := h.wakeCount()
		a.start(h)
		if !a.busy(h.media()) || h.wakeCount() <= before || a.calls(h) != 0 {
			t.Fatalf("发起时应置忙并唤醒，请求还没发：置忙 %v，唤醒 %d→%d，请求 %d", a.busy(h.media()), before, h.wakeCount(), a.calls(h))
		}
		before = h.wakeCount()
		q.Run()
		if a.busy(h.media()) || h.wakeCount() <= before {
			t.Fatalf("落定后应放下并唤醒：置忙 %v，唤醒 %d→%d", a.busy(h.media()), before, h.wakeCount())
		}
	})
}

// 没有详情、列表选中项与详情对不上时什么也不做。
func TestMediaActionsRequireTheShownDetail(t *testing.T) {
	for _, a := range mediaActions() {
		t.Run(a.name+"/无详情", func(t *testing.T) {
			h := mediaHarness(t)
			a.start(h)
			if a.calls(h) != 0 || a.busy(h.media()) || len(h.queuedToasts()) != 0 {
				t.Fatalf("没有详情不该发起：%v %+v", h.svc.Calls(), h.media())
			}
		})
		t.Run(a.name+"/选中项对不上", func(t *testing.T) {
			h := mediaHarness(t)
			h.open(a.id)
			h.desyncSelected("别的条目")
			a.start(h)
			if a.calls(h) != 0 || a.busy(h.media()) {
				t.Fatalf("选中项对不上不该发起：%v %+v", h.svc.Calls(), h.media())
			}
		})
	}
}

// 失败（调用本身出错、回复不成功、回复没给原因）：错误提示带原因，原文进可复制的详情，留一条日志，忙标记放下，可重试。
func TestMediaActionsFailureShowsErrorAndLogs(t *testing.T) {
	cases := []struct {
		name string
		res  services.LibraryMediaActionResult
		err  error
		msg  string
	}{
		{"调用出错", services.LibraryMediaActionResult{}, errors.New("网络断开"), "网络断开"},
		{"回复不成功", services.LibraryMediaActionResult{Error: "模型返回了空结果"}, nil, "模型返回了空结果"},
		{"回复没给原因", services.LibraryMediaActionResult{}, nil, locales.CommonUnknownError()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachMediaAction(t, func(t *testing.T, h *harness, a mediaAction) {
				jumps := settingsJumps(h)
				a.seed(h, c.res, c.err)
				a.start(h)
				toast := onlyToast(t, h)
				if toast.Kind != kit.ToastError || toast.Message != a.failTitle(c.msg) || toast.Detail != c.msg {
					t.Fatalf("提示：%+v，期望标题 %q 详情 %q", toast, a.failTitle(c.msg), c.msg)
				}
				if got := h.loggedUnder(a.logAction); len(got) != 1 || !strings.Contains(got[0], c.msg) {
					t.Fatalf("应恰好一条动作名为 %s 的日志：%v", a.logAction, h.log.entries())
				}
				if jumps.Load() != 0 || a.busy(h.media()) {
					t.Fatalf("不该跳设置页，忙标记应放下：%d %+v", jumps.Load(), h.media())
				}
				if d := h.detail(); d.Item.Title == "新标题" {
					t.Fatalf("失败不该改详情：%+v", d.Item)
				}
				a.seed(h, okResult(a.id, a.itemType), nil)
				a.start(h)
				if a.calls(h) != 2 {
					t.Fatalf("失败后应能重试：%d", a.calls(h))
				}
			})
		})
	}
}

// 未配置模型：提示去配置并跳设置页的 AI 分区，不留失败日志（用户该做的是去配置，不是故障）。刷新讨论没有这一支。
func TestMediaActionsNotConfiguredJumpsToSettings(t *testing.T) {
	forEachMediaAction(t, func(t *testing.T, h *harness, a mediaAction) {
		if a.notConfigured == "" {
			t.Skip("这个动作没有未配置模型的分支")
		}
		jumps := settingsJumps(h)
		a.seed(h, services.LibraryMediaActionResult{NotConfigured: true, Error: "no model"}, nil)
		a.start(h)
		if toast := onlyToast(t, h); toast.Kind != kit.ToastError || toast.Message != a.notConfigured || toast.Detail != "" {
			t.Fatalf("提示：%+v", toast)
		}
		if jumps.Load() != 1 || len(h.log.entries()) != 0 || a.busy(h.media()) {
			t.Fatalf("应跳一次设置页、不留日志、放下忙标记：%d %v %+v", jumps.Load(), h.log.entries(), h.media())
		}
	})
}

// 没挂跳转回调时（OnOpenSettingsAI 为空）未配置模型照样提示，不崩溃。
func TestMediaNotConfiguredWithoutSettingsHook(t *testing.T) {
	h := mediaHarness(t)
	h.open("aud")
	h.svc.SeedTranscribe(services.LibraryMediaActionResult{NotConfigured: true}, nil)
	h.store.Transcribe(h.ctx, false)
	if toast := onlyToast(t, h); toast.Message != locales.LibraryTranscribeNotConfigured() {
		t.Fatalf("提示：%+v", toast)
	}
}

// 刷新讨论没有「未配置模型」：回复里标了 NotConfigured 也按失败处理，不跳设置页。
func TestRefreshForumNotConfiguredIsJustAFailure(t *testing.T) {
	h := mediaHarness(t)
	jumps := settingsJumps(h)
	h.open("forum")
	h.svc.SeedRefreshForum(services.LibraryMediaActionResult{NotConfigured: true, Error: "站点拒绝访问"}, nil)
	h.store.RefreshForumDiscussion(h.ctx)
	toast := onlyToast(t, h)
	if toast.Kind != kit.ToastError || toast.Message != locales.LibraryForumDiscussionRefreshFailed("站点拒绝访问") || jumps.Load() != 0 {
		t.Fatalf("提示 %+v，跳转 %d", toast, jumps.Load())
	}
}

// 先落盘再请求：发起前把未落盘的编辑存进服务端（主进程基于库中正文写回）；落盘失败就收手——不发请求、不弹提示、忙标记放下。
func TestMediaActionsFlushPendingEditsFirst(t *testing.T) {
	forEachMediaAction(t, func(t *testing.T, h *harness, a mediaAction) {
		if !a.flushes {
			t.Skip("这个动作不先落盘")
		}
		a.seed(h, okResult(a.id, a.itemType), nil)
		h.edit(patchTitle("先存的标题"))
		a.start(h)
		calls := h.svc.Calls()
		save := slices.Index(calls, "SaveDraft:"+a.id)
		req := slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, a.method) })
		if save < 0 || req < 0 || save > req {
			t.Fatalf("应先落盘再请求：%v", calls)
		}
		if now, _ := h.svc.ItemNow(a.id); now.Title != "先存的标题" {
			t.Fatalf("发请求前草稿应已落盘：%+v", now)
		}
	})
}

func TestMediaActionsStopWhenFlushFails(t *testing.T) {
	forEachMediaAction(t, func(t *testing.T, h *harness, a mediaAction) {
		if !a.flushes {
			t.Skip("这个动作不先落盘")
		}
		jumps := settingsJumps(h)
		h.edit(patchContent("没存下的正文"))
		h.svc.FailOn("SaveDraft", errors.New("磁盘已满"))
		before := h.wakeCount()
		a.start(h)
		d := h.detail()
		if a.calls(h) != 0 || a.busy(d.Media) || len(h.queuedToasts()) != 0 || jumps.Load() != 0 {
			t.Fatalf("落盘失败不该发请求、弹提示：请求 %d，%+v，提示 %+v", a.calls(h), d.Media, h.queuedToasts())
		}
		if !d.Unsaved || d.SaveError == "" || d.Item.Content != "没存下的正文" {
			t.Fatalf("改动应留在待保存里、原因在详情头部：%+v", d)
		}
		if h.wakeCount() <= before {
			t.Fatal("收手后也要唤醒界面，放下忙标记")
		}
		h.svc.FailOn("SaveDraft", nil)
		a.start(h)
		if a.calls(h) != 1 {
			t.Fatalf("放下忙标记后应能重试：%d", a.calls(h))
		}
	})
}

// ---- 换条目的竞态 ----

// 在途期间换了条目：写回照样进服务端、列表行跟上，但不碰新条目的详情、不弹提示；新条目自己的忙标记不被旧请求放下。
func TestMediaActionsSwitchingItemOnlyUpdatesTheRow(t *testing.T) {
	forEachMediaAction(t, func(t *testing.T, h *harness, a mediaAction) {
		a.seed(h, okResult(a.id, a.itemType), nil)
		q := &spawnQueue{}
		h.store.Spawn = q.Spawn
		a.start(h)
		syncSpawn(h)
		h.open("other")
		if h.media() != (MediaView{}) {
			t.Fatalf("换条目应清空媒体状态：%+v", h.media())
		}
		h.store.mu.Lock() // 新条目自己有一次同类动作在途
		a.mark(&h.store.view.Detail.Media)
		h.store.mu.Unlock()

		q.Run()
		d := h.detail()
		if d.Item.ID != "other" || d.Item.Title != "笔记" || d.Item.Content != "笔记正文" {
			t.Fatalf("新条目的详情不该被旧请求改动：%+v", d.Item)
		}
		if !a.busy(d.Media) {
			t.Fatalf("旧请求落定不该放下新条目的忙标记：%+v", d.Media)
		}
		if row := listRow(h, a.id); row.UpdatedAt != 5000 || row.Title != "新标题" {
			t.Fatalf("列表行仍应跟上：%+v", row)
		}
		if len(h.queuedToasts()) != 0 {
			t.Fatalf("过期的成功不该弹提示：%+v", h.queuedToasts())
		}
		if a.calls(h) != 1 {
			t.Fatalf("旧请求照常发出：%v", h.svc.Calls())
		}
	})
}

// 离开又回到同一条：旧请求属于上一次挂载（代号已变），回来时详情是新取的，不该被旧请求写回的版本换掉，
// 也不该放下新一次挂载的忙标记；列表行照样跟上。
func TestMediaActionsLeavingAndReturningToTheSameItemIgnoresTheOldRequest(t *testing.T) {
	forEachMediaAction(t, func(t *testing.T, h *harness, a mediaAction) {
		a.seed(h, okResult(a.id, a.itemType), nil)
		q := &spawnQueue{}
		h.store.Spawn = q.Spawn
		a.start(h)
		syncSpawn(h)
		h.open("other")
		h.open(a.id)
		before := h.detail().Item
		if before == nil || before.ID != a.id || before.Title == "新标题" {
			t.Fatalf("前提不成立：回到这一条之后详情应是新取的原版：%+v", before)
		}
		h.store.mu.Lock() // 新一次挂载自己有一次同类动作在途
		a.mark(&h.store.view.Detail.Media)
		h.store.mu.Unlock()

		q.Run()
		d := h.detail()
		if d.Item.Title != before.Title || d.Item.Content != before.Content || d.Item.UpdatedAt != before.UpdatedAt {
			t.Fatalf("旧请求写回的版本不该换掉新取的详情：%+v → %+v", before, d.Item)
		}
		if !a.busy(d.Media) {
			t.Fatalf("旧请求落定不该放下新一次挂载的忙标记：%+v", d.Media)
		}
		if row := listRow(h, a.id); row.UpdatedAt != 5000 || row.Title != "新标题" {
			t.Fatalf("列表行仍应跟上：%+v", row)
		}
		if len(h.queuedToasts()) != 0 {
			t.Fatalf("过期的成功不该弹提示：%+v", h.queuedToasts())
		}
	})
}

// 长稿门槛与切块大小要和渲染层 packages/shared/constants/transcript.ts 一致（那边是 50_000 与 1600）：
// 其余测试都用常量本身造夹具，常量被改了它们会跟着变，所以这里把数值钉死。
func TestTranscriptFormatConstantsMatchTheSharedConstants(t *testing.T) {
	if transcriptFormatLongChars != 50_000 || transcriptFormatChunkChars != 1600 {
		t.Fatalf("长稿门槛应是 50000、切块应是 1600（packages/shared/constants/transcript.ts），实际 %d、%d",
			transcriptFormatLongChars, transcriptFormatChunkChars)
	}
}

// 过期的失败只留日志：不弹提示；过期的未配置模型只静默（不提示、不跳设置页、不留日志）。
func TestMediaActionsStaleFailureOnlyLogs(t *testing.T) {
	stale := func(t *testing.T, a mediaAction, res services.LibraryMediaActionResult, err error) (*harness, *atomic.Int32) {
		h := mediaHarness(t)
		jumps := settingsJumps(h)
		h.open(a.id)
		a.seed(h, res, err)
		q := &spawnQueue{}
		h.store.Spawn = q.Spawn
		a.start(h)
		syncSpawn(h)
		h.open("other")
		q.Run()
		return h, jumps
	}
	for _, a := range mediaActions() {
		t.Run(a.name+"/失败", func(t *testing.T) {
			h, jumps := stale(t, a, services.LibraryMediaActionResult{}, errors.New("网络断开"))
			if len(h.queuedToasts()) != 0 || jumps.Load() != 0 || len(h.loggedUnder(a.logAction)) != 1 {
				t.Fatalf("过期的失败只留日志：提示 %+v，跳转 %d，日志 %v", h.queuedToasts(), jumps.Load(), h.log.entries())
			}
		})
		if a.notConfigured == "" {
			continue
		}
		t.Run(a.name+"/未配置模型", func(t *testing.T) {
			h, jumps := stale(t, a, services.LibraryMediaActionResult{NotConfigured: true}, nil)
			if len(h.queuedToasts()) != 0 || jumps.Load() != 0 || len(h.log.entries()) != 0 {
				t.Fatalf("过期的未配置模型只静默：提示 %+v，跳转 %d，日志 %v", h.queuedToasts(), jumps.Load(), h.log.entries())
			}
		})
	}
}

// 清掉详情（筛选把选中项挤出列表）也算换代：旧请求回来不再写进详情、不弹提示。
func TestMediaActionsClearingTheDetailDropsStaleOutcome(t *testing.T) {
	forEachMediaAction(t, func(t *testing.T, h *harness, a mediaAction) {
		a.seed(h, okResult(a.id, a.itemType), nil)
		q := &spawnQueue{}
		h.store.Spawn = q.Spawn
		a.start(h)
		h.store.mu.Lock()
		h.store.clearDetailLocked()
		h.store.mu.Unlock()
		q.Run()
		if d := h.detail(); d.Item != nil || d.Media != (MediaView{}) || len(h.queuedToasts()) != 0 {
			t.Fatalf("清详情之后旧请求不该写回：%+v %+v", d.Item, h.queuedToasts())
		}
	})
}

// ---- 写回时的正文基线 ----

// 在途期间用户只改了标题：没有未落盘的正文改动，正文基线同步成服务端版本（否则之后保存标题会因旧基线被判为冲突）；标题草稿保留。
// 在途期间用户改了正文：草稿保留、基线不动。
func TestMediaWriteBackSyncsTheContentBaseOnlyWithoutPendingContent(t *testing.T) {
	t.Run("只改标题：基线跟上", func(t *testing.T) {
		h := mediaHarness(t)
		h.open("aud")
		release := h.svc.HoldTranscribe()
		h.svc.SeedTranscribe(okResult("aud", services.LibraryTypeAudio), nil)
		goSpawn(h)
		h.store.Transcribe(h.ctx, false)
		waitUntil(t, "转写请求在途", func() bool { return len(mediaCalls(h)) == 1 })
		h.edit(patchTitle("草稿标题"))
		release()
		waitUntil(t, "转写落定", func() bool { return h.media().RunningAction == "" })

		d := h.detail()
		if d.Item.Content != "服务端新正文" || d.Item.Title != "草稿标题" || d.Item.Transcript != "新文字稿" {
			t.Fatalf("正文换成服务端的、标题草稿保留：%+v", d.Item)
		}
		if b := h.baseContent("aud"); b == nil || *b != "服务端新正文" {
			t.Fatalf("正文基线应同步成服务端版本：%v", b)
		}
		if !d.Unsaved {
			t.Fatal("标题草稿仍未落盘")
		}
	})
	t.Run("改了正文：草稿保留", func(t *testing.T) {
		h := mediaHarness(t)
		h.open("aud")
		release := h.svc.HoldTranscribe()
		h.svc.SeedTranscribe(okResult("aud", services.LibraryTypeAudio), nil)
		goSpawn(h)
		h.store.Transcribe(h.ctx, false)
		waitUntil(t, "转写请求在途", func() bool { return len(mediaCalls(h)) == 1 })
		h.edit(patchContent("我的草稿"))
		release()
		waitUntil(t, "转写落定", func() bool { return h.media().RunningAction == "" })

		d := h.detail()
		if d.Item.Content != "我的草稿" || d.Item.Title != "新标题" {
			t.Fatalf("正文草稿保留、其余换成服务端的：%+v", d.Item)
		}
		if b := h.baseContent("aud"); b == nil || *b != "音频正文" {
			t.Fatalf("有未落盘的正文改动时基线不动：%v", b)
		}
		if row := listRow(h, "aud"); row.UpdatedAt != 5000 {
			t.Fatalf("列表行仍应跟上：%+v", row)
		}
	})
}

// 没编辑过（没有基线）时写回不凭空建基线。
func TestMediaWriteBackWithoutDraftCreatesNoBase(t *testing.T) {
	h := mediaHarness(t)
	h.open("aud")
	h.svc.SeedTranscribe(okResult("aud", services.LibraryTypeAudio), nil)
	h.store.Transcribe(h.ctx, false)
	if b := h.baseContent("aud"); b != nil {
		t.Fatalf("没编辑过不该有基线：%v", *b)
	}
	if d := h.detail(); d.Item.Content != "服务端新正文" || d.Unsaved {
		t.Fatalf("详情：%+v", d)
	}
}

// ---- 转写 ----

// 两个转写按钮各自的运行态：RunningAction 区分「转写」与「区分说话人」，请求带上 diarize；任一在跑时另一个也被挡住。
func TestTranscribeRunningActionDistinguishesTheButtons(t *testing.T) {
	h := mediaHarness(t)
	h.open("aud")
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn

	h.store.Transcribe(h.ctx, true)
	if got := h.media().RunningAction; got != MediaActionDiarize {
		t.Fatalf("区分说话人在跑：%q", got)
	}
	h.store.Transcribe(h.ctx, false)
	if got := h.media().RunningAction; got != MediaActionDiarize || len(q.queued) != 1 {
		t.Fatalf("在途时转写按钮也被挡住：%q，排队 %d", got, len(q.queued))
	}
	q.Run()
	if calls := mediaCalls(h); len(calls) != 1 || !calls[0].Diarize || calls[0].ItemID != "aud" {
		t.Fatalf("请求：%+v", calls)
	}

	h.store.Transcribe(h.ctx, false)
	if got := h.media().RunningAction; got != MediaActionTranscribe {
		t.Fatalf("转写在跑：%q", got)
	}
	q.Run()
	if calls := mediaCalls(h); len(calls) != 2 || calls[1].Diarize {
		t.Fatalf("第二次请求不带 diarize：%+v", calls)
	}
}

// 要了分离却只出来一个说话人：警告（带原因详情），不是绿色的「已生成」；写回照常。
func TestTranscribeWarnsWhenOnlyOneSpeaker(t *testing.T) {
	h := mediaHarness(t)
	h.open("aud")
	res := okResult("aud", services.LibraryTypeAudio)
	res.Warning = "只识别出一个说话人"
	h.svc.SeedTranscribe(res, nil)
	h.store.Transcribe(h.ctx, true)
	if toast := onlyToast(t, h); toast.Kind != kit.ToastWarning || toast.Message != locales.LibraryTranscribeOneSpeaker() || toast.Detail != "只识别出一个说话人" {
		t.Fatalf("提示：%+v", toast)
	}
	if d := h.detail(); d.Item.Transcript != "新文字稿" {
		t.Fatalf("警告也照常写回：%+v", d.Item)
	}
}

// 过期的警告不弹。
func TestTranscribeStaleWarningIsSilent(t *testing.T) {
	h := mediaHarness(t)
	h.open("aud")
	res := okResult("aud", services.LibraryTypeAudio)
	res.Warning = "只识别出一个说话人"
	h.svc.SeedTranscribe(res, nil)
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.Transcribe(h.ctx, true)
	syncSpawn(h)
	h.open("other")
	q.Run()
	if len(h.queuedToasts()) != 0 {
		t.Fatalf("过期的警告不该弹：%+v", h.queuedToasts())
	}
}

// ---- 文字稿排版与长稿确认 ----

func formatCalls(h *harness) []servicestest.MediaCall {
	var out []servicestest.MediaCall
	for _, c := range mediaCalls(h) {
		if c.Method == servicestest.MediaMethodFormat {
			out = append(out, c)
		}
	}
	return out
}

// 不长的文字稿直接排版（allowLong 为假）。边界：去首尾空白后恰好等于上限不算长。
func TestFormatTranscriptRunsDirectlyWhenNotLong(t *testing.T) {
	body := strings.Repeat("字", transcriptFormatLongChars)
	h := mediaHarness(t)
	h.setTranscript("talk", "\n  "+body+"  \n")
	h.open("talk")
	h.svc.SeedFormatTranscript(okResult("talk", services.LibraryTypeAudio), nil)
	h.store.FormatTranscript(h.ctx)
	if calls := formatCalls(h); len(calls) != 1 || calls[0].AllowLong || calls[0].ItemID != "talk" {
		t.Fatalf("应直接发起、不带 allowLong：%+v", calls)
	}
	if h.media().PendingLongFormat != nil {
		t.Fatal("不该有确认提示")
	}
}

// 超长文字稿先弹确认：不发请求，提示里是字数与块数（ceil(字数/1600)）；确认后发请求、带 allowLong，提示清掉。
func TestFormatTranscriptAsksBeforeFormattingALongTranscript(t *testing.T) {
	h := mediaHarness(t)
	h.setTranscript("talk", strings.Repeat("字", 51201))
	h.open("talk")
	h.svc.SeedFormatTranscript(okResult("talk", services.LibraryTypeAudio), nil)
	before := h.wakeCount()
	h.store.FormatTranscript(h.ctx)
	m := h.media()
	if m.PendingLongFormat == nil || *m.PendingLongFormat != (LongFormatPrompt{Chars: 51201, Chunks: 33}) {
		t.Fatalf("确认提示：%+v", m.PendingLongFormat)
	}
	if len(formatCalls(h)) != 0 || m.Formatting || len(h.queuedToasts()) != 0 || h.wakeCount() <= before {
		t.Fatalf("长稿先等确认：请求 %d，%+v，提示 %+v，唤醒 %d→%d", len(formatCalls(h)), m, h.queuedToasts(), before, h.wakeCount())
	}

	h.store.ConfirmLongFormat(h.ctx)
	if calls := formatCalls(h); len(calls) != 1 || !calls[0].AllowLong {
		t.Fatalf("确认后应带 allowLong 发起：%+v", calls)
	}
	if m = h.media(); m.PendingLongFormat != nil || m.Formatting {
		t.Fatalf("提示应清掉、排版已落定：%+v", m)
	}
	if toast := onlyToast(t, h); toast.Message != locales.LibraryTranscriptFormatDone() {
		t.Fatalf("提示：%+v", toast)
	}
}

// 长度按渲染层字符串的 UTF-16 码元数算：表情符号占两个码元，不能按字符数或字节数。
func TestFormatTranscriptLengthIsCountedInUTF16Units(t *testing.T) {
	t.Run("码元数超限而字符数没超", func(t *testing.T) {
		h := mediaHarness(t)
		h.setTranscript("talk", strings.Repeat("😀", 25601)) // 51202 个码元、25601 个字符
		h.open("talk")
		h.store.FormatTranscript(h.ctx)
		if p := h.media().PendingLongFormat; p == nil || *p != (LongFormatPrompt{Chars: 51202, Chunks: 33}) {
			t.Fatalf("确认提示：%+v", p)
		}
	})
	t.Run("字节数超限而码元数没超", func(t *testing.T) {
		h := mediaHarness(t)
		h.setTranscript("talk", strings.Repeat("😀", 24999)) // 49998 个码元、近 10 万字节
		h.open("talk")
		h.svc.SeedFormatTranscript(okResult("talk", services.LibraryTypeAudio), nil)
		h.store.FormatTranscript(h.ctx)
		if h.media().PendingLongFormat != nil || len(formatCalls(h)) != 1 {
			t.Fatalf("不该弹确认：%+v，请求 %d", h.media().PendingLongFormat, len(formatCalls(h)))
		}
	})
	t.Run("上限加一", func(t *testing.T) {
		h := mediaHarness(t)
		h.setTranscript("talk", strings.Repeat("字", transcriptFormatLongChars+1))
		h.open("talk")
		h.store.FormatTranscript(h.ctx)
		if p := h.media().PendingLongFormat; p == nil || p.Chars != transcriptFormatLongChars+1 || p.Chunks != 32 {
			t.Fatalf("确认提示：%+v", p)
		}
	})
}

// 放弃：提示清掉、不发请求、唤醒界面；没有提示时放弃什么也不做；没有待确认的提示时确认也什么也不做（防重复点击）。
func TestFormatTranscriptCancelAndStrayConfirm(t *testing.T) {
	h := mediaHarness(t)
	h.setTranscript("talk", strings.Repeat("字", 60000))
	h.open("talk")
	h.store.ConfirmLongFormat(h.ctx)
	if len(formatCalls(h)) != 0 || h.media().Formatting {
		t.Fatalf("没有待确认的提示时确认不该发起：%v", h.svc.Calls())
	}

	h.store.FormatTranscript(h.ctx)
	if h.media().PendingLongFormat == nil {
		t.Fatal("前提：应有确认提示")
	}
	before := h.wakeCount()
	h.store.CancelLongFormat()
	if h.media().PendingLongFormat != nil || len(formatCalls(h)) != 0 || h.wakeCount() <= before {
		t.Fatalf("放弃：%+v 请求 %d 唤醒 %d→%d", h.media(), len(formatCalls(h)), before, h.wakeCount())
	}
	before = h.wakeCount()
	h.store.CancelLongFormat()
	if h.wakeCount() != before {
		t.Fatal("没有提示时放弃不该唤醒")
	}

	h.store.FormatTranscript(h.ctx)
	h.store.ConfirmLongFormat(h.ctx)
	h.store.ConfirmLongFormat(h.ctx)
	if len(formatCalls(h)) != 1 {
		t.Fatalf("重复确认只发一次：%d", len(formatCalls(h)))
	}
}

// 换条目时确认提示随之清空。
func TestFormatTranscriptPromptClearsWhenSwitchingItems(t *testing.T) {
	h := mediaHarness(t)
	h.setTranscript("talk", strings.Repeat("字", 60000))
	h.open("talk")
	h.store.FormatTranscript(h.ctx)
	h.open("other")
	if h.media() != (MediaView{}) {
		t.Fatalf("换条目应清掉提示：%+v", h.media())
	}
	h.open("talk")
	h.store.ConfirmLongFormat(h.ctx)
	if len(formatCalls(h)) != 0 {
		t.Fatal("换回来之后上一次的提示已作废")
	}
}

// 转写在途时不能排版；排版在途时再点排版也不行；排版期间转写不受影响（渲染层转写只看自己的标记）。
func TestFormatTranscriptIsBlockedWhileTranscribingOrFormatting(t *testing.T) {
	h := mediaHarness(t)
	h.open("talk")
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn

	h.store.Transcribe(h.ctx, false)
	h.store.FormatTranscript(h.ctx)
	if h.media().Formatting || len(q.queued) != 1 {
		t.Fatalf("转写在途时不能排版：%+v 排队 %d", h.media(), len(q.queued))
	}
	q.Run()

	h.store.FormatTranscript(h.ctx)
	h.store.FormatTranscript(h.ctx)
	if !h.media().Formatting || len(q.queued) != 1 {
		t.Fatalf("排版在途时再点排版什么也不做：%+v 排队 %d", h.media(), len(q.queued))
	}
	h.store.Transcribe(h.ctx, false)
	if h.media().RunningAction != MediaActionTranscribe {
		t.Fatalf("排版期间转写不受影响：%+v", h.media())
	}
}

// 排版只排了一部分也算成功（结果已落库）：给警告（带原因），不弹绿色的完成。未配置模型、失败（标题不带原因、原因进详情）见通用用例。
func TestFormatTranscriptWarnsOnPartialResult(t *testing.T) {
	h := mediaHarness(t)
	h.open("talk")
	res := okResult("talk", services.LibraryTypeAudio)
	res.Warning = "第 3 块排版失败"
	h.svc.SeedFormatTranscript(res, nil)
	h.store.FormatTranscript(h.ctx)
	if toast := onlyToast(t, h); toast.Kind != kit.ToastWarning || toast.Message != locales.LibraryTranscriptFormatPartial() || toast.Detail != "第 3 块排版失败" {
		t.Fatalf("提示：%+v", toast)
	}
	if d := h.detail(); d.Item.Transcript != "新文字稿" {
		t.Fatalf("部分结果也写回：%+v", d.Item)
	}
}

// 排版不先落盘（渲染层 runFormat 没有 flushPendingSave）：落盘会失败也照样发起。
func TestFormatTranscriptDoesNotFlushPendingEdits(t *testing.T) {
	h := mediaHarness(t)
	h.open("talk")
	h.edit(patchContent("没存下的正文"))
	h.svc.FailOn("SaveDraft", errors.New("磁盘已满"))
	h.store.FormatTranscript(h.ctx)
	if len(formatCalls(h)) != 1 || callsWithPrefix(h, "SaveDraft") != 0 {
		t.Fatalf("排版不该落盘：%v", h.svc.Calls())
	}
}

// ---- 总结 ----

// 总结与排版可以同时在途（各看各的标记）。
func TestSummarizeMediaRunsIndependentlyOfFormatting(t *testing.T) {
	h := mediaHarness(t)
	h.open("talk")
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.FormatTranscript(h.ctx)
	h.store.SummarizeMedia(h.ctx)
	if m := h.media(); !m.Formatting || !m.Summarizing {
		t.Fatalf("两者各自在途：%+v", m)
	}
	q.Run()
	if m := h.media(); m != (MediaView{}) {
		t.Fatalf("都落定后应清空：%+v", m)
	}
}

// ---- 刷新论坛讨论 ----

// 只对论坛条目且来源是 Linux.do / 小众软件论坛 / 2libra 的发起；其余（别的论坛、没有来源、非论坛类型）什么也不做。
func TestRefreshForumOnlyForSupportedPlatforms(t *testing.T) {
	for _, id := range []string{"forum", "forum-appinn", "forum-2libra"} {
		t.Run("支持/"+id, func(t *testing.T) {
			h := mediaHarness(t)
			h.open(id)
			h.store.RefreshForumDiscussion(h.ctx)
			if calls := mediaCalls(h); len(calls) != 1 || calls[0].Method != servicestest.MediaMethodRefresh || calls[0].ItemID != id {
				t.Fatalf("请求：%+v", calls)
			}
		})
	}
	for _, id := range []string{"forum-v2ex", "forum-nosrc", "aud-linuxdo", "other"} {
		t.Run("不支持/"+id, func(t *testing.T) {
			h := mediaHarness(t)
			h.open(id)
			h.store.RefreshForumDiscussion(h.ctx)
			if len(mediaCalls(h)) != 0 || h.media().RefreshingDiscussion || len(h.queuedToasts()) != 0 {
				t.Fatalf("不该发起：%v %+v", h.svc.Calls(), h.media())
			}
		})
	}
}

// 成功提示看发起时有没有讨论总结：有则说明「原讨论总结已标记为过期」，没有则只说「讨论已刷新」。
func TestRefreshForumToastDependsOnTheSummaryAtStart(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"forum", locales.LibraryForumDiscussionRefreshedSummaryStale()},
		{"forum-nosum", locales.LibraryForumDiscussionRefreshed()},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			h := mediaHarness(t)
			h.open(c.id)
			h.svc.SeedRefreshForum(okResult(c.id, services.LibraryTypeForum), nil)
			h.store.RefreshForumDiscussion(h.ctx)
			if toast := onlyToast(t, h); toast.Kind != kit.ToastSuccess || toast.Message != c.want {
				t.Fatalf("提示：%+v，期望 %q", toast, c.want)
			}
		})
	}
	if locales.LibraryForumDiscussionRefreshedSummaryStale() == locales.LibraryForumDiscussionRefreshed() {
		t.Fatal("两句提示必须不同，否则上面的断言区分不了")
	}
}

// 发起时的总结状态以详情里当时的正文为准（含未落盘的草稿）：用户把总结小节删光之后刷新，提示不再说「总结已过期」。
func TestRefreshForumUsesTheDraftContentToDecideTheSummaryHint(t *testing.T) {
	h := mediaHarness(t)
	h.open("forum")
	h.edit(patchContent(forumNoSummary))
	h.svc.SeedRefreshForum(okResult("forum", services.LibraryTypeForum), nil)
	h.store.RefreshForumDiscussion(h.ctx)
	if toast := onlyToast(t, h); toast.Message != locales.LibraryForumDiscussionRefreshed() {
		t.Fatalf("提示：%+v", toast)
	}
	if got := notes.SplitForumSections(forumNoSummary).Summary; got != "" {
		t.Fatalf("前提：夹具里没有总结：%q", got)
	}
}

// ---- 进度推送 ----

func transcribeProgress(id string, elapsed int64) services.LibraryTranscribeProgress {
	return services.LibraryTranscribeProgress{ItemID: id, Stage: services.LibraryTranscribeStageTranscribing, ElapsedMs: elapsed}
}

// 转写进度：只收当前选中条目、转写在跑时的推送；来一次换一个对象；唤醒界面；转写落定后清空，晚到的推送被挡掉。
func TestMediaTranscribeProgressIsFilteredByItemAndRunState(t *testing.T) {
	h := mediaHarness(t)
	h.open("aud")

	h.svc.EmitTranscribeProgress(transcribeProgress("aud", 1000))
	if h.media().TranscribeProgress != nil {
		t.Fatal("没有转写在跑时不收推送")
	}

	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.Transcribe(h.ctx, false)
	before := h.wakeCount()
	h.svc.EmitTranscribeProgress(transcribeProgress("aud", 3000))
	first := h.media().TranscribeProgress
	if first == nil || first.ElapsedMs != 3000 || first.Stage != services.LibraryTranscribeStageTranscribing || h.wakeCount() <= before {
		t.Fatalf("应收下并唤醒：%+v 唤醒 %d→%d", first, before, h.wakeCount())
	}

	before = h.wakeCount()
	h.svc.EmitTranscribeProgress(transcribeProgress("talk", 9000)) // 别的条目
	if got := h.media().TranscribeProgress; got != first || h.wakeCount() != before {
		t.Fatalf("别的条目的推送不该收：%+v", got)
	}
	stalled := int64(4000)
	h.svc.EmitTranscribeProgress(services.LibraryTranscribeProgress{ItemID: "aud", Stage: services.LibraryTranscribeStageFormatting, ElapsedMs: 5000, StalledMs: &stalled})
	got := h.media().TranscribeProgress
	if got == first || got.ElapsedMs != 5000 || got.Stage != services.LibraryTranscribeStageFormatting || got.StalledMs == nil || *got.StalledMs != 4000 {
		t.Fatalf("新推送换新对象，旧快照不变：%+v / %+v", got, first)
	}
	if first.ElapsedMs != 3000 {
		t.Fatalf("旧快照被改了：%+v", first)
	}

	q.Run()
	if h.media().TranscribeProgress != nil || h.media().RunningAction != "" {
		t.Fatalf("落定后应清空：%+v", h.media())
	}
	h.svc.EmitTranscribeProgress(transcribeProgress("aud", 7000))
	if h.media().TranscribeProgress != nil {
		t.Fatal("落定之后晚到的推送不该收")
	}
}

// 新一次转写不带上一次的进度（发起时清空）。
func TestMediaTranscribeStartClearsTheOldProgress(t *testing.T) {
	h := mediaHarness(t)
	h.open("aud")
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.Transcribe(h.ctx, false)
	h.svc.EmitTranscribeProgress(transcribeProgress("aud", 3000))
	// 模拟上一次没清干净的残留：直接放下忙标记但保留进度，再发起。
	h.store.mu.Lock()
	h.store.view.Detail.Media.RunningAction = ""
	h.store.mu.Unlock()
	h.store.Transcribe(h.ctx, false)
	if h.media().TranscribeProgress != nil {
		t.Fatalf("发起时应清空进度：%+v", h.media().TranscribeProgress)
	}
}

// 转写落定时（含失败）进度都清空。
func TestMediaTranscribeProgressClearedOnFailure(t *testing.T) {
	h := mediaHarness(t)
	h.open("aud")
	h.svc.SeedTranscribe(services.LibraryMediaActionResult{}, errors.New("网络断开"))
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.Transcribe(h.ctx, true)
	h.svc.EmitTranscribeProgress(transcribeProgress("aud", 2000))
	if h.media().TranscribeProgress == nil {
		t.Fatal("前提：应已收下进度")
	}
	q.Run()
	if m := h.media(); m.TranscribeProgress != nil || m.RunningAction != "" {
		t.Fatalf("失败后也应清空：%+v", m)
	}
}

// 排版进度：只收当前选中条目、排版在跑时的推送；落定后清空；转写在跑而没在排版时不收。
func TestMediaFormatProgressIsFilteredByItemAndRunState(t *testing.T) {
	h := mediaHarness(t)
	h.open("talk")
	h.svc.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: "talk", Current: 1, Total: 4})
	if h.media().FormatProgress != nil {
		t.Fatal("没有排版在跑时不收推送")
	}

	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.Transcribe(h.ctx, false) // 只有转写在跑
	h.svc.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: "talk", Current: 1, Total: 4})
	if h.media().FormatProgress != nil {
		t.Fatal("没有排版在跑（只有转写）时不收排版推送")
	}
	q.Run()

	h.store.FormatTranscript(h.ctx)
	before := h.wakeCount()
	h.svc.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: "talk", Current: 2, Total: 4})
	p := h.media().FormatProgress
	if p == nil || p.Current != 2 || p.Total != 4 || h.wakeCount() <= before {
		t.Fatalf("应收下并唤醒：%+v", p)
	}
	h.svc.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: "aud", Current: 3, Total: 4})
	if h.media().FormatProgress != p {
		t.Fatal("别的条目的推送不该收")
	}
	q.Run()
	if h.media().FormatProgress != nil || h.media().Formatting {
		t.Fatalf("落定后应清空：%+v", h.media())
	}
	h.svc.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: "talk", Current: 4, Total: 4})
	if h.media().FormatProgress != nil {
		t.Fatal("落定之后晚到的推送不该收")
	}
}

// 换条目后旧条目的推送不收，新条目重新开始。
func TestMediaProgressIgnoredAfterSwitchingItems(t *testing.T) {
	h := mediaHarness(t)
	h.open("aud")
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.Transcribe(h.ctx, false)
	syncSpawn(h)
	h.open("talk")
	h.svc.EmitTranscribeProgress(transcribeProgress("aud", 3000))
	h.svc.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: "aud", Current: 1, Total: 2})
	if h.media() != (MediaView{}) {
		t.Fatalf("旧条目的推送不该落进新条目：%+v", h.media())
	}
}

// ---- 订阅生命周期 ----

// Load 订阅一次（重复 Load 不重复订阅）；Close 取消、可重复调用；之后再 Load 重新订阅。
func TestMediaProgressSubscriptionLifecycle(t *testing.T) {
	h := newHarness(t)
	if n := h.svc.MediaSubscribers(); n != 0 {
		t.Fatalf("没 Load 之前不该订阅：%d", n)
	}
	h.store.Load(h.ctx)
	h.store.Load(h.ctx)
	if n := h.svc.MediaSubscribers(); n != 1 {
		t.Fatalf("重复 Load 只订阅一次：%d", n)
	}
	h.store.Close()
	h.store.Close()
	if n := h.svc.MediaSubscribers(); n != 0 {
		t.Fatalf("Close 之后应已取消：%d", n)
	}
	h.store.Load(h.ctx)
	if n := h.svc.MediaSubscribers(); n != 1 {
		t.Fatalf("Close 之后再 Load 应重新订阅：%d", n)
	}
	h.store.Close()
	if n := h.svc.MediaSubscribers(); n != 0 {
		t.Fatalf("最后应清零：%d", n)
	}
}

// Close 之后推送不再进状态。
func TestMediaProgressStopsAfterClose(t *testing.T) {
	h := mediaHarness(t)
	h.open("aud")
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.Transcribe(h.ctx, false)
	h.store.Close()
	h.svc.EmitTranscribeProgress(transcribeProgress("aud", 3000))
	if h.media().TranscribeProgress != nil {
		t.Fatal("Close 之后不该再收推送")
	}
}

// 返回空取消函数的订阅实现（没挂推送总线）不能让 Close 崩溃。
type noStopLibrary struct{ services.Library }

func (noStopLibrary) SubscribeMediaProgress(func(services.LibraryTranscribeProgress), func(services.LibraryTranscriptFormatProgress)) func() {
	return nil
}

func TestMediaSubscriptionToleratesANilStop(t *testing.T) {
	h := newHarness(t)
	h.store.svc = noStopLibrary{h.svc}
	h.store.Load(h.ctx)
	h.store.Close() // 不该 panic
}
