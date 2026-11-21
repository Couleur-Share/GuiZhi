package reader

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

// 图片查看岛宿主的测试：假页面、假打开器、假画布，不需要真的 WebView2。

type recViewerHandler struct{ calls []string }

func (r *recViewerHandler) Closed()          { r.calls = append(r.calls, "closed") }
func (r *recViewerHandler) Failed(err error) { r.calls = append(r.calls, "failed:"+err.Error()) }
func (r *recViewerHandler) take() string {
	out := strings.Join(r.calls, "|")
	r.calls = nil
	return out
}

type viewerRig struct {
	t       *testing.T
	host    *Host
	viewer  *Viewer
	opener  *fakeOpener
	handler *recViewerHandler
	surface *fakeSurface
	clock   time.Time
	diags   []string
}

var viewerBounds = gfx.Rect{X: 0, Y: 0, Width: 1280, Height: 800}

func newViewerRig(t *testing.T) *viewerRig {
	t.Helper()
	r := &viewerRig{t: t, opener: &fakeOpener{}, handler: &recViewerHandler{}, surface: &fakeSurface{}, clock: time.Unix(1_700_000_000, 0)}
	r.host = New(Options{
		Opener: r.opener,
		Diag:   func(err error) { r.diags = append(r.diags, err.Error()) },
		Now:    func() time.Time { return r.clock },
		Media: func(scheme, name string) (string, []byte, bool) {
			return "image/png", []byte(scheme + ":" + name), true
		},
	})
	r.viewer = r.host.Viewer()
	r.viewer.SetHandler(r.handler)
	return r
}

var viewerImages = []Image{{Src: "local-image://a b.png", Alt: "图 A"}, {Src: "local-image://c.png"}}

func (r *viewerRig) show() { r.viewer.Show(viewerImages, 1, lightTheme, LangZh) }

// frame 画一帧（整窗浮层里的 Frame，之后是这一帧的收尾）。
func (r *viewerRig) frame() {
	r.viewer.Frame(r.surface, viewerBounds)
	r.host.EndFrame()
}

func (r *viewerRig) page() *fakePage {
	r.t.Helper()
	p := r.opener.last()
	if p == nil {
		r.t.Fatal("还没有打开过查看岛")
	}
	return p
}

// ready 显示、画一帧（开岛）并让页面完成握手。
func (r *viewerRig) ready() *fakePage {
	r.t.Helper()
	r.show()
	r.frame()
	p := r.page()
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	return p
}

func TestViewerOpensLazilyAndStaysUnplacedUntilTheHandshake(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	if r.viewer.Active() || r.viewer.Open() {
		t.Fatal("没有 Show 之前不该有岛")
	}
	r.viewer.Frame(r.surface, viewerBounds)
	if r.opener.opens != 0 {
		t.Fatal("没有 Show 时 Frame 不该开岛")
	}
	r.show()
	if !r.viewer.Active() || r.viewer.Open() {
		t.Fatal("Show 之后要显示，但岛要等第一次 Frame 才开")
	}
	r.frame()
	p := r.page()
	if p.cfg.Scheme != viewerScheme || p.cfg.Entry != "viewer.html" || p.cfg.CSP != readerCSP {
		t.Errorf("岛的配置不对：scheme %q entry %q", p.cfg.Scheme, p.cfg.Entry)
	}
	if len(p.places) != 0 || r.viewer.Ready() {
		t.Fatal("握手之前不能摆放原生视图：界面一侧的遮罩要顶着")
	}
	if len(r.surface.wakes) == 0 || r.surface.wakes[len(r.surface.wakes)-1] != readyPoll {
		t.Errorf("握手期间要隔一会儿出一帧：%v", r.surface.wakes)
	}
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	if !r.viewer.Ready() {
		t.Fatal("ready 之后应握手完成")
	}
	r.frame()
	if len(p.places) != 1 || p.places[0] != viewerBounds {
		t.Fatalf("握手之后的第一帧要把岛摆在整窗上：%v", p.places)
	}
}

func TestViewerInitCarriesImagesIndexThemeAndLang(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	p := r.ready()
	if got := p.kinds(t); len(got) != 1 || got[0] != "init" {
		t.Fatalf("握手后只发一条 init：%v", got)
	}
	var m viewerInitMessage
	if err := json.Unmarshal([]byte(p.posts[0]), &m); err != nil {
		t.Fatal(err)
	}
	if m.V != ProtocolVersion || m.Index != 1 || m.Lang != LangZh || m.Theme != lightTheme {
		t.Errorf("init 的外观或序号不对：%+v", m)
	}
	if len(m.Images) != 2 || m.Images[0] != (viewerImage{Src: "local-image://a b.png", Alt: "图 A"}) || m.Images[1] != (viewerImage{Src: "local-image://c.png"}) {
		t.Errorf("init 的图片不对：%+v", m.Images)
	}
	if strings.Contains(p.posts[0], `"alt":""`) {
		t.Error("没有说明的图不该带空 alt")
	}
}

func TestViewerHandsFocusToThePageAfterItHasBeenPlacedForAFewFrames(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	p := r.ready()
	r.frame()
	if p.focuses != 0 {
		t.Fatal("刚摆上的那一帧原生视图还没显示出来，不能同一帧要焦点")
	}
	r.frame()
	if p.focuses != 1 {
		t.Fatalf("摆放了 %d 帧之后应把焦点交给页面一次：%d", viewerFocusFrames, p.focuses)
	}
	r.frame()
	r.frame()
	if p.focuses != 1 {
		t.Fatalf("焦点只交一次，之后不能每帧抢：%d", p.focuses)
	}
}

func TestViewerCloseEventDestroysTheIslandAndTellsTheHandler(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	p := r.ready()
	r.frame()
	p.send(r.host, map[string]any{"t": "close"})
	if r.viewer.Active() || r.viewer.Open() || p.closes != 1 {
		t.Fatalf("用户关闭之后岛应销毁、不再显示：active=%v open=%v closes=%d", r.viewer.Active(), r.viewer.Open(), p.closes)
	}
	if got := r.handler.take(); got != "closed" {
		t.Fatalf("应通知 Closed：%q", got)
	}
	r.frame()
	if r.opener.opens != 1 {
		t.Fatal("关闭之后不能再把岛开起来")
	}
}

func TestViewerIgnoresCloseBeforeTheHandshakeAndMalformedEvents(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	r.show()
	r.frame()
	p := r.page()
	p.send(r.host, map[string]any{"t": "close"})
	if !r.viewer.Active() || !r.viewer.Open() || r.handler.take() != "" {
		t.Fatal("握手之前的 close 不认")
	}
	for _, text := range []string{`not json`, `{"t":"nope"}`, `{"t":"ready"}`, `{"t":"error","message":5}`, strings.Repeat("x", maxViewerEventBytes+1)} {
		p.cfg.OnMessage(text)
	}
	if r.viewer.Ready() || !r.viewer.Active() {
		t.Fatal("坏事件不能改变状态")
	}
	if len(r.diags) != 5 {
		t.Fatalf("每条坏事件都要记一条诊断：%d %q", len(r.diags), r.diags)
	}
	for _, d := range r.diags {
		if strings.Contains(d, "nope") || strings.Contains(d, "not json") {
			t.Errorf("诊断不能带页面给的内容：%s", d)
		}
	}
}

func TestViewerRejectsAMismatchedProtocolVersion(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	r.show()
	r.frame()
	r.page().send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion + 1})
	if got := r.handler.take(); !strings.HasPrefix(got, "failed:") || !strings.Contains(got, "协议版本") {
		t.Fatalf("协议版本不符应按失败处理：%q", got)
	}
	if r.viewer.Active() || r.viewer.Open() {
		t.Fatal("失败之后不再显示、岛已销毁")
	}
}

func TestViewerFailsWhenTheIslandCannotOpen(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	r.opener.err = errors.New("没有 WebView2")
	r.show()
	r.frame()
	if got := r.handler.take(); !strings.HasPrefix(got, "failed:") || !strings.Contains(got, "没有 WebView2") {
		t.Fatalf("打不开应报失败并带原因：%q", got)
	}
	if r.viewer.Active() {
		t.Fatal("打不开就结束这次显示")
	}
	r.frame()
	if r.opener.opens != 1 {
		t.Fatalf("不能每帧重试：%d", r.opener.opens)
	}

	noOpener := New(Options{})
	h := &recViewerHandler{}
	noOpener.Viewer().SetHandler(h)
	noOpener.Viewer().Show(viewerImages, 0, lightTheme, LangZh)
	noOpener.Viewer().Frame(&fakeSurface{}, viewerBounds)
	if got := h.take(); !strings.HasPrefix(got, "failed:") {
		t.Fatalf("没有 Opener 应报失败：%q", got)
	}
}

func TestViewerFailsWhenTheHandshakeTimesOut(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	r.show()
	r.frame()
	p := r.page()
	r.clock = r.clock.Add(defaultReadyTimeout - time.Second)
	r.frame()
	if r.handler.take() != "" || !r.viewer.Active() {
		t.Fatal("没到期不能判失败")
	}
	r.clock = r.clock.Add(2 * time.Second)
	r.frame()
	if got := r.handler.take(); !strings.Contains(got, "超时") {
		t.Fatalf("到期应报超时：%q", got)
	}
	if p.closes != 1 || r.viewer.Active() {
		t.Fatal("超时之后岛应销毁")
	}
}

func TestViewerEndsWhenNobodyDrawsItForAFrame(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	p := r.ready()
	r.frame()
	r.host.EndFrame() // 这一帧没有 Frame：知识库没画出来
	if r.viewer.Active() || r.viewer.Open() || p.closes != 1 {
		t.Fatalf("没人画它的那一帧收尾时要结束显示并销毁岛：active=%v open=%v closes=%d", r.viewer.Active(), r.viewer.Open(), p.closes)
	}
	if r.handler.take() != "" {
		t.Fatal("宿主自己收掉不算用户关闭")
	}
}

func TestViewerShowBetweenFramesGetsAFrameOfGrace(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	r.host.EndFrame()
	r.show() // 两帧之间（页面事件里）显示
	r.host.EndFrame()
	if !r.viewer.Active() {
		t.Fatal("Show 之后紧跟的一次收尾不能把它收掉：界面下一帧才画整窗浮层")
	}
	r.host.EndFrame()
	if r.viewer.Active() {
		t.Fatal("再一帧还没人画就该收掉")
	}
}

func TestViewerShowWhileOpenSwapsTheImages(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	p := r.ready()
	r.viewer.Show([]Image{{Src: "local-image://z.png"}}, 5, lightTheme, LangEn)
	if got := p.kinds(t); len(got) != 2 || got[1] != "init" {
		t.Fatalf("已开着时换内容要再发一条 init：%v", got)
	}
	var m viewerInitMessage
	if err := json.Unmarshal([]byte(p.posts[1]), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Images) != 1 || m.Index != 0 || m.Lang != LangEn {
		t.Errorf("越界的序号按 0，语言跟着变：%+v", m)
	}
	if r.opener.opens != 1 {
		t.Fatal("换内容不重开岛")
	}
}

func TestViewerShowRejectsEmptyListsAndBadThemes(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	r.viewer.Show(nil, 0, lightTheme, LangZh)
	if r.viewer.Active() || len(r.diags) != 1 {
		t.Fatalf("没有图片不显示、记一条诊断：active=%v diags=%q", r.viewer.Active(), r.diags)
	}
	r.viewer.Show(viewerImages, 0, Theme{BasePx: 2, Motion: MotionStandard}, LangZh)
	if r.viewer.Active() || !strings.HasPrefix(r.handler.take(), "failed:") {
		t.Fatal("坏外观按失败处理")
	}
}

func TestViewerHideDestroysTheIslandWithoutTellingTheHandler(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	p := r.ready()
	r.viewer.Hide()
	if p.closes != 1 || r.viewer.Active() || r.handler.take() != "" {
		t.Fatal("界面一侧收起查看器：岛销毁、不通知 Handler")
	}
	r.viewer.Hide()
	if p.closes != 1 {
		t.Fatal("重复收起不能重复关岛")
	}
}

func TestViewerSurvivesTheIslandBeingClosedFromOutsideAndReopens(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	p := r.ready()
	r.frame()
	p.closed() // 收托盘前 island.Group 统一销毁
	if !r.viewer.Active() || r.viewer.Open() {
		t.Fatal("被外部关掉的岛：仍要显示，只是岛没了")
	}
	r.frame()
	if r.opener.opens != 2 {
		t.Fatalf("下一帧应重开：%d", r.opener.opens)
	}
	// 旧页面迟到的消息不能影响新页面：新页面还没握手时旧页面的 ready 不能让它被当成已握手（也不能让它收到 init），
	// 新页面握手之后旧页面的 close 也不能把查看器关掉。
	p2 := r.page()
	if p2 == p {
		t.Fatal("重开应是一个新页面")
	}
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	if r.viewer.Ready() || len(p2.posts) != 0 {
		t.Fatal("旧页面迟到的 ready 要丢弃：新页面还没握手")
	}
	p2.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	if !r.viewer.Ready() || len(p2.posts) != 1 {
		t.Fatal("新页面自己的 ready 要认")
	}
	p.send(r.host, map[string]any{"t": "close"})
	if !r.viewer.Active() || r.handler.take() != "" {
		t.Fatal("旧页面迟到的 close 要丢弃")
	}
}

func TestViewerPageScriptErrorsAreOnlyDiagnosedWithoutTheirText(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	p := r.ready()
	before := len(r.diags)
	p.send(r.host, map[string]any{"t": "error", "message": "密钥 sk-123 泄露"})
	if !r.viewer.Active() || !r.viewer.Ready() || r.handler.take() != "" {
		t.Fatal("页面的脚本错误只记诊断：不结束查看器，也不当成失败")
	}
	if len(r.diags) != before+1 || !strings.Contains(r.diags[before], "脚本错误") || strings.Contains(r.diags[before], "sk-123") {
		t.Fatalf("要有且只有一条诊断，且不含页面给的文字：%v", r.diags[before:])
	}
}

func TestViewerDeniedNavigationIsDiagnosedWithoutTheAddressAndOnlyForTheCurrentPage(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	old := r.ready()
	old.closed() // 岛被外部关掉，下一帧重开
	r.frame()
	cur := r.page()
	before := len(r.diags)
	old.cfg.OnDenied("https://old.example/a?token=1", false)
	if len(r.diags) != before {
		t.Fatal("旧页面迟到的导航拒绝要丢弃")
	}
	cur.cfg.OnDenied("https://secret.example/a?token=1", true)
	if len(r.diags) != before+1 || !strings.Contains(r.diags[before], "被拒绝") || strings.Contains(r.diags[before], "secret") {
		t.Fatalf("当前页面的导航拒绝记一条诊断，地址不入日志：%v", r.diags[before:])
	}
}

func TestHostCloseAlsoDestroysTheViewer(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	p := r.ready()
	r.host.Close()
	if p.closes != 1 || r.viewer.Active() {
		t.Fatal("窗口关闭、折叠到托盘前要连查看岛一起销毁")
	}
}

func TestHostFocusReturnsTheKeyboardToTheReaderPage(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.host.Focus()
	r.frame()
	r.host.Focus()
	p := r.page()
	if p.focuses != 0 {
		t.Fatal("没握手的页面不接焦点")
	}
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	r.host.Focus()
	if p.focuses != 1 {
		t.Fatalf("握手之后应把焦点交给阅读页：%d", p.focuses)
	}
}

func TestViewerServesItsPageAssetsAndMedia(t *testing.T) {
	t.Parallel()
	r := newViewerRig(t)
	r.viewer.theme, r.viewer.lang = Theme{Dark: true, Hue: 12, Saturation: 34, BasePx: 18, Motion: MotionOff}, LangEn
	mime, data, ok := r.viewer.serve("viewer.html")
	if !ok || !strings.HasPrefix(mime, "text/html") {
		t.Fatalf("入口页：%q %v", mime, ok)
	}
	html := string(data)
	for _, want := range []string{`<html lang="en" class="dark"`, `--theme-hue:12`, `--base-font-size:18px`, `data-motion="off"`, "./viewer.js", "./viewer.css"} {
		if !strings.Contains(html, want) {
			t.Errorf("入口页缺 %q：\n%s", want, html)
		}
	}
	if _, _, ok := r.viewer.serve(""); !ok {
		t.Error("空路径等同入口页")
	}
	for _, p := range []string{"viewer.js", "viewer.css"} {
		if _, d, ok := r.viewer.serve(p); !ok || len(d) == 0 {
			t.Errorf("查看岛资源 %s 应能取到", p)
		}
	}
	if mime, data, ok := r.viewer.serve("media/local-image/图 1.png"); !ok || mime != "image/png" || string(data) != "local-image:图 1.png" {
		t.Errorf("本地图片：%q %q %v", mime, data, ok)
	}
	// 阅读岛的文件不属于查看岛；路径要规整。
	for _, p := range []string{"reader.js", "index.html", "../index.html", "viewer/../index.html", `a\b`, "a:b", ".hidden", "media/local-audio/a.mp3", "media/local-image/"} {
		if _, _, ok := r.viewer.serve(p); ok {
			t.Errorf("不该提供 %q", p)
		}
	}
	if _, _, ok := New(Options{}).Viewer().serve("media/local-image/a.png"); ok {
		t.Error("没有 Media 时不提供媒体")
	}
}

func TestParseViewerEvent(t *testing.T) {
	t.Parallel()
	good := map[string]ViewerEvent{
		`{"t":"ready","v":1}`:           {Kind: ViewerEventReady, Version: 1},
		`{"t":"close"}`:                 {Kind: ViewerEventClose},
		`{"t":"error","message":"出错了"}`: {Kind: ViewerEventError, Message: "出错了"},
		`{"t":"error","message":""}`:    {Kind: ViewerEventError},
	}
	for text, want := range good {
		got, err := ParseViewerEvent(text)
		if err != nil || got != want {
			t.Errorf("%s：%+v %v，应为 %+v", text, got, err, want)
		}
	}
	bad := []string{
		``, `[]`, `{"t":"ready"}`, `{"t":"ready","v":1.5}`, `{"t":"ready","v":-1}`, `{"t":"ready","v":"1"}`,
		`{"t":"error"}`, `{"t":"error","message":1}`, `{"t":"scroll","seq":1,"top":0}`, `{"t":""}`,
		"{\"t\":\"close\"}\xff", `{"t":"error","message":"` + strings.Repeat("x", maxErrorBytes+1) + `"}`,
		// 字符串里的非法 UTF-8：json 解码会悄悄换成 U+FFFD，只有显式校验才拦得住。
		"{\"t\":\"error\",\"message\":\"a\xffb\"}",
		// 其余字段都合规，只是超过长度上限一个字节（多余字段 json 会忽略，只有长度校验拦得住）。
		`{"t":"close","pad":"` + strings.Repeat("x", maxViewerEventBytes-len(`{"t":"close","pad":""}`)+1) + `"}`,
		// 与常量无关的字面量下限：1 MiB 的事件无论上限怎么调都不该放行（常量被改成「近似不设限」时上面两条会跟着常量走）。
		`{"t":"close","pad":"` + strings.Repeat("x", 1<<20) + `"}`,
	}
	for _, text := range bad {
		if _, err := ParseViewerEvent(text); !errors.Is(err, ErrBadViewerEvent) {
			t.Errorf("%.40q 应被拒绝：%v", text, err)
		}
	}
	// 长度恰好在上限上的合规事件要认（边界）。
	exact := `{"t":"close","pad":"` + strings.Repeat("x", maxViewerEventBytes-len(`{"t":"close","pad":""}`)) + `"}`
	if len(exact) != maxViewerEventBytes {
		t.Fatalf("测试数据长度 %d 不是上限 %d", len(exact), maxViewerEventBytes)
	}
	if got, err := ParseViewerEvent(exact); err != nil || got.Kind != ViewerEventClose {
		t.Errorf("恰好在上限上的事件要认：%+v %v", got, err)
	}
}
