package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// 真实网络冒烟（默认跳过）：GUIZHI_REAL_NET=1 才运行。每个站点只取 1 个公开页面，渲染窗口离屏。
// Real-network smoke test, opt-in only: one public page per site, hidden render window.
// TestRealNetImport 走产品的导入分发入口（importExtractURL）：论坛帖子与视频元数据。
// 每个站点 1 个公开页面，只读、不登录、不带 Cookie。地址默认取已与用户确认的页面，可用环境变量覆盖：
//
//	GUIZHI_REAL_V2EX / GUIZHI_REAL_LINUXDO / GUIZHI_REAL_NGA / GUIZHI_REAL_YOUTUBE / GUIZHI_REAL_BILI
//
// 视频需 GUIZHI_REAL_YTDLP=<yt-dlp 可执行文件路径>（仅用于本测试，不进 PATH）。未设置的站点跳过。
// 被站点挡下（Cloudflare 挑战、地区限制、需登录）是有效结果：测试只要求如实降级并给出原因，不要求一定成功。
func TestRealNetImport(t *testing.T) {
	if os.Getenv("GUIZHI_REAL_NET") != "1" {
		t.Skip("未设置 GUIZHI_REAL_NET=1")
	}
	env := func(key, def string) string {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
		return def
	}
	ytdlp := strings.TrimSpace(os.Getenv("GUIZHI_REAL_YTDLP"))
	cases := []struct {
		name, url string
		video     bool
	}{
		{"V2EX", env("GUIZHI_REAL_V2EX", "https://www.v2ex.com/t/1"), false},
		{"LINUX DO", env("GUIZHI_REAL_LINUXDO", "https://linux.do/t/topic/1"), false},
		{"NGA", env("GUIZHI_REAL_NGA", ""), false},
		{"YouTube", env("GUIZHI_REAL_YOUTUBE", "https://www.youtube.com/watch?v=jNQXAC9IVRw"), true},
		{"B站", env("GUIZHI_REAL_BILI", ""), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.url == "" {
				t.Skip("未提供地址")
			}
			if c.video && ytdlp == "" {
				t.Skip("未设置 GUIZHI_REAL_YTDLP")
			}
			l := &Library{paths: Paths{UserData: t.TempDir()}}
			if c.video {
				l.videoTestDeps = &videoDeps{ytDlpPath: ytdlp}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			var stages []string
			start := time.Now()
			ex, err := l.importExtractURL(ctx, map[string]any{"sourceInput": c.url}, func(s string) { stages = append(stages, s) })
			if err != nil {
				t.Logf("耗时 %v 返回错误（如实报错也算有效结果）：%v", time.Since(start).Round(time.Millisecond), err)
				return
			}
			head := []rune(ex.Content)
			if len(head) > 200 {
				head = head[:200]
			}
			t.Logf("耗时 %v 类型 %s 标题 %q 正文 %d 字符 阶段 %v\n降级原因：%q 警告：%q\n开头：%s",
				time.Since(start).Round(time.Millisecond), ex.ItemType, ex.Title, len([]rune(ex.Content)), stages,
				ex.DegradedReason, ex.WarningReason, strings.ReplaceAll(string(head), "\n", " "))
			if ex.Transcript != nil {
				tr := []rune(*ex.Transcript)
				if len(tr) > 160 {
					tr = tr[:160]
				}
				t.Logf("文字稿 %d 字符，开头：%s", len([]rune(*ex.Transcript)), strings.ReplaceAll(string(tr), "\n", " "))
			} else if c.video {
				t.Logf("文字稿：无")
			}
			if os.Getenv("GUIZHI_REAL_FULL") == "1" {
				t.Logf("全文：\n%s", ex.Content)
			}
			if ex.Title == "" && ex.DegradedReason == "" {
				t.Errorf("既没有标题也没有降级原因：结果不可解释")
			}
		})
	}
}

func TestRealNetCapture(t *testing.T) {
	if os.Getenv("GUIZHI_REAL_NET") != "1" {
		t.Skip("未设置 GUIZHI_REAL_NET=1")
	}
	l := &Library{}
	host := newRenderHost(func() *safeFetcher { return l.webFetcher })
	l.webRenderer = host
	t.Cleanup(host.Close)
	cases := []struct{ name, url, must string }{
		{"静态：example.com", "https://example.com/", "documentation examples"},
		{"静态：维基百科文章", "https://en.wikipedia.org/wiki/SQLite", "SQLite"},
		{"需渲染：quotes.toscrape.com/js", "https://quotes.toscrape.com/js/", "Albert Einstein"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			var stages []string
			start := time.Now()
			r := l.captureWebPage(ctx, webCaptureRequest{TaskID: "real", Purpose: "import", URL: c.url}, func(s string) { stages = append(stages, s) })
			head := r.Markdown
			if len([]rune(head)) > 160 {
				head = string([]rune(head)[:160])
			}
			t.Logf("耗时 %v 引擎 %s 标题 %q 最终地址 %s 正文 %d 字符 阶段 %v\n开头：%s", time.Since(start).Round(time.Millisecond), r.EngineVersion, r.Title, r.FinalURL, len([]rune(r.Markdown)), stages, strings.ReplaceAll(head, "\n", " "))
			if r.Error != nil {
				t.Fatalf("采集失败：%+v", r.Error)
			}
			if !strings.Contains(r.Markdown, c.must) {
				t.Errorf("正文缺少 %q", c.must)
			}
		})
	}
}
