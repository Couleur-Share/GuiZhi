package main

// 本文问答联网查证的手写测试：搜索服务（Tavily / AnySearch）、查证编排、IPC 通道。
// 全部使用本地 httptest 与注入替身，不访问真实搜索服务。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func withSearchEndpoint(t *testing.T, provider, url string) {
	t.Helper()
	old := readingSearchEndpoints[provider]
	readingSearchEndpoints[provider] = url
	t.Cleanup(func() { readingSearchEndpoints[provider] = old })
}

func newSearchLib(t *testing.T) *Library {
	t.Helper()
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	l.searchHTTP = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return l
}

func artParse(t *testing.T, s string) any {
	t.Helper()
	v, err := jsParse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTavilySearchRequestAndParse(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"results":[
			{"url":"https://a.example/1","title":"  甲 ","raw_content":"` + strings.Repeat("文", 70000) + `","published_date":"2026-01-02"},
			{"url":"http://127.0.0.1/x","title":"内网"},
			{"url":"https://b.example/2"},
			null,
			{"url":"https://c.example/3","title":"丙"},{"url":"https://d.example/4"},{"url":"https://e.example/5"},{"url":"https://f.example/6"}]}`))
	}))
	defer srv.Close()
	withSearchEndpoint(t, "tavily", srv.URL)
	l := newSearchLib(t)
	if _, err := l.configureReadingSearch(context.Background(), artParse(t, `{"provider":"tavily","apiKey":" sk-test "}`)); err != nil {
		t.Fatal(err)
	}
	res, err := l.searchReadingWeb(context.Background(), "  量子计算  ", "qa")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("Authorization=%q", gotAuth)
	}
	if gotBody["query"] != "量子计算" || gotBody["include_raw_content"] != "markdown" || gotBody["max_results"] != float64(5) {
		t.Fatalf("请求体 %v", gotBody)
	}
	if len(res) != 5 {
		t.Fatalf("应限制 5 条（内网地址被滤掉）: %d", len(res))
	}
	if res[0].Title != "甲 " && res[0].Title != "  甲 " {
		t.Logf("title=%q", res[0].Title)
	}
	if res[0].Text == nil || utf16Len(*res[0].Text) != 60000 {
		t.Fatalf("正文应截到 60000 码元")
	}
	if res[1].Title != "参考网页" {
		t.Fatalf("缺标题应兜底: %q", res[1].Title)
	}
	if res[0].PublishedAt == nil || *res[0].PublishedAt != "2026-01-02" {
		t.Fatal("publishedAt 丢失")
	}
}

func TestAnySearchParseAndErrors(t *testing.T) {
	status := 200
	body := `{"code":0,"data":{"results":[{"url":"https://a.example/","title":"A","content":"正文"}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	withSearchEndpoint(t, "anysearch", srv.URL)
	l := newSearchLib(t)
	if _, err := l.configureReadingSearch(context.Background(), artParse(t, `{"provider":"anysearch","apiKey":"k"}`)); err != nil {
		t.Fatal(err)
	}
	res, err := l.searchReadingWeb(context.Background(), "q", "qa")
	if err != nil || len(res) != 1 || res[0].Text == nil || *res[0].Text != "正文" {
		t.Fatalf("res=%v err=%v", res, err)
	}
	body = `{"code":1}`
	if _, err = l.searchReadingWeb(context.Background(), "q", "qa"); err == nil || !strings.Contains(err.Error(), "AnySearch 搜索未成功") {
		t.Fatalf("err=%v", err)
	}
	// 上游错误正文不回显
	status, body = 401, `{"secret":"上游错误正文 sk-leak"}`
	_, err = l.searchReadingWeb(context.Background(), "q", "qa")
	if err == nil || strings.Contains(err.Error(), "sk-leak") || !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "密钥无效") {
		t.Fatalf("err=%v", err)
	}
	status, body = 500, "x"
	if _, err = l.searchReadingWeb(context.Background(), "q", "qa"); err == nil || !strings.Contains(err.Error(), "请稍后重试") {
		t.Fatalf("err=%v", err)
	}
	status, body = 200, "not json"
	if _, err = l.searchReadingWeb(context.Background(), "q", "qa"); err == nil || err.Error() != "搜索响应格式无效" {
		t.Fatalf("err=%v", err)
	}
}

func TestSearchRedirectNotFollowedAndNoKey(t *testing.T) {
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()
	withSearchEndpoint(t, "tavily", srv.URL)
	l := newSearchLib(t)
	if _, err := l.searchReadingWeb(context.Background(), "q", "qa"); err == nil || !strings.Contains(err.Error(), "请先在设置中配置 Tavily") {
		t.Fatalf("未配置密钥应提示: %v", err)
	}
	if _, err := l.configureReadingSearch(context.Background(), artParse(t, `{"apiKey":"k"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := l.searchReadingWeb(context.Background(), "q", "qa"); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("重定向应按错误处理: %v", err)
	}
	if hits != 0 {
		t.Fatal("不应跟随重定向")
	}
}

func TestReadingSearchConfigPersistsEncrypted(t *testing.T) {
	l := newSearchLib(t)
	st, err := l.configureReadingSearch(context.Background(), artParse(t, `{"provider":"anysearch","apiKey":"sk-secret-123","defaultEnabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Configured || st.Provider != "anysearch" || !st.DefaultEnabled {
		t.Fatalf("status=%+v", st)
	}
	if readingSearchPersistent() {
		rawB, err := os.ReadFile(l.readingSearchFile())
		raw := string(rawB)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "sk-secret-123") {
			t.Fatal("密钥不应明文落盘")
		}
	}
	// 新实例读回同一份配置
	l2 := newSearchLib(t)
	l2.paths = l.paths
	st2, err := l2.readingSearchStatus()
	if err != nil || st2.Provider != "anysearch" || !st2.Configured {
		t.Fatalf("读回 %+v %v", st2, err)
	}
	// 清空密钥
	if st, err = l.configureReadingSearch(context.Background(), artParse(t, `{"apiKey":"  "}`)); err != nil || st.Configured {
		t.Fatalf("清空后 %+v %v", st, err)
	}
	// 参数校验
	for in, want := range map[string]string{`[]`: "搜索配置格式无效", `{"provider":"x"}`: "搜索服务无效", `{"defaultEnabled":1}`: "搜索默认开关无效",
		`{"test":"y"}`: "搜索测试选项无效", `{"apiKey":"a\nb"}`: "搜索密钥格式无效", `{"provider":"native","apiKey":"k"}`: "原生搜索复用主文本模型凭证，请在模型服务中管理密钥"} {
		if _, err := l.configureReadingSearch(context.Background(), artParse(t, in)); err == nil || err.Error() != want {
			t.Fatalf("%s: err=%v want %s", in, err, want)
		}
	}
}

func TestSearchArticleWebOrchestration(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	long := strings.Repeat("可靠资料。", 80)
	var captured []string
	l.articleAsk.search = func(ctx context.Context, query, scenario string) ([]readingSearchResult, error) {
		if scenario != "qa" {
			t.Fatalf("scenario=%s", scenario)
		}
		if query == "boom" {
			return nil, context.DeadlineExceeded
		}
		s := func(v string) *string { return &v }
		return []readingSearchResult{
			{Title: "长文", URL: "https://a.example/1#x", Text: &long},
			{Title: "重复", URL: "https://a.example/1#y", Text: s("忽略")},
			{Title: "短文需补抓", URL: "https://b.example/2", Text: s("短")},
			{Title: "补抓失败", URL: "https://c.example/3"},
			{Title: "补抓太短", URL: "https://d.example/4"},
			{Title: "超出补抓额度", URL: "https://e.example/5"},
		}, nil
	}
	l.articleAsk.capture = func(ctx context.Context, req webCaptureRequest) webCaptureResult {
		captured = append(captured, req.URL)
		if req.Purpose != "research" {
			t.Fatalf("purpose=%s", req.Purpose)
		}
		switch req.URL {
		case "https://b.example/2":
			return webCaptureResult{Complete: true, Markdown: long}
		case "https://c.example/3":
			return webCaptureResult{Complete: false}
		}
		return webCaptureResult{Complete: true, Markdown: "还是太短"}
	}
	out, err := l.searchArticleWeb(context.Background(), []string{"量子", "boom"})
	if err != nil {
		t.Fatal(err)
	}
	if len(captured) != 3 {
		t.Fatalf("补抓上限 3: %v", captured)
	}
	if len(out.Sources) != 2 || out.Sources[0].URL != "https://a.example/1" || out.Sources[1].URL != "https://b.example/2" {
		t.Fatalf("sources=%+v", out.Sources)
	}
	if out.Sources[0].Kind != "web" || out.Sources[0].CapturedAt == nil {
		t.Fatalf("来源字段 %+v", out.Sources[0])
	}
	want := []string{"context deadline exceeded", "部分网页正文未能取得", "部分搜索结果缺少有效正文，未作为依据"}
	for _, w := range want {
		found := false
		for _, g := range out.Warnings {
			found = found || g == w
		}
		if !found {
			t.Fatalf("缺少警告 %q: %v", w, out.Warnings)
		}
	}
}

func TestSearchArticleWebNoSourcesWarning(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	l.articleAsk.search = func(ctx context.Context, q, s string) ([]readingSearchResult, error) { return nil, nil }
	out, err := l.searchArticleWeb(context.Background(), []string{"q"})
	if err != nil || len(out.Sources) != 0 || len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "未取得有效联网资料") {
		t.Fatalf("%+v %v", out, err)
	}
}

func artCall(t *testing.T, b *Bridge, ch string, arg any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(arg)
	out, err := b.handlers[ch](context.Background(), []json.RawMessage{raw})
	if err != nil {
		t.Fatal(err)
	}
	return toGeneric(t, out).(map[string]any)
}

func TestArticleAskIPCValidationAndCancel(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	b := &Bridge{handlers: map[string]handler{}}
	l.registerArticleAsk(b)
	started := make(chan struct{}, 1)
	l.articleAsk.search = func(ctx context.Context, q, s string) ([]readingSearchResult, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	bad := func(arg any, msg string) {
		t.Helper()
		r := artCall(t, b, chArticleAskSearch, arg)
		if r["success"] != false || r["error"] != msg {
			t.Fatalf("%v → %v", arg, r)
		}
	}
	bad(map[string]any{"requestId": "bad id!", "queries": []string{"a"}}, "请求标识无效")
	bad(map[string]any{"queries": []string{"a"}}, "请求标识无效")
	bad(map[string]any{"requestId": "r1", "queries": []string{}}, "搜索词无效")
	bad(map[string]any{"requestId": "r1", "queries": []string{"a", "b", "c"}}, "搜索词无效")
	bad(map[string]any{"requestId": "r1", "queries": []string{"  "}}, "搜索词无效")
	bad(map[string]any{"requestId": "r1", "queries": []string{strings.Repeat("长", 401)}}, "搜索词无效")
	bad(map[string]any{"requestId": "r1", "queries": []any{1}}, "搜索词无效")
	if r := artCall(t, b, chArticleAskCancel, "bad id"); r["success"] != false || r["error"] != "请求标识无效" {
		t.Fatalf("cancel 校验 %v", r)
	}
	if r := artCall(t, b, chArticleAskCancel, "nobody"); r["success"] != true {
		t.Fatalf("取消不存在的请求应成功: %v", r)
	}

	var wg sync.WaitGroup
	var first map[string]any
	wg.Add(1)
	go func() {
		defer wg.Done()
		first = artCall(t, b, chArticleAskSearch, map[string]any{"requestId": "run-1", "queries": []string{"a", "a"}})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("搜索未启动")
	}
	bad(map[string]any{"requestId": "run-2", "queries": []string{"b"}}, "已有本文搜索正在执行，请稍后重试")
	artCall(t, b, chArticleAskCancel, "run-1")
	wg.Wait()
	if first["success"] != false || first["error"] != "搜索已取消" {
		t.Fatalf("取消后应报搜索已取消: %v", first)
	}
	// 取消后可再次发起
	l.articleAsk.search = func(ctx context.Context, q, s string) ([]readingSearchResult, error) { return nil, nil }
	if r := artCall(t, b, chArticleAskSearch, map[string]any{"requestId": "run-3", "queries": []string{"c"}}); r["success"] != true {
		t.Fatalf("取消后应释放占用: %v", r)
	}
}

func TestArticleAskContextIPC(t *testing.T) {
	_, l := loadGolden(t)
	b := &Bridge{handlers: map[string]handler{}}
	l.registerArticleAsk(b)
	ctx := context.Background()
	if _, err := l.db.Execute(ctx, "INSERT INTO knowledge_items (id,title,content,item_type,created_at,updated_at) VALUES ('ipc-1','标题','正文内容','note',1,1)"); err != nil {
		t.Fatal(err)
	}
	ok := artCall(t, b, chArticleAskContext, map[string]any{"target": map[string]any{"itemId": "ipc-1", "view": "body"}, "question": "问"})
	if ok["success"] != true || ok["context"].(map[string]any)["title"] != "标题" {
		t.Fatalf("%v", ok)
	}
	fail := artCall(t, b, chArticleAskContext, map[string]any{"target": map[string]any{"itemId": "none", "view": "body"}, "question": "问"})
	if fail["success"] != false || fail["error"] != "文章不存在或已删除，请先恢复文章" {
		t.Fatalf("%v", fail)
	}
}

func TestRequestNativeSearchSSE(t *testing.T) {
	var gotBody map[string]any
	var gotAuth, gotAccept string
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotAccept = r.Header.Get("Authorization"), r.Header.Get("Accept")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(status)
		if status != 200 {
			_, _ = w.Write([]byte("上游错误正文 sk-leak"))
			return
		}
		completed := `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":12,"output_tokens":3},"output":[` +
			`{"type":"web_search_call","status":"completed","action":{"type":"search","sources":[{"type":"url","url":"https://n.example/1","title":"来源"}]}}]}}`
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\"}\n\ndata: " + completed + "\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	l := newSearchLib(t)
	cfg := aiClientConfig{Provider: "openai", APIProtocol: "openai", APIURL: srv.URL, APIKey: "sk-main", Model: "gpt-x"}
	resp, err := l.requestNativeSearch(context.Background(), srv.URL+"/v1/responses", cfg, "  查询词 ")
	if err != nil {
		t.Fatal(err)
	}
	res, err := nativeSearchResults(resp)
	if err != nil || len(res) != 1 || res[0].Title != "来源" {
		t.Fatalf("res=%v err=%v", res, err)
	}
	if gotAuth != "Bearer sk-main" || gotAccept != "text/event-stream" || gotBody["input"] != "查询词" || gotBody["tool_choice"] != "required" || gotBody["stream"] != true {
		t.Fatalf("请求 auth=%q accept=%q body=%v", gotAuth, gotAccept, gotBody)
	}
	status = 404
	_, err = l.requestNativeSearch(context.Background(), srv.URL, cfg, "q")
	if err == nil || strings.Contains(err.Error(), "sk-leak") || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("err=%v", err)
	}
}

func TestThemedReadingSearchConfigIPC(t *testing.T) {
	l := newSearchLib(t)
	b := &Bridge{handlers: map[string]handler{}}
	l.registerArticleAsk(b)
	r := artCall(t, b, "themedReading:searchConfig", map[string]any{"provider": "tavily", "apiKey": "k", "defaultEnabled": true})
	s, _ := r["search"].(map[string]any)
	if r["success"] != true || s["configured"] != true || s["defaultEnabled"] != true || s["provider"] != "tavily" {
		t.Fatalf("%v", r)
	}
	r = artCall(t, b, "themedReading:searchConfig", map[string]any{"provider": "bogus"})
	if r["success"] != false || r["error"] != "搜索服务无效" {
		t.Fatalf("%v", r)
	}
}
