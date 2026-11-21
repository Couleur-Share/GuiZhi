//go:build windows

package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/reader"
)

type scenario struct {
	name string
	run  func(*smoke, *result)
}

// drive 依次跑各场景（在驱动 goroutine 上）；单个场景出错（含 panic）不拖垮其余。
func (s *smoke) drive(want map[string]bool, env map[string]any) []*result {
	s.hwnd = findWindow()
	s.scale = windowScale(s.hwnd)
	env["goos"], env["goarch"], env["go"] = runtime.GOOS, runtime.GOARCH, runtime.Version()
	env["scale"] = s.scale
	env["hwndFound"] = s.hwnd != 0
	if s.hwnd != 0 {
		x, y, w, h, ok := clientOnScreen(s.hwnd)
		sw, sh := screenSize()
		env["client"] = map[string]any{"x": x, "y": y, "w": w, "h": h, "ok": ok}
		env["screen"] = map[string]any{"w": sw, "h": sh}
	}
	env["browserArgs"] = os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")

	all := []scenario{
		{"diag", scDiag},
		{"first-paint", scFirstPaint},
		{"render", scRender},
		{"theme", scTheme},
		{"find", scFind},
		{"bounds", scBounds},
		{"overlay", scOverlay},
		{"events", scEvents},
		{"restore", scRestore},
		{"reopen", scReopen},
		{"gallery", scGallery},
		{"replies", scReplies},
		{"viewer", scViewer},
	}
	var results []*result
	for _, sc := range all {
		if len(want) > 0 && !want[sc.name] {
			continue
		}
		r := &result{Name: sc.name}
		start := time.Now()
		func() {
			defer func() {
				if p := recover(); p != nil {
					r.Error = fmt.Sprint("panic：", p)
				}
			}()
			sc.run(s, r)
		}()
		r.TookMs = ms(time.Since(start))
		fmt.Fprintf(os.Stderr, "== %s 用时 %.0f ms，检查 %d 项\n", sc.name, r.TookMs, len(r.Checks))
		results = append(results, r)
	}
	return results
}

func originalView() reader.View {
	return reader.View{Kind: reader.ViewOriginal, Content: markdownFixture(), SourceKind: "body"}
}

// fresh 关掉岛、按 req 重新开一个，等页面渲染出来；返回是否渲染成功与用时。
func (s *smoke) fresh(req reader.Request) (time.Duration, bool) {
	if !s.hide() {
		return 0, false
	}
	start := time.Now()
	s.show(req, nil)
	ok := s.waitRendered(12 * time.Second)
	return time.Since(start), ok
}

// settle 等几帧：窗口线程出帧、DWM 合成、原生视图的裁剪都要时间。
func settle() { time.Sleep(400 * time.Millisecond) }

// ---- diag：页面起不来时看它的状态（只记录，不判定） ----

func scDiag(s *smoke, r *result) {
	if !s.hide() {
		r.check("开始前岛是关的", false, nil)
		return
	}
	s.show(s.request(originalView(), reader.TabBody, true), nil)
	time.Sleep(4 * time.Second)
	var ready, rendered, open bool
	s.onUI(func() { ready, rendered, open = s.host.Ready(), s.host.Rendered(), s.host.Open() })
	r.set("host", map[string]bool{"open": open, "ready": ready, "rendered": rendered})
	dom, ok := s.probe("diag", `
		const v = window.viva;
		return {
		  readyState: document.readyState, href: location.href,
		  viva: typeof v, post: typeof (v && v.postMessage), onmessage: typeof (v && v.onmessage),
		  scripts: [...document.scripts].map(x => x.src || "inline"),
		  rootLen: (document.getElementById("root") || {innerHTML: ""}).innerHTML.length,
		  htmlHead: document.documentElement.outerHTML.slice(0, 600),
		  res: performance.getEntriesByType("resource").map(e => e.name + ":" + Math.round(e.decodedBodySize)),
		};`, 5*time.Second)
	r.set("page", dom)
	r.check("探针有回报（页面主框架里的脚本能跑、桥能用）", ok, nil)

	// 在页面里装一个记录器，再让宿主重发一次内容，看页面到底收到了什么、有没有报错。
	if _, ok := s.probe("hook", `
		window.__msgs = []; window.__errs = [];
		const orig = window.viva.onmessage;
		window.viva.onmessage = (text) => { window.__msgs.push(String(text).slice(0, 160)); if (orig) orig(text); };
		window.addEventListener("error", (e) => window.__errs.push(String(e.message)));
		window.addEventListener("unhandledrejection", (e) => window.__errs.push("rej:" + String(e.reason)));
		return {hooked: typeof orig};`, 5*time.Second); !ok {
		r.check("装记录器", false, nil)
		return
	}
	view := originalView()
	view.Content += "\n\n结尾。"
	s.show(s.request(view, reader.TabBody, true), nil)
	time.Sleep(2 * time.Second)
	after, _ := s.probe("after", `return {msgs: window.__msgs, errs: window.__errs, rootLen: document.getElementById("root").innerHTML.length, rootHead: document.getElementById("root").innerHTML.slice(0, 200)}`, 5*time.Second)
	r.set("afterResend", after)
	s.onUI(func() { rendered = s.host.Rendered() })
	r.set("renderedAfterResend", rendered)
}

// ---- first-paint：岛刚打开、页面还没画出来时，岛的区域是什么颜色（深色主题下出现白底就是闪白） ----

type sample struct {
	AtMs     float64 `json:"atMs"`
	Luma     float64 `json:"luma"`
	Rendered bool    `json:"rendered"`
}

func scFirstPaint(s *smoke, r *result) {
	if !s.hide() {
		r.check("开始前岛是关的", false, nil)
		return
	}
	mark := s.mark()
	t0 := time.Now()
	s.show(s.request(originalView(), reader.TabBody, true), nil)
	var series []sample
	extra := 0
	rendered := false
	for time.Since(t0) < 12*time.Second && extra < 3 {
		l := -1.0
		if img, err := s.capture(); err == nil {
			l = (luma(s.at(img, 6, 6)) + luma(s.at(img, 6, contentH-6)) + luma(s.at(img, 6, contentH/2))) / 3
		}
		var done bool
		s.onUI(func() { done = s.host.Rendered() })
		series = append(series, sample{AtMs: ms(time.Since(t0)), Luma: math.Round(l*1000) / 1000, Rendered: done})
		if done {
			rendered = true
			extra++
		}
	}
	r.check("页面在 12 秒内渲染出来", rendered, nil)
	var white, firstWhite, lastWhite float64 = 0, -1, -1
	for _, sm := range series {
		if sm.Luma > 0.85 && !sm.Rendered {
			white++
			if firstWhite < 0 {
				firstWhite = sm.AtMs
			}
			lastWhite = sm.AtMs
		}
	}
	r.set("samples", series)
	r.set("whiteFramesBeforeRendered", white)
	r.set("firstWhiteMs", firstWhite)
	r.set("lastWhiteMs", lastWhite)
	r.set("loads", s.eventsSince(mark, "load"))
	r.check("深色主题下渲染完成之前没有白底的帧", white == 0, map[string]any{"白帧数": white, "起止毫秒": []float64{firstWhite, lastWhite}})
	settle()
	if img, err := s.shot("first-paint-dark"); err != nil {
		r.check("截图", false, err.Error())
	} else {
		bg := s.at(img, 6, 6)
		r.check("渲染完成后岛的底色是深色", luma(bg) < 0.3, rgb(bg))
	}
}

// ---- render：页面的 DOM 与样式 ----

func scRender(s *smoke, r *result) {
	took, ok := s.fresh(s.request(originalView(), reader.TabBody, true))
	r.set("openToRenderedMs", ms(took))
	if !ok {
		r.check("渲染", false, nil)
		return
	}
	settle()
	dom, ok := s.probe("dom", `
		const q = (sel) => document.querySelectorAll(sel).length;
		const imgs = [...document.images];
		return {
		  headings: q('h1,h2,h3'), tables: q('table'), pre: q('pre'), links: q('a[href]'), items: q('li'),
		  imgs: imgs.map(i => ({src: i.getAttribute('src'), loaded: i.complete && i.naturalWidth > 0, w: i.naturalWidth})),
		  dark: document.documentElement.classList.contains('dark'),
		  lang: document.documentElement.lang,
		  bodyBg: getComputedStyle(document.body).backgroundColor,
		  rootFont: getComputedStyle(document.documentElement).fontSize,
		  dpr: devicePixelRatio, iw: innerWidth, ih: innerHeight,
		  head: document.body.innerText.slice(0, 40),
		};`, 5*time.Second)
	if !ok {
		r.check("探针回报", false, nil)
		return
	}
	r.set("dom", dom)
	num := func(k string) float64 { v, _ := dom[k].(float64); return v }
	r.check("标题不少于 80 个", num("headings") >= 80, num("headings"))
	r.check("表格 1 个", num("tables") == 1, num("tables"))
	r.check("代码块至少 1 个", num("pre") >= 1, num("pre"))
	r.check("外链至少 1 个", num("links") >= 1, num("links"))
	loaded, broken := 0, 0
	for _, v := range dom["imgs"].([]any) {
		m := v.(map[string]any)
		src, _ := m["src"].(string)
		isLoaded, _ := m["loaded"].(bool)
		switch {
		case strings.Contains(src, "smoke-a.png") && isLoaded:
			loaded++
		case strings.Contains(src, "missing.png") && !isLoaded:
			broken++
		}
	}
	r.check("本地图经宿主的媒体回调加载成功", loaded == 1, dom["imgs"])
	r.check("缺失的本地图是裂图而不是别的图", broken == 1, dom["imgs"])
	r.check("页面是深色", dom["dark"] == true, dom["dark"])
	r.check("根字号 16px", dom["rootFont"] == "16px", dom["rootFont"])
	r.check("页面像素比与窗口缩放一致", math.Abs(num("dpr")-s.scale) < 0.01, map[string]any{"页面": num("dpr"), "窗口": s.scale})
	r.check("页面宽度等于阅读区宽度", math.Abs(num("iw")-contentW) <= 1, map[string]any{"页面": num("iw"), "阅读区": contentW})
	r.check("页面高度等于阅读区高度", math.Abs(num("ih")-contentH) <= 1, map[string]any{"页面": num("ih"), "阅读区": contentH})
	if _, err := s.shot("render-dark"); err != nil {
		r.check("截图", false, err.Error())
	}
}

// ---- theme：运行中切换主题，页面跟着变，不重开岛 ----

func scTheme(s *smoke, r *result) {
	if _, ok := s.fresh(s.request(originalView(), reader.TabBody, true)); !ok {
		r.check("渲染", false, nil)
		return
	}
	start := time.Now()
	s.show(s.request(originalView(), reader.TabBody, false), nil)
	switched := false
	for time.Since(start) < 4*time.Second {
		if dom, ok := s.probe("themeCheck", `return {dark: document.documentElement.classList.contains('dark')}`, 2*time.Second); ok && dom["dark"] == false {
			switched = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	r.set("switchMs", ms(time.Since(start)))
	r.check("切到浅色后页面不再是深色", switched, nil)
	settle()
	if img, err := s.shot("theme-light"); err != nil {
		r.check("截图", false, err.Error())
	} else {
		bg := s.at(img, 6, 6)
		r.check("浅色主题的岛底色是浅色", luma(bg) > 0.85, rgb(bg))
	}
	var open bool
	s.onUI(func() { open = s.host.Open() })
	r.check("切换主题没有重开岛（岛一直开着）", open, nil)
}

// ---- find：页内查找的命中数与高亮 ----

func scFind(s *smoke, r *result) {
	req := s.request(originalView(), reader.TabBody, true)
	if _, ok := s.fresh(req); !ok {
		r.check("渲染", false, nil)
		return
	}
	mark := s.mark()
	req.FindQuery, req.FindIndex = findWord, 0
	s.show(req, nil)
	e, ok := s.waitEvent(mark, "matches", 5*time.Second, nil)
	r.check("收到命中数", ok, nil)
	if ok {
		count, _ := e.Data.(int)
		want := strings.Count(markdownFixture(), findWord)
		r.set("matches", count)
		r.set("expectedByTextCount", want)
		r.check("命中数大于 0", count > 0, count)
		r.check("命中数等于正文里词出现的次数", count == want, map[string]any{"页面": count, "正文": want})
	}
	req.FindIndex = 3
	s.show(req, nil)
	settle()
	if dom, ok := s.probe("marks", `return {marks: document.querySelectorAll('mark').length}`, 3*time.Second); ok {
		r.set("marks", dom["marks"])
		n, _ := dom["marks"].(float64)
		r.check("页面里有高亮元素", n > 0, dom["marks"])
	} else {
		r.check("探针回报", false, nil)
	}
	if _, err := s.shot("find"); err != nil {
		r.check("截图", false, err.Error())
	}
}

// ---- bounds：阅读区的位置与大小（逻辑像素）到页面里是否准确 ----

func scBounds(s *smoke, r *result) {
	req := s.request(originalView(), reader.TabBody, true)
	if _, ok := s.fresh(req); !ok {
		r.check("渲染", false, nil)
		return
	}
	bounds := gfx.Rect{X: 100, Y: 60, Width: 500, Height: 400}
	s.show(req, &bounds)
	var last map[string]any
	good := false
	for start := time.Now(); time.Since(start) < 4*time.Second; time.Sleep(40 * time.Millisecond) {
		if dom, ok := s.probe("size", `return {iw: innerWidth, ih: innerHeight, dpr: devicePixelRatio}`, 2*time.Second); ok {
			last = dom
			iw, _ := dom["iw"].(float64)
			ih, _ := dom["ih"].(float64)
			if math.Abs(iw-500) <= 1 && math.Abs(ih-400) <= 1 {
				good = true
				break
			}
		}
	}
	r.set("size", last)
	r.check("页面尺寸跟随阅读区的位置与大小", good, last)
	settle()
	if img, err := s.shot("bounds"); err != nil {
		r.check("截图", false, err.Error())
	} else {
		outside, inside := s.at(img, 50, 300), s.at(img, 400, 300)
		r.set("outsidePixel", rgb(outside))
		r.set("insidePixel", rgb(inside))
		r.check("阅读区之外是窗口自己的底色", near(outside, colorOf(s.bg), 4), rgb(outside))
	}
}

// ---- overlay：更高层的原生控件与岛的上下关系，画面与命中 ----

func scOverlay(s *smoke, r *result) {
	if _, ok := s.fresh(s.request(originalView(), reader.TabBody, true)); !ok {
		r.check("渲染", false, nil)
		return
	}
	settle()
	base, err := s.shot("overlay-0-base")
	if err != nil {
		r.check("基线截图", false, err.Error())
		return
	}
	magenta := gfx.Color{Red: 1, Green: 0, Blue: 1, Alpha: 1}
	m8 := colorOf(magenta)
	rect := gfx.Rect{X: 560, Y: 40, Width: 300, Height: 50}
	cx, cy := float64(rect.X+rect.Width/2), float64(rect.Y+rect.Height/2)
	x0, y0, _, _, _ := clientOnScreen(s.hwnd)
	atPhys := func(img *image.NRGBA, px, py int) color.NRGBA {
		b := img.Bounds()
		return img.NRGBAAt(b.Min.X+clampInt(px, 0, b.Dx()-1), b.Min.Y+clampInt(py, 0, b.Dy()-1))
	}

	// 1. 页面层之上的 Overlay（知识库里查找条、退出专注按钮用的 kit.BandOverIsland）。
	s.onUI(func() { s.float = &floatSpec{band: kit.BandOverIsland, rect: rect, col: magenta} })
	settle()
	img, err := s.shot("overlay-1-band1")
	if err != nil {
		r.check("band 1 截图", false, err.Error())
		return
	}
	inside := s.at(img, cx, cy)
	r.check("更高一层的色块画在岛之上", near(inside, m8, 6), rgb(inside))
	away := s.at(img, 300, 300)
	r.check("色块之外岛照常显示", near(away, s.at(base, 300, 300), 8), map[string]string{"此刻": rgb(away), "基线": rgb(s.at(base, 300, 300))})
	px0, py := int(math.Round(float64(rect.X)*s.scale)), int(math.Round(cy*float64(s.scale)))
	edgeIn, edgeOut := atPhys(img, px0+2, py), atPhys(img, px0-2, py)
	r.set("edge", map[string]string{"inside+2px": rgb(edgeIn), "outside-2px": rgb(edgeOut)})
	r.check("色块左缘：内侧 2px 是色块色", near(edgeIn, m8, 6), rgb(edgeIn))
	r.check("色块左缘：外侧 2px 不是色块色", !near(edgeOut, m8, 40), rgb(edgeOut))

	leafIn, ownedIn := windowAt(s.hwnd, x0+int(math.Round(cx*s.scale)), y0+int(math.Round(cy*s.scale)))
	leafOut, ownedOut := windowAt(s.hwnd, x0+int(math.Round(300*s.scale)), y0+int(math.Round(300*s.scale)))
	r.set("hitInside", map[string]any{"class": leafIn, "ownedByWindow": ownedIn})
	r.set("hitOutside", map[string]any{"class": leafOut, "ownedByWindow": ownedOut})
	r.check("色块区域的命中归窗口（指针不会落到岛里）", ownedIn && leafIn == vivaWindowClass, map[string]any{"类名": leafIn, "属于本窗口": ownedIn})
	r.check("色块之外的命中归岛", ownedOut && leafOut != vivaWindowClass && leafOut != "", map[string]any{"类名": leafOut, "属于本窗口": ownedOut})

	// 2. 同一层（页面层）里在岛之后画：岛在上，色块被盖住（这就是为什么知识库的查找条要抬到更高一层）。
	s.onUI(func() { s.float = nil; s.tree = &floatSpec{rect: rect, col: magenta} })
	settle()
	if img2, err := s.shot("overlay-2-same-layer"); err != nil {
		r.check("同层截图", false, err.Error())
	} else {
		p := s.at(img2, cx, cy)
		r.check("同一层里在岛之后画的色块被岛盖住", !near(p, m8, 40), rgb(p))
	}

	// 3. 半透明的色块：底下透出来的是岛的内容，还是窗口自己的底色（挖洞）。
	s.onUI(func() { s.tree = nil })
	half := gfx.Color{Red: 1, Green: 0, Blue: 1, Alpha: 0.5}
	regionX, regionY, regionW, regionH := 30.0, 25.0, 280.0, 50.0
	s.onUI(func() {
		s.float = &floatSpec{band: kit.BandOverIsland, rect: gfx.Rect{X: 20, Y: 20, Width: 300, Height: 60}, col: half}
	})
	settle()
	if img3, err := s.shot("overlay-3-translucent"); err != nil {
		r.check("半透明截图", false, err.Error())
	} else {
		win := colorOf(s.bg)
		var mseBlend, mseHole, contrast float64
		n := 0
		for ly := regionY; ly < regionY+regionH; ly += 2 {
			for lx := regionX; lx < regionX+regionW; lx += 2 {
				b, f := s.at(base, lx, ly), s.at(img3, lx, ly)
				mseBlend += dist2(f, mix(b, m8, 0.5))
				mseHole += dist2(f, mix(win, m8, 0.5))
				contrast += math.Sqrt(dist2(b, win))
				n++
			}
		}
		fn := float64(n)
		verdict := "inconclusive"
		switch {
		case contrast/fn < 8:
			verdict = "inconclusive（区域里岛的内容与窗口底色太接近）"
		case mseBlend < mseHole*0.5:
			verdict = "blend（半透明色块下透出岛的内容）"
		case mseHole < mseBlend*0.5:
			verdict = "hole（半透明色块下是窗口底色，岛的内容被挖掉）"
		}
		r.set("translucent", map[string]any{"mseBlend": mseBlend / fn, "mseHole": mseHole / fn, "contrast": contrast / fn, "verdict": verdict})
	}

	// 4. 撤掉色块，岛原样露出来。
	s.onUI(func() { s.float = nil })
	settle()
	if img4, err := s.shot("overlay-4-removed"); err != nil {
		r.check("撤掉后截图", false, err.Error())
	} else {
		p := s.at(img4, cx, cy)
		r.check("撤掉色块后岛原样露出", near(p, s.at(base, cx, cy), 8), map[string]string{"此刻": rgb(p), "基线": rgb(s.at(base, cx, cy))})
	}
}

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	f := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-t) + float64(y)*t)) }
	return color.NRGBA{R: f(a.R, b.R), G: f(a.G, b.G), B: f(a.B, b.B), A: 0xFF}
}

func dist2(a, b color.NRGBA) float64 {
	dr, dg, db := float64(a.R)-float64(b.R), float64(a.G)-float64(b.G), float64(a.B)-float64(b.B)
	return dr*dr + dg*dg + db*db
}

// ---- events：页面里发生的事经真消息通道到宿主（脚本合成的 DOM 事件，不是真实输入） ----

func scEvents(s *smoke, r *result) {
	if _, ok := s.fresh(s.request(originalView(), reader.TabBody, true)); !ok {
		r.check("渲染", false, nil)
		return
	}
	settle()
	fire := func(js, kind string, pred func(any) bool) (event, bool) {
		mark := s.mark()
		s.onUI(func() {
			if s.isl != nil {
				s.isl.View().Eval(js)
			}
		})
		return s.waitEvent(mark, kind, 3*time.Second, pred)
	}
	if e, ok := fire(`document.querySelector('a[href^="https://"]').click()`, "link", nil); ok {
		r.check("点外链：宿主收到同一个地址", e.Data == linkURL, e.Data)
	} else {
		r.check("点外链：宿主收到 link", false, nil)
	}
	if e, ok := fire(`navigator.clipboard.writeText("冒烟复制")`, "copy", nil); ok {
		r.check("复制：宿主收到同一段文字", e.Data == "冒烟复制", e.Data)
	} else {
		r.check("复制：宿主收到 copy", false, nil)
	}
	// 复制回执（台账 B-069）：宿主写成功，页面的 writeText 才 resolve；宿主写失败，Promise 拒绝（代码块的「已复制」勾不出现）。
	copyOutcome := func(label string) (string, bool) {
		mark := s.mark()
		s.onUI(func() {
			if s.isl != nil {
				s.isl.View().Eval(`navigator.clipboard.writeText("` + label + `").then(` +
					`()=>window.viva.postMessage(JSON.stringify({t:"smoke",name:"copy-outcome",data:"resolved"})),` +
					`()=>window.viva.postMessage(JSON.stringify({t:"smoke",name:"copy-outcome",data:"rejected"})))`)
			}
		})
		e, ok := s.waitEvent(mark, "smoke:copy-outcome", 3*time.Second, nil)
		got, _ := e.Data.(string)
		return got, ok
	}
	if got, ok := copyOutcome("receipt-ok"); ok {
		r.check("复制回执：宿主写成功 → 页面的 writeText resolve", got == "resolved", got)
	} else {
		r.check("复制回执：页面收到回执（成功）", false, nil)
	}
	s.onUI(func() { s.copyErr = errors.New("冒烟：剪贴板写入失败") })
	if got, ok := copyOutcome("receipt-fail"); ok {
		r.check("复制回执：宿主写失败 → 页面的 writeText reject", got == "rejected", got)
	} else {
		r.check("复制回执：页面收到回执（失败）", false, nil)
	}
	s.onUI(func() { s.copyErr = nil })
	if e, ok := fire(`document.querySelector('img[src*="smoke-a"]').click()`, "lightbox", nil); ok {
		m, _ := e.Data.(map[string]any)
		first, _ := m["first"].(string)
		r.check("点图片：宿主收到查看器请求", strings.HasPrefix(first, "local-image://"), e.Data)
	} else {
		r.check("点图片：宿主收到 lightbox", false, nil)
	}
	if e, ok := fire(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'f',ctrlKey:true,bubbles:true,cancelable:true}))`, "key", nil); ok {
		m, _ := e.Data.(map[string]any)
		r.check("Ctrl+F：宿主收到转交的快捷键", m["key"] == "f" && m["ctrl"] == true, e.Data)
	} else {
		r.check("Ctrl+F：宿主收到 key", false, nil)
	}
	if _, ok := fire(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}))`, "key", nil); ok {
		r.check("Esc 不转交", false, nil)
	} else {
		r.check("Esc 不转交（台账里记为岛内按 Esc 退不出专注）", true, nil)
	}
	if diags := s.eventsSince(0, "diag"); len(diags) > 0 {
		r.set("diag", diags)
		r.check("宿主没有记诊断（没有不合规的页面事件）", false, diags)
	} else {
		r.check("宿主没有记诊断（没有不合规的页面事件）", true, nil)
	}
}

// ---- restore：阅读位置——打开时恢复、之后滚动的上报 ----

func scRestore(s *smoke, r *result) {
	top := 400.0
	req := s.request(originalView(), reader.TabBody, true)
	req.RestoreTop = &top
	if !s.hide() {
		r.check("岛先关掉", false, nil)
		return
	}
	mark := s.mark()
	s.show(req, nil)
	if !s.waitRendered(12 * time.Second) {
		r.check("渲染", false, nil)
		return
	}
	e, ok := s.waitEvent(mark, "scroll", 5*time.Second, nil)
	r.check("打开后页面上报了恢复的位置", ok, nil)
	if ok {
		m, _ := e.Data.(map[string]any)
		got, _ := m["top"].(float64)
		r.set("restoredTop", got)
		r.check("恢复到 400（±2）", math.Abs(got-400) <= 2, got)
	}
	// 之后滚动：把页面里主滚动元素滚到 900，应在防抖之后上报。
	mark = s.mark()
	s.onUI(func() {
		if s.isl != nil {
			s.isl.View().Eval(`(function(){const el=[...document.querySelectorAll('*')].find(e=>e.scrollHeight>e.clientHeight+50&&['auto','scroll'].includes(getComputedStyle(e).overflowY));if(el)el.scrollTop=900;})()`)
		}
	})
	e, ok = s.waitEvent(mark, "scroll", 3*time.Second, nil)
	r.check("滚动之后页面上报了新位置", ok, nil)
	if ok {
		m, _ := e.Data.(map[string]any)
		got, _ := m["top"].(float64)
		r.set("scrolledTop", got)
		r.check("新位置约 900（±2）", math.Abs(got-900) <= 2, got)
	}
}

// ---- reopen：关了再开、统一销毁后自己重开 ----

func scReopen(s *smoke, r *result) {
	req := s.request(originalView(), reader.TabBody, true)
	if _, ok := s.fresh(req); !ok {
		r.check("渲染", false, nil)
		return
	}
	start := time.Now()
	r.check("关岛", s.hide(), nil)
	var groupLen int
	s.onUI(func() { groupLen = s.group.Len() })
	r.check("关岛后 Group 里没有登记的岛", groupLen == 0, groupLen)
	s.show(req, nil)
	ok := s.waitRendered(12 * time.Second)
	r.set("closeAndReopenMs", ms(time.Since(start)))
	r.check("重开后重新渲染", ok, nil)

	// 收托盘前的统一销毁（Root.closeIslands → Group.CloseAll）：阅读区还要显示时，下一帧自己重开。
	var closed int
	s.onUI(func() { closed = s.group.CloseAll() })
	r.check("CloseAll 销毁了 1 个岛", closed == 1, closed)
	start = time.Now()
	ok = s.waitUI(2*time.Second, func() bool { return !s.host.Open() || s.host.Rendered() })
	r.check("销毁后宿主知道岛没了", ok, nil)
	ok = s.waitRendered(12 * time.Second)
	r.set("reopenAfterCloseAllMs", ms(time.Since(start)))
	r.check("统一销毁之后下一帧自己重开并渲染", ok, nil)
	s.onUI(func() { groupLen = s.group.Len() })
	r.check("重开后 Group 里有 1 个岛", groupLen == 1, groupLen)
}

// ---- gallery 与 replies ----

func scGallery(s *smoke, r *result) {
	view := reader.View{Kind: reader.ViewGallery, Content: galleryFixture}
	took, ok := s.fresh(s.request(view, reader.TabImages, true))
	r.set("openToRenderedMs", ms(took))
	if !ok {
		r.check("渲染", false, nil)
		return
	}
	settle()
	dom, ok := s.probe("gallery", `const imgs=[...document.images];return {n: imgs.length, loaded: imgs.filter(i=>i.complete&&i.naturalWidth>0).length}`, 5*time.Second)
	if !ok {
		r.check("探针回报", false, nil)
		return
	}
	r.set("dom", dom)
	r.check("画廊里 2 张图都加载成功", dom["n"] == 2.0 && dom["loaded"] == 2.0, dom)
	if _, err := s.shot("gallery"); err != nil {
		r.check("截图", false, err.Error())
	}
}

func scReplies(s *smoke, r *result) {
	view := reader.View{Kind: reader.ViewReplies, Content: forumFixture}
	took, ok := s.fresh(s.request(view, reader.TabReplies, true))
	r.set("openToRenderedMs", ms(took))
	if !ok {
		r.check("渲染", false, nil)
		return
	}
	settle()
	dom, ok := s.probe("replies", `const t=document.body.innerText;return {has1: t.includes('1 楼'), hasZhang: t.includes('张三'), hasLi: t.includes('李四'), len: t.length}`, 5*time.Second)
	if !ok {
		r.check("探针回报", false, nil)
		return
	}
	r.set("dom", dom)
	r.check("楼层卡片里有 1 楼、张三、李四", dom["has1"] == true && dom["hasZhang"] == true && dom["hasLi"] == true, dom)
	if _, err := s.shot("replies"); err != nil {
		r.check("截图", false, err.Error())
	}
}
