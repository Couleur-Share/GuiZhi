package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type platformMock struct {
	srv       *httptest.Server
	jsonHits  atomic.Int32
	loginPage string
}

func newPlatformMock(t *testing.T) *platformMock {
	t.Helper()
	m := &platformMock{}
	mux := http.NewServeMux()
	hasCookie := func(r *http.Request) bool { c, err := r.Cookie("_t"); return err == nil && c.Value != "" }
	// 登录页：0.6 秒后由页面脚本写入登录 Cookie（模拟用户完成登录）
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><div id="main-outlet">登录页</div><script>setTimeout(function(){document.cookie="_t=abc123; path=/; max-age=86400"},600)</script></body></html>`)
	})
	// 永不登录的页面：用来验证取消
	mux.HandleFunc("/never", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><p>等待</p></body></html>`)
	})
	mux.HandleFunc("/t/1.json", func(w http.ResponseWriter, r *http.Request) {
		m.jsonHits.Add(1)
		if !hasCookie(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":1,"post_stream":{"posts":[{"id":9}]}}`)
	})
	mux.HandleFunc("/t/text.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>not json</body></html>`)
	})
	mux.HandleFunc("/note", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><h1 id="t">笔记正文</h1><script>fetch('/api/data').then(function(r){return r.json()}).then(function(d){document.title=d.title})</script></body></html>`)
	})
	mux.HandleFunc("/expired", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><script>fetch('/api/expired')</script></body></html>`)
	})
	mux.HandleFunc("/api/expired", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":10001,"msg":"未登录"}`)
	})
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"title":"来自接口","comments":[
			{"cid":"c1","text":"第一条","digg_count":5,"create_time":1700000000,"user":{"nickname":"甲"},"replies":[{"cid":"r1","text":"回复","digg_count":99}]},
			{"cid":"c2","text":"第二条","digg_count":8,"user":{"nickname":"乙"}}]}`)
	})
	mux.HandleFunc("/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body style="height:5000px"><script>fetch('/api/data')</script></body></html>`)
	})
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func newTestPlatformSessions(t *testing.T, m *platformMock, loginPath string) *platformSessions {
	if testing.Short() {
		t.Skip("-short：跳过需要启动浏览器的测试")
	}
	// WebView2 的辅助进程退出后句柄释放有延迟，t.TempDir 的一次性清理会偶发失败，改为带重试的清理。
	dir, err := os.MkdirTemp("", "platform-sessions-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = removeAllRetry(dir, 40, 250*time.Millisecond) })
	s := newPlatformSessions(&Library{}, dir)
	s.window = "offscreen" // 离屏运行，不打扰桌面
	s.loginPoll = 200 * time.Millisecond
	s.policy = platformPolicy{
		allowedPlatformURL: func(_, raw string) bool { return strings.HasPrefix(raw, m.srv.URL+"/") },
		officialFlowURL:    func(_, raw string) bool { return strings.HasPrefix(raw, m.srv.URL+"/") },
		resourceURL: func(_, raw string) bool {
			return strings.HasPrefix(raw, m.srv.URL+"/") || strings.HasPrefix(raw, "about:")
		},
		loginURL: func(string) string { return m.srv.URL + loginPath },
	}
	return s
}

func TestPlatformSessionLifecycle(t *testing.T) {
	m := newPlatformMock(t)
	s := newTestPlatformSessions(t, m, "/login")
	ctx := t.Context()

	if jsGet(s.status("linuxdo"), "loggedIn") != false {
		t.Fatal("初始应未登录")
	}
	// 未登录采集：必须明确要求先登录，且不碰浏览器
	if _, err := s.fetchJSONViaSession(ctx, "linuxdo", m.srv.URL+"/t/1.json"); err == nil {
		t.Log("未登录读取接口：", err)
	}
	st, err := s.login(ctx, "linuxdo", false, "")
	if err != nil {
		t.Fatalf("登录失败：%v", err)
	}
	if jsGet(st, "loggedIn") != true || jsGet(st, "browser") != "embedded" || jsGet(st, "busy") != false {
		t.Fatalf("登录后状态不符：%v", st)
	}
	raw, err := os.ReadFile(filepath.Join(s.baseDir, platformSessionStatusFile))
	if err != nil || !strings.Contains(string(raw), `"linuxdo":true`) {
		t.Fatalf("登录标记未落盘：%s %v", raw, err)
	}

	t.Run("会话跨浏览器重启持久，接口携带 Cookie", func(t *testing.T) {
		v, err := s.fetchJSONViaSession(ctx, "linuxdo", m.srv.URL+"/t/1.json")
		if err != nil {
			t.Fatalf("读取接口失败：%v", err)
		}
		if jsToString(jsGet(v, "id")) != "1" {
			t.Fatalf("内容不符：%v", v)
		}
	})
	t.Run("非 JSON 与错误状态", func(t *testing.T) {
		if _, err := s.fetchJSONViaSession(ctx, "linuxdo", m.srv.URL+"/t/text.json"); err == nil || !strings.Contains(err.Error(), "接口未返回 JSON") {
			t.Fatalf("应拒绝非 JSON：%v", err)
		}
	})
	t.Run("capturePage 返回 HTML 与页面自己的 JSON 响应", func(t *testing.T) {
		page, err := s.capturePage(ctx, "linuxdo", m.srv.URL+"/note")
		if err != nil {
			t.Fatalf("采集失败：%v", err)
		}
		if !strings.Contains(page.HTML, "笔记正文") || len(page.JSONPayloads) != 1 || jsToString(jsGet(page.JSONPayloads[0], "title")) != "来自接口" {
			t.Fatalf("采集结果不符：%d 个 JSON，html=%.120s", len(page.JSONPayloads), page.HTML)
		}
	})
	t.Run("登录失效的接口响应会清除登录标记", func(t *testing.T) {
		_, err := s.capturePage(ctx, "linuxdo", m.srv.URL+"/expired")
		if !isPlatformErr(err, "login_required") {
			t.Fatalf("应报 login_required：%v", err)
		}
		if jsGet(s.status("linuxdo"), "loggedIn") != false {
			t.Fatal("登录标记应被清除")
		}
		s.writeLoginState("linuxdo", true) // 还原，供后续用例
	})
	t.Run("评论补采", func(t *testing.T) {
		cs, err := s.captureComments(ctx, "linuxdo", m.srv.URL+"/comments", 10)
		if err != nil {
			t.Fatalf("评论采集失败：%v", err)
		}
		if len(cs) != 2 || cs[0].ExternalID != "c2" || cs[1].ExternalID != "c1" || cs[1].AuthorName != "甲" || cs[1].PublishedAt == nil || *cs[1].PublishedAt != 1700000000000 {
			t.Fatalf("评论不符：%+v", cs)
		}
	})
	t.Run("非平台地址被拒绝", func(t *testing.T) {
		if _, err := s.capturePage(ctx, "linuxdo", "https://example.com/x"); err == nil || err.Error() != "不支持的平台地址" {
			t.Fatalf("应拒绝：%v", err)
		}
	})
	t.Run("退出登录清空会话", func(t *testing.T) {
		if err := s.logout(ctx, "linuxdo"); err != nil {
			t.Fatal(err)
		}
		if jsGet(s.status("linuxdo"), "loggedIn") != false {
			t.Fatal("退出后应未登录")
		}
		if _, err := os.Stat(s.profileDir("linuxdo")); !os.IsNotExist(err) {
			t.Fatalf("会话目录应被清空：%v", err)
		}
		if _, err := s.fetchJSONViaSession(ctx, "linuxdo", m.srv.URL+"/t/1.json"); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
			t.Fatalf("清空后接口应 403：%v", err)
		}
	})
	t.Run("清除全部数据", func(t *testing.T) {
		if err := s.clearAll(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(s.baseDir); !os.IsNotExist(err) {
			t.Fatal("目录应被删除")
		}
	})
}

func TestPlatformLoginCancel(t *testing.T) {
	m := newPlatformMock(t)
	s := newTestPlatformSessions(t, m, "/never")
	done := make(chan error, 1)
	go func() {
		_, err := s.login(t.Context(), "linuxdo", false, "")
		done <- err
	}()
	deadline := time.Now().Add(30 * time.Second)
	for !jsTruthyBusy(s) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond) // 让登录循环跑起来
	if !s.cancelForPlatform("login", "linuxdo") {
		t.Fatal("应能取消进行中的登录")
	}
	select {
	case err := <-done:
		if !isPlatformErr(err, "canceled") {
			t.Fatalf("应报 canceled：%v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("取消后登录未退出")
	}
	if s.cancelForPlatform("login", "linuxdo") {
		t.Fatal("没有进行中的操作时应返回 false")
	}
}

func TestPlatformLoginTimeout(t *testing.T) {
	m := newPlatformMock(t)
	s := newTestPlatformSessions(t, m, "/never")
	s.loginTimeout = 1500 * time.Millisecond
	if _, err := s.login(t.Context(), "linuxdo", false, ""); !isPlatformErr(err, "login_timeout") {
		t.Fatalf("应报 login_timeout：%v", err)
	}
}

func jsTruthyBusy(s *platformSessions) bool { return jsGet(s.status("linuxdo"), "busy") == true }

func TestPlatformPolicyBlocksOutsideRequests(t *testing.T) {
	// 生产策略：未登录流程的域名一律拦截；登录窗口不加载媒体
	cases := []struct {
		platform, url string
		resource      bool
	}{
		{"douyin", "https://www.douyin.com/video/1", true},
		{"douyin", "https://evil.example.com/x.js", false},
		{"douyin", "https://lf-ucenter-web.yhgfb-cn-static.com/a.js", true},
		{"douyin", "http://www.douyin.com/", false},
		{"xiaohongshu", "https://sns-img.xhscdn.com/a.jpg", true},
		{"xiaohongshu", "https://tracker.example.com/", false},
		{"nodeseek", "https://challenges.cloudflare.com/turnstile/v0/api.js", true},
		{"nodeseek", "https://www.nodeseek.com:8443/x", false},
		{"linuxdo", "https://cdn.linux.do/a.png", true},
		{"linuxdo", "http://linux.do/", false},
		{"douyin", "data:image/png;base64,AA", true},
	}
	for _, c := range cases {
		if got := isAllowedBrowserResourceURL(c.platform, c.url); got != c.resource {
			t.Errorf("%s %s 期望 %v", c.platform, c.url, c.resource)
		}
	}
	if !shouldBlockLoginPageRequest("douyin", "https://p3.douyinpic.com/a.jpg", "image") ||
		!shouldBlockLoginPageRequest("xiaohongshu", "https://x.xhscdn.com/v.mp4", "media") ||
		!shouldBlockLoginPageRequest("douyin", "https://www.douyin.com/aweme/v1/web/tab/feed/?a=1", "xhr") ||
		shouldBlockLoginPageRequest("xiaohongshu", "https://x.xhscdn.com/qr.png", "image") {
		t.Error("登录页请求拦截规则不符")
	}
}

func TestProxyArgs(t *testing.T) {
	if got := platformProxyArgs(networkProxySettings{Mode: "direct"}); got != "--no-proxy-server" {
		t.Errorf("direct：%q", got)
	}
	if got := platformProxyArgs(networkProxySettings{Mode: "system"}); got != "" {
		t.Errorf("system：%q", got)
	}
	got := platformProxyArgs(normalizeNetworkProxy(map[string]any{"mode": "manual", "protocol": "http", "host": "127.0.0.1", "port": float64(7890), "username": "u", "password": "secret", "bypass": "<local>,localhost"}))
	if !strings.Contains(got, `--proxy-server="http://127.0.0.1:7890"`) || !strings.Contains(got, `--proxy-bypass-list="<local>;localhost"`) || strings.Contains(got, "secret") {
		t.Errorf("manual：%q", got)
	}
}
