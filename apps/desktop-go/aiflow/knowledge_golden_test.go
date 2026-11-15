package aiflow

// 知识域前半段的 golden 对拍：提示词与消息构造、摘要、标签建议、处理中心 AI 归类。
// 夹具来自 apps/desktop/tests/unit/knowledge-ai/aiflow-golden-knowledge.test.ts（真实 TS 实现 + 假传输导出）。
//
// 长输入在 golden 里是「规格」（{cjk:n} / {ascii:n} / {lit:"…"} 的拼接）加指纹：
// 这里按同一生成器重建，先核对指纹（确认生成器与 TS 一致），再喂给被测函数。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
)

// ---- 文本规格与指纹 ----

const goldenCJK = "知识库归纳整理摘要标签分类检索问答"

func goldenCJKText(n int) string {
	runes := []rune(goldenCJK)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteRune(runes[i%len(runes)])
	}
	return b.String()
}

func goldenASCIIText(n int) string {
	const unit = "abcdefghij"
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(unit[i%len(unit)])
	}
	return b.String()
}

// textSpec 是 golden 里的文本规格：JSON 字符串原样，或一组 {cjk|ascii|lit} 的拼接。
type textSpec string

func (s *textSpec) UnmarshalJSON(data []byte) error {
	var plain string
	if err := json.Unmarshal(data, &plain); err == nil {
		*s = textSpec(plain)
		return nil
	}
	var parts []struct {
		CJK   *int    `json:"cjk"`
		ASCII *int    `json:"ascii"`
		Lit   *string `json:"lit"`
	}
	if err := json.Unmarshal(data, &parts); err != nil {
		return fmt.Errorf("无法解析文本规格：%w", err)
	}
	var b strings.Builder
	for _, part := range parts {
		switch {
		case part.CJK != nil:
			b.WriteString(goldenCJKText(*part.CJK))
		case part.ASCII != nil:
			b.WriteString(goldenASCIIText(*part.ASCII))
		case part.Lit != nil:
			b.WriteString(*part.Lit)
		default:
			return fmt.Errorf("文本规格的片段没有 cjk / ascii / lit：%s", data)
		}
	}
	*s = textSpec(b.String())
	return nil
}

func (s textSpec) String() string { return string(s) }

// textDigest 对应 TS 的 Digest：UTF-16 长度 + sha256 + 首尾各 16 个码元。
type textDigest struct {
	Length int    `json:"length"`
	SHA256 string `json:"sha256"`
	Head   string `json:"head"`
	Tail   string `json:"tail"`
}

func digestOf(text string) textDigest {
	n := u16Len(text)
	sum := sha256.Sum256([]byte(text))
	return textDigest{
		Length: n,
		SHA256: hex.EncodeToString(sum[:]),
		Head:   u16Slice(text, 0, 16),
		Tail:   u16Slice(text, max(0, n-16), n),
	}
}

// compactText 短文本原样、长文本只留指纹（与 TS 的 compactText 同界限）。
func compactText(text string) any {
	if u16Len(text) <= 300 {
		return text
	}
	return digestOf(text)
}

// checkInput 核对重建出的输入与 TS 的指纹一致：不一致说明 Go 的生成器与 TS 不同，后面的比对没有意义。
func checkInput(t *testing.T, text string, want textDigest) {
	t.Helper()
	if got := digestOf(text); got != want {
		t.Fatalf("重建的输入与 TS 的指纹不一致（生成器有差异）\nTS：%+v\nGo：%+v", want, got)
	}
}

// ---- 提示词 ----

func TestPromptsGolden(t *testing.T) {
	var golden struct {
		Constants map[string]any `json:"constants"`
		Truncate  []struct {
			Name         string     `json:"name"`
			Text         textSpec   `json:"text"`
			Input        textDigest `json:"input"`
			Limit        int        `json:"limit"`
			Plain        textDigest `json:"plain"`
			WithNote     textDigest `json:"withNote"`
			PlainText    any        `json:"plainText"`
			WithNoteText any        `json:"withNoteText"`
		} `json:"truncate"`
		UserPrompts []struct {
			Name       string     `json:"name"`
			Title      string     `json:"title"`
			Content    textSpec   `json:"content"`
			Input      textDigest `json:"input"`
			Prompt     textDigest `json:"prompt"`
			PromptText any        `json:"promptText"`
		} `json:"userPrompts"`
		Chunks []struct {
			Name          string       `json:"name"`
			Content       textSpec     `json:"content"`
			Input         textDigest   `json:"input"`
			Count         int          `json:"count"`
			Lengths       []int        `json:"lengths"`
			Digests       []textDigest `json:"digests"`
			CoveredLength int          `json:"coveredLength"`
			CoveredPrefix bool         `json:"coveredIsPrefix"`
		} `json:"chunks"`
		MapReduce struct {
			Map []struct {
				Title  string `json:"title"`
				Chunk  string `json:"chunk"`
				Index  int    `json:"index"`
				Total  int    `json:"total"`
				Prompt string `json:"prompt"`
			} `json:"map"`
			Reduce []struct {
				Title    string   `json:"title"`
				Partials []string `json:"partials"`
				Prompt   string   `json:"prompt"`
			} `json:"reduce"`
		} `json:"mapReduce"`
		QA struct {
			User []struct {
				Name     string   `json:"name"`
				Question string   `json:"question"`
				Blocks   []string `json:"blocks"`
				History  []QaTurn `json:"history"`
				Prompt   string   `json:"prompt"`
			} `json:"user"`
			Agent []struct {
				Name       string   `json:"name"`
				Question   string   `json:"question"`
				History    []QaTurn `json:"history"`
				Transcript []string `json:"transcript"`
				Prompt     string   `json:"prompt"`
			} `json:"agent"`
		} `json:"qa"`
	}
	loadGolden(t, "prompts", &golden)

	assertSame(t, "常量与版本号", golden.Constants, map[string]any{
		"SUMMARY_PROMPT_VERSION":        SummaryPromptVersion,
		"SUMMARY_SYSTEM_PROMPT":         SummarySystemPrompt,
		"SUMMARY_MAP_SYSTEM_PROMPT":     SummaryMapSystemPrompt,
		"TAG_SUGGESTION_PROMPT_VERSION": TagSuggestionPromptVersion,
		"TAG_SUGGESTION_SYSTEM_PROMPT":  TagSuggestionSystemPrompt,
		"QA_PROMPT_VERSION":             QAPromptVersion,
		"QA_SYSTEM_PROMPT":              QASystemPrompt,
		"QA_AGENT_PROMPT_VERSION":       QAAgentPromptVersion,
		"QA_AGENT_SYSTEM_PROMPT":        QAAgentSystemPrompt,
		"MAX_CONTENT_LENGTH":            MaxContentLength,
		"SUMMARY_CHUNK_SIZE":            SummaryChunkSize,
		"SUMMARY_MAX_CHUNKS":            SummaryMaxChunks,
	})

	if len(golden.Truncate) < 15 || len(golden.UserPrompts) < 5 || len(golden.Chunks) < 10 {
		t.Fatalf("golden 用例数异常：truncate=%d userPrompts=%d chunks=%d", len(golden.Truncate), len(golden.UserPrompts), len(golden.Chunks))
	}

	for _, c := range golden.Truncate {
		t.Run("截断/"+c.Name, func(t *testing.T) {
			text := c.Text.String()
			checkInput(t, text, c.Input)
			plain, withNote := TruncatePlain(text, c.Limit), TruncateText(text, c.Limit)
			assertSame(t, "truncatePlain 指纹", c.Plain, digestOf(plain))
			assertSame(t, "truncateText 指纹", c.WithNote, digestOf(withNote))
			assertSame(t, "truncatePlain 短结果", c.PlainText, compactText(plain))
			assertSame(t, "truncateText 短结果", c.WithNoteText, compactText(withNote))
		})
	}

	for _, c := range golden.UserPrompts {
		t.Run("用户消息/"+c.Name, func(t *testing.T) {
			content := c.Content.String()
			checkInput(t, content, c.Input)
			prompt := BuildUserPrompt(c.Title, content)
			assertSame(t, "指纹", c.Prompt, digestOf(prompt))
			assertSame(t, "短结果", c.PromptText, compactText(prompt))
		})
	}

	for _, c := range golden.Chunks {
		t.Run("分块/"+c.Name, func(t *testing.T) {
			content := c.Content.String()
			checkInput(t, content, c.Input)
			chunks := ChunkContent(content)
			lengths := make([]int, len(chunks))
			digests := make([]textDigest, len(chunks))
			for i, chunk := range chunks {
				lengths[i] = u16Len(chunk)
				digests[i] = digestOf(chunk)
			}
			covered := strings.Join(chunks, "")
			assertSame(t, "块数", c.Count, len(chunks))
			assertSame(t, "各块长度", nonNil(c.Lengths), nonNil(lengths))
			assertSame(t, "各块指纹", nonNil(c.Digests), nonNil(digests))
			assertSame(t, "覆盖长度", c.CoveredLength, u16Len(covered))
			assertSame(t, "覆盖的是前缀", c.CoveredPrefix, strings.HasPrefix(content, covered))
		})
	}

	for i, c := range golden.MapReduce.Map {
		if got := BuildSummaryMapPrompt(c.Title, c.Chunk, c.Index, c.Total); got != c.Prompt {
			t.Errorf("map 提示词 #%d 与 TS 不一致\nTS：%q\nGo：%q", i, c.Prompt, got)
		}
	}
	for i, c := range golden.MapReduce.Reduce {
		if got := BuildSummaryReducePrompt(c.Title, c.Partials); got != c.Prompt {
			t.Errorf("reduce 提示词 #%d 与 TS 不一致\nTS：%q\nGo：%q", i, c.Prompt, got)
		}
	}
	for _, c := range golden.QA.User {
		if got := BuildQaUserPrompt(c.Question, c.Blocks, c.History); got != c.Prompt {
			t.Errorf("问答用户消息「%s」与 TS 不一致\nTS：%q\nGo：%q", c.Name, c.Prompt, got)
		}
	}
	for _, c := range golden.QA.Agent {
		if got := BuildQaAgentPrompt(c.Question, c.History, c.Transcript); got != c.Prompt {
			t.Errorf("问答 Agent 消息「%s」与 TS 不一致\nTS：%q\nGo：%q", c.Name, c.Prompt, got)
		}
	}
}

// 孤立代理项是已知且有意的差异（见 jsx.go 文件头）：分块的切口落在代理对中间时，Go 在该处落成 U+FFFD，
// 不会产生非法 UTF-8；TS 会发出孤立代理项。这里固定 Go 的行为，golden 不覆盖这种输入。
func TestChunkContentSplitsSurrogatePairAsReplacementChar(t *testing.T) {
	content := goldenCJKText(SummaryChunkSize-1) + "😀" + "尾"
	chunks := ChunkContent(content)
	if len(chunks) != 2 {
		t.Fatalf("应分成 2 块，实际 %d", len(chunks))
	}
	if !strings.HasSuffix(chunks[0], "\uFFFD") || !strings.HasPrefix(chunks[1], "\uFFFD") {
		t.Fatalf("切口落在代理对中间时两侧应各落成 U+FFFD：%q … %q", u16Slice(chunks[0], SummaryChunkSize-3, SummaryChunkSize), u16Slice(chunks[1], 0, 3))
	}
	if got := len(utf16.Encode([]rune(chunks[0]))); got != SummaryChunkSize {
		t.Fatalf("第一块应保持 %d 个码元，实际 %d", SummaryChunkSize, got)
	}
}

// ---- 流程：公用 ----

// compactRequests 对应 TS 的 compactRequest：请求体超过 2000 个码元只记指纹。
func compactRequests(reqs []recordedRequest) []map[string]any {
	out := make([]map[string]any, 0, len(reqs))
	for _, r := range reqs {
		m := map[string]any{"kind": r.Kind, "method": r.Method, "url": r.URL, "headers": r.Headers}
		if r.Body != nil {
			if u16Len(*r.Body) <= 2000 {
				m["body"] = *r.Body
			} else {
				m["bodyDigest"] = digestOf(*r.Body)
			}
		}
		if r.TimeoutMs != nil {
			m["timeoutMs"] = *r.TimeoutMs
		}
		out = append(out, m)
	}
	return out
}

// runFlow 用脚本化的假传输跑一个流程，返回与 TS 的 FlowOutcome 同形的结果。
func runFlow(t *testing.T, settings map[string]any, script []scriptStep, abort *string, call func(ctx context.Context, r *Runner) (any, error)) map[string]any {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if abort != nil && *abort == "before" {
		cancel()
	}
	fake := newFakeTransport(script, cancel)
	warns := &warnRecorder{}
	usage := &usageRecorder{}
	runner := &Runner{
		Client: &Client{Transport: fake, Log: warns},
		Config: staticConfig{NormalizeAISettings(settings)},
		Usage:  usage,
		Log:    warns,
	}

	result, err := call(ctx, runner)
	var errInfo any
	if err != nil {
		info := map[string]any{"message": err.Error(), "aborted": IsAborted(err)}
		if IsNotConfigured(err) {
			info["notConfigured"] = true
		}
		errInfo = info
		result = nil
	}
	return map[string]any{
		"requests":     compactRequests(fake.Requests),
		"unusedScript": len(fake.steps),
		"usage":        nonNil(usage.Entries),
		"warnings":     nonNil(warns.Messages),
		"result":       result,
		"error":        errInfo,
	}
}

// assertOutcome 逐项比较流程结果，便于定位是请求、用量还是结果不一致。
func assertOutcome(t *testing.T, want, got map[string]any) {
	t.Helper()
	for key, w := range want {
		g, ok := got[key]
		if !ok {
			t.Errorf("Go 的结果缺少 %q", key)
			continue
		}
		assertSame(t, key, w, g)
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("Go 的结果多出 %q（golden 里没有）", key)
		}
	}
}

// ---- 摘要 ----

func TestSummaryGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name     string         `json:"name"`
			Settings map[string]any `json:"settings"`
			Title    string         `json:"title"`
			Content  textSpec       `json:"content"`
			Input    textDigest     `json:"input"`
			Script   []scriptStep   `json:"script"`
			Expect   map[string]any `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "summary", &golden)
	if len(golden.Cases) < 15 {
		t.Fatalf("golden 用例数异常：%d", len(golden.Cases))
	}
	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			content := c.Content.String()
			checkInput(t, content, c.Input)
			got := runFlow(t, c.Settings, c.Script, nil, func(ctx context.Context, r *Runner) (any, error) {
				res, err := r.GenerateSummary(ctx, c.Title, content)
				if err != nil {
					return nil, err
				}
				return map[string]any{"text": res.Text, "truncated": res.Truncated}, nil
			})
			assertOutcome(t, c.Expect, got)
		})
	}
}

// ---- 标签 ----

func TestTagsGolden(t *testing.T) {
	var golden struct {
		Parse []struct {
			Input string   `json:"input"`
			Tags  []string `json:"tags"`
		} `json:"parse"`
		Flows []struct {
			Name     string         `json:"name"`
			Settings map[string]any `json:"settings"`
			Title    string         `json:"title"`
			Content  textSpec       `json:"content"`
			Input    textDigest     `json:"input"`
			Script   []scriptStep   `json:"script"`
			Expect   map[string]any `json:"expect"`
		} `json:"flows"`
	}
	loadGolden(t, "tags", &golden)
	if len(golden.Parse) < 40 || len(golden.Flows) < 8 {
		t.Fatalf("golden 用例数异常：parse=%d flows=%d", len(golden.Parse), len(golden.Flows))
	}

	for _, c := range golden.Parse {
		got := ParseTagResponse(c.Input)
		if fmt.Sprintf("%q", got) != fmt.Sprintf("%q", nonNil(c.Tags)) {
			t.Errorf("parseTagResponse(%q)\nTS：%q\nGo：%q", c.Input, c.Tags, got)
		}
	}

	for _, c := range golden.Flows {
		t.Run(c.Name, func(t *testing.T) {
			content := c.Content.String()
			checkInput(t, content, c.Input)
			got := runFlow(t, c.Settings, c.Script, nil, func(ctx context.Context, r *Runner) (any, error) {
				tags, err := r.SuggestTags(ctx, c.Title, content)
				if err != nil {
					return nil, err
				}
				return tags, nil
			})
			assertOutcome(t, c.Expect, got)
		})
	}
}

// ---- 归类 ----

func assignmentsJSON(list []InboxAIClassificationAssignment) []map[string]any {
	out := make([]map[string]any, len(list))
	for i, a := range list {
		out[i] = map[string]any{"itemId": a.ItemID, "collectionName": a.CollectionName}
	}
	return out
}

func TestClassifyGolden(t *testing.T) {
	var golden struct {
		Names []struct {
			Input      string `json:"input"`
			Normalized string `json:"normalized"`
			Key        string `json:"key"`
			Valid      bool   `json:"valid"`
			Reserved   bool   `json:"reserved"`
		} `json:"names"`
		Parse []struct {
			Name   string   `json:"name"`
			Text   string   `json:"text"`
			IDs    []string `json:"ids"`
			Result any      `json:"result"`
			Error  *string  `json:"error"`
		} `json:"parse"`
		Flows []struct {
			Name     string         `json:"name"`
			Settings map[string]any `json:"settings"`
			Items    []struct {
				ItemID  string `json:"itemId"`
				Title   string `json:"title"`
				Excerpt string `json:"excerpt"`
			} `json:"items"`
			Existing []string       `json:"existing"`
			Abort    *string        `json:"abort"`
			Script   []scriptStep   `json:"script"`
			Expect   map[string]any `json:"expect"`
		} `json:"flows"`
	}
	loadGolden(t, "classify", &golden)
	if len(golden.Names) < 30 || len(golden.Parse) < 25 || len(golden.Flows) < 12 {
		t.Fatalf("golden 用例数异常：names=%d parse=%d flows=%d", len(golden.Names), len(golden.Parse), len(golden.Flows))
	}

	for _, c := range golden.Names {
		if got := NormalizeAICollectionName(c.Input); got != c.Normalized {
			t.Errorf("normalizeAiCollectionName(%q)\nTS：%q\nGo：%q", c.Input, c.Normalized, got)
		}
		if got := AICollectionNameKey(c.Input); got != c.Key {
			t.Errorf("aiCollectionNameKey(%q)\nTS：%q\nGo：%q", c.Input, c.Key, got)
		}
		if got := IsValidAICollectionName(c.Input); got != c.Valid {
			t.Errorf("isValidAiCollectionName(%q)：TS=%v Go=%v", c.Input, c.Valid, got)
		}
		if got := IsReservedNewCollectionName(c.Input); got != c.Reserved {
			t.Errorf("isReservedNewCollectionName(%q)：TS=%v Go=%v", c.Input, c.Reserved, got)
		}
	}

	for _, c := range golden.Parse {
		t.Run("解析/"+c.Name, func(t *testing.T) {
			result, err := ParseCollectionClassificationResponse(c.Text, nonNil(c.IDs))
			switch {
			case c.Error != nil && err == nil:
				t.Fatalf("TS 报错 %q，Go 却成功：%v", *c.Error, assignmentsJSON(result))
			case c.Error != nil && err.Error() != *c.Error:
				t.Fatalf("错误文案不一致\nTS：%q\nGo：%q", *c.Error, err.Error())
			case c.Error == nil && err != nil:
				t.Fatalf("TS 成功，Go 报错：%v", err)
			case c.Error == nil:
				assertSame(t, "解析结果", c.Result, assignmentsJSON(result))
			}
		})
	}

	for _, c := range golden.Flows {
		t.Run(c.Name, func(t *testing.T) {
			items := make([]InboxAIClassificationSource, len(c.Items))
			for i, it := range c.Items {
				items[i] = InboxAIClassificationSource{ItemID: it.ItemID, Title: it.Title, Excerpt: it.Excerpt}
			}
			progress := [][2]int{}
			got := runFlow(t, c.Settings, c.Script, c.Abort, func(ctx context.Context, r *Runner) (any, error) {
				list, err := r.ClassifyInboxItems(ctx, items, c.Existing, func(done, total int) {
					progress = append(progress, [2]int{done, total})
				})
				if err != nil {
					return nil, err
				}
				return assignmentsJSON(list), nil
			})
			got["progress"] = progress
			assertOutcome(t, c.Expect, got)
		})
	}
}
