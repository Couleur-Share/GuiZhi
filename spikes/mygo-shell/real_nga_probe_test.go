package main

// NGA 线上 A/B 探针（Go 侧）。默认跳过；需要 GUIZHI_REAL_NET=1 与 GUIZHI_REAL_NGA_PROBE=<摘要输出文件>。
// 走产品的 fetchNgaThread，但把请求数封顶：只读第 1 页、楼主分页最多 19 页、不补拉父楼、
// 不重试、附件图最多真实请求 3 次（其余直接记为失败而不发请求）。
// TS 侧是 verify/nga-ab/nga-probe.ts，参数完全相同。逐跳证据由 GUIZHI_FETCH_TRACE 写出。
// 建议同时设置 GUIZHI_FETCH_MIN_INTERVAL_MS=1000 放慢节奏。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealNgaProbe(t *testing.T) {
	out := strings.TrimSpace(os.Getenv("GUIZHI_REAL_NGA_PROBE"))
	if os.Getenv("GUIZHI_REAL_NET") != "1" || out == "" {
		t.Skip("未设置 GUIZHI_REAL_NET=1 与 GUIZHI_REAL_NGA_PROBE=<摘要输出文件>")
	}
	const topic = "37194262"
	l := &Library{paths: Paths{UserData: t.TempDir()}}
	one, opPages, none, imgLimit := 1, 19, 0, 3
	var imageAttempts, imageCapped atomic.Int32
	deps := ngaDeps{
		retryDelays:    []time.Duration{}, // 不重试，请求数可预期
		maxSummaryPage: &one,
		maxOpPages:     &opPages,
		maxParentFetch: &none,
		imageLimit:     &imgLimit,
		downloadImage: func(ctx context.Context, u string) (string, string, error) {
			if imageAttempts.Add(1) > 3 {
				imageCapped.Add(1)
				return "", "", errors.New("探针封顶：不再发起真实图片请求")
			}
			return l.ngaDefaultDownloadImage(ctx, u)
		},
		saveImageAsset: func(string) (string, error) { return "probe.jpg", nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	start := time.Now()
	th, err := l.fetchNgaThread(ctx, topic, deps)
	summary := map[string]any{
		"side": "go", "elapsedMs": time.Since(start).Milliseconds(),
		"imageNetworkAttempts": min(int(imageAttempts.Load()), 3), "imageCapped": imageCapped.Load(),
	}
	if err != nil {
		summary["error"] = err.Error()
		t.Logf("探针返回错误（如实记录）：%v", err)
	} else {
		summary["title"] = th.Title
		summary["author"] = th.Author
		summary["replyCount"] = th.ReplyCount
		summary["contentChars"] = len([]rune(th.Content))
		summary["opReplies"] = len(th.Replies)
		summary["summaryReplies"] = len(th.SummaryReplies)
		summary["warning"] = th.WarningReason
	}
	b, _ := json.MarshalIndent(summary, "", " ")
	if werr := os.WriteFile(out, b, 0o600); werr != nil {
		t.Fatal(werr)
	}
	t.Logf("摘要已写入 %s", out)
}
