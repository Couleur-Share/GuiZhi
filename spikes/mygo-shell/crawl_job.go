package main

// 对应 packages/db/src/crawl-job.ts（CrawlJobDB）与 shared/utils/web-scope.ts 的 validateCrawlInput：
// 文档批次的持久化与状态。行里的 payload 与 TS 一样是 JSON，状态与时间以列为准。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/egoist/mygo/plugins/sqlite"
)

type crawlSeed struct {
	URL       string  `json:"url"`
	Mode      string  `json:"mode"`
	Directory *string `json:"directory,omitempty"`
}

type crawlInput struct {
	Purpose         string          `json:"purpose"`
	MaxPages        int             `json:"maxPages"`
	MaxDepth        int             `json:"maxDepth"`
	CollectionID    json.RawMessage `json:"collectionId"`
	ResearchRunID   json.RawMessage `json:"researchRunId,omitempty"`
	DuplicatePolicy string          `json:"duplicatePolicy"`
	Seeds           []crawlSeed     `json:"seeds"`
}

type crawlJob struct {
	ID        string         `json:"id"`
	Input     crawlInput     `json:"input"`
	Status    string         `json:"status"`
	CreatedAt int64          `json:"createdAt"`
	UpdatedAt int64          `json:"updatedAt"`
	Error     *string        `json:"error,omitempty"`
	Counts    map[string]int `json:"counts"`
}

// crawlPage 对应 CrawlPage；Result 保持原始 JSON，旧库里带快照等字段的结果不会在回写时丢失。
type crawlPage struct {
	Stage        string          `json:"stage,omitempty"`
	ID           string          `json:"id"`
	JobID        string          `json:"jobId"`
	URL          string          `json:"url"`
	Depth        int             `json:"depth"`
	SeedIndex    int             `json:"seedIndex"`
	Status       string          `json:"status"`
	ItemID       string          `json:"itemId,omitempty"`
	ImportTaskID string          `json:"importTaskId,omitempty"`
	Error        string          `json:"error,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
}

// ---- validateCrawlInput ----

func jsHas(list []string, v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func jsNullishString(v any) (string, bool) {
	if isNullish(v) {
		return "", false
	}
	return jsToString(v), true
}

// validateCrawlInput 对应 validateCrawlInput(input)：参数来自 IPC 的原始值，报错文本沿用 TS（含 JS 引擎的 TypeError）。
func validateCrawlInput(in any) (crawlInput, error) {
	var out crawlInput
	if !vTruthy(in) || !jsHas([]string{"documents", "research"}, jsGet(in, "purpose")) {
		return out, errors.New("采集用途无效")
	}
	seeds, ok := jsGet(in, "seeds").(jsArr)
	if !ok || len(seeds) == 0 || len(seeds) > 10 {
		return out, errors.New("请提供 1–10 个入口")
	}
	maxPages, maxDepth := any(float64(50)), any(float64(2))
	if v := jsGet(in, "maxPages"); !isNullish(v) {
		maxPages = v
	}
	if v := jsGet(in, "maxDepth"); !isNullish(v) {
		maxDepth = v
	}
	mp, ok1 := jsIsInteger(maxPages)
	md, ok2 := jsIsInteger(maxDepth)
	if !ok1 || mp < 1 || mp > 300 || !ok2 || md < 0 || md > 5 {
		return out, errors.New("页数须为 1–300，深度须为 0–5")
	}
	out.Purpose = jsGet(in, "purpose").(string)
	out.MaxPages, out.MaxDepth = int(mp), int(md)
	// collectionId / researchRunId 在 TS 里原样透传（含 null 与非字符串值）
	out.CollectionID = json.RawMessage("null")
	if v := jsGet(in, "collectionId"); !isNullish(v) {
		out.CollectionID, _ = marshalNoEscape(jsToNative(v))
	}
	if v := jsGet(in, "researchRunId"); v != jsUndefined {
		out.ResearchRunID, _ = marshalNoEscape(jsToNative(v))
	}
	out.DuplicatePolicy = "skip"
	if jsGet(in, "duplicatePolicy") == "update" {
		out.DuplicatePolicy = "update"
	}
	for _, seed := range seeds {
		if isNullish(seed) {
			prop := "undefined"
			if seed == jsNull {
				prop = "null"
			}
			return out, fmt.Errorf("Cannot read properties of %s (reading 'mode')", prop)
		}
		mode := jsGet(seed, "mode")
		if !jsHas([]string{"page", "directory"}, mode) {
			return out, errors.New("入口模式无效")
		}
		url, err := canonicalWebURL(jsToString(jsGet(seed, "url")))
		if err != nil {
			return out, err
		}
		s := crawlSeed{URL: url, Mode: mode.(string)}
		if s.Mode == "directory" {
			var dir *string
			if d := jsGet(seed, "directory"); !isNullish(d) {
				ds, isStr := d.(string)
				if !isStr {
					return out, errors.New("pathname.startsWith is not a function")
				}
				dir = &ds
			}
			scope, err := newWebScope(url, dir)
			if err != nil {
				return out, err
			}
			s.Directory = &scope.Directory
			if scope2, err := newWebScope(url, &scope.Directory); err != nil {
				return out, err
			} else if !inWebScope(url, scope2) {
				return out, errors.New("入口不在所选目录内")
			}
		}
		out.Seeds = append(out.Seeds, s)
	}
	return out, nil
}

// collectionIDString 返回有效的集合 id（空或非字符串视为无）。
func (in crawlInput) collectionIDString() string {
	var s string
	if json.Unmarshal(in.CollectionID, &s) == nil {
		return s
	}
	return ""
}

// jsToNative 把 jsParse 得到的值转成 encoding/json 可直接序列化的原生值。
func jsToNative(v any) any {
	switch x := v.(type) {
	case jsUndefinedT, jsNullT:
		return nil
	case jsArr:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsToNative(e)
		}
		return out
	case *jsObj:
		out := map[string]any{}
		for _, k := range x.keys() {
			out[k] = jsToNative(x.m[k])
		}
		return out
	}
	return v
}

// ---- 持久化 ----

func (l *Library) crawlPayload(job crawlJob) string {
	o := newObj()
	o.set("id", job.ID)
	in, _ := marshalNoEscape(job.Input)
	parsed, _ := jsParse(string(in))
	o.set("input", parsed)
	o.set("status", job.Status)
	o.set("createdAt", float64(job.CreatedAt))
	o.set("updatedAt", float64(job.UpdatedAt))
	counts := newObj()
	for k, v := range job.Counts {
		counts.set(k, float64(v))
	}
	o.set("counts", counts)
	if job.Error != nil {
		o.set("error", *job.Error)
	}
	s, _ := jsStringify(o)
	return s
}

func (l *Library) crawlJobCreate(ctx context.Context, in any) (crawlJob, error) {
	input, err := validateCrawlInput(in)
	if err != nil {
		return crawlJob{}, err
	}
	now := nowMs()
	job := crawlJob{ID: newUUID(), Input: input, Status: "pending", CreatedAt: now, UpdatedAt: now, Counts: map[string]int{}}
	stmts := []sqlite.Statement{{
		SQL:  "INSERT INTO crawl_jobs VALUES (?,?,?,?,?)",
		Args: []any{job.ID, l.crawlPayload(job), job.Status, now, now},
	}}
	for i, seed := range input.Seeds {
		page, err := newCrawlPage(job.ID, seed.URL, 0, i)
		if err != nil {
			return crawlJob{}, err
		}
		stmts = append(stmts, crawlDiscoverStmt(page))
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return crawlJob{}, err
	}
	got, err := l.crawlJobGet(ctx, job.ID)
	if err != nil {
		return crawlJob{}, err
	}
	return *got, nil
}

func newCrawlPage(jobID, raw string, depth, seedIndex int) (crawlPage, error) {
	url, err := canonicalWebURL(raw)
	if err != nil {
		return crawlPage{}, err
	}
	return crawlPage{ID: newUUID(), JobID: jobID, URL: url, Depth: depth, SeedIndex: seedIndex, Status: "pending"}, nil
}

func marshalPagePayload(p crawlPage) string {
	b, _ := marshalNoEscape(p)
	return string(b)
}

func crawlDiscoverStmt(p crawlPage) sqlite.Statement {
	return sqlite.Statement{
		SQL:  "INSERT OR IGNORE INTO crawl_pages VALUES (?,?,?,?,?,?,?)",
		Args: []any{p.ID, p.JobID, p.URL, p.Depth, p.SeedIndex, p.Status, marshalPagePayload(p)},
	}
}

func (l *Library) crawlJobGet(ctx context.Context, id string) (*crawlJob, error) {
	rows, err := l.db.Query(ctx, "SELECT payload,status,updated_at FROM crawl_jobs WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	var job crawlJob
	payload, _ := rows.Values[0][0].(string)
	if err := json.Unmarshal([]byte(payload), &job); err != nil {
		return nil, err
	}
	job.Status, _ = rows.Values[0][1].(string)
	job.UpdatedAt = asInt(rows.Values[0][2])
	job.Counts = map[string]int{}
	counts, err := l.db.Query(ctx, "SELECT status,COUNT(*) AS n FROM crawl_pages WHERE job_id=? GROUP BY status", id)
	if err != nil {
		return nil, err
	}
	for _, r := range counts.Values {
		s, _ := r[0].(string)
		job.Counts[s] = int(asInt(r[1]))
	}
	return &job, nil
}

func (l *Library) crawlJobList(ctx context.Context) ([]crawlJob, error) {
	rows, err := l.db.Query(ctx, "SELECT id FROM crawl_jobs ORDER BY created_at DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	out := []crawlJob{}
	for _, r := range rows.Values {
		id, _ := r[0].(string)
		job, err := l.crawlJobGet(ctx, id)
		if err != nil {
			return nil, err
		}
		if job != nil {
			out = append(out, *job)
		}
	}
	return out, nil
}

// crawlJobSetStatus 对应 setStatus：payload 里的 updatedAt 保持读到的旧值，error 缺省即清除。
func (l *Library) crawlJobSetStatus(ctx context.Context, id, status string, errText *string) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	job, err := l.crawlJobGet(ctx, id)
	if err != nil {
		return err
	}
	if job == nil {
		return errors.New("采集批次不存在")
	}
	job.Status, job.Error = status, errText
	_, err = l.db.Transaction(ctx, []sqlite.Statement{{
		SQL:  "UPDATE crawl_jobs SET status=?,updated_at=?,payload=? WHERE id=?",
		Args: []any{status, nowMs(), l.crawlPayload(*job), id},
	}})
	return err
}

func (l *Library) crawlPages(ctx context.Context, id string) ([]crawlPage, error) {
	rows, err := l.db.Query(ctx, "SELECT payload,status FROM crawl_pages WHERE job_id=? ORDER BY depth,rowid", id)
	if err != nil {
		return nil, err
	}
	out := []crawlPage{}
	for _, r := range rows.Values {
		var p crawlPage
		payload, _ := r[0].(string)
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return nil, err
		}
		p.Status, _ = r[1].(string)
		out = append(out, p)
	}
	return out, nil
}

// crawlDiscover 对应 discover：超出页数/深度上限或网址已存在时返回 false。
func (l *Library) crawlDiscover(ctx context.Context, jobID, raw string, depth, seedIndex int) (bool, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	return l.crawlDiscoverLocked(ctx, jobID, raw, depth, seedIndex)
}

func (l *Library) crawlDiscoverLocked(ctx context.Context, jobID, raw string, depth, seedIndex int) (bool, error) {
	job, err := l.crawlJobGet(ctx, jobID)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, errors.New("采集批次不存在")
	}
	rows, err := l.db.Query(ctx, "SELECT COUNT(*) AS n FROM crawl_pages WHERE job_id=?", jobID)
	if err != nil {
		return false, err
	}
	if int(asInt(rows.Values[0][0])) >= job.Input.MaxPages || depth > job.Input.MaxDepth {
		return false, nil
	}
	page, err := newCrawlPage(jobID, raw, depth, seedIndex)
	if err != nil {
		return false, err
	}
	res, err := l.db.Transaction(ctx, []sqlite.Statement{crawlDiscoverStmt(page)})
	if err != nil {
		return false, err
	}
	return res[0].Changes > 0, nil
}

func crawlSaveStmt(p crawlPage) sqlite.Statement {
	return sqlite.Statement{
		SQL:  "UPDATE crawl_pages SET status=?,payload=? WHERE id=? AND job_id=?",
		Args: []any{p.Status, marshalPagePayload(p), p.ID, p.JobID},
	}
}

func (l *Library) crawlSavePage(ctx context.Context, p crawlPage) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_, err := l.db.Transaction(ctx, []sqlite.Statement{crawlSaveStmt(p)})
	return err
}

// crawlRecover 对应 recover：启动时把中断的批次标成 interrupted、把运行中的页放回队列。
func (l *Library) crawlRecover(ctx context.Context) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_, err := l.db.Transaction(ctx, []sqlite.Statement{
		{SQL: "UPDATE crawl_jobs SET status='interrupted',updated_at=? WHERE status IN ('pending','running')", Args: []any{nowMs()}},
		{SQL: "UPDATE crawl_pages SET status='pending' WHERE status='running'"},
	})
	return err
}

func (l *Library) crawlJobRetry(ctx context.Context, id string) error {
	job, err := l.crawlJobGet(ctx, id)
	if err != nil {
		return err
	}
	if job != nil && job.Status == "running" {
		return errors.New("请先暂停批次")
	}
	l.writeMu.Lock()
	_, err = l.db.Transaction(ctx, []sqlite.Statement{{SQL: "UPDATE crawl_pages SET status='pending' WHERE job_id=? AND status='failed'", Args: []any{id}}})
	l.writeMu.Unlock()
	if err != nil {
		return err
	}
	return l.crawlJobSetStatus(ctx, id, "paused", nil)
}
