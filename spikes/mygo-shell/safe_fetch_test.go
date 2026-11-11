package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// fetchFixture：把 site.test 解析成一个“公网”地址，拨号时转到本地 httptest 服务器；记录实际拨号地址。
type fetchFixture struct {
	srv    *httptest.Server
	f      *safeFetcher
	dialed []string
}

func newFetchFixture(t *testing.T, h http.Handler) *fetchFixture {
	t.Helper()
	fx := &fetchFixture{srv: httptest.NewServer(h)}
	t.Cleanup(fx.srv.Close)
	target := fx.srv.Listener.Addr().String()
	fx.f = &safeFetcher{
		proxy: newNetProxyState(),
		lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			switch host {
			case "site.test", "other.test":
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			case "inner.test":
				return []netip.Addr{netip.MustParseAddr("10.1.2.3")}, nil
			case "fake.test":
				return []netip.Addr{netip.MustParseAddr("198.18.0.9")}, nil
			}
			return nil, fmt.Errorf("getaddrinfo ENOTFOUND %s", host)
		},
		dialOverride: func(ctx context.Context, network, addr string) (net.Conn, error) {
			fx.dialed = append(fx.dialed, addr)
			return (&net.Dialer{}).DialContext(ctx, network, target)
		},
		idle: 2 * time.Second,
	}
	return fx
}

func (fx *fetchFixture) port() string {
	_, p, _ := net.SplitHostPort(fx.srv.Listener.Addr().String())
	return p
}

func TestFetchHTMLPinsAddressAndSendsHeaders(t *testing.T) {
	var gotHost, gotUA, gotLang, gotRef string
	fx := newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost, gotUA, gotLang, gotRef = r.Host, r.Header.Get("User-Agent"), r.Header.Get("Accept-Language"), r.Header.Get("Referer")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body>你好</body></html>")
	}))
	res, err := fx.f.fetchHTML(context.Background(), "http://site.test:"+fx.port()+"/a?b=1#frag", safeRequestOptions{Referer: "http://r.test/"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.HTML, "你好") || res.FinalURL != "http://site.test:"+fx.port()+"/a?b=1#frag" {
		t.Fatalf("结果不符: %+v", res)
	}
	if gotHost != "site.test:"+fx.port() || gotUA != fetchDefaultUA || gotLang == "" || gotRef != "http://r.test/" {
		t.Errorf("请求头不符: host=%q ua=%q lang=%q ref=%q", gotHost, gotUA, gotLang, gotRef)
	}
	if len(fx.dialed) != 1 || fx.dialed[0] != "93.184.216.34:"+fx.port() {
		t.Errorf("应钉扎到解析出的 IP，实际拨号 %v", fx.dialed)
	}
}

func TestFetchBlocksInternalTargets(t *testing.T) {
	fx := newFetchFixture(t, http.NotFoundHandler())
	t.Cleanup(func() { activeNetProxy.Apply(nil) })
	ctx := context.Background()
	cases := map[string]string{
		"http://127.0.0.1:1/":     "不允许访问内网地址",
		"http://localhost/":       "不允许访问本地网络地址",
		"http://a.localhost/":     "不允许访问本地网络地址",
		"http://[::1]/":           "不允许访问内网地址",
		"http://10.0.0.1/":        "不允许访问内网地址",
		"http://169.254.169.254/": "不允许访问内网地址",
		"http://inner.test/":      "不允许访问内网地址",
		"http://fake.test/":       "fake-ip",
		"http://nonexist.test/":   "ENOTFOUND",
		"ftp://site.test/":        "不支持的协议",
		"not a url":               "Invalid URL",
	}
	for u, want := range cases {
		// fake.test：没有任何代理配置时（direct）198.18 是真实内网
		if u == "http://fake.test/" {
			fx.f.proxy.Apply(map[string]any{"mode": "direct"})
		}
		_, err := fx.f.fetchHTML(ctx, u, safeRequestOptions{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s：期望含 %q，实际 %v", u, want, err)
		}
	}
	if len(fx.dialed) != 0 {
		t.Errorf("被拦截的目标不应发生拨号: %v", fx.dialed)
	}
}

func TestFetchFakeIPAllowedOnlyWithProxyConfigured(t *testing.T) {
	fx := newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "ok")
	}))
	fx.f.proxy.Apply(map[string]any{"mode": "direct"})
	if _, err := fx.f.fetchHTML(context.Background(), "http://fake.test:"+fx.port()+"/", safeRequestOptions{}); err == nil {
		t.Fatal("direct 模式下 198.18/15 应被拒")
	}
	fx.f.proxy.Apply(map[string]any{"mode": "system"})
	t.Cleanup(func() { activeNetProxy.Apply(nil) })
	if res, err := fx.f.fetchHTML(context.Background(), "http://fake.test:"+fx.port()+"/", safeRequestOptions{}); err != nil || res.HTML != "ok" {
		t.Fatalf("system 模式（存在代理配置）应放行 fake-ip: %v %+v", err, res)
	}
}

func TestFetchRedirectsAreRevalidated(t *testing.T) {
	hops := 0
	var fx *fetchFixture
	fx = newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-internal":
			http.Redirect(w, r, "http://10.0.0.5/secret", http.StatusFound)
		case "/to-other":
			http.Redirect(w, r, "http://other.test:"+fx.port()+"/final", http.StatusMovedPermanently)
		case "/loop":
			hops++
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/final":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "final")
		}
	}))
	ctx := context.Background()
	if _, err := fx.f.fetchHTML(ctx, "http://site.test:"+fx.port()+"/to-internal", safeRequestOptions{}); err == nil || !strings.Contains(err.Error(), "不允许访问内网地址") {
		t.Errorf("重定向到内网应被拦: %v", err)
	}
	res, err := fx.f.fetchHTML(ctx, "http://site.test:"+fx.port()+"/to-other", safeRequestOptions{})
	if err != nil || res.HTML != "final" || res.FinalURL != "http://other.test:"+fx.port()+"/final" {
		t.Errorf("跨主机重定向: %v %+v", err, res)
	}
	if _, err := fx.f.fetchHTML(ctx, "http://site.test:"+fx.port()+"/loop", safeRequestOptions{}); err == nil || err.Error() != "重定向次数过多" {
		t.Errorf("循环重定向: %v", err)
	}
	if hops != fetchMaxRedirect+1 {
		t.Errorf("应在第 %d 次请求后放弃，实际 %d", fetchMaxRedirect+1, hops)
	}
	if final, err := fx.f.resolveRedirectURL(ctx, "http://site.test:"+fx.port()+"/to-other"); err != nil || final != "http://other.test:"+fx.port()+"/final" {
		t.Errorf("resolveRedirectURL: %v %q", err, final)
	}
}

func TestFetchStatusTypeAndSizeErrors(t *testing.T) {
	fx := newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/404":
			http.NotFound(w, r)
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"a":1}`)
		case "/big":
			w.Header().Set("Content-Type", "text/html")
			w.Write(make([]byte, fetchHTMLMax+10))
		}
	}))
	ctx := context.Background()
	base := "http://site.test:" + fx.port()
	for path, want := range map[string]string{"/404": "HTTP 404", "/json": "不是网页内容: application/json", "/big": "页面超过大小上限"} {
		if _, err := fx.f.fetchHTML(ctx, base+path, safeRequestOptions{}); err == nil || err.Error() != want {
			t.Errorf("%s：期望 %q，实际 %v", path, want, err)
		}
	}
}

func TestFetchTextDecoding(t *testing.T) {
	gbk := func(s string) []byte { b, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(s)); return b }
	fx := newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/header":
			w.Header().Set("Content-Type", "text/html; charset=gb2312")
			w.Write(gbk("<p>中文内容</p>"))
		case "/meta":
			w.Header().Set("Content-Type", "text/html")
			w.Write(append([]byte(`<meta charset="gbk">`), gbk("<p>元标签</p>")...))
		case "/unknown":
			w.Header().Set("Content-Type", "text/html; charset=x-no-such")
			fmt.Fprint(w, "回退 UTF-8")
		}
	}))
	ctx := context.Background()
	base := "http://site.test:" + fx.port()
	for path, want := range map[string]string{"/header": "中文内容", "/meta": "元标签", "/unknown": "回退 UTF-8"} {
		res, err := fx.f.fetchHTML(ctx, base+path, safeRequestOptions{})
		if err != nil || !strings.Contains(res.HTML, want) {
			t.Errorf("%s：%v %q", path, err, res.HTML)
		}
	}
}

func TestFetchRawTextAndJSON(t *testing.T) {
	fx := newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/challenge":
			w.Header().Add("Set-Cookie", "guestJs=abc; Path=/; HttpOnly")
			w.Header().Add("Set-Cookie", "bad")
			w.WriteHeader(403)
			fmt.Fprint(w, "challenge body")
		case "/json":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"ok":true}`)
		case "/badjson":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{oops`)
		case "/htmlwall":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html>login</html>")
		}
	}))
	ctx := context.Background()
	base := "http://site.test:" + fx.port()
	if _, err := fx.f.fetchRawText(ctx, base+"/challenge", safeRequestOptions{}, 0); err == nil || err.Error() != "HTTP 403" {
		t.Errorf("默认应拒 403: %v", err)
	}
	r, err := fx.f.fetchRawText(ctx, base+"/challenge", safeRequestOptions{AcceptStatuses: []int{403}}, 0)
	if err != nil || r.Status != 403 || r.Text != "challenge body" || len(r.SetCookies) != 1 || r.SetCookies[0] != "guestJs=abc" {
		t.Errorf("acceptStatuses=403: %v %+v", err, r)
	}
	if b, err := fx.f.fetchJSON(ctx, base+"/json", safeRequestOptions{}); err != nil || string(b) != `{"ok":true}` {
		t.Errorf("fetchJSON: %v %s", err, b)
	}
	if _, err := fx.f.fetchJSON(ctx, base+"/badjson", safeRequestOptions{}); err == nil || err.Error() != "接口响应不是合法的 JSON" {
		t.Errorf("非法 JSON: %v", err)
	}
	if _, err := fx.f.fetchJSON(ctx, base+"/htmlwall", safeRequestOptions{}); err == nil || err.Error() != "接口未返回 JSON: text/html" {
		t.Errorf("HTML 墙: %v", err)
	}
}

func TestFetchCancelAndIdleTimeout(t *testing.T) {
	release := make(chan struct{})
	fx := newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer close(release)
	base := "http://site.test:" + fx.port() + "/"

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	if _, err := fx.f.fetchHTML(ctx, base, safeRequestOptions{}); err == nil || err.Error() != "已取消" {
		t.Errorf("取消: %v", err)
	}

	fx.f.idle = 200 * time.Millisecond
	start := time.Now()
	if _, err := fx.f.fetchHTML(context.Background(), base, safeRequestOptions{}); err == nil || err.Error() != "请求超时" {
		t.Errorf("空闲超时: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("空闲超时耗时过长: %v", d)
	}
}

func TestDownloadToTempFile(t *testing.T) {
	fx := newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media":
			w.Header().Set("Content-Type", "video/mp4")
			w.Write([]byte(strings.Repeat("x", 1000)))
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html></html>")
		}
	}))
	ctx := context.Background()
	base := "http://site.test:" + fx.port()
	dir, file, err := fx.f.downloadToTempFile(ctx, base+"/media", "v.mp4", 2000, safeRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(file); err != nil || st.Size() != 1000 {
		t.Errorf("下载文件: %v %v", st, err)
	}
	os.RemoveAll(dir)
	for _, c := range []struct {
		path, want string
		max        int64
	}{
		{"/media", "文件超过大小上限", 500},
		{"/page", "下载地址返回的是网页而非媒体文件（可能已失效）", 2000},
	} {
		dir, _, err := fx.f.downloadToTempFile(ctx, base+c.path, "v.mp4", c.max, safeRequestOptions{})
		if err == nil || err.Error() != c.want {
			t.Errorf("%s：期望 %q，实际 %v", c.path, c.want, err)
		}
		if dir != "" {
			t.Errorf("失败时不应返回目录")
		}
	}
	entries, _ := os.ReadDir(os.TempDir())
	_ = entries // 临时目录清理由实现保证；这里只确认失败路径返回空目录
}

func TestFetchViaManualProxy(t *testing.T) {
	var proxyHits atomic.Int32
	var lastTarget, lastAuth string
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		lastTarget, lastAuth = r.URL.String(), r.Header.Get("Proxy-Authorization")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "via-proxy")
	}))
	defer proxySrv.Close()
	pu, _ := url.Parse(proxySrv.URL)
	var lookups atomic.Int32
	f := &safeFetcher{proxy: newNetProxyState(), idle: 2 * time.Second,
		lookup: func(context.Context, string) ([]netip.Addr, error) {
			lookups.Add(1)
			return nil, errors.New("走代理时不应本地解析")
		}}
	t.Cleanup(func() { activeNetProxy.Apply(nil) })
	f.proxy.Apply(map[string]any{"mode": "manual", "protocol": "http", "host": pu.Hostname(), "port": pu.Port(), "username": "u", "password": "p", "bypass": "bypass.test"})

	res, err := f.fetchHTML(context.Background(), "http://remote.test/page?x=1", safeRequestOptions{})
	if err != nil || res.HTML != "via-proxy" {
		t.Fatalf("走代理: %v %+v", err, res)
	}
	if lastTarget != "http://remote.test/page?x=1" || lastAuth == "" {
		t.Errorf("代理应收到绝对 URI 与凭据: %q %q", lastTarget, lastAuth)
	}
	if lookups.Load() != 0 {
		t.Errorf("走代理不应做本地 DNS 解析")
	}
	// 字面私网 IP 即使走代理也要拦
	if _, err := f.fetchHTML(context.Background(), "http://192.168.1.1/", safeRequestOptions{}); err == nil || !strings.Contains(err.Error(), "不允许访问内网地址") {
		t.Errorf("代理下字面内网 IP 应被拦: %v", err)
	}
	// 绕过规则命中的主机直连（此处走本地解析，解析器报错即证明没经代理）
	hitsBefore := proxyHits.Load()
	if _, err := f.fetchHTML(context.Background(), "http://bypass.test/", safeRequestOptions{}); err == nil || proxyHits.Load() != hitsBefore || lookups.Load() != 1 {
		t.Errorf("绕过主机应直连并本地解析: err=%v hits=%d lookups=%d", err, proxyHits.Load(), lookups.Load())
	}
}

func TestProxyEnvironmentFollowsSettings(t *testing.T) {
	s := newNetProxyState()
	t.Cleanup(func() { s.Apply(nil) })
	t.Setenv("HTTP_PROXY", "http://orig.example:1")
	os.Unsetenv("NO_PROXY")
	s.Apply(map[string]any{"mode": "manual", "host": "10.9.8.7", "port": 3128.0})
	if os.Getenv("HTTPS_PROXY") != "http://10.9.8.7:3128" || os.Getenv("http_proxy") != "http://10.9.8.7:3128" || os.Getenv("NO_PROXY") != defaultNetworkProxy.Bypass {
		t.Errorf("手动模式应写入代理环境变量: %q %q", os.Getenv("HTTPS_PROXY"), os.Getenv("NO_PROXY"))
	}
	s.Apply(map[string]any{"mode": "direct"})
	if os.Getenv("HTTP_PROXY") != "http://orig.example:1" || os.Getenv("HTTPS_PROXY") != "" {
		t.Errorf("退出手动模式应还原原值: %q %q", os.Getenv("HTTP_PROXY"), os.Getenv("HTTPS_PROXY"))
	}
}
