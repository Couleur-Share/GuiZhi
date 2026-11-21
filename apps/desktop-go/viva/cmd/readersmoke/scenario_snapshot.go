//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"guizhi-mygo-shell/viva/reader"
)

// ---- snapshot：公众号网页快照（沙箱 iframe 的 srcdoc）在阅读岛里的 CSP 与本地图片（台账 A-018） ----
//
// 要在真 WebView2 里证明四件事：
//  1. 桥接脚本（文档里唯一被放行的内联脚本，岛的 CSP 只认它的 sha256）能跑：高度、阅读布局（目录）、查找结果都经消息回到页面与宿主；
//  2. 本地图片真的画出来：文档里的 local-image://…，宿主改写成岛内的 ./media/local-image/…，页面（可信的主框架）经媒体路径读出、内联成 data: 地址再交给 iframe。
//     不透明来源的沙箱 iframe 自己够不到岛的自有地址（实测，宿主的媒体回调不会被调用），所以只能由页面读图；
//  3. 文章里夹带的别的内联脚本被 CSP 拦下（既不改页面，也发不出消息）；
//  4. 文档里的外部图片没有被请求（文档自带的 <meta> 策略只许岛的 scheme，岛本身没有放开任何外部来源）。
//
// 页面在不透明来源的 iframe 里，主框架读不到它的 DOM，所以证据都是主框架看得见的：iframe 的高度、目录里的条目、宿主收到的事件、
// 截图上的像素，以及本机回环服务器上的请求计数。

const (
	snapInstance = "smoke-snap"
	// 文档里的几段正文：并发一词在可见文字里出现的次数就是查找的预期命中数。
	snapFiller = 1200
)

// snapshotSmokeBody 是文档 <body> 里的内容（不含桥接脚本）：
//   - 左上角 320×200 的本地图片（smoke-a.png，蓝色渐变）；
//   - 一个绝对定位的绿色方块，旁边的内联脚本若能运行会把它改成品红并向宿主发 link；
//   - 一张外部图片（回环服务器上的地址），不该被请求；
//   - 三个二级标题（目录的条目）与足够长的占位（高度要明显大于 iframe 的初始高度 400）。
func snapshotSmokeBody(extURL string) (body string, visibleWords int) {
	var b strings.Builder
	b.WriteString(`<img id="local" src="local-image://smoke-a.png" style="display:block;width:320px;height:200px">`)
	b.WriteString(`<div id="evil-box" style="position:absolute;left:16px;top:240px;width:120px;height:60px;background:#00AA00"></div>`)
	fmt.Fprintf(&b, `<img id="ext" src="%s" style="position:absolute;left:200px;top:240px;width:60px;height:60px">`, extURL)
	b.WriteString(`<h2 style="margin-top:80px">第一节</h2><p>并发是同时处理多件事。</p>`)
	b.WriteString(`<h2>第二节</h2><p>并发与并行不同，并发强调结构。</p>`)
	fmt.Fprintf(&b, `<div style="height:%dpx"></div>`, snapFiller)
	b.WriteString(`<h2>第三节</h2><p>末尾再说一次并发。</p>`)
	body = b.String()
	return body, strings.Count(body, findWord)
}

// snapshotSmokeDoc 拼出与引擎同构的快照文档（jsrc 的 snapshotDocument）：自带的 <meta> 策略只许桥接脚本的哈希，图片只许 local-image:，
// 之后是文章内容、一段夹带的内联脚本，最后才是桥接脚本。
func snapshotSmokeDoc(body string) string {
	sum := sha256.Sum256([]byte(snapshotBridgeJS))
	hash := base64.StdEncoding.EncodeToString(sum[:])
	csp := "default-src 'none'; script-src 'sha256-" + hash + "'; style-src 'unsafe-inline'; img-src local-image:; connect-src 'none'; " +
		"font-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"
	evil := `document.getElementById('evil-box').style.background='#FF00FF';` +
		`parent.postMessage({id:'` + snapInstance + `',type:'link',value:'https://evil.example/pwn'},'*');`
	return `<!doctype html><html data-instance="` + snapInstance + `"><head><meta charset="utf-8">` +
		`<meta http-equiv="Content-Security-Policy" content="` + csp + `"><meta name="referrer" content="no-referrer">` +
		`<style>html{margin:0;padding:0;background:white;color:#222}body{margin:0;padding:16px;box-sizing:border-box;font:16px/1.75 sans-serif}</style>` +
		`</head><body>` + body + `<script>` + evil + `</script><script>` + snapshotBridgeJS + `</script></body></html>`
}

// frameMetrics 是主框架里量到的 iframe 与目录：外框、高度、目录里的条目文字。
const frameMetricsScript = `const f=document.querySelector('iframe[data-article-instance]');if(!f)return {none:true};` +
	`const r=f.getBoundingClientRect();const nav=document.querySelector('[data-testid="snapshot-toc"] nav');` +
	`return {none:false,left:r.left,top:r.top,width:r.width,height:r.height,sandbox:f.getAttribute('sandbox'),` +
	`toc:nav?[...nav.querySelectorAll('button')].map(b=>b.textContent.trim()):null}`

func scSnapshot(s *smoke, r *result) {
	sum := sha256.Sum256([]byte(snapshotBridgeJS))
	got := base64.StdEncoding.EncodeToString(sum[:])
	r.check("夹具里的桥接脚本与 reader.SnapshotBridgeSHA256 一致（岛放行的就是它）", got == reader.SnapshotBridgeSHA256, map[string]string{"夹具": got, "常量": reader.SnapshotBridgeSHA256})

	// 回环服务器：外部图片若被请求就会在这里记一笔。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		r.check("起回环服务器", false, err.Error())
		return
	}
	var hits atomic.Int64
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusNotFound)
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	extURL := "http://" + ln.Addr().String() + "/ext.png"

	body, words := snapshotSmokeBody(extURL)
	view := reader.View{
		Kind: reader.ViewSnapshot, SnapshotDoc: snapshotSmokeDoc(body), SnapshotInstanceID: snapInstance,
		SnapshotWidth: reader.SnapshotWidthWide, SnapshotAssets: []string{"local-image://smoke-a.png"},
	}
	req := s.request(view, reader.TabBody, false)
	req.TocOpen = true
	req.FindQuery, req.FindIndex = findWord, 0
	mark := s.mark()
	took, ok := s.fresh(req)
	r.set("renderMs", ms(took))
	r.check("快照页渲染完成（iframe 加载并上报 rendered）", ok, ms(took))
	if !ok {
		return
	}

	// 1. 桥接脚本：高度回来了（iframe 不再是初始的 400）、目录有三个标题、查找命中数等于可见文字里的次数。
	var m map[string]any
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		m, _ = s.probe("snap-frame", frameMetricsScript, 5*time.Second)
		if h, _ := m["height"].(float64); h > 1000 {
			if toc, _ := m["toc"].([]any); len(toc) >= 4 {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	r.set("frame", m)
	r.check("iframe 是沙箱且不带 allow-same-origin", m["sandbox"] == "allow-scripts", m["sandbox"])
	height, _ := m["height"].(float64)
	r.check("height 消息回来了：iframe 高度随文档增长（初始值 400）", height > float64(snapFiller-200), height)
	toc, _ := m["toc"].([]any)
	tocText := fmt.Sprint(toc)
	r.check("reader-layout 消息回来了：目录列出三个二级标题", strings.Contains(tocText, "第一节") && strings.Contains(tocText, "第二节") && strings.Contains(tocText, "第三节"), toc)

	e, ok := s.waitEvent(mark, "matches", 8*time.Second, nil)
	r.check("find-result 回来了：宿主收到命中数", ok, nil)
	if ok {
		count, _ := e.Data.(int)
		r.set("matches", count)
		r.check("命中数等于可见文字里「并发」的次数", count == words, map[string]any{"页面": count, "预期": words})
	}
	states := s.eventsSince(mark, "snapshotState")
	r.check("页面上报了版面状态（snapshotState）", len(states) > 0, states)
	var lastState map[string]any
	if len(states) > 0 {
		lastState, _ = states[len(states)-1].Data.(map[string]any)
	}
	r.check("目录浮层按请求展开（宿主期望的 tocOpen 原样回报）", lastState["tocOpen"] == true, lastState)

	// 2 与 3. 截图上的像素：本地图片画出来了，夹带的脚本没有把绿方块改成品红。
	settle()
	img, err := s.shot("snapshot-0-open")
	if err != nil {
		r.check("截图", false, err.Error())
		return
	}
	left, _ := m["left"].(float64)
	top, _ := m["top"].(float64)
	pic := s.at(img, left+16+160, top+16+100)
	r.check("本地图片加载并画出（页面读图后内联为 data: 地址，蓝色渐变）", int(pic.B)-int(pic.R) > 60, rgb(pic))
	medias := s.eventsSince(mark, "media")
	r.check("本地图片是页面经岛的媒体路径向宿主取的（宿主收到 local-image/smoke-a.png）", len(medias) > 0 && fmt.Sprint(medias[0].Data) == "local-image/smoke-a.png", medias)
	box := s.at(img, left+16+60, top+240+30)
	r.check("夹带的内联脚本被 CSP 拦下：绿方块没有变品红", int(box.G) > int(box.R)+60 && int(box.G) > int(box.B)+60, rgb(box))
	r.check("夹带的脚本没能经 postMessage 向宿主发 link", len(s.eventsSince(mark, "link")) == 0, s.eventsSince(mark, "link"))

	// 4. 外部图片没有被请求。
	time.Sleep(600 * time.Millisecond)
	r.check("外部图片没有被请求（回环服务器零命中）", hits.Load() == 0, hits.Load())
	r.check("全程没有页面或宿主的失败回报", len(s.eventsSince(mark, "failed")) == 0 && len(s.eventsSince(mark, "diag")) == 0,
		map[string]any{"failed": s.eventsSince(mark, "failed"), "diag": s.eventsSince(mark, "diag")})
	// 对照：同一份文档把 <meta> 的图片来源放宽到 http:，外部图片就会被请求——证明上面的零命中是文档策略挡的，不是计数器失灵。
	loose := view
	loose.SnapshotDoc = strings.Replace(view.SnapshotDoc, "img-src local-image:;", "img-src local-image: http:;", 1)
	if _, ok := s.fresh(s.request(loose, reader.TabBody, false)); ok {
		time.Sleep(900 * time.Millisecond)
		r.check("对照：放宽文档策略后外部图片会被请求（计数器有效）", hits.Load() > 0, hits.Load())
	} else {
		r.check("对照文档渲染", false, nil)
	}
}