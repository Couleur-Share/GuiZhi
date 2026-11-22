package servicestest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

func TestDataFakeRecordsActionsAndInjectsFailures(t *testing.T) {
	var f Data
	ctx := context.Background()
	p, err := f.RuntimePaths(ctx)
	if err != nil || p.BackupsDir == "" || p.LogsDir == "" {
		t.Fatalf("零值应给出默认目录：%+v %v", p, err)
	}
	f.SetPaths(services.RuntimePaths{BackupsDir: `D:\b`, LogsDir: `D:\l`})
	if p, _ := f.RuntimePaths(ctx); p.BackupsDir != `D:\b` {
		t.Errorf("摆好的目录应原样返回：%+v", p)
	}
	if err := f.OpenPath(ctx, `D:\b`); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("没有关联程序")
	f.Fail("OpenPath", boom)
	if err := f.OpenPath(ctx, `D:\l`); !errors.Is(err, boom) {
		t.Errorf("应返回注入的失败：%v", err)
	}
	f.Fail("OpenPath", nil)
	if err := f.OpenPath(ctx, `D:\l`); err != nil {
		t.Errorf("取消注入后应恢复：%v", err)
	}
	if got := f.CallsOf("OpenPath"); len(got) != 3 || got[0].Args[0] != `D:\b` {
		t.Errorf("应记录全部调用：%+v", got)
	}
	if err := f.Relaunch(ctx); err != nil || len(f.CallsOf("Relaunch")) != 1 {
		t.Errorf("重启应被记录：%v", err)
	}
}

func TestBackupFakeLegacySnapshots(t *testing.T) {
	var f Backup
	ctx := context.Background()
	if list, err := f.List(ctx); err != nil || len(list) != 0 {
		t.Fatalf("零值没有快照：%v %v", list, err)
	}
	a, err := f.Create(ctx)
	if err != nil || a.Kind != services.BackupKindManual {
		t.Fatalf("创建：%+v %v", a, err)
	}
	b, _ := f.Create(ctx)
	if list, _ := f.List(ctx); len(list) != 2 || list[0].FileName != b.FileName {
		t.Errorf("新的应排在前面：%+v", list)
	}
	if err := f.Restore(ctx, "不存在.db"); err == nil {
		t.Error("恢复不存在的快照应失败")
	}
	if err := f.Restore(ctx, a.FileName); err != nil {
		t.Errorf("恢复存在的快照：%v", err)
	}
	if err := f.Delete(ctx, a.FileName); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.List(ctx); len(list) != 1 {
		t.Errorf("删除后应剩一份：%+v", list)
	}
	if err := f.Delete(ctx, a.FileName); err == nil {
		t.Error("重复删除应失败")
	}

	boom := errors.New("磁盘已满")
	f.Fail("Create", boom)
	n := len(f.Legacy())
	if _, err := f.Create(ctx); !errors.Is(err, boom) || len(f.Legacy()) != n {
		t.Errorf("失败不能改列表：%v", err)
	}
}

func TestBackupFakeRepositoryPasswordRules(t *testing.T) {
	var f Backup
	ctx := context.Background()
	if err := f.InitRepository(ctx, "太短"); err == nil {
		t.Fatal("过短的口令应被拒绝")
	}
	if st, _ := f.RepositoryStatus(ctx); st.Initialized {
		t.Error("初始化失败不应改变状态")
	}
	const pw = "correct-horse-battery"
	if err := f.InitRepository(ctx, pw); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.RepositoryStatus(ctx); !st.Initialized || !st.AutomaticAccessAvailable {
		t.Errorf("初始化后应已启用：%+v", st)
	}
	if err := f.ChangePassword(ctx, "wrong-current-password", "another-long-password"); err == nil {
		t.Error("当前口令不对应失败")
	}
	if err := f.ChangePassword(ctx, pw, "another-long-password"); err != nil {
		t.Errorf("修改口令：%v", err)
	}
	if err := f.ChangePassword(ctx, pw, "yet-another-long-password"); err == nil {
		t.Error("改过之后旧口令不再有效")
	}

	created, err := f.CreateSnapshot(ctx, "")
	if err != nil || created.CreatedObjects == 0 || created.Snapshot.Summary == nil {
		t.Fatalf("完整备份：%+v %v", created, err)
	}
	prev, err := f.PreviewRestore(ctx, created.Snapshot.FileName, "")
	if err != nil || prev.Snapshot.FileName != created.Snapshot.FileName {
		t.Fatalf("预检：%+v %v", prev, err)
	}
	_, err = f.PreviewRestore(ctx, "不存在", "")
	var pe *services.RestorePreviewError
	if !errors.As(err, &pe) {
		t.Errorf("预检失败应是 *RestorePreviewError：%v", err)
	}
	if err := f.DeleteSnapshot(ctx, created.Snapshot.FileName); err != nil || len(f.Snapshots()) != 0 {
		t.Errorf("删除快照：%v %v", err, f.Snapshots())
	}
	if _, err := f.ImportPortable(ctx, ""); err == nil {
		t.Error("导入便携备份没有口令应失败")
	}
	if got := f.CallsOf("InitRepository"); len(got) != 2 || got[1].Args[0] != pw {
		t.Errorf("口令应记录在调用参数里供核对：%+v", got)
	}
}

func TestBackupFakeHoldStopsCallUntilReleased(t *testing.T) {
	var f Backup
	release := f.Hold("Create")
	done := make(chan struct{})
	go func() {
		_, _ = f.Create(context.Background())
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Hold 期间调用不应返回")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	release() // 可重复调用
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("放行后调用应返回")
	}
}

func TestConfigTransferFakeChecksPasswordAndRecords(t *testing.T) {
	var f ConfigTransfer
	ctx := context.Background()
	if r, err := f.Read(ctx); err != nil || !r.Canceled {
		t.Fatalf("零值读文件视为取消：%+v %v", r, err)
	}
	f.SetRead(services.ConfigRead{FilePath: `D:\c.json`, Preview: services.ConfigPreview{Encrypted: true, ProviderCount: 2}})
	r, err := f.Read(ctx)
	if err != nil || r.Preview.ProviderCount != 2 {
		t.Fatalf("读：%+v %v", r, err)
	}

	opts := services.ConfigExportOptions{IncludeSecrets: true, Password: "s3cret-pass", IncludeUILayout: true}
	if _, err := f.Export(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if got := f.Exports(); len(got) != 1 || got[0] != opts {
		t.Errorf("应记录导出选项：%+v", got)
	}

	f.WantPassword = "right"
	_, err = f.Apply(ctx, r.FilePath, "wrong", services.ConfigApplySelection{Settings: true})
	var wp *services.ConfigWrongPasswordError
	if !errors.As(err, &wp) {
		t.Fatalf("密码不对应返回 *ConfigWrongPasswordError：%v", err)
	}
	f.SetApply(services.ConfigApplyResult{Warnings: []string{"某项未导入"}})
	res, err := f.Apply(ctx, r.FilePath, "right", services.ConfigApplySelection{Settings: true, Shortcuts: true})
	if err != nil || len(res.Warnings) != 1 {
		t.Fatalf("应用：%+v %v", res, err)
	}
	if got := f.Applies(); len(got) != 2 || !got[1].Selection.Shortcuts {
		t.Errorf("应记录每次导入的选择：%+v", got)
	}
}

// 导出选项的字符串形式不能带出导出密码（%v 进日志的路径）。
func TestConfigExportOptionsStringHidesPassword(t *testing.T) {
	o := services.ConfigExportOptions{IncludeSecrets: true, Password: "s3cret-pass"}
	for _, s := range []string{o.String(), o.GoString()} {
		if strings.Contains(s, "s3cret") {
			t.Errorf("字符串形式不应含密码：%q", s)
		}
	}
}

