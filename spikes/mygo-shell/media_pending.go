package main

// 媒体频道依赖、但所属连接器尚未迁移的入口。
//
// 论坛讨论总结 / 刷新已迁移（forum_refresh.go）；在线视频音轨下载仍随视频连接器（video-url.ts）迁移，迁移时替换本文件里的实现，
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
