package main

// CrawlService 行为测试：本地 httptest 站点 + 真实批次库/采集/提取/入库。
// 覆盖 robots 跳过、sitemap 候选与范围过滤、深度与页数上限、逐页入库与导入任务记录、
// 重复策略（skip / update）、暂停与取消、robots 不可用时暂停来源、crawl:* 与 web:adopt 的 IPC 形状。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type crawlSite struct {
	*webTestServer
	mu     sync.Mutex
	suffix string // 追加到页面正文，用来模拟原文更新
	robots int    // 非 0 时 robots.txt 返回该状态码
}

func crawlArticle(title, body string, links ...string) string {
	var as strings.Builder
	for _, l := range links {
		fmt.Fprintf(&as, `<p>参见 <a href="%s">%s</a>。</p>`, l, l)
	}
	para := strings.Repeat(body+"，", 8)
	return fmt.Sprintf(`<!doctype html><html><head><title>%s</title></head><body><main><article><h1>%s</h1><p>%s</p><p>%s</p>%s</article></main></body></html>`, title, title, para, para, as.String())
}

func newCrawlSite(t *testing.T) *crawlSite {
	t.Helper()
	cs := &crawlSite{}
	mux := http.NewServeMux()
	page := func(path, title, body string, links ...string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			cs.mu.Lock()
			extra := cs.suffix
			cs.mu.Unlock()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, crawlArticle(title, body+extra, links...))
		})
	}
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		cs.mu.Lock()
		status := cs.robots
		cs.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		fmt.Fprintf(w, "User-agent: *\nDisallow: /docs/guide/secret\nSitemap: http://%s/sitemap.xml\n", r.Host)
	})
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<?xml version="1.0"?><urlset>
<url><loc>http://%[1]s/docs/guide/b</loc></url>
<url><loc>http://%[1]s/docs/guide/manual.pdf</loc></url>
<url><loc>http://%[1]s/other/x</loc></url>
<url><loc>http://%[1]s/docs/private/s</loc></url></urlset>`, r.Host)
	})
	page("/docs/guide/index", "指南首页", "归知指南首页内容", "/docs/guide/a", "/docs/guide/c", "/docs/guide/secret", "/other/z")
	page("/docs/guide/a", "指南A", "归知指南第一篇内容", "/docs/guide/d")
	page("/docs/guide/b", "指南B", "归知指南第二篇内容")
	page("/docs/guide/c", "指南C", "归知指南第三篇内容")
	page("/docs/guide/d", "指南D", "归知指南第四篇内容")
	page("/docs/guide/secret", "私有", "归知私有页面内容")
	page("/other/z", "范围外", "归知范围外页面内容")
	mux.HandleFunc("/slow/page", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(20 * time.Second):
		}
	})
	cs.webTestServer = &webTestServer{hits: map[string]int{}}
	cs.fx = newFetchFixture(t, mux)
	cs.base = "http://site.test:" + cs.fx.port()
	return cs
}

type crawlEnv struct {
	t    *testing.T
	l    *Library
	b    *Bridge
	site *crawlSite
}

func newCrawlEnv(t *testing.T) *crawlEnv {
	t.Helper()
	_, l := loadGolden(t)
	site := newCrawlSite(t)
	l.webFetcher = site.fx.f
	b := &Bridge{handlers: map[string]handler{}}
	l.registerCrawl(b)
	t.Cleanup(func() { l.crawl.close(context.Background()) })
	return &crawlEnv{t: t, l: l, b: b, site: site}
}

func (e *crawlEnv) call(channel string, args ...any) map[string]any {
	e.t.Helper()
	raw := make([]json.RawMessage, len(args))
	for i, a := range args {
		b, err := json.Marshal(a)
		if err != nil {
			e.t.Fatal(err)
		}
		raw[i] = b
	}
	out, err := e.b.handlers[channel](context.Background(), raw)
	if err != nil {
		e.t.Fatalf("%s: %v", channel, err)
	}
	return toGeneric(e.t, out).(map[string]any)
}

func (e *crawlEnv) create(input map[string]any) string {
	e.t.Helper()
	res := e.call("crawl:create", input)
	if res["ok"] != true {
		e.t.Fatalf("crawl:create 失败: %v", res)
	}
	return res["data"].(string)
}

func (e *crawlEnv) wait(id string, want ...string) map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		data := e.call("crawl:get", id)["data"].(map[string]any)
		job, _ := data["job"].(map[string]any)
		for _, w := range want {
			if job != nil && job["status"] == w {
				// 等调度循环真正退出，页面与批次状态才稳定
				e.l.crawl.mu.Lock()
				_, running := e.l.crawl.running[id]
				e.l.crawl.mu.Unlock()
				if !running {
					return data
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	e.t.Fatalf("批次 %s 未在时限内进入 %v", id, want)
	return nil
}

func pageStatuses(data map[string]any) map[string]string {
	out := map[string]string{}
	for _, p := range data["pages"].([]any) {
		m := p.(map[string]any)
		out[m["url"].(string)] = m["status"].(string)
	}
	return out
}

func (e *crawlEnv) docsInput(extra map[string]any) map[string]any {
	in := map[string]any{
		"purpose":  "documents",
		"maxDepth": 2,
		"seeds":    []any{map[string]any{"mode": "directory", "url": e.site.base + "/docs/guide/index"}},
	}
	for k, v := range extra {
		in[k] = v
	}
	return in
}

func (e *crawlEnv) query(sql string, args ...any) [][]any {
	e.t.Helper()
	rows, err := e.l.db.Query(context.Background(), sql, args...)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows.Values
}

func TestCrawlServiceDirectoryBatch(t *testing.T) {
	e := newCrawlEnv(t)
	base := e.site.base
	id := e.create(e.docsInput(nil))
	data := e.wait(id, "completed")
	got := pageStatuses(data)
	want := map[string]string{
		base + "/docs/guide/index":  "added",
		base + "/docs/guide/a":      "added",
		base + "/docs/guide/b":      "added", // 来自 sitemap
		base + "/docs/guide/c":      "added",
		base + "/docs/guide/d":      "added", // 深度 2
		base + "/docs/guide/secret": "skipped",
	}
	// 范围外、非网页后缀、robots 禁止的 sitemap 项都不应进入批次
	for u, s := range got {
		if u == base+"/docs/guide/secret" {
			continue
		}
		if want[u] != s {
			t.Errorf("页面 %s 状态 %s，期望 %q", u, s, want[u])
		}
	}
	for u, s := range want {
		if u == base+"/docs/guide/secret" {
			continue
		}
		if got[u] != s {
			t.Errorf("缺少页面 %s（%q）", u, s)
		}
	}
	if s, ok := got[base+"/docs/guide/secret"]; !ok || s != "skipped" {
		t.Errorf("robots 禁止的页面应 skipped，实际 %q（存在=%v）", s, ok)
	}
	for _, bad := range []string{"/other/z", "/other/x", "/docs/guide/manual.pdf", "/docs/private/s"} {
		if _, ok := got[base+bad]; ok {
			t.Errorf("不应发现 %s", bad)
		}
	}
	job := data["job"].(map[string]any)
	if counts := job["counts"].(map[string]any); counts["added"] != float64(5) || counts["skipped"] != float64(1) {
		t.Errorf("计数不符: %v", counts)
	}
	for _, p := range data["pages"].([]any) {
		m := p.(map[string]any)
		if m["status"] == "skipped" {
			if m["error"] != "robots.txt 禁止访问" {
				t.Errorf("skipped 原因: %v", m["error"])
			}
			continue
		}
		if m["itemId"] == nil || m["importTaskId"] == nil || m["result"] == nil {
			t.Errorf("已入库页面缺字段: %v", m)
		}
	}
	// 入库：条目、来源记录、网页原文版本、导入任务、全文索引
	if n := e.query("SELECT COUNT(*) FROM knowledge_items WHERE item_type='webpage' AND title LIKE '指南%'")[0][0]; asInt(n) != 5 {
		t.Errorf("网页条目数 %v", n)
	}
	if n := e.query("SELECT COUNT(*) FROM source_records s JOIN knowledge_items i ON i.id=s.item_id WHERE i.title LIKE '指南%' AND s.source_type='url'")[0][0]; asInt(n) != 5 {
		t.Errorf("来源记录数 %v", n)
	}
	if n := e.query("SELECT COUNT(*) FROM web_source_baselines b JOIN knowledge_items i ON i.id=b.item_id WHERE i.title LIKE '指南%'")[0][0]; asInt(n) != 5 {
		t.Errorf("基线数 %v", n)
	}
	tasks := e.query("SELECT source_kind, status, item_type, display_name, result_item_id FROM import_tasks WHERE display_name LIKE '指南%'")
	if len(tasks) != 5 {
		t.Fatalf("导入任务数 %d", len(tasks))
	}
	for _, r := range tasks {
		if r[0] != "url" || r[1] != "completed" || r[2] != "webpage" || r[4] == nil {
			t.Errorf("导入任务不符: %v", r)
		}
	}
	if n := e.query("SELECT COUNT(*) FROM knowledge_fts WHERE knowledge_fts MATCH ?", `"`+SegmentTextForFts("归知指南第三篇内容")+`"`)[0][0]; asInt(n) == 0 {
		t.Errorf("FTS 应检索到新条目")
	}

	// 同一批次再来一遍（skip）：全部 duplicate，不新增条目
	id2 := e.create(e.docsInput(map[string]any{"duplicatePolicy": "skip"}))
	got2 := pageStatuses(e.wait(id2, "completed"))
	for u, s := range got2 {
		if u != base+"/docs/guide/secret" && s != "duplicate" {
			t.Errorf("skip 重复页 %s 状态 %s", u, s)
		}
	}
	if n := e.query("SELECT COUNT(*) FROM knowledge_items WHERE item_type='webpage' AND title LIKE '指南%'")[0][0]; asInt(n) != 5 {
		t.Errorf("skip 后条目数 %v", n)
	}
	if n := e.query("SELECT COUNT(*) FROM import_tasks WHERE status='duplicate' AND duplicate_item_id IS NOT NULL AND display_name LIKE '指南%'")[0][0]; asInt(n) != 5 {
		t.Errorf("duplicate 任务数 %v", n)
	}

	// update 策略：原文变化的页面更新正文，且留下 local 快照版本；向量/Wiki 记录被清
	e.site.mu.Lock()
	e.site.suffix = "，新增一段更新内容"
	e.site.mu.Unlock()
	id3 := e.create(e.docsInput(map[string]any{"duplicatePolicy": "update"}))
	got3 := pageStatuses(e.wait(id3, "completed"))
	for u, s := range got3 {
		if u != base+"/docs/guide/secret" && s != "updated" {
			t.Errorf("update 页 %s 状态 %s", u, s)
		}
	}
	if n := e.query("SELECT COUNT(*) FROM knowledge_items WHERE title='指南A' AND content LIKE '%新增一段更新内容%'")[0][0]; asInt(n) != 1 {
		t.Errorf("正文应已更新")
	}
	if n := e.query("SELECT COUNT(*) FROM web_source_versions v JOIN knowledge_items i ON i.id=v.item_id WHERE i.title='指南A' AND v.payload LIKE '%\"kind\":\"local\"%'")[0][0]; asInt(n) != 1 {
		t.Errorf("应留下一个 local 快照")
	}
	// 再次 update：原文没变 → unchanged
	id4 := e.create(e.docsInput(map[string]any{"duplicatePolicy": "update"}))
	for u, s := range pageStatuses(e.wait(id4, "completed")) {
		if u != base+"/docs/guide/secret" && s != "unchanged" {
			t.Errorf("无变化页 %s 状态 %s", u, s)
		}
	}
}

func TestCrawlServiceLimits(t *testing.T) {
	e := newCrawlEnv(t)
	base := e.site.base
	// 页数上限：入口 + 3 个链接最多 2 页
	id := e.create(e.docsInput(map[string]any{"maxPages": 2}))
	data := e.wait(id, "completed")
	if got := pageStatuses(data); len(got) != 2 {
		t.Errorf("页数上限未生效: %v", got)
	}
	// 深度 0：只采入口
	id = e.create(map[string]any{"purpose": "documents", "maxDepth": 0, "seeds": []any{map[string]any{"mode": "page", "url": base + "/docs/guide/d"}}})
	if got := pageStatuses(e.wait(id, "completed")); len(got) != 1 || got[base+"/docs/guide/d"] != "added" {
		t.Errorf("深度 0: %v", got)
	}
	// 单页模式即使深度 >0 也不跟链接（没有 scope）
	id = e.create(map[string]any{"purpose": "documents", "maxDepth": 2, "seeds": []any{map[string]any{"mode": "page", "url": base + "/docs/guide/c"}}})
	if got := pageStatuses(e.wait(id, "completed")); len(got) != 1 {
		t.Errorf("单页模式: %v", got)
	}
}

func TestCrawlServiceRobotsUnavailablePausesJob(t *testing.T) {
	e := newCrawlEnv(t)
	e.site.mu.Lock()
	e.site.robots = 503
	e.site.mu.Unlock()
	id := e.create(e.docsInput(nil))
	data := e.wait(id, "paused")
	job := data["job"].(map[string]any)
	if job["error"] != "robots.txt 读取失败（HTTP 503），已暂停来源" {
		t.Errorf("暂停原因: %v", job["error"])
	}
	// robots 恢复后可继续
	e.site.mu.Lock()
	e.site.robots = 0
	e.site.mu.Unlock()
	if res := e.call("crawl:resume", id); res["ok"] != true {
		t.Fatalf("resume: %v", res)
	}
	data = e.wait(id, "completed")
	if pageStatuses(data)[e.site.base+"/docs/guide/a"] != "added" {
		t.Errorf("恢复后应完成采集: %v", pageStatuses(data))
	}
}

func TestCrawlServicePauseCancelAndRetry(t *testing.T) {
	e := newCrawlEnv(t)
	base := e.site.base
	id := e.create(map[string]any{"purpose": "documents", "maxDepth": 0, "seeds": []any{map[string]any{"mode": "page", "url": base + "/slow/page"}}})
	// 等页面进入 running 再暂停
	deadline := time.Now().Add(10 * time.Second)
	for {
		data := e.call("crawl:get", id)["data"].(map[string]any)
		if pageStatuses(data)[base+"/slow/page"] == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("页面未进入 running")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if res := e.call("crawl:pause", id); res["ok"] != true {
		t.Fatalf("pause: %v", res)
	}
	data := e.wait(id, "paused")
	var page map[string]any
	for _, p := range data["pages"].([]any) {
		page = p.(map[string]any)
	}
	if page["status"] != "pending" || page["error"] != crawlAbortMessage {
		t.Errorf("暂停后页面应回到 pending 并带中断原因: %v", page)
	}
	// 取消：状态 canceled，待处理页变 canceled，且不可再 resume
	if res := e.call("crawl:cancel", id); res["ok"] != true {
		t.Fatalf("cancel: %v", res)
	}
	data = e.call("crawl:get", id)["data"].(map[string]any)
	if data["job"].(map[string]any)["status"] != "canceled" || pageStatuses(data)[base+"/slow/page"] != "canceled" {
		t.Errorf("取消后状态: %v", data)
	}
	if res := e.call("crawl:resume", id); res["ok"] != false || res["error"] != "批次不存在或已经取消" {
		t.Errorf("已取消不可恢复: %v", res)
	}

	// retry：失败页回到队列并重新运行
	id = e.create(map[string]any{"purpose": "documents", "maxDepth": 0, "seeds": []any{map[string]any{"mode": "page", "url": base + "/nope-404"}}})
	data = e.wait(id, "completed")
	if got := pageStatuses(data); got[base+"/nope-404"] != "failed" {
		t.Fatalf("404 页应 failed: %v", got)
	}
	if res := e.call("crawl:retry", id); res["ok"] != true {
		t.Fatalf("retry: %v", res)
	}
	data = e.wait(id, "completed")
	if got := pageStatuses(data); got[base+"/nope-404"] != "failed" {
		t.Errorf("重试后仍应失败（站点未变）: %v", got)
	}
}

func TestCrawlIPCValidation(t *testing.T) {
	e := newCrawlEnv(t)
	for _, c := range []struct {
		name  string
		input any
		want  string
	}{
		{"research", map[string]any{"purpose": "research", "seeds": []any{}}, "网页研究批次必须通过研究流程创建"},
		{"nil", nil, "网页研究批次必须通过研究流程创建"},
		{"missing-collection", e.docsInput(map[string]any{"collectionId": "no-such-collection"}), "目标集合不存在，请重新选择"},
		{"bad-seeds", map[string]any{"purpose": "documents", "seeds": []any{}}, ""},
	} {
		res := e.call("crawl:create", c.input)
		if res["ok"] != false {
			t.Errorf("%s: 应失败: %v", c.name, res)
			continue
		}
		if c.want != "" && res["error"] != c.want {
			t.Errorf("%s: 错误 %v，期望 %q", c.name, res["error"], c.want)
		}
	}
	for _, ch := range []string{"crawl:get", "crawl:pause", "crawl:resume", "crawl:cancel", "crawl:retry"} {
		for _, bad := range []any{nil, "", 5, strings.Repeat("x", 101)} {
			if res := e.call(ch, bad); res["ok"] != false || res["error"] != "记录 ID 无效" {
				t.Errorf("%s(%v): %v", ch, bad, res)
			}
		}
	}
	if res := e.call("crawl:get", "missing"); res["ok"] != true || res["data"].(map[string]any)["job"] != nil {
		t.Errorf("get 缺失批次: %v", res)
	}
	if res := e.call("crawl:pause", "missing"); res["error"] != "采集批次不存在" {
		t.Errorf("pause 缺失批次: %v", res)
	}
	if res := e.call("crawl:list"); res["ok"] != true {
		t.Errorf("list: %v", res)
	}
	for _, in := range []any{nil, map[string]any{}, map[string]any{"itemId": "a", "versionId": "b", "expectedContentHash": "short", "expectedTitle": "t"}, map[string]any{"itemId": "a", "versionId": "b", "expectedContentHash": strings.Repeat("a", 64)}} {
		res := e.call("web:adopt", in)
		if res["ok"] != false {
			t.Errorf("web:adopt(%v) 应失败: %v", in, res)
		}
	}
	if res := e.call("web:adopt", map[string]any{"itemId": "a", "versionId": "b", "expectedContentHash": strings.Repeat("a", 64), "expectedTitle": "t"}); res["error"] != "条目或完整版本不存在" {
		t.Errorf("web:adopt 缺失: %v", res)
	}
}

// 启动恢复：中断的批次标 interrupted，运行中的页放回队列。
func TestCrawlRecoverOnRegister(t *testing.T) {
	_, l := loadGolden(t)
	ctx := context.Background()
	job, err := l.crawlJobCreate(ctx, mustJS(t, `{"purpose":"documents","seeds":[{"mode":"page","url":"https://a.test/x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.crawlJobSetStatus(ctx, job.ID, "running", nil); err != nil {
		t.Fatal(err)
	}
	pages, _ := l.crawlPages(ctx, job.ID)
	pages[0].Status = "running"
	if err := l.crawlSavePage(ctx, pages[0]); err != nil {
		t.Fatal(err)
	}
	l.registerCrawl(&Bridge{handlers: map[string]handler{}})
	got, _ := l.crawlJobGet(ctx, job.ID)
	pages, _ = l.crawlPages(ctx, job.ID)
	if got.Status != "interrupted" || pages[0].Status != "pending" {
		t.Errorf("恢复后: job=%s page=%s", got.Status, pages[0].Status)
	}
}

func mustJS(t *testing.T, s string) any {
	t.Helper()
	v, err := jsParse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
