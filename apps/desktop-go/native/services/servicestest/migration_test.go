package servicestest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
)

// 零值：没有旧库；设置探测结果后按设置返回；迁移成功后不再发现旧库，失败则保持。
func TestMigrationFakeDetectAndRun(t *testing.T) {
	ctx := t.Context()
	f := &Migration{}
	if _, found, err := f.DetectLegacy(ctx); found || err != nil {
		t.Fatalf("零值应没有旧库：%v %v", found, err)
	}

	f.SetDetection(services.LegacyDetection{Path: "legacy.db", ItemCount: 7}, true)
	f.SetStats(services.LegacyMigrationStats{Items: 7, Tags: 2})
	det, found, err := f.DetectLegacy(ctx)
	if err != nil || !found || det.Path != "legacy.db" || det.ItemCount != 7 {
		t.Fatalf("探测：%+v %v %v", det, found, err)
	}

	f.FailRun(errors.New("legacy database is locked"))
	if _, err := f.RunLegacy(ctx, "legacy.db"); err == nil || err.Error() != "legacy database is locked" {
		t.Fatalf("注入的迁移错误：%v", err)
	}
	if _, found, _ := f.DetectLegacy(ctx); !found {
		t.Fatal("失败的迁移不应改变探测结果")
	}

	f.FailRun(nil)
	stats, err := f.RunLegacy(ctx, "")
	if err != nil || stats.Items != 7 || stats.Tags != 2 {
		t.Fatalf("迁移：%+v %v", stats, err)
	}
	if _, found, _ := f.DetectLegacy(ctx); found {
		t.Fatal("迁移成功后当前库非空，不应再发现旧库")
	}
	if runs := f.Runs(); len(runs) != 2 || runs[0] != "legacy.db" || runs[1] != "" {
		t.Fatalf("迁移收到的路径：%q", runs)
	}
	if f.Detects() != 4 { // 零值、有旧库、失败后、成功后各一次
		t.Fatalf("探测次数：%d", f.Detects())
	}
}

func TestMigrationFakeDetectErrorAndGates(t *testing.T) {
	f := &Migration{}
	f.FailDetect(errors.New("cannot read library"))
	if _, found, err := f.DetectLegacy(t.Context()); found || err == nil {
		t.Fatalf("注入的探测错误：%v %v", found, err)
	}
	f.FailDetect(nil)

	// 卡住的探测随 ctx 取消返回，而不是永远挂着。
	f.DetectGate = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, _, err := f.DetectLegacy(ctx); done <- err }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("取消应返回 ctx 错误：%v", err)
	}

	// 放行后正常返回。
	f.RunGate = make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	var runErr error
	go func() { defer wg.Done(); _, runErr = f.RunLegacy(t.Context(), "x") }()
	close(f.RunGate)
	wg.Wait()
	if runErr != nil {
		t.Fatalf("放行后：%v", runErr)
	}
}

// 并发调用在 -race 下不出竞态。
func TestMigrationFakeConcurrent(t *testing.T) {
	f := &Migration{}
	f.SetDetection(services.LegacyDetection{Path: "p", ItemCount: 1}, true)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); f.DetectLegacy(t.Context()) }()
		go func() { defer wg.Done(); f.RunLegacy(t.Context(), "p") }()
	}
	wg.Wait()
	if f.Detects() != 8 || len(f.Runs()) != 8 {
		t.Fatalf("计数：%d %d", f.Detects(), len(f.Runs()))
	}
}
