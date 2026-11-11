package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const webTestArticle = `<!doctype html><html><head><title>静态文章</title>
<meta property="article:published_time" content="2024-05-01T12:00:00Z"><meta name="author" content="张三"></head>
<body><header><nav><a href="/">首页</a></nav></header><main><article><h1>静态文章</h1>
<p>归知是本地优先的个人知识库，归知是本地优先的个人知识库，归知是本地优先的个人知识库，归知是本地优先的个人知识库，归知是本地优先的个人知识库，归知是本地优先的个人知识库。</p>
<p>参见 <a href="/other?x=1#h">另一篇</a>。</p></article></main><footer>页脚</footer></body></html>`

type webTestServer struct {
	fx    *fetchFixture
	l     *Library
	base  string
	mu    sync.Mutex
	heads []http.Header
	hits  map[string]int
}

func newWebTestServer(t *testing.T) *webTestServer {
	t.Helper()
	ws := &webTestServer{hits: map[string]int{}}
	mux := http.NewServeMux()
	handle := func(path string, h http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			ws.mu.Lock()
			ws.heads = append(ws.heads, r.Header.Clone())
			ws.hits[path]++
			ws.mu.Unlock()
			h(w, r)
		})
	}
	html := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body)
	}
	handle("/article", func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Write([]byte(webTestArticle))
		zw.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(buf.Bytes())
	})
	handle("/redir", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/article#frag", http.StatusFound) })
	handle("/loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop", http.StatusFound) })
	handle("/docs/in", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere/page", http.StatusFound) })
	handle("/gbk", func(w http.ResponseWriter, r *http.Request) {
		b, _ := simplifiedchinese.GBK.NewEncoder().String(strings.Replace(webTestArticle, "静态文章", "国标文章", -1))
		w.Header().Set("Content-Type", "text/html; charset=gbk")
		fmt.Fprint(w, b)
	})
	handle("/bad-utf8", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><p>\xff\xfe坏字节</p></body></html>"))
	})
	handle("/pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4"))
	})
	handle("/spa", func(w http.ResponseWriter, r *http.Request) {
		html(w, `<html><head><title>App</title><script src="/app.js"></script></head><body><div id="root"></div></body></html>`)
	})
	handle("/forbidden", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		html(w, `<html><head><title>Forbidden</title></head><body><p>无权访问</p></body></html>`)
	})
	handle("/captcha", func(w http.ResponseWriter, r *http.Request) {
		html(w, strings.Replace(webTestArticle, "静态文章", "Just a moment...", 1))
	})
	handle("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	ws.fx = newFetchFixture(t, mux)
	ws.l = &Library{webFetcher: ws.fx.f}
	ws.base = "http://site.test:" + ws.fx.port()
	return ws
}

func (ws *webTestServer) capture(path string, mod func(*webCaptureRequest)) webCaptureResult {
	req := webCaptureRequest{TaskID: "t1", Purpose: "import", URL: ws.base + path}
	if mod != nil {
		mod(&req)
	}
	return ws.l.captureWebPage(context.Background(), req, nil)
}

func TestWebCaptureStaticArticle(t *testing.T) {
	ws := newWebTestServer(t)
	var stages []string
	r := ws.l.captureWebPage(context.Background(), webCaptureRequest{TaskID: "t1", Purpose: "import", URL: ws.base + "/redir#x"}, func(s string) { stages = append(stages, s) })
	if r.Error != nil {
		t.Fatalf("不应失败: %+v", r.Error)
	}
	if r.Title != "静态文章" || r.Author != "张三" || r.PublishedAt == nil || *r.PublishedAt != 1714564800000 || r.DateConfidence != "exact" {
		t.Errorf("元数据不符: %+v", r)
	}
	if r.EntryURL != ws.base+"/redir" || r.FinalURL != ws.base+"/article" {
		t.Errorf("入口/最终网址不符: %s %s", r.EntryURL, r.FinalURL)
	}
	if r.EngineVersion != "readability/1-static" || !r.Complete || r.TaskID != "t1" || r.CapturedAt == 0 {
		t.Errorf("结果标记不符: %+v", r)
	}
	if !strings.Contains(r.Markdown, "归知是本地优先的个人知识库") || strings.Contains(r.Markdown, "页脚") || strings.Contains(r.Markdown, "首页") {
		t.Errorf("正文不符: %q", r.Markdown)
	}
	wantLinks := []string{ws.base + "/", ws.base + "/other?x=1#h"}
	if fmt.Sprint(r.Links) != fmt.Sprint(wantLinks) {
		t.Errorf("链接 %v != %v", r.Links, wantLinks)
	}
	if fmt.Sprint(stages) != "[fetching extracting]" {
		t.Errorf("阶段 %v", stages)
	}
	// 身份：自报家门、无 Accept-Language、gzip/deflate、文档 Accept
	h := ws.heads[len(ws.heads)-1]
	if !strings.Contains(h.Get("User-Agent"), "GuiZhi/") || h.Get("Accept-Language") != "" || h.Get("Accept-Encoding") != "gzip, deflate" || h.Get("Accept") != webDocumentAccept {
		t.Errorf("请求头不符: %v", h)
	}
}

func TestWebCaptureCharsetAndFallbacks(t *testing.T) {
	ws := newWebTestServer(t)
	if r := ws.capture("/gbk", nil); r.Error != nil || r.Title != "国标文章" {
		t.Errorf("gbk 页面: %+v", r)
	}
	// 非法 UTF-8 → 转渲染；渲染进程未接入时如实失败
	r := ws.capture("/bad-utf8", nil)
	if r.Error == nil || r.Error.Code != "empty" || !strings.Contains(r.Error.Message, "渲染") {
		t.Errorf("坏字节应转渲染并如实失败: %+v", r.Error)
	}
	// 需要渲染的单页应用：同样
	if r := ws.capture("/spa", nil); r.Error == nil || !strings.Contains(r.Error.Message, "渲染") {
		t.Errorf("单页应用: %+v", r.Error)
	}
}

type fakeRenderer struct {
	calls atomic.Int32
	page  *renderedWebPage
	err   error
}

func (f *fakeRenderer) Render(ctx context.Context, req webCaptureRequest) (*renderedWebPage, error) {
	f.calls.Add(1)
	return f.page, f.err
}

func TestWebCaptureRendersWhenStaticInsufficient(t *testing.T) {
	ws := newWebTestServer(t)
	fr := &fakeRenderer{page: &renderedWebPage{HTML: webTestArticle, URL: ws.base + "/spa", Status: 200, Links: []string{ws.base + "/x"}}}
	ws.l.webRenderer = fr
	r := ws.capture("/spa", nil)
	if r.Error != nil || r.EngineVersion != "readability/1-electron" || r.Title != "静态文章" || len(r.Links) != 1 {
		t.Errorf("渲染回退: %+v", r)
	}
	if fr.calls.Load() != 1 {
		t.Errorf("渲染次数 %d", fr.calls.Load())
	}
	// 渲染失败要如实带出
	fr.err = fmt.Errorf("渲染进程崩溃")
	if r := ws.capture("/spa", nil); r.Error == nil || !strings.Contains(r.Error.Message, "渲染进程崩溃") {
		t.Errorf("渲染失败: %+v", r.Error)
	}
}

func TestWebCaptureErrors(t *testing.T) {
	ws := newWebTestServer(t)
	cases := []struct {
		name, path, code, msg string
		mod                   func(*webCaptureRequest)
	}{
		{name: "403", path: "/forbidden", code: "restricted", msg: "网站拒绝访问"},
		{name: "pdf", path: "/pdf", code: "incomplete", msg: "入口返回非网页内容"},
		{name: "loop", path: "/loop", code: "network", msg: "网页重定向超过 5 次"},
		{name: "scope", path: "/docs/in", code: "security", msg: "重定向超出目录范围", mod: func(r *webCaptureRequest) {
			s, _ := newWebScope(r.URL, nil)
			r.Scope = &s
		}},
		{name: "captcha", path: "/captcha", code: "captcha", msg: webVerificationRequired},
	}
	for _, c := range cases {
		r := ws.capture(c.path, c.mod)
		if r.Error == nil || r.Error.Code != c.code || !strings.Contains(r.Error.Message, c.msg) {
			t.Errorf("%s: %+v", c.name, r.Error)
		}
		if r.Complete || r.Title != ws.base+c.path && r.Title != "Forbidden" && r.Error.Code == "network" {
			t.Errorf("%s: 失败结果标记不符 %+v", c.name, r)
		}
	}
	// 内网目标：安全拦截
	r := ws.l.captureWebPage(context.Background(), webCaptureRequest{URL: "http://inner.test:" + ws.fx.port() + "/x"}, nil)
	if r.Error == nil || r.Error.Code != "security" {
		t.Errorf("内网目标: %+v", r.Error)
	}
	// 非法网址
	if r := ws.l.captureWebPage(context.Background(), webCaptureRequest{URL: "ftp://x.test/"}, nil); r.Error == nil {
		t.Error("ftp 应失败")
	}
	// 交互式验证入口未接入，如实失败并带验证前缀
	r = ws.capture("/article", func(r *webCaptureRequest) { r.InteractiveVerification = true })
	if r.Error == nil || !strings.Contains(r.Error.Message, webVerificationRequired) {
		t.Errorf("交互式验证: %+v", r.Error)
	}
}

func TestWebCaptureCancelAndSerialPerOrigin(t *testing.T) {
	ws := newWebTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	r := ws.l.captureWebPage(ctx, webCaptureRequest{URL: ws.base + "/slow"}, nil)
	if r.Error == nil || (r.Error.Code != "timeout" && r.Error.Code != "canceled") {
		t.Fatalf("超时应带归类: %+v", r.Error)
	}
	// 同源串行：占住入口后第二个等待者随 ctx 放弃，且名额被正确释放
	if err := webGate.enter(context.Background(), "http://o.test"); err != nil {
		t.Fatal(err)
	}
	wctx, wcancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer wcancel()
	if err := webGate.enter(wctx, "http://o.test"); err == nil {
		t.Error("同源应排队直至放弃")
	}
	webGate.leave("http://o.test")
	if err := webGate.enter(context.Background(), "http://o.test"); err != nil {
		t.Errorf("释放后应可进入: %v", err)
	}
	webGate.leave("http://o.test")
	webGate.mu.Lock()
	active := webGate.active
	webGate.mu.Unlock()
	if active != 0 {
		t.Errorf("名额泄漏: %d", active)
	}
}

func TestWebExtractFailureIsReported(t *testing.T) {
	big := strings.Repeat("a", webExtractFrame+1)
	_, err := extractWebBody(context.Background(), big, "https://x.test/", 200)
	if err == nil || !strings.Contains(err.Error(), "16 MiB") {
		t.Errorf("超限输入: %v", err)
	}
}

// 端到端：采集 → 提取 → 入库 → FTS → 网页来源版本/基线。
func TestWebCaptureImportEndToEnd(t *testing.T) {
	_, l := loadGolden(t)
	ws := newWebTestServer(t)
	l.webFetcher = ws.fx.f
	ctx := context.Background()
	task := map[string]any{"id": "task-web-1", "sourceKind": "url", "sourceInput": ws.base + "/article"}
	ex, err := l.defaultExtract(ctx, task, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if ex.DegradedReason != "" || ex.WebCapture == nil || ex.ItemType != "webpage" || ex.SourceURI == nil || *ex.SourceURI != ws.base+"/article" {
		t.Fatalf("抽取产物不符: %+v", ex)
	}
	norm := ws.base + "/article"
	id, err := l.importSaveItem(ctx, importSaveParams{extracted: ex, task: task, normalizedURI: &norm, contentHash: webContentHash(ex.Content)})
	if err != nil {
		t.Fatal(err)
	}
	q := func(sql string, args ...any) [][]any {
		rows, err := l.db.Query(ctx, sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		return rows.Values
	}
	if got := q("SELECT title, item_type FROM knowledge_items WHERE id=?", id); len(got) != 1 || got[0][0] != "静态文章" || got[0][1] != "webpage" {
		t.Errorf("条目: %v", got)
	}
	if got := q("SELECT item_id FROM knowledge_fts WHERE knowledge_fts MATCH ?", `"`+SegmentTextForFts("个人知识库")+`"`); len(got) == 0 {
		t.Errorf("FTS 应能检索到新条目")
	}
	if got := q("SELECT source_type, source_uri, access_uri FROM source_records WHERE item_id=?", id); len(got) != 1 || got[0][0] != "url" || got[0][1] != ws.base+"/article" {
		t.Errorf("来源记录: %v", got)
	}
	vers := q("SELECT payload, captured_at FROM web_source_versions WHERE item_id=?", id)
	if len(vers) != 1 {
		t.Fatalf("版本数 %d", len(vers))
	}
	payload := vers[0][0].(string)
	for _, want := range []string{`"kind":"remote"`, `"engineVersion":"readability/1-static"`, `"complete":true`, `"sourceUrl":"` + ws.base + `/article"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("版本载荷缺 %s: %s", want, payload)
		}
	}
	base := q("SELECT version_id, content_hash, title, remote_hash, summary_stale FROM web_source_baselines WHERE item_id=?", id)
	if len(base) != 1 || base[0][2] != "静态文章" || base[0][1] != webContentHash(ex.Content) || base[0][3] != webContentHash(ex.WebCapture.Markdown) || asInt(base[0][4]) != 0 {
		t.Errorf("基线: %v", base)
	}
}

// 抓取失败时导入侧得到降级原因（不入库空壳）。
func TestWebCaptureDegradedForImport(t *testing.T) {
	ws := newWebTestServer(t)
	task := map[string]any{"id": "t", "sourceKind": "url", "sourceInput": ws.base + "/forbidden"}
	ex, err := ws.l.defaultExtract(context.Background(), task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ex.DegradedReason, "网站拒绝访问") {
		t.Errorf("降级原因: %q", ex.DegradedReason)
	}
	// 视频与论坛已迁移：其分发分别由 video_run_test.go / forum_import_test.go 用注入的替身覆盖，这里不能触发真实联网
}
