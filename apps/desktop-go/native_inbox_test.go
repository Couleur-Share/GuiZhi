package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

// 处理中心适配器的集成测试：真实数据库（临时数据目录）、真实 IPC 处理器、本地假 AI 服务；不碰真实的用户数据目录。

func newInboxAdapterForTest(t *testing.T) (*inboxAdapter, *Bridge, *Library) {
	t.Helper()
	_, b, l := newLibraryAIAdapterForTest(t)
	l.setPush(func(string, any) {})
	// 采集队列的重试会把任务重新排队：用一个永远失败的提取器并先暂停队列，测试期间不会有后台协程写库
	l.extractOverride = func(context.Context, map[string]any, func(string)) (*extractedContent, error) {
		return nil, errors.New("测试：不抓取")
	}
	l.registerImport(b)
	l.registerImportList(b)
	l.registerInbox(b)
	l.registerInboxWrite(b)
	l.registerSemantic(b)
	l.registerWikiCompiler(b)
	if _, err := b.call(t.Context(), "import:pause"); err != nil {
		t.Fatalf("import:pause: %v", err)
	}
	return newInboxAdapter(b), b, l
}

type inboxFixture struct {
	review, plain1, plain2, filed string // 条目 ID
}

// seedInbox 建四个条目与三个采集任务：一个待复核、两个未归合集、一个已归合集；失败 / 重复 / 带警告的完成各一个任务。
func seedInbox(t *testing.T, b *Bridge, l *Library) inboxFixture {
	t.Helper()
	ctx := t.Context()
	col, err := callAs[struct {
		ID string `json:"id"`
	}](ctx, b, "collection:create", map[string]any{"name": "已有合集"})
	if err != nil || col.ID == "" {
		t.Fatalf("collection:create: %+v %v", col, err)
	}
	body := "这是一段足够长的正文，用来让语义索引与 Wiki 预览把它当成有效资料处理。"
	fx := inboxFixture{
		review: createOpsItem(t, b, map[string]any{"title": "待复核", "content": body, "reviewStatus": "needs_review", "reviewReasons": []string{"来源存疑", "正文偏短"}}),
		plain1: createOpsItem(t, b, map[string]any{"title": "未归类甲", "content": body + "甲"}),
		plain2: createOpsItem(t, b, map[string]any{"title": "未归类乙", "content": body + "乙"}),
		filed:  createOpsItem(t, b, map[string]any{"title": "已归类", "content": body + "丙", "collectionId": col.ID}),
	}
	insert := func(id, name, status string, errMsg, warn any, result, dup any, created int64) {
		t.Helper()
		if _, err := l.db.Execute(ctx, `INSERT INTO import_tasks
 (id, source_kind, source_input, display_name, status, error, warning, result_item_id, duplicate_item_id, created_at, updated_at)
 VALUES (?, 'url', ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, "https://example.com/"+id, name, status, errMsg, warn, result, dup, created, created); err != nil {
			t.Fatal(err)
		}
	}
	insert("t-fail", "失败的采集", "failed", "解析失败", nil, nil, nil, 3_000)
	insert("t-dup", "重复的采集", "duplicate", nil, nil, nil, fx.filed, 2_000)
	insert("t-warn", "带警告的采集", "completed", nil, "语音转写失败", fx.plain1, nil, 1_000)
	return fx
}

func findInboxItem(list services.InboxList, kind, key string) (services.InboxItem, bool) {
	for _, it := range list.Items {
		if it.Kind == kind && (it.ItemID == key || it.TaskID == key || key == "") {
			return it, true
		}
	}
	return services.InboxItem{}, false
}

// inbox:list 的输出与 DTO 逐字段对得上：分组、原因列表、采集任务的状态与消息、计数、去重总数与语义索引聚合行。
func TestInboxAdapterListAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	a, b, l := newInboxAdapterForTest(t)

	empty, err := a.List(ctx)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.Total != 0 {
		t.Fatalf("空库应是空列表（非 nil）且没有错误：%+v %v", empty, err)
	}

	fx := seedInbox(t, b, l)
	list, err := a.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	review, ok := findInboxItem(list, services.InboxKindReviewRequired, fx.review)
	if !ok || review.Title != "待复核" || !slices.Equal(review.Reasons, []string{"来源存疑", "正文偏短"}) || review.CreatedAt == 0 {
		t.Fatalf("复核条目不对：%+v ok=%v", review, ok)
	}
	if got := review.KnowledgeItemID(); got != fx.review {
		t.Fatalf("复核条目的知识条目 ID 应是 %s，实际 %s", fx.review, got)
	}
	for _, id := range []string{fx.plain1, fx.plain2} {
		if _, ok := findInboxItem(list, services.InboxKindUnclassified, id); !ok {
			t.Fatalf("未归合集的条目 %s 应在列表里", id)
		}
	}
	if _, ok := findInboxItem(list, services.InboxKindUnclassified, fx.filed); ok {
		t.Fatal("已归入合集的条目不应出现在未归合集里")
	}

	fail, ok := findInboxItem(list, services.InboxKindImportIssue, "t-fail")
	if !ok || fail.Status != "failed" || fail.Message != "解析失败" || fail.Title != "失败的采集" || fail.ResultItemID != "" || fail.KnowledgeItemID() != "" {
		t.Fatalf("失败任务不对：%+v ok=%v", fail, ok)
	}
	dup, _ := findInboxItem(list, services.InboxKindImportIssue, "t-dup")
	if dup.Status != "duplicate" || dup.DuplicateItemID != fx.filed || dup.Message != "需要处理" {
		t.Fatalf("重复任务不对（无错误无警告时消息取默认）：%+v", dup)
	}
	warn, _ := findInboxItem(list, services.InboxKindImportIssue, "t-warn")
	if warn.Status != "completed" || warn.Message != "语音转写失败" || warn.ResultItemID != fx.plain1 {
		t.Fatalf("带警告的完成任务不对：%+v", warn)
	}

	// 语义索引聚合行：四个条目都还没有向量
	sem, ok := findInboxItem(list, services.InboxKindSemanticPending, "")
	if !ok || sem.Count != 4 || !sem.IsAggregate() || sem.KnowledgeItemID() != "" {
		t.Fatalf("语义索引聚合行不对：%+v ok=%v", sem, ok)
	}
	want := services.InboxCounts{ReviewRequired: 1, Unclassified: 2, ImportIssue: 3, SemanticPending: 4}
	if list.Counts != want {
		t.Fatalf("计数不对：got %+v want %+v", list.Counts, want)
	}
	// 去重总数：待复核条目 + 失败任务 + 重复任务指向的已归类条目 + 带警告的任务对应的 plain1（条目维度各算一次）
	if list.Total != 4 {
		t.Fatalf("去重总数应是 4，实际 %d", list.Total)
	}
	if list.Counts.Of(services.InboxKindImportIssue) != 3 || list.Counts.With(services.InboxKindWikiPending, 7).WikiPending != 7 {
		t.Fatalf("InboxCounts 的 Of / With 不对：%+v", list.Counts)
	}
}

// 批量整理与标记已复核：返回实际改动条数，列表随之变化；合集不存在时处理器的错误原样带回。
func TestInboxAdapterOrganizeAndMarkReviewed(t *testing.T) {
	ctx := t.Context()
	a, b, l := newInboxAdapterForTest(t)
	fx := seedInbox(t, b, l)

	col, err := callAs[struct {
		ID string `json:"id"`
	}](ctx, b, "collection:create", map[string]any{"name": "目标合集"})
	if err != nil {
		t.Fatal(err)
	}

	changed, err := a.Organize(ctx, services.InboxOrganizeInput{ItemIDs: []string{fx.plain1, fx.plain2}, CollectionID: col.ID, AddTagNames: []string{"归档"}})
	if err != nil || changed != 2 {
		t.Fatalf("Organize 应改动 2 条：changed=%d err=%v", changed, err)
	}
	list, _ := a.List(ctx)
	if list.Counts.Unclassified != 0 {
		t.Fatalf("整理后不应再有未归合集：%+v", list.Counts)
	}
	moved, err := callAs[struct {
		CollectionID string   `json:"collectionId"`
		Tags         []string `json:"tagNames"`
	}](ctx, b, "knowledge:get", fx.plain1)
	if err != nil || moved.CollectionID != col.ID {
		t.Fatalf("条目应已归入目标合集：%+v %v", moved, err)
	}

	if n, err := a.Organize(ctx, services.InboxOrganizeInput{}); err != nil || n != 0 {
		t.Fatalf("没有条目时 Organize 是 0 且不报错：%d %v", n, err)
	}
	if _, err := a.Organize(ctx, services.InboxOrganizeInput{ItemIDs: []string{fx.review}, CollectionID: "no-such-collection"}); err == nil {
		t.Fatal("合集不存在时 Organize 应返回错误")
	}

	changed, err = a.MarkReviewed(ctx, []string{fx.review})
	if err != nil || changed != 1 {
		t.Fatalf("MarkReviewed 应改动 1 条：changed=%d err=%v", changed, err)
	}
	list, _ = a.List(ctx)
	if _, ok := findInboxItem(list, services.InboxKindReviewRequired, fx.review); ok || list.Counts.ReviewRequired != 0 {
		t.Fatalf("标记已复核后不应再出现在复核分组里：%+v", list.Counts)
	}
	if n, err := a.MarkReviewed(ctx, nil); err != nil || n != 0 {
		t.Fatalf("空列表应是 0 且不报错：%d %v", n, err)
	}
}

// 「不再提醒」：第一次处理一条，第二次是 0（早已处理），任务从列表消失但采集记录还在；失败任务不能被它处理。
func TestInboxAdapterAcknowledgeImportWarning(t *testing.T) {
	ctx := t.Context()
	a, b, l := newInboxAdapterForTest(t)
	seedInbox(t, b, l)

	if n, err := a.AcknowledgeImportWarning(ctx, "t-warn"); err != nil || n != 1 {
		t.Fatalf("第一次应处理 1 条：%d %v", n, err)
	}
	if n, err := a.AcknowledgeImportWarning(ctx, "t-warn"); err != nil || n != 0 {
		t.Fatalf("第二次应是 0：%d %v", n, err)
	}
	if n, err := a.AcknowledgeImportWarning(ctx, "t-fail"); err != nil || n != 0 {
		t.Fatalf("失败的任务不是警告，不应被处理：%d %v", n, err)
	}
	list, _ := a.List(ctx)
	if _, ok := findInboxItem(list, services.InboxKindImportIssue, "t-warn"); ok {
		t.Fatal("已确认的警告不应再出现")
	}
	rows, err := l.db.Query(ctx, "SELECT COUNT(*) FROM import_tasks WHERE id = 't-warn'")
	if err != nil || asInt(rows.Values[0][0]) != 1 {
		t.Fatalf("采集记录应保留：%v %v", rows, err)
	}
}

// 重试采集任务：任务存在返回 found=true（任务回到队列），不存在是 found=false 而不是错误。
func TestInboxAdapterRetryImportTask(t *testing.T) {
	ctx := t.Context()
	a, b, l := newInboxAdapterForTest(t)
	seedInbox(t, b, l)

	found, err := a.RetryImportTask(ctx, "t-fail")
	if err != nil || !found {
		t.Fatalf("存在的失败任务应能重试：found=%v err=%v", found, err)
	}
	rows, err := l.db.Query(ctx, "SELECT status FROM import_tasks WHERE id = 't-fail'")
	if err != nil || rows.Values[0][0] == "failed" {
		t.Fatalf("重试后任务不应还是 failed：%v %v", rows.Values, err)
	}
	found, err = a.RetryImportTask(ctx, "no-such-task")
	if err != nil || found {
		t.Fatalf("不存在的任务应是 found=false 且没有错误：found=%v err=%v", found, err)
	}
}

// ---- 假 AI 服务 ----

type inboxMockAI struct {
	srv           *httptest.Server
	failEmbed     atomic.Bool
	blockChat     atomic.Bool
	embedRequests atomic.Int32
	chatRequests  atomic.Int32
}

func newInboxMockAI(t *testing.T) *inboxMockAI {
	t.Helper()
	m := &inboxMockAI{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/embeddings"):
			m.embedRequests.Add(1)
			if m.failEmbed.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
				return
			}
			var req struct {
				Input []string `json:"input"`
			}
			_ = json.Unmarshal(raw, &req)
			data := make([]any, len(req.Input))
			for i := range req.Input {
				data[i] = map[string]any{"index": i, "embedding": []float64{3, 4, 0}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]any{"prompt_tokens": 5}})
		default:
			m.chatRequests.Add(1)
			if m.blockChat.Load() {
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
				return
			}
			var req struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.Unmarshal(raw, &req)
			var prompt struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			}
			if n := len(req.Messages); n > 0 {
				_ = json.Unmarshal([]byte(req.Messages[n-1].Content), &prompt)
			}
			assignments := make([]any, len(prompt.Items))
			for i, it := range prompt.Items {
				assignments[i] = map[string]any{"id": it.ID, "collection": "技术文章"}
			}
			reply, _ := json.Marshal(map[string]any{"assignments": assignments})
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"content": string(reply)}, "finish_reason": "stop"}},
				"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 3},
			})
		}
	}))
	t.Cleanup(m.srv.Close)
	return m
}

// configure 写入 AI 设置：chat 模型承担文本场景（含归类与 Wiki），embedding 模型承担语义索引。
func (m *inboxMockAI) configure(t *testing.T, b *Bridge, chat, embedding bool) {
	t.Helper()
	model := func(id, name string, caps map[string]any) map[string]any {
		return map[string]any{
			"id": id, "providerId": "p1", "provider": "openai", "apiProtocol": "openai", "apiKey": "sk-test-secret-key",
			"apiUrl": m.srv.URL, "model": name, "isDefault": id == "m1", "enabled": true, "capabilities": caps,
		}
	}
	models := []any{}
	routes := map[string]any{}
	if chat {
		models = append(models, model("m1", "gpt-4o-mini", map[string]any{"chat": true}))
		routes["mainText"], routes["fastText"] = "m1", "m1"
	}
	if embedding {
		models = append(models, model("m2", "text-embedding-3-small", map[string]any{"chat": false, "embedding": true}))
		routes["embedding"] = "m2"
	}
	payload := map[string]any{
		"aiProviders": []any{map[string]any{
			"id": "p1", "name": "mock", "provider": "openai", "apiProtocol": "openai", "apiKey": "sk-test-secret-key", "apiUrl": m.srv.URL, "enabled": true,
		}},
		"aiModels":           models,
		"modelRouteDefaults": routes,
	}
	if _, err := b.call(t.Context(), "settings:set", payload); err != nil {
		t.Fatalf("settings:set: %v", err)
	}
}

// AI 智能归类全程走真实处理器与假模型：取摘要 → 分批请求 → 应用计划；进度回调按批；未配置返回哨兵；请求进行中取消满足 context.Canceled。
func TestInboxAdapterSmartClassifyAgainstMockAI(t *testing.T) {
	ctx := t.Context()
	a, b, l := newInboxAdapterForTest(t)
	fx := seedInbox(t, b, l)
	ai := newInboxMockAI(t)

	sources, err := a.AIClassificationSources(ctx, []string{fx.plain1, fx.plain2, fx.plain1})
	if err != nil || len(sources) != 2 {
		t.Fatalf("摘要应按条目去重得到 2 条：%+v %v", sources, err)
	}
	if sources[0].Title == "" || sources[0].Excerpt == "" {
		t.Fatalf("摘要应带标题与摘录：%+v", sources[0])
	}
	if empty, err := a.AIClassificationSources(ctx, nil); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("没有 ID 应是空列表（非 nil）：%+v %v", empty, err)
	}
	if nonMatch, err := a.AIClassificationSources(ctx, []string{fx.review, fx.filed}); err != nil || len(nonMatch) != 0 {
		t.Fatalf("待复核与已归类的条目不符合归类条件：%+v %v", nonMatch, err)
	}

	if _, err := a.Classify(ctx, sources, nil, nil); !errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("未配置模型应返回 ErrAINotConfigured，实际：%v", err)
	}

	ai.configure(t, b, true, false)
	var progress [][2]int
	plan, err := a.Classify(ctx, sources, []string{"已有合集"}, func(done, total int) { progress = append(progress, [2]int{done, total}) })
	if err != nil || len(plan) != 2 {
		t.Fatalf("Classify 应返回 2 条计划：%+v %v", plan, err)
	}
	if plan[0].ItemID != sources[0].ItemID || plan[0].CollectionName != "技术文章" || plan[1].ItemID != sources[1].ItemID {
		t.Fatalf("计划应与摘要同序：%+v", plan)
	}
	if len(progress) != 1 || progress[0] != [2]int{1, 1} {
		t.Fatalf("两条内容只有一批，进度应是 (1,1)：%v", progress)
	}

	res, err := a.ApplyAIClassification(ctx, plan)
	if err != nil || res.Classified != 2 || res.Skipped != 0 || !slices.Equal(res.CreatedCollectionNames, []string{"技术文章"}) {
		t.Fatalf("应用计划应归类 2 条并新建合集：%+v %v", res, err)
	}
	list, _ := a.List(ctx)
	if list.Counts.Unclassified != 0 {
		t.Fatalf("归类后不应再有未归合集：%+v", list.Counts)
	}
	// 再应用一次：条目已有合集，被跳过
	again, err := a.ApplyAIClassification(ctx, plan)
	if err != nil || again.Classified != 0 || again.Skipped != 2 || len(again.CreatedCollectionNames) != 0 {
		t.Fatalf("重复应用应全部跳过：%+v %v", again, err)
	}
	if _, err := a.ApplyAIClassification(ctx, []services.InboxAIAssignment{{ItemID: "x", CollectionName: ""}}); err == nil {
		t.Fatal("无效的合集名应返回处理器的错误")
	}

	// 请求进行中被取消
	ai.blockChat.Store(true)
	cctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err := a.Classify(cctx, sources, nil, nil); !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("取消应满足 context.Canceled 或超时：%v", err)
	}
}

// 语义索引：未配置时状态为「未配置」且待索引数为 0；配置后手动执行一轮，回执与状态都对得上；失败进回执而不是 error；
// 不同时跑两轮。
func TestInboxAdapterSemanticIndexingAgainstMockAI(t *testing.T) {
	ctx := t.Context()
	a, b, l := newInboxAdapterForTest(t)
	seedInbox(t, b, l)
	ai := newInboxMockAI(t)

	status, err := a.SemanticStatus(ctx)
	if err != nil || status.Configured || status.Pending() != 0 {
		t.Fatalf("未配置 embedding 时应是未配置、待索引 0：%+v %v", status, err)
	}
	notice, err := a.RunSemanticIndexing(ctx)
	if err != nil || notice == nil || notice.Kind != services.InboxSemanticNotConfigured {
		t.Fatalf("未配置时回执应是 not-configured：%+v %v", notice, err)
	}
	if a.SemanticIndexing() {
		t.Fatal("执行结束后「正在执行」应复位")
	}

	ai.configure(t, b, true, true)
	status, err = a.SemanticStatus(ctx)
	if err != nil || !status.Configured || status.EligibleItems != 4 || status.IndexedItems != 0 || status.Pending() != 4 {
		t.Fatalf("配置后应有 4 条待索引：%+v %v", status, err)
	}

	notice, err = a.RunSemanticIndexing(ctx)
	if err != nil || notice == nil || notice.Kind != services.InboxSemanticDone || notice.Indexed != 4 {
		t.Fatalf("应索引 4 条：%+v %v", notice, err)
	}
	status, _ = a.SemanticStatus(ctx)
	if status.Pending() != 0 || status.IndexedItems != 4 {
		t.Fatalf("索引后应没有待办：%+v", status)
	}
	list, _ := a.List(ctx)
	if _, ok := findInboxItem(list, services.InboxKindSemanticPending, ""); ok || list.Counts.SemanticPending != 0 {
		t.Fatalf("索引后不应再有语义索引聚合行：%+v", list.Counts)
	}
	notice, err = a.RunSemanticIndexing(ctx)
	if err != nil || notice == nil || notice.Kind != services.InboxSemanticNothing {
		t.Fatalf("没有待办时回执应是 nothing：%+v %v", notice, err)
	}

	// 失败：新条目 + embedding 服务返回 500
	createOpsItem(t, b, map[string]any{"title": "新条目", "content": "新增的、尚未建立索引的正文内容，长度足够。"})
	ai.failEmbed.Store(true)
	notice, err = a.RunSemanticIndexing(ctx)
	if err != nil || notice == nil || notice.Kind != services.InboxSemanticFailed || !strings.Contains(notice.Message, "500") {
		t.Fatalf("embedding 失败应归在回执里（failed，带 HTTP 状态）：%+v %v", notice, err)
	}
	if a.SemanticIndexing() {
		t.Fatal("失败后「正在执行」也应复位")
	}
	status, _ = a.SemanticStatus(ctx)
	if status.Pending() != 1 {
		t.Fatalf("失败的条目应仍待索引：%+v", status)
	}
}

// 同一时刻只跑一轮：正在执行时再调用返回 nil（没有回执），不另起一轮。
func TestInboxAdapterSemanticIndexingRejectsOverlap(t *testing.T) {
	ctx := t.Context()
	a, _, _ := newInboxAdapterForTest(t)

	a.indexing.Store(true)
	notice, err := a.RunSemanticIndexing(ctx)
	if err != nil || notice != nil {
		t.Fatalf("已有一轮在跑时应返回 nil 回执：%+v %v", notice, err)
	}
	if !a.SemanticIndexing() {
		t.Fatal("被拒绝的调用不能把别人的「正在执行」标记清掉")
	}
}

// Wiki：没配文本模型时候选都计入「等待」且模型未就绪；配置后候选变成「可编译」且待办与 Wiki 预览的计数一致；待复核条目单独计数。
func TestInboxAdapterWikiCandidatesAndModelReady(t *testing.T) {
	ctx := t.Context()
	a, b, l := newInboxAdapterForTest(t)
	seedInbox(t, b, l)
	ai := newInboxMockAI(t)

	ready, err := a.WikiModelReady(ctx)
	if err != nil || ready {
		t.Fatalf("未配置时模型应未就绪：%v %v", ready, err)
	}
	c, err := a.WikiCandidates(ctx)
	if err != nil || c.Waiting != 3 || c.Review != 1 || c.Ready != 0 {
		t.Fatalf("未配置时 3 条等待、1 条需复核：%+v %v", c, err)
	}

	ai.configure(t, b, true, false)
	ready, err = a.WikiModelReady(ctx)
	if err != nil || !ready {
		t.Fatalf("配置后模型应就绪：%v %v", ready, err)
	}
	c, err = a.WikiCandidates(ctx)
	if err != nil || c.Ready != 3 || c.Review != 1 || c.Waiting != 0 || c.Upgrade != 0 {
		t.Fatalf("配置后 3 条可编译、1 条需复核：%+v %v", c, err)
	}
}

// 处理器出错（库不可用）时读取返回错误，而不是空结果：界面据此显示加载失败而不是「已处理完」。
func TestInboxAdapterReadErrorsAreNotEmptyResults(t *testing.T) {
	a, b, _ := newInboxAdapterForTest(t)
	delete(b.handlers, "inbox:list")
	if _, err := a.List(t.Context()); err == nil {
		t.Fatal("处理器缺失时 List 应返回错误")
	}
	delete(b.handlers, "semantic:status")
	// 未配置 embedding 时不会调用 semantic:status，所以不报错；配置后才会暴露处理器缺失
	ai := newInboxMockAI(t)
	ai.configure(t, b, true, true)
	if _, err := a.SemanticStatus(t.Context()); err == nil {
		t.Fatal("处理器缺失时 SemanticStatus 应返回错误")
	}
}
