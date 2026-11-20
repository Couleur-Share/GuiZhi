package reader

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/island"
)

// 本文件是测试替身：假页面、假打开器、假画布、记录型 Handler。宿主的全部逻辑都不需要真的 WebView2。

type fakePage struct {
	cfg    island.Config
	closed func()
	posts  []string
	places []gfx.Rect
	closes int
}

func (p *fakePage) Post(text string)            { p.posts = append(p.posts, text) }
func (p *fakePage) Place(_ Surface, b gfx.Rect) { p.places = append(p.places, b) }
func (p *fakePage) Close()                      { p.closes++ }

// kinds 是已发消息的 t 字段，按发送顺序。
func (p *fakePage) kinds(t *testing.T) []string {
	t.Helper()
	out := make([]string, 0, len(p.posts))
	for i := range p.posts {
		out = append(out, fmt.Sprint(p.msg(t, i)["t"]))
	}
	return out
}

func (p *fakePage) msg(t *testing.T, i int) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(p.posts[i]), &m); err != nil {
		t.Fatalf("第 %d 条消息不是 JSON：%v\n%s", i, err, p.posts[i])
	}
	return m
}

// send 模拟页面向宿主发一条事件。
func (p *fakePage) send(h *Host, fields map[string]any) {
	text, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	p.cfg.OnMessage(string(text))
}

type fakeOpener struct {
	pages []*fakePage
	err   error
	opens int
}

func (o *fakeOpener) Open(cfg island.Config, closed func()) (Page, error) {
	o.opens++
	if o.err != nil {
		return nil, o.err
	}
	p := &fakePage{cfg: cfg, closed: closed}
	o.pages = append(o.pages, p)
	return p, nil
}

func (o *fakeOpener) last() *fakePage {
	if len(o.pages) == 0 {
		return nil
	}
	return o.pages[len(o.pages)-1]
}

type fakeSurface struct {
	views []gfx.NativeViewID
	wakes []time.Duration
}

func (s *fakeSurface) NativeView(id gfx.NativeViewID, _ gfx.Rect) { s.views = append(s.views, id) }
func (s *fakeSurface) RequestFrameIn(d time.Duration)             { s.wakes = append(s.wakes, d) }

// recHandler 把每个回调记成一行文字，测试直接比较。
type recHandler struct{ calls []string }

func (r *recHandler) rec(format string, args ...any) {
	r.calls = append(r.calls, fmt.Sprintf(format, args...))
}
func (r *recHandler) Matches(count int) { r.rec("matches:%d", count) }
func (r *recHandler) Scroll(id string, tab Tab, top float64) {
	r.rec("scroll:%s/%s/%v", id, tab, top)
}
func (r *recHandler) Catalog(open bool) { r.rec("catalog:%v", open) }
func (r *recHandler) Toc(open bool)     { r.rec("toc:%v", open) }
func (r *recHandler) Link(url string)   { r.rec("link:%s", url) }
func (r *recHandler) Copy(text string)  { r.rec("copy:%s", text) }
func (r *recHandler) Lightbox(images []Image, index int) {
	names := make([]string, 0, len(images))
	for _, image := range images {
		names = append(names, image.Src+"|"+image.Alt)
	}
	r.rec("lightbox:%d:%s", index, strings.Join(names, ","))
}
func (r *recHandler) Toast(text string) { r.rec("toast:%s", text) }
func (r *recHandler) Key(key string, ctrl, alt, shift bool) {
	r.rec("key:%s/%v/%v/%v", key, ctrl, alt, shift)
}
func (r *recHandler) FindReset()       { r.rec("findReset") }
func (r *recHandler) Failed(err error) { r.rec("failed:%v", err) }

func (r *recHandler) take() []string {
	out := r.calls
	r.calls = nil
	return out
}

var lightTheme = Theme{Hue: 210, Saturation: 35, BasePx: 16, Motion: MotionStandard}

// rig 是一套宿主 + 假打开器 + 记录型 Handler + 可拨的时钟。
type rig struct {
	t       *testing.T
	host    *Host
	opener  *fakeOpener
	handler *recHandler
	surface *fakeSurface
	clock   time.Time
	diags   []string
	req     Request
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, opener: &fakeOpener{}, handler: &recHandler{}, surface: &fakeSurface{}, clock: time.Unix(1_700_000_000, 0)}
	r.host = New(Options{
		Opener: r.opener,
		Diag:   func(err error) { r.diags = append(r.diags, err.Error()) },
		Now:    func() time.Time { return r.clock },
		Media: func(scheme, name string) (string, []byte, bool) {
			return "image/png", []byte(scheme + ":" + name), true
		},
	})
	r.host.SetHandler(r.handler)
	r.req = Request{
		ItemID: "item-1", Tab: TabBody,
		View:  View{Kind: ViewOriginal, Content: "# 标题\n\n正文", SourceKind: "body"},
		Theme: lightTheme, Lang: LangZh,
	}
	return r
}

var testBounds = gfx.Rect{X: 10, Y: 20, Width: 600, Height: 400}

// frame 以 r.req 画一帧。
func (r *rig) frame() { r.host.Frame(r.surface, testBounds, r.req) }

// page 是最近打开的页面。
func (r *rig) page() *fakePage {
	r.t.Helper()
	p := r.opener.last()
	if p == nil {
		r.t.Fatal("还没有打开过页面")
	}
	return p
}

// ready 画一帧（打开页面）并让页面完成握手。
func (r *rig) ready() *fakePage {
	r.t.Helper()
	r.frame()
	p := r.page()
	p.send(r.host, map[string]any{"t": "ready", "v": ProtocolVersion})
	return p
}

func (r *rig) wantCalls(want ...string) {
	r.t.Helper()
	got := r.handler.take()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		r.t.Errorf("Handler 调用不符\n  实际 %q\n  期望 %q", got, want)
	}
}
