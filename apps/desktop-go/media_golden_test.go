package main

// media 差分基线：TS 侧（golden-media.mts）用真实模块产出，这里逐项比对。
// 基线来源：$GUIZHI_MEDIA_GOLDEN_DIR/golden-media.json（调试用独立运行器），
// 否则 $GUIZHI_GOLDEN_DIR/golden.json 的 media 段。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type gMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type mediaGoldenFile struct {
	Media *mediaGolden `json:"media"`
}

type scriptedResp struct {
	Status      int             `json:"status"`
	ContentType string          `json:"contentType"`
	JSON        json.RawMessage `json:"json"`
	Raw         *string         `json:"raw"`
}

type recordedReq struct {
	Method  string         `json:"method"`
	Path    string         `json:"path"`
	Headers map[string]any `json:"headers"`
	RawBody string         `json:"rawBody"`
	Fields  [][]any        `json:"fields"`
	File    *struct {
		Name   string `json:"name"`
		Size   int    `json:"size"`
		Sha256 string `json:"sha256"`
	} `json:"file"`
}

type chatSpecG struct {
	Name   string `json:"name"`
	Config struct {
		Provider    string `json:"provider"`
		APIProtocol string `json:"apiProtocol"`
		APIKey      string `json:"apiKey"`
		URLPath     string `json:"urlPath"`
		Model       string `json:"model"`
	} `json:"config"`
	Messages []gMsg `json:"messages"`
	Options  *struct {
		Temperature    *float64 `json:"temperature"`
		MaxTokens      int      `json:"maxTokens"`
		ResponseFormat *struct {
			Type string `json:"type"`
		} `json:"responseFormat"`
		Stream bool `json:"stream"`
	} `json:"options"`
	Responses []scriptedResp `json:"responses"`
}

type outcomeG struct {
	OK           bool            `json:"ok"`
	Content      string          `json:"content"`
	FinishReason *string         `json:"finishReason"`
	Usage        *chatTokenUsage `json:"usage"`
	Error        string          `json:"error"`
}

type mediaGolden struct {
	Protocol struct {
		ResolveCases []struct {
			C        struct{ APIProtocol, Provider, APIUrl string }
			Protocol string
		} `json:"resolveCases"`
		Endpoints []struct {
			APIUrl, Protocol string
			Base             struct {
				Protocol string
				Explicit bool
				BaseUrl  string
			}
			Chat, Transcriptions string
		} `json:"endpoints"`
		HeaderCases []struct {
			Protocol string
			O        *struct {
				Accept              string
				ContentType         *bool
				UseNativeGeminiAuth bool
			}
			Headers map[string]string
		} `json:"headerCases"`
		Usages []struct {
			Payload  any
			Protocol string
			Usage    *struct{ PromptTokens, CompletionTokens float64 }
		} `json:"usages"`
	} `json:"protocol"`
	ChatCases []struct {
		Spec     chatSpecG     `json:"spec"`
		Requests []recordedReq `json:"requests"`
		Progress []int         `json:"progress"`
		Deltas   []string      `json:"deltas"`
		Outcome  outcomeG      `json:"outcome"`
	} `json:"chatCases"`
	Glossary []struct {
		Sources []*string `json:"sources"`
		Terms   []string  `json:"terms"`
	} `json:"glossary"`
	Splits []struct {
		Text          string
		Chunks, Small []string
	} `json:"splits"`
	Rejects []struct {
		Raw, Content string
		FinishReason string
		Reason       *string
	} `json:"rejects"`
	Speakers []struct {
		Text  string
		Count int
		List  []string
	} `json:"speakers"`
	FormatCases []struct {
		Sc struct {
			Name string
			Raw  string
			Mode string
			Opts *struct {
				MaxTotalChars *int     `json:"maxTotalChars"`
				Glossary      []string `json:"glossary"`
				TimeBudgetMs  *int     `json:"timeBudgetMs"`
			}
		}
		Calls []struct {
			Chunk    string
			Messages []gMsg
			Options  struct {
				Temperature *float64
				MaxTokens   int
				TimeoutMs   int
			}
		}
		Progress []int
		Outcome  struct {
			OK            bool
			Text          string
			SkippedReason *string
			PartialReason *string
			Error         string
		}
	} `json:"formatCases"`
	Sanitized []struct{ S, Out string } `json:"sanitized"`
	Titles    []struct {
		S   string
		Out struct {
			Title *string
			Body  string
		}
	} `json:"titles"`
	SummaryCases []struct {
		Sc struct {
			Name    string
			Input   struct{ Title, Context, Transcript string }
			Replies json.RawMessage
		}
		Calls []struct {
			Messages []gMsg
			Options  struct {
				Temperature *float64
				MaxTokens   int
				TimeoutMs   int
			}
		}
		Outcome struct {
			OK      bool
			Summary string
			Title   *string
			Error   string
		}
	} `json:"summaryCases"`
	ContentTitleCases []struct {
		Name, Source, Reply string
		Calls               []struct {
			Messages []gMsg
			Options  struct {
				Temperature *float64
				MaxTokens   int
				TimeoutMs   int
			}
		}
		Title *string
	} `json:"contentTitleCases"`
	Upserts            []struct{ Content, Heading, Out string }  `json:"upserts"`
	OriginalTitleCases []struct{ Content, Title, Out string }    `json:"originalTitleCases"`
	SourceNotes        []struct{ Content, Strip, Upsert string } `json:"sourceNotes"`
	Assets             []struct {
		C   string
		Ref *string
	} `json:"assets"`
	Cleans        []struct{ S, Out string } `json:"cleans"`
	EndpointCases []struct {
		C struct {
			APIUrl, APIProtocol, Provider string
		}
		Endpoint string
	} `json:"endpointCases"`
	Wavs []struct {
		Seconds float64
		Size    int
		Sha256  string
	} `json:"wavs"`
	AudioSha        string `json:"audioSha"`
	TranscribeCases []struct {
		Sc struct {
			Name      string
			Diarize   bool
			Responses []scriptedResp
			Cfg       *struct {
				APIProtocol string `json:"apiProtocol"`
			}
		}
		Requests []recordedReq
		Outcome  struct {
			OK    bool
			Text  string
			Error string
		}
	} `json:"transcribeCases"`
}

func loadMediaGolden(t *testing.T) *mediaGolden {
	t.Helper()
	var raw []byte
	var err error
	if dir := os.Getenv("GUIZHI_MEDIA_GOLDEN_DIR"); dir != "" {
		raw, err = os.ReadFile(filepath.Join(dir, "golden-media.json"))
	} else if dir := os.Getenv("GUIZHI_GOLDEN_DIR"); dir != "" {
		raw, err = os.ReadFile(filepath.Join(dir, "golden.json"))
	} else {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR / GUIZHI_MEDIA_GOLDEN_DIR")
	}
	if err != nil {
		t.Fatal(err)
	}
	var g mediaGoldenFile
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if g.Media == nil {
		t.Skip("golden 里没有 media 段，请重新生成")
	}
	return g.Media
}

func nullStr(s *string) string {
	if s == nil {
		return "<null>"
	}
	return *s
}

func TestMediaGoldenProtocol(t *testing.T) {
	g := loadMediaGolden(t).Protocol
	for _, c := range g.ResolveCases {
		if got := resolveAIProtocol(c.C.APIProtocol, c.C.Provider, c.C.APIUrl); got != c.Protocol {
			t.Errorf("resolveAIProtocol(%+v)=%q want %q", c.C, got, c.Protocol)
		}
	}
	for _, c := range g.Endpoints {
		base := resolveProtocolBase(c.APIUrl, c.Protocol)
		if base.Protocol != c.Base.Protocol || base.Explicit != c.Base.Explicit || base.BaseURL != c.Base.BaseUrl {
			t.Errorf("base(%q,%s)=%+v want %+v", c.APIUrl, c.Protocol, base, c.Base)
		}
		if got := buildChatEndpoint(base); got != c.Chat {
			t.Errorf("chat(%q,%s)=%q want %q", c.APIUrl, c.Protocol, got, c.Chat)
		}
		if got := buildTranscriptionsEndpointFromBase(base); got != c.Transcriptions {
			t.Errorf("transcriptions(%q,%s)=%q want %q", c.APIUrl, c.Protocol, got, c.Transcriptions)
		}
	}
	for _, c := range g.HeaderCases {
		var o headerOpts
		if c.O != nil {
			o = headerOpts{Accept: c.O.Accept, NoContent: c.O.ContentType != nil && !*c.O.ContentType, NativeGemin: c.O.UseNativeGeminiAuth}
		}
		if got := buildHeadersForProtocol(c.Protocol, "KEY", o); !reflect.DeepEqual(got, c.Headers) {
			t.Errorf("headers(%s,%+v)=%v want %v", c.Protocol, c.O, got, c.Headers)
		}
	}
	for i, c := range g.Usages {
		got := extractUsageFromChatResponse(c.Payload, c.Protocol)
		switch {
		case got == nil && c.Usage == nil:
		case got == nil || c.Usage == nil:
			t.Errorf("usage[%d](%v,%s)=%v want %v", i, c.Payload, c.Protocol, got, c.Usage)
		case got.PromptTokens != c.Usage.PromptTokens || got.CompletionTokens != c.Usage.CompletionTokens:
			t.Errorf("usage[%d](%v,%s)=%v want %v", i, c.Payload, c.Protocol, *got, *c.Usage)
		}
	}
}

// scriptedServer 起一个按脚本应答的本地服务，并记录收到的请求。
func scriptedServer(t *testing.T, responses []scriptedResp, record func(r *http.Request, body []byte)) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	index := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		record(r, body)
		mu.Lock()
		resp := responses[min(index, len(responses)-1)]
		index++
		mu.Unlock()
		ct := resp.ContentType
		if ct == "" {
			ct = "application/json"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(resp.Status)
		if resp.Raw != nil {
			_, _ = io.WriteString(w, *resp.Raw)
		} else {
			var buf bytes.Buffer
			_ = json.Compact(&buf, resp.JSON) // 基线文件是缩进过的；TS 发出的是紧凑文本
			_, _ = w.Write(buf.Bytes())
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

var pickedHeaderNames = []string{"content-type", "accept", "authorization", "x-api-key", "anthropic-version", "x-goog-api-key", "user-agent"}

func pickReqHeaders(r *http.Request, dropAccept bool) map[string]any {
	out := map[string]any{}
	for _, k := range pickedHeaderNames {
		if v := r.Header.Get(k); v != "" {
			if k == "content-type" && strings.HasPrefix(v, "multipart/form-data") {
				v = "multipart/form-data"
			}
			out[k] = v
		}
	}
	if dropAccept {
		delete(out, "accept")
	}
	return out
}

func normJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMediaGoldenChat(t *testing.T) {
	g := loadMediaGolden(t)
	for _, c := range g.ChatCases {
		t.Run(c.Spec.Name, func(t *testing.T) {
			reqs := []recordedReq{}
			var mu sync.Mutex
			srv := scriptedServer(t, c.Spec.Responses, func(r *http.Request, body []byte) {
				mu.Lock()
				defer mu.Unlock()
				reqs = append(reqs, recordedReq{Method: r.Method, Path: r.URL.RequestURI(), Headers: pickReqHeaders(r, false), RawBody: string(body)})
			})
			l := &Library{aiChatHTTP: &http.Client{}}
			var progress []int
			var deltas []string
			opts := aiChatOptions{OnProgress: func(n int) { progress = append(progress, n) }, OnDelta: func(s string) { deltas = append(deltas, s) }}
			if o := c.Spec.Options; o != nil {
				opts.Temperature, opts.MaxTokens, opts.Stream = o.Temperature, o.MaxTokens, o.Stream
				if o.ResponseFormat != nil {
					opts.ResponseFormat = o.ResponseFormat.Type
				}
			}
			var msgs []aiChatMessage
			for _, m := range c.Spec.Messages {
				msgs = append(msgs, aiChatMessage{Role: m.Role, Content: m.Content})
			}
			cfg := aiClientConfig{Provider: c.Spec.Config.Provider, APIProtocol: c.Spec.Config.APIProtocol, APIKey: c.Spec.Config.APIKey, APIURL: srv.URL + c.Spec.Config.URLPath, Model: c.Spec.Config.Model}
			res, err := l.chatCompletion(context.Background(), cfg, msgs, opts)

			if !reflect.DeepEqual(normJSON(t, reqs), normJSON(t, c.Requests)) {
				t.Errorf("请求不一致\n got  %v\n want %v", normJSON(t, reqs), normJSON(t, c.Requests))
			}
			if c.Outcome.OK {
				if err != nil {
					t.Fatalf("期望成功，得到错误 %v", err)
				}
				if res.Content != c.Outcome.Content || (res.FinishReason != "") != (c.Outcome.FinishReason != nil) || (c.Outcome.FinishReason != nil && res.FinishReason != *c.Outcome.FinishReason) {
					t.Errorf("结果 %+v want %+v", res, c.Outcome)
				}
				if !reflect.DeepEqual(normJSON(t, res.Usage), normJSON(t, c.Outcome.Usage)) {
					t.Errorf("usage %v want %v", normJSON(t, res.Usage), normJSON(t, c.Outcome.Usage))
				}
			} else {
				if err == nil {
					t.Fatalf("期望错误 %q，得到成功 %+v", c.Outcome.Error, res)
				}
				// JSON 语法错误等运行时文本不逐字比对（Node 与 Go 的措辞不同），只比我们自己生成的消息
				if !strings.HasPrefix(c.Outcome.Error, "Unexpected") && err.Error() != c.Outcome.Error {
					t.Errorf("错误 %q want %q", err.Error(), c.Outcome.Error)
				}
			}
			if len(progress) != len(c.Progress) || !reflect.DeepEqual(append([]int{}, progress...), append([]int{}, c.Progress...)) {
				if !(len(progress) == 0 && len(c.Progress) == 0) {
					t.Errorf("progress %v want %v", progress, c.Progress)
				}
			}
			if strings.Join(deltas, "|") != strings.Join(c.Deltas, "|") {
				t.Errorf("deltas %v want %v", deltas, c.Deltas)
			}
		})
	}
}

func TestMediaGoldenFormatPure(t *testing.T) {
	g := loadMediaGolden(t)
	for i, c := range g.Glossary {
		var sources []string
		for _, s := range c.Sources {
			if s != nil {
				sources = append(sources, *s)
			} else {
				sources = append(sources, "")
			}
		}
		got := extractGlossaryTerms(sources...)
		if len(got) != len(c.Terms) || (len(got) > 0 && !reflect.DeepEqual(got, c.Terms)) {
			t.Errorf("glossary[%d]=%v want %v", i, got, c.Terms)
		}
	}
	for i, c := range g.Splits {
		for _, pair := range []struct {
			max  int
			want []string
		}{{transcriptFormatChunkChars, c.Chunks}, {50, c.Small}} {
			got := splitTranscriptChunks(c.Text, pair.max)
			if len(got) != len(pair.want) || (len(got) > 0 && !reflect.DeepEqual(got, pair.want)) {
				t.Errorf("split[%d](max=%d) 块数 %d want %d", i, pair.max, len(got), len(pair.want))
			}
		}
	}
	for i, c := range g.Rejects {
		got := rejectFormattedChunk(c.Raw, aiChatResult{Content: c.Content, FinishReason: c.FinishReason})
		want := ""
		if c.Reason != nil {
			want = *c.Reason
		}
		if got != want {
			t.Errorf("reject[%d]=%q want %q", i, got, want)
		}
	}
	for i, c := range g.Speakers {
		if got := countSpeakerPrefixes(c.Text); got != c.Count {
			t.Errorf("speaker count[%d]=%d want %d", i, got, c.Count)
		}
		if got := listSpeakers(c.Text); len(got) != len(c.List) || (len(got) > 0 && !reflect.DeepEqual(got, c.List)) {
			t.Errorf("speaker list[%d]=%v want %v", i, got, c.List)
		}
	}
}

var punctuate20 = regexp.MustCompile(`(.{20})`)

func TestMediaGoldenFormatTranscript(t *testing.T) {
	g := loadMediaGolden(t)
	chunkRe := regexp.MustCompile(`(?s)【待整理文字稿开始】\n(.*)\n【待整理文字稿结束】`)
	for _, c := range g.FormatCases {
		t.Run(c.Sc.Name, func(t *testing.T) {
			l := &Library{}
			type call struct {
				chunk    string
				messages []aiChatMessage
				opts     aiChatOptions
			}
			var mu sync.Mutex
			var calls []call
			attempts := map[string]int{}
			chat := func(_ context.Context, _ aiClientConfig, messages []aiChatMessage, opts aiChatOptions) (aiChatResult, error) {
				chunk := chunkRe.FindStringSubmatch(messages[1].Content)[1]
				mu.Lock()
				calls = append(calls, call{chunk, messages, opts})
				attempts[chunk]++
				n := attempts[chunk]
				mu.Unlock()
				punct := func(s string) string { return punctuate20.ReplaceAllString(s, "$1，") + "。" }
				switch c.Sc.Mode {
				case "throw":
					return aiChatResult{}, errors.New("模型报错 401")
				case "echo":
					return aiChatResult{Content: chunk}, nil
				case "lengthFinish":
					return aiChatResult{Content: punct(chunk), FinishReason: "length"}, nil
				case "emptyThenOk":
					if n == 1 {
						return aiChatResult{Content: ""}, nil
					}
					return aiChatResult{Content: punct(chunk), Usage: &chatTokenUsage{PromptTokens: 1, CompletionTokens: 2}}, nil
				case "failChunk2", "failAt3":
					at := 1
					if c.Sc.Mode == "failAt3" {
						at = 3
					}
					chunks := splitTranscriptChunks(c.Sc.Raw, transcriptFormatChunkChars)
					if chunk == chunks[at] {
						return aiChatResult{}, fmt.Errorf("第 %d 块失败", at+1)
					}
					return aiChatResult{Content: punct(chunk)}, nil
				case "dropSpeaker":
					return aiChatResult{Content: regexp.MustCompile(`说话人 [0-9]+：`).ReplaceAllString(chunk, "") + "。，"}, nil
				}
				if strings.Contains(chunk, "说话人") {
					return aiChatResult{Content: chunk + "。"}, nil
				}
				return aiChatResult{Content: punct(chunk)}, nil
			}
			var progress []int
			opts := formatTranscriptOptions{Chat: chat, OnProgress: func(c, tot int) {
				mu.Lock()
				progress = append(progress, c*1000+tot)
				mu.Unlock()
			}}
			if o := c.Sc.Opts; o != nil {
				if o.MaxTotalChars != nil {
					opts.MaxTotalChars = *o.MaxTotalChars
				}
				opts.Glossary = o.Glossary
				if o.TimeBudgetMs != nil {
					opts.TimeBudget = time.Duration(*o.TimeBudgetMs) * time.Millisecond
					if opts.TimeBudget == 0 {
						opts.TimeBudget = -time.Second // 零预算：Windows 时钟粒度粗，1ns 不保证已过期
					}
				}
			}
			res, err := l.formatTranscript(context.Background(), c.Sc.Raw, aiClientConfig{APIKey: "k", APIURL: "u", Model: "stub-model"}, opts)

			if c.Outcome.OK {
				if err != nil {
					t.Fatalf("期望成功，得到 %v", err)
				}
				if res.Text != c.Outcome.Text || res.SkippedReason != strings.TrimPrefix(nullStr(c.Outcome.SkippedReason), "<null>") || res.PartialReason != strings.TrimPrefix(nullStr(c.Outcome.PartialReason), "<null>") {
					t.Errorf("结果 %+v want %+v", res, c.Outcome)
				}
			} else if err == nil || err.Error() != c.Outcome.Error {
				t.Fatalf("错误 %v want %q", err, c.Outcome.Error)
			}
			sort.Slice(calls, func(i, j int) bool { return calls[i].chunk < calls[j].chunk })
			if len(calls) != len(c.Calls) {
				t.Fatalf("调用数 %d want %d", len(calls), len(c.Calls))
			}
			for i, w := range c.Calls {
				got := calls[i]
				if got.chunk != w.Chunk || !reflect.DeepEqual(normJSON(t, toMsgs(got.messages)), normJSON(t, w.Messages)) {
					t.Errorf("调用[%d] 消息不一致（块 %d 字）\n got  %q\n want %q", i, utf16Len(w.Chunk), got.messages[1].Content, w.Messages[1].Content)
				}
				if got.opts.Temperature == nil || w.Options.Temperature == nil || *got.opts.Temperature != *w.Options.Temperature ||
					got.opts.MaxTokens != w.Options.MaxTokens || int(got.opts.Timeout/time.Millisecond) != w.Options.TimeoutMs {
					t.Errorf("调用[%d] 选项 %+v want %+v", i, got.opts, w.Options)
				}
			}
			sort.Ints(progress)
			if len(progress) != len(c.Progress) || (len(progress) > 0 && !reflect.DeepEqual(progress, c.Progress)) {
				t.Errorf("progress %v want %v", progress, c.Progress)
			}
		})
	}
}

func toMsgs(ms []aiChatMessage) []map[string]string {
	out := make([]map[string]string, len(ms))
	for i, m := range ms {
		out[i] = map[string]string{"role": m.Role, "content": m.Content}
	}
	return out
}

func TestMediaGoldenSummary(t *testing.T) {
	g := loadMediaGolden(t)
	for i, c := range g.Sanitized {
		if got := sanitizeMediaSummary(c.S); got != c.Out {
			t.Errorf("sanitize[%d]=%q want %q", i, got, c.Out)
		}
	}
	for i, c := range g.Titles {
		title, has, body := splitTitleFromSummary(c.S)
		if has != (c.Out.Title != nil) || (has && title != *c.Out.Title) || body != c.Out.Body {
			t.Errorf("title[%d](%q)=(%q,%v,%q) want (%v,%q)", i, c.S, title, has, body, nullStr(c.Out.Title), c.Out.Body)
		}
	}
	// 回复脚本在 ScRaw 里：replies 是字符串数组或 "throw"/"empty"
	for _, c := range g.SummaryCases {
		t.Run(c.Sc.Name, func(t *testing.T) {
			var replies []string
			mode := ""
			if err := json.Unmarshal(c.Sc.Replies, &replies); err != nil {
				_ = json.Unmarshal(c.Sc.Replies, &mode)
			}
			l := &Library{}
			var calls []map[string]any
			n := 0
			chat := func(_ context.Context, _ aiClientConfig, messages []aiChatMessage, opts aiChatOptions) (aiChatResult, error) {
				calls = append(calls, map[string]any{"messages": toMsgs(messages), "options": map[string]any{"temperature": *opts.Temperature, "maxTokens": opts.MaxTokens, "timeoutMs": int(opts.Timeout / time.Millisecond)}})
				switch mode {
				case "throw":
					return aiChatResult{}, errors.New("上游失败")
				case "empty":
					return aiChatResult{Content: "   "}, nil
				}
				r := "兜底"
				if n < len(replies) {
					r = replies[n]
				}
				n++
				return aiChatResult{Content: r}, nil
			}
			res, err := l.generateMediaSummary(context.Background(), mediaSummaryInput{Title: c.Sc.Input.Title, Context: c.Sc.Input.Context, Transcript: c.Sc.Input.Transcript}, aiClientConfig{APIKey: "k", APIURL: "u", Model: "m"}, chat)
			if c.Outcome.OK {
				if err != nil {
					t.Fatalf("期望成功，得到 %v", err)
				}
				if res.Summary != c.Outcome.Summary || res.HasTitle != (c.Outcome.Title != nil) || (res.HasTitle && res.Title != *c.Outcome.Title) {
					t.Errorf("结果 %+v want %+v", res, c.Outcome)
				}
			} else if err == nil || err.Error() != c.Outcome.Error {
				t.Fatalf("错误 %v want %q", err, c.Outcome.Error)
			}
			want := make([]map[string]any, len(c.Calls))
			for i, w := range c.Calls {
				want[i] = map[string]any{"messages": w.Messages, "options": map[string]any{"temperature": *w.Options.Temperature, "maxTokens": w.Options.MaxTokens, "timeoutMs": w.Options.TimeoutMs}}
			}
			if len(calls) != len(want) || (len(calls) > 0 && !reflect.DeepEqual(normJSON(t, calls), normJSON(t, want))) {
				t.Errorf("对话调用不一致：%d 次 want %d 次", len(calls), len(want))
			}
		})
	}
	for _, c := range g.ContentTitleCases {
		l := &Library{}
		var calls []map[string]any
		chat := func(_ context.Context, _ aiClientConfig, messages []aiChatMessage, opts aiChatOptions) (aiChatResult, error) {
			calls = append(calls, map[string]any{"messages": toMsgs(messages), "options": map[string]any{"temperature": *opts.Temperature, "maxTokens": opts.MaxTokens, "timeoutMs": int(opts.Timeout / time.Millisecond)}})
			return aiChatResult{Content: c.Reply}, nil
		}
		title, ok, err := l.generateContentTitle(context.Background(), c.Source, aiClientConfig{APIKey: "k", APIURL: "u", Model: "m"}, chat)
		if err != nil {
			t.Fatal(err)
		}
		if ok != (c.Title != nil) || (ok && title != *c.Title) {
			t.Errorf("contentTitle[%s]=(%q,%v) want %v", c.Name, title, ok, nullStr(c.Title))
		}
		want := make([]map[string]any, len(c.Calls))
		for i, w := range c.Calls {
			want[i] = map[string]any{"messages": w.Messages, "options": map[string]any{"temperature": *w.Options.Temperature, "maxTokens": w.Options.MaxTokens, "timeoutMs": w.Options.TimeoutMs}}
		}
		if len(calls) != len(want) || (len(calls) > 0 && !reflect.DeepEqual(normJSON(t, calls), normJSON(t, want))) {
			t.Errorf("contentTitle[%s] 调用不一致", c.Name)
		}
	}
}

func TestMediaGoldenNotes(t *testing.T) {
	g := loadMediaGolden(t)
	for i, c := range g.Upserts {
		if got := upsertMediaSummarySection(c.Content, c.Heading, "  新总结第一行\n- 要点  "); got != c.Out {
			t.Errorf("upsert[%d]\n got  %q\n want %q", i, got, c.Out)
		}
	}
	for i, c := range g.OriginalTitleCases {
		if got := appendOriginalTitleNote(c.Content, c.Title); got != c.Out {
			t.Errorf("originalTitle[%d]=%q want %q", i, got, c.Out)
		}
	}
	for i, c := range g.SourceNotes {
		if got := stripTranscriptionNote(c.Content); got != c.Strip {
			t.Errorf("strip[%d]=%q want %q", i, got, c.Strip)
		}
		if got := upsertTranscriptionSourceNote(c.Content, "音频识别（sensevoice）"); got != c.Upsert {
			t.Errorf("sourceNote[%d]=%q want %q", i, got, c.Upsert)
		}
	}
	for i, c := range g.Assets {
		ref, ok := extractLocalVideoRef(c.C)
		if ok != (c.Ref != nil) || (ok && ref != *c.Ref) {
			t.Errorf("asset[%d](%q)=(%q,%v) want %v", i, c.C, ref, ok, nullStr(c.Ref))
		}
	}
}

func TestMediaGoldenTranscribe(t *testing.T) {
	g := loadMediaGolden(t)
	for i, c := range g.Cleans {
		if got := cleanTranscriptText(c.S); got != c.Out {
			t.Errorf("clean[%d](%q)=%q want %q", i, c.S, got, c.Out)
		}
	}
	for i, c := range g.EndpointCases {
		if got := buildTranscriptionsEndpoint(transcriptionModelConfig{APIURL: c.C.APIUrl, APIProtocol: c.C.APIProtocol, Provider: c.C.Provider}); got != c.Endpoint {
			t.Errorf("endpoint[%d]=%q want %q", i, got, c.Endpoint)
		}
	}
	for _, w := range g.Wavs {
		b := silentWav(w.Seconds)
		sum := sha256.Sum256(b)
		if len(b) != w.Size || hex.EncodeToString(sum[:]) != w.Sha256 {
			t.Errorf("wav(%v) size %d/%d sha %s/%s", w.Seconds, len(b), w.Size, hex.EncodeToString(sum[:]), w.Sha256)
		}
	}

	audio := make([]byte, 5000)
	for i := range audio {
		audio[i] = byte((i * 7) % 256)
	}
	if sum := sha256.Sum256(audio); hex.EncodeToString(sum[:]) != g.AudioSha {
		t.Fatal("测试音频与基线不一致")
	}
	audioFile := filepath.Join(t.TempDir(), "talk.mp3")
	if err := os.WriteFile(audioFile, audio, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, c := range g.TranscribeCases {
		t.Run(c.Sc.Name, func(t *testing.T) {
			var reqs []recordedReq
			var mu sync.Mutex
			srv := scriptedServer(t, c.Sc.Responses, func(r *http.Request, body []byte) {
				rec := recordedReq{Method: r.Method, Path: r.URL.RequestURI(), Headers: pickReqHeaders(r, true)}
				_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
				mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
				for {
					part, err := mr.NextPart()
					if err != nil {
						break
					}
					data, _ := io.ReadAll(part)
					if part.FormName() == "file" {
						sum := sha256.Sum256(data)
						rec.File = &struct {
							Name   string `json:"name"`
							Size   int    `json:"size"`
							Sha256 string `json:"sha256"`
						}{part.FileName(), len(data), hex.EncodeToString(sum[:])}
					} else {
						rec.Fields = append(rec.Fields, []any{part.FormName(), string(data)})
					}
				}
				mu.Lock()
				reqs = append(reqs, rec)
				mu.Unlock()
			})
			l := &Library{aiChatHTTP: &http.Client{}}
			cfg := transcriptionModelConfig{APIURL: srv.URL + "/v1", APIKey: "sk-stt", Model: "whisper-x"}
			if c.Sc.Cfg != nil {
				cfg.APIProtocol = c.Sc.Cfg.APIProtocol
			}
			text, err := l.transcribeMediaFile(context.Background(), audioFile, cfg, c.Sc.Diarize)
			if c.Outcome.OK {
				if err != nil || text != c.Outcome.Text {
					t.Fatalf("结果 (%q,%v) want %q", text, err, c.Outcome.Text)
				}
			} else if err == nil || err.Error() != c.Outcome.Error {
				t.Fatalf("错误 %v want %q", err, c.Outcome.Error)
			}
			if len(reqs) != len(c.Requests) || (len(reqs) > 0 && !reflect.DeepEqual(normJSON(t, reqs), normJSON(t, c.Requests))) {
				t.Errorf("请求不一致\n got  %v\n want %v", normJSON(t, reqs), normJSON(t, c.Requests))
			}
		})
	}
}
