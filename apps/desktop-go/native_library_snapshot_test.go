package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

// snapshotAdapterEnv 在网页快照测试环境上再挂 import:list，并让导入队列保持暂停：
// 补采任务只入队、不执行，状态是确定的 pending（需要时由用例自己 Resume）。
func snapshotAdapterEnv(t *testing.T) (*webSnapEnv, libraryAdapter) {
	t.Helper()
	e := newWebSnapEnv(t)
	e.l.registerImportList(e.b)
	e.l.imports.Pause()
	t.Cleanup(func() {
		e.l.imports.Pause()
		deadline := time.Now().Add(5 * time.Second)
		for !e.l.imports.Idle() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
	})
	return e, webAdapterForTest(e)
}

// addWechatSource 给条目登记一个公众号来源，补采才会接受它。
func (e *webSnapEnv) addWechatSource() {
	e.t.Helper()
	e.exec("INSERT INTO source_records (id,item_id,source_type,source_uri,access_uri,captured_at) VALUES (?,?,?,?,?,?)",
		"s1", e.itemID, "url", "https://mp.weixin.qq.com/s/abc", "https://mp.weixin.qq.com/s/abc", 5)
}

// 正常读取：默认版本是比较基线，视图带实例标识、整页文档与快照元数据；
// 三个切片永不为 nil；正文被改过时 Edited 为真，有更新的快照版本时 Pending 为真。
func TestLibrarySnapshotAdapterReadsDefaultVersion(t *testing.T) {
	ctx := t.Context()
	e, svc := snapshotAdapterEnv(t)
	name, hash := e.image("abc")
	e.version("v1", 1000, snapshotFor(name, hash, `<p>a</p><img src="local-image://`+name+`">`, ""), true)

	got, err := svc.WebSnapshot(ctx, e.itemID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Error != "" || got.Version == nil || got.Version.ID != "v1" || got.Version.CapturedAt != 1000 {
		t.Fatalf("版本：%+v", got)
	}
	if got.InstanceID == "" || !strings.Contains(got.Document, `data-instance="`+got.InstanceID+`"`) || !strings.Contains(got.Document, "local-image://"+name) {
		t.Fatalf("文档应带实例标识与本地图引用：%.200s", got.Document)
	}
	if got.Edited || got.Pending {
		t.Fatalf("edited/pending：%+v", got)
	}
	meta := got.Snapshot()
	if meta == nil || len(meta.Assets) != 1 {
		t.Fatalf("快照元数据：%+v", meta)
	}
	if a := meta.Assets[0]; a.FileName != name || a.SHA256 != hash || a.SourceURL != "https://mmbiz.qpic.cn/x" || a.Bytes != 3 {
		t.Fatalf("图片字段：%+v", a)
	}
	if meta.Failures == nil || meta.Warnings == nil || meta.Incomplete() {
		t.Fatalf("失败与警告应是空切片而不是 nil：%+v", meta)
	}

	e.exec("UPDATE knowledge_items SET content=? WHERE id=?", "改过的正文", e.itemID)
	e.version("v2", 2000, snapshotFor(name, hash, "<p>new</p>", ""), false)
	got, err = svc.WebSnapshot(ctx, e.itemID, "")
	if err != nil || !got.Edited || !got.Pending || got.Version.ID != "v1" {
		t.Fatalf("改过正文且有更新版本：%+v %v", got, err)
	}
}

// 指定版本：读到那个版本（它是最新的就没有待处理的更新）；版本不存在、条目不存在、ID 无效都是 error；
// 版本存在但没有快照是「还没有快照」的状态，不是错误；请求里没有版本时不会把空串当作版本 ID 发给处理器。
func TestLibrarySnapshotAdapterReadsSpecificVersionAndRejects(t *testing.T) {
	ctx := t.Context()
	e, svc := snapshotAdapterEnv(t)
	name, hash := e.image("abc")
	e.version("v1", 1000, snapshotFor(name, hash, "<p>a</p>", ""), true)
	e.version("v2", 2000, snapshotFor(name, hash, "<p>b</p>", ""), false)
	e.version("v3", 3000, nil, false)

	got, err := svc.WebSnapshot(ctx, e.itemID, "v2")
	if err != nil || got.Version == nil || got.Version.ID != "v2" || got.Version.CapturedAt != 2000 || got.Pending {
		t.Fatalf("指定版本：%+v %v", got, err)
	}
	if !strings.Contains(got.Snapshot().Assets[0].FileName, "wechat-") {
		t.Fatalf("快照元数据：%+v", got.Snapshot())
	}

	none, err := svc.WebSnapshot(ctx, e.itemID, "v3")
	if err != nil || none.Version != nil || none.Error != "" || none.Document != "" || none.Snapshot() != nil {
		t.Fatalf("没有快照的版本应是空视图：%+v %v", none, err)
	}

	if _, err := svc.WebSnapshot(ctx, e.itemID, "ghost"); err == nil || err.Error() != "原文版本不存在" {
		t.Fatalf("版本不存在应是错误：%v", err)
	}
	if _, err := svc.WebSnapshot(ctx, "nope", ""); err == nil || err.Error() != "条目不存在" {
		t.Fatalf("条目不存在应是错误：%v", err)
	}
	if _, err := svc.WebSnapshot(ctx, "", ""); err == nil || err.Error() != "记录 ID 无效" {
		t.Fatalf("ID 无效应是错误：%v", err)
	}
	if _, err := svc.WebSnapshot(ctx, e.itemID, strings.Repeat("x", 101)); err == nil || err.Error() != "记录 ID 无效" {
		t.Fatalf("版本 ID 过长应是错误：%v", err)
	}
}

// 失败归一化：快照损坏（图片缺失、校验失败、格式无效）是视图里的 Error 与空版本，不是 Go 错误；
// 失败与警告原样解码，Incomplete 为真。
func TestLibrarySnapshotAdapterNormalizesFailures(t *testing.T) {
	ctx := t.Context()
	e, svc := snapshotAdapterEnv(t)
	name, hash := e.image("abc")
	snap := snapshotFor(name, hash, "<p>a</p>", "")
	snap["failures"] = []any{map[string]any{"url": "https://mmbiz.qpic.cn/y", "reason": "timeout"}}
	snap["warnings"] = []any{"one image skipped"}
	e.version("v1", 1000, snap, true)

	got, err := svc.WebSnapshot(ctx, e.itemID, "")
	if err != nil {
		t.Fatal(err)
	}
	meta := got.Snapshot()
	if meta == nil || !meta.Incomplete() || len(meta.Failures) != 1 || meta.Failures[0].URL != "https://mmbiz.qpic.cn/y" || meta.Failures[0].Reason != "timeout" ||
		!slices.Equal(meta.Warnings, []string{"one image skipped"}) {
		t.Fatalf("失败与警告：%+v", meta)
	}

	if err := os.WriteFile(filepath.Join(e.l.paths.ImagesDir(), name), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = svc.WebSnapshot(ctx, e.itemID, "")
	if err != nil || got.Version != nil || got.Error != "原文图片校验失败，请重新补采" || got.Document != "" {
		t.Fatalf("校验失败应是视图里的 Error：%+v %v", got, err)
	}
	if err := os.Remove(filepath.Join(e.l.paths.ImagesDir(), name)); err != nil {
		t.Fatal(err)
	}
	got, err = svc.WebSnapshot(ctx, e.itemID, "")
	if err != nil || got.Version != nil || !strings.Contains(got.Error, "原文图片缺失") {
		t.Fatalf("图片缺失应是视图里的 Error：%+v %v", got, err)
	}

	bad := snapshotFor(name, hash, "<p>x</p>", "")
	bad["formatVersion"] = 2
	e.exec("DELETE FROM web_source_baselines")
	e.exec("DELETE FROM web_source_versions")
	e.version("v9", 3000, bad, true)
	got, err = svc.WebSnapshot(ctx, e.itemID, "")
	if err != nil || got.Version != nil || got.Error != "原文快照格式无效" {
		t.Fatalf("格式无效应是视图里的 Error：%+v %v", got, err)
	}
}

// 回复壳的几种形状（桩处理器）：ok 为假带处理器给的原因，没给原因用兜底；ok 为真但没有 data 是错误。
func TestLibrarySnapshotAdapterReplyShell(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryOpsAdapterForTest(t)
	reply := map[string]any{"ok": false, "error": "条目不存在"}
	b.handlers["web:snapshot"] = func(context.Context, []json.RawMessage) (any, error) { return reply, nil }
	b.handlers["web:snapshot-export"] = func(context.Context, []json.RawMessage) (any, error) { return reply, nil }

	if _, err := svc.WebSnapshot(ctx, "a", ""); err == nil || err.Error() != "条目不存在" {
		t.Fatalf("读取失败带处理器的原因：%v", err)
	}
	if _, err := svc.ExportWebSnapshotHTML(ctx, "a", "v"); err == nil || err.Error() != "条目不存在" {
		t.Fatalf("导出失败带处理器的原因：%v", err)
	}
	reply = map[string]any{"ok": false}
	if _, err := svc.WebSnapshot(ctx, "a", ""); err == nil || err.Error() != "读取网页快照失败" {
		t.Fatalf("读取失败没给原因用兜底：%v", err)
	}
	if _, err := svc.ExportWebSnapshotHTML(ctx, "a", "v"); err == nil || err.Error() != "导出网页快照失败" {
		t.Fatalf("导出失败没给原因用兜底：%v", err)
	}
	reply = map[string]any{"ok": true}
	if _, err := svc.WebSnapshot(ctx, "a", ""); err == nil {
		t.Fatal("ok 但没有 data 不能当成功")
	}
	if _, err := svc.ExportWebSnapshotHTML(ctx, "a", "v"); err == nil {
		t.Fatal("ok 但没有 data 不能当成功")
	}
}

// 导出经真实处理器：没有目录选择框与用户取消都是 Canceled；成功给目录与 Incomplete；
// 无快照的版本与不存在的版本是 error。
func TestLibrarySnapshotAdapterExportsAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	e, svc := snapshotAdapterEnv(t)
	name, hash := e.image("abc")
	snap := snapshotFor(name, hash, `<p>x</p><img src="local-image://`+name+`">`, "")
	snap["warnings"] = []any{"one image skipped"}
	e.version("v1", 1700000000123, snap, true)
	e.version("v2", 1700000000456, snapshotFor(name, hash, "<p>y</p>", ""), false)
	e.version("v3", 1700000000789, nil, false)

	res, err := svc.ExportWebSnapshotHTML(ctx, e.itemID, "v1")
	if err != nil || !res.Canceled || res.Path != "" {
		t.Fatalf("没有目录选择框应视为取消：%+v %v", res, err)
	}

	parent := t.TempDir()
	e.l.pickDirectory = func(string) (string, error) { return parent, nil }
	res, err = svc.ExportWebSnapshotHTML(ctx, e.itemID, "v1")
	if err != nil || res.Canceled || !res.Incomplete || res.Path == "" || filepath.Dir(res.Path) != parent {
		t.Fatalf("导出不完整的快照：%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "index.html")); err != nil {
		t.Fatalf("应写出 index.html：%v", err)
	}
	res, err = svc.ExportWebSnapshotHTML(ctx, e.itemID, "v2")
	if err != nil || res.Canceled || res.Incomplete || res.Path == "" {
		t.Fatalf("导出完整的快照：%+v %v", res, err)
	}

	e.l.pickDirectory = func(string) (string, error) { return "", nil }
	if res, err = svc.ExportWebSnapshotHTML(ctx, e.itemID, "v1"); err != nil || !res.Canceled {
		t.Fatalf("用户取消不算失败：%+v %v", res, err)
	}
	if _, err := svc.ExportWebSnapshotHTML(ctx, e.itemID, "v3"); err == nil || err.Error() != "此版本没有原文排版" {
		t.Fatalf("无快照的版本应失败：%v", err)
	}
	if _, err := svc.ExportWebSnapshotHTML(ctx, e.itemID, "ghost"); err == nil || err.Error() != "原文版本不存在" {
		t.Fatalf("版本不存在应失败：%v", err)
	}
	if _, err := svc.ExportWebSnapshotHTML(ctx, e.itemID, ""); err == nil || err.Error() != "记录 ID 无效" {
		t.Fatalf("导出必须指定版本：%v", err)
	}
}

// 补采任务与进度经真实处理器：入队返回新建的任务 ID（重复条目去重），任务状态带上它要刷新的条目；
// 队列暂停时是 pending，取消后变 canceled，对已结束或不存在的任务取消返回 false 而不是错误；
// 恢复队列后任务跑到终态，失败原因进 Error。入队的前置失败带处理器的原因。
func TestLibrarySnapshotAdapterSupplementAndProgressAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	e, svc := snapshotAdapterEnv(t)

	if _, err := svc.SupplementWechatTasks(ctx, []string{e.itemID}); err == nil || err.Error() != "此条目不是微信公众号文章" {
		t.Fatalf("没有公众号来源应失败：%v", err)
	}
	if _, err := svc.SupplementWechatTasks(ctx, nil); err == nil || err.Error() != "单次补采 1–50 篇文章" {
		t.Fatalf("空列表应失败：%v", err)
	}
	if err := svc.SupplementWechat(ctx, make([]string, 51)); err == nil || err.Error() != "单次补采 1–50 篇文章" {
		t.Fatalf("超过 50 篇应失败：%v", err)
	}

	e.addWechatSource()
	ids, err := svc.SupplementWechatTasks(ctx, []string{e.itemID, e.itemID})
	if err != nil || len(ids) != 1 || ids[0] == "" {
		t.Fatalf("入队（重复 ID 去重）：%v %v", ids, err)
	}
	statuses, err := svc.ImportTaskStatuses(ctx, ids)
	if err != nil || len(statuses) != 1 {
		t.Fatalf("读状态：%+v %v", statuses, err)
	}
	if s := statuses[0]; s.ID != ids[0] || s.Status != services.ImportStatusPending || s.RefreshOfItemID != e.itemID || s.Error != "" || s.Warning != "" || s.Finished() {
		t.Fatalf("暂停的队列里应是 pending：%+v", s)
	}

	canceled, err := svc.CancelImportTask(ctx, ids[0])
	if err != nil || !canceled {
		t.Fatalf("取消排队中的任务：%v %v", canceled, err)
	}
	if after, _ := svc.ImportTaskStatuses(ctx, ids); len(after) != 1 || after[0].Status != services.ImportStatusCanceled || !after[0].Finished() {
		t.Fatalf("取消后：%+v", after)
	}
	if canceled, err = svc.CancelImportTask(ctx, ids[0]); err != nil || canceled {
		t.Fatalf("对已结束的任务取消应返回 false：%v %v", canceled, err)
	}
	if canceled, err = svc.CancelImportTask(ctx, "ghost"); err != nil || canceled {
		t.Fatalf("对不存在的任务取消应返回 false：%v %v", canceled, err)
	}
	// 旧的只关心成败的入口仍然可用。
	if err := svc.SupplementWechat(ctx, []string{e.itemID}); err != nil {
		t.Fatalf("SupplementWechat：%v", err)
	}

	e.l.imports.Resume()
	next, err := svc.SupplementWechatTasks(ctx, []string{e.itemID})
	if err != nil || len(next) != 1 {
		t.Fatalf("再次补采：%v %v", next, err)
	}
	var last services.LibraryImportTaskStatus
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, err := svc.ImportTaskStatuses(ctx, next)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 1 && got[0].Finished() {
			last = got[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if last.ID != next[0] || last.Status != services.ImportStatusFailed || !strings.Contains(last.Error, "测试不执行抽取") || last.RefreshOfItemID != e.itemID {
		t.Fatalf("任务应跑到失败并带原因：%+v", last)
	}
}

// 状态查询的口径（桩处理器）：合并 active 与 entries、按请求顺序返回、重复 ID 只回一份、查不到的任务不在结果里；
// 找齐就不再翻页；翻页有上限，游标不终止时也会停下；不带 ID 不访问处理器；处理器出错原样返回。
func TestLibrarySnapshotAdapterImportStatusesPaging(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryOpsAdapterForTest(t)
	var args []map[string]any
	pages := map[string]map[string]any{
		"": {
			"entries":    []any{map[string]any{"id": "t3", "status": "completed", "refreshOfItemId": "i3", "warning": "w"}},
			"active":     []any{map[string]any{"id": "t1", "status": "processing", "refreshOfItemId": "i1"}},
			"nextCursor": "c1",
		},
		"c1": {
			"entries":    []any{map[string]any{"id": "t2", "status": "failed", "refreshOfItemId": "i2", "error": "boom"}, map[string]any{"id": "other", "status": "completed"}},
			"active":     []any{},
			"nextCursor": nil,
		},
	}
	b.handlers["import:list"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		var arg map[string]any
		if err := json.Unmarshal(a[0], &arg); err != nil {
			t.Fatalf("import:list 必须带对象参数：%v", err)
		}
		args = append(args, arg)
		cursor, _ := arg["cursor"].(string)
		return pages[cursor], nil
	}

	got, err := svc.ImportTaskStatuses(ctx, []string{"t2", "t1", "t2", "t3", "gone"})
	if err != nil {
		t.Fatal(err)
	}
	want := []services.LibraryImportTaskStatus{
		{ID: "t2", Status: "failed", RefreshOfItemID: "i2", Error: "boom"},
		{ID: "t1", Status: "processing", RefreshOfItemID: "i1"},
		{ID: "t3", Status: "completed", RefreshOfItemID: "i3", Warning: "w"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("结果：%+v", got)
	}
	if len(args) != 2 || args[0]["origin"] != "all" || args[0]["status"] != "all" || args[0]["pageSize"] != float64(importStatusPageSize) || args[1]["cursor"] != "c1" {
		t.Fatalf("翻页参数：%+v", args)
	}

	args = nil
	if got, err = svc.ImportTaskStatuses(ctx, []string{"t1", "t3"}); err != nil || len(got) != 2 || len(args) != 1 {
		t.Fatalf("在第一页找齐就不再翻页：%+v %v 请求 %d 次", got, err, len(args))
	}

	args = nil
	pages["c1"]["nextCursor"] = "c1" // 游标永不终止
	if got, err = svc.ImportTaskStatuses(ctx, []string{"never"}); err != nil || len(got) != 0 || len(args) != importStatusMaxPages {
		t.Fatalf("翻页应有上限：%+v %v 请求 %d 次", got, err, len(args))
	}

	args = nil
	if got, err = svc.ImportTaskStatuses(ctx, nil); err != nil || got == nil || len(got) != 0 || len(args) != 0 {
		t.Fatalf("不带 ID 不访问处理器：%+v %v 请求 %d 次", got, err, len(args))
	}

	b.handlers["import:list"] = func(context.Context, []json.RawMessage) (any, error) {
		return nil, context.DeadlineExceeded
	}
	if _, err := svc.ImportTaskStatuses(ctx, []string{"t1"}); err == nil {
		t.Fatal("处理器出错应原样返回")
	}
}

// 补采入队的回复壳（桩处理器）：ok 为假带处理器的原因，没给原因用兜底；ok 为真没有任务数组是空列表；只取有 ID 的任务。
func TestLibrarySnapshotAdapterSupplementReplyShell(t *testing.T) {
	ctx := t.Context()
	svc, b, _ := newLibraryOpsAdapterForTest(t)
	reply := map[string]any{"ok": true, "data": []any{map[string]any{"id": "a"}, map[string]any{"id": ""}, map[string]any{"id": "b"}}}
	b.handlers["web:snapshot-enqueue"] = func(context.Context, []json.RawMessage) (any, error) { return reply, nil }
	b.handlers["import:cancel"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		var id string
		_ = json.Unmarshal(a[0], &id)
		return id == "live", nil
	}

	if ids, err := svc.SupplementWechatTasks(ctx, []string{"x"}); err != nil || !slices.Equal(ids, []string{"a", "b"}) {
		t.Fatalf("任务 ID：%v %v", ids, err)
	}
	reply = map[string]any{"ok": true}
	if ids, err := svc.SupplementWechatTasks(ctx, []string{"x"}); err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("没有任务数组应是空列表：%v %v", ids, err)
	}
	reply = map[string]any{"ok": false, "error": "导入服务尚未就绪"}
	if _, err := svc.SupplementWechatTasks(ctx, []string{"x"}); err == nil || err.Error() != "导入服务尚未就绪" {
		t.Fatalf("失败带处理器的原因：%v", err)
	}
	reply = map[string]any{"ok": false}
	if err := svc.SupplementWechat(ctx, []string{"x"}); err == nil || err.Error() != "补采公众号网页失败" {
		t.Fatalf("没给原因用兜底：%v", err)
	}
	if ok, err := svc.CancelImportTask(ctx, "live"); err != nil || !ok {
		t.Fatalf("取消：%v %v", ok, err)
	}
	if ok, err := svc.CancelImportTask(ctx, "done"); err != nil || ok {
		t.Fatalf("取消已结束的：%v %v", ok, err)
	}
}
