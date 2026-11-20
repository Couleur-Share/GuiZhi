package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// openAdapterTestDB 在临时用户数据目录里建一个真实的库（含迁移）并打开；测试结束时自动关闭。
func openAdapterTestDB(t *testing.T) (context.Context, Paths, *sqlite.DB) {
	t.Helper()
	ctx := context.Background()
	paths := Paths{UserData: t.TempDir()}
	if err := os.MkdirAll(paths.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	lease, err := initDatabaseFile(ctx, paths.DatabasePath(), dbInitOptions{BackupsDir: paths.BackupsDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.release)
	db, err := sqlite.Open(ctx, paths.DatabasePath(), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return ctx, paths, db
}

// 条目事实经真实的 knowledge:create / knowledge:get 往返：字数是渲染层 `content.trim().length` 的口径（UTF-16 码元），
// 媒体个数按文件名去重，标签取名称；不存在的条目是 found=false 而不是错误。
func TestImportsAdapterItemFactsAgainstRealHandler(t *testing.T) {
	ctx, paths, db := openAdapterTestDB(t)
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}, db: db}
	l := &Library{db: db, dbPath: paths.DatabasePath(), paths: paths}
	l.registerWrite(b)
	svc := newNativeServices(b, newPushHub()).Imports

	// 正文两端的空白不算；😀 在 UTF-16 里占两个码元；同名媒体引用只算一个。
	content := "\n  正文😀 local-image://a.png local-video://b.mp4 local-image://a.png  \n"
	created, err := callAs[struct {
		ID string `json:"id"`
	}](ctx, b, "knowledge:create", map[string]any{
		"title": "AI 中转站调研", "content": content, "transcript": "文字稿", "tagNames": []string{"b", "a"},
	})
	if err != nil || created.ID == "" {
		t.Fatalf("创建条目失败：%+v %v", created, err)
	}

	facts, found, err := svc.ItemFacts(ctx, created.ID)
	if err != nil || !found {
		t.Fatalf("应读到刚创建的条目：found=%v err=%v", found, err)
	}
	want := services.ImportItemFacts{ID: created.ID, Title: "AI 中转站调研", BodyLength: 64, TranscriptLength: 3, AssetCount: 2, Tags: []string{"a", "b"}}
	if facts.ID != want.ID || facts.Title != want.Title || facts.BodyLength != want.BodyLength ||
		facts.TranscriptLength != want.TranscriptLength || facts.AssetCount != want.AssetCount || strings.Join(facts.Tags, ",") != "a,b" {
		t.Fatalf("条目事实不对：\n got  %+v\n want %+v", facts, want)
	}

	missing, found, err := svc.ItemFacts(ctx, "no-such-item")
	if err != nil || found || missing.ID != "" {
		t.Fatalf("不存在的条目应是 found=false 且没有错误：%+v found=%v err=%v", missing, found, err)
	}
}

// 用真实库与真实处理器验证 DTO 与 import:list 输出的对应关系：字段改名或类型变化时这里先红。
func TestImportsAdapterAgainstRealHandler(t *testing.T) {
	ctx, paths, db := openAdapterTestDB(t)

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

// 原生界面的错误日志必须落进渲染进程用的同一个 error.log，且走同一套截断与脱敏。
func TestLogAdapterWritesThroughTheRendererHandler(t *testing.T) {
	paths := Paths{UserData: t.TempDir()}
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}}
	l := &Library{paths: paths}
	l.registerAppRuntime(b)
	log := newNativeServices(b, newPushHub()).Log

	home, _ := os.UserHomeDir()
	log.AppError("native", "保存设置", `写入 `+home+`\config\x.json 失败`)
	log.AppError("native", "超长", strings.Repeat("长", 3000))
	log.AppError("native", "空消息", "   ") // 与渲染进程一致：空消息不记

	raw, err := os.ReadFile(filepath.Join(paths.LogsDir(), "error.log"))
	if err != nil {
		t.Fatalf("应写出 error.log：%v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("应有 2 条日志（空消息不记）：%d\n%s", len(lines), raw)
	}
	var first, second map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if first["scope"] != "native" || first["action"] != "保存设置" {
		t.Errorf("范围/动作不对：%v", first)
	}
	msg, _ := first["message"].(string)
	if home != "" && strings.Contains(strings.ToLower(msg), strings.ToLower(home)) {
		t.Errorf("家目录应被脱敏成 ~：%s", msg)
	}
	if !strings.Contains(msg, "~") {
		t.Errorf("脱敏后应含 ~：%s", msg)
	}
	long, _ := second["message"].(string)
	if n := utf16Len(long); n != 2000 {
		t.Errorf("超长内容应截到 2000 个 UTF-16 码元：%d", n)
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

// webLinkOpener 只放行带主机名的 http / https 地址：来源链接来自采集到的网页，其余协议（file:、javascript:、ms-settings: 等）一律拒绝，
// 被拒绝时不能调到系统的打开动作；放行时交给系统的是解析后重新序列化的地址。
func TestWebLinkOpenerOnlyAllowsHTTPAndHTTPS(t *testing.T) {
	cases := []struct {
		raw     string
		want    string // 非空：应放行并交出这个地址
		blocked bool
	}{
		{raw: "https://example.com/a?b=1#c", want: "https://example.com/a?b=1#c"},
		{raw: "http://example.com", want: "http://example.com"},
		{raw: "HTTPS://Example.com/x", want: "https://Example.com/x"}, // 协议名大小写不敏感，序列化后统一小写
		{raw: "file:///C:/Windows/System32/calc.exe", blocked: true},
		{raw: "javascript:alert(1)", blocked: true},
		{raw: "ms-settings:network", blocked: true},
		{raw: "//example.com/x", blocked: true}, // 没有协议
		{raw: "https:///nohost", blocked: true}, // 没有主机名
		{raw: "https://", blocked: true},
		{raw: "", blocked: true},
		{raw: "https://exa mple.com/", blocked: true}, // 解析失败
	}
	for _, c := range cases {
		var got []string
		open := webLinkOpener(func(u string) error { got = append(got, u); return nil })
		err := open(c.raw)
		if c.blocked {
			if err == nil || len(got) != 0 {
				t.Errorf("%q 应被拒绝且不调用系统打开，实际 err=%v 调用 %v", c.raw, err, got)
			}
			continue
		}
		if err != nil || len(got) != 1 || got[0] != c.want {
			t.Errorf("%q 应放行并交出 %q，实际 err=%v 调用 %v", c.raw, c.want, err, got)
		}
	}
	// 系统打开失败原样返回（界面把它弹成带原因的提示）。
	failing := webLinkOpener(func(string) error { return os.ErrPermission })
	if err := failing("https://example.com"); err != os.ErrPermission {
		t.Errorf("系统打开失败的原因应原样返回，实际 %v", err)
	}
}
