package main

// 视频连接器里尚未迁移的平台来源（抖音 / 小红书 / 图文作品）。迁移时由各自的 video_*.go 替换本文件里的实现。

import (
	"context"
	"errors"
)

type douyinDeps struct{}
type xiaohongshuDeps struct{}
type imageNoteDeps struct{}

func (l *Library) resolveDouyinSource(ctx context.Context, url string, deps *videoDeps) (*mediaSource, error) {
	return nil, errors.New("抖音采集尚未迁移到新内核")
}

func (l *Library) resolveXiaohongshuSource(ctx context.Context, url string, deps *videoDeps) (*mediaSource, error) {
	return nil, errors.New("小红书采集尚未迁移到新内核")
}
