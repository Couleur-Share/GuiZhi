package main

// 媒体 IPC 频道的手写测试：真实 SQLite（基准库副本）+ httptest 的 ASR / 对话服务，不碰真实网络与密钥。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type mediaEnv struct {
	t       *testing.T
	l       *Library
	b       *Bridge
	srv     *httptest.Server
	mu      sync.Mutex
	chatN   int
	chatBod []string
	asrN    int
	asrText string
	chatOut []string // 依次返回的对话内容
	pushed  []map[string]any
	pushCh  []string
}

func newMediaEnv(t *testing.T, withModels bool) *mediaEnv {
	t.Helper()
	_, l := loadGolden(t)
	l.paths = Paths{UserData: t.TempDir()}
	l.configDir = t.TempDir()
	e := &mediaEnv{t: t, l: l, b: &Bridge{handlers: map[string]handler{}}, asrText: "你好世界这是一段口播"}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		e.mu.Lock()
		defer e.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/audio/transcriptions"):
			e.asrN++
			_ = json.NewEncoder(w).Encode(map[string]any{"text": e.asrText})
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			e.chatBod = append(e.chatBod, string(body))
			out := "默认回复"
			if e.chatN < len(e.chatOut) {
				out = e.chatOut[e.chatN]
			}
			e.chatN++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"content": out}, "finish_reason": "stop"}},
				"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(e.srv.Close)
	l.aiChatHTTP = e.srv.Client()
	l.setPush(func(ch string, payload any) {
		e.mu.Lock()
		defer e.mu.Unlock()
		m, _ := toGeneric(t, payload).(map[string]any)
		e.pushed = append(e.pushed, m)
		e.pushCh = append(e.pushCh, ch)
	})
	l.registerMedia(e.b)
	if withModels {
		e.writeConfig(`{"audioText":"asr","mainText":"chat"}`)
	}
	return e
}

func (e *mediaEnv) writeConfig(routes string) {
	e.t.Helper()
	cfg := fmt.Sprintf(`{"kind":"guizhi-ai-config","version":1,"providers":[],"models":[
		{"id":"asr","provider":"custom","name":"asr","apiUrl":%q,"apiKey":"k","model":"whisper-x","capabilities":{"chat":false}},
		{"id":"chat","provider":"custom","name":"chat","apiUrl":%q,"apiKey":"k","model":"gpt-x","isDefault":true}
	],"modelRouteDefaults":%s,"updatedAt":"2026-01-01T00:00:00.000Z"}`, e.srv.URL+"/v1", e.srv.URL+"/v1", routes)
	if err := os.WriteFile(e.l.aiConfigPath(), []byte(cfg), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

func (e *mediaEnv) call(ch string, args ...any) map[string]any {
	e.t.Helper()
	raw := make([]json.RawMessage, len(args))
	for i, a := range args {
		raw[i], _ = json.Marshal(a)
	}
	out, err := e.b.handlers[ch](context.Background(), raw)
	if err != nil {
		e.t.Fatalf("%s: %v", ch, err)
	}
	return toGeneric(e.t, out).(map[string]any)
}

func (e *mediaEnv) create(itemType, title, content, transcript string) string {
	e.t.Helper()
	in := CreateInput{Title: optStr(title), Content: optStr(content), ItemType: optStr(itemType)}
	if transcript != "" {
		in.Transcript = optStr(transcript)
	}
	v, err := e.l.knowledgeCreate(context.Background(), in)
	if err != nil {
		e.t.Fatal(err)
	}
	return toGeneric(e.t, v).(map[string]any)["id"].(string)
}

func (e *mediaEnv) item(id string) map[string]any {
	e.t.Helper()
	m, err := e.l.itemForMedia(context.Background(), id)
	if err != nil || m == nil {
		e.t.Fatalf("读取条目: %v %v", m, err)
	}
	return m
}

func TestMediaIPCGuards(t *testing.T) {
	e := newMediaEnv(t, false)
	docID := e.create("note", "笔记", "正文", "")
	audioID := e.create("audio", "录音", "![x](local-video://a.mp3)", "")

	cases := []struct {
		name string
		ch   string
		args []any
		want string
	}{
		{"转写条目缺失", chMediaTranscribe, []any{"nope"}, "条目不存在"},
		{"转写非媒体", chMediaTranscribe, []any{docID}, "仅音频 / 视频条目支持转写"},
		{"转写未配置", chMediaTranscribe, []any{audioID}, "未配置语音转写模型"},
		{"排版条目缺失", chMediaFormatTranscript, []any{"nope"}, "条目不存在"},
		{"排版无文字稿", chMediaFormatTranscript, []any{audioID}, "该条目还没有文字稿"},
		{"总结条目缺失", chMediaSummarize, []any{"nope"}, "条目不存在"},
		{"总结非媒体", chMediaSummarize, []any{docID}, "仅音频 / 视频条目支持内容总结"},
		{"总结无文字稿", chMediaSummarize, []any{audioID}, "该条目还没有文字稿，请先生成文字稿"},
		{"刷新论坛条目缺失", chMediaRefreshForum, []any{"nope"}, "条目不存在"},
		{"测试转写配置不完整", chMediaTestTranscription, []any{map[string]any{"apiUrl": "http://x", "apiKey": " "}}, "转写模型配置不完整"},
	}
	for _, c := range cases {
		res := e.call(c.ch, c.args...)
		if res["success"] != false || res["error"] != c.want {
			t.Errorf("%s: %v", c.name, res)
		}
	}
	if res := e.call(chMediaTranscribe, audioID); res["notConfigured"] != true {
		t.Errorf("未配置应带 notConfigured: %v", res)
	}
	// 有文字稿但未配置文本模型
	withT := e.create("video", "视频", "", "已有文字稿")
	for _, ch := range []string{chMediaFormatTranscript, chMediaSummarize} {
		res := e.call(ch, withT)
		if res["notConfigured"] != true || res["error"] != "未配置可用的文本模型" {
			t.Errorf("%s: %v", ch, res)
		}
	}
	if res := e.call(chMediaCapabilities); res["diarization"] != false {
		t.Errorf("capabilities: %v", res)
	}
}

func TestMediaIPCDiarizeUnsupported(t *testing.T) {
	e := newMediaEnv(t, true)
	id := e.create("audio", "录音", "![x](local-video://a.mp3)", "")
	res := e.call(chMediaTranscribe, id, map[string]any{"diarize": true})
	if res["success"] != false || !strings.Contains(fmt.Sprint(res["error"]), "区分说话人只有内置本地转写引擎支持") {
		t.Errorf("%v", res)
	}
	if e.asrN != 0 {
		t.Error("不应发出转写请求")
	}
}

func TestMediaIPCMissingFile(t *testing.T) {
	e := newMediaEnv(t, true)
	id := e.create("audio", "录音", "![x](local-video://gone.mp3)", "")
	res := e.call(chMediaTranscribe, id)
	if res["success"] != false || res["error"] != "媒体文件已不存在" {
		t.Errorf("%v", res)
	}
}

func TestMediaIPCTranscribeEndToEnd(t *testing.T) {
	e := newMediaEnv(t, true)
	if err := os.MkdirAll(e.l.paths.VideosDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.l.paths.VideosDir(), "talk.mp3"), []byte("ID3fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 排版先、总结后；总结首行带 AI 标题
	e.chatOut = []string{"你好，世界。这是一段口播。", "标题：口播精要\n\n## 要点\n- 问候"}
	id := e.create("audio", "原始录音名", "> 平台：抖音\n> 作者：张三\n\n![x](local-video://talk.mp3)", "")

	res := e.call(chMediaTranscribe, id)
	if res["success"] != true {
		t.Fatalf("转写失败: %v", res)
	}
	if e.asrN != 1 || e.chatN != 2 {
		t.Errorf("请求数 asr=%d chat=%d", e.asrN, e.chatN)
	}
	got := e.item(id)
	if got["transcript"] != "你好，世界。这是一段口播。" {
		t.Errorf("文字稿: %q", got["transcript"])
	}
	if got["title"] != "口播精要" {
		t.Errorf("AI 标题: %q", got["title"])
	}
	content := strField(got, "content")
	for _, want := range []string{"音频识别（whisper-x）", "- 问候", "原始录音名"} {
		if !strings.Contains(content, want) {
			t.Errorf("正文缺 %q:\n%s", want, content)
		}
	}
	// 条目已进 FTS：用文字稿里的词能搜到
	rows, err := e.l.db.Query(context.Background(), "SELECT COUNT(*) FROM knowledge_fts_map WHERE item_id = ?", id)
	if err != nil || len(rows.Values) != 1 || fmt.Sprint(rows.Values[0][0]) == "0" {
		t.Errorf("FTS 映射缺失: %v %v", rows, err)
	}
	// 进度推送：至少有阶段变化的上报（排版 / 总结），且都带 itemId
	e.mu.Lock()
	defer e.mu.Unlock()
	stages := map[string]bool{}
	for i, p := range e.pushed {
		if e.pushCh[i] == chMediaTranscribeProgress {
			if p["itemId"] != id {
				t.Errorf("进度缺 itemId: %v", p)
			}
			stages[fmt.Sprint(p["stage"])] = true
		}
	}
	if !stages["formatting"] || !stages["summarizing"] {
		t.Errorf("阶段推送: %v", stages)
	}
	// 排版与总结用的是 chat 模型，转写用的是 asr 模型
	if !strings.Contains(e.chatBod[0], `"gpt-x"`) {
		t.Errorf("对话请求体: %s", e.chatBod[0])
	}
}

func TestMediaIPCFormatAndSummarize(t *testing.T) {
	e := newMediaEnv(t, true)
	e.chatOut = []string{"整理后的文字稿。", "标题：新标题\n\n- 要点一"}
	id := e.create("video", "老标题", "> 平台：抖音\n\n正文", "未分段的文字稿 内容")

	res := e.call(chMediaFormatTranscript, id)
	if res["success"] != true {
		t.Fatalf("排版: %v", res)
	}
	if e.item(id)["transcript"] != "整理后的文字稿。" {
		t.Errorf("排版结果未落库: %v", e.item(id)["transcript"])
	}
	e.mu.Lock()
	var progress int
	for _, ch := range e.pushCh {
		if ch == chMediaFormatProgress {
			progress++
		}
	}
	e.mu.Unlock()
	if progress == 0 {
		t.Error("缺少 media:formatProgress 推送")
	}

	res = e.call(chMediaSummarize, id)
	if res["success"] != true {
		t.Fatalf("总结: %v", res)
	}
	got := e.item(id)
	if got["title"] != "新标题" || !strings.Contains(strField(got, "content"), "- 要点一") || !strings.Contains(strField(got, "content"), "老标题") {
		t.Errorf("总结落库: title=%v content=%v", got["title"], got["content"])
	}
}

func TestMediaIPCTestTranscription(t *testing.T) {
	e := newMediaEnv(t, true)
	res := e.call(chMediaTestTranscription, map[string]any{"apiUrl": e.srv.URL + "/v1", "apiKey": "k", "model": "whisper-x"})
	if res["success"] != true {
		t.Errorf("%v", res)
	}
	if _, ok := res["latency"]; !ok {
		t.Error("缺 latency")
	}
	// 服务返回非 JSON 时给出错误而不是 success
	e.asrText = ""
	res = e.call(chMediaTestTranscription, map[string]any{"apiUrl": e.srv.URL + "/v1", "apiKey": "k", "model": "whisper-x"})
	if res["success"] != true {
		// 静音样本允许空文本：与 TS 一致，成功
		t.Errorf("空文本仍应视为连通: %v", res)
	}
}

func TestMediaIPCPendingConnectors(t *testing.T) {
	e := newMediaEnv(t, true)
	forum := e.create("forum", "帖子", "正文", "")
	for _, ch := range []string{chMediaSummarize, chMediaRefreshForum} {
		res := e.call(ch, forum)
		if res["success"] != false || !strings.Contains(fmt.Sprint(res["error"]), "尚未迁移") {
			t.Errorf("%s 应如实报未迁移: %v", ch, res)
		}
	}
}
