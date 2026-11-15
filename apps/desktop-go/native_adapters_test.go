package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"

	"guizhi-mygo-shell/native/services"
)

func TestPushHubFanOutAndCancel(t *testing.T) {
	h := newPushHub()
	var a, b atomic.Int32
	cancelA := h.Subscribe("import:changed", func(any) { a.Add(1) })
	h.Subscribe("import:changed", func(any) { b.Add(1) })
	h.Subscribe("other", func(any) { t.Error("不该收到别的频道的推送") })

	h.Dispatch("import:changed", nil)
	cancelA()
	cancelA() // 可重复取消
	h.Dispatch("import:changed", nil)
	h.Dispatch("nobody-listens", nil) // 没有订阅者：静默丢弃

	if a.Load() != 1 || b.Load() != 2 {
		t.Fatalf("分发次数不对：a=%d b=%d，期望 1 和 2", a.Load(), b.Load())
	}
}

func TestBridgeCallErrors(t *testing.T) {
	b := &Bridge{handlers: map[string]handler{}, seen: map[string]int{}, missing: map[string]int{}}
	if _, err := b.call(context.Background(), "no:such"); err == nil || !strings.Contains(err.Error(), "no:such") {
		t.Fatalf("未实现的频道应报错并带上频道名：%v", err)
	}
	b.handlers["bad:shape"] = func(context.Context, []json.RawMessage) (any, error) { return "字符串", nil }
	if _, err := callAs[services.ImportPage](context.Background(), b, "bad:shape"); err == nil || !strings.Contains(err.Error(), "bad:shape") {
		t.Fatalf("结果形状对不上 DTO 时应带频道名报错：%v", err)
	}
}

// 用真实库与真实处理器验证 DTO 与 import:list 输出的对应关系：字段改名或类型变化时这里先红。
func TestImportsAdapterAgainstRealHandler(t *testing.T) {
	ctx := context.Background()
	paths := Paths{UserData: t.TempDir()}
	if err := os.MkdirAll(paths.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	lease, err := initDatabaseFile(ctx, paths.DatabasePath(), dbInitOptions{BackupsDir: paths.BackupsDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	db, err := sqlite.Open(ctx, paths.DatabasePath(), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	insert := func(id, kind, input, name, status string, stage, errMsg, warn any, created int64) {
		t.Helper()
		if _, err := db.Execute(ctx, `INSERT INTO import_tasks
 (id, source_kind, source_input, display_name, status, stage, error, warning, created_at, updated_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, kind, input, name, status, stage, errMsg, warn, created, created+500); err != nil {
			t.Fatal(err)
		}
	}
	insert("t-ok", "url", "https://www.example.com/a", "已完成的文章", "completed", nil, nil, nil, 1_000)
	insert("t-warn", "url", "https://example.com/b", "有缺失的视频", "completed", nil, nil, "语音转写失败", 2_000)
	insert("t-fail", "file", `D:\docs\spec.pdf`, "spec.pdf", "failed", nil, "解析失败", nil, 3_000)
	insert("t-run", "url", "https://github.com/egoist/mygo", "mygo", "processing", "extracting", nil, nil, 4_000)

	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}, db: db}
	l := &Library{db: db, dbPath: paths.DatabasePath(), paths: paths}
	l.registerImportList(b)
	svc := newNativeServices(b, newPushHub()).Imports

	page, err := svc.List(ctx, services.ImportQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || len(page.Entries) != 4 {
		t.Fatalf("总数/条目数不对：total=%d entries=%d", page.Total, len(page.Entries))
	}
	// 按创建时间倒序：最新的在前
	if page.Entries[0].ID != "t-run" || page.Entries[3].ID != "t-ok" {
		t.Fatalf("排序不对：%s … %s", page.Entries[0].ID, page.Entries[3].ID)
	}
	run := page.Entries[0]
	if run.Status != "processing" || run.Stage != "extracting" || run.Origin != "desktop" || run.SourceKind != "url" {
		t.Fatalf("进行中任务的字段不对：%+v", run)
	}
	ok := page.Entries[3]
	if ok.Error != "" || ok.Stage != "" || ok.Warning != "" || ok.CreatedAt != 1_000 || ok.UpdatedAt != 1_500 {
		t.Fatalf("空列应解码为零值，时间应是数字：%+v", ok)
	}
	if page.Entries[1].Error != "解析失败" || page.Entries[2].Warning != "语音转写失败" {
		t.Fatalf("错误/警告文本不对：%+v / %+v", page.Entries[1], page.Entries[2])
	}
	if len(page.Active) != 1 || page.Active[0].ID != "t-run" {
		t.Fatalf("active 应只含进行中的任务：%+v", page.Active)
	}
	if page.Counts["completed"] != 2 || page.Counts["failed"] != 1 || page.Counts["processing"] != 1 || page.DegradedCount != 1 {
		t.Fatalf("计数不对：%+v degraded=%d", page.Counts, page.DegradedCount)
	}

	failed, err := svc.List(ctx, services.ImportQuery{Status: "failed", PageSize: 20})
	if err != nil || failed.Total != 1 || failed.Entries[0].ID != "t-fail" {
		t.Fatalf("按状态过滤：%+v %v", failed, err)
	}
	found, err := svc.List(ctx, services.ImportQuery{Query: "egoist"})
	if err != nil || len(found.Entries) != 1 || found.Entries[0].ID != "t-run" {
		t.Fatalf("按关键词搜索：%+v %v", found, err)
	}
}

func TestImportsAdapterOnChangeReceivesPush(t *testing.T) {
	hub := newPushHub()
	svc := newNativeServices(&Bridge{}, hub).Imports
	var n atomic.Int32
	cancel := svc.OnChange(func() { n.Add(1) })
	hub.Dispatch(chImportChanged, map[string]any{"id": "x"})
	cancel()
	hub.Dispatch(chImportChanged, nil)
	if n.Load() != 1 {
		t.Fatalf("OnChange 回调次数 = %d，期望 1", n.Load())
	}
}
