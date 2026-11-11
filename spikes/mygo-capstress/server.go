package main

import (
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 本地模拟站点：回放 59 页真实网页的静态 HTML，并提供各类故障与安全探针。
// 监听 127.0.0.1 与 127.0.0.2（同端口），后者代表“不该被访问的内网地址”。全程不访问外网。

type page struct {
	id    string
	body  []byte
	title string
}

type server struct {
	pages  []page
	byID   map[string]*page
	port   int
	mu     sync.Mutex
	hits   map[string]int // 路径 → 命中次数（按 Host 区分：路径@host）
	xtask  map[string]int // 任务串扰统计：ok / mismatch / missing
	secret []string       // 命中 /secret 的记录（host、via）
	lns    []net.Listener
}

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func loadCorpus(dir string) ([]page, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ps []page
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "static.html"))
		if err != nil {
			continue
		}
		t := ""
		if m := titleRe.FindSubmatch(b); m != nil {
			t = strings.Join(strings.Fields(html.UnescapeString(string(m[1]))), " ")
		}
		ps = append(ps, page{id: e.Name(), body: b, title: t})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].id < ps[j].id })
	return ps, nil
}

func newServer(corpus string) (*server, error) {
	ps, err := loadCorpus(corpus)
	if err != nil {
		return nil, err
	}
	s := &server{pages: ps, byID: map[string]*page{}, hits: map[string]int{}, xtask: map[string]int{}}
	for i := range s.pages {
		s.byID[s.pages[i].id] = &s.pages[i]
	}
	var ln net.Listener
	for try := 0; try < 20; try++ {
		l1, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		port := l1.Addr().(*net.TCPAddr).Port
		l2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.2:%d", port))
		if err != nil {
			l1.Close()
			continue
		}
		s.port = port
		s.lns = []net.Listener{l1, l2}
		ln = l1
		break
	}
	if ln == nil {
		return nil, fmt.Errorf("无法在 127.0.0.1 与 127.0.0.2 上绑定同一端口")
	}
	h := http.HandlerFunc(s.handle)
	for _, l := range s.lns {
		go http.Serve(l, h)
	}
	return s, nil
}

func (s *server) close() {
	for _, l := range s.lns {
		l.Close()
	}
}

func (s *server) base() string { return "http://127.0.0.1:" + strconv.Itoa(s.port) }

func (s *server) hitCount(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k, v := range s.hits {
		if strings.HasPrefix(k, path+"@") {
			n += v
		}
	}
	return n
}

func (s *server) secretHits() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.secret...)
}

func (s *server) resetSecret() {
	s.mu.Lock()
	s.secret = nil
	s.mu.Unlock()
}

func (s *server) crosstalk() (ok, mismatch, missing int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.xtask["ok"], s.xtask["mismatch"], s.xtask["missing"]
}

func (s *server) handle(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	s.mu.Lock()
	s.hits[r.URL.Path+"@"+host]++
	s.mu.Unlock()
	q := r.URL.Query()
	htmlOut := func(body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body)
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/p/"):
		// 真实页面：校验请求头里的任务令牌与 URL 中的一致（并发串扰检测）。
		// 只统计主文档（路径恰好是某个页面 id 且带 task 参数）；页面里的相对路径子资源不带 task 参数，不计入。
		want, got := q.Get("task"), r.Header.Get("X-Task")
		if want == "" || s.byID[strings.TrimPrefix(r.URL.Path, "/p/")] == nil {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		switch {
		case got == "":
			s.xtask["missing"]++
		case got != want:
			s.xtask["mismatch"]++
		default:
			s.xtask["ok"]++
		}
		s.mu.Unlock()
		p := s.byID[strings.TrimPrefix(r.URL.Path, "/p/")]
		if p == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(p.body)
	case r.URL.Path == "/secret":
		s.mu.Lock()
		s.secret = append(s.secret, host+" via="+q.Get("via"))
		s.mu.Unlock()
		htmlOut(`<!doctype html><title>SECRET</title><body>internal</body>`)
	case r.URL.Path == "/ok":
		htmlOut(`<!doctype html><title>OK</title><body><p>正常页面 ` + q.Get("n") + `</p></body>`)
	case r.URL.Path == "/slow":
		ms, _ := strconv.Atoi(q.Get("ms"))
		time.Sleep(time.Duration(ms) * time.Millisecond)
		htmlOut(`<!doctype html><title>SLOW</title><body>slow</body>`)
	case r.URL.Path == "/status500":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(500)
		fmt.Fprint(w, `<!doctype html><title>ERR500</title><body>server error</body>`)
	case r.URL.Path == "/redir-loop":
		http.Redirect(w, r, "/redir-loop", 302)
	case r.URL.Path == "/reset":
		if hj, ok := w.(http.Hijacker); ok {
			c, _, _ := hj.Hijack()
			c.Close()
		}
	case r.URL.Path == "/dl":
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="cap.bin"`)
		fmt.Fprint(w, "binary")
	case r.URL.Path == "/dialog":
		htmlOut(`<!doctype html><title>DIALOG</title><body onbeforeunload="return 'x'"><script>alert(1);confirm(2);prompt(3);</script>done</body>`)
	case r.URL.Path == "/popup":
		htmlOut(`<!doctype html><title>POPUP</title><body>popup page</body>`)
	case r.URL.Path == "/oopif":
		htmlOut(fmt.Sprintf(`<!doctype html><title>OOPIF</title><body>
<iframe src="http://localhost:%d/ok?n=frame"></iframe>
<iframe src="http://127.0.0.2:%d/secret?via=iframe"></iframe></body>`, s.port, s.port))
	case r.URL.Path == "/hang":
		htmlOut(`<!doctype html><title>HANG</title><body>hang<script>addEventListener('load',()=>setTimeout(()=>{for(;;);},50))</script></body>`)
	case r.URL.Path == "/big":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>BIG</title><body>`)
		line := strings.Repeat("大页面压测文本 ", 40)
		for i := 0; i < 30*1024*1024/len(line); i++ {
			fmt.Fprintf(w, "<p>%d %s</p>\n", i, line)
		}
	case r.URL.Path == "/ssrf-redirect":
		http.Redirect(w, r, fmt.Sprintf("http://127.0.0.2:%d/secret?via=redirect", s.port), 302)
	case r.URL.Path == "/meta-ssrf":
		htmlOut(fmt.Sprintf(`<!doctype html><title>META</title><meta http-equiv="refresh" content="0;url=http://127.0.0.2:%d/secret?via=meta">`, s.port))
	case r.URL.Path == "/ext":
		htmlOut(`<!doctype html><title>EXT</title><body><img src="http://203.0.113.9/a.png"><script src="http://example.invalid/x.js"></script>ext</body>`)
	case r.URL.Path == "/iso":
		// 用于隔离测试：脚本把 URL 里的 id 写进 window.__id，并回传一次 /iso-ping。
		id := q.Get("id")
		htmlOut(fmt.Sprintf(`<!doctype html><title>ISO %s</title><body><script>window.__id=%q;fetch('/iso-ping?id=%s')</script>iso</body>`, id, id, id))
	case r.URL.Path == "/iso-ping":
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}
