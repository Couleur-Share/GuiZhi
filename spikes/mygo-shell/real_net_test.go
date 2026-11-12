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
