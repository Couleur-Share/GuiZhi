//go:build windows

// readersmoke 在真 WebView2 里核实阅读岛（viva/reader 加 apps/desktop/src/renderer/reader 的页面）的真窗口行为：
// 握手与渲染、首帧底色、主题切换、页内查找计数、阅读位置恢复与上报、更高层的原生控件盖住岛（画面与命中）、关闭后重开、
// 页面事件（链接、复制、查看器、快捷键）经真消息通道到宿主。
// 不进产品；结果写成 report.json 与截图，结论记在台账 R-15 与开发文档 §8.3。
//
// 用法（在 apps/desktop-go 下；务必用一次性的 LOCALAPPDATA，别碰真实的 %LOCALAPPDATA%\GuiZhi）：
//
//	pwsh -NoProfile -File D:\Project\GuiZhi-bench\tools\run-reader-smoke.ps1 -Label r1
//
// 它会在屏幕上开一个窗口（临时置顶，要从屏幕上截图），跑完自己退出；全程不碰用户的归知与数据目录。
// 不测的：真实的指针、滚轮、Tab 焦点、F5、Esc（脚本合成的 DOM 事件不等于真实输入，台账 R-15）。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/island"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/reader"
)

// smokeClass 是 WebView2 profile 的专用目录名（绝不能叫 GuiZhi）。
const smokeClass = "GuiZhiReaderSmoke"

const (
	contentW, contentH = 960, 640 // 窗口内容区（逻辑像素）
	itemID             = "smoke-item"
)

func main() {
	out := flag.String("out", "", "输出目录（截图与 report.json），必填")
	only := flag.String("only", "", "只跑这些场景，逗号分隔；空为全部")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "需要 -out")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "建输出目录失败：", err)
		os.Exit(1)
	}
	// 与产品一样登记窗口 Class：开岛前会清理以前留下的临时 profile 目录（R5）。
	island.SetProfileClass(smokeClass)

	want := map[string]bool{}
	for _, name := range strings.Split(*only, ",") {
		if name = strings.TrimSpace(name); name != "" {
			want[name] = true
		}
	}
	s, err := newSmoke(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败：", err)
		os.Exit(1)
	}
	report := s.runApp(want)
	text, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(text))
	if err := os.WriteFile(filepath.Join(*out, "report.json"), append(text, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写报告失败：", err)
		os.Exit(1)
	}
	for _, r := range report.Results {
		if r.Error != "" {
			os.Exit(3)
		}
		for _, c := range r.Checks {
			if !c.OK {
				os.Exit(3)
			}
		}
	}
}

type event struct {
	AtMs float64 `json:"atMs"`
	Kind string  `json:"kind"`
	Data any     `json:"data,omitempty"`
}

type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail any    `json:"detail,omitempty"`
}

type result struct {
	Name   string         `json:"name"`
	Checks []check        `json:"checks"`
	Data   map[string]any `json:"data,omitempty"`
	Error  string         `json:"error,omitempty"`
	TookMs float64        `json:"tookMs"`
}

func (r *result) check(name string, ok bool, detail any) {
	r.Checks = append(r.Checks, check{Name: name, OK: ok, Detail: detail})
}

func (r *result) set(key string, value any) {
	if r.Data == nil {
		r.Data = map[string]any{}
	}
	r.Data[key] = value
}

type report struct {
	StartedAt string         `json:"startedAt"`
	Env       map[string]any `json:"env"`
	Results   []*result      `json:"results"`
	Events    []event        `json:"events"`
	RunError  string         `json:"runError,omitempty"`
}

// floatSpec 是盖在岛上的一块原生色块（更高一层的 Overlay）。
type floatSpec struct {
	band ui.Band
	rect gfx.Rect
	col  gfx.Color
}

type smoke struct {
	out   string
	web   *vapp.WebHost
	inv   vapp.Invalidator
	app   *vapp.Application
	win   *vapp.Window
	host  *reader.Host
	group island.Group
	media reader.Media
	spec  kit.Spec
	bg    gfx.Color
	theme gfx.Theme
	start time.Time

	hwnd  uintptr
	scale float64

	mu   sync.Mutex
	jobs []func()
	log  []event

	// 窗口线程独占：
	copyErr error // 非空时宿主的 Copy 返回它（测复制回执的失败路径）
	isl     *island.Island
	shown   bool
	req     reader.Request
	bounds  *gfx.Rect
	float   *floatSpec
	tree    *floatSpec // 同层（页面层）里、岛之后画的色块
}

func newSmoke(out string) (*smoke, error) {
	imgs := filepath.Join(out, "media")
	if err := os.MkdirAll(imgs, 0o755); err != nil {
		return nil, err
	}
	if err := writeFixtureImages(imgs); err != nil {
		return nil, err
	}
	s := &smoke{out: out, web: &vapp.WebHost{}, spec: kit.DefaultSpec(), start: time.Now(), theme: gfx.DarkTheme()}
	s.bg = gfx.Color{Red: 0.1, Green: 0.1, Blue: 0.12, Alpha: 1}
	s.media = func(scheme, name string) (string, []byte, bool) {
		if scheme != "local-image" || strings.ContainsAny(name, `/\:`) {
			return "", nil, false
		}
		data, err := os.ReadFile(filepath.Join(imgs, name))
		if err != nil {
			return "", nil, false
		}
		return "image/png", data, true
	}
	s.host = reader.New(reader.Options{
		Opener: smokeOpener{s},
		Media:  s.media,
		Diag:   func(err error) { s.record("diag", err.Error()) },
	})
	s.host.SetHandler(handler{s})
	s.host.Viewer().SetHandler(viewerHandler{s})
	return s, nil
}

func (s *smoke) record(kind string, data any) {
	s.mu.Lock()
	s.log = append(s.log, event{AtMs: ms(time.Since(s.start)), Kind: kind, Data: data})
	s.mu.Unlock()
}

func ms(d time.Duration) float64 { return math.Round(float64(d)/float64(time.Millisecond)*10) / 10 }

// mark 返回此刻事件日志的长度；waitEvent 只看 mark 之后的事件。
func (s *smoke) mark() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.log)
}

func (s *smoke) waitEvent(mark int, kind string, timeout time.Duration, pred func(any) bool) (event, bool) {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		for _, e := range s.log[mark:] {
			if e.Kind == kind && (pred == nil || pred(e.Data)) {
				s.mu.Unlock()
				return e, true
			}
		}
		s.mu.Unlock()
		if time.Now().After(deadline) {
			return event{}, false
		}
		time.Sleep(8 * time.Millisecond)
	}
}

func (s *smoke) eventsSince(mark int, kind string) []event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event
	for _, e := range s.log[mark:] {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// post 把 fn 排到窗口线程上（OnWake）；onUI 在此之上等它跑完。任意 goroutine。
func (s *smoke) post(fn func()) {
	s.mu.Lock()
	s.jobs = append(s.jobs, fn)
	s.mu.Unlock()
	s.inv.Invalidate()
}

func (s *smoke) drain() {
	s.mu.Lock()
	jobs := s.jobs
	s.jobs = nil
	s.mu.Unlock()
	for _, job := range jobs {
		job()
	}
}

func (s *smoke) onUI(fn func()) {
	done := make(chan struct{})
	s.post(func() {
		defer close(done)
		fn()
	})
	<-done
}

// waitUI 每隔一会儿在窗口线程上问 cond，直到为真或超时。
func (s *smoke) waitUI(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		var ok bool
		s.onUI(func() { ok = cond() })
		if ok {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// view 是窗口的界面：阅读区铺满内容区，岛按需由宿主打开；float 是更高层的原生色块，tree 是同层色块。
func (s *smoke) view(c ui.C) {
	ui.Column(c).Fill().Draw(func(d *ui.DrawContext) {
		d.Canvas.Fill(d.Rect, s.bg)
		if s.shown {
			bounds := d.Rect
			if s.bounds != nil {
				bounds = *s.bounds
			}
			s.host.Frame(d.Canvas, bounds, s.req)
		}
		if s.tree != nil {
			d.Canvas.Fill(s.tree.rect, s.tree.col)
		}
	})
	if v := s.host.Viewer(); v.Active() {
		// 与知识库的整窗浮层（screens/library/detailviewer.go）同一层、同一套次序：遮罩先画，岛握手之后原生视图盖住它。
		ui.Overlay(c.Key("viewer"), ui.OverlaySpec{Band: kit.BandViewer, Bounds: c.Bounds()}).
			Draw(func(d *ui.DrawContext) { d.Canvas.Fill(d.Rect, gfx.Color{Alpha: 0.88}) }).
			DrawOver(func(d *ui.DrawContext) { v.Frame(d.Canvas, d.Rect) })
	}
	if f := s.float; f != nil {
		ui.Overlay(c.Key("float"), ui.OverlaySpec{Band: f.band, Bounds: f.rect}).Draw(func(d *ui.DrawContext) {
			d.Canvas.Fill(d.Rect, f.col)
		})
	}
	s.host.EndFrame()
}

func (s *smoke) runApp(want map[string]bool) *report {
	rep := &report{StartedAt: time.Now().Format(time.RFC3339), Env: map[string]any{}}
	ready := make(chan struct{})
	var readyOnce sync.Once
	s.web.OnReady = func() { readyOnce.Do(func() { close(ready) }) }

	driverDone := make(chan struct{})
	go func() {
		defer close(driverDone)
		<-ready
		rep.Results = s.drive(want, rep.Env)
		s.onUI(func() {
			s.host.Close()
			s.app.Quit()
		})
	}()

	page := ui.NewPage(ui.PageConfig{View: s.view, Theme: &s.theme, Background: &s.bg, Invalidator: &s.inv})
	origin := gfx.Point{X: 60, Y: 40}
	err := vapp.RunApplication(func(a *vapp.Application) error {
		config := page.Config(vapp.Config{
			Title: "reader-smoke", Class: smokeClass, Width: contentW, Height: contentH, Origin: &origin,
			Web: s.web, Invalidator: &s.inv, OnWake: s.drain,
		})
		var canvas gfx.Canvas
		win, err := a.OpenWindow(config, &canvas)
		if err != nil {
			return err
		}
		s.app, s.win = a, win
		// 要从屏幕上截图：窗口临时置顶，免得被别的窗口盖住；跑完随进程一起结束。
		_ = win.SetAlwaysOnTop(true)
		return nil
	})
	if err != nil {
		rep.RunError = err.Error()
		// 窗口没起来时驱动 goroutine 还在等 ready：不会有 ready，直接返回。
		return rep
	}
	<-driverDone
	s.mu.Lock()
	rep.Events = append(rep.Events, s.log...)
	s.mu.Unlock()
	return rep
}

// ---- 页面事件 ----

type handler struct{ s *smoke }

func (h handler) Matches(count int) { h.s.record("matches", count) }
func (h handler) Scroll(id string, tab reader.Tab, top float64) {
	h.s.record("scroll", map[string]any{"item": id, "tab": string(tab), "top": top})
}
func (h handler) Catalog(open bool) { h.s.record("catalog", open) }
func (h handler) Toc(open bool)     { h.s.record("toc", open) }
func (h handler) Link(url string)   { h.s.record("link", url) }
func (h handler) Copy(text string) error {
	h.s.record("copy", text)
	return h.s.copyErr
}
func (h handler) Lightbox(images []reader.Image, index int) {
	first := ""
	if len(images) > 0 {
		first = images[0].Src
	}
	h.s.record("lightbox", map[string]any{"count": len(images), "index": index, "first": first})
}
func (h handler) Toast(text string) { h.s.record("toast", text) }
func (h handler) Key(key string, ctrl, alt, shift bool) {
	h.s.record("key", map[string]any{"key": key, "ctrl": ctrl, "alt": alt, "shift": shift})
}
func (h handler) FindReset()       { h.s.record("findReset", nil) }
func (h handler) Failed(err error) { h.s.record("failed", err.Error()) }

// viewerHandler 接收图片查看岛的结局。
type viewerHandler struct{ s *smoke }

func (h viewerHandler) Closed()          { h.s.record("viewer:closed", nil) }
func (h viewerHandler) Failed(err error) { h.s.record("viewer:failed", err.Error()) }

// ---- 开岛：与 reader.IslandOpener 同一套，多记一份页面的加载进度，并让探针脚本的消息不进宿主 ----

type smokeOpener struct{ s *smoke }

func (o smokeOpener) Open(cfg island.Config, closed func()) (reader.Page, error) {
	s := o.s
	hostMessage := cfg.OnMessage
	cfg.OnMessage = func(text string) {
		if strings.HasPrefix(text, `{"t":"smoke"`) {
			var msg struct {
				Name string          `json:"name"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal([]byte(text), &msg); err != nil {
				s.record("smoke:bad", err.Error())
				return
			}
			var data any
			_ = json.Unmarshal(msg.Data, &data)
			s.record("smoke:"+msg.Name, data)
			return
		}
		hostMessage(text)
	}
	cfg.OnLoad = func(l vapp.WebLoad) { s.record("load", fmt.Sprintf("%+v", l)) }
	isl, err := island.Open(s.web, cfg)
	if err != nil {
		return nil, err
	}
	s.isl = isl
	page := &smokePage{s: s, isl: isl, entry: cfg.Entry, closed: closed}
	s.group.Track(page)
	return page, nil
}

type smokePage struct {
	s      *smoke
	isl    *island.Island
	entry  string
	closed func()
	once   sync.Once
}

func (p *smokePage) Post(text string) { p.isl.View().PostMessage(text) }
func (p *smokePage) Focus() {
	p.s.record("focus", p.entry)
	p.isl.View().Focus()
}
func (p *smokePage) Place(surface reader.Surface, bounds gfx.Rect) {
	surface.NativeView(p.isl.View().ID(), bounds)
}
func (p *smokePage) Close() {
	p.once.Do(func() {
		p.s.group.Forget(p)
		p.isl.Close()
		if p.s.isl == p.isl {
			p.s.isl = nil
		}
		if p.closed != nil {
			p.closed()
		}
	})
}

// ---- 请求、探针、截图 ----

func (s *smoke) theme16(dark bool) reader.Theme {
	return reader.Theme{Dark: dark, Hue: s.spec.Hue, Saturation: s.spec.Saturation, BasePx: 16, Motion: reader.MotionStandard}
}

func (s *smoke) request(view reader.View, tab reader.Tab, dark bool) reader.Request {
	return reader.Request{ItemID: itemID, Tab: tab, View: view, Theme: s.theme16(dark), Lang: reader.LangZh}
}

// show 设置期望状态并让阅读区出一帧；bounds 为空铺满内容区。
func (s *smoke) show(req reader.Request, bounds *gfx.Rect) {
	s.onUI(func() { s.req, s.shown, s.bounds = req, true, bounds })
}

// hide 让阅读区不再出现（宿主在这一帧收尾时关岛），并等到岛真的关了。
func (s *smoke) hide() bool {
	s.onUI(func() { s.shown = false })
	return s.waitUI(5*time.Second, func() bool { return !s.host.Open() })
}

func (s *smoke) waitRendered(timeout time.Duration) bool {
	return s.waitUI(timeout, func() bool { return s.host.Rendered() })
}

// probe 在页面里跑一段脚本（body 里 return 一个可 JSON 化的值），结果经消息通道回来。
func (s *smoke) probe(name, body string, timeout time.Duration) (map[string]any, bool) {
	mark := s.mark()
	script := fmt.Sprintf(`window.viva.postMessage(JSON.stringify({t:"smoke",name:%q,data:(function(){try{%s}catch(e){return {error:String(e)}}})()}))`, name, body)
	var have bool
	s.onUI(func() {
		if s.isl != nil {
			s.isl.View().Eval(script)
			have = true
		}
	})
	if !have {
		return nil, false
	}
	e, ok := s.waitEvent(mark, "smoke:"+name, timeout, nil)
	if !ok {
		return nil, false
	}
	m, _ := e.Data.(map[string]any)
	return m, m != nil
}

func (s *smoke) capture() (*image.NRGBA, error) {
	if s.hwnd == 0 {
		return nil, errors.New("没有找到窗口")
	}
	x, y, w, h, ok := clientOnScreen(s.hwnd)
	if !ok {
		return nil, errors.New("取不到客户区位置")
	}
	sw, sh := screenSize()
	if x < 0 || y < 0 || x+w > sw || y+h > sh {
		return nil, fmt.Errorf("窗口客户区 (%d,%d %dx%d) 超出屏幕 %dx%d", x, y, w, h, sw, sh)
	}
	return captureScreen(x, y, w, h)
}

func (s *smoke) shot(name string) (*image.NRGBA, error) {
	img, err := s.capture()
	if err != nil {
		return nil, err
	}
	f, err := os.Create(filepath.Join(s.out, name+".png"))
	if err != nil {
		return img, err
	}
	defer f.Close()
	return img, png.Encode(f, img)
}

// at 取截图上逻辑坐标 (lx, ly) 处的像素。
func (s *smoke) at(img *image.NRGBA, lx, ly float64) color.NRGBA {
	b := img.Bounds()
	x := clampInt(int(math.Round(lx*s.scale)), 0, b.Dx()-1)
	y := clampInt(int(math.Round(ly*s.scale)), 0, b.Dy()-1)
	return img.NRGBAAt(b.Min.X+x, b.Min.Y+y)
}

func clampInt(v, lo, hi int) int { return max(lo, min(hi, v)) }

func luma(c color.NRGBA) float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

// near 判断两个颜色逐通道相差不超过 tol。
func near(a, b color.NRGBA, tol int) bool {
	d := func(x, y uint8) int { return int(math.Abs(float64(x) - float64(y))) }
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol
}

func rgb(c color.NRGBA) string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

func colorOf(c gfx.Color) color.NRGBA {
	return color.NRGBA{R: uint8(math.Round(float64(c.Red) * 255)), G: uint8(math.Round(float64(c.Green) * 255)), B: uint8(math.Round(float64(c.Blue) * 255)), A: 0xFF}
}
