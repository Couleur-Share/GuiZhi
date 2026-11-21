package reader

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/viva/island"
)

func TestFrameOpensPageOnlyWhenThereIsContent(t *testing.T) {
	r := newRig(t)
	r.req.View = View{Kind: ViewNone}
	r.frame()
	if r.opener.opens != 0 || r.host.Open() {
		t.Fatalf("没有内容不该开岛：opens=%d", r.opener.opens)
	}
	r.req.View = View{Kind: ""}
	r.frame()
	if r.opener.opens != 0 {
		t.Fatalf("空种类等同没有内容：opens=%d", r.opener.opens)
	}
	r.req.View = View{Kind: ViewOriginal, Content: "x", SourceKind: "body"}
	r.frame()
	if r.opener.opens != 1 || !r.host.Open() {
		t.Fatalf("有内容就开岛：opens=%d", r.opener.opens)
	}
	r.frame()
	if r.opener.opens != 1 {
		t.Fatalf("已开的岛不重复开：opens=%d", r.opener.opens)
	}
}

func TestOpenedPageConfigIsLockedDown(t *testing.T) {
	r := newRig(t)
	r.frame()
	cfg := r.page().cfg
	if cfg.Scheme != "gzreader" || cfg.Entry != "index.html" {
		t.Errorf("scheme/入口 = %q/%q", cfg.Scheme, cfg.Entry)
	}
	if cfg.CSP != readerCSP {
		t.Errorf("CSP 没有用阅读岛专用的策略：%q", cfg.CSP)
	}
	for _, must := range []string{"default-src 'none'", "script-src 'self'", "connect-src 'self'", "object-src 'none'", "frame-src about:"} {
		if !strings.Contains(cfg.CSP, must) {
			t.Errorf("CSP 缺少 %q", must)
		}
	}
	// 只放宽图片：脚本、连接与媒体不许出现远程来源。
	for _, bad := range []string{"script-src 'self' ", "connect-src 'self' ", "media-src 'self' https", "unsafe-eval", "script-src 'unsafe-inline'"} {
		if strings.Contains(cfg.CSP, bad) {
			t.Errorf("CSP 不该含 %q", bad)
		}
	}
	if cfg.OnMessage == nil || cfg.Serve == nil || cfg.OnDenied == nil {
		t.Error("岛配置缺回调")
	}
}

func TestPlaceEveryFrameAndNothingPostedBeforeReady(t *testing.T) {
	r := newRig(t)
	r.frame()
	r.frame()
	p := r.page()
	if len(p.places) != 2 || p.places[0] != testBounds {
		t.Errorf("每帧都要摆放原生视图：%v", p.places)
	}
	if len(p.posts) != 0 {
		t.Errorf("握手之前不能发消息：%v", p.posts)
	}
	if r.host.Ready() {
		t.Error("还没握手却报 Ready")
	}
}

func TestReadyHandshakeSendsInitThenView(t *testing.T) {
	r := newRig(t)
	restore := 123.5
	r.req.RestoreTop = &restore
	r.req.CatalogOpen, r.req.TocOpen = true, true
	p := r.ready()
	if got, want := p.kinds(t), []string{"init", "view"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("消息顺序 %v，期望 %v", got, want)
	}
	init := p.msg(t, 0)
	if init["v"] != float64(ProtocolVersion) || init["lang"] != "zh" {
		t.Errorf("init = %v", init)
	}
	theme, _ := init["theme"].(map[string]any)
	if theme["dark"] != false || theme["hue"] != float64(210) || theme["basePx"] != float64(16) || theme["motion"] != "standard" {
		t.Errorf("init.theme = %v", theme)
	}
	view := p.msg(t, 1)
	if view["seq"] != float64(1) || view["itemId"] != "item-1" || view["tab"] != "body" ||
		view["restoreTop"] != 123.5 || view["catalogOpen"] != true || view["tocOpen"] != true {
		t.Errorf("view = %v", view)
	}
	body, _ := view["view"].(map[string]any)
	if body["kind"] != "original" || body["content"] != "# 标题\n\n正文" || body["sourceKind"] != "body" {
		t.Errorf("view.view = %v", body)
	}
	if !r.host.Ready() || r.host.Seq() != 1 {
		t.Errorf("Ready=%v Seq=%d", r.host.Ready(), r.host.Seq())
	}
}

func TestNoRepostWithoutChange(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	n := len(p.posts)
	r.frame()
	r.frame()
	if len(p.posts) != n {
		t.Errorf("状态没变不该重发：多了 %v", p.posts[n:])
	}
}

func TestThemeChangePostsThemeOnly(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	n := len(p.posts)
	r.req.Theme.Dark = true
	r.req.Lang = LangEn
	r.frame()
	if got := p.kinds(t)[n:]; !reflect.DeepEqual(got, []string{"theme"}) {
		t.Fatalf("外观变了只发 theme：%v", got)
	}
	m := p.msg(t, n)
	if m["lang"] != "en" || m["theme"].(map[string]any)["dark"] != true {
		t.Errorf("theme = %v", m)
	}
	r.req.Lang = "xx"
	r.frame()
	if got := p.kinds(t)[n+1:]; !reflect.DeepEqual(got, []string{"theme"}) || p.msg(t, n+1)["lang"] != "zh" {
		t.Errorf("未知语言按中文发：%v", p.posts[n+1:])
	}
}

func TestFindIsPostedOnlyWhenItChanges(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view"}) {
		t.Fatalf("初始查找为空不该发 find：%v", got)
	}
	r.req.FindQuery, r.req.FindIndex = "go", 2
	r.frame()
	r.frame()
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view", "find"}) {
		t.Fatalf("查找词变了发一次 find：%v", got)
	}
	if m := p.msg(t, 2); m["query"] != "go" || m["index"] != float64(2) {
		t.Errorf("find = %v", m)
	}
	r.req.FindIndex = 3
	r.frame()
	r.req.FindQuery, r.req.FindIndex = "", 0
	r.frame()
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view", "find", "find", "find"}) {
		t.Fatalf("序号与清空都要发：%v", got)
	}
	if m := p.msg(t, 4); m["query"] != "" || m["index"] != float64(0) {
		t.Errorf("清空 = %v", m)
	}
}

func TestInitialFindIsSentWhenQueryAlreadyActive(t *testing.T) {
	r := newRig(t)
	r.req.FindQuery = "已有的查找词"
	p := r.ready()
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view", "find"}) {
		t.Fatalf("打开时已有查找词要发 find：%v", got)
	}
	if p.msg(t, 2)["query"] != "已有的查找词" {
		t.Errorf("find = %v", p.msg(t, 2))
	}
}

func TestSameKeyContentChangeDropsRestoreTop(t *testing.T) {
	r := newRig(t)
	top := 40.0
	r.req.RestoreTop = &top
	p := r.ready()
	if p.msg(t, 1)["restoreTop"] != 40.0 {
		t.Fatalf("打开时带恢复位置：%v", p.msg(t, 1))
	}
	r.req.View.Content = "改过的正文"
	r.frame()
	m := p.msg(t, 2)
	if p.kinds(t)[2] != "view" || m["seq"] != float64(2) {
		t.Fatalf("正文变了发新的 view：%v", p.kinds(t))
	}
	if v, present := m["restoreTop"]; !present || v != nil {
		t.Errorf("同一份内容的更新不带恢复位置（应为 null）：%v", m)
	}
}

func TestKeyChangeCarriesRestoreTopOfNewContent(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	top := 77.0
	r.req.Tab = TabSummary
	r.req.View = View{Kind: ViewOriginal, Content: "总结", SourceKind: "summary"}
	r.req.RestoreTop = &top
	r.frame()
	m := p.msg(t, 2)
	if m["seq"] != float64(2) || m["tab"] != "summary" || m["restoreTop"] != 77.0 {
		t.Errorf("换标签页是一份新内容，要带它的恢复位置：%v", m)
	}
	r.req.ItemID = "item-2"
	r.req.RestoreTop = nil
	r.frame()
	m = p.msg(t, 3)
	if m["seq"] != float64(3) || m["itemId"] != "item-2" || m["restoreTop"] != nil {
		t.Errorf("换条目：%v", m)
	}
}

func TestViewKindChangeIsNewContent(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	r.req.View = View{Kind: ViewReplies, Content: "{}"}
	r.req.Tab = TabReplies
	r.frame()
	m := p.msg(t, 2)
	if m["seq"] != float64(2) || m["view"].(map[string]any)["kind"] != "replies" {
		t.Errorf("view = %v", m)
	}
}

func TestUIStateChangePostsUIMessageOnly(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	r.req.CatalogOpen = true
	r.frame()
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view", "ui"}) {
		t.Fatalf("%v", got)
	}
	if m := p.msg(t, 2); m["catalogOpen"] != true || m["tocOpen"] != false {
		t.Errorf("ui = %v", m)
	}
	r.req.TocOpen = true
	r.frame()
	if m := p.msg(t, 3); m["catalogOpen"] != true || m["tocOpen"] != true {
		t.Errorf("ui = %v", m)
	}
}

func TestViewMessageCarriesUIStateSoNoSeparateUIMessage(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	r.req.ItemID = "item-2"
	r.req.CatalogOpen = true
	r.frame()
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view", "view"}) {
		t.Fatalf("view 已带目录开合，不该再发 ui：%v", got)
	}
}

func TestEventsDispatchToHandler(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	cases := []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{"matches", map[string]any{"t": "matches", "seq": 1, "count": 7}, "matches:7"},
		{"scroll", map[string]any{"t": "scroll", "seq": 1, "top": 321.5}, "scroll:item-1/body/321.5"},
		{"catalog", map[string]any{"t": "catalog", "seq": 1, "open": true}, "catalog:true"},
		{"toc", map[string]any{"t": "toc", "seq": 1, "open": true}, "toc:true"},
		{"link", map[string]any{"t": "link", "seq": 1, "url": "https://example.com/a?b=1"}, "link:https://example.com/a?b=1"},
		{"copy", map[string]any{"t": "copy", "seq": 1, "id": 1, "text": "代码\n第二行"}, "copy:代码\n第二行"},
		{"lightbox", map[string]any{"t": "lightbox", "seq": 1, "index": 1,
			"images": []any{map[string]any{"src": "local-image://a.png"}, map[string]any{"src": "local-image://b 1.png", "alt": "图"}}},
			"lightbox:1:local-image://a.png|,local-image://b 1.png|图"},
		{"toast", map[string]any{"t": "toast", "seq": 1, "kind": "warning", "text": "请先登录"}, "toast:请先登录"},
		{"key", map[string]any{"t": "key", "seq": 1, "key": "f", "ctrl": true}, "key:f/true/false/false"},
		{"findReset", map[string]any{"t": "findReset", "seq": 1}, "findReset"},
	}
	for _, c := range cases {
		p.send(r.host, c.fields)
		r.wantCalls(c.want)
	}
	p.send(r.host, map[string]any{"t": "rendered", "seq": 1})
	r.wantCalls()
	if !r.host.Rendered() {
		t.Error("rendered 事件没有记下")
	}
	if len(r.diags) != 0 {
		t.Errorf("正常事件不该有诊断：%v", r.diags)
	}
}

// lastPost 返回页面最近收到的一条消息；没有就失败。
func lastPost(t *testing.T, p *fakePage) map[string]any {
	t.Helper()
	if len(p.posts) == 0 {
		t.Fatal("页面没有收到任何消息")
	}
	return p.msg(t, len(p.posts)-1)
}

// 台账 B-069：复制要给页面回执，页面的 writeText 据此成功或拒绝，「已复制」勾才不会先于写入结果出现。
func TestCopyGetsAReceiptWithTheCopyId(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	before := len(p.posts)
	p.send(r.host, map[string]any{"t": "copy", "seq": 1, "id": 41, "text": "代码"})
	r.wantCalls("copy:代码")
	if len(p.posts) != before+1 {
		t.Fatalf("应当恰好回一条消息：%d → %d", before, len(p.posts))
	}
	got := lastPost(t, p)
	if got["t"] != "copyResult" || got["id"] != float64(41) || got["ok"] != true {
		t.Errorf("成功的回执不对：%v", got)
	}
}

func TestCopyFailureIsReportedBackAsNotOk(t *testing.T) {
	r := newRig(t)
	r.handler.copyErr = errors.New("剪贴板被占用")
	p := r.ready()
	p.send(r.host, map[string]any{"t": "copy", "seq": 1, "id": 42, "text": "代码"})
	r.wantCalls("copy:代码")
	got := lastPost(t, p)
	if got["t"] != "copyResult" || got["id"] != float64(42) || got["ok"] != false {
		t.Errorf("失败的回执不对：%v", got)
	}
	if body := p.posts[len(p.posts)-1]; strings.Contains(body, "剪贴板被占用") {
		t.Errorf("回执不能带错误原因（原因由界面向用户说明）：%s", body)
	}
}

func TestCopyWithoutHandlerIsAnsweredNotOk(t *testing.T) {
	r := newRig(t)
	r.host.SetHandler(nil)
	p := r.ready()
	p.send(r.host, map[string]any{"t": "copy", "seq": 1, "id": 43, "text": "x"})
	got := lastPost(t, p)
	if got["t"] != "copyResult" || got["ok"] != false {
		t.Errorf("没有接收者时要回失败：%v", got)
	}
}

func TestStaleOrBadCopyGetsNoReceipt(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	before := len(p.posts)
	p.send(r.host, map[string]any{"t": "copy", "seq": 0, "id": 1, "text": "旧内容"}) // 序号不是当前内容的：丢弃
	p.cfg.OnMessage(`{"t":"copy","seq":1,"text":"没有编号"}`)                         // 不合规：丢弃
	r.wantCalls()
	if len(p.posts) != before {
		t.Errorf("被丢弃的复制不该有回执：%d → %d", before, len(p.posts))
	}
}

// editorRig 是已握手、阅读区在编辑视图的一套宿主。
func editorRig(t *testing.T) (*rig, *fakePage) {
	t.Helper()
	r := newRig(t)
	r.req.View = View{Kind: ViewEditor, Content: "# 草稿", SourceKind: "body"}
	return r, r.ready()
}

func TestEditorViewIsPostedWithItsDraftAndSource(t *testing.T) {
	r, p := editorRig(t)
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view"}) {
		t.Fatalf("消息序列不对：%v", got)
	}
	view, _ := p.msg(t, 1)["view"].(map[string]any)
	if view["kind"] != "editor" || view["content"] != "# 草稿" || view["sourceKind"] != "body" {
		t.Errorf("编辑视图的编码不对：%v", view)
	}
	r.wantCalls()
}

func TestEditEventsReachTheHandlerAndTheEchoIsNotSentBack(t *testing.T) {
	r, p := editorRig(t)
	posts := len(p.posts)
	p.send(r.host, map[string]any{"t": "edit", "seq": 1, "text": "甲"})
	r.wantCalls("edit:item-1/body/甲")
	// 界面把草稿记成页面给的那份：下一帧的期望与页面里的文档一致，不必把整篇再发回去。
	r.req.View.Content = "甲"
	r.frame()
	p.send(r.host, map[string]any{"t": "edit", "seq": 1, "text": "甲乙"})
	r.wantCalls("edit:item-1/body/甲乙")
	r.req.View.Content = "甲乙"
	r.frame()
	if len(p.posts) != posts {
		t.Errorf("页面自己的改动不该被回发：%v", p.kinds(t)[posts:])
	}
	// 界面改了草稿（和页面里的不同）：照常重发，页面把文档换成它。
	r.req.View.Content = "甲乙\n"
	r.frame()
	if got := p.kinds(t)[posts:]; !reflect.DeepEqual(got, []string{"view"}) {
		t.Errorf("草稿被界面改过要重发：%v", got)
	}
}

func TestEditIsKeptForItsOwnDraftAfterTheViewMovedOn(t *testing.T) {
	r, p := editorRig(t)
	r.req.ItemID = "item-2"
	r.req.View = View{Kind: ViewOriginal, Content: "别的条目", SourceKind: "body"}
	r.frame() // seq 2
	// 换内容之前页面的最后一笔改动，到达时界面已经在别的条目：仍记在原来那份草稿名下，不能丢（丢了就少存了字）。
	p.send(r.host, map[string]any{"t": "edit", "seq": 1, "text": "最后一笔"})
	r.wantCalls("edit:item-1/body/最后一笔")
	if r.host.sent.view.Content != "别的条目" {
		t.Errorf("迟到的改动不能改写页面当前内容的记录：%q", r.host.sent.view.Content)
	}
	// 属于非编辑视图的序号、没发过的序号：丢弃。
	p.send(r.host, map[string]any{"t": "edit", "seq": 2, "text": "冒充"})
	p.send(r.host, map[string]any{"t": "edit", "seq": 99, "text": "伪造"})
	r.wantCalls()
}

func TestEditBeforeHandshakeOrWithoutHandlerIsHarmless(t *testing.T) {
	r := newRig(t)
	r.req.View = View{Kind: ViewEditor, Content: "x", SourceKind: "body"}
	r.frame()
	r.page().send(r.host, map[string]any{"t": "edit", "seq": 0, "text": "偷偷改"})
	r.wantCalls()
	r.page().send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	r.host.SetHandler(nil)
	r.page().send(r.host, map[string]any{"t": "edit", "seq": 1, "text": "没人接"})
	if r.host.sent.view.Content != "没人接" {
		t.Errorf("没有接收者时也要记下页面里的文档：%q", r.host.sent.view.Content)
	}
}

func TestOversizeEditFailsTheEditorInsteadOfFallingBehindSilently(t *testing.T) {
	r, p := editorRig(t)
	secret := "SECRET-DOC-TEXT"
	p.cfg.OnMessage(`{"t":"edit","seq":1,"text":"` + secret + strings.Repeat("a", MaxEditTextBytes) + `"}`)
	if !strings.HasPrefix(strings.Join(r.handler.take(), "\n"), "failed:") {
		t.Fatalf("超限的编辑要让宿主按页面失败处理")
	}
	if r.host.Open() {
		t.Error("失败要关掉页面")
	}
	err := r.host.Failure(r.req)
	if !errors.Is(err, ErrEditTooLarge) {
		t.Fatalf("失败原因要认得出是编辑过大：%v", err)
	}
	if len(r.diags) != 1 || strings.Contains(r.diags[0], secret) || len(r.diags[0]) > 300 {
		t.Errorf("诊断一条、不带文档内容：%v", r.diags)
	}
	// 重试以宿主已有的草稿重开编辑器。
	r.host.Retry()
	r.frame()
	if r.opener.opens != 2 {
		t.Errorf("重试应当重新打开页面：打开了 %d 次", r.opener.opens)
	}
}

func TestOversizeEditOutsideTheEditorOnlyLeavesADiagnostic(t *testing.T) {
	r := newRig(t)
	p := r.ready() // 原文视图
	p.cfg.OnMessage(`{"t":"edit","seq":1,"text":"` + strings.Repeat("a", MaxEditTextBytes+1) + `"}`)
	r.wantCalls()
	if !r.host.Open() || r.host.Failure(r.req) != nil {
		t.Error("不在编辑视图时，过大的编辑事件只是一条诊断")
	}
	if len(r.diags) != 1 {
		t.Errorf("诊断数 %d，期望 1", len(r.diags))
	}
}

func TestFocusEditorOnlyReachesALiveEditor(t *testing.T) {
	r := newRig(t)
	if r.host.FocusEditor() {
		t.Error("岛没开：不能聚焦")
	}
	r.req.View = View{Kind: ViewEditor, Content: "x", SourceKind: "body"}
	r.frame()
	if r.host.FocusEditor() {
		t.Error("还没握手：不能聚焦")
	}
	p := r.page()
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	posts := len(p.posts)
	if !r.host.FocusEditor() {
		t.Fatal("编辑器在显示：应当聚焦")
	}
	if p.focuses != 1 || len(p.posts) != posts+1 || lastPost(t, p)["t"] != "focus" {
		t.Errorf("要先把焦点交给 WebView，再发 focus：focuses=%d 消息 %v", p.focuses, p.kinds(t)[posts:])
	}
	// 回到原文视图：同一个页面，但已经不是编辑器。
	r.req.View = View{Kind: ViewOriginal, Content: "x", SourceKind: "body"}
	r.frame()
	posts, focuses := len(p.posts), p.focuses
	if r.host.FocusEditor() || p.focuses != focuses || len(p.posts) != posts {
		t.Error("不是编辑视图：什么也不做")
	}
}

func TestRenderedResetsWhenContentChanges(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	p.send(r.host, map[string]any{"t": "rendered", "seq": 1})
	r.req.View.Content = "新内容"
	r.frame()
	if r.host.Rendered() {
		t.Error("发出新内容后，旧的 rendered 不再成立")
	}
	p.send(r.host, map[string]any{"t": "rendered", "seq": 1})
	if r.host.Rendered() {
		t.Error("迟到的旧序号 rendered 不能算新内容")
	}
	p.send(r.host, map[string]any{"t": "rendered", "seq": 2})
	if !r.host.Rendered() {
		t.Error("新序号的 rendered 要记下")
	}
}

func TestStaleEventsAreDroppedButScrollIsAttributedToItsOwnContent(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	r.req.ItemID = "item-2"
	r.req.Tab = TabSummary
	r.req.View = View{Kind: ViewOriginal, Content: "总结", SourceKind: "summary"}
	r.frame() // seq 2
	p.send(r.host, map[string]any{"t": "matches", "seq": 1, "count": 9})
	p.send(r.host, map[string]any{"t": "link", "seq": 1, "url": "https://example.com"})
	p.send(r.host, map[string]any{"t": "catalog", "seq": 1, "open": true})
	r.wantCalls()
	// 换内容前页面补报的阅读位置：属于旧内容，要记在旧条目名下。
	p.send(r.host, map[string]any{"t": "scroll", "seq": 1, "top": 88})
	r.wantCalls("scroll:item-1/body/88")
	p.send(r.host, map[string]any{"t": "scroll", "seq": 2, "top": 5})
	r.wantCalls("scroll:item-2/summary/5")
	p.send(r.host, map[string]any{"t": "scroll", "seq": 99, "top": 5})
	r.wantCalls() // 没发过的序号：伪造
}

func TestViewContextHistoryIsBounded(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	for i := 2; i <= maxViewContexts+3; i++ {
		r.req.ItemID = fmt.Sprintf("item-%d", i)
		r.frame()
	}
	p.send(r.host, map[string]any{"t": "scroll", "seq": 1, "top": 1})
	r.wantCalls()
	last := maxViewContexts + 3
	p.send(r.host, map[string]any{"t": "scroll", "seq": last, "top": 2})
	r.wantCalls(fmt.Sprintf("scroll:item-%d/body/2", last))
	if len(r.host.contexts) != maxViewContexts {
		t.Errorf("历史长度 %d，期望 %d", len(r.host.contexts), maxViewContexts)
	}
}

func TestEventsBeforeReadyAndBeforeFirstViewAreIgnored(t *testing.T) {
	r := newRig(t)
	r.frame()
	p := r.page()
	p.send(r.host, map[string]any{"t": "copy", "seq": 0, "id": 1, "text": "偷偷复制"})
	p.send(r.host, map[string]any{"t": "link", "seq": 0, "url": "https://example.com"})
	r.wantCalls()
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	// 握手之后、第一份内容之前不存在（flush 立刻发出了 view），序号 0 的事件也不是任何已发内容的。
	p.send(r.host, map[string]any{"t": "copy", "seq": 0, "id": 1, "text": "序号 0"})
	r.wantCalls()
}

func TestEventBeforeAnyViewWasSentIsIgnored(t *testing.T) {
	r := newRig(t)
	r.frame()
	r.host.ready = true // 白盒：握手完成但还没发过任何 view（防御分支）
	r.page().send(r.host, map[string]any{"t": "copy", "seq": 0, "id": 1, "text": "x"})
	r.page().send(r.host, map[string]any{"t": "scroll", "seq": 0, "top": 1})
	r.wantCalls()
}

func TestBadEventsAreDroppedAndDiagnosedWithoutEchoingTheContent(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	secret := "SECRET-TOKEN-1234"
	for _, text := range []string{
		"not json",
		`{"t":"link","seq":1,"url":"` + strings.Repeat("a", MaxLinkBytes+1) + `"}`,
		`{"t":"copy","seq":"1","text":"` + secret + `"}`,
		`{"t":"key","seq":1,"key":"` + secret + `"}`,
		`{"t":"` + secret + `","seq":1}`,
	} {
		p.cfg.OnMessage(text)
	}
	r.wantCalls()
	if len(r.diags) != 5 {
		t.Fatalf("诊断数 %d，期望 5：%v", len(r.diags), r.diags)
	}
	for _, d := range r.diags {
		if strings.Contains(d, secret) || len(d) > 300 {
			t.Errorf("诊断不能回显页面给的内容、也不能很长：%q", d)
		}
	}
}

func TestLinkPolicy(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	for _, url := range []string{"javascript:alert(1)", "file:///C:/Windows/win.ini", "data:text/html,x", "ftp://example.com/x", "https://user:pw@example.com/", "https:///nohost", "gzreader://doc/index.html"} {
		p.send(r.host, map[string]any{"t": "link", "seq": 1, "url": url})
	}
	r.wantCalls()
	if len(r.diags) != 7 {
		t.Errorf("每个被拒的链接都要有诊断：%d", len(r.diags))
	}
	for _, d := range r.diags {
		if strings.Contains(d, "alert") || strings.Contains(d, "win.ini") || strings.Contains(d, "pw@") {
			t.Errorf("诊断里不能有地址：%q", d)
		}
	}
	p.send(r.host, map[string]any{"t": "link", "seq": 1, "url": "http://example.com/页面"})
	if got := r.handler.take(); len(got) != 1 || !strings.HasPrefix(got[0], "link:http://example.com/") {
		t.Errorf("http 链接放行：%v", got)
	}
}

func TestPageInitiatedUIStateIsNotEchoedBack(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	n := len(p.posts)
	p.send(r.host, map[string]any{"t": "catalog", "seq": 1, "open": true})
	p.send(r.host, map[string]any{"t": "toc", "seq": 1, "open": true})
	r.wantCalls("catalog:true", "toc:true")
	// 界面把它们写成新状态：与页面一致，不再回发。
	r.req.CatalogOpen, r.req.TocOpen = true, true
	r.frame()
	if len(p.posts) != n {
		t.Errorf("页面自己改的开合不该回发：%v", p.posts[n:])
	}
	// 界面拒绝了页面的改动（状态仍是关）：宿主把页面拉回界面的状态。
	r.req.CatalogOpen = false
	r.frame()
	if m := p.msg(t, n); p.kinds(t)[n] != "ui" || m["catalogOpen"] != false || m["tocOpen"] != true {
		t.Errorf("界面状态是唯一来源，要把页面拉回去：%v", p.posts[n:])
	}
}

func TestVersionMismatchFailsAndClosesPage(t *testing.T) {
	r := newRig(t)
	r.frame()
	p := r.page()
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion + 1})
	if p.closes != 1 || r.host.Open() {
		t.Errorf("版本对不上要关岛：closes=%d open=%v", p.closes, r.host.Open())
	}
	if got := r.handler.take(); len(got) != 1 || !strings.HasPrefix(got[0], "failed:") {
		t.Errorf("要通知 Failed：%v", got)
	}
	if len(p.posts) != 0 {
		t.Errorf("不一致的页面不该收到任何消息：%v", p.posts)
	}
	// 同一份内容不再重试。
	r.frame()
	r.frame()
	if r.opener.opens != 1 {
		t.Errorf("同一份内容失败后每帧重开：opens=%d", r.opener.opens)
	}
}

func TestOpenFailureIsReportedOncePerContent(t *testing.T) {
	r := newRig(t)
	r.opener.err = errors.New("WebView2 不可用")
	r.frame()
	r.frame()
	r.frame()
	if r.opener.opens != 1 {
		t.Fatalf("同一份内容打开失败后不重试：opens=%d", r.opener.opens)
	}
	got := r.handler.take()
	if len(got) != 1 || !strings.Contains(got[0], "WebView2 不可用") {
		t.Fatalf("Failed 通知 %v", got)
	}
	// 换了内容（用户点了别的条目）就再试一次。
	r.req.ItemID = "item-2"
	r.frame()
	if r.opener.opens != 2 {
		t.Errorf("换内容要重试：opens=%d", r.opener.opens)
	}
	r.opener.err = nil
	r.req.ItemID = "item-3"
	r.frame()
	if !r.host.Open() || len(r.handler.take()) != 1 { // item-2 的失败通知已累计一条
		t.Errorf("恢复后应当能打开")
	}
}

func TestFailureIsReportedForTheFailedContentOnlyAndRetryClearsIt(t *testing.T) {
	r := newRig(t)
	r.opener.err = errors.New("WebView2 不可用")
	r.frame()
	if err := r.host.Failure(r.req); err == nil || !strings.Contains(err.Error(), "WebView2 不可用") {
		t.Fatalf("失败要能查到原因：%v", err)
	}
	other := r.req
	other.ItemID = "item-2"
	if r.host.Failure(other) != nil {
		t.Error("失败只属于那一份内容")
	}
	other = r.req
	other.Tab = TabSummary
	if r.host.Failure(other) != nil {
		t.Error("换标签页是另一份内容")
	}
	r.opener.err = nil
	r.frame()
	if r.host.Open() {
		t.Fatal("没点重试之前同一份内容不重开")
	}
	r.host.Retry()
	if r.host.Failure(r.req) != nil {
		t.Error("重试清掉失败记录")
	}
	r.frame()
	if !r.host.Open() || r.host.Failure(r.req) != nil {
		t.Error("重试之后下一帧重新打开")
	}
}

func TestNoOpenerFails(t *testing.T) {
	h := New(Options{})
	rec := &recHandler{}
	h.SetHandler(rec)
	h.Frame(&fakeSurface{}, testBounds, Request{ItemID: "a", Tab: TabBody, View: View{Kind: ViewMarkdown, Tab: TabBody}, Theme: lightTheme})
	if got := rec.take(); len(got) != 1 || !strings.HasPrefix(got[0], "failed:") {
		t.Errorf("没有 Opener 要报 Failed：%v", got)
	}
}

func TestInvalidThemeFailsInsteadOfPostingGarbage(t *testing.T) {
	r := newRig(t)
	r.req.Theme.BasePx = 0
	r.frame()
	if r.host.Open() || r.opener.opens != 0 {
		t.Errorf("外观无效不该开岛")
	}
	if got := r.handler.take(); len(got) != 1 || !strings.HasPrefix(got[0], "failed:") {
		t.Errorf("Failed：%v", got)
	}
	// 已开的岛上外观变成无效：关掉，不发垃圾。
	r = newRig(t)
	p := r.ready()
	n := len(p.posts)
	r.req.Theme.Hue = math.NaN()
	r.frame()
	if len(p.posts) != n || p.closes != 1 {
		t.Errorf("无效外观不该发出去：posts=%d closes=%d", len(p.posts)-n, p.closes)
	}
}

func TestEndFrameClosesPageThatWasNotDrawn(t *testing.T) {
	r := newRig(t)
	r.ready()
	r.host.EndFrame() // 这一帧画了（Frame 在前）
	if !r.host.Open() {
		t.Fatal("画了的这一帧不该关岛")
	}
	r.host.EndFrame() // 下一帧没画
	if r.host.Open() || r.page().closes != 1 {
		t.Fatalf("没画的帧要关岛：open=%v closes=%d", r.host.Open(), r.page().closes)
	}
	r.frame()
	if r.opener.opens != 2 || !r.host.Open() {
		t.Errorf("再画时按需重开：opens=%d", r.opener.opens)
	}
	if r.host.Ready() {
		t.Error("新开的页面还没握手")
	}
}

func TestViewNoneClosesTheOpenPage(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	r.req.View = View{Kind: ViewNone}
	r.frame()
	if r.host.Open() || p.closes != 1 {
		t.Errorf("内容没了要关岛：open=%v closes=%d", r.host.Open(), p.closes)
	}
	if len(r.handler.take()) != 0 {
		t.Error("正常关闭不是失败")
	}
}

func TestCloseIsIdempotentAndResetsState(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	r.host.Close()
	r.host.Close()
	if p.closes != 1 || r.host.Open() || r.host.Ready() || r.host.Rendered() {
		t.Errorf("closes=%d open=%v", p.closes, r.host.Open())
	}
	// 关闭后迟到的页面消息被丢弃。
	p.send(r.host, map[string]any{"t": "copy", "seq": 1, "id": 1, "text": "迟到"})
	r.wantCalls()
}

func TestExternalCloseResetsAndTheNextFrameReopens(t *testing.T) {
	r := newRig(t)
	first := r.ready()
	first.closed() // island.Group.CloseAll：收托盘前统一销毁
	if r.host.Open() || r.host.Ready() {
		t.Fatal("外部关闭后宿主要复位")
	}
	if first.closes != 0 {
		t.Error("外部已经关了，宿主不必再关一次")
	}
	r.frame()
	second := r.page()
	if second == first || r.opener.opens != 2 {
		t.Fatalf("下一帧要重开：opens=%d", r.opener.opens)
	}
	second.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	if got := second.kinds(t); !reflect.DeepEqual(got, []string{"init", "view"}) {
		t.Errorf("新页面从头握手：%v", got)
	}
	if second.msg(t, 1)["seq"] != float64(2) {
		t.Errorf("序号单调递增，不因重开回到 1：%v", second.msg(t, 1))
	}
	// 旧页面迟到的事件到不了 Handler（它已经被销毁，OnMessage 是旧闭包但宿主已经指向新页面）。
	first.send(r.host, map[string]any{"t": "copy", "seq": 1, "id": 1, "text": "旧页面"})
	r.wantCalls()
}

func TestSecondReadyResendsEverything(t *testing.T) {
	r := newRig(t)
	r.req.FindQuery = "词"
	p := r.ready()
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view", "find"}) {
		t.Fatalf("%v", got)
	}
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion}) // 页面被刷新
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view", "find", "init", "view", "find"}) {
		t.Fatalf("刷新后的页面什么状态都没有，要从头发：%v", got)
	}
	if p.msg(t, 4)["seq"] != float64(2) {
		t.Errorf("重发的 view 用新序号：%v", p.msg(t, 4))
	}
}

func TestReadyTimeoutFailsAndPollsUntilThen(t *testing.T) {
	r := newRig(t)
	r.frame()
	if len(r.surface.wakes) != 1 || r.surface.wakes[0] != readyPoll {
		t.Fatalf("等握手期间要定时出帧：%v", r.surface.wakes)
	}
	r.clock = r.clock.Add(defaultReadyTimeout - time.Second)
	r.frame()
	if !r.host.Open() || len(r.handler.take()) != 0 {
		t.Fatal("没到期不该失败")
	}
	r.clock = r.clock.Add(2 * time.Second)
	r.frame()
	if r.host.Open() || r.page().closes != 1 {
		t.Fatalf("超时要关岛：open=%v", r.host.Open())
	}
	if got := r.handler.take(); len(got) != 1 || !strings.Contains(got[0], "超时") {
		t.Errorf("Failed：%v", got)
	}
	wakes := len(r.surface.wakes)
	r.frame()
	if len(r.surface.wakes) != wakes || r.opener.opens != 1 {
		t.Error("失败后同一份内容不再重试，也不再轮询")
	}
}

func TestReadyTimeoutIsConfigurable(t *testing.T) {
	r := newRig(t)
	r.host.opts.ReadyTimeout = 3 * time.Second
	r.frame()
	r.clock = r.clock.Add(4 * time.Second)
	r.frame()
	if r.host.Open() {
		t.Error("自定义的超时没有生效")
	}
}

func TestNoPollingOnceReady(t *testing.T) {
	r := newRig(t)
	r.ready()
	n := len(r.surface.wakes)
	r.frame()
	r.frame()
	if len(r.surface.wakes) != n {
		t.Errorf("握手完成后不该再定时出帧：%v", r.surface.wakes[n:])
	}
}

func TestPageErrorEventOnlyLeavesADiagnostic(t *testing.T) {
	r := newRig(t)
	p := r.ready()
	p.send(r.host, map[string]any{"t": "error", "seq": 1, "message": "TypeError: secret-path C:\\Users\\x"})
	r.wantCalls()
	if len(r.diags) != 1 || strings.Contains(r.diags[0], "secret-path") {
		t.Errorf("页面报错只记长度，不记内容：%v", r.diags)
	}
}

func TestOnDeniedLeavesNoAddressInTheLog(t *testing.T) {
	r := newRig(t)
	r.frame()
	r.page().cfg.OnDenied("https://evil.example/steal?token=abc", true)
	if len(r.diags) != 1 || strings.Contains(r.diags[0], "evil") || strings.Contains(r.diags[0], "token") {
		t.Errorf("导航被拒的诊断不能含地址：%v", r.diags)
	}
}

func TestServeIsWiredToTheHost(t *testing.T) {
	r := newRig(t)
	r.frame()
	var cfg island.Config = r.page().cfg
	mime, data, ok := cfg.Serve("reader.js")
	if !ok || !strings.HasPrefix(mime, "text/javascript") || len(data) == 0 {
		t.Errorf("Serve(reader.js) = %q %d %v", mime, len(data), ok)
	}
}
