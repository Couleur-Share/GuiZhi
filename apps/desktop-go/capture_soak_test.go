package main

// 采集窗口池的长时间 / 大批量压测（默认跳过）。页面全部来自本地 httptest，不访问任何真实站点。
//
//	GUIZHI_SOAK_TASKS=<任务数>  才运行；GUIZHI_SOAK_OUT=<结果 JSON 路径>；
//	GUIZHI_SOAK_CONCURRENCY=<并发，默认 4>；GUIZHI_SOAK_IDLE_WAIT=<秒>：任务结束后空闲等待，验证空闲回收。
//
// 页面混合了正常脚本渲染、大 DOM、大量图片、弹窗、子框架、Cookie/存储、页面内持有大内存，
// 以及两类「必然失败」的任务（请求数预算耗尽、超时取消），用来走遍「窗口被替换」的路径。
// 进程树内存由外部脚本（verify/run-pool-soak.ps1）按进程父子关系采样，这里只负责驱动与记账。
// Soak test for the capture window pool: local pages only; memory is sampled externally.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type soakKind struct {
	name       string
	path       string
	mustHave   string        // 成功时渲染结果必须包含的文字
	expectFail bool          // 本类任务必然失败（失败本身是预期）
	timeout    time.Duration // 单任务超时
}

func TestCapturePoolSoak(t *testing.T) {
	total, _ := strconv.Atoi(os.Getenv("GUIZHI_SOAK_TASKS"))
	if total <= 0 {
		t.Skip("未设置 GUIZHI_SOAK_TASKS")
	}
	conc, _ := strconv.Atoi(os.Getenv("GUIZHI_SOAK_CONCURRENCY"))
	if conc <= 0 {
		conc = 4
	}
	idleWait, _ := strconv.Atoi(os.Getenv("GUIZHI_SOAK_IDLE_WAIT"))

	mux := http.NewServeMux()
	html := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body)
	}
	// 随任务号变化的内容，避免任何层面的缓存命中把压力抹平
	mux.HandleFunc("/spa", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><head><title>应用</title><script src="/app.js?n=`+r.URL.Query().Get("n")+`"></script></head><body><div id="root"></div></body></html>`)
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, `fetch('/api/data').then(r=>r.json()).then(d=>{document.getElementById('root').innerHTML='<article><h1>'+d.title+'</h1><p>'+d.text+'</p></article>'})`)
	})
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"title":"SOAKSPA","text":%q}`, renderLongText)
	})
	mux.HandleFunc("/heavy", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`<html><body><p>SOAKHEAVY ` + renderLongText + `</p>`)
		for i := 0; i < 20000; i++ {
			fmt.Fprintf(&b, "<div><span>节点 %d</span></div>", i)
		}
		b.WriteString(`</body></html>`)
		html(w, b.String())
	})
	mux.HandleFunc("/images", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`<html><body><p>SOAKIMAGES ` + renderLongText + `</p>`)
		for i := 0; i < 80; i++ {
			fmt.Fprintf(&b, `<img src="/pic.png?i=%d">`, i)
		}
		b.WriteString(`</body></html>`)
		html(w, b.String())
	})
	mux.HandleFunc("/popup", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><p>SOAKPOPUP `+renderLongText+`</p><script>window.open('/popped');alert('hi');document.cookie='k=1'</script></body></html>`)
	})
	mux.HandleFunc("/popped", func(w http.ResponseWriter, r *http.Request) { html(w, `<p>弹窗</p>`) })
	mux.HandleFunc("/frames", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><p>SOAKFRAMES `+renderLongText+`</p><iframe src="/inner"></iframe><iframe src="http://other.test/foreign"></iframe></body></html>`)
	})
	mux.HandleFunc("/inner", func(w http.ResponseWriter, r *http.Request) { html(w, `<p>同源子框架</p>`) })
	mux.HandleFunc("/cookie", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "secret", Path: "/"})
		html(w, `<html><body><p>SOAKCOOKIE `+renderLongText+`</p><script>localStorage.setItem('x','1');indexedDB.open('db'+Math.random())</script></body></html>`)
	})
	// 页面自己持有约 64 MB：窗口复用 / 替换后这部分必须被回收，否则进程树内存只增不减
	mux.HandleFunc("/hold", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><p>SOAKHOLD `+renderLongText+`</p><script>window.__hold=[];for(let i=0;i<16;i++){window.__hold.push(new Uint8Array(4*1024*1024).fill(i+1))}</script></body></html>`)
	})
	mux.HandleFunc("/pic.png", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("PNG")) })
	mux.HandleFunc("/flood", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><body><p>`+renderLongText+`</p><script>for(let i=0;i<260;i++){fetch('/api/data?i='+i).catch(()=>{})}</script></body></html>`)
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	})

	fx := newFetchFixture(t, mux)
	base := "http://site.test:" + fx.port()
	host := newRenderHost(func() *safeFetcher { return fx.f })
	t.Cleanup(host.Close)

	// 加权循环：每 20 个任务一轮
	kinds := []soakKind{
		{"spa", "/spa", "SOAKSPA", false, 30 * time.Second},
		{"heavy", "/heavy", "SOAKHEAVY", false, 40 * time.Second},
		{"images", "/images", "SOAKIMAGES", false, 30 * time.Second},
		{"popup", "/popup", "SOAKPOPUP", false, 30 * time.Second},
		{"frames", "/frames", "SOAKFRAMES", false, 30 * time.Second},
		{"cookie", "/cookie", "SOAKCOOKIE", false, 30 * time.Second},
		{"hold", "/hold", "SOAKHOLD", false, 30 * time.Second},
		{"flood", "/flood", "", true, 30 * time.Second},
		{"slow", "/slow", "", true, 4 * time.Second},
	}
	cycle := []int{0, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 0, 0, 1, 6, 7, 8, 2, 5}

	type stat struct {
		ok, failed, unexpected int
		totalMs                int64
	}
	var mu sync.Mutex
	stats := map[string]*stat{}
	var unexpected []string
	for _, k := range kinds {
		stats[k.name] = &stat{}
	}
	var next atomic.Int64
	goroutinesAtStart := runtime.NumGoroutine()
	started := time.Now()

	var wg sync.WaitGroup
	for w := 0; w < conc; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				n := int(next.Add(1)) - 1
				if n >= total {
					return
				}
				k := kinds[cycle[n%len(cycle)]]
				ctx, cancel := context.WithTimeout(context.Background(), k.timeout)
				t0 := time.Now()
				page, err := host.Render(ctx, webCaptureRequest{TaskID: fmt.Sprint(n), Purpose: "import", URL: fmt.Sprintf("%s%s?n=%d", base, k.path, n)})
				cancel()
				ms := time.Since(t0).Milliseconds()
				mu.Lock()
				s := stats[k.name]
				s.totalMs += ms
				switch {
				case k.expectFail && err != nil:
					s.failed++
				case !k.expectFail && err == nil && strings.Contains(page.HTML, k.mustHave):
					s.ok++
				default:
					s.unexpected++
					if len(unexpected) < 12 {
						msg := "成功但内容不符"
						if err != nil {
							msg = err.Error()
						} else if k.expectFail {
							msg = "本应失败却成功"
						}
						unexpected = append(unexpected, fmt.Sprintf("#%d %s：%s", n, k.name, msg))
					}
				}
				mu.Unlock()
				if n > 0 && n%200 == 0 {
					t.Logf("进度 %d/%d，已用 %s", n, total, time.Since(started).Round(time.Second))
				}
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(started)

	// 本轮用过多少个不同的窗口（每次替换都会新增一个 target）
	windows := 0
	host.mu.Lock()
	if host.b != nil && host.b.pool != nil {
		host.b.pool.mu.Lock()
		windows = host.b.pool.replaced
		host.b.pool.mu.Unlock()
	}
	host.mu.Unlock()

	idleStopped := "未等待"
	if idleWait > 0 {
		time.Sleep(time.Duration(idleWait) * time.Second)
		host.mu.Lock()
		if host.b == nil {
			idleStopped = "已回收（浏览器进程已停止）"
		} else {
			idleStopped = "未回收"
		}
		host.mu.Unlock()
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	totalOK, totalUnexpected := 0, 0
	for _, s := range stats {
		totalOK += s.ok + s.failed
		totalUnexpected += s.unexpected
	}
	summary := map[string]any{
		"tasks": total, "concurrency": conc, "seconds": int(elapsed.Seconds()),
		"asExpected": totalOK, "unexpected": totalUnexpected, "unexpectedSamples": unexpected,
		"windowsReplaced": windows, "perKind": stats,
		"goroutinesStart": goroutinesAtStart, "goroutinesEnd": runtime.NumGoroutine(), "goHeapMB": ms.HeapAlloc >> 20,
		"idleReclaim": idleStopped,
	}
	if out := os.Getenv("GUIZHI_SOAK_OUT"); out != "" {
		perKind := map[string]any{}
		for k, s := range stats {
			perKind[k] = map[string]any{"ok": s.ok, "failedAsExpected": s.failed, "unexpected": s.unexpected, "avgMs": func() int64 {
				if n := s.ok + s.failed + s.unexpected; n > 0 {
					return s.totalMs / int64(n)
				}
				return 0
			}()}
		}
		summary["perKind"] = perKind
		b, _ := json.MarshalIndent(summary, "", " ")
		_ = os.WriteFile(out, b, 0o600)
	}
	t.Logf("压测完成：%v", summary)
	if totalUnexpected > 0 {
		t.Errorf("%d 个任务结果不符合预期：%v", totalUnexpected, unexpected)
	}
}
