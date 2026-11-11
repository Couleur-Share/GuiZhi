package main

// 媒体频道依赖、但所属连接器尚未迁移的三个入口。
//
// 它们在 TS 里分别来自 services/import/video-url.ts（在线视频音轨下载）与论坛连接器
// （appinn / linuxdo / twolibra / forum-summary / forum-note）。迁移这些连接器时直接替换本文件里的实现，
// 调用方（media_ipc.go）不用改。在此之前如实报错，不假装成功。

import (
	"context"
	"errors"
)

// errYtDlpNotFound 对应 YtDlpNotFoundError。
var errYtDlpNotFound = errors.New("尚未安装 yt-dlp")

const msgConnectorPending = "该功能依赖的连接器尚未迁移到新外壳"

// downloadItemVideoAudio 按条目来源链接下载音轨到临时目录（对应 video-retranscription.ts）。
func (l *Library) downloadItemVideoAudio(ctx context.Context, item map[string]any) (dir, file string, err error) {
	return "", "", errors.New("在线视频重新转写：" + msgConnectorPending)
}

// regenerateForumSummary 重新生成论坛条目的讨论总结（对应 media.ipc.ts regenerateForumSummary）。
func (l *Library) regenerateForumSummary(ctx context.Context, item map[string]any) map[string]any {
	return mediaFail("论坛讨论总结：" + msgConnectorPending)
}

// refreshForumDiscussion 抓取支持站点的最新全楼层，替换讨论段并把旧总结标成过期。
func (l *Library) refreshForumDiscussion(ctx context.Context, item map[string]any) map[string]any {
	return mediaFail("刷新论坛讨论：" + msgConnectorPending)
}
