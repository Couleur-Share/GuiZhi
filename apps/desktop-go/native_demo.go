package main

// 原生界面的演示数据：给 pnpm shot:native 与帧预算测试一份确定的、有代表性的库内容。
// 只由 -native-demo 触发，且 runNative 保证它只在自动化实例（隔离库）里运行。

import (
	"context"
	"fmt"
	"time"
)

// seedNativeDemo 往库里写入演示用的导入任务：各种状态、来源与时间跨度都有，
// 但不含 pending（队列会真的去抓取假地址）。
func seedNativeDemo(ctx context.Context, lib *Library) error {
	type demo struct {
		kind, input, name, status string
		stage, errMsg, warn       any
	}
	tasks := []demo{
		{"url", "https://www.v2ex.com/t/1253950", "Wails 迁到 MyGo 原生界面的坑点", "completed", nil, nil, nil},
		{"url", "https://zhuanlan.zhihu.com/p/639887211", "SQLite WAL 模式下的备份注意事项", "completed", nil, nil, nil},
		{"url", "https://www.bilibili.com/video/BV1xx411c7mD", "Go 并发模式精讲（视频）", "completed", nil, nil, "语音转写失败，已保留平台字幕"},
		{"file", `D:\资料\规格说明\归知产品边界.pdf`, "归知产品边界.pdf", "completed", nil, nil, nil},
		{"url", "https://mp.weixin.qq.com/s/abcdefg", "微信公众号文章：本地优先的知识管理", "failed", nil, "抓取失败：目标站点要求登录（403）", nil},
		{"url", "https://github.com/egoist/mygo", "egoist/mygo", "processing", "extracting", nil, nil},
		{"url", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "视频：语音转写进行中", "processing", "transcribing", nil, nil},
		{"url", "https://example.com/duplicate", "重复的链接", "duplicate", nil, nil, nil},
		{"url", "https://news.ycombinator.com/item?id=1", "已取消的采集", "canceled", nil, nil, nil},
		{"text", "一段直接粘贴的文字：本地优先、可备份、可迁移。", "粘贴的文字", "completed", nil, nil, nil},
	}
	now := time.Now().UnixMilli()
	const hour = int64(time.Hour / time.Millisecond)
	for i, d := range tasks {
		// 前几条在今天（显示时分），后面的拉开到昨天、本年更早、去年（覆盖三种时间格式）
		age := int64(i) * 40 * 60_000
		switch {
		case i >= 8:
			age = 400 * 24 * hour
		case i >= 6:
			age = (3 + int64(i)) * 24 * hour
		case i >= 4:
			age += 26 * hour
		}
		created := now - age
		if _, err := lib.db.Execute(ctx, `INSERT INTO import_tasks
 (id, source_kind, source_input, display_name, status, stage, error, warning, created_at, updated_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("demo-%02d", i), d.kind, d.input, d.name, d.status, d.stage, d.errMsg, d.warn, created, created+90_000); err != nil {
			return err
		}
	}
	return nil
}
