package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ocrTestLib(t *testing.T, srv *httptest.Server) (*Library, string) {
	t.Helper()
	img := filepath.Join(t.TempDir(), "p.jpg")
	if err := os.WriteFile(img, []byte("ABC"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Library{paths: Paths{UserData: t.TempDir()}, aiChatHTTP: srv.Client()}, img
}

func TestRecognizeImageFileOpenAI(t *testing.T) {
	var gotPath, gotAuth, gotUA string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotUA = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("User-Agent")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"  图中文字  "}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`))
	}))
	defer srv.Close()
	l, img := ocrTestLib(t, srv)
	text, err := l.recognizeImageFile(context.Background(), img, ocrModelConfig{APIURL: srv.URL, APIKey: "sk-test", Model: "vision"})
	if err != nil || text != "图中文字" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	if !strings.HasSuffix(gotPath, "/chat/completions") || gotAuth != "Bearer sk-test" || gotUA != "node" {
		t.Fatalf("请求头/路径不符: %s %s %s", gotPath, gotAuth, gotUA)
	}
	msgs := gotBody["messages"].([]any)[0].(map[string]any)["content"].([]any)
	url := msgs[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if url != "data:image/jpeg;base64,QUJD" {
		t.Fatalf("data URL = %s", url)
	}
}

func TestRecognizeImageFileAnthropic(t *testing.T) {
	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.Path, r.Header.Get("x-api-key")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"甲"},{"type":"text","text":"乙"}]}`))
	}))
	defer srv.Close()
	l, img := ocrTestLib(t, srv)
	text, err := l.recognizeImageFile(context.Background(), img, ocrModelConfig{APIURL: srv.URL, APIKey: "k", Model: "claude", APIProtocol: "anthropic"})
	if err != nil || text != "甲乙" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	if !strings.HasSuffix(gotPath, "/messages") || gotKey != "k" {
		t.Fatalf("anthropic 请求不符: %s %s", gotPath, gotKey)
	}
}

func TestRecognizeImageFileErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"http-error", 500, strings.Repeat("错", 400), "OCR 请求失败 (HTTP 500): " + strings.Repeat("错", 300)},
		{"empty", 200, `{"choices":[{"message":{"content":"   "}}]}`, "OCR 未识别到内容"},
		{"missing", 200, `{"choices":[]}`, "OCR 响应缺少文本内容"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			l, img := ocrTestLib(t, srv)
			_, err := l.recognizeImageFile(context.Background(), img, ocrModelConfig{APIURL: srv.URL, APIKey: "k", Model: "m"})
			if err == nil || err.Error() != c.want {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	// 读文件失败直接返回
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	if _, err := l.recognizeImageFile(context.Background(), filepath.Join(t.TempDir(), "nope.png"), ocrModelConfig{}); err == nil {
		t.Fatal("文件不存在应报错")
	}
}

// 图文采集的取消语义：下载阶段返回「已取消」要整条中止；OCR 阶段上下文已取消同样中止；单张下载失败只记入正文。
func TestImageNoteCancellation(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	src := imageNoteSource{PlatformLabel: "抖音", Title: "t", ImageMirrors: [][]string{{"u"}}}
	_, err := l.buildImageNoteEntry(context.Background(), src, imageNoteDeps{
		downloadImage:  func(ctx context.Context, m []string) (string, string, error) { return "", "", errCanceledMsg },
		getOcrConfig:   func() *ocrModelConfig { return nil },
		getTitleConfig: func() *aiClientConfig { return nil },
	})
	if err == nil || !isCanceledError(err) {
		t.Fatalf("下载取消应中止: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	_, err = l.buildImageNoteEntry(ctx, src, imageNoteDeps{
		downloadImage: func(ctx context.Context, m []string) (string, string, error) {
			return "", filepath.Join(t.TempDir(), "a.png"), nil
		},
		saveAsset:    func(string) (string, error) { return "a.png", nil },
		getOcrConfig: func() *ocrModelConfig { return &ocrModelConfig{} },
		recognize: func(ctx context.Context, fp string, c ocrModelConfig) (string, error) {
			cancel()
			return "", context.Canceled
		},
		getTitleConfig: func() *aiClientConfig { return nil },
	})
	if err == nil || !isCanceledError(err) {
		t.Fatalf("OCR 取消应中止: %v", err)
	}
}
