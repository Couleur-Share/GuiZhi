package main

// 主题阅读渲染引擎的按需预热。
//
// 引擎（goja 运行时 + 内置渲染脚本）首次构造约 150–185 ms、常驻约 14 MB。构造发生在
// 第一次读到主题页数据时（themedValidate），也就是用户第一次打开带主题页的条目的那条 IPC 里，
// 这段延迟会直接落在界面上。
// 但不是每个用户都用主题阅读：无条件预热会让所有人白占 14 MB，所以只在库里确有主题页数据时才预热。
// 生成管线引擎（约 130 ms）只在 AI 生成主题时用，调用前本来就要等数秒，不预热；
// 网页净化引擎（约 45 ms）与网络抓取同量级，也不预热。
//
// Warm the themed-reading renderer in the background, only when the library actually holds themed pages.

import (
	"context"
	"log"
	"time"
)

// themedPrewarmDelay 启动后等多久再预热：让首屏渲染与启动期查询先跑完，不与它们抢 CPU。
const themedPrewarmDelay = 3 * time.Second

// hasThemedReadingData 报告库里是否存在主题阅读版本或任务。出错（含库已关闭、旧库缺表）一律按没有处理：
// 预热只是优化，宁可不预热也不能影响启动。
func (l *Library) hasThemedReadingData(ctx context.Context) bool {
	rows, err := l.rdb().Query(ctx, "SELECT 1 FROM themed_reading_versions UNION ALL SELECT 1 FROM themed_reading_tasks LIMIT 1")
	return err == nil && len(rows.Values) > 0
}

// prewarmThemedEngineIfUsed 延迟后在后台预热渲染引擎。不阻塞调用方；ctx 结束（退出）时放弃。
// getThemedEngine 内部是 sync.Once：预热进行中用户恰好首次访问，只会等剩余的构造时间，不会比不预热更慢。
func (l *Library) prewarmThemedEngineIfUsed(ctx context.Context, delay time.Duration) {
	go func() {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		if !l.hasThemedReadingData(ctx) {
			return
		}
		started := time.Now()
		if _, err := getThemedEngine(); err != nil {
			// 构造结果由 sync.Once 缓存，首次使用时会得到同一个错误，行为与不预热一致，只是更早留下日志。
			log.Printf("[themed] 渲染引擎预热失败：%v", err)
			return
		}
		log.Printf("[themed] 渲染引擎已预热（%dms）", time.Since(started).Milliseconds())
	}()
}
