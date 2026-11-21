package library

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// TestDetailFrameBudget 读出卡片视图里打开条目详情之后的帧耗时（模块完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）。
//
// 库里有 1 万条，列表一页 20 条（默认页大小），右侧是详情栏（头部、内容面板工具栏与标签页；阅读区是留白，台账 B-059）。
// 量四个动作：稳态帧、悬停一步（指针在头部两枚按钮之间来回）、键盘切换一步（↓ 选下一条，详情随之重取与重排）、
// 标题里敲一个字（输入 + 出帧；防抖保存的计时由测试时钟接管，读数里不含保存的往返）。
// 另一组带着页内查找条打开（Ctrl+F），看工具栏下多一行输入的成本。
// 公众号快照一组（M2-5b）：公众号条目切到「网页快照」视图（快照工具栏、已编辑与新版本提示条、缺失资源与警告条），看原生外壳的稳态成本。
// 文字稿一组（M2-6d）：视频条目挂约 5 万字文字稿，切到「文字稿」页签并开着查找高亮（上千处命中），看长稿 pre-wrap 排版的稳态成本。
//
// 无头会话的 Frame() 只记录绘制命令、不栅格化，所以量的是界面线程的出帧成本（布局 + 记录）；栅格化加回读另量一次作为上界。
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/library -run TestDetailFrameBudget -v -count=1。
// 每个读数前先核对操作确实改变了画面，不让「什么都没做」的空操作混进读数。
func TestDetailFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	t.Run("detail_10000_entries", func(t *testing.T) { measureDetailFrames(t, "detail_10000_entries", false) })
	t.Run("detail_10000_entries_find", func(t *testing.T) { measureDetailFrames(t, "detail_10000_entries_find", true) })
	t.Run("detail_10000_entries_tools", func(t *testing.T) { measureDetailToolsFrames(t, "detail_10000_entries_tools", "") })
	t.Run("detail_10000_entries_tools_summary_expanded", func(t *testing.T) {
		measureDetailToolsFrames(t, "detail_10000_entries_tools_summary_expanded", frameBudgetSummary)
	})
	t.Run("detail_10000_entries_transcript_50k", func(t *testing.T) { measureDetailTranscriptFrames(t, "detail_10000_entries_transcript_50k") })
	t.Run("detail_10000_entries_snapshot", func(t *testing.T) { measureDetailSnapshotFrames(t, "detail_10000_entries_snapshot") })
}

// frameBudgetTranscript 造一份约 5 万字的文字稿：一千多个短段落，每段都含查找词「命运」，开启页内查找之后满屏高亮（M2-6d 的长稿成本）。
func frameBudgetTranscript() string {
	var b strings.Builder
	for i := 0; utf8.RuneCountInString(b.String()) < 50_000; i++ {
		fmt.Fprintf(&b, "第%04d段：这是一段用于帧耗时读数的文字稿内容，提到了命运石之门，也提到了其他普通的话。\n", i)
	}
	return b.String()
}

// measureDetailTranscriptFrames 在 1 万条的库里选中一条在线视频（挂约 5 万字的文字稿），切到「文字稿」页签并开着页内查找高亮，
// 量稳态帧、悬停一步（工具栏两枚钮之间）、滚轮一步（文字稿正文）、Go 堆增量与栅格化上界（M2-6d：几千行的 pre-wrap 排版，只画看得见的行）。
func measureDetailTranscriptFrames(t *testing.T, name string) {
	heapBefore := frametime.HeapInUse()
	const videoID = "item-000050" // GenerateEntries 里每 50 条置顶一次、置顶的排在第一页；第 51 条恰好是视频（active、不在回收站）
	transcript := frameBudgetTranscript()
	r := wsRig(t, func(r *rig) {
		r.svc.SeedEntries(servicestest.GenerateEntries(10000))
		r.svc.SeedDetail(videoID, servicestest.DetailBody{Content: "视频", Transcript: transcript, SourceURI: mediaBili})
	})
	entries := r.view().List.Entries
	idx := slices.IndexFunc(entries, func(e services.LibraryEntry) bool { return e.ID == videoID })
	if idx < 0 {
		t.Fatalf("第一页里找不到视频条目 %s（夹具失效）", videoID)
	}
	card, ok := r.cardOf(entries[idx])
	if !ok {
		t.Fatal("找不到视频卡片（夹具失效）")
	}
	cx, cy := centerOf(card)
	r.clickAt(cx, cy)
	r.frames(4)
	if d := r.view().Detail; d.Item == nil || d.Item.ID != videoID || len(d.Item.Transcript) < 50_000 {
		t.Fatalf("点卡片之后详情应是带长文字稿的 %s：%+v", videoID, d.Item)
	}
	r.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
	r.frames(4)
	if got := r.screen.detail.panel.tab; got != TabTranscript {
		t.Fatalf("应已切到文字稿页签，实际 %q", got)
	}
	r.keys("ctrl+f")
	r.typeText("命运")
	r.frames(4)
	if n := r.screen.detail.panel.findCount; n < 1000 {
		t.Fatalf("查找高亮应命中上千处（实际 %d），读数无效", n)
	}
	if len(r.markedBounds()) == 0 {
		t.Fatal("画面上应有高亮的命中词，读数无效")
	}
	t.Logf("%s 文字稿 %d 字（UTF-8 %d KiB）、命中 %d 处；打开后 Go 堆增量（含无头会话）：%d KiB", name,
		utf8.RuneCountInString(transcript), len(transcript)/1024, r.screen.detail.panel.findCount,
		(int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录）：%s", name, idle)

	format, ok1 := r.detailNode(gfx.RoleButton, locales.LibraryTranscriptFormat())
	regen, ok2 := r.detailNode(gfx.RoleButton, locales.LibraryTranscribeRegenerate())
	if !ok1 || !ok2 {
		t.Fatalf("工具栏里找不到「%s」或「%s」钮（%v、%v）", locales.LibraryTranscriptFormat(), locales.LibraryTranscribeRegenerate(), ok1, ok2)
	}
	ax, ay := centerOf(format)
	bx, by := centerOf(regen)
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

	// 滚轮一步：在文字稿正文上来回滚；先核对滚动量和画面上的文字确实变了。
	vp := r.screen.detail.panel.tx.vp
	wx, wy := vp.X+vp.Width/2, vp.Y+vp.Height/2
	topBefore, textBefore := r.screen.detail.panel.tx.top, r.screenText()
	if err := r.session.Scroll(gfx.ScrollEvent{X: wx, Y: wy, DeltaY: 400, Precise: true}); err != nil {
		t.Fatalf("预检滚轮失败：%v", err)
	}
	r.frames(2)
	if r.screen.detail.panel.tx.top == topBefore || r.screenText() == textBefore {
		t.Fatalf("滚轮之后文字稿没有滚动（top %.1f → %.1f），滚动读数无效", topBefore, r.screen.detail.panel.tx.top)
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

	raster, err := frametime.Measure(30, func(int) error {
		_, err := r.session.Image()
		return err
	})
	if err != nil {
		t.Fatalf("栅格化失败：%v", err)
	}
	t.Logf("%s 栅格化+回读（Image，上界）：%s", name, raster)
}

// measureDetailSnapshotFrames 在 1 万条的库里选中一条公众号文章（置顶的条目改成网页类型、来源是公众号链接），点「网页快照」进入快照视图，
// 视图带「已编辑」「有新来源版本」与缺失资源 / 警告通知条，工具栏里有「目录」钮、宽度下拉与「更多」。
// 量稳态帧、悬停一步（「目录」钮与「更多」之间）、Go 堆增量与栅格化上界（M2-5b）。阅读区里的快照 iframe 是阅读岛的内容，无头会话里画不出来，不在读数里。
func measureDetailSnapshotFrames(t *testing.T, name string) {
	heapBefore := frametime.HeapInUse()
	const wechatID = "item-000050" // 置顶的条目排在第一页；改成网页类型当作公众号文章
	all := servicestest.GenerateEntries(10000)
	if all[50].ID != wechatID {
		t.Fatalf("夹具失效：第 51 条是 %s", all[50].ID)
	}
	all[50].ItemType = services.LibraryTypeWebpage
	view := wsView("v2")
	view.Edited, view.Pending = true, true
	view.Version.Snapshot.Warnings = []string{"帧耗时读数警告：部分样式未能内联"}
	view.Version.Snapshot.Failures = []services.LibraryWebSnapshotFailure{{URL: "https://mmbiz.qpic.cn/a.png", Reason: "图片 404"}}
	r := wsRig(t, func(r *rig) {
		r.svc.SeedEntries(all)
		r.svc.SeedDetail(wechatID, servicestest.DetailBody{Content: "公众号正文", SourceURI: "https://mp.weixin.qq.com/s/frame-budget"})
		r.svc.SeedSnapshot(wechatID, "", view)
	})
	entries := r.view().List.Entries
	idx := slices.IndexFunc(entries, func(e services.LibraryEntry) bool { return e.ID == wechatID })
	if idx < 0 {
		t.Fatalf("第一页里找不到公众号条目 %s（夹具失效）", wechatID)
	}
	card, ok := r.cardOf(entries[idx])
	if !ok {
		t.Fatal("找不到公众号卡片（夹具失效）")
	}
	cx, cy := centerOf(card)
	r.clickAt(cx, cy)
	r.frames(4)
	if d := r.view().Detail; d.Item == nil || d.Item.ID != wechatID {
		t.Fatalf("点卡片之后详情应是 %s：%+v", wechatID, d.Item)
	}
	r.click(gfx.RoleButton, locales.ArticleAskViewSnapshot())
	r.frames(4)
	// 无头会话没有岛，页面不会回报版面状态：目录不常驻侧栏，工具栏里要有「目录」钮。
	r.screen.ReportSnapshotLayout(false, false)
	r.frames(2)
	sn := r.screen.detail.panel.snap
	if sn.view == nil || sn.loading || sn.err != "" {
		t.Fatalf("快照应已读到：%+v", sn)
	}
	// 已编辑的快照默认进标准排版；点「查看网页快照」切到原文快照，工具栏才有宽度下拉与「目录」钮。
	if sn.simple {
		r.click(gfx.RoleButton, locales.LibrarySnapshotViewOriginal())
		r.frames(3)
	}
	if !r.screen.detail.panel.snap.original() {
		t.Fatal("应是原文快照，读数无效")
	}
	text := r.screenText()
	for _, want := range []string{"帧耗时读数警告", "图片 404", locales.LibrarySnapshotPendingVersion()} {
		if !strings.Contains(text, want) {
			t.Fatalf("画面上应有通知条「%s」，读数无效；画面文字 %q", want, r.session.Texts())
		}
	}
	toc := r.mustNode(gfx.RoleButton, locales.LibraryCatalog())
	more := r.mustNode(gfx.RoleCombobox, locales.CommonMore())
	if !r.has(gfx.RoleCombobox, locales.LibrarySnapshotWidth()) {
		t.Fatal("原文快照的工具栏应有宽度下拉，读数无效")
	}
	t.Logf("%s 打开快照视图后 Go 堆增量（含无头会话）：%d KiB", name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录）：%s", name, idle)

	ax, ay := centerOf(toc)
	bx, by := centerOf(more)
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

	raster, err := frametime.Measure(30, func(int) error {
		_, err := r.session.Image()
		return err
	})
	if err != nil {
		t.Fatalf("栅格化失败：%v", err)
	}
	t.Logf("%s 栅格化+回读（Image，上界）：%s", name, raster)
}

// frameBudgetSummary 是展开态 AI 摘要读数用的摘要：含标题、段落（粗体、行内代码、链接）、无序列表与结论小节，覆盖 Markdown 排版的各类块（M2-6c）。
const frameBudgetSummary = "## 核心要点\n\n本文介绍 **本地优先** 知识库的设计，重点是 `SQLite` 的 WAL 模式与备份策略；同时讨论了 [同步方案](https://example.com/sync) 的取舍。\n\n- **并发**：goroutine 与 channel 的使用边界\n- 同步：`sync.Mutex` 与 `sync.RWMutex` 的选择\n- 备份：使用 `VACUUM INTO` 生成一致性快照\n\n### 结论\n\n优先保证数据可恢复，再考虑性能优化。"

// measureDetailToolsFrames 在 1 万条的库里选中第一条并打开文章工具侧栏，量稳态帧、悬停一步、滚轮一步与栅格化上界（M2-6b）。
// summary 非空时先给第一条写入这份摘要，打开侧栏后展开 AI 摘要卡片再量（M2-6c：Markdown 排版的帧成本）。
func measureDetailToolsFrames(t *testing.T, name, summary string) {
	heapBefore := frametime.HeapInUse()
	r := wsRig(t, func(r *rig) { r.svc.SeedEntries(servicestest.GenerateEntries(10000)) })
	entries := r.view().List.Entries
	if len(entries) < 3 {
		t.Fatalf("夹具应读到一页条目，实际 %d 条", len(entries))
	}
	card, ok := r.cardOf(entries[0])
	if !ok {
		t.Fatal("找不到第一张卡片（夹具失效）")
	}
	if summary != "" {
		r.svc.SeedDetail(entries[0].ID, servicestest.DetailBody{Content: "正文", Summary: summary})
	}
	cx, cy := centerOf(card)
	r.clickAt(cx, cy)
	r.frames(4)
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.ArticleReaderTools())
	r.frames(4)
	panel := r.panelRect()
	if panel.Width == 0 {
		t.Fatal("打开文章工具侧栏后未找到面板，读数无效")
	}
	toggleName := locales.LibraryAiSummaryGenerate()
	if summary != "" {
		// 展开摘要卡片：核对 Markdown 真的排出来了（标题与列表文字在画面上），否则读到的是折叠态。
		r.click(gfx.RoleButton, locales.LibraryAiSummaryExpand())
		r.frames(4)
		if !r.has(gfx.RoleButton, locales.LibraryAiSummaryCollapse()) || !strings.Contains(r.screenText(), "核心要点") || !strings.Contains(r.screenText(), "sync.Mutex") {
			t.Fatalf("展开摘要卡片之后应排出 Markdown 内容，读数无效；画面文字 %q", r.session.Texts())
		}
		toggleName = locales.LibraryAiSummaryCollapse()
		panel = r.panelRect()
	}
	t.Logf("%s 打开工具侧栏后 Go 堆增量（含无头会话）：%d KiB", name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录）：%s", name, idle)

	closeBtn, ok := r.nodeIn(panel, gfx.RoleButton, locales.ArticleReaderCloseTools())
	if !ok {
		t.Fatal("侧栏内找不到关闭钮")
	}
	copyBtn, ok := r.nodeIn(panel, gfx.RoleButton, toggleName)
	if !ok {
		t.Fatalf("侧栏内找不到「%s」钮", toggleName)
	}
	ax, ay := centerOf(closeBtn)
	bx, by := centerOf(copyBtn)
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

	// 滚动一步：侧栏打开时在左侧卡片列表上滚轮来回，先核对无障碍指纹确实变了。
	before := frametime.Fingerprint(r.session.Access())
	if err := r.session.Scroll(gfx.ScrollEvent{X: cx, Y: 400, DeltaY: 400, Precise: true}); err != nil {
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
		if err := r.session.Scroll(gfx.ScrollEvent{X: cx, Y: 400, DeltaY: dy, Precise: true}); err != nil {
			return err
		}
		return r.session.Frame()
	})
	if err != nil {
		t.Fatalf("滚轮帧失败：%v", err)
	}
	t.Logf("%s 滚轮一步（滚轮+出帧）：%s", name, scroll)

	raster, err := frametime.Measure(30, func(int) error {
		_, err := r.session.Image()
		return err
	})
	if err != nil {
		t.Fatalf("栅格化失败：%v", err)
	}
	t.Logf("%s 栅格化+回读（Image，上界）：%s", name, raster)
}

// measureDetailFrames 在 1 万条的库里选中第一条，再逐项量帧；find 为真时先打开页内查找条。
func measureDetailFrames(t *testing.T, name string, find bool) {
	heapBefore := frametime.HeapInUse()
	r := wsRig(t, func(r *rig) { r.svc.SeedEntries(servicestest.GenerateEntries(10000)) })
	entries := r.view().List.Entries
	if len(entries) < 3 {
		t.Fatalf("夹具应读到一页条目，实际 %d 条", len(entries))
	}
	card, ok := r.cardOf(entries[0])
	if !ok {
		t.Fatal("找不到第一张卡片（夹具失效）")
	}
	cx, cy := centerOf(card)
	r.clickAt(cx, cy)
	r.frames(4)
	if d := r.view().Detail; d.Item == nil || d.Item.ID != entries[0].ID {
		t.Fatalf("点卡片之后详情应是 %s：%+v", entries[0].ID, d)
	}
	if find {
		fields := r.textFieldCount()
		r.keys("ctrl+f")
		if got := r.textFieldCount(); got != fields+1 {
			t.Fatalf("Ctrl+F 之后应多出一个查找输入框（原 %d 个，现 %d 个），读数无效", fields, got)
		}
	}
	t.Logf("%s 打开详情后 Go 堆增量（含无头会话）：%d KiB", name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	// 稳态帧：画面不变，连出 300 帧。
	idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录）：%s", name, idle)

	// 悬停一步：指针在头部「专注阅读」与「收藏」两枚按钮之间来回，每次移动都改变悬停态。
	// 提问钮只出现在可提问的条目上（AskAvailable），夹具生成的条目不可提问，所以不用它；收藏钮的名字随条目状态变。
	focus := r.mustNode(gfx.RoleButton, locales.LibraryEnterFocusReading())
	favName := locales.LibraryFavorite()
	if r.view().Detail.Item.IsFavorite {
		favName = locales.LibraryUnfavorite()
	}
	fav := r.mustNode(gfx.RoleButton, favName)
	ax, ay := centerOf(focus)
	fx, fy := centerOf(fav)
	hover, err := frametime.Measure(200, func(i int) error {
		x, y := ax, ay
		if i%2 == 1 {
			x, y = fx, fy
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

	// 键盘切换一步：焦点不在输入框里时 ↓ 选下一条，详情随之重取（假服务同步返回）与重排；先预检选中项确实在动。
	if find {
		r.clickAt(cx, cy) // 查找框开着、键盘焦点在它里面：点一下卡片把焦点交回列表
		r.frames(3)
	}
	before := r.view().List.SelectedID
	if err := r.session.Keys("down"); err != nil {
		t.Fatalf("预检按键失败：%v", err)
	}
	r.frames(2)
	if r.view().List.SelectedID == before {
		t.Fatalf("按 ↓ 之后选中项没有变化，键盘读数无效")
	}
	if d := r.view().Detail; d.Item == nil || d.Item.ID != r.view().List.SelectedID {
		t.Fatalf("选中项变了但详情没有跟上：%+v", d)
	}
	keys, err := frametime.Measure(18, func(i int) error {
		key := "down"
		if i >= 9 {
			key = "up"
		}
		if err := r.session.Keys(key); err != nil {
			return err
		}
		return r.session.Frame()
	})
	if err != nil {
		t.Fatalf("按键帧失败：%v", err)
	}
	t.Logf("%s 键盘切换一步（按键+详情重排+出帧）：%s", name, keys)

	// 敲字一步：点进标题框、光标到末尾，每步敲一个字并出帧；预检标题确实变长。
	r.click(gfx.RoleTextField, locales.LibraryTitlePlaceholder())
	r.keys("end")
	titleBefore := len(r.titleBox())
	if err := r.session.Type("x"); err != nil {
		t.Fatalf("预检输入失败：%v", err)
	}
	r.frames(2)
	if len(r.titleBox()) != titleBefore+1 {
		t.Fatalf("敲字之后标题没有变长（%d → %d），输入读数无效", titleBefore, len(r.titleBox()))
	}
	typing, err := frametime.Measure(100, func(int) error {
		if err := r.session.Type("x"); err != nil {
			return err
		}
		return r.session.Frame()
	})
	if err != nil {
		t.Fatalf("敲字帧失败：%v", err)
	}
	t.Logf("%s 敲字一步（输入+出帧）：%s", name, typing)

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

// textFieldCount 是画面上文本输入框的个数（标题框、查找框……）。
func (r *rig) textFieldCount() int {
	n := 0
	for _, a := range r.session.Access() {
		if a.Role == gfx.RoleTextField {
			n++
		}
	}
	return n
}
