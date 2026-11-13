package main

// 生图 Go 移植的行为测试：本地 httptest 服务回放 OpenAI / Gemini 协议的成功与失败，不碰真实网络与真实密钥。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestImageGenProtocols(t *testing.T) {
	png := rtPNG("img", 16)
	b64 := base64.StdEncoding.EncodeToString(png)
	var gotBody map[string]any
	var gotAuth, gotPath string
	var hits atomic.Int32
	mode := "ok"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		switch mode {
		case "ok":
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + b64 + `"}]}`))
		case "flaky":
			if hits.Load() == 1 {
				http.Error(w, `{"error":{"message":"upstream busy"}}`, 503)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + b64 + `"}]}`))
		case "moderation":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"Your request was rejected by the safety system"}}`))
		case "slow":
			time.Sleep(300 * time.Millisecond)
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + b64 + `"}]}`))
		case "gemini":
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + b64 + `"}}]}}]}`))
		case "empty":
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer srv.Close()
	l := &Library{}
	cfg := aiClientConfig{Provider: "openai", APIProtocol: "openai", APIKey: "sk-test", APIURL: srv.URL + "/v1", Model: "img-model"}
	ctx := context.Background()

	t.Run("openai-b64", func(t *testing.T) {
		hits.Store(0)
		mode = "ok"
		img, err := l.generateImage(ctx, "画一只猫", "4:3", cfg, imageGenOptions{Scenario: "themedReading"}.withNoRetry())
		if err != nil {
			t.Fatal(err)
		}
		if string(img.Data) != string(png) || img.Extension != ".png" {
			t.Fatalf("图片内容或扩展名不对：%q %q", img.Data, img.Extension)
		}
		if gotPath != "/v1/images/generations" || gotAuth != "Bearer sk-test" {
			t.Fatalf("端点或鉴权不对：%s %s", gotPath, gotAuth)
		}
		if gotBody["model"] != "img-model" || gotBody["response_format"] != "b64_json" || gotBody["size"] != "1536x1024" || gotBody["prompt"] != "画一只猫" {
			t.Fatalf("请求体不对：%v", gotBody)
		}
	})
	t.Run("retry-then-ok", func(t *testing.T) {
		mode = "flaky"
		hits.Store(0)
		var requests int
		_, err := l.generateImage(ctx, "p", "1:1", cfg, imageGenOptions{RetryDelays: []time.Duration{time.Millisecond}, OnRequest: func() error { requests++; return nil }})
		if err != nil || hits.Load() != 2 || requests != 2 {
			t.Fatalf("应重试一次后成功：err=%v hits=%d requests=%d", err, hits.Load(), requests)
		}
	})
	t.Run("no-retry-reports-first-failure", func(t *testing.T) {
		mode = "flaky"
		hits.Store(0)
		_, err := l.generateImage(ctx, "p", "1:1", cfg, imageGenOptions{}.withNoRetry())
		if err == nil || hits.Load() != 1 || !strings.Contains(err.Error(), "upstream busy") {
			t.Fatalf("不重试应直接给出首次失败原因：err=%v hits=%d", err, hits.Load())
		}
	})
	t.Run("moderation-not-retried", func(t *testing.T) {
		mode = "moderation"
		hits.Store(0)
		_, err := l.generateImage(ctx, "p", "1:1", cfg, imageGenOptions{RetryDelays: []time.Duration{time.Millisecond, time.Millisecond}})
		if err == nil || hits.Load() != 1 {
			t.Fatalf("4xx 不应重试：err=%v hits=%d", err, hits.Load())
		}
	})
	t.Run("timeout", func(t *testing.T) {
		mode = "slow"
		_, err := l.generateImage(ctx, "p", "1:1", cfg, imageGenOptions{Timeout: 50 * time.Millisecond}.withNoRetry())
		if err == nil || !strings.Contains(err.Error(), "超时") {
			t.Fatalf("应报超时：%v", err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		mode = "slow"
		cctx, cancel := context.WithCancelCause(ctx)
		go func() { time.Sleep(30 * time.Millisecond); cancel(context.Canceled) }()
		_, err := l.generateImage(cctx, "p", "1:1", cfg, imageGenOptions{}.withNoRetry())
		if err == nil {
			t.Fatal("取消后不应成功")
		}
	})
	t.Run("empty-response", func(t *testing.T) {
		mode = "empty"
		_, err := l.generateImage(ctx, "p", "1:1", cfg, imageGenOptions{}.withNoRetry())
		if err == nil || !strings.Contains(err.Error(), "没有图片数据") {
			t.Fatalf("空响应应报错：%v", err)
		}
	})
	t.Run("gemini", func(t *testing.T) {
		mode = "gemini"
		g := aiClientConfig{Provider: "gemini", APIProtocol: "gemini", APIKey: "k", APIURL: srv.URL, Model: "gemini-img"}
		img, err := l.generateImage(ctx, "p", "16:9", g, imageGenOptions{}.withNoRetry())
		if err != nil || string(img.Data) != string(png) {
			t.Fatalf("Gemini 协议应取到图片：%v", err)
		}
		if !strings.HasSuffix(gotPath, "/models/gemini-img:generateContent") {
			t.Fatalf("Gemini 端点不对：%s", gotPath)
		}
	})
}
