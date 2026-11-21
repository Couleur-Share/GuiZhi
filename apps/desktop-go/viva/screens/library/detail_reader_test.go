package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/island"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// 本文件测阅读区的界面一侧（detailreader.go、detailreadertoolbar.go）：岛里显示什么、界面状态怎样变成给页面的期望、页面事件怎样落回界面。
// 岛宿主（viva/reader）自己的握手、差量与校验在那个包里测过；这里用同一个宿主配一个假打开器，页面由测试扮演：
// 不拉起 WebView2，所以这些测试不覆盖真实页面里的渲染与交互（台账 R-15）。

type fakeReaderPage struct {
	cfg     island.Config
	posts   []string
	places  []gfx.Rect
	closes  int
	focuses int
	closed  func()
}

func (p *fakeReaderPage) Post(text string)                   { p.posts = append(p.posts, text) }
func (p *fakeReaderPage) Place(_ reader.Surface, b gfx.Rect) { p.places = append(p.places, b) }
func (p *fakeReaderPage) Focus()                             { p.focuses++ }
func (p *fakeReaderPage) Close()                             { p.closes++ }

// messages 是已发给页面的、种类为 kind 的消息（按发送顺序，解成通用对象）。
func (p *fakeReaderPage) messages(t *testing.T, kind string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, text := range p.posts {
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			t.Fatalf("发给页面的消息不是 JSON：%v\n%s", err, text)
		}
		if m["t"] == kind {
			out = append(out, m)
		}
	}
	return out
}

// last 是最近一条 kind 消息；没有就失败。
func (p *fakeReaderPage) last(t *testing.T, kind string) map[string]any {
	t.Helper()
	all := p.messages(t, kind)
	if len(all) == 0 {
		t.Fatalf("页面还没有收到 %q 消息；收到过 %s", kind, p.kinds(t))
	}
	return all[len(all)-1]
}

func (p *fakeReaderPage) kinds(t *testing.T) string {
	t.Helper()
	var ks []string
	for _, text := range p.posts {
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		ks = append(ks, fmt.Sprint(m["t"]))
	}
	return strings.Join(ks, ",")
}

// send 扮演页面向宿主发一条事件。
func (p *fakeReaderPage) send(t *testing.T, fields map[string]any) {
	t.Helper()
	text, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	p.cfg.OnMessage(string(text))
}

type fakeReaderOpener struct {
	pages []*fakeReaderPage
	opens int
	err   error
}

func (o *fakeReaderOpener) Open(cfg island.Config, closed func()) (reader.Page, error) {
	o.opens++
	if o.err != nil {
		return nil, o.err
	}
	p := &fakeReaderPage{cfg: cfg, closed: closed}
	o.pages = append(o.pages, p)
	return p, nil
}

func (o *fakeReaderOpener) last() *fakeReaderPage {
	if len(o.pages) == 0 {
		return nil
	}
	return o.pages[len(o.pages)-1]
}

// readerRig 是接了假岛宿主的详情夹具（四条加追加条目，侧栏加卡片列表）。
type readerRig struct {
	*rig
	opener *fakeReaderOpener
	host   *reader.Host
	opened []string
}

func newReaderRig(t *testing.T, mutate ...func(*rig)) *readerRig {
	t.Helper()
	rr := &readerRig{opener: &fakeReaderOpener{}}
	rr.host = reader.New(reader.Options{Opener: rr.opener})
	rr.rig = shellRig(t, append([]func(*rig){func(r *rig) {
		r.screen.SetReader(rr.host)
		r.screen.OpenExternal = func(url string) error { rr.opened = append(rr.opened, url); return nil }
	}}, mutate...)...)
	return rr
}

// page 是最近打开的页面；没有就失败。
func (rr *readerRig) page() *fakeReaderPage {
	rr.t.Helper()
	p := rr.opener.last()
	if p == nil {
		rr.t.Fatal("阅读区没有打开过岛")
	}
	return p
}

// ready 让最近打开的页面完成握手，再出几帧让宿主把差量发过去。
func (rr *readerRig) ready() *fakeReaderPage {
	rr.t.Helper()
	p := rr.page()
	p.send(rr.t, map[string]any{"t": "ready", "v": reader.ProtocolVersion})
	rr.frames(3)
	return p
}

// event 扮演页面发一条事件：带上页面当前这份内容的序号（宿主丢弃序号不符的事件），发完出几帧。
func (rr *readerRig) event(p *fakeReaderPage, fields map[string]any) {
	rr.t.Helper()
	fields["seq"] = p.last(rr.t, "view")["seq"]
	p.send(rr.t, fields)
	rr.frames(3)
}

// scrollEvent 扮演页面上报阅读位置：seq 取页面当前这份内容的序号（最近一条 view 消息的）。
func (rr *readerRig) scrollEvent(p *fakeReaderPage, top float64) {
	rr.t.Helper()
	seq := p.last(rr.t, "view")["seq"]
	p.send(rr.t, map[string]any{"t": "scroll", "seq": seq, "top": top})
	rr.frames(2)
}

// ---- 阅读区显示什么 ----

func TestReaderViewFollowsTheRendererBranches(t *testing.T) {
	t.Parallel()
	forum := services.LibraryItem{ItemType: services.LibraryTypeForum, Content: detailForumContent}
	note := services.LibraryItem{ItemType: services.LibraryTypeNote, Content: "# 标题\n\n正文"}
	image := services.LibraryItem{ItemType: services.LibraryTypeImage, Content: "文案段落\n\n## 图中文字\n\n识别出的字\n"}
	wechat := ItemCaps{IsWechat: true}
	for _, c := range []struct {
		name     string
		item     services.LibraryItem
		caps     ItemCaps
		snapshot bool
		tab      ReadingTab
		kind     reader.ViewKind
		source   string
	}{
		{"笔记正文走主题阅读的原文", note, ItemCaps{}, false, TabBody, reader.ViewOriginal, "body"},
		{"论坛的讨论总结页签读总结", forum, ItemCaps{}, false, TabSummary, reader.ViewOriginal, "summary"},
		{"论坛的正文页签读正文", forum, ItemCaps{}, false, TabBody, reader.ViewOriginal, "body"},
		{"论坛的讨论页签是楼层讨论", forum, ItemCaps{}, false, TabReplies, reader.ViewReplies, ""},
		{"配图页签是画廊", note, ItemCaps{}, false, TabImages, reader.ViewGallery, ""},
		{"图片条目的识别页签是 Markdown", image, ItemCaps{}, false, TabRecognized, reader.ViewMarkdown, ""},
		{"文字稿页签留给文字稿面板", note, ItemCaps{}, false, TabTranscript, reader.ViewNone, ""},
		{"公众号的网页快照还没读到文档时是标准排版", note, wechat, true, TabBody, reader.ViewOriginal, "body"},
		{"公众号不在快照里时仍是原文", note, wechat, false, TabBody, reader.ViewOriginal, "body"},
		{"没有这个页签的条目留白", note, ItemCaps{}, false, TabRecognized, reader.ViewNone, ""},
		{"没有讨论的条目讨论页签留白", note, ItemCaps{}, false, TabReplies, reader.ViewNone, ""},
	} {
		got := readerViewFor(c.item, c.caps, snapshotPane{view: c.snapshot}, c.tab)
		if got.Kind != c.kind || got.SourceKind != c.source {
			t.Errorf("%s：kind=%q source=%q，应为 %q / %q", c.name, got.Kind, got.SourceKind, c.kind, c.source)
		}
	}
	if got := readerViewFor(forum, ItemCaps{}, snapshotPane{}, TabReplies); !strings.Contains(got.Content, "用 VACUUM INTO。") || strings.Contains(got.Content, "大家认为") {
		t.Errorf("楼层讨论只该带讨论段：%q", got.Content)
	}
	if got := readerViewFor(forum, ItemCaps{}, snapshotPane{}, TabSummary); !strings.Contains(got.Content, "VACUUM INTO") || strings.Contains(got.Content, "如何备份") {
		t.Errorf("总结页签只该带总结段：%q", got.Content)
	}
	if got := readerViewFor(image, ItemCaps{}, snapshotPane{}, TabBody); got.Content != "文案段落" {
		t.Errorf("图片条目的正文页签读文案：%q", got.Content)
	}
	if got := readerViewFor(image, ItemCaps{}, snapshotPane{}, TabRecognized); !strings.Contains(got.Content, "识别出的字") || got.Tab != reader.TabRecognized {
		t.Errorf("识别页签读「图中文字」段：%+v", got)
	}
}

func TestReadingSourceTextHandlesEveryItemType(t *testing.T) {
	t.Parallel()
	forum := services.LibraryItem{ItemType: services.LibraryTypeForum, Content: detailForumContent}
	if text, ok := readingSourceText(forum, sourceSummary); !ok || !strings.Contains(text, "VACUUM INTO") {
		t.Errorf("论坛的总结：%q %v", text, ok)
	}
	if text, ok := readingSourceText(forum, sourceBody); !ok || !strings.Contains(text, "如何备份") || strings.Contains(text, "大家认为") {
		t.Errorf("论坛的正文：%q %v", text, ok)
	}
	note := services.LibraryItem{ItemType: services.LibraryTypeNote, Content: "正文"}
	if text, ok := readingSourceText(note, sourceBody); !ok || text != "正文" {
		t.Errorf("笔记的正文：%q %v", text, ok)
	}
	if _, ok := readingSourceText(note, sourceSummary); ok {
		t.Error("非论坛条目没有「总结」这一份")
	}
	image := services.LibraryItem{ItemType: services.LibraryTypeImage, Content: "文案\n\n## 图中文字\n\n字\n"}
	if text, ok := readingSourceText(image, sourceBody); !ok || text != "文案" {
		t.Errorf("图片条目的正文只取文案：%q %v", text, ok)
	}
}

// ---- 打开、握手与外观 ----

func TestOpeningAnItemOpensOneIslandAndSendsTheViewAndTheTheme(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	if rr.opener.opens != 1 {
		t.Fatalf("选中一条应只开一次岛：%d", rr.opener.opens)
	}
	p := rr.ready()
	view := p.last(t, "view")["view"].(map[string]any)
	if view["kind"] != "original" || view["sourceKind"] != "body" || !strings.Contains(fmt.Sprint(view["content"]), "并发模式") {
		t.Errorf("笔记应以原文的正文打开：%v", view)
	}
	if p.last(t, "view")["itemId"] != detailNoteID || p.last(t, "view")["tab"] != "body" {
		t.Errorf("view 消息要带条目与标签：%v", p.last(t, "view"))
	}
	init := p.last(t, "init")
	theme := init["theme"].(map[string]any)
	want := readerTheme(rr.look, 1280)
	if theme["dark"] != want.Dark || theme["hue"] != want.Hue || theme["saturation"] != want.Saturation || theme["basePx"] != want.BasePx {
		t.Errorf("外观与界面不一致：%v 应为 %+v", theme, want)
	}
	// 响应式断点按窗口宽度判定：岛只有阅读区那么宽（592 px 量过），页面却要按窗口宽度（1280）算 sm / xl 这些断点。
	if theme["viewportWidth"] != float64(1280) {
		t.Errorf("外观要带窗口宽度 1280，不是阅读区宽度：%v", theme["viewportWidth"])
	}
	if init["lang"] != "zh" {
		t.Errorf("界面语言应是中文：%v", init["lang"])
	}
	before := len(p.posts)
	rr.frames(20)
	if len(p.posts) != before || rr.opener.opens != 1 {
		t.Errorf("什么都没变时逐帧重画不该再发消息或重开岛：%d → %d 条，开了 %d 次", before, len(p.posts), rr.opener.opens)
	}
}

func TestSwitchingItemsSendsANewViewToTheSameIsland(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.pick(detailNoteID)
	p := rr.ready()
	rr.pick(detailForumID)
	if rr.opener.opens != 1 {
		t.Fatalf("换条目复用同一座岛：开了 %d 次", rr.opener.opens)
	}
	views := p.messages(t, "view")
	if len(views) != 2 || views[1]["itemId"] != detailForumID {
		t.Fatalf("换条目应发一条新的 view：%v", views)
	}
	if seq := views[1]["seq"].(float64); seq <= views[0]["seq"].(float64) {
		t.Errorf("序号应递增：%v → %v", views[0]["seq"], seq)
	}
}

func TestTheIslandClosesWhenNothingIsShown(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.pick(detailVideoID)
	rr.ready()
	rr.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
	if rr.page().closes != 1 || rr.host.Open() {
		t.Fatalf("文字稿页签没有岛内容（B-067），岛应关掉：closes=%d open=%v", rr.page().closes, rr.host.Open())
	}
	rr.click(gfx.RoleButton, locales.LibraryBodySection())
	if rr.opener.opens != 2 || !rr.host.Open() {
		t.Fatalf("回到正文应重新开岛：opens=%d", rr.opener.opens)
	}

	rr.ready()
	rr.wsHidden = true
	rr.frames(3)
	if rr.host.Open() {
		t.Error("切走知识库（这一帧没画工作区）应关岛")
	}
}

// ---- 失败 ----

func TestAnIslandThatCannotOpenShowsALoadErrorAndRetries(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.opener.err = errors.New("WebView2 运行时不可用")
	rr.openCard(detailNoteID)
	if !rr.hasText(locales.CommonLoadFailed()) || !strings.Contains(rr.screenText(), "WebView2 运行时不可用") {
		t.Fatalf("打不开岛应画「读取失败」并给出原因：\n%s", rr.screenText())
	}
	before := rr.opener.opens
	rr.frames(10)
	if rr.opener.opens != before {
		t.Fatalf("同一份内容失败后不该逐帧重试：%d → %d", before, rr.opener.opens)
	}
	rr.opener.err = nil
	rr.click(gfx.RoleButton, locales.CommonRetry())
	rr.frames(2)
	if rr.opener.opens != before+1 || rr.hasText(locales.CommonLoadFailed()) {
		t.Fatalf("点重试应再开一次、失败态消失：opens=%d", rr.opener.opens)
	}
}

// ---- 页内查找 ----

func TestFindBarDrivesThePageAndShowsTheMatchCount(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.keys("ctrl+f")
	rr.typeText("并发")
	find := p.last(t, "find")
	if find["query"] != "并发" || find["index"] != float64(0) {
		t.Fatalf("输入应发给页面做高亮：%v", find)
	}
	rr.event(p, map[string]any{"t": "matches", "count": 3})
	rr.frames(2)
	if !rr.hasText(locales.LibraryPanelFindMatchIndex(1, 3)) {
		t.Fatalf("命中数应显示在查找条上：\n%s", rr.screenText())
	}
	rr.click(gfx.RoleButton, locales.LibraryPanelFindNextMatch())
	if got := p.last(t, "find")["index"]; got != float64(1) {
		t.Fatalf("下一个应把序号推到 1：%v", got)
	}
	rr.click(gfx.RoleButton, locales.CommonClose())
	if got := p.last(t, "find")["query"]; got != "" {
		t.Fatalf("关查找条应撤掉页面里的高亮：%v", got)
	}
}

func TestDiscussionFilterKeepsWorkingAfterTheFindBarCloses(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailForumID)
	rr.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	p := rr.ready()
	rr.keys("ctrl+f")
	rr.typeText("李四")
	if got := p.last(t, "find")["query"]; got != "李四" {
		t.Fatalf("讨论过滤词应发给页面：%v", got)
	}
	rr.click(gfx.RoleButton, locales.CommonClose())
	if got := p.last(t, "find")["query"]; got != "李四" {
		t.Fatalf("关查找条不撤讨论的过滤词（渲染层 query={findQuery}）：%v", got)
	}
}

func TestPageRequestedFindResetClearsTheQuery(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailForumID)
	rr.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	p := rr.ready()
	rr.keys("ctrl+f")
	rr.typeText("张三")
	rr.event(p, map[string]any{"t": "findReset"})
	rr.frames(3)
	if got := p.last(t, "find")["query"]; got != "" {
		t.Fatalf("页面里的清除应清空查找词：%v", got)
	}
	if rr.screen.detail.panel.find.Text() != "" {
		t.Error("查找框里的字也要清掉")
	}
}

// ---- 目录与阅读位置 ----

func TestTocButtonTogglesThePageAndPersistsPerItemAndSource(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	if loadTocOpen(rr.prefs, detailNoteID, "body") {
		t.Fatal("目录缺省收起")
	}
	rr.click(gfx.RoleButton, locales.LibraryCatalog())
	if got := p.last(t, "ui")["tocOpen"]; got != true {
		t.Fatalf("点目录应让页面展开目录：%v", got)
	}
	if !loadTocOpen(rr.prefs, detailNoteID, "body") {
		t.Fatal("目录的开合应按「条目 + 来源」写进偏好")
	}
	if loadTocOpen(rr.prefs, detailNoteID, "summary") || loadTocOpen(rr.prefs, detailForumID, "body") {
		t.Error("别的条目、别的来源不受影响")
	}
	uiMessages := len(p.messages(t, "ui"))
	rr.event(p, map[string]any{"t": "toc", "open": false})
	if loadTocOpen(rr.prefs, detailNoteID, "body") {
		t.Fatal("页面里收起目录应写回偏好")
	}
	if rr.screen.detail.panel.tocOpen {
		t.Fatal("页面里收起目录应落回界面状态")
	}
	if got := len(p.messages(t, "ui")); got != uiMessages {
		t.Fatalf("页面自己已经收起，界面不必再发一条 ui：%d → %d", uiMessages, got)
	}
}

func TestTocFlagIsRestoredWhenTheItemIsReopened(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t, func(r *rig) {
		if err := storeTocOpen(r.prefs, detailNoteID, "body", true); err != nil {
			t.Fatal(err)
		}
	})
	rr.openCard(detailNoteID)
	p := rr.ready()
	if got := p.last(t, "view")["tocOpen"]; got != true {
		t.Fatalf("偏好里记着展开，打开时应带着：%v", got)
	}
	if !rr.has(gfx.RoleButton, locales.LibraryCatalog()) {
		t.Fatal("原文版本里应有目录钮")
	}
}

// 读者工具条的可访问结构与渲染层一致（台账 B-065）：「阅读版本」组里是原文（按下）与 AI 阅读两个钮；
// 目录钮是展开收起钮，可访问状态取「展开」，不取「按下」。
func TestReaderToolbarAccessibleStructureMatchesTheRenderer(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	rr.ready()
	if !rr.has(gfx.RoleGroup, locales.ThemedReadingReaderVersion()) {
		t.Fatalf("读者工具条里应有「%s」分组", locales.ThemedReadingReaderVersion())
	}
	if n := rr.mustNode(gfx.RoleButton, locales.ThemedReadingReaderOriginal()); !n.Selected {
		t.Error("原文是选中的版本，应是按下状态")
	}
	if n := rr.mustNode(gfx.RoleButton, locales.ThemedReadingReaderAi()); n.Selected {
		t.Error("AI 阅读没有选中")
	}
	if n := rr.mustNode(gfx.RoleButton, locales.LibraryCatalog()); n.Expanded || n.Selected {
		t.Fatalf("目录收起时不该有展开或按下状态：expanded=%v selected=%v", n.Expanded, n.Selected)
	}
	rr.click(gfx.RoleButton, locales.LibraryCatalog())
	if n := rr.mustNode(gfx.RoleButton, locales.LibraryCatalog()); !n.Expanded || n.Selected {
		t.Fatalf("目录展开后应是展开状态、不是按下状态：expanded=%v selected=%v", n.Expanded, n.Selected)
	}
}

func TestScrollEventsAreRememberedForTheTabTheyBelongTo(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.scrollEvent(p, 321)
	mem, ok := LoadReadingMemory(rr.prefs, detailNoteID, rigNow)
	if !ok || mem.ScrollTopByTab[TabBody] != 321 || mem.Tab != TabBody {
		t.Fatalf("上报的位置应写进阅读记忆：%+v", mem)
	}
	// 换条目：宿主按序号把换走前补报的那一次记到旧条目名下，不会串到新条目。
	rr.pick(detailForumID)
	rr.scrollEvent(p, 77)
	forum, _ := LoadReadingMemory(rr.prefs, detailForumID, rigNow)
	if forum.ScrollTopByTab[TabSummary] != 77 {
		t.Fatalf("新条目的位置记在它自己名下：%+v", forum)
	}
	note, _ := LoadReadingMemory(rr.prefs, detailNoteID, rigNow)
	if note.ScrollTopByTab[TabBody] != 321 {
		t.Fatalf("旧条目的位置不被覆盖：%+v", note)
	}
}

func TestRememberedPositionIsRestoredOnlyWhenTheItemOpens(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t, func(r *rig) {
		top := map[ReadingTab]float64{TabBody: 250, TabReplies: 90}
		if err := PatchReadingMemory(r.prefs, detailForumID, ReadingPatch{Tab: TabBody, ScrollTopByTab: top}, rigNow); err != nil {
			t.Fatal(err)
		}
	})
	rr.openCard(detailForumID)
	p := rr.ready()
	if got := p.last(t, "view")["restoreTop"]; got != float64(250) {
		t.Fatalf("打开条目应恢复记着的位置：%v", got)
	}
	rr.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	if got := p.last(t, "view")["restoreTop"]; got != nil {
		t.Fatalf("切标签页不恢复位置（渲染层 changeTab 之后 restoredRef 恒为真）：%v", got)
	}
}

// 岛被收起（托盘、关窗口）后重开：回到用户最后看到的位置，而不是打开条目时记着的那个。
func TestReopenedIslandRestoresTheLatestReportedPosition(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t, func(r *rig) {
		top := map[ReadingTab]float64{TabBody: 250}
		if err := PatchReadingMemory(r.prefs, detailNoteID, ReadingPatch{Tab: TabBody, ScrollTopByTab: top}, rigNow); err != nil {
			t.Fatal(err)
		}
	})
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.scrollEvent(p, 400)
	rr.host.Close()
	rr.frames(3)
	if rr.opener.opens != 2 || rr.page() == p {
		t.Fatalf("收起之后下一帧应重开一座岛：opens=%d", rr.opener.opens)
	}
	if got := rr.ready().last(t, "view")["restoreTop"]; got != float64(400) {
		t.Fatalf("重开应回到最后上报的位置：%v", got)
	}
}

func TestRestoreUsesTheTabThatIsActuallyShown(t *testing.T) {
	t.Parallel()
	// 记忆里的标签（文字稿）这条已经没有入口，退回正文；位置照样取正文的（渲染层用 memory.scrollTopByTab[activeTab]）。
	rr := newReaderRig(t, func(r *rig) {
		top := map[ReadingTab]float64{TabBody: 40, TabTranscript: 900}
		if err := PatchReadingMemory(r.prefs, detailNoteID, ReadingPatch{Tab: TabTranscript, ScrollTopByTab: top}, rigNow); err != nil {
			t.Fatal(err)
		}
	})
	rr.openCard(detailNoteID)
	p := rr.ready()
	if got := p.last(t, "view")["restoreTop"]; got != float64(40) {
		t.Fatalf("应取实际显示的正文标签的位置：%v", got)
	}
}

// ---- 页面事件落回界面 ----

func TestLinkFromThePageOpensTheSystemBrowser(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.event(p, map[string]any{"t": "link", "url": "https://example.com/a?b=1"})
	rr.frames(2)
	if len(rr.opened) != 1 || rr.opened[0] != "https://example.com/a?b=1" {
		t.Fatalf("链接应交给系统浏览器：%v", rr.opened)
	}
	rr.event(p, map[string]any{"t": "link", "url": "javascript:alert(1)"})
	rr.event(p, map[string]any{"t": "link", "url": "file:///C:/Windows/win.ini"})
	rr.frames(2)
	if len(rr.opened) != 1 {
		t.Fatalf("非 http(s) 链接不放行：%v", rr.opened)
	}
}

func TestCopyFromThePageReachesTheClipboardAndReportsFailure(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.event(p, map[string]any{"t": "copy", "id": 1, "text": "fmt.Println(1)"})
	rr.frames(2)
	if len(rr.copied) != 1 || rr.copied[0] != "fmt.Println(1)" {
		t.Fatalf("代码块的复制应写进剪贴板：%q", rr.copied)
	}
	// 台账 B-069：写成功了要回执 ok，页面才亮「已复制」勾。
	if got := p.last(rr.t, "copyResult"); got["id"] != float64(1) || got["ok"] != true {
		t.Fatalf("成功的复制要回 ok 回执：%v", got)
	}
	rr.notifier.Clipboard = func(string) error { return errors.New("剪贴板被别的程序占用") }
	rr.event(p, map[string]any{"t": "copy", "id": 2, "text": "x"})
	rr.frames(2)
	if text := strings.ReplaceAll(rr.toastText(), "\n", ""); !strings.Contains(text, locales.LibraryCopyCode()) || !strings.Contains(text, "剪贴板被别的程序占用") {
		t.Fatalf("剪贴板写入失败要如实提示并带原因：\n%s", text)
	}
	if got := p.last(rr.t, "copyResult"); got["id"] != float64(2) || got["ok"] != false {
		t.Fatalf("失败的复制要回 not-ok 回执（页面不亮勾）：%v", got)
	}
}

func TestToastFromThePage(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.event(p, map[string]any{"t": "toast", "kind": "warning", "text": "图片加载失败"})
	rr.frames(2)
	if !strings.Contains(rr.screenText(), "图片加载失败") {
		t.Fatalf("页面要求的警告应显示出来：\n%s", rr.screenText())
	}
}

func TestKeysForwardedByThePageReachTheWindowShortcuts(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.event(p, map[string]any{"t": "key", "key": "f", "ctrl": true})
	rr.frames(3)
	if !rr.has(gfx.RoleTextField, locales.LibraryPanelFindPlaceholder()) {
		t.Fatal("页面转交的 Ctrl+F 应打开查找条")
	}
	rr.event(p, map[string]any{"t": "key", "key": "z", "alt": true})
	rr.frames(3)
	if !rr.screen.FocusReading() {
		t.Fatal("页面转交的 Alt+Z 应进专注阅读")
	}
	// 网页快照的 iframe 里按 Esc：页面转交 escape，退出专注阅读。
	rr.event(p, map[string]any{"t": "key", "key": "escape"})
	rr.frames(3)
	if rr.screen.FocusReading() {
		t.Fatal("页面转交的 Esc 应退出专注阅读")
	}
	rr.event(p, map[string]any{"t": "key", "key": "s", "ctrl": true})
	rr.frames(3)
}

func TestAltArrowsFromThePageStepTheReviewModalOnly(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	before := len(rr.svc.Calls())
	rr.event(p, map[string]any{"t": "key", "key": "arrowright", "alt": true})
	if len(rr.svc.Calls()) != before {
		t.Fatal("卡片视图里没有整理浮层，Alt+→ 不该做任何事")
	}

	modal := newReaderRig(t)
	entries := modal.openModal()
	mp := modal.ready()
	modal.event(mp, map[string]any{"t": "key", "key": "arrowright", "alt": true})
	if got := modal.view().List.SelectedID; got != entries[1].ID {
		t.Fatalf("详情浮层里页面转交的 Alt+→ 应整理到下一条：选中 %q，应为 %q", got, entries[1].ID)
	}
	modal.event(mp, map[string]any{"t": "key", "key": "arrowleft", "alt": true})
	if got := modal.view().List.SelectedID; got != entries[0].ID {
		t.Fatalf("Alt+← 应回到上一条：选中 %q，应为 %q", got, entries[0].ID)
	}
	modal.event(mp, map[string]any{"t": "key", "key": "arrowright", "alt": true, "ctrl": true})
	if got := modal.view().List.SelectedID; got != entries[0].ID {
		t.Fatalf("带 Ctrl 的方向键不是整理快捷键：选中 %q", got)
	}
}

// ---- 查找条抬到岛之上 ----

// textLayer 是画面上第一处恰好等于 text 的文字所在的层号。
func (r *rig) textLayer(text string) (uint16, bool) {
	for _, t := range r.session.Snapshot().Texts {
		if t.Value == text {
			return t.Layer, true
		}
	}
	return 0, false
}

func TestFindBarIsLiftedAboveTheIslandAndStaysWhereItWas(t *testing.T) {
	t.Parallel()
	placeholder := locales.LibraryPanelFindPlaceholder()
	open := func(r *rig) app.AccessNode {
		r.keys("ctrl+f")
		r.typeText("x")
		return r.mustNode(gfx.RoleTextField, placeholder)
	}
	plain := detailRig(t)
	plain.openCard(detailNoteID)
	inTree := open(plain)
	if layer, ok := plain.textLayer(locales.LibraryPanelFindEmptyShort()); !ok || layer != 0 {
		t.Fatalf("没有岛时查找条画在页面层：%d %v", layer, ok)
	}

	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	rr.ready()
	lifted := open(rr.rig)
	if layer, ok := rr.textLayer(locales.LibraryPanelFindEmptyShort()); !ok || layer != uint16(kit.BandOverIsland) {
		t.Fatalf("岛在场时查找条要抬到第 %d 层：%d %v", kit.BandOverIsland, layer, ok)
	}
	for _, d := range []struct {
		name      string
		got, want float32
	}{
		{"X", lifted.Bounds.X, inTree.Bounds.X}, {"Y", lifted.Bounds.Y, inTree.Bounds.Y},
		{"宽", lifted.Bounds.Width, inTree.Bounds.Width}, {"高", lifted.Bounds.Height, inTree.Bounds.Height},
	} {
		if diff := d.got - d.want; diff > 1 || diff < -1 {
			t.Errorf("抬起来的查找框的%s应与页面层里一致：%.1f 对 %.1f", d.name, d.got, d.want)
		}
	}
	panel := rr.screen.detail.panel.panelRect
	if lifted.Bounds.X+lifted.Bounds.Width > panel.X+panel.Width || lifted.Bounds.Y < panel.Y {
		t.Errorf("查找框应在面板之内：%+v 面板 %+v", lifted.Bounds, panel)
	}
}

func TestFindBarInTheDetailModalIsLiftedAboveTheModalLayer(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openModal()
	rr.ready()
	rr.keys("ctrl+f")
	rr.typeText("x")
	if layer, ok := rr.textLayer(locales.LibraryPanelFindEmptyShort()); !ok || layer != uint16(kit.BandOverIslandInModal) {
		t.Fatalf("详情浮层里的岛在浮层那一层上，查找条要再高一层（%d）：%d %v", kit.BandOverIslandInModal, layer, ok)
	}
}

func TestFindBarStaysInTheTreeWhenNothingIsShownInTheIsland(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.pick(detailVideoID)
	rr.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
	rr.keys("ctrl+f")
	rr.typeText("x")
	if layer, ok := rr.textLayer(locales.LibraryPanelFindEmptyShort()); !ok || layer != 0 {
		t.Fatalf("岛里没有内容（文字稿页签）时没有原生视图，查找条留在页面层：%d %v", layer, ok)
	}
}

// ---- 「退出专注」浮钮抬到岛之上 ----

// exitPill 是专注阅读右下角的「退出专注」浮钮：头部那枚 32x32 的图标钮与它同名，靠宽度分开。
func (r *rig) exitPill() app.AccessNode {
	r.t.Helper()
	name := locales.LibraryExitFocusReading()
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleButton && n.Label == name && n.Bounds.Width > 40 {
			return n
		}
	}
	r.t.Fatalf("找不到右下角的「%s」浮钮", name)
	return app.AccessNode{}
}

// 专注阅读窄窗口里浮钮落在岛上：岛在场时它抬到更高的一层，位置与页面层里一致；点它仍退出专注。
func TestExitFocusButtonIsLiftedAboveTheIslandAndStaysWhereItWas(t *testing.T) {
	t.Parallel()
	short := locales.LibraryExitFocusReadingShort()
	plain := detailRig(t)
	plain.openCard(detailNoteID)
	plain.keys("alt+z")
	plain.frames(3)
	inTree := plain.exitPill()
	if layer, ok := plain.textLayer(short); !ok || layer != 0 {
		t.Fatalf("没有岛时浮钮画在页面层：%d %v", layer, ok)
	}

	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	rr.ready()
	rr.keys("alt+z")
	rr.frames(4)
	if !rr.screen.readerHost.Open() {
		t.Fatal("夹具失效：专注阅读里岛应还开着")
	}
	lifted := rr.exitPill()
	if layer, ok := rr.textLayer(short); !ok || layer != uint16(kit.BandOverIsland) {
		t.Fatalf("岛在场时浮钮要抬到第 %d 层：%d %v", kit.BandOverIsland, layer, ok)
	}
	for _, d := range []struct {
		name      string
		got, want float32
	}{
		{"X", lifted.Bounds.X, inTree.Bounds.X}, {"Y", lifted.Bounds.Y, inTree.Bounds.Y},
		{"宽", lifted.Bounds.Width, inTree.Bounds.Width}, {"高", lifted.Bounds.Height, inTree.Bounds.Height},
	} {
		if diff := d.got - d.want; diff > 1 || diff < -1 {
			t.Errorf("抬起来的浮钮的%s应与页面层里一致：%.1f 对 %.1f", d.name, d.got, d.want)
		}
	}
	x, y := centerOf(lifted)
	rr.clickAt(x, y)
	if rr.screen.FocusReading() {
		t.Fatal("点抬起来的浮钮应退出专注阅读")
	}
	if _, ok := rr.textLayer(short); ok {
		t.Fatal("退出专注之后浮钮应消失")
	}
}

// 岛里没有内容（没有选中条目、文字稿页签）时没有原生视图，浮钮留在页面层，一帧也不抬。
func TestExitFocusButtonStaysInTheTreeWhenNothingIsShownInTheIsland(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.pick(detailVideoID)
	rr.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
	rr.keys("alt+z")
	rr.frames(4)
	if layer, ok := rr.textLayer(locales.LibraryExitFocusReadingShort()); !ok || layer != 0 {
		t.Fatalf("岛里没有内容时浮钮留在页面层：%d %v", layer, ok)
	}
}

// ---- 读者工具条 ----

func TestReaderToolbarGroupAppearsWithTheIsland(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	for _, name := range []string{locales.ThemedReadingReaderOriginal(), locales.ThemedReadingReaderAi(), locales.LibraryCatalog(), locales.ThemedReadingReaderEditOriginal()} {
		if !rr.has(gfx.RoleButton, name) {
			t.Errorf("工具栏缺「%s」", name)
		}
	}
	rr.pick(detailForumID)
	if !rr.has(gfx.RoleButton, locales.ThemedReadingReaderEditSummary()) {
		t.Error("讨论总结页签的编辑钮要叫「编辑讨论总结」")
	}
	rr.pickTrashed(shellTrashNoteID)
	if rr.has(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal()) {
		t.Error("回收站里的条目没有编辑钮")
	}
	if !rr.has(gfx.RoleButton, locales.ThemedReadingReaderOriginal()) {
		t.Error("回收站里的条目仍有版本切换")
	}
}

func TestReaderToolbarIsAbsentWithoutAHostUnlessAsked(t *testing.T) {
	t.Parallel()
	plain := shellRig(t)
	plain.pick(detailNoteID)
	if plain.has(gfx.RoleButton, locales.ThemedReadingReaderOriginal()) {
		t.Fatal("没接宿主时工具栏里不画读者工具条")
	}
	asked := shellRig(t, func(r *rig) { r.screen.ReaderToolbar = true })
	asked.pick(detailNoteID)
	if !asked.has(gfx.RoleButton, locales.ThemedReadingReaderOriginal()) {
		t.Fatal("ReaderToolbar 为真时即使没有宿主也画读者工具条（对照用）")
	}
}

func TestUnmigratedReaderActionsFailLoudlyWithTheirMilestone(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderAi())
	if text := rr.toastText(); !strings.Contains(text, "M3") || !strings.Contains(text, "B-064") {
		t.Fatalf("AI 阅读未迁移：应写明里程碑与台账编号：\n%s", text)
	}
}

// ---- 请求的组装 ----

func TestReaderFindOnlyAppliesWhereTheRendererAppliesIt(t *testing.T) {
	t.Parallel()
	p := &panelState{findQuery: "词", findActive: 2}
	for _, c := range []struct {
		kind  reader.ViewKind
		open  bool
		query string
		index int
	}{
		{reader.ViewReplies, false, "词", 2},
		{reader.ViewReplies, true, "词", 2},
		{reader.ViewOriginal, false, "", 0},
		{reader.ViewOriginal, true, "词", 2},
		{reader.ViewMarkdown, false, "", 0},
		{reader.ViewMarkdown, true, "词", 2},
		{reader.ViewGallery, true, "", 0},
		{reader.ViewNone, true, "", 0},
	} {
		p.findOpen = c.open
		query, index := p.readerFind(c.kind)
		if query != c.query || index != c.index {
			t.Errorf("%s 查找条开=%v：(%q, %d)，应为 (%q, %d)", c.kind, c.open, query, index, c.query, c.index)
		}
	}
}

func TestTocPreferenceRoundTripAndLimit(t *testing.T) {
	t.Parallel()
	prefs := &servicestest.Prefs{}
	if loadTocOpen(prefs, "a", "body") || loadTocOpen(nil, "a", "body") {
		t.Fatal("没有存储时收起")
	}
	if err := storeTocOpen(prefs, "a", "body", true); err != nil {
		t.Fatal(err)
	}
	if err := storeTocOpen(prefs, "a", "summary", true); err != nil {
		t.Fatal(err)
	}
	if !loadTocOpen(prefs, "a", "body") || !loadTocOpen(prefs, "a", "summary") || loadTocOpen(prefs, "b", "body") {
		t.Fatal("按条目与来源分别记")
	}
	if err := storeTocOpen(prefs, "a", "body", false); err != nil {
		t.Fatal(err)
	}
	if loadTocOpen(prefs, "a", "body") || !loadTocOpen(prefs, "a", "summary") {
		t.Fatal("收起只删这一份")
	}
	for i := 0; i < maxCatalogFlags+20; i++ {
		if err := storeTocOpen(prefs, fmt.Sprintf("item-%04d", i), "body", true); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := prefs.Get(settings.PrefCatalogOpen)
	var flags map[string]bool
	if err := json.Unmarshal(raw, &flags); err != nil || len(flags) > maxCatalogFlags {
		t.Fatalf("超过上限要裁掉：%d 项 %v", len(flags), err)
	}
}

func TestWindowResizeSendsANewThemeWithTheNewWindowWidth(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	if len(p.messages(t, "theme")) != 0 {
		t.Fatal("什么都没变时不该发 theme")
	}
	rr.look.Viewport = 900 // 窗口变窄（页面宽度不变，断点依据变了）
	rr.frames(2)
	themes := p.messages(t, "theme")
	if len(themes) != 1 {
		t.Fatalf("窗口宽度变了要发一条 theme：%d 条", len(themes))
	}
	if themes[0]["theme"].(map[string]any)["viewportWidth"] != float64(900) {
		t.Errorf("theme 要带新的窗口宽度：%v", themes[0])
	}
	rr.frames(5)
	if len(p.messages(t, "theme")) != 1 {
		t.Error("同一个宽度不该重复发")
	}
}
