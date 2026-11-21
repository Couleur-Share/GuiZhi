package reader

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// 网页快照视图（ViewSnapshot）的协议与宿主测试：编码与校验、页面事件、增量同步（宽度与目录开合不重发整页）、
// 回调映射（查找命中、链接、图片、转发键、版面状态）。

const snapshotDoc = `<!doctype html><html><body><h2>章节</h2><p>正文 <a href="https://example.com">链接</a></p><img src="local-image://a.png"></body></html>`

// adaptedSnapshotDoc 是 snapshotDoc 发给页面时的样子：本地图片地址已换成岛里的相对路径（snapshot_adapt.go）。
const adaptedSnapshotDoc = `<!doctype html><html><body><h2>章节</h2><p>正文 <a href="https://example.com">链接</a></p><img src="./media/local-image/a.png"></body></html>`

func snapshotView() View {
	return View{
		Kind: ViewSnapshot, SnapshotDoc: snapshotDoc, SnapshotInstanceID: "inst-1", SnapshotVersionID: "ver-1",
		SnapshotWidth: SnapshotWidthFluid, SnapshotAssets: []string{"local-image://a.png", "local-image://b.png"},
	}
}

// snapshotRig 是已握手、显示着网页快照的宿主。
func snapshotRig(t *testing.T) (*rig, *fakePage) {
	t.Helper()
	r := newRig(t)
	r.req.View = snapshotView()
	return r, r.ready()
}

func TestSnapshotViewMarshalShape(t *testing.T) {
	data, err := json.Marshal(snapshotView())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"kind": "snapshot", "document": adaptedSnapshotDoc, "instanceId": "inst-1", "versionId": "ver-1", "width": "fluid",
		"assets": []any{"local-image://a.png", "local-image://b.png"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("编码形状不符\n  实际 %v\n  期望 %v", got, want)
	}

	// 空宽度按 fluid；没有图片时 assets 是空数组而不是 null；版本标识可以为空（当前版本）。
	v := View{Kind: ViewSnapshot, SnapshotDoc: "<p>x</p>", SnapshotInstanceID: "i"}
	data, err = json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"width":"fluid"`) || !strings.Contains(string(data), `"assets":[]`) || !strings.Contains(string(data), `"versionId":""`) {
		t.Errorf("默认值：%s", data)
	}
}

func TestSnapshotViewMarshalRejects(t *testing.T) {
	tooManyAssets := make([]string, MaxLightboxImages+1)
	for i := range tooManyAssets {
		tooManyAssets[i] = "local-image://x.png"
	}
	mutate := func(f func(*View)) View {
		v := snapshotView()
		f(&v)
		return v
	}
	cases := map[string]View{
		"文档为空":       mutate(func(v *View) { v.SnapshotDoc = "" }),
		"文档超限":       mutate(func(v *View) { v.SnapshotDoc = strings.Repeat("a", MaxSnapshotDocBytes+1) }),
		"文档非法 UTF-8": mutate(func(v *View) { v.SnapshotDoc = "<p>\xff\xfe</p>" }),
		"实例标识为空":     mutate(func(v *View) { v.SnapshotInstanceID = "" }),
		"实例标识超长":     mutate(func(v *View) { v.SnapshotInstanceID = strings.Repeat("i", maxSnapshotIDBytes+1) }),
		"版本标识超长":     mutate(func(v *View) { v.SnapshotVersionID = strings.Repeat("v", maxSnapshotIDBytes+1) }),
		"宽度未知":       mutate(func(v *View) { v.SnapshotWidth = "huge" }),
		"图片过多":       mutate(func(v *View) { v.SnapshotAssets = tooManyAssets }),
		"图片是远程地址":    mutate(func(v *View) { v.SnapshotAssets = []string{"https://example.com/a.png"} }),
		"图片是文件地址":    mutate(func(v *View) { v.SnapshotAssets = []string{"file:///C:/a.png"} }),
		"图片空名称":      mutate(func(v *View) { v.SnapshotAssets = []string{"local-image://"} }),
		"图片地址超长": mutate(func(v *View) {
			v.SnapshotAssets = []string{"local-image://" + strings.Repeat("a", maxImageSrcBytes)}
		}),
	}
	for name, view := range cases {
		_, err := json.Marshal(view)
		if err == nil {
			t.Errorf("%s：应当拒绝编码", name)
			continue
		}
		if strings.Contains(err.Error(), "<p>") || strings.Contains(err.Error(), "章节") {
			t.Errorf("%s：错误文字不能带文档内容：%v", name, err)
		}
	}
}

func TestSnapshotViewMarshalLimitsAreInclusive(t *testing.T) {
	v := snapshotView()
	v.SnapshotDoc = strings.Repeat("a", MaxSnapshotDocBytes)
	v.SnapshotInstanceID = strings.Repeat("i", maxSnapshotIDBytes)
	v.SnapshotAssets = make([]string, MaxLightboxImages)
	for i := range v.SnapshotAssets {
		v.SnapshotAssets[i] = "local-image://" + strings.Repeat("a", maxImageSrcBytes-len("local-image://"))
	}
	if _, err := json.Marshal(v); err != nil {
		t.Fatalf("恰在上限内的快照应当可以编码：%v", err)
	}
}

func TestParseEventSnapshotState(t *testing.T) {
	ev := mustEvent(t, `{"t":"snapshotState","seq":2,"wideRail":true,"open":false}`)
	if want := (Event{Kind: EventSnapshotState, Seq: 2, WideRail: true}); !reflect.DeepEqual(ev, want) {
		t.Errorf("实际 %+v 期望 %+v", ev, want)
	}
	ev = mustEvent(t, `{"t":"snapshotState","seq":2,"wideRail":false,"open":true}`)
	if want := (Event{Kind: EventSnapshotState, Seq: 2, Open: true}); !reflect.DeepEqual(ev, want) {
		t.Errorf("实际 %+v 期望 %+v", ev, want)
	}
	if ev := mustEvent(t, `{"t":"key","seq":2,"key":"escape"}`); ev.Key != "escape" {
		t.Errorf("escape 在转发白名单内：%+v", ev)
	}
	for name, text := range map[string]string{
		"snapshotState 缺 wideRail": `{"t":"snapshotState","seq":1,"open":true}`,
		"snapshotState 缺 open":     `{"t":"snapshotState","seq":1,"wideRail":true}`,
		"snapshotState 缺序号":        `{"t":"snapshotState","wideRail":true,"open":true}`,
		"snapshotState 字段是数字":      `{"t":"snapshotState","seq":1,"wideRail":1,"open":true}`,
		"key 大写 Escape 不算":         `{"t":"key","seq":1,"key":"Escape"}`,
	} {
		if _, err := ParseEvent(text); !errors.Is(err, ErrBadEvent) {
			t.Errorf("%s：应当以 ErrBadEvent 拒绝，实际 %v", name, err)
		}
	}
}

func TestSnapshotViewIsSentOnceWithEveryField(t *testing.T) {
	r, p := snapshotRig(t)
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view"}) {
		t.Fatalf("消息序列：%v", got)
	}
	m := p.msg(t, 1)
	view := m["view"].(map[string]any)
	if m["seq"] != float64(1) || view["kind"] != "snapshot" || view["document"] != adaptedSnapshotDoc ||
		view["instanceId"] != "inst-1" || view["versionId"] != "ver-1" || view["width"] != "fluid" {
		t.Errorf("view = %v", m)
	}
	r.frame()
	r.frame()
	if got := p.kinds(t); len(got) != 2 {
		t.Errorf("没有变化的帧不该再发消息：%v", got)
	}
}

func TestSnapshotWidthChangeIsAnIncrementalUIMessage(t *testing.T) {
	r, p := snapshotRig(t)
	r.req.View.SnapshotWidth = SnapshotWidthWide
	r.frame()
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view", "ui"}) {
		t.Fatalf("只改宽度只该发 ui，不能重发整页：%v", got)
	}
	if m := p.msg(t, 2); m["snapshotWidth"] != "wide" || m["tocOpen"] != false {
		t.Errorf("ui = %v", m)
	}
	if r.host.Seq() != 1 {
		t.Errorf("宽度变化不换内容身份，序号应不变：%d", r.host.Seq())
	}
	// 空值等同 fluid：从 wide 改回 fluid 要发。
	r.req.View.SnapshotWidth = ""
	r.frame()
	if m := p.msg(t, 3); p.kinds(t)[3] != "ui" || m["snapshotWidth"] != "fluid" {
		t.Errorf("回到默认宽度：%v", m)
	}
	r.frame()
	if len(p.posts) != 4 {
		t.Errorf("宽度已同步，不该再发：%v", p.kinds(t))
	}
}

func TestSnapshotTocToggleIsAnIncrementalUIMessage(t *testing.T) {
	r, p := snapshotRig(t)
	r.req.TocOpen = true
	r.frame()
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view", "ui"}) {
		t.Fatalf("%v", got)
	}
	if m := p.msg(t, 2); m["tocOpen"] != true || m["snapshotWidth"] != "fluid" {
		t.Errorf("ui = %v", m)
	}
	r.req.TocOpen = false
	r.frame()
	if m := p.msg(t, 3); p.kinds(t)[3] != "ui" || m["tocOpen"] != false {
		t.Errorf("收起：%v", m)
	}
}

func TestSnapshotDocumentOrAssetsChangeResendsViewWithoutReopening(t *testing.T) {
	r, p := snapshotRig(t)
	r.req.View.SnapshotDoc = snapshotDoc + "<p>更新</p>"
	r.req.View.SnapshotInstanceID = "inst-2"
	r.frame()
	if got := p.kinds(t); !reflect.DeepEqual(got, []string{"init", "view", "view"}) {
		t.Fatalf("文档换了要重发 view：%v", got)
	}
	if m := p.msg(t, 2); m["seq"] != float64(2) || m["restoreTop"] != nil {
		t.Errorf("同一内容身份内的更新不恢复位置：%v", m)
	}
	r.req.View.SnapshotAssets = []string{"local-image://c.png"}
	r.frame()
	if got := p.kinds(t); len(got) != 4 || got[3] != "view" {
		t.Fatalf("图片清单变了要重发 view：%v", got)
	}
	if r.opener.opens != 1 {
		t.Errorf("同一内容身份内不重开岛：opens=%d", r.opener.opens)
	}
}

func TestSnapshotAssetsAreSnapshottedNotAliased(t *testing.T) {
	// 调用方每帧可能复用同一个切片并原地改写：宿主记下的「已发送」副本不能跟着变，否则改动被当成没变而漏发。
	r := newRig(t)
	assets := []string{"local-image://a.png", "local-image://b.png"}
	r.req.View = snapshotView()
	r.req.View.SnapshotAssets = assets
	p := r.ready()
	assets[0] = "local-image://z.png"
	r.frame()
	if got := p.kinds(t); len(got) != 3 || got[2] != "view" {
		t.Fatalf("原地改了图片清单，要重发 view：%v", got)
	}
	if view := p.msg(t, 2)["view"].(map[string]any); view["assets"].([]any)[0] != "local-image://z.png" {
		t.Errorf("assets = %v", view["assets"])
	}
}

func TestSnapshotOversizeDocumentFailsThePageWithoutLeakingIt(t *testing.T) {
	r := newRig(t)
	r.req.View = snapshotView()
	r.req.View.SnapshotDoc = "<p>机密正文</p>" + strings.Repeat("a", MaxSnapshotDocBytes)
	p := r.ready()
	if p.closes != 1 || r.host.Open() {
		t.Fatalf("超限文档按页面失败处理（关页）：closes=%d open=%v", p.closes, r.host.Open())
	}
	if r.host.Failure(r.req) == nil {
		t.Error("同一份内容的失败要能向宿主查到，界面据此画重试")
	}
	calls := r.handler.take()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "failed:") || strings.Contains(calls[0], "机密正文") {
		t.Errorf("Failed 的原因不能带文档内容：%q", calls)
	}
	for _, d := range r.diags {
		if strings.Contains(d, "机密正文") {
			t.Errorf("诊断不能带文档内容：%q", d)
		}
	}
}

func TestSnapshotUnknownWidthInUIMessageFailsThePage(t *testing.T) {
	r, p := snapshotRig(t)
	r.req.View.SnapshotWidth = "huge"
	r.frame()
	if p.closes != 1 || r.host.Failure(r.req) == nil {
		t.Fatalf("未知宽度要拒绝并按页面失败处理：closes=%d", p.closes)
	}
	if got := p.kinds(t); len(got) != 2 {
		t.Errorf("坏宽度不能发给页面：%v", got)
	}
}

func TestSnapshotStateEventReportsLayoutAndSuppressesEcho(t *testing.T) {
	r, p := snapshotRig(t)
	p.send(r.host, map[string]any{"t": "snapshotState", "seq": 1, "wideRail": true, "open": true})
	r.wantCalls("snapshotState:true/true")
	// 页面自己展开了目录：宿主记下它，下一帧即使期望里的 TocOpen 还没跟上，也不把旧值再发回页面。
	r.req.TocOpen = true
	r.frame()
	if got := p.kinds(t); len(got) != 2 {
		t.Errorf("页面已经是展开状态，不必再发 ui：%v", got)
	}
	p.send(r.host, map[string]any{"t": "snapshotState", "seq": 1, "wideRail": false, "open": false})
	r.wantCalls("snapshotState:false/false")
}

func TestSnapshotStateEventIsDroppedWhenStaleOrNotSnapshot(t *testing.T) {
	r, p := snapshotRig(t)
	r.req.ItemID = "item-2" // 换条目：序号变成 2
	r.frame()
	p.send(r.host, map[string]any{"t": "snapshotState", "seq": 1, "wideRail": true, "open": true})
	r.wantCalls()

	// 当前不是网页快照视图：即使序号对得上，也不该出现这个事件。
	other := newRig(t)
	op := other.ready()
	op.send(other.host, map[string]any{"t": "snapshotState", "seq": 1, "wideRail": true, "open": true})
	other.wantCalls()
}

func TestSnapshotMalformedEventsAreDroppedAndLogged(t *testing.T) {
	r, p := snapshotRig(t)
	for _, text := range []string{
		`{"t":"snapshotState","seq":1,"wideRail":"yes","open":true}`,
		`{"t":"snapshotState","seq":1}`,
		`{"t":"lightbox","seq":1,"index":5,"images":[{"src":"local-image://a.png"}]}`,
		`{"t":"lightbox","seq":1,"index":0,"images":[{"src":"https://example.com/a.png"}]}`,
		`{"t":"key","seq":1,"key":"delete"}`,
		`{"t":"matches","seq":1,"count":-3}`,
		`{"t":"link","seq":1,"url":""}`,
	} {
		p.cfg.OnMessage(text)
	}
	r.wantCalls()
	if len(r.diags) != 7 {
		t.Errorf("每条坏事件各记一条诊断：%d %q", len(r.diags), r.diags)
	}
}

func TestSnapshotEventsMapToExistingHandlerCallbacks(t *testing.T) {
	r, p := snapshotRig(t)
	cases := []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{"查找命中数", map[string]any{"t": "matches", "seq": 1, "count": 12}, "matches:12"},
		{"外链", map[string]any{"t": "link", "seq": 1, "url": "https://example.com/a?b=1"}, "link:https://example.com/a?b=1"},
		{"查找快捷键", map[string]any{"t": "key", "seq": 1, "key": "f", "ctrl": true}, "key:f/true/false/false"},
		{"退出专注阅读", map[string]any{"t": "key", "seq": 1, "key": "escape"}, "key:escape/false/false/false"},
		{"点图", map[string]any{"t": "lightbox", "seq": 1, "index": 1,
			"images": []map[string]any{{"src": "local-image://a.png"}, {"src": "local-image://b.png"}}},
			"lightbox:1:local-image://a.png|,local-image://b.png|"},
	}
	for _, c := range cases {
		p.send(r.host, c.fields)
		r.wantCalls(c.want)
	}
	if len(r.diags) != 0 {
		t.Errorf("合规事件不该产生诊断：%q", r.diags)
	}
}

func TestSnapshotLinksAreVettedByTheHost(t *testing.T) {
	r, p := snapshotRig(t)
	for _, url := range []string{
		"javascript:alert(1)", "file:///C:/Windows/win.ini", "data:text/html,<p>x</p>", "local-image://a.png",
		"https://user:pass@example.com/", "http:///no-host", "https://example.com/a\nb",
	} {
		p.send(r.host, map[string]any{"t": "link", "seq": 1, "url": url})
	}
	r.wantCalls()
	if len(r.diags) != 7 {
		t.Errorf("每个被拒的链接记一条诊断（地址本身不入日志）：%d", len(r.diags))
	}
	for _, d := range r.diags {
		if strings.Contains(d, "javascript") || strings.Contains(d, "pass") {
			t.Errorf("诊断不能带地址：%q", d)
		}
	}
}

func TestSnapshotPageReloadResendsEverything(t *testing.T) {
	r, p := snapshotRig(t)
	r.req.View.SnapshotWidth = SnapshotWidthOriginal
	r.req.TocOpen = true
	r.frame()
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion}) // 页面被刷新
	kinds := p.kinds(t)
	tail := kinds[len(kinds)-2:]
	if !reflect.DeepEqual(tail, []string{"init", "view"}) {
		t.Fatalf("重握手后从头发：%v", kinds)
	}
	m := p.msg(t, len(kinds)-1)
	if view := m["view"].(map[string]any); view["width"] != "original" || m["tocOpen"] != true {
		t.Errorf("重发的 view 要带当前宽度与目录开合：%v", m)
	}
}

func TestSnapshotViewSameContentIgnoresOnlyWidth(t *testing.T) {
	base := snapshotView()
	wide := base.clone()
	wide.SnapshotWidth = SnapshotWidthWide
	if !base.sameContent(wide) {
		t.Error("宽度不同不算内容不同")
	}
	for name, change := range map[string]func(*View){
		"文档":   func(v *View) { v.SnapshotDoc += "x" },
		"实例":   func(v *View) { v.SnapshotInstanceID = "other" },
		"版本":   func(v *View) { v.SnapshotVersionID = "other" },
		"图片增加": func(v *View) { v.SnapshotAssets = append(v.SnapshotAssets, "local-image://c.png") },
		"图片改名": func(v *View) { v.SnapshotAssets = []string{"local-image://a.png", "local-image://x.png"} },
		"图片清空": func(v *View) { v.SnapshotAssets = nil },
	} {
		other := base.clone()
		change(&other)
		if base.sameContent(other) {
			t.Errorf("%s不同应当算内容不同", name)
		}
	}
	// nil 与空切片等价（没有图片）。
	a, b := base.clone(), base.clone()
	a.SnapshotAssets, b.SnapshotAssets = nil, []string{}
	if !a.sameContent(b) {
		t.Error("nil 与空切片都是没有图片")
	}
}
