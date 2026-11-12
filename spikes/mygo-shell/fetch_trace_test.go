package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 请求跟踪：每一跳一行 JSON，Cookie 只记名字，URL 里疑似机密的查询值打码。
func TestFetchTraceRecordsHopsAndMasksSecrets(t *testing.T) {
	var fx *fetchFixture
	fx = newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			w.Header().Add("Set-Cookie", "guestJs=NEWSECRET; Path=/")
			w.Header().Add("Set-Cookie", "lastvisit=123; Path=/")
			w.Header().Set("Server", "gate-1")
			http.Redirect(w, r, "/b?access_token=LEAKME&tid=5", http.StatusFound)
		case "/b":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "ok")
		case "/bad":
			http.Error(w, "x", http.StatusInternalServerError)
		}
	}))
	var buf bytes.Buffer
	fx.f.trace = newFetchTracer(&buf)
	base := "http://site.test:" + fx.port()
	_, err := fx.f.fetchRawText(context.Background(), base+"/a?token=SECRET9&tid=5", safeRequestOptions{
		UserAgent: "UA-X", Referer: "https://ref.test/", Cookie: "guestJs=COOKIEVAL; ngaPassportUid=77",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fx.f.fetchRawText(context.Background(), base+"/bad", safeRequestOptions{}, 0)

	raw := buf.String()
	for _, leak := range []string{"SECRET9", "LEAKME", "COOKIEVAL", "77", "NEWSECRET"} {
		if strings.Contains(raw, leak) {
			t.Errorf("跟踪日志不应含 %q：\n%s", leak, raw)
		}
	}
	var entries []fetchTraceEntry
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		var e fetchTraceEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("不是合法 JSON 行 %q：%v", line, err)
		}
		entries = append(entries, e)
	}
	if len(entries) != 3 {
		t.Fatalf("应有 3 条（两跳 + 一次失败），实际 %d：\n%s", len(entries), raw)
	}
	a, b, bad := entries[0], entries[1], entries[2]
	if a.Hop != 0 || a.Status != 302 || a.Seq != 1 || !strings.HasSuffix(a.Location, "/b?access_token=***&tid=5") {
		t.Errorf("第一跳不符：%+v", a)
	}
	// 响应侧只记名字：重定向跳上的 Set-Cookie（NGA 自重定向循环的关键线索）、Server 与响应头名
	if strings.Join(a.SetCookieNames, ",") != "guestJs,lastvisit" || a.Server != "gate-1" {
		t.Errorf("应记录 Set-Cookie 名与 Server：%+v", a)
	}
	if !strings.Contains(strings.Join(a.ResponseHeaders, ","), "Server") {
		t.Errorf("应记录响应头名：%v", a.ResponseHeaders)
	}
	if !strings.Contains(a.URL, "token=***") || !strings.Contains(a.URL, "tid=5") {
		t.Errorf("URL 应保留 tid、打码 token：%q", a.URL)
	}
	if b.Hop != 1 || b.Status != 200 || b.Seq != 1 || b.ContentType != "text/plain" {
		t.Errorf("第二跳不符：%+v", b)
	}
	if b.Request["User-Agent"] != "UA-X" || b.Request["Referer"] != "https://ref.test/" {
		t.Errorf("应记录 UA 与 Referer：%+v", b.Request)
	}
	if b.Request["Cookie"] != "[names: guestJs, ngaPassportUid]" {
		t.Errorf("Cookie 只应记名字：%q", b.Request["Cookie"])
	}
	if bad.Seq != 2 || bad.Status != 500 || bad.Hop != 0 {
		t.Errorf("失败请求不符：%+v", bad)
	}
	if a.KeepAlive {
		t.Errorf("Go 版抓取层不复用连接，应记录 keepAlive=false")
	}
}

func TestFetchTraceRecordsTransportErrors(t *testing.T) {
	fx := newFetchFixture(t, http.NotFoundHandler())
	var buf bytes.Buffer
	fx.f.trace = newFetchTracer(&buf)
	fx.srv.Close() // 拨号必然失败
	_, err := fx.f.fetchRawText(context.Background(), "http://site.test:"+fx.port()+"/x", safeRequestOptions{}, 0)
	if err == nil {
		t.Fatal("应报错")
	}
	var e fetchTraceEntry
	if jerr := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &e); jerr != nil {
		t.Fatalf("应记录一条错误：%q %v", buf.String(), jerr)
	}
	if e.Status != 0 || e.Error == "" {
		t.Errorf("传输错误应记录 error 且无状态码：%+v", e)
	}
}

// 限速：两次请求起点之间至少间隔 minInterval，等待可被取消。
func TestFetchPaceSpacesRequestsAndCancels(t *testing.T) {
	fx := newFetchFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	}))
	fx.f.minInterval = 150 * time.Millisecond
	url := "http://site.test:" + fx.port() + "/"
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := fx.f.fetchRawText(context.Background(), url, safeRequestOptions{}, 0); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 300*time.Millisecond {
		t.Errorf("3 次请求应至少耗时 300ms（2 个间隔），实际 %v", d)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := fx.f.fetchRawText(ctx, url, safeRequestOptions{}, 0)
	if err == nil || err.Error() != "已取消" {
		t.Errorf("等待间隔期间取消应返回「已取消」：%v", err)
	}
}

func TestFetchEnvConfig(t *testing.T) {
	cases := map[string]time.Duration{"": 0, "0": 0, "abc": 0, "-5": 0, "250": 250 * time.Millisecond, " 1500 ": 1500 * time.Millisecond}
	for in, want := range cases {
		if got := parseFetchMinInterval(in); got != want {
			t.Errorf("parseFetchMinInterval(%q) = %v，期望 %v", in, got, want)
		}
	}
}
