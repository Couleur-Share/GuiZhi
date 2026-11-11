// 阶段 0 验证原型：MyGo 能否承接归知的采集链路。
// 逐项验证：持久会话与 cookie、隐藏窗口渲染、页面内 JS 取值（Eval）、
// 网络活动观测（PerformanceObserver 兜底 webRequest）、截图。
// 运行：go run .   （第二次运行应能读到上一次写入的持久 cookie）
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/egoist/mygo"
)

// 注入页面最早执行：用 PerformanceObserver 收集资源请求，替代 Electron 的 webRequest。
const preload = `
window.__net = [];
try {
  new PerformanceObserver(function (l) {
    l.getEntries().forEach(function (e) { window.__net.push({ n: e.name, t: e.initiatorType, d: Math.round(e.duration) }); });
  }).observe({ type: 'resource', buffered: true });
} catch (e) {}
`

const page = `<!doctype html><html><head><meta charset="utf-8"><title>spike</title></head>
<body><h1 id="t">归知 spike</h1>
<script>window.__INITIAL_STATE__ = {title: "hello", n: 42};</script>
<script>fetch('/api').then(function(r){return r.json()}).then(function(j){document.body.dataset.api=j.ok});</script>
<img src="/pixel.png" width="1" height="1">
</body></html>`

func report(name string, v any, err error) {
	if err != nil {
		fmt.Printf("[FAIL] %-28s %v\n", name, err)
		return
	}
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	fmt.Printf("[ OK ] %-28s %s\n", name, s)
}

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "srv", Value: "from-server", Path: "/", Expires: time.Now().Add(24 * time.Hour)})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":"yes"}`)
	})
	mux.HandleFunc("/pixel.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\n"))
	})
	go http.Serve(ln, mux)
	url := "http://" + ln.Addr().String() + "/"

	sess := mygo.NewSession("guizhi-capture-spike") // 持久会话，对应 Electron partition
	deadline := time.AfterFunc(60*time.Second, func() {
		fmt.Println("[FAIL] 超时：60 秒内未完成")
		os.Exit(2)
	})
	defer deadline.Stop()

	mygo.App.WhenReady(func() {
		// 上一次运行遗留的 cookie（持久会话验证）
		before, err := sess.Cookies.List(mygo.CookieFilter{Name: "persist"})
		report("持久 cookie（运行前）", len(before), err)

		win := mygo.NewWindow(mygo.WindowOptions{
			Title:  "spike",
			Width:  1280,
			Height: 800,
			URL:    url,
			Hidden: true,
			Page: mygo.PageOptions{
				Session:              sess,
				BackgroundThrottling: mygo.ThrottlingOff,
				PreloadScript:        preload,
			},
		})
		p := win.Page()
		p.OnDidFinishLoad(func() {
			go func() {
				defer mygo.App.Quit()
				time.Sleep(500 * time.Millisecond) // 等 fetch 与图片

				v, err := p.Eval(`document.title`)
				report("Eval document.title", v, err)
				v, err = p.Eval(`JSON.stringify(window.__INITIAL_STATE__)`)
				report("Eval __INITIAL_STATE__", v, err)
				v, err = p.Eval(`document.documentElement.outerHTML.length`)
				report("Eval outerHTML.length", v, err)
				v, err = p.Eval(`document.body.dataset.api`)
				report("fetch 结果回写 DOM", v, err)
				v, err = p.Eval(`JSON.stringify(window.__net)`)
				report("PerformanceObserver 资源", v, err)
				v, err = p.Eval(`(async () => { await new Promise(r => setTimeout(r, 100)); return 'async-ok'; })()`)
				report("Eval 返回 Promise", v, err)

				png, err := win.CapturePage()
				if err == nil {
					err = os.WriteFile("capture.png", png, 0o644)
				}
				report("CapturePage(隐藏窗口)", len(png), err)

				cs, err := sess.Cookies.List(mygo.CookieFilter{})
				names := []string{}
				for _, c := range cs {
					names = append(names, c.Name)
				}
				report("Cookies.List（含服务端下发）", names, err)

				err = sess.Cookies.Set(mygo.Cookie{
					Name: "persist", Value: "1", Domain: "127.0.0.1", Path: "/",
					Expires: time.Now().Add(24 * time.Hour), HTTPOnly: true,
				})
				report("Cookies.Set 持久 cookie", "set", err)
			}()
		})
		p.OnDidFailLoad(func(e *mygo.LoadError) {
			fmt.Printf("[FAIL] 页面加载失败：%v\n", e)
			mygo.App.Quit()
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
