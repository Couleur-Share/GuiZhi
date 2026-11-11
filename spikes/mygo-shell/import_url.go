package main

// 对应 connectors.ts 里 case "url" 的分发顺序：视频 → 论坛 → 公众号 → 通用网页。
// 视频、论坛、公众号随各自切片接入；在此之前如实报错，不假装成功。

import (
	"context"
	"errors"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

var errURLKindNotMigrated = errors.New("该来源类型的采集尚未迁移到新内核")

func (l *Library) importExtractURL(ctx context.Context, task map[string]any, onStage func(string)) (*extractedContent, error) {
	raw, _ := task["sourceInput"].(string)
	url := jsTrim(raw)
	if onStage == nil {
		onStage = func(string) {}
	}
	if detectVideoPlatform(url) != "" || detectForumPlatform(url) != nil {
		return nil, errURLKindNotMigrated
	}
	parsed, err := whatwg.Parse(url)
	if err != nil {
		return nil, errors.New("Invalid URL")
	}
	taskID, _ := task["id"].(string)
	// 公众号正文已在 HTML 中，直接读取可避免页面脚本、统计请求耗尽浏览器预算。
	if parsed.Hostname() == "mp.weixin.qq.com" {
		result, err := l.captureWechat(ctx, url, "import-wechat")
		if err != nil {
			return nil, err
		}
		ex := webCaptureToExtracted(result)
		ex.WarningReason = strings.Join(result.Warnings, "；")
		return ex, nil
	}
	onStage("web-preparing")
	result := l.captureWebPage(ctx, webCaptureRequest{TaskID: taskID, Purpose: "import", URL: url}, onStage)
	return webCaptureToExtracted(result), nil
}

// disposeExtractedContent 对应 import-service 的 disposeExtracted：释放公众号快照资产租约，并清掉未被引用的资产文件。
func (l *Library) disposeExtractedContent(ex *extractedContent) {
	if l.disposeExtracted != nil {
		l.disposeExtracted(ex)
		return
	}
	if ex == nil || ex.WebCapture == nil {
		return
	}
	r := ex.WebCapture
	if r.release != nil {
		r.release()
	}
	if len(r.SnapshotAssets) > 0 {
		if _, err := l.cleanupOrphanAssets(context.Background(), r.SnapshotAssets); err != nil {
			logAppErrorf("main", "清理公众号快照资源", "%v", err)
		}
	}
}
