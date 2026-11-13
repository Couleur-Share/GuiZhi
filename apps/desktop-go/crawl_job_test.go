package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

type crawlGolden struct {
	Crawl struct {
		Validate []struct {
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
			OK    bool            `json:"ok"`
			V     json.RawMessage `json:"v"`
		} `json:"validate"`
		Ops []struct {
			Op struct {
				Fn   string            `json:"fn"`
				Args []json.RawMessage `json:"args"`
			} `json:"op"`
			Value json.RawMessage `json:"value"`
			Error *string         `json:"error"`
		} `json:"ops"`
		Dump struct {
			Jobs  [][]any `json:"jobs"`
			Pages [][]any `json:"pages"`
		} `json:"dump"`
		Robots struct {
			URLs  []string `json:"urls"`
			Cases []struct {
				Text   string `json:"text"`
				Policy struct {
					Rules []struct {
						Allow bool   `json:"allow"`
						Path  string `json:"path"`
					} `json:"rules"`
					Sitemaps []string `json:"sitemaps"`
				} `json:"policy"`
				Allows []bool `json:"allows"`
			} `json:"cases"`
		} `json:"robots"`
		Sitemaps []struct {
			Text string   `json:"text"`
			Locs []string `json:"locs"`
		} `json:"sitemaps"`
	} `json:"crawl"`
}

func loadCrawlGolden(t *testing.T) crawlGolden {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g crawlGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Crawl.Ops) == 0 {
		t.Fatal("golden 缺少 crawl 段")
	}
	return g
}

func jsonAny(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func goAny(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return jsonAny(t, raw)
}

func TestValidateCrawlInputMatchesTS(t *testing.T) {
	g := loadCrawlGolden(t)
	for _, c := range g.Crawl.Validate {
		in := any(jsUndefined)
		if v, err := jsParse(string(c.Input)); err == nil {
			in = v
		}
		got, err := validateCrawlInput(in)
		if c.OK {
			if err != nil {
				t.Errorf("%s: 不应报错: %v", c.Name, err)
				continue
			}
			if !reflect.DeepEqual(goAny(t, got), jsonAny(t, c.V)) {
				t.Errorf("%s:\n got %v\nwant %s", c.Name, goAny(t, got), c.V)
			}
		} else {
			var want string
			_ = json.Unmarshal(c.V, &want)
			if err == nil || err.Error() != want {
				t.Errorf("%s: 应报错 %q，实际 %v", c.Name, want, err)
			}
		}
	}
}

func TestRobotsMatchesTS(t *testing.T) {
	g := loadCrawlGolden(t)
	for i, c := range g.Crawl.Robots.Cases {
		p := parseRobots(c.Text)
		if len(p.Rules) != len(c.Policy.Rules) || len(p.Sitemaps) != len(c.Policy.Sitemaps) {
			t.Errorf("#%d 规则数不一致: got %d/%d want %d/%d (%q)", i, len(p.Rules), len(p.Sitemaps), len(c.Policy.Rules), len(c.Policy.Sitemaps), c.Text)
			continue
		}
		for j, r := range p.Rules {
			if r.Allow != c.Policy.Rules[j].Allow || r.Path != c.Policy.Rules[j].Path {
				t.Errorf("#%d 规则 %d: %+v != %+v", i, j, r, c.Policy.Rules[j])
			}
		}
		for j, s := range p.Sitemaps {
			if s != c.Policy.Sitemaps[j] {
				t.Errorf("#%d sitemap %d: %q != %q", i, j, s, c.Policy.Sitemaps[j])
			}
		}
		for j, u := range g.Crawl.Robots.URLs {
			if got := robotsAllows(u, p); got != c.Allows[j] {
				t.Errorf("#%d robotsAllows(%s)=%v want %v (%q)", i, u, got, c.Allows[j], c.Text)
			}
		}
	}
}

func TestSitemapLocsMatchTS(t *testing.T) {
	g := loadCrawlGolden(t)
	for i, c := range g.Crawl.Sitemaps {
		got := sitemapLocs(c.Text)
		if len(got) == 0 && len(c.Locs) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.Locs) {
			t.Errorf("#%d sitemap 不一致\n got %q\nwant %q\n文本 %.120q", i, got, c.Locs, c.Text)
		}
	}
}

// ---- CrawlJobDB 操作序列 ----

var reUUID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// maskCrawlIDs 把随机 uuid 按首次出现顺序编号，把当前时间戳抹成 NOW。
func maskCrawlIDs(t *testing.T, v any) string {
	t.Helper()
	var mask func(v any) any
	mask = func(v any) any {
		switch x := v.(type) {
		case float64:
			if x >= nowThreshold {
				return "NOW"
			}
		case []any:
			for i := range x {
				x[i] = mask(x[i])
			}
		case map[string]any:
			for k := range x {
				x[k] = mask(x[k])
			}
		}
		return v
	}
	raw, err := json.Marshal(mask(v))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	return reUUID.ReplaceAllStringFunc(string(raw), func(id string) string {
		if n, ok := seen[id]; ok {
			return n
		}
		n := fmt.Sprintf("UUID#%d", len(seen)+1)
		seen[id] = n
		return n
	})
}

// parsePayloadColumns 把转储行里的 JSON 文本列展开成对象，免得键序差异影响比较。
func parsePayloadColumns(t *testing.T, rows [][]any, cols ...int) []any {
	t.Helper()
	out := make([]any, len(rows))
	for i, r := range rows {
		row := append([]any{}, r...)
		for _, c := range cols {
			if s, ok := row[c].(string); ok {
				var v any
				if err := json.Unmarshal([]byte(s), &v); err != nil {
					t.Fatalf("payload 不是 JSON: %v (%s)", err, s)
				}
				row[c] = v
			}
		}
		out[i] = row
	}
	return out
}

func TestCrawlJobDBMatchesTS(t *testing.T) {
	g := loadCrawlGolden(t)
	_, l := loadGolden(t)
	ctx := context.Background()
	for _, tbl := range []string{"crawl_pages", "crawl_jobs"} {
		if _, err := l.db.Execute(ctx, "DELETE FROM "+tbl); err != nil {
			t.Fatal(err)
		}
	}
	var jobIDs []string
	resolve := func(raw json.RawMessage) any {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		var walk func(v any) any
		walk = func(v any) any {
			switch x := v.(type) {
			case []any:
				for i := range x {
					x[i] = walk(x[i])
				}
			case map[string]any:
				if n, ok := x["$job"]; ok {
					i := int(n.(float64))
					if i < len(jobIDs) {
						return jobIDs[i]
					}
					return "missing-job"
				}
				if p, ok := x["$page"]; ok {
					a := p.([]any)
					pages, _ := l.crawlPages(ctx, jobIDs[int(a[0].(float64))])
					return pages[int(a[1].(float64))].ID
				}
				if p, ok := x["$pageUpdate"]; ok {
					a := p.([]any)
					pages, _ := l.crawlPages(ctx, jobIDs[int(a[0].(float64))])
					page := pages[int(a[1].(float64))]
					raw, _ := json.Marshal(page)
					var m map[string]any
					_ = json.Unmarshal(raw, &m)
					for k, e := range a[2].(map[string]any) {
						m[k] = e
					}
					return m
				}
				for k := range x {
					x[k] = walk(x[k])
				}
			}
			return v
		}
		return walk(v)
	}
	var want, got []any
	for i, c := range g.Crawl.Ops {
		time.Sleep(3 * time.Millisecond)
		args := make([]any, len(c.Op.Args))
		for j, a := range c.Op.Args {
			args[j] = resolve(a)
		}
		var value any
		var err error
		str := func(i int) string { s, _ := args[i].(string); return s }
		switch c.Op.Fn {
		case "create":
			in, _ := jsParse(string(c.Op.Args[0]))
			var job crawlJob
			if job, err = l.crawlJobCreate(ctx, in); err == nil {
				jobIDs = append(jobIDs, job.ID)
				value = job
			}
		case "get":
			var job *crawlJob
			job, err = l.crawlJobGet(ctx, str(0))
			if job != nil {
				value = job
			}
		case "list":
			value, err = l.crawlJobList(ctx)
		case "pages":
			value, err = l.crawlPages(ctx, str(0))
		case "discover":
			value, err = l.crawlDiscover(ctx, str(0), str(1), int(args[2].(float64)), int(args[3].(float64)))
		case "setStatus":
			var msg *string
			if len(args) > 2 {
				s := str(2)
				msg = &s
			}
			err = l.crawlJobSetStatus(ctx, str(0), str(1), msg)
		case "save":
			raw, _ := json.Marshal(args[0])
			var p crawlPage
			if err = json.Unmarshal(raw, &p); err == nil {
				err = l.crawlSavePage(ctx, p)
			}
		case "recover":
			err = l.crawlRecover(ctx)
		case "retry":
			err = l.crawlJobRetry(ctx, str(0))
		default:
			t.Fatalf("未知操作 %s", c.Op.Fn)
		}
		w := map[string]any{"fn": c.Op.Fn}
		if c.Error != nil {
			w["error"] = *c.Error
		} else if v := jsonAny(t, c.Value); v != nil {
			w["value"] = v
		}
		r := map[string]any{"fn": c.Op.Fn}
		if err != nil {
			r["error"] = err.Error()
		} else if value != nil {
			r["value"] = goAny(t, value)
		}
		want, got = append(want, w), append(got, r)
		_ = i
	}
	if a, b := maskCrawlIDs(t, got), maskCrawlIDs(t, want); a != b {
		t.Fatalf("操作返回值不一致\n got %s\nwant %s", a, b)
	}
	jobsRows, _ := l.db.Query(ctx, "SELECT id, payload, status, created_at, updated_at FROM crawl_jobs ORDER BY created_at, rowid")
	pageRows, _ := l.db.Query(ctx, "SELECT id, job_id, url, depth, seed_index, status, payload FROM crawl_pages ORDER BY job_id, depth, rowid")
	gotJobs, wantJobs := goRows(jobsRows.Values), g.Crawl.Dump.Jobs
	gotDump := map[string]any{"jobs": parsePayloadColumns(t, gotJobs, 1), "pages": parsePayloadColumns(t, groupPagesByJob(gotJobs, goRows(pageRows.Values)), 6)}
	wantDump := map[string]any{"jobs": parsePayloadColumns(t, wantJobs, 1), "pages": parsePayloadColumns(t, groupPagesByJob(wantJobs, g.Crawl.Dump.Pages), 6)}
	// 页面按 job_id 排序，随机 id 的字典序在两侧不同；按（深度、插入序）已稳定，这里只比较内容
	if a, b := maskCrawlIDs(t, gotDump), maskCrawlIDs(t, wantDump); a != b {
		_ = os.WriteFile(filepath.Join(os.TempDir(), "crawl-got.json"), []byte(a), 0o644)
		_ = os.WriteFile(filepath.Join(os.TempDir(), "crawl-want.json"), []byte(b), 0o644)
		t.Fatalf("转储不一致（已写入临时目录 crawl-got.json / crawl-want.json）")
	}
	if !strings.Contains(maskCrawlIDs(t, got), "interrupted") {
		t.Error("recover 之后的读取里应出现 interrupted 状态")
	}
}

// groupPagesByJob 把页面行按所属批次的创建顺序重排（job_id 是随机 uuid，按它排序在两侧顺序不同）；批次内保持 depth、rowid 的原顺序。
func groupPagesByJob(jobs, pages [][]any) [][]any {
	order := map[string]int{}
	for i, j := range jobs {
		order[fmt.Sprint(j[0])] = i
	}
	out := append([][]any{}, pages...)
	sort.SliceStable(out, func(a, b int) bool { return order[fmt.Sprint(out[a][1])] < order[fmt.Sprint(out[b][1])] })
	return out
}

func goRows(vals [][]any) [][]any {
	out := make([][]any, len(vals))
	for i, r := range vals {
		row := make([]any, len(r))
		for j, v := range r {
			switch x := v.(type) {
			case int64:
				row[j] = float64(x)
			default:
				row[j] = v
			}
		}
		out[i] = row
	}
	return out
}
