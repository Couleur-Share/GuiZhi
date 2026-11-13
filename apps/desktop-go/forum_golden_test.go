package main

// 论坛连接器差分测试：对拍 golden-forum.mts 用真实 forum-note / forum-summary / v2ex / discourse / twolibra / forum-post
// 录制的结果（GUIZHI_FORUM_GOLDEN_DIR 或 GUIZHI_GOLDEN_DIR）。cooked HTML 转 Markdown 与 Turndown 的字节差异属已接受差异。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type forumRec struct {
	Kind   string          `json:"kind"`
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
}

func loadForumGolden(t *testing.T) []forumRec {
	t.Helper()
	dir := os.Getenv("GUIZHI_FORUM_GOLDEN_DIR")
	if dir == "" {
		dir = os.Getenv("GUIZHI_GOLDEN_DIR")
	}
	if dir == "" {
		t.Skip("未设置 GUIZHI_FORUM_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-forum.json"))
	if err != nil {
		t.Skip("未找到 golden-forum.json：" + err.Error())
	}
	var g struct {
		Recs []forumRec `json:"recs"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g.Recs
}

func fgDecode(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func fgCompare(t *testing.T, want, got any) {
	t.Helper()
	g := toGeneric(t, got)
	if !reflect.DeepEqual(want, g) {
		w, _ := json.MarshalIndent(want, "", " ")
		gg, _ := json.MarshalIndent(g, "", " ")
		t.Fatalf("与 TS 基线不一致\n--- want ---\n%s\n--- got ---\n%s", w, gg)
	}
}

// ---- 与 TS 同形的输出投影 ----

func fgReplyTo(r *forumReplyTo) any {
	if r == nil {
		return nil
	}
	m := map[string]any{"author": r.Author, "snippet": r.Snippet}
	if r.Floor != nil {
		m["floor"] = *r.Floor
	}
	return m
}

func fgMillis(ts int64) int64 {
	if math.Abs(float64(ts-time.Now().UnixMilli())) < 600_000 {
		return -1
	}
	return ts
}

func fgReplies(rs []forumReply) []any {
	out := []any{}
	for _, r := range rs {
		m := map[string]any{"floor": r.Floor, "author": r.Author, "content": r.Content, "createdAt": fgMillis(r.CreatedAt)}
		if r.ReplyTo != nil {
			m["replyTo"] = fgReplyTo(r.ReplyTo)
		}
		out = append(out, m)
	}
	return out
}

func fgThread(th *forumThread) any {
	if th == nil {
		return nil
	}
	m := map[string]any{
		"platform": th.Platform, "topicId": th.TopicID, "title": th.Title, "author": th.Author, "node": th.Node,
		"createdAt": fgMillis(th.CreatedAt), "replyCount": th.ReplyCount, "content": th.Content,
		"replies": fgReplies(th.Replies), "webpageUrl": th.WebpageURL,
	}
	if th.ReplyRetention != "" {
		m["replyRetention"] = th.ReplyRetention
	}
	if th.SummaryReplies != nil {
		m["summaryReplies"] = fgReplies(th.SummaryReplies)
	}
	if th.WarningReason != "" {
		m["warningReason"] = th.WarningReason
	}
	return m
}

func fgEntries(es []forumReplyEntry) []any {
	out := []any{}
	for _, e := range es {
		m := map[string]any{"floor": e.Floor, "author": e.Author, "content": e.Content}
		if e.ReplyTo != nil {
			m["replyTo"] = fgReplyTo(e.ReplyTo)
		}
		out = append(out, m)
	}
	return out
}

// ---- 夹具抓取器 ----

type fgOutcome struct {
	json    json.RawMessage
	hasJSON bool
	err     *string
}

func fgParseOutcome(t *testing.T, raw json.RawMessage) fgOutcome {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	var o fgOutcome
	if j, ok := m["json"]; ok {
		o.json, o.hasJSON = j, true
	}
	if e, ok := m["error"]; ok {
		var s string
		_ = json.Unmarshal(e, &s)
		o.err = &s
	}
	return o
}

// fgFetcher 返回按 URL 取夹具、记录调用顺序的 fetchJSON；夹具值为对象或对象数组（依次消费，最后一个重复）。
func fgFetcher(t *testing.T, fx map[string]json.RawMessage) (func(context.Context, string) (any, error), *[]string) {
	calls := []string{}
	counters := map[string]int{}
	return func(ctx context.Context, url string) (any, error) {
		calls = append(calls, url)
		raw, ok := fx[url]
		if !ok {
			return nil, errors.New("HTTP 404")
		}
		var list []json.RawMessage
		if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
			_ = json.Unmarshal(raw, &list)
		} else {
			list = []json.RawMessage{raw}
		}
		i := min(counters[url], len(list)-1)
		counters[url]++
		o := fgParseOutcome(t, list[i])
		if o.err != nil {
			return nil, errors.New(*o.err)
		}
		return jsParse(string(o.json))
	}, &calls
}

func TestForumGolden(t *testing.T) {
	recs := loadForumGolden(t)
	l := &Library{}
	ctx := context.Background()
	for _, r := range recs {
		want := fgDecode(t, r.Output)
		t.Run(r.Kind+"/"+r.Name, func(t *testing.T) {
			switch r.Kind {
			case "sanitize":
				var in string
				_ = json.Unmarshal(r.Input, &in)
				fgCompare(t, want, sanitizeForumSummary(in))
			case "needsTitle":
				var in string
				_ = json.Unmarshal(r.Input, &in)
				fgCompare(t, want, needsForumAiTitle(in))
			case "chunks":
				var in struct {
					Replies  []forumSummaryReply `json:"replies"`
					MaxChars int                 `json:"maxChars"`
				}
				fgUnmarshalReplies(t, r.Input, &in)
				got := splitForumReplyChunks(in.Replies, in.MaxChars)
				if got == nil {
					got = []string{}
				}
				fgCompare(t, want, got)
			case "summary":
				fgSummary(t, l, r, want)
			case "thread":
				fgThreadCase(t, l, r, want)
			case "noteOps":
				fgNoteOps(t, r, want)
			case "snippet":
				var in string
				_ = json.Unmarshal(r.Input, &in)
				fgCompare(t, want, normalizeForumSnippet(in, 200))
			case "replyBlock":
				one, two := 1, 1
				_ = two
				got := []string{
					formatForumReplyBlock(1, "甲", "内容", nil),
					formatForumReplyBlock(2, "", "匿名", &forumReplyTo{Author: " 乙 ", Floor: &one, Snippet: "摘<br>要"}),
					formatForumReplyBlock(3, "丙", "无作者引用", &forumReplyTo{Author: "", Snippet: ""}),
					formatForumReplyBlock(4, "丁", "仅摘要", &forumReplyTo{Author: "戊", Snippet: "x"}),
				}
				fgCompare(t, want, got)
			case "post":
				fgPost(t, l, ctx, r, want)
			default:
				if strings.HasPrefix(r.Kind, "nga") {
					return // NGA 基线由 TestForumNgaGolden 对拍
				}
				t.Fatalf("未知基线类型 %s", r.Kind)
			}
		})
	}
}

// fgUnmarshalReplies 把 {replies:[{floor,author,content}], ...} 解到带 json tag 的结构里。
func fgUnmarshalReplies(t *testing.T, raw json.RawMessage, out *struct {
	Replies  []forumSummaryReply `json:"replies"`
	MaxChars int                 `json:"maxChars"`
}) {
	t.Helper()
	var tmp struct {
		Replies []struct {
			Floor   int    `json:"floor"`
			Author  string `json:"author"`
			Content string `json:"content"`
		} `json:"replies"`
		MaxChars int `json:"maxChars"`
	}
	if err := json.Unmarshal(raw, &tmp); err != nil {
		t.Fatal(err)
	}
	out.MaxChars = tmp.MaxChars
	for _, r := range tmp.Replies {
		out.Replies = append(out.Replies, forumSummaryReply{Floor: r.Floor, Author: r.Author, Content: r.Content})
	}
}

func fgSummary(t *testing.T, l *Library, r forumRec, want any) {
	var in struct {
		Input struct {
			Title   string `json:"title"`
			Content string `json:"content"`
			Replies []struct {
				Floor   int    `json:"floor"`
				Author  string `json:"author"`
				Content string `json:"content"`
			} `json:"replies"`
		} `json:"input"`
		Outputs []json.RawMessage `json:"outputs"`
	}
	if err := json.Unmarshal(r.Input, &in); err != nil {
		t.Fatal(err)
	}
	sin := forumSummaryInput{Title: in.Input.Title, Content: in.Input.Content}
	for _, x := range in.Input.Replies {
		sin.Replies = append(sin.Replies, forumSummaryReply{Floor: x.Floor, Author: x.Author, Content: x.Content})
	}
	calls := []any{}
	idx := 0
	chat := func(_ context.Context, _ aiClientConfig, messages []aiChatMessage, opts aiChatOptions) (aiChatResult, error) {
		ms := []any{}
		for _, m := range messages {
			ms = append(ms, map[string]any{"role": m.Role, "content": m.Content})
		}
		calls = append(calls, map[string]any{"messages": ms, "temperature": *opts.Temperature, "maxTokens": opts.MaxTokens, "timeoutMs": int(opts.Timeout / time.Millisecond)})
		raw := in.Outputs[min(idx, len(in.Outputs)-1)]
		idx++
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return aiChatResult{Content: s, FinishReason: "stop", Usage: &chatTokenUsage{PromptTokens: 1, CompletionTokens: 2}}, nil
		}
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return aiChatResult{}, errors.New(e.Error)
	}
	res, err := l.generateForumSummary(context.Background(), sin, aiClientConfig{Model: "m"}, chat)
	out := map[string]any{"result": nil, "error": nil, "calls": calls}
	if err != nil {
		out["error"] = err.Error()
	} else if res != nil {
		var title any
		if res.HasTitle {
			title = res.Title
		}
		out["result"] = map[string]any{"summary": res.Summary, "title": title}
	}
	fgCompare(t, want, out)
}

func fgThreadCase(t *testing.T, l *Library, r forumRec, want any) {
	var in struct {
		Platform    string                     `json:"platform"`
		TopicID     string                     `json:"topicId"`
		Fixture     map[string]json.RawMessage `json:"fixture"`
		Auth        map[string]json.RawMessage `json:"auth"`
		RetryDelays []int                      `json:"retryDelays"`
		PageSize    int                        `json:"pageSize"`
		MaxPages    int                        `json:"maxPages"`
	}
	if err := json.Unmarshal(r.Input, &in); err != nil {
		t.Fatal(err)
	}
	fetch, calls := fgFetcher(t, in.Fixture)
	deps := forumDeps{fetchJSON: fetch, retryDelays: make([]time.Duration, len(in.RetryDelays)), commentPageSize: in.PageSize, maxCommentPages: in.MaxPages}
	var authCalls *[]string
	if in.Auth != nil {
		deps.fetchAuthenticatedJSON, authCalls = fgFetcher(t, in.Auth)
	}
	var th *forumThread
	var err error
	switch in.Platform {
	case "v2ex":
		th, err = l.fetchV2exThread(context.Background(), in.TopicID, deps)
	case "appinn":
		th, err = l.fetchDiscourseThread(context.Background(), in.TopicID, appinnSite, deps)
	case "linuxdo":
		th, err = l.fetchDiscourseThread(context.Background(), in.TopicID, linuxdoSite, deps)
	case "twolibra":
		th, err = l.fetchTwolibraThread(context.Background(), in.TopicID, deps)
	default:
		t.Fatalf("未知平台 %s", in.Platform)
	}
	out := map[string]any{"thread": nil, "error": nil, "calls": *calls, "authCalls": []string{}}
	if authCalls != nil {
		out["authCalls"] = *authCalls
	}
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["thread"] = fgThread(th)
	}
	// 已接受差异：html-to-markdown 与 Turndown 的列表缩进、围栏语言标注等字节差异。
	// 其余字段仍严格对拍；正文改与手写的 Go 期望对比，并断言基线确实带有富文本结构。
	if r.Name == "ds-cooked-rich" {
		wt := want.(map[string]any)["thread"].(map[string]any)
		gt := out["thread"].(map[string]any)
		const goWant = "## 小标题\n\n- 一\n- 二\n  \n  - 二点一\n\n1. 甲\n2. 乙\n\n> 引用\n\n```js\nconst a = 1;\nconsole.log(a);\n```\n\n行内 `x`  \n换行\n\n* * *\n\n格"
		if gt["content"] != goWant {
			t.Fatalf("富文本正文与 Go 期望不一致：%q", gt["content"])
		}
		if !strings.Contains(wt["content"].(string), "## 小标题") {
			t.Fatal("基线缺少富文本结构")
		}
		wt["content"], gt["content"] = "", ""
	}
	fgCompare(t, want, out)
}

func fgNoteOps(t *testing.T, r forumRec, want any) {
	var in struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(r.Input, &in)
	content := in.Content
	sec := splitForumNoteSections(content)
	replies := parseForumReplies(content)
	filtered := []any{}
	for _, q := range []string{"", "乙", "1 楼", "回复", "不存在的关键字", "多行"} {
		floors := []any{}
		for _, e := range filterForumReplies(replies, q) {
			floors = append(floors, e.Floor)
		}
		filtered = append(filtered, map[string]any{"q": q, "floors": floors})
	}
	one, nine := 1, 9
	resolve := []any{}
	for _, rt := range []forumReplyTo{{Author: "甲", Floor: &one}, {Author: "乙"}, {Author: ""}, {Author: "不存在", Floor: &nine}, {Author: "旧作者"}} {
		if f, ok := resolveReplyTargetFloor(replies, rt); ok {
			resolve = append(resolve, f)
		} else {
			resolve = append(resolve, nil)
		}
	}
	newReplies := []forumReplyEntry{
		{Floor: 1, Author: "新甲", Content: "新内容"},
		{Floor: 2, Author: "新乙", Content: "回复", ReplyTo: &forumReplyTo{Author: "新甲", Floor: &one, Snippet: "新内容"}},
	}
	out := map[string]any{
		"sections":       map[string]any{"summary": sec.Summary, "body": sec.Body, "replies": sec.Replies},
		"replies":        fgEntries(replies),
		"sectionReplies": fgEntries(parseForumReplySection(sec.Replies)),
		"filtered":       filtered,
		"resolve":        resolve,
		"upsert":         upsertForumSummarySection(content, "新的总结\n\n- 点一\n- 点二"),
		"replace":        replaceForumRepliesSection(content, newReplies),
		"replaceEmpty":   replaceForumRepliesSection(content, nil),
	}
	fgCompare(t, want, out)
}

func fgPost(t *testing.T, l *Library, ctx context.Context, r forumRec, want any) {
	var in struct {
		Target forumTarget `json:"target"`
		Thread *struct {
			Platform       string `json:"platform"`
			TopicID        string `json:"topicId"`
			Title          string `json:"title"`
			Author         string `json:"author"`
			Node           string `json:"node"`
			CreatedAt      int64  `json:"createdAt"`
			ReplyCount     int    `json:"replyCount"`
			Content        string `json:"content"`
			Replies        []fgJSONReply
			ReplyRetention string         `json:"replyRetention"`
			SummaryReplies *[]fgJSONReply `json:"summaryReplies"`
			WebpageURL     string         `json:"webpageUrl"`
			WarningReason  string         `json:"warningReason"`
		} `json:"thread"`
		FetchError *string         `json:"fetchError"`
		Cfg        bool            `json:"cfg"`
		Summarize  json.RawMessage `json:"summarize"`
	}
	if err := json.Unmarshal(r.Input, &in); err != nil {
		t.Fatal(err)
	}
	// forumTarget 没有 json tag：Platform/TopicID 大小写不敏感匹配 platform/topicId
	stages := []string{}
	summarizeCalls := []any{}
	deps := forumPostDeps{onStage: func(s string) { stages = append(stages, s) }}
	deps.getSummaryConfig = func() *aiClientConfig {
		if in.Cfg {
			return &aiClientConfig{Model: "m"}
		}
		return nil
	}
	deps.summarize = func(_ context.Context, sin forumSummaryInput, _ aiClientConfig) (*forumSummaryResult, error) {
		rs := []any{}
		for _, x := range sin.Replies {
			rs = append(rs, map[string]any{"floor": x.Floor, "author": x.Author, "content": x.Content})
		}
		summarizeCalls = append(summarizeCalls, map[string]any{"title": sin.Title, "content": sin.Content, "replies": rs})
		raw := strings.TrimSpace(string(in.Summarize))
		if raw == "null" || raw == `"undefined"` {
			return nil, nil
		}
		var e struct {
			Error   *string `json:"error"`
			Summary string  `json:"summary"`
			Title   *string `json:"title"`
		}
		_ = json.Unmarshal(in.Summarize, &e)
		if e.Error != nil {
			return nil, errors.New(*e.Error)
		}
		res := &forumSummaryResult{Summary: e.Summary}
		if e.Title != nil {
			res.Title, res.HasTitle = *e.Title, true
		}
		return res, nil
	}
	if in.Thread != nil {
		th := &forumThread{
			Platform: in.Thread.Platform, TopicID: in.Thread.TopicID, Title: in.Thread.Title, Author: in.Thread.Author, Node: in.Thread.Node,
			CreatedAt: in.Thread.CreatedAt, ReplyCount: in.Thread.ReplyCount, Content: in.Thread.Content,
			ReplyRetention: in.Thread.ReplyRetention, WebpageURL: in.Thread.WebpageURL, WarningReason: in.Thread.WarningReason,
		}
		th.Replies = fgToReplies(in.Thread.Replies)
		if in.Thread.SummaryReplies != nil {
			th.SummaryReplies = fgToReplies(*in.Thread.SummaryReplies)
		}
		deps.fetchThread = func(context.Context, forumTarget) (*forumThread, error) { return th, nil }
	} else if in.FetchError != nil {
		deps.fetchThread = func(context.Context, forumTarget) (*forumThread, error) { return nil, errors.New(*in.FetchError) }
	}
	// TS 把完整回复对象交给 summarize；Go 只传总结用得上的三个字段，基线按此投影后再比
	if wm, ok := want.(map[string]any); ok {
		if calls, ok := wm["summarizeCalls"].([]any); ok {
			for _, c := range calls {
				cm, _ := c.(map[string]any)
				rs, _ := cm["replies"].([]any)
				for i, x := range rs {
					xm, _ := x.(map[string]any)
					rs[i] = map[string]any{"floor": xm["floor"], "author": xm["author"], "content": xm["content"]}
				}
			}
		}
	}
	ex, err := l.extractForumPost(ctx, in.Target, deps)
	out := map[string]any{"result": nil, "error": nil, "stages": stages, "summarizeCalls": summarizeCalls}
	if err != nil {
		out["error"] = err.Error()
	} else {
		var uri any
		if ex.SourceURI != nil {
			uri = *ex.SourceURI
		}
		m := map[string]any{"title": ex.Title, "content": ex.Content, "itemType": ex.ItemType, "sourceUri": uri}
		if ex.WarningReason != "" {
			m["warningReason"] = ex.WarningReason
		}
		if ex.DegradedReason != "" {
			m["degradedReason"] = ex.DegradedReason
		}
		out["result"] = m
	}
	fgCompare(t, want, out)
}

type fgJSONReply struct {
	Floor     int    `json:"floor"`
	Author    string `json:"author"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"createdAt"`
	ReplyTo   *struct {
		Author  string `json:"author"`
		Floor   *int   `json:"floor"`
		Snippet string `json:"snippet"`
	} `json:"replyTo"`
}

func fgToReplies(in []fgJSONReply) []forumReply {
	out := []forumReply{}
	for _, r := range in {
		fr := forumReply{Floor: r.Floor, Author: r.Author, Content: r.Content, CreatedAt: r.CreatedAt}
		if r.ReplyTo != nil {
			fr.ReplyTo = &forumReplyTo{Author: r.ReplyTo.Author, Floor: r.ReplyTo.Floor, Snippet: r.ReplyTo.Snippet}
		}
		out = append(out, fr)
	}
	return out
}

var _ = fmt.Sprint
