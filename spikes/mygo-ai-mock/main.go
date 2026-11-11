// 本地模拟的 AI 服务，用来对拍 ai:httpRequest / ai:httpStream：TS 原版与 Go 移植都连它。
// 只监听回环地址；启动后打印 "MOCK http://127.0.0.1:<port>"。
// 用法：go run . [-port 0]
package main

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Test", "sse")
}

func main() {
	port := flag.Int("port", 0, "监听端口，0 为随机")
	flag.Parse()
	mux := http.NewServeMux()

	// 20 个 SSE 事件，每个间隔 10ms，最后 [DONE]
	mux.HandleFunc("/sse/basic", func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		for i := 0; i < 20; i++ {
			fmt.Fprintf(w, "data: {\"i\":%d}\n\n", i)
			flush(w)
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	// 多字节字符被拆在 TCP 写之间：逐字节写出「你好🙂」
	mux.HandleFunc("/sse/utf8split", func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		for _, b := range []byte("data: 你好🙂 世界\n\n") {
			w.Write([]byte{b})
			flush(w)
			time.Sleep(2 * time.Millisecond)
		}
	})
	// 非法 UTF-8：孤立高位字节、被截断的三字节序列后接 ASCII
	mux.HandleFunc("/sse/malformed", func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		w.Write([]byte("ok \xe2\x82A \xff\xfe end \xf0\x9f\x99\n\n"))
	})
	// 3 MB 一次写出
	mux.HandleFunc("/sse/big", func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		line := strings.Repeat("x", 1023) + "\n"
		for i := 0; i < 3*1024; i++ {
			io.WriteString(w, line)
		}
	})
	// 5000 个小事件，不加延迟
	mux.HandleFunc("/sse/many", func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		for i := 0; i < 5000; i++ {
			fmt.Fprintf(w, "data: {\"n\":%05d}\n\n", i)
			if i%50 == 49 {
				flush(w)
			}
		}
	})
	// 发出头和一个事件后停住 30 秒：用来测取消与超时
	mux.HandleFunc("/sse/stall", func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		fmt.Fprint(w, "data: first\n\n")
		flush(w)
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	})
	// 发两个事件后直接断开连接，不发结束块
	mux.HandleFunc("/sse/abort", func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		fmt.Fprint(w, "data: one\n\ndata: two\n\n")
		flush(w)
		time.Sleep(30 * time.Millisecond)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	})
	// 3 秒后才发响应头
	mux.HandleFunc("/sse/slowheaders", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(3 * time.Second):
		}
		sseHeaders(w)
		fmt.Fprint(w, "data: late\n\n")
	})
	mux.HandleFunc("/json/ok", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Test", "ok")
		io.WriteString(w, `{"ok":true,"msg":"你好"}`)
	})
	for _, code := range []int{400, 401, 404, 429, 500, 503} {
		code := code
		mux.HandleFunc(fmt.Sprintf("/json/%d", code), func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			fmt.Fprintf(w, `{"error":{"message":"status %d"}}`, code)
		})
	}
	// 流式请求遇到非 2xx：整体当作普通响应返回，不发 chunk
	mux.HandleFunc("/sse/500", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(500)
		io.WriteString(w, "upstream exploded")
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"method":%q,"auth":%q,"ct":%q,"body":%q}`, r.Method, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(b))
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/json/ok", http.StatusFound)
	})
	mux.HandleFunc("/redirect-meta", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	})
	mux.HandleFunc("/gzip", func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write([]byte(`{"gzip":true}`))
		gz.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(buf.Bytes())
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		panic(err)
	}
	fmt.Printf("MOCK http://%s\n", ln.Addr())
	_ = http.Serve(ln, mux)
}
