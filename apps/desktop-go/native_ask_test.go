package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

const askTestSecret = "sk-test-secret-key"

// newAskAdapterForTest 用真实处理器装配问答适配器：会话、条目、Wiki、语义检索、本文问答、AI 代理与设置都是生产实现，
// 数据库与用户目录都在 t.TempDir() 下，不碰真实的 %APPDATA%\GuiZhi。
func newAskAdapterForTest(t *testing.T) (*askAdapter, *Bridge, *Library) {
	t.Helper()
	_, b, l := newLibraryAIAdapterForTest(t)
	l.registerAskSessions(b)
	l.registerSemantic(b)
	l.registerWiki(b)
	l.registerArticleAsk(b)
	return newAskAdapter(b), b, l
}

// askFakeLLM 是本地的假 OpenAI 端点：按请求序号回放脚本，流式请求回 SSE，非流式回 JSON。
type askFakeLLM struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	bodies []string
	auths  []string
	// script 按请求序号（从 0 起）给出这次请求的响应；返回 nil 表示用 fallback。
	script   func(n int, body string) *askLLMReply
	fallback *askLLMReply
	arrived  chan struct{}
}

type askLLMReply struct {
	Status int
	// Chunks 是流式请求的正文分块；非流式请求把它们拼起来当作整段内容。
	Chunks []string
	Finish string
	// Raw 非空时原样当响应体返回（错误体）。
	Raw string
	// Hang 为 true 时不回应，等客户端断开。
	Hang bool
}

func newAskFakeLLM(t *testing.T, script func(n int, body string) *askLLMReply) *askFakeLLM {
	t.Helper()
	f := &askFakeLLM{t: t, script: script, arrived: make(chan struct{}, 16)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *askFakeLLM) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	n := len(f.bodies)
	f.bodies = append(f.bodies, string(raw))
	f.auths = append(f.auths, r.Header.Get("Authorization"))
	f.mu.Unlock()
	select {
	case f.arrived <- struct{}{}:
	default:
	}
	reply := f.fallback
	if f.script != nil {
		if got := f.script(n, string(raw)); got != nil {
			reply = got
		}
	}
	if reply == nil {
		reply = &askLLMReply{Chunks: []string{"ok"}}
	}
	if reply.Hang {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
		return
	}
	finish := reply.Finish
	if finish == "" {
		finish = "stop"
	}
	if reply.Status != 0 && reply.Status != http.StatusOK {
		w.WriteHeader(reply.Status)
		_, _ = w.Write([]byte(reply.Raw))
		return
	}
	var req struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(raw, &req)
	if !req.Stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": strings.Join(reply.Chunks, "")}, "finish_reason": finish}},
			"usage":   map[string]any{"prompt_tokens": 5, "completion_tokens": 3},
		})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	for _, chunk := range reply.Chunks {
		payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": chunk}}}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
	done, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": finish}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", done)
}

func (f *askFakeLLM) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func (f *askFakeLLM) request(n int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[n]
}

func (f *askFakeLLM) waitArrived(t *testing.T) {
	t.Helper()
	select {
	case <-f.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("假端点一直没有收到请求")
	}
}

func askErrorLog(t *testing.T, l *Library) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(l.paths.LogsDir(), "error.log"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func askQuestionItem(t *testing.T, b *Bridge, title, content string) string {
	t.Helper()
	return createOpsItem(t, b, map[string]any{"title": title, "content": content, "itemType": "note"})
}

// ---- 会话持久化 ----

func TestAskAdapterSessionsRoundTripThroughRealHandlers(t *testing.T) {
	ctx := t.Context()
	a, _, _ := newAskAdapterForTest(t)

	if rec, err := a.GetSession(ctx, "missing"); err != nil || rec != nil {
		t.Fatalf("不存在的会话应读回 nil：%v %v", rec, err)
	}
	saved, err := a.SaveSession(ctx, services.SaveAskSessionInput{ID: "s1", Title: "第一个问题", MessagesJSON: `[{"id":"m1","question":"归知是什么","answer":"一个本地知识库"}]`})
	if err != nil || saved.ID != "s1" || saved.Title != "第一个问题" || saved.IsArticle() || saved.CreatedAt == 0 {
		t.Fatalf("保存：%+v %v", saved, err)
	}
	web := false
	art, err := a.SaveSession(ctx, services.SaveAskSessionInput{
		ID: "a1", Title: "本文提问", MessagesJSON: "[]", Scope: services.AskScopeArticle, ItemID: "item-1", ArticleTitle: "某文章",
		WebEnabled: &web, Target: &aiflow.ArticleTarget{ItemID: "item-1", View: "body"},
	})
	if err != nil || !art.IsArticle() || art.WebEnabledOrDefault() || art.Target == nil || art.Target.ItemID != "item-1" || art.ArticleTitle != "某文章" {
		t.Fatalf("保存本文会话：%+v %v", art, err)
	}
	if _, err := a.SaveSession(ctx, services.SaveAskSessionInput{ID: "bad", Title: "x", MessagesJSON: "[]", Scope: services.AskScopeArticle, ItemID: "item-1"}); err == nil {
		t.Fatal("本文会话缺少目标应被后端拒绝")
	}

	got, err := a.GetSession(ctx, "s1")
	if err != nil || got == nil || !strings.Contains(got.MessagesJSON, "归知是什么") {
		t.Fatalf("读回：%+v %v", got, err)
	}

	list, err := a.ListSessions(ctx, services.AskSessionFilter{Scope: services.AskScopeArticle, ItemID: "item-1"})
	if err != nil || len(list) != 1 || list[0].ID != "a1" || list[0].WebEnabledOrDefault() {
		t.Fatalf("按文章筛选：%+v %v", list, err)
	}

	page, err := a.QuerySessions(ctx, services.AskSessionQuery{Limit: 10})
	if err != nil || len(page.Entries) != 2 || page.NextCursor != nil || len(page.Articles) != 1 || page.Articles[0].ID != "item-1" || page.Articles[0].Title != "某文章" {
		t.Fatalf("分页查询应带文章列表：%+v %v", page, err)
	}
	small, err := a.QuerySessions(ctx, services.AskSessionQuery{Limit: 1})
	if err != nil || len(small.Entries) != 1 || small.NextCursor == nil {
		t.Fatalf("一页一条应给出游标：%+v %v", small, err)
	}
	rest, err := a.QuerySessions(ctx, services.AskSessionQuery{Limit: 1, Cursor: small.NextCursor})
	if err != nil || len(rest.Entries) != 1 || rest.Entries[0].ID == small.Entries[0].ID || rest.Articles != nil {
		t.Fatalf("下一页：%+v %v", rest, err)
	}
	found, err := a.QuerySessions(ctx, services.AskSessionQuery{Search: "本地知识库"})
	if err != nil || len(found.Entries) != 1 || found.Entries[0].ID != "s1" {
		t.Fatalf("全文搜索：%+v %v", found, err)
	}

	title, pinned := "改过的标题", true
	meta, err := a.UpdateSessionMeta(ctx, "s1", services.AskSessionMetaPatch{Title: &title, Pinned: &pinned})
	if err != nil || meta.Title != title || !meta.Pinned {
		t.Fatalf("改标题与置顶：%+v %v", meta, err)
	}
	empty := "  "
	if _, err := a.UpdateSessionMeta(ctx, "s1", services.AskSessionMetaPatch{Title: &empty}); err == nil {
		t.Fatal("空标题应被拒绝")
	}
	if _, err := a.UpdateSessionMeta(ctx, "nope", services.AskSessionMetaPatch{Pinned: &pinned}); err == nil {
		t.Fatal("会话不存在应报错")
	}

	if ok, err := a.DeleteSession(ctx, "s1"); err != nil || !ok {
		t.Fatalf("删除：%v %v", ok, err)
	}
	if ok, err := a.DeleteSession(ctx, "s1"); err != nil || ok {
		t.Fatalf("重复删除应返回 false：%v %v", ok, err)
	}
}

func TestAskAdapterConfigItemInfoAndSanitize(t *testing.T) {
	ctx := t.Context()
	a, b, _ := newAskAdapterForTest(t)

	if ok, err := a.QAConfigured(ctx); err != nil || ok {
		t.Fatalf("没有模型时问答未配置：%v %v", ok, err)
	}
	llm := newAskFakeLLM(t, nil)
	configureAITestModels(t, b, llm.srv.URL, "openai", "gpt-4o-mini")
	if ok, err := a.QAConfigured(ctx); err != nil || !ok {
		t.Fatalf("配置模型后问答已配置：%v %v", ok, err)
	}
	if ok, err := a.WebSearchConfigured(ctx); err != nil || ok {
		t.Fatalf("没配搜索服务时联网搜索未配置：%v %v", ok, err)
	}

	id := askQuestionItem(t, b, "一篇笔记", "正文")
	if item, found, err := a.ItemInfo(ctx, id); err != nil || !found || item.Title != "一篇笔记" || item.Deleted {
		t.Fatalf("条目信息：%+v %v %v", item, found, err)
	}
	if _, err := b.call(ctx, "knowledge:moveToTrash", []string{id}); err != nil {
		t.Fatalf("移到回收站：%v", err)
	}
	if item, found, err := a.ItemInfo(ctx, id); err != nil || !found || !item.Deleted {
		t.Fatalf("回收站里的条目仍读得到并标记已删除：%+v %v %v", item, found, err)
	}
	if _, found, err := a.ItemInfo(ctx, "no-such-item"); err != nil || found {
		t.Fatalf("不存在的条目：%v %v", found, err)
	}

	in := `[{"id":"m1","question":"q","answer":"答案 [1]","sources":[{"ordinal":1,"kind":"item","refId":"gone","title":"T","evidence":{"kind":"item","title":"T","text":"机密正文","sourceId":"gone","capturedAt":1}}]}]`
	out, err := a.SanitizeConversation(in, []string{"gone"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "机密正文") || !strings.Contains(out, `"answer":"答案 [1]"`) {
		t.Fatalf("应清掉证据正文并保留回答：%s", out)
	}
	if _, err := a.SanitizeConversation("not json", nil); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

// ---- 全局问答 ----

func TestAskAdapterAskKnowledgeStreamsAnswerWithSourcesAndEvidence(t *testing.T) {
	ctx := t.Context()
	a, b, _ := newAskAdapterForTest(t)
	itemID := askQuestionItem(t, b, "归知笔记", "归知是本地优先的个人知识库，数据都存在本机。")

	llm := newAskFakeLLM(t, func(n int, body string) *askLLMReply {
		switch n {
		case 0:
			return &askLLMReply{Chunks: []string{`{"action":"search","query":"归知"}`}}
		case 1:
			return &askLLMReply{Chunks: []string{`{"action":"read","target":1}`}}
		default:
			return &askLLMReply{Chunks: []string{`{"action":"answer","text":"归知是`, `本地优先的知识库 [1]`, `。"}`}}
		}
	})
	configureAITestModels(t, b, llm.srv.URL, "openai", "gpt-4o-mini")

	var (
		mu       sync.Mutex
		steps    []string
		texts    []string
		evidence [][]services.AskSource
	)
	answer, err := a.AskKnowledge(ctx, services.AskRequest{Question: "归知是什么？"}, services.AskCallbacks{
		OnStep: func(s string) { mu.Lock(); steps = append(steps, s); mu.Unlock() },
		OnText: func(s string) { mu.Lock(); texts = append(texts, s); mu.Unlock() },
		OnEvidence: func(s []services.AskSource) {
			mu.Lock()
			evidence = append(evidence, s)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("问答失败：%v", err)
	}
	if answer.Text != "归知是本地优先的知识库 [1]。" || answer.Truncated || answer.UsedFallback || answer.Model == "" {
		t.Fatalf("回答：%+v", answer)
	}
	if len(answer.Sources) != 1 || answer.Sources[0].RefID != itemID || answer.Sources[0].Kind != "item" || answer.Sources[0].Ordinal != 1 || answer.Sources[0].Evidence == nil {
		t.Fatalf("来源应是被阅读的条目并带证据快照：%+v", answer.Sources)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(steps) < 2 || !strings.Contains(steps[0], "归知") {
		t.Fatalf("进度应先报检索词：%v", steps)
	}
	if len(texts) < 2 || texts[len(texts)-1] != answer.Text {
		t.Fatalf("流式文本应逐步变长，最后一次等于最终回答：%v", texts)
	}
	for i := 1; i < len(texts); i++ {
		if !strings.HasPrefix(texts[i], texts[i-1]) {
			t.Fatalf("流式文本是累计全文，应一路延长：%v", texts)
		}
	}
	if len(evidence) == 0 || len(evidence[len(evidence)-1]) != 1 {
		t.Fatalf("阅读之后应回调证据：%+v", evidence)
	}
	if llm.requestCount() != 3 {
		t.Fatalf("检索、阅读、回答各一次请求，实际 %d", llm.requestCount())
	}
	for _, auth := range llm.auths {
		if auth != "Bearer "+askTestSecret {
			t.Fatalf("请求应带上配置的密钥：%q", auth)
		}
	}
}

func TestAskAdapterAskKnowledgeErrorClasses(t *testing.T) {
	ctx := t.Context()
	a, b, l := newAskAdapterForTest(t)

	// 未配置：不发任何请求，返回哨兵。
	if _, err := a.AskKnowledge(ctx, services.AskRequest{Question: "q"}, services.AskCallbacks{}); !errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("未配置应返回 ErrAINotConfigured：%v", err)
	}
	if err := a.AskArticle(ctx, services.ArticleAskRequest{Question: "q", Target: aiflow.ArticleTarget{ItemID: "x", View: "body"}}, func(aiflow.ArticlePatch) {}); !errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("本文问答未配置也应返回 ErrAINotConfigured：%v", err)
	}

	llm := newAskFakeLLM(t, nil)
	llm.fallback = &askLLMReply{Chunks: []string{`{"action":"search","query":"完全不存在的词"}`}}
	configureAITestModels(t, b, llm.srv.URL, "openai", "gpt-4o-mini")

	// 没有相关资料：知识库是空的，连续检索不到会收口成 ErrAskNoSource。
	if _, err := a.AskKnowledge(ctx, services.AskRequest{Question: "完全不存在的词是什么"}, services.AskCallbacks{}); !errors.Is(err, services.ErrAskNoSource) {
		t.Fatalf("没有资料应返回 ErrAskNoSource：%v", err)
	}

	// 上游报错：普通错误，文本里不能出现密钥，日志里也没有。
	llm.script = func(int, string) *askLLMReply {
		return &askLLMReply{Status: http.StatusUnauthorized, Raw: `{"error":{"message":"invalid api key ` + askTestSecret + `"}}`}
	}
	_, err := a.AskKnowledge(ctx, services.AskRequest{Question: "q"}, services.AskCallbacks{})
	if err == nil || errors.Is(err, services.ErrAINotConfigured) || errors.Is(err, services.ErrAskNoSource) || errors.Is(err, context.Canceled) {
		t.Fatalf("上游 401 应是普通错误：%v", err)
	}
	if strings.Contains(err.Error(), askTestSecret) {
		t.Fatalf("错误文本泄露了 API Key：%v", err)
	}
	if log := askErrorLog(t, l); strings.Contains(log, askTestSecret) {
		t.Fatalf("日志泄露了 API Key：%s", log)
	}
}

func TestAskAdapterAskKnowledgeCancelReturnsCanceled(t *testing.T) {
	a, b, _ := newAskAdapterForTest(t)
	llm := newAskFakeLLM(t, func(int, string) *askLLMReply { return &askLLMReply{Hang: true} })
	configureAITestModels(t, b, llm.srv.URL, "openai", "gpt-4o-mini")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := a.AskKnowledge(ctx, services.AskRequest{Question: "q"}, services.AskCallbacks{})
		done <- err
	}()
	llm.waitArrived(t)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消应返回 context.Canceled：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("取消之后问答一直没有返回")
	}
}

// 上游先拒绝 max_tokens 参数、重试成功的场景会写一条诊断日志（带当前请求的密钥原文）：日志必须已经抹掉密钥。
func TestAskAdapterDiagnosticLogNeverContainsAPIKey(t *testing.T) {
	ctx := t.Context()
	a, b, l := newAskAdapterForTest(t)
	llm := newAskFakeLLM(t, nil)
	configureAITestModels(t, b, llm.srv.URL, "openai", "gpt-4o-mini")
	itemID := askQuestionItem(t, b, "本文", "正文内容")

	// 本文问答（不联网）：第一次请求被拒绝，重试后成功。
	llm.script = func(n int, _ string) *askLLMReply {
		if n == 0 {
			return &askLLMReply{Status: http.StatusBadRequest, Raw: `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model, key ` + askTestSecret + `"}}`}
		}
		return &askLLMReply{Chunks: []string{"回答内容"}}
	}
	err := a.AskArticle(ctx, services.ArticleAskRequest{
		Target: aiflow.ArticleTarget{ItemID: itemID, View: "body"}, Question: "这篇讲什么", RequestID: "req-log-1",
	}, func(aiflow.ArticlePatch) {})
	if err != nil {
		t.Fatalf("重试后应成功：%v", err)
	}
	log := askErrorLog(t, l)
	if !strings.Contains(log, "Token parameter mismatch") {
		t.Fatalf("参数不兼容重试应留下诊断日志：%q", log)
	}
	if strings.Contains(log, askTestSecret) {
		t.Fatalf("诊断日志泄露了 API Key：%s", log)
	}
}

// ---- 围绕本文提问 ----

func TestAskAdapterAskArticleStreamsPatches(t *testing.T) {
	ctx := t.Context()
	a, b, _ := newAskAdapterForTest(t)
	llm := newAskFakeLLM(t, func(int, string) *askLLMReply {
		return &askLLMReply{Chunks: []string{"这篇文章", "讲的是知识库", "。"}}
	})
	configureAITestModels(t, b, llm.srv.URL, "openai", "gpt-4o-mini")
	itemID := askQuestionItem(t, b, "知识库设计", "知识库要本地优先，数据放在本机，AI 请求走壳代理。")

	var (
		mu      sync.Mutex
		patches []aiflow.ArticlePatch
	)
	err := a.AskArticle(ctx, services.ArticleAskRequest{
		Target: aiflow.ArticleTarget{ItemID: itemID, View: "body"}, Question: "这篇讲什么", RequestID: "req-article-1",
	}, func(p aiflow.ArticlePatch) { mu.Lock(); patches = append(patches, p); mu.Unlock() })
	if err != nil {
		t.Fatalf("本文问答失败：%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	var sawContext, sawStep bool
	var answers []string
	last := patches[len(patches)-1]
	for _, p := range patches {
		sawContext = sawContext || (p.Context != nil && len(p.Sources) > 0 && p.Sources[0].Evidence != nil)
		sawStep = sawStep || (p.Step != nil && *p.Step == "读取本文")
		if p.Answer != nil {
			answers = append(answers, *p.Answer)
		}
	}
	if !sawContext || !sawStep {
		t.Fatalf("应先读取本文并带上带证据的来源：context=%v step=%v", sawContext, sawStep)
	}
	if len(answers) < 2 || answers[len(answers)-1] != "这篇文章讲的是知识库。" {
		t.Fatalf("回答应流式累计到最终全文：%v", answers)
	}
	if last.Status == nil || *last.Status != services.AskStatusDone || !last.ClearStep || last.Truncated == nil || *last.Truncated {
		t.Fatalf("最后一个补丁应标记完成并清掉步骤：%+v", last)
	}
	if !strings.Contains(llm.request(0), `"stream":true`) {
		t.Fatalf("本文回答应走流式请求：%s", llm.request(0))
	}
}

func TestAskAdapterAskArticleMissingArticleAndCancel(t *testing.T) {
	ctx := t.Context()
	a, b, _ := newAskAdapterForTest(t)
	llm := newAskFakeLLM(t, func(int, string) *askLLMReply { return &askLLMReply{Hang: true} })
	configureAITestModels(t, b, llm.srv.URL, "openai", "gpt-4o-mini")

	err := a.AskArticle(ctx, services.ArticleAskRequest{
		Target: aiflow.ArticleTarget{ItemID: "no-such-item", View: "body"}, Question: "q", RequestID: "req-missing",
	}, func(aiflow.ArticlePatch) {})
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, services.ErrAINotConfigured) {
		t.Fatalf("文章不存在应是带原因的普通错误：%v", err)
	}

	itemID := askQuestionItem(t, b, "T", "正文")
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- a.AskArticle(cctx, services.ArticleAskRequest{
			Target: aiflow.ArticleTarget{ItemID: itemID, View: "body"}, Question: "q", RequestID: "req-cancel",
		}, func(aiflow.ArticlePatch) {})
	}()
	llm.waitArrived(t)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消应返回 context.Canceled：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("取消之后本文问答一直没有返回")
	}
}
