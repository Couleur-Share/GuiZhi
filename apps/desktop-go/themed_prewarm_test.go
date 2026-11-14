package main

// 主题阅读引擎预热（themed_prewarm.go）：按数据判断、关闭后安全、取消时放弃。

import (
	"context"
	"testing"
	"time"
)

func TestHasThemedReadingDataFollowsStoredPages(t *testing.T) {
	ctx, rw, ro := openRoPair(t)
	l := &Library{db: rw, ro: ro}

	if l.hasThemedReadingData(ctx) {
		t.Fatal("新库没有主题页，不应预热")
	}

	if _, err := rw.Execute(ctx, "INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES ('k1', 't', 'c', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := rw.Execute(ctx, "INSERT INTO themed_reading_versions (id, item_id, source_kind, role, payload, created_at, updated_at) VALUES ('v1', 'k1', 'body', 'current', '{}', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	if !l.hasThemedReadingData(ctx) {
		t.Fatal("有主题页版本后应预热（只读连接要能看到已提交的写入）")
	}
}

func TestHasThemedReadingDataIsFalseWhenDatabaseClosed(t *testing.T) {
	ctx, rw, ro := openRoPair(t)
	l := &Library{db: rw, ro: ro}
	if err := l.closeDatabases(); err != nil {
		t.Fatal(err)
	}
	if l.hasThemedReadingData(ctx) {
		t.Fatal("库已关闭时应按没有处理，不能 panic 或报真")
	}
}

func TestPrewarmThemedEngineIfUsedRespectsCancelAndDoesNotBlock(t *testing.T) {
	_, rw, ro := openRoPair(t)
	l := &Library{db: rw, ro: ro}
	ctx, cancel := context.WithCancel(context.Background())

	start := time.Now()
	l.prewarmThemedEngineIfUsed(ctx, time.Hour) // 必须立即返回
	if time.Since(start) > time.Second {
		t.Fatal("预热不应阻塞调用方")
	}
	cancel() // 取消后后台协程应退出；这里无法直接观察协程，至少保证不 panic、不卡测试进程退出
	time.Sleep(50 * time.Millisecond)
}
