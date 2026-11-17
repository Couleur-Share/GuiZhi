package main

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
)

// hitCounter 记录本机回环 HTTP 服务收到的请求：路径 → 次数。
// 岛里的页面把这个地址当作「外部网站」：只要计数涨了，就说明页面的请求真的出了岛。
type hitCounter struct {
	mu     sync.Mutex
	m      map[string]int
	detail []map[string]string
}

// hitHeaders 是记下来用来判断「这个请求是什么发起的」的请求头。
var hitHeaders = []string{"Sec-Fetch-Dest", "Sec-Fetch-Mode", "Sec-Fetch-Site", "Sec-Fetch-User", "Sec-Purpose", "Purpose", "Referer", "Origin", "Upgrade-Insecure-Requests", "Content-Type"}

// record 记一次请求：路径计数，并留下方法与关键请求头（最多 64 条）。
func (h *hitCounter) record(r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.m == nil {
		h.m = map[string]int{}
	}
	h.m[r.URL.Path]++
	if len(h.detail) >= 64 {
		return
	}
	entry := map[string]string{"path": r.URL.Path, "method": r.Method}
	for _, name := range hitHeaders {
		if v := r.Header.Get(name); v != "" {
			entry[name] = v
		}
	}
	h.detail = append(h.detail, entry)
}

// details 返回请求明细（按到达顺序）的副本。
func (h *hitCounter) details() []map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]map[string]string(nil), h.detail...)
}

func (h *hitCounter) reset() {
	h.mu.Lock()
	h.m = map[string]int{}
	h.detail = nil
	h.mu.Unlock()
}

// snapshot 返回当前计数的副本。
func (h *hitCounter) snapshot() map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]int, len(h.m))
	for k, v := range h.m {
		out[k] = v
	}
	return out
}

// paths 返回命中过的路径（排序）。
func (h *hitCounter) paths() []string {
	snap := h.snapshot()
	out := make([]string, 0, len(snap))
	for k := range snap {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// startLoopback 在 127.0.0.1 的随机端口起一个 HTTP 服务，返回它的地址、计数器与停止函数。
// 每个请求都带 CORS 放行头，让「请求是否到达服务」与「页面能否读到响应」分开判断。
func startLoopback() (url string, hits *hitCounter, stop func()) {
	hits = &hitCounter{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("回环服务监听失败：%v", err))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hits.record(r)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		switch r.URL.Path {
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
		case "/script.js":
			w.Header().Set("Content-Type", "application/javascript")
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		fmt.Fprint(w, "ok")
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	return "http://" + listener.Addr().String(), hits, func() { _ = server.Close() }
}
