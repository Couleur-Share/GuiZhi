package library

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 本文件测详情里媒体动作与文字稿面板的界面接线（detailtoolbar.go、detailtranscript.go，台账 B-067）：
// 工具栏五个动作（转写 / 区分说话人 / AI 排版 / 总结 / 刷新讨论）的触发、转圈归属与互锁；长稿排版确认框；文字稿空态；
// 正文态（保留段落与折行、转写 / 排版进度胶囊、页内查找高亮与计数）；阅读位置的恢复与防抖写入。
// 动作背后的规则（防重入、写回、失败提示）在 store_media_test.go 里测过，这里只测界面接对了没有。
// 夹具里后台动作默认同步执行；要观察「在途」就 spawnTracker 并用服务的 Hold* 卡住。

const (
	mediaLongID   = "x-media-long"   // 文字稿超过 5 万字的视频（长稿排版确认框）
	mediaScrollID = "x-media-scroll" // 60 行文字稿，里面有两处「目标」（滚动、查找定位、阅读位置）
	mediaFindID   = "x-media-find"   // 三行短文字稿（页内查找高亮与计数）
	mediaParaID   = "x-media-para"   // 有空行、连续空格、超长行的文字稿（保留版式）
	mediaLocalID  = "x-media-local"  // 本地视频、没有文字稿
	mediaWrapID   = "x-media-wrap"   // 60 行文字稿，第 40 行里有一处折成好几行的长命中（查找定位取整个命中的中心）
	mediaBili     = "https://www.bilibili.com/video/BV1media"
)

// mediaLongTranscript 是 1700 行、每行 30 个字的文字稿：去掉末尾换行后 52 699 个 UTF-16 单元，超过长稿门槛 5 万。
func mediaLongTranscript() string {
	return strings.Repeat(strings.Repeat("这是一句话。", 5)+"\n", 1700)
}

// scrollLine 是 mediaScrollTranscript 里第 n 段（从 1 起）没有「目标」时的整行文字。
func scrollLine(n int) string {
	return fmt.Sprintf("第%02d段，这里是一些普通的内容。", n)
}

// mediaScrollTranscript 是 60 行，每行一段；第 5 与第 40 段里各有一个「目标」。
func mediaScrollTranscript() string {
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = scrollLine(i + 1)
	}
	lines[4] = "第05段，目标就在这里。"
	lines[39] = "第40段，另一个目标也在这里。" // 不放在最末：最末的命中滚不到正中（滚动量到顶了）
	return strings.Join(lines, "\n")
}

func mediaExtras() []shellExtra {
	video := func(id, title string, edit ...func(*services.LibraryEntry)) services.LibraryEntry {
		return shellEntry(id, title, "video", edit...)
	}
	body := func(transcript string) servicestest.DetailBody {
		return servicestest.DetailBody{Content: "视频", Transcript: transcript, SourceURI: mediaBili}
	}
	return []shellExtra{
		{video(mediaLongID, "长稿视频"), body(mediaLongTranscript())},
		{video(mediaScrollID, "长文字稿视频"), body(mediaScrollTranscript())},
		{video(mediaWrapID, "折行命中视频"), body(mediaWrapTranscript())},
		{video(mediaFindID, "查找用视频"), body("命运石之门里的命运\n第二行也有命运\nAbc abc ABC")},
		{video(mediaParaID, "版式用视频"), body("第一段。\n\n第二段  含两个空格。\n" + strings.Repeat("长", 100))},
		{video(mediaLocalID, "本地视频"), servicestest.DetailBody{Content: "local-video://clip_01.mp4"}},
	}
}

// mediaRig 是壳夹具再追加上面几条视频。
func mediaRig(t *testing.T, mutate ...func(*rig)) *rig {
	t.Helper()
	return shellRig(t, append([]func(*rig){func(r *rig) {
		entries := detailEntries()
		for _, x := range shellExtras() {
			entries = append(entries, x.entry)
		}
		for _, x := range mediaExtras() {
			entries = append(entries, x.entry)
			r.svc.SeedDetail(x.entry.ID, x.body)
		}
		r.svc.SeedEntries(entries)
	}}, mutate...)...)
}

// mediaOK 是一次成功的动作回复：写回的条目带着来源与文字稿，工具栏与文字稿页签不会因为写回而消失。
func mediaOK(id, itemType, content, transcript, source string) services.LibraryMediaActionResult {
	item := services.LibraryItem{
		ID: id, Title: "新标题", Content: content, Transcript: transcript, SourceURI: source, ItemType: itemType,
		Status: services.LibraryStatusActive, UpdatedAt: 5000, Tags: []services.LibraryTag{},
	}
	return services.LibraryMediaActionResult{Item: &item}
}

func mediaVideoOK(id string) services.LibraryMediaActionResult {
	return mediaOK(id, "video", "视频", "新文字稿", mediaBili)
}

// openTranscript 选中一条并切到文字稿页签。
func (r *rig) openTranscript(id string) {
	r.t.Helper()
	r.pick(id)
	r.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
	if got := r.screen.detail.panel.tab; got != TabTranscript {
		r.t.Fatalf("点文字稿页签之后应在文字稿页，实际 %q", got)
	}
	r.frames(4)
}

// detailDisabled 是详情栏里某个按钮此刻是不是禁用的。
func (r *rig) detailDisabled(name string) bool {
	r.t.Helper()
	n, ok := r.detailNode(gfx.RoleButton, name)
	if !ok {
		r.t.Fatalf("详情栏里找不到按钮「%s」", name)
	}
	return n.Disabled
}

// cropDetail 截下详情栏里某个按钮的画面（指针先挪开，免得悬停底色混进来）。
func (r *rig) cropDetail(name string) *image.RGBA {
	r.t.Helper()
	if err := r.session.Move(2, 2); err != nil {
		r.t.Fatalf("移动指针失败：%v", err)
	}
	r.frames(2)
	n, ok := r.detailNode(gfx.RoleButton, name)
	if !ok {
		r.t.Fatalf("详情栏里找不到按钮「%s」", name)
	}
	img, err := r.session.Image()
	if err != nil {
		r.t.Fatalf("取图失败：%v", err)
	}
	rect := image.Rect(int(n.Bounds.X), int(n.Bounds.Y), int(n.Bounds.X+n.Bounds.Width), int(n.Bounds.Y+n.Bounds.Height))
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), img, rect.Min, draw.Src)
	return out
}

func sameImage(a, b *image.RGBA) bool { return a.Rect == b.Rect && bytes.Equal(a.Pix, b.Pix) }

// requireSpinnerFollowsTheInitiator 断言「转圈的只是发起的那一枚」：禁用的钮图标不变、发起的钮换成转圈，
// 所以同一个钮在「自己发起」与「对方发起」两种情形下的画面必然不同。
func requireSpinnerFollowsTheInitiator(t *testing.T, first, second string, firstWhenFirstStarts, firstWhenSecondStarts, secondWhenFirstStarts, secondWhenSecondStarts *image.RGBA) {
	t.Helper()
	if sameImage(firstWhenFirstStarts, firstWhenSecondStarts) {
		t.Errorf("「%s」自己发起与对方发起时的画面应不同（自己发起时才转圈）", first)
	}
	if sameImage(secondWhenFirstStarts, secondWhenSecondStarts) {
		t.Errorf("「%s」自己发起与对方发起时的画面应不同（自己发起时才转圈）", second)
	}
}

// ---- 工具栏：触发 ----

func TestDetailMediaToolbarTriggersTheVideoActions(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	done := mediaVideoOK(detailVideoID)
	r.svc.SeedTranscribe(done, nil)
	r.svc.SeedFormatTranscript(done, nil)
	r.svc.SeedSummarizeMedia(done, nil)
	r.openTranscript(detailVideoID)
	r.setDetail(func(d *DetailView) { d.CanDiarize = true })

	r.clickInDetail(gfx.RoleButton, locales.LibraryTranscribeRegenerate())
	r.setDetail(func(d *DetailView) { d.CanDiarize = true })
	r.clickInDetail(gfx.RoleButton, locales.LibraryTranscribeDiarize())
	r.clickInDetail(gfx.RoleButton, locales.LibraryTranscriptFormat())
	r.clickPrefix(gfx.RoleButton, locales.LibraryBodySection())
	r.clickInDetail(gfx.RoleButton, locales.LibraryMediaSummaryGenerateVideo())

	want := []servicestest.MediaCall{
		{Method: servicestest.MediaMethodTranscribe, ItemID: detailVideoID},
		{Method: servicestest.MediaMethodTranscribe, ItemID: detailVideoID, Diarize: true},
		{Method: servicestest.MediaMethodFormat, ItemID: detailVideoID},
		{Method: servicestest.MediaMethodSummarize, ItemID: detailVideoID},
	}
	if got := r.svc.MediaCalls(); !slices.Equal(got, want) {
		t.Fatalf("四个按钮应各发一次请求（区分说话人带 Diarize、排版不带 AllowLong）：\n got %+v\nwant %+v", got, want)
	}
}

func TestDetailMediaDiarizeNeedsTheCapability(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.openTranscript(detailVideoID)
	if r.hasInDetail(gfx.RoleButton, locales.LibraryTranscribeDiarize()) {
		t.Fatal("没有区分说话人能力时不该有这枚钮")
	}
	if !r.hasInDetail(gfx.RoleButton, locales.LibraryTranscribeRegenerate()) {
		t.Fatal("夹具失效：重新转写钮应在")
	}
	r.setDetail(func(d *DetailView) { d.CanDiarize = true })
	if !r.hasInDetail(gfx.RoleButton, locales.LibraryTranscribeDiarize()) {
		t.Fatal("有区分说话人能力时应出现这枚钮")
	}
}

func TestDetailMediaForumToolbarRefreshesAndSummarizes(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	forum := mediaOK(detailForumID, "forum", detailForumContent, "", "https://linux.do/t/123")
	r.svc.SeedRefreshForum(forum, nil)
	r.svc.SeedSummarizeMedia(forum, nil)
	r.pick(detailForumID)
	r.click(gfx.RoleButton, locales.LibraryForumRepliesSection())

	r.clickInDetail(gfx.RoleButton, locales.LibraryForumDiscussionRefresh())
	r.clickInDetail(gfx.RoleButton, summaryLabel(true, false, true))
	want := []servicestest.MediaCall{
		{Method: servicestest.MediaMethodRefresh, ItemID: detailForumID},
		{Method: servicestest.MediaMethodSummarize, ItemID: detailForumID},
	}
	if got := r.svc.MediaCalls(); !slices.Equal(got, want) {
		t.Fatalf("刷新讨论与总结应各发一次请求：\n got %+v\nwant %+v", got, want)
	}
}

// ---- 工具栏：转圈与互锁 ----

// 转写在途：重新转写、区分说话人、AI 排版都禁用，只有发起的那一枚转圈；结束后全部恢复。点禁用的钮不发请求。
func TestDetailMediaTranscribeAndDiarizeShareOneBusyFlag(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.svc.SeedTranscribe(mediaVideoOK(detailVideoID), nil)
	r.openTranscript(detailVideoID)
	r.setDetail(func(d *DetailView) { d.CanDiarize = true })
	wg := r.spawnTracker()
	regenerate, diarize, format := locales.LibraryTranscribeRegenerate(), locales.LibraryTranscribeDiarize(), locales.LibraryTranscriptFormat()
	idle := r.cropDetail(regenerate)

	run := func(initiator, action string) (regenerateImg, diarizeImg *image.RGBA) {
		t.Helper()
		release := r.once(r.svc.HoldTranscribe())
		r.clickInDetail(gfx.RoleButton, initiator)
		if got := r.view().Detail.Media.RunningAction; got != action {
			t.Fatalf("点「%s」之后运行态应是 %q，实际 %q", initiator, action, got)
		}
		for _, name := range []string{regenerate, diarize, format} {
			if !r.detailDisabled(name) {
				t.Errorf("转写在途时「%s」应禁用", name)
			}
		}
		regenerateImg, diarizeImg = r.cropDetail(regenerate), r.cropDetail(diarize)
		before := len(r.svc.MediaCalls())
		r.clickInDetail(gfx.RoleButton, diarize)
		r.clickInDetail(gfx.RoleButton, format)
		if got := len(r.svc.MediaCalls()); got != before {
			t.Errorf("点禁用的钮不该发请求：%d → %d", before, got)
		}
		release()
		wg.Wait()
		r.frames(3)
		for _, name := range []string{regenerate, diarize, format} {
			if r.detailDisabled(name) {
				t.Errorf("转写结束后「%s」应恢复可点", name)
			}
		}
		return regenerateImg, diarizeImg
	}
	regenA, diaA := run(regenerate, MediaActionTranscribe)
	regenB, diaB := run(diarize, MediaActionDiarize)
	requireSpinnerFollowsTheInitiator(t, regenerate, diarize, regenA, regenB, diaA, diaB)
	if sameImage(idle, regenA) {
		t.Error("在途的重新转写钮应与空闲时不同")
	}
	want := []servicestest.MediaCall{
		{Method: servicestest.MediaMethodTranscribe, ItemID: detailVideoID},
		{Method: servicestest.MediaMethodTranscribe, ItemID: detailVideoID, Diarize: true},
	}
	if got := r.svc.MediaCalls(); !slices.Equal(got, want) {
		t.Fatalf("两次发起各一个请求：\n got %+v\nwant %+v", got, want)
	}
}

// AI 排版在途：排版钮自己转圈并禁用，名字换成逐块进度；转写钮不受影响（渲染层只有转写钮挡排版，没有反过来挡）。
func TestDetailMediaFormatShowsProgressAndLocksItself(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.svc.SeedFormatTranscript(mediaVideoOK(detailVideoID), nil)
	r.openTranscript(detailVideoID)
	r.setDetail(func(d *DetailView) { d.CanDiarize = true })
	wg := r.spawnTracker()
	release := r.once(r.svc.HoldFormatTranscript())

	r.clickInDetail(gfx.RoleButton, locales.LibraryTranscriptFormat())
	if !r.view().Detail.Media.Formatting {
		t.Fatal("点 AI 排版之后应在排版中")
	}
	if !r.detailDisabled(locales.LibraryTranscriptFormat()) {
		t.Error("排版在途时排版钮应禁用")
	}
	for _, name := range []string{locales.LibraryTranscribeRegenerate(), locales.LibraryTranscribeDiarize()} {
		if r.detailDisabled(name) {
			t.Errorf("排版在途时「%s」不该被禁用", name)
		}
	}
	if !r.hasText(locales.LibraryTranscriptFormatting()) {
		t.Errorf("没有逐块进度时胶囊应写「%s」", locales.LibraryTranscriptFormatting())
	}

	r.svc.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: "other", Current: 9, Total: 9})
	r.frames(2)
	if r.view().Detail.Media.FormatProgress != nil {
		t.Fatal("别的条目的排版进度不该收下")
	}
	r.svc.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: detailVideoID, Current: 2, Total: 5})
	r.frames(3)
	progress := locales.LibraryTranscriptFormatProgress(2, 5)
	if !r.hasInDetail(gfx.RoleButton, progress) || r.hasInDetail(gfx.RoleButton, locales.LibraryTranscriptFormat()) {
		t.Errorf("逐块进度到了之后，排版钮的名字应是「%s」", progress)
	}
	if !r.hasText(progress) {
		t.Errorf("胶囊应写「%s」", progress)
	}

	release()
	wg.Wait()
	r.frames(3)
	if r.detailDisabled(locales.LibraryTranscriptFormat()) || r.hasText(progress) {
		t.Fatal("排版结束后钮应恢复、进度应消失")
	}
}

// 总结与刷新讨论互锁：各自转圈，对方禁用。
func TestDetailMediaSummaryAndRefreshLockEachOther(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	forum := mediaOK(detailForumID, "forum", detailForumContent, "", "https://linux.do/t/123")
	r.svc.SeedRefreshForum(forum, nil)
	r.svc.SeedSummarizeMedia(forum, nil)
	r.pick(detailForumID)
	r.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	wg := r.spawnTracker()
	refresh, summary := locales.LibraryForumDiscussionRefresh(), summaryLabel(true, false, true)

	run := func(initiator string, hold func() func(), busy func(MediaView) bool) (refreshImg, summaryImg *image.RGBA) {
		t.Helper()
		release := r.once(hold())
		r.clickInDetail(gfx.RoleButton, initiator)
		if !busy(r.view().Detail.Media) {
			t.Fatalf("点「%s」之后对应的忙标记应为真：%+v", initiator, r.view().Detail.Media)
		}
		if !r.detailDisabled(refresh) || !r.detailDisabled(summary) {
			t.Errorf("总结或刷新讨论在途时两枚钮都应禁用")
		}
		refreshImg, summaryImg = r.cropDetail(refresh), r.cropDetail(summary)
		release()
		wg.Wait()
		r.frames(3)
		if r.detailDisabled(refresh) || r.detailDisabled(summary) {
			t.Errorf("结束之后两枚钮应恢复可点")
		}
		return refreshImg, summaryImg
	}
	refreshA, summaryA := run(refresh, func() func() { return r.svc.HoldRefreshForum() }, func(m MediaView) bool { return m.RefreshingDiscussion })
	refreshB, summaryB := run(summary, func() func() { return r.svc.HoldSummarizeMedia() }, func(m MediaView) bool { return m.Summarizing })
	requireSpinnerFollowsTheInitiator(t, refresh, summary, refreshA, refreshB, summaryA, summaryB)
}

// ---- 长稿排版确认框 ----

func TestDetailMediaLongFormatAsksBeforeSpending(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.svc.SeedFormatTranscript(mediaVideoOK(mediaLongID), nil)
	r.openTranscript(mediaLongID)
	format := locales.LibraryTranscriptFormat()

	r.clickInDetail(gfx.RoleButton, format)
	pending := r.view().Detail.Media.PendingLongFormat
	if pending == nil {
		t.Fatal("长稿点 AI 排版应先弹确认框")
	}
	if want := (LongFormatPrompt{Chars: 52_699, Chunks: (52_699 + transcriptFormatChunkChars - 1) / transcriptFormatChunkChars}); *pending != want {
		t.Fatalf("确认框的字数与块数应是 %+v，实际 %+v", want, *pending)
	}
	if got := len(r.svc.MediaCalls()); got != 0 {
		t.Fatalf("确认之前不该发请求，实际 %d 次", got)
	}
	if !r.hasText(format) {
		t.Error("确认框标题应是「AI 排版」")
	}
	if text := strings.ReplaceAll(r.screenText(), "\n", ""); !strings.Contains(text, locales.LibraryTranscriptFormatLongConfirm(pending.Chars, pending.Chunks)) {
		t.Errorf("确认框正文应写出字数与块数：%q", text)
	}
	if !r.has(gfx.RoleButton, locales.CommonConfirm()) || !r.has(gfx.RoleButton, locales.CommonCancel()) {
		t.Fatal("确认框应有确认与取消两枚钮")
	}

	// 取消：框关掉、不发请求。
	r.click(gfx.RoleButton, locales.CommonCancel())
	if r.view().Detail.Media.PendingLongFormat != nil || r.has(gfx.RoleButton, locales.CommonConfirm()) || len(r.svc.MediaCalls()) != 0 {
		t.Fatal("取消应关框且不发请求")
	}

	// Esc 同取消。
	r.clickInDetail(gfx.RoleButton, format)
	r.keys("escape")
	if r.view().Detail.Media.PendingLongFormat != nil || len(r.svc.MediaCalls()) != 0 {
		t.Fatal("Esc 应放弃长稿排版")
	}

	// 确认：带 AllowLong 发请求，框关掉。
	r.clickInDetail(gfx.RoleButton, format)
	r.click(gfx.RoleButton, locales.CommonConfirm())
	want := []servicestest.MediaCall{{Method: servicestest.MediaMethodFormat, ItemID: mediaLongID, AllowLong: true}}
	if got := r.svc.MediaCalls(); !slices.Equal(got, want) {
		t.Fatalf("确认之后应带 AllowLong 发一次请求：\n got %+v\nwant %+v", got, want)
	}
	if r.view().Detail.Media.PendingLongFormat != nil || r.has(gfx.RoleButton, locales.CommonConfirm()) {
		t.Fatal("确认之后框应关掉")
	}
}

func TestDetailMediaShortTranscriptFormatsWithoutAsking(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.svc.SeedFormatTranscript(mediaVideoOK(detailVideoID), nil)
	r.openTranscript(detailVideoID)
	r.clickInDetail(gfx.RoleButton, locales.LibraryTranscriptFormat())
	if r.has(gfx.RoleButton, locales.CommonConfirm()) {
		t.Fatal("短稿不该弹确认框")
	}
	want := []servicestest.MediaCall{{Method: servicestest.MediaMethodFormat, ItemID: detailVideoID}}
	if got := r.svc.MediaCalls(); !slices.Equal(got, want) {
		t.Fatalf("短稿应直接排版：\n got %+v\nwant %+v", got, want)
	}
}

// ---- 文字稿空态 ----

func TestDetailMediaEmptyStateOffersToGenerate(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.svc.SeedTranscribe(mediaVideoOK(shellBareVideoID), nil)
	r.openTranscript(shellBareVideoID)
	if !r.hasText(locales.LibraryTranscriptEmpty()) {
		t.Fatalf("没有文字稿时应显示「%s」", locales.LibraryTranscriptEmpty())
	}
	if r.has(gfx.RoleButton, locales.LibraryTranscriptFormat()) {
		t.Fatal("没有文字稿时工具栏没有排版钮")
	}
	r.click(gfx.RoleButton, locales.LibraryTranscribeRegenerateOnline())
	want := []servicestest.MediaCall{{Method: servicestest.MediaMethodTranscribe, ItemID: shellBareVideoID}}
	if got := r.svc.MediaCalls(); !slices.Equal(got, want) {
		t.Fatalf("在线视频的空态按钮应发一次转写：\n got %+v\nwant %+v", got, want)
	}
	if r.hasText(locales.LibraryTranscriptEmpty()) || !r.hasText("新文字稿") {
		t.Fatal("转写写回之后应换成正文态")
	}
}

func TestDetailMediaEmptyStateOfALocalVideoSaysGenerate(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.svc.SeedTranscribe(mediaVideoOK(mediaLocalID), nil)
	r.openTranscript(mediaLocalID)
	if r.has(gfx.RoleButton, locales.LibraryTranscribeRegenerateOnline()) {
		t.Fatal("本地视频不该说「在线」")
	}
	r.click(gfx.RoleButton, locales.LibraryTranscribeGenerate())
	want := []servicestest.MediaCall{{Method: servicestest.MediaMethodTranscribe, ItemID: mediaLocalID}}
	if got := r.svc.MediaCalls(); !slices.Equal(got, want) {
		t.Fatalf("本地视频的空态按钮应发一次转写：\n got %+v\nwant %+v", got, want)
	}
}

func TestDetailMediaEmptyStateShowsProgressAndStall(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.svc.SeedTranscribe(mediaVideoOK(shellBareVideoID), nil)
	r.openTranscript(shellBareVideoID)
	wg := r.spawnTracker()
	release := r.once(r.svc.HoldTranscribe())
	r.click(gfx.RoleButton, locales.LibraryTranscribeRegenerateOnline())

	running := locales.LibraryTranscribeRunning()
	if n := r.mustNode(gfx.RoleButton, running); !n.Disabled {
		t.Fatal("转写中空态的按钮应禁用")
	}
	stage := locales.LibraryStageTranscribing()
	ms := func(v int64) *int64 { return &v }
	for _, c := range []struct {
		name string
		p    services.LibraryTranscribeProgress
		want string
	}{
		{"别的条目的推送不收", services.LibraryTranscribeProgress{ItemID: "other", Stage: "transcribing", ElapsedMs: 9000}, running},
		{"已用时长", services.LibraryTranscribeProgress{ItemID: shellBareVideoID, Stage: "transcribing", ElapsedMs: 83_000}, locales.LibraryTranscribeElapsed(stage, "1:23")},
		{"停滞不足一分钟不提示", services.LibraryTranscribeProgress{ItemID: shellBareVideoID, Stage: "transcribing", ElapsedMs: 83_000, StalledMs: ms(59_999)}, locales.LibraryTranscribeElapsed(stage, "1:23")},
		{"停滞满一分钟", services.LibraryTranscribeProgress{ItemID: shellBareVideoID, Stage: "transcribing", ElapsedMs: 83_000, StalledMs: ms(75_000)}, locales.LibraryTranscribeStalled(stage, "1:23", "1:15")},
		{"排版阶段", services.LibraryTranscribeProgress{ItemID: shellBareVideoID, Stage: "formatting", ElapsedMs: 5000}, locales.LibraryTranscribeElapsed(locales.LibraryStageFormatting(), "0:05")},
	} {
		r.svc.EmitTranscribeProgress(c.p)
		r.frames(3)
		if n, ok := r.node(gfx.RoleButton, c.want); !ok || !n.Disabled {
			t.Errorf("%s：按钮应叫「%s」且禁用（有：%v）", c.name, c.want, ok)
		}
	}
	release()
	wg.Wait()
	r.frames(3)
	if r.hasText(locales.LibraryTranscriptEmpty()) {
		t.Fatal("转写结束写回之后不该还是空态")
	}
}

func TestTranscribeStatusTextAndFormatClock(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		ms   int64
		want string
	}{
		{0, "0:00"}, {499, "0:00"}, {500, "0:01"}, {1499, "0:01"}, {1500, "0:02"},
		{59_499, "0:59"}, {59_500, "1:00"}, {83_000, "1:23"}, {3_600_000, "60:00"}, {-5000, "0:00"},
	} {
		if got := formatClock(c.ms); got != c.want {
			t.Errorf("formatClock(%d) = %q，应为 %q", c.ms, got, c.want)
		}
	}
	ms := func(v int64) *int64 { return &v }
	transcribing := locales.LibraryStageTranscribing()
	for _, c := range []struct {
		name string
		p    *services.LibraryTranscribeProgress
		want string
	}{
		{"还没有推送", nil, locales.LibraryTranscribeRunning()},
		{"转写阶段", &services.LibraryTranscribeProgress{Stage: "transcribing", ElapsedMs: 5000}, locales.LibraryTranscribeElapsed(transcribing, "0:05")},
		{"排版阶段", &services.LibraryTranscribeProgress{Stage: "formatting", ElapsedMs: 5000}, locales.LibraryTranscribeElapsed(locales.LibraryStageFormatting(), "0:05")},
		{"总结阶段", &services.LibraryTranscribeProgress{Stage: "summarizing", ElapsedMs: 5000}, locales.LibraryTranscribeElapsed(locales.LibraryStageSummarizing(), "0:05")},
		{"不认得的阶段按转写算", &services.LibraryTranscribeProgress{Stage: "mystery", ElapsedMs: 5000}, locales.LibraryTranscribeElapsed(transcribing, "0:05")},
		{"没有停滞数据", &services.LibraryTranscribeProgress{Stage: "transcribing", ElapsedMs: 5000, StalledMs: nil}, locales.LibraryTranscribeElapsed(transcribing, "0:05")},
		{"停滞差一毫秒", &services.LibraryTranscribeProgress{Stage: "transcribing", ElapsedMs: 5000, StalledMs: ms(59_999)}, locales.LibraryTranscribeElapsed(transcribing, "0:05")},
		{"停滞满一分钟", &services.LibraryTranscribeProgress{Stage: "transcribing", ElapsedMs: 5000, StalledMs: ms(60_000)}, locales.LibraryTranscribeStalled(transcribing, "0:05", "1:00")},
	} {
		if got := transcribeStatusText(c.p); got != c.want {
			t.Errorf("%s：得到 %q，应为 %q", c.name, got, c.want)
		}
	}
}

// ---- 文字稿正文态 ----

// textRow 是文字稿视口里画出的一行：行顶与拼起来的文字。
type textRow struct {
	y    float32
	text string
}

// transcriptRows 是文字稿视口里画出的每一行文字（同一行里被标记切开的片段按横坐标拼起来），自上而下。
func (r *rig) transcriptRows() []textRow {
	vp := r.screen.detail.panel.tx.vp
	type frag struct {
		x float32
		v string
	}
	byY := map[int][]frag{}
	for _, tx := range r.session.Snapshot().Texts {
		if tx.Bounds.Y+tx.Bounds.Height < vp.Y || tx.Bounds.Y > vp.Y+vp.Height || tx.Bounds.X < vp.X {
			continue
		}
		key := int(tx.Bounds.Y + 0.5)
		byY[key] = append(byY[key], frag{tx.Bounds.X, tx.Value})
	}
	var rows []textRow
	for y, frags := range byY {
		sort.Slice(frags, func(i, j int) bool { return frags[i].x < frags[j].x })
		var sb strings.Builder
		for _, f := range frags {
			sb.WriteString(f.v)
		}
		rows = append(rows, textRow{float32(y), sb.String()})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].y < rows[j].y })
	return rows
}

func rowTexts(rows []textRow) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.text
	}
	return out
}

// 正文保留段落空行、连续空格，超长行按宽折行，且不超过 48rem。
func TestTranscriptBodyKeepsLayoutAndWraps(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.openTranscript(mediaParaID)
	rows := r.transcriptRows()
	index := func(text string) int {
		for i, row := range rows {
			if row.text == text {
				return i
			}
		}
		t.Fatalf("文字稿里找不到整行 %q；画出的行：%q", text, rowTexts(rows))
		return -1
	}
	first, second, third := index("第一段。"), index("第二段  含两个空格。"), -1
	line := r.look.Rem(2)
	if d := rows[second].y - rows[first].y; d < 2*line-1 || d > 2*line+1 {
		t.Errorf("两段之间的空行应占一整行：相距 %.1f，应为 %.1f", d, 2*line)
	}
	for i := second + 1; i < len(rows); i++ {
		if strings.HasPrefix(rows[i].text, "长") {
			third = i
			break
		}
	}
	if third < 0 {
		t.Fatalf("找不到超长行；画出的行：%q", rowTexts(rows))
	}
	var wrapped strings.Builder
	n := 0
	for i := third; i < len(rows) && strings.HasPrefix(rows[i].text, "长"); i++ {
		wrapped.WriteString(rows[i].text)
		n++
		if i > third {
			if d := rows[i].y - rows[i-1].y; d < line-1 || d > line+1 {
				t.Errorf("折行的行距应是 %.1f，实际 %.1f", line, d)
			}
		}
	}
	if wrapped.String() != strings.Repeat("长", 100) || n < 2 {
		t.Errorf("超长行应折成多行且一字不少：%d 行、%d 字", n, len([]rune(wrapped.String())))
	}
	for _, tx := range r.session.Snapshot().Texts {
		if strings.HasPrefix(tx.Value, "长") && tx.Bounds.Width > r.look.Rem(48)+1 {
			t.Errorf("正文宽不该超过 48rem：%.1f", tx.Bounds.Width)
		}
	}
}

// 转写在途：正文上方出现进度胶囊并随推送更新；排版在途同理。
func TestTranscriptBodyShowsRunningAndFormatPills(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.svc.SeedTranscribe(mediaVideoOK(detailVideoID), nil)
	r.svc.SeedFormatTranscript(mediaVideoOK(detailVideoID), nil)
	r.openTranscript(detailVideoID)
	if !r.hasText("这是一段文字稿内容。") {
		t.Fatal("正文态应显示文字稿")
	}
	if r.hasText(locales.LibraryTranscribeRunning()) || r.hasText(locales.LibraryTranscriptFormatting()) {
		t.Fatal("没有动作在跑时不该有胶囊")
	}
	wg := r.spawnTracker()

	release := r.once(r.svc.HoldTranscribe())
	r.clickInDetail(gfx.RoleButton, locales.LibraryTranscribeRegenerate())
	if !r.hasText(locales.LibraryTranscribeRunning()) || !r.hasText("这是一段文字稿内容。") {
		t.Fatal("转写在途时应有胶囊，正文仍在")
	}
	r.svc.EmitTranscribeProgress(services.LibraryTranscribeProgress{ItemID: detailVideoID, Stage: "transcribing", ElapsedMs: 5000})
	r.frames(3)
	if want := locales.LibraryTranscribeElapsed(locales.LibraryStageTranscribing(), "0:05"); !r.hasText(want) {
		t.Fatalf("胶囊应更新成「%s」", want)
	}
	release()
	wg.Wait()
	r.frames(3)
	if r.hasText(locales.LibraryTranscribeElapsed(locales.LibraryStageTranscribing(), "0:05")) {
		t.Fatal("转写结束之后胶囊应消失")
	}

	release = r.once(r.svc.HoldFormatTranscript())
	r.clickInDetail(gfx.RoleButton, locales.LibraryTranscriptFormat())
	if !r.hasText(locales.LibraryTranscriptFormatting()) {
		t.Fatal("排版在途时应有胶囊")
	}
	r.svc.EmitFormatProgress(services.LibraryTranscriptFormatProgress{ItemID: detailVideoID, Current: 1, Total: 3})
	r.frames(3)
	if want := locales.LibraryTranscriptFormatProgress(1, 3); !r.hasText(want) {
		t.Fatalf("胶囊应更新成「%s」", want)
	}
	release()
	wg.Wait()
	r.frames(3)
	if r.hasText(locales.LibraryTranscriptFormatProgress(1, 3)) {
		t.Fatal("排版结束之后胶囊应消失")
	}
}

// ---- 页内查找 ----

// markedBounds 是画面上标记字色（amber-950）的文字的外框。
func (r *rig) markedBounds() []gfx.Rect {
	want := r.look.Col(tokens.Tailwind("amber-950"))
	var out []gfx.Rect
	for _, tx := range r.session.Snapshot().Texts {
		if tx.Style.Color == want {
			out = append(out, tx.Bounds)
		}
	}
	return out
}

// clearFindField 清空查找框（无头会话没有「全选」键，按退格清到空）。
func (r *rig) clearFindField() {
	r.t.Helper()
	r.click(gfx.RoleTextField, locales.LibraryPanelFindPlaceholder())
	r.keys("end")
	for range 4 {
		r.keys("backspace")
	}
}

func TestTranscriptFindHighlightsCountsAndSteps(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.openTranscript(mediaFindID)
	r.keys("ctrl+f")
	r.typeText("命运")
	if got := r.markedTexts(); !slices.Equal(got, []string{"命运", "命运", "命运"}) {
		t.Fatalf("三处命中都应标出：%q", got)
	}
	if !r.hasText(locales.LibraryPanelFindMatchIndex(1, 3)) || r.screen.detail.panel.findCount != 3 {
		t.Fatalf("命中计数应是 1/3（findCount=%d）", r.screen.detail.panel.findCount)
	}
	next, prev := locales.LibraryPanelFindNextMatch(), locales.LibraryPanelFindPrevMatch()
	for _, step := range []struct {
		click string
		want  int
	}{{next, 2}, {next, 3}, {next, 1}, {prev, 3}} {
		r.click(gfx.RoleButton, step.click)
		if !r.hasText(locales.LibraryPanelFindMatchIndex(step.want, 3)) {
			t.Fatalf("点「%s」之后应是 %d/3（循环）", step.click, step.want)
		}
	}

	// 大小写不敏感，标出的是原文的写法。
	r.clearFindField()
	r.typeText("abc")
	if got := r.markedTexts(); !slices.Equal(got, []string{"Abc", "abc", "ABC"}) {
		t.Fatalf("大小写不同也应命中并保留原文写法：%q", got)
	}
	if !r.hasText(locales.LibraryPanelFindMatchIndex(1, 3)) {
		t.Fatal("改了查找词之后命中序号应回到第一处")
	}

	// 渲染层的高亮只看查找词、不看查找条开着与否：关掉查找条，高亮还在。
	r.click(gfx.RoleButton, locales.CommonClose())
	if got := r.markedTexts(); len(got) != 3 {
		t.Fatalf("关掉查找条之后高亮应还在（与渲染层一致）：%q", got)
	}

	r.keys("ctrl+f")
	r.clearFindField()
	r.typeText("不存在的词")
	if got := r.markedTexts(); len(got) != 0 {
		t.Fatalf("没有命中时不该有标记：%q", got)
	}
	if !r.hasText(locales.LibraryPanelFindEmptyShort()) {
		t.Fatalf("没有命中应显示「%s」", locales.LibraryPanelFindEmptyShort())
	}
}

// 当前命中滚到视口正中；只画看得见的行（长文字稿不是整篇都进画面）。
func TestTranscriptFindScrollsTheActiveMatchToTheCenter(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.openTranscript(mediaScrollID)
	if !r.hasText(scrollLine(1)) {
		t.Fatal("打开时应在顶部")
	}
	if r.hasText(scrollLine(60)) {
		t.Fatal("视口之外的行不该画出来")
	}
	r.keys("ctrl+f")
	r.typeText("目标")
	if !r.hasText(locales.LibraryPanelFindMatchIndex(1, 2)) {
		t.Fatal("夹具失效：应有两处命中")
	}
	vp := r.screen.detail.panel.tx.vp
	center := vp.Y + vp.Height/2
	r.click(gfx.RoleButton, locales.LibraryPanelFindNextMatch())
	marks := r.markedBounds()
	if len(marks) != 1 {
		t.Fatalf("第二处命中滚进视口之后应只有它一处标记：%d", len(marks))
	}
	if mid := marks[0].Y + marks[0].Height/2; mid < center-r.look.Rem(1) || mid > center+r.look.Rem(1) {
		t.Errorf("当前命中应在视口正中（%.1f）附近，实际 %.1f", center, mid)
	}
	if r.hasText(scrollLine(1)) {
		t.Error("滚到下面之后顶部的行不该再画")
	}
	if r.screen.detail.panel.tx.top <= 0 {
		t.Error("滚动量应大于 0")
	}
	r.click(gfx.RoleButton, locales.LibraryPanelFindPrevMatch())
	if r.screen.detail.panel.tx.top != 0 || !r.hasText(scrollLine(1)) {
		t.Errorf("回到第一处命中应滚回顶部（top=%.1f）", r.screen.detail.panel.tx.top)
	}
}

// mediaWrapQuery 够长（120 个字，文字稿栏最宽 48rem、每行不到 50 个字），命中一定折成三行以上。
var mediaWrapQuery = strings.Repeat("长", 120)

// mediaWrapTranscript 是 60 行，第 40 行里的 mediaWrapQuery 折成多行。
func mediaWrapTranscript() string {
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = scrollLine(i + 1)
	}
	lines[39] = "第40段，" + mediaWrapQuery
	return strings.Join(lines, "\n")
}

// 命中折成多行时，滚到视口正中的是整个命中（所有行的并集）的中心，而不是它第一行的中心：
// 渲染层 scrollElementIntoContainer 取 <mark> 的 getBoundingClientRect，跨行的内联元素外框是各行的并集。
func TestTranscriptFindCentersAWrappedMatchAsAWhole(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	r.openTranscript(mediaWrapID)
	r.keys("ctrl+f")
	r.typeText(mediaWrapQuery)
	if !r.hasText(locales.LibraryPanelFindMatchIndex(1, 1)) {
		t.Fatal("夹具失效：应有一处命中")
	}
	marks := r.markedBounds()
	if len(marks) < 3 {
		t.Fatalf("夹具失效：命中应折成至少三行，实际 %d 片", len(marks))
	}
	top, bottom := marks[0].Y, marks[0].Y+marks[0].Height
	for _, m := range marks[1:] {
		top, bottom = min(top, m.Y), max(bottom, m.Y+m.Height)
	}
	vp := r.screen.detail.panel.tx.vp
	if mid, center := (top+bottom)/2, vp.Y+vp.Height/2; mid < center-r.look.Rem(0.5) || mid > center+r.look.Rem(0.5) {
		t.Errorf("折行命中的整体应在视口正中（%.1f）附近，实际 %.1f（%d 片，%.1f–%.1f）", center, mid, len(marks), top, bottom)
	}
}

// ---- 阅读位置 ----

func (r *rig) setNow(d time.Duration) {
	r.screen.Now = func() time.Time { return rigNow.Add(d) }
}

func (r *rig) wheelTranscript(dy float32) {
	r.t.Helper()
	vp := r.screen.detail.panel.tx.vp
	if err := r.session.Scroll(gfx.ScrollEvent{X: vp.X + vp.Width/2, Y: vp.Y + vp.Height/2, DeltaY: dy, Precise: true}); err != nil {
		r.t.Fatalf("滚轮失败：%v", err)
	}
	r.frames(3)
}

func (r *rig) savedTranscriptTop(id string) (float64, bool) {
	mem, ok := LoadReadingMemory(r.prefs, id, rigNow)
	if !ok {
		return 0, false
	}
	top, ok := mem.ScrollTopByTab[TabTranscript]
	return top, ok
}

func TestTranscriptRestoresTheRememberedPosition(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	if err := PatchReadingMemory(r.prefs, mediaScrollID, ReadingPatch{Tab: TabTranscript, ScrollTopByTab: map[ReadingTab]float64{TabTranscript: 640}}, rigNow); err != nil {
		t.Fatal(err)
	}
	r.pick(mediaScrollID)
	r.frames(8)
	if r.screen.detail.panel.tab != TabTranscript {
		t.Fatalf("记忆里的页签是文字稿，应直接打开它：%q", r.screen.detail.panel.tab)
	}
	if got := r.screen.detail.panel.tx.top; got != 640 {
		t.Fatalf("应恢复到记忆里的 640，实际 %.1f", got)
	}
	if !r.hasText(scrollLine(22)) || r.hasText(scrollLine(1)) {
		t.Fatalf("恢复之后应从第 21 段附近开始：%q", rowTexts(r.transcriptRows()))
	}
	if top, _ := r.savedTranscriptTop(mediaScrollID); top != 640 {
		t.Fatalf("只是恢复不该改写记忆：%v", top)
	}
}

// 滚动之后 200 ms 才写进记忆；离开这一页（换页签、换条目）时立即补写。
func TestTranscriptScrollIsSavedAfterTheDebounceAndOnLeaving(t *testing.T) {
	t.Parallel()
	r := mediaRig(t)
	if err := PatchReadingMemory(r.prefs, mediaScrollID, ReadingPatch{Tab: TabTranscript, ScrollTopByTab: map[ReadingTab]float64{TabTranscript: 640}}, rigNow); err != nil {
		t.Fatal(err)
	}
	r.pick(mediaScrollID)
	r.frames(8)

	r.wheelTranscript(200)
	top := r.screen.detail.panel.tx.top
	if top <= 640 {
		t.Fatalf("滚轮之后应往下滚：%.1f", top)
	}
	if saved, _ := r.savedTranscriptTop(mediaScrollID); saved != 640 {
		t.Fatalf("防抖没到点不该写：%v", saved)
	}
	r.setNow(100 * time.Millisecond)
	r.clock.Advance(100 * time.Millisecond)
	r.frames(2)
	if saved, _ := r.savedTranscriptTop(mediaScrollID); saved != 640 {
		t.Fatalf("100 ms 时还不该写：%v", saved)
	}
	r.setNow(250 * time.Millisecond)
	r.clock.Advance(150 * time.Millisecond)
	r.frames(2)
	if saved, _ := r.savedTranscriptTop(mediaScrollID); saved != float64(top) {
		t.Fatalf("200 ms 之后应写入 %.1f，实际 %v", top, saved)
	}

	// 换页签：还没到点的那一笔立即补上。
	r.wheelTranscript(200)
	top2 := r.screen.detail.panel.tx.top
	if saved, _ := r.savedTranscriptTop(mediaScrollID); top2 == top || saved != float64(top) {
		t.Fatalf("夹具失效：第二次滚动应有新位置且尚未写入（top2=%.1f saved=%v）", top2, saved)
	}
	r.clickPrefix(gfx.RoleButton, locales.LibraryBodySection())
	if saved, _ := r.savedTranscriptTop(mediaScrollID); saved != float64(top2) {
		t.Fatalf("换页签应立即补写 %.1f，实际 %v", top2, saved)
	}

	// 换条目：同理。
	r.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
	r.frames(4)
	r.wheelTranscript(300)
	top3 := r.screen.detail.panel.tx.top
	if saved, _ := r.savedTranscriptTop(mediaScrollID); top3 == top2 || saved != float64(top2) {
		t.Fatalf("夹具失效：第三次滚动应有新位置且尚未写入（top3=%.1f saved=%v）", top3, saved)
	}
	r.pick(detailNoteID)
	if saved, _ := r.savedTranscriptTop(mediaScrollID); saved != float64(top3) {
		t.Fatalf("换条目应立即补写 %.1f，实际 %v", top3, saved)
	}
}

// 文字稿长度按 UTF-16 单元数（与渲染层 transcript.length 同口径）：页签后缀与长稿门槛用的是同一个数。
func TestTranscriptLongFixtureMatchesTheUTF16Count(t *testing.T) {
	t.Parallel()
	if got := len(utf16.Encode([]rune(jsTrim(mediaLongTranscript())))); got != 52_699 {
		t.Fatalf("夹具的 UTF-16 长度应是 52699，实际 %d", got)
	}
}
