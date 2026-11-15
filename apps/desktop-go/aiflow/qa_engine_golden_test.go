package aiflow

// 全局问答引擎（Agent 循环 + 单发兜底）的 golden 对拍：夹具来自
// apps/desktop/tests/unit/knowledge-ai/aiflow-golden-qa-engine.test.ts（真实 TS 的 askKnowledgeBase + 脚本化假依赖）。
// 同一份脚本驱动 Go 的 QAEngine，对依赖的每次调用（含对话的消息与选项）、各回调序列和最终结果逐项比对。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

type qaChatStep struct {
	Content     string  `json:"content"`
	Model       string  `json:"model"`
	Finish      string  `json:"finish"`
	Chunk       int     `json:"chunk"`
	Throws      *string `json:"throws"`
	AbortDuring bool    `json:"abortDuring"`
}

type qaEngineCase struct {
	Name     string `json:"name"`
	Question string `json:"question"`
	History  []struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	} `json:"history"`
	Chat       []qaChatStep    `json:"chat"`
	Items      []any           `json:"items"`
	Read       map[string]any  `json:"read"`
	Catalog    any             `json:"catalog"`
	WikiSearch *[]any          `json:"wikiSearch"`
	WikiPages  *map[string]any `json:"wikiPages"`
	Stream     bool            `json:"stream"`
	Evidence   *bool           `json:"evidence"`
	Abort      string          `json:"abort"`
	AbortOn    *struct {
		Call  string `json:"call"`
		Index int    `json:"index"`
	} `json:"abortOn"`
	AbortOnStep *string `json:"abortOnStep"`
	AbortOnText bool    `json:"abortOnText"`
	Now         int64   `json:"now"`
}

// qaPromptLimit 对应 TS 的 BODY_LIMIT：提示词超过它只记指纹（外加末尾 300 个码点）。
const qaPromptLimit = 2000

func compactPrompt(text string) any {
	if u16Len(text) <= qaPromptLimit {
		return text
	}
	d := digestOf(text)
	points := []rune(text)
	return map[string]any{
		"length": d.Length, "sha256": d.SHA256, "head": d.Head, "tail": d.Tail,
		"last": string(points[max(0, len(points)-300):]),
	}
}

// qaPieces 对应 TS 的 pieces：按码点分块。
func qaPieces(text string, size int) []string {
	points := []rune(text)
	var out []string
	for i := 0; i < len(points); i += size {
		out = append(out, string(points[i:min(i+size, len(points))]))
	}
	return out
}

// qaRefOutput 对应 TS 的 compactRef：证据文本长了只留指纹。
func qaRefOutput(t *testing.T, ref QASourceRef) map[string]any {
	t.Helper()
	out := map[string]any{"ordinal": ref.Ordinal, "kind": string(ref.Kind), "refId": ref.RefID, "title": ref.Title}
	if ref.Evidence != nil {
		raw, err := json.Marshal(ref.Evidence)
		if err != nil {
			t.Fatalf("证据无法序列化：%v", err)
		}
		var evidence map[string]any
		if err := json.Unmarshal(raw, &evidence); err != nil {
			t.Fatalf("证据无法解析：%v", err)
		}
		evidence["text"] = compactText(ref.Evidence.Text)
		out["evidence"] = evidence
	}
	return out
}

func qaRefOutputs(t *testing.T, refs []QASourceRef) []map[string]any {
	out := make([]map[string]any, len(refs))
	for i, ref := range refs {
		out[i] = qaRefOutput(t, ref)
	}
	return out
}

// qaHarness 把一个用例的脚本变成 QADeps，并记录对依赖的调用与各回调。
type qaHarness struct {
	t           *testing.T
	c           *qaEngineCase
	ctx         context.Context
	cancel      context.CancelFunc
	counts      map[string]int
	calls       []map[string]any
	steps       []string
	answerTexts []any
	evidence    [][]map[string]any
	chatQueue   []qaChatStep
	itemQueue   []any
	wikiQueue   []any
}

func (h *qaHarness) begin(kind string, detail map[string]any) {
	index := h.counts[kind]
	h.counts[kind]++
	call := map[string]any{"call": kind}
	for key, value := range detail {
		call[key] = value
	}
	h.calls = append(h.calls, call)
	if h.c.AbortOn != nil && h.c.AbortOn.Call == kind && h.c.AbortOn.Index == index {
		h.cancel()
	}
}

// remarshal 经 JSON 往返把通用结构转成目标类型。
func remarshal[T any](t *testing.T, v any) T {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("无法序列化：%v", err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("无法解析：%v", err)
	}
	return out
}

// qaTake 对应 TS 的 queued：取队首应答；队列用尽或应答是 {throws} 都以错误返回。
func qaTake(queue *[]any, what string) ([]any, error) {
	if len(*queue) == 0 {
		return nil, fmt.Errorf("脚本已用尽：%s", what)
	}
	next := (*queue)[0]
	*queue = (*queue)[1:]
	if fail, ok := next.(map[string]any); ok {
		return nil, errors.New(fail["throws"].(string))
	}
	return next.([]any), nil
}

// qaLookup 对应 TS 的 lookup：按 id 取条目 / 页面；null 表示不存在，{throws} 表示读取失败。
func qaLookup(table map[string]any, id, what string) (map[string]any, error) {
	value, ok := table[id]
	if !ok {
		return nil, fmt.Errorf("脚本里没有%s %s", what, id)
	}
	if value == nil {
		return nil, nil
	}
	entry := value.(map[string]any)
	if throws, ok := entry["throws"]; ok {
		return nil, errors.New(throws.(string))
	}
	return entry, nil
}

func (h *qaHarness) chat(ctx context.Context, messages []ChatMessage, opts ScenarioChatOptions) (ScenarioChatResult, error) {
	compact := make([]map[string]any, len(messages))
	for i, message := range messages {
		var content any = compactPrompt(message.Content)
		switch message.Content {
		case QAAgentSystemPrompt:
			content = "$agent"
		case QASystemPrompt:
			content = "$qa"
		}
		compact[i] = map[string]any{"role": message.Role, "content": content}
	}
	var temperature any
	if opts.Temperature != nil {
		temperature = *opts.Temperature
	}
	h.begin("chat", map[string]any{
		"messages": compact, "temperature": temperature, "maxTokens": opts.MaxTokens,
		"streaming": opts.OnDelta != nil, "signal": ctx == h.ctx,
	})
	if len(h.chatQueue) == 0 {
		return ScenarioChatResult{}, errors.New("脚本已用尽：chat")
	}
	step := h.chatQueue[0]
	h.chatQueue = h.chatQueue[1:]
	if opts.OnDelta != nil && step.Chunk > 0 {
		for _, piece := range qaPieces(step.Content, step.Chunk) {
			opts.OnDelta(piece)
		}
	}
	if step.AbortDuring {
		h.cancel()
	}
	if step.Throws != nil {
		if *step.Throws == "$abort" {
			return ScenarioChatResult{}, ErrAborted
		}
		return ScenarioChatResult{}, errors.New(*step.Throws)
	}
	model := step.Model
	if model == "" {
		model = "test-model"
	}
	return ScenarioChatResult{Content: step.Content, Model: model, FinishReason: step.Finish}, nil
}

func (h *qaHarness) searchItems(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	h.begin("searchItems", map[string]any{"query": query, "limit": limit, "signal": ctx == h.ctx})
	list, err := qaTake(&h.itemQueue, "searchItems")
	if err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, len(list))
	for _, entry := range list {
		raw := entry.(map[string]any)
		hit := SearchHit{ID: raw["id"].(string), Title: raw["title"].(string), Snippet: raw["snippet"].(string)}
		if v, ok := raw["matchText"].(string); ok {
			hit.MatchText = v
		}
		if v, ok := raw["reviewStatus"].(string); ok {
			hit.ReviewStatus = v
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

func (h *qaHarness) readItem(_ context.Context, id string) (*QAItemContent, error) {
	h.begin("readItem", map[string]any{"id": id})
	raw, err := qaLookup(h.c.Read, id, "条目")
	if err != nil || raw == nil {
		return nil, err
	}
	item := &QAItemContent{Title: raw["title"].(string), Content: raw["content"].(string)}
	if v, ok := raw["transcript"].(string); ok {
		item.Transcript = &v
	}
	if v, ok := raw["updatedAt"].(float64); ok {
		n := int64(v)
		item.UpdatedAt = &n
	}
	if v, ok := raw["reviewStatus"].(string); ok {
		item.ReviewStatus = v
	}
	if v, ok := raw["reviewReasons"].([]any); ok {
		item.ReviewReasons = []string{}
		for _, reason := range v {
			item.ReviewReasons = append(item.ReviewReasons, reason.(string))
		}
	}
	return item, nil
}

func (h *qaHarness) wikiCatalog(context.Context) ([]WikiCatalogEntry, error) {
	h.begin("getWikiCatalog", map[string]any{})
	if fail, ok := h.c.Catalog.(map[string]any); ok {
		return nil, errors.New(fail["throws"].(string))
	}
	return remarshal[[]WikiCatalogEntry](h.t, h.c.Catalog), nil
}

func (h *qaHarness) searchWikiPages(_ context.Context, query string, limit int) ([]WikiSearchHit, error) {
	h.begin("searchWikiPages", map[string]any{"query": query, "limit": limit})
	list, err := qaTake(&h.wikiQueue, "searchWikiPages")
	if err != nil {
		return nil, err
	}
	return remarshal[[]WikiSearchHit](h.t, list), nil
}

func (h *qaHarness) readWikiPage(_ context.Context, id string) (*WikiPageDetail, error) {
	h.begin("readWikiPage", map[string]any{"id": id})
	raw, err := qaLookup(*h.c.WikiPages, id, "页面")
	if err != nil || raw == nil {
		return nil, err
	}
	detail := remarshal[WikiPageDetail](h.t, raw)
	return &detail, nil
}

func runQAEngineCase(t *testing.T, c *qaEngineCase) map[string]any {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if c.Abort == "before" {
		cancel()
	}
	h := &qaHarness{
		t: t, c: c, ctx: ctx, cancel: cancel, counts: map[string]int{},
		chatQueue: append([]qaChatStep(nil), c.Chat...),
		itemQueue: append([]any(nil), c.Items...),
	}
	if c.WikiSearch != nil {
		h.wikiQueue = append([]any(nil), (*c.WikiSearch)...)
	}

	deps := QADeps{Chat: h.chat, SearchItems: h.searchItems, ReadItem: h.readItem}
	if c.Catalog != nil {
		deps.WikiCatalog = h.wikiCatalog
	}
	if c.WikiSearch != nil {
		deps.SearchWikiPages = h.searchWikiPages
	}
	if c.WikiPages != nil {
		deps.ReadWikiPage = h.readWikiPage
	}
	if c.Evidence == nil || *c.Evidence {
		deps.OnEvidence = func(refs []QASourceRef) { h.evidence = append(h.evidence, qaRefOutputs(t, refs)) }
	}

	var history []QaTurn
	for _, turn := range c.History {
		history = append(history, QaTurn{Question: turn.Question, Answer: turn.Answer})
	}
	var onAnswerText func(string)
	if c.Stream {
		onAnswerText = func(text string) {
			h.answerTexts = append(h.answerTexts, compactText(text))
			if c.AbortOnText {
				cancel()
			}
		}
	}
	engine := &QAEngine{Deps: deps, Now: func() int64 { return c.Now }}
	answer, err := engine.Ask(ctx, c.Question, history, func(description string) {
		h.steps = append(h.steps, description)
		if c.AbortOnStep != nil && description == *c.AbortOnStep {
			cancel()
		}
	}, onAnswerText)

	out := map[string]any{}
	if err != nil {
		info := flowErrorInfo(err, false)
		if errors.Is(err, ErrQANoSource) {
			info["noSource"] = true
		}
		out["error"] = info
	} else {
		out["answer"] = map[string]any{
			"text": compactText(answer.Text), "model": answer.Model,
			"usedFallback": answer.UsedFallback, "truncated": answer.Truncated,
			"sources": qaRefOutputs(t, answer.Sources),
		}
	}
	unused := map[string]any{}
	if n := len(h.chatQueue); n > 0 {
		unused["chat"] = n
	}
	if n := len(h.itemQueue); n > 0 {
		unused["items"] = n
	}
	if n := len(h.wikiQueue); n > 0 {
		unused["wikiSearch"] = n
	}
	out["calls"] = nonNil(h.calls)
	out["steps"] = nonNil(h.steps)
	out["answerTexts"] = nonNil(h.answerTexts)
	out["evidence"] = nonNil(h.evidence)
	out["unused"] = unused
	return out
}

func TestQAEngineGolden(t *testing.T) {
	var golden struct {
		Cases []json.RawMessage `json:"cases"`
	}
	loadGolden(t, "qa-engine", &golden)
	if len(golden.Cases) < 100 {
		t.Fatalf("golden 里只有 %d 个用例，预期不少于 100 个", len(golden.Cases))
	}
	for _, raw := range golden.Cases {
		var generic map[string]any
		if err := json.Unmarshal(raw, &generic); err != nil {
			t.Fatalf("解析用例失败：%v", err)
		}
		name, _ := generic["name"].(string)
		t.Run(name, func(t *testing.T) {
			expect := generic["expect"].(map[string]any)
			delete(generic, "expect")
			c := remarshal[qaEngineCase](t, expandTexts(t, generic, &[]textDigest{}))
			assertOutcome(t, expect, runQAEngineCase(t, &c))
		})
	}
}
