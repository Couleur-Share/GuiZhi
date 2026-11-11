package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// 渲染窗口的端到端测试：真实 WebView2 隐藏窗口 + CDP，页面全部来自本地 httptest 服务器（经 site.test 钉扎到本地）。

const renderLongText = "归知是本地优先的个人知识库，正文需要脚本渲染后才会出现。归知是本地优先的个人知识库，正文需要脚本渲染后才会出现。归知是本地优先的个人知识库，正文需要脚本渲染后才会出现。归知是本地优先的个人知识库，正文需要脚本渲染后才会出现。"

type renderFixture struct {
	fx   *fetchFixture
	host *renderHost
	base string
	mu   sync.Mutex
	hits map[string]int
}

func (r *renderFixture) hit(key string) {
	r.mu.Lock()
	r.hits[key]++
	r.mu.Unlock()
}

func (r *renderFixture) count(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits[key]
}

func newRenderFixture(t *testing.T) *renderFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("-short：跳过需要启动浏览器的渲染测试")
	}
	rf := &renderFixture{hits: map[string]int{}}
	mux := http.NewServeMux()
	page := func(path string, h http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			rf.hit(r.Host + path)
			h(w, r)
		})
	}
	html := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body)
	}
	page("/spa", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><head><title>应用</title><script src="/app.js"></script></head><body><div id="root"></div><a href="/next?a=1">下一页</a><img src="/pic.png"></body></html>`)
	})
	page("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, `fetch('/api/data').then(r=>r.json()).then(d=>{document.getElementById('root').innerHTML='<article><h1>'+d.title+'</h1><p>'+d.text+'</p></article>'})`)
	})
	page("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"title":"脚本渲染标题","text":%q}`, renderLongText)
	})
	page("/pic.png", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("PNG")) })
	page("/redir", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/spa", http.StatusFound) })
	page("/frames", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><p>`+renderLongText+`</p><iframe src="/inner"></iframe><iframe src="http://other.test:`+rf.port()+`/foreign"></iframe></body></html>`)
	})
	page("/inner", func(w http.ResponseWriter, r *http.Request) { html(w, `<p>同源子框架</p>`) })
	page("/foreign", func(w http.ResponseWriter, r *http.Request) { html(w, `<p>跨源子框架</p>`) })
	page("/popup", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><p>`+renderLongText+`</p><script>window.open('/popped');alert('hi');document.cookie='k=1'</script></body></html>`)
	})
	page("/popped", func(w http.ResponseWriter, r *http.Request) { html(w, `<p>弹窗</p>`) })
	page("/cookie-set", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "secret", Path: "/"})
		html(w, `<html><body><p>`+renderLongText+`</p><script>localStorage.setItem('x','1')</script></body></html>`)
	})
	page("/cookie-read", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><p>`+renderLongText+`</p><div id="c"></div><script>document.getElementById('c').textContent='COOKIE['+document.cookie+']LS['+localStorage.getItem('x')+']'</script></body></html>`)
	})
	page("/private", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><p>`+renderLongText+`</p><div id="r"></div><script>fetch('http://127.0.0.1:1/x').then(()=>{document.getElementById('r').textContent='REACHED'}).catch(()=>{document.getElementById('r').textContent='BLOCKED'})</script></body></html>`)
	})
	page("/flood", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><p>`+renderLongText+`</p><script>for(let i=0;i<260;i++){fetch('/api/data?i='+i).catch(()=>{})}</script></body></html>`)
	})
	page("/in/page", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><script>location.href="/out/page"</script></body></html>`)
	})
	page("/out/page", func(w http.ResponseWriter, r *http.Request) { html(w, `<p>范围外</p>`) })
	page("/pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4"))
	})
	page("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(8 * time.Second):
		}
	})
	page("/jsredirect", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><script>location.replace('/spa')</script></body></html>`)
	})
	rf.fx = newFetchFixture(t, mux)
	rf.base = "http://site.test:" + rf.fx.port()
	rf.host = newRenderHost(func() *safeFetcher { return rf.fx.f })
	t.Cleanup(rf.host.Close)
	return rf
}

func (r *renderFixture) port() string { return r.fx.port() }

func (r *renderFixture) render(ctx context.Context, path string, mod func(*webCaptureRequest)) (*renderedWebPage, error) {
	req := webCaptureRequest{TaskID: "t", Purpose: "import", URL: r.base + path}
	if mod != nil {
		mod(&req)
	}
	return r.host.Render(ctx, req)
}

func TestRenderScriptRenderedPage(t *testing.T) {
	rf := newRenderFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	started := time.Now()
	page, err := rf.render(ctx, "/spa", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("首次渲染（含浏览器启动）%s", time.Since(started))
	if !strings.Contains(page.HTML, "脚本渲染标题") || !strings.Contains(page.HTML, renderLongText[:30]) {
		t.Errorf("渲染后 HTML 缺少脚本写入的正文: %.300s", page.HTML)
	}
	if page.Status != 200 || page.URL != rf.base+"/spa" {
		t.Errorf("状态/地址不符: %d %s", page.Status, page.URL)
	}
	if fmt.Sprint(page.Links) != fmt.Sprint([]string{rf.base + "/next?a=1"}) {
		t.Errorf("链接 %v", page.Links)
	}
	if n := rf.count("site.test:" + rf.port() + "/pic.png"); n != 0 {
		t.Errorf("图片应以占位图本地应答，实际请求了 %d 次", n)
	}
	// 同一浏览器第二次渲染（服务端重定向）应复用窗口且更快
	second := time.Now()
	page, err = rf.render(ctx, "/redir", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("第二次渲染（重定向）%s", time.Since(second))
	if page.URL != rf.base+"/spa" || !strings.Contains(page.HTML, "脚本渲染标题") {
		t.Errorf("重定向后: %s %.200s", page.URL, page.HTML)
	}
	// 脚本跳转
	page, err = rf.render(ctx, "/jsredirect", nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.URL != rf.base+"/spa" || !strings.Contains(page.HTML, "脚本渲染标题") {
		t.Errorf("脚本跳转后: %s %.200s", page.URL, page.HTML)
	}
}

func TestRenderSubframePopupAndIsolation(t *testing.T) {
	rf := newRenderFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	page, err := rf.render(ctx, "/frames", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rf.count("site.test:"+rf.port()+"/inner") != 1 {
		t.Errorf("同源子框架应加载")
	}
	if rf.count("other.test:"+rf.port()+"/foreign") != 0 {
		t.Errorf("跨源子框架不应加载")
	}
	_ = page
	// 弹窗与 alert：弹窗页不得被请求，alert 不得卡住渲染
	if _, err := rf.render(ctx, "/popup", nil); err != nil {
		t.Fatal(err)
	}
	if rf.count("site.test:"+rf.port()+"/popped") != 0 {
		t.Errorf("window.open 的弹窗不应被请求")
	}
	// 任务之间不共享 Cookie / localStorage
	if _, err := rf.render(ctx, "/cookie-set", nil); err != nil {
		t.Fatal(err)
	}
	read, err := rf.render(ctx, "/cookie-read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read.HTML, "COOKIE[]LS[null]") {
		t.Errorf("后一个任务读到了前一个任务的存储: %s", read.HTML[strings.Index(read.HTML, "COOKIE")-20:])
	}
}

func TestRenderSecurityBoundaries(t *testing.T) {
	rf := newRenderFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	// 内网/回环地址：与 Electron 版一致，出口的安全拒绝会终止整次采集，且出口没有拨过去
	page, err := rf.render(ctx, "/private", nil)
	if err == nil || webCaptureErrorOf(err).Code != "security" {
		t.Errorf("页面访问回环地址应以 security 终止采集: %v %v", err, page)
	}
	for _, d := range rf.fx.dialed {
		if strings.HasPrefix(d, "127.0.0.1:1") {
			t.Errorf("出口不应拨向 %s", d)
		}
	}
	// 目录范围：入口在范围内，但页面脚本把主框架跳到范围外 → 终止，且范围外的页面没有被请求
	_, err = rf.render(ctx, "/in/page", func(r *webCaptureRequest) {
		r.Scope = &webScope{Origin: rf.base, Directory: "/in/"}
	})
	if err == nil || webCaptureErrorOf(err).Code != "security" {
		t.Errorf("导航越界应以 security 终止: %v", err)
	}
	if n := rf.count("site.test:" + rf.port() + "/out/page"); n != 0 {
		t.Errorf("范围外页面不应被请求，实际 %d 次", n)
	}
	// 请求数预算
	_, err = rf.render(ctx, "/flood", nil)
	if err == nil || !strings.Contains(err.Error(), "页面网络预算已用尽") {
		t.Errorf("请求洪泛应终止采集: %v", err)
	}
	// 非网页内容
	_, err = rf.render(ctx, "/pdf", nil)
	if err == nil || webCaptureErrorOf(err).Code != "incomplete" {
		t.Errorf("PDF 入口应报非网页内容: %v", err)
	}
	// 洪泛后窗口被替换，浏览器仍可正常渲染
	if page, err = rf.render(ctx, "/spa", nil); err != nil || !strings.Contains(page.HTML, "脚本渲染标题") {
		t.Errorf("安全终止后浏览器应可继续使用: %v", err)
	}
}

func TestRenderCancelAndConcurrency(t *testing.T) {
	rf := newRenderFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	// 取消：慢请求被及时中止，返回取消错误
	short, stop := context.WithTimeout(ctx, 1500*time.Millisecond)
	started := time.Now()
	_, err := rf.render(short, "/slow", nil)
	stop()
	if err == nil || webCaptureErrorOf(err).Code != "timeout" {
		t.Errorf("超时应归为 timeout: %v", err)
	}
	if time.Since(started) > 10*time.Second {
		t.Errorf("取消响应过慢：%s", time.Since(started))
	}
	// 两个并发渲染 + 第三个排队
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := rf.render(ctx, "/spa", nil)
			if err == nil && !strings.Contains(p.HTML, "脚本渲染标题") {
				err = fmt.Errorf("缺少正文")
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Errorf("并发渲染 %d 失败: %v", i, e)
		}
	}
}

// 整条链路：静态判定需要渲染 → 渲染窗口取页 → 正文提取。
func TestRenderThroughCaptureWebPage(t *testing.T) {
	rf := newRenderFixture(t)
	l := &Library{webFetcher: rf.fx.f, webRenderer: rf.host}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var stages []string
	r := l.captureWebPage(ctx, webCaptureRequest{TaskID: "t", Purpose: "import", URL: rf.base + "/spa"}, func(s string) { stages = append(stages, s) })
	if r.Error != nil {
		t.Fatalf("不应失败: %+v", r.Error)
	}
	if !strings.Contains(r.Markdown, "归知是本地优先的个人知识库") || r.FinalURL != rf.base+"/spa" {
		t.Errorf("渲染提取结果不符: %+v", r)
	}
	t.Logf("引擎 %s 标题 %q 阶段 %v", r.EngineVersion, r.Title, stages)
}

// 采集进程崩溃：进行中的渲染立即失败（不等超时），下一次渲染自动拉起新进程。
func TestRenderBrowserCrashRecovery(t *testing.T) {
	rf := newRenderFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, err := rf.render(ctx, "/spa", nil); err != nil {
		t.Fatal(err)
	}
	rf.host.mu.Lock()
	b := rf.host.b
	rf.host.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := rf.render(ctx, "/slow", nil)
		done <- err
	}()
	time.Sleep(800 * time.Millisecond)
	killed := time.Now()
	b.cmd.Process.Kill()
	select {
	case err := <-done:
		if err == nil {
			t.Errorf("进程被杀后渲染不应成功")
		}
		if d := time.Since(killed); d > 5*time.Second {
			t.Errorf("崩溃检测过慢：%s（%v）", d, err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("进程被杀后渲染没有返回")
	}
	page, err := rf.render(ctx, "/spa", nil)
	if err != nil || !strings.Contains(page.HTML, "脚本渲染标题") {
		t.Errorf("崩溃后应自动重启并可继续渲染: %v", err)
	}
}
