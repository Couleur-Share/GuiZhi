// 阶段 0 验证原型：独立采集进程模型。
// 主进程（带界面，不开调试端口）拉起子进程（采集进程，开 --remote-debugging-port=0），
// 然后只通过 DevTools 协议驱动采集进程：空白页附加后再导航、拦截首个文档请求、
// 下载与权限拦截、截图；并验证主界面不会被暴露、采集进程崩溃不影响主进程。
// 运行：go run .
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

func report(name string, ok bool, detail string) {
	tag := "[ OK ]"
	if !ok {
		tag = "[FAIL]"
	}
	fmt.Printf("%s %-36s %s\n", tag, name, detail)
}

func findPortFile(dir string, wait time.Duration) string {
	end := time.Now().Add(wait)
	for time.Now().Before(end) {
		var found string
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "DevToolsActivePort" {
				found = p
			}
			return nil
		})
		if found != "" {
			return found
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ""
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-child" {
		runChild(os.Args[2])
		return
	}
	runParent()
}

// ---------- 采集进程：只负责托管一个 WebView2，并把调试端口告诉父进程 ----------

func runChild(userData string) {
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title: "capture-child", Width: 900, Height: 600, URL: "about:blank", Hidden: true,
			Page: mygo.PageOptions{BackgroundThrottling: mygo.ThrottlingOff},
		})
		go func() {
			f := findPortFile(userData, 10*time.Second)
			raw, _ := os.ReadFile(f)
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) < 2 {
				fmt.Println("ERR no-port")
				mygo.App.Quit()
				return
			}
			fmt.Printf("READY %s %s\n", strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]))
			io.Copy(io.Discard, os.Stdin) // 父进程退出或关闭管道即结束
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
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

func getHit(path string) (int, http.Header) {
	hitsMu.Lock()
	defer hitsMu.Unlock()
	if h := hits[path]; h != nil {
		return h.count, h.hdr
	}
	return 0, nil
}

func serve() (string, func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	wrap := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			hitsMu.Lock()
			e := hits[r.URL.Path]
			if e == nil {
				e = &hit{}
				hits[r.URL.Path] = e
			}
			e.count++
			e.hdr = r.Header.Clone()
			hitsMu.Unlock()
			h(w, r)
		}
	}
	mux.HandleFunc("/ui", wrap(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>PARENT-UI</title><body>parent ui</body>`)
	}))
	mux.HandleFunc("/doc", wrap(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>DOC</title><body><h1>采集页面</h1><img src="/img" width="2" height="2"></body>`)
	}))
	mux.HandleFunc("/img", wrap(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	mux.HandleFunc("/dl", wrap(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="x.bin"`)
		fmt.Fprint(w, "binary-data")
	}))
	go http.Serve(ln, mux)
	return "http://" + ln.Addr().String(), func() { ln.Close() }
}

// ---------- 父进程 ----------

func runParent() {
	parentData, _ := os.MkdirTemp("", "guizhi-netproc-parent-")
	childData, _ := os.MkdirTemp("", "guizhi-netproc-child-")
	defer os.RemoveAll(parentData)
	defer os.RemoveAll(childData)
	os.Setenv("WEBVIEW2_USER_DATA_FOLDER", parentData)
	os.Unsetenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS") // 主界面不开调试端口

	base, stop := serve()
	defer stop()

	time.AfterFunc(120*time.Second, func() {
		fmt.Println("[FAIL] 超时：120 秒内未完成")
		os.Exit(2)
	})

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "parent-ui", Width: 900, Height: 600, URL: base + "/ui", Hidden: true,
			Page: mygo.PageOptions{BackgroundThrottling: mygo.ThrottlingOff},
		})
		p := win.Page()
		var once sync.Once
		p.OnDidFinishLoad(func() {
			once.Do(func() { go func() { defer mygo.App.Quit(); runTests(parentData, childData, base, p) }() })
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func runTests(parentData, childData, base string, parentPage *mygo.Page) {
	// 1) 主界面不开调试端口。
	if f := findPortFile(parentData, 1500*time.Millisecond); f == "" {
		report("主进程无调试端口", true, "parent 用户数据目录里没有 DevToolsActivePort")
	} else {
		report("主进程无调试端口", false, f)
	}

	// 2) 拉起采集进程，环境变量只给它。
	cmd := exec.Command(os.Args[0], "-child", childData)
	cmd.Env = append(os.Environ(),
		"WEBVIEW2_USER_DATA_FOLDER="+childData,
		"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=0")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		report("拉起采集进程", false, err.Error())
		return
	}
	defer func() { stdin.Close(); cmd.Process.Kill() }()
	lineCh := make(chan string, 1)
	go func() {
		buf := make([]byte, 256)
		n, _ := stdout.Read(buf)
		lineCh <- strings.TrimSpace(string(buf[:n]))
	}()
	var line string
	select {
	case line = <-lineCh:
	case <-time.After(30 * time.Second):
		report("采集进程就绪", false, "超时")
		return
	}
	f := strings.Fields(line)
	if len(f) != 3 || f[0] != "READY" {
		report("采集进程就绪", false, line)
		return
	}
	report("采集进程就绪", true, "port="+f[1])

	d, err := dialCDP(context.Background(), "ws://127.0.0.1:"+f[1]+f[2])
	if err != nil {
		report("连接采集进程 CDP", false, err.Error())
		return
	}

	// 3) 采集进程只暴露自己的页面，看不到主界面。
	res, _ := d.call("", "Target.getTargets", nil)
	var tg struct {
		TargetInfos []struct{ TargetID, Type, URL, Title string } `json:"targetInfos"`
	}
	json.Unmarshal(res, &tg)
	var targetID string
	seeParent := false
	for _, t := range tg.TargetInfos {
		if t.Type == "page" && targetID == "" {
			targetID = t.TargetID
		}
		if strings.Contains(t.URL, "/ui") || t.Title == "PARENT-UI" {
			seeParent = true
		}
	}
	report("采集进程的 CDP 看不到主界面", targetID != "" && !seeParent, fmt.Sprintf("targets=%d seeParent=%v", len(tg.TargetInfos), seeParent))
	if targetID == "" {
		return
	}
	res, err = d.call("", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true})
	if err != nil {
		report("附加空白页", false, err.Error())
		return
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(res, &at)
	sid := at.SessionID

	// 4) 事件：Fetch 拦截、加载完成、下载。
	loaded := make(chan struct{}, 4)
	var evMu sync.Mutex
	var downloads []string
	d.onEvent = func(m cdpMsg) {
		switch m.Method {
		case "Page.loadEventFired":
			select {
			case loaded <- struct{}{}:
			default:
			}
		case "Browser.downloadWillBegin":
			evMu.Lock()
			downloads = append(downloads, "begin")
			evMu.Unlock()
		case "Browser.downloadProgress":
			var e struct{ State string }
			json.Unmarshal(m.Params, &e)
			evMu.Lock()
			downloads = append(downloads, e.State)
			evMu.Unlock()
		case "Fetch.requestPaused":
			go func() {
				var e struct {
					RequestID string `json:"requestId"`
					Request   struct {
						URL     string            `json:"url"`
						Headers map[string]string `json:"headers"`
					} `json:"request"`
				}
				json.Unmarshal(m.Params, &e)
				path := strings.TrimPrefix(e.Request.URL, base)
				switch path {
				case "/img":
					d.call(m.SessionID, "Fetch.failRequest", map[string]any{"requestId": e.RequestID, "errorReason": "BlockedByClient"})
				case "/doc":
					hs := []map[string]string{{"name": "X-Guizhi", "value": "doc"}}
					for k, v := range e.Request.Headers {
						hs = append(hs, map[string]string{"name": k, "value": v})
					}
					d.call(m.SessionID, "Fetch.continueRequest", map[string]any{"requestId": e.RequestID, "headers": hs})
				default:
					d.call(m.SessionID, "Fetch.continueRequest", map[string]any{"requestId": e.RequestID})
				}
			}()
		}
	}
	// 先在空白页上启用拦截，再导航：首个文档请求也会被拦到。
	for _, c := range []struct {
		sid string
		m   string
		p   any
	}{
		{sid, "Page.enable", nil},
		{sid, "Network.enable", nil},
		{sid, "Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}}},
		{"", "Browser.setDownloadBehavior", map[string]any{"behavior": "deny", "eventsEnabled": true}},
		{"", "Browser.setPermission", map[string]any{"permission": map[string]any{"name": "geolocation"}, "setting": "denied"}},
	} {
		if _, err := d.call(c.sid, c.m, c.p); err != nil {
			report(c.m, false, err.Error())
		} else {
			report(c.m, true, "")
		}
	}

	// 5) 先附加再导航。
	if _, err := d.call(sid, "Page.navigate", map[string]any{"url": base + "/doc"}); err != nil {
		report("Page.navigate", false, err.Error())
		return
	}
	select {
	case <-loaded:
	case <-time.After(8 * time.Second):
		report("文档加载完成", false, "超时")
	}
	n, hdr := getHit("/doc")
	report("首个文档请求被拦截并改写", n == 1 && hdr.Get("X-Guizhi") == "doc", fmt.Sprintf("hits=%d X-Guizhi=%q", n, hdr.Get("X-Guizhi")))
	time.Sleep(300 * time.Millisecond)
	n, _ = getHit("/img")
	report("文档内子资源被拦截", n == 0, fmt.Sprintf("img hits=%d", n))

	// 6) 取值与截图（替代 Page.Eval / CapturePage）。
	res, err = d.call(sid, "Runtime.evaluate", map[string]any{"expression": "document.title + '|' + document.body.innerText.trim()", "returnByValue": true})
	var ev struct{ Result struct{ Value string } }
	json.Unmarshal(res, &ev)
	report("Runtime.evaluate 取值", err == nil && strings.HasPrefix(ev.Result.Value, "DOC|"), ev.Result.Value)
	res, err = d.call(sid, "Page.captureScreenshot", map[string]any{"format": "png"})
	var shot struct{ Data string }
	json.Unmarshal(res, &shot)
	report("Page.captureScreenshot（隐藏窗口）", err == nil && len(shot.Data) > 1000, fmt.Sprintf("base64 长度=%d", len(shot.Data)))

	// 7) 权限：地理位置应被直接拒绝而不是挂起。
	res, err = d.call(sid, "Runtime.evaluate", map[string]any{
		"awaitPromise": true, "returnByValue": true,
		"expression": `Promise.race([new Promise(r => navigator.geolocation.getCurrentPosition(() => r('granted'), e => r('denied:' + e.code))), new Promise(r => setTimeout(() => r('hang'), 4000))])`,
	})
	json.Unmarshal(res, &ev)
	report("权限拦截（geolocation）", ev.Result.Value == "denied:1", ev.Result.Value)

	// 8) 下载：应被拒绝，不落盘。
	d.call(sid, "Page.navigate", map[string]any{"url": base + "/dl"})
	time.Sleep(2 * time.Second)
	n, _ = getHit("/dl")
	var files []string
	filepath.WalkDir(childData, func(p string, de fs.DirEntry, err error) error {
		if err == nil && !de.IsDir() && (strings.HasSuffix(p, "x.bin") || strings.HasSuffix(p, "x.bin.crdownload")) {
			files = append(files, p)
		}
		return nil
	})
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, "Downloads", "x.bin")} {
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
	}
	evMu.Lock()
	dl := strings.Join(downloads, ",")
	evMu.Unlock()
	report("下载被拒绝且不落盘", len(files) == 0, fmt.Sprintf("server hits=%d files=%v events=[%s]", n, files, dl))

	// 9) 崩溃隔离：杀掉采集进程，主进程窗口仍可用。
	cmd.Process.Kill()
	time.Sleep(500 * time.Millisecond)
	v, err := parentPage.Eval(`document.title`)
	report("采集进程被杀后主进程窗口仍可用", err == nil && fmt.Sprint(v) == "PARENT-UI", fmt.Sprint(v))
}
