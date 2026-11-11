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
	if strings.EqualFold(parsed.Hostname(), "mp.weixin.qq.com") {
		return nil, errURLKindNotMigrated
	}
	taskID, _ := task["id"].(string)
	onStage("web-preparing")
	result := l.captureWebPage(ctx, webCaptureRequest{TaskID: taskID, Purpose: "import", URL: url}, onStage)
	return webCaptureToExtracted(result), nil
}
