// 阶段 0 验证原型：MyGo 里能否替代 Electron 的网络层拦截（webRequest / debugger / setUserAgent）。
// 思路：进程启动前设置 WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=0，
// 再由 Go 自己连上 WebView2 的 DevTools 协议，用 Fetch / Network / Emulation 域做拦截与观测。
// 运行：go run .
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/egoist/mygo"
)

// ---------- 最小 CDP 客户端 ----------

type cdpMsg struct {
	ID        int                       `json:"id"`
	Method    string                    `json:"method"`
	Params    json.RawMessage           `json:"params"`
	Result    json.RawMessage           `json:"result"`
	Error     *struct{ Message string } `json:"error"`
	SessionID string                    `json:"sessionId"`
}

type cdp struct {
	c       *websocket.Conn
	mu      sync.Mutex
	nextID  int
	pend    map[int]chan cdpMsg
	onEvent func(m cdpMsg)
}

func dialCDP(ctx context.Context, url string) (*cdp, error) {
	// 不带 Origin：DevTools 对浏览器来源的 WebSocket 会拒绝，非浏览器客户端不受影响。
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(64 << 20)
	d := &cdp{c: c, pend: map[int]chan cdpMsg{}}
	go func() {
		for {
			_, b, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var m cdpMsg
			if json.Unmarshal(b, &m) != nil {
				continue
			}
			if m.ID != 0 {
				d.mu.Lock()
				ch := d.pend[m.ID]
				delete(d.pend, m.ID)
				d.mu.Unlock()
				if ch != nil {
					ch <- m
				}
				continue
			}
			if d.onEvent != nil {
				d.onEvent(m)
			}
		}
	}()
	return d, nil
}

func (d *cdp) call(sid, method string, params any) (json.RawMessage, error) {
	d.mu.Lock()
	d.nextID++
	id := d.nextID
	ch := make(chan cdpMsg, 1)
	d.pend[id] = ch
	d.mu.Unlock()
	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if sid != "" {
		req["sessionId"] = sid
	}
	b, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := d.c.Write(ctx, websocket.MessageText, b); err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		return m.Result, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: 超时", method)
	}
}

// ---------- 测试服务器 ----------

type hit struct {
	count int
	hdr   http.Header
}

var (
	hitsMu sync.Mutex
	hits   = map[string]*hit{}
)

func record(r *http.Request) {
	hitsMu.Lock()
	defer hitsMu.Unlock()
	h := hits[r.URL.Path]
	if h == nil {
		h = &hit{}
		hits[r.URL.Path] = h
	}
	h.count++
	h.hdr = r.Header.Clone()
}

func getHit(path string) (int, http.Header) {
	hitsMu.Lock()
	defer hitsMu.Unlock()
	if h := hits[path]; h != nil {
		return h.count, h.hdr
	}
	return 0, nil
}

func report(name string, ok bool, detail string) {
	tag := "[ OK ]"
	if !ok {
		tag = "[FAIL]"
	}
	fmt.Printf("%s %-34s %s\n", tag, name, detail)
}

func main() {
	userData, _ := os.MkdirTemp("", "guizhi-mygo-net-")
	defer os.RemoveAll(userData)
	os.Setenv("WEBVIEW2_USER_DATA_FOLDER", userData)
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--remote-debugging-port=0")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>net</title><body>net spike</body>`)
	})
	for _, p := range []string{"/api", "/blocked", "/nav-blocked"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			record(r)
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			fmt.Fprint(w, "ok")
		})
	}
	mux.HandleFunc("/resp", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(404)
		fmt.Fprint(w, "nope")
	})
	go http.Serve(ln, mux)
	base := "http://" + ln.Addr().String()

	time.AfterFunc(90*time.Second, func() {
		fmt.Println("[FAIL] 超时：90 秒内未完成")
		os.Exit(2)
	})

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "net", Width: 900, Height: 600, URL: base + "/", Hidden: true,
			Page: mygo.PageOptions{BackgroundThrottling: mygo.ThrottlingOff},
		})
		p := win.Page()
		failed := make(chan string, 4)
		p.OnDidFailLoad(func(e *mygo.LoadError) { failed <- fmt.Sprint(e) })
		var once sync.Once
		p.OnDidFinishLoad(func() {
			once.Do(func() { go func() { defer mygo.App.Quit(); run(userData, base, p, failed) }() })
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func evalJSON(p *mygo.Page, js string) string {
	v, err := p.Eval(js)
	if err != nil {
		return "ERR " + err.Error()
	}
	return fmt.Sprint(v)
}

func run(userData, base string, p *mygo.Page, failed chan string) {
	// 1) 找到调试端口：--remote-debugging-port=0 时 Chromium 把端口写进 DevToolsActivePort。
	var portFile string
	for i := 0; i < 50 && portFile == ""; i++ {
		filepath.WalkDir(userData, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "DevToolsActivePort" {
				portFile = path
			}
			return nil
		})
		if portFile == "" {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if portFile == "" {
		report("DevToolsActivePort", false, "未生成")
		return
	}
	raw, _ := os.ReadFile(portFile)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	port, wsPath := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
	report("DevToolsActivePort", true, "port="+port)

	// 安全：只应监听回环地址。
	exposed := false
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			c, err := net.DialTimeout("tcp", net.JoinHostPort(ipn.IP.String(), port), time.Second)
			if err == nil {
				c.Close()
				exposed = true
			}
		}
	}
	report("调试端口不对局域网开放", !exposed, fmt.Sprintf("exposed=%v", exposed))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := dialCDP(ctx, "ws://127.0.0.1:"+port+wsPath)
	if err != nil {
		report("连接 CDP", false, err.Error())
		return
	}
	report("连接 CDP（browser 端点）", true, "")

	// 2) 找到本页并附加（flatten 模式，命令带 sessionId）。
	res, err := d.call("", "Target.getTargets", nil)
	if err != nil {
		report("Target.getTargets", false, err.Error())
		return
	}
	var tg struct {
		TargetInfos []struct{ TargetID, Type, URL string } `json:"targetInfos"`
	}
	json.Unmarshal(res, &tg)
	var targetID string
	for _, t := range tg.TargetInfos {
		if t.Type == "page" && strings.HasPrefix(t.URL, base) {
			targetID = t.TargetID
		}
	}
	if targetID == "" {
		report("定位页面 target", false, fmt.Sprintf("%+v", tg.TargetInfos))
		return
	}
	res, err = d.call("", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true})
	if err != nil {
		report("attachToTarget", false, err.Error())
		return
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(res, &at)
	sid := at.SessionID
	report("附加到页面 target", sid != "", "")

	// 3) 事件处理：Fetch 拦截 + Network 观测。
	var evMu sync.Mutex
	statuses := map[string]int{}
	d.onEvent = func(m cdpMsg) {
		switch m.Method {
		case "Network.responseReceived":
			var e struct {
				Response struct {
					URL    string
					Status int
				}
			}
			json.Unmarshal(m.Params, &e)
			evMu.Lock()
			statuses[strings.TrimPrefix(e.Response.URL, base)] = e.Response.Status
			evMu.Unlock()
		case "Fetch.requestPaused":
			go handlePaused(d, m, base)
		}
	}
	for _, c := range []struct {
		m string
		p any
	}{
		{"Network.enable", nil},
		{"Fetch.enable", map[string]any{"patterns": []map[string]any{
			{"urlPattern": "*", "requestStage": "Request"},
			{"urlPattern": "*/resp*", "requestStage": "Response"},
		}}},
		{"Emulation.setUserAgentOverride", map[string]any{"userAgent": "Mozilla/5.0 GuiZhiUA/1"}},
	} {
		if _, err := d.call(sid, c.m, c.p); err != nil {
			report(c.m, false, err.Error())
			return
		}
	}
	// handlePaused 用的 sessionId 通过全局变量传入，避免改签名。
	curSession = sid

	// 4) 触发请求。
	out := evalJSON(p, `(async () => {
	  const r = {};
	  r.api = (await fetch('/api')).status;
	  try { await fetch('/blocked'); r.blocked = 'passed'; } catch (e) { r.blocked = 'failed'; }
	  const x = await fetch('/resp');
	  r.resp = [x.status, x.headers.get('x-guizhi-resp')];
	  return JSON.stringify(r);
	})()`)
	report("页面内三次 fetch", !strings.HasPrefix(out, "ERR"), out)
	time.Sleep(300 * time.Millisecond)

	// 5) 校验。
	n, hdr := getHit("/api")
	report("请求头改写（X-Guizhi 到达服务端）", n == 1 && hdr.Get("X-Guizhi") == "1", fmt.Sprintf("hits=%d X-Guizhi=%q", n, hdr.Get("X-Guizhi")))
	report("请求头改写后原有头仍在（Accept 等）", hdr.Get("Accept") != "", fmt.Sprintf("Accept=%q", hdr.Get("Accept")))
	report("User-Agent 覆盖", strings.Contains(hdr.Get("User-Agent"), "GuiZhiUA/1"), hdr.Get("User-Agent"))
	n, _ = getHit("/blocked")
	report("子请求拦截（服务端 0 命中）", n == 0, fmt.Sprintf("hits=%d", n))
	evMu.Lock()
	st := statuses["/resp"]
	evMu.Unlock()
	report("响应状态码观测（Network）", st == 404, fmt.Sprintf("status=%d", st))

	// 6) 主框架导航拦截：应触发 OnDidFailLoad，服务端不收到请求。
	p.Eval(`location.href='/nav-blocked'; 1`)
	select {
	case msg := <-failed:
		n, _ = getHit("/nav-blocked")
		report("主框架导航拦截 + OnDidFailLoad", n == 0, fmt.Sprintf("hits=%d err=%s", n, msg))
	case <-time.After(5 * time.Second):
		n, _ = getHit("/nav-blocked")
		report("主框架导航拦截 + OnDidFailLoad", false, fmt.Sprintf("未收到失败事件 hits=%d", n))
	}
}

var curSession string

func handlePaused(d *cdp, m cdpMsg, base string) {
	var e struct {
		RequestID          string `json:"requestId"`
		ResponseStatusCode int    `json:"responseStatusCode"`
		Request            struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"request"`
		ResponseHeaders []map[string]string `json:"responseHeaders"`
	}
	json.Unmarshal(m.Params, &e)
	path := strings.TrimPrefix(e.Request.URL, base)
	sid := m.SessionID
	if sid == "" {
		sid = curSession
	}
	if e.ResponseStatusCode != 0 { // 响应阶段：改写响应头，保留原状态码。
		hs := append(e.ResponseHeaders, map[string]string{"name": "X-Guizhi-Resp", "value": "rewritten"})
		d.call(sid, "Fetch.continueResponse", map[string]any{"requestId": e.RequestID, "responseCode": e.ResponseStatusCode, "responseHeaders": hs})
		return
	}
	switch path {
	case "/blocked", "/nav-blocked":
		d.call(sid, "Fetch.failRequest", map[string]any{"requestId": e.RequestID, "errorReason": "BlockedByClient"})
	case "/api":
		hs := []map[string]string{{"name": "X-Guizhi", "value": "1"}}
		for k, v := range e.Request.Headers { // continueRequest 的 headers 是整体替换，必须带上原有的。
			hs = append(hs, map[string]string{"name": k, "value": v})
		}
		d.call(sid, "Fetch.continueRequest", map[string]any{"requestId": e.RequestID, "headers": hs})
	default:
		d.call(sid, "Fetch.continueRequest", map[string]any{"requestId": e.RequestID})
	}
}
