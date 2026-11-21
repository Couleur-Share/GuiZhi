package servicestest

import (
	"errors"
	"slices"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

func snapshotViewFor(version string) services.LibraryWebSnapshotView {
	return services.LibraryWebSnapshotView{
		InstanceID: "inst-" + version,
		Document:   "<html></html>",
		Version: &services.LibraryWebSnapshotVersion{
			ID: version, CapturedAt: 10,
			Snapshot: &services.LibraryWebSnapshotMeta{
				Assets:   []services.LibraryWebSnapshotAsset{{FileName: "a.png", SHA256: "h", Bytes: 1}},
				Failures: []services.LibraryWebSnapshotFailure{},
				Warnings: []string{},
			},
		},
	}
}

// 读取按（条目，版本）查预设；没有预设的条目与版本分别是错误；返回的是拷贝，改它不影响预设。
func TestFakeWebSnapshotReads(t *testing.T) {
	ctx := t.Context()
	f := &Library{}
	f.SeedSnapshot("i1", "", snapshotViewFor("v2"))
	f.SeedSnapshot("i1", "v1", snapshotViewFor("v1"))

	got, err := f.WebSnapshot(ctx, "i1", "")
	if err != nil || got.Version.ID != "v2" {
		t.Fatalf("default: %+v %v", got, err)
	}
	got.Version.Snapshot.Assets[0].FileName = "changed"
	got.Version.Snapshot.Warnings = append(got.Version.Snapshot.Warnings, "x")
	again, _ := f.WebSnapshot(ctx, "i1", "")
	if again.Version.Snapshot.Assets[0].FileName != "a.png" || len(again.Version.Snapshot.Warnings) != 0 {
		t.Fatalf("seeded view must not alias: %+v", again.Version.Snapshot)
	}
	if got, err = f.WebSnapshot(ctx, "i1", "v1"); err != nil || got.Version.ID != "v1" {
		t.Fatalf("specific: %+v %v", got, err)
	}
	if _, err = f.WebSnapshot(ctx, "i1", "ghost"); !errors.Is(err, errSnapshotVersionMissing) {
		t.Fatalf("missing version: %v", err)
	}
	if _, err = f.WebSnapshot(ctx, "nope", ""); !errors.Is(err, errSnapshotItemMissing) {
		t.Fatalf("missing item: %v", err)
	}
	if want := []SnapshotCall{{"i1", ""}, {"i1", ""}, {"i1", "v1"}, {"i1", "ghost"}, {"nope", ""}}; !slices.Equal(f.SnapshotCalls(), want) {
		t.Fatalf("calls: %+v", f.SnapshotCalls())
	}

	boom := errors.New("boom")
	f.FailOn("WebSnapshot:i1", boom)
	if _, err = f.WebSnapshot(ctx, "i1", ""); !errors.Is(err, boom) {
		t.Fatalf("FailOn: %v", err)
	}
}

// 闸门只卡一次，并且尊重 ctx 取消。
func TestFakeWebSnapshotHold(t *testing.T) {
	f := &Library{}
	f.SeedSnapshot("i1", "", snapshotViewFor("v1"))
	release := f.HoldSnapshot()
	done := make(chan error, 1)
	go func() {
		_, err := f.WebSnapshot(t.Context(), "i1", "")
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("must block until released")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := f.WebSnapshot(t.Context(), "i1", ""); err != nil {
		t.Fatalf("hold is one-shot: %v", err)
	}
}

// 导出：预设按条目；没预设给固定路径；闸门与 FailOn 同其它入口。
func TestFakeWebSnapshotExport(t *testing.T) {
	ctx := t.Context()
	f := &Library{}
	f.SeedSnapshotExport("i1", services.LibraryWebSnapshotExportResult{Canceled: true})
	if res, err := f.ExportWebSnapshotHTML(ctx, "i1", "v1"); err != nil || !res.Canceled {
		t.Fatalf("seeded: %+v %v", res, err)
	}
	if res, err := f.ExportWebSnapshotHTML(ctx, "i2", "v1"); err != nil || res.Canceled || res.Path != "/export/i2/v1" {
		t.Fatalf("default: %+v %v", res, err)
	}
	if want := []SnapshotCall{{"i1", "v1"}, {"i2", "v1"}}; !slices.Equal(f.SnapshotExports(), want) {
		t.Fatalf("exports: %+v", f.SnapshotExports())
	}
	boom := errors.New("boom")
	f.FailOn("ExportWebSnapshotHTML", boom)
	if _, err := f.ExportWebSnapshotHTML(ctx, "i1", "v1"); !errors.Is(err, boom) {
		t.Fatalf("FailOn: %v", err)
	}
}

// 补采：生成的任务 ID 按条目去重、跨调用递增；预设的 ID 原样返回；与旧入口共用批次记录。
func TestFakeSupplementTasksAndProgress(t *testing.T) {
	ctx := t.Context()
	f := &Library{}
	ids, err := f.SupplementWechatTasks(ctx, []string{"a", "a", "b"})
	if err != nil || !slices.Equal(ids, []string{"snap-task-1", "snap-task-2"}) {
		t.Fatalf("generated: %v %v", ids, err)
	}
	f.SeedSupplementTasks("t-fixed")
	if ids, err = f.SupplementWechatTasks(ctx, []string{"c"}); err != nil || !slices.Equal(ids, []string{"t-fixed"}) {
		t.Fatalf("seeded: %v %v", ids, err)
	}
	if err := f.SupplementWechat(ctx, []string{"d"}); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"a", "a", "b"}, {"c"}, {"d"}}; !slices.EqualFunc(f.Supplemented(), want, slices.Equal) {
		t.Fatalf("batches: %v", f.Supplemented())
	}

	// 状态：只返回登记过的任务，按请求顺序；取消只对未结束的任务生效。
	f.SeedImportStatuses(
		services.LibraryImportTaskStatus{ID: "t1", Status: services.ImportStatusProcessing, RefreshOfItemID: "a"},
		services.LibraryImportTaskStatus{ID: "t2", Status: services.ImportStatusCompleted, RefreshOfItemID: "b"},
	)
	got, err := f.ImportTaskStatuses(ctx, []string{"t2", "ghost", "t1"})
	if err != nil || len(got) != 2 || got[0].ID != "t2" || got[1].ID != "t1" {
		t.Fatalf("statuses: %+v %v", got, err)
	}
	if ok, err := f.CancelImportTask(ctx, "t1"); err != nil || !ok {
		t.Fatalf("cancel running: %v %v", ok, err)
	}
	if ok, err := f.CancelImportTask(ctx, "t2"); err != nil || ok {
		t.Fatalf("cancel finished: %v %v", ok, err)
	}
	if ok, err := f.CancelImportTask(ctx, "ghost"); err != nil || ok {
		t.Fatalf("cancel unknown: %v %v", ok, err)
	}
	got, _ = f.ImportTaskStatuses(ctx, []string{"t1"})
	if got[0].Status != services.ImportStatusCanceled || !got[0].Finished() {
		t.Fatalf("after cancel: %+v", got)
	}
	if want := []string{"t1", "t2", "ghost"}; !slices.Equal(f.CanceledImportTasks(), want) {
		t.Fatalf("cancel calls: %v", f.CanceledImportTasks())
	}

	boom := errors.New("boom")
	f.FailOn("ImportTaskStatuses", boom)
	if _, err := f.ImportTaskStatuses(ctx, []string{"t1"}); !errors.Is(err, boom) {
		t.Fatalf("FailOn statuses: %v", err)
	}
	f.FailOn("CancelImportTask:t1", boom)
	if _, err := f.CancelImportTask(ctx, "t1"); !errors.Is(err, boom) {
		t.Fatalf("FailOn cancel: %v", err)
	}
	f.FailOn("SupplementWechatTasks", boom)
	if _, err := f.SupplementWechatTasks(ctx, []string{"a"}); !errors.Is(err, boom) {
		t.Fatalf("FailOn supplement: %v", err)
	}
}
